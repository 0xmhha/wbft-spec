# B-08 Candidate 출처

- Part: B (StableNet block 유효성)
- Area code: `SRC`
- Status: draft
- Reference implementation: go-stablenet `740526d03`

Part A 는 실행 계층을 추상 application 으로 다룬다 (`A-09`). 그 연산 가운데 셋이 StableNet contract state 에 의존한다. 첫째는 다음 epoch 을 계산할 때 쓰는 BLS key 가 딸린 candidate 목록이고, 둘째는 proposer 자격 검사이고, 셋째는 모든 header 가 싣는 gas tip 이다. 이 장은 각 연산을 contract state 에 묶는다. 곧 어느 block 의 state 를 읽는지, 어느 contract 주소를 읽는지, 어느 slot 을 읽는지, 어떤 알고리즘을 쓰는지, 데이터가 없으면 무슨 일이 일어나는지를 정한다.

storage 배치와 읽기 함수(`validator_list`, `bls_public_key`, `gas_tip`, `system_contracts_at`, `Extra` 비트)는 `B-04` 가 정한다. 이 state 를 바꾸는 거버넌스 동작은 `B-05` 가 정한다.

---

## 1. 바인딩 요약 (informative)

| Part A 연산 | 호출되는 곳 | 읽는 state | Contract 주소 | 읽기 함수 (`B-04`) |
|---|---|---|---|---|
| genesis 의 candidate | block 0 | state 를 읽지 않고 `anzeon.init` 설정을 쓴다 | — | — |
| proposer 자격 (blacklist) | 모든 block `n > 0` 의 `header.coinbase` | parent 의 state `S(n−1)` | — (계정 필드) | `Extra` 비트 63 |
| gas tip | 모든 block `n > 0` | parent 의 state `S(n−1)` | genesis `GovValidator` | `gas_tip` |

`S(k)` 는 block `k` 가 commit 한 state, 곧 root 가 `header(k).root` 인 state 를 뜻한다.

이름에 관해 덧붙인다. 추상 연산의 이름은 `candidates(epoch_header, post_state)` 다 (`A-09` §4.2). StableNet 바인딩은 `candidates_at_epoch(state_e, e, upgrades)` 다 (2.4 절). 반면 `is_eligible_proposer(parent, addr)` 와 gas tip 은 parent 의 state 를 읽는다 (3 절과 4 절).

---

## 2. 다음 epoch 의 candidate 와 BLS key

### 2.1 Candidate 목록을 읽는 시점

노드는 epoch 마다 한 번, epoch block 을 finalize 하는 동안 candidate 목록을 읽는다. epoch block 은 `A-04` (`is_epoch_block`)가 정한다. proposer 는 block 을 조립할 때 `writeEpoch` 에서 이 계산을 하고, block 을 검증하는 모든 노드는 block 을 import 할 때 `verifyEpoch` 에서 같은 계산을 한다. 두 쪽 모두 각자 그 block 을 실행한 결과 위에서 계산한다. block 을 검증하는 노드는 계산 결과를 header 의 `EpochInfo` 와 비교한다.

[SNET-SRC-001] 모든 epoch block `e > 0` 에 대해 노드는 반드시 `e` 를 finalize 하는 동안 contract state 에서 다음 epoch 의 candidate 목록을 계산해야 한다. block 을 검증하는 노드(verifier)는 `e` 의 header 에 있는 `EpochInfo` 가 자신이 계산한 값과 같지 않으면 반드시 block `e` 를 거부해야 한다. 같다는 것은 candidate 를 원소별로 주소와 diligence 로 비교하고, validator 인덱스를 원소별로 비교하고, BLS key 를 원소별로 바이트로 비교해서 모두 같다는 뜻이다.
Source: consensus/wbft/engine/engine.go:947-958 (processFinalize, epoch step), consensus/wbft/engine/engine.go:1193-1208 (writeEpoch), consensus/wbft/engine/engine.go:1212-1263 (verifyEpoch)
Observable: header

[SNET-SRC-002] block 0 에서는 candidate 목록을 contract state 에서 읽어서는 안 된다. genesis `EpochInfo` 는 `anzeon.init.validators` 와 `anzeon.init.blsPublicKeys` 로 만든다 (`B-02`).
Source: consensus/wbft/engine/engine.go:659-662, consensus/wbft/config.go:258-281 (CreateInitialEpochInfo)
Observable: header

