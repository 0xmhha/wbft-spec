# B-02 Anzeon genesis

- Status: draft
- Area code: `GEN`
- Reference implementation: go-stablenet `740526d03`

Every node of a network must start from a byte-identical genesis block, because the genesis hash is the parent hash of block 1 and the genesis state is the pre-state of block 1. On an Anzeon chain the genesis block is not simply "the file": go-stablenet rebuilds two parts of it from the chain configuration. It replaces `extraData` with an encoded `WBFTExtra` that holds the first validator set and the initial gas tip, and it injects the bytecode and initial storage of the five system contracts into the allocation. It also overrides the base fee. A node that loaded the genesis file literally would compute a different hash. This chapter specifies that construction step by step, the consistency that must hold between the genesis extra and the genesis storage, the genesis hash, and the two preset genesis blocks as worked examples.

---

## 1. Inputs and overall procedure

The inputs are a `Genesis` specification (`config`, `nonce`, `timestamp`, `extraData`, `gasLimit`, `difficulty`, `mixHash`, `coinbase`, `alloc`, and the test-only `number`, `gasUsed`, `parentHash`, `baseFeePerGas`, `excessBlobGas`, `blobGasUsed`) and the chain configuration of `B-01` (`genesis.config`).

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

[SNET-GEN-001] On an Anzeon chain the genesis block MUST be built by `build_anzeon_genesis` in this order. The `extraData` of the genesis specification MUST be ignored and replaced; the allocation MUST be the input allocation after `inject_contracts`.
Source: core/genesis.go:242-263 (initializeAnzeonGenesis)
Source: core/genesis.go:297-307, 320-336, 341-351 (called on every SetupGenesisBlock path before ToBlock)
Observable: header, state

---

## 2. Header fields

```python
def to_header(g) -> Header:
    h = Header(
        parent_hash = g.parentHash,            # zero in practice
        uncle_hash  = EMPTY_UNCLE_HASH,        # keccak256(rlp([]))
        coinbase    = g.coinbase,
        root        = state_root(g.alloc),     # §5
        tx_hash     = EMPTY_ROOT_HASH,
        receipt_hash= EMPTY_ROOT_HASH,
        bloom       = zero,
        difficulty  = g.difficulty,
        number      = g.number,                # MUST be 0 to be committed
        gas_limit   = g.gasLimit if g.gasLimit != 0 else GENESIS_GAS_LIMIT,   # 4_712_388
        gas_used    = g.gasUsed,
        time        = g.timestamp,
        extra       = g.extraData,             # already replaced, §3
        mix_digest  = g.mixHash,
        nonce       = g.nonce,                 # 8-byte big-endian
        base_fee    = MIN_BASE_FEE,            # 20_000_000_000_000, Anzeon override
    )
    if g.difficulty is None and g.mixHash == ZERO_HASH:
        h.difficulty = 131072                  # go-ethereum GenesisDifficulty; unreachable from JSON
    # Shanghai / Cancun genesis fields are only set if those forks are active at genesis,
    # which a StableNet configuration does not do (B-01 SNET-CFG-025).
    return h
```

[SNET-GEN-002] On an Anzeon chain the genesis `BaseFee` MUST be `MinBaseFee = 20_000_000_000_000` regardless of `baseFeePerGas` in the genesis specification and regardless of whether London is active at block 0.
Source: core/genesis.go:535-546 (ToBlock: AnzeonEnabled case first)
Observable: header

[SNET-GEN-003] The genesis `GasLimit` MUST be the specification's `gasLimit`, or `4_712_388` when it is zero. `Difficulty`, `MixDigest`, `Nonce`, `Time`, `Coinbase`, `ParentHash`, `GasUsed` MUST be copied from the specification without constraint. In particular the genesis difficulty is not forced to `1`.
Source: core/genesis.go:510-534 (ToBlock)
Source: core/genesis.go:617-632 (mainnet preset difficulty 1, testnet preset difficulty 0 and gasLimit 0)
Observable: header

[SNET-GEN-004] A genesis specification with `number != 0` MUST NOT be committed ("can't commit genesis block with number > 0").
Source: core/genesis.go:575-579 (Commit)

Implementation note (informative). The genesis `MixDigest` is the parent randao mix of block 1 (`A-02` `randao_mix`). Both presets use the zero hash. The genesis `Difficulty` enters the stored total difficulty (`rawdb.WriteTd`), which `B-09` uses for peer selection; the testnet starts at total difficulty 0, the mainnet at 1.

