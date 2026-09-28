# B-05 거버넌스 의미론

- Status: draft
- Area code: `GOV`
- Reference: go-stablenet `740526d03`. 모든 `Source:` 경로는 저장소 루트 기준 상대 경로다.

이 장은 StableNet 거버넌스 system contract 가 각 연산에서 자기 state 에 무엇을 하는지 연산 단위로 정의한다.

이 장은 `GovBase`(공통 member·proposal 엔진), `GovValidator`, `GovCouncil`, `blsPoP` precompile, `AccountManager` native manager 를 다룬다. Minting contract(`GovMasterMinter`, `GovMinter` v1/v2, `NativeCoinAdapter`)는 이 장에서 요약만 한다. Storage slot 배치는 `B-04` 에 있다. 이 contract 들의 genesis 초기화는 `B-02` 에 있다. 합의 엔진이 contract state 를 candidate, BLS 키, proposer 자격에 연결하는 방식은 `B-08` 에 있다. Header 의 gas tip 검증은 `B-06` 에 있고, 트랜잭션 수준의 blacklist 규칙과 authorized 계정 규칙은 `B-07` 에 있다.

---

## 1. 범위, conformance, 실행 모델

### 규범 내용

이 장의 규범 내용은 각 거버넌스 연산이 관찰 가능한 state 에 주는 효과다. 효과는 다음 네 가지다.

- 연산이 contract storage 에 쓰는 값이다. 대상은 member, member version, quorum, proposal, vote bitmap, attempt counter, validator mapping, BLS 키, gas tip, blacklist 목록, authorized 목록이다.
- 연산이 `AccountManager` 를 통해 쓰는 계정 `Extra` 비트다.
- 연산이 내보내는 log 와 그 인자, 그리고 한 호출 안에서 log 가 나가는 순서다.
- 호출이 revert 하는지, 그리고 revert 한다면 어떤 custom error 로 revert 하는지다. Custom error 는 `eth_call` 과 `debug_trace*` 로 볼 수 있으며, receipt 는 status 만 보여 준다.

Gas 양은 규범이 아니다. 연산의 결과가 하위 호출에 넘기는 gas 양에 따라 달라지는 곳은 이 장이 따로 표시한다(§13).

### Pseudocode 가 쓰는 실행 모델

Pseudocode 는 각 contract 를 변경 가능한 객체로 모델링한다. `ctx.sender` 는 현재 call frame 의 `msg.sender` 이다. `ctx.timestamp` 는 `block.timestamp` 이며, 트랜잭션을 담은 block 의 `header.Time` 과 같다. `revert(E)` 는 현재 call frame 을 중단한다. 그러면 그 frame 과 하위 frame 이 만든 모든 storage 쓰기, `Extra` 비트 쓰기, log 가 버려지고, caller 는 error `E` 로 실패한 호출을 본다. `emit X(...)` 는 현재 frame 에 log 를 하나 덧붙인다. `require_*` helper 는 이름에 적힌 error 로 revert 한다.

[SNET-GOV-001] 각 거버넌스 contract(`GovValidator`, `GovCouncil`, `GovMinter`, `GovMasterMinter`)는 반드시 독립된 `GovBase` instance 로 취급해야 한다. 각 instance 는 자기 member 집합, member version, quorum, proposal counter, proposal, attempt counter, 활성 proposal counter 를 따로 가진다. 한 instance 의 member 는 다른 instance 에서 아무 권한이 없다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:131-150
Source: systemcontracts/solidity/v1/GovValidator.sol:23
Source: systemcontracts/solidity/v1/GovCouncil.sol:58
Source: systemcontracts/solidity/v1/GovMinter.sol:52-55
Source: params/config_wbft.go:31-45
Observable: state

[SNET-GOV-002] 거버넌스 연산은 그 연산을 수행하는 call frame 과, 트랜잭션까지 그 frame 을 감싸는 모든 frame 이 revert 없이 끝날 때, 그리고 그때에만 효력을 가진다. Member 는 EOA 여도 되고 contract 여도 된다(MAY). 행위자는 항상 거버넌스 contract 로 들어온 호출의 `msg.sender` 이며, 이 호출은 다른 contract 가 만든 내부 호출일 수 있다. 그러므로 거버넌스 state 의 mirror 는 반드시 최상위 트랜잭션 필드가 아니라 call frame 단위의 호출이나 state 를 근거로 갱신되어야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:24
Source: systemcontracts/solidity/abstracts/GovBase.sol:182-186
Observable: state

> 해설: SNET-GOV-002 는 §13 에서 mirror 의 입력을 고를 때 결정적인 근거가 된다. Member 가 contract 이면 거버넌스 호출은 내부 호출로 일어나므로, 최상위 트랜잭션만 보는 tracer 는 그 호출을 놓친다.

[SNET-GOV-003] 거버넌스의 모든 시간 비교는 반드시 `block.timestamp` 를 써야 한다. Block 번호에 기반한 만료는 없다. 시각 `c` 에 만든 proposal 은 반드시 `t > c + proposalExpiry` 일 때, 그리고 그때에만 시각 `t` 에서 *만료* 된 것으로 다뤄야 한다. 이 비교는 등호를 포함하지 않는 엄격한 부등호이므로, `t == c + proposalExpiry` 인 시각에는 proposal 이 아직 살아 있다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:334
Source: systemcontracts/solidity/abstracts/GovBase.sol:757
Source: systemcontracts/solidity/abstracts/GovBase.sol:863
Observable: state

```python
def is_expired(g, p, now) -> bool:
    return now > p.created_at + g.proposal_expiry   # uint256, cannot overflow for genesis-sized values
```

---

## 2. 타입, 상수, state

### 상수

| 이름 | 값 | Source |
|---|---|---|
| `MAX_MEMBER_INDEX` | 255 | `GovBase.sol:118` |
| `MAX_RETRY_COUNT` | 3 (retry 횟수가 아니라 전체 실행 시도 횟수다) | `GovBase.sol:119` |
| `INITIAL_MEMBER_VERSION` | 1 (선언된 initializer 값이다. genesis 가 주입한 code 에서는 실행되지 않는다. §12 참고) | `GovBase.sol:120, 132` |
| `BLS_PUBLIC_KEY_LENGTH` / `BLS_SIGNATURE_LENGTH` | 48 / 96 | `GovValidator.sol:26-27` |

[SNET-GOV-004] Action type 은 아래 ASCII 문자열의 Keccak-256 hash 다. 문자열의 형식은 일정하지 않다. `GovBase` action 의 문자열만 `ACTION_` 접두사를 가진다. Action type 은 proposal 에 저장되고 `ProposalCreated` 에 실려 나가므로, 구현은 반드시 정확히 이 preimage 를 써야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:123-127
Source: systemcontracts/solidity/v1/GovValidator.sol:50
Source: systemcontracts/solidity/v1/GovCouncil.sol:71-80
Observable: state

| Contract | Action | Preimage | `callData` (ABI 인코딩) |
|---|---|---|---|
| 모두 | member 추가 | `"ACTION_ADD_MEMBER"` | `(address newMember, uint32 newQuorum)` |
| 모두 | member 제거 | `"ACTION_REMOVE_MEMBER"` | `(address member, uint32 newQuorum)` |
| 모두 | member 변경 | `"ACTION_CHANGE_MEMBER"` | 상수는 있지만 이 상수를 쓰는 proposal 은 없다 (`changeMember` 는 직접 호출이다, §6) |
| 모두 | quorum 변경 | `"ACTION_CHANGE_QUORUM"` | `(uint32 newQuorum)` |
| 모두 | 최대 proposal 수 변경 | `"ACTION_CHANGE_MAX_PROPOSALS"` | `(uint256 newMax)` |
| GovValidator | gas tip 설정 | `"SET_GAS_TIP"` | `(uint256 newTip)` |
| GovCouncil | blacklist 추가 / 제거 | `"ADD_BLACKLIST"` / `"REMOVE_BLACKLIST"` | `(address)` |
| GovCouncil | blacklist 일괄 추가 / 제거 | `"ADD_BLACKLIST_BATCH"` / `"REMOVE_BLACKLIST_BATCH"` | `(address[])` |
| GovCouncil | authorized 추가 / 제거 | `"ADD_AUTHORIZED_ACCOUNT"` / `"REMOVE_AUTHORIZED_ACCOUNT"` | `(address)` |
| GovCouncil | authorized 일괄 추가 / 제거 | `"ADD_AUTHORIZED_ACCOUNT_BATCH"` / `"REMOVE_AUTHORIZED_ACCOUNT_BATCH"` | `(address[])` |

### Proposal status

[SNET-GOV-005] `ProposalStatus` 는 반드시 8 비트 enum 으로 인코딩해야 하며, 값은 `None = 0`, `Voting = 1`, `Approved = 2`, `Executed = 3`, `Cancelled = 4`, `Expired = 5`, `Failed = 6`, `Rejected = 7` 이다. `Voting` 과 `Approved` 는 *살아 있는(live)* status 다. `Executed`, `Cancelled`, `Expired`, `Failed`, `Rejected` 는 *종료(terminal)* status 이며, 어떤 연산도 종료된 proposal 을 바꿔서는 안 된다.
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

## 3. Member 집합과 version

Member 집합은 version 별 snapshot 의 연속으로 관리한다. Member 를 추가하거나 제거하면 새 version 이 생긴다. Member 의 주소를 바꾸면 현재 version 을 그 자리에서 고친다. Proposal 은 만들어질 때의 version 을 기록하고, proposal 이 살아 있는 동안 내내 그 snapshot 으로 vote 권한을 정한다.

