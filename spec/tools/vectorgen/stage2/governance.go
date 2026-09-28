// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `governance`, handler `scenarios` (B-05 §13, "Scenario test
// vectors"). A case is a genesis given by its governance parameters and a
// sequence of calls; the record of each call is its outcome, its logs, the
// storage it wrote and the consensus projection after it (A-11 §3.1,
// "Governance scenarios", field `projection`).
//
// The reference builds the genesis with core.SetupGenesisBlock (the system
// contracts are initialized by the reference initializers) and applies every
// call as a transaction message of its own block with core.ApplyMessage, as
// core.ApplyTransaction does, on the state that the previous call left. The
// storage written by a call is found with an EVM logger that notes every
// SSTORE slot; the write set lists the noted slots whose value after the call
// differs from their value before it. The projection is read with the
// readers of B-04 §8.2 and the Extra flags of the accounts named in the case.

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/systemcontracts"
	"github.com/ethereum/go-ethereum/triedb"
)

// ------------------------------------------------------------ call data

func abiType(t string) abi.Type {
	ty, err := abi.NewType(t, "", nil)
	must(err)
	return ty
}

// call encodes `sig` (for example "approveProposal(uint256)") with its arguments.
func call(sig string, args ...any) []byte {
	open := strings.Index(sig, "(")
	var ts abi.Arguments
	if inner := sig[open+1 : len(sig)-1]; inner != "" {
		for _, t := range strings.Split(inner, ",") {
			ts = append(ts, abi.Argument{Type: abiType(t)})
		}
	}
	packed, err := ts.Pack(args...)
	must(err)
	return append(crypto.Keccak256([]byte(sig))[:4], packed...)
}

func u256b(x uint64) *big.Int { return new(big.Int).SetUint64(x) }

// popOf returns the BLS public key of account `i` and its proof of possession
// (the BLS signature of the key's bytes, B-05 §9) signed by account `signer`.
func popOf(i, signer int) ([]byte, []byte) {
	pk := accts[i].bls.PublicKey().Marshal()
	return pk, accts[signer].bls.Sign(pk).Marshal()
}

// ------------------------------------------------------------ genesis

// govParams is a GovBase instance of the genesis (B-05 §12).
type govParams struct {
	members    []int
	quorum     uint32
	expiry     uint64
	maxProps   uint64
	validators []int // GovValidator only
	gasTip     *big.Int
}

func (g govParams) params() map[string]string {
	var m []string
	for _, i := range g.members {
		m = append(m, accts[i].addr.Hex())
	}
	p := map[string]string{
		"quorum": fmt.Sprint(g.quorum), "expiry": fmt.Sprint(g.expiry), "memberVersion": "1",
		"maxProposals": fmt.Sprint(g.maxProps), "members": strings.Join(m, ","),
	}
	if g.validators != nil {
		var v, k []string
		for _, i := range g.validators {
			v = append(v, accts[i].addr.Hex())
			k = append(k, "0x"+common.Bytes2Hex(accts[i].bls.PublicKey().Marshal()))
		}
		p["validators"], p["blsPublicKeys"] = strings.Join(v, ","), strings.Join(k, ",")
	}
	if g.gasTip != nil {
		p["gasTip"] = g.gasTip.String()
	}
	return p
}

func (g govParams) yaml() M {
	ms := []any{}
	for _, i := range g.members {
		ms = append(ms, hexs(accts[i].addr.Bytes()))
	}
	m := M{{"members", ms}, {"quorum", dec(uint64(g.quorum))}, {"expiry", dec(g.expiry)}, {"max_proposals", dec(g.maxProps)}}
	if g.validators != nil {
		vs, ks := []any{}, []any{}
		for _, i := range g.validators {
			vs = append(vs, hexs(accts[i].addr.Bytes()))
			ks = append(ks, hexs(accts[i].bls.PublicKey().Marshal()))
		}
		m = append(m, KV{"validators", vs}, KV{"bls_public_keys", ks}, KV{"gas_tip", decBig(g.gasTip)})
	}
	return m
}

type govGenesis struct {
	validator, council govParams
	funded             []int // accounts funded with 1 000 coin
}

func (g govGenesis) yaml() M {
	fs := []any{}
	for _, i := range g.funded {
		fs = append(fs, M{{"address", hexs(accts[i].addr.Bytes())}, {"balance", decBig(coin(1000, 1))}})
	}
	return M{{"preset", "8282"}, {"gov_validator", g.validator.yaml()}, {"gov_council", g.council.yaml()}, {"alloc", fs}}
}

