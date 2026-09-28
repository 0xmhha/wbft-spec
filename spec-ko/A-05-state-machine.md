# A-05 합의 state machine

- 영역 코드: `SM`
- 문서 상태: draft
- 레퍼런스 구현: go-stablenet `740526d03`. 모든 `Source:` 경로는 저장소 root 를 기준으로 한 상대 경로이다.

이 장은 WBFT 노드가 합의 core 에 도달할 수 있는 모든 event 에 어떻게 반응하는지 정한다. 그 event 는 새 chain head, block builder 가 넘겨준 proposal, 받은 합의 메시지, 미뤄 둔 메시지의 replay, timer 만료이다. 이 장은 event 마다 어떤 state 변수가 바뀌는지, 노드가 어떤 메시지를 어떤 내용으로 보내는지, 받은 메시지 중 무엇을 relay 하는지, 무엇을 commit 하도록 application 에 넘기는지를 적는다.

이 장은 다음 장에 의존한다.

- `A-03`: 메시지 encoding, signing payload, `block_hash` 를 정한다.
- `A-04`: `validators_at`, `quorum_size`, `f_value`, `calc_proposer` 를 정한다.
- `A-06`: round timer, retry timer, future-proposal 대기의 시간 값과 새 round 통지의 시점을 정한다.
- `A-07`: 메시지를 network 에서 보내고, relay 하고, dedup 하는 방법을 정한다.
- `A-08`: proposal 검증과, 이 장에서 모은 seal 로 다음 header 를 만드는 방법을 정한다.
- `A-09`: §1.3 에 나오는 application interface 연산을 정한다.

---

## 1. 소개 (informative)

### 1.1 이 장에서 얻는 것

WBFT 의 core 는 QBFT (Moniz, arXiv 2002.03613) 이다. WBFT 는 QBFT 에 wire 와 header 에 들어가는 내용을 바꾸는 두 가지를 더했다. 첫째, PREPARE 와 COMMIT 메시지는 BLS seal 을 싣고, 이 seal 은 header 안에서 aggregate 된다. 둘째, quorum 이 찬 뒤에 도착한 seal 을 *extra seal* 로 모아 다음 header 에 합친다. 이 밖에 WBFT 에는 미래 view 의 메시지를 미뤄 두는, 크기가 제한된 message backlog 가 있다. 이 backlog 는 Quorum 의 QBFT 구현에서 가져온 것이다 (consensus/wbft/core/backlog.go:18). 다만 크기 제한, 즉 too-far 필터, source 별 key, source 별 크기 상한(§13.1)은 WBFT 가 더한 것이다. backlog 는 메시지를 처리하고 relay 하는 시점만 바꾸며, 메시지의 바이트나 header 는 바꾸지 않는다.

규범 내용은 두 번 나온다. §§4-14 는 각 성질을 번호가 붙은 requirement 로 적는다. §15 는 그 requirement 들을 잘라 낸 원본인 레퍼런스 의사코드 전체를 싣는다. 둘이 다르면 편집 오류이며, 오류가 고쳐질 때까지는 requirement 문장이 우선한다.

### 1.2 이 장의 표기

- `node` 는 validator 하나의 합의 state 이다 (§3). 이름이 `on_*` 인 handler 가 이 state 를 바꾼다.
- `Q = quorum_size(node.validators)` 와 `F = f_value(len(node.validators))` 는 검사하는 순간의 현재 validator 집합으로 계산한다 (`A-04`). `F` 는 실수이다.
- `OK` 와 `ERR` 는 handler 가 돌려주는 두 결과이다. 둘 사이에서 밖으로 관측되는 차이는 relay 하나뿐이다 (§4.4). 처리 결과가 `OK` 인 메시지는 relay 되고, 처리 결과가 `ERR` 인 메시지는 relay 되지 않는다.
- `schedule(event)` 는 같은 노드가 그 event 를 나중에 처리하도록 queue 에 넣는다는 뜻이다. 그 event 는 현재 handler 가 돌아온 뒤 정해지지 않은 시각에 처리되며, queue 에 있는 다른 event 와의 순서는 보장되지 않는다 (§4.1).
- `broadcast(node, m)` 는 노드가 `m` 을 다른 validator 에게 보내고 (`A-07`), 같은 바이트를 받은 메시지로 자기에게 전달하는 self-delivery 를 `schedule` 한다는 뜻이다 (§4.5).
- view 는 사전식으로 비교한다. 먼저 sequence 를 비교하고, sequence 가 같으면 round 를 비교한다 (`A-01`).
- `uint64(x)` 는 큰 정수를 하위 64 비트로 자르고, `uint32(x)` 는 하위 32 비트로 자른다. 레퍼런스 구현도 이렇게 자른다. `int64(x)` 는 `x` 의 하위 64 비트를 2의 보수 부호 있는 정수로 읽은 값이다 (`big.Int.Int64()`). §5.2 를 통과해 도달할 수 있는 round 와 `2^63` 보다 작은 sequence 에서는 자르기 전과 후의 값이 같다.

### 1.3 사용하는 application interface 연산

이 연산들은 `A-09` 에 속하며, 이름은 `A-09` 와 `NAMES.md` 의 이름을 따른다.

| 연산 | 이 장에서의 뜻 |
|---|---|
| `app.head()` | 현재 chain head block 과 그 block 의 `Coinbase` 를 돌려준다. `Coinbase` 는 *마지막 proposer* 이며, genesis block 에서는 zero address 이다. |
| `app.validate_proposal(p)` | proposal 을 검증한다 (`A-08`). 결과는 `VALID`, 대기 시간 `d` 를 담은 `FUTURE(d)`, 또는 오류이다. |
| `app.finalize(p, prepared_seals, committed_seals, round)` | decide 된 proposal 을 seal 목록과 함께 application 에 넘긴다 (`A-09` §4.7). 이 연산은 `B-06` 의 실행 측 `process_finalize` 와 다르다. 결과는 성공 또는 실패이다. |
| `app.is_bad_block(hash)` | application 이 그 block 을 무효로 표시했는지 알려 준다. |
| `app.notify_new_round(round)` | round 가 시작되었다고 block builder 에 알린다. 이 연산은 `A-09` 의 `ready_to_build(wait, round)` 이며, `wait` 는 head 에서 구하고 `round` 는 `A-06` §8.1 과 같다. |
| `validators_at(number)` | block `number` 를 seal 하는 validator 집합을 돌려준다 (`A-04`). |

---

## 2. 실행 모델

[WBFT-SM-001] 노드는 반드시 합의 event 를 한 번에 하나씩 처리해야 한다. §15 의 각 handler 는 보내는 모든 메시지와 바꾸는 모든 state 를 포함해 끝까지 실행되며, 그 뒤에야 다음 event 가 처리된다.
Source: consensus/wbft/core/handler.go:102-181 (handleEvents)

[WBFT-SM-002] handler 가 `schedule` 로 만든 event 는 그 handler 가 돌아오기 전에 처리되어서는 안 된다. 이 제약 밖에서는 conforming 노드가 scheduled event, 받은 메시지, timer 만료를 어떤 순서로 처리해도 된다. 노드가 즉시 처리하지 않고 schedule 하는 event 는 다음과 같다.

1. 노드가 broadcast 하는 모든 메시지의 self-delivery
2. backlog 에 있던 메시지의 모든 replay
3. 저장된 proposal request 의 모든 replay
4. 미래 PRE-PREPARE 의 재투입
5. 모든 timer 만료

Source: consensus/wbft/backend/backend.go:167-171 (Broadcast: `go Post`), consensus/wbft/core/backlog.go:304, consensus/wbft/core/request.go:116-118, consensus/wbft/core/preprepare.go:156-162, consensus/wbft/core/core.go:411-413, consensus/wbft/core/core.go:446-448

> **구현 노트 (informative).** 레퍼런스 구현은 모든 event 를 별도 goroutine 에서 버퍼가 없는 `event.TypeMux` 에 넣는다. goroutine 하나(`handleEvents`)가 네 subscription (request/message/backlog, timeout, final-committed, retry) 을 `select` 로 기다린다. Go 의 `select` 는 준비된 channel 중 하나를 무작위로 고르고, event 를 넣는 goroutine 들도 서로 경쟁한다. 그래서 같은 종류의 event 사이에서도 순서가 보장되지 않는다. §17 은 이 때문에 관측되는 결과를 나열한다.

[WBFT-SM-003] 노드는 자기 주소가 `node.validators` 의 member 가 아닌 동안에는 자기 합의 메시지(PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE)를 보내서는 안 된다. 이 경우 `broadcast` 는 아무것도 보내지 않고 self-delivery 도 schedule 하지 않으며, handler 는 전송이 실패한 것처럼 계속 진행한다.
Source: consensus/wbft/backend/backend.go:152-162 (Broadcast returns ErrUnauthorizedAddress)
Observable: network

그래서 validator 가 아니면서 합의 core 를 돌리는 노드도 받은 합의 메시지를 검증하고, relay 하고, 그 메시지로 decide 한다 (§8 ~ §10). 다만 그 노드는 자기 메시지를 보내지 않는다. conforming peer 는 그 노드를 validator 로 세는 동안에만 그 노드에게 메시지를 보낸다 (`A-07` §6.2).

> 해설: 정상 노드는 validator 에게만 합의 메시지를 보낸다. 그래서 validator 가 아닌 노드가 합의 core 를 돌리더라도, 실제로는 그 노드에 합의 메시지가 거의 도착하지 않는다.

---

## 3. 노드 state

### 3.1 State 변수

노드의 합의 state 는 아래 record 이다. field 이름은 이 명세의 나머지 부분에서 쓰는 이름이고, 표의 "레퍼런스" 열은 레퍼런스 구현에서 대응하는 field 를 보여 준다.

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

| 변수 | 레퍼런스 | 설정하는 곳 | 비우거나 바꾸는 곳 |
|---|---|---|---|
| `current.view` | `current.sequence`, `current.round` | §6 | 새 round 와 새 sequence 마다 바뀐다 |
| `state` | `Core.state` | §6, §8, §9, §10 | 새 round 와 새 sequence 마다 `AcceptRequest` 로 돌아간다 |
| `validators` (proposer 포함) | `Core.valSet` | §6 | 새 sequence 에서 새 집합으로 바뀐다. proposer 는 새 round 마다 다시 계산한다 |
| `current.preprepare` | `roundState.Preprepare` | PRE-PREPARE 를 받아들일 때 | 새 sequence 에서만 비운다. round 가 바뀔 때는 값을 유지한다 |
| `current.prepares`, `current.commits` | `WBFTPrepares`, `WBFTCommits` | §9, §10 | 새 round 와 새 sequence 마다 비운다 |
| `current.prepared_round`, `current.prepared_block` | `preparedRound`, `preparedBlock` | PREPARE quorum 이 찰 때 | 새 sequence 에서 비운다. bad-block 규칙(§6.3)으로도 비운다 |
| `current.pending_request` | `pendingRequest` | §7 | 새 sequence 에서만 비운다 |
| `current.preprepare_sent` | `preprepareSent` | §8.1 | 새 round 와 새 sequence 마다 0 으로 둔다 |
| `prepared_certificate` | `WBFTPreparedPrepares` | PREPARE quorum 이 찰 때 | `start_new_round` 가 인자 `0` 으로 불릴 때에만 비운다 (§6.4) |
| `round_changes` | `roundChangeSet` | §11.2 | `start_new_round` 가 인자 `0` 으로 불릴 때에만 새로 만든다. 그 밖에는 일부만 지운다 (§6.4) |
| `pending_requests` | `pendingRequests` | §7 | 항목은 replay 되거나 old 로 판정되면 빠진다 |
| `backlog`, `backlog_keys` | `backlogs`, `backlogKeys` | §13 | 항목은 replay 되거나, old·invalid 로 판정되거나, 보낸 validator 가 validator 집합을 떠나면 빠진다 |
| `extra_prepare_seals`, `extra_commit_seals` | `prepareExtraSeals`, `commitExtraSeals` | §12 | sequence 를 기준으로 지운다 (§12.5) |
| `prior` | `priorState` | 새 sequence 에 들어갈 때 | 다음 새 sequence 에 들어갈 때 바뀐다 |

Source: consensus/wbft/core/core.go:56-124 (New, Core), consensus/wbft/core/roundstate.go:33-69, consensus/wbft/core/roundchange.go:226-244, consensus/wbft/core/priorstate.go:28-45, consensus/wbft/core/types.go:37-42

> 해설: `state` 의 네 값 사이의 전이를 그림으로 그리면 다음과 같다. round 가 바뀌면 `state` 는 항상 `AcceptRequest` 로 돌아가지만, 받아들인 PRE-PREPARE, lock, 저장된 proposal request 는 새 round 로 옮겨 간다 (WBFT-SM-005). 새 sequence 에서는 이 값들을 모두 비운다 (WBFT-SM-006).
>
> ```mermaid
> stateDiagram-v2
>     [*] --> AcceptRequest: 시작, 새 sequence, 새 round
>     AcceptRequest --> Preprepared: justification 이 유효한 PRE-PREPARE 를 받아들이고 PREPARE 를 보낸다
>     Preprepared --> Prepared: PREPARE 가 quorum 만큼 모이면 lock 을 걸고 COMMIT 을 보낸다
>     Prepared --> Committed: COMMIT 이 quorum 만큼 모이면 finalize 를 호출한다
>     Preprepared --> AcceptRequest: timeout 이나 F+1 규칙으로 round 가 바뀐다
>     Prepared --> AcceptRequest: round 가 바뀌어도 lock 은 유지한다
>     Committed --> AcceptRequest: 새 head 가 오거나 round timer 가 만료된다
> ```

### 3.2 수명

[WBFT-SM-004] 합의 core 가 시작되면 노드는 반드시 다음 값으로 시작해야 한다.

1. `current = None`
2. `state = AcceptRequest`
3. 비어 있는 `backlog`, `pending_requests`, `extra_prepare_seals`, `extra_commit_seals`
4. `prior = PriorState(round=0, proposal=None, validators=None)`

그런 다음 노드는 반드시 `start_new_round(node, 0)` 을 실행해야 한다 (§6).
Source: consensus/wbft/core/core.go:56-78, consensus/wbft/core/handler.go:35-47

[WBFT-SM-005] sequence 안에서 round 가 바뀔 때 (§6.2 의 `ROUND_CHANGE` branch), 노드는 반드시 `current.preprepare`, `current.prepared_round`, `current.prepared_block`, `current.pending_request` 를 새 round state 로 옮겨야 하며, 이때 §6.3 의 조건을 따른다. 또한 노드는 반드시 새 round 를 비어 있는 `prepares` 와 `commits`, 그리고 `preprepare_sent = 0` 으로 시작해야 한다.
Source: consensus/wbft/core/core.go:277-288, consensus/wbft/core/roundstate.go:33-49

[WBFT-SM-006] 새 sequence 에 들어갈 때 (§6.2 의 `INITIAL`, `CATCH_UP` branch), 노드는 반드시 `preprepare`, `prepared_round`, `prepared_block`, `pending_request` 가 모두 `None` 인 round state 로 시작해야 한다.
Source: consensus/wbft/core/core.go:288-294

