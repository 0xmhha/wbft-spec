// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `execution`, handler `process_finalize` (B-06): the finalization of
// one block on the state after its transactions. The chain fixture gives the
// headers that finalization reads (the parent for the gas tip, the epoch
// headers for the validator set and the diligence); the state after the
// transactions is the input `accounts` and the gas tip slot of the parent
// state is the input `parent_gas_tip`, because a fixture carries no state.
//
// The expected values come from Engine.Finalize (import path, verify_epoch)
// and Engine.FinalizeAndAssemble (proposer path, write_epoch) of the
// reference, over fxChain with StateAt answering the parent gas tip.

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/wbft/backend"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/systemcontracts"
)

// chainAEpoch is chainA with the given EpochInfo at block 4.
func chainAEpoch(t0 uint64, upTo int, ei *types.EpochInfo, spec *cfgSpec) *builder {
	s := specA(t0)
	if spec != nil {
		s = *spec
	}
	b := newBuilder(s, t0, nil)
	plan := []struct {
		key     int
		round   uint32
		sealers []int
	}{
		{0, 0, []int{0, 1, 2}},
		{1, 1, []int{0, 1, 2, 3}},
		{2, 0, []int{1, 2, 3}},
		{3, 0, []int{0, 1, 2}},
		{4, 0, []int{0, 1, 2}},
		{0, 0, []int{0, 1, 2, 3}},
		{3, 0, []int{0, 1, 2}},
	}
	for i := 0; i < upTo; i++ {
		var e *types.EpochInfo
		if i+1 == 4 {
			e = ei
		}
		b.block(plan[i].key, plan[i].round, plan[i].sealers, e)
	}
	return b
}

// epochInfoDil is epochInfo with the given diligence per candidate.
func epochInfoDil(cands, vals []int, dil []uint64) *types.EpochInfo {
	ei := epochInfo(cands, vals)
	for i, d := range dil {
		ei.Candidates[i].Diligence = d
	}
	return ei
}

// gvAccount is the GovValidator account of the genesis path (code of
// version v1 and the storage written by the initializer) listing the
// validators of the given accounts.
func gvAccount(idx []int) acct {
	a, k := keysOf(idx)
	p := gvParams(a, k, "")
	st, err := systemcontracts.GetSystemContractsTransition(&params.SystemContracts{GovValidator: &params.SystemContract{Address: params.DefaultGovValidatorAddress, Version: "v1", Params: p}}, nil)
	must(err)
	return acct{addr: params.DefaultGovValidatorAddress, balance: new(big.Int), code: hexutil.MustDecode(st.Codes[0].Code), storage: gvStorage(params.DefaultGovValidatorAddress, p)}
}

// holders gives each account of the list a balance of `bal` coin.
func holders(idx []int, bal int64) []acct {
	var out []acct
	for _, i := range idx {
		out = append(out, acct{addr: accts[i].addr, balance: coin(bal, 1)})
	}
	return out
}

type finIn struct {
	name, desc string
	reqs       []string
	x          fixture
	h          *types.Header
	tip        *big.Int // gas-tip slot of the parent state; nil: the parent state is unavailable
	accounts   []acct
	proposer   int // >= 0: proposer path with this account's engine; < 0: import path
	watch      []common.Address
}

func eiYAML(ei *types.EpochInfo) any {
	if ei == nil {
		return nil
	}
	cs := []any{}
	for _, c := range ei.Candidates {
		cs = append(cs, M{{"address", hexs(c.Addr.Bytes())}, {"diligence", dec(c.Diligence)}})
	}
	vs := []any{}
	for _, v := range ei.Validators {
		vs = append(vs, dec(uint64(v)))
	}
	ks := []any{}
	for _, k := range ei.BLSPublicKeys {
		ks = append(ks, hexs(k))
	}
	return M{{"candidates", cs}, {"validators", vs}, {"bls_public_keys", ks}}
}

