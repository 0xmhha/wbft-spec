// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Command stage3 generates the stage-3 conformance vectors of A-11 §3.3:
// runner `state_machine` (check_message, is_justified, rounds), runner
// `network` (receive_outcome), and `chain/fork_schedule`, `chain/genesis`.
//
// The state-machine and network vectors are produced by generator tests that
// are injected into consensus/wbft/core and consensus/wbft/backend with
// `go test -overlay` (the reference repository is not modified; see
// overlay.go). The chain handlers call exported reference functions in this
// process.
//
// Usage (from tools/vectorgen):
//
//	GOTOOLCHAIN=go1.23.12 go run ./stage3 -out ../../vectors
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"vectorgen/internal/vecfile"
)

const (
	generatorName    = "vectorgen-stage3"
	generatorVersion = "0.3.1"
)

type (
	KV   = vecfile.KV
	M    = vecfile.M
	Case = vecfile.Case
)

func main() {
	out := flag.String("out", "../../vectors", "output directory (the vectors/ root)")
	skipCheck := flag.Bool("skip-ref-check", false, "do not check toolchain and reference checkout (for development only; output is then not a valid vector set)")
	keep := flag.Bool("keep-overlay-dir", false, "keep the temporary overlay directory (for debugging)")
	withheld := flag.String("withheld", "", "file listing withheld requirement IDs (publish.py --list-withheld); omits them and the cases left without a requirement. Empty: internal vector set")
	flag.Parse()

	ref := vecfile.Unchecked
	if !*skipCheck {
		ref = vecfile.CheckReference()
	}
	refDir := ref.Dir
	if refDir == "" {
		refDir = replaceDir()
	}

	var cases []Case
	cases = append(cases, forkScheduleCases()...)
	cases = append(cases, genesisCases()...)
	ov, err := runOverlay(refDir, *keep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen-stage3:", err)
		os.Exit(1)
	}
	cases = append(cases, ov...)
	if !*skipCheck {
		vecfile.CheckClean(refDir) // the overlay run must leave the reference untouched
	}

	cases = vecfile.ApplyPolicy(cases, vecfile.ReadWithheld(*withheld))
	if err := vecfile.Validate(cases); err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen-stage3:", err)
		os.Exit(1)
	}
	if err := vecfile.Write(*out, cases, ref, vecfile.Generator{Name: generatorName, Version: generatorVersion}); err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen-stage3:", err)
		os.Exit(1)
	}
	vecfile.Summary(cases)
}

func replaceDir() string {
	bi, ok := debug.ReadBuildInfo()
	if ok {
		for _, d := range bi.Deps {
			if d.Path == vecfile.ReferenceModule && d.Replace != nil {
				return d.Replace.Path
			}
		}
	}
	vecfile.Fail("%s is not replaced by a local checkout", vecfile.ReferenceModule)
	return ""
}
