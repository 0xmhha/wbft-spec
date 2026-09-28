# Name table

This file is the canonical name table of the specification. Every chapter uses the names below with the meaning given; the chapter in the "Defined in" column holds the normative definition, and other chapters refer to it. Where a chapter restates a function of another chapter, the defining chapter governs. The table was merged from the "New names" lists of all chapters in the editorial pass of 2026-09-24; section 8 lists the names retired in that pass.

---

## 1. Types

| Name | Meaning | Defined in |
|---|---|---|
| `Address` | 20-byte account address | A-01 |
| `Hash` | 32-byte Keccak-256 digest | A-01 |
| `bytesN`, `bigint` | fixed-size byte string; unbounded non-negative integer (RLP big integer) | A-01 |
| `Sequence` | block height being agreed on (message field; big integer on the wire) | A-01 |
| `Round` | round number within a sequence (big integer on the wire, `uint32` in the header extra) | A-01 |
| `View` | pair `(sequence, round)`; ordered by sequence, then round | A-01 |
| `Proposal` | a block proposed for a view (full block: header and body) | A-01 |
| `Digest` | `block_hash(proposal)`: hash of the header with the current-block seals removed and round set to 0 | A-01, A-03 |
| `ECDSASignature` | 65 bytes `R ‖ S ‖ V` | A-01 |
| `BLSPublicKey` | 48 bytes (G1 compressed) | A-01 |
| `BLSSignature` / `Seal` | 96 bytes (G2 compressed) | A-01 |
| `SealType` | `PREPARE_SEAL = 0x00`, `COMMIT_SEAL = 0x01` | A-01 |
| `SealerSet` | bitmap of validator indices | A-01, A-03 |
| `AggregatedSeal` | `(sealers: SealerSet, signature: BLSSignature)` | A-01, A-03 |
| `SealData` | `(sealer, seal)`: one element of the seal lists handed to `finalize` | A-05 |
| `WBFTExtra` | consensus data in `Header.Extra`; fields `vanity_data`, `randao_reveal`, `prev_round`, `prev_prepared_seal`, `prev_committed_seal`, `round`, `prepared_seal`, `committed_seal`, `gas_tip`, `epoch_info` | A-01, A-03 |
| `EpochInfo` | fields `candidates`, `validators` (indices into `candidates`), `bls_public_keys` | A-01, A-03 |
| `Candidate` | `(addr: Address, diligence: uint64)`, an element of `EpochInfo.candidates` | A-01, A-03 |
| `ValidatorSet` | ordered list of `(address, bls_public_key)` for one height, with the proposer policy of that height | A-01, A-04 |
| `Config` | consensus configuration for one height (after transitions) | A-01 |
| `Transition` | height-dependent consensus override | A-01 §6.4 |
| `Upgrade` | system-contract upgrade entry `(block, system_contracts)` | B-01 §7 |
| `ConsensusState` | enum of the node states `AcceptRequest`, `Preprepared`, `Prepared`, `Committed` | A-05 |
| `RoundState`, `RoundChangeSet`, `PriorState`, `Node` | node state records | A-05 §3 |
| `CheckResult` | result classes of `check_message`: `PROCESS`, `FUTURE`, `OLD`, `INVALID`, `EXTRA_SEAL`, `TOO_FAR` | A-05 |
| `rc_payload`, `prepared`, `SignedRoundChangePayload`, `prepared_block`, `justification` | ROUND-CHANGE schema parts | A-03 |
| `LightResult` | `VALID`, `INVALID`, `CANNOT_DECIDE` (result of `verify_light`) | A-08 |
| `HeadInfo` | `(block, proposer)` returned by `head()` | A-09 |
| `ConsensusAttributes` | header fields set by consensus in a proposal | A-09 |
| `Eligibility` | `ELIGIBLE`, `INELIGIBLE`, `UNKNOWN` (result of `is_eligible_proposer`) | A-09 |
| `CandidateEntry` | `(addr, bls_public_key)`, an element returned by `candidates(epoch_header, post_state)`; not `Candidate` | A-09 |
| `GovBaseState`, `GovValidatorState`, `GovCouncilState`, `GovProposal`, `Member`, `OrderedAddressSet`, `StrictAddressSet` | governance contract state models; `GovProposal` is a governance proposal, not a `Proposal` | B-05 |
| `BlockSigners`, `Status`, `SealerActivity`, `BlockRange`, `RoundStats` | RPC result schemas | B-09 |

