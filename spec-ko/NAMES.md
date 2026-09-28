# 이름 표

이 파일은 명세의 정식 이름 표다. 모든 장은 아래 이름을 표에 적힌 뜻으로 쓴다. "정의한 곳" 칸에 적힌 장이 그 이름의 규범 정의를 담고, 다른 장은 그 정의를 참조한다. 어떤 장이 다른 장의 함수를 다시 적은 경우에는 정의한 장이 우선한다. 이 표는 2026-09-24 편집 작업에서 모든 장의 "New names" 목록을 합쳐 만들었다. 8절은 그 편집 작업에서 폐기한 이름을 나열한다.

---

## 1. 타입

| 이름 | 뜻 | 정의한 곳 |
|---|---|---|
| `Address` | 20 바이트 계정 주소 | A-01 |
| `Hash` | 32 바이트 Keccak-256 digest | A-01 |
| `bytesN`, `bigint` | 고정 길이 바이트 문자열, 그리고 상한이 없는 음이 아닌 정수 (RLP big integer) | A-01 |
| `Sequence` | 합의 대상인 block 높이 (메시지 필드이며 wire 에서는 big integer 이다) | A-01 |
| `Round` | 한 sequence 안의 round 번호 (wire 에서는 big integer, header extra 에서는 `uint32` 이다) | A-01 |
| `View` | `(sequence, round)` 쌍이며, sequence 로 먼저 정렬하고 같으면 round 로 정렬한다 | A-01 |
| `Proposal` | 한 view 에 제안된 block (header 와 body 를 모두 갖춘 block) | A-01 |
| `Digest` | `block_hash(proposal)`: 현재 block 의 seal 을 빼고 round 를 0 으로 둔 header 의 hash | A-01, A-03 |
| `ECDSASignature` | 65 바이트 `R ‖ S ‖ V` | A-01 |
| `BLSPublicKey` | 48 바이트 (압축한 G1 점) | A-01 |
| `BLSSignature` / `Seal` | 96 바이트 (압축한 G2 점) | A-01 |
| `SealType` | `PREPARE_SEAL = 0x00`, `COMMIT_SEAL = 0x01` | A-01 |
| `SealerSet` | validator 인덱스의 bitmap | A-01, A-03 |
| `AggregatedSeal` | `(sealers: SealerSet, signature: BLSSignature)` | A-01, A-03 |
| `SealData` | `(sealer, seal)`: `finalize` 에 넘기는 seal 목록의 원소 하나 | A-05 |
| `WBFTExtra` | `Header.Extra` 에 들어가는 합의 데이터이며, 필드는 `vanity_data`, `randao_reveal`, `prev_round`, `prev_prepared_seal`, `prev_committed_seal`, `round`, `prepared_seal`, `committed_seal`, `gas_tip`, `epoch_info` 이다 | A-01, A-03 |
| `EpochInfo` | 필드는 `candidates`, `validators` (`candidates` 의 인덱스), `bls_public_keys` 이다 | A-01, A-03 |
| `Candidate` | `(addr: Address, diligence: uint64)` 이며 `EpochInfo.candidates` 의 원소다 | A-01, A-03 |
| `ValidatorSet` | 한 높이의 `(address, bls_public_key)` 를 순서대로 담은 목록이며, 그 높이의 proposer policy 를 함께 갖는다 | A-01, A-04 |
| `Config` | 한 높이의 합의 설정 (transition 을 적용한 뒤의 값) | A-01 |
| `Transition` | 높이에 따라 합의 설정을 덮어쓰는 항목 | A-01 §6.4 |
| `Upgrade` | system contract upgrade 항목 `(block, system_contracts)` | B-01 §7 |
| `ConsensusState` | 노드 상태 `AcceptRequest`, `Preprepared`, `Prepared`, `Committed` 의 열거형 | A-05 |
| `RoundState`, `RoundChangeSet`, `PriorState`, `Node` | 노드 state 레코드 | A-05 §3 |
| `CheckResult` | `check_message` 의 결과 분류: `PROCESS`, `FUTURE`, `OLD`, `INVALID`, `EXTRA_SEAL`, `TOO_FAR` | A-05 |
| `rc_payload`, `prepared`, `SignedRoundChangePayload`, `prepared_block`, `justification` | ROUND-CHANGE 스키마의 구성 요소 | A-03 |
| `LightResult` | `VALID`, `INVALID`, `CANNOT_DECIDE` (`verify_light` 의 결과) | A-08 |
| `HeadInfo` | `head()` 가 돌려주는 `(block, proposer)` | A-09 |
| `ConsensusAttributes` | proposal 에서 합의 쪽이 채우는 header 필드 | A-09 |
| `Eligibility` | `ELIGIBLE`, `INELIGIBLE`, `UNKNOWN` (`is_eligible_proposer` 의 결과) | A-09 |
| `CandidateEntry` | `candidates(epoch_header, post_state)` 가 돌려주는 원소 `(addr, bls_public_key)` 이며, `Candidate` 와 다른 타입이다 | A-09 |
| `GovBaseState`, `GovValidatorState`, `GovCouncilState`, `GovProposal`, `Member`, `OrderedAddressSet`, `StrictAddressSet` | 거버넌스 contract 의 state 모델이다. `GovProposal` 은 거버넌스 제안이며 `Proposal` 과 다르다 | B-05 |
| `BlockSigners`, `Status`, `SealerActivity`, `BlockRange`, `RoundStats` | RPC 결과 스키마 | B-09 |

