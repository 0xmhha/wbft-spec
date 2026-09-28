// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `encoding`, message handlers (A-03 §8, A-07 §5): message_codec,
// signing_payload, dedup_key.

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	"github.com/ethereum/go-ethereum/consensus/wbft/messages"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

var codeName = map[uint64]string{
	messages.PreprepareCode:  "PREPREPARE",
	messages.PrepareCode:     "PREPARE",
	messages.CommitCode:      "COMMIT",
	messages.RoundChangeCode: "ROUND_CHANGE",
}

type msgFixture struct {
	block              *types.Block
	prep, com          []byte // PREPARE by key 1, COMMIT by key 2 (A-03 §9.6)
	rcNone, rcPrepared []byte // ROUND-CHANGE by key 3, round 1
	pp0, pp1           []byte // PRE-PREPARE (2,0); PRE-PREPARE (2,1) with justification
	prepBig            []byte // PREPARE with sequence 2^64+1 and round 2^32+3
	prepObj            *messages.Prepare
}

func signMsg(k int, m messages.WBFTMessage) {
	p, err := m.EncodePayloadForSigning()
	must(err)
	m.SetSignature(ecdsaSign(fx.keys[k], p))
}

func newPrepare(k int, seq, round *big.Int, digest common.Hash, seal []byte) *messages.Prepare {
	p := messages.NewPrepare(seq, round, digest, seal)
	signMsg(k, p)
	return p
}

var mf = newMsgFixture()

func newMsgFixture() *msgFixture {
	f := &msgFixture{}
	f.block = types.NewBlockWithHeader(fx.H)
	seq, r0, r1 := big.NewInt(2), big.NewInt(0), big.NewInt(1)
	digest := fx.H.Hash()

	f.prepObj = newPrepare(1, seq, r0, digest, fx.pSeals[1].Seal)
	f.prep = enc(f.prepObj)

	com := messages.NewCommit(seq, r0, digest, fx.cSeals[2].Seal)
	signMsg(2, com)
	f.com = enc(com)

	rc := messages.NewRoundChange(seq, r1, nil, nil)
	signMsg(3, rc)
	f.rcNone = enc(rc)

	rc2 := messages.NewRoundChange(seq, r1, r0, f.block)
	rc2.Justification = []*messages.Prepare{f.prepObj}
	signMsg(3, rc2)
	f.rcPrepared = enc(rc2)

	pp := messages.NewPreprepare(seq, r0, f.block)
	signMsg(0, pp)
	f.pp0 = enc(pp)

	// PRE-PREPARE for round 1 (proposer of round 1 is assumed to be key 1;
	// the proposer choice is not part of the encoding), justified by round
	// changes of keys 1,2,3 prepared at round 0 and prepares of keys 0,1,2.
	pp1 := messages.NewPreprepare(seq, r1, f.block)
	for k := 1; k <= 3; k++ {
		rcx := messages.NewRoundChange(seq, r1, r0, f.block)
		signMsg(k, rcx)
		pp1.JustificationRoundChanges = append(pp1.JustificationRoundChanges, &rcx.SignedRoundChangePayload)
	}
	for k := 0; k < 3; k++ {
		pp1.JustificationPrepares = append(pp1.JustificationPrepares, newPrepare(k, seq, r0, digest, fx.pSeals[k].Seal))
	}
	signMsg(1, pp1)
	f.pp1 = enc(pp1)

	two64p1 := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	two32p3 := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 32), big.NewInt(3))
	f.prepBig = enc(newPrepare(0, two64p1, two32p3, digest,
		fx.blsKeys[0].Sign(wbftcore.PrepareSeal(fx.H, 3, wbftcore.SealTypePrepare)).Marshal()))
	return f
}

// prepareWire builds a PREPARE/COMMIT-shaped wire from raw items.
func prepareWire(seq, round, digest, seal, sig rlp.RawValue) []byte {
	return enc([]any{[]any{seq, round, digest, seal}, sig})
}

func bigOrNull(b *big.Int) Dec { return Dec{b} }

