# B-02 Anzeon genesis

- Status: draft
- Area code: `GEN`
- Reference implementation: go-stablenet `740526d03`

네트워크의 모든 노드는 바이트까지 같은 genesis block 에서 시작해야 한다. genesis hash 가 block 1 의 parent hash 이고, genesis state 가 block 1 의 이전 state 이기 때문이다. Anzeon chain 에서 genesis block 은 단순히 "파일 그대로" 가 아니다. go-stablenet 은 chain 설정을 바탕으로 genesis block 의 두 부분을 다시 만든다. 첫째, `extraData` 를 첫 validator 집합과 초기 gas tip 을 담은 `WBFTExtra` 인코딩으로 바꾼다. 둘째, 다섯 system contract 의 bytecode 와 초기 storage 를 allocation 에 주입한다. 또한 base fee 도 덮어쓴다. 그래서 genesis 파일을 글자 그대로 읽은 노드는 다른 hash 를 계산한다. 이 장은 그 구성 과정을 단계별로 정하고, genesis extra 와 genesis storage 사이에 성립해야 하는 일관성, genesis hash, 그리고 계산 예로서 두 preset genesis block 을 정한다.

---

## 1. 입력과 전체 절차

입력은 두 가지이다. 하나는 `Genesis` 명세(`config`, `nonce`, `timestamp`, `extraData`, `gasLimit`, `difficulty`, `mixHash`, `coinbase`, `alloc`, 그리고 테스트 전용인 `number`, `gasUsed`, `parentHash`, `baseFeePerGas`, `excessBlobGas`, `blobGasUsed`)이고, 다른 하나는 `B-01` 의 chain 설정(`genesis.config`)이다.

```python
def build_anzeon_genesis(g: Genesis) -> Block:
    check_anzeon_config(g.config.Anzeon)          # B-01 SNET-CFG-012
    g.extraData = create_initial_extra(g.config.Anzeon)          # §3
    for addr, acct in g.alloc.items():
        validate_extra_bits(acct.extra)                          # §4.1
    inject_contracts(g)                                          # §4.2 - §4.4
    header = to_header(g)                                        # §2
    header.root = state_root(g.alloc)                            # §5
    return Block(header, txs=[], uncles=[], withdrawals=None)
```

[SNET-GEN-001] Anzeon chain 에서 genesis block 은 반드시 `build_anzeon_genesis` 로 이 순서대로 만들어야 한다. genesis 명세의 `extraData` 는 반드시 무시하고 다른 값으로 바꿔야 한다. allocation 은 반드시 입력 allocation 에 `inject_contracts` 를 적용한 결과여야 한다.
Source: core/genesis.go:242-263 (initializeAnzeonGenesis)
Source: core/genesis.go:297-307, 320-336, 341-351 (called on every SetupGenesisBlock path before ToBlock)
Observable: header, state

---

## 2. Header 필드

```python
def to_header(g) -> Header:
    h = Header(
        parent_hash = g.parentHash,            # 실제로는 0
        uncle_hash  = EMPTY_UNCLE_HASH,        # keccak256(rlp([]))
        coinbase    = g.coinbase,
        root        = state_root(g.alloc),     # §5
        tx_hash     = EMPTY_ROOT_HASH,
        receipt_hash= EMPTY_ROOT_HASH,
        bloom       = zero,
        difficulty  = g.difficulty,
        number      = g.number,                # commit 하려면 반드시 0 이어야 한다
        gas_limit   = g.gasLimit if g.gasLimit != 0 else GENESIS_GAS_LIMIT,   # 4_712_388
        gas_used    = g.gasUsed,
        time        = g.timestamp,
        extra       = g.extraData,             # 이미 바뀌어 있다, §3
        mix_digest  = g.mixHash,
        nonce       = g.nonce,                 # 8 바이트 big-endian
        base_fee    = MIN_BASE_FEE,            # 20_000_000_000_000, Anzeon 이 덮어쓴다
    )
    if g.difficulty is None and g.mixHash == ZERO_HASH:
        h.difficulty = 131072                  # go-ethereum GenesisDifficulty; JSON 에서는 도달할 수 없다
    # Shanghai / Cancun genesis 필드는 그 fork 가 genesis 에서 활성일 때만 설정된다.
    # StableNet 설정은 그 fork 를 켜지 않는다 (B-01 SNET-CFG-025).
    return h
```

[SNET-GEN-002] Anzeon chain 에서 genesis `BaseFee` 는 반드시 `MinBaseFee = 20_000_000_000_000` 이어야 한다. genesis 명세의 `baseFeePerGas` 값이나 block 0 에서 London 이 활성인지 여부는 이 값에 영향을 주지 않는다.
Source: core/genesis.go:535-546 (ToBlock: AnzeonEnabled case first)
Observable: header

