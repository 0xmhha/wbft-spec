# A-10 보안 고려 사항

- Area code: `SEC`
- Status: draft
- Reference: go-stablenet `740526d03`.

이 장은 Part A 의 보장이 성립하는 가정과, 그 가정을 약하게 만들거나 그 가정에 기대는 참조 구현의 성질을 적는다. 각 절은 같은 순서를 따른다. 먼저 가정이나 위협을 적고, 참조 구현이 하는 일을 source 와 함께 적는다. 그다음 관측 가능한 동작을 바꾸지 않고 요구사항을 세울 수 있으면 규범 요구사항을 적는다.

이 장의 요구사항은 두 종류다. 하나는 *가정 요구사항* ("배포는 반드시 …해야 한다") 이다. Part A 의 보장이 성립하려면 적합한 네트워크가 이 요구사항을 만족해야 한다. 다른 하나는 적합한 노드에 대한 *행동 요구사항* 이다. 어느 종류도 Part A 의 validity 규칙을 바꾸지 않는다.

system contract 의 보안 고려 사항은 `B-04` §13 이 다룬다. 그 절은 서명만으로 native coin 이 옮겨지는 경로, token 서명의 replay 범위, blacklist 검사 시점을 적는다.

---

## 1. 위협 모델

| 요소 | 가정 |
|---|---|
| Validator | 높이마다 `N = |validators_at(h)|` 이고, 모든 validator 의 가중치는 같다. `A-04` 에서 모든 candidate 의 power 가 1 이기 때문이다. 그 가운데 byzantine validator 는 많아야 `f` 명이며, validator 수로 세어 `3f < N` 이다. byzantine validator 는 서로 공모할 수 있고, 자기 키로 무엇이든 서명할 수 있고, 메시지와 block 을 보류할 수 있고, 메시지를 보낼 시점을 고를 수 있다. |
| 정직한 validator | 정직한 validator 는 Part A 를 따르고, node key 를 비밀로 지키며, 서로의 시계 차이가 `AllowedFutureBlockTime` 안에 있다 (§10, WBFT-SEC-090). |
| 네트워크 | 네트워크는 부분 동기다. 알 수 없는 global stabilisation time 이 지난 뒤에는 정직한 validator 사이의 메시지가 `A-06` 의 round timeout 보다 작은 한도 안에 전달된다. 그 전에는 공격자가 메시지를 지연시키고, 순서를 바꾸고, 복제하고, 버릴 수 있지만 signature 를 위조할 수는 없다. validator 들은 직접 연결되어 있다. `A-07` 에 따라 노드는 합의 메시지를 validator peer 에게만 보낸다. validator 는 메시지를 성공적으로 처리한 뒤에 자기 validator peer 에게 relay 한다. 그래서 relay 하는 노드에게 메시지가 아직 future 이면 그 지점에서 relay 가 멈춘다 (`WBFT-SEC-002`). |
| Non-validator peer | non-validator peer 는 믿을 수 없다. non-validator peer 는 `istanbul/100` 과 `eth` protocol 에서 임의의 바이트를 보낼 수 있다. |
| 실행 | 실행은 결정적이다. 정직한 노드는 모두 같은 block 과 parent state 에 대해 같은 post-state 를 계산한다 (§9). |
| 로컬 운영자 인터페이스 | Engine API (JWT 인증을 쓰는 authrpc 와, JWT 없이 여는 IPC), admin RPC, miner RPC 에는 공격자가 닿을 수 없다 (§13). |
| 암호 | ECDSA secp256k1, BLS12-381 (proof-of-possession 방식, `A-02`), Keccak-256 은 안전하다. `EpochInfo` 의 BLS public key 는 소유가 증명되어 있다 (§4). |

QBFT 형식 명세(commit `1630128e7`)는 이 모델과 가까운 모델에서 safety 성질을 증명한다. 그 모델의 공격자는 고정된 집합에서 많아야 `f` 명의 validator 를 제어하고, 자기 key 로 무엇이든 서명하며, 받은 메시지는 무엇이든 replay 한다. 그러나 정직한 노드의 signature 는 위조하지 못한다. 그 모델의 네트워크는 메시지를 한없이 늦추거나 아예 전달하지 않을 수 있다 (`dafny/ver/L1/distr_system_spec/adversary.dfy:18-82`, `dafny/ver/L1/distr_system_spec/network.dfy:29-43`). 증명은 또 validator 집합이 바뀌지 않고 정직한 노드가 상태를 잃지 않는다고 가정한다. WBFT 는 이 두 가정을 만족하지 않는다 (`A-05` §18.5).

