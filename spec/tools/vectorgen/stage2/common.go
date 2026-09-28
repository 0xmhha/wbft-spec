// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Shared pieces of stage 2: keys (A-11 WBFT-VEC-021), the configuration
// shape used in vector inputs, and the `chain` fixture (A-11 §3.1): a
// chain configuration, a genesis header and stored headers, all given as RLP.

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/consensus/wbft/backend"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/systemcontracts"

	"vectorgen/internal/vecfile"
)

type (
	KV   = vecfile.KV
	M    = vecfile.M
	Case = vecfile.Case
)

func dec(v uint64) vecfile.Dec      { return vecfile.DecU(v) }
func decBig(v *big.Int) vecfile.Dec { return vecfile.DecBig(v) }
func hexs(b []byte) vecfile.Hex     { return vecfile.Hexs(b) }

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func enc(v any) []byte {
	b, err := rlp.EncodeToBytes(v)
	must(err)
	return b
}

func u64(v uint64) *uint64 { return &v }

// ---------------------------------------------------------------- keys

// key_i = keccak256(ASCII("wbft-spec-vector-key-" || decimal(i))) (WBFT-VEC-021).
func keySeed(i int) []byte {
	return crypto.Keccak256([]byte(fmt.Sprintf("wbft-spec-vector-key-%d", i)))
}

type account struct {
	key  *ecdsa.PrivateKey
	addr common.Address
	bls  bls.SecretKey
}

var accts = func() []account {
	var out []account
	for i := 0; i < 16; i++ {
		k, err := crypto.ToECDSA(keySeed(i))
		must(err)
		b, err := bls.DeriveFromECDSA(k)
		must(err)
		out = append(out, account{key: k, addr: crypto.PubkeyToAddress(k.PublicKey), bls: b})
	}
	return out
}()

