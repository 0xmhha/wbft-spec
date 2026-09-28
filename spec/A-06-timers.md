# A-06 Timers

- Status: draft
- Area code: `TIMER`
- Reference: go-stablenet `740526d03`. The timer code in `consensus/wbft/core/core.go` and `core/preprepare.go` is byte-identical between the reference commit and `v1.1.0` (`71e3f820f`); see §11.

This chapter specifies every time-driven behaviour of a WBFT node: the round-change timeout and its value per round, when that timer is armed, re-armed and cancelled, what the node sends when it fires, the ROUND-CHANGE retransmission timer, the deferred processing of a PRE-PREPARE whose block timestamp is in the future, the block-period wait before a proposal is built, and the timestamp that the proposer writes into the header. It ends with the expected timeline of one height so that an external observer (the inspector) can predict when messages should appear.

The message handlers themselves (what a PRE-PREPARE, ROUND-CHANGE, ... does to the state) are in `A-05`. Message encodings are in `A-03`. What "sending" a message means on the network (targets, deduplication) is in `A-07`; §6.4 of this chapter depends on it.

---

## 1. Terms

| Term | Meaning |
|---|---|
| *wall clock* | The node's real-time clock, `now()`, in nanoseconds since the Unix epoch. `unix_now()` is `floor(now() / 10^9)`. |
| *monotonic clock* | A clock that measures elapsed time and does not jump. All timer durations in this chapter are measured on it. |
| *view* | `(sequence, round)`, see `A-01`. `h` denotes the sequence (height) being agreed, `r` the round. |
| *entering a view* | The moment the node executes the "start new round" procedure of `A-05` (`start_new_round`) and its current view changes. |
| *ROUND-CHANGE broadcast procedure* | `broadcast_round_change(node, round)` of `A-05` (WBFT-SM-051, WBFT-SM-052). |
| `RT(h)` | `config_at(h).request_timeout` (`A-01` §6.1), the request timeout in milliseconds that applies to height `h` (§3). |
| `MRT(h)` | `config_at(h).max_request_timeout_seconds`, the round-timeout cap in seconds that applies to height `h`; `0` means "no cap". |
| `BP(h)` | `config_at(h).block_period` (seconds). |
| `AFBT` | `allowed_future_block_time` (seconds) of the base configuration (not transition-aware, §3). |
| `Time(b)` | The `Time` field (Unix seconds) of the header of block `b`. |

---

## 2. Clock assumptions

A WBFT node uses two clocks, and the protocol has different assumptions about each.

Durations (the round-change timeout, the retry interval, the future-proposal wait once computed, the block-period wait once computed) are measured on the monotonic clock. A wall-clock step during a running timer does not shorten or lengthen it.

Absolute times come from the wall clock and are compared with header timestamps, which are whole Unix seconds: the block-period wait (§8.1) is computed as the difference between a header-derived Unix second and `now()`, the proposer writes `unix_now()` into a header (§8.2), and a receiver decides whether a proposal is "from the future" by comparing its `Time` with `unix_now()` (§7).

[WBFT-TIMER-001] A node MUST measure every timer duration in this chapter on a monotonic clock. Once a wait duration has been computed from the wall clock (§7, §8.1), it MUST NOT be recomputed if the wall clock changes while the wait is running.
Source: consensus/wbft/core/core.go:411 (`time.AfterFunc`), consensus/wbft/core/core.go:446, consensus/wbft/core/preprepare.go:156, miner/worker.go:436-438 (`time.Sleep(waitTime)`)
Observable: log

The protocol is live only if the validators' wall clocks agree closely. The reference implementation contains no clock-synchronisation mechanism and no bound check; the assumptions below are informative and state what goes wrong when they are violated.

- *Proposer clock ahead of a receiver by `δ` seconds.* The receiver sees `Time(proposal) > unix_now() + AFBT` and defers the PRE-PREPARE by up to `δ` (§7). If the deferral exceeds the remaining round timeout, the receiver changes round without having accepted the proposal.
- *Proposer clock behind.* The header timestamp is still at least `Time(parent) + BP(h)` (§8.2), so receivers accept it; the proposal is simply built later in real time.
- *Receiver clock behind.* Same effect as a proposer clock ahead.
- With the default `AFBT = 0`, a receiver whose clock is behind the proposer's by more than the time the proposal needs to be built and delivered (typically well under one second) defers a round-0 PRE-PREPARE built at `Time(h − 1) + BP(h)`, until its own clock reaches that second. Operators SHOULD keep validator clocks within a small fraction of a second (for example with NTP).

---

## 3. Which configuration applies

The timers of a view `(h, r)` read the consensus configuration of height `h` (the height being agreed, not the parent). Configuration transitions (`B-01`) take effect at the first height whose number is at least the transition's block number.

[WBFT-TIMER-002] For every timer of view `(h, r)` (round-change timer, retry timer, block-period wait) a node MUST use the parameters `RT(h)`, `MRT(h)`, `BP(h)` of `config_at(h)`, where a transition with block number `B` applies to all heights `h >= B`, later transitions override earlier ones field by field (transitions are sorted by block number when the engine is created, `B-01` §6.2), and a transition field equal to `0` (for `requestTimeoutSeconds`, `blockPeriodSeconds`) or absent (for `maxRequestTimeoutSeconds`) leaves the previous value in place.
Source: consensus/wbft/config.go:161-192 (`GetConfig`, `getTransitionValue`), consensus/wbft/core/core.go:364, consensus/wbft/core/core.go:439, consensus/wbft/engine/engine.go:482-484 (`PeriodToNextBlock`), eth/ethconfig/config.go:254-262 (sort)
Observable: network, log

[WBFT-TIMER-003] `RT(h)` MUST be `requestTimeoutSeconds × 1000` (milliseconds) taken from the genesis `anzeon.wbft` section or from the latest applicable transition. A transition that sets `maxRequestTimeoutSeconds` explicitly to `0` MUST remove the cap from that height on.
Source: eth/ethconfig/config.go:215-217, eth/ethconfig/config.go:231-233, consensus/wbft/config.go:165-168, consensus/wbft/config.go:178-180, params/config_wbft.go:186-198
Observable: network

