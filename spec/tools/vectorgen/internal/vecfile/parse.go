// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package vecfile

// A reader of the YAML subset of A-11 WBFT-VEC-013, as the emitter of this
// package writes it: block mappings and sequences indented by two spaces,
// double-quoted strings with JSON escaping, true, false, null, [] and {}. It
// returns the JSON data model (map[string]any, []any, string, bool, nil) and
// rejects anything else; it does not replace check_yaml_subset.py.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var keyRe = regexp.MustCompile(`^([a-z0-9_]+):(?: (.*))?$`)

type yline struct {
	indent int
	text   string // without the indentation
	no     int    // line number, from 1
}

// Parse reads one vector document.
func Parse(data []byte) (any, error) {
	var ls []yline
	for i, raw := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		t := strings.TrimLeft(raw, " ")
		n := len(raw) - len(t)
		if t == "" || n%2 != 0 {
			return nil, fmt.Errorf("line %d: empty line or odd indentation", i+1)
		}
		ls = append(ls, yline{n, t, i + 1})
	}
	if len(ls) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	v, next, err := parseBlock(ls, 0, 0)
	if err != nil {
		return nil, err
	}
	if next != len(ls) {
		return nil, fmt.Errorf("line %d: unexpected content", ls[next].no)
	}
	return v, nil
}

func isSeq(l yline) bool { return l.text == "-" || strings.HasPrefix(l.text, "- ") }

func parseBlock(ls []yline, i, indent int) (any, int, error) {
	if i >= len(ls) || ls[i].indent != indent {
		return nil, i, fmt.Errorf("line %d: missing block at indentation %d", lineNo(ls, i), indent)
	}
	if isSeq(ls[i]) {
		return parseSeq(ls, i, indent)
	}
	return parseMap(ls, i, indent)
}

func lineNo(ls []yline, i int) int {
	if i < len(ls) {
		return ls[i].no
	}
	return ls[len(ls)-1].no + 1
}

func parseMap(ls []yline, i, indent int) (any, int, error) {
	m := map[string]any{}
	for i < len(ls) && ls[i].indent == indent && !isSeq(ls[i]) {
		mt := keyRe.FindStringSubmatch(ls[i].text)
		if mt == nil {
			return nil, i, fmt.Errorf("line %d: not a mapping entry", ls[i].no)
		}
		if _, dup := m[mt[1]]; dup {
			return nil, i, fmt.Errorf("line %d: duplicate key %q", ls[i].no, mt[1])
		}
		if mt[2] == "" {
			v, next, err := parseBlock(ls, i+1, indent+2)
			if err != nil {
				return nil, i, err
			}
			m[mt[1]] = v
			i = next
			continue
		}
		v, err := scalarValue(mt[2], ls[i].no)
		if err != nil {
			return nil, i, err
		}
		m[mt[1]] = v
		i++
	}
	if i < len(ls) && ls[i].indent > indent {
		return nil, i, fmt.Errorf("line %d: unexpected indentation", ls[i].no)
	}
	return m, i, nil
}

func parseSeq(ls []yline, i, indent int) (any, int, error) {
	l := []any{}
	for i < len(ls) && ls[i].indent == indent && isSeq(ls[i]) {
		if ls[i].text == "-" {
			v, next, err := parseBlock(ls, i+1, indent+2)
			if err != nil {
				return nil, i, err
			}
			l = append(l, v)
			i = next
			continue
		}
		rest := ls[i].text[2:]
		if keyRe.MatchString(rest) {
			// a mapping whose first entry is on the dash line: parse it as a
			// mapping at indent + 2
			saved := ls[i]
			ls[i] = yline{indent + 2, rest, saved.no}
			v, next, err := parseMap(ls, i, indent+2)
			ls[i] = saved
			if err != nil {
				return nil, i, err
			}
			l = append(l, v)
			i = next
			continue
		}
		v, err := scalarValue(rest, ls[i].no)
		if err != nil {
			return nil, i, err
		}
		l = append(l, v)
		i++
	}
	if i < len(ls) && ls[i].indent > indent {
		return nil, i, fmt.Errorf("line %d: unexpected indentation", ls[i].no)
	}
	return l, i, nil
}

func scalarValue(s string, no int) (any, error) {
	switch s {
	case "null":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "[]":
		return []any{}, nil
	case "{}":
		return map[string]any{}, nil
	}
	if !strings.HasPrefix(s, `"`) {
		return nil, fmt.Errorf("line %d: scalar is not a double-quoted string: %s", no, s)
	}
	var out string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("line %d: %v", no, err)
	}
	return out, nil
}