[WBFT-SEC-001] 배포는 반드시 모든 높이에서 byzantine validator 의 수를 `N/3` 보다 엄격하게 작게 유지해야 한다. 이 가정 아래에서는 높이마다 많아야 block 하나가 유효한 committed seal 을 모은다 (`A-05`). 이 장의 어떤 장치도 가정 위반을 검출하거나 위반에서 복구하지 않는다.
Source: `consensus/wbft/validator/default.go:222-229`, `consensus/wbft/engine/engine.go:1338-1369`, `consensus/wbft/engine/engine.go:881`

> 해설: 가정이 깨졌을 때를 위한 장치가 없다는 점이 중요하다. equivocation 증거(§5)도, 벌칙도, chain 분기를 푸는 규칙도 없다. `A-08 §9` 의 TD 규칙은 seal 을 보지 않는다. 그래서 가정이 깨지면 사후 분석조차 어렵다. 운영자는 이 점을 알고 validator 구성을 정해야 한다.

[WBFT-SEC-002] 배포는 모든 validator 쌍을 `istanbul/100` 으로 직접 연결하는 것이 좋다 (SHOULD). 노드는 합의 메시지를 validator peer 에게만 보낸다. 노드는 받은 메시지를 성공적으로 처리한 뒤에만 자기 validator peer 에게 relay 한다. 메시지가 future 로 backlog 에 들어가 있는 동안에는 relay 하지 않고, old 나 invalid 로 분류된 메시지는 절대 relay 하지 않는다. extra seal 메시지로 분류된 메시지는 extra seal 저장소가 오류 없이 돌아오면 relay 한다. 여기에는 digest 와 seal 이 검증된 PREPARE 와 COMMIT 이 들어가고, 노드가 대상 proposal 을 갖고 있지 않을 때에는 어떤 메시지든(PRE-PREPARE 와 검증할 수 없는 seal 을 가진 메시지를 포함해) 들어간다 (`A-07` WBFT-NET-042). non-validator 는 적합한 노드로부터 합의 메시지를 받지 않으므로 (`A-07` §6.2) 보통은 아무것도 relay 하지 않는다. 그래서 직접 연결되지 않은 validator 끼리는 서로의 메시지를 늦게 보거나 아예 보지 못한다.
Source: `consensus/wbft/backend/backend.go:176-210`, `eth/handler_istanbul.go:42-54`, `consensus/wbft/core/handler.go:128-150, 210-221`, `consensus/wbft/core/extraseal.go:38-47`

---

## 2. Quorum 산술

`quorum_size(N) = ceil(N − (N−1)/3)` 이며, IEEE-754 배정밀도로 계산한다 (`consensus/wbft/validator/default.go:222-229`). `N < 3·2^50 + 3` 인 모든 `N` 에서 이 값은 정확한 유리수 올림 `ceil((2N+1)/3)` 과 같다. 현실적인 validator 수는 모두 이 범위에 든다. binary64 반올림 때문에 두 값이 처음 달라지는 값은 `N = 3·2^50 + 3` 이다. 이 경계 아래에서는 `f = floor((N−1)/3)` 에 대해 두 quorum 의 교집합에는 적어도 `f + 1` 명의 validator 가 들어 있고, 이 교집합 성질이 safety 의 근거다. `N ≡ 0 or 2 (mod 3)` 이면 quorum 이 `2f + 1` 보다 크다 (예: `N = 6` 이면 `f = 1` 이고 quorum 은 5 다). 그래서 이런 크기는 `N − 1` 명이나 `N − 2` 명일 때보다 더 많은 결함을 견디지 못하면서, liveness 를 위해 더 많은 signature 를 필요로 한다.

[WBFT-SEC-010] 적합한 노드는 반드시 위의 식으로 `quorum_size` 를 계산해야 하며, 두 값이 다른 크기에서 `2f + 1` 로 대신해서는 안 된다.
Source: `consensus/wbft/validator/default.go:222-229`
Observable: header

참고 (informative). WBFT-SEC-010 이 금지하는 치환은 Quorum QBFT 를 옮겨 온 구현에 가장 들어오기 쉬운 치환이다. Quorum commit `5ffacc48` 의 `QuorumSize` 는 `2FPlus1Enabled` transition 이 켜져 있거나, `Ceil2Nby3Block` 이 설정되지 않았거나 (폐기 예정인 `istanbul` genesis 절에 `ceil2Nby3Block` 이 없을 때), sequence 가 `Ceil2Nby3Block` 보다 작으면 `2F + 1` 을 돌려준다. 여기서 `F = ceil(N/3) − 1 = (N − 1) // 3 = f` 이다. 그 밖의 경우에만 `ceil(2N/3)` 을 쓴다 (`consensus/istanbul/qbft/core/core.go:306-313`, `consensus/istanbul/validator/default.go:205`, `eth/ethconfig/config.go:248`; `A-04` §2.3).

