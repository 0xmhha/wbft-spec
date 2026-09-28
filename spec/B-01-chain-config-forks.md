# B-01 Chain configuration and forks

- Status: draft
- Area code: `CFG`
- Reference implementation: go-stablenet `740526d03`

This chapter specifies the configuration object from which every other chapter of Part B, and the consensus configuration of Part A, is derived. go-stablenet keeps one `params.ChainConfig` per chain. It contains the Ethereum fork schedule inherited from go-ethereum, two StableNet forks (`ApplepieBlock`, `BohoBlock`), the Anzeon section (WBFT parameters, initial validators, system contracts), per-fork overlays (`Boho`), and WBFT parameter transitions. Two nodes that load different values for any field marked *consensus-critical* below will disagree on block validity at some height, even if both implement Part A and Part B correctly. The chapter therefore defines (1) the meaning of each field, (2) the functions that derive height-dependent values from it, (3) the startup checks that go-stablenet applies, and (4) the exact values of the two built-in network presets.

---

## 1. Notation

- `cfg` denotes the loaded `ChainConfig`. `n` is a block number, `t` a block timestamp (seconds).
- `forked(s, n)` is the block-fork predicate of §3.1; `forked_time(s, t)` the timestamp-fork predicate.
- `wbft_cfg` denotes the consensus `Config` derived in §6 (Part A calls the per-height view `config_at(n)`, see `A-01`).
- `system_contracts_at(n)` and `upgrades_at(n)` are defined in §7. `genesis_system_contracts` is `cfg.Anzeon.SystemContracts` (the unmodified genesis section).

---

## 2. Fields of `ChainConfig` used by StableNet

JSON names are those of the genesis file and of the stored chain configuration. Fields not listed (`DAOForkBlock`, `DAOForkSupport`, `TerminalTotalDifficulty`, `TerminalTotalDifficultyPassed`, `Ethash`, `Clique`) are inherited from go-ethereum and are absent in StableNet configurations; they are out of scope.

| Field | JSON | Type | Consensus-critical | StableNet meaning |
|---|---|---|---|---|
| `ChainID` | `chainId` | integer | yes | Replay protection (EIP-155), transaction signer, randao data (`A-02`) |
| `HomesteadBlock` .. `LondonBlock` | `homesteadBlock`, `eip150Block`, `eip155Block`, `eip158Block`, `byzantiumBlock`, `constantinopleBlock`, `petersburgBlock`, `istanbulBlock`, `muirGlacierBlock`, `berlinBlock`, `londonBlock` | block number or absent | yes | Ethereum forks, §4.1 |
| `ArrowGlacierBlock`, `GrayGlacierBlock` | `arrowGlacierBlock`, `grayGlacierBlock` | block number or absent | no effect on block validity | Difficulty-bomb delays; no effect under WBFT (difficulty is fixed, `A-08`). A non-zero value is part of the fork ID (EIP-2124), so peers with different values may be rejected at the eth handshake |
| `MergeNetsplitBlock` | `mergeNetsplitBlock` | block number or absent | no effect on block validity | Not used by StableNet. A non-zero value is part of the fork ID (EIP-2124), so peers with different values may be rejected at the eth handshake |
| `ApplepieBlock` | `applepieBlock` | block number or absent | yes | Fee delegation, §4.3. A non-zero value is part of the fork ID (EIP-2124, §11.1) |
| `BohoBlock` | `bohoBlock` | block number or absent | yes | Boho fork, §4.4. A non-zero value is part of the fork ID (EIP-2124, §11.1) |
| `ShanghaiTime`, `CancunTime`, `PragueTime`, `VerkleTime` | `shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime` | timestamp or absent | yes | Unsupported under WBFT, §9 |
| `Anzeon` | `anzeon` | `AnzeonConfig` or absent | yes | Presence makes the chain an Anzeon chain, §5 |
| `Boho` | `boho` | `AnzeonConfig` or absent | yes | System-contract overlay activated at `BohoBlock`, §5.4 |
| `Transitions` | `transitions` | list of `Transition` | yes (see §6) | WBFT parameter changes by height |

[SNET-CFG-001] A conforming node MUST derive every height-dependent rule of Part A and Part B from a single `ChainConfig` with the fields above, and two nodes of the same network MUST load equal values for every field marked consensus-critical.
Source: params/config.go:785-834 (ChainConfig)
Source: eth/ethconfig/config.go:192-201 (CreateConsensusEngine uses the same ChainConfig)

---

## 3. Fork predicates

### 3.1 Block-number and timestamp predicates

```python
def forked(s: Optional[int], n: Optional[int]) -> bool:
    # absent fork or absent height: not active
    if s is None or n is None:
        return False
    return s <= n

def forked_time(s: Optional[int], t: int) -> bool:
    if s is None:
        return False
    return s <= t
```

[SNET-CFG-002] For every block-numbered fork `X` (Homestead through London, Applepie, Boho), `is_X(n)` MUST equal `forked(cfg.XBlock, n)`, with the single go-ethereum exception that `is_petersburg(n)` is also true when `PetersburgBlock` is absent and `forked(ConstantinopleBlock, n)` holds.
Source: params/config.go:1017-1045 (IsPetersburg, IsApplepie, IsBoho)
Source: params/config.go:1364-1369 (isBlockForked)

[SNET-CFG-003] For every timestamp fork `Y` in {Shanghai, Cancun, Prague, Verkle}, `is_Y(n, t)` MUST equal `is_london(n) and forked_time(cfg.YTime, t)`.
Source: params/config.go:1066-1083 (IsShanghai, IsCancun, IsPrague, IsVerkle)
Source: params/config.go:1390-1395 (isTimestampForked)

### 3.2 Anzeon activation

