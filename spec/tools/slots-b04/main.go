// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/systemcontracts"
	"github.com/ethereum/go-ethereum/crypto"
)

type mapState map[common.Address]map[common.Hash]common.Hash

func (m mapState) GetState(a common.Address, k common.Hash) common.Hash { return m[a][k] }

type bw struct{ b []byte }

func (w *bw) Bytes() []byte { return w.b }

func main() {
	cfg := params.StableNetTestnetChainConfig
	st, err := systemcontracts.GetSystemContractsTransition(cfg.Anzeon.SystemContracts, nil)
	if err != nil {
		panic(err)
	}
	gv := cfg.Anzeon.SystemContracts.GovValidator.Address
	ms := mapState{}
	for _, s := range st.States {
		if ms[s.Address] == nil {
			ms[s.Address] = map[common.Hash]common.Hash{}
		}
		ms[s.Address][s.Key] = s.Value
	}
	// dump GovValidator storage sorted
	keys := []common.Hash{}
	for k := range ms[gv] {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Big().Cmp(keys[j].Big()) < 0 })
	fmt.Println("GovValidator genesis storage entries:", len(keys))
	for _, k := range keys {
		fmt.Printf("  %s = %s\n", k.Hex(), ms[gv][k].Hex())
	}
	fmt.Println("ValidatorList:")
	for i, a := range systemcontracts.ValidatorList(gv, ms) {
		pk := systemcontracts.GetBLSPublicKey(gv, ms, a)
		fmt.Printf("  [%d] %s bls=%x\n", i, a.Hex(), pk)
	}
	fmt.Println("gasTip:", systemcontracts.GetGasTip(gv, ms))

	// slot derivations
	h := func(n int64) common.Hash { return common.BigToHash(big.NewInt(n)) }
	fmt.Println("keccak(0x33) =", crypto.Keccak256Hash(h(0x33).Bytes()).Hex())
	v0 := common.HexToAddress("0x9f06600b2c17108662e3840e76bb27c9468eb73d")
	fmt.Println("_indexes[v0] slot =", systemcontracts.CalculateMappingSlot(h(0x34), v0).Hex())
	blsSlot := systemcontracts.CalculateMappingSlot(h(0x37), v0)
	fmt.Println("validatorToBlsKey[v0] slot =", blsSlot.Hex())
	fmt.Println("  data slot0 =", systemcontracts.CalculateDynamicSlot(blsSlot, big.NewInt(0)).Hex())
	fmt.Println("  data slot1 =", systemcontracts.CalculateDynamicSlot(blsSlot, big.NewInt(1)).Hex())
	pk := common.FromHex("0x96683524c3b7e224f2146a0dbb87593e3dee21b7d97c1b24ef6ed799c977be40d3dff8e45b7fcdb48f7713095d84dd9c")
	fmt.Println("blsKeyToValidator[pk0] slot =", systemcontracts.CalculateMappingSlot(h(0x38), &bw{pk}).Hex())
	fmt.Println("   check keccak(pk||0x38) =", crypto.Keccak256Hash(pk, h(0x38).Bytes()).Hex())
	fmt.Println("validatorToOperator[v0] slot =", systemcontracts.CalculateMappingSlot(h(0x35), v0).Hex())
	m0 := common.HexToAddress("0x58f13FE4294652526C4B893b4e4f343B3F184D43")
	fmt.Println("operatorToValidator[m0] slot =", systemcontracts.CalculateMappingSlot(h(0x36), m0).Hex())
	fmt.Println("members[m0] slot =", systemcontracts.CalculateMappingSlot(h(5), m0).Hex())
	fmt.Println("versionedMemberList[1] len slot =", systemcontracts.CalculateMappingSlot(h(6), h(1)).Hex())
	// short bytes example
	fmt.Println("short bytes 'WKRC' =", systemcontracts.VarLenBytesToMultipleHash([]byte("WKRC"))[0].Hex())
	// mainnet
	mc := params.StableNetMainnetChainConfig
	st2, _ := systemcontracts.GetSystemContractsTransition(mc.Anzeon.SystemContracts, nil)
	ms2 := mapState{}
	for _, s := range st2.States {
		if ms2[s.Address] == nil {
			ms2[s.Address] = map[common.Hash]common.Hash{}
		}
		ms2[s.Address][s.Key] = s.Value
	}
	fmt.Println("mainnet ValidatorList:", systemcontracts.ValidatorList(gv, ms2), "gasTip", systemcontracts.GetGasTip(gv, ms2))
	// missing contract
	fmt.Println("empty state: list len", len(systemcontracts.ValidatorList(gv, mapState{})), "gasTip", systemcontracts.GetGasTip(gv, mapState{}), "bls len", len(systemcontracts.GetBLSPublicKey(gv, mapState{}, v0)))
}
