// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Generator test of vectorgen stage 3 (A-11 §3.3, runner state_machine).
// Injected into package core with `go test -overlay` together with
// zz_vectorgen_stage3.go; the reference repository is not modified. It writes
// one JSON object per case into $VECTORGEN_STAGE3_OUT/<handler>.jsonl.
package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/consensus/wbft/messages"
	"github.com/ethereum/go-ethereum/core/types"
)

func vgWrite(t *testing.T, name string, cs []VGCase) {
	dir := os.Getenv("VECTORGEN_STAGE3_OUT")
	f, err := os.Create(filepath.Join(dir, name+".jsonl"))
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

func vgSkip(t *testing.T) {
	if os.Getenv("VECTORGEN_STAGE3_OUT") == "" {
		t.Skip("VECTORGEN_STAGE3_OUT not set")
	}
}

var vgCodeName = map[uint64]string{messages.PreprepareCode: "preprepare", messages.PrepareCode: "prepare", messages.CommitCode: "commit", messages.RoundChangeCode: "round_change"}

// ------------------------------------------------------------ check_message

func TestVectorgenStage3CheckMessage(t *testing.T) {
	vgSkip(t)
	states := []State{StateAcceptRequest, StatePreprepared, StatePrepared, StateCommitted}
	codes := []uint64{messages.PreprepareCode, messages.PrepareCode, messages.CommitCode, messages.RoundChangeCode}
	type view struct{ seq, round *big.Int }
	b := func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }
	two64 := new(big.Int).Lsh(big.NewInt(1), 64)
	type grid struct {
		tag        string
		cur        view
		prior      *big.Int
		views      []view
		desc       string
		onlyStates []State
	}
	grids := []grid{
		{"", view{b(10), b(2)}, b(1), []view{
			{b(8), b(0)}, {b(9), b(0)}, {b(9), b(1)}, {b(9), b(2)}, {b(10), b(0)}, {b(10), b(1)}, {b(10), b(2)},
			{b(10), b(3)}, {b(10), b(12)}, {b(10), b(13)}, {b(11), b(0)}, {b(11), b(9)}, {b(11), b(10)}, {b(12), b(0)},
		}, "node at view (10, 2), prior round 1", nil},
		{"seq1_", view{b(1), b(0)}, b(0), []view{{b(0), b(0)}, {b(0), b(1)}, {b(1), b(0)}, {b(1), b(10)}, {b(1), b(11)}, {b(2), b(0)}},
			"node at view (1, 0) after the genesis head, prior round 0", nil},
		{"big_", view{b(10), b(2)}, b(1), []view{{b(10), new(big.Int).Add(two64, b(2))}, {b(9), new(big.Int).Add(two64, b(1))}},
			"node at view (10, 2), prior round 1; message rounds at or above 2^64 are compared as big integers", []State{StateAcceptRequest, StatePrepared}},
	}
	var cs []VGCase
	for _, g := range grids {
		for _, st := range states {
			if g.onlyStates != nil {
				ok := false
				for _, s := range g.onlyStates {
					ok = ok || s == st
				}
				if !ok {
					continue
				}
			}
			for _, code := range codes {
				for _, v := range g.views {
					c := New(&VGApp{}, wbft.DefaultConfig)
					c.current = newRoundState(&wbft.View{Sequence: g.cur.seq, Round: g.cur.round}, nil, nil, nil, nil, nil, func(common.Hash) bool { return false })
					c.state = st
					c.priorState = priorState{new(sync.RWMutex), g.prior, nil, nil}
					res := VGCheckClass(c.checkMessage(code, &wbft.View{Sequence: v.seq, Round: v.round}))
					rs := v.round.String()
					if v.round.Cmp(two64) >= 0 {
						rs = "2p64_plus_" + new(big.Int).Sub(v.round, two64).String()
					}
					name := fmt.Sprintf("%s%s_%s_%s_%s", g.tag, strings.ToLower(map[State]string{StateAcceptRequest: "accept_request", StatePreprepared: "preprepared", StatePrepared: "prepared", StateCommitted: "committed"}[st]), vgCodeName[code], v.seq, rs)
					cs = append(cs, VGCase{Runner: "state_machine", Handler: "check_message", Name: name, Kind: "pure",
						Desc: fmt.Sprintf("%s, state %s, %s for view (%v, %v): %s (A-05 §5.2)", g.desc, vgStateName(st), strings.ToUpper(vgCodeName[code]), v.seq, v.round, res),
						Reqs: []string{"WBFT-SM-019"},
						Input: VGM{
							{"view", VGM{{"sequence", g.cur.seq.String()}, {"round", g.cur.round.String()}}},
							{"state", vgStateName(st)},
							{"prior_round", g.prior.String()},
							{"code", VGU(code)},
							{"message_view", VGM{{"sequence", v.seq.String()}, {"round", v.round.String()}}},
						},
						Expected: VGM{{"result", res}}})
				}
			}
		}
	}
	vgWrite(t, "check_message", cs)
}

// ------------------------------------------------------------- is_justified