Anzeon is not scheduled by block number. It is switched on by the presence of the `anzeon` section and is then active from the genesis block onwards.

[SNET-CFG-004] A chain MUST be treated as an Anzeon chain at every height, including the genesis block, if and only if `cfg.Anzeon` is present. There is no Anzeon activation block.
Source: params/config.go:1085-1087 (AnzeonEnabled)
Source: params/config.go:1407-1411 (isForkAnzeonIncompatible: enable/disable by presence)

### 3.3 Execution rule flags

The EVM and the state transition read a `Rules` value computed once per block.

[SNET-CFG-005] The per-block rule flags MUST be computed as follows: `IsAnzeon = (cfg.Anzeon is present)`; `IsApplepie = is_applepie(n)` (not gated by Anzeon); `IsBoho = IsAnzeon and is_boho(n)`; `IsMerge = is_merge_flag and is_london(n)`; `IsShanghai`, `IsCancun`, `IsPrague`, `IsVerkle` are the predicates of SNET-CFG-003 additionally gated by `IsMerge`.
Source: params/config.go:1512-1541 (Rules)

[SNET-CFG-006] For WBFT blocks (header `Difficulty == 1`) the `is_merge_flag` passed to SNET-CFG-005 MUST be false, so that `IsMerge`, `IsShanghai`, `IsCancun`, `IsPrague` and `IsVerkle` are false inside the EVM regardless of the timestamp forks. The EVM random value (`PREVRANDAO`, opcode `0x44`) MUST be `header.MixDigest`.
Source: core/vm/evm.go:162 (Rules called with Difficulty==0 && Random!=nil)
Source: core/evm.go:61-64 (Random = &header.MixDigest when Difficulty is 0 or 1)
Observable: state

Implementation note (informative). `Rules.IsBoho` is gated by Anzeon but `Rules.IsApplepie` is not. On an Anzeon chain the difference is invisible. The raw `ChainConfig.IsBoho` is used only inside `Rules`.

---

## 4. What each fork changes

### 4.1 Ethereum forks

[SNET-CFG-007] Each Ethereum fork from Homestead through London MUST take effect with the go-ethereum semantics of that fork at the block given by its field, except that the EIP-170 limit on deployed code is `MAX_CODE_SIZE = 253_952` bytes (§10) instead of `24_576`: a `CREATE`, `CREATE2` or contract-creation transaction whose returned code is longer than `MAX_CODE_SIZE` fails with an exceptional halt that consumes all gas of the creating frame, and shorter code up to that limit is deployed at the usual 200 gas per byte (`B-07` SNET-TX-088). On both presets (§11) all of them are at block 0, so every StableNet block is a London block.
Source: params/config.go:44-65, 155-169 (presets)
Source: params/config.go:974-1034 (predicates)
Source: params/protocol_params.go:140 (MaxCodeSize), core/vm/evm.go:528-530 (EIP-170 check)
Observable: state

The London rule set is modified by Anzeon in the base-fee formula (`B-03` §2.3) and in the base-fee destination (`B-06` §3). EIP-3529 refunds, EIP-3198 `BASEFEE`, EIP-2718/2930/1559 transaction types are as in go-ethereum.

Features of later Ethereum forks are not enabled by fork rules on a WBFT chain (SNET-CFG-006) but chosen one by one through the Anzeon instruction and precompile sets. An implementation that imports an EVM with Shanghai, Cancun or Prague rules must switch off everything else.

[SNET-CFG-029] On an Anzeon chain exactly the following post-London execution features MUST be active: EIP-3855 (`PUSH0`); the `CREATE`/`CREATE2` part of EIP-3860 (initcode word gas and the initcode limit `MAX_INITCODE_SIZE = 253_952` bytes of §10, not `49_152`, `B-07` SNET-TX-088); EIP-1153 (`TLOAD`, `TSTORE`); EIP-5656 (`MCOPY`); EIP-6780 (`SELFDESTRUCT`); EIP-7702 (type `0x04`, `B-07` SNET-TX-091 to SNET-TX-094); the KZG point-evaluation precompile at `0x0a`; and, from `BohoBlock` on, `P256VERIFY` (SNET-CFG-010). Every other rule of Shanghai, Cancun and Prague MUST NOT be applied at any height, in particular EIP-3651 (warm coinbase), the transaction part of EIP-3860 (initcode gas in the intrinsic gas and the initcode size check of a contract-creation transaction), EIP-4895 (withdrawals), EIP-4844 blob transactions and `BLOBHASH`, EIP-7516 (`BLOBBASEFEE`), EIP-4788 (beacon root), the EIP-2537 BLS12-381 precompiles, EIP-2935 (history storage), EIP-7623 (calldata floor, `B-07` SNET-TX-092) and EIP-7685 (execution requests). An address outside the precompile set of `B-07` §9.4 and the native managers is an ordinary account.
Source: params/config.go:1512-1541 (Rules: IsShanghai, IsCancun, IsPrague require IsMerge)
Source: core/vm/jump_table.go:117-140 (newAnzeonInstructionSet), core/vm/interpreter.go:58-67 (table selection)
Source: core/vm/contracts.go:127-157 (Anzeon and Boho precompile sets), params/protocol_params.go:141 (MaxInitCodeSize)
Source: core/state_transition.go:482, 501 (intrinsic gas and initcode check follow IsShanghai), core/state/statedb.go:1388 (EIP-3651)
Observable: state

### 4.2 Anzeon (presence of `cfg.Anzeon`)

The list below is exhaustive for effects on validity and state; the rows point to the chapter that specifies each one normatively. It was built from every call site of `AnzeonEnabled()` and `Rules.IsAnzeon` in the reference implementation (non-test files).