> 해설: `2f + 1` 로 바꾸면 header seal 검증 결과가 달라진다. `A-13` §3.2 는 같은 식을 QBFT 의 `ceil(2N/3)` 과 비교해 "`N` 이 3 의 배수일 때에만 WBFT 쪽이 하나 크다" 고 적는다. 이 문장은 이 절의 "`N ≡ 0, 2 (mod 3)` 이면 `2f + 1` 보다 크다" 와 모순처럼 보이지만, 비교 대상이 다를 뿐이다 (`ceil(2N/3)` 과 `2f + 1`). 예를 들어 `N = 5` 이면 WBFT 와 QBFT 의 quorum 은 모두 4 이고 `2f + 1` 은 3 이다.

Informative. validator `N` 명의 집합은 결함이 있는 validator `F = (N − 1) // 3` 명을 견딘다. `F` 명을 견디는 가장 작은 크기는 `3F + 1` 이다. 크기가 `3F + 2` 나 `3F + 3` 인 집합은 같은 `F` 를 견디면서 quorum 만 커진다. 예를 들어 `N = 5` 와 `N = 6` 은 `N = 4` 처럼 결함이 있는 validator 한 명을 견디지만, quorum 은 3 이 아니라 각각 4 와 5 가 필요하다. 그러므로 `3F + 1` 꼴의 validator 수가 집합을 가장 효율적으로 쓴다.

---

## 3. Replay 범위

signature 는 그 signed 바이트를 받아들이는 모든 문맥에서 replay 될 수 있다. WBFT 산출물의 signed 바이트는 다음과 같다 (`A-02`, `A-03`):

| 산출물 | Signed 바이트 | chain 에 묶이는가? | 높이와 round 에 묶이는가? |
|---|---|---|---|
| PRE-PREPARE (ECDSA) | `rlp([0x12, [seq, round, block]])` | 예. block(parent hash)을 거쳐 묶인다 | 예 |
| PREPARE, COMMIT (ECDSA) | `rlp([code, [seq, round, digest, seal]])` | 예. `digest` 를 거쳐 묶인다 | 예 |
| prepared certificate 가 있는 ROUND-CHANGE | `rlp([0x15, [seq, round, [prepared_round, prepared_digest]]])` | `prepared_digest` 를 거쳐 묶인다 | 예 |
| Seal (BLS) | `seal_data(header, round, type)` | 예 (header) | 예 |
| Randao reveal (ECDSA) | `randao_data(chain_id, number)`. 이 값은 이미 keccak256 digest 다 (`A-02` §7.1) | 예 (`chain_id`) | 높이에만 묶인다 |

Source: `consensus/wbft/messages/preprepare.go:50-56`, `consensus/wbft/messages/prepare.go:61-67`, `consensus/wbft/messages/commit.go:49-55`, `consensus/wbft/messages/roundchange.go:158-182`, `consensus/wbft/core/core.go:465-469`, `consensus/wbft/engine/engine.go:563-573`

[WBFT-SEC-021] 한 chain 안에서 노드는 replay 된 합의 메시지를 반드시 원본과 똑같이 받아들여야 한다. 메시지에 nonce 나 timestamp 가 없기 때문이다. 노드는 중복을 signature 의 유일성으로 억제하지 않는다. 노드는 `dedup_key(payload)` 로 중복을 걸러 낸다. `dedup_key` 는 payload 바이트를 RLP string 으로 인코딩한 값의 keccak256 이다 (`A-03`, `A-07`). 또 노드는 메시지 집합 안에서 sender 별로 메시지를 교체한다 (`A-05`).
Source: `consensus/wbft/backend/handler.go:54-67, 83-95`, `consensus/wbft/utils.go:31-36`, `consensus/wbft/core/qbft_msg_set.go:66-71`
Observable: network

---

## 4. 키 결합과 키 관리

node key (secp256k1) 는 validator 의 유일한 비밀이다. 참조 구현은 node key 에서 다음을 끌어낸다:

