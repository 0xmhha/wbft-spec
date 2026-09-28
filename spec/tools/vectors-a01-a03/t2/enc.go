// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

import "github.com/ethereum/go-ethereum/rlp"

func rlpEncode(v interface{}) ([]byte, error) { return rlp.EncodeToBytes(v) }