[SNET-GOV-010] Version `v` 에서 member 의 index 는 반드시 `member_index_by_version[v][addr]` 에 `index + 1` 로 저장해야 한다. 저장값 `0` 은 "`v` 에서 member 가 아니다" 를 뜻한다. `index_at(addr, v)` 는 반드시 `member_index_by_version[v][addr] - 1` 을 돌려주고, 저장값이 0 이면 `2**256 - 1` 을 돌려줘야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1297-1303
Observable: state

[SNET-GOV-011] 거버넌스 연산이 성공할 때마다, 현재 version `V = member_version` 에 대해 다음이 반드시 성립해야 한다. 첫째, `a` 가 `versioned_member_list[V]` 에 있을 때, 그리고 그때에만 `members[a].is_active` 가 참이다. 둘째, 각 주소는 그 목록에 많아야 한 번 나온다. 셋째, 모든 `i` 에 대해 `member_index_by_version[V][list[i]] == i + 1` 이다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:933-978
Source: systemcontracts/solidity/abstracts/GovBase.sol:1049-1095
Source: systemcontracts/solidity/abstracts/GovBase.sol:448-494
Observable: state

[SNET-GOV-012] `v < member_version` 인 snapshot `versioned_member_list[v]` 는 바뀌어서는 안 된다. 현재 version 의 snapshot 은 반드시 `changeMember`(§6)가 그 자리에서 고쳐야 한다. 그래서 현재 version 에서 만든 proposal 은, 다음 추가나 제거가 새 version 을 만들기 전까지 원래 주소가 아니라 바뀐 주소를 본다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:456-476
Source: systemcontracts/solidity/abstracts/GovBase.sol:909-915
Observable: state

[SNET-GOV-013] 어떤 version 에서도 member 수는 255(`MAX_MEMBER_INDEX`)를 넘어서는 안 된다. 현재 version 에 member 가 이미 255 명일 때 member 를 추가하면 `MemberIndexOverflow` 로 revert 한다.
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

[SNET-GOV-020] Quorum 값 `q` 는 반드시 (`n == 1` 이고 `q == 1`) 이거나 (`n >= 2` 이고 `2 <= q <= n`) 일 때, 그리고 그때에만 member 수 `n` 에 대해 유효한 것으로 다뤄야 한다. Member 를 제거한 뒤 member 수가 0 이 되는 경우는 결코 유효하지 않다. 그래서 마지막 member 는 제거할 수 없다.
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

[SNET-GOV-022] Contract 는 `quorum_by_version[v]` 에 반드시 새 quorum 을 써야 한다. Member 추가와 제거가 끝날 때마다 새 version 의 항목에 쓰고, quorum 을 바꿀 때마다 현재 version 의 항목에 쓴다. Proposal 로직은 `quorum_by_version` 을 참조하지 않는다. Proposal 은 만들어질 때 `quorum` 에서 가져온 `required_approvals` snapshot 을 쓰며, 이후의 quorum 변경은 기존 proposal 에 영향을 주지 않는다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:704
Source: systemcontracts/solidity/abstracts/GovBase.sol:977
Source: systemcontracts/solidity/abstracts/GovBase.sol:1008
Source: systemcontracts/solidity/abstracts/GovBase.sol:1094
Observable: state

[SNET-GOV-023] Proposal `p` 의 거부 문턱은 반드시 `max_rejections = len(versioned_member_list[p.member_version]) - p.required_approvals` 여야 한다. 이 뺄셈은 uint32 산술로 계산한다. `rejected > max_rejections` 가 되면 proposal 은 반드시 `Rejected` 가 되어야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:818-824
Observable: state

---

## 5. Proposal 의 생애주기

### State machine

아래 표는 모든 전이를 나열한다. "attempt" 는 `execute_proposal` 의 호출 한 번을 뜻한다(SNET-GOV-050..057). 표에 없는 전이는 존재하지 않는다. 특히 어떤 전이도 종료 status 에서 벗어나지 않으며, `Approved` 는 `Voting` 으로 돌아가지 않는다.

| 현재 status | 계기 | 조건 (이 순서로 검사한다) | 다음 status | Log (순서대로) |
|---|---|---|---|---|
| — | `propose*` | SNET-GOV-030/031 의 전제 조건이 성립한다 | `Voting` | `ProposalCreated`, 이어서 proposer 의 vote log |
| `Voting`/`Approved` | 찬성 | 만료되었다 | `Expired` | `ProposalExpired` |
| `Voting` | 찬성 | `approved + 1 < required` | `Voting` | `ProposalVoted(true)` |
| `Voting` | 찬성 | `approved + 1 >= required` | `Approved` 가 된 뒤 attempt 를 한다 | `ProposalVoted(true)`, `ProposalApproved`, attempt 의 log |
| `Approved` | 찬성 | 만료되지 않았다 | `Approved` 에서 attempt 를 한다 | `ProposalVoted(true)`, attempt 의 log |
| `Voting`/`Approved` | 반대 | 만료되었다 | `Expired` | `ProposalExpired` |
| `Voting` | 반대 | `rejected + 1 > max_rejections` | `Rejected` | `ProposalVoted(false)`, `ProposalRejected` |
| `Voting`/`Approved` | 반대 | 그 밖의 경우 | 바뀌지 않는다 | `ProposalVoted(false)` |
| `Approved` | attempt | 만료되었다 | `Expired` | `ProposalExpired` |
| `Approved` | attempt | `execution_count >= 3` | `Failed` | `ProposalFailed("Max retry count reached")` |
| `Approved` | attempt | action 이 성공한다 | `Executed` | action 의 log, `ProposalExecuted(true)` |
| `Approved` | attempt, retry 모드 | action 이 false 를 돌려준다 | `Approved` | action 의 log, `ProposalExecuted(false)` |
| `Approved` | attempt, terminal 모드 | action 이 false 를 돌려준다 | `Failed` | action 의 log, `ProposalExecuted(false)` |
| 살아 있는 모든 status | attempt / vote | action 이 revert 한다 | 호출 전체가 revert 하고 아무것도 바뀌지 않는다 | 없음 |
| `Voting` | `cancelProposal` | sender 가 proposer 이고, `approved <= 1`, `rejected == 0` 이다 | `Cancelled` | `ProposalCancelled` |
| `Voting`/`Approved` | `expireProposal` | 만료되었다 | `Expired` | `ProposalExpired` |

종료 status 로 가는 모든 전이는 `finalize_proposal`(SNET-GOV-062)을 거친다. `finalize_proposal` 은 종료 log 가 나가기 전에 실행된다.

### 생성

[SNET-GOV-030] 모든 public `propose*` 함수는 반드시 먼저 `ctx.sender` 가 활성 member 인지 검사해야 한다(`NotAMember`). 그다음 함수별 인자 검사를 §6, §8, §10 에 적힌 순서대로 해야 하고, 그다음 `create_proposal` 을 호출해야 한다. `create_proposal` 은 다음을 순서대로 검사한다.

1. 다시 활성 member 인지 검사한다.
2. `proposal_expiry != 0` 인지 검사한다(`InvalidProposalExpiry`).
3. `member_active_proposal_count[sender] < max_active_proposals_per_member` 인지 검사한다(`TooManyActiveProposals`).

처음 실패한 검사의 error 가 revert error 가 된다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:686-693
Observable: rpc

[SNET-GOV-031] `create_proposal` 은 반드시 `pid = current_proposal_id + 1` 을 할당해야 한다. 그리고 `status = Voting`, `member_version = member_version`, `required_approvals = quorum`, `created_at = ctx.timestamp`, 0 인 counter 와 bitmap 으로 proposal 을 저장해야 한다. 그다음 `ProposalCreated(pid, sender, action_type, member_version, quorum, call_data)` 를 내보내고, `member_active_proposal_count[sender]` 를 1 늘린 뒤, 자동 실행을 켠 채로 proposer 의 찬성표를 던져야 한다(SNET-GOV-040). Proposal id 는 1 부터 시작하며 재사용되지 않는다.
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

[SNET-GOV-032] Proposer 의 vote 가 자동 실행을 일으키므로, `required_approvals <= 1` 인 proposal 은 반드시 그 proposal 을 만드는 호출 안에서 실행되어야 한다. 그 실행이 revert 하면 생성 전체가 revert 하며, proposal id 도 소비되지 않는다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:717-718
Source: systemcontracts/solidity/abstracts/GovBase.sol:797-808
Observable: state

> 해설: Quorum 이 1 인 instance 에서는 모든 proposal 이 이 경로를 탄다. 곧 proposal 은 만드는 호출 안에서 바로 실행되며, 별도의 찬성 단계가 없다.

### Vote

[SNET-GOV-040] `approveProposal(pid)` 와 `disapproveProposal(pid)` 는 반드시 `require_proposal_member` 를 검사해야 한다. 이 검사는 `pid == 0` 이거나 `pid > current_proposal_id` 일 때의 `InvalidProposal` 을 포함한다. 그다음 찬성이면 `auto_execute = True` 로, 반대이면 `auto_execute = False` 로 `vote` 를 실행해야 한다. `vote` 는 다음을 순서대로 검사한다.

1. 유효한 id 인지 검사한다(`InvalidProposal`).
2. status 가 살아 있는지 검사한다(`ProposalNotInVoting`).
3. Proposal 이 만료되었는지 검사한다(SNET-GOV-041).
4. snapshot member 인지 검사한다(`NotAMember`).
5. 중복 vote 인지 검사한다(`AlreadyApproved`). 이 error 는 찬성이든 반대든 두 번째 vote 에서 난다.

Source: systemcontracts/solidity/abstracts/GovBase.sol:262-271
Source: systemcontracts/solidity/abstracts/GovBase.sol:743-784
Observable: state, rpc

