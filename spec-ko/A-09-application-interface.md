# A-09 Application interface

- Area code: `APP`
- Status: draft
- Reference: go-stablenet `740526d03`.

이 장은 WBFT 합의 프로토콜이 execution layer(*application*)에 요구하는 것을 추상적으로 정의한다. 기준은 reference 구현이 현재 실현하고 있는 모습이다. 이 장은 연산, 입력, 출력, 실패의 의미, 그리고 합의 구현이 기댈 수 있는 순서와 동시성을 정한다. `B-08` 은 state 를 읽는 연산을 StableNet 컨트랙트 storage 에 묶는다.

이 interface 는 설계된 것이 아니라 관찰된 것이다. reference 구현에서 이 interface 는 `consensus/wbft/core.Backend`(`consensus/wbft/core/types.go:93-142`), `consensus/wbft/backend` 의 `consensus.Engine` 메서드, miner worker, `core.BlockChain` 에 흩어져 있다.

---

## 1. 모델

당사자는 둘이다.

- **Consensus** 는 `A-05` 의 state machine, `A-08` 의 header 규칙, `A-04` 의 validator·epoch 논리이다.
- **Application** 은 block storage 와 head, transaction pool, block 만들기와 실행, state, 시스템 컨트랙트이다.

interface 의 모양은 다음 세 사실이 정한다.

1. **proposer 를 뺀 모든 노드는 합의 뒤에 실행한다.** proposer 는 제안하기 전에 block 을 만들면서 그 block 을 실행한다. 다른 validator 는 PRE-PREPARE 에서 header 규칙과 transaction root 만 검증하고, block 이 finalize 된 뒤에 실행한다(§5).
2. **consensus 는 두 시점에서 application state 를 읽는다.** consensus 는 height 의 parent state 에서 proposer 자격과 gas tip 을 읽고, epoch block 의 실행 뒤 state 에서 candidate 를 읽는다. parent state 를 읽는 일은 header 규칙 안에서 동기적으로 이루어진다(`A-08` H15b, H21). candidate 를 읽는 일은 epoch block 의 finalization 안에서 이루어지며(`B-06` §4), header 검증 중에는 결코 이루어지지 않는다.
3. **실행이 consensus 에 결과를 알려 주지 않는다.** `finalize`(§4.7)는 block 을 넘기고 끝난다. 그 뒤 실행이 실패하면 consensus 는 그 사실을 간접적으로만 알게 된다. consensus 는 timer 가 만료된 뒤 bad block 기록을 확인할 때에야 실패를 알아차린다(§5.3).

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

## 2. 타입

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

## 3. 약속

- "height `n` 의 parent state" 는 block `n−1` 을 실행한 뒤의 state 이며, `ParentHash` 가 가리키는 header 의 `Root` 로 식별한다.
- "block `n` 의 실행 뒤 state" 는 block `n` 의 transaction 들과, `B-06` 의 finalization 단계 가운데 epoch 처리보다 앞선 단계(`n` 에서의 시스템 컨트랙트 업그레이드, base fee 분배)를 실행한 뒤의 state 이다.
- 연산 이름은 추상적인 이름이다. §7 의 표가 이 이름들을 reference 구현에 대응시킨다.

---

## 4. 연산

### 4.1 체인 조회

| 연산 | 돌려주는 값 | Reference |
|---|---|---|
| `head()` | 현재 canonical head 의 `HeadInfo`(full block) | `Backend.LastProposal`, `consensus/wbft/backend/backend.go:327-342`; `currentBlock = chain.CurrentFullBlock`, `miner/worker.go:447` |
| `header_by_number(n)` | canonical 색인 `n` 의 canonical header. 그런 header 가 없으면 아무것도 돌려주지 않는다 | `ChainHeaderReader.GetHeaderByNumber` |
| `header(hash, n)` | 그 hash 를 가지고 canonical 색인이 `n` 인 header. 그런 header 가 없으면 아무것도 돌려주지 않는다 | `ChainHeaderReader.GetHeader` |
| `header_by_hash(hash)` | 그 hash 를 가진 header. 그런 header 가 없으면 아무것도 돌려주지 않는다 | `ChainHeaderReader.GetHeaderByHash` |
| `has_block(hash, n)` | bool | `Backend.HasProposal`, `backend.go:306-308` |
| `proposer_of(n)` | `n` 의 canonical header 의 `Coinbase`, 없으면 zero address | `Backend.GetProposer`, `backend.go:311-317` |
| `is_bad_block(hash)` | bool: `hash` 에 대한 bad block 기록이 있는지 | `Backend.HasBadProposal`, `backend.go:344-349`. 이 함수가 `rawdb.HasBadBlock` 을 호출한다 |
| `now()` | 로컬 wall clock 의 초 | `time.Now()` in `consensus/wbft/engine/engine.go:202, 503` |

