# B-06 Block finalization

- Status: draft
- Area code: `FIN`
- Reference implementation: go-stablenet `740526d03`

Finalization is the set of state changes and checks that a StableNet block undergoes after its transactions have been executed and before its state root is fixed. It is where the chain's own rules touch the state directly rather than through transactions: system-contract code is replaced at an upgrade height, the base fee that transactions paid is handed to the validators, the epoch block receives (or is checked against) the next validator set, and the header gas tip is checked against governance. Because these changes are part of the state root, a node that finalizes differently computes a different root and rejects every later block.

The reference implementation runs the same procedure, `processFinalize`, in two places. The proposer runs it inside `FinalizeAndAssemble` to build a block, with an epoch hook that writes `EpochInfo`. An importing node runs it inside `Finalize` during block processing, with an epoch hook that verifies `EpochInfo`. The two paths are otherwise identical, but they differ in what happens around them: the proposer never re-executes the block it built, and the other validators vote on a proposal without executing it (§6).

---

## 1. Inputs

- `header`: the header of block `n`. On the proposer path this is the header being built (its `Extra` already holds the consensus fields written by `A-08` proposal construction, including `GasTip`). On the import path it is a copy of the received header; changes made to it by finalization are discarded.
- `state`: the state after the last transaction of block `n` (`B-03` SNET-BODY-006).
- `epoch_handler`: `write_epoch` on the proposer path, `verify_epoch` on the import path.

[SNET-FIN-001] Finalization MUST run exactly once per block, after all transactions of the block, on the post-transaction state, and its state changes MUST be included in the block's state root.
Source: core/state_processor.go:83-104 (Process: transactions, then engine.Finalize)
Source: miner/worker.go:977, 1401 (proposer: transactions applied to env.state, then FinalizeAndAssemble)
Observable: state

---

## 2. Procedure

```python
def process_finalize(header, state, epoch_handler) -> None:
    n = header.number
    # 1. system-contract upgrades scheduled exactly at n
    st = merged_upgrade_transition(n)                     # §2.1; error -> block invalid
    if st is not None:
        for (addr, code) in st.codes:
            state.set_code(addr, code)
        for (addr, key, value) in st.states:
            state.set_storage(addr, key, value)
    # 2. base-fee distribution
    if is_london(n):
        distribute_base_fee(header, state)                # §3; error -> block invalid
    # 3. epoch information
    if is_epoch_block(n):                                 # A-04
        epoch_handler(header, state)                      # §4
    else:
        extra = decode_extra(header)                      # error -> block invalid
        if extra.epoch_info is not None:
            raise Invalid(ErrEpochInfoIsNotNil)           # "epoch info should be nil for non-epoch block"
    # 4. gas tip
    verify_gas_tip(header)                                # §5; every error -> block invalid
    # 5. roots
    header.root = state.intermediate_root(delete_empty=is_eip158(n))
    header.uncle_hash = EMPTY_UNCLE_HASH
```

[SNET-FIN-002] Finalization MUST perform its steps in the order of `process_finalize`: upgrades, base-fee distribution, epoch information, gas tip, state root. An error in any step MUST make the block invalid, and later steps MUST NOT be applied.
Source: consensus/wbft/engine/engine.go:929-970 (processFinalize)
Observable: state

### 2.1 System-contract upgrades

