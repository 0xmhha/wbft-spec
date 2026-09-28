// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Overlay test for spec chapter A-04. Injected into package wbftengine with
// `go test -overlay`; the reference repository is not modified.
package wbftengine

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/consensus/wbft/validator"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/systemcontracts"
)

func addrN(i int) common.Address { return common.BigToAddress(big.NewInt(int64(0x1000 + i))) }

// ---------------------------------------------------------------- F / Q table
func TestSpecA04_Quorum(t *testing.T) {
	fmt.Println("N | F (float64, %.17g) | Q=ceil(N-F) | F+1 trigger | QBFT-paper f=floor((N-1)/3) | ceil((N+f+1)/2) | Quorum-qbft ceil(2N/3)")
	for n := 0; n <= 22; n++ {
		addrs := make([]common.Address, n)
		keys := make([][]byte, n)
		for i := range addrs {
			addrs[i] = addrN(i)
			keys[i] = []byte{byte(i)}
		}
		vs := validator.NewSet(addrs, keys, wbft.NewRoundRobinProposerPolicy())
		trig := []int{}
		for num := 0; num <= n+2; num++ {
			if float64(num) > vs.F() && float64(num) <= vs.F()+1 {
				trig = append(trig, num)
			}
		}
		f := 0
		if n > 0 {
			f = (n - 1) / 3
		}
		fmt.Printf("%d | %.17g | %d | %v | %d | %d | %d\n", n, vs.F(), vs.QuorumSize(), trig, f,
			int(math.Ceil(float64(n+f+1)/2)), int(math.Ceil(float64(2*n)/3)))
	}
}

// ------------------------------------------------------------ IsProposer edge
func TestSpecA04_EmptySetIsProposer(t *testing.T) {
	vs := validator.NewSet(nil, nil, wbft.NewRoundRobinProposerPolicy())
	vs.CalcProposer(common.Address{}, 0)
	fmt.Println("empty set: IsProposer(0x..01) =", vs.IsProposer(common.BigToAddress(big.NewInt(1))), "Q =", vs.QuorumSize(), "F =", vs.F())

	// same address twice with different BLS keys
	a := addrN(0)
	vs2 := validator.NewSet([]common.Address{a, addrN(1), a}, [][]byte{{1}, {2}, {3}}, wbft.NewRoundRobinProposerPolicy())
	vs2.CalcProposer(addrN(1), 0) // RR: idx(1)+0+1 = 2 -> third entry (a, key 3)
	fmt.Println("dup address: proposer index-2 entry; IsProposer(a) =", vs2.IsProposer(a))
	vs2.CalcProposer(addrN(1), 1) // RR: 1+1+1=3 mod 3 = 0 -> first entry (a, key 1)
	fmt.Println("dup address: proposer index-0 entry; IsProposer(a) =", vs2.IsProposer(a))
}

// -------------------------------------------------------- proposer examples
func TestSpecA04_Proposer(t *testing.T) {
	addrs := []common.Address{addrN(0), addrN(1), addrN(2), addrN(3)}
	keys := [][]byte{{0}, {1}, {2}, {3}}
	for _, pol := range []*wbft.ProposerPolicy{wbft.NewRoundRobinProposerPolicy(), wbft.NewStickyProposerPolicy(), wbft.NewProposerPolicy(7)} {
		vs := validator.NewSet(addrs, keys, pol)
		for _, last := range []common.Address{{}, addrN(1), addrN(3), addrN(9)} {
			out := []int{}
			for r := uint64(0); r < 6; r++ {
				vs.CalcProposer(last, r)
				i, _ := vs.GetByAddress(vs.GetProposer().Address())
				out = append(out, i)
			}
			fmt.Printf("policy=%d last=%x rounds0..5 -> %v\n", pol.Id, last[18:], out)
		}
	}
}

// ------------------------------------------------------------- epoch blocks
func TestSpecA04_EpochBlocks(t *testing.T) {
	u := func(x uint64) *uint64 { return &x }
	cfg := &wbft.Config{Epoch: 10, ProposerPolicy: wbft.NewRoundRobinProposerPolicy(), Transitions: []params.Transition{
		{Block: big.NewInt(25), WBFTConfig: &params.WBFTConfig{EpochLength: 7}},
		{Block: big.NewInt(40), WBFTConfig: &params.WBFTConfig{BlockPeriodSeconds: 2, ProposerPolicy: u(1)}},
		{Block: big.NewInt(50), WBFTConfig: &params.WBFTConfig{EpochLength: 7}},
	}}
	e := NewEngine(cfg, common.Address{}, nil, nil)
	eps := []uint64{}
	for n := uint64(0); n <= 70; n++ {
		is, last, _ := e.IsEpochBlockNumber(nil, new(big.Int).SetUint64(n))
		if is {
			eps = append(eps, n)
		}
		if n == 24 || n == 25 || n == 26 || n == 31 || n == 32 || n == 45 || n == 49 || n == 50 || n == 56 || n == 57 || n == 70 {
			fmt.Printf("n=%d is_epoch=%v last_epoch_block=%v policy=%d\n", n, is, last, cfg.GetConfig(new(big.Int).SetUint64(n)).ProposerPolicy.Id)
		}
	}
	fmt.Println("epoch blocks 0..70:", eps)
}

