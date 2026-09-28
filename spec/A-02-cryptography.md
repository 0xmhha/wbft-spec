# A-02. Cryptography

- Status: draft
- Area: `CRYPTO`
- Reference implementation: go-stablenet `740526d03`; BLS through `github.com/supranational/blst v0.3.16` (`go.mod:63`); secp256k1 through the bundled libsecp256k1 (`crypto/secp256k1`) in cgo builds.

WBFT uses three primitives. Keccak-256 hashes everything except inside BLS (§2). secp256k1 ECDSA, with the node key, signs consensus messages and the randao reveal. BLS12-381 signatures, with a key derived from the node key, form the seals that are aggregated into the header. This chapter defines each primitive as the reference uses it, the byte strings that are signed (`seal_data`, `randao_data`), the randao mix, and gives test vectors computed with the reference code.

The encodings that carry these values (message fields, `WBFTExtra`) are in `A-03`. Which validator set a signature is checked against, and what happens to a message or block when a check fails, is in `A-05` and `A-08`.

---

## 1. Notation

- `keccak256(x)` returns 32 bytes (§2).
- `n` is the order of the secp256k1 group: `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141`.
- `r_bls` is the order of the BLS12-381 groups: `0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001` (decimal in `crypto/bls/constants.go:11`).
- `be_min(x)`, `‖`, `bytesN` are defined in `A-01 §1`.

---

## 2. Keccak-256

[WBFT-CRYPTO-001] `keccak256(x)` MUST be the original Keccak-256 with the Keccak padding (the hash used by Ethereum), not the FIPS-202 SHA3-256. `keccak256("")` is `0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470`.
- Source: `crypto/crypto.go:85-93`, `consensus/wbft/utils.go:31-36` (sha3.NewLegacyKeccak256)
- Observable: header, network

Every hash in this specification other than the SHA-256 used inside BLS key derivation (§5.1) and BLS hash-to-curve (§5.4), that is, the block hash, `seal_data`, `randao_data`, the message-signing digest, the deduplication key of `A-03 §8.8` and the shuffle hash of `A-04`, is `keccak256`.

---

## 3. ECDSA over secp256k1

### 3.1 Keys and addresses

[WBFT-CRYPTO-010] A node's identity MUST be the address of its node key: `address = keccak256(X ‖ Y)[12:32]`, where `X ‖ Y` is the 64-byte uncompressed public key without the `0x04` prefix.
- Source: `crypto/crypto.go:287-290` (PubkeyToAddress), `consensus/wbft/backend/backend.go:75`
- Observable: header, network

### 3.2 Signing

```python
def ecdsa_sign(node_key, data: bytes) -> bytes65:
    """Hash-then-sign as done by Backend.Sign."""
    digest = keccak256(data)
    (r, s, recid) = secp256k1_sign_recoverable(node_key, digest)   # RFC 6979 nonce, low-S
    return be32(r) + be32(s) + bytes([recid])                    # recid in {0, 1}
```

[WBFT-CRYPTO-011] A node MUST produce every ECDSA signature of this protocol as `ecdsa_sign(node_key, data)`: the signature is over `keccak256(data)`, and the output is the 65 bytes `R ‖ S ‖ V` with `R`, `S` as 32-byte big-endian integers and `V ∈ {0, 1}` the recovery id (not `27`/`28`).
- Source: `consensus/wbft/backend/backend.go:281-284` (Sign), `crypto/signature_cgo.go:53-60`, `crypto/secp256k1/secp256.go:62-91`
- Observable: network, header

[WBFT-CRYPTO-017] A node SHOULD produce deterministic (RFC 6979) ECDSA signatures, as the reference does. Verifiers cannot check the nonce.
- Source: `crypto/secp256k1/secp256.go:76-79` (secp256k1_nonce_function_rfc6979)
- Observable: network, header

The data signed with ECDSA is:

| What | `data` | Specified in |
|---|---|---|
| consensus message | `message_signing_payload(msg)` | `A-03 §8` |
| randao reveal | `randao_data(chain_id, number)` | §7 |

### 3.3 Recovery

```python
def ecdsa_recover_address(data: bytes, sig: bytes) -> Address | Error:
    """GetSignatureAddress (cgo build)."""
    if len(sig) != 65:                 return Error("invalid signature length")
    if sig[64] >= 4:                   return Error("invalid signature recovery id")
    r, s, v = int(sig[0:32]), int(sig[32:64]), sig[64]
    # libsecp256k1 compact parse: r and s must be in [1, n-1]; recovery must succeed
    Q = secp256k1_recover(keccak256(data), r, s, v)   # Error("recovery failed") on failure
    return address(Q)
```

[WBFT-CRYPTO-013] A verifier MUST recover the signer of `sig` over `data` as `ecdsa_recover_address(data, sig)`: it MUST reject `sig` whose length is not 65, whose recovery byte is `≥ 4`, whose `R` or `S` is `0` or `≥ n`, or for which public-key recovery fails; otherwise the signer is the address of the recovered key.
- Source: `consensus/wbft/utils.go:39-48` (GetSignatureAddress), `crypto/signature_cgo.go:32-43`, `crypto/secp256k1/secp256.go:97-114`, `crypto/secp256k1/secp256.go:163-171`
- Observable: network, header