[SNET-GOV-041] Vote 할 때 proposal 이 만료되어 있으면, `vote` 는 반드시 그 proposal 을 `Expired` 로 finalize 하고, `ProposalExpired(pid, sender)` 를 내보내고, vote 를 기록하지 않은 채 반환해야 한다. 이때 호출은 revert 하지 않는다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:757-768
Observable: state

[SNET-GOV-042] 찬성은 반드시 voter 의 비트를 켜고, `approved` 를 1 늘리고, `ProposalVoted(pid, sender, true, approved, rejected)` 를 내보내야 한다. `approved >= required_approvals` 이면 다음을 한다. Status 가 아직 `Approved` 가 아니면 status 는 반드시 `Approved` 가 되어야 하고, `ProposalApproved(pid, sender, approved, rejected)` 를 반드시 내보내야 한다. 그리고 `auto_execute` 가 참이면 같은 호출 안에서 반드시 retry 모드의 실행 attempt 가 이어져야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:787-809
Observable: state

[SNET-GOV-043] 이미 `Approved` 인 proposal 에 대한 찬성은 반드시 vote 를 기록하고 실행 attempt 를 한 번 더 일으켜야 한다. 이미 `Approved` 인 proposal 은 앞선 attempt 가 실패해 `Approved` 에 남은 경우에 생긴다. 이때 `ProposalApproved` 는 다시 나가지 않는다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:798-808
Observable: state

[SNET-GOV-044] 반대는 반드시 voter 의 비트를 켜고, `rejected` 를 1 늘리고, `ProposalVoted(pid, sender, false, approved, rejected)` 를 내보내야 한다. `rejected > max_rejections`(SNET-GOV-023)이면 proposal 은 반드시 `Rejected` 로 finalize 되어야 하고, `ProposalRejected(pid, sender, approved, rejected)` 가 나가야 한다. 반대는 결코 실행을 일으키지 않는다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:810-825
Observable: state

[SNET-GOV-045] (유도된 성질.) `Approved` proposal 은 `Rejected` 가 될 수 없다. 각 snapshot member 는 많아야 한 번 vote 하므로, `approved >= required` 이면 `rejected <= n - required` 이기 때문이다. 구현은 `Approved` proposal 에 거부 경로를 추가해서는 안 된다.
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

### 실행

[SNET-GOV-050] `executeProposal(pid)`(retry 모드)와 `executeWithFailure(pid)`(terminal 모드)는 반드시 먼저 `require_proposal_member` 를 검사하고, 그다음 reentrancy guard(SNET-GOV-058)를 검사해야 한다. 그 뒤 다음을 순서대로 검사해야 한다.

1. 유효한 id 인지 검사한다(`InvalidProposal`).
2. `status == Approved` 인지 검사한다(`ProposalNotExecutable`).
3. `approved >= required_approvals` 인지 검사한다(`InsufficientApprovals`).

`Voting` proposal 은 실행할 수 없다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:279-289
Source: systemcontracts/solidity/abstracts/GovBase.sol:851-858
Observable: rpc

[SNET-GOV-051] Proposal 이 만료되었으면, attempt 는 반드시 그 proposal 을 `Expired` 로 finalize 하고, `ProposalExpired(pid, sender)` 를 내보내고, revert 하지 않고 attempt counter 도 늘리지 않은 채 false 를 돌려줘야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:863-867
Observable: state

[SNET-GOV-052] `proposal_execution_count[pid] >= 3` 이면, attempt 는 반드시 action 을 실행하지 않고 proposal 을 `Failed` 로 finalize 하고, `ProposalFailed(pid, sender, "Max retry count reached")` 를 내보내고, false 를 돌려줘야 한다. reason 인자는 이 문자열의 ASCII 바이트 그대로다. 이 규칙은 두 모드 모두에 적용된다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:870-874
Observable: state

[SNET-GOV-053] 그 밖의 경우 attempt 는 반드시 `proposal_execution_count[pid]` 를 1 늘리고 action 을 실행해야 한다(SNET-GOV-074). Counter 는 호출의 state 에 속하므로, action 이 revert 한 attempt 는 counter 를 바꾸지 않는다. Action 이 true 나 false 를 *돌려준* attempt 만 counter 에 세어진다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:876-880
Observable: state

[SNET-GOV-054] Action 이 true 를 돌려주면 proposal 은 반드시 `Executed` 로 finalize 되어야 하고, action 자신의 log 뒤에 `ProposalExecuted(pid, sender, true)` 가 나가야 한다. 이 finalize 는 `executed_at = ctx.timestamp` 로 설정한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:883-885
Observable: state

[SNET-GOV-055] Retry 모드에서 action 이 false 를 돌려주면, status 는 반드시 `Approved` 로 남아야 하고 `ProposalExecuted(pid, sender, false)` 가 반드시 나가야 한다. 그 뒤 `executeProposal`, `executeWithFailure`, 또는 추가 찬성이 이 proposal 의 실행을 다시 attempt 할 수 있다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:886-895
Observable: state

[SNET-GOV-058] `execute_proposal` 은 reentrancy guard 로 보호되는 유일한 함수다. 진입할 때 `reentrancy_guard == 1` 이면 반드시 `ReentrantCall` 로 revert 해야 하고, 그렇지 않으면 guard 를 1 로 설정해야 한다. 정상적으로 빠져나갈 때는 반드시 guard 를 0 으로 되돌려야 한다. 그러므로 action 안에서 일어나는 중첩 attempt 는 revert 한다. 예를 들어 같은 contract 의 `executeProposal` 을 다시 호출하거나, quorum 을 채우는 proposal 의 `approveProposal` 을 다시 호출하는 action 이 이 경우에 해당한다. Vote, propose, 취소, `changeMember`, `configureValidator` 호출은 guard 로 보호되지 않는다.
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

### 취소, 수동 만료, finalization

[SNET-GOV-061] `expireProposal(pid)` 는 반드시 `require_proposal_member` 를 검사해야 한다. Status 가 살아 있지 않거나 proposal 이 만료되지 않았으면, 반드시 state 를 바꾸지 않고 false 를 돌려줘야 한다. 그렇지 않으면 반드시 `Expired` 로 finalize 하고, `ProposalExpired(pid, sender)` 를 내보내고, true 를 돌려줘야 한다. Member 가 아닌 주소는 `expireProposal` 을 호출할 수 없다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:323-342
Observable: state

[SNET-GOV-062] `finalize_proposal(pid, s)`(Solidity 의 `_finalizeProposal`)는 반드시 다음을 이 순서로 해야 한다.

1. `status = s` 로 설정한다.
2. `s == Executed` 이면 `executed_at = ctx.timestamp` 로 설정한다.
3. `member_active_proposal_count[proposer]` 가 0 보다 크면 1 줄인다. 그래서 이 counter 는 0 아래로 내려가지 않는다.
4. 파생 hook `on_proposal_finalized(pid)` 를 호출한다. 이 hook 은 `GovMinter`(§11)를 제외하면 아무 일도 하지 않는다.

Caller 는 그 뒤에 종료 log 를 내보낸다. 그래서 hook 이 내보내는 log 는 종료 log 보다 앞에 온다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1247-1261
Source: systemcontracts/solidity/abstracts/GovBase.sol:1309-1314
Observable: state

---

## 6. Member 관리와 action dispatch

### Proposal 함수 (모든 GovBase instance 에 있다)

[SNET-GOV-070] `proposeAddMember(newMember, newQuorum)` 은 반드시 다음을 순서대로 검사해야 한다.

1. 활성 member 인지 검사한다.
2. `newMember != 0` 인지 검사한다(`InvalidMemberAddress`).
3. `!members[newMember].is_active` 인지 검사한다(`AlreadyAMember`).
4. 현재 member 수가 `< 255` 인지 검사한다(`MemberIndexOverflow`).
5. `valid_quorum(newQuorum, count + 1)` 인지 검사한다(`InvalidQuorum`).

그다음 `ACTION_ADD_MEMBER` 로 proposal 을 만들어야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:362-378
Observable: state, rpc

[SNET-GOV-071] `proposeRemoveMember(member, newQuorum)` 는 반드시 다음을 순서대로 검사해야 한다.

1. 활성 member 인지 검사한다.
2. `members[member].is_active` 인지 검사한다(`NotAMember`).
3. 현재 member 수가 `> 1` 인지 검사한다(`InvalidQuorum`).
4. `valid_quorum(newQuorum, count - 1)` 인지 검사한다(`InvalidQuorum`).

그다음 `ACTION_REMOVE_MEMBER` 로 proposal 을 만들어야 한다. Member 는 자기 자신의 제거를 propose 해도 된다(MAY).
Source: systemcontracts/solidity/abstracts/GovBase.sol:385-400
Observable: state, rpc

[SNET-GOV-072] `proposeChangeQuorum(newQuorum)` 은 반드시 활성 member 인지와 `valid_quorum(newQuorum, count)` 인지(`InvalidQuorum`)를 검사한 뒤 `ACTION_CHANGE_QUORUM` 으로 proposal 을 만들어야 한다. 현재 quorum 과 같은 값을 propose 하는 것도 허용된다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:407-419
Observable: state, rpc

[SNET-GOV-073] `proposeChangeMaxProposals(newMax)` 는 반드시 활성 member 인지와 `1 <= newMax <= 50` 인지(`InvalidMaxProposals`)를 검사한 뒤 `ACTION_CHANGE_MAX_PROPOSALS` 로 proposal 을 만들어야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:426-432
Observable: state, rpc

[SNET-GOV-074] Action dispatcher 는 네 `GovBase` action 을 반드시 직접 처리해야 한다. 네 action 은 각각 true 를 돌려주거나 revert 한다. Dispatcher 는 그 밖의 모든 action type 을 파생 contract 의 custom action handler 로 넘겨야 한다. 파생 contract 가 모르는 action type 이면 handler 는 false 를 돌려주며, 이 결과를 soft failure 라고 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1109-1130
Source: systemcontracts/solidity/abstracts/GovBase.sol:1160-1163
Observable: state

