# A-01. 표기법, 기본 타입, 상수, 설정 파라미터

- 상태: 초안이다.
- 영역: `TYPE`, `PARAM`
- 레퍼런스 구현: go-stablenet `740526d03`. 모든 `Source:` 줄의 경로는 그 commit 의 저장소 root 를 기준으로 한다.

이 장은 다른 모든 장이 쓰는 어휘를 고정한다. 이 장이 정하는 것은 다음과 같다.

- 용어집,
- 각 기본 타입의 정확한 크기와 범위,
- 한 값의 표현이 wire 와 block header 에서 서로 다를 때 각 자리의 표현,
- 프로토콜 상수,
- 합의 설정 `Config` 와, 주어진 높이에 적용되는 설정을 돌려주는 함수 `config_at(number)`.

인코딩 자체는 `A-03` 에서 정의하고, 암호 함수는 `A-02` 에서 정의한다.

> 해설: 이 장과 `A-02`, `A-03` 은 나머지 모든 장이 기대는 바닥이다. 같은 값이 메시지에서는 상한 없는 정수이고 header 에서는 32 비트로 잘린다. 이런 차이에서 한 바이트라도 어긋나면 signature 가 verify 되지 않거나 block hash 가 달라진다. 그러면 구현은 go-stablenet 노드와 같은 chain 에 머물 수 없다.

---

## 1. 이 장에서 쓰는 규약

요구사항 용어, 요구사항 식별자, `Source:` 태그와 `Observable:` 태그는 `README.md §2` 를 따른다. 의사코드는 `README.md §2.4` 를 따른다. 이 장은 여기에 다음 규약을 더한다.

- `uint8`, `uint32`, `uint64` 는 그 비트 폭의 부호 없는 정수다. 이 타입의 산술은 본문이 그렇다고 밝힌 곳에서만 2^width 로 나눈 나머지로 wrap 된다.
- `bigint` 는 상한이 없는 음이 아닌 정수다. 레퍼런스 구현은 이런 값을 `*big.Int` 에 담는다. 디코딩에서 음수 값은 나오지 않는다 (`A-03 §3`).
- `bytes` 는 길이에 제한이 없는 바이트 문자열이고, `bytesN` 은 길이가 정확히 `N` 바이트인 바이트 문자열이다.
- `x[a:b]` 는 offset `a` (포함) 부터 offset `b` (제외) 까지의 바이트 조각이다.
- `‖` 는 바이트 문자열 연결이다.
- `be_min(x)` 는 `bigint` 의 최소 big-endian 바이트 표현이다. 이 표현은 앞자리에 0 바이트를 두지 않고, 0 은 빈 문자열로 표현한다. Go 의 `big.Int.Bytes()` 가 돌려주는 값이 바로 이 표현이다.

---

## 2. 용어집

이 표의 용어는 모든 장에서 정확히 이 뜻으로 쓴다.

| 용어 | 뜻 |
|---|---|
| validator | 어떤 높이의 validator 집합에 속하고, 그 높이에 대해 합의 메시지와 seal 을 보낼 수 있는 계정이다 (`A-04`). validator 는 자신의 `Address` 와 `BLSPublicKey` 로 식별한다. |
| candidate | `EpochInfo.candidates` 에 나열된 계정이며, 다음 epoch 의 validator 는 candidate 가운데서 고른다 (`A-04`). 모든 validator 는 candidate 이지만, 모든 candidate 가 validator 인 것은 아니다. |
| node key | 노드의 secp256k1 비밀 키다. node key 는 합의 메시지와 randao reveal 에 서명하며, 노드의 BLS 키는 node key 에서 유도한다 (`A-02 §5.1`). |
| proposer | 한 view 에 대해 PRE-PREPARE 를 보낼 자격이 있는 validator 다 (`A-04`). proposer 가 조립한 block 은 `Header.Coinbase` 에 proposer 의 주소를 담는다. |
| sequence | 합의 대상인 높이 (block 번호) 다. header 맥락에서는 *number* 라고 부른다. |
| round | 한 sequence 안의 counter 다. round 는 0 에서 시작하고 round change 가 일어날 때마다 증가한다 (`A-05`). |
| view | `(sequence, round)` 쌍이다. view 는 sequence 로 먼저 정렬하고, sequence 가 같으면 round 로 정렬한다. |
| proposal | 한 view 에 제안된, header 와 body 를 모두 갖춘 block 이다. |
| digest | `block_hash(proposal.header)` (`A-03 §6`) 이다. 즉 현재 block 의 seal 을 빼고 round 를 0 으로 둔 header 의 hash 다. |
| seal | validator 하나가 `seal_data(header, round, seal_type)` (`A-02 §6`) 에 대해 만든 96 바이트 BLS signature 다. |
| prepare seal / commit seal | `seal_type = PREPARE_SEAL` 인 seal 과 `seal_type = COMMIT_SEAL` 인 seal 이다. 각각 PREPARE 메시지와 COMMIT 메시지에 실린다. |
| aggregated seal | validator 집합 하나가 만든 seal 들을 BLS 로 aggregate 한 값과, 그 validator 들의 인덱스를 담은 bitmap (`SealerSet`) 의 짝이다 (`A-03 §4.4`). |
| sealer index | seal 대상 block 이 속한 높이의 validator 집합에서, 순서를 매긴 목록 안의 validator 위치다 (`A-04`). `SealerSet` 의 비트 `i` 는 sealer index `i` 를 가리킨다. |
| quorum | 메시지나 seal 을 보내야 하는 서로 다른 validator 의 최소 수다. `quorum_size(n)` 은 `A-04` 에서 정의한다. |
| extra seals | decide 된 block 에 대한 seal 가운데 quorum 이 모인 뒤에 도착한 seal 이다. extra seal 은 다음 block 의 `prev_prepared_seal` / `prev_committed_seal` 에 합쳐진다 (`A-05`, `A-08`). |
| previous-block seals | `WBFTExtra` 의 필드 `prev_round`, `prev_prepared_seal`, `prev_committed_seal` 이다. 이 필드는 이 block 의 proposer 가 갖고 있던 parent block 의 round 와 aggregated seal 을 담으며, extra seal 로 확장될 수 있다. 현재 block 의 seal 은 노드마다 다를 수 있다 (`A-08` WBFT-HDR-053). 그러나 previous-block seal 필드는 한 번 기록되면 block hash 에 포함되므로 모든 노드에서 같다. |
| epoch | validator 집합이 고정되는, 연속한 높이의 구간이다 (`A-04`). |
| epoch block | `WBFTExtra.epoch_info` 가 비어 있지 않은 block 이다. epoch block 은 다음 epoch 의 candidate, validator, BLS 키를 담는다 (`A-04`). epoch block `e` 는 자신이 닫는 epoch 의 마지막 block 이며, 이전 validator 집합이 block `e` 를 seal 한다. block `e` 의 `EpochInfo` 는 block `e + 1` 부터 적용된다. block 0 도 epoch block 이다. |
| diligence | candidate 마다 매기는 점수이며, 단위는 10^-6 이고 범위는 0 부터 `2 · DILIGENCE_DENOMINATOR` 까지다 (`A-04`). |
| randao reveal | `randao_data(chain_id, number)` 에 대해 node key 로 만든 ECDSA signature 이며, `WBFTExtra.randao_reveal` 에 저장된다 (`A-02 §7`). |
| randao mix | `Header.MixDigest` 에 저장되는 값이며, `randao_mix(parent.MixDigest, reveal)` 로 계산한다 (`A-02 §7`). |
| gas tip | 거버넌스가 정한 priority fee 이며 단위는 wei 다. gas tip 은 모든 block 의 `WBFTExtra.gas_tip` 에 저장된다 (`B-06`, `B-07`). |
| justification | PRE-PREPARE 에 첨부된 ROUND-CHANGE payload 와 PREPARE 메시지이며, 그 round 에 그 proposal 을 보내도 된다는 것을 증명한다. ROUND-CHANGE 에 첨부된 PREPARE 도 justification 이다 (`A-05`). |
| legacy message | 코드 `ISTANBUL_MSG = 0x11` 로 받은 메시지다 (`A-03 §8.6`). |
| transition | 어떤 block 부터 합의 파라미터를 바꾸는 `ChainConfig.Transitions` 의 항목이다 (§6.4). |
| reference implementation | commit `740526d03` 의 go-stablenet 이다. |

