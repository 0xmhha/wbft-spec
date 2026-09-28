# A-07 네트워크

- Status: draft
- Area code: `NET`
- Reference: go-stablenet `740526d03`. 여기서 인용하는 파일은 §12 가 따로 밝힌 경우를 빼면 `v1.1.0` 에서도 같다.

WBFT 노드는 이름이 `istanbul` 이고 버전이 `100` 인 devp2p subprotocol 로 합의 메시지를 주고받는다. 이 subprotocol 은 같은 RLPx 연결에서 `eth` 프로토콜 옆에서 돈다. 이 장은 그 subprotocol 을 정한다. 이 장이 다루는 내용은 다음과 같다.

- 노드가 subprotocol 을 어떻게 식별하고 협상하는가
- subprotocol 이 `eth` handshake 에 어떻게 의존하는가
- 메시지 frame 의 정확한 형식과 크기 한도는 무엇인가
- 받는 노드가 메시지 코드마다 무엇을 하는가
- 두 dedup cache 는 무엇이고 그 키는 어떻게 계산하는가
- 노드가 어느 peer 에게 메시지를 보내고 relay 하는가
- 노드가 어떤 조건에서 메시지를 버리거나, 무시하거나, 받아들이거나, 연결을 끊는가
- 프로토콜이 liveness 를 가지려면 네트워크 topology 가 어떤 모양이어야 하는가

메시지 본문(PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE)과 그 signing payload 는 `A-03` 에서 정의한다. 메시지가 consensus core 에 도착한 뒤 어떻게 처리되는지는 `A-05` 에서 정의한다. 이 장은 그 처리가 성공했는지 실패했는지를 노드가 메시지를 relay 할지 정하는 데에만 쓴다.

---

## 1. 용어

| 용어 | 뜻 |
|---|---|
| *connection* | devp2p `Hello` 교환을 마치고 원격 노드와 맺은 devp2p(RLPx) 세션이다. |
| *peer address* | `address_of(pubkey)` 이다. 원격 노드의 secp256k1 devp2p public key 에서 Ethereum 계정 주소와 같은 방식(`A-02`)으로 끌어낸 20 바이트 주소이다. |
| *eth peer* | `eth` 프로토콜 handshake(`Status`)가 성공했고 노드의 peer 집합에 등록된 connection 이다. |
| *consensus link* | `istanbul/100` 도 협상되어 붙어 있는 eth peer 이다(§3). |
| *code* | `istanbul` 코드 공간의 시작을 기준으로 한 상대 메시지 코드이며, 범위는 `0x00..0x15` 이다. |
| *wire code* | RLPx frame 에 실제로 쓰이는 코드 `offset + code` 이다(§2.2). |
| *payload* | frame 에서 코드 뒤에 실리는 메시지 본문 바이트이다. snappy 압축 전의 값을 뜻한다. |
| *message key* | `dedup_key(data) = keccak256(rlp_encode(data))` 이다. 여기서 `data` 는 RLP 바이트 문자열로 다룬다(§5.2, 정의는 `A-03` WBFT-MSG-060). |
| *validator set* | 노드가 현재 합의 중인 height `h` 에 대한 `validators_at(h)` 이다(`A-04`). |
| *core* | `A-05` 의 합의 state machine 이다. "engine 이 돌고 있다" 는 core 가 시작되었고 멈추지 않았다는 뜻이다. |

---

## 2. Subprotocol 식별

### 2.1 Capability

[WBFT-NET-001] WBFT 노드는 devp2p `Hello` 메시지에서 반드시 capability `("istanbul", 100)` 을 광고해야 하며, 이 capability 의 메시지 코드 공간의 길이는 22(코드 `0x00..0x15`)이다.
Source: eth/quorum_protocol.go:38-42, eth/quorum_protocol.go:52-54, eth/handler_istanbul.go:61-67
Observable: network

[WBFT-NET-002] WBFT 노드는 반드시 `("eth", 68)`(길이 17)도 광고해야 한다. 노드는 `("snap", 1)`(길이 8)을 광고해도 된다.
Source: eth/protocols/eth/protocol.go:42-46, eth/protocols/snap/protocol.go:39-43, eth/backend.go:512-524
Observable: network

구현 노트 (참고). reference 구현은 Anzeon 이 없는 chain 설정에서도 `istanbul/100` 을 무조건 광고한다. `eth/backend.go:518-522` 의 조건문이 상수 이름 `"istanbul"` 을 `"eth"` 와 비교하므로 언제나 참이 되기 때문이다. `istanbul` 프로토콜 항목은 `eth` 프로토콜의 `NodeInfo`, `PeerInfo`, ENR 항목(`eth` fork ID), dial 후보를 그대로 다시 쓴다(`eth/handler_istanbul.go:104-111`). `istanbul` 은 자기 ENR 항목을 추가하지 않는다.

[WBFT-NET-052] 노드가 보내는 RLPx (EIP-8) auth 메시지나 ack 메시지의 크기 prefix 는 2048 을 넘어서는 안 된다. reference 구현은 이보다 큰 메시지를 받으면 handshake 를 실패시킨다. go-ethereum v1.13.15 에는 이 한도가 없다.
Source: p2p/rlpx/rlpx.go:600-612
Observable: network

그 밖의 RLPx handshake 는 upstream 과 같고, 다른 점이 하나 더 있다. 노드는 handshake 에서 받은 public key 나 ECIES 점이 곡선 위에 있지 않으면, 그 점에 자기 node key 나 ephemeral key 를 곱하기 전에 거부한다. 이 규칙은 `A-10` WBFT-SEC-032 이다. go-ethereum v1.13.15 는 이 점을 검사하지 않는다 (`A-10` §4).

### 2.2 Wire code

devp2p 는 협상된 capability 마다 기본 프로토콜 코드 16 개 뒤에 연속된 코드 범위를 배정한다. capability 는 이름순, 그다음 버전순으로 정렬되며, 협상에 성공한 capability 만 센다.

