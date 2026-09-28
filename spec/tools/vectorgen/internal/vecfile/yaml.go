// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Package vecfile holds the helpers shared by the generator stages: a small
// deterministic YAML emitter for the vector files, the case type, the
// reference checks and the writer of a case directory.
//
// The emitted documents use a restricted YAML subset so that any YAML 1.2
// parser (and a trivial converter to JSON) reads them the same way:
//   - mappings keep insertion order (type M),
//   - scalars are double-quoted strings, true/false or null,
//   - integers are written as decimal strings and byte strings as "0x..." hex
//     (A-11 section 3.1, WBFT-VEC-013 and WBFT-VEC-014; check_yaml_subset.py checks it).
//
// Strings are escaped with JSON escaping, which is valid inside YAML
// double-quoted scalars.
package vecfile

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// KV is one key/value pair of an ordered mapping.
type KV struct {
	K string
	V any
}

// M is an ordered mapping.
type M []KV

// Hex renders a byte string as a quoted 0x-prefixed lowercase hex scalar.
type Hex []byte

// Dec renders an integer as a quoted decimal scalar.
type Dec struct{ *big.Int }

// DecU renders a uint64 as a decimal scalar.
func DecU(v uint64) Dec { return Dec{new(big.Int).SetUint64(v)} }

// DecBig renders a copy of v as a decimal scalar.
func DecBig(v *big.Int) Dec { return Dec{new(big.Int).Set(v)} }

// Hexs renders a copy of b as a hex scalar.
func Hexs(b []byte) Hex { return Hex(append([]byte{}, b...)) }

func quote(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(s); err != nil {
		panic(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "null", true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case string:
		return quote(x), true
	case Hex:
		return quote("0x" + hex.EncodeToString(x)), true
	case Dec:
		if x.Int == nil {
			return "null", true
		}
		return quote(x.Int.String()), true
	case M:
		if len(x) == 0 {
			return "{}", true
		}
	case []any:
		if len(x) == 0 {
			return "[]", true
		}
	default:
		panic(fmt.Sprintf("yaml: unsupported type %T", v))
	}
	return "", false
}

// Emit renders an ordered mapping as a YAML document.
func Emit(m M) []byte {
	var sb strings.Builder
	writeMap(&sb, m, 0)
	return []byte(sb.String())
}

func writeMap(sb *strings.Builder, m M, indent int) {
	pad := strings.Repeat("  ", indent)
	for _, kv := range m {
		if s, ok := scalar(kv.V); ok {
			fmt.Fprintf(sb, "%s%s: %s\n", pad, kv.K, s)
			continue
		}
		fmt.Fprintf(sb, "%s%s:\n", pad, kv.K)
		switch x := kv.V.(type) {
		case M:
			writeMap(sb, x, indent+1)
		case []any:
			writeList(sb, x, indent+1)
		}
	}
}

func writeList(sb *strings.Builder, l []any, indent int) {
	pad := strings.Repeat("  ", indent)
	for _, v := range l {
		if s, ok := scalar(v); ok {
			fmt.Fprintf(sb, "%s- %s\n", pad, s)
			continue
		}
		switch x := v.(type) {
		case M:
			// First key on the dash line, the rest aligned below it.
			var inner strings.Builder
			writeMap(&inner, x, 0)
			lines := strings.Split(strings.TrimSuffix(inner.String(), "\n"), "\n")
			for i, ln := range lines {
				if i == 0 {
					fmt.Fprintf(sb, "%s- %s\n", pad, ln)
				} else {
					fmt.Fprintf(sb, "%s  %s\n", pad, ln)
				}
			}
		case []any:
			fmt.Fprintf(sb, "%s-\n", pad)
			writeList(sb, x, indent+1)
		}
	}
}