```python
def merged_upgrade_transition(n) -> Optional[StateTransition]:
    merged = None
    for u in upgrade_list:                                # B-01 §7.1, non-decreasing block order
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

[SNET-FIN-003] At block `n > 0`, every upgrade entry with `block == n` MUST be applied (restating `B-04` SNET-SYS-021, which owns the upgrade mechanism): first every code replacement of the merged transition in order (member order `govValidator`, `nativeCoinAdapter`, `govMinter`, `govMasterMinter`, `govCouncil` within an entry, entries in list order), then every storage write in order. A code replacement MUST change only the account's code (creating the account if it does not exist) and MUST keep its balance, nonce, extra and storage.
Source: consensus/wbft/config.go:199-225 (GetSystemContractsStateTransition)
Source: consensus/wbft/engine/engine.go:930-939
Observable: state

[SNET-FIN-004] Storage writes are produced only for entries whose version is `v1` and whose `params` is present, using the initialisers of `B-04` with no allocation (so the `NativeCoinAdapter` total supply and the `GovCouncil` blacklist and authorized sets are not initialised). Any other version MUST change code only.
Source: systemcontracts/systemcontracts.go:62-157
Source: systemcontracts/coin_adapter.go:153-166; systemcontracts/gov_council.go:111-146 (alloc == nil skips)
Observable: state

[SNET-FIN-005] If an upgrade entry at `n` names a version that is not registered for its contract type, or an initialiser rejects its `params`, block `n` MUST be invalid ("unsupported version %s for contract %s" or the initialiser's error).
Source: systemcontracts/contracts.go:80-90 (getContractCode)
Source: consensus/wbft/engine/engine.go:930-931

[SNET-FIN-006] Because upgrades are applied after the transactions, the transactions of block `n` MUST execute against the code in force before the upgrade; the new code applies from block `n + 1`. Fork predicates of `B-01` §3 (for example the Boho precompile) are already active in block `n`.
Source: core/state_processor.go:83-104 (transactions before Finalize)
Source: params/config.go:1534 (IsBoho inclusive of BohoBlock)
Observable: state

Example: on the testnet, block `14_408_500` executes its transactions with `GovMinter` v1 code and `P256VERIFY` available; the `GovMinter` code in the post-state of that block is v2; no storage slot of `0x…1003` changes.

---

## 3. Base-fee distribution

On StableNet the base fee is not burned for good. Each transaction pays `gas_used × effective_gas_price` and the coinbase receives only `gas_used × effective_tip`, so `gas_used × base_fee` leaves circulation during execution (`B-07`). Finalization then credits `header.BaseFee × header.GasUsed` to the validators of the block, in proportion to their diligence, and the rounding remainder to the coinbase. The native-coin supply is therefore unchanged by fees.

```python
def distribute_base_fee(header, state) -> None:
    if header.gas_used == 0 or header.number == 0:
        return                                                   # no state change
    if header.base_fee is None:
        raise Invalid("WBFT: baseFee is nil")
    epoch_block, info = epoch_info_for(chain, header.number, header.parent_hash)   # A-04 §3.1, restated in §3.1
    total = header.base_fee * header.gas_used                    # unbounded integer
    diligence_sum = 0                                            # uint64 in the reference; cannot overflow, see note
    for idx in info.validators:
        if idx >= len(info.candidates):
            raise Invalid("WBFT: validator candidate index out of range")
        diligence_sum += info.candidates[idx].diligence
    dust = total
    if diligence_sum != 0:
        for idx in info.validators:                              # list order, duplicates included
            c = info.candidates[idx]
            share = total * c.diligence // diligence_sum         # truncating division
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

[SNET-FIN-007] If `is_london(n)` holds, the node MUST run `distribute_base_fee` exactly as above. It MUST make no state change when `header.GasUsed == 0` or `n == 0`, and in that case MUST NOT evaluate any of the later conditions (in particular a missing `BaseFee` is then not an error).
Source: consensus/wbft/engine/engine.go:941-946, 972-975
Observable: state

[SNET-FIN-008] The amount distributed MUST be `header.BaseFee × header.GasUsed` taken from the header being finalized. On the import path this is the received header's value; a mismatch with the executed gas is detected afterwards by `B-03` SNET-BODY-007, which makes the block invalid.
Source: consensus/wbft/engine/engine.go:985-986
Source: core/block_validator.go:126-128
Observable: state

[SNET-FIN-009] Each validator entry `idx` of `info.validators`, in list order, MUST receive `floor(total × candidates[idx].diligence / diligence_sum)` when that value is non-zero, where `diligence_sum` is the sum over the same list (an index that appears twice is counted and paid twice). A share of zero MUST NOT be credited and MUST NOT touch the account.
Source: consensus/wbft/engine/engine.go:988-1035
Source: core/state/state_object.go:94-96, :400-408 (a zero `AddBalance` touches an empty account), core/state/statedb.go:882-895 (touched empty accounts are deleted), consensus/wbft/engine/engine.go:967 (`IntermediateRoot(IsEIP158)`)
Observable: state

