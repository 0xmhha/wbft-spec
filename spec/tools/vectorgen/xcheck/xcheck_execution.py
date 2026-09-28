#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The wbft-spec authors
# SPDX-License-Identifier: LGPL-3.0-or-later
"""Independent cross-check of the runners `execution` and `source` (no
go-stablenet code involved).

  execution/p256_verify         B-07 SNET-TX-089 recomputed: length rule, point
                                and range checks, ECDSA verification over NIST
                                P-256 (python-ecdsa, no low-S rule), 6 900 gas
                                from BohoBlock on, no precompile (0 gas) before
  execution/gas_tip_enforcement B-07 §3, §5, §7, §8.2: every transaction is
  execution/fee_delegation      decoded (pyrlp) and its sender and fee payer
                                recovered (secp256k1 over the signing hashes of
                                B-07 §4.2); per receipt the gas price
                                min(tip + base fee, fee cap) with the governance
                                tip unless the sender is authorized, the
                                AuthorizedTxExecuted log, and the balances after
                                the block from the flows of B-07 §7.1 (payer,
                                value, coinbase tip, no base-fee credit); for a
                                fail case, that the model finds a
                                transaction-invalidating rule
  execution/process_finalize    B-06 §3 base-fee distribution: the shares from
                                the EpochInfo that governs the block (A-04
                                §3.1) and the remainder to the coinbase, checked
                                against the balances; the EpochInfo presence rule
                                of a non-epoch block (SNET-FIN-016); the gas-tip
                                comparison (SNET-FIN-018)
  source/candidates_at_epoch    B-04 §8.2 readers over the storage of the input:
                                validator_list (slot 0x33), bls_public_key
                                (mapping 0x37, Solidity bytes), gas_tip (0x39)
  governance/scenarios          the consensus projection of every step from a
                                model of B-05 §8 and §10 driven by the call data
                                and the logs (validator list with swap-and-pop,
                                keys, gas tip, blacklist and authorized sets),
                                and a B-05 custom error for every revert

Not recomputed: state roots (they need the account trie), the EpochInfo of an
epoch block (the diligence needs the seal history of A-04 §6), the gas used
by a transaction (taken from the receipt), and the logs and write sets of the
governance steps (they need the GovBase semantics of B-05 §5 and the storage
layout of B-04).

Usage: python3 xcheck_execution.py [VECTORS_DIR]
Requires: pycryptodome, ecdsa, rlp, pyyaml.  Exit code 1 on any mismatch.
"""
import pathlib, sys

import ecdsa
import rlp
import yaml
from Crypto.Hash import keccak

ROOT = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).resolve().parents[3] / "vectors")


def k256(b):
    return keccak.new(digest_bits=256, data=b).digest()


def hx(s):
    return bytes.fromhex(s[2:])


def bi(b):
    return int.from_bytes(b, "big")


def load(case):
    i = yaml.safe_load((case / "input.yaml").read_text())
    p = case / "expected.yaml"
    return i, (yaml.safe_load(p.read_text()) if p.exists() else None)


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


PRESET = {"8282": {"chain_id": 8282, "applepie": 0, "boho": 0}, "8283": {"chain_id": 8283, "applepie": 0, "boho": 14408500}}
GV0 = bytes.fromhex("0000000000000000000000000000000000001001")
ACCOUNT_MANAGER = bytes.fromhex("0000000000000000000000000000000000b00003")
AUTH_TOPIC = bytes.fromhex("40e728a89c7f5b192cf1c1b747fb64d51d81c7a2b3ed4607b94d3a1e6a3e0373")
BLACKLISTED, AUTHORIZED = 1 << 63, 1 << 62


def cfg_of(c):
    p = dict(PRESET[c["preset"]])
    ov = c.get("overrides", {})
    if ov.get("applepie_block") is not None:
        p["applepie"] = int(ov["applepie_block"])
    if ov.get("boho_block") is not None:
        p["boho"] = int(ov["boho_block"])
    return p


# ------------------------------------------------------------------ P-256
P256 = ecdsa.NIST256p
P256P, P256N, P256A, P256B = P256.curve.p(), P256.order, P256.curve.a(), P256.curve.b()
P256G = (P256.generator.x(), P256.generator.y())