### Member action 의 실행

[SNET-GOV-075] `ACTION_ADD_MEMBER(a, q)` 를 실행할 때는 반드시 다음을 순서대로 해야 한다.

1. `a` 가 활성이면 `AlreadyAMember` 로 revert 한다.
2. `a == 0` 이면 `InvalidMemberAddress` 로 revert 한다.
3. `member_version` 을 `V' = V + 1` 로 늘린다.
4. 옛 목록에 항목이 255 개 있으면 `MemberIndexOverflow` 로 revert 한다.
5. `valid_quorum(q, old_count + 1)` 이 아니면 `InvalidQuorum` 으로 revert 한다.
6. 옛 목록을 순서대로 version `V'` 로 복사하고, index 를 `1..n` 으로 매긴다.
7. `a` 를 index `n + 1` 로 덧붙인다.
8. `members[a] = Member(True, uint32(ctx.timestamp))` 로 설정한다.
9. `quorum = q` 로 설정한다.
10. `MemberAdded(a, n + 1, q)` 를 내보낸 뒤 `QuorumUpdated(old_quorum, q)` 를 내보낸다. 값이 바뀌지 않아도 `QuorumUpdated` 는 나간다.
11. 파생 hook `on_member_added(a)` 를 호출한다.
12. `quorum_by_version[V'] = q` 로 설정한다.

Source: systemcontracts/solidity/abstracts/GovBase.sol:933-978
Observable: state

[SNET-GOV-076] `ACTION_REMOVE_MEMBER(a, q)` 를 실행할 때는 반드시 다음을 순서대로 해야 한다.

1. `a` 가 활성이 아니면 `NotAMember` 로 revert 한다.
2. `member_version` 을 `V'` 로 늘린다.
3. 새 member 수가 0 이거나 `!valid_quorum(q, old_count - 1)` 이면 `InvalidQuorum` 으로 revert 한다.
4. 옛 목록을 `V'` 로 복사하되 `a` 는 건너뛴다. 나머지 member 의 상대 순서는 유지하고, index 를 `1..n-1` 로 다시 매긴다.
5. `members[a].is_active = False` 로 설정한다. `joined_at` 은 그대로 둔다.
6. `quorum = q` 로 설정한다.
7. `MemberRemoved(a, n - 1, q)` 를 내보낸 뒤 `QuorumUpdated(old_quorum, q)` 를 내보낸다.
8. `on_member_removed(a)` 를 호출한다.
9. `quorum_by_version[V'] = q` 로 설정한다.

Source: systemcontracts/solidity/abstracts/GovBase.sol:1049-1095
Observable: state

[SNET-GOV-077] `ACTION_CHANGE_QUORUM(q)` 를 실행할 때, `valid_quorum(q, len(versioned_member_list[member_version]))` 이 아니면 반드시 `InvalidQuorum` 으로 revert 해야 한다. 유효하면 `quorum = q` 로 설정하고, `QuorumUpdated(old, q)` 를 내보내고, `quorum_by_version[member_version] = q` 로 설정해야 한다. Member version 은 바뀌지 않는다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:991-1009
Observable: state

[SNET-GOV-078] `ACTION_CHANGE_MAX_PROPOSALS(m)` 을 실행할 때, `1 <= m <= 50` 이 아니면 반드시 `InvalidMaxProposals` 로 revert 해야 한다. 범위 안이면 `max_active_proposals_per_member = m` 으로 설정하고 `MaxProposalsPerMemberUpdated(old, m)` 을 내보내야 한다. 최댓값을 낮춰도 기존 proposal 에는 영향이 없다. 다만 어떤 member 의 counter 가 새 최댓값 이상인 동안에는 그 member 가 새 proposal 을 만들 수 없다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:1022-1031
Source: systemcontracts/solidity/abstracts/GovBase.sol:691-693
Observable: state

### 직접 주소 변경

[SNET-GOV-080] Hook 은 반드시 다음 지점에서만 호출되어야 한다. `on_member_added` 는 member 추가의 log 가 나간 뒤, `quorum_by_version` 을 쓰기 전에 호출한다. `on_member_removed` 는 member 제거의 log 가 나간 뒤, `quorum_by_version` 을 쓰기 전에 호출한다. `on_member_changed` 는 `MemberChanged` 뒤에 호출한다. `on_proposal_finalized` 는 `finalize_proposal` 안에서 호출한다. Hook 이 파생 state 에 주는 효과는 같은 원자적 호출의 일부다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:974-977
Source: systemcontracts/solidity/abstracts/GovBase.sol:1091-1094
Source: systemcontracts/solidity/abstracts/GovBase.sol:489-493
Source: systemcontracts/solidity/abstracts/GovBase.sol:1259-1260
Observable: state

---

## 7. GovBase view

[SNET-GOV-090] 읽기 전용 함수는 반드시 아래 값을 돌려줘야 한다. 이 값은 `eth_call` 로 관찰할 수 있으며, mirror 를 교차 확인할 때 권장하는 인터페이스다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:504-657
Observable: rpc

| 함수 | 결과 |
|---|---|
| `getProposal(pid)` | 저장된 proposal 을 돌려준다. `pid` 가 무효이면 `InvalidProposal` 로 revert 한다 |
| `isProposalInVoting(pid)` | `status == Voting and not expired` 의 결과를 돌려준다 (Approved 이면 false 다). `pid` 가 무효이면 `InvalidProposal` 로 revert 한다 |
| `isProposalExecutable(pid)` | `status == Approved and not expired and approved >= required` 의 결과를 돌려준다. attempt counter 는 보지 않는다. `pid` 가 무효이면 `InvalidProposal` 로 revert 한다 |
| `canExecuteProposal(pid)` | `(InvalidProposalId, 0)` / `(NotApproved, 0)` / `(Expired, 0)` / `(TooManyAttempts, 0)` / `(Executable, 3 - count)` 를 이 순서로 검사해 돌려준다. 결코 revert 하지 않는다 |
| `hasApproved(member, pid)` | snapshot 에서 `member` 의 비트가 켜져 있는지 돌려준다. 찬성**과** 반대 모두 비트를 켠다. `pid` 가 무효이면 `InvalidProposal` 로 revert 한다 |
| `getMemberCount(v)`, `getMemberAt(v, i)`, `isMember(a, v)` | snapshot 조회다. `1 <= v <= member_version` 이 아니면 `InvalidMemberVersion` 으로 revert 한다. `getMemberAt` 은 `IndexOutOfBounds` 로 revert 할 수 있다 |
| `getQuorum(v)` | `quorum_by_version[v]` 를 돌려준다. 값이 0 이면 `InvalidQuorum` 으로 revert 한다 |
| `getMemberActiveProposalCount(a)`, `canCreateProposal(a)` | counter 값과 `counter < max_active_proposals_per_member` 의 결과를 돌려준다 |
| public getter | `proposalExpiry`, `memberVersion`, `currentProposalId`, `quorum`, `members(a)`, `versionedMemberList(v, i)`, `proposals(pid)` (필드 11개를 선언 순서대로 돌려주며, `callData` 도 `bytes` 로 포함한다. `pid` 는 검사하지 않는다), `proposalExecutionCount(pid)`, `memberActiveProposalCount(a)`, `maxActiveProposalsPerMember` |

---

## 8. GovValidator

`GovValidator` 는 validator candidate, 그 BLS public key, gas tip 을 보관한다. 각 활성 member(여기서는 *operator* 라고 부른다)는 proposal 없이 validator 주소 하나와 그 BLS 키를 직접 등록해도 된다. Validator 는 operator 가 제거되거나 operator 가 validator 를 교체할 때에만 빠진다.

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

[SNET-GOV-100] `validators` 는 반드시 OpenZeppelin `EnumerableSet.AddressSet` 처럼 동작해야 한다. `add(x)` 는 `x` 가 없으면 끝에 덧붙이고, 있으면 아무것도 하지 않는다. `remove(x)` 는 위치 `i` 에 있는 원소를 지울 때 마지막 원소를 위치 `i` 로 옮기고 목록 길이를 하나 줄인다. 이 제거 방식을 swap-and-pop 이라고 한다. `values()` 는 storage 순서대로 목록을 돌려준다. 노드가 이 순서대로 목록을 읽어 candidate 순서로 쓰므로, storage 순서는 합의에 영향을 준다(SNET-GOV-115).
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

[SNET-GOV-101] `configureValidator(newValidator, blsKey, blsSig)` 는 반드시 proposal 없이 모든 활성 member 가 호출할 수 있어야 하며, 반드시 다음을 순서대로 검사해야 한다.

1. 활성 member 인지 검사한다(`NotAMember`).
2. `newValidator != 0` 인지 검사한다(`InvalidValidator`).
3. `validator_to_operator[newValidator]` 가 0 이거나 `sender` 인지 검사한다(`AlreadyValidatorExists`).
4. SNET-GOV-123 의 BLS 검사를 한다.
5. `old = operator_to_validator[sender]` 라고 할 때, `bls_key_to_validator[blsKey]` 가 0 이거나 `old` 인지 검사한다(`AlreadyRegisteredBlsKey`).

Source: systemcontracts/solidity/v1/GovValidator.sol:64-77
Source: systemcontracts/test/gov_validator_test.go:219-311
Observable: state, rpc

[SNET-GOV-102] 경우 1 은 `old == 0` 인 경우, 곧 처음 등록하는 경우다. 이 경우 contract 는 반드시 `validators.add(newValidator)` 로 `newValidator` 를 목록 끝에 덧붙여야 한다. 그리고 두 operator mapping 을 설정하고, `validator_to_bls_key[newValidator] = blsKey` 와 `bls_key_to_validator[blsKey] = newValidator` 를 설정해야 한다.
Source: systemcontracts/solidity/v1/GovValidator.sol:82-84
Source: systemcontracts/solidity/v1/GovValidator.sol:97-107
Observable: state

