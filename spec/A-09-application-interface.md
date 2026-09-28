# A-09 Application interface

- Area code: `APP`
- Status: draft
- Reference: go-stablenet `740526d03`.

This chapter defines, abstractly, what the WBFT consensus protocol requires from the execution layer (the *application*), as the reference implementation realises it today. It fixes operations, inputs, outputs, failure semantics, and the ordering and concurrency that a consensus implementation may rely on. `B-08` binds the state-reading operations to StableNet contract storage.

The interface is observed, not designed: in the reference implementation it is spread over `consensus/wbft/core.Backend` (`consensus/wbft/core/types.go:93-142`), the `consensus.Engine` methods of `consensus/wbft/backend`, the miner worker, and `core.BlockChain`.

---

## 1. Model

Two parties:

- **Consensus** — the state machine of `A-05`, the header rules of `A-08`, the validator/epoch logic of `A-04`.
- **Application** — block storage and head, transaction pool, block building and execution, state, system contracts.

Three facts shape the interface:

1. **Execution happens after consensus for everyone except the proposer.** The proposer executes the block while building it (before proposing). Other validators verify only header rules and the transaction root at PRE-PREPARE and execute the block after it is finalized (§5).
2. **Consensus reads application state at two points**: the parent state of a height (proposer eligibility, gas tip) and the post-execution state of an epoch block (candidates). The parent-state reads are made synchronously from inside header rules (`A-08` H15b, H21); the candidate read is made inside finalization of the epoch block (`B-06` §4), never during header verification.
3. **There is no acknowledgement from execution to consensus.** `finalize` (§4.7) hands the block over; if execution then fails, consensus learns about it only indirectly (a timer fires and the bad-block record is consulted, §5.3).

```
                   consensus                                  application
   new height ─────────────────────────────────────────────── on_new_head()     (§4.8)
   build   ───── ready_to_build(wait, round) ──────────────▶  build_proposal()  (§4.4)
                 ◀──── submit_proposal(block) ─────────────
   PRE-PREPARE ─ validate_proposal(block) ───────────────▶   (header rules + body roots, §4.6)
                 is_bad_block, header lookups, eligibility, gas tip
   COMMIT quorum ─ finalize(block, prepared, committed, round) ─▶ commit path (§5)
                                                              execution, storage, head
                 ◀──── on_new_head() ───────────────────────
```

---

## 2. Types

```python
class HeadInfo:              # §4.1
    block: Block             # full block of the current head (header used by consensus)
    proposer: Address        # Coinbase of the head; zero address for genesis

class ConsensusAttributes:   # §4.4, fields set by consensus in the proposal header (A-08 §3)
    coinbase: Address
    difficulty: int          # 1
    nonce: bytes8            # zero
    time: uint64
    mix_digest: Hash
    extra: WBFTExtra         # vanity, randao reveal, prev round/seals, gas tip (EpochInfo later)

class Eligibility(Enum):     # §4.3
    ELIGIBLE = 0
    INELIGIBLE = 1           # blacklisted
    UNKNOWN = 2              # parent state not available

class CandidateEntry:        # §4.2 (binding in B-08); not A-01's Candidate (addr, diligence)
    addr: Address
    bls_public_key: bytes    # 48 bytes or empty if none registered
```

---

## 3. Conventions

- "Parent state of height `n`" is the state after executing block `n−1`, identified by `Root` of the header `ParentHash` points to.
- "Post-execution state of block `n`" is the state after executing the transactions of block `n` and the finalization steps of `B-06` that precede the epoch handling (system-contract upgrades at `n`, base-fee distribution).
- Operation names are abstract; the table in §7 maps them to the reference implementation.

---

## 4. Operations

### 4.1 Chain queries

| Operation | Returns | Reference |
|---|---|---|
| `head()` | `HeadInfo` of the current canonical head (full block) | `Backend.LastProposal`, `consensus/wbft/backend/backend.go:327-342`; `currentBlock = chain.CurrentFullBlock`, `miner/worker.go:447` |
| `header_by_number(n)` | canonical header at canonical index `n`, or none | `ChainHeaderReader.GetHeaderByNumber` |
| `header(hash, n)` | header with that hash whose canonical index is `n`, or none | `ChainHeaderReader.GetHeader` |
| `header_by_hash(hash)` | header, or none | `ChainHeaderReader.GetHeaderByHash` |
| `has_block(hash, n)` | bool | `Backend.HasProposal`, `backend.go:306-308` |
| `proposer_of(n)` | `Coinbase` of the canonical header at `n`, zero address if none | `Backend.GetProposer`, `backend.go:311-317` |
| `is_bad_block(hash)` | bool: a bad-block record exists for `hash` | `Backend.HasBadProposal`, `backend.go:344-349` → `rawdb.HasBadBlock` |
| `now()` | local wall-clock seconds | `time.Now()` in `consensus/wbft/engine/engine.go:202, 503` |

