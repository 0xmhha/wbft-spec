// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// The scenarios of B-05 §13 "Scenario test vectors" (V-01 ... V-26) that
// this generator writes. Members A, B, C, D are accounts 8, 9, 10, 11 and
// their validators vA ... vD accounts 0 ... 3 (BLS keys derived from the same
// accounts); E = 12, A2 = 13, B2 = 14, X = 15, Y = 7; the replacement
// validators vX = 5 and vY = 6, the replacement key of C is the key of 4.

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

const (
	mA, mB, mC, mD = 8, 9, 10, 11
	mE, mA2, mB2   = 12, 13, 14
	aX, aY         = 15, 7
	vX, vY         = 5, 6
	week           = 604_800
)

var (
	govV = params.DefaultGovValidatorAddress
	govC = params.DefaultGovCouncilAddress
	t0   = gwei(27_600)
	t1   = gwei(30_000)
)

func stdGenesis() govGenesis {
	return govGenesis{
		validator: govParams{members: []int{mA, mB, mC, mD}, quorum: 3, expiry: week, maxProps: 3, validators: []int{0, 1, 2, 3}, gasTip: t0},
		council:   govParams{members: []int{mA, mB, mC, mD}, quorum: 3, expiry: week, maxProps: 3},
		funded:    []int{mA, mB, mC, mD, mE, mA2, mB2},
	}
}

var stdNamed = []int{0, 1, 2, 3, vX, vY, mA, mB, mC, mD, mE, mA2, mB2, aX, aY}

func st(label string, sender int, to common.Address, data []byte) govStep {
	return govStep{label: label, sender: sender, to: to, data: data, dt: 1}
}

func stAt(label string, sender int, to common.Address, data []byte, dt uint64) govStep {
	return govStep{label: label, sender: sender, to: to, data: data, dt: dt}
}

func approve(pid uint64) []byte    { return call("approveProposal(uint256)", u256b(pid)) }
func disapprove(pid uint64) []byte { return call("disapproveProposal(uint256)", u256b(pid)) }

func configure(val, key, popSigner int) []byte {
	pk, pop := popOf(key, popSigner)
	return call("configureValidator(address,bytes,bytes)", accts[val].addr, pk, pop)
}

func addr(i int) common.Address { return accts[i].addr }

