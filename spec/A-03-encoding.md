# A-03. Encoding

- Status: draft
- Areas: `ENC` (RLP conventions, header extra data, block hash), `MSG` (consensus message wire formats and signing payloads)
- Reference implementation: go-stablenet `740526d03`, including its vendored `rlp` package.

Every byte string that WBFT puts on the wire or into a header is an RLP encoding. This chapter first fixes the RLP rules the reference applies (which are go-ethereum's, including its conventions for absent values), then specifies the consensus data in `Header.Extra` (`WBFTExtra`), the block-hash rule that removes the current-block seals, and the four consensus messages: their wire format, the bytes that are signed, the checks made while decoding, and the key used to recognise duplicates. Worked byte-level examples computed with the reference code are in §9.

What a node does with a decoded message (acceptance, state changes) is in `A-05`; how messages are framed and relayed on `istanbul/100` is in `A-07`; which header fields are checked and in what order is in `A-08`.

---

## 1. Notation

- `rlp_encode(v)` / `rlp_decode(b, T)` are the functions of §2 and §3 applied to a value of the schema type `T`.
- Schemas are written as lists in brackets: `[a: T1, b: T2]` is an RLP list whose first item encodes `a` as `T1`, and so on.
- Item types: `bytes` (RLP string of any length), `bytesN` (RLP string of exactly `N` bytes), `uint32`, `uint64`, `bigint` (§3), `list[T]` (RLP list whose items are all `T`), `opt[T]` (an absent-able value, §2.4).
- `0x80` is the empty string, `0xc0` the empty list.

---

## 2. RLP conventions

### 2.1 Items and canonical form

[WBFT-ENC-001] All encodings in this specification MUST be RLP as defined in the Ethereum Yellow Paper, appendix B.
- Source: `rlp/encode.go:77-93` (EncodeToBytes), `rlp/decode.go:1038-1097` (readKind)
- Observable: header, network

[WBFT-ENC-002] A decoder MUST reject input that is not in canonical form, specifically: a single byte in `0x00 … 0x7f` encoded as a one-byte string (`0x81 xx`); a long-form length (prefix `0xb8 … 0xbf` or `0xf8 … 0xff`) whose value is below 56; a long-form length with a leading zero byte; a length that exceeds the remaining input.
- Source: `rlp/decode.go:1038-1096` (readKind), `rlp/decode.go:1098-1121` (readUint), `rlp/decode.go:1011-1036` (Kind: element/value too large), `rlp/decode.go:635-658` (Bytes), `rlp/decode.go:369-399` (decodeByteArray), `rlp/decode.go:742-773` (uint), `rlp/decode.go:848-889` (decodeBigInt)
- Observable: header, network

[WBFT-ENC-003] When a whole byte string is decoded as one value (header extra, message payload), the decoder MUST reject the input if bytes remain after the first complete item. (The items of the PRE-PREPARE round-change justification are also decoded with this function, but each item is first delimited as exactly one RLP item, so the rule has no effect there.)
- Source: `rlp/decode.go:92-106` (DecodeBytes, `ErrMoreThanOneValue`), `rlp/decode.go:690-716` (Raw), `consensus/wbft/messages/preprepare.go:84`, `consensus/wbft/messages/preprepare.go:96`
- Observable: header, network

### 2.2 Structures

[WBFT-ENC-004] A structure MUST be encoded as a list of its fields in declaration order. A decoder MUST reject a list with fewer items than fields ("too few elements") or more items than fields ("input list has too many elements"). None of the WBFT structures of this chapter has optional or tail fields; among the go-ethereum structures used by this chapter, `Header` (§5.1) and the block list `extblock` (§5.2, trailing `withdrawals`) have optional trailing fields. (The same `rlp:"optional"` rule, under which trailing zero-valued optional fields are omitted on encoding and missing ones decode as zero, also governs the state account's `Extra` field, `B-04` SNET-SYS-070.)
- Source: `rlp/decode.go:404-435` (makeStructDecoder), `core/types/block.go:84-97` (the `rlp:"optional"` fields of `Header`), `core/types/block.go:234` (`extblock.Withdrawals`), `core/types/state_account.go:37` (`StateAccount.Extra`)
- Observable: header, network

### 2.3 Byte strings and fixed-size arrays

[WBFT-ENC-005] A field of type `bytes` MUST accept any length, including 0. A field of type `bytesN` (`Address` = `bytes20`, `Hash` = `bytes32`, `Bloom` = `bytes256`, `BlockNonce` = `bytes8`) MUST reject a string of any other length and MUST reject a list.
- Source: `rlp/decode.go:369-399` (decodeByteArray), `rlp/encode.go:253-265`
- Observable: header, network

### 2.4 Absent values

The reference represents an optional value as a Go pointer, and an absent value as a nil pointer. On the wire an absent value is an empty item. Which empty item is used depends on the type:

| Type of the optional value | Encoding of "absent" | Decoding of an empty item in that position |
|---|---|---|
| structure (`AggregatedSeal`, `EpochInfo`) with the `rlp:"nil"` tag | `0xc0` | `0xc0` → absent; `0x80` → error "wrong kind of empty value" |
| structure without the `rlp:"nil"` tag | `0xc0` | the structure's own decoder runs on the empty list and fails ("too few elements") |
| `bigint` (`*big.Int`), with or without the `rlp:"nil"` tag | `0x80` | `0x80` → **present, value 0**; `0xc0` → error "expected input string or byte" |
| `bytes` (nil slice) | `0x80` | `0x80` → present, empty |
| `list[T]` (nil slice) | `0xc0` | `0xc0` → present, empty list |

`Block` is never tagged `rlp:"nil"`: an absent block encodes as `0xc0` (row 2). How an empty item is decoded in the ROUND-CHANGE `prepared_block` position is specified by [WBFT-MSG-032] step 4; the PRE-PREPARE `proposal` has no tag, so `0xc0` there fails ([WBFT-MSG-041]).

[WBFT-ENC-006] An encoder MUST encode an absent structure as `0xc0` and an absent `bigint` or `bytes` as `0x80`.
- Source: `rlp/encode.go:413-432` (makePtrWriter), `rlp/encode.go:215-226` (writeBigIntPtr), `rlp/internal/rlpstruct/rlpstruct.go:49-55` (DefaultNilValue), `rlp/typecache.go:215-232`
- Observable: header, network

[WBFT-ENC-007] For a structure field tagged `rlp:"nil"`, a decoder MUST decode `0xc0` as absent and MUST reject `0x80`; any non-empty item MUST be decoded as the structure.
- Source: `rlp/decode.go:476-513` (makeNilPtrDecoder)
- Observable: header, network

[WBFT-ENC-008] A `bigint` field MUST NOT be decoded as absent, even when it is tagged `rlp:"nil"`: `0x80` decodes to the value 0. Consequently an encoder's "absent" and "zero" are the same bytes, and a decoded `bigint` is never absent.
- Source: `rlp/decode.go:156-163` (the `*big.Int` case precedes the pointer case), `rlp/decode.go:231-243` (decodeBigInt)
- Observable: header

---

## 3. Scalar encodings

### 3.1 Fixed-width unsigned integers

[WBFT-ENC-010] A `uint32` or `uint64` MUST be encoded as the RLP string `be_min(x)` (so `0` is `0x80`, `1 … 127` a single byte). A decoder MUST reject: a string with a leading zero byte, including the single byte `0x00` ("non-canonical integer"); a string longer than the type width (more than 4 bytes for `uint32`, 8 for `uint64`); a list.
- Source: `rlp/encode.go:205-213` (writeUint), `rlp/decode.go:742-773` (Stream.uint)
- Observable: header, network

### 3.2 Unbounded integers

[WBFT-ENC-011] A `bigint` MUST be encoded as the RLP string `be_min(x)`; a negative value cannot be encoded. A decoder MUST accept any length and MUST reject a leading zero byte, the single byte `0x00`, and a list. Decoded values are therefore never negative.
- Source: `rlp/encode.go:215-235`, `rlp/decode.go:848-889` (decodeBigInt)
- Observable: header, network

---

## 4. `WBFTExtra`

### 4.1 Layout

`Header.Extra` of every WBFT block is exactly the RLP encoding of one `WBFTExtra` list. There is no byte prefix outside the RLP list; the vanity bytes are the first item of the list.

```
WBFTExtra = [
    vanity_data:         bytes,
    randao_reveal:       bytes,                    # 65-byte ECDSA signature (A-02 §7.2)
    prev_round:          uint32,
    prev_prepared_seal:  opt[AggregatedSeal],      # rlp:"nil": absent = 0xc0
    prev_committed_seal: opt[AggregatedSeal],      # rlp:"nil"
    round:               uint32,
    prepared_seal:       opt[AggregatedSeal],      # rlp:"nil"
    committed_seal:      opt[AggregatedSeal],      # rlp:"nil"
    gas_tip:             bigint,                   # absent on encode = 0x80; decodes to 0 (WBFT-ENC-008)
    epoch_info:          opt[EpochInfo],           # rlp:"nil"
]
AggregatedSeal = [ sealers: bytes, signature: bytes ]
EpochInfo      = [ candidates: list[Candidate], validators: list[uint32], bls_public_keys: list[bytes] ]
Candidate      = [ addr: bytes20, diligence: uint64 ]
```

[WBFT-ENC-020] `WBFTExtra` MUST be encoded as a list of exactly the ten items above, in that order, with the item types shown.
- Source: `core/types/istanbul.go:80-92`, `core/types/istanbul.go:105-119` (EncodeRLP)
- Observable: header

[WBFT-ENC-021] `decode_extra(header)` MUST be `rlp_decode(header.Extra, WBFTExtra)` with the rules of §2 and §3: exactly ten items, `rlp:"nil"` semantics for the four seal fields and `epoch_info`, `bigint` semantics for `gas_tip`, no trailing bytes after the list.
- Source: `core/types/istanbul.go:121-149` (DecodeRLP), `core/types/istanbul.go:248-258` (ExtractWBFTExtra)
- Observable: header

[WBFT-ENC-022] `encode_extra(extra)` MUST be `rlp_encode(extra)` per [WBFT-ENC-020]. For every byte string `b` accepted by `decode_extra`, `encode_extra(decode_extra(b)) = b` holds: every accepted input is canonical, and on the inputs accepted in the positions of §4.1 each empty item decodes to a value that re-encodes to the same empty item.
- Source: `core/types/istanbul.go:105-149`; checked in §9.1 (`ca808080c0c080c0c080c0` re-encodes to itself)
- Observable: header

A verifier rejects a header whose extra does not decode with `ErrInvalidExtraDataFormat` ("invalid extra data format") (`consensus/wbft/engine/engine.go:300-304`, `A-08`).

### 4.2 `vanity_data`

[WBFT-ENC-030] A verifier MUST accept any length and content of `vanity_data`; it is not interpreted.
- Source: `consensus/wbft/engine/engine.go:196-346` (no check), `core/types/istanbul.go:121-149`
- Observable: header

[WBFT-ENC-031] A proposer following the reference builds the extra of a new header from the header's pre-existing `Extra` (the miner's configured extra data, at most `MAXIMUM_EXTRA_DATA_SIZE = 32` bytes) as follows: if it is shorter than 32 bytes, the new extra starts from `vanity_data = Extra ‖ 0x00 × (32 − len(Extra))`, empty `randao_reveal`, `prev_round = round = 0`, all seals and `epoch_info` absent, `gas_tip` absent; if it is 32 bytes or longer, it is decoded as a `WBFTExtra` and the decoded fields are the starting point (header preparation fails if it does not decode). A proposer SHOULD produce a 32-byte `vanity_data`.
- Source: `consensus/wbft/engine/engine.go:1162-1182` (getExtra), `consensus/wbft/engine/apply_extra.go:38-50` (ApplyHeaderWBFTExtra), `consensus/wbft/engine/engine.go:486-550` (Prepare), `miner/worker.go:1150-1152`, `eth/backend.go:307-321`
- Observable: header

### 4.3 Other scalar fields

[WBFT-ENC-032] `prev_round` and `round` MUST be `uint32` ([WBFT-ENC-010]); a round of 2^32 or more cannot be represented and is stored modulo 2^32 (`A-01` [WBFT-TYPE-012]).
- Source: `core/types/istanbul.go:84`, `core/types/istanbul.go:87`, `consensus/wbft/engine/engine.go:153-158`
- Observable: header

[WBFT-ENC-033] `gas_tip` MUST be a `bigint` in wei. After decoding it is always present (an encoded `0x80` is the value 0). A proposer writes it in every block with `number ≥ 1` (`B-06`), and the genesis extra carries the initial gas tip (§4.7).
- Source: `core/types/istanbul.go:90`, `core/types/istanbul.go:132`, `consensus/wbft/engine/engine.go:599-605` (WriteGasTip)
- Observable: header

`randao_reveal` is an arbitrary `bytes` at the encoding layer; its length and meaning are fixed by `A-02 §7.2`. Whether `prev_*` seals and `epoch_info` must be present at a given height is specified in `A-08` and `A-04`.

### 4.4 `AggregatedSeal` and `SealerSet`

```python
def sealer_indices(sealers: bytes) -> list[int]:
    """SealerSet.GetSealers: ascending indices; bit b of byte k is index 8*k + b (b = 0 is the least significant bit)."""
    return [8 * k + b for k in range(len(sealers)) for b in range(8) if sealers[k] >> b & 1]

def make_sealer_set(indices) -> bytes:
    """SealerSet.SetSealer applied to each index: minimal length."""
    out = bytearray()
    for i in indices:
        if len(out) <= i // 8:
            out += bytes(i // 8 + 1 - len(out))
        out[i // 8] |= 1 << (i % 8)
    return bytes(out)
```

[WBFT-ENC-040] `AggregatedSeal` MUST be encoded as the two-item list `[sealers, signature]`, both `bytes`.
- Source: `core/types/istanbul.go:52-74`
- Observable: header

[WBFT-ENC-041] In `sealers`, sealer index `i` MUST be represented by bit `i mod 8` (counting from the least significant bit) of byte `i div 8`. The indices named by a bitmap are `sealer_indices(sealers)` in ascending order.
- Source: `core/types/istanbul.go:290-325` (SealerSet)
- Observable: header

[WBFT-ENC-043] `signature` is the 96-byte compressed BLS aggregate (`A-02 §5.6`); its length is not checked by the decoder. An `AggregatedSeal` that is present but has an empty `signature` (`0xc2 0x80 0x80`) and an absent one (`0xc0`) are different encodings; header verification MUST treat both as "no seal" (`A-08`).
- Source: `core/types/istanbul.go:52-74`, `consensus/wbft/engine/engine.go:390-392`, `consensus/wbft/engine/engine.go:422-425`
- Observable: header

### 4.5 `Candidate` and `EpochInfo`

[WBFT-ENC-050] `Candidate` MUST be encoded as `[addr: bytes20, diligence: uint64]`. `EpochInfo` MUST be encoded as `[candidates, validators, bls_public_keys]` with the element types of §4.1. Each element of `candidates` MUST be a non-empty `Candidate` list (an empty list `0xc0` as an element is rejected).
- Source: `core/types/istanbul.go:94-103`, `core/types/istanbul.go:204-246`
- Observable: header

[WBFT-ENC-051] Decoding MUST NOT check consistency between the three lists of `EpochInfo` (equal lengths of `validators` and `bls_public_keys`, `validators[i] < len(candidates)`, uniqueness, key lengths). Those properties are required of a valid epoch block by `A-04` / `B-06` and checked there.
- Source: `core/types/istanbul.go:213-225`, `consensus/wbft/engine/engine.go:1212-1263` (verifyEpoch)
- Observable: header

### 4.6 Summary of decode failures of `WBFTExtra`

| Input defect | Result |
|---|---|
| not an RLP list, non-canonical RLP, or trailing bytes | fail |
| fewer or more than 10 items | fail |
| `prev_round` / `round` longer than 4 bytes, leading zero, or `0x00` | fail |
| a seal field or `epoch_info` equal to `0x80` | fail ("wrong kind of empty value") |
| `gas_tip` equal to `0xc0` | fail ("expected input string or byte") |
| `AggregatedSeal` not a 2-item list; `Candidate` not a 2-item list; `EpochInfo` not a 3-item list | fail |
| `Candidate.addr` not 20 bytes; `diligence` longer than 8 bytes | fail |
| any length of `vanity_data`, `randao_reveal`, `sealers`, `signature`, a BLS key | accepted by decoding |

Source: computed with the reference decoder, §9.1.

### 4.7 Genesis extra

[WBFT-ENC-060] The genesis header's `Extra` MUST be `encode_extra(WBFTExtra{ vanity_data = empty, randao_reveal = empty, prev_round = 0, seals absent, round = 0, gas_tip = G, epoch_info = E })`, where `E = EpochInfo{ candidates = [(init.validators[i], DEFAULT_DILIGENCE)], validators = [0, 1, …, k−1], bls_public_keys = [init.blsPublicKeys[i]] }` in the order of `init.validators`, and `G` is `govValidator.params.gasTip` parsed as a base-10 integer with optional sign (Go `big.Int.SetString(s, 10)`), or `INITIAL_GAS_TIP` if that parameter is absent, empty or unparseable; a negative value makes extra-data construction fail (`rlp: cannot encode negative big.Int`). Genesis construction is specified in `B-02`; a present empty or non-decimal value makes it fail (`B-02` SNET-GEN-010), so on a valid genesis only the absent case uses `INITIAL_GAS_TIP`.
- Source: `consensus/wbft/config.go:227-281` (CreateInitialExtraData, CreateInitialEpochInfo)
- Observable: header

---

## 5. Header and block

### 5.1 Header

[WBFT-ENC-070] The header MUST be encoded as the go-ethereum header list: `[ParentHash: bytes32, UncleHash: bytes32, Coinbase: bytes20, Root: bytes32, TxHash: bytes32, ReceiptHash: bytes32, Bloom: bytes256, Difficulty: bigint, Number: bigint, GasLimit: uint64, GasUsed: uint64, Time: uint64, Extra: bytes, MixDigest: bytes32, Nonce: bytes8]`, followed by the optional fields `BaseFee: bigint, WithdrawalsHash: bytes32, BlobGasUsed: uint64, ExcessBlobGas: uint64, ParentBeaconRoot: bytes32`, where an optional field is emitted if it or any later optional field is present, and a missing optional field that must be emitted is written as `0x80`.
- Source: `core/types/block.go:67-97` (Header), `core/types/gen_header_rlp.go:8-89` (EncodeRLP)
- Observable: header

WBFT headers carry `BaseFee` (London is active from block 0 in the StableNet presets) and none of the later optional fields (`A-08` rejects them), so a WBFT header is a 16-item list.

### 5.2 Block (proposal)

[WBFT-ENC-071] A `Proposal` MUST be encoded as the go-ethereum block list `[header, transactions: list, uncles: list[header]]`, with a fourth item `withdrawals` only if the block has a withdrawals list. The decoder accepts three or four items.
- Source: `core/types/block.go:230-235` (extblock), `core/types/block.go:331-350`
- Observable: network

---

## 6. Block hash

### 6.1 Filtered header

```python
def filtered_header(header, round: uint32) -> Header | None:
    """WBFTFilteredHeaderWithRound."""
    extra = decode_extra(header)          # None if it fails
    if extra is None:
        return None
    extra.prepared_seal  = None
    extra.committed_seal = None
    extra.round          = round
    h = copy(header)
    h.Extra = encode_extra(extra)
    return h
```

[WBFT-ENC-080] `filtered_header(header, round)` MUST be the header with `Extra` replaced by the re-encoding of its decoded `WBFTExtra` in which `prepared_seal` and `committed_seal` are absent and `round` is replaced by `round`. Every other header field and every other extra field (including `vanity_data`, `randao_reveal`, `prev_round`, `prev_prepared_seal`, `prev_committed_seal`, `gas_tip`, `epoch_info`) MUST be kept unchanged. If the extra does not decode, `filtered_header` is undefined.
- Source: `core/types/istanbul.go:260-288`
- Observable: header

### 6.2 Hash with round

[WBFT-ENC-081] `hash_with_round(header, round)` MUST be `keccak256(rlp_encode(filtered_header(header, round)))`. When the extra does not decode, the reference returns `keccak256(0xc0)` (the RLP of an absent header), regardless of the other fields; this value MUST NOT be used as a seal target ([WBFT-CRYPTO-044]). `hash_with_round` does not depend on `Difficulty`.
- Source: `core/types/block.go:169-172` (WBFTHashWithRoundNumber), `core/types/hashing.go:58-64` (rlpHash), `rlp/encode.go:413-432`
- Observable: header, network

### 6.3 `block_hash`

```python
def block_hash(header) -> Hash:
    """Header.Hash."""
    if header.Difficulty == 1:
        fh = filtered_header(header, 0)
        if fh is not None:
            return keccak256(rlp_encode(fh))
    return keccak256(rlp_encode(header))
```

[WBFT-ENC-082] `block_hash(header)` MUST be `keccak256(rlp_encode(filtered_header(header, 0)))` when `header.Difficulty = 1` and the extra decodes, and `keccak256(rlp_encode(header))` otherwise. This value is the block hash used everywhere (`ParentHash` of the child, block lookup, RPC, the consensus digest).
- Source: `core/types/block.go:119-131` (Header.Hash), `core/types/block.go:507-514` (Block.Hash)
- Observable: header, rpc

[WBFT-ENC-084] For a WBFT header whose extra does not decode, `block_hash` falls back to `keccak256(rlp_encode(header))` (the Ethereum rule). Such a header is invalid ([WBFT-ENC-021], `A-08`), but a node SHOULD still compute its hash by this rule, for example when it looks the header up or reports it as a bad block.
- Source: `core/types/block.go:121-131`; checked in §9.3
- Observable: header

---

## 7. Re-encoding

The reference decodes messages and extras into structures and re-encodes them when it needs bytes again (to compute a signing payload, to hash a header, to relay a message taken from its backlog). Re-encoding is the identity on canonical input except in the cases below.

[WBFT-ENC-090] An implementation MUST compute the message signing payload (§8.1) and `filtered_header` from the decoded field values, not from the received bytes. In the cases where decode-then-encode is not the identity (listed in this section), the reference's result is the re-encoded form.
- Source: `consensus/wbft/core/handler.go:268-317` (verifySignatures uses EncodePayloadForSigning), `core/types/istanbul.go:280-285`
- Observable: network, header

| Structure | Received | Re-encoded |
|---|---|---|
| ROUND-CHANGE `prepared_block` / `justification` | `0x80`, or a single byte `0x00 … 0x7f` (accepted as absent, [WBFT-MSG-032]) | `0xc0` |
| ROUND-CHANGE `prepared` | `[pr, 0x00…00]` (accepted) | `[]` (the signing payload no longer matches what was signed) |
| PRE-PREPARE justification lists | as received | lists re-encoded item by item; identical for canonical items, except that a round-change item with `prepared = [pr, 0x00…00]` is re-encoded with `prepared = []` (as in the row above) |

---

## 8. Consensus messages

### 8.1 Common structure

Every consensus message has the fields `sequence: bigint`, `round: bigint` and a 65-byte ECDSA `signature`. The message code (`A-01 §4.1`) and the sender address are not part of the encoded message: the code is the devp2p message code (`A-07`), and the sender (`source`) is recovered from the signature.

```python
def message_signing_payload(msg) -> bytes:
    """EncodePayloadForSigning."""
    return rlp_encode([msg.code, signed_fields(msg)])     # code as uint64 (0x12 .. 0x15)

def message_signature(node_key, msg) -> bytes65:
    return ecdsa_sign(node_key, message_signing_payload(msg))       # A-02 §3.2
```

[WBFT-MSG-001] The payload of a devp2p message with code `c ∈ {0x12, 0x13, 0x14, 0x15}` MUST be exactly `rlp_encode(msg)` of the message type for `c`, without further wrapping.
- Source: `consensus/wbft/backend/backend.go:202-206`, `eth/protocols/eth/peer.go:519-525` (SendWithNoEncoding), `consensus/wbft/backend/handler.go:54-67`
- Observable: network

[WBFT-MSG-002] The signature of a message MUST be `ecdsa_sign(node_key, message_signing_payload(msg))`, where `message_signing_payload(msg) = rlp_encode([code, signed_fields])` and `signed_fields` is the message-specific list given in §8.2 – §8.5. Because the code is the first item, a signature for one message type is never valid for another.
- Source: `consensus/wbft/messages/prepare.go:61-67`, `consensus/wbft/messages/commit.go:49-55`, `consensus/wbft/messages/roundchange.go:170-182`, `consensus/wbft/messages/preprepare.go:50-56`, `consensus/wbft/core/prepare.go:52-62`
- Observable: network

[WBFT-MSG-003] A receiver MUST determine the sender of a message as `ecdsa_recover_address(message_signing_payload(decoded_msg), signature)` ([WBFT-CRYPTO-013], [WBFT-ENC-090]), and MUST do the same for every signed payload embedded in the message (the PREPAREs in a ROUND-CHANGE justification; the round-change payloads and PREPAREs in a PRE-PREPARE justification). Validator-set membership is then checked per `A-05`.
- Source: `consensus/wbft/core/handler.go:268-317` (verifySignatures)
- Observable: network

[WBFT-MSG-004] A receiver MUST decode the payload with the decoder for the message code; a code outside `{0x12, 0x13, 0x14, 0x15}` MUST NOT be decoded as a consensus message (the reference reports `invalid message event code`; `messages.Decode` itself returns `ErrInvalidMessage`).
- Source: `consensus/wbft/core/handler.go:188-201`, `consensus/wbft/messages/decode.go:27-59`
- Observable: network, log

### 8.2 PREPARE (`0x13`)

```
PREPARE        = [ [sequence: bigint, round: bigint, digest: bytes32, prepare_seal: bytes], signature: bytes ]
signed_fields  =   [sequence, round, digest, prepare_seal]
signing payload = rlp_encode([0x13, [sequence, round, digest, prepare_seal]])
```

[WBFT-MSG-010] A PREPARE MUST be encoded as shown. `digest` is `block_hash` of the proposal (`A-05`); `prepare_seal` is the 96-byte prepare seal ([WBFT-CRYPTO-042]).
- Source: `consensus/wbft/messages/prepare.go:31-36`, `consensus/wbft/messages/prepare.go:61-80`
- Observable: network

[WBFT-MSG-011] A PREPARE decoder MUST reject any input that is not a two-item list whose first item is a four-item list with the item types shown ([WBFT-ENC-004], [WBFT-ENC-005], [WBFT-ENC-011]). The lengths of `prepare_seal` and `signature` are not checked by the decoder. The reference reports a PREPARE decode failure with `ErrFailedDecodeCommit` ("failed to decode COMMIT message").
- Source: `consensus/wbft/messages/prepare.go:82-102`, `consensus/wbft/messages/decode.go:36-42`
- Observable: log

### 8.3 COMMIT (`0x14`)

```
COMMIT         = [ [sequence: bigint, round: bigint, digest: bytes32, commit_seal: bytes], signature: bytes ]
signing payload = rlp_encode([0x14, [sequence, round, digest, commit_seal]])
```

[WBFT-MSG-020] A COMMIT MUST be encoded as shown; `commit_seal` is the 96-byte commit seal ([WBFT-CRYPTO-042]). Its decoder has the same rules as [WBFT-MSG-011] and reports failures with `ErrFailedDecodeCommit`.
- Source: `consensus/wbft/messages/commit.go:30-83`, `consensus/wbft/messages/decode.go:43-49`
- Observable: network

The wire formats of PREPARE and COMMIT are identical; only the code and the seal type inside the seal differ. A PREPARE payload delivered with code `0x14` decodes as a COMMIT and fails the signature check ([WBFT-MSG-002]).

### 8.4 ROUND-CHANGE (`0x15`)

```
rc_payload     = [ sequence: bigint, round: bigint, prepared ]
prepared       = [] | [ prepared_round: bigint, prepared_digest: bytes32 ]
SignedRoundChangePayload = [ rc_payload, signature: bytes ]
ROUND-CHANGE   = [ SignedRoundChangePayload, prepared_block, justification ]
prepared_block = <empty item> | Block (§5.2)
justification  = <empty item> | list[PREPARE]
signing payload = rlp_encode([0x15, rc_payload])
```

[WBFT-MSG-030] An encoder MUST write `prepared = [prepared_round, prepared_digest]` if and only if `prepared_round` is present and `prepared_digest` is not 32 zero bytes, and `prepared = []` otherwise. In particular a prepared round of 0 with a non-zero digest is written `[0x80, digest]`.
- Source: `consensus/wbft/messages/roundchange.go:158-168` (encodePayloadInternal)
- Observable: network

[WBFT-MSG-031] A ROUND-CHANGE MUST be encoded as the three-item list shown, with an absent `prepared_block` and an absent or empty `justification` encoded as `0xc0`. When the sender has a prepared block, `prepared_digest` MUST equal `block_hash(prepared_block.header)`.
- Source: `consensus/wbft/messages/roundchange.go:43-62` (NewRoundChange), `consensus/wbft/messages/roundchange.go:184-200` (EncodeRLP)
- Observable: network

[WBFT-MSG-032] A ROUND-CHANGE decoder MUST, in this order:
1. read the outer list and the `SignedRoundChangePayload` list;
2. decode `rc_payload` as a list of `sequence: bigint`, `round: bigint` and `prepared`, where `prepared` MUST be a list of either zero items or exactly two items `(prepared_round: bigint, prepared_digest: bytes32)`; any other number of items fails;
3. decode `signature: bytes` and require the end of the `SignedRoundChangePayload` list;
4. read the next item: if it is the empty string, the empty list, or a single byte `0x00 … 0x7f`, treat `prepared_block` as absent; otherwise decode it as a Block and fail unless `block_hash(prepared_block.header) = prepared_digest`;
5. read the next item: if it is empty in the same sense, treat `justification` as absent; otherwise decode it as `list[PREPARE]`;
6. require the end of the outer list (a missing item or an extra item fails).

The reference reports every failure with `ErrFailedDecodeRoundChange` ("failed to decode ROUND-CHANGE message"); the digest mismatch of step 4 is logged as `WBFT: Error m.PreparedDigest.Hash() != digest`.
- Source: `consensus/wbft/messages/roundchange.go:202-317` (RoundChange.DecodeRLP), `consensus/wbft/messages/decode.go:50-56`; edge cases checked in §9.6
- Observable: network, log

[WBFT-MSG-033] A decoder MUST NOT require `prepared_block` to be present when `prepared_digest` is non-zero, and MUST NOT require the `prepared` list to be non-empty when `prepared_block` is absent; the consistency of prepared round, digest, block and justification is checked by `A-05`.
- Source: `consensus/wbft/messages/roundchange.go:239-291`
- Observable: network

[WBFT-MSG-034] A `SignedRoundChangePayload` on its own (as used in a PRE-PREPARE justification) MUST be encoded as `[rc_payload, signature]` and decoded by steps 1 – 3 of [WBFT-MSG-032] applied to a two-item list.
- Source: `consensus/wbft/messages/roundchange.go:75-156`
- Observable: network

### 8.5 PRE-PREPARE (`0x12`)

```
PRE-PREPARE    = [ [ [sequence: bigint, round: bigint, proposal: Block], signature: bytes ],
                   [ justification_round_changes: list[SignedRoundChangePayload],
                     justification_prepares:      list[PREPARE] ] ]
signing payload = rlp_encode([0x12, [sequence, round, proposal]])
```

[WBFT-MSG-040] A PRE-PREPARE MUST be encoded as shown. The signing payload contains the complete RLP encoding of the proposal block; the justification lists are not covered by the PRE-PREPARE signature (each item carries its own signature, [WBFT-MSG-003]). Empty justification lists are encoded as `0xc0`.
- Source: `consensus/wbft/messages/preprepare.go:32-71`
- Observable: network

[WBFT-MSG-041] A PRE-PREPARE decoder MUST reject input that does not match the schema, including an empty or non-block `proposal` (a proposal encoded as `0xc0` fails), a justification that is not a two-item list, a round-change item that does not decode per [WBFT-MSG-034] (each item is delimited as one RLP item and decoded on its own; an item list with a missing or extra element fails), and a PREPARE item that does not decode per [WBFT-MSG-011]. The reference reports every failure with `ErrFailedDecodePreprepare` ("failed to decode PRE-PREPARE message").
- Source: `consensus/wbft/messages/preprepare.go:73-110`, `consensus/wbft/messages/decode.go:29-35`; nil-proposal case checked in §9.6
- Observable: network, log

The check `sequence = proposal.Number` is not a decoding rule; it is made by `A-05`.

### 8.6 Legacy code `0x11`

[WBFT-MSG-050] On receipt of a message with code `ISTANBUL_MSG = 0x11`, a node MUST decode the devp2p payload as one RLP byte string `data`, ignoring any bytes that follow it; if that fails, the message is rejected with `errDecodeFailed` (consequences in `A-07`). Otherwise the node computes the deduplication key over `data` ([WBFT-MSG-060]) and, unless that key is already known (`A-07`), delivers `(0x11, data)` to the consensus core, which MUST NOT process it ([WBFT-MSG-004]). A `0x11` message is never relayed.
- Source: `consensus/wbft/backend/handler.go:54-67` (decode), `p2p/message.go:56-62` (Msg.Decode, no trailing-byte check), `consensus/wbft/backend/handler.go:70-101` (HandleMsg), `consensus/wbft/core/handler.go:128-135`, `consensus/wbft/core/handler.go:188-194`
- Observable: network, log

[WBFT-MSG-051] A node MUST send each consensus message with its own code `0x12 … 0x15`. (The reference would send any other code as `0x11` with the payload not wrapped in an RLP string; it never produces such a message.)
- Source: `consensus/wbft/backend/backend.go:202-206`
- Observable: network

### 8.7 `View` (informative)

The reference defines an RLP encoding of `View` as `[round, sequence]` (round first) (`consensus/wbft/types.go:68-85`). No message of this chapter embeds a `View`; messages carry `sequence` before `round`. The encoding is recorded here only to prevent confusion.

### 8.8 Deduplication key

```python
def dedup_key(payload: bytes) -> Hash:
    """wbft.RLPHash(data): keccak256 of the RLP string encoding of the payload bytes."""
    return keccak256(rlp_encode(payload))           # payload encoded as an RLP *string*
```

[WBFT-MSG-060] The key under which a node records a consensus message as seen MUST be `dedup_key(payload) = keccak256(rlp_encode_string(payload))`, computed over the message payload bytes only (for `0x11`, over the inner `data`). The key does not include the message code. The same key is used for received and for sent messages.
- Source: `consensus/wbft/utils.go:31-36` (RLPHash), `consensus/wbft/backend/handler.go:66`, `consensus/wbft/backend/backend.go:177`
- Observable: network

For a payload `p` of length `L`, `rlp_encode_string(p)` is `p` itself if `L = 1` and `p[0] < 0x80`, `0x80+L ‖ p` if `L ≤ 55`, and `0xb7+len(be_min(L)) ‖ be_min(L) ‖ p` otherwise. Consequently `dedup_key` of a message equals `keccak256` of the devp2p payload of the same message sent with code `0x11`.

How the key is used (per-peer and global caches, when a message is marked as seen) is specified in `A-07`.

### 8.9 Summary of decode results

| Code | Decoder | Failure reported by the reference |
|---|---|---|
| `0x12` | [WBFT-MSG-041] | `ErrFailedDecodePreprepare` |
| `0x13` | [WBFT-MSG-011] | `ErrFailedDecodeCommit` (sic) |
| `0x14` | [WBFT-MSG-020] | `ErrFailedDecodeCommit` |
| `0x15` | [WBFT-MSG-032] | `ErrFailedDecodeRoundChange` |
| `0x11` | [WBFT-MSG-050] | decoded to bytes, then rejected by the core |
| other | not a consensus message | `ErrInvalidMessage` / `invalid message event code` |

A message that fails to decode is dropped and is not relayed; it is logged at level Error (`WBFT: invalid message`, `consensus/wbft/core/handler.go:197-201`). The error value is not sent to the peer.

---

## 9. Worked examples

All hex strings below were produced by running the reference encoders and decoders from a Go program linked against go-stablenet `740526d03` (see `A-02 §8`); the block hash of §9.3 and the deduplication key of §9.6 were recomputed independently in Python and match. Keys and header `H` are those of `A-02 §8`.

### 9.1 Absent values and decode failures of `WBFTExtra`

| Input (hex) | Meaning | Result |
|---|---|---|
| `ca808080c0c080c0c080c0` | `WBFTExtra{}` (everything absent/zero) | decodes; `gas_tip` = 0 (present); seals and `epoch_info` absent; `vanity_data` empty; re-encodes to the same bytes |
| `cc808080c0c080c28080c080c0` | `prepared_seal` present with empty `sealers` and `signature` | decodes (present, empty) |
| `ca808080808080c0c080c0` | `0x80` in `prev_prepared_seal` | fails: wrong kind of empty value (got String, want List) |
| `ca808080c0c080c0c0c0c0` | `0xc0` in `gas_tip` | fails: expected input string or byte for `*big.Int` |
| `ca808000c0c080c0c080c0` | `prev_round` = `0x00` | fails: non-canonical integer |
| `ce8080850100000000c0c080c0c080c0` | `prev_round` = 2^32 | fails: input string too long for uint32 |
| `ca808080c0c080c0c000c0` | `gas_tip` = `0x00` | fails: non-canonical integer |
| `cb808080c0c080c0c08100c0` | `gas_tip` = `0x8100` | fails: non-canonical size information |
| `c9808080c0c080c0c080` | 9 items | fails: too few elements |
| `cb808080c0c080c0c080c080` | 11 items | fails: input list has too many elements |
| `ca808080c0c080c0c080c000` | valid list followed by `00` | fails: input contains more than one value |

### 9.2 `SealerSet`

| Sealer indices | `sealers` bytes | RLP item | `sealer_indices` |
|---|---|---|---|
| {} | (empty) | `80` | [] |
| {0} | `01` | `01` | [0] |
| {0, 1, 2} | `07` | `07` | [0, 1, 2] |
| {1, 3} | `0a` | `0a` | [1, 3] |
| {0, 8} | `0101` | `820101` | [0, 8] |
| {9} | `0002` | `820002` | [9] |
| {15} | `0080` | `820080` | [15] |
| {16} | `000001` | `83000001` | [16] |

### 9.3 Extra, header and block hash of `H`

Extra of the proposal `H` (before sealing), 116 bytes:

```
f872                                                              list, 114 bytes
  a0 0000000000000000000000000000000000000000000000000000000000000000   vanity_data (32 bytes)
  b841 6243139f0e6b881248e468a6566eb5ed9c278bb1b54d8bd46ebe50055bcb92a5
       435af24253453da96c927a47f9d23f4c19eb2f5b53ead248e93d0182be94c448
       00                                                            randao_reveal (65 bytes, V = 0)
  80                                                                prev_round = 0
  c0                                                                prev_prepared_seal absent
  c0                                                                prev_committed_seal absent
  80                                                                round = 0
  c0                                                                prepared_seal absent
  c0                                                                committed_seal absent
  86 191a20322000                                                   gas_tip = 27600000000000
  c0                                                                epoch_info absent
```

RLP of the header `H` (16 items, 628 bytes):

```
f90271
a0 1111111111111111111111111111111111111111111111111111111111111111   ParentHash
a0 1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347   UncleHash
94 376ab524a47492d1c8222c67c0cff8d5fec34112                           Coinbase
a0 2222222222222222222222222222222222222222222222222222222222222222   Root
a0 56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421   TxHash
a0 56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421   ReceiptHash
b90100 00…00 (256 bytes)                                             Bloom
01                                                                   Difficulty = 1
02                                                                   Number = 2
84 06422c40                                                          GasLimit = 105000000
80                                                                   GasUsed = 0
84 6553f102                                                          Time = 1700000002
b874 f872…c0 (the 116-byte extra above)                              Extra
a0 0000000000000000000000000000000000000000000000000000000000000000   MixDigest
88 0000000000000000                                                  Nonce
86 12309ce54000                                                      BaseFee = 20000000000000
```

`block_hash(H) = keccak256(header RLP) = 6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e` (the filtered header of an unsealed round-0 proposal is the header itself).

Extra of `H` after `CommitHeader` with the seals of validators 0, 1, 2 at round 0 (317 bytes):

```
f9013a
  a0 00…00                  vanity_data
  b841 6243…c448 00         randao_reveal (unchanged)
  80 c0 c0                  prev_round, prev seals (unchanged)
  80                        round = 0
  f863 07 b860 895488e1…88164359     prepared_seal: sealers = 07, 96-byte aggregate (A-02 §8.4)
  f863 07 b860 b1d4051b…5fb341       committed_seal
  86 191a20322000           gas_tip
  c0                        epoch_info
```

The block hash of the committed header is again `6284d1a2…0a1e`; `filtered_header(committed, 0).Extra` equals the proposal extra byte for byte.

Other checks on the same header:

| Change to `H` | Hash |
|---|---|
| `Difficulty = 2` | `170754e9c004b76a6670c61ac483945b4e247ab089331ecd3c168421340cc9f3` = `keccak256(rlp(header))` (Ethereum rule) |
| `Extra` with a trailing `00` byte (undecodable), `Difficulty = 1` | `f54bd294c2e969aeae6134f0733e394379523e4f0f38136ab48a267cb59fd8c4` = `keccak256(rlp(header))` ([WBFT-ENC-084]) |
| `Extra` empty, `Difficulty = 1` (other fields zero) | equals `keccak256(rlp(header))` |

### 9.4 `EpochInfo` and genesis extra

`Candidate(key0 address, DEFAULT_DILIGENCE)`: `d9 94 376ab524a47492d1c8222c67c0cff8d5fec34112 83 1cfde0` (`1_900_000 = 0x1cfde0`).

`EpochInfo{}` (all lists empty): `c3c0c0c0`.

Genesis extra for validators key0 … key3 with `gas_tip = INITIAL_GAS_TIP` (330 bytes):

```
f90147
  80 80 80 c0 c0 80 c0 c0          vanity, randao_reveal, prev_round, prev seals, round, seals: empty/absent
  86 191a20322000                  gas_tip = 27600000000000
  f90135                           epoch_info
    f868                             candidates (4 × 26 bytes)
      d994 376ab524a47492d1c8222c67c0cff8d5fec34112 831cfde0
      d994 1111f6010b9b757b30e00a7d519cd98094b7a610 831cfde0
      d994 503578b72bf5c96ab41508e2ac70303691dfcddb 831cfde0
      d994 084ac57c61cd5dd6244eb0b4c42ca72d073a6c14 831cfde0
    c4 80 01 02 03                   validators = [0, 1, 2, 3]
    f8c4                             bls_public_keys (4 × 49 bytes)
      b0 8eaaca1bbb29c07cb653b346e8434bb6b3bc63a4bb7c8dfea5c46bae2473646fe8041e3ff81da70bba7effda935a4576
      b0 8a8f913a1bac306b3b1661fc78ce376a650341ce314f7c0f615fc7abf3722eb6c256046483c266d3e03ce82edbed13a3
      b0 976a4cd50f07ad6fb3fc0a7a27a803212be7cf84b779d9a5562af81316187dec8ee319a0ce421c020dc1bf1faa494abe
      b0 aa6b0cda839b65a5e2e556484ccb3d67fac59c1cc66ab2bf1b9160ecee30a0deb6dbbad04836ca99ceb74c225a0c184e
```

### 9.5 View

`View{round = 1, sequence = 2}` → `c20102` (round first; not used on the wire).

### 9.6 Messages

All for sequence 2, digest `block_hash(H) = 6284d1a2…0a1e`.

**PREPARE** by key 1, round 0, `prepare_seal` = prepare seal 1 of `A-02 §8.4`:

```
signing payload (138 bytes):
f888 13 f885 02 80 a0 6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e
          b860 b9ed2c8ca8ef593fa5b1749082a5bc211460b913a9c5a3eec8770d5acff6bf6501dfa61735f6cdd045e5e75a9489801a
               0bca5554be5abae320e5d883dae61838ad5765c1427cb4a5672dabd5649969760505ff7aaee60aa2e2e5a623af1726a1
signature = ecdsa_sign(key1, payload):
210a2b4c90e9be64976a7b81aab05aad51146d6abc3bd0061f7a35af8290e6ae31038a77129d7441c68ade4ea87372a481e343a794b711f3cb8ff5b4d6b7bd5501
wire (204 bytes):
f8ca f885 02 80 a0 6284…0a1e b860 b9ed…26a1  b841 210a…bd55 01
dedup_key = keccak256(b8cc ‖ wire) = 7e20c27fc270ed22dfe8b8ee9dbc97f2b9672cf8fd6dc360fbb0321e7f4d4aec
keccak256(wire)                    = 849415fd2fa5eac21fe4db0460f5d78f70ec241f027a15a41f9fbd3ceaa97d16   (not the dedup key)
recovered sender = 0x1111F6010b9b757b30e00A7D519cd98094b7A610
```

Full wire hex: `f8caf8850280a06284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1eb860b9ed2c8ca8ef593fa5b1749082a5bc211460b913a9c5a3eec8770d5acff6bf6501dfa61735f6cdd045e5e75a9489801a0bca5554be5abae320e5d883dae61838ad5765c1427cb4a5672dabd5649969760505ff7aaee60aa2e2e5a623af1726a1b841210a2b4c90e9be64976a7b81aab05aad51146d6abc3bd0061f7a35af8290e6ae31038a77129d7441c68ade4ea87372a481e343a794b711f3cb8ff5b4d6b7bd5501`

**COMMIT** by key 2, round 0, `commit_seal` = commit seal 2 of `A-02 §8.4`:

```
signing payload: f888 14 f885 02 80 a0 6284…0a1e b860 a0b37939…23164
wire: f8caf8850280a06284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1eb860a0b379390308e1abca3dd3fda9b8ca2bb97bec9d21a273d715f98e0d9571ca25872c717ec95185377bf4ff56ad37a273163b0429116df980359598028a79e7dbadec4e9580d8a6111e2106c2e8081de928cd898e1db0757fb65f98a264323164b84117e43ecb53f6a2b39b150e4b3e9604f74f16a6540b9bb65383fd98e401323baf27e323bf21ecd5e0df8503a5eeea973e7e92f96a0271d4edf832163963ead9a101
```

**ROUND-CHANGE** by key 3 for round 1, nothing prepared:

```
signing payload: c5 15 c3 02 01 c0
wire: f84b f847 c30201c0 b841 789095e9d1a82b67dccb9490c515938a6e26fa3af3ba7484f287604b7e32e5c931fea427d585be99670a799d12ae59c22b1eb5bb337ceef815aa3108912417dc00 c0 c0
```

**ROUND-CHANGE** by key 3 for round 1, prepared at round 0 with block `H` and the PREPARE above as justification:

```
signing payload: e7 15 e5 02 01 e2 80 a0 6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e
wire: 949 bytes = [ [rc_payload, sig], block(H, no txs), [PREPARE] ]; decodes without error
```

ROUND-CHANGE decoding edge cases (signed payload `c6 c30201c0 81aa`, i.e. `rc_payload = [2, 1, []]`, signature `aa`):

| Wire | Result |
|---|---|
| `c9 c6c30201c081aa c0 c0` | accepted; re-encodes to itself |
| `c9 c6c30201c081aa 80 80` | accepted (both absent); re-encodes with `c0 c0` |
| `c9 c6c30201c081aa 05 00` | accepted (single bytes treated as absent); re-encodes with `c0 c0` |
| `c8 c6c30201c081aa c0` | fails (missing third item) |
| `ca c6c30201c081aa c0 c0 c0` | fails (extra item) |
| `prepared = c180` (one item) | fails |
| `prepared = [0, 0x00…00, 1]` (three items) | fails |
| `prepared = [0, 0x00…00]` | accepted; signing payload re-encodes as `c515c30201c0` ([WBFT-ENC-090]) |
| `prepared = [1, 0x00…01]` | accepted; signing payload `e715e50201e201a0…01` |
| prepared block present with `prepared_digest ≠ block_hash` | fails (`ErrFailedDecodeRoundChange`) |

**PRE-PREPARE** by key 0 for `(2, 0)` with proposal = block `H` without transactions and empty justification (714 bytes):

```
f902c7
  f902c1                       [ [seq, round, proposal], signature ]
    f9027b 02 80 f90276 <header H: f90271…> c0 c0      signed fields: 2, 0, block = [header, [], []]
    b841 3a5f2bea6f18e89d704efb5ac54790314b24bdd0ef1121de7ad5ec7f68f1199f7da8d2981c15a295929cdd93792580dd29076e34f0e25b8f1b154654c57b13df01
  c2 c0 c0                     justification: no round changes, no prepares
signing payload (642 bytes) starts f9027f 12 f9027b 02 80 f90276 f90271 a0…
```

A PRE-PREPARE with an absent proposal encodes as `c9 c5 c3 02 80 c0 80 c2 c0 c0` and fails to decode (`ErrFailedDecodePreprepare`).