[SNET-GOV-103] 경우 2 는 `old == newValidator` 인 경우, 곧 키만 바꾸는 경우다. 이 경우 `bls_key_to_validator[blsKey] == old` 이면, 곧 이미 등록된 같은 키를 다시 보내면, contract 는 반드시 `NoConfigurationChanging` 으로 revert 해야 한다. 그렇지 않으면 contract 는 `bls_key_to_validator[validator_to_bls_key[old]]` 를 지우고 새 키를 양방향 mapping 에 설정해야 한다. `validators` 에서 `old` 의 위치는 바뀌지 않는다.
Source: systemcontracts/solidity/v1/GovValidator.sol:85-90
Source: systemcontracts/solidity/v1/GovValidator.sol:109-112
Observable: state

[SNET-GOV-104] 경우 3 은 `old != 0` 이고 `old != newValidator` 인 경우, 곧 validator 를 교체하는 경우다. 이때 키는 같아도 되고 새 키여도 된다. 이 경우 contract 는 반드시 먼저 `old` 를 완전히 제거해야 한다. 제거 단계에서는 swap-and-pop 으로 `validators.remove(old)` 를 하고, `validator_to_operator[old]`, `operator_to_validator[sender]`, `bls_key_to_validator[validator_to_bls_key[old]]`, `validator_to_bls_key[old]` 를 지운다. 그다음 경우 1 과 같이 `newValidator` 를 등록해야 한다. 그러므로 교체는 operator 의 validator 를 목록 끝으로 옮기고, 원래 마지막에 있던 validator 를 비워진 위치로 옮긴다.
Source: systemcontracts/solidity/v1/GovValidator.sol:91-94
Source: systemcontracts/solidity/v1/GovValidator.sol:114-126
Source: systemcontracts/test/gov_validator_test.go:388-458
Observable: state

[SNET-GOV-107] (withdrawn; informative) 이 절의 연산(SNET-GOV-101 부터 SNET-GOV-104, SNET-GOV-109)과 SNET-GOV-013 에서 유도되는 genesis 이후의 불변식이다. `v = operator_to_validator[o] != 0` 인 모든 operator `o` 에 대해 다음이 성립한다. `validator_to_operator[v] == o` 이고, `v` 는 `validators` 에 있고, `bls_key_to_validator[validator_to_bls_key[v]] == v` 이고, `o` 는 활성 member 다. 따라서 `len(validators) <= ` 활성 member 수 `<= 255` 이다.
Source: systemcontracts/solidity/v1/GovValidator.sol:97-146
Source: systemcontracts/gov_validator.go:113-167

### Member hook

[SNET-GOV-109] `on_member_changed(old, new)` 는 `operator_to_validator[old] != 0` 이면 반드시 `operator_to_validator[new]` 를 `operator_to_validator[old]` 의 값으로 설정하고, `validator_to_operator[validator] = new` 로 설정하고, `operator_to_validator[old]` 를 지워야 한다. Validator 의 위치, 키, `validators` 소속은 바뀌지 않는다.
Source: systemcontracts/solidity/v1/GovValidator.sol:139-146
Observable: state

[SNET-GOV-110] `on_member_added(m)` 은 반드시 아무 효과도 없어야 한다. 새 member 는 `configureValidator` 를 호출하기 전까지 validator 를 갖지 않는다.
Source: systemcontracts/solidity/v1/GovValidator.sol:135-137
Observable: state

### Gas tip

[SNET-GOV-112] `SET_GAS_TIP(t)` 를 실행하면 반드시 같은 값인지 다시 검사하지 않고 `gas_tip = t` 로 설정해야 한다. 그리고 `GasTipUpdated(old, t, updater)` 를 내보낸 뒤 true 를 돌려줘야 한다. 여기서 `updater` 는 attempt 를 일으킨 호출의 `msg.sender`, 곧 마지막 찬성자나 실행자다. 목표 값이 같은 두 proposal 은 둘 다 실행될 수 있다.
Source: systemcontracts/solidity/v1/GovValidator.sol:148-155
Source: systemcontracts/solidity/v1/GovValidator.sol:186-190
Observable: state

[SNET-GOV-113] (withdrawn; informative)
Source: systemcontracts/solidity/v1/GovValidator.sol:148-155
Source: systemcontracts/solidity/abstracts/GovBase.sol:1109-1130

### View 와 노드의 읽기

[SNET-GOV-114] `validatorList()` 는 반드시 `validators.values` 를 storage 순서대로 돌려줘야 한다. `isValidator(a)`, `validatorCount()`, public mapping, `gasTip()` 은 저장된 값을 돌려준다. `getGasTipGwei()` 는 `gas_tip // 10**9` 를 돌려준다.
Source: systemcontracts/solidity/v1/GovValidator.sol:52-62
Source: systemcontracts/solidity/v1/GovValidator.sol:192-194
Observable: rpc

[SNET-GOV-115] 노드는 `GovValidator` storage 를 호출이 아니라 직접 읽는다. 노드가 읽는 값은 반드시 위에서 정의한 값이어야 하며, 그 값은 세 가지다. 첫째, 순서가 있는 validator 목록을 다음 epoch 의 candidate 목록으로 읽는다. 둘째, 선택된 각 validator 의 BLS 키를 읽는다. 키가 비어 있으면 그 validator 는 그 epoch 에서 빠진다. 셋째, 각 block 의 parent state 에서 `gas_tip` 을 읽어 header 의 `GasTip` 필드에 쓴다. 각 읽기가 어느 state 에서 일어나는지는 `B-08` 과 `B-06` 에서 정한다. 이 장은 목록 순서, 키 바이트, tip 값이 위에서 정의한 값이라는 것만 고정한다.
Source: consensus/wbft/engine/engine.go:606-612
Source: consensus/wbft/engine/engine.go:806-810
Source: consensus/wbft/engine/engine.go:890-903
Source: consensus/wbft/engine/engine.go:622-646
Source: consensus/wbft/engine/engine.go:1280-1294
Source: miner/worker.go:1218
Observable: header, state

---

## 9. BLS proof-of-possession precompile

[SNET-GOV-120] Anzeon 규칙이 적용되는 동안 주소 `0x0000000000000000000000000000000000B00001` 은 반드시 precompile contract(`blsPoP`)여야 하며, Anzeon precompile 집합과 Boho precompile 집합 양쪽에 있어야 한다. `GovValidator` 는 자기 `blsPoP` 필드에 저장된 주소를 호출하며, genesis 는 이 필드를 이 주소로 설정한다.
Source: params/protocol_params.go:208
Source: core/vm/contracts.go:127-139
Source: core/vm/contracts.go:141-155
Source: systemcontracts/gov_validator.go:57-62
Observable: state

[SNET-GOV-121] Precompile 입력은 반드시 정확히 144 바이트여야 한다. 입력은 48 바이트 압축 BLS12-381 G1 public key 와 그 뒤의 96 바이트 압축 G2 signature 로 이루어진다. 다른 길이의 입력은 반드시 호출을 실패시켜야 한다. 이때 precompile 이 error 를 돌려주므로, 호출한 `staticcall` 은 `success = false` 를 돌려준다.
Source: core/vm/contracts.go:1196-1205
Observable: state

[SNET-GOV-123] `GovValidator._checkBlsKey` 는 반드시 다음을 순서대로 검사해야 한다.

1. 키 길이가 48 인지 검사한다(`InvalidBlsKeyLength`).
2. signature 길이가 96 인지 검사한다(`InvalidSignatureLength`).
3. `staticcall(bls_pop, key ‖ sig)` 가 성공하는지 검사한다(`FailedToVerifyBlsKey`).
4. 돌려받은 word 가 true 로 디코딩되는지 검사한다(`InvalidBlsKey`).

따라서 인코딩이 잘못되었거나, 부분군에 들지 않거나, 무한원점이어서 precompile 이 실패하면 `_checkBlsKey` 는 `FailedToVerifyBlsKey` 로 revert 한다. 형식은 맞지만 틀린 signature 이면 `_checkBlsKey` 는 `InvalidBlsKey` 로 revert 한다.
Source: systemcontracts/solidity/v1/GovValidator.sol:157-172
Source: systemcontracts/test/gov_validator_test.go:256-298
Observable: rpc

[SNET-GOV-124] BLS 키의 유일성(`AlreadyRegisteredBlsKey`)은 정확한 바이트 문자열로 판단한다. 같은 group element 에 대한 서로 다른 두 48 바이트 문자열이 둘 다 precompile 을 통과할 수는 없다. 압축 해제는 점마다 인코딩 하나만 받아들이기 때문이다(`A-02` WBFT-CRYPTO-056). 그래서 바이트 문자열로 판단한 유일성은 키의 유일성과 같다. Native 모듈은 키를 스스로 정규화하지 말고 반드시 같은 decoder(A-02)를 써야 한다.
Source: systemcontracts/solidity/v1/GovValidator.sol:46
Source: systemcontracts/solidity/v1/GovValidator.sol:75-77
Source: crypto/bls/blst/public_key.go:44-48
Observable: state

> 해설: Go 구현은 같은 `blst` binding 을 쓰면 된다. Rust 로 옮기면서 다른 BLS 라이브러리를 쓴다면, 비정규 인코딩을 어떻게 처리하는지 따로 시험해야 한다.

---

## 10. GovCouncil 과 AccountManager native manager

