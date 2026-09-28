# B-09 Synchronisation, head selection and RPC

- Part: B (StableNet block validity)
- Area codes: `SYNC`, `RPC`
- Status: draft
- Reference: go-stablenet `740526d03`

This chapter specifies how a StableNet node catches up with the chain, how it chooses its head, what the `finalized` and `safe` tags mean, and the RPC surface that tools use to observe WBFT consensus. The first half (`SYNC`) matters for interoperability because a node that syncs differently can accept a different chain or report a different head. The second half (`RPC`) matters because the inspector and operators read consensus data through it; where the reference implementation's RPC output is irregular, the irregularity is specified as-is so that tools written against go-stablenet keep working.

Block validity itself is in `A-08` (consensus header fields), `B-03` (execution header and body) and `B-07` (transactions). The consensus core's reaction to a new head (`NewChainHead`, `FinalCommittedEvent`) is in `A-05`. The `istanbul/100` sub-protocol is in `A-07`; the values a node uses in the `eth/68` and `snap/1` protocols, and the places where the reference implementation is stricter than go-ethereum v1.13.15, are in §12.

---

## 1. Scope and conformance

Normative in this chapter:

- which blocks a node imports during synchronisation and which checks it applies (`SYNC`, observable through the chain the node serves),
- head selection and the `latest`/`finalized`/`safe` tags (`SYNC`, `RPC`),
- request parameters, response schemas, limits and errors of the `istanbul` namespace and of the Anzeon-specific parts of `eth`, `personal`, `miner`, `debug` and `admin` (`RPC`),
- the `eth/68` `Status` values, the response limits of `eth/68` and `snap/1`, and the transaction-propagation limits (`SYNC`, §12).

Informative: peer selection, downloader scheduling, metrics names, bootstrap nodes (§12.4), node defaults (§13), and the Engine API behaviour (specified because it exists and is dangerous, §11; a conforming WBFT node is not required to expose it).

JSON conventions: go-ethereum JSON-RPC 2.0. `QUANTITY` is a `0x`-prefixed hex integer without leading zeros; `DATA` is `0x`-prefixed hex bytes; addresses are 20-byte `DATA` in lower case (Go `common.Address` text encoding). A `BlockNumber` parameter accepts a `QUANTITY` or one of the tags `"earliest"` (0), `"latest"` (-2), `"pending"` (-1), `"finalized"` (-3), `"safe"` (-4).

Source: rpc/types.go:63-71

---

## 2. Synchronisation modes

[SNET-SYNC-001] The default synchronisation mode MUST be full sync (every block is executed from genesis).
Source: eth/ethconfig/config.go:55-60
Observable: rpc

A node configured for full sync switches to snap sync at start-up when its head state is missing or an earlier snap sync did not finish; a node configured for snap sync switches to full sync when its database already has a head with state.

Source: eth/handler.go:180-206

[SNET-SYNC-002] In snap sync, headers MUST be verified with the full WBFT header verification (`A-08`), including every prepared and committed seal of every header; the node MUST NOT sample seals.
Source: core/headerchain.go:306-340, core/blockchain.go:2455-2462, consensus/wbft/backend/engine.go:87-112
Observable: rpc

[SNET-SYNC-003] In full sync the node MUST verify each header with the full WBFT header verification and then execute the block against its parent state and check `GasUsed`, `Bloom`, `ReceiptHash` and `Root` (`B-03`).
Source: core/blockchain.go:1563-1592, core/blockchain.go:1779-1792, core/block_validator.go:124-145
Observable: rpc

Snap-synced receipts do not carry `effectiveGasPrice`; it is derived later from the header gas tip and the `AuthorizedTxExecuted` log (`B-07 §8.2`). Snap-synced state carries the account `Extra` bits, because the slim account encoding has the same optional field.

Source: core/types/receipt.go:396-411, core/types/state_account.go:73

---

## 3. State-dependent checks during synchronisation

Two parts of WBFT header verification need the parent's post-state: the `Coinbase` blacklist check (`A-08` step H15b, `B-08` SNET-SRC-020, `verifySigner`) and the gas-tip check (`A-08` step H21, `B-06`, `verifyGasTip`). `A-08` §6.6 calls them the state-dependent steps. When that state is not available the reference implementation skips them.

```python
def verify_header_state_parts(header, parent):
    state = state_at(parent.root)            # may be unavailable
    if state is None:
        pass                                  # blacklist check skipped
    elif is_blacklisted(state, header.coinbase):
        raise ErrBlacklistedSigner

    try:
        want = gov_validator_gas_tip(state_at(get_header(parent.hash).root))
    except Exception:                         # any error other than a mismatch (A-08 WBFT-HDR-111):
        return                                # unavailable state, missing parent, zero root, ... -> skipped
    if header.extra.gas_tip != want:
        raise GasTipMismatchError
```

Source: consensus/wbft/engine/engine.go:290-298, consensus/wbft/engine/engine.go:329-344, consensus/wbft/engine/engine.go:352-380, consensus/wbft/engine/engine.go:622-645

[SNET-SYNC-010] During header verification, if the parent's state is unavailable, a node MUST skip the proposer blacklist check and the gas-tip check instead of rejecting the header.
Source: consensus/wbft/engine/engine.go:290-298, consensus/wbft/engine/engine.go:329-344
Observable: rpc

The seal checks, randao checks and the derivation of the validator set from earlier `EpochInfo` headers do not need state (they read headers only) and are always applied (`A-08`). The content of a new `EpochInfo` is checked only when the block is executed (`B-06` SNET-FIN-015), so snap sync never checks it.

---

## 4. When a node starts a sync

The chain syncer compares its total difficulty (TD) with the best peer's TD. Because every WBFT block has difficulty 1 (`A-08`), `TD(h) = TD(genesis) + h`, so TD comparisons are height comparisons.

[SNET-SYNC-020] On an Anzeon chain, a node MUST NOT start a downloader sync while the best peer's TD is at most `local_td + 1`, unless the force flag is set; with the force flag it MUST start a sync when the peer's TD exceeds `local_td`. The force flag is set when the local head TD is equal at two consecutive `TdSyncInterval` ticks (default 10 s, so a stall is detected after 10 to 20 s) and is cleared by the first sync evaluation that reaches the TD comparison (an evaluation that stops earlier for lack of peers leaves it set).
Source: eth/sync.go:161-217, eth/sync.go:135-145, eth/ethconfig/config.go:62
Observable: network