---

## 3. Genesis extra data

```python
def create_initial_extra(a: AnzeonConfig) -> bytes:
    epoch = EpochInfo(candidates=[], validators=[], bls_public_keys=[])
    for i, addr in enumerate(a.Init.validators):
        epoch.candidates.append(Candidate(addr=addr, diligence=DEFAULT_DILIGENCE))   # 1_900_000
        epoch.validators.append(uint32(i))
        epoch.bls_public_keys.append(hex_decode(a.Init.blsPublicKeys[i]))           # panics on bad hex
    s = a.SystemContracts.GovValidator.params.get("gasTip", "")
    gas_tip = parse_decimal(s) if s != "" and parse_decimal_ok(s) else INITIAL_GAS_TIP  # 27_600_000_000_000
    extra = WBFTExtra(vanity=b"", randao_reveal=b"", prev_round=0,
                      prev_prepared_seal=None, prev_committed_seal=None,
                      round=0, prepared_seal=None, committed_seal=None,
                      gas_tip=gas_tip, epoch_info=epoch)
    return rlp_encode(extra)            # A-03 encoding of WBFTExtra
```

[SNET-GEN-005] The genesis `Extra` MUST be `rlp_encode(WBFTExtra)` with empty vanity and randao reveal, `prev_round = 0`, `round = 0`, all four aggregated seals absent (each encoded as the empty list `0xc0`), `gas_tip` as in SNET-GEN-006 and `epoch_info` as in SNET-GEN-007.
Source: consensus/wbft/config.go:227-256 (CreateInitialExtraData)
Source: core/types/istanbul.go:106-119 (WBFTExtra.EncodeRLP field order)
Observable: header

[SNET-GEN-006] The genesis `GasTip` MUST be `anzeon.systemContracts.govValidator.params["gasTip"]` parsed as a base-10 integer when the key is present, non-empty and parseable, and `InitialGasTip = 27_600_000_000_000` otherwise. A parseable negative value (an optional leading `+` or `-` is accepted) does not give a genesis block: construction fails (SNET-GEN-010).
Source: consensus/wbft/config.go:233-243
Observable: header

[SNET-GEN-007] The genesis `EpochInfo` MUST list the candidates in the order of `anzeon.init.validators`, each with diligence `DefaultDiligence = 1_900_000`; `validators = [0, 1, ..., n-1]` (no shuffling); `bls_public_keys` = the hex-decoded `anzeon.init.blsPublicKeys` in the same order. Duplicate addresses in `init.validators` are not removed.
Source: consensus/wbft/config.go:258-281 (CreateInitialEpochInfo)
Observable: header

The genesis `EpochInfo` is the validator set of blocks `1 .. E` where `E` is the first epoch block (`A-04`). Base-fee distribution in those blocks (`B-06` §3) uses its diligences, which are all equal.

Implementation note (informative). `CreateInitialExtraData` falls back to `InitialGasTip` for an unparseable `gasTip`, but the storage initialiser (§4.3) rejects the same value, so an unparseable value aborts genesis construction. The fallback is reachable only when the key is absent or empty. For an empty string the storage initialiser also fails. For an absent key both sides use `InitialGasTip`, provided `govValidator.params` is present and the version is `v1`; if `params` is absent, the extra holds `InitialGasTip` but slot `0x39` stays 0 (SNET-GEN-010).

---

## 4. Allocation: system-contract injection

### 4.1 Account extra bits in the input allocation

[SNET-GEN-008] Genesis construction MUST fail if any input allocation account has a bit set in `extra` outside `AccountExtraValidMask = (1 << 63) | (1 << 62)` (blacklisted, authorized).
Source: core/genesis.go:253-260
Source: core/types/state_account_extra.go:31-46, 103-108 (ValidateExtra)

[SNET-GEN-020] In the genesis file, the `extra` of an allocation account MUST be read as an unsigned 64-bit integer written as a JSON string with a `0x` or `0X` prefix (hexadecimal), a JSON string of decimal digits, or a JSON number. An absent `extra`, `null` or the empty string means 0. A negative value, a value of `2^64` or more, or any other form MUST make the genesis file invalid. (When the reference writes a genesis file, for example the dumped preset files, it writes `extra` as a `0x` hex string and omits it when it is 0.)
Source: core/types/gen_account.go:17-39 (MarshalJSON), 41-74 (UnmarshalJSON: extra as math.HexOrDecimal64)
Source: common/math/integer.go:48-82 (HexOrDecimal64, ParseUint64)
Observable: header, state

