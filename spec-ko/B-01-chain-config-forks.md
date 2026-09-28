# B-01 Chain 설정과 fork

- Status: draft
- Area code: `CFG`
- Reference implementation: go-stablenet `740526d03`

이 장은 설정 객체를 정한다. Part B 의 다른 모든 장과 Part A 의 합의 설정은 이 설정 객체에서 나온다. go-stablenet 은 chain 마다 `params.ChainConfig` 하나를 둔다. 이 객체에는 go-ethereum 에서 물려받은 Ethereum fork 일정, StableNet fork 두 개(`ApplepieBlock`, `BohoBlock`), Anzeon 섹션(WBFT 파라미터, 초기 validator, system contract), fork 별 overlay(`Boho`), WBFT 파라미터 transition 이 들어 있다. 두 노드가 아래에서 *consensus-critical* 로 표시한 필드 가운데 하나라도 다른 값을 읽으면, 두 노드가 모두 Part A 와 Part B 를 올바르게 구현했더라도 어느 height 에서 block 유효성 판단이 갈린다. 그래서 이 장은 다음 네 가지를 정한다.

1. 각 필드의 뜻
2. 그 필드에서 height 에 따라 달라지는 값을 끌어내는 함수
3. go-stablenet 이 시작할 때 하는 검사
4. 내장된 두 네트워크 preset 의 정확한 값

---

## 1. 표기

- `cfg` 는 읽어 들인 `ChainConfig` 이다. `n` 은 block 번호이고, `t` 는 block timestamp(초)이다.
- `forked(s, n)` 은 §3.1 의 block 번호 fork 판정 함수이고, `forked_time(s, t)` 는 timestamp fork 판정 함수이다.
- `wbft_cfg` 는 §6 에서 끌어낸 합의 `Config` 이다. Part A 는 height 별로 본 설정을 `config_at(n)` 이라고 부른다(`A-01` 참조).
- `system_contracts_at(n)` 과 `upgrades_at(n)` 은 §7 에서 정의한다. `genesis_system_contracts` 는 `cfg.Anzeon.SystemContracts`, 곧 고치지 않은 genesis 섹션이다.

---

## 2. StableNet 이 쓰는 `ChainConfig` 필드

JSON 이름은 genesis 파일과 저장된 chain 설정에서 쓰는 이름이다. 표에 없는 필드(`DAOForkBlock`, `DAOForkSupport`, `TerminalTotalDifficulty`, `TerminalTotalDifficultyPassed`, `Ethash`, `Clique`)는 go-ethereum 에서 물려받았지만 StableNet 설정에는 없으므로 이 명세의 범위 밖이다.

| 필드 | JSON | 타입 | Consensus-critical | StableNet 에서의 뜻 |
|---|---|---|---|---|
| `ChainID` | `chainId` | integer | 예 | replay 방지(EIP-155), transaction signer, randao 데이터(`A-02`) |
| `HomesteadBlock` .. `LondonBlock` | `homesteadBlock`, `eip150Block`, `eip155Block`, `eip158Block`, `byzantiumBlock`, `constantinopleBlock`, `petersburgBlock`, `istanbulBlock`, `muirGlacierBlock`, `berlinBlock`, `londonBlock` | block 번호 또는 없음 | 예 | Ethereum fork, §4.1 |
| `ArrowGlacierBlock`, `GrayGlacierBlock` | `arrowGlacierBlock`, `grayGlacierBlock` | block 번호 또는 없음 | block 유효성에는 효과 없음 | difficulty bomb 을 늦추는 fork 이다. WBFT 에서는 difficulty 가 고정이므로 효과가 없다(`A-08`). 0 이 아닌 값은 fork ID(EIP-2124)에 들어가므로, 값이 다른 peer 는 eth handshake 에서 거절될 수 있다 |
| `MergeNetsplitBlock` | `mergeNetsplitBlock` | block 번호 또는 없음 | block 유효성에는 효과 없음 | StableNet 은 쓰지 않는다. 0 이 아닌 값은 fork ID(EIP-2124)에 들어가므로, 값이 다른 peer 는 eth handshake 에서 거절될 수 있다 |
| `ApplepieBlock` | `applepieBlock` | block 번호 또는 없음 | 예 | fee delegation, §4.3. 0 이 아닌 값은 fork ID(EIP-2124, §11.1)에 들어간다 |
| `BohoBlock` | `bohoBlock` | block 번호 또는 없음 | 예 | Boho fork, §4.4. 0 이 아닌 값은 fork ID(EIP-2124, §11.1)에 들어간다 |
| `ShanghaiTime`, `CancunTime`, `PragueTime`, `VerkleTime` | `shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime` | timestamp 또는 없음 | 예 | WBFT 에서 지원하지 않는다, §9 |
| `Anzeon` | `anzeon` | `AnzeonConfig` 또는 없음 | 예 | 이 필드가 있으면 chain 이 Anzeon chain 이 된다, §5 |
| `Boho` | `boho` | `AnzeonConfig` 또는 없음 | 예 | `BohoBlock` 에서 활성화되는 system contract overlay, §5.4 |
| `Transitions` | `transitions` | `Transition` 목록 | 예 (§6 참조) | height 에 따른 WBFT 파라미터 변경 |

[SNET-CFG-001] 적합한 노드는 반드시 Part A 와 Part B 의 height 에 따라 달라지는 모든 규칙을 위 필드를 가진 `ChainConfig` 하나에서 끌어내야 하며, 같은 네트워크의 두 노드는 반드시 consensus-critical 로 표시된 모든 필드에 같은 값을 읽어야 한다.
Source: params/config.go:785-834 (ChainConfig)
Source: eth/ethconfig/config.go:192-201 (CreateConsensusEngine uses the same ChainConfig)

---

## 3. Fork 판정 함수

### 3.1 Block 번호와 timestamp 판정 함수

```python
def forked(s: Optional[int], n: Optional[int]) -> bool:
    # fork 가 없거나 height 가 없으면 활성이 아니다
    if s is None or n is None:
        return False
    return s <= n

def forked_time(s: Optional[int], t: int) -> bool:
    if s is None:
        return False
    return s <= t
```

[SNET-CFG-002] block 번호로 정하는 모든 fork `X`(Homestead 부터 London 까지, Applepie, Boho)에 대해 `is_X(n)` 은 반드시 `forked(cfg.XBlock, n)` 과 같아야 한다. 예외는 go-ethereum 의 예외 하나뿐이다. `PetersburgBlock` 이 없고 `forked(ConstantinopleBlock, n)` 이 참이면 `is_petersburg(n)` 도 참이다.
Source: params/config.go:1017-1045 (IsPetersburg, IsApplepie, IsBoho)
Source: params/config.go:1364-1369 (isBlockForked)

[SNET-CFG-003] {Shanghai, Cancun, Prague, Verkle} 의 모든 timestamp fork `Y` 에 대해 `is_Y(n, t)` 는 반드시 `is_london(n) and forked_time(cfg.YTime, t)` 와 같아야 한다.
Source: params/config.go:1066-1083 (IsShanghai, IsCancun, IsPrague, IsVerkle)
Source: params/config.go:1390-1395 (isTimestampForked)

### 3.2 Anzeon 활성화

Anzeon 은 block 번호로 예약하지 않는다. 설정에 `anzeon` 섹션이 있으면 Anzeon 이 켜지고, 그때부터 genesis block 을 포함한 모든 block 에서 활성이다.

