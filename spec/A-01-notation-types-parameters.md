# A-01. Notation, primitive types, constants and configuration parameters

- Status: draft
- Areas: `TYPE`, `PARAM`
- Reference implementation: go-stablenet `740526d03`. All `Source:` lines are relative to the repository root at that commit.

This chapter fixes the vocabulary used by every other chapter: the glossary, the primitive types with their exact sizes and ranges, how a value is represented on the wire and in the block header when the two differ, the protocol constants, and the consensus configuration `Config` together with the function `config_at(number)` that yields the configuration in force at a given height. The encodings themselves are specified in `A-03`; the cryptographic functions in `A-02`.

---

## 1. Conventions used in this chapter

The requirement language, requirement identifiers, `Source:` and `Observable:` tags follow `README.md §2`. Pseudocode follows `README.md §2.4`. In addition:

- `uint8`, `uint32`, `uint64` are unsigned integers of that width. Arithmetic on them wraps modulo 2^width only where the text says so.
- `bigint` is an unbounded non-negative integer. The reference implementation holds such values in `*big.Int`; negative values never arise from decoding (`A-03 §3`).
- `bytes` is a byte string of any length, `bytesN` a byte string of exactly `N` bytes.
- `x[a:b]` is the byte slice from offset `a` (inclusive) to `b` (exclusive).
- `‖` is byte-string concatenation.
- `be_min(x)` is the minimal big-endian byte representation of a `bigint`: no leading zero byte, and the empty string for zero. It is what Go's `big.Int.Bytes()` returns.

---

## 2. Glossary

Terms in this table are used with exactly this meaning in every chapter.

| Term | Meaning |
|---|---|
| validator | An account that is a member of the validator set for a height and may send consensus messages and seals for it (`A-04`). Identified by its `Address` and its `BLSPublicKey`. |
| candidate | An account listed in `EpochInfo.candidates` from which the validators of the next epoch are chosen (`A-04`). Every validator is a candidate; not every candidate is a validator. |
| node key | The secp256k1 private key of a node. It signs consensus messages and randao reveals, and the node's BLS key is derived from it (`A-02 §5.1`). |
| proposer | The validator that is entitled to send the PRE-PREPARE for a view (`A-04`). A block it builds carries its address in `Header.Coinbase`. |
| sequence | The height (block number) being agreed on. Called *number* in header contexts. |
| round | A counter inside a sequence, starting at 0, incremented by round changes (`A-05`). |
| view | The pair `(sequence, round)`. Views are ordered by sequence first, then round. |
| proposal | A full block (header and body) proposed for a view. |
| digest | `block_hash(proposal.header)` (`A-03 §6`): the header hash with the current-block seals removed and the round set to 0. |
| seal | A 96-byte BLS signature by one validator over `seal_data(header, round, seal_type)` (`A-02 §6`). |
| prepare seal / commit seal | A seal with `seal_type = PREPARE_SEAL` / `COMMIT_SEAL`. Carried in PREPARE / COMMIT messages. |
| aggregated seal | The BLS aggregate of the seals of a set of validators together with a bitmap (`SealerSet`) of their indices (`A-03 §4.4`). |
| sealer index | The position of a validator in the ordered validator set of the height whose block is sealed (`A-04`). Bit `i` of a `SealerSet` refers to sealer index `i`. |
| quorum | The minimum number of distinct validators whose messages or seals are required; `quorum_size(n)` is defined in `A-04`. |
| extra seals | Seals for a decided block that arrive after the quorum was reached; they are merged into the next block's `prev_prepared_seal` / `prev_committed_seal` (`A-05`, `A-08`). |
| previous-block seals | The fields `prev_round`, `prev_prepared_seal`, `prev_committed_seal` of `WBFTExtra`: the round and aggregated seals of the parent block as held by the proposer of this block (the current-block seals are node-local, `A-08` WBFT-HDR-053), possibly extended with extra seals. Once written, they are covered by the block hash and identical on every node. |
| epoch | A range of consecutive heights during which the validator set is fixed (`A-04`). |
| epoch block | The block whose `WBFTExtra.epoch_info` is non-empty; it carries the candidates, validators and BLS keys for the next epoch (`A-04`). Epoch block `e` is the last block of the epoch it closes (it is itself sealed by the previous set); its `EpochInfo` applies from block `e + 1`. Block 0 is an epoch block. |
| diligence | A per-candidate score in units of 10^-6, between 0 and `2 · DILIGENCE_DENOMINATOR` (`A-04`). |
| randao reveal | The node-key ECDSA signature over `randao_data(chain_id, number)` stored in `WBFTExtra.randao_reveal` (`A-02 §7`). |
| randao mix | The value stored in `Header.MixDigest`: `randao_mix(parent.MixDigest, reveal)` (`A-02 §7`). |
| gas tip | The governance-agreed priority fee in wei, stored in `WBFTExtra.gas_tip` of every block (`B-06`, `B-07`). |
| justification | The ROUND-CHANGE payloads and PREPARE messages attached to a PRE-PREPARE (or the PREPAREs attached to a ROUND-CHANGE) that prove the proposal may be sent in that round (`A-05`). |
| legacy message | A message received on code `ISTANBUL_MSG = 0x11` (`A-03 §8.6`). |
| transition | An entry of `ChainConfig.Transitions` that changes consensus parameters from a given block onwards (§6.4). |
| reference implementation | go-stablenet at commit `740526d03`. |

