# B-05 Governance semantics

- Status: draft
- Area code: `GOV`
- Reference: go-stablenet `740526d03`. All `Source:` paths are relative to the repository root.

This chapter defines, operation by operation, what the StableNet governance system contracts do to their state.

The chapter covers `GovBase` (the shared member/proposal engine), `GovValidator`, `GovCouncil`, the `blsPoP` precompile and the `AccountManager` native manager, and bounds the minting contracts (`GovMasterMinter`, `GovMinter` v1/v2, `NativeCoinAdapter`) to a summary. Storage slot layouts are in `B-04`; the genesis initialisation of these contracts is in `B-02`; how the consensus engine binds contract state to candidates, BLS keys and proposer eligibility is in `B-08`; gas-tip verification in the header is in `B-06`; transaction-level blacklist and authorized-account rules are in `B-07`.

---

## 1. Scope, conformance and execution model

### Normative content

The normative content of this chapter is the effect of each governance operation on observable state:

- contract storage (members, member versions, quorum, proposals, vote bitmaps, attempt counters, validator mappings, BLS keys, gas tip, blacklist and authorized lists),
- account `Extra` bits written through `AccountManager`,
- the logs emitted, their arguments and their order within a call,
- whether the call reverts, and the custom error it reverts with (visible through `eth_call`, `debug_trace*`; receipts expose only the status).

Gas amounts are not normative. Where the outcome depends on the amount of gas forwarded to a sub-call, this is called out (§13).

### Execution model used by the pseudocode

The pseudocode models each contract as a mutable object. `ctx.sender` is `msg.sender` of the current call frame and `ctx.timestamp` is `block.timestamp`, which equals `header.Time` of the block that contains the transaction. `revert(E)` aborts the current call frame: every storage write, `Extra`-bit write and log produced in that frame and its sub-frames is discarded, and the caller observes a failed call with error `E`. `emit X(...)` appends a log to the current frame. `require_*` helpers revert with the named error.

[SNET-GOV-001] Each governance contract (`GovValidator`, `GovCouncil`, `GovMinter`, `GovMasterMinter`) MUST be treated as an independent `GovBase` instance with its own member set, member versions, quorum, proposal counter, proposals, attempt counters and active-proposal counters. A member of one instance has no rights in another instance.
Source: systemcontracts/solidity/abstracts/GovBase.sol:131-150
Source: systemcontracts/solidity/v1/GovValidator.sol:23
Source: systemcontracts/solidity/v1/GovCouncil.sol:58
Source: systemcontracts/solidity/v1/GovMinter.sol:52-55
Source: params/config_wbft.go:31-45
Observable: state

[SNET-GOV-002] A governance operation takes effect if and only if the call frame that performs it, and every enclosing frame up to the transaction, completes without revert. Members MAY be externally owned accounts or contracts; the acting identity is always `msg.sender` of the call into the governance contract, which can be an internal call made by another contract. A mirror of governance state MUST therefore be driven at call-frame granularity (or from state), not from top-level transaction fields.
Source: systemcontracts/solidity/abstracts/GovBase.sol:24
Source: systemcontracts/solidity/abstracts/GovBase.sol:182-186
Observable: state

[SNET-GOV-003] Every time comparison in governance MUST use `block.timestamp`; there is no block-number-based expiry. A proposal created at time `c` MUST be treated as *expired* at time `t` if and only if `t > c + proposalExpiry` (strict); at `t == c + proposalExpiry` it is still live.
Source: systemcontracts/solidity/abstracts/GovBase.sol:334
Source: systemcontracts/solidity/abstracts/GovBase.sol:757
Source: systemcontracts/solidity/abstracts/GovBase.sol:863
Observable: state

```python
def is_expired(g, p, now) -> bool:
    return now > p.created_at + g.proposal_expiry   # uint256, cannot overflow for genesis-sized values
```

---

## 2. Types, constants and state

### Constants

| Name | Value | Source |
|---|---|---|
| `MAX_MEMBER_INDEX` | 255 | `GovBase.sol:118` |
| `MAX_RETRY_COUNT` | 3 (total execution attempts, not retries) | `GovBase.sol:119` |
| `INITIAL_MEMBER_VERSION` | 1 (declared initialiser; not executed for genesis-injected code, see §12) | `GovBase.sol:120, 132` |
| `BLS_PUBLIC_KEY_LENGTH` / `BLS_SIGNATURE_LENGTH` | 48 / 96 | `GovValidator.sol:26-27` |

[SNET-GOV-004] Action types are the Keccak-256 hash of the ASCII strings below. The strings are not uniform (only the `GovBase` actions carry the `ACTION_` prefix); an implementation MUST use exactly these preimages because the action type is stored in the proposal and emitted in `ProposalCreated`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:123-127
Source: systemcontracts/solidity/v1/GovValidator.sol:50
Source: systemcontracts/solidity/v1/GovCouncil.sol:71-80
Observable: state

| Contract | Action | Preimage | `callData` (ABI encoding) |
|---|---|---|---|
| all | add member | `"ACTION_ADD_MEMBER"` | `(address newMember, uint32 newQuorum)` |
| all | remove member | `"ACTION_REMOVE_MEMBER"` | `(address member, uint32 newQuorum)` |
| all | change member | `"ACTION_CHANGE_MEMBER"` | constant exists but no proposal uses it (`changeMember` is direct, §6) |
| all | change quorum | `"ACTION_CHANGE_QUORUM"` | `(uint32 newQuorum)` |
| all | change max proposals | `"ACTION_CHANGE_MAX_PROPOSALS"` | `(uint256 newMax)` |
| GovValidator | set gas tip | `"SET_GAS_TIP"` | `(uint256 newTip)` |
| GovCouncil | blacklist add / remove | `"ADD_BLACKLIST"` / `"REMOVE_BLACKLIST"` | `(address)` |
| GovCouncil | blacklist batch add / remove | `"ADD_BLACKLIST_BATCH"` / `"REMOVE_BLACKLIST_BATCH"` | `(address[])` |
| GovCouncil | authorized add / remove | `"ADD_AUTHORIZED_ACCOUNT"` / `"REMOVE_AUTHORIZED_ACCOUNT"` | `(address)` |
| GovCouncil | authorized batch add / remove | `"ADD_AUTHORIZED_ACCOUNT_BATCH"` / `"REMOVE_AUTHORIZED_ACCOUNT_BATCH"` | `(address[])` |

### Proposal status

[SNET-GOV-005] `ProposalStatus` MUST be encoded as an 8-bit enum with values `None = 0`, `Voting = 1`, `Approved = 2`, `Executed = 3`, `Cancelled = 4`, `Expired = 5`, `Failed = 6`, `Rejected = 7`. `Voting` and `Approved` are *live*; `Executed`, `Cancelled`, `Expired`, `Failed`, `Rejected` are *terminal*, and a terminal proposal MUST NOT be changed by any operation.
Source: systemcontracts/solidity/abstracts/GovBase.sol:96-105
Source: systemcontracts/gov_base.go:94-103
Observable: state, rpc

### GovBase state

```python
class Member:
    is_active: bool
    joined_at: uint32            # uint32(block.timestamp) at activation; 0 for genesis members

class GovProposal:                 # governance proposal; not A-01's `Proposal` (a block)
    action_type: bytes32
    member_version: uint256      # snapshot of member_version at creation
    voted_bitmap: uint256        # bit i set <=> member at index i of the snapshot has voted (either way)
    created_at: uint256
    executed_at: uint256         # set only on Executed
    proposer: Address
    required_approvals: uint32   # snapshot of quorum at creation
    approved: uint32
    rejected: uint32
    status: ProposalStatus
    call_data: bytes

class GovBaseState:
    proposal_expiry: uint256                           # no operation changes it after genesis
    member_version: uint256
    current_proposal_id: uint256
    reentrancy_guard: uint256                          # 0 or 1
    quorum: uint32
    members: dict[Address, Member]                     # default Member(False, 0)
    versioned_member_list: dict[uint256, list[Address]]
    member_index_by_version: dict[uint256, dict[Address, uint32]]   # index + 1; 0 = absent
    quorum_by_version: dict[uint256, uint32]
    proposals: dict[uint256, GovProposal]
    proposal_execution_count: dict[uint256, uint256]
    member_active_proposal_count: dict[Address, uint256]
    max_active_proposals_per_member: uint256
```

Source: systemcontracts/solidity/abstracts/GovBase.sol:57-94, 131-150. The slot positions of these fields are defined in `B-04`.

---

## 3. Member set and versions

