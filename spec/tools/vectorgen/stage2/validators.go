// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `validators` handlers that use only exported reference functions:
// quorum, proposer, epoch_boundary, validators_at (A-04 §§2-5).

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/consensus/wbft/validator"
)

func addrN(i int) common.Address { return common.BigToAddress(big.NewInt(int64(0x1000 + i))) }

// ---------------------------------------------------------------- quorum

// quorumCases: validator.NewSet(...).F() and .QuorumSize(); the F+1 value is
// the unique count in the window of roundchange.go:161
// (float64(num) > F && float64(num) <= F+1), scanned over num = 0 .. n+2.
func quorumCases() []Case {
	sizes := []int{}
	for n := 0; n <= 22; n++ {
		sizes = append(sizes, n)
	}
	sizes = append(sizes, 31, 64, 100, 101, 102, 128, 255, 256, 1000)
	var cs []Case
	for _, n := range sizes {
		addrs := make([]common.Address, n)
		keys := make([][]byte, n)
		for i := range addrs {
			addrs[i] = addrN(i)
			keys[i] = []byte{byte(i)}
		}
		vs := validator.NewSet(addrs, keys, wbft.NewRoundRobinProposerPolicy())
		f := vs.F()
		var win []int
		for num := 0; num <= n+2; num++ {
			if float64(num) > f && float64(num) <= f+1 {
				win = append(win, num)
			}
		}
		if len(win) != 1 {
			panic(fmt.Sprintf("n=%d: F+1 window holds %v", n, win))
		}
		bits := make([]byte, 8)
		binary.BigEndian.PutUint64(bits, math.Float64bits(f))
		desc := fmt.Sprintf("N = %d: F = %.17g (binary64), quorum %d, F+1 threshold %d", n, f, vs.QuorumSize(), win[0])
		if n == 0 {
			desc += "; N = 0 is not reachable on a valid chain but is evaluated when no validator set can be loaded (A-04 §2.2)"
		}
		cs = append(cs, Case{
			Runner: "validators", Handler: "quorum", Name: fmt.Sprintf("n_%d", n), Kind: "pure",
			Desc:     desc,
			Reqs:     []string{"WBFT-VAL-001", "WBFT-VAL-002", "WBFT-VAL-003"},
			Input:    M{{"n", dec(uint64(n))}},
			Expected: M{{"f_float64_bits", hexs(bits)}, {"quorum", dec(uint64(vs.QuorumSize()))}, {"f_plus_one", dec(uint64(win[0]))}},
		})
	}
	return cs
}

// -------------------------------------------------------------- proposer

func proposerCases() []Case {
	type in struct {
		name, desc string
		n          int
		policy     uint64
		last       common.Address
		round      uint64
		reqs       []string
	}
	rr := []string{"WBFT-PROP-003", "WBFT-PROP-005"}
	st := []string{"WBFT-PROP-004", "WBFT-PROP-005"}
	var ins []in
	lasts := []struct {
		tag  string
		addr common.Address
		desc string
	}{
		{"zero", common.Address{}, "last proposer ZERO_ADDRESS (sequence 1)"},
		{"idx1", addrN(1), "last proposer at index 1"},
		{"idx3", addrN(3), "last proposer at index 3 (the last entry)"},
		{"nonmember", addrN(9), "last proposer not a member: offset 0 (WBFT-PROP-006)"},
	}
	for _, pol := range []struct {
		id  uint64
		tag string
		rq  []string
	}{{0, "round_robin", rr}, {1, "sticky", st}, {7, "id7", rr}} {
		for _, l := range lasts {
			for _, r := range []uint64{0, 1, 2, 5} {
				reqs := append([]string{"WBFT-PROP-001"}, pol.rq...)
				if l.tag == "nonmember" {
					reqs = append(reqs, "WBFT-PROP-006")
				}
				ins = append(ins, in{fmt.Sprintf("%s_last_%s_round_%d", pol.tag, l.tag, r),
					fmt.Sprintf("4 validators, policy id %d, %s, round %d", pol.id, l.desc, r), 4, pol.id, l.addr, r, reqs})
			}
		}
	}
	ins = append(ins,
		in{"round_robin_seed_wraps_uint64", "3 validators, RoundRobin, last proposer at index 2, round 2^64-1: seed 2 + (2^64-1) + 1 wraps modulo 2^64 to 2, index 2 (unbounded arithmetic would give index 0)", 3, 0, addrN(2), math.MaxUint64, append([]string{"WBFT-PROP-001"}, rr...)},
		in{"sticky_seed_wraps_uint64", "3 validators, Sticky, last proposer at index 2, round 2^64-1: seed wraps modulo 2^64 to 1", 3, 1, addrN(2), math.MaxUint64, append([]string{"WBFT-PROP-001"}, st...)},
		in{"round_robin_single_validator", "1 validator, RoundRobin, last proposer itself, round 3", 1, 0, addrN(0), 3, append([]string{"WBFT-PROP-001"}, rr...)},
		in{"empty_set", "empty validator set: no proposer (calc_proposer returns None, A-04 §5.1)", 0, 0, addrN(1), 0, append([]string{"WBFT-PROP-001"}, rr...)},
	)
	var cs []Case
	for _, x := range ins {
		addrs := make([]common.Address, x.n)
		keys := make([][]byte, x.n)
		vl := []any{}
		for i := range addrs {
			addrs[i] = addrN(i)
			keys[i] = []byte{byte(i)}
			vl = append(vl, hexs(addrs[i].Bytes()))
		}
		vs := validator.NewSet(addrs, keys, wbft.NewProposerPolicy(wbft.ProposerPolicyId(x.policy)))
		vs.CalcProposer(x.last, x.round)
		var exp M
		if p := vs.GetProposer(); p == nil {
			exp = M{{"index", nil}, {"address", nil}}
		} else {
			i, _ := vs.GetByAddress(p.Address())
			exp = M{{"index", dec(uint64(i))}, {"address", hexs(p.Address().Bytes())}}
		}
		cs = append(cs, Case{
			Runner: "validators", Handler: "proposer", Name: x.name, Kind: "pure", Desc: x.desc, Reqs: x.reqs,
			Input:    M{{"validators", vl}, {"policy", dec(x.policy)}, {"last_proposer", hexs(x.last.Bytes())}, {"round", dec(x.round)}},
			Expected: exp,
		})
	}
	return cs
}