---

## 3. 기본 타입

### 3.1 고정 크기 타입

[WBFT-TYPE-001] `Address` 는 반드시 정확히 20 바이트여야 한다. secp256k1 public key 의 주소는 `keccak256(uncompressed_pubkey[1:65])[12:32]` 이다 (`A-02 §3`).
- Source: `crypto/crypto.go:287-290` (PubkeyToAddress)
- Observable: header, network

[WBFT-TYPE-002] `Hash` 는 반드시 정확히 32 바이트여야 한다. `Hash` 필드 (`Digest`, `PreparedDigest`, `MixDigest`, `ParentHash`) 를 RLP 로 디코딩할 때, 입력 문자열의 길이가 32 가 아니면 그 필드를 감싼 구조 전체의 디코딩이 반드시 실패해야 한다.
- Source: `rlp/decode.go:369-399` (decodeByteArray)
- Observable: network, header

[WBFT-TYPE-003] `ECDSASignature` 는 65 바이트 `R(32) ‖ S(32) ‖ V(1)` 이다. `A-03` 의 디코딩은 이 값의 길이를 제한하지 않는다. 노드는 서명자를 recover 할 때 길이가 65 가 아닌 값을 반드시 거부해야 한다 (`A-02 §3.3`).
- Source: `crypto/crypto.go:39`, `crypto/secp256k1/secp256.go:163-171`
- Observable: network, header

[WBFT-TYPE-004] `BLSPublicKey` 는 48 바이트 (압축한 G1 점) 이다. `BLSSignature` (`Seal` 이라고도 부른다) 는 96 바이트 (압축한 G2 점) 이다. BLS secret key 는 32 바이트이며 big-endian 이다. `A-03` 의 디코딩은 이 값들의 길이를 제한하지 않는다. 노드는 길이가 틀린 값을 그 값을 쓰는 시점에 반드시 거부해야 한다 (`A-02 §5.3`).
- Source: `crypto/bls/common/constants.go:9-11`
- Observable: header, network

[WBFT-TYPE-005] `SealType` 은 1 바이트다. `PREPARE_SEAL = 0x00` 이고 `COMMIT_SEAL = 0x01` 이다. 노드는 이 두 값 말고 다른 `SealType` 값으로 seal 에 서명하거나 seal 을 verify 해서는 안 된다.
- Source: `consensus/wbft/core/core.go:41-46`
- Observable: header, network

### 3.2 Sequence, block 번호, round

같은 높이와 같은 round 가 자리마다 다른 비트 폭으로 나타난다. 아래 표는 각 자리의 표현을 요약하고, 그 아래의 requirement 가 변환 방법을 정한다.

| 값 | 합의 메시지에서 | header 에서 | 레퍼런스가 쓰는 변환 |
|---|---|---|---|
| 높이 | `Sequence`: 상한 없는 RLP `bigint` | `Header.Number`: RLP `bigint` 이다. | `uint64` 가 필요한 모든 곳에서 `big.Int.Uint64()` 를 쓴다 |
| block 의 round | `Round`: 상한 없는 RLP `bigint` | `WBFTExtra.round`, `WBFTExtra.prev_round`: `uint32` | `uint32(round.Uint64())`, 즉 `round mod 2^32` 를 쓴다 |
| `seal_data` 안의 round | — | — | `uint32(round.Uint64())` |
| timer 가 쓰는 round | — | — | `round.Uint64()` (`A-06`) |

[WBFT-TYPE-010] 합의 메시지의 `Sequence` 는 상한이 없는 음이 아닌 `bigint` 다. 디코더는 이 필드에 대해 정규형 RLP 정수라면 어떤 값이든 반드시 받아들여야 한다 (`A-03 §3.2`). 범위 제한은 디코딩이 적용하지 않고, `A-05` 의 메시지 수락 규칙이 적용한다.
- Source: `consensus/wbft/messages/common.go:30-36`, `consensus/wbft/messages/prepare.go:83-94`
- Observable: network

[WBFT-TYPE-011] (withdrawn; informative) `Header.Number` 는 음이 아닌 `bigint` 다.
- Source: `core/types/block.go:149-152` (SanityCheck), `consensus/wbft/engine/engine.go:250-263` (Uint64), `consensus/wbft/engine/engine.go:270` (GetConfig), `consensus/wbft/engine/engine.go:220`, `consensus/wbft/engine/engine.go:227`, `consensus/wbft/engine/engine.go:277` (fork checks), `consensus/wbft/engine/engine.go:313`, `consensus/wbft/engine/engine.go:570` (randao_data), `consensus/wbft/engine/engine.go:491-494, 695, 759-760` (Uint64), `consensus/wbft/core/core.go:196`, `consensus/wbft/core/request.go:89` (Int64)

[WBFT-TYPE-012] 합의 메시지의 `Round` 는 상한이 없는 음이 아닌 `bigint` 다. 레퍼런스가 round 를 `uint32` 로 저장하거나 서명하는 곳 (header 필드 `round` 와 `prev_round`, 그리고 `seal_data` 의 round 인자) 에서는 반드시 `round mod 2^32` 를 써야 한다. 그 결과 round `r` 과 round `r + k·2^32` 는 같은 header 필드 값과 같은 `seal_data` 를 만든다.
- Source: `consensus/wbft/engine/engine.go:153-158` (writeRoundNumber), `consensus/wbft/core/prepare.go:47`, `consensus/wbft/core/prepare.go:105`, `consensus/wbft/core/commit.go:49`, `consensus/wbft/core/commit.go:108`
- Observable: header, network

