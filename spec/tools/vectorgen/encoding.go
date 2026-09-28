// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `encoding` (A-03): extra_codec, block_hash, hash_with_round,
// filtered_header. The message handlers are in messages.go.

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

func encodingCases() []Case {
	var cs []Case
	cs = append(cs, extraCodecCases()...)
	cs = append(cs, blockHashCases()...)
	cs = append(cs, hashWithRoundCases()...)
	cs = append(cs, filteredHeaderCases()...)
	cs = append(cs, messageCodecCases()...)
	cs = append(cs, signingPayloadCases()...)
	cs = append(cs, dedupKeyCases()...)
	return cs
}

// ---------------------------------------------------------------- extra_codec

func sealField(s *types.WBFTAggregatedSeal) any {
	if s == nil {
		return nil
	}
	return M{{"sealers", hexs(s.Sealers)}, {"signature", hexs(s.Signature)}}
}

func epochField(e *types.EpochInfo) any {
	if e == nil {
		return nil
	}
	cands := make([]any, len(e.Candidates))
	for i, c := range e.Candidates {
		cands[i] = M{{"addr", hexs(c.Addr.Bytes())}, {"diligence", dec(c.Diligence)}}
	}
	vals := make([]any, len(e.Validators))
	for i, v := range e.Validators {
		vals[i] = dec(uint64(v))
	}
	return M{
		{"candidates", cands},
		{"validators", vals},
		{"bls_public_keys", listHex(e.BLSPublicKeys...)},
	}
}

// extraFields is the expected output of extra_codec: every decoded field
// (absent optional values are null) and the re-encoding.
func extraFields(e *types.WBFTExtra) M {
	gt := Dec{e.GasTip}
	return M{
		{"vanity_data", hexs(e.VanityData)},
		{"randao_reveal", hexs(e.RandaoReveal)},
		{"prev_round", dec(uint64(e.PrevRound))},
		{"prev_prepared_seal", sealField(e.PrevPreparedSeal)},
		{"prev_committed_seal", sealField(e.PrevCommittedSeal)},
		{"round", dec(uint64(e.Round))},
		{"prepared_seal", sealField(e.PreparedSeal)},
		{"committed_seal", sealField(e.CommittedSeal)},
		{"gas_tip", gt},
		{"epoch_info", epochField(e.EpochInfo)},
		{"encoded", hexs(enc(e))},
	}
}

func raw(h string) rlp.RawValue { return rlp.RawValue(common.FromHex(h)) }

// extraList encodes a WBFTExtra-shaped list from raw items, so that
// malformed items can be placed in any position.
func extraList(items ...rlp.RawValue) []byte {
	l := make([]any, len(items))
	for i, it := range items {
		l[i] = it
	}
	return enc(l)
}

// emptyItems returns the ten items of WBFTExtra{} (A-03 §9.1 first row).
func emptyItems() []rlp.RawValue {
	return []rlp.RawValue{raw("80"), raw("80"), raw("80"), raw("c0"), raw("c0"), raw("80"), raw("c0"), raw("c0"), raw("80"), raw("c0")}
}

func withItem(pos int, item rlp.RawValue) []byte {
	it := emptyItems()
	it[pos] = item
	return extraList(it...)
}