[WBFT-SM-007] (withdrawn; informative) 레퍼런스 구현은 합의 core 를 멈췄다가 다시 시작할 때 합의 state 를 하나도 유지하지 않는다. 노드를 재시작하거나, block builder 를 멈췄다가 다시 시작하는 경우가 그 예이다. 다시 시작한 노드는 그 전에 무엇을 보냈는지와 무관하게 WBFT-SM-004 대로 동작한다. 즉 그 노드에는 lock 도, prepared certificate 도, backlog 도, extra seal 도 없다. 예외는 하나이다. `Stop` 이 timer 들을 취소한 뒤에 이전 core 의 event loop 가 건 timer 는 취소되지 않는다. 그 timer 가 재시작 뒤에 만료되면 새 core 는 그 만료를 자기 것으로 처리한다 (`A-06` WBFT-TIMER-018).
Source: consensus/wbft/backend/backend.go:355-382 (startWBFT creates a new Core; stop discards it), consensus/wbft/core/handler.go:104-107, consensus/wbft/core/handler.go:50-59

lock(`current.prepared_round`, `current.prepared_block`)과 마지막으로 받아들인 PRE-PREPARE(`current.preprepare`)는 수명이 서로 다른 별개의 변수이며(WBFT-SM-005, WBFT-SM-025), 서로 다른 block 을 가리킬 수 있다. lock 은 PREPARE quorum 이 찰 때만 바뀐다. 반면 `current.preprepare` 는 노드가 PRE-PREPARE 를 받아들일 때마다 바뀌며, 뒤 round 에서 다른 block 을 제안하는 justified PRE-PREPARE 를 받아들일 때도 바뀐다(`A-14` WBFT-PROTO-002). ROUND-CHANGE 는 lock 을 보고하고(§11.1), `prior.proposal` 은 `current.preprepare` 에서 가져온다(WBFT-SM-025).

---

## 4. Event

### 4.1 Event 종류

| Event | 만드는 곳 | Handler |
|---|---|---|
| `Start` | application 이 합의를 시작한다 (`A-09`) | `on_start` |
| `Stop` | application 이 합의를 멈춘다 | `on_stop` |
| `NewHead` | application 의 chain head event 마다 생긴다. 자기 decision 과 다른 노드의 decision 이 여기에 해당한다. 동기화 중에는 block 마다가 아니라 import 한 묶음마다 적어도 한 번 생긴다 | `on_new_head` |
| `Request(p)` | block builder 가 proposal 을 넘기거나, 저장된 request 가 replay 된다 (§7) | `on_request` |
| `Message(code, payload)` | peer 에게서 받은 합의 메시지이거나 (`A-07`), broadcast 의 self-delivery 이다 | `on_message_received` |
| `Backlog(m)` | backlog 메시지의 replay 이거나 (§13), 미래 PRE-PREPARE 의 재투입이다 (§8.3) | `on_backlog_event` |
| `RoundTimeout(t)` | round timer `t` 가 만료되었다 (`A-06`) | `on_round_timeout` |
| `RetryTimeout(r)` | round `r` 로 걸어 둔 retry timer 가 만료되었다 (`A-06`) | `on_retry_timeout` |

Source: consensus/wbft/core/handler.go:64-81, consensus/wbft/core/handler.go:109-179, consensus/wbft/backend/handler.go:138-146, consensus/wbft/backend/engine.go:186-219

### 4.2 시작과 정지

[WBFT-SM-008] `Start` 를 받으면 노드는 반드시 WBFT-SM-004 대로 state 를 초기화하고, 다른 event 를 처리하기 전에 `start_new_round(node, 0)` 을 실행해야 한다.
Source: consensus/wbft/core/handler.go:35-47

> **구현 노트 (informative).** 레퍼런스 구현에서 `Core.Start` 는 event loop 를 먼저 띄운 뒤, 호출자의 goroutine 에서 `startNewRound(0)` 을 부른다. 그 사이에 event loop 에 도달하는 event 는 없다. `Backend.HandleMsg` 와 `Backend.NewChainHead` 는 `coreStarted` 가 설정되기 전까지 event 를 거부하고, `coreStarted` 는 `Core.Start` 가 돌아온 뒤에 설정되기 때문이다 (consensus/wbft/backend/engine.go:266-272, consensus/wbft/backend/handler.go:74-76, consensus/wbft/backend/handler.go:141-143). 다만 `Backend.Seal` 이 올리는 `RequestEvent` 는 이렇게 막히지 않는다 (consensus/wbft/backend/engine.go:186-200). 그 사이에 `RequestEvent` 가 도착하면 core 는 그 event 를 `errCurrentIsNil` 로 버린다 (consensus/wbft/core/request.go:68-70). 그 사이에 `Seal` 이 불릴 수 있는지는 확인하지 않았다.

[WBFT-SM-009] `Stop` 을 받으면 노드는 반드시 세 timer 를 모두 멈추고, 반드시 합의 event 처리를 멈추고, 반드시 합의 state 를 버려야 한다 (WBFT-SM-007). 멈춰 있는 동안 노드는 peer 에게서 받은 합의 메시지를 처리하지 않는다. network 수준에서 어떻게 응답하는지는 `A-07` 이 정한다.
Source: consensus/wbft/core/handler.go:50-59, consensus/wbft/backend/engine.go:288-300, consensus/wbft/backend/handler.go:73-76

### 4.3 새 head

[WBFT-SM-010] 노드는 `NewHead` event 마다 반드시 `start_new_round(node, 0)` 을 실행해야 한다. 이 event 는 데이터를 싣지 않으며, `start_new_round` 가 head 를 직접 읽는다 (§6.2).
Source: consensus/wbft/core/final_committed.go:27-31, consensus/wbft/backend/handler.go:138-146, miner/worker.go:644-661

### 4.4 Relay

[WBFT-SM-011] `Message(code, payload)` event 의 처리 결과가 `OK` 이면, 노드는 반드시 `payload` 를 바꾸지 않고 `code` 로 `node.validators` 중 자기를 뺀 member 에게 relay 해야 한다. 이때 `node.validators` 는 handler 가 돌아온 뒤의 값으로 평가한다. 이미 그 메시지를 가진 peer 를 대상에서 빼는 규칙은 `A-07` 이 정한다.
Source: consensus/wbft/core/handler.go:128-135, consensus/wbft/backend/backend.go:176-210
Observable: network

[WBFT-SM-012] `Backlog(m)` event 의 처리 결과가 `OK` 이면, 노드는 반드시 `rlp_encode(m)` 을 `m.code` 로 WBFT-SM-011 과 같은 대상에게 relay 해야 한다.
Source: consensus/wbft/core/handler.go:136-150
Observable: network

[WBFT-SM-013] 처리 결과가 `ERR` 인 메시지는 그 시점에 relay 해서는 안 된다. 여기에는 `FUTURE` 로 분류된 메시지, `OLD`, `INVALID`, `TOO_FAR` 로 분류된 메시지, signature 가 무효인 메시지, handler 가 거부한 메시지가 모두 포함된다. `FUTURE` 로 분류된 메시지는 나중에 replay 결과가 `OK` 가 되면 그때 relay 된다.
Source: consensus/wbft/core/handler.go:128-133, consensus/wbft/core/handler.go:211-227
Observable: network

§16 은 처리 결과마다 메시지를 relay 하는지 정리한다. 독자가 예상하지 못할 수 있는 relay 가 두 가지 있다. 하나는 저장되었거나 조용히 무시된 extra seal 메시지이다 (§12.2). 다른 하나는 quorum 을 처리하는 도중 proposer 자신의 justification 자기 검사에서 실패한 ROUND-CHANGE 이다 (§11.5).

> 해설: 반대로 proposal 로 쓸 block 이 없어서 quorum 규칙이 `ERR` 로 끝난 ROUND-CHANGE 는 relay 되지 않는다 (§16 표의 28 행). 관측 도구가 relay 여부만 보고 메시지가 유효했는지 판단하면, 위에서 말한 세 경우 모두에서 잘못 판정한다.

### 4.5 Self-delivery

[WBFT-SM-014] 노드가 broadcast 하는 모든 메시지는 반드시 그 노드 자신도 받은 `Message` event 로 처리해야 한다. 이 처리는 §5 의 수신 경로 전체, 즉 decode, signature 검증, `check_message`, handler 를 모두 거친다. 노드 자신의 메시지는 이 경로를 통해서만 세어지고, 저장되고, relay 된다.
Source: consensus/wbft/backend/backend.go:164-172
Observable: network, log

self-delivery 를 즉시 처리하지 않고 schedule 하기 때문에 생기는 결과는 §17 에 있다.

> 해설: 노드 자신의 PREPARE 와 COMMIT 이 자기 quorum 에 들어가는 길은 self-delivery 하나뿐이다. self-delivery 는 `A-07` 의 known cache 검사를 거치지 않는다 (WBFT-NET-030). 새 구현은 자기 메시지를 즉시 처리해서 순서 문제를 없애도 된다. 다만 그렇게 하면 header 에 자기 seal 이 들어가는 빈도가 레퍼런스와 달라지므로, diligence 분포도 레퍼런스 노드와 조금 달라질 수 있다.

---

## 5. 메시지 수신

### 5.1 Decode 와 signature

[WBFT-SM-015] code 가 `PREPREPARE`, `PREPARE`, `COMMIT`, `ROUND_CHANGE` 중 하나가 아닌 `Message` event 는 반드시 버려야 한다. `A-03` 대로 decode 되지 않는 payload 도 반드시 버려야 한다.
Source: consensus/wbft/core/handler.go:188-202, consensus/wbft/messages/decode.go:27-59

[WBFT-SM-016] view 가 `v` 인 메시지나 justification member `x` 의 signature 를 검증할 때, 노드는 반드시 `signature_validator_set(node, v)` 가 돌려주는 validator 집합을 써야 한다:

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

그래서 미래 view 의 메시지는, sequence `cur.sequence + 1` 의 모든 메시지를 포함해, 그 sequence 를 seal 할 집합이 아니라 *현재* validator 집합으로 검증한다.

[WBFT-SM-017] 노드는 반드시 메시지 자체의 ECDSA signature 를 먼저 검증하고, 이어서 각 justification member 의 signature 를 다음 순서로 검증해야 한다. 그리고 노드는 처음 실패하는 곳에서 반드시 메시지 전체를 버려야 한다.

1. 메시지 자체를 검증한다.
2. ROUND-CHANGE 이면 `justification` 의 각 원소(PREPARE)를 목록 순서대로 검증한다.
3. PRE-PREPARE 이면 `justification_round_changes` 의 각 원소를 목록 순서대로 검증한 뒤, `justification_prepares` 의 각 원소를 목록 순서대로 검증한다.

`ecdsa_recover_address(message_signing_payload(x), x.signature)` 가 성공하고, recover 된 주소가 `signature_validator_set(node, x.view)` 의 member 이면 signature 가 검증된 것이다. 이 함수는 입력을 내부에서 hash 한다 (`A-02` WBFT-CRYPTO-013). recover 된 주소가 `x.source` 가 된다.
Source: consensus/wbft/core/handler.go:268-317, consensus/wbft/utils.go:39-73

[WBFT-SM-018] 노드는 justification PREPARE 의 BLS seal 을 verify 해서는 안 되고, ROUND-CHANGE 의 `prepared_block` 에 `validate_proposal` 을 실행해서도 안 된다. 노드는 justification member 에 대해 ECDSA signature (WBFT-SM-017) 와 §11.2, §11.6 의 구조 검사만 확인한다.
Source: consensus/wbft/core/handler.go:292-314, consensus/wbft/core/roundchange.go:118-151

signature 검증을 통과한 justification member 는 `is_justified` (§11.6) 와 `has_matching_round_change_and_prepares` (§11.3) 에서만 쓰인다. 이 member 들은 노드 자신의 round 의 PREPARE 로 저장되지 않으며, seal 에도 기여하지 않는다.

### 5.2 `check_message`

`check_message` 는 메시지의 code 와 view 를 노드의 현재 view 및 state 와 견주어 메시지를 분류한다. 노드는 메시지가 도착했을 때 이 함수를 평가하고, backlog 메시지를 replay 할지 판단할 때 (§13.3) 다시 평가하며, replay 된 메시지를 처리할 때 또 평가한다.

[WBFT-SM-019] `check_message` 는 반드시 다음 규칙을 이 순서대로 평가해서, 처음 맞는 규칙의 결과를 돌려주어야 한다. 아래에서 `cur = node.current.view`, `s = node.state` 이다.

1. **너무 먼 미래**: 다음 조건 중 하나라도 참이면 `TOO_FAR` 를 돌려준다.
   1. `v.sequence > cur.sequence + SEQUENCE_THRESHOLD` 이다 (`SEQUENCE_THRESHOLD = 1`).
   2. `v.sequence > cur.sequence` 이고 `v.round >= ROUND_THRESHOLD` 이다. 여기서 `ROUND_THRESHOLD = 10` 이며, 이 조건은 round 를 현재 round 와의 차이가 아니라 절대값으로 비교한다.
   3. `v.sequence == cur.sequence` 이고 `v.round > cur.round + ROUND_THRESHOLD` 이다.
2. **ROUND-CHANGE** (`code == ROUND_CHANGE`):
   1. `v.sequence > cur.sequence` 이면 `FUTURE` 를 돌려준다.
   2. `v < cur` 이면 `OLD` 를 돌려준다.
   3. 그 밖이면 `PROCESS` 를 돌려준다. 이 경우 sequence 는 같고 round 는 `cur.round` 부터 `cur.round + 10` 사이의 어떤 값이다.
3. **다른 code, 미래 view**: `v > cur` 이면 `FUTURE` 를 돌려준다.
4. **다른 code, 과거 view**: `v < cur` 이면 다음과 같다.
   1. `cur.sequence - v.sequence == 1` 이고 `v.round == node.prior.round` 이고 `s == AcceptRequest` 이면 `EXTRA_SEAL` 을 돌려준다.
   2. 그 밖이면 `OLD` 를 돌려준다.
5. **다른 code, 현재 view**: `v == cur` 이면 state 에 따라 다음 표의 결과를 돌려준다.

| State `s` \ code | `PREPREPARE` | `PREPARE` | `COMMIT` |
|---|---|---|---|
| `AcceptRequest` | `PROCESS` | `FUTURE` | `FUTURE` |
| `Preprepared` | `INVALID` | `PROCESS` | `FUTURE` |
| `Prepared` | `INVALID` | `EXTRA_SEAL` | `PROCESS` |
| `Committed` | `INVALID` | `EXTRA_SEAL` | `EXTRA_SEAL` |

Source: consensus/wbft/core/backlog.go:44-51, consensus/wbft/core/backlog.go:76-113 (isTooFarFutureMessage), consensus/wbft/core/backlog.go:125-202 (checkMessage)

규칙 4.1 은 code 를 보지 않는다. 그래서 이전 sequence 의 prior round 에 대한 PRE-PREPARE 도 `EXTRA_SEAL` 로 분류되며, 그 PRE-PREPARE 는 §12.2 에서 거부되거나 조용히 무시된다. view 의 sequence 나 round 가 빠진 메시지는 `INVALID` 이지만, `A-03` 의 decoder 는 그런 메시지를 만들지 않는다.
Source: consensus/wbft/core/backlog.go:126-128

