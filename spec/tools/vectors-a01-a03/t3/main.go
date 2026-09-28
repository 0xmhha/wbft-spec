// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/consensus/wbft/messages"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	signed := []interface{}{rlp.RawValue{0xc3, 0x02, 0x01, 0xc0}, []byte{0xaa}}
	for _, tail := range [][]rlp.RawValue{
		{{0xc0}, {0xc0}}, {{0x80}, {0x80}}, {{0x05}, {0x00}}, {{0xc0}}, {{0xc0}, {0xc0}, {0xc0}},
	} {
		items := []interface{}{signed}
		for _, t := range tail {
			items = append(items, t)
		}
		b, _ := rlp.EncodeToBytes(items)
		m, err := messages.Decode(messages.RoundChangeCode, b)
		fmt.Printf("tail=%x wire=%x err=%v", tail, b, err)
		if err == nil {
			rc := m.(*messages.RoundChange)
			fmt.Printf(" pb=%v just=%v", rc.PreparedBlock, rc.Justification)
			re, _ := rlp.EncodeToBytes(rc)
			fmt.Printf(" reencoded=%x", re)
		}
		fmt.Println()
	}
	// prepared list with 1 element / 3 elements / zero digest
	for _, prep := range []rlp.RawValue{
		mustEnc([]interface{}{uint64(0)}),
		mustEnc([]interface{}{uint64(0), common.Hash{}, uint64(1)}),
		mustEnc([]interface{}{uint64(0), common.Hash{}}),
		mustEnc([]interface{}{uint64(1), common.HexToHash("0x01")}),
	} {
		payload := mustEnc([]interface{}{uint64(2), uint64(1), prep})
		b := mustEnc([]interface{}{[]interface{}{rlp.RawValue(payload), []byte{0xaa}}, rlp.RawValue{0xc0}, rlp.RawValue{0xc0}})
		m, err := messages.Decode(messages.RoundChangeCode, b)
		fmt.Printf("prepared=%x err=%v", []byte(prep), err)
		if err == nil {
			sp, _ := m.EncodePayloadForSigning()
			rc := m.(*messages.RoundChange)
			fmt.Printf(" pr=%v pd=%x signing=%x", rc.PreparedRound, rc.PreparedDigest, sp)
		}
		fmt.Println()
	}
}

func mustEnc(v interface{}) []byte {
	b, err := rlp.EncodeToBytes(v)
	if err != nil {
		panic(err)
	}
	return b
}