The genesis validation rejects `requestTimeoutSeconds = 0` and `blockPeriodSeconds = 0` (`params/config_wbft.go:120-125`), so `RT(h) >= 1000` ms and `BP(h) >= 1` s on every chain that passes it. Transitions are not validated in the same way; a transition value of `0` means "unchanged", so the lower bounds still hold.

[WBFT-TIMER-004] `AFBT` MUST be taken from the base configuration (`anzeon.wbft.allowedFutureBlockTime`, default `0`). Transitions MUST NOT change it, even if a transition object carries the field.
Source: eth/ethconfig/config.go:224-226, consensus/wbft/config.go:161-184 (field not copied), consensus/wbft/engine/engine.go:202
Observable: network

Implementation note (informative). `wbft.DefaultConfig` has `RequestTimeout = 1000` ms, but the node never uses it: `CreateConsensusEngine` starts from a zero `wbft.Config` and copies the chain-config values into it (`eth/ethconfig/config.go:193-194`). The "1000 ms" configuration in the tables of §4.4 is therefore `requestTimeoutSeconds = 1`, not an implicit default.

---

## 4. Round timeout

### 4.1 Definition

`round_timeout(config, round)` returns the duration, in nanoseconds, of the round-change timer for a view whose round is `round`. It reproduces the reference implementation's 64-bit arithmetic exactly, including wrap-around, because the result is observable (it decides when ROUND-CHANGE messages are sent).

```python
MAX_INT64 = 2**63 - 1
NS_PER_MS = 1_000_000
NS_PER_S  = 1_000_000_000

def wrap_int64(x: int) -> int:
    """Two's-complement reduction to int64 (Go signed overflow semantics)."""
    x &= 2**64 - 1
    return x - 2**64 if x >= 2**63 else x

def round_timeout(config: Config, round: Round) -> int:   # nanoseconds, int64
    r    = round % 2**64                                              # big.Int.Uint64()
    base = wrap_int64(config.request_timeout * NS_PER_MS)          # time.Duration(ms) * time.Millisecond
    cap  = wrap_int64(config.max_request_timeout_seconds * NS_PER_S)  # time.Duration(s) * time.Second

    if cap > 0:                                   # capped branch
        t = base
        for _ in range(r):
            t = wrap_int64(t * 2)
            if t > cap:
                t = cap
                break
        if t < base:                              # "overflow" guard (see §4.2)
            t = cap
        return t
    else:                                         # uncapped branch (cap == 0, or cap wrapped negative)
        f = float64(2.0 ** r) * float64(base)     # IEEE-754 binary64; 2.0**r is +Inf for r >= 1024
        if isnan(f) or isinf(f) or f > float64(MAX_INT64):
            return MAX_INT64
        return int64(f)                           # truncation toward zero
```

`float64(MAX_INT64)` is `2^63` after rounding. For `base > 0`, `f` can never equal `2^63` exactly, because `base` is a multiple of `10^6` and therefore not a power of two; the conversion `int64(f)` is then in range. When `base` has wrapped to a negative value (`9 223 372 037 <= requestTimeoutSeconds <= 18 446 744 073`), `f` can be below `−2^63`; the Go conversion is then implementation-specific (`MinInt64` on arm64 and amd64), and the pseudocode's `int64(f)` returns the unbounded truncated value instead. Any negative duration makes the timer expire immediately, so the observable behaviour is the same.

[WBFT-TIMER-005] The round-change timer armed for a view `(h, r)` MUST have the duration `round_timeout(config_at(h), r)` as defined in §4.1.
Source: consensus/wbft/core/core.go:363-402
Observable: network, log

[WBFT-TIMER-006] In the capped branch, a node MUST use `base` as the round-0 timeout even when `base > cap` (the cap is applied only inside the doubling loop, which does not execute for round 0). Consequently, when `base > cap`, `round_timeout` is not monotonic: round 0 lasts `base`, every later round lasts `cap`.
Source: consensus/wbft/core/core.go:374-382
Observable: network, log

[WBFT-TIMER-007] When the capped branch ends with `t < base`, the node MUST use `cap` as the timeout. The reference implementation logs `WBFT: Possible request timeout overflow detected, setting timeout value to maxRequestTimeout` (Warn) in that case; when `base > cap` this happens for every round `r >= 1`, although no overflow occurred.
Source: consensus/wbft/core/core.go:383-390
Observable: log

[WBFT-TIMER-008] In the uncapped branch, a node MUST clamp the timeout to `MAX_INT64` nanoseconds (about 292.47 years, i.e. the timer never fires in practice) when `2^r × base` is NaN, infinite, or greater than `2^63` in binary64 arithmetic, and log `WBFT: Timeout overflow detected, setting timeout value to MaxInt64` (Warn).
Source: consensus/wbft/core/core.go:391-402
Observable: log

For every configuration that passes genesis validation and has a realistic cap (`base <= cap <= 2^62` ns, i.e. `maxRequestTimeoutSeconds <= 4 611 686 018`), §4.1 reduces to the closed form

```
round_timeout = base                         if r == 0
              = min(base × 2^r, cap)         if r >= 1 and cap > 0
              = min(base × 2^r, MAX_INT64)   if cap == 0
```

The wrap-around paths of §4.1 are reached only with absurd parameters (`requestTimeoutSeconds > 9 223 372 036` or `maxRequestTimeoutSeconds > 4 611 686 018`). One of them is worth knowing: a cap of `9 223 372 037 <= maxRequestTimeoutSeconds <= 18 446 744 073` wraps to a negative `cap`, and the node silently behaves as uncapped (worked example 5). Larger values wrap modulo `2^64` again and can give a small positive cap (for example `18 446 744 074` gives `cap = 290 448 384` ns).

Implementation note (informative). The doubling loop runs at most `r` iterations and stops at the first value above `cap`. If `cap > 2^62` and `t` wraps to exactly `0`, the loop no longer breaks and runs `r` iterations; `r` is bounded in practice because rounds advance one at a time or by F+1 jumps limited to `current + 10` (`A-05`, `consensus/wbft/core/backlog.go:101-110`).

