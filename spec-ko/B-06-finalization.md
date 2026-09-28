# B-06 Block finalization

- Status: draft
- Area code: `FIN`
- Reference implementation: go-stablenet `740526d03`

Finalization 은 StableNet block 이 transaction 실행을 마친 뒤, state root 가 정해지기 전에 거치는 state 변경과 검사의 모음이다. finalization 은 transaction 을 거치지 않고 chain 자체의 규칙이 state 를 직접 건드리는 곳이다. finalization 은 다음 네 가지를 한다.

1. upgrade height 에서 system contract 코드를 교체한다.
2. transaction 이 낸 base fee 를 validator 에게 넘겨준다.
3. epoch block 에 다음 validator 집합을 쓰거나, epoch block 의 정보를 그 집합과 대조한다.
4. header gas tip 을 governance 값과 대조한다.

이 변경은 state root 에 들어가므로, finalization 을 다르게 하는 노드는 다른 root 를 계산하고 그 뒤의 모든 block 을 거부한다.

참조 구현은 같은 절차 `processFinalize` 를 두 곳에서 실행한다. proposer 는 block 을 만들 때 `FinalizeAndAssemble` 안에서 이 절차를 실행하며, 이때 epoch hook 은 `EpochInfo` 를 쓴다. import 하는 노드는 block 을 처리할 때 `Finalize` 안에서 이 절차를 실행하며, 이때 epoch hook 은 `EpochInfo` 를 검증한다. 두 경로는 그 밖에는 같다. 그러나 두 경로를 둘러싼 동작은 다르다. proposer 는 자신이 만든 block 을 다시 실행하지 않고, 다른 validator 는 proposal 을 실행하지 않고 투표한다(§6).

---

## 1. 입력

- `header`: block `n` 의 header 이다. proposer 경로에서는 만드는 중인 header 이며, 그 `Extra` 에는 `A-08` 의 proposal 구성이 쓴 합의 필드(`GasTip` 포함)가 이미 들어 있다. import 경로에서는 받은 header 의 사본이며, finalization 이 이 사본에 가한 변경은 버린다.
- `state`: block `n` 의 마지막 transaction 뒤의 state 이다(`B-03` SNET-BODY-006).
- `epoch_handler`: proposer 경로에서는 `write_epoch`, import 경로에서는 `verify_epoch` 이다.

[SNET-FIN-001] finalization 은 반드시 block 마다 정확히 한 번, block 의 모든 transaction 뒤에, transaction 이후의 state 위에서 실행되어야 하며, finalization 의 state 변경은 반드시 block 의 state root 에 들어가야 한다.
Source: core/state_processor.go:83-104 (Process: transactions, then engine.Finalize)
Source: miner/worker.go:977, 1401 (proposer: transactions applied to env.state, then FinalizeAndAssemble)
Observable: state

---

## 2. 절차

```python
def process_finalize(header, state, epoch_handler) -> None:
    n = header.number
    # 1. 정확히 n 에 예약된 system contract upgrade
    st = merged_upgrade_transition(n)                     # §2.1; 오류 -> block 무효
    if st is not None:
        for (addr, code) in st.codes:
            state.set_code(addr, code)
        for (addr, key, value) in st.states:
            state.set_storage(addr, key, value)
    # 2. base fee 분배
    if is_london(n):
        distribute_base_fee(header, state)                # §3; 오류 -> block 무효
    # 3. epoch 정보
    if is_epoch_block(n):                                 # A-04
        epoch_handler(header, state)                      # §4
    else:
        extra = decode_extra(header)                      # 오류 -> block 무효
        if extra.epoch_info is not None:
            raise Invalid(ErrEpochInfoIsNotNil)           # "epoch info should be nil for non-epoch block"
    # 4. gas tip
    verify_gas_tip(header)                                # §5; 모든 오류 -> block 무효
    # 5. root
    header.root = state.intermediate_root(delete_empty=is_eip158(n))
    header.uncle_hash = EMPTY_UNCLE_HASH
```