---

## 3. Primitive types

### 3.1 Fixed-size types

[WBFT-TYPE-001] An `Address` MUST be exactly 20 bytes. The address of a secp256k1 public key is `keccak256(uncompressed_pubkey[1:65])[12:32]` (`A-02 §3`).
- Source: `crypto/crypto.go:287-290` (PubkeyToAddress)
- Observable: header, network

[WBFT-TYPE-002] A `Hash` MUST be exactly 32 bytes. When RLP-decoded (`Digest`, `PreparedDigest`, `MixDigest`, `ParentHash`), an input string of any other length MUST cause the decoding of the enclosing structure to fail.
- Source: `rlp/decode.go:369-399` (decodeByteArray)
- Observable: network, header

[WBFT-TYPE-003] An `ECDSASignature` is 65 bytes `R(32) ‖ S(32) ‖ V(1)`. The encodings that decode (`A-03`) do not constrain its length; a node MUST reject any value whose length is not 65 when it recovers the signer (`A-02 §3.3`).
- Source: `crypto/crypto.go:39`, `crypto/secp256k1/secp256.go:163-171`
- Observable: network, header

[WBFT-TYPE-004] A `BLSPublicKey` is 48 bytes (compressed G1 point). A `BLSSignature` (also called `Seal`) is 96 bytes (compressed G2 point). A BLS secret key is 32 bytes, big-endian. The encodings that decode (`A-03`) do not constrain these lengths; a node MUST reject a value of the wrong length when it uses the value (`A-02 §5.3`).
- Source: `crypto/bls/common/constants.go:9-11`
- Observable: header, network

[WBFT-TYPE-005] `SealType` is one byte. `PREPARE_SEAL = 0x00`, `COMMIT_SEAL = 0x01`. A node MUST NOT sign or verify a seal with any other `SealType` value.
- Source: `consensus/wbft/core/core.go:41-46`
- Observable: header, network

### 3.2 Sequence, block number and round

The same height and the same round appear with different widths in different places. The table summarises the representations; the requirements below fix the conversions.

| Quantity | In consensus messages | In the header | Conversion used by the reference |
|---|---|---|---|
| height | `Sequence`: RLP `bigint`, unbounded | `Header.Number`: RLP `bigint` | `big.Int.Uint64()` wherever a `uint64` is needed |
| round of the block | `Round`: RLP `bigint`, unbounded | `WBFTExtra.round`, `WBFTExtra.prev_round`: `uint32` | `uint32(round.Uint64())`, i.e. `round mod 2^32` |
| round inside `seal_data` | — | — | `uint32(round.Uint64())` |
| round for timers | — | — | `round.Uint64()` (`A-06`) |

[WBFT-TYPE-010] `Sequence` in a consensus message is a non-negative `bigint` without an upper bound. A decoder MUST accept any canonical RLP integer for it (`A-03 §3.2`); range restrictions are applied by the message-acceptance rules of `A-05`, not by decoding.
- Source: `consensus/wbft/messages/common.go:30-36`, `consensus/wbft/messages/prepare.go:83-94`
- Observable: network

[WBFT-TYPE-011] (withdrawn; informative) `Header.Number` is a non-negative `bigint`.
- Source: `core/types/block.go:149-152` (SanityCheck), `consensus/wbft/engine/engine.go:250-263` (Uint64), `consensus/wbft/engine/engine.go:270` (GetConfig), `consensus/wbft/engine/engine.go:220`, `consensus/wbft/engine/engine.go:227`, `consensus/wbft/engine/engine.go:277` (fork checks), `consensus/wbft/engine/engine.go:313`, `consensus/wbft/engine/engine.go:570` (randao_data), `consensus/wbft/engine/engine.go:491-494, 695, 759-760` (Uint64), `consensus/wbft/core/core.go:196`, `consensus/wbft/core/request.go:89` (Int64)

[WBFT-TYPE-012] `Round` in a consensus message is a non-negative `bigint` without an upper bound. Where the reference stores or signs a round as `uint32` (the header fields `round` and `prev_round`, and the round argument of `seal_data`), it MUST use `round mod 2^32`. Consequently rounds `r` and `r + k·2^32` produce the same header field value and the same `seal_data`.
- Source: `consensus/wbft/engine/engine.go:153-158` (writeRoundNumber), `consensus/wbft/core/prepare.go:47`, `consensus/wbft/core/prepare.go:105`, `consensus/wbft/core/commit.go:49`, `consensus/wbft/core/commit.go:108`
- Observable: header, network

Implementation note (informative): `big.Int.Uint64()` returns the low 64 bits of the magnitude, so `uint32(x.Uint64())` equals `x mod 2^32` for every non-negative `x`. A round of 2^32 is not reachable in practice: with the timer rules of `A-06` it would take far longer than the lifetime of a chain.

### 3.3 Composite types

The composite types are listed here with their meaning; their exact encodings are in `A-03`.

