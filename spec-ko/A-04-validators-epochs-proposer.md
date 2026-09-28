# A-04 Validator, epoch, proposer 선택

- Areas: `VAL` (fault threshold, quorum, height 별 validator set), `EPOCH` (epoch 경계, 다음 epoch 계산, genesis epoch 정보), `PROP` (proposer 선택)
- Status: draft
- Reference implementation: go-stablenet `740526d03`. 이 장이 사용하는 파일은 `v1.1.0` 에서도 같다. 예외는 `consensus/wbft/engine/engine.go:1291`(gas tip 검사)이며, 이 장은 그 부분을 사용하지 않는다.
- Depends on: `A-01` (타입, `config_at`), `A-02` (`keccak256`), `A-03` (`WBFTExtra`, `EpochInfo`, `SealerSet` 인코딩). Used by: `A-05` (quorum, proposer, F+1 규칙), `A-08` (seal 과 signer 검사), `B-06` (finalization), `B-08` (`candidates` 와 `bls_public_key` 의 binding).

---

## 1. 개요 (informative)

WBFT 는 block height 마다 고정되고 순서가 정해진 validator set 안에서 QBFT 방식 protocol 의 round 를 진행한다. 이 장은 다른 모든 장이 기대는 네 가지 질문에 답한다.

1. 일치하는 메시지가 몇 개 모이면 quorum 이 되는가. 그리고 몇 개가 모이면 이른 round change 가 일어나는가 (2절).
2. 어떤 순서의 validator set 이 어떤 BLS key 로 block `n` 을 seal 하는가 (3절).
3. 어떤 block 이 epoch block 인가. epoch block 은 header 에 다음 epoch 의 `EpochInfo` 를 싣는 block 이다 (4절).
4. view `(n, r)` 의 proposer 는 어느 validator 인가 (5절). 그리고 다음 epoch 의 `EpochInfo` 는 chain 의 기록과 contract state 에서 어떻게 계산하는가 (6절). 이 질문은 genesis 의 경우도 포함한다 (7절).

validator set 은 epoch 경계에서만 바뀐다. epoch block `e` 는 epoch `(L, e]` 를 닫는다. 여기서 `L = last_epoch_block(e - 1)` 이다. block `L+1 .. e` 는 모두 `L` 의 header 에 기록된 set 이 seal 하고, `e` 의 header 는 block `e+1 .. e_next` 에 쓸 set 을 기록한다. 모든 노드는 다음 set 을 아래 세 입력에서 결정론적으로 계산한다.

1. 첫째 입력은 닫히는 epoch 의 header 들에 기록된 previous-block seal 이다.
2. 둘째 입력은 block `e` 의 post-state 에서 읽은 candidate 목록과 BLS key 이다.
3. 셋째 입력은 block `e` 의 randao mix 이다. 이 값은 shuffle 의 seed 로 쓰인다.

아래에서 쓰는 표기는 다음과 같다. `D = DILIGENCE_DENOMINATOR = 1_000_000` 이고 `DEFAULT_DILIGENCE = 1_900_000` 이다. 두 값은 모두 `A-01` 에서 정의한다(`core/types/istanbul.go:43,46`). `ZERO_ADDRESS` 는 0 바이트 20개이다. 정수 나눗셈 `//` 는 Python 과 같은 floor 나눗셈이다. 6절에서는 나누는 수와 나뉘는 수가 모두 음수가 아니므로, 6절의 `//` 는 0 쪽으로 버리는 나눗셈과 결과가 같다. `validators_diff` 와 `being` 은 음수가 될 수 있지만, 이 두 값은 나눗셈에 쓰이지 않는다.

---

## 2. Fault threshold 와 quorum

### 2.1 정의

reference 구현은 fault threshold 를 IEEE-754 binary64 값으로 계산하고, 부동소수점 올림(ceiling)으로 quorum 을 구한다. 아래 의사코드는 그 계산을 그대로 재현한다. 2.2절은 그 값과 정확히 같은 정수 결과를 보여 준다.

```python
def f_value(n: int) -> float64:
    # n - 1 is computed as a signed integer (n = 0 gives -1), then converted.
    return float64(n - 1) / float64(3)          # binary64 division, round-to-nearest-even

def quorum_size(n: int) -> int:
    return int(ceil(float64(n) - f_value(n)))   # binary64 subtraction, then ceiling

def f_plus_one_threshold(n: int) -> int:
    # The unique integer x with f_value(n) < x <= f_value(n) + 1 (see A-05, ROUND-CHANGE).
    return (n - 1) // 3 + 1                     # floor division; n = 0 gives 0
```

[WBFT-VAL-001] 크기가 `n` 인 validator set 의 fault threshold 는 반드시 위에서 정의한 `f_value(n)` 이어야 하며, binary64 반올림도 그대로 따라야 한다.
Source: consensus/wbft/validator/default.go:222 (F)

[WBFT-VAL-002] 크기가 `n` 인 validator set 의 quorum 은 반드시 `quorum_size(n)` 이어야 한다. `n < 3·2^50 + 3` 인 모든 `n` 에 대해 이 값은 `(2 * n) // 3 + 1` 과 같다. 현실적인 set 크기는 모두 이 범위에 든다. `n = 3·2^50 + 3` 은 binary64 반올림 때문에 두 값이 처음으로 달라지는 크기이다(`A-10` §2). reference 구현이 "quorum" 이라고 부르는 합의 문턱값은 모두 이 값을 쓴다. 그 문턱값은 PREPARE 와 COMMIT 의 quorum, ROUND-CHANGE quorum, justification, 그리고 header 의 aggregated seal 에 필요한 최소 sealer 수이다.
Source: consensus/wbft/validator/default.go:226-229 (QuorumSize); consensus/wbft/core/prepare.go:121; consensus/wbft/core/commit.go:123; consensus/wbft/core/roundchange.go:146,170; consensus/wbft/core/preprepare.go:136; consensus/wbft/engine/engine.go:1341-1343 (verifyAggregatedSeal)
Observable: header, network

[WBFT-VAL-003] `A-05` 의 이른 round change 규칙은 validator 수 `num` 을 `float64(num) > f_value(n) and float64(num) <= f_value(n) + 1` 로 비교한다. `n < 2^53 + 2` 인 모든 `n` 에 대해 이 구간을 만족하는 정수는 정확히 하나이며, 그 정수는 `f_plus_one_threshold(n)` 이다. 구현은 정수 형태를 써도 된다.
Source: consensus/wbft/core/roundchange.go:161

구현 노트 (informative). WBFT-VAL-002 의 등가성은 `n < 3·2^50 + 3` 에서 성립한다. `(n - 1) mod 3 = 0` 이면 나눗셈이 정확하므로 `n - f` 는 정수이다. 그렇지 않으면 `n - f = (2n + 1)/3` 의 소수부는 1/3 이나 2/3 이다. `n` 이 작은 동안에는 두 binary64 연산의 반올림 오차와 비교하면 이 소수부는 어느 정수와도 충분히 멀다. `n = 3·2^50 + 3` 에서는 반올림된 `f` 와 반올림된 뺄셈 때문에 `n - f` 가 정수에 놓이고, `quorum_size(n)` 이 `(2 * n) // 3 + 1` 보다 하나 작아진다(`A-10` §2). WBFT-VAL-003 의 구간은 `n < 2^53 + 2` 인 모든 `n` 에서 정수를 정확히 하나 담는다. 2^53 보다 작으면 `float64(n - 1)` 이 정확하고 `f` 의 반올림 오차가 1/3 보다 작기 때문이다. `2^53` 과 `2^53 + 1` 은 계산으로 확인했다. `n = 2^53 + 2` 는 구간이 `f_plus_one_threshold(n)` 대신 `f_plus_one_threshold(n) - 1` 을 담는 첫 크기이다. WBFT-VAL-002 와 WBFT-VAL-003 의 등가성은 `0 <= n <= 3_000_000` 에 대한 전수 검사와 두 경계 부근의 계산으로도 확인했다(Python, binary64 산술).

> 해설: reference 가 실수로 계산하기 때문에 다른 언어로 옮길 때 부동소수점 동작까지 흉내 내야 하는지 걱정할 수 있다. 그러나 위 구현 노트가 보이듯 현실적인 validator 수에서는 결과가 정수식과 정확히 같으므로, 구현은 `(2 * n) // 3 + 1` 과 `(n - 1) // 3 + 1` 을 쓰면 된다. F+1 규칙을 `count >= f + 1` 로 구현하면 안 된다는 점에 주의한다. reference 의 구간 비교는 count 가 `f_plus_one_threshold(n)` 에 도달하는 순간에만 참이 되고, count 가 그 값을 넘어선 뒤에는 참이 되지 않는다. 이 차이가 만드는 문제는 `A-05` 에서 다룬다.

### 2.2 표

이 표는 reference 코드를 실행해 만들었다. `go test -overlay` 로 package `consensus/wbft/engine` 에 overlay test 파일을 넣었다. 이때 reference tree 는 바꾸지 않았다. 그다음 `validator.NewSet(...).F()` 와 `.QuorumSize()` 를 호출했으며, `num = 0 .. n+2` 를 `roundchange.go:161` 의 구간과 비교했다.

