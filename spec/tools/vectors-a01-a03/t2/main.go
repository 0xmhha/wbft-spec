// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

import (
	"fmt"
	"math/big"

	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	h := &types.Header{Difficulty: big.NewInt(1), Number: big.NewInt(1), Extra: []byte{0x01, 0x02}}
	fmt.Printf("hashWithRound(bad extra)=%x emptyUncle=%x\n", h.WBFTHashWithRoundNumber(0), types.EmptyUncleHash)
	fmt.Printf("seal_data(bad, 0, prepare)=%x\n", wbftcore.PrepareSeal(h, 0, wbftcore.SealTypePrepare))
	fmt.Printf("keccak(keccak(c0)||00)=%x\n", crypto.Keccak256(append(types.EmptyUncleHash.Bytes(), 0)))
	fmt.Printf("Header.Hash(bad extra)=%x\n", h.Hash())
	// empty extra (len 0) with difficulty 1
	h2 := &types.Header{Difficulty: big.NewInt(1), Number: big.NewInt(1)}
	fmt.Printf("Header.Hash(empty extra)=%x plain=%x\n", h2.Hash(), crypto.Keccak256Hash(mustEnc(h2)))
}

func mustEnc(h *types.Header) []byte {
	b, err := rlpEncode(h)
	if err != nil {
		panic(err)
	}
	return b
}
