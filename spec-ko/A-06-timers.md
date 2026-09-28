# A-06 Timer

- Status: draft
- Area code: `TIMER`
- Reference: go-stablenet `740526d03`. `consensus/wbft/core/core.go` 와 `core/preprepare.go` 의 timer 코드는 reference commit 과 `v1.1.0` (`71e3f820f`) 에서 바이트 단위로 같다. 자세한 내용은 §11 에 있다.

이 장은 WBFT 노드가 시간에 따라 하는 모든 동작을 정한다. 이 장이 다루는 내용은 다음과 같다.

- round change timeout 이 무엇이고 round 마다 어떤 값을 가지는가
- 노드가 그 timer 를 언제 걸고, 언제 다시 걸고, 언제 취소하는가
- timer 가 만료될 때 노드가 무엇을 보내는가
- ROUND-CHANGE 재전송 timer 는 어떻게 동작하는가
- block timestamp 가 미래인 PRE-PREPARE 를 노드가 어떻게 나중에 처리하는가
- 노드가 제안 block 을 만들기 전에 block period 를 어떻게 기다리는가
- proposer 가 header 에 어떤 timestamp 를 쓰는가

장의 끝에서는 한 height 의 예상 시간표를 제시한다. 외부 observer(inspector)는 이 시간표로 메시지가 언제 나타나야 하는지 예측할 수 있다.

메시지 handler 자체, 즉 PRE-PREPARE, ROUND-CHANGE 등이 state 를 어떻게 바꾸는지는 `A-05` 에 있다. 메시지 인코딩은 `A-03` 에 있다. 네트워크에서 메시지를 "보낸다" 는 것이 무엇인지, 즉 노드가 누구에게 보내고 중복을 어떻게 없애는지는 `A-07` 에 있다. 이 장의 §6.4 는 `A-07` 에 의존한다.

---

## 1. 용어

| 용어 | 뜻 |
|---|---|
| *wall clock* | 노드의 실제 시각 시계 `now()` 이다. Unix epoch 부터 흐른 시간을 나노초로 나타낸다. `unix_now()` 는 `floor(now() / 10^9)` 이다. |
| *monotonic clock* | 흐른 시간을 재며 값이 건너뛰지 않는 시계이다. 이 장의 모든 timer 지속 시간은 이 시계로 잰다. |
| *view* | `(sequence, round)` 이다(`A-01` 참고). `h` 는 합의 중인 sequence(height)를, `r` 은 round 를 뜻한다. |
| *view 진입* | 노드가 `A-05` 의 "start new round" 절차(`start_new_round`)를 실행해서 현재 view 가 바뀌는 순간이다. |
| *ROUND-CHANGE broadcast 절차* | `A-05` 의 `broadcast_round_change(node, round)` 이다 (WBFT-SM-051, WBFT-SM-052). |
| `RT(h)` | `config_at(h).request_timeout` (`A-01` §6.1) 이다. height `h` 에 적용되는 request timeout 을 밀리초로 나타낸다 (§3). |
| `MRT(h)` | `config_at(h).max_request_timeout_seconds` 이다. height `h` 에 적용되는 round timeout 상한을 초로 나타내며, `0` 은 "상한 없음" 을 뜻한다. |
| `BP(h)` | `config_at(h).block_period` 이다(초 단위). |
| `AFBT` | 기본 설정의 `allowed_future_block_time` 이다(초 단위). transition 의 영향을 받지 않는다 (§3). |
| `Time(b)` | block `b` 의 header 에 있는 `Time` 필드(Unix 초)이다. |

---

## 2. 시계에 대한 가정

WBFT 노드는 두 시계를 쓰며, 프로토콜은 두 시계에 서로 다른 가정을 둔다.

지속 시간은 monotonic clock 으로 잰다. 여기에는 round change timeout, retry 간격, 한 번 계산된 뒤의 미래 제안 대기 시간, 한 번 계산된 뒤의 block period 대기 시간이 포함된다. timer 가 도는 동안 wall clock 이 건너뛰어도 timer 는 짧아지거나 길어지지 않는다.

절대 시각은 wall clock 에서 얻고, 정수 Unix 초인 header timestamp 와 비교한다. 이런 비교가 쓰이는 곳은 세 군데이다. block period 대기(§8.1)는 header 에서 얻은 Unix 초와 `now()` 의 차이로 계산한다. proposer 는 header 에 `unix_now()` 를 쓴다(§8.2). 받는 노드는 제안의 `Time` 을 `unix_now()` 와 비교해서 그 제안이 "미래에서 왔는지" 를 판단한다(§7).

[WBFT-TIMER-001] 노드는 이 장의 모든 timer 지속 시간을 반드시 monotonic clock 으로 재야 한다. wall clock 으로 대기 시간을 한 번 계산했다면(§7, §8.1), 대기 중에 wall clock 이 바뀌어도 노드는 그 대기 시간을 다시 계산해서는 안 된다.
Source: consensus/wbft/core/core.go:411 (`time.AfterFunc`), consensus/wbft/core/core.go:446, consensus/wbft/core/preprepare.go:156, miner/worker.go:436-438 (`time.Sleep(waitTime)`)
Observable: log

프로토콜은 validator 들의 wall clock 이 서로 가깝게 맞을 때에만 liveness 를 가진다. reference 구현에는 시계 동기화 장치도 없고 시계 차이의 한도를 검사하는 코드도 없다. 아래 가정은 참고용이며, 가정이 깨졌을 때 무엇이 잘못되는지를 설명한다.

- *proposer 시계가 받는 노드보다 `δ` 초 빠른 경우.* 받는 노드는 `Time(proposal) > unix_now() + AFBT` 를 보고 PRE-PREPARE 처리를 최대 `δ` 만큼 미룬다(§7). 미룬 시간이 남은 round timeout 보다 길면, 받는 노드는 제안을 받아들이지 못한 채 round 를 바꾼다.
- *proposer 시계가 느린 경우.* header timestamp 는 여전히 `Time(parent) + BP(h)` 이상이므로(§8.2) 받는 노드는 제안을 받아들인다. 제안이 실제 시간으로 조금 늦게 만들어질 뿐이다.
- *받는 노드의 시계가 느린 경우.* 이 경우의 효과는 proposer 시계가 빠른 경우와 같다.
- 기본값 `AFBT = 0` 에서는 받는 노드의 시계가 proposer 의 시계보다, 제안을 만들고 전달하는 데 걸리는 시간(보통 1 초보다 훨씬 짧다)보다 더 많이 늦기만 해도, 받는 노드는 `Time(h − 1) + BP(h)` 에 만들어진 round 0 PRE-PREPARE 를 자기 시계가 그 초에 도달할 때까지 미룬다. 운영자는 validator 시계의 차이를 1 초보다 훨씬 작게 유지하는 것이 좋다(SHOULD). 예를 들어 NTP 를 쓸 수 있다.

---

## 3. 어느 설정이 적용되는가

view `(h, r)` 의 timer 는 height `h` 의 합의 설정을 읽는다. 여기서 `h` 는 parent 가 아니라 합의 중인 height 이다. 설정 transition(`B-01`)은 번호가 transition 의 block 번호 이상인 첫 height 부터 효력을 가진다.

[WBFT-TIMER-002] view `(h, r)` 의 모든 timer(round change timer, retry timer, block period 대기)에 대해 노드는 반드시 `config_at(h)` 의 `RT(h)`, `MRT(h)`, `BP(h)` 를 써야 한다. 이때 다음 규칙을 따른다.

1. block 번호가 `B` 인 transition 은 `h >= B` 인 모든 height 에 적용된다.
2. 나중 transition 은 앞선 transition 을 필드 단위로 덮어쓴다. transition 은 engine 을 만들 때 block 번호순으로 정렬된다(`B-01` §6.2).
3. transition 의 `requestTimeoutSeconds` 나 `blockPeriodSeconds` 필드가 `0` 이면 이전 값이 그대로 남는다. transition 에 `maxRequestTimeoutSeconds` 필드가 없을 때에도 이전 값이 그대로 남는다.

Source: consensus/wbft/config.go:161-192 (`GetConfig`, `getTransitionValue`), consensus/wbft/core/core.go:364, consensus/wbft/core/core.go:439, consensus/wbft/engine/engine.go:482-484 (`PeriodToNextBlock`), eth/ethconfig/config.go:254-262 (sort)
Observable: network, log