Implementation note (informative). Executed with the reference code: `"0x4000000000000000"`, `"4611686018427387904"` and the JSON number `4611686018427387904` all give the authorized bit, `"0X10"` gives 16, `null` and an absent key give 0, and `"-1"` and `"18446744073709551616"` fail with `invalid hex or decimal integer`.

### 4.2 Contract transition for the genesis entry

The genesis contracts are produced by `system_contracts_transition(sc, alloc)`, the function the reference calls `GetSystemContractsTransition` (defined in `B-04` §3; restated here). It walks the five members in a fixed order and, for each present member, emits the registered bytecode and, if `version == "v1"` and `params` is present, the initial storage.

```python
ORDER = ["govValidator", "nativeCoinAdapter", "govMinter", "govMasterMinter", "govCouncil"]

def system_contracts_transition(sc, alloc) -> StateTransition:
    st = StateTransition(codes=[], states=[])
    for name in ORDER:
        c = getattr(sc, name)
        if c is None:
            continue
        st.codes.append((c.address, bytecode(name, c.version)))      # error if version not registered
        if c.params is not None and c.version == "v1":
            st.states += initialize(name, c.address, c.params, alloc)  # B-04 layouts; may read/modify alloc
    return st
```

[SNET-GEN-009] The genesis storage MUST be produced by the per-contract initialisers (storage layouts in `B-04` §8 and §10.1, genesis-dependent values in §4.3-§4.4 below) invoked in the order `govValidator`, `nativeCoinAdapter`, `govMinter`, `govMasterMinter`, `govCouncil`, with the input allocation passed to `nativeCoinAdapter` and `govCouncil`. A contract whose `params` is absent, or whose version is not `v1`, receives code but no storage.
Source: systemcontracts/systemcontracts.go:62-157 (GetSystemContractsTransition)
Source: core/genesis.go:742-746 (called with &genesis.Alloc)
Observable: state

The order matters for two reasons. The `NativeCoinAdapter` total supply is the sum of the allocation balances at the moment it is computed (SNET-GEN-011), and `GovCouncil` adds accounts to the allocation afterwards (SNET-GEN-012).

### 4.3 Values that depend on the allocation

[SNET-GEN-010] When `govValidator.params` is present and the version is `v1`, `GovValidator` storage slot `0x39` (`gasTip`) MUST be `params["gasTip"]` parsed as a base-10 integer (Go `big.Int.SetString`, which accepts an optional leading `+` or `-`) and reduced modulo `2^256`, or `InitialGasTip` when the key is absent; a present but unparseable value (including the empty string) MUST abort genesis construction. A negative value also aborts genesis construction, because the genesis `Extra` cannot encode a negative `GasTip` (SNET-GEN-006). A value of `2^256` or more does not abort: the slot holds it modulo `2^256` while the genesis `Extra` holds the full value (SNET-GEN-019). When `params` is absent, or the version is not `v1`, slot `0x39` is not written and reads as 0. The remaining `GovValidator` slots (members, validators, BLS keys, PoP precompile address) follow `B-04`.
Source: systemcontracts/gov_validator.go:52-84 (initializeValidator: gasTip)
Source: systemcontracts/systemcontracts.go:87 (initializer only for present params and v1)
Source: consensus/wbft/config.go:233-256 (extra gas tip; RLP encoding fails for a negative value)
Observable: state

Implementation note (informative). Executed with the reference code on the testnet configuration: `gasTip = "-5"` fails with `rlp: cannot encode negative big.Int`; `"+5"` gives 5 in both the extra and the slot; `2^256 + 1` gives the full value in the extra and 1 in the slot; `govValidator.params = nil` gives `InitialGasTip` in the extra and 0 in the slot.

[SNET-GEN-011] `NativeCoinAdapter` slot `0x0d` (`_totalSupply`) MUST equal the sum of the `balance` fields of the input allocation, taken before `inject_contracts` writes the system-contract accounts (§4.4).
Source: systemcontracts/coin_adapter.go:153-166
Observable: state

### 4.4 GovCouncil initialisation and allocation synchronisation

`GovCouncil` keeps the blacklist and the authorized-account list twice: as `AddressSet` storage in the contract (read by governance) and as bits in each account's `extra` (read by execution, `B-07`). At genesis both copies are built from the union of two sources.