func extraCodecCases() []Case {
	candidate := func(addr []byte, dil rlp.RawValue) rlp.RawValue {
		return enc([]any{addr, dil})
	}
	ei := func(cands, vals, keys rlp.RawValue) rlp.RawValue { return enc([]any{cands, vals, keys}) }
	addr0 := fx.addrs[0].Bytes()
	gt2p256 := new(big.Int).Lsh(big.NewInt(1), 256)
	hcExtra, _ := types.ExtractWBFTExtra(fx.Hc)
	h3Extra, _ := types.ExtractWBFTExtra(fx.H3)

	// Inconsistent epoch info: 1 candidate, validators [0, 5], 1 key of 3 bytes.
	inconsistent := withItem(9, ei(enc([]any{candidate(addr0, raw("01"))}), enc([]uint32{0, 5}), enc([][]byte{{1, 2, 3}})))

	type in struct {
		name, desc string
		reqs       []string
		b          []byte
	}
	codec := []string{"WBFT-ENC-001", "WBFT-ENC-020", "WBFT-ENC-021", "WBFT-ENC-022"}
	ins := []in{
		// accepted
		{"empty", "WBFTExtra{}: everything absent or zero; gas_tip decodes to 0 (present) (A-03 §9.1)", append(codec, "WBFT-ENC-006", "WBFT-ENC-008"), common.FromHex("ca808080c0c080c0c080c0")},
		{"prepared_seal_present_empty", "prepared_seal present with empty sealers and signature (differs from absent)", append(codec, "WBFT-ENC-043"), common.FromHex("cc808080c0c080c28080c080c0")},
		{"proposal_h", "extra of the proposal H before sealing (A-03 §9.3)", codec, fx.H.Extra},
		{"committed_h", "extra of H after CommitHeader with seals of 0,1,2 (A-03 §9.3)", append(codec, "WBFT-ENC-040", "WBFT-ENC-041", "@r04"), fx.Hc.Extra},
		{"block3_prev_seals_epoch", "block 3: prev seals, round 1, seals of 1,2,3 (sealers 0e), gas_tip 1, epoch info", append(codec, "WBFT-ENC-050"), fx.H3.Extra},
		{"genesis_4_validators", "genesis extra of validators key0..key3 with INITIAL_GAS_TIP (A-03 §9.4)", append(codec, "WBFT-ENC-050", "WBFT-ENC-060"), fx.genExtra},
		{"epoch_info_empty_lists", "epoch_info = EpochInfo{} (c3c0c0c0)", append(codec, "WBFT-ENC-050"), withItem(9, raw("c3c0c0c0"))},
		{"epoch_info_inconsistent", "epoch_info whose lists are inconsistent (1 candidate, validators [0,5], one 3-byte key): decoding does not check consistency", append(codec, "WBFT-ENC-051"), inconsistent},
		{"vanity_100_bytes", "vanity_data of 100 bytes and randao_reveal of 3 bytes: any length accepted", append(codec, "WBFT-ENC-030"), func() []byte {
			it := emptyItems()
			it[0] = enc(pattern(100))
			it[1] = enc([]byte{1, 2, 3})
			return extraList(it...)
		}()},
		{"round_max", "round = 2^32 - 1 (4 bytes)", append(codec, "WBFT-ENC-010", "WBFT-ENC-032"), withItem(5, raw("84ffffffff"))},
		{"gas_tip_2pow256", "gas_tip = 2^256 (33 bytes): bigint of any length", append(codec, "WBFT-ENC-011", "WBFT-ENC-033"), withItem(8, enc(gt2p256))},
		{"diligence_max_uint64", "candidate diligence = 2^64 - 1", append(codec, "WBFT-ENC-050", "WBFT-TYPE-022"), withItem(9, ei(enc([]any{candidate(addr0, raw("88ffffffffffffffff"))}), raw("c0"), raw("c0")))},
		// rejected
		{"seal_0x80", "0x80 in prev_prepared_seal: wrong kind of empty value", append(codec, "WBFT-ENC-007"), common.FromHex("ca808080808080c0c080c0")},
		{"committed_seal_0x80", "0x80 in committed_seal", append(codec, "WBFT-ENC-007"), withItem(7, raw("80"))},
		{"epoch_info_0x80", "0x80 in epoch_info", append(codec, "WBFT-ENC-007"), withItem(9, raw("80"))},
		{"gas_tip_0xc0", "0xc0 in gas_tip", append(codec, "WBFT-ENC-008"), common.FromHex("ca808080c0c080c0c0c0c0")},
		{"prev_round_0x00", "prev_round = 0x00: non-canonical integer", append(codec, "WBFT-ENC-010"), common.FromHex("ca808000c0c080c0c080c0")},
		{"prev_round_2pow32", "prev_round = 2^32 (5 bytes): too long for uint32", append(codec, "WBFT-ENC-010", "WBFT-ENC-032"), common.FromHex("ce8080850100000000c0c080c0c080c0")},
		{"round_leading_zero", "round = 0x820001: leading zero byte", append(codec, "WBFT-ENC-010"), withItem(5, raw("820001"))},
		{"round_list", "round = 0xc0 (a list)", append(codec, "WBFT-ENC-010"), withItem(5, raw("c0"))},
		{"gas_tip_0x00", "gas_tip = 0x00: non-canonical integer", append(codec, "WBFT-ENC-011"), common.FromHex("ca808080c0c080c0c000c0")},
		{"gas_tip_0x8100", "gas_tip = 0x8100: non-canonical size", append(codec, "WBFT-ENC-002", "WBFT-ENC-011"), common.FromHex("cb808080c0c080c0c08100c0")},
		{"gas_tip_leading_zero", "gas_tip = 0x820001: leading zero byte", append(codec, "WBFT-ENC-011"), withItem(8, raw("820001"))},
		{"items_9", "9 items: too few elements", append(codec, "WBFT-ENC-004"), common.FromHex("c9808080c0c080c0c080")},
		{"items_11", "11 items: too many elements", append(codec, "WBFT-ENC-004"), common.FromHex("cb808080c0c080c0c080c080")},
		{"trailing_byte", "valid list followed by 00", append(codec, "WBFT-ENC-003"), common.FromHex("ca808080c0c080c0c080c000")},
		{"not_a_list", "0x80 (an empty string)", codec, common.FromHex("80")},
		{"empty_input", "zero-length extra", codec, nil},
		{"single_byte_string_noncanonical", "vanity_data encoded as 8105 (single byte below 0x80 as a one-byte string)", append(codec, "WBFT-ENC-002"), withItem(0, raw("8105"))},
		{"long_form_below_56", "outer list with long-form length f80a (< 56)", append(codec, "WBFT-ENC-002"), append([]byte{0xf8, 0x0a}, common.FromHex("ca808080c0c080c0c080c0")[1:]...)},
		{"length_exceeds_input", "outer list header claims one more byte than present", append(codec, "WBFT-ENC-002"), common.FromHex("cb808080c0c080c0c080c0")},
		{"agg_seal_one_item", "prepared_seal with one item", append(codec, "WBFT-ENC-040"), withItem(6, raw("c180"))},
		{"agg_seal_three_items", "prepared_seal with three items", append(codec, "WBFT-ENC-040"), withItem(6, raw("c3808080"))},
		{"epoch_info_two_items", "epoch_info with two lists", append(codec, "WBFT-ENC-050"), withItem(9, raw("c2c0c0"))},
		{"candidate_empty_list", "epoch_info with a candidate element 0xc0", append(codec, "WBFT-ENC-050"), withItem(9, ei(raw("c1c0"), raw("c0"), raw("c0")))},
		{"candidate_addr_19", "candidate address of 19 bytes", append(codec, "WBFT-ENC-005", "WBFT-ENC-050", "WBFT-TYPE-001"), withItem(9, ei(enc([]any{candidate(addr0[:19], raw("01"))}), raw("c0"), raw("c0")))},
		{"candidate_addr_21", "candidate address of 21 bytes", append(codec, "WBFT-ENC-005", "WBFT-ENC-050", "WBFT-TYPE-001"), withItem(9, ei(enc([]any{candidate(append(append([]byte{}, addr0...), 0), raw("01"))}), raw("c0"), raw("c0")))},
		{"diligence_9_bytes", "candidate diligence of 9 bytes", append(codec, "WBFT-ENC-010", "WBFT-ENC-050"), withItem(9, ei(enc([]any{candidate(addr0, raw("89010000000000000000"))}), raw("c0"), raw("c0")))},
		{"validator_index_5_bytes", "epoch_info validators item of 5 bytes (> uint32)", append(codec, "WBFT-ENC-010", "WBFT-ENC-050"), withItem(9, ei(raw("c0"), raw("c6850100000000"), raw("c0")))},
		{"bls_keys_not_bytes", "epoch_info bls_public_keys containing a list", append(codec, "WBFT-ENC-050"), withItem(9, ei(raw("c0"), raw("c0"), raw("c1c0")))},
	}
	_ = hcExtra
	_ = h3Extra
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "encoding", Handler: "extra_codec", Name: x.name, Kind: "pure",
			Desc:  "decode_extra(extra) and encode_extra of the result: " + x.desc,
			Reqs:  x.reqs,
			Input: M{{"extra", hexs(x.b)}},
		}
		e := new(types.WBFTExtra)
		if err := rlp.DecodeBytes(x.b, e); err != nil {
			c.Err = err.Error()
		} else {
			if !bytes.Equal(enc(e), x.b) {
				panic("extra_codec: re-encoding differs for " + x.name)
			}
			c.Expected = extraFields(e)
		}
		cs = append(cs, c)
	}
	return cs
}

