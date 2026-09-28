# B-03 실행 측 header, body, state 유효성

- Status: draft
- Area codes: `BHDR` (header), `BODY` (body 와 실행 후 state)
- Reference implementation: go-stablenet `740526d03`

`A-08` 은 header 의 합의 필드를 정한다. 합의 필드는 difficulty, timestamp, coinbase 와 signer, extra 데이터, seal, randao 이다. 참조 구현은 이 필드들을 실행 측 필드(gas limit, base fee, 금지된 fork 필드, uncle hash)와 같은 함수에서 검사한다. 그래서 코드를 읽는 사람은 하나의 목록을 보게 된다. 이 장은 그 목록 가운데 실행 측 부분을 정한다. 이어서 header 를 받아들인 뒤에 실행되는 body 검사와, block 을 실행한 뒤에 실행되는 state 검사를 정한다. 마지막으로 block 이 이 검사들을 `A-08`, `B-06`, `B-07` 의 검사와 함께 통과하는 전체 순서를 정한다.

base fee 규칙은 StableNet 이 Ethereum 과 다른 가장 큰 부분이다. Anzeon 은 EIP-1559 의 비례 갱신을 threshold 규칙으로 바꾼다. 이 규칙은 2 % 의 고정 폭으로 움직이며 하한과 상한을 가진다. 정확한 정수 절차는 §2.3 에 있다.

---

## 1. 단독 header 규칙

이 규칙들은 header 와 설정에만 의존한다. 참조 구현은 parent 를 보기 전에, header 검증에 넘어온 모든 header 에 이 규칙들을 적용한다.

[SNET-BHDR-001] header 는 반드시 `UncleHash == keccak256(rlp([])) = 0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347` 이어야 한다. 그렇지 않으면 그 header 는 무효이며, 오류는 `ErrInvalidUncleHash` ("non empty uncle hash")이다.
Source: consensus/wbft/engine/engine.go:54, 207-210
Source: consensus/wbft/common/errors.go:53
Observable: header

[SNET-BHDR-002] header 는 반드시 `GasLimit <= MaxGasLimit = 2^63 - 1` 이어야 한다. 그렇지 않으면 그 header 는 무효이다("invalid gasLimit: have %v, max %v").
Source: consensus/wbft/engine/engine.go:216-219
Observable: header

[SNET-BHDR-003] 노드는 `is_shanghai(number, time)` 이 참인 header(`B-01` SNET-CFG-003)를 반드시 거부해야 한다("wbft does not support shanghai fork").
Source: consensus/wbft/engine/engine.go:220-222
Observable: header

[SNET-BHDR-004] header 에는 반드시 `WithdrawalsHash` 가 없어야 한다. 있으면 그 header 는 무효이다("invalid withdrawalsHash: have %x, expected nil").
Source: consensus/wbft/engine/engine.go:223-226
Observable: header

[SNET-BHDR-005] 노드는 `is_cancun(number, time)` 이 참인 header 를 반드시 거부해야 한다("wbft does not support cancun fork").
Source: consensus/wbft/engine/engine.go:227-229
Observable: header

[SNET-BHDR-006] header 에는 반드시 `ExcessBlobGas`, `BlobGasUsed`, `ParentBeaconRoot` 가 없어야 하며, 노드는 이 순서로 검사한다("invalid excessBlobGas: have %d, expected nil", "invalid blobGasUsed: have %d, expected nil", "invalid parentBeaconRoot, have %#x, expected nil").
Source: consensus/wbft/engine/engine.go:230-238
Observable: header

"없다" 는 header 의 RLP 인코딩에 그 선택 header 필드가 아예 들어 있지 않다는 뜻이다(go-ethereum 의 optional trailing field). 이 필드 가운데 하나에 0 값을 인코딩한 header 는 필드가 "없는" 것이 아니므로 노드는 그 header 를 거부한다.

> 해설: Rust 로 옮길 때 `Option<u64>` 의 `Some(0)` 과 `None` 을 구분해 인코딩하지 않으면 이 규칙에서 판정이 갈린다.

