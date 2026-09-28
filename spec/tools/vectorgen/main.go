// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Command vectorgen generates the WBFT conformance test vectors of A-11 §3
// by running the reference implementation (go-stablenet at the recorded
// commit, linked through the go.mod replace directive).
//
// Stage 1 covers the bottom layer (A-01, A-02, A-03): the runners `crypto`
// and `encoding`. Stage 2 is the program in ./stage2.
//
// Usage:
//
//	GOTOOLCHAIN=go1.23.12 go run . -out ../../vectors
//
// The program refuses to run when the toolchain is not go1.23.12, when the
// replaced module is not a git checkout of the reference commit, or when that
// checkout has local changes (A-11 WBFT-VEC-011, WBFT-VEC-020).
package main

import (
	"flag"
	"fmt"
	"os"

	"vectorgen/internal/vecfile"
)

const (
	generatorName    = "vectorgen"
	generatorVersion = "0.1.1"
)

// Case is one vector case: vectors/<runner>/<handler>/<name>/.
type Case = vecfile.Case

func main() {
	out := flag.String("out", "../../vectors", "output directory (the vectors/ root)")
	skipCheck := flag.Bool("skip-ref-check", false, "do not check toolchain and reference checkout (for development only; output is then not a valid vector set)")
	withheld := flag.String("withheld", "", "file listing withheld requirement IDs (publish.py --list-withheld); omits them and the cases left without a requirement. Empty: internal vector set")
	flag.Parse()

	ref := vecfile.Unchecked
	if !*skipCheck {
		ref = vecfile.CheckReference()
	}

	var cases []Case
	cases = append(cases, cryptoCases()...)
	cases = append(cases, encodingCases()...)

	cases = vecfile.ApplyPolicy(cases, vecfile.ReadWithheld(*withheld))
	if err := vecfile.Validate(cases); err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen:", err)
		os.Exit(1)
	}
	if err := vecfile.Write(*out, cases, ref, vecfile.Generator{Name: generatorName, Version: generatorVersion}); err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen:", err)
		os.Exit(1)
	}
	vecfile.Summary(cases)
}