`has_block` 과 `proposer_of` 는 reference 의 `Backend` interface 에 들어 있지만(`consensus/wbft/core/types.go:128-134`), reference commit 의 consensus 코어는 둘 다 부르지 않는다.

이 조회들의 번호 인자 `n` 은 `uint64` 인 *canonical 색인* 이다. 즉 `Number` 의 하위 64 비트다. go-ethereum 은 header 를 `(uint64(Number), hash)` 키로 저장하고 canonical 대응도 `uint64(Number)` 로 저장한다. 그래서 `n` 으로 조회하면 `uint64(Number)` 가 `n` 인 header 를 찾는다. `2^64` 보다 작은 번호에서는 번호가 `n` 인 header 다. consensus 는 이 조회들에 `uint64(...)` 값을 넘긴다. 다만 `header(hash, n)` 은 header cache 에 `hash` 의 header 가 있으면 `n` 을 보지 않고 그 header 를 돌려준다.
Source: `core/rawdb/accessors_chain.go:394-400`, `core/blockchain.go:942`, `core/headerchain.go:443-448, 479-485`

[WBFT-APP-001] application 은 반드시 full canonical head block 을 돌려주는 `head()` 를 제공해야 한다. consensus 는 그 block 의 번호, hash, `Coinbase` 를 써서 다음 height 를 시작하고 다음 proposer 를 계산한다.
Source: `consensus/wbft/backend/backend.go:327-342`, `consensus/wbft/core/core.go:178-246`

[WBFT-APP-003] `header_by_number` 는 반드시 그 번호에서 application 이 현재 canonical 로 여기는 header 를 돌려주어야 한다. consensus 는 이 조회를 이전 seal(`WBFT-HDR-030`)과 epoch header 조회(`WBFT-HDR-071`)에 쓴다. 이전 seal 은 번호로만 찾는다. epoch header 는 먼저 번호로 찾고, 그 epoch 번호에 canonical header 가 없을 때에만 처리 중인 header 의 조상 관계를 따라간다.
Source: `consensus/wbft/engine/engine.go:516, 1414-1433`

[WBFT-APP-004] `is_bad_block(hash)` 는 import pipeline 이 bad 로 보고한 모든 block 에 대해 반드시 true 를 돌려주어야 하고, 기록이 남아 있는 동안 반드시 계속 true 를 돌려주어야 한다. reference 가 보고하는 경우는 두 가지다(§5.2). 첫째는 import 배치의 첫 block 이 header 검증이나 body 검증에서 실패한 경우다. 다만 future, pruned-ancestor, known-block, queue 에 부모가 있는 unknown-ancestor 결과는 보고하지 않는다. 둘째는 배치 안의 어느 block 이든 실행이나 state 검증에서 실패한 경우다. 배치의 둘째 이후 block 의 header 나 body 검증 실패는 보고하지 않고, block fetcher 의 header 검사에서 import 전에 버려진 block 도 보고하지 않는다. reference 는 번호가 가장 큰 bad block 기록 10 개만 남긴다(`badBlockToKeep = 10`). 기록을 더한 뒤 목록을 불안정 정렬로 번호가 큰 순서로 정렬하고 10 개로 자른다. 그래서 새 기록의 번호가 남아 있는 10 개보다 모두 작으면 새 기록이 바로 버려지고, 남아 있는 기록은 같은 height 이상의 기록 10 개가 뒤에 생기면 밀려난다. 편집 정정: 이전 초안은 기록이 만료되지 않는다고 잘못 적었다.
Source: `core/blockchain.go:1647-1679, 1693, 1778-1792, 1909, 2422-2425`, `eth/fetcher/block_fetcher.go:858-872`, `consensus/wbft/backend/backend.go:344-349`, `core/rawdb/accessors_chain.go:848, 913-928, 991-993`

### 4.2 Epoch block 의 candidate

```python
def candidates(epoch_header, post_state) -> list[CandidateEntry]
```

[WBFT-APP-010] epoch block `n ≥ 1` 에서 consensus 는 반드시 candidate 목록과 각 candidate 의 BLS public key 를 parent state 가 아니라 **block `n` 자신의 실행 뒤 state** 에서 얻어야 한다. 이때 height `n` 에서 유효한 validator registry 위치(`B-08`)를 쓴다.
Source: `consensus/wbft/engine/engine.go:806, 875, 894, 929-953`, `consensus/wbft/engine/engine.go:606-612`
Observable: state, header

