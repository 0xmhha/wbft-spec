// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `source`, handler `candidates_at_epoch` (B-08 §2, §4, §6.3): the
// candidate list and BLS keys that the epoch step reads from GovValidator
// storage, and the gas tip slot. The state is part of the input: the
// GovValidator account with its storage. The storage of every case is written
// by the reference initializer (systemcontracts.GetSystemContractsTransition
// with GovValidator parameters, the path of a genesis), possibly followed by
// one crafted change that the case names. The expected values come from
// Engine.GetGovCandidates, systemcontracts.GetBLSPublicKey at the address that
// Engine uses (wbft.Config.GetSystemContracts) and systemcontracts.GetGasTip at
// the genesis GovValidator address.

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/systemcontracts"
)

// gvParams returns GovValidator parameters with the given validators (account
// indices, or explicit addresses in extra), their BLS keys and members.
func gvParams(vals []common.Address, keys [][]byte, gasTip string) map[string]string {
	var v, k, m []string
	for i, a := range vals {
		v = append(v, a.Hex())
		k = append(k, "0x"+common.Bytes2Hex(keys[i]))
		// the member (operator) of validator i: a deterministic address
		m = append(m, common.BytesToAddress(append([]byte("wbft-vector-operator"), byte(i))).Hex())
	}
	quorum := "1"
	if len(vals) > 1 {
		quorum = "2" // the initializer requires at least 2 for more than one member
	}
	p := map[string]string{
		"quorum": quorum, "expiry": "604800", "memberVersion": "1", "maxProposals": "3",
		"members": strings.Join(m, ","), "validators": strings.Join(v, ","), "blsPublicKeys": strings.Join(k, ","),
	}
	if gasTip != "" {
		p["gasTip"] = gasTip
	}
	return p
}

