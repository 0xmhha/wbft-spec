// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package vecfile

// Requirement aliases and the withheld-requirement policy.
//
// An edition of the specification can leave some requirements out. The
// generator source does not name those requirements: a case refers to one
// through an alias ("@r01") that requirements.tsv maps to the ID, and a
// description refers to it as "{@r01}". In the copy of requirements.tsv that
// is published with an edition, the alias of every requirement left out of
// that edition maps to "-".
//
// Resolution, applied to every case before Validate and Write:
//   - an alias is replaced by its ID; an alias mapped to "-", or an ID in the
//     -withheld list, is omitted from the case's requirements;
//   - a case whose description names an omitted alias is omitted;
//   - a case whose requirements all were omitted is omitted.
//
// With the full table and without -withheld, nothing is omitted.

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"
)

//go:embed requirements.tsv
var requirementTable string

var (
	aliasRe     = regexp.MustCompile(`^@r\d{2}$`)
	descAliasRe = regexp.MustCompile(`\{(@r\d{2})\}`)
	reqIDRe     = regexp.MustCompile(`^(WBFT|SNET)-[A-Z]+-\d{3}$`)
)

// aliases parses requirements.tsv: "alias<TAB>ID" or "alias<TAB>-" per line.
func aliases() map[string]string {
	m := map[string]string{}
	for n, line := range strings.Split(requirementTable, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 2 || !aliasRe.MatchString(f[0]) || !(f[1] == "-" || reqIDRe.MatchString(f[1])) {
			Fail("requirements.tsv:%d: want \"@rNN<TAB>ID\" or \"@rNN<TAB>-\", got %q", n+1, line)
		}
		m[f[0]] = f[1]
	}
	return m
}

// ReadWithheld reads a list of withheld requirement IDs: one ID per line,
// "#" starts a comment. publish.py writes this file (--list-withheld).
func ReadWithheld(path string) map[string]bool {
	w := map[string]bool{}
	if path == "" {
		return w
	}
	data, err := os.ReadFile(path)
	if err != nil {
		Fail("withheld list: %v", err)
	}
	for n, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !reqIDRe.MatchString(line) {
			Fail("withheld list %s:%d: not a requirement ID: %q", path, n+1, line)
		}
		w[line] = true
	}
	return w
}

// ApplyPolicy resolves aliases and omits withheld requirements and cases.
func ApplyPolicy(cases []Case, withheld map[string]bool) []Case {
	table := aliases()
	out := make([]Case, 0, len(cases))
	omittedReqs, omittedCases := 0, 0
	for _, c := range cases {
		id := c.Runner + "/" + c.Handler + "/" + c.Name
		keep := true
		for _, m := range descAliasRe.FindAllStringSubmatch(c.Desc, -1) {
			v, ok := table[m[1]]
			if !ok {
				Fail("case %s: unknown alias %s in description", id, m[1])
			}
			if v == "-" || withheld[v] {
				keep = false
			}
		}
		c.Desc = descAliasRe.ReplaceAllStringFunc(c.Desc, func(s string) string { return table[s[1:len(s)-1]] })
		reqs := make([]string, 0, len(c.Reqs))
		for _, r := range c.Reqs {
			if strings.HasPrefix(r, "@") {
				v, ok := table[r]
				if !ok {
					Fail("case %s: unknown alias %s", id, r)
				}
				r = v
			}
			if r == "-" || withheld[r] {
				omittedReqs++
				continue
			}
			reqs = append(reqs, r)
		}
		if len(c.Reqs) > 0 && len(reqs) == 0 {
			keep = false
		}
		if !keep {
			omittedCases++
			continue
		}
		c.Reqs = reqs
		out = append(out, c)
	}
	if omittedReqs > 0 || omittedCases > 0 {
		fmt.Fprintf(os.Stderr, "withheld: omitted %d requirement reference(s) and %d case(s)\n", omittedReqs, omittedCases)
	}
	return out
}
