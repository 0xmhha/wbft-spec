# WBFT 명세

이 디렉터리는 WBFT 합의 프로토콜의 규범 명세(Part A)와, WBFT 노드가 go-stablenet 과 상호 운용하기 위해 지켜야 하는 StableNet block 유효성 규칙(Part B)을 담는다.

- 상태: 초안이며 작성 중이다.
- 일부 요구사항은 검토가 끝날 때까지 이 판에서 빼 두었으며, 이후 판에 추가한다. 그래서 요구사항 번호가 이어지지 않는 곳이 있다.
- 레퍼런스 구현은 go-stablenet 의 commit `740526d03` 이다. 이 commit 은 branch `dev` 에 있으며, `git describe` 는 `v1.0.0-72-g740526d03` 이다. `v1.1.0` 은 이 commit 의 조상이 아니다. 두 commit 은 merge base `0bf2f4d1b` 에서 갈라졌고, 그 뒤로 `v1.1.0` 쪽에 commit 이 2 개, `740526d03` 쪽에 47 개 있다. 내용 비교는 `A-12` 에 있다. 이 명세의 모든 소스 참조는 이 commit 을 가리킨다.
- 릴리스 `v1.1.0` (commit `71e3f820f`) 과 레퍼런스 commit 의 차이는 `A-12-version-notes.md` 에 기록한다.

이 파일들은 영문 규범 명세 `../spec/` 의 한국어판이다. 파일 이름, 장·절 번호, 요구사항 ID, 표와 의사코드의 배치를 영문판과 똑같이 맞춘다. 규범은 영문판이며, 두 판의 내용이 다르면 영문판이 맞다. 한국어판을 쓰거나 고칠 때는 `STYLE.md` 의 지침을 따른다.

---

## 1. 문서 지도

### Part A — WBFT 합의

| 파일 | 제목 | 영역 코드 |
|---|---|---|
| `A-00-overview.md` | 개요, 범위, conformance 등급 | — |
| `A-14-protocol.md` | 프로토콜 개관 (`A-00` 바로 다음에 읽는다): 한 높이의 메시지 교환, round change 로 이어지는 실패, round change 가 시작된 단계별 차이, round change 뒤의 합의 | `PROTO` |
| `A-01-notation-types-parameters.md` | 표기법, 기본 타입, 상수, 설정 파라미터 | `TYPE`, `PARAM` |
| `A-02-cryptography.md` | 해시, ECDSA, BLS12-381, seal 데이터, randao | `CRYPTO` |
| `A-03-encoding.md` | RLP 규약, `WBFTExtra`, aggregated seal, `EpochInfo`, block hash 규칙, 합의 메시지의 wire 형식과 signing payload | `ENC`, `MSG` |
| `A-04-validators-epochs-proposer.md` | 높이별 validator 집합, epoch 경계, quorum, proposer 선택, 다음 epoch 계산 (diligence, shuffle) | `VAL`, `EPOCH`, `PROP` |
| `A-05-state-machine.md` | 합의 state machine: view, 상태, 메시지 수락, PRE-PREPARE / PREPARE / COMMIT / ROUND-CHANGE, justification, extra seal, decision, 새 round 와 새 sequence | `SM` |
| `A-06-timers.md` | round change timeout, 재전송, 미래 proposal 대기, block period | `TIMER` |
| `A-07-network.md` | `istanbul/100` subprotocol, framing, peer 선택, dedup, relay, 연결 끊기 | `NET` |
| `A-08-header-rules.md` | header 의 합의 필드: proposal 구성, proposal 검증, 확정된 block 검증, light 검증 | `HDR` |
| `A-09-application-interface.md` | 합의 프로토콜이 실행 계층에 요구하는 것 (Part B 로 이어지는 추상 인터페이스) | `APP` |
| `A-10-security.md` | 보안 고려 사항 | `SEC` |
| `A-11-conformance-vectors.md` | conformance 등급과 test vector 목록 | `VEC` |
| `A-12-version-notes.md` | 레퍼런스 commit 과 `v1.1.0` 의 차이 | — |
| `A-13-appendix.md` | 오류 목록, 로그 목록 (참고), QBFT 로부터의 계보 (참고) | — |

### Part B — StableNet block 유효성

| 파일 | 제목 | 영역 코드 |
|---|---|---|
| `B-00-overview.md` | 개요와 Part A 와의 관계 | — |
| `B-01-chain-config-forks.md` | `ChainConfig`, Anzeon 설정, WBFT transition, fork 일정, 네트워크 preset | `CFG` |
| `B-02-genesis.md` | Anzeon genesis: header, extra 데이터, system contract 주입, 거버넌스 초기화 | `GEN` |
| `B-03-header-body.md` | 실행 쪽 header 와 body 의 유효성 (gas limit, base fee, 금지된 fork, uncle, withdrawal) | `BHDR`, `BODY` |
| `B-04-system-contracts.md` | system contract 주소, 버전, 노드가 읽는 storage 배치, upgrade 방식 | `SYS` |
| `B-05-governance.md` | 거버넌스 의미론: GovBase (member, proposal, vote, quorum), GovValidator, GovCouncil, 그리고 minter contract 중 노드 동작에 영향을 주는 부분 | `GOV` |
| `B-06-finalization.md` | block finalization: upgrade, base fee 분배, epoch info 기록과 검증, gas tip 검증, state root | `FIN` |
| `B-07-transaction-rules.md` | Anzeon 트랜잭션 규칙: 계정 extra 비트 (blacklist, authorized), gas tip 강제, fee delegation, block 조립 순서 (참고) | `TX` |
| `B-08-candidate-source.md` | Part A 의 애플리케이션 인터페이스를 contract state 에 연결하는 방법 (candidate, BLS 키, proposer 자격, gas tip) | `SRC` |
| `B-09-sync-rpc.md` | 동기화와 head 선택, 합의 관찰에 필요한 RPC | `SYNC`, `RPC` |

