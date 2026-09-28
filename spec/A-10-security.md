# A-10 Security considerations

- Area code: `SEC`
- Status: draft
- Reference: go-stablenet `740526d03`.

This chapter states the assumptions under which the guarantees of Part A hold, and the properties of the reference implementation that weaken or depend on them. Each section follows the same order: the assumption or threat, what the reference implementation does (with sources), a normative requirement where one can be stated without changing observable behaviour.

Requirements in this chapter are of two kinds: *assumption requirements* ("a deployment MUST …") that a conforming network has to satisfy for Part A's guarantees to hold, and *behaviour requirements* on conforming nodes. Neither kind changes validity rules of Part A.

Security considerations of the system contracts (native coin moved by signatures, the replay domain of token signatures, blacklist timing) are in `B-04` §13.

---

## 1. Threat model

| Element | Assumption |
|---|---|
| Validators | `N = |validators_at(h)|` per height, all with equal weight (`A-04`: every candidate has power 1). At most `f` of them are byzantine, with `3f < N` counted by number of validators. Byzantine validators may collude, sign anything with their own keys, withhold messages and blocks, and choose message timing. |
| Honest validators | follow Part A, keep their node keys secret, and have clocks within `AllowedFutureBlockTime` of each other (§10, WBFT-SEC-090). |
| Network | partially synchronous: after an unknown global stabilisation time, messages between honest validators are delivered within a bound smaller than the round timeout of `A-06`. Before it, the adversary may delay, reorder, duplicate and drop messages, but cannot forge signatures. Validators are directly connected (`A-07`: consensus messages are sent only to validator peers; validators relay a message to their validator peers after processing it successfully, so relay stalls wherever a message is still future for the relaying node — `WBFT-SEC-002`). |
| Non-validator peers | untrusted. They can send arbitrary bytes on `istanbul/100` and `eth` protocols. |
| Execution | deterministic: every honest node computes the same post-state for the same block and parent state (§9). |
| Local operator interfaces | the Engine API (on authrpc with JWT, and on IPC without it), admin and miner RPC are not reachable by the adversary (§13). |
| Cryptography | ECDSA secp256k1, BLS12-381 (proof-of-possession scheme, `A-02`), Keccak-256 are secure; BLS public keys in `EpochInfo` have proven possession (§4). |

The QBFT formal specification (commit `1630128e7`) proves its safety properties under a model close to this one: an adversary that controls at most `f` validators of a fixed set, signs anything with its own keys and replays any message it has received, but forges no signature of an honest node; and a network that may delay a message without bound or never deliver it (`dafny/ver/L1/distr_system_spec/adversary.dfy:18-82`, `dafny/ver/L1/distr_system_spec/network.dfy:29-43`). The proof also assumes that the validator set never changes and that honest nodes never lose their state. WBFT does not meet those two assumptions (`A-05` §18.5).

[WBFT-SEC-001] A deployment MUST keep the number of byzantine validators of every height strictly below `N/3`. Under this assumption, for every height at most one block collects valid committed seals (`A-05`); none of the mechanisms of this chapter detects or recovers from a violation.
Source: `consensus/wbft/validator/default.go:222-229`, `consensus/wbft/engine/engine.go:1338-1369`, `consensus/wbft/engine/engine.go:881`

[WBFT-SEC-002] A deployment SHOULD connect every pair of validators directly on `istanbul/100`. Consensus messages are sent only to validator peers; a node relays a received message to its validator peers only after it has processed the message successfully (not while it is backlogged as future, and never when it is classified old or is invalid; a message classified as an extra-seal message is relayed whenever the extra-seal store returns without error, that is a PREPARE or COMMIT whose digest and seal verify, and any message (including a PRE-PREPARE or a message with an unverifiable seal) when the node holds no target proposal, `A-07` WBFT-NET-042). A non-validator receives no consensus messages from conforming nodes (`A-07` §6.2) and therefore normally relays none. Validators that are not directly connected therefore see each other's messages late or not at all.
Source: `consensus/wbft/backend/backend.go:176-210`, `eth/handler_istanbul.go:42-54`, `consensus/wbft/core/handler.go:128-150, 210-221`, `consensus/wbft/core/extraseal.go:38-47`

