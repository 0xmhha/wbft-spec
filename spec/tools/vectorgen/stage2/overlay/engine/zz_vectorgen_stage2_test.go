// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Generator test of vectorgen stage 2 (A-11 §3.3). Injected into package
// wbftengine with `go test -overlay` by tools/vectorgen/stage2; the reference
// repository is not modified. It runs the unexported computeShuffledIndex,
// sortCandidates and buildEpochInfo and writes one JSON object per case into
// $VECTORGEN_STAGE2_OUT/<handler>.jsonl. Integers are decimal strings and
// byte strings are 0x hex (A-11 WBFT-VEC-014).
package wbftengine

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/systemcontracts"
)

// ------------------------------------------------------------ output

type vgKV struct {
	k string
	v any
}

// vgM is a JSON object that keeps its key order.
type vgM []vgKV

func (m vgM) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.k)
		v, err := json.Marshal(kv.v)
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

type vgCase struct {
	Runner, Handler, Name, Kind, Desc, Err string
	Reqs                                   []string
	Input                                  vgM
	Expected                               vgM
}

func vgD(v uint64) string      { return strconv.FormatUint(v, 10) }
func vgB(v *big.Int) string    { return v.String() }
func vgH(b []byte) string      { return "0x" + hex.EncodeToString(b) }
func vgA(a common.Address) any { return vgH(a.Bytes()) }