---

## 2. Parent 에 대한 규칙

이 규칙들은 `number > 0` 인 모든 header 에 대해, 노드가 parent 를 찾은 뒤에 실행된다. parent 를 모르는 경우와 timestamp 규칙은 `A-08` 이 정한다. genesis header 는 header 검증으로 판정하지 않으며, genesis 의 유효성은 `B-02` 가 정한다. 참조 구현은 시작할 때 현재 header 를 `VerifyHeader` 에 넘기고 결과를 무시하는데, 이 현재 header 는 genesis header 일 수도 있다(`core/blockchain.go:424`).

### 2.1 Gas used 와 gas limit

[SNET-BHDR-007] header 는 반드시 `GasUsed <= GasLimit` 이어야 한다. 그렇지 않으면 그 header 는 무효이다("invalid gasUsed: have %d, gasLimit %d").
Source: consensus/wbft/engine/engine.go:273-276
Observable: header

```python
def verify_gas_limit(parent_gas_limit: uint64, gas_limit: uint64) -> None:
    # 참조 구현: 부호 있는 64 비트 차이를 구한 뒤 절댓값을 취한다
    diff = abs(int64(parent_gas_limit) - int64(gas_limit))
    limit = parent_gas_limit // 1024                  # GasLimitBoundDivisor
    if diff >= limit:
        raise Invalid(f"invalid gas limit: have {gas_limit}, want {parent_gas_limit} +-= {limit - 1}")
    if gas_limit < 5000:                              # MinGasLimit
        raise Invalid("invalid gas limit below 5000")
```

[SNET-BHDR-008] gas limit 은 반드시 `verify_gas_limit(parent_gas_limit, header.GasLimit)` 을 만족해야 한다. 여기서 `parent_gas_limit` 은 `is_london(parent.Number)` 이면 `parent.GasLimit` 이고, header 가 첫 London block 이면 `parent.GasLimit * 2` (elasticity multiplier)이다. 그러므로 block 마다 허용되는 변화량은 `parent_gas_limit // 1024` 보다 엄격히 작고, gas limit 은 5000 아래로 내려가지 않는다.
Source: consensus/misc/eip1559/eip1559.go:33-41 (VerifyEIP1559Header, gas limit part)
Source: consensus/misc/gaslimit.go:27-41 (VerifyGaslimit)
Observable: header

구현 노트 (informative). 두 preset 은 London 이 block 0 이므로 모든 parent 가 London block 이고, multiplier 는 한 번도 적용되지 않는다. `GasLimit <= 2^63 - 1` 이므로(SNET-BHDR-002) 검증된 header 에서 `verify_gas_limit` 의 부호 있는 변환은 overflow 할 수 없다. 다만 첫 London block 은 예외이다. 첫 London block 에서는 `parent_gas_limit = parent.GasLimit * 2` 이므로, `parent.GasLimit > 2^62` 이면 이 값이 `2^63 - 1` 을 넘는다. 그러면 `int64(parent_gas_limit)` 가 음수로 wrap 되고, 64 비트 차이 계산도 wrap 된다. 실제 차이가 `2^63` 보다 작으면 판정은 여전히 맞지만, 훨씬 작은 gas limit 이 통과할 수 있고, 그 header 에는 SNET-BHDR-008 이 말하는 변화 폭 제한이 성립하지 않는다. 참조 코드로 실행해 확인했다. parent gas limit 이 `2^63 - 1` 이면 gas limit 5000 인 첫 London block 은 통과했고, parent 값이 같은 그 뒤의 London block 은 거부되었다. 의사코드는 64 비트 wrap 산술로 계산할 때에만 이 동작을 재현한다. 두 preset 은 London block 이 0 이므로 이 경우에 도달하지 않는다. parent 의 gas limit 이 1024 보다 작으면 `limit` 이 0 이 되어 어떤 자식 block 도 유효할 수 없다. `MinGasLimit = 5000` 이므로 이 경우는 genesis gas limit 이 1024 보다 작을 때에만 생긴다.