`GovCouncil` 은 genesis 이후 계정의 blacklist 비트와 authorized 비트를 켜는 유일한 수단이다. `GovCouncil` 은 자기 storage 에 주소 목록 두 개를 두고, 목록이 바뀔 때마다 `AccountManager` native manager 를 호출해 그 변경을 계정 `Extra` 비트에 반영한다. 노드와 EVM 은 비트만 집행한다(B-07). 합의 엔진은 parent state 에서 `Coinbase` 가 blacklist 된 block 을 거부한다(`B-08` SNET-SRC-020, `A-08` 단계 H15b).

### State 와 주소 집합 라이브러리

```python
class GovCouncilState:
    base: GovBaseState
    blacklist: StrictAddressSet          # AddressSetLib
    authorized: StrictAddressSet
    account_manager: Address             # genesis: 0x...B00003
```

[SNET-GOV-130] `GovCouncil` 의 목록은 반드시 `AddressSetLib.AddressSet` 처럼 동작해야 한다. `add(x)` 는 `x` 가 0 이면 `ZeroAddressNotAllowed` 로 revert 하고, 이미 있으면 `AddressAlreadyExists` 로 revert 하며, 그 밖에는 끝에 덧붙인다. `remove(x)` 는 없으면 `AddressNotFound` 로 revert 하고, 있으면 swap-and-pop 을 한다. `contains(0)` 은 항상 false 다. `EnumerableSet` 과 달리 잘못된 원소의 add 와 remove 는 아무것도 하지 않는 대신 revert 한다. 그러나 `GovCouncil` 은 먼저 `contains` 를 검사하므로 이 revert 에 도달하지 않는다.
Source: systemcontracts/solidity/libraries/AddressSetLib.sol:83-166
Source: systemcontracts/solidity/v1/GovCouncil.sol:64-66
Observable: state

### Proposal 함수

[SNET-GOV-131] Proposal 함수는 반드시 활성 member 검사 뒤에 아래 조건을 현재 목록에 대해 검사하고, 그다음 그 함수에 대응하는 action 과 call data 로 proposal 을 만들어야 한다. Batch 안의 중복은 거부하지 않으며, 빈 batch 도 받아들인다.
Source: systemcontracts/solidity/v1/GovCouncil.sol:133-303
Observable: state, rpc

| 함수 | 검사 (순서대로, batch 는 원소마다) | Error |
|---|---|---|
| `proposeAddBlacklist(a)` | `a != 0`; `a` 가 blacklist 에 없다 | `ZeroAddressNotAllowed`, `AlreadyInBlacklist` |
| `proposeRemoveBlacklist(a)` | `a` 가 blacklist 에 있다 | `NotInBlacklist` |
| `proposeAddBlacklistBatch(as)` | 원소마다: `!= 0`, blacklist 에 없다 | 위와 같다 |
| `proposeRemoveBlacklistBatch(as)` | 원소마다: blacklist 에 있다 | `NotInBlacklist` |
| `proposeAddAuthorizedAccount(a)` / batch | `a != 0`; authorized 목록에 없다 | `ZeroAddressNotAllowed`, `AlreadyInAuthorizedAccountList` |
| `proposeRemoveAuthorizedAccount(a)` / batch | authorized 목록에 있다 | `NotInAuthorizedAccountList` |

### 실행

[SNET-GOV-133] 계정 `a` 에 대한 단일 add/remove action 을 실행할 때는 반드시 다음을 해야 한다.

1. 목록을 다시 검사한다. Add action 인데 `a` 가 이미 목록에 있거나, remove action 인데 `a` 가 목록에 없으면, `ProposalExecutionSkipped(a, current_proposal_id, reason)` 을 내보내고 false 를 돌려준다. Reason 은 `"ALREADY_BLACKLISTED"`, `"NOT_IN_BLACKLIST"`, `"ALREADY_AUTHORIZED"`, `"NOT_AUTHORIZED"` 가운데 하나다.
2. 짝이 맞는 method(`blacklist`, `unBlacklist`, `authorize`, `unAuthorize`)로 `AccountManager` 를 호출한다.
3. 그 호출이 실패하면 `ProposalExecutionSkipped(a, current_proposal_id, "GovCouncil: <method> call failed")` 를 내보내고 false 를 돌려준다.
4. 호출이 성공하면 목록을 갱신하고, 짝이 맞는 event(`AddressBlacklisted`, `AddressUnblacklisted`, `AuthorizedAccountAdded`, `AuthorizedAccountRemoved`)를 `(a, current_proposal_id)` 로 내보낸 뒤 true 를 돌려준다.

Source: systemcontracts/solidity/v1/GovCouncil.sol:379-496
Observable: state

[SNET-GOV-134] Batch action 을 실행할 때는 반드시 각 원소에 단일 원소 절차를 순서대로 적용하고, 각 원소의 반환값은 무시하고, true 를 돌려줘야 한다. 그러므로 모든 원소를 건너뛰어도 batch 는 `Executed` 로 끝나고, 중복 원소는 두 번째로 나올 때 건너뛴다.
Source: systemcontracts/solidity/v1/GovCouncil.sol:326-361
Observable: state

[SNET-GOV-136] (withdrawn; informative) SNET-GOV-051 부터 SNET-GOV-055 에서 따라 나오는 결과다. False 를 돌려준 단일 계정 action 은 proposal 을 retry 모드의 `Approved` 에 남긴다(SNET-GOV-055). 그래서 중복되었거나 낡은 blacklist proposal 은 세 가지 방법으로 닫을 수 있다. 첫째는 `executeWithFailure` 를 호출하는 것이다. 둘째는 counter 에 세어지는 attempt 세 번 뒤에 네 번째 attempt 를 하는 것이며, 이 네 번째 attempt 가 proposal 을 `Failed` 로 만든다. 셋째는 proposal 이 만료되는 것이다. 닫히기 전까지는 이후의 attempt(`executeProposal`, `executeWithFailure`, 추가 찬성)가 현재 목록을 기준으로 action 을 다시 실행한다. 그 사이에 목록이 바뀌었으면 action 이 실행된다. 예를 들어 다른 proposal 이 제거한 주소가 다시 blacklist 된다.
Source: systemcontracts/solidity/v1/GovCouncil.sol:379-385
Source: systemcontracts/test/gov_council_toctou_test.go:76-101

### AccountManager

[SNET-GOV-137] Anzeon 규칙이 적용되는 동안 `0x0000000000000000000000000000000000B00003` 은 반드시 native manager 여야 하며, 입력의 첫 4 바이트로 method 를 고른다. Method 는 state 를 바꾸는 `blacklist(address)`, `unBlacklist(address)`, `authorize(address)`, `unAuthorize(address)` 와, view 인 `isBlacklisted(address)`, `isAuthorized(address)` 다. 입력은 반드시 정확히 4 + 32 바이트여야 하며, 주소는 word 의 낮은 20 바이트다. 알 수 없는 selector, 틀린 길이, context 위반은 반드시 호출을 실패시켜야 한다.
Source: params/protocol_params.go:220
Source: core/vm/native_manager.go:82-111
Source: core/vm/native_manager.go:137-156
Observable: state

[SNET-GOV-138] State 를 바꾸는 네 method 는 `CALL` opcode 로 호출되고 caller 가 genesis chain configuration 의 `GovCouncil` 주소일 때가 아니면 반드시 실패해야 한다. `STATICCALL`, `DELEGATECALL`, `CALLCODE` 로 호출하면 실패한다. 성공하면 대상 계정 `Extra` 필드의 bit 63(blacklisted)이나 bit 62(authorized)를 켜거나 끈다. 존재하지 않는 계정에 비트를 켜면 그 계정이 생긴다. `Extra` 가 0 이 아닌 계정은 빈 계정이 아니기 때문이다. 켜져 있지 않은 비트를 끄면 계정을 touch 만 하므로, 빈 계정은 EIP-161 정리로 다시 제거된다. View method 는 모든 caller 와 context 를 받아들이며 32 바이트 boolean word 를 돌려준다.
Source: core/vm/native_manager.go:288-458
Source: core/vm/native_manager.go:470-489
Source: core/types/state_account_extra.go:31-45
Source: core/state/statedb.go:434-460
Source: core/state/state_object.go:94-96
Source: core/state/state_object.go:524-560
Observable: state

---

## 11. Minting contract (범위 한정)

Minting contract 는 합의에 영향을 주지 않는다. 합의 엔진과 header 검증은 이 contract 들의 storage 를 읽지 않으며, 이 contract 들은 validator, BLS 키, gas tip, 계정 `Extra` 비트를 바꿀 수 없다. 이 contract 들은 `NativeCoinManager` 를 통해 balance 를 바꾸고, 그래서 state root 에 영향을 준다. 노드는 EVM 에서 이 contract 들을 실행해 그 state root 를 얻는다. 합의 전용 검증을 위한 native validator 모듈은 이 contract 들을 구현할 필요가 없다. 아래 요약은 state 변경을 알아보고 test vector 를 쓰는 데 필요한 수준으로 의미를 적는다. 표시한 곳을 빼면 이 요약은 informative 다.

[SNET-GOV-150] 합의 전용 validator 모듈은 `GovMasterMinter`, `GovMinter`, `NativeCoinAdapter` 를 구현하지 않아도 된다(NEED NOT). 완전한 state 동등성을 주장하는 모듈은 반드시 이 contract 들을 다시 구현하지 말고 EVM code 로 실행해야 한다.
Source: consensus/wbft/engine/engine.go:606-646
Source: core/vm/native_manager.go:463-468
Observable: state

