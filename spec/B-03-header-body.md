# B-03 Execution-side header, body and state validity

- Status: draft
- Area codes: `BHDR` (header), `BODY` (body and post-execution state)
- Reference implementation: go-stablenet `740526d03`

`A-08` specifies the consensus fields of a header: difficulty, timestamp, coinbase and signer, extra data, seals, randao. The reference implementation checks those fields in the same function that checks the execution-side fields (gas limit, base fee, forbidden fork fields, uncle hash), so a reader of the code sees one list. This chapter specifies the execution-side part of that list, then the body checks that run once the header is accepted, and the state checks that run after the block has been executed. It ends with the global order in which a block passes all of these checks together with `A-08`, `B-06` and `B-07`.

The rule for the base fee is the main StableNet difference from Ethereum. Anzeon replaces the EIP-1559 proportional update by a threshold rule with a fixed 2 % step and a floor and ceiling; the exact integer procedure is in §2.3.

---

## 1. Standalone header rules

These rules depend only on the header and the configuration. The reference implementation applies them to every header passed to header verification, before it looks at the parent.

[SNET-BHDR-001] A header MUST have `UncleHash == keccak256(rlp([])) = 0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347`; otherwise it is invalid with `ErrInvalidUncleHash` ("non empty uncle hash").
Source: consensus/wbft/engine/engine.go:54, 207-210
Source: consensus/wbft/common/errors.go:53
Observable: header

[SNET-BHDR-002] A header MUST have `GasLimit <= MaxGasLimit = 2^63 - 1`; otherwise it is invalid ("invalid gasLimit: have %v, max %v").
Source: consensus/wbft/engine/engine.go:216-219
Observable: header

[SNET-BHDR-003] A header for which `is_shanghai(number, time)` holds (`B-01` SNET-CFG-003) MUST be rejected ("wbft does not support shanghai fork").
Source: consensus/wbft/engine/engine.go:220-222
Observable: header

[SNET-BHDR-004] A header MUST have `WithdrawalsHash` absent; otherwise it is invalid ("invalid withdrawalsHash: have %x, expected nil").
Source: consensus/wbft/engine/engine.go:223-226
Observable: header

[SNET-BHDR-005] A header for which `is_cancun(number, time)` holds MUST be rejected ("wbft does not support cancun fork").
Source: consensus/wbft/engine/engine.go:227-229
Observable: header

[SNET-BHDR-006] A header MUST have `ExcessBlobGas`, `BlobGasUsed` and `ParentBeaconRoot` absent, checked in that order ("invalid excessBlobGas: have %d, expected nil", "invalid blobGasUsed: have %d, expected nil", "invalid parentBeaconRoot, have %#x, expected nil").
Source: consensus/wbft/engine/engine.go:230-238
Observable: header

Absent means the optional header field is not present in the RLP encoding of the header (go-ethereum optional trailing fields). A header that encodes a zero value for one of these fields is not "absent" and is rejected.

---

## 2. Rules relative to the parent

These rules run for every header with `number > 0` after the parent has been found (`A-08`: unknown parent, timestamp). The genesis header is not judged by header verification; its validity is `B-02`. (The reference passes the current header, which can be the genesis header, through `VerifyHeader` at start-up and ignores the result, `core/blockchain.go:424`.)

### 2.1 Gas used and gas limit

[SNET-BHDR-007] A header MUST have `GasUsed <= GasLimit`; otherwise it is invalid ("invalid gasUsed: have %d, gasLimit %d").
Source: consensus/wbft/engine/engine.go:273-276
Observable: header

```python
def verify_gas_limit(parent_gas_limit: uint64, gas_limit: uint64) -> None:
    # reference: signed 64-bit difference, then absolute value
    diff = abs(int64(parent_gas_limit) - int64(gas_limit))
    limit = parent_gas_limit // 1024                  # GasLimitBoundDivisor
    if diff >= limit:
        raise Invalid(f"invalid gas limit: have {gas_limit}, want {parent_gas_limit} +-= {limit - 1}")
    if gas_limit < 5000:                              # MinGasLimit
        raise Invalid("invalid gas limit below 5000")
```

