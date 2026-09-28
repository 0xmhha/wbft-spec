# A-13. 부록: 오류 카탈로그, 로그 카탈로그, 계보

- Status: draft
- Reference implementation: go-stablenet `740526d03`
- 1절은 오류 값을 나열하며, 그 오류 값을 인용하는 장을 통해서만 부분적으로 규범이 된다. 오류 값은 wire 로 나가지 않으므로, conformance 는 오류의 *정체* 에 달려 있지 않고 결과에만 달려 있다. 여기서 결과는 노드가 메시지를 버리는 것, block 을 거부하는 것, RPC 오류를 돌려주는 것이다. 2절과 3절은 informative 다.

> 해설: 이 말은 `A-08` WBFT-HDR-080 의 "처음 실패한 단계의 오류를 돌려준다" 와 충돌하지 않는다. `A-08` 이 요구하는 것은 오류의 **분류** 다. 즉 호출자가 다음 행동을 고르는 기준인 `ErrFutureBlock`, `ErrUnknownAncestor`, 그 밖의 오류라는 구분이 참조 구현과 같아야 한다는 뜻이다. 오류 문구나 Go 변수 이름을 같게 하라는 뜻은 아니다. Rust 구현은 다른 이름을 써도 되지만, 이 세 분류와 그에 따른 bad block 기록과 future queue 동작은 맞춰야 한다.

---

## 1. 오류 카탈로그

### 1.1 표를 읽는 법

- 경로는 다른 최상위 디렉터리로 시작하지 않는 한 `consensus/wbft/` 에 대한 상대 경로다.
- "반환 위치" 열은 `consensus/wbft/**` 안에서 그 값을 반환하거나 감싸는 테스트가 아닌 모든 지점을, 그 지점을 둘러싼 함수와 함께 나열한다. 이 목록은 테스트가 아닌 모든 파일의 Go 구문 트리를 훑어서 만들었다. 구문 트리에서는 각 package 수준 오류 변수의 식별자가 쓰인 곳과 모든 `errors.New` / `fmt.Errorf` 호출을 찾았고, 그다음 목록을 손으로 확인했다.
- "Observable" 열은 다른 쪽이 결과를 어떻게 볼 수 있는지 말한다:
  - **header**: 결함이 있는 block 이나 header 가 거부된다. peer 는 노드가 그 block 을 거부하는 것을 본다. 참조 구현은 이때 import 오류 문구를 로그로 남긴다.
  - **network**: 합의 메시지가 버려지고 relay 되지 않는다. sender 에게는 답이 가지 않는다.
  - **rpc**: RPC method (`istanbul_*` API, `B-09`) 가 오류 문구를 돌려준다.
  - **log**: 오류가 노드의 로그에서만 보인다.
  - **local**: 오류가 로컬 호출자(start/stop, engine-specific 호출)에게 돌아간다. 바깥에 주는 효과는 없다.
  - **unused**: 오류 값이 정의되어 있지만 이 commit 에서는 반환되지 않는다.

### 1.2 Package `wbft` (`errors.go`, `utils.go`)

| 오류 값 | 메시지 | 반환 위치 | Observable |
|---|---|---|---|
| `ErrUnauthorizedAddress` | `unauthorized address` | `utils.go:72` (CheckValidatorSignature): 복원한 signer 가 validator 집합에 없다. `backend/backend.go:161` (Broadcast): 로컬 노드가 validator 가 아니다 | network, log |
| `ErrStoppedEngine` | `stopped engine` | `backend/engine.go:292` (Stop), `backend/handler.go:75` (HandleMsg: 코어가 멈춘 동안 합의 메시지가 왔다), `backend/handler.go:142` (NewChainHead) | network (A-07: peer 처리), local |
| `ErrStartedEngine` | `started engine` | `backend/engine.go:250` (Start) | local |
| `ErrGasTipContractUnavailable` | `gas tip contract unavailable` | `engine/engine.go:642` (getGasTip). `engine/engine.go:332` (verifyCascadingFields) 에서 치명적 오류로 전파된다. `GetGasTip` 이 `nil` 을 돌려주지 않으므로 도달할 수 없다 (`B-06` SNET-FIN-017, `A-08` WBFT-HDR-111) | unused (unreachable) |
| `GasTipMismatchError{Have, Want}` | `invalid gas tip: have %d, want %d` | `engine/engine.go:1292` (verifyGasTip). `engine/engine.go:335-338` 에서 치명적 오류로 다룬다 | header |

### 1.3 Package `wbftcommon` (`common/errors.go`)

| 오류 값 | 메시지 | 반환 위치 | Observable |
|---|---|---|---|
| `ErrInvalidProposal` | `invalid proposal` | `backend/backend.go:218` (Commit), `backend/backend.go:263` (Verify): proposal 이 block 이 아니다 | network |
| `ErrInvalidSignature` | `invalid signature` | `backend/backend.go:299` (CheckSignature): 복원한 주소가 기대한 주소와 다르다 (randao reveal, A-02 §7.2) | header |
| `ErrUnknownBlock` | `unknown block` | `engine/engine.go:198` (verifyHeader: number 가 nil 이다), `engine/engine.go:356` (verifySigner: genesis), `engine/engine.go:466` 과 `backend/engine.go:126` (VerifySeal: genesis), `backend/api.go:96`, `backend/api.go:106`, `backend/api.go:142`, `backend/api.go:155` (RPC: block 을 찾지 못했다) | header, rpc |
| `ErrUnauthorized` | `unauthorized` | `engine/engine.go:367` (verifySigner: coinbase 가 validator 가 아니다) | header |
| `ErrInvalidDifficulty` | `invalid difficulty` | `engine/engine.go:214` (verifyHeader), `engine/engine.go:471` (VerifySeal) | header |
| `ErrInvalidExtraDataFormat` | `invalid extra data format` | `engine/engine.go:303` (verifyCascadingFields: extra 가 디코드되지 않는다) | header |
| `ErrInvalidUncleHash` | `non empty uncle hash` | `engine/engine.go:169` (VerifyBlockProposal), `engine/engine.go:209` (verifyHeader), `engine/engine.go:455` (VerifyUncles) | header, network |
| `ErrBlacklistedHash` | `blacklisted hash` | `backend/backend.go:269` (Verify: proposal 이 알려진 bad block 이다) | network |
| `ErrInvalidTimestamp` | `invalid timestamp` | `engine/engine.go:271` (verifyCascadingFields: `parent.Time + block_period > Time`) | header |
| `ErrInvalidPreparedSeals` | `invalid prepared seals` | `engine/engine.go:104` (writePreparedSeals: 쓸 seal 이 없다), `engine/engine.go:432` (verifySeals) | header, local |
| `ErrInvalidPrevPreparedSeals` | `invalid prev prepared seals` | `engine/engine.go:397` (verifyPrevSeals) | header |
| `ErrEmptyPreparedSeals` | `zero prepared seals` | `engine/engine.go:425` (verifySeals), `engine/engine.go:525` (Prepare: `number − 1` 의 canonical header 의 prepared seal 이 nil 이다), `backend/engine.go:357` (InheritExtra) | header, local |
| `ErrEmptyPrevPreparedSeals` | `zero prev prepared seals` | `engine/engine.go:392` (verifyPrevSeals) | header |
| `ErrInvalidCommittedSeals` | `invalid committed seals` | `engine/engine.go:119` (writeCommittedSeals), `engine/engine.go:445` (verifySeals) | header, local |
| `ErrInvalidPrevCommittedSeals` | `invalid prev committed seals` | `engine/engine.go:407` (verifyPrevSeals) | header |
| `ErrEmptyCommittedSeals` | `zero committed seals` | `engine/engine.go:438` (verifySeals), `engine/engine.go:529` (Prepare: `number − 1` 의 canonical header 의 committed seal 이 nil 이다), `backend/engine.go:360` (InheritExtra) | header, local |
| `ErrEmptyPrevCommittedSeals` | `zero prev committed seals` | `engine/engine.go:403` (verifyPrevSeals) | header |
| `ErrInvalidSeal` | `invalid seal` | `engine/engine.go:135` (aggregateSeal: seal 이 96 바이트가 아니다), `engine/engine.go:1365` (verifyAggregatedSeal: BLS 검증 결과가 false) | header (위의 seal 오류 넷이 감싸지 않고 바꿔 치운다. 원래 문구는 로그 L140 – L143 에만 나온다), local (`aggregateSeal`) |
| `ErrEmptySeals` | `zero seals` | `engine/engine.go:1324` (getSignerAddress: seal 이 없다) | rpc, header (epoch 계산, A-04) |
| `ErrMismatchTxhashes` | `mismatch transactions hashes` | `engine/engine.go:164` (VerifyBlockProposal) | network |
| `ErrInvalidMessage` | `invalid message` | `messages/decode.go:58` (Decode: 알 수 없는 code). 유일한 호출자 `core/handler.go:197` 앞에서 `core/handler.go:191-193` 이 code 를 먼저 검사하므로 이 지점에는 도달하지 않는다 | unused (도달 불가) |
| `ErrFailedDecodePreprepare` | `failed to decode PRE-PREPARE message` | `messages/decode.go:32` (Decode, code 0x12). `messages/roundchange.go:289` (RoundChange.DecodeRLP: prepared block hash 가 prepared digest 와 다르다. 호출자가 `ErrFailedDecodeRoundChange` 로 가린다) | network, log |
| `ErrFailedDecodeCommit` | `failed to decode COMMIT message` | `messages/decode.go:39` (code 0x13, PREPARE, sic), `messages/decode.go:46` (code 0x14) | network, log |
| `ErrFailedDecodeRoundChange` | `failed to decode ROUND-CHANGE message` | `messages/decode.go:53` (code 0x15) | network, log |
| `ErrInvalidSpecificCall` | `invalid method name for engine specific function` | `backend/engine.go:307`, `:311`, `:315`, `:319`, `:323`, `:331`, `:338`, `:342`, `:346`, `:381`, `:385`, `:389`, `:405`, `:411` (CallEngineSpecific: method 나 인자가 틀렸다) | local |
| `ErrIsNotWBFTBlock` | `block is not a wbft block` | `backend/api.go:420` (Anzeon 이 아닌 chain 에서의 GetWbftExtraInfo) | rpc |
| `ErrEpochInfoIsNotNil` | `epoch info should be nil for non-epoch block` | `engine/engine.go:959` (processFinalize) | header |
| `ErrStateUnavailable` | `state unavailable for verification` | `engine/engine.go:374` (verifySigner: parent state 가 없다). `engine/engine.go:292` 에서 허용된다 | log (Trace) |
| `ErrBlacklistedSigner` | `blacklisted signer` | `engine/engine.go:377` (verifySigner) | header |
| `ErrInvalidMixDigest`, `ErrInvalidNonce`, `ErrInvalidVotingChain`, `ErrInvalidVote`, `ErrInconsistentSubject`, `ErrNotFromProposer`, `ErrIgnored`, `ErrOldMessage`, `ErrInvalidSigner`, `ErrInvalidGenesis`, `ErrFailedDecodePrepare` | (`common/errors.go:47-139` 참조) | 반환되지 않는다 | unused |

