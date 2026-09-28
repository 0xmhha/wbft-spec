# A-08 Header 규칙

- Area code: `HDR`
- Status: draft
- Reference: go-stablenet `740526d03`. 모든 `Source:` 줄은 이 commit 을 가리킨다. 이 장이 다루는 코드에서 `v1.1.0` 과 글자가 다른 곳은 `verifyGasTip` 의 gas tip `nil` 조건(`consensus/wbft/engine/engine.go:1291`) 하나뿐이다. 이 조건에는 도달할 수 없으므로 합의에 영향을 주는 차이는 없다 (`A-12` §2.1, `WBFT-HDR-111`).

이 장은 block header 의 *합의 필드* 를 정한다. 구체적으로는 proposer 가 합의 필드를 어떻게 만드는지, finalized header 가 seal 을 어떻게 받는지, 그리고 노드가 세 시점에서 합의 필드를 어떻게 검증하는지를 정한다. 세 시점은 PRE-PREPARE 에서의 proposal 검증, import 할 때의 finalized header 검증, state 없이 하는 light verification 이다. 참조 구현은 합의 필드 검사와 실행 필드 검사를 함수 하나 안에서 번갈아 한다. 그런 곳에서는 이 장이 순서가 있는 전체 절차를 적고, 각 단계에 다음 표시를 단다.

- **[A]** 는 이 장이 정하는 단계다.
- **[B]** 는 Part B 가 정하는 단계이며, 단계 안에 그 단계를 정하는 Part B 장의 이름을 적는다. 이 장은 그 단계의 순서상 위치를 고정하려고만 그 단계를 나열한다.

이 순서는 규범이다. 적합한 verifier 가 오류를 돌려줄 때는 반드시 처음 실패한 단계의 오류를 돌려주어야 한다. 호출자는 오류의 종류를 보고 다음 행동을 정하기 때문이다. 다음 행동에는 future block queue 에 넣기, unknown ancestor 처리, bad block 기록이 있다 (`A-09 §5` 참조).

> 해설: 결과가 똑같이 "거부" 라면 어느 오류를 먼저 내든 상관없어 보인다. 그러나 호출자는 오류의 종류로 다음 행동을 고른다. `ErrFutureBlock` 이면 노드는 block 을 future queue 에 넣고 나중에 다시 검사한다. `ErrUnknownAncestor` 이면 노드는 parent 가 future queue 에 있는지 보고, 있으면 future 로 처리하고 없으면 bad block 으로 기록한다. 그 밖의 오류이면 노드는 bad block 으로 기록하고 import 를 중단한다. 같은 header 라도 H2(미래 시각) 가 H4(difficulty) 보다 먼저 걸리면 block 은 버려지지 않고 몇 초 뒤에 다시 검사되며, 그때 비로소 `ErrInvalidDifficulty` 로 거부된다 (§10.6). 다른 순서로 검사하는 구현은 같은 입력에 대해 bad block 기록을 다르게 남긴다. bad block 기록은 PRE-PREPARE 거부 조건(`WBFT-HDR-061`)이 되므로, 결국 그 구현은 다른 노드와 다르게 투표한다.

---

## 1. 범위와 필드 소유

| Header 필드 | 소유 | 만드는 곳 | 검증하는 곳 |
|---|---|---|---|
| `ParentHash`, `Number` | 연결 규칙은 A(이 장)가 정하고, 값은 block builder 가 넣는다 | §3.1 | §6 단계 H1, H10, H11 |
| `Coinbase` | A | §3.3 | §6 단계 H15a(membership), `B-08` SNET-SRC-020(blacklist, 단계 H15b) |
| `Difficulty` | A | §3.3 | §6 단계 H4 |
| `Nonce` | A | §3.3 | 검증하지 않는다 (`WBFT-HDR-012`) |
| `Time` | A | §3.4 | §6 단계 H2, H12 |
| `MixDigest` | A | §3.9 | §6 단계 H19 |
| `Extra` (`WBFTExtra` 전체) | 구조, randao, seal, round, `EpochInfo` 존재 여부는 A 가 정하고, `GasTip` 값과 `EpochInfo` 내용은 B-06 이 정한다 | §3.5–§3.8, §4 | §6 단계 H16–H20, §7 |
| `UncleHash`, `GasLimit`, `GasUsed`, `BaseFee`, `WithdrawalsHash`, `BlobGasUsed`, `ExcessBlobGas`, `ParentBeaconRoot` | B-03 | block builder, `B-03` | §6 단계 H3, H5–H9, H13, H14 |
| `Root`, `TxHash`, `ReceiptHash`, `Bloom` | B-03 / B-06 | block builder | 실행(`A-09 §5`)과 단계 P4(`TxHash`) |

인코딩(`WBFTExtra`, `AggregatedSeal`, `SealerSet`, `EpochInfo`)은 `A-03` 이 정의한다. 암호 함수(`seal_data`, `randao_data`, `randao_mix`, `ecdsa_recover_address`, BLS 연산)는 `A-02` 가 정의한다. validator 집합, quorum, epoch(`validators_at`, `quorum_size`, `is_epoch_block`)는 `A-04` 가 정의한다.

> 해설: WBFT block header 의 필드에는 주인이 둘이다. 합의 필드는 Part A 가 정하고, 실행 결과 필드와 fork 관련 필드는 Part B 가 정한다. `Extra` 는 다시 둘로 나뉜다. 구조와 randao, seal, round, `EpochInfo` 의 존재 여부는 Part A 가 정하지만, `GasTip` 값과 `EpochInfo` 의 내용은 B-06 이 정한다. 참조 구현은 두 종류의 검사를 함수 하나(`verifyHeader` 와 그 뒤의 `verifyCascadingFields`) 안에서 번갈아 한다. 그래서 이 장은 그 순서를 그대로 옮겨 H1–H21 로 번호를 붙이고, 단계마다 소유 장을 표시한다.

---

## 2. 이 장에서 쓰는 표기

```python
# From A-01 / A-03 / A-04 (not redefined here)
config_at(number) -> Config            # BlockPeriod, Epoch, ... after transitions with Block <= number
validators_at(number, parent_hash, parents) -> ValidatorSet
                                        # A-04 §3.1 (chain argument implicit); set that seals block `number`;
                                        # number == 0 -> genesis config set; `parents` is A-04's optional
                                        # batch argument (headers not yet stored, consulted first)
quorum_size(n) -> int                   # ceil(n - (n - 1) / 3) evaluated in float64
decode_extra(extra: bytes) -> WBFTExtra # raises on any RLP error, trailing bytes, or missing field
encode_extra(x: WBFTExtra) -> bytes
block_hash(header) -> Hash              # keccak256(rlp(filtered_header(header, 0))) when Difficulty == 1
seal_data(header, round: uint32, seal_type) -> bytes32
randao_data(chain_id, number) -> bytes32
randao_mix(parent_mix: Hash, reveal: bytes) -> Hash
ecdsa_recover_address(data, sig) -> Address   # recovers from keccak256(data), see A-02

EMPTY_UNCLE_HASH = keccak256(rlp([]))
WBFT_DIFFICULTY = 1
EMPTY_NONCE = 0x0000000000000000
EXTRA_VANITY = 32
SEAL_LENGTH = 96
```

`now()` 는 로컬 벽시계 시각을 초 단위 정수(Unix)로 나타낸 값이다.

---

## 3. Proposal header 만들기

합의 코어가 proposal 을 요청하면(`A-09 §4.4`) 노드는 높이 `n = parent.Number + 1` 의 proposal 을 만든다. 먼저 실행 계층이 header 골격을 만든다. 골격에는 `ParentHash`, `Number`, `GasLimit`, `BaseFee`, 임시 `Time`, 임시 `Coinbase`, 그리고 `Extra` 안의 miner vanity 가 들어 있다. 그다음 실행 계층은 이 절에서 정하는 합의 준비 함수를 부르고, 이어서 transaction 을 실행하고 `process_finalize`(`B-06`)를 돌린다. `process_finalize` 는 `EpochInfo` 를 넣을 수 있고(§3.9) `Root` 를 정한다.

> 해설: 이 순서가 중요한 이유는 transaction 실행이 `Time`, `Coinbase`, `MixDigest`(EVM 의 `PREVRANDAO`), `Extra.GasTip` 을 읽기 때문이다. 그래서 합의 필드는 실행 **전에** 정해져야 한다 (`A-09` WBFT-APP-030). 반대로 `EpochInfo` 는 실행 **뒤의** state 에서 계산되므로 block 을 만드는 입력이 될 수 없다 (`A-09` WBFT-APP-031).

### 3.1 입력

| 입력 | 참조 구현에서 얻는 곳 |
|---|---|
| `parent` | block builder 가 시작할 때의 현재 head block header 이다. block builder 가 특정 parent 를 요청받았으면 `parentHash` 가 가리키는 block 이다 (`miner/worker.go:1124-1131`) |
| `header.ParentHash = block_hash(parent)`, `header.Number = parent.Number + 1` | `miner/worker.go:1142-1148` |
| `header.Extra` = miner vanity (0–32 바이트) | `miner/worker.go:1150-1152`, 상한 `params/protocol_params.go:31`, `miner/miner.go:205-211`, `eth/backend.go:307-322` |
| `extra_prepared`, `extra_committed`: 이전 높이가 quorum 에 도달한 뒤에 모은 `(sealer_index, seal)` 목록 (*extra seal*, `A-05`) | `consensus/wbft/backend/engine.go:221-229`, `consensus/wbft/core/extraseal.go:133-183` |
| 노드의 서명 키. `sign` 은 입력의 keccak256 에 ECDSA 로 서명한다 | `consensus/wbft/backend/backend.go:84, 281-284` |

### 3.2 알고리즘

