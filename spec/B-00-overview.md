# B-00 Overview of Part B: StableNet block validity

- Status: draft
- Reference implementation: go-stablenet `740526d03` (see `README.md`)

Part A specifies the WBFT consensus protocol: how validators agree on one block per height, which consensus fields of the header carry the agreement, and how an outside party verifies those fields. Part A deliberately treats the block body and the execution state as opaque. It only requires, through the abstract application interface in `A-09-application-interface.md`, that the execution layer can build a proposal, judge a proposal, finalize a block and report the candidate set.

Part B fills that gap for one concrete execution layer, StableNet (the Anzeon rule set of go-stablenet). A node that implements Part A and Part B together produces and accepts exactly the blocks that go-stablenet produces and accepts, and arrives at the same state root after every block. Part B is therefore the part that makes a WBFT implementation interoperable with a go-stablenet network, not merely consistent with itself.

---

## 1. What Part B binds

The table lists each abstract operation of `A-09` and the Part B chapters that give it a concrete meaning for StableNet.

| `A-09` operation | Meaning in StableNet | Bound in |
|---|---|---|
| `ready_to_build`, `prepare_consensus_fields`, `submit_proposal` (building, `A-09` §4.4) | The application fills the execution-side header fields (gas limit, base fee, forbidden fork fields absent), lets consensus fill the consensus fields including `GasTip`, executes transactions, then finalizes with `FinalizeAndAssemble` (upgrades, base-fee distribution, `EpochInfo`, gas-tip check, state root) | `B-03` §3, `B-06` §2-§6, `B-07` (transaction selection, informative) |
| `compute_epoch_info(header, post_state)` | Next-epoch information computed on the post-execution state of the epoch block and written (proposer) or compared (importer) | `B-06` §4 (algorithm in `A-04`) |
| `candidates(epoch_header, post_state)` | Candidate addresses and BLS keys read from the `GovValidator` in force at the epoch block | `B-08`, `B-04`, `B-01` §7 |
| `is_eligible_proposer(parent, addr)` | Blacklist bit of the proposer account at the parent state | `B-07`, `B-08` |
| `gas_tip(parent)` | `GovValidator` gas-tip slot at the parent state, genesis contract address | `B-06` §5, `B-08` |
| `validate_proposal(block)` | Header-only check before voting (`A-08` P1-P7): transaction root, uncle hash and the header rules of `B-03` §1-§2; transactions are not executed | `B-03` §7, `B-06` §6.1 |
| `finalize(block, prepared, committed, round)` and the commit paths (`A-09` §5) | Seal writing is Part A. On the non-proposer path the sealed block is imported through body validation, execution, finalization and state validation; on the proposer path the build-time state is stored | `B-03` §4-§7, `B-06` §6.1 |

Configuration is shared: the chain configuration of `B-01` produces both the Part A consensus `Config` (block period, epoch length, proposer policy, timeouts, transitions) and the Part B fork schedule and system-contract upgrade list. The genesis block of `B-02` is the anchor of both parts: its `WBFTExtra.EpochInfo` gives the validator set of the first epoch (Part A), and its allocation gives the initial contract state (Part B).

---

## 2. Chapters

| File | Title | Area codes | Scope |
|---|---|---|---|
| `B-00-overview.md` | Overview and relation to Part A | — | This chapter |
| `B-01-chain-config-forks.md` | Chain configuration and forks | `CFG` | `ChainConfig` fields that matter for StableNet, Ethereum forks, `ApplepieBlock`, `BohoBlock`, `AnzeonConfig`, WBFT transitions, upgrade collection, startup validity and compatibility rules, network presets 8282 and 8283 |
| `B-02-genesis.md` | Anzeon genesis | `GEN` | Genesis header fields, `WBFTExtra` of the genesis block, system-contract injection, `GovCouncil` initialisation, consistency between genesis extra and genesis storage, genesis hash, worked examples |
| `B-03-header-body.md` | Execution-side header, body and state validity | `BHDR`, `BODY` | Gas limit bounds, base fee (Anzeon variant of EIP-1559), `GasUsed`, forbidden Shanghai/Cancun fields, uncle and withdrawal rules, body validation, post-execution state validation, global order of checks with `A-08` |
| `B-04-system-contracts.md` | System contracts | `SYS` | Addresses, versions, storage layouts read by the node, upgrade mechanism |
| `B-05-governance.md` | Governance semantics | `GOV` | GovBase, GovValidator, GovCouncil, minter contracts as far as they affect node behaviour |
| `B-06-finalization.md` | Block finalization | `FIN` | Execution-side finalization (`processFinalize`, not the `A-09` `finalize` operation): system-contract upgrades, base-fee distribution, `EpochInfo` write/verify hook, gas-tip verification, state root, proposer path versus import path |
| `B-07-transaction-rules.md` | Anzeon transaction rules | `TX` | Account extra bits, gas-tip enforcement, fee delegation, blacklist in execution |
| `B-08-candidate-source.md` | Candidate source binding | `SRC` | Binding of `A-09` operations to contract state |
| `B-09-sync-rpc.md` | Synchronisation and RPC | `SYNC`, `RPC` | Head selection, sync-time exceptions, `eth`/`snap` wire values and deviations (network ID, fork ID, total difficulty, response checks), RPC relevant to consensus observation |

---

## 3. Reading order

A reader who implements a StableNet-compatible node reads `B-01` (to build the configuration), `B-02` (to build the identical genesis block), then `B-03` and `B-06` together with `A-08` (to accept and produce blocks), and finally `B-07` for the transaction semantics that determine receipts and the state root. `B-04`, `B-05` and `B-08` are needed as soon as the node reads contract storage (candidates, gas tip, blacklist).

## 4. Conventions specific to Part B

- Requirement identifiers are `SNET-<AREA>-<NNN>` (`README.md` §2.3).
- "Anzeon chain" means a chain whose `ChainConfig.Anzeon` is present (`B-01` SNET-CFG-004). Part B applies only to Anzeon chains.
- "Import path" means block insertion through `BlockChain.InsertChain` (header verification, body validation, execution, `Finalize`, state validation). "Proposer path" means block creation by the local miner through `FinalizeAndAssemble` and the write of the sealed block without re-execution. The two paths are specified separately where they differ (`B-06` §6).
- Values in wei are written as decimal integers with `_` separators, for example `20_000_000_000_000`.