[SNET-CFG-004] 노드는 `cfg.Anzeon` 이 있을 때, 그리고 그때에만 chain 을 genesis block 을 포함한 모든 height 에서 반드시 Anzeon chain 으로 다뤄야 한다. Anzeon 활성화 block 은 없다.
Source: params/config.go:1085-1087 (AnzeonEnabled)
Source: params/config.go:1407-1411 (isForkAnzeonIncompatible: enable/disable by presence)

> 해설: Anzeon 이 설정 섹션의 존재로 켜지기 때문에 "Anzeon 이전 block" 이라는 것은 없다. 설정에 `anzeon` 섹션이 있으면 그 chain 은 처음부터 끝까지 Anzeon chain 이다.

### 3.3 실행 규칙 플래그

EVM 과 state transition 은 block 마다 한 번 계산한 `Rules` 값을 읽는다.

[SNET-CFG-005] block 별 규칙 플래그는 반드시 다음과 같이 계산해야 한다.
1. `IsAnzeon = (cfg.Anzeon is present)` 이다.
2. `IsApplepie = is_applepie(n)` 이다. 이 값은 Anzeon 으로 거르지 않는다.
3. `IsBoho = IsAnzeon and is_boho(n)` 이다.
4. `IsMerge = is_merge_flag and is_london(n)` 이다.
5. `IsShanghai`, `IsCancun`, `IsPrague`, `IsVerkle` 는 SNET-CFG-003 의 판정 함수에 `IsMerge` 조건을 더한 값이다.
Source: params/config.go:1512-1541 (Rules)

[SNET-CFG-006] WBFT block(header `Difficulty == 1`)에서는 SNET-CFG-005 에 넘기는 `is_merge_flag` 가 반드시 false 여야 한다. 그래서 EVM 안에서 `IsMerge`, `IsShanghai`, `IsCancun`, `IsPrague`, `IsVerkle` 은 timestamp fork 와 상관없이 모두 false 이다. EVM 난수 값(`PREVRANDAO`, opcode `0x44`)은 반드시 `header.MixDigest` 여야 한다.
Source: core/vm/evm.go:162 (Rules called with Difficulty==0 && Random!=nil)
Source: core/evm.go:61-64 (Random = &header.MixDigest when Difficulty is 0 or 1)
Observable: state

구현 노트 (informative). `Rules.IsBoho` 는 Anzeon 으로 거르지만 `Rules.IsApplepie` 는 거르지 않는다. Anzeon chain 에서는 이 차이가 드러나지 않는다. 참조 구현은 가공하지 않은 `ChainConfig.IsBoho` 를 `Rules` 안에서만 부른다.

> 해설: 참조 구현의 EVM 은 `Difficulty == 0 && Random != nil` 일 때만 merge 이후로 본다. WBFT header 는 `Difficulty == 1` 이므로 merge 이후 fork 는 EVM 안에서 모두 꺼진다. 그런데도 참조 구현은 difficulty 가 0 이나 1 이면 `Random = &header.MixDigest` 로 두므로 `PREVRANDAO` 는 `header.MixDigest` 를 돌려준다. WBFT chain 에서 `MixDigest` 는 randao mix 이므로 컨트랙트는 randao mix 를 난수로 쓰게 된다.

---

## 4. 각 fork 가 바꾸는 것

### 4.1 Ethereum fork

[SNET-CFG-007] Homestead 부터 London 까지의 각 Ethereum fork 는 반드시 그 필드가 가리키는 block 에서 go-ethereum 이 그 fork 에 정한 의미대로 효력을 가져야 한다. 예외는 하나이다. 배포되는 코드의 EIP-170 한도는 `24_576` 이 아니라 `MAX_CODE_SIZE = 253_952` 바이트(§10)이다. `CREATE`, `CREATE2`, contract-creation transaction 이 돌려준 코드가 `MAX_CODE_SIZE` 보다 길면 그 생성은 exceptional halt 로 실패하고, 생성하는 frame 의 gas 를 모두 쓴다. 한도 이하의 코드는 평소처럼 바이트당 200 gas 로 배포된다(`B-07` SNET-TX-088). 두 preset(§11)에서는 이 fork 들이 모두 block 0 에 있으므로 모든 StableNet block 은 London block 이다.
Source: params/config.go:44-65, 155-169 (presets)
Source: params/config.go:974-1034 (predicates)
Source: params/protocol_params.go:140 (MaxCodeSize), core/vm/evm.go:528-530 (EIP-170 check)
Observable: state

Anzeon 은 London 규칙 가운데 두 곳을 바꾼다. 하나는 base fee 공식(`B-03` §2.3)이고, 다른 하나는 base fee 가 가는 곳(`B-06` §3)이다. EIP-3529 환급, EIP-3198 `BASEFEE`, EIP-2718/2930/1559 transaction 타입은 go-ethereum 과 같다.

WBFT chain 에서는 London 뒤 Ethereum fork 의 기능이 fork 규칙으로 켜지지 않는다(SNET-CFG-006). 대신 Anzeon 명령어 집합과 precompile 집합이 기능을 하나씩 골라 켠다. Shanghai, Cancun, Prague 규칙을 가진 EVM 을 가져다 쓰는 구현은 나머지를 모두 꺼야 한다.

[SNET-CFG-029] Anzeon chain 에서는 London 뒤의 실행 기능 가운데 정확히 다음 기능만 반드시 켜져 있어야 한다. EIP-3855(`PUSH0`), EIP-3860 의 `CREATE`/`CREATE2` 부분(initcode word gas 와 §10 의 initcode 한도 `MAX_INITCODE_SIZE = 253_952` 바이트이며, `49_152` 가 아니다. `B-07` SNET-TX-088), EIP-1153(`TLOAD`, `TSTORE`), EIP-5656(`MCOPY`), EIP-6780(`SELFDESTRUCT`), EIP-7702(type `0x04`, `B-07` SNET-TX-091 부터 SNET-TX-094 까지), 주소 `0x0a` 의 KZG point-evaluation precompile, 그리고 `BohoBlock` 부터의 `P256VERIFY`(SNET-CFG-010)이다. Shanghai, Cancun, Prague 의 그 밖의 규칙은 어느 height 에서도 적용해서는 안 된다. 특히 EIP-3651(warm coinbase), EIP-3860 의 transaction 부분(intrinsic gas 의 initcode gas 와 contract-creation transaction 의 initcode 크기 검사), EIP-4895(withdrawal), EIP-4844 blob transaction 과 `BLOBHASH`, EIP-7516(`BLOBBASEFEE`), EIP-4788(beacon root), EIP-2537 BLS12-381 precompile, EIP-2935(history storage), EIP-7623(calldata floor, `B-07` SNET-TX-092), EIP-7685(execution request)를 적용해서는 안 된다. `B-07` §9.4 의 precompile 집합과 native manager 에 들지 않는 주소는 일반 계정이다.
Source: params/config.go:1512-1541 (Rules: IsShanghai, IsCancun, IsPrague require IsMerge)
Source: core/vm/jump_table.go:117-140 (newAnzeonInstructionSet), core/vm/interpreter.go:58-67 (table selection)
Source: core/vm/contracts.go:127-157 (Anzeon and Boho precompile sets), params/protocol_params.go:141 (MaxInitCodeSize)
Source: core/state_transition.go:482, 501 (intrinsic gas and initcode check follow IsShanghai), core/state/statedb.go:1388 (EIP-3651)
Observable: state

### 4.2 Anzeon (`cfg.Anzeon` 이 있을 때)

아래 목록은 유효성과 state 에 미치는 효과를 빠짐없이 모은 것이다. 각 행은 그 효과를 규범으로 정하는 장을 가리킨다. 이 목록은 참조 구현의 테스트가 아닌 파일에서 `AnzeonEnabled()` 와 `Rules.IsAnzeon` 을 부르는 모든 곳을 모아 만들었다.