> 해설: 표의 논리는 다음과 같다. PREPARE 의 seal 을 검증하려면 그 seal 이 어느 block 에 대한 것인지 알아야 한다. `AcceptRequest` 에서는 노드가 아직 PRE-PREPARE 를 받지 않아 block 을 모르므로, 노드는 PREPARE 를 backlog 로 미룬다. COMMIT 은 노드 자신이 `Prepared` 가 된 뒤에야 센다. quorum 을 넘긴 뒤에 도착한 투표는 버리지 않고 extra seal 로 모아 다음 block 의 header 에 기록한다 (§12). `TOO_FAR` 규칙은 backlog 가 쓰는 메모리에 상한을 두기 위한 것이다. threshold 값은 로컬 파라미터이지만, 레퍼런스보다 작게 잡으면 catch-up 에 실패할 수 있다.

[WBFT-SM-020] `check_message` 를 평가한 뒤 노드는 반드시 메시지를 다음과 같이 처리해야 한다:

| 결과 | 처리 | Handler 결과 |
|---|---|---|
| `PROCESS` | code 에 맞는 handler 에 넘긴다 (§8.3, §9.2, §10.2, §11.2) | handler 의 결과 |
| `FUTURE` | `add_to_backlog(node, m)` 을 실행한다 (§13) | `ERR` |
| `EXTRA_SEAL` | `add_extra_seal(node, m)` 을 실행한다 (§12.2) | `add_extra_seal` 의 결과 |
| `OLD`, `INVALID`, `TOO_FAR` | 버린다 | `ERR` |

Source: consensus/wbft/core/handler.go:211-227
Observable: network

> **구현 노트 (informative).** `handleDecodedMessage` 는 오류 변수에 `addToExtraSeal` 의 결과를 다시 대입한다. 그래서 extra seal 을 성공적으로 저장하면 `nil` 이 돌아오고, 그 메시지는 relay 된다. §12.2 는 이 동작에 기댄다.

---

## 6. 새 round 와 새 sequence

### 6.1 개요

`start_new_round(node, round)` 는 view 를 바꾸는 유일한 절차이다. 노드는 시작할 때와 새 head 가 올 때마다 `round = 0` 으로 이 절차를 부르고, round timeout (§14.2) 과 F+1 규칙 (§11.4) 에서는 `round > 0` 으로 부른다. 인자는 *요청한* round 이다. 노드가 실제로 들어가는 view 는 chain head 에 달려 있고, 여러 초기화 동작은 들어간 view 가 아니라 이 인자에 달려 있다.

> 해설: 새 sequence 에 들어갈 때 일어나는 일을 순서대로 적으면 다음과 같다. 노드는 떠나는 round 의 PREPARE 와 COMMIT 을 extra seal 저장소로 옮기고 (WBFT-SM-023), `prior` 에 떠나는 round 번호와 마지막으로 받아들인 proposal 을 기록한다 (WBFT-SM-025). 이어서 노드는 새 validator 집합과 proposer 를 정하고 (WBFT-SM-026), `state` 를 `AcceptRequest` 로 두면서 저장된 proposal request 와 backlog 를 다시 넣는다 (WBFT-SM-027). 요청한 round 가 0 이면 노드는 prepared certificate 를 지우고 round change 집합을 새로 만든다 (WBFT-SM-028). 마지막으로 노드는 block builder 에 새 round 를 알리고 (WBFT-SM-029), round timer 를 건다 (WBFT-SM-030). 마지막 두 단계는 들어간 view 가 아니라 요청한 round 를 기준으로 동작하며, 이 때문에 §14.2 의 경우 3 이 생긴다.

### 6.2 Branch 선택

[WBFT-SM-022] `CATCH_UP` branch 에서 노드는 요청한 `round` 와 무관하게 반드시 새 sequence 의 round 0 에 들어가야 한다.
Source: consensus/wbft/core/core.go:231-236

`ROUND_CHANGE` branch 는 `round == node.current.view.round` 도 받아들인다. 이 경우 노드는 메시지 집합을 비운 채 현재 round 에 다시 들어간다. 그러나 레퍼런스 구현에는 현재 round 를 요청하는 호출자가 없으므로, 이 경우에는 도달할 수 없다.

[WBFT-SM-023] `INITIAL` 과 `CATCH_UP` branch 에서 `node.current is not None` 이면, 노드는 반드시 떠나는 round state 에 대해 먼저 `add_effective_seals_to_extra_seals(node)` (§12.4) 를 실행해야 한다.
Source: consensus/wbft/core/core.go:237-240, consensus/wbft/core/extraseal.go:118-129

### 6.3 Round state 갱신

[WBFT-SM-024] `ROUND_CHANGE` branch 에서 `node.current.prepared_block` 이 설정되어 있고 `app.is_bad_block(block_hash(node.current.prepared_block))` 가 참이면, 노드는 반드시 `prepared_round` 와 `prepared_block` 을 새 round 로 옮기기 전에 비워야 하고, 반드시 `clear_extra_seals(node, node.current.view.sequence + 1)` (§12.5) 을 실행해야 한다. `prepared_certificate` 는 비우지 않는다.
Source: consensus/wbft/core/core.go:278-286

이 규칙은 COMMIT quorum 을 얻었지만 import 할 수 없었던 proposal 을 노드가 버리는 방법이다 (§10.4). 노드는 round timeout 이나 F+1 규칙 (§11.4) 을 통해서만 decide 한 round 를 떠나고, 다음 round 가 시작될 때 lock 이 풀린다.

[WBFT-SM-025] `INITIAL` 과 `CATCH_UP` branch 에서 `node.current is not None` 이면, 노드는 반드시 round state 를 바꾸기 전에 `prior` 를 갱신해야 한다:

```python
def update_prior_state(node):
    node.prior.round = node.current.view.round
    if node.current.preprepare is not None:
        node.prior.proposal = node.current.preprepare.proposal
    if node.validators is not None:
        node.prior.validators = node.validators
```

그래서 `prior` 는 노드가 있던 round 와 노드가 마지막으로 받아들인 proposal 을 나타낸다. 이 값은 실제로 commit 된 round 와 block 과 다를 수 있다.
Source: consensus/wbft/core/core.go:289-292, consensus/wbft/core/priorstate.go:35-45

[WBFT-SM-026] round state 를 갱신한 뒤 노드는 반드시 `node.validators` 를 선택된 branch 의 새 validator 집합(§6.2)으로 설정해야 하고, 반드시 그 집합의 proposer 를 `calc_proposer(node.validators, last_proposer, new_view.round, policy)` (`A-04`) 로 설정해야 한다. 여기서 `last_proposer` 는 `start_new_round` 가 읽은 head 의 `Coinbase` 이다.
Source: consensus/wbft/core/core.go:243-246, consensus/wbft/core/core.go:295, consensus/wbft/backend/backend.go:319-342, consensus/wbft/engine/engine.go:86-88
Observable: log

### 6.4 초기화, 통지, timer

[WBFT-SM-027] 이어서 노드는 반드시 `set_state(node, AcceptRequest)` 를 실행해야 한다. 이 함수는 저장된 proposal request 를 먼저 replay 하고 (§7.3), 그다음 backlog 를 replay 한다 (§13.3).
Source: consensus/wbft/core/core.go:247, consensus/wbft/core/core.go:298-311

[WBFT-SM-028] `set_state` 를 실행한 뒤 노드는 반드시 들어간 view 가 아니라 *요청한* `round` 인자에 따라 round change 집합과 prepared certificate 를 갱신해야 한다.

- `round == 0` 이면 노드는 `prepared_certificate = None` 으로 두고, `round_changes` 를 빈 집합으로 바꾸고, `clear_extra_seals(node, last.number)` (§12.5) 를 실행한다.
- 그 밖이면 노드는 `round_changes.clear_lower_than(round)` (§11.3) 을 실행한다.

두 경우 모두 노드는 이어서 `round_changes.new_round(round)` 를 실행한다.
Source: consensus/wbft/core/core.go:249-258, consensus/wbft/core/roundchange.go:246-256, consensus/wbft/core/roundchange.go:334-346

노드가 `round > 0` 으로 `CATCH_UP` branch 를 타는 경우가 있다. 늦은 round timeout (§14.2) 이 처리된 경우와, head 가 나아간 뒤 F+1 규칙이 발동한 경우이다. 이때 노드는 새 sequence 의 round 0 에 들어가지만, round 인자가 0 이 아니므로 다음과 같이 동작한다.

1. 노드는 이전 sequence 의 ROUND-CHANGE 항목 중 round 가 `round` 이상인 항목을 유지한다.
2. 노드는 이전 sequence 의 `prepared_certificate` 를 유지한다.
3. 노드는 옛 extra seal 을 지우지 않는다.
4. 노드는 `round` 에 대한 빈 round change 항목을 만든다.

round change 집합은 sequence 를 기록하지 않으므로, 그 항목들은 나중에 새 sequence 의 것처럼 세어진다 (§11.4).

[WBFT-SM-029] 그다음 노드는 반드시 요청한 `round` 인자로 `app.notify_new_round(round)` 를 호출해야 한다. 그래서 `round > 0` 인 `CATCH_UP` branch 에서는 노드가 round 0 에 있는데도 block builder 가 round `> 0` 이 시작되었다는 통지를 받는다. `A-06` 의 대기 규칙이 이 값에 달려 있다.
Source: consensus/wbft/core/core.go:260, consensus/wbft/backend/engine.go:277-285

[WBFT-SM-030] 마지막으로 노드는 반드시 들어간 view 의 round timer 를 시작해야 한다 (`A-06`). round timer 를 시작하면 retry timer 와 future-proposal timer 가 멈추고, 이전의 모든 round timer 가 취소된다. 그래서 이미 queue 에 들어간 이전 `RoundTimeout` 은 무시된다 (§14.1). 이 성질은 합의 core 가 한 번 실행되는 동안에만 성립한다. `Stop` 중에 걸린 timer 는 WBFT-SM-075 가 다룬다.
Source: consensus/wbft/core/core.go:268-271, consensus/wbft/core/core.go:336-347, consensus/wbft/core/core.go:355-415
Observable: log

---

## 7. Proposal request

### 7.1 분류

```python
def check_request(node, p) -> str:
    if p is None:                   return "INVALID"
    if node.current is None:        return "INVALID"     # reference: errCurrentIsNil
    if node.current.view.sequence > p.number: return "OLD"
    if node.current.view.sequence < p.number: return "FUTURE"
    return "OK"
```

Source: consensus/wbft/core/request.go:63-79

### 7.2 Request 처리

[WBFT-SM-031] `Request(p)` 를 받으면 노드는 반드시 `check_request` 를 평가해야 한다. 결과가 `FUTURE` 이면 노드는 반드시 `p` 를 `pending_requests` 에 저장해야 하고, 결과가 `OLD` 나 `INVALID` 이면 반드시 `p` 를 버려야 한다.
Source: consensus/wbft/core/handler.go:118-127, consensus/wbft/core/request.go:33-45, consensus/wbft/core/request.go:81-90

[WBFT-SM-032] 결과가 `OK` 이면 노드는 state 와 무관하게 반드시 `current.pending_request = p` 로 설정해야 한다. 그리고 `state == AcceptRequest` 이고 `uint64(current.view.round) == 0` 이면, 노드는 반드시 `send_preprepare(node, p, round_changes=None, prepares=None)` (§8.1) 을 실행해야 한다. round 가 0 보다 크면 노드는 request 를 저장하기만 하고, 그 proposal 은 ROUND-CHANGE quorum 규칙 (§11.5) 을 통해 제안된다.
Source: consensus/wbft/core/request.go:47-56

`send_preprepare` 는 노드가 proposer 인지 스스로 확인하므로 (§8.1), proposer 가 아닌 노드는 request 를 기록하기만 한다. 이 경로에서는 `preprepare_sent` 를 보지 않는다. 그래서 노드가 자신의 PRE-PREPARE 를 받아들이기 전에 같은 sequence 의 서로 다른 request 두 개가 round 0 에서 처리되면, 같은 view 에 proposal 이 서로 다른 PRE-PREPARE 두 개가 나간다 (§17).

### 7.3 저장된 request 의 replay

[WBFT-SM-033] `set_state(node, AcceptRequest)` 가 실행될 때마다 노드는 반드시 `pending_requests` 에서 `int64(p.number)` 가 가장 낮은 request 를 반복해서 꺼내야 한다. 저장된 우선순위는 `-int64(p.number)` 이고, `2^63` 보다 작은 번호에서는 block 번호가 가장 낮은 request 가 된다. 번호가 같은 request 사이의 순서는 정하지 않는다. 노드는 꺼낸 request 를 `check_request` 결과에 따라 다음과 같이 처리한다.

1. 결과가 `FUTURE` 이면 노드는 그 request 를 되돌려 넣고 반복을 멈춘다.
2. 결과가 `OLD` 나 `INVALID` 이면 노드는 그 request 를 버린다.
3. 결과가 `OK` 이면 노드는 `schedule(Request(p))` 를 실행한다.

Source: consensus/wbft/core/core.go:304-306, consensus/wbft/core/request.go:94-120

---

## 8. PRE-PREPARE

### 8.1 전송

[WBFT-SM-034] `send_preprepare(node, p, round_changes, prepares)` 는 `p.number == current.view.sequence` 이고 노드가 `node.validators` 의 proposer 인 경우가 아니면, 반드시 아무것도 보내지 않아야 한다.
Source: consensus/wbft/core/preprepare.go:42-51

[WBFT-SM-035] PRE-PREPARE 는 반드시 다음 값을 실어야 한다.

1. `sequence = current.view.sequence`
2. `round = current.view.round`
3. `proposal = p`
4. signing payload 에 대한 ECDSA signature (`A-03`)
5. `justification_round_changes`: `round_changes` 에 있는 ROUND-CHANGE 메시지들의 signed payload 이다. `round_changes is None` 이면 빈 목록이다.
6. `justification_prepares`: `prepares` 와 같다. `prepares` 가 `None` 이면 빈 목록이다.

`justification_round_changes` 의 순서는 정하지 않는다.
Source: consensus/wbft/core/preprepare.go:52-84, consensus/wbft/messages/preprepare.go:50-71
Observable: network

[WBFT-SM-036] broadcast 가 성공하면 노드는 반드시 `current.preprepare_sent = current.view.round` 로 설정해야 한다. broadcast 가 실패하면 (WBFT-SM-003) `preprepare_sent` 는 바뀌지 않는다.
Source: consensus/wbft/core/preprepare.go:98-105

### 8.2 수신: 검사 순서

[WBFT-SM-037] `PROCESS` 로 분류된 PRE-PREPARE 에 대해 노드는 다음 검사를 순서대로 적용하고, 처음 실패하는 검사에서 state 를 바꾸지 않은 채 반드시 `ERR` 로 거부해야 한다.

1. `m.source` 가 `node.validators` 의 proposer 가 아니다.
2. `uint64(m.sequence) != uint64(m.proposal.number)` 이다.
3. `uint64(m.round) > 0` 이고 `not is_justified(m.proposal, m.view, m.justification_round_changes, m.justification_prepares, Q)` 이다 (§11.6).
4. `app.validate_proposal(m.proposal)` 이 `VALID` 를 돌려주지 않는다. `FUTURE` 를 돌려주는 경우는 WBFT-SM-038 이 정한다.

