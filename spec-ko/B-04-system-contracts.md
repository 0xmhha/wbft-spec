# B-04 System contracts

- Part: B (StableNet block 유효성)
- Area code: `SYS`
- Status: draft
- Reference implementation: go-stablenet `740526d03`

이 장은 WBFT 노드가 보는 system contract 를 정의한다. 어떤 contract 가 어느 주소에 있고 fork 마다 어떤 bytecode 를 쓰는지, 그 code 와 storage 가 genesis 에서 어떻게 설치되고 upgrade block 에서 어떻게 교체되는지, 노드가 직접 읽는 slot 의 정확한 storage 배치가 무엇인지, blacklist 와 authorized 플래그를 담는 계정 `Extra` 비트 필드가 어떻게 생겼는지를 다룬다. 이 contract 들의 거버넌스 의미(member, proposal, vote, quorum, `configureValidator`, gas tip proposal, `GovCouncil`, minter)는 `B-05` 에서 다룬다. 이 값들 가운데 어느 것이 합의 프로토콜로 들어가고 어느 state 에서 읽히는지는 `B-08` 에서 다룬다.

---

## 1. 범위와 읽는 법 (informative)

노드는 합의 판단을 내리기 위해 system contract 를 EVM 으로 호출하지 않는다. 노드는 `GovValidator` 계정의 storage slot 을 state trie 에서 직접 읽으며, 이때 slot key 를 스스로 계산한다. 또한 노드는 계정 `Extra` 필드의 bit 63 을 읽는다. 그러므로 conforming 구현은 다음 세 가지를 비트 단위로 똑같이 재현해야 한다.

1. system contract 주소에 있는 code 와 storage 를 재현한다. 이 값들이 state root 를 정하기 때문이다.
2. 노드가 읽는 변수의 Solidity storage 배치를 재현한다. 노드가 slot key 를 직접 계산하기 때문이다.
3. slot 을 값으로 바꾸는 알고리즘을 재현한다. 결과 값은 순서가 있는 주소 목록, 바이트 문자열, 정수 가운데 하나다.

2절은 contract 목록을 제시한다. 3절과 4절은 설치와 upgrade 를 정의한다. 5절은 모든 reader 가 쓰는 일반적인 Solidity storage 인코딩을 정의한다. 6절부터 8절까지는 `EnumerableSet`, `GovBase`, `GovValidator` 의 배치를 제시한다. 9절은 StableNet testnet genesis 로 계산한 예제다. 10절은 완결성을 위해 나머지 contract 들의 배치를 나열하고, 10.1절에서 그 contract 들의 genesis initializer 가 쓰는 storage 를 정한다. 11절과 12절은 계정 `Extra` 비트, 그 비트를 쓰는 native manager, 그리고 `GovValidator` 가 쓰는 BLS proof-of-possession precompile 을 정의한다. 13절은 token contract 의 bytecode 에서 나오는 보안 성질을 적는다.

따로 말하지 않으면 "slot `0x33`" 은 32 바이트 storage key `0x000…0033` 을 뜻하고, storage 값은 32 바이트 big-endian word 다.

> 해설: 노드가 contract 를 호출하지 않고 slot 을 직접 읽는다는 설계 때문에, 재구현은 Solidity 소스의 의미만 맞춰서는 부족하다. 노드가 계산하는 slot key 와 slot 해석 알고리즘까지 비트 단위로 같아야 같은 validator 목록, 같은 BLS 키, 같은 gas tip 을 얻는다.

---

## 2. System contract 등록부

### 2.1 설정 객체

System contract 하나는 `SystemContract{Address, Version, Params}` 세 값으로 설정한다. 여기서 `Params` 는 문자열에서 문자열로 가는 map 이다. System contract 의 집합은 `SystemContracts{GovValidator, NativeCoinAdapter, GovMinter, GovMasterMinter, GovCouncil}` 이고, 각 필드는 pointer 이므로 없을 수도 있다. Chain configuration 의 Anzeon 절은 genesis 집합(`anzeon.systemContracts`)을 담고, upgrade 는 부분 집합(`Upgrade{Block, SystemContracts}`)을 담는다.

[SNET-SYS-001] Anzeon 이 켜진 chain 은 반드시 다섯 system contract(`GovValidator`, `NativeCoinAdapter`, `GovMasterMinter`, `GovMinter`, `GovCouncil`)를 모두 `anzeon.systemContracts` 에 설정해야 한다. 하나라도 빠진 설정은 무효이며, 노드는 반드시 시작을 거부해야 한다.
Source: params/config_wbft.go:95-115 (AnzeonConfig.CheckValidity)
Source: params/config_wbft.go:146-172 (SystemContracts, SystemContract)

[SNET-SYS-002] `anzeon.systemContracts` 에 있는 모든 contract 의 `Version` 은 반드시 bytecode 가 등록된 version(2.3절)이어야 한다. 그렇지 않으면 설정은 무효다.
Source: systemcontracts/systemcontracts.go:32-60 (checkSystemContractVersions)
Source: params/config_wbft.go:113-115

### 2.2 Contract 목록과 기본 주소

아래 표는 각 contract 와, 내장 네트워크 preset 에서 쓰는 기본 주소, 그리고 bytecode 가 존재하는 version 을 나열한다. 주소는 상수가 아니라 설정이다. Genesis 파일은 contract 를 다른 주소에 두어도 되며(MAY), 이 장의 모든 requirement 는 설정된 주소를 가리킨다.

| Contract | 기본 주소 | Version | Bytecode 출처 | 노드가 실행 중에 읽는가 |
|---|---|---|---|---|
| `NativeCoinAdapter` | `0x0000000000000000000000000000000000001000` | v1 | `artifacts/v1/NativeCoinAdapter` | 읽지 않는다. 이 주소는 coin manager 를 호출할 수 있는 유일한 caller 로만 쓰인다(12.2절) |
| `GovValidator` | `0x0000000000000000000000000000000000001001` | v1 | `artifacts/v1/GovValidator` | 읽는다. candidate 집합, BLS 키, gas tip 을 읽는다(6절, 8절; `B-08`) |
| `GovMasterMinter` | `0x0000000000000000000000000000000000001002` | v1 | `artifacts/v1/GovMasterMinter` | 읽지 않는다 |
| `GovMinter` | `0x0000000000000000000000000000000000001003` | v1, v2 | `artifacts/v1/GovMinter`, `artifacts/v2/GovMinter` | 읽지 않는다 |
| `GovCouncil` | `0x0000000000000000000000000000000000001004` | v1 | `artifacts/v1/GovCouncil` | 읽지 않는다. 이 주소는 account manager 를 호출할 수 있는 유일한 caller 로만 쓰인다(12.1절) |

System contract 가 호출하는 native(Go 로 구현한) contract 와 precompile 은 다음과 같다.

| 이름 | 주소 | 종류 | 정의한 곳 |
|---|---|---|---|
| BLS PoP verifier | `0x0000000000000000000000000000000000b00001` | precompile | 12.3절 |
| `NativeCoinManager` | `0x0000000000000000000000000000000000b00002` | native manager | 12.2절 |
| `AccountManager` | `0x0000000000000000000000000000000000b00003` | native manager | 12.1절 |

[SNET-SYS-003] 내장 StableNet mainnet(chain id 8282)과 testnet(chain id 8283) preset 은 반드시 다섯 system contract 모두에 위의 기본 주소를 써야 하고, genesis 에서 다섯 contract 모두 version `v1` 을 써야 한다.
Source: params/config_wbft.go:31-45 (Default*Address, Default*Version)
Source: params/config.go:77-140 (mainnet preset), params/config.go:197-260 (testnet preset)
Observable: state

Native 주소는 설정이 아니라 고정된 상수다.

[SNET-SYS-004] Anzeon 규칙이 적용되는 모든 block 에서, BLS PoP precompile 은 반드시 `0x…b00001` 에, coin manager 는 `0x…b00002` 에, account manager 는 `0x…b00003` 에 있어야 한다.
Source: params/protocol_params.go:208 (BLSPoPPrecompileAddress), params/protocol_params.go:219-220
Source: core/vm/contracts.go:127-139 (PrecompiledContractsAnzeon), core/vm/contracts.go:141-154 (PrecompiledContractsBoho)
Source: core/vm/native_manager.go:95-109 (NativeManagerContractsAnzeon), core/vm/evm.go:62-72

### 2.3 등록된 bytecode

Bytecode 는 배포된 runtime code 이며, `systemcontracts/artifacts/` 에 hex 텍스트로 저장되어 binary 안에 포함된다. 노드는 이 code 를 계정 code 로 바로 설치하며, constructor 는 실행하지 않는다.

[SNET-SYS-005] 주어진 type 과 version 의 system contract 에 설치하는 code 는 반드시 등록된 artifact 와 바이트 단위로 같아야 한다. Reference commit 에서 등록된 artifact 는 다음과 같다.

| Type, version | Code 길이 (바이트) | `keccak256(code)` |
|---|---|---|
| `GovValidator` v1 | 17266 | `0xb5ce66a9f8c1a08247521c60191f8b11e31e472d8dc26ea163e66de307303dac` |
| `NativeCoinAdapter` v1 | 11179 | `0x5f73e2610ace8f357aba8e0b5e21e263c03d1d65f18a1bb909b5a9b52d0c9703` |
| `GovMinter` v1 | 19124 | `0x3ea99dd77ff9bbc4a88b126e9aa06ddbf144f535a5b432040dbedadd66f3c759` |
| `GovMinter` v2 | 19810 | `0xf369fcc4edab330169f427fd2aedbe97432cf1cfa31200da06464034f3edaa05` |
| `GovMasterMinter` v1 | 18401 | `0x5281507f8f628bbc24856542101a7f1485c88676349b7096b0ae2e734d1c4b54` |
| `GovCouncil` v1 | 20831 | `0x5a6e0f04acf2122487c1c512dc925d5f85a30c4bb6e2a60abba5db009ba472ab` |