---

## 2. Quorum arithmetic

`quorum_size(N) = ceil(N − (N−1)/3)` evaluated in IEEE-754 double precision (`consensus/wbft/validator/default.go:222-229`). For all `N < 3·2^50 + 3` (in particular every realistic validator count) this equals the exact rational ceiling `ceil((2N+1)/3)`; the first value at which binary64 rounding makes it differ is `N = 3·2^50 + 3`. Below that bound the intersection of two quorums then contains at least `f + 1` validators for `f = floor((N−1)/3)`, which is the basis of safety. For `N ≡ 0 or 2 (mod 3)` the quorum is larger than `2f + 1` (e.g. `N = 6`: `f = 1`, quorum 5), so such sizes tolerate no more faults than `N − 1` or `N − 2` validators would but need more signatures for liveness.

[WBFT-SEC-010] A conforming node MUST compute `quorum_size` with the formula above and MUST NOT substitute `2f + 1` for sizes where they differ.
Source: `consensus/wbft/validator/default.go:222-229`
Observable: header

Informative. The substitution forbidden by WBFT-SEC-010 is the one a port of Quorum QBFT is most likely to carry. At Quorum commit `5ffacc48`, `QuorumSize` returns `2F + 1` with `F = ceil(N/3) − 1 = (N − 1) // 3 = f` whenever a `2FPlus1Enabled` transition is active, `Ceil2Nby3Block` is unset (the deprecated `istanbul` genesis section without `ceil2Nby3Block`), or the sequence is below `Ceil2Nby3Block`; only otherwise does it use `ceil(2N/3)` (`consensus/istanbul/qbft/core/core.go:306-313`, `consensus/istanbul/validator/default.go:205`, `eth/ethconfig/config.go:248`; `A-04` §2.3).

Informative. A set of `N` validators tolerates `F = (N − 1) // 3` faulty validators, and `3F + 1` is the smallest size that tolerates `F`. Sizes `3F + 2` and `3F + 3` tolerate the same `F` with a larger quorum: `N = 5` and `N = 6` tolerate one faulty validator, as `N = 4` does, but need quorums of 4 and 5 instead of 3. Validator counts of the form `3F + 1` therefore use the set most efficiently.

---

## 3. Replay domains

A signature is replayable in every context in which its signed bytes are accepted. The signed bytes of WBFT artefacts are (`A-02`, `A-03`):

| Artefact | Signed bytes | Bound to chain? | Bound to height and round? |
|---|---|---|---|
| PRE-PREPARE (ECDSA) | `rlp([0x12, [seq, round, block]])` | yes, through the block (parent hash) | yes |
| PREPARE, COMMIT (ECDSA) | `rlp([code, [seq, round, digest, seal]])` | yes, through `digest` | yes |
| ROUND-CHANGE with prepared certificate | `rlp([0x15, [seq, round, [prepared_round, prepared_digest]]])` | through `prepared_digest` | yes |
| Seal (BLS) | `seal_data(header, round, type)` | yes (header) | yes |
| Randao reveal (ECDSA) | `randao_data(chain_id, number)` (already a keccak256 digest, `A-02` §7.1) | yes (`chain_id`) | height only |

Source: `consensus/wbft/messages/preprepare.go:50-56`, `consensus/wbft/messages/prepare.go:61-67`, `consensus/wbft/messages/commit.go:49-55`, `consensus/wbft/messages/roundchange.go:158-182`, `consensus/wbft/core/core.go:465-469`, `consensus/wbft/engine/engine.go:563-573`

[WBFT-SEC-021] Within one chain, a node MUST accept a replayed consensus message exactly as it accepts the original (messages carry no nonce or timestamp); duplicate suppression is by `dedup_key(payload)` (`A-03`: keccak256 of the RLP string encoding of the payload bytes, `A-07`) and by per-sender replacement in the message sets (`A-05`), not by signature uniqueness.
Source: `consensus/wbft/backend/handler.go:54-67, 83-95`, `consensus/wbft/utils.go:31-36`, `consensus/wbft/core/qbft_msg_set.go:66-71`
Observable: network