| N | F = `f_value(N)` (binary64, `%.17g`) | Q = `quorum_size(N)` | F+1 문턱값 | QBFT 논문 `ceil((N+f+1)/2)`, `f = (N-1)//3` | Quorum `ceil(2N/3)` |
|---|---|---|---|---|---|
| 0 | -0.33333333333333331 | 1 | 0 | 1 | 0 |
| 1 | 0 | 1 | 1 | 1 | 1 |
| 2 | 0.33333333333333331 | 2 | 1 | 2 | 2 |
| 3 | 0.66666666666666663 | 3 | 1 | 2 | 2 |
| 4 | 1 | 3 | 2 | 3 | 3 |
| 5 | 1.3333333333333333 | 4 | 2 | 4 | 4 |
| 6 | 1.6666666666666667 | 5 | 2 | 4 | 4 |
| 7 | 2 | 5 | 3 | 5 | 5 |
| 8 | 2.3333333333333335 | 6 | 3 | 6 | 6 |
| 9 | 2.6666666666666665 | 7 | 3 | 6 | 6 |
| 10 | 3 | 7 | 4 | 7 | 7 |
| 11 | 3.3333333333333335 | 8 | 4 | 8 | 8 |
| 12 | 3.6666666666666665 | 9 | 4 | 8 | 8 |
| 13 | 4 | 9 | 5 | 9 | 9 |
| 14 | 4.333333333333333 | 10 | 5 | 10 | 10 |
| 15 | 4.666666666666667 | 11 | 5 | 10 | 10 |
| 16 | 5 | 11 | 6 | 11 | 11 |
| 17 | 5.333333333333333 | 12 | 6 | 12 | 12 |
| 18 | 5.666666666666667 | 13 | 6 | 12 | 12 |
| 19 | 6 | 13 | 7 | 13 | 13 |
| 20 | 6.333333333333333 | 14 | 7 | 14 | 14 |
| 21 | 6.666666666666667 | 15 | 7 | 14 | 14 |
| 22 | 7 | 15 | 8 | 15 | 15 |

`N = 0` 은 유효한 chain 에서 나올 수 없다(3.5절). 그래도 reference 구현이 validator set 을 불러오지 못할 때 이 값을 계산하므로 표에 넣었다.

### 2.3 QBFT 와의 차이 (informative)

- QBFT 논문(Moniz, 2020)은 quorum 으로 `ceil((N+f+1)/2)` 를 쓰고, ConsenSys Quorum 의 QBFT 는 `ceil(2N/3)` 을 쓴다. 표에서 두 식은 `N >= 1` 에 대해 같은 값을 준다. WBFT 의 `floor(2N/3) + 1` 은 `N mod 3 = 0` 일 때(`N = 3, 6, 9, ...`) 하나 더 크다. `N mod 3 = 0` 이면 WBFT 의 `N - Q = (N - 1) // 3 = f` 는 다른 두 식의 `N - Q = N / 3 = f + 1` 보다 하나 작다. 그러므로 이 크기에서 WBFT 가 quorum 을 유지하며 견딜 수 있는 crash validator 는 하나 적다(N = 3 이면 1 대신 0, N = 6 이면 2 대신 1). 대신 두 quorum 의 교집합은 더 커진다. 다른 `N` 에서는 세 식이 같은 값을 준다. WBFT 값은 실행해서 얻었으므로 확신도가 [High] 이다. commit `5ffacc48` 의 Quorum 식도 확신도가 [High] 이다. `QuorumSize` 는 `2FPlus1Enabled` transition 이 켜져 있지 않고, `Ceil2Nby3Block` 이 설정되어 있으며 그 값이 현재 sequence 보다 크지 않을 때에만 `ceil(2N/3)` 을 쓴다. 그 밖의 경우에는 `F = ceil(N/3) - 1 = (N - 1) // 3` 로 `2F + 1` 을 쓴다(`consensus/istanbul/qbft/core/core.go:306-313`, `consensus/istanbul/validator/default.go:205`). genesis 파일에 `qbft` 나 `ibft` 절이 있으면 `Ceil2Nby3Block` 의 기본값은 0 이다(`consensus/istanbul/config.go:151`, `eth/ethconfig/config.go:340-359`). 폐기 예정인 `istanbul` 절에 `ceil2Nby3Block` 이 없으면 값은 nil 이고, 그래서 block 0 부터 `2F + 1` 을 쓴다(`eth/ethconfig/config.go:248`). 2.2 절 표의 `ceil(2N/3)` 열은 첫 경우의 값이다. Quorum 의 `2F + 1` 은 `N ≢ 1 (mod 3)` 인 모든 `N` 에서 WBFT 의 quorum 보다 작다(N = 3 이면 3 대신 1, N = 6 이면 5 대신 3). 이와 별개로 Quorum 의 header 검증은 committed seal 을 `F + 1` 개만 요구한다(`consensus/istanbul/qbft/engine/engine.go:283`).
- QBFT 형식 명세(commit `1630128e7`)는 `f(n) = (n − 1) div 3`, `quorum(n) = (2n − 1) div 3 + 1` 로 정의한다(`dafny/spec/L1/node_auxiliary_functions.dfy:244-262`). `N >= 1` 에서 이 quorum 은 표의 Quorum 열인 `ceil(2N/3)` 과 같다. 그러므로 `N mod 3 = 0` 이면 형식 명세의 quorum 도 WBFT 의 quorum 보다 하나 작다. 형식 명세의 F+1 문턱값 `f(n) + 1` 은 `f_plus_one_threshold(n)` 과 같다.
- `validator/default.go:227` 의 코드 주석은 "ceil(2N/3)" 이라고 적지만, 코드는 `ceil(N - F)` 를 계산한다. 이 주석은 Quorum 이 `ceil(2N/3)` 분기에서 남기는 trace 메시지를 주석으로 남긴 것이다(commit `5ffacc48` 의 `consensus/istanbul/qbft/core/core.go:311`). 규범은 코드이다.
- F+1 규칙은 `count >= f + 1` 대신 실수 `F` 와 구간을 쓴다. 이 규칙을 발동시키는 count 는 `f_plus_one_threshold(N)` 하나뿐이다(WBFT-VAL-003).

> 해설: 코드 주석을 믿고 quorum 을 `ceil(2N/3)` 으로 구현하면, `N = 3k` 일 때 quorum 을 하나 적게 잡게 된다. 그러면 그 구현은 reference 가 거부하는 header, 즉 sealer 가 하나 모자란 header 를 받아들이고 chain 이 갈라진다.

---

## 3. Height 별 validator set

### 3.1 정의

`ValidatorSet` 은 `(address: Address, bls_public_key: bytes)` 항목의 순서 있는 목록과, 그 height 에서 유효한 proposer policy 로 이루어진다. 항목의 위치를 *validator index* 라고 한다. `SealerSet` 의 비트 `k`(`A-03`)는 index `k` 를 가리킨다.

```python
def candidate_address(info: EpochInfo, i: uint32) -> Address:
    # Out-of-range indices are not an error here; they map to the zero address.
    if i < len(info.candidates):
        return info.candidates[i].addr
    return ZERO_ADDRESS

def epoch_info_for(chain, number: int, parent_hash: Hash, parents: [Header] = None) -> (int, EpochInfo):
    # EpochInfo that governs block `number` (number >= 1).
    # `parents` (optional): headers not yet stored, oldest first, ending with the parent of
    # `number` (batch verification, A-08 §6.1 and §6.7). In the walk-back they are consulted
    # before stored headers.
    # Lookups use the low 64 bits of `number`; uint64(number) - 1 is
    # uint64 arithmetic and wraps to 2^64 - 1 for number = k*2^64. `L` is computed from it
    # with unbounded arithmetic.
    L = last_epoch_block(uint64(number) - 1)
    epoch_header = header_by_number(L)       # canonical first: A-08 WBFT-HDR-071
    if epoch_header is None:
        # No canonical header stored at L: walk back from the parent of `number`,
        # first through `parents` (last element first), then through stored headers.
        pending = list(parents or [])
        h_hash, h_num = parent_hash, uint64(number) - 1
        while True:
            b = pending.pop() if pending else header(h_hash, h_num)  # A-09 chain queries
            if b is None:
                raise ErrUnknownAncestor
            if b.number == L:                                    # full value
                epoch_header = b
                break
            h_hash, h_num = b.parent_hash, uint64(b.number) - 1
    extra = decode_extra(epoch_header.extra) # A-03; decoding failure is an error
    if extra.epoch_info is None:
        raise Error("WBFT: epochInfo is nil")
    return L, extra.epoch_info

def governing_epoch_info(chain, h: Header) -> (int, EpochInfo):
    # Helper used by section 6: the genesis header governs itself.
    if h.number == 0:
        return 0, decode_extra(h.extra).epoch_info       # error if absent
    return epoch_info_for(chain, h.number, h.parent_hash)

def validators_at(chain, number: int, parent_hash: Hash, parents: [Header] = None) -> ValidatorSet:
    if number == 0:
        cfg = genesis_chain_config.anzeon.init
        entries = [(cfg.validators[k], hex_decode(cfg.bls_public_keys[k]))
                   for k in range(len(cfg.validators))]
        return ValidatorSet(entries, policy=base_config.proposer_policy)
    _, info = epoch_info_for(chain, number, parent_hash, parents)
    entries = [(candidate_address(info, info.validators[k]), info.bls_public_keys[k])
               for k in range(len(info.validators))]
    return ValidatorSet(entries, policy=config_at(number).proposer_policy)

def prev_validators_at(chain, header, parents: [Header] = None) -> ValidatorSet:
    # Set used to verify prev_prepared_seal / prev_committed_seal of `header`.
    if uint64(header.number) >= 2:
        if parents:
            # 마지막 원소를 parent 로 쓴다. hash 는 비교하지 않는다.
            parent, rest = parents[-1], parents[:-1]
        else:
            parent, rest = header(header.parent_hash, uint64(header.number) - 1), None
        if parent is None:
            raise ErrUnknownAncestor
        return validators_at(chain, parent.number, parent.parent_hash, rest)
    return validators_at(chain, header.number, header.parent_hash, parents)
```

