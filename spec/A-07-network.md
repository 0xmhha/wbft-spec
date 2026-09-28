# A-07 Network

- Status: draft
- Area code: `NET`
- Reference: go-stablenet `740526d03`. The files cited here are identical in `v1.1.0` except where §12 says otherwise.

WBFT nodes exchange consensus messages over a devp2p sub-protocol named `istanbul`, version `100`, which runs next to the `eth` protocol on the same RLPx connection. This chapter specifies that sub-protocol: how it is identified and negotiated, how it depends on the `eth` handshake, the exact framing of a message, the size limit, what a receiver does with each message code, the two deduplication caches and their key, which peers a node sends to and relays to, the complete list of conditions under which a message is dropped, ignored, accepted or causes a disconnect, and what the network topology must look like for the protocol to be live.

The message bodies (PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE) and their signing payloads are defined in `A-03`. How a message is processed once it reaches the consensus core is defined in `A-05`; this chapter only uses the outcome of that processing (success or failure) to decide about relaying.

---

## 1. Terms

| Term | Meaning |
|---|---|
| *connection* | An established devp2p (RLPx) session with a remote node, after the devp2p `Hello` exchange. |
| *peer address* | `address_of(pubkey)`: the 20-byte address derived (as for Ethereum accounts, `A-02`) from the remote node's secp256k1 devp2p public key. |
| *eth peer* | A connection on which the `eth` protocol handshake (`Status`) succeeded and the peer was registered in the node's peer set. |
| *consensus link* | An eth peer on which `istanbul/100` was also negotiated and attached (§3). |
| *code* | The message code relative to the start of the `istanbul` code space, `0x00..0x15`. |
| *wire code* | The code actually written into the RLPx frame, `offset + code` (§2.2). |
| *payload* | The message body bytes carried in the frame after the code, before snappy compression. |
| *message key* | `dedup_key(data) = keccak256(rlp_encode(data))`, where `data` is treated as an RLP byte string (§5.2; defined in `A-03` WBFT-MSG-060). |
| *validator set* | `validators_at(h)` for the height `h` the node is currently agreeing on (`A-04`). |
| *core* | The consensus state machine of `A-05`; "the engine is running" means the core has been started and not stopped. |

---

## 2. Sub-protocol identification

### 2.1 Capability

[WBFT-NET-001] A WBFT node MUST advertise the capability `("istanbul", 100)` in its devp2p `Hello` message, with a message-code space of length 22 (codes `0x00..0x15`).
Source: eth/quorum_protocol.go:38-42, eth/quorum_protocol.go:52-54, eth/handler_istanbul.go:61-67
Observable: network

[WBFT-NET-002] A WBFT node MUST also advertise `("eth", 68)` (length 17). It MAY advertise `("snap", 1)` (length 8).
Source: eth/protocols/eth/protocol.go:42-46, eth/protocols/snap/protocol.go:39-43, eth/backend.go:512-524
Observable: network

Implementation note (informative). The reference implementation advertises `istanbul/100` unconditionally, also for chain configurations without Anzeon (`eth/backend.go:518-522`: the guard compares the constant name `"istanbul"` with `"eth"` and is always true). The `istanbul` protocol entry reuses the `eth` protocol's `NodeInfo`, `PeerInfo`, ENR entry (`eth` fork ID) and dial candidates (`eth/handler_istanbul.go:104-111`); it adds no ENR entry of its own.

[WBFT-NET-052] The size prefix of an RLPx (EIP-8) auth or ack message a node sends MUST NOT exceed 2048. The reference implementation fails the handshake when it receives a larger one; go-ethereum v1.13.15 has no such limit.
Source: p2p/rlpx/rlpx.go:600-612
Observable: network

The RLPx handshake is otherwise the upstream one, with one more difference: a node rejects an off-curve public key or ECIES point received in the handshake before it multiplies the point by its node key or ephemeral key. That rule is `A-10` WBFT-SEC-032; go-ethereum v1.13.15 does not check the point (`A-10` §4).

### 2.2 Wire code