---

## 4. Key coupling and key management

The node key (secp256k1) is the single secret of a validator. From it the reference implementation derives:

- the p2p identity (enode) and therefore the address used to select consensus peers (`eth/handler_istanbul.go:42-54, 164-166`),
- the validator address (`Coinbase`, message source, `etherbase` forced to it: `eth/backend.go:173-176`),
- the ECDSA key signing consensus messages and the randao reveal (`consensus/wbft/backend/backend.go:281-284`),
- the BLS secret key, deterministically: `bls_secret = KeyGen(ikm = node_key_bytes)` (`consensus/wbft/backend/backend.go:65`, `crypto/bls/bls.go:91-93`, `crypto/bls/blst/secret_key.go:36-46`).

Consequences: compromise of the node key compromises all four roles at once; the BLS key cannot be rotated without changing the node key and therefore the validator address; a remote signer for seals only is impossible without code changes.

The BLS public key that verifiers use is not derived by them: it is the key registered in the validator registry and copied into `EpochInfo` (`B-08`). The registry checks a proof of possession and uniqueness of the key (`systemcontracts/solidity/v1/GovValidator.sol:64-76, 157-165`), which prevents rogue-key aggregation. Consensus itself does not check that the keys in `EpochInfo` are distinct.

[WBFT-SEC-030] A validator MUST register in the registry the BLS public key derived from its node key as above; otherwise its seals do not verify and it counts as absent in every quorum.
Source: `consensus/wbft/backend/backend.go:65, 287-289`, `consensus/wbft/engine/engine.go:1345-1366`
Observable: header, state

[WBFT-SEC-031] A deployment MUST ensure that the BLS public keys of the genesis validator set are distinct and have proven possession, because genesis keys are taken from configuration and are not checked by the registry. No component of the reference checks this (`params/config_wbft.go:76-129`, `systemcontracts/gov_validator.go:113-163`).
Source: `consensus/wbft/engine/engine.go:1104-1107, 660-662`
Source: `params/config_wbft.go:76-129` (CheckValidity checks only that the key list is non-empty and as long as the validator list), `systemcontracts/gov_validator.go:113-163` (duplicate addresses are skipped; a repeated key overwrites the earlier `blsKeyToValidator` entry)

Because the node key is also every validator key, a key-recovery attack on the p2p layer is an attack on the validator. The RLPx handshake decrypts unauthenticated network input with ECIES, that is ECDH between the node key and a point chosen by the sender (`p2p/rlpx/rlpx.go:603-626`). If that point is not checked to lie on secp256k1, a sender can submit points of small order on other curves (an invalid-curve attack) and learn the node key modulo small primes from whether decryption succeeds. The reference rejects such points before the scalar multiplication: ECIES `GenerateShared` returns `ErrInvalidPublicKey` for a point that is not on the curve (`crypto/ecies/ecies.go:123-129`), `UnmarshalPubkey` and `IsOnCurve` reject coordinates not below the field prime `P` (`crypto/crypto.go:176-185`, `crypto/secp256k1/curve.go:75-78`), and the cgo scalar multiplication rejects such coordinates as well (`crypto/secp256k1/ext.h:108-116`). The RLPx test `TestHandshakeECIESInvalidCurveOracle` checks that an auth message whose ephemeral point is replaced by an off-curve point fails with `ErrInvalidPublicKey`. These checks were added after `v1.0.0` (`A-12` §3).

[WBFT-SEC-032] A conforming node MUST reject every secp256k1 public key or point received from the network that it would multiply by its node key or by one of its ephemeral keys (the ECIES point `R` of an RLPx `auth` or `ack` message, the RLPx ephemeral and static keys, and discovery keys used in ECDH) unless both coordinates are below the field prime `P` and the point satisfies `y² = x³ + 7`. The rejection MUST happen before the scalar multiplication, so that it does not depend on the MAC check or on any later step.
Source: `crypto/ecies/ecies.go:123-129, 293-311`, `crypto/secp256k1/ext.h:108-116`, `crypto/crypto.go:176-185`, `crypto/secp256k1/curve.go:75-78`, `p2p/rlpx/rlpx.go:648-666`, `p2p/rlpx/rlpx_oracle_poc_test.go:12-57`

