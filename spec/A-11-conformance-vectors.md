# A-11. Conformance and test vectors

This chapter defines who has to conform to which requirements (conformance classes), how conformance is tested with language-neutral test vectors, and how the vectors are produced and versioned. It is the entry point for the WBFT inspector and for a second implementation (the Rust port).

---

## 1. Conformance classes

A class is a set of requirements an implementation claims to satisfy. An implementation MAY claim more than one class.

| Class | Code | Requirements | Notes |
|---|---|---|---|
| Consensus participant | `CP` | every requirement of Part A and Part B | A validator. Sends and receives consensus messages, builds and seals blocks |
| Verifying node | `VN` | Part A except the send conditions of `A-05`, `A-06`, `A-07` (requirements whose subject is "a node MUST send / broadcast / relay"); all of Part B | A full node that imports and fully validates blocks but does not seal. It still decodes consensus messages if it listens to `istanbul/100` |
| Light verifier | `LV` | `A-01`, `A-02`, `A-03`, `A-04` §§ on validator sets and epochs (not next-epoch computation), `A-08` light verification (`verify_light`) | Checks finality of a header chain without state. Cannot check requirements that need execution state (epoch info recomputation, gas tip, blacklist); `A-08` lists them |
| Observer | `OB` | decoding (`A-02`, `A-03`) plus the checks of every requirement tagged `Observable:` | The inspector. An observer does not claim that it behaves as a node; it claims that it detects violations of observable requirements by others |

[WBFT-VEC-001] An implementation that claims a class MUST satisfy every normative requirement in that class, except requirements marked `(withdrawn)`.
Source: this chapter (definition)

---

## 2. Observable requirements

Requirements carry `Observable:` tags that say from which data source an outside party can check them:

| Tag | Data source | Typical checks |
|---|---|---|
| `header` | block headers (via RPC or p2p) | extra decoding, seals, prev seals, randao, epoch info presence, time rules |
| `state` | account/contract state, receipts and logs at a block | gas tip value, base-fee distribution, candidate list, governance results |
| `network` | `istanbul/100` frames captured on a link | message encoding, signatures, send conditions, relay |
| `log` | node logs | timing of internal events that have no wire trace (timer expiry, round start) |
| `rpc` | node RPC responses | `istanbul_*` results, `finalized`/`safe` tags |

An `Observable:` line holds only a comma-separated list of the tags above; what exactly is observed (a log message, an RPC method) goes on a following `Observed as:` line.

[WBFT-VEC-003] A requirement tagged `Observable: <tag>` MUST be decidable from data of that tag (together with data of lower-numbered chapters that are deterministic, such as configuration). If a requirement needs state that is not available to an outside observer at the reference commit (for example contract storage at an old block on a non-archive node), the requirement text MUST say so, and the vector catalog MUST provide a vector for it instead.
Source: this chapter (editorial rule)

---

## 3. Test vectors

### 3.1 Layout

Vectors follow the layout of the Ethereum consensus-spec tests as run by Lighthouse's `ef_tests`:

```
vectors/
  <runner>/
    <handler>/
      <case>/
        meta.yaml        # requirement IDs covered, reference commit, generator, description
        input.yaml       # inputs
        expected.yaml    # expected output; absent => the operation MUST fail
```

A case is a directory with the three files above; `expected.yaml` is absent when the operation under test must fail. No other file is part of a case. Headers and blocks are carried inside `input.yaml` as their RLP encoding (WBFT-VEC-014), not as separate files.

[WBFT-VEC-010] A test runner MUST treat a case without an `expected` file as a case in which the operation under test MUST fail (reject, error, or panic-equivalent), and MUST report a case that succeeds as a failure.
Source: this chapter (convention adopted from Lighthouse `compare_result`)

[WBFT-VEC-011] Every case MUST list in `meta.yaml` the requirement IDs it covers, the reference commit it was generated from, the toolchain used (`go1.23.12` for the reference), and the generator program and version.
Source: this chapter (convention)

[WBFT-VEC-012] A runner SHOULD record which vector files it opened and fail if any file under `vectors/` was not opened, so that added vectors cannot be silently skipped.
Source: this chapter (convention adopted from Lighthouse `check_all_files_accessed.py`)

[WBFT-VEC-013] Every vector file MUST use the following YAML subset, which converts to JSON one to one: block mappings and block sequences indented by two spaces, keys made of `a-z`, `0-9` and `_`, double-quoted strings with JSON escaping, `true`, `false`, `null`, and the empty collections `[]` and `{}`. No other YAML construct is allowed (no comments, plain or single-quoted scalars, numbers, anchors, aliases, tags, flow collections that are not empty, multi-line scalars, several documents in one file, or duplicate keys). A file outside the subset is not a valid vector, and a runner MUST reject it instead of interpreting it. `tools/vectorgen/check_yaml_subset.py` performs this check.
Source: this chapter (vector format)

[WBFT-VEC-014] In vector files every integer MUST be written as a decimal string, including small values such as `code`, `round` and `seal_type`. Every byte string, including addresses, MUST be written as lowercase hexadecimal with the prefix `0x` (addresses are not checksummed; the empty string is `"0x"`). An absent optional value (a seal, `epoch_info`, a prepared round or block) MUST be `null`. Headers and blocks MUST be given as their RLP encoding.
Source: this chapter (vector format)

[WBFT-VEC-015] `meta.yaml` MUST contain the fields `runner`, `handler`, `case`, `kind`, `description`, `requirements` (a list of requirement IDs), `reference` (with `implementation`, `commit` as the full hash, `toolchain` and `build`, which is `cgo` for the reference) and `generator` (with `name` and `version`); these fields satisfy WBFT-VEC-011. A fail case MAY also contain `expected_error`, the error text of the reference. That field is informative: error texts are not normative, and a runner MUST NOT compare it.
Source: this chapter (vector format)

[WBFT-VEC-016] Runner, handler and case directory names MUST consist only of `a-z`, `0-9` and `_`.
Source: this chapter (vector format)

Chain fixture. An input field named `chain` holds a chain fixture: the headers a handler reads and the configuration they were made with, given as data. It is a mapping with three fields and one optional field:

1. `config`: the chain configuration. `preset` names a network preset of `B-01` §11 (`"8282"`), and the other fields replace parts of that preset: `init` replaces `anzeon.init` (fields `validators` and `bls_public_keys`), `wbft` replaces `anzeon.wbft`, `transitions` replaces `transitions`, and `shanghai_time` and `cancun_time` replace the two fork times (`null` keeps the preset value, which is absent). The optional field `boho_block`, present only when it is set, replaces `BohoBlock` of the preset (the block of the Boho upgrade, `B-01` SNET-CFG-011). `wbft` and every element of `transitions` use the genesis field names in snake case: `request_timeout_seconds`, `block_period_seconds`, `epoch_length`, `allowed_future_block_time`, `proposer_policy` and `max_request_timeout_seconds`, and a transition also has `block`. The two pointer fields `proposer_policy` and `max_request_timeout_seconds` are `null` when absent; the other fields are always given, because for them 0 and absent mean the same. The pure handlers that take a configuration (`chain/config_at`, `validators/epoch_boundary`) take an input `config` with only the fields `wbft` and `transitions`.
2. `genesis`: the RLP of the header of block 0. It is given directly and need not be the header that `B-02` builds from the configuration.
3. `headers`: the RLP of the other stored headers in ascending order of number. Every header of this list is the canonical header at its number. The list need not be contiguous: a number without a header means that the node stores no canonical header at that number, which is how a case expresses a missing ancestor.
4. `non_canonical` (optional, present only when not empty): the RLP of stored headers that are not canonical at their number. A handler finds them by hash (a parent lookup or the walk back of `A-04` §3.1), never by number. This is how a case expresses a fork, for example the lookup of WBFT-HDR-071 (`A-08`).

A chain fixture carries no state. A handler that reads the parent state treats it as unavailable, so header verification skips the steps H15b and H21 (`A-08` §6.6). A handler that needs a value from state takes it as a separate input (`gas_tip` of `header/build_proposal_header`, `candidates` of `validators/next_epoch_info`). The headers of a fixture pass header verification (a non-canonical header on its own branch), except in `validators/next_epoch_info`, whose fixtures carry placeholder seals because that computation does not check them (the case description says so). A case whose input contains a chain fixture has kind `chain`.

Execution state. A chain fixture carries no state, and neither does any other input; a handler that reads account state takes the accounts it reads as the input `accounts`: a list of `address`, `balance`, `nonce`, `extra` (the flag word of `B-04` SNET-SYS-071), `code` and `storage` (a list of `key`, `value`, 32 bytes each). The state holds exactly these accounts; an account of the list exists even if it is empty, and a slot that is not listed is zero. The runner `execution` (except `p256_verify`) and the runner `source` use this input. `config` of these handlers is a preset with `overrides` of `applepie_block` and `boho_block` (`null` keeps the preset value), the shape of `chain/fork_schedule` without the two fork times.

Steps. A case of kind `steps` runs the consensus core of one node through a list of events and records what the node does after each of them. The format below and WBFT-VEC-058 to WBFT-VEC-061 apply only to the steps handlers of the runners `state_machine` and `network` (`state_machine/rounds`, `network/receive_outcome`). The handler `governance/scenarios` is also of kind `steps`, but it runs a sequence of governance operations instead of the consensus core and uses its own steps format, given below under "Governance scenarios".

`input.yaml` has two fields:

1. `initial`: the node and its application.
   - `validators`: the validator set as a list of `address` and `bls_public_key`, in set order. The set is the same at every height of the case; a case does not cross an epoch change.
   - `proposer_policy`: the policy identifier, as in `validators/proposer`.
   - `node_key`: the node's secp256k1 key (32 bytes). The node's address, its BLS key (`A-02` §5.1) and every signature it makes derive from it. ECDSA signatures are deterministic (RFC 6979, `A-02`) and BLS signatures are deterministic, so the node's own messages are compared byte for byte.
   - `head`: the RLP of the head block that `app.head()` returns (`A-05` §1.3); its `Coinbase` is the last proposer.
   - `app`: the other answers of the application: `invalid_proposals` (block hashes for which `app.validate_proposal` fails), `future_proposals` (block hashes for which it answers `FUTURE(d)` until a `future_timeout` step has been processed for a timer armed for that proposal, and `VALID` from then on; the duration `d` is not part of the case), every other proposal being `VALID`; `bad_blocks` (block hashes for which `app.is_bad_block` holds) and `finalize` (`"ok"` or `"fail"`, the result of every `app.finalize`).
   - Network runner only: `engine` (`"running"` or `"stopped"`), `synchronising` (bool) and `peers` (the addresses of the connected peers).

   The node starts as after `Start` (WBFT-SM-004, WBFT-SM-008): view `(head.number + 1, 0)` in `AcceptRequest`, no lock, empty backlog. Any other state (a lock, a later round, stored extra seals) is reached through steps, as the reference reaches it.
2. `steps`: a list of events. Each step has `kind` and `label` (informative, a runner does not use it) and, by kind:
   - `message` (`code`, `payload`): the event `Message(code, payload)` of `A-05` §4.1, a message from a peer or the self-delivery of a message the node broadcast. In the network runner it is only the self-delivery, which bypasses the deduplication caches (`A-07` WBFT-NET-030).
   - `backlog` (`code`, `payload`): the event `Backlog(m)` for the replay scheduled by an earlier step whose `rlp_encode(m)` is `payload`.
   - `request` (`block`): the event `Request(p)` with the block RLP.
   - `round_timeout` (`timer`, optional): the expiry of a round timer. Without `timer` it is the expiry of the round timer armed last, which the case has not cancelled. With `timer` it is the expiry of the round timer with that index (below); if that timer has been cancelled or superseded, or the engine is stopped, the step has no effect (`A-06` WBFT-TIMER-014).
   - `retry_timeout` (`round`, `timer` optional): without `timer`, the event `RetryTimeout(round)`, also when it stands for the expiry of a timer that was queued before the timer was cancelled (`A-05` WBFT-SM-081). With `timer`, the expiry of the retry timer with that index, which remembers `round`: if the timer has been cancelled before the step, or the engine is stopped, it never fires and the step has no effect; otherwise the step is the event `RetryTimeout(round)`.
   - `future_timeout` (`timer`): the expiry of the future-PRE-PREPARE timer with that index (`A-06` §7). From this step on `app.validate_proposal` answers `VALID` for the proposal of that timer. If the timer has not been cancelled and the engine runs, it schedules `Backlog(m)` for its PRE-PREPARE (recorded under `scheduled`); otherwise the step has no effect.
   - `stop`: the engine stops (`A-05` Stop). Scheduled events that no step has processed are lost. Until the next `start` step the case gives only `head` steps with `notify` false and timer steps with `timer`.
   - `start`: the engine starts again at the current application head, as at the beginning of the case: a new core in view `(head.number + 1, 0)` with no lock and an empty backlog (WBFT-SM-004).
   - `head` (`block`, `notify`): the application head becomes `block`; if `notify` is true, the event `NewHead` is then processed.
   - `frame` (`peer`, `code`, `payload`), network runner only: an `istanbul` message with that code and payload received from the peer address.