func vgWrite(t *testing.T, handler string, cs []vgCase) {
	dir := os.Getenv("VECTORGEN_STAGE2_OUT")
	f, err := os.Create(filepath.Join(dir, handler+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, c := range cs {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
}

func vgMust(err error) {
	if err != nil {
		panic(err)
	}
}

// ------------------------------------------------------------ entry

func TestVectorgenStage2Engine(t *testing.T) {
	if os.Getenv("VECTORGEN_STAGE2_OUT") == "" {
		t.Skip("VECTORGEN_STAGE2_OUT not set")
	}
	vgWrite(t, "shuffle", vgShuffleCases())
	vgWrite(t, "sort_candidates", vgSortCases())
	vgWrite(t, "next_epoch_info", vgNextEpochCases(t))
}

// ------------------------------------------------------------ shuffle

func vgShuffleCases() []vgCase {
	var sA04, sVec, zero [32]byte
	copy(sA04[:], crypto.Keccak256([]byte("wbft-spec-a04")))
	copy(sVec[:], crypto.Keccak256([]byte("wbft-vector-shuffle")))
	all := func(n uint64) []uint64 {
		out := make([]uint64, n)
		for i := range out {
			out[i] = uint64(i)
		}
		return out
	}
	type in struct {
		name, desc string
		seed       [32]byte
		count      uint64
		idx        []uint64
	}
	ins := []in{}
	for _, n := range []uint64{1, 2, 3, 4, 5, 8} {
		ins = append(ins, in{fmt.Sprintf("a04_seed_count_%d", n), "seed keccak256(\"wbft-spec-a04\") of A-04 §6.8, every index", sA04, n, all(n)})
	}
	ins = append(ins,
		in{"zero_seed_count_4", "all-zero seed, count 4 (A-04 §6.8)", zero, 4, all(4)},
		in{"count_33", "seed keccak256(\"wbft-vector-shuffle\"), count 33, every index", sVec, 33, all(33)},
		in{"count_100", "count 100, every index", sVec, 100, all(100)},
		in{"count_1000_window_edges", "count 1000: indices around the 256-position windows of the source hash (position >> 8)", sVec, 1000, []uint64{0, 1, 255, 256, 257, 511, 512, 998, 999}},
		in{"count_2_pow_45", "count 2^45: position >> 8 exceeds 2^32 and only its low 32 bits enter the source hash (uint32_le)", sVec, 1 << 45, []uint64{0, 1, 1 << 44, 1<<44 + 12345, 1<<45 - 1}},
	)
	var cs []vgCase
	for _, x := range ins {
		out := []any{}
		idx := []any{}
		for _, i := range x.idx {
			j, err := computeShuffledIndex(i, x.count, x.seed, true)
			vgMust(err)
			out = append(out, vgD(j))
			idx = append(idx, vgD(i))
		}
		cs = append(cs, vgCase{Runner: "validators", Handler: "shuffle", Name: x.name, Kind: "pure", Desc: x.desc,
			Reqs:     []string{"WBFT-EPOCH-018"},
			Input:    vgM{{"seed", vgH(x.seed[:])}, {"count", vgD(x.count)}, {"indices", idx}},
			Expected: vgM{{"shuffled", out}}})
	}
	for _, f := range []struct {
		name  string
		index uint64
		count uint64
	}{{"fail_index_equals_count", 4, 4}, {"fail_index_above_count", 9, 4}, {"fail_count_zero", 0, 0}} {
		_, err := computeShuffledIndex(f.index, f.count, sA04, true)
		if err == nil {
			panic("expected error")
		}
		cs = append(cs, vgCase{Runner: "validators", Handler: "shuffle", Name: f.name, Kind: "pure",
			Desc: fmt.Sprintf("index %d with count %d is out of bounds", f.index, f.count), Reqs: []string{"WBFT-EPOCH-018"},
			Input: vgM{{"seed", vgH(sA04[:])}, {"count", vgD(f.count)}, {"indices", []any{vgD(f.index)}}}, Err: err.Error()})
	}
	return cs
}

// ------------------------------------------------------ sort_candidates

func vgSortCases() []vgCase {
	type in struct {
		name, desc string
		d          []uint64
	}
	rep := func(n int, v uint64) []uint64 {
		out := make([]uint64, n)
		for i := range out {
			out[i] = v
		}
		return out
	}
	stream := func(tag string, n int, vals []uint64) []uint64 {
		out := make([]uint64, n)
		h := crypto.Keccak256([]byte(tag))
		for i := range out {
			if i > 0 && i%32 == 0 {
				h = crypto.Keccak256(h)
			}
			out[i] = vals[int(h[i%32])%len(vals)]
		}
		return out
	}
	two := []uint64{1_800_000, 1_900_000}
	ins := []in{
		{"empty", "no candidates", nil},
		{"one", "one candidate", []uint64{1_900_000}},
		{"two_distinct", "two candidates, the second with higher diligence", []uint64{1_800_000, 1_900_000}},
		{"two_equal", "two equal candidates keep their order", []uint64{1_900_000, 1_900_000}},
		{"five_mixed", "five candidates of A-04 §6.8 epoch 1 (v0..v4)", []uint64{1_888_750, 1_832_500, 1_898_125, 1_870_000, 1_900_000}},
		{"twelve_equal", "12 equal values: identity order", rep(12, 1_900_000)},
		{"twelve_ties", "12 values with ties: ties keep input order", stream("wbft-vector-sort-12", 12, two)},
		{"thirteen_equal", "13 equal values: identity order", rep(13, 1_900_000)},
		{"twenty_equal", "20 equal values", rep(20, 1_900_000)},
	}
	var cs []vgCase
	for _, x := range ins {
		c := make([]PoweredCandidate, len(x.d))
		dl := []any{}
		for i := range c {
			c[i] = PoweredCandidate{Addr: common.BigToAddress(big.NewInt(int64(0x1000 + i))), Power: big.NewInt(1), Diligence: x.d[i]}
			dl = append(dl, vgD(x.d[i]))
		}
		got := sortCandidates(c)
		stable := make([]int, len(x.d))
		for i := range stable {
			stable[i] = i
		}
		sort.SliceStable(stable, func(a, b int) bool { return x.d[stable[a]] > x.d[stable[b]] })
		ord := []any{}
		tieOrder := false
		for i, v := range got {
			ord = append(ord, vgD(uint64(v)))
			if v != stable[i] {
				tieOrder = true
			}
		}
		desc := x.desc
		reqs := []string{"WBFT-EPOCH-016"}
		if len(x.d) > 12 {
			reqs = append(reqs, "@r10")
		}
		if tieOrder {
			// the alias omits the case from an edition that leaves out the tie-order requirement
			desc += "; tie-order case ({@r10})"
		} else {
			desc += "; the order equals a stable sort"
		}
		cs = append(cs, vgCase{Runner: "validators", Handler: "sort_candidates", Name: x.name, Kind: "pure", Desc: desc, Reqs: reqs,
			Input: vgM{{"diligences", dl}}, Expected: vgM{{"order", ord}}})
	}
	return cs
}

// ---------------------------------------------------- next_epoch_info

type vgAcct struct {
	key  *ecdsa.PrivateKey
	addr common.Address
	bls  bls.SecretKey
}

// Keys of A-11 WBFT-VEC-021.
var vgAccts = func() []vgAcct {
	var out []vgAcct
	for i := 0; i < 16; i++ {
		k, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("wbft-spec-vector-key-%d", i))))
		vgMust(err)
		b, err := bls.DeriveFromECDSA(k)
		vgMust(err)
		out = append(out, vgAcct{k, crypto.PubkeyToAddress(k.PublicKey), b})
	}
	return out
}()