Reason (informative). Crediting a zero amount through the state's `AddBalance` is not a no-op: it creates the account object if needed and, if the account is empty (nonce 0, balance 0, no code, `Extra` 0; `B-04` SNET-SYS-072), marks it touched. Under EIP-158, which is active on StableNet chains (all Ethereum forks at block 0, `B-01` §11.1), a touched empty account is deleted when the state is finalised. For an empty account that exists in the state trie this changes the state root; for an account that does not exist it has no effect. An implementation that credited zero shares would therefore compute a different state root whenever a zero-share validator has an existing empty account. The same holds for a zero remainder (SNET-FIN-010).

[SNET-FIN-010] The remainder `total − Σ shares` MUST be credited to `header.Coinbase` when it is non-zero. When `diligence_sum == 0` the whole `total` is the remainder.
Source: consensus/wbft/engine/engine.go:1006, 1037-1046
Observable: state

Implementation note (informative). Several error paths cannot be reached by a block that passed header verification: `BaseFee` is present on every London header (`B-03` SNET-BHDR-010); a decoded `EpochInfo` never has a nil candidate; `share <= total < 2^256` because `BaseFee <= MaxBaseFee` and `GasUsed < 2^63`; and the sum of truncated shares never exceeds `total`, so the dust is never negative. `diligence_sum` is a `uint64` in the reference; each diligence is at most `2 × 10^6` (`A-04`), so overflow would need more than `9 × 10^12` validators.

### 3.1 Which validator set is paid

```python
# A-04 §3.1 epoch_info_for, as it is evaluated here (no batch `parents`)
def epoch_info_for(chain, number, parent_hash) -> tuple[int, EpochInfo]:
    e = last_epoch_block(number - 1)               # A-04: greatest epoch block <= n-1
    h = canonical_header(e) or ancestor_of(parent_hash, e)   # canonical first (A-04)
    info = decode_extra(h).epoch_info
    if info is None:
        raise Invalid("WBFT: epochInfo is nil")
    return e, info
```

[SNET-FIN-012] The validator set paid for block `n` MUST be the `EpochInfo` stored in the extra of the last epoch block at or below `n − 1`, that is, the same `EpochInfo` that defines `validators_at(n)` in `A-04`. For an epoch block `n` this is the previous epoch's information, not the one written into block `n`. For blocks `1 .. E` of the first epoch it is the genesis `EpochInfo` (`B-02` SNET-GEN-007).
Source: consensus/wbft/engine/engine.go:1399-1447 (GetEpochInfo, getEpochInfo, extractEpochInfo)
Observable: state

### 3.2 Worked examples

**Example 1 (dust).** `BaseFee = 20_000_000_000_000`, `GasUsed = 21_000`, so `total = 420_000_000_000_000_000`. The epoch information has candidates `A (1_900_000)`, `B (1_000_000)`, `C (2_000_000)`, `D (1_900_000)` and `validators = [2, 0, 1]` (D is a candidate but not a validator). `diligence_sum = 2_000_000 + 1_900_000 + 1_000_000 = 4_900_000`.

| Order | Validator | Diligence | Share `= total × d // 4_900_000` |
|---|---|---|---|
| 1 | C | 2_000_000 | `171_428_571_428_571_428` |
| 2 | A | 1_900_000 | `162_857_142_857_142_857` |
| 3 | B | 1_000_000 | `85_714_285_714_285_714` |

`Σ shares = 419_999_999_999_999_999`, dust `= 1`, credited to the coinbase. If the coinbase is A, A's balance increases by `162_857_142_857_142_858` in total. D receives nothing.

**Example 2 (all diligence zero).** Same `total`, `validators = [0, 1]`, both with diligence 0. `diligence_sum = 0`, no share is computed, and the coinbase receives `420_000_000_000_000_000`.

**Example 3 (first epoch of the testnet).** Blocks `1 .. 140` use the genesis `EpochInfo`: seven validators, each with diligence `1_900_000`. For `total = 420_000_000_000_000_000`, each validator receives `60_000_000_000_000_000` and the dust is 0.

**Example 4 (empty block).** `GasUsed = 0`: no balance changes, whatever the base fee.

---

## 4. Epoch information

The content of the next-epoch information (candidates with updated diligence, shuffled validator indices, BLS keys) is computed by `compute_next_epoch_info` in `A-04`. This section fixes only when it runs, on which state, and how the two paths use it.