Implementation note (informative): `big.Int.Uint64()` 는 크기의 하위 64 비트를 돌려준다. 그래서 음이 아닌 모든 `x` 에 대해 `uint32(x.Uint64())` 는 `x mod 2^32` 와 같다. 실제로 round 가 2^32 에 이르는 일은 없다. `A-06` 의 timer 규칙을 따르면 그 round 에 이르는 데 chain 의 수명보다 훨씬 긴 시간이 걸리기 때문이다.

> 해설: round `r` 과 `r + 2^32` 가 같은 `seal_data` 를 만들므로, 이론상 두 round 의 seal 을 서로 바꿔 써도 verify 를 통과한다. 명세는 이 상황이 실제로 일어나지 않는다고 판단한다. 구현자가 지킬 점은 두 가지다. 첫째, round 를 서명하거나 header 에 넣을 때 반드시 `mod 2^32` 를 적용한다. 둘째, 메시지 디코더가 round 를 `uint32` 로 제한해서는 안 된다. 범위 검사는 `A-05` 의 메시지 수락 규칙이 맡는다.

### 3.3 복합 타입

아래 표는 복합 타입과 그 뜻을 나열한다. 정확한 인코딩은 `A-03` 에 있다.

| 타입 | 정의 | 인코딩 |
|---|---|---|
| `View` | `(sequence: bigint, round: bigint)` 이며, `(sequence, round)` 의 사전식 순서로 정렬한다 | 하나의 단위로 전송하지 않는다. `A-03 §8.7` 을 본다 |
| `Proposal` | header, 트랜잭션, uncle, 선택적인 withdrawal 로 이루어진 block | `A-03 §5` |
| `Digest` | `Hash` = `block_hash(proposal.header)` | `A-03 §6` |
| `SealerSet` | bitmap 이다. sealer index `i` 가 있으면 byte `i div 8` 의 비트 `i mod 8` 이 켜져 있고, 그 역도 성립한다 | `A-03 §4.4` |
| `AggregatedSeal` | `(sealers: SealerSet, signature: bytes)` | `A-03 §4.4` |
| `Candidate` | `(addr: Address, diligence: uint64)` | `A-03 §4.5` |
| `EpochInfo` | `(candidates: [Candidate], validators: [uint32], bls_public_keys: [bytes])` 이다. `validators[i]` 는 `candidates` 의 인덱스이고, `bls_public_keys[i]` 는 `validators[i]` 의 키다 | `A-03 §4.5` |
| `WBFTExtra` | `Header.Extra` 에 저장하는 열 개의 합의 필드 | `A-03 §4` |
| `ValidatorSet` | `(address, bls_public_key)` 를 순서대로 담은 목록이다. 순서는 governing epoch block 의 `EpochInfo.validators` 순서와 같다 | `A-04` |
| `Config` | 한 높이의 합의 파라미터 | §6 |

[WBFT-TYPE-020] `View` 를 비교할 때는 반드시 `sequence` 를 먼저 비교하고, sequence 가 같을 때에만 `round` 를 비교해야 한다.
- Source: `consensus/wbft/types.go:96-104` (View.Cmp)
- Observable: network

[WBFT-TYPE-021] `EpochInfo` 에서 항목 `bls_public_keys[i]` 는 반드시 candidate `candidates[validators[i]]` 의 BLS public key 여야 한다. `EpochInfo` 로 만든 validator 집합은 `i = 0 .. len(validators)-1` 에 대해 `(candidates[validators[i]].addr, bls_public_keys[i])` 를 이 순서대로 나열하며, 정렬하지 않는다. 형식이 맞는 `EpochInfo` 는 `len(bls_public_keys) = len(validators)` 이고, 모든 `validators[i]` 가 `len(candidates)` 보다 작다. 형식이 맞지 않는 `EpochInfo` 에 대해 레퍼런스는 다음처럼 동작한다.
1. `validators[i] ≥ len(candidates)` 인 항목에는 영 주소가 들어간다.
2. `bls_public_keys` 가 `validators` 보다 짧으면 레퍼런스는 비정상 종료한다 (index out of range).
3. `bls_public_keys` 에 남는 항목은 무시된다.
- Source: `core/types/istanbul.go:177-187` (GetValidators), `core/types/istanbul.go:189-193` (GetCandidate), `consensus/wbft/validator/validator.go:35-41` (NewSet), `consensus/wbft/engine/engine.go:890-903`
- Observable: header

> 해설: seal bitmap 의 비트 번호는 이 순서를 가리킨다. 그래서 구현이 validator 집합을 정렬하거나 순서를 바꾸면, 모든 aggregated seal 이 다른 public key 로 verify 되어 실패한다.

[WBFT-TYPE-022] (withdrawn; informative) `Candidate.diligence` 는 단위가 10^-6 인 `uint64` 이며, 의미 있는 범위는 `0 .. 2 · DILIGENCE_DENOMINATOR` 다. 인코딩은 이 범위를 제한하지 않는다. 이 범위는 `A-04` 의 diligence 계산에서 나오며, 규범은 그 계산의 requirement 가 정한다.
- Source: `core/types/istanbul.go:94-97`, `core/types/istanbul.go:41-46`

### 3.4 시간과 양

| 값 | 타입 | 단위 |
|---|---|---|
| `Header.Time` | `uint64` | Unix epoch 이후의 초 |
| `Config.block_period` | `uint64` | 초 |
| `Config.request_timeout` | `uint64` | 밀리초 |
| `Config.max_request_timeout_seconds` | `uint64` | 초 |
| `Config.allowed_future_block_time` | `uint64` | 초 |
| `WBFTExtra.gas_tip` | `bigint` | wei |

---

## 4. 프로토콜 상수

"분류" 칸은 §7 의 분류를 쓴다. **C** 는 합의 필수 값이다. 이 값이 다르면 어떤 block 이 유효한지, 또는 어떤 바이트에 서명하는지가 달라진다. **L** 은 liveness 에 영향을 주는 값이다. 이 값이 다르면 네트워크가 멈추거나 느려질 수 있지만 유효성은 바뀌지 않는다. **N** 은 로컬 값이다. 노드가 다른 값을 골라도 된다.

### 4.1 메시지 코드와 wire 상수

[WBFT-PARAM-001] 합의 메시지 코드는 반드시 `PREPREPARE = 0x12`, `PREPARE = 0x13`, `COMMIT = 0x14`, `ROUND_CHANGE = 0x15` 여야 한다.
- Source: `consensus/wbft/messages/message.go:28-33`
- Observable: network

[WBFT-PARAM-002] 네트워크 계층은 legacy 코드 `ISTANBUL_MSG = 0x11` 을 반드시 받아들여야 하며, 받은 메시지는 `A-03 §8.6` 의 방식으로 처리한다. 합의 handler 는 eth 코드 `NEW_BLOCK_MSG = 0x07` 을 `A-07` 에 적힌 방식으로 들여다본다.
- Source: `consensus/wbft/backend/handler.go:41-44`
- Observable: network