```python
def init_gov_council(addr, params, alloc) -> list[StateParam]:
    sp = init_gov_base(addr, params)                              # B-04
    sets = {}
    for key, bit in [("blacklist", BLACKLISTED), ("authorizedAccounts", AUTHORIZED)]:
        s = parse_addresses(params.get(key, ""))                  # comma separated; zero address -> error
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
                                          sorted(sets[key], key=bytes))   # ascending by address bytes
    sp.append(StateParam(addr, 0x36, ACCOUNT_MANAGER_ADDRESS))    # 0x…B00003
    return sp
```

`address_set_storage` writes `len` at `values_slot`, element `i` at `keccak256(values_slot) + i`, and position `i + 1` at `keccak256(pad32(a) ‖ positions_slot)` (`B-04`).

[SNET-GEN-012] At genesis, the blacklist (respectively authorized) set MUST be the union of the addresses in `govCouncil.params.blacklist` (respectively `authorizedAccounts`) and the non-zero allocation addresses whose `extra` has the blacklisted (respectively authorized) bit. Every address in the union, except a genesis system-contract address, MUST end with that bit set in its allocation `extra`; an address that was not in the allocation MUST be added with balance 0 (and no code, nonce 0). For an address that is also a genesis system-contract address, `inject_contracts` replaces the allocation entry afterwards (SNET-GEN-014; `B-04` SNET-SYS-011), so that address is in the set with `extra = 0`. A zero address in the parameters MUST abort genesis construction.
Source: systemcontracts/gov_council.go:68-146 (initializeGovCouncil)
Source: systemcontracts/gov_council.go:213-225 (parseParamAddresses)
Source: core/genesis.go:750-752 (system-contract entries replaced after initialisation)
Observable: state

[SNET-GEN-013] The `GovCouncil` `AddressSet` storage for each non-empty set MUST list the addresses sorted ascending by their 20-byte value, and slot `0x36` MUST hold `AccountManagerAddress` (`0x0000000000000000000000000000000000B00003`).
Source: systemcontracts/gov_council.go:148-211 (accountManager slot, initializeAddressSet)
Observable: state

Implementation note (informative). Fix #83 (commit `3eada119e`, contained in both `v1.1.0` and the reference commit) only changes the balance of a params-only account from "absent" to `0`. The state root is unchanged by it, because a nil balance is skipped by the genesis state builder and a zero balance adds nothing. Its effect is that the dumped genesis JSON can be re-read ("missing required field 'balance'" no longer occurs). The genesis hash is the same with and without the fix.

### 4.5 Writing contract accounts and block-0 overlays

```python
def inject_contracts(g):
    st = system_contracts_transition(g.config.Anzeon.SystemContracts, g.alloc)   # §4.2, mutates g.alloc
    for (addr, code) in st.codes:
        g.alloc[addr] = Account(code=code, balance=0, nonce=0, extra=0, storage={})  # replaces any entry
    for (addr, key, value) in st.states:
        g.alloc[addr].storage[key] = value
    for u in collect_upgrades(g.config):                      # B-01 §7.1
        if u.block != 0:
            continue
        ov = system_contracts_transition(u.system_contracts, None)
        for (addr, code) in ov.codes:
            if addr in g.alloc:
                g.alloc[addr].code = code                     # balance, nonce, extra, storage kept
            else:
                g.alloc[addr] = Account(code=code, balance=0, storage={})
        for (addr, key, value) in ov.states:
            g.alloc[addr].storage[key] = value
```

The injection mechanism is specified normatively in `B-04`: SNET-SYS-011 (baseline transition, fresh accounts) and SNET-SYS-012 (block-0 upgrade overlays). SNET-GEN-014 and SNET-GEN-015 restate those two requirements at their place in the genesis procedure; if the wording differs, `B-04` governs.

[SNET-GEN-014] Each genesis system-contract account MUST be written as a fresh account (code from the registry, balance 0, nonce 0, extra 0, storage from the initialisers only). Any input allocation entry at the same address is discarded.
Source: core/genesis.go:735-755 (InjectContracts)
Observable: state

[SNET-GEN-015] For every collected upgrade with `block == 0` (for example `Boho` when `bohoBlock == 0`), the contract code MUST be replaced in the genesis allocation while balance, nonce, extra and storage are kept; storage is added only for `v1` entries with `params` (none on the presets).
Source: core/genesis.go:757-797 (applyUpgradeOverlay)
Observable: state