[SNET-GEN-003] genesis `GasLimit` 는 반드시 명세의 `gasLimit` 이어야 하며, 그 값이 0 이면 `4_712_388` 이어야 한다. `Difficulty`, `MixDigest`, `Nonce`, `Time`, `Coinbase`, `ParentHash`, `GasUsed` 는 반드시 명세의 값을 제약 없이 그대로 복사해야 한다. 특히 노드는 genesis difficulty 를 `1` 로 강제하지 않는다.
Source: core/genesis.go:510-534 (ToBlock)
Source: core/genesis.go:617-632 (mainnet preset difficulty 1, testnet preset difficulty 0 and gasLimit 0)
Observable: header

[SNET-GEN-004] 노드는 `number != 0` 인 genesis 명세를 commit 해서는 안 된다. 참조 구현은 이때 "can't commit genesis block with number > 0" 오류를 낸다.
Source: core/genesis.go:575-579 (Commit)

구현 노트 (informative). genesis `MixDigest` 는 block 1 이 쓰는 parent randao mix 이다(`A-02` `randao_mix`). 두 preset 은 모두 zero hash 를 쓴다. genesis `Difficulty` 는 저장된 total difficulty(`rawdb.WriteTd`)에 들어가고, `B-09` 는 peer 선택에 이 값을 쓴다. 그래서 testnet 은 total difficulty 0 에서, mainnet 은 1 에서 시작한다.

> 해설: testnet genesis 의 difficulty 가 0 이라는 사실은 세 가지 결과를 낳는다. 첫째, genesis hash 를 계산하는 방식이 달라진다(§6). 둘째, testnet genesis 는 WBFT header 검증을 통과할 수 없는 header 이다. header 검증은 `Difficulty == 1` 을 요구하지만(`B-03` §7 H4), genesis 는 header 검증으로 판정하지 않는다. 참조 구현은 시작할 때 현재 header 를 `VerifyHeader` 에 넘기지만 결과를 무시한다(`B-03` §2).  셋째, total difficulty 의 출발점이 달라진다. 동기화의 peer 선택은 total difficulty 를 비교하므로(`B-09`), total difficulty 를 "height + 1" 로 가정하는 코드는 testnet 에서 1 만큼 어긋난다.

---

## 3. Genesis extra 데이터

```python
def create_initial_extra(a: AnzeonConfig) -> bytes:
    epoch = EpochInfo(candidates=[], validators=[], bls_public_keys=[])
    for i, addr in enumerate(a.Init.validators):
        epoch.candidates.append(Candidate(addr=addr, diligence=DEFAULT_DILIGENCE))   # 1_900_000
        epoch.validators.append(uint32(i))
        epoch.bls_public_keys.append(hex_decode(a.Init.blsPublicKeys[i]))           # 잘못된 hex 면 panic 한다
    s = a.SystemContracts.GovValidator.params.get("gasTip", "")
    gas_tip = parse_decimal(s) if s != "" and parse_decimal_ok(s) else INITIAL_GAS_TIP  # 27_600_000_000_000
    extra = WBFTExtra(vanity=b"", randao_reveal=b"", prev_round=0,
                      prev_prepared_seal=None, prev_committed_seal=None,
                      round=0, prepared_seal=None, committed_seal=None,
                      gas_tip=gas_tip, epoch_info=epoch)
    return rlp_encode(extra)            # A-03 의 WBFTExtra 인코딩
```

[SNET-GEN-005] genesis `Extra` 는 반드시 `rlp_encode(WBFTExtra)` 여야 한다. 이 `WBFTExtra` 는 vanity 와 randao reveal 이 비어 있고, `prev_round = 0`, `round = 0` 이며, aggregated seal 네 개가 모두 없다(각각 빈 list `0xc0` 로 인코딩한다). `gas_tip` 은 SNET-GEN-006 을 따르고, `epoch_info` 는 SNET-GEN-007 을 따른다.
Source: consensus/wbft/config.go:227-256 (CreateInitialExtraData)
Source: core/types/istanbul.go:106-119 (WBFTExtra.EncodeRLP field order)
Observable: header

[SNET-GEN-006] genesis `GasTip` 은 `anzeon.systemContracts.govValidator.params["gasTip"]` 키가 있고, 비어 있지 않고, 10진 정수로 파싱할 수 있으면 반드시 그 파싱 값이어야 한다. 그렇지 않으면 반드시 `InitialGasTip = 27_600_000_000_000` 이어야 한다. 파싱할 수 있는 음수 값(앞의 `+` 나 `-` 부호는 받아들인다)으로는 genesis block 이 만들어지지 않는다. genesis 구성이 실패한다(SNET-GEN-010).
Source: consensus/wbft/config.go:233-243
Observable: header

[SNET-GEN-007] genesis `EpochInfo` 는 반드시 candidate 를 `anzeon.init.validators` 순서대로 나열해야 하고, 각 candidate 의 diligence 는 `DefaultDiligence = 1_900_000` 이어야 한다. `validators` 는 `[0, 1, ..., n-1]` 이며, 노드는 이 목록을 shuffle 하지 않는다. `bls_public_keys` 는 `anzeon.init.blsPublicKeys` 를 hex 디코드한 값이며 순서는 같다. 노드는 `init.validators` 의 중복 주소를 제거하지 않는다.
Source: consensus/wbft/config.go:258-281 (CreateInitialEpochInfo)
Observable: header