| 이름 | 값 | 분류 | 뜻 | 출처 |
|---|---|---|---|---|
| `PREPREPARE` | `0x12` | C | PRE-PREPARE 코드 | `consensus/wbft/messages/message.go:29` |
| `PREPARE` | `0x13` | C | PREPARE 코드 | `consensus/wbft/messages/message.go:30` |
| `COMMIT` | `0x14` | C | COMMIT 코드 | `consensus/wbft/messages/message.go:31` |
| `ROUND_CHANGE` | `0x15` | C | ROUND-CHANGE 코드 | `consensus/wbft/messages/message.go:32` |
| `ISTANBUL_MSG` | `0x11` | C | legacy 코드 (`A-03 §8.6`) | `consensus/wbft/backend/handler.go:43` |
| `NEW_BLOCK_MSG` | `0x07` | N | 합의 handler 가 들여다보는 eth block 알림 (`A-07`) | `consensus/wbft/backend/handler.go:42` |
| subprotocol 이름 / 버전 / 길이 | `"istanbul"` / `100` / `22` | C | devp2p capability (`A-07`) | `eth/quorum_protocol.go:39`, `eth/quorum_protocol.go:41`, `eth/quorum_protocol.go:54` |

### 4.2 header 와 extra 데이터 상수

[WBFT-PARAM-010] `Number ≥ 1` 인 모든 WBFT header 는 반드시 `Difficulty = WBFT_DIFFICULTY = 1` 이어야 한다. 이 값은 WBFT block hash 규칙을 고르는 데에도 쓰인다 (`A-03 §6`). genesis 의 difficulty 는 genesis 파일의 값을 제한 없이 그대로 복사한 값이다 (`B-02` SNET-GEN-003). testnet preset 은 difficulty 로 0 을 쓰므로 testnet genesis 의 hash 는 Ethereum 규칙으로 계산된다.
- Source: `core/types/istanbul.go:37-39`, `core/types/block.go:121-131`, `consensus/wbft/engine/engine.go:212-215`
- Observable: header

[WBFT-PARAM-011] `SEAL_LENGTH = 96` 이다. 노드는 길이가 96 바이트가 아닌 개별 seal 을 aggregate 해서는 안 된다. 레퍼런스는 그런 seal 이 있으면 aggregate 를 `ErrInvalidSeal` 로 실패시킨다.
- Source: `core/types/istanbul.go:35`, `consensus/wbft/engine/engine.go:130-137`
- Observable: header

[WBFT-PARAM-012] `EXTRA_VANITY = 32` 는 proposer 가 `WBFTExtra.vanity_data` 를 만들 때에만 쓴다 (`A-03 §4.2`). 검증하는 노드는 `vanity_data` 의 길이나 내용을 이유로 header 를 거부해서는 안 된다.
- Source: `core/types/istanbul.go:34`, `consensus/wbft/engine/engine.go:1162-1182`, `consensus/wbft/engine/engine.go:196-346` (no vanity check)
- Observable: header

| 이름 | 값 | 분류 | 뜻 | 출처 |
|---|---|---|---|---|
| `WBFT_DIFFICULTY` | `1` | C | WBFT block 임을 표시하고 block hash 규칙을 고르는 값 | `core/types/istanbul.go:39` |
| `EXTRA_VANITY` | `32` | N | proposer 가 쓰는 vanity 채움 길이 | `core/types/istanbul.go:34` |
| `SEAL_LENGTH` | `96` | C | BLS seal 하나의 길이 | `core/types/istanbul.go:35` |
| `EMPTY_UNCLE_HASH` | `keccak256(0xc0)` = `0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347` | C | `Header.UncleHash` 가 가져야 하는 값 | `consensus/wbft/common/constants.go:29`, `core/types/hashes.go:30` |
| `EMPTY_NONCE` | `0x0000000000000000` | N | 레퍼런스 proposer 가 쓰는 nonce 이며, 검증하지 않는다 | `consensus/wbft/common/constants.go:30`, `consensus/wbft/engine/engine.go:488` |
| `MAX_GAS_LIMIT` | `2^63 − 1` | C | `Header.GasLimit` 의 상한 | `params/protocol_params.go:28` |
| `MAXIMUM_EXTRA_DATA_SIZE` | `32` | N | miner 에 설정하는 extra 데이터의 상한 | `params/protocol_params.go:31`, `eth/backend.go:307-321` |

### 4.3 epoch, diligence, shuffle 상수

| 이름 | 값 | 분류 | 뜻 | 출처 |
|---|---|---|---|---|
| `DILIGENCE_DENOMINATOR` | `1_000_000` | C | diligence 의 단위는 10^-6 이고, diligence 의 최댓값은 `2 · DILIGENCE_DENOMINATOR` 다 | `core/types/istanbul.go:41-43` |
| `DEFAULT_DILIGENCE` | `2 · 1_000_000 · 95 / 100 = 1_900_000` | C | 새 candidate 와 genesis candidate 의 diligence | `core/types/istanbul.go:45-46`, `consensus/wbft/config.go:267-270` |
| `SHUFFLE_ROUND_COUNT` | `33` | C | swap-or-not shuffle 의 round 수 | `consensus/wbft/engine/engine.go:1454` |
| shuffle hash | `keccak256` | C | shuffle 이 쓰는 hash | `consensus/wbft/engine/engine.go:1478` |
| shuffle buffer 배치 | seed 32 바이트 ‖ round 1 바이트 ‖ position window 4 바이트 | C | shuffle hash 의 입력 배치 | `consensus/wbft/engine/engine.go:1449-1453` |

shuffle 과 diligence 알고리즘은 `A-04` 에서 정의한다.

### 4.4 암호 상수

| 이름 | 값 | 분류 | 뜻 | 출처 |
|---|---|---|---|---|
| `ECDSA_SIGNATURE_LENGTH` | `65` | C | `R ‖ S ‖ V` | `crypto/crypto.go:39` |
| `BLS_SECRET_KEY_LENGTH` | `32` | C | big-endian scalar | `crypto/bls/common/constants.go:9` |
| `BLS_PUBLIC_KEY_LENGTH` | `48` | C | 압축한 G1 점 | `crypto/bls/common/constants.go:10` |
| `BLS_SIGNATURE_LENGTH` | `96` | C | 압축한 G2 점 | `crypto/bls/common/constants.go:11` |
| `BLS_DST` | ASCII `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` (43 바이트) | C | hash-to-curve 의 domain separation tag | `crypto/bls/blst/signature.go:22` |
| `RANDAO_VERSION` | `1` (한 바이트 `0x01` 로 인코딩한다) | C | `randao_data` 안의 버전 바이트 | `consensus/wbft/engine/engine.go:566-569` |
| `PREPARE_SEAL`, `COMMIT_SEAL` | `0x00`, `0x01` | C | `SealType` | `consensus/wbft/core/core.go:41-46` |
| BLS 키 생성 salt | ASCII `BLS-SIG-KEYGEN-SALT-` | C | `derive_bls_key` 의 초기 salt | blst `src/keygen.c:145` (module `github.com/supranational/blst v0.3.16`, `go.mod:63`) |

