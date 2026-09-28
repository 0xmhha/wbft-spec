#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The wbft-spec authors
# SPDX-License-Identifier: LGPL-3.0-or-later
"""Independent cross-check of stage-2 vectors (no go-stablenet code involved).

Recomputes expected values from the pseudocode of the specification:

  validators/quorum            A-04 §2.1 in binary64 (Python float)
  validators/proposer          A-04 §5.1 calc_proposer (uint64 seed)
  validators/epoch_boundary    A-04 §4.1
  validators/shuffle           A-04 §6.4 compute_shuffled_index (Keccak-256)
  validators/validators_at     A-04 §3.1 epoch_info_for (canonical first, then walk back,
                               including non-canonical fixture headers)
  validators/next_epoch_info   A-04 §6.2 compute_next_epoch_info (with the
                               candidate order and the shuffle); the three epochs
                               of A-04 §6.8 are also compared with the diligence
                               values printed there
  chain/config_at              A-01 §6.5 config_at (transitions sorted by block)
  timers/round_timeout         A-06 §4.1 round_timeout, and the Warn record
  header/build_proposal_header A-08 §3.2: fixed fields, Time, previous seals and
                               their merge bitmap, MixDigest, and the randao
                               reveal (python-ecdsa, RFC 6979 with SHA-256, low S)

Not covered: header/verify_header and header/verify_light (their verdicts
need BLS pairings), the merged aggregate signatures of build_proposal_header
and the BLS keys used by validators_at beyond byte equality.

Usage: python3 xcheck_stage2.py [VECTORS_DIR]   (default ../../../vectors)
Requires: pycryptodome, ecdsa, rlp, pyyaml.  Exit code 1 on any mismatch.
"""
import functools, hashlib, math, pathlib, struct, sys

import ecdsa
import rlp
import yaml
from Crypto.Hash import keccak

ROOT = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).resolve().parents[3] / "vectors")
U64 = 2**64 - 1
D = 1_000_000
DEFAULT_DILIGENCE = 1_900_000


def k256(b: bytes) -> bytes:
    return keccak.new(digest_bits=256, data=b).digest()


def hx(s: str) -> bytes:
    return bytes.fromhex(s[2:])


def h0x(b: bytes) -> str:
    return "0x" + b.hex()


def load(case):
    inp = yaml.safe_load((case / "input.yaml").read_text())
    p = case / "expected.yaml"
    return inp, (yaml.safe_load(p.read_text()) if p.exists() else None)


def cases(runner, handler):
    d = ROOT / runner / handler
    return sorted(p for p in d.iterdir() if p.is_dir()) if d.exists() else []


stats, bad = {}, []


def check(handler, case, cond, detail=""):
    s = stats.setdefault(handler, [0, 0])
    s[0] += 1
    if not cond:
        s[1] += 1
        bad.append(f"{handler}/{case.name} {detail}")


# ------------------------------------------------------------ order
def tie_sort(data, less):
    """Sort data by less(x, y); equal elements keep their order (stable)."""
    return sorted(data, key=functools.cmp_to_key(lambda x, y: -1 if less(x, y) else 1 if less(y, x) else 0))




def sort_candidates(dil):
    return tie_sort(list(range(len(dil))), lambda x, y: dil[x] > dil[y])


# ------------------------------------------------------------ A-04 / A-01
def f_value(n):
    return float(n - 1) / float(3)


def calc_proposer(addrs, last, rnd, policy):
    size = len(addrs)
    if size == 0:
        return None
    if last == bytes(20):
        seed = rnd
    else:
        off = addrs.index(last) if last in addrs else 0
        seed = (off + rnd) & U64
        if policy != 1:
            seed = (seed + 1) & U64
    return seed % size


def shuffle(index, count, seed):
    if index >= count:
        raise ValueError("out of bounds")
    for r in range(33):
        pivot = struct.unpack("<Q", k256(seed + bytes([r]))[:8])[0] % count
        flip = (pivot + count - index) % count
        pos = max(index, flip)
        src = k256(seed + bytes([r]) + struct.pack("<Q", pos >> 8)[:4])
        if (src[(pos & 0xFF) >> 3] >> (pos & 7)) & 1:
            index = flip
    return index


def transitions_sorted(cfg):
    ts = cfg["transitions"]
    return tie_sort(list(ts), lambda x, y: int(x["block"]) < int(y["block"]))


