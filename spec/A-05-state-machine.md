# A-05 Consensus State Machine

- Area code: `SM`
- Status: draft
- Reference implementation: go-stablenet `740526d03`. All `Source:` paths are relative to the repository root.

This chapter specifies how a WBFT node reacts to every event that can reach its consensus core: a new chain head, a proposal handed over by the block builder, a received consensus message, the replay of a postponed message, and the expiry of a timer. For each event it states which state variables change, which messages the node sends and with what content, which received messages are relayed, and what is handed to the application for commitment.

The chapter depends on:

- `A-03` for message encodings, signing payloads and `block_hash`,
- `A-04` for `validators_at`, `quorum_size`, `f_value` and `calc_proposer`,
- `A-06` for the durations of the round timer, the retry timer and the future-proposal wait, and for the timing of the new-round notification,
- `A-07` for how a message is sent, relayed and deduplicated on the network,
- `A-08` for proposal verification and for the construction of the next header from the seals collected here,
- `A-09` for the application interface operations named in §1.3.

---

## 1. Introduction (informative)

### 1.1 What the reader gets from this chapter

The core of WBFT is QBFT (Moniz, arXiv 2002.03613) with two additions that change what goes on the wire and into headers, BLS seals carried in PREPARE and COMMIT messages and aggregated into the header and *extra seals* collected after quorum and merged into the next header, and with a bounded message backlog that postpones messages for a future view. The backlog is taken over from the Quorum QBFT implementation (consensus/wbft/core/backlog.go:18); its bounds (the too-far filter, the per-source key and the per-source size limit, §13.1) are WBFT additions. It changes only when messages are processed and relayed, not their bytes or the headers.

The normative content is given twice. §§4-14 state each property as a numbered requirement. §15 gives the complete reference pseudocode that the requirements are cut from. If the two disagree, that is an editorial error; the requirement text wins until it is fixed.

### 1.2 Notation used in this chapter

- `node` is the consensus state of one validator (§3). Handlers named `on_*` mutate it.
- `Q = quorum_size(node.validators)` and `F = f_value(len(node.validators))` are evaluated on the validator set that is current at the moment of the check (`A-04`). `F` is a real number.
- `OK` and `ERR` are the two results a handler returns. The only observable difference between them is relaying (§4.4): a message whose processing returns `OK` is relayed, a message whose processing returns `ERR` is not.
- `schedule(event)` means: the event is queued for later processing by the same node. It is processed after the current handler returns, at an unspecified time, in no guaranteed order relative to other queued events (§4.1).
- `broadcast(node, m)` means: send `m` to the other validators (`A-07`) and `schedule` a self-delivery of the same bytes as a received message (§4.5).
- Views are compared lexicographically: first by sequence, then by round (`A-01`).
- `uint64(x)` truncates a big integer to its low 64 bits and `uint32(x)` to its low 32 bits, as the reference implementation does. `int64(x)` is the low 64 bits of `x` read as a two's-complement signed integer (`big.Int.Int64()`). For rounds reachable through §5.2 and for sequences below `2^63` these truncations are the identity.

### 1.3 Application interface operations used

These operations belong to `A-09`; the names are those of `A-09` and `NAMES.md`.

| Operation | Meaning here |
|---|---|
| `app.head()` | the current chain head block and its `Coinbase` (the *last proposer*; the zero address for the genesis block) |
| `app.validate_proposal(p)` | proposal verification (`A-08`); returns `VALID`, `FUTURE(d)` with a wait duration `d`, or an error |
| `app.finalize(p, prepared_seals, committed_seals, round)` | hands a decided proposal with its seal lists to the application (`A-09` §4.7; not the execution-side `process_finalize` of `B-06`); returns success or failure |
| `app.is_bad_block(hash)` | whether the application has marked the block as invalid |
| `app.notify_new_round(round)` | tells the block builder that a round started; this is `A-09` `ready_to_build(wait, round)`, with `wait` derived from the head and `round` as in `A-06` §8.1 |
| `validators_at(number)` | validator set that seals block `number` (`A-04`) |

---

## 2. Execution model

[WBFT-SM-001] A node MUST process its consensus events one at a time. Each handler in §15 runs to completion, including every message it sends and every state change it makes, before the next event is processed.
Source: consensus/wbft/core/handler.go:102-181 (handleEvents)

[WBFT-SM-002] An event that a handler creates with `schedule` MUST NOT be processed before that handler returns. Apart from that, a conforming node MAY process scheduled events, received messages and timer expiries in any order. The events that are scheduled rather than processed inline are: the self-delivery of every message the node broadcasts, every replay of a backlogged message, every replay of a stored proposal request, the re-injection of a future PRE-PREPARE, and every timer expiry.
Source: consensus/wbft/backend/backend.go:167-171 (Broadcast: `go Post`), consensus/wbft/core/backlog.go:304, consensus/wbft/core/request.go:116-118, consensus/wbft/core/preprepare.go:156-162, consensus/wbft/core/core.go:411-413, consensus/wbft/core/core.go:446-448

> **Implementation note (informative).** The reference implementation posts every event to an unbuffered `event.TypeMux` from a separate goroutine, and one goroutine (`handleEvents`) selects over four subscriptions (request/message/backlog, timeout, final-committed, retry). Go's `select` picks among ready channels at random, and the posting goroutines race, so no order is guaranteed even between events of the same kind. §17 lists the observable consequences.

[WBFT-SM-003] A node MUST NOT send any consensus message of its own (PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE) while its address is not a member of `node.validators`. In that case `broadcast` sends nothing and schedules no self-delivery, and the handler continues as if the send had failed.
Source: consensus/wbft/backend/backend.go:152-162 (Broadcast returns ErrUnauthorizedAddress)
Observable: network

A node that runs the consensus core without being a validator therefore still validates, relays and decides on any consensus message it receives (§8 to §10), but never sends its own messages. Conforming peers address it only while they count it as a validator (`A-07` §6.2).

---

## 3. Node state

### 3.1 State variables

The consensus state of a node is the record below. Field names are the names used in the rest of the specification; the column "Reference" gives the field in the reference implementation.

```python
class ConsensusState(IntEnum):
    AcceptRequest = 0
    Preprepared   = 1
    Prepared      = 2
    Committed     = 3

@dataclass
class RoundState:                                   # reference: roundState
    view: View                                      # current (sequence, round)
    preprepare: Optional[PrePrepareMsg]             # last accepted PRE-PREPARE (carried across rounds)
    prepares: Dict[Address, PrepareMsg]             # PREPAREs for view, one per source
    commits: Dict[Address, CommitMsg]               # COMMITs for view, one per source
    prepared_round: Optional[Round]                 # lock: round in which a PREPARE quorum was seen
    prepared_block: Optional[Proposal]              # lock: the proposal of that round
    pending_request: Optional[Proposal]             # latest proposal from the block builder for this sequence
    preprepare_sent: Round                          # round of the last PRE-PREPARE this node sent (initial 0)

@dataclass
class RoundChangeSet:                               # reference: roundChangeSet
    messages: Dict[uint64, Dict[Address, RoundChangeMsg]]       # by target round, one per source
    highest_prepared_round: Dict[uint64, Round]
    highest_prepared_block: Dict[uint64, Proposal]
    highest_prepared_justification: Dict[uint64, List[PrepareMsg]]

@dataclass
class PriorState:                                   # reference: priorState
    round: Round                                    # initial 0
    proposal: Optional[Proposal]                    # initial None
    validators: Optional[ValidatorSet]              # initial None

@dataclass
class Node:
    address: Address                                # address of node_key
    node_key: bytes32                               # secp256k1 key (A-01 glossary); signs messages (A-02)
    current: Optional[RoundState]
    state: ConsensusState
    validators: ValidatorSet                        # includes the proposer of node.current.view
    prepared_certificate: Optional[List[PrepareMsg]]    # reference: WBFTPreparedPrepares
    round_changes: RoundChangeSet
    pending_requests: PriorityQueue[Proposal]       # future proposals, keyed by -int64(number)
    backlog: Dict[Address, PriorityQueue[Message]]
    backlog_keys: Dict[Address, Set[Tuple[int, uint64, uint64]]]
    extra_prepare_seals: Dict[Address, PrepareMsg]
    extra_commit_seals: Dict[Address, CommitMsg]
    prior: PriorState
    # timers (A-06): round_timer, retry_timer, future_proposal_timer
```

| Variable | Reference | Set by | Cleared / replaced by |
|---|---|---|---|
| `current.view` | `current.sequence`, `current.round` | §6 | every new round and new sequence |
| `state` | `Core.state` | §6, §8, §9, §10 | every new round and new sequence (to `AcceptRequest`) |
| `validators` (with proposer) | `Core.valSet` | §6 | new sequence (new set); proposer recomputed every new round |
| `current.preprepare` | `roundState.Preprepare` | PRE-PREPARE acceptance | new sequence only (carried across rounds) |
| `current.prepares`, `current.commits` | `WBFTPrepares`, `WBFTCommits` | §9, §10 | every new round and new sequence |
| `current.prepared_round`, `current.prepared_block` | `preparedRound`, `preparedBlock` | PREPARE quorum | new sequence; bad-block rule (§6.3) |
| `current.pending_request` | `pendingRequest` | §7 | new sequence only |
| `current.preprepare_sent` | `preprepareSent` | §8.1 | every new round and new sequence (to 0) |
| `prepared_certificate` | `WBFTPreparedPrepares` | PREPARE quorum | only when `start_new_round` is called with argument `0` (§6.4) |
| `round_changes` | `roundChangeSet` | §11.2 | recreated only when `start_new_round` is called with argument `0`; otherwise pruned (§6.4) |
| `pending_requests` | `pendingRequests` | §7 | entries leave when replayed or found old |
| `backlog`, `backlog_keys` | `backlogs`, `backlogKeys` | §13 | entries leave when replayed, found old/invalid, or their source leaves the validator set |
| `extra_prepare_seals`, `extra_commit_seals` | `prepareExtraSeals`, `commitExtraSeals` | §12 | by sequence (§12.5) |
| `prior` | `priorState` | new sequence | next new sequence |

Source: consensus/wbft/core/core.go:56-124 (New, Core), consensus/wbft/core/roundstate.go:33-69, consensus/wbft/core/roundchange.go:226-244, consensus/wbft/core/priorstate.go:28-45, consensus/wbft/core/types.go:37-42

### 3.2 Lifetimes

[WBFT-SM-004] When the consensus core starts, the node MUST begin with `current = None`, `state = AcceptRequest`, empty `backlog`, `pending_requests`, `extra_prepare_seals`, `extra_commit_seals`, `prior = PriorState(round=0, proposal=None, validators=None)`, and then run `start_new_round(node, 0)` (§6).
Source: consensus/wbft/core/core.go:56-78, consensus/wbft/core/handler.go:35-47

[WBFT-SM-005] On a round change within a sequence (§6.2, branch `ROUND_CHANGE`), the node MUST carry `current.preprepare`, `current.prepared_round`, `current.prepared_block` and `current.pending_request` into the new round state (subject to §6.3), and MUST start the new round with empty `prepares` and `commits` and `preprepare_sent = 0`.
Source: consensus/wbft/core/core.go:277-288, consensus/wbft/core/roundstate.go:33-49

[WBFT-SM-006] On a new sequence (§6.2, branches `INITIAL` and `CATCH_UP`), the node MUST start with a round state in which `preprepare`, `prepared_round`, `prepared_block` and `pending_request` are all `None`.
Source: consensus/wbft/core/core.go:288-294

[WBFT-SM-007] (withdrawn; informative) The reference implementation keeps no consensus state across a stop and a start of the consensus core (for example a node restart, or the block builder being stopped and started). After a start, the node behaves as specified by WBFT-SM-004 regardless of what it sent before: it has no lock, no prepared certificate, no backlog and no extra seals. The one exception is a timer that the old core's event loop armed after `Stop` had cancelled the timers: it is not cancelled, and if it expires after the restart the new core handles it as its own (`A-06` WBFT-TIMER-018).
Source: consensus/wbft/backend/backend.go:355-382 (startWBFT creates a new Core; stop discards it), consensus/wbft/core/handler.go:104-107, consensus/wbft/core/handler.go:50-59