// decodedFields is the expected output of message_codec.
func decodedFields(m messages.WBFTMessage) M {
	out := M{{"type", codeName[m.Code()]}}
	switch x := m.(type) {
	case *messages.Prepare:
		out = append(out, M{
			{"sequence", bigOrNull(x.Sequence)}, {"round", bigOrNull(x.Round)},
			{"digest", hexs(x.Digest.Bytes())}, {"seal", hexs(x.PrepareSeal)},
			{"signature", hexs(x.Signature())},
		}...)
	case *messages.Commit:
		out = append(out, M{
			{"sequence", bigOrNull(x.Sequence)}, {"round", bigOrNull(x.Round)},
			{"digest", hexs(x.Digest.Bytes())}, {"seal", hexs(x.CommitSeal)},
			{"signature", hexs(x.Signature())},
		}...)
	case *messages.RoundChange:
		var pb any
		if x.PreparedBlock != nil {
			pb = hexs(enc(x.PreparedBlock))
		}
		just := make([]any, len(x.Justification))
		for i, p := range x.Justification {
			just[i] = hexs(enc(p))
		}
		out = append(out, M{
			{"sequence", bigOrNull(x.Sequence)}, {"round", bigOrNull(x.Round)},
			{"prepared_round", bigOrNull(x.PreparedRound)},
			{"prepared_digest", hexs(x.PreparedDigest.Bytes())},
			{"signature", hexs(x.Signature())},
			{"prepared_block", pb},
			{"justification", just},
		}...)
	case *messages.Preprepare:
		rcs := make([]any, len(x.JustificationRoundChanges))
		for i, p := range x.JustificationRoundChanges {
			rcs[i] = hexs(enc(p))
		}
		ps := make([]any, len(x.JustificationPrepares))
		for i, p := range x.JustificationPrepares {
			ps[i] = hexs(enc(p))
		}
		out = append(out, M{
			{"sequence", bigOrNull(x.Sequence)}, {"round", bigOrNull(x.Round)},
			{"proposal", hexs(enc(x.Proposal))},
			{"signature", hexs(x.Signature())},
			{"justification_round_changes", rcs},
			{"justification_prepares", ps},
		}...)
	default:
		panic(fmt.Sprintf("unexpected message type %T", m))
	}
	return append(out, KV{"encoded", hexs(enc(m))})
}

type msgIn struct {
	name, desc string
	reqs       []string
	code       uint64
	payload    []byte
}