| 효과 | 정하는 곳 | Source |
|---|---|---|
| WBFT 합의 엔진이 Ethash/Clique 를 대신한다 | Part A | eth/ethconfig/config.go:192-201 |
| Genesis: extra 데이터를 `WBFTExtra` 로 다시 만들고, system contract 를 주입하고, `BaseFee = MinBaseFee` 로 둔다 | `B-02` | core/genesis.go:242-263, 535-538 |
| base fee 공식을 threshold 규칙으로 바꾼다 | `B-03` §2.3 | consensus/misc/eip1559/eip1559.go:62-88 |
| base fee 를 diligence 에 따라 validator 에게 다시 나눠 준다 | `B-06` §3 | consensus/wbft/engine/engine.go:941-946 |
| transaction signer 로 `AnzeonSigner` 를 쓴다. 이 signer 는 fee delegation 타입과 set-code 타입을 더 받아들인다 | `B-07` | core/types/transaction_signing.go:51-52, 79-81 |
| authorized 가 아닌 sender 의 실효 tip cap 을 header `GasTip` 으로 강제한다 | `B-07` | core/state_transition.go:159-172 |
| blacklist 된 sender, recipient, fee payer 를 거부하고, zero address 나 precompile 로 가는 value 전송을 거부한다 | `B-07` | core/state_transition.go:505-516, 578-584; core/vm/evm.go:210-219, 480, 614-627 |
| authorized sender 에게 `AuthorizedTxExecuted` log 를 붙인다 | `B-07` | core/state_transition.go:587-599 |
| value 전송 때 네이티브 코인 `Transfer` log 를 낸다. log 주소는 genesis `NativeCoinAdapter` 이다 | `B-07` | core/vm/evm.go:590-609 |
| receipt 의 `EffectiveGasPrice` 는 메시지 gas price 이다 | `B-07` | core/state_processor.go:157-159; core/types/receipt.go:396-400 |
| EVM 명령어 집합 `anzeonInstructionSet`: London + `PREVRANDAO` + `PUSH0` (EIP-3855) + initcode 한도 (EIP-3860) + `TLOAD`/`TSTORE` (EIP-1153) + `MCOPY` (EIP-5656) + EIP-6780 `SELFDESTRUCT` + EIP-7702 | `B-07` | core/vm/jump_table.go:117-140; core/vm/interpreter.go:66-67 |
| precompile: 0x01..0x0a (KZG point evaluation 을 포함한 Cancun 집합) + `0x…B00001` 의 BLS PoP | `B-07` | core/vm/contracts.go:127-139; core/vm/evm.go:40-60 |
| 네이티브 manager 로 `0x…B00002` 의 `NativeCoinManager` 와 `0x…B00003` 의 `AccountManager` 를 둔다 | `B-07`, `B-04` | core/vm/evm.go:62-72; core/vm/native_manager.go:462-475 |
| EIP-7702 코드 위임(code delegation)을 해석한다 | `B-07` | core/vm/evm.go:631-660 |
| 동기화: peer 선택에서 total difficulty 를 보정한다 | `B-09` | eth/sync.go:205-214 |

[SNET-CFG-008] Anzeon chain 에서는 반드시 위 표의 모든 효과를 모든 height 에서 가리킨 장에 정한 대로 적용해야 한다.
Source: see table
Observable: header, state

### 4.3 Applepie (`ApplepieBlock`)

Applepie 는 fee delegation 을 켠다. Applepie 는 header 필드를 바꾸지 않는다.

[SNET-CFG-009] fee payer 가 설정되어 있고 sender 와 다른 transaction 은, 그 transaction 을 담은 block `n` 에서 `is_applepie(n)` 이 false 이면 반드시 `ErrTxTypeNotSupported` ("fee delegation type not supported")로 거부해야 한다. 그런 transaction 을 담은 block 은 무효이다. `ApplepieBlock` 부터 fee delegation 은 `B-07` 을 따른다.
Source: core/state_transition.go:267-271 (buyGas)
Source: core/txpool/validation.go:72-74 (pool-side gate, informative)
Observable: state

### 4.4 Boho (`BohoBlock`)

Boho 는 서로 독립인 효과 두 가지를 가진다. 첫째 효과는 fork 판정 함수에서 나오고, 둘째 효과는 `boho` overlay 섹션에서 나온다.

[SNET-CFG-010] `Rules.IsBoho` 가 참인 block, 곧 block `BohoBlock` 과 그 뒤의 모든 block 에서 precompile 집합은 반드시 Anzeon 집합에 주소 `0x0000000000000000000000000000000000000100` 의 `P256VERIFY` 를 더한 집합이어야 한다. `P256VERIFY` 는 처음의 EIP-7212 가 아니라 EIP-7951 을 따른다. 그래서 호출마다 gas 는 `3_450` 이 아니라 `P256_VERIFY_GAS = 6_900`(§10)이고, 입력이 정확히 160 바이트가 아니면 오류 없이 빈 출력을 돌려준다. 전체 동작은 `B-07` SNET-TX-089 에 있다.
Source: core/vm/contracts.go:141-155 (PrecompiledContractsBoho), 1227-1251 (p256Verify)
Source: core/vm/evm.go:40-45 (precompile selection)
Source: params/protocol_params.go:171 (P256VerifyGas)
Observable: state

[SNET-CFG-011] `BohoBlock`, `Boho`, `Boho.SystemContracts` 가 모두 있으면 `Boho.SystemContracts` 에 나열된 system contract 는 반드시 §7 과 `B-06` §2 에 정한 대로 block `BohoBlock` 에서 upgrade 되어야 한다. `BohoBlock == 0` 이면 이 upgrade 는 `B-02` §4 를 통해 genesis 에서 적용된다. 두 preset 에서 이 upgrade 는 `GovMinter` (`0x…1003`)의 코드를 버전 `v2` 로 바꾸고 storage 는 바꾸지 않는다.
Source: params/config.go:1108-1126 (CollectUpgrades)
Source: params/config.go:144-151, 264-271 (preset overlays)
Observable: state

구현 노트 (informative). upgrade 는 block `BohoBlock` 의 transaction 을 실행한 뒤에 적용된다(`B-06` §2). 그래서 block `BohoBlock` 안의 transaction 은 이미 `P256VERIFY` 를 쓸 수 있지만, `GovMinter` 는 아직 v1 코드로 실행한다. v2 코드는 block `BohoBlock + 1` 부터 효력을 가진다.

노드는 `Boho` 에서 `SystemContracts` 부분만 읽는다. 그래서 `Boho.WBFT` 와 `Boho.Init` 은 효과가 없다.

> 해설: "fork block 부터 새 규칙" 이라는 직관을 따라 코드 교체를 transaction 실행 전에 하면, `BohoBlock` 에서 `GovMinter` 를 부르는 transaction 이 있을 때 state root 가 달라진다. testnet 에서는 block `14_408_500` 이 이 경계이다. 또한 `boho` overlay 로 합의 파라미터를 바꿀 수 있다고 오해하면 안 된다. overlay 에서 읽는 것은 `systemContracts` 뿐이다.

---

## 5. `AnzeonConfig`

### 5.1 구조

| 필드 | JSON | 타입 | 뜻 |
|---|---|---|---|
| `WBFT` | `wbft` | `WBFTConfig` | 기본 합의 파라미터 (§6) |
| `Init` | `init` | `WBFTInit` | 초기 validator: `validators`(주소 목록, 순서에 의미가 있다)와 `blsPublicKeys`(`0x` hex 문자열 목록, 같은 순서) |
| `SystemContracts` | `systemContracts` | `SystemContracts` | genesis system contract (§5.2) |