### 4.5 genesis 에서만 쓰는 상수

| 이름 | 값 | 분류 | 뜻 | 출처 |
|---|---|---|---|---|
| `INITIAL_GAS_TIP` | `27_600_000_000_000` wei | C | `anzeon.systemContracts.govValidator.params.gasTip` 이 없을 때 쓰는 genesis gas tip 이다. genesis extra 를 만드는 코드는 값이 비었거나 10진수가 아닐 때에도 이 값을 쓰지만, 그런 값이 있으면 genesis 생성 자체가 실패한다 (`B-02` SNET-GEN-010) | `params/protocol_params.go:138`, `consensus/wbft/config.go:233-243` |

### 4.6 로컬 구현 상수

아래 값은 레퍼런스 구현 동작의 일부이지만, conformance 를 따르는 노드는 다른 값을 골라도 된다. 이 값을 참조하는 장 (`A-05`, `A-07`, `B-09`) 이 정의 하나를 함께 쓰도록 여기에 나열한다.

| 이름 | 값 | 분류 | 뜻 | 출처 |
|---|---|---|---|---|
| `SEQUENCE_THRESHOLD` | `1` | N (liveness 에 영향) | 현재 sequence 보다 앞선 메시지를 몇 sequence 앞까지 보관하는지 정하는 값 (`A-05`) | `consensus/wbft/core/backlog.go:45` |
| `ROUND_THRESHOLD` | `10` | N (liveness 에 영향) | 현재 round 보다 앞선 메시지를 몇 round 앞까지 보관하는지 정하는 값 (`A-05`) | `consensus/wbft/core/backlog.go:46` |
| `MAX_BACKLOG_SIZE_PER_VALIDATOR` | `4 · (10 + 1) · (1 + 1) = 88` | N | 보낸 validator 하나당 backlog 상한 (`A-05`) | `consensus/wbft/core/backlog.go:50` |
| `INMEMORY_PEERS` | `40` | N | peer 별로 본 메시지를 기억하는 cache 가 추적하는 peer 수 (`A-07`) | `consensus/wbft/backend/engine.go:40` |
| `INMEMORY_MESSAGES` | `1024` | N | 본 메시지를 기억하는 cache 하나의 항목 수 (`A-07`) | `consensus/wbft/backend/engine.go:41` |
| `MAX_STATUS_BLOCK_RANGE` | `1024` | N | RPC `istanbul_status` 의 범위 상한 (`B-09`) | `consensus/wbft/backend/api.go:206` |
| `MAX_ISTANBUL_MSG_SIZE` | `10 485 760` (10 MiB) | L | 노드가 읽는 가장 큰 `istanbul/100` frame 크기다. 이보다 큰 frame 을 받으면 노드는 그 peer 와의 연결을 끊는다 (`A-07` §4.3). 이 값을 더 작게 잡은 노드는 큰 PRE-PREPARE 나 ROUND-CHANGE 를 보내는 peer 와의 연결을 끊으므로 합의가 늦어진다 | `eth/handler.go:59` |

---

## 5. 파라미터와 상수의 차이

*상수* (§4) 는 모든 WBFT 네트워크에서 값이 같다. *파라미터* (§6) 는 chain 설정 (genesis) 에서 읽으며, 네트워크마다 다를 수 있고, transition 을 통해 한 네트워크 안에서도 높이마다 다를 수 있다.

---

## 6. 합의 설정

### 6.1 `Config` 레코드

`Config` 는 노드가 갖는 합의 설정이다. `config_at(number)` (§6.5) 는 `Config` 에서 높이 `number` 에 적용되는 설정을 유도한다.

| 필드 (명세 이름) | 레퍼런스 필드 | 타입 | 단위 | 뜻 | 분류 |
|---|---|---|---|---|---|
| `request_timeout` | `RequestTimeout` | `uint64` | ms | round change timeout 의 기본값 (`A-06`) | L |
| `block_period` | `BlockPeriod` | `uint64` | s | parent 와의 `Header.Time` 간격의 최솟값 (`A-08`) | C |
| `proposer_policy` | `ProposerPolicy` | `{ROUND_ROBIN = 0, STICKY = 1}` | — | proposer 순환 규칙 (`A-04`) | C |
| `epoch` | `Epoch` | `uint64` | block 수 | epoch 길이 (`A-04`) | C |
| `allowed_future_block_time` | `AllowedFutureBlockTime` | `uint64` | s | header timestamp 가 로컬 시계보다 앞서도 허용하는 폭 (`A-08`) | L |
| `max_request_timeout_seconds` | `MaxRequestTimeoutSeconds` | `uint64` | s | round `>= 1` 의 round change timeout 상한이다. round 0 은 `request_timeout` 이 이 상한보다 커도 언제나 `request_timeout` 을 쓴다. `0` 은 상한이 없다는 뜻이다 (`A-06` WBFT-TIMER-006) | L |
| `transitions` | `Transitions` | `Transition` 의 목록 | — | 높이에 따라 설정을 덮어쓰는 항목 (§6.4) | C |
| `system_contract_upgrades` | `SystemContractUpgrades` | `Upgrade` 의 목록 | — | 높이별 system contract 주소와 버전 (`B-01`, `B-04`) | C |

- Source: `consensus/wbft/config.go:105-114`

[WBFT-PARAM-020] `proposer_policy` 의 식별자가 `1` 이면 반드시 `STICKY` 로 해석하고, 그 밖의 모든 식별자는 반드시 `ROUND_ROBIN` 으로 해석해야 한다.
- Source: `consensus/wbft/config.go:37-42`, `consensus/wbft/validator/default.go:71-87`
- Observable: header, network

> 해설: 오타로 넣은 `2` 같은 값도 `ROUND_ROBIN` 이 된다. 알 수 없는 값을 거부하는 구현은 같은 설정에서 레퍼런스와 다르게 동작한다. 그래서 알 수 없는 값을 거부하려면 노드 시작 단계에서 거부해야 하고, 실행 중에 다른 policy 를 골라서는 안 된다.

### 6.2 설정의 출처

레퍼런스 노드에는 노드별 합의 설정 파일이 없다. 노드는 시작할 때 genesis `ChainConfig` (`config.anzeon.wbft` 와 `config.transitions`) 로 `Config` 를 한 번 만든다. 아래 표의 JSON 필드 이름은 genesis 파일의 이름이다.