---

## 5. Equivocation

Within one run of the consensus core, honest nodes send at most one PREPARE and one COMMIT per `(sequence, round)`, and at most one PRE-PREPARE per `(sequence, round)` for rounds above 0: a PREPARE is sent only on accepting a PRE-PREPARE in state `AcceptRequest`, a COMMIT only on the transition to `Prepared` (`A-05`). A byzantine validator can sign two different digests for the same view. At round 0 an honest proposer can send two PRE-PREPAREs with different blocks for the same view: while it is still in `AcceptRequest` (for example because the self-delivery of its first PRE-PREPARE was deferred as a future block), a second request for the same sequence sends another PRE-PREPARE, because the round-0 request path does not check `preprepareSent` (`A-05` WBFT-SM-082). Across a stop and a start of the consensus core (for example a restart) the reference keeps no record of what it sent (`A-05` WBFT-SM-007), so after a restart it can sign a different PREPARE, COMMIT or proposal for a `(sequence, round)` it had already signed. `A-05` states both qualifications in the note after WBFT-SM-082.

The reference implementation does not detect or record equivocation:

- message sets keep one message per sender and overwrite (`consensus/wbft/core/qbft_msg_set.go:66-71`);
- a PREPARE or COMMIT whose digest differs from the current proposal is rejected without being stored (`A-05`);
- a second PRE-PREPARE for the same view is ignored once the node has left `AcceptRequest` (`consensus/wbft/core/backlog.go:166-173`, `consensus/wbft/core/preprepare.go:172`);
- there is no evidence type, no slashing, and no penalty.

[WBFT-SEC-040] Within one run of its consensus core, a conforming node MUST NOT sign two different digests in PREPARE, or two different digests in COMMIT, for the same `(sequence, round)`, and a proposer MUST NOT sign two different proposals for the same `(sequence, round)` with `round > 0`. The reference implementation also signs two different round-0 proposals for one view while it is still in `AcceptRequest` (`A-05` WBFT-SM-082), and keeps no record of signed messages across a restart (`A-05` WBFT-SM-007); an implementation SHOULD avoid both (a local-only change).
Source: `consensus/wbft/core/preprepare.go:42-51, 171-194`, `consensus/wbft/core/prepare.go:47`, `consensus/wbft/core/commit.go:49`, `consensus/wbft/core/request.go:47-56`, `consensus/wbft/backend/backend.go:355-382`
Observable: network

[WBFT-SEC-041] An observer that sees two valid signatures of one validator on different digests for the same view (PREPARE, COMMIT, or a PREPARE justification inside ROUND-CHANGE/PRE-PREPARE) SHOULD report it as equivocation; the reference implementation offers no other detection.
Source: `consensus/wbft/core/qbft_msg_set.go:66-71`
Observable: network

The seals in headers are not evidence of equivocation within a height because only one digest per height reaches the chain (under `WBFT-SEC-001`).

---

## 6. Peer accountability

Consensus-level validity (signature, sender membership, view, proposal validity) is evaluated asynchronously in the consensus event loop after the network handler has already returned success. The only errors that terminate the `istanbul/100` connection are decode failures of the frame, oversize frames, and "engine stopped" outside synchronisation (`eth/handler_istanbul.go:115-153`, `consensus/wbft/backend/handler.go:70-101`). Invalid proposals, invalid seals and messages from non-validators cost the receiver signature recovery and are then dropped, with no score or disconnection.

On the `eth` protocol, a peer whose propagated block fails header verification in the block fetcher with any error other than `ErrFutureBlock` is dropped (`eth/fetcher/block_fetcher.go:858-872`).

