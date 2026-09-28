# A-08 Header rules

- Area code: `HDR`
- Status: draft
- Reference: go-stablenet `740526d03`. All `Source:` lines point to this commit. The only textual difference from `v1.1.0` in this chapter's code is the gas-tip `nil` condition in `verifyGasTip` (`consensus/wbft/engine/engine.go:1291`); it is unreachable, so there is no consensus-relevant difference (`A-12` §2.1, `WBFT-HDR-111`).

This chapter specifies the *consensus fields* of a block header: how a proposer constructs them, how the finalized header receives its seals, and how a node verifies them at three points (proposal at PRE-PREPARE, finalized header at import, stateless light verification). Where the reference implementation interleaves consensus-field checks with execution-field checks in a single function, the full ordered procedure is given here, and each step is marked:

- **[A]** — specified in this chapter,
- **[B]** — specified in Part B (the chapter is named in the step). The step is listed here only to fix its position in the order.

The order is normative: a conforming verifier that returns an error MUST return the error of the first failing step, because the error class decides what the caller does next (future-block queue, unknown-ancestor handling, bad-block record; see `A-09 §5`).

---

## 1. Scope and field ownership

| Header field | Owner | Constructed in | Verified in |
|---|---|---|---|
| `ParentHash`, `Number` | A (this chapter) for the linkage rule; builder sets the value | §3.1 | §6 steps H1, H10, H11 |
| `Coinbase` | A | §3.3 | §6 step H15a (membership); `B-08` SNET-SRC-020 (blacklist, step H15b) |
| `Difficulty` | A | §3.3 | §6 step H4 |
| `Nonce` | A | §3.3 | not verified (`WBFT-HDR-012`) |
| `Time` | A | §3.4 | §6 steps H2, H12 |
| `MixDigest` | A | §3.9 | §6 step H19 |
| `Extra` (whole `WBFTExtra`) | A for structure, randao, seals, round, `EpochInfo` presence; B-06 for the `GasTip` value and the `EpochInfo` content | §3.5–§3.8, §4 | §6 steps H16–H20, §7 |
| `UncleHash`, `GasLimit`, `GasUsed`, `BaseFee`, `WithdrawalsHash`, `BlobGasUsed`, `ExcessBlobGas`, `ParentBeaconRoot` | B-03 | builder, `B-03` | §6 steps H3, H5–H9, H13, H14 |
| `Root`, `TxHash`, `ReceiptHash`, `Bloom` | B-03 / B-06 | builder | execution (`A-09 §5`) and step P4 (`TxHash`) |

Encodings (`WBFTExtra`, `AggregatedSeal`, `SealerSet`, `EpochInfo`) are defined in `A-03`. Cryptographic functions (`seal_data`, `randao_data`, `randao_mix`, `ecdsa_recover_address`, BLS operations) are defined in `A-02`. Validator sets, quorum and epochs (`validators_at`, `quorum_size`, `is_epoch_block`) are defined in `A-04`.

---

## 2. Notation used in this chapter

```python
# From A-01 / A-03 / A-04 (not redefined here)
config_at(number) -> Config            # BlockPeriod, Epoch, ... after transitions with Block <= number
validators_at(number, parent_hash, parents) -> ValidatorSet
                                        # A-04 §3.1 (chain argument implicit); set that seals block `number`;
                                        # number == 0 -> genesis config set; `parents` is A-04's optional
                                        # batch argument (headers not yet stored, consulted first)
quorum_size(n) -> int                   # ceil(n - (n - 1) / 3) evaluated in float64
decode_extra(extra: bytes) -> WBFTExtra # raises on any RLP error, trailing bytes, or missing field
encode_extra(x: WBFTExtra) -> bytes
block_hash(header) -> Hash              # keccak256(rlp(filtered_header(header, 0))) when Difficulty == 1
seal_data(header, round: uint32, seal_type) -> bytes32
randao_data(chain_id, number) -> bytes32
randao_mix(parent_mix: Hash, reveal: bytes) -> Hash
ecdsa_recover_address(data, sig) -> Address   # recovers from keccak256(data), see A-02

EMPTY_UNCLE_HASH = keccak256(rlp([]))
WBFT_DIFFICULTY = 1
EMPTY_NONCE = 0x0000000000000000
EXTRA_VANITY = 32
SEAL_LENGTH = 96
```

`now()` is the local wall-clock time in whole seconds (Unix).

---

## 3. Proposal header construction

A node builds a proposal for height `n = parent.Number + 1` when the consensus core asks for one (`A-09 §4.4`). The execution layer creates the header skeleton (`ParentHash`, `Number`, `GasLimit`, `BaseFee`, a provisional `Time`, a provisional `Coinbase`, the miner vanity in `Extra`), then calls the consensus preparation described here, then executes transactions and runs `process_finalize` (`B-06`), which may add `EpochInfo` (§3.9) and sets `Root`.

### 3.1 Inputs

| Input | Source in the reference implementation |
|---|---|
| `parent` | the current head block header at the time the builder starts, or the block named by `parentHash` when the builder is asked for a specific parent (`miner/worker.go:1124-1131`) |
| `header.ParentHash = block_hash(parent)`, `header.Number = parent.Number + 1` | `miner/worker.go:1142-1148` |
| `header.Extra` = miner vanity (0 to 32 bytes) | `miner/worker.go:1150-1152`, limit `params/protocol_params.go:31`, `miner/miner.go:205-211`, `eth/backend.go:307-322` |
| `extra_prepared`, `extra_committed`: lists of `(sealer_index, seal)` collected after the previous height reached quorum (*extra seals*, `A-05`) | `consensus/wbft/backend/engine.go:221-229`, `consensus/wbft/core/extraseal.go:133-183` |
| signing key of the node (`sign` = ECDSA over keccak256 of the input) | `consensus/wbft/backend/backend.go:84, 281-284` |

### 3.2 Algorithm

```python
def prepare_proposal_header(chain, header, extra_prepared, extra_committed):
    # [WBFT-HDR-010] Coinbase
    header.Coinbase = self.address                      # node key address
    # [WBFT-HDR-012] Nonce
    header.Nonce = EMPTY_NONCE
    n = header.Number
    parent = chain.header_by_hash_and_number(header.ParentHash, uint64(n) - 1)   # low 64 bits
    if parent is None:
        raise ErrUnknownAncestor
    # [WBFT-HDR-011] Difficulty
    header.Difficulty = WBFT_DIFFICULTY
    # [WBFT-HDR-013] Time
    header.Time = max(parent.Time + config_at(n).BlockPeriod, now())
    # [WBFT-HDR-020] gas tip at parent state (value: B-06)
    gas_tip = app.gas_tip(parent)                       # raises -> no proposal (WBFT-HDR-021)
    prev = None
    if n > 1:                                           # full value
        last = chain.header_by_number(uint64(n) - 1)    # WBFT-HDR-030: canonical, by NUMBER (low 64 bits)
        if last.Number == 0:
            pass                                        # unreachable for 1 < n < 2^64; for n = k*2^64 + 1
                                                        # the lookup returns genesis and no previous seals are written
        else:
            lx = decode_extra(last.Extra)               # raises -> no proposal
            if lx.PreparedSeal is None: raise ErrEmptyPreparedSeals
            if lx.CommittedSeal is None: raise ErrEmptyCommittedSeals
            prev = (lx.Round,
                    merge_seals(lx.PreparedSeal, extra_prepared),
                    merge_seals(lx.CommittedSeal, extra_committed))
    # the following steps raise errors wrapped as "failed to write wbft extra: ..."
    x = start_extra(header.Extra)                       # WBFT-HDR-016, may raise
    # [WBFT-HDR-014] randao reveal, signed only after start_extra succeeded
    x.RandaoReveal = ecdsa_sign(self.key, randao_data(chain_id, n))   # 65 bytes; ecdsa_sign hashes (A-02 §7.2)
    if prev is not None:
        x.PrevRound, x.PrevPreparedSeal, x.PrevCommittedSeal = prev
    x.GasTip = gas_tip
    header.Extra = encode_extra(x)
    # [WBFT-HDR-040] MixDigest
    header.MixDigest = randao_mix(parent.MixDigest, x.RandaoReveal)
```

