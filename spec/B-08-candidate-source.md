# B-08 Candidate source

- Part: B (StableNet block validity)
- Area code: `SRC`
- Status: draft
- Reference implementation: go-stablenet `740526d03`

Part A treats the execution layer as an abstract application (`A-09`). Three of its operations depend on StableNet contract state: the candidate list with BLS keys used to compute the next epoch, the proposer-eligibility check, and the gas tip that every header carries. This chapter binds each operation to contract state: which state (by block), which contract address, which slots, which algorithm, and what happens when the data is missing.

The storage layouts and reader functions (`validator_list`, `bls_public_key`, `gas_tip`, `system_contracts_at`, the `Extra` bits) are defined in `B-04`. The governance actions that change this state are defined in `B-05`.

---

## 1. Summary of the bindings (informative)

| Part A operation | Invoked for | State read | Contract address | Reader (`B-04`) |
|---|---|---|---|---|
| candidates at genesis | block 0 | none: `anzeon.init` configuration | — | — |
| proposer eligibility (blacklist) | every block `n > 0`, for `header.coinbase` | state of the parent, `S(n−1)` | — (account field) | `Extra` bit 63 |
| gas tip | every block `n > 0` | state of the parent, `S(n−1)` | genesis `GovValidator` | `gas_tip` |

`S(k)` denotes the state committed by block `k`, that is, the state whose root is `header(k).root`.

Note on naming. The abstract operation is `candidates(epoch_header, post_state)` (`A-09` §4.2). Its StableNet binding is `candidates_at_epoch(state_e, e, upgrades)` (section 2.4). `is_eligible_proposer(parent, addr)` and the gas tip do read the parent state (sections 3 and 4).

---

## 2. Candidates and BLS keys for the next epoch

### 2.1 When the candidate list is read

The candidate list is read once per epoch, while finalizing an epoch block. Epoch blocks are defined in `A-04` (`is_epoch_block`). Both the proposer (`writeEpoch`, when it assembles the block) and every verifier (`verifyEpoch`, when it imports the block) run the same computation on their own execution of the block, and the verifier compares its result with the `EpochInfo` in the header.

[SNET-SRC-001] For every epoch block `e > 0`, the node MUST compute the next epoch's candidate list from contract state during finalization of `e`, and a verifier MUST reject block `e` unless the `EpochInfo` in `e`'s header equals the one it computes (candidates element-wise by address and diligence, validator indices element-wise, BLS keys element-wise by bytes).
Source: consensus/wbft/engine/engine.go:947-958 (processFinalize, epoch step), consensus/wbft/engine/engine.go:1193-1208 (writeEpoch), consensus/wbft/engine/engine.go:1212-1263 (verifyEpoch)
Observable: header

[SNET-SRC-002] For block 0, the candidate list MUST NOT be read from contract state; the genesis `EpochInfo` is built from `anzeon.init.validators` and `anzeon.init.blsPublicKeys` (`B-02`).
Source: consensus/wbft/engine/engine.go:659-662, consensus/wbft/config.go:258-281 (CreateInitialEpochInfo)
Observable: header

Implementation note (informative). Because block 0's candidates come from `anzeon.init` and every later epoch's come from `GovValidator` storage, a genesis whose `anzeon.init.validators` differs from the `GovValidator` `validators` parameter changes the validator set at the first epoch block. `B-02` specifies the genesis consistency rules.

### 2.2 Which state

Finalization of block `e` performs, in order: (1) system-contract upgrades for block `e` (`B-04` SNET-SYS-021), (2) base-fee distribution (`B-06`), (3) the epoch step, (4) gas-tip verification, (5) state-root computation.

[SNET-SRC-004] Since steps (2) to (5) do not write `GovValidator` storage, the values read in the epoch step MUST equal the values of the same slots in `S(e)`. An observer MAY therefore recompute the candidate list of epoch block `e` from `eth_getStorageAt(gv, slot, e)`.
Source: consensus/wbft/engine/engine.go:972-1051 (distributeBaseFee writes balances only), consensus/wbft/engine/engine.go:1280-1295 (verifyGasTip reads only)
Observable: state, rpc

### 2.4 Read algorithm

