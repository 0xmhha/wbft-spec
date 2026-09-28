# A-12. Version notes

This specification is written against go-stablenet commit `740526d03` (branch `dev`). The network release in operation at the time of writing is `v1.1.0` (`71e3f820f`, 2026-07-09, "Boho hardfork"). This chapter records every difference between the two that matters to a conforming implementation.

---

## 1. Method

The difference was computed with `git diff v1.1.0 740526d03` over the whole tree (41 files changed). Commit ancestry is not a reliable guide. `v1.1.0` is not an ancestor of `740526d03`: the two share the merge base `0bf2f4d1b`, from which `v1.1.0` has 2 commits (`9d5460ecf`, #80, and the release commit `71e3f820f`, #109, which carries the content of the `dev` fixes) and `740526d03` has 47 (`git describe`: `v1.0.0-72-g740526d03`). The tree diff covers both sides; every difference it shows is a change on the `740526d03` side, and the content of the two `v1.1.0`-only commits is present at `740526d03`. consensus fixes #82, #84, #85, #89 and #91 appear in `v1.1.0..740526d03` because they were merged into `dev` separately, but their content is already present in `v1.1.0` (the tree diff of `consensus/wbft/core` and `consensus/wbft/backend` is empty; under `consensus/` only `consensus/wbft/engine/engine.go` and its test differ). The same holds for PR #86 (`d7cff3df9`, `istanbul_status` range limit, `B-09`). Only content differences are listed below.

---

## 2. Consensus and block-validity differences

### 2.1 Nil gas tip in the header extra (textual difference only)

| | `v1.1.0` | `740526d03` |
|---|---|---|
| `verifyGasTip` condition | `extra.GasTip != nil && extra.GasTip != expected` → reject | `extra.GasTip == nil || extra.GasTip != expected` → reject |

Source: `consensus/wbft/engine/engine.go:1291` (commit `b46ef9ef3`, #113).

The changed branch is unreachable. `extra.GasTip` comes from decoding the header extra, and a decoded `*big.Int` is never `nil`: the go-ethereum RLP decoder selects the big-integer decoder before the pointer rule that honours `rlp:"nil"` (`rlp/decode.go:161-162`), so an empty `gas_tip` item (`0x80`) decodes to 0, and a list item (`0xc0`) makes the extra undecodable (`A-03-encoding.md`, WBFT-ENC-008). Both versions therefore accept exactly the same blocks: a block whose gas tip is empty is rejected by both unless the governance gas tip is 0. The reference commit and `v1.1.0` have no observable consensus or block-validity difference.

---

## 3. Differences that do not change block validity

These change local behaviour only. They are listed so that implementers know which version's behaviour the informative sections of this specification describe.

| Area | Change | Commit | Effect |
|---|---|---|---|
| Transaction pool | Cumulative affordability check restored, with fee-delegation accounting (sender pays value, fee payer pays gas) | `54a5cbd59` (#116) | Which transactions a node admits to its pool. Local policy (see `B-07-transaction-rules.md`, informative part) |
| RPC transaction construction | `FeeDelegateDynamicFeeTx.SetSenderTx` now allocates the access list before copying; previously the copied access list was always empty | `f4c9490f7` (#115) | Only transactions assembled by the node's own RPC (`internal/ethapi/api.go:673`, `:2238`, `transaction_args.go:557`). Received and decoded transactions are not affected |
| Wallet / transaction API | Fee-delegated transaction signing fixes: a signing request that names `feePayer` is now rejected unless it assembles a type-`0x16` transaction (`fee delegate tx type mismatch`) | `4444af086` (#114) | RPC and wallet behaviour. In `v1.1.0`, `personal_signTransaction` and `eth_signTransaction` with `feePayer` but without the sender's `v`, `r`, `s` sign the assembled non-`0x16` transaction (type `0x02` when `maxFeePerGas` is given) with the fee payer's key, so the fee payer becomes the sender of a transaction it did not intend (`internal/ethapi/api.go:449-471, 2080-2086, 2428-2435`) |
| Blockchain internals | Transaction-lookup cache locking during reorg | `650722d5a` (#127) | Race fix; no protocol effect |
| Chain repair, snap sync | `SetHead` repair path no longer calls `updateFn`; the snap-sync storage-heal check honours the trie scheme | `d607a8f65` (#117), `d651ce405` (#121) | Local recovery and synchronisation behaviour; no protocol effect |
| Simulated backend | `WBFTBackend.Close()` closes the node stack | `05a129e9e` (#120) | Test harness only |
| Tracers, RPC, logging, trie database, command-line tools, simulated backend | Upstream go-ethereum backports | #104, #105, #106, #107, #111, #112, #118, #119, #122, #123, #124, #126, #128, #129 | No protocol effect |

---

## 4. Maintenance rule

When the reference commit changes, this chapter MUST be updated with the new commit, the method above re-run, and every requirement whose `Source:` lines point into changed hunks re-verified.