Source: `consensus/wbft/backend/engine.go:154-167`, `consensus/wbft/engine/engine.go:486-549, 551-573, 583-604`, `consensus/wbft/engine/apply_extra.go:38-50`.

### 3.3 Fixed fields

[WBFT-HDR-010] A proposer MUST set `Coinbase` to the address of its node key (the ECDSA key that also signs consensus messages and the randao reveal).
Source: `consensus/wbft/engine/engine.go:487`, `consensus/wbft/backend/backend.go:75, 84`
Observable: header

[WBFT-HDR-011] A proposer MUST set `Difficulty` to `1`.
Source: `consensus/wbft/engine/engine.go:499`, `core/types/istanbul.go:39`
Observable: header

[WBFT-HDR-012] A proposer MUST set `Nonce` to eight zero bytes. A verifier MUST NOT reject a header because of its `Nonce` value (the reference implementation does not check it at any verification point).
Source: `consensus/wbft/engine/engine.go:488`, `consensus/wbft/common/constants.go:30`; absence of a check: `consensus/wbft/engine/engine.go:196-350`
Observable: header

Implementation note (informative). The execution layer sets a provisional `Coinbase` from `etherbase` (`miner/worker.go:1147`), which on WBFT chains is forced to the node key address (`eth/backend.go:173-176`); `prepare_proposal_header` overwrites it in any case.

### 3.4 Timestamp

[WBFT-HDR-013] A proposer MUST set `Time = max(parent.Time + config_at(n).BlockPeriod, now())`, where `BlockPeriod` is taken from the configuration at the height of the *new* block `n`.
Source: `consensus/wbft/engine/engine.go:501-505`, `consensus/wbft/config.go:161-184`
Observable: header

Consequences (informative):

- If the proposer's clock is behind, `Time = parent.Time + BlockPeriod` and the block is on schedule.
- If the proposer's clock is ahead of the verifiers' clocks by more than `AllowedFutureBlockTime`, verifiers treat the proposal as a future block and re-process it when their clock reaches `Time` (step P6). Chain time can therefore run ahead of wall-clock time while such a proposer is active; every later block is at least `BlockPeriod` after it (`A-10 §10`).
- The builder's own timestamp (`miner/worker.go:1134-1140`) is always overwritten.

### 3.5 Randao reveal

[WBFT-HDR-014] A proposer MUST set `Extra.RandaoReveal` to its 65-byte ECDSA signature `[R ‖ S ‖ V]` (`V ∈ {0,1}`) over `keccak256(randao_data(chain_id, n))`, produced with the node key. The reference signer is RFC 6979 deterministic, so an honest node produces exactly one reveal per `(chain_id, n)`.
Source: `consensus/wbft/engine/engine.go:551-573`, `consensus/wbft/backend/backend.go:281-284`
Observable: header

Note that `randao_data` already is a Keccak-256 digest and the signer hashes it again: the signed 32-byte message is `keccak256(keccak256(chain_id_bytes ‖ 0x01 ‖ number_bytes))` (`A-02`).

### 3.6 Starting the extra data and vanity

`start_extra` reproduces `getExtra`:

```python
def start_extra(extra: bytes) -> WBFTExtra:
    if len(extra) < EXTRA_VANITY:
        return WBFTExtra(VanityData=extra + b"\x00" * (EXTRA_VANITY - len(extra)),
                         RandaoReveal=b"", PrevRound=0, PrevPreparedSeal=None,
                         PrevCommittedSeal=None, Round=0, PreparedSeal=None,
                         CommittedSeal=None, GasTip=None, EpochInfo=None)
    return decode_extra(extra)          # raises on failure
```

Source: `consensus/wbft/engine/engine.go:1162-1182`

[WBFT-HDR-016] A proposer whose builder supplies fewer than 32 bytes of vanity MUST place those bytes, right-padded with zero bytes to exactly 32 bytes, in `Extra.VanityData`.
Source: `consensus/wbft/engine/engine.go:1163-1177`
Observable: header

[WBFT-HDR-017] A verifier MUST accept any `VanityData` value, including values whose length is not 32; the reference implementation places no constraint on it at any verification point.
Source: `consensus/wbft/engine/engine.go:301-304` (decode only), `core/types/istanbul.go:122-149`
Observable: header

Edge case (normative for compatibility): when the builder supplies exactly 32 bytes of vanity, `start_extra` tries to decode them as a complete `WBFTExtra`. For any vanity that is not itself a valid `WBFTExtra` encoding, construction fails and the node cannot propose. If a 32-byte builder vanity does decode as `WBFTExtra`, the decoded value is the starting point: every field that `prepare_proposal_header` does not overwrite (`VanityData`, `Round`, `PreparedSeal`, `CommittedSeal`, `EpochInfo`, and for `n = 1` also `PrevRound`, `PrevPreparedSeal`, `PrevCommittedSeal`) keeps its decoded value.

[WBFT-HDR-018] A proposer given a 32-byte builder vanity that does not decode as `WBFTExtra` MUST fail proposal construction (it produces no proposal for that request).
Source: `consensus/wbft/engine/engine.go:1163, 1181`, `consensus/wbft/engine/engine.go:544-546`, `params/protocol_params.go:31`

### 3.7 Gas tip placement

[WBFT-HDR-020] A proposer MUST set `Extra.GasTip` to the value returned by the application's gas-tip query at the parent state (`A-09 §4.3`, value rules in `B-06`). The field MUST be non-`nil` in every proposal.
Source: `consensus/wbft/engine/engine.go:511-513, 518, 537, 542, 599-604, 622-645`
Observable: header

[WBFT-HDR-021] If the gas-tip query fails (no system-contract configuration, missing parent, parent `Root` equal to the zero hash, parent state unavailable), the proposer MUST NOT produce a proposal for that request.
Source: `consensus/wbft/engine/engine.go:511-513, 622-638`

### 3.8 Previous-block seals

For `n >= 2` the proposal carries the round and the aggregated seals by which the block at height `n-1` was finalized, possibly enlarged by *extra seals* (seals that arrived after quorum, `A-05`).

[WBFT-HDR-030] For `n >= 2`, a proposer MUST take the previous-block seals from the header that its chain stores as the canonical block at number `n-1`, looked up **by number**, not from the header identified by `ParentHash`. In normal operation both are the same header; they differ only if the canonical chain changed between choosing the parent and this lookup.
Source: `consensus/wbft/engine/engine.go:515-517`

[WBFT-HDR-031] For `n >= 2`, if the header at `n-1` has no `PreparedSeal` or no `CommittedSeal`, the proposer MUST fail with `ErrEmptyPreparedSeals` or `ErrEmptyCommittedSeals` respectively (checked in that order) and produce no proposal.
Source: `consensus/wbft/engine/engine.go:520-530`

[WBFT-HDR-032] For `n >= 2`, a proposer MUST set `Extra.PrevRound = Extra(n-1).Round`, `Extra.PrevPreparedSeal = merge_seals(Extra(n-1).PreparedSeal, extra_prepared)` and `Extra.PrevCommittedSeal = merge_seals(Extra(n-1).CommittedSeal, extra_committed)`, where `Extra(n-1)` is the proposer's **local copy** of the header at `n-1` (see `WBFT-HDR-053`: copies may differ between nodes).
Source: `consensus/wbft/engine/engine.go:532-537, 583-590`
Observable: header