- p2p identity (enode), 그리고 그에 따라 합의 peer 를 고를 때 쓰는 주소 (`eth/handler_istanbul.go:42-54, 164-166`)
- validator 주소 (`Coinbase`, 메시지의 source, 이 주소로 강제되는 `etherbase`: `eth/backend.go:173-176`)
- 합의 메시지와 randao reveal 에 서명하는 ECDSA 키 (`consensus/wbft/backend/backend.go:281-284`)
- BLS secret key. 결정적으로 `bls_secret = KeyGen(ikm = node_key_bytes)` 로 끌어낸다 (`consensus/wbft/backend/backend.go:65`, `crypto/bls/bls.go:91-93`, `crypto/bls/blst/secret_key.go:36-46`)

그 결과는 셋이다. node key 가 유출되면 네 역할이 한꺼번에 유출된다. node key 를 바꾸지 않고는, 즉 validator 주소를 바꾸지 않고는 BLS 키를 교체할 수 없다. 코드를 바꾸지 않고는 seal 만 remote signer 로 옮길 수 없다.

verifier 가 쓰는 BLS public key 는 verifier 가 직접 끌어내지 않는다. 그 키는 validator registry 에 등록되어 `EpochInfo` 에 복사된 키다 (`B-08`). registry 는 proof of possession 과 키의 유일성을 검사하므로 (`systemcontracts/solidity/v1/GovValidator.sol:64-76, 157-165`) rogue-key aggregation 을 막는다. 합의 자체는 `EpochInfo` 의 키들이 서로 다른지 검사하지 않는다.

[WBFT-SEC-030] validator 는 반드시 위와 같이 자기 node key 에서 끌어낸 BLS public key 를 registry 에 등록해야 한다. 그렇지 않으면 그 validator 의 seal 은 verify 되지 않고, 그 validator 는 모든 quorum 에서 없는 것으로 계산된다.
Source: `consensus/wbft/backend/backend.go:65, 287-289`, `consensus/wbft/engine/engine.go:1345-1366`
Observable: header, state

[WBFT-SEC-031] 배포는 반드시 genesis validator 집합의 BLS public key 가 서로 다르고 소유가 증명되었음을 보장해야 한다. genesis 키는 설정에서 가져오며 registry 가 검사하지 않기 때문이다. 참조의 어떤 구성 요소도 이 조건을 검사하지 않는다 (`params/config_wbft.go:76-129`, `systemcontracts/gov_validator.go:113-163`).
Source: `consensus/wbft/engine/engine.go:1104-1107, 660-662`
Source: `params/config_wbft.go:76-129` (CheckValidity checks only that the key list is non-empty and as long as the validator list), `systemcontracts/gov_validator.go:113-163` (duplicate addresses are skipped; a repeated key overwrites the earlier `blsKeyToValidator` entry)

node key 가 곧 모든 validator 키이므로, p2p 계층에서 키를 알아내는 공격은 곧 validator 에 대한 공격이다. RLPx handshake 는 인증되지 않은 네트워크 입력을 ECIES 로 복호한다. 이 복호는 node key 와 송신자가 고른 점 사이의 ECDH 다 (`p2p/rlpx/rlpx.go:603-626`). 그 점이 secp256k1 위에 있는지 검사하지 않으면, 송신자는 다른 곡선 위의 작은 위수 점을 보낼 수 있고 (invalid-curve 공격), 복호의 성공 여부로부터 node key 를 작은 소수로 나눈 나머지를 알아낼 수 있다. 참조 구현은 이런 점을 scalar 곱셈 전에 거부한다. ECIES `GenerateShared` 는 곡선 위에 없는 점에 대해 `ErrInvalidPublicKey` 를 돌려준다 (`crypto/ecies/ecies.go:123-129`). `UnmarshalPubkey` 와 `IsOnCurve` 는 field prime `P` 보다 작지 않은 좌표를 거부한다 (`crypto/crypto.go:176-185`, `crypto/secp256k1/curve.go:75-78`). cgo scalar 곱셈도 그런 좌표를 거부한다 (`crypto/secp256k1/ext.h:108-116`). RLPx 테스트 `TestHandshakeECIESInvalidCurveOracle` 는 auth 메시지의 ephemeral 점을 곡선 밖의 점으로 바꾸면 `ErrInvalidPublicKey` 로 실패하는지 확인한다. 이 검사들은 `v1.0.0` 이후에 들어왔다 (`A-12` §3).