[WBFT-NET-003] `istanbul` 메시지의 wire code 는 반드시 `offset + code` 여야 한다. 여기서 `offset = 16 + Σ length(c)` 이고, 합은 이름이 `"istanbul"` 보다 앞에 정렬되는 협상된 capability `c` 에 대해 구한다. consensus link 가 동작하려면 `eth/68` 이 협상되어 있어야 하며(§3), 이때 `offset = 16 + 17 = 0x21` 이다.
Source: p2p/peer.go:45, p2p/peer.go:401-423 (`matchProtocols`), p2p/peer.go:471-480 (`WriteMsg` adds the offset), p2p/peer.go:452-459 (`getProto`)
Observable: network

| 메시지 | code | wire code (offset `0x21`) |
|---|---|---|
| 레거시 `ISTANBUL_MSG` | `0x11` | `0x32` |
| PRE-PREPARE | `0x12` | `0x33` |
| PREPARE | `0x13` | `0x34` |
| COMMIT | `0x14` | `0x35` |
| ROUND-CHANGE | `0x15` | `0x36` |
| (NewBlock 엿보기, §5.6) | `0x07` | `0x28` |

`snap` 은 이름순으로 `istanbul` 뒤에 오므로, `snap` 이 있어도 `istanbul` 의 offset 은 바뀌지 않는다.

> 해설: 패킷 캡처로 분석할 때는 상대 코드(`0x12` 등)와 wire code(`0x33` 등)를 혼동하지 않도록 주의한다.

구현 노트 (참고). p2p metric 은 프로토콜과 상대 코드별로 등록된다. 예를 들어 `p2p/ingress/istanbul/100/0x12` 와 `p2p/egress/istanbul/100/0x12` 가 있다. ingress 는 `p2p/peer.go:373-377` 에서, egress 는 `p2p/transport.go:101-105` 에서 기록되며, 둘 다 metric 이 켜져 있을 때만 기록된다. 이 metric 으로 노드마다 합의 메시지를 종류별로 몇 개 주고받았는지 알 수 있지만, 그 메시지가 어느 peer 와 오갔는지는 알 수 없다.

---

## 3. eth 프로토콜에 대한 의존

`istanbul/100` 에는 handshake 가 없다. 두 노드 사이의 유일한 호환 검사는 같은 connection 에서 이루어지는 `eth` `Status` 교환이다(network ID, total difficulty, head, genesis hash, fork filter 를 쓰는 fork ID, `B-09` §12.1). consensus link 는 이 교환이 성공하고 eth peer 가 등록된 뒤에만 붙는다.

[WBFT-NET-004] 노드는 `istanbul/100` 에서 어떤 handshake 메시지도 보내거나 기대해서는 안 된다. connection 의 첫 `istanbul` 메시지는 합의 메시지여도 된다.
Source: eth/handler_istanbul.go:69-103
Observable: network

[WBFT-NET-005] 노드는 connection 의 `eth` handshake 가 성공하고 원격 노드가 eth peer 로 등록되기 전에는 그 connection 의 `istanbul` 메시지를 처리해서는 안 된다. reference 구현은 그때까지 `istanbul` stream 을 읽지 않는다.
Source: eth/handler_istanbul.go:81-97, eth/handler.go:363-490 (registration; `EthPeerRegistered` signalled at 490)
Observable: network

[WBFT-NET-006] 노드는 connection 의 `istanbul` stream 이 등록된 eth peer 에 붙기 전에는 그 connection 에 합의 메시지를 보내서는 안 된다. reference 구현에서는 `istanbul` stream 이 붙지 않은 eth peer 에게 보낸 메시지가 오류 없이 버려진다. stream 이 아직 붙지 않은 경우와 `istanbul/100` 이 협상되지 않은 경우가 모두 여기에 해당한다. 그런데도 그 peer 는 그 message key 를 받은 것으로 기록되므로(WBFT-NET-032), 그 키가 그 주소의 peer 별 recent cache(§5.2)에 남아 있는 동안 그 메시지는 그 peer 에게 다시 보내지지 않는다.
Source: eth/protocols/eth/peer.go:519-525 (`SendWBFTConsensus` returns `nil` when `consensusRw == nil`), eth/protocols/eth/peer.go:527-530, consensus/wbft/backend/backend.go:188-206
Observable: network

[WBFT-NET-007] `eth` handshake 가 실패하거나 원격 노드를 eth peer 집합에 등록하는 데 실패하면, 노드는 반드시 그 connection 의 `istanbul` 프로토콜을 닫아야 하고, 그 결과 connection 도 닫힌다. 그 밖의 eth 실패 경로에서는 reference 구현이 `istanbul` 프로토콜을 닫지 않는다(아래 참고).
Source: eth/handler_istanbul.go:91-101, eth/handler.go:386-392, eth/handler.go:412-418
Observable: network

reference 구현은 그 두 eth 실패 경로(handshake 실패와 peer 집합 등록 실패)에서만 `istanbul` 프로토콜 handler 에 신호를 보낸다. 다음 경로에서는 `istanbul` 프로토콜 handler 가 계속 기다린다(`eth/handler.go:363-453`).

- handler 가 종료되는 중이어서 `DiscQuitting` 이 나는 경우
- snap 확장을 기다리다 실패하는 경우
- `DiscTooManyPeers` 가 나는 경우
- "peer dropped during handling" 이 나는 경우
- downloader 나 snap syncer 에 peer 를 등록하다 실패하는 경우
- required block 요청이 실패하는 경우

`istanbul` stream 이 붙기 전에 connection 으로 `istanbul` frame 이 도착하면, 그 frame 은 connection 의 read loop 를 막는다. devp2p 는 subprotocol 메시지를 버퍼 없는 channel 로 전달하기 때문이다(`p2p/peer.go:378-383`). 그래서 같은 connection 의 뒤따르는 `eth` frame 도 함께 기다린다. 이 상태는 eth 등록이 끝날 때까지만 이어지고 교착 상태가 되지는 않는다. 적합한 원격 노드는 합의 메시지보다 먼저 `eth` `Status` 를 보내기 때문이다.

---

## 4. Framing

### 4.1 Frame 내용

합의 메시지 하나는 devp2p 메시지 하나로 전달된다. payload 는 `A-03` 의 메시지 RLP 인코딩을 그대로 쓴 것이다. payload 를 다시 RLP 바이트 문자열로 감싸지 않으며, 이 점이 레거시 코드 `0x11` 과 다르다(§4.2).