[SNET-BHDR-008] The gas limit MUST satisfy `verify_gas_limit(parent_gas_limit, header.GasLimit)` where `parent_gas_limit = parent.GasLimit` if `is_london(parent.Number)`, and `parent.GasLimit * 2` (elasticity multiplier) if the header is the first London block. The allowed change per block is therefore strictly less than `parent_gas_limit // 1024`, and the limit never drops below 5000.
Source: consensus/misc/eip1559/eip1559.go:33-41 (VerifyEIP1559Header, gas limit part)
Source: consensus/misc/gaslimit.go:27-41 (VerifyGaslimit)
Observable: header

Implementation note (informative). Both presets have London at block 0, so every parent is a London block and the multiplier is never applied. Because `GasLimit <= 2^63 - 1` (SNET-BHDR-002), the signed conversion in `verify_gas_limit` cannot overflow for a verified header, except on the first London block: there `parent_gas_limit = parent.GasLimit * 2` exceeds `2^63 - 1` when `parent.GasLimit > 2^62`, `int64(parent_gas_limit)` wraps to a negative value, and the 64-bit difference wraps as well. The verdict is still correct while the true difference is below `2^63`, but a far smaller gas limit can then pass, and the bound stated in SNET-BHDR-008 does not hold for that header. Executed with the reference code: with a parent gas limit of `2^63 - 1`, a first London block with gas limit 5000 is accepted, while a later London block with the same parent values is rejected. The pseudocode reproduces this only with 64-bit wrapping arithmetic. The presets cannot reach this case, because their London block is 0. If a parent had a gas limit below 1024, `limit` would be 0 and no child could be valid; with `MinGasLimit = 5000` this can only happen with a genesis gas limit below 1024.

[SNET-BHDR-009] If the header is not a London block, `BaseFee` MUST be absent ("invalid baseFee before fork: have %d, want <nil>") and the gas limit MUST satisfy `verify_gas_limit(parent.GasLimit, header.GasLimit)`.
Source: consensus/wbft/engine/engine.go:277-284
Observable: header

### 2.2 Base fee presence and value

[SNET-BHDR-010] If the header is a London block, `BaseFee` MUST be present ("header is missing baseFee") and MUST equal `calc_base_fee(parent)` of §2.3 ("invalid baseFee: have %s, want %s, parentBaseFee %s, parentGasUsed %d").
Source: consensus/misc/eip1559/eip1559.go:42-52
Source: consensus/wbft/engine/engine.go:285-288
Observable: header

### 2.3 Anzeon base-fee rule

```python
INCREASING_THRESHOLD = 20        # percent
DECREASING_THRESHOLD = 6         # percent
BASE_FEE_CHANGE_RATE = 2         # percent
MIN_BASE_FEE = 20_000_000_000_000
MAX_BASE_FEE = 20_000_000_000_000_000     # 0 would disable the ceiling

def base_fee_delta(parent_base_fee: int) -> int:
    d = parent_base_fee * BASE_FEE_CHANGE_RATE // 100
    return d if d != 0 else 1

def calc_base_fee(parent: Header) -> int:
    if not is_london(parent.number):
        return INITIAL_BASE_FEE                     # 1_000_000_000; first London block only
    # Anzeon chain (B-01 SNET-CFG-004).
    increasing_target = uint64(parent.gas_limit * INCREASING_THRESHOLD) // 100
    decreasing_target = uint64(parent.gas_limit * DECREASING_THRESHOLD) // 100
    base_fee = parent.base_fee
    if parent.gas_used > increasing_target:
        base_fee = base_fee + base_fee_delta(parent.base_fee)
        if MAX_BASE_FEE != 0 and base_fee > MAX_BASE_FEE:
            base_fee = MAX_BASE_FEE
        return base_fee
    if parent.gas_used < decreasing_target:
        base_fee = base_fee - base_fee_delta(parent.base_fee)
        if base_fee < MIN_BASE_FEE:
            base_fee = MIN_BASE_FEE
        return base_fee
    return base_fee                                 # unchanged inside [decreasing_target, increasing_target]
```

The Ethereum proportional rule (`parentGasTarget = GasLimit / 2`, change `/ 8`) exists in the same function for non-Anzeon chains and is never used on StableNet.