| Type | Definition | Encoding |
|---|---|---|
| `View` | `(sequence: bigint, round: bigint)`, ordered lexicographically by `(sequence, round)` | not transmitted as a unit; see `A-03 §8.7` |
| `Proposal` | a block: header, transactions, uncles, optional withdrawals | `A-03 §5` |
| `Digest` | `Hash` = `block_hash(proposal.header)` | `A-03 §6` |
| `SealerSet` | bitmap; bit `i mod 8` of byte `i div 8` set ⇔ sealer index `i` present | `A-03 §4.4` |
| `AggregatedSeal` | `(sealers: SealerSet, signature: bytes)` | `A-03 §4.4` |
| `Candidate` | `(addr: Address, diligence: uint64)` | `A-03 §4.5` |
| `EpochInfo` | `(candidates: [Candidate], validators: [uint32], bls_public_keys: [bytes])`; `validators[i]` is an index into `candidates`; `bls_public_keys[i]` belongs to `validators[i]` | `A-03 §4.5` |
| `WBFTExtra` | the ten consensus fields stored in `Header.Extra` | `A-03 §4` |
| `ValidatorSet` | ordered list of `(address, bls_public_key)`; the order is the order of `EpochInfo.validators` of the governing epoch block | `A-04` |
| `Config` | consensus parameters for one height | §6 |

[WBFT-TYPE-020] `View` comparison MUST compare `sequence` first and `round` only when the sequences are equal.
- Source: `consensus/wbft/types.go:96-104` (View.Cmp)
- Observable: network

[WBFT-TYPE-021] In `EpochInfo`, the entry `bls_public_keys[i]` MUST be the BLS public key of the candidate `candidates[validators[i]]`. The validator set built from an `EpochInfo` lists, in order, `(candidates[validators[i]].addr, bls_public_keys[i])` for `i = 0 .. len(validators)-1`, without sorting. A conforming `EpochInfo` has `len(bls_public_keys) = len(validators)` and every `validators[i] < len(candidates)`. For a non-conforming one the reference behaves as follows: an entry with `validators[i] ≥ len(candidates)` gets the zero address; if `bls_public_keys` is shorter than `validators`, the reference aborts (index out of range); extra `bls_public_keys` entries are ignored.
- Source: `core/types/istanbul.go:177-187` (GetValidators), `core/types/istanbul.go:189-193` (GetCandidate), `consensus/wbft/validator/validator.go:35-41` (NewSet), `consensus/wbft/engine/engine.go:890-903`
- Observable: header

[WBFT-TYPE-022] (withdrawn; informative) `Candidate.diligence` is a `uint64` in units of 10^-6 whose meaningful range is `0 .. 2 · DILIGENCE_DENOMINATOR`. The encoding does not restrict the range; the range follows from the diligence computation of `A-04`, whose requirements are normative.
- Source: `core/types/istanbul.go:94-97`, `core/types/istanbul.go:41-46`

### 3.4 Time and amounts

| Quantity | Type | Unit |
|---|---|---|
| `Header.Time` | `uint64` | seconds since the Unix epoch |
| `Config.block_period` | `uint64` | seconds |
| `Config.request_timeout` | `uint64` | milliseconds |
| `Config.max_request_timeout_seconds` | `uint64` | seconds |
| `Config.allowed_future_block_time` | `uint64` | seconds |
| `WBFTExtra.gas_tip` | `bigint` | wei |

---

## 4. Protocol constants

"Class" uses the classification of §7: **C** consensus-critical (a different value changes which blocks are valid or which bytes are signed), **L** liveness-affecting (a different value can stall or slow the network but does not change validity), **N** local (a node may choose a different value).

### 4.1 Message codes and wire constants

[WBFT-PARAM-001] The consensus message codes MUST be `PREPREPARE = 0x12`, `PREPARE = 0x13`, `COMMIT = 0x14`, `ROUND_CHANGE = 0x15`.
- Source: `consensus/wbft/messages/message.go:28-33`
- Observable: network

[WBFT-PARAM-002] The legacy code `ISTANBUL_MSG = 0x11` MUST be accepted at the network layer with the handling of `A-03 §8.6`. The eth code `NEW_BLOCK_MSG = 0x07` is inspected by the consensus handler as described in `A-07`.
- Source: `consensus/wbft/backend/handler.go:41-44`
- Observable: network

| Name | Value | Class | Meaning | Source |
|---|---|---|---|---|
| `PREPREPARE` | `0x12` | C | PRE-PREPARE code | `consensus/wbft/messages/message.go:29` |
| `PREPARE` | `0x13` | C | PREPARE code | `consensus/wbft/messages/message.go:30` |
| `COMMIT` | `0x14` | C | COMMIT code | `consensus/wbft/messages/message.go:31` |
| `ROUND_CHANGE` | `0x15` | C | ROUND-CHANGE code | `consensus/wbft/messages/message.go:32` |
| `ISTANBUL_MSG` | `0x11` | C | legacy code (`A-03 §8.6`) | `consensus/wbft/backend/handler.go:43` |
| `NEW_BLOCK_MSG` | `0x07` | N | eth block announcement, peeked (`A-07`) | `consensus/wbft/backend/handler.go:42` |
| sub-protocol name / version / length | `"istanbul"` / `100` / `22` | C | devp2p capability (`A-07`) | `eth/quorum_protocol.go:39`, `eth/quorum_protocol.go:41`, `eth/quorum_protocol.go:54` |

