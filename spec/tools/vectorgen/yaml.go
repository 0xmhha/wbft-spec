// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// The YAML emitter and the case writer are shared with stage 2 and live in
// internal/vecfile; these aliases keep the stage-1 source unchanged.

import (
	"math/big"

	"vectorgen/internal/vecfile"
)

type (
	KV  = vecfile.KV
	M   = vecfile.M
	Hex = vecfile.Hex
	Dec = vecfile.Dec
)

func dec(v uint64) Dec      { return vecfile.DecU(v) }
func decBig(v *big.Int) Dec { return vecfile.DecBig(v) }
func hexs(b []byte) Hex     { return vecfile.Hexs(b) }