| Effect | Where specified | Source |
|---|---|---|
| WBFT consensus engine replaces Ethash/Clique | Part A | eth/ethconfig/config.go:192-201 |
| Genesis: extra data rebuilt as `WBFTExtra`, system contracts injected, `BaseFee = MinBaseFee` | `B-02` | core/genesis.go:242-263, 535-538 |
| Base-fee formula replaced by the threshold rule | `B-03` §2.3 | consensus/misc/eip1559/eip1559.go:62-88 |
| Base fee redistributed to validators by diligence | `B-06` §3 | consensus/wbft/engine/engine.go:941-946 |
| Transaction signer: `AnzeonSigner` (adds fee-delegated and set-code types) | `B-07` | core/types/transaction_signing.go:51-52, 79-81 |
| Effective tip cap forced to header `GasTip` for non-authorized senders | `B-07` | core/state_transition.go:159-172 |
| Blacklisted sender/recipient/fee payer rejected; value transfer to zero address or precompile rejected | `B-07` | core/state_transition.go:505-516, 578-584; core/vm/evm.go:210-219, 480, 614-627 |
| `AuthorizedTxExecuted` log appended for authorized senders | `B-07` | core/state_transition.go:587-599 |
| Native coin `Transfer` log emitted on value transfer, address = genesis `NativeCoinAdapter` | `B-07` | core/vm/evm.go:590-609 |
| Receipt `EffectiveGasPrice` = message gas price | `B-07` | core/state_processor.go:157-159; core/types/receipt.go:396-400 |
| EVM instruction set `anzeonInstructionSet`: London + `PREVRANDAO` + `PUSH0` (EIP-3855) + initcode limit (EIP-3860) + `TLOAD`/`TSTORE` (EIP-1153) + `MCOPY` (EIP-5656) + EIP-6780 `SELFDESTRUCT` + EIP-7702 | `B-07` | core/vm/jump_table.go:117-140; core/vm/interpreter.go:66-67 |
| Precompiles: 0x01..0x0a (Cancun set, incl. KZG point evaluation) + BLS PoP at `0x…B00001` | `B-07` | core/vm/contracts.go:127-139; core/vm/evm.go:40-60 |
| Native managers `NativeCoinManager` `0x…B00002`, `AccountManager` `0x…B00003` | `B-07`, `B-04` | core/vm/evm.go:62-72; core/vm/native_manager.go:462-475 |
| EIP-7702 code delegation resolution | `B-07` | core/vm/evm.go:631-660 |
| Sync: total-difficulty adjustment in peer selection | `B-09` | eth/sync.go:205-214 |

[SNET-CFG-008] On an Anzeon chain every effect in the table above MUST be applied at every height, as specified in the referenced chapter.
Source: see table
Observable: header, state

### 4.3 Applepie (`ApplepieBlock`)

Applepie enables fee delegation. It changes no header field.

[SNET-CFG-009] A transaction whose fee payer is set and differs from the sender MUST be rejected with `ErrTxTypeNotSupported` ("fee delegation type not supported") when `is_applepie(n)` is false for the block `n` that contains it; such a block is invalid. From `ApplepieBlock` on, fee delegation follows `B-07`.
Source: core/state_transition.go:267-271 (buyGas)
Source: core/txpool/validation.go:72-74 (pool-side gate, informative)
Observable: state

### 4.4 Boho (`BohoBlock`)

Boho has two independent effects. The first comes from the fork predicate; the second from the `boho` overlay section.

[SNET-CFG-010] From block `BohoBlock` on (inclusive, `Rules.IsBoho`), the precompile set MUST be the Anzeon set plus `P256VERIFY` at address `0x0000000000000000000000000000000000000100`. `P256VERIFY` follows EIP-7951, not the original EIP-7212: every call costs `P256_VERIFY_GAS = 6_900` (§10), not `3_450`, and an input that is not exactly 160 bytes returns empty output without error. The full behaviour is specified in `B-07` SNET-TX-089.
Source: core/vm/contracts.go:141-155 (PrecompiledContractsBoho), 1227-1251 (p256Verify)
Source: core/vm/evm.go:40-45 (precompile selection)
Source: params/protocol_params.go:171 (P256VerifyGas)
Observable: state

[SNET-CFG-011] If `BohoBlock`, `Boho` and `Boho.SystemContracts` are all present, the system contracts listed in `Boho.SystemContracts` MUST be upgraded at block `BohoBlock` as specified in §7 and `B-06` §2 (at genesis via `B-02` §4 when `BohoBlock == 0`). On both presets this replaces the code of `GovMinter` (`0x…1003`) by version `v2` and changes no storage.
Source: params/config.go:1108-1126 (CollectUpgrades)
Source: params/config.go:144-151, 264-271 (preset overlays)
Observable: state

Implementation note (informative). Because the upgrade is applied after the transactions of block `BohoBlock` (`B-06` §2), transactions in block `BohoBlock` already see `P256VERIFY` but still execute the `GovMinter` v1 code. The v2 code is effective from block `BohoBlock + 1`.

Only the `SystemContracts` part of `Boho` is read. `Boho.WBFT` and `Boho.Init` have no effect.

---

## 5. `AnzeonConfig`

### 5.1 Structure

| Field | JSON | Type | Meaning |
|---|---|---|---|
| `WBFT` | `wbft` | `WBFTConfig` | Base consensus parameters (§6) |
| `Init` | `init` | `WBFTInit` | Initial validators: `validators` (list of addresses, order is significant) and `blsPublicKeys` (list of `0x`-hex strings, same order) |
| `SystemContracts` | `systemContracts` | `SystemContracts` | Genesis system contracts (§5.2) |