`base_config` 는 transition 을 적용하기 전의 chain 설정에서 만든 합의 설정이다. `config_at(n)`(`A-01`)은 `block <= n` 인 모든 transition 을 적용한 설정이다.

### 3.2 요구사항

[WBFT-VAL-004] `validators_at(0)` 은 반드시 chain 설정의 `anzeon.init.validators` 목록을 그 순서 그대로 쓴 것이어야 하며, 각 항목은 `anzeon.init.blsPublicKeys` 의 같은 위치에 있는 key 를 hex decode 한 값과 짝을 이룬다. genesis header 자신의 `EpochInfo`(7절)도 같은 목록을 기술한다.
Source: consensus/wbft/engine/engine.go:1104-1108 (GetValidators); params/config_wbft.go:68-74 (GetInitialBLSPublicKeys)

> 해설: epoch block `e` 의 `EpochInfo` 는 block `e` 를 실행한 뒤의 state 에서 계산한다(6절). block 을 실행해야 알 수 있는 값으로 그 block 자신의 sealer 를 정할 수는 없다. 그래서 epoch block 은 자기가 닫는 epoch 의 마지막 block 이 되고, 이전 epoch 의 set 이 epoch block 을 seal 한다. `e` 가 기록한 새 set 은 block `e + 1` 부터 쓰인다. 이 때문에 `validators_at` 은 `number` 가 아니라 `number - 1` 로 `last_epoch_block` 을 부른다.

[WBFT-VAL-006] validator set 의 순서는 반드시 `EpochInfo.validators` 의 순서여야 하고, height 0 에서는 `anzeon.init.validators` 의 순서여야 한다. 노드는 validator set 을 정렬하지 않는다. seal, proposer 선택, quorum 계산에 쓰는 validator index 는 모두 이 순서를 가리킨다.
Source: core/types/istanbul.go:177-187 (EpochInfo.GetValidators); consensus/wbft/validator/validator.go:35-41 (NewSet); consensus/wbft/validator/default.go:71-89 (newDefaultSet)
Observable: header, rpc

구현 노트 (informative). `ProposerPolicy.By` 는 정렬 함수를 담는다. 기본값은 `ValidatorSortByString` 이고, `startWBFT` 가 이 값을 `ValidatorSortByByte` 로 바꾼다. 그러나 이 함수를 호출하는 곳은 없다. `ValidatorSortByFunc.Sort` 는 test 밖에서 호출되지 않는다. 순서가 `EpochInfo` 에서만 온다는 v0.1 초안의 서술은 맞다. Source: consensus/wbft/config.go:45-66,100-103; consensus/wbft/types.go:192-212; consensus/wbft/backend/backend.go:355-358.

> 해설: 코드를 읽는 사람은 `ProposerPolicy.By` 때문에 "validator set 을 주소 순으로 정렬한다" 고 오해하기 쉽다. 실제 순서는 6절의 shuffle 이 정하고, 그 결과가 header 의 `EpochInfo.validators` 에 기록된다.

[WBFT-VAL-007] `EpochInfo.validators` 의 항목이 `EpochInfo.candidates` 의 유효한 index 가 아니면, 그 항목은 반드시 `ZERO_ADDRESS` 로 대응시켜야 한다. 그런 항목은 조회할 때 오류가 아니다. 6절이 계산한 `EpochInfo` 에는 그런 항목이 나타날 수 없다.
Source: core/types/istanbul.go:189-194 (GetCandidate)

[WBFT-VAL-009] `number` 에 적용되는 epoch header 를 찾을 수 없거나, decode 할 수 없거나, 그 header 에 `EpochInfo` 가 없으면 `validators_at(number)` 는 실패한다. 현재 validator set 을 정할 수 없는 header 는 반드시 거부해야 한다. 이때 reference 는 `consensus.ErrUnknownAncestor` 를 돌려준다. previous-block set 을 정할 때 parent header 를 찾지 못하면 reference 는 `consensus.ErrUnknownAncestor` 를 돌려준다. 그 밖의 이유로 previous-block set 을 정하지 못하면 reference 는 원래의 오류를 그대로 돌려준다.
Source: consensus/wbft/engine/engine.go:1438-1447 (extractEpochInfo); consensus/wbft/backend/engine.go:420-450 (GetValidatorsForVerifying)
Observable: header

[WBFT-VAL-010] height `n >= 2` 인 header 의 previous-block seal 은 반드시 `validators_at(n - 1)` 로 검증해야 한다. `n <= 1` 이면 `prev_validators_at` 은 `validators_at(n)` 을 돌려주지만, 그 값은 쓰이지 않는다. parent 가 genesis block 이면 previous-block seal 을 검증하지 않기 때문이다(`A-08` 참조).
Source: consensus/wbft/backend/engine.go:429-447; consensus/wbft/engine/engine.go:322-327,384-388
Observable: header

> 해설: `prev_prepared_seal` 과 `prev_committed_seal` 은 parent block 에 대한 seal 이므로 parent 의 set 으로 검증한다. parent 가 epoch block 이면 parent 의 set 은 child 의 set 과 다르다. 이때 bitmap index 를 child 의 set 으로 해석하면, 검증하는 노드는 엉뚱한 public key 를 더하게 된다.

[WBFT-VAL-011] `validators_at(n)` 에 붙는 proposer policy 는 반드시 `n >= 1` 이면 `config_at(n).proposer_policy` 여야 하고, `n = 0` 이면 기본 policy 여야 한다.
Source: consensus/wbft/engine/engine.go:1106,1115; consensus/wbft/config.go:161-184 (GetConfig)

구현 노트 (informative). 의사코드는 reference 의 조회 방법을 따른다. 조회는 먼저 canonical chain 에서 height 로 header 를 찾는다(`GetHeaderByNumber`). 그 height 에 canonical header 가 저장되어 있지 않을 때에만 조회는 chain 을 거슬러 올라간다. 거슬러 올라갈 때 조회는 먼저 검증 중인 header 묶음인 `parents` 를 마지막 원소부터 보고(`A-08` §6.1), 그다음 `parent_hash` 에서 시작해 저장된 header 를 본다. Source: consensus/wbft/engine/engine.go:1414-1435.

구현 노트 (informative). 합의 코어는 다음 sequence 의 set 을 `Backend.Validators(lastProposal)` 로 얻는다. 이 함수는 `validators_at` 이 실패하면 오류를 알리지 않고 빈 set 을 돌려준다. Source: consensus/wbft/backend/backend.go:319-325. 빈 set 이 가져오는 결과는 5.4절에서 설명한다.

RPC (informative). `istanbul_getValidators(number)` 와 `istanbul_getValidatorsAtHash(hash)` 는 `validators_at(number)` 의 주소를 순서대로 돌려준다. Source: consensus/wbft/backend/api.go:132-163; consensus/wbft/backend/engine.go:232-239.

---

## 4. Epoch 경계

### 4.1 알고리즘

```python
def epoch_schedule(number: int) -> (int, int):
    # Returns (anchor, length) in force at `number`.
    length = base_config.epoch            # chain config anzeon.wbft.epochLength (>= 2, B-01)
    anchor = 0
    for t in transitions:                 # sorted by ascending t.block (B-01)
        if t.block > number:
            break
        if t.epoch_length == 0:           # transition does not change the epoch length
            continue
        anchor = t.block                  # every epoch-length transition block is an epoch block
        length = t.epoch_length
    return anchor, length

def is_epoch_block(number: int) -> bool:
    anchor, length = epoch_schedule(number)
    return (number - anchor) % length == 0

def last_epoch_block(number: int) -> int:
    anchor, length = epoch_schedule(number)
    return number - (number - anchor) % length

def epoch_length_of(e: int) -> int:
    # Number of blocks in the epoch closed by epoch block e >= 1.
    return e - last_epoch_block(e - 1)
```

[WBFT-EPOCH-001] 노드는 block 번호 `n` 을 `is_epoch_block(n)` 이 참일 때, 그리고 그때에만 반드시 epoch block 으로 다뤄야 한다. `epoch_schedule` 은 위에서 정의한 것이다. block 0 은 epoch block 이다.
Source: consensus/wbft/engine/engine.go:1073-1090 (IsEpochBlockNumber)
Observable: header