The member set is kept as a sequence of versioned snapshots. Adding or removing a member creates a new version; changing a member's address rewrites the current version in place. Proposals capture the version at creation and use that snapshot for voting rights for their whole life.

[SNET-GOV-010] A member index at version `v` MUST be stored as `index + 1` in `member_index_by_version[v][addr]`; `0` means "not a member at `v`". `index_at(addr, v)` MUST return `member_index_by_version[v][addr] - 1`, or `2**256 - 1` if the stored value is 0.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1297-1303
Observable: state

[SNET-GOV-011] After every successful governance operation, the following MUST hold for the current version `V = member_version`: `members[a].is_active` is true if and only if `a` occurs in `versioned_member_list[V]`; each address occurs at most once in that list; and `member_index_by_version[V][list[i]] == i + 1` for every `i`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:933-978
Source: systemcontracts/solidity/abstracts/GovBase.sol:1049-1095
Source: systemcontracts/solidity/abstracts/GovBase.sol:448-494
Observable: state

[SNET-GOV-012] The snapshot `versioned_member_list[v]` for `v < member_version` MUST NOT change. The snapshot of the current version MUST be modified in place by `changeMember` (§6), which means a proposal created at the current version sees the replaced address, not the original one, until the next add or remove creates a new version.
Source: systemcontracts/solidity/abstracts/GovBase.sol:456-476
Source: systemcontracts/solidity/abstracts/GovBase.sol:909-915
Observable: state

[SNET-GOV-013] The number of members in any version MUST NOT exceed 255 (`MAX_MEMBER_INDEX`). Adding a member when the current version already has 255 members reverts with `MemberIndexOverflow`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:366-367
Source: systemcontracts/solidity/abstracts/GovBase.sol:940-942
Observable: state

```python
def require_proposal_member(g, sender, pid):
    p = g.proposals[require_valid_proposal_id(g, pid)]
    i = index_at(g, sender, p.member_version)
    snap = g.versioned_member_list[p.member_version]
    if i >= len(snap) or snap[i] != sender:
        revert("NotAMember")

def require_active_member(g, sender):
    if not g.members[sender].is_active:
        revert("NotAMember")

def require_valid_proposal_id(g, pid) -> int:
    if pid == 0 or pid > g.current_proposal_id:
        revert("InvalidProposal")
    return pid
```

---

## 4. Quorum

[SNET-GOV-020] A quorum value `q` MUST be treated as valid for a member count `n` if and only if (`n == 1` and `q == 1`) or (`n >= 2` and `2 <= q <= n`). A member count of 0 is never valid for a removal (the last member cannot be removed).
Source: systemcontracts/solidity/abstracts/GovBase.sol:369-374
Source: systemcontracts/solidity/abstracts/GovBase.sol:388-396
Source: systemcontracts/solidity/abstracts/GovBase.sol:945-949
Source: systemcontracts/solidity/abstracts/GovBase.sol:1059-1064
Observable: state

```python
def valid_quorum(q: int, n: int) -> bool:
    if n == 1:
        return q == 1
    return n >= 2 and 2 <= q <= n
```

[SNET-GOV-022] `quorum_by_version[v]` MUST be written with the new quorum at the end of every add and remove (for the new version) and at every quorum change (for the current version). It is not consulted by proposal logic; proposals use the `required_approvals` snapshot taken from `quorum` at creation, and later quorum changes do not affect existing proposals.
Source: systemcontracts/solidity/abstracts/GovBase.sol:704
Source: systemcontracts/solidity/abstracts/GovBase.sol:977
Source: systemcontracts/solidity/abstracts/GovBase.sol:1008
Source: systemcontracts/solidity/abstracts/GovBase.sol:1094
Observable: state

[SNET-GOV-023] The rejection threshold of proposal `p` MUST be `max_rejections = len(versioned_member_list[p.member_version]) - p.required_approvals` (uint32 arithmetic). A proposal MUST become `Rejected` when `rejected > max_rejections`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:818-824
Observable: state

---

## 5. Proposal lifecycle

### State machine

The table lists every transition. "attempt" means one call of `execute_proposal` (SNET-GOV-050..057). Transitions not listed do not exist; in particular nothing leaves a terminal status, and `Approved` never goes back to `Voting`.

| From | Trigger | Condition (checked in this order) | To | Logs (in order) |
|---|---|---|---|---|
| — | `propose*` | preconditions of SNET-GOV-030/031 hold | `Voting` | `ProposalCreated`, then the proposer's vote |
| `Voting`/`Approved` | approve | expired | `Expired` | `ProposalExpired` |
| `Voting` | approve | `approved + 1 < required` | `Voting` | `ProposalVoted(true)` |
| `Voting` | approve | `approved + 1 >= required` | `Approved`, then attempt | `ProposalVoted(true)`, `ProposalApproved`, attempt logs |
| `Approved` | approve | not expired | `Approved`, then attempt | `ProposalVoted(true)`, attempt logs |
| `Voting`/`Approved` | disapprove | expired | `Expired` | `ProposalExpired` |
| `Voting` | disapprove | `rejected + 1 > max_rejections` | `Rejected` | `ProposalVoted(false)`, `ProposalRejected` |
| `Voting`/`Approved` | disapprove | otherwise | unchanged | `ProposalVoted(false)` |
| `Approved` | attempt | expired | `Expired` | `ProposalExpired` |
| `Approved` | attempt | `execution_count >= 3` | `Failed` | `ProposalFailed("Max retry count reached")` |
| `Approved` | attempt | action succeeds | `Executed` | action logs, `ProposalExecuted(true)` |
| `Approved` | attempt, retry mode | action returns false | `Approved` | action logs, `ProposalExecuted(false)` |
| `Approved` | attempt, terminal mode | action returns false | `Failed` | action logs, `ProposalExecuted(false)` |
| any live | attempt / vote | action reverts | whole call reverts, no change | none |
| `Voting` | `cancelProposal` | sender is proposer, `approved <= 1`, `rejected == 0` | `Cancelled` | `ProposalCancelled` |
| `Voting`/`Approved` | `expireProposal` | expired | `Expired` | `ProposalExpired` |

Every transition into a terminal status goes through `finalize_proposal` (SNET-GOV-062), which runs before the terminal log is emitted.

### Creation

[SNET-GOV-030] Every public `propose*` function MUST first check that `ctx.sender` is an active member (`NotAMember`), then its function-specific argument checks (in the order given for each function in §6, §8, §10), then call `create_proposal`, which checks in order: active member again, `proposal_expiry != 0` (`InvalidProposalExpiry`), `member_active_proposal_count[sender] < max_active_proposals_per_member` (`TooManyActiveProposals`). The first failing check determines the revert error.
Source: systemcontracts/solidity/abstracts/GovBase.sol:686-693
Observable: rpc

[SNET-GOV-031] `create_proposal` MUST allocate `pid = current_proposal_id + 1`, store the proposal with `status = Voting`, `member_version = member_version`, `required_approvals = quorum`, `created_at = ctx.timestamp`, zero counters and bitmap, emit `ProposalCreated(pid, sender, action_type, member_version, quorum, call_data)`, increment `member_active_proposal_count[sender]`, and then cast the proposer's approval with auto-execution enabled (SNET-GOV-040). Proposal ids start at 1 and are never reused.
Source: systemcontracts/solidity/abstracts/GovBase.sol:695-719
Observable: state

```python
def create_proposal(c, ctx, action_type: bytes32, call_data: bytes) -> int:
    g = c.base
    require_active_member(g, ctx.sender)
    if g.proposal_expiry == 0:
        revert("InvalidProposalExpiry")
    if g.member_active_proposal_count[ctx.sender] >= g.max_active_proposals_per_member:
        revert("TooManyActiveProposals")
    g.current_proposal_id += 1
    pid = g.current_proposal_id
    g.proposals[pid] = GovProposal(action_type=action_type, member_version=g.member_version,
                                voted_bitmap=0, created_at=ctx.timestamp, executed_at=0,
                                proposer=ctx.sender, required_approvals=g.quorum,
                                approved=0, rejected=0, status=VOTING, call_data=call_data)
    emit("ProposalCreated", pid, ctx.sender, action_type, g.member_version, g.quorum, call_data)
    g.member_active_proposal_count[ctx.sender] += 1
    vote(c, ctx, pid, approve=True, auto_execute=True)
    return pid
```