round 0 의 PRE-PREPARE 에 대해서는 노드가 justification 목록을 보지 않는다. 여기의 3번 검사와 WBFT-SM-032 의 round 검사는 round 의 하위 64 bit 만 쓴다. 그래서 0 이 아닌 2^64 의 배수인 round 는 round 0 으로 다루어진다. 그러나 노드는 자기 round 보다 `ROUND_THRESHOLD` 넘게 앞선 메시지를 처리하지 않으므로(WBFT-SM-019), 노드의 round 는 한 단계에 제한된 만큼만 오른다. 그래서 그런 round 에는 실제로 닿지 않는다.
Source: consensus/wbft/core/preprepare.go:115-169
Observable: network

이 검사들은 어느 것도 round `> 0` 의 PRE-PREPARE 를 받는 노드 자신의 lock 과 비교하지 않는다. `A-14` WBFT-PROTO-002 를 참고한다.

> 해설: proposal 검증은 block 을 실행하지 않는다. 그래서 epoch block 의 `EpochInfo` 가 틀려도 노드는 이 단계에서 그 사실을 알 수 없다.

### 8.3 미래 proposal

[WBFT-SM-038] `app.validate_proposal` 이 `FUTURE(d)` 를 돌려주면, 노드는 반드시 `ERR` 를 돌려주고 future-proposal timer 를 걸어서 `d` 뒤에 `Backlog(m)` 을 schedule 해야 한다. timer 를 새로 걸면 이전의 future-proposal timer 가 교체되므로, 노드는 한 번에 PRE-PREPARE 하나만 기다린다. 이 timer 는 round timer 가 시작될 때마다 멈춘다 (WBFT-SM-030).
Source: consensus/wbft/core/preprepare.go:148-169, consensus/wbft/core/core.go:321-325

재투입된 PRE-PREPARE 는 `check_message` 를 다시 거치고, 그때 받아들여지면 relay 된다 (WBFT-SM-012). 처음 도착했을 때에는 그 PRE-PREPARE 가 relay 되지 않는다.

> 해설: block timestamp 가 받는 노드의 시계보다 미래이면, 노드는 그 PRE-PREPARE 를 거부하지도 받아들이지도 않고 그 시각까지 기다렸다가 다시 처리한다. 대기 시간의 계산은 `A-06` 이 정한다.

### 8.4 수락

[WBFT-SM-039] 모든 검사를 통과하고 `state == AcceptRequest` 이면, 노드는 반드시 다음을 이 순서대로 해야 한다. 노드는 현재 view 의 round timer 를 다시 시작하고 (`A-06`), `current.preprepare = m` 으로 설정하고, `set_state(node, Preprepared)` 를 실행하고, `broadcast_prepare(node)` (§9.1) 를 실행한다. handler 는 `OK` 를 돌려준다.
Source: consensus/wbft/core/preprepare.go:171-196
Observable: network, log

`check_message` 는 현재 view 의 PRE-PREPARE 를 `AcceptRequest` 에서만 `PROCESS` 로 분류한다. 그래서 합의 core 가 한 번 실행되는 동안 노드는 한 view 에 PRE-PREPARE 를 많아야 하나 받아들이고, PREPARE 도 많아야 하나 보낸다.

---

## 9. PREPARE

### 9.1 전송

[WBFT-SM-040] `broadcast_prepare(node)` 는 반드시 다음 PREPARE 를 broadcast 해야 한다. 그 PREPARE 의 값은 `sequence = current.view.sequence`, `round = current.view.round`, `digest = block_hash(current.preprepare.proposal)`, `prepare_seal = bls_sign(seal_data(header(current.preprepare.proposal), uint32(current.view.round), PREPARE_SEAL))` (`A-02`) 이며, 그 PREPARE 는 signing payload 에 대한 ECDSA 로 서명한다 (`A-03`).
Source: consensus/wbft/core/prepare.go:35-79, consensus/wbft/core/core.go:465-469, consensus/wbft/backend/backend.go:287-289
Observable: network

### 9.2 수신

[WBFT-SM-041] `PROCESS` 로 분류된 PREPARE 는 `m.digest != block_hash(current.preprepare.proposal)` 이거나 `verify_seal(node.validators, header(current.preprepare.proposal), uint32(m.round), PREPARE_SEAL, m.prepare_seal, m.source)` 가 실패하면, state 를 바꾸지 않은 채 반드시 `ERR` 로 거부해야 한다.
Source: consensus/wbft/core/prepare.go:92-109, consensus/wbft/core/core.go:471-488

`verify_seal(vs, header, round, type, seal, sealer)` 는 `seal` 이 BLS signature 로 decode 되고 `bls_verify(bls_public_key(vs, sealer), seal_data(header, round, type), seal)` 가 성립할 때 성공한다 (`A-02`).

Implementation note (informative). 레퍼런스의 `verifySeal` 은 `sealer` 가 `vs` 의 원소인지 확인하지 않고 `vs` 에서 찾는다. 즉 보낸 사람이 넘겨받은 집합의 원소라고 가정한다. 모든 호출 경로가 이 가정을 지킨다. PREPARE 와 COMMIT 을 받을 때는 메시지 signature 를 검사한 집합인 `node.validators` 를 넘긴다. extra seal 경로(§12.2)는 signature 검사가 prior 집합을 썼을 때에만 prior 집합을 넘긴다. backlog 에서 다시 처리하는 메시지(§13.3)는 signature 를 다시 검사하지 않지만, replay 는 먼저 `node.validators` 에 없는 보낸 사람의 backlog 를 지운다. 새 구현은 이 가정에 기대지 않고 원소인지를 명시적으로 검사할 수 있다. Source: consensus/wbft/core/core.go:452-462,471-474; consensus/wbft/core/extraseal.go:38-40; consensus/wbft/core/backlog.go:160-161,257-263; consensus/wbft/core/handler.go:136-141.

[WBFT-SM-042] 그 밖이면 노드는 반드시 `current.prepares[m.source] = m` 으로 저장해야 한다. 같은 source 의 이전 PREPARE 는 이 값으로 대체된다.
Source: consensus/wbft/core/prepare.go:112-115, consensus/wbft/core/qbft_msg_set.go:66-71

[WBFT-SM-043] 저장한 뒤 `len(current.prepares) >= Q` 이고 `state < Prepared` 이면, 노드는 반드시 다음을 이 순서대로 해야 한다.

1. `current.prepared_round = current.view.round` 로 설정한다.
2. `current.prepares` 의 모든 메시지를 복사해서 `prepared_certificate` 로 설정한다. 각 사본에는 sequence, round, digest, prepare_seal, signature, source 가 들어간다.
3. `current.prepared_block = current.preprepare.proposal` 로 설정한다.
4. `set_state(node, Prepared)` 를 실행한다.
5. `broadcast_commit(node)` (§10.1) 을 실행한다.

handler 는 `OK` 를 돌려준다.
Source: consensus/wbft/core/prepare.go:119-145
Observable: network, log

PREPARE 는 `Preprepared` 에서만 handler 에 닿고 (WBFT-SM-019), 각 PREPARE 는 따로 처리된다. 그래서 전이는 Q 번째 서로 다른 source 가 저장되는 순간에 일어나고, `prepared_certificate` 는 정확히 `Q` 개의 PREPARE 를 담는다. 그 뒤에 도착한 현재 view 의 PREPARE 는 extra seal 이 된다 (§12).

> 해설: lock (`prepared_round`, `prepared_block`) 이 필요한 이유는 safety 이다. 노드는 quorum 이 이 block 을 prepare 했다는 사실을 기억해 두었다가, round 가 넘어가면 그 사실을 ROUND-CHANGE 에 실어 "이 block 이 prepared 되었다" 는 증거로 내민다. prepared certificate 가 그 증거이다 (§11).

---

## 10. COMMIT 과 decision

### 10.1 전송

[WBFT-SM-044] `broadcast_commit(node)` 는 반드시 COMMIT 을 broadcast 해야 한다. 그 COMMIT 의 `sequence`, `round`, `digest` 는 WBFT-SM-040 과 같고, `commit_seal = bls_sign(seal_data(header(current.preprepare.proposal), uint32(current.view.round), COMMIT_SEAL))` 이며, 그 COMMIT 은 signing payload 에 대한 ECDSA 로 서명한다.
Source: consensus/wbft/core/commit.go:36-82
Observable: network

`broadcast_commit` 은 `Prepared` 로 전이할 때에만 불린다. 그래서 합의 core 가 한 번 실행되는 동안 노드는 한 view 에 COMMIT 을 많아야 하나 보낸다.

### 10.2 수신

[WBFT-SM-045] `PROCESS` 로 분류된 COMMIT 은 `m.digest != block_hash(current.preprepare.proposal)` 이거나 `verify_seal(node.validators, header(current.preprepare.proposal), uint32(m.round), COMMIT_SEAL, m.commit_seal, m.source)` 가 실패하면, state 를 바꾸지 않은 채 반드시 `ERR` 로 거부해야 한다. 그 밖이면 노드는 반드시 `current.commits[m.source] = m` 으로 저장해야 한다. 같은 source 의 이전 COMMIT 은 이 값으로 대체된다.
Source: consensus/wbft/core/commit.go:90-118

[WBFT-SM-046] 저장한 뒤 `len(current.commits) >= Q` 이면, 노드는 반드시 `decide(node)` (§10.3) 를 실행해야 한다. handler 는 `OK` 를 돌려준다.
Source: consensus/wbft/core/commit.go:122-130
Observable: log

### 10.3 Decision

[WBFT-SM-047] `decide(node)` 는 반드시 먼저 `set_state(node, Committed)` 를 실행한 뒤, 그 순간의 메시지 집합으로 다음 seal 목록 두 개를 만들어야 한다:

```python
prepared_seals  = [SealData(sealer=index_of(node.validators, x.source), seal=x.prepare_seal)
                   for x in current.prepares.values()]
committed_seals = [SealData(sealer=index_of(node.validators, x.source), seal=x.commit_seal)
                   for x in current.commits.values()]
```

그다음 `decide(node)` 는 반드시 `app.finalize(current.preprepare.proposal, prepared_seals, committed_seals, current.view.round)` 를 호출해야 한다. 각 목록의 순서는 정하지 않는다. 이 목록이 header 의 `PreparedSeal`, `CommittedSeal`, `Round` 가 되는 방법은 `A-08` 이 정한다.
Source: consensus/wbft/core/commit.go:137-173, consensus/wbft/engine/engine.go:90-151
Observable: header

[WBFT-SM-048] COMMIT 은 `Prepared` 에서만, PREPARE 는 `Preprepared` 에서만 handler 에 닿고, 각 메시지는 따로 처리된다. 그러므로 `committed_seals` 는 반드시 decision 을 이끈 `Q` 개의 COMMIT 을 정확히 담고, `prepared_seals` 는 `Prepared` 로 이끈 `Q` 개의 PREPARE 를 정확히 담아야 한다. 노드가 그 view 에 대해 받는 다른 seal 은 모두 extra seal 로 처리한다 (§12).
Source: consensus/wbft/core/backlog.go:174-199, consensus/wbft/core/prepare.go:121, consensus/wbft/core/commit.go:123
Observable: header

block hash 는 `PreparedSeal`, `CommittedSeal`, `Round` 를 제외하고 계산한다 (`A-03`). 그래서 같은 proposal 을 decide 한 여러 노드는 hash 는 같지만 seal 집합은 다를 수 있는 header 를 만든다. 서로 다른 round 에서 decide 했다면 `Round` 값도 다르다. 노드가 어느 변형을 저장하는지는 어느 변형을 먼저 import 하는지에 달려 있다.

### 10.4 Decision 뒤

[WBFT-SM-049] `app.finalize` 가 실패하면, 노드는 반드시 view 와 state 를 바꾸지 않은 채 `current.view.round + 1` 에 대한 ROUND-CHANGE (§11.1) 를 broadcast 해야 한다.
Source: consensus/wbft/core/commit.go:172-176, consensus/wbft/core/roundchange.go:40-43
Observable: network

`app.finalize` 는 seal 목록을 header 에 쓸 수 없을 때에만 실패한다. decide 된 proposal 이 나중에 import 에 실패해도 그 사실은 합의 core 에 알려지지 않으며, 노드는 `Committed` 에 머문다 (WBFT-SM-050).

[WBFT-SM-050] `decide` 를 실행한 뒤 노드는 round timer 를 멈추어서는 안 된다. 노드는 decide 한 view 에서 `Committed` 에 머물다가, 다음 세 경우 중 먼저 처리되는 경우에 그 view 를 떠난다. 첫째, `NewHead` event 가 다음 sequence 를 시작한다 (§6). 둘째, decide 한 round 의 round timer 가 만료된다 (§14.2). 셋째, 더 높은 round 의 ROUND-CHANGE 에 대해 F+1 규칙 (§11.4) 이 발동한다. 노드는 `Committed` 에서도 다른 모든 state 에서처럼 ROUND-CHANGE 를 처리한다 (WBFT-SM-019).
Source: consensus/wbft/core/commit.go:137-179 (no timer call), consensus/wbft/core/handler.go:152-160, consensus/wbft/core/backlog.go:136-147, consensus/wbft/core/roundchange.go:161-169
Observable: network, log

---

## 11. ROUND-CHANGE

### 11.1 전송

[WBFT-SM-051] 노드는 반드시 정확히 다음 경우에만 `broadcast_round_change(node, round)` 를 호출해야 한다.

1. round timeout 뒤에 호출한다. target 은 timeout 을 처리할 때의 `current.view.round` 인 `r` 에 1 을 더한 `r + 1` 이다 (§14.2). 노드는 `start_new_round` 를 먼저 실행한 뒤 이 함수를 부른다. `CATCH_UP` branch 를 탔으면 노드는 이 함수를 부를 때 다음 sequence 의 round 0 에 있다.
2. F+1 규칙 뒤에 호출한다. target 은 새 round 이다 (§11.4).
3. `finalize` 가 실패한 뒤에 호출한다. round 는 바뀌지 않으며 target 은 `current.view.round + 1` 이다 (WBFT-SM-049).
4. retry timeout 에서 호출한다. target 은 retry timer 를 걸 때 쓴 round 이다 (§14.3).

Source: consensus/wbft/core/handler.go:250-263, consensus/wbft/core/roundchange.go:161-169, consensus/wbft/core/commit.go:173-176, consensus/wbft/core/handler.go:170-178

[WBFT-SM-052] `broadcast_round_change(node, round)` 는 반드시 먼저 그 순간의 `current.view.round` 로 retry timer 를 걸고 (`A-06`), 그다음에야 target 을 확인해야 한다. `current.view.round > round` 이면 이 함수는 반드시 아무것도 보내지 않아야 한다.
Source: consensus/wbft/core/roundchange.go:52-61, consensus/wbft/core/core.go:426-450

[WBFT-SM-053] 그 밖이면 노드는 반드시 다음 값을 담은 ROUND-CHANGE 를 broadcast 해야 한다:

| Field | 값 |
|---|---|
| `sequence` | `current.view.sequence` |
| `round` | target `round` |
| `prepared_round` | `current.prepared_round` 이다. 값이 `None` 이면 이 field 를 넣지 않는다 |
| `prepared_digest` | `block_hash(current.prepared_block)`. `prepared_block is None` 이면 zero hash 이다 |
| `prepared_block` | `current.prepared_block` 이다. 값이 `None` 이면 이 field 를 넣지 않는다 |
| `justification` | `prepared_certificate`. `None` 이면 빈 목록이다 |
| signature | signing payload 에 대한 ECDSA signature 이다 (`A-03`). prepared 쌍은 `prepared_round` 와 0 이 아닌 `prepared_digest` 가 모두 있을 때에만 encode 한다 |