A node one block behind is expected to receive that block through the block fetcher (propagation, `§6`) or, for validators, through consensus (`A-05`). The downloader takes over at two blocks, or at one block after a stall of 10 to 20 seconds.

[SNET-SYNC-021] A node MUST NOT start a downloader sync with fewer than `min(5, maxPeers)` peers (`defaultMinSyncPeers`, capped by the configured peer limit) unless the force timer (`ForceSyncCycle`, default 10 s) has fired, in which case one peer suffices.
Source: eth/sync.go:176-185, eth/ethconfig/config.go:61
Observable: network

At the end of a successful downloader sync the node marks itself synced (§6) and announces its head block (hash only, `BroadcastBlock(block, false)`) to peers.

Source: eth/sync.go:264-284

---

## 5. Consensus and block building during synchronisation

The downloader publishes `StartEvent` at the beginning of a sync and `DoneEvent` or `FailedEvent` at the end. The miner reacts to these events only until the first `DoneEvent`.

```python
def miner_update(ev):                         # runs until the first DoneEvent
    if ev is StartEvent:
        can_start = False
        if mining: worker.stop(); should_start = True      # stops the WBFT core (Backend.Stop)
        worker.syncing = True
    elif ev in (FailedEvent, DoneEvent):
        can_start = True
        if should_start: worker.start()                    # restarts the WBFT core (Backend.Start)
        worker.syncing = False
        if ev is DoneEvent: unsubscribe()                  # no further reaction to downloader events
```

Source: miner/miner.go:113-178, miner/worker.go:444-458

[SNET-SYNC-030] A validator MUST stop its WBFT consensus core when a downloader sync starts and restart it when that sync ends, until the first successful downloader sync of the process has completed. After the first successful sync, later downloader syncs MUST NOT stop the consensus core.
Source: miner/miner.go:113-178, miner/worker.go:444-458
Observable: network

After the first successful sync, a validator that falls behind keeps its consensus core running while the downloader imports blocks; the core follows each new head through `NewChainHead` (`A-05`) and can send ROUND-CHANGE messages for heights that the rest of the network has already decided. Failed syncs do not end the pause behaviour; only a `DoneEvent` does.

[SNET-SYNC-031] While `worker.syncing` is true, the node MUST NOT assemble proposals or pending blocks in its sealing loop (`commitWork`). The Engine API payload builder (`generateWork`, reached through `getSealingBlock`) does not check this flag (§11).
Source: miner/worker.go:1320-1324
Source: miner/worker.go:1288, miner/worker.go:1430, miner/payload_building.go:194, miner/payload_building.go:230
Observable: rpc

---

## 6. Block fetcher path

Propagated blocks (`NewBlock` announcements and full blocks from `eth` peers) and blocks committed by the local consensus core on a non-proposer (`Backend.Commit` → `Enqueue("istanbul", block)`) enter the chain through the block fetcher. The fetcher verifies the header, re-propagates it, and calls the inserter.

Source: consensus/wbft/backend/backend.go:238-247, eth/handler_istanbul.go:38-40, eth/handler.go:229-307

[SNET-SYNC-040] Until the node is marked synced, the fetcher inserter MUST discard every block it receives (propagated or locally committed) without importing it and without error.
Source: eth/handler.go:265-273
Observable: rpc

The node is marked synced by (a) the end of a successful downloader sync, (b) `StartMining` (validators), or (c) an Engine API `forkchoiceUpdated` (§11).

Source: eth/handler.go:724-737, eth/sync.go:264-270, eth/backend.go:465-468, eth/catalyst/api.go:338

A non-validator node therefore ignores all propagated blocks until its first downloader sync completes. A validator avoids this because `StartMining` marks it synced; otherwise it would discard the blocks its own consensus core commits.

[SNET-SYNC-041] The fetcher MUST drop an announced or delivered block whose number is more than 7 below or more than 32 above the local head.
Source: eth/fetcher/block_fetcher.go:43-44, eth/fetcher/block_fetcher.go:369, eth/fetcher/block_fetcher.go:399
Source: eth/fetcher/block_fetcher.go:783-788 (enqueue: delivered blocks, including local consensus commits)
Observable: network

---

## 7. Head selection and finality

### 7.1 Fork choice

Every inserted block goes through the upstream total-difficulty fork choice.

```python
def reorg_needed(current, extern) -> bool:
    local_td, extern_td = td(current), td(extern)
    if TTD is not None and TTD <= extern_td: return True     # never on WBFT (no TTD)
    if extern_td > local_td: return True
    if extern_td < local_td: return False
    if extern.number < current.number: return True
    if extern.number == current.number:
        cur_p, ext_p = is_local(current), is_local(extern)     # header.Coinbase is etherbase or in txpool.locals
        return (not cur_p) and (ext_p or random() < 0.5)
    return False
```

Source: core/forkchoice.go:77-110, eth/backend.go:376-423

[SNET-SYNC-050] A node MUST make a block the head when its TD is greater than the current head's TD. With difficulty 1 this is "the block is higher than the head".
Source: core/forkchoice.go:92-97
Observable: rpc

[SNET-SYNC-051] For two blocks at the same height (reachable only if two different blocks of the same height carry valid commit quorums, i.e. the `f < n/3` assumption failed), the reference implementation keeps the current head if its coinbase is local (the etherbase or an address in `txpool.locals`, `eth/backend.go:376-397`), adopts the new block if the new block's coinbase is local, and otherwise chooses at random with probability 1/2. A conforming node MAY use any tie-break in this case; it MUST report such an event (two committed blocks at one height) as a safety violation.
Source: core/forkchoice.go:98-110
Observable: rpc

WBFT has no fork-choice rule of its own: a block with a valid commit quorum is final at insertion (`A-05`). The TD rule only ever sees one candidate per height while the safety assumption holds.

### 7.2 Future blocks

[SNET-SYNC-052] A header whose time exceeds the local clock plus `AllowedFutureBlockTime` (default 0 s) MUST be rejected by header verification with `ErrFutureBlock`; a block rejected this way within 30 s of the local clock is queued and retried, not marked bad.
Source: consensus/wbft/engine/engine.go:201-205, core/blockchain.go:105, core/blockchain.go:1501-1520, consensus/wbft/config.go:121
Observable: rpc

