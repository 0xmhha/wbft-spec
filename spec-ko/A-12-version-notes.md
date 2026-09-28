# A-12. 버전 노트

이 명세는 go-stablenet commit `740526d03` (branch `dev`) 을 기준으로 작성되었다. 작성 시점에 운영 중인 network release 는 `v1.1.0` (`71e3f820f`, 2026-07-09, "Boho hardfork") 이다. 이 장은 두 버전 사이의 차이 중 conforming 구현에 중요한 것을 모두 기록한다.

---

## 1. 방법

차이는 트리 전체에 대해 `git diff v1.1.0 740526d03` 을 실행해서 구했으며, 그 결과 41 개 파일이 바뀌었다. commit 계보는 차이를 판단하는 믿을 만한 기준이 아니다. `v1.1.0` 은 `740526d03` 의 조상이 아니다. 두 commit 의 merge base 는 `0bf2f4d1b` 이고, 그 뒤로 `v1.1.0` 쪽에는 commit 이 2 개(`9d5460ecf`, #80 과 release commit `71e3f820f`, #109) 있고 `740526d03` 쪽에는 47 개 있다 (`git describe` 는 `v1.0.0-72-g740526d03` 이다). release commit `71e3f820f` 는 `dev` 수정의 내용을 담고 있다. 트리 diff 는 양쪽을 모두 비교한다. 트리 diff 에 나오는 차이는 모두 `740526d03` 쪽의 변경이며, `v1.1.0` 에만 있는 두 commit 의 내용은 `740526d03` 에도 들어 있다. 합의 수정 #82, #84, #85, #89, #91 은 `v1.1.0..740526d03` 범위에 나타나는데, 이는 이 수정들이 `dev` 에 따로 병합되었기 때문이다. 이 수정들의 내용은 이미 `v1.1.0` 에 들어 있다. 실제로 `consensus/wbft/core` 와 `consensus/wbft/backend` 의 트리 diff 는 비어 있고, `consensus/` 아래에서 달라진 파일은 `consensus/wbft/engine/engine.go` 와 그 테스트뿐이다. `istanbul_status` 의 범위를 제한한 PR #86 (`d7cff3df9`, `B-09`) 도 마찬가지이다. 아래에는 내용이 실제로 다른 변경만 나열한다.

---

## 2. 합의와 block 유효성의 차이

### 2.1 Header extra 의 nil gas tip (코드 글자만 다르다)

| | `v1.1.0` | `740526d03` |
|---|---|---|
| `verifyGasTip` 조건 | `extra.GasTip != nil && extra.GasTip != expected` 이면 거부한다 | `extra.GasTip == nil || extra.GasTip != expected` 이면 거부한다 |

Source: `consensus/wbft/engine/engine.go:1291` (commit `b46ef9ef3`, #113).

바뀐 branch 에는 도달할 수 없다. `extra.GasTip` 은 header extra 를 decode 한 값이며, decode 된 `*big.Int` 는 결코 `nil` 이 아니다. go-ethereum RLP decoder 는 `rlp:"nil"` 을 처리하는 pointer 규칙보다 big integer decoder 를 먼저 고르기 때문이다 (`rlp/decode.go:161-162`). 그래서 빈 `gas_tip` 항목 (`0x80`) 은 0 으로 decode 되고, list 항목 (`0xc0`) 은 extra 전체를 decode 할 수 없게 만든다 (`A-03-encoding.md`, WBFT-ENC-008). 그러므로 두 버전은 정확히 같은 block 을 받아들인다. gas tip 이 빈 block 은 governance gas tip 이 0 이 아닌 한 두 버전 모두 거부한다. 레퍼런스 commit 과 `v1.1.0` 사이에는 관측할 수 있는 합의 차이나 block 유효성 차이가 없다.

---

## 3. Block 유효성을 바꾸지 않는 차이

아래 차이는 로컬 동작만 바꾼다. 이 명세의 informative 절이 어느 버전의 동작을 설명하는지 구현자가 알 수 있도록 나열한다.

| 영역 | 변경 | Commit | 효과 |
|---|---|---|---|
| Transaction pool | 누적 잔액 검사를 되살렸다. 이 검사는 fee delegation 을 반영해서, sender 가 value 를 내고 fee payer 가 gas 를 내는 것으로 계산한다 | `54a5cbd59` (#116) | 노드가 자기 pool 에 어떤 transaction 을 받아들이는지가 바뀐다. 이 검사는 로컬 정책이며, `B-07-transaction-rules.md` 의 informative 부분이 설명한다 |
| RPC transaction 조립 | `FeeDelegateDynamicFeeTx.SetSenderTx` 가 이제 복사하기 전에 access list 를 할당한다. 이전에는 복사된 access list 가 항상 비어 있었다 | `f4c9490f7` (#115) | 노드 자신의 RPC 가 조립한 transaction 에만 영향이 있다 (`internal/ethapi/api.go:673`, `:2238`, `transaction_args.go:557`). 받아서 decode 한 transaction 에는 영향이 없다 |
| Wallet / transaction API | fee-delegated transaction 서명 오류를 고쳤다. 이제 `feePayer` 를 준 서명 요청은 type `0x16` transaction 을 조립하지 않으면 거부된다 (`fee delegate tx type mismatch`) | `4444af086` (#114) | RPC 와 wallet 동작이 바뀐다. `v1.1.0` 에서는 `personal_signTransaction` 과 `eth_signTransaction` 에 `feePayer` 를 주고 sender 의 `v`, `r`, `s` 를 주지 않으면, 노드가 조립한 `0x16` 이 아닌 transaction (`maxFeePerGas` 를 주면 type `0x02`) 에 fee payer 의 key 로 서명한다. 그래서 fee payer 가 의도하지 않은 transaction 의 sender 가 된다 (`internal/ethapi/api.go:449-471, 2080-2086, 2428-2435`) |
| Blockchain 내부 | reorg 중 transaction lookup cache 의 lock 처리를 고쳤다 | `650722d5a` (#127) | race 를 고친 것이며 protocol 에는 영향이 없다 |
| Chain repair, snap sync | `SetHead` repair 경로가 더 이상 `updateFn` 을 부르지 않는다. snap sync 의 storage heal 검사가 trie scheme 을 따른다 | `d607a8f65` (#117), `d651ce405` (#121) | 로컬 복구와 동기화 동작이 바뀐다. protocol 에는 영향이 없다 |
| Simulated backend | `WBFTBackend.Close()` 가 node stack 을 닫는다 | `05a129e9e` (#120) | test harness 에만 영향이 있다 |
| Tracer, RPC, logging, trie database, command-line 도구, simulated backend | upstream go-ethereum 의 변경을 backport 했다 | #104, #105, #106, #107, #111, #112, #118, #119, #122, #123, #124, #126, #128, #129 | protocol 에는 영향이 없다 |

---

## 4. 유지 규칙

레퍼런스 commit 이 바뀌면, 이 장은 반드시 새 commit 을 기준으로 갱신해야 한다. 이때 반드시 위의 방법을 다시 실행해야 하고, `Source:` 줄이 바뀐 hunk 를 가리키는 모든 requirement 를 반드시 다시 확인해야 한다.

> 해설: 이 갱신은 `A-11` 의 vector 재생성, `A-13` 의 오류·로그 카탈로그 재생성과 같은 시점에 함께 일어난다.