```python
def prepare_proposal_header(chain, header, extra_prepared, extra_committed):
    # [WBFT-HDR-010] Coinbase
    header.Coinbase = self.address                      # node key address
    # [WBFT-HDR-012] Nonce
    header.Nonce = EMPTY_NONCE
    n = header.Number
    parent = chain.header_by_hash_and_number(header.ParentHash, uint64(n) - 1)   # low 64 bits
    if parent is None:
        raise ErrUnknownAncestor
    # [WBFT-HDR-011] Difficulty
    header.Difficulty = WBFT_DIFFICULTY
    # [WBFT-HDR-013] Time
    header.Time = max(parent.Time + config_at(n).BlockPeriod, now())
    # [WBFT-HDR-020] gas tip at parent state (value: B-06)
    gas_tip = app.gas_tip(parent)                       # raises -> no proposal (WBFT-HDR-021)
    prev = None
    if n > 1:                                           # full value
        last = chain.header_by_number(uint64(n) - 1)    # WBFT-HDR-030: canonical, by NUMBER (low 64 bits)
        if last.Number == 0:
            pass                                        # unreachable for 1 < n < 2^64; for n = k*2^64 + 1
                                                        # the lookup returns genesis and no previous seals are written
        else:
            lx = decode_extra(last.Extra)               # raises -> no proposal
            if lx.PreparedSeal is None: raise ErrEmptyPreparedSeals
            if lx.CommittedSeal is None: raise ErrEmptyCommittedSeals
            prev = (lx.Round,
                    merge_seals(lx.PreparedSeal, extra_prepared),
                    merge_seals(lx.CommittedSeal, extra_committed))
    # the following steps raise errors wrapped as "failed to write wbft extra: ..."
    x = start_extra(header.Extra)                       # WBFT-HDR-016, may raise
    # [WBFT-HDR-014] randao reveal, signed only after start_extra succeeded
    x.RandaoReveal = ecdsa_sign(self.key, randao_data(chain_id, n))   # 65 bytes; ecdsa_sign hashes (A-02 §7.2)
    if prev is not None:
        x.PrevRound, x.PrevPreparedSeal, x.PrevCommittedSeal = prev
    x.GasTip = gas_tip
    header.Extra = encode_extra(x)
    # [WBFT-HDR-040] MixDigest
    header.MixDigest = randao_mix(parent.MixDigest, x.RandaoReveal)
```

Source: `consensus/wbft/backend/engine.go:154-167`, `consensus/wbft/engine/engine.go:486-549, 551-573, 583-604`, `consensus/wbft/engine/apply_extra.go:38-50`.

### 3.3 고정 필드

[WBFT-HDR-010] proposer 는 반드시 `Coinbase` 를 자기 node key 의 주소로 두어야 한다. node key 는 합의 메시지와 randao reveal 에도 서명하는 ECDSA 키다.
Source: `consensus/wbft/engine/engine.go:487`, `consensus/wbft/backend/backend.go:75, 84`
Observable: header

[WBFT-HDR-011] proposer 는 반드시 `Difficulty` 를 `1` 로 두어야 한다.
Source: `consensus/wbft/engine/engine.go:499`, `core/types/istanbul.go:39`
Observable: header

[WBFT-HDR-012] proposer 는 반드시 `Nonce` 를 0 바이트 여덟 개로 두어야 한다. verifier 는 `Nonce` 값을 이유로 header 를 거부해서는 안 된다. 참조 구현은 어느 검증 시점에서도 `Nonce` 를 검사하지 않는다.
Source: `consensus/wbft/engine/engine.go:488`, `consensus/wbft/common/constants.go:30`; absence of a check: `consensus/wbft/engine/engine.go:196-350`
Observable: header

구현 노트(informative). 실행 계층은 `etherbase` 로 임시 `Coinbase` 를 넣는다 (`miner/worker.go:1147`). WBFT chain 에서 `etherbase` 는 node key 주소로 강제된다 (`eth/backend.go:173-176`). 어느 경우든 `prepare_proposal_header` 가 이 값을 덮어쓴다.

> 해설: verifier 가 `Nonce` 를 전혀 보지 않으므로, 새 구현이 `Nonce` 를 검사하면 기존 노드보다 엄격해진다. 그러면 기존 노드가 받아들이는 block 을 새 구현만 거부할 수 있다.

### 3.4 Timestamp

[WBFT-HDR-013] proposer 는 반드시 `Time = max(parent.Time + config_at(n).BlockPeriod, now())` 로 두어야 한다. 여기서 `BlockPeriod` 는 *새* block 의 높이 `n` 의 설정에서 가져온다.
Source: `consensus/wbft/engine/engine.go:501-505`, `consensus/wbft/config.go:161-184`
Observable: header

결과(informative):

- proposer 의 시계가 늦으면 `Time = parent.Time + BlockPeriod` 가 되고, block 은 예정대로 나온다.
- proposer 의 시계가 verifier 의 시계보다 `AllowedFutureBlockTime` 넘게 앞서면, verifier 는 그 proposal 을 future block 으로 보고 자기 시계가 `Time` 에 이르렀을 때 다시 처리한다 (단계 P6). 그래서 그런 proposer 가 활동하는 동안 chain 시각이 벽시계보다 앞설 수 있다. 그 뒤의 모든 block 은 그 block 보다 적어도 `BlockPeriod` 뒤의 시각을 가진다 (`A-10 §10`).
- `prepare_proposal_header` 는 block builder 가 넣은 timestamp(`miner/worker.go:1134-1140`)를 항상 덮어쓴다.

> 해설: `BlockPeriod` 를 **새 block 의 높이** `n` 의 설정에서 가져온다는 점을 놓치기 쉽다. transition 이 block `k` 에서 `BlockPeriod` 를 바꾸면, 새 값은 `k−1` 과 `k` 사이의 간격부터 적용된다. 검증 쪽 규칙(`WBFT-HDR-085`)도 같다. proposer 의 시계가 앞서서 생기는 대기에는 상한이 없다 (`WBFT-HDR-063`, `A-10 §10`).

### 3.5 Randao reveal

[WBFT-HDR-014] proposer 는 반드시 `Extra.RandaoReveal` 을 node key 로 `keccak256(randao_data(chain_id, n))` 에 서명한 65바이트 ECDSA signature `[R ‖ S ‖ V]` (`V ∈ {0,1}`) 로 두어야 한다. 참조 서명기는 RFC 6979 결정적 서명을 쓰므로, 정직한 노드는 `(chain_id, n)` 마다 reveal 을 정확히 하나만 만든다.
Source: `consensus/wbft/engine/engine.go:551-573`, `consensus/wbft/backend/backend.go:281-284`
Observable: header

`randao_data` 는 이미 Keccak-256 digest 인데 서명기가 그 값을 한 번 더 해시한다는 점에 주의한다. 그래서 실제로 서명되는 32바이트 메시지는 `keccak256(keccak256(chain_id_bytes ‖ 0x01 ‖ number_bytes))` 이다 (`A-02`).

> 해설: 새 구현이 이 값을 한 번만 해시해서 서명하면 그 구현이 만든 모든 reveal 을 기존 노드가 거부한다.

### 3.6 Extra data 시작과 vanity

`start_extra` 는 `getExtra` 를 재현한다:

```python
def start_extra(extra: bytes) -> WBFTExtra:
    if len(extra) < EXTRA_VANITY:
        return WBFTExtra(VanityData=extra + b"\x00" * (EXTRA_VANITY - len(extra)),
                         RandaoReveal=b"", PrevRound=0, PrevPreparedSeal=None,
                         PrevCommittedSeal=None, Round=0, PreparedSeal=None,
                         CommittedSeal=None, GasTip=None, EpochInfo=None)
    return decode_extra(extra)          # raises on failure
```

Source: `consensus/wbft/engine/engine.go:1162-1182`

[WBFT-HDR-016] block builder 가 32바이트보다 짧은 vanity 를 주면, proposer 는 반드시 그 바이트 뒤에 0 바이트를 채워 정확히 32바이트로 만든 값을 `Extra.VanityData` 에 넣어야 한다.
Source: `consensus/wbft/engine/engine.go:1163-1177`
Observable: header

[WBFT-HDR-017] verifier 는 길이가 32 가 아닌 값을 포함해 어떤 `VanityData` 값이든 반드시 받아들여야 한다. 참조 구현은 어느 검증 시점에서도 이 값에 제약을 두지 않는다.
Source: `consensus/wbft/engine/engine.go:301-304` (decode only), `core/types/istanbul.go:122-149`
Observable: header

경계 사례(호환을 위한 규범): block builder 가 정확히 32바이트의 vanity 를 주면, `start_extra` 는 그 바이트를 완전한 `WBFTExtra` 로 디코드하려고 한다. vanity 가 그 자체로 유효한 `WBFTExtra` 인코딩이 아니면 header 만들기가 실패하고, 노드는 proposal 을 내지 못한다. 32바이트 builder vanity 가 `WBFTExtra` 로 디코드되면, 노드는 디코드한 값에서 출발한다. 그래서 `prepare_proposal_header` 가 덮어쓰지 않는 필드는 디코드한 값을 그대로 가진다. 그런 필드는 `VanityData`, `Round`, `PreparedSeal`, `CommittedSeal`, `EpochInfo` 이고, `n = 1` 이면 `PrevRound`, `PrevPreparedSeal`, `PrevCommittedSeal` 도 들어간다.

[WBFT-HDR-018] 32바이트 block builder vanity 를 받았는데 그 값이 `WBFTExtra` 로 디코드되지 않으면, proposer 는 반드시 proposal 만들기를 실패로 끝내야 한다. 이때 proposer 는 그 요청에 대해 proposal 을 만들지 않는다.
Source: `consensus/wbft/engine/engine.go:1163, 1181`, `consensus/wbft/engine/engine.go:544-546`, `params/protocol_params.go:31`

### 3.7 Gas tip 넣기

[WBFT-HDR-020] proposer 는 반드시 `Extra.GasTip` 을 parent state 에서 애플리케이션의 gas tip 조회가 돌려준 값으로 두어야 한다 (`A-09 §4.3`, 값 규칙은 `B-06`). 모든 proposal 에서 `Extra.GasTip` 은 반드시 `nil` 이 아니어야 한다.
Source: `consensus/wbft/engine/engine.go:511-513, 518, 537, 542, 599-604, 622-645`
Observable: header

[WBFT-HDR-021] gas tip 조회가 실패하면 proposer 는 그 요청에 대해 proposal 을 만들어서는 안 된다. 조회가 실패하는 경우는 system contract 설정이 없을 때, parent 가 없을 때, parent `Root` 가 zero hash 일 때, parent state 를 쓸 수 없을 때다.
Source: `consensus/wbft/engine/engine.go:511-513, 622-638`

> 해설: 같은 조회라도 만들 때와 검증할 때 오류를 다르게 다룬다. proposal 을 만들 때는 모든 오류가 proposal 중단으로 이어진다. 검증할 때는 값 불일치만 실패로 보고 나머지 오류는 건너뛴다 (`WBFT-HDR-111`). 구현자는 이 비대칭을 그대로 재현해야 한다.

### 3.8 이전 block 의 seal

`n >= 2` 이면 proposal 은 높이 `n-1` 의 block 이 finalize 될 때의 round 와 aggregated seal 을 싣는다. 이 seal 은 *extra seal* 로 늘어날 수 있다. extra seal 은 quorum 이 모인 뒤에 도착한 seal 이다 (`A-05`).

[WBFT-HDR-030] `n >= 2` 이면 proposer 는 반드시 이전 block 의 seal 을, 자기 chain 이 번호 `n-1` 의 canonical block 으로 저장한 header 에서 가져와야 한다. 이 header 는 `ParentHash` 로 식별되는 header 가 아니라 **번호로** 찾는다. 정상 동작에서는 두 header 가 같다. 두 header 는 parent 를 고른 시점과 이 조회 사이에 canonical chain 이 바뀐 경우에만 다르다.
Source: `consensus/wbft/engine/engine.go:515-517`