Source: consensus/wbft/core/roundchange.go:63-97, consensus/wbft/messages/roundchange.go:43-62, consensus/wbft/messages/roundchange.go:158-168
Observable: network

`justification` 은 `prepared_certificate` 에서 가져오는데, `prepared_certificate` 는 lock 과 수명이 다르다 (§3.1). 그래서 bad-block 규칙 (WBFT-SM-024) 이 lock 을 비운 뒤에는 ROUND-CHANGE 가 prepared 쌍 없이 비어 있지 않은 justification 을 실을 수 있다. 또한 WBFT-SM-028 의 `CATCH_UP` 경우를 거친 뒤에는 ROUND-CHANGE 가 이전 sequence 의 justification 을 실을 수도 있다. 받는 노드는 prepared 쌍 없이 온 justification 을 무시한다 (WBFT-SM-054).

### 11.2 수신과 저장

[WBFT-SM-054] `PROCESS` 로 분류된 ROUND-CHANGE 는 반드시 다음과 같이 처리해야 한다. 이 경우 `m.round >= current.view.round` 가 항상 성립한다. `m.prepared_round` 와 `m.prepared_block` 이 모두 있고 `m.justification` 이 비어 있지 않으면 다음과 같다.

1. `m.prepared_block.number != current.view.sequence` 이면, 노드는 `m` 을 저장하지 않고 반드시 `ERR` 를 돌려주어야 한다.
2. `block_hash(m.prepared_block) != m.prepared_digest` 이면, 노드는 `m` 을 저장하지 않고 반드시 `ERR` 를 돌려주어야 한다. `A-03` 의 decoder 가 이 경우를 이미 거부한다.
3. 그 밖이면 노드는 `(pr, pb, prepares) = (m.prepared_round, m.prepared_block, m.justification)` 으로 `m` 을 추가한다.

그 밖의 모든 경우에는 `m` 이 prepared 쌍을 싣고 있더라도, 노드는 `(pr, pb, prepares) = (None, None, None)` 으로 `m` 을 추가한다.
Source: consensus/wbft/core/roundchange.go:117-151, consensus/wbft/messages/roundchange.go:283-291
Observable: network

[WBFT-SM-055] `round_changes.add(r, m, pr, pb, prepares, Q)` 는 반드시 `m` 을 `messages[uint64(r)][m.source]` 로 저장해야 한다. 같은 round 에 대한 같은 source 의 이전 ROUND-CHANGE 는 이 값으로 대체된다. 그런 다음 아래 세 조건이 모두 성립하면, 이 함수는 `highest_prepared_round[r] = pr`, `highest_prepared_block[r] = pb`, `highest_prepared_justification[r] = prepares` 로 설정한다.

1. `pr is not None` 이다.
2. `highest_prepared_round[r]` 가 설정되지 않았거나 `pr > highest_prepared_round[r]` 이다.
3. `has_matching_round_change_and_prepares(m, prepares, Q)` 가 성립한다.

Source: consensus/wbft/core/roundchange.go:259-284

`highest_prepared_*` 는 round 마다 단조 증가한다. 어떤 source 의 ROUND-CHANGE 가 대체되어도 이 값은 내려가지 않으며, 대체된 메시지가 이 값을 설정한 메시지였어도 마찬가지이다.

### 11.3 Round change 집합 helper

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

[WBFT-SM-056] 위 helper 는 반드시 적힌 그대로 동작해야 한다. 특히 `has_matching_round_change_and_prepares` 는 반드시 source 기준으로 중복을 없애되 처음 나온 메시지를 남겨야 하고, PREPARE 의 sequence 를 확인해서는 안 된다. 그리고 `min_round_above` 는 반드시 메시지 집합이 비어 있는 key 도 후보로 고려해야 한다.
Source: consensus/wbft/core/justification.go:133-151, consensus/wbft/core/justification.go:174-187, consensus/wbft/core/roundchange.go:246-256, consensus/wbft/core/roundchange.go:288-346

### 11.4 F+1 규칙

[WBFT-SM-057] ROUND-CHANGE 가 §11.2 에 따라 추가된 뒤에 (§11.2 가 거부한 ROUND-CHANGE 는 처리를 `ERR` 로 끝낸다) `r0` 를 그 메시지를 처리하기 전의 노드 round 로 두고 `num = higher_round_senders(round_changes, r0)` 로 둔다. `F < num <= F + 1` 이면, 노드는 반드시 `start_new_round(node, min_round_above(round_changes, r0))` 를 실행한 뒤 그 round 로 `broadcast_round_change(node, that round)` 를 실행해야 하고, 이 메시지에 대해 §11.5 의 quorum 규칙을 평가해서는 안 된다. handler 는 `OK` 를 돌려준다.
Source: consensus/wbft/core/roundchange.go:153-169
Observable: network, log

`num` 은 정수이고 `F = (n-1)/3` 은 실수이다. 그래서 구간 `F < num <= F+1` 에는 정수가 정확히 하나, `floor(F) + 1` 만 들어간다. 이 규칙은 count 가 그 값에 *도달하는* 순간에 발동한다. ROUND-CHANGE 를 처리할 때 count 가 이미 그 값보다 크면 규칙은 발동하지 않는다. 노드 자신의 round 가 나아간 뒤에는 이런 일이 생길 수 있다. WBFT-SM-028 의 `CATCH_UP` 경우가 적용되면, 이 count 에는 이전 sequence 에서 ROUND-CHANGE 가 저장된 source 도 들어간다.

### 11.5 Proposer 의 quorum 규칙

[WBFT-SM-058] 다음 네 조건이 모두 성립하면 노드는 반드시 proposal 을 골라야 한다.

- F+1 규칙이 발동하지 않았다.
- `count_at_round(round_changes, r0) >= Q` 이다.
- 노드가 `node.validators` 의 proposer 이다.
- `current.preprepare_sent < r0` 이다.

proposal 은 다음 순서로 고른다.

1. `highest_prepared_block[r0]` 이 설정되어 있으면 그 block 을 고른다.
2. 그렇지 않고 `current.pending_request` 가 설정되어 있으면 그 request 를 고른다.
3. 둘 다 없으면 노드는 `ERR` 를 돌려주고 아무것도 보내지 않는다. 이때 그 ROUND-CHANGE 는 relay 되지 않는다.

Source: consensus/wbft/core/roundchange.go:170-186

[WBFT-SM-059] `rcs = list(round_changes.messages[r0].values())` 로 두고 `ps = round_changes.highest_prepared_justification[r0]` 로 둔다. 그 값이 설정되지 않았으면 `ps` 는 빈 목록이다. 이때 노드는 반드시 `is_justified(proposal, current.view, [rc.payload for rc in rcs], ps, Q)` 를 실행해야 한다. 결과가 거짓이면 노드는 반드시 아무것도 보내지 않고 `OK` 를 돌려주어야 한다. 결과가 참이면 노드는 반드시 `send_preprepare(node, proposal, rcs, ps)` (§8.1) 를 실행하고 `OK` 를 돌려주어야 한다.
Source: consensus/wbft/core/roundchange.go:188-216
Observable: network

그래서 PRE-PREPARE 는 노드가 그 round 에 대해 가진 ROUND-CHANGE 를 *모두* 싣는다. 그 수는 `Q` 보다 많을 수 있고, 순서는 정하지 않는다. 또한 PRE-PREPARE 는 가장 높은 prepared ROUND-CHANGE 의 justification PREPARE 를, 그 ROUND-CHANGE 가 실었던 그대로 싣는다.

[WBFT-SM-060] 두 규칙이 모두 발동하지 않은 ROUND-CHANGE 는 반드시 §11.2 대로 저장해야 하며, handler 는 `OK` 를 돌려준다.
Source: consensus/wbft/core/roundchange.go:213-216

### 11.6 `is_justified`

[WBFT-SM-061] `is_justified(proposal, target_view, round_changes, prepares, Q)` 는 반드시 다음 단계를 이 순서대로 평가해서, 모든 단계를 통과할 때에만 참을 돌려주어야 한다:

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

알고리즘에 대한 참고 사항은 다음과 같다.

- 1 단계(중복 제거)와 3 단계(오래된 view 거부)는 go-stablenet PR #84 와 #85 가 추가했다. 두 단계는 `v1.1.0` 과 레퍼런스 commit 에 모두 들어 있으며, 두 버전에서 이 단계를 구현한 파일은 내용이 같다. 이 단계가 없는 노드는 한 validator 의 메시지를 반복한 justification 이나, 이전 view 의 ROUND-CHANGE 를 재사용한 justification 을 받아들인다. conforming 노드는 반드시 그런 justification 을 거부해야 한다.
- 6 단계는 `prepared_round == 0` 이고 digest 가 zero 인 ROUND-CHANGE 를 "prepared 되지 않음" 으로 센다. round 0 에서 prepared 된 노드의 ROUND-CHANGE 는 digest 가 0 이 아니므로, 6 단계는 그 ROUND-CHANGE 를 세지 않는다.
- 6 단계는 reference 와 같이 loop 안에서 센 값이 정확히 `Q` 가 되는 순간 참을 돌려준다 (`consensus/wbft/core/justification.go:102`). 7 단계는 reference 와 같이 loop 안에서 `>=` 로 비교한다 (`:122`). 센 값이 이미 `Q` 를 넘은 뒤에 `has_match` 가 참이 될 수 있기 때문이다. 두 형태가 단순히 "센 값이 `Q` 이상" 인지 보는 것과 다른 경우는 `Q = 0` 뿐인데, 모든 `n` 에서 `quorum_size(n) >= 1` 이므로 (`A-04` §2) 이 경우는 생기지 않는다.
- 이 알고리즘은 `prepared_round < target_view.round` 인지 확인하지 않고, PREPARE 의 sequence 도 확인하지 않는다.

> 해설: 의사코드의 단계를 문장으로 풀어 쓰면 다음과 같다.
>
> 1. ROUND-CHANGE 목록과 PREPARE 목록에서 각각 보낸 사람이 같은 메시지가 여럿이면 첫 메시지만 남기고 나머지는 버린다.
> 2. ROUND-CHANGE 가 `Q` 개 이상이어야 한다.
> 3. 모든 ROUND-CHANGE 의 sequence 와 round 가 목표 view 와 같아야 한다.
> 4. PREPARE 는 하나도 없거나 `Q` 개 이상이어야 한다.
> 5. PREPARE 가 있으면 모든 PREPARE 의 round 가 같아야 하고, 모든 PREPARE 의 digest 가 제안된 block 의 hash 와 같아야 한다.
> 6. PREPARE 가 없으면 prepared 쌍이 없는 ROUND-CHANGE 가 `Q` 개 이상이어야 한다. 이 조건은 "quorum 이 아무것도 prepare 하지 않았다고 말했으므로 새 block 을 제안해도 된다" 는 뜻이다.
> 7. PREPARE 가 있으면 prepared round 가 그 PREPARE 의 round 이하인 ROUND-CHANGE 가 `Q` 개 이상이어야 하고, 그중 하나의 prepared 쌍은 정확히 (그 round, 제안된 block 의 hash) 여야 한다.
>
> 1 단계나 3 단계를 빠뜨리면 safety 가 깨진다. 3 단계는 safety 논증의 첫 번째 경우, 즉 "아무도 prepare 하지 않았다" 는 증거를 다른 round 나 sequence 의 ROUND-CHANGE 로 위조하지 못하게 막는다 (§18.2).

[WBFT-SM-062] round 가 0 보다 큰 PRE-PREPARE 를 받는 노드는 반드시 `target_view = m.view` 와 자기 현재 validator 집합의 `Q` 로 `is_justified` 를 적용해야 한다 (WBFT-SM-037). 보내는 노드는 보내기 전에 반드시 `target_view = current.view` 로 `is_justified` 를 적용해야 한다 (WBFT-SM-059).
Source: consensus/wbft/core/preprepare.go:135-145, consensus/wbft/core/roundchange.go:197-205
Observable: network

---

## 12. Extra seal

### 12.1 목적

노드가 quorum 을 넘긴 뒤에 도착한 seal 은 *extra seal* 로 보관한다. head block 의 extra seal 은 노드가 다음에 제안하는 block 의 `PrevPreparedSeal` / `PrevCommittedSeal` 에 합쳐진다 (`A-08`). extra seal 은 header 에 영향을 주고, sealer bitmap 을 통해 diligence 에도 영향을 준다 (`A-04`).

> 해설: 이 구조 덕분에 header 에는 "누가 이 block 에 서명했는가" 가 quorum 보다 넓게 남는다. 그래서 늦게 도착한 validator 도 diligence 점수를 받을 수 있다.

### 12.2 수락

[WBFT-SM-063] `EXTRA_SEAL` 로 분류된 메시지에 대해, 노드는 반드시 대상 block 과 validator 집합을 다음과 같이 골라야 한다.

- `state == AcceptRequest` 이면 `block = prior.proposal`, `vs = prior.validators` 이다.
- 그 밖이면 `block = current.preprepare.proposal`, `vs = node.validators` 이다. 노드가 받아들인 PRE-PREPARE 가 없으면 `block` 은 `None` 이다.

`block is None` 이면 노드는 반드시 아무것도 저장하지 않고 `OK` 를 돌려주어야 한다. 그러면 그 메시지는 relay 된다.
Source: consensus/wbft/core/extraseal.go:29-48

[WBFT-SM-064] 그 밖이면 노드는 다음과 같이 처리한다.

1. PREPARE 는 `m.digest != block_hash(block)` 이거나 `verify_seal(vs, header(block), uint32(m.round), PREPARE_SEAL, m.prepare_seal, m.source)` 가 실패하면 반드시 `ERR` 로 거부해야 하고, 그 밖이면 `store_extra(extra_prepare_seals, m)` 로 저장해야 한다.
2. 노드는 COMMIT 도 같은 방식으로 처리하되, `COMMIT_SEAL`, `m.commit_seal`, `extra_commit_seals` 를 쓴다.
3. 그 밖의 code 는 반드시 `ERR` 로 거부해야 한다.

extra seal 이 저장되었거나 무시되면 처리 결과는 `OK` 이고, 그 메시지는 relay 된다 (WBFT-SM-011).
Source: consensus/wbft/core/extraseal.go:50-87, consensus/wbft/core/handler.go:218-222
Observable: network

### 12.3 저장

[WBFT-SM-065] `store_extra(map, m)` 은 반드시 source 하나당 메시지를 많아야 하나 보관해야 한다. map 에 `m.source` 의 메시지가 이미 있고 그 메시지의 view 가 `m.view` 이상이면 이 함수는 `m` 을 저장하지 않고, 그 밖이면 `m` 을 저장한다.
Source: consensus/wbft/core/extraseal.go:89-115

### 12.4 Effective seal