[WBFT-EPOCH-002] `last_epoch_block(n)` 은 반드시 `n - ((n - anchor) mod length)` 여야 한다. 이 값은 `n` 이하인 epoch block 가운데 가장 큰 것이다.
Source: consensus/wbft/engine/engine.go:1087-1089

[WBFT-EPOCH-003] 노드는 `epochLength` 가 0 이 아닌 모든 transition 에서 반드시 schedule 의 기준점(anchor)을 다시 정해야 한다. 그 transition 의 block 은 epoch block 이 되고, 이후의 epoch block 은 그 block 부터 센다. 새 길이가 이전 길이와 같아도 마찬가지이다. `epochLength` 가 0 이거나 없고 다른 WBFT 필드를 하나 이상 가진 transition 은 schedule 에 영향을 주지 않는다.
Source: consensus/wbft/engine/engine.go:1076-1086

[WBFT-EPOCH-006] epoch block 이 아닌 block `n >= 1` 은 `EpochInfo` 를 가져서는 안 된다. 즉 그 block 의 `epoch_info` 필드는 RLP-nil 이다(`A-03`). 이 규칙을 어긴 block 은 실행 단계(`process_finalize`, `B-06`)에서 `ErrEpochInfoIsNotNil` 로 거부된다.
Source: consensus/wbft/engine/engine.go:948-961 (processFinalize)
Observable: header

> 해설: 이 검사는 header 검증이 아니라 block 실행에서 한다. `EpochInfo` 가 실행 단계에서만 검사된다는 성질은 6.5절에서 다시 문제가 된다.

### 4.2 계산 예: transition

설정은 `epochLength = 10` 이고, transition 은 `{block 25, epochLength 7}`, `{block 40, blockPeriodSeconds 2, proposerPolicy 1}`, `{block 50, epochLength 7}` 이다. 아래 값은 reference 코드의 `Engine.IsEpochBlockNumber` 와 `Config.GetConfig` 를 호출해 얻었다(overlay test, 2.2절 참조).

`0 .. 70` 범위의 epoch block 은 `0, 10, 20, 25, 32, 39, 46, 50, 57, 64` 이다.

| n | is_epoch_block | last_epoch_block | n 에서의 policy | 비고 |
|---|---|---|---|---|
| 24 | 아니요 | 20 | 0 | 이전 schedule 을 따른다 |
| 25 | 예 | 25 | 0 | transition block 이다. epoch `(20, 25]` 는 block 5개이다 |
| 26 | 아니요 | 25 | 0 | anchor 는 25, 길이는 7 이다 |
| 31 | 아니요 | 25 | 0 | |
| 32 | 예 | 32 | 0 | |
| 45 | 아니요 | 39 | 1 | 40 의 transition 은 policy 만 바꾼다 |
| 49 | 아니요 | 46 | 1 | |
| 50 | 예 | 50 | 1 | 같은 길이 7 로 기준점을 다시 정한다. epoch `(46, 50]` 은 block 4개이다 |
| 56 | 아니요 | 50 | 1 | |
| 57 | 예 | 57 | 1 | |
| 70 | 아니요 | 64 | 1 | |

두 번째 예는 block 하나짜리 epoch 를 보여 준다. `epochLength = 4` 이고 transition `{block 5, epochLength 4}` 가 있으면 epoch block 은 `0, 4, 5, 9, 13, ...` 이다. 이때 epoch `(4, 5]` 의 길이는 1 이다.

---

## 5. Proposer 선택

### 5.1 알고리즘

```python
STICKY = 1

def index_of(vs: ValidatorSet, addr: Address) -> int:
    for k, v in enumerate(vs.entries):
        if v.address == addr:
            return k                      # first match
    return -1

def last_proposer(chain, n: int) -> Address:
    # Input for sequence n (n >= 1): author of the last committed block.
    if n - 1 == 0:
        return ZERO_ADDRESS
    return header_at(n - 1).coinbase

def calc_proposer(vs: ValidatorSet, last: Address, round: uint64, policy) -> Entry | None:
    size = len(vs.entries)
    if size == 0:
        return None
    if last == ZERO_ADDRESS:
        seed = round
    else:
        offset = max(index_of(vs, last), 0)          # not found -> offset 0
        seed = offset + round                        # uint64, wraps modulo 2**64
        if policy.id != STICKY:                      # RoundRobin and every other id
            seed = seed + 1
    return vs.entries[seed % size]

def is_proposer(vs: ValidatorSet, proposer: Entry | None, addr: Address) -> bool:
    k = index_of(vs, addr)
    candidate = vs.entries[k] if k >= 0 else None
    # reflect.DeepEqual on the two entries: both absent -> True;
    # otherwise address and BLS key bytes must be equal.
    if proposer is None or candidate is None:
        return proposer is None and candidate is None
    return proposer.address == candidate.address and proposer.bls_public_key == candidate.bls_public_key
```

### 5.2 요구사항

[WBFT-PROP-001] view `(n, r)` 의 proposer 는 반드시 `calc_proposer(validators_at(n), last_proposer(n), r, config_at(n).proposer_policy)` 여야 한다. sequence `n` 의 모든 round 에서 같은 `last_proposer(n)` 을 쓰고, `r` 만 바뀐다.
Source: consensus/wbft/core/core.go:178,236,246 (startNewRound); consensus/wbft/validator/default.go:140-144 (CalcProposer)
Observable: network

[WBFT-PROP-002] `last_proposer(n)` 은 반드시 `n - 1 > 0` 이면 block `n - 1` 의 `Coinbase` 여야 하고, `n - 1 = 0` 이면 genesis header 의 `Coinbase` 와 무관하게 `ZERO_ADDRESS` 여야 한다.
Source: consensus/wbft/backend/backend.go:327-342 (LastProposal); consensus/wbft/engine/engine.go:86-88 (Author)

[WBFT-PROP-003] RoundRobin policy 에서 노드는 proposer index 를 반드시 다음과 같이 계산해야 한다. `last = ZERO_ADDRESS` 이면 `r mod N` 이고, 그 밖의 경우에는 `(offset(last) + r + 1) mod N` 이다.
Source: consensus/wbft/validator/default.go:146-170 (calcSeed, roundRobinProposer)
Observable: network

[WBFT-PROP-004] Sticky policy 에서 노드는 proposer index 를 반드시 다음과 같이 계산해야 한다. `last = ZERO_ADDRESS` 이면 `r mod N` 이고, 그 밖의 경우에는 `(offset(last) + r) mod N` 이다.
Source: consensus/wbft/validator/default.go:172-184 (stickyProposer)
Observable: network

[WBFT-PROP-005] 노드는 policy 식별자가 1 일 때, 그리고 그때에만 반드시 Sticky policy 를 골라야 한다. 식별자 0 과 그 밖의 모든 값은 RoundRobin 을 고른다.
Source: consensus/wbft/validator/default.go:83-86; consensus/wbft/config.go:37-42,60-62

[WBFT-PROP-006] `offset(last)` 는 반드시 주소가 `last` 와 같은 첫 항목의 index 여야 하고, 일치하는 항목이 없으면 0 이어야 한다. 예를 들어 직전 작성자가 epoch 경계에서 set 을 떠났으면 일치하는 항목이 없다.
Source: consensus/wbft/validator/default.go:122-129,146-152

> 해설: RoundRobin 은 round 0 에서 직전 proposer 의 다음 validator 에게 차례를 넘기고, Sticky 는 round 0 에서 직전 proposer 가 계속 proposer 가 된다. 두 policy 모두 round 가 하나 늘 때마다 다음 index 로 넘어간다.

[WBFT-PROP-007] validator 는 `is_proposer(vs, current_proposer, source)` 가 참일 때, 그리고 그때에만 PRE-PREPARE 를 반드시 proposer 가 보낸 것으로 다루어야 한다. 여기서 `source` 는 recover 한 보낸 사람 주소이다. validator 는 proposer 항목과, 보낸 사람 주소를 가진 첫 항목을 `reflect.DeepEqual` 로 비교한다. 그러므로 두 항목의 주소와 BLS key 바이트가 모두 같아야 참이 된다.
Source: consensus/wbft/validator/default.go:135-138 (IsProposer); consensus/wbft/core/preprepare.go:123-126
Observable: network

### 5.3 계산 예

validator 는 네 명이고 index 는 0..3 이다(주소는 `0x..1000`..`0x..1003`). 값은 reference `CalcProposer` 를 호출해 얻었다(overlay test). 각 칸은 round 0..5 의 proposer index 를 나열한다.

| 직전 proposer | RoundRobin (id 0) | Sticky (id 1) | id 7 |
|---|---|---|---|
| `ZERO_ADDRESS` (sequence 1) | 0 1 2 3 0 1 | 0 1 2 3 0 1 | 0 1 2 3 0 1 |
| index 1 | 2 3 0 1 2 3 | 1 2 3 0 1 2 | 2 3 0 1 2 3 |
| index 3 | 0 1 2 3 0 1 | 3 0 1 2 3 0 | 0 1 2 3 0 1 |
| 직전 proposer 가 set 의 member 가 아니다 | 1 2 3 0 1 2 | 0 1 2 3 0 1 | 1 2 3 0 1 2 |

> 해설: 마지막 행에서 직전 proposer 가 set 에 없으면 `offset = 0` 이므로 RoundRobin 은 index 1 부터 시작한다. 즉 epoch 가 바뀌어 직전 proposer 가 set 에서 빠졌을 때, 첫 proposer 는 index 0 이 아니라 index 1 이다.