참고: randao mix 불일치는 `ErrInvalidMixDigest` 가 아니라 그 자리에서 만든 오류(`invalid randao mix: have %x, want %x`, `engine/engine.go:318`)로 보고된다.

### 1.4 Package `core` (`core/errors.go`)

이 값들은 노드 밖으로 나가지 않는다. 이 값 가운데 하나를 낸 메시지는 버려지고 relay 되지 않는다. 예외는 둘이다. `errFutureMessage` 는 메시지를 backlog 에 넣으며, 나중에 backlog 처리가 성공하면 그 메시지를 relay 한다. `errExtraSealMessage` 는 메시지를 extra seal 저장소로 보낸다. 그 저장소에 저장되거나 그 저장소가 조용히 무시한 메시지는 오류 없이 처리된 것으로 치므로 **relay 된다** (`A-05` WBFT-SM-011, `A-07` WBFT-NET-042). extra seal 저장소에서 digest 불일치, seal 검증 실패, `errInvalidExtraSealMessage` 가 났을 때에만 그 relay 가 막힌다.

> 해설: 그래서 inspector 가 "relay 되었다" 를 "유효했다" 로 해석하면 틀린다.

| 오류 값 | 메시지 | 반환 위치 | Observable |
|---|---|---|---|
| `errNotFromProposer` | `message does not come from proposer` | `core/preprepare.go:125` (handlePreprepareMsg) | network, log |
| `errFutureMessage` | `future message` | `core/backlog.go:142`, `:152`, `:171`, `:180` (checkMessage). `core/request.go:75` (checkRequestMsg). `core/request.go:106` (processPendingRequests), `core/backlog.go:288`, `core/handler.go:124`, `core/handler.go:215` 에서 검사한다 | network (처리가 지연된다) |
| `errOldMessage` | `old message` | `core/backlog.go:144`, `:163` (checkMessage). `core/request.go:73` | network |
| `errInvalidMessage` | `invalid message` | `core/backlog.go:127`, `:178`, `:189`, `:199` (checkMessage). `core/commit.go:98`, `:105`, `:111`. `core/prepare.go:95`, `:102`, `:108`. `core/extraseal.go:56`, `:72`. `core/handler.go:244` (deliverMessage). `core/request.go:65`. `core/request.go:39` 에서는 검사하고, handleRequest 가 받은 값을 그대로 돌려준다 | network, log |
| `errInvalidSeal` | `invalid seal` | `core/core.go:481` (verifySeal: seal 이 디코드되지 않는다) | network |
| `errInvalidSigner` | `message not signed by the sender` | `core/core.go:485` (verifySeal: BLS 검증 결과가 false). `core/handler.go:281` (verifySignatures: recover 실패나 membership 실패) | network, log |
| `errInvalidPreparedBlock` | `invalid prepared block in round change messages` | `core/preprepare.go:131` (sequence 가 proposal 번호와 다르다), `core/preprepare.go:143` (justification 이 실패한다) | network, log |
| `errExtraSealMessage` | `extra seal message` | `core/backlog.go:161`, `:187`, `:196` (checkMessage). `core/backlog.go:287`, `core/handler.go:218` 에서 검사한다 | network |
| `errInvalidExtraSealMessage` | `invalid extra seal message` | `core/extraseal.go:84` (addToExtraSeal: code 가 틀렸다) | network (그 메시지는 relay 되지 않는다. 로그는 남지 않는다) |
| `errCurrentIsNil` | `current is nil` | `core/request.go:69` (checkRequestMsg) | local |
| `errFutureViewTooFar` | `future view too far ahead: sequence or round difference too large` | `core/backlog.go:133` (checkMessage) | network |
| `justificationError` (type) | `round-change message view does not match target view` / `prepare message round mismatch` / `prepare message digest mismatch` | `core/justification.go:61`, `:77`, `:81` (isJustified) | network, log |

### 1.5 Package `backend` (`backend/handler.go`)

| 오류 값 | 메시지 | 반환 위치 | Observable |
|---|---|---|---|
| `errDecodeFailed` | `fail to decode wbft message` | `backend/handler.go:58` (decode: `0x11` payload 가 RLP string 이 아니다), `backend/handler.go:80` (HandleMsg) | network (A-07: protocol handler 에게 돌려준다) |
| `errPayloadReadFailed` | `unable to read payload from message` | `backend/handler.go:63` (decode). `HandleMsg` 가 이 값을 `errDecodeFailed` 로 바꾸므로 (`backend/handler.go:80`) 이 값 자체는 protocol handler 에 닿지 않는다 | network (`errDecodeFailed` 로, A-07) |

### 1.6 그 자리에서 만드는 오류

| 위치 | 함수 | 메시지 | Observable |
|---|---|---|---|
| `backend/api.go:211`, `:215`, `:235`, `:245`, `:248`, `:254` | calculateBlockRange | `istanbul_status` 의 block 범위 오류 (`pass the end block number`, `pass the start block number`, `unsupported block number: %d`, `start block number should be less than end block number`, `end block number should be less than or equal to current block height`, `requested range too large: %d blocks (max %d)`) | rpc |
| `backend/api.go:264`, `:269`, `:276` | analyzeBlock | `block %d not found`, `block %d: failed to extract WBFT extra: %w`, `block %d: failed to get validators: %w` | rpc |
| `backend/api.go:425` | GetWbftExtraInfo | `block %d not found` | rpc |
| `core/handler.go:193` | handleEncodedMsg | `invalid message event code %v` | log |
| `core/justification.go:54`, `:68`, `:107`, `:128`, `:139`, `:144`, `:147` | isJustified 와 보조 함수 | `number of roundchange messages is less than required quorum of messages`; `number of prepared messages is less than required quorum of messages`; `quorum of roundchange messages with nil prepared round not found`; `quorum of roundchange messages with prepared round and proposal not found`; `number of prepare messages is less than quorum of messages`; `prepared message digest does not match roundchange prepared digest`; `round number in prepared message does not match prepared round in roundchange` | network, log |
| `core/roundchange.go:125`, `:134`, `:184` | handleRoundChangeMsg | `prepared block number %v does not match current sequence %v`; `prepared block hash %s does not match prepared digest %s in ROUND-CHANGE message`; `no proposal as pending request is nil` | network, log |
| `engine/engine.go:182` | VerifyBlockProposal | `unknown parent hash` | network |
| `engine/engine.go:218`, `:221`, `:225`, `:228`, `:233`, `:235`, `:237` | verifyHeader | `invalid gasLimit: have %v, max %v`; `wbft does not support shanghai fork`; `invalid withdrawalsHash: have %x, expected nil`; `wbft does not support cancun fork`; `invalid excessBlobGas …`; `invalid blobGasUsed …`; `invalid parentBeaconRoot …` | header |
| `engine/engine.go:275`, `:280` | verifyCascadingFields | `invalid gasUsed: have %d, gasLimit %d`; `invalid baseFee before fork: have %d, want <nil>` | header |
| `engine/engine.go:314`, `:318` | verifyCascadingFields | `failed to verify randao reveal signature: %w`; `invalid randao mix: have %x, want %x` | header |
| `engine/engine.go:374`, `:377` | verifySigner | `ErrStateUnavailable` / `ErrBlacklistedSigner` 를 감싼다 | header, log |
| `engine/engine.go:545`, `:555` | Prepare, WriteRandao | `failed to write wbft extra: %w`; `failed to sign randao reveal: %w` | local |
| `engine/engine.go:624`, `:632` | getGasTip | `WBFT: GovValidator contract is not enabled`; `WBFT: parent state root is empty` | header, local |
| `engine/engine.go:783`, `:839`, `:865` | buildEpochInfo | `failed to find valid proposer`; `seal count exceed the range for non validator in prior epoch`; `WBFT: Invalid Diligence %d exceeds maximum` | header |
| `engine/engine.go:977`, `:995`, `:1000`, `:1031`, `:1038`, `:1043` | distributeBaseFee | `WBFT: baseFee is nil …`; `WBFT: validator candidate index out of range …`; `WBFT: nil candidate at index …`; `WBFT: %s share overflows uint256 …`; `WBFT: negative dust …`; `WBFT: dust overflows uint256 …` | header |
| `engine/engine.go:1223`, `:1228`, `:1232`, `:1237`, `:1244`, `:1248`, `:1254`, `:1258` | verifyEpoch | `WBFT: epochInfo is nil`; `WBFT: mismatch in candidate sizes`; `WBFT: The two candidates do not match at index %d …`; `WBFT: Diligence mismatch at index %d …`; `WBFT: mismatch in validator sizes`; `WBFT: The two validators do not match`; `WBFT: mismatch in BLS public key sizes`; `WBFT: The two BLS public keys do not match` | header |
| `engine/engine.go:1331` | getSignerAddress | `validator address is zero` | rpc, header |
| `engine/engine.go:1342`, `:1349` | verifyAggregatedSeal | `lack of seal count`; `sealer is not validator` | header (§1.3 의 seal 오류로 바뀐다. 문구는 L140 – L143 에서만 보인다), log |
| `engine/engine.go:1443` | extractEpochInfo | `WBFT: epochInfo is nil` | header |
| `engine/engine.go:1467` | computeShuffledIndex | `input index %d out of bounds: %d` | header |
| `messages/preprepare.go:90`, `:97` | Preprepare.DecodeRLP | `failed to decode preprepare: %w`; `failed to decode SignedRoundChange[%d]: %w` (둘 다 `messages.Decode` 에서 `ErrFailedDecodePreprepare` 로 바뀐다) | network |
| `testutil.go:64` | SetConfigFromChainConfig (테스트용 사본) | `hardfork transition block already exists` | production 에서는 쓰지 않는다 (production 사본은 `eth/ethconfig/config.go:248`) |

