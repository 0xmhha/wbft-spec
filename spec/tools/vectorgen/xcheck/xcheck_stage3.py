#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The wbft-spec authors
# SPDX-License-Identifier: LGPL-3.0-or-later
"""Independent cross-check of stage-3 vectors (no go-stablenet code involved).

Recomputes expected values of the stage-3 handlers from the pseudocode and
tables of the specification and compares them with the files written by
vectorgen stage 3:

  state_machine/check_message   A-05 §15 check_message (the whole result)
  state_machine/is_justified    A-05 §11.6 is_justified (verdict, and the failing
                                step when the description names one)
  state_machine/rounds,         for every message or backlog step: the `check`
  network/receive_outcome       class from check_message on the state snapshot
                                of the step before and the view decoded from the
                                payload; the proposer of every snapshot from
                                calc_proposer (A-04 §5.1) and the head; the view
                                of every armed timer; the signer of every sent
                                message whose bytes are given (secp256k1
                                recovery, A-03 signing payloads); the sealers of
                                a finalize against the stored PREPAREs/COMMITs
  network/receive_outcome       the dedup key (A-07 WBFT-NET-022), the outcome
                                class of every frame from a model of A-07 §5 and
                                §8 (codes, empty payload, 0x11 unwrapping,
                                engine stopped, known cache), and that relays
                                and own broadcasts go only to connected
                                validators other than the node and the sender
  chain/fork_schedule           B-01 §3 predicates, SNET-CFG-005/006 rule flags,
                                §7 system contracts in force and upgrades, and
                                the EIP-2124 fork ID (CRC32) over the preset
                                genesis hash (for bohoBlock 0: the hash of the
                                chain/genesis vector with that configuration)
  chain/genesis                 keccak256 of the header, header fields of B-02 §2
                                (base fee, gas limit, difficulty, time, number,
                                root), the genesis extra rebuilt from B-01 §11.2
                                (B-02 §3), and the preset hashes of B-01 §11.1

Not recomputed: the state root of a genesis block (needs the system-contract
storage layout of B-02 §4), BLS seals, and the content of the state snapshots
beyond the checks above.

Usage: python3 xcheck_stage3.py [VECTORS_DIR]   (default ../../../vectors)
Requires: pycryptodome, ecdsa, rlp, pyyaml.  Exit code 1 on any mismatch.
"""
import pathlib, re, sys, zlib

import ecdsa
import rlp
import yaml
from Crypto.Hash import keccak

ROOT = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).resolve().parents[3] / "vectors")


def k256(b: bytes) -> bytes:
    return keccak.new(digest_bits=256, data=b).digest()


def hx(s: str) -> bytes:
    return bytes.fromhex(s[2:])


def h0x(b: bytes) -> str:
    return "0x" + b.hex()


def bi(b: bytes) -> int:
    return int.from_bytes(b, "big")


def load(case):
    inp = yaml.safe_load((case / "input.yaml").read_text())
    exp_path = case / "expected.yaml"
    exp = yaml.safe_load(exp_path.read_text()) if exp_path.exists() else None
    meta = yaml.safe_load((case / "meta.yaml").read_text())
    return inp, exp, meta


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


# ------------------------------------------------------------------ secp256k1
P = 2**256 - 2**32 - 977
N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
G = (0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798,
     0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8)


def ec_add(a, b):
    if a is None: return b
    if b is None: return a
    if a[0] == b[0] and (a[1] + b[1]) % P == 0: return None
    if a == b:
        l = 3 * a[0] * a[0] * pow(2 * a[1], -1, P) % P
    else:
        l = (b[1] - a[1]) * pow(b[0] - a[0], -1, P) % P
    x = (l * l - a[0] - b[0]) % P
    return (x, (l * (a[0] - x) - a[1]) % P)


def ec_mul(k, pt):
    r = None
    while k:
        if k & 1: r = ec_add(r, pt)
        pt = ec_add(pt, pt)
        k >>= 1
    return r