### 4.2 Header and extra-data constants

[WBFT-PARAM-010] Every WBFT header with `Number ≥ 1` MUST have `Difficulty = WBFT_DIFFICULTY = 1`. The same value selects the WBFT block-hash rule (`A-03 §6`). The genesis difficulty is copied from the genesis file without constraint (`B-02` SNET-GEN-003; the testnet preset uses 0, so its genesis hash uses the Ethereum rule).
- Source: `core/types/istanbul.go:37-39`, `core/types/block.go:121-131`, `consensus/wbft/engine/engine.go:212-215`
- Observable: header

[WBFT-PARAM-011] `SEAL_LENGTH = 96`. A node MUST NOT aggregate an individual seal whose length is not 96 bytes (the reference fails the aggregation with `ErrInvalidSeal`).
- Source: `core/types/istanbul.go:35`, `consensus/wbft/engine/engine.go:130-137`
- Observable: header

[WBFT-PARAM-012] `EXTRA_VANITY = 32` is used only by a proposer when it builds `WBFTExtra.vanity_data` (`A-03 §4.2`). Verifiers MUST NOT reject a header because of the length or content of `vanity_data`.
- Source: `core/types/istanbul.go:34`, `consensus/wbft/engine/engine.go:1162-1182`, `consensus/wbft/engine/engine.go:196-346` (no vanity check)
- Observable: header

| Name | Value | Class | Meaning | Source |
|---|---|---|---|---|
| `WBFT_DIFFICULTY` | `1` | C | WBFT block marker and hash-rule selector | `core/types/istanbul.go:39` |
| `EXTRA_VANITY` | `32` | N | vanity padding length used by proposers | `core/types/istanbul.go:34` |
| `SEAL_LENGTH` | `96` | C | length of one BLS seal | `core/types/istanbul.go:35` |
| `EMPTY_UNCLE_HASH` | `keccak256(0xc0)` = `0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347` | C | required `Header.UncleHash` | `consensus/wbft/common/constants.go:29`, `core/types/hashes.go:30` |
| `EMPTY_NONCE` | `0x0000000000000000` | N | nonce written by the reference proposer; not verified | `consensus/wbft/common/constants.go:30`, `consensus/wbft/engine/engine.go:488` |
| `MAX_GAS_LIMIT` | `2^63 − 1` | C | upper bound of `Header.GasLimit` | `params/protocol_params.go:28` |
| `MAXIMUM_EXTRA_DATA_SIZE` | `32` | N | cap on the miner's configured extra data | `params/protocol_params.go:31`, `eth/backend.go:307-321` |

### 4.3 Epoch, diligence and shuffle constants

| Name | Value | Class | Meaning | Source |
|---|---|---|---|---|
| `DILIGENCE_DENOMINATOR` | `1_000_000` | C | diligence unit is 10^-6; maximum diligence is `2 · DILIGENCE_DENOMINATOR` | `core/types/istanbul.go:41-43` |
| `DEFAULT_DILIGENCE` | `2 · 1_000_000 · 95 / 100 = 1_900_000` | C | diligence of a new candidate and of genesis candidates | `core/types/istanbul.go:45-46`, `consensus/wbft/config.go:267-270` |
| `SHUFFLE_ROUND_COUNT` | `33` | C | rounds of the swap-or-not shuffle | `consensus/wbft/engine/engine.go:1454` |
| shuffle hash | `keccak256` | C | hash used by the shuffle | `consensus/wbft/engine/engine.go:1478` |
| shuffle buffer layout | seed 32 bytes ‖ round 1 byte ‖ position window 4 bytes | C | input layout of the shuffle hash | `consensus/wbft/engine/engine.go:1449-1453` |

The shuffle and diligence algorithms are specified in `A-04`.

### 4.4 Cryptographic constants

| Name | Value | Class | Meaning | Source |
|---|---|---|---|---|
| `ECDSA_SIGNATURE_LENGTH` | `65` | C | `R ‖ S ‖ V` | `crypto/crypto.go:39` |
| `BLS_SECRET_KEY_LENGTH` | `32` | C | big-endian scalar | `crypto/bls/common/constants.go:9` |
| `BLS_PUBLIC_KEY_LENGTH` | `48` | C | compressed G1 | `crypto/bls/common/constants.go:10` |
| `BLS_SIGNATURE_LENGTH` | `96` | C | compressed G2 | `crypto/bls/common/constants.go:11` |
| `BLS_DST` | ASCII `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` (43 bytes) | C | hash-to-curve domain separation tag | `crypto/bls/blst/signature.go:22` |
| `RANDAO_VERSION` | `1` (encoded as the single byte `0x01`) | C | version byte inside `randao_data` | `consensus/wbft/engine/engine.go:566-569` |
| `PREPARE_SEAL`, `COMMIT_SEAL` | `0x00`, `0x01` | C | `SealType` | `consensus/wbft/core/core.go:41-46` |
| BLS key-generation salt | ASCII `BLS-SIG-KEYGEN-SALT-` | C | initial salt of `derive_bls_key` | blst `src/keygen.c:145` (module `github.com/supranational/blst v0.3.16`, `go.mod:63`) |