`WBFTConfig` fields (all JSON keys lower camel case): `requestTimeoutSeconds` (uint64), `blockPeriodSeconds` (uint64), `epochLength` (uint64), `allowedFutureBlockTime` (uint64, optional), `proposerPolicy` (uint64 or null), `maxRequestTimeoutSeconds` (uint64 or null).

Source: params/config_wbft.go:50-59, 186-193

[SNET-CFG-030] The WBFT configuration (`anzeon.wbft` and each entry of `transitions`) consists only of the six fields above (plus `block` in a transition). A node MUST ignore every other key without error, including the Quorum keys `emptyBlockPeriodSeconds`, `ceil2Nby3Block`, `2FPlus1Enabled`, `validatorSelectionMode` and top-level `qbft`, `ibft` or `istanbul` sections; none of them changes the quorum, the block period or the validator set, and there is no empty-block period (a block is produced in every view whether or not there are transactions, `A-06`). The reference reads the chain configuration with Go `encoding/json`, so key names match case-insensitively (`blockperiodseconds` sets `blockPeriodSeconds`) and, when a key occurs twice, the last occurrence wins; a node MUST read the keys of `ChainConfig` in the same way.
Source: params/config_wbft.go:186-198 (WBFTConfig, Transition)
Source: consensus/wbft/config.go:105-114 (Config has no empty-block field)
Source: eth/ethconfig/config.go:213-262 (SetConfigFromChainConfig copies only these fields)
Observable: header

Implementation note (informative). Executed with the reference code: a chain configuration with `emptyBlockPeriodSeconds`, `ceil2Nby3Block`, `2FPlus1Enabled`, `validatorselectionmode` inside `anzeon.wbft`, a top-level `qbft` section and `emptyBlockPeriodSeconds` in a transition decodes without error and keeps only the six fields; `{"blockperiodseconds": 5, "EPOCHLENGTH": 30, "epochLength": 40}` gives block period 5 and epoch length 40.

### 5.2 System contracts

`SystemContracts` has five optional members: `govValidator`, `nativeCoinAdapter`, `govMinter`, `govMasterMinter`, `govCouncil`. Each is a `SystemContract {address, version, params}` where `params` is a map from string to string. The storage layout written from `params` is specified in `B-04`; the genesis procedure in `B-02`.

| Member | Default address | Registered versions |
|---|---|---|
| `nativeCoinAdapter` | `0x0000000000000000000000000000000000001000` | `v1` |
| `govValidator` | `0x0000000000000000000000000000000000001001` | `v1` |
| `govMasterMinter` | `0x0000000000000000000000000000000000001002` | `v1` |
| `govMinter` | `0x0000000000000000000000000000000000001003` | `v1`, `v2` |
| `govCouncil` | `0x0000000000000000000000000000000000001004` | `v1` |

Source: params/config_wbft.go:31-46, 146-164
Source: systemcontracts/contracts.go:25-76 (registered bytecode per version)

### 5.3 Startup validity of the Anzeon section

[SNET-CFG-012] A node MUST refuse to initialise a genesis block, and MUST refuse to start on a stored configuration, whose `Anzeon` section violates any of the following, checked in this order: (1) `init` present; (2) `init.blsPublicKeys` non-empty; (3) `init.validators` non-empty; (4) `len(validators) == len(blsPublicKeys)`; (5) `systemContracts` present; (6) `govValidator`, `nativeCoinAdapter`, `govMasterMinter`, `govMinter`, `govCouncil` all present; (7) every present member's `version` is registered for its contract type (table of §5.2); (8) `wbft` present; (9) `requestTimeoutSeconds > 0`; (10) `blockPeriodSeconds > 0`; (11) `epochLength >= 2`.
Source: params/config_wbft.go:76-130 (CheckValidity)
Source: systemcontracts/systemcontracts.go:32-60 (checkSystemContractVersions)
Source: core/genesis.go:231-238 (validateAnzeonGenesisConfig), core/blockchain.go:286-290 (on start)

The check does not cover `Boho`, `Transitions`, `params` contents (validated later during genesis construction, `B-02`), the format of BLS keys, or the Ethereum fork fields.

### 5.4 Per-fork overlays

A per-fork overlay is an `AnzeonConfig` stored under the fork's name (currently only `boho`). Only its `systemContracts` member is used, through `CollectUpgrades` (§7.1).

[SNET-CFG-013] A per-fork overlay MUST NOT change any consensus parameter; only the contracts named in its `systemContracts` are affected, and each named contract replaces the whole `SystemContract` entry (address, version, params) from the overlay's block onwards (§7.2).
Source: params/config.go:1108-1126 (CollectUpgrades reads only Boho.SystemContracts)
Source: consensus/wbft/config.go:124-151 (GetSystemContracts replaces whole entries)

---

## 6. Consensus parameters and transitions

### 6.1 Base configuration

The consensus engine does not start from `wbft.DefaultConfig`. It starts from a zero-valued `Config` and copies the non-zero fields of `cfg.Anzeon.WBFT`.

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

[SNET-CFG-014] The base consensus configuration MUST be derived as `base_wbft_config` above. In particular `request_timeout = requestTimeoutSeconds * 1000`, and a field that is zero or absent in `anzeon.wbft` keeps the zero value (it does not fall back to `wbft.DefaultConfig`).
Source: eth/ethconfig/config.go:213-233 (SetConfigFromChainConfig)
Source: consensus/wbft/config.go:105-122 (Config, DefaultConfig not used here)

### 6.2 Transitions

A `Transition` is `{block, <WBFTConfig fields flattened>}`. In JSON the WBFT fields sit next to `block`, for example `{"block": 1000, "epochLength": 100}`.