Source: systemcontracts/contracts.go:25-90 (embedded artifacts, SystemContractCodes, getContractCode)
Source: systemcontracts/artifacts/v1/*, systemcontracts/artifacts/v2/GovMinter
Observable: state
Observed as: the account `codeHash`, or `eth_getCode`.

구현 노트 (informative). 위 hash 는 reference commit 의 artifact 파일로 계산했다. `systemcontracts/solidity/` 의 Solidity 소스가 이 artifact 의 출처로 문서화되어 있지만, 이 명세는 다시 컴파일한 결과가 아니라 artifact 자체에 묶인다(2.4절).

### 2.4 재현 가능한 빌드 (informative)

규범 내용은 SNET-SYS-005 의 artifact 바이트이다. 이 절은 Solidity 소스에서 그 바이트를 다시 만드는 데 필요한 것을 적는다. 이 절의 어떤 내용도 적합성 requirement 가 아니다.

- Compiler: `systemcontracts/compile` 은 solc `0.8.14` 를 받아서 쓴다(`compile/compiler/compiler.go:37`).
- 옵션: `--optimize` 를 주고 나머지는 기본값(optimizer runs 200, compiler 의 기본 EVM 버전)을 쓴다. Artifact 는 `bin-runtime` 출력이다(`compile/compiler/compiler.go:48-55`, `compile/main.go:37-73`).
- OpenZeppelin: git submodule 두 개가 있다(`.gitmodules`). `systemcontracts/solidity/openzeppelin/contracts` 는 `openzeppelin-contracts` commit `d4fb3a89f9d0a39c7ee6f2601d33ffbf30085322` 에 고정되어 있고, 이 commit 은 tag `v4.6.0` 이다. `systemcontracts/solidity/openzeppelin/contracts-upgradeable` 는 `openzeppelin-contracts-upgradeable` commit `6b9807b0639e1dd75e07fa062e9432eb3f35dd8c` 에 고정되어 있고, 이 commit 은 tag `v4.7.0` 이다. commit 은 `git ls-tree HEAD systemcontracts/solidity/openzeppelin/` 로, tag 는 두 upstream 저장소의 `git ls-remote` 로 확인했다. import 접두어는 `systemcontracts/solidity/remappings.txt` 로 풀린다.
- Artifact 는 `openzeppelin-contracts` 에서 `EnumerableSet`, `SafeMath`, `IERC20`, `IERC1271` 만 가져온다. 어떤 artifact 도 `openzeppelin-contracts-upgradeable` 을 쓰지 않는다.
- Submodule 은 reference tree 에 checkout 되어 있지 않다. 그래서 tree 만으로는 artifact 를 다시 만들 수 없고, submodule 을 먼저 받아야 한다.

다시 컴파일한 바이트를 SNET-SYS-005 와 대조하는 일은 하지 않았다. 이 대조는 벡터 생성기(`A-11`)의 선택 검사로 둔다.

---

## 3. Genesis 에서의 설치

### 3.1 Transition 객체

설치와 upgrade 는 둘 다 *state transition* 으로 표현한다. State transition 은 code 쓰기 `CodeParam{Address, Code}` 의 목록과 storage 쓰기 `StateParam{Address, Key, Value}` 의 목록으로 이루어진다.

```python
def system_contracts_transition(scs: SystemContracts, alloc) -> StateTransition:
    # Order is fixed: GovValidator, NativeCoinAdapter, GovMinter, GovMasterMinter, GovCouncil.
    st = StateTransition(codes=[], states=[])
    for kind, sc in [("GovValidator", scs.GovValidator), ("NativeCoinAdapter", scs.NativeCoinAdapter),
                     ("GovMinter", scs.GovMinter), ("GovMasterMinter", scs.GovMasterMinter),
                     ("GovCouncil", scs.GovCouncil)]:
        if sc is None:
            continue
        code = registered_code(kind, sc.Version)          # error if not registered
        st.codes.append((sc.Address, code))
        if sc.Params is not None and sc.Version == "v1":
            a = alloc if kind in ("NativeCoinAdapter", "GovCouncil") else None   # only these two read the allocation
            st.states += initializer(kind)(sc.Address, sc.Params, a)   # may raise
    return st
```

[SNET-SYS-010] 설정된 contract 마다 transition 은 반드시 `(type, Version)` 에 등록된 code 를 쓰는 code 쓰기를 하나 담아야 한다. Transition 은 `Version == "v1"` 이고 `Params` 가 있을 때, 그리고 그때에만 반드시 initializer 의 storage 쓰기를 담아야 한다. 다른 version 은 code 만 바꾼다.
Source: systemcontracts/systemcontracts.go:62-157 (GetSystemContractsTransition)

Initializer 가 쓰는 genesis 의존 값(gas tip, native coin 총 공급량, `GovCouncil` 집합)과 parameter 검사는 `B-02` §4.3-§4.4 가 정한다. Parameter 의 거버넌스 의미는 `B-05` 가 정한다. `initializeValidator` 가 만드는 storage 는 노드가 읽는 배치를 정하므로 8.3절에서 정한다. `NativeCoinAdapter`, `GovMinter`, `GovMasterMinter` initializer 가 쓰는 storage 는 10.1절에서 정하고, 나머지 contract 의 배치는 10절(informative)에 있다.

### 3.2 Genesis transition 적용

[SNET-SYS-011] Anzeon genesis state 를 만들 때 노드는 반드시 `anzeon.systemContracts` 로 transition 을 계산해야 한다. 그리고 code 쓰기마다 그 주소의 genesis allocation 항목을 새 계정으로 바꿔야 한다. 새 계정은 등록된 code 를 가지며, balance 0, nonce 0, `Extra` 0, 빈 storage 를 가진다. 그 뒤 노드는 반드시 transition 의 모든 storage 쓰기를 그 allocation 에 적용해야 한다.
Source: core/genesis.go:734-756 (InjectContracts, baseline part)
Observable: state

귀결 (normative, SNET-SYS-011 에서 따라 나온다): 노드는 genesis 파일이 system contract 주소에 직접 준 balance, nonce, `Extra` 비트, storage 를 모두 버린다.

[SNET-SYS-012] Baseline transition 을 적용한 뒤, 노드는 반드시 `Block` 이 0 인 모든 upgrade(4.1절)를 등록부 순서대로 *overlay* 로 적용해야 한다. Overlay 는 code 쓰기마다 기존 allocation 항목의 code 만 바꾸고 balance, nonce, `Extra`, storage 는 그대로 둔다. 그 주소에 항목이 없으면 balance 0 과 빈 storage 를 가진 새 항목을 만든다. 그다음 upgrade 의 storage 쓰기를 적용한다.
Source: core/genesis.go:758-768 (InjectContracts, block-0 upgrades), core/genesis.go:771-797 (applyUpgradeOverlay)
Observable: state

예 (informative). Mainnet preset 은 `BohoBlock = 0` 으로 설정하고 `GovMinter` v2 를 쓴다. 그래서 mainnet genesis 의 `0x…1003` 에는 `GovMinter` v2 code 가 있고, storage 는 v1 initializer 가 쓴 값이다.

[SNET-SYS-013] Genesis allocation 의 계정은 bit 63 과 bit 62(11절) 외의 `Extra` 비트가 켜져 있어서는 안 된다. 다른 비트가 켜진 genesis 는 무효다.
Source: core/genesis.go:252-262 (initializeAnzeonGenesis, ValidateExtra), core/types/state_account_extra.go:100-108
Observable: state

---

## 4. Genesis 이후의 upgrade

### 4.1 Upgrade 목록

노드는 순서가 있는 upgrade 목록 하나를 유지한다. 첫 항목은 block 0 의 Anzeon baseline 이다. 그 뒤 항목들은 hard-fork 등록부 `ChainConfig.CollectUpgrades()` 에서 오며, reference commit 에서는 이 등록부에 항목이 정확히 하나 있다.

| 이름 | Block | 내용 | Preset |
|---|---|---|---|
| Anzeon baseline | 0 | `anzeon.systemContracts` (다섯 개 모두, v1) | 모든 preset |
| Boho | `bohoBlock` | `boho.systemContracts` (preset 에서는 `GovMinter{0x…1003, v2, Params: none}`) | mainnet 은 `bohoBlock = 0`, testnet 은 `bohoBlock = 14408500` |

이 목록은 `B-01` §7.1 의 upgrade 목록 `upgrade_list` 와 같다. `B-01` 은 이 목록을 `collect_upgrades` 로 만든다. 여기서는 chain configuration 을 명시적인 인자로 받도록 썼다.

```python
def upgrade_list(chain_config) -> list[Upgrade]:
    ups = [Upgrade(block=0, contracts=chain_config.anzeon.systemContracts)]
    if chain_config.bohoBlock is not None and chain_config.boho is not None \
            and chain_config.boho.systemContracts is not None:
        ups.append(Upgrade(block=chain_config.bohoBlock, contracts=chain_config.boho.systemContracts))
    return ups   # ascending by block; the fork-order check at start-up enforces this
```

[SNET-SYS-020] 노드가 시작할 때 upgrade 목록(위의 `upgrade_list`)을 만드는 단계에는 `B-01` [SNET-CFG-018] 이 적용된다. 이 단계는 `system_contracts_at` 을 읽거나 4.2절의 upgrade 단계를 수행하기 전이다. 그 목록은 반드시 SNET-CFG-018 이 정하는 목록이어야 한다. 기준 commit 의 hard-fork 등록부로는 그 목록이 block 0 의 Anzeon baseline 으로 시작하고, `bohoBlock` 이 설정되어 있고 `boho.systemContracts` 가 있을 때, 그리고 그때에만 Boho 항목이 뒤따른다.
Source: eth/ethconfig/config.go:264-271 (SystemContractUpgrades assembly)
Source: params/config.go:1089-1126 (CollectUpgrades)
Source: params/config.go:144-151, params/config.go:264-271 (Boho presets), params/config.go:65, params/config.go:169 (BohoBlock)

### 4.2 Block N > 0 에서 upgrade 적용

[SNET-SYS-021] Block `N > 0` 을 finalize 할 때, 노드는 다른 finalization 단계(base fee 분배, epoch 처리, gas tip 검증, state root 계산)보다 먼저 `Block == N` 인 모든 upgrade 의 transition 을 목록 순서대로 반드시 계산해야 한다. 그리고 그 transition 을 block 의 트랜잭션 실행 뒤 state 에 적용해야 한다. 이때 노드는 먼저 모든 code 쓰기를 적용해 code 만 교체하고, 그다음 모든 storage 쓰기를 적용한다.
Source: consensus/wbft/engine/engine.go:929-938 (processFinalize, first step)
Source: consensus/wbft/config.go:199-225 (GetSystemContractsStateTransition)
Observable: state

[SNET-SYS-022] Upgrade block `N` 에 들어 있는 트랜잭션은 반드시 `N` 이전에 적용되던 code 로 실행되어야 한다. 새 code 는 block `N + 1` 부터 적용되며, block `N` 의 finalization 단계 가운데 upgrade 뒤에 오는 단계에도 적용된다.
Source: core/state_processor.go:89-102 (transactions are applied before Finalize)
Source: consensus/wbft/engine/engine.go:929-938
Observable: state

[SNET-SYS-024] Upgrade 의 transition 계산이 실패하면 block `N` 의 finalization 은 반드시 실패해야 한다. 예를 들어 version 이 등록되지 않았거나 parameter 가 잘못되었으면 계산이 실패한다. 따라서 유효한 block `N` 은 존재하지 않는다. 노드는 block `N` 의 finalization 전에 upgrade 항목을 따로 검사하지 않는다.
Source: consensus/wbft/engine/engine.go:930-931, consensus/wbft/config.go:207-218
Source: params/config_wbft.go:113 (only `anzeon.systemContracts` is version-checked)

구현 노트 (informative). Upgrade 로 code 를 교체해도 storage 가 안전하려면 새 code 의 배치가 옛 배치의 상위 집합이어야 한다. `GovMinter` v2 는 v1 변수들 뒤에 변수 하나(`refundableBalance`, slot `0x3d`)만 덧붙이고 v1 의 slot 을 모두 유지한다(10절).

### 4.3 주소 해석

Block `n` 에서 유효한 system contract 집합은 upgrade 목록을 필드 단위로 병합해서 얻는다. 뒤의 upgrade 가 어떤 contract 를 지정하면, 그 contract 의 `SystemContract` 항목 전체(주소, version, params)가 교체된다.

아래 함수는 `B-01` §7.2 의 `system_contracts_at(n)` 과 같으며, 여기서는 upgrade 목록을 명시적인 인자로 받는다.

```python
def system_contracts_at(n: int, upgrades: list[Upgrade]) -> SystemContracts:
    gc = SystemContracts()                       # all fields absent
    for u in upgrades:                           # ascending order
        if u.block > n:
            break
        for field in ["GovValidator", "NativeCoinAdapter", "GovMinter", "GovMasterMinter", "GovCouncil"]:
            if getattr(u.contracts, field) is not None:
                setattr(gc, field, getattr(u.contracts, field))
    return gc
```

[SNET-SYS-025] 아래 표에서 upgrade-aware 주소를 쓰는 모든 reader 에는 `B-01` [SNET-CFG-019] 가 적용된다. block `n` 에서 system contract 의 주소는 반드시 SNET-CFG-019 가 정하는 대로 풀어야 하며, 그 주소는 `system_contracts_at(n)` 에 있는 그 contract 의 `Address` 이다.
Source: consensus/wbft/config.go:124-159 (GetSystemContracts, getSystemContractsValue)

모든 reader 가 upgrade-aware 주소를 쓰지는 않는다. Reference commit 에서 각 reader 는 다음과 같이 주소를 해석한다.

| Reader | 읽는 것 | 사용하는 주소 | Source |
|---|---|---|---|
| Epoch 계산: candidate 목록 | `GovValidator` validator 집합 | upgrade-aware 주소, epoch block 번호 기준 | consensus/wbft/engine/engine.go:607-612, :806 |
| Epoch 계산: BLS 키 | `GovValidator.validatorToBlsKey` | upgrade-aware 주소, epoch block 번호 기준 | consensus/wbft/engine/engine.go:875, :894 |
| Header gas tip (block 을 만들 때와 검증할 때) | `GovValidator.gasTip` | genesis 주소 (`anzeon.systemContracts.GovValidator.Address`) | consensus/wbft/engine/engine.go:639-640 |
| Miner / txpool gas tip | `GovValidator.gasTip` | genesis 주소 | miner/worker.go:1209-1218 |
| Coin manager caller 검사 | caller 주소 | genesis `NativeCoinAdapter` 주소 | core/vm/native_manager.go:463-468 |
| Account manager caller 검사 | caller 주소 | genesis `GovCouncil` 주소 | core/vm/native_manager.go:470-475 |
| Native `Transfer` log | log 주소 | genesis `NativeCoinAdapter` 주소 | core/vm/evm.go:590-611 |

---

## 5. Reader 가 쓰는 Solidity storage 인코딩

이 절은 노드의 reader 가 의존하는 Solidity storage 규칙을 다시 적는다. `pad32(x)` 는 `x` 의 왼쪽을 0 바이트로 채워 32 바이트로 만든 값이다. `u256(i)` 는 정수 `i` 의 32 바이트 big-endian 인코딩이다. `keccak256` 은 Keccak-256(`A-02`)이다. Slot 산술은 2^256 을 법으로 한다.

### 5.1 값 타입과 packing

값 타입 state 변수는 다음 빈 slot 을 차지한다. 32 바이트보다 작은 변수들은 선언 순서대로 같은 slot 에 packing 되며, 가장 낮은(가장 오른쪽) 바이트부터 채운다. `address` 는 낮은 20 바이트에 저장된다. `bool` 은 1 바이트이고, `uint32` 는 4 바이트다.

### 5.2 Mapping

```python
def mapping_slot(base: int, key: bytes, key_is_value_type: bool) -> bytes32:
    # value-type key (address, uintN, bytes32): padded to 32 bytes
    # bytes/string key: raw bytes, not padded
    k = pad32(key) if key_is_value_type else key
    return keccak256(k + u256(base))
```

[SNET-SYS-030] Reader 는 base slot `base` 에 있는 `mapping(K => V)` 의 항목 `key` 의 slot 을 반드시 다음과 같이 계산해야 한다. `K` 가 값 타입이면 slot 은 `keccak256(pad32(key) ‖ u256(base))` 이다. 이때 주소는 20 바이트 주소의 왼쪽을 채운 값이다. `K` 가 `bytes` 나 `string` 이면 slot 은 `keccak256(key ‖ u256(base))` 이다.
Source: systemcontracts/stateutil.go:31-38 (CalculateMappingSlot)
Source: systemcontracts/solidity/v1/GovValidator.sol:43-46

구현 노트 (informative). Reference 구현의 `CalculateMappingSlot` 은 모든 key 를 `LeftPadBytes(key, 32)` 로 왼쪽 패딩하며, 32 바이트 이상인 key 는 바꾸지 않는다. 노드가 쓰는 유일한 `bytes` key 는 48 바이트 BLS public key(`blsKeyToValidator`)이므로 이 경우 결과는 Solidity 규칙과 같다. 32 바이트보다 짧은 `bytes` key 라면 결과가 달라지겠지만, 그런 key 를 쓰는 reader 는 없다.

> 해설: 재구현은 Solidity 규칙대로 짧은 `bytes` key 를 패딩하지 않고 처리하는 것이 옳다. 지금은 그런 key 를 읽는 곳이 없으므로 reference 구현과의 차이가 드러나지 않는다.

중첩 mapping `mapping(A => mapping(B => V))` 의 항목 `[a][b]` 는 `mapping_slot(mapping_slot(base, a), b)` 에 있다. 이때 안쪽 base 는 32 바이트 바깥 slot 을 정수로 해석한 값이다.

### 5.3 동적 배열

```python
def array_length_slot(base: int) -> int:
    return base

def array_element_slot(base: int, i: int) -> int:
    # one element per slot for 32-byte and address elements
    return (int(keccak256(u256(base))) + i) % 2**256
```

[SNET-SYS-031] Reader 는 base slot `base` 에 있는 동적 배열의 길이를 반드시 slot `base` 에서 읽어야 한다. 원소가 32 바이트이거나 address 타입이면, 원소 `i` 는 반드시 slot `keccak256(u256(base)) + i` 에서 읽어야 한다.
Source: systemcontracts/stateutil.go:40-50 (CalculateDynamicSlot)

### 5.4 `bytes` 와 `string`

Base slot 이 `p` 인 `bytes` 또는 `string` 변수는 두 형태 가운데 하나로 저장된다. 두 형태는 `p` 에 있는 word 의 최하위 비트로 구분한다.

- 짧은 형태(길이 `L ≤ 31`): `p` 의 word 는 데이터를 바이트 `0..L-1` 에 왼쪽 정렬로 담고, 바이트 31 에 `2·L` 을 담는다. 최하위 비트는 0 이다.
- 긴 형태(`L ≥ 32`): `p` 의 word 는 정수 `2·L + 1` 을 담는다. 데이터는 `keccak256(u256(p))` 부터 연속된 `ceil(L/32)` 개 slot 에 왼쪽 정렬로 들어가며, 마지막 slot 에서 쓰지 않는 뒷부분은 0 이다.

```python
def read_bytes(state, account, p: int) -> bytes:
    w = state.get_storage(account, p)          # 32 bytes
    if w == bytes(32):
        return b""
    if w[31] & 1 == 0:                          # short form
        L = w[31] >> 1
        return w[:L]
    L = int.from_bytes(w, "big") >> 1           # long form
    out = b""
    i = 0
    while True:
        out += state.get_storage(account, array_element_slot(p, i))
        i += 1
        if len(out) >= L:
            break
    return out[:L]
```

[SNET-SYS-032] `bytes` 값을 읽는 reader 는 5.4절 writer 나 contract 가 만들 수 있는 모든 word(짧은 형태에서 `L ≤ 31`, 긴 형태에서 `L < 2^63`)에 대해 반드시 위의 `read_bytes` 를 구현해야 한다. 구체적으로, `p` 의 word 가 모두 0 이면 결과는 빈 문자열이다. 짧은 형태에서 reader 는 반드시 word 의 처음 `w[31] >> 1` 바이트를 돌려주고 나머지는 무시해야 한다. 긴 형태에서 reader 는 반드시 `max(1, ceil(L/32))` 개의 데이터 slot 을 읽고 처음 `L` 바이트를 돌려줘야 한다. 그 밖의 word 에 대해서는 reference 동작이 값을 내지 않는다. 짧은 형태에서 `w[31] >> 1 > 32` 이거나, 긴 형태에서 `L` 의 하위 64비트가 `2^63 − 1` 보다 커서 음수 부호 정수로 읽히면 reference reader 는 panic 한다. 긴 형태에서 `L` 의 하위 64비트가 0 이면 reference reader 는 slot 하나를 읽은 뒤 빈 문자열을 돌려준다.
Source: systemcontracts/stateutil.go:102-149 (GetBytes)
Observable: state

[SNET-SYS-033] Genesis 에서 `bytes` 값을 storage 에 쓰는 writer 는 반드시 같은 두 형태를 써야 한다. `L ≤ 31` 이면 데이터를 왼쪽 정렬하고 마지막 바이트에 `2·L` 을 넣은 word 하나를 쓴다. `L ≥ 32` 이면 `p` 에 word `2·L + 1` 을 쓰고, 데이터를 `keccak256(u256(p))` 부터 `ceil(L/32)` 개 slot 에 쓴다. `L = 0` 이면 0 word 를 쓴다.
Source: systemcontracts/stateutil.go:151-187 (VarLenBytesToMultipleHash), systemcontracts/stateutil.go:189-237 (EncodeBytesToSlots)
Source: systemcontracts/gov_validator.go:186-201 (MakeMultipleParam)
Observable: state

---

## 6. `EnumerableSet.AddressSet` 배치와 순회 순서

`GovValidator` 는 validator 주소를 OpenZeppelin `EnumerableSet.AddressSet` 에 보관한다(OpenZeppelin Contracts, `EnumerableSet.sol` "last updated v4.6.0", 고정 commit `d4fb3a89`). `AddressSet` 은 `Set _inner` 필드 하나만 가진 struct 이며, `Set` 은 다음과 같다.

```solidity
struct Set {
    bytes32[] _values;                    // slot s
    mapping(bytes32 => uint256) _indexes; // slot s + 1; value = array index + 1, 0 = absent
}
```

주소는 `bytes32(uint256(uint160(addr)))` 로, 곧 왼쪽을 채워 32 바이트로 만든 값으로 저장된다.

### 6.1 배치

Slot `s` 에 선언된 집합의 배치는 다음과 같다.

| 항목 | Slot | 값 |
|---|---|---|
| 길이 `L` | `s` | `u256(L)` |
| 원소 `i` (`0 ≤ i < L`) | `keccak256(u256(s)) + i` | `pad32(addr)` |
| `addr` 의 위치 | `keccak256(pad32(addr) ‖ u256(s + 1))` | `addr` 가 원소 `i` 이면 `u256(i + 1)`, 아니면 0 |

[SNET-SYS-040] Reader 는 slot `s` 에 있는 `AddressSet` 의 원소를 반드시 목록 `[low20(word(keccak256(u256(s)) + i)) for i in 0..L-1]` 로 얻어야 한다. 여기서 `L` 은 slot `s` 의 정수이고, `low20` 은 word 의 낮은 20 바이트를 취한다. 목록 순서는 배열 순서이며, 이 순서는 규범이다.
Source: systemcontracts/stateutil.go:56-100 (EnumerableSet, Values, HashToAddress)
Source: systemcontracts/gov_validator.go:203-206 (ValidatorList)
Observable: state

[SNET-SYS-041] Reader 는 `keccak256(pad32(addr) ‖ u256(s + 1))` 의 word 가 0 이 아닐 때, 그리고 그때에만 반드시 `addr` 를 집합의 원소로 취급해야 한다.
Source: systemcontracts/stateutil.go:73-77 (Contains)
Observable: state

### 6.2 순서가 바뀌는 방식 (contract 동작을 다시 적은 informative 내용이며, mirror 에게는 normative 다)

집합의 배열 순서는 일반적으로 삽입 순서가 아니다. Contract 는 다음 두 방식으로만 순서를 바꾼다.

- `add(v)`: `v` 가 없으면 `v` 를 인덱스 `L` 에 덧붙이고, `_indexes[v] = L + 1` 로 설정하며, 길이는 `L + 1` 이 된다.
- `remove(v)`: `v` 가 인덱스 `k` 에 있을 때, `k ≠ L − 1` 이면 마지막 원소 `w` 를 인덱스 `k` 로 옮기고 `_indexes[w] = k + 1` 로 설정한다. 그다음 마지막 배열 slot 을 pop 하므로 인덱스 `L − 1` 의 slot 은 0 이 된다. 이어서 `_indexes[v]` 를 지우며, 길이는 `L − 1` 이 된다.

```python
def set_add(values: list, v) -> None:
    if v not in values:
        values.append(v)

def set_remove(values: list, v) -> None:
    if v in values:
        k = values.index(v)
        last = values[-1]
        values[k] = last          # no-op when v is last
        values.pop()
```

---

## 7. `GovBase` 배치 (slot `0x00`–`0x31`)

`GovValidator`, `GovCouncil`, `GovMinter`, `GovMasterMinter` 는 `GovBase` 를 상속한다. `GovBase` 의 변수는 37 slot 의 예약 gap 을 포함해 slot 0 부터 49(`0x31`)까지 차지한다. 노드는 실행 중에 이 slot 을 하나도 읽지 않지만, genesis initializer 는 그 가운데 여러 개를 쓴다(`B-02`). 이 배치를 제시하는 이유는 두 가지다. 하나는 mirror 나 inspector 가 거버넌스 state 를 읽을 수 있게 하려는 것이고, 다른 하나는 파생 contract 의 자체 변수가 `0x32` 에서 시작하는 근거를 보이려는 것이다.

| Slot | 변수 | 타입 | 인코딩과 참고 | Genesis 에서 쓰는가 |
|---|---|---|---|---|
| `0x00` | `proposalExpiry` | `uint256` | 정수(초) | `expiry` param 이 있으면 쓴다 |
| `0x01` | `memberVersion` | `uint256` | 정수. constructor 가 실행되지 않으므로 Solidity initializer `= 1` 은 실행되지 않는다 | `members` param 이 있으면 쓴다 (값은 `memberVersion` param) |
| `0x02` | `currentProposalId` | `uint256` | 정수 | 쓰지 않는다 |
| `0x03` | `__reentrancyGuard` | `uint256` | 정수 | 쓰지 않는다 |
| `0x04` | `quorum` | `uint32` | word 의 낮은 4 바이트. 바이트 0–27 은 쓰지 않는다 | `quorum` param 이 있으면 쓴다 |
| `0x05` | `members` | `mapping(address => Member)` | `Member{bool isActive; uint32 joinedAt}` 를 word 하나에 packing 한다. 바이트 31 은 `isActive`, 바이트 27–30 은 `joinedAt`(big-endian)이다 | member 마다 항목 하나를 `isActive = 1`, `joinedAt = 0` 으로 쓴다 |
| `0x06` | `versionedMemberList` | `mapping(uint256 => address[])` | 길이는 `mapping_slot(6, u256(v))` 에 있고, 원소 `i` 는 `keccak256(that slot) + i` 에 있다 | `v = memberVersion` 에 대해 쓴다 |
| `0x07` | `proposals` | `mapping(uint256 => Proposal)` | struct base 는 `b = mapping_slot(7, u256(id))` 이다. `b+0` actionType, `b+1` memberVersion, `b+2` votedBitmap, `b+3` createdAt, `b+4` executedAt, `b+5` proposer (낮은 20 바이트) ‖ requiredApprovals (바이트 8–11) ‖ approved (바이트 4–7) ‖ rejected (바이트 0–3), `b+6` status (낮은 바이트), `b+7` callData (`bytes`, 5.4절) | 쓰지 않는다 |
| `0x08` | `_memberIndexByVersion` | `mapping(uint256 => mapping(address => uint32))` | index + 1 | `v = memberVersion` 에 대해 쓴다 |
| `0x09` | `_quorumByVersion` | `mapping(uint256 => uint32)` | 정수 | `members` param 과 0 이 아닌 `quorum` param 이 모두 있으면 `v = memberVersion` 에 대해 쓴다 |
| `0x0a` | `proposalExecutionCount` | `mapping(uint256 => uint256)` | 정수 | 쓰지 않는다 |
| `0x0b` | `memberActiveProposalCount` | `mapping(address => uint256)` | 정수 | 쓰지 않는다 |
| `0x0c` | `maxActiveProposalsPerMember` | `uint256` | 정수 | 항상 쓴다 (기본값 3) |
| `0x0d`–`0x31` | `__gap` | `uint256[37]` | 예약 공간이며 0 이다 | 쓰지 않는다 |

`proposals` 행의 바이트 오프셋은 word 의 최상위 바이트(바이트 0)부터 최하위 바이트(바이트 31)까지 센다. 그래서 `proposer` 는 바이트 12–31 을 차지한다.

[SNET-SYS-050] `GovBase` 에서 파생된 모든 contract 의 storage 는 반드시 위 표를 따라야 한다. 특히 파생 contract 의 첫 변수는 반드시 slot `0x32` 에 있어야 한다.
Source: systemcontracts/solidity/abstracts/GovBase.sol:57-93 (Member, Proposal), systemcontracts/solidity/abstracts/GovBase.sol:129-153 (state variables and gap)
Source: systemcontracts/gov_base.go:38-66 (SLOT_GOV_BASE_*), systemcontracts/gov_base.go:119-133 (Member.ToHash)
Observable: state

---

## 8. `GovValidator` 배치 (slot `0x32`–`0x39`)

### 8.1 배치

| Slot | 변수 | 타입 | 인코딩 | 노드가 실행 중에 읽는가 |
|---|---|---|---|---|
| `0x32` | `blsPoP` | `address` | 낮은 20 바이트. genesis 값은 `0x…b00001` | 읽지 않는다 |
| `0x33` | `__validators._inner._values` | `bytes32[]` | `AddressSet`(6절). 길이는 이 slot 에, 원소는 `keccak256(u256(0x33)) + i` 에 있다 | 읽는다 (길이, 원소) |
| `0x34` | `__validators._inner._indexes` | `mapping(bytes32 => uint256)` | 위치 + 1 | 읽지 않는다 (genesis 에서 쓴다) |
| `0x35` | `validatorToOperator` | `mapping(address => address)` | 낮은 20 바이트 | 읽지 않는다 |
| `0x36` | `operatorToValidator` | `mapping(address => address)` | 낮은 20 바이트 | 읽지 않는다 |
| `0x37` | `validatorToBlsKey` | `mapping(address => bytes)` | `mapping_slot(0x37, addr)` 에 있는 `bytes`(5.4절). 48 바이트 key 는 긴 형태를 쓴다(head slot 에 `0x61` = 2·48+1, 데이터 slot 둘) | 읽는다 |
| `0x38` | `blsKeyToValidator` | `mapping(bytes => address)` | key 는 패딩하지 않은 48 바이트 BLS 키 원본이다 | 읽지 않는다 |
| `0x39` | `gasTip` | `uint256` | 정수 (wei) | 읽는다 |

[SNET-SYS-060] `GovValidator` storage 는 반드시 위 표를 따라야 한다.
Source: systemcontracts/solidity/v1/GovValidator.sol:40-47 (state variables with slot comments)
Source: systemcontracts/gov_validator.go:28-42 (SLOT_VALIDATOR_*)
Observable: state

구현 노트 (informative). Storage 배치 artifact(`solc --storage-layout`)는 commit 되어 있지 않다. Slot 번호는 세 가지 방법으로 확인했다. 첫째, `GovBase.sol`(slot 0–12 의 변수 13 개와 slot 49 까지의 37 slot gap)과 `GovValidator.sol` 에 Solidity 배치 규칙을 적용해 계산했다. 둘째, Go 상수와 비교했다. 셋째, 포함된 `GovValidator` v1 bytecode 를 reference EVM 에서 실행했다. 그리고 genesis 직후와 `configureValidator` 호출 뒤에 `validatorList()`, `validatorToBlsKey(addr)`, `gasTip()` 의 결과를 slot reader 의 결과와 비교했다(9.3절).

### 8.2 Reader 함수

```python
GOV_VALIDATOR_SET_SLOT = 0x33
GOV_VALIDATOR_BLS_SLOT = 0x37
GOV_VALIDATOR_GASTIP_SLOT = 0x39

def validator_list(state, gv: Address) -> list[Address]:
    L = int(state.get_storage(gv, u256(GOV_VALIDATOR_SET_SLOT)))
    base = int(keccak256(u256(GOV_VALIDATOR_SET_SLOT)))
    return [low20(state.get_storage(gv, u256((base + i) % 2**256))) for i in range(L)]

def bls_public_key(state, gv: Address, validator: Address) -> bytes:
    p = int(mapping_slot(GOV_VALIDATOR_BLS_SLOT, validator, key_is_value_type=True))
    return read_bytes(state, gv, p)          # b"" if absent

def gas_tip(state, gv: Address) -> int:
    return int(state.get_storage(gv, u256(GOV_VALIDATOR_GASTIP_SLOT)))   # 0 if absent
```

[SNET-SYS-061] `validator_list` 는 반드시 slot `0x33` 에 있는 `AddressSet` 의 원소를 배열 순서대로 돌려줘야 한다(SNET-SYS-040). 계정에 storage 가 없으면(예: `gv` 에 contract 가 없으면) 반드시 빈 목록을 돌려줘야 하며, 실패해서는 안 된다.
Source: systemcontracts/gov_validator.go:203-206, systemcontracts/stateutil.go:69-86
Observable: state

[SNET-SYS-062] `bls_public_key` 는 반드시 `mapping_slot(0x37, validator)` 에 대한 `read_bytes` 결과를 돌려줘야 한다. 키가 없는 주소에 대해서는 반드시 빈 문자열을 돌려줘야 한다. 결과가 비어 있지 않을 때 그 길이를 검사해서는 안 된다.
Source: systemcontracts/gov_validator.go:208-210, systemcontracts/stateutil.go:103-149
Observable: state

### 8.3 Genesis initializer 가 쓰는 storage

`initializeValidator` 는 먼저 `GovBase` initializer(7절)를 실행하고, 그다음 아래 slot 을 쓴다. Parameter 이름은 `GovValidator.Params` 의 key 다. Parameter 검사는 `B-02` 에 있다.

1. slot `0x32` 에 `pad32(0x…b00001)` 을 쓴다.
2. slot `0x39` 에 `u256(abs(g) mod 2^256)` 을 쓴다. 여기서 `g` 는 parameter `gasTip` 이 있으면 그 값을 Go `big.Int.SetString` 이 10진수 정수로 파싱한 값이고(앞에 붙은 부호도 받아들인다), 없으면 `InitialGasTip = 27600000000000` 이다. Genesis 에서 `g` 가 음수이면 genesis 생성이 멈춘다. Genesis `Extra` 가 음수 gas tip 을 인코딩할 수 없기 때문이다(`B-02` SNET-GEN-010). `g` 가 `2^256` 이상이면 slot 에는 `2^256` 으로 나눈 나머지가 들어가고, genesis `Extra` 에는 원래 값 전체가 들어간다.
3. `validators` 가 있으면 다음을 수행한다. `M` 은 `members` 를 파싱한 목록이다. 파싱은 쉼표로 나누고, 각 항목의 앞뒤 공백을 지우고, 빈 항목은 버리며, 중복은 제거하지 않는다. `V` 는 `validators` 를 같은 방법으로 파싱한 목록이고, `K` 는 `blsPublicKeys` 를 파싱해 hex 디코딩한 목록이다. Initializer 는 각 `i` 에 대해 순서대로 아래 쓰기를 하며, 이미 나온 `V[i]` 는 건너뛴다. 여기서 `j` 는 지금까지 쓴 validator 의 수다.
   - `mapping_slot(0x34, V[i])` 에 `u256(j + 1)` 을 쓴다.
   - `keccak256(u256(0x33)) + j` 에 `pad32(V[i])` 를 쓴다.
   - `mapping_slot(0x35, V[i])` 에 `pad32(M[i])` 를 쓴다.
   - `mapping_slot(0x36, M[i])` 에 `pad32(V[i])` 를 쓴다.
   - `mapping_slot(0x37, V[i])` 의 `bytes` 에 `K[i]` 를 5.4절의 writer 방식으로 쓴다.
   - `keccak256(K[i] ‖ u256(0x38))` 에 `pad32(V[i])` 를 쓴다.
   마지막으로, validator 를 하나 이상 썼으면 slot `0x33` 에 `u256(count)` 를 쓴다.

[SNET-SYS-064] Genesis `GovValidator` storage 는 반드시 위의 쓰기에 `GovBase` 쓰기를 더한 결과와 정확히 같아야 한다. 특히 genesis 의 validator 집합 순서는 반드시 `validators` parameter 의 순서에서 뒤에 나온 중복을 제거한 순서여야 한다. 그리고 각 validator 의 operator 는 반드시 `members` parameter 에서 같은 위치에 있는 member 여야 한다.
Source: systemcontracts/gov_validator.go:52-184 (initializeValidator)
Source: params/protocol_params.go:138 (InitialGasTip)
Observable: state

구현 노트 (informative). Genesis initializer 는 BLS 키가 48 바이트인지, 유효한 proof of possession 을 가지는지, 두 validator 의 키가 서로 다른지를 검사하지 않는다. Contract 는 `configureValidator` 에서 이 세 가지를 모두 검사한다(`B-05`). Genesis 에 중복 키가 있으면 `blsKeyToValidator[key]` 는 뒤에 나온 validator 를 가리킨다.

---

## 9. 계산 예

### 9.1 Slot 계산 (testnet genesis)

아래 값은 모두 reference code 로 계산했고, `keccak256` 으로 독립적으로 다시 계산해 확인했다. `gv = 0x…1001` 이다. `v0 = 0x9f06600b2c17108662e3840e76bb27c9468eb73d` 는 testnet 의 첫 validator 이고, `m0 = 0x58f13fe4294652526c4b893b4e4f343b3f184d43` 은 그 operator 이며, `pk0` = `0x96683524…84dd9c`(48 바이트)는 그 BLS 키다.

| 항목 | 계산 | Slot | Genesis 값 |
|---|---|---|---|
| validator 수 | slot `0x33` | `0x…33` | `0x…07` |
| validator 원소 base | `keccak256(u256(0x33))` | `0x82a75bdeeae8604d839476ae9efd8b0e15aa447e21bfd7f41283bb54e22c9a82` | `pad32(v0)` |
| 원소 6 | base + 6 | `0x82a75bdeeae8604d839476ae9efd8b0e15aa447e21bfd7f41283bb54e22c9a88` | `pad32(0x0a8dd92ce7ce53bbf6aed40228f9029bb3f92702)` |
| `_indexes[v0]` | `keccak256(pad32(v0) ‖ u256(0x34))` | `0xec0327518f50ff4d7959d421f64d0b6e04957388d59098190131c501f34a6367` | `0x…01` |
| `validatorToBlsKey[v0]` head | `keccak256(pad32(v0) ‖ u256(0x37))` | `0xac97b7a74f2ce4afa1a5c3a3246bf361b051d0fccda38d723e050918cdd16cb2` | `0x…61` (긴 형태, 48 바이트) |
| key 데이터 0 | `keccak256(head)` | `0xd44d80d30d239c4a95e1b27b4a03fdd4739ecd5410a0a79c9bdd315cb66c7b53` | `0x96683524c3b7e224f2146a0dbb87593e3dee21b7d97c1b24ef6ed799c977be40` |
| key 데이터 1 | `keccak256(head) + 1` | `0xd44d80d30d239c4a95e1b27b4a03fdd4739ecd5410a0a79c9bdd315cb66c7b54` | `0xd3dff8e45b7fcdb48f7713095d84dd9c00000000000000000000000000000000` |
| `blsKeyToValidator[pk0]` | `keccak256(pk0 ‖ u256(0x38))` (48 바이트 key, 패딩 없음) | `0x8292702d46cd2a8f2bf69e1697c26f72cd4d34a9f2faaa8a6103c45027637dca` | `pad32(v0)` |
| `validatorToOperator[v0]` | `keccak256(pad32(v0) ‖ u256(0x35))` | `0x9cdd7df4cfbd604045380b9d09961b2818399d3d10fd389d16daf4e44036e0e2` | `pad32(m0)` |
| `operatorToValidator[m0]` | `keccak256(pad32(m0) ‖ u256(0x36))` | `0x7a3fdea8e2b01249181632dbd8bd87108e4c8c03230fcab3ca1b75c084ac0bd8` | `pad32(v0)` |
| `gasTip` | slot `0x39` | `0x…39` | `0x…191a20322000` (27 600 000 000 000) |
| `blsPoP` | slot `0x32` | `0x…32` | `0x…b00001` |
| `members[m0]` | `keccak256(pad32(m0) ‖ u256(5))` | `0x6552c94a3b040b3fd9d247ee0a17428223c0948df2bbdf50b8648526c09cbf95` | `0x…01` (active, joinedAt 0) |
| `versionedMemberList[1]` 길이 | `keccak256(u256(1) ‖ u256(6))` | `0x3e5fec24aa4dc4e5aee2e025e51e1392c72a2500577559fae9665c6d52bd6a31` | `0x…07` |
| `versionedMemberList[1][0]` | `keccak256(previous slot)` | `0x80497882cf9008f7f796a89e5514a7b55bd96eab88ecb66aee4fb0a6fd34811c` | `pad32(m0)` |
| `_memberIndexByVersion[1][m0]` | `keccak256(pad32(m0) ‖ keccak256(u256(1) ‖ u256(8)))` | `0x64bf6b8db42c7b9ab77dbb417d8b6ecce1802b4062189763f959c21e335c551d` | `0x…01` |
| `_quorumByVersion[1]` | `keccak256(u256(1) ‖ u256(9))` | `0x92e85d02570a8092d09a6e3a57665bc3815a2699a4074001bf1ccabf660f5a36` | `0x…02` |
| `quorum` | slot `0x04` | `0x…04` | `0x…02` |
| `proposalExpiry` | slot `0x00` | `0x…00` | `0x…093a80` (604 800) |
| `maxActiveProposalsPerMember` | slot `0x0c` | `0x…0c` | `0x…03` |

Testnet genesis 의 `GovValidator` 계정에는 0 이 아닌 storage slot 이 86 개 있다.

### 9.2 Reader 결과 (testnet genesis)

`validator_list(genesis_state, 0x…1001)` 은 다음 주소를 이 순서로 돌려준다.

```
0x9f06600b2c17108662e3840e76bb27c9468eb73d
0x1aa18ec0b3131171b1b1ddba2dffd81410b30a5a
0xe63b413353e1ba4ac99f2bd1892328e2365ec574
0x20f681210071932dbe6387378adf6f26af029f7d
0x5803c14973690550d6ffc2014b7bffd8005f6021
0x59f6e6add1fbeab316a7b17c9f1966b3655efedb
0x0a8dd92ce7ce53bbf6aed40228f9029bb3f92702
```

`bls_public_key` 는 각 주소에 대해 `blsPublicKeys` 에서 같은 위치에 있는 키를 돌려주고, `gas_tip` 은 27 600 000 000 000 을 돌려준다. Mainnet preset 에서 `validator_list` 는 `[0xaa5faa65e9cc0f74a85b6fdfb5f6991f5c094697]` 을 돌려준다. `GovValidator` 계정이 없는 state 에서 `validator_list` 는 `[]` 를, `bls_public_key` 는 `b""` 를, `gas_tip` 은 0 을 돌려준다.

### 9.3 Contract bytecode 와의 교차 확인 (informative)

다음 절차를 reference commit 에서 실행했고 모두 통과했다. 이 절차는 reader 나 mirror(`B-08` 6절)를 재구현할 때 권장하는 conformance 시험이다.

1. Member 네 명 `op0..op3` 과 validator `v0..v3` 로 `GovValidator` v1 genesis 를 만든다. BLS 키는 validator 키에서 유도한다.
2. 메모리 안의 state 에 code 와 storage 를 설치하고(3절), Anzeon 규칙으로 EVM 을 실행한다.
3. EVM 호출 결과인 `validatorList()`, `validatorToBlsKey(v)`, `gasTip()` 을 slot reader 결과인 `validator_list`, `bls_public_key`, `gas_tip` 과 비교한다. 그 결과 두 쪽의 값이 같고 순서도 같다.
4. `op0` 이 `configureValidator(n, key_n, pop_n)` 을 호출한다. 그 결과 두 쪽 모두 `[v3, v1, v2, n]` 을 돌려준다. `_indexes[v0]`, `validatorToBlsKey[v0]` 의 head slot 과 그 첫 데이터 slot 은 0 이다. 인덱스 4 의 배열 slot 은 0 이고, 길이는 4 다.
5. `op1` 이 `configureValidator(v1, key', pop')` 을 호출해서 `v1` 의 BLS key 만 바꾼다. 그 결과 `bls_public_key(v1)` 은 `key'` 가 되고, slot `keccak256(old_key ‖ u256(0x38))` 은 0 이 되며, validator 순서는 바뀌지 않는다.

---

## 10. 다른 system contract (informative)

노드는 실행 중에 아래 배치를 읽지 않는다. Genesis initializer 가 이 배치로 storage 를 쓰며(`B-02`, 10.1절), 의미는 `B-05` 가 정한다. 이 목록은 inspector 가 storage 를 해석할 수 있게 하고, upgrade 호환성을 확인할 수 있게 하려고 싣는다. 표는 informative 이고, 10.1절은 규범이다.

| Contract | Slot | 변수 |
|---|---|---|
| `GovCouncil` v1 | `0x00`–`0x31` | `GovBase` |
| | `0x32`, `0x33` | `_currentBlacklist` (`AddressSetLib.AddressSet`: `_values` 배열은 `0x32`, `_positions` mapping 은 `0x33`, 1부터 센다) |
| | `0x34`, `0x35` | `_currentAuthorizedAccounts` (같은 구조) |
| | `0x36` | `__accountManager` (genesis 값 `0x…b00003`) |
| `GovMasterMinter` v1 | `0x32` | `fiatToken` |
| | `0x33` | `maxMinterAllowance` |
| | `0x34` | `isMinter` (`mapping(address => bool)`) |
| | `0x35` | `__minterList` (`address[]`) |
| | `0x36` | `minterIndex` |
| | `0x37` | `emergencyPaused` |
| `GovMinter` v1 | `0x32`–`0x3c` | `fiatToken`, `usedProofHashes`, `depositIdToProposalId`, `executedDepositIds`, `withdrawalIdToProposalId`, `executedWithdrawalIds`, `burnProposals`, `reservedMintAmount`, `mintProposalAmounts`, `burnBalance`, `emergencyPaused` |
| `GovMinter` v2 | `0x32`–`0x3c` | v1 과 같다 |
| | `0x3d` | `refundableBalance` (v2 에서 새로 추가) |
| `NativeCoinAdapter` v1 | `0x00` | `masterMinter` |
| | `0x01`, `0x02` | `_minters`, `_minterAllowed` |
| | `0x03` | `_DEPRECATED_CACHED_DOMAIN_SEPARATOR` (`bytes32`, 쓰지도 읽지도 않는다. separator 는 호출마다 계산한다, 13.2절) |
| | `0x04` | `__authorizationStates` (`mapping(address => mapping(bytes32 => bool))`, 쓰였거나 취소된 EIP-3009 nonce, `[authorizer][nonce]`) |
| | `0x05` | `__permitNonces` (`mapping(address => uint256)`, EIP-2612 nonce) |
| | `0x06`, `0x07` | `_coinManager` (`0x…b00002`), `_accountManager` (`0x…b00003`) |
| | `0x08`, `0x09`, `0x0b` | `name`, `symbol`, `currency` (5.4절 인코딩의 `string`) |
| | `0x0a` | `decimals` (`uint8`, 하위 바이트) |
| | `0x0c` | `__allowed` (`mapping(address => mapping(address => uint256))`, `[owner][spender]`) |
| | `0x0d` | `_totalSupply` |

Source: systemcontracts/gov_council.go:55-61, systemcontracts/gov_master_minter.go:34-50, systemcontracts/gov_minter.go:36-58, systemcontracts/coin_adapter.go:13-23
Source: systemcontracts/solidity/v1/GovCouncil.sol:60-66, systemcontracts/solidity/libraries/AddressSetLib.sol:38-48, :120-145
Source: systemcontracts/solidity/v1/GovMinter.sol:100-129, systemcontracts/solidity/v2/GovMinter.sol:102-134
Source: systemcontracts/solidity/abstracts/eip/EIP712Domain.sol:36, systemcontracts/solidity/abstracts/eip/EIP3009.sol:51, systemcontracts/solidity/abstracts/eip/EIP2612.sol:40, systemcontracts/solidity/v1/NativeCoinAdapter.sol:79

`NativeCoinAdapter` 의 slot `0x03`–`0x05` 는 `AbstractFiatToken, Mintable, EIP3009, EIP2612` 의 C3 linearisation 을 따른다. 두 EIP contract 가 함께 상속하는 `EIP712Domain` 이 `EIP3009` 보다 앞에 온다. `NativeCoinAdapter.sol:56-69` 의 주석은 `EIP3009` 를 `0x03`, `EIP2612` 를 `0x04`, `EIP712Domain` 을 `0x05` 에 두는데, 이 주석은 틀렸다. 표는 참조 EVM 에서 artifact 를 실행해 확인했다. `mapping_slot(0x05, owner)` 에 쓴 값은 `nonces(owner)` 가 돌려주었고, `0x04` 의 `[owner][nonce]` 항목에 쓴 값은 `authorizationState(owner, nonce)` 가, `0x0c` 의 `[owner][spender]` 항목에 쓴 값은 `allowance(owner, spender)` 가 돌려주었다. slot `0x03` 에 값을 써도 `DOMAIN_SEPARATOR()` 는 바뀌지 않았다.

`GovCouncil` 의 집합은 OpenZeppelin 이 아니라 로컬 라이브러리 `AddressSetLib` 를 쓴다. 그러나 `AddressSetLib` 의 배치는 `EnumerableSet` 과 같다. `AddressSetLib` 도 배열과 1부터 세는 위치 mapping 을 쓰고, 원소를 제거할 때 같은 swap-and-pop 방식을 쓴다. Genesis initializer 는 이 집합을 주소의 바이트 오름차순으로 쓴다.

### 10.1 Minting contract 의 genesis initializer

아래 initializer 는 `Params` 가 있는 `v1` 항목에서만 실행된다 (SNET-SYS-010). `GovMinter` 와 `GovMasterMinter` 의 `GovBase` 부분은 `B-05` SNET-GOV-160 이다. 아래 initializer 는 parsing 도우미 둘과 참조 구현의 정수 parser 둘을 쓴다.

```python
def split_and_trim(s: str) -> list[str]:
    # "," 로 나누고, 각 항목의 공백을 지우고, 빈 항목은 버린다. 중복은 남긴다
    return [x.strip() for x in s.split(",") if x.strip() != ""]

def hex_to_address(s: str) -> Address:
    # go-ethereum common.HexToAddress: 앞의 "0x"/"0X" 를 떼고, 길이가 홀수이면 앞에 "0" 을 붙이고,
    # 첫 잘못된 쌍 전까지 hex 쌍을 decode 한다 (그때까지 decode 한 바이트는 남기고 나머지는
    # 무시하며 오류는 없다). 마지막 20 바이트를 남기고 앞을 0 으로 채운다.
    ...

# set_string(s): Go big.Int.SetString(s, 10): 앞에 "+" 나 "-" 하나를 둘 수 있고 그 뒤는 10진 숫자다.
#     그 밖의 입력은 오류다. slot 값은 u256(abs(v) mod 2**256) 이다 (common.BigToHash).
# parse_u8(s): Go strconv.ParseUint(s, 10, 8): 10진 숫자만, 0..255. 그 밖의 입력은 오류다.
```

[SNET-SYS-091] `NativeCoinAdapter` initializer 는 반드시 정확히 아래의 storage 쓰기를 만들어야 하고, 나열한 오류가 나면 transition 계산은 반드시 실패해야 한다. 그러면 genesis 생성이나 upgrade block 의 finalization 이 실패한다.
1. `masterMinter` 가 없거나 비어 있으면 오류다. slot `0x00` 에 `pad32(hex_to_address(masterMinter))` 를 쓴다. 결과가 zero 여도 거부하지 않는다.
2. `minters` 가 있고 비어 있지 않으면 `M = split_and_trim(minters)` 다. `minterAllowed` 가 있고 비어 있지 않으면 `A = split_and_trim(minterAllowed)` 이다. 이때 `len(A) != len(M)` 이면 오류이고, `set_string` 이 거부하는 항목이 있으면 오류다. 각 `i` 에 대해 순서대로 `mapping_slot(0x01, hex_to_address(M[i]))` 에 `u256(1)` 을 쓰고, `A` 가 주어졌으면 `mapping_slot(0x02, hex_to_address(M[i]))` 에 `u256(abs(A[i]) mod 2^256)` 을 쓴다. 중복된 minter 는 다시 쓰이므로 뒤의 allowance 가 남는다.
3. slot `0x06` 에 `pad32(0x…b00002)` 를, slot `0x07` 에 `pad32(0x…b00003)` 을 쓴다.
4. `name` 이 없거나 비어 있으면 오류이고, `symbol` 이 없거나 비어 있으면 오류다. `name` 의 UTF-8 바이트를 5.4절 인코딩(SNET-SYS-033)으로 slot `0x08` 에, `symbol` 을 slot `0x09` 에 쓴다.
5. `decimals` 가 없거나 비어 있거나 `parse_u8` 이 거부하면 오류다. slot `0x0a` 에 `u256(decimals)` 를 쓴다.
6. `currency` 가 없거나 비어 있으면 오류다. `currency` 를 5.4절 인코딩으로 slot `0x0b` 에 쓴다.
7. genesis allocation 이 있을 때만 (upgrade 경로에서는 쓰지 않는다) slot `0x0d` 를 `B-02` SNET-GEN-011 대로 쓴다.
Source: systemcontracts/coin_adapter.go:34-169 (initializeCoinAdapter)
Source: systemcontracts/gov_base.go:68-77 (splitAndTrim), systemcontracts/stateutil.go:189-237 (EncodeBytesToSlots)
Observable: state

[SNET-SYS-092] `GovMinter` initializer 는 반드시 `GovBase` 쓰기(`B-05` SNET-GOV-160)를 만든 뒤, key `fiatToken` 이 있으면 (비어 있어도) slot `0x32` 에 `pad32(hex_to_address(fiatToken))` 을 써야 한다. 결과가 zero 이면 오류다. 그 밖의 slot 은 써서는 안 된다.
Source: systemcontracts/gov_minter.go:87-111 (initializeMinter)
Observable: state

[SNET-SYS-093] `GovMasterMinter` initializer 는 반드시 `GovBase` 쓰기를 만든 뒤 다음을 해야 한다.
1. key `fiatToken` 이 있으면 slot `0x32` 에 `pad32(hex_to_address(fiatToken))` 을 쓴다. 결과가 zero 이면 오류다.
2. key `minters` 가 있으면 `M = split_and_trim(minters)` 이다. `M` 이 비어 있으면 오류이고, `hex_to_address(M[i])` 가 zero 이면 오류다. slot `0x35` 에 `u256(len(M))` 을 쓰고, 각 `i` 에 대해 순서대로 `keccak256(u256(0x35)) + i` 에 `pad32(M[i])` 를, `mapping_slot(0x34, M[i])` 에 `u256(1)` 을, `mapping_slot(0x36, M[i])` 에 `u256(i + 1)` 을 쓴다. 중복은 없애지 않는다. 그래서 배열에는 모든 항목이 남고, 중복된 주소의 index 는 마지막으로 나온 위치에 1 을 더한 값이다.
3. slot `0x33` 에 `u256(v mod 2^256)` 을 쓴다. key `maxMinterAllowance` 가 있으면 `v = set_string(maxMinterAllowance)` 이고 (parse 오류나 `v ≤ 0` 은 오류다), 없으면 `10^28` 이다.
Source: systemcontracts/gov_master_minter.go:59-159 (initializeMasterMinter)
Observable: state

구현 노트 (informative). 참조 initializer 로 실행해 확인했다. testnet parameter 로 만든 `NativeCoinAdapter` 의 slot `0x08` 은 `0x574b5243…08` (`"WKRC"`, 짧은 형태), `0x0a` 는 18, `0x0b` 는 `0x4b5257…06` (`"KRW"`)이다. `minterAllowed = "-5"` 는 5 로 쓰인다. `masterMinter = "not-an-address"` 는 오류 없이 zero address 로 쓰인다. `GovMasterMinter` 에 `maxMinterAllowance = 2^256 + 1` 을 주면 slot `0x33` 에 1 이 쓰인다. `minters` 에 같은 주소를 두 번 주면 길이 2, 두 원소, index 2 가 쓰인다. 어느 initializer 도 세 contract 가 서로를 가리키는지(`masterMinter`, `fiatToken`), minter 목록이 서로 맞는지 검사하지 않는다. 프리셋은 일관된다 (`B-01` §11).

---

## 11. 계정 `Extra` 비트

### 11.1 인코딩

State trie 의 모든 계정은 Ethereum 필드 외에 64 비트 부호 없는 필드 `Extra` 를 가진다. `Extra` 는 계정 RLP 목록의 다섯 번째 원소이며 선택적이다. 값이 0 이면 인코딩에서 빠진다(go-ethereum `rlp:"optional"` 규칙, `A-03`).

| 비트 (LSB 부터) | Mask | 이름 | 의미 |
|---|---|---|---|
| 63 | `0x8000000000000000` | blacklisted | 이 계정은 송금, 수신, 수수료 지불을 할 수 없다(`B-07`). Parent state 를 쓸 수 있을 때, 이 계정이 `Coinbase` 인 block 은 거부된다(`B-08` SNET-SRC-020). |
| 62 | `0x4000000000000000` | authorized | 이 계정은 header gas tip 최솟값을 적용받지 않는다(`B-07`) |
| 0–61 | — | reserved | 반드시 0 이어야 한다 (MUST) |

[SNET-SYS-070] 계정 인코딩은 반드시 `rlp([nonce, balance, storageRoot, codeHash, extra])` 여야 하며, `extra` 가 0 이면 빠져야 한다.
Source: core/types/state_account.go:28-37 (StateAccount.Extra `rlp:"optional"`)
Observable: state

[SNET-SYS-071] 노드는 반드시 `Extra` 의 bit 63 이 켜져 있을 때, 그리고 그때에만 계정을 blacklisted 로 다뤄야 한다. 노드는 반드시 bit 62 가 켜져 있을 때, 그리고 그때에만 계정을 authorized 로 다뤄야 한다. 노드는 반드시 존재하지 않는 계정을 둘 다 아닌 것으로 다뤄야 한다.
Source: core/types/state_account_extra.go:30-45, :71-94
Source: core/state/statedb.go:310-327 (IsBlacklisted, IsAuthorized on a missing account return false)
Observable: state

[SNET-SYS-072] 기본값이 아닌 필드가 `Extra` 뿐인 계정을 빈 계정으로 취급해서는 안 된다. 그래서 EIP-161 삭제는 그런 계정을 지워서는 안 된다.
Source: core/state/state_object.go:94-96 (empty includes `Extra == 0`)
Observable: state

### 11.2 비트를 쓰는 곳

비트는 정확히 네 곳에서 쓰인다.

1. Genesis allocation: allocation 항목의 `extra` 필드가 비트를 정한다. 이 필드는 SNET-SYS-013 의 제약을 받는다.
2. `GovCouncil` genesis initializer: `blacklist` / `authorizedAccounts` parameter 와, 이미 비트가 켜진 allocation 항목을 합친 집합을 만든다. Initializer 는 이 합집합을 allocation 과 `GovCouncil` 집합 양쪽에 쓴다(`B-02`). Allocation 에는 `Extra |= mask` 로 비트를 켜며, 항목이 없는 계정에는 balance 0 인 항목을 새로 만든다. 그 주소가 genesis 시스템 컨트랙트 주소이기도 하면 그 뒤에 `inject_contracts` 가 allocation 항목을 새 항목으로 바꾸므로(`B-02` SNET-GEN-012, SNET-GEN-014; SNET-SYS-011), 그 주소는 집합에는 들어 있지만 `Extra = 0` 이다.
3. 실행 중에는 `AccountManager` native manager(12.1절)가 비트를 쓴다. 이 manager 는 `GovCouncil` contract 만 호출할 수 있다. `GovCouncil` 은 blacklist proposal 이나 authorized-account proposal 이 실행될 때 이 manager 를 호출하며(`B-05`), 호출이 성공한 경우에만 자기 집합을 갱신한다.
4. 실행 중에 그 계정 주소에 contract 가 생성될 때 비트가 지워진다. `CREATE`, `CREATE2`, contract 생성 트랜잭션은 새 주소에 nonce 가 0 이고 code 가 없는 계정이 있으면 충돌로 보지 않는다. 이때 `StateDB.CreateAccount` 가 계정 객체를 새로 만들고 balance 만 옮기므로 두 비트가 모두 0 이 된다. 생성 경로와 생성 트랜잭션 검사는 새 주소의 blacklist 비트를 보지 않으며, 생성자(와 트랜잭션 발신자)만 검사한다(`B-07` SNET-TX-041, SNET-TX-044).

---

## 12. Native manager 와 BLS PoP precompile

### 12.1 `AccountManager` (`0x…b00003`)

이 native manager 는 입력의 첫 4 바이트(Solidity function selector)로 method 를 고른다. 입력은 정확히 selector 에 parameter 하나당 32 바이트를 더한 길이여야 한다. 주소 parameter 는 각 word 의 낮은 20 바이트에서 읽는다.

| Method | Selector preimage | Param 수 | Caller 제한 | Gas | 효과 |
|---|---|---|---|---|---|
| blacklist | `blacklist(address)` | 1 | genesis `GovCouncil` 에서 온 `CALL` 만 허용 | 4500 (계정이 없으면 +25000) | bit 63 을 켠다 |
| unBlacklist | `unBlacklist(address)` | 1 | 위와 같다 | 4500 | bit 63 을 끈다 |
| isBlacklisted | `isBlacklisted(address)` | 1 | 제한 없음 | 0 | 32 바이트 word 를 돌려준다. 비트가 켜져 있으면 마지막 바이트가 1 이다 |
| authorize | `authorize(address)` | 1 | blacklist 와 같다 | 4500 (+25000) | bit 62 를 켠다 |
| unAuthorize | `unAuthorize(address)` | 1 | 위와 같다 | 4500 | bit 62 를 끈다 |
| isAuthorized | `isAuthorized(address)` | 1 | 제한 없음 | 0 | 32 바이트 word 를 돌려준다. 비트가 켜져 있으면 마지막 바이트가 1 이다 |

[SNET-SYS-080] Account manager 는 호출 opcode 가 `CALL` 이고 직접 caller 가 `anzeon.systemContracts` 의 `GovCouncil` 주소일 때가 아니면, state 를 바꾸는 method 를 반드시 거부해야 한다. 또한 알 수 없는 selector 와 길이가 틀린 입력도 반드시 거부해야 한다. 거부는 revert 가 아니라 호출 오류다. 다른 실패한 호출과 마찬가지로 그 호출의 state 변경은 되돌려지고, 호출에 넘긴 gas 는 모두 소모된다.
Source: core/vm/native_manager.go:137-155 (runNativeManager), core/vm/native_manager.go:470-489 (canRunAccountManager, validateCaller, validateCallContext)
Source: core/vm/native_manager.go:81-92 (selectors)
Source: core/vm/evm.go:262-263, :283-288 (native manager dispatch; non-revert error consumes remaining gas)
Observable: state

Native manager 의 gas 와 실패 의미 전체는 실행 규칙(`B-07`)에 속한다. 여기서 요약하는 이유는 native manager 가 `Extra` 비트를 쓰는 유일한 writer 이기 때문이다.

### 12.2 `NativeCoinManager` (`0x…b00002`)

Coin manager(`mint(address,uint256)`, `burn(address,uint256)`, `transfer(address,address,uint256)`)는 balance 를 직접 바꾼다. 이 manager 는 `anzeon.systemContracts` 의 `NativeCoinAdapter` 주소에서 `CALL` 로 호출될 때만 동작한다. 이 manager 는 노드가 읽는 system contract storage 를 건드리지 않는다.
Source: core/vm/native_manager.go:83-85, :196-284, :463-468

### 12.3 BLS proof-of-possession precompile (`0x…b00001`)

`GovValidator.configureValidator` 는 이 precompile 을 `staticcall(blsKey ‖ blsSig)` 로 호출하며, 호출이 성공하고 `true` 를 돌려줘야 진행한다.

| 속성 | 값 |
|---|---|
| 입력 | 정확히 144 바이트: 48 바이트 압축 G1 public key ‖ 96 바이트 압축 G2 signature |
| Gas | precompile 이 출력을 돌려주면 입력과 무관하게 45 000 이다. 실패하면 호출은 전달받은 gas 를 모두 소비한다(오류가 revert 가 아니기 때문이다, `core/vm/evm.go:283-287`) |
| 출력 | 32 바이트 word. `bls_verify(pk, msg = pk_bytes, sig)` 가 성립하면(`A-02`) 마지막 바이트가 1 이고, 아니면 모두 0 이다 |
| 실패 | 길이가 144 가 아니거나, key 의 압축을 풀 수 없거나 key 가 G1 부분군 밖이거나 무한원점이거나, signature 의 압축을 풀 수 없거나 signature 가 G2 부분군 밖이면 오류가 나고 호출이 실패한다. G2 무한원점 signature 는 디코딩되고 검증 결과가 0 이다(출력이 모두 0 이며 실패가 아니다) |

[SNET-SYS-090] BLS PoP precompile 은 반드시 위 표대로 동작해야 한다. 여기서 서명 대상 메시지는 48 바이트 public key 자체다.
Source: core/vm/contracts.go:1189-1222 (blsPoP), core/vm/evm.go:283-287 (all gas consumed on a non-revert error)
Source: crypto/bls/blst/public_key.go:48-52, crypto/bls/blst/signature.go:56-66
Source: params/protocol_params.go:208-209 (address, gas)
Source: systemcontracts/solidity/v1/GovValidator.sol:157-172 (`_checkBlsKey`)
Observable: state

`GovValidator` 가 쓰는 verifier 주소는 contract 에 하드코딩되어 있지 않다. 그 주소는 genesis 에서 쓴 slot `0x32`(`blsPoP`)의 값이다(8.3절).

---

## 13. System contract 의 보안 고려 사항

아래 동작은 등록된 bytecode (SNET-SYS-005)에서 나온다. 그 bytecode 를 설치하고 실행하는 노드는 이 동작을 그대로 재현한다. 이 동작은 Ethereum 과 다르고 native coin 사용자에게 영향을 주기 때문에 여기에 적는다. 이 절은 block 유효성을 바꾸지 않는다.

### 13.1 서명으로 옮겨지는 native coin

native coin 은 `NativeCoinAdapter` 주소의 ERC-20 token 으로 보인다. 그 token 의 모든 transfer 경로는 coin manager 를 거쳐 native 잔액 자체를 옮긴다 (12.2절). 그래서 Ethereum 의 ETH 와 달리, native coin 은 소유자가 transaction 을 보내지 않아도 옮겨질 수 있다. `approve` 나 EIP-2612 `permit` 뒤의 `transferFrom`, 그리고 EIP-3009 `transferWithAuthorization` 과 `receiveWithAuthorization` 이 그 경로다. `permit` 이나 authorization 에는 만료가 없을 수 있다 (`deadline = 2^256 − 1` 이거나 먼 `validBefore`). 새어 나간 서명은 그 nonce 가 쓰이거나 취소될 때까지 자금을 옮길 수 있다.

서명 검사는 `SignatureChecker.isValidSignatureNow(signer, digest, sig)` 가 한다. 이 함수는 `EXTCODESIZE(signer) > 0` 이면 `signer` 의 ERC-1271 `isValidSignature(digest, sig)` 만 부르고 magic value `0x1626ba7e` 를 받아들인다. 그렇지 않으면 `v ∈ {27, 28}`, `s ≤ n/2` 이고 `signer` 로 복원되는 65 바이트 ECDSA 서명을 받아들인다. EIP-7702 로 위임한 계정은 code (23 바이트 designator, `B-07` SNET-TX-094)를 가지므로 ERC-1271 경로만 적용된다.

### 13.2 Token 서명의 replay 범위

[SNET-SYS-096] `NativeCoinAdapter` v1 의 EIP-712 domain separator 는 반드시 호출할 때마다 `keccak256(abi.encode(0x8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f, keccak256(name), keccak256("1"), CHAINID, adapter_address))` 로 계산되어야 한다. 첫 word 는 `keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")` 이고, `name` 은 slot `0x08` 의 문자열이다. slot `0x03` 은 쓰이지 않는다.
Source: systemcontracts/solidity/v1/NativeCoinAdapter.sol:486-507, systemcontracts/solidity/libraries/EIP712.sol:39-50
Source: systemcontracts/solidity/abstracts/eip/EIP712Domain.sol:36-52
Observable: state

구현 노트 (informative). artifact 를 실행해 확인했다. testnet state 에서 `DOMAIN_SEPARATOR()` 는 `0xcdd475bf7b9e816f1c14ddff1c9ca33596570bcd2644311f8ebd90c41ffc9028` 이었고 위 식과 같았다. chain id 만 8282 로 바꾸면 `0x9ce49ea6…9c98` 이었다. slot `0x03` 에 값을 써도 결과는 바뀌지 않았다.

그래서 서명은 chain id, adapter 주소, `name`, 버전 `"1"` 이 같은 곳이면 어디서나 유효하다. mainnet 과 testnet 프리셋은 chain id 가 다르지만 adapter 주소(`0x…1000`)와 `name`(`WKRC`)은 같다.

[SNET-SYS-097] 배포는 `NativeCoinAdapter` 의 주소와 `name` 이 같은 체인에 다른 StableNet 체인(mainnet 과 testnet 프리셋 포함)의 chain id 를 다시 써서는 안 된다. 그렇게 하면 한 체인을 위해 만든 EIP-2612 와 EIP-3009 서명이 다른 체인에서도 유효하다. chain id 를 유지한 채 체인이 갈라진 경우도 마찬가지다.
Source: params/config.go:45, params/config.go:156 (chain ids), params/config_wbft.go:31-45 (default addresses)

### 13.3 Blacklist 와 미리 서명된 authorisation

adapter 는 실행할 때에만 blacklist 를 검사한다 (`B-05` SNET-GOV-176). 그래서 계정이 blacklist 되기 전에 준 allowance 와 만든 EIP-2612, EIP-3009 서명은 유효한 채로 남고, 계정이 blacklist 에서 빠지면 다시 쓸 수 있다. blacklist 에서 계정을 빼는 운영자는 그런 authorisation 이 있다고 가정해야 한다. blacklist 된 authorizer 도 자기 EIP-3009 authorization 을 취소할 수 있지만, 취소는 자금을 옮기지 않는다.