Implementation note (informative). Because the genesis base fee is `MinBaseFee` (`B-02` SNET-GEN-002) and the rule never goes below the floor after a decrease nor above the ceiling after an increase, every base fee on a chain that is London from genesis lies in `[MIN_BASE_FEE, MAX_BASE_FEE]`. On a chain whose London block is later than 0, the first London block gets `INITIAL_BASE_FEE = 10^9`, which is below the floor. The value stays below the floor while the parent usage is inside the band (unchanged) or above the increase threshold (it rises by 2 % per block, and the floor is not applied after an increase). The first block whose parent usage is below `DECREASING_THRESHOLD` gets `MIN_BASE_FEE`, because the floor is applied after the decrease. Executed with the reference code: a parent base fee of `10^9` with `GasUsed = 0` gives `20_000_000_000_000`, and with `GasUsed = 10_000_000` (inside the band) gives `1_000_000_000`.

### 2.4 Worked examples

Parent gas limit `105_000_000` (mainnet preset genesis): `increasing_target = 21_000_000`, `decreasing_target = 6_300_000`. The values were computed with the reference `CalcBaseFee`.

| Parent base fee | Parent gas used | Case | Child base fee |
|---|---|---|---|
| `20_000_000_000_000` | `0` | decrease, clamped to floor | `20_000_000_000_000` |
| `20_000_000_000_000` | `6_299_999` | decrease, clamped to floor | `20_000_000_000_000` |
| `20_000_000_000_000` | `6_300_000` | unchanged (not `<` target) | `20_000_000_000_000` |
| `20_000_000_000_000` | `21_000_000` | unchanged (not `>` target) | `20_000_000_000_000` |
| `20_000_000_000_000` | `21_000_001` | increase by `400_000_000_000` | `20_400_000_000_000` |
| `30_000_000_000_000` | `0` | decrease by `600_000_000_000` | `29_400_000_000_000` |
| `30_000_000_000_000` | `21_000_001` | increase by `600_000_000_000` | `30_600_000_000_000` |
| `19_999_999_999_999_999` | `105_000_000` | increase, clamped to ceiling | `20_000_000_000_000_000` |

---

## 3. Proposal construction (execution-side fields)

`build_proposal` (`A-09`) fills the fields of this chapter as follows. Only the resulting values are normative (they must pass §1-§2); the way a node chooses its gas limit is local.

[SNET-BHDR-012] A proposer MUST set `BaseFee = calc_base_fee(parent)`, MUST leave `WithdrawalsHash`, `ExcessBlobGas`, `BlobGasUsed` and `ParentBeaconRoot` absent, and MUST choose a `GasLimit` that satisfies SNET-BHDR-008.
Source: miner/worker.go:1141-1164 (prepareWork: CalcGasLimit, CalcBaseFee; Cancun fields only if Cancun)
Observable: header

Implementation note (informative). The reference miner computes `GasLimit = CalcGasLimit(parent.GasLimit, GasCeil)`: it moves toward the local `--miner.gaslimit` target by at most `parent.GasLimit // 1024 - 1` per block and never below 5000 (`core/block_validator.go:151-172`). `UncleHash`, `TxHash`, `ReceiptHash`, `Bloom`, `Root` are set during finalization (`B-06` §6).

[SNET-BHDR-013] Unless the node operator configures another target, a proposer SHOULD use the gas-limit target `105_000_000` (the reference default of `--miner.gaslimit`; upstream go-ethereum uses `30_000_000`) and set `GasLimit = CalcGasLimit(parent.GasLimit, 105_000_000)` as described above. All validators of a network SHOULD use the same target, because proposers with different targets make the header `GasLimit` move up and down between their blocks; such blocks stay valid. With the default target, the mainnet preset stays at its genesis gas limit `105_000_000`, and the testnet preset, whose genesis gas limit is `4_712_388` (`B-02` §8.2), rises toward the target by at most `parent.GasLimit // 1024 - 1` per block.
Source: miner/miner.go:73-75 (DefaultConfig.GasCeil), cmd/utils/flags.go:442 (flag default)
Source: miner/worker.go:1145, 1162 (CalcGasLimit with GasCeil), core/block_validator.go:151-172 (CalcGasLimit)
Observable: header

---

