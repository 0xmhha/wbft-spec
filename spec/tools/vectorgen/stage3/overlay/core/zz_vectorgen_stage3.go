// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Added to package core by tools/vectorgen/stage3 through `go test -overlay`
// (never in the reference repository). It is a non-test file so that the
// generator tests of package consensus/wbft/backend can use it as well.
//
// It holds
//   - the hooks that the overlay copies of core.go, backlog.go and request.go
//     call instead of time.AfterFunc and `go c.sendEvent(...)`. With no
//     recorder installed they behave exactly like the replaced code. With a
//     recorder the timers are recorded and never fire, and scheduled events
//     are recorded instead of being posted from a goroutine, so that the
//     generator decides when (and whether) they are processed;
//   - VGApp, a fake application backend (A-05 §1.3) whose answers come from the
//     vector input;
//   - VGDriver, which applies the steps of a `steps` vector (A-11 §3.1) to a
//     reference Core synchronously, as handleEvents would, and records the
//     observable outputs of each step;
//   - helpers that build blocks and signed messages from the vector keys.
package core

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/consensus/wbft/messages"
	"github.com/ethereum/go-ethereum/consensus/wbft/validator"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// ------------------------------------------------------------------ hooks

var (
	// vgTimerRec records an armed timer: kind "round" (seq, round, the
	// cancellation flag of the timer), "retry" (round) or "future" (the view
	// and the proposal of the deferred PRE-PREPARE, and the function the timer
	// would run). t is the placeholder timer handed to the core, which the
	// core stops when it cancels the timer.
	vgTimerRec func(kind string, seq, round *big.Int, t *time.Timer, canceled *bool, f func(), proposal common.Hash)
	// vgScheduleRec records an event that the core schedules for later processing.
	vgScheduleRec func(ev interface{})
)

const vgNever = 1000 * time.Hour

// vgRoundTimer replaces `time.AfterFunc` in newRoundChangeTimer. The
// cancellation flag of the new timer is c.lastSentTimeoutCanceled, which
// newRoundChangeTimer has set just before.
func vgRoundTimer(c *Core, seq, round *big.Int, d time.Duration, f func()) *time.Timer {
	if vgTimerRec != nil {
		t := time.AfterFunc(vgNever, func() {})
		vgTimerRec("round", seq, round, t, c.lastSentTimeoutCanceled, nil, common.Hash{})
		return t
	}
	return time.AfterFunc(d, f)
}

// vgRetryTimer replaces `time.AfterFunc` in newRetrySendingRoundChangeTimer.
func vgRetryTimer(round *big.Int, d time.Duration, f func()) *time.Timer {
	if vgTimerRec != nil {
		t := time.AfterFunc(vgNever, func() {})
		vgTimerRec("retry", nil, round, t, nil, nil, common.Hash{})
		return t
	}
	return time.AfterFunc(d, f)
}

// vgFutureTimer replaces `time.AfterFunc` of the future-PRE-PREPARE timer in
// handlePreprepareMsg (A-05 WBFT-SM-038).
func vgFutureTimer(pp *messages.Preprepare, d time.Duration, f func()) *time.Timer {
	if vgTimerRec != nil {
		t := time.AfterFunc(vgNever, func() {})
		v := pp.View()
		vgTimerRec("future", v.Sequence, v.Round, t, nil, f, pp.Proposal.Hash())
		return t
	}
	return time.AfterFunc(d, f)
}

// vgSchedule replaces `go c.sendEvent(ev)` in processBacklog and processPendingRequests.
func vgSchedule(c *Core, ev interface{}) {
	if vgScheduleRec != nil {
		vgScheduleRec(ev)
		return
	}
	go c.sendEvent(ev)
}

// vgPost replaces `c.sendEvent(backlogEvent{...})` in the function of the
// future-PRE-PREPARE timer, which runs on the timer's goroutine.
func vgPost(c *Core, ev interface{}) {
	if vgScheduleRec != nil {
		vgScheduleRec(ev)
		return
	}
	c.sendEvent(ev)
}

// ---------------------------------------------------------- ordered JSON

// VGKV is one key/value pair of an ordered mapping.
type VGKV struct {
	K string
	V any
}

// VGM is an ordered mapping; it marshals to a JSON object in insertion order.
type VGM []VGKV

func (m VGM) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.K)
		v, err := json.Marshal(kv.V)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// VGCase is one vector case as written to the JSON lines file.
type VGCase struct {
	Runner, Handler, Name, Kind, Desc, Err string
	Reqs                                   []string
	Input                                  VGM
	Expected                               VGM
}

// VGHex renders bytes as 0x hex; VGDec renders an integer as a decimal string.
func VGHex(b []byte) string { return "0x" + hex.EncodeToString(b) }
func VGDec(x *big.Int) any {
	if x == nil {
		return nil
	}
	return x.String()
}
func VGU(v uint64) string { return strconv.FormatUint(v, 10) }