[WBFT-HDR-031] `n >= 2` 인데 `n-1` 의 header 에 `PreparedSeal` 이 없거나 `CommittedSeal` 이 없으면, proposer 는 반드시 각각 `ErrEmptyPreparedSeals` 또는 `ErrEmptyCommittedSeals` 로 실패하고 proposal 을 만들지 않아야 한다. 검사 순서는 `PreparedSeal` 이 먼저다.
Source: `consensus/wbft/engine/engine.go:520-530`

[WBFT-HDR-032] `n >= 2` 이면 proposer 는 반드시 `Extra.PrevRound = Extra(n-1).Round`, `Extra.PrevPreparedSeal = merge_seals(Extra(n-1).PreparedSeal, extra_prepared)`, `Extra.PrevCommittedSeal = merge_seals(Extra(n-1).CommittedSeal, extra_committed)` 로 두어야 한다. 여기서 `Extra(n-1)` 은 proposer 가 가진 `n-1` header 의 **로컬 사본** 이다. 사본은 노드마다 다를 수 있다 (`WBFT-HDR-053` 참조).
Source: `consensus/wbft/engine/engine.go:532-537, 583-590`
Observable: header

[WBFT-HDR-033] `n = 1` 이고 block builder vanity 가 32바이트보다 짧으면 proposer 는 반드시 `PrevRound = 0`, `PrevPreparedSeal = nil`, `PrevCommittedSeal = nil` 로 남겨 두어야 한다. 32바이트 vanity 의 경우는 §3.6 이 정한다.
Source: `consensus/wbft/engine/engine.go:82-84, 539-543`
Observable: header

#### 3.8.1 `merge_seals`

```python
def merge_seals(seal: AggregatedSeal, extra: list[tuple[uint32, bytes]]) -> AggregatedSeal:
    if len(extra) == 0:
        return seal                                   # same object, unchanged
    sigs = [seal.signature]                           # the existing aggregate, compressed
    sealers = copy(seal.sealers)                      # same byte length as the input bitmap
    for (index, sig) in extra:                        # order irrelevant: aggregation is commutative
        if sealers.is_sealer(index):
            continue                                  # never double-count an index
        sealers.set_sealer(index)                     # may extend the bitmap by whole bytes
        sigs.append(sig)
    try:
        agg = bls_aggregate_compressed(sigs)          # group-checks every input (A-02)
    except Error:
        return seal                                   # fallback: all extra seals discarded
    return AggregatedSeal(sealers=sealers, signature=compress(agg))
```

Source: `consensus/wbft/engine/engine.go:1371-1397`, `core/types/istanbul.go:290-313`, `crypto/bls/blst/signature.go:70-77`

[WBFT-HDR-034] proposer 는 반드시 병합된 이전 seal 을 `merge_seals` 와 똑같이 계산해야 한다. 입력 bitmap 에 이미 있는 index 는 건너뛴다. 새 index 는 bitmap 에 켜고, 그 signature 를 기존 aggregate 에 합친다. 어느 입력에서든 aggregate 가 실패하면 결과는 반드시 변경되지 않은 입력 seal 이어야 한다. 즉 부분 병합은 없다.
Source: `consensus/wbft/engine/engine.go:1371-1397`
Observable: header

[WBFT-HDR-035] `merge_seals` 에 넘기는 extra seal 은 반드시 다음 두 조건을 모두 만족하는 seal 뿐이어야 한다.

1. 합의 코어가 view `(head.Number, prior_round)` 에 대해 기록한 seal 이다.
2. seal 의 digest 가 `block_hash(head)` 와 같다.

여기서 `head` 는 이 호출 시점의 chain head 이며, 보통은 block `n-1` 이다. 노드는 각 seal 을 이전 validator 집합 안의 index 로 대응시키고, 그 집합에 없는 sender 의 seal 은 버린다. 합의 코어가 돌고 있지 않으면 두 목록은 모두 비어 있다.
Source: `consensus/wbft/core/extraseal.go:133-183`, `consensus/wbft/core/core.go:133-139`, `consensus/wbft/backend/engine.go:221-229`

`merge_seals` 는 병합한 aggregate 를 검증하지 않는다. 각 extra seal 은 받을 때 이전 proposal 의 header 와 그 메시지의 round 로 하나씩 검증되었다 (`consensus/wbft/core/extraseal.go:50-82`). 그런데 `prior_round` 가 `Extra(n-1).Round` 와 다를 수 있다. 이 노드가 다른 round 에 있는 동안 block `n-1` 을 import 했다면 두 값이 다르다. 그러면 병합한 aggregate 는 서로 다른 두 `seal_data` 값에 대한 signature 를 섞게 되고, 그 결과로 만든 proposal 은 모든 verifier 에서 단계 H20 에 실패한다. 그 뒤 그 높이는 round change 로 진행된다.

[WBFT-HDR-036] proposer 는 병합한 이전 seal 이 검증에 실패하는 proposal 을 만들어도 된다. 참조 구현은 병합 결과를 검사하지 않는다. verifier 는 그런 proposal 을 단계 H20 에서 `ErrInvalidPrevPreparedSeals` 또는 `ErrInvalidPrevCommittedSeals` 로 거부한다.
Source: `consensus/wbft/engine/engine.go:532-537, 1371-1397`, `consensus/wbft/core/backlog.go:155-162`, `consensus/wbft/core/priorstate.go:35-45`

### 3.9 Mix digest

[WBFT-HDR-040] proposer 는 반드시 `MixDigest = randao_mix(parent.MixDigest, Extra.RandaoReveal)` 로 두어야 한다. 여기서 `parent` 는 `ParentHash` 로 식별되는 header 다.
Source: `consensus/wbft/engine/engine.go:547, 575-581`
Observable: header

> 해설: 이전 seal(`WBFT-HDR-030`)은 번호로 찾지만, `MixDigest` 의 parent 는 hash 로 찾는다.

### 3.10 실행 뒤에 채우는 필드

transaction 을 실행한 뒤 `process_finalize`(`B-06`)가 header 를 완성한다.

- epoch block 이면 `process_finalize` 는 `Extra.EpochInfo` 를 실행 뒤 state 에서 계산해서 쓴다. 다만 노드가 `validators_at(n)` 의 member 일 때만 쓰고, member 가 아니면 `nil` 로 남긴다. `nil` 로 남긴 proposal 은 무효이지만, member 가 아닌 노드는 proposal 을 낼 수 없으므로 해가 없다.
- `process_finalize` 는 `Root` 를 finalization 뒤의 state 로 정하고, `UncleHash` 를 `EMPTY_UNCLE_HASH` 로 둔다.

Source: `consensus/wbft/engine/engine.go:929-970, 1193-1207, 1053-1061`

[WBFT-HDR-041] 노드는 proposal 의 digest 로 반드시 `process_finalize` *뒤의* header 의 `block_hash` 를 써야 한다. 이 digest 는 `EpochInfo`, `GasTip`, `RandaoReveal`, `PrevRound`, 이전 seal 을 포함하고, `Round`, `PreparedSeal`, `CommittedSeal` 은 포함하지 않는다.
Source: `core/types/block.go:121-131`, `core/types/istanbul.go:263-288`, `miner/worker.go:1401`
Observable: header

### 3.11 Block builder 가 소유하는 header 골격 필드

[WBFT-HDR-042] proposer 는 반드시 `ParentHash = block_hash(parent)`, `Number = parent.Number + 1` 로 두어야 한다.
Source: `miner/worker.go:1142-1145`
Observable: header

---

## 4. Finalized header: seal 쓰기

합의 코어가 round `r` 에서 proposal `B` 에 대한 COMMIT quorum 을 모으면, `B` 의 header extra data 에 세 필드를 써서 finalized header 를 만든다. *언제* 쓰는지는 `A-05` 가 정하고, 이 절은 *무엇을* 쓰는지를 정한다.

```python
def commit_header(header, prepared: list[SealData], committed: list[SealData], round: int):
    x = decode_extra(header.Extra)                    # len(Extra) >= 32 here
    if len(prepared) == 0: raise ErrInvalidPreparedSeals
    x.PreparedSeal = aggregate_seals(prepared)
    if len(committed) == 0: raise ErrInvalidCommittedSeals
    x.CommittedSeal = aggregate_seals(committed)
    x.Round = uint32(round mod 2**64)                 # truncation to uint32 (A-03)
    header.Extra = encode_extra(x)

def aggregate_seals(seals: list[SealData]) -> AggregatedSeal:
    sealers = SealerSet()
    sigs = []
    for s in seals:
        if len(s.seal) != SEAL_LENGTH: raise ErrInvalidSeal
        sealers.set_sealer(s.sealer)
        sigs.append(s.seal)
    return AggregatedSeal(sealers, compress(bls_aggregate_compressed(sigs)))   # raises if any input fails the group check
```

Source: `consensus/wbft/engine/engine.go:90-158`, `consensus/wbft/backend/backend.go:213-230`, `consensus/wbft/core/commit.go:137-179`

[WBFT-HDR-050] round `r` 에서 proposal `B` 를 decide 하고 `finalize` (`A-09` §4.7) 로 넘기는 노드는 반드시 다음 세 필드를 쓰고, 다른 header 필드는 모두 그대로 두어야 한다. `Extra.PreparedSeal` 에는 `(n, r)` 에 대해 가진 PREPARE seal 의 aggregate 를 쓴다. `Extra.CommittedSeal` 에는 `(n, r)` 에 대해 가진 COMMIT seal 의 aggregate 를 쓴다. `Extra.Round` 에는 `r` 의 하위 32비트를 쓴다.
Source: `consensus/wbft/engine/engine.go:90-98`, `consensus/wbft/core/commit.go:143-173`
Observable: header

[WBFT-HDR-051] seal 을 써도 block hash 가 바뀌어서는 안 된다: `block_hash(commit_header(B)) == block_hash(B)`.
Source: `core/types/block.go:121-131`, `core/types/istanbul.go:269-288`, `consensus/wbft/backend/backend.go:229, 239`
Observable: header

[WBFT-HDR-052] seal 목록 가운데 하나가 비어 있거나, 어떤 seal 이 96바이트가 아니거나, 한 목록의 BLS aggregate 가 실패하면, seal 쓰기(`finalize`, `A-09` §4.7)는 반드시 `ErrInvalidPreparedSeals`, `ErrInvalidSeal`, BLS aggregate 오류, `ErrInvalidCommittedSeals` 가운데 하나로 실패해야 한다. 검사 순서는 `commit_header` 의 순서를 따른다.