genesis `EpochInfo` 는 block `1 .. E` 의 validator 집합이다. 여기서 `E` 는 첫 epoch block 이다(`A-04`). 이 block 들의 base fee 분배(`B-06` §3)는 genesis `EpochInfo` 의 diligence 를 쓰며, 그 diligence 는 모두 같다.

구현 노트 (informative). `CreateInitialExtraData` 는 `gasTip` 을 파싱할 수 없으면 `InitialGasTip` 으로 대신한다. 그러나 storage 초기화 함수(§4.3)는 같은 값을 거부하므로, 파싱할 수 없는 값은 genesis 구성을 중단시킨다. 따라서 실행이 `InitialGasTip` 대체 경로에 도달하는 것은 키가 없거나 값이 비어 있을 때뿐이다. 빈 문자열이면 storage 초기화 함수도 실패한다. `govValidator.params` 가 있고 버전이 `v1` 일 때 키가 없으면 두 쪽 모두 `InitialGasTip` 을 쓴다. `params` 가 없으면 extra 에는 `InitialGasTip` 이 들어가지만 slot `0x39` 는 0 으로 남는다(SNET-GEN-010).

---

## 4. Allocation: system contract 주입

### 4.1 입력 allocation 의 계정 extra 비트

[SNET-GEN-008] 입력 allocation 의 계정 가운데 하나라도 `extra` 에 `AccountExtraValidMask = (1 << 63) | (1 << 62)` (blacklisted, authorized) 밖의 비트가 켜져 있으면, genesis 구성은 반드시 실패해야 한다.
Source: core/genesis.go:253-260
Source: core/types/state_account_extra.go:31-46, 103-108 (ValidateExtra)

[SNET-GEN-020] genesis 파일에서 allocation 계정의 `extra` 는 반드시 부호 없는 64 비트 정수로 읽어야 한다. 이 정수는 `0x` 나 `0X` 로 시작하는 JSON 문자열(16진수), 10진 숫자로 된 JSON 문자열, JSON 숫자 가운데 하나로 쓴다. `extra` 가 없거나, `null` 이거나, 빈 문자열이면 0 이다. 음수, `2^64` 이상의 값, 그 밖의 형식은 반드시 genesis 파일을 무효로 만들어야 한다. 참조 구현이 genesis 파일을 쓸 때(예를 들어 덤프한 preset 파일)는 `extra` 를 `0x` hex 문자열로 쓰고, 값이 0 이면 빼 버린다.
Source: core/types/gen_account.go:17-39 (MarshalJSON), 41-74 (UnmarshalJSON: extra as math.HexOrDecimal64)
Source: common/math/integer.go:48-82 (HexOrDecimal64, ParseUint64)
Observable: header, state

구현 노트 (informative). 참조 코드로 실행해 확인했다. `"0x4000000000000000"`, `"4611686018427387904"`, JSON 숫자 `4611686018427387904` 는 모두 authorized 비트가 되었다. `"0X10"` 은 16 이 되었고, `null` 과 key 가 없는 경우는 0 이 되었다. `"-1"` 과 `"18446744073709551616"` 은 `invalid hex or decimal integer` 오류로 실패했다.

### 4.2 Genesis 항목의 컨트랙트 transition

genesis 컨트랙트는 `system_contracts_transition(sc, alloc)` 이 만든다. 참조 구현은 이 함수를 `GetSystemContractsTransition` 이라고 부른다. 이 함수는 `B-04` §3 에서 정의하며, 이 장에서는 이해를 돕기 위해 다시 적는다. 이 함수는 다섯 멤버를 정해진 순서로 훑는다. 있는 멤버마다 등록된 bytecode 를 내보내고, `version == "v1"` 이고 `params` 가 있으면 초기 storage 도 내보낸다.

```python
ORDER = ["govValidator", "nativeCoinAdapter", "govMinter", "govMasterMinter", "govCouncil"]

def system_contracts_transition(sc, alloc) -> StateTransition:
    st = StateTransition(codes=[], states=[])
    for name in ORDER:
        c = getattr(sc, name)
        if c is None:
            continue
        st.codes.append((c.address, bytecode(name, c.version)))      # 등록되지 않은 버전이면 오류
        if c.params is not None and c.version == "v1":
            st.states += initialize(name, c.address, c.params, alloc)  # B-04 layout; alloc 을 읽거나 고칠 수 있다
    return st
```