// VGList converts a slice to []any (empty slices stay empty lists).
func VGList[T any](xs []T) []any {
	out := make([]any, 0, len(xs))
	for _, x := range xs {
		out = append(out, x)
	}
	return out
}

// ------------------------------------------------------------------ keys

// VGKey returns key_i = keccak256(ASCII("wbft-spec-vector-key-" || decimal(i))) (A-11 WBFT-VEC-021).
func VGKey(i int) *ecdsa.PrivateKey {
	k, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("wbft-spec-vector-key-%d", i))))
	if err != nil {
		panic(err)
	}
	return k
}

// VGBLS returns the BLS key derived from key_i (A-02 §5.1).
func VGBLS(i int) bls.SecretKey {
	k, err := bls.DeriveFromECDSA(VGKey(i))
	if err != nil {
		panic(err)
	}
	return k
}

func VGAddr(i int) common.Address { return crypto.PubkeyToAddress(VGKey(i).PublicKey) }

// ---------------------------------------------------------------- blocks

// VGBlock builds a block with a decodable WBFTExtra (so that seal data depends
// on the round, A-02 WBFT-CRYPTO-044) and no transactions. tag makes blocks of
// the same number distinct (it changes the timestamp).
func VGBlock(number uint64, parent common.Hash, coinbase common.Address, tag uint64) *types.Block {
	extra, err := rlp.EncodeToBytes(&types.WBFTExtra{VanityData: make([]byte, 32), GasTip: big.NewInt(0)})
	if err != nil {
		panic(err)
	}
	h := &types.Header{
		ParentHash:  parent,
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    coinbase,
		Root:        crypto.Keccak256Hash([]byte(fmt.Sprintf("wbft-vector-state-root-%d-%d", number, tag))),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      new(big.Int).SetUint64(number),
		GasLimit:    105_000_000,
		Time:        1_700_000_000 + number + 1000*tag,
		Extra:       extra,
		BaseFee:     big.NewInt(20_000_000_000_000),
	}
	return types.NewBlock(h, nil, nil, nil, trie.NewStackTrie(nil))
}

func vgMustEnc(v any) []byte {
	b, err := rlp.EncodeToBytes(v)
	if err != nil {
		panic(err)
	}
	return b
}

func VGEncode(v any) []byte { return vgMustEnc(v) }

// ---------------------------------------------------------------- messages

func vgSign(key *ecdsa.PrivateKey, m messages.WBFTMessage) []byte {
	p, err := m.EncodePayloadForSigning()
	if err != nil {
		panic(err)
	}
	sig, err := crypto.Sign(crypto.Keccak256(p), key)
	if err != nil {
		panic(err)
	}
	m.SetSignature(sig)
	m.SetSource(crypto.PubkeyToAddress(key.PublicKey))
	return vgMustEnc(m)
}

// VGPrepare returns a PREPARE of key k for block b at (seq, round) with a valid seal.
func VGPrepare(k int, b *types.Block, seq, round uint64) (*messages.Prepare, []byte) {
	seal := VGBLS(k).Sign(PrepareSeal(b.Header(), uint32(round), SealTypePrepare)).Marshal()
	p := messages.NewPrepare(new(big.Int).SetUint64(seq), new(big.Int).SetUint64(round), b.Hash(), seal)
	return p, vgSign(VGKey(k), p)
}

// VGPrepareRaw returns a PREPARE with the given digest and seal (for invalid cases).
func VGPrepareRaw(k int, seq, round uint64, digest common.Hash, seal []byte) (*messages.Prepare, []byte) {
	p := messages.NewPrepare(new(big.Int).SetUint64(seq), new(big.Int).SetUint64(round), digest, seal)
	return p, vgSign(VGKey(k), p)
}

// VGCommit returns a COMMIT of key k for block b at (seq, round) with a valid seal.
func VGCommit(k int, b *types.Block, seq, round uint64) []byte {
	seal := VGBLS(k).Sign(PrepareSeal(b.Header(), uint32(round), SealTypeCommit)).Marshal()
	return vgSign(VGKey(k), messages.NewCommit(new(big.Int).SetUint64(seq), new(big.Int).SetUint64(round), b.Hash(), seal))
}

// VGCommitRaw returns a COMMIT with the given digest and seal.
func VGCommitRaw(k int, seq, round uint64, digest common.Hash, seal []byte) []byte {
	return vgSign(VGKey(k), messages.NewCommit(new(big.Int).SetUint64(seq), new(big.Int).SetUint64(round), digest, seal))
}