func messageCodecCases() []Case {
	digest := fx.H.Hash()
	sig := rlp.RawValue(enc(make([]byte, 65)))
	seal := rlp.RawValue(enc(fx.pSeals[1].Seal))
	dg := rlp.RawValue(enc(digest.Bytes()))
	// ROUND-CHANGE edge cases of A-03 §9.6: signed payload c6 c30201c0 81aa.
	signed := []any{raw("c30201c0"), []byte{0xaa}}
	rcTail := func(tail ...string) []byte {
		items := []any{signed}
		for _, t := range tail {
			items = append(items, raw(t))
		}
		return enc(items)
	}
	rcPrepared := func(prepared rlp.RawValue) []byte {
		payload := enc([]any{uint64(2), uint64(1), prepared})
		return enc([]any{[]any{rlp.RawValue(payload), []byte{0xaa}}, raw("c0"), raw("c0")})
	}
	// ROUND-CHANGE with the block of H but a different prepared digest.
	rcMismatch := func() []byte {
		rc := messages.NewRoundChange(big.NewInt(2), big.NewInt(1), big.NewInt(0), mf.block)
		rc.PreparedDigest = common.HexToHash("0x01")
		signMsg(3, rc)
		return enc(rc)
	}()
	// PRE-PREPARE with an absent proposal (A-03 §9.6).
	ppNil := common.FromHex("c9c5c30280c080c2c0c0")
	// PRE-PREPARE (2,0) whose justification is a one-item list.
	ppOuter := func(just rlp.RawValue) []byte {
		var l []rlp.RawValue
		must(rlp.DecodeBytes(mf.pp0, &l))
		return enc([]any{l[0], just})
	}
	rcNoneSigned := func() rlp.RawValue {
		var l []rlp.RawValue
		must(rlp.DecodeBytes(mf.rcNone, &l))
		return l[0] // [rc_payload, signature]
	}()

	msgReqs := []string{"WBFT-MSG-001", "WBFT-MSG-004"}
	prepReqs := append([]string{"WBFT-MSG-010", "WBFT-MSG-011"}, msgReqs...)
	rcReqs := append([]string{"WBFT-MSG-030", "WBFT-MSG-031", "WBFT-MSG-032"}, msgReqs...)
	ppReqs := append([]string{"WBFT-MSG-040", "WBFT-MSG-041"}, msgReqs...)
	ins := []msgIn{
		{"prepare_key1", "PREPARE by key 1 for (2, 0) with prepare seal 1 of A-02 §8.4 (A-03 §9.6)", prepReqs, messages.PrepareCode, mf.prep},
		{"commit_key2", "COMMIT by key 2 for (2, 0) (A-03 §9.6)", append([]string{"WBFT-MSG-020"}, msgReqs...), messages.CommitCode, mf.com},
		{"prepare_big_view", "PREPARE with sequence 2^64+1 and round 2^32+3 (bigint fields)", append(prepReqs, "WBFT-TYPE-010", "WBFT-TYPE-012"), messages.PrepareCode, mf.prepBig},
		{"prepare_as_commit", "PREPARE payload delivered with code 0x14 decodes as a COMMIT", append([]string{"WBFT-MSG-020"}, msgReqs...), messages.CommitCode, mf.prep},
		{"prepare_zero_view_empty_fields", "PREPARE (0, 0) with empty seal and empty signature: lengths not checked by the decoder", prepReqs, messages.PrepareCode, prepareWire(raw("80"), raw("80"), dg, raw("80"), raw("80"))},
		{"prepare_empty_list", "PREPARE payload 0xc0", prepReqs, messages.PrepareCode, common.FromHex("c0")},
		{"prepare_trailing_byte", "valid PREPARE followed by 00", append(prepReqs, "WBFT-ENC-003"), messages.PrepareCode, append(append([]byte{}, mf.prep...), 0)},
		{"prepare_digest_31", "PREPARE with a 31-byte digest", append(prepReqs, "WBFT-TYPE-002"), messages.PrepareCode, prepareWire(raw("02"), raw("80"), enc(digest.Bytes()[:31]), seal, sig)},
		{"prepare_sequence_0x00", "PREPARE with sequence 0x00 (non-canonical)", prepReqs, messages.PrepareCode, prepareWire(raw("00"), raw("80"), dg, seal, sig)},
		{"prepare_five_inner_items", "PREPARE whose inner list has five items", prepReqs, messages.PrepareCode, enc([]any{[]any{raw("02"), raw("80"), dg, seal, raw("80")}, sig})},
		{"prepare_three_outer_items", "PREPARE with an extra outer item", prepReqs, messages.PrepareCode, enc([]any{[]any{raw("02"), raw("80"), dg, seal}, sig, raw("80")})},
		{"prepare_as_preprepare", "PREPARE payload delivered with code 0x12", ppReqs, messages.PreprepareCode, mf.prep},
		{"code_0x11", "code 0x11 (legacy ISTANBUL_MSG) is not decoded as a consensus message", append([]string{"WBFT-MSG-050", "WBFT-PARAM-002"}, msgReqs...), 0x11, mf.prep},
		{"code_0x16", "code 0x16 is not a consensus message", append([]string{"WBFT-PARAM-001"}, msgReqs...), 0x16, mf.prep},
		{"round_change_none", "ROUND-CHANGE by key 3 for round 1, nothing prepared (A-03 §9.6)", rcReqs, messages.RoundChangeCode, mf.rcNone},
		{"round_change_prepared", "ROUND-CHANGE by key 3 for round 1, prepared at round 0 with block H and the PREPARE of key 1 as justification (A-03 §9.6)", rcReqs, messages.RoundChangeCode, mf.rcPrepared},
		{"round_change_tail_c0_c0", "signed payload c6c30201c081aa, tail c0 c0: accepted, re-encodes to itself", rcReqs, messages.RoundChangeCode, rcTail("c0", "c0")},
		{"round_change_tail_80_80", "tail 80 80: both absent; re-encodes with c0 c0", append(rcReqs, "WBFT-ENC-090"), messages.RoundChangeCode, rcTail("80", "80")},
		{"round_change_tail_single_bytes", "tail 05 00: single bytes treated as absent; re-encodes with c0 c0", append(rcReqs, "WBFT-ENC-090"), messages.RoundChangeCode, rcTail("05", "00")},
		{"round_change_missing_item", "only two outer items", rcReqs, messages.RoundChangeCode, rcTail("c0")},
		{"round_change_extra_item", "four outer items", rcReqs, messages.RoundChangeCode, rcTail("c0", "c0", "c0")},
		{"round_change_prepared_one_item", "prepared = c180 (one item)", rcReqs, messages.RoundChangeCode, rcPrepared(raw("c180"))},
		{"round_change_prepared_three_items", "prepared = [0, 0x00..00, 1] (three items)", rcReqs, messages.RoundChangeCode, rcPrepared(enc([]any{uint64(0), common.Hash{}, uint64(1)}))},
		{"round_change_prepared_zero_digest", "prepared = [0, 0x00..00]: accepted; re-encodes with prepared = []", append(rcReqs, "WBFT-ENC-090", "WBFT-MSG-033"), messages.RoundChangeCode, rcPrepared(enc([]any{uint64(0), common.Hash{}}))},
		{"round_change_prepared_no_block", "prepared = [1, 0x00..01] without a block: accepted", append(rcReqs, "WBFT-MSG-033"), messages.RoundChangeCode, rcPrepared(enc([]any{uint64(1), common.HexToHash("0x01")}))},
		{"round_change_digest_mismatch", "prepared block H with prepared digest 0x00..01", rcReqs, messages.RoundChangeCode, rcMismatch},
		{"preprepare_round0", "PRE-PREPARE by key 0 for (2, 0) with block H and empty justification (A-03 §9.6)", append(ppReqs, "WBFT-ENC-071"), messages.PreprepareCode, mf.pp0},
		{"preprepare_round1_justified", "PRE-PREPARE for (2, 1) with three round changes and three prepares as justification", append(ppReqs, "WBFT-MSG-034"), messages.PreprepareCode, mf.pp1},
		{"preprepare_nil_proposal", "PRE-PREPARE with an absent proposal c0 (A-03 §9.6)", ppReqs, messages.PreprepareCode, ppNil},
		{"preprepare_justification_one_list", "justification with one list instead of two", ppReqs, messages.PreprepareCode, ppOuter(raw("c1c0"))},
		{"preprepare_rc_item_missing_signature", "justification round-change item [rc_payload] without signature", ppReqs, messages.PreprepareCode, ppOuter(enc([]any{[]any{[]any{raw("c30201c0")}}, []any{}}))},
		{"preprepare_rc_item_extra_element", "justification round-change item with an extra element", ppReqs, messages.PreprepareCode, ppOuter(enc([]any{[]any{[]any{raw("c30201c0"), []byte{0xaa}, raw("80")}}, []any{}}))},
		{"preprepare_rc_item_ok", "justification with the signed payload of round_change_none (accepted)", append(ppReqs, "WBFT-MSG-034"), messages.PreprepareCode, ppOuter(enc([]any{[]any{rcNoneSigned}, []any{}}))},
		{"preprepare_bad_prepare_item", "justification prepares containing 0xc0", ppReqs, messages.PreprepareCode, ppOuter(enc([]any{[]any{}, []any{raw("c0")}}))},
	}
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "encoding", Handler: "message_codec", Name: x.name, Kind: "pure",
			Desc:  "decode the devp2p payload with the decoder for the code, then re-encode: " + x.desc,
			Reqs:  x.reqs,
			Input: M{{"code", dec(x.code)}, {"payload", hexs(x.payload)}},
		}
		m, err := messages.Decode(x.code, x.payload)
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = decodedFields(m)
		}
		cs = append(cs, c)
	}
	return cs
}