이 문단은 informative 구현 노트다. block 0 의 candidate 는 `anzeon.init` 에서 오고, 그 뒤 모든 epoch 의 candidate 는 `GovValidator` storage 에서 온다. 그래서 genesis 의 `anzeon.init.validators` 가 `GovValidator` 의 `validators` 인자와 다르면, 첫 epoch block 에서 validator 집합이 바뀐다. genesis 일관성 규칙은 `B-02` 가 정한다.

### 2.2 어느 state 인가

block `e` 의 finalization 은 다음 순서로 진행한다.

1. block `e` 의 system contract upgrade 를 적용한다 (`B-04` SNET-SYS-021).
2. base fee 를 분배한다 (`B-06`).
3. epoch 단계를 실행한다.
4. gas tip 을 검증한다.
5. state root 를 계산한다.

아래에서 (1) 부터 (5) 까지는 이 단계 번호를 가리킨다.

[SNET-SRC-004] (2) 부터 (5) 까지의 단계는 `GovValidator` storage 를 쓰지 않으므로, epoch 단계에서 읽은 값은 반드시 `S(e)` 의 같은 slot 값과 같아야 한다. 그래서 observer 는 `eth_getStorageAt(gv, slot, e)` 로 epoch block `e` 의 candidate 목록을 다시 계산해도 된다.
Source: consensus/wbft/engine/engine.go:972-1051 (distributeBaseFee writes balances only), consensus/wbft/engine/engine.go:1280-1295 (verifyGasTip reads only)
Observable: state, rpc

### 2.4 읽기 알고리즘

```python
def candidates_at_epoch(state_e, e: int, upgrades) -> list[tuple[Address, bytes]]:
    """state_e: 2.2 절이 정한 state. 순서 있는 (address, bls_key) 쌍을 돌려준다."""
    gv = system_contracts_at(e, upgrades).GovValidator.Address      # B-04 section 4.3
    addrs = validator_list(state_e, gv)                               # B-04 section 8.2, 배열 순서
    return [(a, bls_public_key(state_e, gv, a)) for a in addrs]      # 없으면 b""

def epoch_info_from_candidates(pairs, prev_epoch_data, header_e) -> EpochInfo:
    # Candidates: 모든 주소를 목록 순서대로 담고, diligence 는 A-04 에서 가져온다.
    # diligence(...) 는 A-04 compute_next_epoch_info 의 4 단계를 뜻한다 (별도 함수가 아니다).
    candidates = [Candidate(addr=a, diligence=diligence(a, prev_epoch_data)) for a, _ in pairs]
    # Validator 순서: A-04 compute_next_epoch_info 의 5 단계 (sort_candidates 뒤에
    # header_e.mix_digest 로 compute_shuffled_index). 참조 구현의 decideValidators 다.
    order = decide_validators(candidates, header_e.mix_digest)       # candidate 인덱스 목록
    validators, keys = [], []
    for idx in order:
        key = pairs[idx][1]
        if len(key) == 0:
            continue                                                  # 건너뛰지만 candidate 로는 남는다
        validators.append(idx)
        keys.append(key)
    return EpochInfo(candidates=candidates, validators=validators, bls_public_keys=keys)
```

[SNET-SRC-006] 새 `EpochInfo` 의 `Candidates` 는 반드시 `validator_list` 가 돌려준 주소와 정확히 같아야 하고, 순서도 같아야 하며, 주소 하나에 항목 하나여야 한다. 이 순서는 규범이다. validator 인덱스가 이 순서를 가리키고, `A-04` 정렬 단계의 동점 순서가 이 순서에 따라 정해지기 때문이다.
Source: consensus/wbft/engine/engine.go:806-866 (newEpoch.Candidates built in `newCandidates` order)
Source: consensus/wbft/engine/engine.go:1297-1320 (sortCandidates: unstable sort over the candidate indices)
Observable: header

[SNET-SRC-007] 모든 candidate 는 반드시 validator 선택에 올라가야 한다. StableNet 에서는 모든 candidate 의 power 가 1 로 같다. validator 선택과 순서는 `A-04` 가 정한다.
Source: consensus/wbft/engine/engine.go:876-889 (PoweredCandidate with Power 1, decideValidators over all candidates)
Observable: header

[SNET-SRC-008] 선택된 candidate 인덱스마다, 선택 순서대로, 노드는 반드시 candidate 목록에 쓰는 state 와 주소에서 `bls_public_key` 를 읽어야 한다. key 가 비어 있으면 노드는 반드시 그 인덱스를 `Validators` 에서 빼야 하고 key 를 덧붙이지 않는다. key 가 비어 있지 않으면 노드는 인덱스를 `Validators` 에 덧붙이고, key 바이트를 바꾸지 않은 채 `BLSPublicKeys` 에 덧붙인다. 이렇게 빠진 candidate 는 `Candidates` 에 남는다.
Source: consensus/wbft/engine/engine.go:890-905
Observable: header