The timers of each kind (`round`, `retry`, `future`) are numbered from 0 in the order in which the case arms them, as the `timers` records list them, starting with the record of `start` and continuing across `stop` and `start` steps. A timer step with `timer` names a timer by that number.

A scheduled event (`A-05` WBFT-SM-002: self-deliveries, backlog replays, request replays) is never processed implicitly. Every scheduled event that a case processes is a step of its own, at the position the case chooses; a scheduled event that no step processes is lost, like a message lost on the network. This removes the order in which the reference processes concurrent events (a Go `select` over an `event.TypeMux`) from the vectors.

`expected.yaml` has `start`, the record of `Start` (`null` when the engine is stopped), and `steps`, one record per step. A record has these fields:

1. `check`: the `check_message` class (`A-05` §5.2) of a `message`, `backlog` or `frame` step, or `null` if the message was discarded before (unknown code, undecodable, invalid signature) or the step is of another kind.
2. `relay`: whether the processing returned `OK` and the message was relayed (WBFT-SM-011, WBFT-SM-012); `null` for steps that process no message.
3. `sent`: the messages the node broadcast, in send order. Each is written with the fields of `encoding/message_codec`, with every justification list sorted by the byte value of its members' encodings, and `encoded` holds the bytes, or `null` when a list has two or more members, because their order is unspecified (WBFT-SM-035). When a later step self-delivers such a message, its `payload` is the encoding with the sorted lists; the signatures stay valid because the lists are not signed (`A-03`).
4. `timers`: the timers armed in the step, in order: `{kind: "round", sequence, round}` for the round timer of a view, `{kind: "retry", round}` for the retry timer and `{kind: "future", sequence, round}` for the future-PRE-PREPARE timer, with the view of the deferred PRE-PREPARE. Durations are not recorded; they are the subject of `timers/round_timeout` and `timers/build_wait`. Cancellations are not recorded either; timer steps with `timer` make them observable.
5. `new_round`: the rounds passed to `app.notify_new_round` in the step.
6. `finalized`: `null`, or the call of `app.finalize`: `proposal` (block hash), `round`, `prepared_seals` and `committed_seals` (lists of `sealer`, `seal`, sorted by sealer) and `result`.
7. `scheduled`: the events scheduled in the step: `{kind: "backlog", code, payload}` sorted by the source address (in release order for one source), then `{kind: "request", block}`.
8. `state`: the state variables of `A-05` §3.1 after the step: `view`, `state`, `proposer`, `locked_round`, `locked_block`, `preprepare` (`round`, `proposal`), `pending_request`, `preprepare_sent`, `prepares` and `commits` (source addresses), `certificate` (`null` or `source`, `sequence`, `round`, `digest` of each member), `round_changes` (per round key, including keys with no message: `round`, `sources`, `prepared_round`, `prepared_block`), `backlog` (`source`, `code`, `sequence`, `round` of each queued message), `prior` (`round`, `proposal`), `extra_prepare_seals` and `extra_commit_seals` (`source`, `sequence`, `round`). Blocks are given by block hash, and every set is sorted.

While the engine is stopped (the record of a `stop` step and of every step before the next `start` step), `state` is `null` and the other fields are `null` or empty.

A network record has three more fields in front: `outcome`, the class of `A-07` §8 of a `frame` step (`ACCEPT`, `IGNORE`, `DROP_SILENT`, `DISCONNECT`; `null` for other steps), `dedup_key` (`A-07` WBFT-NET-022, for a frame with a code from `0x11` to `0x15` and a non-empty payload that is not longer than the size limit (`A-07` WBFT-NET-013) while the engine runs; the payload of `0x11` is unwrapped first), and `relay_to`, the sorted addresses of the peers the relay was sent to (`null` when nothing was relayed). Each element of `sent` has one more field, `to`, the sorted addresses of the peers it was sent to. When the engine is stopped a record has only the three network fields.

Governance scenarios. A case of `governance/scenarios` runs a sequence of calls against the governance contracts of a genesis and records what each call does (`B-05` §13 "Scenario test vectors"). `input.yaml` has four fields:

1. `genesis`: `preset` (`"8282"`); `gov_validator` and `gov_council`, the parameters of the two `GovBase` instances (`B-05` §12: `members`, `quorum`, `expiry`, `max_proposals`, and for `gov_validator` also `validators`, `bls_public_keys` and `gas_tip`; the member version is 1); and `alloc`, the funded accounts (`address`, `balance`). The genesis is the genesis of the preset with these parameters, with `anzeon.init` set to the validators and keys of `gov_validator` (`B-02`) and the other system contracts of the preset.
2. `genesis_time`: the timestamp of the genesis block.
3. `named_accounts`: the accounts whose flags the projection reports.
4. `steps`: the calls, each with `label` (informative), `sender`, `to`, `data` (the call data), `gas`, `time` (the block timestamp) and `number` (the block number). Each step is the only transaction of its block and is applied to the state that the previous step left (the genesis state for the first): a message from `sender` with value 0, gas limit `gas`, the nonce of `sender` and the gas price of `B-07` §3 in a block with base fee 20 000 gwei, the GovValidator gas tip before the step and the first validator as coinbase. No signature is involved. Sub-calls (the proof-of-possession precompile, `AccountManager`) run as part of the call; their results are not inputs.

