// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `chain`, handler `config_at` (A-01 §6.5, B-01 §6.2): the consensus
// configuration governing a height, built by eth/ethconfig.SetConfigFromChainConfig
// (which sorts the transitions) and wbft.Config.GetConfig.

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func configAtCases() []Case {
	type cc struct {
		tag, desc string
		spec      cfgSpec
		nums      []uint64
		reqs      []string
	}
	base := []string{"WBFT-PARAM-050", "SNET-CFG-015"}
	a01 := cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(0)}, T: []trans{
		{200, wbftP{BlockPeriodSeconds: 2, MaxRequestTimeoutSeconds: u64(30)}},
		{100, wbftP{EpochLength: 20, RequestTimeoutSeconds: 0, ProposerPolicy: u64(1)}},
		{300, wbftP{MaxRequestTimeoutSeconds: u64(0)}},
	}}
	fromPreset := func(p *params.ChainConfig) cfgSpec {
		w := p.Anzeon.WBFT
		return cfgSpec{W: wbftP{RequestTimeoutSeconds: w.RequestTimeoutSeconds, BlockPeriodSeconds: w.BlockPeriodSeconds, EpochLength: w.EpochLength,
			AllowedFutureBlockTime: w.AllowedFutureBlockTime, ProposerPolicy: w.ProposerPolicy, MaxRequestTimeoutSeconds: w.MaxRequestTimeoutSeconds}}
	}
	all := []cc{
		{"a01_example", "A-01 §6.6: transitions given unsorted (200, 100, 300); a 0 requestTimeoutSeconds does not override, an explicit maxRequestTimeoutSeconds 0 does", a01,
			[]uint64{0, 99, 100, 199, 200, 299, 300, 1000000}, append(base, "@r13", "WBFT-PARAM-051")},
		{"preset_8282", "genesis section of preset 8282 (B-01 §11.1), no transitions", fromPreset(params.StableNetMainnetChainConfig), []uint64{0, 1, 1000}, append(base, "WBFT-PARAM-030", "SNET-CFG-014")},
		{"preset_8283", "genesis section of preset 8283 (B-01 §11.1), no transitions", fromPreset(params.StableNetTestnetChainConfig), []uint64{0, 14408500}, append(base, "WBFT-PARAM-030", "SNET-CFG-014")},
		{"request_timeout_wraps", "requestTimeoutSeconds 18446744073709552 in the genesis section and 18446744073710 in a transition at 5: the product with 1000 is taken modulo 2^64", cfgSpec{W: wbftP{RequestTimeoutSeconds: 18446744073709552, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(0)}, T: []trans{{5, wbftP{RequestTimeoutSeconds: 18446744073710}}}},
			[]uint64{4, 5}, append(base, "WBFT-PARAM-052", "SNET-CFG-014")},
		{"policy_other_id", "a transition with proposerPolicy 7 (an identifier other than 0 and 1 selects RoundRobin; config_at keeps the identifier)", cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(1)}, T: []trans{{3, wbftP{ProposerPolicy: u64(7)}}}},
			[]uint64{2, 3}, append(base, "WBFT-PARAM-020", "WBFT-PARAM-051")},
		{"policy_explicit_zero", "a transition with proposerPolicy 0 overrides Sticky (a present pointer field overrides even when 0)", cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(1)}, T: []trans{{3, wbftP{ProposerPolicy: u64(0)}}}},
			[]uint64{2, 3}, append(base, "WBFT-PARAM-051")},
		{"allowed_future_block_time_ignored", "allowedFutureBlockTime 5 in the genesis section; a transition at 2 that sets it to 30 (and blockPeriodSeconds 2) does not change it", cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, AllowedFutureBlockTime: 5, ProposerPolicy: u64(0)}, T: []trans{{2, wbftP{AllowedFutureBlockTime: 30, BlockPeriodSeconds: 2}}}},
			[]uint64{1, 2}, append(base, "WBFT-PARAM-033", "WBFT-TIMER-004")},
		{"max_request_timeout_in_genesis", "maxRequestTimeoutSeconds 10 in the genesis section, removed by an explicit 0 at block 8 and set to 60 at block 9", cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(0), MaxRequestTimeoutSeconds: u64(10)}, T: []trans{{9, wbftP{MaxRequestTimeoutSeconds: u64(60)}}, {8, wbftP{MaxRequestTimeoutSeconds: u64(0)}}}},
			[]uint64{7, 8, 9}, append(base, "WBFT-PARAM-051", "WBFT-TIMER-003")},
		{"same_block_two", "two transitions at block 4 that set requestTimeoutSeconds 3 and 5: they are applied in configuration order, so 5 wins", cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(0)}, T: []trans{{4, wbftP{RequestTimeoutSeconds: 3}}, {4, wbftP{RequestTimeoutSeconds: 5}}}},
			[]uint64{3, 4}, append(base, "@r13", "@r01")},
		{"same_block_twelve", "12 transitions (blocks alternate 20 and 10, each sets a distinct requestTimeoutSeconds): ties keep configuration order", tieSpec(12, 0),
			[]uint64{10, 20}, append(base, "@r13", "@r01")},
	}
	var cs []Case
	for _, c := range all {
		cfg := wbftConfig(c.spec.chainConfigLite())
		for _, n := range c.nums {
			g := cfg.GetConfig(new(big.Int).SetUint64(n))
			var pol any
			if g.ProposerPolicy != nil {
				pol = dec(uint64(g.ProposerPolicy.Id))
			}
			cs = append(cs, Case{
				Runner: "chain", Handler: "config_at", Name: fmt.Sprintf("%s_n_%d", c.tag, n), Kind: "pure",
				Desc: c.desc, Reqs: c.reqs,
				Input: M{{"config", c.spec.pureYAML()}, {"number", dec(n)}},
				Expected: M{
					{"request_timeout", dec(g.RequestTimeout)},
					{"block_period", dec(g.BlockPeriod)},
					{"epoch", dec(g.Epoch)},
					{"proposer_policy", pol},
					{"max_request_timeout_seconds", dec(g.MaxRequestTimeoutSeconds)},
					{"allowed_future_block_time", dec(g.AllowedFutureBlockTime)},
				},
			})
		}
	}
	return cs
}

// tieSpec returns n transitions whose blocks are 10 or 20 (pattern from a
// keccak stream seeded by `seed`), transition i setting requestTimeoutSeconds
// 100+i and epochLength 2+i.
func tieSpec(n int, seed uint64) cfgSpec {
	s := cfgSpec{W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 10, ProposerPolicy: u64(0)}}
	stream := crypto.Keccak256([]byte(fmt.Sprintf("wbft-vector-ties-%d-%d", n, seed)))
	for i := 0; i < n; i++ {
		if i%32 == 0 && i > 0 {
			stream = crypto.Keccak256(stream)
		}
		blk := uint64(10)
		if seed == 0 && n <= 12 {
			if i%2 == 0 {
				blk = 20
			}
		} else if stream[i%32]&1 == 1 {
			blk = 20
		}
		s.T = append(s.T, trans{blk, wbftP{RequestTimeoutSeconds: 100 + uint64(i), EpochLength: 2 + uint64(i)}})
	}
	return s
}