[WBFT-SEC-032] 적합한 노드는 네트워크에서 받은 secp256k1 public key 나 점 가운데 자기 node key 나 ephemeral key 로 곱할 것은, 두 좌표가 모두 field prime `P` 보다 작고 그 점이 `y² = x³ + 7` 을 만족하지 않으면 반드시 거부해야 한다. 대상은 RLPx `auth` 나 `ack` 메시지의 ECIES 점 `R`, RLPx ephemeral key 와 static key, 그리고 ECDH 에 쓰는 discovery key 다. 노드는 반드시 scalar 곱셈 전에 거부해야 한다. 그래야 거부 여부가 MAC 검사나 그 뒤 단계의 결과에 기대지 않는다.
Source: `crypto/ecies/ecies.go:123-129, 293-311`, `crypto/secp256k1/ext.h:108-116`, `crypto/crypto.go:176-185`, `crypto/secp256k1/curve.go:75-78`, `p2p/rlpx/rlpx.go:648-666`, `p2p/rlpx/rlpx_oracle_poc_test.go:12-57`

---

## 5. Equivocation

합의 코어가 한 번 실행되는 동안, 정직한 노드는 `(sequence, round)` 마다 PREPARE 하나와 COMMIT 하나까지만 보내고, round 가 0 보다 크면 PRE-PREPARE 도 하나까지만 보낸다. 노드는 `AcceptRequest` state 에서 PRE-PREPARE 를 받아들일 때에만 PREPARE 를 보내고, `Prepared` 로 전이할 때에만 COMMIT 을 보낸다 (`A-05`). byzantine validator 는 같은 view 에 대해 서로 다른 두 digest 에 서명할 수 있다. round 0 에서는 정직한 proposer 도 같은 view 에 서로 다른 block 의 PRE-PREPARE 를 두 번 보낼 수 있다. proposer 가 아직 `AcceptRequest` 에 있을 때 같은 sequence 의 두 번째 요청이 오면 PRE-PREPARE 를 또 보낸다. 예를 들어 첫 PRE-PREPARE 의 self-delivery 가 future block 으로 미뤄지면 proposer 는 `AcceptRequest` 에 남는다. round 0 요청 경로가 `preprepareSent` 를 검사하지 않기 때문이다 (`A-05` WBFT-SM-082). 합의 코어를 멈췄다가 다시 시작하면 (예: 재시작), 참조 구현은 자기가 보낸 메시지를 기억하지 않는다 (`A-05` WBFT-SM-007). 그래서 재시작 뒤에는 이미 서명한 `(sequence, round)` 에 대해 다른 PREPARE, COMMIT, proposal 에 서명할 수 있다. `A-05` 는 WBFT-SM-082 뒤의 노트에서 두 한정을 모두 적는다.

참조 구현은 equivocation 을 검출하지도 기록하지도 않는다:

- 메시지 집합은 sender 마다 메시지 하나만 두고 덮어쓴다 (`consensus/wbft/core/qbft_msg_set.go:66-71`).
- digest 가 현재 proposal 과 다른 PREPARE 나 COMMIT 은 저장하지 않고 거부한다 (`A-05`).
- 노드가 `AcceptRequest` 를 떠난 뒤에 같은 view 의 두 번째 PRE-PREPARE 가 오면 무시한다 (`consensus/wbft/core/backlog.go:166-173`, `consensus/wbft/core/preprepare.go:172`).
- 증거 형식도, slashing 도, 벌칙도 없다.

[WBFT-SEC-040] 합의 코어가 한 번 실행되는 동안, 적합한 노드는 같은 `(sequence, round)` 에 대해 PREPARE 에서 서로 다른 두 digest 에 서명하거나, COMMIT 에서 서로 다른 두 digest 에 서명해서는 안 된다. proposer 는 `round > 0` 인 같은 `(sequence, round)` 에 대해 서로 다른 두 proposal 에 서명해서는 안 된다. 참조 구현은 두 경우에 이 범위 밖에서 서명한다. 첫째, 참조 구현은 아직 `AcceptRequest` 에 있는 동안 한 view 의 round 0 proposal 두 개에 서명한다 (`A-05` WBFT-SM-082). 둘째, 참조 구현은 재시작을 넘어 서명한 메시지를 기억하지 않는다 (`A-05` WBFT-SM-007). 구현은 두 경우를 모두 피하는 것이 좋다 (SHOULD). 이 변경은 local-only 다.
Source: `consensus/wbft/core/preprepare.go:42-51, 171-194`, `consensus/wbft/core/prepare.go:47`, `consensus/wbft/core/commit.go:49`, `consensus/wbft/core/request.go:47-56`, `consensus/wbft/backend/backend.go:355-382`
Observable: network