---

## 2. Constants

Canonical names are those of `A-01` §4 (Part A) and `B-01` §10 (Part B). Section 8 lists the aliases that were replaced.

### 2.1 Part A

| Name | Value | Defined in |
|---|---|---|
| `PREPREPARE`, `PREPARE`, `COMMIT`, `ROUND_CHANGE` | `0x12`, `0x13`, `0x14`, `0x15` | A-01 §4.1 |
| `ISTANBUL_MSG` | `0x11` (legacy code) | A-01 §4.1 |
| `NEW_BLOCK_MSG` | `0x07` (eth code inspected on the consensus stream) | A-01 §4.1 |
| `WBFT_DIFFICULTY` | `1` | A-01 §4.2 |
| `EXTRA_VANITY` | `32` | A-01 §4.2 |
| `SEAL_LENGTH` | `96` | A-01 §4.2 |
| `EMPTY_UNCLE_HASH` | `keccak256(0xc0)` | A-01 §4.2 |
| `EMPTY_NONCE` | `0x0000000000000000` | A-01 §4.2 |
| `MAX_GAS_LIMIT` | `2^63 − 1` | A-01 §4.2 (also B-01 §10) |
| `MAXIMUM_EXTRA_DATA_SIZE` | `32` | A-01 §4.2 |
| `DILIGENCE_DENOMINATOR` | `1_000_000` | A-01 §4.3 |
| `DEFAULT_DILIGENCE` | `1_900_000` | A-01 §4.3 |
| `SHUFFLE_ROUND_COUNT` | `33` | A-01 §4.3 |
| `ECDSA_SIGNATURE_LENGTH`, `BLS_SECRET_KEY_LENGTH`, `BLS_PUBLIC_KEY_LENGTH`, `BLS_SIGNATURE_LENGTH` | `65`, `32`, `48`, `96` | A-01 §4.4 |
| `BLS_DST` | `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` | A-01 §4.4 |
| `RANDAO_VERSION` | `1` | A-01 §4.4 |
| `PREPARE_SEAL`, `COMMIT_SEAL` | `0x00`, `0x01` | A-01 §4.4 |
| `n`, `r_bls`, `G2_INFINITY` | secp256k1 group order; BLS12-381 group order; G2 point at infinity | A-02 |
| `INITIAL_GAS_TIP` | `27_600_000_000_000` wei | A-01 §4.5 (also B-01 §10, B-04) |
| `SEQUENCE_THRESHOLD`, `ROUND_THRESHOLD` | `1`, `10` (local, liveness-relevant) | A-01 §4.6 |
| `MAX_BACKLOG_SIZE_PER_VALIDATOR` | `88` (local) | A-01 §4.6 |
| `INMEMORY_PEERS`, `INMEMORY_MESSAGES` | `40`, `1024`: peers in the per-peer recent cache, and keys per key cache (each per-peer entry and the known cache) | A-01 §4.6 |
| `MAX_STATUS_BLOCK_RANGE` | `1024` | A-01 §4.6 (used in B-09) |
| `MAX_ISTANBUL_MSG_SIZE` | `10 485 760` bytes (reference `protocolMaxMsgSize`) | A-07 §4.3 |
| `ZERO_ADDRESS`, `ZERO_HASH` | 20 and 32 zero bytes | A-04, A-05 (notation) |
| `MAX_INT64`, `NS_PER_MS`, `NS_PER_S` | arithmetic helpers of `round_timeout` | A-06 §4.1 |

### 2.2 Part B