[WBFT-TIMER-003] `RT(h)` 는 반드시 genesis 의 `anzeon.wbft` 섹션 또는 적용되는 가장 최근 transition 에서 가져온 `requestTimeoutSeconds × 1000` (밀리초) 이어야 한다. `maxRequestTimeoutSeconds` 를 명시적으로 `0` 으로 설정한 transition 은 반드시 그 height 부터 상한을 없애야 한다.
Source: eth/ethconfig/config.go:215-217, eth/ethconfig/config.go:231-233, consensus/wbft/config.go:165-168, consensus/wbft/config.go:178-180, params/config_wbft.go:186-198
Observable: network

genesis 검증은 `requestTimeoutSeconds = 0` 과 `blockPeriodSeconds = 0` 을 거부한다(`params/config_wbft.go:120-125`). 그러므로 이 검증을 통과한 모든 체인에서 `RT(h) >= 1000` ms 이고 `BP(h) >= 1` s 이다. transition 은 같은 방식으로 검증되지 않지만, transition 값 `0` 은 "바꾸지 않음" 을 뜻하므로 하한은 여전히 성립한다.

[WBFT-TIMER-004] `AFBT` 는 반드시 기본 설정(`anzeon.wbft.allowedFutureBlockTime`, 기본값 `0`)에서 가져와야 한다. transition 객체에 이 필드가 있더라도 transition 이 `AFBT` 를 바꿔서는 안 된다.
Source: eth/ethconfig/config.go:224-226, consensus/wbft/config.go:161-184 (field not copied), consensus/wbft/engine/engine.go:202
Observable: network

구현 노트 (참고). `wbft.DefaultConfig` 의 `RequestTimeout` 은 1000 ms 이지만 노드는 이 값을 쓰지 않는다. `CreateConsensusEngine` 은 값이 0 인 `wbft.Config` 에서 시작해서 chain config 의 값을 복사해 넣기 때문이다(`eth/ethconfig/config.go:193-194`). 그러므로 §4.4 표의 "1000 ms" 설정은 암묵적 기본값이 아니라 `requestTimeoutSeconds = 1` 을 뜻한다.

---

## 4. Round timeout

### 4.1 정의

`round_timeout(config, round)` 는 round 가 `round` 인 view 의 round change timer 지속 시간을 나노초로 돌려준다. 이 함수는 reference 구현의 64비트 연산을 넘침(wrap-around)까지 그대로 재현한다. 결과 값이 ROUND-CHANGE 메시지를 보내는 시점을 정하고, 그래서 외부에서 관측되기 때문이다.

```python
MAX_INT64 = 2**63 - 1
NS_PER_MS = 1_000_000
NS_PER_S  = 1_000_000_000

def wrap_int64(x: int) -> int:
    """int64 로의 2의 보수 축소 (Go 의 부호 있는 정수 넘침 의미)."""
    x &= 2**64 - 1
    return x - 2**64 if x >= 2**63 else x

def round_timeout(config: Config, round: Round) -> int:   # 나노초, int64
    r    = round % 2**64                                              # big.Int.Uint64()
    base = wrap_int64(config.request_timeout * NS_PER_MS)          # time.Duration(ms) * time.Millisecond
    cap  = wrap_int64(config.max_request_timeout_seconds * NS_PER_S)  # time.Duration(s) * time.Second

    if cap > 0:                                   # 상한 분기
        t = base
        for _ in range(r):
            t = wrap_int64(t * 2)
            if t > cap:
                t = cap
                break
        if t < base:                              # "넘침" 검사 (§4.2 참고)
            t = cap
        return t
    else:                                         # 상한 없는 분기 (cap == 0 이거나 cap 이 음수로 넘친 경우)
        f = float64(2.0 ** r) * float64(base)     # IEEE-754 binary64; r >= 1024 이면 2.0**r 은 +Inf
        if isnan(f) or isinf(f) or f > float64(MAX_INT64):
            return MAX_INT64
        return int64(f)                           # 0 쪽으로 버림
```

`float64(MAX_INT64)` 는 반올림되어 `2^63` 이 된다. `base > 0` 이면 `base` 는 `10^6` 의 배수라서 2 의 거듭제곱이 아니므로 `f` 는 `2^63` 과 정확히 같아질 수 없다. 그러므로 이때 `int64(f)` 변환은 범위 안에 있다. `base` 가 음수로 넘친 경우(`9 223 372 037 <= requestTimeoutSeconds <= 18 446 744 073`)에는 `f` 가 `−2^63` 보다 작아질 수 있다. 이때 Go 의 변환 결과는 구현마다 정해지며(arm64 와 amd64 에서는 `MinInt64` 이다), 의사코드의 `int64(f)` 는 범위를 벗어난, 버림한 값을 그대로 돌려준다. 음수 지속 시간이면 timer 는 곧바로 만료되므로, 관측되는 동작은 같다.

[WBFT-TIMER-005] view `(h, r)` 에 거는 round change timer 는 반드시 §4.1 에서 정의한 `round_timeout(config_at(h), r)` 을 지속 시간으로 가져야 한다.
Source: consensus/wbft/core/core.go:363-402
Observable: network, log

[WBFT-TIMER-006] 상한 분기에서 노드는 `base > cap` 인 경우에도 반드시 `base` 를 round 0 timeout 으로 써야 한다. 상한은 두 배로 늘리는 반복 안에서만 적용되고, round 0 에서는 그 반복이 실행되지 않기 때문이다. 그 결과 `base > cap` 이면 `round_timeout` 은 단조 증가하지 않는다. round 0 은 `base` 동안 지속되고, 그 뒤의 모든 round 는 `cap` 동안 지속된다.
Source: consensus/wbft/core/core.go:374-382
Observable: network, log

[WBFT-TIMER-007] 상한 분기가 `t < base` 로 끝나면 노드는 반드시 `cap` 을 timeout 으로 써야 한다. 이 경우 reference 구현은 `WBFT: Possible request timeout overflow detected, setting timeout value to maxRequestTimeout` (Warn) 을 로그에 남긴다. `base > cap` 이면 실제로 넘침이 일어나지 않았는데도 `r >= 1` 인 모든 round 에서 이 로그가 남는다.
Source: consensus/wbft/core/core.go:383-390
Observable: log

[WBFT-TIMER-008] 상한 없는 분기에서 binary64 연산으로 계산한 `2^r × base` 가 NaN 이거나 무한대이거나 `2^63` 보다 크면, 노드는 반드시 timeout 을 `MAX_INT64` 나노초로 고정해야 하고 `WBFT: Timeout overflow detected, setting timeout value to MaxInt64` (Warn) 을 로그에 남겨야 한다. `MAX_INT64` 나노초는 약 292.47 년이므로, 이 timer 는 실제로 만료되지 않는다.
Source: consensus/wbft/core/core.go:391-402
Observable: log

genesis 검증을 통과하고 현실적인 상한(`base <= cap <= 2^62` ns, 즉 `maxRequestTimeoutSeconds <= 4 611 686 018`)을 가진 모든 설정에서 §4.1 은 다음 닫힌 식으로 줄어든다.

```
round_timeout = base                         if r == 0
              = min(base × 2^r, cap)         if r >= 1 and cap > 0
              = min(base × 2^r, MAX_INT64)   if cap == 0
```

§4.1 의 넘침 경로는 터무니없는 매개변수(`requestTimeoutSeconds > 9 223 372 036` 또는 `maxRequestTimeoutSeconds > 4 611 686 018`)에서만 도달한다. 그 가운데 하나는 알아 둘 만하다. `9 223 372 037 <= maxRequestTimeoutSeconds <= 18 446 744 073` 인 상한은 음수 `cap` 으로 넘치고, 노드는 아무 표시 없이 상한이 없는 것처럼 동작한다(예제 5). 그보다 큰 값은 `2^64` 를 법으로 다시 넘치므로 작은 양수 상한이 될 수 있다. 예를 들어 `18 446 744 074` 는 `cap = 290 448 384` ns 가 된다.

구현 노트 (참고). 두 배로 늘리는 반복은 최대 `r` 번 돌고, 값이 처음으로 `cap` 을 넘으면 멈춘다. `cap > 2^62` 이고 `t` 가 넘쳐서 정확히 `0` 이 되면 반복은 더 이상 멈추지 않고 `r` 번을 모두 돈다. 실제로 `r` 은 한정되어 있다. round 는 하나씩 올라가거나, `current + 10` 으로 제한된 F+1 점프로만 올라가기 때문이다(`A-05`, `consensus/wbft/core/backlog.go:101-110`).