// ---------------------------------------------------------------- header hashes

type hdrCase struct {
	name, desc string
	reqs       []string
	h          *types.Header
}

func headerVariants() []hdrCase {
	d2 := types.CopyHeader(fx.H)
	d2.Difficulty = big.NewInt(2)
	emptyExtra := &types.Header{Difficulty: big.NewInt(1), Number: big.NewInt(1)}
	d0bad := types.CopyHeader(fx.Hbad)
	d0bad.Difficulty = big.NewInt(0)
	genesis := &types.Header{
		UncleHash:   types.EmptyUncleHash,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(0),
		GasLimit:    105000000,
		Extra:       fx.genExtra,
		BaseFee:     new(big.Int).SetUint64(params.InitialBaseFee),
	}
	noBaseFee := types.CopyHeader(fx.H)
	noBaseFee.BaseFee = nil
	return []hdrCase{
		{"proposal_h", "proposal header H of A-02 §8.4", nil, fx.H},
		{"committed_h", "H after CommitHeader (seals present): same hash as H", []string{"@r05"}, fx.Hc},
		{"block3_round1", "block 3 with prev seals and epoch info, committed at round 1", []string{"@r05"}, fx.H3},
		{"genesis_like", "number 0, genesis extra of 4 validators, difficulty 1", nil, genesis},
		{"difficulty_2", "H with Difficulty = 2: Ethereum rule keccak256(rlp(header))", []string{"WBFT-PARAM-010"}, d2},
		{"undecodable_extra", "H with a trailing byte in Extra, Difficulty = 1: falls back to keccak256(rlp(header))", []string{"WBFT-ENC-084"}, fx.Hbad},
		{"undecodable_extra_difficulty_0", "undecodable Extra and Difficulty = 0", []string{"WBFT-ENC-084"}, d0bad},
		{"empty_extra", "Extra empty, Difficulty = 1, other fields zero (15-item header)", []string{"WBFT-ENC-084"}, emptyExtra},
		{"no_base_fee", "H without BaseFee (15-item header)", []string{"WBFT-ENC-070"}, noBaseFee},
	}
}