`expected.yaml` has `steps`, one record per step:

1. `status`: `success`, `revert` (the call reverted) or `failure` (another exceptional halt).
2. `revert_data`: the revert data of a reverted call (a custom error of `B-05`), otherwise `"0x"`.
3. `logs`: the logs of the call in order, each `address`, `topics`, `data`.
4. `write_set`: the storage slots whose value the call changed, with the new value (`address`, `key`, `value`), sorted by address and key. It gives the change of the `GovBase` state of the touched instance and of the derived state of `B-05` §8 and §10 in the storage layout of `B-04` and `B-05` §2.
5. `projection`: the consensus projection after the step: `validators` (in list order), `bls_public_keys` (the key of each validator, in the same order), `gas_tip`, and `blacklisted` and `authorized` (sorted, restricted to `named_accounts`).

Balances and gas are not recorded; they are the subject of the runner `execution`.

### 3.2 Origin of vectors

[WBFT-VEC-020] Expected values MUST be produced by running the reference implementation (go-stablenet at the recorded commit, built with the recorded toolchain). Values computed by a second implementation MUST NOT be used as expected values, except for cases that the reference cannot express (the case then states so in `meta.yaml`), because a vector produced by the implementation under test does not test it.
Source: this chapter; lesson from tendermint-rs

[WBFT-VEC-021] Keys used in vectors MUST be derived deterministically from a documented seed, so that any case can be regenerated. The vectors of this specification use the seed of `A-02` §8.1, `key_i = keccak256(ASCII("wbft-spec-vector-key-" ‖ decimal(i)))`, so that they reproduce the worked examples of `A-02` and `A-03`.
Source: this chapter (convention adopted from tendermint-rs `testgen`)

[WBFT-VEC-022] Expected values of ECDSA signer recovery MUST follow the cgo build of the reference (`A-02` §3.3). A case whose signature the no-cgo build would accept but the cgo build rejects (for example a recovery byte `V = 4`) is a fail case, and its description says so.
Source: `A-02` §3.3

The drafting tools in `tools/` (`vectors-a01-a03`, `epoch-a04`, `genesis-b02`, `slots-b04`, `logcat`) computed the worked examples of this specification and are the starting point of the generator. The generator is `tools/vectorgen` (see its `README.md`); it writes the vectors into `vectors/` next to this chapter.

### 3.3 Catalog

The table lists the runners and handlers. "Kind" says whether a case is a pure function of its input (`pure`), a sequence of events applied to a state (`steps`, in the style of the Ethereum fork-choice tests: an initial state, then a list of steps with the record of each; §3.1 "Steps" gives the format of the `state_machine` and `network` handlers, and `governance/scenarios` has its own format), or a chain fixture (`chain`: a genesis and a list of blocks).