func governanceCases() []Case {
	base := []string{"SNET-GOV-001", "SNET-GOV-160", "SNET-GOV-161", "SNET-GOV-162", "@r48", "@r49", "@r50"}
	r := func(ids ...string) []string { return append(append([]string{}, base...), ids...) }
	proposal := []string{"SNET-GOV-004", "SNET-GOV-005", "SNET-GOV-030", "SNET-GOV-031", "SNET-GOV-040", "SNET-GOV-042", "SNET-GOV-054", "SNET-GOV-062"}
	rp := func(ids ...string) []string { return r(append(append([]string{}, proposal...), ids...)...) }
	g := stdGenesis()
	one := govGenesis{
		validator: govParams{members: []int{mA}, quorum: 1, expiry: week, maxProps: 3, validators: []int{0}, gasTip: t0},
		council:   govParams{members: []int{mA}, quorum: 1, expiry: week, maxProps: 3},
		funded:    []int{mA},
	}
	cs := []govCase{
		{"v01_gas_tip_proposal", "B-05 V-01: A proposes the gas tip T1 = 30 000 gwei, B and C approve; the third approval reaches the quorum of 3, executes the proposal (GasTipUpdated(T0, T1, C)) and changes the gas tip of the projection",
			rp("SNET-GOV-112", "@r40"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1)", mA, govV, call("proposeGasTip(uint256)", t1)),
				st("B.approveProposal(1)", mB, govV, approve(1)),
				st("C.approveProposal(1): quorum, executed", mC, govV, approve(1)),
			}},
		{"v02_same_gas_tip", "B-05 V-02: proposing the current gas tip reverts with SameGasTip", r("SNET-GOV-030", "@r40"), g, stdNamed, []govStep{
			st("A.proposeGasTip(T0)", mA, govV, call("proposeGasTip(uint256)", t0)),
		}},
		{"v03_two_gas_tip_proposals", "B-05 V-03: A and B both propose T1 and both proposals are approved: both are executed, the second GasTipUpdated has old = new = T1",
			rp("SNET-GOV-112"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1)", mA, govV, call("proposeGasTip(uint256)", t1)),
				st("B.proposeGasTip(T1)", mB, govV, call("proposeGasTip(uint256)", t1)),
				st("C.approveProposal(1)", mC, govV, approve(1)),
				st("D.approveProposal(1): executed", mD, govV, approve(1)),
				st("C.approveProposal(2)", mC, govV, approve(2)),
				st("D.approveProposal(2): executed, T1 to T1", mD, govV, approve(2)),
			}},
		{"v05_replace_validator", "B-05 V-05: B replaces its validator vB by vX with the same key: vB is removed by swap-and-pop (vD moves into its place) and vX is appended",
			r("SNET-GOV-100", "SNET-GOV-101", "SNET-GOV-104", "@r45"), g, stdNamed, []govStep{
				st("B.configureValidator(vX, kB, popB)", mB, govV, configure(vX, 1, 1)),
			}},
		{"v10_change_member", "B-05 V-10: B changes its address to B2 (no proposal); the validator vB now belongs to operator B2, whose configureValidator of vB with a new key is a key change",
			r("SNET-GOV-012", "SNET-GOV-103", "SNET-GOV-109", "@r42"), g, stdNamed, []govStep{
				st("B.changeMember(B2)", mB, govV, call("changeMember(address)", addr(mB2))),
				st("B2.configureValidator(vB, kY, popY)", mB2, govV, configure(1, vY, vY)),
			}},
		{"v13_vote_at_expiry", "B-05 V-13: a vote exactly expiry seconds after the proposal was created is recorded",
			rp("SNET-GOV-003"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1) at time c", mA, govV, call("proposeGasTip(uint256)", t1)),
				stAt("B.approveProposal(1) at c + expiry", mB, govV, approve(1), week),
			}},
		{"v14_vote_after_expiry", "B-05 V-14: a vote expiry + 1 seconds after creation does not revert: the proposal becomes Expired (ProposalExpired), the vote is not recorded and the proposer's active count is decremented",
			rp("SNET-GOV-003", "SNET-GOV-041"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1) at time c", mA, govV, call("proposeGasTip(uint256)", t1)),
				stAt("B.approveProposal(1) at c + expiry + 1", mB, govV, approve(1), week+1),
			}},
		{"v15_too_many_active_proposals", "B-05 V-15: A's fourth live proposal reverts with TooManyActiveProposals (maximum 3); after A cancels one, the fourth succeeds",
			rp("@r47"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1) #1", mA, govV, call("proposeGasTip(uint256)", t1)),
				st("A.proposeGasTip(T1 + 1) #2", mA, govV, call("proposeGasTip(uint256)", new(big.Int).Add(t1, big.NewInt(1)))),
				st("A.proposeGasTip(T1 + 2) #3", mA, govV, call("proposeGasTip(uint256)", new(big.Int).Add(t1, big.NewInt(2)))),
				st("A.proposeGasTip(T1 + 3): reverts", mA, govV, call("proposeGasTip(uint256)", new(big.Int).Add(t1, big.NewInt(3)))),
				st("A.cancelProposal(2)", mA, govV, call("cancelProposal(uint256)", u256b(2))),
				st("A.proposeGasTip(T1 + 3) #4", mA, govV, call("proposeGasTip(uint256)", new(big.Int).Add(t1, big.NewInt(3)))),
			}},
		{"v16_cancel_after_vote", "B-05 V-16: after B disapproves A's proposal, A cannot cancel it (ProposalAlreadyInVoting)",
			rp("SNET-GOV-044", "@r47"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1)", mA, govV, call("proposeGasTip(uint256)", t1)),
				st("B.disapproveProposal(1)", mB, govV, disapprove(1)),
				st("A.cancelProposal(1): reverts", mA, govV, call("cancelProposal(uint256)", u256b(1))),
			}},
		{"v17_rejection", "B-05 V-17: with quorum 3 of 4 the second disapproval rejects the proposal (2 > 4 - 3)",
			rp("SNET-GOV-023", "SNET-GOV-044"), g, stdNamed, []govStep{
				st("A.proposeGasTip(T1)", mA, govV, call("proposeGasTip(uint256)", t1)),
				st("B.disapproveProposal(1)", mB, govV, disapprove(1)),
				st("C.disapproveProposal(1): Rejected", mC, govV, disapprove(1)),
			}},
		{"v20_single_member", "B-05 V-20: an instance with one member and quorum 1: the proposal is executed inside the creating call",
			r("SNET-GOV-020", "SNET-GOV-031", "SNET-GOV-032", "SNET-GOV-112"), one, []int{0, mA}, []govStep{
				st("A.proposeGasTip(T1): executed at creation", mA, govV, call("proposeGasTip(uint256)", t1)),
			}},
		{"v23_retry_limit", "B-05 V-23: a skipped GovCouncil proposal is attempted three more times in retry mode (ProposalExecuted(false) each time); the next attempt makes it Failed with ProposalFailed(\"Max retry count reached\")",
			rp("SNET-GOV-050", "SNET-GOV-052", "SNET-GOV-053", "SNET-GOV-055", "SNET-GOV-133"), g, stdNamed, []govStep{
				st("A.proposeAddBlacklist(X) #1", mA, govC, call("proposeAddBlacklist(address)", addr(aX))),
				st("B.proposeAddBlacklist(X) #2", mB, govC, call("proposeAddBlacklist(address)", addr(aX))),
				st("C.approveProposal(1)", mC, govC, approve(1)),
				st("D.approveProposal(1): X blacklisted", mD, govC, approve(1)),
				st("C.approveProposal(2)", mC, govC, approve(2)),
				st("D.approveProposal(2): attempt 1 skipped", mD, govC, approve(2)),
				st("A.executeProposal(2): attempt 2", mA, govC, call("executeProposal(uint256)", u256b(2))),
				st("B.executeProposal(2): attempt 3", mB, govC, call("executeProposal(uint256)", u256b(2))),
				st("C.executeProposal(2): Failed", mC, govC, call("executeProposal(uint256)", u256b(2))),
			}},
		{"v24_batch_with_duplicate", "B-05 V-24: a GovCouncil batch [X, X, Y] is executed: X is blacklisted once, the second X is skipped with a log, Y is blacklisted",
			rp("SNET-GOV-131", "SNET-GOV-133", "SNET-GOV-134", "SNET-GOV-137"), g, stdNamed, []govStep{
				st("A.proposeAddBlacklistBatch([X, X, Y])", mA, govC, call("proposeAddBlacklistBatch(address[])", []common.Address{addr(aX), addr(aX), addr(aY)})),
				st("B.approveProposal(1)", mB, govC, approve(1)),
				st("C.approveProposal(1): executed", mC, govC, approve(1)),
			}},
	}
	dup := govGenesis{
		validator: govParams{members: []int{mA, mB, mC, mA}, quorum: 2, expiry: week, maxProps: 3, validators: []int{0, 1, 2, 3}, gasTip: t0},
		council:   govParams{members: []int{mA, mB, mC, mD}, quorum: 3, expiry: week, maxProps: 3},
		funded:    []int{mA, mB, mC, mD},
	}
	cs = append(cs, govCase{"v26_duplicated_genesis_member", "B-05 V-26: a genesis whose GovValidator members list A twice, paired with vA and vD: the projection shows both validators; removing A removes only the validator paired with the later entry",
		rp("SNET-GOV-071", "SNET-GOV-076", "@r39"), dup, stdNamed, []govStep{
			st("B.proposeRemoveMember(A, 2)", mB, govV, call("proposeRemoveMember(address,uint32)", addr(mA), uint32(2))),
			st("C.approveProposal(1): executed", mC, govV, approve(1)),
		}})
	var out []Case
	for _, x := range cs {
		c := x.run()
		c.Desc += "; each step runs as a transaction message of its own block (core.ApplyMessage, as core.ApplyTransaction does) on the state the previous step left"
		out = append(out, c)
	}
	return out
}