### 4.2 계산 예제

1. mainnet preset(`requestTimeoutSeconds = 2`, 상한 없음)에서 `r = 3` 인 경우이다. `base = 2 000 000 000` ns 이다. 상한 없는 분기에서 `f = 8 × 2·10^9 = 1.6·10^10` 이므로 결과는 **16 s** 이다.
2. `requestTimeoutSeconds = 2`, `maxRequestTimeoutSeconds = 10` 이고 `r = 3` 인 경우이다. 상한 분기에서 `t` 는 `2 s`, `4 s`, `8 s`, `16 s` 로 늘어난다. `16 s > 10 s` 이므로 `t = 10 s` 로 두고 반복을 멈춘다. `10 s >= 2 s` 이므로 결과는 **10 s** 이다.
3. `--dev` preset 은 `requestTimeoutSeconds = 1000`, `maxRequestTimeoutSeconds = 4` 를 쓴다. 그래서 `base = 1 000 s`, `cap = 4 s` 이다. `r = 0` 이면 반복이 돌지 않아 `t = 1 000 s` 이고, `t < base` 가 아니므로 결과는 **1 000 s** 이다. `r = 1` 이면 `t = 2 000 s > 4 s` 이므로 `t = 4 s` 로 두고 반복을 멈춘다. `4 s < 1 000 s` 이므로 넘침 검사가 걸려 Warn 로그가 남고, 결과는 **4 s** 이다. `r >= 1` 인 모든 round 에서 결과는 **4 s** 이고 Warn 로그가 남는다.
4. mainnet preset 에서 `r = 33` 인 경우이다. `f = 2^33 × 2·10^9 = 1.718·10^19 > 2^63` 이므로 결과는 **`MAX_INT64` ns** 이고 Warn 로그가 남는다. `r = 32` 에서는 여전히 `8 589 934 592 000 000 000` ns(약 272.2 년)가 나온다.
5. `requestTimeoutSeconds = 2`, `maxRequestTimeoutSeconds = 10 000 000 000` 인 경우이다. `cap = wrap_int64(10^19) = -8 446 744 073 709 551 616` 이다. 이 값은 `> 0` 이 아니므로 노드는 상한 없는 분기로 가고, 상한을 설정하지 않은 것처럼 동작한다.

### 4.3 Preset

reference 구현(`params/config.go`)에 컴파일되어 들어 있는 preset 은 다음과 같다.

| Preset | `requestTimeoutSeconds` | `RT` (ms) | `maxRequestTimeoutSeconds` | 분기 |
|---|---|---|---|---|
| `StableNetMainnetChainConfig` | 2 | 2 000 | 없음 (0) | 상한 없음 |
| `StableNetTestnetChainConfig` | 2 | 2 000 | 없음 (0) | 상한 없음 |
| `AllDevChainProtocolChanges` (`--dev`) | 1 000 | 1 000 000 | 4 | 상한 있음, `base > cap` |
| `TestWBFTChainConfig` | 1 000 | 1 000 000 | 4 | 상한 있음, `base > cap` |

Source: params/config.go:68-72, params/config.go:172-176, params/config.go:404, params/config.go:424-429, params/config.go:630-635

운영 preset 8282 와 8283 은 `requestTimeoutSeconds = 2` 를 쓴다 (`params/config.go:70`, `params/config.go:174`).

### 4.4 Timeout 표

표의 값은 그 행에 적힌 round 의 `round_timeout` 이고, 그 옆 칸은 PRE-PREPARE 를 한 번도 받아들이지 않았을 때 round 0 에 들어간 시점부터 round `r` 에 들어간 시점까지의 누적 시간 `Σ_{i<r} round_timeout(i)` 이다. PRE-PREPARE 를 받아들여서 timer 를 다시 걸면(§5.2) round 는 이 값보다 길어진다.

`RT = 2 000 ms` (mainnet, testnet):

| Round `r` | 상한 없음 | 누적 | 상한 4 s | 누적 | 상한 10 s | 누적 | 상한 60 s | 누적 |
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

| Round `r` | 상한 없음 | 누적 | 상한 4 s | 누적 | 상한 10 s | 누적 |
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

`--dev` preset 과 test preset 처럼 `RT = 1 000 000 ms` 이고 상한이 4 s 이면, round 0 은 1 000 s 이고 `r >= 1` 인 모든 round 는 4 s 이다. `r >= 1` 인 round 마다 WBFT-TIMER-007 의 Warn 로그가 함께 남는다. retry 간격(§6)은 1 000 s 이다.

> 해설: 두 네트워크 preset 은 상한이 없으므로, proposer 가 연달아 멈추는 상황이 길어지면 복구가 매우 느려진다. round 10 은 34 분이 넘고, round 33 부터는 timer 가 사실상 만료되지 않는다. 운영자는 `maxRequestTimeoutSeconds` 를 설정하는 것을 검토할 만하다. 다만 단점이 두 가지 있다. 첫째, 이 값은 모든 노드가 같게 써야 하는 값(WBFT-PARAM-060)이므로 운영 중인 네트워크에서는 transition 으로 한꺼번에 바꿔야 한다. 둘째, 상한에 도달한 뒤에는 round 가 올라가도 timeout 이 길어지지 않으므로, 상한보다 오래 걸리는 합의(예: 아주 큰 block)는 round 를 바꿔도 계속 실패한다.

---

## 5. Round change timer

consensus engine 을 실행하는 노드는 언제나 걸려 있는 round change timer 를 최대 하나만 가지며, 그 timer 는 현재 view 에 속한다.

### 5.1 view 에 들어갈 때 timer 를 건다

[WBFT-TIMER-010] 노드가 view `(h, r)` 에 들어갈 때마다, 노드는 반드시 걸려 있는 round change timer 를 취소하고 지속 시간이 `round_timeout(config_at(h), r)` 인 새 timer 를 걸어야 한다. 새 view 는 `r = 0` 인 새 sequence 일 수도 있고 현재 sequence 의 새 round 일 수도 있다. 이 지속 시간은 view 에 들어간 순간부터 잰다.
Source: consensus/wbft/core/core.go:163-274 (`startNewRound`; timer armed at 268-271), consensus/wbft/core/core.go:355-415
Observable: network, log

노드가 어느 view 에 언제 들어가는지는 `A-05` 가 정한다. 노드는 다음 네 가지 경우에 view 에 들어간다.

1. engine 이 시작될 때(`consensus/wbft/core/handler.go:44`)
2. 새 chain head 가 생길 때(`consensus/wbft/core/final_committed.go:27-31`)
3. round change timer 가 만료될 때(§5.4)
4. 더 높은 round 에 대한 ROUND-CHANGE 메시지를 F+1 개 받을 때(`consensus/wbft/core/roundchange.go:161-169`)

[WBFT-TIMER-011] "start new round" 절차가 view 를 바꾸지 않고 끝나면, 노드는 어떤 timer 도 건드려서는 안 된다. 이때 걸려 있는 round change timer, retry timer, 미래 PRE-PREPARE timer 는 모두 그대로 계속 돈다. 절차가 view 를 바꾸지 않고 끝나는 경우는 다음 세 가지이다.

1. 목표 round 가 `0` 인데 chain head 가 아직 `h - 1` 이다.
2. 목표 round 가 현재 round 보다 낮다.
3. chain head 가 현재 sequence 보다 둘 이상 뒤처져 있다.

Source: consensus/wbft/core/core.go:196-209
Observable: network, log

### 5.2 PRE-PREPARE 를 받아들일 때 timer 를 다시 건다

[WBFT-TIMER-012] 노드가 현재 view `(h, r)` 의 PRE-PREPARE 를 받아들이면, 노드는 PREPARE 를 보내기 전에 반드시 걸려 있는 round change timer 를 취소하고 전체 지속 시간 `round_timeout(config_at(h), r)` 을 가진 새 timer 를 걸어야 한다. 이 지속 시간은 받아들인 순간부터 잰다. 여기서 "받아들인다" 는 것은 노드가 `AcceptRequest` state 에 있는 동안 그 PRE-PREPARE 가 `A-05` 의 모든 검사를 통과했다는 뜻이다.
Source: consensus/wbft/core/preprepare.go:171-193
Observable: network, log

