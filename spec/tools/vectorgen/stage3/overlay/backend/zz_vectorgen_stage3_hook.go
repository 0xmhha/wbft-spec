// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Added to package backend by tools/vectorgen/stage3 through `go test -overlay`
// (never in the reference repository). The overlay copies of handler.go and
// backend.go call these hooks instead of posting or sending from a goroutine.
// With no recorder installed each hook does exactly what the replaced line did.
package backend

import (
	"time"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/wbft"
)

var (
	// vgRecvRec records a message that HandleMsg delivers to the core.
	vgRecvRec func(ev wbft.MessageEvent)
	// vgSelfRec records the self-delivery of a broadcast message.
	vgSelfRec func(ev wbft.MessageEvent)
	// vgSendRec records a send to a peer.
	vgSendRec func(p consensus.Peer, code uint64, payload []byte)
	// vgNow is the clock of NotifyNewRound (timers/build_wait).
	vgNow *time.Time
)

// vgUntil replaces `time.Until` in NotifyNewRound.
func vgUntil(t time.Time) time.Duration {
	if vgNow != nil {
		return t.Sub(*vgNow)
	}
	return time.Until(t)
}

// vgPostReceived replaces `go sb.istanbulEventMux.Post(wbft.MessageEvent{...})` in HandleMsg.
func vgPostReceived(sb *Backend, ev wbft.MessageEvent) {
	if vgRecvRec != nil {
		vgRecvRec(ev)
		return
	}
	go sb.istanbulEventMux.Post(ev)
}

// vgPostSelf replaces `go sb.istanbulEventMux.Post(msg)` in Broadcast.
func vgPostSelf(sb *Backend, ev wbft.MessageEvent) {
	if vgSelfRec != nil {
		vgSelfRec(ev)
		return
	}
	go sb.istanbulEventMux.Post(ev)
}

// vgSend replaces `go p.SendWBFTConsensus(outboundCode, payload)` in Gossip.
func vgSend(p consensus.Peer, code uint64, payload []byte) {
	if vgSendRec != nil {
		vgSendRec(p, code, payload)
		return
	}
	go p.SendWBFTConsensus(code, payload)
}