[SNET-GOV-032] Because the proposer's vote auto-executes, a proposal whose `required_approvals <= 1` MUST be executed inside the creating call. If that execution reverts, the creation reverts as a whole and no proposal id is consumed.
Source: systemcontracts/solidity/abstracts/GovBase.sol:717-718
Source: systemcontracts/solidity/abstracts/GovBase.sol:797-808
Observable: state

### Voting

[SNET-GOV-040] `approveProposal(pid)` and `disapproveProposal(pid)` MUST check `require_proposal_member` (which includes `InvalidProposal` for `pid == 0` or `pid > current_proposal_id`) and then run `vote` with `auto_execute = True` for approval and `False` for disapproval. `vote` checks, in order: valid id (`InvalidProposal`), status live (`ProposalNotInVoting`), expiry (SNET-GOV-041), snapshot membership (`NotAMember`), duplicate vote (`AlreadyApproved`, raised for a second vote in either direction).
Source: systemcontracts/solidity/abstracts/GovBase.sol:262-271
Source: systemcontracts/solidity/abstracts/GovBase.sol:743-784
Observable: state, rpc

[SNET-GOV-041] If the proposal is expired when a vote is cast, `vote` MUST finalize it as `Expired`, emit `ProposalExpired(pid, sender)` and return without recording the vote; the call does not revert.
Source: systemcontracts/solidity/abstracts/GovBase.sol:757-768
Observable: state

[SNET-GOV-042] An approval MUST set the voter's bit, increment `approved`, and emit `ProposalVoted(pid, sender, true, approved, rejected)`. If `approved >= required_approvals`, then: if the status is not yet `Approved`, the status MUST become `Approved` and `ProposalApproved(pid, sender, approved, rejected)` MUST be emitted; and if `auto_execute`, an execution attempt in retry mode MUST follow in the same call.
Source: systemcontracts/solidity/abstracts/GovBase.sol:787-809
Observable: state

[SNET-GOV-043] An approval on a proposal that is already `Approved` (possible after a failed attempt left it `Approved`) MUST record the vote and trigger another execution attempt; `ProposalApproved` is not emitted again.
Source: systemcontracts/solidity/abstracts/GovBase.sol:798-808
Observable: state

[SNET-GOV-044] A disapproval MUST set the voter's bit, increment `rejected`, and emit `ProposalVoted(pid, sender, false, approved, rejected)`. If `rejected > max_rejections` (SNET-GOV-023), the proposal MUST be finalized as `Rejected` and `ProposalRejected(pid, sender, approved, rejected)` emitted. A disapproval never triggers execution.
Source: systemcontracts/solidity/abstracts/GovBase.sol:810-825
Observable: state

[SNET-GOV-045] (Derived property.) An `Approved` proposal cannot become `Rejected`: each snapshot member votes at most once, so `approved >= required` implies `rejected <= n - required`. An implementation MUST NOT add a rejection path for `Approved` proposals.
Source: systemcontracts/solidity/abstracts/GovBase.sol:784-787
Source: systemcontracts/solidity/abstracts/GovBase.sol:820-821
Observable: state

```python
def vote(c, ctx, pid, approve: bool, auto_execute: bool):
    g = c.base
    if pid == 0 or pid > g.current_proposal_id:
        revert("InvalidProposal")
    p = g.proposals[pid]
    if p.status not in (VOTING, APPROVED):
        revert("ProposalNotInVoting")
    if is_expired(g, p, ctx.timestamp):
        finalize_proposal(c, pid, EXPIRED)
        emit("ProposalExpired", pid, ctx.sender)
        return
    snap = g.versioned_member_list[p.member_version]
    i = index_at(g, ctx.sender, p.member_version)
    if i >= len(snap):
        revert("NotAMember")
    bit = 1 << i
    if p.voted_bitmap & bit:
        revert("AlreadyApproved")
    p.voted_bitmap |= bit
    if approve:
        p.approved += 1
        emit("ProposalVoted", pid, ctx.sender, True, p.approved, p.rejected)
        if p.approved >= p.required_approvals:
            if p.status != APPROVED:
                p.status = APPROVED
                emit("ProposalApproved", pid, ctx.sender, p.approved, p.rejected)
            if auto_execute:
                execute_proposal(c, ctx, pid, mark_failed_on_error=False)
    else:
        p.rejected += 1
        emit("ProposalVoted", pid, ctx.sender, False, p.approved, p.rejected)
        if p.rejected > len(snap) - p.required_approvals:
            finalize_proposal(c, pid, REJECTED)
            emit("ProposalRejected", pid, ctx.sender, p.approved, p.rejected)
```

### Execution

[SNET-GOV-050] `executeProposal(pid)` (retry mode) and `executeWithFailure(pid)` (terminal mode) MUST check `require_proposal_member`, then the reentrancy guard (SNET-GOV-058), then: valid id (`InvalidProposal`), `status == Approved` (`ProposalNotExecutable`), `approved >= required_approvals` (`InsufficientApprovals`). A `Voting` proposal cannot be executed.
Source: systemcontracts/solidity/abstracts/GovBase.sol:279-289
Source: systemcontracts/solidity/abstracts/GovBase.sol:851-858
Observable: rpc

[SNET-GOV-051] If the proposal is expired, the attempt MUST finalize it as `Expired`, emit `ProposalExpired(pid, sender)` and return false without reverting and without incrementing the attempt counter.
Source: systemcontracts/solidity/abstracts/GovBase.sol:863-867
Observable: state

[SNET-GOV-052] If `proposal_execution_count[pid] >= 3`, the attempt MUST finalize the proposal as `Failed`, emit `ProposalFailed(pid, sender, "Max retry count reached")` (the reason is the raw ASCII bytes) and return false without running the action. This applies in both modes.
Source: systemcontracts/solidity/abstracts/GovBase.sol:870-874
Observable: state

[SNET-GOV-053] Otherwise the attempt MUST increment `proposal_execution_count[pid]` and run the action (SNET-GOV-074). The counter is part of the call's state, so an attempt whose action reverts leaves the counter unchanged; only attempts whose action *returns* (true or false) are counted.
Source: systemcontracts/solidity/abstracts/GovBase.sol:876-880
Observable: state

[SNET-GOV-054] If the action returns true, the proposal MUST be finalized as `Executed` (setting `executed_at = ctx.timestamp`) and `ProposalExecuted(pid, sender, true)` emitted after the action's own logs.
Source: systemcontracts/solidity/abstracts/GovBase.sol:883-885
Observable: state

[SNET-GOV-055] If the action returns false in retry mode, the status MUST remain `Approved` and `ProposalExecuted(pid, sender, false)` MUST be emitted. The proposal can be attempted again by `executeProposal`, `executeWithFailure`, or any further approval.
Source: systemcontracts/solidity/abstracts/GovBase.sol:886-895
Observable: state