[SNET-FIN-002] finalization 은 반드시 `process_finalize` 의 순서, 곧 upgrade, base fee 분배, epoch 정보, gas tip, state root 순서로 단계를 실행해야 한다. 어느 단계에서든 오류가 나면 반드시 그 block 을 무효로 해야 하며, 뒤의 단계는 적용해서는 안 된다.
Source: consensus/wbft/engine/engine.go:929-970 (processFinalize)
Observable: state

> 해설: upgrade 가 가장 먼저 오므로 같은 block 의 뒤 단계는 새 코드를 본다. epoch 단계는 upgrade 와 base fee 분배를 마친 state 를 읽는다(SNET-FIN-013). base fee 분배는 잔액만 쓰고 `GovValidator` storage 는 쓰지 않는다. 그래서 epoch 단계가 읽는 candidate 값은 block `e` 의 최종 state `S(e)` 의 같은 slot 값과 같다(`B-08` SNET-SRC-004). 그래서 관찰자는 `eth_getStorageAt(gv, slot, e)` 로 candidate 목록을 다시 계산할 수 있다. gas tip 단계는 현재 state 가 아니라 parent state 를 읽으므로(SNET-FIN-017) 값 자체는 순서와 무관하다. 다만 어느 오류가 먼저 보고되는지는 순서가 정한다.

### 2.1 System contract upgrade

```python
def merged_upgrade_transition(n) -> Optional[StateTransition]:
    merged = None
    for u in upgrade_list:                                # B-01 §7.1, block 번호가 줄지 않는 순서
        if u.block == n:
            st = system_contracts_transition(u.system_contracts, alloc=None)   # B-02 §4.2
            if merged is None:
                merged = st
            else:
                merged.codes += st.codes
                merged.states += st.states
        elif n < u.block:
            break
    return merged
```

[SNET-FIN-003] block `n > 0` 에서 노드는 `block == n` 인 모든 upgrade 항목을 반드시 적용해야 한다. 이 요구사항은 upgrade 방식을 정하는 `B-04` SNET-SYS-021 을 다시 적은 것이다. 노드는 먼저 병합한 transition 의 모든 코드 교체를 순서대로 적용하고, 그다음 모든 storage 쓰기를 순서대로 적용한다. 순서는 항목 안에서는 멤버 순서 `govValidator`, `nativeCoinAdapter`, `govMinter`, `govMasterMinter`, `govCouncil` 이고, 항목끼리는 목록 순서이다. 코드 교체는 반드시 계정의 코드만 바꿔야 하며, 반드시 그 계정의 잔액, nonce, extra, storage 를 유지해야 한다. 계정이 없으면 코드 교체가 그 계정을 만든다.
Source: consensus/wbft/config.go:199-225 (GetSystemContractsStateTransition)
Source: consensus/wbft/engine/engine.go:930-939
Observable: state

[SNET-FIN-004] storage 쓰기는 버전이 `v1` 이고 `params` 가 있는 항목에 대해서만 생긴다. 노드는 이때 `B-04` 의 초기화 함수를 allocation 없이 부른다. 그래서 `NativeCoinAdapter` 총 공급량과 `GovCouncil` 의 blacklist 집합 및 authorized 집합은 초기화되지 않는다. 다른 버전은 반드시 코드만 바꿔야 한다.
Source: systemcontracts/systemcontracts.go:62-157
Source: systemcontracts/coin_adapter.go:153-166; systemcontracts/gov_council.go:111-146 (alloc == nil skips)
Observable: state

[SNET-FIN-005] `n` 의 upgrade 항목이 그 컨트랙트 타입에 등록되지 않은 버전을 가리키거나, 초기화 함수가 그 항목의 `params` 를 거부하면, block `n` 은 반드시 무효여야 한다("unsupported version %s for contract %s" 또는 초기화 함수의 오류).
Source: systemcontracts/contracts.go:80-90 (getContractCode)
Source: consensus/wbft/engine/engine.go:930-931