[WBFT-SEC-041] 같은 view 에 대해 한 validator 가 서로 다른 digest 에 한 유효한 signature 두 개를 본 observer 는 그 두 signature 를 equivocation 으로 보고하는 것이 좋다 (SHOULD). 대상은 PREPARE, COMMIT, 그리고 ROUND-CHANGE 나 PRE-PREPARE 안의 PREPARE justification 이다. 참조 구현에는 다른 검출 수단이 없다.
Source: `consensus/wbft/core/qbft_msg_set.go:66-71`
Observable: network

block header 의 seal 은 한 높이 안의 equivocation 증거가 되지 않는다. `WBFT-SEC-001` 이 성립하면 높이마다 digest 하나만 chain 에 오르기 때문이다.

---

## 6. Peer 책임

합의 수준의 유효성(signature, sender membership, view, proposal 유효성)은 network handler 가 이미 성공을 돌려준 뒤에 합의 event loop 에서 비동기로 평가된다. `istanbul/100` 연결을 끊는 오류는 frame 디코드 실패, 크기를 넘는 frame, 그리고 동기화 중이 아닐 때의 "engine stopped" 뿐이다 (`eth/handler_istanbul.go:115-153`, `consensus/wbft/backend/handler.go:70-101`). 무효 proposal, 무효 seal, non-validator 의 메시지는 받는 노드에게 signature recover 비용만 들게 한 뒤 버려진다. 점수를 매기거나 연결을 끊는 일은 없다.

`eth` protocol 에서는 노드가, 전파한 block 이 block fetcher 의 header 검증에서 `ErrFutureBlock` 이 아닌 오류로 실패한 peer 와 연결을 끊는다 (`eth/fetcher/block_fetcher.go:858-872`).

[WBFT-SEC-050] 적합한 노드는 반드시 모든 합의 메시지와 그 메시지에 끼워 넣은 모든 justification 의 ECDSA signature 와 validator membership 을, 그 메시지를 backlog 나 메시지 집합에 저장하기 전에 verify 해야 한다.
Source: `consensus/wbft/core/handler.go:188-209, 268-317`, `consensus/wbft/core/core.go:452-462`
Observable: network

---

## 7. 자원 한도

| 자원 | 참조 구현의 한도 | Source |
|---|---|---|
| `istanbul/100` frame | 10 MiB | `eth/handler.go:59`, `eth/handler_istanbul.go:138-140` |
| PRE-PREPARE 와 ROUND-CHANGE 안의 proposal (block) | frame 한도뿐이다. proposal 에는 header sanity check (`Number`, `Difficulty` 비트 길이, `Extra ≤ 100 KiB`) 를 적용하지 않는다 | `core/types/block.go:149-167`, `eth/protocols/eth/handlers.go:305-307` (`NewBlockMsg` 에만 적용) |
| validator 당 future 메시지 | `4 · (10 + 1) · (1 + 1) = 88`. sequence ≤ current + 1 이다. 현재 sequence 에서는 round ≤ 현재 round + 10 이고, sequence current + 1 에서는 round ≤ 9 이다 | `consensus/wbft/core/backlog.go:44-50, 76-105` |
| 중복 cache | peer 40 개 × hash 1024 개, 자기 hash 1024 개 | `consensus/wbft/backend/engine.go:39-42` |
| header 안의 `EpochInfo` 와 sealer bitmap 크기 | 디코딩 말고는 제한이 없다 | `core/types/istanbul.go:99-103, 290-325` |
| ROUND-CHANGE 재전송 | `RequestTimeout` 마다 다시 만들며, prepared block 이 있으면 매번 싣는다. 최근 cache 에 그 key 가 있는 peer 에게는 바이트가 같은 사본을 다시 보내지 않는다. `finalize` 가 실패한 뒤, `CATCH_UP` 분기 뒤, 그리고 새 height 에서 처리한 retry 는 원래 메시지와 다른 메시지이므로 wire 로 나간다 (`A-14` §5.8) | `A-06` WBFT-TIMER-021, WBFT-TIMER-024, `A-07` WBFT-NET-032 |