[WBFT-NET-010] 코드가 `c ∈ {0x12, 0x13, 0x14, 0x15}` 인 합의 메시지 `m` 을 보낼 때, 노드는 반드시 wire code 가 `offset + c` 이고 payload 가 `rlp_encode(m)`(`A-03`)인 devp2p 메시지 하나를 바꾸지 않고 써야 한다. devp2p 메시지 크기는 반드시 `len(rlp_encode(m))` 이어야 한다. 그러므로 RLPx frame 데이터는 양쪽이 devp2p 버전 5 이상을 쓰면 `rlp_encode(offset + c) ‖ snappy(rlp_encode(m))` 이고, 그렇지 않으면 `rlp_encode(offset + c) ‖ rlp_encode(m)` 이다.
Source: p2p/message.go:109-113 (`SendWithNoEncoding`), eth/protocols/eth/peer.go:519-525, consensus/wbft/backend/backend.go:202-206, p2p/rlpx/rlpx.go:207-242
Observable: network

[WBFT-NET-011] 노드는 코드 `0x11` 을 보내서는 안 된다. 노드는 `0x12..0x15` 가 아닌 어떤 `istanbul` 코드도 보내서는 안 된다.
Source: consensus/wbft/backend/backend.go:202-205 (`0x11` is used only for codes outside `0x12..0x15`, which the core never produces), consensus/wbft/messages/message.go:28-43
Observable: network

### 4.2 레거시 코드 0x11

[WBFT-NET-012] 코드 `0x11` 을 받으면, 노드는 반드시 payload 를 RLP 바이트 문자열 하나로 decode 하고 그 내용을 메시지 데이터 `data` 로 써야 한다. payload 가 유효한 RLP 바이트 문자열로 시작하지 않으면, 노드는 반드시 그 peer 와의 연결을 끊어야 한다(§8). 노드는 첫 RLP 항목 뒤의 바이트를 무시한다.
Source: consensus/wbft/backend/handler.go:54-60, consensus/wbft/backend/handler.go:78-81, p2p/message.go:56-61
Observable: network

decode 에 성공한 `0x11` 메시지는 코드가 `0x11` 인 합의 메시지로 다뤄지며, core 는 그 메시지를 거부한다(§8, IGNORE 등급). 이 메시지가 남기는 지속적인 효과는 dedup cache 에 대한 것뿐이다(§5.2).

### 4.3 크기 한도

[WBFT-NET-013] peer 가 보낸 `istanbul` 메시지의 payload 를 snappy 압축을 푼 뒤 잰 길이가 `MAX_ISTANBUL_MSG_SIZE` = `10 485 760` 바이트(10 MiB, reference 의 `protocolMaxMsgSize`)보다 길면, 노드는 반드시 그 peer 와의 연결을 끊어야 한다. 이와 별도로 devp2p 는 압축을 푼 payload 가 `16 777 215` 바이트(`2^24 − 1`)를 넘는 모든 frame 을 거부하고 연결을 끊는다.
Source: eth/handler.go:59 (`protocolMaxMsgSize`), eth/handler_istanbul.go:138-140, p2p/rlpx/rlpx.go:146-155
Observable: network

---

## 5. 받는 경로

### 5.1 코드

[WBFT-NET-020] consensus link 로 `istanbul` 메시지를 받을 때마다, 노드는 반드시 상대 코드에 따라 다음과 같이 동작해야 한다.

| Code | engine 이 돌고 있음 | engine 이 멈췄고 노드가 동기화 중 | engine 이 멈췄고 동기화 중이 아님 |
|---|---|---|---|
| `0x12..0x15` | §5.3 | 버린다 (DROP-SILENT) | 연결을 끊는다 (§5.4) |
| `0x11` | §4.2 를 거친 뒤 §5.3 | 버린다 (DROP-SILENT) | 연결을 끊는다 (§5.4) |
| `0x07` | §5.6 (효과 없음) | 버린다 | 버린다 |
| 그 밖의 `0x00..0x10` | 버린다 | 버린다 | 버린다 |

Source: consensus/wbft/backend/handler.go:69-131, eth/handler_istanbul.go:115-153
Observable: network

코드 `0x16` 이상은 `istanbul` 범위 밖이다. devp2p 는 그 코드를 다음 capability 의 범위로 보내고, 맞는 범위가 없으면 "msg code out of range" 로 연결을 끊는다(`p2p/peer.go:368-372`).

[WBFT-NET-021] 코드 `0x12..0x15` 에 대해 노드는 반드시 payload 전체를 메시지 데이터 `data` 로 써야 한다. payload 가 비어 있으면 reference 구현은 그 peer 와의 연결을 끊는다. 빈 payload 에서 0 바이트를 읽으면 end-of-file 이 보고되고, 그 end-of-file 이 decode 실패로 다뤄지기 때문이다.
Source: consensus/wbft/backend/handler.go:60-64, consensus/wbft/backend/handler.go:78-81
Observable: network

### 5.2 Dedup cache

노드는 메시지 키를 담는 in-memory cache 두 개를 둔다.

- **peer 별 recent cache** 는 peer 주소마다, 노드가 그 peer 에게서 받았거나 그 peer 에게 보낸 키를 기록한다. 이 cache 는 peer 주소를 최대 `INMEMORY_PEERS` = 40 개까지 담고, 주소마다 키를 최대 `INMEMORY_MESSAGES` = 1 024 개까지 담는다(`A-01`). 어느 한도든 넘치면 cache 는 가장 오래 쓰지 않은 항목을 밀어낸다. 이 cache 는 connection 이 아니라 peer 주소를 키로 쓰므로 재연결 뒤에도 남는다.
- **known cache** 는 노드가 이미 자기 core 에 전달했거나 스스로 보낸 키를 기록한다. 이 cache 는 키를 최대 `INMEMORY_MESSAGES` = 1 024 개까지 담고, 한도를 넘치면 가장 오래 쓰지 않은 키를 밀어낸다.

[WBFT-NET-022] 메시지 키는 반드시 `data` 를 RLP 바이트 문자열로 인코딩한 `dedup_key(data) = keccak256(rlp_encode(data))`(`A-03` WBFT-MSG-060)여야 한다. 구체적으로 다음과 같다.