// VGRoundChange returns a ROUND-CHANGE of key k; pr/pb is the prepared pair
// (nil for none) and just the PREPARE justification.
func VGRoundChange(k int, seq, round uint64, pr *big.Int, pb *types.Block, just []*messages.Prepare) (*messages.RoundChange, []byte) {
	var prop wbft.Proposal
	if pb != nil {
		prop = pb
	}
	rc := messages.NewRoundChange(new(big.Int).SetUint64(seq), new(big.Int).SetUint64(round), pr, prop)
	rc.Justification = just
	return rc, vgSign(VGKey(k), rc)
}

// VGPreprepare returns a PRE-PREPARE of key k.
func VGPreprepare(k int, seq, round uint64, b *types.Block, rcs []*messages.RoundChange, ps []*messages.Prepare) []byte {
	m := messages.NewPreprepare(new(big.Int).SetUint64(seq), new(big.Int).SetUint64(round), b)
	for _, rc := range rcs {
		m.JustificationRoundChanges = append(m.JustificationRoundChanges, &rc.SignedRoundChangePayload)
	}
	m.JustificationPrepares = ps
	return vgSign(VGKey(k), m)
}

// ------------------------------------------------------------ application

// VGApp is the application side of the core (A-05 §1.3). Its answers come
// from the vector input; Broadcast and Gossip are recorded (or passed to the
// functions set by the network generator).
type VGApp struct {
	key    *ecdsa.PrivateKey
	blsKey bls.SecretKey
	addr   common.Address
	vals   wbft.ValidatorSet
	mux    *event.TypeMux

	mu           sync.Mutex
	head         *types.Block
	headProposer common.Address

	Invalid      map[common.Hash]bool // validate_proposal fails
	Future       map[common.Hash]bool // validate_proposal answers FUTURE until its wait has passed
	Elapsed      map[common.Hash]bool // proposals of Future whose wait has passed (a future_timeout step)
	Bad          map[common.Hash]bool // is_bad_block
	FinalizeFail bool

	// BroadcastFn and GossipFn replace the default recording (network runner).
	BroadcastFn func(vs wbft.ValidatorSet, code uint64, payload []byte) error
	GossipFn    func(vs wbft.ValidatorSet, code uint64, payload []byte) error

	sent      []vgSent
	finalized []VGM
	notified  []any
}

type vgSent struct {
	code    uint64
	payload []byte
}

// NewVGApp creates the application of node key `node` with the validator set
// of the given key indices (in set order) and the round-robin policy.
func NewVGApp(node int, valKeys []int, head *types.Block) *VGApp {
	var addrs []common.Address
	var keys [][]byte
	for _, i := range valKeys {
		addrs = append(addrs, VGAddr(i))
		keys = append(keys, VGBLS(i).PublicKey().Marshal())
	}
	return &VGApp{
		key:          VGKey(node),
		blsKey:       VGBLS(node),
		addr:         VGAddr(node),
		vals:         validator.NewSet(addrs, keys, wbft.NewRoundRobinProposerPolicy()),
		mux:          new(event.TypeMux),
		head:         head,
		headProposer: head.Coinbase(),
		Invalid:      map[common.Hash]bool{},
		Future:       map[common.Hash]bool{},
		Elapsed:      map[common.Hash]bool{},
		Bad:          map[common.Hash]bool{},
	}
}

func (a *VGApp) Address() common.Address                    { return a.addr }
func (a *VGApp) Validators(wbft.Proposal) wbft.ValidatorSet { return a.vals.Copy() }
func (a *VGApp) EventMux() *event.TypeMux                   { return a.mux }
func (a *VGApp) Sign(data []byte) ([]byte, error) {
	return crypto.Sign(crypto.Keccak256(data), a.key)
}
func (a *VGApp) SignWithoutHashing(data []byte) []byte { return a.blsKey.Sign(data).Marshal() }
func (a *VGApp) CheckSignature([]byte, common.Address, []byte) error {
	return errors.New("not used")
}
func (a *VGApp) HasProposal(common.Hash, *big.Int) bool { return false }
func (a *VGApp) GetProposer(uint64) common.Address      { return common.Address{} }
func (a *VGApp) HasBadProposal(h common.Hash) bool      { return a.Bad[h] }
func (a *VGApp) Close() error                           { return nil }
func (a *VGApp) NotifyNewRound(r *big.Int)              { a.notified = append(a.notified, r.String()) }
func (a *VGApp) LastProposal() (wbft.Proposal, common.Address) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.head, a.headProposer
}
func (a *VGApp) SetHead(b *types.Block) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.head, a.headProposer = b, b.Coinbase()
}
func (a *VGApp) Verify(p wbft.Proposal) (time.Duration, error) {
	if a.Invalid[p.Hash()] {
		return 0, errors.New("invalid proposal (vector input)")
	}
	if a.Future[p.Hash()] && !a.Elapsed[p.Hash()] {
		// FUTURE(d): the duration is not part of the vectors (A-11 §3.1
		// "Steps"); the expiry is a future_timeout step
		return time.Second, consensus.ErrFutureBlock
	}
	return 0, nil
}