sender 별 또는 view 별 속도 제한은 없다. 바이트가 새로운 frame 은 모두 디코딩되고 signature 가 recover 된다 (`consensus/wbft/backend/handler.go:66-95`, `consensus/wbft/core/handler.go:185-209`). frame 하나의 비용은 frame 크기로만 한정된다. 노드는 메시지에 대해 ECDSA recover 를 한 번 하고, 첫 실패가 나올 때까지 justification 항목마다 한 번씩 더 한다 (`consensus/wbft/core/handler.go:285-312`). 정직한 ROUND-CHANGE 재전송은 정상적인 경우 대역폭을 더하지 않는다. 서명이 결정적이므로 재전송한 ROUND-CHANGE 는 바이트가 같고, 이미 그 key 를 가진 모든 peer 에게는 건너뛴다 (`A-06` WBFT-TIMER-024). 재전송본은 원래 송신 뒤에 처음 연결된 peer 나 cache 항목이 밀려난 peer 에게만 wire 로 나간다. 다만 `A-14` §5.8 의 세 경우에는 retry 가 원래 메시지와 달라서 모든 validator peer 에게 나간다. retry 는 앞서 보낸 메시지와 바이트까지 같을 때에만 억제된다 (`A-06` WBFT-TIMER-024).


---

## 9. Quorum seal 을 가진 bad block

PRE-PREPARE 검증은 proposal 을 실행하지 않는다 (`A-09 §4.6`). 그래서 byzantine proposer 는 실행에 실패하는 block 에 대해 COMMIT quorum 을 얻을 수 있다. 예를 들어 blacklist 된 sender 의 transaction, 무효 state root, 틀린 `EpochInfo` 가 들어간 block 이다. 그러면 정직한 non-proposer 는 모두 그 block 의 import 에 실패하고, hash 를 bad 로 기록한 뒤 새 round 에서 그 높이를 계속 진행한다 (`A-09 §5.3`). liveness 비용은 round 하나와 bad block 을 실행하는 시간이다. bad block 은 정직한 노드에서 canonical 이 되지 않으므로 safety 는 영향을 받지 않는다.

해가 없다고 할 수 없는 경우가 둘이다:

- **비결정적 실행.** 정직한 노드끼리 block 이 실행되는지에 대해 판단이 갈리면, 일부는 block 을 저장하고 앞으로 가고, 나머지는 bad 로 기록하고 같은 높이에서 다른 block 을 decide 한다. 두 무리가 모두 그 높이에서 유효한 committed seal 을 가진 block 을 가진다. 그래서 chain 이 갈라지고, TD head 규칙 (`A-08 §9`) 은 한 branch 가 더 길어질 때까지 이 분기를 풀지 못한다.
- **bad block 을 만든 정직한 builder.** builder 는 자기 block 을 다시 실행하지 않고 저장한다 (`WBFT-APP-080`). builder 가 만들 때의 실행과 다른 노드가 import 할 때의 실행이 어긋나면 builder 혼자 앞으로 간다.

[WBFT-SEC-080] finalized block 의 실행은 반드시 모든 적합한 구현에서 결정적이어야 한다. 노드는 실행 결과가 로컬 설정, 타이밍, 자원에 따라 달라지게 해서는 안 된다.
Source: `core/blockchain.go:1778-1792`, `core/state_processor.go:60-107`
Observable: state

[WBFT-SEC-081] 높이 `h` 에서 bad block 이 생긴 뒤, 적합한 노드는 높이 `h` 에서 그 bad block 을 다시 PREPARE 하거나 COMMIT 해서는 안 된다. 노드는 PRE-PREPARE 에서 그 block 을 거부하고, round change 때 그 block 을 prepared block 에서 버린다. 참조 구현에서 이 보호는 bad block 기록이 남아 있는 동안에만 유지되며, 참조 구현은 번호가 가장 큰 기록 10 개만 남긴다 (`A-09` WBFT-APP-004).
Source: `consensus/wbft/backend/backend.go:266-270`, `consensus/wbft/core/core.go:278-286`
Observable: network

---

## 10. 시계 가정

- proposal 은 `Time = max(parent.Time + BlockPeriod, proposer_now)` 를 싣는다 (`WBFT-HDR-013`).
- verifier 는 `Time > now + AllowedFutureBlockTime` 인 proposal 을 future 로 보고, `Time` 에 다시 처리한다. 이 대기에는 상한이 없다 (`WBFT-HDR-063`). `AllowedFutureBlockTime` 의 기본값은 0 이다.
- round timer 는 로컬 상대 시간을 쓴다 (`A-06`). round timer 는 block 시각을 쓰지 않는다.

