// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Generator test of vectorgen stage 3 (A-11 §3.3, network/receive_outcome).
// Injected into package backend with `go test -overlay`; the reference
// repository is not modified. A real Backend (HandleMsg, the dedup caches,
// Broadcast and Gossip with a fake peer set) is combined with a reference Core
// driven by core.VGDriver. Writes $VECTORGEN_STAGE3_OUT/receive_outcome.jsonl.
package backend

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	"github.com/ethereum/go-ethereum/consensus/wbft/messages"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
)

type (
	vgM  = wbftcore.VGM
	vgKV = wbftcore.VGKV
)

var vgHex = wbftcore.VGHex

type vgPeer struct{ addr common.Address }

func (p *vgPeer) SendWBFTConsensus(uint64, []byte) error { return nil }

type vgBroadcaster struct {
	peers map[common.Address]consensus.Peer
}

func (b *vgBroadcaster) Enqueue(string, *types.Block) {}
func (b *vgBroadcaster) FindPeers(targets map[common.Address]bool) map[common.Address]consensus.Peer {
	m := map[common.Address]consensus.Peer{}
	for a, p := range b.peers {
		if targets[a] {
			m[a] = p
		}
	}
	return m
}

// Record layout of a network step: outcome, dedup_key, relay_to, then the
// state-machine record (check, relay, sent, timers, new_round, finalized,
// scheduled, state).
const vgSchedIdx = 3 + 6

// vgMaxMsgSize is protocolMaxMsgSize of eth/handler.go:59 (10 MiB).
const vgMaxMsgSize = 10 * 1024 * 1024

// vgNet is one network case: a Backend of node key `node` connected to `peers`.
type vgNet struct {
	sb       *Backend
	d        *wbftcore.VGDriver
	running  bool
	syncing  bool
	peers    []common.Address
	valKeys  []int
	node     int
	head     *types.Block
	bucket   *[]common.Address
	bSends   [][]common.Address // targets per own broadcast of the current step
	relay    []common.Address
	received *wbft.MessageEvent
	inSteps  []any
	outSteps []any
	start    vgM
	lastRec  vgM
}

func newVGNet(node int, valKeys []int, head *types.Block, peers []common.Address, running, syncing bool) *vgNet {
	cfg := *wbft.DefaultConfig
	n := &vgNet{node: node, valKeys: valKeys, head: head, peers: peers, running: running, syncing: syncing}
	n.sb = New(&cfg, wbftcore.VGKey(node), rawdb.NewMemoryDatabase())
	bc := &vgBroadcaster{peers: map[common.Address]consensus.Peer{}}
	for _, a := range peers {
		bc.peers[a] = &vgPeer{a}
	}
	n.sb.SetBroadcaster(bc)
	app := wbftcore.NewVGApp(node, valKeys, head)
	app.BroadcastFn = func(vs wbft.ValidatorSet, code uint64, payload []byte) error {
		var to []common.Address
		n.bucket = &to
		err := n.sb.Broadcast(vs, code, payload)
		n.bucket = nil
		if err == nil {
			n.bSends = append(n.bSends, to)
		}
		return err
	}
	app.GossipFn = func(vs wbft.ValidatorSet, code uint64, payload []byte) error {
		n.bucket = &n.relay
		err := n.sb.Gossip(vs, code, payload)
		n.bucket = nil
		return err
	}
	n.d = wbftcore.NewVGDriver(app)
	vgRecvRec = func(ev wbft.MessageEvent) { e := ev; n.received = &e }
	vgSelfRec = func(wbft.MessageEvent) {} // self-deliveries are steps of the case
	vgSendRec = func(p consensus.Peer, code uint64, payload []byte) {
		if n.bucket == nil {
			panic("send outside a broadcast or relay")
		}
		*n.bucket = append(*n.bucket, p.(*vgPeer).addr)
	}
	if running {
		n.sb.coreStarted = true
		n.sb.core = n.d.C
		n.begin()
		n.start = n.decorate(n.d.Start(), nil, nil)
	}
	return n
}

func vgAddrList(as []common.Address) []any {
	s := make([]string, 0, len(as))
	for _, a := range as {
		s = append(s, vgHex(a.Bytes()))
	}
	sort.Strings(s)
	return wbftcore.VGList(s)
}

