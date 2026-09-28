// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
)

func show(name string, g *core.Genesis, want string) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)
	_, hash, err := core.SetupGenesisBlock(db, tdb, g)
	if err != nil {
		fmt.Println(name, "err", err)
		return
	}
	blk := rawdb.ReadBlock(db, hash, 0)
	h := blk.Header()
	enc, _ := rlp.EncodeToBytes(h)
	plain := crypto.Keccak256Hash(enc)
	fmt.Printf("== %s\nhash=%s want=%s\nplainRlpHash=%s\n", name, hash.Hex(), want, plain.Hex())
	fmt.Printf("difficulty=%v gasLimit=%d baseFee=%v time=%d nonce=%x mix=%s coinbase=%s root=%s\n", h.Difficulty, h.GasLimit, h.BaseFee, h.Time, h.Nonce, h.MixDigest.Hex(), h.Coinbase.Hex(), h.Root.Hex())
	fmt.Printf("uncleHash=%s txHash=%s receiptHash=%s\n", h.UncleHash.Hex(), h.TxHash.Hex(), h.ReceiptHash.Hex())
	fmt.Printf("extra=%s\n", hexutil.Encode(h.Extra))
	ex, _ := types.ExtractWBFTExtra(h)
	fmt.Printf("extra.GasTip=%v epoch.validators=%v cand0=%v\n", ex.GasTip, ex.EpochInfo.Validators, ex.EpochInfo.Candidates[0])
	f := types.WBFTFilteredHeader(h)
	fmt.Printf("filteredExtraEqual=%v\n", hexutil.Encode(f.Extra) == hexutil.Encode(h.Extra))
}

func main() {
	show("mainnet", core.DefaultStableNetMainnetGenesisBlock(), params.StableNetMainnetGenesisHash.Hex())
	show("testnet", core.DefaultStableNetTestnetGenesisBlock(), params.StableNetTestnetGenesisHash.Hex())

	// minimal genesis: extra only
	cfg := params.StableNetTestnetChainConfig
	parent := &types.Header{Number: big.NewInt(0), GasLimit: 105_000_000, GasUsed: 0, BaseFee: new(big.Int).SetUint64(params.MinBaseFee)}
	for _, gu := range []uint64{0, 6_300_000, 6_299_999, 21_000_000, 21_000_001, 105_000_000} {
		parent.GasUsed = gu
		parent.Number = big.NewInt(5)
		fmt.Println("gasUsed", gu, "baseFee", eip1559.CalcBaseFee(cfg, parent))
	}
	parent.BaseFee = big.NewInt(30_000_000_000_000)
	for _, gu := range []uint64{0, 21_000_001} {
		parent.GasUsed = gu
		fmt.Println("pbf 3e13 gasUsed", gu, "baseFee", eip1559.CalcBaseFee(cfg, parent))
	}
	parent.BaseFee = new(big.Int).SetUint64(params.MaxBaseFee - 1)
	parent.GasUsed = 105_000_000
	fmt.Println("near max ->", eip1559.CalcBaseFee(cfg, parent))
}