시계가 앞서가는 proposer 는 모든 정직한 verifier 에서 자기 proposal 을 그 시차만큼 늦춘다. 시차가 round timeout 을 넘으면 그 proposal 은 그 round 에서 받아들여지지 않는다. 그 block 이 일단 finalize 되면 chain 시각이 앞으로 밀린다. 그 뒤의 정직한 proposer 는 적어도 `parent.Time + BlockPeriod` 를 써야 하는데, 이 값은 그들의 시계보다 앞서 있으므로 그들의 proposal 도 벽시계가 따라잡을 때까지 늦어진다. 그래서 byzantine proposer 는 chain 을 느리게 만들 수 있지만 safety 를 깰 수는 없다. 시계가 다른 노드보다 앞선 정직한 노드도 의도하지 않게 같은 효과를 낸다.

[WBFT-SEC-090] 배포는 반드시 validator 시계의 차이를 다음 두 한도 안으로 맞춰야 한다. proposal 이 지연 없이 받아들여지려면 시계 차이가 `AllowedFutureBlockTime` 안에 있어야 하고, liveness 를 위해서는 시계 차이가 round-0 timeout 보다 충분히 작아야 한다.
Source: `consensus/wbft/engine/engine.go:201-205, 501-505`, `consensus/wbft/core/preprepare.go:150-163`

---

## 11. 난수

`MixDigest(h) = MixDigest(h−1) XOR keccak256(RandaoReveal(h))` 이다 (`A-02`). `MixDigest` 는 contract 에 `PREVRANDAO` 로 노출되고 (`core/evm.go:61-63`), epoch block 에서는 다음 epoch 의 validator shuffle 의 seed 가 된다 (`consensus/wbft/engine/engine.go:1265-1277`).

- 정직한 proposer 의 reveal 은 결정적이다 (RFC 6979). 그래서 모두 정직한 chain 의 mix 수열은 높이와, 각 block 을 어느 validator 가 만들었는지로 정해진다. 어느 validator 가 block 을 만드는지는 round change 로 바뀔 수 있다.
- 마지막 기여자에게는 흔히 알려진 RANDAO 편향이 있다. 마지막 기여자는 보류할 수 있다. 즉 proposal 을 내지 않아서 그 높이를 다음 proposer 와 다른 reveal 에 넘길 수 있다.
- proposer 는 자기 block 의 mix 를 제안하기 전에 안다.


---

## 13. Engine API 노출

WBFT chain 에서도 노드는 developer mode 로 돌지 않는 한 Engine API (`engine_*`)를 등록한다 (`cmd/gstable/config.go:223-235`, `eth/catalyst/api.go:44-54`). 노드는 Engine API 를 JWT 인증을 쓰는 authrpc endpoint 에서 제공한다. authrpc 는 인증 API 가 하나라도 등록되면 항상 기동된다 (`node/node.go:501-508`). 또 노드는 JWT 없이 IPC endpoint 와 in-process 에서도 Engine API 를 제공한다. IPC 와 in-process 서버는 `personal` 을 뺀 모든 등록 API 를 받기 때문이다 (`node/node.go:379-398`). Engine API 를 끄는 설정은 없다.

[WBFT-SEC-120] WBFT 노드는 Engine API 를 자기 운영자 말고는 누구에게도 노출해서는 안 된다. 배포는 반드시 authrpc 를 로컬 인터페이스에 묶고 비밀 JWT 키를 써야 하며, IPC 를 끄거나 (`--ipcdisable`) IPC socket 을 운영자만 쓰게 제한하는 것이 좋다 (SHOULD).
Source: `cmd/gstable/config.go:223-235`, `eth/catalyst/api.go:44-54, 265-371`, `node/node.go:379-398, 501-508`, `consensus/merger.go:60-94`, `eth/handler.go:228-291`

---

## 14. 관측한 header 의 무결성

저장된 header 의 seal 필드 `Round`, `PreparedSeal`, `CommittedSeal` 은 노드 로컬이며 block hash 에 포함되지 않는다 (`WBFT-HDR-053`). 노드 하나에서 header 를 읽는 RPC client 는 그 노드의 사본을 받는다. seal 이 verify 되면(`verify_light`) 그 사본은 진짜다. 그러나 그 사본의 sealer 집합은 참여의 canonical 기록이 아니다. block `h` 에 누가 seal 했는지의 canonical 기록은 block `h+1` 의 이전 seal 필드이며, 이 필드는 hash 에 포함된다.

[WBFT-SEC-130] validator 의 참여를 집계하는 observer 는 반드시 block `h+1` 의 `PrevPreparedSeal` 과 `PrevCommittedSeal` 을 block `h` 의 기록으로 써야 한다. observer 는 block `h` 에 저장된 seal 을 그 노드 하나의 관점으로만 써도 된다.
Source: `core/types/istanbul.go:263-288`, `consensus/wbft/engine/engine.go:515-537`
Observable: header, rpc