[SNET-BHDR-009] header 가 London block 이 아니면 `BaseFee` 가 반드시 없어야 하며("invalid baseFee before fork: have %d, want <nil>"), gas limit 은 반드시 `verify_gas_limit(parent.GasLimit, header.GasLimit)` 을 만족해야 한다.
Source: consensus/wbft/engine/engine.go:277-284
Observable: header

### 2.2 Base fee 의 존재와 값

[SNET-BHDR-010] header 가 London block 이면 `BaseFee` 가 반드시 있어야 하며("header is missing baseFee"), 그 값은 반드시 §2.3 의 `calc_base_fee(parent)` 와 같아야 한다("invalid baseFee: have %s, want %s, parentBaseFee %s, parentGasUsed %d").
Source: consensus/misc/eip1559/eip1559.go:42-52
Source: consensus/wbft/engine/engine.go:285-288
Observable: header

### 2.3 Anzeon base fee 규칙

```python
INCREASING_THRESHOLD = 20        # percent
DECREASING_THRESHOLD = 6         # percent
BASE_FEE_CHANGE_RATE = 2         # percent
MIN_BASE_FEE = 20_000_000_000_000
MAX_BASE_FEE = 20_000_000_000_000_000     # 0 이면 상한을 끈다

def base_fee_delta(parent_base_fee: int) -> int:
    d = parent_base_fee * BASE_FEE_CHANGE_RATE // 100
    return d if d != 0 else 1

def calc_base_fee(parent: Header) -> int:
    if not is_london(parent.number):
        return INITIAL_BASE_FEE                     # 1_000_000_000; 첫 London block 에서만
    # Anzeon chain (B-01 SNET-CFG-004).
    increasing_target = uint64(parent.gas_limit * INCREASING_THRESHOLD) // 100
    decreasing_target = uint64(parent.gas_limit * DECREASING_THRESHOLD) // 100
    base_fee = parent.base_fee
    if parent.gas_used > increasing_target:
        base_fee = base_fee + base_fee_delta(parent.base_fee)
        if MAX_BASE_FEE != 0 and base_fee > MAX_BASE_FEE:
            base_fee = MAX_BASE_FEE
        return base_fee
    if parent.gas_used < decreasing_target:
        base_fee = base_fee - base_fee_delta(parent.base_fee)
        if base_fee < MIN_BASE_FEE:
            base_fee = MIN_BASE_FEE
        return base_fee
    return base_fee                                 # [decreasing_target, increasing_target] 안이면 그대로 둔다
```

같은 함수 안에는 Anzeon 이 아닌 chain 을 위한 Ethereum 비례 규칙(`parentGasTarget = GasLimit / 2`, 변화 `/ 8`)도 있지만, StableNet 에서는 이 규칙을 쓰지 않는다.

구현 노트 (informative). genesis base fee 는 `MinBaseFee` 이고(`B-02` SNET-GEN-002), 이 규칙은 감소한 뒤에 하한 아래로, 증가한 뒤에 상한 위로 가지 않는다. 그러므로 genesis 부터 London 인 chain 에서 모든 base fee 는 `[MIN_BASE_FEE, MAX_BASE_FEE]` 안에 있다. London block 이 0 보다 늦은 chain 에서는 첫 London block 이 `INITIAL_BASE_FEE = 10^9` 을 받는다. 이 값은 하한보다 작다. parent 사용량이 구간 안에 있으면 base fee 는 그대로이고, 증가 기준을 넘으면 block 마다 2 % 씩 오르며 증가 뒤에는 하한을 적용하지 않는다. 그래서 이 두 경우에는 base fee 가 하한 아래에 머문다. parent 사용량이 `DECREASING_THRESHOLD` 보다 작은 첫 block 에서는 base fee 가 `MIN_BASE_FEE` 가 된다. 감소한 뒤에 하한을 적용하기 때문이다. 참조 코드로 실행해 확인했다. parent base fee 가 `10^9` 이고 `GasUsed = 0` 이면 `20_000_000_000_000` 이 나왔고, `GasUsed = 10_000_000`(구간 안)이면 `1_000_000_000` 이 나왔다.