// signer computes the signing payload of a decoded message and recovers its
// signer.
func signer(m messages.WBFTMessage) ([]byte, common.Address, error) {
	p, err := m.EncodePayloadForSigning()
	if err != nil {
		return nil, common.Address{}, err
	}
	a, err := wbft.GetSignatureAddress(p, m.Signature())
	return p, a, err
}

func signingPayloadRef(code uint64, payload []byte) (M, error) {
	m, err := messages.Decode(code, payload)
	if err != nil {
		return nil, err
	}
	p, a, err := signer(m)
	if err != nil {
		return nil, err
	}
	var embedded []messages.WBFTMessage
	switch x := m.(type) {
	case *messages.RoundChange:
		for _, j := range x.Justification {
			embedded = append(embedded, j)
		}
	case *messages.Preprepare:
		for _, j := range x.JustificationRoundChanges {
			embedded = append(embedded, j)
		}
		for _, j := range x.JustificationPrepares {
			embedded = append(embedded, j)
		}
	}
	var emb []any
	for _, e := range embedded {
		ep, ea, err := signer(e)
		if err != nil {
			return nil, fmt.Errorf("embedded: %w", err)
		}
		emb = append(emb, M{{"signing_payload", hexs(ep)}, {"signer", hexs(ea.Bytes())}})
	}
	if emb == nil {
		emb = []any{}
	}
	return M{{"signing_payload", hexs(p)}, {"signer", hexs(a.Bytes())}, {"embedded", emb}}, nil
}