[SNET-GEN-009] genesis storage 는 반드시 컨트랙트별 초기화 함수가 만들어야 한다. storage layout 은 `B-04` §8 과 §10.1 에 있고, genesis 에 따라 달라지는 값은 아래 §4.3-§4.4 에 있다. 노드는 초기화 함수를 `govValidator`, `nativeCoinAdapter`, `govMinter`, `govMasterMinter`, `govCouncil` 순서로 부르고, `nativeCoinAdapter` 와 `govCouncil` 의 초기화 함수에는 입력 allocation 을 넘긴다. `params` 가 없거나 버전이 `v1` 이 아닌 컨트랙트는 코드만 받고 storage 는 받지 않는다.
Source: systemcontracts/systemcontracts.go:62-157 (GetSystemContractsTransition)
Source: core/genesis.go:742-746 (called with &genesis.Alloc)
Observable: state

순서가 중요한 이유는 두 가지이다. 첫째, `NativeCoinAdapter` 의 총 공급량은 그 값을 계산하는 시점의 allocation 잔액 합이다(SNET-GEN-011). 둘째, `GovCouncil` 은 그 뒤에 allocation 에 계정을 추가한다(SNET-GEN-012).

> 해설: `GovCouncil` 이 추가하는 계정은 잔액이 0 이므로 지금은 순서를 바꿔도 합계가 같다. 그러나 순서를 바꾸는 구현은 이 논증을 스스로 다시 검증해야 한다.

### 4.3 Allocation 에 따라 달라지는 값

[SNET-GEN-010] `govValidator.params` 가 있고 버전이 `v1` 이면, `GovValidator` storage slot `0x39` (`gasTip`)은 반드시 `params["gasTip"]` 을 10진 정수로 파싱하고 `2^256` 으로 나눈 나머지여야 하며, 키가 없으면 `InitialGasTip` 이어야 한다. 파싱에는 Go `big.Int.SetString` 을 쓰며, 이 함수는 맨 앞의 `+` 나 `-` 부호를 받아들인다. 키가 있는데 값을 파싱할 수 없으면 노드는 반드시 genesis 구성을 중단해야 한다. 값이 빈 문자열인 경우도 파싱할 수 없는 경우에 들어간다. 음수 값도 genesis 구성을 중단시킨다. genesis `Extra` 가 음수 `GasTip` 을 인코딩하지 못하기 때문이다(SNET-GEN-006). `2^256` 이상인 값은 genesis 구성을 중단시키지 않는다. 이때 slot 에는 `2^256` 으로 나눈 나머지가 들어가고, genesis `Extra` 에는 원래 값 전체가 들어간다(SNET-GEN-019). `params` 가 없거나 버전이 `v1` 이 아니면 노드는 slot `0x39` 를 쓰지 않으므로, 그 slot 은 0 으로 읽힌다. 나머지 `GovValidator` slot(member, validator, BLS key, PoP precompile 주소)은 `B-04` 를 따른다.
Source: systemcontracts/gov_validator.go:52-84 (initializeValidator: gasTip)
Source: systemcontracts/systemcontracts.go:87 (initializer only for present params and v1)
Source: consensus/wbft/config.go:233-256 (extra gas tip; RLP encoding fails for a negative value)
Observable: state

구현 노트 (informative). testnet 설정에서 참조 코드로 실행해 확인했다. `gasTip = "-5"` 이면 genesis 구성이 `rlp: cannot encode negative big.Int` 오류로 실패했다. `"+5"` 이면 extra 와 slot 이 모두 5 였다. `2^256 + 1` 이면 extra 에는 원래 값 전체가, slot 에는 1 이 들어갔다. `govValidator.params = nil` 이면 extra 에는 `InitialGasTip` 이, slot 에는 0 이 들어갔다.

[SNET-GEN-011] `NativeCoinAdapter` slot `0x0d` (`_totalSupply`)는 반드시 입력 allocation 의 `balance` 필드 합과 같아야 한다. 이 합은 `inject_contracts` 가 system contract 계정을 쓰기 전에 계산한다(§4.4).
Source: systemcontracts/coin_adapter.go:153-166
Observable: state

### 4.4 GovCouncil 초기화와 allocation 동기화

`GovCouncil` 은 blacklist 와 authorized 계정 목록을 두 곳에 둔다. 하나는 컨트랙트 안의 `AddressSet` storage 로, governance 가 읽는다. 다른 하나는 각 계정 `extra` 의 비트로, 실행이 읽는다(`B-07`). genesis 에서는 두 출처의 합집합으로 두 사본을 모두 만든다.

```python
def init_gov_council(addr, params, alloc) -> list[StateParam]:
    sp = init_gov_base(addr, params)                              # B-04
    sets = {}
    for key, bit in [("blacklist", BLACKLISTED), ("authorizedAccounts", AUTHORIZED)]:
        s = parse_addresses(params.get(key, ""))                  # 쉼표로 구분한다; zero address 면 오류
        if alloc is not None:
            for a, acct in alloc.items():
                if a != ZERO_ADDRESS and acct.extra & bit:
                    s.add(a)
        sets[key] = s
    if alloc is not None:
        for key, bit in [("blacklist", BLACKLISTED), ("authorizedAccounts", AUTHORIZED)]:
            for a in sets[key]:
                acct = alloc.get(a, Account())
                if acct.balance is None:
                    acct.balance = 0                              # fix #83
                acct.extra |= bit
                alloc[a] = acct
        for key, values_slot, positions_slot in [("blacklist", 0x32, 0x33),
                                                 ("authorizedAccounts", 0x34, 0x35)]:
            if sets[key]:
                sp += address_set_storage(addr, values_slot, positions_slot,
                                          sorted(sets[key], key=bytes))   # 주소 바이트 오름차순
    sp.append(StateParam(addr, 0x36, ACCOUNT_MANAGER_ADDRESS))    # 0x…B00003
    return sp
```