### 4.2 Worked examples

1. Mainnet preset (`requestTimeoutSeconds = 2`, no cap), `r = 3`. `base = 2 000 000 000` ns. Uncapped branch: `f = 8 × 2·10^9 = 1.6·10^10` → **16 s**.
2. `requestTimeoutSeconds = 2`, `maxRequestTimeoutSeconds = 10`, `r = 3`. Capped branch: `t = 2 s → 4 s → 8 s → 16 s > 10 s` → `t = 10 s`, break; `10 s >= 2 s` → **10 s**.
3. `--dev` preset (`requestTimeoutSeconds = 1000`, `maxRequestTimeoutSeconds = 4`). `base = 1 000 s`, `cap = 4 s`. `r = 0`: loop does not run, `t = 1 000 s`, not `< base` → **1 000 s**. `r = 1`: `t = 2 000 s > 4 s` → `t = 4 s`, break; `4 s < 1 000 s` → overflow guard, Warn log → **4 s**. Every `r >= 1` gives **4 s** with the Warn log.
4. Mainnet preset, `r = 33`. `f = 2^33 × 2·10^9 = 1.718·10^19 > 2^63` → **`MAX_INT64` ns** with the Warn log. `r = 32` still gives `8 589 934 592 000 000 000` ns (about 272.2 years).
5. `requestTimeoutSeconds = 2`, `maxRequestTimeoutSeconds = 10 000 000 000`. `cap = wrap_int64(10^19) = -8 446 744 073 709 551 616` → not `> 0` → uncapped branch, as if no cap were configured.

### 4.3 Presets

The presets compiled into the reference implementation (`params/config.go`) are:

| Preset | `requestTimeoutSeconds` | `RT` (ms) | `maxRequestTimeoutSeconds` | Branch |
|---|---|---|---|---|
| `StableNetMainnetChainConfig` | 2 | 2 000 | absent (0) | uncapped |
| `StableNetTestnetChainConfig` | 2 | 2 000 | absent (0) | uncapped |
| `AllDevChainProtocolChanges` (`--dev`) | 1 000 | 1 000 000 | 4 | capped, `base > cap` |
| `TestWBFTChainConfig` | 1 000 | 1 000 000 | 4 | capped, `base > cap` |

Source: params/config.go:68-72, params/config.go:172-176, params/config.go:404, params/config.go:424-429, params/config.go:630-635

The operating presets 8282 and 8283 use `requestTimeoutSeconds = 2` (`params/config.go:70`, `params/config.go:174`).

### 4.4 Timeout tables

Values are `round_timeout` for the listed round, followed by the cumulative time from entering round 0 to entering round `r` when no PRE-PREPARE is ever accepted, `Σ_{i<r} round_timeout(i)`. A timer restart on PRE-PREPARE acceptance (§5.2) lengthens a round beyond these values.

`RT = 2 000 ms` (mainnet, testnet):

| Round `r` | Uncapped | cumulative | Cap 4 s | cumulative | Cap 10 s | cumulative | Cap 60 s | cumulative |
|---|---|---|---|---|---|---|---|---|
| 0 | 2 s | 0 s | 2 s | 0 s | 2 s | 0 s | 2 s | 0 s |
| 1 | 4 s | 2 s | 4 s | 2 s | 4 s | 2 s | 4 s | 2 s |
| 2 | 8 s | 6 s | 4 s | 6 s | 8 s | 6 s | 8 s | 6 s |
| 3 | 16 s | 14 s | 4 s | 10 s | 10 s | 14 s | 16 s | 14 s |
| 4 | 32 s | 30 s | 4 s | 14 s | 10 s | 24 s | 32 s | 30 s |
| 5 | 64 s | 62 s | 4 s | 18 s | 10 s | 34 s | 60 s | 62 s |
| 6 | 128 s | 126 s | 4 s | 22 s | 10 s | 44 s | 60 s | 122 s |
| 8 | 512 s | 510 s | 4 s | 30 s | 10 s | 64 s | 60 s | 242 s |
| 10 | 2 048 s (34 min 8 s) | 2 046 s | 4 s | 38 s | 10 s | 84 s | 60 s | 362 s |
| 16 | 131 072 s (36 h 24 min 32 s) | 131 070 s | 4 s | 62 s | 10 s | 144 s | 60 s | 722 s |
| 32 | 8.59·10^9 s (≈272 y) | — | 4 s | — | 10 s | — | 60 s | — |
| ≥ 33 | `MAX_INT64` ns (≈292 y) | — | 4 s | — | 10 s | — | 60 s | — |

`RT = 1 000 ms` (`requestTimeoutSeconds = 1`):

| Round `r` | Uncapped | cumulative | Cap 4 s | cumulative | Cap 10 s | cumulative |
|---|---|---|---|---|---|---|
| 0 | 1 s | 0 s | 1 s | 0 s | 1 s | 0 s |
| 1 | 2 s | 1 s | 2 s | 1 s | 2 s | 1 s |
| 2 | 4 s | 3 s | 4 s | 3 s | 4 s | 3 s |
| 3 | 8 s | 7 s | 4 s | 7 s | 8 s | 7 s |
| 4 | 16 s | 15 s | 4 s | 11 s | 10 s | 15 s |
| 5 | 32 s | 31 s | 4 s | 15 s | 10 s | 25 s |
| 10 | 1 024 s | 1 023 s | 4 s | 35 s | 10 s | 75 s |
| 33 | 8.59·10^9 s | — | 4 s | — | 10 s | — |
| ≥ 34 | `MAX_INT64` ns | — | 4 s | — | 10 s | — |

`RT = 1 000 000 ms`, cap 4 s (`--dev`, test preset): round 0 = 1 000 s; every round `r >= 1` = 4 s (with the Warn log of WBFT-TIMER-007). The retry interval (§6) is 1 000 s.

---

## 5. Round-change timer

At any time a node that runs the consensus engine has at most one armed round-change timer, belonging to its current view.

### 5.1 Arming on entering a view