`WBFTConfig` 의 필드는 다음 여섯 개이며, JSON key 는 모두 lower camel case 이다: `requestTimeoutSeconds` (uint64), `blockPeriodSeconds` (uint64), `epochLength` (uint64), `allowedFutureBlockTime` (uint64, 선택), `proposerPolicy` (uint64 또는 null), `maxRequestTimeoutSeconds` (uint64 또는 null).

Source: params/config_wbft.go:50-59, 186-193

[SNET-CFG-030] WBFT 설정(`anzeon.wbft` 와 `transitions` 의 각 항목)은 위 여섯 필드만으로 이루어진다. transition 에는 `block` 이 더 있다. 노드는 그 밖의 모든 key 를 반드시 오류 없이 무시해야 한다. Quorum 의 `emptyBlockPeriodSeconds`, `ceil2Nby3Block`, `2FPlus1Enabled`, `validatorSelectionMode` 와 최상위의 `qbft`, `ibft`, `istanbul` 섹션도 무시한다. 이 key 들 가운데 어느 것도 quorum, block 주기, validator 집합을 바꾸지 않는다. 빈 block 주기도 없으므로 block 은 transaction 이 있든 없든 view 마다 만들어진다(`A-06`). 참조 구현은 chain 설정을 Go `encoding/json` 으로 읽는다. 그래서 key 이름은 대소문자를 가리지 않고 맞춰지며(`blockperiodseconds` 도 `blockPeriodSeconds` 를 정한다), 같은 key 가 두 번 나오면 마지막 값이 이긴다. 노드는 `ChainConfig` 의 key 를 반드시 같은 방식으로 읽어야 한다.
Source: params/config_wbft.go:186-198 (WBFTConfig, Transition)
Source: consensus/wbft/config.go:105-114 (Config has no empty-block field)
Source: eth/ethconfig/config.go:213-262 (SetConfigFromChainConfig copies only these fields)
Observable: header

구현 노트 (informative). 참조 코드로 실행해 확인했다. `anzeon.wbft` 안에 `emptyBlockPeriodSeconds`, `ceil2Nby3Block`, `2FPlus1Enabled`, `validatorselectionmode` 를 넣고, 최상위에 `qbft` 섹션을, transition 안에 `emptyBlockPeriodSeconds` 를 넣은 chain 설정은 오류 없이 읽혔고 여섯 필드만 남았다. `{"blockperiodseconds": 5, "EPOCHLENGTH": 30, "epochLength": 40}` 은 block 주기 5, epoch 길이 40 이 되었다.

### 5.2 System contract

`SystemContracts` 는 선택 멤버 다섯 개를 가진다: `govValidator`, `nativeCoinAdapter`, `govMinter`, `govMasterMinter`, `govCouncil`. 각 멤버는 `SystemContract {address, version, params}` 이고, `params` 는 문자열에서 문자열로 가는 map 이다. `params` 로 쓰는 storage layout 은 `B-04` 가 정하고, genesis 절차는 `B-02` 가 정한다.

| 멤버 | 기본 주소 | 등록된 버전 |
|---|---|---|
| `nativeCoinAdapter` | `0x0000000000000000000000000000000000001000` | `v1` |
| `govValidator` | `0x0000000000000000000000000000000000001001` | `v1` |
| `govMasterMinter` | `0x0000000000000000000000000000000000001002` | `v1` |
| `govMinter` | `0x0000000000000000000000000000000000001003` | `v1`, `v2` |
| `govCouncil` | `0x0000000000000000000000000000000000001004` | `v1` |

Source: params/config_wbft.go:31-46, 146-164
Source: systemcontracts/contracts.go:25-76 (registered bytecode per version)

### 5.3 Anzeon 섹션의 시작 시 유효성

[SNET-CFG-012] `Anzeon` 섹션이 아래 조건 가운데 하나라도 어기면, 노드는 반드시 genesis block 초기화를 거부해야 하고, 저장된 설정으로 시작하는 것도 반드시 거부해야 한다. 노드는 조건을 다음 순서로 검사한다.
1. `init` 이 있다.
2. `init.blsPublicKeys` 가 비어 있지 않다.
3. `init.validators` 가 비어 있지 않다.
4. `len(validators) == len(blsPublicKeys)` 이다.
5. `systemContracts` 가 있다.
6. `govValidator`, `nativeCoinAdapter`, `govMasterMinter`, `govMinter`, `govCouncil` 이 모두 있다.
7. 있는 멤버마다 `version` 이 그 컨트랙트 타입에 등록된 버전이다(§5.2 표).
8. `wbft` 가 있다.
9. `requestTimeoutSeconds > 0` 이다.
10. `blockPeriodSeconds > 0` 이다.
11. `epochLength >= 2` 이다.
Source: params/config_wbft.go:76-130 (CheckValidity)
Source: systemcontracts/systemcontracts.go:32-60 (checkSystemContractVersions)
Source: core/genesis.go:231-238 (validateAnzeonGenesisConfig), core/blockchain.go:286-290 (on start)

이 검사는 `Boho`, `Transitions`, `params` 내용, BLS key 형식, Ethereum fork 필드를 보지 않는다. 노드는 `params` 내용을 나중에 genesis 를 만드는 중에 따로 검증한다(`B-02`).

> 해설: 검사하지 않는 항목의 오류는 시작할 때가 아니라 나중에 드러난다. 예를 들어 `boho` 의 버전 오타는 `BohoBlock` 의 finalization 에서 처음 드러나고, 그때 그 block 은 무효가 된다(`B-06` SNET-FIN-005). 즉 설정 오타 하나가 미래의 특정 height 에서 chain 을 멈출 수 있다.

### 5.4 Fork 별 overlay

fork 별 overlay 는 fork 이름 아래에 저장한 `AnzeonConfig` 이다. 지금은 `boho` 하나뿐이다. 노드는 overlay 에서 `systemContracts` 멤버만 읽고, 그 멤버를 `CollectUpgrades`(§7.1)로 upgrade 목록에 넣는다.

[SNET-CFG-013] fork 별 overlay 는 합의 파라미터를 바꿔서는 안 된다. overlay 는 `systemContracts` 에 이름을 댄 컨트랙트에만 영향을 주며, 이름을 댄 컨트랙트마다 overlay 의 block 부터 `SystemContract` 항목 전체(address, version, params)를 교체한다(§7.2).
Source: params/config.go:1108-1126 (CollectUpgrades reads only Boho.SystemContracts)
Source: consensus/wbft/config.go:124-151 (GetSystemContracts replaces whole entries)

---

## 6. 합의 파라미터와 transition

### 6.1 기본 설정

합의 엔진은 `wbft.DefaultConfig` 에서 시작하지 않는다. 합의 엔진은 모든 값이 0 인 `Config` 에서 시작해 `cfg.Anzeon.WBFT` 의 0 이 아닌 필드만 복사한다.

```python
def base_wbft_config(cfg) -> WbftConfig:
    w = cfg.Anzeon.WBFT
    c = WbftConfig(request_timeout=0, block_period=0, epoch=0,
                   allowed_future_block_time=0, proposer_policy=None,
                   max_request_timeout_seconds=0)
    if w.requestTimeoutSeconds != 0: c.request_timeout = w.requestTimeoutSeconds * 1000
    if w.blockPeriodSeconds != 0:    c.block_period = w.blockPeriodSeconds
    if w.epochLength != 0:           c.epoch = w.epochLength
    if w.allowedFutureBlockTime != 0: c.allowed_future_block_time = w.allowedFutureBlockTime
    if w.proposerPolicy is not None: c.proposer_policy = w.proposerPolicy   # 0 round-robin, 1 sticky
    if w.maxRequestTimeoutSeconds is not None:
        c.max_request_timeout_seconds = w.maxRequestTimeoutSeconds
    c.transitions = sorted_transitions(cfg.Transitions)            # §6.2
    c.system_contract_upgrades = [Upgrade(0, cfg.Anzeon.SystemContracts)] + collect_upgrades(cfg)  # §7
    return c
```