devp2p assigns each negotiated capability a contiguous code range after the 16 base-protocol codes. Capabilities are ordered by name, then version, and only the matched ones are counted.

[WBFT-NET-003] The wire code of an `istanbul` message MUST be `offset + code`, where `offset = 16 + Σ length(c)` over the negotiated capabilities `c` whose name sorts before `"istanbul"`. With `eth/68` negotiated (required for the link to work, §3), `offset = 16 + 17 = 0x21`.
Source: p2p/peer.go:45, p2p/peer.go:401-423 (`matchProtocols`), p2p/peer.go:471-480 (`WriteMsg` adds the offset), p2p/peer.go:452-459 (`getProto`)
Observable: network

| Message | code | wire code (offset `0x21`) |
|---|---|---|
| legacy `ISTANBUL_MSG` | `0x11` | `0x32` |
| PRE-PREPARE | `0x12` | `0x33` |
| PREPARE | `0x13` | `0x34` |
| COMMIT | `0x14` | `0x35` |
| ROUND-CHANGE | `0x15` | `0x36` |
| (NewBlock peek, §5.6) | `0x07` | `0x28` |

`snap` sorts after `istanbul`, so its presence does not change the `istanbul` offset.

Implementation note (informative). p2p metrics are registered per protocol and relative code, for example `p2p/ingress/istanbul/100/0x12` and `p2p/egress/istanbul/100/0x12` (`p2p/peer.go:373-377` for ingress, `p2p/transport.go:101-105` for egress; recorded only when metrics are enabled), which gives per-node counts of each consensus message type without peer breakdown.

---

## 3. Dependency on the eth protocol

`istanbul/100` has no handshake. The only compatibility check between two nodes is the `eth` `Status` exchange on the same connection (network ID, total difficulty, head, genesis hash, fork ID with fork filter, `B-09` §12.1). A consensus link is attached only after that exchange succeeded and the eth peer was registered.

[WBFT-NET-004] A node MUST NOT send or expect any handshake message on `istanbul/100`. The first `istanbul` message on a connection MAY be a consensus message.
Source: eth/handler_istanbul.go:69-103
Observable: network

[WBFT-NET-005] A node MUST NOT process `istanbul` messages on a connection before the `eth` handshake on that connection has succeeded and the remote has been registered as an eth peer. The reference implementation does not read the `istanbul` stream until then.
Source: eth/handler_istanbul.go:81-97, eth/handler.go:363-490 (registration; `EthPeerRegistered` signalled at 490)
Observable: network

[WBFT-NET-006] A node MUST NOT send consensus messages to a connection before that connection's `istanbul` stream has been attached to the registered eth peer. In the reference implementation a message addressed to an eth peer that has no attached `istanbul` stream (not yet attached, or `istanbul/100` not negotiated) is discarded without error, and the peer is nevertheless recorded as having received the message key (WBFT-NET-032), so the message is not sent to that peer again while the key stays in that address's per-peer recent cache (§5.2).
Source: eth/protocols/eth/peer.go:519-525 (`SendWBFTConsensus` returns `nil` when `consensusRw == nil`), eth/protocols/eth/peer.go:527-530, consensus/wbft/backend/backend.go:188-206
Observable: network

[WBFT-NET-007] If the `eth` handshake fails, or registering the remote in the eth peer set fails, the node MUST close the `istanbul` protocol on that connection (and hence the connection). On the other eth failure paths the reference implementation does not close it (see below).
Source: eth/handler_istanbul.go:91-101, eth/handler.go:386-392, eth/handler.go:412-418
Observable: network

The reference implementation signals the `istanbul` protocol handler only on those two eth failure paths (handshake failure and peer-set registration failure). On the other paths (`DiscQuitting` because the handler is shutting down, snap-extension wait failure, `DiscTooManyPeers`, "peer dropped during handling", downloader or snap-syncer registration failure, required-block request failure; `eth/handler.go:363-453`) the `istanbul` protocol handler keeps waiting.