> 해설: 시작 시 검사(`B-01` SNET-CFG-012)는 `boho` 의 버전을 보지 않는다. 그래서 설정의 버전 오타는 시작할 때가 아니라 이 규칙에서 처음 드러나고, 그 upgrade height 에서 chain 이 멈춘다.

[SNET-FIN-006] upgrade 는 transaction 뒤에 적용되므로, block `n` 의 transaction 은 반드시 upgrade 전에 유효하던 코드로 실행되어야 한다. 새 코드는 block `n + 1` 부터 적용된다. `B-01` §3 의 fork 판정 함수(예: Boho precompile)는 block `n` 에서 이미 활성이다.
Source: core/state_processor.go:83-104 (transactions before Finalize)
Source: params/config.go:1534 (IsBoho inclusive of BohoBlock)
Observable: state

예: testnet 의 block `14_408_500` 은 `GovMinter` v1 코드로 transaction 을 실행하며, 이때 `P256VERIFY` 를 쓸 수 있다. 그 block 의 실행 후 state 에서 `GovMinter` 코드는 v2 이다. `0x…1003` 의 storage slot 은 하나도 바뀌지 않는다.

---

## 3. Base fee 분배

StableNet 에서 base fee 는 영구히 태워지지 않는다. transaction 마다 지불자는 `gas_used × effective_gas_price` 를 내고 coinbase 는 `gas_used × effective_tip` 만 받는다. 그래서 `gas_used × base_fee` 는 실행 중에 유통에서 빠진다(`B-07`). 그 뒤 finalization 이 `header.BaseFee × header.GasUsed` 를 block 의 validator 들에게 diligence 에 비례해 나눠 주고, 나눗셈 나머지는 coinbase 에 준다. 그러므로 수수료는 네이티브 코인 공급량을 바꾸지 않는다.

```python
def distribute_base_fee(header, state) -> None:
    if header.gas_used == 0 or header.number == 0:
        return                                                   # state 를 바꾸지 않는다
    if header.base_fee is None:
        raise Invalid("WBFT: baseFee is nil")
    epoch_block, info = epoch_info_for(chain, header.number, header.parent_hash)   # A-04 §3.1, §3.1 에서 다시 적는다
    total = header.base_fee * header.gas_used                    # 크기 제한 없는 정수
    diligence_sum = 0                                            # 참조 구현에서는 uint64; overflow 할 수 없다, 노트 참조
    for idx in info.validators:
        if idx >= len(info.candidates):
            raise Invalid("WBFT: validator candidate index out of range")
        diligence_sum += info.candidates[idx].diligence
    dust = total
    if diligence_sum != 0:
        for idx in info.validators:                              # 목록 순서, 중복 포함
            c = info.candidates[idx]
            share = total * c.diligence // diligence_sum         # 버림 나눗셈
            if share == 0:
                continue
            dust -= share
            if share >= 2**256:
                raise Invalid("WBFT: share overflows uint256")
            state.add_balance(c.addr, share)
    if dust < 0:
        raise Invalid("WBFT: negative dust after base fee distribution")
    if dust != 0:
        if dust >= 2**256:
            raise Invalid("WBFT: dust overflows uint256")
        state.add_balance(header.coinbase, dust)
```

[SNET-FIN-007] `is_london(n)` 이 참이면 노드는 반드시 `distribute_base_fee` 를 위와 정확히 같게 실행해야 한다. `header.GasUsed == 0` 이거나 `n == 0` 이면 노드는 반드시 state 를 바꾸지 않아야 하며, 이 경우 그 뒤의 조건은 하나도 평가해서는 안 된다. 특히 이때는 `BaseFee` 가 없어도 오류가 아니다.
Source: consensus/wbft/engine/engine.go:941-946, 972-975
Observable: state

[SNET-FIN-008] 분배하는 금액은 반드시 finalize 하는 header 에서 가져온 `header.BaseFee × header.GasUsed` 여야 한다. import 경로에서는 받은 header 의 값을 쓴다. 이 값이 실행한 gas 와 다르면 그 뒤에 `B-03` SNET-BODY-007 이 불일치를 잡아 block 을 무효로 만든다.
Source: consensus/wbft/engine/engine.go:985-986
Source: core/block_validator.go:126-128
Observable: state