[SNET-CFG-014] 기본 합의 설정은 반드시 위 `base_wbft_config` 대로 끌어내야 한다. 특히 `request_timeout = requestTimeoutSeconds * 1000` 이고, `anzeon.wbft` 에서 0 이거나 없는 필드는 0 값을 그대로 유지한다. 그런 필드는 `wbft.DefaultConfig` 의 값으로 대체되지 않는다.
Source: eth/ethconfig/config.go:213-233 (SetConfigFromChainConfig)
Source: consensus/wbft/config.go:105-122 (Config, DefaultConfig not used here)

### 6.2 Transition

`Transition` 은 `{block, <WBFTConfig 필드를 펼친 것>}` 이다. JSON 에서는 WBFT 필드가 `block` 옆에 나란히 놓인다. 예를 들면 `{"block": 1000, "epochLength": 100}` 이다.

```python
def sorted_transitions(ts):
    # block 오름차순으로 정렬한다. block 이 없는 항목은 맨 뒤로 간다.
    return sort(ts, key=lambda t: (t.block is None, t.block))

def config_at(n) -> WbftConfig:           # Part A 이름; 참조 구현: Config.GetConfig
    c = copy(base)
    for t in base.transitions:            # 이미 정렬되어 있다
        if t.block > n:
            break
        if t.requestTimeoutSeconds != 0:  c.request_timeout = t.requestTimeoutSeconds * 1000
        if t.blockPeriodSeconds != 0:     c.block_period = t.blockPeriodSeconds
        if t.epochLength != 0:            c.epoch = t.epochLength
        if t.proposerPolicy is not None:  c.proposer_policy = t.proposerPolicy
        if t.maxRequestTimeoutSeconds is not None:
            c.max_request_timeout_seconds = t.maxRequestTimeoutSeconds
        # transition 안의 allowedFutureBlockTime 은 무시한다
    return c
```

[SNET-CFG-015] `config_at(n)` 은 반드시 `block <= n` 인 모든 transition 을 `block` 오름차순으로 적용해야 한다. 이때 `config_at(n)` 은 위 코드에 보인 필드만 덮어쓴다. 정수 필드는 값이 0 이 아닐 때 덮어쓰고, `proposerPolicy` 와 `maxRequestTimeoutSeconds` 는 값이 있을 때 덮어쓴다. transition 안의 `allowedFutureBlockTime` 은 반드시 무시해야 한다.
Source: consensus/wbft/config.go:161-192 (GetConfig, getTransitionValue)
Source: eth/ethconfig/config.go:245-262 (copy and sort)
Observable: header

[SNET-CFG-016] 노드는 반드시 transition 을 `config_at` 과 따로 epoch 규칙에 적용해야 한다. `epochLength != 0` 인 transition 은 그 `block` 에서 epoch 세기를 다시 시작하고, 그 block 은 epoch block 이다. `epochLength == 0` 인 transition 은 epoch 경계에 영향을 주지 않는다. 알고리즘은 `A-04` (`is_epoch_block`)에 있다.
Source: consensus/wbft/engine/engine.go:1073-1090 (IsEpochBlockNumber)
Observable: header

---

## 7. System contract upgrade

### 7.1 Upgrade 수집

```python
def collect_upgrades(cfg) -> list[Upgrade]:
    ups = []
    # registry 는 block 오름차순이다. 새 fork 는 Boho 뒤에 덧붙인다
    if cfg.BohoBlock is not None and cfg.Boho is not None and cfg.Boho.SystemContracts is not None:
        ups.append(Upgrade(block=cfg.BohoBlock, system_contracts=cfg.Boho.SystemContracts))
    return ups

upgrade_list = [Upgrade(0, cfg.Anzeon.SystemContracts)] + collect_upgrades(cfg)
```

[SNET-CFG-018] upgrade 목록은 반드시 genesis 항목 `(0, Anzeon.SystemContracts)` 뒤에 `collect_upgrades(cfg)` 를 이은 것이어야 한다. 목록은 반드시 block 번호가 줄어들지 않는 순서여야 한다. 참조 구현은 이 순서에 기대며 목록을 정렬하지 않는다.
Source: eth/ethconfig/config.go:264-271
Source: params/config.go:1089-1126 (CollectUpgrades and its ordering contract)

> 해설: 참조 구현이 목록을 정렬하지 않기 때문에, 새 fork 를 registry 에 추가할 때는 block 순서를 지켜 덧붙여야 한다.

### 7.2 어느 height 에서 유효한 컨트랙트

```python
def system_contracts_at(n) -> SystemContracts:
    sc = SystemContracts()                  # 모든 멤버가 없는 상태에서 시작한다
    for u in upgrade_list:
        if u.block > n:
            break
        for name in MEMBERS:                # govValidator, nativeCoinAdapter, govMinter, govMasterMinter, govCouncil
            if getattr(u.system_contracts, name) is not None:
                setattr(sc, name, getattr(u.system_contracts, name))
    return sc

def upgrades_at(n) -> list[Upgrade]:        # block 이 정확히 n 인 항목
    return [u for u in upgrade_list if u.block == n]
```

[SNET-CFG-019] 이 명세가 "height `n` 에서 유효한" 컨트랙트라고 쓰는 곳에서는 반드시 그 컨트랙트를 `system_contracts_at(n)` 으로 구해야 한다. 이 값에는 `n` 에서의 upgrade 도 포함된다.
Source: consensus/wbft/config.go:124-159 (GetSystemContracts, getSystemContractsValue)

[SNET-CFG-020] 참조 구현은 모든 곳에서 `system_contracts_at(n)` 을 쓰지는 않는다. 다음 읽기는 height 와 상관없이 반드시 `genesis_system_contracts` (genesis `Anzeon.SystemContracts` 항목)를 써야 한다.
1. header gas tip 을 읽는 `GovValidator` 주소 (`B-06` §5).
2. 네이티브 `Transfer` log 를 내고 `NativeCoinManager` 를 부를 수 있는 `NativeCoinAdapter` 주소.
3. `AccountManager` 를 부를 수 있는 `GovCouncil` 주소.

반면 candidate 와 BLS key 를 읽는 `GovValidator` 주소는 반드시 `system_contracts_at(n)` 을 써야 한다(`B-08`).
Source: consensus/wbft/engine/engine.go:622-645 (getGasTip: genesis address)
Source: consensus/wbft/engine/engine.go:607-612, 875 (candidates: GetSystemContracts)
Source: core/vm/evm.go:606; core/vm/native_manager.go:462-475

구현 노트 (informative). 현재 fork registry 에서는 `Boho` 가 `GovMinter` 의 버전만 바꾸므로 `system_contracts_at(n)` 과 `genesis_system_contracts` 는 같은 주소를 준다. 두 방식의 결과는 미래의 overlay 가 컨트랙트를 다른 주소로 옮길 때에만 달라진다.

