# A-04 Validators, epochs and proposer selection

- Areas: `VAL` (fault threshold, quorum, validator set per height), `EPOCH` (epoch boundaries, next-epoch computation, genesis epoch info), `PROP` (proposer selection)
- Status: draft
- Reference implementation: go-stablenet `740526d03`. The files used by this chapter are identical at `v1.1.0` except `consensus/wbft/engine/engine.go:1291` (gas-tip check, not used here).
- Depends on: `A-01` (types, `config_at`), `A-02` (`keccak256`), `A-03` (`WBFTExtra`, `EpochInfo`, `SealerSet` encodings). Used by: `A-05` (quorum, proposer, F+1 rule), `A-08` (seal and signer checks), `B-06` (finalization), `B-08` (binding of `candidates` and `bls_public_key`).

---

## 1. Overview (informative)

WBFT runs rounds of a QBFT-style protocol among a fixed, ordered validator set per block height. This chapter answers four questions that every other chapter relies on:

1. How many matching messages form a quorum, and how many trigger the early round change (section 2).
2. Which ordered validator set, with which BLS keys, seals block `n` (section 3).
3. Which blocks are epoch blocks, i.e. blocks whose header carries the `EpochInfo` of the following epoch (section 4).
4. Which validator is the proposer for view `(n, r)` (section 5), and how the `EpochInfo` of the next epoch is computed from the chain history and from contract state (section 6), including the genesis case (section 7).

The validator set changes only at epoch boundaries. An epoch block `e` closes the epoch `(L, e]`, where `L = last_epoch_block(e - 1)`; all blocks `L+1 .. e` are sealed by the set recorded in the header of `L`, and the header of `e` records the set for blocks `e+1 .. e_next`. The next set is computed deterministically by every node from (a) the previous-block seals recorded in the headers of the closing epoch, (b) the candidate list and BLS keys read from the post-state of block `e`, and (c) the randao mix of block `e`, which seeds a shuffle.

Notation used below: `D = DILIGENCE_DENOMINATOR = 1_000_000`, `DEFAULT_DILIGENCE = 1_900_000` (both defined in `A-01`, `core/types/istanbul.go:43,46`), `ZERO_ADDRESS` = 20 zero bytes. Integer division `//` is floor division (as in Python). In section 6 every dividend and divisor is non-negative, so there it equals truncation; `validators_diff` and `being` may be negative but are never divided.

---

## 2. Fault threshold and quorum

### 2.1 Definitions

The reference implementation computes the fault threshold as an IEEE-754 binary64 value and derives the quorum with a floating-point ceiling. The pseudocode reproduces that computation; section 2.2 gives the exact integer equivalents.

```python
def f_value(n: int) -> float64:
    # n - 1 is computed as a signed integer (n = 0 gives -1), then converted.
    return float64(n - 1) / float64(3)          # binary64 division, round-to-nearest-even

def quorum_size(n: int) -> int:
    return int(ceil(float64(n) - f_value(n)))   # binary64 subtraction, then ceiling

def f_plus_one_threshold(n: int) -> int:
    # The unique integer x with f_value(n) < x <= f_value(n) + 1 (see A-05, ROUND-CHANGE).
    return (n - 1) // 3 + 1                     # floor division; n = 0 gives 0
```

[WBFT-VAL-001] The fault threshold of a validator set of size `n` MUST be `f_value(n)` as defined above, including the binary64 rounding.
Source: consensus/wbft/validator/default.go:222 (F)

[WBFT-VAL-002] The quorum of a validator set of size `n` MUST be `quorum_size(n)`. For every `n < 3·2^50 + 3`, which includes every realistic set size, this equals `(2 * n) // 3 + 1`; `n = 3·2^50 + 3` is the first size at which binary64 rounding makes the two differ (`A-10` §2). Every consensus threshold that the reference implementation calls "quorum" uses this value: PREPARE and COMMIT quorums, ROUND-CHANGE quorum, justification, and the minimum number of sealers in an aggregated seal in a header.
Source: consensus/wbft/validator/default.go:226-229 (QuorumSize); consensus/wbft/core/prepare.go:121; consensus/wbft/core/commit.go:123; consensus/wbft/core/roundchange.go:146,170; consensus/wbft/core/preprepare.go:136; consensus/wbft/engine/engine.go:1341-1343 (verifyAggregatedSeal)
Observable: header, network

[WBFT-VAL-003] The early round-change rule of `A-05` compares a count `num` of validators with `float64(num) > f_value(n) and float64(num) <= f_value(n) + 1`. For every `n < 2^53 + 2` exactly one integer satisfies this window, namely `f_plus_one_threshold(n)`; an implementation MAY use the integer form.
Source: consensus/wbft/core/roundchange.go:161

Implementation note (informative). The equivalence in WBFT-VAL-002 holds for `n < 3·2^50 + 3`: when `(n - 1) mod 3 = 0` the division is exact and `n - f` is an integer; otherwise `n - f = (2n + 1)/3` has fractional part 1/3 or 2/3, far from any integer compared with the rounding error of the two binary64 operations while `n` is small. At `n = 3·2^50 + 3` the rounded `f` and the rounded subtraction land `n - f` on an integer, and `quorum_size(n)` is one smaller than `(2 * n) // 3 + 1` (`A-10` §2). The window of WBFT-VAL-003 holds exactly one integer for every `n < 2^53 + 2` (below 2^53 `float64(n - 1)` is exact and the rounding error of `f` stays below 1/3; `2^53` and `2^53 + 1` were computed); `n = 2^53 + 2` is the first size at which it holds `f_plus_one_threshold(n) - 1` instead. The equivalences of WBFT-VAL-002 and WBFT-VAL-003 were also checked by brute force for `0 <= n <= 3_000_000`, and around both bounds (Python, binary64 arithmetic).

### 2.2 Table

The table was produced by running the reference code: an overlay test file injected into package `consensus/wbft/engine` with `go test -overlay` (reference tree unchanged), calling `validator.NewSet(...).F()` and `.QuorumSize()` and scanning `num = 0 .. n+2` against the window of `roundchange.go:161`.