---

## 2. 상수

정식 이름은 `A-01` §4 (Part A) 와 `B-01` §10 (Part B) 의 이름이다. 8절은 이 이름으로 대체된 별칭을 나열한다.

### 2.1 Part A

| 이름 | 값 | 정의한 곳 |
|---|---|---|
| `PREPREPARE`, `PREPARE`, `COMMIT`, `ROUND_CHANGE` | `0x12`, `0x13`, `0x14`, `0x15` | A-01 §4.1 |
| `ISTANBUL_MSG` | `0x11` (legacy 코드) | A-01 §4.1 |
| `NEW_BLOCK_MSG` | `0x07` (합의 stream 에서 들여다보는 eth 코드) | A-01 §4.1 |
| `WBFT_DIFFICULTY` | `1` | A-01 §4.2 |
| `EXTRA_VANITY` | `32` | A-01 §4.2 |
| `SEAL_LENGTH` | `96` | A-01 §4.2 |
| `EMPTY_UNCLE_HASH` | `keccak256(0xc0)` | A-01 §4.2 |
| `EMPTY_NONCE` | `0x0000000000000000` | A-01 §4.2 |
| `MAX_GAS_LIMIT` | `2^63 − 1` | A-01 §4.2 (B-01 §10 에도 있다) |
| `MAXIMUM_EXTRA_DATA_SIZE` | `32` | A-01 §4.2 |
| `DILIGENCE_DENOMINATOR` | `1_000_000` | A-01 §4.3 |
| `DEFAULT_DILIGENCE` | `1_900_000` | A-01 §4.3 |
| `SHUFFLE_ROUND_COUNT` | `33` | A-01 §4.3 |
| `ECDSA_SIGNATURE_LENGTH`, `BLS_SECRET_KEY_LENGTH`, `BLS_PUBLIC_KEY_LENGTH`, `BLS_SIGNATURE_LENGTH` | `65`, `32`, `48`, `96` | A-01 §4.4 |
| `BLS_DST` | `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` | A-01 §4.4 |
| `RANDAO_VERSION` | `1` | A-01 §4.4 |
| `PREPARE_SEAL`, `COMMIT_SEAL` | `0x00`, `0x01` | A-01 §4.4 |
| `n`, `r_bls`, `G2_INFINITY` | secp256k1 군의 위수, BLS12-381 군의 위수, G2 의 무한원점 | A-02 |
| `INITIAL_GAS_TIP` | `27_600_000_000_000` wei | A-01 §4.5 (B-01 §10, B-04 에도 있다) |
| `SEQUENCE_THRESHOLD`, `ROUND_THRESHOLD` | `1`, `10` (로컬 상수이며 liveness 에 영향을 준다) | A-01 §4.6 |
| `MAX_BACKLOG_SIZE_PER_VALIDATOR` | `88` (로컬 상수) | A-01 §4.6 |
| `INMEMORY_PEERS`, `INMEMORY_MESSAGES` | `40`, `1024`: peer 별 최근 cache 가 추적하는 peer 수, 그리고 key cache 하나가 담는 key 수 (peer 별 항목 하나와 known cache 각각에 적용된다) | A-01 §4.6 |
| `MAX_STATUS_BLOCK_RANGE` | `1024` | A-01 §4.6 (B-09 에서 쓴다) |
| `MAX_ISTANBUL_MSG_SIZE` | `10 485 760` 바이트 (레퍼런스의 `protocolMaxMsgSize`) | A-07 §4.3 |
| `ZERO_ADDRESS`, `ZERO_HASH` | 0 바이트 20 개와 0 바이트 32 개 | A-04, A-05 (표기) |
| `MAX_INT64`, `NS_PER_MS`, `NS_PER_S` | `round_timeout` 의 산술 보조 상수 | A-06 §4.1 |