```python
def sorted_transitions(ts):
    # ascending by block; entries with an absent block go last.
    return sort(ts, key=lambda t: (t.block is None, t.block))

def config_at(n) -> WbftConfig:           # Part A name; reference: Config.GetConfig
    c = copy(base)
    for t in base.transitions:            # already sorted
        if t.block > n:
            break
        if t.requestTimeoutSeconds != 0:  c.request_timeout = t.requestTimeoutSeconds * 1000
        if t.blockPeriodSeconds != 0:     c.block_period = t.blockPeriodSeconds
        if t.epochLength != 0:            c.epoch = t.epochLength
        if t.proposerPolicy is not None:  c.proposer_policy = t.proposerPolicy
        if t.maxRequestTimeoutSeconds is not None:
            c.max_request_timeout_seconds = t.maxRequestTimeoutSeconds
        # allowedFutureBlockTime in a transition is ignored
    return c
```

[SNET-CFG-015] `config_at(n)` MUST apply, in ascending `block` order, every transition with `block <= n`, overriding exactly the fields shown above when they are non-zero (integers) or present (`proposerPolicy`, `maxRequestTimeoutSeconds`). `allowedFutureBlockTime` in a transition MUST be ignored.
Source: consensus/wbft/config.go:161-192 (GetConfig, getTransitionValue)
Source: eth/ethconfig/config.go:245-262 (copy and sort)
Observable: header

[SNET-CFG-016] A node MUST apply transitions to the epoch rule separately from `config_at`: a transition with `epochLength != 0` restarts epoch counting at its `block`, and that block is an epoch block; a transition with `epochLength == 0` does not affect epoch boundaries. The algorithm is specified in `A-04` (`is_epoch_block`).
Source: consensus/wbft/engine/engine.go:1073-1090 (IsEpochBlockNumber)
Observable: header

---

## 7. System-contract upgrades

### 7.1 Collecting upgrades

```python
def collect_upgrades(cfg) -> list[Upgrade]:
    ups = []
    # registry in ascending block order; new forks are appended after Boho
    if cfg.BohoBlock is not None and cfg.Boho is not None and cfg.Boho.SystemContracts is not None:
        ups.append(Upgrade(block=cfg.BohoBlock, system_contracts=cfg.Boho.SystemContracts))
    return ups

upgrade_list = [Upgrade(0, cfg.Anzeon.SystemContracts)] + collect_upgrades(cfg)
```

[SNET-CFG-018] The upgrade list MUST be the genesis entry `(0, Anzeon.SystemContracts)` followed by `collect_upgrades(cfg)`. The list MUST be in non-decreasing block order; the reference implementation relies on this order and does not sort it.
Source: eth/ethconfig/config.go:264-271
Source: params/config.go:1089-1126 (CollectUpgrades and its ordering contract)

### 7.2 Contracts in force at a height

```python
def system_contracts_at(n) -> SystemContracts:
    sc = SystemContracts()                  # all members absent
    for u in upgrade_list:
        if u.block > n:
            break
        for name in MEMBERS:                # govValidator, nativeCoinAdapter, govMinter, govMasterMinter, govCouncil
            if getattr(u.system_contracts, name) is not None:
                setattr(sc, name, getattr(u.system_contracts, name))
    return sc

def upgrades_at(n) -> list[Upgrade]:        # entries whose block is exactly n
    return [u for u in upgrade_list if u.block == n]
```

[SNET-CFG-019] Wherever this specification uses the contract "in force at height `n`", it MUST be resolved with `system_contracts_at(n)` (inclusive of upgrades at `n`).
Source: consensus/wbft/config.go:124-159 (GetSystemContracts, getSystemContractsValue)

[SNET-CFG-020] The reference implementation does not use `system_contracts_at(n)` everywhere. The following reads MUST use `genesis_system_contracts` (the genesis `Anzeon.SystemContracts` entry) regardless of height: the `GovValidator` address from which the header gas tip is read (`B-06` §5); the `NativeCoinAdapter` address that emits native `Transfer` logs and that may call `NativeCoinManager`; the `GovCouncil` address that may call `AccountManager`. The `GovValidator` address from which candidates and BLS keys are read MUST use `system_contracts_at(n)` (`B-08`).
Source: consensus/wbft/engine/engine.go:622-645 (getGasTip: genesis address)
Source: consensus/wbft/engine/engine.go:607-612, 875 (candidates: GetSystemContracts)
Source: core/vm/evm.go:606; core/vm/native_manager.go:462-475

Implementation note (informative). With the current fork registry the two resolutions give the same addresses, because `Boho` changes only the version of `GovMinter`. They diverge only if a future overlay moves a contract.

[SNET-CFG-021] Upgrades with `block == 0` MUST be applied during genesis construction (`B-02` §4); upgrades with `block > 0` MUST be applied during finalization of block `block` (`B-06` §2). A contract whose upgrade entry has version `v1` and a present (non-null) `params` map, even an empty one, also receives initial storage; any other version changes code only.
Source: core/genesis.go:757-767 (block 0 overlays)
Source: consensus/wbft/config.go:199-225 (GetSystemContractsStateTransition)
Source: systemcontracts/systemcontracts.go:62-157
Observable: state

---

## 8. Startup ordering and compatibility checks

These checks run when a node opens its database. They do not affect the validity of any block, but they decide which configuration a node ends up using, which in turn decides validity.