type vgChain struct {
	cc     *params.ChainConfig
	byHash map[common.Hash]*types.Header
	byNum  map[uint64]*types.Header
	list   []*types.Header
}

func (c *vgChain) Config() *params.ChainConfig  { return c.cc }
func (c *vgChain) CurrentHeader() *types.Header { return c.list[len(c.list)-1] }
func (c *vgChain) GetHeader(h common.Hash, n uint64) *types.Header {
	x := c.byHash[h]
	if x == nil || x.Number.Uint64() != n {
		return nil
	}
	return x
}
func (c *vgChain) GetHeaderByNumber(n uint64) *types.Header    { return c.byNum[n] }
func (c *vgChain) GetHeaderByHash(h common.Hash) *types.Header { return c.byHash[h] }
func (c *vgChain) GetTd(common.Hash, uint64) *big.Int          { return nil }
func (c *vgChain) StateAt(common.Hash) (*state.StateDB, error) {
	return nil, errors.New("no state")
}
func (c *vgChain) add(h *types.Header) {
	c.byHash[h.Hash()] = h
	c.byNum[h.Number.Uint64()] = h
	c.list = append(c.list, h)
}

// vgState is a systemcontracts.StateReader over storage slots.
type vgState map[common.Address]map[common.Hash]common.Hash

func (s vgState) GetState(a common.Address, k common.Hash) common.Hash { return s[a][k] }

type vgTrans struct {
	block, epoch uint64
	policy       *uint64
}

type vgScenario struct {
	epoch   uint64
	policy  uint64
	trans   []vgTrans
	init    []int
	last    uint64 // build blocks 1..last
	rounds  map[uint64]uint32
	seals   map[uint64][2][]int // prev prepared / committed sealers carried by block n (indices into V(n-1)); nil: all
	noSeals map[uint64]bool     // block n carries no previous seals
	coin    map[uint64]int      // coinbase override: account index
	cands   func(n uint64) ([]int, map[int]bool)
	crafted map[uint64]*types.EpochInfo // EpochInfo written at an epoch block instead of the computed one
}

func (s vgScenario) chainConfig() *params.ChainConfig {
	cc := *params.StableNetMainnetChainConfig
	an := *cc.Anzeon
	pol := s.policy
	an.WBFT = &params.WBFTConfig{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: s.epoch, ProposerPolicy: &pol}
	init := &params.WBFTInit{}
	for _, i := range s.init {
		init.Validators = append(init.Validators, vgAccts[i].addr)
		init.BLSPublicKeys = append(init.BLSPublicKeys, hexutil.Encode(vgAccts[i].bls.PublicKey().Marshal()))
	}
	an.Init = init
	sc := *an.SystemContracts
	gv := *sc.GovValidator
	gv.Params = map[string]string{}
	for k, v := range an.SystemContracts.GovValidator.Params {
		gv.Params[k] = v
	}
	sc.GovValidator = &gv
	an.SystemContracts = &sc
	cc.Anzeon = &an
	cc.Transitions = nil
	for _, t := range s.trans {
		cc.Transitions = append(cc.Transitions, params.Transition{Block: new(big.Int).SetUint64(t.block), WBFTConfig: &params.WBFTConfig{EpochLength: t.epoch, ProposerPolicy: t.policy}})
	}
	return &cc
}