// -------------------------------------------------------- epoch boundary

func epochBoundaryCases() []Case {
	type cfgCase struct {
		tag, desc string
		spec      cfgSpec
		nums      []uint64
		reqs      []string
	}
	pol := u64(0)
	base := func(epoch uint64, ts ...trans) cfgSpec {
		return cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: epoch, ProposerPolicy: pol}, T: ts}
	}
	a04 := base(10,
		trans{25, wbftP{EpochLength: 7}},
		trans{40, wbftP{BlockPeriodSeconds: 2, ProposerPolicy: u64(1)}},
		trans{50, wbftP{EpochLength: 7}})
	unsorted := base(10,
		trans{50, wbftP{EpochLength: 7}},
		trans{25, wbftP{EpochLength: 7}},
		trans{40, wbftP{BlockPeriodSeconds: 2, ProposerPolicy: u64(1)}})
	sameBlock := base(10,
		trans{30, wbftP{EpochLength: 5}},
		trans{30, wbftP{EpochLength: 6}})
	all := []cfgCase{
		{"a04_example", "A-04 §4.2: epochLength 10; transitions {25, epochLength 7}, {40, blockPeriodSeconds 2, proposerPolicy 1}, {50, epochLength 7}", a04,
			[]uint64{0, 1, 9, 10, 20, 24, 25, 26, 31, 32, 39, 40, 45, 46, 49, 50, 56, 57, 64, 70}, []string{"WBFT-EPOCH-001", "WBFT-EPOCH-002", "WBFT-EPOCH-003", "WBFT-PARAM-054"}},
		{"unsorted_transitions", "the transitions of A-04 §4.2 in configuration order 50, 25, 40: they are sorted by block first", unsorted,
			[]uint64{25, 32, 50, 57}, []string{"WBFT-EPOCH-001", "@r06", "@r13"}},
		{"no_transitions_preset_8282", "epochLength 10, no transitions (preset 8282)", base(10), []uint64{0, 1, 10, 11, 99, 100, 18446744073709551610}, []string{"WBFT-EPOCH-001", "WBFT-EPOCH-002"}},
		{"no_transitions_preset_8283", "epochLength 140, no transitions (preset 8283)", base(140), []uint64{139, 140, 141, 14408500, 14408540}, []string{"WBFT-EPOCH-001", "WBFT-EPOCH-002"}},
		{"reanchor_same_length", "epochLength 4, {6, epochLength 4}: re-anchored at 6 with the same length", base(4, trans{6, wbftP{EpochLength: 4}}),
			[]uint64{4, 6, 8, 10, 12}, []string{"WBFT-EPOCH-003"}},
		{"transition_without_epoch_length", "epochLength 4, {6, blockPeriodSeconds 3}: a transition with epochLength 0 does not move the schedule", base(4, trans{6, wbftP{BlockPeriodSeconds: 3}}),
			[]uint64{6, 8, 12}, []string{"WBFT-EPOCH-003"}},
		{"transition_at_block_0", "epochLength 10, {0, epochLength 3}", base(10, trans{0, wbftP{EpochLength: 3}}),
			[]uint64{0, 3, 10, 12}, []string{"WBFT-EPOCH-001", "WBFT-EPOCH-003"}},
		{"same_block_transitions", "two transitions at block 30 with epochLength 5 and 6: applied in configuration order, so length 6 is in force", sameBlock,
			[]uint64{30, 35, 36}, []string{"@r06", "@r13"}},
	}
	var cs []Case
	for _, c := range all {
		cfg := wbftConfig(c.spec.chainConfigLite())
		e := wbftengine.NewEngine(cfg, common.Address{}, nil, nil)
		for _, n := range c.nums {
			is, last, err := e.IsEpochBlockNumber(nil, new(big.Int).SetUint64(n))
			must(err)
			cs = append(cs, Case{
				Runner: "validators", Handler: "epoch_boundary", Name: fmt.Sprintf("%s_n_%d", c.tag, n), Kind: "pure",
				Desc: c.desc, Reqs: c.reqs,
				Input:    M{{"config", c.spec.pureYAML()}, {"number", dec(n)}},
				Expected: M{{"is_epoch_block", is}, {"last_epoch_block", decBig(last)}},
			})
		}
	}
	return cs
}

// chainConfigLite is chainConfig for the pure handlers (anzeon.init is not
// used by them; the preset's init is kept).
func (s cfgSpec) chainConfigLite() *chainConfigT {
	if len(s.Init) == 0 {
		s.Init = []int{0}
	}
	return s.chainConfig()
}