### 4.5 Genesis-only constants

| Name | Value | Class | Meaning | Source |
|---|---|---|---|---|
| `INITIAL_GAS_TIP` | `27_600_000_000_000` wei | C | genesis gas tip when `anzeon.systemContracts.govValidator.params.gasTip` is absent (the genesis-extra builder also falls back for an empty or non-decimal value, but such a value makes genesis construction fail, `B-02` SNET-GEN-010) | `params/protocol_params.go:138`, `consensus/wbft/config.go:233-243` |

### 4.6 Local implementation constants

These values are part of the reference implementation's behaviour but a conforming node MAY choose different values. They are listed so that the chapters that reference them (`A-05`, `A-07`, `B-09`) share one definition.

| Name | Value | Class | Meaning | Source |
|---|---|---|---|---|
| `SEQUENCE_THRESHOLD` | `1` | N (liveness-relevant) | how many sequences ahead a message may be and still be kept (`A-05`) | `consensus/wbft/core/backlog.go:45` |
| `ROUND_THRESHOLD` | `10` | N (liveness-relevant) | how many rounds ahead a message may be and still be kept (`A-05`) | `consensus/wbft/core/backlog.go:46` |
| `MAX_BACKLOG_SIZE_PER_VALIDATOR` | `4 · (10 + 1) · (1 + 1) = 88` | N | backlog bound per sender (`A-05`) | `consensus/wbft/core/backlog.go:50` |
| `INMEMORY_PEERS` | `40` | N | peers tracked by the per-peer seen-message cache (`A-07`) | `consensus/wbft/backend/engine.go:40` |
| `INMEMORY_MESSAGES` | `1024` | N | entries per seen-message cache (`A-07`) | `consensus/wbft/backend/engine.go:41` |
| `MAX_STATUS_BLOCK_RANGE` | `1024` | N | RPC `istanbul_status` range cap (`B-09`) | `consensus/wbft/backend/api.go:206` |
| `MAX_ISTANBUL_MSG_SIZE` | `10 485 760` (10 MiB) | L | largest `istanbul/100` frame a node reads; a larger frame disconnects the peer (`A-07` §4.3). A node with a smaller value disconnects peers that send large PRE-PREPARE or ROUND-CHANGE messages, which delays agreement | `eth/handler.go:59` |

---

## 5. Parameters versus constants

A *constant* (§4) has the same value on every WBFT network. A *parameter* (§6) is read from the chain configuration (genesis) and may differ between networks and, through transitions, between heights of one network.

---

## 6. Consensus configuration

### 6.1 The `Config` record

`Config` is the consensus configuration held by a node. `config_at(number)` (§6.5) derives from it the configuration that governs height `number`.

| Field (spec name) | Reference field | Type | Unit | Meaning | Class |
|---|---|---|---|---|---|
| `request_timeout` | `RequestTimeout` | `uint64` | ms | base round-change timeout (`A-06`) | L |
| `block_period` | `BlockPeriod` | `uint64` | s | minimum `Header.Time` distance to the parent (`A-08`) | C |
| `proposer_policy` | `ProposerPolicy` | `{ROUND_ROBIN = 0, STICKY = 1}` | — | proposer rotation rule (`A-04`) | C |
| `epoch` | `Epoch` | `uint64` | blocks | epoch length (`A-04`) | C |
| `allowed_future_block_time` | `AllowedFutureBlockTime` | `uint64` | s | tolerance for header timestamps ahead of the local clock (`A-08`) | L |
| `max_request_timeout_seconds` | `MaxRequestTimeoutSeconds` | `uint64` | s | cap of the round-change timeout for rounds `>= 1` (round 0 always uses `request_timeout`, even when it is larger); `0` means no cap (`A-06` WBFT-TIMER-006) | L |
| `transitions` | `Transitions` | list of `Transition` | — | height-dependent overrides (§6.4) | C |
| `system_contract_upgrades` | `SystemContractUpgrades` | list of `Upgrade` | — | system-contract addresses and versions by height (`B-01`, `B-04`) | C |

- Source: `consensus/wbft/config.go:105-114`

[WBFT-PARAM-020] `proposer_policy` MUST be interpreted as `STICKY` when its identifier is `1` and as `ROUND_ROBIN` for every other identifier.
- Source: `consensus/wbft/config.go:37-42`, `consensus/wbft/validator/default.go:71-87`
- Observable: header, network

### 6.2 Source of the configuration

The reference node does not have a node-local consensus configuration file. `Config` is built once at start-up from the genesis `ChainConfig` (`config.anzeon.wbft` and `config.transitions`). The JSON field names below are those of the genesis file.

