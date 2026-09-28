// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `execution`: `p256_verify` (B-07 §9.4), `gas_tip_enforcement` and
// `fee_delegation` (B-07 §3 to §8, worked examples E-1 to E-9 of B-07 §10).
//
// The state a handler needs is part of its input: the accounts before
// the first transaction. The expected values come from the reference EVM:
// vm.EVM.Call for the precompile, core.ApplyTransaction for transactions,
// over a state.StateDB built in memory from the input accounts.

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

// ------------------------------------------------------------ configuration

// execCfg is the `config` input of the execution handlers: a network preset
// (B-01 §11) and overrides of its fork blocks (null keeps the preset value),
// the same shape as the `config` of chain/fork_schedule without the two fork
// times, which a WBFT header forbids anyway.
type execCfg struct {
	Preset         string
	Applepie, Boho *uint64
}

func (c execCfg) yaml() M {
	return M{{"preset", c.Preset}, {"overrides", M{{"applepie_block", optDec(c.Applepie)}, {"boho_block", optDec(c.Boho)}}}}
}

func (c execCfg) chainConfig() *params.ChainConfig {
	var cc params.ChainConfig
	switch c.Preset {
	case "8282":
		cc = *params.StableNetMainnetChainConfig
	case "8283":
		cc = *params.StableNetTestnetChainConfig
	default:
		panic("unknown preset " + c.Preset)
	}
	if c.Applepie != nil {
		cc.ApplepieBlock = new(big.Int).SetUint64(*c.Applepie)
	}
	if c.Boho != nil {
		cc.BohoBlock = new(big.Int).SetUint64(*c.Boho)
	}
	return &cc
}

var mainnet = execCfg{Preset: "8282"}

// ------------------------------------------------------------------ state

const (
	extraBlacklisted = uint64(1) << 63
	extraAuthorized  = uint64(1) << 62
)

// acct is one account of the input state.
type acct struct {
	addr    common.Address
	balance *big.Int
	nonce   uint64
	extra   uint64
	code    []byte
	storage []slot
}

type slot struct{ key, value common.Hash }

func (a acct) yaml() M {
	st := []any{}
	for _, s := range a.storage {
		st = append(st, M{{"key", hexs(s.key.Bytes())}, {"value", hexs(s.value.Bytes())}})
	}
	return M{{"address", hexs(a.addr.Bytes())}, {"balance", decBig(a.balance)}, {"nonce", dec(a.nonce)},
		{"extra", dec(a.extra)}, {"code", hexs(a.code)}, {"storage", st}}
}

func acctsYAML(as []acct) []any {
	out := []any{}
	for _, a := range as {
		out = append(out, a.yaml())
	}
	return out
}

// memState builds an in-memory state holding exactly the given accounts
// (an account of the list exists even if it is empty).
func memState(as []acct) *state.StateDB {
	sdb, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	must(err)
	for _, a := range as {
		sdb.CreateAccount(a.addr)
		sdb.SetBalance(a.addr, uint256.MustFromBig(a.balance))
		sdb.SetNonce(a.addr, a.nonce)
		if a.extra != 0 {
			sdb.SetExtra(a.addr, a.extra)
		}
		if len(a.code) > 0 {
			sdb.SetCode(a.addr, a.code)
		}
		for _, s := range a.storage {
			sdb.SetState(a.addr, s.key, s.value)
		}
	}
	return sdb
}

// coin returns x · 10^18 / den wei.
func coin(x, den int64) *big.Int {
	v := new(big.Int).Mul(big.NewInt(x), big.NewInt(1_000_000_000_000_000_000))
	return v.Div(v, big.NewInt(den))
}

func gwei(x uint64) *big.Int { return new(big.Int).Mul(new(big.Int).SetUint64(x), big.NewInt(1_000_000_000)) }

// ------------------------------------------------------------- p256_verify

// p256Case is one entry of core/vm/testdata/precompiles/p256Verify.json.
type p256Case struct {
	Input, Expected, Name string
	Gas                   uint64
}

