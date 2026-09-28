# A-00. 개요

WBFT 는 StableNet 의 Byzantine fault tolerant 합의 프로토콜이다. WBFT 는 validator 집합 안에서 높이마다 block 하나를 decide 하고, 즉시 finality 를 주며, 각 decision 의 증거 (BLS aggregated seal) 를 block header 에 기록한다. WBFT 는 Istanbul BFT / QBFT 에서 갈라져 나왔지만, 메시지 규칙, seal 형식, epoch, validator 선택, 난수 생성을 따로 갖춘 별개의 프로토콜이다. 이 명세와 QBFT 문헌이 다르면 이 명세가 맞다.

이 장은 명세가 무엇을 다루는지, 각 Part 가 어떻게 맞물리는지, 누가 무엇을 따라야 하는지를 설명한다. 이 장에는 requirement 가 없다.

---

## 1. 범위

명세는 두 Part 로 이루어진다.

**Part A — WBFT 합의** 는 노드가 WBFT 합의에 참여하거나 WBFT 합의를 검증하려면 무엇을 해야 하는지 정의한다. Part A 가 다루는 내용은 다음과 같다.

- 데이터 타입, 상수, 높이에 따라 달라지는 설정 (`A-01`),
- 암호 연산: ECDSA 메시지 signature, BLS12-381 seal, randao reveal 과 mix (`A-02`),
- 바이트 수준의 인코딩: header extra 에 들어가는 합의 데이터, 네 가지 합의 메시지와 그 signing payload, block hash 규칙 (`A-03`),
- 각 높이를 seal 하는 validator, epoch 의 경계를 정하는 방법, 각 round 의 proposer 를 고르는 방법, 다음 epoch 의 validator 집합을 계산하는 방법 (`A-04`),
- 합의 state machine: view, 상태, 메시지를 받아들이는 규칙, PRE-PREPARE / PREPARE / COMMIT / ROUND-CHANGE, justification, 늦게 도착한 seal, decision (`A-05`),
- timer (`A-06`) 와 `istanbul/100` 네트워크 subprotocol (`A-07`),
- header 의 합의 필드, 그리고 header 를 만들고 검증하는 방법 (`A-08`),
- 합의 프로토콜이 실행 계층에 요구하는 것 (`A-09`),
- 보안 고려 사항 (`A-10`), conformance 와 test vector (`A-11`), 버전 노트 (`A-12`), 오류와 로그의 목록 (`A-13`).

**Part B — StableNet block 유효성** 은 합의 밖의 규칙을 정의한다. 노드는 이 규칙을 적용해야 go-stablenet 이 만들고 받아들이는 block 과 정확히 같은 block 을 만들고 받아들일 수 있다. Part B 가 다루는 내용은 다음과 같다.

- chain 설정과 fork,
- genesis,
- 실행 쪽 header 와 body 규칙,
- system contract 와 그 storage,
- validator candidate 를 누가 맡을지 정하는 거버넌스 의미론,
- block finalization: upgrade, base fee 분배, epoch info, gas tip,
- Anzeon 트랜잭션 규칙,
- Part A 의 애플리케이션 인터페이스를 contract state 에 연결하는 방법,
- 동기화와 RPC.

`B-00` 이 Part B 의 장별 지도를 제공한다.

두 Part 의 경계는 `A-09` 의 애플리케이션 인터페이스다. Part A 는 candidate, proposer 자격, gas tip, block 조립, proposal 검증, block import 를 요청한다. Part B (`B-08` 과 `B-08` 이 인용하는 장) 는 StableNet 이 그 요청에 어떻게 답하는지 정한다.

다음은 범위 밖이다. 명세는 EVM 자체를 다루지 않는다. StableNet 은 Part B 가 따로 정한 곳을 빼면 레퍼런스 commit 의 go-ethereum 을 따르기 때문이다. 트랜잭션 풀 정책은 참고로만 설명하고, 클라이언트 사용자 인터페이스는 다루지 않는다.

`eth/68` 과 `snap/1` 프로토콜, devp2p discovery 는 각자의 공개 명세를 따른다. 그렇다고 이 프로토콜들이 통째로 범위 밖인 것은 아니다. StableNet 노드가 이 프로토콜 안에서 써야 하는 값 (network ID, fork ID, total difficulty) 과, 레퍼런스 구현이 go-ethereum v1.13.15 보다 엄격하게 검사하는 곳은 `B-09` §12 가 정한다. 레퍼런스 구현은 v1.13.15 이후의 upstream 변경을 가져왔기 때문이다.

---

## 2. 프로토콜의 흐름 한눈에 보기

각 높이 `h` 에서 validator 집합 `V(h)` 는 `h` 보다 앞선 마지막 epoch block 의 header 에 기록된 epoch 정보에서 가져온다 (`A-04`). round `r` 의 proposer 는 이전 block 의 proposer 와 `r` 만으로 결정적으로 정해진다. 모든 validator 가 candidate block 을 조립하지만, 현재 round 의 proposer 만 그 block 을 PRE-PREPARE 에 담아 broadcast 한다 (`A-05`).