1. `len(data) = 1` 이고 `data[0] < 0x80` 이면 `keccak256(data)` 이다.
2. 그렇지 않고 `len(data) <= 55` 이면 `keccak256(0x80 + len ‖ data)` 이다.
3. 그 밖의 경우에는 `keccak256(0xb7 + len(be(len)) ‖ be(len) ‖ data)` 이다. 여기서 `be` 는 최소 길이의 big-endian 인코딩이다.

메시지 코드는 키에 포함되지 않는다.
Source: consensus/wbft/utils.go:31-36 (`RLPHash`), consensus/wbft/backend/handler.go:66, consensus/wbft/backend/backend.go:177
Observable: network

[WBFT-NET-023] engine 이 돌고 있는 동안 peer 주소 `a` 에게서 코드가 `0x11..0x15` 인 `data` 를 받으면, 노드는 반드시 `dedup_key(data)` 를 `a` 의 peer 별 recent cache 에 추가해야 한다. 노드는 반드시 known cache 검사보다 먼저 이 추가를 해야 하며, 나중에 그 메시지가 유효한 것으로 판명되는지와 관계없이 추가해야 한다.
Source: consensus/wbft/backend/handler.go:82-88
Observable: network

[WBFT-NET-024] `dedup_key(data)` 가 known cache 에 있으면, 노드는 반드시 그 메시지를 버려야 한다(DROP-SILENT). 그렇지 않으면 노드는 반드시 그 키를 known cache 에 추가하고 `(code, data)` 를 core 에 전달해야 한다. 키는 core 가 메시지를 검사하기 전에 추가된다.
Source: consensus/wbft/backend/handler.go:90-99
Observable: network

### 5.3 Core 로 전달

[WBFT-NET-025] 노드는 한 peer 에게서 받았든 여러 peer 에게서 받았든, 메시지가 받은 순서대로 처리된다고 가정해서는 안 된다. reference 구현은 각 메시지를 비동기로 core 에 넘긴다.
Source: consensus/wbft/backend/handler.go:96-99 (`go ...Post`), consensus/wbft/backend/backend.go:171
Observable: network

core 는 메시지를 decode 하고 verify 한 뒤 `A-05` 가 정한 대로 처리한다. 그 처리 결과에 따라 노드가 메시지를 relay 할지(§7), 그리고 메시지가 §8 의 어느 등급에 속할지가 정해진다.

[WBFT-NET-026] peer 주소는 반드시 중복 제거(§5.2)와 송신 대상 선택(§6)에만 쓰여야 한다. peer 주소를 합의 메시지를 인증하는 데 써서는 안 된다. 메시지는 signature 에서 recover 한 validator 에게 귀속된다(`A-05`). 그러므로 validator `V` 가 서명한 메시지는 validator 가 아닌 peer 를 포함한 어떤 peer 에게서 와도 받아들여진다.
Source: eth/handler_istanbul.go:163-167, consensus/wbft/core/handler.go:268-317
Observable: network

### 5.4 Engine 이 멈춘 경우

[WBFT-NET-027] core 가 돌지 않는 노드가 코드 `0x11..0x15` 를 받으면, 그 순간 block downloader 가 동기화 중이면 노드는 반드시 그 메시지를 버리고 connection 을 유지해야 하고, 동기화 중이 아니면 반드시 그 peer 와의 연결을 끊어야 한다.
Source: consensus/wbft/backend/handler.go:73-76, eth/handler_istanbul.go:118-127
Observable: network

core 가 돌지 않는 노드는 두 종류이다. 첫째, block 을 생산하지 않는 노드이다. block 생산이 꺼져 있거나, core 를 시작한 적이 없는 validator 가 아닌 노드가 여기에 속한다. 둘째, block 을 생산하는 노드가 동기화를 시작한 때부터 동기화 뒤 생산을 다시 시작할 때까지의 구간이다. 이 구간은 프로세스가 처음으로 동기화에 성공할 때까지만 생긴다(`miner/miner.go:139-161`, `B-09` SNET-SYNC-030). 적합한 송신자는 validator peer 에게만 메시지를 보내므로(§6.2), validator 가 아닌 노드는 보통 합의 메시지를 받지 않는다. 그러나 block 생산 없이 돌고 있는 validator 는 자기에게 메시지를 보내는 모든 validator 와의 연결을 끊고, 그 validator 들은 다시 연결해서 다시 보낸다.

### 5.5 연결 끊기 이유 (참고)

`istanbul` handler 가 오류를 돌려주면 devp2p 는 connection 을 닫고 이유 `0x10`(subprotocol error)을 담은 `Disconnect` 를 보낸다(`p2p/peer.go:295-297`, `p2p/peer_error.go:102-119`, `p2p/transport.go:109-128`). devp2p read loop 자체에서 생긴 오류(frame 오류, 범위를 벗어난 코드)는 이유 `0x01`(network error)로 connection 을 닫으며, 이때는 `Disconnect` 메시지를 보내지 않는다.

### 5.6 합의 stream 의 NewBlock 코드

[WBFT-NET-028] `istanbul/100` 으로 받은 코드 `0x07` 메시지는 반드시 합의 state 에 아무 효과도 없어야 한다. 그 메시지가 크기 한도를 넘지 않는 한, 노드는 그 메시지 때문에 연결을 끊어서는 안 된다.
Source: consensus/wbft/backend/handler.go:102-130, eth/handler_istanbul.go:145-152
Observable: network

구현 노트 (참고). 노드가 현재 proposer 이면 reference 구현은 payload 를 `{Block, TD}` 로 decode 한다. 그리고 그 block 의 difficulty 가 1 이고 노드가 현재 진행 중인 제안과 같으면 `WBFT: block already proposed` 를 로그에 남긴다. 어느 경우든 결과는 버려진다. 이 동작은 Quorum 에서 물려받은 것이다(commit `5ffacc48` 의 `consensus/istanbul/backend/handler.go:104-131`). Quorum 의 옛 consensus 프로토콜 `istanbul/64` 와 `istanbul/99` 는 consensus handler 가 처리하지 않은 메시지를 `eth` handler 로 넘긴다(`eth/handler.go:780-811`). 그래서 Quorum 에서는 같은 handler 가 `eth` stream 도 처리했다. Quorum 은 `MixDigest == IstanbulDigest` 로 block 을 알아보고, reference 는 `Difficulty == 1` 로 알아본다. go-stablenet 에서는 `eth` 메시지가 이 handler 에 도달하지 않으므로, 적합한 peer 에게서는 이 경로에 도달할 수 없다.

