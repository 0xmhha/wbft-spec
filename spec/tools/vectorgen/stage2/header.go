// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Chain fixtures and the handlers that use them through exported reference
// functions: validators/validators_at, header/verify_header,
// header/verify_light, header/build_proposal_header (A-04 §3, A-08).

import (
	"errors"
	"fmt"
	"strings"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/consensus/wbft/backend"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/rlp"
)

// ------------------------------------------------------------- fixtures

func specA(t0 uint64) cfgSpec {
	return cfgSpec{Init: []int{0, 1, 2, 3}, W: wbftP{RequestTimeoutSeconds: 2, BlockPeriodSeconds: 1, EpochLength: 4, ProposerPolicy: u64(0)}}
}

// chainA: 4 genesis validators (keys 0..3), epoch length 4. Block 4 records
// candidates keys 0..4 and validators [3, 1, 4, 0], so blocks 5 and later are
// sealed by another ordered set than blocks 1..4.
func chainA(t0 uint64, upTo int, epoch4 bool) *builder {
	b := newBuilder(specA(t0), t0, nil)
	plan := []struct {
		key     int
		round   uint32
		sealers []int
	}{
		{0, 0, []int{0, 1, 2}},
		{1, 1, []int{0, 1, 2, 3}},
		{2, 0, []int{1, 2, 3}},
		{3, 0, []int{0, 1, 2}},
		{4, 0, []int{0, 1, 2}}, // V(5) = [key3, key1, key4, key0]; key4 is index 2
		{0, 0, []int{0, 1, 2, 3}},
		{3, 0, []int{0, 1, 2}},
	}
	for i := 0; i < upTo; i++ {
		var ei *types.EpochInfo
		if i+1 == 4 && epoch4 {
			ei = epochInfo([]int{0, 1, 2, 3, 4}, []int{3, 1, 4, 0})
		}
		p := plan[i]
		b.block(p.key, p.round, p.sealers, ei)
	}
	return b
}

// forkBuilder: blocks 1..3 as in chainA, then a branch 4', 5' in which the
// epoch block 4' records candidates and validators keys 0..3 (in this order).
// Its headers differ from chainA's 4 and 5 and are stored as non-canonical.
func forkBuilder() *builder {
	b := chainA(pastTime, 3, true)
	b.block(3, 0, []int{0, 1, 2}, epochInfo([]int{0, 1, 2, 3}, []int{0, 1, 2, 3}))
	b.block(0, 0, []int{0, 1, 2}, nil)
	return b
}

// forkFixture returns chainA (canonical 1..7, or 1..3 only when !withCanonical)
// with the branch 4', 5' of forkBuilder as non-canonical headers.
func forkFixture(withCanonical bool) fixture {
	x := chainA(pastTime, 7, true).fixture()
	if !withCanonical {
		x = x.keep(1, 2, 3)
	}
	fb := forkBuilder()
	for _, h := range fb.headers[3:] {
		x.nonCanonical = append(x.nonCanonical, enc(h))
	}
	return x
}

// cancelKey returns the BLS secret key c = -(sk0 + sk1) mod r, so that the
// public keys of accounts 0 and 1 and c sum to the point at infinity.
func cancelKey() bls.SecretKey {
	r, _ := new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
	a := new(big.Int).SetBytes(accts[0].bls.Marshal())
	b := new(big.Int).SetBytes(accts[1].bls.Marshal())
	c := new(big.Int).Neg(new(big.Int).Add(a, b))
	c.Mod(c, r)
	buf := make([]byte, 32)
	c.FillBytes(buf)
	sk, err := bls.SecretKeyFromBytes(buf)
	must(err)
	return sk
}

// chainInf: genesis validators keys 0..3, but the BLS key of validator 2 is
// c = -(sk0 + sk1): the keys of validators 0, 1, 2 sum to infinity.
func chainInf() *builder {
	c := cancelKey()
	s := specA(pastTime)
	s.InitBLS = map[int][]byte{2: c.PublicKey().Marshal()}
	return newBuilder(s, pastTime, map[int]bls.SecretKey{2: c})
}

// ------------------------------------------------------------ helpers

func setExtra(h *types.Header, f func(x *types.WBFTExtra)) {
	x, err := types.ExtractWBFTExtra(h)
	must(err)
	f(x)
	h.Extra = enc(x)
}

func randaoData(chainID, number *big.Int) []byte {
	var d []byte
	d = append(d, chainID.Bytes()...)
	d = append(d, 0x01)
	d = append(d, number.Bytes()...)
	return crypto.Keccak256(d)
}

// resignRandao writes a reveal signed by account `key` over the randao data
// of `number` and the matching mix digest.
func resignRandao(b *builder, h *types.Header, key int, number *big.Int) {
	rev, err := crypto.Sign(crypto.Keccak256(randaoData(b.cc.ChainID, number)), accts[key].key)
	must(err)
	setExtra(h, func(x *types.WBFTExtra) { x.RandaoReveal = rev })
	parent := b.ch.GetHeaderByHash(h.ParentHash)
	h.MixDigest = wbftengine.CalculateRandaoMix(parent.MixDigest, rev)
}

// variant proposes the next block with account `key`, applies pre (before
// sealing), seals it (unless sealers is nil) and applies post.
func (b *builder) variant(key int, round uint32, sealers []int, pre, post func(h *types.Header)) *types.Header {
	h := b.propose(b.head(), key)
	if pre != nil {
		pre(h)
	}
	if sealers != nil {
		b.seal(h, round, sealers)
	}
	if post != nil {
		post(h)
	}
	return h
}

func aggSeal(seals []wbft.SealData) *types.WBFTAggregatedSeal {
	var sigs [][]byte
	var s types.SealerSet
	for _, x := range seals {
		s.SetSealer(x.Sealer)
		sigs = append(sigs, x.Seal)
	}
	agg, err := bls.AggregateCompressedSignatures(sigs)
	must(err)
	return &types.WBFTAggregatedSeal{Sealers: s, Signature: agg.Marshal()}
}