def recover(data: bytes, sig: bytes):
    if len(sig) != 65 or sig[64] >= 4: return None
    r, s, v = bi(sig[:32]), bi(sig[32:64]), sig[64]
    if not (1 <= r < N and 1 <= s < N): return None
    x = r + N if v & 2 else r
    y2 = (x ** 3 + 7) % P
    y = pow(y2, (P + 1) // 4, P)
    if y * y % P != y2: return None
    if y & 1 != v & 1: y = P - y
    e = bi(k256(data))
    rinv = pow(r, -1, N)
    Q = ec_add(ec_mul(s * rinv % N, (x, y)), ec_mul((-e * rinv) % N, G))
    return None if Q is None else k256(Q[0].to_bytes(32, "big") + Q[1].to_bytes(32, "big"))[12:]


def addr_of_key(key: bytes) -> bytes:
    vk = ecdsa.SigningKey.from_string(key, curve=ecdsa.SECP256k1).get_verifying_key().to_string()
    return k256(vk)[12:]


def norm_rc_payload(p):
    if len(p[2]) == 2 and p[2][1] == bytes(32):
        p = [p[0], p[1], []]
    return p


def signing_top(code: int, wire: bytes):
    """(signing payload, signature) of a message (A-03 §6)."""
    w = rlp.decode(wire)
    if code in (0x13, 0x14):
        return rlp.encode([code, w[0]]), w[1]
    if code == 0x15:
        return rlp.encode([code, norm_rc_payload(w[0][0])]), w[0][1]
    return rlp.encode([code, w[0][0]]), w[0][1]


def msg_view(code: int, wire: bytes):
    w = rlp.decode(wire)
    p = w[0] if code in (0x13, 0x14) else w[0][0]
    return bi(p[0]), bi(p[1])


# ------------------------------------------------------------------ A-05 pseudocode
PREPREPARE, PREPARE, COMMIT, ROUND_CHANGE = 0x12, 0x13, 0x14, 0x15
SEQUENCE_THRESHOLD, ROUND_THRESHOLD = 1, 10


def check_message(cur, s, prior_round, code, v):
    """A-05 §15 check_message; cur and v are (sequence, round)."""
    if v[0] > cur[0] + SEQUENCE_THRESHOLD: return "TOO_FAR"
    if v[0] > cur[0] and v[1] >= ROUND_THRESHOLD: return "TOO_FAR"
    if v[0] == cur[0] and v[1] > cur[1] + ROUND_THRESHOLD: return "TOO_FAR"
    if code == ROUND_CHANGE:
        if v[0] > cur[0]: return "FUTURE"
        if v < cur: return "OLD"
        return "PROCESS"
    if v > cur: return "FUTURE"
    if v < cur:
        if cur[0] - v[0] == 1 and v[1] == prior_round and s == "AcceptRequest":
            return "EXTRA_SEAL"
        return "OLD"
    if s == "AcceptRequest":
        return "PROCESS" if code == PREPREPARE else "FUTURE"
    if s == "Preprepared":
        return "INVALID" if code == PREPREPARE else ("PROCESS" if code == PREPARE else "FUTURE")
    if s == "Prepared":
        return "EXTRA_SEAL" if code == PREPARE else ("INVALID" if code == PREPREPARE else "PROCESS")
    return "EXTRA_SEAL" if code in (PREPARE, COMMIT) else "INVALID"


def dedup(msgs):
    seen, out = set(), []
    for m in msgs:
        if m["source"] in seen: continue
        seen.add(m["source"])
        out.append(m)
    return out


ZERO = "0x" + "00" * 32


def is_justified(proposal, tv, round_changes, prepares, Q):
    """A-05 §11.6; returns (verdict, step at which it fails or None)."""
    rcs, ps = dedup(round_changes), dedup(prepares)
    if len(rcs) < Q: return False, 2
    for rc in rcs:
        if int(rc["sequence"]) != tv[0] or int(rc["round"]) != tv[1]: return False, 3
    if len(ps) != 0 and len(ps) < Q: return False, 4
    pr = None
    if ps:
        pr = int(ps[0]["round"])
        for p in ps:
            if int(p["round"]) != pr or p["digest"] != proposal: return False, 5
    if pr is None:
        nil = sum(1 for rc in rcs if (rc["prepared_round"] is None or int(rc["prepared_round"]) == 0) and rc["prepared_digest"] == ZERO)
        return (True, None) if nil >= Q else (False, 6)
    le, match = 0, False
    for rc in rcs:
        rpr = None if rc["prepared_round"] is None else int(rc["prepared_round"])
        if rpr is None or rpr <= pr:
            le += 1
            if rpr == pr and rc["prepared_digest"] == proposal: match = True
            if le >= Q and match: return True, None
    return False, 7


# ------------------------------------------------------------------ check_message
for c in cases("state_machine", "check_message"):
    i, e, _ = load(c)
    cur = (int(i["view"]["sequence"]), int(i["view"]["round"]))
    v = (int(i["message_view"]["sequence"]), int(i["message_view"]["round"]))
    got = check_message(cur, i["state"], int(i["prior_round"]), int(i["code"]), v)
    check("check_message", c, got == e["result"], f"python {got}, vector {e['result']}")

# ------------------------------------------------------------------ is_justified
for c in cases("state_machine", "is_justified"):
    i, e, m = load(c)
    tv = (int(i["target_view"]["sequence"]), int(i["target_view"]["round"]))
    ok, step = is_justified(i["proposal"], tv, i["round_changes"], i["prepares"], int(i["quorum"]))
    good = ok == e["justified"]
    mm = re.search(r"fails at step (\d)", m["description"])
    if mm:
        good = good and step == int(mm.group(1))
    check("is_justified", c, good, f"python {ok} (step {step}), vector {e['justified']}")


# ------------------------------------------------------------------ steps
def calc_proposer_rr(addrs, last, rnd):
    """A-04 §5.1 round-robin (policy 0)."""
    if not addrs: return None
    if last == "0x" + "00" * 20:
        seed = rnd
    else:
        off = addrs.index(last) if last in addrs else 0
        seed = off + rnd + 1
    return addrs[seed % len(addrs)]


def head_coinbase(block_rlp: str) -> str:
    return h0x(rlp.decode(hx(block_rlp))[0][2])


def check_steps(handler, c, i, e, network):
    ini = i["initial"]
    addrs = [v["address"] for v in ini["validators"]]
    node = h0x(addr_of_key(hx(ini["node_key"])))
    head = ini["head"]
    basis = [head]   # the head read by the last start_new_round (WBFT-SM-026)
    peers = set(ini.get("peers", []))
    prev = e["start"]
    running = ini.get("engine", "running") == "running"
    syncing = ini.get("synchronising", False)
    known = set()

    # timers armed so far per kind, with a flag "still armed": arming a round
    # timer cancels every armed round, retry and future timer (A-06
    # WBFT-TIMER-010, -013), arming a retry or a future timer cancels the
    # previous one of its kind (WBFT-TIMER-020, -032), a stop cancels all
    # (WBFT-TIMER-018); nothing else cancels a timer (WBFT-TIMER-016, -023)
    armed = {"round": [], "retry": [], "future": []}

    def arm(rec):
        for t in rec["timers"]:
            if t["kind"] == "round":
                for ts in armed.values():
                    for x in ts:
                        x[0] = False
            else:
                for x in armed[t["kind"]]:
                    x[0] = False
            armed[t["kind"]].append([True])

    def check_record(rec, where):
        st = rec["state"]
        if st is None:   # the engine is stopped: nothing happens
            check(handler, c, not rec["sent"] and not rec["timers"] and not rec["new_round"] and not rec["scheduled"], f"{where}: activity while stopped")
            return
        if rec["new_round"]:   # start_new_round ran in this step (it calls notify_new_round)
            basis[0] = head
        want = calc_proposer_rr(addrs, head_coinbase(basis[0]), int(st["view"]["round"]))
        check(handler, c, st["proposer"] == want, f"{where}: proposer {st['proposer']} != {want}")
        for t in rec["timers"]:
            if t["kind"] in ("round", "future"):
                check(handler, c, (t["sequence"], t["round"]) == (st["view"]["sequence"], st["view"]["round"]), f"{where}: {t['kind']} timer {t}")
            else:
                check(handler, c, t["round"] == st["view"]["round"], f"{where}: retry timer {t}")
        for mrec in rec["sent"]:
            if mrec["encoded"] is not None:
                pl, sig = signing_top(int(mrec["code"]), hx(mrec["encoded"]))
                check(handler, c, recover(pl, sig) is not None and h0x(recover(pl, sig)) == node, f"{where}: signer of sent {mrec['type']}")
                check(handler, c, msg_view(int(mrec["code"]), hx(mrec["encoded"])) == (int(mrec["sequence"]), int(mrec["round"])), f"{where}: view of sent")
                known.add(k256(rlp.encode(hx(mrec["encoded"]))))
            if network:
                conn = (set(addrs) & peers) - {node}
                check(handler, c, set(mrec["to"]) <= conn, f"{where}: own broadcast to {mrec['to']}")
        fin = rec["finalized"]
        if fin is not None:
            idx = lambda srcs: sorted(addrs.index(s) for s in srcs)
            check(handler, c, [int(x["sealer"]) for x in fin["prepared_seals"]] == idx(st["prepares"]), f"{where}: prepared sealers")
            check(handler, c, [int(x["sealer"]) for x in fin["committed_seals"]] == idx(st["commits"]), f"{where}: committed sealers")

    if prev is not None:
        check_record(prev, "start")
        arm(prev)
    for k, (s, r) in enumerate(zip(i["steps"], e["steps"])):
        where = f"step {k}"
        kind = s["kind"]
        if kind in ("round_timeout", "retry_timeout", "future_timeout") and "timer" in s:
            tk = {"round_timeout": "round", "retry_timeout": "retry", "future_timeout": "future"}[kind]
            live = armed[tk][int(s["timer"])][0]
            armed[tk][int(s["timer"])][0] = False
            quiet = not r["sent"] and not r["timers"] and not r["new_round"] and not r["scheduled"]
            if not live:
                check(handler, c, quiet, f"{where}: expiry of a cancelled {tk} timer had an effect")
            elif tk == "round":
                check(handler, c, bool(r["new_round"]) or bool(r["sent"]), f"{where}: live round timer without effect")
            elif tk == "retry":
                check(handler, c, any(t["kind"] == "retry" for t in r["timers"]), f"{where}: live retry timer did not re-arm")
            else:
                check(handler, c, len(r["scheduled"]) == 1 and r["scheduled"][0]["kind"] == "backlog" and int(r["scheduled"][0]["code"]) == 0x12, f"{where}: live future timer scheduled {r['scheduled']}")
        if kind == "stop":
            for ts in armed.values():
                for x in ts:
                    x[0] = False
        if s["kind"] == "head":
            head = s["block"]
        if s["kind"] in ("message", "backlog", "frame") and r.get("check") is not None:
            ps = prev["state"]
            cur = (int(ps["view"]["sequence"]), int(ps["view"]["round"]))
            data = hx(s["payload"])
            code = int(s["code"])
            if s["kind"] == "frame" and code == 0x11:
                data = rlp.decode(data, strict=False)
            got = check_message(cur, ps["state"], int(ps["prior"]["round"]), code, msg_view(code, data))
            check(handler, c, got == r["check"], f"{where}: check python {got}, vector {r['check']}")
        if network and s["kind"] == "frame":
            code, pl = int(s["code"]), hx(s["payload"])
            # A-07 §5 / §8 model
            data, key, outcome = None, None, None
            if len(pl) > 10 * 1024 * 1024:   # A-07 WBFT-NET-013, before any other rule
                outcome = "DISCONNECT"
            elif not running:
                outcome = ("DROP_SILENT" if syncing else "DISCONNECT") if 0x11 <= code <= 0x15 else "DROP_SILENT"
            elif not (0x11 <= code <= 0x15):
                outcome = "DROP_SILENT"
            elif len(pl) == 0:
                outcome = "DISCONNECT"
            else:
                data = pl
                if code == 0x11:
                    try:
                        data = rlp.decode(pl, strict=False)   # bytes after the first item are ignored (WBFT-NET-012)
                        if not isinstance(data, bytes): raise ValueError
                    except Exception:
                        outcome, data = "DISCONNECT", None
                if data is not None:
                    key = k256(rlp.encode(data))
                    if key in known:
                        outcome = "DROP_SILENT"
                    else:
                        known.add(key)
                        outcome = "ACCEPT" if r["relay"] else "IGNORE"
            check(handler, c, outcome == r["outcome"], f"{where}: outcome python {outcome}, vector {r['outcome']}")
            check(handler, c, (key is None and r["dedup_key"] is None) or (key is not None and r["dedup_key"] == h0x(key)), f"{where}: dedup key")
            if r["relay_to"] is not None:
                conn = (set(addrs) & peers) - {node, s["peer"]}
                check(handler, c, set(r["relay_to"]) <= conn, f"{where}: relay_to {r['relay_to']}")
        if network and s["kind"] == "backlog" and r["relay_to"] is not None:
            check(handler, c, set(r["relay_to"]) <= (set(addrs) & peers) - {node}, f"{where}: backlog relay_to")
        if network and s["kind"] in ("backlog",) and r["relay"]:
            known.add(k256(rlp.encode(hx(s["payload"]))))
        if network and r.get("relay") and s["kind"] == "message":
            known.add(k256(rlp.encode(hx(s["payload"]))))
        if "state" in r:
            check_record(r, where)
            arm(r)
            prev = r


for c in cases("state_machine", "rounds"):
    i, e, _ = load(c)
    check_steps("rounds", c, i, e, False)

for c in cases("network", "receive_outcome"):
    i, e, _ = load(c)
    check_steps("receive_outcome", c, i, e, True)

# ------------------------------------------------------------------ fork_schedule
PRESET_HASH = {"8282": "f192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b",
               "8283": "2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490"}
PRESET_FORKS = {"8282": {"applepie_block": 0, "boho_block": 0}, "8283": {"applepie_block": 0, "boho_block": 14408500}}
ETH_FORKS = ["homestead", "eip150", "eip155", "eip158", "byzantium", "constantinople", "petersburg", "istanbul", "muir_glacier", "berlin", "london"]
SC = [("gov_validator", "0x0000000000000000000000000000000000001001"), ("native_coin_adapter", "0x0000000000000000000000000000000000001000"),
      ("gov_minter", "0x0000000000000000000000000000000000001003"), ("gov_master_minter", "0x0000000000000000000000000000000000001002"),
      ("gov_council", "0x0000000000000000000000000000000000001004")]


def genesis_hash_for(preset, ov):
    # genesis depends on the configuration only through bohoBlock == 0 (B-02 §4.5)
    boho = ov["boho_block"] if ov["boho_block"] is not None else PRESET_FORKS[preset]["boho_block"]
    if int(boho) == 0 and PRESET_FORKS[preset]["boho_block"] != 0:
        for g in cases("chain", "genesis"):
            gi, ge, _ = load(g)
            go = gi["genesis"]["overrides"]
            if gi["genesis"]["preset"] == preset and go["boho_block"] == "0" and all(go[k] in (None, []) for k in go if k != "boho_block"):
                return hx(ge["hash"])
        return None
    return bytes.fromhex(PRESET_HASH[preset])


for c in cases("chain", "fork_schedule"):
    i, e, _ = load(c)
    preset, ov = i["config"]["preset"], i["config"]["overrides"]
    n, t = int(i["number"]), int(i["time"])
    fk = dict(PRESET_FORKS[preset])
    for k in ("applepie_block", "boho_block"):
        if ov[k] is not None: fk[k] = int(ov[k])
    times = {k: (int(ov[k]) if ov.get(k) is not None else None) for k in ("shanghai_time", "cancun_time")}
    forked = lambda s: s is not None and s <= n
    forked_t = lambda s: s is not None and s <= t
    want = {f: True for f in ETH_FORKS}
    want.update({"dao_fork": False, "arrow_glacier": False, "gray_glacier": False, "applepie": forked(fk["applepie_block"]), "boho": forked(fk["boho_block"]),
                 "shanghai": forked_t(times["shanghai_time"]), "cancun": forked_t(times["cancun_time"]), "prague": False, "verkle": False, "anzeon": True})
    check("fork_schedule", c, all(e["forks"][k] == v for k, v in want.items()) and set(e["forks"]) == set(want), "forks")
    rules = {"is_anzeon": True, "is_applepie": want["applepie"], "is_boho": want["boho"], "is_merge": False,
             "is_shanghai": False, "is_cancun": False, "is_prague": False, "is_verkle": False}
    check("fork_schedule", c, e["rules"] == rules, "rules")
    sc = [{"name": nm, "address": a, "version": ("v2" if nm == "gov_minter" and forked(fk["boho_block"]) else "v1")} for nm, a in SC]
    check("fork_schedule", c, e["system_contracts"] == sc, "system contracts")
    ups = [{"name": nm, "address": a, "version": "v1"} for nm, a in SC] if n == 0 else []   # genesis entry (SNET-CFG-018)
    if fk["boho_block"] == n:
        ups.append({"name": "gov_minter", "address": SC[2][1], "version": "v2"})
    check("fork_schedule", c, e["upgrades_at"] == ups, f"upgrades {e['upgrades_at']} != {ups}")
    gh = genesis_hash_for(preset, ov)
    if gh is None:
        check("fork_schedule", c, False, "no genesis hash for the fork ID")
        continue
    h = zlib.crc32(gh)
    nxt = 0
    for f in sorted({v for v in (fk["applepie_block"], fk["boho_block"]) if v is not None and v > 0}):
        if f <= n:
            h = zlib.crc32(f.to_bytes(8, "big"), h)
        else:
            nxt = f
            break
    if nxt == 0:
        for f in sorted({v for v in times.values() if v is not None and v > 0}):
            if f <= t:
                h = zlib.crc32(f.to_bytes(8, "big"), h)
            else:
                nxt = f
                break
    check("fork_schedule", c, e["fork_id"] == {"hash": "0x%08x" % h, "next": str(nxt)}, f"fork id {e['fork_id']} != {'0x%08x' % h} {nxt}")

# ------------------------------------------------------------------ genesis
INIT = {
    "8282": [("aa5faa65e9cc0f74a85b6fdfb5f6991f5c094697", "aec493af8fa358a1c6f05499f2dd712721ade88c477d21b799d38e9b84582b6fbe4f4adc21e1e454bc37522eb3478b9b")],
    "8283": [("9f06600b2c17108662e3840e76bb27c9468eb73d", "96683524c3b7e224f2146a0dbb87593e3dee21b7d97c1b24ef6ed799c977be40d3dff8e45b7fcdb48f7713095d84dd9c"),
             ("1aa18ec0b3131171b1b1ddba2dffd81410b30a5a", "80bd166ebfdb29553801dc22f5b83534945cc2a6dacf39d422383cb3041c8afab8cd430ffc29e9297ba2e114efae487f"),
             ("e63b413353e1ba4ac99f2bd1892328e2365ec574", "a418ff21040af17cb3f5109fa075c92d771e508e715230413d6548d54114666011b715d2e60dfd4e20661d5327b67d6d"),
             ("20f681210071932dbe6387378adf6f26af029f7d", "a529026635cf95fd84a9633c62bedaf2a5999f2d5542164086176e24b0c2256a3daa7b76344b631c0bd344895b3f44b9"),
             ("5803c14973690550d6ffc2014b7bffd8005f6021", "b2aa67ceb23d96e4de5dca871dfbda6ba122a3b5a55b3a00547fdda906ab8e4892e98de4b36b541088ac8ff6de7d6a35"),
             ("59f6e6add1fbeab316a7b17c9f1966b3655efedb", "88717d8edbabaf65015b656b5a14bc27705bead8a75f81b9bc064b69b7a5e8f5a010d54cdb85b5b3cab16ce5d816062f"),
             ("0a8dd92ce7ce53bbf6aed40228f9029bb3f92702", "86949700dc2722f48cd649af74cff788bf17553d19278592ca7be54929d1646d60efd1ea12f9dabd7d0143d9813bfea8")],
}
PRESET_GEN = {"8282": {"difficulty": 1, "gas_limit": 105_000_000}, "8283": {"difficulty": 0, "gas_limit": 0}}


def be_min(i: int) -> bytes:
    return i.to_bytes((i.bit_length() + 7) // 8, "big")


def initial_extra(preset):
    vals = INIT[preset]
    epoch = [[[bytes.fromhex(a), be_min(1_900_000)] for a, _ in vals], [be_min(i) for i in range(len(vals))], [bytes.fromhex(k) for _, k in vals]]
    return rlp.encode([b"", b"", b"", [], [], b"", [], [], be_min(27_600_000_000_000), epoch])


for c in cases("chain", "genesis"):
    i, e, _ = load(c)
    preset, ov = i["genesis"]["preset"], i["genesis"]["overrides"]
    if e is None:
        check("genesis", c, ov["number"] not in (None, "0"), "only a non-zero number fails")
        continue
    hdr = hx(e["header"])
    h = rlp.decode(hdr)
    check("genesis", c, h0x(k256(hdr)) == e["hash"], "hash")
    check("genesis", c, h0x(h[3]) == e["state_root"] and h0x(h[12]) == e["extra"], "root/extra fields")
    check("genesis", c, h[12] == initial_extra(preset), "extra rebuilt from B-01 §11.2")
    gl = int(ov["gas_limit"]) if ov["gas_limit"] is not None else PRESET_GEN[preset]["gas_limit"]
    check("genesis", c, bi(h[9]) == (gl if gl != 0 else 4_712_388), "gas limit")
    df = int(ov["difficulty"]) if ov["difficulty"] is not None else PRESET_GEN[preset]["difficulty"]
    check("genesis", c, bi(h[7]) == df and bi(h[8]) == 0, "difficulty/number")
    check("genesis", c, bi(h[11]) == (int(ov["timestamp"]) if ov["timestamp"] is not None else 0), "time")
    check("genesis", c, bi(h[15]) == 20_000_000_000_000, "base fee")
    if all(v in (None, []) for k, v in ov.items() if k != "extra_data"):
        check("genesis", c, e["hash"][2:] == PRESET_HASH[preset], "preset hash constant")

for h, (n, f) in stats.items():
    print(f"{h:24s} checked={n:4d} mismatches={f}")
for b in bad:
    print("MISMATCH", b)
print(f"total checked={sum(n for n, _ in stats.values())} mismatches={len(bad)}")
sys.exit(1 if bad else 0)