// p256Call calls 0x…0100 with 100 000 gas through vm.EVM.Call on an empty
// state at block `number` of the configuration and returns the output and
// the gas used.
func p256Call(cfg execCfg, number uint64, input []byte) ([]byte, uint64) {
	cc := cfg.chainConfig()
	sdb := memState(nil)
	h := &types.Header{Number: new(big.Int).SetUint64(number), Time: pastTime + number, Difficulty: big.NewInt(1), GasLimit: gasLimit, BaseFee: big.NewInt(genesisFee)}
	coinbase := accts[3].addr
	ctx := core.NewEVMBlockContext(h, nil, &coinbase)
	caller := accts[4].addr
	evm := vm.NewEVM(ctx, vm.TxContext{Origin: caller, GasPrice: new(big.Int)}, sdb, cc, vm.Config{})
	const gas = 100_000
	ret, left, err := evm.Call(vm.AccountRef(caller), common.BytesToAddress([]byte{0x01, 0x00}), input, gas, new(uint256.Int))
	if err != nil {
		panic(fmt.Sprintf("p256 call failed: %v", err))
	}
	return ret, gas - left
}

func p256Cases(refDir string) []Case {
	raw, err := os.ReadFile(filepath.Join(refDir, "core/vm/testdata/precompiles/p256Verify.json"))
	must(err)
	var ref []p256Case
	must(json.Unmarshal(raw, &ref))
	if len(ref) != 782 {
		panic(fmt.Sprintf("p256Verify.json has %d entries, want 782", len(ref)))
	}
	reqs := []string{"SNET-TX-085", "SNET-TX-089", "SNET-CFG-010"}
	var out []Case
	add := func(name, desc string, rq []string, cfg execCfg, number uint64, input []byte) (string, uint64) {
		ret, gas := p256Call(cfg, number, input)
		out = append(out, Case{Runner: "execution", Handler: "p256_verify", Name: name, Kind: "pure", Desc: desc, Reqs: rq,
			Input:    M{{"config", cfg.yaml()}, {"number", dec(number)}, {"input", hexs(input)}},
			Expected: M{{"output", hexs(ret)}, {"gas", dec(gas)}}})
		return common.Bytes2Hex(ret), gas
	}
	for i, c := range ref {
		input := common.FromHex(c.Input)
		got, gas := add(fmt.Sprintf("ref_%03d", i),
			fmt.Sprintf("entry %d of the reference file core/vm/testdata/precompiles/p256Verify.json (%q), called with 100 000 gas at block 1 of the mainnet preset (BohoBlock 0); the file expects output %q and gas %d, which the call reproduces", i, strings.TrimSpace(c.Name), "0x"+c.Expected, c.Gas),
			reqs, mainnet, 1, input)
		if got != c.Expected || gas != c.Gas {
			panic(fmt.Sprintf("p256 entry %d: got %s gas %d, file %s gas %d", i, got, gas, c.Expected, c.Gas))
		}
	}
	valid := common.FromHex(ref[0].Input)
	boho := uint64(100)
	add("input_159_bytes", "the first reference input without its last byte (159 bytes): empty output, 6 900 gas, no error", reqs, mainnet, 1, valid[:159])
	add("input_161_bytes", "the first reference input with one zero byte appended (161 bytes): empty output, 6 900 gas", reqs, mainnet, 1, append(append([]byte{}, valid...), 0))
	add("input_empty", "an empty input: empty output, 6 900 gas", reqs, mainnet, 1, nil)
	add("before_boho", "the first reference input at block 99 with BohoBlock 100: 0x…0100 is an account without code, so the call succeeds with empty output and uses no gas", []string{"SNET-TX-085", "SNET-CFG-010"}, execCfg{Preset: "8282", Boho: &boho}, 99, valid)
	add("at_boho", "the first reference input at block 100 with BohoBlock 100: the precompile is active, output 0x…01, 6 900 gas", reqs, execCfg{Preset: "8282", Boho: &boho}, 100, valid)
	return out
}

// --------------------------------------------------------- transactions

