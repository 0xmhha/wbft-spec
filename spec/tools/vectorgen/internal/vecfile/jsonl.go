// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package vecfile

// Reading the JSON-lines cases that generator tests injected with
// `go test -overlay` write (one case per line, mappings in insertion order).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type jsonCase struct {
	Runner, Handler, Name, Kind, Desc, Err string
	Reqs                                   []string
	Input, Expected                        json.RawMessage
}

// ReadJSONL reads the cases of one JSON-lines file.
func ReadJSONL(path string) ([]Case, error) {
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
		in, err := DecodeOrdered(jc.Input)
		if err != nil {
			return nil, err
		}
		c := Case{Runner: jc.Runner, Handler: jc.Handler, Name: jc.Name, Kind: jc.Kind, Desc: jc.Desc, Reqs: jc.Reqs, Err: jc.Err, Input: in.(M)}
		if len(jc.Expected) > 0 && string(jc.Expected) != "null" {
			ex, err := DecodeOrdered(jc.Expected)
			if err != nil {
				return nil, err
			}
			c.Expected = ex.(M)
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

// DecodeOrdered decodes JSON keeping the key order of objects (as M).
// Only strings, booleans, null, arrays and objects are accepted: integers are
// decimal strings in the vectors (WBFT-VEC-014).
func DecodeOrdered(raw []byte) (any, error) {
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