func accountIndex(a common.Address) int {
	for i, x := range accts {
		if x.addr == a {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------- configuration

// wbftP is a genesis `anzeon.wbft` section or the WBFT part of a transition.
type wbftP struct {
	RequestTimeoutSeconds, BlockPeriodSeconds, EpochLength, AllowedFutureBlockTime uint64
	ProposerPolicy, MaxRequestTimeoutSeconds                                       *uint64
}

func (p wbftP) params() *params.WBFTConfig {
	return &params.WBFTConfig{
		RequestTimeoutSeconds:    p.RequestTimeoutSeconds,
		BlockPeriodSeconds:       p.BlockPeriodSeconds,
		EpochLength:              p.EpochLength,
		AllowedFutureBlockTime:   p.AllowedFutureBlockTime,
		ProposerPolicy:           p.ProposerPolicy,
		MaxRequestTimeoutSeconds: p.MaxRequestTimeoutSeconds,
	}
}

func optDec(p *uint64) any {
	if p == nil {
		return nil
	}
	return dec(*p)
}

// yaml renders the section. Absent pointer fields are null; the other fields
// are always written (0 and absent are the same for them).
func (p wbftP) yaml() M {
	return M{
		{"request_timeout_seconds", dec(p.RequestTimeoutSeconds)},
		{"block_period_seconds", dec(p.BlockPeriodSeconds)},
		{"epoch_length", dec(p.EpochLength)},
		{"allowed_future_block_time", dec(p.AllowedFutureBlockTime)},
		{"proposer_policy", optDec(p.ProposerPolicy)},
		{"max_request_timeout_seconds", optDec(p.MaxRequestTimeoutSeconds)},
	}
}

type trans struct {
	Block uint64
	P     wbftP
}

// cfgSpec is the configuration of a vector: the WBFT section, the transitions
// in configuration order and, for chain fixtures, anzeon.init and the two
// fork times that header verification rejects.
type cfgSpec struct {
	W        wbftP
	T        []trans
	Init     []int          // account indices of anzeon.init.validators
	InitBLS  map[int][]byte // position -> BLS public key replacing the derived one
	Shanghai *uint64
	Cancun   *uint64
	Boho     *uint64 // overrides BohoBlock of the preset; written only when set
}

func (s cfgSpec) transitionsYAML() []any {
	out := []any{}
	for _, t := range s.T {
		m := M{{"block", dec(t.Block)}}
		m = append(m, t.P.yaml()...)
		out = append(out, m)
	}
	return out
}

// pureYAML is the `config` input of the pure handlers.
func (s cfgSpec) pureYAML() M {
	return M{{"wbft", s.W.yaml()}, {"transitions", s.transitionsYAML()}}
}

func (s cfgSpec) initBLS() [][]byte {
	var out [][]byte
	for pos, i := range s.Init {
		if k, ok := s.InitBLS[pos]; ok {
			out = append(out, k)
		} else {
			out = append(out, accts[i].bls.PublicKey().Marshal())
		}
	}
	return out
}

// chainYAML is the `config` of a chain fixture.
func (s cfgSpec) chainYAML() M {
	var vals, keys []any
	for pos, i := range s.Init {
		vals = append(vals, hexs(accts[i].addr.Bytes()))
		keys = append(keys, hexs(s.initBLS()[pos]))
	}
	m := M{
		{"preset", "8282"},
		{"init", M{{"validators", vals}, {"bls_public_keys", keys}}},
		{"wbft", s.W.yaml()},
		{"transitions", s.transitionsYAML()},
		{"shanghai_time", optDec(s.Shanghai)},
		{"cancun_time", optDec(s.Cancun)},
	}
	if s.Boho != nil {
		m = append(m, KV{"boho_block", dec(*s.Boho)})
	}
	return m
}

// chainConfig is the mainnet preset (B-01 §11) with anzeon.wbft, anzeon.init,
// transitions and the two fork times replaced. The preset is not modified.
func (s cfgSpec) chainConfig() *params.ChainConfig {
	cc := *params.StableNetMainnetChainConfig
	an := *cc.Anzeon
	an.WBFT = s.W.params()
	init := &params.WBFTInit{}
	for pos, i := range s.Init {
		init.Validators = append(init.Validators, accts[i].addr)
		init.BLSPublicKeys = append(init.BLSPublicKeys, "0x"+common.Bytes2Hex(s.initBLS()[pos]))
	}
	an.Init = init
	cc.Anzeon = &an
	cc.Transitions = nil
	for _, t := range s.T {
		cc.Transitions = append(cc.Transitions, params.Transition{Block: new(big.Int).SetUint64(t.Block), WBFTConfig: t.P.params()})
	}
	cc.ShanghaiTime = s.Shanghai
	cc.CancunTime = s.Cancun
	if s.Boho != nil {
		cc.BohoBlock = new(big.Int).SetUint64(*s.Boho)
	}
	return &cc
}

// wbftConfig builds the consensus configuration exactly as a node does
// (eth/ethconfig.SetConfigFromChainConfig, which also sorts the transitions).
func wbftConfig(cc *params.ChainConfig) *wbft.Config {
	cfg := &wbft.Config{}
	must(ethconfig.SetConfigFromChainConfig(cfg, cc))
	return cfg
}

// --------------------------------------------------------- chain fixture

// fxChain is a consensus.ChainHeaderReader over a fixture: every stored header
// is canonical at its number. StateAt fails unless a gas tip is set (then it
// returns a state whose GovValidator gas-tip slot holds it).
type fxChain struct {
	cc     *params.ChainConfig
	byHash map[common.Hash]*types.Header
	byNum  map[uint64]*types.Header
	head   *types.Header
	gasTip *big.Int
}

var _ consensus.ChainHeaderReader = (*fxChain)(nil)

func newFx(cc *params.ChainConfig) *fxChain {
	return &fxChain{cc: cc, byHash: map[common.Hash]*types.Header{}, byNum: map[uint64]*types.Header{}}
}

func (f *fxChain) add(h *types.Header) {
	f.byHash[h.Hash()] = h
	f.byNum[h.Number.Uint64()] = h
	if f.head == nil || h.Number.Cmp(f.head.Number) > 0 {
		f.head = h
	}
}

func (f *fxChain) Config() *params.ChainConfig  { return f.cc }
func (f *fxChain) CurrentHeader() *types.Header { return f.head }
func (f *fxChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	h := f.byHash[hash]
	if h == nil || h.Number.Uint64() != number {
		return nil
	}
	return h
}
func (f *fxChain) GetHeaderByNumber(number uint64) *types.Header  { return f.byNum[number] }
func (f *fxChain) GetHeaderByHash(hash common.Hash) *types.Header { return f.byHash[hash] }
func (f *fxChain) GetTd(common.Hash, uint64) *big.Int             { return nil }
func (f *fxChain) StateAt(root common.Hash) (*state.StateDB, error) {
	if f.gasTip == nil {
		return nil, errors.New("state unavailable (vector fixture has no state)")
	}
	sdb, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	if err != nil {
		return nil, err
	}
	sdb.SetState(f.cc.Anzeon.SystemContracts.GovValidator.Address, common.HexToHash(systemcontracts.SLOT_VALIDATOR_gasTip), common.BigToHash(f.gasTip))
	return sdb, nil
}

// fixture is a chain fixture: its configuration and the RLP of its headers.
// nonCanonical holds stored headers that are not canonical at their number
// (found by hash only); it is written only when it is not empty.
type fixture struct {
	spec         cfgSpec
	genesis      []byte
	headers      [][]byte
	nonCanonical [][]byte
}

func (x fixture) yaml() M {
	hs := []any{}
	for _, h := range x.headers {
		hs = append(hs, hexs(h))
	}
	m := M{{"config", x.spec.chainYAML()}, {"genesis", hexs(x.genesis)}, {"headers", hs}}
	if len(x.nonCanonical) > 0 {
		nc := []any{}
		for _, h := range x.nonCanonical {
			nc = append(nc, hexs(h))
		}
		m = append(m, KV{"non_canonical", nc})
	}
	return m
}

// reader decodes the fixture again, so that every expected value is computed
// from exactly the bytes written into the vector. It has no state.
func (x fixture) reader() *fxChain {
	f := newFx(x.spec.chainConfig())
	for _, b := range append([][]byte{x.genesis}, x.headers...) {
		h := new(types.Header)
		must(rlp.DecodeBytes(b, h))
		f.add(h)
	}
	for _, b := range x.nonCanonical {
		h := new(types.Header)
		must(rlp.DecodeBytes(b, h))
		f.byHash[h.Hash()] = h // by hash only: not canonical, not the head
	}
	return f
}

// withHeaders returns a copy of the fixture that keeps only the stored headers
// with the given numbers (the genesis header is always kept).
func (x fixture) keep(nums ...uint64) fixture {
	want := map[uint64]bool{}
	for _, n := range nums {
		want[n] = true
	}
	y := x
	y.headers = nil
	for _, b := range x.headers {
		h := new(types.Header)
		must(rlp.DecodeBytes(b, h))
		if want[h.Number.Uint64()] {
			y.headers = append(y.headers, b)
		}
	}
	return y
}

// withSpec returns the fixture with another configuration (same headers).
func (x fixture) withSpec(s cfgSpec) fixture {
	y := x
	y.spec = s
	return y
}

// ---------------------------------------------------------- chain builder

const (
	gasLimit    = 105_000_000
	genesisFee  = 20_000_000_000_000
	defaultTip  = params.InitialGasTip
	pastTime    = 1_700_000_000 // chains used for verification: times in the past
	futureTime  = 2_100_000_000 // chains used for proposal construction: parent time dominates the clock
	vectorNow   = 2_000_000_000 // the `now` input of time-dependent cases
	futureStamp = 3_000_000_000 // a header time that is in the future for `now`
)

type builder struct {
	spec    cfgSpec
	cc      *params.ChainConfig
	cfg     *wbft.Config
	ch      *fxChain
	genesis *types.Header
	headers []*types.Header
	blsOv   map[common.Address]bls.SecretKey
}

func rootFor(n uint64) common.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("wbft-vector-state-root-%d", n)))
}