func TestVectorgenStage3IsJustified(t *testing.T) {
	vgSkip(t)
	head := VGBlock(9, common.Hash{}, VGAddr(3), 0)
	B := VGBlock(10, head.Hash(), VGAddr(0), 1)
	B2 := VGBlock(10, head.Hash(), VGAddr(1), 2)
	zero := common.Hash{}
	type rcIn struct {
		src        int
		seq, round uint64
		pr         *big.Int
		pd         common.Hash
	}
	type pIn struct {
		src        int
		seq, round uint64
		digest     common.Hash
	}
	nilRC := func(src int, seq, round uint64) rcIn { return rcIn{src, seq, round, nil, zero} }
	prRC := func(src int, seq, round uint64, pr int64, pd common.Hash) rcIn {
		return rcIn{src, seq, round, big.NewInt(pr), pd}
	}
	type jc struct {
		name, desc string
		prop       *types.Block
		seq, round uint64
		rcs        []rcIn
		ps         []pIn
		q          int
	}
	pB0 := func(srcs ...int) []pIn {
		var out []pIn
		for _, s := range srcs {
			out = append(out, pIn{s, 10, 0, B.Hash()})
		}
		return out
	}
	cases := []jc{
		{"nil_quorum", "three ROUND-CHANGEs for (10, 1) without a prepared pair, no PREPAREs: justified by step 6", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1)}, nil, 3},
		{"nil_four", "four ROUND-CHANGEs without a prepared pair: justified", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1), nilRC(3, 10, 1)}, nil, 3},
		{"too_few_round_changes", "two ROUND-CHANGEs with Q = 3: fails at step 2", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1)}, nil, 3},
		{"duplicate_source_round_change", "three ROUND-CHANGEs, two from the same source: after deduplication (step 1) two remain, fails at step 2", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(0, 10, 1), nilRC(1, 10, 1)}, nil, 3},
		{"duplicate_source_different_content", "four ROUND-CHANGEs, the first two from source 0 (the first is kept by step 1, the second for round 2 is dropped): justified", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(0, 10, 2), nilRC(1, 10, 1), nilRC(2, 10, 1)}, nil, 3},
		{"stale_round", "four ROUND-CHANGEs, one of them for round 0: fails at step 3 (every ROUND-CHANGE must be for the target view)", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1), nilRC(3, 10, 0)}, nil, 3},
		{"stale_sequence", "four ROUND-CHANGEs, one for sequence 9: fails at step 3", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1), nilRC(3, 9, 1)}, nil, 3},
		{"higher_round", "four ROUND-CHANGEs, one for round 2: fails at step 3", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1), nilRC(3, 10, 2)}, nil, 3},
		{"prepares_below_quorum", "three nil ROUND-CHANGEs and two PREPAREs (0 < 2 < Q): fails at step 4", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1)}, pB0(0, 1), 3},
		{"prepares_duplicate_source", "three PREPAREs, two from the same source (step 1 keeps two): fails at step 4", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, B.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, pB0(0, 0, 1), 3},
		{"prepares_mixed_rounds", "three PREPAREs for rounds 0, 0 and 1: fails at step 5", B, 10, 2, []rcIn{prRC(0, 10, 2, 0, B.Hash()), nilRC(1, 10, 2), nilRC(2, 10, 2)}, []pIn{{0, 10, 0, B.Hash()}, {1, 10, 0, B.Hash()}, {2, 10, 1, B.Hash()}}, 3},
		{"prepares_other_digest", "three PREPAREs for another block than the proposal: fails at step 5", B2, 10, 1, []rcIn{prRC(0, 10, 1, 0, B.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, pB0(0, 1, 2), 3},
		{"prepared_valid", "one ROUND-CHANGE prepared (0, B), two nil, three PREPAREs of round 0 for B, proposal B: justified by step 7", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, B.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, pB0(0, 1, 2), 3},
		{"prepared_all_three", "three ROUND-CHANGEs prepared (0, B) and three PREPAREs: justified", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, B.Hash()), prRC(1, 10, 1, 0, B.Hash()), prRC(2, 10, 1, 0, B.Hash())}, pB0(0, 1, 3), 3},
		{"prepared_higher_round_exists", "target (10, 2): ROUND-CHANGEs prepared (0, B), (1, B2) and nil; PREPAREs of round 0 for B, proposal B: only two ROUND-CHANGEs have a prepared round <= 0, fails at step 7", B, 10, 2, []rcIn{prRC(0, 10, 2, 0, B.Hash()), prRC(1, 10, 2, 1, B2.Hash()), nilRC(2, 10, 2)}, pB0(0, 1, 2), 3},
		{"prepared_highest_round", "target (10, 2): ROUND-CHANGEs prepared (0, B), (1, B2) and nil; PREPAREs of round 1 for B2, proposal B2: justified (the highest prepared round wins)", B2, 10, 2, []rcIn{prRC(0, 10, 2, 0, B.Hash()), prRC(1, 10, 2, 1, B2.Hash()), nilRC(2, 10, 2)}, []pIn{{0, 10, 1, B2.Hash()}, {1, 10, 1, B2.Hash()}, {3, 10, 1, B2.Hash()}}, 3},
		{"prepared_no_matching_round_change", "three nil ROUND-CHANGEs and three PREPAREs for B: no ROUND-CHANGE carries (0, B), fails at step 7", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1)}, pB0(0, 1, 2), 3},
		{"prepared_digest_mismatch", "ROUND-CHANGE prepared (0, B2) while the PREPAREs and the proposal are B: no match, fails at step 7", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, B2.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, pB0(0, 1, 2), 3},
		{"prepares_for_other_sequence", "PREPAREs of sequence 9 (the sequence of PREPAREs is not checked, A-05 §11.6 notes): justified", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, B.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, []pIn{{0, 9, 0, B.Hash()}, {1, 9, 0, B.Hash()}, {2, 9, 0, B.Hash()}}, 3},
		{"prepared_round_not_below_target", "PREPAREs and a ROUND-CHANGE for prepared round 5 with target round 1 (prepared_round < target round is not checked): justified", B, 10, 1, []rcIn{prRC(0, 10, 1, 5, B.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, []pIn{{0, 10, 5, B.Hash()}, {1, 10, 5, B.Hash()}, {2, 10, 5, B.Hash()}}, 3},
		{"nil_with_prepared_round_zero", "three ROUND-CHANGEs with prepared_round 0 and the zero digest: counted as not prepared by step 6, justified", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, zero), prRC(1, 10, 1, 0, zero), nilRC(2, 10, 1)}, nil, 3},
		{"nil_with_digest_only", "one ROUND-CHANGE without prepared round but with a non-zero digest: not counted by step 6, two remain, fails", B, 10, 1, []rcIn{{0, 10, 1, nil, B.Hash()}, nilRC(1, 10, 1), nilRC(2, 10, 1)}, nil, 3},
		{"prepared_round_change_without_prepares", "one ROUND-CHANGE prepared (0, B) and two nil, no PREPAREs: step 6 counts two, fails", B, 10, 1, []rcIn{prRC(0, 10, 1, 0, B.Hash()), nilRC(1, 10, 1), nilRC(2, 10, 1)}, nil, 3},
		{"quorum_four", "N = 5 (Q = 4): four nil ROUND-CHANGEs are justified", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1), nilRC(4, 10, 1)}, nil, 4},
		{"quorum_four_three_given", "N = 5 (Q = 4): three nil ROUND-CHANGEs fail at step 2", B, 10, 1, []rcIn{nilRC(0, 10, 1), nilRC(1, 10, 1), nilRC(2, 10, 1)}, nil, 4},
		{"quorum_one", "Q = 1: one nil ROUND-CHANGE is justified", B, 10, 1, []rcIn{nilRC(0, 10, 1)}, nil, 1},
		{"empty", "no ROUND-CHANGE, Q = 1: fails at step 2", B, 10, 1, nil, nil, 1},
	}
	var cs []VGCase
	for _, x := range cases {
		var rcs []*messages.SignedRoundChangePayload
		rcl := []any{}
		for _, r := range x.rcs {
			rc := messages.NewRoundChange(new(big.Int).SetUint64(r.seq), new(big.Int).SetUint64(r.round), r.pr, nil)
			rc.PreparedDigest = r.pd
			rc.SetSource(VGAddr(r.src))
			rcs = append(rcs, &rc.SignedRoundChangePayload)
			rcl = append(rcl, VGM{{"source", VGHex(VGAddr(r.src).Bytes())}, {"sequence", VGU(r.seq)}, {"round", VGU(r.round)}, {"prepared_round", VGDec(r.pr)}, {"prepared_digest", VGHex(r.pd.Bytes())}})
		}
		var ps []*messages.Prepare
		pl := []any{}
		for _, p := range x.ps {
			m := messages.NewPrepare(new(big.Int).SetUint64(p.seq), new(big.Int).SetUint64(p.round), p.digest, nil)
			m.SetSource(VGAddr(p.src))
			ps = append(ps, m)
			pl = append(pl, VGM{{"source", VGHex(VGAddr(p.src).Bytes())}, {"sequence", VGU(p.seq)}, {"round", VGU(p.round)}, {"digest", VGHex(p.digest.Bytes())}})
		}
		err := isJustified(x.prop, wbft.View{Sequence: new(big.Int).SetUint64(x.seq), Round: new(big.Int).SetUint64(x.round)}, rcs, ps, x.q)
		cs = append(cs, VGCase{Runner: "state_machine", Handler: "is_justified", Name: x.name, Kind: "pure",
			Desc: x.desc + fmt.Sprintf(" (reference result: %v)", map[bool]string{true: "justified", false: "not justified: " + fmt.Sprint(err)}[err == nil]),
			Reqs: []string{"WBFT-SM-061"},
			Input: VGM{
				{"proposal", VGHex(x.prop.Hash().Bytes())},
				{"target_view", VGM{{"sequence", VGU(x.seq)}, {"round", VGU(x.round)}}},
				{"round_changes", rcl},
				{"prepares", pl},
				{"quorum", VGU(uint64(x.q))},
			},
			Expected: VGM{{"justified", err == nil}}})
	}
	vgWrite(t, "is_justified", cs)
}