[WBFT-TIMER-010] Every time a node enters a view `(h, r)` (a new sequence with `r = 0`, or a new round of the current sequence), it MUST cancel any armed round-change timer and arm a new one with duration `round_timeout(config_at(h), r)`, measured from the moment it enters the view.
Source: consensus/wbft/core/core.go:163-274 (`startNewRound`; timer armed at 268-271), consensus/wbft/core/core.go:355-415
Observable: network, log

The views that are entered, and when, are defined in `A-05`: engine start (`consensus/wbft/core/handler.go:44`), a new chain head (`consensus/wbft/core/final_committed.go:27-31`), the round-change timer firing (§5.4), and F+1 ROUND-CHANGE messages for higher rounds (`consensus/wbft/core/roundchange.go:161-169`).

[WBFT-TIMER-011] When the "start new round" procedure returns without changing the view (the target round is `0` while the chain head is still `h - 1`; the target round is lower than the current round; or the chain head is behind the current sequence by more than one), the node MUST NOT touch any timer: the armed round-change timer, retry timer and future-PRE-PREPARE timer keep running unchanged.
Source: consensus/wbft/core/core.go:196-209
Observable: network, log

### 5.2 Re-arming on PRE-PREPARE acceptance

[WBFT-TIMER-012] When a node accepts a PRE-PREPARE for its current view `(h, r)` (the PRE-PREPARE passes every check of `A-05` while the node is in state `AcceptRequest`), it MUST cancel the armed round-change timer and arm a new one with the full duration `round_timeout(config_at(h), r)`, measured from the moment of acceptance, before it sends its PREPARE.
Source: consensus/wbft/core/preprepare.go:171-193
Observable: network, log

The deadline of round `r` is therefore `t_enter(h, r) + round_timeout(r)` if no PRE-PREPARE is accepted, and `t_accept(h, r) + round_timeout(r)` otherwise. Because `t_accept >= t_enter`, the time a round can last before it times out is between `round_timeout(r)` and `(t_accept − t_enter) + round_timeout(r)`. In round 0 the block-period wait (§8.1) is part of `t_accept − t_enter`.

Arming or re-arming the round-change timer (WBFT-TIMER-010, WBFT-TIMER-012) is implemented by one procedure that first stops all three timers of the core. The following requirement states the side effect.

[WBFT-TIMER-013] Whenever a node arms or re-arms the round-change timer, it MUST also cancel the ROUND-CHANGE retry timer (§6) and the future-PRE-PREPARE timer (§7).
Source: consensus/wbft/core/core.go:336-347 (`stopTimer`), consensus/wbft/core/core.go:356
Observable: network

### 5.3 Cancellation

A timer that has already expired may have queued its event before it was cancelled. The reference implementation gives each round-change timer its own `canceled` flag; cancelling sets the flag, and the event handler drops an event whose flag is set.

[WBFT-TIMER-014] Within one run of the core (between an engine start and the following stop), an expiry of a round-change timer that has since been cancelled or superseded by a newer round-change timer MUST have no effect: the node MUST NOT change view and MUST NOT send anything because of it, regardless of whether the expiry event was generated before or after the cancellation. For a timer of an earlier run, see WBFT-TIMER-018.
Source: consensus/wbft/core/core.go:331-335, consensus/wbft/core/core.go:405-414, consensus/wbft/core/handler.go:152-160
Observable: network, log

Implementation note (informative). The flag pointer is captured in the timer's closure since commit `c37994e9b` (#82, "race conditions in newRoundChangeTimer"). Before that fix the closure read the core's *current* flag at firing time, so a stale expiry could pick up the flag of a newer, uncancelled timer and cause an unintended round change. The flag is written under `timerMu` and read by the event loop without taking `timerMu`.

### 5.4 Expiry

[WBFT-TIMER-015] When the effective (not cancelled) round-change timer of view `(h, r)` expires, the node MUST, in this order: (1) execute the "start new round" procedure of `A-05` with target round `r + 1`; (2) execute the ROUND-CHANGE broadcast procedure (§6.1) for target round `r + 1`. If step (1) enters view `(h, r + 1)`, the node therefore sends a ROUND-CHANGE for `(h, r + 1)` and has a new round-change timer of duration `round_timeout(config_at(h), r + 1)` and a retry timer (§6) armed.
Source: consensus/wbft/core/handler.go:250-263 (`handleTimeoutMsg`), consensus/wbft/core/roundchange.go:52-98
Observable: network, log
Observed as: the log lines `WBFT: TIMER CHANGING ROUND` (Warn) and `WBFT: broadcast ROUND-CHANGE message` (Info).

The timer is not specific to "no PRE-PREPARE received": it fires whenever the view does not change within the timeout, whatever the node's state. In particular, reaching a COMMIT quorum does not stop it.

[WBFT-TIMER-016] A node MUST NOT cancel the round-change timer when it reaches a COMMIT quorum, enters state `Committed`, or hands the decided block to the application. The timer is cancelled only by the next view change (WBFT-TIMER-010), by a PRE-PREPARE acceptance (WBFT-TIMER-012), or by stopping the engine (WBFT-TIMER-018).
Source: consensus/wbft/core/commit.go:123-126, consensus/wbft/core/commit.go:137-179 (`commitWBFT` only sets the state), consensus/wbft/core/core.go:336-347
Observable: network

The observable consequence depends on where the decided block is when the timer fires. Let `(h, r)` be the view in which the node decided.