// build returns the configuration and the state of the genesis block, built
// by core.SetupGenesisBlock from the mainnet preset genesis with the two
// GovBase instances, anzeon.init (the validators of GovValidator) and the
// funded accounts replaced.
func (g govGenesis) build() (*params.ChainConfig, *state.StateDB, *types.Header) {
	cc := *params.StableNetMainnetChainConfig
	an := *cc.Anzeon
	sc := *an.SystemContracts
	gv := *sc.GovValidator
	gv.Params = g.validator.params()
	sc.GovValidator = &gv
	gc := *sc.GovCouncil
	gc.Params = g.council.params()
	sc.GovCouncil = &gc
	an.SystemContracts = &sc
	init := &params.WBFTInit{}
	for _, i := range g.validator.validators {
		init.Validators = append(init.Validators, accts[i].addr)
		init.BLSPublicKeys = append(init.BLSPublicKeys, "0x"+common.Bytes2Hex(accts[i].bls.PublicKey().Marshal()))
	}
	an.Init = init
	cc.Anzeon = &an
	gen := core.DefaultStableNetMainnetGenesisBlock()
	gen.Config = &cc
	alloc := types.GenesisAlloc{}
	for _, i := range g.funded {
		alloc[accts[i].addr] = types.Account{Balance: coin(1000, 1)}
	}
	gen.Alloc = alloc
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)
	_, hash, err := core.SetupGenesisBlock(db, tdb, gen)
	must(err)
	blk := rawdb.ReadBlock(db, hash, 0)
	sdb, err := state.New(blk.Root(), state.NewDatabaseWithNodeDB(db, tdb), nil)
	must(err)
	return &cc, sdb, blk.Header()
}

// ------------------------------------------------------------ steps

type govStep struct {
	label  string
	sender int
	to     common.Address
	data   []byte
	dt     uint64 // seconds after the previous step
}

// sstoreLogger notes every SSTORE slot and the slot's value at the start of
// the call (the write set is decided afterwards against the state).
type sstoreLogger struct {
	env  *vm.EVM
	pre  map[[2]common.Hash]common.Hash
	keys [][2]common.Hash
	addr map[[2]common.Hash]common.Address
}

func (l *sstoreLogger) CaptureTxStart(env *vm.EVM, gasLimit uint64, _ []types.SetCodeAuthorization) {
	l.env = env
}
func (l *sstoreLogger) CaptureTxEnd(uint64)                                                     {}
func (l *sstoreLogger) CaptureStart(common.Address, common.Address, bool, []byte, uint64, *big.Int) {}
func (l *sstoreLogger) CaptureEnd([]byte, uint64, error)                                        {}
func (l *sstoreLogger) CaptureEnter(vm.OpCode, common.Address, common.Address, []byte, uint64, *big.Int) {
}
func (l *sstoreLogger) CaptureExit([]byte, uint64, error) {}
func (l *sstoreLogger) CaptureFault(uint64, vm.OpCode, uint64, uint64, *vm.ScopeContext, int, error) {
}
func (l *sstoreLogger) CaptureState(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, rData []byte, depth int, err error) {
	if op != vm.SSTORE || err != nil {
		return
	}
	a := scope.Contract.Address()
	key := common.Hash(scope.Stack.Back(0).Bytes32())
	id := [2]common.Hash{common.BytesToHash(a.Bytes()), key}
	if _, ok := l.pre[id]; ok {
		return
	}
	l.pre[id] = l.env.StateDB.GetCommittedState(a, key)
	l.keys = append(l.keys, id)
	l.addr[id] = a
}

type govCase struct {
	name, desc string
	reqs       []string
	gen        govGenesis
	named      []int // accounts whose flags the projection reports
	steps      []govStep
}

const govStepGas = 3_000_000

