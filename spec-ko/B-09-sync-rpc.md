# B-09 Synchronisation, head 선택, RPC

- Part: B (StableNet block 유효성)
- Area codes: `SYNC`, `RPC`
- Status: draft
- Reference: go-stablenet `740526d03`

이 장은 StableNet 노드가 체인을 어떻게 따라잡는지, head 를 어떻게 고르는지, `finalized` 태그와 `safe` 태그가 무엇을 뜻하는지, 그리고 도구가 WBFT 합의를 관찰할 때 쓰는 RPC 를 정한다. 앞쪽 절반 (`SYNC`)은 상호운용에 중요하다. 다르게 sync 하는 노드는 다른 체인을 받아들이거나 다른 head 를 보고할 수 있기 때문이다. 뒤쪽 절반 (`RPC`)은 inspector 와 운영자가 이 RPC 로 합의 데이터를 읽기 때문에 중요하다. 참조 구현의 RPC 출력이 불규칙한 곳에서도, 이 장은 그 불규칙함을 그대로 규범으로 적는다. 그래야 go-stablenet 을 대상으로 만든 도구가 계속 동작한다.

block 유효성 자체는 `A-08` (합의 header 필드), `B-03` (실행 header 와 body), `B-07` (transaction)에 있다. 새 head 에 대한 합의 코어의 반응 (`NewChainHead`, `FinalCommittedEvent`)은 `A-05` 에 있다. `istanbul/100` subprotocol 은 `A-07` 에 있다. 노드가 `eth/68` protocol 과 `snap/1` protocol 에서 쓰는 값과, 참조 구현이 go-ethereum v1.13.15 보다 엄격하게 검사하는 곳은 §12 에 있다.

---

## 1. 범위와 conformance

이 장에서 규범인 것은 다음과 같다.

- 노드가 synchronisation 중에 어떤 block 을 import 하고 어떤 검사를 적용하는지가 규범이다 (`SYNC`). 이 동작은 노드가 제공하는 체인으로 관찰된다.
- head 를 고르는 방법과 `latest`/`finalized`/`safe` 태그의 뜻이 규범이다 (`SYNC`, `RPC`).
- `istanbul` namespace 와, `eth`, `personal`, `miner`, `debug`, `admin` 의 Anzeon 고유 부분에 대한 요청 인자, 응답 schema, 한도, 오류가 규범이다 (`RPC`).
- `eth/68` `Status` 의 값, `eth/68` 과 `snap/1` 응답의 한도, transaction 전파의 한도가 규범이다 (`SYNC`, §12).

informative 인 것은 peer 선택, downloader 일정, metric 이름, bootstrap 노드 (§12.4), 노드 기본값 (§13), Engine API 동작이다. Engine API 동작은 실제로 존재하고 위험하기 때문에 적었다 (§11). conforming WBFT 노드가 Engine API 를 노출할 필요는 없다.

JSON 관례는 go-ethereum JSON-RPC 2.0 을 따른다. `QUANTITY` 는 앞자리 0 이 없는 `0x` 접두 16 진 정수다. `DATA` 는 `0x` 접두 16 진 바이트다. 주소는 소문자로 쓴 20 바이트 `DATA` 이며, Go `common.Address` 의 텍스트 인코딩과 같다. `BlockNumber` 인자는 `QUANTITY` 나 태그 `"earliest"` (0), `"latest"` (-2), `"pending"` (-1), `"finalized"` (-3), `"safe"` (-4) 가운데 하나를 받는다.

Source: rpc/types.go:63-71

---

## 2. Synchronisation 방식

[SNET-SYNC-001] 기본 synchronisation 방식은 반드시 full sync 여야 한다. full sync 는 genesis 부터 모든 block 을 실행하는 방식이다.
Source: eth/ethconfig/config.go:55-60
Observable: rpc

full sync 로 설정한 노드는 시작할 때 head state 가 없거나 이전 snap sync 가 끝나지 않았으면 snap sync 로 바꾼다. snap sync 로 설정한 노드는 데이터베이스에 이미 state 를 가진 head 가 있으면 full sync 로 바꾼다.

Source: eth/handler.go:180-206

[SNET-SYNC-002] snap sync 에서 노드는 반드시 모든 header 를 WBFT header 검증 전체 (`A-08`)로 verify 해야 하며, 여기에는 모든 header 의 모든 prepared seal 과 committed seal 이 포함된다. 노드는 seal 을 표본으로 골라 검사해서는 안 된다.
Source: core/headerchain.go:306-340, core/blockchain.go:2455-2462, consensus/wbft/backend/engine.go:87-112
Observable: rpc

[SNET-SYNC-003] full sync 에서 노드는 반드시 각 header 를 WBFT header 검증 전체로 verify 한 뒤, parent state 위에서 block 을 실행하고 `GasUsed`, `Bloom`, `ReceiptHash`, `Root` 를 검사해야 한다 (`B-03`).
Source: core/blockchain.go:1563-1592, core/blockchain.go:1779-1792, core/block_validator.go:124-145
Observable: rpc

snap sync 로 받은 receipt 에는 `effectiveGasPrice` 가 없다. 노드는 이 값을 나중에 header gas tip 과 `AuthorizedTxExecuted` log 로 유도한다 (`B-07 §8.2`). snap sync 로 받은 state 에는 계정 `Extra` 비트가 들어 있다. slim 계정 인코딩에도 같은 선택 필드가 있기 때문이다.

Source: core/types/receipt.go:396-411, core/types/state_account.go:73

---

## 3. Synchronisation 중 state 가 필요한 검사

WBFT header 검증의 두 부분은 parent 의 실행 뒤 state 가 필요하다. 하나는 `Coinbase` blacklist 검사 (`A-08` 단계 H15b, `B-08` SNET-SRC-020, `verifySigner`)이고, 다른 하나는 gas tip 검사 (`A-08` 단계 H21, `B-06`, `verifyGasTip`)다. `A-08` §6.6 은 이 둘을 state 의존 단계라고 부른다. 그 state 가 없으면 참조 구현은 두 검사를 건너뛴다.

```python
def verify_header_state_parts(header, parent):
    state = state_at(parent.root)            # 없을 수 있다
    if state is None:
        pass                                  # blacklist 검사를 건너뛴다
    elif is_blacklisted(state, header.coinbase):
        raise ErrBlacklistedSigner

    try:
        want = gov_validator_gas_tip(state_at(get_header(parent.hash).root))
    except Exception:                         # 값 불일치가 아닌 모든 오류 (A-08 WBFT-HDR-111):
        return                                # state 없음, parent 없음, zero root, ... -> 건너뛴다
    if header.extra.gas_tip != want:
        raise GasTipMismatchError
```

Source: consensus/wbft/engine/engine.go:290-298, consensus/wbft/engine/engine.go:329-344, consensus/wbft/engine/engine.go:352-380, consensus/wbft/engine/engine.go:622-645

[SNET-SYNC-010] header 검증 중에 parent 의 state 가 없으면, 노드는 header 를 거부하지 말고 반드시 proposer blacklist 검사와 gas tip 검사를 건너뛰어야 한다.
Source: consensus/wbft/engine/engine.go:290-298, consensus/wbft/engine/engine.go:329-344
Observable: rpc

seal 검사, randao 검사, 앞선 `EpochInfo` header 로부터 validator 집합을 유도하는 일은 header 만 읽으므로 state 가 필요 없다. 그래서 노드는 이 검사들을 항상 적용한다 (`A-08`). 새 `EpochInfo` 의 내용은 block 을 실행할 때만 검사하므로 (`B-06` SNET-FIN-015), snap sync 는 그 내용을 한 번도 검사하지 않는다.

---

## 4. 노드가 sync 를 시작하는 시점

chain syncer 는 자신의 total difficulty (TD)를 가장 좋은 peer 의 TD 와 비교한다. 모든 WBFT block 은 difficulty 가 1 이므로 (`A-08`) `TD(h) = TD(genesis) + h` 이고, 그래서 TD 비교는 height 비교와 같다.

[SNET-SYNC-020] Anzeon 체인에서 force flag 가 켜져 있지 않으면, 노드는 가장 좋은 peer 의 TD 가 `local_td + 1` 이하인 동안 downloader sync 를 시작해서는 안 된다. force flag 가 켜져 있으면, 노드는 peer 의 TD 가 `local_td` 를 넘을 때 반드시 sync 를 시작해야 한다. force flag 는 연속한 두 `TdSyncInterval` tick (기본 10 초)에서 로컬 head TD 가 같으면 켜진다. 그래서 head 가 멈춘 것은 10 초에서 20 초 사이에 감지된다. force flag 는 TD 비교까지 도달한 첫 sync 평가가 끈다. peer 가 모자라 그 전에 끝난 평가는 flag 를 끄지 않는다.
Source: eth/sync.go:161-217, eth/sync.go:135-145, eth/ethconfig/config.go:62
Observable: network