func (s vgScenario) configYAML() vgM {
	vals, keys := []any{}, []any{}
	for _, i := range s.init {
		vals = append(vals, vgA(vgAccts[i].addr))
		keys = append(keys, vgH(vgAccts[i].bls.PublicKey().Marshal()))
	}
	wb := func(epoch uint64, pol *uint64, rt, bp uint64) vgM {
		var p any
		if pol != nil {
			p = vgD(*pol)
		}
		return vgM{{"request_timeout_seconds", vgD(rt)}, {"block_period_seconds", vgD(bp)}, {"epoch_length", vgD(epoch)},
			{"allowed_future_block_time", "0"}, {"proposer_policy", p}, {"max_request_timeout_seconds", nil}}
	}
	ts := []any{}
	for _, t := range s.trans {
		m := vgM{{"block", vgD(t.block)}}
		m = append(m, wb(t.epoch, t.policy, 0, 0)...)
		ts = append(ts, m)
	}
	pol := s.policy
	return vgM{
		{"preset", "8282"},
		{"init", vgM{{"validators", vals}, {"bls_public_keys", keys}}},
		{"wbft", wb(s.epoch, &pol, 2, 1)},
		{"transitions", ts},
		{"shanghai_time", nil},
		{"cancun_time", nil},
	}
}

// state returns GovValidator storage initialised by the reference from
// govValidator params with the given candidate accounts; the BLS key slot of
// the accounts in `noKey` is cleared afterwards (as A-04 §6.8 does).
func (s vgScenario) state(cc *params.ChainConfig, cands []int, noKey map[int]bool) vgState {
	var vals, keys []string
	for _, i := range cands {
		vals = append(vals, vgAccts[i].addr.Hex())
		keys = append(keys, hexutil.Encode(vgAccts[i].bls.PublicKey().Marshal()))
	}
	gvp := cc.Anzeon.SystemContracts.GovValidator.Params
	gvp[systemcontracts.GOV_BASE_PARAM_MEMBERS] = strings.Join(vals, ",")
	gvp[systemcontracts.GOV_BASE_PARAM_QUORUM] = strconv.Itoa(len(cands))
	gvp[systemcontracts.GOV_VALIDATOR_PARAM_VALIDATORS] = strings.Join(vals, ",")
	gvp[systemcontracts.GOV_VALIDATOR_PARAM_BLS_KEYS] = strings.Join(keys, ",")
	cfg := new(wbft.Config)
	vgMust(wbft.SetConfigFromChainConfig(cfg, cc))
	st, err := wbft.GetSystemContractsStateTransition(cfg, big.NewInt(0))
	vgMust(err)
	out := vgState{}
	for _, p := range st.States {
		if out[p.Address] == nil {
			out[p.Address] = map[common.Hash]common.Hash{}
		}
		out[p.Address][p.Key] = p.Value
	}
	gv := cc.Anzeon.SystemContracts.GovValidator.Address
	for i := range noKey {
		out[gv][systemcontracts.CalculateMappingSlot(common.HexToHash(systemcontracts.SLOT_VALIDATOR_validatorToBlsKey), vgAccts[i].addr)] = common.Hash{}
	}
	return out
}

var vgPlaceholderSig = append([]byte{0xc0}, make([]byte, 95)...)

func vgBitmap(idx []int) *types.WBFTAggregatedSeal {
	var s types.SealerSet
	for _, i := range idx {
		s.SetSealer(uint32(i))
	}
	return &types.WBFTAggregatedSeal{Sealers: s, Signature: append([]byte{}, vgPlaceholderSig...)}
}

type vgBuilt struct {
	cc      *params.ChainConfig
	e       *Engine
	ch      *vgChain
	headers map[uint64]*types.Header // every built header, epoch blocks with their EpochInfo
	states  map[uint64]vgState
}