func verifier(x fixture) (*backend.Backend, *fxChain) {
	ch := x.reader()
	be := backend.New(wbftConfig(ch.cc), accts[15].key, rawdb.NewMemoryDatabase())
	return be, ch
}

// ------------------------------------------------------- validators_at

func validatorsAtCases() []Case {
	a := chainA(pastTime, 7, true).fixture()
	c := chainA(pastTime, 4, false).fixture()
	polSpec := specA(pastTime)
	polSpec.T = []trans{{6, wbftP{ProposerPolicy: u64(1)}}}
	type in struct {
		name, desc string
		reqs       []string
		x          fixture
		number     uint64
	}
	base := []string{"@r17", "WBFT-VAL-006", "@r18", "WBFT-VAL-011"}
	ins := []in{
		{"genesis", "height 0: anzeon.init validators and keys, base policy", []string{"WBFT-VAL-004", "WBFT-VAL-011"}, a, 0},
		{"block_1", "height 1: the genesis header's EpochInfo", append(base, "WBFT-HDR-121"), a, 1},
		{"block_4_epoch_block", "height 4 (an epoch block) is sealed by the set recorded at 0, not by the set it records", base, a, 4},
		{"block_5_new_epoch", "height 5: the set recorded at block 4, in the order of its EpochInfo.validators ([3, 1, 4, 0] of candidates keys 0..4)", base, a, 5},
		{"block_8_next_epoch_boundary", "height 8 (the next epoch block) is still sealed by the set recorded at 4; header 8 is not stored, parent_hash is block 7", base, a, 8},
		{"policy_transition_before", "transition {6, proposerPolicy 1}: height 5 keeps policy 0", base, a.withSpec(polSpec), 5},
		{"policy_transition_at", "transition {6, proposerPolicy 1}: height 6 has policy 1 (config_at(6))", base, a.withSpec(polSpec), 6},
		{"missing_epoch_header", "the stored headers are 5 and 6 only: the epoch header 4 is neither canonical nor reachable from the parent, so the lookup fails (unknown ancestor)", []string{"@r17", "WBFT-VAL-009"}, a.keep(5, 6), 7},
		{"epoch_header_without_epoch_info", "block 4 is an epoch block without EpochInfo: the lookup for height 5 fails (WBFT: epochInfo is nil)", []string{"WBFT-VAL-009"}, c, 5},
		{"fork_canonical_epoch_header_first", "canonical blocks 1..7 and a non-canonical branch 4', 5' whose epoch block 4' records the set of keys 0..3; height 6 with parent 5': the canonical epoch header 4 is used, not the one on the parent's branch (A-08 WBFT-HDR-071)", []string{"@r17", "WBFT-HDR-071"}, forkFixture(true), 6},
		{"fork_walk_back_to_branch_epoch_header", "canonical blocks 1..3 only and the non-canonical branch 4', 5': no canonical header at 4, so the lookup for height 6 walks back from parent 5' and uses the EpochInfo of 4' (A-04 §3.1)", []string{"@r17", "WBFT-HDR-071"}, forkFixture(false), 6},
	}
	var cs []Case
	for _, x := range ins {
		ch := x.x.reader()
		var parentHash common.Hash
		if len(x.x.nonCanonical) > 0 {
			// the query's parent is the last header of the non-canonical branch
			h := new(types.Header)
			must(rlp.DecodeBytes(x.x.nonCanonical[len(x.x.nonCanonical)-1], h))
			parentHash = h.Hash()
		} else if x.number > 0 {
			if p := ch.byNum[x.number-1]; p != nil {
				parentHash = p.Hash()
			} else {
				parentHash = ch.head.Hash()
			}
		}
		e := wbftengine.NewEngine(wbftConfig(ch.cc), common.Address{}, nil, nil)
		vs, err := e.GetValidators(ch, new(big.Int).SetUint64(x.number), parentHash, nil)
		cse := Case{Runner: "validators", Handler: "validators_at", Name: x.name, Kind: "chain", Desc: x.desc, Reqs: x.reqs,
			Input: M{{"chain", x.x.yaml()}, {"number", dec(x.number)}, {"parent_hash", hexs(parentHash.Bytes())}}}
		if err != nil {
			cse.Err = err.Error()
		} else {
			list := []any{}
			for _, v := range vs.List() {
				list = append(list, M{{"address", hexs(v.Address().Bytes())}, {"bls_public_key", hexs(v.BLSPublicKey())}})
			}
			cse.Expected = M{{"validators", list}, {"proposer_policy", dec(uint64(vs.Policy().Id))}}
		}
		cs = append(cs, cse)
	}
	return cs
}

// ------------------------------------------------------- verify_header

type hdrCase struct {
	name, desc string
	reqs       []string
	x          fixture
	h          *types.Header
}

func verdict(x fixture, h *types.Header) (string, string) {
	be, ch := verifier(x)
	// verify the header as decoded from its RLP
	hd := new(types.Header)
	must(rlp.DecodeBytes(enc(h), hd))
	err := be.VerifyHeader(ch, hd)
	switch {
	case err == nil:
		return "accept", ""
	case errors.Is(err, consensus.ErrFutureBlock):
		return "deferred", err.Error()
	default:
		return "reject", err.Error()
	}
}