| `Config` 필드 | genesis 출처 | 규칙 |
|---|---|---|
| `request_timeout` | `anzeon.wbft.requestTimeoutSeconds` (`uint64`) | 값이 0 이 아니면 `uint64` 로 `requestTimeoutSeconds × 1000` 을 계산한다 (2^64 로 나눈 나머지로 wrap 된다). 값이 0 이면 `0` 으로 남는다 |
| `block_period` | `anzeon.wbft.blockPeriodSeconds` | 값이 0 이 아니면 복사한다 |
| `epoch` | `anzeon.wbft.epochLength` | 값이 0 이 아니면 복사한다 |
| `allowed_future_block_time` | `anzeon.wbft.allowedFutureBlockTime` | 값이 0 이 아니면 복사한다 |
| `proposer_policy` | `anzeon.wbft.proposerPolicy` (`*uint64`) | 값이 있으면 (`0` 도 포함한다) 설정하고, 없으면 비워 둔다 |
| `max_request_timeout_seconds` | `anzeon.wbft.maxRequestTimeoutSeconds` (`*uint64`) | 값이 있으면 복사하고, 없으면 `0` 으로 둔다 |
| `transitions` | `transitions` (`ChainConfig` 의 최상위 필드) | 복사한 뒤 정렬한다 (§6.4) |
| `system_contract_upgrades` | block 0 의 `anzeon.systemContracts`, 그다음 `ChainConfig.CollectUpgrades()` | `B-01` 을 본다 |

- Source: `eth/ethconfig/config.go:192-200` (CreateConsensusEngine), `eth/ethconfig/config.go:213-273` (SetConfigFromChainConfig), `params/config_wbft.go:186-198`

[WBFT-PARAM-030] genesis 값을 적용하기 전의 기본 설정은 반드시 모든 값이 0 인 `Config` 여야 한다. 즉 모든 숫자 필드가 `0` 이고, `proposer_policy` 가 없고, transition 이 없다. 노드는 레퍼런스의 `DefaultConfig` (`request_timeout = 1000`, `block_period = 1`, `ROUND_ROBIN`, `epoch = 10`) 를 쓰지 않는다.
- Source: `eth/ethconfig/config.go:193`, `consensus/wbft/config.go:116-122` (DefaultConfig, referenced only from tests)
- Observable: header

> 해설: `DefaultConfig` 는 테스트에서만 쓰인다. 그래서 genesis 에 값이 빠지면 "기본값" 이 채워지는 것이 아니라 필드가 0 이나 nil 로 남는다.

[WBFT-PARAM-031] Anzeon chain 설정이 다음 조건 가운데 하나라도 어기면, 노드는 반드시 시작을 거부해야 한다.
1. `init` 이 있다.
2. `init.blsPublicKeys` 가 비어 있지 않다.
3. `init.validators` 가 비어 있지 않다.
4. `len(init.validators) == len(init.blsPublicKeys)` 이다.
5. `systemContracts` 가 있고, 그 안에 `govValidator`, `nativeCoinAdapter`, `govMasterMinter`, `govMinter`, `govCouncil` 이 있다.
6. system contract 버전이 허용되는 버전이다 (`B-04`).
7. `wbft` 가 있다.
8. `wbft.requestTimeoutSeconds > 0` 이다.
9. `wbft.blockPeriodSeconds > 0` 이다.
10. `wbft.epochLength ≥ 2` 이다.

레퍼런스는 `gstable init` 명령과 genesis block 설정 단계에서 이 조건 위반을 오류(`Invalid genesis config: …`)로 거부한다. 다만 노드가 빈 데이터베이스에서 코드로 받은 genesis 로 직접 시작하면, `wbft` 절이 없을 때 검사가 돌기 전에 `SetConfigFromChainConfig` 의 nil 역참조로 비정상 종료한다. 이 검사는 `init.blsPublicKeys` 의 16진 형식을 보지 않는다. 16진 문자열이 아닌 항목이 있으면 레퍼런스는 genesis extra 를 만들 때 `hexutil.MustDecode` 에서 비정상 종료한다.
- Source: `params/config_wbft.go:76-130` (CheckValidity), `core/blockchain.go:286-290`, `core/genesis.go:231-238`, `cmd/gstable/chaincmd.go:224-227` (init), `core/genesis.go:297-300`, `eth/backend.go:159-164`, `core/genesis.go:398-408`, `eth/ethconfig/config.go:214-215`, `params/config_wbft.go:68-74`, `consensus/wbft/config.go:272`

[WBFT-PARAM-032] [WBFT-PARAM-031] 의 유효성 검사는 `proposerPolicy` 를 검사하지 않는다. `anzeon.wbft.proposerPolicy` 가 없으면, 레퍼런스 노드는 기본 정책으로 validator 집합을 만들 때마다 비정상 종료하고 (nil 역참조), validator 가 engine 을 시작할 때(`startWBFT`)는 항상 비정상 종료한다. 기본 정책은 `proposerPolicy` 를 정하는 첫 transition 보다 낮은 높이와, 높이 0 의 집합(예를 들어 validator RPC 가 block 번호 0 을 요청할 때 만든다)에 쓰인다. 그런 transition 이상의 높이는 transition 의 정책을 쓰므로 비정상 종료하지 않는다. 그러므로 WBFT 네트워크의 설정은 반드시 `proposerPolicy` 를 포함해야 한다.
- Source: `eth/ethconfig/config.go:228-230`, `consensus/wbft/validator/default.go:84` (`policy.Id` on a nil policy), `consensus/wbft/engine/engine.go:1106` (height 0, base policy), `consensus/wbft/engine/engine.go:1115` (`config_at(h)`), `consensus/wbft/config.go:175-177`, `consensus/wbft/backend/backend.go:357`, `miner/worker.go:445-448`

[WBFT-PARAM-034] 노드는 `anzeon.wbft.proposerPolicy` 가 없는 설정([WBFT-PARAM-032])으로 validator 로서 WBFT engine 을 시작해서는 안 된다. 레퍼런스는 engine 을 시작할 때 비정상 종료하므로, 그런 설정을 시작 단계에서 거부하는 노드는 관측상 레퍼런스와 같다. validator 가 아닌 레퍼런스 노드는 기본 정책으로 validator 집합을 처음 만들 때 비로소 비정상 종료한다.
- Source: `consensus/wbft/backend/backend.go:357` (`ProposerPolicy.Use` on a nil policy), `consensus/wbft/config.go:101-103`, `consensus/wbft/backend/engine.go:266`, `eth/ethconfig/config.go:228-230`

[WBFT-PARAM-033] 노드는 `allowed_future_block_time` 을 반드시 `anzeon.wbft` 에서만 가져와야 한다. transition 은 이 값을 바꾸지 않는다.
- Source: `eth/ethconfig/config.go:224-226`, `consensus/wbft/config.go:161-184` (no case for it)
- Observable: header

Implementation note (informative): 합의 engine 의 `Config.String()` 은 `"wbft"` 를 돌려준다. 이 commit 에서 `Config` 의 TOML tag 는 어떤 노드 설정 파일에도 연결되어 있지 않다. `wbft.Config` 가 `ethconfig.Config` 의 어느 필드에도 나오지 않기 때문이다. simulation hook (`SimApplier`, `consensus/wbft/backend/backend.go:56-58`, `consensus/wbft/backend/engine.go:156-158`) 은 테스트에서만 설정을 바꿀 수 있다.