[WBFT-HDR-033] For `n = 1`, a proposer whose builder vanity is shorter than 32 bytes MUST leave `PrevRound = 0`, `PrevPreparedSeal = nil` and `PrevCommittedSeal = nil` (for a 32-byte vanity see §3.6).
Source: `consensus/wbft/engine/engine.go:82-84, 539-543`
Observable: header

#### 3.8.1 `merge_seals`

```python
def merge_seals(seal: AggregatedSeal, extra: list[tuple[uint32, bytes]]) -> AggregatedSeal:
    if len(extra) == 0:
        return seal                                   # same object, unchanged
    sigs = [seal.signature]                           # the existing aggregate, compressed
    sealers = copy(seal.sealers)                      # same byte length as the input bitmap
    for (index, sig) in extra:                        # order irrelevant: aggregation is commutative
        if sealers.is_sealer(index):
            continue                                  # never double-count an index
        sealers.set_sealer(index)                     # may extend the bitmap by whole bytes
        sigs.append(sig)
    try:
        agg = bls_aggregate_compressed(sigs)          # group-checks every input (A-02)
    except Error:
        return seal                                   # fallback: all extra seals discarded
    return AggregatedSeal(sealers=sealers, signature=compress(agg))
```

Source: `consensus/wbft/engine/engine.go:1371-1397`, `core/types/istanbul.go:290-313`, `crypto/bls/blst/signature.go:70-77`

[WBFT-HDR-034] A proposer MUST compute merged previous seals exactly as `merge_seals`: an index already present in the input bitmap is skipped; every new index is set in the bitmap and its signature is aggregated with the existing aggregate; if aggregation fails for any input, the result MUST be the unmodified input seal (no partial merge).
Source: `consensus/wbft/engine/engine.go:1371-1397`
Observable: header

[WBFT-HDR-035] The extra seals passed to `merge_seals` MUST be only those that the consensus core recorded for view `(head.Number, prior_round)` whose digest equals `block_hash(head)`, where `head` is the chain head at the time of the call (normally block `n-1`), each mapped to its index in the prior validator set; seals from senders not in that set are dropped. If the consensus core is not running, both lists are empty.
Source: `consensus/wbft/core/extraseal.go:133-183`, `consensus/wbft/core/core.go:133-139`, `consensus/wbft/backend/engine.go:221-229`

`merge_seals` does not verify the merged aggregate. Each extra seal was verified individually when it was received, against the header of the prior proposal and the round of the message (`consensus/wbft/core/extraseal.go:50-82`). If `prior_round` differs from `Extra(n-1).Round` (possible when this node imported block `n-1` while it was in a different round), the merged aggregate combines signatures over two different `seal_data` values and the resulting proposal fails step H20 at every verifier; the height then proceeds through a round change.

[WBFT-HDR-036] A proposer MAY produce a proposal whose merged previous seals fail verification (the reference implementation does not check the merge result); such a proposal is rejected by verifiers at step H20 with `ErrInvalidPrevPreparedSeals` or `ErrInvalidPrevCommittedSeals`.
Source: `consensus/wbft/engine/engine.go:532-537, 1371-1397`, `consensus/wbft/core/backlog.go:155-162`, `consensus/wbft/core/priorstate.go:35-45`

### 3.9 Mix digest

[WBFT-HDR-040] A proposer MUST set `MixDigest = randao_mix(parent.MixDigest, Extra.RandaoReveal)`, where `parent` is the header identified by `ParentHash`.
Source: `consensus/wbft/engine/engine.go:547, 575-581`
Observable: header

### 3.10 Fields completed after execution

After transaction execution, `process_finalize` (`B-06`) completes the header:

- On an epoch block, `Extra.EpochInfo` is computed from the post-execution state and written, but only if the node is a member of `validators_at(n)`; otherwise it is left `nil` (such a proposal would be invalid, which is harmless because a non-member cannot propose).
- `Root` is set from the post-finalization state; `UncleHash` is set to `EMPTY_UNCLE_HASH`.

Source: `consensus/wbft/engine/engine.go:929-970, 1193-1207, 1053-1061`

[WBFT-HDR-041] A node MUST use as the digest of a proposal the `block_hash` of the header *after* `process_finalize`; `EpochInfo`, `GasTip`, `RandaoReveal`, `PrevRound` and the previous seals are covered by it, while `Round`, `PreparedSeal` and `CommittedSeal` are not.
Source: `core/types/block.go:121-131`, `core/types/istanbul.go:263-288`, `miner/worker.go:1401`
Observable: header

### 3.11 Header skeleton fields owned by the builder

[WBFT-HDR-042] A proposer MUST set `ParentHash = block_hash(parent)` and `Number = parent.Number + 1`.
Source: `miner/worker.go:1142-1145`
Observable: header

---

## 4. Finalized header: writing the seals

When the consensus core has collected a COMMIT quorum for proposal `B` in round `r`, it produces the finalized header by writing three fields into the extra data of `B`'s header (`A-05` decides *when*; this section specifies *what*).

```python
def commit_header(header, prepared: list[SealData], committed: list[SealData], round: int):
    x = decode_extra(header.Extra)                    # len(Extra) >= 32 here
    if len(prepared) == 0: raise ErrInvalidPreparedSeals
    x.PreparedSeal = aggregate_seals(prepared)
    if len(committed) == 0: raise ErrInvalidCommittedSeals
    x.CommittedSeal = aggregate_seals(committed)
    x.Round = uint32(round mod 2**64)                 # truncation to uint32 (A-03)
    header.Extra = encode_extra(x)

def aggregate_seals(seals: list[SealData]) -> AggregatedSeal:
    sealers = SealerSet()
    sigs = []
    for s in seals:
        if len(s.seal) != SEAL_LENGTH: raise ErrInvalidSeal
        sealers.set_sealer(s.sealer)
        sigs.append(s.seal)
    return AggregatedSeal(sealers, compress(bls_aggregate_compressed(sigs)))   # raises if any input fails the group check
```

Source: `consensus/wbft/engine/engine.go:90-158`, `consensus/wbft/backend/backend.go:213-230`, `consensus/wbft/core/commit.go:137-179`

[WBFT-HDR-050] A node that decides proposal `B` at round `r` and hands it over with `finalize` (`A-09` §4.7) MUST write `Extra.PreparedSeal` as the aggregate of the PREPARE seals it holds for `(n, r)`, `Extra.CommittedSeal` as the aggregate of the COMMIT seals it holds for `(n, r)`, and `Extra.Round = r` (low 32 bits), leaving every other header field unchanged.
Source: `consensus/wbft/engine/engine.go:90-98`, `consensus/wbft/core/commit.go:143-173`
Observable: header

[WBFT-HDR-051] Writing the seals MUST NOT change the block hash: `block_hash(commit_header(B)) == block_hash(B)`.
Source: `core/types/block.go:121-131`, `core/types/istanbul.go:269-288`, `consensus/wbft/backend/backend.go:229, 239`
Observable: header

[WBFT-HDR-052] If either seal list is empty, any seal is not 96 bytes, or BLS aggregation of a list fails, seal writing (`finalize`, `A-09` §4.7) MUST fail with `ErrInvalidPreparedSeals`, `ErrInvalidSeal`, the BLS aggregation error, `ErrInvalidCommittedSeals`, `ErrInvalidSeal` or the BLS aggregation error, checked in the order of `commit_header`: prepared list empty, prepared seal lengths, prepared aggregation, committed list empty, committed seal lengths, committed aggregation; the consensus core then broadcasts ROUND-CHANGE for round `r+1` (`A-05`). (The core copies every seal into a 96-byte buffer, so a length error arises only from an empty `SealData` left by a source outside the validator set.)
Source: `consensus/wbft/engine/engine.go:101-158`, `crypto/bls/blst/signature.go:70-77`, `consensus/wbft/core/commit.go:149-154, 164-169, 173-176`