Until the `istanbul` stream is attached, an `istanbul` frame that arrives on the connection blocks the connection's read loop (devp2p delivers sub-protocol messages through an unbuffered channel, `p2p/peer.go:378-383`), so later `eth` frames on the same connection wait as well. This lasts until eth registration completes and does not deadlock, because the `eth` `Status` precedes any consensus message from a conforming remote.

---

## 4. Framing

### 4.1 Frame content

A consensus message is carried as one devp2p message. The payload is the message's RLP encoding from `A-03` as is. It is not wrapped again as an RLP byte string; this is the difference from the legacy code `0x11` (§4.2).

[WBFT-NET-010] To send a consensus message `m` with code `c ∈ {0x12, 0x13, 0x14, 0x15}`, a node MUST write one devp2p message with wire code `offset + c` and payload `rlp_encode(m)` (`A-03`), unchanged. The devp2p message size MUST be `len(rlp_encode(m))`. The RLPx frame data is therefore `rlp_encode(offset + c) ‖ snappy(rlp_encode(m))` when both sides use devp2p version 5 or later, and `rlp_encode(offset + c) ‖ rlp_encode(m)` otherwise.
Source: p2p/message.go:109-113 (`SendWithNoEncoding`), eth/protocols/eth/peer.go:519-525, consensus/wbft/backend/backend.go:202-206, p2p/rlpx/rlpx.go:207-242
Observable: network

[WBFT-NET-011] A node MUST NOT send code `0x11`. It MUST NOT send any `istanbul` code other than `0x12..0x15`.
Source: consensus/wbft/backend/backend.go:202-205 (`0x11` is used only for codes outside `0x12..0x15`, which the core never produces), consensus/wbft/messages/message.go:28-43
Observable: network

### 4.2 Legacy code 0x11

[WBFT-NET-012] On receiving code `0x11`, a node MUST decode the payload as one RLP byte string and use its content as the message data `data`. If the payload does not start with a valid RLP byte string, the node MUST disconnect the peer (§8). Bytes after the first RLP item are ignored.
Source: consensus/wbft/backend/handler.go:54-60, consensus/wbft/backend/handler.go:78-81, p2p/message.go:56-61
Observable: network

A successfully decoded `0x11` message is then treated as a consensus message with code `0x11`, which the core rejects (§8, class IGNORE). Its only lasting effect is on the deduplication caches (§5.2).

### 4.3 Size limit

[WBFT-NET-013] A node MUST disconnect a peer that sends an `istanbul` message whose payload (after snappy decompression) is longer than `MAX_ISTANBUL_MSG_SIZE` = `10 485 760` bytes (10 MiB; reference `protocolMaxMsgSize`). Independently, devp2p rejects any frame whose decompressed payload exceeds `16 777 215` bytes (`2^24 − 1`) and disconnects.
Source: eth/handler.go:59 (`protocolMaxMsgSize`), eth/handler_istanbul.go:138-140, p2p/rlpx/rlpx.go:146-155
Observable: network

---

## 5. Receive path

### 5.1 Codes

[WBFT-NET-020] On each `istanbul` message received on a consensus link, a node MUST act by relative code:

| Code | Engine running | Engine stopped, node synchronising | Engine stopped, not synchronising |
|---|---|---|---|
| `0x12..0x15` | §5.3 | discard (DROP-SILENT) | disconnect (§5.4) |
| `0x11` | §4.2, then §5.3 | discard (DROP-SILENT) | disconnect (§5.4) |
| `0x07` | §5.6 (no effect) | discard | discard |
| other `0x00..0x10` | discard | discard | discard |

Source: consensus/wbft/backend/handler.go:69-131, eth/handler_istanbul.go:115-153
Observable: network

Codes `0x16` and above are outside the `istanbul` range. devp2p routes them to the next capability's range or, if none matches, disconnects with "msg code out of range" (`p2p/peer.go:368-372`).

[WBFT-NET-021] For codes `0x12..0x15` the node MUST use the complete payload as the message data `data`. If the payload is empty, the reference implementation disconnects the peer (reading zero bytes from an empty payload reports end-of-file, which is treated as a decode failure).
Source: consensus/wbft/backend/handler.go:60-64, consensus/wbft/backend/handler.go:78-81
Observable: network