def p_add(a, b):
    if a is None: return b
    if b is None: return a
    if a[0] == b[0] and (a[1] + b[1]) % P256P == 0: return None
    if a == b:
        l = (3 * a[0] * a[0] + P256A) * pow(2 * a[1], -1, P256P) % P256P
    else:
        l = (b[1] - a[1]) * pow(b[0] - a[0], -1, P256P) % P256P
    x = (l * l - a[0] - b[0]) % P256P
    return (x, (l * (a[0] - x) - a[1]) % P256P)


def p_mul(k, pt):
    r = None
    while k:
        if k & 1: r = p_add(r, pt)
        pt = p_add(pt, pt)
        k >>= 1
    return r


def p256_verify(inp):
    """EIP-7951 / B-07 SNET-TX-089: output 0x..01 or empty."""
    if len(inp) != 160:
        return b""
    h, r, s, x, y = (bi(inp[i:i + 32]) for i in range(0, 160, 32))
    if not (0 < r < P256N and 0 < s < P256N) or x >= P256P or y >= P256P:
        return b""
    if (y * y - (x ** 3 + P256A * x + P256B)) % P256P != 0:
        return b""   # not on the curve (the point at infinity has no affine form)
    w = pow(s, -1, P256N)
    X = p_add(p_mul(h * w % P256N, P256G), p_mul(r * w % P256N, (x, y)))
    ok = X is not None and X[0] % P256N == r
    return (1).to_bytes(32, "big") if ok else b""


for c in cases("execution", "p256_verify"):
    i, e = load(c)
    cfg = cfg_of(i["config"])
    active = int(i["number"]) >= cfg["boho"]
    out, gas = (p256_verify(hx(i["input"])), 6900) if active else (b"", 0)
    check("p256_verify", c, "0x" + out.hex() == e["output"] and str(gas) == e["gas"], f"{out.hex()} {gas}")

# ------------------------------------------------------------ secp256k1
SP = 2**256 - 2**32 - 977
SN = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
SG = (0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798,
      0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8)


def ec_add(a, b):
    if a is None: return b
    if b is None: return a
    if a[0] == b[0] and (a[1] + b[1]) % SP == 0: return None
    if a == b:
        l = 3 * a[0] * a[0] * pow(2 * a[1], -1, SP) % SP
    else:
        l = (b[1] - a[1]) * pow(b[0] - a[0], -1, SP) % SP
    x = (l * l - a[0] - b[0]) % SP
    return (x, (l * (a[0] - x) - a[1]) % SP)


def ec_mul(k, pt):
    r = None
    while k:
        if k & 1: r = ec_add(r, pt)
        pt = ec_add(pt, pt)
        k >>= 1
    return r


