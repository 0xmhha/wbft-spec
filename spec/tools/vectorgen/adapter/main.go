// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Command adapter answers the cases of the vectors that vectorgen writes over
// the adapter protocol wbft-vector/1 (A-11 §3.4, WBFT-VEC-062).
//
// At start it runs the three generator programs into a temporary directory,
// so that every answer is a value computed from the reference in this run,
// and indexes the cases they write by runner, handler and input. It never
// reads the vector files the runner uses (WBFT-VEC-041). A case whose input is
// not one the generators wrote is answered "unsupported": the adapter covers
// the vectors of its generator, not arbitrary inputs.
//
// Usage (from tools/vectorgen):
//
//	GOTOOLCHAIN=go1.23.12 go run ./adapter            # generate, then serve on stdin/stdout
//	GOTOOLCHAIN=go1.23.12 go run ./adapter -from DIR  # serve the cases of an earlier generator run in DIR
package main

import (
	"bufio"
	"encoding/json"
	"flag"
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

const (
	protocol = "wbft-vector/1"
	name     = "vectorgen-adapter"
	version  = "0.1.0"
)

type answer struct {
	output map[string]any // nil: the operation fails
	err    string
}

func logf(f string, a ...any) { fmt.Fprintf(os.Stderr, "adapter: "+f+"\n", a...) }

func canonical(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func readYAML(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return vecfile.Parse(b)
}

// load indexes every case under root by "runner/handler" and canonical input.
func load(root string) (map[string]map[string]answer, string, error) {
	idx := map[string]map[string]answer{}
	commit := ""
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "meta.yaml" {
			return err
		}
		dir := filepath.Dir(p)
		meta, err := readYAML(p)
		if err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		mm := meta.(map[string]any)
		in, err := readYAML(filepath.Join(dir, "input.yaml"))
		if err != nil {
			return fmt.Errorf("%s: %v", dir, err)
		}
		a := answer{}
		if ex, err := readYAML(filepath.Join(dir, "expected.yaml")); err == nil {
			a.output = ex.(map[string]any)
		} else if os.IsNotExist(err) {
			a.err, _ = mm["expected_error"].(string)
		} else {
			return fmt.Errorf("%s: %v", dir, err)
		}
		if c, ok := mm["reference"].(map[string]any)["commit"].(string); ok {
			commit = c
		}
		h := mm["runner"].(string) + "/" + mm["handler"].(string)
		if idx[h] == nil {
			idx[h] = map[string]answer{}
		}
		idx[h][canonical(in)] = a
		return nil
	})
	return idx, commit, err
}

func generate(dir string) error {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(file))
	for _, prog := range []string{".", "./stage2", "./stage3"} {
		cmd := exec.Command("go", "run", prog, "-out", dir)
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go run %s: %v", prog, err)
		}
	}
	return nil
}

func main() {
	from := flag.String("from", "", "serve the cases of an earlier generator run in this directory instead of generating them")
	flag.Parse()
	dir := *from
	if dir == "" {
		tmp, err := os.MkdirTemp("", "vectorgen-adapter-")
		if err != nil {
			logf("%v", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmp)
		if err := generate(tmp); err != nil {
			logf("%v", err)
			os.Exit(1)
		}
		dir = tmp
	}
	idx, commit, err := load(dir)
	if err != nil {
		logf("%v", err)
		os.Exit(1)
	}
	var handlers []string
	for h := range idx {
		handlers = append(handlers, h)
	}
	sort.Strings(handlers)
	logf("%d handlers ready", len(handlers))

	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	send := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}
	helloSeen := false
	for {
		line, err := in.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) == 0 {
			if err == io.EOF {
				return
			}
			if err != nil {
				logf("read: %v", err)
				os.Exit(1)
			}
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			logf("not a JSON object: %v", err)
			os.Exit(1)
		}
		switch msg["type"] {
		case "hello":
			if msg["protocol"] != protocol {
				logf("protocol %v, want %s", msg["protocol"], protocol)
				os.Exit(1)
			}
			helloSeen = true
			send(map[string]any{"type": "hello", "protocol": protocol,
				"impl":     map[string]any{"name": name, "version": version, "commit": commit, "lang": "go", "build": "cgo"},
				"handlers": handlers, "improvements": []any{}})
		case "case":
			if !helloSeen {
				logf("case before hello")
				os.Exit(1)
			}
			id := msg["id"]
			h := fmt.Sprint(msg["runner"]) + "/" + fmt.Sprint(msg["handler"])
			a, ok := idx[h][canonical(msg["input"])]
			switch {
			case !ok:
				send(map[string]any{"type": "result", "id": id, "status": "unsupported"})
			case a.output == nil:
				send(map[string]any{"type": "result", "id": id, "status": "error", "error_class": "reference", "message": a.err})
			default:
				send(map[string]any{"type": "result", "id": id, "status": "ok", "output": a.output})
			}
		case "bye":
			return
		default:
			logf("unexpected message type %v", msg["type"])
			os.Exit(1)
		}
	}
}
