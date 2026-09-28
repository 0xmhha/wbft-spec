// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runs the generator tests injected with `go test -overlay` into
// consensus/wbft/core and consensus/wbft/backend and reads their cases back.
//
// Besides adding files, the overlay replaces seven reference source files by
// copies in which exactly one line each is changed. The changed lines only
// move a timer start, a goroutine post, a goroutine send or a clock read behind a hook
// (zz_vectorgen_stage3.go, zz_vectorgen_stage3_hook.go); with no recorder
// installed each hook does exactly what the replaced line did. With a recorder
// the generator sees every timer start and every scheduled event and decides
// itself when a scheduled event is processed (A-11 §3.1, "Steps").

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"vectorgen/internal/vecfile"
)

type edit struct {
	file     string // relative to the reference root
	old, new string
}

var edits = []edit{
	{"consensus/wbft/core/core.go",
		"c.roundChangeTimer = time.AfterFunc(timeout, func() {",
		"c.roundChangeTimer = vgRoundTimer(c, seq, round, timeout, func() {"},
	{"consensus/wbft/core/core.go",
		"c.retrySendingRoundChangeTimer = time.AfterFunc(timeout, func() {",
		"c.retrySendingRoundChangeTimer = vgRetryTimer(round, timeout, func() {"},
	{"consensus/wbft/core/preprepare.go",
		"c.futurePreprepareTimer = time.AfterFunc(duration, func() {",
		"c.futurePreprepareTimer = vgFutureTimer(preprepare, duration, func() {"},
	{"consensus/wbft/core/preprepare.go",
		"c.sendEvent(backlogEvent{",
		"vgPost(c, backlogEvent{"},
	{"consensus/wbft/backend/engine.go",
		"waitDuration = time.Until(time.Unix(int64(sb.timeForNextWork()), 0))",
		"waitDuration = vgUntil(time.Unix(int64(sb.timeForNextWork()), 0))"},
	{"consensus/wbft/core/backlog.go",
		"go c.sendEvent(event)",
		"vgSchedule(c, event)"},
	{"consensus/wbft/core/request.go",
		"go c.sendEvent(wbft.RequestEvent{",
		"vgSchedule(c, wbft.RequestEvent{"},
	{"consensus/wbft/backend/handler.go",
		"go sb.istanbulEventMux.Post(wbft.MessageEvent{",
		"vgPostReceived(sb, wbft.MessageEvent{"},
	{"consensus/wbft/backend/backend.go",
		"go sb.istanbulEventMux.Post(msg)",
		"vgPostSelf(sb, msg)"},
	{"consensus/wbft/backend/backend.go",
		"go p.SendWBFTConsensus(outboundCode, payload)",
		"vgSend(p, outboundCode, payload)"},
}

// added files: reference path -> source in overlay/
var added = map[string]string{
	"consensus/wbft/core/zz_vectorgen_stage3.go":         "core/zz_vectorgen_stage3.go",
	"consensus/wbft/core/zz_vectorgen_stage3_test.go":    "core/zz_vectorgen_stage3_test.go",
	"consensus/wbft/backend/zz_vectorgen_stage3_hook.go": "backend/zz_vectorgen_stage3_hook.go",
	"consensus/wbft/backend/zz_vectorgen_stage3_test.go": "backend/zz_vectorgen_stage3_test.go",
}

func overlaySrc() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		vecfile.Fail("cannot locate the overlay sources")
	}
	return filepath.Join(filepath.Dir(file), "overlay")
}

func runOverlay(refDir string, keep bool) ([]Case, error) {
	tmp, err := os.MkdirTemp("", "vectorgen-stage3-")
	if err != nil {
		return nil, err
	}
	if !keep {
		defer os.RemoveAll(tmp)
	} else {
		fmt.Fprintln(os.Stderr, "overlay dir:", tmp)
	}
	src := overlaySrc()
	replace := map[string]string{}
	// apply the one-line edits file by file
	byFile := map[string][]edit{}
	var files []string
	for _, e := range edits {
		if _, ok := byFile[e.file]; !ok {
			files = append(files, e.file)
		}
		byFile[e.file] = append(byFile[e.file], e)
	}
	for i, f := range files {
		orig, err := os.ReadFile(filepath.Join(refDir, f))
		if err != nil {
			return nil, err
		}
		s := string(orig)
		for _, e := range byFile[f] {
			if n := strings.Count(s, e.old); n != 1 {
				return nil, fmt.Errorf("%s: %d occurrences of %q, want 1", f, n, e.old)
			}
			s = strings.Replace(s, e.old, e.new, 1)
		}
		patched := filepath.Join(tmp, fmt.Sprintf("%d_%s", i, filepath.Base(f)))
		if err := os.WriteFile(patched, []byte(s), 0o644); err != nil {
			return nil, err
		}
		replace[filepath.Join(refDir, f)] = patched
	}
	for dst, s := range added {
		replace[filepath.Join(refDir, dst)] = filepath.Join(src, s)
	}
	ov, _ := json.MarshalIndent(map[string]any{"Replace": replace}, "", "  ")
	ovFile := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(ovFile, ov, 0o644); err != nil {
		return nil, err
	}
	outDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "test", "-vet=off", "-count=1", "-p", "1", "-overlay", ovFile, "-run", "^TestVectorgenStage3", "./consensus/wbft/core", "./consensus/wbft/backend")
	cmd.Dir = refDir
	cmd.Env = append(os.Environ(), "VECTORGEN_STAGE3_OUT="+outDir, "GOTOOLCHAIN="+vecfile.RequiredToolchain, "GOFLAGS=")
	var o bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &o
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go test -overlay: %v\n%s", err, o.String())
	}
	paths, _ := filepath.Glob(filepath.Join(outDir, "*.jsonl"))
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("the overlay tests wrote no cases:\n%s", o.String())
	}
	var cs []Case
	for _, f := range paths {
		c, err := vecfile.ReadJSONL(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", f, err)
		}
		cs = append(cs, c...)
	}
	return cs, nil
}