[SNET-CFG-022] The fork order check MUST reject a configuration in which, walking the list `homestead, daoFork(optional), eip150, eip155, eip158, byzantium, constantinople, petersburg, istanbul, muirGlacier(optional), berlin, london, arrowGlacier(optional), grayGlacier(optional), applepie(optional), boho(optional), mergeNetsplit(optional), shanghaiTime(optional), cancunTime(optional), pragueTime(optional), verkleTime(optional)` and skipping absent optional entries, (a) a non-optional fork is absent while a later one is present, (b) a block-based fork is scheduled before its predecessor, (c) a timestamp fork is scheduled before its timestamp predecessor, or (d) a block-based fork follows a timestamp fork. Condition (d) can never hold, because every timestamp fork comes after every block-based fork in the list; the reference check for it is unreachable.
Source: params/config.go:1153-1221 (CheckConfigForkOrder)
Source: core/genesis.go:355-357, 584-586 (called on setup and on genesis commit)

[SNET-CFG-023] When a stored configuration exists, the node MUST report a configuration conflict if, for any of Homestead, DAO, EIP-150, EIP-155, EIP-158, Byzantium, Constantinople, Petersburg, Istanbul, Muir Glacier, Berlin, London, Arrow Glacier, Gray Glacier, Applepie, Boho, Merge-netsplit, the stored and new block differ and either one is already active at the current head (a new Petersburg block equal to the stored Constantinople block is not a conflict); if the stored DAO fork is active and the DAO support flag differs; if the stored EIP-158 fork is active and the chain ID differs; likewise for the four timestamp forks against the head timestamp; and if exactly one of the stored and new configurations has an `anzeon` section. The rewind point of a block conflict is `min(stored, new) - 1` over the configured values, or 0 when that minimum is 0; the rewind point of an `anzeon` conflict is 0. The lowest conflict is reported. Differences in `anzeon.wbft`, `anzeon.init`, `anzeon.systemContracts`, `boho` or `transitions` are NOT detected.
Source: params/config.go:1128-1151, 1223-1301 (CheckCompatible, checkCompatible)
Source: params/config.go:1407-1411, 1431-1451, 1475-1482 (Anzeon presence, rewind point)

[SNET-CFG-028] A conflict of SNET-CFG-023 does NOT make the node refuse the new configuration on start. If the head number is not 0 and the block rewind point is not 0, or the head timestamp is not 0 and the time rewind point is not 0, the node MUST rewind the chain to the rewind point, or further back to the nearest block at or below it whose state is available, and then store the new configuration. Otherwise, which includes every `anzeon` conflict and every fork moved to or from block 0, the node MUST store the new configuration without rewinding. Only `gstable init` refuses: when the rewind condition holds it exits with an error and leaves the stored configuration unchanged; when it does not hold it stores the new configuration.
Source: core/genesis.go:377-387 (rewind condition, overwrite)
Source: core/blockchain.go:281-284, 468-477 (ConfigCompatError not fatal; rewind and store)
Source: core/blockchain.go:580-600, 654 (SetHead, setHeadBeyondRoot)
Source: cmd/gstable/chaincmd.go:243-246 (init exits on error)

Implementation note (informative). Executed with the reference code (a chain of 4 blocks, stored Homestead block 2): a new Homestead block 3 rewinds the head to 1 in archive mode and to 0 (the nearest persisted state) in the default mode, and stores the new block; a new Homestead block 0, or a stored block 0 with a new block 2, keeps the head at 4 and stores the new value without an error.

[SNET-CFG-024] When the node starts without an explicit genesis (no `--mainnet`, `--testnet` or genesis file) on a database whose genesis hash is neither the Ethereum mainnet hash nor `StableNetMainnetGenesisHash`, it MUST use the stored configuration as is. This includes the StableNet testnet: the testnet preset in the binary is applied only when `--testnet` is given.
Source: core/genesis.go:364-373 (private-network special case)
Source: core/genesis.go:482-501 (configOrDefault)
Source: cmd/utils/flags.go:1759-1771 (flags select presets)

---

## 9. Unsupported forks

WBFT rejects every header of a Shanghai or Cancun block (`B-03` SNET-BHDR-003, SNET-BHDR-005). A configuration that schedules either fork therefore makes the chain stop at the first block whose timestamp reaches the fork time.

[SNET-CFG-025] A StableNet configuration MUST leave `shanghaiTime`, `cancunTime`, `pragueTime` and `verkleTime` absent. The reference implementation does not reject such a configuration at startup; it rejects the blocks (`B-03`).
Source: consensus/wbft/engine/engine.go:220-229 (verifyHeader)
Source: params/config.go:57-63 (mainnet preset leaves them nil)

Implementation note (informative). "Unsupported" refers to header fields, withdrawals, blob transactions and the beacon-root system call. Several EVM features of those forks are nevertheless part of the Anzeon instruction set (§4.2): `PUSH0`, initcode metering, transient storage, `MCOPY`, EIP-6780 and EIP-7702. The exact list of enabled and disabled features is SNET-CFG-029.

The reference binary keeps the go-ethereum options `--override.cancun` and `--override.verkle`. They set `cancunTime` or `verkleTime` on top of the loaded configuration, both for `gstable init` and on every start, and the result is stored when it passes the checks of §8.

[SNET-CFG-031] A node MUST NOT let a runtime option set `shanghaiTime`, `cancunTime`, `pragueTime` or `verkleTime` on an Anzeon chain. The reference implementation accepts `--override.cancun` and `--override.verkle`: once every block-numbered fork has passed, the node announces the override time as the next fork in its fork ID (§11.1), and from that time on it rejects every header (`wbft does not support cancun fork`, SNET-BHDR-005) and changes its fork ID hash, so it leaves the network. The overridden value is stored. A later start without the option keeps it when the stored configuration is used as is (SNET-CFG-024); a start that applies a preset (`--mainnet`, `--testnet`, or a StableNet mainnet database) replaces it with the preset value as long as the override time has not been reached.
Source: cmd/utils/flags.go:251-260 (flags), cmd/gstable/config.go:174-181, cmd/gstable/chaincmd.go:213-221 (applied on start and on init)
Source: core/genesis.go:215-228 (applyOverrides), 355-387 (applied, checked and stored)
Source: consensus/wbft/engine/engine.go:227-229 (Cancun header rejected)
Source: core/forkid/forkid.go:242-290 (timestamp forks enter the fork ID)
Observable: network, header