### 1.7 WBFT 경로가 돌려주는, `consensus/wbft` 밖에서 온 오류

| 오류 | 생기는 곳 | Observable |
|---|---|---|
| `consensus.ErrUnknownAncestor` | parent header 가 없다 (`engine/engine.go:264`, `engine/engine.go:476`, `engine/engine.go:495`, `engine/engine.go:629`, `engine/engine.go:1428`, `backend/engine.go:95`, `backend/engine.go:426`, `backend/engine.go:439`) | header |
| `consensus.ErrFutureBlock` | `Header.Time > now + allowed_future_block_time` (`engine/engine.go:202-205`). `VerifyBlockProposal` 에서 대기 시간으로 바뀐다 (`engine/engine.go:174-175`) | header, network |
| RLP 디코드 오류 (`rlp: …`) | 위의 extra 오류와 메시지 오류로 바뀐다. API 경로(`backend/api.go:269`)는 `%w` 로 감싼다 | log |
| BLS 오류 (`public key must be 48 bytes`, `received an infinite public key`, `signature not in group`, …) | `crypto/bls/blst/*.go`. engine 경로에서는 seal 오류로 바뀐다. public key 가 잘못되면 `core.verifySeal` 이 감싸지 않고 그대로 돌려준다 (`core/core.go:474-477`) | log |
| secp256k1 오류 (`invalid signature length`, `invalid signature recovery id`, `recovery failed`) | `crypto/secp256k1/secp256.go`. 메시지 경로에서는 `errInvalidSigner` 로 바뀐다 (`core/handler.go:279-281`). header 경로에서는 `failed to verify randao reveal signature: %w` 로 감싸진다 (`engine/engine.go:314`) | log |
| `types.ErrInvalidIstanbulHeaderExtra` (`core/types/istanbul.go:49`) | 정의되어 있지만 반환되지 않는다 | unused |

---

## 2. 로그 카탈로그 (informative)

### 2.1 범위와 방법

아래 표는 `consensus/wbft/**` 의 테스트가 아닌 파일에 있는 로그 호출 가운데, 메시지가 `WBFT: ` 로 시작하는 문자열 literal(또는 `fmt.Sprintf` format)인 호출을 모두 나열한다. 이 목록은 각 파일의 Go 구문 트리를 훑어서 만들었고, 그다음 손으로 일부를 확인했다. 찾은 문장은 184 개다. v0.1 초안은 178 개를 나열했다. 초안에는 `config.go:277`, `core/commit.go:174`, `core/core.go:280`, `core/core.go:302`, `core/final_committed.go:28`, `engine/engine.go:908` 이 빠져 있었다.

열:

- **위치**: `consensus/wbft/` 에 대한 상대 경로와 호출이 있는 줄이다.
- **레벨**: geth 로그 레벨이다 (`Trace` < `Debug` < `Info` < `Warn` < `Error`).
- **메시지**: literal 이다. `fmt: …` 는 `fmt.Sprintf` format 문자열을 표시한다.
- **호출 필드**: 호출에 넘긴 key/value 쌍이며, `key=expression` 으로 적는다. `x...` 는 쌍들이 `x` 에서 펼쳐진다는 뜻이다.
- **상속된 context**: 호출 전에 logger 에 붙은 key 다 (`logger.New(...)`, `currentLogger`, `withMsg`). 코어의 기본 logger 는 `address` (노드 주소, `core/core.go:62`) 를 가진다. `currentLogger(state, msg)` 는 round state 가 있으면 `current.round`, `current.sequence` 를, `state = true` 이면 `state` 를, 메시지가 주어지면 `msg.code`, `msg.source`, `msg.round`, `msg.sequence` 를 더한다 (`core/handler.go:319-343`). logger 변수가 여러 분기에서 대입되면, 표는 소스 순서로 마지막 대입의 context 를 보인다. `—` 는 상속된 key 가 없다는 뜻이며, package 수준 `log` 나 backend logger 를 쓰는 호출이 여기에 해당한다.

geth 는 레코드를 `LVL [date|time] message key=value …` 로 찍는다. 위의 key 는 상속된 context 뒤에 그 순서대로 나타난다.

이 카탈로그의 역할. 이 카탈로그는 `Observable: log` 태그가 붙은 requirement 가 가리키는 로그 문구의 원본이다. inspector 가 참조 구현에 쓰는 구현 프로파일(로그 레코드를 event 로 바꾸는 표, 예를 들어 `gstable@740526d03`)은 이 표에서 생성하며, 손으로 고치지 않는다. 참조 commit 이 바뀌면 이 표를 다시 만들고(`tools/logcat`) 프로파일도 다시 생성한다. 그래서 로그 문구는 한 곳에서만 정의되고, 프로파일이 명세와 어긋나지 않는다.

값의 표기. terminal 형식과 logfmt 형식에서 문자열이 아닌 값은 다음과 같이 찍힌다. `currentLogger` 가 붙이는 `state` 는 `"Accept request"`, `Preprepared`, `Prepared`, `Committed` 가운데 하나로 찍히고, 공백이 있으면 따옴표로 감싸진다. `old.state` 와 `new.state` 는 같은 문자열로 넘겨진다. validator (`next.proposer`, `old.proposer`) 는 checksum 주소로 찍힌다. validator 목록 (`next.valSet`) 은 `"[addr addr …]"` 로 찍힌다. big integer 와 uint64 값은 10진수로 찍힌다. float 인 `F` (L131) 는 terminal 형식에서 `1.000` 으로, logfmt 에서 `1` 로 찍힌다. json 형식 (`--log.format json`) 에서는 big integer 가 JSON 문자열로, uint64 값과 float 가 JSON 숫자로 찍힌다. validator 목록은 빈 object 의 목록 (`[{},{},…]`) 으로 찍히므로, json log 에서는 새 validator 집합의 주소가 보이지 않는다. view (L019) 는 `{Round: r, Sequence: s}` 문자열로 넘겨지며, 두 숫자는 uint64 로 잘린다. Sources: `core/types.go:44-56` (State.String), `types.go:87-89` (View.String), `validator/default.go:45-47` (validator String), `core/core.go:215`, `:273`, `:302`, `core/handler.go:319-343`.

> 해설: inspector 가 log 에서 height 마다 validator 집합을 읽으려면 terminal 이나 logfmt 형식의 log 를 받아야 한다. json log 만 모으는 환경에서는 RPC `istanbul_getValidators` 로 validator 집합을 대신 얻는다.

개별 항목에 대한 참고:

- L147 (`engine/engine.go:690`) 은 필드를 넘긴다 (`"err", "number", it.Number, err`). 그래서 레코드에는 `err=number` 가 찍히고, 그 뒤에 block 번호로 만든 key 가 따라온다.
- L160 – L167 과 L169 – L183 (`messages/roundchange.go`) 은 신뢰할 수 없는 입력을 디코드하다 실패하면 Error 레벨로 로그를 남긴다. L168 은 디코드에 성공했을 때 남기는 Debug 기록이다.

> 해설: `L001`–`L184` 식별자는 commit 마다 다시 만들어지므로 requirement ID 와 달리 안정적이지 않다. inspector 가 `L` 번호를 규칙의 key 로 쓰면, 기준 commit 이 바뀔 때 규칙이 조용히 엉뚱한 로그를 가리킨다. 그래서 inspector 는 메시지 문구와 위치를 함께 key 로 쓰고, commit 이 바뀔 때 목록을 다시 대조해야 한다. 이 방식의 단점은 문구가 바뀔 때에도 규칙을 손봐야 한다는 것이다. 또 외부 peer 는 Error 레벨 로그를 마음대로 만들어 낼 수 있다 (L070, L071, L160–L183). 그래서 Error 레벨 로그의 양을 경보 기준으로 쓰면, 외부 peer 가 그 경보를 마음대로 울릴 수 있다.

### 2.2 표