| `Config` field | Genesis source | Rule |
|---|---|---|
| `request_timeout` | `anzeon.wbft.requestTimeoutSeconds` (`uint64`) | if non-zero: `requestTimeoutSeconds × 1000` computed in `uint64` (wraps modulo 2^64); else stays `0` |
| `block_period` | `anzeon.wbft.blockPeriodSeconds` | copied if non-zero |
| `epoch` | `anzeon.wbft.epochLength` | copied if non-zero |
| `allowed_future_block_time` | `anzeon.wbft.allowedFutureBlockTime` | copied if non-zero |
| `proposer_policy` | `anzeon.wbft.proposerPolicy` (`*uint64`) | set if present (including `0`); else absent |
| `max_request_timeout_seconds` | `anzeon.wbft.maxRequestTimeoutSeconds` (`*uint64`) | copied if present; else `0` |
| `transitions` | `transitions` (top-level `ChainConfig` field) | copied, then sorted (§6.4) |
| `system_contract_upgrades` | `anzeon.systemContracts` at block 0, then `ChainConfig.CollectUpgrades()` | `B-01` |

- Source: `eth/ethconfig/config.go:192-200` (CreateConsensusEngine), `eth/ethconfig/config.go:213-273` (SetConfigFromChainConfig), `params/config_wbft.go:186-198`

[WBFT-PARAM-030] The base configuration from which the genesis values are applied MUST be the all-zero `Config` (every numeric field `0`, `proposer_policy` absent, no transitions). The reference's `DefaultConfig` (`request_timeout = 1000`, `block_period = 1`, `ROUND_ROBIN`, `epoch = 10`) is not used by a node.
- Source: `eth/ethconfig/config.go:193`, `consensus/wbft/config.go:116-122` (DefaultConfig, referenced only from tests)
- Observable: header

[WBFT-PARAM-031] A node MUST refuse to start on an Anzeon chain configuration that violates any of: `init` present; `init.blsPublicKeys` non-empty; `init.validators` non-empty; `len(init.validators) == len(init.blsPublicKeys)`; `systemContracts` present with `govValidator`, `nativeCoinAdapter`, `govMasterMinter`, `govMinter`, `govCouncil`; system-contract versions accepted (`B-04`); `wbft` present; `wbft.requestTimeoutSeconds > 0`; `wbft.blockPeriodSeconds > 0`; `wbft.epochLength ≥ 2`. The reference rejects these violations with an error (`Invalid genesis config: …`) in the `gstable init` command and when it sets up the genesis block; only when the node itself is started on an empty database with a genesis supplied in code, a missing `wbft` section makes it abort (nil dereference in `SetConfigFromChainConfig`) before the check runs. The check does not cover the hex format of `init.blsPublicKeys`: an entry that is not valid hex makes the reference abort (`hexutil.MustDecode`) when it builds the genesis extra data.
- Source: `params/config_wbft.go:76-130` (CheckValidity), `core/blockchain.go:286-290`, `core/genesis.go:231-238`, `cmd/gstable/chaincmd.go:224-227` (init), `core/genesis.go:297-300`, `eth/backend.go:159-164`, `core/genesis.go:398-408`, `eth/ethconfig/config.go:214-215`, `params/config_wbft.go:68-74`, `consensus/wbft/config.go:272`

[WBFT-PARAM-032] The validity check of [WBFT-PARAM-031] does not cover `proposerPolicy`. When `anzeon.wbft.proposerPolicy` is absent, the reference node terminates abnormally (nil dereference) whenever it builds a validator set with the base policy, and always when a validator starts its engine (`startWBFT`). The base policy is used for heights below the first transition that sets `proposerPolicy` and for the height-0 set (requested for block number 0, e.g. by the validator RPC); a height at or above such a transition uses the transition's policy and does not abort. A configuration for a WBFT network MUST therefore include `proposerPolicy`.
- Source: `eth/ethconfig/config.go:228-230`, `consensus/wbft/validator/default.go:84` (`policy.Id` on a nil policy), `consensus/wbft/engine/engine.go:1106` (height 0, base policy), `consensus/wbft/engine/engine.go:1115` (`config_at(h)`), `consensus/wbft/config.go:175-177`, `consensus/wbft/backend/backend.go:357`, `miner/worker.go:445-448`

[WBFT-PARAM-034] A node MUST NOT start the WBFT engine as a validator with a configuration that has no `anzeon.wbft.proposerPolicy` ([WBFT-PARAM-032]). The reference aborts at engine start, so a node that refuses such a configuration at start-up is observably equivalent to it. A non-validator reference node aborts only when it first builds a validator set with the base policy.
- Source: `consensus/wbft/backend/backend.go:357` (`ProposerPolicy.Use` on a nil policy), `consensus/wbft/config.go:101-103`, `consensus/wbft/backend/engine.go:266`, `eth/ethconfig/config.go:228-230`

[WBFT-PARAM-033] A node MUST take `allowed_future_block_time` only from `anzeon.wbft`; a transition does not change it.
- Source: `eth/ethconfig/config.go:224-226`, `consensus/wbft/config.go:161-184` (no case for it)
- Observable: header

Implementation note (informative): the consensus engine's `Config.String()` is `"wbft"`. The TOML tags on `Config` are not wired to any node configuration file at this commit (`wbft.Config` appears in no `ethconfig.Config` field). A simulation hook (`SimApplier`, `consensus/wbft/backend/backend.go:56-58`, `consensus/wbft/backend/engine.go:156-158`) can mutate the configuration in tests only.

### 6.3 Network presets