// newBuilder creates the genesis header from the configuration with the
// reference function wbft.CreateInitialExtraData (A-04 §7, B-02).
func newBuilder(spec cfgSpec, t0 uint64, blsOv map[int]bls.SecretKey) *builder {
	b := &builder{spec: spec, cc: spec.chainConfig(), blsOv: map[common.Address]bls.SecretKey{}}
	b.cfg = wbftConfig(b.cc)
	for pos, sk := range blsOv {
		b.blsOv[accts[spec.Init[pos]].addr] = sk
	}
	extra, err := wbft.CreateInitialExtraData(b.cc.Anzeon)
	must(err)
	g := &types.Header{
		UncleHash:   types.EmptyUncleHash,
		Root:        rootFor(0),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(0),
		GasLimit:    gasLimit,
		Time:        t0,
		Extra:       extra,
		BaseFee:     big.NewInt(genesisFee),
	}
	b.ch = newFx(b.cc)
	b.ch.gasTip = new(big.Int).SetUint64(defaultTip)
	b.ch.add(g)
	b.genesis = g
	return b
}

func (b *builder) engine() *wbftengine.Engine {
	return wbftengine.NewEngine(b.cfg, common.Address{}, nil, nil)
}

func (b *builder) head() *types.Header { return b.ch.head }

