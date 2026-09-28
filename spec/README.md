# WBFT Specification

This directory contains the normative specification of the WBFT consensus protocol (Part A) and of the StableNet block-validity rules that a WBFT node needs in order to interoperate with go-stablenet (Part B).

- Status: draft, work in progress
- Some requirements are withheld from this edition until their review is complete; they will be added in a later edition. Requirement numbers are therefore not contiguous.
- Reference implementation: go-stablenet, commit `740526d03` (branch `dev`; `git describe`: `v1.0.0-72-g740526d03`). `v1.1.0` is not an ancestor of this commit: both descend from the merge base `0bf2f4d1b`, with 2 further commits on the `v1.1.0` side and 47 on the `740526d03` side; the content comparison is in `A-12`. All source references in this specification point to this commit.
- Differences from release `v1.1.0` (`71e3f820f`) are recorded in `A-12-version-notes.md`.

---

## 1. Document map

### Part A — WBFT consensus

| File | Title | Area codes |
|---|---|---|
| `A-00-overview.md` | Overview, scope, conformance classes | — |
| `A-14-protocol.md` | Protocol overview (read right after `A-00`): the message exchange of one height, failures that lead to a round change, round change by the stage at which it starts, consensus after a round change | `PROTO` |
| `A-01-notation-types-parameters.md` | Notation, primitive types, constants, configuration parameters | `TYPE`, `PARAM` |
| `A-02-cryptography.md` | Hashing, ECDSA, BLS12-381, seal data, randao | `CRYPTO` |
| `A-03-encoding.md` | RLP conventions, `WBFTExtra`, aggregated seals, `EpochInfo`, block hash rule, consensus message wire formats and signing payloads | `ENC`, `MSG` |
| `A-04-validators-epochs-proposer.md` | Validator set per height, epoch boundaries, quorum, proposer selection, next-epoch computation (diligence, shuffle) | `VAL`, `EPOCH`, `PROP` |
| `A-05-state-machine.md` | Consensus state machine: views, states, message acceptance, PRE-PREPARE / PREPARE / COMMIT / ROUND-CHANGE, justification, extra seals, decision, new round and new sequence | `SM` |
| `A-06-timers.md` | Round-change timeout, retransmission, future-proposal wait, block period | `TIMER` |
| `A-07-network.md` | `istanbul/100` sub-protocol, framing, peer targeting, deduplication, relay, disconnection | `NET` |
| `A-08-header-rules.md` | Consensus fields of the header: proposal construction, proposal verification, finalized-block verification, light verification | `HDR` |
| `A-09-application-interface.md` | What the consensus protocol requires from the execution layer (abstract interface to Part B) | `APP` |
| `A-10-security.md` | Security considerations | `SEC` |
| `A-11-conformance-vectors.md` | Conformance classes and the test-vector catalog | `VEC` |
| `A-12-version-notes.md` | Differences between the reference commit and `v1.1.0` | — |
| `A-13-appendix.md` | Error catalog, log catalog (informative), lineage from QBFT (informative) | — |

### Part B — StableNet block validity

| File | Title | Area codes |
|---|---|---|
| `B-00-overview.md` | Overview and relation to Part A | — |
| `B-01-chain-config-forks.md` | `ChainConfig`, Anzeon configuration, WBFT transitions, fork schedule, network presets | `CFG` |
| `B-02-genesis.md` | Anzeon genesis: header, extra data, system-contract injection, governance initialisation | `GEN` |
| `B-03-header-body.md` | Execution-side header and body validity (gas limit, base fee, forbidden forks, uncles, withdrawals) | `BHDR`, `BODY` |
| `B-04-system-contracts.md` | System contract addresses, versions, storage layouts read by the node, upgrade mechanism | `SYS` |
| `B-05-governance.md` | Governance semantics: GovBase (members, proposals, votes, quorum), GovValidator, GovCouncil, and the parts of the minter contracts that affect node behaviour | `GOV` |
| `B-06-finalization.md` | Block finalization: upgrades, base-fee distribution, epoch info write/verify, gas-tip verification, state root | `FIN` |
| `B-07-transaction-rules.md` | Anzeon transaction rules: account extra bits (blacklist, authorized), gas-tip enforcement, fee delegation, block-building order (informative) | `TX` |
| `B-08-candidate-source.md` | Binding of Part A's application interface to contract state (candidates, BLS keys, proposer eligibility, gas tip) | `SRC` |
| `B-09-sync-rpc.md` | Synchronisation and head selection; RPC surface relevant to consensus observation | `SYNC`, `RPC` |