[SNET-FIN-009] `info.validators` 의 각 validator 항목 `idx` 는 목록 순서대로 `floor(total × candidates[idx].diligence / diligence_sum)` 이 0 이 아니면 반드시 그 값을 받아야 한다. 여기서 `diligence_sum` 은 같은 목록에 대한 합이다. 같은 인덱스가 목록에 두 번 나오면 그 인덱스는 합에서 두 번 세고, 몫도 두 번 받는다. 몫이 0 이면 노드는 그 몫을 적립해서는 안 되며, 그 계정을 건드려서도 안 된다.
Source: consensus/wbft/engine/engine.go:988-1035
Source: core/state/state_object.go:94-96, :400-408 (a zero `AddBalance` touches an empty account), core/state/statedb.go:882-895 (touched empty accounts are deleted), consensus/wbft/engine/engine.go:967 (`IntermediateRoot(IsEIP158)`)
Observable: state

이유 (informative). state 의 `AddBalance` 로 0 을 적립하는 것은 아무 일도 하지 않는 호출이 아니다. 이 호출은 필요하면 계정 객체를 만들고, 그 계정이 비어 있으면 touch 된 것으로 표시한다. 빈 계정이란 nonce 가 0, 잔액이 0, 코드가 없고 `Extra` 가 0 인 계정이다(`B-04` SNET-SYS-072). StableNet chain 에서는 모든 Ethereum fork 가 block 0 에 있으므로(`B-01` §11.1) EIP-158 이 활성이다. 그래서 touch 된 빈 계정은 state 를 마무리할 때 지워진다. state trie 에 있는 빈 계정이라면 이 삭제가 state root 를 바꾸고, 없는 계정이라면 아무 효과가 없다. 그러므로 몫 0 을 적립하는 구현은, 몫이 0 인 validator 가 기존의 빈 계정을 가지고 있을 때마다 다른 state root 를 계산한다. 나머지가 0 일 때도 마찬가지이다(SNET-FIN-010).

[SNET-FIN-010] 나머지 `total − Σ shares` 가 0 이 아니면 노드는 반드시 그 나머지를 `header.Coinbase` 에 적립해야 한다. `diligence_sum == 0` 이면 `total` 전체가 나머지이다.
Source: consensus/wbft/engine/engine.go:1006, 1037-1046
Observable: state

구현 노트 (informative). header 검증을 통과한 block 은 여러 오류 경로에 도달할 수 없다. 이유는 다음과 같다. 모든 London header 에는 `BaseFee` 가 있다(`B-03` SNET-BHDR-010). 디코드한 `EpochInfo` 에는 nil candidate 가 없다. `BaseFee <= MaxBaseFee` 이고 `GasUsed < 2^63` 이므로 `share <= total < 2^256` 이다. 버린 몫들의 합은 `total` 을 넘지 않으므로 dust 는 음수가 되지 않는다. 참조 구현에서 `diligence_sum` 은 `uint64` 이다. diligence 하나는 최대 `2 × 10^6` 이므로(`A-04`), overflow 하려면 validator 가 `9 × 10^12` 보다 많아야 한다.

### 3.1 어느 validator 집합에 지급하는가

```python
# A-04 §3.1 의 epoch_info_for 를 여기서 평가하는 방식 (batch `parents` 없음)
def epoch_info_for(chain, number, parent_hash) -> tuple[int, EpochInfo]:
    e = last_epoch_block(number - 1)               # A-04: n-1 이하에서 가장 큰 epoch block
    h = canonical_header(e) or ancestor_of(parent_hash, e)   # canonical 먼저 (A-04)
    info = decode_extra(h).epoch_info
    if info is None:
        raise Invalid("WBFT: epochInfo is nil")
    return e, info
```