// ----------------------------------------------------------------- shuffle
func TestSpecA04_Shuffle(t *testing.T) {
	var seed [32]byte
	copy(seed[:], crypto.Keccak256([]byte("wbft-spec-a04")))
	fmt.Printf("seed = %x\n", seed)
	for _, n := range []uint64{1, 2, 3, 4, 5, 8} {
		perm := []uint64{}
		for i := uint64(0); i < n; i++ {
			j, _ := computeShuffledIndex(i, n, seed, true)
			perm = append(perm, j)
		}
		fmt.Printf("n=%d shuffled(i) for i=0..n-1: %v\n", n, perm)
	}
	var zero [32]byte
	perm := []uint64{}
	for i := uint64(0); i < 4; i++ {
		j, _ := computeShuffledIndex(i, 4, zero, true)
		perm = append(perm, j)
	}
	fmt.Printf("zero seed n=4: %v\n", perm)
	_, err := computeShuffledIndex(4, 4, zero, true)
	fmt.Println("index==count err:", err)
	// trace first 3 rounds for index 0, n = 4
	idx, n := uint64(0), uint64(4)
	buf := make([]byte, 37)
	copy(buf, seed[:])
	for r := 0; r < 3; r++ {
		buf[32] = byte(r)
		h := crypto.Keccak256(buf[:33])
		pivot := binary.LittleEndian.Uint64(h[:8]) % n
		flip := (pivot + n - idx) % n
		pos := idx
		if flip > pos {
			pos = flip
		}
		binary.LittleEndian.PutUint32(buf[33:], uint32(pos>>8))
		src := crypto.Keccak256(buf)
		bit := (src[(pos&0xff)>>3] >> (pos & 7)) & 1
		fmt.Printf("trace r=%d h[:8]=%x pivot=%d flip=%d pos=%d src=%x.. byte=%02x bit=%d\n", r, h[:8], pivot, flip, pos, src[:4], src[(pos&0xff)>>3], bit)
		if bit == 1 {
			idx = flip
		}
	}
}

// ------------------------------------------------------------ sort ties
func TestSpecA04_SortTies(t *testing.T) {
	for _, n := range []int{12, 13, 20} {
		c := make([]PoweredCandidate, n)
		for i := range c {
			c[i] = PoweredCandidate{Addr: addrN(i), Power: big.NewInt(1), Diligence: 1900000}
		}
		fmt.Printf("all-equal n=%d sortCandidates=%v\n", n, sortCandidates(c))
	}
}

// ---------------------------------------------------- next-epoch scenario
type specAcct struct {
	account
}

func detAccounts(n int) []account {
	out := []account{}
	for i := 0; i < n; i++ {
		k, _ := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("wbft-spec-a04-key-%d", i))))
		b, _ := bls.DeriveFromECDSA(k)
		out = append(out, account{key: k, blsKey: b, addr: crypto.PubkeyToAddress(k.PublicKey)})
	}
	return out
}

func stateFor(cfg *wbft.Config, chainCfg *params.ChainConfig, accts []account) *state.StateDB {
	updateChainConfig(chainCfg, accts)
	db := rawdb.NewMemoryDatabase()
	sdb, _ := state.New(types.EmptyRootHash, state.NewDatabase(db), nil)
	cfg.SystemContractUpgrades = nil
	wbft.SetConfigFromChainConfig(cfg, chainCfg)
	st, _ := wbft.GetSystemContractsStateTransition(cfg, big.NewInt(0))
	for _, c := range st.Codes {
		sdb.SetCode(c.Address, hexutil.MustDecode(c.Code))
	}
	for _, s := range st.States {
		sdb.SetState(s.Address, s.Key, s.Value)
	}
	return sdb
}

func bitmap(idx []int) *types.WBFTAggregatedSeal {
	var s types.SealerSet
	for _, i := range idx {
		s.SetSealer(uint32(i))
	}
	return &types.WBFTAggregatedSeal{Sealers: s, Signature: []byte{1}}
}