### 6.3 네트워크 preset

레퍼런스는 StableNet preset 두 개를 정의한다. 두 preset 에는 `transitions` 가 없다.

| 항목 | Mainnet preset | Testnet preset |
|---|---|---|
| `chainId` | `8282` | `8283` |
| `anzeon.wbft.epochLength` | `10` | `140` |
| `anzeon.wbft.blockPeriodSeconds` | `1` | `1` |
| `anzeon.wbft.requestTimeoutSeconds` | `2` (그 결과 `request_timeout = 2000` ms 가 된다) | `2` (그 결과 `2000` ms 가 된다) |
| `anzeon.wbft.proposerPolicy` | `0` (`ROUND_ROBIN`) | `0` (`ROUND_ROBIN`) |
| `anzeon.wbft.maxRequestTimeoutSeconds` | 없다 (그 결과 `0` 이 되어 상한이 없다) | 없다 (그 결과 `0` 이 되어 상한이 없다) |
| `anzeon.wbft.allowedFutureBlockTime` | 없다 (그 결과 `0` 이 된다) | 없다 (그 결과 `0` 이 된다) |
| `anzeon.init.validators` | 1 (`0xaa5faa65e9cc0f74a85b6fdfb5f6991f5c094697`) | 7 |
| genesis gas tip (`govValidator.params.gasTip`) | `27600000000000` | `27600000000000` |
| `BohoBlock` (system contract upgrade, `B-01`) | `0` | `14408500` |
| genesis hash 상수 | `0xf192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b` | `0x2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490` |

- Source: `params/config.go:31-32`, `params/config.go:44-152` (StableNetMainnetChainConfig), `params/config.go:155-272` (StableNetTestnetChainConfig)

두 preset 모두 규범이다. 소스는 아직 mainnet preset 에 테스트 값이라는 주석을 달고 있다 (`// TODO: this is just for test on mainnet`, `params/config.go:67`, 그리고 `// TODO: define initial validators`, `params/config.go:74`). 이 주석은 conformance 를 따르는 노드가 쓰는 값을 바꾸지 않는다 (`B-01` SNET-CFG-027). 표는 genesis 값만 보여 준다.

참고: 개발용 preset `AllDevChainProtocolChanges` (`chainId 1337`, `params/config.go:407-431`) 와 테스트용 preset `TestWBFTChainConfig` (`params/config.go:602-640`) 는 `requestTimeoutSeconds = 1000` 과 `maxRequestTimeoutSeconds = 4` 를 설정한다. 그 결과 기본 timeout 이 1000 초가 된다.

### 6.4 Transitions

`Transition` 은 `{ block: bigint, <anzeon.wbft 의 필드들> }` 이다. genesis 파일에서는 WBFT 필드를 `block` 옆에 나란히 쓴다. 예를 들면 `{"block": 1000, "epochLength": 50}` 이다.

Implementation note (informative): `SetConfigFromChainConfig` 는 hard fork transition block 과 같은 block 을 가진 transition 을 거부하려고 `*big.Int` 포인터를 key 로 쓰는 map `hfTransitionBlocks` 를 유지한다. 이 commit 에서는 그 map 이 늘 비어 있으므로 이 검사는 한 번도 동작하지 않는다 (`eth/ethconfig/config.go:235-252`).

### 6.5 `config_at(number)`

```python
def config_at(base: Config, number: bigint) -> Config:
    """Configuration governing height `number`. Pure. Mirrors Config.GetConfig."""
    cfg = copy(base)                      # transitions/system_contract_upgrades 는 공유하며 바꾸지 않는다
    for t in base.transitions:            # 이미 block 오름차순으로 정렬되어 있다
        if t.block > number:
            break
        if t.request_timeout_seconds != 0:
            cfg.request_timeout = (t.request_timeout_seconds * 1000) % 2**64
        if t.block_period_seconds != 0:
            cfg.block_period = t.block_period_seconds
        if t.epoch_length != 0:
            cfg.epoch = t.epoch_length
        if t.proposer_policy is not None:           # 포인터 필드: 0 도 명시적인 값이다
            cfg.proposer_policy = policy_from_id(t.proposer_policy)   # WBFT-PARAM-020
        if t.max_request_timeout_seconds is not None:  # 포인터 필드: 0 은 상한을 없앤다
            cfg.max_request_timeout_seconds = t.max_request_timeout_seconds
        # t.allowed_future_block_time 은 무시한다 (WBFT-PARAM-033)
    return cfg
```

[WBFT-PARAM-050] `config_at(number)` 은 반드시 기본 설정에서 출발해서, `block ≤ number` 인 모든 transition 을 적용하고, `block > number` 인 첫 transition 에서 멈춰야 한다.
- Source: `consensus/wbft/config.go:161-192` (GetConfig, getTransitionValue)
- Observable: header

[WBFT-PARAM-051] transition 안의 `requestTimeoutSeconds`, `blockPeriodSeconds`, `epochLength` 가 `0` 이면 현재 값을 덮어써서는 안 된다. transition 에 `proposerPolicy` 나 `maxRequestTimeoutSeconds` 가 있으면 그 값이 `0` 이어도 반드시 현재 값을 덮어써야 한다.
- Source: `consensus/wbft/config.go:165-180`
- Observable: header

> 해설: 핵심은 필드마다 "비어 있음" 의 뜻이 다르다는 점이다. `requestTimeoutSeconds`, `blockPeriodSeconds`, `epochLength` 는 `0` 이든 없든 현재 값을 덮어쓰지 않는다. `proposerPolicy` 와 `maxRequestTimeoutSeconds` 는 포인터 필드이므로, 값이 없으면 덮어쓰지 않지만 `0` 이 있으면 `0` 으로 덮어쓴다. 그래서 `proposerPolicy: 0` 은 policy 를 `ROUND_ROBIN` 으로 바꾸고, `maxRequestTimeoutSeconds: 0` 은 상한을 없앤다. `allowedFutureBlockTime` 은 transition 이 아예 반영하지 않는다 (WBFT-PARAM-033).

[WBFT-PARAM-052] transition 의 `requestTimeoutSeconds` 는 반드시 `uint64` 산술로 1000 을 곱해서 `request_timeout` 으로 변환해야 한다.
- Source: `consensus/wbft/config.go:165-168`

[WBFT-PARAM-053] `config_at` 에 넘기는 높이 인자는 용도마다 정해져 있으며, 구현은 반드시 레퍼런스와 같은 인자를 써야 한다.