def epoch_schedule(cfg, n):
    length, anchor = int(cfg["wbft"]["epoch_length"]), 0
    for t in transitions_sorted(cfg):
        if int(t["block"]) > n:
            break
        if int(t["epoch_length"]) == 0:
            continue
        anchor, length = int(t["block"]), int(t["epoch_length"])
    return anchor, length


def is_epoch_block(cfg, n):
    a, l = epoch_schedule(cfg, n)
    return (n - a) % l == 0


def last_epoch_block(cfg, n):
    a, l = epoch_schedule(cfg, n)
    return n - (n - a) % l


def config_at(cfg, n):
    w = cfg["wbft"]
    c = {"request_timeout": (int(w["request_timeout_seconds"]) * 1000) & U64 if int(w["request_timeout_seconds"]) else 0,
         "block_period": int(w["block_period_seconds"]), "epoch": int(w["epoch_length"]),
         "proposer_policy": None if w["proposer_policy"] is None else int(w["proposer_policy"]),
         "max_request_timeout_seconds": 0 if w["max_request_timeout_seconds"] is None else int(w["max_request_timeout_seconds"]),
         "allowed_future_block_time": int(w["allowed_future_block_time"])}
    for t in transitions_sorted(cfg):
        if int(t["block"]) > n:
            break
        if int(t["request_timeout_seconds"]):
            c["request_timeout"] = (int(t["request_timeout_seconds"]) * 1000) & U64
        if int(t["block_period_seconds"]):
            c["block_period"] = int(t["block_period_seconds"])
        if int(t["epoch_length"]):
            c["epoch"] = int(t["epoch_length"])
        if t["proposer_policy"] is not None:
            c["proposer_policy"] = int(t["proposer_policy"])
        if t["max_request_timeout_seconds"] is not None:
            c["max_request_timeout_seconds"] = int(t["max_request_timeout_seconds"])
    return c


# ------------------------------------------------------------ A-06
def wrap64(x):
    x &= U64
    return x - 2**64 if x >= 2**63 else x


def round_timeout(rt_ms, max_s, rnd):
    r = rnd & U64
    base, cap = wrap64(rt_ms * 10**6), wrap64(max_s * 10**9)
    if cap > 0:
        t = base
        for _ in range(r):
            t = wrap64(t * 2)
            if t > cap:
                t = cap
                break
        if t < base:
            return cap, "cap_overflow_guard"
        return t, "none"
    try:
        f = float(2.0 ** r) * float(base)
    except OverflowError:
        f = math.inf
    if math.isnan(f) or math.isinf(f) or f > float(2**63 - 1):
        return 2**63 - 1, "max_int64_clamp"
    return int(f), "none"


# ------------------------------------------------------------ headers
def be_int(b):
    return int.from_bytes(b, "big")