| Name | Value | Defined in |
|---|---|---|
| `GAS_LIMIT_BOUND_DIVISOR`, `MIN_GAS_LIMIT`, `GENESIS_GAS_LIMIT`, `ELASTICITY_MULTIPLIER` | `1024`, `5000`, `4_712_388`, `2` | B-01 §10 |
| `INITIAL_BASE_FEE`, `MIN_BASE_FEE`, `MAX_BASE_FEE` | `1_000_000_000`, `20_000_000_000_000`, `20_000_000_000_000_000` | B-01 §10 |
| `INCREASING_THRESHOLD`, `DECREASING_THRESHOLD`, `BASE_FEE_CHANGE_RATE` | `20`, `6`, `2` (%) | B-01 §10 |
| `EMPTY_ROOT_HASH` | root hash of the empty trie (go-ethereum value) | B-02 |
| `GOV_VALIDATOR_SET_SLOT`, `GOV_VALIDATOR_BLS_SLOT`, `GOV_VALIDATOR_GASTIP_SLOT` | `0x33`, `0x37`, `0x39` | B-04 §8 |
| `BLACKLISTED`, `AUTHORIZED` | `1 << 63`, `1 << 62` (account `Extra` bits of B-04 §11) | B-07 §1 |
| `BLS_POP_PRECOMPILE_ADDRESS`, `NATIVE_COIN_MANAGER_ADDRESS`, `ACCOUNT_MANAGER_ADDRESS` | `0x…B00001`, `0x…B00002`, `0x…B00003` | B-04 §12, B-07 |
| `FEE_DELEGATE_DYNAMIC_FEE_TX_TYPE` | `0x16` | B-07 |
| `AUTHORIZED_TX_EXECUTED_TOPIC`, `TRANSFER_TOPIC` | log topics | B-07 |
| `ACTION_*`, `MAX_RETRY_COUNT`, `INITIAL_MEMBER_VERSION`, `MAX_MEMBER_INDEX` and the other governance action identifiers | governance constants | B-05 |
| `DEFAULT_STATUS_WINDOW`, `MAX_UNCLE_DIST`, `MAX_QUEUE_DIST`, `DEFAULT_MIN_SYNC_PEERS`, `TD_SYNC_INTERVAL`, `FORCE_SYNC_CYCLE`, `MAX_TIME_FUTURE_BLOCKS` | `64`, `7`, `32`, `5`, `10 s`, `10 s`, `30 s` | B-09 |

---

## 3. Configuration field names

| Spec name | Unit | Defined in |
|---|---|---|
| `request_timeout` | ms (derived as `requestTimeoutSeconds × 1000`, B-01 §6.1) | A-01 §6.1 |
| `block_period` | s | A-01 §6.1 |
| `proposer_policy` (`ROUND_ROBIN = 0`, `STICKY = 1`; helper `policy_from_id`) | — | A-01 §6.1 |
| `epoch` | blocks | A-01 §6.1 |
| `allowed_future_block_time` | s | A-01 §6.1 |
| `max_request_timeout_seconds` | s; caps rounds `>= 1` only | A-01 §6.1, A-06 WBFT-TIMER-006 |
| `transitions`, `system_contract_upgrades` | — | A-01 §6.1, B-01 §6-§7 |
| `RT(h)`, `MRT(h)`, `BP(h)`, `AFBT` | shorthands for the fields above | A-06 §1 |

---

## 4. Functions

### 4.1 Part A