| 용도 | 인자 | 출처 |
|---|---|---|
| header 검증과 새 proposal 의 timestamp 에 쓰는 `block_period` | 검증하거나 만드는 header 자신의 번호 (그래서 block `h-1` 과 block `h` 사이의 간격은 `config_at(h).block_period` 다) | `consensus/wbft/engine/engine.go:270`, `consensus/wbft/engine/engine.go:502` |
| 다음 block 일정을 잡을 때 쓰는 `block_period` | `latest_number + 1` | `consensus/wbft/backend/engine.go:147-149`, `consensus/wbft/engine/engine.go:482-484` |
| `request_timeout`, `max_request_timeout_seconds` | 현재 sequence | `consensus/wbft/core/core.go:364-367`, `consensus/wbft/core/core.go:439-440` |
| 높이 `h` 의 validator 집합에 쓰는 `proposer_policy` | `h` (`GetValidators`). 높이 0 에서는 기본 policy 를 쓴다 | `consensus/wbft/engine/engine.go:1097-1117` |
| epoch block 의 diligence 계산에 쓰는 `proposer_policy` | epoch block 의 번호 | `consensus/wbft/engine/engine.go:775` |

- Source: `consensus/wbft/engine/engine.go:270`, `consensus/wbft/engine/engine.go:502`, `consensus/wbft/engine/engine.go:482-484`, `consensus/wbft/core/core.go:364-367`, `consensus/wbft/core/core.go:439-440`, `consensus/wbft/engine/engine.go:1097-1117`, `consensus/wbft/engine/engine.go:775`
- Observable: header

[WBFT-PARAM-054] 노드는 epoch 경계를 `config_at(number).epoch` 만으로 유도해서는 안 된다. `A-04` 의 epoch 산술(WBFT-EPOCH-001, WBFT-EPOCH-003)은 `epochLength` 를 설정하는 모든 transition 의 block 에서 계산을 다시 시작한다. `config_at` 은 epoch 의 길이만 알려 준다.
- Source: `consensus/wbft/engine/engine.go:1073-1095` (IsEpochBlockNumber)
- Observable: header

> 해설: 그러므로 epoch 길이만 보고 `number % epoch == 0` 으로 epoch block 을 판정하면 틀린다.

### 6.6 계산 예

genesis 값은 `requestTimeoutSeconds = 2`, `blockPeriodSeconds = 1`, `epochLength = 10`, `proposerPolicy = 0` 이고, `maxRequestTimeoutSeconds` 는 없다. transition 은 genesis 파일에 정렬되지 않은 채로 다음과 같이 적혀 있다.

```
[ {"block": 200, "blockPeriodSeconds": 2, "maxRequestTimeoutSeconds": 30},
  {"block": 100, "epochLength": 20, "requestTimeoutSeconds": 0, "proposerPolicy": 1},
  {"block": 300, "maxRequestTimeoutSeconds": 0} ]
```

정렬하면 순서는 100, 200, 300 이 된다.

| `number` | `request_timeout` | `block_period` | `epoch` | `proposer_policy` | `max_request_timeout_seconds` |
|---|---|---|---|---|---|
| 0 .. 99 | 2000 | 1 | 10 | ROUND_ROBIN | 0 |
| 100 .. 199 | 2000 (`0` 은 덮어쓰지 않는다) | 1 | 20 | STICKY | 0 |
| 200 .. 299 | 2000 | 2 | 20 | STICKY | 30 |
| ≥ 300 | 2000 | 2 | 20 | STICKY | 0 (명시한 `0` 이 덮어쓴다) |

(이 표는 [WBFT-PARAM-050] – [WBFT-PARAM-052] 를 따라 손으로 계산했으며, 기계로 생성한 값이 아니다.)

---

## 7. 파라미터의 분류

| 분류 | 정의 | 이 분류에 속하는 항목 |
|---|---|---|
| 합의 필수 (C) | 값이 다른 두 노드는 block 유효성, 서명하는 바이트, 또는 chain 에 기록되는 값에서 서로 어긋난다. | 메시지 코드; sub-protocol 이름, 버전, 길이; `WBFT_DIFFICULTY`; `SEAL_LENGTH`; `DILIGENCE_DENOMINATOR`; `DEFAULT_DILIGENCE`; `SHUFFLE_ROUND_COUNT`, shuffle hash, shuffle 버퍼 배치; `ECDSA_SIGNATURE_LENGTH`, `BLS_SECRET_KEY_LENGTH`, `BLS_PUBLIC_KEY_LENGTH`, `BLS_SIGNATURE_LENGTH`; BLS DST; 키 생성 salt; `RANDAO_VERSION`; `SealType` 값; `EMPTY_UNCLE_HASH`; `MAX_GAS_LIMIT`; `INITIAL_GAS_TIP` (genesis); `Config.block_period`, `Config.epoch`, `Config.proposer_policy` (이 값은 epoch block 의 diligence 계산에 들어간다, `A-04`); `Config.transitions`; `Config.system_contract_upgrades`; genesis validator, BLS 키, gas tip; chain id (`randao_data` 안에 들어간다, `A-02 §7`) |
| liveness 영향 (L) | 값이 달라도 유효성은 바뀌지 않지만, 합의를 막거나 늦출 수 있다. | `Config.request_timeout`; `Config.max_request_timeout_seconds`; `Config.allowed_future_block_time` (로컬 시계에 따라 달라지는 header 유효성 입력이다. 값이 다른 노드들은 미래 시각이 적힌 proposal 을 서로 다른 시점에 받아들인다); `MAX_ISTANBUL_MSG_SIZE` |
| 로컬 (N) | 노드가 자신의 값을 골라도 되며, 적힌 한계 안에서는 상호 운용에 영향이 없다. | `EXTRA_VANITY` 와 miner 의 extra 데이터; `EMPTY_NONCE`; `NEW_BLOCK_MSG`; `SEQUENCE_THRESHOLD`, `ROUND_THRESHOLD`, `MAX_BACKLOG_SIZE_PER_VALIDATOR` (레퍼런스 값보다 훨씬 작게 잡으면 liveness 에 영향을 준다); `INMEMORY_PEERS`, `INMEMORY_MESSAGES`; `MAX_STATUS_BLOCK_RANGE`; `MAXIMUM_EXTRA_DATA_SIZE` |

[WBFT-PARAM-060] 한 네트워크의 모든 노드는 §7 의 합의 필수 항목에 대해 반드시 같은 값을 써야 하며, liveness 영향 항목에 대해서도 같은 값을 쓰는 것이 좋다 (SHOULD).
- Source: `consensus/wbft/engine/engine.go:270` (block_period in validity), `consensus/wbft/engine/engine.go:1073-1095` (epoch), `consensus/wbft/engine/engine.go:775` (proposer_policy in epoch computation), `consensus/wbft/core/core.go:364-367` (timeouts)
- Observable: header

> 해설: 분류에서 눈여겨볼 항목이 두 개 있다. `proposer_policy` 는 proposer 순서만 정하는 것처럼 보이지만, epoch block 의 diligence 계산에 들어가므로 C 로 분류된다. `MAX_ISTANBUL_MSG_SIZE` 는 수신 한도일 뿐이지만 L 로 분류된다. 이 값을 작게 잡은 노드는 큰 PRE-PREPARE 나 ROUND-CHANGE 를 보내는 peer 와의 연결을 끊어 합의를 늦추기 때문이다 (§4.6).
