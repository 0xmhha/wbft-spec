# B-04 System contracts

- Part: B (StableNet block validity)
- Area code: `SYS`
- Status: draft
- Reference implementation: go-stablenet `740526d03`

This chapter defines the system contracts as a WBFT node sees them: which contracts exist, at which addresses, with which bytecode per fork; how their code and storage are installed at genesis and replaced at upgrade blocks; the exact storage layout of the slots the node reads directly; and the account `Extra` bit field that holds the blacklist and authorized flags. The governance meaning of these contracts (members, proposals, votes, quorum, `configureValidator`, gas-tip proposals, `GovCouncil`, minters) is in `B-05`. Which of these values feed the consensus protocol, and from which state, is in `B-08`.

---

## 1. Scope and reading guide (informative)

The node never calls a system contract through the EVM to make a consensus decision. It reads storage slots of the `GovValidator` account directly from the state trie and computes the slot keys itself, and it reads bit 63 of an account's `Extra` field. A conforming implementation therefore has to reproduce three things bit for bit:

1. the code and storage that exist at the system-contract addresses (because they determine the state root),
2. the Solidity storage layout of the variables that the node reads (because the node computes the slot keys), and
3. the algorithms that turn those slots into values (an ordered address list, a byte string, an integer).

Section 2 lists the contracts. Sections 3 and 4 define installation and upgrade. Section 5 defines the general Solidity storage encoding used by all readers. Sections 6 to 8 give the layouts of `EnumerableSet`, `GovBase` and `GovValidator`. Section 9 is a worked example on the StableNet testnet genesis. Section 10 lists the other contracts' layouts for completeness and, in section 10.1, the storage written by their genesis initializers. Sections 11 and 12 define the account `Extra` bits, the native managers that write them, and the BLS proof-of-possession precompile that `GovValidator` uses. Section 13 states the security properties of the token contract that follow from its bytecode.

Unless stated otherwise, "slot `0x33`" means the 32-byte storage key `0x000…0033`, and storage values are 32-byte big-endian words.

---

## 2. Registry of system contracts

### 2.1 Configuration objects

A system contract is configured by a triple `SystemContract{Address, Version, Params}` (`Params` is a string-to-string map). The set of system contracts is `SystemContracts{GovValidator, NativeCoinAdapter, GovMinter, GovMasterMinter, GovCouncil}`; each field is a pointer and may be absent. The Anzeon section of the chain configuration holds the genesis set (`anzeon.systemContracts`); an upgrade holds a partial set (`Upgrade{Block, SystemContracts}`).

[SNET-SYS-001] A chain with Anzeon enabled MUST configure all five system contracts in `anzeon.systemContracts` (`GovValidator`, `NativeCoinAdapter`, `GovMasterMinter`, `GovMinter`, `GovCouncil`); a configuration that omits any of them is invalid and the node MUST refuse to start.
Source: params/config_wbft.go:95-115 (AnzeonConfig.CheckValidity)
Source: params/config_wbft.go:146-172 (SystemContracts, SystemContract)

[SNET-SYS-002] The `Version` of every contract in `anzeon.systemContracts` MUST be a version for which bytecode is registered (section 2.3); otherwise the configuration is invalid.
Source: systemcontracts/systemcontracts.go:32-60 (checkSystemContractVersions)
Source: params/config_wbft.go:113-115

### 2.2 Contract list and default addresses

The table lists the contracts, their default addresses in the built-in network presets, and the versions for which bytecode exists. Addresses are configuration, not constants: a genesis file MAY place a contract elsewhere, and every requirement in this chapter refers to the configured address.

| Contract | Default address | Versions | Bytecode source | Read by the node at run time |
|---|---|---|---|---|
| `NativeCoinAdapter` | `0x0000000000000000000000000000000000001000` | v1 | `artifacts/v1/NativeCoinAdapter` | no (address used as the only permitted caller of the coin manager, section 12.2) |
| `GovValidator` | `0x0000000000000000000000000000000000001001` | v1 | `artifacts/v1/GovValidator` | yes: candidate set, BLS keys, gas tip (sections 6, 8; `B-08`) |
| `GovMasterMinter` | `0x0000000000000000000000000000000000001002` | v1 | `artifacts/v1/GovMasterMinter` | no |
| `GovMinter` | `0x0000000000000000000000000000000000001003` | v1, v2 | `artifacts/v1/GovMinter`, `artifacts/v2/GovMinter` | no |
| `GovCouncil` | `0x0000000000000000000000000000000000001004` | v1 | `artifacts/v1/GovCouncil` | no (address used as the only permitted caller of the account manager, section 12.1) |

Native (Go-implemented) contracts and precompiles that the system contracts call:

| Name | Address | Kind | Defined in |
|---|---|---|---|
| BLS PoP verifier | `0x0000000000000000000000000000000000b00001` | precompile | section 12.3 |
| `NativeCoinManager` | `0x0000000000000000000000000000000000b00002` | native manager | section 12.2 |
| `AccountManager` | `0x0000000000000000000000000000000000b00003` | native manager | section 12.1 |

[SNET-SYS-003] The built-in StableNet mainnet (chain id 8282) and testnet (chain id 8283) presets MUST use the default addresses above for all five system contracts, and version `v1` for all five at genesis.
Source: params/config_wbft.go:31-45 (Default*Address, Default*Version)
Source: params/config.go:77-140 (mainnet preset), params/config.go:197-260 (testnet preset)
Observable: state

The native addresses are fixed constants, not configuration.

[SNET-SYS-004] The BLS PoP precompile MUST be at `0x…b00001`, the coin manager at `0x…b00002`, and the account manager at `0x…b00003`, for every block where Anzeon rules are active.
Source: params/protocol_params.go:208 (BLSPoPPrecompileAddress), params/protocol_params.go:219-220
Source: core/vm/contracts.go:127-139 (PrecompiledContractsAnzeon), core/vm/contracts.go:141-154 (PrecompiledContractsBoho)
Source: core/vm/native_manager.go:95-109 (NativeManagerContractsAnzeon), core/vm/evm.go:62-72

### 2.3 Registered bytecode

The bytecode is the deployed (runtime) code, stored as hex text in `systemcontracts/artifacts/` and embedded into the binary. It is installed directly as account code; no constructor runs.

[SNET-SYS-005] The code installed for a system contract of a given type and version MUST be byte-identical to the registered artifact. The registered artifacts at the reference commit are:

| Type, version | Code length (bytes) | `keccak256(code)` |
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

Implementation note (informative). The hashes above were computed from the artifact files at the reference commit. The Solidity sources in `systemcontracts/solidity/` are the documented origin of these artifacts, but this specification binds to the artifacts, not to a recompilation (section 2.4).

### 2.4 Reproducible build (informative)

The normative content is the artifact bytes of SNET-SYS-005. This section records what is needed to rebuild them from the Solidity sources; nothing in it is a conformance requirement.