| Runner | Handler | Kind | Chapter / requirements | Notes |
|---|---|---|---|---|
| `crypto` | `keccak256`, `ecdsa_sign`, `ecdsa_recover` | pure | A-02 | includes high-S twins and `V` edge values; cgo vs no-cgo acceptance differs (A-02) |
| `crypto` | `bls_derive`, `bls_sign`, `bls_verify`, `bls_aggregate`, `aggregate_public_keys` | pure | A-02 | infinity and non-subgroup inputs as fail cases; every non-canonical compressed encoding of WBFT-CRYPTO-056 (flag variants, a coordinate component not below `p`) as a fail case and sign-flag flips as decodable cases; public keys whose sum is the point at infinity, whose aggregate signature `c0 ‖ 95 × 00` MUST fail `bls_verify` (WBFT-CRYPTO-055, `A-08` WBFT-HDR-100). `A-02` §8.3 lists these cases |
| `crypto` | `seal_data`, `randao_data`, `randao_mix` | pure | A-02 | |
| `encoding` | `extra_codec` | pure | A-03 `ENC` | round trip and every reject rule (`0x80` vs `0xc0`, short/long fields, trailing bytes) |
| `encoding` | `block_hash`, `hash_with_round`, `filtered_header` | pure | A-03 | includes the undecodable-extra constant case |
| `encoding` | `message_codec`, `signing_payload`, `dedup_key` | pure | A-03 `MSG`, A-07 | four message types; ROUND-CHANGE "absent" variants; re-encoding identity |
| `validators` | `quorum` | pure | A-04 | `N = 0..22`, float64 behaviour. The sizes at which binary64 rounding changes the result (`A-10` §2) are too large to build a set and have no case |
| `validators` | `proposer` | pure | A-04 `PROP` | both policies, zero last proposer, index-not-found |
| `validators` | `epoch_boundary` | pure | A-04 `EPOCH` | transitions, one-block epochs (the boundary computation itself does not fail; a base `epochLength` ≤ 1 is rejected by configuration, so one-block epochs come only from transitions). |
| `validators` | `validators_at` | chain | A-04 §3 | height 0 from the configuration, later heights from the governing epoch header, the policy of `config_at`; missing epoch header and epoch header without `EpochInfo` as fail cases |
| `validators` | `shuffle` | pure | A-04 | 33 rounds, keccak256 |
| `validators` | `sort_candidates` | pure | A-04 | order by diligence, descending |
| `validators` | `next_epoch_info` | chain | A-04 | inputs: a chain fixture that ends at the parent of the epoch block (epoch header history and previous epoch infos), the epoch header without `EpochInfo` (its mix digest is the seed), and the candidate list with BLS keys (`A-09` `candidates`) |
| `timers` | `round_timeout` | pure | A-06 | cap not applied at round 0; overflow and clamp cases |
| `timers` | `build_wait` | pure | A-06 WBFT-TIMER-040, WBFT-TIMER-041 | the wait before the block builder starts: in round 0 from the head time, the block period and the clock, including a clock behind the head and a negative wait; no wait for rounds from 1 |
| `state_machine` | `check_message` | pure | A-05 | every state × code × view relation |
| `state_machine` | `is_justified` | pure | A-05 | dedup, stale view, prepared-round rules |
| `state_machine` | `rounds` | steps | A-05, A-06 | normal height, round change with and without prepared block, F+1 skip, late timeout catch-up, extra seals, backlog replay order; future proposals (deferral, replacement, cancellation by the round timer); expiry of cancelled and live timers; engine stop and restart at the next height |
| `network` | `receive_outcome` | steps | A-07 | DROP-SILENT / IGNORE / ACCEPT / DISCONNECT per condition, including the size limit; dedup pre-emption |
| `header` | `build_proposal_header` | chain | A-08 | prev seals, merge of extra seals, vanity rules |
| `header` | `verify_header` | chain | A-08, B-03 | one case per rejection step H1–H21, with the expected verdict (WBFT-VEC-031); an aggregated seal whose sealers' keys sum to the point at infinity (`ErrInvalidSeal`, WBFT-HDR-100). A fixture has no state, so H15b and H21 are skipped. H1 (no `Number`) cannot be written in RLP, and a header that reaches H9 must carry a `WithdrawalsHash` in RLP and is rejected at H7 first; these two steps have no case of their own |
| `header` | `verify_headers` | chain | A-08 §6.7 | batch verification: input order, the preceding headers of the batch as parents (including the epoch header), `ErrUnknownAncestor` for every header after the first failure or deferral |
| `header` | `verify_light` | chain | A-08 | `VALID`, `INVALID`, `CANNOT_DECIDE` |
| `chain` | `config_at`, `fork_schedule` | pure | A-01, B-01 | presets 8282 and 8283; transitions with the same block |
| `chain` | `genesis` | pure | B-02 | preset genesis hashes, extra bytes, state roots |
| `execution` | `gas_tip_enforcement`, `fee_delegation` | pure | B-07 | worked examples E-1 … E-9 of B-07 with the state before the block's transactions as input; gas-tip override for non-authorized and authorized senders, legacy transactions, fee-cap bounds, fee payer balance and signature rules, Applepie gating, refunds, `AuthorizedTxExecuted` and `Transfer` logs |
| `execution` | `p256_verify` | pure | B-07 SNET-TX-089 | the 782 cases of the reference file `core/vm/testdata/precompiles/p256Verify.json` (EIP-7951; 567 with output `0x…01`, 215 with empty output, gas 6 900 in every case), which `TestPrecompiledP256Verify` passes at the reference commit; plus inputs that are not 160 bytes long (empty output, 6 900 gas) and a call to `0x…0100` before `BohoBlock` (no precompile, no gas) |
| `execution` | `process_finalize` | chain | B-06 | upgrade application at `BohoBlock`, base-fee distribution with and without remainder (also for diligence 0 and a blacklisted validator), gas-tip check against the parent state, `EpochInfo` on a non-epoch block, epoch write (proposer path) and verify (import path) |
| `governance` | `scenarios` | steps | B-05 §13 | scenarios V-01 … V-10, V-12 … V-24 and V-26 (operation sequences with logs, write sets and the consensus projection); own steps format (§3.1 "Governance scenarios"). |
| `source` | `candidates_at_epoch` | pure | B-08 | the readers of `B-04` §8.2 over GovValidator storage written by the genesis initializer: V-SRC-001, V-SRC-007, V-SRC-008, the candidate part of V-SRC-012, more than 12 candidates, a key that is not checked. The action vectors V-SRC-002 … 006, 009 and 011 need governance transactions and are not generated yet |

Vectors generated so far (2026-09-28, reference `740526d03`; stage 1 by `tools/vectorgen` 0.1.1, stage 2 by `tools/vectorgen/stage2` 0.3.0, stage 3 by `tools/vectorgen/stage3` 0.3.1). The handlers not listed here have no vectors yet. A `header/verify_header`, `header/verify_headers` or `header/verify_light` case that expects a rejection is not a fail case: its `expected.yaml` holds the verdict.

| Runner | Handler | Cases | of which fail cases |
|---|---|---|---|
| `crypto` | `keccak256` | 9 | 0 |
| `crypto` | `ecdsa_sign` | 11 | 0 |
| `crypto` | `ecdsa_recover` | 19 | 14 |
| `crypto` | `bls_derive` | 10 | 0 |
| `crypto` | `bls_sign` | 5 | 0 |
| `crypto` | `bls_verify` | 13 | 4 |
| `crypto` | `bls_aggregate` | 21 | 12 |
| `crypto` | `aggregate_public_keys` | 20 | 14 |
| `crypto` | `seal_data` | 10 | 0 |
| `crypto` | `randao_data` | 9 | 0 |
| `crypto` | `randao_mix` | 5 | 0 |
| `encoding` | `extra_codec` | 41 | 28 |
| `encoding` | `block_hash` | 9 | 0 |
| `encoding` | `hash_with_round` | 27 | 0 |
| `encoding` | `filtered_header` | 18 | 6 |
| `encoding` | `message_codec` | 34 | 19 |
| `encoding` | `signing_payload` | 12 | 2 |
| `encoding` | `dedup_key` | 9 | 0 |
| `validators` | `quorum` | 32 | 0 |
| `validators` | `proposer` | 52 | 0 |
| `validators` | `epoch_boundary` | 58 | 0 |
| `validators` | `validators_at` | 11 | 2 |
| `validators` | `shuffle` | 14 | 3 |
| `validators` | `sort_candidates` | 30 | 0 |
| `validators` | `next_epoch_info` | 14 | 3 |
| `timers` | `round_timeout` | 77 | 0 |
| `timers` | `build_wait` | 7 | 0 |
| `chain` | `config_at` | 34 | 0 |
| `chain` | `fork_schedule` | 16 | 0 |
| `chain` | `genesis` | 10 | 1 |
| `state_machine` | `check_message` | 336 | 0 |
| `state_machine` | `is_justified` | 27 | 0 |
| `state_machine` | `rounds` | 26 | 0 |
| `network` | `receive_outcome` | 8 | 0 |
| `header` | `build_proposal_header` | 16 | 4 |
| `header` | `verify_header` | 47 | 0 |
| `header` | `verify_headers` | 9 | 0 |
| `header` | `verify_light` | 18 | 0 |
| `execution` | `p256_verify` | 787 | 0 |
| `execution` | `gas_tip_enforcement` | 11 | 3 |
| `execution` | `fee_delegation` | 10 | 6 |
| `execution` | `process_finalize` | 17 | 6 |
| `source` | `candidates_at_epoch` | 8 | 0 |
| `governance` | `scenarios` | 24 | 0 |

