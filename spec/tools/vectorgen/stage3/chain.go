// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `chain`, handlers `fork_schedule` (B-01 §3, §7, §11.1) and `genesis`
// (B-02). Both call exported reference functions in this process: the fork
// predicates and `Rules` of params.ChainConfig, the system-contract resolution
// of wbft.Config.GetSystemContracts after eth/ethconfig.SetConfigFromChainConfig,
// forkid.NewID, and core.SetupGenesisBlock on an empty in-memory database.

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"

	"vectorgen/internal/vecfile"
)

func dec(v uint64) vecfile.Dec      { return vecfile.DecU(v) }
func decBig(v *big.Int) vecfile.Dec { return vecfile.DecBig(v) }
func hexs(b []byte) vecfile.Hex     { return vecfile.Hexs(b) }
func u64(v uint64) *uint64          { return &v }

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func optDec(p *uint64) any {
	if p == nil {
		return nil
	}
	return dec(*p)
}

func optBig(p *big.Int) any {
	if p == nil {
		return nil
	}
	return decBig(p)
}

func presetConfig(preset string) *params.ChainConfig {
	switch preset {
	case "8282":
		return params.StableNetMainnetChainConfig
	case "8283":
		return params.StableNetTestnetChainConfig
	}
	panic("unknown preset " + preset)
}

func presetGenesis(preset string) *core.Genesis {
	switch preset {
	case "8282":
		return core.DefaultStableNetMainnetGenesisBlock()
	case "8283":
		return core.DefaultStableNetTestnetGenesisBlock()
	}
	panic("unknown preset " + preset)
}

// ------------------------------------------------------------ fork_schedule

// forkOv replaces fork fields of a preset; a nil field keeps the preset value.
type forkOv struct {
	Applepie, Boho           *uint64
	ShanghaiTime, CancunTime *uint64
}

func (o forkOv) yaml() M {
	return M{{"applepie_block", optDec(o.Applepie)}, {"boho_block", optDec(o.Boho)}, {"shanghai_time", optDec(o.ShanghaiTime)}, {"cancun_time", optDec(o.CancunTime)}}
}

// chainConfig returns a copy of the preset with the overrides applied (the
// preset itself is not modified).
func (o forkOv) chainConfig(preset string) *params.ChainConfig {
	cc := *presetConfig(preset)
	if o.Applepie != nil {
		cc.ApplepieBlock = new(big.Int).SetUint64(*o.Applepie)
	}
	if o.Boho != nil {
		cc.BohoBlock = new(big.Int).SetUint64(*o.Boho)
	}
	if o.ShanghaiTime != nil {
		cc.ShanghaiTime = u64(*o.ShanghaiTime)
	}
	if o.CancunTime != nil {
		cc.CancunTime = u64(*o.CancunTime)
	}
	return &cc
}

func scYAML(name string, sc *params.SystemContract) M {
	if sc == nil {
		return M{{"name", name}, {"address", nil}, {"version", nil}}
	}
	return M{{"name", name}, {"address", hexs(sc.Address.Bytes())}, {"version", sc.Version}}
}

func scList(s params.SystemContracts) []any {
	return []any{
		scYAML("gov_validator", s.GovValidator),
		scYAML("native_coin_adapter", s.NativeCoinAdapter),
		scYAML("gov_minter", s.GovMinter),
		scYAML("gov_master_minter", s.GovMasterMinter),
		scYAML("gov_council", s.GovCouncil),
	}
}

// genesisBlock builds the genesis block of the configuration as a node does
// (core.SetupGenesisBlock on an empty database) from the preset genesis
// specification with the given configuration.
func genesisBlock(preset string, cc *params.ChainConfig) *types.Block {
	g := presetGenesis(preset)
	g.Config = cc
	db := rawdb.NewMemoryDatabase()
	_, hash, err := core.SetupGenesisBlock(db, triedb.NewDatabase(db, nil), g)
	must(err)
	return rawdb.ReadBlock(db, hash, 0)
}