[WBFT-APP-011] candidate 조회는 반드시 `(n, post_state)` 의 순수 함수여야 한다. candidate 조회는 반드시 proposer(block 을 만드는 동안)와 모든 verifier(block 을 실행하는 동안)에게 같은 순서의 목록을 돌려주어야 한다. candidate 의 순서가 `EpochInfo` 의 일부이기 때문이다.
Source: `consensus/wbft/engine/engine.go:806-872, 1226-1240`
Observable: header

[WBFT-APP-012] BLS 키를 등록하지 않은 candidate 는 반드시 빈 키와 함께 반환되어야 한다. 그러면 consensus 는 그 candidate 를 다음 validator 집합에서 뺀다. 그 candidate 는 candidate 목록에는 그대로 남는다. reference 구현에서 이 조회 자체에는 실패하는 경우가 없다.
Source: `consensus/wbft/engine/engine.go:892-904`, `systemcontracts/gov_validator.go:212-215`
Observable: header

candidate 조회는 두 곳에서 이루어진다. 하나는 `build_proposal` 이 만들고 있는 epoch block 에 대해 `process_finalize` 를 실행하는 동안이다(§4.4). 이 조회는 로컬 노드가 그 epoch block 높이의 validator 일 때에만 일어나고, 그렇지 않으면 `EpochInfo` 를 쓰지 않는다(`consensus/wbft/engine/engine.go:1193-1198`). 다른 하나는 노드가 epoch block 을 실행하는 동안이다(§5). 그러므로 candidate 조회는 실행 뒤 state 가 손에 없는 상태에서는 결코 이루어지지 않는다.

### 4.3 Parent state 조회: 자격과 gas tip

```python
def is_eligible_proposer(parent: Header, addr: Address) -> Eligibility
def gas_tip(parent: Header) -> uint256 | Error
```

[WBFT-APP-021] `gas_tip(parent)` 는 반드시 parent state 의 gas tip 을 돌려주어야 한다(값 규칙은 `B-06`, 위치는 `B-08`). consensus 는 이 값을 두 곳에 쓴다. (a) 제안의 `Extra.GasTip` 을 채울 때 쓰며, 이때 어떤 오류든 제안을 중단시킨다. (b) header 검증과 실행 중에 `Extra.GasTip` 을 검사할 때 쓰며, 이때는 `WBFT-HDR-111` 의 오류 분류를 따른다.
Source: `consensus/wbft/engine/engine.go:511-513, 622-645, 963-965, 1280-1295`

자격은 검사에만 쓰이고 proposer 를 고르는 데에는 쓰이지 않는다. `calc_proposer`(`A-04`)는 blacklist 를 무시하므로, blacklist 에 있는 validator 도 proposer 로 뽑힌다.

### 4.4 제안 만들기

consensus 는 block 을 만들지 않는다. consensus 는 application 에게 block 하나를 만들도록 요청하고, application 은 합의 필드를 얻기 위해 consensus 를 다시 부른다.

```python
# consensus -> application
def ready_to_build(wait: Duration, round: int) -> None
# application -> consensus (during building)
def prepare_consensus_fields(header) -> ConsensusAttributes   # A-08 §3 (prepare_proposal_header)
def compute_epoch_info(header, post_state) -> EpochInfo | None # A-04 / B-06, epoch blocks only
# application -> consensus (result)
def submit_proposal(block) -> None
```

reference 구현의 순서는 다음과 같다.

1. 새 round 마다 consensus 는 `ready_to_build(wait, round)` 를 부른다. round 0 에서는 `wait = head.Time + BlockPeriod(head.Number + 1) − now` 이고, 그 밖의 round 에서는 `wait = 0` 이다(`consensus/wbft/backend/engine.go:139-150, 277-285`, `consensus/wbft/core/core.go:260`).
2. `wait` 가 지나면 application 은 자기 현재 head 위에 header 뼈대를 만들고 `prepare_consensus_fields` 를 부른다. 이어서 application 은 pool 의 transaction 들을 실행하고, `process_finalize`(`B-06`)를 실행한 뒤 block 을 조립한다. epoch block 이면 `process_finalize` 가 `compute_epoch_info` 도 실행한다(`miner/worker.go:432-441, 664-669, 1119-1198, 1320-1425`, `consensus/wbft/engine/engine.go:1053-1061, 1193-1207`).
3. application 은 block 을 consensus 에 넘긴다. reference 구현에서는 `Seal` 이 `RequestEvent` 를 보내서 넘긴다(`consensus/wbft/backend/engine.go:186-219`). block 번호가 현재 sequence 와 같으면 consensus 는 그 block 을 대기 중인 request 로 보관한다. consensus 는 round 0 이고 state 가 `AcceptRequest` 이며 노드가 현재 view 의 proposer 일 때에만 PRE-PREPARE 를 바로 보낸다(`consensus/wbft/core/request.go:47-55`, `consensus/wbft/core/preprepare.go:51`). 뒤의 round 에서는 ROUND-CHANGE quorum 이 모인 뒤 대기 중인 request 를 제안한다(`A-05`). 미래 sequence 의 block 은 queue 에 넣어 두었다가 그 sequence 가 시작되면 다시 넣는다(`consensus/wbft/core/request.go:94-117`).