Implementation note (informative). Executed with the reference code (`forkid.NewID` on the preset genesis with `cancunTime = 2_000_000_000`, head time 1000): the mainnet fork ID becomes `0xbfc8373b`, next `2000000000`; the testnet fork ID before Boho stays `0xdea2c682`, next `14408500`, because block-numbered forks are announced first.

---

## 10. Protocol constants used with the configuration

These values are compiled in; they are not part of `ChainConfig` and cannot be changed per network.

| Spec name | Reference name | Value | Used in |
|---|---|---|---|
| `GAS_LIMIT_BOUND_DIVISOR` | `GasLimitBoundDivisor` | `1024` | `B-03` §2.1 |
| `MIN_GAS_LIMIT` | `MinGasLimit` | `5000` | `B-03` §2.1 |
| `MAX_GAS_LIMIT` | `MaxGasLimit` | `2^63 - 1` | `B-03` §2.1 |
| `GENESIS_GAS_LIMIT` | `GenesisGasLimit` | `4_712_388` | `B-02` §2 (genesis `gasLimit == 0`) |
| `ELASTICITY_MULTIPLIER` | `ElasticityMultiplier` | `2` | `B-03` §2.1 (only when the parent is pre-London) |
| `INITIAL_BASE_FEE` | `InitialBaseFee` | `1_000_000_000` | `B-03` §2.3 (first London block) |
| `INCREASING_THRESHOLD` | `IncreasingThreshold` | `20` (%) | `B-03` §2.3 |
| `DECREASING_THRESHOLD` | `DecreasingThreshold` | `6` (%) | `B-03` §2.3 |
| `BASE_FEE_CHANGE_RATE` | `BaseFeeChangeRate` | `2` (%) | `B-03` §2.3 |
| `MIN_BASE_FEE` | `MinBaseFee` | `20_000_000_000_000` | `B-02` §2, `B-03` §2.3 |
| `MAX_BASE_FEE` | `MaxBaseFee` | `20_000_000_000_000_000` (0 disables) | `B-03` §2.3 |
| `INITIAL_GAS_TIP` | `InitialGasTip` | `27_600_000_000_000` | `B-02` §3 |
| `DILIGENCE_DENOMINATOR` | `DiligenceDenominator` | `1_000_000` | `A-04`, `B-06` §3 |
| `DEFAULT_DILIGENCE` | `DefaultDiligence` | `1_900_000` | `B-02` §3 |
| `MAX_CODE_SIZE` | `MaxCodeSize` | `253_952` (EIP-170 value `24_576` not used) | SNET-CFG-007, `B-07` SNET-TX-088 |
| `MAX_INITCODE_SIZE` | `MaxInitCodeSize` | `253_952` (`= MaxCodeSize`; EIP-3860 value `49_152` not used) | SNET-CFG-029 (`CREATE`/`CREATE2` only), `B-07` SNET-TX-088 |
| `P256_VERIFY_GAS` | `P256VerifyGas` | `6_900` (EIP-7951; EIP-7212 value `3_450` not used) | SNET-CFG-010, `B-07` SNET-TX-089 |

[SNET-CFG-026] A conforming node MUST use the constant values in the table above.
Source: params/protocol_params.go:26-29, 128-141, 171
Source: params/config.go:1303-1336 (accessors return the constants)
Source: core/types/istanbul.go:43-46 (DiligenceDenominator, DefaultDiligence)
Observable: header, state

---

## 11. Network presets

go-stablenet ships two presets, selected by `--mainnet` (network id 8282) and `--testnet` (network id 8283). The genesis blocks built from them are given in `B-02` §8. Both presets are normative. Their default bootstrap nodes are listed in `B-09` §12.4.

### 11.1 Fork schedule and consensus parameters

| Parameter | Mainnet (8282) | Testnet (8283) |
|---|---|---|
| `chainId` | `8282` | `8283` |
| Homestead .. London (11 forks incl. Muir Glacier) | all `0` | all `0` |
| `arrowGlacierBlock`, `grayGlacierBlock`, `mergeNetsplitBlock` | absent | absent |
| `shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime` | absent | absent |
| `applepieBlock` | `0` | `0` |
| `bohoBlock` | `0` | `14_408_500` |
| `anzeon.wbft.epochLength` | `10` | `140` |
| `anzeon.wbft.blockPeriodSeconds` | `1` | `1` |
| `anzeon.wbft.requestTimeoutSeconds` | `2` | `2` |
| `anzeon.wbft.proposerPolicy` | `0` (round-robin) | `0` (round-robin) |
| `anzeon.wbft.maxRequestTimeoutSeconds`, `allowedFutureBlockTime` | absent | absent |
| `transitions` | none | none |
| `boho.systemContracts` | `govMinter {0x…1003, v2}` | `govMinter {0x…1003, v2}` |
| Network ID (default of the preset flag) | `8282` | `8283` |
| Fork ID (EIP-2124) | `0xbfc8373b`, next `0` | `0xdea2c682`, next `14408500` while head < `14_408_500`; `0x7b44b412`, next `0` from `14_408_500` on |
| Genesis hash constant | `0xf192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b` | `0x2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490` |

Source: params/config.go:29-32, 44-152, 155-272
Source: cmd/utils/flags.go:1759-1771