한 block 뒤처진 노드는 그 block 을 전파 경로인 block fetcher 로 받거나 (`§6`), validator 라면 합의로 받을 것으로 기대된다 (`A-05`). 노드가 두 block 뒤처지면 downloader 가 따라잡기를 맡고, 10 초에서 20 초 동안 head 가 멈춰 있으면 한 block 차이에서도 downloader 가 맡는다.

[SNET-SYNC-021] 노드는 force timer (`ForceSyncCycle`, 기본 10 초)가 끝나기 전에는 peer 가 `min(5, maxPeers)` 개보다 적으면 downloader sync 를 시작해서는 안 된다. 이 값은 `defaultMinSyncPeers` 이며, 설정된 peer 상한이 더 작으면 그 상한으로 낮아진다. force timer 가 끝났으면 peer 하나로도 충분하다.
Source: eth/sync.go:176-185, eth/ethconfig/config.go:61
Observable: network

downloader sync 가 성공적으로 끝나면 노드는 자신을 synced 로 표시하고 (§6), head block 을 peer 에게 announce 한다. 이때 노드는 hash 만 알리며 (`BroadcastBlock(block, false)`), block 전체를 보내지는 않는다.

Source: eth/sync.go:264-284

---

## 5. Synchronisation 중 합의와 block 조립

downloader 는 sync 를 시작할 때 `StartEvent` 를, 끝날 때 `DoneEvent` 나 `FailedEvent` 를 publish 한다. miner 는 첫 `DoneEvent` 까지만 이 event 에 반응한다.

```python
def miner_update(ev):                         # 첫 DoneEvent 까지만 실행된다
    if ev is StartEvent:
        can_start = False
        if mining: worker.stop(); should_start = True      # WBFT 코어를 멈춘다 (Backend.Stop)
        worker.syncing = True
    elif ev in (FailedEvent, DoneEvent):
        can_start = True
        if should_start: worker.start()                    # WBFT 코어를 다시 시작한다 (Backend.Start)
        worker.syncing = False
        if ev is DoneEvent: unsubscribe()                  # 이후 downloader event 에 반응하지 않는다
```

Source: miner/miner.go:113-178, miner/worker.go:444-458

[SNET-SYNC-030] validator 는 프로세스의 첫 번째 성공한 downloader sync 가 끝날 때까지, downloader sync 가 시작되면 반드시 WBFT 합의 코어를 멈추고 그 sync 가 끝나면 다시 시작해야 한다. 첫 번째 성공한 sync 뒤의 downloader sync 는 합의 코어를 멈춰서는 안 된다.
Source: miner/miner.go:113-178, miner/worker.go:444-458
Observable: network

첫 번째 성공한 sync 뒤에 뒤처진 validator 는 downloader 가 block 을 import 하는 동안에도 합의 코어를 계속 돌린다. 합의 코어는 새 head 마다 `NewChainHead` (`A-05`)로 따라가고, 네트워크의 나머지가 이미 decide 한 height 에 대해 ROUND-CHANGE 메시지를 보낼 수 있다. sync 가 실패해도 첫 sync 까지만 코어를 멈추는 이 동작은 끝나지 않는다. 이 동작을 끝내는 것은 `DoneEvent` 뿐이다.

[SNET-SYNC-031] `worker.syncing` 이 참인 동안 노드는 sealing loop(`commitWork`)에서 proposal 이나 pending block 을 조립해서는 안 된다. Engine API 의 payload builder(`getSealingBlock` 에서 부르는 `generateWork`)는 이 flag 를 검사하지 않는다 (§11).
Source: miner/worker.go:1320-1324
Source: miner/worker.go:1288, miner/worker.go:1430, miner/payload_building.go:194, miner/payload_building.go:230
Observable: rpc

---

## 6. Block fetcher 경로

block fetcher 를 거쳐 체인에 들어가는 block 은 두 종류다. 하나는 전파된 block 으로, `eth` peer 가 보낸 `NewBlock` announcement 와 전체 block 이다. 다른 하나는 non-proposer 에서 로컬 합의 코어가 commit 한 block 으로, `Backend.Commit` 이 `Enqueue("istanbul", block)` 을 불러 넘긴다. fetcher 는 header 를 verify 하고, 그 header 를 다시 전파하고, inserter 를 부른다.

Source: consensus/wbft/backend/backend.go:238-247, eth/handler_istanbul.go:38-40, eth/handler.go:229-307

[SNET-SYNC-040] 노드가 synced 로 표시될 때까지 fetcher inserter 는 반드시 받은 모든 block (전파된 block 이든 로컬에서 commit 한 block 이든)을 import 하지 않고 오류 없이 버려야 한다.
Source: eth/handler.go:265-273
Observable: rpc

노드는 다음 셋 가운데 하나가 일어나면 synced 로 표시된다.

1. downloader sync 가 성공적으로 끝난다.
2. validator 가 `StartMining` 을 부른다.
3. 노드가 Engine API `forkchoiceUpdated` 를 받는다 (§11).

Source: eth/handler.go:724-737, eth/sync.go:264-270, eth/backend.go:465-468, eth/catalyst/api.go:338

그래서 validator 가 아닌 노드는 첫 downloader sync 가 끝날 때까지 전파된 block 을 모두 무시한다. validator 는 `StartMining` 이 synced 로 표시하므로 이 문제를 피한다. 그렇지 않았다면 validator 는 자기 합의 코어가 commit 한 block 을 버렸을 것이다.

[SNET-SYNC-041] fetcher 는 번호가 로컬 head 보다 7 넘게 낮거나 32 넘게 높은, announce 되거나 전달된 block 을 반드시 버려야 한다.
Source: eth/fetcher/block_fetcher.go:43-44, eth/fetcher/block_fetcher.go:369, eth/fetcher/block_fetcher.go:399
Source: eth/fetcher/block_fetcher.go:783-788 (enqueue: delivered blocks, including local consensus commits)
Observable: network

---

## 7. Head 선택과 finality

### 7.1 Fork choice

insert 되는 모든 block 은 upstream 의 total difficulty fork choice 를 거친다.

```python
def reorg_needed(current, extern) -> bool:
    local_td, extern_td = td(current), td(extern)
    if TTD is not None and TTD <= extern_td: return True     # WBFT 에서는 일어나지 않는다 (TTD 없음)
    if extern_td > local_td: return True
    if extern_td < local_td: return False
    if extern.number < current.number: return True
    if extern.number == current.number:
        cur_p, ext_p = is_local(current), is_local(extern)     # header.Coinbase 가 etherbase 이거나 txpool.locals 에 있다
        return (not cur_p) and (ext_p or random() < 0.5)
    return False
```

Source: core/forkchoice.go:77-110, eth/backend.go:376-423

[SNET-SYNC-050] 노드는 block 의 TD 가 현재 head 의 TD 보다 크면 반드시 그 block 을 head 로 삼아야 한다. difficulty 가 1 이므로 이 조건은 "block 이 head 보다 높다" 와 같다.
Source: core/forkchoice.go:92-97
Observable: rpc

[SNET-SYNC-051] 같은 height 의 두 block 에 대해 참조 구현은 다음과 같이 동작한다. 이 상황은 같은 height 의 서로 다른 두 block 이 모두 유효한 commit quorum 을 가질 때, 곧 `f < n/3` 가정이 깨졌을 때만 생긴다.
1. 현재 head 의 coinbase 가 로컬 (etherbase 이거나 `txpool.locals` 에 있는 주소, `eth/backend.go:376-397`)이면 현재 head 를 유지한다.
2. 새 block 의 coinbase 가 로컬이면 새 block 을 받아들인다.
3. 그 밖의 경우 1/2 확률로 무작위로 고른다.
conforming 노드는 이 경우 어떤 tie-break 을 써도 된다. 노드는 반드시 그런 event, 곧 한 height 에 commit 된 block 이 둘 있는 상황을 safety 위반으로 보고해야 한다.
Source: core/forkchoice.go:98-110
Observable: rpc

WBFT 에는 자체 fork choice 규칙이 없다. 유효한 commit quorum 을 가진 block 은 insert 시점에 final 이다 (`A-05`). safety 가정이 성립하는 동안 TD 규칙은 height 마다 candidate 를 하나만 본다.

### 7.2 미래 block