// txHeader is the header of the block in which the transactions run: block
// 100, base fee B = 20 000 gwei (the Anzeon minimum), coinbase key 3 and the
// governance gas tip T in its extra (A-03).
func txHeader(tip *big.Int) *types.Header {
	extra := enc(&types.WBFTExtra{VanityData: make([]byte, 32), RandaoReveal: []byte{}, GasTip: tip})
	return &types.Header{
		ParentHash:  crypto.Keccak256Hash([]byte("wbft-vector-parent-99")),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    accts[3].addr,
		Root:        rootFor(100),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(100),
		GasLimit:    gasLimit,
		Time:        pastTime + 100,
		Extra:       extra,
		MixDigest:   crypto.Keccak256Hash([]byte("wbft-vector-mix-100")),
		BaseFee:     gwei(20_000),
	}
}

// txChain is the ChainContext of core.ApplyTransaction; the vectors use no
// BLOCKHASH, so it answers no header.
type txChain struct{}

func (txChain) Engine() consensus.Engine                    { return nil }
func (txChain) GetHeader(common.Hash, uint64) *types.Header { return nil }

type txResult struct {
	receipts []any
	after    []any
	err      error
}

// applyTxs runs the transactions in order with core.ApplyTransaction, as
// StateProcessor.Process does for the transactions of a block, and records
// the receipts and the accounts of the input afterwards. It stops at the
// first transaction-invalidating error: the block is then invalid (SNET-TX-060).
func applyTxs(cfg execCfg, h *types.Header, as []acct, txs []*types.Transaction) txResult {
	cc := cfg.chainConfig()
	sdb := memState(as)
	gp := new(core.GasPool).AddGas(h.GasLimit)
	var used uint64
	res := txResult{receipts: []any{}}
	for i, tx := range txs {
		sdb.SetTxContext(tx.Hash(), i)
		r, err := core.ApplyTransaction(cc, txChain{}, &h.Coinbase, gp, sdb, h, tx, &used, vm.Config{})
		if err != nil {
			res.err = fmt.Errorf("transaction %d: %w", i, err)
			return res
		}
		logs := []any{}
		for _, l := range r.Logs {
			topics := []any{}
			for _, t := range l.Topics {
				topics = append(topics, hexs(t.Bytes()))
			}
			logs = append(logs, M{{"address", hexs(l.Address.Bytes())}, {"topics", topics}, {"data", hexs(l.Data)}})
		}
		res.receipts = append(res.receipts, M{
			{"status", dec(r.Status)},
			{"gas_used", dec(r.GasUsed)},
			{"cumulative_gas_used", dec(r.CumulativeGasUsed)},
			{"effective_gas_price", decBig(r.EffectiveGasPrice)},
			{"logs", logs},
		})
	}
	res.after = []any{}
	for _, a := range as {
		res.after = append(res.after, M{{"address", hexs(a.addr.Bytes())}, {"balance", decBig(sdb.GetBalance(a.addr).ToBig())}, {"nonce", dec(sdb.GetNonce(a.addr))}})
	}
	return res
}

func signer(cc *params.ChainConfig, h *types.Header) types.Signer {
	return types.MakeSigner(cc, h.Number, h.Time)
}

// dynTx signs a type-0x02 transaction of account `key`.
func dynTx(cc *params.ChainConfig, h *types.Header, key int, nonce uint64, to common.Address, value *big.Int, gas uint64, feeCap, tipCap *big.Int, data []byte) *types.Transaction {
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: cc.ChainID, Nonce: nonce, GasTipCap: tipCap, GasFeeCap: feeCap, Gas: gas, To: &to, Value: value, Data: data}), signer(cc, h), accts[key].key)
	must(err)
	return tx
}

// legacyTx signs an EIP-155 legacy transaction of account `key`.
func legacyTx(cc *params.ChainConfig, h *types.Header, key int, nonce uint64, to common.Address, value *big.Int, gas uint64, price *big.Int) *types.Transaction {
	tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: nonce, GasPrice: price, Gas: gas, To: &to, Value: value}), signer(cc, h), accts[key].key)
	must(err)
	return tx
}