func forkScheduleCases() []Case {
	type fc struct {
		name, desc string
		preset     string
		ov         forkOv
		points     [][2]uint64 // (number, time)
		reqs       []string
	}
	base := []string{"SNET-CFG-002", "SNET-CFG-003", "SNET-CFG-004", "SNET-CFG-005", "SNET-CFG-006", "SNET-CFG-018", "SNET-CFG-019"}
	all := []fc{
		{"preset_8282", "preset 8282: every fork at block 0, Boho at 0, no timestamp fork", "8282", forkOv{}, [][2]uint64{{0, 0}, {1, 1}, {1000000, 1800000000}}, []string{"SNET-CFG-032"}},
		{"preset_8283", "preset 8283: Boho at 14408500 (GovMinter v2 from that block)", "8283", forkOv{}, [][2]uint64{{0, 0}, {14408499, 0}, {14408500, 0}, {14408501, 0}}, []string{"SNET-CFG-032", "SNET-CFG-010", "SNET-CFG-011"}},
		{"testnet_boho_at_0", "preset 8283 with bohoBlock 0: the Boho upgrade is in force from genesis", "8283", forkOv{Boho: u64(0)}, [][2]uint64{{0, 0}, {1, 0}}, []string{"SNET-CFG-011", "SNET-CFG-021"}},
		{"testnet_boho_at_100", "preset 8283 with bohoBlock 100", "8283", forkOv{Boho: u64(100)}, [][2]uint64{{99, 0}, {100, 0}}, []string{"SNET-CFG-011"}},
		{"applepie_at_5", "preset 8283 with applepieBlock 5 (fee delegation from block 5)", "8283", forkOv{Applepie: u64(5)}, [][2]uint64{{4, 0}, {5, 0}}, []string{"SNET-CFG-009"}},
		{"shanghai_cancun_times", "preset 8282 with shanghaiTime 1000 and cancunTime 2000: the predicates follow the time, the rule flags of a WBFT block stay false (is_merge is false)", "8282", forkOv{ShanghaiTime: u64(1000), CancunTime: u64(2000)}, [][2]uint64{{10, 999}, {10, 1000}, {10, 2000}}, []string{"SNET-CFG-029"}},
	}
	var cs []Case
	for _, x := range all {
		cc := x.ov.chainConfig(x.preset)
		cfg := &wbft.Config{}
		must(ethconfig.SetConfigFromChainConfig(cfg, cc))
		gen := genesisBlock(x.preset, cc)
		for _, pt := range x.points {
			n, t := new(big.Int).SetUint64(pt[0]), pt[1]
			r := cc.Rules(n, false, t) // WBFT block: Difficulty 1, so is_merge is false (SNET-CFG-006)
			forks := M{
				{"homestead", cc.IsHomestead(n)}, {"dao_fork", cc.IsDAOFork(n)}, {"eip150", cc.IsEIP150(n)}, {"eip155", cc.IsEIP155(n)}, {"eip158", cc.IsEIP158(n)},
				{"byzantium", cc.IsByzantium(n)}, {"constantinople", cc.IsConstantinople(n)}, {"petersburg", cc.IsPetersburg(n)}, {"istanbul", cc.IsIstanbul(n)},
				{"muir_glacier", cc.IsMuirGlacier(n)}, {"berlin", cc.IsBerlin(n)}, {"london", cc.IsLondon(n)}, {"arrow_glacier", cc.IsArrowGlacier(n)},
				{"gray_glacier", cc.IsGrayGlacier(n)}, {"applepie", cc.IsApplepie(n)}, {"boho", cc.IsBoho(n)},
				{"shanghai", cc.IsShanghai(n, t)}, {"cancun", cc.IsCancun(n, t)}, {"prague", cc.IsPrague(n, t)}, {"verkle", cc.IsVerkle(n, t)},
				{"anzeon", cc.AnzeonEnabled()},
			}
			rules := M{{"is_anzeon", r.IsAnzeon}, {"is_applepie", r.IsApplepie}, {"is_boho", r.IsBoho}, {"is_merge", r.IsMerge},
				{"is_shanghai", r.IsShanghai}, {"is_cancun", r.IsCancun}, {"is_prague", r.IsPrague}, {"is_verkle", r.IsVerkle}}
			ups := []any{}
			for _, u := range cfg.SystemContractUpgrades {
				if u.Block.Cmp(n) == 0 {
					for _, e := range scList(*u.SystemContracts) {
						if e.(M)[1].V != nil {
							ups = append(ups, e)
						}
					}
				}
			}
			id := forkid.NewID(cc, gen, pt[0], t)
			cs = append(cs, Case{Runner: "chain", Handler: "fork_schedule", Name: fmt.Sprintf("%s_n%d_t%d", x.name, pt[0], t), Kind: "pure",
				Desc: x.desc + fmt.Sprintf("; block %d, time %d", pt[0], t),
				Reqs: append(append([]string{}, base...), x.reqs...),
				Input: M{
					{"config", M{{"preset", x.preset}, {"overrides", x.ov.yaml()}}},
					{"number", dec(pt[0])},
					{"time", dec(t)},
				},
				Expected: M{
					{"forks", forks},
					{"rules", rules},
					{"system_contracts", scList(cfg.GetSystemContracts(n, cc))},
					{"upgrades_at", ups},
					{"fork_id", M{{"hash", hexs(id.Hash[:])}, {"next", dec(id.Next)}}},
				}})
		}
	}
	return cs
}

// ------------------------------------------------------------------ genesis

// genOv replaces fields of a preset genesis specification (B-02 §1); nil keeps the preset value.
type genOv struct {
	Timestamp, GasLimit, Number *uint64
	Difficulty                  *big.Int
	ExtraData                   []byte
	Boho                        *uint64
	Alloc                       []allocAdd
}

type allocAdd struct {
	addr    common.Address
	balance *big.Int
}

func (o genOv) yaml() M {
	var extra any
	if o.ExtraData != nil {
		extra = hexs(o.ExtraData)
	}
	al := []any{}
	for _, a := range o.Alloc {
		al = append(al, M{{"address", hexs(a.addr.Bytes())}, {"balance", decBig(a.balance)}})
	}
	return M{{"timestamp", optDec(o.Timestamp)}, {"gas_limit", optDec(o.GasLimit)}, {"difficulty", optBig(o.Difficulty)},
		{"number", optDec(o.Number)}, {"extra_data", extra}, {"boho_block", optDec(o.Boho)}, {"alloc_add", al}}
}