[SNET-FIN-012] block `n` 에 대해 지급받는 validator 집합은 반드시 `n − 1` 이하의 마지막 epoch block 의 extra 에 저장된 `EpochInfo` 여야 한다. 이 `EpochInfo` 는 `A-04` 에서 `validators_at(n)` 을 정의하는 `EpochInfo` 와 같다. epoch block `n` 에서 지급받는 집합은 block `n` 에 새로 쓰는 정보가 아니라 이전 epoch 의 정보이다. 첫 epoch 의 block `1 .. E` 에서 지급받는 집합은 genesis `EpochInfo` 이다(`B-02` SNET-GEN-007).
Source: consensus/wbft/engine/engine.go:1399-1447 (GetEpochInfo, getEpochInfo, extractEpochInfo)
Observable: state

### 3.2 계산 예

**예 1 (dust).** `BaseFee = 20_000_000_000_000`, `GasUsed = 21_000` 이므로 `total = 420_000_000_000_000_000` 이다. epoch 정보의 candidate 는 `A (1_900_000)`, `B (1_000_000)`, `C (2_000_000)`, `D (1_900_000)` 이고, `validators = [2, 0, 1]` 이다. 따라서 D 는 candidate 이지만 validator 는 아니다. `diligence_sum = 2_000_000 + 1_900_000 + 1_000_000 = 4_900_000` 이다.

| 순서 | Validator | Diligence | 몫 `= total × d // 4_900_000` |
|---|---|---|---|
| 1 | C | 2_000_000 | `171_428_571_428_571_428` |
| 2 | A | 1_900_000 | `162_857_142_857_142_857` |
| 3 | B | 1_000_000 | `85_714_285_714_285_714` |

`Σ shares = 419_999_999_999_999_999` 이므로 dust `= 1` 이고, 이 dust 는 coinbase 에 적립된다. coinbase 가 A 이면 A 의 잔액은 모두 `162_857_142_857_142_858` 늘어난다. D 는 아무것도 받지 않는다.

**예 2 (diligence 가 모두 0).** `total` 은 같고, `validators = [0, 1]` 이며, 두 validator 의 diligence 는 모두 0 이다. `diligence_sum = 0` 이므로 몫을 계산하지 않고, coinbase 가 `420_000_000_000_000_000` 을 받는다.

**예 3 (testnet 의 첫 epoch).** block `1 .. 140` 은 genesis `EpochInfo` 를 쓴다. 이 정보에는 validator 가 일곱이 있고, 각 validator 의 diligence 는 `1_900_000` 이다. `total = 420_000_000_000_000_000` 이면 각 validator 는 `60_000_000_000_000_000` 을 받고 dust 는 0 이다.

**예 4 (빈 block).** `GasUsed = 0` 이면 base fee 가 얼마이든 잔액은 바뀌지 않는다.

---

## 4. Epoch 정보

다음 epoch 정보의 내용(diligence 를 갱신한 candidate, shuffle 한 validator 인덱스, BLS key)은 `A-04` 의 `compute_next_epoch_info` 가 계산한다. 이 절은 그 계산이 언제 실행되는지, 어느 state 위에서 실행되는지, 두 경로가 그 결과를 어떻게 쓰는지만 정한다.

[SNET-FIN-013] epoch block `n` (`A-04` `is_epoch_block`)에서 다음 epoch 정보는 반드시 `process_finalize` 의 1 단계와 2 단계가 만든 state 로 계산해야 한다. 그 state 는 block `n` 의 모든 transaction, `n` 의 upgrade, base fee 분배를 마친 state 이다. 노드는 candidate 주소와 BLS key 를 `n` 에서 유효한 `GovValidator` 에서 읽는다(`B-01` SNET-CFG-019, `B-08`). 그러므로 block `n` 안에서 `GovValidator` 의 validator 집합을 바꾸는 transaction 은 바로 다음 epoch 에 영향을 준다.
Source: consensus/wbft/engine/engine.go:950-953 (handler receives the finalization state)
Source: consensus/wbft/engine/engine.go:806, 875, 894 (candidates and BLS keys read from that state)
Observable: header, state