### 2.4 계산 예

parent gas limit 이 `105_000_000` (mainnet preset genesis)이면 `increasing_target = 21_000_000`, `decreasing_target = 6_300_000` 이다. 아래 값은 참조 구현의 `CalcBaseFee` 로 계산했다.

| Parent base fee | Parent gas used | 경우 | 자식 base fee |
|---|---|---|---|
| `20_000_000_000_000` | `0` | 감소, 하한에서 잘린다 | `20_000_000_000_000` |
| `20_000_000_000_000` | `6_299_999` | 감소, 하한에서 잘린다 | `20_000_000_000_000` |
| `20_000_000_000_000` | `6_300_000` | 그대로 둔다. gas used 가 감소 목표보다 작지 않다 | `20_000_000_000_000` |
| `20_000_000_000_000` | `21_000_000` | 그대로 둔다. gas used 가 증가 목표보다 크지 않다 | `20_000_000_000_000` |
| `20_000_000_000_000` | `21_000_001` | `400_000_000_000` 만큼 증가 | `20_400_000_000_000` |
| `30_000_000_000_000` | `0` | `600_000_000_000` 만큼 감소 | `29_400_000_000_000` |
| `30_000_000_000_000` | `21_000_001` | `600_000_000_000` 만큼 증가 | `30_600_000_000_000` |
| `19_999_999_999_999_999` | `105_000_000` | 증가, 상한에서 잘린다 | `20_000_000_000_000_000` |

---

## 3. Proposal 구성 (실행 측 필드)

`build_proposal` (`A-09`)은 이 장의 필드를 다음과 같이 채운다. 규범인 것은 결과 값뿐이며, 결과 값은 §1-§2 의 규칙을 통과해야 한다. 노드가 gas limit 을 고르는 방식은 로컬 결정이다.

[SNET-BHDR-012] proposer 는 반드시 `BaseFee = calc_base_fee(parent)` 로 두어야 하고, 반드시 `WithdrawalsHash`, `ExcessBlobGas`, `BlobGasUsed`, `ParentBeaconRoot` 를 비워 두어야 하며, 반드시 SNET-BHDR-008 을 만족하는 `GasLimit` 을 골라야 한다.
Source: miner/worker.go:1141-1164 (prepareWork: CalcGasLimit, CalcBaseFee; Cancun fields only if Cancun)
Observable: header

구현 노트 (informative). 참조 구현의 miner 는 `GasLimit = CalcGasLimit(parent.GasLimit, GasCeil)` 로 계산한다. 이 함수는 로컬 `--miner.gaslimit` 목표 쪽으로 block 마다 최대 `parent.GasLimit // 1024 - 1` 만큼 움직이며, 5000 아래로는 내려가지 않는다(`core/block_validator.go:151-172`). 노드는 `UncleHash`, `TxHash`, `ReceiptHash`, `Bloom`, `Root` 를 finalization 중에 설정한다(`B-06` §6).

[SNET-BHDR-013] 노드 운영자가 다른 목표를 정하지 않았다면, proposer 는 gas limit 목표 `105_000_000` 을 쓰고 위에서 설명한 대로 `GasLimit = CalcGasLimit(parent.GasLimit, 105_000_000)` 으로 두는 것이 좋다(SHOULD). 이 값은 참조 구현의 `--miner.gaslimit` 기본값이며, upstream go-ethereum 은 `30_000_000` 을 쓴다. 한 네트워크의 validator 는 모두 같은 목표를 쓰는 것이 좋다. 목표가 다른 proposer 들이 섞이면 header 의 `GasLimit` 이 그들의 block 사이에서 오르내리기 때문이다. 그런 block 도 유효하다. 기본 목표를 쓰면 mainnet preset 은 genesis gas limit `105_000_000` 에 머물고, genesis gas limit 이 `4_712_388` 인 testnet preset(`B-02` §8.2)은 block 마다 최대 `parent.GasLimit // 1024 - 1` 씩 목표 쪽으로 오른다.
Source: miner/miner.go:73-75 (DefaultConfig.GasCeil), cmd/utils/flags.go:442 (flag default)
Source: miner/worker.go:1145, 1162 (CalcGasLimit with GasCeil), core/block_validator.go:151-172 (CalcGasLimit)
Observable: header