### 5.4 경계 사례

---

## 6. 다음 epoch 계산

### 6.1 입력

epoch block `e >= 1` 의 `EpochInfo` 는 다음 값의 함수이다.

1. `L = last_epoch_block(e - 1)` 일 때 header `L+1 .. e` 이다. 계산이 쓰는 것은 그 header 들의 `Coinbase` 와, `prev_prepared_seal` 및 `prev_committed_seal` 의 sealer bitmap 이다. 여기서는 signature 를 다시 검사하지 않는다. header 검증이 이미 검사했기 때문이다.
2. 세 개의 `EpochInfo` 이다. 첫째는 `L` 의 `EpochInfo` 인 `latest` 이다. 둘째는 block `L` 에 적용되는 epoch block 의 `EpochInfo` 이며, block `L+1` 이 싣고 있는 previous-block seal 을 누구의 것으로 볼지 정하는 데 쓴다. 셋째는 block `L - 1` 에 적용되는 epoch block 의 `EpochInfo` 인 `prior` 이며, 이전에 누가 validator 였는지 정하는 데 쓴다.
3. `L` 의 header 이다. proposer replay 는 그 header 의 `Coinbase` 에서 시작한다.
4. block `e` 의 randao mix 인 `e_header.mix_digest`(`A-02`)이다. 계산은 이 값을 shuffle seed 로 쓴다.
5. `post_state(e)` 이다. `post_state(e)` 는 block `e` 의 transaction 을 실행하고, `e` 에 예정된 system contract upgrade 를 적용하고, `e` 의 base fee 를 분배한 뒤의 state 이다. epoch 계산과 state root 계산 사이에는 state 를 바꾸는 것이 없으므로, `post_state(e)` 는 root 가 `e_header.root` 인 state 이다.
6. application interface 연산 `candidates(epoch_header, post_state)` 이다(`A-09` §4.2). StableNet 에서 이 연산의 binding 은 `B-08` 의 `candidates_at_epoch(state_e, e, upgrades)` 이다. 이 연산은 순서가 있고 중복이 없는 candidate 목록을 돌려주며, 각 항목에는 BLS public key 가 붙는다. key 가 없는 항목에는 빈 값이 붙는다. 아래에서 `bls_public_key(state, addr)` 는 `candidates(e_header, state)` 가 `addr` 에 대해 돌려주는 key 를 뜻한다. reference 는 key 를 따로, 그리고 선택된 candidate 에 대해서만 읽는다. 두 방법은 같은 state 를 읽으므로 결과가 같다.
7. `config_at(e).proposer_policy` 이다.

그러므로 `e` 에서 계산할 때 세는 previous-block seal 은 block `L+1 .. e` 에 실린 seal, 즉 block `L .. e-1` 의 seal 이다. block `e` 자신의 seal 은 block `e+1` 에 실리며, 다음 epoch block 의 계산에서 `e` 를 seal 한 set 기준으로 센다.

[WBFT-EPOCH-007] epoch block `e` 에 대해 `candidates` 와 `bls_public_key` 가 읽는 state 는 반드시 위에서 정의한 `post_state(e)` 여야 한다.
Source: consensus/wbft/engine/engine.go:929-970 (processFinalize: upgrades 930-939, base fee 941-946, epoch 948-953, root 967); core/state_processor.go:102
Observable: state

> 해설: `post_state(e)` 가 실행 뒤의 state 라는 점이 중요하다. 이 선택 때문에 `EpochInfo` 는 header 만 보고는 검증할 수 없고 block 을 실행해야 한다(6.5절). 또한 epoch block 자신에 들어 있는 transaction 이 다음 epoch 의 candidate 에 바로 반영된다.

### 6.2 알고리즘

```python
def compute_next_epoch_info(chain, e_header: Header, state) -> EpochInfo:
    e = e_header.number
    assert is_epoch_block(e)                               # otherwise: no EpochInfo
    if e == 0:
        return initial_epoch_info(genesis_chain_config)    # section 7

    # ---- 1. seal accounting over blocks e, e-1, ..., L+1 -------------------------
    L, latest = governing_epoch_info(chain, e_header)      # L = last_epoch_block(e - 1)
    proposed  = defaultdict(int)   # seals carried in blocks the address authored
    submitted = defaultdict(int)   # seals the address contributed
    being     = defaultdict(int)   # proposer opportunities in the epoch
    proposers = []
    first_proposer = ZERO_ADDRESS
    last_prop      = ZERO_ADDRESS
    validators_diff = 0
    epoch_length = 0
    it = e_header
    while it.number != L:
        extra = decode_extra(it.extra)
        proposers.append(it.coinbase)
        parent = header(it.parent_hash, uint64(it.number) - 1)
        info = latest
        if parent.number == L:                             # it is block L+1
            _, info = governing_epoch_info(chain, parent)  # the set that sealed block L
            first_proposer = it.coinbase
            last_prop = parent.coinbase
            validators_diff = len(latest.validators) - len(info.validators)   # may be negative
        if parent.number == 0:
            being[it.coinbase] -= 1                        # block 1 carries no previous-block seals
        else:
            for seal in (extra.prev_prepared_seal, extra.prev_committed_seal):
                signers = signer_addresses(info, seal)
                proposed[it.coinbase] += len(signers)
                for a in signers:
                    submitted[a] += 1
        it = parent
        epoch_length += 1

    # ---- 2. who is (was) a validator ----------------------------------------------
    cmap = {}
    for c in latest.candidates:
        cmap[c.addr] = CandInfo(candidate=c, is_val=False, was_val=False)
    current = []
    for i in latest.validators:
        a = candidate_address(latest, i)
        cmap[a].is_val = True
        current.append(a)
    if L > 0:
        _, prior = governing_epoch_info(chain, header(it.parent_hash, uint64(L) - 1))  # governs block L-1
        for i in prior.validators:
            a = candidate_address(prior, i)
            if a in cmap:
                cmap[a].was_val = True

    # ---- 3. replay proposer opportunities, oldest block first ---------------------
    vs = ValidatorSet(zip(current, latest.bls_public_keys), policy=config_at(e).proposer_policy)
    for author in reversed(proposers):                     # blocks L+1, ..., e
        rnd = 0
        while True:
            if rnd >= len(current):
                raise Error("failed to find valid proposer")
            p = calc_proposer(vs, last_prop, rnd, vs.policy).address
            being[p] += 1
            if p == author:
                break
            rnd += 1
        last_prop = author

    # ---- 4. diligence ----------------------------------------------------------------
    E = epoch_length
    new_candidates = []
    for addr in [c.addr for c in candidates(e_header, state)]:
        ci = cmap.get(addr)
        if ci is None:
            d = DEFAULT_DILIGENCE
        else:
            s = submitted[addr]
            rate = E
            d = s * D // (2 * E)
            if not ci.is_val:
                rate = 1
                d = s * D // 2
            elif not ci.was_val:
                rate = E - 1
                if s > 2 * (E - 1):
                    raise Error("seal count exceed the range for non validator in prior epoch")
                d = s * D // (2 * (E - 1))                 # E == 1: division by zero
            w = being[addr]
            if w > 0:
                max_p = 2 * len(latest.validators) * w
                if addr == first_proposer and ci.was_val:
                    max_p -= 2 * validators_diff
                d += proposed[addr] * D // max_p
            else:
                d += D
            d = (ci.candidate.diligence * (10 * E - rate) + d * rate) // 10 // E
        if d > 2 * D:
            raise Error("WBFT: Invalid Diligence %d exceeds maximum" % d)
        new_candidates.append(Candidate(addr=addr, diligence=d))

    # ---- 5. selection and order ------------------------------------------------------
    order = sort_candidates(new_candidates)                # 6.4
    n = len(order)
    chosen = [order[compute_shuffled_index(i, n, e_header.mix_digest)] for i in range(n)]
    validators, keys = [], []
    for idx in chosen:
        pk = bls_public_key(state, new_candidates[idx].addr)
        if len(pk) == 0:
            continue                                        # candidate kept, not a validator
        validators.append(uint32(idx))
        keys.append(pk)
    return EpochInfo(candidates=new_candidates, validators=validators, bls_public_keys=keys)

def signer_addresses(info: EpochInfo, seal) -> [Address]:
    if seal is None:
        raise Error("empty seals")                          # ErrEmptySeals
    out = []
    for k in sealer_indices(seal.sealers):                  # ascending bit order, A-03
        a = candidate_address(info, info.validators[k])
        if a == ZERO_ADDRESS:
            raise Error("validator address is zero")
        out.append(a)
    return out
```