[SNET-CFG-021] `block == 0` 인 upgrade 는 반드시 genesis 를 만드는 중에 적용해야 하고(`B-02` §4), `block > 0` 인 upgrade 는 반드시 block `block` 의 finalization 중에 적용해야 한다(`B-06` §2). upgrade 항목의 버전이 `v1` 이고 `params` map 이 있는(null 이 아닌) 컨트랙트는 초기 storage 도 받는다. `params` 가 빈 map 이어도 같다. 다른 버전은 코드만 바꾼다.
Source: core/genesis.go:757-767 (block 0 overlays)
Source: consensus/wbft/config.go:199-225 (GetSystemContractsStateTransition)
Source: systemcontracts/systemcontracts.go:62-157
Observable: state

---

## 8. 시작 시 순서 검사와 호환성 검사

이 검사들은 노드가 데이터베이스를 열 때 실행된다. 이 검사들은 어떤 block 의 유효성에도 직접 영향을 주지 않는다. 그러나 노드가 결국 어떤 설정을 쓰는지를 정하고, 그 설정이 유효성을 정한다.

[SNET-CFG-022] fork 순서 검사는 반드시 다음 설정을 거부해야 한다. 검사는 목록 `homestead, daoFork(optional), eip150, eip155, eip158, byzantium, constantinople, petersburg, istanbul, muirGlacier(optional), berlin, london, arrowGlacier(optional), grayGlacier(optional), applepie(optional), boho(optional), mergeNetsplit(optional), shanghaiTime(optional), cancunTime(optional), pragueTime(optional), verkleTime(optional)` 을 차례로 훑고, 없는 optional 항목은 건너뛴다. 거부하는 경우는 다음과 같다.
1. 선택이 아닌 fork 가 없는데 그 뒤의 fork 가 있다.
2. block 기반 fork 가 앞선 fork 보다 이른 block 에 예약되어 있다.
3. timestamp fork 가 앞선 timestamp fork 보다 이른 시각에 예약되어 있다.
4. block 기반 fork 가 timestamp fork 뒤에 온다.

목록에서 모든 timestamp fork 는 모든 block 기반 fork 뒤에 있으므로 4번 조건은 성립할 수 없다. 참조 구현에서 이 조건을 검사하는 코드에는 도달하지 않는다.
Source: params/config.go:1153-1221 (CheckConfigForkOrder)
Source: core/genesis.go:355-357, 584-586 (called on setup and on genesis commit)

[SNET-CFG-023] 저장된 설정이 있으면, 노드는 다음 경우에 반드시 설정 충돌을 보고해야 한다.
1. Homestead, DAO, EIP-150, EIP-155, EIP-158, Byzantium, Constantinople, Petersburg, Istanbul, Muir Glacier, Berlin, London, Arrow Glacier, Gray Glacier, Applepie, Boho, Merge-netsplit 가운데 하나에서 저장된 block 과 새 block 이 다르고, 둘 가운데 하나가 현재 head 에서 이미 활성이다. 다만 새 Petersburg block 이 저장된 Constantinople block 과 같으면 충돌이 아니다.
2. 저장된 DAO fork 가 활성인데 DAO support flag 가 다르다.
3. 저장된 EIP-158 fork 가 활성인데 chain ID 가 다르다.
4. 네 timestamp fork 에 대해 head timestamp 기준으로 1번과 같은 조건이 성립한다.
5. 저장된 설정과 새 설정 가운데 정확히 하나에만 `anzeon` 섹션이 있다.

block 충돌의 되감기 지점은 설정된 값 가운데 작은 값 `min(stored, new)` 에서 1 을 뺀 값이다. 그 작은 값이 0 이면 되감기 지점은 0 이다. `anzeon` 충돌의 되감기 지점은 0 이다. 노드는 가장 낮은 충돌을 보고한다.

노드는 `anzeon.wbft`, `anzeon.init`, `anzeon.systemContracts`, `boho`, `transitions` 의 차이를 감지하지 않는다.
Source: params/config.go:1128-1151, 1223-1301 (CheckCompatible, checkCompatible)
Source: params/config.go:1407-1411, 1431-1451, 1475-1482 (Anzeon presence, rewind point)

[SNET-CFG-028] SNET-CFG-023 의 충돌이 있어도 노드는 시작할 때 새 설정을 거부하지 않는다. 노드는 다음과 같이 반응해야 한다.
1. head 번호가 0 이 아니고 block 되감기 지점이 0 이 아니거나, head timestamp 가 0 이 아니고 time 되감기 지점이 0 이 아니면, 노드는 반드시 chain 을 되감기 지점까지 되감은 뒤 새 설정을 저장해야 한다. 되감기 지점의 state 가 없으면 노드는 그 지점 이하에서 state 가 남아 있는 가장 가까운 block 까지 더 되감는다.
2. 그 밖의 경우에는 노드는 반드시 되감지 않고 새 설정을 저장해야 한다. 모든 `anzeon` 충돌과, block 0 으로 옮기거나 block 0 에서 옮긴 모든 fork 가 여기에 들어간다.

거부하는 것은 `gstable init` 뿐이다. `gstable init` 은 1번 조건이 성립하면 오류를 내고 종료하며, 저장된 설정을 바꾸지 않는다. 1번 조건이 성립하지 않으면 `gstable init` 도 새 설정을 저장한다.
Source: core/genesis.go:377-387 (rewind condition, overwrite)
Source: core/blockchain.go:281-284, 468-477 (ConfigCompatError not fatal; rewind and store)
Source: core/blockchain.go:580-600, 654 (SetHead, setHeadBeyondRoot)
Source: cmd/gstable/chaincmd.go:243-246 (init exits on error)

구현 노트 (informative). 참조 코드로 실행해 확인했다. block 4 개짜리 chain 에 저장된 Homestead block 이 2 일 때, 새 Homestead block 을 3 으로 주면 archive 모드에서는 head 가 1 로, 기본 모드에서는 state 가 저장된 가장 가까운 block 인 0 으로 되감겼고 새 값이 저장되었다. 새 Homestead block 을 0 으로 주거나, 저장된 block 0 에 새 block 2 를 주면 head 는 4 에 그대로 있었고, 노드는 오류 없이 새 값을 저장했다.

[SNET-CFG-024] 노드가 명시적인 genesis 없이, 곧 `--mainnet`, `--testnet`, genesis 파일 가운데 아무것도 주지 않고 시작하고, 데이터베이스에 저장된 genesis hash 가 Ethereum mainnet hash 도 아니고 `StableNetMainnetGenesisHash` 도 아니면, 노드는 반드시 저장된 설정을 그대로 써야 한다. StableNet testnet 도 여기에 해당한다. 바이너리 안의 testnet preset 은 `--testnet` 을 줄 때에만 적용된다.
Source: core/genesis.go:364-373 (private-network special case)
Source: core/genesis.go:482-501 (configOrDefault)
Source: cmd/utils/flags.go:1759-1771 (flags select presets)

---

## 9. 지원하지 않는 fork

WBFT 는 Shanghai block 이나 Cancun block 의 header 를 모두 거부한다(`B-03` SNET-BHDR-003, SNET-BHDR-005). 그러므로 두 fork 가운데 하나라도 예약한 설정을 쓰면, chain 은 timestamp 가 fork 시각에 도달한 첫 block 에서 멈춘다.

[SNET-CFG-025] StableNet 설정은 반드시 `shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime` 을 비워 두어야 한다. 참조 구현은 시작할 때 그런 설정을 거부하지 않고, 대신 그 시각 이후의 block 을 거부한다(`B-03`).
Source: consensus/wbft/engine/engine.go:220-229 (verifyHeader)
Source: params/config.go:57-63 (mainnet preset leaves them nil)