---

## 4. Body 규칙

body 검증은 block 의 header 를 받아들인 뒤, 실행하기 전에 실행된다.

[SNET-BODY-001] block 에는 반드시 uncle 이 없어야 한다. uncle 이 하나라도 있으면 그 block 은 무효이며, 오류는 `ErrInvalidUncleHash` ("non empty uncle hash")이다. header `UncleHash` 는 반드시 `keccak256(rlp(uncles))` 와 같아야 한다("uncle root hash mismatch (header value %x, calculated %x)").
Source: core/block_validator.go:62-67
Source: consensus/wbft/engine/engine.go:453-458 (VerifyUncles)
Observable: header

[SNET-BODY-002] header `TxHash` 는 반드시 transaction 목록의 Merkle-Patricia root 와 같아야 한다. 이 root 는 합의 인코딩에 대해 `rlp(index)` 를 key 로 `DeriveSha` 를 적용해 구한다("transaction root hash mismatch (header value %x, calculated %x)").
Source: core/block_validator.go:68-70
Observable: header

[SNET-BODY-003] `WithdrawalsHash` 가 없으므로(SNET-BHDR-004) block body 는 withdrawal 목록을 담아서는 안 된다. 목록이 있으면 빈 목록이라도 그 block 은 무효이다("withdrawals present in block body").
Source: core/block_validator.go:72-84
Source: core/types/block.go:190-194, 230-235 (withdrawals is an optional trailing body field)
Observable: header

[SNET-BODY-004] block 의 어떤 transaction 도 blob sidecar 를 담아서는 안 된다("unexpected blob sidecar in transaction at index %d"). 또한 `BlobGasUsed` 가 없으므로 모든 transaction 의 blob hash 개수 합은 반드시 0 이어야 한다("data blobs present in block body"). sidecar 검사는 목록 순서대로 transaction 마다 실행되며, 개수 검사보다 먼저 실행된다.
Source: core/block_validator.go:86-110
Observable: header

[SNET-BODY-005] parent block 을 모르면 body 검증은 반드시 `ErrUnknownAncestor` 를 보고해야 하고, parent block 은 알지만 그 state 가 없으면 반드시 `ErrPrunedAncestor` 를 보고해야 한다. 이 두 결과는 무효 판정이 아니다. 조상이 준비되면 노드는 그 block 을 다시 시도하거나 side chain 으로 처리한다.
Source: core/block_validator.go:112-118

구현 노트 (informative). `ValidateBody` 는 block 과 그 state 가 이미 있으면 먼저 `ErrKnownBlock` 을 돌려준다. `ErrKnownBlock` 은 검사를 건너뛰는 지름길일 뿐이며, 유효성 판정이 아니다.

---

## 5. 실행

실행은 block 의 transaction 을 목록 순서대로 parent state 에 적용한 뒤 finalize 한다. transaction 의미, gas pool, receipt 는 `B-07` 이 정한다. gas pool 은 `header.GasLimit` 으로 초기화되며, gas limit 이 남은 pool 보다 큰 transaction 은 그 block 을 무효로 만든다. finalization(upgrade, base fee 분배, epoch 정보, gas tip, state root)은 `B-06` 이 정한다.

[SNET-BODY-006] 실행은 반드시 finalization 전에 모든 transaction 을 처리해야 하고, finalization 은 반드시 마지막 transaction 뒤의 state 를 보아야 한다. `B-07` 의 실행 전 검사(nonce, 잔액, gas pool, signer, blacklist, fee delegation 조건)를 통과하지 못하는 transaction 을 담은 block 은 무효이다.
Source: core/state_processor.go:60-107 (Process: loop, then engine.Finalize)
Observable: state