## 4. Body rules

Body validation runs after the header of the block has been accepted and before execution.

[SNET-BODY-001] A block MUST contain no uncles; any uncle makes it invalid with `ErrInvalidUncleHash` ("non empty uncle hash"). The header `UncleHash` MUST equal `keccak256(rlp(uncles))` ("uncle root hash mismatch (header value %x, calculated %x)").
Source: core/block_validator.go:62-67
Source: consensus/wbft/engine/engine.go:453-458 (VerifyUncles)
Observable: header

[SNET-BODY-002] The header `TxHash` MUST equal the Merkle-Patricia root of the transaction list (`DeriveSha` over the consensus encodings, keyed by `rlp(index)`) ("transaction root hash mismatch (header value %x, calculated %x)").
Source: core/block_validator.go:68-70
Observable: header

[SNET-BODY-003] Because `WithdrawalsHash` is absent (SNET-BHDR-004), the block body MUST NOT carry a withdrawals list; a present list, including an empty one, makes the block invalid ("withdrawals present in block body").
Source: core/block_validator.go:72-84
Source: core/types/block.go:190-194, 230-235 (withdrawals is an optional trailing body field)
Observable: header

[SNET-BODY-004] No transaction of the block may carry a blob sidecar ("unexpected blob sidecar in transaction at index %d"), and because `BlobGasUsed` is absent, the total number of blob hashes over all transactions MUST be zero ("data blobs present in block body"). The sidecar check runs per transaction in list order before the count check.
Source: core/block_validator.go:86-110
Observable: header

[SNET-BODY-005] Body validation MUST report `ErrUnknownAncestor` if the parent block is unknown and `ErrPrunedAncestor` if the parent block is known but its state is not available. These are not invalidity verdicts: the block is retried or processed as a side chain once the ancestor is available.
Source: core/block_validator.go:112-118

Implementation note (informative). `ValidateBody` first returns `ErrKnownBlock` if the block and its state are already present. This is a short-circuit, not a validity verdict.

---

## 5. Execution

Execution applies the transactions of the block in list order to the parent state, then finalizes. Transaction semantics, the gas pool (initialised to `header.GasLimit`; a transaction whose gas limit exceeds the remaining pool makes the block invalid) and receipts are specified in `B-07`. Finalization (upgrades, base-fee distribution, epoch info, gas tip, state root) is specified in `B-06`.

[SNET-BODY-006] Execution MUST process all transactions before finalization; finalization MUST see the state after the last transaction. A block containing a transaction that fails the pre-execution checks of `B-07` (nonce, balance, gas pool, signer, blacklist, fee-delegation gate) is invalid.
Source: core/state_processor.go:60-107 (Process: loop, then engine.Finalize)
Observable: state

---

## 6. State rules after execution

[SNET-BODY-007] The header `GasUsed` MUST equal the sum of the gas used by all transactions as computed by execution ("invalid gas used (remote: %d local: %d)").
Source: core/block_validator.go:126-128
Observable: header, state

[SNET-BODY-008] The header `Bloom` MUST equal the OR of the receipt blooms ("invalid bloom (remote: %x  local: %x)").
Source: core/block_validator.go:129-134
Observable: header

[SNET-BODY-009] The header `ReceiptHash` MUST equal the Merkle-Patricia root of the consensus-encoded receipts ("invalid receipt root hash (remote: %x local: %x)"). The consensus receipt encoding is the go-ethereum one (type, status, cumulative gas used, bloom, logs); `EffectiveGasPrice` and the other derived fields are not part of it.
Source: core/block_validator.go:135-139
Observable: header

[SNET-BODY-010] The header `Root` MUST equal the state root obtained after execution and finalization, computed with deletion of empty accounts (`is_eip158(number)`, true on StableNet) and with the StableNet account encoding (`B-02` SNET-GEN-016) ("invalid merkle root (remote: %x local: %x) dberr: %w").
Source: core/block_validator.go:140-144
Source: consensus/wbft/engine/engine.go:967 (the same computation in finalization)
Observable: header, state