`has_block` and `proposer_of` are part of the reference `Backend` interface (`consensus/wbft/core/types.go:128-134`) but are not called by the consensus core at the reference commit.

The number argument `n` of these queries is a `uint64` *canonical index*: the low 64 bits of `Number`. go-ethereum stores a header under `(uint64(Number), hash)` and the canonical mapping under `uint64(Number)`, so a query with `n` finds a header whose `uint64(Number)` equals `n`. Below `2^64` this is the header with number `n`. Consensus passes `uint64(...)` values to these queries. (`header(hash, n)` returns a header held in the header cache for `hash` without looking at `n`.)
Source: `core/rawdb/accessors_chain.go:394-400`, `core/blockchain.go:942`, `core/headerchain.go:443-448, 479-485`

[WBFT-APP-001] The application MUST provide `head()` returning the full canonical head block; consensus uses its number, hash and `Coinbase` to start the next height and to compute the next proposer.
Source: `consensus/wbft/backend/backend.go:327-342`, `consensus/wbft/core/core.go:178-246`

[WBFT-APP-003] `header_by_number` MUST return the header that the application currently considers canonical at that number. Consensus uses it for the previous seals (`WBFT-HDR-030`), by number only, and for the epoch header lookup (`WBFT-HDR-071`), by number first; the ancestry of the header being processed is followed only when no canonical header exists at the epoch number.
Source: `consensus/wbft/engine/engine.go:516, 1414-1433`

[WBFT-APP-004] `is_bad_block(hash)` MUST return true for every block that the import pipeline reported as bad and MUST keep returning true while the record is retained. The reference reports a header or body validation failure of the first block of an import batch, other than a future, pruned-ancestor, known-block or queued-unknown-ancestor outcome, and an execution or state-validation failure of any block of the batch (§5.2). A header or body validation failure of a later block of the batch is not reported, and a block dropped by the block fetcher's header check before import is not reported either. The reference retains only the 10 highest-numbered bad-block records (`badBlockToKeep = 10`): after a record is added, the list is sorted by descending number with an unstable sort and cut to 10, so a new record lower than all 10 retained records is discarded at once, and a retained record is evicted by 10 later records at the same or a greater height. (Editorial correction: an earlier draft said records never expire.)
Source: `core/blockchain.go:1647-1679, 1693, 1778-1792, 1909, 2422-2425`, `eth/fetcher/block_fetcher.go:858-872`, `consensus/wbft/backend/backend.go:344-349`, `core/rawdb/accessors_chain.go:848, 913-928, 991-993`

### 4.2 Candidates at an epoch block

```python
def candidates(epoch_header, post_state) -> list[CandidateEntry]
```

[WBFT-APP-010] At an epoch block `n ≥ 1`, consensus MUST obtain the candidate list and each candidate's BLS public key from the **post-execution state of block `n` itself**, not from the parent state, using the validator-registry location in force at height `n` (`B-08`).
Source: `consensus/wbft/engine/engine.go:806, 875, 894, 929-953`, `consensus/wbft/engine/engine.go:606-612`
Observable: state, header

[WBFT-APP-011] The candidate query MUST be a pure function of `(n, post_state)`: it MUST return the same ordered list to the proposer (while building) and to every verifier (while executing), because the order of candidates is part of `EpochInfo`.
Source: `consensus/wbft/engine/engine.go:806-872, 1226-1240`
Observable: header

[WBFT-APP-012] A candidate without a registered BLS key MUST be returned with an empty key; consensus then leaves it out of the next validator set (it stays in the candidate list). The query itself has no failure mode in the reference implementation.
Source: `consensus/wbft/engine/engine.go:892-904`, `systemcontracts/gov_validator.go:212-215`
Observable: header

The candidate query is issued from inside `build_proposal` when the local node is a validator of the epoch block's height (§4.4, while running `process_finalize` on an epoch block being built; otherwise no `EpochInfo` is written, `consensus/wbft/engine/engine.go:1193-1198`) and from inside execution of an epoch block (§5). It is therefore never issued without the post-execution state at hand.

### 4.3 Parent-state queries: eligibility and gas tip