// feeTx wraps a signed type-0x02 transaction of `sender` into a type-0x16
// transaction with fee payer `payer`, signed by `payerKey` (B-07 §4.2).
// payer < 0 leaves the fee payer unset.
func feeTx(cc *params.ChainConfig, sender *types.Transaction, payer, payerKey int) *types.Transaction {
	bin, err := sender.MarshalBinary()
	must(err)
	var inner types.DynamicFeeTx
	must(rlp.DecodeBytes(bin[1:], &inner))
	fd := &types.FeeDelegateDynamicFeeTx{SenderTx: inner}
	if payer >= 0 {
		a := accts[payer].addr
		fd.FeePayer = &a
	}
	tx, err := types.SignTx(types.NewTx(fd), types.NewFeeDelegateSigner(cc.ChainID), accts[payerKey].key)
	must(err)
	return tx
}

// storeCode is a contract that reads slot 0, writes 1 into slot 1 and stops:
// a call to it uses more gas than a transfer and less than 50 000.
var storeCode = common.FromHex("0x60005450600160015500")

type txIn struct {
	name, desc string
	reqs       []string
	cfg        execCfg
	tip        *big.Int
	accounts   []acct
	txs        func(cc *params.ChainConfig, h *types.Header) []*types.Transaction
}

func txCases(handler string, ins []txIn) []Case {
	var out []Case
	for _, x := range ins {
		cc := x.cfg.chainConfig()
		h := txHeader(x.tip)
		txs := x.txs(cc, h)
		tl := []any{}
		for _, tx := range txs {
			b, err := tx.MarshalBinary()
			must(err)
			tl = append(tl, hexs(b))
		}
		res := applyTxs(x.cfg, h, x.accounts, txs)
		c := Case{Runner: "execution", Handler: handler, Name: x.name, Kind: "pure", Desc: x.desc + "; transactions applied with core.ApplyTransaction in order, as StateProcessor.Process applies the transactions of a block", Reqs: x.reqs,
			Input: M{{"config", x.cfg.yaml()}, {"header", hexs(enc(h))}, {"accounts", acctsYAML(x.accounts)}, {"transactions", tl}}}
		if res.err != nil {
			c.Err = res.err.Error()
		} else {
			c.Expected = M{{"receipts", res.receipts}, {"accounts", res.after}}
		}
		out = append(out, c)
	}
	return out
}

// Accounts of the worked examples: sender S = key 4, fee payer P = key 5,
// recipient R = key 6, authorized sender A = key 7, coinbase = key 3,
// contract C at a fixed address.
var contractC = common.HexToAddress("0x00000000000000000000000000000000000c0c0c")

func baseAccts(extraS uint64, balS, balP *big.Int, extraP uint64) []acct {
	return []acct{
		{addr: accts[4].addr, balance: balS, extra: extraS},
		{addr: accts[5].addr, balance: balP, extra: extraP},
		{addr: accts[6].addr, balance: new(big.Int)},
		{addr: accts[7].addr, balance: coin(10, 1), extra: extraAuthorized},
		{addr: accts[3].addr, balance: new(big.Int)},
		{addr: contractC, balance: new(big.Int), code: storeCode},
	}
}