### 7.3 `finalized` and `safe` tags

WBFT finality is immediate, so the natural meaning of `finalized` and `safe` is "the head". The reference implementation updates them only on some insertion paths.

| Path | Updates `finalized` / `safe` |
|---|---|
| Fetcher inserter (propagated blocks; non-proposer local commit) | yes, to the last block of the inserted batch, after a successful `InsertChain` |
| Proposer (`WriteBlockAndSetHead` from the miner result loop) | no |
| Downloader (`InsertChain`, `InsertReceiptChain`, `InsertHeaderChain`) | no |
| Engine API `forkchoiceUpdated` | yes, to the given hashes (§11) |
| Restart | `finalized` restored from the database; `safe` set equal to it |
| `SetHead` below them | reset to none |

Source: eth/handler.go:297-305, core/blockchain.go:539-549, core/blockchain.go:620-640, core/blockchain.go:810-818, eth/catalyst/api.go:340-371

[SNET-RPC-001] The `finalized` and `safe` block tags MUST resolve to the block last recorded by the paths in the table above; if none has been recorded, a request for them MUST fail with the error `finalized block not found` / `safe block not found`.
Source: eth/api_backend.go:80-93, eth/api_backend.go:132-145
Observable: rpc

Consequences: right after the local node proposed a block, and throughout a downloader catch-up, `finalized` lags the head. A tool that treats `finalized < latest` as a finality delay misreads a WBFT node. The metric `chain/head/finalized` has the same lag.

### 7.4 Weak subjectivity

[SNET-SYNC-060] A node MUST obtain the validator set of any height by following epoch info from genesis through the header chain (`A-04`); the reference implementation has no checkpoint, trusted-header or weak-subjectivity sync. `RequiredBlocks` (`--eth.requiredblocks`) only drops peers whose block at a given number has a different hash; it does not change what is verified.
Source: consensus/wbft/backend/engine.go:420-449, eth/handler.go:103, eth/handler.go:445-470 (RequiredBlocks)
Observable: rpc

The trust anchor is the genesis block (validator set, BLS keys, configuration). A new node that syncs after the whole validator set has changed can be fed a different chain signed by keys that were validators long ago (long-range attack); only the genesis anchor and the peers it chooses protect it.

---

## 8. `istanbul` namespace

The WBFT backend registers the `istanbul` namespace, version `1.0`, public (available on HTTP/WS/IPC if the namespace is enabled by `--http.api`/`--ws.api`).

Source: consensus/wbft/backend/engine.go:232-239

[SNET-RPC-041] The `istanbul` namespace MUST contain exactly the eight methods of §8.1-§8.6: `nodeAddress`, `getCommitSignersFromBlock`, `getCommitSignersFromBlockByHash`, `getValidators`, `getValidatorsAtHash`, `isValidator`, `status` and `getWbftExtraInfo`. The Quorum methods `getSignersFromBlock`, `getSignersFromBlockByHash`, `getSnapshot`, `getSnapshotAtHash`, `candidates`, `propose` and `discard` MUST NOT exist (a call fails with JSON-RPC "method not found"); validators are not voted in headers (`A-04`). `status` has the same name as Quorum's method but a different result (SNET-RPC-016).
Source: consensus/wbft/backend/api.go:79-162, consensus/wbft/backend/api.go:164-207, consensus/wbft/backend/api.go:347-363, consensus/wbft/backend/api.go:416-452, consensus/wbft/backend/engine.go:232-239
Observable: rpc

The `istanbul` module of the reference JavaScript console still lists the Quorum methods above and does not list `getCommitSignersFromBlock` or `getCommitSignersFromBlockByHash`; calling a listed Quorum method from the console fails with "method not found".

Source: internal/web3ext/web3ext.go:36, internal/web3ext/web3ext.go:930-1012

Common rules:

- An optional `BlockNumber` parameter that is omitted or `"latest"` means the current head header.
- For `getCommitSignersFromBlock`, `getValidators` and `getWbftExtraInfo`, any other negative tag (`"pending"`, `"finalized"`, `"safe"`) is converted to `uint64` without a check and therefore never matches a block.
- Errors are returned as JSON-RPC errors with code `-32000` and the message shown.

### 8.1 `istanbul_nodeAddress`

| | |
|---|---|
| Params | none |
| Result | `DATA` (20 bytes): the local validator (signer) address |

Source: consensus/wbft/backend/api.go:79-82

[SNET-RPC-010] `istanbul_nodeAddress` MUST return the address the node uses to sign consensus messages and seals.
Source: consensus/wbft/backend/api.go:79-82
Observable: rpc

### 8.2 `istanbul_getCommitSignersFromBlock`, `istanbul_getCommitSignersFromBlockByHash`

| | |
|---|---|
| Params | `[number?: BlockNumber]` / `[hash: DATA(32)]` |
| Result | object `BlockSigners` |
| Errors | `unknown block`; `zero seals` (block without committed seal, e.g. genesis); `validator address is zero` |

`BlockSigners` has no JSON tags, so its keys are the Go field names:

```json
{
  "Number": 1234,                                  // JSON number (decimal), not QUANTITY
  "Hash": "0x…",                                   // 32-byte DATA
  "Author": "0x…",                                 // header.Coinbase
  "Committers": ["0x…", "0x…"]                     // addresses whose bit is set in CommittedSeal.Sealers,
}                                                  // in ascending validator-index order
```

Source: consensus/wbft/backend/api.go:42-48, consensus/wbft/backend/api.go:84-129, consensus/wbft/engine/engine.go:1131-1141, consensus/wbft/engine/engine.go:1322-1336

[SNET-RPC-011] `Committers` MUST list, for each sealer index `i` set in the block's committed-seal bitmap in ascending order, the candidate address of `EpochInfo.Validators[i]` from the epoch info that governs that block (`A-04`). The bitmap is read from the serving node's own copy of the header; `CommittedSeal` is node-local (`A-08` WBFT-HDR-053), so two conforming nodes MAY return different `Committers` for the same block.
Source: consensus/wbft/engine/engine.go:1131-1141, consensus/wbft/engine/engine.go:1322-1336
Observable: rpc

[SNET-RPC-012] `Author` MUST be `header.Coinbase` (the reference `Author` does not recover a signature).
Source: consensus/wbft/engine/engine.go:86-88
Observable: rpc

### 8.3 `istanbul_getValidators`, `istanbul_getValidatorsAtHash`