// build creates blocks 1..s.last. The epoch block `target` is built without
// EpochInfo and not stored.
func (s vgScenario) build(target uint64) *vgBuilt {
	cc := s.chainConfig()
	cfg := new(wbft.Config)
	vgMust(wbft.SetConfigFromChainConfig(cfg, cc))
	e := NewEngine(cfg, common.Address{}, nil, nil)
	extra, err := wbft.CreateInitialExtraData(cc.Anzeon)
	vgMust(err)
	g := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1), UncleHash: types.EmptyUncleHash, TxHash: types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash, GasLimit: 105_000_000, BaseFee: big.NewInt(20_000_000_000_000), Time: 1_700_000_000, Extra: extra}
	ch := &vgChain{cc: cc, byHash: map[common.Hash]*types.Header{}, byNum: map[uint64]*types.Header{}}
	ch.add(g)
	b := &vgBuilt{cc: cc, e: e, ch: ch, headers: map[uint64]*types.Header{0: g}, states: map[uint64]vgState{}}
	parent := g
	var last common.Address
	for n := uint64(1); n <= target; n++ {
		num := new(big.Int).SetUint64(n)
		vs, err := e.GetValidators(ch, num, parent.Hash(), nil)
		vgMust(err)
		vs.CalcProposer(last, uint64(s.rounds[n]))
		coinbase := vs.GetProposer().Address()
		if k, ok := s.coin[n]; ok {
			coinbase = vgAccts[k].addr
		}
		x := &types.WBFTExtra{VanityData: make([]byte, 32), RandaoReveal: []byte{}, Round: s.rounds[n], GasTip: new(big.Int).SetUint64(params.InitialGasTip)}
		if n >= 2 && !s.noSeals[n] {
			pvs, err := e.GetValidators(ch, parent.Number, parent.ParentHash, nil)
			vgMust(err)
			all := make([]int, pvs.Size())
			for i := range all {
				all[i] = i
			}
			pp, pc := all, all
			if sl, ok := s.seals[n]; ok {
				pp, pc = sl[0], sl[1]
			}
			x.PrevRound = s.rounds[n-1]
			x.PrevPreparedSeal, x.PrevCommittedSeal = vgBitmap(pp), vgBitmap(pc)
		}
		h := &types.Header{ParentHash: parent.Hash(), UncleHash: types.EmptyUncleHash, Coinbase: coinbase, TxHash: types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(1), Number: num, GasLimit: 105_000_000, Time: parent.Time + 1,
			MixDigest: crypto.Keccak256Hash(num.Bytes()), BaseFee: big.NewInt(20_000_000_000_000)}
		h.Extra, err = rlp.EncodeToBytes(x)
		vgMust(err)
		cands, noKey := s.cands(n)
		st := s.state(cc, cands, noKey)
		b.states[n] = st
		if n == target {
			b.headers[n] = h
			break
		}
		if is, _, _ := e.IsEpochBlockNumber(cc, num); is {
			ei := s.crafted[n]
			if ei == nil {
				ei, err = e.buildEpochInfo(ch, h, st)
				vgMust(err)
			}
			_, err = ApplyHeaderWBFTExtra(h, WriteEpochInfo(ei))
			vgMust(err)
		}
		ch.add(h)
		b.headers[n] = h
		parent = h
		last = coinbase
	}
	return b
}

func vgEpochInfoYAML(ei *types.EpochInfo) any {
	if ei == nil {
		return nil
	}
	cands, vals, keys := []any{}, []any{}, []any{}
	for _, c := range ei.Candidates {
		cands = append(cands, vgM{{"address", vgA(c.Addr)}, {"diligence", vgD(c.Diligence)}})
	}
	for _, v := range ei.Validators {
		vals = append(vals, vgD(uint64(v)))
	}
	for _, k := range ei.BLSPublicKeys {
		keys = append(keys, vgH(k))
	}
	return vgM{{"candidates", cands}, {"validators", vals}, {"bls_public_keys", keys}}
}