func finalizeCase(x finIn) Case {
	for _, a := range x.accounts {
		if a.balance.Sign() == 0 && a.nonce == 0 && len(a.code) == 0 {
			panic(fmt.Sprintf("process_finalize %s: input account %s is empty", x.name, a.addr.Hex()))
		}
	}
	ch := x.x.reader()
	ch.gasTip = x.tip
	cfg := wbftConfig(ch.cc)
	sdb := memState(x.accounts)
	h := new(types.Header)
	must(rlp.DecodeBytes(enc(x.h), h))
	var err error
	var written *types.EpochInfo
	path := "import"
	if x.proposer >= 0 {
		path = "propose"
		be := backend.New(cfg, accts[x.proposer].key, rawdb.NewMemoryDatabase())
		var blk *types.Block
		blk, err = be.Engine().FinalizeAndAssemble(ch, h, sdb, nil, nil, nil)
		if err == nil {
			h = blk.Header()
			xe, e2 := types.ExtractWBFTExtra(h)
			must(e2)
			written = xe.EpochInfo
		}
	} else {
		err = wbftengine.NewEngine(cfg, common.Address{}, nil, nil).Finalize(ch, h, sdb, nil, nil)
	}
	var tip any
	if x.tip != nil {
		tip = decBig(x.tip)
	}
	c := Case{Runner: "execution", Handler: "process_finalize", Name: x.name, Kind: "chain", Desc: x.desc, Reqs: x.reqs,
		Input: M{{"chain", x.x.yaml()}, {"header", hexs(enc(x.h))}, {"parent_gas_tip", tip}, {"accounts", acctsYAML(x.accounts)}, {"path", path}}}
	if err != nil {
		// a rejected block has no state root: keep only what the rejection asserts
		var rq []string
		for _, id := range c.Reqs {
			if id != "SNET-FIN-001" && id != "SNET-FIN-021" {
				rq = append(rq, id)
			}
		}
		c.Reqs = rq
		c.Err = err.Error()
		return c
	}
	after := []any{}
	seen := map[common.Address]bool{}
	for _, a := range append(append([]common.Address{}, addrsOf(x.accounts)...), x.watch...) {
		if seen[a] {
			continue
		}
		seen[a] = true
		after = append(after, M{{"address", hexs(a.Bytes())}, {"balance", decBig(sdb.GetBalance(a).ToBig())}, {"code_hash", hexs(sdb.GetCodeHash(a).Bytes())}})
	}
	var epoch any
	if path == "propose" {
		epoch = eiYAML(written)
	}
	c.Expected = M{{"root", hexs(h.Root.Bytes())}, {"epoch_info", epoch}, {"accounts", after}}
	return c
}

func addrsOf(as []acct) []common.Address {
	var out []common.Address
	for _, a := range as {
		out = append(out, a.addr)
	}
	return out
}

