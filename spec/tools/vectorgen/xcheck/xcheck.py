#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The wbft-spec authors
# SPDX-License-Identifier: LGPL-3.0-or-later
"""Independent cross-check of stage-1 vectors (no go-stablenet code involved).

Recomputes expected values with other implementations and compares them with
the files written by vectorgen:

  keccak256, randao_data, randao_mix, dedup_key      pycryptodome Keccak-256
  ecdsa_sign                                         python-ecdsa (RFC 6979, low S)
  ecdsa_recover, signing_payload signers             secp256k1 recovery in plain Python
  bls_derive                                         HKDF (hashlib/hmac) + G1 scalar
                                                     multiplication in plain Python
  aggregate_public_keys                              G1 decompression (flags, x < p,
                                                     on curve, r-subgroup), point sum
  block_hash, hash_with_round, filtered_header,      pyrlp + Keccak
  seal_data, signing_payload payloads

Not covered: BLS signing, verification and signature aggregation (G2,
hash-to-curve, pairing), and the reject cases of extra_codec/message_codec.

Usage: python3 xcheck.py [VECTORS_DIR]   (default ../../../vectors)
Requires: pycryptodome, ecdsa, rlp, pyyaml.  Exit code 1 on any mismatch.
"""
import hashlib, hmac, pathlib, sys

import ecdsa
import rlp
import yaml
from Crypto.Hash import keccak

ROOT = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).resolve().parents[3] / "vectors")


def k256(b: bytes) -> bytes:
    return keccak.new(digest_bits=256, data=b).digest()


def hx(s: str) -> bytes:
    return bytes.fromhex(s[2:])


def load(case: pathlib.Path):
    inp = yaml.safe_load((case / "input.yaml").read_text())
    exp_path = case / "expected.yaml"
    exp = yaml.safe_load(exp_path.read_text()) if exp_path.exists() else None
    return inp, exp


def cases(runner, handler):
    d = ROOT / runner / handler
    return sorted(p for p in d.iterdir() if p.is_dir()) if d.exists() else []


# ------------------------------------------------------------------ secp256k1
P = 2**256 - 2**32 - 977
N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
G = (0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798,
     0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8)


def ec_add(a, b, p=P):
    if a is None: return b
    if b is None: return a
    if a[0] == b[0] and (a[1] + b[1]) % p == 0: return None
    if a == b:
        l = 3 * a[0] * a[0] * pow(2 * a[1], -1, p) % p
    else:
        l = (b[1] - a[1]) * pow(b[0] - a[0], -1, p) % p
    x = (l * l - a[0] - b[0]) % p
    return (x, (l * (a[0] - x) - a[1]) % p)


def ec_mul(k, pt, add=ec_add):
    r = None
    while k:
        if k & 1: r = add(r, pt)
        pt = add(pt, pt)
        k >>= 1
    return r


def addr_of(Q) -> bytes:
    return k256(Q[0].to_bytes(32, "big") + Q[1].to_bytes(32, "big"))[12:]