// decorate adds the targets of each own broadcast and the network fields.
func (n *vgNet) decorate(rec vgM, outcome, key any) vgM {
	sent := rec[2].V.([]any)
	if len(sent) != len(n.bSends) {
		panic(fmt.Sprintf("%d messages sent, %d target lists", len(sent), len(n.bSends)))
	}
	for i := range sent {
		sent[i] = append(sent[i].(vgM), vgKV{K: "to", V: vgAddrList(n.bSends[i])})
	}
	var relayTo any
	if rec[1].V == true {
		relayTo = vgAddrList(n.relay)
	}
	out := vgM{{K: "outcome", V: outcome}, {K: "dedup_key", V: key}, {K: "relay_to", V: relayTo}}
	return append(out, rec...)
}

func (n *vgNet) begin() {
	n.bSends, n.relay, n.received = nil, nil, nil
}

func (n *vgNet) add(in, out vgM) vgM {
	n.inSteps = append(n.inSteps, in)
	n.outSteps = append(n.outSteps, out)
	n.lastRec = out
	return out
}

// frame delivers an `istanbul` message (code, payload) from peer `from`.
func (n *vgNet) frame(label string, from common.Address, code uint64, payload []byte) vgM {
	n.begin()
	in := vgM{{K: "kind", V: "frame"}, {K: "label", V: label}, {K: "peer", V: vgHex(from.Bytes())}, {K: "code", V: wbftcore.VGU(code)}, {K: "payload", V: vgHex(payload)}}
	if len(payload) > vgMaxMsgSize {
		// eth/handler_istanbul.go:138-140: the handler returns errMsgTooLarge
		// before HandleMsg, which closes the connection (A-07 WBFT-NET-013);
		// the rule is composed by the generator. The frame never reaches the
		// deduplication caches, so it has no dedup key.
		if !n.running {
			return n.add(in, vgM{{K: "outcome", V: "DISCONNECT"}, {K: "dedup_key", V: nil}, {K: "relay_to", V: nil}})
		}
		return n.add(in, n.decorate(n.d.Idle(), "DISCONNECT", nil))
	}
	msg := p2p.Msg{Code: code, Size: uint32(len(payload)), Payload: bytes.NewReader(payload)}
	handled, err := n.sb.HandleMsg(from, msg)
	var key any
	if n.running && code >= 0x11 && code <= 0x15 && len(payload) > 0 {
		// the key as HandleMsg computes it (Backend.decode: 0x11 is read as
		// one RLP byte string from the stream, trailing bytes ignored)
		if _, h, err := n.sb.decode(p2p.Msg{Code: code, Size: uint32(len(payload)), Payload: bytes.NewReader(payload)}); err == nil {
			key = vgHex(h.Bytes())
		}
	}
	var outcome string
	switch {
	case errors.Is(err, wbft.ErrStoppedEngine):
		// eth/handler_istanbul.go:118-127: kept (and ignored) while synchronising, otherwise the handler returns the error
		outcome = map[bool]string{true: "DROP_SILENT", false: "DISCONNECT"}[n.syncing]
	case err != nil:
		outcome = "DISCONNECT"
	case !handled || n.received == nil:
		outcome = "DROP_SILENT"
	}
	if outcome != "" {
		if !n.running {
			return n.add(in, vgM{{K: "outcome", V: outcome}, {K: "dedup_key", V: key}, {K: "relay_to", V: nil}})
		}
		return n.add(in, n.decorate(n.d.Idle(), outcome, key))
	}
	rec := n.d.Message(n.received.Code, n.received.Payload)
	outcome = map[bool]string{true: "ACCEPT", false: "IGNORE"}[rec[1].V == true]
	return n.add(in, n.decorate(rec, outcome, key))
}

// self delivers the node's own broadcasts of the last step to its core
// (Broadcast bypasses the known cache, A-07 WBFT-NET-030).
func (n *vgNet) self(label string) {
	codes, pls := n.d.SentPayloads()
	for i := range codes {
		n.begin()
		in := vgM{{K: "kind", V: "message"}, {K: "label", V: label}, {K: "code", V: wbftcore.VGU(codes[i])}, {K: "payload", V: vgHex(pls[i])}}
		n.add(in, n.decorate(n.d.Message(codes[i], pls[i]), nil, nil))
	}
}