| N | F = `f_value(N)` (binary64, `%.17g`) | Q = `quorum_size(N)` | F+1 threshold | QBFT paper `ceil((N+f+1)/2)`, `f = (N-1)//3` | Quorum `ceil(2N/3)` |
|---|---|---|---|---|---|
| 0 | -0.33333333333333331 | 1 | 0 | 1 | 0 |
| 1 | 0 | 1 | 1 | 1 | 1 |
| 2 | 0.33333333333333331 | 2 | 1 | 2 | 2 |
| 3 | 0.66666666666666663 | 3 | 1 | 2 | 2 |
| 4 | 1 | 3 | 2 | 3 | 3 |
| 5 | 1.3333333333333333 | 4 | 2 | 4 | 4 |
| 6 | 1.6666666666666667 | 5 | 2 | 4 | 4 |
| 7 | 2 | 5 | 3 | 5 | 5 |
| 8 | 2.3333333333333335 | 6 | 3 | 6 | 6 |
| 9 | 2.6666666666666665 | 7 | 3 | 6 | 6 |
| 10 | 3 | 7 | 4 | 7 | 7 |
| 11 | 3.3333333333333335 | 8 | 4 | 8 | 8 |
| 12 | 3.6666666666666665 | 9 | 4 | 8 | 8 |
| 13 | 4 | 9 | 5 | 9 | 9 |
| 14 | 4.333333333333333 | 10 | 5 | 10 | 10 |
| 15 | 4.666666666666667 | 11 | 5 | 10 | 10 |
| 16 | 5 | 11 | 6 | 11 | 11 |
| 17 | 5.333333333333333 | 12 | 6 | 12 | 12 |
| 18 | 5.666666666666667 | 13 | 6 | 12 | 12 |
| 19 | 6 | 13 | 7 | 13 | 13 |
| 20 | 6.333333333333333 | 14 | 7 | 14 | 14 |
| 21 | 6.666666666666667 | 15 | 7 | 14 | 14 |
| 22 | 7 | 15 | 8 | 15 | 15 |

`N = 0` is not reachable on a valid chain (section 3.5) but is listed because the reference implementation still evaluates it when it cannot load a validator set.

### 2.3 Differences from QBFT (informative)

- The QBFT paper (Moniz, 2020) and ConsenSys Quorum's QBFT use a quorum of `ceil((N+f+1)/2)` and `ceil(2N/3)` respectively; both give the same values for `N >= 1` in the table. WBFT's `floor(2N/3) + 1` is one larger when `N mod 3 = 0` (`N = 3, 6, 9, ...`). For `N mod 3 = 0`, WBFT's `N - Q = (N - 1) // 3 = f` is one smaller than the `N - Q = N / 3 = f + 1` of the other two formulas: at these sizes WBFT keeps a quorum with one fewer crashed validator (N = 3: 0 instead of 1; N = 6: 1 instead of 2), and the intersection of two quorums is larger. For other `N` the three formulas coincide. [High] for the WBFT values (executed). [High] for the Quorum formula at commit `5ffacc48`: `QuorumSize` uses `ceil(2N/3)` only when no `2FPlus1Enabled` transition is active and `Ceil2Nby3Block` is set and not above the current sequence; otherwise it uses `2F + 1` with `F = ceil(N/3) - 1 = (N - 1) // 3` (`consensus/istanbul/qbft/core/core.go:306-313`, `consensus/istanbul/validator/default.go:205`). `Ceil2Nby3Block` defaults to 0 for genesis files with a `qbft` or `ibft` section (`consensus/istanbul/config.go:151`, `eth/ethconfig/config.go:340-359`) and is nil, hence `2F + 1` from block 0, for the deprecated `istanbul` section without `ceil2Nby3Block` (`eth/ethconfig/config.go:248`). The `ceil(2N/3)` column of section 2.2 is the first case. Quorum's `2F + 1` is smaller than WBFT's quorum for every `N ≢ 1 (mod 3)` (N = 3: 1 against 3; N = 6: 3 against 5). Independently, Quorum's header verification requires only `F + 1` committed seals (`consensus/istanbul/qbft/engine/engine.go:283`).
- The QBFT formal specification (commit `1630128e7`) defines `f(n) = (n − 1) div 3` and `quorum(n) = (2n − 1) div 3 + 1` (`dafny/spec/L1/node_auxiliary_functions.dfy:244-262`). For `N >= 1` its quorum equals `ceil(2N/3)`, the Quorum column of the table, so it is also one smaller than WBFT's when `N mod 3 = 0`. Its F+1 threshold `f(n) + 1` equals `f_plus_one_threshold(n)`.
- The code comment at `validator/default.go:227` says "ceil(2N/3)"; the code computes `ceil(N - F)`. The comment is Quorum's trace message for its `ceil(2N/3)` branch (`consensus/istanbul/qbft/core/core.go:311` at commit `5ffacc48`), kept as a comment. The code is normative.
- The F+1 rule uses a real-valued `F` and a window rather than `count >= f + 1`; the only triggering count is `f_plus_one_threshold(N)` (WBFT-VAL-003).

---

## 3. Validator set per height

### 3.1 Definitions

A `ValidatorSet` is an ordered list of entries `(address: Address, bls_public_key: bytes)` together with the proposer policy in force for that height. The position of an entry is its *validator index*; `SealerSet` bit `k` (`A-03`) refers to index `k`.

```python
def candidate_address(info: EpochInfo, i: uint32) -> Address:
    # Out-of-range indices are not an error here; they map to the zero address.
    if i < len(info.candidates):
        return info.candidates[i].addr
    return ZERO_ADDRESS

def epoch_info_for(chain, number: int, parent_hash: Hash, parents: [Header] = None) -> (int, EpochInfo):
    # EpochInfo that governs block `number` (number >= 1).
    # `parents` (optional): headers not yet stored, oldest first, ending with the parent of
    # `number` (batch verification, A-08 §6.1 and §6.7). In the walk-back they are consulted
    # before stored headers.
    # Lookups use the low 64 bits of `number`; uint64(number) - 1 is
    # uint64 arithmetic and wraps to 2^64 - 1 for number = k*2^64. `L` is computed from it
    # with unbounded arithmetic.
    L = last_epoch_block(uint64(number) - 1)
    epoch_header = header_by_number(L)       # canonical first: A-08 WBFT-HDR-071
    if epoch_header is None:
        # No canonical header stored at L: walk back from the parent of `number`,
        # first through `parents` (last element first), then through stored headers.
        pending = list(parents or [])
        h_hash, h_num = parent_hash, uint64(number) - 1
        while True:
            b = pending.pop() if pending else header(h_hash, h_num)  # A-09 chain queries
            if b is None:
                raise ErrUnknownAncestor
            if b.number == L:                                    # full value
                epoch_header = b
                break
            h_hash, h_num = b.parent_hash, uint64(b.number) - 1
    extra = decode_extra(epoch_header.extra) # A-03; decoding failure is an error
    if extra.epoch_info is None:
        raise Error("WBFT: epochInfo is nil")
    return L, extra.epoch_info

def governing_epoch_info(chain, h: Header) -> (int, EpochInfo):
    # Helper used by section 6: the genesis header governs itself.
    if h.number == 0:
        return 0, decode_extra(h.extra).epoch_info       # error if absent
    return epoch_info_for(chain, h.number, h.parent_hash)

def validators_at(chain, number: int, parent_hash: Hash, parents: [Header] = None) -> ValidatorSet:
    if number == 0:
        cfg = genesis_chain_config.anzeon.init
        entries = [(cfg.validators[k], hex_decode(cfg.bls_public_keys[k]))
                   for k in range(len(cfg.validators))]
        return ValidatorSet(entries, policy=base_config.proposer_policy)
    _, info = epoch_info_for(chain, number, parent_hash, parents)
    entries = [(candidate_address(info, info.validators[k]), info.bls_public_keys[k])
               for k in range(len(info.validators))]
    return ValidatorSet(entries, policy=config_at(number).proposer_policy)

def prev_validators_at(chain, header, parents: [Header] = None) -> ValidatorSet:
    # Set used to verify prev_prepared_seal / prev_committed_seal of `header`.
    if uint64(header.number) >= 2:
        if parents:
            # The last element is taken as the parent; its hash is not compared.
            parent, rest = parents[-1], parents[:-1]
        else:
            parent, rest = header(header.parent_hash, uint64(header.number) - 1), None
        if parent is None:
            raise ErrUnknownAncestor
        return validators_at(chain, parent.number, parent.parent_hash, rest)
    return validators_at(chain, header.number, header.parent_hash, parents)
```