[WBFT-SM-066] `add_effective_seals_to_extra_seals(node)` 는 반드시 `current.prepares` 의 모든 메시지에 `store_extra` 를 적용해 `extra_prepare_seals` 에 넣고, `current.commits` 의 모든 메시지에 `store_extra` 를 적용해 `extra_commit_seals` 에 넣어야 한다.
Source: consensus/wbft/core/extraseal.go:118-129

이 함수는 노드가 sequence 를 떠날 때 (WBFT-SM-023), 떠나는 round 의 메시지 집합에 대해 실행된다. 그 순간 노드가 decide 한 round 에 있지 않았다면, 예를 들어 decide 뒤에 다음 round 로 넘어갔다면 (WBFT-SM-050), 그 집합은 비어 있거나 다른 round 의 것이다.

### 12.5 사용과 삭제

[WBFT-SM-067] application 이 다음 proposal 의 header 를 만들 때 (`A-08`), 노드는 반드시 다음 함수로 고른 extra seal 을 제공해야 한다:

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

합의 core 가 돌고 있지 않으면 두 목록은 모두 비어 있다. 이전 block 의 seal 에 합치는 규칙은 `A-08` 이 정하며, 그 규칙은 이미 들어 있는 sealer 를 건너뛴다.
Source: consensus/wbft/core/extraseal.go:133-183, consensus/wbft/backend/engine.go:154-167, consensus/wbft/backend/engine.go:221-229, consensus/wbft/engine/engine.go:527-532, consensus/wbft/engine/engine.go:1371-1397
Observable: header

선택할 때에는 노드의 `prior.round` 를 쓰지만, 합칠 때 쓰는 `PrevRound` 는 노드가 저장한 head header 의 `Round` 에서 온다. 노드가 저장한 head 가 다른 round 에서 decide 된 변형이면 (WBFT-SM-048) 두 값이 다르고, 그러면 합친 aggregate 는 verify 되지 않는다.

[WBFT-SM-068] `clear_extra_seals(node, n)` 은 반드시 sequence 가 `n` 보다 작은 extra seal 을 모두 지워야 한다. 이 함수는 `start_new_round` 가 인자 0 으로 불릴 때 `n = head.number` 로 호출되고 (WBFT-SM-028), bad-block 규칙에서 `n = current.view.sequence + 1` 로 호출된다 (WBFT-SM-024). bad-block 규칙의 호출은 이전 sequence 의 extra seal 까지 지운다.
Source: consensus/wbft/core/extraseal.go:186-204

---

## 13. Backlog

### 13.1 입장

[WBFT-SM-069] `add_to_backlog(node, m)` 은 `m.source == node.address` 이면 반드시 `m` 을 버려야 한다.
Source: consensus/wbft/core/backlog.go:210-214

[WBFT-SM-070] 그 밖의 source 에 대해, 노드는 반드시 key `(m.code, uint64(m.sequence), uint64(m.round))` 하나당 backlog 메시지를 많아야 하나 보관해야 한다. 그 source 에 대해 같은 key 가 이미 queue 에 있으면 새 메시지는 반드시 버려야 한다. 그래서 한 slot 에서는 그 slot 이 replay 되거나 빠질 때까지 처음 온 메시지가 유지된다.
Source: consensus/wbft/core/backlog.go:221-235, consensus/wbft/core/backlog.go:242

[WBFT-SM-071] 노드는 반드시 source 하나당 backlog 메시지 수에 상한을 두어야 한다. 레퍼런스 상한은 `MAX_BACKLOG_SIZE_PER_VALIDATOR = 4 * (ROUND_THRESHOLD + 1) * (SEQUENCE_THRESHOLD + 1) = 88` 이며, 이 상한을 넘게 만드는 메시지는 버린다. 이 상한은 로컬 파라미터이다. 구현은 WBFT-SM-019 와 WBFT-SM-070 으로 입장할 수 있는 서로 다른 key 의 수보다 작지 않은 다른 값을 써도 된다.
Source: consensus/wbft/core/backlog.go:44-51, consensus/wbft/core/backlog.go:236-240

입장 검사 (WBFT-SM-069 ~ WBFT-SM-071, 그리고 WBFT-SM-019 의 too-far 규칙) 는 go-stablenet PR #89 가 추가한 방어이다. 어떤 source 의 첫 메시지는 source 별 key 규칙과 크기 상한을 어길 수 없으므로, 노드는 첫 메시지에 대해 두 검사를 건너뛴다.

### 13.2 우선순위

[WBFT-SM-072] 각 source 의 queue 는 반드시 다음 값이 큰 순서로 메시지를 내보내야 한다.

```python
def backlog_priority(m) -> int64:
    if m.code == ROUND_CHANGE:
        return -int64(uint64(m.sequence) * 1000)
    return -int64(uint64(m.sequence) * 1000 + uint64(m.round) * 10 + {PREPREPARE: 1, COMMIT: 2, PREPARE: 3}[m.code])
```

값이 같은 메시지 사이의 순서는 정하지 않는다. 그래서 한 sequence 안에서는 ROUND-CHANGE 가 먼저 나오고, 그다음 메시지는 round 오름차순으로 나오며, 한 round 안에서는 PRE-PREPARE, COMMIT, PREPARE 순서로 나온다.
Source: consensus/wbft/core/backlog.go:34-42, consensus/wbft/core/backlog.go:243, consensus/wbft/core/backlog.go:309-318, common/prque/prque.go:46-51

### 13.3 Replay

[WBFT-SM-073] `set_state` 가 실행될 때마다, state 가 바뀌지 않는 경우를 포함해, 노드는 반드시 `process_backlog` 를 실행해야 한다:

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

source 별로 첫 `FUTURE` 메시지에서 멈추는 규칙과 WBFT-SM-072 의 순서가 합쳐지면 다음 결과가 생긴다. `Preprepared` 에서는 backlog 에 든 어떤 source 의 현재 view COMMIT 이 아직 `FUTURE` 이므로, 그 COMMIT 이 같은 source 의 같은 view PREPARE 가 replay 되는 것을 막는다. 그런 PREPARE 는 노드가 다른 경로로 `Prepared` 에 도달한 뒤에야 replay 되며, 그때는 extra seal 로 처리된다.

[WBFT-SM-074] 노드는 replay 된 메시지의 signature 를 다시 검증해서는 안 된다. 그 메시지의 `source` 는 처음 받을 때 recover 한 값이다. replay 된 메시지를 처리할 때, 노드는 반드시 `Backlog` event 를 처리하는 시점에 `check_message` 를 다시 실행해야 한다 (§15). 그래서 replay 된 메시지는 그 순간에 도착한 것처럼 다뤄진다.
Source: consensus/wbft/core/handler.go:136-142

---

## 14. Event 로서의 timer

시간 값은 `A-06` 이 정한다. 이 절은 각 timer 가 만료될 때 무엇을 하는지 정한다.

### 14.1 Round timer

[WBFT-SM-075] 노드는 branch 를 고른 모든 `start_new_round` 의 끝에서 (WBFT-SM-030), 그리고 PRE-PREPARE 를 받아들일 때마다 (WBFT-SM-039) 반드시 round timer 를 시작하거나 재시작해야 한다. 두 경우 모두 timer 는 `current.view` 에 대한 것이다. round timer 를 시작할 때마다 이전 round timer 가 취소된다. 취소된 timer 의 `RoundTimeout` 이 이미 queue 에 있으면, 노드는 그 event 를 처리할 때 반드시 무시해야 한다. 이 규칙은 합의 core 의 한 실행 안에서만 성립한다. `Stop` 이 timer 들을 취소한 뒤에 event loop 가 건 round timer 는 취소되지 않고, 다시 시작한 core 가 그 만료를 처리한다 (`A-06` WBFT-TIMER-018).
Source: consensus/wbft/core/core.go:331-347, consensus/wbft/core/core.go:405-413, consensus/wbft/core/handler.go:152-160, consensus/wbft/core/preprepare.go:175-185, consensus/wbft/core/handler.go:50-59

### 14.2 Round timeout

§6 과 §11.1 에서 다음 경우가 나온다.

1. 정상인 경우: head 가 아직 `current.view.sequence - 1` 이다. 노드는 round `r + 1` 에 들어가서, 자기 lock 을 실은 ROUND-CHANGE `(sequence, r + 1)` 을 보낸다.
2. decide 한 block 이 아직 import 되지 않은 경우 (WBFT-SM-050): 노드는 경우 1 과 같이 동작하지만, 그 sequence 는 이미 로컬에서 decide 되었다. head 가 도착하면 노드는 `NewHead` 를 처리하면서 새 sequence 의 round 0 으로 catch-up 한다. 다른 validator 들의 timer 가 먼저 만료되었다면 노드는 F+1 규칙 (§11.4) 으로 decide 한 round 를 더 일찍 떠날 수도 있다.
3. head 가 나아간 뒤 `NewHead` event 가 처리되기 전에 늦은 timeout 이 온 경우: `start_new_round` 가 인자 `r + 1` 로 `CATCH_UP` branch 를 탄다 (WBFT-SM-022, WBFT-SM-028, WBFT-SM-029). 노드는 `(head + 1, 0)` 에 들어간 뒤, prepared 쌍은 없지만 이전 sequence 의 `prepared_certificate` 를 justification 으로 실은 ROUND-CHANGE `(head + 1, r + 1)` 을 보낸다. retry timer 는 round 0 으로 걸리므로, 그 뒤의 retry 는 ROUND-CHANGE `(head + 1, 0)` 이다.

### 14.3 Retry timeout

[WBFT-SM-077] `RetryTimeout(r)` 을 받으면 노드는 반드시 `broadcast_round_change(node, r)` 을 실행해야 한다. retry timeout 은 취소할 수 없다. retry timer 를 멈출 때 이미 queue 에 있던 retry timeout 도 처리된다.
Source: consensus/wbft/core/handler.go:170-178, consensus/wbft/core/core.go:417-450
Observable: network

`broadcast_round_change` 는 호출될 때마다 retry timer 를 다시 건다 (WBFT-SM-052). 그래서 ROUND-CHANGE 를 보낸 노드는 round timer 가 시작되어 retry timer 를 멈출 때까지, retry 간격마다 ROUND-CHANGE 를 반복한다. round timer 는 새 round 에 들어갈 때나 PRE-PREPARE 를 받아들일 때 시작된다. 반복되는 메시지의 target 은 timer 를 걸 때 노드가 있던 round 이다. WBFT-SM-049 뒤에는 이 round 가 decide 한 round 이며, timer 를 건 ROUND-CHANGE 의 round 가 아니다.

> 해설: byte 가 같은 재전송은 이미 그 메시지를 가진 peer 에게 wire 로 다시 나가지 않는다 (`A-06` WBFT-TIMER-024). 그래도 self-delivery 는 일어난다. 그래서 proposer 가 자기 retry 를 self-delivery 로 받는 것이, round `r >= 1` 에서 quorum 규칙을 다시 평가하게 만드는 경로가 된다 (§7.2 해설).

---

## 15. 레퍼런스 의사코드

이 절은 완전한 model 이다. 여기서 정의하지 않은 함수는 참조한 절이나 다른 장에서 정의한다. `Q` 와 `F` 는 §1.2 대로 평가한다.

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

## 16. 메시지 처리 결과

아래 표는 받은 합의 메시지의 처리가 끝날 수 있는 모든 경우를 나열한다. "Relay" 열은 WBFT-SM-011/012 에 따른 relay 여부이다.

| # | 상황 | State 변경 | 보내는 메시지 | Relay |
|---|---|---|---|---|
| 1 | code 를 알 수 없거나 payload 를 decode 할 수 없다 | 없음 | 없음 | 아니오 |
| 2 | 메시지나 justification member 의 signature 가 무효이거나, 서명자가 `signature_validator_set` 에 없다 | 없음 | 없음 | 아니오 |
| 3 | `TOO_FAR` | 없음 | 없음 | 아니오 |
| 4 | `OLD` | 없음 | 없음 | 아니오 |
| 5 | `INVALID`: 현재 view 의 PRE-PREPARE 가 `AcceptRequest` 가 아닌 state 에서 도착했다 | 없음 | 없음 | 아니오 |
| 6 | `FUTURE` 이고 backlog 에 입장했다 | backlog 에 메시지와 key 를 추가한다 | 지금은 없다. 나중에 replay 한다 | 지금은 아니오. replay 결과가 `OK` 이면 그때 relay 한다 |
| 7 | `FUTURE` 이지만 자기 메시지이거나, key 가 중복이거나, backlog 가 가득 찼다 | 없음 | 없음 | 아니오. 끝내 relay 하지 않는다 |
| 8 | `EXTRA_SEAL` 이지만 대상 block 이 없다 | 없음 | 없음 | **예** |
| 9 | `EXTRA_SEAL` 이고 PREPARE/COMMIT 의 digest 나 seal 이 무효이다 | 없음 | 없음 | 아니오 |
| 10 | `EXTRA_SEAL` 이고 PREPARE/COMMIT 이 유효하며, 그 source 의 저장된 seal 이 없거나 저장된 것보다 새롭다 | extra seal 을 저장한다 | 없음 | 예 |
| 11 | `EXTRA_SEAL` 이고 PREPARE/COMMIT 이 유효하지만 저장된 것보다 새롭지 않다 | 없음 | 없음 | **예** |
| 12 | `EXTRA_SEAL` 이고 대상 block 이 있는데, 메시지가 PRE-PREPARE 이거나 PREPARE/COMMIT 이 아닌 다른 code 이다 | 없음 | 없음 | 아니오 |
| 13 | PRE-PREPARE 가 proposer 에게서 오지 않았거나, sequence 가 proposal 번호와 다르거나, justify 되지 않았거나, proposal 이 무효이다 | 없음 | 없음 | 아니오 |
| 14 | PRE-PREPARE 의 proposal 이 미래 시각이다 | future-proposal timer 를 건다 | 지금은 없다 | 지금은 아니오. 나중에 받아들여지면 그때 relay 한다 |
| 15 | PRE-PREPARE 를 받아들였다 | PRE-PREPARE 를 저장하고, `Preprepared` 로 가고, round timer 를 재시작하고, backlog 를 replay 한다 | PREPARE | 예 |
| 16 | PREPARE 의 digest 나 seal 이 무효이다 | 없음 | 없음 | 아니오 |
| 17 | PREPARE 를 저장했고 quorum 미만이다 | `prepares` 에 저장한다 | 없음 | 예 |
| 18 | PREPARE 가 quorum 에 도달했다 | lock 과 prepared certificate 를 설정하고, `Prepared` 로 가고, backlog 를 replay 한다 | COMMIT | 예 |
| 19 | COMMIT 의 digest 나 seal 이 무효이다 | 없음 | 없음 | 아니오 |
| 20 | COMMIT 을 저장했고 quorum 미만이다 | `commits` 에 저장한다 | 없음 | 예 |
| 21 | COMMIT 이 quorum 에 도달했고 `finalize` 가 성공했다 | `Committed` 로 가고 backlog 를 replay 한다 | 없다. application 이 block 을 저장한다 | 예 |
| 22 | COMMIT 이 quorum 에 도달했고 `finalize` 가 실패했다 | `Committed` 로 가고, backlog 를 재생하고, retry timer 를 건다 | ROUND-CHANGE(round+1) | 예 |
| 23 | ROUND-CHANGE 의 prepared block 이 다른 sequence 의 것이다 | 없음 | 없음 | 아니오 |
| 24 | ROUND-CHANGE 를 저장했고 어떤 규칙도 발동하지 않았다 | round change 집합에 저장한다 | 없음 | 예 |
| 25 | ROUND-CHANGE 를 저장했고 F+1 규칙이 발동했다 | 새 round 에 들어간다 | ROUND-CHANGE(새 round) | 예 |
| 26 | ROUND-CHANGE 를 저장했고 quorum 규칙에서 justify 되었다 | `preprepare_sent` 를 갱신한다 | PRE-PREPARE | 예 |
| 27 | ROUND-CHANGE 를 저장했고 quorum 규칙의 자기 검사가 실패했다 | 저장 외에는 없음 | 없음 | **예** |
| 28 | ROUND-CHANGE 를 저장했고 quorum 규칙에서 쓸 proposal 이 없다 | 저장 외에는 없음 | 없음 | **아니오** |