산술 노트. reference 는 `d`, `s * D`, `proposed * D`, `E`, 그리고 평활화(smoothing) 곱에 `uint64` 를 쓴다. `N` 을 `latest` 의 크기와 block `L` 을 seal 한 set 의 크기 가운데 큰 값이라고 하자. `proposed[addr] <= 2 * N * E` 이므로, `2 * N * E * D < 2**64` 이면 `proposed * D` 는 overflow 하지 않는다. 평활화 곱 `D_prev * (10E - rate) + d * rate` 는 `(20 * D + d) * E < 2**64` 이면 overflow 하지 않는다. 새 항 `d` 는 평활화 전에는 `2D` 이하라는 보장이 없다. 설정된 모든 epoch 길이와 set 크기는 이 한계보다 훨씬 작다. `max_p` 는 부호 있는 정수로 계산한 뒤 `uint64` 로 변환한다. 이전 set 이 비어 있지 않으면 `max_p` 는 항상 양수이다. 연산 순서가 결과에 영향을 준다. 평활화 단계는 먼저 10 으로 나누고 그다음 `E` 로 나누므로, 버림이 두 번 일어난다.

### 6.3 요구사항: 집계와 diligence

[WBFT-EPOCH-008] epoch block `e` 의 seal 집계는 반드시 `e` 의 chain 위에서 정확히 block `L+1 .. e` 만 방문해야 한다(`L = last_epoch_block(e - 1)`). 그리고 방문한 각 block `b >= 2` 에 대해 반드시 다음 credit 을 세어야 한다.
1. `prev_prepared_seal` 의 sealer 비트마다 그 sealer 에게 `submitted` credit 을 하나 준다.
2. `prev_committed_seal` 의 sealer 비트마다 그 sealer 에게 `submitted` credit 을 하나 준다.
3. `Coinbase(b)` 에게 `len(prepare signers) + len(commit signers)` 개의 `proposed` credit 을 준다.
Source: consensus/wbft/engine/engine.go:664-745
Observable: header

[WBFT-EPOCH-009] block `L+1` 의 previous-block seal 에 있는 sealer index 는 반드시 block `L` 에 적용되는 `EpochInfo` 로 해석해야 하고, block `L+2 .. e` 의 sealer index 는 `latest` 로 해석해야 한다. `L` 에 기록된 set 이 `L` 을 seal 한 set 과 다르면 두 `EpochInfo` 값이 다르다. 그 경우 block `L` 의 seal 은 이전 set 의 member 에게 credit 되며, 이제 validator 가 아닌 member 도 포함된다.
Source: consensus/wbft/engine/engine.go:696-709

[WBFT-EPOCH-010] block 1 이 방문 범위에 있으면 block 1 작성자의 `being` 값에서 반드시 1 을 빼야 하며, block 1 에 대해서는 seal 을 세지 않는다.
Source: consensus/wbft/engine/engine.go:712-716

[WBFT-EPOCH-011] `is_val` 은 반드시 `latest.validators` 가 가리키는 주소에 대해서만 참이어야 한다. `was_val` 은 반드시 `latest` 의 candidate 가운데 block `L - 1` 에 적용되는 `EpochInfo` 의 validator 가 가리키는 candidate 에 대해서만 참이어야 하며, `L = 0` 이면 모든 candidate 에 대해 거짓이어야 한다.
Source: consensus/wbft/engine/engine.go:747-772

[WBFT-EPOCH-013] `candidates(e_header, post_state(e))` 가 돌려준 각 주소의 diligence 는 반드시 정수 연산 순서까지 포함해 4단계와 정확히 같이 계산해야 한다. `latest` 의 candidate 가 아닌 주소는 반드시 `DEFAULT_DILIGENCE`(1,900,000)를 받아야 한다. `latest` 의 candidate 가운데 `candidates` 가 더 이상 돌려주지 않는 candidate 는 새 candidate 목록에서 빠진다.
Source: consensus/wbft/engine/engine.go:806-872; core/types/istanbul.go:43,46
Observable: header, state

평활화 가중치 `rate` 는 현재 epoch 와 이전 epoch 에서 모두 validator 였던 주소에게는 `E` 이고, 현재 epoch 에 합류한 validator 에게는 `E - 1` 이며, 현재 set 에 없는 candidate 에게는 `1` 이다.

| 경우 | seal 항 `d_s` | 새 항의 가중치 | 식 |
|---|---|---|---|
| `is_val` 과 `was_val` 이 모두 참이다 | `s*D // (2E)` | `E / 10E` | `(D_prev*(10E - E) + d*E) // 10 // E` |
| `is_val` 은 참이고 `was_val` 은 거짓이다 | `s*D // (2(E-1))`, `s > 2(E-1)` 이면 오류 | `(E-1) / 10E` | `(D_prev*(9E + 1) + d*(E-1)) // 10 // E` |
| `is_val` 이 거짓이다 | `s*D // 2` | `1 / 10E` | `(D_prev*(10E - 1) + d) // 10 // E` |

모든 경우에 `d = d_s + d_p` 이다. 주소가 proposer 기회를 받았으면(`w > 0`) `d_p = proposed*D // max_p` 이고, 기회가 없었으면 `d_p = D` 이다.

> 해설: 세 가지 집계의 뜻은 다음과 같다. `submitted[a]` 는 주소 `a` 가 previous-block seal bitmap 에 켜진 횟수이며, prepared 와 committed 를 따로 세므로 block 하나당 최대 2 이다. `proposed[a]` 는 `a` 가 `Coinbase` 인 block 에 실린 seal 비트 수의 합이며, `a` 가 자기 block 에 다른 validator 의 seal 을 얼마나 모아 넣었는지를 나타낸다. `being[a]` 는 `a` 가 받은 proposer 기회의 수이다. round 1 에서 commit 된 block 은 round 0 의 proposer 에게도 기회 하나를 주므로, 기회를 받고도 block 을 만들지 못한 validator 가 드러난다. block 1 은 parent 가 genesis 라서 previous-block seal 이 없으므로, block 1 작성자의 `being` 에서 1 을 빼서 replay 에서 더해질 1 을 상쇄한다(WBFT-EPOCH-010).
>
> seal 항은 계속 validator 였던 주소가 모든 block 에서 두 seal 을 다 냈을 때 만점 `D` 가 된다. 제안 항은 자기가 만든 모든 block 에 모든 validator 의 seal 두 개를 다 모았을 때 만점 `D` 가 된다. 평활화는 이전 점수와 새 항을 가중 평균하며, 새 항의 가중치는 최대 1/10 이다. 평활화에서 `// 10` 다음에 `// E` 로 두 번 나누는 순서를 지켜야 한다. 한 번에 `// (10E)` 로 나누면 결과가 달라질 수 있다.
>
> 6.8절 epoch 1 의 v0 을 예로 들면 다음과 같다. `L = 0` 이므로 네 validator 는 모두 `was_val` 이 거짓이고, `rate = E - 1 = 3` 과 seal 분모 `2(E-1) = 6` 을 쓴다. block 2, 3, 4 가 싣고 있는 previous-block seal 에서 v0 의 비트는 6번 켜졌으므로 seal 항은 `6*D//6 = 1,000,000` 이다. v0 은 block 1 과 4 를 만들었다. block 1 에는 seal 이 없고, block 4 는 block 3 의 seal 을 싣는데 prepared 와 committed 모두 v0, v1, v2 로 6비트이다. 그래서 `proposed = 6` 이다. replay 는 block 1 과 4 에서 v0 에게 기회를 하나씩 주지만, block 1 보정으로 1 을 빼므로 `being = 1` 이고 `max_p = 2 * 4 * 1 = 8` 이다. 제안 항은 `6*D//8 = 750,000` 이다. 새 항은 `d = 1,750,000` 이고, 평활화 결과는 `(1,900,000*37 + 1,750,000*3)//10//4 = 1,888,750` 이다.

### 6.4 요구사항: 선택, 순서, shuffle

```python
def sort_candidates(cands: [Candidate]) -> [int]:
    # Indices 0..n-1 ordered by (power desc, diligence desc); power is 1 for every candidate.
    indices = list(range(len(cands)))
    sort_by_diligence_desc(indices, less=lambda i, j: cands[indices[i]].diligence > cands[indices[j]].diligence)
    return indices

def compute_shuffled_index(index: uint64, count: uint64, seed: bytes32) -> uint64:
    if index >= count:
        raise Error("input index %d out of bounds: %d" % (index, count))
    for r in range(33):                                        # SHUFFLE_ROUND_COUNT
        pivot_input = seed + uint8(r)                          # 33 bytes
        pivot = uint64_le(keccak256(pivot_input)[0:8]) % count
        flip = (pivot + count - index) % count
        position = max(index, flip)
        source_input = seed + uint8(r) + uint32_le(position >> 8)   # 37 bytes
        source = keccak256(source_input)
        byte = source[(position & 0xff) >> 3]
        bit = (byte >> (position & 0x07)) & 1
        if bit == 1:
            index = flip
    return index
```

`uint64_le(x)` 는 8바이트를 little-endian 으로 읽는다. `uint32_le(v)` 는 64비트 값 `v` 를 little-endian 으로 encode 한 결과의 하위 4바이트이다. 이 알고리즘은 Ethereum consensus layer 의 `compute_shuffled_index` 에서 SHA-256 을 Keccak-256 으로 바꾸고 round 수를 33 으로 둔 것이다.

[WBFT-EPOCH-016] `sort_candidates` 는 반드시 candidate index 를 power 의 내림차순으로, 그다음 diligence 의 내림차순으로 정렬해야 한다. reference 에서는 모든 candidate 의 power 가 1 이므로, 순서는 diligence 만으로 정해진다.
Source: consensus/wbft/engine/engine.go:875-884,1297-1320

