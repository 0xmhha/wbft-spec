// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Cross-check: Go slot readers vs. the embedded GovValidator v1 runtime bytecode.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/params"
	sc "github.com/ethereum/go-ethereum/systemcontracts"
)

const abiJSON = `[
{"name":"validatorList","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"address[]"}]},
{"name":"validatorToBlsKey","type":"function","stateMutability":"view","inputs":[{"type":"address"}],"outputs":[{"type":"bytes"}]},
{"name":"gasTip","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
{"name":"configureValidator","type":"function","stateMutability":"nonpayable","inputs":[{"type":"address"},{"type":"bytes"},{"type":"bytes"}],"outputs":[]}
]`

type cand struct {
	valKey, opKey *ecdsa.PrivateKey
	val, op       common.Address
	blsPub, pop   []byte
}

func newCand() *cand {
	vk, _ := crypto.GenerateKey()
	ok, _ := crypto.GenerateKey()
	c := &cand{valKey: vk, opKey: ok, val: crypto.PubkeyToAddress(vk.PublicKey), op: crypto.PubkeyToAddress(ok.PublicKey)}
	c.setBLS(vk)
	return c
}
func (c *cand) setBLS(k *ecdsa.PrivateKey) {
	sk, err := bls.DeriveFromECDSA(k)
	if err != nil {
		panic(err)
	}
	c.blsPub = sk.PublicKey().Marshal()
	c.pop = sk.Sign(c.blsPub).Marshal()
}

func main() {
	a, _ := abi.JSON(strings.NewReader(abiJSON))
	cands := []*cand{newCand(), newCand(), newCand(), newCand()}
	var ms, vs, ks []string
	for _, c := range cands {
		ms = append(ms, c.op.Hex())
		vs = append(vs, c.val.Hex())
		ks = append(ks, hexutil.Encode(c.blsPub))
	}
	gvAddr := params.DefaultGovValidatorAddress
	scs := &params.SystemContracts{GovValidator: &params.SystemContract{Address: gvAddr, Version: "v1", Params: map[string]string{
		"members": strings.Join(ms, ","), "quorum": "2", "expiry": "604800", "memberVersion": "1",
		"validators": strings.Join(vs, ","), "blsPublicKeys": strings.Join(ks, ","),
	}}}
	st, err := sc.GetSystemContractsTransition(scs, nil)
	if err != nil {
		panic(err)
	}
	sdb, _ := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	for _, c := range st.Codes {
		sdb.SetCode(c.Address, hexutil.MustDecode(c.Code))
	}
	for _, s := range st.States {
		sdb.SetState(s.Address, s.Key, s.Value)
	}
	cfg := params.StableNetTestnetChainConfig
	call := func(from common.Address, method string, args ...interface{}) ([]interface{}, error) {
		in, err := a.Pack(method, args...)
		if err != nil {
			panic(err)
		}
		ret, _, err := runtime.Call(gvAddr, in, &runtime.Config{ChainConfig: cfg, State: sdb, Origin: from, GasLimit: 10_000_000, BlockNumber: big.NewInt(1)})
		if err != nil {
			return nil, err
		}
		return a.Unpack(method, ret)
	}
	check := func(label string) {
		out, err := call(cands[0].op, "validatorList")
		if err != nil {
			panic(err)
		}
		evm := out[0].([]common.Address)
		goList := sc.ValidatorList(gvAddr, sdb)
		same := len(evm) == len(goList)
		for i := range evm {
			if same && evm[i] != goList[i] {
				same = false
			}
		}
		fmt.Printf("[%s] EVM validatorList == Go ValidatorList (same order): %v\n", label, same)
		for i, v := range goList {
			o, _ := call(cands[0].op, "validatorToBlsKey", v)
			evmKey := o[0].([]byte)
			goKey := sc.GetBLSPublicKey(gvAddr, sdb, v)
			fmt.Printf("   [%d] %s  blsEqual=%v len=%d\n", i, v.Hex(), bytes.Equal(evmKey, goKey), len(goKey))
		}
		g, _ := call(cands[0].op, "gasTip")
		fmt.Printf("   gasTip EVM=%v Go=%v\n", g[0], sc.GetGasTip(gvAddr, sdb))
	}
	for i, c := range cands {
		fmt.Printf("cand%d val=%s op=%s\n", i, c.val.Hex(), c.op.Hex())
	}
	check("genesis")

	// case 3: operator 0 changes validator address (swap-and-pop, then append)
	nv := newCand()
	if _, err := call(cands[0].op, "configureValidator", nv.val, nv.blsPub, nv.pop); err != nil {
		panic(fmt.Errorf("configureValidator case3: %w", err))
	}
	fmt.Printf("new validator for op0: %s\n", nv.val.Hex())
	check("after op0 changes validator address")

	// case 2: operator 1 changes BLS key only
	k2, _ := crypto.GenerateKey()
	old := append([]byte{}, cands[1].blsPub...)
	cands[1].setBLS(k2)
	if _, err := call(cands[1].op, "configureValidator", cands[1].val, cands[1].blsPub, cands[1].pop); err != nil {
		panic(fmt.Errorf("configureValidator case2: %w", err))
	}
	fmt.Printf("op1 BLS key changed; Go reader returns new key: %v\n", bytes.Equal(sc.GetBLSPublicKey(gvAddr, sdb, cands[1].val), cands[1].blsPub))
	oldSlot := crypto.Keccak256Hash(old, common.BigToHash(big.NewInt(0x38)).Bytes())
	fmt.Printf("old blsKeyToValidator slot cleared: %v\n", sdb.GetState(gvAddr, oldSlot) == (common.Hash{}))
	check("after op1 changes BLS key")

	// removed validator's residual slots
	v0 := cands[0].val
	idx := sc.CalculateMappingSlot(common.BigToHash(big.NewInt(0x34)), v0)
	bk := sc.CalculateMappingSlot(common.BigToHash(big.NewInt(0x37)), v0)
	fmt.Printf("removed v0: _indexes=%s blsKeyHead=%s blsData0=%s\n", sdb.GetState(gvAddr, idx).Hex(), sdb.GetState(gvAddr, bk).Hex(), sdb.GetState(gvAddr, sc.CalculateDynamicSlot(bk, big.NewInt(0))).Hex())
	arr7 := sc.CalculateDynamicSlot(common.BigToHash(big.NewInt(0x33)), big.NewInt(4))
	fmt.Printf("array slot index 4 after pop+push: %s ; length=%s\n", sdb.GetState(gvAddr, arr7).Hex(), sdb.GetState(gvAddr, common.BigToHash(big.NewInt(0x33))).Hex())
}