---

## 6. 보내는 경로

### 6.1 Broadcast

core 는 두 가지 송신 연산을 쓴다. *Broadcast* 는 노드가 스스로 만든 메시지(PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE, `A-05`)에 쓴다. *Gossip* 은 relay(§7)에 쓰이고, Broadcast 도 내부에서 Gossip 을 쓴다.

[WBFT-NET-030] 노드가 스스로 만든 메시지를 broadcast 할 때, 노드는 반드시 먼저 자기 주소가 core 가 넘긴 validator 집합에 있는지 확인해야 한다. 주소가 없으면 노드는 반드시 아무것도 보내지 않아야 한다. 이때 노드는 peer 에게도 자기 자신에게도 보내지 않는다. 주소가 있으면 노드는 반드시 그 메시지를 gossip 해야 하고(§6.2), 반드시 그 메시지를 받은 것처럼 자기 core 에 전달해야 한다. 이때 그 메시지는 WBFT-NET-024 의 known cache 검사를 거치지 않는다.
Source: consensus/wbft/backend/backend.go:152-173
Observable: network, log
Observed as: the log line `WBFT: invalid validator` (Error).

노드는 자기가 만든 메시지를 자기 core 에도 전달하므로, 노드 자신의 PREPARE 와 COMMIT 은 일반 수신 경로를 거쳐 자기 quorum 에 들어간다. 그리고 노드는 자기 메시지를 처리한 뒤 그 메시지를 다시 relay 한다. 보통은 모든 대상이 이미 그 키를 가지고 있으므로, 이 relay 는 wire 에서 아무 일도 하지 않는다. 다만 원래 송신 뒤에 연결된 peer 와 cache 항목이 밀려난 peer 는 이 relay 를 받는다.

> 해설: 자기 PREPARE 와 COMMIT 이 quorum 에 들어가는 길은 이 자기 전달 하나뿐이다. 자기 전달은 known cache 를 거치지 않으므로, 바이트가 같은 ROUND-CHANGE 재전송(`A-06` §6.3)도 노드 자신에게는 매번 다시 전달된다.

### 6.2 Gossip 대상

[WBFT-NET-031] validator 집합 `S` 로 `(code, data)` 를 gossip 할 때, 노드는 반드시 `dedup_key(data)` 를 known cache 에 추가하고, peer 주소가 `S` 에 있고 노드 자신의 주소가 아닌 eth peer 를 대상으로 골라야 한다. 노드는 그 밖의 어떤 peer 에게도 그 메시지를 보내서는 안 된다.
Source: consensus/wbft/backend/backend.go:176-187, eth/handler_istanbul.go:42-54 (`FindPeers`)
Observable: network

[WBFT-NET-032] 대상 peer 주소 `a` 마다, `a` 의 peer 별 recent cache 에 `dedup_key(data)` 가 있으면 노드는 반드시 `a` 를 건너뛰어야 한다. 그렇지 않으면 노드는 반드시 그 키를 `a` 의 peer 별 recent cache 에 추가하고 WBFT-NET-010 에 따라 `a` 에게 그 메시지를 보내야 한다. 노드는 키를 송신 전에 기록하며, 송신이 성공하는지와 관계없이 기록한다.
Source: consensus/wbft/backend/backend.go:188-206
Observable: network

[WBFT-NET-033] 노드는 같은 peer 에게 보낸 두 메시지가 보낸 순서대로 도착한다고 가정해서는 안 된다. reference 구현은 송신마다 별도의 goroutine 을 쓴다.
Source: consensus/wbft/backend/backend.go:206
Observable: network

WBFT-NET-031 과 WBFT-NET-032 에서 다음 결과가 나온다.

- 노드는 같은 바이트를 받은 peer 에게, 그 키가 그 peer 의 recent cache 에 남아 있는 동안 그 메시지를 되돌려 보내지 않는다. 다시 인코딩한 메시지의 relay(WBFT-NET-041)는 키가 다를 수 있다.
- 노드는 어떤 메시지의 키가 peer 주소의 recent cache 에 있는 동안, 그 메시지를 그 주소에 최대 한 번 보낸다. 그러므로 바이트가 같은 재전송(A-06 WBFT-TIMER-024)은 다시 보내지지 않는다.
- validator 가 아닌 노드는 적합한 노드에게서 합의 메시지를 결코 받지 못한다. devp2p 키가 자기 validator 주소에 대응하지 않는 validator 도 합의 메시지를 받지 못한다(§9.2).
- `K` 개의 validator 가 모두 서로 연결되어 있으면, 메시지 하나에 대해 만든 노드는 최대 `K − 1` 번 보내고, 받은 노드는 각각 최대 `K − 2` 번 relay 한다. 실제 횟수는 건너뛴 peer 수만큼 줄어든다. 모든 링크를 관측하는 observer 는 같은 메시지 키를 여러 번 보지만, 각 링크의 한 방향마다 최대 한 번만 본다.

---

## 7. Relay

노드는 core 가 받은 메시지를 성공적으로 처리하면, 그 메시지를 아직 보지 못한 validator 에게 relay 한다. relay 덕분에 메시지를 만든 노드와 직접 연결되지 않은 validator 도 그 메시지를 받을 수 있다.

[WBFT-NET-040] core 가 받은 메시지 `(code, data)` 를 오류 없이 처리하면, 노드는 반드시 받은 바이트를 바꾸지 않고 *현재* validator 집합으로 `(code, data)` 를 gossip 해야 한다. 현재 validator 집합은 relay 하는 순간의 집합이며, 메시지 height 의 집합과 다를 수 있다. relay 하는 노드는 validator 가 아니어도 된다.
Source: consensus/wbft/core/handler.go:128-135
Observable: network

[WBFT-NET-041] 나중에 처리하려고 먼저 저장해 둔 메시지는 반드시 그 나중 처리가 성공할 때, 그리고 성공할 때에만 relay 되어야 한다. 이런 메시지에는 backlog 에 들어간 미래 메시지(`A-05`)와, block 이 미래에서 왔기 때문에 미뤄 둔 PRE-PREPARE(A-06 §7)가 있다. relay 하는 바이트는 원래 받은 바이트가 아니라, decode 한 메시지를 다시 RLP 로 인코딩한 것이다(`A-03`).
Source: consensus/wbft/core/handler.go:136-150, consensus/wbft/core/backlog.go:250-307, consensus/wbft/core/preprepare.go:156-162
Observable: network