func blockHashCases() []Case {
	var cs []Case
	for _, x := range headerVariants() {
		cs = append(cs, Case{
			Runner: "encoding", Handler: "block_hash", Name: x.name, Kind: "pure",
			Desc:     "block_hash(header): keccak256(rlp(filtered_header(header, 0))) if Difficulty = 1 and the extra decodes, else keccak256(rlp(header)): " + x.desc,
			Reqs:     append([]string{"WBFT-ENC-070", "WBFT-ENC-082"}, x.reqs...),
			Input:    M{{"header", hexs(enc(x.h))}},
			Expected: M{{"hash", hexs(x.h.Hash().Bytes())}},
		})
	}
	return cs
}

func hashWithRoundCases() []Case {
	var cs []Case
	for _, x := range headerVariants() {
		for _, r := range []uint32{0, 1, 0xffffffff} {
			name := x.name + "_r" + dec(uint64(r)).String()
			if r == 0xffffffff {
				name = x.name + "_rmax"
			}
			cs = append(cs, Case{
				Runner: "encoding", Handler: "hash_with_round", Name: name, Kind: "pure",
				Desc:     "hash_with_round(header, round) = keccak256(rlp(filtered_header(header, round))); keccak256(0xc0) when the extra does not decode; independent of Difficulty: " + x.desc,
				Reqs:     []string{"WBFT-ENC-081"},
				Input:    M{{"header", hexs(enc(x.h))}, {"round", dec(uint64(r))}},
				Expected: M{{"hash", hexs(x.h.WBFTHashWithRoundNumber(r).Bytes())}},
			})
		}
	}
	return cs
}

func filteredHeaderCases() []Case {
	var cs []Case
	for _, x := range headerVariants() {
		for _, r := range []uint32{0, 5} {
			c := Case{
				Runner: "encoding", Handler: "filtered_header", Name: x.name + "_r" + dec(uint64(r)).String(), Kind: "pure",
				Desc:  "filtered_header(header, round): Extra re-encoded without prepared_seal and committed_seal and with round replaced; fails when the extra does not decode: " + x.desc,
				Reqs:  []string{"WBFT-ENC-080", "WBFT-ENC-090"},
				Input: M{{"header", hexs(enc(x.h))}, {"round", dec(uint64(r))}},
			}
			fh := types.WBFTFilteredHeaderWithRound(x.h, r)
			if fh == nil {
				_, err := types.ExtractWBFTExtra(x.h)
				c.Err = "extra does not decode: " + err.Error()
			} else {
				c.Expected = M{{"header", hexs(enc(fh))}}
			}
			cs = append(cs, c)
		}
	}
	return cs
}