The lock (`current.prepared_round`, `current.prepared_block`) and the last accepted PRE-PREPARE (`current.preprepare`) are separate variables with separate lifetimes (WBFT-SM-005, WBFT-SM-025), and they can refer to different blocks. The lock changes only at a PREPARE quorum, while `current.preprepare` changes at every accepted PRE-PREPARE, including a justified PRE-PREPARE of a later round for another block (`A-14` WBFT-PROTO-002). The ROUND-CHANGE reports the lock (§11.1), and `prior.proposal` is taken from `current.preprepare` (WBFT-SM-025).

---

## 4. Events

### 4.1 Event kinds

| Event | Produced by | Handler |
|---|---|---|
| `Start` | the application starts consensus (`A-09`) | `on_start` |
| `Stop` | the application stops consensus | `on_stop` |
| `NewHead` | every chain-head event of the application (own decision, others' decision; during synchronisation at least once per imported batch, not for every block) | `on_new_head` |
| `Request(p)` | the block builder hands over a proposal; or a stored request is replayed (§7) | `on_request` |
| `Message(code, payload)` | a consensus message received from a peer (`A-07`), or the self-delivery of a broadcast | `on_message_received` |
| `Backlog(m)` | replay of a backlogged message (§13), or re-injection of a future PRE-PREPARE (§8.3) | `on_backlog_event` |
| `RoundTimeout(t)` | expiry of round timer `t` (`A-06`) | `on_round_timeout` |
| `RetryTimeout(r)` | expiry of the retry timer armed for round `r` (`A-06`) | `on_retry_timeout` |

Source: consensus/wbft/core/handler.go:64-81, consensus/wbft/core/handler.go:109-179, consensus/wbft/backend/handler.go:138-146, consensus/wbft/backend/engine.go:186-219

### 4.2 Start and stop

[WBFT-SM-008] On `Start`, the node MUST initialise its state as in WBFT-SM-004 and run `start_new_round(node, 0)` before processing any other event.
Source: consensus/wbft/core/handler.go:35-47

> **Implementation note (informative).** In the reference implementation `Core.Start` launches the event loop before calling `startNewRound(0)` from the caller's goroutine. No event reaches the loop in that window because `Backend.HandleMsg` and `Backend.NewChainHead` reject events until `coreStarted` is set, which happens after `Core.Start` returns (consensus/wbft/backend/engine.go:266-272, consensus/wbft/backend/handler.go:74-76, consensus/wbft/backend/handler.go:141-143). A `RequestEvent` posted by `Backend.Seal` is not gated this way (consensus/wbft/backend/engine.go:186-200); if one arrives in that window it is dropped as `errCurrentIsNil` (consensus/wbft/core/request.go:68-70). Whether `Seal` can be called in that window was not checked.

[WBFT-SM-009] On `Stop`, the node MUST stop all three timers, MUST stop processing consensus events, and MUST discard its consensus state (WBFT-SM-007). While stopped, consensus messages received from peers are not processed (`A-07` specifies the network-level reply).
Source: consensus/wbft/core/handler.go:50-59, consensus/wbft/backend/engine.go:288-300, consensus/wbft/backend/handler.go:73-76

### 4.3 New head

[WBFT-SM-010] On every `NewHead` event the node MUST run `start_new_round(node, 0)`. The event carries no data; `start_new_round` reads the head itself (§6.2).
Source: consensus/wbft/core/final_committed.go:27-31, consensus/wbft/backend/handler.go:138-146, miner/worker.go:644-661

### 4.4 Relaying

[WBFT-SM-011] When the processing of a `Message(code, payload)` event returns `OK`, the node MUST relay `payload` unchanged under `code` to the members of `node.validators` other than itself, as evaluated after the handler returned. Target filtering (peers that already have the message) is specified in `A-07`.
Source: consensus/wbft/core/handler.go:128-135, consensus/wbft/backend/backend.go:176-210
Observable: network

[WBFT-SM-012] When the processing of a `Backlog(m)` event returns `OK`, the node MUST relay `rlp_encode(m)` under `m.code` to the same targets as in WBFT-SM-011.
Source: consensus/wbft/core/handler.go:136-150
Observable: network

[WBFT-SM-013] A message whose processing returns `ERR` MUST NOT be relayed at that time. This includes messages classified `FUTURE` (they are relayed later, if and when their replay returns `OK`), `OLD`, `INVALID`, `TOO_FAR`, messages with an invalid signature, and messages rejected by a handler.
Source: consensus/wbft/core/handler.go:128-133, consensus/wbft/core/handler.go:211-227
Observable: network

§16 lists, for every outcome, whether the message is relayed. Two outcomes are relayed that a reader might not expect: an extra-seal message that was stored or silently ignored (§12.2), and a ROUND-CHANGE whose quorum processing failed the proposer's own justification self-check (§11.5).

### 4.5 Self-delivery

[WBFT-SM-014] Every message a node broadcasts MUST also be processed by the node itself as a received `Message` event, through the full receive path of §5 (decoding, signature verification, `check_message`, handler). The node's own messages are counted, stored and relayed only through that path.
Source: consensus/wbft/backend/backend.go:164-172
Observable: network, log

Consequences of the self-delivery being scheduled rather than inline are listed in §17.

---

## 5. Message intake

### 5.1 Decoding and signatures

[WBFT-SM-015] A `Message` event whose code is not one of `PREPREPARE`, `PREPARE`, `COMMIT`, `ROUND_CHANGE` MUST be discarded. A payload that does not decode under `A-03` MUST be discarded.
Source: consensus/wbft/core/handler.go:188-202, consensus/wbft/messages/decode.go:27-59

[WBFT-SM-016] For signature verification of a message or justification member `x` with view `v`, the node MUST use the validator set returned by `signature_validator_set(node, v)`:

```python
def signature_validator_set(node, v: View) -> ValidatorSet:
    cur = node.current.view
    if (v < cur
            and node.state == AcceptRequest
            and v == View(cur.sequence - 1, node.prior.round)
            and node.prior.validators is not None):
        return node.prior.validators
    return node.validators
```

Source: consensus/wbft/core/core.go:452-462 (checkValidatorSignature)

Messages for a future view, including every message for sequence `cur.sequence + 1`, are therefore verified against the *current* validator set, not the set that will seal that sequence.

[WBFT-SM-017] The node MUST verify the ECDSA signature of the message itself and then of each justification member, in this order, and MUST discard the whole message at the first failure:

1. the message;
2. for a ROUND-CHANGE: each element of `justification` (PREPAREs), in list order;
3. for a PRE-PREPARE: each element of `justification_round_changes` in list order, then each element of `justification_prepares` in list order.

A signature verifies when `ecdsa_recover_address(message_signing_payload(x), x.signature)` (which hashes its input, `A-02` WBFT-CRYPTO-013) succeeds and the recovered address is a member of `signature_validator_set(node, x.view)`. The recovered address becomes `x.source`.
Source: consensus/wbft/core/handler.go:268-317, consensus/wbft/utils.go:39-73

[WBFT-SM-018] The node MUST NOT verify the BLS seals of justification PREPAREs, and MUST NOT run `validate_proposal` on the `prepared_block` of a ROUND-CHANGE. Only the ECDSA signatures of justification members are checked (WBFT-SM-017), plus the structural checks of §11.2 and §11.6.
Source: consensus/wbft/core/handler.go:292-314, consensus/wbft/core/roundchange.go:118-151

Justification members that pass signature verification are used only through `is_justified` (§11.6) and `has_matching_round_change_and_prepares` (§11.3). They are never stored as PREPAREs of the node's own round and never contribute seals.

### 5.2 `check_message`

`check_message` classifies a message by its code and view against the node's current view and state. It is evaluated when a message arrives, and again when a backlogged message is considered for replay (§13.3) and when a replayed message is processed.

[WBFT-SM-019] `check_message` MUST return the first matching result of the following rules, evaluated in this order. `cur = node.current.view`, `s = node.state`.

1. **Too far ahead** → `TOO_FAR` if any of:
   1. `v.sequence > cur.sequence + SEQUENCE_THRESHOLD` (`SEQUENCE_THRESHOLD = 1`);
   2. `v.sequence > cur.sequence` and `v.round >= ROUND_THRESHOLD` (`ROUND_THRESHOLD = 10`; the round is compared absolutely);
   3. `v.sequence == cur.sequence` and `v.round > cur.round + ROUND_THRESHOLD`.
2. **ROUND-CHANGE** (`code == ROUND_CHANGE`):
   1. `v.sequence > cur.sequence` → `FUTURE`;
   2. `v < cur` → `OLD`;
   3. otherwise → `PROCESS` (same sequence, any round from `cur.round` to `cur.round + 10`).
3. **Other codes, future view**: `v > cur` → `FUTURE`.
4. **Other codes, past view**: `v < cur` →
   1. `EXTRA_SEAL` if `cur.sequence - v.sequence == 1` and `v.round == node.prior.round` and `s == AcceptRequest`;
   2. otherwise `OLD`.
5. **Other codes, current view** (`v == cur`), by state:

| State `s` \ code | `PREPREPARE` | `PREPARE` | `COMMIT` |
|---|---|---|---|
| `AcceptRequest` | `PROCESS` | `FUTURE` | `FUTURE` |
| `Preprepared` | `INVALID` | `PROCESS` | `FUTURE` |
| `Prepared` | `INVALID` | `EXTRA_SEAL` | `PROCESS` |
| `Committed` | `INVALID` | `EXTRA_SEAL` | `EXTRA_SEAL` |

Source: consensus/wbft/core/backlog.go:44-51, consensus/wbft/core/backlog.go:76-113 (isTooFarFutureMessage), consensus/wbft/core/backlog.go:125-202 (checkMessage)

Rule 4.1 does not look at the code: a PRE-PREPARE of the previous sequence at the prior round is also classified `EXTRA_SEAL` and then rejected or silently ignored by §12.2. A message whose view has a missing sequence or round is `INVALID`; the decoder of `A-03` never produces one.
Source: consensus/wbft/core/backlog.go:126-128

[WBFT-SM-020] After `check_message`, the node MUST dispose of the message as follows:

| Result | Disposition | Handler result |
|---|---|---|
| `PROCESS` | deliver to the handler for its code (§8.3, §9.2, §10.2, §11.2) | the handler's result |
| `FUTURE` | `add_to_backlog(node, m)` (§13) | `ERR` |
| `EXTRA_SEAL` | `add_extra_seal(node, m)` (§12.2) | the result of `add_extra_seal` |
| `OLD`, `INVALID`, `TOO_FAR` | discard | `ERR` |

Source: consensus/wbft/core/handler.go:211-227
Observable: network

> **Implementation note (informative).** `handleDecodedMessage` reassigns the error variable to the result of `addToExtraSeal`, so a successfully stored extra seal returns `nil` and is relayed. §12.2 relies on this.

---

## 6. New round and new sequence

### 6.1 Overview

`start_new_round(node, round)` is the only procedure that changes the view. It is called with `round = 0` on start and on every new head, and with `round > 0` on a round timeout (§14.2) and on the F+1 rule (§11.4). The argument is a *requested* round; which view the node actually enters depends on the chain head, and several resets depend on the argument rather than on the entered view.

### 6.2 Branch selection

[WBFT-SM-022] In branch `CATCH_UP` the node MUST enter round 0 of the new sequence regardless of the requested `round`.
Source: consensus/wbft/core/core.go:231-236

The `ROUND_CHANGE` branch also admits `round == node.current.view.round`, which re-enters the current round with emptied message sets. No caller in the reference implementation requests the current round; the case is unreachable.

[WBFT-SM-023] In branches `INITIAL` and `CATCH_UP`, if `node.current is not None`, the node MUST first run `add_effective_seals_to_extra_seals(node)` (§12.4) on the round state it is leaving.
Source: consensus/wbft/core/core.go:237-240, consensus/wbft/core/extraseal.go:118-129

### 6.3 Round state update

[WBFT-SM-024] In branch `ROUND_CHANGE`, if `node.current.prepared_block` is set and `app.is_bad_block(block_hash(node.current.prepared_block))` is true, the node MUST clear `prepared_round` and `prepared_block` before carrying them over, and MUST run `clear_extra_seals(node, node.current.view.sequence + 1)` (§12.5). `prepared_certificate` is not cleared.
Source: consensus/wbft/core/core.go:278-286

This rule is how a node abandons a proposal that gathered a COMMIT quorum but could not be imported (§10.4): the node leaves the decided round only through a round timeout or the F+1 rule (§11.4), and the lock is released when the next round starts.

[WBFT-SM-025] In branches `INITIAL` and `CATCH_UP`, when `node.current is not None`, the node MUST update `prior` before replacing the round state:

```python
def update_prior_state(node):
    node.prior.round = node.current.view.round
    if node.current.preprepare is not None:
        node.prior.proposal = node.current.preprepare.proposal
    if node.validators is not None:
        node.prior.validators = node.validators
```

`prior` therefore describes the round the node was in and the last proposal it accepted, not necessarily the round and block that were committed.
Source: consensus/wbft/core/core.go:289-292, consensus/wbft/core/priorstate.go:35-45

[WBFT-SM-026] After the round state update the node MUST set `node.validators` to the new validator set of the selected branch (§6.2) and MUST set its proposer to `calc_proposer(node.validators, last_proposer, new_view.round, policy)` (`A-04`), where `last_proposer` is the `Coinbase` of the head read by `start_new_round`.
Source: consensus/wbft/core/core.go:243-246, consensus/wbft/core/core.go:295, consensus/wbft/backend/backend.go:319-342, consensus/wbft/engine/engine.go:86-88
Observable: log

### 6.4 Resets, notification and timer

[WBFT-SM-027] The node MUST then run `set_state(node, AcceptRequest)`, which replays stored proposal requests (§7.3) and then the backlog (§13.3).
Source: consensus/wbft/core/core.go:247, consensus/wbft/core/core.go:298-311

[WBFT-SM-028] After `set_state`, the node MUST update the round-change set and the prepared certificate according to the *requested* `round` argument, not the entered view:

- if `round == 0`: set `prepared_certificate = None`, replace `round_changes` with an empty set, and run `clear_extra_seals(node, last.number)` (§12.5);
- otherwise: run `round_changes.clear_lower_than(round)` (§11.3);

and in both cases run `round_changes.new_round(round)`.
Source: consensus/wbft/core/core.go:249-258, consensus/wbft/core/roundchange.go:246-256, consensus/wbft/core/roundchange.go:334-346

When branch `CATCH_UP` is taken with `round > 0` (a late round timeout, §14.2, or the F+1 rule firing after the head advanced), the node enters round 0 of a new sequence but keeps the ROUND-CHANGE entries of the previous sequence for rounds `>= round`, keeps its previous-sequence `prepared_certificate`, does not clear old extra seals, and creates an empty round-change entry for `round`. The round-change set does not record sequences, so those entries are later counted as if they belonged to the new sequence (§11.4).

[WBFT-SM-029] The node MUST then call `app.notify_new_round(round)` with the requested `round` argument. In branch `CATCH_UP` with `round > 0` the block builder is therefore told that a round `> 0` started although the node is in round 0 (the wait rule of `A-06` depends on this value).
Source: consensus/wbft/core/core.go:260, consensus/wbft/backend/engine.go:277-285

[WBFT-SM-030] Finally the node MUST start the round timer for the entered view (`A-06`). Starting the round timer stops the retry timer and the future-proposal timer and cancels every earlier round timer, so that an earlier `RoundTimeout` that is already queued is ignored (§14.1). This holds within one run of the consensus core; see WBFT-SM-075 for timers armed during `Stop`.
Source: consensus/wbft/core/core.go:268-271, consensus/wbft/core/core.go:336-347, consensus/wbft/core/core.go:355-415
Observable: log

---

## 7. Proposal requests

### 7.1 Classification

```python
def check_request(node, p) -> str:
    if p is None:                   return "INVALID"
    if node.current is None:        return "INVALID"     # reference: errCurrentIsNil
    if node.current.view.sequence > p.number: return "OLD"
    if node.current.view.sequence < p.number: return "FUTURE"
    return "OK"
```

Source: consensus/wbft/core/request.go:63-79

### 7.2 Handling a request

[WBFT-SM-031] On `Request(p)`, the node MUST evaluate `check_request`. On `FUTURE` it MUST store `p` in `pending_requests`; on `OLD` or `INVALID` it MUST discard `p`.
Source: consensus/wbft/core/handler.go:118-127, consensus/wbft/core/request.go:33-45, consensus/wbft/core/request.go:81-90

[WBFT-SM-032] On `OK`, the node MUST set `current.pending_request = p` in every state, and, if `state == AcceptRequest` and `uint64(current.view.round) == 0`, MUST run `send_preprepare(node, p, round_changes=None, prepares=None)` (§8.1). In a round `> 0` the request is only stored; it is proposed by the ROUND-CHANGE quorum rule (§11.5).
Source: consensus/wbft/core/request.go:47-56

`send_preprepare` itself checks that the node is the proposer (§8.1), so non-proposers only record the request. `preprepare_sent` is not consulted on this path: two different requests for the same sequence processed in round 0 before the node has accepted its own PRE-PREPARE lead to two PRE-PREPAREs with different proposals for the same view (§17).

### 7.3 Replaying stored requests

[WBFT-SM-033] Whenever `set_state(node, AcceptRequest)` runs, the node MUST repeatedly take the request with the lowest `int64(p.number)` from `pending_requests` (the stored priority is `-int64(p.number)`; below `2^63` this is the lowest block number; ties in unspecified order) and: on `FUTURE`, put it back and stop; on `OLD` or `INVALID`, discard it; on `OK`, `schedule(Request(p))`.
Source: consensus/wbft/core/core.go:304-306, consensus/wbft/core/request.go:94-120

---

## 8. PRE-PREPARE

### 8.1 Sending

[WBFT-SM-034] `send_preprepare(node, p, round_changes, prepares)` MUST send nothing unless `p.number == current.view.sequence` and the node is the proposer of `node.validators`.
Source: consensus/wbft/core/preprepare.go:42-51

[WBFT-SM-035] The PRE-PREPARE MUST carry `sequence = current.view.sequence`, `round = current.view.round`, `proposal = p`, an ECDSA signature over its signing payload (`A-03`), `justification_round_changes` equal to the signed payloads of the ROUND-CHANGE messages in `round_changes` (empty when `round_changes is None`), and `justification_prepares` equal to `prepares` (empty when `None`). The order of `justification_round_changes` is unspecified.
Source: consensus/wbft/core/preprepare.go:52-84, consensus/wbft/messages/preprepare.go:50-71
Observable: network

[WBFT-SM-036] After a successful broadcast the node MUST set `current.preprepare_sent = current.view.round`. If the broadcast fails (WBFT-SM-003) `preprepare_sent` is unchanged.
Source: consensus/wbft/core/preprepare.go:98-105

### 8.2 Receipt: order of checks

[WBFT-SM-037] A PRE-PREPARE classified `PROCESS` MUST be rejected with `ERR`, without any state change, at the first failing check of this sequence:

1. `m.source` is not the proposer of `node.validators`;
2. `uint64(m.sequence) != uint64(m.proposal.number)`;
3. `uint64(m.round) > 0` and `not is_justified(m.proposal, m.view, m.justification_round_changes, m.justification_prepares, Q)` (§11.6);
4. `app.validate_proposal(m.proposal)` does not return `VALID` (see WBFT-SM-038 for `FUTURE`).

For round 0 the justification lists are not inspected. Check 3 here and the round test of WBFT-SM-032 use the low 64 bits of the round, so a round that is a non-zero multiple of 2^64 is treated as round 0; such a round is not reached in practice, because a node does not process messages more than `ROUND_THRESHOLD` rounds ahead of its own (WBFT-SM-019) and so its round grows by a bounded amount per step.
Source: consensus/wbft/core/preprepare.go:115-169
Observable: network

None of these checks compares a round `> 0` PRE-PREPARE with the receiver's own lock; see `A-14` WBFT-PROTO-002.

### 8.3 Future proposals

[WBFT-SM-038] If `app.validate_proposal` returns `FUTURE(d)`, the node MUST return `ERR` and arm the future-proposal timer so that after `d` it schedules `Backlog(m)`. Arming the timer replaces any earlier future-proposal timer (at most one PRE-PREPARE waits at a time). The timer is also stopped by every start of the round timer (WBFT-SM-030).
Source: consensus/wbft/core/preprepare.go:148-169, consensus/wbft/core/core.go:321-325

The re-injected PRE-PREPARE goes through `check_message` again and is relayed if it is then accepted (WBFT-SM-012). It is not relayed when it first arrives.

### 8.4 Acceptance

[WBFT-SM-039] If all checks pass and `state == AcceptRequest`, the node MUST, in this order: restart the round timer for the current view (`A-06`), set `current.preprepare = m`, run `set_state(node, Preprepared)`, and run `broadcast_prepare(node)` (§9.1). The handler returns `OK`.
Source: consensus/wbft/core/preprepare.go:171-196
Observable: network, log

Because `check_message` classifies a current-view PRE-PREPARE as `PROCESS` only in `AcceptRequest`, a node accepts at most one PRE-PREPARE per view, and sends at most one PREPARE per view, during one run of the consensus core.

---

## 9. PREPARE

### 9.1 Sending

[WBFT-SM-040] `broadcast_prepare(node)` MUST broadcast a PREPARE with `sequence = current.view.sequence`, `round = current.view.round`, `digest = block_hash(current.preprepare.proposal)`, and `prepare_seal = bls_sign(seal_data(header(current.preprepare.proposal), uint32(current.view.round), PREPARE_SEAL))` (`A-02`), signed with ECDSA over its signing payload (`A-03`).
Source: consensus/wbft/core/prepare.go:35-79, consensus/wbft/core/core.go:465-469, consensus/wbft/backend/backend.go:287-289
Observable: network

### 9.2 Receipt

[WBFT-SM-041] A PREPARE classified `PROCESS` MUST be rejected with `ERR`, without any state change, if `m.digest != block_hash(current.preprepare.proposal)`, or if `verify_seal(node.validators, header(current.preprepare.proposal), uint32(m.round), PREPARE_SEAL, m.prepare_seal, m.source)` fails.
Source: consensus/wbft/core/prepare.go:92-109, consensus/wbft/core/core.go:471-488

`verify_seal(vs, header, round, type, seal, sealer)` succeeds when `seal` decodes as a BLS signature and `bls_verify(bls_public_key(vs, sealer), seal_data(header, round, type), seal)` holds (`A-02`).

Implementation note (informative). The reference `verifySeal` looks up `sealer` in `vs` without checking that it is a member; it assumes that the sender is a member of the set it is given. Every caller guarantees this. PREPARE and COMMIT receipt pass `node.validators`, the set that the message signature was checked against. The extra-seal path (§12.2) passes the prior set exactly when the signature check used the prior set. A message replayed from the backlog (§13.3) is not checked again, but the replay first drops the backlog of every sender that is not in `node.validators`. A new implementation can check membership explicitly instead of relying on this assumption. Source: consensus/wbft/core/core.go:452-462,471-474; consensus/wbft/core/extraseal.go:38-40; consensus/wbft/core/backlog.go:160-161,257-263; consensus/wbft/core/handler.go:136-141.

[WBFT-SM-042] Otherwise the node MUST store `current.prepares[m.source] = m`, replacing any earlier PREPARE from the same source.
Source: consensus/wbft/core/prepare.go:112-115, consensus/wbft/core/qbft_msg_set.go:66-71

[WBFT-SM-043] If after storing `len(current.prepares) >= Q` and `state < Prepared`, the node MUST, in this order: set `current.prepared_round = current.view.round`; set `prepared_certificate` to copies (sequence, round, digest, prepare_seal, signature, source) of all messages in `current.prepares`; set `current.prepared_block = current.preprepare.proposal`; run `set_state(node, Prepared)`; run `broadcast_commit(node)` (§10.1). The handler returns `OK`.
Source: consensus/wbft/core/prepare.go:119-145
Observable: network, log

Since PREPAREs reach the handler only in `Preprepared` (WBFT-SM-019) and each one is processed separately, the transition happens when the Q-th distinct source is stored, and `prepared_certificate` holds exactly `Q` PREPAREs. PREPAREs for the current view that arrive later are extra seals (§12).

---

## 10. COMMIT and decision

### 10.1 Sending

[WBFT-SM-044] `broadcast_commit(node)` MUST broadcast a COMMIT with `sequence`, `round` and `digest` as in WBFT-SM-040 and `commit_seal = bls_sign(seal_data(header(current.preprepare.proposal), uint32(current.view.round), COMMIT_SEAL))`, signed with ECDSA over its signing payload.
Source: consensus/wbft/core/commit.go:36-82
Observable: network

A node sends at most one COMMIT per view during one run of the consensus core, because `broadcast_commit` is called only on the transition to `Prepared`.

### 10.2 Receipt

[WBFT-SM-045] A COMMIT classified `PROCESS` MUST be rejected with `ERR`, without any state change, if `m.digest != block_hash(current.preprepare.proposal)` or if `verify_seal(node.validators, header(current.preprepare.proposal), uint32(m.round), COMMIT_SEAL, m.commit_seal, m.source)` fails. Otherwise the node MUST store `current.commits[m.source] = m`, replacing any earlier COMMIT from the same source.
Source: consensus/wbft/core/commit.go:90-118

[WBFT-SM-046] If after storing `len(current.commits) >= Q`, the node MUST run `decide(node)` (§10.3). The handler returns `OK`.
Source: consensus/wbft/core/commit.go:122-130
Observable: log

### 10.3 Decision

[WBFT-SM-047] `decide(node)` MUST first run `set_state(node, Committed)` and then build two seal lists from the node's message sets as they are at that moment:

```python
prepared_seals  = [SealData(sealer=index_of(node.validators, x.source), seal=x.prepare_seal)
                   for x in current.prepares.values()]
committed_seals = [SealData(sealer=index_of(node.validators, x.source), seal=x.commit_seal)
                   for x in current.commits.values()]
```

and call `app.finalize(current.preprepare.proposal, prepared_seals, committed_seals, current.view.round)`. The order of each list is unspecified. `A-08` specifies how the lists become `PreparedSeal`, `CommittedSeal` and `Round` of the header.
Source: consensus/wbft/core/commit.go:137-173, consensus/wbft/engine/engine.go:90-151
Observable: header

[WBFT-SM-048] Because COMMITs reach the handler only in `Prepared` and PREPAREs only in `Preprepared`, and each is processed separately, `committed_seals` MUST contain exactly the `Q` COMMITs that led to the decision and `prepared_seals` exactly the `Q` PREPAREs that led to `Prepared`. Every other seal for the view that the node receives is handled as an extra seal (§12).
Source: consensus/wbft/core/backlog.go:174-199, consensus/wbft/core/prepare.go:121, consensus/wbft/core/commit.go:123
Observable: header

The block hash excludes `PreparedSeal`, `CommittedSeal` and `Round` (`A-03`). Different nodes that decide the same proposal therefore produce headers with the same hash but possibly different seal sets and, if they decided in different rounds, different `Round` values. Which variant a node stores depends on which one it imports first.

### 10.4 After the decision

[WBFT-SM-049] If `app.finalize` fails, the node MUST broadcast a ROUND-CHANGE for `current.view.round + 1` (§11.1) without changing its view or state.
Source: consensus/wbft/core/commit.go:172-176, consensus/wbft/core/roundchange.go:40-43
Observable: network

`app.finalize` fails only if the seal lists cannot be written into the header. A decided proposal that later fails to import is not reported back; the node stays in `Committed` (WBFT-SM-050).

[WBFT-SM-050] After `decide` the node MUST NOT stop its round timer. It remains in `Committed` for the decided view until a `NewHead` event starts the next sequence (§6), the round timer of the decided round expires (§14.2), or the F+1 rule (§11.4) fires on a ROUND-CHANGE for a higher round, whichever is processed first. ROUND-CHANGEs are processed in `Committed` like in every other state (WBFT-SM-019).
Source: consensus/wbft/core/commit.go:137-179 (no timer call), consensus/wbft/core/handler.go:152-160, consensus/wbft/core/backlog.go:136-147, consensus/wbft/core/roundchange.go:161-169
Observable: network, log

---

## 11. ROUND-CHANGE

### 11.1 Sending

[WBFT-SM-051] A node MUST call `broadcast_round_change(node, round)` in exactly these cases: after a round timeout (§14.2: target `r + 1`, where `r` is `current.view.round` when the timeout is processed; the call follows `start_new_round`, and after the `CATCH_UP` branch the node is in round 0 of the next sequence when it sends); after the F+1 rule (§11.4, target the new round); after a failed `finalize` (WBFT-SM-049, target `current.view.round + 1` without a round change); on a retry timeout (§14.3, target the round the retry was armed with).
Source: consensus/wbft/core/handler.go:250-263, consensus/wbft/core/roundchange.go:161-169, consensus/wbft/core/commit.go:173-176, consensus/wbft/core/handler.go:170-178

[WBFT-SM-052] `broadcast_round_change(node, round)` MUST first arm the retry timer with the round `current.view.round` read at that moment (`A-06`), and only then check the target: if `current.view.round > round` it MUST send nothing.
Source: consensus/wbft/core/roundchange.go:52-61, consensus/wbft/core/core.go:426-450

[WBFT-SM-053] Otherwise the node MUST broadcast a ROUND-CHANGE with:

| Field | Value |
|---|---|
| `sequence` | `current.view.sequence` |
| `round` | the target `round` |
| `prepared_round` | `current.prepared_round` (absent when `None`) |
| `prepared_digest` | `block_hash(current.prepared_block)`, or the zero hash when `prepared_block is None` |
| `prepared_block` | `current.prepared_block` (absent when `None`) |
| `justification` | `prepared_certificate`, or empty when `None` |
| signature | ECDSA over the signing payload (`A-03`; the prepared pair is encoded only when both `prepared_round` and a non-zero `prepared_digest` are present) |

Source: consensus/wbft/core/roundchange.go:63-97, consensus/wbft/messages/roundchange.go:43-62, consensus/wbft/messages/roundchange.go:158-168
Observable: network

`justification` is taken from `prepared_certificate`, which has a different lifetime from the lock (§3.1). A ROUND-CHANGE can therefore carry a non-empty justification with no prepared pair (after the bad-block rule, WBFT-SM-024) or a justification from the previous sequence (after WBFT-SM-028's `CATCH_UP` case). Receivers ignore a justification that comes without a prepared pair (WBFT-SM-054).

### 11.2 Receipt and storage

[WBFT-SM-054] A ROUND-CHANGE classified `PROCESS` (necessarily `m.round >= current.view.round`) MUST be handled as follows. If `m.prepared_round`, `m.prepared_block` are both present and `m.justification` is non-empty:

1. if `m.prepared_block.number != current.view.sequence` the node MUST return `ERR` without storing `m`;
2. if `block_hash(m.prepared_block) != m.prepared_digest` the node MUST return `ERR` without storing `m` (the decoder of `A-03` already rejects this case);
3. otherwise the node adds `m` with `(pr, pb, prepares) = (m.prepared_round, m.prepared_block, m.justification)`.

In every other case the node adds `m` with `(pr, pb, prepares) = (None, None, None)`, even if `m` carries a prepared pair.
Source: consensus/wbft/core/roundchange.go:117-151, consensus/wbft/messages/roundchange.go:283-291
Observable: network

[WBFT-SM-055] `round_changes.add(r, m, pr, pb, prepares, Q)` MUST store `m` as `messages[uint64(r)][m.source]`, replacing any earlier ROUND-CHANGE from the same source for the same round, and then, if `pr is not None` and (`highest_prepared_round[r]` is unset or `pr > highest_prepared_round[r]`) and `has_matching_round_change_and_prepares(m, prepares, Q)` holds, set `highest_prepared_round[r] = pr`, `highest_prepared_block[r] = pb`, `highest_prepared_justification[r] = prepares`.
Source: consensus/wbft/core/roundchange.go:259-284

`highest_prepared_*` is monotone per round: replacing a source's ROUND-CHANGE never lowers it, even if the replaced message was the one that set it.

### 11.3 Round-change set helpers

```python
def has_matching_round_change_and_prepares(rc, prepares, Q) -> bool:
    ps = dedup_by_source(prepares)                 # keep the first message of each source
    if len(ps) < Q:
        return False
    return all(p.digest == rc.prepared_digest and p.round == rc.prepared_round for p in ps)

def higher_round_senders(rcs, r) -> int:           # distinct sources with a ROUND-CHANGE for any round > r
    return len({src for k, msgs in rcs.messages.items() if k > uint64(r) for src in msgs})

def count_at_round(rcs, r) -> int:
    return len(rcs.messages.get(uint64(r), {}))

def min_round_above(rcs, r) -> Round:              # smallest key > r, including keys whose set is empty
    keys = sorted(k for k in rcs.messages if k > uint64(r))
    return keys[0] if keys else r

def clear_lower_than(rcs, r):                      # also deletes every round whose set is empty
    for k in list(rcs.messages):
        if len(rcs.messages[k]) == 0 or k < uint64(r):
            delete rcs.messages[k], rcs.highest_prepared_round[k], rcs.highest_prepared_block[k], rcs.highest_prepared_justification[k]

def new_round(rcs, r):
    rcs.messages.setdefault(uint64(r), {})
    rcs.highest_prepared_justification.setdefault(uint64(r), [])
```

[WBFT-SM-056] The helpers above MUST behave exactly as written. In particular `has_matching_round_change_and_prepares` MUST deduplicate by source keeping the first occurrence, MUST NOT check the sequence of the PREPAREs, and `min_round_above` MUST consider keys whose message set is empty.
Source: consensus/wbft/core/justification.go:133-151, consensus/wbft/core/justification.go:174-187, consensus/wbft/core/roundchange.go:246-256, consensus/wbft/core/roundchange.go:288-346

### 11.4 F+1 rule

[WBFT-SM-057] After a ROUND-CHANGE has been added as in §11.2 (a ROUND-CHANGE rejected by §11.2 ends processing with `ERR`), with `r0` the node's round before the message was processed, `num = higher_round_senders(round_changes, r0)`, if `F < num <= F + 1` the node MUST run `start_new_round(node, min_round_above(round_changes, r0))` and then `broadcast_round_change(node, that round)`, and MUST NOT evaluate the quorum rule of §11.5 for this message. The handler returns `OK`.
Source: consensus/wbft/core/roundchange.go:153-169
Observable: network, log

With integer `num` and real `F = (n-1)/3`, the window `F < num <= F+1` contains exactly one integer, `floor(F) + 1`. The rule fires when the count *reaches* that value. If the count is already larger when a ROUND-CHANGE is processed (possible after the node's own round advanced), the rule does not fire. The count includes sources whose ROUND-CHANGE was stored in a previous sequence when WBFT-SM-028's `CATCH_UP` case applies.

### 11.5 Quorum rule for the proposer

[WBFT-SM-058] If the F+1 rule did not fire, and `count_at_round(round_changes, r0) >= Q`, and the node is the proposer of `node.validators`, and `current.preprepare_sent < r0`, the node MUST select a proposal:

1. `highest_prepared_block[r0]` if set;
2. otherwise `current.pending_request` if set;
3. otherwise return `ERR` (the ROUND-CHANGE is not relayed) and send nothing.

Source: consensus/wbft/core/roundchange.go:170-186

[WBFT-SM-059] With `rcs = list(round_changes.messages[r0].values())` and `ps = round_changes.highest_prepared_justification[r0]` (empty when unset), the node MUST run `is_justified(proposal, current.view, [rc.payload for rc in rcs], ps, Q)`. If it fails the node MUST send nothing and return `OK`. If it holds the node MUST run `send_preprepare(node, proposal, rcs, ps)` (§8.1) and return `OK`.
Source: consensus/wbft/core/roundchange.go:188-216
Observable: network

The PRE-PREPARE therefore carries *every* ROUND-CHANGE the node holds for the round (possibly more than `Q`, in unspecified order) and the justification PREPAREs of the highest prepared ROUND-CHANGE as that ROUND-CHANGE carried them.

[WBFT-SM-060] A ROUND-CHANGE for which neither rule fires MUST be stored as in §11.2 and the handler returns `OK`.
Source: consensus/wbft/core/roundchange.go:213-216

### 11.6 `is_justified`

[WBFT-SM-061] `is_justified(proposal, target_view, round_changes, prepares, Q)` MUST return true exactly when all of the following steps pass, evaluated in this order:

```python
def is_justified(proposal, target_view, round_changes, prepares, Q) -> bool:
    rcs = dedup_by_source(round_changes)          # 1. keep the first message of each source
    ps  = dedup_by_source(prepares)
    if len(rcs) < Q:                              # 2. quorum of ROUND-CHANGE
        return False
    for rc in rcs:                                # 3. every ROUND-CHANGE is for target_view
        if rc.sequence != target_view.sequence or rc.round != target_view.round:
            return False
    if len(ps) != 0 and len(ps) < Q:              # 4. PREPAREs: none or a quorum
        return False
    prepared_round = None
    if len(ps) > 0:                               # 5. all PREPAREs same round, digest == proposal
        prepared_round = ps[0].round
        for p in ps:
            if p.round != prepared_round:          return False
            if p.digest != block_hash(proposal):   return False
    if prepared_round is None:                    # 6. no PREPAREs: Q ROUND-CHANGEs without prepared pair
        nil_count = 0
        for rc in rcs:
            if (rc.prepared_round is None or rc.prepared_round == 0) and rc.prepared_digest == ZERO_HASH:
                nil_count += 1
                if nil_count == Q:
                    return True
        return False
    lower_or_equal = 0                            # 7. Q ROUND-CHANGEs with pr <= prepared_round,
    has_match = False                             #    one of them with (pr, digest) == (prepared_round, proposal)
    for rc in rcs:
        if rc.prepared_round is None or rc.prepared_round <= prepared_round:
            lower_or_equal += 1
            if rc.prepared_round == prepared_round and rc.prepared_digest == block_hash(proposal):
                has_match = True
            if lower_or_equal >= Q and has_match:
                return True
    return False
```

Source: consensus/wbft/core/justification.go:42-129, consensus/wbft/core/justification.go:156-187

Notes on the algorithm:

- Step 1 (deduplication) and step 3 (stale-view rejection) were added by go-stablenet PRs #84 and #85. Both are present in `v1.1.0` and at the reference commit (the files are identical). A node without them accepts a justification that repeats one validator's message or reuses ROUND-CHANGEs of an earlier view; a conforming node MUST reject those.
- Step 6 counts a ROUND-CHANGE with `prepared_round == 0` and a zero digest as "not prepared". A ROUND-CHANGE of a node prepared in round 0 has a non-zero digest and is not counted.
- Step 6 returns true when the running count reaches `Q` exactly inside the loop, as the reference does (`consensus/wbft/core/justification.go:102`). Step 7 compares with `>=` inside the loop, also as the reference does (`:122`), because `has_match` can become true after the count has passed `Q`. Both differ from a plain "count at least `Q`" only for `Q = 0`, which does not occur: `quorum_size(n) >= 1` for every `n` (`A-04` §2).
- Neither `prepared_round < target_view.round` nor the sequence of the PREPAREs is checked.

[WBFT-SM-062] The receiver of a PRE-PREPARE with `round > 0` MUST apply `is_justified` with `target_view = m.view` and `Q` of its current validator set (WBFT-SM-037). The sender MUST apply it with `target_view = current.view` before sending (WBFT-SM-059).
Source: consensus/wbft/core/preprepare.go:135-145, consensus/wbft/core/roundchange.go:197-205
Observable: network

---

## 12. Extra seals

### 12.1 Purpose

A seal that arrives after the node has passed the quorum it belongs to is kept as an *extra seal*. Extra seals for the head block are merged into `PrevPreparedSeal` / `PrevCommittedSeal` of the next block the node proposes (`A-08`). They matter for the header and, through the sealer bitmaps, for diligence (`A-04`).

### 12.2 Acceptance

[WBFT-SM-063] For a message classified `EXTRA_SEAL`, the node MUST select a target block and validator set:

- if `state == AcceptRequest`: `block = prior.proposal`, `vs = prior.validators`;
- otherwise: `block = current.preprepare.proposal` (or `None`), `vs = node.validators`.

If `block is None` the node MUST store nothing and return `OK` (the message is relayed).
Source: consensus/wbft/core/extraseal.go:29-48

[WBFT-SM-064] Otherwise:

1. a PREPARE MUST be rejected with `ERR` if `m.digest != block_hash(block)` or if `verify_seal(vs, header(block), uint32(m.round), PREPARE_SEAL, m.prepare_seal, m.source)` fails, and otherwise stored with `store_extra(extra_prepare_seals, m)`;
2. a COMMIT likewise with `COMMIT_SEAL`, `m.commit_seal` and `extra_commit_seals`;
3. any other code MUST be rejected with `ERR`.

A stored or ignored extra seal returns `OK` and is relayed (WBFT-SM-011).
Source: consensus/wbft/core/extraseal.go:50-87, consensus/wbft/core/handler.go:218-222
Observable: network

### 12.3 Storage

[WBFT-SM-065] `store_extra(map, m)` MUST keep at most one message per source: it stores `m` unless the map already holds a message from `m.source` whose view is greater than or equal to `m.view`.
Source: consensus/wbft/core/extraseal.go:89-115

### 12.4 Effective seals

[WBFT-SM-066] `add_effective_seals_to_extra_seals(node)` MUST apply `store_extra` to every message of `current.prepares` (into `extra_prepare_seals`) and of `current.commits` (into `extra_commit_seals`).
Source: consensus/wbft/core/extraseal.go:118-129

This runs when the node leaves a sequence (WBFT-SM-023), on the message sets of the round it is leaving. If the node was not in the decided round at that moment (for example it moved to the next round after deciding, WBFT-SM-050), those sets are empty or belong to another round.

### 12.5 Use and clearing

[WBFT-SM-067] When the application builds the header of the next proposal (`A-08`), the node MUST supply the extra seals selected by:

```python
def process_extra_seals(node, head):   # head = app.head() at build time
    target = View(head.number, node.prior.round)
    def pick(msgs, seal_of):
        out = []
        for m in msgs.values():                     # order unspecified
            if m.view == target and m.digest == block_hash(head):
                idx = index_of(node.prior.validators, m.source)
                if idx >= 0:
                    out.append(SealData(sealer=idx, seal=seal_of(m)))
        return out
    return (pick(node.extra_prepare_seals, lambda m: m.prepare_seal),
            pick(node.extra_commit_seals,  lambda m: m.commit_seal))
```

If the consensus core is not running, both lists are empty. The merge into the previous-block seals (sealers already present are skipped) is specified in `A-08`.
Source: consensus/wbft/core/extraseal.go:133-183, consensus/wbft/backend/engine.go:154-167, consensus/wbft/backend/engine.go:221-229, consensus/wbft/engine/engine.go:527-532, consensus/wbft/engine/engine.go:1371-1397
Observable: header

The selection uses the node's `prior.round`, while the merge writes `PrevRound` from the `Round` of the node's stored head header. The two differ when the node's stored head is a variant decided in another round (WBFT-SM-048), and then the merged aggregate does not verify.

[WBFT-SM-068] `clear_extra_seals(node, n)` MUST delete every extra seal whose sequence is less than `n`. It is called with `n = head.number` when `start_new_round` is called with argument 0 (WBFT-SM-028), and with `n = current.view.sequence + 1` by the bad-block rule (WBFT-SM-024), which also deletes the extra seals of the previous sequence.
Source: consensus/wbft/core/extraseal.go:186-204

---

## 13. Backlog

### 13.1 Admission

[WBFT-SM-069] `add_to_backlog(node, m)` MUST discard `m` if `m.source == node.address`.
Source: consensus/wbft/core/backlog.go:210-214

[WBFT-SM-070] For every other source the node MUST keep at most one backlogged message per key `(m.code, uint64(m.sequence), uint64(m.round))`: a message whose key is already queued for its source MUST be discarded, so the first message for a slot wins until that slot is replayed or dropped.
Source: consensus/wbft/core/backlog.go:221-235, consensus/wbft/core/backlog.go:242

[WBFT-SM-071] The node MUST bound the number of backlogged messages per source. The reference bound is `MAX_BACKLOG_SIZE_PER_VALIDATOR = 4 * (ROUND_THRESHOLD + 1) * (SEQUENCE_THRESHOLD + 1) = 88`; a message that would exceed it is discarded. The bound is a local parameter: an implementation MAY use a different value not smaller than the number of distinct keys admissible under WBFT-SM-019 and WBFT-SM-070.
Source: consensus/wbft/core/backlog.go:44-51, consensus/wbft/core/backlog.go:236-240

The admission checks (WBFT-SM-069 to WBFT-SM-071, together with the too-far rules of WBFT-SM-019) are the hardening added by go-stablenet PR #89. The per-source key and the size bound are skipped for the first message of a source, which cannot violate them.

### 13.2 Priority

[WBFT-SM-072] Each source's queue MUST release messages in decreasing order of

```python
def backlog_priority(m) -> int64:
    if m.code == ROUND_CHANGE:
        return -int64(uint64(m.sequence) * 1000)
    return -int64(uint64(m.sequence) * 1000 + uint64(m.round) * 10 + {PREPREPARE: 1, COMMIT: 2, PREPARE: 3}[m.code])
```

with ties released in unspecified order. Within one sequence this releases ROUND-CHANGE first, then by increasing round, and within a round PRE-PREPARE, then COMMIT, then PREPARE.
Source: consensus/wbft/core/backlog.go:34-42, consensus/wbft/core/backlog.go:243, consensus/wbft/core/backlog.go:309-318, common/prque/prque.go:46-51

### 13.3 Replay

[WBFT-SM-073] Every time `set_state` runs (including when the state does not change), the node MUST run `process_backlog`:

```python
def process_backlog(node):
    for src in list(node.backlog):                       # order across sources unspecified
        if src not in node.validators:
            delete node.backlog[src], node.backlog_keys[src]
            continue
        q = node.backlog[src]
        while not q.empty():
            m = q.pop()                                  # WBFT-SM-072 order
            r = check_message(node, m.code, m.view)
            if r == FUTURE:
                q.push(m)                                # put back and stop this source
                break
            node.backlog_keys[src].discard(key(m))
            if r in (PROCESS, EXTRA_SEAL):
                schedule(Backlog(m))
            # OLD, INVALID, TOO_FAR: dropped
```

Source: consensus/wbft/core/core.go:308-310, consensus/wbft/core/backlog.go:250-307

The per-source stop at the first `FUTURE` message combined with the order of WBFT-SM-072 means that, in `Preprepared`, a source's backlogged COMMIT for the current view (still `FUTURE`) blocks that source's backlogged PREPARE for the same view. Such PREPAREs are replayed only after the node reaches `Prepared` by other means, and then as extra seals.

[WBFT-SM-074] A replayed message MUST NOT have its signatures verified again; its `source` is the one recovered on first receipt. Its processing MUST run `check_message` again at the time the `Backlog` event is processed (§15), so a replayed message is treated as if it had arrived at that moment.
Source: consensus/wbft/core/handler.go:136-142

---

## 14. Timers as events

Durations are specified in `A-06`. This section specifies what the expiry of each timer does.

### 14.1 Round timer

[WBFT-SM-075] The node MUST start (restart) the round timer at the end of every `start_new_round` that selects a branch (WBFT-SM-030) and on every PRE-PREPARE acceptance (WBFT-SM-039), in both cases for `current.view`. Each start cancels the previous round timer: a `RoundTimeout` of a cancelled timer that is already queued MUST be ignored when processed. This holds within one run of the consensus core: a round timer that the event loop arms after `Stop` has cancelled the timers is not cancelled, and a restarted core processes its expiry (`A-06` WBFT-TIMER-018).
Source: consensus/wbft/core/core.go:331-347, consensus/wbft/core/core.go:405-413, consensus/wbft/core/handler.go:152-160, consensus/wbft/core/preprepare.go:175-185, consensus/wbft/core/handler.go:50-59

### 14.2 Round timeout

These cases follow from §6 and §11.1:

1. Normal: the head is still `current.view.sequence - 1`. The node enters round `r + 1` and sends ROUND-CHANGE `(sequence, r + 1)` with its lock.
2. After a decision whose block is not yet imported (WBFT-SM-050): same as 1, for a sequence that has already been decided locally. When the head arrives the node catches up (`NewHead`, round 0). The node can also leave the decided round earlier through the F+1 rule (§11.4) when other validators' timers fired first.
3. Late timeout after the head advanced but before the `NewHead` event is processed: `start_new_round` takes branch `CATCH_UP` with argument `r + 1` (WBFT-SM-022, WBFT-SM-028, WBFT-SM-029), the node enters `(head + 1, 0)`, and then sends ROUND-CHANGE `(head + 1, r + 1)` without a prepared pair but with the previous sequence's `prepared_certificate` as justification. The retry timer is armed with round 0, so the retry that follows is a ROUND-CHANGE `(head + 1, 0)`.

### 14.3 Retry timeout

[WBFT-SM-077] On a `RetryTimeout(r)` the node MUST run `broadcast_round_change(node, r)`. Retry timeouts are not cancellable: one that is already queued when the retry timer is stopped is still processed.
Source: consensus/wbft/core/handler.go:170-178, consensus/wbft/core/core.go:417-450
Observable: network

Because each `broadcast_round_change` re-arms the retry timer (WBFT-SM-052), a node that has sent a ROUND-CHANGE repeats a ROUND-CHANGE every retry interval until the retry timer is stopped by a round-timer start (a new round or a PRE-PREPARE acceptance). The repeated message targets the round the node was in when the timer was armed, which after WBFT-SM-049 is the decided round, not the round of the ROUND-CHANGE that armed it.

---

## 15. Reference pseudocode

This section is the complete model. Functions not defined here are defined in the sections referenced or in other chapters. `Q`, `F` are evaluated as in §1.2.

```python
# ------------------------------------------------------------------ events

def on_start(node):
    init_state(node)                                    # WBFT-SM-004
    start_new_round(node, 0)

def on_stop(node):
    stop_all_timers(node)                               # A-06
    discard(node)                                       # WBFT-SM-007

def on_new_head(node):
    start_new_round(node, 0)                            # WBFT-SM-010

def on_request(node, p):
    r = check_request(node, p)
    if r == "FUTURE":
        node.pending_requests.push(p, priority=-int64(p.number))
        return
    if r != "OK":
        return
    node.current.pending_request = p
    if node.state == AcceptRequest and uint64(node.current.view.round) == 0:
        send_preprepare(node, p, None, None)

def on_message_received(node, code, payload):
    if code not in (PREPREPARE, PREPARE, COMMIT, ROUND_CHANGE):
        return
    m = decode_message(code, payload)                   # A-03
    if m is None or not verify_message_signatures(node, m):
        return
    if handle_decoded(node, m) == OK:
        relay(node, code, payload)                      # A-07, to node.validators minus self

def on_backlog_event(node, m):                          # replay, or future PRE-PREPARE re-injection
    if handle_decoded(node, m) == OK:
        relay(node, m.code, rlp_encode(m))

def on_round_timeout(node, timer):
    if timer.cancelled:
        return
    nxt = node.current.view.round + 1
    start_new_round(node, nxt)
    broadcast_round_change(node, nxt)

def on_retry_timeout(node, r):
    broadcast_round_change(node, r)

# ------------------------------------------------------------------ intake

def verify_message_signatures(node, m) -> bool:
    members = [m]
    if m.code == ROUND_CHANGE:
        members += m.justification
    elif m.code == PREPREPARE:
        members += m.justification_round_changes + m.justification_prepares
    for x in members:
        signer = ecdsa_recover_address(message_signing_payload(x), x.signature)   # hashes internally (A-02)
        if signer is None or signer not in signature_validator_set(node, x.view):
            return False
        x.source = signer
    return True

def handle_decoded(node, m):
    r = check_message(node, m.code, m.view)
    if r == FUTURE:
        add_to_backlog(node, m)
        return ERR
    if r == EXTRA_SEAL:
        return add_extra_seal(node, m)
    if r != PROCESS:
        return ERR
    return {PREPREPARE: handle_preprepare, PREPARE: handle_prepare,
            COMMIT: handle_commit, ROUND_CHANGE: handle_round_change}[m.code](node, m)

def check_message(node, code, v) -> CheckResult:
    cur, s = node.current.view, node.state
    if v.sequence > cur.sequence + SEQUENCE_THRESHOLD:                       return TOO_FAR
    if v.sequence > cur.sequence and v.round >= ROUND_THRESHOLD:              return TOO_FAR
    if v.sequence == cur.sequence and v.round > cur.round + ROUND_THRESHOLD:  return TOO_FAR
    if code == ROUND_CHANGE:
        if v.sequence > cur.sequence: return FUTURE
        if v < cur:                   return OLD
        return PROCESS
    if v > cur:
        return FUTURE
    if v < cur:
        if cur.sequence - v.sequence == 1 and v.round == node.prior.round and s == AcceptRequest:
            return EXTRA_SEAL
        return OLD
    if s == AcceptRequest:
        return PROCESS if code == PREPREPARE else FUTURE
    if s == Preprepared:
        return INVALID if code == PREPREPARE else (PROCESS if code == PREPARE else FUTURE)
    if s == Prepared:
        return EXTRA_SEAL if code == PREPARE else (INVALID if code == PREPREPARE else PROCESS)
    # Committed
    return EXTRA_SEAL if code in (PREPARE, COMMIT) else INVALID

# ------------------------------------------------------------------ view changes

def start_new_round(node, round):
    last, last_proposer = app.head()
    if node.current is None:
        round_change = False
    elif last.number >= node.current.view.sequence:
        round_change = False                                         # CATCH_UP
    elif last.number == int64(node.current.view.sequence) - 1:        # int64 arithmetic (§6.2)
        if round == 0 or round < node.current.view.round:
            return
        round_change = True
    else:
        return
    if round_change:
        new_view, next_vs = View(node.current.view.sequence, round), node.validators
    else:
        new_view, next_vs = View(last.number + 1, 0), validators_at(last.number + 1)
        if node.current is not None:
            add_effective_seals_to_extra_seals(node)
    update_round_state(node, next_vs, new_view, round_change)
    node.validators.proposer = calc_proposer(node.validators, last_proposer, new_view.round, policy)
    set_state(node, AcceptRequest)
    if round == 0:                                                   # the argument, not new_view.round
        node.prepared_certificate = None
        node.round_changes = RoundChangeSet()
        clear_extra_seals(node, last.number)
    else:
        clear_lower_than(node.round_changes, round)
    new_round(node.round_changes, round)
    app.notify_new_round(round)                                      # the argument
    start_round_timer(node, node.current.view)                       # A-06; stops retry and future timers

def update_round_state(node, next_vs, view, round_change):
    if round_change and node.current is not None:
        cur = node.current
        if cur.prepared_block is not None and app.is_bad_block(block_hash(cur.prepared_block)):
            cur.prepared_round, cur.prepared_block = None, None
            clear_extra_seals(node, cur.view.sequence + 1)
        node.current = RoundState(view, cur.preprepare, {}, {}, cur.prepared_round,
                                  cur.prepared_block, cur.pending_request, preprepare_sent=0)
    else:
        if node.current is not None:
            update_prior_state(node)
        node.current = RoundState(view, None, {}, {}, None, None, None, preprepare_sent=0)
    node.validators = next_vs

def set_state(node, s):
    node.state = s
    if s == AcceptRequest:
        process_pending_requests(node)                               # §7.3
    process_backlog(node)                                            # §13.3

# ------------------------------------------------------------------ normal case

def send_preprepare(node, p, round_changes, prepares):
    v = node.current.view
    if not (p.number == v.sequence and is_proposer(node.validators, node.address)):
        return
    m = PrePrepareMsg(v.sequence, v.round, p)
    m.signature = ecdsa_sign(node.node_key, message_signing_payload(m))   # hashes internally (A-02)
    m.justification_round_changes = [rc.payload for rc in round_changes] if round_changes is not None else []
    m.justification_prepares = prepares if prepares is not None else []
    if broadcast(node, m):
        node.current.preprepare_sent = v.round

def handle_preprepare(node, m):
    if not is_proposer(node.validators, m.source):                  return ERR
    if uint64(m.sequence) != uint64(m.proposal.number):              return ERR
    if uint64(m.round) > 0 and not is_justified(m.proposal, m.view, m.justification_round_changes,
                                                m.justification_prepares, Q):
        return ERR
    res = app.validate_proposal(m.proposal)
    if res.is_future:
        arm_future_proposal_timer(node, res.duration, m)             # schedules Backlog(m)
        return ERR
    if not res.is_valid:
        return ERR
    if node.state == AcceptRequest:
        start_round_timer(node, node.current.view)
        node.current.preprepare = m
        set_state(node, Preprepared)
        broadcast_prepare(node)
    return OK

def broadcast_prepare(node):
    v, p = node.current.view, node.current.preprepare.proposal
    m = PrepareMsg(v.sequence, v.round, block_hash(p),
                   bls_sign(seal_data(header(p), uint32(v.round), PREPARE_SEAL)))
    m.signature = ecdsa_sign(node.node_key, message_signing_payload(m))   # hashes internally (A-02)
    broadcast(node, m)

def handle_prepare(node, m):
    p = node.current.preprepare.proposal
    if m.digest != block_hash(p):                                                      return ERR
    if not verify_seal(node.validators, header(p), uint32(m.round), PREPARE_SEAL, m.prepare_seal, m.source):
        return ERR
    node.current.prepares[m.source] = m
    if len(node.current.prepares) >= Q and node.state < Prepared:
        node.current.prepared_round = node.current.view.round
        node.prepared_certificate = [copy(x) for x in node.current.prepares.values()]
        node.current.prepared_block = p
        set_state(node, Prepared)
        broadcast_commit(node)
    return OK

def broadcast_commit(node):
    v, p = node.current.view, node.current.preprepare.proposal
    m = CommitMsg(v.sequence, v.round, block_hash(p),
                  bls_sign(seal_data(header(p), uint32(v.round), COMMIT_SEAL)))
    m.signature = ecdsa_sign(node.node_key, message_signing_payload(m))   # hashes internally (A-02)
    broadcast(node, m)

def handle_commit(node, m):
    p = node.current.preprepare.proposal
    if m.digest != block_hash(p):                                                      return ERR
    if not verify_seal(node.validators, header(p), uint32(m.round), COMMIT_SEAL, m.commit_seal, m.source):
        return ERR
    node.current.commits[m.source] = m
    if len(node.current.commits) >= Q:
        decide(node)
    return OK

def decide(node):
    set_state(node, Committed)
    p = node.current.preprepare.proposal
    prepared  = [SealData(index_of(node.validators, x.source), x.prepare_seal) for x in node.current.prepares.values()]
    committed = [SealData(index_of(node.validators, x.source), x.commit_seal)  for x in node.current.commits.values()]
    if not app.finalize(p, prepared, committed, node.current.view.round):
        broadcast_round_change(node, node.current.view.round + 1)
    # the round timer keeps running (WBFT-SM-050)

# ------------------------------------------------------------------ round change

def broadcast_round_change(node, round):
    arm_retry_timer(node, node.current.view.round)                   # A-06; fires RetryTimeout(that round)
    if node.current.view.round > round:
        return
    cur = node.current
    m = RoundChangeMsg(sequence=cur.view.sequence, round=round,
                       prepared_round=cur.prepared_round,
                       prepared_digest=block_hash(cur.prepared_block) if cur.prepared_block else ZERO_HASH,
                       prepared_block=cur.prepared_block)
    m.signature = ecdsa_sign(node.node_key, message_signing_payload(m))   # hashes internally (A-02)
    m.justification = node.prepared_certificate if node.prepared_certificate is not None else []
    broadcast(node, m)

def handle_round_change(node, m):
    r0 = node.current.view.round
    if m.round >= r0:                                                # always true after check_message
        pr = pb = prepares = None
        if m.prepared_round is not None and m.prepared_block is not None and len(m.justification) > 0:
            if m.prepared_block.number != node.current.view.sequence: return ERR
            if block_hash(m.prepared_block) != m.prepared_digest:     return ERR
            pr, pb, prepares = m.prepared_round, m.prepared_block, m.justification
        round_changes_add(node.round_changes, m.round, m, pr, pb, prepares, Q)
    num = higher_round_senders(node.round_changes, r0)
    if F < num <= F + 1:
        target = min_round_above(node.round_changes, r0)
        start_new_round(node, target)
        broadcast_round_change(node, target)
    elif (count_at_round(node.round_changes, r0) >= Q
          and is_proposer(node.validators, node.address)
          and node.current.preprepare_sent < r0):
        proposal = node.round_changes.highest_prepared_block.get(uint64(r0))
        if proposal is None:
            if node.current.pending_request is None:
                return ERR
            proposal = node.current.pending_request
        rcs = list(node.round_changes.messages[uint64(r0)].values())
        ps = node.round_changes.highest_prepared_justification.get(uint64(r0), [])
        if not is_justified(proposal, node.current.view, [rc.payload for rc in rcs], ps, Q):
            return OK
        send_preprepare(node, proposal, rcs, ps)
    return OK

def round_changes_add(rcs, r, m, pr, pb, prepares, Q):
    rcs.messages.setdefault(uint64(r), {})[m.source] = m
    k = uint64(r)
    if pr is not None and (k not in rcs.highest_prepared_round or pr > rcs.highest_prepared_round[k]):
        if has_matching_round_change_and_prepares(m, prepares, Q):
            rcs.highest_prepared_round[k] = pr
            rcs.highest_prepared_block[k] = pb
            rcs.highest_prepared_justification[k] = prepares

# ------------------------------------------------------------------ extra seals and backlog

def add_extra_seal(node, m):
    if node.state == AcceptRequest:
        block, vs = node.prior.proposal, node.prior.validators
    else:
        block = node.current.preprepare.proposal if node.current.preprepare else None
        vs = node.validators
    if block is None:
        return OK
    if m.code == PREPARE:
        if m.digest != block_hash(block): return ERR
        if not verify_seal(vs, header(block), uint32(m.round), PREPARE_SEAL, m.prepare_seal, m.source): return ERR
        store_extra(node.extra_prepare_seals, m)
        return OK
    if m.code == COMMIT:
        if m.digest != block_hash(block): return ERR
        if not verify_seal(vs, header(block), uint32(m.round), COMMIT_SEAL, m.commit_seal, m.source): return ERR
        store_extra(node.extra_commit_seals, m)
        return OK
    return ERR

def store_extra(msgs, m):
    old = msgs.get(m.source)
    if old is not None and old.view >= m.view:
        return
    msgs[m.source] = m

def add_to_backlog(node, m):
    if m.source == node.address:
        return
    k = (m.code, uint64(m.sequence), uint64(m.round))
    if m.source in node.backlog:
        if k in node.backlog_keys[m.source]:                         return
        if len(node.backlog[m.source]) >= MAX_BACKLOG_SIZE_PER_VALIDATOR:    return
    else:
        node.backlog[m.source], node.backlog_keys[m.source] = PriorityQueue(), set()
    node.backlog_keys[m.source].add(k)
    node.backlog[m.source].push(m, backlog_priority(m))
```

Source: consensus/wbft/core/*.go as cited in §§4-14

---

## 16. Message-handling outcomes

The table lists every way the processing of a received consensus message can end. "Relay" is WBFT-SM-011/012.

| # | Situation | State change | Sends | Relay |
|---|---|---|---|---|
| 1 | unknown code, undecodable payload | none | none | no |
| 2 | signature of message or of a justification member invalid, or signer not in `signature_validator_set` | none | none | no |
| 3 | `TOO_FAR` | none | none | no |
| 4 | `OLD` | none | none | no |
| 5 | `INVALID` (PRE-PREPARE for current view outside `AcceptRequest`) | none | none | no |
| 6 | `FUTURE`, admitted to backlog | backlog + key | none now; replay later | no (later yes if replay returns `OK`) |
| 7 | `FUTURE`, from self / duplicate key / backlog full | none | none | no (never) |
| 8 | `EXTRA_SEAL`, no target block | none | none | **yes** |
| 9 | `EXTRA_SEAL`, PREPARE/COMMIT digest or seal invalid | none | none | no |
| 10 | `EXTRA_SEAL`, PREPARE/COMMIT valid, no stored seal from the source or newer than the stored one | extra seal stored | none | yes |
| 11 | `EXTRA_SEAL`, PREPARE/COMMIT valid, not newer than stored | none | none | **yes** |
| 12 | `EXTRA_SEAL`, PRE-PREPARE (or other code) with a target block | none | none | no |
| 13 | PRE-PREPARE not from proposer / sequence ≠ proposal number / not justified / proposal invalid | none | none | no |
| 14 | PRE-PREPARE proposal in the future | future-proposal timer armed | none now | no (later yes if accepted) |
| 15 | PRE-PREPARE accepted | preprepare, `Preprepared`, timer restarted, backlog replayed | PREPARE | yes |
| 16 | PREPARE digest or seal invalid | none | none | no |
| 17 | PREPARE stored, below quorum | `prepares` | none | yes |
| 18 | PREPARE reaches quorum | lock, certificate, `Prepared`, backlog replayed | COMMIT | yes |
| 19 | COMMIT digest or seal invalid | none | none | no |
| 20 | COMMIT stored, below quorum | `commits` | none | yes |
| 21 | COMMIT reaches quorum, `finalize` succeeds | `Committed`, backlog replayed | none (application stores the block) | yes |
| 22 | COMMIT reaches quorum, `finalize` fails | `Committed`, backlog replayed, retry timer armed | ROUND-CHANGE(round+1) | yes |
| 23 | ROUND-CHANGE prepared block for another sequence | none | none | no |
| 24 | ROUND-CHANGE stored, no rule fires | round-change set | none | yes |
| 25 | ROUND-CHANGE stored, F+1 rule fires | new round | ROUND-CHANGE(new round) | yes |
| 26 | ROUND-CHANGE stored, quorum rule, justified | `preprepare_sent` | PRE-PREPARE | yes |
| 27 | ROUND-CHANGE stored, quorum rule, self-check fails | none beyond storage | none | **yes** |
| 28 | ROUND-CHANGE stored, quorum rule, no proposal available | none beyond storage | none | **no** |

Source: consensus/wbft/core/handler.go:111-151, consensus/wbft/core/handler.go:188-227, and the handlers cited in §§8-13

---

## 17. Consequences of asynchronous processing

These requirements define the set of behaviours a conforming node may show because of WBFT-SM-002. An observer MUST NOT treat any of them as a protocol violation.

[WBFT-SM-078] A node's own PREPARE (resp. COMMIT) MAY be processed after the node has already reached `Prepared` (resp. `Committed`) on other validators' messages, in which case it is handled as an extra seal and is absent from the `PreparedSeal` (resp. `CommittedSeal`) the node builds. Likewise the proposer's own PRE-PREPARE is processed only when its self-delivery is, so the proposer enters `Preprepared` and sends its PREPARE after that delivery.
Source: consensus/wbft/backend/backend.go:164-172, consensus/wbft/core/backlog.go:183-199
Observable: header, network

[WBFT-SM-079] Messages replayed from the backlog MAY be processed in an order different from the priority order of WBFT-SM-072, interleaved with other events. Each is classified again when processed (WBFT-SM-074).
Source: consensus/wbft/core/backlog.go:303-304

[WBFT-SM-080] A round timeout that fired before the node processed a `NewHead` event MAY be processed first. The node then shows case 2 or case 3 of §14.2; in case 3 it sends a ROUND-CHANGE for the new sequence with a round greater than 0 immediately after entering that sequence.
Source: consensus/wbft/core/handler.go:152-169, consensus/wbft/core/core.go:185-196
Observable: network

[WBFT-SM-081] A `RetryTimeout` queued before the retry timer was stopped MAY be processed after the node has entered a new round or accepted a PRE-PREPARE. The node then re-arms the retry timer for its current round (WBFT-SM-052) and, if the retry's round is not below its current round, sends a ROUND-CHANGE; the re-armed timer later sends a ROUND-CHANGE for the current round even though the node may have accepted a PRE-PREPARE in it. This includes a node that has entered a new sequence through `NewHead`: at `(h + 1, 0)` a queued `RetryTimeout(r_c)` armed in sequence `h` sends ROUND-CHANGE `(h + 1, r_c)` without a prepared pair and with an empty justification, and with `r_c >= 1` that message counts towards other nodes' F+1 rule (§11.4).
Source: consensus/wbft/core/handler.go:170-178, consensus/wbft/core/roundchange.go:52-61, consensus/wbft/core/handler.go:161-169
Observable: network

[WBFT-SM-082] If two requests for the current sequence are processed in round 0 while the node is the proposer and still in `AcceptRequest` (its first PRE-PREPARE not yet self-delivered, or held as a future proposal or rejected by its own checks), the node MAY send two PRE-PREPAREs with different proposals for the same view.
Source: consensus/wbft/core/request.go:47-56, consensus/wbft/core/preprepare.go:42-51, consensus/wbft/core/preprepare.go:147-169
Observable: network

> **Implementation note (informative).** Two requests for the same sequence can arise when a request stored as `FUTURE` is replayed on entering the sequence while the block builder, notified by the same `start_new_round`, hands over a freshly built proposal. Receivers accept whichever arrives first and classify the other as `INVALID`.

Apart from WBFT-SM-082, a proposer sends at most one PRE-PREPARE per view during one run of the consensus core: in round 0 only the request path sends (WBFT-SM-032), and in a round `> 0` only the ROUND-CHANGE quorum rule sends, and only while `current.preprepare_sent` is below the round (WBFT-SM-058, WBFT-SM-036). Together with the bounds on PREPARE and COMMIT (§8.4, §10.1, WBFT-SM-083), these are the only per-view bounds on a node's own signed messages, and none of them holds across a restart (WBFT-SM-007). `A-10` WBFT-SEC-040 is subject to both qualifications.

---

## 18. Safety and liveness (informative)

### 18.1 Quorum intersection

`Q = quorum_size(n) = ceil(n - (n-1)/3) = ceil((2n+1)/3)` (`A-04`). Any two sets of `Q` validators intersect in at least `2Q - n >= (n+2)/3` validators, which exceeds `f = floor((n-1)/3)`. So two quorums always share an honest validator when at most `f` validators are faulty.

### 18.2 Agreement within a sequence

The argument is the QBFT one. If a proposal `B` is decided in `(h, r)`, at least `Q` validators sent COMMIT for `B` in round `r`, so at least `Q - f` honest validators prepared `B` in round `r` and hold the lock `(r, B)` (or a later lock). For a different proposal `B'` to be accepted in a round `r' > r`, the PRE-PREPARE must satisfy `is_justified`:

- with no PREPAREs, step 6 needs `Q` ROUND-CHANGEs for `(h, r')` without a prepared pair, which is impossible because any `Q` senders include an honest validator locked at a round `>= r`;
- with PREPAREs for a round `pr`, step 7 needs a quorum of PREPAREs for `B'` in round `pr` and `Q` ROUND-CHANGEs with prepared round `<= pr`. If `pr < r`, some honest sender reports a lock `>= r > pr`; if `pr >= r`, the `Q` PREPAREs for `B'` in round `pr` would require an honest validator to prepare `B'` after locking `B`, which by induction on rounds it does only when `B'` was itself justified.

Step 3 (stale-view rejection) is what prevents the first case from being defeated by replaying ROUND-CHANGEs from another round or sequence; step 1 prevents a single signer from being counted more than once.

### 18.3 Where the reference implementation weakens the argument

- **Bad-block unlock.** WBFT-SM-024 releases a lock when the application marks the locked block as bad. Agreement then relies on every honest validator reaching the same verdict (deterministic execution, `A-09`).
- **Unchecked prepared round.** The protocol does not require `prepared_round < round` in a ROUND-CHANGE. The justification rules do not depend on it.

### 18.4 Liveness

Liveness follows the QBFT argument under partial synchrony: round timers grow with the round (`A-06`), the F+1 rule pulls lagging validators to a round that at least one honest validator has reached, and the retry timer re-sends ROUND-CHANGE so that a peer that connected after the original send receives it (a byte-identical retransmission is not repeated towards peers that already have it, `A-06` WBFT-TIMER-024). The QBFT formal specification does not prove liveness (§18.5), so this argument is informal for QBFT as well.

### 18.5 Relation to the QBFT formal specification

The ConsenSys QBFT formal specification (`github.com/Consensys/qbft-formal-spec-and-verification`, commit `1630128e7`, Dafny, abstraction level L1) models one node as a state machine (`dafny/spec/L1/node.dfy`, `dafny/spec/L1/node_auxiliary_functions.dfy`), the network and a byzantine adversary as a distributed system (`dafny/ver/L1/distr_system_spec/`), and proves two safety properties about it. This section states those properties in WBFT terms, lists the assumptions of the proof and whether WBFT meets them, and records three definitions of the formal specification that WBFT satisfies without stating them elsewhere. Paths in this section are paths in that repository. The element-by-element comparison of the state machine is `A-13` §3.4.

**Proved properties.** The proof (`dafny/ver/L1/theorems.dfy:20-67`) establishes, for every trace allowed by the model:

- *consistency*: at every step, the chains of any two honest nodes are prefixes of one another;
- *consistency and stability*: the same holds between the chain of one honest node at one step and the chain of another honest node at any other step, so an honest node never replaces a block it has appended.

Chains are compared without their commit seals and round numbers: `consistentBlockchains` compares "raw" blocks, whose header keeps only the proposer, the height and the timestamp (`dafny/ver/L1/theorems_defs.dfy:10-16`, `dafny/spec/L1/types.dfy:31-62`). This is the WBFT block hash, which excludes `PreparedSeal`, `CommittedSeal` and `Round` (`A-03` §6). In WBFT terms the two properties say: the block hashes of the chains of any two honest nodes form sequences of which one is a prefix of the other, at any two moments. Two honest nodes can hold different seal sets and different `Round` values for the same block hash (WBFT-SM-048); the formal model allows the same, since a deciding node appends the proposed block while it multicasts a copy carrying the commit seals it collected (`dafny/spec/L1/node.dfy:364-386`). Liveness is not proved ("Formal verification of the liveness of QBFT is still pending", `README.md:15`); §18.4 is an informal argument for QBFT as well as for WBFT.

**Assumptions of the proof and WBFT.**

| Assumption | Formal specification | WBFT | Consequence |
|---|---|---|---|
| The validator set never changes | `axiomRawValidatorsNeverChange`, `dafny/ver/L1/support_lemmas/axioms.dfy:17-18` | the set changes at every epoch block (`A-04` §4, §6) | The proof does not cover a height whose validator set differs from the previous one. Safety across an epoch boundary rests on every honest node computing the same `validators_at(h)` (`A-04`) and on WBFT-SEC-001 holding at every height; neither is proved. Messages of the next sequence are verified against the current set (WBFT-SM-016). |
| At most `f(|V|)` byzantine nodes, fixed from the start | `AdversaryInit`, `dafny/ver/L1/distr_system_spec/adversary.dfy:18-23` | at most `f` byzantine validators per height (`A-10` WBFT-SEC-001) | Equivalent while the set is constant. |
| An honest node keeps its whole state for ever | `NodeNext` has no crash or restart step, `dafny/spec/L1/node.dfy:67-105`; `DSNextNode`, `dafny/ver/L1/distr_system_spec/distributed_system.dfy:56-80` | no consensus state survives a restart (WBFT-SM-007) | A restart inside a height loses the lock and the record of sent votes, which the proof relies on (§18.3). |
| The lock is released only when a block of the height is appended | `lastPreparedBlock` is reset only in `UponCommit` and `UponNewBlock`, `dafny/spec/L1/node.dfy:379-386, 407-413` | the bad-block rule releases the lock in a round change (WBFT-SM-024) | Agreement then depends on deterministic execution (§18.3, `A-10` §9). |
| `digest` is injective | `lemmaDigest`, `dafny/spec/L1/node_auxiliary_functions.dfy:52-53` | `block_hash` is Keccak-256 (`A-02` §2, `A-03` §6) | Met in the computational sense (collision resistance), as for any hash-based protocol. |
| One signature per payload and signer; the signer is recoverable; signatures cannot be forged | `lemmaSigned*`, `dafny/spec/L1/node_auxiliary_functions.dfy:56-93`; note at `:32-38` | ECDSA signatures are accepted in either `S` form (`A-10` §3); BLS seals are deterministic | Uniqueness does not hold for ECDSA, but WBFT does not depend on it: message sets keep one message per recovered source (WBFT-SM-042, WBFT-SM-045, WBFT-SM-055) and `is_justified` counts each source once (WBFT-SM-061 step 1). |
| Asynchronous network: a sent message may be delayed without bound or never delivered, but is not altered | `NetworkDeliverNext`, `dafny/ver/L1/distr_system_spec/network.dfy:29-43`; `AdversaryNext`, `dafny/ver/L1/distr_system_spec/adversary.dfy:25-82` | `A-10` §1 (partial synchrony is assumed only for liveness); relaying (`A-07`) | The safety properties need no timing assumption. Relaying delivers copies of sent messages, which the model already allows. |

**Definitions of the formal specification that WBFT satisfies.**

- *Lock invariant* (`validNodeState`, `dafny/spec/L1/node_auxiliary_functions.dfy:840-855`): the prepared round and the prepared block are present together, and while present a quorum of valid PREPAREs for them has been received. In WBFT, `prepared_round` and `prepared_block` are set together at the PREPARE quorum and cleared together (by a new sequence or the bad-block rule), and while they are set `prepared_certificate` holds exactly the `Q` PREPAREs, from distinct sources, for `(current.view.sequence, prepared_round, block_hash(prepared_block))` that set them (consensus/wbft/core/prepare.go:119-138, consensus/wbft/core/core.go:249-252, consensus/wbft/core/core.go:278-294). The converse does not hold: `prepared_certificate` can outlive the pair (WBFT-SM-053).
- *Validator set of the chain without seals* (`validators`, `dafny/spec/L1/node_auxiliary_functions.dfy:212-225`): the set that decides a height is a function of the chain with commit seals removed. In WBFT, `validators_at(h)` is read from hash-covered data only: the `EpochInfo` in the extra of the governing epoch block, and, for its computation, the previous-block seal bitmaps, `Coinbase` and `MixDigest` of hash-covered headers and the post-state of the epoch block (`A-04` §3.1, §6.1; the diligence count reads `PrevPreparedSeal` and `PrevCommittedSeal`, consensus/wbft/engine/engine.go:717-729). The node-local `PreparedSeal`, `CommittedSeal` and `Round` of a stored header are not inputs, so honest nodes that hold different seal variants of the same blocks compute the same sets.
- *Round-independent re-proposal* (`replaceRoundInBlock`, `dafny/spec/L1/node_auxiliary_functions.dfy:276-283, 548-553`): the formal block carries its round number, so a block prepared in round `pr` and re-proposed in round `r` is compared with the prepared digest after its round is set back to `pr`. WBFT keeps `Round` out of `block_hash` (`A-03` §6), so the re-proposed block is byte-identical and the comparison is plain `block_hash` equality (WBFT-SM-054, WBFT-SM-061 steps 5 and 7); the round is bound by the seals instead (`A-02` §6).

**Where the WBFT rules are weaker than the model.** `is_justified` accepts some justifications that the formal predicate `isProposalJustification` (`dafny/spec/L1/node_auxiliary_functions.dfy:515-562`) rejects: it needs `Q` unprepared ROUND-CHANGEs rather than all of them unprepared, and `Q` ROUND-CHANGEs with a prepared round at most the justified one rather than all; it does not check the sequence of justification PREPAREs or `prepared_round < round` (`A-13` §3.4). The first two relaxations are the quorum subset of the paper's rule, on which §18.2 is built. A PREPARE sequence other than the block number cannot come from an honest validator, because an honest validator prepares a proposal only when its number equals the PRE-PREPARE's sequence (WBFT-SM-037) and the digest is the hash of that proposal. An honest ROUND-CHANGE with `prepared_round == round` comes only from the retry timer (WBFT-SM-090). These arguments are informal; the proof does not cover them.

---

## 19. Properties an observer can check

These properties restate the invariants I-08 to I-15, I-23 and I-24 of the v0.1 draft, corrected against the code. The ones stated as requirements are normative for honest nodes. "Within one run" means between a start and a stop of the consensus core of that node.

[WBFT-SM-083] (I-08) Within one run, an honest node MUST NOT send two PREPAREs, or two COMMITs, with different digests for the same view. Across a restart the property does not hold (WBFT-SM-007).
Source: consensus/wbft/core/backlog.go:166-201, consensus/wbft/core/preprepare.go:172, consensus/wbft/core/prepare.go:121
Observable: network

[WBFT-SM-084] (I-09) Every PRE-PREPARE with `round > 0` sent by an honest node MUST be signed by the proposer of its view and MUST satisfy `is_justified(proposal, view, justification_round_changes, justification_prepares, Q)`.
Source: consensus/wbft/core/roundchange.go:170-212, consensus/wbft/core/preprepare.go:51
Observable: network

[WBFT-SM-085] (I-10) Every PRE-PREPARE sent by an honest node MUST have `proposal.number == sequence`, and every PREPARE and COMMIT of an honest node for a view MUST carry `digest == block_hash(p)` where `p` is the proposal of the PRE-PREPARE that node accepted for that view.
Source: consensus/wbft/core/preprepare.go:51, consensus/wbft/core/prepare.go:39-48, consensus/wbft/core/commit.go:41-50
Observable: network

[WBFT-SM-086] (I-11) In a header whose seals were written by an honest node's decision, `CommittedSeal` and `PreparedSeal` MUST each have exactly `Q(V(h))` sealers, and those sealers MUST be validators that sent a COMMIT (resp. PREPARE) for `(h, Round(h))` with `digest == block_hash(h)`. A node that imports block `h` from another node stores that node's header, so two honest nodes can hold different sealer sets and different `Round(h)` for the same block hash (WBFT-SM-048). `A-08` specifies the verification rule (at least `Q`).
Source: consensus/wbft/core/commit.go:137-173
Observable: header, network

[WBFT-SM-087] (I-12, corrected) All ROUND-CHANGEs an honest node sends for one view `(h, r)` MUST carry the same `(prepared_round, prepared_digest)`, except that a node that decided in round `r` and whose `finalize` failed MAY later send ROUND-CHANGE `(h, r)` with `prepared_round == r` from its retry timer (WBFT-SM-049, WBFT-SM-052); a ROUND-CHANGE sent in the late-timeout `CATCH_UP` case (WBFT-SM-080) carries no prepared pair even if the node later sends a ROUND-CHANGE for the same view with one (after locking in an earlier round of the new sequence); the bad-block rule (WBFT-SM-024) MAY remove the pair between two ROUND-CHANGEs for the same view; and except the races of WBFT-SM-081.
Source: consensus/wbft/core/roundchange.go:52-97, consensus/wbft/core/core.go:426-450, consensus/wbft/core/core.go:185-195, consensus/wbft/core/core.go:278-286
Observable: network

[WBFT-SM-088] (I-13) A ROUND-CHANGE sent by an honest node that carries a prepared pair MUST carry `prepared_block` with `block_hash(prepared_block) == prepared_digest` and `prepared_block.number == sequence`, and a `justification` of exactly `Q` PREPAREs from distinct sources with round `prepared_round` and digest `prepared_digest`. A ROUND-CHANGE without a prepared pair MAY still carry a non-empty justification (WBFT-SM-053).
Source: consensus/wbft/core/prepare.go:125-137, consensus/wbft/core/roundchange.go:63-82
Observable: network

[WBFT-SM-089] (I-15, corrected) An honest node MUST send its first ROUND-CHANGE with target round `r + 1` for sequence `h` only in one of these situations: its round timer for `(h, r)` expired (at least `round_timeout(r)` after the later of entering round `r` and accepting a PRE-PREPARE in it, `A-06`); the F+1 rule fired; its `finalize` for `(h, r)` failed; a late round timeout of the previous sequence took the `CATCH_UP` branch (WBFT-SM-080), in which case `r + 1` is one more than the round it had in sequence `h - 1`; or a retry timeout armed in sequence `h - 1` was processed after the node entered sequence `h` (WBFT-SM-081), in which case the target is the round the node had in sequence `h - 1` when that retry timer was armed; or a round-change or retry timer armed by the previous run of the consensus core expired after a restart (`A-06` WBFT-TIMER-018), in which case the target comes from that timer (the new core's current round plus one for a round-change timer, the remembered round for a retry timer) and the timing bound of the first situation does not apply.
Source: consensus/wbft/core/handler.go:250-263, consensus/wbft/core/roundchange.go:161-169, consensus/wbft/core/commit.go:173-176, consensus/wbft/core/handler.go:170-178
Observable: network, log

[WBFT-SM-090] (I-23, corrected) A ROUND-CHANGE sent by an honest node MUST have `prepared_round < round`, except that a ROUND-CHANGE sent from the retry timer MAY have `prepared_round == round`: after a failed `finalize` (WBFT-SM-049), or after a stale retry timeout re-armed the retry timer in the current round (WBFT-SM-081) and the node then reached `Prepared` in that round. Receivers do not check this property.
Source: consensus/wbft/core/roundchange.go:52-63, consensus/wbft/core/roundchange.go:117-151, consensus/wbft/core/handler.go:170-178, consensus/wbft/core/core.go:336-347
Observable: network

[WBFT-SM-091] (I-24) An honest node MAY send ROUND-CHANGE `(h, r + 1)` after it decided `(h, r)` and before it processes the `NewHead` of `h` (WBFT-SM-050). Observers MUST NOT count this as a fault; its frequency measures block import time against `round_timeout(r)`, or other validators' earlier timeouts through the F+1 rule.
Source: consensus/wbft/core/commit.go:137-179, consensus/wbft/core/handler.go:152-160
Observable: network, log

I-14 of the v0.1 draft ("a height without timeouts decides in round 0 within `round_timeout(0)`") is an operational expectation, not a protocol property; it is not restated as a requirement.