The reference defines two StableNet presets. They contain no `transitions`.

| Item | Mainnet preset | Testnet preset |
|---|---|---|
| `chainId` | `8282` | `8283` |
| `anzeon.wbft.epochLength` | `10` | `140` |
| `anzeon.wbft.blockPeriodSeconds` | `1` | `1` |
| `anzeon.wbft.requestTimeoutSeconds` | `2` (→ `request_timeout = 2000` ms) | `2` (→ `2000` ms) |
| `anzeon.wbft.proposerPolicy` | `0` (`ROUND_ROBIN`) | `0` (`ROUND_ROBIN`) |
| `anzeon.wbft.maxRequestTimeoutSeconds` | absent (→ `0`, no cap) | absent (→ `0`, no cap) |
| `anzeon.wbft.allowedFutureBlockTime` | absent (→ `0`) | absent (→ `0`) |
| `anzeon.init.validators` | 1 (`0xaa5faa65e9cc0f74a85b6fdfb5f6991f5c094697`) | 7 |
| genesis gas tip (`govValidator.params.gasTip`) | `27600000000000` | `27600000000000` |
| `BohoBlock` (system-contract upgrade, `B-01`) | `0` | `14408500` |
| genesis hash constant | `0xf192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b` | `0x2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490` |

- Source: `params/config.go:31-32`, `params/config.go:44-152` (StableNetMainnetChainConfig), `params/config.go:155-272` (StableNetTestnetChainConfig)

Both presets are normative. The source still annotates the mainnet preset as a test value (`// TODO: this is just for test on mainnet`, `params/config.go:67`, and `// TODO: define initial validators`, `params/config.go:74`); the comments do not change the values a conforming node uses (`B-01` SNET-CFG-027). The table gives the genesis values only.

Informative: the developer preset `AllDevChainProtocolChanges` (`chainId 1337`, `params/config.go:407-431`) and the test preset `TestWBFTChainConfig` (`params/config.go:602-640`) set `requestTimeoutSeconds = 1000` and `maxRequestTimeoutSeconds = 4`, which yields a base timeout of 1000 s.

### 6.4 Transitions

A `Transition` is `{ block: bigint, <the fields of anzeon.wbft> }`. In the genesis file the WBFT fields are written next to `block`, for example `{"block": 1000, "epochLength": 50}`.

Implementation note (informative): `SetConfigFromChainConfig` keeps a map `hfTransitionBlocks` keyed by `*big.Int` pointers to reject a transition whose block equals a hard-fork transition block. The map is always empty at this commit, so the check never fires (`eth/ethconfig/config.go:235-252`).

### 6.5 `config_at(number)`

```python
def config_at(base: Config, number: bigint) -> Config:
    """Configuration governing height `number`. Pure. Mirrors Config.GetConfig."""
    cfg = copy(base)                      # transitions/system_contract_upgrades are shared, not changed
    for t in base.transitions:            # already sorted by ascending block
        if t.block > number:
            break
        if t.request_timeout_seconds != 0:
            cfg.request_timeout = (t.request_timeout_seconds * 1000) % 2**64
        if t.block_period_seconds != 0:
            cfg.block_period = t.block_period_seconds
        if t.epoch_length != 0:
            cfg.epoch = t.epoch_length
        if t.proposer_policy is not None:           # pointer field: 0 is an explicit value
            cfg.proposer_policy = policy_from_id(t.proposer_policy)   # WBFT-PARAM-020
        if t.max_request_timeout_seconds is not None:  # pointer field: 0 removes the cap
            cfg.max_request_timeout_seconds = t.max_request_timeout_seconds
        # t.allowed_future_block_time is ignored (WBFT-PARAM-033)
    return cfg
```

[WBFT-PARAM-050] `config_at(number)` MUST start from the base configuration and apply every transition with `block ≤ number`, stopping at the first transition with `block > number`.
- Source: `consensus/wbft/config.go:161-192` (GetConfig, getTransitionValue)
- Observable: header

[WBFT-PARAM-051] In a transition, a `requestTimeoutSeconds`, `blockPeriodSeconds` or `epochLength` equal to `0` MUST NOT override the current value. A present `proposerPolicy` or `maxRequestTimeoutSeconds` MUST override the current value even when it is `0`.
- Source: `consensus/wbft/config.go:165-180`
- Observable: header

[WBFT-PARAM-052] A transition's `requestTimeoutSeconds` MUST be converted to `request_timeout` by multiplying by 1000 in `uint64` arithmetic.
- Source: `consensus/wbft/config.go:165-168`

[WBFT-PARAM-053] The height argument used with `config_at` is fixed per use, and implementations MUST use the same argument as the reference:

| Use | Argument | Source |
|---|---|---|
| `block_period` in header verification and in the timestamp of a new proposal | the number of the header being verified or built (so the period between blocks `h-1` and `h` is `config_at(h).block_period`) | `consensus/wbft/engine/engine.go:270`, `consensus/wbft/engine/engine.go:502` |
| `block_period` for scheduling the next block | `latest_number + 1` | `consensus/wbft/backend/engine.go:147-149`, `consensus/wbft/engine/engine.go:482-484` |
| `request_timeout`, `max_request_timeout_seconds` | the current sequence | `consensus/wbft/core/core.go:364-367`, `consensus/wbft/core/core.go:439-440` |
| `proposer_policy` for the validator set of height `h` | `h` (`GetValidators`); for height 0 the base policy | `consensus/wbft/engine/engine.go:1097-1117` |
| `proposer_policy` in the diligence computation of an epoch block | the epoch block's number | `consensus/wbft/engine/engine.go:775` |