func verifyHeaderCases() []Case {
	bA := chainA(pastTime, 6, true)
	a := bA.fixture()
	head := bA.head()
	next := func(pre, post func(h *types.Header)) *types.Header {
		return bA.variant(3, 0, []int{0, 1, 2}, pre, post)
	}
	stored := func(n int) *types.Header { return bA.headers[n-1] }

	shanghai := specA(pastTime)
	shanghai.Shanghai = u64(0)
	cancun := specA(pastTime)
	cancun.Cancun = u64(0)
	bp2 := specA(pastTime)
	bp2.T = []trans{{7, wbftP{BlockPeriodSeconds: 2}}}
	hdr := []string{"WBFT-HDR-080"}
	r := func(ids ...string) []string { return append(append([]string{}, hdr...), ids...) }

	var cs []hdrCase
	add := func(name, desc string, reqs []string, x fixture, h *types.Header) {
		cs = append(cs, hdrCase{name, desc, reqs, x, h})
	}

	// ---- accepted headers
	add("accept_next_block", "block 7 on the stored head 6, proposed by a validator of V(7) and sealed by 3 of 4 validators; H15b and H21 are skipped because the fixture has no state", r("WBFT-HDR-086", "WBFT-HDR-100", "WBFT-HDR-102", "WBFT-HDR-103", "@r12", "WBFT-HDR-111"), a, next(nil, nil))
	add("accept_block_1", "stored block 1 on the genesis header: V = genesis EpochInfo, previous seals not examined", r("WBFT-HDR-091", "WBFT-HDR-121"), a, stored(1))
	add("accept_block_2", "stored block 2: the first block that carries previous seals (block 1 was committed in round 0); block 2 itself was committed in round 1", r("WBFT-HDR-091", "WBFT-HDR-103"), a, stored(2))
	add("accept_epoch_block_4", "stored epoch block 4 with EpochInfo; header verification does not check the EpochInfo content", r("WBFT-HDR-131"), a, stored(4))
	add("accept_block_5_across_epoch", "stored block 5: V(5) comes from block 4, its previous seals (of block 4) are checked with Vp = V(4) from the genesis header, so V and Vp differ", r("WBFT-VAL-010", "WBFT-HDR-101", "WBFT-HDR-103"), a, stored(5))
	add("accept_genesis_header", "the genesis header: H1-H9 only, then H10 accepts it without examining extra, parent or signer", r("WBFT-HDR-083"), a, bA.genesis)
	add("accept_block_1_arbitrary_prev_fields", "block 1 with PrevRound 5 and previous seals copied from block 2: not examined for Number 1", r("WBFT-HDR-091"),
		a.keep(), func() *types.Header {
			g := newBuilder(specA(pastTime), pastTime, nil)
			x2, err := types.ExtractWBFTExtra(stored(2))
			must(err)
			return g.variant(0, 0, []int{0, 1, 2}, func(h *types.Header) {
				setExtra(h, func(x *types.WBFTExtra) {
					x.PrevRound = 5
					x.PrevPreparedSeal, x.PrevCommittedSeal = x2.PrevPreparedSeal, x2.PrevCommittedSeal
				})
			}, nil)
		}())
	add("accept_prev_seals_with_extra_seal", "block 7 whose previous seals carry all four sealers of block 6 (quorum plus one extra seal)", r("WBFT-HDR-100", "WBFT-HDR-103"), a,
		next(func(h *types.Header) {
			all := bA.sealsFor(head, 0, wbftcore.SealTypePrepare, []int{0, 1, 2, 3})
			allc := bA.sealsFor(head, 0, wbftcore.SealTypeCommit, []int{0, 1, 2, 3})
			setExtra(h, func(x *types.WBFTExtra) { x.PrevPreparedSeal, x.PrevCommittedSeal = aggSeal(all), aggSeal(allc) })
		}, nil))
	add("accept_block_period_transition", "transition {7, blockPeriodSeconds 2}; block 7 with Time = parent.Time + 2", r("WBFT-HDR-085"), a.withSpec(bp2),
		func() *types.Header {
			bb := chainA(pastTime, 6, true)
			bb.spec, bb.cc, bb.cfg = bp2, bp2.chainConfig(), wbftConfig(bp2.chainConfig())
			bb.ch.cc = bb.cc
			return bb.variant(3, 0, []int{0, 1, 2}, nil, nil)
		}())

	fb := forkBuilder()
	fork6 := fb.variant(1, 0, []int{0, 1, 2}, nil, nil) // block 6' on 5', sealed by keys 0, 1, 2 of the branch's set
	add("accept_fork_block_walk_back", "block 6' on the non-canonical parent 5'; no canonical header at the epoch block 4, so V(6') and the previous-seal set come from the branch's epoch block 4' (A-04 §3.1 walk back)", r("WBFT-HDR-070", "WBFT-HDR-071"), forkFixture(false), fork6)
	add("reject_fork_block_canonical_epoch_header", "the same block 6' when the canonical epoch header 4 is stored: the canonical EpochInfo (validators keys 3, 1, 4, 0) is used, so the seals made by the branch's set do not verify (A-08 WBFT-HDR-071)", r("WBFT-HDR-071"), forkFixture(true), fork6)

	// ---- deferred
	add("deferred_future_time", "block 7 with Time 3000000000 > now + allowedFutureBlockTime", r("WBFT-HDR-081", "WBFT-VEC-031"), a, next(func(h *types.Header) { h.Time = futureStamp }, nil))
	add("deferred_future_time_and_bad_difficulty", "block 7 with Time 3000000000 and Difficulty 2: H2 precedes H4, so the verdict is deferred (A-08 §10.6)", r("WBFT-HDR-081", "WBFT-VEC-031"), a,
		next(func(h *types.Header) { h.Time = futureStamp; h.Difficulty = big.NewInt(2) }, nil))
	add("reject_future_time_unknown_parent", "block 7 with Time 3000000000 and an unknown parent hash: the parent lookup V0b runs before H2, so the header is rejected, not deferred", r("WBFT-HDR-070", "WBFT-HDR-081"), a,
		next(func(h *types.Header) {
			h.Time = futureStamp
			h.ParentHash = crypto.Keccak256Hash([]byte("unknown parent"))
		}, nil))

	// ---- rejected, in the order of H1-H21
	add("reject_uncle_hash", "H3: UncleHash is not keccak256(rlp([]))", r(), a, next(func(h *types.Header) { h.UncleHash = crypto.Keccak256Hash([]byte("uncles")) }, nil))
	add("reject_difficulty_2", "H4: Difficulty 2", r("WBFT-HDR-082"), a, next(func(h *types.Header) { h.Difficulty = big.NewInt(2) }, nil))
	add("reject_difficulty_0", "H4: Difficulty 0", r("WBFT-HDR-082"), a, next(func(h *types.Header) { h.Difficulty = big.NewInt(0) }, nil))
	add("reject_gas_limit_above_max", "H5: GasLimit 2^63", r(), a, next(func(h *types.Header) { h.GasLimit = 1 << 63 }, nil))
	add("reject_shanghai_active", "H6: configuration with shanghaiTime 0 (B-03 SNET-BHDR-003)", r(), a.withSpec(shanghai), next(nil, nil))
	add("reject_withdrawals_hash", "H7: WithdrawalsHash present", r(), a, next(func(h *types.Header) { w := types.EmptyWithdrawalsHash; h.WithdrawalsHash = &w }, nil))
	add("reject_cancun_active", "H8: configuration with cancunTime 0 (B-03 SNET-BHDR-005)", r(), a.withSpec(cancun), next(nil, nil))
	add("reject_unknown_parent", "H11 (reported by the parent lookup V0b): ParentHash of no stored header", r("WBFT-HDR-070", "WBFT-HDR-084"), a,
		next(nil, func(h *types.Header) { h.ParentHash = crypto.Keccak256Hash([]byte("unknown parent")) }))
	add("reject_parent_number_mismatch", "H11: block 7 whose ParentHash is the hash of block 5", r("WBFT-HDR-084"), a,
		next(nil, func(h *types.Header) { h.ParentHash = stored(5).Hash() }))
	add("reject_timestamp_equal_parent", "H12: Time = parent.Time with block period 1", r("WBFT-HDR-085"), a, next(func(h *types.Header) { h.Time = head.Time }, nil))
	add("reject_block_period_transition", "H12: transition {7, blockPeriodSeconds 2}; block 7 with Time = parent.Time + 1 (the period of the child's height applies)", r("WBFT-HDR-085"), a.withSpec(bp2), next(nil, nil))
	add("reject_gas_used_above_limit", "H13: GasUsed > GasLimit", r(), a, next(func(h *types.Header) { h.GasUsed = h.GasLimit + 1 }, nil))
	add("reject_base_fee", "H14: BaseFee one above the value computed from the parent", r(), a, next(func(h *types.Header) { h.BaseFee = new(big.Int).Add(h.BaseFee, big.NewInt(1)) }, nil))
	add("reject_gas_limit_jump", "H14: GasLimit doubled against the parent", r(), a, next(func(h *types.Header) { h.GasLimit = 2 * h.GasLimit }, nil))
	add("reject_coinbase_not_validator", "H15a: block 7 proposed by key 5, not a member of V(7), with a correct reveal for key 5", r("WBFT-HDR-086", "@r14"), a, bA.variant(5, 0, []int{0, 1, 2}, nil, nil))
	add("reject_extra_undecodable", "H16: a zero byte appended to Extra", r("WBFT-HDR-088"), a, next(nil, func(h *types.Header) { h.Extra = append(h.Extra, 0x00) }))
	add("reject_prepared_seal_absent", "H17: PreparedSeal absent", r("WBFT-HDR-102"), a, next(nil, func(h *types.Header) { setExtra(h, func(x *types.WBFTExtra) { x.PreparedSeal = nil }) }))
	add("reject_prepared_seal_below_quorum", "H17: PreparedSeal of 2 sealers, quorum of 4 is 3", r("WBFT-HDR-100", "WBFT-HDR-102"), a,
		next(nil, func(h *types.Header) {
			setExtra(h, func(x *types.WBFTExtra) {
				x.PreparedSeal = aggSeal(bA.sealsFor(h, 0, wbftcore.SealTypePrepare, []int{0, 1}))
			})
		}))
	add("reject_prepared_sealer_out_of_range", "H17: PreparedSeal with sealer index 4 in a set of 4 (signature of indices 0, 1, 2)", r("WBFT-HDR-100", "WBFT-HDR-101"), a,
		next(nil, func(h *types.Header) {
			setExtra(h, func(x *types.WBFTExtra) { x.PreparedSeal.Sealers.SetSealer(4) })
		}))
	add("reject_prepared_seal_wrong_round", "H17: PreparedSeal signed for round 1 while Extra.Round is 0", r("WBFT-HDR-100", "WBFT-HDR-102"), a,
		next(nil, func(h *types.Header) {
			setExtra(h, func(x *types.WBFTExtra) {
				x.PreparedSeal = aggSeal(bA.sealsFor(h, 1, wbftcore.SealTypePrepare, []int{0, 1, 2}))
			})
		}))
	add("reject_committed_seal_wrong_type", "H17: CommittedSeal signed with the PREPARE seal type", r("WBFT-HDR-100", "WBFT-HDR-102"), a,
		next(nil, func(h *types.Header) {
			setExtra(h, func(x *types.WBFTExtra) {
				x.CommittedSeal = aggSeal(bA.sealsFor(h, 0, wbftcore.SealTypePrepare, []int{0, 1, 2}))
			})
		}))
	add("reject_committed_seal_empty_signature", "H17: CommittedSeal present with an empty signature", r("WBFT-HDR-102"), a,
		next(nil, func(h *types.Header) { setExtra(h, func(x *types.WBFTExtra) { x.CommittedSeal.Signature = nil }) }))
	add("reject_randao_wrong_signer", "H18: RandaoReveal signed by key 1 (a validator, not the Coinbase), mix digest recomputed from it", r("WBFT-HDR-089", "@r11"), a,
		next(func(h *types.Header) { resignRandao(bA, h, 1, h.Number) }, nil))
	add("reject_randao_wrong_number", "H18: RandaoReveal of the Coinbase over the randao data of block 6, mix digest recomputed from it", r("WBFT-HDR-089"), a,
		next(func(h *types.Header) { resignRandao(bA, h, 3, big.NewInt(6)) }, nil))
	add("reject_mix_digest", "H19: MixDigest with the last byte flipped", r("WBFT-HDR-090"), a, next(func(h *types.Header) { h.MixDigest[31] ^= 1 }, nil))
	add("reject_prev_prepared_absent", "H20: PrevPreparedSeal absent", r("WBFT-HDR-091", "WBFT-HDR-103"), a,
		next(func(h *types.Header) { setExtra(h, func(x *types.WBFTExtra) { x.PrevPreparedSeal = nil }) }, nil))
	add("reject_prev_committed_below_quorum", "H20: PrevCommittedSeal of 2 sealers of block 6", r("WBFT-HDR-103"), a,
		next(func(h *types.Header) {
			setExtra(h, func(x *types.WBFTExtra) {
				x.PrevCommittedSeal = aggSeal(bA.sealsFor(head, 0, wbftcore.SealTypeCommit, []int{0, 1}))
			})
		}, nil))
	add("reject_prev_round_wrong", "H20: PrevRound 1 while the previous seals are over round 0 of block 6", r("WBFT-HDR-103"), a,
		next(func(h *types.Header) { setExtra(h, func(x *types.WBFTExtra) { x.PrevRound = 1 }) }, nil))

	// ---- validator-set lookup
	c := chainA(pastTime, 4, false)
	add("reject_epoch_header_without_epoch_info", "block 5 on a chain whose epoch block 4 has no EpochInfo: V(5) cannot be determined (unknown ancestor)", r("WBFT-HDR-070", "WBFT-VAL-009", "WBFT-HDR-131"), c.fixture(),
		c.variant(0, 0, nil, nil, nil))

	// ---- aggregated seal whose keys sum to infinity (WBFT-HDR-100, WBFT-CRYPTO-055)
	inf := chainInf()
	add("reject_seal_keys_sum_to_infinity", "block 1 sealed by validators 0, 1, 2 whose BLS keys sum to the point at infinity: the aggregate signature is c0 || 95 x 00 and the pairing holds trivially, but the seal MUST be rejected", r("WBFT-HDR-100", "WBFT-CRYPTO-055"), inf.fixture(), inf.variant(0, 0, []int{0, 1, 2}, nil, nil))
	add("accept_seal_with_cancel_key_not_summing_to_infinity", "same chain, block 1 sealed by validators 0, 1, 3", r("WBFT-HDR-100"), inf.fixture(), inf.variant(0, 0, []int{0, 1, 3}, nil, nil))
	add("accept_seal_all_four_with_cancel_key", "same chain, block 1 sealed by all four validators: the keys sum to the key of validator 3 and the aggregate is its signature", r("WBFT-HDR-100"), inf.fixture(), inf.variant(0, 0, []int{0, 1, 2, 3}, nil, nil))

	if time.Now().Unix() >= vectorNow {
		panic("the wall clock is past the `now` input of the verify_header vectors")
	}
	var out []Case
	for _, x := range cs {
		v, e := verdict(x.x, x.h)
		desc := x.desc
		if e != "" {
			desc += "; reference error (informative): " + e
		}
		out = append(out, Case{Runner: "header", Handler: "verify_header", Name: x.name, Kind: "chain", Desc: desc, Reqs: x.reqs,
			Input:    M{{"chain", x.x.yaml()}, {"header", hexs(enc(x.h))}, {"now", dec(vectorNow)}},
			Expected: M{{"verdict", v}}})
	}
	return out
}