`A-03` 의 decoder 가 받아들이는 대부분의 메시지에서 다시 인코딩한 결과는 받은 payload 와 같다. go-ethereum RLP 가 정규형이 아닌 인코딩을 거부하기 때문이다. 예외는 `A-03` §7(WBFT-ENC-090)에 나열된 ROUND-CHANGE 경우들이다. 이 경우에는 decode 한 뒤 다시 인코딩한 결과가 원래와 같지 않으므로, relay 하는 바이트와 그 message key 가 받은 것과 달라진다.

[WBFT-NET-042] `check_message` 가 *extra seal* 로 분류하고 core 가 저장하거나 무시한 메시지는 오류 없이 처리된 것으로 치며, 반드시 relay 되어야 한다. extra seal 은 `A-05` §12 가 정하며, 두 가지가 있다. 하나는 노드가 `AcceptRequest` 에 있는 동안 받은, 이전 sequence 와 prior round 의 메시지이다. 다른 하나는 현재 view 의 PREPARE 나 COMMIT 가운데, 노드가 그 종류의 quorum 에 도달한 뒤에 받은 메시지이다. 이 규칙은 노드에 seal 을 검사할 대상 제안이 없어서 아무것도 저장하지 않는 경우도 포함한다. 그 경우 메시지는 코드와 관계없이 relay 된다.
Source: consensus/wbft/core/handler.go:211-224 (the `errExtraSealMessage` branch returns the result of `addToExtraSeal`), consensus/wbft/core/extraseal.go:29-48, consensus/wbft/core/extraseal.go:86
Observable: network

[WBFT-NET-043] 노드는 다음 경우에 메시지를 relay 해서는 안 된다.

1. 메시지가 중복으로 버려졌거나(WBFT-NET-024) engine 이 멈춘 동안 버려졌다(WBFT-NET-027).
2. 코드가 `0x11` 이다.
3. payload 가 그 코드의 메시지 형식으로 decode 되지 않는다(`A-03`).
4. 메시지 signature, 또는 그 justification 안의 어떤 signature 가 적용되는 validator 집합으로 verify 되지 않는다(`A-05`).
5. core 가 view 를 유효하지 않거나, 지났거나, 너무 먼 미래로 분류한다(`A-05` 의 `check_message` 결과 `INVALID`, `OLD`, `TOO_FAR`).
6. 메시지가 미래 메시지(`FUTURE`)이다. 이 메시지는 backlog 에 저장되고 WBFT-NET-041 에 따라서만 relay 된다. 이 메시지가 backlog 에서 버려지면 결코 relay 되지 않는다. 메시지는 다음 경우에 backlog 에서 버려진다.
   - 메시지가 노드 자신에게서 왔다.
   - 그 보낸 사람의 `(code, sequence, round)` 칸이 이미 차 있다.
   - 그 보낸 사람이 이미 `MAX_BACKLOG_SIZE_PER_VALIDATOR` 개의 메시지를 backlog 에 두고 있다. 이 값은 reference 에서 88 이다(`A-01` §4.6).
   - backlog 를 처리할 때 보낸 사람이 더 이상 현재 validator 집합에 없다.
   - backlog 를 처리할 때 메시지가 `FUTURE` 가 아니라 `OLD`, `INVALID`, `TOO_FAR` 로 분류된다.
7. extra seal 의 digest 가 대상 제안(`AcceptRequest` 에서는 prior 제안, 그 밖에서는 현재 제안)과 맞지 않거나 seal 이 verify 되지 않는다. 또는 노드에 대상 제안이 있는데 PREPARE 나 COMMIT 이 아닌 메시지가 extra seal 로 분류되었다(`errInvalidExtraSealMessage`).
8. `A-05` 의 메시지 handler 가 오류를 돌려준다. handler 는 다음 경우에 오류를 돌려준다.
   - PRE-PREPARE 가 proposer 에게서 오지 않았다.
   - PRE-PREPARE 의 sequence 가 block 번호와 다르다.
   - PRE-PREPARE 의 justification 이 유효하지 않다.
   - block 검증이 실패했다. "future block" 이어서 처리를 미룬 경우도 여기에 들어간다.
   - PREPARE 나 COMMIT 의 digest 가 맞지 않거나 seal 이 유효하지 않다.
   - ROUND-CHANGE 의 prepared block 번호가 현재 sequence 와 다르거나, prepared block hash 가 prepared digest 와 다르다.
   - proposer 가 ROUND-CHANGE quorum 을 평가했는데 제안할 block 을 찾지 못했다.

Source: consensus/wbft/core/handler.go:128-150, consensus/wbft/core/handler.go:188-227, consensus/wbft/core/backlog.go:125-244, consensus/wbft/core/backlog.go:254-298, consensus/wbft/core/preprepare.go:115-169, consensus/wbft/core/prepare.go:87-115, consensus/wbft/core/commit.go:90-118, consensus/wbft/core/roundchange.go:103-186, consensus/wbft/core/extraseal.go:49-85
Observable: network

proposer 의 quorum 검사를 실행시켰는데 그 검사가 justification 검사에서 실패한 ROUND-CHANGE 는 그래도 relay 된다. handler 가 실패를 로그에 남기고 오류 없이 돌아오기 때문이다(`consensus/wbft/core/roundchange.go:197-204`).

---

## 8. 결과 등급

consensus link 로 받은 모든 `istanbul` 메시지는 네 등급 가운데 정확히 하나로 끝난다. 이 등급은 밖에서 관측할 수 있다. DISCONNECT 는 connection 을 닫는다. ACCEPT 는 relay 를 만들고, 받는 노드의 새 메시지를 만들 수도 있다(`A-05`). DROP-SILENT 와 IGNORE 는 둘 다 만들지 않는다. DROP-SILENT 와 IGNORE 의 차이는 메시지가 core 에 도달했는지이며, 이 차이는 노드의 로그에서만 보인다.