[WBFT-SEC-050] A conforming node MUST verify the ECDSA signature and validator membership of every consensus message (and of every piggybacked justification) before storing it in any backlog or message set.
Source: `consensus/wbft/core/handler.go:188-209, 268-317`, `consensus/wbft/core/core.go:452-462`
Observable: network

---

## 7. Resource limits

| Resource | Limit in the reference implementation | Source |
|---|---|---|
| `istanbul/100` frame | 10 MiB | `eth/handler.go:59`, `eth/handler_istanbul.go:138-140` |
| Proposal (block) inside PRE-PREPARE and ROUND-CHANGE | frame limit only; no header sanity check (`Number`, `Difficulty` bit length, `Extra ≤ 100 KiB`) is applied to proposals | `core/types/block.go:149-167`, `eth/protocols/eth/handlers.go:305-307` (applied to `NewBlockMsg` only) |
| Future messages per validator | `4 · (10 + 1) · (1 + 1) = 88`; sequence ≤ current + 1; at the current sequence round ≤ current round + 10, at sequence current + 1 round ≤ 9 | `consensus/wbft/core/backlog.go:44-50, 76-105` |
| Duplicate caches | 40 peers × 1024 hashes; 1024 own hashes | `consensus/wbft/backend/engine.go:39-42` |
| `EpochInfo`, sealer bitmap sizes in headers | none beyond decoding | `core/types/istanbul.go:99-103, 290-325` |
| ROUND-CHANGE retransmission | re-created every `RequestTimeout`, each carrying the prepared block if any; a byte-identical copy is not sent again to a peer whose recent cache holds its key; the retry is a different message, and is sent, after a failed `finalize`, after the `CATCH_UP` branch and for a retry processed in a new height (`A-14` §5.8) | `A-06` WBFT-TIMER-021, WBFT-TIMER-024, `A-07` WBFT-NET-032 |

There is no per-sender or per-view rate limit: every frame with new bytes is decoded and its signatures recovered (`consensus/wbft/backend/handler.go:66-95`, `consensus/wbft/core/handler.go:185-209`). The cost per frame is bounded by the frame size: one ECDSA recovery for the message plus one per justification item until the first failure (`consensus/wbft/core/handler.go:285-312`). Honest retransmission of ROUND-CHANGE adds no bandwidth in the normal case: signing is deterministic, so a retransmitted ROUND-CHANGE is byte-identical and is skipped for every peer that already has its key (`A-06` WBFT-TIMER-024). It reaches the wire only towards peers first reached after the original send, or whose cache entry was evicted, except in the three cases of `A-14` §5.8 in which the retry differs from the original and reaches every validator peer. A retry is suppressed only when it repeats an earlier message byte for byte (`A-06` WBFT-TIMER-024).


---

## 9. Bad blocks with quorum seals

PRE-PREPARE validation does not execute the proposal (`A-09 §4.6`). A byzantine proposer can therefore obtain a COMMIT quorum on a block whose execution fails (for example a transaction from a blacklisted sender, an invalid state root, a wrong `EpochInfo`). Every honest non-proposer then fails to import it, records the hash as bad and continues the height in a new round (`A-09 §5.3`). Liveness costs one round plus the time to execute the bad block; safety is unaffected because the bad block never becomes canonical at honest nodes.

Two cases are not benign:

- **Non-deterministic execution.** If honest nodes disagree about whether the block executes, some store it and advance while others record it as bad and decide another block for the same height. Both groups hold a block with valid committed seals at that height: the chain splits, and the TD head rule (`A-08 §9`) does not resolve it until one branch is longer.
- **Honest builder of the bad block.** The builder stores its own block without re-executing it (`WBFT-APP-080`). If its building and the others' import disagree, the builder alone advances.

[WBFT-SEC-080] Execution of a finalized block MUST be deterministic across all conforming implementations; a node MUST NOT make the result of execution depend on local configuration, timing or resources.
Source: `core/blockchain.go:1778-1792`, `core/state_processor.go:60-107`
Observable: state