`base_config` is the consensus configuration built from the chain configuration before transitions are applied; `config_at(n)` (`A-01`) applies all transitions with `block <= n`.

### 3.2 Requirements

[WBFT-VAL-004] `validators_at(0)` MUST be the list `anzeon.init.validators` of the chain configuration, in that order, each paired with the hex-decoded key at the same position of `anzeon.init.blsPublicKeys`. The genesis header's own `EpochInfo` (section 7) describes the same list.
Source: consensus/wbft/engine/engine.go:1104-1108 (GetValidators); params/config_wbft.go:68-74 (GetInitialBLSPublicKeys)

[WBFT-VAL-006] The order of a validator set MUST be the order of `EpochInfo.validators` (or of `anzeon.init.validators` for height 0). No sorting is applied. Validator indices in seals, proposer selection and quorum counting refer to this order.
Source: core/types/istanbul.go:177-187 (EpochInfo.GetValidators); consensus/wbft/validator/validator.go:35-41 (NewSet); consensus/wbft/validator/default.go:71-89 (newDefaultSet)
Observable: header, rpc

Implementation note (informative). `ProposerPolicy.By` holds a sort function (`ValidatorSortByString` by default, replaced by `ValidatorSortByByte` in `startWBFT`), but nothing calls it: `ValidatorSortByFunc.Sort` has no caller outside tests. The v0.1 draft's statement that ordering comes only from `EpochInfo` is correct. Source: consensus/wbft/config.go:45-66,100-103; consensus/wbft/types.go:192-212; consensus/wbft/backend/backend.go:355-358.

[WBFT-VAL-007] An entry of `EpochInfo.validators` that is not a valid index into `EpochInfo.candidates` MUST be mapped to `ZERO_ADDRESS`; this is not an error at lookup time. (Such an entry cannot appear in an `EpochInfo` computed by section 6.)
Source: core/types/istanbul.go:189-194 (GetCandidate)

[WBFT-VAL-009] If the governing epoch header of `number` cannot be found, cannot be decoded, or has no `EpochInfo`, `validators_at(number)` fails. A header whose current validator set cannot be determined MUST be rejected (the reference returns `consensus.ErrUnknownAncestor`). For the previous-block set, a missing parent header is reported as `consensus.ErrUnknownAncestor`; any other failure to determine the previous-block set is returned as the underlying error.
Source: consensus/wbft/engine/engine.go:1438-1447 (extractEpochInfo); consensus/wbft/backend/engine.go:420-450 (GetValidatorsForVerifying)
Observable: header

[WBFT-VAL-010] The previous-block seals of a header at height `n >= 2` MUST be verified against `validators_at(n - 1)`. For `n <= 1`, `prev_validators_at` returns `validators_at(n)`, but it is never used: previous-block seals are not verified when the parent is the genesis block (see `A-08`).
Source: consensus/wbft/backend/engine.go:429-447; consensus/wbft/engine/engine.go:322-327,384-388
Observable: header

[WBFT-VAL-011] The proposer policy attached to `validators_at(n)` MUST be `config_at(n).proposer_policy` for `n >= 1` and the base policy for `n = 0`.
Source: consensus/wbft/engine/engine.go:1106,1115; consensus/wbft/config.go:161-184 (GetConfig)

Implementation note (informative). The pseudocode follows the reference lookup: first by height in the canonical chain (`GetHeaderByNumber`), and only when no canonical header is stored at that height by walking back, first through `parents` from the last element (the batch of headers being verified, `A-08` §6.1), then from `parent_hash` through stored headers. Source: consensus/wbft/engine/engine.go:1414-1435.

Implementation note (informative). The consensus core obtains the set for the next sequence through `Backend.Validators(lastProposal)`, which returns an empty set when `validators_at` fails instead of reporting the error. Source: consensus/wbft/backend/backend.go:319-325. Consequences are in section 5.4.

RPC (informative). `istanbul_getValidators(number)` and `istanbul_getValidatorsAtHash(hash)` return the addresses of `validators_at(number)` in order. Source: consensus/wbft/backend/api.go:132-163; consensus/wbft/backend/engine.go:232-239.

---

## 4. Epoch boundaries

### 4.1 Algorithm

```python
def epoch_schedule(number: int) -> (int, int):
    # Returns (anchor, length) in force at `number`.
    length = base_config.epoch            # chain config anzeon.wbft.epochLength (>= 2, B-01)
    anchor = 0
    for t in transitions:                 # sorted by ascending t.block (B-01)
        if t.block > number:
            break
        if t.epoch_length == 0:           # transition does not change the epoch length
            continue
        anchor = t.block                  # every epoch-length transition block is an epoch block
        length = t.epoch_length
    return anchor, length

def is_epoch_block(number: int) -> bool:
    anchor, length = epoch_schedule(number)
    return (number - anchor) % length == 0

def last_epoch_block(number: int) -> int:
    anchor, length = epoch_schedule(number)
    return number - (number - anchor) % length

def epoch_length_of(e: int) -> int:
    # Number of blocks in the epoch closed by epoch block e >= 1.
    return e - last_epoch_block(e - 1)
```

[WBFT-EPOCH-001] A node MUST treat a block number `n` as an epoch block if and only if `is_epoch_block(n)` holds, with `epoch_schedule` as above. Block 0 is an epoch block.
Source: consensus/wbft/engine/engine.go:1073-1090 (IsEpochBlockNumber)
Observable: header