[SNET-GOV-058] `execute_proposal` is the only function protected by the reentrancy guard. On entry it MUST revert with `ReentrantCall` if `reentrancy_guard == 1`, otherwise set it to 1; on normal exit it MUST reset it to 0. A nested attempt inside an action (for example an action that calls back into the same contract's `executeProposal` or `approveProposal` of a quorum-reaching proposal) therefore reverts. Voting, proposing, cancelling, `changeMember` and `configureValidator` are not guarded.
Source: systemcontracts/solidity/abstracts/GovBase.sol:195-199
Source: systemcontracts/solidity/abstracts/GovBase.sol:241-250
Source: systemcontracts/solidity/abstracts/GovBase.sol:851
Observable: state

```python
def execute_proposal(c, ctx, pid, mark_failed_on_error: bool) -> bool:
    g = c.base
    if g.reentrancy_guard == 1:
        revert("ReentrantCall")
    g.reentrancy_guard = 1
    try:
        if pid == 0 or pid > g.current_proposal_id:
            revert("InvalidProposal")
        p = g.proposals[pid]
        if p.status != APPROVED:
            revert("ProposalNotExecutable")
        if p.approved < p.required_approvals:
            revert("InsufficientApprovals")
        if is_expired(g, p, ctx.timestamp):
            finalize_proposal(c, pid, EXPIRED)
            emit("ProposalExpired", pid, ctx.sender)
            return False
        if g.proposal_execution_count[pid] >= MAX_RETRY_COUNT:
            finalize_proposal(c, pid, FAILED)
            emit("ProposalFailed", pid, ctx.sender, b"Max retry count reached")
            return False
        g.proposal_execution_count[pid] += 1
        ok = execute_internal_action(c, ctx, p.action_type, p.call_data)   # may revert
        if ok:
            finalize_proposal(c, pid, EXECUTED)
            emit("ProposalExecuted", pid, ctx.sender, True)
        else:
            if mark_failed_on_error:
                finalize_proposal(c, pid, FAILED)
            else:
                p.status = APPROVED
            emit("ProposalExecuted", pid, ctx.sender, False)
        return ok
    finally:
        g.reentrancy_guard = 0     # on revert the whole frame is discarded anyway
```

### Cancellation, manual expiry, finalization

[SNET-GOV-061] `expireProposal(pid)` MUST check `require_proposal_member`; it MUST return false without state change if the status is not live or the proposal is not expired; otherwise it MUST finalize as `Expired`, emit `ProposalExpired(pid, sender)` and return true. Non-members cannot call it.
Source: systemcontracts/solidity/abstracts/GovBase.sol:323-342
Observable: state

[SNET-GOV-062] `finalize_proposal(pid, s)` (Solidity `_finalizeProposal`) MUST, in this order: set `status = s`; if `s == Executed`, set `executed_at = ctx.timestamp`; decrement `member_active_proposal_count[proposer]` if it is greater than 0 (saturating at 0); call the derived hook `on_proposal_finalized(pid)` (a no-op except in `GovMinter`, §11). The caller emits the terminal log afterwards, so logs emitted by the hook precede the terminal log.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1247-1261
Source: systemcontracts/solidity/abstracts/GovBase.sol:1309-1314
Observable: state

---

## 6. Member management and action dispatch

### Proposal functions (available on every GovBase instance)

[SNET-GOV-070] `proposeAddMember(newMember, newQuorum)` MUST check in order: active member; `newMember != 0` (`InvalidMemberAddress`); `!members[newMember].is_active` (`AlreadyAMember`); current member count `< 255` (`MemberIndexOverflow`); `valid_quorum(newQuorum, count + 1)` (`InvalidQuorum`); then create a proposal with `ACTION_ADD_MEMBER`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:362-378
Observable: state, rpc

[SNET-GOV-071] `proposeRemoveMember(member, newQuorum)` MUST check in order: active member; `members[member].is_active` (`NotAMember`); current count `> 1` (`InvalidQuorum`); `valid_quorum(newQuorum, count - 1)` (`InvalidQuorum`); then create a proposal with `ACTION_REMOVE_MEMBER`. A member MAY propose its own removal.
Source: systemcontracts/solidity/abstracts/GovBase.sol:385-400
Observable: state, rpc

[SNET-GOV-072] `proposeChangeQuorum(newQuorum)` MUST check active member and `valid_quorum(newQuorum, count)` (`InvalidQuorum`), then create a proposal with `ACTION_CHANGE_QUORUM`. Proposing the current quorum value is allowed.
Source: systemcontracts/solidity/abstracts/GovBase.sol:407-419
Observable: state, rpc

[SNET-GOV-073] `proposeChangeMaxProposals(newMax)` MUST check active member and `1 <= newMax <= 50` (`InvalidMaxProposals`), then create a proposal with `ACTION_CHANGE_MAX_PROPOSALS`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:426-432
Observable: state, rpc

[SNET-GOV-074] The action dispatcher MUST handle the four `GovBase` actions itself (each returns true or reverts) and pass every other action type to the derived contract's custom-action handler; an action type unknown to the derived contract returns false (a soft failure).
Source: systemcontracts/solidity/abstracts/GovBase.sol:1109-1130
Source: systemcontracts/solidity/abstracts/GovBase.sol:1160-1163
Observable: state

### Execution of member actions

[SNET-GOV-075] Executing `ACTION_ADD_MEMBER(a, q)` MUST, in order: revert `AlreadyAMember` if `a` is active; revert `InvalidMemberAddress` if `a == 0`; increment `member_version` to `V' = V + 1`; revert `MemberIndexOverflow` if the old list has 255 entries; revert `InvalidQuorum` unless `valid_quorum(q, old_count + 1)`; copy the old list in order into version `V'` with indices `1..n`; append `a` with index `n + 1`; set `members[a] = Member(True, uint32(ctx.timestamp))`; set `quorum = q`; emit `MemberAdded(a, n + 1, q)` then `QuorumUpdated(old_quorum, q)` (emitted even if unchanged); call the derived hook `on_member_added(a)`; set `quorum_by_version[V'] = q`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:933-978
Observable: state

[SNET-GOV-076] Executing `ACTION_REMOVE_MEMBER(a, q)` MUST, in order: revert `NotAMember` if `a` is not active; increment `member_version` to `V'`; revert `InvalidQuorum` if the new count is 0 or `!valid_quorum(q, old_count - 1)`; copy the old list into `V'` skipping `a`, preserving the relative order of the others and renumbering indices `1..n-1`; set `members[a].is_active = False` (keeping `joined_at`); set `quorum = q`; emit `MemberRemoved(a, n - 1, q)` then `QuorumUpdated(old_quorum, q)`; call `on_member_removed(a)`; set `quorum_by_version[V'] = q`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1049-1095
Observable: state

[SNET-GOV-077] Executing `ACTION_CHANGE_QUORUM(q)` MUST revert `InvalidQuorum` unless `valid_quorum(q, len(versioned_member_list[member_version]))`; otherwise set `quorum = q`, emit `QuorumUpdated(old, q)`, and set `quorum_by_version[member_version] = q`. The member version does not change.
Source: systemcontracts/solidity/abstracts/GovBase.sol:991-1009
Observable: state

[SNET-GOV-078] Executing `ACTION_CHANGE_MAX_PROPOSALS(m)` MUST revert `InvalidMaxProposals` unless `1 <= m <= 50`; otherwise set `max_active_proposals_per_member = m` and emit `MaxProposalsPerMemberUpdated(old, m)`. Lowering the maximum does not affect existing proposals; it only blocks new ones while a member's count is at or above the new maximum.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1022-1031
Source: systemcontracts/solidity/abstracts/GovBase.sol:691-693
Observable: state

### Direct address change

[SNET-GOV-080] Hooks MUST be invoked at these points and nowhere else: `on_member_added` after the add's logs and before `quorum_by_version` is written; `on_member_removed` after the remove's logs and before `quorum_by_version` is written; `on_member_changed` after `MemberChanged`; `on_proposal_finalized` inside `finalize_proposal`. Hook effects on derived state are part of the same atomic call.
Source: systemcontracts/solidity/abstracts/GovBase.sol:974-977
Source: systemcontracts/solidity/abstracts/GovBase.sol:1091-1094
Source: systemcontracts/solidity/abstracts/GovBase.sol:489-493
Source: systemcontracts/solidity/abstracts/GovBase.sol:1259-1260
Observable: state

---

## 7. GovBase views

[SNET-GOV-090] The read-only functions MUST return the values below. They are observable through `eth_call` and are the recommended cross-check interface for a mirror.
Source: systemcontracts/solidity/abstracts/GovBase.sol:504-657
Observable: rpc

| Function | Result |
|---|---|
| `getProposal(pid)` | the stored proposal; reverts `InvalidProposal` for invalid `pid` |
| `isProposalInVoting(pid)` | `status == Voting and not expired` (Approved returns false); reverts `InvalidProposal` for invalid `pid` |
| `isProposalExecutable(pid)` | `status == Approved and not expired and approved >= required`; ignores the attempt counter; reverts `InvalidProposal` for invalid `pid` |
| `canExecuteProposal(pid)` | `(InvalidProposalId, 0)` / `(NotApproved, 0)` / `(Expired, 0)` / `(TooManyAttempts, 0)` / `(Executable, 3 - count)`, checked in that order; never reverts |
| `hasApproved(member, pid)` | whether `member`'s bit in the snapshot is set, for approval **or** rejection; reverts `InvalidProposal` for invalid `pid` |
| `getMemberCount(v)`, `getMemberAt(v, i)`, `isMember(a, v)` | snapshot queries; revert `InvalidMemberVersion` unless `1 <= v <= member_version`; `getMemberAt` reverts `IndexOutOfBounds` |
| `getQuorum(v)` | `quorum_by_version[v]`; reverts `InvalidQuorum` if it is 0 |
| `getMemberActiveProposalCount(a)`, `canCreateProposal(a)` | the counter; `counter < max_active_proposals_per_member` |
| public getters | `proposalExpiry`, `memberVersion`, `currentProposalId`, `quorum`, `members(a)`, `versionedMemberList(v, i)`, `proposals(pid)` (all eleven fields in declaration order, including `callData` as `bytes`; no `pid` validation), `proposalExecutionCount(pid)`, `memberActiveProposalCount(a)`, `maxActiveProposalsPerMember` |

---

## 8. GovValidator

`GovValidator` holds the validator candidates, their BLS public keys and the gas tip. Each active member (called *operator* here) may register at most one validator address and its BLS key directly, without a proposal. Validators leave only when their operator is removed or replaces them.

### State

```python
class GovValidatorState:
    base: GovBaseState
    bls_pop: Address                         # genesis: 0x...B00001
    validators: OrderedAddressSet            # OpenZeppelin EnumerableSet.AddressSet
    validator_to_operator: dict[Address, Address]
    operator_to_validator: dict[Address, Address]
    validator_to_bls_key: dict[Address, bytes]
    bls_key_to_validator: dict[bytes, Address]   # keyed by the exact byte string
    gas_tip: uint256
```

Source: systemcontracts/solidity/v1/GovValidator.sol:41-47

[SNET-GOV-100] `validators` MUST behave as the OpenZeppelin `EnumerableSet.AddressSet`: `add(x)` appends `x` at the end if absent (no-op otherwise); `remove(x)` of a present element at position `i` moves the last element into position `i` and shortens the list by one (swap-and-pop); `values()` returns the list in storage order. The storage order is consensus-relevant because the node reads the list in this order as the candidate order (SNET-GOV-115).
Source: systemcontracts/solidity/v1/GovValidator.sol:20
Source: systemcontracts/solidity/v1/GovValidator.sol:56-58
Source: systemcontracts/stateutil.go:79-86
Source: systemcontracts/solidity/remappings.txt (`@openzeppelin/contracts/` resolves to the `openzeppelin-contracts` submodule: OpenZeppelin `v4.6.0`, commit `d4fb3a89`; `B-04` section 2.4)
Observable: state, rpc

```python
class OrderedAddressSet:
    values: list[Address]
    pos: dict[Address, int]          # index + 1
    def add(self, x):
        if self.pos.get(x, 0) == 0:
            self.values.append(x); self.pos[x] = len(self.values)
    def remove(self, x):
        k = self.pos.get(x, 0)
        if k == 0: return
        last = self.values[-1]
        self.values[k - 1] = last; self.pos[last] = k
        self.values.pop(); self.pos[x] = 0
```

### configureValidator

[SNET-GOV-101] `configureValidator(newValidator, blsKey, blsSig)` MUST be callable by any active member without a proposal, and MUST check in order: active member (`NotAMember`); `newValidator != 0` (`InvalidValidator`); `validator_to_operator[newValidator]` is 0 or `sender` (`AlreadyValidatorExists`); the BLS checks of SNET-GOV-123; with `old = operator_to_validator[sender]`, `bls_key_to_validator[blsKey]` is 0 or `old` (`AlreadyRegisteredBlsKey`).
Source: systemcontracts/solidity/v1/GovValidator.sol:64-77
Source: systemcontracts/test/gov_validator_test.go:219-311
Observable: state, rpc

[SNET-GOV-102] Case 1 (`old == 0`, register): MUST `validators.add(newValidator)` (appended at the end), set both operator mappings, and set `validator_to_bls_key[newValidator] = blsKey`, `bls_key_to_validator[blsKey] = newValidator`.
Source: systemcontracts/solidity/v1/GovValidator.sol:82-84
Source: systemcontracts/solidity/v1/GovValidator.sol:97-107
Observable: state

[SNET-GOV-103] Case 2 (`old == newValidator`, key change): MUST revert `NoConfigurationChanging` if `bls_key_to_validator[blsKey] == old` (same key); otherwise delete `bls_key_to_validator[validator_to_bls_key[old]]` and set the new key in both directions. The position of `old` in `validators` does not change.
Source: systemcontracts/solidity/v1/GovValidator.sol:85-90
Source: systemcontracts/solidity/v1/GovValidator.sol:109-112
Observable: state

[SNET-GOV-104] Case 3 (`old != 0` and `old != newValidator`, validator replacement, with the same or a new key): MUST first remove `old` completely (`validators.remove(old)` with swap-and-pop, delete `validator_to_operator[old]`, `operator_to_validator[sender]`, `bls_key_to_validator[validator_to_bls_key[old]]`, `validator_to_bls_key[old]`) and then register `newValidator` as in case 1. The replacement therefore moves the operator's validator to the end of the list and moves the previously last validator into the vacated position.
Source: systemcontracts/solidity/v1/GovValidator.sol:91-94
Source: systemcontracts/solidity/v1/GovValidator.sol:114-126
Source: systemcontracts/test/gov_validator_test.go:388-458
Observable: state

[SNET-GOV-107] (withdrawn; informative) Invariants after genesis, derived from the operations of this section (SNET-GOV-101 to SNET-GOV-104, SNET-GOV-109) and SNET-GOV-013: for every operator `o` with `v = operator_to_validator[o] != 0`, `validator_to_operator[v] == o`, `v` is in `validators`, `bls_key_to_validator[validator_to_bls_key[v]] == v`, and `o` is an active member. Consequently `len(validators) <= ` number of active members `<= 255`.
Source: systemcontracts/solidity/v1/GovValidator.sol:97-146
Source: systemcontracts/gov_validator.go:113-167

### Member hooks

[SNET-GOV-109] `on_member_changed(old, new)`: if `operator_to_validator[old] != 0`, MUST set `operator_to_validator[new]` to it, set `validator_to_operator[validator] = new`, and delete `operator_to_validator[old]`. The validator's position, key and membership in `validators` do not change.
Source: systemcontracts/solidity/v1/GovValidator.sol:139-146
Observable: state

[SNET-GOV-110] `on_member_added(m)` MUST have no effect: a new member has no validator until it calls `configureValidator`.
Source: systemcontracts/solidity/v1/GovValidator.sol:135-137
Observable: state

### Gas tip

[SNET-GOV-112] Executing `SET_GAS_TIP(t)` MUST set `gas_tip = t` without re-checking equality and emit `GasTipUpdated(old, t, updater)` where `updater` is `msg.sender` of the call that triggered the attempt (the last approver or the executor), then return true. Two proposals with the same target can both execute.
Source: systemcontracts/solidity/v1/GovValidator.sol:148-155
Source: systemcontracts/solidity/v1/GovValidator.sol:186-190
Observable: state

[SNET-GOV-113] (withdrawn; informative)
Source: systemcontracts/solidity/v1/GovValidator.sol:148-155
Source: systemcontracts/solidity/abstracts/GovBase.sol:1109-1130

### Views and node reads

[SNET-GOV-114] `validatorList()` MUST return `validators.values` in storage order; `isValidator(a)`, `validatorCount()`, the public mappings and `gasTip()` return the stored values; `getGasTipGwei()` returns `gas_tip // 10**9`.
Source: systemcontracts/solidity/v1/GovValidator.sol:52-62
Source: systemcontracts/solidity/v1/GovValidator.sol:192-194
Observable: rpc

[SNET-GOV-115] The node reads `GovValidator` storage directly (not through calls). The values it reads MUST be those defined above: the ordered validator list as the candidate list for the next epoch, the BLS key of each selected validator (an empty key excludes that validator from the epoch), and `gas_tip` from the parent state of each block for the header `GasTip` field. The state at which each read happens is defined in `B-08` and `B-06`; this chapter only fixes that the list order, the key bytes and the tip value are those defined above.
Source: consensus/wbft/engine/engine.go:606-612
Source: consensus/wbft/engine/engine.go:806-810
Source: consensus/wbft/engine/engine.go:890-903
Source: consensus/wbft/engine/engine.go:622-646
Source: consensus/wbft/engine/engine.go:1280-1294
Source: miner/worker.go:1218
Observable: header, state

---

## 9. BLS proof-of-possession precompile

[SNET-GOV-120] While Anzeon rules are active, address `0x0000000000000000000000000000000000B00001` MUST be a precompiled contract (`blsPoP`), present in both the Anzeon and the Boho precompile sets. `GovValidator` calls the address stored in its `blsPoP` field, which genesis sets to this address.
Source: params/protocol_params.go:208
Source: core/vm/contracts.go:127-139
Source: core/vm/contracts.go:141-155
Source: systemcontracts/gov_validator.go:57-62
Observable: state

[SNET-GOV-121] The precompile input MUST be exactly 144 bytes: a 48-byte compressed BLS12-381 G1 public key followed by a 96-byte compressed G2 signature. Any other length MUST make the call fail (the precompile returns an error, so the calling `staticcall` returns `success = false`).
Source: core/vm/contracts.go:1196-1205
Observable: state

[SNET-GOV-123] `GovValidator._checkBlsKey` MUST check in order: key length 48 (`InvalidBlsKeyLength`); signature length 96 (`InvalidSignatureLength`); `staticcall(bls_pop, key ‖ sig)` succeeds (`FailedToVerifyBlsKey`); the returned word decodes to true (`InvalidBlsKey`). A precompile failure (bad encoding, subgroup, infinity) therefore surfaces as `FailedToVerifyBlsKey` and a well-formed but wrong signature as `InvalidBlsKey`.
Source: systemcontracts/solidity/v1/GovValidator.sol:157-172
Source: systemcontracts/test/gov_validator_test.go:256-298
Observable: rpc

[SNET-GOV-124] Uniqueness of BLS keys (`AlreadyRegisteredBlsKey`) is by exact byte string. Two distinct 48-byte strings cannot both pass the precompile for the same group element: decompression accepts exactly one encoding per point (`A-02` WBFT-CRYPTO-056), so uniqueness by byte string is also uniqueness by key. A native module MUST apply the same decoder (A-02) rather than normalising keys itself.
Source: systemcontracts/solidity/v1/GovValidator.sol:46
Source: systemcontracts/solidity/v1/GovValidator.sol:75-77
Source: crypto/bls/blst/public_key.go:44-48
Observable: state

---

## 10. GovCouncil and the AccountManager native manager

`GovCouncil` is the only way, after genesis, to set the blacklist and authorized-account bits of an account. It keeps two address lists in its own storage and mirrors each change into the account `Extra` bits by calling the `AccountManager` native manager. The node and the EVM enforce only the bits (B-07), and the consensus engine rejects a block whose `Coinbase` is blacklisted in the parent state (`B-08` SNET-SRC-020, `A-08` step H15b).

### State and the address-set library

```python
class GovCouncilState:
    base: GovBaseState
    blacklist: StrictAddressSet          # AddressSetLib
    authorized: StrictAddressSet
    account_manager: Address             # genesis: 0x...B00003
```

[SNET-GOV-130] `GovCouncil`'s lists MUST behave as `AddressSetLib.AddressSet`: `add(x)` reverts `ZeroAddressNotAllowed` for 0 and `AddressAlreadyExists` if present, otherwise appends; `remove(x)` reverts `AddressNotFound` if absent, otherwise swap-and-pop; `contains(0)` is always false. Unlike `EnumerableSet`, add and remove of a wrong element revert rather than no-op; `GovCouncil` never reaches those reverts because it checks `contains` first.
Source: systemcontracts/solidity/libraries/AddressSetLib.sol:83-166
Source: systemcontracts/solidity/v1/GovCouncil.sol:64-66
Observable: state

### Proposal functions

[SNET-GOV-131] The proposal functions MUST check, after the active-member check, the conditions below against the current list, and then create a proposal with the corresponding action and call data. Duplicates within a batch are not rejected; an empty batch is accepted.
Source: systemcontracts/solidity/v1/GovCouncil.sol:133-303
Observable: state, rpc

| Function | Checks (in order, per element for batches) | Error |
|---|---|---|
| `proposeAddBlacklist(a)` | `a != 0`; `a` not in blacklist | `ZeroAddressNotAllowed`, `AlreadyInBlacklist` |
| `proposeRemoveBlacklist(a)` | `a` in blacklist | `NotInBlacklist` |
| `proposeAddBlacklistBatch(as)` | for each: `!= 0`, not in blacklist | as above |
| `proposeRemoveBlacklistBatch(as)` | for each: in blacklist | `NotInBlacklist` |
| `proposeAddAuthorizedAccount(a)` / batch | `a != 0`; not in authorized | `ZeroAddressNotAllowed`, `AlreadyInAuthorizedAccountList` |
| `proposeRemoveAuthorizedAccount(a)` / batch | in authorized | `NotInAuthorizedAccountList` |

### Execution

[SNET-GOV-133] Executing a single add/remove action for account `a` MUST: re-check the list (if already present for add, or absent for remove, emit `ProposalExecutionSkipped(a, current_proposal_id, reason)` and return false, with reasons `"ALREADY_BLACKLISTED"`, `"NOT_IN_BLACKLIST"`, `"ALREADY_AUTHORIZED"`, `"NOT_AUTHORIZED"`); call `AccountManager` with the matching method (`blacklist`, `unBlacklist`, `authorize`, `unAuthorize`); if that call fails, emit `ProposalExecutionSkipped(a, current_proposal_id, "GovCouncil: <method> call failed")` and return false; otherwise update the list, emit the matching event (`AddressBlacklisted`, `AddressUnblacklisted`, `AuthorizedAccountAdded`, `AuthorizedAccountRemoved`) with `(a, current_proposal_id)` and return true.
Source: systemcontracts/solidity/v1/GovCouncil.sol:379-496
Observable: state

[SNET-GOV-134] Executing a batch action MUST apply the single-element procedure to each element in order, ignore each element's return value, and return true. A batch therefore ends `Executed` even if every element was skipped, and a duplicate element is skipped on its second occurrence.
Source: systemcontracts/solidity/v1/GovCouncil.sol:326-361
Observable: state

[SNET-GOV-136] (withdrawn; informative) A consequence of SNET-GOV-051 to SNET-GOV-055: a single-account action that returns false leaves the proposal `Approved` in retry mode (SNET-GOV-055), so a duplicate or stale blacklist proposal can be closed by `executeWithFailure`, by three counted attempts followed by a fourth (which yields `Failed`), or by expiry. Until then any later attempt (`executeProposal`, `executeWithFailure`, or a further approval) re-runs the action against the current list and executes it if the list changed in the meantime, for example re-blacklisting an address that another proposal removed.
Source: systemcontracts/solidity/v1/GovCouncil.sol:379-385
Source: systemcontracts/test/gov_council_toctou_test.go:76-101

### AccountManager

[SNET-GOV-137] While Anzeon rules are active, `0x0000000000000000000000000000000000B00003` MUST be a native manager with methods selected by the first 4 bytes of the input: `blacklist(address)`, `unBlacklist(address)`, `authorize(address)`, `unAuthorize(address)` (mutating) and `isBlacklisted(address)`, `isAuthorized(address)` (views). The input MUST be exactly 4 + 32 bytes; the address is the low 20 bytes of the word. An unknown selector, a wrong length, or a context violation MUST fail the call.
Source: params/protocol_params.go:220
Source: core/vm/native_manager.go:82-111
Source: core/vm/native_manager.go:137-156
Observable: state

[SNET-GOV-138] The four mutating methods MUST fail unless invoked by the `CALL` opcode (not `STATICCALL`, `DELEGATECALL`, `CALLCODE`) and unless the caller is the `GovCouncil` address from the genesis chain configuration. On success they set or clear bit 63 (blacklisted) or bit 62 (authorized) of the target account's `Extra` field. Setting a bit on a non-existent account creates it (an account with non-zero `Extra` is not empty); clearing a bit that is not set only touches the account, so an empty account is pruned again by EIP-161 clearing. The view methods accept any caller and context and return a 32-byte boolean word.
Source: core/vm/native_manager.go:288-458
Source: core/vm/native_manager.go:470-489
Source: core/types/state_account_extra.go:31-45
Source: core/state/statedb.go:434-460
Source: core/state/state_object.go:94-96
Source: core/state/state_object.go:524-560
Observable: state

---

## 11. Minting contracts (bounded)

The minting contracts do not influence consensus: nothing in the consensus engine or header validation reads their storage, and they cannot change validators, BLS keys, the gas tip or account `Extra` bits. They affect balances (through `NativeCoinManager`) and therefore the state root, which a node obtains by executing them in the EVM. A native validator module for consensus-only verification does not need to implement them. The summaries below state the semantics at the level needed to recognise state changes and to write test vectors; they are informative except where marked.

[SNET-GOV-150] A consensus-only validator module NEED NOT implement `GovMasterMinter`, `GovMinter` or `NativeCoinAdapter`. A module that claims full state equivalence MUST execute them as EVM code rather than re-implement them.
Source: consensus/wbft/engine/engine.go:606-646
Source: core/vm/native_manager.go:463-468
Observable: state

**GovMasterMinter** (`0x1002`, v1). A `GovBase` instance that manages the minter set of the fiat token (`NativeCoinAdapter`, whose `masterMinter` is this contract). Actions `CONFIGURE_MINTER(minter, allowance)` (`0 < allowance <= maxMinterAllowance`), `REMOVE_MINTER(minter)`, `UPDATE_MAX_MINTER_ALLOWANCE(limit > 0)`, `PAUSE`, `UNPAUSE`. Each re-validates at execution and reverts on violation; configure/remove are blocked while paused and call `fiatToken.configureMinter` / `removeMinter`, reverting on failure. Soft failure (false) happens only for unknown actions. (Source: `systemcontracts/solidity/v1/GovMasterMinter.sol:118-350`.)

**GovMinter v1** (`0x1003`). A `GovBase` instance whose members propose mints and burns backed by off-chain proofs. `proposeMint(proof)` requires non-paused, non-zero beneficiary and amount, non-empty `depositId` and `bankReference`, `amount <= minterAllowance(this) - reservedMintAmount` (checked subtraction), an unused `depositId` (not executed, not attached to a live proposal) and an unused proof hash; it reserves the amount (`reservedMintAmount += amount`, `mintProposalAmounts[pid] = amount`) after `_createProposal` returns. `proposeBurn(proof)` is payable, requires `proof.from == sender` and `msg.value == amount`, credits `burnBalance[sender]`, and records the burn. Execution uses `try fiatToken.mint/burn`: a caught failure returns false (soft failure, counted attempt); paused reverts. `_onProposalFinalized` releases the mint reservation. `proposeBurn` also records its bookkeeping (`withdrawalIdToProposalId`, `burnProposals`) after `_createProposal`. (Source: `systemcontracts/solidity/v1/GovMinter.sol:202-317`, `377-525`, `700-760`, `875-920`.)

**GovMinter v2** (Boho upgrade, code only). Adds `refundableBalance`, moves the `burnBalance` credit after validation, adds `if totalAllowance <= reservedMintAmount revert InsufficientMinterAllowance()` before the subtraction, and on finalization of a non-executed burn proposal moves the deposit from `burnBalance` to `refundableBalance` (emitting `BurnDepositRefunded` before the terminal log); `claimBurnRefund()` pays it out to the caller. Deposits stranded under v1 are not migrated. (Source: `systemcontracts/solidity/v2/GovMinter.sol` diff against v1: `:133-135`, `:226`, `:296-308`, `:350-364`, `:937-964`.)

**NativeCoinAdapter** (`0x1000`). An ERC-20/EIP-2612/EIP-3009 facade over native balances: `balanceOf` is the account balance, `mint`/`burn`/`transfer*` call `NativeCoinManager` (`0x…B00002`), which only accepts `CALL`s from the genesis `NativeCoinAdapter` address. `mint` requires a minter with sufficient `minterAllowed`, and neither the minter nor the recipient blacklisted (queried from `AccountManager`); `burn` burns the caller's own native balance. `totalSupply` is a counter maintained by mint and burn only. Because the token is the native coin, it can be moved by signatures alone (EIP-2612, EIP-3009, ERC-1271); the security consequences are in `B-04` §13. (Source: `systemcontracts/solidity/v1/NativeCoinAdapter.sol:53-180`, `systemcontracts/solidity/abstracts/Mintable.sol:52-121`, `core/vm/native_manager.go:463-468`.)

[SNET-GOV-176] (restatement of the artifact) `NativeCoinAdapter` v1 MUST revert with `"NativeCoinAdapter: account is blacklisted"`, before any signature check or state change, when an address listed for the called function in the table below has bit 63 set at execution time; for `mint` and `burn` the minter check runs first. Callers not listed are covered by the EVM call-level check (`B-07` SNET-TX-043) and, for the transaction sender, by `B-07` SNET-TX-040.
Source: systemcontracts/solidity/v1/NativeCoinAdapter.sol:86-101 (notBlacklisted, AccountManager.isBlacklisted), systemcontracts/solidity/v1/NativeCoinAdapter.sol:139-204, systemcontracts/solidity/v1/NativeCoinAdapter.sol:245-280, systemcontracts/solidity/v1/NativeCoinAdapter.sol:320-477
Source: core/vm/evm.go:614-627
Observable: state

| Function | Addresses the contract checks | Not checked by the contract |
|---|---|---|
| `mint` | `msg.sender`, `_to` | — |
| `burn` | `msg.sender` | — |
| `transfer` | `msg.sender`, `to` | — |
| `transferFrom` | `msg.sender`, `from`, `to` | — |
| `approve`, `increaseAllowance`, `decreaseAllowance` | `msg.sender`, `spender` | — |
| `permit` (both forms) | `owner`, `spender` | `msg.sender` (checked by the EVM) |
| `transferWithAuthorization`, `receiveWithAuthorization` (both forms) | `from`, `to` | `msg.sender` (checked by the EVM) |
| `cancelAuthorization` (both forms) | none | `authorizer` |

Executed against the artifact: a `permit` whose `owner` has bit 63 set reverted with the message above before the signature was examined; a `cancelAuthorization` by an `authorizer` with bit 63 set succeeded and set `authorizationState` to true. Because the checks run at execution time only, allowances and signatures made before an account was blacklisted become usable again when it is removed (`B-04` §13.3).

---

## 12. Genesis-initialised state (summary)

The contracts have no initialiser function: genesis writes their storage directly (B-02 specifies the procedure, B-04 the slots). A mirror MUST start from exactly that state, including values that differ from Solidity's declared initialisers.

[SNET-GOV-160] The initial `GovBase` state of each instance MUST be: `quorum` = the `quorum` parameter or 0 if absent; `proposal_expiry` = the `expiry` parameter or 0 if absent; `member_version` = the `memberVersion` parameter (required when `members` is given) or 0 if `members` is absent; the de-duplicated `members` list, in parameter order, as `versioned_member_list[member_version]` with `joined_at = 0`; `quorum_by_version[member_version] = quorum` only if `quorum > 0`; `max_active_proposals_per_member` = `maxProposals` or 3; `current_proposal_id = 0`.
Source: systemcontracts/gov_base.go:135-336
Observable: state

[SNET-GOV-161] The initial `GovValidator` state MUST additionally be: `bls_pop = 0x…B00001`; `gas_tip` = the `gasTip` parameter or `InitialGasTip` (27,600,000,000,000 wei); and, if `validators` is given, for each index `i` in parameter order, skipping a validator address already seen, `validators.add(validators[i])`, `operator_to_validator[members[i]] = validators[i]`, `validator_to_operator[validators[i]] = members[i]`, and the key mappings with `blsPublicKeys[i]`. The `members` list used for this pairing is the raw parameter list (not de-duplicated). No PoP, key-length or key-uniqueness check is applied at genesis.
Source: systemcontracts/gov_validator.go:52-184
Source: params/protocol_params.go:138
Observable: state

[SNET-GOV-162] The initial `GovCouncil` state MUST additionally be: `account_manager = 0x…B00003`; and, only when the genesis allocation is available, the blacklist and authorized lists equal to the union of the parameter lists and the accounts whose allocation `Extra` has the corresponding bit, stored in ascending byte order of address, with the bits set on those accounts in the allocation; for an address that is also a genesis system-contract address, `inject_contracts` replaces the allocation entry afterwards (`B-02` SNET-GEN-012, SNET-GEN-014; `B-04` SNET-SYS-011), so that address is in the list with `Extra = 0`.
Source: systemcontracts/gov_council.go:68-156
Source: systemcontracts/gov_council.go:163-211
Source: core/genesis.go:750-752
Observable: state

---

## 13. Native module equivalence

### Scenario test vectors

The vectors below should be generated by running each sequence against the reference contracts (the `systemcontracts/test` simulated-backend harness, e.g. `NewGovWBFT`, `systemcontracts/test/gov_base_versioned_membership_test.go:52-86`) and recording, per step: the sender, call, arguments and block timestamp; success or revert with the custom error; the logs; and after each step the consensus projection plus the `GovBase` state of the touched instance. A module passes a vector if every step matches. Unless stated otherwise, the starting point is 4 members `A, B, C, D` with validators `vA..vD` in that order, quorum 3, expiry 604800 s, `maxProposals` 3, gas tip `T0`.

| Id | Sequence | Expected (key facts) |
|---|---|---|
| V-01 | `A.proposeGasTip(T1)`; `B.approve(1)`; `C.approve(1)` | step 3: `Approved`, `Executed`, `gas_tip = T1`, `GasTipUpdated(T0, T1, C)`, log order `ProposalVoted, ProposalApproved, GasTipUpdated, ProposalExecuted` |
| V-02 | `A.proposeGasTip(T0)` | revert `SameGasTip` |
| V-03 | two proposals for `T1` by `A` and `B`, both approved | both `Executed`; two `GasTipUpdated`, second with `old = new = T1` |
| V-04 | `E` new: `A.proposeAddMember(E, 3)`, approvals to quorum; `E.configureValidator(vE, kE, popE)` | `member_version = 2`; validators `[vA, vB, vC, vD, vE]`; no log from `configureValidator` |
| V-05 | `B.configureValidator(vX, kB, popB)` (replace, same key) | validators `[vA, vD, vC, vX]` (swap-and-pop then append) |
| V-06 | `C.configureValidator(vC, kC2, popC2)` (key change) | order unchanged; `bls_key_to_validator[kC] = 0` |
| V-07 | `A.configureValidator(vA, kA, popA)` | revert `NoConfigurationChanging` |
| V-08 | `A.configureValidator(vB, …)` / `(…, kB, …)` / wrong PoP / malformed key | `AlreadyValidatorExists` / `AlreadyRegisteredBlsKey` / `InvalidBlsKey` / `FailedToVerifyBlsKey` |
| V-09 | `A.proposeRemoveMember(B, 2)`, approvals to quorum | validators `[vA, vD, vC]`; `member_version = 2`; logs of the final approval `ProposalVoted, ProposalApproved, MemberRemoved, QuorumUpdated, ProposalExecuted`; no validator log |
| V-10 | `B.changeMember(B2)`; then `B2.configureValidator(vB, kB3, popB3)` | `operator_to_validator[B2] = vB`; second call is a key change (case 2) |
| V-12 | `A.proposeChangeQuorum(4)`; remove `D` via another proposal; approvals for the first to quorum | final approval reverts `InvalidQuorum`; proposal stays `Voting` |
| V-13 | proposal created at `t`; vote at `t + expiry` | vote recorded |
| V-14 | proposal created at `t`; vote at `t + expiry + 1` | no revert; `Expired`; `ProposalExpired`; vote not recorded; active count decremented |
| V-15 | `A` creates 3 proposals; 4th | revert `TooManyActiveProposals`; after one is cancelled, 4th succeeds |
| V-16 | `A.propose…`; `B.disapprove`; `A.cancelProposal` | revert `ProposalAlreadyInVoting` |
| V-17 | with quorum 3 of 4: two disapprovals | second: `Rejected` (`2 > 4 - 3`) |
| V-18 | removed member `A` approves a proposal created before its removal | accepted; can complete the quorum |
| V-19 | `A.changeMember(A2)` with a live proposal of the current version that `A` created | `A2` cannot cancel (`NotProposer`); `A` cannot vote on it (`NotAMember`); `A2`'s bit is `A`'s |
| V-20 | 1-member instance, quorum 1: `A.proposeGasTip(T1)` | executed in the creating call |
| V-21 | GovCouncil: `A.proposeAddBlacklist(X)` and `B.proposeAddBlacklist(X)`; approve #1 then #2 | #1 `Executed`, bit 63 of `X` set; #2 stays `Approved` with `ProposalExecutionSkipped(X, 2, "ALREADY_BLACKLISTED")`; `executeWithFailure(2)` → `Failed` |
| V-23 | GovCouncil: retry-mode attempts on a skipped proposal ×3, then a 4th | attempts 1–3: `ProposalExecuted(false)`; 4th: `Failed`, `ProposalFailed("Max retry count reached")` |
| V-24 | GovCouncil batch `[X, X, Y]` add | `Executed`; `X` once, skip log for second `X`; `Y` added |
| V-26 | genesis with duplicated member entry paired with two validators | projection read from storage shows both validators; removal of the member removes only the second |

---

## 14. Events reference

| Contract | Event | Emitted by |
|---|---|---|
| GovBase | `ProposalCreated(uint256 indexed id, address indexed proposer, bytes32 actionType, uint256 memberVersion, uint256 requiredApprovals, bytes callData)` | SNET-GOV-031 |
| GovBase | `ProposalVoted(uint256 indexed id, address indexed voter, bool approval, uint256 approved, uint256 rejected)` | SNET-GOV-042, 044 |
| GovBase | `ProposalApproved(id, approver, approved, rejected)`, `ProposalRejected(id, rejector, approved, rejected)` | SNET-GOV-042, 044 |
| GovBase | `ProposalExecuted(id, executor, bool success)`, `ProposalFailed(id, executor, bytes reason)`, `ProposalExpired(id, executor)`, `ProposalCancelled(id, canceller)` | §5 |
| GovBase | `MemberAdded(address indexed member, uint256 totalMembers, uint32 newQuorum)`, `MemberRemoved(...)`, `MemberChanged(address indexed old, address indexed new)`, `QuorumUpdated(uint32 old, uint32 new)`, `MaxProposalsPerMemberUpdated(uint256 old, uint256 new)` | §6 |
| GovValidator | `GasTipUpdated(uint256 oldTip, uint256 newTip, address indexed updater)` | SNET-GOV-112 |
| GovCouncil | `AddressBlacklisted`, `AddressUnblacklisted`, `AuthorizedAccountAdded`, `AuthorizedAccountRemoved` `(address indexed account, uint256 indexed proposalId)`; `ProposalExecutionSkipped(address indexed account, uint256 indexed proposalId, string reason)` | SNET-GOV-133..135 |
| NativeCoinAdapter | `Mint(address indexed minter, address indexed to, uint256 amount)` `0xab8530f8…c9f8`; `Burn(address indexed burner, uint256 amount)` `0xcc16f5db…7ca5`; `MinterConfigured(address indexed minter, uint256 minterAllowedAmount)` `0x46980fca…0d20`; `MinterRemoved(address indexed oldMinter)` `0xe94479a9…6692`; `MasterMinterChanged(address indexed newMasterMinter)` `0xdb66dfa9…07e6`; `Approval(address indexed owner, address indexed spender, uint256 value)` `0x8c5be1e5…b925`; `AuthorizationUsed(address indexed authorizer, bytes32 indexed nonce)` `0x98de5035…10a5`; `AuthorizationCanceled(address indexed authorizer, bytes32 indexed nonce)` `0x1cdd46ff…3d81` | §11 |
| GovMasterMinter | `MinterConfigured(address indexed minter, uint256 allowance)` `0x46980fca…0d20`; `MinterRemoved(address indexed minter)` `0xe94479a9…6692`; `MaxMinterAllowanceUpdated(uint256 oldLimit, uint256 newLimit)` `0x0f657be9…5d5c`; `EmergencyPaused(uint256 indexed proposalId)` `0x11d8c430…67d5`; `EmergencyUnpaused(uint256 indexed proposalId)` `0x4e8fee66…9080` | §11 |
| GovMinter v1, v2 | `DepositMintProposed(uint256 indexed proposalId, string indexed depositId, address indexed requester, address beneficiary, uint256 amount, string bankReference)` `0x53586a2e…f4c3`; `BurnPrepaid(address indexed user, uint256 amount)` `0x9ac4ab95…799f`; `BurnExecuted(address indexed from, uint256 indexed amount, string withdrawalId)` `0xc4a1fb50…6530`; `EmergencyPaused` `0x11d8c430…67d5`; `EmergencyUnpaused` `0x4e8fee66…9080`; v2 only: `BurnDepositRefunded(uint256 indexed proposalId, address indexed requester, uint256 amount)` `0x116044c8…2062`, `BurnRefundClaimed(address indexed requester, uint256 amount)` `0x9543fa26…af24` | §11 |

Source: systemcontracts/solidity/abstracts/GovBase.sol:156-177; systemcontracts/solidity/v1/GovValidator.sol:175; systemcontracts/solidity/v1/GovCouncil.sol:87-108.
Source: systemcontracts/solidity/abstracts/Mintable.sol:42-47, systemcontracts/solidity/abstracts/eip/EIP3009.sol:53-54, systemcontracts/solidity/v1/NativeCoinAdapter.sol:260 (Approval, from the OpenZeppelin `IERC20` interface)
Source: systemcontracts/solidity/v1/GovMasterMinter.sol:77-89, systemcontracts/solidity/v1/GovMinter.sol:133-153, systemcontracts/solidity/v2/GovMinter.sol:138-164

The hex values after the minting-contract events are the first and last four bytes of topic0 (`keccak256` of the signature); each full value occurs in the corresponding artifact. Events with the same signature are emitted by more than one system contract: `MinterConfigured` and `MinterRemoved` by `NativeCoinAdapter` and `GovMasterMinter` (one `CONFIGURE_MINTER` execution emits both), and `EmergencyPaused` and `EmergencyUnpaused` by `GovMasterMinter` and `GovMinter`; they are distinguished only by the log address.