| Function | Defined in |
|---|---|
| `be_min(x)` (minimal big-endian bytes) | A-01 |
| `config_at(number)` (StableNet derivation with `base_wbft_config`, `sorted_transitions` in B-01 §6) | A-01 §6.5 |
| `keccak256`, `ecdsa_sign(node_key, data)`, `ecdsa_recover_address(data, sig)`, `check_validator_signature(V, data, sig)` | A-02 §2-§3 |
| `derive_bls_key(node_key)`, `bls_sign(sk, msg)`, `bls_verify(pk, msg, sig)`, `bls_aggregate(sigs)`, `aggregate_public_keys(pks)`, `bls_public_key_from_bytes`, `bls_signature_from_bytes` | A-02 §5 |
| `verify_aggregated_seal(V, header, round, agg, seal_type)` (A-08 §6.3 restates it) | A-02 §5.7 |
| `seal_data(header, round, seal_type)`, `randao_data(chain_id, number)`, `randao_mix(parent_mix, reveal)` | A-02 §6-§7 |
| `rlp_encode`, `rlp_decode`, `rlp_encode_string`, `encode_extra`, `decode_extra`, `filtered_header(header, round)`, `hash_with_round(header, round)`, `block_hash(header)` | A-03 |
| `sealer_indices(sealers)`, `make_sealer_set(indices)` | A-03 |
| `message_signing_payload(msg)`, `message_signature`, `signed_fields(msg)`, `dedup_key(payload)` | A-03 |
| `f_value(n)` (binary64), `quorum_size(n)`, `f_plus_one_threshold(n)` | A-04 §2 |
| `validators_at(chain, number, parent_hash, parents=None)`: the set that seals block `number`; `parents` is the batch of not-yet-stored headers used by batch verification (A-08 §6.1) | A-04 §3 |
| `prev_validators_at(chain, header)`, `epoch_info_for(chain, number, parent_hash, parents=None)`, `governing_epoch_info(chain, header)`, `candidate_address(info, i)` | A-04 §3 (B-06 §3.1 restates `epoch_info_for`) |
| `epoch_schedule(number)`, `is_epoch_block(number)`, `last_epoch_block(number)`, `epoch_length_of(e)` | A-04 §4 |
| `index_of(vs, addr)`, `last_proposer(chain, n)`, `calc_proposer(validators, last_proposer, round, policy)`, `is_proposer(vs, proposer, addr)` | A-04 §5 |
| `compute_next_epoch_info(chain, e_header, state)` (diligence is its step 4; selection and shuffle its step 5), `signer_addresses(info, seal)`, `sort_candidates(cands)`, `compute_shuffled_index(index, count, seed)`, `initial_epoch_info(config)`, `post_state(e)` | A-04 §6-§7 |
| `check_message(state, current_view, code, view)`, `is_justified(proposal, target_view, round_changes, prepares, quorum)`, `signature_validator_set(node, view)`, `verify_message_signatures`, `verify_seal(vs, header, round, seal_type, seal, sealer)`, `dedup_by_source(list)` | A-05 |
| `on_start`, `on_stop`, `on_message_received`, `on_backlog_event`, `on_request`, `on_new_head`, `on_round_timeout`, `on_retry_timeout` (event handlers) | A-05 |
| `handle_decoded`, `handle_preprepare`, `handle_prepare`, `handle_commit`, `handle_round_change`, `start_new_round`, `new_round`, `update_round_state`, `update_prior_state`, `set_state`, `send_preprepare`, `broadcast_prepare`, `broadcast_commit`, `broadcast_round_change` (the "ROUND-CHANGE broadcast procedure" of A-06), `decide`, `check_request` | A-05 |
| `round_changes_add`, `has_matching_round_change_and_prepares`, `higher_round_senders`, `count_at_round`, `min_round_above`, `clear_lower_than` | A-05 §11 |
| `add_extra_seal`, `store_extra`, `add_effective_seals_to_extra_seals`, `process_extra_seals`, `clear_extra_seals`, `add_to_backlog`, `backlog_priority`, `process_backlog` | A-05 §12-§13 |
| `schedule(event)`, `broadcast(node, m)`, `relay(node, code, payload)` (network parts in A-07) | A-05 §1.2 |
| `start_round_timer`, `arm_retry_timer`, `arm_future_proposal_timer`, `stop_all_timers` (timer semantics in A-06) | A-05 |
| `round_timeout(config, round)`, `wrap_int64(x)`, `unix_now()` | A-06 |
| `address_of(pubkey)` (peer address; derivation as in A-02) | A-07 §1 |
| `prepare_proposal_header`, `start_extra`, `merge_seals`, `commit_header`, `aggregate_seals` | A-08 §3-§4 |
| `verify_proposal_header` (steps P1-P7), `validators_for_verifying` (V0a, V0b), `verify_header` (steps H1-H21, with H15a/H15b), `verify_seals`, `verify_prev_seals`, `verify_header_batch`, `verify_light` | A-08 §5-§8 |
| `prior_round`, `extra_prepared`, `extra_committed` (inputs of `prepare_proposal_header`) | A-08 §3 |