---

## 6. 실행 후 state 규칙

[SNET-BODY-007] header `GasUsed` 는 반드시 실행이 계산한 모든 transaction 의 사용 gas 합과 같아야 한다("invalid gas used (remote: %d local: %d)").
Source: core/block_validator.go:126-128
Observable: header, state

[SNET-BODY-008] header `Bloom` 은 반드시 receipt bloom 들의 OR 와 같아야 한다("invalid bloom (remote: %x  local: %x)").
Source: core/block_validator.go:129-134
Observable: header

[SNET-BODY-009] header `ReceiptHash` 는 반드시 합의 인코딩한 receipt 들의 Merkle-Patricia root 와 같아야 한다("invalid receipt root hash (remote: %x local: %x)"). 합의 receipt 인코딩은 go-ethereum 의 인코딩(type, status, cumulative gas used, bloom, log)이다. `EffectiveGasPrice` 와 다른 파생 필드는 이 인코딩에 들어가지 않는다.
Source: core/block_validator.go:135-139
Observable: header

[SNET-BODY-010] header `Root` 는 반드시 실행과 finalization 뒤에 얻은 state root 와 같아야 한다. 노드는 이 state root 를 빈 계정을 지운 state 에서 StableNet 계정 인코딩(`B-02` SNET-GEN-016)으로 계산한다. 빈 계정 삭제는 `is_eip158(number)` 가 참일 때 일어나며, StableNet 에서는 이 값이 항상 참이다. 두 root 가 다르면 오류는 "invalid merkle root (remote: %x local: %x) dberr: %w" 이다.
Source: core/block_validator.go:140-144
Source: consensus/wbft/engine/engine.go:967 (the same computation in finalization)
Observable: header, state

[SNET-BODY-011] state 검증은 SNET-BODY-007 부터 SNET-BODY-010 까지만 검사한다. state 검증은 header `GasTip` 을 `GovValidator` 컨트랙트와 대조하지 않으며, proposer 의 blacklist 상태도 검사하지 않는다. 그 두 검사는 header 검증(`B-08` SNET-SRC-020 의 단계 H15b, `B-06` §5 의 단계 H21)과 finalization(`B-06` §5)이 맡는다. 이 검사들을 자신의 state 검증에 합친 구현도 반드시 정확히 같은 block 을 거부해야 한다.
Source: core/block_validator.go:124-146 (no gasTip or blacklist check)
Source: consensus/wbft/engine/engine.go:290-298, 329-348, 963-965

---

## 7. Import 경로의 전체 검사 순서

block 은 아래의 모든 검사를 통과할 때, 그리고 그때에만 유효하다. 단계 H1–H21 의 순서는 `A-08` 이 정한 대로 규범이다(WBFT-HDR-080). 검증하는 노드는 단계를 주어진 순서대로 적용하고, 처음 실패한 단계의 오류를 돌려준다. 오류의 종류가 호출자의 다음 동작을 정하기 때문이다. 호출자는 미래 block 을 queue 에 넣고 다시 시도하며, 조상을 모르는 block 은 조상을 모르는 경우의 절차로 처리하고, import 에 실패한 block 은 bad block 으로 기록한다(`A-08` §6.2, `A-09` §5). 이 표는 그 순서를 다시 적은 것이며 순서를 완화하지 않는다. 두 문서가 다르면 `A-08` 이 맞다. 뒤따르는 단계(B1, E1, F1, S1)는 header 결과가 나온 뒤에 표에 보인 순서로 실행된다. 담당 열은 그 검사를 정하는 장을 가리킨다.

