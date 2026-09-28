// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runs the generator tests injected with `go test -overlay` and reads their
// cases back (JSON lines, one case per line, mappings in insertion order).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"vectorgen/internal/vecfile"
)

// The only change to a reference source file: the round-change timer is
// started through a package variable, so that the generator test can read
// the duration that newRoundChangeTimer computed. The arithmetic is untouched.
const (
	timerCall = "c.roundChangeTimer = time.AfterFunc(timeout, func() {"
	timerHook = "c.roundChangeTimer = vectorgenAfterFunc(timeout, func() {"
)

func overlaySrc() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		vecfile.Fail("cannot locate the overlay sources")
	}
	return filepath.Join(filepath.Dir(file), "overlay")
}

func runOverlay(refDir string, keep bool) ([]Case, error) {
	tmp, err := os.MkdirTemp("", "vectorgen-stage2-")
	if err != nil {
		return nil, err
	}
	if !keep {
		defer os.RemoveAll(tmp)
	} else {
		fmt.Fprintln(os.Stderr, "overlay dir:", tmp)
	}
	src := overlaySrc()
	coreGo := filepath.Join(refDir, "consensus/wbft/core/core.go")
	orig, err := os.ReadFile(coreGo)
	if err != nil {
		return nil, err
	}
	if n := strings.Count(string(orig), timerCall); n != 1 {
		return nil, fmt.Errorf("core.go: %d occurrences of the round-change timer call, want 1", n)
	}
	patched := filepath.Join(tmp, "core.go")
	if err := os.WriteFile(patched, []byte(strings.Replace(string(orig), timerCall, timerHook, 1)), 0o644); err != nil {
		return nil, err
	}
	replace := map[string]string{
		filepath.Join(refDir, "consensus/wbft/engine/zz_vectorgen_stage2_test.go"): filepath.Join(src, "engine/zz_vectorgen_stage2_test.go"),
		filepath.Join(refDir, "consensus/wbft/core/zz_vectorgen_stage2_test.go"):   filepath.Join(src, "core/zz_vectorgen_stage2_test.go"),
		filepath.Join(refDir, "consensus/wbft/core/zz_vectorgen_stage2_hook.go"):   filepath.Join(src, "core/zz_vectorgen_stage2_hook.go"),
		coreGo: patched,
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
	cmd := exec.Command("go", "test", "-vet=off", "-count=1", "-overlay", ovFile, "-run", "^TestVectorgenStage2", "./consensus/wbft/engine", "./consensus/wbft/core")
	cmd.Dir = refDir
	cmd.Env = append(os.Environ(), "VECTORGEN_STAGE2_OUT="+outDir, "GOTOOLCHAIN="+vecfile.RequiredToolchain, "GOFLAGS=")
	var o bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &o
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go test -overlay: %v\n%s", err, o.String())
	}
	files, _ := filepath.Glob(filepath.Join(outDir, "*.jsonl"))
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("the overlay tests wrote no cases:\n%s", o.String())
	}
	var cs []Case
	for _, f := range files {
		c, err := readJSONL(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", f, err)
		}
		cs = append(cs, c...)
	}
	return cs, nil
}

type jsonCase struct {
	Runner, Handler, Name, Kind, Desc, Err string
	Reqs                                   []string
	Input, Expected                        json.RawMessage
}

func readJSONL(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		var jc jsonCase
		if err := json.Unmarshal(sc.Bytes(), &jc); err != nil {
			return nil, err
		}
		in, err := decodeOrdered(jc.Input)
		if err != nil {
			return nil, err
		}
		c := Case{Runner: jc.Runner, Handler: jc.Handler, Name: jc.Name, Kind: jc.Kind, Desc: jc.Desc, Reqs: jc.Reqs, Err: jc.Err, Input: in.(M)}
		if len(jc.Expected) > 0 && string(jc.Expected) != "null" {
			ex, err := decodeOrdered(jc.Expected)
			if err != nil {
				return nil, err
			}
			c.Expected = ex.(M)
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

// decodeOrdered decodes JSON keeping the key order of objects (as M).
// Only strings, booleans, null, arrays and objects are accepted.
func decodeOrdered(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	v, err := decodeValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data")
	}
	return v, nil
}

func decodeValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			m := M{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				m = append(m, KV{K: k.(string), V: v})
			}
			_, err := d.Token()
			return m, err
		case '[':
			l := []any{}
			for d.More() {
				v, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				l = append(l, v)
			}
			_, err := d.Token()
			return l, err
		}
	case string, bool, nil:
		return x, nil
	}
	return nil, fmt.Errorf("unsupported JSON token %v (numbers are not allowed; integers are decimal strings)", t)
}