### 4.2 Application interface (Part A abstract, Part B binding)

| Abstract operation (A-09) | Meaning | StableNet binding |
|---|---|---|
| `head()` | canonical head block and its proposer (`Coinbase`, zero address for genesis) | chain (A-09 §7) |
| `header_by_number(n)`, `header(hash, n)`, `header_by_hash(hash)`, `has_block(hash, n)`, `proposer_of(n)`, `is_bad_block(hash)`, `now()` | chain queries | chain (A-09 §7) |
| `candidates(epoch_header, post_state)` | ordered candidate list with BLS keys, read from the **post-execution state of epoch block `e`** (not a parent state) | `candidates_at_epoch(state_e, e, upgrades)` (B-08 §2.4); storage readers `validator_list`, `bls_public_key` (B-04 §8.2) |
| `is_eligible_proposer(parent, addr)` returning `Eligibility` | blacklist check at the **parent** state | `is_blacklisted` (B-07), B-08 §3 |
| `gas_tip(parent)` | governance gas tip at the **parent** state | `expected_gas_tip(n, chain)` (B-08 §4), `required_gas_tip(header)` (B-06 §5); storage reader `gas_tip(state, gv)` (B-04 §8.2) |
| `ready_to_build(wait, round)` (A-05 writes it as `app.notify_new_round(round)`) | start building a candidate block | miner (A-09 §7) |
| `prepare_consensus_fields(header)` | consensus header fields (= A-08 `prepare_proposal_header`) | A-08 §3 |
| `compute_epoch_info(header, post_state)` | epoch step while building (= A-04 `compute_next_epoch_info`) | `write_epoch` (B-06 §4) |
| `submit_proposal(block)` | hand a built block to consensus | miner |
| `validate_proposal(block)` | proposal validation (= A-08 `verify_proposal_header`); does not execute (WBFT-APP-050) | A-08 §5 |
| `finalize(block, prepared, committed, round)` | **consensus hand-over** of the sealed block after COMMIT quorum; writes the seals (A-08 §4). It is not execution-side finalization | commit paths (A-09 §5) |
| `on_new_head()` | head notification to consensus | miner loop |

Execution-side finalization is `process_finalize(header, state, epoch_handler)` (B-06 §2), with the epoch handlers `write_epoch` (proposer path) and `verify_epoch` (import path). The governance step that closes a proposal is `finalize_proposal` (B-05). Unqualified `finalize` always means the A-09 operation.

### 4.3 Part B