// skeleton is the header the execution layer hands to Prepare (A-08 §3).
func (b *builder) skeleton(parent *types.Header, vanity []byte) *types.Header {
	n := new(big.Int).Add(parent.Number, big.NewInt(1))
	return &types.Header{
		ParentHash:  parent.Hash(),
		UncleHash:   types.EmptyUncleHash,
		Root:        rootFor(n.Uint64()),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(0),
		Number:      n,
		GasLimit:    parent.GasLimit,
		Extra:       append([]byte{}, vanity...),
		BaseFee:     eip1559.CalcBaseFee(b.cc, parent),
	}
}

// propose runs the reference Backend.Prepare as account `key` on top of
// parent and then fixes Time to parent.Time + block period, so that a chain
// in the past does not depend on the wall clock (Prepare uses max(.., now)).
func (b *builder) propose(parent *types.Header, key int) *types.Header {
	h := b.skeleton(parent, nil)
	be := backend.New(b.cfg, accts[key].key, rawdb.NewMemoryDatabase())
	must(be.Prepare(b.ch, h))
	h.Time = parent.Time + b.cfg.GetConfig(h.Number).BlockPeriod
	return h
}

// blsOf returns the BLS secret key that seals for address a.
func (b *builder) blsOf(a common.Address) bls.SecretKey {
	if sk, ok := b.blsOv[a]; ok {
		return sk
	}
	i := accountIndex(a)
	if i < 0 {
		panic("unknown validator address " + a.Hex())
	}
	return accts[i].bls
}

// sealsFor signs seal_data(h, round, type) with the validators at the given
// indices of validators_at(h.Number).
func (b *builder) sealsFor(h *types.Header, round uint32, st wbftcore.SealType, idx []int) []wbft.SealData {
	vs, err := b.engine().GetValidators(b.ch, h.Number, h.ParentHash, nil)
	must(err)
	var out []wbft.SealData
	for _, i := range idx {
		a := vs.GetByIndex(uint64(i)).Address()
		out = append(out, wbft.SealData{Sealer: uint32(i), Seal: b.blsOf(a).Sign(wbftcore.PrepareSeal(h, round, st)).Marshal()})
	}
	return out
}

// seal writes the seals of the given validator indices with the reference
// CommitHeader (A-08 §4).
func (b *builder) seal(h *types.Header, round uint32, idx []int) {
	must(b.engine().CommitHeader(h, b.sealsFor(h, round, wbftcore.SealTypePrepare, idx), b.sealsFor(h, round, wbftcore.SealTypeCommit, idx), new(big.Int).SetUint64(uint64(round))))
}

func (b *builder) store(h *types.Header) {
	b.ch.add(h)
	b.headers = append(b.headers, h)
}

// block proposes, optionally writes an EpochInfo, seals and stores the next block.
func (b *builder) block(key int, round uint32, sealers []int, ei *types.EpochInfo) *types.Header {
	h := b.propose(b.head(), key)
	if ei != nil {
		_, err := wbftengine.ApplyHeaderWBFTExtra(h, wbftengine.WriteEpochInfo(ei))
		must(err)
	}
	b.seal(h, round, sealers)
	b.store(h)
	return h
}

func (b *builder) fixture() fixture {
	x := fixture{spec: b.spec, genesis: enc(b.genesis)}
	for _, h := range b.headers {
		x.headers = append(x.headers, enc(h))
	}
	return x
}

// epochInfo builds an EpochInfo over the given candidate accounts and
// validator positions (indices into the candidates).
func epochInfo(cands []int, vals []int) *types.EpochInfo {
	ei := &types.EpochInfo{}
	for _, c := range cands {
		ei.Candidates = append(ei.Candidates, &types.Candidate{Addr: accts[c].addr, Diligence: types.DefaultDiligence})
	}
	for _, v := range vals {
		ei.Validators = append(ei.Validators, uint32(v))
		ei.BLSPublicKeys = append(ei.BLSPublicKeys, accts[cands[v]].bls.PublicKey().Marshal())
	}
	return ei
}

func sortedKeys[K ~string | ~int](m map[K]bool) []K {
	var out []K
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type chainConfigT = params.ChainConfig