- **Case A: the block is not yet the chain head.** Step (1) of WBFT-TIMER-015 enters `(h, r + 1)` (the head is still `h − 1`). The node sends a ROUND-CHANGE for `(h, r + 1)` that carries its prepared round `r`, the prepared block and the PREPARE justification. When the block becomes the head, the node enters `(h + 1, 0)` normally. Side effect on the next header: the node's prior round becomes `r + 1` instead of `r`, so it no longer collects late PREPARE/COMMIT seals for `(h, r)` as extra seals (`A-05`, `consensus/wbft/core/backlog.go:160-162`, `consensus/wbft/core/priorstate.go:35-45`); if this node proposes `h + 1`, the previous-seal sets in its header contain only the quorum seals of block `h`.
- **Case B: the block became the head, but the node has not yet processed the corresponding new-head event.** Step (1) takes the "catch up" path of `A-05` and enters `(h + 1, 0)`, with a new round-change timer of `round_timeout(config_at(h + 1), 0)`, but the round-change message set and the stored PREPARE justification of height `h` are not reset because the target round is not `0`. Step (2) then signs and sends a ROUND-CHANGE for view `(h + 1, r + 1)` with no prepared round or block (and, if the node was prepared at `h`, a stale `justification` field, the PREPARE certificate of height `h`). The following new-head event does nothing (target round `0` with head `h`, WBFT-TIMER-011).

[WBFT-TIMER-017] When the round-change timer of `(h, r)` fires after the node's chain head has advanced to `h` but before the node has entered `(h + 1, 0)` through the new-head event, the node MUST enter `(h + 1, 0)` and MUST then send a ROUND-CHANGE whose view is `(h + 1, r + 1)` (sequence of the new height, round computed from the old one).
Source: consensus/wbft/core/handler.go:250-263, consensus/wbft/core/core.go:185-195, consensus/wbft/core/core.go:231-240, consensus/wbft/core/core.go:250-258, consensus/wbft/core/roundchange.go:57-63
Observable: network

Case A needs block insertion to take longer than the remaining timeout (`round_timeout(r)` counted from PRE-PREPARE acceptance). Case B needs the expiry to fall into the short interval between chain-head update and new-head processing.

### 5.5 Engine start and stop

[WBFT-TIMER-018] When the consensus engine is stopped, the node MUST cancel the round-change timer, the retry timer and the future-PRE-PREPARE timer, and MUST NOT send any consensus message because of a timer while the engine is stopped. In the reference implementation, a timer that the event loop arms between the `stopTimer` call and the unsubscription in `Stop` is not cancelled. Its expiry has no effect while the engine is stopped, but if it expires after the engine has been restarted, the new core handles it as its own: a stale round-change expiry makes the new core change round and send a ROUND-CHANGE, a stale retry expiry sends a ROUND-CHANGE, and a stale future-PRE-PREPARE expiry re-processes that PRE-PREPARE.
Source: consensus/wbft/core/handler.go:50-59 (`Stop`: `stopTimer`, then unsubscribe), consensus/wbft/backend/engine.go:288-300, consensus/wbft/backend/backend.go:355-367 (a new core on the same event mux), event/event.go:204-216, consensus/wbft/core/handler.go:152-178
Observable: network

When the engine is started it enters view `(head + 1, 0)` (`consensus/wbft/core/handler.go:35-47`), which arms the round-change timer by WBFT-TIMER-010. The reference implementation stops the engine when the downloader starts a synchronisation and restarts it when the synchronisation ends (`miner/miner.go:139-161`), but only until the first successful synchronisation of the process; later synchronisations do not stop it (`B-09` SNET-SYNC-030). Each restart begins a fresh round-0 timer (see WBFT-TIMER-018 for a stale timer of the previous run).

---

## 6. ROUND-CHANGE retry timer

### 6.1 Arming

The ROUND-CHANGE broadcast procedure (`A-05`) is invoked on four occasions: the round-change timer fired (§5.4), F+1 ROUND-CHANGE messages for higher rounds were received, handing a decided block to the application failed, and the retry timer fired (§6.2). Its first action, before any check, is to arm the retry timer.

[WBFT-TIMER-020] Every invocation of the ROUND-CHANGE broadcast procedure MUST first cancel the armed retry timer and arm a new one with duration `RT(h)` milliseconds (no doubling, no cap), where `(h, r_c)` is the node's current view at that moment; the new timer MUST remember `r_c`.
Source: consensus/wbft/core/roundchange.go:52-53, consensus/wbft/core/core.go:426-450, consensus/wbft/core/roundchange.go:40-43, consensus/wbft/core/roundchange.go:161-169, consensus/wbft/core/commit.go:173-176, consensus/wbft/core/handler.go:170-178
Observable: log

The retry timer is armed even if the procedure then sends nothing: because the target round is below the current round, or because the node is not in the validator set (`A-07` WBFT-NET-030).

### 6.2 Expiry

[WBFT-TIMER-021] When the retry timer that remembers round `r_c` expires, the node MUST invoke the ROUND-CHANGE broadcast procedure with target round `r_c`. If the node's current round is not greater than `r_c` (it is still `r_c`, or a queued expiry is handled after the node entered a new height), this re-creates, re-signs and broadcasts a ROUND-CHANGE for `(current sequence, r_c)` carrying the node's current prepared round, prepared block and PREPARE justification, and (by WBFT-TIMER-020) re-arms the retry timer.
Source: consensus/wbft/core/handler.go:170-178, consensus/wbft/core/roundchange.go:52-98
Observable: log
Observed as: the log line `WBFT: broadcast ROUND-CHANGE message` (Info), every `RT(h)`.

[WBFT-TIMER-022] If the node's current round is greater than `r_c` when the retry timer expires, the node MUST NOT send a ROUND-CHANGE, but it MUST still arm a new retry timer for its current view (WBFT-TIMER-020).
Source: consensus/wbft/core/roundchange.go:53-61
Observable: log
Observed as: the log line `WBFT: invalid past target round` (Warn).

Retry expiries have no cancellation flag. An expiry that was queued before the retry timer was cancelled is still processed. If the node has meanwhile entered a higher round of the same height, it sends nothing (WBFT-TIMER-022). If the node has meanwhile entered a new height, its round `0` is not greater than `r_c`, and it signs and sends a ROUND-CHANGE for `(new sequence, r_c)` with its current (empty) prepared round and block. In both cases it re-arms a retry timer for its current view, which fires `RT(h)` later and then sends a ROUND-CHANGE for the current round unless the timer has been cancelled in the meantime.

[WBFT-TIMER-023] The retry timer MUST be cancelled only by arming or re-arming the round-change timer (WBFT-TIMER-013) or by stopping the engine (WBFT-TIMER-018). It MUST NOT be cancelled by reaching a PREPARE or COMMIT quorum.
Source: consensus/wbft/core/core.go:336-347, consensus/wbft/core/core.go:417-422
Observable: log