[WBFT-APP-030] application 은 반드시 모든 제안을 자기 현재 head 위에 만들어야 하고, transaction 을 실행하기 전에 반드시 consensus 가 `Coinbase`, `Difficulty`, `Nonce`, `Time`, `MixDigest`, `Extra`(`EpochInfo` 제외)를 설정하게 해야 한다. transaction 실행이 `Time`, `Coinbase`, `MixDigest`(PREVRANDAO), `Extra.GasTip` 을 읽기 때문이다.
Source: `miner/worker.go:1124, 1180-1187`, `core/state_processor.go:84`, `core/evm.go:41-77`
Observable: header, state

[WBFT-APP-031] epoch block 에서 application 은 반드시 transaction 과 앞선 finalization 단계를 실행한 **뒤**, 그리고 block hash 를 계산하기 **전**에 consensus 에게서 `EpochInfo` 를 얻어야 한다. 그 결과 `EpochInfo` 는 block 만들기의 입력이 될 수 없다.
Source: `consensus/wbft/engine/engine.go:929-970, 1193-1207`, `miner/worker.go:1401`
Observable: header

[WBFT-APP-032] 노드는 모든 round 에서 제안을 만들어도 된다. 여기에는 노드가 proposer 가 아닌 round 와 prepared block 이 재제안될 round 도 포함된다. 번호가 현재 sequence 와 같은 block 가운데 가장 최근에 제출된 block 은 그 sequence 의 round 가 바뀌어도 대기 중인 request 로 남는다. 제출할 때 노드가 proposer 였는지는 상관없다. 노드는 round 0 이고 state 가 `AcceptRequest` 이며 자신이 proposer 일 때 그 block 을 바로 제안한다. round `r > 0` 에서는 자신이 proposer 이고 prepared block 을 담지 않은 ROUND-CHANGE quorum 을 모았을 때 그 block 을 제안한다(`A-05`). reference 구현에서는 모든 validator 가 모든 round 에서 block 을 만든다.
Source: `miner/worker.go:664-669`, `consensus/wbft/core/request.go:47-55`, `consensus/wbft/core/core.go:287`, `consensus/wbft/core/roundchange.go:176-184`, `consensus/wbft/core/preprepare.go:42-51`

[WBFT-APP-033] block 만들기가 실패하면 노드는 그 round 에서 제안해서는 안 되며, 그 round 는 timeout 으로 끝난다(`A-06`). block 만들기는 다음 경우에 실패한다.

1. parent state 가 없다.
2. gas tip 을 조회할 수 없다.
3. vanity 가 32 바이트이다.
4. 이전 seal 이 빠져 있다.
5. `process_finalize` 가 실행 오류를 낸다.

노드는 그 round 안에서 block 만들기를 다시 시도하지 않는다.
Source: `miner/worker.go:1180-1183, 1336-1343, 1391-1404`, `consensus/wbft/engine/engine.go:486-549`

[WBFT-APP-034] application 은 동기화 중에 block 을 만들어서는 안 된다. reference 구현은 동기화 중에 온 block 만들기 요청을 버린다.
Source: `miner/worker.go:1320-1324`

구현 노트 (참고). consensus 코어가 시작되지 않은 노드(mining 하지 않는 노드이며, validator 인지는 상관없다)는 대신 새 head 마다 pending block 을 만든다. 이 경로도 `prepare_consensus_fields` 를 부른다(`miner/worker.go:644-662, 1119-1198`, `consensus/wbft/backend/handler.go:138-146`). mining 하는 노드는 validator 이든 아니든 코어를 시작하므로 이 경로를 타지 않는다.

### 4.5 다음 height 의 validator 집합

[WBFT-APP-040] consensus 는 height `n+1` 을 시작할 때 반드시 application 이 가진 `EpochInfo` header 들로 `validators_at(n+1)` 을 계산해야 한다(`A-04`). reference 구현에서는 이 조회가 실패하면 오류 없이 **빈** validator 집합이 나오고, 노드는 그 height 에 참여할 수 없다.
Source: `consensus/wbft/backend/backend.go:319-325`, `consensus/wbft/core/core.go:236`