`address_set_storage` 는 `values_slot` 에 `len` 을 쓰고, `keccak256(values_slot) + i` 에 원소 `i` 를 쓰며, `keccak256(pad32(a) ‖ positions_slot)` 에 위치 `i + 1` 을 쓴다(`B-04`).

[SNET-GEN-012] genesis 에서 blacklist 집합은 반드시 `govCouncil.params.blacklist` 의 주소와, allocation 에서 `extra` 에 blacklisted 비트가 켜진 0 이 아닌 주소의 합집합이어야 한다. authorized 집합도 `authorizedAccounts` 와 authorized 비트로 같은 방식으로 만든다. genesis system contract 주소를 뺀 합집합의 모든 주소는 반드시 allocation `extra` 에서 그 비트가 켜진 상태로 끝나야 한다. 노드는 allocation 에 없던 주소를 반드시 잔액 0, nonce 0 이고 코드가 없는 계정으로 추가해야 한다. 합집합의 주소가 genesis system contract 주소이기도 하면, `inject_contracts` 가 그 뒤에 그 주소의 allocation 항목을 새 계정으로 바꾼다(SNET-GEN-014, `B-04` SNET-SYS-011). 그래서 그 주소는 집합에는 남지만 `extra` 는 0 이다. 파라미터에 zero address 가 있으면 노드는 반드시 genesis 구성을 중단해야 한다.
Source: systemcontracts/gov_council.go:68-146 (initializeGovCouncil)
Source: systemcontracts/gov_council.go:213-225 (parseParamAddresses)
Source: core/genesis.go:750-752 (system-contract entries replaced after initialisation)
Observable: state

[SNET-GEN-013] 비어 있지 않은 집합마다 `GovCouncil` `AddressSet` storage 는 반드시 주소를 20 바이트 값의 오름차순으로 나열해야 하며, slot `0x36` 에는 반드시 `AccountManagerAddress` (`0x0000000000000000000000000000000000B00003`)가 있어야 한다.
Source: systemcontracts/gov_council.go:148-211 (accountManager slot, initializeAddressSet)
Observable: state

구현 노트 (informative). Fix #83 은 commit `3eada119e` 이며, `v1.1.0` 과 기준 commit 에 모두 들어 있다. 이 수정은 params 에만 있는 계정의 잔액을 "없음" 에서 `0` 으로 바꿀 뿐이다. 이 수정은 state root 를 바꾸지 않는다. genesis state builder 가 nil 잔액을 건너뛰고, 0 잔액은 아무것도 더하지 않기 때문이다. 이 수정 덕분에 노드는 덤프한 genesis JSON 을 다시 읽을 수 있게 되었다. 다시 읽을 때 "missing required field 'balance'" 오류가 더 이상 나지 않는다. genesis hash 는 수정 전후에 같다.

### 4.5 컨트랙트 계정 쓰기와 block 0 overlay

```python
def inject_contracts(g):
    st = system_contracts_transition(g.config.Anzeon.SystemContracts, g.alloc)   # §4.2, g.alloc 을 고친다
    for (addr, code) in st.codes:
        g.alloc[addr] = Account(code=code, balance=0, nonce=0, extra=0, storage={})  # 기존 항목을 대체한다
    for (addr, key, value) in st.states:
        g.alloc[addr].storage[key] = value
    for u in collect_upgrades(g.config):                      # B-01 §7.1
        if u.block != 0:
            continue
        ov = system_contracts_transition(u.system_contracts, None)
        for (addr, code) in ov.codes:
            if addr in g.alloc:
                g.alloc[addr].code = code                     # balance, nonce, extra, storage 는 유지한다
            else:
                g.alloc[addr] = Account(code=code, balance=0, storage={})
        for (addr, key, value) in ov.states:
            g.alloc[addr].storage[key] = value
```

주입 방식의 규범은 `B-04` 가 정한다. SNET-SYS-011 은 기준 transition 과 새 계정을, SNET-SYS-012 는 block 0 upgrade overlay 를 정한다. SNET-GEN-014 와 SNET-GEN-015 는 그 두 요구사항을 genesis 절차 안에서 적용되는 위치에 다시 적은 것이다. 문구가 다르면 `B-04` 가 우선한다.