// Broadcast mirrors backend.Backend.Broadcast: nothing is sent by a node that
// is not in the set (A-05 WBFT-SM-003). The self-delivery is not processed
// here: the case delivers it as a step.
func (a *VGApp) Broadcast(vs wbft.ValidatorSet, code uint64, payload []byte) error {
	if a.BroadcastFn != nil {
		err := a.BroadcastFn(vs, code, payload)
		if err == nil {
			a.sent = append(a.sent, vgSent{code, append([]byte{}, payload...)})
		}
		return err
	}
	if _, v := vs.GetByAddress(a.addr); v == nil {
		return wbft.ErrUnauthorizedAddress
	}
	a.sent = append(a.sent, vgSent{code, append([]byte{}, payload...)})
	return nil
}

func (a *VGApp) Gossip(vs wbft.ValidatorSet, code uint64, payload []byte) error {
	if a.GossipFn != nil {
		return a.GossipFn(vs, code, payload)
	}
	return nil
}

func (a *VGApp) Commit(p wbft.Proposal, prepared, committed []wbft.SealData, round *big.Int) error {
	seals := func(sd []wbft.SealData) []any {
		s := append([]wbft.SealData{}, sd...)
		sort.Slice(s, func(i, j int) bool { return s[i].Sealer < s[j].Sealer })
		out := []any{}
		for _, x := range s {
			out = append(out, VGM{{"sealer", VGU(uint64(x.Sealer))}, {"seal", VGHex(x.Seal)}})
		}
		return out
	}
	res := "ok"
	var err error
	if a.FinalizeFail {
		res, err = "fail", errors.New("finalize fails (vector input)")
	}
	a.finalized = append(a.finalized, VGM{
		{"proposal", VGHex(p.Hash().Bytes())},
		{"round", round.String()},
		{"prepared_seals", seals(prepared)},
		{"committed_seals", seals(committed)},
		{"result", res},
	})
	return err
}

// ------------------------------------------------------------------ driver

type vgSched struct {
	backlog *backlogEvent
	request *wbft.RequestEvent
	enc     []byte // rlp(msg) of a backlog replay
}

// vgTimer is a timer armed during the case (kept for the timer steps).
type vgTimer struct {
	t        *time.Timer // placeholder handed to the core; stopped when the core cancels it
	canceled *bool       // round timer: the flag checked when its expiry is processed
	round    *big.Int    // retry timer: the round it remembers
	f        func()      // future timer: the function it runs
	proposal common.Hash // future timer: the deferred proposal
	expired  bool
}

// VGDriver applies steps to a reference Core without its event loop.
type VGDriver struct {
	App     *VGApp
	C       *Core
	queued  []vgSched // scheduled and not yet processed
	step    []vgSched // scheduled during the current step
	timers  []any     // timers armed during the current step
	armed   map[string][]*vgTimer
	stopped bool // between a Stop step and the next Start step
}

// NewVGDriver creates the core as core.New does and installs the recorders.
func NewVGDriver(app *VGApp) *VGDriver {
	cfg := *wbft.DefaultConfig
	d := &VGDriver{App: app, C: New(app, &cfg), armed: map[string][]*vgTimer{}}
	d.install()
	return d
}

func (d *VGDriver) install() {
	vgTimerRec = func(kind string, seq, round *big.Int, t *time.Timer, canceled *bool, f func(), proposal common.Hash) {
		switch kind {
		case "round", "future":
			d.timers = append(d.timers, VGM{{"kind", kind}, {"sequence", seq.String()}, {"round", round.String()}})
		default:
			d.timers = append(d.timers, VGM{{"kind", "retry"}, {"round", round.String()}})
		}
		d.armed[kind] = append(d.armed[kind], &vgTimer{t: t, canceled: canceled, round: new(big.Int).Set(round), f: f, proposal: proposal})
	}
	vgScheduleRec = func(ev interface{}) {
		switch e := ev.(type) {
		case backlogEvent:
			e2 := e
			d.step = append(d.step, vgSched{backlog: &e2, enc: vgMustEnc(e.msg)})
		case wbft.RequestEvent:
			e2 := e
			d.step = append(d.step, vgSched{request: &e2})
		default:
			panic(fmt.Sprintf("unexpected scheduled event %T", ev))
		}
	}
}

// Uninstall removes the recorders (the hooks fall back to the reference behaviour).
func (d *VGDriver) Uninstall() {
	d.C.stopTimer()
	vgTimerRec, vgScheduleRec = nil, nil
}