func finalizeCases() []Case {
	tip := new(big.Int).SetUint64(defaultTip)
	// chain with unequal diligence at the epoch block 4 (the values of A-04 §6.8 epoch 1)
	dil := []uint64{1_888_750, 1_832_500, 1_898_125, 1_870_000, 1_900_000}
	bD := chainAEpoch(pastTime, 5, epochInfoDil([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0}, dil), nil)
	d := bD.fixture()
	gas := func(g uint64) func(h *types.Header) { return func(h *types.Header) { h.GasUsed = g } }
	b6 := bD.variant(0, 0, []int{0, 1, 2, 3}, gas(72_000), nil) // block 6 by key 0
	vals := holders([]int{0, 1, 2, 3, 4}, 1)
	base := []string{"SNET-FIN-001", "SNET-FIN-002", "SNET-FIN-021"}
	r := func(ids ...string) []string { return append(append([]string{}, base...), ids...) }
	var cs []Case
	add := func(x finIn) { cs = append(cs, finalizeCase(x)) }

	// ---- base-fee distribution (B-06 §3)
	add(finIn{"base_fee_distribution_with_dust", "block 6 with gas used 72 000 and base fee 20 000 gwei: the base fee 1.44 coin is shared among the validators of V(6) (keys 3, 1, 4, 0 of the EpochInfo of block 4) in proportion to their diligence 1 870 000, 1 832 500, 1 900 000 and 1 888 750; the rounding remainder goes to the coinbase (key 0)",
		r("SNET-FIN-007", "SNET-FIN-008", "SNET-FIN-009", "SNET-FIN-010", "SNET-FIN-012", "SNET-FIN-017", "SNET-FIN-018", "SNET-TX-051"), d, b6, tip, vals, -1, nil})
	add(finIn{"no_gas_used", "block 6 with gas used 0: nothing is distributed; the state root is that of the input state", r("SNET-FIN-007", "SNET-FIN-017", "SNET-FIN-018"), d, bD.variant(0, 0, []int{0, 1, 2, 3}, nil, nil), tip, vals, -1, nil})
	eq := chainA(pastTime, 5, true)
	add(finIn{"base_fee_distribution_equal_diligence", "block 6 with gas used 21 001 on the chain whose EpochInfo at block 4 gives every candidate the default diligence: four equal shares and no remainder", r("SNET-FIN-007", "SNET-FIN-009", "SNET-FIN-010", "SNET-FIN-012"), eq.fixture(), eq.variant(0, 0, []int{0, 1, 2, 3}, gas(21_001), nil), tip, vals, -1, nil})
	zero := chainAEpoch(pastTime, 5, epochInfoDil([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0}, []uint64{0, 0, 0, 0, 0}), nil)
	add(finIn{"base_fee_zero_diligence", "block 6 on the chain whose EpochInfo at block 4 gives every candidate diligence 0: the diligence sum is 0, so the whole base fee goes to the coinbase (key 0)", r("SNET-FIN-007", "SNET-FIN-010", "SNET-FIN-012"), zero.fixture(), zero.variant(0, 0, []int{0, 1, 2, 3}, gas(72_000), nil), tip, vals, -1, nil})
	// ---- gas tip (B-06 §5)
	add(finIn{"gas_tip_mismatch", "block 6 whose header gas tip is the default 27 600 gwei while the parent state holds 1 gwei: the block is invalid", r("SNET-FIN-017", "SNET-FIN-018"), d, b6, big.NewInt(1_000_000_000), vals, -1, nil})
	add(finIn{"gas_tip_parent_state_unavailable", "block 6 when the parent state is unavailable: finalization fails (unlike header verification, which skips the check)", r("SNET-FIN-018"), d, b6, nil, vals, -1, nil})
	// ---- epoch information on a non-epoch block (SNET-FIN-016)
	add(finIn{"epoch_info_on_non_epoch_block", "block 6 (not an epoch block) whose extra carries an EpochInfo: the block is invalid", r("SNET-FIN-016", "WBFT-HDR-131"), d,
		bD.variant(0, 0, []int{0, 1, 2, 3}, func(h *types.Header) {
			h.GasUsed = 72_000
			_, err := wbftengine.ApplyHeaderWBFTExtra(h, wbftengine.WriteEpochInfo(epochInfo([]int{0, 1, 2, 3}, []int{0, 1, 2, 3})))
			must(err)
		}, nil), tip, vals, -1, nil})

	// ---- epoch block 8 (B-06 §4, A-04 §6)
	bE := chainAEpoch(pastTime, 7, epochInfoDil([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0}, dil), nil)
	e := bE.fixture()
	gvAcc := gvAccount([]int{0, 1, 2, 3, 4})
	state8 := append(holders([]int{0, 1, 2, 3, 4}, 1), gvAcc)
	h8 := bE.propose(bE.head(), 1) // proposer key 1, a validator of V(8)
	propose := finalizeCase(finIn{"epoch_block_propose", "epoch block 8 on the proposer path: the EpochInfo for the next epoch is computed from the state after the transactions (candidates keys 0..4 in the GovValidator list, diligence from the seals of blocks 5..8) and written into the header before the gas-tip check and the state root",
		r("SNET-FIN-013", "SNET-FIN-014", "SNET-FIN-017", "SNET-FIN-018", "@r24", "SNET-SRC-001", "SNET-SRC-006", "SNET-SRC-007"), e, h8, tip, state8, 1, nil})
	cs = append(cs, propose)
	// the same block as the import path receives it: the written EpochInfo, sealed
	written := new(types.Header)
	must(rlp.DecodeBytes(enc(h8), written))
	var ei *types.EpochInfo
	{
		cfg := wbftConfig(e.reader().cc)
		ch := e.reader()
		ch.gasTip = tip
		be := backend.New(cfg, accts[1].key, rawdb.NewMemoryDatabase())
		blk, err := be.Engine().FinalizeAndAssemble(ch, written, memState(state8), nil, nil, nil)
		must(err)
		written = blk.Header()
		xe, err := types.ExtractWBFTExtra(written)
		must(err)
		ei = xe.EpochInfo
	}
	sealed := func(mod func(x *types.EpochInfo) *types.EpochInfo) *types.Header {
		h := types.CopyHeader(written)
		setExtra(h, func(x *types.WBFTExtra) { x.EpochInfo = mod(ei) })
		bE.seal(h, 0, []int{0, 1, 2})
		return h
	}
	same := func(x *types.EpochInfo) *types.EpochInfo { return x }
	add(finIn{"epoch_block_import_valid", "epoch block 8 as the import path receives it, with the EpochInfo that the proposer path wrote, sealed: the recomputed EpochInfo matches",
		r("SNET-FIN-013", "SNET-FIN-015", "SNET-FIN-017", "SNET-FIN-018", "@r24", "WBFT-EPOCH-021", "SNET-SRC-001"), e, sealed(same), tip, state8, -1, nil})
	add(finIn{"epoch_block_import_missing_epoch_info", "epoch block 8 without EpochInfo: the block is invalid (WBFT: epochInfo is nil)", r("SNET-FIN-015", "SNET-SRC-001"), e,
		sealed(func(*types.EpochInfo) *types.EpochInfo { return nil }), tip, state8, -1, nil})
	add(finIn{"epoch_block_import_diligence_mismatch", "epoch block 8 whose EpochInfo gives the first candidate a diligence one higher than the recomputed one: the block is invalid", r("SNET-FIN-015", "SNET-SRC-001"), e,
		sealed(func(x *types.EpochInfo) *types.EpochInfo {
			y := types.EpochInfo{Validators: x.Validators, BLSPublicKeys: x.BLSPublicKeys}
			for i, c := range x.Candidates {
				cc := *c
				if i == 0 {
					cc.Diligence++
				}
				y.Candidates = append(y.Candidates, &cc)
			}
			return &y
		}), tip, state8, -1, nil})
	h8x := bE.propose(bE.head(), 6)
	add(finIn{"epoch_block_propose_not_validator", "epoch block 8 prepared by key 6, which is not in V(8): the proposer path writes no EpochInfo (the block would then be invalid on import)", r("SNET-FIN-014"), e, h8x, tip, state8, 6, nil})

	// ---- system-contract upgrade at the block (B-06 §2.1, B-04)
	up := specA(pastTime)
	boho := uint64(6)
	up.Boho = &boho
	bU := chainAEpoch(pastTime, 5, epochInfoDil([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0}, dil), &up)
	minter := params.StableNetMainnetChainConfig.Anzeon.SystemContracts.GovMinter.Address
	add(finIn{"upgrade_at_boho_block", "BohoBlock 6: finalizing block 6 applies the Boho upgrade first (GovMinter code of version v2), then the base-fee distribution; the GovMinter account is not in the input state, so it is created with the new code",
		r("SNET-FIN-003", "SNET-FIN-004", "SNET-SYS-021", "@r30", "SNET-CFG-011"), bU.fixture(), bU.variant(0, 0, []int{0, 1, 2, 3}, gas(72_000), nil), tip, vals, -1, []common.Address{minter}})
	add(finIn{"no_upgrade_after_boho_block", "BohoBlock 6: block 7 applies no upgrade (upgrades are applied only at their block)", r("SNET-SYS-021", "SNET-CFG-011"),
		func() fixture {
			b := chainAEpoch(pastTime, 6, epochInfoDil([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0}, dil), &up)
			return b.fixture()
		}(), func() *types.Header {
			b := chainAEpoch(pastTime, 6, epochInfoDil([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0}, dil), &up)
			return b.variant(3, 0, []int{0, 1, 2}, gas(72_000), nil)
		}(), tip, vals, -1, []common.Address{minter}})
	return cs
}