---

## 2. Conventions

### 2.1 Requirement language

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, RECOMMENDED, MAY and OPTIONAL are to be interpreted as described in RFC 2119 and RFC 8174 when, and only when, they appear in all capitals.

### 2.2 What is normative

Only behaviour that another party can observe is normative:

- the bytes on the wire (message encodings, signing payloads, framing),
- block and header validity (what a conforming node accepts or rejects),
- the conditions under which a node sends a message, and the content and timing of that message,
- values committed to the chain (seals, epoch info, randao mix, balances changed by finalization).

Implementation structure (goroutines, locks, caches, channel sizes, log text) is described in sections marked **Implementation note (informative)**. A conforming implementation MAY differ in those sections as long as the normative behaviour is preserved.

### 2.3 Requirement identifiers

Each normative statement is a numbered requirement:

```
[WBFT-SM-012] A node in state Preprepared MUST ignore a PRE-PREPARE for its current view.
Source: consensus/wbft/core/backlog.go:125-190 (checkMessage)
```

- Part A identifiers: `WBFT-<AREA>-<NNN>`. Part B identifiers: `SNET-<AREA>-<NNN>`. Area codes are listed in the document map.
- Numbers are three digits, unique within an area, and never reused. When a requirement is withdrawn, it stays in place marked `(withdrawn)`.
- A requirement whose statement only describes reference behaviour, and states no property that another party can check, is kept in place as informative text and marked `(withdrawn; informative)` at the start of its text. It keeps its `Source:` lines, has no `Observable:` line, and is not a conformance requirement (`A-11` WBFT-VEC-001). The normative behaviour it describes is stated by the requirements it cites.
- A requirement states one testable property. If a sentence contains two MUSTs that can fail independently, split it.
- `Source:` lines are informative and cite `path:line` in go-stablenet at the reference commit. Every requirement has at least one `Source:` line.
- Requirements that the inspector can check from outside a node are additionally tagged `Observable: header | network | log | rpc | state` (one or more, separated by commas, with no other text on the line; details of what is observed go on a following `Observed as:` line).

### 2.4 Pseudocode

Algorithms are given as Python-like pseudocode, in the style of the Ethereum consensus specifications:

- functions are pure unless they are named `on_*` (event handlers) or documented as mutating the node state,
- integer arithmetic is unbounded unless a type is given (`uint32`, `uint64`, `uint256`); where the reference implementation uses floating point or truncating conversions, the pseudocode reproduces that behaviour exactly and says so,
- `bytes32`, `Address` (20 bytes), `Hash` (32 bytes) and the other types are defined in `A-01`,
- `rlp_encode` / `rlp_decode` follow `A-03` (go-ethereum RLP rules, including the `nil` conventions).

### 2.5 Other conventions

- Byte strings are written in hexadecimal with `0x` prefix. Bit positions are counted from the least significant bit.
- Heights are called *sequence* in message contexts and *number* in header contexts; they are the same value.
- "The reference implementation" means go-stablenet at the reference commit.
- Terms defined in `A-01` §2 (Glossary) are used with that meaning everywhere.

---

## 3. Relation to other documents

- `../spec-ko/`: Korean edition (informative). It mirrors this directory file by file, section by section and requirement by requirement, and adds explanatory "해설" paragraphs. If the Korean edition and this specification disagree, this specification is correct. Writing rules: `../spec-ko/STYLE.md`.