1. prepared 목록이 비어 있는지 검사한다.
2. prepared seal 의 길이를 검사한다.
3. prepared seal 을 aggregate 한다.
4. committed 목록이 비어 있는지 검사한다.
5. committed seal 의 길이를 검사한다.
6. committed seal 을 aggregate 한다.

seal 쓰기가 실패하면 합의 코어는 round `r+1` 의 ROUND-CHANGE 를 broadcast 한다 (`A-05`). 합의 코어는 모든 seal 을 96바이트 버퍼에 복사하므로, 길이 오류는 validator 집합에 없는 source 가 남긴 빈 `SealData` 에서만 생긴다.
Source: `consensus/wbft/engine/engine.go:101-158`, `crypto/bls/blst/signature.go:70-77`, `consensus/wbft/core/commit.go:149-154, 164-169, 173-176`

Informative. 참조 구현은 quorum 에 도달하는 순간 COMMIT aggregate 를 쓴다. 그래서 COMMIT aggregate 에는 보통 정확히 `quorum_size(N)` 명의 sealer 가 들어 있다. PREPARE aggregate 도 마찬가지다. PREPARE quorum 뒤에 도착한 PREPARE 는 extra seal 로 처리되기 때문이다 (`A-05` WBFT-SM-048). 더 늦게 도착한 seal 은 extra seal 이 되고, block `n+1` 의 이전 seal 필드에 병합되어 나타난다 (§3.8).

[WBFT-HDR-053] finalized header 의 `Round`, `PreparedSeal`, `CommittedSeal` 값은 **노드 로컬** 이다. 각 노드는 자기가 모은 seal 을 쓰고, 노드가 저장하는 사본은 그 노드가 처음 import 한 sealed 사본이다. 적합한 두 노드가 hash 는 같고 이 세 필드의 값은 다른 header 를 저장해도 된다. verifier 나 observer 는 노드 사이에서 이 필드가 다르다는 것을 결함으로 다뤄서는 안 된다. 또 verifier 나 observer 는 자기가 가진 block `n` 사본을 기준으로 `PrevRound(n+1) == Round(n)` 이나 `PrevPreparedSeal(n+1).sealers ⊇ PreparedSeal(n).sealers` 를 요구해서는 안 된다.
Source: `consensus/wbft/backend/backend.go:221-249`, `consensus/wbft/engine/engine.go:515-537`, `eth/fetcher/block_fetcher.go:790-799`, `core/blockchain.go:1605-1624`
Observable: header, rpc

---

## 5. PRE-PREPARE 에서의 proposal 검증

PRE-PREPARE 가 `A-05` 의 메시지 검사를 통과하면, 노드는 제안된 block 을 `verify_proposal_header` 로 검증한다. `A-05` 의 메시지 검사는 sender 가 그 view 의 proposer 인지, `Sequence` 가 proposal 번호와 같은지, `round > 0` 이면 justification 이 있는지를 본다. 노드는 block body 에서 transaction root 와 uncle 만 검사하고, transaction 은 **실행하지 않는다** (`A-09 §4.6`).

```python
def verify_proposal_header(block) -> (Duration, Error | None):
    # P1
    if not is_block(block):                      return 0, ErrInvalidProposal
    # P2
    if app.is_bad_block(block_hash(block.header)): return 0, ErrBlacklistedHash
    # P3
    V, Vp = validators_for_verifying(block.header, parents=[])   # errors: §6.1
    # P4 [B-03]
    if derive_sha(block.transactions) != block.header.TxHash:   return 0, ErrMismatchTxhashes
    # P5 [B-03]
    if calc_uncle_hash(block.uncles) != EMPTY_UNCLE_HASH:         return 0, ErrInvalidUncleHash
    # P6
    err = verify_header(block.header, parents=[], V, Vp, check_seals=False)
    if err == ErrFutureBlock:
        return block.header.Time - now_precise(), ErrFutureBlock
    if err:                                                     return 0, err
    # P7
    if app.header_by_hash(block.header.ParentHash) is None:     return 0, "unknown parent hash"
    return 0, None
```

Source: `consensus/wbft/backend/backend.go:258-278`, `consensus/wbft/engine/engine.go:160-186`, `consensus/wbft/core/preprepare.go:147-169`

[WBFT-HDR-060] 노드는 반드시 proposal 을 단계 P1–P7 순서로 검증해야 하고, 반드시 처음 실패한 단계를 결과로 다뤄야 한다.
Source: `consensus/wbft/backend/backend.go:258-278`, `consensus/wbft/engine/engine.go:160-186`
Observable: network

[WBFT-HDR-061] 노드는 hash 가 bad block 으로 기록된 proposal 을 반드시 `ErrBlacklistedHash` 로 거부해야 한다. 노드는 type 검사를 뺀 다른 모든 검사보다 먼저 이 거부 검사를 한다.
Source: `consensus/wbft/backend/backend.go:266-270`, `core/blockchain.go:2422-2425`
Observable: network

> 해설: 이 검사는 finalize 뒤에 실행에 실패한 block 을 다시 PREPARE 하지 않게 하는 장치다. bad block 기록의 보관 한도와 복구 절차는 `A-09` §5.3 과 `A-10 §9` 가 다룬다.

[WBFT-HDR-063] `verify_header` 가 `ErrFutureBlock` 을 돌려주면, proposal 검증은 반드시 `ErrFutureBlock` 과 함께 기간 `time.Until(Unix(int64(Time)))` 을 돌려주어야 하고, 노드는 반드시 그 기간이 지난 뒤 같은 PRE-PREPARE 를 다시 처리해야 한다 (`A-05`, `A-06`). `Time < 2^63` 이면 이 기간은 초 미만 단위까지 계산한 `Time − now` 이고, Go `Duration` 의 최댓값(약 292년)에서 포화한다. 그 밖의 상한은 없다. `Time ≥ 2^63` 이면 `int64` 변환이 넘쳐서 기간이 음수가 된다. 그러면 노드는 PRE-PREPARE 를 즉시 다시 처리하고, H2 가 계속 실패하므로 같은 처리를 되풀이한다.
Source: `consensus/wbft/engine/engine.go:174-175`, `consensus/wbft/core/preprepare.go:150-163`
Observable: network

proposal 검증이 검사하지 **않는** 것(informative): state root, receipts root, bloom, 실행 기준의 gas used, transaction 의 EVM 유효성, `EpochInfo` 내용, block 자신의 aggregated seal. 노드는 이 항목들을 finalized block 을 import 할 때에만 검사한다 (`A-09 §5`). 그래서 P1–P7 을 통과한 proposal 도 finalize 뒤에 bad block 이 될 수 있다 (`A-10 §9`).

---

## 6. Header 검증

참조 구현에서 모든 header 검증은 `verify_header` 라는 절차 하나를 거친다:

| 호출자 | `check_seals` | `parents` | 위치 |
|---|---|---|---|
| proposal 검증 (§5) | false | `[]` | `consensus/wbft/engine/engine.go:173` |
| 단일 header (`VerifyHeader`): block fetcher, 시작 시점 | true | `[]` | `consensus/wbft/backend/engine.go:71-81`, `eth/handler.go:245`, `core/blockchain.go:424` |
| batch (`VerifyHeaders`): `InsertChain`, header chain 삽입(snap sync) | true | batch 안의 앞선 header | `consensus/wbft/backend/engine.go:87-112`, `core/blockchain.go:1587`, `core/headerchain.go:327` |

### 6.1 검증에 쓰는 validator 집합

```python
def validators_for_verifying(header, parents) -> (ValidatorSet, ValidatorSet):
    # V0a
    try:
        V = validators_at(header.Number, header.ParentHash, parents)   # for Number != 0 the epoch is
                                                     # found from uint64(Number) - 1 (wraps for k*2^64)
    except Error:
        raise ErrUnknownAncestor                     # every error is mapped
    # V0b
    if uint64(header.Number) >= 2:
        parent = parents[-1] if parents else chain.header(header.ParentHash, uint64(header.Number) - 1)
        if parent is None:
            raise ErrUnknownAncestor
        Vp = validators_at(parent.Number, parent.ParentHash, parents[:-1])   # errors returned as-is
    else:
        Vp = V
    return V, Vp
```

Source: `consensus/wbft/backend/engine.go:420-450`, `consensus/wbft/engine/engine.go:1097-1117, 1407-1436` (`uint64` at `:1408`, `backend/engine.go:429-431`)

[WBFT-HDR-070] verifier 는 반드시 다른 header 검사보다 먼저 header 높이의 validator 집합 `V` 와 parent 높이의 집합 `Vp` 를 정해야 하고, `V` 를 정하지 못한 모든 실패를 반드시 `ErrUnknownAncestor` 로 보고해야 한다.
Source: `consensus/wbft/backend/engine.go:76-79, 425-427`

[WBFT-HDR-071] verifier 는 `V` 를 정의하는 epoch header 를 찾을 때, 반드시 먼저 epoch block 번호에 canonical 로 저장된 header 를 써야 한다. 그런 header 가 저장되어 있지 않을 때에만, 검증 중인 header 에서 시작해 `parents` 를 거쳐 `ParentHash` 연결을 따라 거슬러 올라간다.
Source: `consensus/wbft/engine/engine.go:1407-1436`

### 6.2 순서가 있는 절차