```python
def candidates_at_epoch(state_e, e: int, upgrades) -> list[tuple[Address, bytes]]:
    """state_e: the state defined in section 2.2. Returns ordered (address, bls_key) pairs."""
    gv = system_contracts_at(e, upgrades).GovValidator.Address      # B-04 section 4.3
    addrs = validator_list(state_e, gv)                               # B-04 section 8.2, array order
    return [(a, bls_public_key(state_e, gv, a)) for a in addrs]      # b"" when absent

def epoch_info_from_candidates(pairs, prev_epoch_data, header_e) -> EpochInfo:
    # Candidates: every address, in list order, with diligence from A-04.
    # diligence(...) stands for step 4 of A-04 compute_next_epoch_info (not a separate function).
    candidates = [Candidate(addr=a, diligence=diligence(a, prev_epoch_data)) for a, _ in pairs]
    # Validator order: step 5 of A-04 compute_next_epoch_info (sort_candidates, then
    # compute_shuffled_index with header_e.mix_digest); reference decideValidators.
    order = decide_validators(candidates, header_e.mix_digest)       # list of candidate indices
    validators, keys = [], []
    for idx in order:
        key = pairs[idx][1]
        if len(key) == 0:
            continue                                                  # skipped, stays a candidate
        validators.append(idx)
        keys.append(key)
    return EpochInfo(candidates=candidates, validators=validators, bls_public_keys=keys)
```

[SNET-SRC-006] The `Candidates` of the new `EpochInfo` MUST be exactly the addresses returned by `validator_list`, in the same order, one entry per address. The order is normative: validator indices refer to it, and the tie order of the ordering step of `A-04` depends on it.
Source: consensus/wbft/engine/engine.go:806-866 (newEpoch.Candidates built in `newCandidates` order)
Source: consensus/wbft/engine/engine.go:1297-1320 (sortCandidates: unstable sort over the candidate indices)
Observable: header

[SNET-SRC-007] Every candidate MUST be offered to validator selection; in StableNet all candidates have equal power (1). Validator selection and ordering are defined in `A-04`.
Source: consensus/wbft/engine/engine.go:876-889 (PoweredCandidate with Power 1, decideValidators over all candidates)
Observable: header

[SNET-SRC-008] For each selected candidate index, in the selection order, the node MUST read `bls_public_key` from the state and address used for the candidate list. If the key is empty, the index MUST be omitted from `Validators` and no key appended; otherwise the index is appended to `Validators` and the key bytes, unmodified, to `BLSPublicKeys`. A candidate omitted this way remains in `Candidates`.
Source: consensus/wbft/engine/engine.go:890-905
Observable: header

