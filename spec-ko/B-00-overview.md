# B-00 Part B 개요: StableNet block 유효성

- Status: draft
- Reference implementation: go-stablenet `740526d03` (`README.md` 참조)

Part A 는 WBFT 합의 protocol 을 정한다. 즉 validator 들이 height 마다 block 하나에 합의하는 방법, 그 합의를 싣는 header 의 합의 필드, 그리고 외부에서 그 필드를 검증하는 방법을 정한다. Part A 는 block body 와 실행 state 를 일부러 내용을 모르는 값(opaque)으로 다룬다. Part A 가 요구하는 것은 `A-09-application-interface.md` 의 추상 application interface 를 통해 실행 계층이 proposal 을 만들고, proposal 을 판정하고, block 을 finalize 하고, candidate 집합을 알려 줄 수 있어야 한다는 것뿐이다.

Part B 는 구체적인 실행 계층 하나, 곧 StableNet(go-stablenet 의 Anzeon 규칙 집합)에 대해 그 빈자리를 채운다. Part A 와 Part B 를 함께 구현한 노드는 go-stablenet 이 만들고 받아들이는 block 과 정확히 같은 block 을 만들고 받아들이며, block 마다 같은 state root 에 도달한다. 그러므로 Part B 는 WBFT 구현이 자기 자신과 일관되는 데서 그치지 않고 go-stablenet 네트워크와 상호 운용(interoperable)되게 만드는 부분이다.

> 해설: Part A 만 구현한 노드는 합의 필드가 맞는 block 을 만들 수는 있지만, go-stablenet 과 같은 state root 를 계산한다는 보장은 없다. state root 가 한 block 에서라도 다르면 그 노드는 그 뒤의 모든 block 을 거부한다. 그래서 go-stablenet 네트워크에 참여하려는 노드는 Part B 까지 구현해야 한다.

---

## 1. Part B 가 묶는 것

아래 표는 `A-09` 의 추상 연산마다 그 연산에 StableNet 에서의 구체적인 뜻을 주는 Part B 장을 보여 준다.

| `A-09` 연산 | StableNet 에서의 뜻 | 정하는 곳 |
|---|---|---|
| `ready_to_build`, `prepare_consensus_fields`, `submit_proposal` (building, `A-09` §4.4) | application 이 실행 측 header 필드(gas limit, base fee, 금지된 fork 필드의 부재)를 채우고, 합의가 `GasTip` 을 포함한 합의 필드를 채우게 한 뒤, transaction 을 실행하고 `FinalizeAndAssemble` 로 finalize 한다(upgrade, base fee 분배, `EpochInfo`, gas tip 검사, state root) | `B-03` §3, `B-06` §2-§6, `B-07` (transaction 선택, informative) |
| `compute_epoch_info(header, post_state)` | epoch block 의 실행 후 state 에서 다음 epoch 정보를 계산하고, proposer 는 그 정보를 쓰고 import 하는 노드는 그 정보를 대조한다 | `B-06` §4 (알고리즘은 `A-04`) |
| `candidates(epoch_header, post_state)` | epoch block 에서 유효한 `GovValidator` 에서 읽은 candidate 주소와 BLS key 목록 | `B-08`, `B-04`, `B-01` §7 |
| `is_eligible_proposer(parent, addr)` | parent state 에서 본 proposer 계정의 blacklist 비트 | `B-07`, `B-08` |
| `gas_tip(parent)` | parent state 에서 읽은, genesis 에서 정한 컨트랙트 주소에 있는 `GovValidator` 의 gas tip slot 값 | `B-06` §5, `B-08` |
| `validate_proposal(block)` | 투표 전에 header 만 보는 검사(`A-08` P1-P7): transaction root, uncle hash, `B-03` §1-§2 의 header 규칙을 검사하고 transaction 은 실행하지 않는다 | `B-03` §7, `B-06` §6.1 |
| `finalize(block, prepared, committed, round)` 와 commit 경로 (`A-09` §5) | seal 을 쓰는 일은 Part A 의 몫이다. proposer 가 아닌 경로에서는 seal 된 block 을 body 검증, 실행, finalization, state 검증을 거쳐 import 한다. proposer 경로에서는 build 할 때 만든 state 를 저장한다 | `B-03` §4-§7, `B-06` §6.1 |

설정은 두 Part 가 공유한다. `B-01` 의 chain 설정 하나에서 Part A 의 합의 `Config`(block period, epoch 길이, proposer 정책, timeout, transition)와 Part B 의 fork 일정 및 system contract upgrade 목록이 함께 나온다. `B-02` 의 genesis block 은 두 Part 의 기준점이다. genesis block 의 `WBFTExtra.EpochInfo` 는 첫 epoch 의 validator 집합을 주고(Part A), genesis allocation 은 초기 컨트랙트 state 를 준다(Part B).