```python
def verify_header(header, parents, V, Vp, check_seals) -> Error | None:
    # ---- verifyHeader: context-free and fork checks ----
    H1  [A]  if header.Number is None:                         return ErrUnknownBlock
    H2  [A]  if header.Time > now() + AllowedFutureBlockTime:  return ErrFutureBlock
    H3  [B-03] if header.UncleHash != EMPTY_UNCLE_HASH:          return ErrInvalidUncleHash
    H4  [A]  if header.Difficulty != 1:                        return ErrInvalidDifficulty
    H5  [B-03] if header.GasLimit > 2**63 - 1:                 return "invalid gasLimit"
    H6  [B-03] if is_shanghai(header.Number, header.Time):     return "wbft does not support shanghai fork"   # SNET-BHDR-003
    H7  [B-03] if header.WithdrawalsHash is not None:          return "invalid withdrawalsHash"
    H8  [B-03] if is_cancun(header.Number, header.Time):       return "wbft does not support cancun fork"     # SNET-BHDR-005
    H9  [B-03] ExcessBlobGas, then BlobGasUsed, then ParentBeaconRoot must be None
    # ---- verifyCascadingFields ----
    H10 [A]  n = uint64(header.Number); if n == 0:             return None      # genesis
    H11 [A]  parent = parents[-1] if parents else chain.header(header.ParentHash, n - 1)
             if parent is None or uint64(parent.Number) != n - 1 or block_hash(parent) != header.ParentHash:
                                                               return ErrUnknownAncestor
    H12 [A]  if parent.Time + config_at(header.Number).BlockPeriod > header.Time:
                                                               return ErrInvalidTimestamp
    H13 [B-03] if header.GasUsed > header.GasLimit:            return "invalid gasUsed"
    H14 [B-03] if not is_london(header.Number): BaseFee must be None, gas-limit bounds against parent
               else: EIP-1559 header rules (gas limit bounds, base fee)
    H15a [A] if header.Coinbase not in V:                      return ErrUnauthorized
    H15b [B-08] state = state_at(parent.Root)                  # SNET-SRC-020
             if unavailable:                                   skip (continue at H16)
             if is_blacklisted(state, header.Coinbase):       return ErrBlacklistedSigner
    H16 [A]  x = decode_extra(header.Extra) or                 return ErrInvalidExtraDataFormat
    H17 [A]  if check_seals: verify_seals(header, V, x)        # §6.4
    H18 [A]  if ecdsa_recover_address(randao_data(chain_id, header.Number), x.RandaoReveal) != header.Coinbase:
                                                               return "failed to verify randao reveal signature: ..."
    H19 [A]  if randao_mix(parent.MixDigest, x.RandaoReveal) != header.MixDigest:
                                                               return "invalid randao mix: ..."
    H20 [A]  if header.Number > 1: verify_prev_seals(parent, Vp, x)   # §6.5
    H21 [B-06] gas tip check at parent state; skipped unless the error is a mismatch
             or ErrGasTipContractUnavailable (§6.6; the latter is unreachable, WBFT-HDR-111);
             the parent is re-read from the chain by (ParentHash, uint64(Number) - 1), `parents` is ignored
    return None
```

Source: `consensus/wbft/engine/engine.go:196-350` (H1 `:197-199`, H2 `:201-205`, H3 `:208-210`, H4 `:213-215`, H5 `:217-219`, H6 `:220-222`, H7 `:224-226`, H8 `:227-229`, H9 `:231-238`, H10 `:250-253`, H11 `:256-265`, H12 `:270-272`, H13 `:274-276`, H14 `:277-288`, H15 `:291-298, 352-381`, H16 `:301-304`, H17 `:307-311`, H18 `:313-315`, H19 `:316-319`, H20 `:322-327`, H21 `:329-348`)

[WBFT-HDR-080] verifier 는 반드시 단계 H1–H21 을 주어진 순서대로 적용해야 하고, 반드시 처음 실패한 단계의 오류를 돌려주어야 한다. [B-xx] 로 표시된 단계는 이름이 적힌 Part B 장이 정하고, 순서상 위치는 이 장이 정한다. H6 과 H8 은 `B-03` 의 SNET-BHDR-003 과 SNET-BHDR-005 이고, 두 단계가 평가하는 fork 판정식은 `B-01` 이 정의한다.
Source: `consensus/wbft/engine/engine.go:196-350`
Observable: header

[WBFT-HDR-081] (H2) `Time > now() + AllowedFutureBlockTime` 이면 verifier 는 반드시 `ErrFutureBlock` 을 돌려주어야 한다. 여기서 `AllowedFutureBlockTime` 은 노드에 설정된 값이다. 이 값은 transition 에 따라 바뀌지 않으며 기본값은 `0` 이다. `verify_header` 안에서 이 검사는 H1 을 뺀 다른 모든 검사보다 앞선다. 그러나 validator 집합 조회(V0a, V0b, §6.1)는 이 검사보다 먼저 실행되고, proposal 검증에서는 단계 P1–P5 도 먼저 실행된다. 그래서 그 단계들의 실패는 future header 에 대해서도 기다리지 않고 바로 보고된다. 예를 들어 parent 가 없으면 V0b 가 `uint64(Number) ≥ 2` 에서 `ErrUnknownAncestor` 를 보고한다.
Source: `consensus/wbft/engine/engine.go:201-205`, `consensus/wbft/config.go:110, 116-122`, `consensus/wbft/backend/engine.go:76-79, 429-440`, `consensus/wbft/backend/backend.go:273-277`
Observable: header

[WBFT-HDR-082] (H4) verifier 는 `Difficulty` 가 없거나 `1` 이 아닌 header 를 반드시 `ErrInvalidDifficulty` 로 거부해야 한다.
Source: `consensus/wbft/engine/engine.go:213-215`
Observable: header

[WBFT-HDR-083] (H10) verifier 는 `uint64(Number) = 0` 인 header 를 H1–H9 가 통과하면 반드시 받아들여야 하며, 이때 extra data, parent, timestamp, signer 를 검사하지 않는다. `uint64(Number) = 0` 은 `Number ≡ 0 mod 2^64` 라는 뜻이고 genesis 번호 0 도 포함한다. 이 규칙은 `V` 와 `Vp` 가 주어졌을 때의 `verify_header` 규칙이다. 노드에서는 `uint64(Number) = 0` 이고 `Number ≠ 0` 인 header 의 `V` 를 정할 수 없다 (V0a 가 `ErrUnknownAncestor` 를 돌려준다). 그런 번호의 proposal 은 `Sequence = Number` 검사에서 실패한다. 그래서 노드의 모든 경로에서 그런 header 는 H10 에 닿기 전에 거부되고, observer 가 보는 H10 적용은 genesis header 뿐이다. genesis 의 유효성은 `B-02` 가 정한다. genesis block 은 header 검증을 거쳐 import 되는 일이 없다. 참조 구현은 새 데이터베이스의 현재 header 인 genesis 를 시작할 때에만 `verify_header` 에 넣고, 그 결과를 무시한다 (`core/blockchain.go:424`). 그래서 `Difficulty = 0` 인 testnet genesis(`B-02`)처럼 H1–H9 에 실패하는 genesis 도 그대로 쓰인다.
Source: `consensus/wbft/engine/engine.go:250-253`
Observable: header

[WBFT-HDR-084] (H11) `Number ≥ 1` 이면 verifier 는 반드시 parent 를 다음과 같이 얻어야 한다. batch 가 주어지면 batch 의 마지막 header(`parents[-1]`)를 쓰고, batch 가 없으면 자기 chain 에서 `(ParentHash, Number − 1)` 로 찾는다. parent 가 없거나, parent 의 번호가 `Number − 1` 이 아니거나, parent 의 hash 가 `ParentHash` 가 아니면 verifier 는 반드시 `ErrUnknownAncestor` 를 돌려주어야 한다.
Source: `consensus/wbft/engine/engine.go:256-265`
Observable: header

[WBFT-HDR-085] (H12) `parent.Time + config_at(Number).BlockPeriod > Time` 이면 verifier 는 반드시 `ErrInvalidTimestamp` 를 돌려주어야 한다. 이때 쓰는 block period 는 child 높이에서 유효한 값이다. 그래서 transition 이 block `k` 에서 `BlockPeriod` 를 바꾸면 새 값은 `k−1` 과 `k` 사이의 간격에 적용된다.
Source: `consensus/wbft/engine/engine.go:267-272`
Observable: header

[WBFT-HDR-086] (H15a) `Coinbase` 가 `V` 에 있는 주소가 아니면 verifier 는 반드시 `ErrUnauthorized` 를 돌려주어야 한다.
Source: `consensus/wbft/engine/engine.go:365-368`
Observable: header

[WBFT-HDR-088] (H16) `Extra` 가 `WBFTExtra` 로 디코드되지 않으면 verifier 는 반드시 `ErrInvalidExtraDataFormat` 을 돌려주어야 한다. `A-03` 에 따르면 `WBFTExtra` 인코딩은 list 요소가 정확히 열 개이고 뒤에 남는 바이트가 없어야 한다.
Source: `consensus/wbft/engine/engine.go:301-304`, `core/types/istanbul.go:122-149, 251-258`
Observable: header

[WBFT-HDR-089] (H18) verifier 는 §6.2 절차의 단계 H18 에서, 즉 단계 H17 뒤와 단계 H19 의 randao mix 검사(WBFT-HDR-090) 앞에서, 반드시 `A-02` §7.2 의 randao reveal 검사를 적용해야 한다. 오류 메시지는 `failed to verify randao reveal signature:` 로 시작한다.
Source: `consensus/wbft/engine/engine.go:313-315, 563-573`, `consensus/wbft/backend/backend.go:292-303`, `consensus/wbft/utils.go:39-48`
Observable: header

[WBFT-HDR-090] (H19) `MixDigest` 가 `randao_mix(parent.MixDigest, RandaoReveal)` 와 다르면 verifier 는 반드시 그 header 를 거부해야 한다. 오류 메시지는 `invalid randao mix:` 로 시작한다.
Source: `consensus/wbft/engine/engine.go:316-319, 575-581`
Observable: header

[WBFT-HDR-091] (H20) `Number ≥ 2` 이면 verifier 는 반드시 이전 block 의 seal 을 검증해야 한다 (§6.5). `Number = 1` 이면 verifier 는 `PrevRound`, `PrevPreparedSeal`, `PrevCommittedSeal` 을 검사해서는 안 된다. 그래서 verifier 는 이 세 필드에 어떤 값이 있어도 받아들인다.
Source: `consensus/wbft/engine/engine.go:82-84, 321-327`
Observable: header

> 해설: 단계별 흐름을 문장으로 풀면 다음과 같다. H1–H9 는 header 하나만 보고 판단할 수 있는 검사이고, 그 가운데 H2 가 거의 맨 앞에 있다. 그래서 future header 는 다른 결함이 있어도 먼저 "나중에 다시 검사할 header" 로 분류된다. H10 에서 번호가 0 이면 검증이 끝난다. H11 부터는 parent 가 필요하다. H16 을 지나야 `Extra` 를 읽을 수 있으므로 H17–H20 은 H16 뒤에 온다. parent **state** 를 읽는 단계는 H15b 와 H21 둘뿐이다 (§6.6).

#### 6.2.1 Number 범위

### 6.3 Aggregated seal 검증

이 절차는 `A-02` §5.7 (WBFT-CRYPTO-030) 의 `verify_aggregated_seal(V, header, round, agg, seal_type)` 이다. 이 장은 그 절차를 다시 적지 않고 그대로 쓴다. header 검증은 이 절차를 두 단계에서 적용한다. `verify_seals`(§6.4)는 block 자신의 prepared seal 과 committed seal 을 그 block height 의 집합 `V` 로 검사하고, `verify_prev_seals`(§6.5)는 이전 seal 을 parent height 의 집합 `Vp` 로 검사한다. 이 절차 안의 실패는 그 절차를 부른 단계의 바깥 오류로 보고한다(WBFT-HDR-102). 그 두 단계에서 verifier 가 이 절차에 대해 관측할 수 있는 것은 WBFT-HDR-100 과 WBFT-HDR-101 이 정한다.