```python
def is_eligible_proposer(parent: Header, addr: Address) -> Eligibility
def gas_tip(parent: Header) -> uint256 | Error
```

[WBFT-APP-021] `gas_tip(parent)` MUST return the gas tip at the parent state (value rules `B-06`, location `B-08`). Consensus uses it (a) to fill `Extra.GasTip` of a proposal, where any error aborts the proposal, and (b) to check `Extra.GasTip` during header verification and execution, with the error classes of `WBFT-HDR-111`.
Source: `consensus/wbft/engine/engine.go:511-513, 622-645, 963-965, 1280-1295`

Eligibility is only checked, never used to pick proposers: `calc_proposer` (`A-04`) ignores the blacklist, so a blacklisted validator is still selected as proposer.

### 4.4 Building a proposal

Consensus does not build blocks; it asks the application to build one and to call back into consensus for the consensus fields.

```python
# consensus -> application
def ready_to_build(wait: Duration, round: int) -> None
# application -> consensus (during building)
def prepare_consensus_fields(header) -> ConsensusAttributes   # A-08 §3 (prepare_proposal_header)
def compute_epoch_info(header, post_state) -> EpochInfo | None # A-04 / B-06, epoch blocks only
# application -> consensus (result)
def submit_proposal(block) -> None
```

Sequence in the reference implementation:

1. On every new round, consensus calls `ready_to_build(wait, round)` with `wait = head.Time + BlockPeriod(head.Number + 1) − now` for round 0 and `wait = 0` otherwise (`consensus/wbft/backend/engine.go:139-150, 277-285`, `consensus/wbft/core/core.go:260`).
2. After `wait`, the application creates the header skeleton on top of its current head, calls `prepare_consensus_fields`, executes transactions from its pool, runs `process_finalize` (`B-06`) including `compute_epoch_info` on an epoch block, and assembles the block (`miner/worker.go:432-441, 664-669, 1119-1198, 1320-1425`, `consensus/wbft/engine/engine.go:1053-1061, 1193-1207`).
3. The application hands the block to consensus (`Seal` → `RequestEvent`, `consensus/wbft/backend/engine.go:186-219`). If the block number equals the current sequence, consensus stores the block as the pending request. It sends a PRE-PREPARE at once only in round 0, in state `AcceptRequest`, and only if the node is the proposer of the current view (`consensus/wbft/core/request.go:47-55`, `consensus/wbft/core/preprepare.go:51`). In later rounds the pending request is proposed after a ROUND-CHANGE quorum (`A-05`). A block for a future sequence is queued and re-submitted when that sequence starts (`consensus/wbft/core/request.go:94-117`).

[WBFT-APP-030] The application MUST build every proposal on top of its current head and MUST let consensus set `Coinbase`, `Difficulty`, `Nonce`, `Time`, `MixDigest` and `Extra` (except `EpochInfo`) before executing transactions, because transaction execution reads `Time`, `Coinbase`, `MixDigest` (PREVRANDAO) and `Extra.GasTip`.
Source: `miner/worker.go:1124, 1180-1187`, `core/state_processor.go:84`, `core/evm.go:41-77`
Observable: header, state

[WBFT-APP-031] On an epoch block the application MUST obtain `EpochInfo` from consensus **after** executing the transactions and the preceding finalization steps and **before** computing the block hash. Consequently `EpochInfo` cannot be an input to block building.
Source: `consensus/wbft/engine/engine.go:929-970, 1193-1207`, `miner/worker.go:1401`
Observable: header

[WBFT-APP-032] A node MAY build proposals in every round, including rounds where it is not the proposer and rounds where a prepared block will be re-proposed. The most recently submitted block whose number equals the current sequence is kept as the pending request across the rounds of that sequence, whether or not the node was the proposer when it was submitted. The node proposes it directly only in round 0, in state `AcceptRequest`, when it is the proposer; in a round `r > 0` it proposes it when it is the proposer and has collected a ROUND-CHANGE quorum that carries no prepared block (`A-05`). (Reference: every validator builds in every round.)
Source: `miner/worker.go:664-669`, `consensus/wbft/core/request.go:47-55`, `consensus/wbft/core/core.go:287`, `consensus/wbft/core/roundchange.go:176-184`, `consensus/wbft/core/preprepare.go:42-51`