> 해설: 합의 설정과 fork 일정을 서로 다른 파일이나 서로 다른 parser 에서 만들면 안 된다. 두 노드가 consensus-critical 필드 하나라도 다르게 읽으면, 두 노드가 모두 명세를 올바르게 구현했더라도 어느 height 에서 block 판정이 갈린다(`B-01` SNET-CFG-001).

---

## 2. 장 목록

| 파일 | 제목 | 영역 코드 | 범위 |
|---|---|---|---|
| `B-00-overview.md` | 개요와 Part A 와의 관계 | — | 이 장 |
| `B-01-chain-config-forks.md` | Chain 설정과 fork | `CFG` | StableNet 에 중요한 `ChainConfig` 필드, Ethereum fork, `ApplepieBlock`, `BohoBlock`, `AnzeonConfig`, WBFT transition, upgrade 수집, 시작 시 유효성 검사와 호환성 규칙, 네트워크 preset 8282 와 8283 |
| `B-02-genesis.md` | Anzeon genesis | `GEN` | genesis header 필드, genesis block 의 `WBFTExtra`, system contract 주입, `GovCouncil` 초기화, genesis extra 와 genesis storage 의 일관성, genesis hash, 계산 예 |
| `B-03-header-body.md` | 실행 측 header, body, state 유효성 | `BHDR`, `BODY` | gas limit 범위, base fee(EIP-1559 의 Anzeon 변형), `GasUsed`, 금지된 Shanghai/Cancun 필드, uncle 과 withdrawal 규칙, body 검증, 실행 후 state 검증, `A-08` 과 합친 전체 검사 순서 |
| `B-04-system-contracts.md` | System contract | `SYS` | 주소, 버전, 노드가 읽는 storage layout, upgrade 방식 |
| `B-05-governance.md` | Governance 의미 | `GOV` | 노드 동작에 영향을 주는 범위에서 GovBase, GovValidator, GovCouncil, minter 컨트랙트 |
| `B-06-finalization.md` | Block finalization | `FIN` | 실행 측 finalization(`A-09` 의 `finalize` 연산이 아니라 `processFinalize`): system contract upgrade, base fee 분배, `EpochInfo` 쓰기/검증 hook, gas tip 검증, state root, proposer 경로와 import 경로의 차이 |
| `B-07-transaction-rules.md` | Anzeon transaction 규칙 | `TX` | 계정 extra 비트, gas tip 강제, fee delegation, 실행 중 blacklist |
| `B-08-candidate-source.md` | Candidate source 연결 | `SRC` | `A-09` 연산을 컨트랙트 state 에 연결하는 규칙 |
| `B-09-sync-rpc.md` | 동기화와 RPC | `SYNC`, `RPC` | head 선택, 동기화 중의 예외, `eth`/`snap` wire 의 값과 차이(network ID, fork ID, total difficulty, 응답 검사), 합의 관찰에 필요한 RPC |

---

## 3. 읽는 순서

StableNet 호환 노드를 구현하는 사람은 먼저 `B-01` 을 읽고 설정을 만들며, 다음으로 `B-02` 를 읽고 똑같은 genesis block 을 만든다. 그다음 `B-03` 과 `B-06` 을 `A-08` 과 함께 읽고 block 을 받아들이고 만드는 법을 익힌다. 마지막으로 receipt 와 state root 를 결정하는 transaction 의미를 `B-07` 에서 읽는다. 노드가 컨트랙트 storage(candidate, gas tip, blacklist)를 읽기 시작하면 곧바로 `B-04`, `B-05`, `B-08` 도 필요하다.

## 4. Part B 에만 적용되는 규약

- 요구사항 식별자는 `SNET-<AREA>-<NNN>` 형식이다(`README.md` §2.3).
- "Anzeon chain" 은 `ChainConfig.Anzeon` 이 있는 chain 을 뜻한다(`B-01` SNET-CFG-004). Part B 는 Anzeon chain 에만 적용된다.
- "Import 경로" 는 `BlockChain.InsertChain` 을 거치는 block 삽입을 뜻한다. 이 경로는 header 검증, body 검증, 실행, `Finalize`, state 검증으로 이루어진다. "Proposer 경로" 는 로컬 miner 가 `FinalizeAndAssemble` 로 block 을 만들고, seal 된 block 을 다시 실행하지 않고 저장하는 경로를 뜻한다. 두 경로가 다른 곳에서는 명세가 두 경로를 따로 정한다(`B-06` §6).
- wei 단위 값은 `_` 구분자를 넣은 10진 정수로 쓴다. 예를 들면 `20_000_000_000_000` 이다.