[WBFT-HDR-100] verifier 는 반드시 aggregated seal 이 다음 세 조건을 모두 만족할 때에만 그 seal 을 받아들여야 한다.

1. bitmap 에 켜진 서로 다른 index 의 수가 적어도 `quorum_size(|vs|)` 이다.
2. 모든 index 가 `|vs|` 보다 작다.
3. index 로 가리킨 validator 들의 BLS public key 합이 무한원점이 아니고, signature 가 그 합 아래에서 `seal_data(header, round, seal_type)` 에 대한 단일 BLS signature 로 verify 된다 (`A-02` WBFT-CRYPTO-055).

가리킨 key 의 합이 무한원점이면 verifier 는 반드시 그 seal 을 `ErrInvalidSeal` 로 거부해야 한다. signature 도 무한원점이어서 pairing 등식이 자명하게 성립하는 경우에도 마찬가지다. verifier 는 가장 높은 켜진 비트보다 뒤에 0 바이트가 더 붙은 bitmap 도 반드시 받아들여야 한다.
Source: `consensus/wbft/engine/engine.go:1338-1369`, `core/types/istanbul.go:315-325`, `crypto/bls/blst/signature.go:119-122`, blst v0.3.16 `src/aggregate.c:294-297`
Observable: header

[WBFT-HDR-101] sealer bitmap 의 index `i` 는 반드시 검사에 넘긴 집합의 `i` 번째 validator 로 해석해야 한다. 이 집합은 정의하는 epoch block 의 `EpochInfo.Validators` 순서를 따르며, 다시 정렬하지 않는다.
Source: `consensus/wbft/validator/validator.go:35-41`, `consensus/wbft/validator/default.go:71-89, 113-120`, `consensus/wbft/engine/engine.go:1115`
Observable: header

### 6.4 Block 자신의 seal (`verify_seals`)

```python
def verify_seals(header, V, x):
    if header.Number == 0: return None                         # unreachable after H10
    if x.PreparedSeal is None or len(x.PreparedSeal.signature) == 0:  raise ErrEmptyPreparedSeals
    if verify_aggregated_seal(V, header, x.Round, x.PreparedSeal, PREPARE_SEAL):  raise ErrInvalidPreparedSeals
    if x.CommittedSeal is None or len(x.CommittedSeal.signature) == 0: raise ErrEmptyCommittedSeals
    if verify_aggregated_seal(V, header, x.Round, x.CommittedSeal, COMMIT_SEAL):  raise ErrInvalidCommittedSeals
```

Source: `consensus/wbft/engine/engine.go:414-449`

[WBFT-HDR-102] `check_seals` 가 true 이면 verifier 는 반드시 다음 순서로 검사해야 한다.

1. `PreparedSeal` 이 있고 signature 가 비어 있지 않다 (`ErrEmptyPreparedSeals`).
2. `PreparedSeal` 이 `(V, header, Extra.Round, PREPARE_SEAL)` 에 대해 유효하다 (`ErrInvalidPreparedSeals`).
3. `CommittedSeal` 이 있고 signature 가 비어 있지 않다 (`ErrEmptyCommittedSeals`).
4. `CommittedSeal` 이 `(V, header, Extra.Round, COMMIT_SEAL)` 에 대해 유효하다 (`ErrInvalidCommittedSeals`).

verifier 는 `verify_aggregated_seal` 의 내부 실패를 모두 반드시 그 실패가 일어난 단계의 바깥 오류로 보고해야 한다.
Source: `consensus/wbft/engine/engine.go:414-449`
Observable: header

> 해설: 내부 실패 이유는 `lack of seal count`, `sealer is not validator`, `ErrInvalidSeal` 이고, 이 이유들은 `ErrInvalidPreparedSeals` 같은 바깥 오류로 감싸져 보고된다. 그래서 바깥 오류만 보고서는 어느 조건이 실패했는지 알 수 없다.

### 6.5 이전 block 의 seal (`verify_prev_seals`)

```python
def verify_prev_seals(parent, Vp, x):
    if parent.Number == 0: return None                         # parent is the genesis header: previous seals are not checked
    if x.PrevPreparedSeal is None or len(x.PrevPreparedSeal.signature) == 0:   raise ErrEmptyPrevPreparedSeals
    if verify_aggregated_seal(Vp, parent, x.PrevRound, x.PrevPreparedSeal, PREPARE_SEAL):   raise ErrInvalidPrevPreparedSeals
    if x.PrevCommittedSeal is None or len(x.PrevCommittedSeal.signature) == 0: raise ErrEmptyPrevCommittedSeals
    if verify_aggregated_seal(Vp, parent, x.PrevRound, x.PrevCommittedSeal, COMMIT_SEAL):   raise ErrInvalidPrevCommittedSeals
```

Source: `consensus/wbft/engine/engine.go:384-411`

[WBFT-HDR-103] `Number ≥ 2` 이면 verifier 는 반드시 header `h` 의 이전 block seal 을 다음 세 값으로 검사해야 한다. 첫째는 H11 에서 찾은 **parent header** 이며, verifier 가 가진 parent 안의 seal 사본이 아니다. 둘째는 parent 의 validator 집합 `Vp` 이고, 셋째는 round `h.Extra.PrevRound` 다. 검사 순서와 오류는 `verify_prev_seals` 를 따른다. `seal_data` 는 parent 에서 seal 과 round 를 지우고 `PrevRound` 를 넣어 계산한다. 그래서 parent 자신의 `Round`, `PreparedSeal`, `CommittedSeal` 은 결과에 영향을 주지 않는다.
Source: `consensus/wbft/engine/engine.go:322-327, 384-411`, `core/types/istanbul.go:269-288`
Observable: header

이전 seal 은 seal 집합이 block hash 안에 commit 되는 유일한 곳이다. block `h` 의 `PrevPreparedSeal` 과 `PrevCommittedSeal` 은 `block_hash(h)` 에 포함된다 (`WBFT-HDR-041`). 그래서 두 필드는 모든 노드에서 같고, block `h−1` 에 누가 seal 했는지의 canonical 기록이 된다 (`A-04` 는 diligence 계산에 이 필드를 쓴다).

> 해설: 이전 seal 이 block hash 에 포함되기 때문에 §4 의 "노드마다 다른 seal" 이 검증 결과를 흔들지 않는다. epoch 경계에서는 `V` 와 `Vp` 가 다르다. 예를 들어 epoch block 20 다음의 block 21 은 `EpochInfo(20)` 의 집합이 seal 하지만, block 21 이 싣는 block 20 의 이전 seal 은 `EpochInfo(10)` 의 집합으로 검증한다 (§10.5).

### 6.6 State 에 의존하는 단계와 그 단계를 건너뛰는 경우

두 단계가 parent state 를 읽는다. H15b(blacklist)와 H21(gas tip)이다. 이 둘이 header 검증의 *state 의존* 단계다. parent state 가 없으면 verifier 는 두 단계에서 header 를 거부하지 않고 검사를 건너뛴다. `B-09` SNET-SYNC-010 은 동기화에서 바로 이 건너뛰기 동작에 기댄다. 다음 상황에서는 state 를 쓸 수 없으며, 모두 참조 구현에서 실제로 일어난다.

| 상황 | parent state 가 없는 이유 | Source |
|---|---|---|
| snap sync 중 header chain 삽입 | header chain 에는 state 가 전혀 없다. `StateAt` 는 항상 실패한다 | `core/headerchain.go:327, 437-438` |
| `InsertChain` batch 의 header `i ≥ 1` | 별도의 goroutine 이 실행보다 앞서 header 를 검증한다. header `i` 를 검증할 때 header `i−1` 이 이미 실행되었는지는 타이밍에 달려 있다 | `consensus/wbft/backend/engine.go:87-112`, `core/blockchain.go:1587-1592, 1744-1792` |
| parent state 가 prune 됨 | 노드는 최근 state 만 보관한다 | `core/blockchain.go:1646-1656` |
| snap sync 의 receipt import | body 와 receipt 를 실행 없이 import 한다 | 주석 `consensus/wbft/engine/engine.go:340-346` |

H21 은 parent 를 `(ParentHash, uint64(Number) − 1)` 로 chain 에서 다시 읽고, `parents` 를 보지 않는다 (`consensus/wbft/engine/engine.go:627-630`). 그래서 batch 의 header `i ≥ 1` 은 parent 가 아직 저장되지 않았으면 parent 가 없다는 오류로 H21 을 건너뛴다. H15b 가 batch 의 parent 를 썼더라도 H21 은 이렇게 건너뛴다.

[WBFT-HDR-111] (H21, 오류 처리만 정함. 값 규칙은 `B-06`) gas tip 검사가 불일치(`GasTipMismatchError`, `Extra.GasTip = nil` 포함)나 `ErrGasTipContractUnavailable` 을 보고하면 verifier 는 반드시 검증을 실패로 끝내야 한다. 그 밖의 모든 오류에 대해서는 verifier 가 반드시 이 검사를 건너뛰어야 한다. 즉 H21 에 관한 한 검증은 성공한다. 건너뛰는 오류에는 다음이 들어간다.

1. system contract 설정이 없다.
2. parent 가 없다.
3. parent `Root` 가 0 이다.
4. parent state 를 쓸 수 없다.
5. 검사 안에서 `Extra` 디코드가 실패한다.

참조 구현에서는 이 실패 분기 가운데 둘에 도달할 수 없다. 첫째, `ErrGasTipContractUnavailable` 은 `GetGasTip` 이 `nil` 을 돌려줄 때에만 나오는데, `GetGasTip` 은 `nil` 을 돌려주지 않는다. 빈 slot 은 0 으로 읽히기 때문이다 (`B-06` SNET-FIN-017, `B-04`). 둘째, 디코드한 `Extra.GasTip` 은 `nil` 이 되지 않는다 (`A-03` WBFT-ENC-008). 그래서 실제로 적용되는 규칙은 "값이 다르면 실패하고, 그 밖의 오류는 모두 건너뛴다" 이다.
Source: `consensus/wbft/engine/engine.go:329-348, 622-645, 1280-1295`

### 6.7 Batch 의미 (`VerifyHeaders`)

```python
def verify_header_batch(headers) -> list[Error | None]:
    results = []
    failed = False
    for i, h in enumerate(headers):
        if failed:
            results.append(ErrUnknownAncestor)
            continue
        V, Vp = validators_for_verifying(h, headers[:i])      # may raise -> result
        err = verify_header(h, headers[:i], V, Vp, check_seals=True)
        results.append(err)
        failed = failed or err is not None
    return results
```

Source: `consensus/wbft/backend/engine.go:87-112`