### 4.6 제안 검증

```python
def validate_proposal(block) -> (Duration, Error | None)    # A-08 §5, steps P1-P7
```

[WBFT-APP-050] 제안 검증은 반드시 정확히 `A-08 §5` 의 검사로 이루어져야 한다. 특히 제안 검증은 transaction 을 실행해서는 안 된다. 제안의 state root, receipts root, bloom, gas used 는 decide 된 block 을 실행하기 전까지 검사되지 않는다(§5).
Source: `consensus/wbft/backend/backend.go:258-278`, `consensus/wbft/engine/engine.go:160-186`

[WBFT-APP-051] `ErrFutureBlock` 이 아닌 오류가 나면, consensus 는 그 제안에 PREPARE 를 보내서는 안 되고 반드시 다른 어떤 동작도 하지 않아야 한다. 그 round 는 timeout 으로 끝난다(`A-05`, `A-06`). `ErrFutureBlock` 이면 consensus 는 반환된 지속 시간이 지난 뒤 그 PRE-PREPARE 를 다시 처리한다.
Source: `consensus/wbft/core/preprepare.go:147-169`
Observable: network

### 4.7 Block finalize

```python
def finalize(block, prepared: list[SealData], committed: list[SealData], round: int) -> Error | None
```

`finalize` 는 seal 을 쓰고(`A-08 §4`) seal 을 쓴 block 을 application 에 넘긴다. §5 가 두 경로를 정한다. 이 명세에서 `finalize` 는 언제나 이 합의 쪽 인계를 뜻한다. 실행 쪽에서 block 을 마무리하는 단계(업그레이드, base fee 분배, epoch 정보, gas tip, state root)는 `B-06` 의 `process_finalize` 이다.

[WBFT-APP-060] `finalize` 는 반드시 seal 쓰기가 실패할 때에만 오류를 돌려주어야 한다(`WBFT-HDR-052`). 그 경우 consensus 는 다음 round 의 ROUND-CHANGE 를 broadcast 한다. 뒤이은 실행이나 저장의 실패는 `finalize` 로 보고되지 **않는다**.
Source: `consensus/wbft/backend/backend.go:213-250`, `consensus/wbft/core/commit.go:173-176`

### 4.8 Head 알림

[WBFT-APP-070] application 은 block 을 import 하거나 쓴 결과로 canonical head 가 바뀔 때마다 반드시 consensus 에 알려야 한다. 그러면 consensus 는 `head.Number ≥` 현재 sequence 일 때 `head.Number + 1` 의 round 0 을 시작한다. 정상 진행과 catch-up 은 같은 분기를 쓴다. head 가 `sequence − 1` 에 있는 알림(예: 같은 height 의 reorg)이나 그보다 낮은 head 의 알림은 아무것도 바꾸지 않는다.
Source: `miner/worker.go:644-662`, `consensus/wbft/backend/handler.go:138-146`, `consensus/wbft/core/final_committed.go:27-31`, `consensus/wbft/core/core.go:185-208`

[WBFT-APP-071] 배치 import 는 반드시 배치가 끝난 뒤 마지막 canonical block 에 대해 알림을 적어도 하나 만들어야 한다. 중간 block 에 대한 알림은 생략해도 된다.
Source: `core/blockchain.go:1576-1581, 1492-1494`

구현 노트 (참고). 알림은 `chainHeadFeed` 에서 출발해 `worker.newWorkLoopWBFT` 와 `Backend.NewChainHead` 를 거쳐 `FinalCommittedEvent` 로 전달된다(`miner/worker.go:644-662`). consensus core 가 멈춰 있으면 worker 는 대신 pending block 을 만든다. consensus 는 head 를 받는 데 miner loop 에 의존한다.

---

## 5. Commit 경로

### 5.1 두 경로

COMMIT quorum 뒤에 `finalize` 는 seal 을 쓴 block `B'` 를 만든다. `B'` 의 hash 는 제안 `B` 의 hash 와 같다. 그다음에 일어나는 일은 이 노드의 miner 가 그 hash 의 block 을 기다리고 있는지에 따라 달라진다. miner 가 기다리는 경우는 정확히, 이 노드가 `B` 를 만들었고 `B` 가 아직 이 노드의 현재 sealing 작업인 경우이다.