---

## 2. 규약

### 2.1 요구사항 용어

MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, RECOMMENDED, MAY, OPTIONAL 이라는 키워드는 모두 대문자로 쓰였을 때에만 RFC 2119 와 RFC 8174 에 적힌 뜻으로 해석한다. 한국어판은 이 키워드를 `STYLE.md` §2 의 표에 따라 "반드시 ~해야 한다", "~하는 것이 좋다", "~해도 된다" 같은 한국어 표현으로 옮긴다.

### 2.2 규범의 범위

다른 당사자가 관찰할 수 있는 동작만 규범이다. 규범에 속하는 것은 다음과 같다.

- wire 위의 바이트 (메시지 인코딩, signing payload, framing),
- block 과 header 의 유효성, 즉 conformance 를 따르는 노드가 어떤 block 과 header 를 받아들이고 어떤 것을 거부하는지,
- 노드가 메시지를 보내는 조건, 그리고 그 메시지의 내용과 보내는 시점,
- 체인에 기록되는 값 (seal, epoch info, randao mix, finalization 이 바꾼 잔액).

goroutine, lock, cache, channel 크기, 로그 문구 같은 구현 구조는 **Implementation note (informative)** 로 표시한 절에서 설명한다. conformance 를 따르는 구현은 규범 동작을 유지하는 한 이 절들의 내용과 달라도 된다.

### 2.3 요구사항 식별자

규범 문장 하나하나는 번호가 붙은 requirement 이다.

```
[WBFT-SM-012] A node in state Preprepared MUST ignore a PRE-PREPARE for its current view.
Source: consensus/wbft/core/backlog.go:125-190 (checkMessage)
```

- Part A 의 식별자는 `WBFT-<AREA>-<NNN>` 형식이고, Part B 의 식별자는 `SNET-<AREA>-<NNN>` 형식이다. 영역 코드는 문서 지도에 나와 있다.
- 번호는 세 자리이고, 한 영역 안에서 유일하며, 다시 쓰지 않는다. 철회한 requirement 는 그 자리에 남겨 두고 `(withdrawn)` 으로 표시한다.
- 참조 동작을 서술하기만 하고 다른 쪽이 확인할 수 있는 성질을 말하지 않는 requirement 는 그 자리에 informative 문장으로 남기고, 문장 맨 앞에 `(withdrawn; informative)` 를 붙인다. 이런 requirement 는 `Source:` 줄을 유지하고, `Observable:` 줄을 두지 않으며, 적합성 requirement 가 아니다(`A-11` WBFT-VEC-001). 그 문장이 서술하는 규범 동작은 그 문장이 인용하는 requirement 가 정한다.
- requirement 하나는 시험할 수 있는 성질 하나를 말한다. 한 문장에 서로 따로 실패할 수 있는 MUST 가 두 개 있으면 문장을 나눈다.
- `Source:` 줄은 참고용이며, 레퍼런스 commit 의 go-stablenet 에서 `path:line` 을 인용한다. 모든 requirement 에는 `Source:` 줄이 하나 이상 있다.
- inspector 가 노드 밖에서 확인할 수 있는 requirement 에는 `Observable: header | network | log | rpc | state` 태그를 추가로 단다. 태그는 하나 이상이며 쉼표로 구분하고, 그 줄에는 다른 글을 쓰지 않는다. 무엇을 관찰하는지에 대한 설명은 다음 줄의 `Observed as:` 에 쓴다.

### 2.4 의사코드

알고리즘은 Ethereum consensus 명세의 방식을 따라 Python 과 비슷한 의사코드로 적는다.

- 함수 이름이 `on_*` (event handler) 이거나 노드 state 를 바꾼다고 문서에 적혀 있지 않으면, 그 함수는 순수 함수다.
- 타입 (`uint32`, `uint64`, `uint256`) 이 주어지지 않은 정수 연산에는 범위 제한이 없다. 레퍼런스 구현이 부동소수점이나 잘라내는 변환을 쓰는 곳에서는 의사코드가 그 동작을 그대로 재현하고, 재현한다는 사실을 밝힌다.
- `bytes32`, `Address` (20 바이트), `Hash` (32 바이트) 와 그 밖의 타입은 `A-01` 에서 정의한다.
- `rlp_encode` / `rlp_decode` 는 `A-03` 을 따른다. `A-03` 은 go-ethereum 의 RLP 규칙과 `nil` 규약을 따른다.

### 2.5 그 밖의 규약

- 바이트 문자열은 `0x` 접두사를 붙인 16진수로 적는다. 비트 위치는 최하위 비트부터 센다.
- 높이는 메시지 맥락에서 *sequence* 라고 부르고, header 맥락에서 *number* 라고 부른다. 두 이름은 같은 값을 가리킨다.
- "레퍼런스 구현" 은 레퍼런스 commit 의 go-stablenet 을 뜻한다.
- `A-01` §2 (용어집) 에서 정의한 용어는 어디서나 그 뜻으로 쓴다.

---

## 3. 다른 문서와의 관계

- `../spec-ko/`: 한국어판이며 참고용이다. 한국어판은 이 디렉터리를 파일, 절, requirement 단위로 똑같이 따르고, 설명을 담은 "해설" 단락을 덧붙인다. 한국어판과 이 명세가 다르면 이 명세가 맞다. 한국어판의 작성 지침은 `../spec-ko/STYLE.md` 에 있다.