[SNET-BODY-011] State validation checks only SNET-BODY-007 to SNET-BODY-010. It does NOT check the header `GasTip` against the `GovValidator` contract, nor the blacklist status of the proposer; those are enforced by header verification (`B-08` SNET-SRC-020 at step H15b, `B-06` §5 at step H21) and by finalization (`B-06` §5). An implementation that folds these checks into its state validation MUST still reject exactly the same blocks.
Source: core/block_validator.go:124-146 (no gasTip or blacklist check)
Source: consensus/wbft/engine/engine.go:290-298, 329-348, 963-965

---

## 7. Global order of checks on the import path

A block is valid if and only if all of the checks below pass. The order of steps H1–H21 is normative as `A-08` specifies it (WBFT-HDR-080): a verifier applies them in the order given and returns the error of the first failing step, because the kind of error decides what the caller does next (a future block is queued and retried, an unknown ancestor is handled as such, a failed import is recorded as a bad block; `A-08` §6.2, `A-09` §5). This table restates that order and does not relax it; where the two differ, `A-08` is correct. The stages that follow (B1, E1, F1, S1) run in the order shown, after the header result. The owner column names the chapter that specifies the check.

| Step | Stage | Check | Owner |
|---|---|---|---|
| H1 | Header | `Number` present | `A-08` |
| H2 | Header | not in the future beyond `AllowedFutureBlockTime` | `A-08` |
| H3 | Header | `UncleHash` empty (SNET-BHDR-001) | `B-03` |
| H4 | Header | `Difficulty == 1` | `A-08` |
| H5 | Header | `GasLimit <= 2^63-1` (SNET-BHDR-002) | `B-03` |
| H6 | Header | not Shanghai (SNET-BHDR-003) | `B-03` (predicate in `B-01`) |
| H7 | Header | `WithdrawalsHash` absent (SNET-BHDR-004) | `B-03` |
| H8 | Header | not Cancun (SNET-BHDR-005) | `B-03` (predicate in `B-01`) |
| H9 | Header | blob and beacon-root fields absent (SNET-BHDR-006) | `B-03` |
| H10 | Header | stop here for `number == 0` | `A-08` |
| H11 | Header | parent known, number and hash match | `A-08` |
| H12 | Header | `parent.Time + block_period(number) <= Time` | `A-08` |
| H13 | Header | `GasUsed <= GasLimit` (SNET-BHDR-007) | `B-03` |
| H14 | Header | gas limit bound and base fee (SNET-BHDR-008 to SNET-BHDR-010) | `B-03` |
| H15a, H15b | Header | coinbase is a validator; coinbase not blacklisted at parent state (skipped when parent state is unavailable) | `A-08`, `B-08` SNET-SRC-020 |
| H16 | Header | extra decodes as `WBFTExtra` | `A-08` |
| H17 | Header | prepared and committed seals (full verification only) | `A-08` |
| H18, H19 | Header | randao reveal signature and mix | `A-08` |
| H20 | Header | previous-block seals (when required) | `A-08` |
| H21 | Header | `GasTip` equals parent-state contract value (a value mismatch fails; every other error of the check, including unavailable state, is skipped; `A-08` WBFT-HDR-111) | `B-06` §5 |
| B1 | Body | uncles, tx root, withdrawals, blobs, ancestor (SNET-BODY-001 to SNET-BODY-005) | `B-03` |
| E1 | Execution | transactions in order | `B-07` |
| F1 | Finalization | upgrades, base-fee distribution, epoch info verify or nil check, gas tip (no skip), state root | `B-06` |
| S1 | State | gas used, bloom, receipt root, state root (SNET-BODY-007 to SNET-BODY-010) | `B-03` |

The step labels `H1`-`H21` are those of `A-08` §6.2; `B1`, `E1`, `F1`, `S1` are local to this chapter.

Source: consensus/wbft/engine/engine.go:196-350 (H1-H21)
Source: core/blockchain_insert.go:118-134 (header result, then ValidateBody)
Source: core/blockchain.go:1587, 1779-1792 (VerifyHeaders, Process, ValidateState)
Source: core/state_processor.go:83-104 (E1, F1)

The proposal-voting path (`validate_proposal`, `A-08` steps P1-P7) checks the transaction root and the uncle hash of the body (P4, P5) and runs H1-H21 without H17; it does not run B1, E1, F1 or S1. See `B-06` §6.