그러므로 round `r` 의 마감 시각은 PRE-PREPARE 를 받아들이지 않았으면 `t_enter(h, r) + round_timeout(r)` 이고, 받아들였으면 `t_accept(h, r) + round_timeout(r)` 이다. `t_accept >= t_enter` 이므로 한 round 가 timeout 되기 전까지 지속되는 시간은 `round_timeout(r)` 이상이고 `(t_accept − t_enter) + round_timeout(r)` 이하이다. round 0 에서는 block period 대기(§8.1)가 `t_accept − t_enter` 에 포함된다.

round change timer 를 걸거나 다시 거는 동작(WBFT-TIMER-010, WBFT-TIMER-012)은 하나의 절차로 구현되며, 그 절차는 먼저 core 의 세 timer 를 모두 멈춘다. 다음 요구사항은 이 부수 효과를 정한다.

[WBFT-TIMER-013] 노드가 round change timer 를 걸거나 다시 걸 때마다, 노드는 반드시 ROUND-CHANGE retry timer(§6)와 미래 PRE-PREPARE timer(§7)도 취소해야 한다.
Source: consensus/wbft/core/core.go:336-347 (`stopTimer`), consensus/wbft/core/core.go:356
Observable: network

### 5.3 취소

이미 만료된 timer 는 취소되기 전에 event 를 queue 에 넣었을 수 있다. reference 구현은 round change timer 마다 고유한 `canceled` 표시를 둔다. timer 를 취소하면 그 표시가 설정되고, event handler 는 표시가 설정된 event 를 버린다.

[WBFT-TIMER-014] core 의 한 실행 안에서(engine 시작부터 그다음 정지까지), 이미 취소되었거나 더 새로운 round change timer 로 대체된 round change timer 가 만료되면, 그 만료는 반드시 아무 효과도 없어야 한다. 만료 event 가 취소 전에 생겼든 뒤에 생겼든, 노드는 그 만료 때문에 view 를 바꿔서는 안 되고 아무것도 보내서도 안 된다. 이전 실행의 timer 는 WBFT-TIMER-018 이 다룬다.
Source: consensus/wbft/core/core.go:331-335, consensus/wbft/core/core.go:405-414, consensus/wbft/core/handler.go:152-160
Observable: network, log