def recover(data: bytes, sig: bytes):
    """Returns the address or None (cgo rules of A-02 WBFT-CRYPTO-013)."""
    if len(sig) != 65 or sig[64] >= 4: return None
    r, s, v = int.from_bytes(sig[:32], "big"), int.from_bytes(sig[32:64], "big"), sig[64]
    if not (1 <= r < N and 1 <= s < N): return None
    x = r + N if v & 2 else r
    if x >= P: return None
    y2 = (x ** 3 + 7) % P
    y = pow(y2, (P + 1) // 4, P)
    if y * y % P != y2: return None
    if y & 1 != v & 1: y = P - y
    e = int.from_bytes(k256(data), "big")
    rinv = pow(r, -1, N)
    Q = ec_add(ec_mul(s * rinv % N, (x, y)), ec_mul((-e * rinv) % N, G))
    return None if Q is None else addr_of(Q)


# ------------------------------------------------------------------ BLS12-381 G1
BP = 0x1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab
BR = 0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001
G1 = (0x17f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb,
      0x08b3f481e3aaa0f1a09e30ed741d8ae4fcf5e095d5d00af600db18cb2c04b3edd03cc744a2888ae40caa232946c5e7e1)
g1add = lambda a, b: ec_add(a, b, BP)


def g1_compress(Q) -> bytes:
    if Q is None: return bytes([0xc0]) + bytes(47)
    b = bytearray(Q[0].to_bytes(48, "big"))
    b[0] |= 0x80
    if Q[1] > BP - Q[1]: b[0] |= 0x20
    return bytes(b)


def g1_decompress(b: bytes):
    """Returns (ok, point). Rejections of A-02 WBFT-CRYPTO-023/056."""
    if len(b) != 48 or not b[0] & 0x80: return False, None
    if b[0] & 0x40:
        if b[0] & 0x3f or any(b[1:]): return False, None
        return True, None  # infinity (then rejected as a key)
    x = int.from_bytes(bytes([b[0] & 0x1f]) + b[1:], "big")
    if x >= BP: return False, None
    y2 = (x ** 3 + 4) % BP
    y = pow(y2, (BP + 1) // 4, BP)
    if y * y % BP != y2: return False, None
    if (y > BP - y) != bool(b[0] & 0x20): y = BP - y
    return True, (x, y)


def g1_key(b: bytes):
    ok, Q = g1_decompress(b)
    if not ok or Q is None: return None
    if ec_mul(BR, Q, g1add) is not None: return None  # not in the r-subgroup
    return Q


def bls_keygen(ikm: bytes) -> int:
    salt = b"BLS-SIG-KEYGEN-SALT-"
    while True:
        salt = hashlib.sha256(salt).digest()
        prk = hmac.new(salt, ikm + b"\x00", hashlib.sha256).digest()
        okm, t, i = b"", b"", 1
        while len(okm) < 48:
            t = hmac.new(prk, t + b"\x00\x30" + bytes([i]), hashlib.sha256).digest()
            okm += t
            i += 1
        sk = int.from_bytes(okm[:48], "big") % BR
        if sk: return sk


# ------------------------------------------------------------------ RLP helpers
def be_min(i: int) -> bytes:
    return i.to_bytes((i.bit_length() + 7) // 8, "big")


def filtered(header: bytes, rnd: int):
    h = rlp.decode(header)
    try:
        e = rlp.decode(h[12])
        if not isinstance(e, list) or len(e) != 10: return None
    except Exception:
        return None
    e[5], e[6], e[7] = be_min(rnd), [], []
    h[12] = rlp.encode(e)
    return rlp.encode(h)


def hash_with_round(header: bytes, rnd: int) -> bytes:
    f = filtered(header, rnd)
    return k256(b"\xc0") if f is None else k256(f)


def block_hash(header: bytes) -> bytes:
    h = rlp.decode(header)
    if int.from_bytes(h[7], "big") == 1 and filtered(header, 0) is not None:
        return k256(filtered(header, 0))
    return k256(header)


def norm_rc_payload(p):
    # prepared = [pr, 0x00..00] re-encodes as [] (A-03 WBFT-ENC-090, WBFT-MSG-030)
    if len(p[2]) == 2 and p[2][1] == bytes(32):
        p = [p[0], p[1], []]
    return p


def signing(code: int, wire: bytes):
    w = rlp.decode(wire)
    if code in (0x13, 0x14):
        top = (rlp.encode([code, w[0]]), w[1])
        emb = []
    elif code == 0x15:
        top = (rlp.encode([code, norm_rc_payload(w[0][0])]), w[0][1])
        emb = [(rlp.encode([0x13, j[0]]), j[1]) for j in (w[2] if isinstance(w[2], list) else [])]
    else:
        top = (rlp.encode([code, w[0][0]]), w[0][1])
        emb = [(rlp.encode([0x15, norm_rc_payload(j[0])]), j[1]) for j in w[1][0]]
        emb += [(rlp.encode([0x13, j[0]]), j[1]) for j in w[1][1]]
    return top, emb


# ------------------------------------------------------------------ checks
stats, bad = {}, []


def check(handler, case, cond, detail=""):
    s = stats.setdefault(handler, [0, 0])
    s[0] += 1
    if not cond:
        s[1] += 1
        bad.append(f"{handler}/{case.name} {detail}")


for c in cases("crypto", "keccak256"):
    i, e = load(c)
    check("keccak256", c, "0x" + k256(hx(i["data"])).hex() == e["hash"])

for c in cases("crypto", "ecdsa_sign"):
    i, e = load(c)
    sk = ecdsa.SigningKey.from_string(hx(i["private_key"]), curve=ecdsa.SECP256k1)
    digest = k256(hx(i["data"]))
    rs = sk.sign_digest_deterministic(digest, hashfunc=hashlib.sha256, sigencode=ecdsa.util.sigencode_strings_canonize)
    rs = rs[0] + rs[1]
    vk = sk.get_verifying_key().to_string()
    addr = k256(vk)[12:]
    v = next(v for v in (0, 1) if recover(hx(i["data"]), rs + bytes([v])) == addr)
    check("ecdsa_sign", c, "0x" + (rs + bytes([v])).hex() == e["signature"] and "0x" + addr.hex() == e["address"])

for c in cases("crypto", "ecdsa_recover"):
    i, e = load(c)
    a = recover(hx(i["data"]), hx(i["signature"]))
    check("ecdsa_recover", c, (a is None and e is None) or (a is not None and e is not None and "0x" + a.hex() == e["address"]), f"got {a}")

for c in cases("crypto", "bls_derive"):
    i, e = load(c)
    sk = bls_keygen(hx(i["private_key"]))
    pk = g1_compress(ec_mul(sk, G1, g1add))
    check("bls_derive", c, "0x%064x" % sk == e["secret_key"] and "0x" + pk.hex() == e["public_key"])

for c in cases("crypto", "aggregate_public_keys"):
    i, e = load(c)
    keys = [g1_key(hx(k)) for k in i["public_keys"]]
    if not keys or any(k is None for k in keys):
        check("aggregate_public_keys", c, e is None, "python rejects, reference accepts")
        continue
    acc = None
    for k in keys:
        acc = g1add(acc, k)
    check("aggregate_public_keys", c, e is not None and "0x" + g1_compress(acc).hex() == e["public_key"])

for c in cases("crypto", "randao_data"):
    i, e = load(c)
    pre = be_min(int(i["chain_id"])) + b"\x01" + be_min(int(i["number"]))
    check("randao_data", c, "0x" + k256(pre).hex() == e["randao_data"])

for c in cases("crypto", "randao_mix"):
    i, e = load(c)
    mix = bytes(a ^ b for a, b in zip(hx(i["parent_mix"]), k256(hx(i["reveal"]))))
    check("randao_mix", c, "0x" + mix.hex() == e["mix"])

for c in cases("crypto", "seal_data"):
    i, e = load(c)
    h = hash_with_round(hx(i["header"]), int(i["round"]))
    check("seal_data", c, "0x" + k256(h + bytes([int(i["seal_type"])])).hex() == e["seal_data"])

for c in cases("encoding", "block_hash"):
    i, e = load(c)
    check("block_hash", c, "0x" + block_hash(hx(i["header"])).hex() == e["hash"])

for c in cases("encoding", "hash_with_round"):
    i, e = load(c)
    check("hash_with_round", c, "0x" + hash_with_round(hx(i["header"]), int(i["round"])).hex() == e["hash"])

for c in cases("encoding", "filtered_header"):
    i, e = load(c)
    f = filtered(hx(i["header"]), int(i["round"]))
    check("filtered_header", c, (f is None and e is None) or (f is not None and e is not None and "0x" + f.hex() == e["header"]))

for c in cases("encoding", "dedup_key"):
    i, e = load(c)
    check("dedup_key", c, "0x" + k256(rlp.encode(hx(i["payload"]))).hex() == e["key"])

for c in cases("encoding", "signing_payload"):
    i, e = load(c)
    if e is None:
        (p, s), _ = signing(int(i["code"]), hx(i["payload"]))
        check("signing_payload", c, recover(p, s) is None, "python recovers, reference fails")
        continue
    (p, s), emb = signing(int(i["code"]), hx(i["payload"]))
    ok = "0x" + p.hex() == e["signing_payload"] and recover(p, s) is not None and "0x" + recover(p, s).hex() == e["signer"]
    ok = ok and len(emb) == len(e["embedded"])
    for (ep, es), x in zip(emb, e["embedded"]):
        ok = ok and "0x" + ep.hex() == x["signing_payload"] and "0x" + recover(ep, es).hex() == x["signer"]
    check("signing_payload", c, ok)

for h, (n, f) in stats.items():
    print(f"{h:24s} checked={n:3d} mismatches={f}")
for b in bad:
    print("MISMATCH", b)
print(f"total checked={sum(n for n, _ in stats.values())} mismatches={len(bad)}")
sys.exit(1 if bad else 0)