- Compiler: solc `0.8.14`, fetched by `systemcontracts/compile` (`compile/compiler/compiler.go:37`).
- Options: `--optimize` with otherwise default settings (200 optimizer runs, the compiler's default EVM version); the artifacts are the `bin-runtime` output (`compile/compiler/compiler.go:48-55`, `compile/main.go:37-73`).
- OpenZeppelin: two git submodules (`.gitmodules`). `systemcontracts/solidity/openzeppelin/contracts` is pinned to `openzeppelin-contracts` commit `d4fb3a89f9d0a39c7ee6f2601d33ffbf30085322`, which is tag `v4.6.0`. `systemcontracts/solidity/openzeppelin/contracts-upgradeable` is pinned to `openzeppelin-contracts-upgradeable` commit `6b9807b0639e1dd75e07fa062e9432eb3f35dd8c`, which is tag `v4.7.0` (commits from `git ls-tree HEAD systemcontracts/solidity/openzeppelin/`, tags from `git ls-remote` of the two upstream repositories). The import prefixes resolve through `systemcontracts/solidity/remappings.txt`.
- The artifacts take only `EnumerableSet`, `SafeMath`, `IERC20` and `IERC1271` from `openzeppelin-contracts`; no artifact uses `openzeppelin-contracts-upgradeable`.
- The submodules are not checked out in the reference tree, so the tree alone cannot rebuild the artifacts; they must be fetched first.

Recompiling and comparing the bytes with SNET-SYS-005 has not been done. It is an optional check of the vector generator (`A-11`).

---

## 3. Installation at genesis

### 3.1 Transition objects

Installation and upgrade are both expressed as a *state transition*: a list of code writes `CodeParam{Address, Code}` and a list of storage writes `StateParam{Address, Key, Value}`.

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

[SNET-SYS-010] For each configured contract, the transition MUST contain one code write of the registered code for `(type, Version)`. It MUST contain the initializer's storage writes if and only if `Version == "v1"` and `Params` is present. Any other version is code-only.
Source: systemcontracts/systemcontracts.go:62-157 (GetSystemContractsTransition)

The genesis-dependent values and parameter validation of the initializers are specified in `B-02` §4.3-§4.4 (gas tip, native-coin total supply, `GovCouncil` sets); the governance meaning of the parameters is in `B-05`. The storage that `initializeValidator` produces is specified in section 8.3 because it defines the layout the node reads; the storage written by the `NativeCoinAdapter`, `GovMinter` and `GovMasterMinter` initializers is specified in section 10.1, and the layouts of the other contracts are in section 10 (informative).

### 3.2 Applying the genesis transition

[SNET-SYS-011] When building the Anzeon genesis state, the node MUST compute the transition from `anzeon.systemContracts` and, for each code write, replace the genesis allocation entry at that address with an account that has the registered code, zero balance, zero nonce, zero `Extra`, and empty storage; it MUST then apply every storage write of the transition to that allocation.
Source: core/genesis.go:734-756 (InjectContracts, baseline part)
Observable: state

Consequence (normative, follows from SNET-SYS-011): any balance, nonce, `Extra` bits or storage that the genesis file itself assigns to a system-contract address are discarded.

[SNET-SYS-012] After the baseline transition, the node MUST apply every upgrade whose `Block` is 0 (section 4.1), in registry order, as an *overlay*: for each code write, replace only the code of the existing allocation entry (keeping balance, nonce, `Extra` and storage; create a fresh entry with zero balance and empty storage if the address has none), then apply the upgrade's storage writes.
Source: core/genesis.go:758-768 (InjectContracts, block-0 upgrades), core/genesis.go:771-797 (applyUpgradeOverlay)
Observable: state

Example (informative). The mainnet preset sets `BohoBlock = 0` with `GovMinter` v2. Its genesis therefore holds `GovMinter` v2 code at `0x…1003` with the storage written by the v1 initializer.

[SNET-SYS-013] Accounts in the genesis allocation MUST NOT have any `Extra` bit set other than bits 63 and 62 (section 11); a genesis with another bit set is invalid.
Source: core/genesis.go:252-262 (initializeAnzeonGenesis, ValidateExtra), core/types/state_account_extra.go:100-108
Observable: state

---

## 4. Upgrades after genesis

### 4.1 The upgrade list

The node keeps one ordered list of upgrades. Its first entry is the Anzeon baseline at block 0; the following entries come from the hard-fork registry `ChainConfig.CollectUpgrades()`, which at the reference commit has exactly one entry.

| Name | Block | Contents | Presets |
|---|---|---|---|
| Anzeon baseline | 0 | `anzeon.systemContracts` (all five, v1) | all |
| Boho | `bohoBlock` | `boho.systemContracts` (in the presets: `GovMinter{0x…1003, v2, Params: none}`) | mainnet `bohoBlock = 0`, testnet `bohoBlock = 14408500` |

This is the upgrade list of `B-01` §7.1 (`upgrade_list`, built with `collect_upgrades`), written with the chain configuration as an explicit argument.

```python
def upgrade_list(chain_config) -> list[Upgrade]:
    ups = [Upgrade(block=0, contracts=chain_config.anzeon.systemContracts)]
    if chain_config.bohoBlock is not None and chain_config.boho is not None \
            and chain_config.boho.systemContracts is not None:
        ups.append(Upgrade(block=chain_config.bohoBlock, contracts=chain_config.boho.systemContracts))
    return ups   # ascending by block; the fork-order check at start-up enforces this
```

[SNET-SYS-020] `B-01` [SNET-CFG-018] applies when the node assembles the upgrade list at start-up (`upgrade_list` above), before any read of `system_contracts_at` or any upgrade step of section 4.2: the list MUST be the one that SNET-CFG-018 defines. With the hard-fork registry of the reference commit, that list is the Anzeon baseline at block 0, followed by the Boho entry if and only if `bohoBlock` is set and `boho.systemContracts` is present.
Source: eth/ethconfig/config.go:264-271 (SystemContractUpgrades assembly)
Source: params/config.go:1089-1126 (CollectUpgrades)
Source: params/config.go:144-151, params/config.go:264-271 (Boho presets), params/config.go:65, params/config.go:169 (BohoBlock)

### 4.2 Applying an upgrade at block N > 0

[SNET-SYS-021] During finalization of block `N > 0`, before any other finalization step (base-fee distribution, epoch processing, gas-tip verification, state-root computation), the node MUST compute the transition of every upgrade whose `Block == N`, in list order, and apply it to the block's post-transaction state: first all code writes (replace code only), then all storage writes.
Source: consensus/wbft/engine/engine.go:929-938 (processFinalize, first step)
Source: consensus/wbft/config.go:199-225 (GetSystemContractsStateTransition)
Observable: state

[SNET-SYS-022] Transactions included in the upgrade block `N` MUST execute against the code that was in effect before `N`; the new code applies from block `N + 1` (and to finalization steps of `N` that follow the upgrade).
Source: core/state_processor.go:89-102 (transactions are applied before Finalize)
Source: consensus/wbft/engine/engine.go:929-938
Observable: state

[SNET-SYS-024] If computing the transition of an upgrade fails (unregistered version, invalid parameters), finalization of block `N` MUST fail, so no block `N` is valid. The node performs no earlier check of upgrade entries.
Source: consensus/wbft/engine/engine.go:930-931, consensus/wbft/config.go:207-218
Source: params/config_wbft.go:113 (only `anzeon.systemContracts` is version-checked)

Implementation note (informative). Upgrade code replacement is safe for storage only if the new code's layout is a superset of the old one. `GovMinter` v2 appends one variable (`refundableBalance`, slot `0x3d`) after the v1 variables and keeps all v1 slots (section 10).

### 4.3 Address resolution

The effective set of system contracts at block `n` is obtained by merging the upgrade list, field by field: a later upgrade that names a contract replaces the whole `SystemContract` entry (address, version, params) of that contract.

This is `system_contracts_at(n)` of `B-01` §7.2 with the upgrade list passed explicitly.

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

[SNET-SYS-025] `B-01` [SNET-CFG-019] applies to every reader in the table below that uses the upgrade-aware address: the address of a system contract at block `n` MUST be resolved as SNET-CFG-019 defines, which is the `Address` of that contract in `system_contracts_at(n)`.
Source: consensus/wbft/config.go:124-159 (GetSystemContracts, getSystemContractsValue)

Not every reader uses the upgrade-aware address. At the reference commit the readers resolve addresses as follows.

| Reader | What it reads | Address used | Source |
|---|---|---|---|
| Epoch computation: candidate list | `GovValidator` validator set | upgrade-aware, at the epoch block number | consensus/wbft/engine/engine.go:607-612, :806 |
| Epoch computation: BLS keys | `GovValidator.validatorToBlsKey` | upgrade-aware, at the epoch block number | consensus/wbft/engine/engine.go:875, :894 |
| Header gas tip (proposal and verification) | `GovValidator.gasTip` | genesis (`anzeon.systemContracts.GovValidator.Address`) | consensus/wbft/engine/engine.go:639-640 |
| Miner / txpool gas tip | `GovValidator.gasTip` | genesis | miner/worker.go:1209-1218 |
| Coin manager caller check | caller address | genesis `NativeCoinAdapter` | core/vm/native_manager.go:463-468 |
| Account manager caller check | caller address | genesis `GovCouncil` | core/vm/native_manager.go:470-475 |
| Native `Transfer` log | log address | genesis `NativeCoinAdapter` | core/vm/evm.go:590-611 |

---

## 5. Solidity storage encoding used by the readers

This section restates the Solidity storage rules that the node's readers depend on. `pad32(x)` is `x` left-padded with zero bytes to 32 bytes; `u256(i)` is the 32-byte big-endian encoding of integer `i`; `keccak256` is Keccak-256 (`A-02`). Slot arithmetic is modulo 2^256.

### 5.1 Value types and packing

A value-type state variable occupies the next free slot; variables smaller than 32 bytes are packed into the same slot in declaration order, starting from the least significant (right-most) byte. An `address` is stored in the low 20 bytes. A `bool` is 1 byte. A `uint32` is 4 bytes.

### 5.2 Mappings

```python
def mapping_slot(base: int, key: bytes, key_is_value_type: bool) -> bytes32:
    # value-type key (address, uintN, bytes32): padded to 32 bytes
    # bytes/string key: raw bytes, not padded
    k = pad32(key) if key_is_value_type else key
    return keccak256(k + u256(base))
```

[SNET-SYS-030] A reader MUST compute the slot of `mapping(K => V)` entry `key` at base slot `base` as `keccak256(pad32(key) ‖ u256(base))` when `K` is a value type (addresses are the 20-byte address left-padded), and as `keccak256(key ‖ u256(base))` when `K` is `bytes` or `string`.
Source: systemcontracts/stateutil.go:31-38 (CalculateMappingSlot)
Source: systemcontracts/solidity/v1/GovValidator.sol:43-46

Implementation note (informative). The reference `CalculateMappingSlot` left-pads every key with `LeftPadBytes(key, 32)`, which leaves keys of 32 bytes or more unchanged. For the only `bytes` key the node uses (a 48-byte BLS public key, `blsKeyToValidator`) this equals the Solidity rule. For a `bytes` key shorter than 32 bytes it would not; no reader uses such a key.

A nested mapping `mapping(A => mapping(B => V))` entry `[a][b]` is at `mapping_slot(mapping_slot(base, a), b)`, where the inner base is the 32-byte outer slot interpreted as an integer.

### 5.3 Dynamic arrays

```python
def array_length_slot(base: int) -> int:
    return base

def array_element_slot(base: int, i: int) -> int:
    # one element per slot for 32-byte and address elements
    return (int(keccak256(u256(base))) + i) % 2**256
```

[SNET-SYS-031] A reader MUST read the length of a dynamic array at base slot `base` from slot `base`, and element `i` (for elements of 32 bytes or of address type) from slot `keccak256(u256(base)) + i`.
Source: systemcontracts/stateutil.go:40-50 (CalculateDynamicSlot)

### 5.4 `bytes` and `string`

A `bytes` or `string` variable with base slot `p` is stored in one of two forms, distinguished by the least significant bit of the word at `p`:

- short form (length `L ≤ 31`): the word at `p` holds the data left-aligned in bytes `0..L-1` and `2·L` in byte 31; the lowest bit is 0.
- long form (`L ≥ 32`): the word at `p` holds `2·L + 1` as an integer; the data is in `ceil(L/32)` consecutive slots starting at `keccak256(u256(p))`, left-aligned, with the unused tail of the last slot zero.

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

[SNET-SYS-032] A reader of a `bytes` value MUST implement `read_bytes` above for every word that the section 5.4 writer or the contract can produce (short form with `L ≤ 31`, long form with `L < 2^63`). In particular: an all-zero word at `p` yields the empty string; in the short form the reader MUST return the first `w[31] >> 1` bytes of the word and ignore the rest; in the long form the reader MUST read `max(1, ceil(L/32))` data slots and return the first `L` bytes. For other words the reference behaviour is not a value: a short-form word with `w[31] >> 1 > 32`, or a long-form word whose `L` has low 64 bits above `2^63 − 1` (read as a negative signed integer), makes the reference reader panic; a long-form word whose `L` has low 64 bits equal to 0 yields the empty string after reading one slot.
Source: systemcontracts/stateutil.go:102-149 (GetBytes)
Observable: state

[SNET-SYS-033] A writer that stores a `bytes` value at genesis MUST use the same two forms: for `L ≤ 31`, one word with data left-aligned and `2·L` in the last byte; for `L ≥ 32`, the word `2·L + 1` at `p` and the data in `ceil(L/32)` slots from `keccak256(u256(p))`; for `L = 0`, the zero word.
Source: systemcontracts/stateutil.go:151-187 (VarLenBytesToMultipleHash), systemcontracts/stateutil.go:189-237 (EncodeBytesToSlots)
Source: systemcontracts/gov_validator.go:186-201 (MakeMultipleParam)
Observable: state

---

## 6. `EnumerableSet.AddressSet` layout and iteration order

`GovValidator` keeps its validator addresses in an OpenZeppelin `EnumerableSet.AddressSet` (OpenZeppelin Contracts, `EnumerableSet.sol` "last updated v4.6.0", pinned commit `d4fb3a89`). An `AddressSet` is a struct with one member `Set _inner`, and `Set` is:

```solidity
struct Set {
    bytes32[] _values;                    // slot s
    mapping(bytes32 => uint256) _indexes; // slot s + 1; value = array index + 1, 0 = absent
}
```

Addresses are stored as `bytes32(uint256(uint160(addr)))`, that is, left-padded to 32 bytes.

### 6.1 Layout

For a set declared at slot `s`:

| Item | Slot | Value |
|---|---|---|
| length `L` | `s` | `u256(L)` |
| element `i` (`0 ≤ i < L`) | `keccak256(u256(s)) + i` | `pad32(addr)` |
| position of `addr` | `keccak256(pad32(addr) ‖ u256(s + 1))` | `u256(i + 1)` if `addr` is element `i`, else 0 |

[SNET-SYS-040] A reader MUST obtain the members of an `AddressSet` at slot `s` as the list `[low20(word(keccak256(u256(s)) + i)) for i in 0..L-1]` where `L` is the integer at slot `s` and `low20` takes the low 20 bytes of the word. The list order is the array order and is normative.
Source: systemcontracts/stateutil.go:56-100 (EnumerableSet, Values, HashToAddress)
Source: systemcontracts/gov_validator.go:203-206 (ValidatorList)
Observable: state

[SNET-SYS-041] A reader MUST treat `addr` as a member if and only if the word at `keccak256(pad32(addr) ‖ u256(s + 1))` is non-zero.
Source: systemcontracts/stateutil.go:73-77 (Contains)
Observable: state

### 6.2 How the order changes (informative restatement of the contract, normative for mirrors)

The set's array order is not insertion order in general. The contract changes it only in two ways:

- `add(v)`, if `v` is absent: append `v` at index `L`, set `_indexes[v] = L + 1`, length `L + 1`.
- `remove(v)`, if `v` is at index `k`: if `k ≠ L − 1`, move the last element `w` to index `k` and set `_indexes[w] = k + 1`; then pop the last array slot (the slot at index `L − 1` is set to zero), delete `_indexes[v]`, length `L − 1`.

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

## 7. `GovBase` layout (slots `0x00`–`0x31`)

`GovValidator`, `GovCouncil`, `GovMinter` and `GovMasterMinter` inherit `GovBase`, whose variables occupy slots 0 to 49 (`0x31`), including a 37-slot reserved gap. The node reads none of these slots at run time; the genesis initializer writes several of them (`B-02`). The layout is given so that a mirror or an inspector can read governance state, and so that the start of the derived contracts' own variables (`0x32`) is justified.

| Slot | Variable | Type | Encoding / notes | Written at genesis |
|---|---|---|---|---|
| `0x00` | `proposalExpiry` | `uint256` | integer (seconds) | if `expiry` param |
| `0x01` | `memberVersion` | `uint256` | integer; the Solidity initializer `= 1` never runs because no constructor runs | if `members` param (value = `memberVersion` param) |
| `0x02` | `currentProposalId` | `uint256` | integer | no |
| `0x03` | `__reentrancyGuard` | `uint256` | integer | no |
| `0x04` | `quorum` | `uint32` | low 4 bytes of the word; bytes 0–27 unused | if `quorum` param |
| `0x05` | `members` | `mapping(address => Member)` | `Member{bool isActive; uint32 joinedAt}` packed in one word: byte 31 = `isActive`, bytes 27–30 = `joinedAt` (big-endian) | one entry per member, `isActive = 1`, `joinedAt = 0` |
| `0x06` | `versionedMemberList` | `mapping(uint256 => address[])` | length at `mapping_slot(6, u256(v))`; element `i` at `keccak256(that slot) + i` | for `v = memberVersion` |
| `0x07` | `proposals` | `mapping(uint256 => Proposal)` | struct base `b = mapping_slot(7, u256(id))`: `b+0` actionType, `b+1` memberVersion, `b+2` votedBitmap, `b+3` createdAt, `b+4` executedAt, `b+5` proposer (low 20 bytes) ‖ requiredApprovals (bytes 8–11) ‖ approved (bytes 4–7) ‖ rejected (bytes 0–3), `b+6` status (low byte), `b+7` callData (`bytes`, section 5.4) | no |
| `0x08` | `_memberIndexByVersion` | `mapping(uint256 => mapping(address => uint32))` | index + 1 | for `v = memberVersion` |
| `0x09` | `_quorumByVersion` | `mapping(uint256 => uint32)` | integer | for `v = memberVersion`, if both `members` and a non-zero `quorum` param are given |
| `0x0a` | `proposalExecutionCount` | `mapping(uint256 => uint256)` | integer | no |
| `0x0b` | `memberActiveProposalCount` | `mapping(address => uint256)` | integer | no |
| `0x0c` | `maxActiveProposalsPerMember` | `uint256` | integer | always (default 3) |
| `0x0d`–`0x31` | `__gap` | `uint256[37]` | reserved, zero | no |

In the `proposals` row, byte offsets are counted from the most significant byte of the word (byte 0) to the least significant (byte 31), so `proposer` occupies bytes 12–31.

[SNET-SYS-050] The storage of every contract derived from `GovBase` MUST follow the table above; in particular the first variable of the derived contract MUST be at slot `0x32`.
Source: systemcontracts/solidity/abstracts/GovBase.sol:57-93 (Member, Proposal), systemcontracts/solidity/abstracts/GovBase.sol:129-153 (state variables and gap)
Source: systemcontracts/gov_base.go:38-66 (SLOT_GOV_BASE_*), systemcontracts/gov_base.go:119-133 (Member.ToHash)
Observable: state

---

## 8. `GovValidator` layout (slots `0x32`–`0x39`)

### 8.1 Layout

| Slot | Variable | Type | Encoding | Read by the node at run time |
|---|---|---|---|---|
| `0x32` | `blsPoP` | `address` | low 20 bytes; genesis value `0x…b00001` | no |
| `0x33` | `__validators._inner._values` | `bytes32[]` | `AddressSet`, section 6 (length here, elements at `keccak256(u256(0x33)) + i`) | yes (length, elements) |
| `0x34` | `__validators._inner._indexes` | `mapping(bytes32 => uint256)` | position + 1 | no (written at genesis) |
| `0x35` | `validatorToOperator` | `mapping(address => address)` | low 20 bytes | no |
| `0x36` | `operatorToValidator` | `mapping(address => address)` | low 20 bytes | no |
| `0x37` | `validatorToBlsKey` | `mapping(address => bytes)` | `bytes` at `mapping_slot(0x37, addr)`, section 5.4; 48-byte keys use the long form (`0x61` = 2·48+1 at the head slot, two data slots) | yes |
| `0x38` | `blsKeyToValidator` | `mapping(bytes => address)` | key is the raw 48-byte BLS key, unpadded | no |
| `0x39` | `gasTip` | `uint256` | integer (wei) | yes |

[SNET-SYS-060] The `GovValidator` storage MUST follow the table above.
Source: systemcontracts/solidity/v1/GovValidator.sol:40-47 (state variables with slot comments)
Source: systemcontracts/gov_validator.go:28-42 (SLOT_VALIDATOR_*)
Observable: state

Implementation note (informative). No storage-layout artifact (`solc --storage-layout`) is committed. The slot numbers were checked in three ways: by the Solidity layout rules applied to `GovBase.sol` (13 variables in slots 0–12, a 37-slot gap to slot 49) and `GovValidator.sol`; against the Go constants; and by executing the embedded `GovValidator` v1 bytecode in the reference EVM and comparing its `validatorList()`, `validatorToBlsKey(addr)` and `gasTip()` results with the slot readers after genesis and after `configureValidator` calls (section 9.3).

### 8.2 Reader functions

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

[SNET-SYS-061] `validator_list` MUST return the members of the `AddressSet` at slot `0x33` in array order (SNET-SYS-040). If the account has no storage (for example, no contract at `gv`), it MUST return the empty list; it MUST NOT fail.
Source: systemcontracts/gov_validator.go:203-206, systemcontracts/stateutil.go:69-86
Observable: state

[SNET-SYS-062] `bls_public_key` MUST return `read_bytes` of `mapping_slot(0x37, validator)`. It MUST return the empty string for an address without a key; it MUST NOT check the length of a non-empty result.
Source: systemcontracts/gov_validator.go:208-210, systemcontracts/stateutil.go:103-149
Observable: state

### 8.3 Storage written by the genesis initializer

`initializeValidator` first runs the `GovBase` initializer (section 7), then writes the slots below. The parameter names are the keys of `GovValidator.Params`. Validation of the parameters is in `B-02`.

1. slot `0x32` ← `pad32(0x…b00001)`.
2. slot `0x39` ← `u256(abs(g) mod 2^256)`, where `g` is the parameter `gasTip` parsed as a base-10 integer by Go `big.Int.SetString` (an optional leading sign is accepted) if present, else `InitialGasTip = 27600000000000`. At genesis a negative `g` aborts genesis construction, because the genesis `Extra` cannot encode a negative gas tip (`B-02` SNET-GEN-010). A `g` of `2^256` or more is reduced modulo `2^256` in the slot, while the genesis `Extra` holds the full value.
3. If `validators` is present: let `M` be the list parsed from `members` (comma separated, trimmed, empty items dropped, not deduplicated), `V` from `validators`, `K` from `blsPublicKeys` (hex-decoded). For each `i` in order, skipping any `V[i]` already seen, with `j` the number of validators written so far:
   - `mapping_slot(0x34, V[i])` ← `u256(j + 1)`;
   - `keccak256(u256(0x33)) + j` ← `pad32(V[i])`;
   - `mapping_slot(0x35, V[i])` ← `pad32(M[i])`;
   - `mapping_slot(0x36, M[i])` ← `pad32(V[i])`;
   - `bytes` at `mapping_slot(0x37, V[i])` ← `K[i]` (section 5.4 writer);
   - `keccak256(K[i] ‖ u256(0x38))` ← `pad32(V[i])`;
   and finally slot `0x33` ← `u256(count)` if at least one validator was written.

[SNET-SYS-064] The genesis `GovValidator` storage MUST be exactly the writes above (plus the `GovBase` writes). In particular, the validator set order at genesis MUST be the order of the `validators` parameter with later duplicates removed, and each validator's operator MUST be the member at the same position of the `members` parameter.
Source: systemcontracts/gov_validator.go:52-184 (initializeValidator)
Source: params/protocol_params.go:138 (InitialGasTip)
Observable: state

Implementation note (informative). The genesis initializer does not check that a BLS key is 48 bytes, that it has a valid proof of possession, or that two validators have different keys; the contract checks all three in `configureValidator` (`B-05`). A duplicate key at genesis leaves `blsKeyToValidator[key]` pointing to the later validator.

---

## 9. Worked examples

### 9.1 Slot computations (testnet genesis)

All values below were computed with the reference code and re-computed independently with `keccak256`. `gv = 0x…1001`, `v0 = 0x9f06600b2c17108662e3840e76bb27c9468eb73d` (first testnet validator), `m0 = 0x58f13fe4294652526c4b893b4e4f343b3f184d43` (its operator), `pk0` = `0x96683524…84dd9c` (48 bytes).

| Item | Computation | Slot | Genesis value |
|---|---|---|---|
| validator count | slot `0x33` | `0x…33` | `0x…07` |
| validator element base | `keccak256(u256(0x33))` | `0x82a75bdeeae8604d839476ae9efd8b0e15aa447e21bfd7f41283bb54e22c9a82` | `pad32(v0)` |
| element 6 | base + 6 | `0x82a75bdeeae8604d839476ae9efd8b0e15aa447e21bfd7f41283bb54e22c9a88` | `pad32(0x0a8dd92ce7ce53bbf6aed40228f9029bb3f92702)` |
| `_indexes[v0]` | `keccak256(pad32(v0) ‖ u256(0x34))` | `0xec0327518f50ff4d7959d421f64d0b6e04957388d59098190131c501f34a6367` | `0x…01` |
| `validatorToBlsKey[v0]` head | `keccak256(pad32(v0) ‖ u256(0x37))` | `0xac97b7a74f2ce4afa1a5c3a3246bf361b051d0fccda38d723e050918cdd16cb2` | `0x…61` (long form, 48 bytes) |
| key data 0 | `keccak256(head)` | `0xd44d80d30d239c4a95e1b27b4a03fdd4739ecd5410a0a79c9bdd315cb66c7b53` | `0x96683524c3b7e224f2146a0dbb87593e3dee21b7d97c1b24ef6ed799c977be40` |
| key data 1 | `keccak256(head) + 1` | `0xd44d80d30d239c4a95e1b27b4a03fdd4739ecd5410a0a79c9bdd315cb66c7b54` | `0xd3dff8e45b7fcdb48f7713095d84dd9c00000000000000000000000000000000` |
| `blsKeyToValidator[pk0]` | `keccak256(pk0 ‖ u256(0x38))` (48-byte key, unpadded) | `0x8292702d46cd2a8f2bf69e1697c26f72cd4d34a9f2faaa8a6103c45027637dca` | `pad32(v0)` |
| `validatorToOperator[v0]` | `keccak256(pad32(v0) ‖ u256(0x35))` | `0x9cdd7df4cfbd604045380b9d09961b2818399d3d10fd389d16daf4e44036e0e2` | `pad32(m0)` |
| `operatorToValidator[m0]` | `keccak256(pad32(m0) ‖ u256(0x36))` | `0x7a3fdea8e2b01249181632dbd8bd87108e4c8c03230fcab3ca1b75c084ac0bd8` | `pad32(v0)` |
| `gasTip` | slot `0x39` | `0x…39` | `0x…191a20322000` (27 600 000 000 000) |
| `blsPoP` | slot `0x32` | `0x…32` | `0x…b00001` |
| `members[m0]` | `keccak256(pad32(m0) ‖ u256(5))` | `0x6552c94a3b040b3fd9d247ee0a17428223c0948df2bbdf50b8648526c09cbf95` | `0x…01` (active, joinedAt 0) |
| `versionedMemberList[1]` length | `keccak256(u256(1) ‖ u256(6))` | `0x3e5fec24aa4dc4e5aee2e025e51e1392c72a2500577559fae9665c6d52bd6a31` | `0x…07` |
| `versionedMemberList[1][0]` | `keccak256(previous slot)` | `0x80497882cf9008f7f796a89e5514a7b55bd96eab88ecb66aee4fb0a6fd34811c` | `pad32(m0)` |
| `_memberIndexByVersion[1][m0]` | `keccak256(pad32(m0) ‖ keccak256(u256(1) ‖ u256(8)))` | `0x64bf6b8db42c7b9ab77dbb417d8b6ecce1802b4062189763f959c21e335c551d` | `0x…01` |
| `_quorumByVersion[1]` | `keccak256(u256(1) ‖ u256(9))` | `0x92e85d02570a8092d09a6e3a57665bc3815a2699a4074001bf1ccabf660f5a36` | `0x…02` |
| `quorum` | slot `0x04` | `0x…04` | `0x…02` |
| `proposalExpiry` | slot `0x00` | `0x…00` | `0x…093a80` (604 800) |
| `maxActiveProposalsPerMember` | slot `0x0c` | `0x…0c` | `0x…03` |

The testnet genesis `GovValidator` account holds 86 non-zero storage slots.

### 9.2 Reader results (testnet genesis)

`validator_list(genesis_state, 0x…1001)` returns, in this order:

```
0x9f06600b2c17108662e3840e76bb27c9468eb73d
0x1aa18ec0b3131171b1b1ddba2dffd81410b30a5a
0xe63b413353e1ba4ac99f2bd1892328e2365ec574
0x20f681210071932dbe6387378adf6f26af029f7d
0x5803c14973690550d6ffc2014b7bffd8005f6021
0x59f6e6add1fbeab316a7b17c9f1966b3655efedb
0x0a8dd92ce7ce53bbf6aed40228f9029bb3f92702
```

and `bls_public_key` returns for each the key at the same position of `blsPublicKeys`; `gas_tip` returns 27 600 000 000 000. On the mainnet preset, `validator_list` returns `[0xaa5faa65e9cc0f74a85b6fdfb5f6991f5c094697]`. On a state with no `GovValidator` account, `validator_list` returns `[]`, `bls_public_key` returns `b""`, and `gas_tip` returns 0.

### 9.3 Cross-check against the contract bytecode (informative)

The following procedure was run at the reference commit and passed. It is the recommended conformance test for any reimplementation of the readers or of a mirror (`B-08` section 6).

1. Build a `GovValidator` v1 genesis with four members `op0..op3` and validators `v0..v3` with BLS keys derived from the validator keys.
2. Install code and storage (section 3) in an in-memory state; run the EVM with Anzeon rules.
3. Compare `validatorList()`, `validatorToBlsKey(v)` and `gasTip()` (EVM calls) with `validator_list`, `bls_public_key` and `gas_tip` (slot readers). Result: equal, same order.
4. `op0` calls `configureValidator(n, key_n, pop_n)`. Result: both views return `[v3, v1, v2, n]`; `_indexes[v0]`, the head slot of `validatorToBlsKey[v0]` and its first data slot are zero; the array slot at index 4 is zero; length is 4.
5. `op1` calls `configureValidator(v1, key', pop')` (key change only). Result: `bls_public_key(v1) = key'`; the slot `keccak256(old_key ‖ u256(0x38))` is zero; order unchanged.

---

## 10. Other system contracts (informative)

The node does not read these layouts at run time; the genesis initializers write them (`B-02`, section 10.1), and `B-05` defines their semantics. They are listed so that an inspector can decode them and so that upgrade compatibility can be checked. The table is informative; section 10.1 is normative.

| Contract | Slot | Variable |
|---|---|---|
| `GovCouncil` v1 | `0x00`–`0x31` | `GovBase` |
| | `0x32`, `0x33` | `_currentBlacklist` (`AddressSetLib.AddressSet`: `_values` array at `0x32`, `_positions` mapping at `0x33`, 1-based) |
| | `0x34`, `0x35` | `_currentAuthorizedAccounts` (same structure) |
| | `0x36` | `__accountManager` (genesis value `0x…b00003`) |
| `GovMasterMinter` v1 | `0x32` | `fiatToken` |
| | `0x33` | `maxMinterAllowance` |
| | `0x34` | `isMinter` (`mapping(address => bool)`) |
| | `0x35` | `__minterList` (`address[]`) |
| | `0x36` | `minterIndex` |
| | `0x37` | `emergencyPaused` |
| `GovMinter` v1 | `0x32`–`0x3c` | `fiatToken`, `usedProofHashes`, `depositIdToProposalId`, `executedDepositIds`, `withdrawalIdToProposalId`, `executedWithdrawalIds`, `burnProposals`, `reservedMintAmount`, `mintProposalAmounts`, `burnBalance`, `emergencyPaused` |
| `GovMinter` v2 | `0x32`–`0x3c` | as v1 |
| | `0x3d` | `refundableBalance` (new in v2) |
| `NativeCoinAdapter` v1 | `0x00` | `masterMinter` |
| | `0x01`, `0x02` | `_minters`, `_minterAllowed` |
| | `0x03` | `_DEPRECATED_CACHED_DOMAIN_SEPARATOR` (`bytes32`, never written or read; the separator is computed per call, section 13.2) |
| | `0x04` | `__authorizationStates` (`mapping(address => mapping(bytes32 => bool))`, EIP-3009 nonces used or cancelled, `[authorizer][nonce]`) |
| | `0x05` | `__permitNonces` (`mapping(address => uint256)`, EIP-2612 nonces) |
| | `0x06`, `0x07` | `_coinManager` (`0x…b00002`), `_accountManager` (`0x…b00003`) |
| | `0x08`, `0x09`, `0x0b` | `name`, `symbol`, `currency` (`string`s in the section 5.4 encoding) |
| | `0x0a` | `decimals` (`uint8`, low byte) |
| | `0x0c` | `__allowed` (`mapping(address => mapping(address => uint256))`, `[owner][spender]`) |
| | `0x0d` | `_totalSupply` |

Source: systemcontracts/gov_council.go:55-61, systemcontracts/gov_master_minter.go:34-50, systemcontracts/gov_minter.go:36-58, systemcontracts/coin_adapter.go:13-23
Source: systemcontracts/solidity/v1/GovCouncil.sol:60-66, systemcontracts/solidity/libraries/AddressSetLib.sol:38-48, :120-145
Source: systemcontracts/solidity/v1/GovMinter.sol:100-129, systemcontracts/solidity/v2/GovMinter.sol:102-134
Source: systemcontracts/solidity/abstracts/eip/EIP712Domain.sol:36, systemcontracts/solidity/abstracts/eip/EIP3009.sol:51, systemcontracts/solidity/abstracts/eip/EIP2612.sol:40, systemcontracts/solidity/v1/NativeCoinAdapter.sol:79

The `NativeCoinAdapter` slots `0x03`–`0x05` follow the C3 linearisation of `AbstractFiatToken, Mintable, EIP3009, EIP2612`, in which `EIP712Domain` (inherited by both EIP contracts) precedes `EIP3009`. The comment in `NativeCoinAdapter.sol:56-69`, which places `EIP3009` at `0x03`, `EIP2612` at `0x04` and `EIP712Domain` at `0x05`, is wrong. The table was checked by executing the artifact in the reference EVM: a value written at `mapping_slot(0x05, owner)` is returned by `nonces(owner)`, a value at the `[owner][nonce]` entry of `0x04` by `authorizationState(owner, nonce)`, a value at the `[owner][spender]` entry of `0x0c` by `allowance(owner, spender)`, and a value written at slot `0x03` does not change `DOMAIN_SEPARATOR()`.

The `GovCouncil` sets use the local library `AddressSetLib`, not OpenZeppelin, but with the same array-plus-1-based-position layout and the same swap-and-pop removal. The genesis initializer writes them in ascending byte order of the address.

### 10.1 Genesis initializers of the minting contracts

The initializers below run only for a `v1` entry with `Params` present (SNET-SYS-010). The `GovBase` part of `GovMinter` and `GovMasterMinter` is `B-05` SNET-GOV-160. They use two parsing helpers and two integer parsers of the reference.

```python
def split_and_trim(s: str) -> list[str]:
    # split on ",", strip white space of each item, drop empty items; duplicates are kept
    return [x.strip() for x in s.split(",") if x.strip() != ""]

def hex_to_address(s: str) -> Address:
    # go-ethereum common.HexToAddress: drop an optional "0x"/"0X" prefix, prepend "0" if the
    # length is odd, decode hex pairs up to the first invalid pair (the bytes decoded so far are
    # kept, the rest is ignored, no error), keep the last 20 bytes, left-pad with zeros.
    ...

# set_string(s): Go big.Int.SetString(s, 10): an optional leading "+" or "-", then decimal digits;
#     anything else is an error. The slot value is u256(abs(v) mod 2**256) (common.BigToHash).
# parse_u8(s): Go strconv.ParseUint(s, 10, 8): decimal digits only, 0..255; anything else is an error.
```

[SNET-SYS-091] The `NativeCoinAdapter` initializer MUST produce exactly the storage writes below, and computing the transition MUST fail (so genesis construction or the finalization of the upgrade block fails) on each listed error:
1. `masterMinter` absent or empty: error. Slot `0x00` ← `pad32(hex_to_address(masterMinter))`; a zero result is not rejected.
2. If `minters` is present and non-empty, `M = split_and_trim(minters)`. If `minterAllowed` is present and non-empty, `A = split_and_trim(minterAllowed)`; `len(A) != len(M)` is an error, and an item that `set_string` rejects is an error. For each `i` in order: `mapping_slot(0x01, hex_to_address(M[i]))` ← `u256(1)`, and, if `A` was given, `mapping_slot(0x02, hex_to_address(M[i]))` ← `u256(abs(A[i]) mod 2^256)`. A duplicated minter is written again, so the later allowance wins.
3. Slot `0x06` ← `pad32(0x…b00002)`; slot `0x07` ← `pad32(0x…b00003)`.
4. `name` absent or empty: error; `symbol` absent or empty: error. The section 5.4 encoding (SNET-SYS-033) of the UTF-8 bytes of `name` at slot `0x08` and of `symbol` at slot `0x09`.
5. `decimals` absent or empty, or rejected by `parse_u8`: error. Slot `0x0a` ← `u256(decimals)`.
6. `currency` absent or empty: error. The section 5.4 encoding of `currency` at slot `0x0b`.
7. Slot `0x0d` as in `B-02` SNET-GEN-011, only when the genesis allocation is available (not on the upgrade path).
Source: systemcontracts/coin_adapter.go:34-169 (initializeCoinAdapter)
Source: systemcontracts/gov_base.go:68-77 (splitAndTrim), systemcontracts/stateutil.go:189-237 (EncodeBytesToSlots)
Observable: state

[SNET-SYS-092] The `GovMinter` initializer MUST produce the `GovBase` writes (`B-05` SNET-GOV-160) and then, if the key `fiatToken` is present (also when empty), slot `0x32` ← `pad32(hex_to_address(fiatToken))`, where a zero result is an error. It MUST NOT write any other slot.
Source: systemcontracts/gov_minter.go:87-111 (initializeMinter)
Observable: state

[SNET-SYS-093] The `GovMasterMinter` initializer MUST produce the `GovBase` writes and then:
1. if the key `fiatToken` is present: slot `0x32` ← `pad32(hex_to_address(fiatToken))`; a zero result is an error;
2. if the key `minters` is present: `M = split_and_trim(minters)`; an empty `M` is an error; a zero `hex_to_address(M[i])` is an error; slot `0x35` ← `u256(len(M))`; for each `i` in order: `keccak256(u256(0x35)) + i` ← `pad32(M[i])`, `mapping_slot(0x34, M[i])` ← `u256(1)`, `mapping_slot(0x36, M[i])` ← `u256(i + 1)`. Duplicates are not removed: the array keeps every item and the index of a duplicated address is the position of its last occurrence plus one;
3. slot `0x33` ← `u256(v mod 2^256)`, where `v = set_string(maxMinterAllowance)` if the key is present (a parse error or `v ≤ 0` is an error), else `10^28`.
Source: systemcontracts/gov_master_minter.go:59-159 (initializeMasterMinter)
Observable: state

Implementation note (informative). Executed with the reference initializers: with the testnet parameters, `NativeCoinAdapter` slot `0x08` is `0x574b5243…08` (`"WKRC"`, short form), `0x0a` is 18 and `0x0b` is `0x4b5257…06` (`"KRW"`); `minterAllowed = "-5"` is written as 5; `masterMinter = "not-an-address"` is written as the zero address without error; `GovMasterMinter` with `maxMinterAllowance = 2^256 + 1` writes 1 at slot `0x33`; `minters` with one address twice writes length 2, both elements, and index 2. None of the initializers checks that the three contracts point to each other (`masterMinter`, `fiatToken`) or that the minter lists agree; the presets are consistent (`B-01` §11).

---

## 11. Account `Extra` bits

### 11.1 Encoding

Every account in the state trie has, in addition to the Ethereum fields, a 64-bit unsigned field `Extra`. It is the fifth element of the account RLP list and is optional: it is omitted when zero (go-ethereum `rlp:"optional"` rule, `A-03`).

| Bit (from LSB) | Mask | Name | Meaning |
|---|---|---|---|
| 63 | `0x8000000000000000` | blacklisted | the account may not send, receive or pay fees (`B-07`), and a block whose `Coinbase` it is is rejected when the parent state is available (`B-08` SNET-SRC-020). |
| 62 | `0x4000000000000000` | authorized | the account is exempt from the header gas-tip minimum (`B-07`) |
| 0–61 | — | reserved | MUST be zero |

[SNET-SYS-070] The account encoding MUST be `rlp([nonce, balance, storageRoot, codeHash, extra])` with `extra` omitted when it is zero.
Source: core/types/state_account.go:28-37 (StateAccount.Extra `rlp:"optional"`)
Observable: state

[SNET-SYS-071] A node MUST treat an account as blacklisted if and only if bit 63 of `Extra` is set, and as authorized if and only if bit 62 is set. A node MUST treat an account that does not exist as neither.
Source: core/types/state_account_extra.go:30-45, :71-94
Source: core/state/statedb.go:310-327 (IsBlacklisted, IsAuthorized on a missing account return false)
Observable: state

[SNET-SYS-072] An account whose only non-default field is `Extra` MUST NOT be considered empty (EIP-161 deletion MUST NOT remove it).
Source: core/state/state_object.go:94-96 (empty includes `Extra == 0`)
Observable: state

### 11.2 Who writes the bits

The bits are written in exactly four places.

1. Genesis allocation: the `extra` field of an allocation entry (subject to SNET-SYS-013).
2. `GovCouncil` genesis initializer: the union of the `blacklist` / `authorizedAccounts` parameters and the allocation entries that already have the bit is written both to the allocation (`Extra |= mask`, creating zero-balance entries as needed) and to the `GovCouncil` sets (`B-02`); for an address that is also a genesis system-contract address, `inject_contracts` replaces the allocation entry afterwards (`B-02` SNET-GEN-012, SNET-GEN-014; SNET-SYS-011), so that address is in the set with `Extra = 0`.
3. At run time, the `AccountManager` native manager (section 12.1), which only the `GovCouncil` contract can call. `GovCouncil` calls it when a blacklist or authorized-account proposal executes (`B-05`) and updates its own set only if the call succeeded.
4. At run time, contract creation at the account's address. `CREATE`, `CREATE2` and a contract-creation transaction whose new address holds an account with nonce 0 and no code do not treat it as a collision; `StateDB.CreateAccount` replaces the account object and carries over only its balance, so both bits become 0. Neither the creation path nor the creation-transaction checks look at the blacklist bit of the new address; only the creator (and the transaction sender) is checked (`B-07` SNET-TX-041, SNET-TX-044).

---

## 12. Native managers and the BLS PoP precompile

### 12.1 `AccountManager` (`0x…b00003`)

The native manager dispatches on the first 4 bytes of the input (the Solidity function selector) and requires the input to be exactly the selector plus 32 bytes per parameter; each address parameter is taken from the low 20 bytes of its word.

| Method | Selector preimage | Params | Caller restriction | Gas | Effect |
|---|---|---|---|---|---|
| blacklist | `blacklist(address)` | 1 | `CALL` from genesis `GovCouncil` | 4500 (+25000 if the account does not exist) | set bit 63 |
| unBlacklist | `unBlacklist(address)` | 1 | same | 4500 | clear bit 63 |
| isBlacklisted | `isBlacklisted(address)` | 1 | none | 0 | returns a 32-byte word, last byte 1 if set |
| authorize | `authorize(address)` | 1 | same as blacklist | 4500 (+25000) | set bit 62 |
| unAuthorize | `unAuthorize(address)` | 1 | same | 4500 | clear bit 62 |
| isAuthorized | `isAuthorized(address)` | 1 | none | 0 | returns a 32-byte word, last byte 1 if set |

[SNET-SYS-080] The account manager MUST reject a state-changing method unless the call opcode is `CALL` and the immediate caller is the `GovCouncil` address in `anzeon.systemContracts`; it MUST reject an unknown selector and an input of the wrong length. A rejection is a call error (not a revert): the state changes of the call are reverted and all gas passed to the call is consumed, as for any failing call.
Source: core/vm/native_manager.go:137-155 (runNativeManager), core/vm/native_manager.go:470-489 (canRunAccountManager, validateCaller, validateCallContext)
Source: core/vm/native_manager.go:81-92 (selectors)
Source: core/vm/evm.go:262-263, :283-288 (native manager dispatch; non-revert error consumes remaining gas)
Observable: state

The full gas and failure semantics of native managers belong to the execution rules (`B-07`); they are summarised here because they are the only writer of the `Extra` bits.

### 12.2 `NativeCoinManager` (`0x…b00002`)

The coin manager (`mint(address,uint256)`, `burn(address,uint256)`, `transfer(address,address,uint256)`) changes balances directly and may only be called with `CALL` from the `NativeCoinAdapter` address in `anzeon.systemContracts`. It does not touch system-contract storage read by the node.
Source: core/vm/native_manager.go:83-85, :196-284, :463-468

### 12.3 BLS proof-of-possession precompile (`0x…b00001`)

`GovValidator.configureValidator` calls this precompile with `staticcall(blsKey ‖ blsSig)` and requires the call to succeed and return `true`.

| Property | Value |
|---|---|
| Input | exactly 144 bytes: 48-byte compressed G1 public key ‖ 96-byte compressed G2 signature |
| Gas | 45 000, independent of input, when the precompile returns output; on failure the call consumes all gas forwarded to it (the error is not a revert, `core/vm/evm.go:283-287`) |
| Output | 32-byte word, last byte 1 if `bls_verify(pk, msg = pk_bytes, sig)` holds (`A-02`), else all zero |
| Failure | error (the call fails) if the length is not 144, if the key does not decompress, is not in the G1 subgroup or is the point at infinity, or if the signature does not decompress or is not in the G2 subgroup. The G2 point at infinity decodes and verifies to 0 (output all zero, not a failure) |

[SNET-SYS-090] The BLS PoP precompile MUST behave as in the table, where the signed message is the 48-byte public key itself.
Source: core/vm/contracts.go:1189-1222 (blsPoP), core/vm/evm.go:283-287 (all gas consumed on a non-revert error)
Source: crypto/bls/blst/public_key.go:48-52, crypto/bls/blst/signature.go:56-66
Source: params/protocol_params.go:208-209 (address, gas)
Source: systemcontracts/solidity/v1/GovValidator.sol:157-172 (`_checkBlsKey`)
Observable: state

The address of the verifier used by `GovValidator` is not hard-coded in the contract: it is the value of slot `0x32` (`blsPoP`), written at genesis (section 8.3).

---

## 13. Security considerations of the system contracts

The behaviour below follows from the registered bytecode (SNET-SYS-005); a node that installs and executes that bytecode reproduces it. It is stated because it differs from what holds on Ethereum and affects users of the native coin. Nothing in this section changes block validity.

### 13.1 Native coin moved by signatures

The native coin is exposed as an ERC-20 token at the `NativeCoinAdapter` address, and every transfer path of that token moves the native balance itself through the coin manager (section 12.2). Unlike ETH on Ethereum, the native coin can therefore be moved without a transaction from its owner: by `transferFrom` after `approve` or an EIP-2612 `permit`, and by EIP-3009 `transferWithAuthorization` or `receiveWithAuthorization`. A `permit` or an authorization may have no expiry (`deadline = 2^256 − 1`, or a far `validBefore`); a leaked signature moves funds until its nonce is used or cancelled.

Signature validation uses `SignatureChecker.isValidSignatureNow(signer, digest, sig)`: if `EXTCODESIZE(signer) > 0` it only calls ERC-1271 `isValidSignature(digest, sig)` on `signer` and accepts the magic value `0x1626ba7e`; otherwise it accepts a 65-byte ECDSA signature with `v ∈ {27, 28}` and `s ≤ n/2` that recovers to `signer`. An EIP-7702 delegated account has code (the 23-byte designator, `B-07` SNET-TX-094), so only the ERC-1271 path applies to it.

### 13.2 Replay domain of token signatures

[SNET-SYS-096] The EIP-712 domain separator of `NativeCoinAdapter` v1 MUST be computed at every call as `keccak256(abi.encode(0x8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f, keccak256(name), keccak256("1"), CHAINID, adapter_address))`, where the first word is `keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")` and `name` is the string at slot `0x08`; slot `0x03` is not used.
Source: systemcontracts/solidity/v1/NativeCoinAdapter.sol:486-507, systemcontracts/solidity/libraries/EIP712.sol:39-50
Source: systemcontracts/solidity/abstracts/eip/EIP712Domain.sol:36-52
Observable: state

Implementation note (informative). Executed against the artifact: on the testnet state `DOMAIN_SEPARATOR()` returned `0xcdd475bf7b9e816f1c14ddff1c9ca33596570bcd2644311f8ebd90c41ffc9028`, equal to the formula; with the chain id changed to 8282 it returned `0x9ce49ea6…9c98`; a value written at slot `0x03` did not change it.

A signature is therefore valid wherever the chain id, the adapter address, `name` and the version `"1"` are the same. The mainnet and testnet presets have different chain ids but the same adapter address (`0x…1000`) and `name` (`WKRC`).

[SNET-SYS-097] A deployment MUST NOT reuse the chain id of another StableNet chain (including the mainnet and testnet presets) for a chain whose `NativeCoinAdapter` has the same address and `name`; otherwise EIP-2612 and EIP-3009 signatures made for one chain are valid on the other. The same holds across a chain split that keeps the chain id.
Source: params/config.go:45, params/config.go:156 (chain ids), params/config_wbft.go:31-45 (default addresses)

### 13.3 Blacklist and pre-signed authorisations

The adapter checks the blacklist at execution time only (`B-05` SNET-GOV-176). Allowances granted and EIP-2612 or EIP-3009 signatures produced before an account was blacklisted stay valid and become usable again when the account is removed from the blacklist; an operator that removes an account from the blacklist has to assume that such authorisations exist. A blacklisted authorizer can still cancel its own EIP-3009 authorization, which moves no funds.