[SNET-FIN-013] On an epoch block `n` (`A-04` `is_epoch_block`), the next-epoch information MUST be computed from the state produced by steps 1 and 2 of `process_finalize`, that is, after all transactions of block `n`, the upgrades at `n` and the base-fee distribution. Candidate addresses and BLS keys are read from the `GovValidator` in force at `n` (`B-01` SNET-CFG-019, `B-08`). A transaction in block `n` that changes the `GovValidator` validator set therefore affects the next epoch.
Source: consensus/wbft/engine/engine.go:950-953 (handler receives the finalization state)
Source: consensus/wbft/engine/engine.go:806, 875, 894 (candidates and BLS keys read from that state)
Observable: header, state

[SNET-FIN-014] On the proposer path (`write_epoch`), the proposer MUST write the computed information into `header.Extra.EpochInfo` before steps 4 and 5, so that it is covered by the block hash. If the proposer's own address is not in `validators_at(n)`, the reference writes nothing, and the resulting block is invalid under SNET-FIN-015.
Source: consensus/wbft/engine/engine.go:1193-1207 (writeEpoch)
Observable: header

[SNET-FIN-015] On the import path (`verify_epoch`), the node MUST recompute the information and MUST reject the block if the header's `EpochInfo` is absent ("WBFT: epochInfo is nil"), or differs in the number of candidates ("WBFT: mismatch in candidate sizes"), in any candidate address or diligence at the same index, in the number of validators ("WBFT: mismatch in validator sizes"), in any validator index ("WBFT: The two validators do not match"), in the number of BLS keys ("WBFT: mismatch in BLS public key sizes"), or in any BLS key bytes ("WBFT: The two BLS public keys do not match"). The comparisons are exact and position-wise, in this order.
Source: consensus/wbft/engine/engine.go:1212-1263 (verifyEpoch)
Observable: header

[SNET-FIN-016] On a block that is not an epoch block, the header extra MUST decode as `WBFTExtra` and its `EpochInfo` MUST be absent; otherwise the block is invalid (`ErrEpochInfoIsNotNil`, "epoch info should be nil for non-epoch block").
Source: consensus/wbft/engine/engine.go:954-961
Source: consensus/wbft/common/errors.go:145
Observable: header

---

## 5. Gas tip

Every block carries in `WBFTExtra.GasTip` the tip that non-authorized senders pay in that block (`B-07`). The value is decided by governance in the `GovValidator` contract and read from the parent state, so it is known before the block's transactions run.

```python
def required_gas_tip(header) -> int:
    if cfg.Anzeon.SystemContracts is None:
        raise Invalid("WBFT: GovValidator contract is not enabled")     # unreachable after SNET-CFG-012
    parent = header_by_hash(header.parent_hash, header.number - 1)
    if parent is None:
        raise Invalid(ErrUnknownAncestor)
    if parent.root == ZERO_HASH:
        raise Invalid("WBFT: parent state root is empty")
    s = state_at(parent.root)                                           # may fail: state unavailable
    addr = genesis_system_contracts.govValidator.address                # B-01 SNET-CFG-020
    return uint256(s.get_storage(addr, 0x39))                           # 0 if the slot is empty

def verify_gas_tip(header) -> None:
    extra = decode_extra(header)                                        # error -> invalid
    want = required_gas_tip(header)
    if extra.gas_tip is None or extra.gas_tip != want:
        raise Invalid(GasTipMismatchError(have=extra.gas_tip, want=want))  # "invalid gas tip: have %d, want %d"
```

[SNET-FIN-017] The required gas tip of block `n` MUST be the 256-bit value of storage slot `0x39` of the genesis `GovValidator` address in the state whose root is the parent header's `Root`. An empty slot yields 0; there is no "unavailable contract" outcome.
Source: consensus/wbft/engine/engine.go:620-645 (getGasTip)
Source: systemcontracts/gov_validator.go:41, 212-215 (slot 0x39; GetGasTip never returns nil)
Observable: header, state

[SNET-FIN-018] During finalization, a block MUST be rejected if its `GasTip` is absent or differs from the required gas tip, and also if the required gas tip cannot be determined for any reason.
Source: consensus/wbft/engine/engine.go:963-965, 1280-1295 (verifyGasTip)
Observable: header