[SNET-SRC-009] 노드는 `EpochInfo` 를 만들 때 storage 에서 읽은 비어 있지 않은 BLS key 의 길이나 유효성을 검사해서는 안 된다. 유효한 48 바이트 compressed G1 point 가 아닌 key 가 들어가면, 그 validator 의 seal 은 `A-02` 에서 verify 할 수 없다. 다만 그런 key 는 `configureValidator` 로 등록할 수 없다. `configureValidator` 가 48 바이트 길이와 proof of possession 을 요구하기 때문이다 (`B-05`).
Source: consensus/wbft/engine/engine.go:894-905, systemcontracts/gov_validator.go:208-210
Observable: header

### 2.5 경계 사례

| 상황 | 읽기 결과 | 결과 `EpochInfo` | 참조 commit 의 규범 동작 |
|---|---|---|---|
| 일부 candidate 에 BLS key 가 없다 | 주소는 나오고 key 는 `b""` 다 | 그 candidate 는 있지만 validator 가 아니다 | 받아들인다 |

---

## 3. Proposer 자격

참조 구현에서 block `n` 을 만들 자격은 두 부분으로 이루어진다. 첫째는 membership 이다. membership 은 coinbase 가 `n` 을 seal 하는 epoch 의 validator 여야 한다는 규칙이고, Part A 가 이 규칙을 정한다 (`A-08`). 둘째는 StableNet 이 더한 부분인 blacklist 다.

[SNET-SRC-020] `coinbase` 의 `Extra` 비트 63 이 `S(n−1)` 에서 켜져 있으면, 노드는 block `n > 0` 을 반드시 거부해야 한다.
Source: consensus/wbft/engine/engine.go:352-381 (verifySigner: validator-set membership, then `state.IsBlacklisted(signer)` on `StateAt(parent.Root)`)
Source: consensus/wbft/engine/engine.go:86-88 (Author = header.Coinbase)
Observable: header, state

[SNET-SRC-022] proposer 선택 (`A-04`, `calc_proposer`)은 blacklisted validator 를 건너뛰어서는 안 된다. blacklisted validator 가 proposer 로 뽑히면, 그 validator 는 자기 `Coinbase` 로 유효한 block 을 만들 수 없다. 그 validator 가 이전 round 에서 prepared 된 block 을 다시 제안하는 경우에는 원래의 `Coinbase` 가 유지되므로 문제가 없다. 그 경우가 아니면 그 round 는 timeout 과 round change 로 끝난다 (`A-05`, `A-06`).
Source: consensus/wbft/engine/engine.go:1265-1278 (decideValidators), consensus/wbft/engine/engine.go:806-905 (no blacklist input to validator selection)
Observable: header

---

## 4. Gas tip

모든 header `n > 0` 은 `WBFTExtra` 에 gas tip 을 싣는다 (`A-03`). 실행 쪽에서 gas tip 을 authorized 가 아닌 sender 의 priority fee 로 쓰는 방식은 `B-07` 에 있다. 이 절은 header 값이 어디서 오는지를 정한다.

```python
def expected_gas_tip(n: int, chain) -> int:
    parent = chain.header(n - 1)
    if parent is None:
        raise UnknownAncestor
    if parent.root == ZERO_HASH:
        raise Error("parent state root is empty")
    s = chain.state_at(parent.root)                       # StateUnavailable 이 날 수 있다
    gv = chain.config.anzeon.systemContracts.GovValidator.Address   # genesis 주소
    return gas_tip(s, gv)                                 # B-04 section 8.2; 없으면 0
```

[SNET-SRC-030] block `n > 0` 의 header 에 있는 gas tip 은 반드시 있어야 하고, 반드시 `gas_tip(S(n−1), gv0)` 과 같아야 한다. 여기서 `gv0` 은 `anzeon.systemContracts` 의 `GovValidator` 주소다.
Source: consensus/wbft/engine/engine.go:620-645 (getGasTip), consensus/wbft/engine/engine.go:1280-1295 (verifyGasTip: nil or different value is a mismatch)
Observable: header, state

[SNET-SRC-031] proposer 는 자신이 제안하는 header 에 반드시 `expected_gas_tip(n)` 을 써야 한다.
Source: consensus/wbft/engine/engine.go:511-513, :518, :537, :542 (Prepare → WriteGasTip)
Observable: header

