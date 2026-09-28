// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Generator test of vectorgen stage 2 (A-11 §3.3, timers/round_timeout).
// Injected into package core with `go test -overlay`; the reference
// repository is not modified. It calls the reference newRoundChangeTimer and
// records the duration it passes to the timer (through vectorgenAfterFunc)
// and the Warn record it logs, and writes one JSON object per case into
// $VECTORGEN_STAGE2_OUT/round_timeout.jsonl.
package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/log"
)

type vgKV struct {
	k string
	v any
}

type vgM []vgKV

func (m vgM) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.k)
		v, err := json.Marshal(kv.v)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type vgCase struct {
	Runner, Handler, Name, Kind, Desc, Err string
	Reqs                                   []string
	Input                                  vgM
	Expected                               vgM
}

// vgWarn records the message of every Warn record.
type vgWarn struct{ msgs *[]string }

func (h vgWarn) Enabled(context.Context, slog.Level) bool { return true }
func (h vgWarn) Handle(_ context.Context, r slog.Record) error {
	if r.Level == log.LevelWarn {
		*h.msgs = append(*h.msgs, r.Message)
	}
	return nil
}
func (h vgWarn) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h vgWarn) WithGroup(string) slog.Handler      { return h }

// vgTimeout runs the reference newRoundChangeTimer for (seq = 1, round) and
// returns the duration handed to the timer and the Warn messages.
func vgTimeout(requestTimeoutMs, maxSeconds uint64, round *big.Int) (time.Duration, []string) {
	var got time.Duration
	vectorgenAfterFunc = func(d time.Duration, f func()) *time.Timer {
		got = d
		return time.AfterFunc(time.Hour, func() {}) // never fires during the test
	}
	var msgs []string
	c := &Core{
		config: &wbft.Config{RequestTimeout: requestTimeoutMs, MaxRequestTimeoutSeconds: maxSeconds},
		logger: log.NewLogger(vgWarn{&msgs}),
	}
	c.newRoundChangeTimer(big.NewInt(1), round)
	c.stopTimer()
	return got, msgs
}

func TestVectorgenStage2RoundTimeout(t *testing.T) {
	dir := os.Getenv("VECTORGEN_STAGE2_OUT")
	if dir == "" {
		t.Skip("VECTORGEN_STAGE2_OUT not set")
	}
	type in struct {
		name, desc string
		rt, max    uint64
		round      *big.Int
		reqs       []string
	}
	r := func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }
	base := []string{"WBFT-TIMER-005"}
	capped := append(append([]string{}, base...), "WBFT-TIMER-006", "WBFT-TIMER-007")
	uncapped := append(append([]string{}, base...), "WBFT-TIMER-008")
	var ins []in
	// A-06 §4.4 tables
	for _, rd := range []uint64{0, 1, 2, 3, 4, 5, 6, 8, 10, 16, 32, 33, 34, 63, 64, 1023, 1024, 1100} {
		ins = append(ins, in{fmt.Sprintf("rt2000_uncapped_r%d", rd), "request_timeout 2000 ms (presets 8282, 8283), no cap", 2000, 0, r(rd), uncapped})
	}
	for _, cp := range []uint64{4, 10, 60} {
		for _, rd := range []uint64{0, 1, 2, 3, 4, 5, 6, 8, 10, 16, 32, 33, 100} {
			ins = append(ins, in{fmt.Sprintf("rt2000_cap%d_r%d", cp, rd), fmt.Sprintf("request_timeout 2000 ms, maxRequestTimeoutSeconds %d", cp), 2000, cp, r(rd), capped})
		}
	}
	for _, rd := range []uint64{0, 1, 4, 10, 33, 34} {
		ins = append(ins, in{fmt.Sprintf("rt1000_uncapped_r%d", rd), "request_timeout 1000 ms, no cap", 1000, 0, r(rd), uncapped})
	}
	for _, rd := range []uint64{0, 1, 2, 5, 100} {
		ins = append(ins, in{fmt.Sprintf("dev_preset_r%d", rd), "--dev and test preset: request_timeout 1000000 ms (1000 s), cap 4 s; round 0 lasts 1000 s, every later round 4 s with the Warn log (base > cap)", 1_000_000, 4, r(rd), capped})
	}
	ins = append(ins,
		in{"a06_example_1", "A-06 §4.2 example 1: mainnet preset, round 3", 2000, 0, r(3), uncapped},
		in{"a06_example_2", "A-06 §4.2 example 2: 2000 ms, cap 10 s, round 3", 2000, 10, r(3), capped},
		in{"a06_example_5_cap_wraps_negative", "A-06 §4.2 example 5: maxRequestTimeoutSeconds 10000000000 wraps to a negative cap, so the uncapped branch applies", 2000, 10_000_000_000, r(3), uncapped},
		in{"cap_wraps_to_small_positive", "maxRequestTimeoutSeconds 18446744074 wraps to a cap of 290448384 ns", 2000, 18_446_744_074, r(3), capped},
		in{"cap_equals_base", "cap equal to the base: every round lasts the base", 2000, 2, r(5), capped},
		in{"cap_above_2_pow_62_breaks", "request_timeout 1 ms and cap 4611686019 s (above 2^62 ns), round 70: 10^6 ns * 2^43 is the first value above the cap, so the loop stops there and returns the cap without the Warn log", 1, 4_611_686_019, r(70), capped},
		in{"cap_near_max_doubling_wraps", "request_timeout 2000 ms and cap 9223372036 s (just below 2^63 ns), round 70: no doubled value is above the cap, the doubling wraps through negative values to 0, the loop runs all 70 rounds, and the overflow guard returns the cap with the Warn log (A-06 §4.1 implementation note)", 2000, 9_223_372_036, r(70), capped},
		in{"base_wraps_capped", "request_timeout 9223372037000 ms: base = 9223372037000 * 10^6 wraps to a negative int64; with a cap the capped branch returns the negative base at round 0 (a negative duration fires at once)", 9_223_372_037_000, 10, r(0), capped},
		in{"round_above_2_pow_64", "round 2^64 + 3: only the low 64 bits (3) are used", 2000, 0, new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(3)), uncapped},
	)
	var cs []vgCase
	for _, x := range ins {
		d, warns := vgTimeout(x.rt, x.max, x.round)
		w := "none"
		switch {
		case len(warns) == 0:
		case len(warns) == 1 && warns[0] == "WBFT: Possible request timeout overflow detected, setting timeout value to maxRequestTimeout":
			w = "cap_overflow_guard"
		case len(warns) == 1 && warns[0] == "WBFT: Timeout overflow detected, setting timeout value to MaxInt64":
			w = "max_int64_clamp"
		default:
			t.Fatalf("%s: unexpected warnings %v", x.name, warns)
		}
		desc := x.desc
		if int64(d) == math.MaxInt64 {
			desc += "; clamped to MaxInt64 ns"
		}
		cs = append(cs, vgCase{Runner: "timers", Handler: "round_timeout", Name: x.name, Kind: "pure", Desc: desc, Reqs: x.reqs,
			Input:    vgM{{"request_timeout", strconv.FormatUint(x.rt, 10)}, {"max_request_timeout_seconds", strconv.FormatUint(x.max, 10)}, {"round", x.round.String()}},
			Expected: vgM{{"timeout", strconv.FormatInt(int64(d), 10)}, {"warning", w}}})
	}
	f, err := os.Create(filepath.Join(dir, "round_timeout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	for _, c := range cs {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		bw.Write(b)
		bw.WriteByte('\n')
	}
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
}