[WBFT-EPOCH-002] `last_epoch_block(n)` MUST be `n - ((n - anchor) mod length)`; it is the greatest epoch block `<= n`.
Source: consensus/wbft/engine/engine.go:1087-1089

[WBFT-EPOCH-003] A node MUST re-anchor the schedule at every transition with a non-zero `epochLength`: its block is an epoch block, and later epoch blocks are counted from it, even if the new length equals the old one. A transition whose `epochLength` is zero or absent, and that carries at least one other WBFT field, has no effect on the schedule.
Source: consensus/wbft/engine/engine.go:1076-1086

[WBFT-EPOCH-006] A block `n >= 1` that is not an epoch block MUST NOT carry an `EpochInfo` (the field is RLP-nil, `A-03`). A block that violates this is rejected during execution (`process_finalize`, `B-06`) with `ErrEpochInfoIsNotNil`.
Source: consensus/wbft/engine/engine.go:948-961 (processFinalize)
Observable: header

### 4.2 Worked example: transitions

Configuration: `epochLength = 10`; transitions `{block 25, epochLength 7}`, `{block 40, blockPeriodSeconds 2, proposerPolicy 1}`, `{block 50, epochLength 7}`. Values were produced by calling `Engine.IsEpochBlockNumber` and `Config.GetConfig` of the reference code (overlay test, see section 2.2).

Epoch blocks in `0 .. 70`: `0, 10, 20, 25, 32, 39, 46, 50, 57, 64`.

| n | is_epoch_block | last_epoch_block | policy at n | Note |
|---|---|---|---|---|
| 24 | no | 20 | 0 | old schedule |
| 25 | yes | 25 | 0 | transition block; epoch `(20, 25]` has 5 blocks |
| 26 | no | 25 | 0 | anchor 25, length 7 |
| 31 | no | 25 | 0 | |
| 32 | yes | 32 | 0 | |
| 45 | no | 39 | 1 | transition at 40 changes the policy only |
| 49 | no | 46 | 1 | |
| 50 | yes | 50 | 1 | re-anchored with the same length 7; epoch `(46, 50]` has 4 blocks |
| 56 | no | 50 | 1 | |
| 57 | yes | 57 | 1 | |
| 70 | no | 64 | 1 | |

A second example shows a one-block epoch: `epochLength = 4` and a transition `{block 5, epochLength 4}` give epoch blocks `0, 4, 5, 9, 13, ...`; the epoch `(4, 5]` has length 1.

---

## 5. Proposer selection

### 5.1 Algorithm

```python
STICKY = 1

def index_of(vs: ValidatorSet, addr: Address) -> int:
    for k, v in enumerate(vs.entries):
        if v.address == addr:
            return k                      # first match
    return -1

def last_proposer(chain, n: int) -> Address:
    # Input for sequence n (n >= 1): author of the last committed block.
    if n - 1 == 0:
        return ZERO_ADDRESS
    return header_at(n - 1).coinbase

def calc_proposer(vs: ValidatorSet, last: Address, round: uint64, policy) -> Entry | None:
    size = len(vs.entries)
    if size == 0:
        return None
    if last == ZERO_ADDRESS:
        seed = round
    else:
        offset = max(index_of(vs, last), 0)          # not found -> offset 0
        seed = offset + round                        # uint64, wraps modulo 2**64
        if policy.id != STICKY:                      # RoundRobin and every other id
            seed = seed + 1
    return vs.entries[seed % size]

def is_proposer(vs: ValidatorSet, proposer: Entry | None, addr: Address) -> bool:
    k = index_of(vs, addr)
    candidate = vs.entries[k] if k >= 0 else None
    # reflect.DeepEqual on the two entries: both absent -> True;
    # otherwise address and BLS key bytes must be equal.
    if proposer is None or candidate is None:
        return proposer is None and candidate is None
    return proposer.address == candidate.address and proposer.bls_public_key == candidate.bls_public_key
```

### 5.2 Requirements

[WBFT-PROP-001] The proposer of view `(n, r)` MUST be `calc_proposer(validators_at(n), last_proposer(n), r, config_at(n).proposer_policy)`. The same `last_proposer(n)` is used for every round of sequence `n`; only `r` changes.
Source: consensus/wbft/core/core.go:178,236,246 (startNewRound); consensus/wbft/validator/default.go:140-144 (CalcProposer)
Observable: network

[WBFT-PROP-002] `last_proposer(n)` MUST be the `Coinbase` of block `n - 1` when `n - 1 > 0`, and `ZERO_ADDRESS` when `n - 1 = 0`, regardless of the genesis header's `Coinbase`.
Source: consensus/wbft/backend/backend.go:327-342 (LastProposal); consensus/wbft/engine/engine.go:86-88 (Author)

[WBFT-PROP-003] Policy RoundRobin: a node MUST compute the proposer index as `r mod N` when `last = ZERO_ADDRESS`, and as `(offset(last) + r + 1) mod N` otherwise.
Source: consensus/wbft/validator/default.go:146-170 (calcSeed, roundRobinProposer)
Observable: network

[WBFT-PROP-004] Policy Sticky: a node MUST compute the proposer index as `r mod N` when `last = ZERO_ADDRESS`, and as `(offset(last) + r) mod N` otherwise.
Source: consensus/wbft/validator/default.go:172-184 (stickyProposer)
Observable: network

[WBFT-PROP-005] A node MUST select the Sticky policy if and only if the policy identifier is 1. Identifier 0, and every other value, selects RoundRobin.
Source: consensus/wbft/validator/default.go:83-86; consensus/wbft/config.go:37-42,60-62

[WBFT-PROP-006] `offset(last)` MUST be the index of the first entry whose address equals `last`, and 0 when no entry matches (for example when the previous author has left the set at an epoch boundary).
Source: consensus/wbft/validator/default.go:122-129,146-152

[WBFT-PROP-007] A validator MUST treat a PRE-PREPARE as coming from the proposer if and only if `is_proposer(vs, current_proposer, source)` holds, where `source` is the recovered sender address. Equality is `reflect.DeepEqual` of the proposer entry and the first entry with the sender's address: address and BLS key bytes must both match.
Source: consensus/wbft/validator/default.go:135-138 (IsProposer); consensus/wbft/core/preprepare.go:123-126
Observable: network

### 5.3 Worked examples

Four validators with indices 0..3 (addresses `0x..1000`..`0x..1003`); values from the reference `CalcProposer` (overlay test). Each cell lists the proposer index for rounds 0..5.

