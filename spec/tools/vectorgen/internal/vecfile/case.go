// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package vecfile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

const (
	ReferenceModule   = "github.com/ethereum/go-ethereum"
	ReferenceCommit   = "740526d03" // spec base: go-stablenet dev branch at this commit
	RequiredToolchain = "go1.23.12" // toolchain of the reference (the `toolchain` line of its go.mod)
)

// Case is one vector case: vectors/<runner>/<handler>/<name>/.
type Case struct {
	Runner, Handler, Name string
	Kind                  string // "pure", "steps" or "chain" (A-11 §3.3)
	Desc                  string
	Reqs                  []string
	Input                 M
	Expected              M      // nil: the operation MUST fail (WBFT-VEC-010)
	Err                   string // reference error for a fail case (informative)
}

// Generator names the program in meta.yaml (WBFT-VEC-015).
type Generator struct {
	Name, Version string
}

// Ref is the checked reference checkout.
type Ref struct {
	Dir, Commit string
}

// Unchecked is the Ref recorded with -skip-ref-check (not a valid vector set).
var Unchecked = Ref{Commit: "unchecked"}

// CheckReference enforces the toolchain and the reference commit: the
// running binary must be built with go1.23.12 and the replaced module must be
// a clean git checkout whose HEAD starts with ReferenceCommit.
func CheckReference() Ref {
	if runtime.Version() != RequiredToolchain {
		Fail("toolchain is %s, want %s (run with GOTOOLCHAIN=%s)", runtime.Version(), RequiredToolchain, RequiredToolchain)
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		Fail("no build info")
	}
	dir := ""
	for _, d := range bi.Deps {
		if d.Path == ReferenceModule && d.Replace != nil {
			dir = d.Replace.Path
		}
	}
	if dir == "" {
		Fail("%s is not replaced by a local checkout (see README)", ReferenceModule)
	}
	head := strings.TrimSpace(Git(dir, "rev-parse", "HEAD"))
	if !strings.HasPrefix(head, ReferenceCommit) {
		Fail("reference checkout %s is at %s, want %s", dir, head, ReferenceCommit)
	}
	CheckClean(dir)
	return Ref{Dir: dir, Commit: head}
}

// CheckClean fails if the reference checkout has local changes.
func CheckClean(dir string) {
	if st := strings.TrimSpace(Git(dir, "status", "--porcelain")); st != "" {
		Fail("reference checkout %s has local changes:\n%s", dir, st)
	}
}

// Git runs git in dir and returns its standard output.
func Git(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var o bytes.Buffer
	cmd.Stdout = &o
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		Fail("git %v in %s: %v", args, dir, err)
	}
	return o.String()
}

// Fail prints an error and exits with status 1.
func Fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "vectorgen: "+f+"\n", a...)
	os.Exit(1)
}

// Validate checks case identity, names and the fields every case needs.
func Validate(cases []Case) error {
	seen := map[string]bool{}
	for _, c := range cases {
		id := c.Runner + "/" + c.Handler + "/" + c.Name
		if seen[id] {
			return fmt.Errorf("duplicate case %s", id)
		}
		seen[id] = true
		if len(c.Reqs) == 0 {
			return fmt.Errorf("case %s lists no requirement", id)
		}
		if c.Expected == nil && c.Err == "" {
			return fmt.Errorf("fail case %s has no reference error", id)
		}
		for _, ch := range c.Name {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_') {
				return fmt.Errorf("case name %q: only [a-z0-9_] allowed", c.Name)
			}
		}
	}
	return nil
}

// Write replaces every handler directory that the given cases own and writes
// the cases. Directories of handlers not generated here are left untouched.
func Write(root string, cases []Case, ref Ref, gen Generator) error {
	owned := map[string]bool{}
	for _, c := range cases {
		owned[filepath.Join(root, c.Runner, c.Handler)] = true
	}
	dirs := make([]string, 0, len(owned))
	for d := range owned {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	for _, c := range cases {
		dir := filepath.Join(root, c.Runner, c.Handler, c.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.yaml"), Emit(Meta(c, ref, gen)), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "input.yaml"), Emit(c.Input), 0o644); err != nil {
			return err
		}
		if c.Expected != nil {
			if err := os.WriteFile(filepath.Join(dir, "expected.yaml"), Emit(c.Expected), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// Meta builds meta.yaml (WBFT-VEC-011, WBFT-VEC-015).
func Meta(c Case, ref Ref, gen Generator) M {
	reqs := make([]any, len(c.Reqs))
	for i, r := range c.Reqs {
		reqs[i] = r
	}
	m := M{
		{"runner", c.Runner},
		{"handler", c.Handler},
		{"case", c.Name},
		{"kind", c.Kind},
		{"description", c.Desc},
		{"requirements", reqs},
		{"reference", M{
			{"implementation", "go-stablenet"},
			{"commit", ref.Commit},
			{"toolchain", RequiredToolchain},
			{"build", "cgo"},
		}},
		{"generator", M{
			{"name", gen.Name},
			{"version", gen.Version},
		}},
	}
	if c.Expected == nil {
		m = append(m, KV{"expected_error", c.Err})
	}
	return m
}

// Summary prints the number of ok and fail cases per handler.
func Summary(cases []Case) {
	type key struct{ r, h string }
	count := map[key][2]int{}
	var order []key
	for _, c := range cases {
		k := key{c.Runner, c.Handler}
		v, ok := count[k]
		if !ok {
			order = append(order, k)
		}
		if c.Expected == nil {
			v[1]++
		} else {
			v[0]++
		}
		count[k] = v
	}
	total := [2]int{}
	for _, k := range order {
		v := count[k]
		fmt.Printf("%-13s %-22s ok=%3d fail=%3d\n", k.r, k.h, v[0], v[1])
		total[0] += v[0]
		total[1] += v[1]
	}
	fmt.Printf("%-36s ok=%3d fail=%3d cases=%d\n", "total", total[0], total[1], total[0]+total[1])
}