def recover_digest(digest, r, s, v):
    """secp256k1 recovery with the homestead rules (low S); None on failure."""
    if v not in (0, 1) or not (1 <= r < SN and 1 <= s <= SN // 2):
        return None
    y2 = (r ** 3 + 7) % SP
    y = pow(y2, (SP + 1) // 4, SP)
    if y * y % SP != y2: return None
    if y & 1 != v: y = SP - y
    e = bi(digest)
    rinv = pow(r, -1, SN)
    Q = ec_add(ec_mul(s * rinv % SN, (r, y)), ec_mul((-e * rinv) % SN, SG))
    return None if Q is None else k256(Q[0].to_bytes(32, "big") + Q[1].to_bytes(32, "big"))[12:]


def decode_tx(raw, chain_id):
    """Return a dict with the fields the model needs, or raise ValueError."""
    if raw[0] >= 0xc0:   # legacy
        f = rlp.decode(raw)
        nonce, price, gas, to, value, data, v, r, s = f
        v = bi(v)
        cid = (v - 35) // 2
        payload = rlp.encode([nonce, price, gas, to, value, data, cid, 0, 0])
        sender = recover_digest(k256(payload), bi(r), bi(s), (v - 35) % 2)
        return dict(type=0, sender=sender, to=to, value=bi(value), gas=bi(gas), tip=bi(price), fee_cap=bi(price), payer=None, payer_ok=True)
    t, f = raw[0], rlp.decode(raw[1:])
    if t == 0x02:
        cid, nonce, tip, cap, gas, to, value, data, al, v, r, s = f
        sender = recover_digest(k256(b"\x02" + rlp.encode(f[:9])), bi(r), bi(s), bi(v))
        return dict(type=2, sender=sender, to=to, value=bi(value), gas=bi(gas), tip=bi(tip), fee_cap=bi(cap), payer=None, payer_ok=True)
    if t == 0x16:
        inner, payer, fv, fr, fs = f
        cid, nonce, tip, cap, gas, to, value, data, al, v, r, s = inner
        sender = recover_digest(k256(b"\x02" + rlp.encode(inner[:9])), bi(r), bi(s), bi(v))
        if payer == b"":
            return dict(type=0x16, sender=sender, to=to, value=bi(value), gas=bi(gas), tip=bi(tip), fee_cap=bi(cap), payer=None, payer_ok=False)
        sighash = k256(b"\x16" + rlp.encode([[chain_id] + inner[1:], payer]))
        rec = recover_digest(sighash, bi(fr), bi(fs), bi(fv))
        return dict(type=0x16, sender=sender, to=to, value=bi(value), gas=bi(gas), tip=bi(tip), fee_cap=bi(cap), payer=payer, payer_ok=(rec == payer))
    raise ValueError(f"type {t:#x}")


def header_fields(raw):
    h = rlp.decode(raw)
    x = rlp.decode(h[12])
    return dict(coinbase=h[2], number=bi(h[8]), base_fee=bi(h[15]), gas_tip=bi(x[8]))


def tx_model(handler, c, i, e):
    cfg = cfg_of(i["config"])
    hd = header_fields(hx(i["header"]))
    base, T = hd["base_fee"], hd["gas_tip"]
    bal = {hx(a["address"]): int(a["balance"]) for a in i["accounts"]}
    extra = {hx(a["address"]): int(a["extra"]) for a in i["accounts"]}
    invalid = None
    for k, raw in enumerate(i["transactions"]):
        tx = decode_tx(hx(raw), cfg["chain_id"])
        s = tx["sender"]
        auth = extra.get(s, 0) & AUTHORIZED != 0
        tip = tx["tip"] if auth else T
        if tx["type"] == 0x16 and not tx["payer_ok"]:
            invalid = "fee payer"; break
        delegated = tx["payer"] is not None and tx["payer"] != s
        if delegated and hd["number"] < cfg["applepie"]:
            invalid = "delegation before Applepie"; break
        if tx["fee_cap"] < tip:
            invalid = "tip above fee cap"; break
        if tx["fee_cap"] < base:
            invalid = "fee cap below base fee"; break
        gp = min(tip + base, tx["fee_cap"])
        payer = tx["payer"] if tx["payer"] is not None else s
        if delegated:
            if bal.get(payer, 0) < tx["gas"] * gp or bal.get(s, 0) < tx["value"]:
                invalid = "insufficient funds"; break
        elif bal.get(s, 0) < tx["gas"] * tx["fee_cap"] + tx["value"]:
            invalid = "insufficient funds"; break
        if tx["payer"] is not None and extra.get(tx["payer"], 0) & BLACKLISTED:
            invalid = "blacklisted fee payer"; break
        if e is None:
            continue
        rc = e["receipts"][k]
        used = int(rc["gas_used"])
        check(handler, c, int(rc["effective_gas_price"]) == gp, f"tx {k}: gas price {rc['effective_gas_price']} != {gp}")
        last = rc["logs"][-1] if rc["logs"] else None
        has_auth = last is not None and hx(last["address"]) == ACCOUNT_MANAGER and [hx(t) for t in last["topics"]] == [AUTH_TOPIC]
        check(handler, c, has_auth == auth, f"tx {k}: AuthorizedTxExecuted log {has_auth}, authorized {auth}")
        bal[payer] = bal.get(payer, 0) - used * gp
        bal[s] = bal.get(s, 0) - tx["value"]
        to = tx["to"]
        bal[to] = bal.get(to, 0) + tx["value"]
        bal[hd["coinbase"]] = bal.get(hd["coinbase"], 0) + used * (gp - base)
    if e is None:
        check(handler, c, invalid is not None, "reference fails, the model finds every transaction valid")
        return
    check(handler, c, invalid is None, f"reference succeeds, the model finds: {invalid}")
    for a in e["accounts"]:
        check(handler, c, int(a["balance"]) == bal.get(hx(a["address"]), 0), f"balance of {a['address']}: {a['balance']} != {bal.get(hx(a['address']), 0)}")


for h in ("gas_tip_enforcement", "fee_delegation"):
    for c in cases("execution", h):
        i, e = load(c)
        tx_model(h, c, i, e)


# ---------------------------------------------------------------- storage
def sload(storage, key):
    return bi(storage.get(key, bytes(32)))


def u256(x):
    return x.to_bytes(32, "big")


def read_bytes(storage, slot):
    """Solidity `bytes` at `slot`: short form (len < 32) in the slot itself,
    long form (2·len + 1 in the slot, data from keccak(slot))."""
    w = storage.get(u256(slot), bytes(32))
    if w[31] & 1 == 0:
        return w[:w[31] // 2]
    n = (bi(w) - 1) // 2
    base = bi(k256(u256(slot)))
    data = b"".join(storage.get(u256(base + j), bytes(32)) for j in range((n + 31) // 32))
    return data[:n]


def reader(storage):
    n = sload(storage, u256(0x33))
    base = bi(k256(u256(0x33)))
    out = []
    for j in range(n):
        addr = storage.get(u256(base + j), bytes(32))[12:]
        key = read_bytes(storage, bi(k256(bytes(12) + addr + u256(0x37))))
        out.append((addr, key))
    return out, sload(storage, u256(0x39))


for c in cases("source", "candidates_at_epoch"):
    i, e = load(c)
    st = {}
    for a in i["accounts"]:
        if hx(a["address"]) == GV0:
            st = {hx(s["key"]): hx(s["value"]) for s in a["storage"]}
    got, tip = reader(st)
    want = [(hx(x["address"]), hx(x["bls_public_key"])) for x in e["candidates"]]
    check("candidates_at_epoch", c, got == want and str(tip) == e["gas_tip"], f"{len(got)} candidates, tip {tip}")


# ---------------------------------------------------------- process_finalize
def extra_of(raw_header):
    h = rlp.decode(raw_header)
    return h, rlp.decode(h[12])


def epoch_info_at(chain, n):
    """EpochInfo governing block n (A-04 §3.1): the canonical epoch header at
    or below n - 1 of the fixture's epoch length (no transitions here)."""
    E = int(chain["config"]["wbft"]["epoch_length"])
    last = ((n - 1) // E) * E
    raw = hx(chain["genesis"]) if last == 0 else next(hx(x) for x in chain["headers"] if bi(rlp.decode(hx(x))[8]) == last)
    return extra_of(raw)[1][9]


for c in cases("execution", "process_finalize"):
    i, e = load(c)
    h, x = extra_of(hx(i["header"]))
    n, gas_used, base_fee, coinbase = bi(h[8]), bi(h[10]), bi(h[15]), h[2]
    E = int(i["chain"]["config"]["wbft"]["epoch_length"])
    tip_ok = i["parent_gas_tip"] is not None and int(i["parent_gas_tip"]) == bi(x[8])
    epoch_ok = n % E == 0 or x[9] in (b"", [])
    if e is None:
        check("process_finalize", c, not tip_ok or not epoch_ok or n % E == 0, "reference fails, the model accepts a non-epoch block")
        continue
    check("process_finalize", c, tip_ok and epoch_ok, "reference accepts a gas-tip or EpochInfo mismatch")
    if n % E == 0 or gas_used == 0:
        want = {hx(a["address"]): int(a["balance"]) for a in i["accounts"]}
    else:
        ei = epoch_info_at(i["chain"], n)
        cands, vals = ei[0], [bi(v) for v in ei[1]]
        dsum = sum(bi(cands[v][1]) for v in vals)
        total = gas_used * base_fee
        want = {hx(a["address"]): int(a["balance"]) for a in i["accounts"]}
        dust = total
        for v in vals:
            share = total * bi(cands[v][1]) // dsum if dsum else 0
            if share:
                want[cands[v][0]] = want.get(cands[v][0], 0) + share
                dust -= share
        if dust:
            want[coinbase] = want.get(coinbase, 0) + dust
    for a in e["accounts"]:
        check("process_finalize", c, int(a["balance"]) == want.get(hx(a["address"]), 0), f"balance of {a['address']}")

# --------------------------------------------------------- governance/scenarios
# A model of the consensus projection (A-11 §3.1 "Governance scenarios") driven by the call
# data of successful steps and their logs: the validator list of B-05 §8
# (configureValidator cases 1 to 3 with swap-and-pop, the removal hook of
# MemberRemoved, the operator move of MemberChanged), the gas tip of
# GasTipUpdated, and the flag sets of AddressBlacklisted and
# AuthorizedAccountAdded. Every revert is checked to carry a custom error of
# B-05 (a 4-byte selector of a known error).
def sel(sig):
    return k256(sig.encode())[:4]


def topic(sig):
    return k256(sig.encode())


GOV_ERRORS = {sel(e + "()") for e in (
    "AlreadyAMember AlreadyApproved DuplicateMember IndexOutOfBounds InsufficientApprovals InvalidMemberAddress "
    "InvalidMaxProposals InvalidProposal InvalidProposalExpiry InvalidMemberVersion InvalidQuorum MemberIndexOverflow "
    "NotAMember NotProposer ProposalAlreadyInVoting ProposalNotExecutable ProposalNotInVoting ReentrantCall "
    "TooManyActiveProposals TooManyExecutionAttempts InvalidValidator AlreadyValidatorExists NoConfigurationChanging "
    "InvalidBlsKeyLength InvalidSignatureLength AlreadyRegisteredBlsKey FailedToVerifyBlsKey InvalidBlsKey InvalidGasTip "
    "SameGasTip AlreadyInBlacklist NotInBlacklist AlreadyInAuthorizedAccountList NotInAuthorizedAccountList").split()}
T_GAS_TIP = topic("GasTipUpdated(uint256,uint256,address)")
T_REMOVED = topic("MemberRemoved(address,uint256,uint32)")
T_CHANGED = topic("MemberChanged(address,address)")
T_BLACK = topic("AddressBlacklisted(address,uint256)")
T_AUTH = topic("AuthorizedAccountAdded(address,uint256)")
S_CONFIGURE = sel("configureValidator(address,bytes,bytes)")

for c in cases("governance", "scenarios"):
    i, e = load(c)
    gv = i["genesis"]["gov_validator"]
    vals = [hx(v) for v in gv["validators"]]
    keys = {hx(v): hx(k) for v, k in zip(gv["validators"], gv["bls_public_keys"])}
    op2val = {hx(m): hx(v) for m, v in zip(gv["members"], gv["validators"])}
    tip = int(gv["gas_tip"])
    black, auth = set(), set()
    for k, (s, r) in enumerate(zip(i["steps"], e["steps"])):
        where = f"step {k}"
        if r["status"] != "success":
            check("scenarios", c, hx(r["revert_data"])[:4] in GOV_ERRORS and not r["logs"] and not r["write_set"], f"{where}: revert without a B-05 error")
        else:
            data = hx(s["data"])
            if data[:4] == S_CONFIGURE and hx(s["to"]) == GV0:
                args = data[4:]
                new = args[12:32]
                off = bi(args[32:64])
                ln = bi(args[off:off + 32])
                key = args[off + 32:off + 32 + ln]
                old = op2val.get(hx(s["sender"]))
                if old is None:
                    vals.append(new)
                elif old != new:
                    j = vals.index(old)
                    vals[j] = vals[-1]
                    vals.pop()
                    del keys[old]
                    vals.append(new)
                keys[new] = key
                op2val[hx(s["sender"])] = new
            for l in r["logs"]:
                t0 = hx(l["topics"][0])
                if t0 == T_GAS_TIP:
                    tip = bi(hx(l["data"])[32:64])
                elif t0 == T_REMOVED and hx(l["address"]) == GV0:
                    m = hx(l["topics"][1])[12:]
                    v = op2val.pop(m, None)
                    if v is not None:
                        j = vals.index(v)
                        vals[j] = vals[-1]
                        vals.pop()
                        del keys[v]
                elif t0 == T_CHANGED and hx(l["address"]) == GV0:
                    old, new = hx(l["topics"][1])[12:], hx(l["topics"][2])[12:]
                    if old in op2val:
                        op2val[new] = op2val.pop(old)
                elif t0 == T_BLACK:
                    black.add(hx(l["topics"][1])[12:])
                elif t0 == T_AUTH:
                    auth.add(hx(l["topics"][1])[12:])
        pj = r["projection"]
        ok = [hx(v) for v in pj["validators"]] == vals and [hx(x) for x in pj["bls_public_keys"]] == [keys[v] for v in vals] \
            and int(pj["gas_tip"]) == tip and set(hx(x) for x in pj["blacklisted"]) == black and set(hx(x) for x in pj["authorized"]) == auth
        check("scenarios", c, ok, f"{where}: projection differs from the model")

for h, (n, f) in stats.items():
    print(f"{h:24s} checked={n:4d} mismatches={f}")
for b in bad:
    print("MISMATCH", b)
print(f"total checked={sum(n for n, _ in stats.values())} mismatches={len(bad)}")
sys.exit(1 if bad else 0)