[SNET-SYNC-052] 시간이 로컬 시계에 `AllowedFutureBlockTime` (기본 0 초)을 더한 값보다 큰 header 는 header 검증에서 반드시 `ErrFutureBlock` 으로 거부해야 한다. 이렇게 거부된 block 이 로컬 시계와 30 초 이내이면, 노드는 그 block 을 bad 로 표시하지 않고 queue 에 넣어 retry 한다.
Source: consensus/wbft/engine/engine.go:201-205, core/blockchain.go:105, core/blockchain.go:1501-1520, consensus/wbft/config.go:121
Observable: rpc

### 7.3 `finalized` 태그와 `safe` 태그

WBFT finality 는 즉시 일어나므로 `finalized` 와 `safe` 의 자연스러운 뜻은 "head" 다. 그런데 참조 구현은 일부 insert 경로에서만 두 태그를 갱신한다.

| 경로 | `finalized` / `safe` 를 갱신하는가 |
|---|---|
| fetcher inserter (전파된 block, non-proposer 의 로컬 commit) | 갱신한다. `InsertChain` 이 성공한 뒤, insert 한 batch 의 마지막 block 으로 갱신한다 |
| Proposer (miner 결과 루프의 `WriteBlockAndSetHead`) | 갱신하지 않는다 |
| Downloader (`InsertChain`, `InsertReceiptChain`, `InsertHeaderChain`) | 갱신하지 않는다 |
| Engine API `forkchoiceUpdated` | 갱신한다. 주어진 hash 로 갱신한다 (§11) |
| 재시작 | `finalized` 를 데이터베이스에서 복원하고, `safe` 를 같은 값으로 둔다 |
| 두 태그보다 낮은 곳으로 `SetHead` | 없음으로 초기화한다 |

Source: eth/handler.go:297-305, core/blockchain.go:539-549, core/blockchain.go:620-640, core/blockchain.go:810-818, eth/catalyst/api.go:340-371

[SNET-RPC-001] `finalized` 와 `safe` block 태그는 반드시 위 표의 경로가 마지막으로 기록한 block 으로 해석되어야 한다. 기록된 block 이 없으면 두 태그에 대한 요청은 반드시 실패해야 하며, 오류는 각각 `finalized block not found` 와 `safe block not found` 다.
Source: eth/api_backend.go:80-93, eth/api_backend.go:132-145
Observable: rpc

결과는 다음과 같다. 로컬 노드가 block 을 제안한 직후와 downloader catch-up 동안에는 `finalized` 가 head 보다 뒤처진다. 그래서 `finalized < latest` 를 finality 지연으로 해석하는 도구는 WBFT 노드를 잘못 읽는다. metric `chain/head/finalized` 도 똑같이 뒤처진다.

### 7.4 Weak subjectivity

[SNET-SYNC-060] 노드는 반드시 genesis 부터 header 체인을 따라 epoch 정보를 추적해서 어느 height 의 validator 집합이든 얻어야 한다 (`A-04`). 참조 구현에는 checkpoint sync, trusted-header sync, weak-subjectivity sync 가 없다. `RequiredBlocks` (`--eth.requiredblocks`)는 주어진 번호의 block hash 가 다른 peer 를 끊을 뿐이고, 무엇을 verify 하는지는 바꾸지 않는다.
Source: consensus/wbft/backend/engine.go:420-449, eth/handler.go:103, eth/handler.go:445-470 (RequiredBlocks)
Observable: rpc

신뢰의 기준점은 genesis block (validator 집합, BLS key, 설정)이다. validator 집합 전체가 바뀐 뒤에 sync 하는 새 노드는, 오래전에 validator 였던 key 로 서명한 다른 체인을 받을 수 있다 (long-range attack). 그 노드를 보호하는 것은 genesis 기준점과 노드가 고른 peer 뿐이다.

---

## 8. `istanbul` namespace

WBFT backend 는 `istanbul` namespace 를 버전 `1.0` 의 공개 namespace 로 등록한다. `--http.api`/`--ws.api` 로 이 namespace 를 켜면 HTTP/WS/IPC 에서 쓸 수 있다.

Source: consensus/wbft/backend/engine.go:232-239

[SNET-RPC-041] `istanbul` namespace 에는 반드시 §8.1-§8.6 의 method 여덟 개, 곧 `nodeAddress`, `getCommitSignersFromBlock`, `getCommitSignersFromBlockByHash`, `getValidators`, `getValidatorsAtHash`, `isValidator`, `status`, `getWbftExtraInfo` 만 있어야 한다. Quorum method 인 `getSignersFromBlock`, `getSignersFromBlockByHash`, `getSnapshot`, `getSnapshotAtHash`, `candidates`, `propose`, `discard` 는 있어서는 안 된다. 이 method 를 부르면 JSON-RPC "method not found" 로 실패한다. validator 는 header 에서 투표로 정하지 않는다 (`A-04`). `status` 는 Quorum method 와 이름이 같지만 결과가 다르다 (SNET-RPC-016).
Source: consensus/wbft/backend/api.go:79-162, consensus/wbft/backend/api.go:164-207, consensus/wbft/backend/api.go:347-363, consensus/wbft/backend/api.go:416-452, consensus/wbft/backend/engine.go:232-239
Observable: rpc

참조 구현의 JavaScript console 에 있는 `istanbul` 모듈은 위 Quorum method 를 여전히 나열하고, `getCommitSignersFromBlock` 과 `getCommitSignersFromBlockByHash` 는 나열하지 않는다. console 에서 나열된 Quorum method 를 부르면 "method not found" 로 실패한다.

Source: internal/web3ext/web3ext.go:36, internal/web3ext/web3ext.go:930-1012

> 해설: Quorum 용 도구 (예: validator 투표 script)를 WBFT 노드에 붙이면 `method not found` 를 받는다. WBFT 의 validator 집합은 epoch 로 정해지므로 투표 method 가 필요 없다. 새 구현이 호환을 위해 Quorum method 를 더하면 참조 구현과 RPC 표면이 달라지므로, SNET-RPC-041 은 method 집합을 닫아 둔다.

공통 규칙은 다음과 같다.

- 선택적인 `BlockNumber` 인자를 생략하거나 `"latest"` 로 주면 현재 head header 를 뜻한다.
- `getCommitSignersFromBlock`, `getValidators`, `getWbftExtraInfo` 에서는 그 밖의 음수 태그 (`"pending"`, `"finalized"`, `"safe"`)를 검사 없이 `uint64` 로 바꾸므로, 그 값은 어떤 block 과도 맞지 않는다.
- 오류는 코드 `-32000` 과 표에 적은 메시지를 가진 JSON-RPC 오류로 돌아온다.

### 8.1 `istanbul_nodeAddress`

| | |
|---|---|
| Params | 없다 |
| Result | `DATA` (20 바이트): 로컬 validator (signer) 주소 |

Source: consensus/wbft/backend/api.go:79-82

[SNET-RPC-010] `istanbul_nodeAddress` 는 반드시 노드가 합의 메시지와 seal 에 서명할 때 쓰는 주소를 돌려줘야 한다.
Source: consensus/wbft/backend/api.go:79-82
Observable: rpc

### 8.2 `istanbul_getCommitSignersFromBlock`, `istanbul_getCommitSignersFromBlockByHash`

| | |
|---|---|
| Params | `[number?: BlockNumber]` / `[hash: DATA(32)]` |
| Result | `BlockSigners` 객체 |
| Errors | `unknown block`; `zero seals` (committed seal 이 없는 block, 예를 들어 genesis); `validator address is zero` |

`BlockSigners` 에는 JSON tag 가 없으므로, 키는 Go 필드 이름이다.

```json
{
  "Number": 1234,                                  // JSON number (10 진), QUANTITY 가 아니다
  "Hash": "0x…",                                   // 32 바이트 DATA
  "Author": "0x…",                                 // header.Coinbase
  "Committers": ["0x…", "0x…"]                     // CommittedSeal.Sealers 에서 비트가 켜진 주소,
}                                                  // validator 인덱스 오름차순
```

Source: consensus/wbft/backend/api.go:42-48, consensus/wbft/backend/api.go:84-129, consensus/wbft/engine/engine.go:1131-1141, consensus/wbft/engine/engine.go:1322-1336

[SNET-RPC-011] `Committers` 는 반드시 block 의 committed seal bitmap 에서 켜진 sealer 인덱스 `i` 마다, 오름차순으로, 그 block 을 지배하는 epoch 정보 (`A-04`)의 `EpochInfo.Validators[i]` 에 해당하는 candidate 주소를 나열해야 한다. bitmap 은 응답하는 노드 자신이 가진 header 사본에서 읽는다. `CommittedSeal` 은 node-local 이므로 (`A-08` WBFT-HDR-053), 두 conforming 노드가 같은 block 에 대해 서로 다른 `Committers` 를 돌려줘도 된다.
Source: consensus/wbft/engine/engine.go:1131-1141, consensus/wbft/engine/engine.go:1322-1336
Observable: rpc