| last proposer | RoundRobin (id 0) | Sticky (id 1) | id 7 |
|---|---|---|---|
| `ZERO_ADDRESS` (sequence 1) | 0 1 2 3 0 1 | 0 1 2 3 0 1 | 0 1 2 3 0 1 |
| index 1 | 2 3 0 1 2 3 | 1 2 3 0 1 2 | 2 3 0 1 2 3 |
| index 3 | 0 1 2 3 0 1 | 3 0 1 2 3 0 | 0 1 2 3 0 1 |
| not a member | 1 2 3 0 1 2 | 0 1 2 3 0 1 | 1 2 3 0 1 2 |

### 5.4 Edge cases

---

## 6. Next-epoch computation

### 6.1 Inputs

The `EpochInfo` of epoch block `e >= 1` is a function of:

1. the headers `L+1 .. e` with `L = last_epoch_block(e - 1)`, specifically their `Coinbase` and their `prev_prepared_seal` / `prev_committed_seal` sealer bitmaps (signatures are not re-checked here; header verification has already checked them);
2. the `EpochInfo` of `L` (`latest`), of the epoch block governing block `L` (used to attribute the previous-block seals carried by block `L+1`), and of the epoch block governing block `L - 1` (`prior`, used to decide who was a validator before);
3. the header of `L` (its `Coinbase` starts the proposer replay);
4. `e_header.mix_digest`, the randao mix of block `e` (`A-02`), as the shuffle seed;
5. `post_state(e)`: the state after executing the transactions of block `e`, applying the system-contract upgrades scheduled at `e`, and distributing the base fee of `e`. Nothing modifies the state between the epoch computation and the computation of the state root, so `post_state(e)` is the state whose root is `e_header.root`;
6. the application interface operation `candidates(epoch_header, post_state)` (`A-09` §4.2; StableNet binding `candidates_at_epoch(state_e, e, upgrades)` in `B-08`), which returns the ordered, duplicate-free candidate list, each entry with its BLS public key (empty when none). Below, `bls_public_key(state, addr)` denotes the key that `candidates(e_header, state)` returns for `addr`; the reference reads the key separately and only for selected candidates, which is equivalent because both are reads of the same state;
7. `config_at(e).proposer_policy`.

Consequently the previous-block seals counted by the computation at `e` are those carried in blocks `L+1 .. e`, that is, the seals of blocks `L .. e-1`. The seals of block `e` itself are carried by block `e+1` and are counted by the computation at the next epoch block, attributed with the set that sealed `e`.

[WBFT-EPOCH-007] The state read by `candidates` and `bls_public_key` for epoch block `e` MUST be `post_state(e)` as defined above.
Source: consensus/wbft/engine/engine.go:929-970 (processFinalize: upgrades 930-939, base fee 941-946, epoch 948-953, root 967); core/state_processor.go:102
Observable: state

### 6.2 Algorithm

```python
def compute_next_epoch_info(chain, e_header: Header, state) -> EpochInfo:
    e = e_header.number
    assert is_epoch_block(e)                               # otherwise: no EpochInfo
    if e == 0:
        return initial_epoch_info(genesis_chain_config)    # section 7

    # ---- 1. seal accounting over blocks e, e-1, ..., L+1 -------------------------
    L, latest = governing_epoch_info(chain, e_header)      # L = last_epoch_block(e - 1)
    proposed  = defaultdict(int)   # seals carried in blocks the address authored
    submitted = defaultdict(int)   # seals the address contributed
    being     = defaultdict(int)   # proposer opportunities in the epoch
    proposers = []
    first_proposer = ZERO_ADDRESS
    last_prop      = ZERO_ADDRESS
    validators_diff = 0
    epoch_length = 0
    it = e_header
    while it.number != L:
        extra = decode_extra(it.extra)
        proposers.append(it.coinbase)
        parent = header(it.parent_hash, uint64(it.number) - 1)
        info = latest
        if parent.number == L:                             # it is block L+1
            _, info = governing_epoch_info(chain, parent)  # the set that sealed block L
            first_proposer = it.coinbase
            last_prop = parent.coinbase
            validators_diff = len(latest.validators) - len(info.validators)   # may be negative
        if parent.number == 0:
            being[it.coinbase] -= 1                        # block 1 carries no previous-block seals
        else:
            for seal in (extra.prev_prepared_seal, extra.prev_committed_seal):
                signers = signer_addresses(info, seal)
                proposed[it.coinbase] += len(signers)
                for a in signers:
                    submitted[a] += 1
        it = parent
        epoch_length += 1

    # ---- 2. who is (was) a validator ----------------------------------------------
    cmap = {}
    for c in latest.candidates:
        cmap[c.addr] = CandInfo(candidate=c, is_val=False, was_val=False)
    current = []
    for i in latest.validators:
        a = candidate_address(latest, i)
        cmap[a].is_val = True
        current.append(a)
    if L > 0:
        _, prior = governing_epoch_info(chain, header(it.parent_hash, uint64(L) - 1))  # governs block L-1
        for i in prior.validators:
            a = candidate_address(prior, i)
            if a in cmap:
                cmap[a].was_val = True

    # ---- 3. replay proposer opportunities, oldest block first ---------------------
    vs = ValidatorSet(zip(current, latest.bls_public_keys), policy=config_at(e).proposer_policy)
    for author in reversed(proposers):                     # blocks L+1, ..., e
        rnd = 0
        while True:
            if rnd >= len(current):
                raise Error("failed to find valid proposer")
            p = calc_proposer(vs, last_prop, rnd, vs.policy).address
            being[p] += 1
            if p == author:
                break
            rnd += 1
        last_prop = author

    # ---- 4. diligence ----------------------------------------------------------------
    E = epoch_length
    new_candidates = []
    for addr in [c.addr for c in candidates(e_header, state)]:
        ci = cmap.get(addr)
        if ci is None:
            d = DEFAULT_DILIGENCE
        else:
            s = submitted[addr]
            rate = E
            d = s * D // (2 * E)
            if not ci.is_val:
                rate = 1
                d = s * D // 2
            elif not ci.was_val:
                rate = E - 1
                if s > 2 * (E - 1):
                    raise Error("seal count exceed the range for non validator in prior epoch")
                d = s * D // (2 * (E - 1))                 # E == 1: division by zero
            w = being[addr]
            if w > 0:
                max_p = 2 * len(latest.validators) * w
                if addr == first_proposer and ci.was_val:
                    max_p -= 2 * validators_diff
                d += proposed[addr] * D // max_p
            else:
                d += D
            d = (ci.candidate.diligence * (10 * E - rate) + d * rate) // 10 // E
        if d > 2 * D:
            raise Error("WBFT: Invalid Diligence %d exceeds maximum" % d)
        new_candidates.append(Candidate(addr=addr, diligence=d))

    # ---- 5. selection and order ------------------------------------------------------
    order = sort_candidates(new_candidates)                # 6.4
    n = len(order)
    chosen = [order[compute_shuffled_index(i, n, e_header.mix_digest)] for i in range(n)]
    validators, keys = [], []
    for idx in chosen:
        pk = bls_public_key(state, new_candidates[idx].addr)
        if len(pk) == 0:
            continue                                        # candidate kept, not a validator
        validators.append(uint32(idx))
        keys.append(pk)
    return EpochInfo(candidates=new_candidates, validators=validators, bls_public_keys=keys)

def signer_addresses(info: EpochInfo, seal) -> [Address]:
    if seal is None:
        raise Error("empty seals")                          # ErrEmptySeals
    out = []
    for k in sealer_indices(seal.sealers):                  # ascending bit order, A-03
        a = candidate_address(info, info.validators[k])
        if a == ZERO_ADDRESS:
            raise Error("validator address is zero")
        out.append(a)
    return out
```