// ------------------------------------------------------- verify_light

// lightVerdict composes verify_light (A-08 §8) from reference functions:
// the validator sets come from Engine.GetValidators over the fixture, the
// header check is Engine.VerifyHeader with the parent resolved by hash and
// check_seals, and the EpochInfo presence rule uses IsEpochBlockNumber. The
// CANNOT_DECIDE outcomes follow the definition of verify_light.
func lightVerdict(x fixture, h *types.Header) (string, string) {
	be, ch := verifier(x)
	e := be.Engine()
	if h.Number.Sign() == 0 {
		return "CANNOT_DECIDE", "genesis"
	}
	parent := ch.byHash[h.ParentHash]
	if parent == nil {
		return "CANNOT_DECIDE", "missing parent"
	}
	V, err := e.GetValidators(ch, h.Number, h.ParentHash, nil)
	if err != nil {
		return "CANNOT_DECIDE", "validator set of the header: " + err.Error()
	}
	Vp := V
	if h.Number.Uint64() >= 2 {
		if Vp, err = e.GetValidators(ch, parent.Number, parent.ParentHash, nil); err != nil {
			return "CANNOT_DECIDE", "validator set of the parent: " + err.Error()
		}
	}
	if err := e.VerifyHeader(ch, h, []*types.Header{parent}, V, Vp, true); err != nil {
		if errors.Is(err, consensus.ErrFutureBlock) {
			panic("verify_light vector with a future header")
		}
		return "INVALID", err.Error()
	}
	x2, err := types.ExtractWBFTExtra(h)
	must(err)
	isE, _, err := e.IsEpochBlockNumber(ch.cc, h.Number)
	must(err)
	if (x2.EpochInfo != nil) != isE {
		return "INVALID", "WBFT-HDR-130: EpochInfo presence does not match is_epoch_block"
	}
	return "VALID", ""
}