def be_min(i):
    return i.to_bytes((i.bit_length() + 7) // 8, "big")


class Hdr:
    def __init__(self, raw):
        self.raw = raw
        h = rlp.decode(raw)
        self.f = h
        self.parent, self.coinbase = h[0], h[2]
        self.difficulty, self.number, self.time = be_int(h[7]), be_int(h[8]), be_int(h[11])
        self.extra_raw, self.mix, self.nonce = h[12], h[13], h[14]
        try:
            e = rlp.decode(h[12])
            self.x = e if isinstance(e, list) and len(e) == 10 else None
        except Exception:
            self.x = None
        if self.difficulty == 1 and self.x is not None:
            e = list(self.x)
            e[5], e[6], e[7] = b"", [], []
            f = list(h)
            f[12] = rlp.encode(e)
            self.hash = k256(rlp.encode(f))
        else:
            self.hash = k256(raw)

    def seal(self, i):  # 3 prev prepared, 4 prev committed, 6 prepared, 7 committed
        s = self.x[i]
        return None if s == [] else s

    def epoch_info(self):
        e = self.x[9]
        if e == []:
            return None
        return {"candidates": [(c[0], be_int(c[1])) for c in e[0]], "validators": [be_int(v) for v in e[1]], "keys": list(e[2])}


def sealers(bitmap):
    return [8 * i + b for i, byte in enumerate(bitmap) for b in range(8) if byte >> b & 1]


class Chain:
    def __init__(self, fx):
        self.cfg = fx["config"]
        self.hs = [Hdr(hx(fx["genesis"]))] + [Hdr(hx(h)) for h in fx["headers"]]
        self.by_hash = {h.hash: h for h in self.hs}
        self.by_num = {h.number: h for h in self.hs}
        # non-canonical headers (A-11 §3.1 chain fixture, optional): found by hash only
        for raw in fx.get("non_canonical", []):
            h = Hdr(hx(raw))
            self.by_hash[h.hash] = h

    def header(self, hash_, num):
        h = self.by_hash.get(hash_)
        return h if h is not None and h.number == num else None

    def epoch_info_for(self, number, parent_hash):
        L = last_epoch_block(self.cfg, (number - 1) & U64)
        eh = self.by_num.get(L)
        if eh is None:
            hh, hn = parent_hash, (number - 1) & U64
            while True:
                b = self.header(hh, hn)
                if b is None:
                    raise ValueError("unknown ancestor")
                if b.number == L:
                    eh = b
                    break
                hh, hn = b.parent, (b.number - 1) & U64
        ei = eh.epoch_info()
        if ei is None:
            raise ValueError("epochInfo is nil")
        return L, ei

    def governing(self, h):
        if h.number == 0:
            return 0, h.epoch_info()
        return self.epoch_info_for(h.number, h.parent)

    def validators_at(self, number, parent_hash):
        if number == 0:
            i = self.cfg["init"]
            return [(hx(a), hx(k)) for a, k in zip(i["validators"], i["bls_public_keys"])], self.cfg["wbft"]["proposer_policy"]
        _, ei = self.epoch_info_for(number, parent_hash)
        out = [(cand_addr(ei, v), ei["keys"][k]) for k, v in enumerate(ei["validators"])]
        return out, config_at(self.cfg, number)["proposer_policy"]


def cand_addr(ei, i):
    return ei["candidates"][i][0] if i < len(ei["candidates"]) else bytes(20)


def next_epoch_info(ch, e_hdr, cands):
    e = e_hdr.number
    if not is_epoch_block(ch.cfg, e):
        return None
    L, latest = ch.governing(e_hdr)
    proposed, submitted, being = {}, {}, {}
    proposers, first_proposer, last_prop, vdiff, E = [], bytes(20), bytes(20), 0, 0
    it = e_hdr
    while it.number != L:
        proposers.append(it.coinbase)
        parent = ch.header(it.parent, (it.number - 1) & U64)
        if parent is None and it is e_hdr:
            raise ValueError("parent of the epoch header missing")
        info = latest
        if parent.number == L:
            _, info = ch.governing(parent)
            first_proposer, last_prop = it.coinbase, parent.coinbase
            vdiff = len(latest["validators"]) - len(info["validators"])
        if parent.number == 0:
            being[it.coinbase] = being.get(it.coinbase, 0) - 1
        else:
            for idx in (3, 4):
                s = it.seal(idx)
                if s is None:
                    raise ValueError("empty seals")
                sg = []
                for k in sealers(s[0]):
                    a = cand_addr(info, info["validators"][k])
                    if a == bytes(20):
                        raise ValueError("validator address is zero")
                    sg.append(a)
                proposed[it.coinbase] = proposed.get(it.coinbase, 0) + len(sg)
                for a in sg:
                    submitted[a] = submitted.get(a, 0) + 1
        it = parent
        E += 1
    cmap = {c[0]: {"d": c[1], "is": False, "was": False} for c in latest["candidates"]}
    current = []
    for i in latest["validators"]:
        a = cand_addr(latest, i)
        cmap[a]["is"] = True
        current.append(a)
    if L > 0:
        prior_hdr = ch.header(it.parent, (L - 1) & U64)
        _, prior = ch.governing(prior_hdr)
        for i in prior["validators"]:
            a = cand_addr(prior, i)
            if a in cmap:
                cmap[a]["was"] = True
    policy = config_at(ch.cfg, e)["proposer_policy"]
    for author in reversed(proposers):
        rnd = 0
        while True:
            if rnd >= len(current):
                raise ValueError("failed to find valid proposer")
            p = current[calc_proposer(current, last_prop, rnd, policy)]
            being[p] = being.get(p, 0) + 1
            if p == author:
                break
            rnd += 1
        last_prop = author
    new = []
    for addr, _ in cands:
        ci = cmap.get(addr)
        if ci is None:
            d = DEFAULT_DILIGENCE
        else:
            s = submitted.get(addr, 0)
            rate, d = E, s * D // (2 * E)
            if not ci["is"]:
                rate, d = 1, s * D // 2
            elif not ci["was"]:
                rate = E - 1
                if s > 2 * (E - 1):
                    raise ValueError("seal count exceed")
                d = s * D // (2 * (E - 1))
            w = being.get(addr, 0)
            if w > 0:
                mp = 2 * len(latest["validators"]) * w
                if addr == first_proposer and ci["was"]:
                    mp -= 2 * vdiff
                d += proposed.get(addr, 0) * D // mp
            else:
                d += D
            d = (ci["d"] * (10 * E - rate) + d * rate) // 10 // E
        if d > 2 * D:
            raise ValueError("diligence exceeds maximum")
        new.append((addr, d))
    order = sort_candidates([d for _, d in new])
    n = len(order)
    vals, keys = [], []
    for i in range(n):
        idx = order[shuffle(i, n, e_hdr.mix)]
        pk = cands[idx][1]
        if len(pk) == 0:
            continue
        vals.append(idx)
        keys.append(pk)
    return {"candidates": new, "validators": vals, "keys": keys}


# ------------------------------------------------------------ checks
for c in cases("validators", "quorum"):
    i, e = load(c)
    n = int(i["n"])
    f = f_value(n)
    q = int(math.ceil(float(n) - f))
    check("quorum", c, h0x(struct.pack(">d", f)) == e["f_float64_bits"] and str(q) == e["quorum"]
          and str((n - 1) // 3 + 1 if n > 0 else 0) == e["f_plus_one"] and (n == 0 or q == 2 * n // 3 + 1))

for c in cases("validators", "proposer"):
    i, e = load(c)
    addrs = [hx(a) for a in i["validators"]]
    k = calc_proposer(addrs, hx(i["last_proposer"]), int(i["round"]), int(i["policy"]))
    check("proposer", c, (k is None and e["index"] is None) or (k is not None and str(k) == e["index"] and h0x(addrs[k]) == e["address"]))

for c in cases("validators", "epoch_boundary"):
    i, e = load(c)
    n = int(i["number"])
    check("epoch_boundary", c, is_epoch_block(i["config"], n) == e["is_epoch_block"] and str(last_epoch_block(i["config"], n)) == e["last_epoch_block"])

for c in cases("validators", "shuffle"):
    i, e = load(c)
    try:
        got = [str(shuffle(int(x), int(i["count"]), hx(i["seed"]))) for x in i["indices"]]
    except ValueError:
        got = None
    check("shuffle", c, (got is None and e is None) or (e is not None and got == e["shuffled"]))


for c in cases("validators", "validators_at"):
    i, e = load(c)
    ch = Chain(i["chain"])
    try:
        vs, pol = ch.validators_at(int(i["number"]), hx(i["parent_hash"]))
    except ValueError:
        vs = None
    ok = (vs is None and e is None) or (vs is not None and e is not None and str(pol) == e["proposer_policy"]
                                          and [(h0x(a), h0x(k)) for a, k in vs] == [(v["address"], v["bls_public_key"]) for v in e["validators"]])
    check("validators_at", c, ok)

A04_68 = {"a04_epoch_1": [1888750, 1832500, 1898125, 1870000, 1900000],
          "a04_epoch_2": [1899875, 1829250, 1833312, 1863000, 1892500],
          "a04_epoch_3": [1809887, 1746325, 1824980, 1849200, 1903250]}
for c in cases("validators", "next_epoch_info"):
    i, e = load(c)
    ch = Chain(i["chain"])
    cands = [(hx(x["address"]), hx(x["bls_public_key"])) for x in i["candidates"]]
    try:
        got = next_epoch_info(ch, Hdr(hx(i["header"])), cands)
        err = None
    except ValueError as ex:
        got, err = None, ex
    if e is None:
        check("next_epoch_info", c, err is not None, "python computes a value, reference fails")
        continue
    ee = e["epoch_info"]
    if ee is None:
        check("next_epoch_info", c, got is None and err is None)
        continue
    ok = got is not None and [(h0x(a), str(d)) for a, d in got["candidates"]] == [(x["address"], x["diligence"]) for x in ee["candidates"]] \
        and [str(v) for v in got["validators"]] == ee["validators"] and [h0x(k) for k in got["keys"]] == ee["bls_public_keys"]
    if c.name in A04_68:
        ok = ok and [int(x["diligence"]) for x in ee["candidates"]] == A04_68[c.name]
    check("next_epoch_info", c, ok, "" if ok else f"python={got} err={err}")

for c in cases("chain", "config_at"):
    i, e = load(c)
    g = config_at(i["config"], int(i["number"]))
    want = {k: (None if v is None else int(v)) for k, v in e.items()}
    check("config_at", c, g == want, f"{g} != {want}")

for c in cases("timers", "round_timeout"):
    i, e = load(c)
    t, w = round_timeout(int(i["request_timeout"]), int(i["max_request_timeout_seconds"]), int(i["round"]))
    if t < 0 and int(i["max_request_timeout_seconds"]) == 0:
        continue  # uncapped negative base: implementation-specific conversion (A-06 §4.1)
    check("round_timeout", c, str(t) == e["timeout"] and w == e["warning"], f"{t} {w}")

N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
for c in cases("header", "build_proposal_header"):
    i, e = load(c)
    if e is None:
        continue
    ch = Chain(i["chain"])
    sk_hdr = Hdr(hx(i["header"]))
    h = Hdr(hx(e["header"]))
    parent = ch.by_hash[sk_hdr.parent]
    n = sk_hdr.number
    sk = ecdsa.SigningKey.from_string(hx(i["node_key"]), curve=ecdsa.SECP256k1)
    addr = k256(sk.get_verifying_key().to_string())[12:]
    ok = h.coinbase == addr and h.difficulty == 1 and h.nonce == bytes(8) and h.number == n and h.parent == sk_hdr.parent
    ok = ok and h.time == max(parent.time + config_at(ch.cfg, n)["block_period"], int(i["now"]))
    # untouched builder fields
    ok = ok and all(h.f[k] == sk_hdr.f[k] for k in (0, 1, 3, 4, 5, 6, 9, 10)) and h.f[15:] == sk_hdr.f[15:]
    x = h.x
    ok = ok and be_int(x[8]) == int(i["gas_tip"])
    # randao reveal: RFC 6979 (SHA-256), low S, over keccak256(randao_data)
    rd = k256(be_min(8282) + b"\x01" + be_min(n))
    r_, s_ = sk.sign_digest_deterministic(k256(rd), hashfunc=hashlib.sha256, sigencode=ecdsa.util.sigencode_strings_canonize)
    ok = ok and x[1][:64] == r_ + s_ and len(x[1]) == 65
    mix = (be_int(parent.mix) ^ be_int(k256(x[1]))).to_bytes(32, "big")
    ok = ok and h.mix == mix
    # vanity (A-08 §3.6) and previous seals (A-08 §3.8)
    van = sk_hdr.extra_raw
    if len(van) < 32:
        ok = ok and x[0] == van + bytes(32 - len(van))
    if n > 1:
        last = ch.by_num[n - 1]
        ok = ok and be_int(x[2]) == be_int(last.x[5])
        for pi, li, key in ((3, 6, "extra_prepared"), (4, 7, "extra_committed")):
            want = set(sealers(last.x[li][0]))
            merged = want | {int(s["sealer"]) for s in i[key]}
            got = set(sealers(x[pi][0]))
            same_sig = x[pi][1] == last.x[li][1]
            # either the merge (all new indices) or the unmodified seal (aggregation failed)
            ok = ok and ((got == merged and (same_sig == (merged == want))) or (got == want and same_sig))
    elif len(van) < 32:
        ok = ok and x[2] == b"" and x[3] == [] and x[4] == []
    check("build_proposal_header", c, ok)

# verify_headers (A-08 §6.7, WBFT-HDR-120): the verdicts themselves need BLS
# pairings; recomputed here are the batch rules: one verdict per header, every
# header after the first failure is "reject" (ErrUnknownAncestor), and a
# header whose number does not follow the preceding header of the batch
# (its parent in the batch, H11) is not accepted.
for c in cases("header", "verify_headers"):
    i, e = load(c)
    hs = [Hdr(hx(x)) for x in i["headers"]]
    v = e["verdicts"]
    ok = len(v) == len(hs) and all(x in ("accept", "reject", "deferred") for x in v)
    failed = False
    for k, x in enumerate(v if ok else []):
        if failed:
            ok = ok and x == "reject"
        elif k > 0 and hs[k].number != hs[k - 1].number + 1:
            ok = ok and x != "accept"
        failed = failed or x != "accept"
    check("verify_headers", c, ok, str(v))

for h, (n, f) in stats.items():
    print(f"{h:24s} checked={n:3d} mismatches={f}")
for b in bad:
    print("MISMATCH", b)
print(f"total checked={sum(n for n, _ in stats.values())} mismatches={len(bad)}")
sys.exit(1 if bad else 0)