구현 노트 (informative). 여기서 "지원하지 않는다" 는 header 필드, withdrawal, blob transaction, beacon root system call 을 가리킨다. 그 fork 들의 EVM 기능 가운데 여러 개는 Anzeon 명령어 집합에 들어 있다(§4.2). `PUSH0`, initcode 계량, transient storage, `MCOPY`, EIP-6780, EIP-7702 가 그렇다. 켜진 기능과 꺼진 기능의 정확한 목록은 SNET-CFG-029 에 있다.

참조 바이너리는 go-ethereum 의 `--override.cancun`, `--override.verkle` 옵션을 그대로 가진다. 이 옵션은 불러온 설정 위에 `cancunTime` 이나 `verkleTime` 을 덮어쓴다. `gstable init` 과 매 시작 모두에서 덮어쓰며, 결과가 §8 의 검사를 통과하면 저장된다.

[SNET-CFG-031] 노드는 Anzeon chain 에서 실행 시 옵션이 `shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime` 을 정하게 해서는 안 된다. 참조 구현은 `--override.cancun` 과 `--override.verkle` 을 받아들인다. 그러면 노드는 block 번호 fork 가 모두 지난 뒤부터 fork ID(§11.1)의 다음 fork 로 그 시각을 알리고, 그 시각부터 모든 header 를 거부하며(`wbft does not support cancun fork`, SNET-BHDR-005) fork ID hash 도 바뀌므로 네트워크에서 떨어진다. 덮어쓴 값은 저장된다. 옵션 없이 다시 시작해도 저장된 설정을 그대로 쓰는 경우(SNET-CFG-024)에는 값이 남는다. preset 을 적용하는 시작(`--mainnet`, `--testnet`, StableNet mainnet 데이터베이스)은 덮어쓴 시각에 아직 이르지 않았다면 그 값을 preset 값으로 바꾼다.
Source: cmd/utils/flags.go:251-260 (flags), cmd/gstable/config.go:174-181, cmd/gstable/chaincmd.go:213-221 (applied on start and on init)
Source: core/genesis.go:215-228 (applyOverrides), 355-387 (applied, checked and stored)
Source: consensus/wbft/engine/engine.go:227-229 (Cancun header rejected)
Source: core/forkid/forkid.go:242-290 (timestamp forks enter the fork ID)
Observable: network, header

구현 노트 (informative). 참조 코드로 실행해 확인했다. preset genesis 에 `cancunTime = 2_000_000_000` 을 넣고 head 시각 1000 으로 `forkid.NewID` 를 계산하면, mainnet fork ID 는 `0xbfc8373b`, next `2000000000` 이 된다. testnet 의 Boho 이전 fork ID 는 `0xdea2c682`, next `14408500` 그대로이다. block 번호 fork 를 먼저 알리기 때문이다.

---

## 10. 설정과 함께 쓰는 protocol 상수

아래 값은 바이너리에 컴파일되어 있다. 이 값들은 `ChainConfig` 에 속하지 않으며 네트워크마다 바꿀 수 없다.

| 명세 이름 | 참조 구현 이름 | 값 | 쓰는 곳 |
|---|---|---|---|
| `GAS_LIMIT_BOUND_DIVISOR` | `GasLimitBoundDivisor` | `1024` | `B-03` §2.1 |
| `MIN_GAS_LIMIT` | `MinGasLimit` | `5000` | `B-03` §2.1 |
| `MAX_GAS_LIMIT` | `MaxGasLimit` | `2^63 - 1` | `B-03` §2.1 |
| `GENESIS_GAS_LIMIT` | `GenesisGasLimit` | `4_712_388` | `B-02` §2 (genesis `gasLimit == 0` 일 때) |
| `ELASTICITY_MULTIPLIER` | `ElasticityMultiplier` | `2` | `B-03` §2.1 (parent 가 London 이전일 때만) |
| `INITIAL_BASE_FEE` | `InitialBaseFee` | `1_000_000_000` | `B-03` §2.3 (첫 London block) |
| `INCREASING_THRESHOLD` | `IncreasingThreshold` | `20` (%) | `B-03` §2.3 |
| `DECREASING_THRESHOLD` | `DecreasingThreshold` | `6` (%) | `B-03` §2.3 |
| `BASE_FEE_CHANGE_RATE` | `BaseFeeChangeRate` | `2` (%) | `B-03` §2.3 |
| `MIN_BASE_FEE` | `MinBaseFee` | `20_000_000_000_000` | `B-02` §2, `B-03` §2.3 |
| `MAX_BASE_FEE` | `MaxBaseFee` | `20_000_000_000_000_000` (0 이면 상한을 끈다) | `B-03` §2.3 |
| `INITIAL_GAS_TIP` | `InitialGasTip` | `27_600_000_000_000` | `B-02` §3 |
| `DILIGENCE_DENOMINATOR` | `DiligenceDenominator` | `1_000_000` | `A-04`, `B-06` §3 |
| `DEFAULT_DILIGENCE` | `DefaultDiligence` | `1_900_000` | `B-02` §3 |
| `MAX_CODE_SIZE` | `MaxCodeSize` | `253_952` (EIP-170 값 `24_576` 은 쓰지 않는다) | SNET-CFG-007, `B-07` SNET-TX-088 |
| `MAX_INITCODE_SIZE` | `MaxInitCodeSize` | `253_952` (`= MaxCodeSize`. EIP-3860 값 `49_152` 는 쓰지 않는다) | SNET-CFG-029 (`CREATE`/`CREATE2` 에만), `B-07` SNET-TX-088 |
| `P256_VERIFY_GAS` | `P256VerifyGas` | `6_900` (EIP-7951. EIP-7212 값 `3_450` 은 쓰지 않는다) | SNET-CFG-010, `B-07` SNET-TX-089 |

[SNET-CFG-026] 적합한 노드는 반드시 위 표의 상수 값을 써야 한다.
Source: params/protocol_params.go:26-29, 128-141, 171
Source: params/config.go:1303-1336 (accessors return the constants)
Source: core/types/istanbul.go:43-46 (DiligenceDenominator, DefaultDiligence)
Observable: header, state

---

## 11. 네트워크 preset

go-stablenet 은 preset 두 개를 제공한다. `--mainnet` 은 network id 8282 preset 을, `--testnet` 은 network id 8283 preset 을 고른다. 이 preset 으로 만든 genesis block 은 `B-02` §8 에 있다. 두 preset 은 모두 규범이다. 두 preset 의 기본 bootstrap node 는 `B-09` §12.4 에 있다.

### 11.1 Fork 일정과 합의 파라미터

| 파라미터 | Mainnet (8282) | Testnet (8283) |
|---|---|---|
| `chainId` | `8282` | `8283` |
| Homestead .. London (Muir Glacier 포함 11 개 fork) | 모두 `0` | 모두 `0` |
| `arrowGlacierBlock`, `grayGlacierBlock`, `mergeNetsplitBlock` | 없음 | 없음 |
| `shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime` | 없음 | 없음 |
| `applepieBlock` | `0` | `0` |
| `bohoBlock` | `0` | `14_408_500` |
| `anzeon.wbft.epochLength` | `10` | `140` |
| `anzeon.wbft.blockPeriodSeconds` | `1` | `1` |
| `anzeon.wbft.requestTimeoutSeconds` | `2` | `2` |
| `anzeon.wbft.proposerPolicy` | `0` (round-robin) | `0` (round-robin) |
| `anzeon.wbft.maxRequestTimeoutSeconds`, `allowedFutureBlockTime` | 없음 | 없음 |
| `transitions` | 없음 | 없음 |
| `boho.systemContracts` | `govMinter {0x…1003, v2}` | `govMinter {0x…1003, v2}` |
| Network ID (preset flag 의 기본값) | `8282` | `8283` |
| Fork ID (EIP-2124) | `0xbfc8373b`, next `0` | head 가 `14_408_500` 보다 작으면 `0xdea2c682`, next `14408500`. `14_408_500` 부터 `0x7b44b412`, next `0` |
| Genesis hash 상수 | `0xf192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b` | `0x2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490` |