### 2.2 Part B

| 이름 | 값 | 정의한 곳 |
|---|---|---|
| `GAS_LIMIT_BOUND_DIVISOR`, `MIN_GAS_LIMIT`, `GENESIS_GAS_LIMIT`, `ELASTICITY_MULTIPLIER` | `1024`, `5000`, `4_712_388`, `2` | B-01 §10 |
| `INITIAL_BASE_FEE`, `MIN_BASE_FEE`, `MAX_BASE_FEE` | `1_000_000_000`, `20_000_000_000_000`, `20_000_000_000_000_000` | B-01 §10 |
| `INCREASING_THRESHOLD`, `DECREASING_THRESHOLD`, `BASE_FEE_CHANGE_RATE` | `20`, `6`, `2` (%) | B-01 §10 |
| `EMPTY_ROOT_HASH` | 빈 trie 의 root hash (go-ethereum 의 값) | B-02 |
| `GOV_VALIDATOR_SET_SLOT`, `GOV_VALIDATOR_BLS_SLOT`, `GOV_VALIDATOR_GASTIP_SLOT` | `0x33`, `0x37`, `0x39` | B-04 §8 |
| `BLACKLISTED`, `AUTHORIZED` | `1 << 63`, `1 << 62` (B-04 §11 의 계정 `Extra` 비트) | B-07 §1 |
| `BLS_POP_PRECOMPILE_ADDRESS`, `NATIVE_COIN_MANAGER_ADDRESS`, `ACCOUNT_MANAGER_ADDRESS` | `0x…B00001`, `0x…B00002`, `0x…B00003` | B-04 §12, B-07 |
| `FEE_DELEGATE_DYNAMIC_FEE_TX_TYPE` | `0x16` | B-07 |
| `AUTHORIZED_TX_EXECUTED_TOPIC`, `TRANSFER_TOPIC` | 로그 topic | B-07 |
| `ACTION_*`, `MAX_RETRY_COUNT`, `INITIAL_MEMBER_VERSION`, `MAX_MEMBER_INDEX` 와 그 밖의 거버넌스 action 식별자 | 거버넌스 상수 | B-05 |
| `DEFAULT_STATUS_WINDOW`, `MAX_UNCLE_DIST`, `MAX_QUEUE_DIST`, `DEFAULT_MIN_SYNC_PEERS`, `TD_SYNC_INTERVAL`, `FORCE_SYNC_CYCLE`, `MAX_TIME_FUTURE_BLOCKS` | `64`, `7`, `32`, `5`, `10 s`, `10 s`, `30 s` | B-09 |

---

## 3. 설정 필드 이름

| 명세 이름 | 단위 | 정의한 곳 |
|---|---|---|
| `request_timeout` | ms 이며, `requestTimeoutSeconds × 1000` 으로 계산한다 (B-01 §6.1) | A-01 §6.1 |
| `block_period` | s | A-01 §6.1 |
| `proposer_policy` (`ROUND_ROBIN = 0`, `STICKY = 1`; 보조 함수 `policy_from_id`) | — | A-01 §6.1 |
| `epoch` | block 수 | A-01 §6.1 |
| `allowed_future_block_time` | s | A-01 §6.1 |
| `max_request_timeout_seconds` | s 이며, round `>= 1` 에만 상한을 건다 | A-01 §6.1, A-06 WBFT-TIMER-006 |
| `transitions`, `system_contract_upgrades` | — | A-01 §6.1, B-01 §6-§7 |
| `RT(h)`, `MRT(h)`, `BP(h)`, `AFBT` | 위 필드들의 약칭 | A-06 §1 |