| ID | 위치 | 레벨 | 메시지 | 호출 필드 | 상속된 context |
|---|---|---|---|---|---|
| L001 | `backend/backend.go:155` | Error | WBFT: invalid validator | `address=sb.Address(), validator=validator, payload=hexutil.Encode(payload), code=code` | — |
| L002 | `backend/backend.go:217` | Warn | WBFT: invalid block proposal | `proposal=proposal` | — |
| L003 | `backend/backend.go:231` | Info | WBFT: block proposal committed | `author=sb.Address(), hash=proposal.Hash(), number=proposal.Number().Uint64()` | — |
| L004 | `backend/backend.go:262` | Warn | WBFT: invalid block proposal | `proposal=proposal` | — |
| L005 | `backend/backend.go:268` | Warn | WBFT: bad block proposal | `proposal=proposal` | — |
| L006 | `backend/backend.go:335` | Error | WBFT: last block proposal invalid | `err=err` | — |
| L007 | `backend/backend.go:356` | Info | WBFT: activate WBFT | `—` | — |
| L008 | `backend/backend.go:357` | Trace | WBFT: set ProposerPolicy sorter to ValidatorSortByByteFunc | `—` | — |
| L009 | `backend/backend.go:362` | Error | WBFT: failed to activate WBFT | `err=err` | — |
| L010 | `backend/backend.go:374` | Info | WBFT: deactivate | `—` | — |
| L011 | `backend/backend.go:376` | Error | WBFT: failed to deactivate | `err=err` | — |
| L012 | `backend/engine.go:265` | Info | WBFT: start | `—` | — |
| L013 | `backend/handler.go:107` | Debug | WBFT: received NewBlockMsg | `size=msg.Size, payload.type=reflect.TypeOf(msg.Payload), sender=addr` | — |
| L014 | `backend/handler.go:120` | Warn | WBFT: unable to decode the NewBlockMsg | `error=err` | — |
| L015 | `backend/handler.go:125` | Debug | WBFT: block already proposed | `hash=newRequestedBlock.Hash(), sender=addr` | — |
| L016 | `config.go:275` | Trace | WBFT: initial epoch info | `validators=epochInfo.Validators` | — |
| L017 | `config.go:277` | Trace | fmt: WBFT:   - candidates[%d] | `addr=candi.Addr, diligence=candi.Diligence` | — |
| L018 | `core/backlog.go:82` | Trace | WBFT: future message too far ahead in sequence, dropped | `msg_seq=view.Sequence.String(), curr_seq=curr.Sequence.String(), diff=seqDiff.String()` | address |
| L019 | `core/backlog.go:92` | Trace | WBFT: future sequence message too far ahead in round, dropped | `msg_view=view.String(), curr_view=curr.String(), threshold=roundThreshold` | address |
| L020 | `core/backlog.go:103` | Trace | WBFT: future message too far ahead in round, dropped | `msg_round=view.Round.String(), curr_round=curr.Round.String(), diff=roundDiff.String()` | address |
| L021 | `core/backlog.go:212` | Warn | WBFT: backlog from self | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L022 | `core/backlog.go:216` | Trace | WBFT: new backlog message | `backlogs_size=len(c.backlogs)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L023 | `core/backlog.go:233` | Trace | WBFT: duplicate backlog message, dropping | `src=src` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L024 | `core/backlog.go:238` | Warn | WBFT: backlog is full, dropping message | `src=src, size=backlog.Size()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L025 | `core/backlog.go:268` | Trace | WBFT: process backlog | `—` | address, from, state |
| L026 | `core/backlog.go:290` | Trace | WBFT: stop processing backlog | `msg=msg` | address, from, state |
| L027 | `core/backlog.go:297` | Trace | WBFT: skip backlog message | `msg=msg, err=err` | address, from, state |
| L028 | `core/backlog.go:301` | Trace | WBFT: post backlog event | `msg=msg` | address, from, state |
| L029 | `core/commit.go:56` | Error | WBFT: failed to encode payload of COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L030 | `core/commit.go:62` | Error | WBFT: failed to sign COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L031 | `core/commit.go:70` | Error | WBFT: failed to encode COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L032 | `core/commit.go:74` | Info | WBFT: broadcast COMMIT message | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L033 | `core/commit.go:75` | Trace | WBFT: COMMIT payload | `payload=hexutil.Encode(payload)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L034 | `core/commit.go:79` | Error | WBFT: failed to broadcast COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L035 | `core/commit.go:93` | Debug | WBFT: handle COMMIT message | `commits.count=c.current.WBFTCommits.Size(), quorum=c.valSet.QuorumSize()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L036 | `core/commit.go:97` | Warn | WBFT: invalid COMMIT message digest | `digest=commit.Digest, proposal=c.current.Proposal().Hash().String()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L037 | `core/commit.go:104` | Warn | WBFT: failed to cast proposal from COMMIT message to *types.Block | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L038 | `core/commit.go:110` | Warn | WBFT: failed to verify seal from COMMIT message | `number=block.Header().Number, from=commit.Source()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L039 | `core/commit.go:116` | Error | WBFT: failed to save COMMIT message | `err=err` | address |
| L040 | `core/commit.go:124` | Info | WBFT: received quorum of COMMIT messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, commits.count, quorum |
| L041 | `core/commit.go:127` | Trace | WBFT: accepted new COMMIT messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, commits.count, quorum |
| L042 | `core/commit.go:174` | Error | WBFT: error committing proposal | `err=err` | address, current.round, current.sequence, state |
| L043 | `core/core.go:183` | Debug | WBFT: initialize new round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L044 | `core/core.go:186` | Debug | WBFT: start at the initial round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L045 | `core/core.go:195` | Debug | WBFT: catch up last block proposal | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L046 | `core/core.go:199` | Debug | WBFT: same round, no need to start new round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L047 | `core/core.go:202` | Warn | WBFT: next round is inferior to current round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L048 | `core/core.go:207` | Warn | WBFT: next sequence is before last block proposal | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L049 | `core/core.go:273` | Info | WBFT: start new round | `next.round=newView.Round, next.seq=newView.Sequence, next.proposer=c.valSet.GetProposer(), next.valSet=c.valSet.List(), next.size=c.valSet.Size(), next.IsProposer=c.IsProposer()` | address, old.round, old.sequence, old.state, old.proposer |
| L050 | `core/core.go:280` | Warn | WBFT: Discarding prepared block due to bad proposal | `hash=c.current.preparedBlock.Hash()` | address, current.round, current.sequence |
| L051 | `core/core.go:302` | Debug | WBFT: changed state | `old.state=oldState.String(), new.state=state.String()` | address, current.round, current.sequence |
| L052 | `core/core.go:359` | Warn | WBFT: newRoundChangeTimer skipped: current view not initialized | `—` | address |
| L053 | `core/core.go:385` | Warn | WBFT: Possible request timeout overflow detected, setting timeout value to maxRequestTimeout | `timeout=timeout.Seconds(), max_request_timeout=maxRequestTimeout.Seconds()` | address, current.round, current.sequence |
| L054 | `core/core.go:395` | Warn | WBFT: Timeout overflow detected, setting timeout value to MaxInt64 | `adjusted_timeout=time.Duration(math.MaxInt64).Seconds()` | address, current.round, current.sequence |
| L055 | `core/core.go:404` | Trace | WBFT: start new ROUND-CHANGE timer | `timeout=timeout.Seconds()` | address, current.round, current.sequence |
| L056 | `core/core.go:442` | Trace | WBFT: set ROUND-CHANGE retry timer | `round=round.Uint64(), timeout=timeout.Seconds()` | address |
| L057 | `core/extraseal.go:55` | Error | WBFT: invalid extra PREPARE message digest | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L058 | `core/extraseal.go:61` | Error | WBFT: PREPARE seal verify failed | `round=prepareMsg.CommonPayload.Round, from=prepareMsg.Source(), err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L059 | `core/extraseal.go:71` | Error | WBFT: invalid extra COMMIT message digest | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L060 | `core/extraseal.go:77` | Error | WBFT: COMMIT seal verify failed | `round=commitMsg.CommonPayload.Round, from=commitMsg.Source(), err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L061 | `core/extraseal.go:100` | Trace | WBFT: new extra prepare seal message | `source=prepareMsg.Source(), sequence=prepareMsg.Sequence.Uint64(), round=prepareMsg.Round.Uint64()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L062 | `core/extraseal.go:114` | Trace | WBFT: new extra commit seal message | `source=commitMsg.Source(), sequence=commitMsg.Sequence.Uint64(), round=commitMsg.Round.Uint64()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L063 | `core/extraseal.go:192` | Debug | WBFT: clear extra prepare seal | `source=addr, sequence=msg.Sequence.Uint64(), round=msg.Round.Uint64(), lastNum=lastNum.Uint64()` | — |
| L064 | `core/extraseal.go:200` | Debug | WBFT: clear extra commit seal | `source=addr, sequence=msg.Sequence.Uint64(), round=msg.Round.Uint64(), lastNum=lastNum.Uint64()` | — |
| L065 | `core/final_committed.go:28` | Info | WBFT: handle final committed | `—` | address, current.round, current.sequence, state |
| L066 | `core/handler.go:36` | Info | WBFT: start | `—` | address |
| L067 | `core/handler.go:51` | Info | WBFT: stopping... | `—` | address |
| L068 | `core/handler.go:57` | Info | WBFT: stopped | `—` | address |
| L069 | `core/handler.go:145` | Error | WBFT: can not encode backlog message | `err=err` | address |
| L070 | `core/handler.go:192` | Error | WBFT: invalid message event code | `—` | address, code, data |
| L071 | `core/handler.go:199` | Error | WBFT: invalid message | `err=err` | address, code, data |
| L072 | `core/handler.go:243` | Error | WBFT: invalid messages code | `code=m.Code()` | address |
| L073 | `core/handler.go:256` | Warn | WBFT: TIMER CHANGING ROUND | `pr=c.current.preparedRound` | address, current.round, current.sequence, state |
| L074 | `core/handler.go:258` | Warn | WBFT: TIMER CHANGED ROUND | `pr=c.current.preparedRound` | address, current.round, current.sequence, state |
| L075 | `core/handler.go:275` | Error | WBFT: invalid message payload | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L076 | `core/handler.go:280` | Error | WBFT: invalid message signature | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L077 | `core/justification.go:99` | Trace | WBFT: hasQuorumOfRoundChangeMessagesForNil | `rc=m` | — |
| L078 | `core/justification.go:116` | Trace | WBFT: hasQuorumOfRoundChangeMessagesForPreparedRoundAndBlock | `rc=m` | — |
| L079 | `core/justification.go:162` | Warn | WBFT: duplicate ROUND-CHANGE from same source, skipping | `source=addr` | — |
| L080 | `core/justification.go:180` | Warn | WBFT: duplicate PREPARE from same source, skipping | `source=addr` | — |
| L081 | `core/prepare.go:54` | Error | WBFT: failed to encode payload of PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L082 | `core/prepare.go:59` | Error | WBFT: failed to sign PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L083 | `core/prepare.go:67` | Error | WBFT: failed to encode PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L084 | `core/prepare.go:71` | Info | WBFT: broadcast PREPARE message | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L085 | `core/prepare.go:72` | Trace | WBFT: PREPARE payload | `payload=hexutil.Encode(payload)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L086 | `core/prepare.go:76` | Error | WBFT: failed to broadcast PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L087 | `core/prepare.go:90` | Debug | WBFT: handle PREPARE message | `prepares.count=c.current.WBFTPrepares.Size(), quorum=c.valSet.QuorumSize()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L088 | `core/prepare.go:94` | Warn | WBFT: invalid PREPARE message digest | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L089 | `core/prepare.go:101` | Warn | WBFT: failed to cast proposal from PREPARE message to *types.Block | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L090 | `core/prepare.go:107` | Warn | WBFT: failed to verify seal from PREPARE message | `number=block.Header().Number, from=prepare.Source()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L091 | `core/prepare.go:113` | Error | WBFT: failed to save PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L092 | `core/prepare.go:122` | Info | WBFT: received quorum of PREPARE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, prepares.count, quorum |
| L093 | `core/prepare.go:135` | Trace | WBFT: PREPARE message matches proposal | `proposal=c.current.Proposal().Hash(), prepare=prepare.Digest` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, prepares.count, quorum |
| L094 | `core/prepare.go:142` | Trace | WBFT: accepted PREPARE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, prepares.count, quorum |
| L095 | `core/preprepare.go:60` | Error | WBFT: failed to encode payload of PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L096 | `core/preprepare.go:65` | Error | WBFT: failed to sign PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L097 | `core/preprepare.go:75` | Trace | WBFT: add ROUND-CHANGE justification | `rc=m.(*wbfmessage.RoundChange).SignedRoundChangePayload` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L098 | `core/preprepare.go:77` | Trace | WBFT: extended PRE-PREPARE message with ROUND-CHANGE justifications | `justifications=preprepare.JustificationRoundChanges` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L099 | `core/preprepare.go:83` | Trace | WBFT: extended PRE-PREPARE message with PREPARE justification | `justification=preprepare.JustificationPrepares` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L100 | `core/preprepare.go:89` | Error | WBFT: failed to encode PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L101 | `core/preprepare.go:95` | Info | WBFT: broadcast PRE-PREPARE message | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, block.number, block.hash |
| L102 | `core/preprepare.go:96` | Trace | WBFT: PRE-PREPARE payload | `payload=hexutil.Encode(payload)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, block.number, block.hash |
| L103 | `core/preprepare.go:100` | Error | WBFT: failed to broadcast PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, block.number, block.hash |
| L104 | `core/preprepare.go:120` | Debug | WBFT: handle PRE-PREPARE message | `—` | address |
| L105 | `core/preprepare.go:124` | Warn | WBFT: ignore PRE-PREPARE message from non proposer | `proposer=c.valSet.GetProposer().Address()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L106 | `core/preprepare.go:130` | Warn | WBFT: ignore PRE-PREPARE with mismatched sequence and proposal number | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L107 | `core/preprepare.go:139` | Warn | WBFT: invalid PRE-PREPARE message justification | `je.CtxWithErr()...` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L108 | `core/preprepare.go:141` | Warn | WBFT: invalid PRE-PREPARE message justification | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L109 | `core/preprepare.go:151` | Info | WBFT: PRE-PREPARE block proposal is in the future (will be treated again later) | `duration=duration` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L110 | `core/preprepare.go:165` | Warn | WBFT: invalid PRE-PREPARE block proposal | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L111 | `core/preprepare.go:173` | Debug | WBFT: accepted PRE-PREPARE message | `—` | address |
| L112 | `core/request.go:36` | Debug | WBFT: handle block proposal request | `—` | address, current.round, current.sequence, state |
| L113 | `core/request.go:40` | Error | WBFT: invalid request | `—` | address, current.round, current.sequence, state |
| L114 | `core/request.go:43` | Error | WBFT: unexpected request | `err=err, number=request.Proposal.Number(), hash=request.Proposal.Hash()` | address, current.round, current.sequence, state |
| L115 | `core/request.go:84` | Trace | WBFT: store block proposal request for future treatment | `—` | address, current.round, current.sequence, state, proposal.number, proposal.hash |
| L116 | `core/request.go:99` | Trace | WBFT: lookup for pending block proposal requests | `—` | address, current.round, current.sequence, state |
| L117 | `core/request.go:107` | Trace | WBFT: stop looking up for pending block proposal request | `—` | address, current.round, current.sequence, state |
| L118 | `core/request.go:111` | Trace | WBFT: skip pending invalid block proposal request | `number=r.Proposal.Number(), hash=r.Proposal.Hash(), err=err` | address, current.round, current.sequence, state |
| L119 | `core/request.go:114` | Debug | WBFT: found pending block proposal request | `proposal.number=r.Proposal.Number(), proposal.hash=r.Proposal.Hash()` | address, current.round, current.sequence, state |
| L120 | `core/roundchange.go:59` | Warn | WBFT: invalid past target round | `current=cv.Round, target=round` | address, current.round, current.sequence, state |
| L121 | `core/roundchange.go:68` | Error | WBFT: failed to encode ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L122 | `core/roundchange.go:73` | Error | WBFT: failed to sign ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L123 | `core/roundchange.go:81` | Debug | WBFT: extended ROUND-CHANGE message with PREPARE justification | `justification=roundChange.Justification` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L124 | `core/roundchange.go:87` | Error | WBFT: failed to encode ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L125 | `core/roundchange.go:91` | Info | WBFT: broadcast ROUND-CHANGE message | `payload=hexutil.Encode(data)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L126 | `core/roundchange.go:95` | Error | WBFT: failed to broadcast ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L127 | `core/roundchange.go:115` | Info | WBFT: handle ROUND-CHANGE message | `higherRoundChanges.count=num, currentRoundChanges.count=currentRoundMessages` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L128 | `core/roundchange.go:130` | Warn | WBFT: round-change validation failed | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L129 | `core/roundchange.go:139` | Warn | WBFT: round-change validation failed | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L130 | `core/roundchange.go:148` | Warn | WBFT: failed to add ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L131 | `core/roundchange.go:166` | Info | WBFT: received F+1 ROUND-CHANGE messages | `F=c.valSet.F()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L132 | `core/roundchange.go:171` | Info | WBFT: received quorum of ROUND-CHANGE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L133 | `core/roundchange.go:183` | Warn | WBFT: round change returns an error: no proposal as pending request is nil | `—` | — |
| L134 | `core/roundchange.go:200` | Error | WBFT: invalid ROUND-CHANGE message justification | `je.CtxWithErr()...` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L135 | `core/roundchange.go:202` | Error | WBFT: invalid ROUND-CHANGE message justification | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L136 | `core/roundchange.go:214` | Debug | WBFT: accepted ROUND-CHANGE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L137 | `core/roundchange.go:278` | Warn | WBFT: ROUND-CHANGE prepared justification mismatch, ignoring prepared state | `source=roundChange.Source(), round=round, err=err` | — |
| L138 | `engine/engine.go:297` | Trace | WBFT: Skipping blacklisted signer verification due to unavailable state | `number=header.Number, err=err` | — |
| L139 | `engine/engine.go:347` | Trace | WBFT: Skipping gas tip verification due to unavailable state | `number=header.Number, err=err` | — |
| L140 | `engine/engine.go:396` | Error | WBFT: failed to verify previous prepared seal | `number=parent.Number, round=extra.PrevRound, err=err` | — |
| L141 | `engine/engine.go:406` | Error | WBFT: failed to verify previous committed seal | `number=parent.Number, round=extra.PrevRound, err=err` | — |
| L142 | `engine/engine.go:431` | Error | WBFT: failed to verify prepared seal | `err=err` | — |
| L143 | `engine/engine.go:444` | Error | WBFT: failed to verify committed seal | `err=err` | — |
| L144 | `engine/engine.go:653` | Error | WBFT: failed to determine epoch block | `number=header.Number, err=err` | — |
| L145 | `engine/engine.go:673` | Error | WBFT: failed to get latest epoch info | `number=header.Number, err=err` | — |
| L146 | `engine/engine.go:684` | Error | WBFT: failed to extract wbft extra data | `number=it.Number, err=err` | — |
| L147 | `engine/engine.go:690` | Error | WBFT: failed to get proposer | `err="number", it.Number=err` | — |
| L148 | `engine/engine.go:700` | Error | WBFT: failed to get previous epoch info | `number=parent.Number, err=err` | — |
| L149 | `engine/engine.go:719` | Error | WBFT: failed to get previous prepare signers | `number=it.Number, err=err` | — |
| L150 | `engine/engine.go:731` | Error | WBFT: failed to get previous commit signers | `number=it.Number, err=err` | — |
| L151 | `engine/engine.go:739` | Trace | WBFT: Seals count | `current block number=it.Number, prepareSigners=prepareSigners, commitSigners=commitSigners` | — |
| L152 | `engine/engine.go:763` | Error | WBFT: failed to get prior epoch info | `number=parent.Number, err=err2` | — |
| L153 | `engine/engine.go:784` | Error | WBFT: Invalid round | `num=header.Number.Uint64(), err=err` | — |
| L154 | `engine/engine.go:799` | Trace | WBFT: Seals counts in epoch | `header.number=header.Number, current block number=latestEpoch, proposedSealsInEpoch=proposedSealsInEpoch, submittedSealsInEpoch=submittedSealsInEpoch` | — |
| L155 | `engine/engine.go:887` | Error | WBFT: Failed to decide validators | `err=err` | — |
| L156 | `engine/engine.go:896` | Warn | WBFT: no BLS public key for the validator | `validator=addr` | — |
| L157 | `engine/engine.go:906` | Trace | WBFT: update epoch info | `header.Number=header.Number, validators=newEpoch.Validators` | — |
| L158 | `engine/engine.go:908` | Trace | fmt: WBFT:   - candidates[%d] | `addr=candidate.Addr, diligence=candidate.Diligence` | — |
| L159 | `engine/engine.go:1272` | Error | WBFT: Failed to compute shuffled index | `index=i, err=err` | — |
| L160 | `messages/roundchange.go:90` | Error | WBFT: Error List() Signed Payload | `struct="SignedRoundChangePayload", err=err` | — |
| L161 | `messages/roundchange.go:97` | Error | WBFT: Error Raw() | `err=err` | — |
| L162 | `messages/roundchange.go:104` | Error | WBFT: Error List() Payload | `err=err` | — |
| L163 | `messages/roundchange.go:109` | Error | WBFT: Error Decode(&m.Sequence) | `err=err` | — |
| L164 | `messages/roundchange.go:113` | Error | WBFT: Error Decode(&m.Round) | `err=err` | — |
| L165 | `messages/roundchange.go:120` | Error | WBFT: Error List() Prepared | `err=err` | — |
| L166 | `messages/roundchange.go:125` | Error | WBFT: Error Decode(&m.PreparedRound) | `err=err` | — |
| L167 | `messages/roundchange.go:129` | Error | WBFT: Error Decode(&p.PreparedDigest) | `err=err` | — |
| L168 | `messages/roundchange.go:153` | Debug | WBFT: Correctly decoded SignedRoundChangePayload | `p=p` | — |
| L169 | `messages/roundchange.go:212` | Error | WBFT: Error List() Signed Payload | `err=err` | — |
| L170 | `messages/roundchange.go:219` | Error | WBFT: Error Raw() | `err=err` | — |
| L171 | `messages/roundchange.go:226` | Error | WBFT: Error List() Payload | `err=err` | — |
| L172 | `messages/roundchange.go:231` | Error | WBFT: Error Decode(&m.Sequence) | `err=err` | — |
| L173 | `messages/roundchange.go:235` | Error | WBFT: Error Decode(&m.Round) | `err=err` | — |
| L174 | `messages/roundchange.go:242` | Error | WBFT: Error List() Prepared | `err=err` | — |
| L175 | `messages/roundchange.go:247` | Error | WBFT: Error Decode(&m.PreparedRound) | `err=err` | — |
| L176 | `messages/roundchange.go:251` | Error | WBFT: Error Decode(&m.PreparedDigest) | `err=err` | — |
| L177 | `messages/roundchange.go:274` | Error | WBFT: Error Kind() | `err=err` | — |
| L178 | `messages/roundchange.go:279` | Error | WBFT: Error Raw() | `err=err` | — |
| L179 | `messages/roundchange.go:284` | Error | WBFT: Error Decode(&m.PreparedDigest) | `err=err` | — |
| L180 | `messages/roundchange.go:288` | Error | WBFT: Error m.PreparedDigest.Hash() != digest | `m.hash=m.PreparedBlock.Hash(), m.digest=m.PreparedDigest` | — |
| L181 | `messages/roundchange.go:294` | Error | WBFT: Error Kind() | `err=err` | — |
| L182 | `messages/roundchange.go:299` | Error | WBFT: Error Raw() | `err=err` | — |
| L183 | `messages/roundchange.go:304` | Error | WBFT: Error Decode(&m.Justification) | `err=err` | — |
| L184 | `utils.go:63` | Error | WBFT: Failed to get signer address | `err=err` | — |

---

## 3. QBFT 에서 이어받은 계보 (informative)

WBFT 는 ConsenSys Quorum 의 QBFT 구현("istanbul/qbft")에서 파생되었다. 대부분의 파일에는 "derived from quorum/… (2024.07.25). Modified and improved for the wemix development" 라는 주석이 있다 (예: `consensus/wbft/config.go:18-19`, `core/types/istanbul.go:18-19`, `consensus/wbft/engine/apply_extra.go:17-18`). QBFT 는 다시 H. Moniz 의 "The Istanbul BFT Consensus Algorithm", arXiv:2002.03613 v2 (2020) 의 알고리즘을 구현한다. WBFT 는 별개의 protocol 이다. 이 명세와 QBFT 나 논문이 다르면 이 명세가 옳다. 아래의 차이는 설계상의 사실이며 결함이 아니다.

(A-05) 로 표시된 행은 v0.1 초안의 state machine 비교를 다시 적은 것이다. 이 부록을 쓰면서 그 행들을 다시 확인하지 않았으며, 그 행들에 대해서는 `A-05` 가 규범이다.

이 절의 QBFT 열과 모든 Quorum 경로는 ConsenSys Quorum commit `5ffacc48` (GoQuorum 24.4.1, `consensus/istanbul/**`) 을 가리킨다.

§3.2 의 형식 명세 열과 §3.4 전체는 ConsenSys 의 QBFT 형식 명세(`github.com/Consensys/qbft-formal-spec-and-verification`, commit `1630128e7`, Dafny, `dafny/spec/L1/**`)를 가리킨다. 형식 명세와 Quorum 이 서로 다른 곳에서는 WBFT 가 어느 쪽을 따르는지 각 행에 적었다. 형식 명세에 대해 증명된 safety 성질과, 그 증명의 가정 가운데 WBFT 가 만족하지 않는 것은 `A-05` §18.5 에 있다.

### 3.1 WBFT 가 QBFT 에서 그대로 가져온 것

| 항목 | WBFT | 정의된 곳 |
|---|---|---|
| 메시지 집합 | code `0x12 … 0x15` 의 PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE. legacy `0x11`. devp2p capability `istanbul/100` | `A-01 §4.1`, `A-03 §8`, `A-07` |
| signed payload 형태 | `rlp([code, fields])` 이고, 그 Keccak hash 에 ECDSA 로 서명한다. justification 항목은 자기 signature 를 따로 가진다 | `A-03 §8.1` |
| ROUND-CHANGE payload | `[sequence, round, prepared]` 이고 `prepared = [] | [prepared_round, prepared_digest]` 이다. prepared block 과 PREPARE justification 은 signed 부분 밖에 둔다 | `A-03 §8.4` |
| hash 규칙 | block hash 는 현재 block 의 seal 을 빼고 extra data 의 round 를 0 으로 두고 계산한다 (Quorum `QBFTFilteredHeaderWithRound(h, 0)`, `core/types/istanbul.go:204-228`) | `A-03 §6` |

### 3.2 WBFT 가 바꾸거나 더한 것

| 영역 | QBFT (Quorum `5ffacc48`) | 형식 명세 (`1630128e7`) | WBFT | 정의된 곳 |
|---|---|---|---|---|
| hash 규칙 표지 | `MixDigest == IstanbulDigest` 가 hash 규칙을 고른다 (`core/types/block.go:100-110`). `Difficulty = 1` 도 요구하지만 hash 규칙을 고르지는 않는다 | 모델링하지 않는다. `digest` 는 추상 함수이고, chain 은 `commitSeals` 와 `roundNumber` 를 뺀 채로 비교한다 (`dafny/spec/L1/types.dfy:31-62`) | `Difficulty = 1` 이 hash 규칙을 고른다. `MixDigest` 는 randao mix 를 싣는다 | `A-03 §6`, `A-02 §7` |
| header 에 필요한 seal | 서로 다른 validator 의 committed seal 이 `F + 1` 개 이상이면 된다 (`consensus/istanbul/qbft/engine/engine.go:246-287`) | validator 가 만든 commit seal 이 `quorum(n)` 개 이상 있어야 한다 (`ValidNewBlock`, `dafny/spec/L1/node_auxiliary_functions.dfy:815-820`). WBFT 는 이 규칙과 같고, Quorum 의 `F + 1` 은 다르다 | block 의 aggregated seal 각각과 이전 block 의 seal 에 sealer 가 `Q = quorum_size(N)` 개 이상 있어야 한다 | `A-08 §6.3–6.5` |
| seal 암호 | ECDSA committed seal. validator 마다 65 바이트 signature 하나 | commit seal 을 뺀 block 에 대한 추상 signature `signHash` (`dafny/spec/L1/node_auxiliary_functions.dfy:88-93, 289-297`) | BLS12-381 seal. seal 종류마다 96 바이트 aggregated signature 하나와 bitmap | `A-02 §5`, `A-03 §4.4` |
| 싣는 seal | committed seal 만 | commit seal 만 (`dafny/spec/L1/types.dfy:36-42`) | block 의 prepared seal 과 committed seal, 그리고 이전 block 의 prepared seal 과 committed seal (이전 block 의 seal 은 늦게 온 "extra seal" 로 늘어날 수 있다) | `A-03 §4.1`, `A-05`, `A-08` |
| PREPARE 내용 | view 와 digest | height, round, digest (`dafny/spec/L1/types.dfy:83-87`) | view, digest, BLS prepare seal | `A-03 §8.2` |
| header extra | vanity, validator 목록, vote, round, committed seal | proposer, round 번호, commit seal, height, timestamp (`dafny/spec/L1/types.dfy:36-42`) | 열 개 필드: vanity, randao reveal, 이전 round 와 seal, round, 현재 seal, gas tip, epoch info | `A-03 §4` |
| validator 집합 | header extra 의 투표 (`Vote` 필드) 로 정하거나, contract 에서 읽거나, transition 에 적는다. 주소 순으로 정렬한다 (`core/types/istanbul.go:127-133`, `consensus/istanbul/qbft/engine/engine.go:338-360`, `consensus/istanbul/validator/default.go:61`) | seal 을 뺀 chain 의 추상 함수이고, 증명에서는 바뀌지 않는다 (`dafny/spec/L1/node_auxiliary_functions.dfy:212-225`, `dafny/ver/L1/support_lemmas/axioms.dfy:17-18`) | epoch 로 정한다. 다음 집합은 contract candidate, diligence, keccak 기반 shuffle 로 계산해서 epoch block 에 쓴다 | `A-04` |
| 난수 | 없음 | 없음 | randao reveal (ECDSA) 과 `MixDigest` 의 mix | `A-02 §7` |
| 수수료 정책 | 없음 (고정 block reward 를 선택할 수 있다, `consensus/istanbul/qbft/engine/engine.go:554`) | 없음 | 모든 header 에 governance gas tip 을 싣는다 | `A-03 §4.3`, Part B |
| quorum | `Ceil2Nby3Block` 이 설정되어 있고 그 block 에 이르렀으며 `2FPlus1Enabled` transition 이 켜져 있지 않으면 `ceil(2N/3)` 이다. 그 밖에는 `F = ceil(N/3) − 1` 로 `2F + 1` 이다 (`consensus/istanbul/qbft/core/core.go:306-313`, 자세한 내용은 `A-04` §2.3). `consensus/wbft/validator/default.go:227` 의 주석은 아직 `ceil(2N/3)` 을 적는다 | `quorum(n) = (2n − 1) div 3 + 1` 이고 `ceil(2n/3)` 과 같다 (`dafny/spec/L1/node_auxiliary_functions.dfy:259-262`) | 부동소수로 계산하는 `ceil(N − (N−1)/3)` (`consensus/wbft/validator/default.go:222-229`). `N` 이 3 의 배수가 아니면 `ceil(2N/3)` 과 같고, 3 의 배수이면 WBFT 쪽이 하나 크다 | `A-04` |
| future 메시지 필터 | 상한 없는 backlog (`consensus/istanbul/qbft/core/backlog.go:105-128`) | 없다. 받은 메시지를 모두 보관한다 (`dafny/spec/L1/node.dfy:75-81`) | backlog 에 "too far ahead" 필터 (`SEQUENCE_THRESHOLD`, `ROUND_THRESHOLD`), 발신자별 크기 상한, slot 별 key 를 더했다 | `A-05` §13.1 |
| round change 재전송 | — | 없음 | retry timer 가 `request_timeout` 마다 ROUND-CHANGE 를 다시 만든다 (A-05). 바이트가 같은 사본은 이미 가진 peer 에게 다시 보내지 않으므로, 보통은 트래픽이 생기지 않는다 (WBFT-TIMER-024). `finalize` 가 실패한 뒤, `CATCH_UP` 분기를 탄 뒤, 새 height 에 들어간 뒤의 retry 는 원래 메시지와 달라서 전송된다 (`A-14` §5.8) | `A-06` |
| PRE-PREPARE justification 의 signature | ROUND-CHANGE 항목만 검증한다 (`consensus/istanbul/qbft/core/handler.go:266-282`) | justification 의 ROUND-CHANGE 와 PREPARE 는 모두 validator 가 서명한 것이어야 한다 (`dafny/spec/L1/node_auxiliary_functions.dfy:486, 554-561, 726`). WBFT 는 이 규칙과 같고, Quorum 은 PREPARE 를 검사하지 않는다 | ROUND-CHANGE 항목을 검증한 뒤 PREPARE 항목도 검증한다 | `A-05` WBFT-SM-017 |
| ROUND-CHANGE 의 prepared block | sequence 나 digest 와 대조하지 않는다 (`consensus/istanbul/qbft/core/roundchange.go:112-126`) | block 의 height 가 현재 height 이고, round 를 prepared round 로 되돌린 digest 가 prepared digest 와 같을 때에만 그 block 을 쓴다 (`dafny/spec/L1/node_auxiliary_functions.dfy:542-553`) | 번호가 현재 sequence 와 같아야 하고, hash 가 prepared digest 와 같아야 한다 | `A-05` §11.2 |
| signature 검증에 쓰는 validator 집합 | 현재 집합 (`consensus/istanbul/qbft/core/core.go:302-304`) | 노드의 현재 chain 의 집합 (`dafny/spec/L1/node_auxiliary_functions.dfy:486, 726, 775`) | 현재 집합이다. `AcceptRequest` 에서 이전 view 의 메시지에는 이전 sequence 의 집합을 쓴다 | `A-05` WBFT-SM-016 |
| round 0 의 timer | 노드 자신의 block 요청이나 PRE-PREPARE 수락이 timer 를 건다. round `> 0` 에 들어가면 timer 를 건다 (`consensus/istanbul/qbft/core/core.go:210-212`) | round 0 의 timeout 은 이전 block 을 붙인 때부터 잰다 (`timeLastRoundStart`, `dafny/spec/L1/node.dfy:385, 413, 429`). WBFT 는 이 규칙과 같고, Quorum 은 다르다 | 모든 view 에 들어갈 때 timer 를 건다 | `A-05` WBFT-SM-075, `A-06` §5.1 |
| block 요청에서 나가는 PRE-PREPARE | 모든 round 에서 보낸다 (`consensus/istanbul/qbft/core/request.go:48-55`) | round 0 에서만 보낸다 (`UponBlockTimeout`, `dafny/spec/L1/node.dfy:189-193`). WBFT 는 이 규칙과 같고, Quorum 은 다르다 | round 0 에서만 보낸다. 그 뒤 round 에서는 ROUND-CHANGE quorum 뒤에 제안한다 | `A-05` WBFT-SM-032 |
| validator 순서 | 주소 순으로 정렬한다 | 추상 나열 (`dafny/spec/L1/types.dfy:162`) | `EpochInfo.validators` 의 순서이며 정렬하지 않는다 | `A-04` WBFT-VAL-006 |
| block-period 대기 | block 을 만든 뒤 `Seal` 에서 기다린다 (`consensus/istanbul/backend/engine.go:205-211`) | round 0 의 proposer 는 자기 시계가 부모 timestamp 에 `blockTime` 을 더한 시각에 이르면 제안한다 (`dafny/spec/L1/node.dfy:189-193`) | block 을 만들기 전에 기다린다 | `A-06` §8 |
| 빈 block | `emptyBlockPeriod` 로 늦추고 검사할 수 있다 (`consensus/istanbul/qbft/core/request.go:56-97`) | 모델링하지 않는다 | 그런 규칙이 없다. 거래가 있든 없든 block period 마다 block 을 제안한다 | `A-06` §8 |

Quorum 에서 옮겨 온 구현은 Quorum 의 `F + 1` committed seal 검사와 `MixDigest` 표지를 바꿔야 한다. 두 규칙은 어떤 header 가 유효한지를 바꾸기 때문이다 (`A-08` §6.3, `A-03` §6).

### 3.3 논문과의 비교 (A-05)

Quorum 도 논문과 다음 점에서 같게 다르다. round 는 0 부터 시작하고, 값은 block builder 에서 오고, 미래 시각의 PRE-PREPARE 는 미뤄지고, COMMIT quorum 에서 round timer 를 멈추지 않고, commit certificate 로 답하지 않고, `pr < r` 을 검사하지 않고, F+1 건너뛰기는 수가 정확히 `F + 1` 일 때에만 일어난다 (`consensus/istanbul/qbft/core/roundchange.go:136`). 형식 명세는 같은 규칙을 모델링하는 곳에서 논문 쪽에 선다. 형식 명세는 `pr < r` 을 요구하고, 발신자가 `f + 1` 명 이상이면 언제든 F+1 규칙을 발동한다. 또 commit certificate 로 답하는 대신, decide 한 노드가 모든 노드에 `NewBlock` 메시지를 보낸다 (§3.4). `sequence = proposal.number` 검사, prepare seal 과 그 검증, header 로의 aggregation 은 WBFT 에만 있다.

| 논문 규칙 | WBFT | 종류 |
|---|---|---|
| round 는 1 부터 시작한다 | round 는 0 부터 시작한다 | 표기 |
| 입력 값과 함께 `Start(λ, value)` | 값은 나중에 block builder 에서 온다 (request event) | 변경 |
| PRE-PREPARE 를 받으면 justify 한 뒤 PREPARE 를 보낸다 | `sequence = proposal.number` 도 검사하고, block 유효성 검사를 돌리고, 미래 시각의 proposal 을 기다린다. PREPARE 는 seal 을 싣는다 | 변경 |
| PREPARE 가 quorum 만큼 모이면 COMMIT 을 보낸다 | 각 prepare seal 을 verify 한다. 뒤에 justification 으로 쓰려고 quorum 의 PREPARE 를 보관한다 | 변경 |
| COMMIT 이 quorum 만큼 모이면 timer 를 멈추고 decide 한다 | seal 을 aggregate 해서 header 에 넣고 block 을 chain 에 넘긴다. decide 할 때 round timer 를 멈추지 않는다 | 변경 |
| 더 높은 ROUND-CHANGE 가 `f+1` 개 오면 그 가운데 가장 작은 round 로 건너뛴다 | 더 높은 round 의 ROUND-CHANGE 를 보낸 서로 다른 sender 수가 `(F, F+1]` 안에 있을 때에만 건너뛴다 (`F` 는 실수 값이다, `A-04`). 즉 그 수가 정확히 `floor(F)+1` 일 때에만 건너뛰고, 그 수가 이미 더 크면 건너뛰지 않는다 (`core/roundchange.go:161`) | 변경 |
| decide 한 뒤에는 ROUND-CHANGE 에 commit certificate 로 답한다 | 구현하지 않았다. decide 된 block 과 그 seal 은 block sync 로 퍼진다 | 채택하지 않음 |
| ROUND-CHANGE 가 유효하려면 `pr < r` 이어야 한다 | 검사하지 않는다. justification 검사에 기댄다 | 채택하지 않음 |

### 3.4 QBFT 형식 명세와의 비교

이 표는 형식 명세의 state machine 요소 가운데 §3.2 가 다루지 않았고, WBFT 나 Quorum 이나 둘 다 형식 명세와 다르게 구현한 것을 적는다. 형식 명세 열에서 파일 이름 없이 적은 `:line` 은 `dafny/spec/L1/node_auxiliary_functions.dfy` 를 가리킨다. Quorum 열은 §3.2 와 같이 Quorum `5ffacc48` 을 가리킨다.

| 요소 | 형식 명세 (`1630128e7`) | Quorum `5ffacc48` | WBFT | 정의된 곳 |
|---|---|---|---|---|
| justification 에서 세는 발신자 | ROUND-CHANGE 의 서로 다른 발신자를 센다 (`getSetOfRoundChangeSenders`, `:424-427, 532`) | 메시지를 중복 제거 없이 센다 (`consensus/istanbul/qbft/core/justification.go:26`) | 서로 다른 source 를 센다 | `A-05` WBFT-SM-061 1 단계 |
| justification ROUND-CHANGE 의 view | 각 ROUND-CHANGE 가 proposal 의 height 와 round 에 대한 것이어야 한다 (`validRoundChange`, `:466-475, 533`) | 검사하지 않는다 (`consensus/istanbul/qbft/core/justification.go:20-51`) | 검사한다 | `A-05` WBFT-SM-061 3 단계 |
| justification ROUND-CHANGE 의 `prepared_round < round` | 요구한다 (`:480-483`) | 요구하지 않는다 | 요구하지 않는다 | `A-05` WBFT-SM-061 의 주석, §3.3 |
| prepare 되지 않은 justification | 집합의 모든 ROUND-CHANGE 가 prepare 되지 않은 것이어야 하고, block 은 유효하며 round leader 가 만든 것이어야 한다 (`:534-540`. `validateNonPreparedBlock`, `:449-459`) | prepare 되지 않은 ROUND-CHANGE 가 `Q` 개이면 된다 (`consensus/istanbul/qbft/core/justification.go:55-67`) | Quorum 과 같다 | `A-05` WBFT-SM-061 6 단계 |
| prepare 된 justification 의 ROUND-CHANGE | 집합의 모든 ROUND-CHANGE 가운데 가장 높은 prepared round 를 가진 ROUND-CHANGE 하나가 block 과 맞아야 한다 (`isHighestPrepared`, `:494-502, 546-553`) | prepared round 가 justify 하는 round 이하인 ROUND-CHANGE 가 `Q` 개 있고, 그 가운데 하나가 block 과 맞으면 된다 (`consensus/istanbul/qbft/core/justification.go:71-88`) | Quorum 과 같다 | `A-05` WBFT-SM-061 7 단계 |
| prepare 된 justification 의 PREPARE | PREPARE 가 `quorum(n)` 개 이상이고, 각각 height, prepared round, digest 가 맞고 validator 가 서명한 것이어야 한다 (`:545, 554-561, 714-727`) | round 와 digest 가 같아야 한다. 중복 제거 없이 세고, signature 를 검증하지 않는다 (`consensus/istanbul/qbft/core/justification.go:31-44`, `consensus/istanbul/qbft/core/handler.go:275-281`) | round 와 digest 가 같아야 한다. 중복을 없애고, signature 를 검증한다. sequence 는 검사하지 않는다 | `A-05` WBFT-SM-017, WBFT-SM-061 4-5 단계 |
| 다시 제안된 prepared block 의 유효성 | 다시 검사하지 않는다. block 은 ROUND-CHANGE 가 실어 온 block 가운데 하나여야 한다 (`:542`) | 모든 PRE-PREPARE 에 proposal 검증을 돌린다 (`consensus/istanbul/qbft/core/preprepare.go:130`) | Quorum 과 같다 | `A-05` WBFT-SM-037 검사 4 |
| 더 높은 round 의 PRE-PREPARE | 노드가 현재 round 에서 이미 proposal 을 받아들였으면 받아들이고, 노드는 그 round 로 옮긴다 (`isValidProposal`, `:587-596`. `dafny/spec/L1/node.dfy:254-256`) | 노드가 그 round 에 이를 때까지 backlog 에 둔다 (`consensus/istanbul/qbft/core/backlog.go:65-67`) | Quorum 과 같다 | `A-05` WBFT-SM-019 |
| round timer 다시 걸기 | proposal 이나 노드 자신의 justify 된 proposal 로 더 높은 round 에 들어갈 때, 그리고 block 을 붙일 때 다시 건다. round timeout, F+1 규칙, 현재 round 의 proposal 수락에서는 다시 걸지 않는다 (`dafny/spec/L1/node.dfy:257-261, 385, 413, 486-490`) | round `> 0` 에 들어갈 때, 노드 자신의 block 요청이 올 때, PRE-PREPARE 를 받아들일 때마다 다시 건다 (`consensus/istanbul/qbft/core/core.go:210-212`, `consensus/istanbul/qbft/core/request.go:52`, `consensus/istanbul/qbft/core/preprepare.go:156`) | 모든 view 에 들어갈 때와 PRE-PREPARE 를 받아들일 때마다 다시 건다 | `A-05` WBFT-SM-075, `A-06` WBFT-TIMER-010, WBFT-TIMER-012 |
| round timeout 길이 | 마지막으로 다시 건 때부터 `2^r` 시간 단위이다 (`roundTimeout`, `:267-270`. `dafny/spec/L1/node.dfy:429`) | 다시 걸 때마다 그때부터 `request_timeout · 2^r` 이고, 상한이 있다 (`consensus/istanbul/qbft/core/core.go:258-300`) | Quorum 과 같다 | `A-06` §4 |
| PREPARE quorum 전의 COMMIT | proposal 을 받아들인 뒤라면 COMMIT quorum 으로 decide 한다 (`UponCommit`, `dafny/spec/L1/node.dfy:341-360`) | 노드가 `Prepared` 가 될 때까지 COMMIT 은 future 메시지이다 (`consensus/istanbul/qbft/core/backlog.go:81-89`) | Quorum 과 같다 | `A-05` WBFT-SM-019 |
| 받은 COMMIT 의 seal | 검사한다. seal 은 COMMIT 을 보낸 노드가 proposal 에 한 signature 여야 한다 (`validateCommit`, `:767-776`) | core 가 검사하지 않는다 (`consensus/istanbul/qbft/core/commit.go:88-116`) | 검사한다 (BLS) | `A-05` WBFT-SM-045 |
| decision | block 을 붙이고, 모든 노드에 `NewBlock` 을 보내고, 다음 height 의 round 0 에 들어가고, lock 을 버린다 (`dafny/spec/L1/node.dfy:364-386`) | block 을 chain 에 넘긴다. 새 head 가 timer 를 멈추고 round 0 을 시작한다 (`consensus/istanbul/qbft/core/commit.go:122-142`, `consensus/istanbul/qbft/core/final_committed.go:21-29`) | block 을 chain 에 넘긴다. 새 head 가 올 때까지 round timer 가 도는 채로 `Committed` 에 머문다 | `A-05` WBFT-SM-047, WBFT-SM-050 |
| `NewBlock` 메시지 | 모든 노드에 보내는 합의 메시지이고, commit seal 이 quorum 만큼 있으면 받아들인다 (`dafny/spec/L1/types.dfy:141-143`. `ValidNewBlock`, `:815-820`) | 없다. block 은 `eth` protocol 로 전달된다 | Quorum 과 같다 | `A-07` §5.6, `A-09` §5 |
| F+1 규칙 | 더 높은 round 의 ROUND-CHANGE 를 보낸 발신자가 `f + 1` 명 이상이면 발동하고, 그 가운데 `f + 1` 명의 round 중 가장 작은 round 로 간다. proposal 을 justify 할 수 없을 때에만 평가한다 (`dafny/spec/L1/node.dfy:494-524`) | 발신자가 정확히 `F + 1` 명일 때 발동하고, 가지고 있는 더 높은 round 가운데 가장 작은 round 로 간다. proposal 규칙보다 먼저 평가한다 (`consensus/istanbul/qbft/core/roundchange.go:136-144`) | Quorum 과 같고, 구간 `(F, F + 1]` 을 쓴다. 메시지가 없는 round 도 가지고 있는 round 로 친다 | `A-05` WBFT-SM-056, WBFT-SM-057 |
| proposer 순환 | 이전 block 의 validator 집합에서 이전 block proposer 의 index 에 1 과 round 를 더한다 (`proposer`, `:304-323`) | 현재 집합에서의 index (없으면 0) 에 1 과 round 를 더한다 (`consensus/istanbul/validator/default.go:126-150`) | Quorum 과 같다. Sticky 정책은 1 을 더하지 않는다 | `A-04` WBFT-PROP-003, WBFT-PROP-004, WBFT-PROP-006 |
| validator 가 아닌 노드 | `UponNewBlock` 단계만 밟는다 (`dafny/spec/L1/node.dfy:136-147`) | `Broadcast` 가 membership 을 검사하지 않는다 (`consensus/istanbul/backend/backend.go:154-165`) | 합의 core 를 돌리지만 아무것도 보내지 않는다 | `A-05` WBFT-SM-003 |
| 한 height 안에서 lock 풀기 | 풀지 않는다 (`dafny/spec/L1/node.dfy:379-386, 407-413`) | 풀지 않는다 (`consensus/istanbul/qbft/core/core.go:218-224`) | bad-block 규칙이 lock 을 푼다 | `A-05` WBFT-SM-024 |
| proposer 가 justification 으로 쓰는 PREPARE | 자기가 직접 받은 PREPARE 를 쓴다 (`isReceivedProposalJustification`, `:614-615`) | 가장 높은 prepared ROUND-CHANGE 가 실어 온 justification 을 쓴다 (`consensus/istanbul/qbft/core/roundchange.go:241-247`) | Quorum 과 같다 | `A-05` WBFT-SM-055, WBFT-SM-059 |
| proposal signature 가 덮는 것 | height, round, digest 이고, block 은 그 digest 와 맞아야 한다 (`:585-586`) | height, round, block 전체 | Quorum 과 같다 | `A-03` §8 |

justification 에서 세는 발신자, justification ROUND-CHANGE 의 view, 받은 COMMIT 의 seal 행과, §3.2 의 header 에 필요한 seal, PRE-PREPARE justification 의 signature, round 0 의 timer, block 요청에서 나가는 PRE-PREPARE 행에서는 Quorum 이 형식 명세와 다르고 WBFT 는 형식 명세를 따른다. justification PREPARE 행에서 WBFT 는 형식 명세를 따르지만, PREPARE 의 sequence 는 검사하지 않는다. decision, validator 가 아닌 노드, lock 풀기 행에서 WBFT 는 둘 모두와 다르다. 나머지 행은 Quorum 을 따른다. 이 행들이 증명된 safety 성질과 어떤 관계인지는 `A-05` §18.5 에 적었다.