func (x govCase) run() Case {
	cc, sdb, g := x.gen.build()
	gv := cc.Anzeon.SystemContracts.GovValidator.Address
	t := g.Time
	gp := new(core.GasPool).AddGas(1 << 62)
	in, out := []any{}, []any{}
	for k, s := range x.steps {
		t += s.dt
		tip := systemcontracts.GetGasTip(gv, sdb)
		extra := enc(&types.WBFTExtra{VanityData: make([]byte, 32), RandaoReveal: []byte{}, GasTip: tip})
		h := &types.Header{ParentHash: crypto.Keccak256Hash([]byte(fmt.Sprintf("wbft-vector-gov-%d", k))), Coinbase: accts[0].addr, Number: u256b(uint64(k + 1)),
			GasLimit: gasLimit, Time: t, Difficulty: big.NewInt(1), BaseFee: big.NewInt(genesisFee), Extra: extra, MixDigest: crypto.Keccak256Hash([]byte(fmt.Sprintf("wbft-vector-mix-%d", k)))}
		from := accts[s.sender].addr
		to := s.to
		price := new(big.Int).Add(h.BaseFee, tip)
		msg := &core.Message{From: from, To: &to, Nonce: sdb.GetNonce(from), Value: new(big.Int), GasLimit: govStepGas,
			GasPrice: price, GasFeeCap: price, GasTipCap: tip, Data: s.data}
		txh := crypto.Keccak256Hash([]byte(fmt.Sprintf("wbft-vector-gov-step-%d", k)))
		sdb.SetTxContext(txh, 0)
		lg := &sstoreLogger{pre: map[[2]common.Hash]common.Hash{}, addr: map[[2]common.Hash]common.Address{}}
		evm := vm.NewEVM(core.NewEVMBlockContext(h, txChain{}, &h.Coinbase), core.NewEVMTxContext(msg), sdb, cc, vm.Config{Tracer: lg})
		res, err := core.ApplyMessage(evm, msg, gp)
		if err != nil {
			panic(fmt.Sprintf("governance %s step %d: %v", x.name, k, err))
		}
		sdb.Finalise(true)
		logs := []any{}
		for _, l := range sdb.GetLogs(txh, h.Number.Uint64(), common.Hash{}) {
			topics := []any{}
			for _, tp := range l.Topics {
				topics = append(topics, hexs(tp.Bytes()))
			}
			logs = append(logs, M{{"address", hexs(l.Address.Bytes())}, {"topics", topics}, {"data", hexs(l.Data)}})
		}
		sort.Slice(lg.keys, func(i, j int) bool {
			a, b := lg.keys[i], lg.keys[j]
			if a[0] != b[0] {
				return a[0].Cmp(b[0]) < 0
			}
			return a[1].Cmp(b[1]) < 0
		})
		ws := []any{}
		for _, id := range lg.keys {
			if v := sdb.GetState(lg.addr[id], id[1]); v != lg.pre[id] {
				ws = append(ws, M{{"address", hexs(lg.addr[id].Bytes())}, {"key", hexs(id[1].Bytes())}, {"value", hexs(v.Bytes())}})
			}
		}
		status, rev := "success", []byte{}
		if res.Err != nil {
			if len(ws) != 0 || len(logs) != 0 {
				panic(fmt.Sprintf("governance %s step %d: a failed call wrote state", x.name, k))
			}
			status, rev = "failure", res.Revert()
			if errors.Is(res.Err, vm.ErrExecutionReverted) {
				status = "revert"
			}
		}
		in = append(in, M{{"label", s.label}, {"sender", hexs(from.Bytes())}, {"to", hexs(to.Bytes())}, {"data", hexs(s.data)},
			{"gas", dec(govStepGas)}, {"time", dec(t)}, {"number", dec(h.Number.Uint64())}})
		out = append(out, M{{"status", status}, {"revert_data", hexs(rev)}, {"logs", logs}, {"write_set", ws}, {"projection", projection(cc, sdb, x.named)}})
	}
	named := []any{}
	for _, i := range x.named {
		named = append(named, hexs(accts[i].addr.Bytes()))
	}
	return Case{Runner: "governance", Handler: "scenarios", Name: x.name, Kind: "steps", Desc: x.desc, Reqs: x.reqs,
		Input:    M{{"genesis", x.gen.yaml()}, {"genesis_time", dec(g.Time)}, {"named_accounts", named}, {"steps", in}},
		Expected: M{{"steps", out}}}
}

// projection is the consensus projection (A-11 §3.1, "Governance scenarios")
// with the flag sets restricted to the named accounts.
func projection(cc *params.ChainConfig, sdb *state.StateDB, named []int) M {
	gv := cc.Anzeon.SystemContracts.GovValidator.Address
	vs, ks := []any{}, []any{}
	for _, v := range systemcontracts.ValidatorList(gv, sdb) {
		vs = append(vs, hexs(v.Bytes()))
		ks = append(ks, hexs(systemcontracts.GetBLSPublicKey(gv, sdb, v)))
	}
	var bl, au []string
	for _, i := range named {
		e := sdb.GetExtra(accts[i].addr)
		if e&extraBlacklisted != 0 {
			bl = append(bl, "0x"+common.Bytes2Hex(accts[i].addr.Bytes()))
		}
		if e&extraAuthorized != 0 {
			au = append(au, "0x"+common.Bytes2Hex(accts[i].addr.Bytes()))
		}
	}
	sort.Strings(bl)
	sort.Strings(au)
	return M{{"validators", vs}, {"bls_public_keys", ks}, {"gas_tip", decBig(systemcontracts.GetGasTip(gv, sdb))}, {"blacklisted", VGList(bl)}, {"authorized", VGList(au)}}
}

// VGList converts a string slice to a YAML list (empty stays empty).
func VGList(xs []string) []any {
	out := []any{}
	for _, x := range xs {
		out = append(out, hexsStr(x))
	}
	return out
}

func hexsStr(s string) any { return hexs(common.FromHex(s)) }