// ------------------------------------------------------------------ rounds

// vgStep builds a steps case while running it.
type vgStep struct {
	d        *VGDriver
	valKeys  []int
	node     int
	head     *types.Block
	inSteps  []any
	outSteps []any
	start    VGM
	lastRec  VGM
}

func newVGStep(node int, valKeys []int, head *types.Block, setup func(*VGApp)) *vgStep {
	app := NewVGApp(node, valKeys, head)
	if setup != nil {
		setup(app)
	}
	s := &vgStep{d: NewVGDriver(app), valKeys: valKeys, node: node, head: head}
	s.start = s.d.Start()
	return s
}

func (s *vgStep) add(in VGM, out VGM) VGM {
	s.inSteps = append(s.inSteps, in)
	s.outSteps = append(s.outSteps, out)
	s.lastRec = out
	return out
}

func (s *vgStep) msg(label string, code uint64, data []byte) VGM {
	return s.add(VGM{{"kind", "message"}, {"label", label}, {"code", VGU(code)}, {"payload", VGHex(data)}}, s.d.Message(code, data))
}

// self delivers every message broadcast in the last step (in send order).
func (s *vgStep) self(label string) {
	codes, pls := s.d.SentPayloads()
	for i := range codes {
		s.msg(label+fmt.Sprintf(" (self-delivery of %s)", strings.ToUpper(vgCodeName[codes[i]])), codes[i], pls[i])
	}
}

// lastSent returns the payload of the first message of the given code broadcast in the last step.
func (s *vgStep) lastSent(code uint64) []byte {
	codes, pls := s.d.SentPayloads()
	for i := range codes {
		if codes[i] == code {
			return pls[i]
		}
	}
	panic("no such message sent in the last step")
}

func (s *vgStep) backlog(label string, code uint64, enc []byte) VGM {
	return s.add(VGM{{"kind", "backlog"}, {"label", label}, {"code", VGU(code)}, {"payload", VGHex(enc)}}, s.d.Backlog(enc))
}

// scheduledBacklog returns the backlog replays scheduled in the last step.
func (s *vgStep) scheduledBacklog() (codes []uint64, encs [][]byte) {
	for _, x := range s.lastRec[6].V.([]any) {
		m := x.(VGM)
		if m[0].V == "backlog" {
			var c uint64
			fmt.Sscan(m[1].V.(string), &c)
			b := common.FromHex(m[2].V.(string))
			codes = append(codes, c)
			encs = append(encs, b)
		}
	}
	return
}

func (s *vgStep) replayAll(label string) {
	codes, encs := s.scheduledBacklog()
	for i := range codes {
		s.backlog(label, codes[i], encs[i])
	}
}

func (s *vgStep) timeout(label string) VGM {
	return s.add(VGM{{"kind", "round_timeout"}, {"label", label}}, s.d.RoundTimeout())
}

func (s *vgStep) retry(label string, round uint64) VGM {
	return s.add(VGM{{"kind", "retry_timeout"}, {"label", label}, {"round", VGU(round)}}, s.d.RetryTimeout(round))
}

func (s *vgStep) newHead(label string, b *types.Block, notify bool) VGM {
	return s.add(VGM{{"kind", "head"}, {"label", label}, {"block", VGHex(VGEncode(b))}, {"notify", notify}}, s.d.Head(b, notify))
}

func (s *vgStep) request(label string, b *types.Block) VGM {
	return s.add(VGM{{"kind", "request"}, {"label", label}, {"block", VGHex(VGEncode(b))}}, s.d.Request(b))
}

// timeoutAt, retryAt and futureAt give the expiry of the timer of that kind
// with index k (A-11 §3.1 "Steps", field `timer`).
func (s *vgStep) timeoutAt(label string, k int) VGM {
	return s.add(VGM{{"kind", "round_timeout"}, {"label", label}, {"timer", VGU(uint64(k))}}, s.d.RoundTimeoutAt(k))
}

func (s *vgStep) retryAt(label string, round uint64, k int) VGM {
	if got := s.d.armed["retry"][k].round.Uint64(); got != round {
		panic(fmt.Sprintf("retry timer %d remembers round %d, not %d", k, got, round))
	}
	return s.add(VGM{{"kind", "retry_timeout"}, {"label", label}, {"round", VGU(round)}, {"timer", VGU(uint64(k))}}, s.d.RetryTimeoutAt(k))
}

func (s *vgStep) futureAt(label string, k int) VGM {
	return s.add(VGM{{"kind", "future_timeout"}, {"label", label}, {"timer", VGU(uint64(k))}}, s.d.FutureTimeoutAt(k))
}

func (s *vgStep) stop(label string) VGM {
	return s.add(VGM{{"kind", "stop"}, {"label", label}}, s.d.Stop())
}

func (s *vgStep) restart(label string) VGM {
	return s.add(VGM{{"kind", "start"}, {"label", label}}, s.d.Restart())
}

// armed returns how many timers of the kind the case has armed so far.
func (s *vgStep) armed(kind string) int { return len(s.d.armed[kind]) }

func (s *vgStep) state() VGM { return s.lastRec[7].V.(VGM) }

func (s *vgStep) finish(t *testing.T, name, desc string, reqs []string) VGCase {
	s.d.Uninstall()
	vals := []any{}
	for _, i := range s.valKeys {
		vals = append(vals, VGM{{"address", VGHex(VGAddr(i).Bytes())}, {"bls_public_key", VGHex(VGBLS(i).PublicKey().Marshal())}})
	}
	hashes := func(m map[common.Hash]bool) []any {
		var out []string
		for h, ok := range m {
			if ok {
				out = append(out, VGHex(h.Bytes()))
			}
		}
		sort.Strings(out)
		return VGList(out)
	}
	fin := "ok"
	if s.d.App.FinalizeFail {
		fin = "fail"
	}
	in := VGM{
		{"initial", VGM{
			{"validators", vals},
			{"proposer_policy", "0"},
			{"node_key", VGHex(VGKey(s.node).D.FillBytes(make([]byte, 32)))},
			{"head", VGHex(VGEncode(s.head))},
			{"app", VGM{{"invalid_proposals", hashes(s.d.App.Invalid)}, {"future_proposals", hashes(s.d.App.Future)}, {"bad_blocks", hashes(s.d.App.Bad)}, {"finalize", fin}}},
		}},
		{"steps", s.inSteps},
	}
	ex := VGM{{"start", s.start}, {"steps", s.outSteps}}
	return VGCase{Runner: "state_machine", Handler: "rounds", Name: name, Kind: "steps", Desc: desc, Reqs: reqs, Input: in, Expected: ex}
}

// Fixture: N = 4 validators with keys 0..3 in this order, head = block 9
// proposed by validator 3, so that the proposer of round r of height 10 is
// validator r mod 4 (round-robin, A-04 §5.1). Q = 3, F = 1.
var vgVals4 = []int{0, 1, 2, 3}