Source: consensus/wbft/core/handler.go:111-151, consensus/wbft/core/handler.go:188-227, and the handlers cited in §§8-13

---

## 17. 비동기 처리의 결과

이 절의 requirement 들은 WBFT-SM-002 때문에 conforming 노드가 보일 수 있는 동작의 집합을 정한다. observer 는 이 동작 중 어느 것도 protocol 위반으로 취급해서는 안 된다.

[WBFT-SM-078] 노드 자신의 PREPARE 는 노드가 다른 validator 의 메시지로 이미 `Prepared` 에 도달한 뒤에 처리되어도 된다. 노드 자신의 COMMIT 도 노드가 이미 `Committed` 에 도달한 뒤에 처리되어도 된다. 이 경우 그 PREPARE 나 COMMIT 은 extra seal 로 처리되며, 노드가 만드는 `PreparedSeal` 이나 `CommittedSeal` 에서 빠진다. 마찬가지로 proposer 자신의 PRE-PREPARE 는 그 self-delivery 가 처리될 때에만 처리된다. 그래서 proposer 는 그 self-delivery 뒤에 `Preprepared` 에 들어가고 PREPARE 를 보낸다.
Source: consensus/wbft/backend/backend.go:164-172, consensus/wbft/core/backlog.go:183-199
Observable: header, network

[WBFT-SM-079] backlog 에서 replay 된 메시지는 WBFT-SM-072 의 우선순위와 다른 순서로, 다른 event 와 섞여서 처리되어도 된다. 각 메시지는 처리될 때 다시 분류된다 (WBFT-SM-074).
Source: consensus/wbft/core/backlog.go:303-304

[WBFT-SM-080] 노드가 `NewHead` event 를 처리하기 전에 만료된 round timeout 은 `NewHead` 보다 먼저 처리되어도 된다. 그러면 노드는 §14.2 의 경우 2 나 경우 3 을 보인다. 경우 3 에서 노드는 새 sequence 에 들어가자마자, 그 sequence 에 대해 round 가 0 보다 큰 ROUND-CHANGE 를 보낸다.
Source: consensus/wbft/core/handler.go:152-169, consensus/wbft/core/core.go:185-196
Observable: network

[WBFT-SM-081] retry timer 가 멈추기 전에 queue 에 들어간 `RetryTimeout` 은, 노드가 새 round 에 들어가거나 PRE-PREPARE 를 받아들인 뒤에 처리되어도 된다. 그러면 노드는 현재 round 로 retry timer 를 다시 걸고 (WBFT-SM-052), retry 의 round 가 현재 round 보다 낮지 않으면 ROUND-CHANGE 를 보낸다. 다시 걸린 timer 는 나중에 현재 round 에 대한 ROUND-CHANGE 를 보내며, 노드가 그 round 에서 PRE-PREPARE 를 받아들였더라도 그렇게 한다. `NewHead` 로 새 sequence 에 들어간 노드도 여기에 해당한다. `(h + 1, 0)` 에 있는 노드가 sequence `h` 에서 걸린 `RetryTimeout(r_c)` 를 처리하면, 노드는 prepared 쌍과 justification 이 없는 ROUND-CHANGE `(h + 1, r_c)` 를 보낸다. `r_c >= 1` 이면 그 메시지는 다른 노드의 F+1 규칙 (§11.4) 에 세어진다.
Source: consensus/wbft/core/handler.go:170-178, consensus/wbft/core/roundchange.go:52-61, consensus/wbft/core/handler.go:161-169
Observable: network

[WBFT-SM-082] 노드가 proposer 이고 아직 `AcceptRequest` 에 있을 때, 즉 첫 PRE-PREPARE 가 아직 self-delivery 되지 않았거나, 미래 proposal 로 보류되었거나, 자기 검사에서 거부되었을 때 round 0 에서 현재 sequence 의 request 두 개가 처리되면, 노드는 같은 view 에 proposal 이 서로 다른 PRE-PREPARE 두 개를 보내도 된다.
Source: consensus/wbft/core/request.go:47-56, consensus/wbft/core/preprepare.go:42-51, consensus/wbft/core/preprepare.go:147-169
Observable: network

> **구현 노트 (informative).** 같은 sequence 의 request 두 개는 다음 경우에 생길 수 있다. `FUTURE` 로 저장된 request 는 노드가 그 sequence 에 들어갈 때 replay 된다. 그런데 같은 `start_new_round` 로 통지를 받은 block builder 가 새로 만든 proposal 을 같은 시기에 넘길 수 있다. 받는 노드는 먼저 도착한 PRE-PREPARE 를 받아들이고, 다른 PRE-PREPARE 는 `INVALID` 로 분류한다.

WBFT-SM-082 의 경우를 빼면, proposer 는 합의 코어가 한 번 실행되는 동안 view 하나에 PRE-PREPARE 를 많아야 하나 보낸다. round 0 에서는 request 경로만 PRE-PREPARE 를 보낸다(WBFT-SM-032). round `> 0` 에서는 ROUND-CHANGE quorum 규칙만 PRE-PREPARE 를 보내며, `current.preprepare_sent` 가 그 round 보다 작을 때에만 보낸다(WBFT-SM-058, WBFT-SM-036). PREPARE 와 COMMIT 에 대한 한계(§8.4, §10.1, WBFT-SM-083)와 함께, 이것들이 노드가 스스로 서명하는 메시지에 대한 view 별 한계의 전부이다. 이 한계는 어느 것도 재시작을 넘어서는 성립하지 않는다(WBFT-SM-007). `A-10` WBFT-SEC-040 은 이 두 가지 한정을 따른다.

---

## 18. Safety 와 liveness (informative)

### 18.1 Quorum 교집합

`Q = quorum_size(n) = ceil(n - (n-1)/3) = ceil((2n+1)/3)` 이다 (`A-04`). `Q` 명의 validator 로 이루어진 두 집합은 적어도 `2Q - n >= (n+2)/3` 명의 validator 를 공유하며, 이 수는 `f = floor((n-1)/3)` 보다 크다. 그래서 결함이 있는 validator 가 많아야 `f` 명이면, 두 quorum 은 항상 정직한 validator 를 하나 이상 공유한다.

### 18.2 한 sequence 안의 합의

논증은 QBFT 의 논증과 같다. proposal `B` 가 `(h, r)` 에서 decide 되었다면, 적어도 `Q` 명의 validator 가 round `r` 에서 `B` 에 대한 COMMIT 을 보냈다. 그러므로 적어도 `Q - f` 명의 정직한 validator 가 round `r` 에서 `B` 를 prepare 했고, lock `(r, B)` 또는 그보다 뒤의 lock 을 가지고 있다. 다른 proposal `B'` 가 round `r' > r` 에서 받아들여지려면, 그 PRE-PREPARE 가 `is_justified` 를 만족해야 한다.

- PREPARE 가 없으면, 6 단계는 prepared 쌍이 없는 `(h, r')` 의 ROUND-CHANGE `Q` 개를 요구한다. 그러나 어떤 `Q` 명의 발신자에도 round `>= r` 에서 lock 을 가진 정직한 validator 가 들어 있으므로, 이 조건은 만족될 수 없다.
- round `pr` 의 PREPARE 가 있으면, 7 단계는 round `pr` 에서 `B'` 에 대한 PREPARE quorum 과, prepared round 가 `<= pr` 인 ROUND-CHANGE `Q` 개를 요구한다. `pr < r` 이면 어떤 정직한 발신자가 `>= r > pr` 인 lock 을 보고한다. `pr >= r` 이면, round `pr` 에서 `B'` 에 대한 PREPARE `Q` 개가 있으려면 어떤 정직한 validator 가 `B` 에 lock 을 건 뒤 `B'` 를 prepare 해야 한다. round 에 대한 귀납법에 따르면, 정직한 validator 는 `B'` 자체가 justify 되었을 때에만 그렇게 한다.

3 단계 (오래된 view 거부) 는 다른 round 나 sequence 의 ROUND-CHANGE 를 replay 해서 첫 번째 경우의 논증을 무너뜨리는 것을 막는다. 1 단계는 한 서명자가 두 번 이상 세어지는 것을 막는다.

### 18.3 레퍼런스 구현이 논증을 약하게 만드는 곳

- **Bad-block unlock.** WBFT-SM-024 에 따라 노드는 application 이 lock 이 걸린 block 을 bad 로 표시하면 lock 을 푼다. 이때 합의는 모든 정직한 validator 가 같은 판정에 도달한다는 가정에 기댄다. 이 가정은 실행이 결정적이라는 조건 (`A-09`) 에서 나온다.
- **확인하지 않는 prepared round.** protocol 은 ROUND-CHANGE 에서 `prepared_round < round` 를 요구하지 않는다. justification 규칙은 이 조건에 의존하지 않는다.

### 18.4 Liveness

liveness 는 partial synchrony 가정 아래에서 QBFT 논증을 따른다. round timer 는 round 가 커질수록 길어진다 (`A-06`). F+1 규칙은 뒤처진 validator 를 적어도 한 정직한 validator 가 도달한 round 로 끌어올린다. retry timer 는 원래 전송 뒤에 연결된 peer 도 ROUND-CHANGE 를 받도록 ROUND-CHANGE 를 다시 보낸다. 다만 byte 가 같은 재전송은 이미 그 메시지를 가진 peer 에게 다시 보내지 않는다 (`A-06` WBFT-TIMER-024). QBFT 형식 명세는 liveness 를 증명하지 않는다 (§18.5). 그러므로 이 논증은 QBFT 에 대해서도 비형식 논증이다.

### 18.5 QBFT 형식 명세와의 관계

ConsenSys 의 QBFT 형식 명세(`github.com/Consensys/qbft-formal-spec-and-verification`, commit `1630128e7`, Dafny, 추상화 수준 L1)는 노드 하나를 state machine 으로 모델링하고 (`dafny/spec/L1/node.dfy`, `dafny/spec/L1/node_auxiliary_functions.dfy`), 네트워크와 byzantine 공격자를 분산 시스템으로 모델링한다 (`dafny/ver/L1/distr_system_spec/`). 그리고 그 모델에 대해 safety 성질 두 가지를 증명한다. 이 절은 그 성질을 WBFT 의 말로 옮기고, 증명이 기대는 가정을 WBFT 가 만족하는지 적고, 형식 명세의 정의 가운데 WBFT 가 만족하지만 다른 곳에 적혀 있지 않은 세 가지를 적는다. 이 절의 경로는 그 저장소 안의 경로이다. state machine 을 요소별로 비교한 표는 `A-13` §3.4 에 있다.

**증명된 성질.** 증명(`dafny/ver/L1/theorems.dfy:20-67`)은 모델이 허용하는 모든 trace 에서 다음이 성립함을 보인다.

- *consistency*: 어느 시점에서든 정직한 두 노드의 chain 은 한쪽이 다른 쪽의 prefix 이다.
- *consistency and stability*: 한 정직한 노드의 어느 시점 chain 과 다른 정직한 노드의 다른 어느 시점 chain 사이에서도 같은 관계가 성립한다. 그러므로 정직한 노드는 한 번 붙인 block 을 다른 block 으로 바꾸지 않는다.

두 성질은 chain 을 commit seal 과 round 번호를 뺀 채로 비교한다. `consistentBlockchains` 는 header 에 proposer, height, timestamp 만 남긴 "raw" block 을 비교한다 (`dafny/ver/L1/theorems_defs.dfy:10-16`, `dafny/spec/L1/types.dfy:31-62`). 이 비교 대상은 WBFT 의 block hash 와 같다. WBFT 의 block hash 도 `PreparedSeal`, `CommittedSeal`, `Round` 를 빼고 계산하기 때문이다 (`A-03` §6). 그래서 두 성질을 WBFT 의 말로 옮기면 이렇다. 어느 두 시점에서든, 정직한 두 노드의 chain 을 block hash 의 나열로 보면 한쪽이 다른 쪽의 prefix 이다. 정직한 두 노드는 같은 block hash 에 대해 서로 다른 seal 집합과 서로 다른 `Round` 값을 가질 수 있다 (WBFT-SM-048). 형식 모델도 이것을 허용한다. 모델에서 decide 하는 노드는 제안된 block 을 자기 chain 에 붙이고, 자기가 모은 commit seal 을 실은 사본은 따로 multicast 한다 (`dafny/spec/L1/node.dfy:364-386`). liveness 는 증명되지 않았다 ("Formal verification of the liveness of QBFT is still pending", `README.md:15`). 그러므로 §18.4 는 WBFT 에 대해서도 QBFT 에 대해서도 비형식 논증이다.

**증명의 가정과 WBFT.**