[SNET-GEN-014] genesis system contract 계정은 반드시 새 계정으로 써야 한다. 새 계정의 코드는 registry 에서 가져오고, 잔액은 0, nonce 는 0, extra 는 0 이며, storage 는 초기화 함수가 만든 것만 담는다. 노드는 같은 주소에 있던 입력 allocation 항목을 버린다.
Source: core/genesis.go:735-755 (InjectContracts)
Observable: state

[SNET-GEN-015] 수집된 upgrade 가운데 `block == 0` 인 모든 항목(예: `bohoBlock == 0` 일 때의 `Boho`)에 대해, genesis allocation 에서 반드시 컨트랙트 코드를 교체해야 하며 잔액, nonce, extra, storage 는 유지해야 한다. 노드는 `params` 가 있는 `v1` 항목에 대해서만 storage 를 추가한다. 두 preset 에는 그런 항목이 없다.
Source: core/genesis.go:757-797 (applyUpgradeOverlay)
Observable: state

구현 노트 (informative). `gstable init` 은 allocation 에 다섯 genesis system contract 주소 가운데 하나라도 들어 있는 genesis 파일을 거부한다(`log.Crit`). `core.SetupGenesisBlock` 자체는 거부하지 않으며, 코드 안의 preset 에는 그런 항목이 없다. 그 결과 주입된 컨트랙트를 담고 있는 덤프 파일 `core/genesis_mainnet.json` 과 `core/genesis_testnet.json` 은 `gstable init` 에 다시 넣을 수 없다. Source: cmd/gstable/chaincmd.go:224-231, 252-270.

---

## 5. Genesis state root

노드는 최종 allocation 의 모든 계정을 빈 state 에 계정별로 다음 순서로 쓴다. 잔액이 있으면 잔액을 더하고, 코드를 설정하고, nonce 를 설정하고, extra 를 설정하고, 모든 storage slot 을 설정한다. 그런 다음 노드는 빈 계정을 지우지 않고 그 state 를 commit 한다.

[SNET-GEN-016] genesis state root 는 반드시 최종 allocation 의 계정만 정확히 담은 state trie 의 root 여야 한다. 각 계정은 StableNet 계정 인코딩 `rlp([nonce, balance, storage_root, code_hash] + ([extra] if extra != 0 else []))` 으로 인코딩하고, 값이 0 인 storage slot 은 빼며, 빈 계정은 남긴다.
Source: core/genesis.go:118-148 (hashAlloc; Commit(0, false))
Source: core/types/state_account.go:31-38; core/types/gen_account_rlp.go:8-25 (optional Extra)
Observable: header, state

뒤에 붙는 `extra` 원소가 Ethereum 계정 인코딩과의 유일한 차이이다. 이 원소는 그 뒤의 모든 state root 에도 들어간다(`B-06` §5, `B-07`).

---

## 6. Genesis hash

WBFT header 의 block hash 는 보통 header 를 필터링한 사본으로 계산한다(`A-03` `block_hash`). 필터링은 seal 을 지우고 round 를 0 으로 두는 것이다. 필터를 쓸지는 block 번호가 아니라 `Difficulty == 1` 로 정한다. testnet genesis 는 difficulty 가 0 이므로 hash 는 평범한 header hash 이다. mainnet genesis 는 difficulty 가 1 이므로 필터링된 hash 를 쓴다. 그러나 genesis extra 에는 이미 seal 이 없고 round 가 0 이며, 그 extra 를 디코드했다가 다시 인코딩하면 같은 바이트가 나오므로, 결과는 역시 평범한 header hash 와 같다.

[SNET-GEN-017] genesis block hash 는 반드시 §2 의 genesis header 에 대한 `keccak256(rlp(header))` 여야 한다. `Difficulty == 1` 인 genesis 에서는 `A-03` 의 필터링된 hash 를 적용하는 구현도 반드시 같은 값을 얻어야 한다. genesis extra 가 SNET-GEN-005 의 정규 인코딩이면 이 조건은 항상 성립한다. `Difficulty` 가 1 이 아닌 genesis(예: `Difficulty = 0` 인 testnet genesis)에서는 필터를 고르지 않으므로 hash 는 평범한 `keccak256(rlp(header))` 이다. 구현은 반드시 참조 구현과 똑같이 `Difficulty == 1` 로 hash 규칙을 골라야 한다. block 번호나 chain 으로 고르면 안 된다. 그래야 testnet genesis hash 가 평범한 header hash 가 된다.
Source: core/types/block.go:121-131 (Header.Hash selects the filter by Difficulty == 1)
Source: core/types/istanbul.go:269-288 (WBFTFilteredHeaderWithRound)
Observable: header

[SNET-GEN-018] `--mainnet` 이나 `--testnet` 으로 시작한 노드는, 데이터베이스에 저장된 genesis hash 가 자신이 만든 preset genesis 의 hash 와 다르면 반드시 그 데이터베이스를 거부해야 한다(`GenesisMismatchError`). 또한 노드는 만든 hash 를 `B-01` §11.1 의 상수와 대조하는 것이 좋다(SHOULD).
Source: core/genesis.go:338-351 (hash comparison)
Source: params/config.go:31-32 (constants)
Observable: header