[SNET-FIN-014] proposer 경로(`write_epoch`)에서 proposer 는 반드시 4 단계와 5 단계 전에 계산한 정보를 `header.Extra.EpochInfo` 에 써야 한다. 그래야 block hash 가 그 정보를 덮는다. proposer 자신의 주소가 `validators_at(n)` 에 없으면 참조 구현은 아무것도 쓰지 않으며, 그렇게 만들어진 block 은 SNET-FIN-015 에 따라 무효이다.
Source: consensus/wbft/engine/engine.go:1193-1207 (writeEpoch)
Observable: header

> 해설: 즉 이 경우 참조 구현은 오류를 내는 대신 무효 block 을 만든다.

[SNET-FIN-015] import 경로(`verify_epoch`)에서 노드는 반드시 정보를 다시 계산해야 하며, 다음 가운데 하나라도 해당하면 반드시 그 block 을 거부해야 한다. 비교는 정확히, 위치별로, 아래 순서대로 한다.
1. header 의 `EpochInfo` 가 없다("WBFT: epochInfo is nil").
2. candidate 수가 다르다("WBFT: mismatch in candidate sizes").
3. 같은 인덱스의 candidate 주소나 diligence 가 하나라도 다르다.
4. validator 수가 다르다("WBFT: mismatch in validator sizes").
5. validator 인덱스가 하나라도 다르다("WBFT: The two validators do not match").
6. BLS key 수가 다르다("WBFT: mismatch in BLS public key sizes").
7. BLS key 바이트가 하나라도 다르다("WBFT: The two BLS public keys do not match").
Source: consensus/wbft/engine/engine.go:1212-1263 (verifyEpoch)
Observable: header

[SNET-FIN-016] epoch block 이 아닌 block 에서는 header extra 가 반드시 `WBFTExtra` 로 디코드되어야 하고, 그 `EpochInfo` 는 반드시 없어야 한다. 그렇지 않으면 그 block 은 무효이다(`ErrEpochInfoIsNotNil`, "epoch info should be nil for non-epoch block").
Source: consensus/wbft/engine/engine.go:954-961
Source: consensus/wbft/common/errors.go:145
Observable: header

---

## 5. Gas tip

모든 block 은 `WBFTExtra.GasTip` 에 그 block 에서 authorized 가 아닌 sender 가 내는 tip 을 싣는다(`B-07`). 이 값은 governance 가 `GovValidator` 컨트랙트에서 정하고, 노드는 parent state 에서 읽는다. 그래서 block 의 transaction 이 실행되기 전에 값이 정해진다.

```python
def required_gas_tip(header) -> int:
    if cfg.Anzeon.SystemContracts is None:
        raise Invalid("WBFT: GovValidator contract is not enabled")     # SNET-CFG-012 이후에는 도달할 수 없다
    parent = header_by_hash(header.parent_hash, header.number - 1)
    if parent is None:
        raise Invalid(ErrUnknownAncestor)
    if parent.root == ZERO_HASH:
        raise Invalid("WBFT: parent state root is empty")
    s = state_at(parent.root)                                           # 실패할 수 있다: state 를 쓸 수 없음
    addr = genesis_system_contracts.govValidator.address                # B-01 SNET-CFG-020
    return uint256(s.get_storage(addr, 0x39))                           # slot 이 비어 있으면 0

def verify_gas_tip(header) -> None:
    extra = decode_extra(header)                                        # 오류 -> 무효
    want = required_gas_tip(header)
    if extra.gas_tip is None or extra.gas_tip != want:
        raise Invalid(GasTipMismatchError(have=extra.gas_tip, want=want))  # "invalid gas tip: have %d, want %d"
```

[SNET-FIN-017] block `n` 에 요구되는 gas tip 은 반드시 parent header 의 `Root` 를 root 로 하는 state 에서, genesis `GovValidator` 주소의 storage slot `0x39` 를 256 비트 값으로 읽은 것이어야 한다. slot 이 비어 있으면 요구되는 gas tip 은 0 이다. 그래서 "컨트랙트를 쓸 수 없음" 이라는 결과는 없다.
Source: consensus/wbft/engine/engine.go:620-645 (getGasTip)
Source: systemcontracts/gov_validator.go:41, 212-215 (slot 0x39; GetGasTip never returns nil)
Observable: header, state