[WBFT-APP-033] If building fails (no parent state, gas tip unavailable, 32-byte vanity, missing previous seals, execution error in `process_finalize`), the node MUST NOT propose in that round; the round ends by timeout (`A-06`). There is no retry within the round.
Source: `miner/worker.go:1180-1183, 1336-1343, 1391-1404`, `consensus/wbft/engine/engine.go:486-549`

[WBFT-APP-034] The application MUST NOT build while it is synchronising (reference: the build request is dropped).
Source: `miner/worker.go:1320-1324`

Implementation note (informative). A node whose consensus core is not started (a node that is not mining, whether or not it is a validator) builds a pending block on every new head instead; this path calls `prepare_consensus_fields` too (`miner/worker.go:644-662, 1119-1198`, `consensus/wbft/backend/handler.go:138-146`). A mining node, validator or not, starts the core and does not take this path.

### 4.5 Validator set for the next height

[WBFT-APP-040] When consensus starts height `n+1` it MUST compute `validators_at(n+1)` from `EpochInfo` headers held by the application (`A-04`). In the reference implementation a failure of this lookup yields an **empty** validator set without error, and the node then cannot participate at that height.
Source: `consensus/wbft/backend/backend.go:319-325`, `consensus/wbft/core/core.go:236`

### 4.6 Validating a proposal

```python
def validate_proposal(block) -> (Duration, Error | None)    # A-08 §5, steps P1-P7
```

[WBFT-APP-050] Proposal validation MUST consist of exactly the checks of `A-08 §5`. In particular it MUST NOT execute transactions: state root, receipts root, bloom and gas used of a proposal are not checked before the decided block is executed (§5).
Source: `consensus/wbft/backend/backend.go:258-278`, `consensus/wbft/engine/engine.go:160-186`

[WBFT-APP-051] On any error other than `ErrFutureBlock`, consensus MUST NOT send PREPARE for the proposal and MUST take no other action; the round ends by timeout (`A-05`, `A-06`). On `ErrFutureBlock` it re-processes the PRE-PREPARE after the returned duration.
Source: `consensus/wbft/core/preprepare.go:147-169`
Observable: network

### 4.7 Finalizing a block

```python
def finalize(block, prepared: list[SealData], committed: list[SealData], round: int) -> Error | None
```

`finalize` writes the seals (`A-08 §4`) and hands the sealed block to the application. §5 specifies the two paths. In this specification `finalize` always means this consensus hand-over; the execution-side finalization of a block (upgrades, base-fee distribution, epoch information, gas tip, state root) is `process_finalize` in `B-06`.

[WBFT-APP-060] `finalize` MUST return an error only if writing the seals fails (`WBFT-HDR-052`); consensus then broadcasts ROUND-CHANGE for the next round. Failures of the subsequent execution or storage are **not** reported through `finalize`.
Source: `consensus/wbft/backend/backend.go:213-250`, `consensus/wbft/core/commit.go:173-176`

### 4.8 Head notifications

[WBFT-APP-070] The application MUST notify consensus after every change of the canonical head caused by importing or writing blocks. Consensus then starts round 0 of `head.Number + 1` if `head.Number ≥` its current sequence (the same branch serves normal progress and catching up). A notification for a head at `sequence − 1` (for example a same-height reorganisation) or lower changes nothing.
Source: `miner/worker.go:644-662`, `consensus/wbft/backend/handler.go:138-146`, `consensus/wbft/core/final_committed.go:27-31`, `consensus/wbft/core/core.go:185-208`

[WBFT-APP-071] A batch import MUST produce at least one notification after the batch, for the last canonical block; notifications for intermediate blocks MAY be omitted.
Source: `core/blockchain.go:1576-1581, 1492-1494`

Implementation note (informative). The notification path is `chainHeadFeed → worker.newWorkLoopWBFT → Backend.NewChainHead → FinalCommittedEvent` (`miner/worker.go:644-662`); if the consensus core is stopped, the worker instead builds a pending block. Consensus depends on the miner loop to receive heads.

---

## 5. The commit paths

### 5.1 Two paths

After COMMIT quorum, `finalize` produces the sealed block `B'` (same hash as the proposal `B`). What happens next depends on whether this node's miner is waiting for a block with that hash — which is the case exactly when this node built `B` and it is still its current sealing task.