Arithmetic notes. The reference uses `uint64` for `d`, `s * D`, `proposed * D`, `E` and the smoothing product. With `N` the larger of the sizes of `latest` and of the set that sealed block `L`, `proposed[addr] <= 2 * N * E`, so `proposed * D` does not overflow when `2 * N * E * D < 2**64`; the smoothing product `D_prev * (10E - rate) + d * rate` does not overflow when `(20 * D + d) * E < 2**64`. The new term `d` is not bounded by `2D` before smoothing. All configured epoch lengths and set sizes are far below these bounds. `max_p` is computed as a signed integer and converted to `uint64`; it is positive whenever the previous set is non-empty. Operation order matters: the smoothing step divides by 10 and then by `E` (two truncations).

### 6.3 Requirements: accounting and diligence

[WBFT-EPOCH-008] The seal accounting for epoch block `e` MUST visit exactly the blocks `L+1 .. e` (with `L = last_epoch_block(e - 1)`) on the chain of `e`, and MUST count, for each visited block `b >= 2`, one `submitted` credit per sealer bit in `prev_prepared_seal` and one per sealer bit in `prev_committed_seal`, and `len(prepare signers) + len(commit signers)` `proposed` credits for `Coinbase(b)`.
Source: consensus/wbft/engine/engine.go:664-745
Observable: header

[WBFT-EPOCH-009] Sealer indices in the previous-block seals of block `L+1` MUST be resolved with the `EpochInfo` that governs block `L`; those of blocks `L+2 .. e` with `latest`. The two `EpochInfo` values differ whenever the set recorded at `L` differs from the set that sealed `L`; in that case the seals of block `L` are credited to the members of the old set, including members that are no longer validators.
Source: consensus/wbft/engine/engine.go:696-709

[WBFT-EPOCH-010] When block 1 is in the visited range, its author's `being` count MUST be decremented by one and no seals are counted for block 1.
Source: consensus/wbft/engine/engine.go:712-716

[WBFT-EPOCH-011] `is_val` MUST be true exactly for addresses referenced by `latest.validators`; `was_val` MUST be true exactly for candidates of `latest` referenced by the validators of the `EpochInfo` governing block `L - 1`, and false for all candidates when `L = 0`.
Source: consensus/wbft/engine/engine.go:747-772

[WBFT-EPOCH-013] The diligence of each address returned by `candidates(e_header, post_state(e))` MUST be computed exactly as in step 4, including the integer operation order. An address that is not a candidate of `latest` MUST receive `DEFAULT_DILIGENCE` (1,900,000). Candidates of `latest` that are no longer returned by `candidates` are dropped.
Source: consensus/wbft/engine/engine.go:806-872; core/types/istanbul.go:43,46
Observable: header, state

The smoothing weight `rate` is `E` for a validator in both the current and the prior epoch, `E - 1` for a validator that joined in the current epoch, and `1` for a candidate that is not in the current set:

| Case | Seal term `d_s` | Weight of the new term | Formula |
|---|---|---|---|
| `is_val` and `was_val` | `s*D // (2E)` | `E / 10E` | `(D_prev*(10E - E) + d*E) // 10 // E` |
| `is_val`, not `was_val` | `s*D // (2(E-1))`, error if `s > 2(E-1)` | `(E-1) / 10E` | `(D_prev*(9E + 1) + d*(E-1)) // 10 // E` |
| not `is_val` | `s*D // 2` | `1 / 10E` | `(D_prev*(10E - 1) + d) // 10 // E` |

In every case `d = d_s + d_p`, with `d_p = proposed*D // max_p` when `w > 0` and `d_p = D` when the address had no proposer opportunity.

### 6.4 Requirements: selection, order and shuffle

```python
def sort_candidates(cands: [Candidate]) -> [int]:
    # Indices 0..n-1 ordered by (power desc, diligence desc); power is 1 for every candidate.
    indices = list(range(len(cands)))
    sort_by_diligence_desc(indices, less=lambda i, j: cands[indices[i]].diligence > cands[indices[j]].diligence)
    return indices

def compute_shuffled_index(index: uint64, count: uint64, seed: bytes32) -> uint64:
    if index >= count:
        raise Error("input index %d out of bounds: %d" % (index, count))
    for r in range(33):                                        # SHUFFLE_ROUND_COUNT
        pivot_input = seed + uint8(r)                          # 33 bytes
        pivot = uint64_le(keccak256(pivot_input)[0:8]) % count
        flip = (pivot + count - index) % count
        position = max(index, flip)
        source_input = seed + uint8(r) + uint32_le(position >> 8)   # 37 bytes
        source = keccak256(source_input)
        byte = source[(position & 0xff) >> 3]
        bit = (byte >> (position & 0x07)) & 1
        if bit == 1:
            index = flip
    return index
```

`uint64_le(x)` reads 8 bytes little-endian; `uint32_le(v)` is the low 4 bytes of the little-endian encoding of the 64-bit value `v`. The algorithm is the Ethereum consensus-layer `compute_shuffled_index` with SHA-256 replaced by Keccak-256 and 33 rounds.

[WBFT-EPOCH-016] `sort_candidates` MUST order candidate indices by power descending, then diligence descending. In the reference every candidate has power 1, so the order is by diligence alone.
Source: consensus/wbft/engine/engine.go:875-884,1297-1320

[WBFT-EPOCH-018] `compute_shuffled_index` MUST be computed exactly as above, with `seed = e_header.mix_digest`.
Source: consensus/wbft/engine/engine.go:1449-1521,1270
Observable: header

[WBFT-EPOCH-019] The validator list MUST be `order[compute_shuffled_index(i, n, seed)]` for `i = 0 .. n-1`, in that order, over all `n` candidates, after which every entry whose candidate has an empty BLS key in `post_state(e)` is removed. The removed candidate keeps its place and diligence in `candidates`. `bls_public_keys[k]` MUST be the key read for `validators[k]`. No other filter applies: every candidate with a non-empty key becomes a validator, and the key's length and validity are not checked here.
Source: consensus/wbft/engine/engine.go:885-904,1265-1278
Observable: header, state