Informative. In the reference implementation the COMMIT aggregate is written at the moment the quorum is reached and therefore normally contains exactly `quorum_size(N)` sealers; so does the PREPARE aggregate, because PREPAREs that arrive after the PREPARE quorum are handled as extra seals (`A-05` WBFT-SM-048). Seals that arrive later become extra seals and appear, merged, in the previous-seal fields of block `n+1` (§3.8).

[WBFT-HDR-053] The values of `Round`, `PreparedSeal` and `CommittedSeal` in a finalized header are **node-local**: each node writes the seals it collected, and the copy a node stores is whichever sealed copy it imported first. Two conforming nodes MAY store headers with the same hash and different values of these three fields. A verifier or observer MUST NOT treat a difference in these fields between nodes as a fault, and MUST NOT require `PrevRound(n+1) == Round(n)` or `PrevPreparedSeal(n+1).sealers ⊇ PreparedSeal(n).sealers` against its own copy of block `n`.
Source: `consensus/wbft/backend/backend.go:221-249`, `consensus/wbft/engine/engine.go:515-537`, `eth/fetcher/block_fetcher.go:790-799`, `core/blockchain.go:1605-1624`
Observable: header, rpc

---

## 5. Proposal verification at PRE-PREPARE

When a PRE-PREPARE passes the message checks of `A-05` (sender is the proposer of the view, `Sequence` equals the proposal number, justification for `round > 0`), the node verifies the proposed block with `verify_proposal_header`. The block's body is checked only for its transaction root and uncles; transactions are **not** executed (`A-09 §4.6`).

```python
def verify_proposal_header(block) -> (Duration, Error | None):
    # P1
    if not is_block(block):                      return 0, ErrInvalidProposal
    # P2
    if app.is_bad_block(block_hash(block.header)): return 0, ErrBlacklistedHash
    # P3
    V, Vp = validators_for_verifying(block.header, parents=[])   # errors: §6.1
    # P4 [B-03]
    if derive_sha(block.transactions) != block.header.TxHash:   return 0, ErrMismatchTxhashes
    # P5 [B-03]
    if calc_uncle_hash(block.uncles) != EMPTY_UNCLE_HASH:         return 0, ErrInvalidUncleHash
    # P6
    err = verify_header(block.header, parents=[], V, Vp, check_seals=False)
    if err == ErrFutureBlock:
        return block.header.Time - now_precise(), ErrFutureBlock
    if err:                                                     return 0, err
    # P7
    if app.header_by_hash(block.header.ParentHash) is None:     return 0, "unknown parent hash"
    return 0, None
```

Source: `consensus/wbft/backend/backend.go:258-278`, `consensus/wbft/engine/engine.go:160-186`, `consensus/wbft/core/preprepare.go:147-169`

[WBFT-HDR-060] A node MUST verify a proposal with the steps P1–P7 in that order and MUST treat the first failing step as the result.
Source: `consensus/wbft/backend/backend.go:258-278`, `consensus/wbft/engine/engine.go:160-186`
Observable: network

[WBFT-HDR-061] A node MUST reject a proposal whose hash is recorded as a bad block (`ErrBlacklistedHash`), before any other check except the type check.
Source: `consensus/wbft/backend/backend.go:266-270`, `core/blockchain.go:2422-2425`
Observable: network

[WBFT-HDR-063] If `verify_header` returns `ErrFutureBlock`, proposal verification MUST return `ErrFutureBlock` together with the duration `time.Until(Unix(int64(Time)))`, and the node MUST re-process the same PRE-PREPARE after that duration (`A-05`, `A-06`). For `Time < 2^63` this duration is `Time − now` with sub-second precision, saturating at the maximum Go `Duration` (about 292 years); no other upper bound applies. For `Time ≥ 2^63` the conversion to `int64` wraps, the duration is negative, and the PRE-PREPARE is re-processed immediately and, because H2 keeps failing, repeatedly.
Source: `consensus/wbft/engine/engine.go:174-175`, `consensus/wbft/core/preprepare.go:150-163`
Observable: network

What proposal verification does **not** check (informative): the state root, receipts root, bloom, gas used against execution, the EVM validity of transactions, the `EpochInfo` content, the aggregated seals of the block itself. These are checked only when the finalized block is imported (`A-09 §5`). A proposal that passes P1–P7 may therefore still become a bad block after finalization (`A-10 §9`).

---

## 6. Header verification

`verify_header` is the single procedure behind all header verification in the reference implementation:

| Caller | `check_seals` | `parents` | Where |
|---|---|---|---|
| proposal verification (§5) | false | `[]` | `consensus/wbft/engine/engine.go:173` |
| single header (`VerifyHeader`): block fetcher, start-up | true | `[]` | `consensus/wbft/backend/engine.go:71-81`, `eth/handler.go:245`, `core/blockchain.go:424` |
| batch (`VerifyHeaders`): `InsertChain`, header-chain insertion (snap sync) | true | preceding headers of the batch | `consensus/wbft/backend/engine.go:87-112`, `core/blockchain.go:1587`, `core/headerchain.go:327` |

### 6.1 Validator sets for verification

```python
def validators_for_verifying(header, parents) -> (ValidatorSet, ValidatorSet):
    # V0a
    try:
        V = validators_at(header.Number, header.ParentHash, parents)   # for Number != 0 the epoch is
                                                     # found from uint64(Number) - 1 (wraps for k*2^64)
    except Error:
        raise ErrUnknownAncestor                     # every error is mapped
    # V0b
    if uint64(header.Number) >= 2:
        parent = parents[-1] if parents else chain.header(header.ParentHash, uint64(header.Number) - 1)
        if parent is None:
            raise ErrUnknownAncestor
        Vp = validators_at(parent.Number, parent.ParentHash, parents[:-1])   # errors returned as-is
    else:
        Vp = V
    return V, Vp
```

Source: `consensus/wbft/backend/engine.go:420-450`, `consensus/wbft/engine/engine.go:1097-1117, 1407-1436` (`uint64` at `:1408`, `backend/engine.go:429-431`)

[WBFT-HDR-070] A verifier MUST determine the validator set `V` of the header's height and the set `Vp` of the parent's height before any other header check, and MUST report every failure to determine `V` as `ErrUnknownAncestor`.
Source: `consensus/wbft/backend/engine.go:76-79, 425-427`

[WBFT-HDR-071] To find the epoch header that defines `V`, a verifier MUST first use the header stored as canonical at the epoch block number, and only if none is stored walk back from the verified header through `parents` and then through `ParentHash` links.
Source: `consensus/wbft/engine/engine.go:1407-1436`

### 6.2 The ordered procedure