| 등급 | 효과 | 조건 (전체) | 규칙 |
|---|---|---|---|
| DISCONNECT | connection 이 닫힌다. 합의 계층은 벌점을 기록하지 않는다. devp2p 의 일반적인 재연결 제한은 적용된다. LAN 이 아닌 같은 IP 에서 이전 시도 뒤 30 초 안에 들어온 inbound 시도는 거절되고, 노드는 이전 dial 뒤 35 초 안에는 같은 노드를 다시 dial 하지 않는다 | (1) devp2p frame 오류, 또는 협상된 어느 범위에도 없는 코드; (2) 10 MiB(RLPx 계층에서는 16 MiB)보다 긴 payload; (3) engine 이 멈췄고 노드가 동기화 중이 아닐 때 받은 코드 `0x11..0x15`; (4) payload 가 RLP 바이트 문자열이 아닌 코드 `0x11`; (5) payload 가 빈 코드 `0x12..0x15`; (6) `istanbul` stream 의 모든 읽기 오류 | WBFT-NET-012, -013, -020, -021, -027 |
| DROP-SILENT | core 에 전달되지 않는다. relay 되지 않는다 | (1) 코드 `0x00..0x10`(`0x07` 포함); (2) engine 이 멈췄고 노드가 동기화 중일 때 받은 코드 `0x11..0x15`; (3) message key 가 known cache 에 있는 코드 `0x11..0x15` | WBFT-NET-020, -024, -027, -028 |
| IGNORE | core 에 전달되고, core 가 버리거나 저장한다. 지금은 relay 되지 않는다 | WBFT-NET-043 의 2–8 번 경우. 미래 메시지와 미래 block PRE-PREPARE 는 저장되며, 나중에 ACCEPT 로 바뀔 수 있다(WBFT-NET-041) | WBFT-NET-043 |
| ACCEPT | core 가 처리하고 relay 한다(§7) | core 의 처리가 오류를 돌려주지 않았다. 저장된 extra seal(WBFT-NET-042)과, 나중에 성공한 backlog 처리 또는 미뤄 둔 처리를 포함한다 | WBFT-NET-040, -041, -042 |

[WBFT-NET-044] 노드는 IGNORE 등급의 메시지 때문에 peer 와의 연결을 끊거나 peer 에게 다른 벌점을 주어서는 안 된다. signature 가 유효하지 않은 메시지, seal 이 유효하지 않은 메시지, 제안이 유효하지 않은 메시지도 여기에 포함된다.
Source: eth/handler_istanbul.go:132-153 (only errors returned by `HandleMsg` end the loop; core errors are never returned to it), consensus/wbft/backend/handler.go:96-100
Observable: network

reference 구현에서 무효 메시지 하나는 받는 노드에게 decode 한 번과 signature recover 한 번의 비용을 치르게 한다. 그 메시지가 proposer 에게서 온 PRE-PREPARE 이면 block 검증 비용도 든다. 이 비용을 막는 장치는 같은 바이트를 걸러 내는 known cache 와 backlog 한도(`A-05`)뿐이다.

---

## 9. Topology 와 신원

### 9.1 필요한 연결성

합의 메시지는 validator 사이의 consensus link 로만 전달되고(§6.2), 메시지는 그 메시지를 성공적으로 처리한 validator 만 relay 한다(§7). 뒤처진 validator 는 메시지를 `FUTURE` 로 처리하거나 `TOO_FAR` 로 버리므로, 그 시점에는 그 메시지를 전달하지 않는다.

[WBFT-NET-050] liveness 를 위해, 현재 validator 들과 그 사이의 consensus link 가 이루는 그래프는 반드시 연결되어 있어야 한다. 운영자는 모든 validator 쌍을 직접 연결하는 것(full mesh)이 좋고(SHOULD), validator 들을 서로의 trusted peer 이자 static peer 로 설정하는 것이 좋다(SHOULD). trusted peer 로 설정하면 그 링크는 devp2p 와 eth 의 peer 한도(`maxpeers`)에서 면제된다. static peer 로 설정하면 노드는 그 peer 를 계속 dial 한다. full mesh 이면 relay 가 필요 없다.
Source: consensus/wbft/backend/backend.go:176-210, eth/handler_istanbul.go:42-54, eth/handler.go:405-409 (trusted peers bypass the eth peer limit), p2p/server.go:817-822 (and the devp2p limit; static peers are not exempt)
Observable: network

### 9.2 키 신원

노드는 원격 devp2p public key 의 주소로 gossip 대상을 고른다. 그러므로 validator 는 자기 devp2p 키가 자기 validator 주소의 키일 때에만 합의 메시지를 받는다.

[WBFT-NET-051] validator 는 반드시 같은 secp256k1 키를 devp2p node key 와 합의 서명 키로 써야 한다. 합의 서명 키는 주소가 validator 주소인 키이며, validator 의 BLS 키도 이 키에서 끌어낸다(`A-02`).
Source: eth/backend.go:164 (`CreateConsensusEngine(..., stack.Config().NodeKey(), ...)`), consensus/wbft/backend/backend.go:61-86, eth/handler_istanbul.go:46-50
Observable: network

devp2p 키가 validator 키와 다른 validator 는 gossip 도 relay 도 받지 못한다. 그 validator 는 메시지를 보낼 수 있고 그 메시지는 signature 로 받아들여지지만, PRE-PREPARE 와 투표를 결코 받지 못한다. 그러므로 그 validator 는 prepare 도 commit 도 할 수 없고, crash fault 로 계산된다.

### 9.3 Discovery (참고)

peer discovery 는 go-ethereum 과 같다. discv4/discv5, DNS 목록, `static-nodes`/`trusted-nodes`, `admin_addPeer` 를 쓴다. 노드가 ECDH 에 쓰는 discovery key 에는 `A-10` WBFT-SEC-032 의 점 검사가 적용된다. validator 전용 discovery 는 없다. ENR 에는 `eth` fork ID 항목이 들어 있고, snap 동기화 지원이 켜져 있으면(기본값이다, `eth/backend.go:514-516`, `eth/protocols/snap/handler.go:112`) `snap` 항목도 들어 있다. `istanbul` 항목은 없다. 프로토콜에는 노드가 validator 를 peer 로 우선하게 만드는 장치가 없다. 그래서 validator 수가 `maxpeers` 에 가깝거나 validator 가 아닌 peer 가 많으면, validator 들이 서로의 trusted peer 로 설정되어 있지 않은 한 서로 연결하지 못할 수 있다.