func TestSpecA04_NextEpoch(t *testing.T) {
	accts := detAccounts(5)
	for i, a := range accts {
		fmt.Printf("acct v%d = %s\n", i, a.addr.Hex())
	}
	c := new(fakeChain)
	c.chainConfig = params.TestWBFTChainConfig
	cfg := new(wbft.Config)
	st4 := stateFor(cfg, c.chainConfig, accts[:4])
	st5 := stateFor(cfg, c.chainConfig, accts[:5])
	st5nokey := stateFor(cfg, c.chainConfig, accts[:5])
	gv := c.chainConfig.Anzeon.SystemContracts.GovValidator.Address
	st5nokey.SetState(gv, systemcontracts.CalculateMappingSlot(common.HexToHash(systemcontracts.SLOT_VALIDATOR_validatorToBlsKey), accts[2].addr), common.Hash{})
	fmt.Printf("v2 BLS key len after clearing = %d\n", len(systemcontracts.GetBLSPublicKey(gv, st5nokey, accts[2].addr)))
	cfg.Epoch = 4
	e := NewEngine(cfg, common.Address{}, nil, nil)
	g := makeGenesis(accts[:4])
	g.Time = 0
	c.insertHeader(g)

	// per block: commit round, prev prepared sealers, prev committed sealers (indices into validators_at(n-1))
	type blk struct {
		round     uint64
		pp, pc    []int
		stateAfter *state.StateDB
	}
	all4 := []int{0, 1, 2, 3}
	plan := map[uint64]blk{
		1: {0, nil, nil, st4},
		2: {1, all4, []int{0, 1, 2}, st4},
		3: {0, all4, all4, st4},
		4: {0, []int{0, 1, 2}, []int{0, 1, 2}, st5}, // v4 registered during block 4? (state after block 4 has 5 candidates)
		5: {0, all4, all4, st5},
		6: {0, all4, all4, st5},
		7: {0, all4, all4, st5},
		8: {0, all4, all4, st5},
		9: {0, all4, all4, st5},
		10: {0, []int{0, 1, 2, 3, 4}, []int{0, 1, 2, 3, 4}, st5},
		11: {2, []int{0, 1, 2, 3, 4}, []int{0, 1, 2, 3, 4}, st5},
		12: {0, []int{0, 1, 2, 3, 4}, []int{0, 1, 2, 3}, st5nokey},
	}
	parent := g
	lastProposer := common.Address{}
	for n := uint64(1); n <= 12; n++ {
		p := plan[n]
		vs, err := e.GetValidators(c, new(big.Int).SetUint64(n), parent.Hash(), nil)
		if err != nil {
			t.Fatal(err)
		}
		vs.CalcProposer(lastProposer, p.round)
		h := makeHeader(parent)
		h.Time = n
		h.Coinbase = vs.GetProposer().Address()
		h.MixDigest = crypto.Keccak256Hash(new(big.Int).SetUint64(n).Bytes())
		var pp, pc *types.WBFTAggregatedSeal
		if p.pp != nil {
			pp, pc = bitmap(p.pp), bitmap(p.pc)
		}
		ApplyHeaderWBFTExtra(h, WritePrevSeals(0, pp, pc), WriteEpochInfo(nil))
		ne, err := e.buildEpochInfo(c, h, p.stateAfter)
		if err != nil {
			fmt.Printf("block %d buildEpochInfo err: %v\n", n, err)
		}
		ci, _ := vs.GetByAddress(h.Coinbase)
		fmt.Printf("block %d: V=%v round=%d coinbase=v-index-in-V %d (%s)\n", n, addrIdx(accts, vs.AddressList()), p.round, ci, h.Coinbase.Hex()[:10])
		if ne != nil {
			ApplyHeaderWBFTExtra(h, WriteEpochInfo(ne))
			fmt.Printf("  mix=%x\n", h.MixDigest)
			for i, cd := range ne.Candidates {
				fmt.Printf("  candidate[%d] = v%d diligence=%d\n", i, addrIdx(accts, []common.Address{cd.Addr})[0], cd.Diligence)
			}
			fmt.Printf("  validators(indices)=%v\n", ne.Validators)
		}
		c.insertHeader(h)
		parent = h
		lastProposer = h.Coinbase
	}
}

func addrIdx(accts []account, as []common.Address) []int {
	out := []int{}
	for _, a := range as {
		f := -1
		for i, x := range accts {
			if x.addr == a {
				f = i
			}
		}
		out = append(out, f)
	}
	return out
}