```python
def verify_header(header, parents, V, Vp, check_seals) -> Error | None:
    # ---- verifyHeader: context-free and fork checks ----
    H1  [A]  if header.Number is None:                         return ErrUnknownBlock
    H2  [A]  if header.Time > now() + AllowedFutureBlockTime:  return ErrFutureBlock
    H3  [B-03] if header.UncleHash != EMPTY_UNCLE_HASH:          return ErrInvalidUncleHash
    H4  [A]  if header.Difficulty != 1:                        return ErrInvalidDifficulty
    H5  [B-03] if header.GasLimit > 2**63 - 1:                 return "invalid gasLimit"
    H6  [B-03] if is_shanghai(header.Number, header.Time):     return "wbft does not support shanghai fork"   # SNET-BHDR-003
    H7  [B-03] if header.WithdrawalsHash is not None:          return "invalid withdrawalsHash"
    H8  [B-03] if is_cancun(header.Number, header.Time):       return "wbft does not support cancun fork"     # SNET-BHDR-005
    H9  [B-03] ExcessBlobGas, then BlobGasUsed, then ParentBeaconRoot must be None
    # ---- verifyCascadingFields ----
    H10 [A]  n = uint64(header.Number); if n == 0:             return None      # genesis
    H11 [A]  parent = parents[-1] if parents else chain.header(header.ParentHash, n - 1)
             if parent is None or uint64(parent.Number) != n - 1 or block_hash(parent) != header.ParentHash:
                                                               return ErrUnknownAncestor
    H12 [A]  if parent.Time + config_at(header.Number).BlockPeriod > header.Time:
                                                               return ErrInvalidTimestamp
    H13 [B-03] if header.GasUsed > header.GasLimit:            return "invalid gasUsed"
    H14 [B-03] if not is_london(header.Number): BaseFee must be None, gas-limit bounds against parent
               else: EIP-1559 header rules (gas limit bounds, base fee)
    H15a [A] if header.Coinbase not in V:                      return ErrUnauthorized
    H15b [B-08] state = state_at(parent.Root)                  # SNET-SRC-020
             if unavailable:                                   skip (continue at H16)
             if is_blacklisted(state, header.Coinbase):       return ErrBlacklistedSigner
    H16 [A]  x = decode_extra(header.Extra) or                 return ErrInvalidExtraDataFormat
    H17 [A]  if check_seals: verify_seals(header, V, x)        # §6.4
    H18 [A]  if ecdsa_recover_address(randao_data(chain_id, header.Number), x.RandaoReveal) != header.Coinbase:
                                                               return "failed to verify randao reveal signature: ..."
    H19 [A]  if randao_mix(parent.MixDigest, x.RandaoReveal) != header.MixDigest:
                                                               return "invalid randao mix: ..."
    H20 [A]  if header.Number > 1: verify_prev_seals(parent, Vp, x)   # §6.5
    H21 [B-06] gas tip check at parent state; skipped unless the error is a mismatch
             or ErrGasTipContractUnavailable (§6.6; the latter is unreachable, WBFT-HDR-111);
             the parent is re-read from the chain by (ParentHash, uint64(Number) - 1), `parents` is ignored
    return None
```

Source: `consensus/wbft/engine/engine.go:196-350` (H1 `:197-199`, H2 `:201-205`, H3 `:208-210`, H4 `:213-215`, H5 `:217-219`, H6 `:220-222`, H7 `:224-226`, H8 `:227-229`, H9 `:231-238`, H10 `:250-253`, H11 `:256-265`, H12 `:270-272`, H13 `:274-276`, H14 `:277-288`, H15 `:291-298, 352-381`, H16 `:301-304`, H17 `:307-311`, H18 `:313-315`, H19 `:316-319`, H20 `:322-327`, H21 `:329-348`)

[WBFT-HDR-080] A verifier MUST apply steps H1–H21 in the order given and MUST return the error of the first failing step. Steps marked [B-xx] are specified in the named Part B chapter; their position in the order is specified here. H6 and H8 are `B-03` SNET-BHDR-003 and SNET-BHDR-005; the fork predicates they evaluate are defined in `B-01`.
Source: `consensus/wbft/engine/engine.go:196-350`
Observable: header

[WBFT-HDR-081] (H2) A verifier MUST return `ErrFutureBlock` when `Time > now() + AllowedFutureBlockTime`, where `AllowedFutureBlockTime` is the node's configured value (not transition-dependent, default `0`). Within `verify_header` this check precedes all others except H1. The validator-set lookup (V0a, V0b, §6.1) and, in proposal verification, steps P1–P5 run before it, so their failures (for example a missing parent, which V0b reports as `ErrUnknownAncestor` for `uint64(Number) ≥ 2`) are reported for a future header at once, without waiting.
Source: `consensus/wbft/engine/engine.go:201-205`, `consensus/wbft/config.go:110, 116-122`, `consensus/wbft/backend/engine.go:76-79, 429-440`, `consensus/wbft/backend/backend.go:273-277`
Observable: header

[WBFT-HDR-082] (H4) A verifier MUST reject a header whose `Difficulty` is absent or not equal to `1` with `ErrInvalidDifficulty`.
Source: `consensus/wbft/engine/engine.go:213-215`
Observable: header

[WBFT-HDR-083] (H10) A verifier MUST accept a header with `uint64(Number) = 0` (that is, `Number ≡ 0 mod 2^64`, including the genesis number 0) once H1–H9 pass, without examining its extra data, parent, timestamp or signer. This is the rule of `verify_header` once `V` and `Vp` are given. On a node, `V` cannot be determined for a header with `uint64(Number) = 0` and `Number ≠ 0` (V0a returns `ErrUnknownAncestor`), and a proposal with such a number fails the `Sequence = Number` check, so such a header is rejected before H10 on every node route; an observer only ever sees H10 applied to the genesis header. (Genesis validity is `B-02`. The genesis block is never imported through header verification: the reference passes it through `verify_header` only at start-up, as the current header of a fresh database, and ignores the result (`core/blockchain.go:424`). A genesis that fails H1–H9, such as the testnet genesis with `Difficulty = 0` (`B-02`), is therefore still used.)
Source: `consensus/wbft/engine/engine.go:250-253`
Observable: header

[WBFT-HDR-084] (H11) For `Number ≥ 1` a verifier MUST obtain the parent from the batch (`parents[-1]`) if one is given, otherwise from its chain by `(ParentHash, Number − 1)`, and MUST return `ErrUnknownAncestor` if it is missing, its number is not `Number − 1`, or its hash is not `ParentHash`.
Source: `consensus/wbft/engine/engine.go:256-265`
Observable: header

[WBFT-HDR-085] (H12) A verifier MUST return `ErrInvalidTimestamp` when `parent.Time + config_at(Number).BlockPeriod > Time`. The block period is the one in force at the child's height, so a transition that changes `BlockPeriod` at block `k` applies to the interval between `k−1` and `k`.
Source: `consensus/wbft/engine/engine.go:267-272`
Observable: header

[WBFT-HDR-086] (H15a) A verifier MUST return `ErrUnauthorized` when `Coinbase` is not an address in `V`.
Source: `consensus/wbft/engine/engine.go:365-368`
Observable: header

[WBFT-HDR-088] (H16) A verifier MUST return `ErrInvalidExtraDataFormat` when `Extra` does not decode as `WBFTExtra` (`A-03`: exactly ten list elements, no trailing bytes).
Source: `consensus/wbft/engine/engine.go:301-304`, `core/types/istanbul.go:122-149, 251-258`
Observable: header

[WBFT-HDR-089] (H18) A verifier MUST apply the randao reveal check of `A-02` §7.2 at step H18 of the procedure of §6.2, after step H17 and before the randao-mix check of step H19 (WBFT-HDR-090). The error message is prefixed `failed to verify randao reveal signature:`.
Source: `consensus/wbft/engine/engine.go:313-315, 563-573`, `consensus/wbft/backend/backend.go:292-303`, `consensus/wbft/utils.go:39-48`
Observable: header

[WBFT-HDR-090] (H19) A verifier MUST reject a header unless `MixDigest == randao_mix(parent.MixDigest, RandaoReveal)`; the error message is prefixed `invalid randao mix:`.
Source: `consensus/wbft/engine/engine.go:316-319, 575-581`
Observable: header

[WBFT-HDR-091] (H20) For `Number ≥ 2` a verifier MUST verify the previous-block seals (§6.5); for `Number = 1` it MUST NOT examine `PrevRound`, `PrevPreparedSeal` or `PrevCommittedSeal` (any values are accepted).
Source: `consensus/wbft/engine/engine.go:82-84, 321-327`
Observable: header

#### 6.2.1 Number range

### 6.3 Aggregated seal verification

The procedure is `verify_aggregated_seal(V, header, round, agg, seal_type)` of `A-02` §5.7 (WBFT-CRYPTO-030); this chapter uses it without restating it. Header verification applies it at two steps: `verify_seals` (§6.4) checks the block's own prepared and committed seals with the set `V` of the block's height, and `verify_prev_seals` (§6.5) checks the previous seals with the set `Vp` of the parent's height. A failure inside it is reported with the outer error of the calling step (WBFT-HDR-102). WBFT-HDR-100 and WBFT-HDR-101 state what a verifier observes of it at those steps.