[WBFT-CRYPTO-014] A verifier MUST accept a signature whose `S` is greater than `n/2` (high-S) when recovery succeeds; the reference does not normalise or reject it. For a given `(data, signer)` there are therefore at least two valid encodings, `(R, S, V)` and `(R, n − S, V ⊕ 1)`.
- Source: `crypto/signature_cgo.go:32-43` (no call to ValidateSignatureValues on this path), `consensus/wbft/utils.go:39-48`
- Observable: network, header

Implementation note (informative): in builds without cgo (`crypto/signature_nocgo.go`, build tag `nacl || js || !cgo || gofuzz`), recovery goes through `decred/dcrd/dcrec/secp256k1/v4 v4.0.1`, which does not reject the recovery bytes `0 … 7` at its recovery-byte check (it maps `V + 27` into its compact-signature range `27 … 34`, `crypto/signature_nocgo.go:42-53`) and treats `V ∈ {4 … 7}` as `V − 4`. A signature with `V ∈ {4, 5}` whose `V − 4` recovers is therefore accepted by a no-cgo build and rejected by a cgo build. The normative rule is the cgo behaviour ([WBFT-CRYPTO-013]).

### 3.4 Validator signature check

```python
def check_validator_signature(validators: ValidatorSet, data: bytes, sig: bytes) -> Address | Error:
    signer = ecdsa_recover_address(data, sig)       # propagate the recovery error
    if signer in validators.addresses:
        return signer
    return Error(ErrUnauthorizedAddress)            # "unauthorized address"
```

[WBFT-CRYPTO-015] `check_validator_signature(V, data, sig)` MUST return the recovered signer if it is a member of `V`, MUST fail with the recovery error if recovery fails, and MUST fail with `ErrUnauthorizedAddress` otherwise.
- Source: `consensus/wbft/utils.go:59-73` (CheckValidatorSignature)
- Observable: network