### 6.5 Requirements: writing and verification

[WBFT-EPOCH-020] The header of every epoch block `e >= 1` MUST carry `EpochInfo = compute_next_epoch_info(chain, e_header, post_state(e))`. A proposer computes it when assembling the block, after executing the transactions and before sealing, so the `EpochInfo` is covered by `block_hash` and by the seals.
Source: consensus/wbft/engine/engine.go:1053-1061 (FinalizeAndAssemble), 1193-1207 (writeEpoch)
Observable: header

[WBFT-EPOCH-021] A node MUST verify an epoch block by recomputing `compute_next_epoch_info` on a copy of the header and comparing it with the header's `EpochInfo`: the block is invalid if the `EpochInfo` is absent; if the numbers of candidates differ; if any candidate differs in address or diligence at the same position; if the numbers of validators differ or any validator index differs; if the numbers of BLS keys differ or any key differs byte-wise. This is equivalent to requiring equality of the three lists. Any error of the recomputation also makes the block invalid.
Source: consensus/wbft/engine/engine.go:919-921 (Finalize), 1212-1263 (verifyEpoch)
Observable: header, state

Implementation note (informative). A node that assembles an epoch block computes the `EpochInfo` only if its own address is in `validators_at(e)`; otherwise it leaves the field empty and the block fails verification elsewhere. Since only members of `validators_at(e)` may author block `e`, this affects only blocks that could not be valid anyway. Source: consensus/wbft/engine/engine.go:1193-1198.

### 6.6 Errors

The computation of section 6.2 fails, and the epoch block cannot be built or is rejected, in these cases. The reference does not distinguish them further; the text is the reference error message.

| Condition | Error text | Source |
|---|---|---|
| `IsEpochBlockNumber` fails (not reachable) | propagated | engine.go:652-654 |
| governing epoch header of `e`, of `L`, or of `L - 1` not found | `unknown ancestor` (`consensus.ErrUnknownAncestor`) | engine.go:1427-1429 |
| governing epoch header without `EpochInfo`, or its extra undecodable | `WBFT: epochInfo is nil` / decode error | engine.go:671-675,698-702,761-765,1438-1444 |
| a visited header or the header at `L - 1` not stored (unreachable after header verification) | process abort (nil dereference) | engine.go:695-697,760-761 |
| extra of a visited header undecodable | decode error | engine.go:682-686 |
| previous-block seal absent in a visited block `>= 2` | `empty seals` (`ErrEmptySeals`) | engine.go:717-721,729-733,1323-1325 |
| sealer index resolves to the zero address | `validator address is zero` | engine.go:1329-1332 |
| author not found in `N` replay rounds | `failed to find valid proposer` | engine.go:782-786 |
| new validator with `s > 2(E-1)` | `seal count exceed the range for non validator in prior epoch` | engine.go:838-840 |
| new validator with `E = 1` and `s = 0` (with `s > 0` the previous row applies) | process abort (division by zero) | engine.go:841 |
| diligence `> 2D` | `WBFT: Invalid Diligence %d exceeds maximum` | engine.go:864-866 |

Implementation note (informative). A sealer index at or beyond `len(info.validators)` makes `getSignerAddress` abort with an index-out-of-range panic. Header verification rejects such bitmaps first ("sealer is not validator", `engine.go:1347-1350`), so the path is unreachable for verified headers.

### 6.8 Worked example

The example was executed with the reference `buildEpochInfo` through an overlay test (a copy of the harness of `engine_test.go:TestEpochInfo`: in-memory header chain, GovValidator storage initialised from `govValidator.params`). Coinbases were computed with the reference `CalcProposer`. The `MixDigest` of block `n` was set to `keccak256(big_endian_minimal(n))` instead of a real randao mix, to keep the example self-contained.

Accounts (keys `keccak256("wbft-spec-a04-key-i")`): `v0 = 0x1FeEC135252C6152a4404606AAC45c4fd46E6321`, `v1 = 0x94c3726f51A447C80B5FccD3d00FA68eA4e0f025`, `v2 = 0x5f4ac72ecc55f372f81A918f975A0Ea6f960e658`, `v3 = 0xBB9F9a72edE4C0030c47D276EfAF66D07Fa91b8D`, `v4 = 0x36582928cFab0a09ccA37e823781479535890426`.

Setup: base epoch length 4 (epoch blocks 0, 4, 8, 12), policy RoundRobin, genesis `EpochInfo` with candidates `v0..v3` (diligence 1,900,000) and validators `[0,1,2,3]`, genesis `Coinbase` zero. The candidate list read at block 4 and later is `v0..v4` (v4 registered during epoch 1). At block 12 the BLS key of v2 has been cleared.

#### Epoch 1, blocks 1..4 (set `[v0, v1, v2, v3]`)

| Block | Commit round | Author | Prev prepared sealers | Prev committed sealers |
|---|---|---|---|---|
| 1 | 0 | v0 | (none, parent is genesis) | (none) |
| 2 | 1 | v2 | v0 v1 v2 v3 | v0 v1 v2 |
| 3 | 0 | v3 | v0 v1 v2 v3 | v0 v1 v2 v3 |
| 4 | 0 | v0 | v0 v1 v2 | v0 v1 v2 |

Computation at `e = 4`: `L = 0`, `E = 4`.

1. Seal accounting (blocks 4, 3, 2): `submitted = {v0: 6, v1: 6, v2: 6, v3: 3}`, `proposed = {v0: 6, v3: 8, v2: 7}`. Block 1: `being[v0] = -1`, `first_proposer = v0`.
2. `L = 0`, so `was_val` is false for everyone; `is_val` is true for `v0..v3`.
3. Replay from `ZERO_ADDRESS`: block 1 round 0 gives v0 (`being[v0] = 0`); block 2 round 0 gives v1, round 1 gives v2; block 3 round 0 gives v3; block 4 round 0 gives v0. Final `being = {v0: 1, v1: 1, v2: 1, v3: 1}`.
4. Diligence, all with `rate = E - 1 = 3` and `2(E-1) = 6`, `max_p = 2 * 4 * 1 = 8`:
   - v0: `6*D//6 = 1,000,000`, `+ 6*D//8 = 750,000`, smoothed `(1,900,000*37 + 1,750,000*3)//10//4 = 1,888,750`.
   - v1: `1,000,000 + 0`, smoothed `1,832,500`.
   - v2: `1,000,000 + 875,000`, smoothed `1,898,125`.
   - v3: `3*D//6 = 500,000`, `+ 8*D//8 = 1,000,000`, smoothed `1,870,000`.
   - v4: not a candidate of `latest`, `1,900,000`.