func vgNextEpochCases(t *testing.T) []vgCase {
	five := func(n uint64) ([]int, map[int]bool) {
		if n < 4 {
			return []int{0, 1, 2, 3}, nil
		}
		if n >= 12 {
			return []int{0, 1, 2, 3, 4}, map[int]bool{2: true}
		}
		return []int{0, 1, 2, 3, 4}, nil
	}
	a04Seals := map[uint64][2][]int{
		2:  {{0, 1, 2, 3}, {0, 1, 2}},
		3:  {{0, 1, 2, 3}, {0, 1, 2, 3}},
		4:  {{0, 1, 2}, {0, 1, 2}},
		5:  {{0, 1, 2, 3}, {0, 1, 2, 3}},
		6:  {{0, 1, 2, 3}, {0, 1, 2, 3}},
		7:  {{0, 1, 2, 3}, {0, 1, 2, 3}},
		8:  {{0, 1, 2, 3}, {0, 1, 2, 3}},
		9:  {{0, 1, 2, 3}, {0, 1, 2, 3}},
		10: {{0, 1, 2, 3, 4}, {0, 1, 2, 3, 4}},
		11: {{0, 1, 2, 3, 4}, {0, 1, 2, 3, 4}},
		12: {{0, 1, 2, 3, 4}, {0, 1, 2, 3}},
	}
	a04 := vgScenario{epoch: 4, policy: 0, init: []int{0, 1, 2, 3}, rounds: map[uint64]uint32{2: 1, 11: 2}, seals: a04Seals, cands: five}
	sticky := a04
	sticky.policy = 1
	fourteen := vgScenario{epoch: 4, policy: 0, init: []int{0, 1, 2, 3}, rounds: map[uint64]uint32{}, seals: map[uint64][2][]int{},
		cands: func(n uint64) ([]int, map[int]bool) {
			if n < 4 {
				return []int{0, 1, 2, 3}, nil
			}
			out := []int{}
			for i := 0; i < 14; i++ {
				out = append(out, i)
			}
			return out, nil
		}}
	reanchor := vgScenario{epoch: 4, policy: 0, init: []int{0, 1, 2, 3}, trans: []vgTrans{{block: 6, epoch: 3}}, rounds: map[uint64]uint32{}, seals: map[uint64][2][]int{}, cands: five}
	dropped := vgScenario{epoch: 4, policy: 0, init: []int{0, 1, 2, 3}, rounds: map[uint64]uint32{}, seals: map[uint64][2][]int{},
		cands: func(n uint64) ([]int, map[int]bool) {
			if n < 8 {
				return []int{0, 1, 2, 3, 4}, nil
			}
			return []int{4, 3, 0, 2, 5}, nil // v1 dropped, v5 new, order changed
		}}
	emptySeals := vgScenario{epoch: 4, policy: 0, init: []int{0, 1, 2, 3}, rounds: map[uint64]uint32{}, seals: map[uint64][2][]int{}, noSeals: map[uint64]bool{3: true}, cands: five}
	zeroAddr := &types.EpochInfo{}
	for i := 0; i < 5; i++ {
		zeroAddr.Candidates = append(zeroAddr.Candidates, &types.Candidate{Addr: vgAccts[i].addr, Diligence: types.DefaultDiligence})
	}
	for _, v := range []int{0, 1, 2, 7} {
		zeroAddr.Validators = append(zeroAddr.Validators, uint32(v))
		k := 3
		if v < 5 {
			k = v
		}
		zeroAddr.BLSPublicKeys = append(zeroAddr.BLSPublicKeys, vgAccts[k].bls.PublicKey().Marshal())
	}
	zero := vgScenario{epoch: 4, policy: 0, init: []int{0, 1, 2, 3}, rounds: map[uint64]uint32{}, seals: map[uint64][2][]int{}, cands: five,
		crafted: map[uint64]*types.EpochInfo{4: zeroAddr}, coin: map[uint64]int{5: 0, 6: 1, 7: 2, 8: 0}}

	type in struct {
		name, desc string
		reqs       []string
		s          vgScenario
		e          uint64
	}
	acc := []string{"WBFT-EPOCH-008", "WBFT-EPOCH-009", "WBFT-EPOCH-010", "WBFT-EPOCH-011", "@r08", "WBFT-EPOCH-013", "WBFT-EPOCH-016", "WBFT-EPOCH-018", "WBFT-EPOCH-019", "WBFT-EPOCH-020"}
	ins := []in{
		{"a04_epoch_1", "A-04 §6.8 epoch 1 (e = 4) with the keys of WBFT-VEC-021: L = 0, every validator is new (rate E - 1), v4 enters with the default diligence", acc, a04, 4},
		{"a04_epoch_2", "A-04 §6.8 epoch 2 (e = 8): the seals carried by block 5 are resolved with the genesis EpochInfo; max_p correction for the first proposer", acc, a04, 8},
		{"a04_epoch_3", "A-04 §6.8 epoch 3 (e = 12): the BLS key of v2 is cleared, v2 stays a candidate but is not a validator", acc, a04, 12},
		{"sticky_epoch_1", "the plan of A-04 §6.8 with policy Sticky: the proposer replay uses config_at(e).proposer_policy", acc, sticky, 4},
		{"sticky_epoch_2", "the plan of A-04 §6.8 with policy Sticky, e = 8", acc, sticky, 8},
		{"fourteen_candidates_epoch_1", "14 candidates at e = 4 (10 new with the default diligence 1900000): sort_candidates with 14 entries and ties", append(acc, "@r10"), fourteen, 4},
		{"fourteen_candidates_epoch_2", "14 validators sealing epoch 2, e = 8", append(acc, "@r10"), fourteen, 8},
		{"reanchor_epoch_of_two_blocks", "transition {6, epochLength 3}: epoch blocks 0, 4, 6, 9; at e = 6 the epoch (4, 6] has E = 2 and v4 (new at 4) has rate E - 1 = 1", append(acc, "@r07"), reanchor, 6},
		{"reanchor_next", "same configuration, e = 9 (E = 3)", append(acc, "@r07"), reanchor, 9},
		{"candidate_dropped", "at e = 8 the candidate list is v4, v3, v0, v2, v5: v1 is dropped, v5 enters with the default diligence, the order of the list is kept", acc, dropped, 8},
		{"not_an_epoch_block", "block 5 is not an epoch block: no EpochInfo (null)", []string{"WBFT-EPOCH-006", "WBFT-EPOCH-020"}, a04, 5},
		{"fail_empty_seals", "block 3 carries no previous seals: the seal accounting fails with empty seals", []string{"WBFT-EPOCH-008"}, emptySeals, 4},
		{"fail_validator_address_zero", "the EpochInfo of block 4 (crafted) has validator index 7 outside its 5 candidates: resolving the seals of epoch 2 gives the zero address", []string{"WBFT-EPOCH-008", "WBFT-VAL-007"}, zero, 8},
	}
	var cs []vgCase
	for _, x := range ins {
		b := x.s.build(x.e)
		h := b.headers[x.e]
		headers := []any{}
		for n := uint64(1); n < x.e; n++ {
			enc, err := rlp.EncodeToBytes(b.headers[n])
			vgMust(err)
			headers = append(headers, vgH(enc))
		}
		genesis, _ := rlp.EncodeToBytes(b.headers[0])
		hdr, _ := rlp.EncodeToBytes(h)
		st := b.states[x.e]
		gv := b.cc.Anzeon.SystemContracts.GovValidator.Address
		cands := []any{}
		for _, a := range systemcontracts.ValidatorList(gv, st) {
			cands = append(cands, vgM{{"address", vgA(a)}, {"bls_public_key", vgH(systemcontracts.GetBLSPublicKey(gv, st, a))}})
		}
		c := vgCase{Runner: "validators", Handler: "next_epoch_info", Name: x.name, Kind: "chain", Desc: x.desc + ". The chain fixture is not header-valid: this computation reads only Coinbase, the previous-seal bitmaps, ParentHash links and the MixDigest of the epoch header (keccak256 of the block number, as in A-04 §6.8); aggregate signatures are the placeholder c0 || 95 x 00", Reqs: x.reqs,
			Input: vgM{
				{"chain", vgM{{"config", x.s.configYAML()}, {"genesis", vgH(genesis)}, {"headers", headers}}},
				{"header", vgH(hdr)},
				{"candidates", cands},
			}}
		ne, err := func() (ne *types.EpochInfo, err error) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: panic %v", x.name, r)
				}
			}()
			return b.e.buildEpochInfo(b.ch, h, st)
		}()
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = vgM{{"epoch_info", vgEpochInfoYAML(ne)}}
			if ne != nil && vgTieOrderCase(ne.Candidates) {
				// the alias omits the case from an edition that leaves out the tie-order requirement
				c.Desc += "; tie-order case ({@r10})"
			}
		}
		cs = append(cs, c)
	}
	return cs
}

// vgTieOrderCase reports whether the candidates, as buildEpochInfo gives
// them (power 1), form a tie-order case, which the public edition omits.
func vgTieOrderCase(cands []*types.Candidate) bool {
	pc := make([]PoweredCandidate, len(cands))
	stable := make([]int, len(cands))
	for i, c := range cands {
		pc[i] = PoweredCandidate{Addr: c.Addr, Diligence: c.Diligence, Power: big.NewInt(1)}
		stable[i] = i
	}
	sort.SliceStable(stable, func(a, b int) bool { return cands[stable[a]].Diligence > cands[stable[b]].Diligence })
	for i, v := range sortCandidates(pc) {
		if v != stable[i] {
			return true
		}
	}
	return false
}