func signingPayloadCases() []Case {
	// ROUND-CHANGE by key 3 whose prepared list is [0, 0x00..00]: the
	// reference signs and verifies the re-encoded payload (prepared = []).
	rcZero := func() []byte {
		payload := enc([]any{uint64(2), uint64(1), []any{uint64(0), common.Hash{}}})
		sp := enc([]any{uint64(messages.RoundChangeCode), rlp.RawValue(payload)})
		return enc([]any{[]any{rlp.RawValue(payload), ecdsaSign(fx.keys[3], sp)}, raw("c0"), raw("c0")})
	}()
	// PREPARE of key 1 with a high-S signature twin.
	prepHighS := func() []byte {
		var l []rlp.RawValue
		must(rlp.DecodeBytes(mf.prep, &l))
		var s []byte
		must(rlp.DecodeBytes(l[1], &s))
		sv := new(big.Int).Sub(secpN, new(big.Int).SetBytes(s[32:64]))
		return enc([]any{l[0], withRSV(s, nil, sv, s[64]^1)})
	}()
	prepBadSig := func() []byte {
		var l []rlp.RawValue
		must(rlp.DecodeBytes(mf.prep, &l))
		return enc([]any{l[0], make([]byte, 65)})
	}()
	prepShortSig := func() []byte {
		var l []rlp.RawValue
		must(rlp.DecodeBytes(mf.prep, &l))
		return enc([]any{l[0], []byte{0xaa}})
	}()

	base := []string{"WBFT-MSG-002", "WBFT-MSG-003", "WBFT-CRYPTO-013"}
	ins := []msgIn{
		{"prepare_key1", "PREPARE by key 1 (signing payload of A-03 §9.6)", append(base, "WBFT-MSG-010"), messages.PrepareCode, mf.prep},
		{"commit_key2", "COMMIT by key 2", append(base, "WBFT-MSG-020"), messages.CommitCode, mf.com},
		{"prepare_as_commit", "PREPARE payload with code 0x14: payload starts with 0x14, so the recovered signer is not key 1", append(base, "WBFT-MSG-020"), messages.CommitCode, mf.prep},
		{"prepare_big_view", "PREPARE by key 0 with sequence 2^64+1, round 2^32+3", base, messages.PrepareCode, mf.prepBig},
		{"prepare_high_s", "PREPARE of key 1 with its high-S signature twin: same signer", append(base, "WBFT-CRYPTO-014"), messages.PrepareCode, prepHighS},
		{"prepare_zero_signature", "PREPARE with 65 zero bytes as signature: recovery fails", base, messages.PrepareCode, prepBadSig},
		{"prepare_short_signature", "PREPARE with a 1-byte signature", append(base, "WBFT-TYPE-003"), messages.PrepareCode, prepShortSig},
		{"round_change_none", "ROUND-CHANGE by key 3, nothing prepared (payload c515c30201c0)", append(base, "WBFT-MSG-030"), messages.RoundChangeCode, mf.rcNone},
		{"round_change_prepared", "ROUND-CHANGE by key 3 prepared at round 0 (prepared = [0x80, digest]) with one embedded PREPARE", append(base, "WBFT-MSG-030"), messages.RoundChangeCode, mf.rcPrepared},
		{"round_change_prepared_zero_digest", "ROUND-CHANGE signed by key 3 over prepared = [0, 0x00..00]; the reference re-encodes prepared = [] and recovers a different signer", append(base, "WBFT-ENC-090"), messages.RoundChangeCode, rcZero},
		{"preprepare_round0", "PRE-PREPARE by key 0 for (2, 0): payload holds the complete block", append(base, "WBFT-MSG-040"), messages.PreprepareCode, mf.pp0},
		{"preprepare_round1_justified", "PRE-PREPARE by key 1 for (2, 1): three embedded round changes, then three embedded prepares", append(base, "WBFT-MSG-034", "WBFT-MSG-040"), messages.PreprepareCode, mf.pp1},
	}
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "encoding", Handler: "signing_payload", Name: x.name, Kind: "pure",
			Desc:  "decode the payload, compute message_signing_payload = rlp([code, signed_fields]) from the decoded values and recover the signer; the same for every embedded signed payload (round-change justification prepares; pre-prepare round changes then prepares): " + x.desc,
			Reqs:  x.reqs,
			Input: M{{"code", dec(x.code)}, {"payload", hexs(x.payload)}},
		}
		out, err := signingPayloadRef(x.code, x.payload)
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = out
		}
		cs = append(cs, c)
	}
	return cs
}