- Source: `consensus/wbft/engine/engine.go:270`, `consensus/wbft/engine/engine.go:502`, `consensus/wbft/engine/engine.go:482-484`, `consensus/wbft/core/core.go:364-367`, `consensus/wbft/core/core.go:439-440`, `consensus/wbft/engine/engine.go:1097-1117`, `consensus/wbft/engine/engine.go:775`
- Observable: header

[WBFT-PARAM-054] A node MUST NOT derive epoch boundaries from `config_at(number).epoch` alone: the epoch arithmetic of `A-04` (WBFT-EPOCH-001, WBFT-EPOCH-003) also restarts at the block of every transition that sets `epochLength`. `config_at` gives the length only.
- Source: `consensus/wbft/engine/engine.go:1073-1095` (IsEpochBlockNumber)
- Observable: header

### 6.6 Worked example

Genesis: `requestTimeoutSeconds = 2`, `blockPeriodSeconds = 1`, `epochLength = 10`, `proposerPolicy = 0`, `maxRequestTimeoutSeconds` absent. Transitions (as written in the genesis file, unsorted):

```
[ {"block": 200, "blockPeriodSeconds": 2, "maxRequestTimeoutSeconds": 30},
  {"block": 100, "epochLength": 20, "requestTimeoutSeconds": 0, "proposerPolicy": 1},
  {"block": 300, "maxRequestTimeoutSeconds": 0} ]
```

After sorting: 100, 200, 300.

| `number` | `request_timeout` | `block_period` | `epoch` | `proposer_policy` | `max_request_timeout_seconds` |
|---|---|---|---|---|---|
| 0 .. 99 | 2000 | 1 | 10 | ROUND_ROBIN | 0 |
| 100 .. 199 | 2000 (`0` does not override) | 1 | 20 | STICKY | 0 |
| 200 .. 299 | 2000 | 2 | 20 | STICKY | 30 |
| ≥ 300 | 2000 | 2 | 20 | STICKY | 0 (explicit `0` overrides) |

(Computed by hand from [WBFT-PARAM-050] – [WBFT-PARAM-052]; not machine-generated.)

---

## 7. Classification of parameters

| Class | Definition | Members |
|---|---|---|
| Consensus-critical (C) | Two nodes with different values disagree on block validity, on signed bytes, or on values committed to the chain. | message codes; sub-protocol name, version and length; `WBFT_DIFFICULTY`; `SEAL_LENGTH`; `DILIGENCE_DENOMINATOR`; `DEFAULT_DILIGENCE`; `SHUFFLE_ROUND_COUNT`, shuffle hash and shuffle buffer layout; `ECDSA_SIGNATURE_LENGTH`, `BLS_SECRET_KEY_LENGTH`, `BLS_PUBLIC_KEY_LENGTH`, `BLS_SIGNATURE_LENGTH`; BLS DST; key-generation salt; `RANDAO_VERSION`; `SealType` values; `EMPTY_UNCLE_HASH`; `MAX_GAS_LIMIT`; `INITIAL_GAS_TIP` (genesis); `Config.block_period`, `Config.epoch`, `Config.proposer_policy` (it feeds the diligence computation of epoch blocks, `A-04`); `Config.transitions`; `Config.system_contract_upgrades`; genesis validators, BLS keys and gas tip; chain id (inside `randao_data`, `A-02 §7`) |
| Liveness-affecting (L) | Different values do not change validity but can prevent or delay agreement. | `Config.request_timeout`; `Config.max_request_timeout_seconds`; `Config.allowed_future_block_time` (a header validity input that depends on the local clock; nodes with different values accept a future-dated proposal at different times); `MAX_ISTANBUL_MSG_SIZE` |
| Local (N) | A node may choose its own value; interoperation is unaffected (within the bounds noted). | `EXTRA_VANITY` and the miner's extra data; `EMPTY_NONCE`; `NEW_BLOCK_MSG`; `SEQUENCE_THRESHOLD`, `ROUND_THRESHOLD`, `MAX_BACKLOG_SIZE_PER_VALIDATOR` (liveness-relevant if set far below the reference values); `INMEMORY_PEERS`, `INMEMORY_MESSAGES`; `MAX_STATUS_BLOCK_RANGE`; `MAXIMUM_EXTRA_DATA_SIZE` |

[WBFT-PARAM-060] All nodes of a network MUST use identical values for every consensus-critical item of §7, and SHOULD use identical values for every liveness-affecting item.
- Source: `consensus/wbft/engine/engine.go:270` (block_period in validity), `consensus/wbft/engine/engine.go:1073-1095` (epoch), `consensus/wbft/engine/engine.go:775` (proposer_policy in epoch computation), `consensus/wbft/core/core.go:364-367` (timeouts)
- Observable: header