func (n *vgNet) timeout(label string) vgM {
	n.begin()
	return n.add(vgM{{K: "kind", V: "round_timeout"}, {K: "label", V: label}}, n.decorate(n.d.RoundTimeout(), nil, nil))
}

func (n *vgNet) backlog(label string, code uint64, enc []byte) vgM {
	n.begin()
	return n.add(vgM{{K: "kind", V: "backlog"}, {K: "label", V: label}, {K: "code", V: wbftcore.VGU(code)}, {K: "payload", V: vgHex(enc)}}, n.decorate(n.d.Backlog(enc), nil, nil))
}

func (n *vgNet) finish(name, desc string, reqs []string) wbftcore.VGCase {
	n.d.Uninstall()
	vgRecvRec, vgSelfRec, vgSendRec = nil, nil, nil
	vals := []any{}
	for _, i := range n.valKeys {
		vals = append(vals, vgM{{K: "address", V: vgHex(wbftcore.VGAddr(i).Bytes())}, {K: "bls_public_key", V: vgHex(wbftcore.VGBLS(i).PublicKey().Marshal())}})
	}
	in := vgM{
		{K: "initial", V: vgM{
			{K: "validators", V: vals},
			{K: "proposer_policy", V: "0"},
			{K: "node_key", V: vgHex(wbftcore.VGKey(n.node).D.FillBytes(make([]byte, 32)))},
			{K: "head", V: vgHex(wbftcore.VGEncode(n.head))},
			{K: "app", V: vgM{{K: "invalid_proposals", V: []any{}}, {K: "future_proposals", V: []any{}}, {K: "bad_blocks", V: []any{}}, {K: "finalize", V: "ok"}}},
			{K: "engine", V: map[bool]string{true: "running", false: "stopped"}[n.running]},
			{K: "synchronising", V: n.syncing},
			{K: "peers", V: vgAddrList(n.peers)},
		}},
		{K: "steps", V: n.inSteps},
	}
	var start any
	if n.start != nil {
		start = n.start
	}
	ex := vgM{{K: "start", V: start}, {K: "steps", V: n.outSteps}}
	return wbftcore.VGCase{Runner: "network", Handler: "receive_outcome", Name: name, Kind: "steps", Desc: desc, Reqs: reqs, Input: in, Expected: ex}
}