---

## 7. Genesis extra 와 genesis storage 의 일관성

genesis extra 와 genesis storage 는 설정의 서로 다른 섹션에서 나온다. genesis extra 는 `anzeon.init` 에서, genesis storage 는 `govValidator.params` 에서 나온다. 참조 구현은 두 섹션을 비교하지 않는다. 각 값이 무엇을 결정하는지가 정해져 있으므로, 불일치가 genesis block 을 무효로 만들지는 않는다. 대신 불일치는 나중 height 의 동작을 바꾼다.

| Genesis extra 값 | 대응하는 storage 값 | 읽는 곳 | 불일치의 효과 |
|---|---|---|---|
| `EpochInfo.candidates` / `validators` (`init.validators` 에서 옴) | `GovValidator` validator 집합 (`params.validators` 에서 옴) | Extra: block `1..E` 의 validator 집합(`A-04`). Storage: 첫 epoch block `E` 에서 읽는 candidate 목록(`B-08`) | governance 행위가 없어도 `E + 1` 에서 validator 집합이 storage 쪽 집합으로 바뀐다 |
| `EpochInfo.bls_public_keys` (`init.blsPublicKeys` 에서 옴) | `GovValidator.validatorToBlsKey` (`params.blsPublicKeys` 에서 옴) | Extra: block `1..E` 의 seal 검증. Storage: `E` 에서 쓰는 BLS key | block `1..E` 의 seal 은 extra 의 key 로 검증하고, 그 뒤의 seal 은 storage 의 key 로 검증한다 |
| `GasTip` | `GovValidator` slot `0x39` | Extra: genesis 값을 검증하는 곳은 없다. head 가 genesis block 인 동안 RPC 와 gas price 추정이 이 값을 읽는다(`internal/ethapi/api.go:1562`, `eth/gasprice/anzeon.go:128-129`). Storage: block 1 에 요구되는 `GasTip` (`B-06` §5) | block 1 은 storage 값을 담아야 하며, genesis 값은 참고용일 뿐이다 |

[SNET-GEN-019] 적합한 노드는 아래 세 쌍의 값이 서로 다르다는 이유로 genesis block 을 거부해서는 안 된다. 참조 구현이 그런 genesis 를 받아들이기 때문이다. 다만 genesis 설정은 다음 세 쌍을 같게 두는 것이 좋다(SHOULD).
1. `anzeon.init.validators` 와 `govValidator.params.validators`: 주소와 순서가 같고 중복이 없어야 한다.
2. `anzeon.init.blsPublicKeys` 와 `govValidator.params.blsPublicKeys`
3. genesis `GasTip` 과 `GovValidator` gas tip slot
Source: consensus/wbft/config.go:258-281 (extra from Init)
Source: systemcontracts/gov_validator.go:86-181 (storage from params)
Source: consensus/wbft/engine/engine.go:622-645, 1280-1295 (block 1 gas tip read from storage)

두 preset 은 모두 SNET-GEN-019 를 만족한다(§8).

> 해설: 첫 번째 쌍은 특히 조심해야 한다. `init.validators` 의 순서는 첫 epoch 의 validator 인덱스 `0..n-1` 을 정한다(SNET-GEN-007). `params.validators` 의 순서는 `GovValidator` 의 `EnumerableSet` 배열 순서가 되고, 그 순서가 첫 epoch block 의 candidate 순서가 된다(`B-04` SNET-SYS-064, `B-08` SNET-SRC-006). 주소 집합이 같아도 순서가 다르면 epoch 계산의 동점 처리와 인덱스가 달라질 수 있다. 또한 중복 주소는 extra 쪽에서는 제거되지 않지만 storage 쪽에서는 뒤에 나온 중복이 제거된다. 그래서 SNET-GEN-019 의 "같다" 는 순서가 같고 중복이 없다는 뜻까지 포함한다.

---

## 8. 계산 예: preset genesis block

아래 값은 기준 commit 에서 빈 in-memory 데이터베이스에 `core.SetupGenesisBlock` 으로 코드 안의 preset 을 만들어 얻었다. 두 hash 는 모두 `params/config.go:31-32` 의 상수와 같다.

### 8.1 Mainnet (chain id 8282)

| 필드 | 값 |
|---|---|
| `ParentHash` | `0x00…00` |
| `UncleHash` | `0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347` |
| `Coinbase` | `0x0000000000000000000000000000000000000000` |
| `Root` | `0x8ce793fd139ec3c4695d5fde9048342148a637582ced00fda13ac7f978f4cc05` |
| `TxHash`, `ReceiptHash` | `0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421` |
| `Difficulty` | `1` |
| `Number` | `0` |
| `GasLimit` | `105_000_000` (`0x6422c40`) |
| `GasUsed` | `0` |
| `Time` | `0` |
| `MixDigest` | `0x00…00` |
| `Nonce` | `0x0000000000000000` |
| `BaseFee` | `20_000_000_000_000` |
| Hash | `0xf192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b` (평범한 hash 와 필터링된 hash 가 같다) |