func vgHead9() *types.Block { return VGBlock(9, common.BytesToHash([]byte("block-8")), VGAddr(3), 0) }

type vgBlocks struct{ head, B, OWN, B2, B11 *types.Block }

func vgMkBlocks() vgBlocks {
	h := vgHead9()
	B := VGBlock(10, h.Hash(), VGAddr(0), 1)
	return vgBlocks{h, B, VGBlock(10, h.Hash(), VGAddr(1), 2), VGBlock(10, h.Hash(), VGAddr(1), 3), VGBlock(11, B.Hash(), VGAddr(1), 4)}
}

func must(t *testing.T, cond bool, f string, a ...any) {
	t.Helper()
	if !cond {
		t.Fatalf(f, a...)
	}
}

// prepareQuorum drives node s (not v0) at (10, 0) through PRE-PREPARE(B) from
// v0, its own PREPARE and PREPAREs from `from` until Prepared.
func vgToPrepared(t *testing.T, s *vgStep, bl vgBlocks, from []int) {
	s.msg("PRE-PREPARE(10, 0, B) from v0", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
	s.self("own PREPARE")
	for _, v := range from {
		_, d := VGPrepare(v, bl.B, 10, 0)
		s.msg(fmt.Sprintf("PREPARE(10, 0, B) from v%d", v), messages.PrepareCode, d)
	}
	must(t, s.state()[1].V == "Prepared", "not prepared: %v", s.state()[1].V)
}

func vgToCommitted(t *testing.T, s *vgStep, bl vgBlocks, from []int) {
	vgToPrepared(t, s, bl, from)
	s.self("own COMMIT")
	for _, v := range from {
		s.msg(fmt.Sprintf("COMMIT(10, 0, B) from v%d", v), messages.CommitCode, VGCommit(v, bl.B, 10, 0))
	}
	must(t, s.state()[1].V == "Committed", "not committed: %v", s.state()[1].V)
}

func TestVectorgenStage3Rounds(t *testing.T) {
	vgSkip(t)
	var cs []VGCase
	add := func(c VGCase) { cs = append(cs, c) }
	base := []string{"WBFT-SM-001", "WBFT-SM-014", "WBFT-SM-019", "WBFT-SM-020"}
	reqs := func(r ...string) []string { return append(append([]string{}, base...), r...) }

	// 1. normal height, non-proposer
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		vgToPrepared(t, s, bl, []int{0, 2})
		s.self("own COMMIT")
		_, p3 := VGPrepare(3, bl.B, 10, 0)
		s.msg("PREPARE(10, 0, B) from v3 after the quorum: extra seal", messages.PrepareCode, p3)
		s.msg("COMMIT from v0", messages.CommitCode, VGCommit(0, bl.B, 10, 0))
		s.msg("COMMIT from v2: quorum, decision", messages.CommitCode, VGCommit(2, bl.B, 10, 0))
		s.msg("COMMIT from v3 after the decision: extra seal", messages.CommitCode, VGCommit(3, bl.B, 10, 0))
		s.newHead("block 10 (B) imported, NewHead", bl.B, true)
		_, p3b := VGPrepare(3, bl.B, 10, 0)
		s.msg("the same PREPARE of v3 again in (11, 0): extra seal of the previous sequence, already stored", messages.PrepareCode, p3b)
		add(s.finish(t, "normal_height_non_proposer", "N = 4, node v1 at (10, 0): PRE-PREPARE of v0, PREPARE quorum (own, v0, v2), COMMIT quorum (own, v0, v2), decision in round 0; late PREPARE and COMMIT of v3 are stored as extra seals; NewHead starts (11, 0) (A-14 §4)",
			reqs("WBFT-SM-039", "WBFT-SM-040", "WBFT-SM-042", "WBFT-SM-043", "WBFT-SM-044", "WBFT-SM-045", "WBFT-SM-046", "WBFT-SM-047", "WBFT-SM-048", "WBFT-SM-063", "WBFT-SM-064", "WBFT-SM-065", "WBFT-SM-066", "WBFT-SM-010", "WBFT-SM-011", "WBFT-SM-025", "WBFT-TIMER-010", "WBFT-TIMER-012")))
	}
	// 2. normal height, proposer
	{
		bl := vgMkBlocks()
		s := newVGStep(0, vgVals4, bl.head, nil)
		s.request("the block builder hands over B", bl.B)
		s.self("own PRE-PREPARE")
		s.self("own PREPARE")
		for _, v := range []int{1, 2} {
			_, d := VGPrepare(v, bl.B, 10, 0)
			s.msg(fmt.Sprintf("PREPARE from v%d", v), messages.PrepareCode, d)
		}
		s.self("own COMMIT")
		for _, v := range []int{1, 2} {
			s.msg(fmt.Sprintf("COMMIT from v%d", v), messages.CommitCode, VGCommit(v, bl.B, 10, 0))
		}
		add(s.finish(t, "normal_height_proposer", "N = 4, node v0 is the proposer of (10, 0): the request sends PRE-PREPARE, its self-delivery is accepted, then PREPARE and COMMIT quorums and the decision",
			reqs("WBFT-SM-031", "WBFT-SM-032", "WBFT-SM-034", "WBFT-SM-035", "WBFT-SM-036", "WBFT-SM-039", "WBFT-SM-043", "WBFT-SM-047", "WBFT-TIMER-012")))
	}
	// 3. round change from the PRE-PREPARE stage (A-14 §6): no lock, proposer proposes its own block
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		s.request("the node's own build OWN for height 10 (not proposer of round 0: stored only)", bl.OWN)
		s.msg("PRE-PREPARE(10, 0, B) from v0", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
		s.self("own PREPARE")
		_, p0 := VGPrepare(0, bl.B, 10, 0)
		s.msg("PREPARE from v0 (below quorum)", messages.PrepareCode, p0)
		s.timeout("round timer of (10, 0) expires")
		own := s.lastSent(messages.RoundChangeCode)
		s.msg("own ROUND-CHANGE(10, 1)", messages.RoundChangeCode, own)
		for _, v := range []int{0, 2} {
			_, d := VGRoundChange(v, 10, 1, nil, nil, nil)
			s.msg(fmt.Sprintf("ROUND-CHANGE(10, 1) from v%d", v), messages.RoundChangeCode, d)
		}
		s.self("own PRE-PREPARE(10, 1)")
		s.self("own PREPARE(10, 1)")
		add(s.finish(t, "round_change_from_preprepared", "N = 4, node v1 (proposer of round 1) times out in Preprepared of (10, 0): ROUND-CHANGE(10, 1) without a prepared pair; after a quorum of ROUND-CHANGEs it proposes its own block OWN with the ROUND-CHANGEs as justification and accepts it (A-14 §6.2)",
			reqs("WBFT-SM-005", "@r15", "WBFT-SM-028", "WBFT-SM-029", "WBFT-SM-030", "WBFT-SM-051", "WBFT-SM-052", "WBFT-SM-053", "WBFT-SM-054", "WBFT-SM-055", "WBFT-SM-058", "WBFT-SM-059", "WBFT-SM-062", "@r16", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 4. round change from the PREPARE stage: lock carried, B re-proposed with the certificate
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		s.request("own build OWN", bl.OWN)
		vgToPrepared(t, s, bl, []int{0, 2})
		s.timeout("round timer of (10, 0) expires in Prepared")
		own := s.lastSent(messages.RoundChangeCode)
		s.msg("own ROUND-CHANGE(10, 1) with the lock (0, B) and the certificate", messages.RoundChangeCode, own)
		for _, v := range []int{0, 2} {
			_, d := VGRoundChange(v, 10, 1, nil, nil, nil)
			s.msg(fmt.Sprintf("ROUND-CHANGE(10, 1) from v%d without a lock", v), messages.RoundChangeCode, d)
		}
		s.self("own PRE-PREPARE(10, 1, B)")
		add(s.finish(t, "round_change_from_prepared", "N = 4, node v1 times out in Prepared of (10, 0) (locked on (0, B)): its ROUND-CHANGE(10, 1) carries the lock and the PREPARE certificate; as proposer of round 1 it re-proposes B with the certificate as justification (A-14 §6.2, §7.6)",
			reqs("WBFT-SM-005", "WBFT-SM-043", "WBFT-SM-053", "WBFT-SM-054", "WBFT-SM-055", "WBFT-SM-056", "WBFT-SM-058", "WBFT-SM-059", "WBFT-SM-061", "@r16", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 5. receiver of a justified round-1 PRE-PREPARE with a prepared certificate, while locked on another block
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, nil)
		vgToPrepared(t, s, bl, []int{0, 1})
		s.timeout("round timer of (10, 0) expires")
		// round 1: v1 proposes B2, justified by nil ROUND-CHANGEs of v0, v1, v3
		var rcs []*messages.RoundChange
		for _, v := range []int{0, 1, 3} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			rcs = append(rcs, rc)
		}
		s.msg("justified PRE-PREPARE(10, 1, B2) from v1 while locked on (0, B)", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B2, rcs, nil))
		add(s.finish(t, "locked_node_accepts_justified_other_block", "N = 4, node v2 locked on (0, B) in round 1 accepts a PRE-PREPARE for B2 justified by a quorum of ROUND-CHANGEs without a prepared pair; the lock is not compared (A-14 WBFT-PROTO-002) and it sends PREPARE(10, 1, B2)",
			reqs("WBFT-SM-037", "WBFT-SM-039", "WBFT-SM-061", "WBFT-SM-062", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 6. justified PRE-PREPARE with a prepared certificate, received by a fresh node in round 1
	{
		bl := vgMkBlocks()
		s := newVGStep(3, vgVals4, bl.head, nil)
		s.timeout("round timer of (10, 0) expires (no PRE-PREPARE received)")
		var cert []*messages.Prepare
		for _, v := range []int{0, 1, 2} {
			p, _ := VGPrepare(v, bl.B, 10, 0)
			cert = append(cert, p)
		}
		rc0, _ := VGRoundChange(0, 10, 1, big.NewInt(0), bl.B, cert)
		rc2, _ := VGRoundChange(2, 10, 1, nil, nil, nil)
		rc3, _ := VGRoundChange(3, 10, 1, nil, nil, nil)
		s.msg("PRE-PREPARE(10, 1, B) from v1 justified by ROUND-CHANGEs of v0 (prepared (0, B)), v2, v3 and the three PREPAREs of round 0", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B, []*messages.RoundChange{rc0, rc2, rc3}, cert))
		add(s.finish(t, "justified_preprepare_with_certificate", "N = 4, node v3 in round 1 accepts a PRE-PREPARE that re-proposes B with a prepared certificate (step 7 of is_justified)",
			reqs("WBFT-SM-037", "WBFT-SM-061", "WBFT-SM-062", "@r16", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 7. invalid justifications
	{
		bl := vgMkBlocks()
		s := newVGStep(3, vgVals4, bl.head, nil)
		s.timeout("round timer of (10, 0) expires")
		var rcs2 []*messages.RoundChange
		for _, v := range []int{0, 2} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			rcs2 = append(rcs2, rc)
		}
		s.msg("PRE-PREPARE(10, 1, B) from v1 with two ROUND-CHANGEs (below Q): rejected, not relayed", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B, rcs2, nil))
		var stale []*messages.RoundChange
		for _, v := range []int{0, 2, 3} {
			rc, _ := VGRoundChange(v, 10, 0, nil, nil, nil)
			stale = append(stale, rc)
		}
		s.msg("PRE-PREPARE(10, 1, B) from v1 with three ROUND-CHANGEs for round 0: rejected", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B, stale, nil))
		var ok3 []*messages.RoundChange
		for _, v := range []int{0, 2, 3} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			ok3 = append(ok3, rc)
		}
		s.msg("PRE-PREPARE(10, 1, B) from v2 (not the proposer) with a valid justification: rejected", messages.PreprepareCode, VGPreprepare(2, 10, 1, bl.B, ok3, nil))
		s.msg("PRE-PREPARE(10, 1, B) from v1 with a valid justification: accepted", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B, ok3, nil))
		add(s.finish(t, "preprepare_rejections", "N = 4, node v3 in round 1: PRE-PREPAREs that fail check 3 (justification) or check 1 (not from the proposer) of WBFT-SM-037 are rejected and not relayed; a valid one is accepted",
			reqs("WBFT-SM-013", "WBFT-SM-037", "WBFT-SM-061", "WBFT-SM-062", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 8. F+1 rule
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, nil)
		_, d0 := VGRoundChange(0, 10, 1, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 1) from v0: one sender above round 0 (F = 1), no rule fires", messages.RoundChangeCode, d0)
		_, d3 := VGRoundChange(3, 10, 2, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 2) from v3: two senders above round 0, F+1 rule: the node moves to the smallest such round, 1", messages.RoundChangeCode, d3)
		s.self("own ROUND-CHANGE(10, 1)")
		add(s.finish(t, "f_plus_one", "N = 4 (F = 1), node v2 at (10, 0): the second distinct sender of a ROUND-CHANGE for a higher round triggers start_new_round(min round above) and a ROUND-CHANGE for that round (A-05 §11.4)",
			reqs("WBFT-SM-055", "WBFT-SM-056", "WBFT-SM-057", "WBFT-SM-060", "@r15", "WBFT-SM-051", "WBFT-TIMER-010", "WBFT-TIMER-020")))
	}
	// 9. F+1 rule, N = 5
	{
		h := VGBlock(9, common.BytesToHash([]byte("block-8")), VGAddr(4), 0)
		s := newVGStep(2, []int{0, 1, 2, 3, 4}, h, nil)
		_, d0 := VGRoundChange(0, 10, 3, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 3) from v0 (F = 4/3: one sender does not exceed F)", messages.RoundChangeCode, d0)
		_, d1 := VGRoundChange(1, 10, 3, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 3) from v1: two senders, F < 2 <= F+1, the node moves to round 3", messages.RoundChangeCode, d1)
		_, d3 := VGRoundChange(3, 10, 4, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 4) from v3 in round 3: one sender above round 3, no rule fires", messages.RoundChangeCode, d3)
		add(s.finish(t, "f_plus_one_n5", "N = 5 (F = 4/3, Q = 4), node v2 at (10, 0): the F+1 rule fires at the second sender and moves the node from round 0 directly to round 3",
			reqs("WBFT-SM-057", "WBFT-SM-060", "WBFT-TIMER-010", "WBFT-TIMER-020")))
	}
	// 10. quorum rule without a proposal
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		s.timeout("round timer of (10, 0) expires (no request was handed over)")
		own := s.lastSent(messages.RoundChangeCode)
		s.msg("own ROUND-CHANGE(10, 1)", messages.RoundChangeCode, own)
		_, d0 := VGRoundChange(0, 10, 1, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 1) from v0", messages.RoundChangeCode, d0)
		_, d2 := VGRoundChange(2, 10, 1, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 1) from v2: quorum, but the proposer has no pending request: ERR, not relayed", messages.RoundChangeCode, d2)
		s.request("the block builder hands over OWN in round 1: stored only (round > 0)", bl.OWN)
		_, d3 := VGRoundChange(3, 10, 1, nil, nil, nil)
		s.msg("ROUND-CHANGE(10, 1) from v3: quorum rule again, now proposes OWN", messages.RoundChangeCode, d3)
		add(s.finish(t, "quorum_rule_without_proposal", "N = 4, node v1 is the proposer of round 1 without a pending request: the quorum rule finds no proposal and the ROUND-CHANGE is not relayed (A-05 §16 row 28); after the request arrives the next ROUND-CHANGE triggers the PRE-PREPARE",
			reqs("WBFT-SM-032", "WBFT-SM-058", "WBFT-SM-059", "WBFT-TIMER-010", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 11. late timeout catch-up
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		vgToPrepared(t, s, bl, []int{0, 2})
		s.newHead("block 10 (B) becomes the head, NewHead not yet processed", bl.B, false)
		s.timeout("round timer of (10, 0) expires before NewHead: CATCH_UP branch")
		s.newHead("NewHead for block 10 is processed", bl.B, true)
		s.timeout("round timer of (11, 0) expires")
		add(s.finish(t, "late_timeout_catch_up", "N = 4, node v1 prepared in (10, 0); block 10 is imported but its NewHead is processed after the round timeout: the node enters (11, 0) and sends ROUND-CHANGE(11, 1) without a prepared pair but with the certificate of height 10; the retry timer is armed with round 0; NewHead then changes nothing (A-05 §14.2 case 3)",
			reqs("@r15", "WBFT-SM-022", "WBFT-SM-028", "WBFT-SM-029", "WBFT-SM-051", "@r16", "WBFT-SM-080", "WBFT-TIMER-010", "WBFT-TIMER-011", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-017", "WBFT-TIMER-020", "WBFT-TIMER-041")))
	}
	// 12. bad block unlock
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, func(a *VGApp) { a.Bad[bl.B.Hash()] = true })
		s.request("own build OWN", bl.OWN)
		vgToPrepared(t, s, bl, []int{0, 2})
		s.timeout("round timer of (10, 0) expires; B is a bad block")
		own := s.lastSent(messages.RoundChangeCode)
		s.msg("own ROUND-CHANGE(10, 1): no prepared pair, certificate as justification", messages.RoundChangeCode, own)
		for _, v := range []int{0, 2} {
			_, d := VGRoundChange(v, 10, 1, nil, nil, nil)
			s.msg(fmt.Sprintf("ROUND-CHANGE(10, 1) from v%d", v), messages.RoundChangeCode, d)
		}
		add(s.finish(t, "bad_block_unlock", "N = 4, node v1 locked on (0, B) where the application reports B as a bad block: the round change clears the lock but keeps the certificate, and the node proposes OWN in round 1 (A-05 WBFT-SM-024, A-14 §6.4)",
			reqs("WBFT-SM-024", "WBFT-SM-053", "WBFT-SM-058", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 13. finalize fails
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, func(a *VGApp) { a.FinalizeFail = true })
		vgToCommitted(t, s, bl, []int{0, 2})
		s.retry("retry timer (armed with round 0) expires", 0)
		add(s.finish(t, "committed_finalize_fails", "N = 4, node v1 decides B in (10, 0) but the application cannot finalize it: ROUND-CHANGE(10, 1) without leaving (10, 0) Committed; the retry armed with round 0 sends ROUND-CHANGE(10, 0) (A-05 WBFT-SM-049, WBFT-SM-052, WBFT-SM-077)",
			reqs("WBFT-SM-047", "WBFT-SM-049", "WBFT-SM-050", "WBFT-SM-052", "WBFT-SM-077", "WBFT-TIMER-012", "WBFT-TIMER-020", "WBFT-TIMER-021")))
	}
	// 14. committed, left through F+1
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		vgToCommitted(t, s, bl, []int{0, 2})
		for _, v := range []int{0, 2} {
			_, d := VGRoundChange(v, 10, 1, nil, nil, nil)
			s.msg(fmt.Sprintf("ROUND-CHANGE(10, 1) from v%d while Committed", v), messages.RoundChangeCode, d)
		}
		add(s.finish(t, "committed_left_by_f_plus_one", "N = 4, node v1 decided B in (10, 0) and the block is not imported yet: two ROUND-CHANGEs for round 1 move it to (10, 1) by the F+1 rule, keeping the lock (A-05 WBFT-SM-050)",
			reqs("WBFT-SM-050", "WBFT-SM-057", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-020")))
	}
	// 15. backlog: future messages, replay order, head-of-line
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		_, p2 := VGPrepare(2, bl.B, 10, 0)
		s.msg("PREPARE(10, 0, B) from v2 in AcceptRequest: FUTURE, backlogged", messages.PrepareCode, p2)
		c2 := VGCommit(2, bl.B, 10, 0)
		s.msg("COMMIT(10, 0, B) from v2: FUTURE, backlogged", messages.CommitCode, c2)
		s.msg("the same COMMIT again: key already queued, discarded", messages.CommitCode, c2)
		_, p0n := VGPrepare(0, bl.B11, 11, 0)
		s.msg("PREPARE(11, 0) from v0: FUTURE (next sequence), backlogged", messages.PrepareCode, p0n)
		s.msg("PRE-PREPARE(10, 0, B) from v0: accepted; the backlog of v2 stops at its COMMIT (still FUTURE)", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
		s.self("own PREPARE")
		_, p0 := VGPrepare(0, bl.B, 10, 0)
		s.msg("PREPARE from v0", messages.PrepareCode, p0)
		_, p3 := VGPrepare(3, bl.B, 10, 0)
		s.msg("PREPARE from v3: quorum, Prepared; v2's COMMIT and PREPARE are released", messages.PrepareCode, p3)
		s.replayAll("replay of a backlogged message of v2")
		add(s.finish(t, "backlog_replay", "N = 4, node v1: FUTURE messages are backlogged per source with one slot per (code, sequence, round); after PRE-PREPARE the backlog of v2 is held behind its COMMIT (A-05 WBFT-SM-073 note); after the PREPARE quorum the COMMIT is replayed (PROCESS) and the PREPARE as an extra seal, and both are relayed",
			reqs("WBFT-SM-012", "WBFT-SM-070", "WBFT-SM-072", "WBFT-SM-073", "WBFT-SM-074", "WBFT-TIMER-012")))
	}
	// 16. extra seal without a target block, and PREPARE with a bad seal
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		_, p9 := VGPrepare(2, bl.head, 9, 0)
		s.msg("PREPARE(9, 0) for the head from v2 in (10, 0) AcceptRequest without a prior proposal: extra seal with no target block, stored nothing but relayed", messages.PrepareCode, p9)
		s.msg("PRE-PREPARE(10, 0, B) from v0", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
		_, bad := VGPrepareRaw(2, 10, 0, bl.B.Hash(), VGBLS(2).Sign(PrepareSeal(bl.B.Header(), 1, SealTypePrepare)).Marshal())
		s.msg("PREPARE(10, 0, B) from v2 whose seal is for round 1: rejected, not relayed", messages.PrepareCode, bad)
		_, wrong := VGPrepare(2, bl.B2, 10, 0)
		s.msg("PREPARE(10, 0, B2) from v2: digest differs from the proposal, rejected", messages.PrepareCode, wrong)
		add(s.finish(t, "extra_seal_no_target_and_bad_prepare", "N = 4, node v1: an extra seal without a target block is relayed (A-05 §16 row 8); PREPAREs with a wrong seal round or digest are rejected (row 16)",
			reqs("WBFT-SM-041", "WBFT-SM-063", "WBFT-SM-064", "WBFT-TIMER-012")))
	}
	// 17. ROUND-CHANGE with a prepared block of another sequence
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		var cert []*messages.Prepare
		for _, v := range []int{0, 2, 3} {
			p, _ := VGPrepare(v, bl.B11, 11, 0)
			cert = append(cert, p)
		}
		_, d := VGRoundChange(0, 10, 1, big.NewInt(0), bl.B11, cert)
		s.msg("ROUND-CHANGE(10, 1) from v0 whose prepared block has number 11: rejected, not stored, not relayed", messages.RoundChangeCode, d)
		var cert2 []*messages.Prepare
		for _, v := range []int{0, 2} {
			p, _ := VGPrepare(v, bl.B, 10, 0)
			cert2 = append(cert2, p)
		}
		_, d2 := VGRoundChange(3, 10, 1, big.NewInt(0), bl.B, cert2)
		s.msg("ROUND-CHANGE(10, 1) from v3 prepared (0, B) with only two PREPAREs: stored without the prepared pair", messages.RoundChangeCode, d2)
		add(s.finish(t, "round_change_prepared_checks", "N = 4, node v1: a ROUND-CHANGE whose prepared block is for another sequence is rejected (A-05 §16 row 23); one whose certificate is below quorum is stored but does not set the highest prepared round (WBFT-SM-055)",
			reqs("WBFT-SM-054", "WBFT-SM-055", "WBFT-SM-056")))
	}
	// 18. retry timer and a stale retry after a PRE-PREPARE (SM-081)
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, nil)
		s.timeout("round timer of (10, 0) expires")
		s.retry("retry timer armed with round 1 expires: the same ROUND-CHANGE(10, 1) again", 1)
		var rcs []*messages.RoundChange
		for _, v := range []int{0, 1, 3} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			rcs = append(rcs, rc)
		}
		s.msg("justified PRE-PREPARE(10, 1, B2) from v1", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B2, rcs, nil))
		s.retry("a retry expiry of round 1 queued before the PRE-PREPARE is processed now: sends ROUND-CHANGE(10, 1) and re-arms the retry timer", 1)
		add(s.finish(t, "retry_timeout", "N = 4, node v2: the retry timer repeats the ROUND-CHANGE of the round it was armed with; a stale retry processed after a PRE-PREPARE acceptance still sends and re-arms (A-05 WBFT-SM-077, WBFT-SM-081)",
			reqs("WBFT-SM-052", "WBFT-SM-053", "WBFT-SM-077", "WBFT-SM-081", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-020", "WBFT-TIMER-021")))
	}
	// 19. future request replayed on the new height
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, nil)
		s.request("the block builder hands over block 11 while the node is at (10, 0): FUTURE, stored", bl.B11)
		vgToCommitted(t, s, bl, []int{0, 2})
		s.newHead("block 10 (B) imported, NewHead: the stored request is replayed", bl.B, true)
		s.request("the replayed request for block 11 is processed: v1 is the proposer of (11, 0)", bl.B11)
		add(s.finish(t, "future_request_replayed", "N = 4, node v1: a request for the next height is stored and replayed when the node enters (11, 0), where v1 (proposer, head proposer v0) sends the PRE-PREPARE (A-05 §7.3)",
			reqs("WBFT-SM-031", "WBFT-SM-033", "WBFT-SM-027", "WBFT-SM-032", "WBFT-TIMER-010", "WBFT-TIMER-012")))
	}
	// ---- future proposals (A-05 WBFT-SM-038, A-06 §7, A-09 WBFT-APP-051)
	// 20. deferred PRE-PREPARE, re-processed after its wait
	{
		bl := vgMkBlocks()
		s := newVGStep(1, vgVals4, bl.head, func(a *VGApp) { a.Future[bl.B.Hash()] = true })
		pp := VGPreprepare(0, 10, 0, bl.B, nil, nil)
		s.msg("PRE-PREPARE(10, 0, B) from v0; the application answers FUTURE for B: not accepted, not relayed, future timer armed", messages.PreprepareCode, pp)
		s.futureAt("the future timer expires: B is no longer in the future, Backlog(PRE-PREPARE) is scheduled", 0)
		codes, encs := s.scheduledBacklog()
		must(t, len(codes) == 1, "want one scheduled replay, got %d", len(codes))
		s.backlog("replay of the deferred PRE-PREPARE: accepted, relayed, PREPARE sent", codes[0], encs[0])
		add(s.finish(t, "future_proposal_deferred", "N = 4, node v1 at (10, 0): a PRE-PREPARE whose proposal the application reports as FUTURE is not accepted and not relayed and arms the future timer; when the timer expires the PRE-PREPARE is scheduled again and, on replay, accepted and relayed (A-05 WBFT-SM-038, A-06 WBFT-TIMER-031)",
			reqs("WBFT-SM-012", "WBFT-SM-038", "WBFT-SM-039", "WBFT-SM-074", "WBFT-APP-051", "WBFT-TIMER-012", "WBFT-TIMER-031")))
	}
	// 21. invalid proposal, then two deferred proposals: the second replaces the first
	{
		bl := vgMkBlocks()
		bad := VGBlock(10, bl.head.Hash(), VGAddr(0), 5)
		s := newVGStep(1, vgVals4, bl.head, func(a *VGApp) {
			a.Invalid[bad.Hash()] = true
			a.Future[bl.B.Hash()] = true
			a.Future[bl.B2.Hash()] = true
		})
		s.msg("PRE-PREPARE(10, 0) from v0 whose proposal the application rejects: no PREPARE, no timer, not relayed", messages.PreprepareCode, VGPreprepare(0, 10, 0, bad, nil, nil))
		s.msg("PRE-PREPARE(10, 0, B) from v0, FUTURE: future timer 0", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
		s.msg("PRE-PREPARE(10, 0, B2) from v0, FUTURE: future timer 1 replaces timer 0", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B2, nil, nil))
		s.futureAt("the expiry of future timer 0, which was cancelled: nothing is scheduled", 0)
		s.futureAt("future timer 1 expires: the PRE-PREPARE for B2 is scheduled", 1)
		codes, encs := s.scheduledBacklog()
		must(t, len(codes) == 1, "want one scheduled replay, got %d", len(codes))
		s.backlog("replay of the PRE-PREPARE for B2: accepted", codes[0], encs[0])
		add(s.finish(t, "future_proposal_replaced", "N = 4, node v1 at (10, 0): a PRE-PREPARE whose proposal fails validation causes no action; of two PRE-PREPAREs deferred as FUTURE in the same view only the later one waits, the timer of the first is cancelled and never re-processes it (A-06 WBFT-TIMER-032, A-09 WBFT-APP-051)",
			reqs("WBFT-SM-038", "WBFT-SM-039", "WBFT-APP-051", "WBFT-TIMER-031", "WBFT-TIMER-032")))
	}
	// 22. the round timer cancels the future timer
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, func(a *VGApp) { a.Future[bl.B.Hash()] = true })
		s.msg("PRE-PREPARE(10, 0, B) from v0, FUTURE: future timer armed", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
		s.timeout("round timer of (10, 0) expires: round 1, the new round timer cancels the future timer")
		s.futureAt("the expiry of the cancelled future timer: nothing is scheduled", 0)
		add(s.finish(t, "future_timer_cancelled_by_round_timer", "N = 4, node v2: a PRE-PREPARE deferred as FUTURE in round 0 is dropped when the round timer is re-armed for round 1; its timer never re-processes it (A-05 WBFT-SM-038, A-06 WBFT-TIMER-013, WBFT-TIMER-033)",
			reqs("WBFT-SM-038", "WBFT-TIMER-010", "WBFT-TIMER-013", "WBFT-TIMER-015", "WBFT-TIMER-020", "WBFT-TIMER-033")))
	}
	// ---- timer cancellation and expiry (A-06 §5 to §6)
	// 23. stale round timers and a retry timer cancelled by the round timer
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, nil)
		s.msg("PRE-PREPARE(10, 0, B) from v0: accepted, round timer 1 supersedes round timer 0", messages.PreprepareCode, VGPreprepare(0, 10, 0, bl.B, nil, nil))
		s.timeoutAt("the expiry of round timer 0, which was superseded: no effect", 0)
		s.timeout("round timer 1 expires: round 1 (round timer 2, retry timer 0)")
		var rcs []*messages.RoundChange
		for _, v := range []int{0, 1, 3} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			rcs = append(rcs, rc)
		}
		s.msg("justified PRE-PREPARE(10, 1, B2) from v1: accepted, round timer 3 cancels round timer 2 and retry timer 0", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B2, rcs, nil))
		s.retryAt("the expiry of retry timer 0, which was cancelled: no ROUND-CHANGE", 1, 0)
		s.timeoutAt("the expiry of round timer 2, which was cancelled: no round change", 2)
		add(s.finish(t, "timer_cancellation", "N = 4, node v2: the expiry of a round timer that was cancelled or superseded has no effect, and arming the round timer on a PRE-PREPARE acceptance cancels the retry timer (A-06 WBFT-TIMER-013, WBFT-TIMER-014)",
			reqs("WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-013", "WBFT-TIMER-014", "WBFT-TIMER-015", "WBFT-TIMER-020")))
	}
	// 24. round and retry timers survive the quorums and the decision
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, nil)
		s.timeout("round timer 0 of (10, 0) expires: round 1")
		var rcs []*messages.RoundChange
		for _, v := range []int{0, 1, 3} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			rcs = append(rcs, rc)
		}
		s.msg("justified PRE-PREPARE(10, 1, B2) from v1: accepted (round timer 2)", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B2, rcs, nil))
		s.self("own PREPARE")
		s.retry("a retry expiry of round 1 queued before the PRE-PREPARE: ROUND-CHANGE(10, 1) again, retry timer 1 armed", 1)
		for _, v := range []int{0, 1} {
			_, d := VGPrepare(v, bl.B2, 10, 1)
			s.msg(fmt.Sprintf("PREPARE(10, 1, B2) from v%d", v), messages.PrepareCode, d)
		}
		s.self("own COMMIT")
		for _, v := range []int{0, 1} {
			s.msg(fmt.Sprintf("COMMIT(10, 1, B2) from v%d", v), messages.CommitCode, VGCommit(v, bl.B2, 10, 1))
		}
		must(t, s.state()[1].V == "Committed", "not committed: %v", s.state()[1].V)
		s.retryAt("retry timer 1 expires after the PREPARE and COMMIT quorums: still armed, ROUND-CHANGE(10, 1) again", 1, 1)
		s.timeoutAt("round timer 2 expires after the decision (block 10 not imported yet): still armed, round 2", 2)
		add(s.finish(t, "timers_survive_quorums", "N = 4, node v2 decides B2 in (10, 1): neither the PREPARE quorum nor the COMMIT quorum nor the decision cancels the retry timer or the round timer; both still expire (A-06 WBFT-TIMER-016, WBFT-TIMER-023)",
			reqs("WBFT-SM-081", "WBFT-TIMER-010", "WBFT-TIMER-012", "WBFT-TIMER-015", "WBFT-TIMER-016", "WBFT-TIMER-020", "WBFT-TIMER-021", "WBFT-TIMER-023")))
	}
	// 25. retry expiry after the round advanced
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, nil)
		s.timeout("round timer of (10, 0) expires: round 1, retry timer armed with round 1")
		s.timeout("round timer of (10, 1) expires: round 2, retry timer armed with round 2")
		s.retry("a retry expiry of round 1 queued before the node entered round 2: no ROUND-CHANGE, a new retry timer for round 2", 1)
		add(s.finish(t, "retry_after_round_advanced", "N = 4, node v2 in round 2 handles a retry expiry of round 1: it sends nothing but re-arms the retry timer for its current round (A-06 WBFT-TIMER-022)",
			reqs("WBFT-TIMER-010", "WBFT-TIMER-015", "WBFT-TIMER-020", "WBFT-TIMER-022")))
	}
	// 26. stopping the engine cancels the three timers
	{
		bl := vgMkBlocks()
		s := newVGStep(2, vgVals4, bl.head, func(a *VGApp) { a.Future[bl.B2.Hash()] = true })
		s.timeout("round timer 0 of (10, 0) expires: round 1 (round timer 1, retry timer 0)")
		var rcs []*messages.RoundChange
		for _, v := range []int{0, 1, 3} {
			rc, _ := VGRoundChange(v, 10, 1, nil, nil, nil)
			rcs = append(rcs, rc)
		}
		s.msg("justified PRE-PREPARE(10, 1, B2) from v1, FUTURE: future timer 0", messages.PreprepareCode, VGPreprepare(1, 10, 1, bl.B2, rcs, nil))
		s.stop("the engine stops at (10, 1) with round timer 1, retry timer 0 and future timer 0 armed")
		s.retryAt("the expiry of retry timer 0 while the engine is stopped: nothing is sent", 1, 0)
		s.newHead("block 10 (B) is imported while the engine is stopped", bl.B, false)
		s.restart("the engine starts again: a new core at (11, 0)")
		s.timeoutAt("the expiry of round timer 1 of the previous run: no effect", 1)
		s.futureAt("the expiry of future timer 0 of the previous run: nothing is scheduled", 0)
		add(s.finish(t, "stop_cancels_timers", "N = 4, node v2: stopping the engine cancels the round timer, the retry timer and the future timer; while it is stopped a timer expiry sends nothing, and after a restart at the next height the expiry of a timer of the previous run has no effect (A-06 WBFT-TIMER-018)",
			reqs("WBFT-SM-004", "WBFT-SM-038", "WBFT-TIMER-010", "WBFT-TIMER-015", "WBFT-TIMER-018", "WBFT-TIMER-020")))
	}
	vgWrite(t, "rounds", cs)
}