The `EpochInfo` encoding cases are part of `encoding/extra_codec`. The single-key and single-signature decoding cases of WBFT-CRYPTO-056 are part of `crypto/aggregate_public_keys` and `crypto/bls_aggregate` (a list of one element).

Handler shapes. The input and output fields of each handler are listed in `tools/vectorgen/README.md` ("Handler inputs and outputs"); that list is the handler schema of this specification. Twenty shapes need a reason:

1. `bls_verify` takes a list `public_keys` that is aggregated first (WBFT-CRYPTO-028), so that the case of WBFT-CRYPTO-055 (keys whose sum is the point at infinity) can be expressed; a single point at infinity is already rejected at decoding.
2. The decoding rules of WBFT-CRYPTO-056 are tested through `aggregate_public_keys` and `bls_aggregate` with one element, not through a separate decoding handler.
3. `message_codec` returns the decoded fields and the re-encoding; `signing_payload` returns the signing payload and signer of the message and of every embedded signed payload (WBFT-MSG-003).
4. `filtered_header` fails when the extra does not decode, because the reference returns no header.
5. `quorum` returns `f_float64_bits`, the binary64 value of F as 8 bytes in big-endian order, so that the rounding of WBFT-VAL-001 is compared exactly without a decimal rendering of a float.
6. `round_timeout` returns `timeout` in nanoseconds as a signed decimal (it is negative when the base wraps, which makes the timer fire at once) and `warning`, which names the Warn record of `A-06` WBFT-TIMER-007 (`cap_overflow_guard`) or WBFT-TIMER-008 (`max_int64_clamp`), or is `none`.
7. `verify_header` returns only `verdict` (WBFT-VEC-031); its input has `now`, the verifier's clock in Unix seconds, because H2 depends on it. The reference error of a rejected header is quoted in the case description.
8. `verify_light` returns `result`. The reference has no light verifier, so its expected result is composed from reference calls (`validators_at` for `V` and `Vp`, header verification with `check_seals` and the parent resolved by hash, `is_epoch_block` for WBFT-HDR-130), and `CANNOT_DECIDE` follows the definition of `A-08` §8. This is the exception of WBFT-VEC-020, and each case says so.
9. `check_message` takes the three state variables that the classification reads (`view`, `state`, `prior_round`) and the message's `code` and `view` directly, not a state reached through steps, so that every combination can be written.
10. `is_justified` takes the ROUND-CHANGEs and PREPAREs as decoded fields with their `source`, the signer that WBFT-SM-017 recovered before the check, and the proposal as its block hash, the only property the check reads. `quorum` is an input.
11. `rounds` and `receive_outcome` use the step format of §3.1 "Steps". `receive_outcome` runs the reference `HandleMsg`, deduplication caches and `Gossip` together with the core; which errors of `HandleMsg` close the connection is decided by the `eth` handler (`eth/handler_istanbul.go:118-127`), whose rule the generator applies, and so is the size limit that the same handler checks before `HandleMsg` (`:138-140`) (the exception of WBFT-VEC-020; each case says so).
12. `fork_schedule` takes a preset and overrides of its fork fields, a block number and a time. `fork_id` needs the genesis block of that configuration, which the reference builds (`B-02`).
13. `genesis` takes a preset genesis specification with overrides of its header fields, of `bohoBlock` and additional funded accounts, and returns the header RLP, `hash`, `state_root` and `extra`.
14. `verify_headers` takes a list `headers` and returns `verdicts`, one per header in input order, each compared like the verdict of `verify_header` (WBFT-VEC-031). Every header after the first one that is not accepted is `reject`, also when that first one is deferred, because the reference reports `ErrUnknownAncestor` for it without verifying it (`A-08` WBFT-HDR-120).
15. `build_wait` takes the block period of the new height as `config_at` gives it, the time of the head, the round and the clock `now` in Unix nanoseconds, and returns `wait` in nanoseconds. The reference hands the builder a signed duration and the builder sleeps for it; a sleep of zero or less returns at once, so `wait` is the maximum of that duration and 0 (`A-06` WBFT-TIMER-040). The reference reads the clock with `time.Until`; the generator replaces that call to supply `now`.
16. `p256_verify` takes `config`, the block `number` and the precompile `input`, and returns `output` and `gas`, the gas that a call to `0x…0100` with 100 000 gas uses. Before `BohoBlock` the address is an account without code, so the output is empty and no gas is used.
17. `gas_tip_enforcement` and `fee_delegation` take `config`, the `header` of the block (its `GasTip`, `BaseFee`, `Coinbase` and number are the ones that execution reads), the `accounts` before the first transaction (§3.1 "Execution state") and the signed `transactions` in their binary encoding, and return per transaction a receipt (`status`, `gas_used`, `cumulative_gas_used`, `effective_gas_price`, `logs`) and the `balance` and `nonce` of every input account afterwards. A block with a transaction that is invalid (SNET-TX-060) is a fail case. The reference applies the transactions with `core.ApplyTransaction` in order, as `StateProcessor.Process` does; finalization is the subject of `process_finalize`.
18. `process_finalize` takes a chain fixture that ends at the parent of the block, the `header` of the block, `parent_gas_tip` (slot `0x39` of the genesis GovValidator in the parent state; `null` when the parent state is unavailable), the `accounts` after the block's transactions (none of them empty) and `path` (`import` for `Finalize` with `verify_epoch`, `propose` for `FinalizeAndAssemble` with `write_epoch`, `B-06` §6.1), and returns the state `root`, the `epoch_info` that the proposer path wrote (`null` on the import path or when nothing was written) and the `balance` and `code_hash` of the input accounts and of the accounts an upgrade touched. A block that finalization rejects is a fail case.
19. `candidates_at_epoch` takes `config`, the epoch block `number` and the `accounts` (the GovValidator account with its storage, and other accounts whose `extra` matters), and returns `candidates` (`address`, `bls_public_key` in list order, `B-08` §2.4 without the selection step) and `gas_tip`, the value of slot `0x39` at the genesis GovValidator address (`B-08` §4).
20. `scenarios` uses the format of §3.1 "Governance scenarios". The reference builds the genesis with `core.SetupGenesisBlock` and applies each step with `core.ApplyMessage`, as `core.ApplyTransaction` does; the write set is found by noting every `SSTORE` slot of the call and keeping the slots whose value changed.