| 단계 | 구간 | 검사 | 담당 |
|---|---|---|---|
| H1 | Header | `Number` 가 있다 | `A-08` |
| H2 | Header | `AllowedFutureBlockTime` 을 넘는 미래가 아니다 | `A-08` |
| H3 | Header | `UncleHash` 가 비어 있다 (SNET-BHDR-001) | `B-03` |
| H4 | Header | `Difficulty == 1` | `A-08` |
| H5 | Header | `GasLimit <= 2^63-1` (SNET-BHDR-002) | `B-03` |
| H6 | Header | Shanghai 가 아니다 (SNET-BHDR-003) | `B-03` (판정 함수는 `B-01`) |
| H7 | Header | `WithdrawalsHash` 가 없다 (SNET-BHDR-004) | `B-03` |
| H8 | Header | Cancun 이 아니다 (SNET-BHDR-005) | `B-03` (판정 함수는 `B-01`) |
| H9 | Header | blob 필드와 beacon root 필드가 없다 (SNET-BHDR-006) | `B-03` |
| H10 | Header | `number == 0` 이면 여기서 멈춘다 | `A-08` |
| H11 | Header | parent 를 알고, 번호와 hash 가 맞는다 | `A-08` |
| H12 | Header | `parent.Time + block_period(number) <= Time` | `A-08` |
| H13 | Header | `GasUsed <= GasLimit` (SNET-BHDR-007) | `B-03` |
| H14 | Header | gas limit 범위와 base fee (SNET-BHDR-008 부터 SNET-BHDR-010 까지) | `B-03` |
| H15a, H15b | Header | coinbase 가 validator 이다; parent state 에서 coinbase 가 blacklist 되어 있지 않다 (parent state 가 없으면 건너뛴다) | `A-08`, `B-08` SNET-SRC-020 |
| H16 | Header | extra 가 `WBFTExtra` 로 디코드된다 | `A-08` |
| H17 | Header | prepared seal 과 committed seal (전체 검증에서만) | `A-08` |
| H18, H19 | Header | randao reveal signature 와 mix | `A-08` |
| H20 | Header | 이전 block 의 seal (필요할 때) | `A-08` |
| H21 | Header | `GasTip` 이 parent state 의 컨트랙트 값과 같다. 값이 다르면 이 단계는 실패한다. state 를 쓸 수 없는 경우를 포함해 이 검사의 다른 모든 오류는 건너뛴다(`A-08` WBFT-HDR-111) | `B-06` §5 |
| B1 | Body | uncle, tx root, withdrawal, blob, 조상 (SNET-BODY-001 부터 SNET-BODY-005 까지) | `B-03` |
| E1 | Execution | transaction 을 순서대로 실행한다 | `B-07` |
| F1 | Finalization | upgrade, base fee 분배, epoch 정보 검증 또는 nil 검사, gas tip 검사(이 단계에서는 오류를 건너뛰지 않는다), state root | `B-06` |
| S1 | State | gas used, bloom, receipt root, state root (SNET-BODY-007 부터 SNET-BODY-010 까지) | `B-03` |

단계 이름 `H1`-`H21` 은 `A-08` §6.2 의 이름이다. `B1`, `E1`, `F1`, `S1` 은 이 장에서만 쓰는 이름이다.

Source: consensus/wbft/engine/engine.go:196-350 (H1-H21)
Source: core/blockchain_insert.go:118-134 (header result, then ValidateBody)
Source: core/blockchain.go:1587, 1779-1792 (VerifyHeaders, Process, ValidateState)
Source: core/state_processor.go:83-104 (E1, F1)

proposal 투표 경로(`validate_proposal`, `A-08` 단계 P1-P7)는 body 의 transaction root 와 uncle hash 를 검사하고(P4, P5), H17 을 뺀 H1-H21 을 실행한다. 이 경로는 B1, E1, F1, S1 을 실행하지 않는다. `B-06` §6 을 참조한다.

> 해설: 투표 전에 transaction 을 실행하지 않는다는 점이 중요하다. validator 는 실행 결과를 계산하지 않은 block 에 투표한다. 이 선택의 결과는 `B-06` §6.1 에서 다룬다.