[WBFT-HDR-120] batch verifier 는 반드시 header 를 입력 순서대로 하나씩 검증하고, batch 안의 앞선 header 를 `parents` 로 넘겨야 한다. 처음 실패한 뒤에는 남은 header 를 검증하지 않고 반드시 모두 `ErrUnknownAncestor` 로 보고해야 한다. 결과는 반드시 입력 순서대로 전달해야 하며, 호출자가 중단하면 전달을 일찍 멈춰도 된다.
Source: `consensus/wbft/backend/engine.go:87-112`

Informative (호출자 동작, `A-09 §5.2`). `InsertChain` 은 첫 결과를 본다. header 검증이 통과했으면 첫 결과는 body 검증 결과로 바뀐다 (`core/blockchain_insert.go:129-133`). 첫 결과가 `ErrFutureBlock` 이거나, parent 가 future 로 queue 에 들어 있는 `ErrUnknownAncestor` 이면 block 들을 future queue 로 보낸다. `ErrPrunedAncestor` 와 `ErrKnownBlock` 은 따로 처리한다. header 검증이나 body 검증에서 나온 그 밖의 오류이면 첫 block 을 bad block 으로 기록하고 중단한다 (`core/blockchain.go:1595-1680`). 그래서 header 검증 실패도 그 hash 에 대한 bad block 기록을 만든다.

### 6.8 Genesis 와 block 1

| 높이 | V | Vp | H11–H19 | H20 | 참고 |
|---|---|---|---|---|---|
| 0 | genesis 설정의 validator 와 BLS 키 | = V | 실행하지 않음 (H10) | 실행하지 않음 | H1–H9 만 실행한다. `Extra` 는 디코드하지 않는다 |
| 1 | genesis header 의 `EpochInfo` | = V | 실행한다. parent 는 genesis 이고 `MixDigest(1) = randao_mix(genesis.MixDigest, reveal)` 이다 | 실행하지 않음 | `PrevRound` 와 이전 seal 에 제약이 없지만 (`WBFT-HDR-091`) 비어 있는 값으로 만든다 (`WBFT-HDR-033`) |
| 2 | 1 이하의 마지막 epoch block 의 `EpochInfo` | V(1) | 실행한다 | block 1 을 대상으로 `Vp = V(1)` 로 실행한다 | 이전 seal 을 싣는 첫 block 이다 |

Source: `consensus/wbft/engine/engine.go:82-84, 250-253, 1097-1117`, `consensus/wbft/backend/engine.go:429-447`

[WBFT-HDR-121] block 1 의 validator 집합은 반드시 genesis 설정이 아니라 genesis header 의 `EpochInfo` 에서 가져와야 한다. genesis header 가 높이 0 의 epoch block 이기 때문이다. 노드는 genesis 설정의 집합을 높이 0 자체에만 쓴다.
Source: `consensus/wbft/engine/engine.go:1104-1116, 1407-1415`
Observable: header

---

## 7. Epoch block 과 `EpochInfo` 존재

`is_epoch_block(number)` 는 `A-04` 가 정의한다. `Block ≤ number` 이고 `EpochLength` 가 0 이 아닌 마지막 transition 의 block 과 epoch 길이를 `(first, length)` 라 하자. 그런 transition 이 없으면 `(0, Epoch)` 를 쓴다. 그러면 `(number − first) mod length == 0` 일 때, 그리고 그때에만 `number` 는 epoch block 이다. 0 이 아닌 `EpochLength` 를 정하는 transition block 은 그 자체가 epoch block 이다.

Source: `consensus/wbft/engine/engine.go:1073-1090`

[WBFT-HDR-130] `Number ≥ 1` 인 모든 block 에서, `Extra.EpochInfo` 는 반드시 `is_epoch_block(Number)` 일 때 그리고 그때에만 `nil` 이 아니어야 한다.
Source: `consensus/wbft/engine/engine.go:948-961, 1212-1224`
Observable: header

[WBFT-HDR-131] 참조 구현은 block 을 **실행** 할 때(`Finalize`)에만, 그것도 system contract 전이와 base fee 분배 뒤에 `WBFT-HDR-130` 을 강제한다. `EpochInfo` 가 있는 non-epoch block 은 `ErrEpochInfoIsNotNil` 로 실패하고, `EpochInfo` 가 없는 epoch block 은 `WBFT: epochInfo is nil` 로 실패한다. 다만 epoch 정보를 다시 계산하는 `buildEpochInfo` 가 먼저 실패하면 그 오류를 돌려준다. header 검증(§6)은 이 규칙을 강제하지 않는다. 적합한 verifier 는 반드시 그런 block 을 늦어도 실행 시점까지는 거부해야 하고, header 검증 단계에서 거부해도 된다.
Source: `consensus/wbft/engine/engine.go:929-961, 1213-1224`, `core/state_processor.go:102`

[WBFT-HDR-132] epoch block 의 `EpochInfo` 내용은 반드시 chain 과 그 block 의 실행 뒤 state 에서 다시 계산한 값과 같아야 한다 (`A-04` 알고리즘, `B-06` 비교). header 검증은 이 내용을 검사하지 않는다.
Source: `consensus/wbft/engine/engine.go:1212-1263`
Observable: header, state

header 만 다루는 경로에 대한 결과(informative). 실행 없이 삽입한 header chain(snap sync)에는 `EpochInfo` 가 붙은 non-epoch block 이 검출되지 않고 들어갈 수 있다. `EpochInfo` 가 없는 epoch block 은 간접적으로 검출된다. 그 뒤의 어떤 header 를 검증하든 `validators_at` 이 필요한데, 이 호출이 `WBFT: epochInfo is nil` 로 실패하고 그 실패는 `ErrUnknownAncestor` 로 보고되기 때문이다 (`WBFT-HDR-070`).

---

## 8. Light verification (`verify_light`)

`verify_light` 는 state 가 없는 외부 verifier(inspector, bridge)가 어떤 header 가 올바르게 finalize 된 WBFT header 인지 판단할 때 쓰는 절차다. 이 절차는 §6 가운데 header 만 있으면 되는 부분에 `EpochInfo` 존재 규칙을 더한 것이다.

```python
class LightResult(Enum):
    VALID = 0
    INVALID = 1            # a rule is violated; the reason names the step
    CANNOT_DECIDE = 2      # an input needed for a decision is missing

def verify_light(h, parent, V, Vp, chain_id, cfg, check_future=False) -> (LightResult, str):
    # Inputs: h and parent (block_hash(parent) == h.ParentHash);
    # V  = validators_at(h.Number), Vp = validators_at(h.Number - 1), both obtained from
    #      EpochInfo of epoch headers that were themselves verified with verify_light,
    #      back to the genesis header (trust root: genesis EpochInfo and configuration).
    if h.Number == 0:                          return CANNOT_DECIDE, "genesis: B-02"
    if parent is None or V is None:            return CANNOT_DECIDE, "missing ancestor"
    if check_future and h.Time > now() + cfg.AllowedFutureBlockTime:
                                               return CANNOT_DECIDE, "future"        # H2
    if h.Difficulty != 1:                      return INVALID, "H4"
    if uint64(parent.Number) != h.Number - 1 or block_hash(parent) != h.ParentHash:
                                               return INVALID, "H11"
    if parent.Time + config_at(h.Number).BlockPeriod > h.Time:
                                               return INVALID, "H12"
    if h.Coinbase not in V:                    return INVALID, "H15a"
    try: x = decode_extra(h.Extra)
    except: return INVALID, "H16"
    if verify_seals(h, V, x) fails:            return INVALID, "H17"
    if ecdsa_recover_address(randao_data(chain_id, h.Number), x.RandaoReveal) != h.Coinbase:
                                               return INVALID, "H18"
    if randao_mix(parent.MixDigest, x.RandaoReveal) != h.MixDigest:
                                               return INVALID, "H19"
    if h.Number >= 2:
        if Vp is None:                         return CANNOT_DECIDE, "missing Vp"
        if verify_prev_seals(parent, Vp, x) fails: return INVALID, "H20"
    if (x.EpochInfo is not None) != is_epoch_block(h.Number):
                                               return INVALID, "HDR-130"
    # Optional: header-only Part B rules H3, H5-H9, H13, H14 (B-03).
    return VALID, ""
```

[WBFT-HDR-140] header 에 대해 `VALID` 를 돌려주는 light verifier 는 반드시 H4, H11, H12, H15a, H16, H17, H18, H19, H20 (`Number ≥ 2` 일 때), 그리고 `WBFT-HDR-130` 을 `Number < 2^64` 에서 §6 과 같은 정의로 검사했어야 한다.
Source: `consensus/wbft/engine/engine.go:196-350, 948-961`
Observable: header

> 해설: full node 의 header 검증은 `EpochInfo` 존재 규칙을 보지 않지만, light verification 은 이 규칙을 반드시 본다.

[WBFT-HDR-141] light verifier 는 반드시 `V` 와 `Vp` 를 genesis header 에서 시작해 자기가 직접 받아들인 epoch header 의 `EpochInfo` 에서만 얻어야 한다. 필요한 ancestor 나 epoch header 가 없으면 light verifier 는 반드시 `INVALID` 가 아니라 `CANNOT_DECIDE` 를 돌려주어야 한다.
Source: none in the reference (design requirement for external verifiers). Contrast `consensus/wbft/engine/engine.go:1414, 1427-1428`: the reference takes the epoch header from its canonical chain by number without checking its ancestry, and reports a missing one as `ErrUnknownAncestor`.
Observable: header

> 해설: "자료가 없다" 와 "규칙을 어겼다" 를 섞으면 inspector 가 빠진 데이터를 결함으로 보고한다. 그래서 두 결과를 구별한다. genesis(번호 0)에 대해서도 `verify_light` 는 `CANNOT_DECIDE` 를 돌려주며, genesis 판정은 `B-02` 에서 따로 가져와야 한다. `verify_light` 는 미래 시각 규칙 H2 를 기본으로 꺼 둔다(`check_future=False`). 과거 header 를 검증할 때 verifier 의 시계는 의미가 없기 때문이다.

[WBFT-HDR-142] light verifier 는 header 의 `Round`, `PreparedSeal`, `CommittedSeal` 을 그 child 의 이전 seal 필드와 비교해서는 안 된다 (`WBFT-HDR-053`).
Source: `consensus/wbft/backend/backend.go:221-249`
Observable: header

`verify_light` 가 확정할 수 없는 것(informative):