| Function | Defined in |
|---|---|
| `forked(s, n)`, `forked_time(s, t)` | B-01 §3 |
| `base_wbft_config(cfg)`, `sorted_transitions(ts)` | B-01 §6 |
| `collect_upgrades(cfg)`, `upgrade_list`, `system_contracts_at(n)`, `upgrades_at(n)`, `genesis_system_contracts` | B-01 §7 (B-04 §4 restates `upgrade_list(chain_config)` and `system_contracts_at(n, upgrades)` with explicit arguments) |
| `build_anzeon_genesis(g)`, `to_header(g)`, `create_initial_extra(a)`, `init_gov_council(...)`, `address_set_storage(...)`, `inject_contracts(g)`, `state_root(alloc)` | B-02 |
| `system_contracts_transition(scs, alloc)` (B-02 §4.2 restates it) | B-04 §3 |
| `verify_gas_limit(parent_gas_limit, gas_limit)`, `calc_base_fee(parent)`, `base_fee_delta(parent_base_fee)` | B-03 §2 |
| `mapping_slot`, `array_length_slot`, `array_element_slot`, `read_bytes`, `pad32`, `u256`, `low20`, `set_add`, `set_remove` | B-04 §5-§6 |
| `validator_list(state, gv)`, `bls_public_key(state, gv, addr)`, `gas_tip(state, gv)` (storage readers; A-04 writes `bls_public_key(state, addr)` for the key that `candidates` returns) | B-04 §8.2 |
| `consensus_projection(S)`, `valid_quorum(q, n)`, `is_expired(g, p, now)`, `index_at(addr, v)`, `create_proposal`, `vote`, `execute_proposal`, `finalize_proposal`, `require_proposal_member`, `require_active_member`, `require_valid_proposal_id` | B-05 |
| `process_finalize(header, state, epoch_handler)`, `merged_upgrade_transition(n)`, `distribute_base_fee(header, state)`, `write_epoch`, `verify_epoch`, `required_gas_tip(header)`, `verify_gas_tip(header)`, `canonical_header(e)`, `ancestor_of(hash, e)` | B-06 |
| `account_extra`, `is_blacklisted`, `is_authorized`, `header_gas_tip`, `to_message`, `payer`, `recover_fee_payer`, `fee_payer_sighash`, `recover_from_digest` (recovery from an already-hashed digest; not A-02 `ecdsa_recover_address`), `derive_effective_gas_price`, `fee_effects` | B-07 |
| `candidates_at_epoch(state_e, e, upgrades)`, `epoch_info_from_candidates(...)`, `expected_gas_tip(n, chain)`, `decide_validators` (= step 5 of A-04 `compute_next_epoch_info`); notation `S(k)` (state committed by block `k`), `gv0` (genesis `GovValidator` address) | B-08 |
| `verify_header_state_parts`, `reorg_needed`, `miner_update`, `status_range` | B-09 |

---

## 5. Message and state names