| | Proposer path (노드가 `B` 를 만들었다) | 비 proposer 경로 |
|---|---|---|
| 인계 | `B'` 를 기다리는 sealing 작업(`commitCh`)에 보낸다 | `B'` 를 peer id `"istanbul"` 로 block fetcher 의 queue 에 넣는다 |
| Header 검증 | 없음 | fetcher 에서 `verify_header(check_seals=true)` 를 하고, `InsertChain` 에서 다시 한다 |
| 실행 | 없음. 만들 때 계산한 state 를 쓴다 | 전체 실행(`Process`)과 `ValidateState` |
| 저장 / head | `WriteBlockAndSetHead(B', receipts, state)` | `InsertChain([B'])` |
| `finalized` / `safe` 표시 | 갱신하지 않는다 | `B'` 로 설정한다 |
| Block 전파 | `NewMinedBlockEvent` | fetcher 가 header 검사 뒤와 import 뒤에 broadcast 한다 |
| Block 을 버리는 조건 | blockchain 이 멈췄다(`WriteBlockAndSetHead` 가 `errChainStopped` 를 돌려준다. 멈추지 않았으면 이 호출은 chain lock 을 기다린다. closable mutex 의 `TryLock` 은 lock 이 풀릴 때까지 기다리고, mutex 가 닫혔을 때에만 실패하기 때문이다); seal hash 에 해당하는 pending sealing task 가 없다; block 이 이미 저장되어 있다(손실이 아니다) | 노드가 synced 로 표시되지 않았다; merger 가 PoS-finalized state 에 있거나 TTD 에 도달했다; 같은 hash 의 사본이 이미 queue 에 있거나 이미 저장되어 있다; fetcher peer id `"istanbul"` 로 queue 에 있는 block 이 64 개를 넘는다; head 기준 거리가 −7 보다 작거나 +32 보다 크다; parent 를 모른다; fetcher 의 seal 포함 header 검증이 실패한다(이 경우 bad block 기록은 쓰이지 않는다) |

Source: `consensus/wbft/backend/backend.go:213-250`, `consensus/wbft/backend/engine.go:186-219`, `miner/worker.go:819-894`, `core/blockchain.go:1445-1452`, `internal/syncx/mutex.go:32-37`, `eth/handler_istanbul.go:38-40`, `eth/fetcher/block_fetcher.go:43-46, 369-372, 764-805, 843-887`, `eth/handler.go:228-307`

[WBFT-APP-080] finalize 된 block 을 만든 노드는 반드시 그 block 을 만들 때 계산한 실행 뒤 state 와 함께 저장해야 한다. 이때 노드는 그 block 을 다시 실행하지 않고, 그 header 도 다시 검증하지 않은 채 저장해야 한다.
Source: `consensus/wbft/backend/backend.go:239-243`, `miner/worker.go:870`

[WBFT-APP-081] finalize 된 block 을 만들지 않은 노드는 반드시 일반 import pipeline 으로 그 block 을 import 해야 한다. 그 pipeline 은 seal 을 포함한 header 검증(`A-08 §6`), body 검증, 실행, finalization 검사(`B-06`), state 검증을 거친 뒤 head 를 갱신한다.
Source: `consensus/wbft/backend/backend.go:245-247`, `eth/fetcher/block_fetcher.go:843-887`, `core/blockchain.go:1563-1913`

[WBFT-APP-082] 두 경로 가운데 어느 것을 탈지는 반드시 finalize 된 block 의 hash 를 로컬 miner 가 현재 sealing 중인 block 의 hash 와 비교해서 정해야 하며, `Coinbase` 를 로컬 주소와 비교해서 정하지 않는다. 예를 들어 노드가 새 sealing 작업을 시작한 뒤 다른 노드가 그 노드의 block 을 재제안했다면, 노드는 자기가 만든 block 이라도 비 proposer 경로를 탄다.
Source: `consensus/wbft/backend/backend.go:239`, `consensus/wbft/backend/engine.go:188-195`, `miner/worker.go:789-796`

### 5.2 합의 뒤의 실행

비 proposer 노드에서의 순서는 다음과 같다.

1. `finalize` 가 seal 을 쓰고 `B'` 를 queue 에 넣는다. consensus 는 `Committed` state 에 있고, round timer 는 계속 돈다(`A-05`, `A-06`).
2. fetcher 가 seal 을 포함해 header 를 검증하고, 검증에 성공하면 `B'` 를 peer 들에게 전파한다.
3. `InsertChain` 이 block 하나짜리 배치로 header 를 다시 검증하고, body 를 검증하고, 실행하고, `process_finalize`(`B-06`: 시스템 컨트랙트 업그레이드, base fee 분배, epoch 정보 검사, gas tip 검사)를 실행하고, state 를 검증한다.
4. 성공하면 head 가 올라가고 consensus 가 알림을 받는다(§4.8).