[SNET-SRC-032] block 을 검증하는 노드는 block 실행(finalization) 중에는 조건 없이 반드시 SNET-SRC-030 을 검사해야 한다. header 만 검증할 때는 `S(n−1)` 이 있으면 반드시 검사해야 하고, 값 불일치가 아닌 다른 이유로 gas tip 을 정할 수 없으면 header 를 거부하지 말고 반드시 검사를 건너뛰어야 한다 (`A-08` WBFT-HDR-111, `B-06` SNET-FIN-019).
Source: consensus/wbft/engine/engine.go:962-965 (processFinalize → verifyGasTip, error returned)
Source: consensus/wbft/engine/engine.go:329-348 (header verification: mismatch is fatal, state errors are skipped)
Observable: header

---

## 5. State 가용성 (informative 요약)

| 검사 | 필요한 것 | header 검증 중에 state 가 없을 때 | 실행 중 |
|---|---|---|---|
| candidate 와 BLS key (epoch) | `e` 의 transaction 뒤 state | 검사하지 않는다 (header 검증은 `EpochInfo` 를 계산하지 않는다) | 항상 검사한다 (SNET-SRC-001) |
| Gas tip | `S(n−1)` | 건너뛴다 | 항상 검사한다 (SNET-SRC-032) |

---

## 6. 대안 출처: native validator 모듈

native Go validator 모듈은 `GovValidator` 와 `GovBase` 의 의미를 재현한다. 호환 모드는 노드가 go-stablenet 노드와 같은 네트워크에 참여하는 경우를 말한다. 호환 모드에서는 contract storage 가 여전히 진실의 원천이고, 모듈은 mirror 다.

### 6.3 교차 확인 vector

각 vector 는 시작 state, 동작 순서, 기대하는 관찰 결과를 준다. "목록" 은 지정한 state 의 `validator_list` 다. `V0..V3` 은 validator 이고, 각 validator 의 operator 는 `O0..O3`, key 는 `K0..K3` 이다. V-SRC-007, 008, 010 을 뺀 모든 vector 는 test network 에서 일반 transaction 으로 만들 수 있다. 그 세 vector 는 특별히 만든 genesis 나 upgrade 설정이 필요하다.

| ID | 준비와 동작 | 기대 결과 |
|---|---|---|
| V-SRC-001 | testnet preset genesis | 목록은 testnet validator 7 개를 인자 순서대로 담는다 (`B-04` 9.2 절). gas tip 은 27 600 000 000 000 이다 |
| V-SRC-002 | genesis `[V0..V3]` 에서 `O0` 가 새 주소 `N` 을 설정한다 | 목록은 `[V3, V1, V2, N]` 이다. `V0` 의 key 는 비어 있고, `N` 의 key 는 새 key 다 |
| V-SRC-003 | genesis `[V0..V3]` 에서 `O1` 이 `V1` 을 key `K1'` 로 설정한다 | 목록은 바뀌지 않는다. `V1` 의 key 는 `K1'` 이고, `blsKeyToValidator[K1]` 은 0 이다 |
| V-SRC-004 | genesis `[V0..V3]` 에서 제안으로 member `O3` 을 제거한다 | 목록은 `[V0, V1, V2]` 다 (마지막 원소를 지우므로 swap 이 없다) |
| V-SRC-005 | genesis `[V0..V3]` 에서 제안으로 member `O1` 을 제거한다 | 목록은 `[V0, V3, V2]` 다 |
| V-SRC-006 | genesis `[V0..V3]` 에서 member `O2` 가 `changeMember(O2')` 를 부른다 | 목록은 바뀌지 않는다. `operatorToValidator[O2'] = V2` 다 |
| V-SRC-007 | `validators` 항목 하나가 반복된 genesis | 목록에는 첫 번째 것만 있다. 각 항목의 operator 는 같은 인자 위치의 member 다 |
| V-SRC-008 | `V2` 가 집합에 있지만 `validatorToBlsKey[V2]` 가 비어 있는 state | 다음 `EpochInfo` 에서 `V2` 는 `Candidates` 에 있지만 `Validators` 에는 없다 |
| V-SRC-009 | epoch block `e` 에 `configureValidator` transaction 이 들어 있다 | 변경이 `EpochInfo(e)` 에 반영된다 (다음 epoch block 으로 미뤄지지 않는다) |
| V-SRC-011 | block `k` 에서 gas tip 제안이 실행된다 | header `k` 는 옛 tip 을, header `k+1` 은 새 tip 을 싣는다 |