Which validator set is used for a message (the current one, or the previous height's in one case) is specified in `A-05` (`consensus/wbft/core/core.go:452-462`).

[WBFT-CRYPTO-016] Checking that a signature was made by a given address (used for the randao reveal, §7.2) MUST be `ecdsa_recover_address(data, sig) == address`; a mismatch fails with `ErrInvalidSignature` ("invalid signature").
- Source: `consensus/wbft/backend/backend.go:292-303` (CheckSignature)
- Observable: header

---

## 4. Domain separation of signed data

Informative. The protocol signs four kinds of byte strings. They cannot be confused with each other:

| Signed string | Key | Pre-hash | First byte / length |
|---|---|---|---|
| message signing payload | node key (ECDSA) | `keccak256` | an RLP list whose first element is the message code `0x12 … 0x15` (`A-03 §8`) |
| randao data | node key (ECDSA) | `keccak256` | exactly 32 bytes (itself a hash) |
| seal data | BLS key | none (hash-to-curve with `BLS_DST`) | exactly 32 bytes |
| proof of possession (in `GovValidator`; precompile `B-04` §12.3, registration `B-05`) | BLS key | none (hash-to-curve with `BLS_DST`) | exactly 48 bytes (the public key) |

A message signing payload can itself be 32 bytes long (e.g. a ROUND-CHANGE with 13-byte sequence and round, `df 15 dd …`). The two ECDSA uses are therefore separated by the fact that a `randao_data` value is a `keccak256` output, which matches a message signing payload only with negligible probability, not by length alone.

ECDSA signing payloads carry no chain id; a message is bound to a chain only through the digest and heights it contains.

---

## 5. BLS12-381

The scheme is the IETF BLS signature scheme (draft-irtf-cfrg-bls-signature), "minimal-pubkey-size" variant (public keys in G1, signatures in G2), proof-of-possession ciphersuite `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_`, the same ciphersuite as the Ethereum consensus layer.

### 5.1 Key derivation from the node key

```python
def derive_bls_key(node_key) -> int:
    """bls.DeriveFromECDSA -> blst.KeyGen (IETF KeyGen, draft >= 04; identical to EIP-2333 derive_master_SK)."""
    ikm  = be32(node_key.d)                  # crypto.FromECDSA: 32-byte big-endian private scalar
    salt = b"BLS-SIG-KEYGEN-SALT-"
    while True:
        salt = sha256(salt)
        prk  = hkdf_extract(salt, ikm + b"\x00")                 # HMAC-SHA256
        okm  = hkdf_expand(prk, b"" + (48).to_bytes(2, "big"), 48)  # key_info = "" 
        sk   = int.from_bytes(okm, "big") % r_bls
        if sk != 0:
            return sk
```

[WBFT-CRYPTO-020] A node's BLS secret key MUST be `derive_bls_key(node_key)` as defined above: IKM is the 32-byte big-endian node private key, the salt starts as `SHA-256("BLS-SIG-KEYGEN-SALT-")` and is re-hashed while the result is zero, `key_info` is empty, `L = 48`.
- Source: `crypto/bls/bls.go:89-93` (DeriveFromECDSA), `crypto/crypto.go:168-174` (FromECDSA), `crypto/bls/blst/secret_key.go:36-46` (GenerateKey), blst `bindings/go/blst.go:243-258` (KeyGen), blst `src/keygen.c:135-216`
- Observable: header, network

[WBFT-CRYPTO-021] The BLS public key of a validator MUST be `sk · G1` encoded in the 48-byte compressed form (§5.2). The key registered for a validator (genesis `init.blsPublicKeys`, `GovValidator` state, `EpochInfo.bls_public_keys`) is expected to equal the public key derived by [WBFT-CRYPTO-020] from that validator's node key; a node whose derived key differs cannot produce seals that verify.
- Source: `crypto/bls/blst/secret_key.go:64-67`, `consensus/wbft/core/core.go:471-488`
- Observable: header

Implementation note (informative): the derivation was checked independently of blst with an HKDF implementation in Python for the keys of §8.1; the results match.

### 5.2 Encodings

[WBFT-CRYPTO-022] BLS values MUST be encoded as: secret key — 32 bytes big-endian; public key — 48-byte compressed G1 point; signature — 96-byte compressed G2 point. Compression uses the ZCash BLS12-381 serialisation (the three most significant bits of the first byte are the compression, infinity and sign flags); the point at infinity is `0xc0` followed by zero bytes.
- Source: `crypto/bls/blst/secret_key.go:92-96`, `crypto/bls/blst/public_key.go:81-84`, `crypto/bls/blst/signature.go:277-280`, `crypto/bls/common/constants.go:14-21`
- Observable: header, network

### 5.3 Decoding and validation

```python
def bls_public_key_from_bytes(b: bytes) -> G1 | Error:
    if len(b) != 48:                  return Error("public key must be 48 bytes")
    P = g1_uncompress(b)              # flags, x < p, on curve
    if P is None:                     return Error("could not unmarshal bytes into public key")
    if P.is_infinity() or not P.in_subgroup():
        return Error(ErrInfinitePubKey)   # "received an infinite public key" (also used for subgroup failure)
    return P

def bls_signature_from_bytes(b: bytes) -> G2 | Error:
    if len(b) != 96:                  return Error("could not create signature from byte slice: signature must be 96 bytes")
    S = g2_uncompress(b)
    if S is None:                     return Error("could not create signature from byte slice: could not unmarshal bytes into signature")
    if not S.in_subgroup():           return Error("signature not in group")
    return S                          # the point at infinity is accepted here
```

[WBFT-CRYPTO-023] A public key MUST be rejected if its length is not 48, if it does not decompress to a point on the curve, if it is the point at infinity, or if it is not in the prime-order subgroup.
- Source: `crypto/bls/blst/public_key.go:29-58` (publicKeyFromBytes, KeyValidate)
- Observable: header

[WBFT-CRYPTO-024] A signature MUST be rejected if its length is not 96, if it does not decompress to a point on the curve, or if it is not in the prime-order subgroup. The point at infinity MUST NOT be rejected at decoding; it fails verification instead ([WBFT-CRYPTO-026]).
- Source: `crypto/bls/blst/signature.go:32-43` (signatureFromBytesNoValidation), `crypto/bls/blst/signature.go:55-67` (SignatureFromBytes, SigValidate(false))
- Observable: header, network

[WBFT-CRYPTO-056] Decompression of a public key (48 bytes, G1) or a signature (96 bytes, G2) MUST reject the input if: the compression flag (bit 7 of byte 0) is 0; the infinity flag (bit 6 of byte 0) is 1 and the sign flag (bit 5 of byte 0) or any other bit of the input is 1; the infinity flag is 0 and a coordinate component is not less than the field modulus `p` (for G1, the value of the 48 bytes with the three flag bits cleared; for G2, that value for the first 48 bytes, which hold `x.c1`, and the value of the last 48 bytes, which hold `x.c0`); or no point of the curve has that `x`. The sign flag selects one of the two points with that `x`, so flipping it in a valid encoding yields the encoding of the negated point, which decodes. Every point therefore has exactly one accepted encoding.
- Source: `crypto/bls/blst/public_key.go:43-46` (Uncompress), `crypto/bls/blst/signature.go:32-43` (Uncompress), `go.mod:63` (blst v0.3.16), checked by execution in §8.3
- Observable: header, network

### 5.4 Signing

[WBFT-CRYPTO-025] `bls_sign(sk, msg)` MUST be `sk · hash_to_G2(msg, BLS_DST)` with `hash_to_G2` = `hash_to_curve` of RFC 9380 for suite `BLS12381G2_XMD:SHA-256_SSWU_RO_`, applied to `msg` itself (no prior hashing and no augmentation). In WBFT `msg` is always the 32-byte `seal_data` (§6).
- Source: `consensus/wbft/backend/backend.go:286-289` (SignWithoutHashing), `crypto/bls/blst/secret_key.go:87-90`, `crypto/bls/blst/signature.go:22`
- Observable: network, header

### 5.5 Verification of one signature

[WBFT-CRYPTO-026] `bls_verify(pk, msg, sig)` MUST return true if and only if `e(pk, hash_to_G2(msg, BLS_DST)) = e(G1, sig)`, where `pk` and `sig` passed [WBFT-CRYPTO-023] and [WBFT-CRYPTO-024]. With a valid (non-infinity) `pk`, a signature equal to the point at infinity MUST verify as false.
- Source: `crypto/bls/blst/signature.go:119-122` (Verify), computed check in §8.3
- Observable: network, header

[WBFT-CRYPTO-055] `bls_verify(pk, msg, sig)` MUST return false when `pk` is the point at infinity, whatever `msg` and `sig` are. A key that passed [WBFT-CRYPTO-023] is never the point at infinity, but an aggregate key ([WBFT-CRYPTO-028]) is when the selected keys sum to zero. In that case an aggregated seal MUST be rejected with `ErrInvalidSeal` even if its signature is also the point at infinity, for which the pairing equation of [WBFT-CRYPTO-026] holds trivially. This is the `KeyValidate` step of the IETF `CoreVerify` applied to the aggregate key; blst returns `BLST_PK_IS_INFINITY` before computing a pairing.
- Source: `consensus/wbft/engine/engine.go:1355-1366` (verifyAggregatedSeal), `crypto/bls/blst/public_key.go:61-79` (AggregatePublicKeys: no check on the sum), `crypto/bls/blst/signature.go:119-122` (Verify), blst v0.3.16 `src/aggregate.c:294-297`, checked by execution in §8.3
- Observable: header

### 5.6 Aggregation

```python
def bls_aggregate(sigs: list[bytes96]) -> bytes96 | Error:
    """AggregateCompressedSignatures(sigs, groupcheck=True)."""
    acc = G2_INFINITY
    for b in sigs:
        S = g2_uncompress(b)
        if S is None or not S.in_subgroup():
            return Error("provided signatures fail the group check and cannot be compressed")
        acc = acc + S
    return g2_compress(acc)          # empty list -> 0xc0 || 0x00 * 95
```

[WBFT-CRYPTO-027] `bls_aggregate(sigs)` MUST be the compressed sum of the decompressed, subgroup-checked signatures; it MUST fail if any input fails decompression or the subgroup check. The aggregate of the empty list is the point at infinity; an input equal to the point at infinity is accepted.
- Source: `crypto/bls/blst/signature.go:69-77`, computed checks in §8.3
- Observable: header

[WBFT-CRYPTO-028] Public keys MUST be aggregated as the sum of keys each validated by [WBFT-CRYPTO-023]; aggregation of an empty list fails.
- Source: `crypto/bls/blst/public_key.go:60-79` (AggregatePublicKeys)
- Observable: header

### 5.7 Verification of an aggregated seal

```python
def verify_aggregated_seal(V: ValidatorSet, header, round: uint32,
                           agg: AggregatedSeal, seal_type: SealType) -> None | Error:
    """verifyAggregatedSeal: FastAggregateVerify over the validators named by the bitmap."""
    idx = sealer_indices(agg.sealers)              # A-03 §4.4, ascending
    if len(idx) < quorum_size(len(V)):             # A-04
        return Error("lack of seal count")
    pks = []
    for i in idx:
        if i >= len(V):
            return Error("sealer is not validator")
        pks.append(V[i].bls_public_key)
    msg = seal_data(header, round, seal_type)      # §6
    apk = aggregate_public_keys(pks)               # WBFT-CRYPTO-028 (errors propagate)
    sig = bls_signature_from_bytes(agg.signature)  # WBFT-CRYPTO-024 (errors propagate)
    if not bls_verify(apk, msg, sig):              # false when apk is the point at infinity (WBFT-CRYPTO-055)
        return Error(ErrInvalidSeal)                # "invalid seal"
    return None
```

[WBFT-CRYPTO-030] Verification of an aggregated seal MUST perform the checks of `verify_aggregated_seal` in the order shown and fail at the first failing check. The caller (`A-08`) maps any failure to the field-specific error (`ErrInvalidPreparedSeals`, `ErrInvalidCommittedSeals`, `ErrInvalidPrevPreparedSeals`, `ErrInvalidPrevCommittedSeals`).
- Source: `consensus/wbft/engine/engine.go:1338-1369` (verifyAggregatedSeal), `consensus/wbft/engine/engine.go:384-450`
- Observable: header

[WBFT-CRYPTO-031] Because each sealer index is a bit of a bitmap, a validator can contribute at most once to an aggregated seal. A producer MUST NOT include two seals from the same sealer index in one aggregate: the reference sets the bit once but adds the signature twice, so the resulting aggregate fails verification.
- Source: `consensus/wbft/engine/engine.go:130-151` (aggregateSeal), `core/types/istanbul.go:292-298` (SetSealer is idempotent)
- Observable: header

Implementation note (informative): the aggregated-seal check is the IETF `FastAggregateVerify` expressed as "aggregate the public keys, then `Verify`". Rogue-key resistance relies on every registered key having passed a proof of possession (§5.8). The reference caches decoded public keys in an LRU of 2,000,000 entries (`crypto/bls/blst/public_key.go:16-58`).

### 5.8 Proof of possession (informative)

Registered BLS keys are checked by the `GovValidator` contract through the precompile at `0x0000000000000000000000000000000000B00001`. The precompile requires an input of exactly 144 bytes `pubkey(48) ‖ signature(96)` and decodes both with §5.3. If the length is wrong or either decoding fails, it fails (`invalid input length`, `invalid BLS public key`, `invalid BLS signature`) and `GovValidator` reverts with `FailedToVerifyBlsKey`; otherwise it returns 32 bytes whose last byte is `1` iff `bls_verify(pk, pubkey_bytes, sig)`, that is, the proof of possession is an ordinary signature over the 48-byte compressed public key with `BLS_DST` (not the IETF `PopProve` tag `BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_`). Details belong to `B-04` §12.3 (precompile) and `B-05` (registration).
- Source: `core/vm/contracts.go:1189-1221` (blsPoP), `params/protocol_params.go:208`, `systemcontracts/solidity/v1/GovValidator.sol:157-171`

---

## 6. Seal data

```python
def seal_data(header: Header, round: uint32, seal_type: SealType) -> bytes32:
    """core.PrepareSeal."""
    h = hash_with_round(header, round)             # A-03 §6.2 (WBFTHashWithRoundNumber)
    return keccak256(h + bytes([seal_type]))       # 33-byte preimage
```

[WBFT-CRYPTO-040] `seal_data(header, round, seal_type)` MUST be `keccak256(hash_with_round(header, round) ‖ seal_type)`, a 33-byte preimage, where `hash_with_round` is defined in `A-03 §6.2` and `round` is the `uint32` of `A-01 [WBFT-TYPE-012]`.
- Source: `consensus/wbft/core/core.go:464-469` (PrepareSeal), `core/types/block.go:169-172` (WBFTHashWithRoundNumber)
- Observable: header, network

[WBFT-CRYPTO-041] `seal_data` MUST be computed from the header without the current-block seals: `hash_with_round` removes `prepared_seal` and `committed_seal` and replaces `round`, and keeps every other header field and every other `WBFTExtra` field (`vanity_data`, `randao_reveal`, `prev_round`, `prev_prepared_seal`, `prev_committed_seal`, `gas_tip`, `epoch_info`). For `round = 0`, `hash_with_round(header, 0) = block_hash(header)` whenever `header.Difficulty = 1` and the extra decodes; hence `seal_data(header, 0, t) = keccak256(block_hash(header) ‖ t)`.
- Source: `core/types/istanbul.go:269-288`, `core/types/block.go:121-131`
- Observable: header, network

[WBFT-CRYPTO-042] A seal MUST be `bls_sign(sk, seal_data(header, round, seal_type))`. The prepare seal in a PREPARE uses `seal_type = PREPARE_SEAL`, the commit seal in a COMMIT uses `COMMIT_SEAL`; in both, `header` is the proposal's header and `round` is the message's round modulo 2^32.
- Source: `consensus/wbft/core/prepare.go:47`, `consensus/wbft/core/commit.go:49`
- Observable: network

[WBFT-CRYPTO-043] An individual seal received from validator `a` MUST be checked by: looking up `a`'s BLS key in the validator set; rejecting the key per [WBFT-CRYPTO-023] (error propagated); rejecting the seal per [WBFT-CRYPTO-024] (`errInvalidSeal`); and `bls_verify(pk_a, seal_data(...), seal)` (`errInvalidSigner` on false).
- Source: `consensus/wbft/core/core.go:471-488` (verifySeal)
- Observable: network

[WBFT-CRYPTO-044] When the header's extra does not decode, `hash_with_round(header, round)` in the reference evaluates to `keccak256(0xc0)` for every such header, so `seal_data` is the constant `keccak256(keccak256(0xc0) ‖ seal_type)`. A node MUST NOT sign a seal for a header whose extra does not decode.
- Source: `core/types/istanbul.go:269-275` (returns nil), `core/types/block.go:169-172` (rlpHash of a nil header), computed in §8.4
- Observable: network

---

## 7. Randao

### 7.1 Randao data

```python
def randao_data(chain_id: bigint, number: bigint) -> bytes32:
    return keccak256(be_min(chain_id) + be_min(RANDAO_VERSION) + be_min(number))   # be_min(1) = 0x01
```

[WBFT-CRYPTO-050] `randao_data(chain_id, number)` MUST be `keccak256(be_min(chain_id) ‖ 0x01 ‖ be_min(number))`, with `chain_id` the configured chain id and `number` the header number. The components are concatenated without length prefixes; `be_min(0)` is the empty string.
- Source: `consensus/wbft/engine/engine.go:563-573` (makeRandaoData)
- Observable: header

### 7.2 Randao reveal

### 7.3 Randao mix

```python
def randao_mix(parent_mix: bytes32, reveal: bytes) -> bytes32:
    x = int.from_bytes(parent_mix, "big") ^ int.from_bytes(keccak256(reveal), "big")
    return x.to_bytes(32, "big")        # left-padded: equal to a bytewise XOR of the two 32-byte values
```

[WBFT-CRYPTO-053] `randao_mix(parent_mix, reveal)` MUST be the bytewise XOR of `parent_mix` and `keccak256(reveal)`, returned as 32 bytes. Leading zero bytes of the result MUST be kept (the reference converts through a big integer and left-pads to 32 bytes, which is equivalent).
- Source: `consensus/wbft/engine/engine.go:575-581` (CalculateRandaoMix)
- Observable: header

The randao mix of an epoch block is the shuffle seed for the next epoch's validator order (`A-04`, `consensus/wbft/engine/engine.go:1265-1277`). The mix of every block is also the value of the EVM opcode `PREVRANDAO` (`0x44`) in that block (`core/evm.go:61-64`, `core/vm/jump_table.go:117-124`; specified in Part B).

---

## 8. Test vectors

All vectors in this section were computed by running the reference code: a Go program in a separate module that imports `github.com/ethereum/go-ethereum` with a `replace` directive pointing at go-stablenet `740526d03`, calling the functions named in each table (`crypto.Sign`, `bls.DeriveFromECDSA`, `core.PrepareSeal`, `engine.CalculateRandaoMix`, `Engine.CommitHeader`, …). The keccak-only rows, the key derivation of §8.1 and `seal_data` at round 0 were additionally recomputed in Python (pycryptodome Keccak, hashlib HMAC) and match. The generator program is not part of this specification; the drafting tool `tools/vectors-a01-a03` is its starting point (`A-11` §3.2).

### 8.1 Keys

Node keys are `key_i = keccak256(ASCII("wbft-spec-vector-key-" ‖ decimal(i)))`.

| i | node private key | address | BLS secret key (`derive_bls_key`) | BLS public key |
|---|---|---|---|---|
| 0 | `b96c8bb76f8818a11de0403b189877f6524f4392a3faa9660de7a219670ef59d` | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` | `35de7bc5d48c484e7b87e7b96ebf61d15ae225d6dd3fea0663c9291b5ac10f5a` | `8eaaca1bbb29c07cb653b346e8434bb6b3bc63a4bb7c8dfea5c46bae2473646fe8041e3ff81da70bba7effda935a4576` |
| 1 | `385a3623913dc8f0191c5e3779605215dcce5b25342ede6afdc8d8e8d172de39` | `0x1111F6010b9b757b30e00A7D519cd98094b7A610` | `488c292335a8c97b4c228b4ba2d3e43c875ad2445e3331ff173b8630b47d65bc` | `8a8f913a1bac306b3b1661fc78ce376a650341ce314f7c0f615fc7abf3722eb6c256046483c266d3e03ce82edbed13a3` |
| 2 | `991019e805bf05d3ab0e738b4e7d695c16ae4a9bb56967dfca66611887a05a09` | `0x503578B72bf5c96AB41508E2AC70303691dfcdDb` | `3d57e6be7c50f146643b268d185c00c1bf67708a590c8662ce976b14ee16fc64` | `976a4cd50f07ad6fb3fc0a7a27a803212be7cf84b779d9a5562af81316187dec8ee319a0ce421c020dc1bf1faa494abe` |
| 3 | `07450e791971fe429ceabb5a10a8f826127c4981190057413efaf9b07809f681` | `0x084AC57c61cd5Dd6244eB0b4c42cA72d073A6c14` | `4dbb674eac4df7166edbdca4ef7a7f96a3e73be6935164e276a57a48360f8a47` | `aa6b0cda839b65a5e2e556484ccb3d67fac59c1cc66ab2bf1b9160ecee30a0deb6dbbad04836ca99ceb74c225a0c184e` |

### 8.2 ECDSA

| Case | Input | Output |
|---|---|---|
| `keccak256("wbft")` | ASCII `wbft` | `154d94908e42308ff21897b1445bd5001dedb9fa41095ad342cb656fffe2c55f` |
| `ecdsa_sign(key0, "wbft")` | — | `20a777b74c9e9838257309f91fd348d53485fb0bedbdcabf4a41faeb17b8ce35 65182fbdf4d2e6a7f69491e35053b331f003523e36f6acddf45b22bbedb3757c 00` (R ‖ S ‖ V) |
| recover | the signature above | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` |
| high-S twin: `S' = n − S`, `V' = V ⊕ 1` | `…b8ce35 9ae7d0420b2d1958096b6e1cafac4ccccaab8aa87851f35dcb773bd0e282cbc5 01` | recovers the same address (accepted, [WBFT-CRYPTO-014]) |
| `V = 27` | same R, S | error `invalid signature recovery id` |
| `V = 2` | same R, S | error `recovery failed` |
| 64-byte signature | first 64 bytes | error `invalid signature length` |

### 8.3 BLS

| Case | Input | Output |
|---|---|---|
| `bls_sign(sk0, m)` | `m = keccak256("wbft-bls") = d95400c38384460a9d671ec5ead86860b0dd0955756275a96b01266a72df305b` | `99578d7f3a838333dfeeb2a7ade9fa0e150e913fb852dac9606c06099b929079e2a0a89ed229fe40eb23d82c9511be2b161297f1a426a8fc5dcedd81f20783288897330bf58ad2e94098f6bc63a638547b72f35fb906e273f84f48e77c59066f` |
| `bls_verify(pk0, m, ·)` | the signature above | true |
| public key = infinity | `c0` ‖ 47 × `00` | rejected: `received an infinite public key` |
| public key = 48 zero bytes | — | rejected: `could not unmarshal bytes into public key` |
| public key of 47 bytes | — | rejected: `public key must be 48 bytes` |
| signature = infinity | `c0` ‖ 95 × `00` | decodes; `bls_verify(pk0, m, ·)` = false |
| signature = 96 zero bytes | — | rejected at decoding |
| `bls_aggregate([])` | — | `c0` ‖ 95 × `00` (no error) |
| `bls_aggregate([infinity])` | — | no error |
| signature = infinity with the sign flag | `e0` ‖ 95 × `00` | rejected at decoding ([WBFT-CRYPTO-056]) |
| signature = infinity with a non-zero bit | `c0` ‖ 94 × `00` ‖ `01` | rejected at decoding |
| infinity flag without the compression flag | `40` ‖ 95 × `00` | rejected at decoding |
| valid signature or public key with the compression flag cleared | — | rejected at decoding |
| valid public key with `p` added to `x` (fits in 381 bits) | — | rejected: `could not unmarshal bytes into public key` |
| valid signature with `p` added to the first half (`x.c1`) or to the second half (`x.c0`) | — | rejected at decoding (each half checked separately) |
| valid signature or public key with the sign flag flipped | — | decodes (the negated point); `bls_verify` of the original message = false |
| aggregate of the public keys of `a`, `b`, `(−a−b) mod r` (any secret keys `a`, `b`) | — | `c0` ‖ 47 × `00` (no error) |
| aggregate of their signatures over one message | — | `c0` ‖ 95 × `00`; `bls_verify` with the aggregate key above = false ([WBFT-CRYPTO-055]) |

### 8.4 Seal data

Header `H` (chain 8282, block 2, proposer key 0). It is a vector for hashing only and is not a valid chain block (parent hash, state root and mix digest are placeholders).

| Field | Value |
|---|---|
| ParentHash | `0x1111…11` (32 × `11`) |
| UncleHash | `EMPTY_UNCLE_HASH` |
| Coinbase | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` |
| Root | `0x2222…22` |
| TxHash, ReceiptHash | `0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421` |
| Bloom | 256 zero bytes |
| Difficulty | 1 |
| Number | 2 |
| GasLimit | 105000000 |
| GasUsed | 0 |
| Time | 1700000002 |
| Extra | `WBFTExtra{vanity = 32 × 00, randao_reveal = ecdsa_sign(key0, randao_data(8282, 2)), prev_round = 0, prev seals = nil, round = 0, seals = nil, gas_tip = 27600000000000, epoch_info = nil}` (bytes in `A-03 §9.3`) |
| MixDigest | `0x00…00` |
| Nonce | `0x0000000000000000` |
| BaseFee | 20000000000000 |

| Quantity | Value |
|---|---|
| `block_hash(H)` = `hash_with_round(H, 0)` | `6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e` |
| `hash_with_round(H, 1)` | `12fbf50df755a39e84a982f2cee1ad16ca800ee1e7a74f980cc7948b6d515752` |
| `hash_with_round(H, 2)` | `9493c5c1781d9d10df8b95545bc4272ad33a6c30b0b5319eade35b513dcf2d4e` |
| `seal_data(H, 0, PREPARE_SEAL)` | `9634606e7f65a734cd97a24d8a6458383e38f5e5c9b679864b15d0db09b94391` |
| `seal_data(H, 0, COMMIT_SEAL)` | `087eccbdf4ac7846d8255e2d0ef5e6e5f0b344fef86c625e12b9ba123eb3ee50` |
| `seal_data(H, 2, PREPARE_SEAL)` | `5e2e30b10f096d24641da9829661c6df27738aab23b7071206bcb845c2691152` |
| `seal_data(H, 2, COMMIT_SEAL)` | `364771f56f2ddcf3f80b9f9fdc5085a3d3f8cbc23c80578e5be1543c2aab2e17` |
| `seal_data(X, 0, PREPARE_SEAL)` for any `X` with undecodable extra ([WBFT-CRYPTO-044]) | `f3aac67c5fc8b34464e4fe705b4570279ead9d482adf0d2c875a970920027699` = `keccak256(keccak256(0xc0) ‖ 0x00)` |

Seals over `H` at round 0 by validators 0, 1, 2:

| i | prepare seal | commit seal |
|---|---|---|
| 0 | `948ebbf09e29ab73d442c98b7388743c7fde4fc94f346b5352cd5b70e2964ddc0cde0550fb9797a85a3955fd5f91911012c063e29dc28bf3b4a6d12a0d598ce2104599197c83cf92abc3f2406131e8f38ba7b7c282e5055525ee4867f9d11f18` | `b6e536219a2ccd63720a9f4964350c9b1d165ea490b27aaeeb8755b1958e11db6220d43910e8136d1392ac662e62d3b50cef653210638ffccf2fed38fd0b5580c0db9aaf7643833b410175602dd94ec43cdb3befe578e174b1bd73ad2e22bdf7` |
| 1 | `b9ed2c8ca8ef593fa5b1749082a5bc211460b913a9c5a3eec8770d5acff6bf6501dfa61735f6cdd045e5e75a9489801a0bca5554be5abae320e5d883dae61838ad5765c1427cb4a5672dabd5649969760505ff7aaee60aa2e2e5a623af1726a1` | `8be6ed014837a2899ec095d92355a408ee4031726639fb2a9cbf6da3337a0326f18ff10e5efb8c273bbb3796fe2b15b3128da804e78612920c0621e93a3baf3883085445dc2da093dd96e34b9b373706c801aabef8b2f698034b178cb53b8123` |
| 2 | `98d7eb5e1339363dee799b40f76c632a8bde3451458d9731ef8bcee5557ce85f96f64a228734ee776b3099b88b6ca885046e1b2c3b5764982529b5d9592cc642d9ce399e4d584584443d73b6f9bdea2826daff2be70b8a6d85159f02ee41302d` | `a0b379390308e1abca3dd3fda9b8ca2bb97bec9d21a273d715f98e0d9571ca25872c717ec95185377bf4ff56ad37a273163b0429116df980359598028a79e7dbadec4e9580d8a6111e2106c2e8081de928cd898e1db0757fb65f98a264323164` |

Aggregates (as written by `Engine.CommitHeader`):

| Quantity | Value |
|---|---|
| prepared aggregate: sealers | `07` |
| prepared aggregate: signature | `895488e186a03aca550753cf195015b7affeecb72d78908823c3c6aec07b66b32f16101b35e309c162f05d763ec70b770842d26f3913ae6bf5123566b852c79bcfcf27e7844e513c662b736916ea81fb0b094b1215ed129e6ab3f71a88164359` |
| committed aggregate: sealers | `07` |
| committed aggregate: signature | `b1d4051be1d47483b8a553ae1d32c0a50c604c156bdda2ce050109daf4d2ac5b5c2d9c1bc71d20d7441f3e0f79c1664609db5c613953b11ba97580b068eb427f4743d3cce8853cd0e741e8fc800263ccc6d4f5ab2844be5f6c1f8b619d5fb341` |
| aggregate public key of validators 0, 1, 2 | `ae65590bbc43826eab7e89df4307bfec7006a4bf3e735f7ef63dc707a04c73bb3fb16bdab90576f3c7419d5af9390fb0` |
| `bls_verify(apk, seal_data(H, 0, COMMIT_SEAL), committed signature)` | true |
| `block_hash` of the committed header | `6284d1a2…0a1e` (unchanged, [WBFT-CRYPTO-041]) |
| `CommitHeader` with round `2^32 + 3` | header `round` field = `3` |

### 8.5 Randao

| `chain_id` | `number` | preimage | `randao_data` |
|---|---|---|---|
| 8282 | 0 | `205a01` | `f6c98387ef170854e352073e846537b9947cfe23ddb4fa79932c3c248577ebd2` |
| 8282 | 1 | `205a0101` | `48516c21b70c7a85c71e6fe34cd724419d7803fb58bc1cf3f69a76eb85d98b3f` |
| 8282 | 256 | `205a010100` | `50ee1ee4f218972dbf488dd8f6eee54beb17e0b75169d1cdc543ac439516951c` |
| 8283 | 1 | `205b0101` | `2536b43be46ffc0db54c300982998c0476b79a0c13bc5558e84887db975a046a` |

| Case | Value |
|---|---|
| reveal = `ecdsa_sign(key0, randao_data(8282, 1))` | `c2384d0a5f278af89e4a50f9cb4bab11e13b47a9326bad7dd80676f3b64a8afd5838925a1c50009cdfdfca97b92b95e483bed47591461bbab3d3efa442c8027900` |
| `keccak256(reveal)` | `02d9a1f0741587237b57b8ef79de5d93e99fbac76077a26c03ad4139f01769af` |
| `randao_mix(0x00…00, reveal)` | `02d9a1f0741587237b57b8ef79de5d93e99fbac76077a26c03ad4139f01769af` |
| `randao_mix(0x00ff00ff…00ff, reveal)` | `0226a10f74ea87dc7ba8b81079215d6ce960ba386088a293035241c6f0e86950` |
| `randao_mix(keccak256(reveal), reveal)` (leading zeros kept) | `0000000000000000000000000000000000000000000000000000000000000000` |
| `ecdsa_recover_address(randao_data(8282,1), reveal)` | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` |