[WBFT-APP-090] import 배치의 첫 block 이 header 검증이나 body 검증에서 future, pruned-ancestor, known-block, queue 에 부모가 있는 unknown-ancestor 가 아닌 오류로 실패하거나, 배치 안의 어느 block 이든 실행이나 state 검증에서 실패하면, application 은 반드시 그 block 의 hash 를 bad block 으로 기록해야 하고(§4.1), head 를 그 block 으로 올려서는 안 된다.
Source: `core/blockchain.go:1647-1679, 1778-1792, 2422-2425`
Observable: rpc

### 5.3 Finalize 뒤의 bad block

PRE-PREPARE 검증은 block 을 실행하지 않으므로(§4.6), block 은 COMMIT quorum 을 모은 뒤에 실행에 실패할 수 있다. reference 동작은 다음과 같다.

1. 비 proposer 의 import 가 실패한다. 노드는 그 block hash 를 bad block 으로 기록하고, head 는 움직이지 않는다.
2. consensus 는 round timer 가 만료될 때까지 `Committed` 에 머문다. timer 가 만료되면 round `r+1` 로 넘어가고 ROUND-CHANGE 를 broadcast 한다.
3. `r+1` 로 넘어갈 때, prepared block 이 bad block 으로 기록된 노드는 prepared round 와 prepared block 을 버리고, sequence 가 현재 sequence 보다 크지 않은 extra seal 을 모두 지운다. 여기에는 이전 block 의 늦은 seal 도 들어간다(`consensus/wbft/core/core.go:278-286`, `consensus/wbft/core/extraseal.go:185-203`). bad hash 에 대한 PRE-PREPARE 는 `ErrBlacklistedHash` 로 거부된다(`WBFT-HDR-061`).
4. 그 height 는 다른 block 으로 다시 decide 된다.
5. bad block 을 만든 노드가 정직하고 그 block 을 스스로 finalize 했다면, 그 노드는 이미 자기 state 와 함께 block 을 저장했고(§5.1 의 경로) head 를 올렸다. 이런 일은 그 노드의 만들기 결과와 다른 노드들의 import 결과가 어긋날 때에만 가능하다(예: 비결정적 실행). 그 노드는 그 height 의 새 decide 에 더 이상 참여하지 않는다(`A-10 §9`). byzantine proposer 가 만든 bad block 은 모든 정직한 노드를 옛 head 에 남긴다. PRE-PREPARE 검증이 block 을 실행하지 않으므로, bad block 은 보통 이렇게 byzantine proposer 에게서 생길 것으로 예상된다.

[WBFT-APP-100] finalize 된 block 의 import 가 실패한 뒤, consensus 는 같은 height 에서 그 block 을 다시 decide 해서는 안 되고, 반드시 round timer 가 시작한 ROUND-CHANGE 로 그 height 를 이어 가야 한다.
Source: `consensus/wbft/core/core.go:250-263, 277-296`, `consensus/wbft/backend/backend.go:266-270`
Observable: network, log

> 해설: 5 번의 경우가 중요한 이유는 비결정적 실행 때문이다. 정직한 노드끼리 실행 결과가 다르면 일부는 block 을 저장하고 앞으로 가고, 나머지는 bad block 으로 기록하고 같은 height 에서 다른 block 을 확정한다. 그러면 두 무리가 모두 그 height 에서 committed seal 이 유효한 block 을 가지게 된다. total difficulty 기반 head 선택(`A-08` §9)은 한쪽 체인이 길어질 때까지 이 상황을 풀지 못한다. 그래서 WBFT-SEC-080 은 확정 block 의 실행이 모든 구현에서 결정적이어야 한다고 요구한다. 5 번은 코드를 읽어서 끌어낸 것이며 재현하지 않았다.

### 5.4 순서와 동시성에 대한 기대 (관찰된 것)

| 호출 | 호출하는 맥락 | reference 구현의 직렬화 |
|---|---|---|
| `validate_proposal`, `finalize`, `head`, `is_bad_block`, `validators_at(n+1)`, `ready_to_build` | consensus event loop (goroutine 하나) | 서로 순차적으로 실행된다 |
| `prepare_consensus_fields`, `compute_epoch_info`, `candidates`, `gas_tip` (만들기) | miner main loop | consensus loop 와 동시에 실행된다. consensus read lock 을 잡은 채 extra seal 을 읽는다(`consensus/wbft/backend/engine.go:221-229`) |
| `verify_header` (import), `is_eligible_proposer`, `gas_tip` (검증) | fetcher goroutine, import goroutine, header verifier goroutine | 두 loop 와 동시에 실행된다 |
| `on_new_head` | miner new-work loop | consensus event loop 에 비동기로 넣는다 |