func verifyLightCases() []Case {
	bA := chainA(pastTime, 6, true)
	a := bA.fixture()
	head := bA.head()
	next := func(pre, post func(h *types.Header)) *types.Header {
		return bA.variant(3, 0, []int{0, 1, 2}, pre, post)
	}
	stored := func(n int) *types.Header { return bA.headers[n-1] }
	b7 := chainA(pastTime, 7, true)
	type in struct {
		name, desc string
		reqs       []string
		x          fixture
		h          *types.Header
	}
	lt := []string{"WBFT-HDR-140", "WBFT-HDR-141"}
	r := func(ids ...string) []string { return append(append([]string{}, lt...), ids...) }
	ins := []in{
		{"valid_next_block", "block 7 on block 6", r("WBFT-HDR-142"), a, next(nil, nil)},
		{"valid_block_1", "block 1 on the genesis header", r(), a, stored(1)},
		{"valid_epoch_block_4", "epoch block 4 with EpochInfo", r("WBFT-HDR-130"), a, stored(4)},
		{"valid_block_5_across_epoch", "block 5: V from block 4, Vp from the genesis header", r(), a, stored(5)},
		{"cannot_decide_genesis", "the genesis header (B-02 decides it)", r(), a, bA.genesis},
		{"cannot_decide_missing_parent", "block 7 whose parent is not in the fixture", r(), a.keep(1, 2, 3, 4, 5), next(nil, nil)},
		{"cannot_decide_missing_epoch_header", "block 7 with parent 6 stored but the epoch header 4 missing", r(), a.keep(5, 6), next(nil, nil)},
		{"invalid_difficulty", "H4: Difficulty 2", r(), a, next(func(h *types.Header) { h.Difficulty = big.NewInt(2) }, nil)},
		{"invalid_parent_number", "H11: block 7 whose ParentHash names block 5, which is in the fixture", r(), a, next(nil, func(h *types.Header) { h.ParentHash = stored(5).Hash() })},
		{"invalid_timestamp", "H12: Time = parent.Time", r(), a, next(func(h *types.Header) { h.Time = head.Time }, nil)},
		{"invalid_coinbase", "H15a: Coinbase key 5 is not in V(7)", r(), a, bA.variant(5, 0, []int{0, 1, 2}, nil, nil)},
		{"invalid_extra", "H16: Extra with a trailing byte", r(), a, next(nil, func(h *types.Header) { h.Extra = append(h.Extra, 0x00) })},
		{"invalid_seal_below_quorum", "H17: CommittedSeal of 2 sealers", r(), a, next(nil, func(h *types.Header) {
			setExtra(h, func(x *types.WBFTExtra) {
				x.CommittedSeal = aggSeal(bA.sealsFor(h, 0, wbftcore.SealTypeCommit, []int{0, 1}))
			})
		})},
		{"invalid_randao", "H18: reveal of another validator", r(), a, next(func(h *types.Header) { resignRandao(bA, h, 1, h.Number) }, nil)},
		{"invalid_mix", "H19: MixDigest altered", r(), a, next(func(h *types.Header) { h.MixDigest[0] ^= 1 }, nil)},
		{"invalid_prev_seals", "H20: PrevRound 2 against seals over round 0", r(), a, next(func(h *types.Header) { setExtra(h, func(x *types.WBFTExtra) { x.PrevRound = 2 }) }, nil)},
		{"invalid_epoch_info_on_non_epoch_block", "WBFT-HDR-130: block 7 (not an epoch block) carries an EpochInfo; the reference header verification accepts it (WBFT-HDR-131)", r("WBFT-HDR-130", "WBFT-HDR-131"), a,
			next(func(h *types.Header) {
				_, err := wbftengine.ApplyHeaderWBFTExtra(h, wbftengine.WriteEpochInfo(epochInfo([]int{0, 1, 2, 3}, []int{0, 1, 2, 3})))
				must(err)
			}, nil)},
		{"invalid_epoch_block_without_epoch_info", "WBFT-HDR-130: epoch block 8 without EpochInfo", r("WBFT-HDR-130"), b7.fixture(), b7.variant(1, 0, []int{0, 1, 2}, nil, nil)},
	}
	var out []Case
	for _, x := range ins {
		v, why := lightVerdict(x.x, x.h)
		desc := x.desc + ". verify_light is not a reference function; the result is composed from reference calls (A-11 WBFT-VEC-020 exception, see the generator README)"
		if why != "" {
			desc += "; reason (informative): " + why
		}
		out = append(out, Case{Runner: "header", Handler: "verify_light", Name: x.name, Kind: "chain", Desc: desc, Reqs: x.reqs,
			Input: M{{"chain", x.x.yaml()}, {"header", hexs(enc(x.h))}}, Expected: M{{"result", v}}})
	}
	return out
}