func (d *VGDriver) begin() {
	d.App.sent, d.App.finalized, d.App.notified = nil, nil, nil
	d.step, d.timers = nil, nil
}

// Start runs Core.Start without the event loop (INITIAL branch, WBFT-SM-004, -008).
func (d *VGDriver) Start() VGM {
	d.begin()
	d.C.startNewRound(common.Big0)
	return d.record(nil, nil)
}

var vgClass = map[error]string{
	nil:                 "PROCESS",
	errFutureMessage:    "FUTURE",
	errOldMessage:       "OLD",
	errInvalidMessage:   "INVALID",
	errFutureViewTooFar: "TOO_FAR",
	errExtraSealMessage: "EXTRA_SEAL",
}

// VGCheckClass maps a checkMessage result to the A-05 class name.
func VGCheckClass(err error) string {
	s, ok := vgClass[err]
	if !ok {
		panic(fmt.Sprintf("unexpected checkMessage result %v", err))
	}
	return s
}

// Message processes Message(code, payload) as handleEvents does: handler,
// then relay (Gossip) if the handler returned no error.
func (d *VGDriver) Message(code uint64, data []byte) VGM {
	d.running()
	d.begin()
	var class any
	if _, ok := messages.MessageCodes()[code]; ok {
		if m, err := messages.Decode(code, data); err == nil {
			if d.C.verifySignatures(m) == nil {
				v := m.View()
				class = VGCheckClass(d.C.checkMessage(m.Code(), &v))
			}
		}
	}
	relay := false
	if err := d.C.handleEncodedMsg(code, data); err == nil {
		relay = true
		d.App.Gossip(d.C.valSet, code, data)
	}
	return d.record(class, relay)
}

// Backlog processes the scheduled replay whose encoding is enc.
func (d *VGDriver) Backlog(enc []byte) VGM {
	var ev *backlogEvent
	for i, s := range d.queued {
		if s.backlog != nil && bytes.Equal(s.enc, enc) {
			ev = s.backlog
			d.queued = append(d.queued[:i:i], d.queued[i+1:]...)
			break
		}
	}
	if ev == nil {
		panic("backlog step: no such scheduled replay")
	}
	d.running()
	d.begin()
	v := ev.msg.View()
	class := VGCheckClass(d.C.checkMessage(ev.msg.Code(), &v))
	relay := false
	if err := d.C.handleDecodedMessage(ev.msg); err == nil {
		relay = true
		d.App.Gossip(d.C.valSet, ev.msg.Code(), vgMustEnc(ev.msg))
	}
	return d.record(class, relay)
}

// Request processes Request(p) as handleEvents does (a FUTURE request is stored).
func (d *VGDriver) Request(b *types.Block) VGM {
	d.running()
	for i, s := range d.queued { // a replayed request leaves the queue
		if s.request != nil && s.request.Proposal.Hash() == b.Hash() {
			d.queued = append(d.queued[:i:i], d.queued[i+1:]...)
			break
		}
	}
	d.begin()
	r := &Request{Proposal: b}
	if err := d.C.handleRequest(r); err == errFutureMessage {
		d.C.storeRequestMsg(r)
	}
	return d.record(nil, nil)
}

// RoundTimeout processes the expiry of the current (not cancelled) round timer.
func (d *VGDriver) RoundTimeout() VGM {
	d.running()
	if tm := d.vgArmed("round", -1); *tm.canceled {
		panic("round_timeout step for a cancelled timer (give its index instead)")
	}
	d.begin()
	d.C.handleTimeoutMsg()
	return d.record(nil, nil)
}

// RetryTimeout processes RetryTimeout(round).
func (d *VGDriver) RetryTimeout(round uint64) VGM {
	d.running()
	d.begin()
	d.C.broadcastRoundChange(new(big.Int).SetUint64(round))
	return d.record(nil, nil)
}

func (d *VGDriver) running() {
	if d.stopped {
		panic("event step while the engine is stopped")
	}
}

// vgArmed returns timer k of the kind (k < 0: the one armed last).
func (d *VGDriver) vgArmed(kind string, k int) *vgTimer {
	ts := d.armed[kind]
	if k < 0 {
		k = len(ts) - 1
	}
	if k < 0 || k >= len(ts) {
		panic(fmt.Sprintf("no %s timer %d", kind, k))
	}
	tm := ts[k]
	if tm.expired {
		panic(fmt.Sprintf("%s timer %d expires twice", kind, k))
	}
	tm.expired = true
	return tm
}

// RoundTimeoutAt processes the expiry of round timer k as handleEvents does:
// the expiry of a cancelled or superseded timer has no effect (A-06
// WBFT-TIMER-014), and nothing happens while the engine is stopped.
func (d *VGDriver) RoundTimeoutAt(k int) VGM {
	tm := d.vgArmed("round", k)
	d.begin()
	if !d.stopped && !*tm.canceled {
		d.C.handleTimeoutMsg()
	}
	return d.record(nil, nil)
}