Retransmission therefore continues every `RT(h)` for as long as the node stays in the round it was in when the retry timer was armed (which need not be the target round of the ROUND-CHANGE, `A-05` WBFT-SM-052) and accepts no PRE-PREPARE in it.

### 6.3 Local effect

The retransmitted ROUND-CHANGE is also delivered to the node itself (`A-07` WBFT-NET-030) and processed by the ROUND-CHANGE handler of `A-05`. That re-runs the proposer's "quorum of ROUND-CHANGE for the current round" check. In round `r >= 1` this is what lets a proposer send its PRE-PREPARE when its quorum was complete before its own proposal was built (§8.3).

### 6.4 Wire effect

ECDSA signatures in the reference implementation are deterministic (RFC 6979), and the RLP encoding is canonical. A retransmitted ROUND-CHANGE whose content (view, prepared round, prepared block, justification) is unchanged is therefore byte-identical to the previous one and has the same deduplication key (`A-07` §5.2). The gossip procedure skips every peer that the node already sent that key to or received it from (`A-07` WBFT-NET-032).

[WBFT-TIMER-024] A retransmitted ROUND-CHANGE that is byte-identical to one the node sent earlier MUST be put on the wire only towards validator peers whose per-peer recent-message cache (`A-07` §5.2, WBFT-NET-023 and WBFT-NET-032) does not contain its key (`A-07` WBFT-NET-022). In the reference implementation this cache is keyed by the peer's address and survives reconnection, so during a stalled round retransmissions normally produce no network traffic at all; they are visible in the log and in the node's own processing (§6.3) only.
Source: consensus/wbft/core/roundchange.go:63-94, consensus/wbft/backend/backend.go:176-210, crypto/secp256k1/secp256.go:76-79
Observable: network, log

The retry reaches the wire in these cases: the peer's entry, or the key in it, was evicted from the per-peer cache (40 other peer addresses, or 1 024 other keys of that peer, were used after the last lookup; every retransmission attempt looks the address and the key up and so refreshes both, so during a stalled round eviction needs that many other addresses or keys within one retry interval); the content changed (for example after a failed `finalize` in round `r`, when the retry re-sends `(h, r)` with `prepared_round == r`, `A-05` WBFT-SM-087); or the peer is sent to for the first time since the node restarted (caches are in memory only).

---

## 7. Future PRE-PREPARE wait

A PRE-PREPARE is "from the future" when the header timestamp of its proposal is later than the receiver's wall clock plus `AFBT`. The receiver neither accepts nor rejects it; it re-processes it when the timestamp is reached.

[WBFT-TIMER-030] If, while processing a PRE-PREPARE for its current view, a node finds `Time(proposal) > floor((now() + AFBT × 10^9) / 10^9)` (the header check of `A-08` that runs first among the header checks), the node MUST NOT accept the PRE-PREPARE and MUST NOT relay it, and MUST arm a future-PRE-PREPARE timer with duration `d = Time(proposal) × 10^9 − now()` nanoseconds (the time until the header timestamp, not until `Time − AFBT`).
Source: consensus/wbft/core/preprepare.go:147-169, consensus/wbft/backend/backend.go:258-278, consensus/wbft/engine/engine.go:172-175, consensus/wbft/engine/engine.go:201-205
Observable: network, log
Observed as: the log line `WBFT: PRE-PREPARE block proposal is in the future (will be treated again later)` (Info, with `duration`).

The checks that precede the future check (proposer identity, sequence equals block number, justification for `r > 0`, then proposal steps P1-P5 of `A-08` §5: block type, bad-block list, validator sets, transaction root, uncle hash, and header step H1) must have passed; the header checks after it (`A-08`) have not run yet. They run when the PRE-PREPARE is processed again.

[WBFT-TIMER-031] When the future-PRE-PREPARE timer expires, the node MUST process the deferred PRE-PREPARE again as if it had just arrived with valid signatures: it is checked against the node's view and state at that moment (`A-05`), and, if accepted, the node MUST send its PREPARE and relay the PRE-PREPARE (`A-07` WBFT-NET-041).
Source: consensus/wbft/core/preprepare.go:156-162, consensus/wbft/core/handler.go:136-150
Observable: network

[WBFT-TIMER-032] A node MUST keep at most one deferred PRE-PREPARE. Arming a future-PRE-PREPARE timer MUST cancel the previous one, whose PRE-PREPARE is then not re-processed from the timer, unless that timer had already expired and its event was still waiting to be handled.
Source: consensus/wbft/core/preprepare.go:154-163, consensus/wbft/core/core.go:321-325
Observable: network

[WBFT-TIMER-033] The future-PRE-PREPARE timer MUST be cancelled whenever the round-change timer is armed or re-armed (WBFT-TIMER-013) or the engine stops. A PRE-PREPARE whose deferral `d` is longer than the remaining round-change timeout is therefore never accepted in its round.
Source: consensus/wbft/core/core.go:336-347, consensus/wbft/core/core.go:356
Observable: network

The round-change timer keeps running during the deferral; the deferral does not extend the round.

---

## 8. Block-period wait and header timestamp

### 8.1 When a proposal is built

Every validator whose block production is enabled builds a candidate block when it enters a view, not only the proposer (the proposer is the only one that sends it, `A-05`). The build is triggered after a wait that depends on the round.

[WBFT-TIMER-040] On entering view `(h, 0)` through `start_new_round` with argument 0, a node MUST start building its candidate block for height `h` after a wait of `w = Time(head) × 10^9 + BP(h) × 10^9 − now()` nanoseconds, where `head` is the chain head (block `h − 1`) at the moment the view is entered; if `w <= 0` it MUST start immediately.
Source: consensus/wbft/core/core.go:260, consensus/wbft/backend/engine.go:139-150 (`timeForNextWork`), consensus/wbft/backend/engine.go:277-285 (`NotifyNewRound`), miner/worker.go:432-441, miner/worker.go:664-669
Observable: network
Observed as: the time of the round-0 PRE-PREPARE.