`Extra` (98 바이트):

```
f860                                   list, 96-byte payload
  80                                   vanity           = ""
  80                                   randao_reveal    = ""
  80                                   prev_round       = 0
  c0                                   prev_prepared_seal  (absent)
  c0                                   prev_committed_seal (absent)
  80                                   round            = 0
  c0                                   prepared_seal       (absent)
  c0                                   committed_seal      (absent)
  86 191a20322000                      gas_tip          = 27_600_000_000_000
  f84f                                 epoch_info, 79-byte payload
    da                                   candidates
      d9 94 aa5faa65e9cc0f74a85b6fdfb5f6991f5c094697 83 1cfde0     (addr, diligence 1_900_000)
    c1 80                                validators = [0]
    f1 b0 aec493af8fa358a1c6f05499f2dd712721ade88c477d21b799d38e9b84582b6fbe4f4adc21e1e454bc37522eb3478b9b
                                         bls_public_keys = [48-byte key]
```

Allocation: 입력 allocation 은 비어 있다. 최종 allocation 에는 다섯 system contract 만 있고 잔액은 모두 0 이다. `bohoBlock == 0` 이므로 `GovMinter` 는 `v2` bytecode 를 가진다(§4.5). 덤프 파일의 storage slot 수는 `0x1000`: 10, `0x1001`: 20, `0x1002`: 15, `0x1003`: 10, `0x1004`: 10 이다. `NativeCoinAdapter._totalSupply` (slot `0x0d`)는 0 이다.

### 8.2 Testnet (chain id 8283)

| 필드 | 값 |
|---|---|
| `Root` | `0x9080703111c1f13dbab77af07b2dc96966e2cec0331f96d5479a16bf3a1b635b` |
| `Difficulty` | `0` |
| `GasLimit` | `4_712_388` (preset 이 `gasLimit` 을 0 으로 둔다) |
| `BaseFee` | `20_000_000_000_000` |
| `Time`, `Nonce`, `MixDigest`, `Coinbase`, `ParentHash` | 모두 0 |
| Hash | `0x2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490` (difficulty 가 0 이므로 평범한 hash) |

참고 (informative). testnet preset 은 `Difficulty` 를 0 으로 두고(`core/genesis.go:629`), mainnet preset 은 1 로 둔다(`core/genesis.go:621`). 그래서 testnet genesis 는 그 chain 에서 WBFT filter 로 hash 를 고르지 않는 유일한 header 이다(SNET-GEN-017). 이 값은 운영 중인 testnet 의 genesis hash 에 들어 있으므로, 처음 의도가 무엇이었든 그 chain 의 사실이며 바꿀 수 없다.

`Extra` 는 §8.1 과 모양이 같다. 다만 list header 는 `0xf9022c` 이고, candidate 는 일곱 개 `(init.validators[i], 1_900_000)` 이며 `B-01` §11.2 순서를 따른다. `validators = [0, 1, 2, 3, 4, 5, 6]` (인코딩은 `c7 80 01 02 03 04 05 06`)이고, BLS key 는 일곱 개이며, `gas_tip = 27_600_000_000_000` 이다.

Allocation: 내장 testnet allocation 에서 온 잔액 있는 계정 여덟 개가 있다. `0x4053e68d…15de`, `0x56d97be0…4bd1`, `0x58f13fe4…4d43`, `0x79495f15…9002`, `0x9b690222…6746`, `0xe0e5bdd4…f453`, `0xf7aeccd8…c833` 는 각각 `10^27` 을 가지고, `0xd13657a2f08a266ca816c96c1a97e888990f485b` 는 `10^28` 을 가진다. 여기에 `v1` system contract 다섯 개가 더해진다. `NativeCoinAdapter._totalSupply = 7 × 10^27 + 10^28 = 17_000_000_000_000_000_000_000_000_000` 이며, 이 값은 입력 잔액의 합과 같다(SNET-GEN-011). `GovValidator` slot `0x39 = 27_600_000_000_000` 이며, extra 의 `GasTip` 과 같다. `GovMinter` 는 genesis 에서 `v1` 이고, block `14_408_500` 에서 `v2` 가 된다(`B-06` §2).

구현 노트 (informative). 덤프 파일 `core/genesis_mainnet.json` 과 `core/genesis_testnet.json` 에는 `baseFeePerGas = 0x3b9aca00` 이 적혀 있고, testnet 파일에는 `gasLimit = 0x0` 과 `difficulty = 0x0` 도 적혀 있다. 첫 번째 값은 header 에 실제로 들어가는 값이 아니다. 노드는 그 값을 `MinBaseFee` 로 바꾼다(SNET-GEN-002). 나머지 두 값은 SNET-GEN-003 에 적은 대로 적용된다. 독자는 JSON 의 `baseFeePerGas` 를 genesis base fee 로 받아들여서는 안 된다.