**GovMasterMinter** (`0x1002`, v1). Fiat token(`NativeCoinAdapter`)의 minter 집합을 관리하는 `GovBase` instance 다. `NativeCoinAdapter` 의 `masterMinter` 가 이 contract 다. Action 은 `CONFIGURE_MINTER(minter, allowance)`(`0 < allowance <= maxMinterAllowance`), `REMOVE_MINTER(minter)`, `UPDATE_MAX_MINTER_ALLOWANCE(limit > 0)`, `PAUSE`, `UNPAUSE` 다. 각 action 은 실행 시점에 다시 검사하고, 조건을 어기면 revert 한다. Configure 와 remove 는 pause 상태에서 막히며, `fiatToken.configureMinter` / `removeMinter` 를 호출하고 그 호출이 실패하면 revert 한다. False 를 돌려주는 soft failure 는 모르는 action 에서만 생긴다. (Source: `systemcontracts/solidity/v1/GovMasterMinter.sol:118-350`.)

**GovMinter v1** (`0x1003`). Member 들이 off-chain 증명을 근거로 mint 와 burn 을 propose 하는 `GovBase` instance 다. `proposeMint(proof)` 는 다음을 요구한다. Pause 상태가 아니어야 하고, 수혜자와 금액이 0 이 아니어야 하고, `depositId` 와 `bankReference` 가 비어 있지 않아야 한다. 또한 `amount <= minterAllowance(this) - reservedMintAmount` 여야 하며, 이 뺄셈은 underflow 를 검사한다. `depositId` 는 사용되지 않았어야 한다. 곧 그 `depositId` 는 실행된 적이 없고 살아 있는 proposal 에 붙어 있지도 않아야 한다. Proof hash 도 사용되지 않았어야 한다. 조건을 만족하면 `_createProposal` 이 돌아온 뒤에 그 금액을 예약한다(`reservedMintAmount += amount`, `mintProposalAmounts[pid] = amount`). `proposeBurn(proof)` 는 payable 이며, `proof.from == sender` 와 `msg.value == amount` 를 요구한다. 이 함수는 `burnBalance[sender]` 에 금액을 적립하고 burn 을 기록한다. 실행은 `try fiatToken.mint/burn` 을 쓴다. 실행이 잡은 실패는 false 를 돌려준다. 이 결과는 soft failure 이며, attempt counter 에 세어진다. Pause 상태이면 실행은 revert 한다. `_onProposalFinalized` 는 mint 예약을 해제한다. `proposeBurn` 도 자신의 기록(`withdrawalIdToProposalId`, `burnProposals`)을 `_createProposal` 뒤에 한다. (Source: `systemcontracts/solidity/v1/GovMinter.sol:202-317`, `377-525`, `700-760`, `875-920`.)

**GovMinter v2** (Boho upgrade, code 만 바뀐다). v2 는 다음을 바꾼다. `refundableBalance` 를 추가하고, `burnBalance` 적립을 검사 뒤로 옮기고, 뺄셈 앞에 `if totalAllowance <= reservedMintAmount revert InsufficientMinterAllowance()` 를 추가한다. 또한 실행되지 않은 burn proposal 을 finalize 할 때 예치금을 `burnBalance` 에서 `refundableBalance` 로 옮기며, 이때 종료 log 앞에 `BurnDepositRefunded` 를 내보낸다. `claimBurnRefund()` 는 그 금액을 caller 에게 지급한다. v1 에서 묶인 예치금은 옮겨지지 않는다. (Source: `systemcontracts/solidity/v2/GovMinter.sol` diff against v1: `:133-135`, `:226`, `:296-308`, `:350-364`, `:937-964`.)

**NativeCoinAdapter** (`0x1000`). Native balance 위에 올린 ERC-20/EIP-2612/EIP-3009 facade 다. `balanceOf` 는 계정 balance 다. `mint`/`burn`/`transfer*` 는 `NativeCoinManager`(`0x…B00002`)를 호출하며, 이 manager 는 genesis `NativeCoinAdapter` 주소에서 온 `CALL` 만 받아들인다. `mint` 는 `minterAllowed` 가 충분한 minter 를 요구하고, minter 와 수신자가 모두 blacklist 되지 않았어야 한다(`AccountManager` 로 조회한다). `burn` 은 caller 자신의 native balance 를 태운다. `totalSupply` 는 mint 와 burn 만 갱신하는 counter 다. 이 token 은 native coin 이므로 서명만으로(EIP-2612, EIP-3009, ERC-1271) 옮겨질 수 있다. 그 보안 결과는 `B-04` §13 에 있다. (Source: `systemcontracts/solidity/v1/NativeCoinAdapter.sol:53-180`, `systemcontracts/solidity/abstracts/Mintable.sol:52-121`, `core/vm/native_manager.go:463-468`.)

[SNET-GOV-176] (artifact 동작을 다시 적은 것) 호출된 함수에 대해 아래 표에 나열한 주소가 실행 시점에 bit 63 을 가지면, `NativeCoinAdapter` v1 은 반드시 서명 검사나 state 변경 전에 `"NativeCoinAdapter: account is blacklisted"` 로 revert 해야 한다. `mint` 와 `burn` 에서는 minter 검사가 먼저 실행된다. 표에 없는 caller 는 EVM 의 호출 수준 검사(`B-07` SNET-TX-043)가 다루고, 트랜잭션 송신자는 `B-07` SNET-TX-040 이 다룬다.
Source: systemcontracts/solidity/v1/NativeCoinAdapter.sol:86-101 (notBlacklisted, AccountManager.isBlacklisted), systemcontracts/solidity/v1/NativeCoinAdapter.sol:139-204, systemcontracts/solidity/v1/NativeCoinAdapter.sol:245-280, systemcontracts/solidity/v1/NativeCoinAdapter.sol:320-477
Source: core/vm/evm.go:614-627
Observable: state

| 함수 | contract 가 검사하는 주소 | contract 가 검사하지 않는 주소 |
|---|---|---|
| `mint` | `msg.sender`, `_to` | — |
| `burn` | `msg.sender` | — |
| `transfer` | `msg.sender`, `to` | — |
| `transferFrom` | `msg.sender`, `from`, `to` | — |
| `approve`, `increaseAllowance`, `decreaseAllowance` | `msg.sender`, `spender` | — |
| `permit` (두 형태) | `owner`, `spender` | `msg.sender` (EVM 이 검사한다) |
| `transferWithAuthorization`, `receiveWithAuthorization` (두 형태) | `from`, `to` | `msg.sender` (EVM 이 검사한다) |
| `cancelAuthorization` (두 형태) | 없음 | `authorizer` |

artifact 를 실행해 확인했다. `owner` 가 bit 63 을 가진 `permit` 은 서명을 보기 전에 위 메시지로 revert 했다. bit 63 을 가진 `authorizer` 의 `cancelAuthorization` 은 성공했고 `authorizationState` 를 true 로 만들었다. 검사는 실행 시점에만 하므로, 계정이 blacklist 되기 전에 만든 allowance 와 서명은 계정이 blacklist 에서 빠지면 다시 쓸 수 있다 (`B-04` §13.3).

---

## 12. Genesis 가 초기화한 state (요약)

이 contract 들에는 initializer 함수가 없다. Genesis 가 storage 를 직접 쓴다. 그 절차는 B-02 가 정하고, slot 은 B-04 가 정한다. Mirror 는 반드시 정확히 그 state 에서 시작해야 하며, 여기에는 Solidity 에 선언된 초기값과 다른 값도 포함된다.

[SNET-GOV-160] 각 instance 의 초기 `GovBase` state 는 반드시 다음과 같아야 한다.

1. `quorum` 은 `quorum` parameter 이고, 없으면 0 이다.
2. `proposal_expiry` 는 `expiry` parameter 이고, 없으면 0 이다.
3. `member_version` 은 `memberVersion` parameter 이다. `members` 가 주어지면 이 parameter 는 필수다. `members` 가 없으면 `member_version` 은 0 이다.
4. `versioned_member_list[member_version]` 은 중복을 제거한 `members` 목록을 parameter 순서대로 담는다. 각 member 의 `joined_at` 은 0 이다.
5. `quorum > 0` 일 때에만 `quorum_by_version[member_version] = quorum` 이다.
6. `max_active_proposals_per_member` 는 `maxProposals` 이고, 없으면 3 이다.
7. `current_proposal_id = 0` 이다.

Source: systemcontracts/gov_base.go:135-336
Observable: state

[SNET-GOV-161] 초기 `GovValidator` state 는 반드시 추가로 다음과 같아야 한다. `bls_pop = 0x…B00001` 이다. `gas_tip` 은 `gasTip` parameter 이고, 없으면 `InitialGasTip`(27,600,000,000,000 wei)이다. `validators` 가 주어지면, parameter 순서대로 각 index `i` 에 대해 `validators.add(validators[i])`, `operator_to_validator[members[i]] = validators[i]`, `validator_to_operator[validators[i]] = members[i]` 를 적용하고, `blsPublicKeys[i]` 로 키 mapping 을 설정한다. 이때 이미 나온 validator 주소는 건너뛴다. 이 짝짓기에 쓰는 `members` 목록은 중복을 제거하지 않은 parameter 원본 목록이다. Genesis 에서는 PoP 검사, 키 길이 검사, 키 유일성 검사를 하지 않는다.
Source: systemcontracts/gov_validator.go:52-184
Source: params/protocol_params.go:138
Observable: state

[SNET-GOV-162] 초기 `GovCouncil` state 는 반드시 추가로 다음과 같아야 한다. `account_manager = 0x…B00003` 이다. 그리고 genesis allocation 을 쓸 수 있을 때에만, blacklist 목록과 authorized 목록은 parameter 목록과, allocation 의 `Extra` 에서 대응하는 비트가 켜진 계정을 합친 집합이다. 이 목록은 주소의 바이트 오름차순으로 저장되며, allocation 의 그 계정들에 비트가 켜진다. 그 가운데 genesis 시스템 컨트랙트 주소이기도 한 주소가 있으면, 그 뒤에 `inject_contracts` 가 allocation 항목을 새 항목으로 바꾸므로(`B-02` SNET-GEN-012, SNET-GEN-014; `B-04` SNET-SYS-011), 그 주소는 목록에는 있지만 `Extra = 0` 이다.
Source: systemcontracts/gov_council.go:68-156
Source: systemcontracts/gov_council.go:163-211
Source: core/genesis.go:750-752
Observable: state