[WBFT-HDR-100] A verifier MUST accept an aggregated seal only if (a) the number of distinct indices set in its bitmap is at least `quorum_size(|vs|)`, (b) every index is less than `|vs|`, and (c) the sum of the BLS public keys of the indexed validators is not the point at infinity and the signature verifies as a single BLS signature over `seal_data(header, round, seal_type)` under that sum (`A-02` WBFT-CRYPTO-055). When the indexed keys sum to the point at infinity the seal MUST be rejected with `ErrInvalidSeal` even if the signature is also the point at infinity, for which the pairing equation holds trivially. Bitmap bytes beyond the highest set bit (trailing zero bytes) MUST be accepted.
Source: `consensus/wbft/engine/engine.go:1338-1369`, `core/types/istanbul.go:315-325`, `crypto/bls/blst/signature.go:119-122`, blst v0.3.16 `src/aggregate.c:294-297`
Observable: header

[WBFT-HDR-101] The index `i` in a sealer bitmap MUST be interpreted as the `i`-th validator of the set passed to the check, which is ordered as `EpochInfo.Validators` of the defining epoch block (no re-sorting).
Source: `consensus/wbft/validator/validator.go:35-41`, `consensus/wbft/validator/default.go:71-89, 113-120`, `consensus/wbft/engine/engine.go:1115`
Observable: header

### 6.4 Seals of the block itself (`verify_seals`)

```python
def verify_seals(header, V, x):
    if header.Number == 0: return None                         # unreachable after H10
    if x.PreparedSeal is None or len(x.PreparedSeal.signature) == 0:  raise ErrEmptyPreparedSeals
    if verify_aggregated_seal(V, header, x.Round, x.PreparedSeal, PREPARE_SEAL):  raise ErrInvalidPreparedSeals
    if x.CommittedSeal is None or len(x.CommittedSeal.signature) == 0: raise ErrEmptyCommittedSeals
    if verify_aggregated_seal(V, header, x.Round, x.CommittedSeal, COMMIT_SEAL):  raise ErrInvalidCommittedSeals
```

Source: `consensus/wbft/engine/engine.go:414-449`

[WBFT-HDR-102] When `check_seals` is true, a verifier MUST check, in this order: `PreparedSeal` present with a non-empty signature (`ErrEmptyPreparedSeals`); `PreparedSeal` valid for `(V, header, Extra.Round, PREPARE_SEAL)` (`ErrInvalidPreparedSeals`); `CommittedSeal` present with a non-empty signature (`ErrEmptyCommittedSeals`); `CommittedSeal` valid for `(V, header, Extra.Round, COMMIT_SEAL)` (`ErrInvalidCommittedSeals`). Every inner failure of `verify_aggregated_seal` MUST be reported with the outer error of that step.
Source: `consensus/wbft/engine/engine.go:414-449`
Observable: header

### 6.5 Previous-block seals (`verify_prev_seals`)

```python
def verify_prev_seals(parent, Vp, x):
    if parent.Number == 0: return None                         # parent is the genesis header: previous seals are not checked
    if x.PrevPreparedSeal is None or len(x.PrevPreparedSeal.signature) == 0:   raise ErrEmptyPrevPreparedSeals
    if verify_aggregated_seal(Vp, parent, x.PrevRound, x.PrevPreparedSeal, PREPARE_SEAL):   raise ErrInvalidPrevPreparedSeals
    if x.PrevCommittedSeal is None or len(x.PrevCommittedSeal.signature) == 0: raise ErrEmptyPrevCommittedSeals
    if verify_aggregated_seal(Vp, parent, x.PrevRound, x.PrevCommittedSeal, COMMIT_SEAL):   raise ErrInvalidPrevCommittedSeals
```

Source: `consensus/wbft/engine/engine.go:384-411`