| 성질 | 확정할 수 없는 이유 | 누가 확정하는가 |
|---|---|---|
| `Coinbase` 가 blacklist 에 없음 (H15b) | parent state 가 필요하다 | PRE-PREPARE 에서의 quorum, state 를 가진 full node |
| gas tip 값 (H21) | parent state 가 필요하다 | 실행 시점의 `B-06` |
| `EpochInfo` 내용 (`WBFT-HDR-132`) | 실행 뒤 state 와 candidate 목록이 필요하다 | 실행 시점의 `B-06`. 다음 epoch 의 `V` 를 믿는 근거는 현재 epoch 의 quorum 이 그 epoch block 을 seal 했다는 사실뿐이다 |
| state root, receipt, bloom, gas used | 실행이 필요하다 | `A-09 §5` |
| 그 header 가 모든 노드가 finalize 한 header 라는 것 | seal 은 quorum 이 이 digest 에 서명했다는 것만 증명한다. 높이마다 유일하다는 성질은 `f < N/3` 일 때에만 성립한다 (`A-10 §2`) | — |
| 미래 시각 규칙 (H2) | verifier 의 시계에 달려 있고, 과거 header 에는 의미가 없다 | 살아 있는 노드만 |

---

## 9. Head 선택과 finality (informative)

WBFT 는 높이마다 block 하나를 decide 한다. 그런데도 실행 계층은 go-ethereum 의 total difficulty 기반 head 선택을 그대로 쓴다.

- 모든 WBFT block 은 `Difficulty = 1` 이다. 그래서 어느 chain 에서든 `TD(h) = TD(genesis) + h` 이다 (`core/forkchoice.go:77-113`, `consensus/wbft/engine/engine.go:1067-1069`). 더 긴 chain 이 항상 이긴다.
- 높이가 같은 서로 다른 두 block 가운데서는 노드가 로컬 계정이 만든 block 을 고른다. 로컬 계정이 만든 block 은 `Coinbase` 가 `etherbase` 이거나 `txpool.locals` 에 있는 주소인 block 이다. 두 block 모두 로컬 계정이 만든 것이 아니면 노드는 둘 중 하나를 1/2 확률로 무작위로 고른다 (`core/forkchoice.go:98-111`, `eth/backend.go:376-423`). 이 규칙은 seal 을 보지 않는다.
- `f < N/3` 이면 한 높이에서 committed seal 이 유효한 서로 다른 두 block 은 존재하지 않는다. 그래서 finalized block 에 이 규칙이 쓰일 일은 없다. 이 가정이 깨지면 노드마다 다른 head 를 고를 수 있고, 나중에 더 길게 자란 branch 로 reorganize 한다.
- 합의는 block 이 block fetcher 경로로 import 될 때에만 `finalized` 와 `safe` marker 를 앞으로 옮긴다 (`eth/handler.go:298-305`). proposer 자신의 경로와 downloader 는 두 marker 를 옮기지 않는다 (`A-09 §5`). 두 marker 를 쓰는 다른 곳(Engine API `forkchoiceUpdated`, 재시작)은 `B-09` §7.3 이 나열한다.

[WBFT-HDR-150] 적합한 노드는 `check_seals = true` 로 `verify_header` 를 통과하고 실행에 성공한 block 을 반드시 final 로 다뤄야 한다. 노드는 valid block 으로 이루어진 더 긴 chain 이 제시되지 않는 한, 자기 fork choice 로 그 block 을 되돌려서는 안 된다. 이 규칙은 참조 동작을 적은 것이며, `f < N/3` 이면 더 긴 chain 이 제시되는 일은 일어날 수 없다.
Source: `core/forkchoice.go:77-113`, `core/blockchain.go:1456-1479`

---

## 10. 계산 예

### 10.1 `verify_aggregated_seal` 이 쓰는 quorum 크기

`quorum_size(n) = ceil(n − (n − 1)/3)` 이며 float64 로 계산한다:

| n | (n−1)/3 | quorum |
|---|---|---|
| 1 | 0 | 1 |
| 4 | 1 | 3 |
| 5 | 1.333… | 4 |
| 6 | 1.666… | 5 |
| 7 | 2 | 5 |
| 10 | 3 | 7 |
| 100 | 33 | 67 |

### 10.2 Sealer bitmap

`Sealers = 0x0b01` (두 바이트) 이다. byte 0 = `0x0b = 0b00001011` 은 비트 0, 1, 3 을 켜고, byte 1 = `0x01` 은 비트 0 을 켜므로 index 8 이 된다. 그래서 index 는 `[0, 1, 3, 8]` 이고 개수는 4 다. `|V| = 7` (quorum 5) 이면 이 seal 은 `lack of seal count` 로 실패한다. `|V| = 5` (quorum 4) 이면 index 8 이 5 이상이므로 `sealer is not validator` 로 실패한다. verifier 는 `0x0b0100` 도 같은 집합으로 받아들인다 (`WBFT-HDR-100`).

### 10.3 Timestamp

`parent.Time = 1_700_000_000`, `BlockPeriod(n) = 1`, `AllowedFutureBlockTime = 0` 이라고 하자.

- proposer 시계가 `1_700_000_000` 이면 `Time = 1_700_000_001` 이 된다. 시계가 `1_700_000_000` 인 verifier 는 기간 약 1 초와 함께 `ErrFutureBlock` 을 돌려주고, 그 시각에 PRE-PREPARE 를 다시 처리한다.
- proposer 시계가 `1_700_000_003` 이면 `Time = 1_700_000_003` 이 된다. 이 값은 H12 에서 유효하다 (`1_700_000_001 ≤ 1_700_000_003`).
- `Time = 1_700_000_000` 인 header 는 H12 에서 `ErrInvalidTimestamp` 로 실패한다.

### 10.4 Extra seal 이 있는 이전 seal

`|V(n−1)| = 4` 이고 quorum 은 3 이다. proposer 가 가진 block `n−1` 사본은 `Round = 0`, `PreparedSeal.sealers = 0x07` (index 0, 1, 2), `CommittedSeal.sealers = 0x0b` (0, 1, 3) 이다. view `(n−1, 0)` 의 extra seal 은 index 2 와 3 의 PREPARE, index 2 의 COMMIT 이다.

- `PrevPreparedSeal.sealers = 0x0f` 이고, signature 는 aggregate(기존 PREPARE aggregate, index 3 의 PREPARE seal) 이다. index 2 는 이미 bitmap 에 있으므로 `merge_seals` 가 건너뛴다.
- `PrevCommittedSeal.sealers = 0x0f` 이고, signature 는 aggregate(기존 COMMIT aggregate, index 2 의 COMMIT seal) 이다.
- `PrevRound = 0` 이다.

proposer 의 `prior_round` 가 1 이고 `Extra(n−1).Round = 0` 이라면 extra seal 은 round 1 의 seal 이 된다. 그러면 병합한 aggregate 에 서로 다른 round 가 섞이고, 모든 verifier 가 이 proposal 에 대해 `ErrInvalidPrevPreparedSeals` (H20) 를 돌려준다 (`WBFT-HDR-036`).

### 10.5 Epoch block 과 validator 집합

`Epoch = 10` 이고 transition 은 `{Block: 25, EpochLength: 7}` 하나다.

- epoch block 은 0, 10, 20, 25, 32, 39, … 이다.
- `V(11)` … `V(20)` 은 `EpochInfo(10)` 에서 온다. block 20 은 높이 21–25 의 `EpochInfo` 를 싣는다. `V(26)` … `V(32)` 는 `EpochInfo(25)` 에서 온다.
- block 20 은 `EpochInfo(10)` 에서 온 `V(20)` 이 seal 한다. block 20 의 이전 seal(block 19 의 seal) 은 `Vp = V(19)` 로 검사하며, 이 집합도 `EpochInfo(10)` 에서 온다. block 21 은 `EpochInfo(20)` 에서 온 `V(21)` 이 seal 하지만, block 21 의 이전 seal(block 20 의 seal) 은 `Vp = V(20)` 으로 검사한다. 즉 epoch 경계를 넘으면 `V` 와 `Vp` 가 다르다.

### 10.6 순서가 있는 실패

높이 50 의 header 가 `Difficulty = 2` 이고, `Time` 이 10 초 뒤의 미래이며, parent 가 없다고 하자. engine 수준(`V`, `Vp` 를 주고 `verify_header` 를 부를 때)에서 이 header 는 먼저 `ErrFutureBlock` 을 돌려준다. H2 가 H4 와 H11 보다 앞서기 때문이다. 약 10 초 뒤 다시 검증하면 H4 가 H11 보다 앞서므로 결과는 `ErrInvalidDifficulty` 가 된다. `Difficulty = 1` 이었다면 그 결과는 `ErrUnknownAncestor` (H11) 였을 것이다.

그러나 `Backend.Verify`(proposal 검증)와 `Backend.VerifyHeader` 에서는 V0b 가 `uint64(Number) ≥ 2` 일 때 parent 를 필요로 하므로, H2 에 이르기 전에 `ErrUnknownAncestor` 를 돌려준다. canonical chain 에 무엇이 있든 결과는 같다. 그래서 proposal 검증은 이 proposal 을 바로 거부하고(기간 0) 다시 검사를 예약하지 않는다. 노드에서 engine 수준의 순서가 보이는 것은 batch 의 header `i ≥ 1` 처럼 parent 를 `parents` 에서 얻는 경우뿐이다.

---

## 11. 구현 노트 (informative)

- `sigHash`/`SealHash` 는 `block_hash` 와 같다 (`consensus/wbft/engine/engine.go:1063-1065, 1155-1160`). miner 는 이 값으로 sealing 결과를 작업과 맞춘다.
- `Backend.VerifySeal` (`consensus/wbft/backend/engine.go:122-136`, `consensus/wbft/engine/engine.go:462-480`) 은 difficulty, parent, signer 만 검사한다. 노드 안에서 이 함수를 부르는 곳은 없다.
- `CallEngineSpecific("InheritExtra" | "SetMixDigest" | "SetCoinbase")` (`consensus/wbft/backend/engine.go:329-408`) 는 test chain 생성기(`core/chain_makers.go:483-500`)를 위해 `prepare_proposal_header` 의 일부를 재현하며, 규범적인 header 만들기 경로가 아니다. `prepare_proposal_header` 와 달리 이 함수는 parent 의 seal 을 병합하지 않고 복사하며, parent 를 hash 로 찾는다.
- `Extra` 를 디코드할 수 없으면 `Header.Hash` 는 header 전체를 해시하는 방식으로 되돌아간다 (`core/types/block.go:121-131`). 그런 header 는 H16 에서 실패하므로, 이 대체 동작은 그 header 에 관한 메시지가 싣는 digest 에만 영향을 준다.
- `prepare_proposal_header` 는 `n−1` 의 canonical header 를 `nil` 검사 없이 역참조한다 (`consensus/wbft/engine/engine.go:516-517`). 그 번호에 canonical header 가 없으면 참조 구현은 panic 한다.
- validator 가 아닌 노드도 pending block 을 만들 때 `prepare_proposal_header` 를 실행하므로, 자기 node key 로 randao data 에 서명한다 (`miner/worker.go:644-662, 1320-1343`).