The fork ID is computed as in EIP-2124 from the genesis hash and every top-level `…Block` and `…Time` field of `ChainConfig` that is set, including `applepieBlock` and `bohoBlock`; forks at block 0 are dropped. `anzeon`, `boho` and `transitions` do not enter it. On the mainnet preset every fork is at block 0, so the fork ID is the CRC32 of the genesis hash; on the testnet preset `bohoBlock` is the only fork after genesis. The computation rule (SNET-SYNC-063) and the `Status` handshake that carries the fork ID are in `B-09` §12.1. The values of the table were computed with `forkid.NewID` on the preset genesis.

[SNET-CFG-032] A node that joins network 8282 or 8283 MUST announce the fork ID of the table above for its current head (`B-09` SNET-SYNC-063), and therefore MUST include `applepieBlock` and `bohoBlock` in its EIP-2124 fork list.
Source: core/forkid/forkid.go:75-99, 242-290 (NewID, gatherForks)
Source: params/config.go:44-272 (preset fork blocks)
Observable: network

### 11.2 Initial validators

Mainnet: one validator `0xaa5faa65e9cc0f74a85b6fdfb5f6991f5c094697` with BLS key `0xaec493af8fa358a1c6f05499f2dd712721ade88c477d21b799d38e9b84582b6fbe4f4adc21e1e454bc37522eb3478b9b`. The preset carries the comment "TODO: this is just for test on mainnet", but it is the preset of the operating mainnet and is normative. This is the genesis validator set.

Testnet: seven validators, in this order (the order defines validator indices 0..6 of the first epoch, `B-02` §3):

| Index | Validator | BLS public key |
|---|---|---|
| 0 | `0x9f06600b2c17108662e3840e76bb27c9468eb73d` | `0x96683524c3b7e224f2146a0dbb87593e3dee21b7d97c1b24ef6ed799c977be40d3dff8e45b7fcdb48f7713095d84dd9c` |
| 1 | `0x1aa18ec0b3131171b1b1ddba2dffd81410b30a5a` | `0x80bd166ebfdb29553801dc22f5b83534945cc2a6dacf39d422383cb3041c8afab8cd430ffc29e9297ba2e114efae487f` |
| 2 | `0xe63b413353e1ba4ac99f2bd1892328e2365ec574` | `0xa418ff21040af17cb3f5109fa075c92d771e508e715230413d6548d54114666011b715d2e60dfd4e20661d5327b67d6d` |
| 3 | `0x20f681210071932dbe6387378adf6f26af029f7d` | `0xa529026635cf95fd84a9633c62bedaf2a5999f2d5542164086176e24b0c2256a3daa7b76344b631c0bd344895b3f44b9` |
| 4 | `0x5803c14973690550d6ffc2014b7bffd8005f6021` | `0xb2aa67ceb23d96e4de5dca871dfbda6ba122a3b5a55b3a00547fdda906ab8e4892e98de4b36b541088ac8ff6de7d6a35` |
| 5 | `0x59f6e6add1fbeab316a7b17c9f1966b3655efedb` | `0x88717d8edbabaf65015b656b5a14bc27705bead8a75f81b9bc064b69b7a5e8f5a010d54cdb85b5b3cab16ce5d816062f` |
| 6 | `0x0a8dd92ce7ce53bbf6aed40228f9029bb3f92702` | `0x86949700dc2722f48cd649af74cff788bf17553d19278592ca7be54929d1646d60efd1ea12f9dabd7d0143d9813bfea8` |

Source: params/config.go:73-76, 177-196

### 11.3 System-contract parameters

All five contracts use their default addresses (§5.2) and version `v1` at genesis. `expiry = 604800` (7 days), `memberVersion = 1` and `maxProposals = 3` on every governance contract of both presets.

| Contract | Parameter | Mainnet | Testnet |
|---|---|---|---|
| `govValidator` | `quorum` | `1` | `2` |
| | `members` | the single validator address | 7 member addresses `0x58f13FE4…4D43`, `0xE0E5BDD4…f453`, `0x4053E68d…15dE`, `0x56D97Be0…4bD1`, `0x79495F15…9002`, `0x9B690222…6746`, `0xF7AeCcD8…c833` (full list in source) |
| | `validators` | same as `init.validators` | same as `init.validators` (same order) |
| | `blsPublicKeys` | same as `init.blsPublicKeys` | same as `init.blsPublicKeys` |
| | `gasTip` | `27600000000000` | `27600000000000` |
| `nativeCoinAdapter` | `masterMinter` / `minters` | `0x…1002` / `0x…1003` | `0x…1002` / `0x…1003` |
| | `minterAllowed` | `10000000000000000000000000000` (1e28) | `100000000000000000000000000000` (1e29) |
| | `name` / `symbol` / `decimals` / `currency` | `WKRC` / `WKRC` / `18` / `KRW` | `WKRC` / `WKRC` / `18` / `KRW` |
| `govMasterMinter` | `quorum`, `members` | `1`, the single validator | `2`, the 7 members |
| | `fiatToken` / `minters` | `0x…1000` / `0x…1003` | `0x…1000` / `0x…1003` |
| | `maxMinterAllowance` | 1e28 | 1e29 |
| `govMinter` | `quorum`, `members`, `fiatToken` | `1`, the single validator, `0x…1000` | `2`, the 7 members, `0x…1000` |
| `govCouncil` | `quorum`, `members` | `1`, the single validator | `2`, the 7 members |

`govCouncil` has no `blacklist` or `authorizedAccounts` parameter on either preset.

[SNET-CFG-027] A node that claims to join network 8282 or 8283 MUST use exactly the preset values of §11.1-§11.3 (and the genesis allocation of `B-02` §8).
Source: params/config.go:44-272
Source: core/genesis.go:617-632 (preset genesis blocks)
Observable: header, state