### 5.2 Deduplication caches

A node keeps two in-memory caches of message keys.

- The **per-peer recent cache** records, for each peer address, the keys the node has received from that peer or sent to it. It holds at most `INMEMORY_PEERS` = 40 peer addresses (least recently used evicted) and at most `INMEMORY_MESSAGES` = 1 024 keys per address (least recently used evicted) (`A-01`). It is keyed by peer address, not by connection, so it survives a reconnect.
- The **known cache** records keys the node has already delivered to its core or sent itself. It holds at most `INMEMORY_MESSAGES` = 1 024 keys (least recently used evicted).

[WBFT-NET-022] The message key MUST be `dedup_key(data) = keccak256(rlp_encode(data))` (`A-03` WBFT-MSG-060) with `data` encoded as an RLP byte string: `keccak256(data)` if `len(data) = 1` and `data[0] < 0x80`, `keccak256(0x80 + len ‖ data)` otherwise for `len(data) <= 55`, `keccak256(0xb7 + len(be(len)) ‖ be(len) ‖ data)` otherwise, where `be` is the minimal big-endian encoding. The message code is not part of the key.
Source: consensus/wbft/utils.go:31-36 (`RLPHash`), consensus/wbft/backend/handler.go:66, consensus/wbft/backend/backend.go:177
Observable: network

[WBFT-NET-023] On receiving `data` with code `0x11..0x15` from peer address `a` while the engine is running, the node MUST add `dedup_key(data)` to the per-peer recent cache of `a`. It MUST do so before the known-cache check, whether or not the message is later found valid.
Source: consensus/wbft/backend/handler.go:82-88
Observable: network

[WBFT-NET-024] If `dedup_key(data)` is in the known cache, the node MUST discard the message (DROP-SILENT). Otherwise it MUST add the key to the known cache and deliver `(code, data)` to the core. The key is added before the core has checked the message.
Source: consensus/wbft/backend/handler.go:90-99
Observable: network

### 5.3 Delivery to the core

[WBFT-NET-025] A node MUST NOT assume that messages are processed in the order in which they were received, whether from one peer or from several. The reference implementation hands each message to the core asynchronously.
Source: consensus/wbft/backend/handler.go:96-99 (`go ...Post`), consensus/wbft/backend/backend.go:171
Observable: network

The core decodes and verifies the message and processes it as defined in `A-05`; the outcome decides relaying (§7) and the class in §8.

[WBFT-NET-026] The peer address MUST be used only for deduplication (§5.2) and sender selection (§6). It MUST NOT be used to authenticate a consensus message. A message is attributed to the validator recovered from its signature (`A-05`), so a message signed by validator `V` is accepted from any peer, including a non-validator.
Source: eth/handler_istanbul.go:163-167, consensus/wbft/core/handler.go:268-317
Observable: network

### 5.4 Engine stopped

[WBFT-NET-027] When a node whose core is not running receives code `0x11..0x15`, it MUST discard the message and keep the connection if its block downloader is synchronising at that moment, and MUST disconnect the peer otherwise.
Source: consensus/wbft/backend/handler.go:73-76, eth/handler_istanbul.go:118-127
Observable: network

The core is not running on a node that does not produce blocks (block production disabled, or a non-validator that never started it), and on a producing node between the start of a synchronisation and the restart of production after it, which happens only until the first successful synchronisation of the process (`miner/miner.go:139-161`, `B-09` SNET-SYNC-030). Because conforming senders address only validator peers (§6.2), a non-validator normally receives no consensus messages. A validator that runs without block production, however, disconnects every validator that sends to it, and those validators reconnect and send again.

### 5.5 Disconnect reason (informative)

When the `istanbul` handler returns an error, devp2p closes the connection and sends `Disconnect` with reason `0x10` (subprotocol error) (`p2p/peer.go:295-297`, `p2p/peer_error.go:102-119`, `p2p/transport.go:109-128`). Errors raised in the devp2p read loop itself (frame errors, code out of range) close the connection with reason `0x01` (network error), for which no `Disconnect` message is sent.