func gasTipCases() []Case {
	T := gwei(27_600)
	r := func(ids ...string) []string {
		return append([]string{"SNET-TX-001", "SNET-TX-012", "SNET-TX-015", "SNET-TX-050", "SNET-TX-051", "SNET-TX-071"}, ids...)
	}
	S, R, A := 4, accts[6].addr, 7
	ins := []txIn{
		{"e1_non_authorized_transfer", "B-07 E-1: non-authorized EIP-1559 transfer of 5 coin, max_fee 100 000 gwei, max_priority_fee 1 gwei: the tip cap becomes T = 27 600 gwei, gas price 47 600 gwei, coinbase 0.5796 coin, one Transfer log",
			r("SNET-TX-010", "SNET-TX-032", "SNET-TX-073"), mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, S, 0, R, coin(5, 1), 21_000, gwei(100_000), gwei(1), nil)}
			}},
		{"e2_fee_cap_between_bounds", "B-07 E-2: as E-1 with max_fee 30 000 gwei, between max(B, T) and B + T: valid, gas price 30 000 gwei, tip 10 000 gwei",
			r("SNET-TX-010", "SNET-TX-013"), mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, S, 0, R, coin(5, 1), 21_000, gwei(30_000), gwei(1), nil)}
			}},
		{"e3_fee_cap_below_gas_tip", "B-07 E-3: max_fee 25 000 gwei, above B but below T: after the override the tip cap exceeds the fee cap, the transaction is invalid (ErrTipAboveFeeCap) and so is the block",
			[]string{"SNET-TX-010", "SNET-TX-013", "SNET-TX-060"}, mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, S, 0, R, coin(5, 1), 21_000, gwei(25_000), gwei(1), nil)}
			}},
		{"fee_cap_below_base_fee", "authorized sender (no override), max_fee 19 999 gwei below the base fee and max_priority_fee 0: invalid (ErrFeeCapTooLow)",
			[]string{"@r19", "SNET-TX-011", "SNET-TX-060"}, mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, A, 0, R, coin(1, 1), 21_000, gwei(19_999), gwei(0), nil)}
			}},
		{"e4_non_authorized_legacy", "B-07 E-4: non-authorized legacy transaction with gas price 1 000 000 gwei: tip cap T, fee cap 1 000 000 gwei, gas price 47 600 gwei; the balance check uses the fee cap (21 coin)",
			r("SNET-TX-010", "SNET-TX-032", "SNET-TX-073"), mainnet, T, baseAccts(0, coin(22, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{legacyTx(cc, h, S, 0, R, coin(1, 1), 21_000, gwei(1_000_000))}
			}},
		{"e4_legacy_balance_below_fee_cap", "E-4 with a sender balance of 20 coin: below gas_limit × gas_price + value = 22 coin, so the transaction is invalid although the charged amount would be 0.9996 coin",
			[]string{"SNET-TX-032", "SNET-TX-060"}, mainnet, T, baseAccts(0, coin(20, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{legacyTx(cc, h, S, 0, R, coin(1, 1), 21_000, gwei(1_000_000))}
			}},
		{"e5_authorized_zero_tip", "B-07 E-5: authorized sender, max_fee 20 000 gwei, max_priority_fee 0: no override, gas price 20 000 gwei, coinbase 0; the receipt ends with the AuthorizedTxExecuted log",
			r("SNET-TX-011", "@r46", "SNET-TX-073"), mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, A, 0, R, coin(1, 1), 21_000, gwei(20_000), gwei(0), nil)}
			}},
		{"e6_authorized_high_tip", "B-07 E-6: authorized sender, max_fee 200 000 gwei, max_priority_fee 100 000 gwei: gas price 120 000 gwei, coinbase 2.1 coin (more than T)",
			r("SNET-TX-011", "@r46", "SNET-TX-073"), mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, A, 0, R, coin(1, 1), 21_000, gwei(200_000), gwei(100_000), nil)}
			}},
		{"authorized_legacy", "authorized sender with a legacy transaction of gas price 30 000 gwei: its own tip cap is the gas price, so the gas price is 30 000 gwei",
			r("SNET-TX-011", "@r46"), mainnet, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{legacyTx(cc, h, A, 0, R, coin(1, 1), 21_000, gwei(30_000))}
			}},
		{"zero_gas_tip_header", "a header gas tip of 0: a non-authorized sender with max_priority_fee 5 000 gwei pays only the base fee (the override applies whenever the extra carries a gas tip)",
			r("SNET-TX-010"), mainnet, new(big.Int), baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{dynTx(cc, h, S, 0, R, coin(1, 1), 21_000, gwei(100_000), gwei(5_000), nil)}
			}},
		{"e9_block_totals", "B-07 E-9: E-1, E-5 and E-7 (with this vector's contract call) in one block: the coinbase receives the three tips, the cumulative gas used is the sum of the three, and the base fee is credited to nobody during execution (B-06 distributes it)",
			r("SNET-TX-010", "SNET-TX-011", "SNET-TX-034", "@r46", "SNET-TX-073"), mainnet, T, baseAccts(0, coin(10, 1), coin(10, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				e1 := dynTx(cc, h, S, 0, R, coin(5, 1), 21_000, gwei(100_000), gwei(1), nil)
				e5 := dynTx(cc, h, A, 0, R, coin(1, 1), 21_000, gwei(20_000), gwei(0), nil)
				e7 := feeTx(cc, dynTx(cc, h, S, 1, contractC, coin(1, 1), 50_000, gwei(100_000), gwei(1), nil), 5, 5)
				return []*types.Transaction{e1, e5, e7}
			}},
	}
	return txCases("gas_tip_enforcement", ins)
}