---

## 4. 함수

### 4.1 Part A

| 함수 | 정의한 곳 |
|---|---|
| `be_min(x)` (최소 big-endian 바이트) | A-01 |
| `config_at(number)` (StableNet 에서는 B-01 §6 의 `base_wbft_config`, `sorted_transitions` 로 유도한다) | A-01 §6.5 |
| `keccak256`, `ecdsa_sign(node_key, data)`, `ecdsa_recover_address(data, sig)`, `check_validator_signature(V, data, sig)` | A-02 §2-§3 |
| `derive_bls_key(node_key)`, `bls_sign(sk, msg)`, `bls_verify(pk, msg, sig)`, `bls_aggregate(sigs)`, `aggregate_public_keys(pks)`, `bls_public_key_from_bytes`, `bls_signature_from_bytes` | A-02 §5 |
| `verify_aggregated_seal(V, header, round, agg, seal_type)` (A-08 §6.3 이 다시 적는다) | A-02 §5.7 |
| `seal_data(header, round, seal_type)`, `randao_data(chain_id, number)`, `randao_mix(parent_mix, reveal)` | A-02 §6-§7 |
| `rlp_encode`, `rlp_decode`, `rlp_encode_string`, `encode_extra`, `decode_extra`, `filtered_header(header, round)`, `hash_with_round(header, round)`, `block_hash(header)` | A-03 |
| `sealer_indices(sealers)`, `make_sealer_set(indices)` | A-03 |
| `message_signing_payload(msg)`, `message_signature`, `signed_fields(msg)`, `dedup_key(payload)` | A-03 |
| `f_value(n)` (binary64), `quorum_size(n)`, `f_plus_one_threshold(n)` | A-04 §2 |
| `validators_at(chain, number, parent_hash, parents=None)`: block `number` 를 seal 하는 validator 집합을 돌려준다. `parents` 는 batch 검증(A-08 §6.1)이 쓰는, 아직 저장하지 않은 header 묶음이다 | A-04 §3 |
| `prev_validators_at(chain, header)`, `epoch_info_for(chain, number, parent_hash, parents=None)`, `governing_epoch_info(chain, header)`, `candidate_address(info, i)` | A-04 §3 (B-06 §3.1 이 `epoch_info_for` 를 다시 적는다) |
| `epoch_schedule(number)`, `is_epoch_block(number)`, `last_epoch_block(number)`, `epoch_length_of(e)` | A-04 §4 |
| `index_of(vs, addr)`, `last_proposer(chain, n)`, `calc_proposer(validators, last_proposer, round, policy)`, `is_proposer(vs, proposer, addr)` | A-04 §5 |
| `compute_next_epoch_info(chain, e_header, state)` (diligence 는 4단계에서 계산하고, 선택과 shuffle 은 5단계에서 한다), `signer_addresses(info, seal)`, `sort_candidates(cands)`, `compute_shuffled_index(index, count, seed)`, `initial_epoch_info(config)`, `post_state(e)` | A-04 §6-§7 |
| `check_message(state, current_view, code, view)`, `is_justified(proposal, target_view, round_changes, prepares, quorum)`, `signature_validator_set(node, view)`, `verify_message_signatures`, `verify_seal(vs, header, round, seal_type, seal, sealer)`, `dedup_by_source(list)` | A-05 |
| `on_start`, `on_stop`, `on_message_received`, `on_backlog_event`, `on_request`, `on_new_head`, `on_round_timeout`, `on_retry_timeout` (event handler) | A-05 |
| `handle_decoded`, `handle_preprepare`, `handle_prepare`, `handle_commit`, `handle_round_change`, `start_new_round`, `new_round`, `update_round_state`, `update_prior_state`, `set_state`, `send_preprepare`, `broadcast_prepare`, `broadcast_commit`, `broadcast_round_change` (A-06 의 "ROUND-CHANGE broadcast 절차"), `decide`, `check_request` | A-05 |
| `round_changes_add`, `has_matching_round_change_and_prepares`, `higher_round_senders`, `count_at_round`, `min_round_above`, `clear_lower_than` | A-05 §11 |
| `add_extra_seal`, `store_extra`, `add_effective_seals_to_extra_seals`, `process_extra_seals`, `clear_extra_seals`, `add_to_backlog`, `backlog_priority`, `process_backlog` | A-05 §12-§13 |
| `schedule(event)`, `broadcast(node, m)`, `relay(node, code, payload)` (네트워크 쪽 내용은 A-07 에 있다) | A-05 §1.2 |
| `start_round_timer`, `arm_retry_timer`, `arm_future_proposal_timer`, `stop_all_timers` (timer 의미론은 A-06 에 있다) | A-05 |
| `round_timeout(config, round)`, `wrap_int64(x)`, `unix_now()` | A-06 |
| `address_of(pubkey)` (peer 주소이며, A-02 와 같은 방식으로 유도한다) | A-07 §1 |
| `prepare_proposal_header`, `start_extra`, `merge_seals`, `commit_header`, `aggregate_seals` | A-08 §3-§4 |
| `verify_proposal_header` (단계 P1-P7), `validators_for_verifying` (V0a, V0b), `verify_header` (단계 H1-H21, H15a/H15b 포함), `verify_seals`, `verify_prev_seals`, `verify_header_batch`, `verify_light` | A-08 §5-§8 |
| `prior_round`, `extra_prepared`, `extra_committed` (`prepare_proposal_header` 의 입력) | A-08 §3 |

