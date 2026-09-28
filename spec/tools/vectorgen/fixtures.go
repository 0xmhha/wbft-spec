// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Shared fixtures: deterministic keys (A-11 WBFT-VEC-021) and the header H of
// A-02 §8.4, built with the reference types.

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const numKeys = 8

// keySeed is the documented seed of WBFT-VEC-021:
// key_i = keccak256(ASCII("wbft-spec-vector-key-" || decimal(i))),
// the same keys as A-02 §8.1 (i = 0..3).
func keySeed(i int) []byte {
	return crypto.Keccak256([]byte(fmt.Sprintf("wbft-spec-vector-key-%d", i)))
}

type fixture struct {
	keys     []*ecdsa.PrivateKey
	addrs    []common.Address
	blsKeys  []bls.SecretKey
	blsPubs  [][]byte
	H        *types.Header // A-02 §8.4 proposal header, chain 8282, block 2
	Hc       *types.Header // H after CommitHeader with seals of 0,1,2 at round 0
	H3       *types.Header // block 3: prev seals from Hc, committed at round 1, with epoch info
	Hbad     *types.Header // H with a trailing byte in Extra (undecodable)
	pSeals   []wbft.SealData
	cSeals   []wbft.SealData
	genExtra []byte
	ei       *types.EpochInfo
}

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

func ecdsaSign(k *ecdsa.PrivateKey, data []byte) []byte {
	s, err := crypto.Sign(crypto.Keccak256(data), k)
	must(err)
	return s
}

func randaoData(chainID, number *big.Int) []byte {
	var pre []byte
	pre = append(pre, chainID.Bytes()...)
	pre = append(pre, 0x01) // RANDAO_VERSION
	pre = append(pre, number.Bytes()...)
	return crypto.Keccak256(pre)
}

var fx = newFixture()

func newFixture() *fixture {
	f := &fixture{}
	for i := 0; i < numKeys; i++ {
		k, err := crypto.ToECDSA(keySeed(i))
		must(err)
		sk, err := bls.DeriveFromECDSA(k)
		must(err)
		f.keys = append(f.keys, k)
		f.addrs = append(f.addrs, crypto.PubkeyToAddress(k.PublicKey))
		f.blsKeys = append(f.blsKeys, sk)
		f.blsPubs = append(f.blsPubs, sk.PublicKey().Marshal())
	}

	// Epoch info and genesis extra for validators 0..3 (A-03 §9.4).
	f.ei = &types.EpochInfo{}
	for i := 0; i < 4; i++ {
		f.ei.Candidates = append(f.ei.Candidates, &types.Candidate{Addr: f.addrs[i], Diligence: types.DefaultDiligence})
		f.ei.Validators = append(f.ei.Validators, uint32(i))
		f.ei.BLSPublicKeys = append(f.ei.BLSPublicKeys, f.blsPubs[i])
	}
	f.genExtra = enc(&types.WBFTExtra{EpochInfo: f.ei, GasTip: new(big.Int).SetUint64(params.InitialGasTip)})

	// Header H (A-02 §8.4).
	chain := big.NewInt(8282)
	reveal2 := ecdsaSign(f.keys[0], randaoData(chain, big.NewInt(2)))
	extra := &types.WBFTExtra{
		VanityData:   make([]byte, 32),
		RandaoReveal: reveal2,
		GasTip:       new(big.Int).SetUint64(params.InitialGasTip),
	}
	f.H = &types.Header{
		ParentHash:  common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    f.addrs[0],
		Root:        common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(2),
		GasLimit:    105000000,
		Time:        1700000002,
		Extra:       enc(extra),
		BaseFee:     big.NewInt(20000000000000),
	}
	for i := 0; i < 3; i++ {
		ps := f.blsKeys[i].Sign(wbftcore.PrepareSeal(f.H, 0, wbftcore.SealTypePrepare)).Marshal()
		cs := f.blsKeys[i].Sign(wbftcore.PrepareSeal(f.H, 0, wbftcore.SealTypeCommit)).Marshal()
		f.pSeals = append(f.pSeals, wbft.SealData{Sealer: uint32(i), Seal: ps})
		f.cSeals = append(f.cSeals, wbft.SealData{Sealer: uint32(i), Seal: cs})
	}
	eng := wbftengine.NewEngine(&wbft.Config{}, common.Address{}, nil, nil)
	f.Hc = types.CopyHeader(f.H)
	must(eng.CommitHeader(f.Hc, f.pSeals, f.cSeals, big.NewInt(0)))

	// Block 3: carries H's aggregates as prev seals and an epoch info, then
	// is committed at round 1 by validators 1, 2, 3.
	hcExtra, err := types.ExtractWBFTExtra(f.Hc)
	must(err)
	extra3 := &types.WBFTExtra{
		VanityData:        []byte("wbft-vector"),
		RandaoReveal:      ecdsaSign(f.keys[1], randaoData(chain, big.NewInt(3))),
		PrevRound:         0,
		PrevPreparedSeal:  hcExtra.PreparedSeal,
		PrevCommittedSeal: hcExtra.CommittedSeal,
		Round:             1,
		GasTip:            big.NewInt(1),
		EpochInfo:         f.ei,
	}
	f.H3 = &types.Header{
		ParentHash:  f.Hc.Hash(),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    f.addrs[1],
		Root:        common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(3),
		GasLimit:    105000000,
		Time:        1700000003,
		Extra:       enc(extra3),
		MixDigest:   common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
		BaseFee:     big.NewInt(20000000000000),
	}
	var p3, c3 []wbft.SealData
	for i := 1; i < 4; i++ {
		p3 = append(p3, wbft.SealData{Sealer: uint32(i), Seal: f.blsKeys[i].Sign(wbftcore.PrepareSeal(f.H3, 1, wbftcore.SealTypePrepare)).Marshal()})
		c3 = append(c3, wbft.SealData{Sealer: uint32(i), Seal: f.blsKeys[i].Sign(wbftcore.PrepareSeal(f.H3, 1, wbftcore.SealTypeCommit)).Marshal()})
	}
	must(eng.CommitHeader(f.H3, p3, c3, big.NewInt(1)))

	f.Hbad = types.CopyHeader(f.H)
	f.Hbad.Extra = append(append([]byte{}, f.H.Extra...), 0x00)
	return f
}