> 해설: 값을 parent state 에서 읽기 때문에, block `k` 에서 실행된 gas tip 제안은 block `k + 1` 의 header 에서 처음 보인다(`B-08` §4). 참조 구현의 `getGasTip` 에는 `systemcontracts.GetGasTip` 이 `nil` 을 돌려주면 `ErrGasTipContractUnavailable` 을 내는 분기가 있다. 그러나 `GetGasTip` 은 slot 값을 `Hash.Big()` 으로 바꾸므로 `nil` 을 돌려주지 않는다. 그러므로 그 분기에는 도달할 수 없다. 구현자는 거버넌스 컨트랙트가 없을 때 오류를 내는 방어 코드를 추가해서는 안 된다. header 검증에서 `ErrGasTipContractUnavailable` 은 건너뛰는 오류가 아니라 거부하는 오류로 분류되어 있으므로(WBFT-HDR-111), 그런 방어 코드는 참조 구현이 요구값 0 으로 받아들이는 header 를 거부하게 만든다.

[SNET-FIN-018] finalization 중에 노드는 block 의 `GasTip` 이 없거나 요구되는 gas tip 과 다르면 반드시 그 block 을 거부해야 하며, 어떤 이유로든 요구되는 gas tip 을 정할 수 없을 때도 반드시 거부해야 한다.
Source: consensus/wbft/engine/engine.go:963-965, 1280-1295 (verifyGasTip)
Observable: header

[SNET-FIN-019] header 검증 중에는(`A-08`, `B-03` §7 단계 H21) 같은 검사가 `GasTip` 이 다를 때 반드시 block 을 거부해야 한다. 디코드한 `GasTip` 은 항상 있으므로 `GasTip` 이 없는 경우는 생기지 않는다(`A-03` WBFT-ENC-008). 그러나 이 검사의 다른 실패로는 block 을 거부해서는 안 된다. 다른 실패란 parent state 를 쓸 수 없는 경우(예: snap sync 중), parent 가 없는 경우, parent root 가 0 인 경우, system contract 설정이 없는 경우, 검사 안에서 extra 디코드가 실패하는 경우이다(`A-08` WBFT-HDR-111). 그런 경우 검사는 finalization 으로 미뤄지며, 그 block 이 실행될 때에만 이루어진다.
Source: consensus/wbft/engine/engine.go:329-348
Observable: header

[SNET-FIN-020] proposer 는 header 를 준비할 때(`A-08`) 반드시 `GasTip = required_gas_tip(header)` 를 extra 에 써야 한다. 그래야 proposer 자신의 block 이 SNET-FIN-018 을 만족한다.
Source: consensus/wbft/engine/engine.go:511-513, 518, 537, 542 (Prepare: WriteGasTip)
Observable: header

버전 노트. `v1.1.0` 에서는 비교가 `extra.GasTip != nil && extra.GasTip != want` 였다. 이 차이는 관찰할 수 없다. 디코드한 `GasTip` 은 절대 `nil` 이 아니기 때문이다(A-03 [WBFT-ENC-008]). RLP decoder 는 `rlp:"nil"` pointer 규칙보다 먼저 `*big.Int` 를 처리한다(`rlp/decode.go:161-162`). 그래서 빈 `gas_tip` 항목은 0 으로 디코드되고, 두 버전 모두 0 을 governance 값과 비교한다.
Source: `git -C go-stablenet show v1.1.0:consensus/wbft/engine/engine.go` line 1291

---

## 6. State root, block 조립, 두 경로

[SNET-FIN-021] finalize 된 state root 는 반드시 finalization state 의 `intermediate_root(delete_empty = is_eip158(n))` 이어야 한다. 이 계산은 touch 된 빈 계정을 지우며, StableNet 에서 `is_eip158` 은 참이다. header `UncleHash` 는 반드시 빈 list 의 hash 여야 한다.
Source: consensus/wbft/engine/engine.go:967-968
Observable: header, state