[WBFT-TIMER-041] When `start_new_round` is called with an argument `r >= 1`, a node MUST start building a new candidate block for height `h` without waiting. This includes the late-timeout `CATCH_UP` case, in which the node enters `(h, 0)` but the builder is told the requested round (`A-05` WBFT-SM-029, WBFT-SM-080).
Source: consensus/wbft/backend/engine.go:279-283, miner/worker.go:664-669
Observable: network

The build is skipped while the miner's `syncing` flag is set, which happens only during synchronisations before the first successful one (`miner/miner.go:139-165`, `miner/worker.go:1322-1324`), the same limitation as the engine stop in §5.5. A new build interrupts an unfinished one.

Implementation note (informative). WBFT has no empty-block period. A validator builds, and the proposer proposes, a block in every view whether or not its pool holds transactions (`commitWork` has no transaction-count condition, `miner/worker.go:1320-1383`), so a chain without traffic still produces one block per `BP(h)` seconds. Quorum's `emptyBlockPeriod` (`consensus/istanbul/qbft/core/request.go:56-97` at commit `5ffacc48`) has no counterpart, and the Quorum configuration keys are ignored (`B-01` SNET-CFG-030).

### 8.2 Header timestamp

[WBFT-TIMER-042] A node building a candidate block for height `h` with parent `p` MUST set `Time = max(Time(p) + BP(h), unix_now())`, with `unix_now()` read when the header is prepared.
Source: consensus/wbft/engine/engine.go:501-505, miner/worker.go:1134-1146 (overwritten by `Prepare`), miner/worker.go:1180
Observable: header

Consequences for validity (`A-08`, informative here): the header satisfies `Time >= Time(parent) + BP(h)` (checked by every verifier, `consensus/wbft/engine/engine.go:270-272`), and on a correctly synchronised clock it is never "from the future" for other validators, because the proposer builds at or after `Time`.

With WBFT-TIMER-040, the round-0 header timestamp is `Time(h − 1) + BP(h)` whenever the node entered `(h, 0)` before that instant and the build started within the same second; otherwise it is the wall-clock second in which the header was prepared. Blocks are then exactly `BP` seconds apart as long as agreeing on and importing a block takes less than `BP`; slower heights push the timestamps later, and the schedule does not catch up.

### 8.3 When the PRE-PREPARE is sent

[WBFT-TIMER-043] A proposer MUST NOT send the round-0 PRE-PREPARE for height `h` before its wall clock has reached `Time(h − 1) + BP(h)` (it sends it when its build for `(h, 0)` completes), except after a late round timeout that took the `CATCH_UP` branch (`A-05` WBFT-SM-080), where the build starts without waiting (WBFT-TIMER-041). The reference implementation can also send it earlier when a build that was started without waiting for a round `r >= 1` of height `h − 1` reads the chain head after block `h − 1` has been imported, and so builds block `h`.
Source: consensus/wbft/core/request.go:33-57, consensus/wbft/backend/engine.go:186-218 (`Seal` posts the request), miner/worker.go:432-441, miner/worker.go:664-669, miner/worker.go:1124 (parent read at build time)
Observable: network

For `r >= 1` the PRE-PREPARE is not triggered by the build: the request handler only stores the new block (`consensus/wbft/core/request.go:47-54`). The PRE-PREPARE is sent while processing any ROUND-CHANGE for the current sequence whose round is at or above the current round (unless that message triggers the F+1 rule), once the quorum condition of `A-05` holds and a proposal is available (a prepared block from the justification, or the stored block; `consensus/wbft/core/roundchange.go:178-186`). The stored block (`pending_request`) is carried across the rounds of a height (`A-05` WBFT-SM-005, `consensus/wbft/core/core.go:287`), and every validator builds a block in round 0 (§8.1). A proposer that has its round-0 block therefore has a proposal when the `Q`-th ROUND-CHANGE for round `r` is processed, and sends the PRE-PREPARE at once. The stored block is the round-0 block, or the block of the round-`r` build (WBFT-TIMER-041) if that build was handed over first, because every handed-over block replaces the stored one (`consensus/wbft/core/request.go:47`).

---

## 9. Expected timeline of a height (informative)

This section combines §5–§8 into the times an observer can predict. Notation: `t0 = Time(h − 1)`, `P = BP(h)`, `T = RT(h)` in seconds, `Tr = round_timeout(config_at(h), r)` in seconds. `T_head` is the wall-clock time at which the node processes the new head `h − 1`. All times are on one node's clock; network delay and execution time are written `ε`.

### 9.1 Round 0, correct proposer

| Event | Time | Rule |
|---|---|---|
| Enter `(h, 0)`, arm round timer (deadline `T_head + T0`), schedule build | `T_head` | WBFT-TIMER-010, WBFT-TIMER-040 |
| All validators build a candidate | `max(T_head, t0 + P)` | WBFT-TIMER-040 |
| Proposer's header timestamp | `max(t0 + P, floor(build time))` | WBFT-TIMER-042 |
| Proposer sends PRE-PREPARE | build time + execution `ε` | WBFT-TIMER-043 |
| Receivers accept, re-arm round timer (deadline `t_accept + T0`), send PREPARE | + network `ε` | WBFT-TIMER-012 |
| PREPARE quorum, COMMIT sent; COMMIT quorum, decision | + 2 network `ε` | `A-05` |
| Block imported, enter `(h + 1, 0)` | + import `ε` | WBFT-TIMER-010 |

Worked example, mainnet preset (`P = 1`, `T = 2`), 7 validators, synchronised clocks. Block `h − 1` has `t0 = 1 000` and is imported at `1 000.35`. The round-0 timer is armed with deadline `1 002.35`; the build is scheduled `0.65 s` later. At `1 001.00` every validator builds; the proposer's header gets `Time = 1 001`; its PRE-PREPARE leaves at about `1 001.05`. Receivers accept at about `1 001.06`, re-arming the timer to `1 003.06`. The decision and import follow within tens of milliseconds, block `h` is imported at about `1 001.15`, and height `h + 1` starts with a `0.85 s` wait. The chain produces one block per second with timestamps `1 000, 1 001, 1 002, ...`.