[SNET-RPC-012] `Author` 는 반드시 `header.Coinbase` 여야 한다. 참조 구현의 `Author` 는 signature 를 recover 하지 않는다.
Source: consensus/wbft/engine/engine.go:86-88
Observable: rpc

### 8.3 `istanbul_getValidators`, `istanbul_getValidatorsAtHash`

| | |
|---|---|
| Params | `[number?: BlockNumber]` / `[hash: DATA(32)]` |
| Result | `DATA(20)` 배열: 주어진 block 을 seal 하는 validator 집합 (`validators_at(number)`, `A-04`)을 validator 인덱스 순서로 담는다 |
| Errors | `unknown block`; validator 집합 유도에서 나온 오류 |

Source: consensus/wbft/backend/api.go:131-162

[SNET-RPC-013] `istanbul_getValidators(n)` 은 반드시 `validators_at(n)`, 곧 block `n` 에 seal 이 들어 있는 집합을 돌려줘야 한다. block `n` 이 다음 epoch 을 위해 선출한 집합을 돌려줘서는 안 된다.
Source: consensus/wbft/backend/api.go:144-148
Observable: rpc

### 8.4 `istanbul_isValidator`

| | |
|---|---|
| Params | `[number?: BlockNumber]` |
| Result | `bool`. `istanbul_nodeAddress` 가 `istanbul_getValidators(number)` 에 있으면 `true` 다 |

[SNET-RPC-014] validator 집합을 정할 수 없으면, `istanbul_isValidator` 는 반드시 오류가 아니라 `false` 를 돌려줘야 한다. block 을 몰라서 집합을 정할 수 없는 경우도 여기에 포함된다.
Source: consensus/wbft/backend/api.go:347-363
Observable: rpc

### 8.5 `istanbul_status`

| | |
|---|---|
| Params | `[start?: BlockNumber, end?: BlockNumber]` |
| Result | `Status` 객체 |

범위 규칙은 go-stablenet PR #86 (commit `d7cff3df9`)에서 강화되었다. 이 commit 은 `v1.1.0` 태그의 조상이 아니지만, 그 내용은 `v1.1.0` release 에 들어 있다. `git diff v1.1.0 740526d03 -- consensus/wbft/backend` 가 비어 있기 때문이다 (`A-12` §1).

