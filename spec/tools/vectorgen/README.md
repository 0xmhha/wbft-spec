# vectorgen: conformance test-vector generator

`vectorgen` writes the test vectors of `A-11` §3 into `../../vectors/`. It computes every expected value by calling the reference implementation (go-stablenet `740526d03`) through a `replace` directive in `go.mod`, as the other tools in this directory do (option "scratch module with replace"). The reference repository is not modified.

Stage 1 (this directory's main program) covers the bottom layer (`A-01`, `A-02`, `A-03`), that is the runners `crypto` and `encoding`. Stage 2 (the program in `stage2/`) covers the runner `validators`, `timers/round_timeout`, `chain/config_at`, the runner `header` (including the batch verification `verify_headers`), the runner `execution` (`p256_verify`, `gas_tip_enforcement`, `fee_delegation`, `process_finalize`), the runner `source` (`candidates_at_epoch`) and the runner `governance` (`scenarios`). Stage 3 (the program in `stage3/`) covers the runner `state_machine` (`check_message`, `is_justified`, the step-wise `rounds`), `network/receive_outcome`, `timers/build_wait`, `chain/fork_schedule` and `chain/genesis`.

## Run

```
cd tools/vectorgen
GOTOOLCHAIN=go1.23.12 go run . -out ../../vectors
```

The program refuses to run if the toolchain is not `go1.23.12`, if the replaced module is not a git checkout whose `HEAD` starts with `740526d03`, or if that checkout has local changes. `meta.yaml` records the full commit hash read from the checkout. On another machine, set the `replace` path first (see `../README.md`).

Stage 2 is run the same way and makes the same checks:

```
GOTOOLCHAIN=go1.23.12 go run ./stage2 -out ../../vectors
```

It also runs `go test` in the reference checkout (see "Stage 2: how the reference is run") and checks afterwards that the checkout is still clean.

Stage 3 likewise (see "Stage 3: how the reference is run"):

```
GOTOOLCHAIN=go1.23.12 go run ./stage3 -out ../../vectors
```

Each program deletes and rewrites only the handler directories it generates (`vectors/crypto/<handler>/`, `vectors/encoding/<handler>/`); vectors of other handlers are left untouched. It prints the number of cases per handler.

`-skip-ref-check` disables the toolchain and checkout checks. It exists for development only; vectors written with it record the commit `unchecked` and are not valid vectors (WBFT-VEC-011, WBFT-VEC-020).

Requirements left out of an edition. An edition of the specification can leave some requirements out, and its vectors then do not name them. The generator source refers to such a requirement through an alias (`"@r01"` in a requirement list, `{@r01}` in a description) that `internal/vecfile/requirements.tsv` maps to the requirement ID; in the copy of the table published with an edition, the alias of every requirement left out of that edition maps to `-`. All three programs resolve the aliases before writing: a requirement mapped to `-` is omitted from `requirements:`, and a case whose description names it, or whose requirements are all omitted, is not written. `-withheld FILE` omits, in the same way, every requirement ID listed in `FILE` (one ID per line, `#` starts a comment). With the full table and without `-withheld` nothing is omitted. The programs print how many requirement references and cases they omitted.

## Checks

Determinism: run the generator twice and compare.

```
GOTOOLCHAIN=go1.23.12 go run . -out /tmp/v1 && GOTOOLCHAIN=go1.23.12 go run . -out /tmp/v2 && diff -r /tmp/v1 /tmp/v2
GOTOOLCHAIN=go1.23.12 go run ./stage2 -out /tmp/w1 && GOTOOLCHAIN=go1.23.12 go run ./stage2 -out /tmp/w2 && diff -r /tmp/w1 /tmp/w2
GOTOOLCHAIN=go1.23.12 go run ./stage3 -out /tmp/s1 && GOTOOLCHAIN=go1.23.12 go run ./stage3 -out /tmp/s2 && diff -r /tmp/s1 /tmp/s2
```

Subset check: `check_yaml_subset.py` parses every vector file with its own strict parser (standard library only) and rejects any file outside the YAML subset of `A-11` WBFT-VEC-013, a byte string that is not lowercase even-length hex (WBFT-VEC-014), a `meta.yaml` without the fields of WBFT-VEC-015, and a directory name or case layout that breaks WBFT-VEC-010 or WBFT-VEC-016. `--self-test` runs its built-in negative samples. Run it after every generation; it exits 1 on any problem.

```
python3 check_yaml_subset.py --self-test
python3 check_yaml_subset.py ../../vectors
```

Independent cross-check: `xcheck/xcheck.py` recomputes a subset of the expected values without go-stablenet (pycryptodome Keccak-256, python-ecdsa, pyrlp, and plain-Python secp256k1 recovery, BLS key derivation and BLS12-381 G1 arithmetic). It needs `pycryptodome`, `ecdsa`, `rlp` and `pyyaml`.

```
python3 xcheck/xcheck.py ../../vectors
```

| Handler | Recomputed with |
|---|---|
| `keccak256`, `randao_data`, `randao_mix`, `dedup_key` | Keccak-256 |
| `ecdsa_sign` | python-ecdsa, RFC 6979 with SHA-256, low S |
| `ecdsa_recover` | secp256k1 recovery with the cgo acceptance rules of `A-02` §3.3 |
| `bls_derive` | HKDF KeyGen (`A-02` §5.1) and G1 scalar multiplication |
| `aggregate_public_keys` | G1 decompression with the checks of WBFT-CRYPTO-023 and WBFT-CRYPTO-056, then point addition |
| `seal_data`, `block_hash`, `hash_with_round`, `filtered_header` | pyrlp and Keccak-256 |
| `signing_payload` | pyrlp re-encoding of the signed fields and secp256k1 recovery, including the embedded payloads |

Not cross-checked: BLS signing, verification and signature aggregation (these need hash-to-curve and pairings over G2), and the decoded fields of `extra_codec` and `message_codec`. Their expected values come only from the reference.

Stage 2 has its own cross-check, `xcheck/xcheck_stage2.py` (same packages). It recomputes the stage-2 values from the pseudocode of the specification, without go-stablenet:

```
python3 xcheck/xcheck_stage2.py ../../vectors
```

| Handler | Recomputed with |
|---|---|
| `quorum` | `A-04` §2.1 in binary64 (Python float), and `(2n)//3 + 1` |
| `proposer` | `A-04` §5.1 `calc_proposer` with a `uint64` seed |
| `epoch_boundary` | `A-04` §4.1 |
| `shuffle` | `A-04` §6.4 `compute_shuffled_index` (Keccak-256) |
| `validators_at` | `A-04` §3.1 `epoch_info_for` over the decoded fixture (canonical first, then walk back) |
| `next_epoch_info` | `A-04` §6.2 `compute_next_epoch_info` with the candidate order (a stable sort by diligence) and the shuffle above; the three epochs of `A-04` §6.8 are also compared with the diligence values printed there |
| `config_at` | `A-01` §6.5, transitions sorted by block (a stable sort) |
| `round_timeout` | `A-06` §4.1 and the Warn records of WBFT-TIMER-007 and WBFT-TIMER-008 |
| `build_proposal_header` | `A-08` §3.2: fixed fields, `Time`, untouched builder fields, gas tip, the previous-seal bitmaps (merge or unchanged), `MixDigest`, and the randao reveal (python-ecdsa, RFC 6979 with SHA-256, low S) |
| `verify_headers` | `A-08` §6.7 batch rules only: one verdict per header, `reject` for every header after the first one not accepted, and no `accept` for a header whose number does not follow the preceding header of the batch |

Not cross-checked in stage 2: `verify_header`, `verify_light` and the individual verdicts of `verify_headers` (they need BLS pairings), and the merged aggregate signatures of `build_proposal_header`.

The runners `execution` and `source` have their own cross-check, `xcheck/xcheck_execution.py` (same packages):

```
python3 xcheck/xcheck_execution.py ../../vectors
```

| Handler | Recomputed with |
|---|---|
| `p256_verify` | `B-07` SNET-TX-089: length rule, coordinate and range checks, curve membership, ECDSA verification over NIST P-256 in plain Python, 6 900 gas from `BohoBlock` on and none before |
| `gas_tip_enforcement`, `fee_delegation` | transactions decoded with pyrlp, sender and fee payer recovered over the signing hashes of `B-07` §4.2; per receipt the gas price of SNET-TX-012 with the tip of SNET-TX-010/011 and the `AuthorizedTxExecuted` log; the balances after the block from the flows of `B-07` §7.1 with the gas used of the receipts; for a fail case, that one of the rules of `B-07` §3 to §6 rejects a transaction |
| `process_finalize` | the base-fee distribution of `B-06` §3 from the `EpochInfo` of the fixture (shares, remainder to the coinbase) against the balances, the gas-tip comparison and the `EpochInfo` presence rule |
| `candidates_at_epoch` | the readers of `B-04` §8.2 over the input storage (the `AddressSet` at `0x33`, Solidity `bytes` at the mapping `0x37`, the word at `0x39`) |
| `scenarios` | the projection of every step from a model of `B-05` §8 and §10 driven by the call data and the logs (validator list with swap-and-pop, keys, gas tip, blacklist and authorized sets), and a `B-05` custom error for every revert |

Not cross-checked: state roots, the `EpochInfo` of an epoch block (its diligence needs the seal history of `A-04` §6), the gas used by a transaction, and the logs and write sets of the governance steps.

Stage 3 has its own cross-check, `xcheck/xcheck_stage3.py` (same packages), which recomputes from the pseudocode and tables of the specification:

```
python3 xcheck/xcheck_stage3.py ../../vectors
```

| Handler | Recomputed with |
|---|---|
| `check_message` | `A-05` §15 `check_message` |
| `is_justified` | `A-05` §11.6 `is_justified`, the verdict and the failing step named in the description |
| `rounds`, `receive_outcome` | for every message, backlog and frame step the `check` class from `check_message` on the snapshot of the step before and the view decoded from the payload (pyrlp); the `proposer` of every snapshot from `calc_proposer` (`A-04` §5.1) and the head read by the last view change; the view of every armed timer; for every timer step with `timer`, whether it may have an effect, from a model of the cancellation rules of `A-06` (a round timer cancels every armed timer, a retry or future timer the previous one of its kind, a stop all of them); no activity while the engine is stopped; the signer of every sent message with `encoded` (secp256k1 recovery over the signing payload of `A-03`); the sealers of a finalize against the stored PREPAREs and COMMITs |
| `receive_outcome` | the dedup key (Keccak-256), the outcome class of every frame from a model of `A-07` §5 and §8 (size limit, codes, empty payload, `0x11` unwrapping of the first RLP item, engine stopped, known cache), relays and own broadcasts only to connected validators other than the node and the sender |
| `fork_schedule` | `B-01` §3 predicates, the rule flags of SNET-CFG-005 and SNET-CFG-006, the system contracts in force and the upgrades (`B-01` §7), the EIP-2124 fork ID (CRC32 over the preset genesis hash, or the hash of the `chain/genesis` vector of the same configuration) |
| `genesis` | Keccak-256 of the header, the header fields of `B-02` §2, the genesis extra rebuilt from the initial validators of `B-01` §11.2 (`B-02` §3), the preset hashes of `B-01` §11.1 |

Not cross-checked in stage 3: the state root of a genesis block (it needs the system-contract storage of `B-02` §4), BLS seals, and the state snapshots beyond the checks above.

Tamper test: after changing one expected value in each of the six stage-3 handlers of a copy of `vectors/` (a class, a verdict, a `check`, a retry round, an outcome, a fork ID, a genesis extra), `xcheck_stage3.py` reports each of the seven changes; after writing a number without quotes, a comment line and an unterminated flow sequence into three files, `check_yaml_subset.py` reports each of the three.

## Adapter (WBFT-VEC-062)

`adapter/` answers the cases of these vectors over the adapter protocol `wbft-vector/1` of `A-11` §3.4. At start it runs the three generator programs into a temporary directory (with the same toolchain and reference checks), so every answer is a value computed from the reference in this run, and indexes the cases by runner, handler and input. It never reads the vector files that the runner uses (WBFT-VEC-041). A case whose input the generators did not write is answered `unsupported`; the adapter covers the vectors of its generator, not arbitrary inputs. A fail case is answered `error` with the reference error as `message`. `check_adapter.py` is a minimal runner that sends every case of a vector directory and decides it as WBFT-VEC-047 and WBFT-VEC-048 say.

```
GOTOOLCHAIN=go1.23.12 go build -o /tmp/vg-adapter ./adapter
GOTOOLCHAIN=go1.23.12 python3 check_adapter.py ../../vectors -- /tmp/vg-adapter
```

`-from DIR` serves the cases of an earlier generator run in `DIR` instead of generating them. A changed expected value shows as a failed case and a changed input as an unsupported one.

## Files

Each case is a directory `vectors/<runner>/<handler>/<case>/` with `meta.yaml`, `input.yaml` and, unless the operation must fail, `expected.yaml` (WBFT-VEC-010). The format rules (a YAML subset that converts to JSON one to one, integers as decimal strings, bytes as `0x` hex, `null` for absent values, informative `expected_error`) are in `A-11` §3.1 (WBFT-VEC-013 to WBFT-VEC-016); this generator's emitter (`yaml.go`) writes only that subset, and `check_yaml_subset.py` checks it.

| Source file | Content |
|---|---|
| `main.go` | stage 1 command line |
| `internal/vecfile/` | shared by both stages: deterministic YAML emitter, case type, reference checks, writer of a case directory and `meta.yaml` |
| `yaml.go` | aliases of the shared emitter types for the stage-1 sources |
| `fixtures.go` | keys (WBFT-VEC-021) and headers `H`, committed `H`, block 3 |
| `crypto.go` | runner `crypto` |
| `encoding.go` | `extra_codec`, `block_hash`, `hash_with_round`, `filtered_header` |
| `messages.go` | `message_codec`, `signing_payload`, `dedup_key` |
| `xcheck/xcheck.py` | independent cross-check of stage 1 |
| `check_yaml_subset.py` | YAML subset and case layout check (`A-11` WBFT-VEC-013 to WBFT-VEC-016) |
| `stage2/main.go` | stage 2 command line |
| `stage2/common.go` | keys, configuration shape, chain fixture (reader and builder) |
| `stage2/validators.go` | `quorum`, `proposer`, `epoch_boundary` |
| `stage2/config.go` | `chain/config_at` |
| `stage2/header.go` | chain fixtures, `validators_at`, `verify_header`, `verify_light`, `build_proposal_header`, `verify_headers` |
| `stage2/execution.go` | runner `execution`: `p256_verify`, `gas_tip_enforcement`, `fee_delegation`; the input state |
| `stage2/finalize.go` | `execution/process_finalize` |
| `stage2/source.go` | runner `source`: `candidates_at_epoch` |
| `stage2/governance.go`, `stage2/governance_cases.go` | runner `governance`: `scenarios` (the step runner and the scenarios of `B-05` §13) |
| `xcheck/xcheck_execution.py` | independent cross-check of the runners `execution`, `source` and `governance` |
| `adapter/main.go` | the adapter of WBFT-VEC-062 (`wbft-vector/1`) |
| `internal/vecfile/parse.go` | reader of the YAML subset (used by the adapter) |
| `check_adapter.py` | minimal runner of `wbft-vector/1` that checks the vectors against an adapter |
| `stage2/overlay.go` | runs the injected generator tests and reads their cases |
| `stage2/overlay/engine/zz_vectorgen_stage2_test.go` | injected into `consensus/wbft/engine`: `shuffle`, `sort_candidates`, `next_epoch_info` |
| `stage2/overlay/core/zz_vectorgen_stage2_test.go`, `zz_vectorgen_stage2_hook.go` | injected into `consensus/wbft/core`: `round_timeout` |
| `xcheck/xcheck_stage2.py` | independent cross-check of stage 2 |
| `internal/vecfile/jsonl.go` | reader of the JSON-lines cases written by injected generator tests (stages 2 and 3) |
| `stage3/main.go` | stage 3 command line |
| `stage3/chain.go` | `chain/fork_schedule`, `chain/genesis` |
| `stage3/overlay.go` | writes the overlay (added files and the one-line edits of "Stage 3: how the reference is run"), runs the injected generator tests and reads their cases |
| `stage3/overlay/core/zz_vectorgen_stage3.go` | added to `consensus/wbft/core` (non-test file): the hooks, the fake application `VGApp`, the step driver `VGDriver`, block and message builders over the vector keys |
| `stage3/overlay/core/zz_vectorgen_stage3_test.go` | injected into `consensus/wbft/core`: `check_message`, `is_justified`, `rounds` |
| `stage3/overlay/backend/zz_vectorgen_stage3_hook.go`, `zz_vectorgen_stage3_test.go` | injected into `consensus/wbft/backend`: `receive_outcome`, `build_wait` |
| `xcheck/xcheck_stage3.py` | independent cross-check of stage 3 |

Keys: `key_i = keccak256(ASCII("wbft-spec-vector-key-" ‖ decimal(i)))` for `i = 0 … 7` (stage 2: `i = 0 … 15`), the keys of `A-02` §8.1. One case (`aggregate_public_keys/decode_pk_x_plus_p`) searches further keys of the same seed for a BLS public key whose `x + p` fits in 381 bits and names the key index in its description.

## Handler inputs and outputs

This table is the handler schema of the specification (`A-11` §3.3 "Handler shapes", WBFT-VEC-032).

| Handler | `input.yaml` | `expected.yaml` |
|---|---|---|
| `crypto/keccak256` | `data` | `hash` |
| `crypto/ecdsa_sign` | `private_key`, `data` | `signature` (65 bytes), `address` |
| `crypto/ecdsa_recover` | `data`, `signature` | `address` |
| `crypto/bls_derive` | `private_key` (node key) | `secret_key`, `public_key` |
| `crypto/bls_sign` | `secret_key`, `message` | `signature` |
| `crypto/bls_verify` | `public_keys` (list, aggregated first), `message`, `signature` | `valid` (bool) |
| `crypto/bls_aggregate` | `signatures` (list) | `signature` |
| `crypto/aggregate_public_keys` | `public_keys` (list) | `public_key` |
| `crypto/seal_data` | `header` (RLP), `round`, `seal_type` | `seal_data` |
| `crypto/randao_data` | `chain_id`, `number` | `randao_data` |
| `crypto/randao_mix` | `parent_mix`, `reveal` | `mix` |
| `encoding/extra_codec` | `extra` | the ten decoded fields (absent seals and `epoch_info` are `null`) and `encoded` |
| `encoding/block_hash` | `header` (RLP) | `hash` |
| `encoding/hash_with_round` | `header`, `round` | `hash` |
| `encoding/filtered_header` | `header`, `round` | `header` (RLP) |
| `encoding/message_codec` | `code`, `payload` | `type`, the decoded fields, `encoded` |
| `encoding/signing_payload` | `code`, `payload` | `signing_payload`, `signer`, `embedded` (list of `signing_payload`, `signer`) |
| `encoding/dedup_key` | `payload` | `key` |
| `validators/quorum` | `n` | `f_float64_bits` (F as 8 bytes, big-endian binary64), `quorum`, `f_plus_one` |
| `validators/proposer` | `validators` (addresses), `policy` (identifier), `last_proposer`, `round` | `index`, `address` (both `null` for an empty set) |
| `validators/epoch_boundary` | `config` (`wbft`, `transitions`), `number` | `is_epoch_block` (bool), `last_epoch_block` |
| `validators/validators_at` | `chain`, `number`, `parent_hash` | `validators` (list of `address`, `bls_public_key`), `proposer_policy` |
| `validators/shuffle` | `seed`, `count`, `indices` (list) | `shuffled` (list, same order) |
| `validators/sort_candidates` | `diligences` (list; every power is 1) | `order` (candidate indices) |
| `validators/next_epoch_info` | `chain` (ends at the parent of the epoch block), `header` (the epoch header without `EpochInfo`), `candidates` (list of `address`, `bls_public_key`; `"0x"` for no key) | `epoch_info` (`candidates` as `address`, `diligence`; `validators`; `bls_public_keys`), `null` when `header` is not an epoch block |
| `timers/round_timeout` | `request_timeout` (ms, as `config_at` gives it), `max_request_timeout_seconds`, `round` | `timeout` (ns, signed), `warning` (`none`, `cap_overflow_guard`, `max_int64_clamp`) |
| `timers/build_wait` | `block_period` (seconds, as `config_at` gives it for the new height), `head_time` (Unix seconds), `round`, `now` (Unix nanoseconds) | `wait` (ns, at least 0) |
| `chain/config_at` | `config` (`wbft`, `transitions`), `number` | `request_timeout` (ms), `block_period`, `epoch`, `proposer_policy` (identifier or `null`), `max_request_timeout_seconds`, `allowed_future_block_time` |
| `header/build_proposal_header` | `chain`, `header` (the builder's skeleton, RLP), `node_key`, `gas_tip` (`null`: the query fails), `extra_prepared`, `extra_committed` (lists of `sealer`, `seal`), `now` | `header` (RLP) |
| `header/verify_header` | `chain`, `header` (RLP), `now` (Unix seconds) | `verdict` (`accept`, `reject`, `deferred`) |
| `header/verify_headers` | `chain`, `headers` (list of RLP, the batch in input order), `now` (Unix seconds) | `verdicts` (list, one of `accept`, `reject`, `deferred` per header) |
| `header/verify_light` | `chain`, `header` (RLP) | `result` (`VALID`, `INVALID`, `CANNOT_DECIDE`) |
| `state_machine/check_message` | `view` (`sequence`, `round`), `state` (`AcceptRequest`, `Preprepared`, `Prepared`, `Committed`), `prior_round`, `code`, `message_view` | `result` (`PROCESS`, `FUTURE`, `OLD`, `INVALID`, `TOO_FAR`, `EXTRA_SEAL`) |
| `state_machine/is_justified` | `proposal` (block hash), `target_view`, `round_changes` (list of `source`, `sequence`, `round`, `prepared_round`, `prepared_digest`), `prepares` (list of `source`, `sequence`, `round`, `digest`), `quorum` | `justified` (bool) |
| `state_machine/rounds` | `initial` (its `app` with `invalid_proposals`, `future_proposals`, `bad_blocks`, `finalize`), `steps` (`A-11` §3.1 "Steps", including the timer steps with `timer`, `future_timeout`, `stop` and `start`) | `start`, `steps` (one record per step; `timers` of kind `round`, `retry`, `future`) |
| `network/receive_outcome` | `initial` (with `engine`, `synchronising`, `peers`), `steps` (with `frame`) | `start`, `steps` (records with `outcome`, `dedup_key`, `relay_to`) |
| `chain/fork_schedule` | `config` (`preset`, `overrides`: `applepie_block`, `boho_block`, `shanghai_time`, `cancun_time`, `null` keeps the preset), `number`, `time` | `forks` (the predicates of `B-01` §3.1 and `anzeon`), `rules` (flags of a WBFT block), `system_contracts` (in force, `B-01` §7.2), `upgrades_at` (entries at `number`), `fork_id` (`hash`, `next`) |
| `execution/p256_verify` | `config` (`preset`, `overrides`: `applepie_block`, `boho_block`), `number`, `input` | `output`, `gas` |
| `execution/gas_tip_enforcement`, `execution/fee_delegation` | `config`, `header` (RLP), `accounts` (list of `address`, `balance`, `nonce`, `extra`, `code`, `storage`), `transactions` (binary encodings) | `receipts` (list of `status`, `gas_used`, `cumulative_gas_used`, `effective_gas_price`, `logs` of `address`, `topics`, `data`), `accounts` (`address`, `balance`, `nonce` after the block's transactions) |
| `execution/process_finalize` | `chain`, `header` (RLP), `parent_gas_tip` (`null`: parent state unavailable), `accounts` (after the transactions), `path` (`import`, `propose`) | `root`, `epoch_info` (written on the proposer path, else `null`), `accounts` (`address`, `balance`, `code_hash`) |
| `source/candidates_at_epoch` | `config`, `number`, `accounts` | `candidates` (list of `address`, `bls_public_key`), `gas_tip` |
| `governance/scenarios` | `genesis` (`preset`, `gov_validator`, `gov_council`, `alloc`), `genesis_time`, `named_accounts`, `steps` (`label`, `sender`, `to`, `data`, `gas`, `time`, `number`) | `steps` (`status`, `revert_data`, `logs`, `write_set` of `address`, `key`, `value`, `projection` of `validators`, `bls_public_keys`, `gas_tip`, `blacklisted`, `authorized`) |
| `chain/genesis` | `genesis` (`preset`, `overrides`: `timestamp`, `gas_limit`, `difficulty`, `number`, `extra_data`, `boho_block`, `alloc_add` (list of `address`, `balance`)) | `header` (RLP), `hash`, `state_root`, `extra` |

Single-key and single-signature decoding cases (WBFT-CRYPTO-056) are expressed through `aggregate_public_keys` and `bls_aggregate` with a list of one element, whose result is the re-compressed decoded point.

The shape of `chain` (a chain fixture, including the optional `non_canonical` headers) and of `config` is defined in `A-11` §3.1 ("Chain fixture"); the shape of a steps case in `A-11` §3.1 ("Steps"). The time-dependent cases use `now = 2000000000`; the generator refuses to run after that time, and its chains are timed so that the wall clock of the generating run gives the same result as `now` (headers to verify lie in the past, parents of proposals lie beyond `now`).

## Stage 2: how the reference is run

Exported functions are called in the stage-2 process: `validator.NewSet` (`F`, `QuorumSize`, `CalcProposer`), `Engine.IsEpochBlockNumber`, `eth/ethconfig.SetConfigFromChainConfig` with `Config.GetConfig` (the node's own path, which sorts the transitions), `Engine.GetValidators`, `backend.New` with `Backend.Prepare` (to build the fixture chains) and `Engine.Prepare` (for `build_proposal_header`, with the extra seals as arguments), `Engine.CommitHeader`, `Backend.VerifyHeader` (validator-set lookup V0a/V0b, then header verification with `check_seals`) and `Backend.VerifyHeaders` (the batch; all results are read in input order, so the abort channel is not used). The fixture chains are verified through their RLP as written into the vector.

The execution handlers build a `state.StateDB` in memory from the input `accounts` and call `vm.EVM.Call` (`p256_verify`; the 782 entries of `core/vm/testdata/precompiles/p256Verify.json` are read from the checkout and each result is compared with the file), `core.ApplyTransaction` in transaction order with `SetTxContext`, as `StateProcessor.Process` applies a block's transactions (`gas_tip_enforcement`, `fee_delegation`), and `Engine.Finalize` or `Engine.FinalizeAndAssemble` over the fixture chain, whose `StateAt` answers the parent gas tip (`process_finalize`). `candidates_at_epoch` calls `Engine.GetGovCandidates`, `systemcontracts.GetBLSPublicKey` at the address of `wbft.Config.GetSystemContracts` and `systemcontracts.GetGasTip`. The GovValidator storage of a case is written by the reference initializer `systemcontracts.GetSystemContractsTransition` (the genesis path), in two cases followed by one crafted change that the description names. The transactions are signed with the vector keys (`types.SignTx`, the Anzeon signer, and `types.NewFeeDelegateSigner` for the fee payer).

`governance/scenarios` builds the genesis with `core.SetupGenesisBlock` from the mainnet preset genesis with the governance parameters of the case, and applies every step with `core.ApplyMessage` in a block of its own, as `core.ApplyTransaction` does, with an EVM logger (`vm.Config.Tracer`) that notes every `SSTORE` slot and its value at the start of the call; the write set keeps the noted slots whose value changed. The projection is read with `systemcontracts.ValidatorList`, `GetBLSPublicKey`, `GetGasTip` and `StateDB.GetExtra`. The harness of `systemcontracts/test` is not used: it compiles the contracts with solc 0.8.14 and the OpenZeppelin sources, which the checkout does not contain; the contracts that run are the artifacts of the genesis in both cases.

The unexported functions run in generator tests that are injected with `go test -overlay` into the reference packages, the method of `../epoch-a04`. The overlay file is written into a temporary directory with absolute paths, so nothing has to be edited by hand:

- `consensus/wbft/engine`: `computeShuffledIndex`, `sortCandidates`, and `buildEpochInfo` on chains built in the test (coinbases from `CalcProposer`, previous-seal bitmaps with a placeholder signature, `MixDigest = keccak256(number)` as in `A-04` §6.8; the candidate list is read with `systemcontracts.ValidatorList` and `GetBLSPublicKey` from GovValidator storage that the reference initialises from `govValidator` parameters). The package cannot import `eth/ethconfig` (import cycle), so this test uses the reference's test copy `wbft.SetConfigFromChainConfig`, which has the same transition sort.
- `consensus/wbft/core`: `newRoundChangeTimer`. The timeout it computes is passed only to `time.AfterFunc`. The overlay replaces `core.go` by a copy in which exactly one line differs, `c.roundChangeTimer = vectorgenAfterFunc(timeout, func() {`, and adds `zz_vectorgen_stage2_hook.go` with `var vectorgenAfterFunc = time.AfterFunc`; the test swaps the variable to record the duration and reads the Warn records through a log handler. The generator refuses to run if the line does not occur exactly once.

The tests write one JSON object per case (integers as decimal strings, bytes as hex, keys in order), and the program converts them with the shared writer. `verify_light` has no reference function: its result is composed from `Engine.GetValidators`, `Engine.VerifyHeader` (parent resolved by hash, `check_seals` on) and `Engine.IsEpochBlockNumber`, and each case says so (`A-11` WBFT-VEC-020, handler shape 8).

`go.mod` of this module requires the reference's full dependency set for stage 2 (it imports `eth/ethconfig` and the backend), so `go.sum` holds the entries of the reference's `go.sum`.

Not generated: `quorum` at the sizes where binary64 rounding changes the result (`A-10` §2; such a set cannot be built), `verify_header` cases for H1 and H9 (not expressible in RLP, see `A-11` §3.3), H15b and H21 (a fixture has no state), and `build_proposal_header` with `now` above the parent time plus the block period (the reference reads the wall clock).

The fork cases of `validators_at` and `verify_header` (WBFT-HDR-071) use the fixture field `non_canonical`: `forkBuilder` in `stage2/header.go` builds the branch 4', 5' (and block 6') from the same keys and times as chainA, with another `EpochInfo` at 4'.

## Stage 3: how the reference is run

`fork_schedule` and `genesis` call exported functions in the stage-3 process: the fork predicates and `Rules` of `params.ChainConfig`, `wbft.Config.GetSystemContracts` after `eth/ethconfig.SetConfigFromChainConfig` (which appends the genesis entry and `CollectUpgrades`), `forkid.NewID`, and `core.SetupGenesisBlock` on an empty in-memory database, on copies of the presets.

The state-machine and network cases run the reference `Core` in generator tests injected with `go test -overlay` into `consensus/wbft/core` and `consensus/wbft/backend`. The generator drives the core synchronously, as `handleEvents` would: `handleEncodedMsg` then `Gossip` for a message, `handleDecodedMessage` then `Gossip` of the re-encoding for a backlog replay, `handleRequest` (and `storeRequestMsg` for a future request), `handleTimeoutMsg` for a round timeout (for a named timer only if its cancellation flag is not set, as `handleEvents` checks it), `broadcastRoundChange(r)` for a retry (for a named timer only if the core has not stopped it), the function of the future-PRE-PREPARE timer for a `future_timeout` (only if the core has not stopped the timer), `handleFinalCommittedMsg` for NewHead, `startNewRound(0)` for Start, `stopTimer` for a `stop` step, and `core.New` followed by `startNewRound(0)` for a `start` step (the backend creates a new core on every start, `backend.go:355-367`). The event loop is not run. The application is `VGApp`, whose answers come from the vector input; for the network runner its `Broadcast` and `Gossip` go to a real `backend.Backend` (with `HandleMsg`, the known and recent caches, and a fake peer set).

The overlay adds four files and replaces seven reference files by copies in which one line each (two in `preprepare.go` and in `backend.go`) is changed. The generator refuses to run if a line does not occur exactly once. Each changed line calls a hook that does exactly what the line did unless the generator has installed a recorder:

| File | Line | Replacement | Recorded |
|---|---|---|---|
| `consensus/wbft/core/core.go` | `c.roundChangeTimer = time.AfterFunc(timeout, func() {` | `vgRoundTimer(c, seq, round, timeout, func() {` | round timer armed for (seq, round) with its cancellation flag; it never fires |
| `consensus/wbft/core/core.go` | `c.retrySendingRoundChangeTimer = time.AfterFunc(timeout, func() {` | `vgRetryTimer(round, timeout, func() {` | retry timer armed with round; it never fires |
| `consensus/wbft/core/preprepare.go` | `c.futurePreprepareTimer = time.AfterFunc(duration, func() {` | `vgFutureTimer(preprepare, duration, func() {` | future-PRE-PREPARE timer armed for the view of the PRE-PREPARE; it never fires by itself |
| `consensus/wbft/core/preprepare.go` | `c.sendEvent(backlogEvent{` (inside that timer's function) | `vgPost(c, backlogEvent{` | backlog replay scheduled when a `future_timeout` step runs the timer's function |
| `consensus/wbft/core/backlog.go` | `go c.sendEvent(event)` | `vgSchedule(c, event)` | backlog replay scheduled (processed by a `backlog` step) |
| `consensus/wbft/core/request.go` | `go c.sendEvent(wbft.RequestEvent{` | `vgSchedule(c, wbft.RequestEvent{` | request replay scheduled (processed by a `request` step) |
| `consensus/wbft/backend/handler.go` | `go sb.istanbulEventMux.Post(wbft.MessageEvent{` | `vgPostReceived(sb, wbft.MessageEvent{` | message delivered to the core by `HandleMsg` |
| `consensus/wbft/backend/backend.go` | `go sb.istanbulEventMux.Post(msg)` | `vgPostSelf(sb, msg)` | self-delivery of a broadcast (processed by a `message` step) |
| `consensus/wbft/backend/backend.go` | `go p.SendWBFTConsensus(outboundCode, payload)` | `vgSend(p, outboundCode, payload)` | send to a peer (the `to` and `relay_to` fields) |
| `consensus/wbft/backend/engine.go` | `waitDuration = time.Until(time.Unix(int64(sb.timeForNextWork()), 0))` | `waitDuration = vgUntil(time.Unix(int64(sb.timeForNextWork()), 0))` | the clock of `NotifyNewRound` is the `now` of a `build_wait` case |

The changes only move a timer start, a post or a send out of a goroutine, or replace a clock read; the consensus logic is untouched. `VGApp` answers `FUTURE` (with a nominal duration of one second, which the vectors do not record) for the proposals of `future_proposals` until a `future_timeout` step for one of their timers.

`network/receive_outcome` applies two rules of the `eth` handler that are not in `HandleMsg`: the disconnect rule for handler errors (`eth/handler_istanbul.go:118-127`) and the size limit checked before `HandleMsg` (`:138-140`, `protocolMaxMsgSize` of `eth/handler.go:59`). The dedup key of a frame is the hash that `Backend.decode` returns (for `0x11` the first RLP item of the payload, trailing bytes ignored).

`timers/build_wait` runs `Backend.NotifyNewRound` with `currentBlock` returning a head of the given time, `notifyNewRound` recording the duration, and the clock hook set to `now`.

Normalisation. The reference iterates Go maps where the specification leaves the order open (the backlog sources in `processBacklog`, `wbftMsgSet.Values()` for the prepared certificate, the seal lists and the justification of a PRE-PREPARE). The records sort these sets (`A-11` §3.1 "Steps"), `encoded` is `null` for a message with a list of two or more members, and the self-delivery of such a message carries the encoding with sorted lists (`VGCanonical`). ECDSA (libsecp256k1, RFC 6979) and BLS signatures are deterministic. The generator was run three times with identical output.

## Maintenance

When the reference commit changes (`A-12` §4), update `ReferenceCommit` in `internal/vecfile/case.go` and the `replace` path, regenerate both stages, and review the diff of expected values. When a case is added or changed, bump `generatorVersion` of that stage (`main.go`, `stage2/main.go` or `stage3/main.go`). When the reference commit changes, stage 3 also refuses to run if one of its one-line edits no longer applies; check the replaced lines against the new code before adjusting them.

## Later stages

- Stages 2 and 3 are done (see above), including batch verification (`verify_headers`), future proposals, the timer steps and the size limit. The runners `execution` and `source` are generated by stage 2 with the state as input (`A-11` §3.1 "Execution state"). The runner `governance` is generated by stage 2 as well (`A-11` §3.1 "Governance scenarios"). Open: validator-set changes inside a steps case; the action vectors V-SRC-002 … 006, 009, 011 of `source/candidates_at_epoch`, which can now be written as governance steps followed by the reader.
- Not generated in stage 3: a frame of exactly 10 MiB that is accepted (it needs a valid PRE-PREPARE of that size and another file of 21 MB; the case above the limit is generated), and the duration of the future-PRE-PREPARE timer (`A-06` WBFT-TIMER-030 computes it inside `validate_proposal` from the header time and the clock; the steps carry no clock, so WBFT-TIMER-030 has no vector).