func (o genOv) apply(preset string) *core.Genesis {
	g := presetGenesis(preset)
	cc := *presetConfig(preset)
	if o.Boho != nil {
		cc.BohoBlock = new(big.Int).SetUint64(*o.Boho)
	}
	g.Config = &cc
	if o.Timestamp != nil {
		g.Timestamp = *o.Timestamp
	}
	if o.GasLimit != nil {
		g.GasLimit = *o.GasLimit
	}
	if o.Number != nil {
		g.Number = *o.Number
	}
	if o.Difficulty != nil {
		g.Difficulty = o.Difficulty
	}
	if o.ExtraData != nil {
		g.ExtraData = o.ExtraData
	}
	if len(o.Alloc) > 0 {
		al := types.GenesisAlloc{}
		for a, acc := range g.Alloc {
			al[a] = acc
		}
		for _, a := range o.Alloc {
			al[a.addr] = types.Account{Balance: a.balance}
		}
		g.Alloc = al
	}
	return g
}

func genesisCases() []Case {
	type gc struct {
		name, desc string
		preset     string
		ov         genOv
		reqs       []string
	}
	base := []string{"SNET-GEN-001", "SNET-GEN-002", "SNET-GEN-003", "SNET-GEN-005", "SNET-GEN-006", "SNET-GEN-007", "SNET-GEN-016", "SNET-GEN-017"}
	e18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	all := []gc{
		{"preset_8282", "preset 8282 genesis (B-02 §8.1); its hash equals the constant of B-01 §11.1", "8282", genOv{}, []string{"SNET-GEN-018"}},
		{"preset_8283", "preset 8283 genesis (B-02 §8.2): difficulty 0, so the plain header hash; gas limit 0 becomes 4712388", "8283", genOv{}, []string{"SNET-GEN-018"}},
		{"extra_data_ignored", "preset 8282 with extraData 0x1234: the input extra is replaced by the WBFTExtra, same genesis as the preset", "8282", genOv{ExtraData: []byte{0x12, 0x34}}, nil},
		{"timestamp_gas_limit", "preset 8282 with timestamp 1700000000 and gasLimit 30000000", "8282", genOv{Timestamp: u64(1_700_000_000), GasLimit: u64(30_000_000)}, nil},
		{"testnet_difficulty_1", "preset 8283 with difficulty 1: the hash rule is selected by Difficulty == 1 and gives the plain header hash because the genesis extra is canonical", "8283", genOv{Difficulty: big.NewInt(1)}, nil},
		{"difficulty_2", "preset 8282 with difficulty 2 (the genesis difficulty is not forced to 1)", "8282", genOv{Difficulty: big.NewInt(2)}, nil},
		{"testnet_boho_at_0", "preset 8283 with bohoBlock 0: GovMinter v2 bytecode at genesis changes the state root", "8283", genOv{Boho: u64(0)}, []string{"SNET-CFG-021"}},
		{"mainnet_funded_account", "preset 8282 with one funded account (10^18 wei): the NativeCoinAdapter total supply follows the allocation", "8282", genOv{Alloc: []allocAdd{{common.HexToAddress("0x00000000000000000000000000000000000000aa"), e18}}}, []string{"SNET-GEN-011"}},
		{"testnet_funded_account", "preset 8283 with one more funded account (5 * 10^18 wei)", "8283", genOv{Alloc: []allocAdd{{common.HexToAddress("0x00000000000000000000000000000000000000bb"), new(big.Int).Mul(big.NewInt(5), e18)}}}, []string{"SNET-GEN-011"}},
		{"number_not_zero", "preset 8282 with number 1: the genesis block cannot be committed", "8282", genOv{Number: u64(1)}, []string{"SNET-GEN-004"}},
	}
	var cs []Case
	for _, x := range all {
		db := rawdb.NewMemoryDatabase()
		_, hash, err := core.SetupGenesisBlock(db, triedb.NewDatabase(db, nil), x.ov.apply(x.preset))
		c := Case{Runner: "chain", Handler: "genesis", Name: x.name, Kind: "pure", Desc: x.desc,
			Reqs:  append(append([]string{}, base...), x.reqs...),
			Input: M{{"genesis", M{{"preset", x.preset}, {"overrides", x.ov.yaml()}}}}}
		if x.name == "number_not_zero" {
			c.Reqs = x.reqs
		}
		if err != nil {
			c.Err = err.Error()
		} else {
			h := rawdb.ReadBlock(db, hash, 0).Header()
			hb, err := rlp.EncodeToBytes(h)
			must(err)
			c.Expected = M{{"header", hexs(hb)}, {"hash", hexs(hash.Bytes())}, {"state_root", hexs(h.Root.Bytes())}, {"extra", hexs(h.Extra)}}
		}
		cs = append(cs, c)
	}
	return cs
}