// RetryTimeoutAt processes the expiry of retry timer k: a timer that the core
// has stopped never fires; a live one delivers RetryTimeout(its round).
func (d *VGDriver) RetryTimeoutAt(k int) VGM {
	tm := d.vgArmed("retry", k)
	d.begin()
	if tm.t.Stop() && !d.stopped {
		d.C.broadcastRoundChange(tm.round)
	}
	return d.record(nil, nil)
}

// FutureTimeoutAt processes the expiry of future-PRE-PREPARE timer k: from
// now on the application answers VALID for its proposal (its wait has
// passed); if the core has not stopped the timer, the timer's function runs
// and schedules Backlog(m) of the deferred PRE-PREPARE.
func (d *VGDriver) FutureTimeoutAt(k int) VGM {
	tm := d.vgArmed("future", k)
	d.App.Elapsed[tm.proposal] = true
	d.begin()
	if tm.t.Stop() && !d.stopped {
		tm.f()
	}
	return d.record(nil, nil)
}

// Stop runs the part of Core.Stop that the synchronous driver needs: the
// timers are cancelled (stopTimer); without an event loop there is nothing to
// unsubscribe. Scheduled events that no step processed are lost.
func (d *VGDriver) Stop() VGM {
	d.begin()
	d.C.stopTimer()
	d.stopped = true
	d.queued = nil
	return d.record(nil, nil)
}

// Restart creates a new core as Backend.startWBFT does and runs its Start.
func (d *VGDriver) Restart() VGM {
	if !d.stopped {
		panic("start step while the engine runs")
	}
	cfg := *wbft.DefaultConfig
	d.C = New(d.App, &cfg)
	d.stopped = false
	return d.Start()
}

// Head sets the application head; with notify it processes NewHead.
func (d *VGDriver) Head(b *types.Block, notify bool) VGM {
	if d.stopped && notify {
		panic("NewHead while the engine is stopped")
	}
	d.begin()
	d.App.SetHead(b)
	if notify {
		d.C.handleFinalCommittedMsg()
	}
	return d.record(nil, nil)
}

// Idle records a step in which the core processed nothing (a frame that did
// not reach it, network runner).
func (d *VGDriver) Idle() VGM {
	d.begin()
	return d.record(nil, nil)
}

// ------------------------------------------------------------ recording

func vgHashOrNil(p wbft.Proposal) any {
	if p == nil {
		return nil
	}
	if b, ok := p.(*types.Block); ok && b == nil {
		return nil
	}
	return VGHex(p.Hash().Bytes())
}

func vgStateName(s State) string {
	return [...]string{"AcceptRequest", "Preprepared", "Prepared", "Committed"}[s]
}

func vgSortedAddrs(m map[common.Address]bool) []any {
	var out []string
	for a := range m {
		out = append(out, VGHex(a.Bytes()))
	}
	sort.Strings(out)
	return VGList(out)
}

func vgSources(ms *wbftMsgSet) []any {
	set := map[common.Address]bool{}
	for _, m := range ms.Values() {
		set[m.Source()] = true
	}
	return vgSortedAddrs(set)
}

// VGMsgRecord renders a message as the decoded fields of encoding/message_codec
// (A-11 §3.3), with every justification list sorted by the byte value of its
// members' encodings, and `encoded` null when a list has two or more members
// (their order is unspecified, A-05 WBFT-SM-035).
func VGMsgRecord(code uint64, payload []byte) VGM {
	m, err := messages.Decode(code, payload)
	if err != nil {
		panic(err)
	}
	sortedEnc := func(xs []any) ([]any, int) {
		var s []string
		for _, x := range xs {
			s = append(s, VGHex(vgMustEnc(x)))
		}
		sort.Strings(s)
		return VGList(s), len(s)
	}
	multi := false
	var r VGM
	switch x := m.(type) {
	case *messages.Preprepare:
		var rcs, ps []any
		for _, j := range x.JustificationRoundChanges {
			rcs = append(rcs, j)
		}
		for _, j := range x.JustificationPrepares {
			ps = append(ps, j)
		}
		jr, n1 := sortedEnc(rcs)
		jp, n2 := sortedEnc(ps)
		multi = n1 > 1 || n2 > 1
		r = VGM{{"type", "PREPREPARE"}, {"sequence", x.Sequence.String()}, {"round", x.Round.String()},
			{"proposal", VGHex(vgMustEnc(x.Proposal))}, {"signature", VGHex(x.Signature())},
			{"justification_round_changes", jr}, {"justification_prepares", jp}}
	case *messages.Prepare:
		r = VGM{{"type", "PREPARE"}, {"sequence", x.Sequence.String()}, {"round", x.Round.String()},
			{"digest", VGHex(x.Digest.Bytes())}, {"seal", VGHex(x.PrepareSeal)}, {"signature", VGHex(x.Signature())}}
	case *messages.Commit:
		r = VGM{{"type", "COMMIT"}, {"sequence", x.Sequence.String()}, {"round", x.Round.String()},
			{"digest", VGHex(x.Digest.Bytes())}, {"seal", VGHex(x.CommitSeal)}, {"signature", VGHex(x.Signature())}}
	case *messages.RoundChange:
		var js []any
		for _, j := range x.Justification {
			js = append(js, j)
		}
		jj, n := sortedEnc(js)
		multi = n > 1
		var pb any
		if x.PreparedBlock != nil {
			pb = VGHex(vgMustEnc(x.PreparedBlock))
		}
		r = VGM{{"type", "ROUND_CHANGE"}, {"sequence", x.Sequence.String()}, {"round", x.Round.String()},
			{"prepared_round", VGDec(x.PreparedRound)}, {"prepared_digest", VGHex(x.PreparedDigest.Bytes())},
			{"signature", VGHex(x.Signature())}, {"prepared_block", pb}, {"justification", jj}}
	}
	enc := any(VGHex(payload))
	if multi {
		enc = nil
	}
	return append(VGM{{"code", VGU(code)}}, append(r, VGKV{"encoded", enc})...)
}