func TestVectorgenStage3ReceiveOutcome(t *testing.T) {
	dir := os.Getenv("VECTORGEN_STAGE3_OUT")
	if dir == "" {
		t.Skip("VECTORGEN_STAGE3_OUT not set")
	}
	A := wbftcore.VGAddr
	vals := []int{0, 1, 2, 3}
	head := wbftcore.VGBlock(9, common.BytesToHash([]byte("block-8")), A(3), 0)
	B := wbftcore.VGBlock(10, head.Hash(), A(0), 1)
	peers := []common.Address{A(0), A(2), A(3)}
	var cs []wbftcore.VGCase
	pp := wbftcore.VGPreprepare(0, 10, 0, B, nil, nil)
	_, p2 := wbftcore.VGPrepare(2, B, 10, 0)

	// 1. accept, relay targets, known-cache drop, message relayed by another peer
	{
		n := newVGNet(1, vals, head, peers, true, false)
		n.frame("PRE-PREPARE(10, 0, B) of v0 from peer v0: accepted, relayed to v2 and v3 (v0 already has the key)", A(0), messages.PreprepareCode, pp)
		n.self("own PREPARE")
		n.frame("the same PRE-PREPARE from peer v2: key in the known cache", A(2), messages.PreprepareCode, pp)
		n.frame("PREPARE of v2 received from peer v3: attributed to v2 by its signature, relayed to v0 and v2", A(3), messages.PrepareCode, p2)
		n.frame("the same PREPARE from peer v2: known", A(2), messages.PrepareCode, p2)
		cs = append(cs, n.finish("accept_relay_and_known", "N = 4, node v1 connected to v0, v2, v3: an accepted message is relayed with the received bytes to the validators whose recent cache lacks its key; a repeat from any peer is dropped silently; the sender of a message is its signer, not the peer (A-07 §5.2, §6.2, §7)",
			[]string{"WBFT-NET-022", "WBFT-NET-023", "WBFT-NET-024", "WBFT-NET-026", "WBFT-NET-030", "WBFT-NET-031", "WBFT-NET-032", "WBFT-NET-040", "WBFT-SM-011"}))
	}
	// 2. IGNORE classes and a later accept from the backlog
	{
		n := newVGNet(1, vals, head, peers, true, false)
		n.frame("PREPARE(10, 0, B) of v2 in AcceptRequest: FUTURE, stored, not relayed", A(2), messages.PrepareCode, p2)
		_, pOut := wbftcore.VGPrepare(7, B, 10, 0)
		n.frame("PREPARE signed by key 7, which is not a validator: IGNORE", A(3), messages.PrepareCode, pOut)
		_, pOld := wbftcore.VGPrepare(3, head, 8, 0)
		n.frame("PREPARE(8, 0) of v3: OLD, IGNORE", A(3), messages.PrepareCode, pOld)
		n.frame("undecodable payload under code 0x13: IGNORE", A(3), messages.PrepareCode, []byte{0xc2, 0x01, 0x02})
		n.frame("PRE-PREPARE(10, 0, B) of v0: accepted; the stored PREPARE of v2 is scheduled for replay", A(0), messages.PreprepareCode, pp)
		sched := n.lastRec[vgSchedIdx].V.([]any)
		n.self("own PREPARE")
		for _, x := range sched {
			m := x.(vgM)
			var code uint64
			fmt.Sscan(m[1].V.(string), &code)
			n.backlog("replay of the stored PREPARE of v2: processed and relayed with its re-encoding", code, common.FromHex(m[2].V.(string)))
		}
		_, p0 := wbftcore.VGPrepare(0, B, 10, 0)
		n.frame("PREPARE of v0: quorum", A(0), messages.PrepareCode, p0)
		cs = append(cs, n.finish("ignore_then_backlog_accept", "N = 4, node v1: FUTURE, non-validator signature, OLD and undecodable messages are IGNORE (not relayed, no disconnect); the FUTURE message is relayed when its replay succeeds (A-07 §7, §8)",
			[]string{"WBFT-NET-041", "WBFT-NET-042", "WBFT-NET-043", "WBFT-NET-044", "WBFT-SM-012", "WBFT-SM-013"}))
	}
	// 3. codes and framing
	{
		n := newVGNet(1, vals, head, peers, true, false)
		junk, _ := rlp.EncodeToBytes([]byte("not a consensus message"))
		n.frame("code 0x11 wrapping bytes that are not a message: delivered under code 0x11, which the core rejects: IGNORE", A(0), 0x11, junk)
		n.frame("code 0x11 whose payload is not an RLP byte string: DISCONNECT", A(0), 0x11, []byte{0xc1, 0x80})
		n.frame("code 0x12 with an empty payload: DISCONNECT", A(0), messages.PreprepareCode, []byte{})
		n.frame("code 0x10: not handled by the consensus handler, DROP_SILENT", A(0), 0x10, []byte{0x80})
		nb, _ := rlp.EncodeToBytes(struct {
			Block *types.Block
			TD    *big.Int
		}{B, big.NewInt(10)})
		n.frame("code 0x07 (NewBlock) while the node is not the proposer: DROP_SILENT", A(0), 0x07, nb)
		cs = append(cs, n.finish("codes_and_framing", "N = 4, node v1: outcome classes by code and framing (A-07 §5.1, §8); for an error returned by HandleMsg the DISCONNECT is decided by eth/handler_istanbul.go, whose rule the generator composes",
			[]string{"WBFT-NET-020", "WBFT-NET-021", "WBFT-NET-028", "WBFT-NET-043"}))
	}
	// 4. engine stopped
	for _, syncing := range []bool{true, false} {
		n := newVGNet(1, vals, head, peers, false, syncing)
		wrapped, _ := rlp.EncodeToBytes(pp)
		n.frame("PRE-PREPARE while the engine is stopped", A(0), messages.PreprepareCode, pp)
		n.frame("code 0x11 while the engine is stopped", A(0), 0x11, wrapped)
		n.frame("code 0x10 while the engine is stopped", A(0), 0x10, []byte{0x80})
		name, what := "engine_stopped_synchronising", "the node is synchronising: consensus codes are dropped silently and the connection is kept"
		if !syncing {
			name, what = "engine_stopped_not_synchronising", "the node is not synchronising: a consensus code disconnects the peer"
		}
		cs = append(cs, n.finish(name, "engine stopped, "+what+" (A-07 §5.4; the rule of eth/handler_istanbul.go:118-127 is composed by the generator)",
			[]string{"WBFT-NET-020", "WBFT-NET-027"}))
	}
	// 5. extra seal without a target, relayed; non-validator peer
	{
		np := append(append([]common.Address{}, peers...), A(7))
		n := newVGNet(1, vals, head, np, true, false)
		_, p9 := wbftcore.VGPrepare(2, head, 9, 0)
		n.frame("PREPARE(9, 0) of v2 in (10, 0) without a prior proposal: extra seal with no target, relayed", A(7), messages.PrepareCode, p9)
		n.frame("PRE-PREPARE(10, 0, B) of v0 from the non-validator peer: accepted and relayed to the validators only", A(7), messages.PreprepareCode, pp)
		cs = append(cs, n.finish("extra_seal_and_non_validator_peer", "N = 4, node v1 also connected to a non-validator peer (key 7): messages from that peer are accepted by signature, an extra seal without a target block is relayed, and neither relays nor own broadcasts go to the non-validator peer (A-07 WBFT-NET-026, WBFT-NET-031, WBFT-NET-042)",
			[]string{"WBFT-NET-026", "WBFT-NET-031", "WBFT-NET-042", "WBFT-SM-063"}))
	}
	// 6. own broadcast targets
	{
		n := newVGNet(2, vals, head, []common.Address{A(0), A(1)}, true, false)
		n.timeout("round timer of (10, 0) expires: ROUND-CHANGE(10, 1) to the connected validators v0 and v1")
		n.self("own ROUND-CHANGE")
		_, rc0 := wbftcore.VGRoundChange(0, 10, 1, nil, nil, nil)
		n.frame("ROUND-CHANGE(10, 1) of v0 from peer v1: relayed to v0 only", A(1), messages.RoundChangeCode, rc0)
		cs = append(cs, n.finish("broadcast_targets", "N = 4, node v2 connected to v0 and v1 only: its own ROUND-CHANGE goes to the connected validators; a ROUND-CHANGE of v0 received from v1 is relayed to v0 only",
			[]string{"WBFT-NET-030", "WBFT-NET-031", "WBFT-NET-032", "WBFT-NET-040"}))
	}

	// 7. size limit and trailing bytes after a 0x11 payload
	{
		n := newVGNet(1, vals, head, peers, true, false)
		n.frame("code 0x12 with a payload of 10 485 761 bytes (one above 10 MiB): DISCONNECT before the payload is read", A(0), messages.PreprepareCode, make([]byte, vgMaxMsgSize+1))
		junk, _ := rlp.EncodeToBytes([]byte("not a consensus message either"))
		n.frame("code 0x11 whose payload is an RLP byte string followed by two more bytes: the bytes after the first RLP item are ignored, the content is delivered under code 0x11 and rejected by the core: IGNORE", A(2), 0x11, append(junk, 0x00, 0x01))
		n.frame("PRE-PREPARE(10, 0, B) of v0 from peer v3: accepted", A(3), messages.PreprepareCode, pp)
		cs = append(cs, n.finish("size_limit_and_legacy_trailing_bytes", "N = 4, node v1: an istanbul message longer than 10 MiB closes the connection; the rule of eth/handler_istanbul.go:138-140 is composed by the generator (A-11 shape 11). A 0x11 payload is read as its first RLP item (A-07 WBFT-NET-012, WBFT-NET-013)",
			[]string{"WBFT-NET-012", "WBFT-NET-013", "WBFT-NET-043"}))
	}

	f, err := os.Create(filepath.Join(dir, "receive_outcome.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	for _, c := range cs {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		bw.Write(b)
		bw.WriteByte('\n')
	}
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------------- build_wait

// TestVectorgenStage3BuildWait runs the reference Backend.NotifyNewRound with
// the clock of its time.Until replaced (vgUntil) and records the wait it hands
// to the block builder (A-06 WBFT-TIMER-040, WBFT-TIMER-041).
func TestVectorgenStage3BuildWait(t *testing.T) {
	dir := os.Getenv("VECTORGEN_STAGE3_OUT")
	if dir == "" {
		t.Skip("VECTORGEN_STAGE3_OUT not set")
	}
	const T = 1_700_000_000
	type in struct {
		name, desc string
		reqs       []string
		bp         uint64
		headTime   uint64
		round      uint64
		now        int64 // Unix nanoseconds
	}
	s := int64(1_000_000_000)
	ins := []in{
		{"round_0_head_just_made", "round 0, 250 ms after the head time, block period 1: wait until head time + 1 s", []string{"WBFT-TIMER-040"}, 1, T, 0, T*s + 250_000_000},
		{"round_0_at_head_time", "round 0 at the head time, block period 2: wait 2 s", []string{"WBFT-TIMER-040"}, 2, T, 0, T * s},
		{"round_0_clock_behind_head", "round 0, the clock 3 s behind the head time, block period 1: wait 4 s", []string{"WBFT-TIMER-040"}, 1, T, 0, (T - 3) * s},
		{"round_0_due_exactly", "round 0 exactly at head time + block period: no wait", []string{"WBFT-TIMER-040"}, 1, T, 0, (T + 1) * s},
		{"round_0_late", "round 0, 5.5 s after the head time with block period 1: the computed wait is negative, so building starts at once", []string{"WBFT-TIMER-040"}, 1, T, 0, T*s + 5_500_000_000},
		{"round_1_no_wait", "round 1, 250 ms after the head time: no wait", []string{"WBFT-TIMER-041"}, 1, T, 1, T*s + 250_000_000},
		{"round_5_clock_behind_head", "round 5 with the clock behind the head time: no wait", []string{"WBFT-TIMER-041"}, 3, T, 5, (T - 10) * s},
	}
	var cs []wbftcore.VGCase
	for _, x := range ins {
		cfg := *wbft.DefaultConfig
		cfg.BlockPeriod = x.bp
		sb := New(&cfg, wbftcore.VGKey(0), rawdb.NewMemoryDatabase())
		head := types.NewBlockWithHeader(&types.Header{Number: big.NewInt(9), Time: x.headTime})
		sb.currentBlock = func() *types.Block { return head }
		var got time.Duration
		called := false
		sb.notifyNewRound = func(w time.Duration, r *big.Int) {
			called = true
			if r.Uint64() != x.round {
				t.Fatalf("round %v", r)
			}
			got = w
		}
		now := time.Unix(0, x.now)
		vgNow = &now
		sb.NotifyNewRound(new(big.Int).SetUint64(x.round))
		vgNow = nil
		if !called {
			t.Fatal("notifyNewRound not called")
		}
		desc := x.desc + fmt.Sprintf("; the reference computes %d ns, and the builder sleeps for that duration (time.Sleep returns at once for a duration <= 0), so the effective wait is its maximum with 0", int64(got))
		w := got
		if w < 0 {
			w = 0
		}
		cs = append(cs, wbftcore.VGCase{Runner: "timers", Handler: "build_wait", Name: x.name, Kind: "pure", Desc: desc, Reqs: x.reqs,
			Input: vgM{{K: "block_period", V: wbftcore.VGU(x.bp)}, {K: "head_time", V: wbftcore.VGU(x.headTime)}, {K: "round", V: wbftcore.VGU(x.round)},
				{K: "now", V: fmt.Sprint(x.now)}},
			Expected: vgM{{K: "wait", V: fmt.Sprint(int64(w))}}})
	}
	f, err := os.Create(filepath.Join(dir, "build_wait.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	for _, c := range cs {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		bw.Write(b)
		bw.WriteByte('\n')
	}
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
}