| | |
|---|---|
| Params | `[number?: BlockNumber]` / `[hash: DATA(32)]` |
| Result | array of `DATA(20)`: the validator set that seals the given block (`validators_at(number)`, `A-04`), in validator-index order |
| Errors | `unknown block`; errors from validator-set derivation |

Source: consensus/wbft/backend/api.go:131-162

[SNET-RPC-013] `istanbul_getValidators(n)` MUST return `validators_at(n)`, the set whose seals appear in block `n`, not the set that block `n` elects for the next epoch.
Source: consensus/wbft/backend/api.go:144-148
Observable: rpc

### 8.4 `istanbul_isValidator`

| | |
|---|---|
| Params | `[number?: BlockNumber]` |
| Result | `bool`: whether `istanbul_nodeAddress` is in `istanbul_getValidators(number)` |

[SNET-RPC-014] `istanbul_isValidator` MUST return `false` (not an error) when the validator set cannot be determined, including for unknown blocks.
Source: consensus/wbft/backend/api.go:347-363
Observable: rpc

### 8.5 `istanbul_status`

| | |
|---|---|
| Params | `[start?: BlockNumber, end?: BlockNumber]` |
| Result | object `Status` |

Range rules (hardened in go-stablenet PR #86, commit `d7cff3df9`; the commit is not an ancestor of the `v1.1.0` tag, but its content is in the `v1.1.0` release: `git diff v1.1.0 740526d03 -- consensus/wbft/backend` is empty, `A-12` §1):

```python
MAX_STATUS_BLOCK_RANGE = 1024

def status_range(start, end, head):
    if start is not None and end is None: raise "pass the end block number"
    if start is None and end is not None: raise "pass the start block number"
    if start is None:                               # both omitted: last 64 blocks
        end = head; start = end - 63 if end >= 63 else 0
    else:
        def resolve(n):
            if n >= 0: return n
            if n == LATEST: return head
            raise f"unsupported block number: {n}"  # pending, finalized, safe
        end, start = resolve(end), resolve(start)
        if start > end: raise "start block number should be less than end block number"
        if end > head:  raise "end block number should be less than or equal to current block height"
    n = end - start + 1
    if n > MAX_STATUS_BLOCK_RANGE: raise f"requested range too large: {n} blocks (max 1024)"
    return start, end, n
```

Source: consensus/wbft/backend/api.go:205-257

[SNET-RPC-015] `istanbul_status` MUST reject a range of more than 1024 blocks and MUST apply the range rules above.
Source: consensus/wbft/backend/api.go:205-257
Observable: rpc

Response:

```json
{
  "sealerActivity": {
    "total":         { "<addr>": <int>, … },   // prepared + committed + prevPrepared + prevCommitted
    "prepared":      { "<addr>": <int>, … },   // bits in PreparedSeal
    "committed":     { "<addr>": <int>, … },   // bits in CommittedSeal
    "prevPrepared":  { "<addr>": <int>, … },   // bits in PrevPreparedSeal, mapped with the previous block's set
    "prevCommitted": { "<addr>": <int>, … }
  },
  "author":     { "<addr>": <int>, … },        // count of header.Coinbase
  "blockRange": { "startBlock": <int>, "endBlock": <int>, "totalBlocks": <int> },
  "roundStats": { "roundDistribution": { "<round>": <int>, … } }  // key: decimal round of the block
}
```

All counts and range fields are JSON numbers; map keys are lower-case hex addresses or decimal round numbers. `prepared`, `committed` and `roundStats` come from the serving node's own copy of each header and are node-local (`A-08` WBFT-HDR-053): different nodes MAY return different values for the same range. `prevPrepared` and `prevCommitted` are covered by the block hash and are identical on every node.

Source: consensus/wbft/backend/api.go:50-77, consensus/wbft/backend/api.go:164-203

[SNET-RPC-016] For each block in the range, `istanbul_status` MUST count every sealer index set in `PreparedSeal` and `CommittedSeal` against the validator set of that block, and every index set in `PrevPreparedSeal` and `PrevCommittedSeal` against the validator set of the previous block, ignoring indices beyond the set size; every counted seal MUST also increment `total`.
Source: consensus/wbft/backend/api.go:309-333
Observable: rpc

[SNET-RPC-017] Every validator of the first block of the range and of each epoch transition inside the range MUST appear with count 0 in `prepared`, `committed`, `total`, `author`, `prevPrepared` and `prevCommitted` even if it signed nothing; validators of the previous set MUST appear with 0 in `prevPrepared`, `prevCommitted`, `total` and `author`.
Source: consensus/wbft/backend/api.go:272-298
Observable: rpc

[SNET-RPC-018] If any block in the range is missing, has an undecodable extra, or its validator sets cannot be derived, `istanbul_status` MUST fail as a whole (`block <n> not found`, `block <n>: failed to extract WBFT extra: …`, `block <n>: failed to get validators: …`) instead of returning partial counts.
Source: consensus/wbft/backend/api.go:261-277
Observable: rpc

The validator-set cache is refreshed only at the first block and after a block that carries `EpochInfo`. `roundStats` no longer has `totalRounds` (removed in PR #86).

### 8.6 `istanbul_getWbftExtraInfo`

| | |
|---|---|
| Params | `[number: BlockNumber]` (required) |
| Result | object with the decoded `WBFTExtra` of that block |
| Errors | `block is not a wbft block` (chain without Anzeon); `block <n> not found`; extra decode errors; validator-set errors |

```json
{
  "vanityData":        "0x…",            // DATA
  "randaoReveal":      "0x…",            // DATA (65-byte ECDSA signature, A-02)
  "prevRound":         "0x…",            // QUANTITY
  "prevPreparedSeal":  { "sealers": ["0x…"], "signature": "0x…" } | null,   // sealers mapped with the previous block's set
  "prevCommittedSeal": { … } | null,
  "round":             "0x…",            // QUANTITY; node-local (A-08 WBFT-HDR-053)
  "preparedSeal":      { … } | null,     // sealers mapped with this block's set; node-local
  "committedSeal":     { … } | null,
  "gasTip":            "27600000000000", // DECIMAL string (big.Int.String); never absent (A-03 WBFT-ENC-008)
  "epochInfo": null | {
     "candidates": [ { "addr": "0x…", "diligence": "0x…" }, … ],
     "validators": [ { "index": "0x…", "addr": "0x…", "bls": "0x…" }, … ]  // addr is 0x000…0 if index is out of range
  }
}
```

In `istanbul_getWbftExtraInfo`, every address (`sealers`, `epochInfo.candidates[].addr`, `epochInfo.validators[].addr`) is a checksummed string (Go `Address.Hex()`), unlike the lower-case addresses of the other methods in this namespace.

Source: consensus/wbft/backend/api.go:365-452, core/types/istanbul.go:189-194

[SNET-RPC-019] `istanbul_getWbftExtraInfo` MUST encode `gasTip` as a decimal string and the other numeric fields as hex strings, as in the schema above.
Source: consensus/wbft/backend/api.go:438-449
Observable: rpc

Because the parameter is a non-pointer `BlockNumber`, `"latest"` is accepted by the parser but becomes `uint64(-2)` and fails with `block -2 not found`.

---

## 9. Anzeon-specific `eth`, `personal`, `miner`, `debug` and `admin` behaviour

### 9.1 Fee suggestions

[SNET-RPC-030] On an Anzeon chain `eth_maxPriorityFeePerGas` MUST return the node's cached governance gas tip (the GovValidator value last read by the miner), not an oracle estimate; `eth_gasPrice` MUST return that tip plus the current head's base fee.
Source: eth/api_backend.go:365-372, internal/ethapi/api.go:70-89
Observable: rpc

The cache is refreshed after each imported block in a separate goroutine, so it can briefly lag or, during batch import, briefly regress.

Source: core/blockchain.go:1855-1866

`eth_feeHistory` rewards and the gas-price oracle use the Anzeon effective tip (header gas tip for non-authorized senders), judging authorization on the post-state of the block (`header.Root`), not on the pre-transaction state used by execution (`B-07` SNET-TX-010); the two differ when authorization changes inside the block.

Source: eth/gasprice/feehistory.go:115-120, eth/gasprice/gasprice.go:254

### 9.2 Transaction objects

Transaction objects (`eth_getTransactionByHash`, blocks with full transactions) add four fields for type `0x16`:

| Field | Type | Value |
|---|---|---|
| `feePayer` | `DATA(20)` | `tx.fee_payer` |
| `fv`, `fr`, `fs` | `QUANTITY` | fee-payer signature |

Source: internal/ethapi/api.go:1414-1419, internal/ethapi/api.go:1478-1498

For type `0x16`, `from`, `v`, `r`, `s` and `yParity` (`= v`) are the sender's signature values from `SenderTx`, and `chainId`, `accessList`, `maxFeePerGas`, `maxPriorityFeePerGas`, `nonce`, `gas`, `to`, `value` and `input` are the `SenderTx` fields. For type `0x04`, the object adds `authorizationList`, an array of `{chainId, address, nonce, yParity, r, s}` with every number as a `QUANTITY`.

Source: internal/ethapi/api.go:1424-1441, internal/ethapi/api.go:1479-1497, internal/ethapi/api.go:1515-1529, core/types/tx_fee_delegation.go:117-146, core/types/gen_authorization.go:17-33

[SNET-RPC-031] For a mined transaction of type `0x02`, `0x04` or `0x16`, the `gasPrice` field MUST be `min(header_gas_tip + base_fee, max_fee_per_gas)` when the block has a gas tip, and `min(max_priority_fee_per_gas + base_fee, max_fee_per_gas)` otherwise. For legacy and access-list transactions it MUST be the transaction's own gas price.
Source: internal/ethapi/api.go:1463-1476, internal/ethapi/api.go:1534-1550
Observable: rpc

This value ignores the sender's authorization, so for an authorized sender it differs from what was charged; the receipt's `effectiveGasPrice` (`B-07`, `SNET-TX-071`) is the charged price. For legacy transactions it also differs from the charged price (`B-07` example E-4).

Receipts (`eth_getTransactionReceipt`) have no fee-payer field.

Source: internal/ethapi/api.go:1870-1906

### 9.3 Fee-delegation signing methods

| Method | Params | Behaviour |
|---|---|---|
| `eth_signRawFeeDelegateTransaction` | `[args: TransactionArgs (feePayer required), input: DATA]` | without `args.feePayer` fails with `missing FeePayer`; `input` must decode as a type-`0x02` transaction, else `senderTx type error` (its `v, r, s` are copied into the `SenderTx` without verification); wraps it into type `0x16` with `args.feePayer` and signs as fee payer with an unlocked local key. Returns `{raw, tx}` |
| `personal_signRawFeeDelegateTransaction` | `[args, input, passwd]` | same, unlocking with the password |
| `eth_sendTransaction`, `eth_signTransaction`, `personal_signTransaction` with `feePayer` | usual | signs with the fee payer's wallet; rejects the request if the assembled transaction is not type `0x16` (`fee delegate tx type mismatch: got …, want 0x16`) |

Source: internal/ethapi/api.go:447-470, internal/ethapi/api.go:629-680, internal/ethapi/api.go:2204-2250, internal/ethapi/api.go:2430-2435

The sender's signed type-`0x02` input is itself a valid transaction (`B-07 §4.2`).

[SNET-RPC-042] `TransactionArgs` MUST accept the additional fields `feePayer` (`DATA(20)`), `v`, `r`, `s` (`QUANTITY`, the sender's signature of the type-`0x02` transaction) and `authorizationList`. After default filling, a node MUST assemble a type-`0x16` transaction from `TransactionArgs` only when the request assembles a type-`0x02` transaction (`maxFeePerGas` set, no `authorizationList`, no blob hashes) and `feePayer`, `v`, `r` and `s` are all present; with `authorizationList` it MUST assemble a type-`0x04` transaction. A signing request that names `feePayer` but assembles any other type MUST be rejected as in the table above.
Source: internal/ethapi/transaction_args.go:66-83, internal/ethapi/transaction_args.go:471-560, internal/ethapi/api.go:2428-2435
Observable: rpc

[SNET-RPC-043] In `eth_call`, `eth_estimateGas`, `eth_createAccessList` and `debug_traceCall`, a node MUST ignore `feePayer`: the simulated message has no fee payer, so the sender is charged and checked as for a non-delegated transaction (`B-07` SNET-TX-032), and the fee payer's balance and blacklist flag are not checked.
Source: internal/ethapi/transaction_args.go:381-466, internal/ethapi/api.go:1186, internal/ethapi/api.go:1271, internal/ethapi/api.go:1698, eth/tracers/api.go:955
Observable: rpc

### 9.4 Header fields

Block and header objects are upstream: `difficulty` is `0x1`, `mixHash` is the randao mix, `totalDifficulty` is `TD(genesis) + number`, and `extraData` is the raw RLP of `WBFTExtra` (`A-03`). The gas tip is only available decoded through `istanbul_getWbftExtraInfo`.

Source: internal/ethapi/api.go:1299-1330, internal/ethapi/api.go:1376

### 9.5 `miner_setGasPrice`

[SNET-RPC-032] On an Anzeon chain `miner_setGasPrice` MUST return `false` without changing the pool or miner tip.
Source: eth/api_miner.go:61-71
Observable: rpc

### 9.6 Account proofs

[SNET-RPC-044] `eth_getProof` MUST add to its result the field `extra`, the account's `Extra` value (`B-04` SNET-SYS-070) as a `QUANTITY`, and MUST omit the field when `Extra` is 0. `accountProof` is the proof of the StableNet account encoding, so a verifier rebuilds the leaf as `rlp([nonce, balance, storageHash, codeHash])` when `extra` is absent and as `rlp([nonce, balance, storageHash, codeHash, extra])` otherwise.
Source: internal/ethapi/api.go:730-740, internal/ethapi/api.go:826-837, core/types/state_account.go:28-37
Observable: rpc

### 9.7 Batch submission

[SNET-RPC-045] A node that serves `eth_sendRawTransactions` MUST accept one parameter, an array of `DATA`, and MUST return an array of 32-byte hashes of the same length (JSON `null` for an empty array). Each element MUST be decoded as the RLP network encoding of a transaction (a legacy transaction as an RLP list, a typed transaction as an RLP byte string wrapping `type ‖ payload`), not as the binary encoding used by `eth_sendRawTransaction`. For an element that fails to decode or is rejected by the pool, the corresponding result MUST be the zero hash; the call itself MUST NOT return an error for such elements. The array length has no limit.
Source: internal/ethapi/api.go:2183-2200, core/types/transaction.go:161-191
Observable: rpc

### 9.8 Additional methods

[SNET-RPC-046] `eth_getReceiptsByHash` MUST take one parameter, a block hash, and MUST return the same array as `eth_getBlockReceipts` for that block when the block is canonical, and `null` when the block is unknown or not canonical.
Source: internal/ethapi/api.go:993-1026
Observable: rpc

`admin_peerInfo(id)` returns the `admin_peers` entry of the peer with the given node ID, or `null` if that peer is not connected.

Source: node/api.go:318-325, p2p/server.go:1137-1152

[SNET-RPC-047] The state-override object accepted by `eth_call`, `eth_estimateGas`, `eth_createAccessList` and `debug_traceCall` MUST accept, per account, the field `extra` (`QUANTITY`, 64-bit) and MUST set the account's `Extra` to that value before execution, so that the blacklist and authorization rules of `B-07` apply to the overridden flags. The reference implementation does not mask the undefined bits 0-61 in this field.
Source: internal/ethapi/api.go:1028-1062
Observable: rpc

### 9.9 Tracing and access lists

[SNET-RPC-048] The built-in `prestateTracer` MUST report the field `extra` as a JSON number in decimal, not as a hex string, for every reported account whose `Extra` is non-zero. In diff mode the post state carries `extra` only for an account whose `Extra` changed to a non-zero value; a change to 0 is visible only in the pre state.
Source: eth/tracers/native/prestate.go:42-48, eth/tracers/native/prestate.go:290-294, eth/tracers/native/prestate.go:362-368, eth/tracers/native/gen_account_json.go:15-31
Observable: rpc

Every value with bit 62 or 63 set is at least 2^62, above the exact integer range (2^53) of IEEE-754 doubles. The flag values themselves (bits 62 and 63 only) are exactly representable, but a reader that parses JSON numbers as doubles loses any lower bits set together with them (possible through a state override, SNET-RPC-047) and cannot test the bits with 32-bit integer operators.

[SNET-RPC-049] `eth_createAccessList` MUST accept a state override (SNET-RPC-047) as its fourth parameter, and MUST exclude from the returned list the sender, the recipient (or created address), the active precompiles, the active native managers (`B-07` §9.5) and the authority of every authorization whose chain ID is 0 or the node's chain ID and whose signature recovers. It MUST fail with `insufficient gas to process all authorizations` when the number of authorizations exceeds `gas / CallNewAccountGas`.
Source: internal/ethapi/api.go:1603-1682
Observable: rpc

In the JavaScript tracer, `isPrecompiled(addr)` also returns `true` for the native manager addresses.

Source: eth/tracers/js/goja.go:491-507

### 9.10 `admin_nodeInfo`

[SNET-RPC-050] In `admin_nodeInfo`, `protocols.eth.config` MUST be the chain configuration without the `anzeon` member; `boho` and `transitions` are included. `protocols.istanbul.config` is the full chain configuration including `anzeon`, so a tool reads the WBFT configuration there.
Source: eth/protocols/eth/handler.go:108-116
Source: eth/handler_istanbul.go:104-106, eth/protocols/eth/qlight_deps.go:30-32 (the `istanbul` protocol returns the node information without the copy), p2p/server.go:1103-1110 (one entry per protocol name)
Observable: rpc

---

## 10. Metrics relevant to consensus (informative)

| Name | Kind | Meaning | Source |
|---|---|---|---|
| `consensus/wbft/core/round` | meter | marks the round increase when a new round starts | consensus/wbft/core/core.go:49, :219 |
| `consensus/wbft/core/sequence` | meter | marks the sequence increase | consensus/wbft/core/core.go:50, :189 |
| `consensus/wbft/core/consensus` | timer | time from accepting a proposal to starting the next sequence | consensus/wbft/core/core.go:51, :192 |
| `consensus/wbft/core/timeout_round` | meter | round-change timer expirations | consensus/wbft/core/core.go:52, consensus/wbft/core/handler.go:260 |
| `consensus/wbft/core/commitwork` | timer | block assembly time | miner/worker.go:92, :1414 |
| `chain/head/block`, `chain/head/header` | gauge | head numbers | core/blockchain.go:57-58 |
| `chain/head/finalized`, `chain/head/safe` | gauge | lag the head (§7.3) | core/blockchain.go:60-61 |

---

## 11. Engine API on WBFT chains

The reference `gstable` binary registers the Engine API (`engine` namespace, JWT-authenticated, served on the auth-RPC endpoint) on every node that is not in developer mode, including WBFT nodes. The same methods are also served without JWT on the IPC endpoint and in-process, which receive every registered API except `personal`.

Source: cmd/gstable/config.go:220-236, eth/catalyst/api.go:44-53, node/node.go:379-398, node/node.go:501-508

The Engine API assumes a proof-of-stake beacon client and a terminal total difficulty (TTD). WBFT chains have no TTD. The reference behaviour is:

| Call | Behaviour on a WBFT node |
|---|---|
| `engine_forkchoiceUpdatedV*` with an unknown head | returns `SYNCING`; if the head header was stashed by an earlier `engine_newPayloadV*` (unknown parent, `delayPayloadImport`), it first calls `Merger.ReachTTD()`, which persists `{LeftPoW}` in the database, cancels the downloader and starts a beacon sync |
| `engine_newPayloadV*` with an unknown parent | stashes the header (`remoteBlocks`) and returns `SYNCING` |
| `engine_forkchoiceUpdatedV*` with a known, non-canonical head | TTD checks are skipped (TTD is nil); `SetCanonical(head)` rewinds or reorgs the chain to that block |
| any `forkchoiceUpdated` that reaches the canonical branch | marks the node synced (`SetSynced`) |
| `forkchoiceUpdated` with a non-zero `finalizedBlockHash` | calls `Merger.FinalizePoS()`, which persists `{LeftPoW, EnteredPoS}` in the database; then sets `finalized` (and `safe`) to the given canonical blocks |
| `engine_newPayloadV*` with a known parent | dereferences the nil TTD; the RPC layer recovers the panic and returns `method handler crashed` |
| `engine_exchangeTransitionConfigurationV1` | fails (`invalid ttd`) |

Source: eth/catalyst/api.go:238-371, eth/catalyst/api.go:538-640, eth/catalyst/api.go:645-660, eth/catalyst/api.go:415-443, consensus/merger.go:60-94, rpc/service.go:193-203

Once the merger status says `EnteredPoS` (it survives restarts), the `eth` handler treats the chain as post-merge:

- the fetcher validator and inserter reject every block (`unexpected behavior after transition`), including the blocks the local WBFT core commits on a non-proposer,
- block announcements and block broadcasts from peers are rejected and the peer is dropped (`disallowed block announcement` / `broadcast`),
- the node stops propagating blocks,
- the chain syncer never starts a downloader sync again (`TDDReached`).

Source: eth/handler.go:229-235, eth/handler.go:250-264, eth/handler_eth.go:100-135, eth/handler.go:602-608, eth/sync.go:166-175

A single `engine_forkchoiceUpdated` call with a non-zero finalized hash (JWT-authenticated on the auth-RPC endpoint, or without JWT over IPC) therefore stops a WBFT node from following the chain permanently. `FinalizePoS` runs before the finalized block is looked up, so the status is persisted even when the call returns `Invalid forkchoice state` for an unknown finalized hash; a validator in that state stops contributing blocks while its consensus core may keep voting.

Already with `{LeftPoW}` alone (without `EnteredPoS`), reached by an `engine_newPayload` with an unknown parent followed by an `engine_forkchoiceUpdated` naming that payload as head (zero finalized hash), the chain syncer never starts a downloader sync again (`TDDReached`), and the fetcher inserter discards every block, including local consensus commits, because no block is a terminal PoW block when TTD is nil. Announcements and broadcasts from peers are still accepted and the node still propagates blocks, since those checks look at `EnteredPoS` only.

Source: eth/catalyst/api.go:256-291, eth/catalyst/api.go:340-352, eth/catalyst/api.go:583-586, eth/handler.go:274-294, eth/sync.go:174-176, params/config.go:1058-1061, eth/handler_eth.go:103-105, eth/handler.go:602-608

[SNET-RPC-040] A conforming WBFT node SHOULD NOT expose the Engine API. If it does, it MUST NOT let an Engine API call change the canonical head, the `finalized`/`safe` tags, or the persistent merge-transition status.
Source: cmd/gstable/config.go:229-235, eth/catalyst/api.go:322-371
Observable: rpc

---

## 12. eth and snap wire protocols

StableNet nodes speak `eth/68` and `snap/1` as go-ethereum does, with the values and the deviations listed in this section. Everything not listed here follows the devp2p `eth/68` and `snap/1` specifications. The reference implementation backports the per-peer request tracker and the stricter response checks of go-ethereum releases after v1.13.15 (ethereum#33835 and related, go-stablenet commit `b23c5a831`), so an implementation that follows v1.13.15 is disconnected in the cases of §12.2 and §12.3.

### 12.1 Status handshake

[SNET-SYNC-061] A node MUST advertise `eth` version 68 and no other `eth` version, and MUST disconnect a peer whose first `eth` message is not `Status`, or whose `Status` has a different network ID, protocol version or genesis hash, or a fork ID that the EIP-2124 filter rejects.
Source: eth/protocols/eth/protocol.go:33-46, eth/protocols/eth/handshake.go:83-111, eth/protocols/eth/handler.go:197-202
Observable: network

[SNET-SYNC-062] The network ID MUST be the value configured by the operator; if none is configured it MUST be `8282` with the mainnet preset, `8283` with the testnet preset, and the chain ID otherwise.
Source: cmd/utils/flags.go:1645-1647, cmd/utils/flags.go:1759-1771, eth/backend.go:168-171
Observable: network

[SNET-SYNC-063] The fork ID MUST be computed as in EIP-2124 from the genesis hash (`B-02` §6) and a fork list formed by every top-level chain-configuration block number whose name ends in `Block` (`homesteadBlock`, `daoForkBlock`, `eip150Block` … `mergeNetsplitBlock`, `applepieBlock`, `bohoBlock`) and every top-level fork timestamp (`shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime`) that is set, sorted, deduplicated, with block 0 and timestamps at or before the genesis time removed. `anzeon`, `boho`, `transitions` and every WBFT parameter MUST NOT enter the fork ID. The resulting values for the presets are given in `B-01` §11.1.
Source: core/forkid/forkid.go:242-296, eth/handler.go:385-386
Observable: network

[SNET-SYNC-064] The total difficulty of a block `b` MUST be `genesis.Difficulty + b.Number` (every WBFT header after genesis has difficulty 1, `A-01` WBFT-PARAM-010; the genesis difficulty is 1 on the mainnet preset and 0 on the testnet preset, `B-02` SNET-GEN-003). A node MUST send this value as the head TD in `Status` and as the TD of the carried block in `NewBlock`. The receiver of `NewBlock` records `td - 1` as the TD of the peer's head and uses it for sync-target selection (§4).
Source: eth/handler.go:376-387, eth/handler.go:617-630, eth/handler_eth.go:124-147
Observable: network

The RLPx handshake that carries these protocols limits the size of `auth` and `ack` messages to 2048 bytes (`A-07` WBFT-NET-052).

### 12.2 Response validation

A response that does not match an outstanding request makes the receiver disconnect.

[SNET-SYNC-065] A node that answers an `eth/68` or `snap/1` request MUST answer with the response code of that request and the request ID it carried, and MUST NOT return more items than requested: `BlockHeaders` at most `amount` headers; `BlockBodies`, `Receipts`, `PooledTransactions` and `ByteCodes` at most one item per requested hash; `TrieNodes` at most one node per requested path. It MUST keep the RLP content of the account list of `AccountRange`, and of the slot lists of `StorageRanges`, within twice the requested `bytes`, and MUST NOT send more than 128 proof nodes in `AccountRange` or `StorageRanges`. A node MUST NOT send a response to a request it did not receive.
Source: p2p/tracker/tracker.go:211-245, eth/protocols/eth/dispatcher.go:199-212, eth/protocols/eth/peer.go:338-484, eth/protocols/eth/handlers.go:327-379, eth/protocols/eth/handlers.go:473-512, eth/protocols/eth/handlers.go:588-609, eth/protocols/snap/peer.go:90-181, eth/protocols/snap/handler.go:172-322
Observable: network

[SNET-SYNC-066] The reference implementation disconnects a peer whose response violates SNET-SYNC-065, and also a peer whose response arrives after the request expired from the per-peer tracker (5 minutes for `eth`, 1 minute for `snap`), because the late response no longer matches a request. A node SHOULD answer within these times.
Source: eth/protocols/eth/peer.go:119, eth/protocols/snap/peer.go:51, p2p/tracker/tracker.go:142-185
Observable: network

In go-ethereum v1.13.15 the tracker only recorded metrics. There, an unsolicited `BlockHeaders`, `BlockBodies` or `Receipts` response already caused a disconnect through the request dispatcher (`errDanglingResponse`), but surplus items, an unsolicited `PooledTransactions`, and every `snap` case above did not.

### 12.3 Transaction propagation

[SNET-SYNC-067] A node MUST NOT send more than 5000 transactions in one `Transactions` message, MUST NOT include the same transaction twice in one `Transactions` or `PooledTransactions` message, MUST NOT include a type-`0x03` transaction in `Transactions`, and MUST NOT include a type-`0x03` transaction without its sidecar in `PooledTransactions`. The reference implementation disconnects a peer that does, while it accepts transactions (after it is marked synced, §6); before that it ignores both messages.
Source: eth/protocols/eth/protocol.go:51-52, eth/protocols/eth/handlers.go:572-609, eth/handler_eth.go:73-91, eth/handler_eth.go:150-178
Observable: network

The transaction fetcher also disconnects a peer whose delivered transaction fails the pool's KZG check (`ErrKZGVerificationError`) and drops the rest of that delivery. On an Anzeon chain type-`0x03` transactions are rejected at sender recovery (`B-07` §4.1), so this path was not traced to a reachable case.

Source: eth/fetcher/tx_fetcher.go:340-357, eth/fetcher/tx_fetcher.go:728-732

### 12.4 Bootstrap nodes (informative)

go-stablenet uses the eight StableNet mainnet enodes of `params/bootnodes.go` (TCP port 8589) as default bootstrap nodes whenever no bootnodes are configured by flag or configuration file, including for a custom genesis without `--mainnet`, and the two testnet enodes with `--testnet`. No DNS discovery tree is configured for either network. A node with a custom genesis and no configured bootnodes therefore contacts the mainnet bootnodes; its fork ID differs, so the `eth` handshake fails, but the discovery tables of both sides learn each other.

Source: params/bootnodes.go:20-35, params/bootnodes.go:108-126, cmd/utils/flags.go:1008-1030, cmd/utils/flags.go:1878-1891

---

## 13. Node defaults (informative)

Defaults of go-stablenet that differ from go-ethereum v1.13.15 and are visible through RPC or the network. WBFT timers, block period and epoch length have no command-line flags; they come from the genesis configuration (`B-01` §6, `A-06` §3). The default sync mode (full) is SNET-SYNC-001.

| Setting | go-stablenet | go-ethereum v1.13.15 | Visible effect |
|---|---|---|---|
| `RPCTxFeeCap` (`--rpc.txfeecap`) | 0 (no cap) | 1 ether | `eth_sendTransaction` and `eth_signTransaction` never reject a transaction for its fee |
| `TransactionHistory`, `TxLookupLimit` (`--history.transactions`) | 31 536 000 blocks | 2 350 000 blocks | `eth_getTransactionByHash` and `eth_getTransactionReceipt` return `null` for transactions older than the index (about one year at one block per second) |
| Maximum transaction size in the pool | 262 144 bytes (8 slots of 32 KiB) | 131 072 bytes | larger transactions are rejected by the pool and not gossiped; they are still valid in a block |
| `ForceSyncCycle`, `TdSyncInterval` (`--sync.forcecycle`, `--sync.tdinterval`) | 10 s, 10 s | force cycle a 10 s constant; no TD-stall check | used by SNET-SYNC-020 and SNET-SYNC-021 |
| `GasCeil` (`--miner.gaslimit`) | 105 000 000 | 30 000 000 | the header `GasLimit` of blocks this node proposes moves toward this target (`B-03` SNET-BHDR-013) |

Source: eth/ethconfig/config.go:55-81, cmd/utils/flags.go:283-290, cmd/utils/flags.go:518-524, core/txpool/legacypool/legacypool.go:45-58, cmd/utils/flags.go:782-793

An implementation that relays go-stablenet traffic should accept into its pool and relay transactions up to 262 144 bytes; with a lower limit it drops transactions that go-stablenet nodes gossip.