func (d *VGDriver) snapshot() VGM {
	c := d.C
	cur := c.current
	var pp any
	if cur.Preprepare != nil {
		pp = VGM{{"round", cur.Preprepare.Round.String()}, {"proposal", VGHex(cur.Preprepare.Proposal.Hash().Bytes())}}
	}
	var pend any
	if cur.pendingRequest != nil {
		pend = VGHex(cur.pendingRequest.Proposal.Hash().Bytes())
	}
	var cert any
	if c.WBFTPreparedPrepares != nil {
		ps := append([]*messages.Prepare{}, c.WBFTPreparedPrepares...)
		sort.Slice(ps, func(i, j int) bool { return bytes.Compare(ps[i].Source().Bytes(), ps[j].Source().Bytes()) < 0 })
		l := []any{}
		for _, p := range ps {
			l = append(l, VGM{{"source", VGHex(p.Source().Bytes())}, {"sequence", p.Sequence.String()}, {"round", p.Round.String()}, {"digest", VGHex(p.Digest.Bytes())}})
		}
		cert = l
	}
	var rounds []uint64
	for k := range c.roundChangeSet.roundChanges {
		rounds = append(rounds, k)
	}
	sort.Slice(rounds, func(i, j int) bool { return rounds[i] < rounds[j] })
	rcl := []any{}
	for _, k := range rounds {
		rcl = append(rcl, VGM{{"round", VGU(k)}, {"sources", vgSources(c.roundChangeSet.roundChanges[k])},
			{"prepared_round", VGDec(c.roundChangeSet.highestPreparedRound[k])},
			{"prepared_block", vgHashOrNil(c.roundChangeSet.highestPreparedBlock[k])}})
	}
	type bk struct {
		src string
		k   backlogKey
	}
	var bks []bk
	for src, ks := range c.backlogKeys {
		for k := range ks {
			bks = append(bks, bk{VGHex(src.Bytes()), k})
		}
	}
	sort.Slice(bks, func(i, j int) bool {
		a, b := bks[i], bks[j]
		if a.src != b.src {
			return a.src < b.src
		}
		if a.k.sequence != b.k.sequence {
			return a.k.sequence < b.k.sequence
		}
		if a.k.round != b.k.round {
			return a.k.round < b.k.round
		}
		return a.k.code < b.k.code
	})
	bl := []any{}
	for _, x := range bks {
		bl = append(bl, VGM{{"source", x.src}, {"code", VGU(x.k.code)}, {"sequence", VGU(x.k.sequence)}, {"round", VGU(x.k.round)}})
	}
	extra := func(get func() map[common.Address]messages.WBFTMessage) []any {
		m := get()
		var srcs []common.Address
		for a := range m {
			srcs = append(srcs, a)
		}
		sort.Slice(srcs, func(i, j int) bool { return bytes.Compare(srcs[i].Bytes(), srcs[j].Bytes()) < 0 })
		out := []any{}
		for _, a := range srcs {
			v := m[a].View()
			out = append(out, VGM{{"source", VGHex(a.Bytes())}, {"sequence", v.Sequence.String()}, {"round", v.Round.String()}})
		}
		return out
	}
	eps := extra(func() map[common.Address]messages.WBFTMessage {
		m := map[common.Address]messages.WBFTMessage{}
		for a, x := range c.prepareExtraSeals {
			if x != nil {
				m[a] = x
			}
		}
		return m
	})
	ecs := extra(func() map[common.Address]messages.WBFTMessage {
		m := map[common.Address]messages.WBFTMessage{}
		for a, x := range c.commitExtraSeals {
			if x != nil {
				m[a] = x
			}
		}
		return m
	})
	var proposer any
	if p := c.valSet.GetProposer(); p != nil {
		proposer = VGHex(p.Address().Bytes())
	}
	return VGM{
		{"view", VGM{{"sequence", cur.Sequence().String()}, {"round", cur.Round().String()}}},
		{"state", vgStateName(c.state)},
		{"proposer", proposer},
		{"locked_round", VGDec(cur.preparedRound)},
		{"locked_block", vgHashOrNil(cur.preparedBlock)},
		{"preprepare", pp},
		{"pending_request", pend},
		{"preprepare_sent", cur.preprepareSent.String()},
		{"prepares", vgSources(cur.WBFTPrepares)},
		{"commits", vgSources(cur.WBFTCommits)},
		{"certificate", cert},
		{"round_changes", rcl},
		{"backlog", bl},
		{"prior", VGM{{"round", VGDec(c.priorState.Round())}, {"proposal", vgHashOrNil(c.priorState.Proposal())}}},
		{"extra_prepare_seals", eps},
		{"extra_commit_seals", ecs},
	}
}