> 해설: Constructor 가 실행되지 않으므로 Solidity 소스의 초기값(예: `INITIAL_MEMBER_VERSION = 1`)을 믿고 mirror 를 초기화하면 틀린다. Mirror 는 genesis 가 실제로 쓴 storage 에서 시작해야 한다.

---

## 13. Native 모듈의 동등성

### 시나리오 test vector

아래 vector 는 각 sequence 를 reference contract 에서 실행해 만든다. 실행 환경은 `systemcontracts/test` 의 simulated backend harness 이다(예: `NewGovWBFT`, `systemcontracts/test/gov_base_versioned_membership_test.go:52-86`). 단계마다 다음을 기록한다. 송신자, 호출, 인자, block timestamp 를 기록하고, 성공인지 아니면 custom error 로 revert 했는지를 기록하고, log 를 기록한다. 또한 각 단계 뒤의 합의 투영과, 건드린 instance 의 `GovBase` state 를 기록한다. 모듈은 모든 단계가 일치할 때 그 vector 를 통과한다. 따로 말하지 않으면, 시작 상태는 member 4 명 `A, B, C, D` 와 그 순서의 validator `vA..vD`, quorum 3, 만료 604800 초, `maxProposals` 3, gas tip `T0` 이다.

| Id | Sequence | 기대 결과 (핵심 사실) |
|---|---|---|
| V-01 | `A.proposeGasTip(T1)`; `B.approve(1)`; `C.approve(1)` | 3 단계에서 `Approved` 를 거쳐 `Executed` 가 되고, `gas_tip = T1`, `GasTipUpdated(T0, T1, C)` 이다. Log 순서는 `ProposalVoted, ProposalApproved, GasTipUpdated, ProposalExecuted` 다 |
| V-02 | `A.proposeGasTip(T0)` | `SameGasTip` 으로 revert 한다 |
| V-03 | `A` 와 `B` 가 각각 `T1` proposal 을 만들고, 둘 다 찬성을 받는다 | 둘 다 `Executed` 가 된다. `GasTipUpdated` 가 두 번 나가며, 두 번째는 `old = new = T1` 이다 |
| V-04 | 새 member `E`: `A.proposeAddMember(E, 3)` 후 quorum 까지 찬성; 이어서 `E.configureValidator(vE, kE, popE)` | `member_version = 2` 이다. validator 는 `[vA, vB, vC, vD, vE]` 이다. `configureValidator` 는 log 를 내지 않는다 |
| V-05 | `B.configureValidator(vX, kB, popB)` (교체, 같은 키) | validator 는 `[vA, vD, vC, vX]` 이다 (swap-and-pop 뒤 덧붙이기) |
| V-06 | `C.configureValidator(vC, kC2, popC2)` (키 변경) | 순서는 바뀌지 않는다. `bls_key_to_validator[kC] = 0` 이다 |
| V-07 | `A.configureValidator(vA, kA, popA)` | `NoConfigurationChanging` 으로 revert 한다 |
| V-08 | `A.configureValidator(vB, …)` / `(…, kB, …)` / 틀린 PoP / 형식이 잘못된 키 | 각각 `AlreadyValidatorExists` / `AlreadyRegisteredBlsKey` / `InvalidBlsKey` / `FailedToVerifyBlsKey` 다 |
| V-09 | `A.proposeRemoveMember(B, 2)` 후 quorum 까지 찬성 | validator 는 `[vA, vD, vC]` 이다. `member_version = 2` 이다. 마지막 찬성의 log 는 `ProposalVoted, ProposalApproved, MemberRemoved, QuorumUpdated, ProposalExecuted` 이며, validator log 는 없다 |
| V-10 | `B.changeMember(B2)`; 이어서 `B2.configureValidator(vB, kB3, popB3)` | `operator_to_validator[B2] = vB` 이다. 두 번째 호출은 키 변경(경우 2)이다 |
| V-12 | `A.proposeChangeQuorum(4)`; 다른 proposal 로 `D` 를 제거; 첫 proposal 에 quorum 까지 찬성 | 마지막 찬성은 `InvalidQuorum` 으로 revert 한다. Proposal 은 `Voting` 에 남는다 |
| V-13 | 시각 `t` 에 proposal 을 만들고, `t + expiry` 에 vote | vote 가 기록된다 |
| V-14 | 시각 `t` 에 proposal 을 만들고, `t + expiry + 1` 에 vote | revert 하지 않는다. Proposal 은 `Expired` 가 되고 `ProposalExpired` 가 나간다. Vote 는 기록되지 않고, 활성 proposal counter 는 1 줄어든다 |
| V-15 | `A` 가 proposal 3 개를 만든 뒤 4 번째를 만든다 | `TooManyActiveProposals` 로 revert 한다. 하나를 취소한 뒤에는 4 번째가 성공한다 |
| V-16 | `A.propose…`; `B.disapprove`; `A.cancelProposal` | `ProposalAlreadyInVoting` 으로 revert 한다 |
| V-17 | 4 명 중 quorum 3 에서 반대 두 표 | 두 번째 반대에서 `Rejected` 가 된다 (`2 > 4 - 3`) |
| V-18 | 제거된 member `A` 가 자신이 제거되기 전에 만든 proposal 에 찬성한다 | 받아들여진다. 이 찬성으로 quorum 을 채울 수 있다 |
| V-19 | `A` 가 만든 현재 version 의 살아 있는 proposal 이 있을 때 `A.changeMember(A2)` | `A2` 는 그 proposal 을 취소할 수 없다 (`NotProposer`). `A` 는 그 proposal 에 vote 할 수 없다 (`NotAMember`). `A2` 의 비트는 `A` 의 비트다 |
| V-20 | member 1 명, quorum 1 인 instance: `A.proposeGasTip(T1)` | proposal 을 만드는 호출 안에서 실행된다 |
| V-21 | GovCouncil: `A.proposeAddBlacklist(X)` 와 `B.proposeAddBlacklist(X)`; #1 에 찬성한 뒤 #2 에 찬성 | #1 은 `Executed` 가 되고 `X` 의 bit 63 이 켜진다. #2 는 `ProposalExecutionSkipped(X, 2, "ALREADY_BLACKLISTED")` 와 함께 `Approved` 에 남는다. `executeWithFailure(2)` 를 부르면 `Failed` 가 된다 |
| V-23 | GovCouncil: 건너뛴 proposal 에 retry 모드 attempt 를 세 번 한 뒤 네 번째 attempt | attempt 1–3 은 `ProposalExecuted(false)` 를 낸다. 네 번째는 `Failed` 와 `ProposalFailed("Max retry count reached")` 를 낸다 |
| V-24 | GovCouncil batch `[X, X, Y]` 추가 | `Executed` 가 된다. `X` 는 한 번만 추가되고, 두 번째 `X` 에는 skip log 가 나간다. `Y` 는 추가된다 |
| V-26 | 중복된 member 항목이 두 validator 와 짝지어진 genesis | storage 에서 읽은 투영은 두 validator 를 모두 보여 준다. 그 member 를 제거하면 두 번째 validator 만 제거된다 |

---

## 14. Event 참고표

| Contract | Event | 내보내는 곳 |
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
| GovMinter v1, v2 | `DepositMintProposed(uint256 indexed proposalId, string indexed depositId, address indexed requester, address beneficiary, uint256 amount, string bankReference)` `0x53586a2e…f4c3`; `BurnPrepaid(address indexed user, uint256 amount)` `0x9ac4ab95…799f`; `BurnExecuted(address indexed from, uint256 indexed amount, string withdrawalId)` `0xc4a1fb50…6530`; `EmergencyPaused` `0x11d8c430…67d5`; `EmergencyUnpaused` `0x4e8fee66…9080`; v2 에만: `BurnDepositRefunded(uint256 indexed proposalId, address indexed requester, uint256 amount)` `0x116044c8…2062`, `BurnRefundClaimed(address indexed requester, uint256 amount)` `0x9543fa26…af24` | §11 |

Source: systemcontracts/solidity/abstracts/GovBase.sol:156-177; systemcontracts/solidity/v1/GovValidator.sol:175; systemcontracts/solidity/v1/GovCouncil.sol:87-108.
Source: systemcontracts/solidity/abstracts/Mintable.sol:42-47, systemcontracts/solidity/abstracts/eip/EIP3009.sol:53-54, systemcontracts/solidity/v1/NativeCoinAdapter.sol:260 (Approval, from the OpenZeppelin `IERC20` interface)
Source: systemcontracts/solidity/v1/GovMasterMinter.sol:77-89, systemcontracts/solidity/v1/GovMinter.sol:133-153, systemcontracts/solidity/v2/GovMinter.sol:138-164

minting contract event 뒤의 hex 값은 topic0 (signature 의 `keccak256`)의 앞 4 바이트와 뒤 4 바이트다. 각 값 전체는 해당 artifact 안에 있다. 같은 signature 의 event 를 둘 이상의 system contract 가 낸다. `MinterConfigured` 와 `MinterRemoved` 는 `NativeCoinAdapter` 와 `GovMasterMinter` 가 내며, `CONFIGURE_MINTER` 한 번의 실행이 두 event 를 모두 낸다. `EmergencyPaused` 와 `EmergencyUnpaused` 는 `GovMasterMinter` 와 `GovMinter` 가 낸다. 이런 event 는 log 주소로만 구분된다.