// -------------------------------------------------- build_proposal_header

func buildProposalCases() []Case {
	bD := chainA(futureTime, 3, true)
	d := bD.fixture()
	h3 := bD.headers[2]
	type seal struct {
		idx int
		sig []byte
	}
	type in struct {
		name, desc string
		reqs       []string
		x          fixture
		parent     *types.Header
		vanity     []byte
		key        int
		gasTip     *big.Int
		xp, xc     []seal
		skeleton   func(h *types.Header)
	}
	tip := new(big.Int).SetUint64(defaultTip)
	sig := func(h *types.Header, key int, round uint32, st wbftcore.SealType) []byte {
		return accts[key].bls.Sign(wbftcore.PrepareSeal(h, round, st)).Marshal()
	}
	// a 32-byte vanity that decodes as a WBFTExtra: VanityData of 21 bytes, Round 7
	decodable := enc(&types.WBFTExtra{VanityData: []byte("wbft-vector-vanity-32"), RandaoReveal: []byte{}, Round: 7})
	if len(decodable) != 32 {
		panic(fmt.Sprintf("decodable vanity has %d bytes", len(decodable)))
	}
	bp3 := specA(futureTime)
	bp3.T = []trans{{4, wbftP{BlockPeriodSeconds: 3}}}
	unsealed := chainA(futureTime, 2, true)
	u3 := unsealed.propose(unsealed.head(), 2)
	unsealed.store(u3) // block 3 stored without seals
	base := []string{"WBFT-HDR-010", "WBFT-HDR-011", "WBFT-HDR-012", "WBFT-HDR-013", "WBFT-HDR-014", "WBFT-HDR-020", "WBFT-HDR-040", "WBFT-HDR-042"}
	r := func(ids ...string) []string { return append(append([]string{}, base...), ids...) }
	ins := []in{
		{"block_1_on_genesis", "block 1: no previous seals, vanity empty", r("WBFT-HDR-016", "WBFT-HDR-033"), d.keep(), bD.genesis, nil, 0, tip, nil, nil, nil},
		{"block_3_prev_round_1", "block 3: previous seals and PrevRound 1 of block 2 (committed in round 1)", r("WBFT-HDR-030", "WBFT-HDR-032"), d.keep(1, 2), bD.headers[1], nil, 2, tip, nil, nil, nil},
		{"block_4", "block 4 on block 3 (sealed by validators 1, 2, 3 in round 0) by validator key 3", r("WBFT-HDR-032"), d, h3, nil, 3, tip, nil, nil, nil},
		{"vanity_short", "builder vanity \"wbft\" (4 bytes): right-padded with zeros to 32 bytes", r("WBFT-HDR-016"), d, h3, []byte("wbft"), 3, tip, nil, nil, nil},
		{"vanity_32_bytes_decodable", "builder vanity of exactly 32 bytes that decodes as a WBFTExtra (VanityData of 21 bytes, Round 7): the decoded fields not overwritten are kept (A-08 §3.6 edge case)", r("WBFT-HDR-016"), d, h3, decodable, 3, tip, nil, nil, nil},
		{"gas_tip_zero", "gas-tip query returns 0: GasTip is written as 0 (non-nil)", r(), d, h3, nil, 3, big.NewInt(0), nil, nil, nil},
		{"proposer_not_validator", "account key 6, not a validator, builds block 4: construction does not check membership", r(), d, h3, nil, 6, tip, nil, nil, nil},
		{"extra_seal_new_index", "extra PREPARE and COMMIT seals of validator 0 for block 3, round 0: merged into the previous seals (bitmap 0x0f)", r("WBFT-HDR-034"), d, h3, nil, 3, tip,
			[]seal{{0, sig(h3, 0, 0, wbftcore.SealTypePrepare)}}, []seal{{0, sig(h3, 0, 0, wbftcore.SealTypeCommit)}}, nil},
		{"extra_seal_duplicate_index", "extra PREPARE seal of validator 1, already in the bitmap: skipped, the seal is unchanged", r("WBFT-HDR-034"), d, h3, nil, 3, tip,
			[]seal{{1, sig(h3, 1, 0, wbftcore.SealTypePrepare)}}, nil, nil},
		{"extra_seal_undecodable", "extra PREPARE seal of 96 zero bytes (not a valid compressed point): aggregation fails and the previous prepared seal is left unmodified; the valid extra COMMIT seal of validator 0 is merged", r("WBFT-HDR-034"), d, h3, nil, 3, tip,
			[]seal{{0, make([]byte, 96)}}, []seal{{0, sig(h3, 0, 0, wbftcore.SealTypeCommit)}}, nil},
		{"extra_seal_wrong_round", "extra PREPARE seal of validator 0 over round 1 of block 3: merged without verification (the proposal would fail H20, WBFT-HDR-036)", r("WBFT-HDR-034", "WBFT-HDR-036"), d, h3, nil, 3, tip,
			[]seal{{0, sig(h3, 0, 1, wbftcore.SealTypePrepare)}}, nil, nil},
		{"block_period_transition", "transition {4, blockPeriodSeconds 3}: Time = parent.Time + 3 (period of the new block's height)", r("WBFT-HDR-013"), d.withSpec(bp3), h3, nil, 3, tip, nil, nil, nil},
		// fail cases
		{"fail_vanity_32_bytes_undecodable", "builder vanity of exactly 32 bytes that does not decode as WBFTExtra: no proposal", r("WBFT-HDR-018"), d, h3, []byte("wbft-vector-vanity-of-32-bytes!!"), 3, tip, nil, nil, nil},
		{"fail_gas_tip_unavailable", "the gas-tip query fails (no parent state): no proposal", r("WBFT-HDR-021"), d, h3, nil, 3, nil, nil, nil, nil},
		{"fail_unknown_parent", "ParentHash of no stored header", r(), d, h3, nil, 3, tip, nil, nil, func(h *types.Header) { h.ParentHash = crypto.Keccak256Hash([]byte("unknown parent")) }},
		{"fail_last_block_without_prepared_seal", "the stored canonical block 3 has no PreparedSeal (not committed): ErrEmptyPreparedSeals", r("WBFT-HDR-031"), unsealed.fixture(), u3, nil, 3, tip, nil, nil, nil},
	}
	var out []Case
	for _, x := range ins {
		ch := x.x.reader()
		ch.gasTip = x.gasTip
		cfg := wbftConfig(ch.cc)
		parent := ch.byHash[x.parent.Hash()]
		sk := (&builder{cc: ch.cc}).skeletonOf(parent, x.vanity)
		if x.skeleton != nil {
			x.skeleton(sk)
		}
		skRLP := enc(sk)
		var xp, xc []wbft.SealData
		lp, lc := []any{}, []any{}
		for _, s := range x.xp {
			xp = append(xp, wbft.SealData{Sealer: uint32(s.idx), Seal: s.sig})
			lp = append(lp, M{{"sealer", dec(uint64(s.idx))}, {"seal", hexs(s.sig)}})
		}
		for _, s := range x.xc {
			xc = append(xc, wbft.SealData{Sealer: uint32(s.idx), Seal: s.sig})
			lc = append(lc, M{{"sealer", dec(uint64(s.idx))}, {"seal", hexs(s.sig)}})
		}
		var gt any
		if x.gasTip != nil {
			gt = decBig(x.gasTip)
		}
		h := new(types.Header)
		must(rlp.DecodeBytes(skRLP, h))
		be := backend.New(cfg, accts[x.key].key, rawdb.NewMemoryDatabase())
		err := be.Engine().Prepare(ch, h, xp, xc)
		c := Case{Runner: "header", Handler: "build_proposal_header", Name: x.name, Kind: "chain", Desc: x.desc, Reqs: x.reqs,
			Input: M{
				{"chain", x.x.yaml()},
				{"header", hexs(skRLP)},
				{"node_key", hexs(crypto.FromECDSA(accts[x.key].key))},
				{"gas_tip", gt},
				{"extra_prepared", lp},
				{"extra_committed", lc},
				{"now", dec(vectorNow)},
			}}
		if err != nil {
			c.Err = err.Error()
		} else {
			if h.Time < vectorNow {
				panic("build_proposal_header: Time depends on the clock")
			}
			c.Expected = M{{"header", hexs(enc(h))}}
		}
		out = append(out, c)
	}
	return out
}