func (d *VGDriver) record(check, relay any) VGM {
	sent := []any{}
	for _, s := range d.App.sent {
		sent = append(sent, VGMsgRecord(s.code, s.payload))
	}
	// scheduled events: backlog replays sorted by source (in release order per
	// source), then request replays
	sort.SliceStable(d.step, func(i, j int) bool {
		a, b := d.step[i], d.step[j]
		if (a.backlog == nil) != (b.backlog == nil) {
			return a.backlog != nil
		}
		if a.backlog != nil {
			return bytes.Compare(a.backlog.msg.Source().Bytes(), b.backlog.msg.Source().Bytes()) < 0
		}
		return false
	})
	sched := []any{}
	for _, s := range d.step {
		if s.backlog != nil {
			sched = append(sched, VGM{{"kind", "backlog"}, {"code", VGU(s.backlog.msg.Code())}, {"payload", VGHex(s.enc)}})
		} else {
			sched = append(sched, VGM{{"kind", "request"}, {"block", VGHex(vgMustEnc(s.request.Proposal))}})
		}
	}
	d.queued = append(d.queued, d.step...)
	var fin any
	if len(d.App.finalized) > 1 {
		panic("more than one finalize in a step")
	} else if len(d.App.finalized) == 1 {
		fin = d.App.finalized[0]
	}
	var state any
	if !d.stopped {
		state = d.snapshot()
	}
	timers := d.timers
	if timers == nil {
		timers = []any{}
	}
	notified := d.App.notified
	if notified == nil {
		notified = []any{}
	}
	return VGM{
		{"check", check},
		{"relay", relay},
		{"sent", sent},
		{"timers", timers},
		{"new_round", notified},
		{"finalized", fin},
		{"scheduled", sched},
		{"state", state},
	}
}

// SentPayloads returns the (code, payload) pairs broadcast in the last step,
// each in canonical form (VGCanonical).
func (d *VGDriver) SentPayloads() (codes []uint64, payloads [][]byte) {
	for _, s := range d.App.sent {
		codes = append(codes, s.code)
		payloads = append(payloads, VGCanonical(s.code, s.payload))
	}
	return
}

// VGCanonical re-encodes a message with every justification list sorted by
// the byte value of its members' encodings. The signatures stay valid: the
// lists are not part of any signing payload (A-03). The generator uses this
// form for the self-delivery steps of a node's own messages, whose list order
// is unspecified (A-05 WBFT-SM-035), so that the vectors are deterministic.
func VGCanonical(code uint64, payload []byte) []byte {
	m, err := messages.Decode(code, payload)
	if err != nil {
		panic(err)
	}
	less := func(a, b any) bool { return bytes.Compare(vgMustEnc(a), vgMustEnc(b)) < 0 }
	switch x := m.(type) {
	case *messages.Preprepare:
		sort.SliceStable(x.JustificationRoundChanges, func(i, j int) bool {
			return less(x.JustificationRoundChanges[i], x.JustificationRoundChanges[j])
		})
		sort.SliceStable(x.JustificationPrepares, func(i, j int) bool {
			return less(x.JustificationPrepares[i], x.JustificationPrepares[j])
		})
	case *messages.RoundChange:
		sort.SliceStable(x.Justification, func(i, j int) bool { return less(x.Justification[i], x.Justification[j]) })
	default:
		return payload
	}
	return vgMustEnc(m)
}