[WBFT-VEC-031] For `header/verify_header` a runner MUST compare only the final verdict of a case: accept, reject, or deferred (the header is not rejected but kept for later, as for a header from the future, `A-08` §10.6). The kind of error that the reference reports for a rejected or deferred header is informative and MUST NOT be compared. Whether a header is deferred depends on the order of the checks in `A-08`, so that order stays observable through the verdict.
Source: `A-08` §10.6

[WBFT-VEC-030] The catalog MUST contain at least one vector for every requirement that is tagged `Observable:` but whose data is not available to an outside observer at the reference commit (WBFT-VEC-003), and for every requirement of classes `LV` and `OB`.
Source: this chapter (coverage rule)

### 3.4 Adapter protocol and schema ownership

The runner and the implementation under test are separate processes. They exchange one JSON object per line over standard input and output, with the protocol `wbft-vector/1`: the runner sends `hello` with the protocol name, the implementation answers `hello` with its name, version and the handlers it supports, the runner sends one `case` per vector with the converted `input.yaml` as `input`, the implementation answers `result` with `status` `ok` and an `output`, `error`, or `unsupported`, and the runner ends with `bye`. The runner converts the files to JSON (WBFT-VEC-013), compares `output` with the converted `expected.yaml`, and treats a crash or a timeout as a failed operation. A case of kind `steps` is sent whole (initial state and all steps), and the implementation returns the record of its observable outputs per step. WBFT-VEC-033 to WBFT-VEC-062 below fix the fields and rules of this exchange. An example exchange, in which the runner sends each case after the result of the previous one (WBFT-VEC-042):

```text
runner -> impl   {"type":"hello","protocol":"wbft-vector/1",
                  "runner":{"name":"inspector","version":"0.1.0"},"spec_commit":"def456"}
impl -> runner   {"type":"hello","protocol":"wbft-vector/1",
                  "impl":{"name":"wbft","version":"0.1.0","commit":"abc123","lang":"go","build":"cgo"},
                  "handlers":["crypto/keccak256","encoding/extra_codec","network/receive_outcome"],
                  "improvements":[]}
runner -> impl   {"type":"case","id":17,"runner":"encoding","handler":"extra_codec",
                  "case":"trailing_byte","kind":"pure","input":{"extra":"0xf8..."}}
impl -> runner   {"type":"result","id":17,"status":"error","error_class":"decode","message":"trailing bytes"}
runner -> impl   {"type":"case","id":18,"runner":"crypto","handler":"keccak256",
                  "case":"ascii_wbft","kind":"pure","input":{"data":"0x77626674"}}
impl -> runner   {"type":"result","id":18,"status":"ok","output":{"hash":"0x154d...c55f"}}
runner -> impl   {"type":"case","id":19,"runner":"network","handler":"receive_outcome",
                  "case":"codes_and_framing","kind":"steps","input":{"initial":{...},"steps":[...]}}
impl -> runner   {"type":"result","id":19,"status":"unsupported"}
runner -> impl   {"type":"bye"}
```

`error_class` and `message` are informative, like `expected_error` (WBFT-VEC-015).

[WBFT-VEC-032] The adapter protocol `wbft-vector/1`, the file format of §3.1 and the handler schema of §3.3 are part of this specification and are versioned with the vectors in the `wbft-spec` repository. The inspector, the Go and Rust implementations and the generator implement the protocol; they do not define it. A change of the protocol or of the schema that is not backward compatible MUST change the protocol name (`wbft-vector/2`) in the same commit as the vectors that need it.
Source: this chapter (schema ownership)

Protocol details. The requirements below fix the exchange described above. A runner is the program that reads the vectors and compares results (for example the inspector); an adapter is the program that runs the implementation under test and speaks the protocol. A session is the exchange between one runner and one adapter process, from `hello` to `bye`.

Transport.

[WBFT-VEC-033] Runner and adapter MUST exchange UTF-8 encoded JSON objects, one object per line, each line terminated by a single line feed. The runner writes to the adapter's standard input and reads the adapter's standard output.
Source: this chapter (adapter protocol)

[WBFT-VEC-034] An adapter MUST NOT write anything but protocol messages to its standard output. It writes diagnostics to standard error.
Source: this chapter (adapter protocol)

[WBFT-VEC-035] A runner MAY record what an adapter writes to standard error, and MUST NOT interpret it.
Source: this chapter (adapter protocol)

[WBFT-VEC-036] A runner MUST accept lines of at least 64 MiB.
Source: this chapter (adapter protocol)

Messages.

[WBFT-VEC-037] The first message of a session MUST be the runner's `{"type":"hello","protocol":"wbft-vector/1","runner":{"name":…,"version":…},"spec_commit":…}`, where `spec_commit` names the commit of the specification whose vectors the runner reads.
Source: this chapter (adapter protocol)

[WBFT-VEC-038] An adapter MUST answer the runner's `hello` with `{"type":"hello","protocol":"wbft-vector/1","impl":{"name":…,"version":…,"commit":…,"lang":…,"build":…},"handlers":[…],"improvements":[…]}`. `handlers` lists the handlers the adapter supports, each as `"<runner>/<handler>"`. `build` names how the implementation was built (`"cgo"`, `"nocgo"` or a language-specific value); it is informative, because the expected values follow the cgo build of the reference in every case (WBFT-VEC-022). `improvements` lists the IDs, and only the IDs, of the improvement items enabled in the implementation under test.
Source: this chapter (adapter protocol)

[WBFT-VEC-039] If the protocol name in the adapter's `hello` differs from the runner's, the runner MUST end the session and report a usage error.
Source: WBFT-VEC-032