### 4.2 애플리케이션 인터페이스 (Part A 는 추상, Part B 는 binding)

| 추상 연산 (A-09) | 뜻 | StableNet binding |
|---|---|---|
| `head()` | canonical head block 과 그 block 의 proposer 를 돌려준다 (proposer 는 `Coinbase` 이며, genesis 에서는 0 주소다) | chain (A-09 §7) |
| `header_by_number(n)`, `header(hash, n)`, `header_by_hash(hash)`, `has_block(hash, n)`, `proposer_of(n)`, `is_bad_block(hash)`, `now()` | chain 조회 연산 | chain (A-09 §7) |
| `candidates(epoch_header, post_state)` | BLS 키를 포함한 candidate 목록을 순서대로 돌려준다. 이 목록은 parent state 가 아니라 **epoch block `e` 의 실행 후 state** 에서 읽는다 | `candidates_at_epoch(state_e, e, upgrades)` (B-08 §2.4); storage reader `validator_list`, `bls_public_key` (B-04 §8.2) |
| `Eligibility` 를 돌려주는 `is_eligible_proposer(parent, addr)` | **parent** state 에서 blacklist 여부를 검사한다 | `is_blacklisted` (B-07), B-08 §3 |
| `gas_tip(parent)` | **parent** state 의 거버넌스 gas tip 을 돌려준다 | `expected_gas_tip(n, chain)` (B-08 §4), `required_gas_tip(header)` (B-06 §5); storage reader `gas_tip(state, gv)` (B-04 §8.2) |
| `ready_to_build(wait, round)` (A-05 는 `app.notify_new_round(round)` 로 적는다) | candidate block 조립을 시작한다 | miner (A-09 §7) |
| `prepare_consensus_fields(header)` | header 의 합의 필드를 채운다 (A-08 의 `prepare_proposal_header` 와 같다) | A-08 §3 |
| `compute_epoch_info(header, post_state)` | block 을 조립하면서 epoch 단계를 수행한다 (A-04 의 `compute_next_epoch_info` 와 같다) | `write_epoch` (B-06 §4) |
| `submit_proposal(block)` | 조립한 block 을 합의 쪽에 넘긴다 | miner |
| `validate_proposal(block)` | proposal 을 검증한다 (A-08 의 `verify_proposal_header` 와 같다). block 을 실행하지 않는다 (WBFT-APP-050) | A-08 §5 |
| `finalize(block, prepared, committed, round)` | COMMIT 이 quorum 에 이른 뒤 seal 된 block 을 **합의 쪽이 넘겨주는 연산**이며, seal 을 기록한다 (A-08 §4). 실행 쪽의 finalization 과 다르다 | commit 경로 (A-09 §5) |
| `on_new_head()` | 합의 쪽에 새 head 를 알린다 | miner loop |