// skeletonOf is the builder skeleton for an arbitrary parent (A-08 §3.1).
func (b *builder) skeletonOf(parent *types.Header, vanity []byte) *types.Header {
	return b.skeleton(parent, vanity)
}

// ------------------------------------------------------- verify_headers

// verdicts runs the reference batch verifier Backend.VerifyHeaders (A-08 §6.7)
// over headers decoded from their RLP and maps each result as verdict does.
func verdicts(x fixture, hs []*types.Header) ([]any, []string) {
	be, ch := verifier(x)
	var in []*types.Header
	for _, h := range hs {
		hd := new(types.Header)
		must(rlp.DecodeBytes(enc(h), hd))
		in = append(in, hd)
	}
	abort, results := be.VerifyHeaders(ch, in)
	defer close(abort)
	out := []any{}
	var errs []string
	for range in {
		err := <-results
		switch {
		case err == nil:
			out = append(out, "accept")
		case errors.Is(err, consensus.ErrFutureBlock):
			out = append(out, "deferred")
			errs = append(errs, err.Error())
		default:
			out = append(out, "reject")
			errs = append(errs, err.Error())
		}
	}
	return out, errs
}

func verifyHeadersCases() []Case {
	bA := chainA(pastTime, 7, true)
	a := bA.fixture()
	hdr := func(n int) *types.Header { return bA.headers[n-1] }

	// block 6 with Difficulty 2 (sealed as it is) and a valid block 7 on it
	bBad := chainA(pastTime, 5, true)
	bad6 := bBad.variant(0, 0, []int{0, 1, 2, 3}, func(h *types.Header) { h.Difficulty = big.NewInt(2) }, nil)
	bBad.store(bad6)
	bad7 := bBad.block(3, 0, []int{0, 1, 2}, nil)
	if v, _ := verdict(bBad.fixture(), bad7); v != "accept" {
		panic("verify_headers: block 7 on the bad block 6 must be valid on its own")
	}

	// blocks 6 and 7 with times in the future
	bFut := chainA(pastTime, 5, true)
	fut6 := bFut.variant(0, 0, []int{0, 1, 2, 3}, func(h *types.Header) { h.Time = futureStamp }, nil)
	bFut.store(fut6)
	fut7 := bFut.block(3, 0, []int{0, 1, 2}, nil)
	if v, _ := verdict(bFut.fixture(), fut7); v != "deferred" {
		panic("verify_headers: block 7 after the future block 6 must be deferred on its own")
	}

	fb := forkBuilder()
	fork6 := fb.variant(1, 0, []int{0, 1, 2}, nil, nil)
	branch := append(append([]*types.Header{}, fb.headers[3:]...), fork6) // 4', 5', 6'

	type in struct {
		name, desc string
		reqs       []string
		x          fixture
		hs         []*types.Header
	}
	base := []string{"WBFT-HDR-120"}
	r := func(ids ...string) []string { return append(append([]string{}, base...), ids...) }
	ins := []in{
		{"valid_on_stored_parent", "stored headers 1..4; the batch 5, 6, 7 is verified in order, each header with the preceding headers of the batch as parents", r(), a.keep(1, 2, 3, 4), []*types.Header{hdr(5), hdr(6), hdr(7)}},
		{"whole_chain_from_genesis", "only the genesis header is stored; the batch 1..7: block 1 uses the genesis EpochInfo, blocks 5..7 find the epoch block 4 among the preceding headers of the batch (no canonical header at 4)", r("WBFT-HDR-071", "WBFT-HDR-121"), a.keep(), []*types.Header{hdr(1), hdr(2), hdr(3), hdr(4), hdr(5), hdr(6), hdr(7)}},
		{"failure_stops_verification", "stored headers 1..4; the batch 5, 6', 7' where 6' has Difficulty 2 (H4) and 7' is a valid block on 6' (accepted when verified on its own on a chain that stores 6'): after the first failure the remaining header is reported as ErrUnknownAncestor without being verified", r(), a.keep(1, 2, 3, 4), []*types.Header{hdr(5), bad6, bad7}},
		{"deferred_stops_verification", "stored headers 1..4; the batch 5, 6'', 7'' where 6'' has Time 3000000000 (deferred by H2) and 7'' follows it (deferred when verified on its own): a deferred header is a failure of the batch, so 7'' is rejected with ErrUnknownAncestor instead of being deferred", r("WBFT-HDR-081", "WBFT-VEC-031"), a.keep(1, 2, 3, 4), []*types.Header{hdr(5), fut6, fut7}},
		{"first_header_fails", "stored headers 1..4; the batch 6, 7 without block 5: the parent of 6 is neither in the batch nor stored (V0b), so 6 is rejected and 7 is reported as ErrUnknownAncestor", r("WBFT-HDR-070"), a.keep(1, 2, 3, 4), []*types.Header{hdr(6), hdr(7)}},
		{"gap_in_batch", "stored headers 1..4; the batch 5, 7: the preceding header of the batch is used as the parent of 7, whose number does not match (H11)", r("WBFT-HDR-084"), a.keep(1, 2, 3, 4), []*types.Header{hdr(5), hdr(7)}},
		{"empty_batch", "an empty batch has no results", r(), a, []*types.Header{}},
		{"fork_branch_walk_back", "stored canonical headers 1..3 only; the batch 4', 5', 6' of a branch whose epoch block 4' records the set of keys 0..3: no canonical header at 4, so 5' and 6' find 4' among the preceding headers of the batch", r("WBFT-HDR-071"), a.keep(1, 2, 3), branch},
		{"fork_branch_canonical_epoch_header", "stored canonical headers 1..7; the same batch 4', 5', 6': the canonical epoch header 4 (validators keys 3, 1, 4, 0) is used for 5', whose seals made by the branch's set do not verify; 6' is then reported as ErrUnknownAncestor", r("WBFT-HDR-071"), a, branch},
	}
	if time.Now().Unix() >= vectorNow {
		panic("the wall clock is past the `now` input of the verify_headers vectors")
	}
	var out []Case
	for _, x := range ins {
		vs, errs := verdicts(x.x, x.hs)
		desc := x.desc
		if len(errs) > 0 {
			desc += "; reference errors in order (informative): " + strings.Join(errs, "; ")
		}
		hl := []any{}
		for _, h := range x.hs {
			hl = append(hl, hexs(enc(h)))
		}
		out = append(out, Case{Runner: "header", Handler: "verify_headers", Name: x.name, Kind: "chain", Desc: desc, Reqs: x.reqs,
			Input:    M{{"chain", x.x.yaml()}, {"headers", hl}, {"now", dec(vectorNow)}},
			Expected: M{{"verdicts", vs}}})
	}
	return out
}