[WBFT-EPOCH-018] `compute_shuffled_index` 는 반드시 위와 정확히 같이 계산해야 하며, 이때 `seed` 는 `e_header.mix_digest` 이다.
Source: consensus/wbft/engine/engine.go:1449-1521,1270
Observable: header

[WBFT-EPOCH-019] validator 목록은 반드시 `i = 0 .. n-1` 에 대해 `order[compute_shuffled_index(i, n, seed)]` 를 이 순서대로 나열한 것이어야 하며, `n` 개의 candidate 전체에 대해 계산한다. 그런 다음 `post_state(e)` 에서 BLS key 가 빈 candidate 의 항목을 모두 제거한다. 제거된 candidate 는 `candidates` 안의 자리와 diligence 를 그대로 유지한다. `bls_public_keys[k]` 는 반드시 `validators[k]` 에 대해 읽은 key 여야 한다. 그 밖의 필터는 없다. key 가 비어 있지 않은 candidate 는 모두 validator 가 되며, 여기서는 key 의 길이와 유효성을 검사하지 않는다.
Source: consensus/wbft/engine/engine.go:885-904,1265-1278
Observable: header, state

### 6.5 요구사항: 기록과 검증

[WBFT-EPOCH-020] 모든 epoch block `e >= 1` 의 header 는 반드시 `EpochInfo = compute_next_epoch_info(chain, e_header, post_state(e))` 를 실어야 한다. proposer 는 block 을 조립할 때 transaction 을 실행한 뒤, seal 하기 전에 이 값을 계산한다. 그래서 `EpochInfo` 는 `block_hash` 와 seal 에 포함된다.
Source: consensus/wbft/engine/engine.go:1053-1061 (FinalizeAndAssemble), 1193-1207 (writeEpoch)
Observable: header

[WBFT-EPOCH-021] 노드는 반드시 header 의 사본으로 `compute_next_epoch_info` 를 다시 계산하고 그 결과를 header 의 `EpochInfo` 와 비교해 epoch block 을 검증해야 한다. block 은 다음 경우에 유효하지 않다.
1. `EpochInfo` 가 없다.
2. candidate 수가 다르다.
3. 같은 위치의 candidate 가 주소나 diligence 에서 다르다.
4. validator 수가 다르거나, 어떤 validator index 가 다르다.
5. BLS key 수가 다르거나, 어떤 key 가 바이트 단위로 다르다.

이 검사는 세 목록이 같기를 요구하는 것과 같다. 다시 계산하는 도중 오류가 나도 block 은 유효하지 않다.
Source: consensus/wbft/engine/engine.go:919-921 (Finalize), 1212-1263 (verifyEpoch)
Observable: header, state

구현 노트 (informative). epoch block 을 조립하는 노드는 자기 주소가 `validators_at(e)` 에 있을 때에만 `EpochInfo` 를 계산한다. 그렇지 않으면 그 필드를 비워 두고, 그 block 은 다른 검사에서 실패한다. `validators_at(e)` 의 member 만 block `e` 를 만들 수 있으므로, 이 동작은 어차피 유효할 수 없는 block 에만 영향을 준다. Source: consensus/wbft/engine/engine.go:1193-1198.

> 해설: light verifier 는 "quorum 이 이 `EpochInfo` 에 동의했다" 는 사실을 믿는 것이며, 그 계산이 맞았는지는 확인하지 않는다.

### 6.6 오류

6.2절의 계산은 다음 경우에 실패한다. 계산이 실패하면 epoch block 을 만들 수 없거나, 그 block 이 거부된다. reference 는 이 경우들을 더 구분하지 않는다. 오류 문구 칸의 값은 reference 의 오류 메시지이다.

| 조건 | 오류 문구 | Source |
|---|---|---|
| `IsEpochBlockNumber` 가 실패한다 (도달할 수 없다) | 그대로 전달된다 | engine.go:652-654 |
| `e`, `L`, `L - 1` 에 적용되는 epoch header 를 찾지 못한다 | `unknown ancestor` (`consensus.ErrUnknownAncestor`) | engine.go:1427-1429 |
| 적용되는 epoch header 에 `EpochInfo` 가 없거나, 그 extra 를 decode 할 수 없다 | `WBFT: epochInfo is nil` / decode 오류 | engine.go:671-675,698-702,761-765,1438-1444 |
| 방문한 header 나 `L - 1` 의 header 가 저장되어 있지 않다 (header 검증을 통과했다면 도달할 수 없다) | process 가 중단된다 (nil 역참조) | engine.go:695-697,760-761 |
| 방문한 header 의 extra 를 decode 할 수 없다 | decode 오류 | engine.go:682-686 |
| 방문한 block `>= 2` 에 previous-block seal 이 없다 | `empty seals` (`ErrEmptySeals`) | engine.go:717-721,729-733,1323-1325 |
| sealer index 가 zero address 로 해석된다 | `validator address is zero` | engine.go:1329-1332 |
| replay round `N` 번 안에 작성자를 찾지 못한다 | `failed to find valid proposer` | engine.go:782-786 |
| 새 validator 의 `s` 가 `2(E-1)` 보다 크다 | `seal count exceed the range for non validator in prior epoch` | engine.go:838-840 |
| `E = 1` 인데 `s = 0` 인 새 validator 가 있다 (`s > 0` 이면 바로 위 행이 적용된다) | process 가 중단된다 (0 으로 나눈다) | engine.go:841 |
| diligence 가 `2D` 보다 크다 | `WBFT: Invalid Diligence %d exceeds maximum` | engine.go:864-866 |

구현 노트 (informative). sealer index 가 `len(info.validators)` 이상이면 `getSignerAddress` 가 index-out-of-range panic 으로 중단된다. header 검증이 그런 bitmap 을 먼저 거부하므로("sealer is not validator", `engine.go:1347-1350`), 검증을 통과한 header 에서는 이 경로에 도달할 수 없다.

### 6.8 계산 예

이 예제는 overlay test 로 reference `buildEpochInfo` 를 실행해 얻었다. overlay test 는 `engine_test.go:TestEpochInfo` 의 harness 를 복사한 것으로, in-memory header chain 을 쓰고 GovValidator storage 를 `govValidator.params` 로 초기화한다. coinbase 는 reference `CalcProposer` 로 계산했다. 예제가 다른 입력 없이 완결되도록, block `n` 의 `MixDigest` 는 실제 randao mix 대신 `keccak256(big_endian_minimal(n))` 으로 두었다.

계정은 다음과 같다. 각 key 는 `keccak256("wbft-spec-a04-key-i")` 로 만들었다. `v0 = 0x1FeEC135252C6152a4404606AAC45c4fd46E6321`, `v1 = 0x94c3726f51A447C80B5FccD3d00FA68eA4e0f025`, `v2 = 0x5f4ac72ecc55f372f81A918f975A0Ea6f960e658`, `v3 = 0xBB9F9a72edE4C0030c47D276EfAF66D07Fa91b8D`, `v4 = 0x36582928cFab0a09ccA37e823781479535890426`.

설정은 다음과 같다. 기본 epoch 길이는 4 이고(epoch block 0, 4, 8, 12), policy 는 RoundRobin 이다. genesis `EpochInfo` 는 candidate `v0..v3`(diligence 1,900,000)와 validator `[0,1,2,3]` 을 가지며, genesis `Coinbase` 는 zero 이다. block 4 와 그 이후에 읽는 candidate 목록은 `v0..v4` 이다(v4 는 epoch 1 동안 등록했다). block 12 시점에는 v2 의 BLS key 가 지워져 있다.

#### Epoch 1, block 1..4 (set `[v0, v1, v2, v3]`)

| Block | Commit round | 작성자 | prev prepared sealer | prev committed sealer |
|---|---|---|---|---|
| 1 | 0 | v0 | (없음, parent 가 genesis 이다) | (없음) |
| 2 | 1 | v2 | v0 v1 v2 v3 | v0 v1 v2 |
| 3 | 0 | v3 | v0 v1 v2 v3 | v0 v1 v2 v3 |
| 4 | 0 | v0 | v0 v1 v2 | v0 v1 v2 |

`e = 4` 에서의 계산은 `L = 0`, `E = 4` 로 진행한다.

1. seal 집계(block 4, 3, 2)의 결과는 `submitted = {v0: 6, v1: 6, v2: 6, v3: 3}`, `proposed = {v0: 6, v3: 8, v2: 7}` 이다. block 1 에서는 `being[v0] = -1` 이 되고 `first_proposer = v0` 이다.
2. `L = 0` 이므로 모든 주소의 `was_val` 은 거짓이다. `v0..v3` 의 `is_val` 은 참이다.
3. replay 는 `ZERO_ADDRESS` 에서 시작한다. block 1 의 round 0 은 v0 을 준다(`being[v0] = 0`). block 2 의 round 0 은 v1 을, round 1 은 v2 를 준다. block 3 의 round 0 은 v3 을 준다. block 4 의 round 0 은 v0 을 준다. 최종 결과는 `being = {v0: 1, v1: 1, v2: 1, v3: 1}` 이다.
4. diligence 는 모두 `rate = E - 1 = 3`, `2(E-1) = 6`, `max_p = 2 * 4 * 1 = 8` 로 계산한다.
   - v0 은 `6*D//6 = 1,000,000` 에 `6*D//8 = 750,000` 을 더하고, 평활화하면 `(1,900,000*37 + 1,750,000*3)//10//4 = 1,888,750` 이다.
   - v1 은 `1,000,000 + 0` 이고, 평활화하면 `1,832,500` 이다.
   - v2 는 `1,000,000 + 875,000` 이고, 평활화하면 `1,898,125` 이다.
   - v3 은 `3*D//6 = 500,000` 에 `8*D//8 = 1,000,000` 을 더하고, 평활화하면 `1,870,000` 이다.
   - v4 는 `latest` 의 candidate 가 아니므로 `1,900,000` 이다.