[WBFT-APP-110] 합의 구현은 application 의 head, canonical mapping, bad block 기록이 두 호출 사이에서 안정적이라고 가정해서는 안 된다. reference 구현은 호출 사이에 lock 을 잡지 않으므로, `head()` 와 그 뒤의 `header_by_number` 사이에 head 가 올라갈 수 있다(`WBFT-HDR-030`).
Source: `consensus/wbft/backend/backend.go:327-342`, `consensus/wbft/engine/engine.go:493, 516`, `miner/worker.go:1120-1124`

[WBFT-APP-111] application 은 제안 검증을 위해 반드시 현재 head 의 parent state 를 사용할 수 있게 유지해야 한다. parent state 를 쓸 수 없으면 PRE-PREPARE 에서 자격은 `UNKNOWN` 이 되고, consensus 는 gas tip 검사를 건너뛴다(§4.3). 살아 있는 validator 는 이런 상황을 결코 만나지 않을 것으로 예상된다.
Source: `consensus/wbft/engine/engine.go:291-298, 329-348`

---

## 6. 실패 의미 요약

| 연산 | 실패 | Consensus 의 반응 | Requirement |
|---|---|---|---|
| `head` | 없음 (언제나 돌려준다) | — | APP-001 |
| `validators_at(n+1)` | 조회 오류 | 빈 집합으로 계속하며, 그 height 에서 멈춘다 | APP-040 |
| `build_proposal` | 모든 실패 | 이 round 에 제안이 없고 timeout 된다 | APP-033 |
| `validate_proposal` | `ErrFutureBlock` | 지속 시간이 지난 뒤 다시 처리한다 | APP-051 |
| `validate_proposal` | 그 밖의 오류 | PREPARE 를 보내지 않고 timeout 된다 | APP-051 |
| `finalize` (seal 쓰기) | 오류 | 곧바로 ROUND-CHANGE `r+1` 을 보낸다 | APP-060 |
| `finalize` 뒤의 import | 오류 | bad block 을 기록하고, timeout 뒤 ROUND-CHANGE 를 보내며, prepared block 을 버린다 | APP-090, APP-100 |
| `is_eligible_proposer` | state 를 쓸 수 없음 | 검사를 건너뛴다 | APP-020 |
| `gas_tip` | state 를 쓸 수 없음 (검증) | 검사를 건너뛴다 | APP-021 |
| `candidates` | (실패하는 경우가 없음) | — | APP-012 |

---

## 7. Reference 구현으로의 대응 (참고)

| 추상 연산 | Reference |
|---|---|
| `head()` | `Backend.LastProposal` 이 `chain.CurrentFullBlock` 을 호출한다 |
| `header_by_number`, `header`, `header_by_hash`, `has_block` | `consensus.ChainHeaderReader`, `Backend.HasProposal` |
| `proposer_of(n)` | `Backend.GetProposer` |
| `is_bad_block` | `Backend.HasBadProposal` 이 `rawdb.HasBadBlock` 을 호출한다 |
| `candidates` | `Engine.GetGovCandidates`, `systemcontracts.ValidatorList`, `systemcontracts.GetBLSPublicKey` (inside `buildEpochInfo`) |
| `is_eligible_proposer` | `chain.StateAt(parent.Root).IsBlacklisted` (inside `verifySigner`) |
| `gas_tip` | `Engine.getGasTip` 이 `systemcontracts.GetGasTip` 을 호출한다 |
| `ready_to_build` | `Backend.NotifyNewRound` 가 `worker.readyToCommit` 에 알린다 |
| `prepare_consensus_fields` | `Backend.Prepare` 가 `Engine.Prepare` 를 호출한다 |
| `compute_epoch_info` | `writeEpoch` / `buildEpochInfo` in `FinalizeAndAssemble` |
| `submit_proposal` | `Backend.Seal` 이 `RequestEvent` 를 보낸다 |
| `validate_proposal` | `Backend.Verify` 가 `Engine.VerifyBlockProposal` 을 호출한다 |
| `finalize` | `Backend.Commit` 이 `Engine.CommitHeader` 를 호출한 뒤 `commitCh` 또는 `broadcaster.Enqueue` 로 block 을 넘긴다 |
| `on_new_head` | `ChainHeadEvent` 를 받으면 `Backend.NewChainHead` 가 `FinalCommittedEvent` 를 보낸다 |