[WBFT-HDR-103] For `Number ≥ 2`, a verifier MUST check the previous-block seals of header `h` against the **parent header** (as resolved in H11, not the verifier's own copy of the seals in it), the parent's validator set `Vp`, and the round `h.Extra.PrevRound`, in the order and with the errors of `verify_prev_seals`. Because `seal_data` is computed from the parent with its seals and round removed and `PrevRound` substituted, the parent's own `Round`, `PreparedSeal` and `CommittedSeal` do not influence the result.
Source: `consensus/wbft/engine/engine.go:322-327, 384-411`, `core/types/istanbul.go:269-288`
Observable: header

The previous seals are the only place where a seal set is committed into a block hash: `PrevPreparedSeal` and `PrevCommittedSeal` of block `h` are covered by `block_hash(h)` (`WBFT-HDR-041`), so they are identical on every node and are the canonical record of who sealed block `h−1` (`A-04` uses them for diligence).

### 6.6 State-dependent steps and when they are skipped

Two steps read the parent state: H15b (blacklist) and H21 (gas tip). They are the *state-dependent* steps of header verification; their skip semantics when the parent state is missing are the ones `B-09` SNET-SYNC-010 relies on for synchronisation (skip instead of reject). The state is unavailable in the following situations, all of which occur in the reference implementation:

| Situation | Why the parent state is missing | Source |
|---|---|---|
| Header-chain insertion during snap sync | the header chain has no state at all; `StateAt` always fails | `core/headerchain.go:327, 437-438` |
| `InsertChain` batch, header `i ≥ 1` | headers are verified ahead of execution by a separate goroutine; whether header `i−1` has been executed when header `i` is verified depends on timing | `consensus/wbft/backend/engine.go:87-112`, `core/blockchain.go:1587-1592, 1744-1792` |
| Parent state pruned | the node keeps only recent states | `core/blockchain.go:1646-1656` |
| Snap-sync receipt import | bodies and receipts are imported without execution | comment `consensus/wbft/engine/engine.go:340-346` |

H21 re-reads the parent from the chain by `(ParentHash, uint64(Number) − 1)` and ignores `parents` (`consensus/wbft/engine/engine.go:627-630`). In a batch, a header `i ≥ 1` whose parent is not yet stored therefore skips H21 (missing parent), even though H15b used the parent from the batch.

[WBFT-HDR-111] (H21, error handling only; value rule in `B-06`) A verifier MUST fail verification when the gas-tip check reports a mismatch (`GasTipMismatchError`, including `Extra.GasTip = nil`) or `ErrGasTipContractUnavailable`, and MUST skip the check (verification succeeds as far as H21 is concerned) for every other error, including: no system-contract configuration, missing parent, parent `Root` equal to zero, parent state unavailable, and an `Extra` decode failure inside the check. In the reference implementation two of these failure branches are unreachable: `ErrGasTipContractUnavailable` is returned only when `GetGasTip` returns `nil`, which it never does (an empty slot reads as 0; `B-06` SNET-FIN-017, `B-04`), and a decoded `Extra.GasTip` is never `nil` (`A-03` WBFT-ENC-008). The effective rule is therefore: fail on a value mismatch, skip on every other error.
Source: `consensus/wbft/engine/engine.go:329-348, 622-645, 1280-1295`

### 6.7 Batch semantics (`VerifyHeaders`)

```python
def verify_header_batch(headers) -> list[Error | None]:
    results = []
    failed = False
    for i, h in enumerate(headers):
        if failed:
            results.append(ErrUnknownAncestor)
            continue
        V, Vp = validators_for_verifying(h, headers[:i])      # may raise -> result
        err = verify_header(h, headers[:i], V, Vp, check_seals=True)
        results.append(err)
        failed = failed or err is not None
    return results
```

Source: `consensus/wbft/backend/engine.go:87-112`

[WBFT-HDR-120] A batch verifier MUST verify headers sequentially in input order, passing the preceding headers of the batch as `parents`, and after the first failure MUST report `ErrUnknownAncestor` for every remaining header without verifying it. Results MUST be delivered in input order; delivery MAY stop early if the caller aborts.
Source: `consensus/wbft/backend/engine.go:87-112`

Informative (caller behaviour, `A-09 §5.2`). `InsertChain` inspects the first result, which is the body validation result if header verification passed (`core/blockchain_insert.go:129-133`): `ErrFutureBlock` (and `ErrUnknownAncestor` whose parent is queued as future) sends the blocks to the future queue; `ErrPrunedAncestor` and `ErrKnownBlock` are handled separately; any other error from header verification or body validation records the first block as a bad block and aborts (`core/blockchain.go:1595-1680`). A header verification failure therefore also creates a bad-block record for that hash.

### 6.8 Genesis and block 1

| Height | V | Vp | H11–H19 | H20 | Notes |
|---|---|---|---|---|---|
| 0 | genesis configuration validators and BLS keys | = V | not run (H10) | not run | only H1–H9; `Extra` is not decoded |
| 1 | `EpochInfo` of the genesis header | = V | run; parent is genesis; `MixDigest(1) = randao_mix(genesis.MixDigest, reveal)` | not run | `PrevRound`/prev seals unconstrained (`WBFT-HDR-091`) but produced empty (`WBFT-HDR-033`) |
| 2 | `EpochInfo` of the last epoch block ≤ 1 | V(1) | run | run against block 1 with `Vp = V(1)` | first block that carries previous seals |

Source: `consensus/wbft/engine/engine.go:82-84, 250-253, 1097-1117`, `consensus/wbft/backend/engine.go:429-447`

[WBFT-HDR-121] For block 1 the validator set MUST be taken from the genesis header's `EpochInfo` (the genesis header is the epoch block for height 0), not from the genesis configuration; the genesis configuration set is used only for height 0 itself.
Source: `consensus/wbft/engine/engine.go:1104-1116, 1407-1415`
Observable: header

---

## 7. Epoch blocks and `EpochInfo` presence

`is_epoch_block(number)` is defined in `A-04`: with `(first, length)` the block and epoch length of the last transition with `Block ≤ number` and non-zero `EpochLength` (or `(0, Epoch)` if none), `number` is an epoch block iff `(number − first) mod length == 0`. Every transition block that sets a non-zero `EpochLength` is itself an epoch block.

Source: `consensus/wbft/engine/engine.go:1073-1090`

[WBFT-HDR-130] For every block with `Number ≥ 1`, `Extra.EpochInfo` MUST be non-`nil` if and only if `is_epoch_block(Number)`.
Source: `consensus/wbft/engine/engine.go:948-961, 1212-1224`
Observable: header

[WBFT-HDR-131] The reference implementation enforces `WBFT-HDR-130` only when the block is **executed** (`Finalize`), after the system-contract transition and base-fee distribution: a non-epoch block with `EpochInfo` fails with `ErrEpochInfoIsNotNil`; an epoch block without `EpochInfo` fails with `WBFT: epochInfo is nil`, unless recomputing the epoch information (`buildEpochInfo`) fails first, in which case that error is returned. Header verification (§6) does not enforce it. A conforming verifier MUST reject such blocks no later than execution and MAY reject them at header verification.
Source: `consensus/wbft/engine/engine.go:929-961, 1213-1224`, `core/state_processor.go:102`

[WBFT-HDR-132] The content of `EpochInfo` on an epoch block MUST equal the value recomputed from the chain and the post-execution state of that block (`A-04` algorithm, `B-06` comparison); header verification does not check it.
Source: `consensus/wbft/engine/engine.go:1212-1263`
Observable: header, state

Consequences for header-only paths (informative). A header chain inserted without execution (snap sync) can contain a non-epoch block with `EpochInfo` without detection. An epoch block without `EpochInfo` is detected indirectly: verifying any later header needs `validators_at`, which fails with `WBFT: epochInfo is nil`, reported as `ErrUnknownAncestor` (`WBFT-HDR-070`).

---

## 8. Light verification (`verify_light`)

`verify_light` is the procedure an external, stateless verifier (inspector, bridge) uses to decide whether a header is a correctly finalized WBFT header. It is the subset of §6 that needs only headers, plus the `EpochInfo` presence rule.

```python
class LightResult(Enum):
    VALID = 0
    INVALID = 1            # a rule is violated; the reason names the step
    CANNOT_DECIDE = 2      # an input needed for a decision is missing

def verify_light(h, parent, V, Vp, chain_id, cfg, check_future=False) -> (LightResult, str):
    # Inputs: h and parent (block_hash(parent) == h.ParentHash);
    # V  = validators_at(h.Number), Vp = validators_at(h.Number - 1), both obtained from
    #      EpochInfo of epoch headers that were themselves verified with verify_light,
    #      back to the genesis header (trust root: genesis EpochInfo and configuration).
    if h.Number == 0:                          return CANNOT_DECIDE, "genesis: B-02"
    if parent is None or V is None:            return CANNOT_DECIDE, "missing ancestor"
    if check_future and h.Time > now() + cfg.AllowedFutureBlockTime:
                                               return CANNOT_DECIDE, "future"        # H2
    if h.Difficulty != 1:                      return INVALID, "H4"
    if uint64(parent.Number) != h.Number - 1 or block_hash(parent) != h.ParentHash:
                                               return INVALID, "H11"
    if parent.Time + config_at(h.Number).BlockPeriod > h.Time:
                                               return INVALID, "H12"
    if h.Coinbase not in V:                    return INVALID, "H15a"
    try: x = decode_extra(h.Extra)
    except: return INVALID, "H16"
    if verify_seals(h, V, x) fails:            return INVALID, "H17"
    if ecdsa_recover_address(randao_data(chain_id, h.Number), x.RandaoReveal) != h.Coinbase:
                                               return INVALID, "H18"
    if randao_mix(parent.MixDigest, x.RandaoReveal) != h.MixDigest:
                                               return INVALID, "H19"
    if h.Number >= 2:
        if Vp is None:                         return CANNOT_DECIDE, "missing Vp"
        if verify_prev_seals(parent, Vp, x) fails: return INVALID, "H20"
    if (x.EpochInfo is not None) != is_epoch_block(h.Number):
                                               return INVALID, "HDR-130"
    # Optional: header-only Part B rules H3, H5-H9, H13, H14 (B-03).
    return VALID, ""
```

[WBFT-HDR-140] A light verifier that returns `VALID` for a header MUST have checked H4, H11, H12, H15a, H16, H17, H18, H19, H20 (for `Number ≥ 2`) and `WBFT-HDR-130`, with the same definitions as §6 for `Number < 2^64`.
Source: `consensus/wbft/engine/engine.go:196-350, 948-961`
Observable: header

[WBFT-HDR-141] A light verifier MUST derive `V` and `Vp` only from `EpochInfo` of epoch headers that it has itself accepted, starting from the genesis header, and MUST return `CANNOT_DECIDE` rather than `INVALID` when a required ancestor or epoch header is missing.
Source: none in the reference (design requirement for external verifiers). Contrast `consensus/wbft/engine/engine.go:1414, 1427-1428`: the reference takes the epoch header from its canonical chain by number without checking its ancestry, and reports a missing one as `ErrUnknownAncestor`.
Observable: header

[WBFT-HDR-142] A light verifier MUST NOT compare `Round`, `PreparedSeal` or `CommittedSeal` of a header with the previous-seal fields of its child (`WBFT-HDR-053`).
Source: `consensus/wbft/backend/backend.go:221-249`
Observable: header

What `verify_light` cannot establish (informative):

| Property | Why not | Who establishes it |
|---|---|---|
| `Coinbase` not blacklisted (H15b) | needs parent state | quorum at PRE-PREPARE; full node with state |
| Gas tip value (H21) | needs parent state | `B-06` at execution |
| `EpochInfo` content (`WBFT-HDR-132`) | needs post-execution state and candidate list | `B-06` at execution. Trust in `V` for the next epoch rests on the quorum of the current epoch having sealed the epoch block |
| State root, receipts, bloom, gas used | need execution | `A-09 §5` |
| That the header is the one every node finalized | seals prove a quorum signed this digest; uniqueness per height holds only under `f < N/3` (`A-10 §2`) | — |
| Future-time rule (H2) | depends on the verifier's clock; meaningless for historical headers | live nodes only |

---

## 9. Head selection and finality (informative)

WBFT decides one block per height; the execution layer nevertheless keeps the go-ethereum total-difficulty head selection.

- `Difficulty = 1` for every WBFT block, so `TD(h) = TD(genesis) + h` along any chain (`core/forkchoice.go:77-113`, `consensus/wbft/engine/engine.go:1067-1069`). A longer chain always wins.
- Between two different blocks at the same height, the rule prefers the one authored by a local account (`Coinbase` equals `etherbase` or a `txpool.locals` address), otherwise chooses at random with probability 1/2 (`core/forkchoice.go:98-111`, `eth/backend.go:376-423`). Seals are not consulted.
- Under `f < N/3` two different blocks with valid committed seals at one height do not exist, so this rule is never exercised by finalized blocks. If the assumption fails, nodes may choose different heads and later reorganize to whichever branch grows longer.
- Consensus advances the `finalized` and `safe` markers only when a block is imported through the block fetcher path (`eth/handler.go:298-305`); the proposer's own path and the downloader do not advance them (`A-09 §5`). Other writers of the markers (Engine API `forkchoiceUpdated`, restart) are listed in `B-09` §7.3.

[WBFT-HDR-150] A conforming node MUST treat a block that passed `verify_header` with `check_seals = true` and was executed successfully as final; it MUST NOT revert it through its own fork choice unless a longer chain of valid blocks is presented (reference behaviour; under `f < N/3` this cannot happen).
Source: `core/forkchoice.go:77-113`, `core/blockchain.go:1456-1479`

---

## 10. Worked examples

### 10.1 Quorum sizes used by `verify_aggregated_seal`

`quorum_size(n) = ceil(n − (n − 1)/3)` in float64:

| n | (n−1)/3 | quorum |
|---|---|---|
| 1 | 0 | 1 |
| 4 | 1 | 3 |
| 5 | 1.333… | 4 |
| 6 | 1.666… | 5 |
| 7 | 2 | 5 |
| 10 | 3 | 7 |
| 100 | 33 | 67 |

### 10.2 Sealer bitmap

`Sealers = 0x0b01` (two bytes). Byte 0 = `0x0b = 0b00001011` sets bits 0, 1, 3; byte 1 = `0x01` sets bit 0 → index 8. Indices = `[0, 1, 3, 8]`, count 4. With `|V| = 7` (quorum 5) the seal fails with `lack of seal count`. With `|V| = 5` (quorum 4) it fails with `sealer is not validator` (index 8 ≥ 5). `0x0b0100` is accepted as the same set (`WBFT-HDR-100`).

### 10.3 Timestamp

`parent.Time = 1_700_000_000`, `BlockPeriod(n) = 1`, `AllowedFutureBlockTime = 0`.

- Proposer clock `1_700_000_000` → `Time = 1_700_000_001`. A verifier whose clock reads `1_700_000_000` returns `ErrFutureBlock` with duration ≈ 1 s and re-processes the PRE-PREPARE then.
- Proposer clock `1_700_000_003` → `Time = 1_700_000_003`; valid at H12 (`1_700_000_001 ≤ 1_700_000_003`).
- A header with `Time = 1_700_000_000` fails H12 with `ErrInvalidTimestamp`.

### 10.4 Previous seals with extra seals

`|V(n−1)| = 4`, quorum 3. The proposer's copy of block `n−1` has `Round = 0`, `PreparedSeal.sealers = 0x07` (indices 0, 1, 2), `CommittedSeal.sealers = 0x0b` (0, 1, 3). Extra seals for view `(n−1, 0)`: PREPARE from indices 2 and 3, COMMIT from index 2.

- `PrevPreparedSeal.sealers = 0x0f`, signature = aggregate(old PREPARE aggregate, PREPARE seal of 3); index 2 skipped.
- `PrevCommittedSeal.sealers = 0x0f`, signature = aggregate(old COMMIT aggregate, COMMIT seal of 2).
- `PrevRound = 0`.

If the proposer's `prior_round` were 1 while `Extra(n−1).Round = 0`, the extra seals would be for round 1; the merged aggregates would mix rounds, and every verifier would return `ErrInvalidPrevPreparedSeals` (H20) for this proposal (`WBFT-HDR-036`).

### 10.5 Epoch blocks and validator sets

`Epoch = 10`, one transition `{Block: 25, EpochLength: 7}`.

- Epoch blocks: 0, 10, 20, 25, 32, 39, …
- `V(11)` … `V(20)` come from `EpochInfo(10)`; block 20 carries `EpochInfo` for heights 21–25; `V(26)` … `V(32)` come from `EpochInfo(25)`.
- Block 20 is sealed by `V(20)` (from `EpochInfo(10)`); its previous seals (block 19) are checked with `Vp = V(19)`, also from `EpochInfo(10)`. Block 21 is sealed by `V(21)` from `EpochInfo(20)`, while its previous seals (block 20) are checked with `Vp = V(20)`: across an epoch boundary `V` and `Vp` differ.

### 10.6 Ordered failure

A header at height 50 with `Difficulty = 2`, `Time` 10 s in the future and a missing parent, verified at engine level (`verify_header` with given `V`, `Vp`), first returns `ErrFutureBlock` (H2 precedes H4 and H11); re-verified after ≈ 10 s it returns `ErrInvalidDifficulty` (H4 precedes H11), and with `Difficulty = 1` it would return `ErrUnknownAncestor` (H11).

Through `Backend.Verify` (proposal verification) and `Backend.VerifyHeader`, however, V0b needs the parent for `uint64(Number) ≥ 2` and returns `ErrUnknownAncestor` before H2 is reached, whatever the canonical chain contains. Proposal verification therefore rejects this proposal at once (duration 0) and schedules no re-check. The engine-level order is observed on a node only for a header `i ≥ 1` of a batch, whose parent comes from `parents`.

---

## 11. Implementation notes (informative)

- `sigHash`/`SealHash` equals `block_hash` (`consensus/wbft/engine/engine.go:1063-1065, 1155-1160`); the miner uses it to match sealing results to tasks.
- `Backend.VerifySeal` (`consensus/wbft/backend/engine.go:122-136`, `consensus/wbft/engine/engine.go:462-480`) checks difficulty, parent and signer only; no caller in the node uses it.
- `CallEngineSpecific("InheritExtra" | "SetMixDigest" | "SetCoinbase")` (`consensus/wbft/backend/engine.go:329-408`) reproduces parts of `prepare_proposal_header` for the test chain generator (`core/chain_makers.go:483-500`) and is not a normative construction path. Unlike `prepare_proposal_header` it copies the parent's seals without merging and uses the parent by hash.
- If `Extra` cannot be decoded, `Header.Hash` falls back to hashing the full header (`core/types/block.go:121-131`); such a header fails H16, so the fallback matters only for the digest carried in messages about it.
- `prepare_proposal_header` dereferences the canonical header at `n−1` without a `nil` check (`consensus/wbft/engine/engine.go:516-517`); if no canonical header exists at that number the reference implementation panics.
- Non-validator nodes also run `prepare_proposal_header` when they build their pending block, and therefore sign randao data with their node key (`miner/worker.go:644-662, 1320-1343`).