// gvStorage runs the reference GovValidator initializer and returns the
// storage it writes, sorted by key.
func gvStorage(addr common.Address, p map[string]string) []slot {
	st, err := systemcontracts.GetSystemContractsTransition(&params.SystemContracts{GovValidator: &params.SystemContract{Address: addr, Version: "v1", Params: p}}, nil)
	must(err)
	m := map[common.Hash]common.Hash{}
	for _, s := range st.States {
		if s.Address != addr {
			panic("initializer wrote another address")
		}
		m[s.Key] = s.Value
	}
	var out []slot
	for k, v := range m {
		out = append(out, slot{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key.Cmp(out[j].key) < 0 })
	return out
}

// withoutKey removes the storage of validatorToBlsKey[val] (the length slot
// of the bytes and its data slots): the crafted state of V-SRC-008.
func withoutKey(st []slot, val common.Address) []slot {
	base := systemcontracts.CalculateMappingSlot(common.HexToHash(systemcontracts.SLOT_VALIDATOR_validatorToBlsKey), val)
	drop := map[common.Hash]bool{base: true}
	for i := int64(0); i < 8; i++ {
		drop[systemcontracts.CalculateDynamicSlot(base, big.NewInt(i))] = true
	}
	var out []slot
	for _, s := range st {
		if !drop[s.key] {
			out = append(out, s)
		}
	}
	return out
}

func keysOf(idx []int) ([]common.Address, [][]byte) {
	var a []common.Address
	var k [][]byte
	for _, i := range idx {
		a = append(a, accts[i].addr)
		k = append(k, accts[i].bls.PublicKey().Marshal())
	}
	return a, k
}

func candidatesCases() []Case {
	type in struct {
		name, desc string
		reqs       []string
		cfg        execCfg
		number     uint64
		accounts   []acct
	}
	gv := params.DefaultGovValidatorAddress
	gvAcct := func(st []slot) acct { return acct{addr: gv, balance: new(big.Int), storage: st} }
	// the preset genesis parameters of GovValidator
	preset := func(cc *params.ChainConfig) []slot {
		return gvStorage(cc.Anzeon.SystemContracts.GovValidator.Address, cc.Anzeon.SystemContracts.GovValidator.Params)
	}
	a4, k4 := keysOf([]int{0, 1, 2, 3})
	dupA := []common.Address{a4[0], a4[1], a4[0], a4[2]}
	dupK := [][]byte{k4[0], k4[1], k4[3], k4[2]}
	var a13 []common.Address
	var k13 [][]byte
	for i := 0; i < 13; i++ {
		a, k := keysOf([]int{i})
		a13 = append(a13, a[0])
		k13 = append(k13, k[0])
	}
	shortKey := [][]byte{k4[0], []byte("not-a-bls-key-20byte"), k4[2], k4[3]}
	base := []string{"SNET-SRC-006", "@r51", "@r52", "SNET-SYS-040", "SNET-SYS-061", "SNET-SYS-062", "@r29"}
	r := func(ids ...string) []string { return append(append([]string{}, base...), ids...) }
	testnet := execCfg{Preset: "8283"}
	ins := []in{
		{"testnet_genesis", "B-08 V-SRC-001: the GovValidator storage that the testnet preset genesis writes: the 7 testnet validators in parameter order, their keys, gas tip 27 600 gwei", r(), testnet, 10, []acct{gvAcct(preset(testnet.chainConfig()))}},
		{"mainnet_genesis", "the GovValidator storage that the mainnet preset genesis writes: one validator", r(), mainnet, 10, []acct{gvAcct(preset(mainnet.chainConfig()))}},
		{"four_validators", "GovValidator parameters with the validators of keys 0..3 in this order and their BLS keys, gas tip 1 000 gwei", r(), mainnet, 10, []acct{gvAcct(gvStorage(gv, gvParams(a4, k4, "1000000000000")))}},
		{"repeated_validator_parameter", "B-08 V-SRC-007: the validators parameter lists key 0 twice (positions 0 and 2): the initializer keeps the first occurrence only, so the list is keys 0, 1, 2", r(), mainnet, 10, []acct{gvAcct(gvStorage(gv, gvParams(dupA, dupK, "")))}},
		{"thirteen_candidates", "13 validators (keys 0..12): the list keeps the parameter order, which the tie order of A-04 {@r10} depends on", r(), mainnet, 10, []acct{gvAcct(gvStorage(gv, gvParams(a13, k13, "")))}},
		{"validator_without_key", "B-08 V-SRC-008: the four validators of keys 0..3, then the storage of the BLS key of key 2 is removed (a crafted state): key 2 stays a candidate and its key is empty", r(), mainnet, 10, []acct{gvAcct(withoutKey(gvStorage(gv, gvParams(a4, k4, "")), a4[2]))}},
		{"key_not_checked", "a BLS key parameter of 20 bytes for key 1: the reader returns the stored bytes unmodified (no length or point check when the epoch information is built)", r(), mainnet, 10, []acct{gvAcct(gvStorage(gv, gvParams(a4, shortKey, "")))}},
	}
	var out []Case
	for _, x := range ins {
		cc := x.cfg.chainConfig()
		cfg := wbftConfig(cc)
		e := wbftengine.NewEngine(cfg, common.Address{}, nil, nil)
		sdb := memState(x.accounts)
		n := new(big.Int).SetUint64(x.number)
		addr := cfg.GetSystemContracts(n, cc).GovValidator.Address
		list := []any{}
		for _, a := range e.GetGovCandidates(cc, sdb, n) {
			list = append(list, M{{"address", hexs(a.Bytes())}, {"bls_public_key", hexs(systemcontracts.GetBLSPublicKey(addr, sdb, a))}})
		}
		tip := systemcontracts.GetGasTip(cc.Anzeon.SystemContracts.GovValidator.Address, sdb)
		out = append(out, Case{Runner: "source", Handler: "candidates_at_epoch", Name: x.name, Kind: "pure",
			Desc: x.desc + fmt.Sprintf("; the reader is Engine.GetGovCandidates and systemcontracts.GetBLSPublicKey at the GovValidator address in force at block %d, and systemcontracts.GetGasTip at the genesis GovValidator address", x.number),
			Reqs: x.reqs,
			Input: M{{"config", x.cfg.yaml()}, {"number", dec(x.number)}, {"accounts", acctsYAML(x.accounts)}},
			Expected: M{{"candidates", list}, {"gas_tip", decBig(tip)}}})
	}
	return out
}