5. `order = [4, 2, 0, 3, 1]` 이다. block 4 의 seed(`0xf343681465b9efe82c933c3e8748c70cb8aa06539c361de20f72eac04e766393`)로 shuffle 하면 `i = 0..4` 가 위치 `[3, 4, 0, 2, 1]` 로 가므로 `validators = [3, 1, 4, 0, 2]`(`v3, v1, v4, v0, v2`)이다.

실행 출력은 candidate `v0 1888750, v1 1832500, v2 1898125, v3 1870000, v4 1900000` 과 validator `[3 1 4 0 2]` 이다.

> 해설: v1 은 만든 block 이 없어서 제안 항이 0 이다. 그런데 replay 에서 block 2 의 round 0 proposer 가 v1 이었으므로 v1 은 기회를 한 번 받았다. 그래서 v1 은 기회를 받고도 block 을 만들지 못한 validator 가 되어 가장 낮은 점수 1,832,500 을 받는다.

#### Epoch 2, block 5..8 (set `[v3, v1, v4, v0, v2]`)

| Block | 작성자 (index) | prev sealer (두 seal 모두) | 해석에 쓴 set |
|---|---|---|---|
| 5 | v2 (4) | index 0..3 | genesis `EpochInfo` (block 4 를 seal 했다): v0 v1 v2 v3 |
| 6 | v3 (0) | index 0..3 | `latest`: v3 v1 v4 v0 |
| 7 | v1 (1) | index 0..3 | v3 v1 v4 v0 |
| 8 | v4 (2) | index 0..3 | v3 v1 v4 v0 |

`e = 8` 에서의 계산 값은 `L = 4`, `E = 4`, `first_proposer = v2`, `validators_diff = 5 - 4 = 1` 이다. prior set(block 3 에 적용되는 set)은 `v0..v3` 이므로, `was_val` 이 거짓인 validator 는 v4 뿐이다. 집계 결과는 `submitted = {v0: 8, v1: 8, v3: 8, v4: 6, v2: 2}`, `proposed = {v2: 8, v3: 8, v1: 8, v4: 8}`, `being = {v2: 1, v3: 1, v1: 1, v4: 1}` 이고, `max_p = 10` 이다. 다만 첫 proposer 인 v2 의 `max_p` 는 `10 - 2*1 = 8` 이다.

- v0 은 proposer 기회가 없었으므로 `1,000,000 + 1,000,000` 이다. 평활화하면 `(1,888,750*36 + 2,000,000*4)//10//4 = 1,899,875` 이다.
- v1 은 `1,000,000 + 800,000` 이고, 결과는 `1,829,250` 이다.
- v2 는 `2*D//8 = 250,000` 에 `8*D//8 = 1,000,000` 을 더한다. `(1,898,125*36 + 1,250,000*4) = 73,332,500` 이고, `//10 = 7,333,250`, `//4 = 1,833,312` 이다.
- v3 은 `1,000,000 + 800,000` 이고, 결과는 `1,863,000` 이다.
- v4 는 `rate = 3` 이다. `6*D//6 = 1,000,000` 에 `800,000` 을 더하고, `(1,900,000*37 + 1,800,000*3)//10//4 = 1,892,500` 이다.

실행 출력은 candidate `v0 1899875, v1 1829250, v2 1833312, v3 1863000, v4 1892500` 과 validator `[2 1 0 4 3]` 이다.

#### Epoch 3, block 9..12 (set `[v2, v1, v0, v4, v3]`)

block 9 의 작성자는 v3 이고, block 9 의 prev sealer 는 epoch 2 set 의 index 0..3 이다. block 10 의 작성자는 v2 이고, 다섯 validator 가 모두 seal 했다. block 11 은 round 2 에서 commit 되었고, 작성자는 v4 이며 다섯 validator 가 모두 seal 했다. block 12 의 작성자는 v3 이다. block 12 의 prepared sealer 는 다섯 validator 모두이고, committed sealer 는 index 0..3 이어서 v3 이 빠졌다. 모든 validator 의 `was_val` 은 참이고, `first_proposer = v3`, `validators_diff = 0` 이다. replay 결과는 `being = {v3: 2, v2: 1, v1: 1, v0: 1, v4: 1}` 이다. block 11 의 replay 는 round 0 과 round 1 의 proposer 인 v1 과 v0 에게도 credit 을 준다.

- v3 은 `s = 7` 이므로 `7*D//8 = 875,000` 이다. `proposed = 17`, `max_p = 2*5*2 = 20` 이므로 제안 항은 `850,000` 이다. 평활화하면 `(1,863,000*36 + 1,725,000*4)//10//4 = 1,849,200` 이다.
- v4 는 `1,000,000 + 10*D//10` 이고, 결과는 `1,903,250` 이다.

실행 출력은 candidate `v0 1809887, v1 1746325, v2 1824980, v3 1849200, v4 1903250` 과 validator `[0 3 4 1]` 이다. v2 는 shuffle 로 목록에 들어갔다가 BLS key 가 비어 있어서 제거되었다. v2 는 자기 diligence 를 가진 candidate 2 로 남는다.

#### Shuffle vector

값은 reference `computeShuffledIndex` 에서 얻었다. `seed = keccak256("wbft-spec-a04") = 0xf28e414632a7647b703bc5559648840eb265067bf241c4c7471284bd4b114c28` 이다.

| count | `i = 0 .. count-1` 에 대한 `compute_shuffled_index(i)` |
|---|---|
| 1 | 0 |
| 2 | 1 0 |
| 3 | 1 0 2 |
| 4 | 1 2 0 3 |
| 5 | 0 3 2 4 1 |
| 8 | 4 2 5 1 0 7 6 3 |

seed 가 모두 0 이고 count 가 4 이면 결과는 `2 3 0 1` 이다. `compute_shuffled_index(4, 4, seed)` 는 `input index 4 out of bounds: 4` 로 실패한다.

`index = 0`, `count = 4` 와 위 seed 로 처음 세 round 를 추적한 결과는 다음과 같다.

| r | `keccak256(seed ‖ r)[0:8]` | pivot | flip | position | `source[0..3]` | byte | bit | 처리 후 index |
|---|---|---|---|---|---|---|---|---|
| 0 | `c5a417700cca37b6` | 1 | 1 | 1 | `8e480dcb` | `0x8e` | 1 | 1 |
| 1 | `e45652eb292b3c80` | 0 | 3 | 3 | `b7af3932` | `0xb7` | 0 | 1 |
| 2 | `a4eef6a40729094b` | 0 | 3 | 3 | `f4541406` | `0xf4` | 0 | 1 |

---

## 7. Genesis epoch 정보

```python
def initial_epoch_info(anzeon) -> EpochInfo:
    info = EpochInfo(candidates=[], validators=[], bls_public_keys=[])
    for i, addr in enumerate(anzeon.init.validators):
        info.candidates.append(Candidate(addr=addr, diligence=DEFAULT_DILIGENCE))
        info.validators.append(uint32(i))
        info.bls_public_keys.append(hex_decode(anzeon.init.bls_public_keys[i]))
    return info
```

[WBFT-EPOCH-023] genesis header 의 `EpochInfo` 는 반드시 `initial_epoch_info(anzeon)` 이어야 한다. 즉 `anzeon.init.validators` 의 항목마다 순서대로 candidate 하나를 두고 각각 diligence 1,900,000 을 주며, validator 는 `[0, 1, ..., n-1]` 이고, BLS key 는 hex decode 한 값을 순서대로 둔다. genesis extra 는 chain 설정에서 만들며, genesis 파일에 있는 `extraData` 는 무엇이든 덮어쓴다. `GasTip` 을 포함한 나머지 필드는 `B-02` 가 정한다.
Source: consensus/wbft/config.go:227-281 (CreateInitialExtraData, CreateInitialEpochInfo); core/genesis.go:242-262 (initializeAnzeonGenesis)
Observable: header

[WBFT-EPOCH-024] chain 설정은 반드시 `len(anzeon.init.validators) = len(anzeon.init.blsPublicKeys) > 0` 을 만족해야 한다. 이 조건을 어기는 genesis 는 거부된다.
Source: params/config_wbft.go:76-94 (CheckValidity); core/genesis.go:231-239

구현 노트 (informative). `compute_next_epoch_info` 는 block 0 에 대해 `initial_epoch_info` 를 돌려준다("if a transition occurs"). genesis block 은 `process_finalize` 로 실행되지 않으므로, 정상 운영에서는 이 분기에 도달하지 않는다. Source: consensus/wbft/engine/engine.go:659-662.