[SNET-FIN-019] During header verification (`A-08`, `B-03` §7 step H21), the same check MUST reject the block on a mismatching `GasTip` (an absent one cannot occur, `A-03` WBFT-ENC-008), but MUST NOT reject it for any other failure of the check: parent state unavailable (for example during snap sync), missing parent, zero parent root, missing system-contract configuration, or an extra decode failure inside the check (`A-08` WBFT-HDR-111). The check is then deferred to finalization, if the block is ever executed.
Source: consensus/wbft/engine/engine.go:329-348
Observable: header

[SNET-FIN-020] A proposer MUST write `GasTip = required_gas_tip(header)` into the extra when it prepares the header (`A-08`), so that SNET-FIN-018 holds for its own block.
Source: consensus/wbft/engine/engine.go:511-513, 518, 537, 542 (Prepare: WriteGasTip)
Observable: header

Version note. In `v1.1.0` the comparison was `extra.GasTip != nil && extra.GasTip != want`. The difference is not observable: a decoded `GasTip` is never `nil` (A-03 [WBFT-ENC-008]; the RLP decoder handles `*big.Int` before the `rlp:"nil"` pointer rule, `rlp/decode.go:161-162`), so an empty `gas_tip` item decodes to 0 and both versions compare 0 with the governance value.
Source: `git -C go-stablenet show v1.1.0:consensus/wbft/engine/engine.go` line 1291

---

## 6. State root, block assembly, and the two paths

[SNET-FIN-021] The finalized state root MUST be `intermediate_root(delete_empty = is_eip158(n))` of the finalization state (deleting touched empty accounts; `is_eip158` is true on StableNet), and the header `UncleHash` MUST be the empty-list hash.
Source: consensus/wbft/engine/engine.go:967-968
Observable: header, state

[SNET-FIN-022] On the proposer path, the block MUST be assembled from the finalized header with `TxHash` = transaction root (`EMPTY_ROOT_HASH` if there are no transactions), `ReceiptHash` = receipt root (`EMPTY_ROOT_HASH` if none), `Bloom` = OR of the receipt blooms (zero if none), no uncles and no withdrawals.
Source: consensus/wbft/engine/engine.go:1053-1061 (FinalizeAndAssemble)
Source: core/types/block.go:243-273 (NewBlock)
Observable: header

### 6.1 Proposer path versus import path

The two paths differ in who executes what.

- **Proposer.** The proposer prepares the header (`A-08`), executes the transactions against its own state, runs `process_finalize` with `write_epoch`, and hands the block to consensus. When the block is committed with the same hash, the node writes the block and the state it computed while building it. It does not verify the header, does not re-execute the block and does not run state validation on it.
- **Other validators before voting.** `validate_proposal` in the reference implementation checks the transaction root, the uncle hash and the header rules H1-H21 of `B-03` §7 except the seal step H17 (`A-08` steps P1-P7). It does **not** execute the transactions and does not run finalization. A validator therefore sends PREPARE and COMMIT for a block whose execution result it has not computed.
- **Every other node after commit.** The committed block, with seals, is inserted through the import path (`B-03` §7, all steps H1-S1), which executes it and runs `process_finalize` with `verify_epoch`.

[SNET-FIN-023] A conforming node MUST accept a block on the import path if and only if it passes all steps of `B-03` §7. Whether the node voted for the block, or proposed it, MUST NOT change that verdict.
Source: core/blockchain.go:1587, 1779-1792 (import path)
Source: consensus/wbft/backend/backend.go:213-250 (Commit: own proposal to Seal, others to the fetcher)
Source: miner/worker.go:819-873 (resultLoop: WriteBlockAndSetHead with the build-time state, no re-execution)
Observable: header, state

[SNET-FIN-024] A node that votes on a proposal without executing it (the reference behaviour, `A-09` WBFT-APP-050) MUST still reject the committed block on import if it fails any of `B1`, `E1`, `F1`, `S1` of `B-03` §7. Validity does not depend on the vote.
Source: consensus/wbft/engine/engine.go:160-186 (VerifyBlockProposal: no execution)
Source: consensus/wbft/backend/backend.go:257-278 (Verify)

Implementation note (informative). Because votes do not depend on execution, a proposer can obtain a quorum certificate for a block that honest nodes then cannot import. What the consensus layer does in that situation (the height has a committed block that nobody can insert) is outside Part B: the block is recorded as bad and the height is decided again after the round timer (`A-09` §5.3, WBFT-APP-100; `A-10` §9).