func dedupKeyCases() []Case {
	type in struct {
		name, desc string
		p          []byte
	}
	ins := []in{
		{"prepare_key1", "PREPARE wire of A-03 §9.6 (204 bytes)", mf.prep},
		{"round_change_none", "ROUND-CHANGE wire (77 bytes)", mf.rcNone},
		{"preprepare_round0", "PRE-PREPARE wire (714 bytes)", mf.pp0},
		{"empty", "empty payload: rlp string 0x80", nil},
		{"single_byte_7f", "single byte 0x7f: encoded as itself", []byte{0x7f}},
		{"single_byte_80", "single byte 0x80: encoded as 0x81 0x80", []byte{0x80}},
		{"len55", "55 bytes: short string header 0xb7", pattern(55)},
		{"len56", "56 bytes: long string header 0xb8 0x38", pattern(56)},
		{"len256", "256 bytes: long string header 0xb9 0x0100", pattern(256)},
	}
	var cs []Case
	for _, x := range ins {
		cs = append(cs, Case{
			Runner: "encoding", Handler: "dedup_key", Name: x.name, Kind: "pure",
			Desc:     "dedup_key(payload) = keccak256(rlp_encode_string(payload)): " + x.desc,
			Reqs:     []string{"WBFT-MSG-060", "WBFT-NET-022"},
			Input:    M{{"payload", hexs(x.p)}},
			Expected: M{{"key", hexs(wbft.RLPHash(x.p).Bytes())}},
		})
	}
	_ = crypto.Keccak256
	return cs
}
