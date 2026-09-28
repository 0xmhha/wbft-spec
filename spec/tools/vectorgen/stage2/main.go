// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Command stage2 generates the stage-2 conformance vectors of A-11 §3.3:
// runner `validators` (quorum, proposer, epoch_boundary, validators_at,
// shuffle, sort_candidates, next_epoch_info), `timers/round_timeout`,
// `chain/config_at` and runner `header` (build_proposal_header,
// verify_header, verify_light).
//
// Handlers whose reference function is exported are computed in this
// process. The others (computeShuffledIndex, sortCandidates, buildEpochInfo
// and the round-change timer of the core) are run by generator tests that are
// injected into the reference packages with `go test -overlay` (the
// reference repository is not modified); they write their cases as JSON lines
// into a temporary directory that this program reads back.
//
// Usage (from tools/vectorgen):
//
//	GOTOOLCHAIN=go1.23.12 go run ./stage2 -out ../../vectors
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"vectorgen/internal/vecfile"
)

const (
	generatorName    = "vectorgen-stage2"
	generatorVersion = "0.3.0"
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
	cases = append(cases, quorumCases()...)
	cases = append(cases, proposerCases()...)
	cases = append(cases, epochBoundaryCases()...)
	cases = append(cases, validatorsAtCases()...)
	cases = append(cases, configAtCases()...)
	cases = append(cases, buildProposalCases()...)
	cases = append(cases, verifyHeaderCases()...)
	cases = append(cases, verifyLightCases()...)
	cases = append(cases, verifyHeadersCases()...)
	cases = append(cases, p256Cases(refDir)...)
	cases = append(cases, gasTipCases()...)
	cases = append(cases, feeDelegationCases()...)
	cases = append(cases, candidatesCases()...)
	cases = append(cases, finalizeCases()...)
	cases = append(cases, governanceCases()...)
	ov, err := runOverlay(refDir, *keep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen-stage2:", err)
		os.Exit(1)
	}
	cases = append(cases, ov...)
	if !*skipCheck {
		vecfile.CheckClean(refDir) // the overlay run must leave the reference untouched
	}

	cases = vecfile.ApplyPolicy(cases, vecfile.ReadWithheld(*withheld))
	if err := vecfile.Validate(cases); err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen-stage2:", err)
		os.Exit(1)
	}
	if err := vecfile.Write(*out, cases, ref, vecfile.Generator{Name: generatorName, Version: generatorVersion}); err != nil {
		fmt.Fprintln(os.Stderr, "vectorgen-stage2:", err)
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