Implementation note (informative). `gstable init` refuses (`log.Crit`) a genesis file whose allocation contains any of the five genesis system-contract addresses. `core.SetupGenesisBlock` itself does not; the in-code presets have no such entries. A consequence is that the dumped files `core/genesis_mainnet.json` and `core/genesis_testnet.json`, which contain the injected contracts, cannot be fed back into `gstable init`. Source: cmd/gstable/chaincmd.go:224-231, 252-270.

---

## 5. Genesis state root

Every account of the final allocation is written into an empty state in this per-account order: add balance (if present), set code, set nonce, set extra, set every storage slot. The state is committed without deleting empty accounts.

[SNET-GEN-016] The genesis state root MUST be the root of the state trie that contains exactly the accounts of the final allocation, each encoded as the StableNet account `rlp([nonce, balance, storage_root, code_hash] + ([extra] if extra != 0 else []))`, with storage slots whose value is zero omitted, and with empty accounts kept.
Source: core/genesis.go:118-148 (hashAlloc; Commit(0, false))
Source: core/types/state_account.go:31-38; core/types/gen_account_rlp.go:8-25 (optional Extra)
Observable: header, state

The trailing `extra` element is the only difference from the Ethereum account encoding. It is also present in every later state root (`B-06` §5, `B-07`).

---

## 6. Genesis hash

The block hash of a WBFT header is normally computed over a filtered copy of the header (`A-03` `block_hash`): seals removed and round set to 0. The filter is selected by `Difficulty == 1`, not by block number. The testnet genesis has difficulty 0, so its hash is the plain header hash. The mainnet genesis has difficulty 1, so the filtered hash is used; the genesis extra already has no seals and round 0, and decoding then re-encoding it reproduces the same bytes, so the result is again the plain header hash.

[SNET-GEN-017] The genesis block hash MUST be `keccak256(rlp(header))` of the genesis header of §2. For a genesis whose `Difficulty == 1`, an implementation that applies the `A-03` filtered hash MUST obtain the same value; this holds whenever the genesis extra is the canonical encoding of SNET-GEN-005. For a genesis whose `Difficulty` is not 1, such as the testnet genesis with `Difficulty = 0`, the filter is not selected and the hash is the plain `keccak256(rlp(header))`. An implementation MUST select the hash rule by `Difficulty == 1` exactly as the reference does (not by block number and not by chain), so that the testnet genesis hash is the plain header hash.
Source: core/types/block.go:121-131 (Header.Hash selects the filter by Difficulty == 1)
Source: core/types/istanbul.go:269-288 (WBFTFilteredHeaderWithRound)
Observable: header

[SNET-GEN-018] A node started with `--mainnet` or `--testnet` MUST reject a database whose stored genesis hash differs from the hash of the preset genesis it builds (`GenesisMismatchError`), and SHOULD check the built hash against the constants of `B-01` §11.1.
Source: core/genesis.go:338-351 (hash comparison)
Source: params/config.go:31-32 (constants)
Observable: header

---

## 7. Consistency between genesis extra and genesis storage

The genesis extra and the genesis storage come from different sections of the configuration (`anzeon.init` and `govValidator.params` respectively). The reference implementation does not compare them. What each one controls is fixed, so a mismatch does not make the genesis block invalid; it changes behaviour at later heights.

| Genesis extra value | Storage counterpart | Read by | Effect of a mismatch |
|---|---|---|---|
| `EpochInfo.candidates` / `validators` (from `init.validators`) | `GovValidator` validator set (from `params.validators`) | Extra: validator set of blocks `1..E` (`A-04`). Storage: candidate list at the first epoch block `E` (`B-08`) | The validator set changes at `E + 1` to the storage set even without any governance action |
| `EpochInfo.bls_public_keys` (from `init.blsPublicKeys`) | `GovValidator.validatorToBlsKey` (from `params.blsPublicKeys`) | Extra: seal verification for blocks `1..E`. Storage: BLS keys written at `E` | Seals of blocks `1..E` are verified against the extra keys; later seals against the storage keys |
| `GasTip` | `GovValidator` slot `0x39` | Extra: nothing verifies the genesis value; RPC and gas-price estimation read it while the head is the genesis block (`internal/ethapi/api.go:1562`, `eth/gasprice/anzeon.go:128-129`). Storage: the required `GasTip` of block 1 (`B-06` §5) | Block 1 must carry the storage value; the genesis value is only informational |