실행 쪽의 finalization 은 `process_finalize(header, state, epoch_handler)` (B-06 §2) 이다. 이 함수는 epoch handler 로 `write_epoch` (proposer path) 와 `verify_epoch` (import path) 를 쓴다. 거버넌스 제안을 닫는 단계는 `finalize_proposal` (B-05) 이다. 아무 수식어 없이 `finalize` 라고 쓰면 언제나 A-09 의 연산을 뜻한다.

### 4.3 Part B

| 함수 | 정의한 곳 |
|---|---|
| `forked(s, n)`, `forked_time(s, t)` | B-01 §3 |
| `base_wbft_config(cfg)`, `sorted_transitions(ts)` | B-01 §6 |
| `collect_upgrades(cfg)`, `upgrade_list`, `system_contracts_at(n)`, `upgrades_at(n)`, `genesis_system_contracts` | B-01 §7 (B-04 §4 가 `upgrade_list(chain_config)` 와 `system_contracts_at(n, upgrades)` 를 인자를 명시해 다시 적는다) |
| `build_anzeon_genesis(g)`, `to_header(g)`, `create_initial_extra(a)`, `init_gov_council(...)`, `address_set_storage(...)`, `inject_contracts(g)`, `state_root(alloc)` | B-02 |
| `system_contracts_transition(scs, alloc)` (B-02 §4.2 가 다시 적는다) | B-04 §3 |
| `verify_gas_limit(parent_gas_limit, gas_limit)`, `calc_base_fee(parent)`, `base_fee_delta(parent_base_fee)` | B-03 §2 |
| `mapping_slot`, `array_length_slot`, `array_element_slot`, `read_bytes`, `pad32`, `u256`, `low20`, `set_add`, `set_remove` | B-04 §5-§6 |
| `validator_list(state, gv)`, `bls_public_key(state, gv, addr)`, `gas_tip(state, gv)` (storage reader 이다. A-04 는 `candidates` 가 돌려주는 키를 가리킬 때 `bls_public_key(state, addr)` 로 적는다) | B-04 §8.2 |
| `consensus_projection(S)`, `valid_quorum(q, n)`, `is_expired(g, p, now)`, `index_at(addr, v)`, `create_proposal`, `vote`, `execute_proposal`, `finalize_proposal`, `require_proposal_member`, `require_active_member`, `require_valid_proposal_id` | B-05 |
| `process_finalize(header, state, epoch_handler)`, `merged_upgrade_transition(n)`, `distribute_base_fee(header, state)`, `write_epoch`, `verify_epoch`, `required_gas_tip(header)`, `verify_gas_tip(header)`, `canonical_header(e)`, `ancestor_of(hash, e)` | B-06 |
| `account_extra`, `is_blacklisted`, `is_authorized`, `header_gas_tip`, `to_message`, `payer`, `recover_fee_payer`, `fee_payer_sighash`, `recover_from_digest` (이미 hash 한 digest 에서 recover 한다. A-02 의 `ecdsa_recover_address` 와 다르다), `derive_effective_gas_price`, `fee_effects` | B-07 |
| `candidates_at_epoch(state_e, e, upgrades)`, `epoch_info_from_candidates(...)`, `expected_gas_tip(n, chain)`, `decide_validators` (A-04 `compute_next_epoch_info` 의 5단계와 같다); 표기 `S(k)` (block `k` 가 commit 한 state), `gv0` (genesis 의 `GovValidator` 주소) | B-08 |
| `verify_header_state_parts`, `reorg_needed`, `miner_update`, `status_range` | B-09 |

---

## 5. 메시지와 상태의 이름