### 9.2 Round 0, silent proposer

Continuing the example with the proposer of `(h, 0)` offline:

| Time | Event |
|---|---|
| `1 000.35` | Enter `(h, 0)`, deadline `1 002.35` |
| `1 001.00` | Builds; nobody sends a PRE-PREPARE |
| `1 002.35` | Round timer fires on every live validator (about the same time); each enters `(h, 1)`, sends ROUND-CHANGE `(h, 1)`, arms round timer `T1 = 4 s` (deadline `1 006.35`) and retry timer `2 s`, and builds a new candidate immediately (header `Time = max(1 001, 1 002) = 1 002`) |
| `1 002.35 + ε` | F+1 and quorum of ROUND-CHANGE `(h, 1)` observed; the proposer of `(h, 1)` sends its PRE-PREPARE with round-change justification at once, proposing the block it built at `1 001.00` (`Time = 1 001`), or its round-1 block (`Time = 1 002`) if that build was handed over first (§8.3). Only a proposer without a round-0 block waits for the next ROUND-CHANGE it processes for round 1, at the latest its own retry at `1 004.35` (§8.3) |
| before `1 006.35` | PRE-PREPARE accepted, round timer re-armed for `4 s`, decision |

If the round-1 proposer is also silent, round 2 starts at `1 006.35` with `T2 = 8 s`, round 3 at `1 014.35`, and so on (cumulative column of §4.4).

### 9.3 Liveness constraint between `RT` and `BP` (informative)

The round-0 timer starts at `T_head`, but the round-0 proposal is built only at `t0 + P`. A correct round 0 needs

```
T0  >  (t0 + P − T_head) + build + propagation + verification
```

`T_head >= t0` whenever the previous block was agreed after its own timestamp, so `RT` greater than `BP` plus the build, propagation and verification time is sufficient; with `RT <= BP × 1000` round 0 times out unless `T_head − t0` exceeds `BP − RT/1000` plus the build, propagation and verification time, i.e. only heights whose predecessor was agreed late can still finish in round 0. Both network presets use `RT = 2 000 ms` with `BP = 1 s`.

### 9.4 Predictions an observer can check

| Prediction | Holds when | Rules |
|---|---|---|
| Round-0 PRE-PREPARE of `h` is sent at or after `Time(h − 1) + BP(h)` on the proposer's clock | always, except after a late-timeout `CATCH_UP` (`A-05` WBFT-SM-080) and after a round `r >= 1` build of `h − 1` that read the new head | WBFT-TIMER-043 |
| `Time(h) = max(Time(h − 1) + BP(h), second of the proposal build)` | always | WBFT-TIMER-042 |
| First ROUND-CHANGE `(h, 1)` from a node about `T0` after it entered `(h, 0)` | no PRE-PREPARE accepted, no F+1 ROUND-CHANGE messages for higher rounds received, and no retry or round-change expiry armed in height `h − 1` or before a restart processed (`A-05` WBFT-SM-081, WBFT-TIMER-018) | WBFT-TIMER-010, WBFT-TIMER-015 |
| ROUND-CHANGE `(h, r + 1)` about `Tr` after the node entered `(h, r)` | no PRE-PREPARE accepted in round `r` | WBFT-TIMER-015 |
| ROUND-CHANGE `(h, r + 1)` about `Tr` after the node accepted a PRE-PREPARE in round `r` | no view change, including after a COMMIT quorum | WBFT-TIMER-012, WBFT-TIMER-016 |
| Log line `WBFT: broadcast ROUND-CHANGE message` repeats every `RT(h)` for the same round | node stays in the round and accepts no PRE-PREPARE in it | WBFT-TIMER-021 |
| The repeated ROUND-CHANGE is *not* seen on validator links again | caches not evicted, and the retry re-sends the message it armed with: not after a failed `finalize`, the `CATCH_UP` branch or a queued retry processed in a new height (WBFT-TIMER-021, WBFT-TIMER-022, `A-14` §5.8) | WBFT-TIMER-024 |
| A PRE-PREPARE with `Time > receiver's unix_now() + AFBT` produces no PREPARE until the receiver's clock reaches `Time` | the round has not ended by then | WBFT-TIMER-030, WBFT-TIMER-033 |

---

## 10. Implementation notes (informative)

- All three timers are `time.AfterFunc` timers. Their callbacks post an event into the core's `event.TypeMux`; the core's single event loop handles timeout, retry and backlog events, so timer effects are serialised with message processing.
- The round-change timer and the future-PRE-PREPARE timer are protected by `timerMu`; the retry timer is re-armed under `timerMu` as well. `stopTimer` takes `timerMu` itself, so `newRoundChangeTimer` calls it before taking the lock again (`consensus/wbft/core/core.go:356`, `:405`).
- `newRoundChangeTimer` receives a snapshot of the view instead of reading `c.current` (since #82); `newRetrySendingRoundChangeTimer` snapshots the view under `currentMutex.RLock` (`consensus/wbft/core/core.go:426-437`).
- The future-PRE-PREPARE callback reads `c.valSet` at firing time to fill the event's `src` field; the handler ignores `src` (`consensus/wbft/core/preprepare.go:157`, `consensus/wbft/core/handler.go:136-150`).
- The block-period wait is a `time.Sleep` in a goroutine of the miner (`miner/worker.go:436-438`). It is not cancelled by a view change: a round-0 wait that is still sleeping when the node enters round 1 still triggers a build when it ends, which interrupts the round-1 build and starts another one.

---

## 11. Version notes

- `git diff v1.1.0 740526d03 -- consensus/wbft/core` is empty: all timer code is the same in `v1.1.0` and at the reference commit.
- Commit `c37994e9b` (#82) is not an ancestor of the `v1.1.0` tag, but its change is contained in the `v1.1.0` release commit `71e3f820f` (a squash of `dev`). Nodes built from any tag before `v1.1.0` can act on a stale round-change expiry (§5.3 note); those versions are outside this specification.