[WBFT-SEC-081] After a bad block at height `h`, a conforming node MUST NOT PREPARE or COMMIT the bad block again at height `h` (it rejects it at PRE-PREPARE and drops it as prepared block on round change). In the reference this protection lasts while the bad-block record is retained (the 10 highest-numbered records, `A-09` WBFT-APP-004).
Source: `consensus/wbft/backend/backend.go:266-270`, `consensus/wbft/core/core.go:278-286`
Observable: network

---

## 10. Clock assumptions

- Proposals carry `Time = max(parent.Time + BlockPeriod, proposer_now)` (`WBFT-HDR-013`).
- A verifier treats a proposal with `Time > now + AllowedFutureBlockTime` as future and re-processes it at `Time`, with no upper bound on the wait (`WBFT-HDR-063`). `AllowedFutureBlockTime` defaults to 0.
- Round timers are local and relative (`A-06`); they do not use block time.

A proposer whose clock runs ahead delays its own proposal at every honest verifier by the offset; if the offset exceeds the round timeout the proposal is not accepted in that round. Its block, once finalized, moves chain time forward; subsequent honest proposers must use at least `parent.Time + BlockPeriod`, which is ahead of their clocks, and their proposals are delayed in turn until wall-clock time catches up. A byzantine proposer can thus slow the chain but not break safety. Honest clocks ahead of the others cause the same effect unintentionally.

[WBFT-SEC-090] A deployment MUST synchronise validator clocks to within `AllowedFutureBlockTime` of each other for proposals to be accepted without delay, and to well within the round-0 timeout for liveness.
Source: `consensus/wbft/engine/engine.go:201-205, 501-505`, `consensus/wbft/core/preprepare.go:150-163`

---

## 11. Randomness

`MixDigest(h) = MixDigest(h−1) XOR keccak256(RandaoReveal(h))` (`A-02`); `MixDigest` is exposed to contracts as `PREVRANDAO` (`core/evm.go:61-63`) and seeds the validator shuffle of the next epoch at an epoch block (`consensus/wbft/engine/engine.go:1265-1277`).

- The reveal of an honest proposer is deterministic (RFC 6979), so the mix sequence of an all-honest chain is fixed by the heights and by which validator built each block (which round changes can alter).
- The last contributor has the usual RANDAO bias: it can withhold (not propose), leaving the height to the next proposer and a different reveal.
- The proposer knows the mix of its own block before proposing it.


---

## 13. Engine API exposure

On WBFT chains the node still registers the Engine API (`engine_*`) unless it runs in developer mode (`cmd/gstable/config.go:223-235`, `eth/catalyst/api.go:44-54`). It is served on the JWT-authenticated authrpc endpoint, which starts whenever an authenticated API is registered (`node/node.go:501-508`), and, without JWT, on the IPC endpoint and in-process, which receive every registered API except `personal` (`node/node.go:379-398`). There is no option to turn it off.

[WBFT-SEC-120] A WBFT node MUST NOT expose the Engine API to any party other than its operator: a deployment MUST keep authrpc bound to a local interface with a secret JWT key and SHOULD disable IPC (`--ipcdisable`) or restrict the IPC socket to the operator.
Source: `cmd/gstable/config.go:223-235`, `eth/catalyst/api.go:44-54, 265-371`, `node/node.go:379-398, 501-508`, `consensus/merger.go:60-94`, `eth/handler.go:228-291`

---

## 14. Integrity of observed headers

The seal fields `Round`, `PreparedSeal`, `CommittedSeal` of a stored header are node-local and are not covered by the block hash (`WBFT-HDR-053`). An RPC client that reads a header from a single node receives that node's copy. The copy is authentic if its seals verify (`verify_light`), but its sealer sets are not a canonical record of participation; the canonical record of who sealed block `h` is the previous-seal fields of block `h+1`, which are hash-covered.

[WBFT-SEC-130] An observer that attributes participation to validators MUST use `PrevPreparedSeal` and `PrevCommittedSeal` of block `h+1` as the record for block `h`, and MAY use the seals stored in block `h` only as a per-node view.
Source: `core/types/istanbul.go:263-288`, `consensus/wbft/engine/engine.go:515-537`
Observable: header, rpc