- 본문에서 메시지 타입 이름은 PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE 로 쓴다. 각 타입의 코드 값은 §2.1 에 있다.
- 노드의 합의 상태는 `AcceptRequest`, `Preprepared`, `Prepared`, `Committed` 이다 (A-05).
- justification 의 구성 요소는 `justification_round_changes`, `justification_prepares` 이다 (A-03, A-05).
- `prepared_certificate` 는 나중에 justification 으로 쓰기 위해 보관하는 PREPARE quorum 이다. 레퍼런스에서는 `WBFTPreparedPrepares` 라고 부른다 (A-05).
- extra 에 들어가는 이전 block 의 seal 은 `prev_round`, `prev_prepared_seal`, `prev_committed_seal` 이다. 이 필드들은 block hash 에 포함되므로 모든 노드에서 같다. 현재 block 의 seal 인 `round`, `prepared_seal`, `committed_seal` 은 노드마다 다를 수 있다 (A-08 WBFT-HDR-053).
- 늦게 도착한 seal 은 *extra seal* 이라고 부른다. extra seal 은 quorum 이 모인 뒤에 받은 seal 이며, 다음 block 의 이전 block seal 에 합쳐진다. extra seal 을 담은 메시지는 relay 된다 (A-07 WBFT-NET-042).
- `start_new_round` 의 분기는 `INITIAL`, `CATCH_UP`, `ROUND_CHANGE` 이다 (A-05 §6.2).
- 네트워크 처리 결과의 분류는 DROP-SILENT, IGNORE, ACCEPT, DISCONNECT 이다 (A-07 §8).
- header 검증 단계는 A-08 의 P1-P7 (proposal), V0a/V0b (validator 집합), H1-H21 (header) 이다. B-03 §7 은 import path 에 B1, E1, F1, S1 을 더한다.
- 로그 항목 식별자는 `L001`-`L184` 이다. 이 식별자는 A-13 에 있으며, commit 마다 다시 생성한다.

---

## 6. 용어집 항목

`A-01` §2 의 용어집은 두 Part 모두에 규범으로 적용된다. 다른 장이 추가한 용어는 다음과 같다.

| 용어 | 뜻 | 정의한 곳 |
|---|---|---|
| validator, candidate, node key, proposer, sequence, round, view, proposal, digest, seal, prepare seal / commit seal, aggregated seal, sealer index, quorum, extra seals, previous-block seals, epoch, diligence, randao reveal, randao mix, gas tip, justification, legacy message, transition, reference implementation | A-01 을 본다 | A-01 §2 |
| epoch block | `EpochInfo` 가 비어 있지 않은 block `e` 이다. block `e` 는 자신이 닫는 epoch 의 **마지막** block 이며, 이전 validator 집합이 seal 한다. block `e` 의 `EpochInfo` 는 `e + 1` 부터 적용된다. block 0 도 epoch block 이다 | A-01 §2, A-04 §1 |
| governing epoch block of `n` | `last_epoch_block(n − 1)` 이다 | A-04 §3 |
| post-execution state of block `n` | block `n` 의 트랜잭션과, epoch 단계보다 앞선 finalization 단계 (`n` 의 upgrade, base fee 분배) 를 적용한 뒤의 state 이다 | A-09 §3, B-06 §2 |
| parent state of height `n` | block `n − 1` 의 `Root` 를 root 로 갖는 state 이다 | A-09 §3 |
| state-dependent steps | header 검증 단계 H15b (blacklist) 와 H21 (gas tip) 이다. parent state 를 쓸 수 없으면 이 단계를 건너뛴다 | A-08 §6.6, B-09 SNET-SYNC-010 |
| wall clock, monotonic clock, entering a view | timer 를 설명할 때 쓰는 용어 | A-06 §1 |
| round-change timer, retry timer, future-PRE-PREPARE timer | 합의가 쓰는 세 가지 timer | A-06 |
| peer address, consensus link, message key, per-peer recent cache, known cache, Broadcast, Gossip | 네트워크를 설명할 때 쓰는 용어 | A-07 §1, §5.2, §6 |
| operator, attempt, soft failure, hard failure, retry mode, terminal mode, live / terminal status, mirror fault | 거버넌스를 설명할 때 쓰는 용어 | B-05 |
| proposer path, import path (non-proposer path) | 두 가지 commit 경로 | A-09 §5.1, B-06 §6.1 |
| lock, certificate | lock 은 PREPARE quorum 으로 정해지는 쌍 `(current.prepared_round, current.prepared_block)` 이고, certificate 는 그 lock 을 정한 PREPARE `Q` 개인 `prepared_certificate` 이다 | A-14 §1 (상태 필드는 A-05 §3.1) |