[SNET-SRC-009] The node MUST NOT check the length or validity of a non-empty BLS key read from storage when building `EpochInfo`. (A key that is not a valid 48-byte compressed G1 point makes that validator's seals unverifiable under `A-02`; it cannot be registered through `configureValidator`, which enforces 48 bytes and a proof of possession, `B-05`.)
Source: consensus/wbft/engine/engine.go:894-905, systemcontracts/gov_validator.go:208-210
Observable: header

### 2.5 Edge cases

| Situation | Reader result | Resulting `EpochInfo` | Normative behaviour at the reference commit |
|---|---|---|---|
| Some candidates have no BLS key | addresses returned, keys `b""` | those candidates present, not validators | accepted |

---

## 3. Proposer eligibility

In the reference implementation, eligibility to produce block `n` has two parts. Membership (the coinbase is a validator of the epoch that seals `n`) is a Part A rule (`A-08`). The StableNet part is the blacklist.

[SNET-SRC-020] A block `n > 0` whose `coinbase` has bit 63 of `Extra` set in `S(n−1)` MUST be rejected.
Source: consensus/wbft/engine/engine.go:352-381 (verifySigner: validator-set membership, then `state.IsBlacklisted(signer)` on `StateAt(parent.Root)`)
Source: consensus/wbft/engine/engine.go:86-88 (Author = header.Coinbase)
Observable: header, state

[SNET-SRC-022] Proposer selection (`A-04`, `calc_proposer`) MUST NOT skip a blacklisted validator. A blacklisted validator that is selected as proposer cannot produce a valid block with its own `Coinbase`; unless it re-proposes a block prepared in an earlier round (which keeps its original `Coinbase`), the round ends by timeout and round change (`A-05`, `A-06`).
Source: consensus/wbft/engine/engine.go:1265-1278 (decideValidators), consensus/wbft/engine/engine.go:806-905 (no blacklist input to validator selection)
Observable: header

---

## 4. Gas tip

Every header `n > 0` carries a gas tip in its `WBFTExtra` (`A-03`). The execution-side use of the gas tip (minimum priority fee for non-authorized senders) is in `B-07`; this section defines where the header value comes from.

```python
def expected_gas_tip(n: int, chain) -> int:
    parent = chain.header(n - 1)
    if parent is None:
        raise UnknownAncestor
    if parent.root == ZERO_HASH:
        raise Error("parent state root is empty")
    s = chain.state_at(parent.root)                       # may raise StateUnavailable
    gv = chain.config.anzeon.systemContracts.GovValidator.Address   # genesis address
    return gas_tip(s, gv)                                 # B-04 section 8.2; 0 if absent
```

[SNET-SRC-030] The gas tip in the header of block `n > 0` MUST be present and MUST equal `gas_tip(S(n−1), gv0)`, where `gv0` is the `GovValidator` address in `anzeon.systemContracts`.
Source: consensus/wbft/engine/engine.go:620-645 (getGasTip), consensus/wbft/engine/engine.go:1280-1295 (verifyGasTip: nil or different value is a mismatch)
Observable: header, state

[SNET-SRC-031] A proposer MUST write `expected_gas_tip(n)` into the header it proposes.
Source: consensus/wbft/engine/engine.go:511-513, :518, :537, :542 (Prepare → WriteGasTip)
Observable: header

[SNET-SRC-032] A verifier MUST check SNET-SRC-030 during block execution (finalization) unconditionally; during header-only verification it MUST check it when `S(n−1)` is available and MUST skip it, not reject the header, when the gas tip cannot be determined for any reason other than a mismatch (`A-08` WBFT-HDR-111, `B-06` SNET-FIN-019).
Source: consensus/wbft/engine/engine.go:962-965 (processFinalize → verifyGasTip, error returned)
Source: consensus/wbft/engine/engine.go:329-348 (header verification: mismatch is fatal, state errors are skipped)
Observable: header

---

## 5. State availability (informative summary)

| Check | Needs | If the state is missing during header verification | During execution |
|---|---|---|---|
| Candidates and BLS keys (epoch) | post-transaction state of `e` | not checked (header verification does not compute `EpochInfo`) | always checked (SNET-SRC-001) |
| Gas tip | `S(n−1)` | skipped | always checked (SNET-SRC-032) |

---

## 6. Alternative source: native validator module

A native Go validator module replicates the `GovValidator` and `GovBase` semantics. In compatibility mode (the node joins a network with go-stablenet nodes), the contract storage remains the source of truth and the module is a mirror.

### 6.3 Cross-check vectors

Each vector gives a starting state, an action sequence, and the expected observable results. "List" is `validator_list` at the named state; `V0..V3` are validators with operators `O0..O3` and keys `K0..K3`. All vectors except V-SRC-007, 008 and 010 can be produced with ordinary transactions on a test network; those three need a crafted genesis or upgrade configuration.

| ID | Setup and actions | Expected |
|---|---|---|
| V-SRC-001 | testnet preset genesis | list = the 7 testnet validators in parameter order (`B-04` section 9.2); gas tip 27 600 000 000 000 |
| V-SRC-002 | genesis `[V0..V3]`; `O0` configures new address `N` | list `[V3, V1, V2, N]`; key of `V0` empty; key of `N` = new key |
| V-SRC-003 | genesis `[V0..V3]`; `O1` configures `V1` with key `K1'` | list unchanged; key of `V1` = `K1'`; `blsKeyToValidator[K1]` zero |
| V-SRC-004 | genesis `[V0..V3]`; member `O3` removed by proposal | list `[V0, V1, V2]` (last element removed, no swap) |
| V-SRC-005 | genesis `[V0..V3]`; member `O1` removed by proposal | list `[V0, V3, V2]` |
| V-SRC-006 | genesis `[V0..V3]`; member `O2` calls `changeMember(O2')` | list unchanged; `operatorToValidator[O2'] = V2` |
| V-SRC-007 | genesis where one `validators` entry is repeated | list has the first occurrence only; the operator of each entry is the member at the same parameter position |
| V-SRC-008 | state where `V2` is in the set but `validatorToBlsKey[V2]` is empty | `V2` in `Candidates`, not in `Validators` of the next `EpochInfo` |
| V-SRC-009 | `configureValidator` transaction included in epoch block `e` | the change is in `EpochInfo(e)` (not deferred to the next epoch block) |
| V-SRC-011 | gas-tip proposal executed in block `k` | header `k` carries the old tip, header `k+1` the new one |