Source: params/config.go:29-32, 44-152, 155-272
Source: cmd/utils/flags.go:1759-1771

fork ID 는 EIP-2124 방식으로 계산한다. 입력은 genesis hash 와, `ChainConfig` 최상위의 `…Block`, `…Time` 필드 가운데 값이 있는 모든 필드이다. `applepieBlock` 과 `bohoBlock` 도 여기에 들어가고, block 0 의 fork 는 빠진다. `anzeon`, `boho`, `transitions` 는 fork ID 에 들어가지 않는다. mainnet preset 에서는 모든 fork 가 block 0 이므로 fork ID 는 genesis hash 의 CRC32 이다. testnet preset 에서는 `bohoBlock` 이 genesis 뒤의 유일한 fork 이다. 계산 규칙(SNET-SYNC-063)과 fork ID 를 싣는 `Status` handshake 는 `B-09` §12.1 에 있다. 표의 값은 preset genesis 에 `forkid.NewID` 를 돌려 계산했다.

[SNET-CFG-032] 네트워크 8282 나 8283 에 참여하는 노드는 반드시 자기 현재 head 에 대해 위 표의 fork ID 를 알려야 한다(`B-09` SNET-SYNC-063). 그러므로 노드는 EIP-2124 fork 목록에 반드시 `applepieBlock` 과 `bohoBlock` 을 넣어야 한다.
Source: core/forkid/forkid.go:75-99, 242-290 (NewID, gatherForks)
Source: params/config.go:44-272 (preset fork blocks)
Observable: network

> 해설: mainnet 의 `epochLength 10` 은 10 block 마다 epoch block 이 온다는 뜻이다. 그래서 mainnet 에서는 epoch 계산(`A-04`)과 base fee 분배에 쓰는 validator 집합의 전환이 testnet 보다 14 배 자주 일어난다. mainnet 호환 시험을 testnet 시험으로 대신하면 epoch 경계 버그를 놓치기 쉽다.

### 11.2 초기 validator

Mainnet: validator 는 하나이다. 주소는 `0xaa5faa65e9cc0f74a85b6fdfb5f6991f5c094697` 이고 BLS key 는 `0xaec493af8fa358a1c6f05499f2dd712721ade88c477d21b799d38e9b84582b6fbe4f4adc21e1e454bc37522eb3478b9b` 이다. 이 preset 에는 "TODO: this is just for test on mainnet" 이라는 주석이 달려 있다. 그러나 이 preset 은 운영 중인 mainnet 의 preset 이며 규범이다. 이 validator 하나가 genesis 의 validator 집합이다.

Testnet: validator 는 일곱이며 아래 순서를 따른다. 이 순서가 첫 epoch 의 validator 인덱스 0..6 을 정한다(`B-02` §3).

| 인덱스 | Validator | BLS public key |
|---|---|---|
| 0 | `0x9f06600b2c17108662e3840e76bb27c9468eb73d` | `0x96683524c3b7e224f2146a0dbb87593e3dee21b7d97c1b24ef6ed799c977be40d3dff8e45b7fcdb48f7713095d84dd9c` |
| 1 | `0x1aa18ec0b3131171b1b1ddba2dffd81410b30a5a` | `0x80bd166ebfdb29553801dc22f5b83534945cc2a6dacf39d422383cb3041c8afab8cd430ffc29e9297ba2e114efae487f` |
| 2 | `0xe63b413353e1ba4ac99f2bd1892328e2365ec574` | `0xa418ff21040af17cb3f5109fa075c92d771e508e715230413d6548d54114666011b715d2e60dfd4e20661d5327b67d6d` |
| 3 | `0x20f681210071932dbe6387378adf6f26af029f7d` | `0xa529026635cf95fd84a9633c62bedaf2a5999f2d5542164086176e24b0c2256a3daa7b76344b631c0bd344895b3f44b9` |
| 4 | `0x5803c14973690550d6ffc2014b7bffd8005f6021` | `0xb2aa67ceb23d96e4de5dca871dfbda6ba122a3b5a55b3a00547fdda906ab8e4892e98de4b36b541088ac8ff6de7d6a35` |
| 5 | `0x59f6e6add1fbeab316a7b17c9f1966b3655efedb` | `0x88717d8edbabaf65015b656b5a14bc27705bead8a75f81b9bc064b69b7a5e8f5a010d54cdb85b5b3cab16ce5d816062f` |
| 6 | `0x0a8dd92ce7ce53bbf6aed40228f9029bb3f92702` | `0x86949700dc2722f48cd649af74cff788bf17553d19278592ca7be54929d1646d60efd1ea12f9dabd7d0143d9813bfea8` |

Source: params/config.go:73-76, 177-196

### 11.3 System contract 파라미터

genesis 에서 다섯 컨트랙트는 모두 기본 주소(§5.2)와 버전 `v1` 을 쓴다. 두 preset 의 모든 governance 컨트랙트는 `expiry = 604800` (7일), `memberVersion = 1`, `maxProposals = 3` 을 쓴다.

| 컨트랙트 | 파라미터 | Mainnet | Testnet |
|---|---|---|---|
| `govValidator` | `quorum` | `1` | `2` |
| | `members` | 유일한 validator 주소 | 멤버 주소 7 개 `0x58f13FE4…4D43`, `0xE0E5BDD4…f453`, `0x4053E68d…15dE`, `0x56D97Be0…4bD1`, `0x79495F15…9002`, `0x9B690222…6746`, `0xF7AeCcD8…c833` (전체 목록은 source 에 있다) |
| | `validators` | `init.validators` 와 같다 | `init.validators` 와 같다(순서도 같다) |
| | `blsPublicKeys` | `init.blsPublicKeys` 와 같다 | `init.blsPublicKeys` 와 같다 |
| | `gasTip` | `27600000000000` | `27600000000000` |
| `nativeCoinAdapter` | `masterMinter` / `minters` | `0x…1002` / `0x…1003` | `0x…1002` / `0x…1003` |
| | `minterAllowed` | `10000000000000000000000000000` (1e28) | `100000000000000000000000000000` (1e29) |
| | `name` / `symbol` / `decimals` / `currency` | `WKRC` / `WKRC` / `18` / `KRW` | `WKRC` / `WKRC` / `18` / `KRW` |
| `govMasterMinter` | `quorum`, `members` | `1`, 유일한 validator | `2`, 멤버 7 개 |
| | `fiatToken` / `minters` | `0x…1000` / `0x…1003` | `0x…1000` / `0x…1003` |
| | `maxMinterAllowance` | 1e28 | 1e29 |
| `govMinter` | `quorum`, `members`, `fiatToken` | `1`, 유일한 validator, `0x…1000` | `2`, 멤버 7 개, `0x…1000` |
| `govCouncil` | `quorum`, `members` | `1`, 유일한 validator | `2`, 멤버 7 개 |

두 preset 모두 `govCouncil` 에 `blacklist` 나 `authorizedAccounts` 파라미터가 없다.

[SNET-CFG-027] 네트워크 8282 나 8283 에 참여한다고 주장하는 노드는 반드시 §11.1-§11.3 의 preset 값(그리고 `B-02` §8 의 genesis allocation)을 정확히 그대로 써야 한다.
Source: params/config.go:44-272
Source: core/genesis.go:617-632 (preset genesis blocks)
Observable: header, state