### 5.6 NewBlock code on the consensus stream

[WBFT-NET-028] A message with code `0x07` received on `istanbul/100` MUST have no effect on consensus state and MUST NOT cause a disconnect (unless it exceeds the size limit).
Source: consensus/wbft/backend/handler.go:102-130, eth/handler_istanbul.go:145-152
Observable: network

Implementation note (informative). If the node is the current proposer, the reference implementation decodes the payload as `{Block, TD}` and, if the block has difficulty 1 and is the proposal the node is currently driving, logs `WBFT: block already proposed`. The result is discarded either way. This is inherited from Quorum (commit `5ffacc48`, `consensus/istanbul/backend/handler.go:104-131`), where the legacy consensus protocols `istanbul/64` and `istanbul/99` pass the messages the consensus handler does not handle to the `eth` handler (`eth/handler.go:780-811`), so the same handler also served the `eth` stream. Quorum recognises the block by `MixDigest == IstanbulDigest`, the reference by `Difficulty == 1`. In go-stablenet, `eth` messages never reach it, so the path is unreachable from a conforming peer.

---

## 6. Send path

### 6.1 Broadcast

The core uses two send operations. *Broadcast* is used for messages the node creates itself (PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE, `A-05`). *Gossip* is used for relaying (§7) and by Broadcast.

[WBFT-NET-030] When a node broadcasts a message it created, it MUST first check that its own address is in the validator set passed by the core; if not, it MUST send nothing (not to peers, not to itself). Otherwise it MUST gossip the message (§6.2) and MUST deliver the message to its own core as if it had been received, without passing it through the known-cache check of WBFT-NET-024.
Source: consensus/wbft/backend/backend.go:152-173
Observable: network, log
Observed as: the log line `WBFT: invalid validator` (Error).

Self-delivery means that a node's own PREPARE and COMMIT count towards its quorums through the normal receive path, and that the node relays its own message again after processing it (normally a no-op on the wire, because every target already has the key; peers connected since the original send, or whose cache entry was evicted, receive it).

### 6.2 Gossip targets

[WBFT-NET-031] To gossip `(code, data)` with validator set `S`, a node MUST add `dedup_key(data)` to its known cache and select as targets the eth peers whose peer address is in `S` and is not the node's own address. It MUST NOT send the message to any other peer.
Source: consensus/wbft/backend/backend.go:176-187, eth/handler_istanbul.go:42-54 (`FindPeers`)
Observable: network

[WBFT-NET-032] For each target peer address `a`, the node MUST skip `a` if the per-peer recent cache of `a` contains `dedup_key(data)`. Otherwise it MUST add the key to the per-peer recent cache of `a` and send the message to `a` as in WBFT-NET-010. The key is recorded before, and independently of, the success of the send.
Source: consensus/wbft/backend/backend.go:188-206
Observable: network

[WBFT-NET-033] A node MUST NOT assume that two messages sent to the same peer arrive in the order they were sent. The reference implementation performs each send in its own goroutine.
Source: consensus/wbft/backend/backend.go:206
Observable: network

Consequences of WBFT-NET-031 and WBFT-NET-032:

- A node does not send a message back to a peer it received the same bytes from, as long as the key is still in that peer's recent cache (relays of re-encoded messages, WBFT-NET-041, can have a different key).
- A node sends a given message at most once to each peer address while the key is in that address's recent cache. A byte-identical retransmission (A-06 WBFT-TIMER-024) is therefore not sent again.
- A non-validator never receives consensus messages from a conforming node, and a validator whose devp2p key does not correspond to its validator address never receives them either (§9.2).
- With `K` fully connected validators, one message causes at most `K − 1` sends by its creator and at most `K − 2` relays by each receiver, minus the skips. An observer tapping all links sees the same message key several times, but at most once per direction of each link.

---

## 7. Relay

A node relays a received message to the validators that have not seen it, once its core has processed the message successfully. Relaying is what lets a validator without a direct link to the creator still receive the message.

