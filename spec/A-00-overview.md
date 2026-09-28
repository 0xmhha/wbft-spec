# A-00. Overview

WBFT is the Byzantine fault tolerant consensus protocol of StableNet. It decides one block per height among a set of validators, gives immediate finality, and records the evidence of each decision (aggregated BLS seals) in the block header. It descends from Istanbul BFT / QBFT but is a separate protocol with its own message rules, seal format, epochs, validator selection and randomness; where this specification and the QBFT literature differ, this specification is correct.

This chapter explains what the specification covers, how the parts fit together, and who has to conform to what. It contains no requirements.

---

## 1. Scope

The specification has two parts.

**Part A — WBFT consensus** defines what a node must do to take part in, or verify, WBFT consensus:

- the data types, constants and height-dependent configuration (`A-01`),
- the cryptographic operations: ECDSA message signatures, BLS12-381 seals, the randao reveal and mix (`A-02`),
- the byte-level encodings: the consensus data in the header extra, the four consensus messages and their signing payloads, the block hash rule (`A-03`),
- which validators seal each height, how epochs are delimited, how the proposer of each round is chosen, and how the validator set of the next epoch is computed (`A-04`),
- the consensus state machine: views, states, message acceptance, PRE-PREPARE / PREPARE / COMMIT / ROUND-CHANGE, justification, late seals, decision (`A-05`),
- timers (`A-06`), the `istanbul/100` network sub-protocol (`A-07`),
- the consensus fields of the header and how headers are built and verified (`A-08`),
- what the consensus protocol requires from the execution layer (`A-09`),
- security considerations (`A-10`), conformance and test vectors (`A-11`), version notes (`A-12`), and catalogs (`A-13`).

**Part B — StableNet block validity** defines the rules outside consensus that a node must apply to produce and accept exactly the blocks go-stablenet produces and accepts: chain configuration and forks, genesis, execution-side header and body rules, the system contracts and their storage, the governance semantics that decide who the validator candidates are, block finalization (upgrades, base-fee distribution, epoch info, gas tip), Anzeon transaction rules, the binding of Part A's application interface to contract state, and synchronisation and RPC. `B-00` gives the chapter map.

The boundary between the parts is the application interface of `A-09`: Part A asks for candidates, proposer eligibility, the gas tip, block building, proposal validation and block import; Part B (`B-08` and the chapters it cites) says how StableNet answers.

Out of scope: the EVM itself (StableNet follows go-ethereum at the reference commit except where Part B says otherwise), the transaction pool policy (described informatively only), and client user interfaces. The `eth/68` and `snap/1` protocols and devp2p discovery follow their public specifications. They are not out of scope as a whole: the values a StableNet node must use in them (network ID, fork ID, total difficulty) and the places where the reference implementation is stricter than go-ethereum v1.13.15 (it carries later upstream changes) are specified in `B-09` §12.

---

## 2. How the protocol works in one pass

For each height `h`, the validator set `V(h)` is taken from the epoch information recorded in the header of the last epoch block before `h` (`A-04`). The proposer of round `r` is a deterministic function of the previous block's proposer and `r`. Every validator builds a candidate block; only the proposer of the current round broadcasts it in a PRE-PREPARE (`A-05`).

A validator that accepts the PRE-PREPARE signs the block digest with its BLS key and broadcasts a PREPARE carrying that *prepare seal*. On a quorum `Q` of PREPAREs it broadcasts a COMMIT carrying a *commit seal*. On a quorum of COMMITs the block is decided: the node aggregates the quorum of prepare seals and commit seals into the header's `prepared_seal` and `committed_seal`, and hands the sealed block to the execution layer. Seals that arrive after the quorum (*extra seals*) are merged into the next block's `prev_prepared_seal` / `prev_committed_seal`, so the chain records who sealed each block. The hash of a block excludes its own seals and round, so all nodes agree on the block hash even though their copies of the seal fields may differ (`A-03`, `A-08`).

If a round does not decide within its timeout, validators move to the next round and broadcast ROUND-CHANGE messages that carry any prepared block and its PREPARE certificate. The proposer of the new round re-proposes the highest prepared block, or its own block if none was prepared, with a justification that every receiver checks (`A-05`, `A-06`).

At an epoch block the proposer computes the next epoch's candidates, their diligence scores (from the seal history of the epoch) and the shuffled validator order, and writes them into the header; every other node recomputes and compares them when it executes the block (`A-04`, `B-06`). Candidates and their BLS keys come from the `GovValidator` system contract, whose membership is governed on chain (`B-04`, `B-05`, `B-08`).

Consensus messages travel on the `istanbul/100` devp2p sub-protocol, only between directly connected validators, and are relayed after successful processing (`A-07`). Decided blocks reach non-validators through the normal eth block propagation and synchronisation.

---

## 3. Who conforms to what

`A-11` defines four conformance classes. In short:

| Class | Typical implementation | Must implement |
|---|---|---|
| Consensus participant | a validator node (`wbft` + `wbft-stablenet`, go-stablenet) | all of Part A and Part B |
| Verifying node | a full node that does not validate | Part A except the sending behaviour of `A-05`/`A-06`/`A-07`, and all of Part B |
| Light verifier | a bridge or light client that checks finality from headers | `A-01`, `A-02`, `A-03`, the validator-set and epoch sections of `A-04` (not the next-epoch computation), the light procedure of `A-08` |
| Observer | the WBFT inspector | decoding (`A-02`, `A-03`), and the checks of every requirement tagged `Observable` |

---

## 4. Reference implementation and versions

The specification was written from go-stablenet at commit `740526d03` (branch `dev`). Every requirement cites the code it was derived from in `Source:` lines. The network release at the time of writing, `v1.1.0`, has no observable consensus or block-validity difference from the reference commit (`A-12`).

---

## 5. Reading guide

- First, for everyone: `A-00`, then `A-14`, which shows the protocol as a message exchange between nodes (normal case, round change, consensus after a round change) before the chapters that specify each part.
- To implement a validator: `A-14` → `A-01` → `A-03` → `A-04` → `A-05` → `A-06` → `A-07` → `A-08` → `A-09`, then Part B from `B-00`.
- To verify finality from outside: `A-02`, `A-03`, `A-04` §§1–5, `A-08` (light verification), `A-11` (vectors).
- To build an inspector: `A-11` first (which requirements are observable and how vectors are organised), then the chapters it points to.
- Names: `NAMES.md` is the canonical name table. Conventions: `README.md`.