---

## 7. Observable 분류

`Observable:` 태그의 값은 `header`, `network`, `log`, `rpc`, `state` 이다 (README §2.3). A-13 의 오류 목록은 이 값에 `local` 과 `unused` 를 더해 쓴다.

---

## 8. 폐기한 이름

| 폐기한 이름 | 대신 쓰는 이름 | 이유 |
|---|---|---|
| `candidates(parent)`, `candidates(state)`, `candidates(post_state(e))`, `candidates(e)` | `candidates(epoch_header, post_state)`; binding 은 `candidates_at_epoch(state_e, e, upgrades)` | candidate 는 parent state 가 아니라 epoch block `e` 의 실행 후 state 에서 온다 |
| `bls_fast_aggregate_verify` | `verify_aggregated_seal` (A-02) | 레퍼런스는 공개 키를 aggregate 한 뒤 한 번만 verify 하고, quorum 검사와 인덱스 검사를 함께 한다 |
| `compute_diligence(...)` | `compute_next_epoch_info` 의 4단계 (A-04) | 레퍼런스에는 diligence 를 계산하는 별도 함수가 없다 |
| chain 인자가 없는 `validators_at(number)`, 그리고 따로 둔 batch 변형 | `validators_at(chain, number, parent_hash, parents=None)` | A-04 에 정의를 하나만 두기 위해서다 |
| `commit_proposal` (A-05 초안) | `finalize` (A-09) | 두 이름은 같은 연산을 가리킨다 |
| `finalize_block_state` (제안된 이름) | `process_finalize` (B-06) | B-06 이 이미 `process_finalize` 라는 이름을 썼다 |
| `finalize` (B-05 거버넌스 초안) | `finalize_proposal` | A-09 의 `finalize` 와 이름이 겹치지 않게 한다 |
| `Proposal` (B-05 거버넌스 초안) | `GovProposal` | A-01 의 `Proposal` 과 이름이 겹치지 않게 한다 |
| BLS 키를 가진 `Candidate` (A-09 초안) | `CandidateEntry` | A-01 의 `Candidate` 와 이름이 겹치지 않게 한다 |
| `msg_key(data)` (A-07 초안) | `dedup_key(data)` (A-03) | 두 이름은 같은 key 를 가리킨다 |
| `epoch_info_for(header)` (B-06 초안) | `epoch_info_for(chain, number, parent_hash)` (A-04) | 두 이름은 같은 조회 연산을 가리킨다 |
| `RECENT_PEERS`, `RECENT_KEYS_PER_PEER`, `KNOWN_KEYS` | `INMEMORY_PEERS`, `INMEMORY_MESSAGES` | A-01 의 이름을 쓴다 |
| `VANITY_LENGTH` | `EXTRA_VANITY` | A-01 의 이름을 쓴다 |
| `MAX_BACKLOG_PER_SOURCE` | `MAX_BACKLOG_SIZE_PER_VALIDATOR` | A-01 의 이름을 쓴다 |
| `NIL_UNCLE_HASH` | `EMPTY_UNCLE_HASH` | A-01 의 이름을 쓴다 |
| `request_timeout_ms`, `block_period_seconds`, `allowed_future_block_time_seconds` (`Config` 필드로 쓴 경우) | `request_timeout`, `block_period`, `allowed_future_block_time` | A-01 의 이름을 쓴다. 단위는 바뀌지 않는다 |
| `GasLimitBoundDivisor`, `MinBaseFee` 와 B-01 §10 의 그 밖의 Go 이름 | B-01 §10 의 SCREAMING_CASE 명세 이름 | Go 이름은 레퍼런스 코드의 이름으로만 남는다 |