[SNET-GEN-019] A genesis configuration SHOULD make `anzeon.init.validators` equal to `govValidator.params.validators` (same addresses, same order, no duplicates), `anzeon.init.blsPublicKeys` equal to `govValidator.params.blsPublicKeys`, and the genesis `GasTip` equal to the `GovValidator` gas-tip slot. A conforming node MUST NOT reject a genesis block because these differ, since the reference implementation accepts it.
Source: consensus/wbft/config.go:258-281 (extra from Init)
Source: systemcontracts/gov_validator.go:86-181 (storage from params)
Source: consensus/wbft/engine/engine.go:622-645, 1280-1295 (block 1 gas tip read from storage)

Both presets satisfy SNET-GEN-019 (§8).

---

## 8. Worked examples: the preset genesis blocks

The values below were produced by building the in-code presets with `core.SetupGenesisBlock` on an empty in-memory database at the reference commit; both hashes equal the constants in `params/config.go:31-32`.

### 8.1 Mainnet (chain id 8282)

| Field | Value |
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
| Hash | `0xf192f2ba82c9265777bad7d33b7fd561430ae5e1f60f2e99c893073c81dc5b7b` (plain and filtered hash coincide) |

`Extra` (98 bytes):

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

Allocation: the input allocation is empty. The final allocation has the five system contracts only, with balance 0; `GovMinter` carries the `v2` bytecode because `bohoBlock == 0` (§4.5). Storage slot counts in the dumped file are `0x1000`: 10, `0x1001`: 20, `0x1002`: 15, `0x1003`: 10, `0x1004`: 10. `NativeCoinAdapter._totalSupply` (slot `0x0d`) is 0.

### 8.2 Testnet (chain id 8283)

| Field | Value |
|---|---|
| `Root` | `0x9080703111c1f13dbab77af07b2dc96966e2cec0331f96d5479a16bf3a1b635b` |
| `Difficulty` | `0` |
| `GasLimit` | `4_712_388` (preset leaves `gasLimit` at 0) |
| `BaseFee` | `20_000_000_000_000` |
| `Time`, `Nonce`, `MixDigest`, `Coinbase`, `ParentHash` | all zero |
| Hash | `0x2bdf79b3d3cc49f9e6638ff81f3bb85065c79945a8fe4556cd0ff47bbfc02490` (plain hash, because difficulty is 0) |

Note (informative). The testnet preset sets `Difficulty` to 0 (`core/genesis.go:629`), while the mainnet preset sets 1 (`core/genesis.go:621`). The testnet genesis is therefore the only header of that chain whose hash is not selected by the WBFT filter (SNET-GEN-017). The value is part of the genesis hash of the operating testnet, so it is a fact of that chain and cannot be changed, whatever the original intent.

`Extra` has the same shape as §8.1 with a `0xf9022c` list header, seven candidates `(init.validators[i], 1_900_000)` in the order of `B-01` §11.2, `validators = [0, 1, 2, 3, 4, 5, 6]` (encoded `c7 80 01 02 03 04 05 06`), seven BLS keys, and `gas_tip = 27_600_000_000_000`.

Allocation: eight funded accounts from the embedded testnet allocation (`0x4053e68d…15de`, `0x56d97be0…4bd1`, `0x58f13fe4…4d43`, `0x79495f15…9002`, `0x9b690222…6746`, `0xe0e5bdd4…f453`, `0xf7aeccd8…c833` with `10^27` each, and `0xd13657a2f08a266ca816c96c1a97e888990f485b` with `10^28`), plus the five `v1` system contracts. `NativeCoinAdapter._totalSupply = 7 × 10^27 + 10^28 = 17_000_000_000_000_000_000_000_000_000`, which equals the sum of the input balances (SNET-GEN-011). `GovValidator` slot `0x39 = 27_600_000_000_000`, equal to the extra `GasTip`. `GovMinter` is `v1` at genesis; it becomes `v2` at block `14_408_500` (`B-06` §2).

Implementation note (informative). The dumped files `core/genesis_mainnet.json` and `core/genesis_testnet.json` show `baseFeePerGas = 0x3b9aca00` and, for the testnet, `gasLimit = 0x0` and `difficulty = 0x0`. The first value is not what the header contains (it is replaced by `MinBaseFee`, SNET-GEN-002); the other two are applied as described in SNET-GEN-003. Readers must not take the JSON `baseFeePerGas` as the genesis base fee.