구현 노트 (참고). commit `c37994e9b` (#82, "race conditions in newRoundChangeTimer") 이후로 표시의 포인터는 timer 의 closure 에 붙잡혀 있다. 이 수정 전에는 closure 가 만료 시점에 core 의 *현재* 표시를 읽었다. 그래서 옛 timer 의 만료가 취소되지 않은 새 timer 의 표시를 읽고 의도하지 않은 round change 를 일으킬 수 있었다. 표시는 `timerMu` 를 잡은 채 쓰이고, event loop 는 `timerMu` 를 잡지 않고 표시를 읽는다.

### 5.4 만료

[WBFT-TIMER-015] view `(h, r)` 의 유효한(취소되지 않은) round change timer 가 만료되면, 노드는 반드시 다음 순서로 동작해야 한다.

1. 노드는 목표 round `r + 1` 로 `A-05` 의 "start new round" 절차를 실행한다.
2. 노드는 목표 round `r + 1` 로 ROUND-CHANGE broadcast 절차(§6.1)를 실행한다.

1단계가 view `(h, r + 1)` 에 들어가면, 노드는 그 결과로 `(h, r + 1)` 의 ROUND-CHANGE 를 보내고, 지속 시간이 `round_timeout(config_at(h), r + 1)` 인 새 round change timer 와 retry timer(§6)를 걸어 둔 상태가 된다.
Source: consensus/wbft/core/handler.go:250-263 (`handleTimeoutMsg`), consensus/wbft/core/roundchange.go:52-98
Observable: network, log
Observed as: the log lines `WBFT: TIMER CHANGING ROUND` (Warn) and `WBFT: broadcast ROUND-CHANGE message` (Info).

이 timer 는 "PRE-PREPARE 를 받지 못한 경우" 에만 쓰이는 것이 아니다. 노드의 state 와 관계없이 timeout 안에 view 가 바뀌지 않으면 timer 는 만료된다. 특히 COMMIT quorum 에 도달해도 timer 는 멈추지 않는다.

[WBFT-TIMER-016] 노드는 COMMIT quorum 에 도달했을 때, `Committed` state 에 들어갔을 때, 또는 decide 한 block 을 application 에 넘겼을 때 round change timer 를 취소해서는 안 된다. round change timer 는 노드가 다음 view 로 바꿀 때(WBFT-TIMER-010), PRE-PREPARE 를 받아들일 때(WBFT-TIMER-012), engine 을 멈출 때(WBFT-TIMER-018)에만 취소된다.
Source: consensus/wbft/core/commit.go:123-126, consensus/wbft/core/commit.go:137-179 (`commitWBFT` only sets the state), consensus/wbft/core/core.go:336-347
Observable: network

timer 가 만료될 때 decide 한 block 이 어디에 있느냐에 따라 관측되는 결과가 달라진다. 노드가 decide 한 view 를 `(h, r)` 이라 하자.

- **경우 A: block 이 아직 chain head 가 아니다.** head 가 아직 `h − 1` 이므로 WBFT-TIMER-015 의 1단계는 `(h, r + 1)` 에 들어간다. 노드는 `(h, r + 1)` 의 ROUND-CHANGE 를 보내며, 이 ROUND-CHANGE 에는 노드의 prepared round `r`, prepared block, PREPARE justification 이 실린다. 그 block 이 head 가 되면 노드는 정상적으로 `(h + 1, 0)` 에 들어간다. 이 경우는 다음 header 에 부수 효과를 남긴다. 노드의 prior round 가 `r` 이 아니라 `r + 1` 이 되므로, 노드는 `(h, r)` 의 늦게 도착한 PREPARE/COMMIT seal 을 더 이상 extra seal 로 모으지 않는다(`A-05`, `consensus/wbft/core/backlog.go:160-162`, `consensus/wbft/core/priorstate.go:35-45`). 이 노드가 `h + 1` 을 제안하면, 그 header 의 이전 seal 집합에는 block `h` 의 quorum seal 만 들어간다.
- **경우 B: block 은 head 가 되었지만, 노드가 그 block 에 대한 new head event 를 아직 처리하지 않았다.** 1단계는 `A-05` 의 "catch up" 경로를 타서 `(h + 1, 0)` 에 들어가고, 지속 시간이 `round_timeout(config_at(h + 1), 0)` 인 새 round change timer 를 건다. 그러나 목표 round 가 `0` 이 아니므로, 노드는 height `h` 의 round change 메시지 집합과 저장된 PREPARE justification 을 초기화하지 않는다. 이어서 2단계는 view `(h + 1, r + 1)` 의 ROUND-CHANGE 에 서명해서 보낸다. 이 ROUND-CHANGE 에는 prepared round 도 prepared block 도 없다. 다만 노드가 height `h` 에서 prepared 상태였다면, 낡은 `justification` 필드, 즉 height `h` 의 PREPARE certificate 가 실린다. 그 뒤에 처리되는 new head event 는 head 가 `h` 인 상태에서 목표 round `0` 으로 절차를 부르므로 아무것도 하지 않는다(WBFT-TIMER-011).

[WBFT-TIMER-017] 노드의 chain head 가 `h` 로 올라간 뒤, 노드가 new head event 를 처리해서 `(h + 1, 0)` 에 들어가기 전에 `(h, r)` 의 round change timer 가 만료되면, 노드는 반드시 `(h + 1, 0)` 에 들어가야 하고, 이어서 반드시 view 가 `(h + 1, r + 1)` 인 ROUND-CHANGE 를 보내야 한다. 이 view 의 sequence 는 새 height 이고 round 는 옛 height 의 round 로 계산한 값이다.
Source: consensus/wbft/core/handler.go:250-263, consensus/wbft/core/core.go:185-195, consensus/wbft/core/core.go:231-240, consensus/wbft/core/core.go:250-258, consensus/wbft/core/roundchange.go:57-63
Observable: network

경우 A 는 block insert 가 남은 timeout 보다 오래 걸릴 때 생긴다. 여기서 남은 timeout 은 노드가 PRE-PREPARE 를 받아들인 순간부터 센 `round_timeout(r)` 이다. 경우 B 는 노드가 chain head 를 갱신한 뒤 new head event 를 처리하기 전의 짧은 구간에 timer 가 만료될 때 생긴다.

### 5.5 Engine 시작과 정지

[WBFT-TIMER-018] consensus engine 이 정지하면, 노드는 반드시 round change timer, retry timer, 미래 PRE-PREPARE timer 를 취소해야 하고, engine 이 멈춰 있는 동안에는 timer 때문에 어떤 합의 메시지도 보내서는 안 된다. reference 구현에서는 `Stop` 이 `stopTimer` 를 부른 뒤 구독을 해제하기 전까지의 구간에 event loop 가 건 timer 가 취소되지 않는다. 이 timer 의 만료는 engine 이 멈춰 있는 동안에는 아무 효과도 없다. 그러나 engine 이 다시 시작된 뒤에 만료되면, 새 core 는 그 만료를 자기 것으로 처리한다. 낡은 round change 만료는 새 core 가 round 를 바꾸고 ROUND-CHANGE 를 보내게 한다. 낡은 retry 만료는 ROUND-CHANGE 를 보내게 한다. 낡은 미래 PRE-PREPARE 만료는 그 PRE-PREPARE 를 다시 처리하게 한다.
Source: consensus/wbft/core/handler.go:50-59 (`Stop`: `stopTimer`, then unsubscribe), consensus/wbft/backend/engine.go:288-300, consensus/wbft/backend/backend.go:355-367 (a new core on the same event mux), event/event.go:204-216, consensus/wbft/core/handler.go:152-178
Observable: network

engine 이 시작되면 노드는 view `(head + 1, 0)` 에 들어가고(`consensus/wbft/core/handler.go:35-47`), WBFT-TIMER-010 에 따라 round change timer 를 건다. reference 구현은 downloader 가 동기화를 시작하면 engine 을 멈추고, 동기화가 끝나면 다시 시작한다(`miner/miner.go:139-161`). 다만 이 동작은 프로세스가 처음으로 동기화에 성공할 때까지만 일어나고, 그 뒤의 동기화는 engine 을 멈추지 않는다(`B-09` SNET-SYNC-030). engine 을 다시 시작할 때마다 round 0 timer 가 새로 시작된다. 이전 실행의 낡은 timer 는 WBFT-TIMER-018 을 참고한다.

---

## 6. ROUND-CHANGE retry timer

### 6.1 Timer 걸기

노드는 다음 네 가지 경우에 ROUND-CHANGE broadcast 절차(`A-05`)를 부른다.

1. round change timer 가 만료되었을 때(§5.4)
2. 더 높은 round 에 대한 ROUND-CHANGE 메시지를 F+1 개 받았을 때
3. decide 한 block 을 application 에 넘기는 데 실패했을 때
4. retry timer 가 만료되었을 때(§6.2)

이 절차는 어떤 검사보다도 먼저 retry timer 를 건다.

[WBFT-TIMER-020] ROUND-CHANGE broadcast 절차는 불릴 때마다 반드시 먼저 걸려 있는 retry timer 를 취소하고, 지속 시간이 `RT(h)` 밀리초인 새 timer 를 걸어야 한다. 이 지속 시간은 두 배로 늘리지 않고 상한도 적용하지 않는다. 여기서 `(h, r_c)` 는 그 순간 노드의 현재 view 이며, 새 timer 는 반드시 `r_c` 를 기억해야 한다.
Source: consensus/wbft/core/roundchange.go:52-53, consensus/wbft/core/core.go:426-450, consensus/wbft/core/roundchange.go:40-43, consensus/wbft/core/roundchange.go:161-169, consensus/wbft/core/commit.go:173-176, consensus/wbft/core/handler.go:170-178
Observable: log

절차가 결국 아무것도 보내지 않더라도 retry timer 는 걸린다. 아무것도 보내지 않는 경우는 목표 round 가 현재 round 보다 낮을 때와 노드가 validator 집합에 없을 때(`A-07` WBFT-NET-030)이다.

### 6.2 만료

[WBFT-TIMER-021] round `r_c` 를 기억하는 retry timer 가 만료되면, 노드는 반드시 목표 round `r_c` 로 ROUND-CHANGE broadcast 절차를 불러야 한다. 노드의 현재 round 가 `r_c` 보다 크지 않으면, 이 절차는 `(현재 sequence, r_c)` 의 ROUND-CHANGE 를 새로 만들고 다시 서명해서 broadcast 한다. 이 ROUND-CHANGE 에는 노드의 현재 prepared round, prepared block, PREPARE justification 이 실린다. 그리고 절차는 WBFT-TIMER-020 에 따라 retry timer 를 다시 건다. 현재 round 가 `r_c` 보다 크지 않은 경우는 두 가지이다. 하나는 현재 round 가 여전히 `r_c` 인 경우이고, 다른 하나는 노드가 새 height 에 들어간 뒤에 queue 에 있던 만료를 처리하는 경우이다.
Source: consensus/wbft/core/handler.go:170-178, consensus/wbft/core/roundchange.go:52-98
Observable: log
Observed as: the log line `WBFT: broadcast ROUND-CHANGE message` (Info), every `RT(h)`.

[WBFT-TIMER-022] retry timer 가 만료될 때 노드의 현재 round 가 `r_c` 보다 크면, 노드는 ROUND-CHANGE 를 보내서는 안 되지만, 그래도 반드시 현재 view 에 대한 새 retry timer 를 걸어야 한다(WBFT-TIMER-020).
Source: consensus/wbft/core/roundchange.go:53-61
Observable: log
Observed as: the log line `WBFT: invalid past target round` (Warn).

retry 만료에는 취소 표시가 없다. 그래서 retry timer 가 취소되기 전에 queue 에 들어간 만료는 그대로 처리된다. 그 사이에 노드가 같은 height 의 더 높은 round 에 들어갔다면, 노드는 아무것도 보내지 않는다(WBFT-TIMER-022). 그 사이에 노드가 새 height 에 들어갔다면, 노드의 round `0` 은 `r_c` 보다 크지 않으므로 노드는 `(새 sequence, r_c)` 의 ROUND-CHANGE 에 서명해서 보낸다. 이 ROUND-CHANGE 에는 노드의 현재 prepared round 와 prepared block 이 실리며, 둘 다 비어 있다. 두 경우 모두 노드는 현재 view 에 대한 retry timer 를 다시 건다. 그 timer 는 `RT(h)` 뒤에 만료되고, 그 사이에 취소되지 않았다면 현재 round 의 ROUND-CHANGE 를 보낸다.

[WBFT-TIMER-023] retry timer 는 반드시 round change timer 를 걸거나 다시 걸 때(WBFT-TIMER-013) 또는 engine 을 멈출 때(WBFT-TIMER-018)에만 취소되어야 한다. PREPARE quorum 이나 COMMIT quorum 에 도달했다고 해서 retry timer 가 취소되어서는 안 된다.
Source: consensus/wbft/core/core.go:336-347, consensus/wbft/core/core.go:417-422
Observable: log

그러므로 노드가 retry timer 를 걸 때의 round 에 머물고 그 round 에서 PRE-PREPARE 를 받아들이지 않는 동안, 재전송은 `RT(h)` 마다 계속된다. 이 round 가 ROUND-CHANGE 의 목표 round 와 같을 필요는 없다(`A-05` WBFT-SM-052).

> 해설: 이 재전송의 의도는 유실된 ROUND-CHANGE 를 복구하고, 늦게 연결된 validator 가 따라잡게 하는 것이다. 그러나 §6.4 에서 보듯이 실제로는 대부분의 재전송이 wire 에 나가지 않는다.

### 6.3 로컬 효과

재전송된 ROUND-CHANGE 는 노드 자신에게도 전달되고(`A-07` WBFT-NET-030), `A-05` 의 ROUND-CHANGE handler 가 처리한다. 그러면 proposer 의 "현재 round 의 ROUND-CHANGE 가 quorum 만큼 모였는가" 검사가 다시 실행된다. round `r >= 1` 에서는 proposer 가 자기 제안 block 을 다 만들기 전에 quorum 이 이미 채워졌을 때, 이 재검사 덕분에 proposer 가 PRE-PREPARE 를 보낼 수 있다(§8.3).

### 6.4 Wire 효과

reference 구현의 ECDSA signature 는 결정적이고(RFC 6979) RLP 인코딩은 정규형이다. 그러므로 내용(view, prepared round, prepared block, justification)이 바뀌지 않은 채 재전송된 ROUND-CHANGE 는 이전 메시지와 바이트 단위로 같고, 중복 제거 키도 같다(`A-07` §5.2). gossip 절차는 노드가 그 키를 이미 보냈거나 그 키를 받은 모든 peer 를 건너뛴다(`A-07` WBFT-NET-032).

[WBFT-TIMER-024] 노드가 이전에 보낸 메시지와 바이트 단위로 같은 ROUND-CHANGE 를 재전송할 때, 노드는 반드시 peer 별 recent message cache(`A-07` §5.2, WBFT-NET-023, WBFT-NET-032)에 그 키(`A-07` WBFT-NET-022)가 없는 validator peer 에게만 그 ROUND-CHANGE 를 wire 로 보내야 한다. reference 구현에서 이 cache 는 peer 의 주소를 키로 쓰고 재연결 뒤에도 남는다. 그래서 round 가 멈춘 동안 재전송은 보통 네트워크 트래픽을 전혀 만들지 않으며, 로그와 노드 자신의 처리(§6.3)에서만 보인다.
Source: consensus/wbft/core/roundchange.go:63-94, consensus/wbft/backend/backend.go:176-210, crypto/secp256k1/secp256.go:76-79
Observable: network, log

재전송이 wire 에 나가는 경우는 다음 세 가지이다.

1. peer 별 cache 에서 그 peer 의 항목이나 그 항목 안의 키가 밀려났다. 마지막 조회 뒤에 다른 peer 주소 40 개, 또는 그 peer 의 다른 키 1 024 개가 쓰였을 때이다. 재전송을 시도할 때마다 노드는 주소와 키를 조회하므로 둘 다 갱신된다. 그래서 round 가 멈춘 동안 항목이 밀려나려면 retry 간격 하나 안에 그만큼의 다른 주소나 키가 쓰여야 한다.
2. 내용이 바뀌었다. 예를 들어 round `r` 에서 `finalize` 가 실패한 뒤 retry 가 `(h, r)` 를 `prepared_round == r` 로 다시 보내는 경우이다(`A-05` WBFT-SM-087).
3. 노드가 재시작한 뒤 그 peer 에게 처음 보낸다. cache 는 메모리에만 있기 때문이다.

---

## 7. 미래 PRE-PREPARE 대기

PRE-PREPARE 제안의 header timestamp 가 받는 노드의 wall clock 에 `AFBT` 를 더한 값보다 늦으면, 그 PRE-PREPARE 는 "미래에서 온" 것이다. 받는 노드는 그 PRE-PREPARE 를 받아들이지도 거부하지도 않고, timestamp 에 도달했을 때 다시 처리한다.

[WBFT-TIMER-030] 노드가 현재 view 의 PRE-PREPARE 를 처리하는 도중 `Time(proposal) > floor((now() + AFBT × 10^9) / 10^9)` 임을 발견하면, 노드는 그 PRE-PREPARE 를 받아들여서는 안 되고 relay 해서도 안 되며, 반드시 지속 시간이 `d = Time(proposal) × 10^9 − now()` 나노초인 미래 PRE-PREPARE timer 를 걸어야 한다. 이 지속 시간은 `Time − AFBT` 까지가 아니라 header timestamp 까지의 시간이다. 여기서 쓰는 비교는 `A-08` 의 header 검사 가운데 가장 먼저 실행되는 검사이다.
Source: consensus/wbft/core/preprepare.go:147-169, consensus/wbft/backend/backend.go:258-278, consensus/wbft/engine/engine.go:172-175, consensus/wbft/engine/engine.go:201-205
Observable: network, log
Observed as: the log line `WBFT: PRE-PREPARE block proposal is in the future (will be treated again later)` (Info, with `duration`).

미래 검사보다 앞선 검사들은 이미 통과했어야 한다. 앞선 검사는 proposer 신원, sequence 와 block 번호의 일치, `r > 0` 일 때의 justification, 그리고 `A-08` §5 의 제안 단계 P1-P5(block 형식, bad block 목록, validator 집합, transaction root, uncle hash)와 header 단계 H1 이다. 미래 검사 뒤의 header 검사(`A-08`)는 아직 실행되지 않았으며, PRE-PREPARE 를 다시 처리할 때 실행된다.

[WBFT-TIMER-031] 미래 PRE-PREPARE timer 가 만료되면, 노드는 반드시 미뤄 둔 PRE-PREPARE 를 유효한 signature 와 함께 방금 도착한 것처럼 다시 처리해야 한다. 노드는 그 시점의 view 와 state 를 기준으로 그 PRE-PREPARE 를 검사한다(`A-05`). 그 PRE-PREPARE 를 받아들이면 노드는 반드시 PREPARE 를 보내고 그 PRE-PREPARE 를 relay 해야 한다(`A-07` WBFT-NET-041).
Source: consensus/wbft/core/preprepare.go:156-162, consensus/wbft/core/handler.go:136-150
Observable: network

[WBFT-TIMER-032] 노드는 미뤄 둔 PRE-PREPARE 를 반드시 최대 하나만 가져야 한다. 미래 PRE-PREPARE timer 를 걸면 반드시 이전 timer 를 취소해야 하며, 이전 timer 의 PRE-PREPARE 는 그 뒤로 timer 에 의해 다시 처리되지 않는다. 다만 이전 timer 가 이미 만료되었고 그 event 가 아직 처리를 기다리고 있었다면, 그 PRE-PREPARE 는 다시 처리된다.
Source: consensus/wbft/core/preprepare.go:154-163, consensus/wbft/core/core.go:321-325
Observable: network

[WBFT-TIMER-033] round change timer 를 걸거나 다시 걸 때(WBFT-TIMER-013)와 engine 이 멈출 때, 미래 PRE-PREPARE timer 는 반드시 취소되어야 한다. 그러므로 미룬 시간 `d` 가 남은 round change timeout 보다 긴 PRE-PREPARE 는 그 round 에서 결코 받아들여지지 않는다.
Source: consensus/wbft/core/core.go:336-347, consensus/wbft/core/core.go:356
Observable: network

노드가 PRE-PREPARE 처리를 미루는 동안에도 round change timer 는 계속 돈다. 그래서 PRE-PREPARE 를 미뤄도 round 가 길어지지는 않는다.

---

## 8. Block period 대기와 header timestamp

### 8.1 제안을 만드는 시점

block 생산이 켜진 모든 validator 는 view 에 들어갈 때 candidate block 을 만든다. proposer 만 만드는 것이 아니다. 다만 그 block 을 보내는 것은 proposer 뿐이다(`A-05`). 노드는 round 에 따라 정해진 시간을 기다린 뒤 block 을 만들기 시작한다.

[WBFT-TIMER-040] 노드가 인자 0 으로 `start_new_round` 를 불러 view `(h, 0)` 에 들어가면, 노드는 반드시 `w = Time(head) × 10^9 + BP(h) × 10^9 − now()` 나노초를 기다린 뒤 height `h` 의 candidate block 을 만들기 시작해야 한다. 여기서 `head` 는 view 에 들어간 순간의 chain head(block `h − 1`)이다. `w <= 0` 이면 노드는 반드시 바로 만들기 시작해야 한다.
Source: consensus/wbft/core/core.go:260, consensus/wbft/backend/engine.go:139-150 (`timeForNextWork`), consensus/wbft/backend/engine.go:277-285 (`NotifyNewRound`), miner/worker.go:432-441, miner/worker.go:664-669
Observable: network
Observed as: the time of the round-0 PRE-PREPARE.

[WBFT-TIMER-041] 인자 `r >= 1` 로 `start_new_round` 가 불리면, 노드는 반드시 기다리지 않고 height `h` 의 새 candidate block 을 만들기 시작해야 한다. 여기에는 늦은 timeout 의 `CATCH_UP` 경우도 포함된다. 이 경우 노드는 `(h, 0)` 에 들어가지만 block builder 에게는 요청된 round 가 전달된다(`A-05` WBFT-SM-029, WBFT-SM-080).
Source: consensus/wbft/backend/engine.go:279-283, miner/worker.go:664-669
Observable: network

miner 의 `syncing` 표시가 설정되어 있는 동안 노드는 candidate block 을 만들지 않는다. 이 표시는 처음으로 동기화에 성공하기 전의 동기화 동안에만 설정된다(`miner/miner.go:139-165`, `miner/worker.go:1322-1324`). 이것은 §5.5 의 engine 정지와 같은 한계이다. 노드가 새로 block 을 만들기 시작하면, 끝나지 않은 이전 block 만들기는 중단된다.

구현 노트 (informative). WBFT 에는 빈 block 주기(empty-block period)가 없다. validator 는 pool 에 transaction 이 있든 없든 모든 view 에서 block 을 만들고, proposer 는 그 block 을 제안한다(`commitWork` 에는 transaction 수에 대한 조건이 없다, `miner/worker.go:1320-1383`). 그래서 거래가 없는 chain 도 `BP(h)` 초마다 block 을 하나씩 만든다. Quorum 의 `emptyBlockPeriod`(commit `5ffacc48` 의 `consensus/istanbul/qbft/core/request.go:56-97`)에 대응하는 것은 없고, Quorum 설정 key 는 무시된다(`B-01` SNET-CFG-030).

### 8.2 Header timestamp

[WBFT-TIMER-042] parent 가 `p` 인 height `h` 의 candidate block 을 만드는 노드는 반드시 `Time = max(Time(p) + BP(h), unix_now())` 로 설정해야 한다. 이때 `unix_now()` 는 header 를 준비하는 시점에 읽는다.
Source: consensus/wbft/engine/engine.go:501-505, miner/worker.go:1134-1146 (overwritten by `Prepare`), miner/worker.go:1180
Observable: header

유효성에 대한 결과는 다음과 같다(`A-08` 이 규범이고, 여기서는 참고로만 적는다). header 는 `Time >= Time(parent) + BP(h)` 를 만족하며, 모든 verifier 가 이 조건을 검사한다(`consensus/wbft/engine/engine.go:270-272`). 그리고 시계가 올바르게 맞춰져 있으면 proposer 는 `Time` 이후에 block 을 만들기 때문에, 그 header 는 다른 validator 에게 결코 "미래에서 온" 것으로 보이지 않는다.

WBFT-TIMER-040 에 따르면, 노드가 `Time(h − 1) + BP(h)` 시각보다 먼저 `(h, 0)` 에 들어갔고 같은 초 안에 만들기를 시작했다면 round 0 header 의 timestamp 는 `Time(h − 1) + BP(h)` 이다. 그렇지 않으면 timestamp 는 header 를 준비한 wall clock 의 초이다. 그러므로 block 하나를 합의하고 import 하는 데 `BP` 보다 적은 시간이 걸리는 동안에는 block 들이 정확히 `BP` 초 간격으로 나온다. 어떤 height 가 느려지면 그 뒤의 timestamp 가 늦춰지고, 일정은 다시 따라잡지 않는다.

### 8.3 PRE-PREPARE 를 보내는 시점

[WBFT-TIMER-043] proposer 는 wall clock 이 `Time(h − 1) + BP(h)` 에 도달하기 전에 height `h` 의 round 0 PRE-PREPARE 를 보내서는 안 된다. proposer 는 `(h, 0)` 에 대한 만들기가 끝날 때 그 PRE-PREPARE 를 보낸다. 예외는 늦은 round timeout 이 `CATCH_UP` 분기를 탄 경우(`A-05` WBFT-SM-080)이며, 이때는 기다리지 않고 만들기를 시작한다(WBFT-TIMER-041). reference 구현은 다음 경우에도 그 PRE-PREPARE 를 더 일찍 보낼 수 있다. height `h − 1` 의 round `r >= 1` 에서 기다리지 않고 시작한 만들기가 block `h − 1` 의 import 가 끝난 뒤에 chain head 를 읽으면, 그 만들기는 block `h` 를 만든다.
Source: consensus/wbft/core/request.go:33-57, consensus/wbft/backend/engine.go:186-218 (`Seal` posts the request), miner/worker.go:432-441, miner/worker.go:664-669, miner/worker.go:1124 (parent read at build time)
Observable: network

`r >= 1` 에서는 만들기가 PRE-PREPARE 를 일으키지 않는다. request handler 는 새 block 을 저장하기만 한다(`consensus/wbft/core/request.go:47-54`). proposer 는 현재 sequence 의 ROUND-CHANGE 가운데 round 가 현재 round 이상인 어떤 ROUND-CHANGE 를 처리하는 도중, `A-05` 의 quorum 조건이 성립하고 제안할 block 이 있을 때 PRE-PREPARE 를 보낸다. 제안할 block 은 justification 에서 얻은 prepared block 이거나 저장된 block 이다(`consensus/wbft/core/roundchange.go:178-186`). 다만 처리 중인 ROUND-CHANGE 가 F+1 규칙을 일으키면 이 평가는 실행되지 않는다. 저장된 block(`pending_request`)은 한 height 의 round 를 넘어 유지되고(`A-05` WBFT-SM-005, `consensus/wbft/core/core.go:287`), 모든 validator 는 round 0 에서 block 을 만든다(§8.1). 그러므로 round 0 block 을 가진 proposer 는 round `r` 의 `Q` 번째 ROUND-CHANGE 를 처리할 때 이미 제안할 block 을 가지고 있고, 곧바로 PRE-PREPARE 를 보낸다. 이때 저장된 block 은 round 0 block 이다. 다만 round `r` 의 만들기(WBFT-TIMER-041)가 먼저 끝나서 넘어왔다면 저장된 block 은 그 round `r` block 이다. 넘어온 block 은 언제나 저장된 block 을 대체하기 때문이다(`consensus/wbft/core/request.go:47`).

---

## 9. 한 height 의 예상 시간표 (참고)

이 절은 §5–§8 을 합쳐서 observer 가 예측할 수 있는 시각을 정리한다. 표기는 다음과 같다. `t0 = Time(h − 1)`, `P = BP(h)`, `T = RT(h)`(초), `Tr = round_timeout(config_at(h), r)`(초)이다. `T_head` 는 노드가 새 head `h − 1` 을 처리하는 wall clock 시각이다. 모든 시각은 한 노드의 시계로 잰 것이며, 네트워크 지연과 실행 시간은 `ε` 로 적는다.

### 9.1 Round 0, 올바른 proposer

| Event | 시각 | 규칙 |
|---|---|---|
| `(h, 0)` 에 들어가서 round timer 를 걸고(마감 `T_head + T0`) 만들기를 예약한다 | `T_head` | WBFT-TIMER-010, WBFT-TIMER-040 |
| 모든 validator 가 candidate 를 만든다 | `max(T_head, t0 + P)` | WBFT-TIMER-040 |
| proposer 의 header timestamp | `max(t0 + P, floor(build time))` | WBFT-TIMER-042 |
| proposer 가 PRE-PREPARE 를 보낸다 | 만들기 시각 + 실행 `ε` | WBFT-TIMER-043 |
| 받는 노드가 PRE-PREPARE 를 받아들이고, round timer 를 다시 걸고(마감 `t_accept + T0`), PREPARE 를 보낸다 | + 네트워크 `ε` | WBFT-TIMER-012 |
| PREPARE quorum 이 모여 COMMIT 을 보내고, COMMIT quorum 이 모여 decide 한다 | + 네트워크 `ε` 2 회 | `A-05` |
| block 을 import 하고 `(h + 1, 0)` 에 들어간다 | + import `ε` | WBFT-TIMER-010 |

계산 예제로 mainnet preset(`P = 1`, `T = 2`), validator 7 개, 시계가 맞춰진 상황을 보자. block `h − 1` 은 `t0 = 1 000` 이고 `1 000.35` 에 import 된다. round 0 timer 는 마감 `1 002.35` 로 걸리고, 만들기는 `0.65 s` 뒤로 예약된다. `1 001.00` 에 모든 validator 가 block 을 만들고, proposer 의 header 는 `Time = 1 001` 을 가지며, proposer 의 PRE-PREPARE 는 약 `1 001.05` 에 나간다. 받는 노드들은 약 `1 001.06` 에 그 PRE-PREPARE 를 받아들이고 timer 를 `1 003.06` 으로 다시 건다. decide 와 import 가 수십 밀리초 안에 이어져 block `h` 는 약 `1 001.15` 에 import 되고, height `h + 1` 은 `0.85 s` 대기로 시작한다. 체인은 초마다 block 하나를 만들고, timestamp 는 `1 000, 1 001, 1 002, ...` 이다.

### 9.2 Round 0, 응답하지 않는 proposer

같은 예제에서 `(h, 0)` 의 proposer 가 오프라인이라고 하자.

| 시각 | Event |
|---|---|
| `1 000.35` | `(h, 0)` 에 들어간다. 마감은 `1 002.35` 이다 |
| `1 001.00` | 모두 block 을 만들지만 아무도 PRE-PREPARE 를 보내지 않는다 |
| `1 002.35` | 살아 있는 모든 validator 에서 round timer 가 거의 같은 시각에 만료된다. 각 validator 는 `(h, 1)` 에 들어가서 ROUND-CHANGE `(h, 1)` 을 보내고, round timer `T1 = 4 s`(마감 `1 006.35`)와 retry timer `2 s` 를 걸고, 곧바로 새 candidate 를 만든다(header `Time = max(1 001, 1 002) = 1 002`) |
| `1 002.35 + ε` | 노드들이 ROUND-CHANGE `(h, 1)` 의 F+1 과 quorum 을 관측한다. `(h, 1)` 의 proposer 는 round change justification 을 붙인 PRE-PREPARE 를 곧바로 보낸다. 이 PRE-PREPARE 는 `1 001.00` 에 만든 block(`Time = 1 001`)을 제안하고, round 1 의 만들기가 먼저 넘어왔다면 round 1 block(`Time = 1 002`)을 제안한다(§8.3). round 0 block 이 없는 proposer 만 round 1 의 다음 ROUND-CHANGE 를 처리할 때까지 기다리며, 늦어도 `1 004.35` 의 자기 재전송 때 PRE-PREPARE 를 보낸다(§8.3) |
| `1 006.35` 전 | 노드들이 PRE-PREPARE 를 받아들이고, round timer 를 `4 s` 로 다시 걸고, decide 한다 |

round 1 의 proposer 도 응답하지 않으면 round 2 는 `1 006.35` 에 `T2 = 8 s` 로 시작하고, round 3 은 `1 014.35` 에 시작하며, 이후도 같은 방식으로 이어진다(§4.4 의 누적 칸).

### 9.3 `RT` 와 `BP` 사이의 liveness 조건 (참고)

round 0 timer 는 `T_head` 에 시작하지만, round 0 제안은 `t0 + P` 에야 만들어진다. round 0 이 성공하려면 다음 조건이 필요하다.

```
T0  >  (t0 + P − T_head) + build + propagation + verification
```

이전 block 이 자기 timestamp 이후에 합의되었다면 `T_head >= t0` 이다. 그러므로 `RT` 가 `BP` 에 block 을 만들고 전파하고 검증하는 시간을 더한 값보다 크면 이 조건은 충분히 성립한다. 반대로 `RT <= BP × 1000` 이면, `T_head − t0` 가 `BP − RT/1000` 에 block 을 만들고 전파하고 검증하는 시간을 더한 값보다 클 때를 제외하고 round 0 은 timeout 된다. 즉 이전 block 이 늦게 합의된 height 만 round 0 에서 끝날 수 있다. 두 네트워크 preset 은 `RT = 2 000 ms`, `BP = 1 s` 를 쓴다.

### 9.4 Observer 가 확인할 수 있는 예측

| 예측 | 성립하는 조건 | 규칙 |
|---|---|---|
| `h` 의 round 0 PRE-PREPARE 는 proposer 시계로 `Time(h − 1) + BP(h)` 이후에 나간다 | 늦은 timeout `CATCH_UP`(`A-05` WBFT-SM-080) 뒤와, 새 head 를 읽은 `h − 1` 의 round `r >= 1` 만들기 뒤를 제외하면 언제나 | WBFT-TIMER-043 |
| `Time(h) = max(Time(h − 1) + BP(h), 제안을 만든 초)` | 언제나 | WBFT-TIMER-042 |
| 노드는 `(h, 0)` 에 들어간 뒤 약 `T0` 가 지나서 첫 ROUND-CHANGE `(h, 1)` 을 보낸다 | PRE-PREPARE 를 받아들이지 않았고, 더 높은 round 의 ROUND-CHANGE 를 F+1 개 받지 않았으며, height `h − 1` 에서 걸렸거나 재시작 전에 걸린 retry 만료나 round change 만료를 처리하지 않았을 때(`A-05` WBFT-SM-081, WBFT-TIMER-018) | WBFT-TIMER-010, WBFT-TIMER-015 |
| 노드는 `(h, r)` 에 들어간 뒤 약 `Tr` 가 지나서 ROUND-CHANGE `(h, r + 1)` 을 보낸다 | round `r` 에서 PRE-PREPARE 를 받아들이지 않았을 때 | WBFT-TIMER-015 |
| 노드는 round `r` 에서 PRE-PREPARE 를 받아들인 뒤 약 `Tr` 가 지나서 ROUND-CHANGE `(h, r + 1)` 을 보낸다 | view 가 바뀌지 않았을 때(COMMIT quorum 뒤를 포함) | WBFT-TIMER-012, WBFT-TIMER-016 |
| 로그 줄 `WBFT: broadcast ROUND-CHANGE message` 가 같은 round 에 대해 `RT(h)` 마다 반복된다 | 노드가 그 round 에 머물고 그 round 에서 PRE-PREPARE 를 받아들이지 않을 때 | WBFT-TIMER-021 |
| 반복된 ROUND-CHANGE 는 validator 링크에서 다시 보이지 *않는다* | cache 항목이 밀려나지 않았고, retry 가 timer 를 걸 때의 메시지를 다시 보낼 때. `finalize` 실패 뒤, `CATCH_UP` branch 뒤, 새 height 에서 처리된 queue 의 retry 는 여기에 해당하지 않는다(WBFT-TIMER-021, WBFT-TIMER-022, `A-14` §5.8) | WBFT-TIMER-024 |
| `Time > 받는 노드의 unix_now() + AFBT` 인 PRE-PREPARE 에 대해서는 받는 노드의 시계가 `Time` 에 도달할 때까지 PREPARE 가 나오지 않는다 | 그때까지 round 가 끝나지 않았을 때 | WBFT-TIMER-030, WBFT-TIMER-033 |

---

## 10. 구현 노트 (참고)

- 세 timer 는 모두 `time.AfterFunc` timer 이다. timer 의 callback 은 core 의 `event.TypeMux` 에 event 를 넣는다. core 의 단일 event loop 가 timeout, retry, backlog event 를 처리하므로, timer 의 효과는 메시지 처리와 직렬화된다.
- round change timer 와 미래 PRE-PREPARE timer 는 `timerMu` 로 보호되고, retry timer 도 `timerMu` 를 잡은 채 다시 걸린다. `stopTimer` 는 스스로 `timerMu` 를 잡으므로, `newRoundChangeTimer` 는 lock 을 다시 잡기 전에 `stopTimer` 를 부른다(`consensus/wbft/core/core.go:356`, `:405`).
- `newRoundChangeTimer` 는 `c.current` 를 읽는 대신 view 의 snapshot 을 받는다(#82 이후). `newRetrySendingRoundChangeTimer` 는 `currentMutex.RLock` 을 잡은 채 view 의 snapshot 을 만든다(`consensus/wbft/core/core.go:426-437`).
- 미래 PRE-PREPARE callback 은 만료 시점에 `c.valSet` 을 읽어서 event 의 `src` 필드를 채운다. handler 는 `src` 를 무시한다(`consensus/wbft/core/preprepare.go:157`, `consensus/wbft/core/handler.go:136-150`).
- block period 대기는 miner 의 goroutine 안에서 실행되는 `time.Sleep` 이다(`miner/worker.go:436-438`). 이 대기는 view 가 바뀌어도 취소되지 않는다. 그래서 노드가 round 1 에 들어간 뒤에도 아직 잠들어 있던 round 0 대기가 끝나면, 노드는 block 을 한 번 더 만들기 시작한다. 이 새 block 만들기는 round 1 에서 진행 중이던 block 만들기를 중단시킨다.

---

## 11. 버전 노트

- `git diff v1.1.0 740526d03 -- consensus/wbft/core` 는 비어 있다. 모든 timer 코드는 `v1.1.0` 과 reference commit 에서 같다.
- commit `c37994e9b` (#82) 는 `v1.1.0` tag 의 조상이 아니지만, 그 변경은 `v1.1.0` release commit `71e3f820f`(`dev` 를 squash 한 commit)에 들어 있다. `v1.1.0` 이전의 tag 로 빌드한 노드는 낡은 round change 만료에 반응할 수 있다(§5.3 노트). 그런 버전은 이 명세의 대상이 아니다.