[WBFT-NET-040] After the core has processed a received message `(code, data)` without error, the node MUST gossip `(code, data)` with its *current* validator set (the set at the moment of relaying, which may differ from the set of the message's height), using the received bytes unchanged. The relaying node does not have to be a validator itself.
Source: consensus/wbft/core/handler.go:128-135
Observable: network

[WBFT-NET-041] A message that was first stored for later processing (a future message in the backlog, `A-05`, or a PRE-PREPARE deferred because its block is from the future, A-06 §7) MUST be relayed when, and only if, that later processing succeeds. The relayed bytes are the RLP re-encoding of the decoded message (`A-03`), not the originally received bytes.
Source: consensus/wbft/core/handler.go:136-150, consensus/wbft/core/backlog.go:250-307, consensus/wbft/core/preprepare.go:156-162
Observable: network

For most messages that the decoder of `A-03` accepts, the re-encoding equals the received payload, because go-ethereum RLP rejects non-canonical encodings. The exceptions are the ROUND-CHANGE cases listed in `A-03` §7 (WBFT-ENC-090), where decode-then-encode is not the identity; for those, the relayed bytes and therefore the message key differ from the received ones.

[WBFT-NET-042] A message that `check_message` classifies as an *extra seal* (`A-05` §12: a message for the previous sequence and the prior round while in `AcceptRequest`, or a current-view PREPARE or COMMIT after the corresponding quorum) and that the core stores or ignores counts as processed without error and MUST be relayed. This includes the case in which the node has no target proposal to check the seal against and stores nothing; in that case the message is relayed whatever its code.
Source: consensus/wbft/core/handler.go:211-224 (the `errExtraSealMessage` branch returns the result of `addToExtraSeal`), consensus/wbft/core/extraseal.go:29-48, consensus/wbft/core/extraseal.go:86
Observable: network

[WBFT-NET-043] A node MUST NOT relay a message in any of the following cases:

1. the message was discarded as a duplicate (WBFT-NET-024) or while the engine was stopped (WBFT-NET-027);
2. the code is `0x11`;
3. the payload does not decode as the message type of its code (`A-03`);
4. the message signature, or any signature in its justification, does not verify against the applicable validator set (`A-05`);
5. the core classifies the view as invalid, old or too far in the future (`A-05` `check_message` results `INVALID`, `OLD`, `TOO_FAR`);
6. the message is a future message (`FUTURE`). It is stored in the backlog and relayed only under WBFT-NET-041. It is never relayed if it is dropped from the backlog: it came from the node itself, its `(code, sequence, round)` slot for that sender is already taken, or the sender already has `MAX_BACKLOG_SIZE_PER_VALIDATOR` (88 in the reference, `A-01` §4.6) backlogged messages; or, when the backlog is processed, its sender is no longer in the current validator set, or it is classified `OLD`, `INVALID` or `TOO_FAR` instead of `FUTURE`;
7. an extra seal whose digest does not match the target proposal (the prior proposal in `AcceptRequest`, otherwise the current one) or whose seal does not verify; or a message other than PREPARE or COMMIT classified as an extra seal while the node has a target proposal (`errInvalidExtraSealMessage`);
8. the message handler of `A-05` returns an error: PRE-PREPARE not from the proposer, sequence different from the block number, justification invalid, block verification failed (including the "future block" deferral); PREPARE or COMMIT digest mismatch or invalid seal; ROUND-CHANGE whose prepared block number differs from the current sequence or whose prepared block hash differs from its prepared digest; a proposer's ROUND-CHANGE quorum evaluation that finds no proposal to propose.

Source: consensus/wbft/core/handler.go:128-150, consensus/wbft/core/handler.go:188-227, consensus/wbft/core/backlog.go:125-244, consensus/wbft/core/backlog.go:254-298, consensus/wbft/core/preprepare.go:115-169, consensus/wbft/core/prepare.go:87-115, consensus/wbft/core/commit.go:90-118, consensus/wbft/core/roundchange.go:103-186, consensus/wbft/core/extraseal.go:49-85
Observable: network

A ROUND-CHANGE that makes the proposer's quorum check run and fail the justification check is still relayed (the handler logs the failure and returns without error, `consensus/wbft/core/roundchange.go:197-204`).

---

## 8. Outcome classes

Every `istanbul` message received on a consensus link ends in exactly one of four classes. The classes are observable from outside: DISCONNECT closes the connection, ACCEPT produces relays (and possibly new messages from the receiver, `A-05`), and DROP-SILENT and IGNORE produce neither. DROP-SILENT and IGNORE differ in whether the message reached the core, which is visible in the node's log only.

| Class | Effect | Conditions (complete) | Rules |
|---|---|---|---|
| DISCONNECT | Connection closed. No penalty is recorded by the consensus layer; devp2p's usual reconnection limits apply (an inbound attempt from the same non-LAN IP within 30 s of the previous one is rejected, and a node is not redialled within 35 s of the previous dial) | (1) devp2p frame error or code outside all negotiated ranges; (2) payload longer than 10 MiB (or 16 MiB at the RLPx layer); (3) code `0x11..0x15` while the engine is stopped and the node is not synchronising; (4) code `0x11` whose payload is not an RLP byte string; (5) code `0x12..0x15` with empty payload; (6) any read error on the `istanbul` stream | WBFT-NET-012, -013, -020, -021, -027 |
| DROP-SILENT | Not delivered to the core. Not relayed | (1) code `0x00..0x10` (including `0x07`); (2) code `0x11..0x15` while the engine is stopped and the node is synchronising; (3) code `0x11..0x15` whose message key is in the known cache | WBFT-NET-020, -024, -027, -028 |
| IGNORE | Delivered to the core, which drops or stores it. Not relayed now | the cases 2–8 of WBFT-NET-043. Future messages and future-block PRE-PREPAREs are stored and may move to ACCEPT later (WBFT-NET-041) | WBFT-NET-043 |
| ACCEPT | Processed by the core and relayed (§7) | the core's processing returned no error, including stored extra seals (WBFT-NET-042) and a later successful backlog or deferred processing | WBFT-NET-040, -041, -042 |

[WBFT-NET-044] A node MUST NOT disconnect or otherwise penalise a peer for a message in class IGNORE, including messages with invalid signatures, invalid seals or invalid proposals.
Source: eth/handler_istanbul.go:132-153 (only errors returned by `HandleMsg` end the loop; core errors are never returned to it), consensus/wbft/backend/handler.go:96-100
Observable: network

In the reference implementation, an invalid message costs the receiver one decode and one signature recovery (and, for a PRE-PREPARE from the proposer, block verification). The only protections are the known cache (identical bytes) and the backlog limits (`A-05`).

---

## 9. Topology and identity

### 9.1 Required connectivity

Consensus messages travel only over consensus links between validators (§6.2), and a message is relayed only by a validator that has processed it successfully (§7). A validator that is behind (processing the message as `FUTURE` or dropping it as `TOO_FAR`) does not pass it on at that time.

[WBFT-NET-050] For liveness, the graph formed by the current validators and the consensus links between them MUST be connected. Operators SHOULD keep every pair of validators directly connected (full mesh), and SHOULD configure validators as each other's trusted peers, which exempts those links from the devp2p and eth peer limits (`maxpeers`), and as static peers, which makes the node keep dialling them.
Source: consensus/wbft/backend/backend.go:176-210, eth/handler_istanbul.go:42-54, eth/handler.go:405-409 (trusted peers bypass the eth peer limit), p2p/server.go:817-822 (and the devp2p limit; static peers are not exempt)
Observable: network

### 9.2 Key identity

A node selects gossip targets by the address of the remote devp2p public key. A validator therefore receives consensus messages only if its devp2p key is the key of its validator address.

[WBFT-NET-051] A validator MUST use the same secp256k1 key as its devp2p node key and as its consensus signing key (the key whose address is its validator address, from which its BLS key is derived, `A-02`).
Source: eth/backend.go:164 (`CreateConsensusEngine(..., stack.Config().NodeKey(), ...)`), consensus/wbft/backend/backend.go:61-86, eth/handler_istanbul.go:46-50
Observable: network

A validator whose devp2p key differs from its validator key receives neither gossip nor relays. It can send, and its messages are accepted by signature, but it never receives PRE-PREPAREs or votes. It therefore cannot prepare or commit, and it counts as a crash fault.

### 9.3 Discovery (informative)

Peer discovery is unchanged from go-ethereum: discv4/discv5, DNS lists, `static-nodes`/`trusted-nodes` and `admin_addPeer`. A discovery key that a node uses in ECDH is subject to the point check of `A-10` WBFT-SEC-032. There is no validator-specific discovery, and the ENR carries the `eth` fork-ID entry and, when snap sync support is enabled (the default, `eth/backend.go:514-516`, `eth/protocols/snap/handler.go:112`), the `snap` entry; there is no `istanbul` entry. Nothing in the protocol makes a node prefer validators as peers. With a validator count close to `maxpeers`, or with many non-validator peers, validators may fail to connect to each other unless they are trusted peers of each other.

---

## 10. Worked example: one PREPARE over the wire

Validator `A` (4 validators `A, B, C, D`, full mesh, eth/68 and istanbul/100 negotiated, snappy on) creates a PREPARE with RLP encoding `P` of length 213 bytes.

1. `A` checks it is in the validator set, computes `k = keccak256(0xb8 ‖ 0xd5 ‖ P)` (`213 = 0xd5 > 55`), adds `k` to its known cache, adds `k` to the recent caches of `B`, `C`, `D`, and sends to each a devp2p message with wire code `0x34` and payload `P` (frame data `0x34 ‖ snappy(P)`). It also delivers `P` to its own core.
2. `B` receives `(0x13, P)` from `A`: adds `k` to `recent[A]`; `k` is not known → adds `k` to its known cache and delivers to its core. The PREPARE is valid and for `B`'s current view; the core processes it without error.
3. `B` relays: targets `{A, C, D}` minus self; `recent[A]` has `k` → skip; `recent[C]` and `recent[D]` do not → sends `P` to `C` and `D`.
4. `C` received `P` from `A` as well and processes and relays it in the same way: it skips `A` (`recent[A]` has `k`) and sends to `B` and `D` unless it has already received `P` from them. A copy that arrives at a node which already has `k` in its known cache (for example `B`'s relay reaching `C` after `A`'s original) only adds `k` to the recent cache of the sending peer and is dropped silently. Which of the relays between `B`, `C` and `D` actually happen depends on timing.

An observer on all six links sees `P` at most once in each direction of each link.

---

## 11. Implementation notes (informative)

- `Backend.HandleMsg` runs under an exclusive lock (`coreMu`) for every `istanbul` message from every peer (`consensus/wbft/backend/handler.go:71-72`). Receive processing up to the hand-off to the core is therefore serialised across peers.
- The caches are go-ethereum `common/lru` caches; the per-peer cache is an LRU of LRUs (`consensus/wbft/backend/backend.go:62-63`, `consensus/wbft/backend/engine.go:39-42`). Both are in memory only and are lost on restart.
- `consensusRw` is written by the `istanbul` handler goroutine and read by gossip goroutines without synchronisation (`eth/protocols/eth/peer.go:100`, `:519-530`).
- The eth fetcher is told about blocks decided by consensus through `Enqueue("istanbul", block)` (`consensus/wbft/backend/backend.go:245-247`), unless the block is the one this node's own `Seal` is waiting for, which is handed back to the miner instead (`consensus/wbft/backend/backend.go:239-243`); `"istanbul"` is a pseudo peer ID. Block propagation after a decision uses the normal `eth` announcements (`B-09`), not `istanbul/100`.

---

## 12. Version notes

`git diff v1.1.0 740526d03` is empty for `eth/handler_istanbul.go`, `eth/quorum_protocol.go`, `eth/handler.go`, `eth/protocols/eth/peer.go`, `p2p/peer.go`, `p2p/message.go`, `consensus/wbft/backend/` and `consensus/wbft/core/`. Everything in this chapter applies to `v1.1.0` as well.