| 가정 | 형식 명세 | WBFT | 결과 |
|---|---|---|---|
| validator 집합이 바뀌지 않는다 | `axiomRawValidatorsNeverChange`, `dafny/ver/L1/support_lemmas/axioms.dfy:17-18` | epoch block 마다 집합이 바뀐다 (`A-04` §4, §6) | validator 집합이 이전 높이와 다른 높이는 증명이 다루지 않는다. epoch 경계의 safety 는 모든 정직한 노드가 같은 `validators_at(h)` 를 계산한다는 점 (`A-04`) 과 WBFT-SEC-001 이 모든 높이에서 성립한다는 점에 기대며, 둘 다 증명되지 않았다. 다음 sequence 의 메시지는 현재 집합으로 검증된다 (WBFT-SM-016). |
| byzantine 노드는 많아야 `f(|V|)` 명이고, 처음부터 정해져 있다 | `AdversaryInit`, `dafny/ver/L1/distr_system_spec/adversary.dfy:18-23` | 높이마다 byzantine validator 가 많아야 `f` 명이다 (`A-10` WBFT-SEC-001) | 집합이 바뀌지 않는 동안에는 같은 가정이다. |
| 정직한 노드는 자기 상태 전체를 영원히 유지한다 | `NodeNext` 에는 crash 나 재시작 단계가 없다, `dafny/spec/L1/node.dfy:67-105`. `DSNextNode`, `dafny/ver/L1/distr_system_spec/distributed_system.dfy:56-80` | 재시작하면 합의 상태가 하나도 남지 않는다 (WBFT-SM-007) | 한 높이 안에서 재시작하면 노드는 lock 과 보낸 투표의 기록을 잃는다. 증명은 이 둘에 기댄다 (§18.3). |
| lock 은 그 높이의 block 을 붙일 때에만 풀린다 | `lastPreparedBlock` 은 `UponCommit` 과 `UponNewBlock` 에서만 비워진다, `dafny/spec/L1/node.dfy:379-386, 407-413` | bad-block 규칙이 round change 에서 lock 을 푼다 (WBFT-SM-024) | 이때 합의는 실행이 결정적이라는 조건에 기댄다 (§18.3, `A-10` §9). |
| `digest` 는 단사 함수이다 | `lemmaDigest`, `dafny/spec/L1/node_auxiliary_functions.dfy:52-53` | `block_hash` 는 Keccak-256 이다 (`A-02` §2, `A-03` §6) | hash 를 쓰는 모든 protocol 과 마찬가지로 계산적 의미(충돌 저항성)에서 만족한다. |
| payload 와 서명자마다 signature 는 하나이고, 서명자를 recover 할 수 있으며, signature 는 위조할 수 없다 | `lemmaSigned*`, `dafny/spec/L1/node_auxiliary_functions.dfy:56-93`. `:32-38` 의 주석 | ECDSA signature 는 `S` 의 두 형태를 모두 받아들인다 (`A-10` §3). BLS seal 은 결정적이다 | ECDSA 에서는 signature 가 하나라는 성질이 성립하지 않는다. 그러나 WBFT 는 이 성질에 기대지 않는다. 메시지 집합은 recover 한 source 마다 메시지 하나만 둔다 (WBFT-SM-042, WBFT-SM-045, WBFT-SM-055). `is_justified` 도 source 하나를 한 번만 센다 (WBFT-SM-061 1 단계). |
| 비동기 네트워크: 보낸 메시지는 한없이 늦게 오거나 아예 오지 않을 수 있지만, 바뀌지는 않는다 | `NetworkDeliverNext`, `dafny/ver/L1/distr_system_spec/network.dfy:29-43`. `AdversaryNext`, `dafny/ver/L1/distr_system_spec/adversary.dfy:25-82` | `A-10` §1 (partial synchrony 는 liveness 에만 가정한다). relay (`A-07`) | safety 성질에는 시간 가정이 필요 없다. relay 는 보낸 메시지의 사본을 전달하는 것이고, 모델이 이미 허용하는 일이다. |

**WBFT 가 만족하는 형식 명세의 정의.**

- *lock 불변식* (`validNodeState`, `dafny/spec/L1/node_auxiliary_functions.dfy:840-855`): prepared round 와 prepared block 은 함께 있거나 함께 없다. 둘이 있는 동안에는 그 둘에 대한 유효한 PREPARE 를 quorum 만큼 받은 상태이다. WBFT 에서 `prepared_round` 와 `prepared_block` 은 PREPARE quorum 에서 함께 설정되고, 새 sequence 나 bad-block 규칙으로 함께 비워진다. 둘이 설정되어 있는 동안 `prepared_certificate` 는 그 둘을 설정한 PREPARE 를 정확히 `Q` 개 담는다. 그 PREPARE 들의 source 는 서로 다르고, 모두 `(current.view.sequence, prepared_round, block_hash(prepared_block))` 에 대한 것이다 (consensus/wbft/core/prepare.go:119-138, consensus/wbft/core/core.go:249-252, consensus/wbft/core/core.go:278-294). 역은 성립하지 않는다. `prepared_certificate` 는 쌍보다 오래 남을 수 있다 (WBFT-SM-053).
- *seal 을 뺀 chain 의 validator 집합* (`validators`, `dafny/spec/L1/node_auxiliary_functions.dfy:212-225`): 한 높이를 decide 하는 집합은 commit seal 을 뺀 chain 의 함수이다. WBFT 에서 `validators_at(h)` 는 hash 가 덮는 데이터만 읽는다. 그 데이터는 그 높이를 다스리는 epoch block 의 extra 에 든 `EpochInfo` 이고, `EpochInfo` 를 계산할 때는 hash 가 덮는 header 의 이전 block seal bitmap, `Coinbase`, `MixDigest` 와 epoch block 의 post-state 이다 (`A-04` §3.1, §6.1. diligence 계산은 `PrevPreparedSeal` 과 `PrevCommittedSeal` 을 읽는다, consensus/wbft/engine/engine.go:717-729). 저장된 header 의 노드별 `PreparedSeal`, `CommittedSeal`, `Round` 는 입력이 아니다. 그래서 같은 block 들의 seal 변형을 서로 다르게 가진 정직한 노드들도 같은 집합을 계산한다.
- *round 와 무관한 재제안* (`replaceRoundInBlock`, `dafny/spec/L1/node_auxiliary_functions.dfy:276-283, 548-553`): 형식 모델의 block 은 자기 round 번호를 담는다. 그래서 round `pr` 에서 prepare 되고 round `r` 에서 다시 제안된 block 은 round 를 `pr` 로 되돌린 뒤 prepared digest 와 비교된다. WBFT 는 `Round` 를 `block_hash` 에서 뺀다 (`A-03` §6). 그래서 다시 제안된 block 은 바이트까지 같고, 비교는 그냥 `block_hash` 가 같은지 보는 것이다 (WBFT-SM-054, WBFT-SM-061 5 단계와 7 단계). 대신 round 는 seal 이 묶는다 (`A-02` §6).

**WBFT 규칙이 모델보다 약한 곳.** `is_justified` 는 형식 명세의 `isProposalJustification` (`dafny/spec/L1/node_auxiliary_functions.dfy:515-562`) 이 거부하는 justification 일부를 받아들인다. `is_justified` 는 prepare 되지 않은 ROUND-CHANGE 가 전부가 아니라 `Q` 개이기만 하면 되고, prepared round 가 justify 하는 round 이하인 ROUND-CHANGE 도 전부가 아니라 `Q` 개이기만 하면 된다. 또 justification PREPARE 의 sequence 와 `prepared_round < round` 를 검사하지 않는다 (`A-13` §3.4). 앞의 두 완화는 논문 규칙의 quorum 부분집합 형태이고, §18.2 는 이 형태 위에서 논증한다. block 번호와 다른 sequence 의 PREPARE 는 정직한 validator 에게서 나올 수 없다. 정직한 validator 는 proposal 의 번호가 PRE-PREPARE 의 sequence 와 같을 때에만 그 proposal 에 PREPARE 를 보내고 (WBFT-SM-037), digest 는 그 proposal 의 hash 이기 때문이다. `prepared_round == round` 인 정직한 ROUND-CHANGE 는 retry timer 에서만 나온다 (WBFT-SM-090). 이 논증들은 비형식이며, 증명은 이 경우들을 다루지 않는다.

---

## 19. Observer 가 확인할 수 있는 성질

이 성질들은 v0.1 초안의 불변식 I-08 ~ I-15, I-23, I-24 를 코드에 맞춰 고쳐서 다시 적은 것이다. requirement 로 적은 성질은 정직한 노드에 대한 규범이다. "한 번의 실행 안에서" 는 그 노드의 합의 core 가 시작된 뒤 멈추기 전까지를 뜻한다.

[WBFT-SM-083] (I-08) 한 번의 실행 안에서 정직한 노드는 같은 view 에 digest 가 다른 PREPARE 두 개, 또는 digest 가 다른 COMMIT 두 개를 보내서는 안 된다. 노드가 재시작하면 재시작 전과 후를 합쳐서는 이 성질이 성립하지 않는다 (WBFT-SM-007).
Source: consensus/wbft/core/backlog.go:166-201, consensus/wbft/core/preprepare.go:172, consensus/wbft/core/prepare.go:121
Observable: network

[WBFT-SM-084] (I-09) 정직한 노드가 보내는, round 가 0 보다 큰 모든 PRE-PREPARE 는 반드시 그 view 의 proposer 가 서명해야 하고, 반드시 `is_justified(proposal, view, justification_round_changes, justification_prepares, Q)` 를 만족해야 한다.
Source: consensus/wbft/core/roundchange.go:170-212, consensus/wbft/core/preprepare.go:51
Observable: network

[WBFT-SM-085] (I-10) 정직한 노드가 보내는 모든 PRE-PREPARE 는 반드시 `proposal.number == sequence` 를 만족해야 하고, 정직한 노드가 한 view 에 보내는 모든 PREPARE 와 COMMIT 은 반드시 `digest == block_hash(p)` 를 실어야 한다. 여기서 `p` 는 그 노드가 그 view 에 받아들인 PRE-PREPARE 의 proposal 이다.
Source: consensus/wbft/core/preprepare.go:51, consensus/wbft/core/prepare.go:39-48, consensus/wbft/core/commit.go:41-50
Observable: network

[WBFT-SM-086] (I-11) 정직한 노드의 decision 으로 seal 이 쓰인 header 에서, `CommittedSeal` 과 `PreparedSeal` 은 각각 반드시 정확히 `Q(V(h))` 명의 sealer 를 가져야 하고, 그 sealer 들은 반드시 `(h, Round(h))` 에 대해 `digest == block_hash(h)` 인 메시지를 보낸 validator 여야 한다. `CommittedSeal` 의 sealer 는 그런 COMMIT 을, `PreparedSeal` 의 sealer 는 그런 PREPARE 를 보낸 validator 이다. 다른 노드에게서 block `h` 를 import 한 노드는 그 노드의 header 를 저장한다. 그래서 두 정직한 노드가 같은 block hash 에 대해 서로 다른 sealer 집합과 서로 다른 `Round(h)` 를 가질 수 있다 (WBFT-SM-048). sealer 가 적어도 `Q` 명이어야 한다는 verify 규칙은 `A-08` 이 정한다.
Source: consensus/wbft/core/commit.go:137-173
Observable: header, network

[WBFT-SM-087] (I-12, 수정됨) 정직한 노드가 한 view `(h, r)` 에 보내는 모든 ROUND-CHANGE 는 반드시 같은 `(prepared_round, prepared_digest)` 를 실어야 한다. 예외는 네 가지이다. 첫째, round `r` 에서 decide 했고 `finalize` 가 실패한 노드는 나중에 retry timer 로 `prepared_round == r` 인 ROUND-CHANGE `(h, r)` 을 보내도 된다 (WBFT-SM-049, WBFT-SM-052). 둘째, 늦은 timeout 의 `CATCH_UP` 경우 (WBFT-SM-080) 에 보낸 ROUND-CHANGE 는 prepared 쌍을 싣지 않는다. 노드가 그 뒤 새 sequence 의 앞선 round 에서 lock 을 얻고 같은 view 의 ROUND-CHANGE 를 prepared 쌍과 함께 다시 보내더라도 그렇다. 셋째, bad-block 규칙 (WBFT-SM-024) 이 같은 view 의 두 ROUND-CHANGE 사이에서 prepared 쌍을 없애도 된다. 넷째, WBFT-SM-081 의 경합도 예외이다.
Source: consensus/wbft/core/roundchange.go:52-97, consensus/wbft/core/core.go:426-450, consensus/wbft/core/core.go:185-195, consensus/wbft/core/core.go:278-286
Observable: network

[WBFT-SM-088] (I-13) 정직한 노드가 보내는 ROUND-CHANGE 가 prepared 쌍을 실으면, 그 ROUND-CHANGE 는 반드시 `block_hash(prepared_block) == prepared_digest` 이고 `prepared_block.number == sequence` 인 `prepared_block` 을 싣고, round 가 `prepared_round` 이고 digest 가 `prepared_digest` 인 서로 다른 source 의 PREPARE 정확히 `Q` 개로 이루어진 `justification` 을 실어야 한다. prepared 쌍이 없는 ROUND-CHANGE 도 비어 있지 않은 justification 을 실어도 된다 (WBFT-SM-053).
Source: consensus/wbft/core/prepare.go:125-137, consensus/wbft/core/roundchange.go:63-82
Observable: network

[WBFT-SM-089] (I-15, 수정됨) 정직한 노드는 sequence `h` 에서 target round 가 `r + 1` 인 첫 ROUND-CHANGE 를 반드시 다음 상황 중 하나에서만 보내야 한다.

1. `(h, r)` 의 round timer 가 만료되었다. 이 만료는 노드가 round `r` 에 들어간 시각과 그 round 에서 PRE-PREPARE 를 받아들인 시각 중 늦은 시각으로부터 적어도 `round_timeout(r)` 뒤에 일어난다 (`A-06`).
2. F+1 규칙이 발동했다.
3. `(h, r)` 에 대한 `finalize` 가 실패했다.
4. 이전 sequence 의 늦은 round timeout 이 `CATCH_UP` branch 를 탔다 (WBFT-SM-080). 이 경우 `r + 1` 은 노드가 sequence `h - 1` 에서 가졌던 round 보다 하나 크다.
5. sequence `h - 1` 에서 걸린 retry timeout 이 노드가 sequence `h` 에 들어간 뒤에 처리되었다 (WBFT-SM-081). 이 경우 target 은 그 retry timer 를 걸 때 노드가 sequence `h - 1` 에서 가졌던 round 이다.
6. 합의 core 의 이전 실행이 건 round change timer 나 retry timer 가 재시작 뒤에 만료되었다 (`A-06` WBFT-TIMER-018). 이 경우 target 은 그 timer 에서 나온다. round change timer 이면 target 은 새 core 의 현재 round 에 1 을 더한 값이고, retry timer 이면 그 timer 가 기억한 round 이다. 1번 상황의 시간 조건은 이 경우에 적용되지 않는다.

Source: consensus/wbft/core/handler.go:250-263, consensus/wbft/core/roundchange.go:161-169, consensus/wbft/core/commit.go:173-176, consensus/wbft/core/handler.go:170-178
Observable: network, log

[WBFT-SM-090] (I-23, 수정됨) 정직한 노드가 보내는 ROUND-CHANGE 는 반드시 `prepared_round < round` 를 만족해야 한다. 예외로, retry timer 가 보내는 ROUND-CHANGE 는 `prepared_round == round` 이어도 된다. 이 예외는 두 경우에 생긴다. 첫째, `finalize` 가 실패한 뒤이다 (WBFT-SM-049). 둘째, 늦게 처리된 retry timeout 이 현재 round 로 retry timer 를 다시 건 뒤 (WBFT-SM-081) 노드가 그 round 에서 `Prepared` 에 도달한 경우이다. 받는 노드는 이 성질을 확인하지 않는다.
Source: consensus/wbft/core/roundchange.go:52-63, consensus/wbft/core/roundchange.go:117-151, consensus/wbft/core/handler.go:170-178, consensus/wbft/core/core.go:336-347
Observable: network

[WBFT-SM-091] (I-24) 정직한 노드는 `(h, r)` 을 decide 한 뒤 `h` 의 `NewHead` 를 처리하기 전에 ROUND-CHANGE `(h, r + 1)` 을 보내도 된다 (WBFT-SM-050). observer 는 이 ROUND-CHANGE 를 결함으로 세어서는 안 된다. 이 ROUND-CHANGE 의 빈도는 block import 시간을 `round_timeout(r)` 과 비교하는 지표가 되고, F+1 규칙을 통해 다른 validator 들이 먼저 timeout 된 정도도 반영한다.
Source: consensus/wbft/core/commit.go:137-179, consensus/wbft/core/handler.go:152-160
Observable: network, log

v0.1 초안의 I-14 ("timeout 이 없는 height 는 `round_timeout(0)` 안에 round 0 에서 decide 한다") 는 protocol 의 성질이 아니라 운영상의 기대이다. 그래서 이 장은 I-14 를 requirement 로 다시 적지 않는다.