---

## 10. 계산 예제: PREPARE 하나가 wire 로 퍼지는 과정

validator `A` 가 PREPARE 를 만들고, 그 RLP 인코딩 `P` 의 길이는 213 바이트라고 하자. validator 는 `A, B, C, D` 네 개이고, 모두 서로 연결되어 있으며(full mesh), eth/68 과 istanbul/100 이 협상되어 있고, snappy 가 켜져 있다.

1. `A` 는 자기가 validator 집합에 있는지 확인한다. 길이 `213 = 0xd5` 가 55 보다 크므로 `A` 는 `k = keccak256(0xb8 ‖ 0xd5 ‖ P)` 를 계산한다. `A` 는 `k` 를 known cache 에 추가하고, `B`, `C`, `D` 의 recent cache 에 `k` 를 추가한 뒤, 세 노드 각각에게 wire code 가 `0x34` 이고 payload 가 `P` 인 devp2p 메시지를 보낸다. 이때 frame 데이터는 `0x34 ‖ snappy(P)` 이다. `A` 는 `P` 를 자기 core 에도 전달한다.
2. `B` 는 `A` 에게서 `(0x13, P)` 를 받는다. `B` 는 `recent[A]` 에 `k` 를 추가한다. `k` 가 known cache 에 없으므로 `B` 는 `k` 를 known cache 에 추가하고 `P` 를 core 에 전달한다. 그 PREPARE 는 유효하고 `B` 의 현재 view 에 속하므로, core 는 오류 없이 처리한다.
3. `B` 는 relay 한다. 대상은 자신을 뺀 `{A, C, D}` 이다. `recent[A]` 에 `k` 가 있으므로 `B` 는 `A` 를 건너뛴다. `recent[C]` 와 `recent[D]` 에는 `k` 가 없으므로 `B` 는 `C` 와 `D` 에게 `P` 를 보낸다.
4. `C` 도 `A` 에게서 `P` 를 받았으므로 같은 방식으로 처리하고 relay 한다. `recent[A]` 에 `k` 가 있으므로 `C` 는 `A` 를 건너뛴다. `C` 는 `B` 와 `D` 에게서 이미 `P` 를 받지 않았다면 두 노드에게 보낸다. known cache 에 이미 `k` 가 있는 노드에 도착한 사본(예: `A` 의 원본 뒤에 `C` 에 도착한 `B` 의 relay)은 보낸 peer 의 recent cache 에 `k` 를 추가하기만 하고 조용히 버려진다. `B`, `C`, `D` 사이의 relay 가운데 실제로 어느 것이 일어나는지는 timing 에 달려 있다.

여섯 링크를 모두 관측하는 observer 는 각 링크의 각 방향에서 `P` 를 최대 한 번 본다.

> 해설: 아래 그림은 §5 와 §7 의 받는 경로를 한 번에 보여 준다. 핵심은 노드가 core 의 검사보다 먼저 key 를 known cache 에 기록한다는 점이다.
>
> ```mermaid
> flowchart TD
>     R[노드가 peer a 에게서 code c 와 data 를 받는다] --> S{engine 이 돌고 있는가}
>     S -- 아니오, 노드가 동기화 중이다 --> D1[노드는 메시지를 버린다. 등급은 DROP-SILENT 이다]
>     S -- 아니오, 노드가 동기화 중이 아니다 --> X[노드는 연결을 끊는다. 등급은 DISCONNECT 이다]
>     S -- 예 --> A[노드는 메시지를 검증하기 전에, 메시지가 유효한지와 관계없이 key 를 recent cache a 에 기록한다]
>     A --> K{key 가 known cache 에 있는가}
>     K -- 예 --> D2[노드는 메시지를 버린다. 등급은 DROP-SILENT 이다]
>     K -- 아니오 --> KN[노드는 core 가 검사하기 전에 key 를 known cache 에 기록한다] --> C[노드는 메시지를 core 에 비동기로 전달한다]
>     C --> OK{core 가 오류 없이 처리했는가}
>     OK -- 예 --> G[노드는 메시지를 현재 validator 집합에 gossip 한다. 등급은 ACCEPT 이다]
>     OK -- 아니오 --> I[노드는 메시지를 relay 하지 않는다. 등급은 IGNORE 이다]
> ```

---

## 11. 구현 노트 (참고)

- `Backend.HandleMsg` 는 모든 peer 에게서 온 모든 `istanbul` 메시지에 대해 배타적 lock(`coreMu`)을 잡은 채 실행된다(`consensus/wbft/backend/handler.go:71-72`). 그러므로 core 로 넘기기 전까지의 수신 처리는 peer 사이에서 직렬화된다.
- cache 는 go-ethereum 의 `common/lru` cache 이다. peer 별 cache 는 LRU 들을 담은 LRU 이다(`consensus/wbft/backend/backend.go:62-63`, `consensus/wbft/backend/engine.go:39-42`). 두 cache 는 모두 메모리에만 있고 재시작하면 사라진다.
- `consensusRw` 는 `istanbul` handler goroutine 이 쓰고, gossip goroutine 들이 동기화 없이 읽는다(`eth/protocols/eth/peer.go:100`, `:519-530`).
- 합의로 decide 된 block 은 `Enqueue("istanbul", block)` 로 eth fetcher 에 전달된다(`consensus/wbft/backend/backend.go:245-247`). 다만 그 block 이 이 노드의 `Seal` 이 기다리는 block 이면, 노드는 `Enqueue` 를 부르지 않고 그 block 을 miner 에게 돌려준다(`consensus/wbft/backend/backend.go:239-243`). 여기서 `"istanbul"` 은 가짜 peer ID 이다. decide 뒤의 block 전파는 `istanbul/100` 이 아니라 일반 `eth` 알림(`B-09`)을 쓴다.

---

## 12. 버전 노트

`git diff v1.1.0 740526d03` 은 `eth/handler_istanbul.go`, `eth/quorum_protocol.go`, `eth/handler.go`, `eth/protocols/eth/peer.go`, `p2p/peer.go`, `p2p/message.go`, `consensus/wbft/backend/`, `consensus/wbft/core/` 에 대해 비어 있다. 이 장의 모든 내용은 `v1.1.0` 에도 적용된다.