```python
MAX_STATUS_BLOCK_RANGE = 1024

def status_range(start, end, head):
    if start is not None and end is None: raise "pass the end block number"
    if start is None and end is not None: raise "pass the start block number"
    if start is None:                               # 둘 다 생략: 마지막 64 개 block
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

[SNET-RPC-015] `istanbul_status` 는 반드시 1024 개보다 많은 block 범위를 거부해야 하고, 반드시 위의 범위 규칙을 적용해야 한다.
Source: consensus/wbft/backend/api.go:205-257
Observable: rpc

응답은 다음과 같다.

```json
{
  "sealerActivity": {
    "total":         { "<addr>": <int>, … },   // prepared + committed + prevPrepared + prevCommitted
    "prepared":      { "<addr>": <int>, … },   // PreparedSeal 의 비트
    "committed":     { "<addr>": <int>, … },   // CommittedSeal 의 비트
    "prevPrepared":  { "<addr>": <int>, … },   // PrevPreparedSeal 의 비트, 이전 block 의 집합으로 매핑
    "prevCommitted": { "<addr>": <int>, … }
  },
  "author":     { "<addr>": <int>, … },        // header.Coinbase 횟수
  "blockRange": { "startBlock": <int>, "endBlock": <int>, "totalBlocks": <int> },
  "roundStats": { "roundDistribution": { "<round>": <int>, … } }  // 키: block 의 10 진 round
}
```

모든 횟수와 범위 필드는 JSON number 다. map 키는 소문자 16 진 주소이거나 10 진 round 번호다. `prepared`, `committed`, `roundStats` 는 응답하는 노드 자신의 header 사본에서 나오므로 node-local 이다 (`A-08` WBFT-HDR-053). 그래서 노드마다 같은 범위에 대해 다른 값을 돌려줘도 된다. `prevPrepared` 와 `prevCommitted` 는 block hash 가 덮으므로 모든 노드에서 같다.

Source: consensus/wbft/backend/api.go:50-77, consensus/wbft/backend/api.go:164-203

[SNET-RPC-016] 범위 안의 block 마다 `istanbul_status` 는 반드시 `PreparedSeal` 과 `CommittedSeal` 에서 켜진 모든 sealer 인덱스를 그 block 의 validator 집합으로 세고, `PrevPreparedSeal` 과 `PrevCommittedSeal` 에서 켜진 모든 인덱스를 이전 block 의 validator 집합으로 세야 한다. 이때 집합 크기를 넘는 인덱스는 무시한다. 센 seal 은 모두 반드시 `total` 도 하나 올려야 한다.
Source: consensus/wbft/backend/api.go:309-333
Observable: rpc

[SNET-RPC-017] 범위의 첫 block 의 validator 와, 범위 안에서 epoch 이 바뀔 때마다 새로 적용되는 validator 는, 아무것도 서명하지 않았더라도 반드시 `prepared`, `committed`, `total`, `author`, `prevPrepared`, `prevCommitted` 에 횟수 0 으로 나타나야 한다. 이전 집합의 validator 는 반드시 `prevPrepared`, `prevCommitted`, `total`, `author` 에 0 으로 나타나야 한다.
Source: consensus/wbft/backend/api.go:272-298
Observable: rpc

[SNET-RPC-018] 범위 안의 block 하나라도 없거나, extra 를 decode 할 수 없거나, validator 집합을 유도할 수 없으면, `istanbul_status` 는 부분 횟수를 돌려주지 말고 반드시 전체가 실패해야 한다 (`block <n> not found`, `block <n>: failed to extract WBFT extra: …`, `block <n>: failed to get validators: …`).
Source: consensus/wbft/backend/api.go:261-277
Observable: rpc

validator 집합 cache 는 첫 block 에서, 그리고 `EpochInfo` 를 실은 block 다음에만 새로 고친다. `roundStats` 에는 더 이상 `totalRounds` 가 없다. PR #86 이 이 필드를 제거했다.

### 8.6 `istanbul_getWbftExtraInfo`

| | |
|---|---|
| Params | `[number: BlockNumber]` (필수) |
| Result | 그 block 의 decode 된 `WBFTExtra` 를 담은 객체 |
| Errors | `block is not a wbft block` (Anzeon 이 없는 체인); `block <n> not found`; extra decode 오류; validator 집합 오류 |

```json
{
  "vanityData":        "0x…",            // DATA
  "randaoReveal":      "0x…",            // DATA (65 바이트 ECDSA signature, A-02)
  "prevRound":         "0x…",            // QUANTITY
  "prevPreparedSeal":  { "sealers": ["0x…"], "signature": "0x…" } | null,   // sealers 는 이전 block 의 집합으로 매핑
  "prevCommittedSeal": { … } | null,
  "round":             "0x…",            // QUANTITY; node-local (A-08 WBFT-HDR-053)
  "preparedSeal":      { … } | null,     // sealers 는 이 block 의 집합으로 매핑; node-local
  "committedSeal":     { … } | null,
  "gasTip":            "27600000000000", // 10 진 문자열 (big.Int.String); 비어 있지 않다 (A-03 WBFT-ENC-008)
  "epochInfo": null | {
     "candidates": [ { "addr": "0x…", "diligence": "0x…" }, … ],
     "validators": [ { "index": "0x…", "addr": "0x…", "bls": "0x…" }, … ]  // index 가 범위 밖이면 addr 는 0x000…0
  }
}
```

`istanbul_getWbftExtraInfo` 의 모든 주소(`sealers`, `epochInfo.candidates[].addr`, `epochInfo.validators[].addr`)는 checksum 문자열 (Go `Address.Hex()`)이다. 이 namespace 의 다른 method 는 소문자 주소를 쓰므로 이 method 만 다르다.

Source: consensus/wbft/backend/api.go:365-452, core/types/istanbul.go:189-194

[SNET-RPC-019] `istanbul_getWbftExtraInfo` 는 위 schema 와 같이 반드시 `gasTip` 을 10 진 문자열로, 다른 숫자 필드를 16 진 문자열로 인코딩해야 한다.
Source: consensus/wbft/backend/api.go:438-449
Observable: rpc

인자가 pointer 가 아닌 `BlockNumber` 이므로, parser 는 `"latest"` 를 받아들이지만 그 값은 `uint64(-2)` 가 되고, 호출은 `block -2 not found` 로 실패한다.

---

## 9. Anzeon 고유의 `eth`, `personal`, `miner`, `debug`, `admin` 동작

### 9.1 수수료 제안

[SNET-RPC-030] Anzeon 체인에서 `eth_maxPriorityFeePerGas` 는 반드시 oracle 추정이 아니라 노드가 cache 한 거버넌스 gas tip (miner 가 마지막으로 읽은 GovValidator 값)을 돌려줘야 한다. `eth_gasPrice` 는 반드시 그 tip 에 현재 head 의 base fee 를 더한 값을 돌려줘야 한다.
Source: eth/api_backend.go:365-372, internal/ethapi/api.go:70-89
Observable: rpc

이 cache 는 block 을 import 할 때마다 별도 goroutine 에서 새로 고친다. 그래서 cache 는 잠깐 뒤처질 수 있고, batch import 중에는 잠깐 뒤로 돌아갈 수도 있다.

Source: core/blockchain.go:1855-1866

`eth_feeHistory` 의 reward 와 gas price oracle 은 Anzeon effective tip 을 쓴다. authorized 가 아닌 sender 에게는 header gas tip 이 effective tip 계산에 쓰인다. 이때 authorized 여부는 block 의 post-state (`header.Root`)에서 판정한다. 실행은 transaction 직전 state 로 판정하므로 (`B-07` SNET-TX-010), block 안에서 authorization 이 바뀌면 두 값이 다를 수 있다.

Source: eth/gasprice/feehistory.go:115-120, eth/gasprice/gasprice.go:254

### 9.2 Transaction 객체

`eth_getTransactionByHash` 가 돌려주는 transaction 객체와, 전체 transaction 을 담은 block 의 transaction 객체는 type `0x16` 에 대해 필드 네 개를 더한다.

| 필드 | Type | 값 |
|---|---|---|
| `feePayer` | `DATA(20)` | `tx.fee_payer` |
| `fv`, `fr`, `fs` | `QUANTITY` | fee payer signature |

Source: internal/ethapi/api.go:1414-1419, internal/ethapi/api.go:1478-1498

type `0x16` 에서 `from`, `v`, `r`, `s`, `yParity` (`= v`)는 `SenderTx` 에 담긴 sender 의 signature 값이다. `chainId`, `accessList`, `maxFeePerGas`, `maxPriorityFeePerGas`, `nonce`, `gas`, `to`, `value`, `input` 은 `SenderTx` 의 필드이다. type `0x04` 의 객체에는 `authorizationList` 가 더해진다. 이 필드는 `{chainId, address, nonce, yParity, r, s}` 의 배열이며, 숫자는 모두 `QUANTITY` 로 쓴다.

Source: internal/ethapi/api.go:1424-1441, internal/ethapi/api.go:1479-1497, internal/ethapi/api.go:1515-1529, core/types/tx_fee_delegation.go:117-146, core/types/gen_authorization.go:17-33

[SNET-RPC-031] type `0x02`, `0x04`, `0x16` 의 채굴된 transaction 에서 `gasPrice` 필드는, block 에 gas tip 이 있으면 반드시 `min(header_gas_tip + base_fee, max_fee_per_gas)` 여야 하고, 없으면 반드시 `min(max_priority_fee_per_gas + base_fee, max_fee_per_gas)` 여야 한다. legacy transaction 과 access-list transaction 에서는 반드시 그 transaction 자신의 gas price 여야 한다.
Source: internal/ethapi/api.go:1463-1476, internal/ethapi/api.go:1534-1550
Observable: rpc

이 값은 sender 의 authorization 을 무시한다. 그래서 authorized sender 에 대해서는 이 값이 실제로 청구된 가격과 다르다. 청구된 가격은 receipt 의 `effectiveGasPrice` (`B-07`, `SNET-TX-071`)다. legacy transaction 에서도 이 값은 청구된 가격과 다르다 (`B-07` 예 E-4).

receipt (`eth_getTransactionReceipt`)에는 fee payer 필드가 없다.

Source: internal/ethapi/api.go:1870-1906

### 9.3 Fee delegation 서명 method

| Method | Params | 동작 |
|---|---|---|
| `eth_signRawFeeDelegateTransaction` | `[args: TransactionArgs (feePayer required), input: DATA]` | `args.feePayer` 가 없으면 `missing FeePayer` 로 실패한다. `input` 은 type-`0x02` transaction 으로 디코딩되어야 하며, 아니면 `senderTx type error` 로 실패한다. 이 method 는 `input` 의 `v, r, s` 를 검증하지 않고 `SenderTx` 에 복사한다. 이 method 는 `input` 을 `args.feePayer` 와 함께 type `0x16` 으로 감싸고, unlock 된 로컬 key 로 fee payer 서명을 한다. `{raw, tx}` 를 돌려준다 |
| `personal_signRawFeeDelegateTransaction` | `[args, input, passwd]` | 위와 같지만, password 로 unlock 한다 |
| `feePayer` 를 준 `eth_sendTransaction`, `eth_signTransaction`, `personal_signTransaction` | 평소와 같다 | fee payer 의 wallet 으로 서명한다. 조립된 transaction 이 type `0x16` 이 아니면 요청을 거부한다 (`fee delegate tx type mismatch: got …, want 0x16`) |

Source: internal/ethapi/api.go:447-470, internal/ethapi/api.go:629-680, internal/ethapi/api.go:2204-2250, internal/ethapi/api.go:2430-2435

sender 가 서명한 type-`0x02` 입력은 그 자체로 유효한 transaction 이다 (`B-07 §4.2`).

[SNET-RPC-042] `TransactionArgs` 는 반드시 추가 필드 `feePayer` (`DATA(20)`), `v`, `r`, `s` (`QUANTITY`, type-`0x02` transaction 에 대한 sender 의 signature), `authorizationList` 를 받아야 한다. 기본값을 채운 뒤, 노드는 요청이 type-`0x02` transaction 으로 조립되고 (`maxFeePerGas` 가 있고, `authorizationList` 와 blob hash 가 없고) `feePayer`, `v`, `r`, `s` 가 모두 있을 때에만 `TransactionArgs` 로 type-`0x16` transaction 을 반드시 조립해야 한다. `authorizationList` 가 있으면 반드시 type-`0x04` transaction 을 조립해야 한다. `feePayer` 를 주었는데 다른 type 으로 조립되는 서명 요청은 위 표처럼 반드시 거부해야 한다.
Source: internal/ethapi/transaction_args.go:66-83, internal/ethapi/transaction_args.go:471-560, internal/ethapi/api.go:2428-2435
Observable: rpc

[SNET-RPC-043] `eth_call`, `eth_estimateGas`, `eth_createAccessList`, `debug_traceCall` 에서 노드는 반드시 `feePayer` 를 무시해야 한다. 시뮬레이션하는 message 에는 fee payer 가 없다. 그래서 sender 가 fee delegation 이 없는 transaction 과 같이 비용을 내고 검사를 받으며 (`B-07` SNET-TX-032), fee payer 의 잔고와 blacklist flag 는 검사하지 않는다.
Source: internal/ethapi/transaction_args.go:381-466, internal/ethapi/api.go:1186, internal/ethapi/api.go:1271, internal/ethapi/api.go:1698, eth/tracers/api.go:955
Observable: rpc

> 해설: block 안에서 실행하면 결과가 반대이다 (`B-07` SNET-TX-031). 잔고가 0 인 sender 가 fee payer 를 두어도 `eth_estimateGas` 는 `insufficient funds` 로 실패하고, fee payer 의 잔고가 0 이어도 `eth_call` 은 성공한다. 새 구현이 시뮬레이션에 fee payer 를 반영하면 go-stablenet 과 RPC 결과가 달라진다.

### 9.4 Header 필드

block 객체와 header 객체는 upstream 과 같다. `difficulty` 는 `0x1` 이고, `mixHash` 는 randao mix 이고, `totalDifficulty` 는 `TD(genesis) + number` 이고, `extraData` 는 `WBFTExtra` 의 raw RLP 다 (`A-03`). gas tip 은 `istanbul_getWbftExtraInfo` 로 decode 해야만 볼 수 있다.

Source: internal/ethapi/api.go:1299-1330, internal/ethapi/api.go:1376

### 9.5 `miner_setGasPrice`

[SNET-RPC-032] Anzeon 체인에서 `miner_setGasPrice` 는 반드시 pool tip 과 miner tip 을 바꾸지 않고 `false` 를 돌려줘야 한다.
Source: eth/api_miner.go:61-71
Observable: rpc

### 9.6 Account proof

[SNET-RPC-044] `eth_getProof` 는 반드시 결과에 필드 `extra` 를 더해야 한다. 이 필드는 계정의 `Extra` 값 (`B-04` SNET-SYS-070)을 `QUANTITY` 로 쓴 것이다. `Extra` 가 0 이면 반드시 이 필드를 빼야 한다. `accountProof` 는 StableNet 계정 인코딩에 대한 proof 이다. 그래서 검증하는 쪽은 `extra` 가 없으면 leaf 를 `rlp([nonce, balance, storageHash, codeHash])` 로, 있으면 `rlp([nonce, balance, storageHash, codeHash, extra])` 로 다시 만든다.
Source: internal/ethapi/api.go:730-740, internal/ethapi/api.go:826-837, core/types/state_account.go:28-37
Observable: rpc

### 9.7 일괄 제출

[SNET-RPC-045] `eth_sendRawTransactions` 를 제공하는 노드는 반드시 `DATA` 배열 하나를 인자로 받고, 반드시 같은 길이의 32 바이트 hash 배열을 돌려줘야 한다. 빈 배열이면 JSON `null` 을 돌려준다. 노드는 반드시 각 원소를 transaction 의 RLP network 인코딩으로 decode 해야 한다. legacy transaction 은 RLP list 이고, typed transaction 은 `type ‖ payload` 를 감싼 RLP byte string 이다. `eth_sendRawTransaction` 이 쓰는 binary 인코딩으로 decode 해서는 안 된다. decode 에 실패하거나 pool 이 거부한 원소의 결과는 반드시 zero hash 여야 하며, 그런 원소 때문에 호출 자체가 오류를 돌려줘서는 안 된다. 배열 길이에는 한도가 없다.
Source: internal/ethapi/api.go:2183-2200, core/types/transaction.go:161-191
Observable: rpc

### 9.8 추가 method

[SNET-RPC-046] `eth_getReceiptsByHash` 는 반드시 block hash 하나를 인자로 받아야 한다. 그 block 이 canonical 이면 반드시 그 block 에 대한 `eth_getBlockReceipts` 와 같은 배열을 돌려줘야 하고, block 을 모르거나 canonical 이 아니면 반드시 `null` 을 돌려줘야 한다.
Source: internal/ethapi/api.go:993-1026
Observable: rpc

`admin_peerInfo(id)` 는 주어진 node ID 를 가진 peer 의 `admin_peers` 항목을 돌려주고, 그 peer 가 연결되어 있지 않으면 `null` 을 돌려준다.

Source: node/api.go:318-325, p2p/server.go:1137-1152

[SNET-RPC-047] `eth_call`, `eth_estimateGas`, `eth_createAccessList`, `debug_traceCall` 이 받는 state override 객체는 계정마다 반드시 필드 `extra` (`QUANTITY`, 64 비트)를 받아야 하고, 실행 전에 반드시 계정의 `Extra` 를 그 값으로 바꿔야 한다. 그러면 `B-07` 의 blacklist 규칙과 authorization 규칙이 바꾼 flag 에 적용된다. 참조 구현은 이 필드에서 정의되지 않은 비트 0-61 을 걸러내지 않는다.
Source: internal/ethapi/api.go:1028-1062
Observable: rpc

### 9.9 Tracing 과 access list

[SNET-RPC-048] 내장 `prestateTracer` 는 보고하는 계정의 `Extra` 가 0 이 아니면 반드시 필드 `extra` 를 hex 문자열이 아닌 10 진 JSON 숫자로 내야 한다. diff 모드에서 post state 는 `Extra` 가 0 이 아닌 값으로 바뀐 계정에만 `extra` 를 담는다. `Extra` 가 0 으로 바뀐 것은 pre state 에서만 보인다.
Source: eth/tracers/native/prestate.go:42-48, eth/tracers/native/prestate.go:290-294, eth/tracers/native/prestate.go:362-368, eth/tracers/native/gen_account_json.go:15-31
Observable: rpc

비트 62 나 63 이 켜진 값은 모두 2^62 이상이므로, IEEE-754 double 이 정확하게 나타내는 정수 범위 (2^53)를 넘는다. flag 값 자체 (비트 62 와 63 만 켜진 값)는 double 로 정확하게 나타낼 수 있다. 그러나 JSON 숫자를 double 로 읽는 도구는 flag 와 함께 켜진 아래 비트를 잃는다. 아래 비트는 state override 로 넣을 수 있다 (SNET-RPC-047). 또 그런 도구는 32 비트 정수 연산으로 비트를 검사할 수 없다.

[SNET-RPC-049] `eth_createAccessList` 는 반드시 넷째 인자로 state override (SNET-RPC-047)를 받아야 한다. 또 돌려주는 목록에서 반드시 sender, 수신자 (또는 생성되는 주소), 활성 precompile, 활성 native manager (`B-07` §9.5), 그리고 chain ID 가 0 이거나 노드의 chain ID 이면서 signature 가 recover 되는 authorization 의 authority 를 빼야 한다. authorization 개수가 `gas / CallNewAccountGas` 를 넘으면 반드시 `insufficient gas to process all authorizations` 로 실패해야 한다.
Source: internal/ethapi/api.go:1603-1682
Observable: rpc

JavaScript tracer 의 `isPrecompiled(addr)` 는 native manager 주소에 대해서도 `true` 를 돌려준다.

Source: eth/tracers/js/goja.go:491-507

### 9.10 `admin_nodeInfo`

[SNET-RPC-050] `admin_nodeInfo` 에서 `protocols.eth.config` 는 반드시 `anzeon` 필드를 뺀 chain 설정이어야 한다. `boho` 와 `transitions` 는 들어간다. `protocols.istanbul.config` 는 `anzeon` 을 포함한 chain 설정 전체이므로, 도구는 WBFT 설정을 그곳에서 읽는다.
Source: eth/protocols/eth/handler.go:108-116
Source: eth/handler_istanbul.go:104-106, eth/protocols/eth/qlight_deps.go:30-32 (the `istanbul` protocol returns the node information without the copy), p2p/server.go:1103-1110 (one entry per protocol name)
Observable: rpc

---

## 10. 합의 관련 metric (informative)

| 이름 | 종류 | 뜻 | Source |
|---|---|---|---|
| `consensus/wbft/core/round` | meter | 새 round 가 시작될 때 round 증가를 기록한다 | consensus/wbft/core/core.go:49, :219 |
| `consensus/wbft/core/sequence` | meter | sequence 증가를 기록한다 | consensus/wbft/core/core.go:50, :189 |
| `consensus/wbft/core/consensus` | timer | proposal 을 받아들인 때부터 다음 sequence 를 시작할 때까지의 시간 | consensus/wbft/core/core.go:51, :192 |
| `consensus/wbft/core/timeout_round` | meter | round change timer 만료 횟수 | consensus/wbft/core/core.go:52, consensus/wbft/core/handler.go:260 |
| `consensus/wbft/core/commitwork` | timer | block 조립 시간 | miner/worker.go:92, :1414 |
| `chain/head/block`, `chain/head/header` | gauge | head 번호 | core/blockchain.go:57-58 |
| `chain/head/finalized`, `chain/head/safe` | gauge | `finalized`/`safe` block 번호. 이 값은 head 보다 뒤처진다 (§7.3) | core/blockchain.go:60-61 |

---

## 11. WBFT 체인의 Engine API

참조 `gstable` 바이너리는 개발자 모드가 아닌 모든 노드에 Engine API 를 등록한다. Engine API 는 `engine` namespace 이고, JWT 인증을 거쳐 auth-RPC endpoint 에서 제공된다. WBFT 노드도 예외가 아니다. 같은 method 는 JWT 없이 IPC endpoint 와 in-process 에서도 제공된다. 이 두 곳은 `personal` 을 뺀 모든 등록 API 를 받는다.

Source: cmd/gstable/config.go:220-236, eth/catalyst/api.go:44-53, node/node.go:379-398, node/node.go:501-508

Engine API 는 proof-of-stake beacon client 와 terminal total difficulty (TTD)를 전제로 한다. WBFT 체인에는 TTD 가 없다. 참조 구현의 동작은 다음과 같다.

| 호출 | WBFT 노드에서의 동작 |
|---|---|
| 모르는 head 를 준 `engine_forkchoiceUpdatedV*` | `SYNCING` 을 돌려준다. 그 head header 를 앞선 `engine_newPayloadV*` 가 보관해 두었으면 (parent 를 모르는 경우, `delayPayloadImport`), 먼저 `Merger.ReachTTD()` 를 부른다. 이 함수는 `{LeftPoW}` 를 데이터베이스에 영속한다. 이어서 downloader 를 취소하고 beacon sync 를 시작한다 |
| parent 를 모르는 `engine_newPayloadV*` | header 를 보관하고 (`remoteBlocks`) `SYNCING` 을 돌려준다 |
| 알려진 non-canonical head 를 준 `engine_forkchoiceUpdatedV*` | TTD 검사를 건너뛴다 (TTD 가 nil 이다). `SetCanonical(head)` 가 체인을 그 block 으로 되감거나 reorg 한다 |
| canonical branch 에 도달하는 모든 `forkchoiceUpdated` | 노드를 synced 로 표시한다 (`SetSynced`) |
| 0 이 아닌 `finalizedBlockHash` 를 준 `forkchoiceUpdated` | `Merger.FinalizePoS()` 를 부르고, 이 함수가 `{LeftPoW, EnteredPoS}` 를 데이터베이스에 영속한다. 그 뒤 `finalized` 와 `safe` 를 주어진 canonical block 으로 설정한다 |
| 알려진 parent 를 준 `engine_newPayloadV*` | nil TTD 를 역참조한다. RPC 계층이 panic 을 복구하고 `method handler crashed` 를 돌려준다 |
| `engine_exchangeTransitionConfigurationV1` | 실패한다 (`invalid ttd`) |

Source: eth/catalyst/api.go:238-371, eth/catalyst/api.go:538-640, eth/catalyst/api.go:645-660, eth/catalyst/api.go:415-443, consensus/merger.go:60-94, rpc/service.go:193-203

merger 상태가 `EnteredPoS` 가 되면 `eth` handler 는 체인을 merge 이후로 다룬다. 이 상태는 재시작해도 유지된다.

- fetcher validator 와 inserter 는 모든 block 을 거부하며 (`unexpected behavior after transition`), 여기에는 non-proposer 에서 로컬 WBFT 코어가 commit 한 block 도 포함된다.
- handler 는 peer 의 block announcement 와 block broadcast 를 거부하고, 그 peer 를 끊는다. 이때 오류는 `disallowed block announcement` 나 `disallowed block broadcast` 다.
- 노드는 block 전파를 멈춘다.
- chain syncer 는 downloader sync 를 다시는 시작하지 않는다 (`TDDReached`).

Source: eth/handler.go:229-235, eth/handler.go:250-264, eth/handler_eth.go:100-135, eth/handler.go:602-608, eth/sync.go:166-175

그래서 0 이 아닌 finalized hash 를 준 `engine_forkchoiceUpdated` 호출 한 번이 WBFT 노드가 체인을 따라가지 못하도록 영구히 멈춘다. 이 호출은 auth-RPC endpoint 에서는 JWT 인증을 거치고, IPC 에서는 JWT 없이 들어온다. `FinalizePoS` 는 finalized block 을 찾기 전에 실행된다. 그래서 finalized hash 를 몰라서 호출이 `Invalid forkchoice state` 를 돌려줄 때에도 상태는 영속된다. 그 상태의 validator 는 block 을 import 하지 못해서 체인에 기여하지 못하지만, 그 validator 의 합의 코어는 계속 투표할 수 있다.

`EnteredPoS` 없이 `{LeftPoW}` 만 있어도 비슷한 일이 일어난다. 이 상태는 parent 를 모르는 `engine_newPayload` 를 보낸 뒤 그 payload 를 head 로 준 `engine_forkchoiceUpdated` 를 보내면 만들어진다. 이때 finalized hash 는 0 이어도 된다. 그러면 chain syncer 는 downloader sync 를 다시는 시작하지 않는다 (`TDDReached`). fetcher inserter 는 로컬 합의가 commit 한 block 을 포함해 모든 block 을 버린다. TTD 가 nil 이면 어떤 block 도 terminal PoW block 이 아니기 때문이다. peer 의 announcement 와 broadcast 는 계속 받아들이고 노드는 block 전파도 계속한다. 그 검사들은 `EnteredPoS` 만 보기 때문이다.

Source: eth/catalyst/api.go:256-291, eth/catalyst/api.go:340-352, eth/catalyst/api.go:583-586, eth/handler.go:274-294, eth/sync.go:174-176, params/config.go:1058-1061, eth/handler_eth.go:103-105, eth/handler.go:602-608

[SNET-RPC-040] conforming WBFT 노드는 Engine API 를 노출하지 않는 것이 좋다 (SHOULD NOT). Engine API 를 노출한다면, 노드는 Engine API 호출이 canonical head, `finalized`/`safe` 태그, 영속하는 merge 전환 상태를 바꾸게 해서는 안 된다.
Source: cmd/gstable/config.go:229-235, eth/catalyst/api.go:322-371
Observable: rpc

---

## 12. eth protocol 과 snap wire protocol

StableNet 노드는 go-ethereum 과 같이 `eth/68` 과 `snap/1` 을 쓰며, 이 절에 적은 값과 차이만 다르다. 이 절에 없는 내용은 devp2p `eth/68` 명세와 `snap/1` 명세를 따른다. 참조 구현은 go-ethereum v1.13.15 이후 release 의 peer 별 request tracker 와 더 엄격한 응답 검사를 backport 했다 (ethereum#33835 등, go-stablenet commit `b23c5a831`). 그래서 v1.13.15 를 따라 만든 구현은 §12.2 와 §12.3 의 경우에 연결이 끊긴다.

### 12.1 Status handshake

[SNET-SYNC-061] 노드는 반드시 `eth` 버전 68 만 알려야 하고 다른 `eth` 버전을 알려서는 안 된다. 첫 `eth` 메시지가 `Status` 가 아닌 peer, 또는 `Status` 의 network ID, protocol 버전, genesis hash 가 다르거나 fork ID 를 EIP-2124 filter 가 거부하는 peer 와는 반드시 연결을 끊어야 한다.
Source: eth/protocols/eth/protocol.go:33-46, eth/protocols/eth/handshake.go:83-111, eth/protocols/eth/handler.go:197-202
Observable: network

[SNET-SYNC-062] network ID 는 반드시 노드 운영자가 설정한 값이어야 한다. 설정이 없으면 mainnet preset 에서는 반드시 `8282`, testnet preset 에서는 반드시 `8283`, 그 밖에는 반드시 chain ID 여야 한다.
Source: cmd/utils/flags.go:1645-1647, cmd/utils/flags.go:1759-1771, eth/backend.go:168-171
Observable: network

[SNET-SYNC-063] fork ID 는 반드시 EIP-2124 방식으로 계산해야 한다. 입력은 genesis hash (`B-02` §6)와 fork 목록이다. fork 목록은 chain 설정의 최상위 필드 가운데 이름이 `Block` 으로 끝나는 block 번호 (`homesteadBlock`, `daoForkBlock`, `eip150Block` … `mergeNetsplitBlock`, `applepieBlock`, `bohoBlock`)와 최상위 fork 시각 (`shanghaiTime`, `cancunTime`, `pragueTime`, `verkleTime`) 가운데 값이 있는 것을 모아 정렬하고 중복을 없앤 뒤, block 0 과 genesis 시각 이하의 시각을 뺀 것이다. `anzeon`, `boho`, `transitions` 와 WBFT parameter 는 fork ID 에 들어가서는 안 된다. preset 의 결과 값은 `B-01` §11.1 에 있다.
Source: core/forkid/forkid.go:242-296, eth/handler.go:385-386
Observable: network

[SNET-SYNC-064] block `b` 의 total difficulty 는 반드시 `genesis.Difficulty + b.Number` 여야 한다. genesis 뒤의 WBFT header 는 모두 difficulty 1 이다 (`A-01` WBFT-PARAM-010). genesis difficulty 는 mainnet preset 에서 1, testnet preset 에서 0 이다 (`B-02` SNET-GEN-003). 노드는 반드시 이 값을 `Status` 의 head TD 로 보내고, `NewBlock` 이 싣는 block 의 TD 로 보내야 한다. `NewBlock` 을 받은 노드는 `td - 1` 을 그 peer 의 head TD 로 기록하고, sync 대상을 고를 때 쓴다 (§4).
Source: eth/handler.go:376-387, eth/handler.go:617-630, eth/handler_eth.go:124-147
Observable: network

> 해설: `B-01` 은 `arrowGlacierBlock`, `grayGlacierBlock`, `mergeNetsplitBlock` 이 fork ID 에 들어간다고 적지만, StableNet 이 더한 `applepieBlock` 과 `bohoBlock` 도 같은 규칙으로 들어간다. 새 구현이 upstream 의 fork 목록만 쓰면 testnet 에서 fork ID 가 달라지고 EIP-2124 filter 가 연결을 거부한다. mainnet 은 모든 fork 가 block 0 이라 우연히 맞는다. TD 를 post-merge 방식으로 0 으로 보내거나 genesis difficulty 를 빼고 계산하면, go-stablenet 은 그 peer 를 sync 대상으로 고르지 않거나 잘못 고른다.

이 protocol 을 싣는 RLPx handshake 는 `auth` 메시지와 `ack` 메시지의 크기를 2048 바이트로 제한한다 (`A-07` WBFT-NET-052).

### 12.2 응답 검증

진행 중인 요청과 맞지 않는 응답을 받으면 받은 노드는 연결을 끊는다.

[SNET-SYNC-065] `eth/68` 요청이나 `snap/1` 요청에 답하는 노드는 반드시 그 요청의 응답 code 와 요청이 실어 온 request ID 로 답해야 하고, 요청한 것보다 많은 항목을 돌려줘서는 안 된다. `BlockHeaders` 는 `amount` 개 이하, `BlockBodies`, `Receipts`, `PooledTransactions`, `ByteCodes` 는 요청한 hash 하나에 항목 하나 이하, `TrieNodes` 는 요청한 path 하나에 node 하나 이하이다. `AccountRange` 의 account 목록과 `StorageRanges` 의 slot 목록은 RLP 내용 길이를 반드시 요청한 `bytes` 의 두 배 안에 두어야 하고, `AccountRange` 나 `StorageRanges` 에 proof node 를 128 개보다 많이 보내서는 안 된다. 노드는 받지 않은 요청에 대한 응답을 보내서는 안 된다.
Source: p2p/tracker/tracker.go:211-245, eth/protocols/eth/dispatcher.go:199-212, eth/protocols/eth/peer.go:338-484, eth/protocols/eth/handlers.go:327-379, eth/protocols/eth/handlers.go:473-512, eth/protocols/eth/handlers.go:588-609, eth/protocols/snap/peer.go:90-181, eth/protocols/snap/handler.go:172-322
Observable: network

[SNET-SYNC-066] 참조 구현은 SNET-SYNC-065 를 어긴 응답을 보낸 peer 와 연결을 끊는다. 또 peer 별 tracker 에서 요청이 만료된 뒤 (`eth` 는 5 분, `snap` 은 1 분) 도착한 응답도 더는 어떤 요청과도 맞지 않으므로, 그 응답을 보낸 peer 와 연결을 끊는다. 노드는 이 시간 안에 답하는 것이 좋다 (SHOULD).
Source: eth/protocols/eth/peer.go:119, eth/protocols/snap/peer.go:51, p2p/tracker/tracker.go:142-185
Observable: network

go-ethereum v1.13.15 에서 tracker 는 metric 만 기록했다. v1.13.15 에서도 요청하지 않은 `BlockHeaders`, `BlockBodies`, `Receipts` 응답은 request dispatcher 가 `errDanglingResponse` 로 연결을 끊었다. 그러나 항목 수 초과, 요청하지 않은 `PooledTransactions`, 위의 `snap` 경우는 모두 연결을 끊지 않았다.

> 해설: 새 구현이 go-stablenet 에 header, body, receipt, transaction, snap 데이터를 줄 때 이 한도를 넘으면 go-stablenet 이 연결을 끊는다. v1.13.15 는 snap 응답 크기를 `bytes` 의 soft limit 로만 다루었으므로, 이 한도를 크게 넘기는 snap server 는 v1.13.15 에서는 문제가 없지만 go-stablenet 에서는 끊긴다. 반대 방향으로 go-stablenet 이 보내는 응답은 이 한도 안에 있다.

### 12.3 Transaction 전파

[SNET-SYNC-067] 노드는 `Transactions` 메시지 하나에 transaction 을 5000 개보다 많이 보내서는 안 되고, `Transactions` 나 `PooledTransactions` 메시지 하나에 같은 transaction 을 두 번 넣어서는 안 되고, `Transactions` 에 type-`0x03` transaction 을 넣어서는 안 되고, `PooledTransactions` 에 sidecar 없는 type-`0x03` transaction 을 넣어서는 안 된다. 참조 구현은 transaction 을 받는 동안 (synced 로 표시된 뒤, §6) 이를 어긴 peer 와 연결을 끊는다. 그 전에는 두 메시지를 무시한다.
Source: eth/protocols/eth/protocol.go:51-52, eth/protocols/eth/handlers.go:572-609, eth/handler_eth.go:73-91, eth/handler_eth.go:150-178
Observable: network

transaction fetcher 도, 전달된 transaction 이 pool 의 KZG 검사에서 실패하면 (`ErrKZGVerificationError`) 그 전달의 나머지 transaction 을 버리고 보낸 peer 와 연결을 끊는다. Anzeon 체인에서는 type-`0x03` transaction 이 sender recover 단계에서 거부되므로 (`B-07` §4.1), 이 경로에 도달하는 경우는 찾지 못했다.

Source: eth/fetcher/tx_fetcher.go:340-357, eth/fetcher/tx_fetcher.go:728-732

> 해설: 연결이 끊기면 같은 연결의 합의 메시지 (`istanbul/100`)도 끊긴다. 그래서 validator 사이의 연결에서 이 규칙을 어기면 liveness 에 영향이 간다 (`A-07` WBFT-NET-050).

### 12.4 Bootstrap 노드 (informative)

go-stablenet 은 flag 나 설정 파일로 bootnode 를 주지 않으면 `params/bootnodes.go` 의 StableNet mainnet enode 여덟 개 (TCP port 8589)를 기본 bootstrap 노드로 쓴다. `--mainnet` 없이 custom genesis 를 쓰는 경우도 마찬가지이다. `--testnet` 이면 testnet enode 두 개를 쓴다. 두 network 모두 DNS discovery tree 는 설정되어 있지 않다. 그래서 custom genesis 를 쓰고 bootnode 를 설정하지 않은 노드는 mainnet bootnode 에 연결을 시도한다. fork ID 가 다르므로 `eth` handshake 는 실패하지만, 양쪽 discovery table 은 서로를 알게 된다.

Source: params/bootnodes.go:20-35, params/bootnodes.go:108-126, cmd/utils/flags.go:1008-1030, cmd/utils/flags.go:1878-1891

---

## 13. 노드 기본값 (informative)

아래는 go-stablenet 의 기본값 가운데 go-ethereum v1.13.15 와 다르고 RPC 나 network 로 보이는 것이다. WBFT timer, block 주기, epoch 길이에는 명령행 flag 가 없고 genesis 설정에서 온다 (`B-01` §6, `A-06` §3). 기본 sync 방식 (full)은 SNET-SYNC-001 에 있다.

| 설정 | go-stablenet | go-ethereum v1.13.15 | 보이는 효과 |
|---|---|---|---|
| `RPCTxFeeCap` (`--rpc.txfeecap`) | 0 (상한 없음) | 1 ether | `eth_sendTransaction` 과 `eth_signTransaction` 은 수수료 때문에 transaction 을 거부하지 않는다 |
| `TransactionHistory`, `TxLookupLimit` (`--history.transactions`) | 31 536 000 block | 2 350 000 block | index 보다 오래된 transaction 에 대해 `eth_getTransactionByHash` 와 `eth_getTransactionReceipt` 는 `null` 을 돌려준다 (block 주기가 1 초이면 약 1 년) |
| pool 의 transaction 최대 크기 | 262 144 바이트 (32 KiB slot 8 개) | 131 072 바이트 | 이보다 큰 transaction 은 pool 이 거부하고 gossip 하지 않는다. block 안에서는 여전히 유효하다 |
| `ForceSyncCycle`, `TdSyncInterval` (`--sync.forcecycle`, `--sync.tdinterval`) | 10 초, 10 초 | force cycle 은 10 초 상수이고 TD 정체 검사는 없다 | SNET-SYNC-020 과 SNET-SYNC-021 이 쓴다 |
| `GasCeil` (`--miner.gaslimit`) | 105 000 000 | 30 000 000 | 이 노드가 제안하는 block 의 header `GasLimit` 이 이 목표 쪽으로 움직인다 (`B-03` SNET-BHDR-013) |

Source: eth/ethconfig/config.go:55-81, cmd/utils/flags.go:283-290, cmd/utils/flags.go:518-524, core/txpool/legacypool/legacypool.go:45-58, cmd/utils/flags.go:782-793

go-stablenet 의 traffic 을 relay 하는 구현은 262 144 바이트까지의 transaction 을 pool 에 받고 relay 하는 것이 좋다. 한도가 더 낮으면 go-stablenet 노드가 gossip 하는 transaction 을 버리게 된다.