5. `order = [4, 2, 0, 3, 1]`; with the seed of block 4 (`0xf343681465b9efe82c933c3e8748c70cb8aa06539c361de20f72eac04e766393`) the shuffle maps `i = 0..4` to positions `[3, 4, 0, 2, 1]`, so `validators = [3, 1, 4, 0, 2]` (`v3, v1, v4, v0, v2`).

Executed output: candidates `v0 1888750, v1 1832500, v2 1898125, v3 1870000, v4 1900000`, validators `[3 1 4 0 2]`.

#### Epoch 2, blocks 5..8 (set `[v3, v1, v4, v0, v2]`)

| Block | Author (index) | Prev sealers (both seals) | Resolved with |
|---|---|---|---|
| 5 | v2 (4) | indices 0..3 | genesis `EpochInfo` (sealed block 4): v0 v1 v2 v3 |
| 6 | v3 (0) | indices 0..3 | `latest`: v3 v1 v4 v0 |
| 7 | v1 (1) | indices 0..3 | v3 v1 v4 v0 |
| 8 | v4 (2) | indices 0..3 | v3 v1 v4 v0 |

Computation at `e = 8`: `L = 4`, `E = 4`, `first_proposer = v2`, `validators_diff = 5 - 4 = 1`, prior set (governing block 3) is `v0..v3`, so v4 is the only validator with `was_val` false. `submitted = {v0: 8, v1: 8, v3: 8, v4: 6, v2: 2}`, `proposed = {v2: 8, v3: 8, v1: 8, v4: 8}`, `being = {v2: 1, v3: 1, v1: 1, v4: 1}`, `max_p = 10` (for v2: `10 - 2*1 = 8`).

- v0: `1,000,000 + 1,000,000` (no opportunity), smoothed `(1,888,750*36 + 2,000,000*4)//10//4 = 1,899,875`.
- v1: `1,000,000 + 800,000`, `1,829,250`.
- v2: `2*D//8 = 250,000`, `+ 8*D//8 = 1,000,000`, `(1,898,125*36 + 1,250,000*4) = 73,332,500`, `//10 = 7,333,250`, `//4 = 1,833,312`.
- v3: `1,000,000 + 800,000`, `1,863,000`.
- v4: `rate = 3`, `6*D//6 = 1,000,000`, `+ 800,000`, `(1,900,000*37 + 1,800,000*3)//10//4 = 1,892,500`.

Executed output: candidates `v0 1899875, v1 1829250, v2 1833312, v3 1863000, v4 1892500`, validators `[2 1 0 4 3]`.

#### Epoch 3, blocks 9..12 (set `[v2, v1, v0, v4, v3]`)

Block 9 author v3 (prev sealers: indices 0..3 of the epoch-2 set), block 10 author v2 (all five), block 11 committed in round 2 with author v4 (all five), block 12 author v3 (prepared: all five; committed: indices 0..3, v3 missing). All validators have `was_val` true; `first_proposer = v3`, `validators_diff = 0`. Replay gives `being = {v3: 2, v2: 1, v1: 1, v0: 1, v4: 1}` (block 11 credits v1 and v0 for rounds 0 and 1).

- v3: `s = 7`, `7*D//8 = 875,000`; `proposed = 17`, `max_p = 2*5*2 = 20`, `850,000`; smoothed `(1,863,000*36 + 1,725,000*4)//10//4 = 1,849,200`.
- v4: `1,000,000 + 10*D//10`, `1,903,250`.

Executed output: candidates `v0 1809887, v1 1746325, v2 1824980, v3 1849200, v4 1903250`, validators `[0 3 4 1]`: v2 was shuffled into the list and then removed because its BLS key is empty; it remains candidate 2 with its diligence.

#### Shuffle vectors

Values from the reference `computeShuffledIndex`. `seed = keccak256("wbft-spec-a04") = 0xf28e414632a7647b703bc5559648840eb265067bf241c4c7471284bd4b114c28`.

| count | `compute_shuffled_index(i)` for `i = 0 .. count-1` |
|---|---|
| 1 | 0 |
| 2 | 1 0 |
| 3 | 1 0 2 |
| 4 | 1 2 0 3 |
| 5 | 0 3 2 4 1 |
| 8 | 4 2 5 1 0 7 6 3 |

With the all-zero seed and count 4: `2 3 0 1`. `compute_shuffled_index(4, 4, seed)` fails with `input index 4 out of bounds: 4`.

Trace of the first three rounds for `index = 0`, `count = 4` and the seed above:

| r | `keccak256(seed ‖ r)[0:8]` | pivot | flip | position | `source[0..3]` | byte | bit | index after |
|---|---|---|---|---|---|---|---|---|
| 0 | `c5a417700cca37b6` | 1 | 1 | 1 | `8e480dcb` | `0x8e` | 1 | 1 |
| 1 | `e45652eb292b3c80` | 0 | 3 | 3 | `b7af3932` | `0xb7` | 0 | 1 |
| 2 | `a4eef6a40729094b` | 0 | 3 | 3 | `f4541406` | `0xf4` | 0 | 1 |

---

## 7. Genesis epoch information

```python
def initial_epoch_info(anzeon) -> EpochInfo:
    info = EpochInfo(candidates=[], validators=[], bls_public_keys=[])
    for i, addr in enumerate(anzeon.init.validators):
        info.candidates.append(Candidate(addr=addr, diligence=DEFAULT_DILIGENCE))
        info.validators.append(uint32(i))
        info.bls_public_keys.append(hex_decode(anzeon.init.bls_public_keys[i]))
    return info
```

[WBFT-EPOCH-023] The genesis header's `EpochInfo` MUST be `initial_epoch_info(anzeon)`: one candidate per entry of `anzeon.init.validators` in order, each with diligence 1,900,000, validators `[0, 1, ..., n-1]`, and the hex-decoded BLS keys in order. The genesis extra is built from the chain configuration, overwriting any `extraData` of the genesis file (the remaining fields, including `GasTip`, are specified in `B-02`).
Source: consensus/wbft/config.go:227-281 (CreateInitialExtraData, CreateInitialEpochInfo); core/genesis.go:242-262 (initializeAnzeonGenesis)
Observable: header

[WBFT-EPOCH-024] The chain configuration MUST have `len(anzeon.init.validators) = len(anzeon.init.blsPublicKeys) > 0`; a genesis that violates this is refused.
Source: params/config_wbft.go:76-94 (CheckValidity); core/genesis.go:231-239

Implementation note (informative). `compute_next_epoch_info` returns `initial_epoch_info` for block 0 ("if a transition occurs"). The genesis block is not executed through `process_finalize`, so this branch is not reached in normal operation. Source: consensus/wbft/engine/engine.go:659-662.