func feeDelegationCases() []Case {
	T := gwei(27_600)
	late := uint64(1000)
	before := execCfg{Preset: "8282", Applepie: &late}
	S := 4
	C := contractC
	sendTx := func(cc *params.ChainConfig, h *types.Header, value *big.Int) *types.Transaction {
		return dynTx(cc, h, S, 0, C, value, 50_000, gwei(100_000), gwei(1), nil)
	}
	base := []string{"SNET-TX-012", "@r20", "@r21"}
	r := func(ids ...string) []string { return append(append([]string{}, base...), ids...) }
	ins := []txIn{
		{"e7_delegated_call", "B-07 E-7: sender S (non-authorized), fee payer P, value 1 coin to a contract, gas 50 000, max_fee 100 000 gwei, after Applepie: gas price 47 600 gwei; P is debited gas × gas price and refunded the unused gas, S pays only the value",
			r("SNET-TX-031", "@r22", "SNET-TX-034", "SNET-TX-050"), mainnet, T, baseAccts(0, coin(2, 1), coin(3, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{feeTx(cc, sendTx(cc, h, coin(1, 1)), 5, 5)}
			}},
		{"fee_payer_balance_too_low", "as E-7 with P holding 2 coin, below 2.38 coin: invalid (ErrInsufficientFunds of the fee payer)",
			[]string{"SNET-TX-012", "SNET-TX-031", "SNET-TX-060"}, mainnet, T, baseAccts(0, coin(2, 1), coin(2, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{feeTx(cc, sendTx(cc, h, coin(1, 1)), 5, 5)}
			}},
		{"sender_balance_below_value", "as E-7 with S holding 0.5 coin, below the value of 1 coin: invalid (ErrInsufficientFunds of the sender)",
			[]string{"SNET-TX-031", "SNET-TX-060"}, mainnet, T, baseAccts(0, coin(1, 2), coin(3, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{feeTx(cc, sendTx(cc, h, coin(1, 1)), 5, 5)}
			}},
		{"authorized_sender_delegated", "as E-7 with an authorized sender (key 7): the tip is the sender's own max_priority_fee (1 gwei); authorization is looked up for the sender, not the fee payer",
			r("SNET-TX-011", "SNET-TX-031", "SNET-TX-034", "@r46"), mainnet, T, baseAccts(0, coin(2, 1), coin(3, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{feeTx(cc, dynTx(cc, h, 7, 0, C, coin(1, 1), 50_000, gwei(100_000), gwei(1), nil), 5, 5)}
			}},
		{"before_applepie_delegated", "ApplepieBlock 1000, block 100: a type-0x16 transaction whose fee payer differs from the sender is invalid (fee delegation type not supported)",
			[]string{"SNET-TX-030", "SNET-CFG-009", "SNET-TX-060"}, before, T, baseAccts(0, coin(2, 1), coin(3, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{feeTx(cc, sendTx(cc, h, coin(1, 1)), 5, 5)}
			}},
		{"before_applepie_self_paid", "ApplepieBlock 1000, block 100: a type-0x16 transaction whose fee payer is the sender is accepted and charged like a type-0x02 transaction; the refund goes to the fee payer, that is the sender",
			r("SNET-TX-030", "SNET-TX-032", "SNET-TX-034"), before, T, baseAccts(0, coin(10, 1), coin(0, 1), 0),
			func(cc *params.ChainConfig, h *types.Header) []*types.Transaction {
				return []*types.Transaction{feeTx(cc, sendTx(cc, h, coin(1, 1)), S, S)}
			}},
	}
	return txCases("fee_delegation", ins)
}