[SNET-FIN-022] proposer 경로에서 proposer 는 반드시 finalize 된 header 로 block 을 조립해야 한다. 조립한 block 은 다음을 만족한다.
1. `TxHash` 는 transaction root 이며, transaction 이 없으면 `EMPTY_ROOT_HASH` 이다.
2. `ReceiptHash` 는 receipt root 이며, receipt 가 없으면 `EMPTY_ROOT_HASH` 이다.
3. `Bloom` 은 receipt bloom 들의 OR 이며, receipt 가 없으면 0 이다.
4. uncle 과 withdrawal 이 없다.
Source: consensus/wbft/engine/engine.go:1053-1061 (FinalizeAndAssemble)
Source: core/types/block.go:243-273 (NewBlock)
Observable: header

### 6.1 Proposer 경로와 import 경로

두 경로는 누가 무엇을 실행하는지가 다르다.

- **Proposer.** proposer 는 header 를 준비하고(`A-08`), 자신의 state 에서 transaction 을 실행하고, `write_epoch` 로 `process_finalize` 를 실행한 뒤, block 을 합의에 넘긴다. block 이 같은 hash 로 commit 되면 노드는 그 block 과 만들 때 계산한 state 를 저장한다. 노드는 그 block 의 header 를 검증하지 않고, 다시 실행하지 않으며, state 검증도 하지 않는다.
- **투표 전의 다른 validator.** 참조 구현의 `validate_proposal` 은 transaction root, uncle hash, 그리고 `B-03` §7 의 header 규칙 H1-H21 가운데 seal 단계 H17 을 뺀 나머지를 검사한다(`A-08` 단계 P1-P7). 이 함수는 transaction 을 실행하지 **않으며** finalization 도 실행하지 않는다. 그러므로 validator 는 실행 결과를 계산하지 않은 block 에 PREPARE 와 COMMIT 을 보낸다.
- **commit 뒤의 나머지 모든 노드.** seal 이 붙은 commit 된 block 은 import 경로(`B-03` §7, 단계 H1-S1 전체)로 삽입된다. 이 경로는 block 을 실행하고 `verify_epoch` 로 `process_finalize` 를 실행한다.

[SNET-FIN-023] 적합한 노드는 반드시 `B-03` §7 의 모든 단계를 통과할 때, 그리고 그때에만 import 경로에서 block 을 받아들여야 한다. 노드가 그 block 에 투표했는지, 그 block 을 제안했는지는 그 판정을 바꿔서는 안 된다.
Source: core/blockchain.go:1587, 1779-1792 (import path)
Source: consensus/wbft/backend/backend.go:213-250 (Commit: own proposal to Seal, others to the fetcher)
Source: miner/worker.go:819-873 (resultLoop: WriteBlockAndSetHead with the build-time state, no re-execution)
Observable: header, state

[SNET-FIN-024] proposal 을 실행하지 않고 투표하는 노드도, commit 된 block 이 `B-03` §7 의 `B1`, `E1`, `F1`, `S1` 가운데 하나라도 통과하지 못하면 import 할 때 반드시 그 block 을 거부해야 한다. 실행하지 않고 투표하는 것은 참조 구현의 동작이다(`A-09` WBFT-APP-050). 유효성은 투표에 의존하지 않는다.
Source: consensus/wbft/engine/engine.go:160-186 (VerifyBlockProposal: no execution)
Source: consensus/wbft/backend/backend.go:257-278 (Verify)

구현 노트 (informative). 투표가 실행에 의존하지 않으므로, proposer 는 정직한 노드가 import 할 수 없는 block 에 대해 quorum certificate 를 얻을 수 있다. 그 height 에 commit 된 block 이 있는데 아무 노드도 그 block 을 삽입할 수 없을 때 합의 계층이 무엇을 하는지는 Part B 의 범위 밖이다. 노드는 그 block 을 bad block 으로 기록하고, round timer 가 지난 뒤 그 height 를 다시 결정한다(`A-09` §5.3, WBFT-APP-100; `A-10` §9).