| | Proposer path (node built `B`) | Non-proposer path |
|---|---|---|
| Hand-over | `B'` sent to the waiting sealing task (`commitCh`) | `B'` enqueued to the block fetcher with peer id `"istanbul"` |
| Header verification | none | `verify_header(check_seals=true)` in the fetcher, then again in `InsertChain` |
| Execution | none — the state computed while building is written | full execution (`Process`), `ValidateState` |
| Storage / head | `WriteBlockAndSetHead(B', receipts, state)` | `InsertChain([B'])` |
| `finalized` / `safe` markers | not updated | set to `B'` |
| Block propagation | `NewMinedBlockEvent` | fetcher broadcasts after header check and after import |
| Conditions that drop the block | the blockchain is stopped (`WriteBlockAndSetHead` returns `errChainStopped`; otherwise the call waits for the chain lock, because `TryLock` of the closable mutex blocks until the lock is free and fails only when the mutex is closed); no pending sealing task for the seal hash; the block is already stored (not a loss) | node not marked synced; merger in PoS-finalized state or TTD reached; a copy with the same hash already queued or already stored; more than 64 blocks queued for the fetcher peer id `"istanbul"`; distance from the head below −7 or above +32; parent unknown; header verification with seals fails in the fetcher (no bad-block record is written in this case) |

Source: `consensus/wbft/backend/backend.go:213-250`, `consensus/wbft/backend/engine.go:186-219`, `miner/worker.go:819-894`, `core/blockchain.go:1445-1452`, `internal/syncx/mutex.go:32-37`, `eth/handler_istanbul.go:38-40`, `eth/fetcher/block_fetcher.go:43-46, 369-372, 764-805, 843-887`, `eth/handler.go:228-307`

[WBFT-APP-080] A node that built the finalized block MUST store it with the post-execution state computed while building, without re-executing it and without re-verifying its header.
Source: `consensus/wbft/backend/backend.go:239-243`, `miner/worker.go:870`

[WBFT-APP-081] A node that did not build the finalized block MUST import it through the normal import pipeline: header verification with seals (`A-08 §6`), body validation, execution, finalization checks (`B-06`), state validation, then head update.
Source: `consensus/wbft/backend/backend.go:245-247`, `eth/fetcher/block_fetcher.go:843-887`, `core/blockchain.go:1563-1913`

[WBFT-APP-082] The decision between the two paths MUST be made by comparing the finalized block's hash with the hash of the block the local miner is currently sealing, not by comparing `Coinbase` with the local address. (A node whose block was re-proposed by another node after it started a new sealing task takes the non-proposer path for its own block.)
Source: `consensus/wbft/backend/backend.go:239`, `consensus/wbft/backend/engine.go:188-195`, `miner/worker.go:789-796`

### 5.2 Execution after consensus

The order on a non-proposer is:

1. `finalize` writes seals and enqueues `B'`. Consensus is in state `Committed`; its round timer keeps running (`A-05`, `A-06`).
2. The fetcher verifies the header (with seals) and, on success, propagates `B'` to peers.
3. `InsertChain` verifies the header again (batch of one), validates the body, executes, runs `process_finalize` (`B-06`: system-contract upgrades, base-fee distribution, epoch-info check, gas-tip check), validates the state.
4. On success the head advances and consensus is notified (§4.8).

[WBFT-APP-090] If header or body validation of the first block of an import batch fails with an error other than a future, pruned-ancestor, known-block or queued-unknown-ancestor outcome, or if execution or state validation of any block of the batch fails, the application MUST record that block's hash as bad (§4.1) and MUST NOT advance the head to it.
Source: `core/blockchain.go:1647-1679, 1778-1792, 2422-2425`
Observable: rpc

### 5.3 Bad block after finalization

A block can collect a COMMIT quorum and then fail execution, because PRE-PREPARE validation does not execute it (§4.6). The reference behaviour is:

1. The non-proposer's import fails; the hash is recorded as bad; the head does not move.
2. Consensus stays in `Committed` until its round timer fires, then moves to round `r+1` and broadcasts ROUND-CHANGE.
3. When moving to `r+1`, a node whose prepared block is recorded as bad discards its prepared round and block and clears all stored extra seals whose sequence is not greater than the current sequence, including the late seals of the previous block (`consensus/wbft/core/core.go:278-286`, `consensus/wbft/core/extraseal.go:185-203`); a PRE-PREPARE for the bad hash is rejected with `ErrBlacklistedHash` (`WBFT-HDR-061`).
4. The height is decided again with a different block.
5. If the node that built the bad block is honest and finalized it itself (possible only when its building and the others' import disagree, e.g. non-deterministic execution), it has already stored the block with its own state (path of §5.1) and advanced its head; it no longer takes part in the new decision for that height (`A-10 §9`). A bad block built by a byzantine proposer (the expected case: PRE-PREPARE validation does not execute) leaves all honest nodes at the old head.

[WBFT-APP-100] After a finalized block fails import, consensus MUST NOT decide that block again at the same height and MUST continue the height through ROUND-CHANGE started by its round timer.
Source: `consensus/wbft/core/core.go:250-263, 277-296`, `consensus/wbft/backend/backend.go:266-270`
Observable: network, log

### 5.4 Ordering and concurrency expectations (as observed)

| Call | Caller context | Serialisation in the reference implementation |
|---|---|---|
| `validate_proposal`, `finalize`, `head`, `is_bad_block`, `validators_at(n+1)`, `ready_to_build` | consensus event loop (one goroutine) | sequential among themselves |
| `prepare_consensus_fields`, `compute_epoch_info`, `candidates`, `gas_tip` (build) | miner main loop | concurrent with the consensus loop; reads extra seals under the consensus read lock (`consensus/wbft/backend/engine.go:221-229`) |
| `verify_header` (import), `is_eligible_proposer`, `gas_tip` (verify) | fetcher goroutine, import goroutine, header-verifier goroutine | concurrent with both loops |
| `on_new_head` | miner new-work loop | asynchronous post to the consensus event loop |

[WBFT-APP-110] A consensus implementation MUST NOT assume that the application's head, canonical mapping or bad-block records are stable between two calls; the reference implementation takes no lock across calls, and the head can advance between `head()` and a later `header_by_number` (`WBFT-HDR-030`).
Source: `consensus/wbft/backend/backend.go:327-342`, `consensus/wbft/engine/engine.go:493, 516`, `miner/worker.go:1120-1124`

[WBFT-APP-111] The application MUST keep the parent state of the current head available for proposal validation; if it is unavailable, eligibility is `UNKNOWN` and the gas-tip check is skipped at PRE-PREPARE (§4.3), which a live validator is expected never to encounter.
Source: `consensus/wbft/engine/engine.go:291-298, 329-348`

---

## 6. Failure semantics summary

| Operation | Failure | Consensus reaction | Requirement |
|---|---|---|---|
| `head` | none (always returns) | — | APP-001 |
| `validators_at(n+1)` | lookup error | continues with empty set; stalls at that height | APP-040 |
| `build_proposal` | any | no proposal this round; timeout | APP-033 |
| `validate_proposal` | `ErrFutureBlock` | retry after duration | APP-051 |
| `validate_proposal` | other | no PREPARE; timeout | APP-051 |
| `finalize` (seal writing) | error | ROUND-CHANGE `r+1` immediately | APP-060 |
| import after `finalize` | error | bad-block record; timeout; ROUND-CHANGE; prepared block discarded | APP-090, APP-100 |
| `is_eligible_proposer` | state unavailable | check skipped | APP-020 |
| `gas_tip` | state unavailable (verify) | check skipped | APP-021 |
| `candidates` | (no failure mode) | — | APP-012 |

---

## 7. Mapping to the reference implementation (informative)

| Abstract operation | Reference |
|---|---|
| `head()` | `Backend.LastProposal` → `chain.CurrentFullBlock` |
| `header_by_number`, `header`, `header_by_hash`, `has_block` | `consensus.ChainHeaderReader`, `Backend.HasProposal` |
| `proposer_of(n)` | `Backend.GetProposer` |
| `is_bad_block` | `Backend.HasBadProposal` → `rawdb.HasBadBlock` |
| `candidates` | `Engine.GetGovCandidates`, `systemcontracts.ValidatorList`, `systemcontracts.GetBLSPublicKey` (inside `buildEpochInfo`) |
| `is_eligible_proposer` | `chain.StateAt(parent.Root).IsBlacklisted` (inside `verifySigner`) |
| `gas_tip` | `Engine.getGasTip` → `systemcontracts.GetGasTip` |
| `ready_to_build` | `Backend.NotifyNewRound` → `worker.readyToCommit` |
| `prepare_consensus_fields` | `Backend.Prepare` → `Engine.Prepare` |
| `compute_epoch_info` | `writeEpoch` / `buildEpochInfo` in `FinalizeAndAssemble` |
| `submit_proposal` | `Backend.Seal` → `RequestEvent` |
| `validate_proposal` | `Backend.Verify` → `Engine.VerifyBlockProposal` |
| `finalize` | `Backend.Commit` → `Engine.CommitHeader`; `commitCh` or `broadcaster.Enqueue` |
| `on_new_head` | `ChainHeadEvent` → `Backend.NewChainHead` → `FinalCommittedEvent` |