PRE-PREPARE 를 받아들인 validator 는 block digest 에 자신의 BLS 키로 서명하고, 그 *prepare seal* 을 담은 PREPARE 를 broadcast 한다. PREPARE 가 quorum `Q` 만큼 모이면 validator 는 *commit seal* 을 담은 COMMIT 을 broadcast 한다. COMMIT 이 quorum 만큼 모이면 block 이 decide 된다. 이때 노드는 quorum 만큼의 prepare seal 과 commit seal 을 각각 aggregate 해서 header 의 `prepared_seal` 과 `committed_seal` 에 넣고, seal 된 block 을 실행 계층에 넘긴다. quorum 이 모인 뒤에 도착한 seal 을 *extra seal* 이라고 부른다. extra seal 은 다음 block 의 `prev_prepared_seal` / `prev_committed_seal` 에 합쳐진다. 그래서 chain 에는 각 block 을 누가 seal 했는지가 기록된다. block 의 hash 는 그 block 자신의 seal 과 round 를 포함하지 않는다. 그러므로 노드마다 가진 seal 필드의 값이 달라도 모든 노드가 같은 block hash 에 합의한다 (`A-03`, `A-08`).

한 round 가 timeout 안에 decide 하지 못하면 validator 는 다음 round 로 넘어가고 ROUND-CHANGE 메시지를 broadcast 한다. ROUND-CHANGE 는 prepared block 이 있으면 그 block 과 PREPARE certificate 를 담는다. 새 round 의 proposer 는 가장 높은 round 에서 prepared 된 block 을 다시 제안하고, prepared 된 block 이 없으면 자신의 block 을 제안한다. 이때 proposer 는 justification 을 함께 보내며, 받는 쪽은 모두 이 justification 을 검사한다 (`A-05`, `A-06`).

epoch block 에서 proposer 는 다음 epoch 의 candidate, 각 candidate 의 diligence 점수, shuffle 한 validator 순서를 계산해 header 에 기록한다. diligence 점수는 그 epoch 의 seal 기록으로 계산한다. 다른 모든 노드는 그 block 을 실행할 때 같은 값을 다시 계산해서 header 의 값과 비교한다 (`A-04`, `B-06`). candidate 와 그 BLS 키는 `GovValidator` system contract 에서 온다. 이 contract 의 구성원은 on-chain 거버넌스가 정한다 (`B-04`, `B-05`, `B-08`).

합의 메시지는 `istanbul/100` devp2p subprotocol 로 전달된다. 합의 메시지는 직접 연결된 validator 사이에서만 오간다. 노드는 합의 메시지를 성공적으로 처리한 뒤에 그 메시지를 relay 한다 (`A-07`). decide 된 block 은 일반적인 eth block 전파와 동기화를 통해 validator 가 아닌 노드에 전달된다.

> 해설: block hash 가 자기 seal 과 round 를 빼는 이유는 seal 이 서명하는 대상이 바로 그 hash 이기 때문이다. 서명은 자기 자신을 포함할 수 없다. 또한 quorum 에 들어간 seal 의 조합은 노드마다 다를 수 있으므로, seal 을 hash 에 넣으면 같은 block 에 대해 노드마다 hash 가 달라진다. 그 대가로 같은 hash 를 가진 두 header 의 바이트가 서로 다를 수 있다. 자세한 규칙은 `A-03` 과 `A-08` 에 있다.

---

## 3. 누가 무엇을 따르는가

`A-11` 은 네 가지 conformance 등급을 정의한다. 요약하면 다음과 같다.

| 등급 | 대표적인 구현 | 구현해야 하는 범위 |
|---|---|---|
| 합의 참여자 | validator 노드 (`wbft` + `wbft-stablenet`, go-stablenet) | Part A 전체와 Part B 전체 |
| 검증 노드 | validator 역할을 하지 않는 full node | `A-05`/`A-06`/`A-07` 의 메시지 송신 동작을 뺀 Part A, 그리고 Part B 전체 |
| light verifier | header 로 finality 를 확인하는 bridge 나 light client | `A-01`, `A-02`, `A-03`, `A-04` 의 validator 집합과 epoch 절 (다음 epoch 계산은 제외한다), `A-08` 의 light 절차 |
| observer | WBFT inspector | 디코딩 (`A-02`, `A-03`), 그리고 `Observable` 태그가 붙은 모든 requirement 를 검사하는 기능 |

---

## 4. 레퍼런스 구현과 버전

이 명세는 go-stablenet 의 commit `740526d03` (branch `dev`) 을 보고 작성했다. 모든 requirement 는 자신이 유도된 코드를 `Source:` 줄로 인용한다. 작성 시점의 네트워크 릴리스인 `v1.1.0` 은 레퍼런스 commit 과 비교해 관찰할 수 있는 합의 차이나 block 유효성 차이가 없다 (`A-12`).

---

## 5. 읽는 순서

- 누구든 먼저 `A-00` 을 읽고 이어서 `A-14` 를 읽는다. `A-14` 는 각 부분을 정하는 장들에 앞서, 프로토콜을 노드 사이의 메시지 교환으로 보여 준다 (정상 경우, round change, round change 뒤의 합의).
- validator 를 구현하려면 `A-14`, `A-01`, `A-03`, `A-04`, `A-05`, `A-06`, `A-07`, `A-08`, `A-09` 의 순서로 읽은 뒤, `B-00` 부터 Part B 를 읽는다.
- 밖에서 finality 를 검증하려면 `A-02`, `A-03`, `A-04` §§1–5, `A-08` (light 검증), `A-11` (vector) 을 읽는다.
- inspector 를 만들려면 먼저 `A-11` 을 읽어 어떤 requirement 가 관찰 가능한지와 vector 가 어떻게 구성되는지를 확인한 뒤, `A-11` 이 가리키는 장을 읽는다.
- 정식 이름 표는 `NAMES.md` 이다. 규약은 `README.md` 에 있다.