[WBFT-VEC-040] A runner MUST send a case as `{"type":"case","id":…,"runner":…,"handler":…,"case":…,"kind":…,"input":…}` and with nothing else: `id` is a JSON integer unique within the session, `runner`, `handler`, `case` and `kind` are the fields of `meta.yaml`, and `input` is `input.yaml` converted to JSON (WBFT-VEC-013). No other content of `meta.yaml` and no content of `expected.yaml` is sent. The value rules of WBFT-VEC-014 apply to `input` and `output`, not to the fields of the envelope such as `id`.
Source: this chapter (adapter protocol)

[WBFT-VEC-041] An adapter MUST NOT read the vector files. Everything it needs is in the `case` message, and the runner records which files a run used (WBFT-VEC-012).
Source: WBFT-VEC-012

[WBFT-VEC-042] A runner MUST NOT send a case before it has received the result of the previous case of the same session. A runner that runs cases in parallel starts several adapter processes.
Source: this chapter (adapter protocol)

[WBFT-VEC-043] An adapter MUST answer each case with `{"type":"result","id":…,"status":…}`, where `id` is the `id` of the case and `status` is `"ok"`, `"error"` or `"unsupported"`. With `"ok"` the result MUST also have `output`, a JSON object with the output fields of the handler schema (§3.3) whose values follow WBFT-VEC-014: integers as decimal strings without leading zeros, with a minus sign only in a field that the handler schema declares signed (`timeout` of `timers/round_timeout`); byte strings as lowercase hexadecimal of even length with the prefix `0x`; `null` for absent values. With `"error"` the result MAY have `error_class` and `message`, which are informative. `"unsupported"` means that the implementation does not provide the operation of this case.
Source: WBFT-VEC-014

[WBFT-VEC-044] A runner MUST treat as a protocol error a line from the adapter that is not a JSON object, a message whose `type` is not the one expected at that point of the session, and a result whose `id` is not that of the pending case. It then proceeds as when the adapter exits (WBFT-VEC-048, WBFT-VEC-052).
Source: this chapter (adapter protocol)

[WBFT-VEC-045] A runner MUST end a session with `{"type":"bye"}` and then close the adapter's standard input.
Source: this chapter (adapter protocol)

[WBFT-VEC-046] After `bye` an adapter MUST exit with status 0 within 5 seconds. A runner MAY terminate an adapter that has not exited by then.
Source: this chapter (adapter protocol)

Decision of a case.

[WBFT-VEC-047] A runner MUST decide that a case with an `expected.yaml` passes if and only if the result has status `"ok"` and `output` is equal to `expected.yaml` converted to JSON: the same keys at every level, lists of the same length in the same order, and equal strings, booleans and nulls. An `output` with a key that `expected.yaml` does not have, or without a key that it has, fails. For `header/verify_header` the comparison covers `verdict`, the only field of its `expected.yaml` (WBFT-VEC-031).
Source: WBFT-VEC-031

[WBFT-VEC-048] A runner MUST decide that a case without `expected.yaml` passes if and only if the operation fails: the result has status `"error"`, or the adapter exits or exceeds the time limit of the case before it answers. A result with status `"ok"` for such a case is a failure.
Source: WBFT-VEC-010

[WBFT-VEC-049] A runner MUST report a case without `expected.yaml` that passed because the adapter exceeded the time limit apart from the other passed cases, so that an implementation that hangs instead of failing stays visible.
Source: this chapter (adapter protocol)

[WBFT-VEC-050] A runner MUST report a case as unsupported, neither passed nor failed, when no adapter of the run lists its handler or every adapter that lists it answered `"unsupported"`.
Source: this chapter (adapter protocol)

Sessions.

[WBFT-VEC-051] A runner MUST apply a time limit to every case.
Source: this chapter (adapter protocol)

[WBFT-VEC-052] When the adapter exits, a case exceeds its time limit or a protocol error occurs, a runner MUST terminate the adapter, start a new adapter process and exchange `hello` again before it sends the next case.
Source: this chapter (adapter protocol)

[WBFT-VEC-053] An adapter MUST compute the result of each case independently of the cases it received before.
Source: this chapter (adapter protocol)

[WBFT-VEC-054] An implementation MAY be tested through several adapters, for example one for the consensus layer and one for the execution layer. A runner then MUST offer each case to the adapters that list its handler, in the order the user gave, and MUST offer a case answered `"unsupported"` to the next of them; it reports the results as results of one implementation.
Source: this chapter (adapter protocol)

Steps cases. The `output` of a case of kind `steps` of a `state_machine` or `network` handler holds `start` and `steps` in the record format of §3.1 "Steps".

[WBFT-VEC-058] For a case of kind `steps` of a `state_machine` or `network` handler an adapter MUST build the node as after `Start` from `initial` (§3.1 "Steps"), not from any other state.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

[WBFT-VEC-059] For a case of kind `steps` of a `state_machine` or `network` handler an adapter MUST process exactly the events that the steps give, in their order, and no other event: no timer of the implementation fires, and no scheduled event is processed unless a step gives it.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

[WBFT-VEC-060] For a case of kind `steps` of a `state_machine` or `network` handler an adapter MUST process a step that gives a scheduled event (`message` as a self-delivery, `backlog`, `request`) as given, whether or not the implementation scheduled that event. To do so it keeps every event scheduled during the case in a queue addressed by content until a step processes it.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

[WBFT-VEC-061] An adapter that cannot report the state variables of `A-05` §3.1 MUST answer `"unsupported"` for every case of a `state_machine` or `network` handler of kind `steps` instead of returning partial records.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

Generator.

[WBFT-VEC-062] The generator MUST provide an adapter that answers every case of the vectors it wrote with the values computed from the reference, so that a runner can check the vectors against their generator.
Source: WBFT-VEC-032

---

## 4. Maintenance

- When the reference commit changes (`A-12` §4), all vectors are regenerated and the diff of expected values is reviewed; a changed expected value is either a consensus change (recorded in `A-12`) or a generator bug.
- Requirement IDs are stable, so a vector keeps its IDs across regenerations.
- The vectors move with the specification to the `wbft-spec` repository, together with the adapter protocol and the schema (WBFT-VEC-032).