- Message type names in prose: PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE (codes in §2.1).
- Node consensus states: `AcceptRequest`, `Preprepared`, `Prepared`, `Committed` (A-05).
- Justification parts: `justification_round_changes`, `justification_prepares` (A-03, A-05).
- `prepared_certificate`: the PREPARE quorum kept for later justification (reference `WBFTPreparedPrepares`, A-05).
- Previous-block seals in the extra: `prev_round`, `prev_prepared_seal`, `prev_committed_seal`; covered by the block hash and identical on every node. Current-block seals `round`, `prepared_seal`, `committed_seal` are node-local (A-08 WBFT-HDR-053).
- Late seals: *extra seals* (seals received after quorum, merged into the next block's previous-block seals; messages carrying them are relayed, A-07 WBFT-NET-042).
- Branches of `start_new_round`: `INITIAL`, `CATCH_UP`, `ROUND_CHANGE` (A-05 §6.2).
- Network outcome classes: DROP-SILENT, IGNORE, ACCEPT, DISCONNECT (A-07 §8).
- Header verification steps: P1-P7 (proposal), V0a/V0b (validator sets), H1-H21 (header) in A-08; B-03 §7 adds B1, E1, F1, S1 for the import path.
- Log entry identifiers `L001`-`L184` (A-13, regenerated per commit).

---

## 6. Glossary terms

The glossary of `A-01` §2 is normative for both parts. Terms added by other chapters:

| Term | Meaning | Defined in |
|---|---|---|
| validator, candidate, node key, proposer, sequence, round, view, proposal, digest, seal, prepare seal / commit seal, aggregated seal, sealer index, quorum, extra seals, previous-block seals, epoch, diligence, randao reveal, randao mix, gas tip, justification, legacy message, transition, reference implementation | see A-01 | A-01 §2 |
| epoch block | block `e` whose `EpochInfo` is non-empty; the **last** block of the epoch it closes, sealed by the previous set; its `EpochInfo` applies from `e + 1`; block 0 is an epoch block | A-01 §2, A-04 §1 |
| governing epoch block of `n` | `last_epoch_block(n − 1)` | A-04 §3 |
| post-execution state of block `n` | state after the transactions of `n` and the finalization steps that precede the epoch step (upgrades at `n`, base-fee distribution) | A-09 §3, B-06 §2 |
| parent state of height `n` | state whose root is the `Root` of block `n − 1` | A-09 §3 |
| state-dependent steps | header steps H15b (blacklist) and H21 (gas tip); skipped when the parent state is unavailable | A-08 §6.6, B-09 SNET-SYNC-010 |
| wall clock, monotonic clock, entering a view | timer terms | A-06 §1 |
| round-change timer, retry timer, future-PRE-PREPARE timer | the three consensus timers | A-06 |
| peer address, consensus link, message key, per-peer recent cache, known cache, Broadcast, Gossip | network terms | A-07 §1, §5.2, §6 |
| operator, attempt, soft failure, hard failure, retry mode, terminal mode, live / terminal status, mirror fault | governance terms | B-05 |
| proposer path, import path (non-proposer path) | the two commit paths | A-09 §5.1, B-06 §6.1 |
| lock, certificate | lock: the pair `(current.prepared_round, current.prepared_block)` set by a PREPARE quorum; certificate: `prepared_certificate`, the `Q` PREPAREs that set it | A-14 §1 (state fields in A-05 §3.1) |

---

## 7. Observable categories

`Observable:` tags use `header`, `network`, `log`, `rpc`, `state` (README §2.3). The error catalog of A-13 additionally uses `local` and `unused`.

---

## 8. Retired names

| Retired | Use instead | Reason |
|---|---|---|
| `candidates(parent)`, `candidates(state)`, `candidates(post_state(e))`, `candidates(e)` | `candidates(epoch_header, post_state)`; binding `candidates_at_epoch(state_e, e, upgrades)` | candidates come from the post-execution state of epoch block `e`, not from a parent state |
| `bls_fast_aggregate_verify` | `verify_aggregated_seal` (A-02) | the reference aggregates the keys and verifies once, with the quorum and index checks |
| `compute_diligence(...)` | step 4 of `compute_next_epoch_info` (A-04) | not a separate function in the reference |
| `validators_at(number)` without chain arguments; a separate batch variant | `validators_at(chain, number, parent_hash, parents=None)` | one definition in A-04 |
| `commit_proposal` (A-05 draft) | `finalize` (A-09) | same operation |
| `finalize_block_state` (proposed) | `process_finalize` (B-06) | B-06 already used this name |
| `finalize` (B-05 governance draft) | `finalize_proposal` | avoids a clash with A-09 `finalize` |
| `Proposal` (B-05 governance draft) | `GovProposal` | avoids a clash with A-01 `Proposal` |
| `Candidate` with a BLS key (A-09 draft) | `CandidateEntry` | avoids a clash with A-01 `Candidate` |
| `msg_key(data)` (A-07 draft) | `dedup_key(data)` (A-03) | same key |
| `epoch_info_for(header)` (B-06 draft) | `epoch_info_for(chain, number, parent_hash)` (A-04) | same lookup |
| `RECENT_PEERS`, `RECENT_KEYS_PER_PEER`, `KNOWN_KEYS` | `INMEMORY_PEERS`, `INMEMORY_MESSAGES` | A-01 names |
| `VANITY_LENGTH` | `EXTRA_VANITY` | A-01 name |
| `MAX_BACKLOG_PER_SOURCE` | `MAX_BACKLOG_SIZE_PER_VALIDATOR` | A-01 name |
| `NIL_UNCLE_HASH` | `EMPTY_UNCLE_HASH` | A-01 name |
| `request_timeout_ms`, `block_period_seconds`, `allowed_future_block_time_seconds` (as `Config` fields) | `request_timeout`, `block_period`, `allowed_future_block_time` | A-01 names; units unchanged |
| `GasLimitBoundDivisor`, `MinBaseFee` and the other Go names of B-01 §10 | the SCREAMING_CASE spec names of B-01 §10 | the Go names remain as reference names |
