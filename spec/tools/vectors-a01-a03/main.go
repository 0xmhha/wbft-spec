// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Vector generator for WBFT spec chapters A-02 / A-03.
// Uses go-stablenet at commit 740526d03 through a go.mod replace directive.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/consensus/wbft/messages"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func key(i int) *ecdsa.PrivateKey {
	// key_i = keccak256("wbft-spec-vector-key-" || decimal(i))
	d := crypto.Keccak256([]byte(fmt.Sprintf("wbft-spec-vector-key-%d", i)))
	k, err := crypto.ToECDSA(d)
	must(err)
	return k
}

func enc(v interface{}) []byte {
	b, err := rlp.EncodeToBytes(v)
	must(err)
	return b
}

func sign(k *ecdsa.PrivateKey, data []byte) []byte {
	s, err := crypto.Sign(crypto.Keccak256(data), k)
	must(err)
	return s
}

func section(s string) { fmt.Printf("\n===== %s\n", s) }

func main() {
	keys := []*ecdsa.PrivateKey{key(0), key(1), key(2), key(3)}
	blsKeys := make([]bls.SecretKey, 4)
	section("KEYS")
	for i, k := range keys {
		sk, err := bls.DeriveFromECDSA(k)
		must(err)
		blsKeys[i] = sk
		fmt.Printf("key%d ecdsa_priv=%x\n", i, crypto.FromECDSA(k))
		fmt.Printf("key%d address=%s\n", i, crypto.PubkeyToAddress(k.PublicKey).Hex())
		fmt.Printf("key%d bls_sk=%x\n", i, sk.Marshal())
		fmt.Printf("key%d bls_pk=%x\n", i, sk.PublicKey().Marshal())
	}

	section("ECDSA")
	msg := []byte("wbft")
	sig := sign(keys[0], msg)
	fmt.Printf("keccak256(\"wbft\")=%x\n", crypto.Keccak256(msg))
	fmt.Printf("sig(key0, \"wbft\")=%x\n", sig)
	addr, err := wbft.GetSignatureAddress(msg, sig)
	fmt.Printf("recover=%s err=%v\n", addr.Hex(), err)
	// high-S variant
	n := crypto.S256().Params().N
	s := new(big.Int).SetBytes(sig[32:64])
	hs := new(big.Int).Sub(n, s)
	sig2 := make([]byte, 65)
	copy(sig2, sig[:32])
	copy(sig2[32:64], common.LeftPadBytes(hs.Bytes(), 32))
	sig2[64] = sig[64] ^ 1
	addr2, err2 := wbft.GetSignatureAddress(msg, sig2)
	fmt.Printf("highS sig=%x recover=%s err=%v\n", sig2, addr2.Hex(), err2)
	sig3 := append([]byte{}, sig...)
	sig3[64] = 27
	_, err3 := wbft.GetSignatureAddress(msg, sig3)
	fmt.Printf("V=27 err=%v\n", err3)
	sig4 := append([]byte{}, sig...)
	sig4[64] = 2
	a4, err4 := wbft.GetSignatureAddress(msg, sig4)
	fmt.Printf("V=2 recover=%s err=%v\n", a4.Hex(), err4)
	_, err5 := wbft.GetSignatureAddress(msg, sig[:64])
	fmt.Printf("len64 err=%v\n", err5)

	section("BLS basic")
	m32 := crypto.Keccak256([]byte("wbft-bls"))
	bsig := blsKeys[0].Sign(m32)
	fmt.Printf("msg=%x\nsig0=%x\n", m32, bsig.Marshal())
	fmt.Printf("verify=%v\n", bsig.Verify(blsKeys[0].PublicKey(), m32))
	// infinity pubkey
	inf := make([]byte, 48)
	inf[0] = 0xc0
	_, err = bls.PublicKeyFromBytes(inf)
	fmt.Printf("pk infinity err=%v\n", err)
	_, err = bls.PublicKeyFromBytes(make([]byte, 48))
	fmt.Printf("pk zero bytes err=%v\n", err)
	_, err = bls.PublicKeyFromBytes(make([]byte, 47))
	fmt.Printf("pk len47 err=%v\n", err)
	infs := make([]byte, 96)
	infs[0] = 0xc0
	isig, err := bls.SignatureFromBytes(infs)
	fmt.Printf("sig infinity from bytes err=%v\n", err)
	if isig != nil {
		fmt.Printf("sig infinity verify=%v\n", isig.Verify(blsKeys[0].PublicKey(), m32))
	}
	_, err = bls.SignatureFromBytes(make([]byte, 96))
	fmt.Printf("sig zero bytes err=%v\n", err)
	_, err = bls.AggregateCompressedSignatures([][]byte{})
	fmt.Printf("aggregate empty err=%v\n", err)
	agg0, err := bls.AggregateCompressedSignatures([][]byte{})
	if err == nil {
		fmt.Printf("aggregate empty = %x\n", agg0.Marshal())
	}
	_, err = bls.AggregateCompressedSignatures([][]byte{infs})
	fmt.Printf("aggregate [inf] err=%v\n", err)

	section("RANDAO")
	for _, c := range []struct {
		chain int64
		num   int64
	}{{8282, 0}, {8282, 1}, {8282, 256}, {8283, 1}} {
		cfg := &params.ChainConfig{ChainID: big.NewInt(c.chain)}
		var pre []byte
		pre = append(pre, big.NewInt(c.chain).Bytes()...)
		pre = append(pre, 0x01)
		pre = append(pre, big.NewInt(c.num).Bytes()...)
		rd := crypto.Keccak256(pre)
		fmt.Printf("chain=%d num=%d preimage=%x randao_data=%x\n", c.chain, c.num, pre, rd)
		_ = cfg
	}
	cfg := &params.ChainConfig{ChainID: big.NewInt(8282)}
	pre := append(append(big.NewInt(8282).Bytes(), 0x01), big.NewInt(1).Bytes()...)
	rd := crypto.Keccak256(pre)
	reveal := sign(keys[0], rd)
	fmt.Printf("reveal(key0, 8282, 1)=%x\n", reveal)
	parentMix := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
	mix := wbftengine.CalculateRandaoMix(parentMix, reveal)
	fmt.Printf("keccak(reveal)=%x mix(parent=0)=%x\n", crypto.Keccak256(reveal), mix)
	pm2 := common.HexToHash("0x00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff")
	fmt.Printf("mix(parent=%x)=%x\n", pm2, wbftengine.CalculateRandaoMix(pm2, reveal))
	selfx := common.BytesToHash(crypto.Keccak256(reveal))
	fmt.Printf("mix(parent=keccak(reveal))=%x\n", wbftengine.CalculateRandaoMix(selfx, reveal))
	// reveal verification path
	fmt.Printf("checkSig=%v\n", func() error {
		a, e := wbft.GetSignatureAddress(rd, reveal)
		if e != nil {
			return e
		}
		if a != crypto.PubkeyToAddress(keys[0].PublicKey) {
			return fmt.Errorf("mismatch")
		}
		return nil
	}())
	_ = cfg

	section("SEALERSET")
	for _, idx := range [][]uint32{{}, {0}, {0, 1, 2}, {0, 8}, {9}, {3, 1}, {15}, {16}} {
		var ss types.SealerSet
		for _, i := range idx {
			ss.SetSealer(i)
		}
		fmt.Printf("indices=%v bitmap=%x rlp=%x getSealers=%v\n", idx, []byte(ss), enc([]byte(ss)), ss.GetSealers())
	}
	fmt.Printf("bitmap 0x0700 getSealers=%v\n", types.SealerSet{0x07, 0x00}.GetSealers())

	section("EXTRA nil conventions")
	emptyExtra := &types.WBFTExtra{}
	fmt.Printf("WBFTExtra{} = %x\n", enc(emptyExtra))
	dec := new(types.WBFTExtra)
	must(rlp.DecodeBytes(enc(emptyExtra), dec))
	fmt.Printf("decoded GasTip=%v (nil=%v) PrevPreparedSeal nil=%v EpochInfo nil=%v Vanity=%v(nil=%v)\n", dec.GasTip, dec.GasTip == nil, dec.PrevPreparedSeal == nil, dec.EpochInfo == nil, dec.VanityData, dec.VanityData == nil)
	fmt.Printf("re-encode = %x\n", enc(dec))
	// 0x80 in seal position
	bad := []byte{0xca, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0xc0, 0xc0, 0x80, 0xc0}
	err = rlp.DecodeBytes(bad, new(types.WBFTExtra))
	fmt.Printf("0x80 for PrevPreparedSeal: %x err=%v\n", bad, err)
	bad2 := []byte{0xca, 0x80, 0x80, 0x80, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0xc0, 0xc0}
	err = rlp.DecodeBytes(bad2, new(types.WBFTExtra))
	fmt.Printf("0xc0 for GasTip: %x err=%v\n", bad2, err)
	bad3 := []byte{0xc9, 0x80, 0x80, 0x80, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0x80}
	err = rlp.DecodeBytes(bad3, new(types.WBFTExtra))
	fmt.Printf("9 elements: %x err=%v\n", bad3, err)
	bad4 := []byte{0xcb, 0x80, 0x80, 0x80, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0x80, 0xc0, 0x80}
	err = rlp.DecodeBytes(bad4, new(types.WBFTExtra))
	fmt.Printf("11 elements: %x err=%v\n", bad4, err)
	bad5 := append(enc(emptyExtra), 0x00)
	err = rlp.DecodeBytes(bad5, new(types.WBFTExtra))
	fmt.Printf("trailing byte: %x err=%v\n", bad5, err)
	bad6 := []byte{0xca, 0x80, 0x80, 0x00, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0x80, 0xc0}
	err = rlp.DecodeBytes(bad6, new(types.WBFTExtra))
	fmt.Printf("PrevRound 0x00: %x err=%v\n", bad6, err)
	bad7 := []byte{0xce, 0x80, 0x80, 0x85, 0x01, 0x00, 0x00, 0x00, 0x00, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0x80, 0xc0}
	err = rlp.DecodeBytes(bad7, new(types.WBFTExtra))
	fmt.Printf("PrevRound 2^32: %x err=%v\n", bad7, err)
	gt0 := []byte{0xca, 0x80, 0x80, 0x80, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0x00, 0xc0}
	err = rlp.DecodeBytes(gt0, new(types.WBFTExtra))
	fmt.Printf("GasTip 0x00: %x err=%v\n", gt0, err)
	gtz := []byte{0xcb, 0x80, 0x80, 0x80, 0xc0, 0xc0, 0x80, 0xc0, 0xc0, 0x81, 0x00, 0xc0}
	err = rlp.DecodeBytes(gtz, new(types.WBFTExtra))
	fmt.Printf("GasTip 0x8100: %x err=%v\n", gtz, err)
	// aggregated seal present but empty
	es := &types.WBFTExtra{PreparedSeal: &types.WBFTAggregatedSeal{}}
	fmt.Printf("PreparedSeal = {} : %x\n", enc(es))
	// round truncation
	fmt.Printf("uint32(2^32+5)=%d\n", uint32(new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 32), big.NewInt(5)).Uint64()))

	section("EPOCHINFO / GENESIS EXTRA")
	ei := &types.EpochInfo{}
	for i := 0; i < 4; i++ {
		ei.Candidates = append(ei.Candidates, &types.Candidate{Addr: crypto.PubkeyToAddress(keys[i].PublicKey), Diligence: types.DefaultDiligence})
		ei.Validators = append(ei.Validators, uint32(i))
		ei.BLSPublicKeys = append(ei.BLSPublicKeys, blsKeys[i].PublicKey().Marshal())
	}
	fmt.Printf("candidate0 rlp=%x\n", enc(ei.Candidates[0]))
	fmt.Printf("EpochInfo(4 validators) rlp=%x\n", enc(ei))
	gen := &types.WBFTExtra{EpochInfo: ei, GasTip: new(big.Int).SetUint64(params.InitialGasTip)}
	genb := enc(gen)
	fmt.Printf("genesis extra len=%d rlp=%x\n", len(genb), genb)
	fmt.Printf("EpochInfo{} rlp=%x\n", enc(&types.EpochInfo{}))
	fmt.Printf("DefaultDiligence=%d InitialGasTip rlp=%x\n", types.DefaultDiligence, enc(new(big.Int).SetUint64(params.InitialGasTip)))

	section("HEADER / BLOCK HASH / SEAL DATA")
	// block 2 on chain 8282, proposer key0, validators key0..3
	vanity := make([]byte, 32)
	pre2 := append(append(big.NewInt(8282).Bytes(), 0x01), big.NewInt(2).Bytes()...)
	reveal2 := sign(keys[0], crypto.Keccak256(pre2))
	// previous block seals (block 1 committed at round 0 by validators 0,1,2)
	parentHeaderHashPlaceholder := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	_ = parentHeaderHashPlaceholder
	extra := &types.WBFTExtra{
		VanityData:   vanity,
		RandaoReveal: reveal2,
		PrevRound:    0,
		Round:        0,
		GasTip:       new(big.Int).SetUint64(params.InitialGasTip),
	}
	h := &types.Header{
		ParentHash:  common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    crypto.PubkeyToAddress(keys[0].PublicKey),
		Root:        common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(2),
		GasLimit:    105000000,
		GasUsed:     0,
		Time:        1700000002,
		Extra:       enc(extra),
		MixDigest:   common.Hash{},
		BaseFee:     big.NewInt(20000000000000),
	}
	fmt.Printf("proposal extra=%x\n", h.Extra)
	fmt.Printf("proposal header rlp=%x\n", enc(h))
	fmt.Printf("block_hash(proposal)=%x\n", h.Hash())
	fmt.Printf("WBFTHashWithRoundNumber(0)=%x (1)=%x (2)=%x\n", h.WBFTHashWithRoundNumber(0), h.WBFTHashWithRoundNumber(1), h.WBFTHashWithRoundNumber(2))
	for _, r := range []uint32{0, 2} {
		for _, t := range []wbftcore.SealType{wbftcore.SealTypePrepare, wbftcore.SealTypeCommit} {
			sd := wbftcore.PrepareSeal(h, r, t)
			fmt.Printf("seal_data(round=%d,type=%d)=%x\n", r, t, sd)
		}
	}
	// seals by 0,1,2 at round 0
	var pSeals, cSeals []wbft.SealData
	for i := 0; i < 3; i++ {
		ps := blsKeys[i].Sign(wbftcore.PrepareSeal(h, 0, wbftcore.SealTypePrepare)).Marshal()
		cs := blsKeys[i].Sign(wbftcore.PrepareSeal(h, 0, wbftcore.SealTypeCommit)).Marshal()
		fmt.Printf("prepare_seal[%d]=%x\ncommit_seal[%d]=%x\n", i, ps, i, cs)
		pSeals = append(pSeals, wbft.SealData{Sealer: uint32(i), Seal: ps})
		cSeals = append(cSeals, wbft.SealData{Sealer: uint32(i), Seal: cs})
	}
	committed := types.CopyHeader(h)
	eng := wbftengine.NewEngine(&wbft.Config{}, common.Address{}, nil, nil)
	must(eng.CommitHeader(committed, pSeals, cSeals, big.NewInt(0)))
	fmt.Printf("committed extra=%x\n", committed.Extra)
	fmt.Printf("block_hash(committed)=%x equal=%v\n", committed.Hash(), committed.Hash() == h.Hash())
	ce, _ := types.ExtractWBFTExtra(committed)
	fmt.Printf("agg prepared sealers=%x sig=%x\n", []byte(ce.PreparedSeal.Sealers), ce.PreparedSeal.Signature)
	fmt.Printf("agg committed sealers=%x sig=%x\n", []byte(ce.CommittedSeal.Sealers), ce.CommittedSeal.Signature)
	// verify aggregated committed seal with pk agg
	var pks [][]byte
	for i := 0; i < 3; i++ {
		pks = append(pks, blsKeys[i].PublicKey().Marshal())
	}
	apk, _ := bls.AggregatePublicKeys(pks)
	asig, _ := bls.SignatureFromBytes(ce.CommittedSeal.Signature)
	fmt.Printf("agg pk=%x verify=%v\n", apk.Marshal(), asig.Verify(apk, wbftcore.PrepareSeal(h, 0, wbftcore.SealTypeCommit)))
	// round truncation in commit header
	c2 := types.CopyHeader(h)
	must(eng.CommitHeader(c2, pSeals, cSeals, new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 32), big.NewInt(3))))
	e2, _ := types.ExtractWBFTExtra(c2)
	fmt.Printf("round 2^32+3 written as %d\n", e2.Round)
	// non-wbft difficulty hash
	h2 := types.CopyHeader(h)
	h2.Difficulty = big.NewInt(2)
	fmt.Printf("difficulty=2 hash=%x keccak(rlp(header))=%x\n", h2.Hash(), crypto.Keccak256(enc(h2)))
	// undecodable extra with difficulty 1
	h3 := types.CopyHeader(h)
	h3.Extra = append(append([]byte{}, h.Extra...), 0x00)
	fmt.Printf("trailing-byte extra hash=%x keccak(rlp(header))=%x\n", h3.Hash(), crypto.Keccak256(enc(h3)))
	// filtered header vs original when unfiltered fields
	fh := types.WBFTFilteredHeader(committed)
	fmt.Printf("filtered extra of committed=%x equals proposal extra=%v\n", fh.Extra, bytes.Equal(fh.Extra, h.Extra))

	section("MESSAGES")
	seq := big.NewInt(2)
	round := big.NewInt(0)
	digest := h.Hash()
	// PREPARE by key1
	prep := messages.NewPrepare(seq, round, digest, pSeals[1].Seal)
	sp, _ := prep.EncodePayloadForSigning()
	prep.SetSignature(sign(keys[1], sp))
	pw := enc(&prep)
	fmt.Printf("PREPARE signing payload=%x\nPREPARE signature=%x\nPREPARE wire=%x\n", sp, prep.Signature(), pw)
	fmt.Printf("PREPARE dedup_key=%x keccak(wire)=%x\n", wbft.RLPHash(pw), crypto.Keccak256(pw))
	m, err := messages.Decode(messages.PrepareCode, pw)
	fmt.Printf("decode err=%v\n", err)
	sp2, _ := m.EncodePayloadForSigning()
	a, err := wbft.GetSignatureAddress(sp2, m.Signature())
	fmt.Printf("recovered=%s err=%v\n", a.Hex(), err)
	// COMMIT by key2
	com := messages.NewCommit(seq, round, digest, cSeals[2].Seal)
	sc, _ := com.EncodePayloadForSigning()
	com.SetSignature(sign(keys[2], sc))
	cw := enc(&com)
	fmt.Printf("COMMIT signing payload=%x\nCOMMIT wire=%x\n", sc, cw)
	// ROUND-CHANGE without prepared
	rc := messages.NewRoundChange(seq, big.NewInt(1), nil, nil)
	src, _ := rc.EncodePayloadForSigning()
	rc.SetSignature(sign(keys[3], src))
	rcw := enc(&rc)
	fmt.Printf("RC(no prepared) signing payload=%x\nRC wire=%x\n", src, rcw)
	// ROUND-CHANGE with prepared block (the proposal) and one prepare justification
	blk := types.NewBlockWithHeader(h)
	rc2 := messages.NewRoundChange(seq, big.NewInt(1), big.NewInt(0), blk)
	rc2.Justification = []*messages.Prepare{prep}
	src2, _ := rc2.EncodePayloadForSigning()
	rc2.SetSignature(sign(keys[3], src2))
	rc2w := enc(&rc2)
	fmt.Printf("RC(prepared r=0) signing payload=%x\nRC2 wire len=%d\n", src2, len(rc2w))
	_, err = messages.Decode(messages.RoundChangeCode, rc2w)
	fmt.Printf("RC2 decode err=%v\n", err)
	// RC with prepared block mismatched digest: craft by replacing digest
	rc3 := messages.NewRoundChange(seq, big.NewInt(1), big.NewInt(0), blk)
	rc3.PreparedDigest = common.HexToHash("0x01")
	rc3.SetSignature(sign(keys[3], src2))
	_, err = messages.Decode(messages.RoundChangeCode, enc(&rc3))
	fmt.Printf("RC3 (digest mismatch) decode err=%v\n", err)
	// RC with prepared round 0 but zero digest -> encodes empty prepared
	rc4 := messages.NewRoundChange(seq, big.NewInt(1), big.NewInt(0), nil)
	s4, _ := rc4.EncodePayloadForSigning()
	fmt.Printf("RC(pr=0, digest=0) signing payload=%x\n", s4)
	// RC outer list with only 1 element
	onlySigned := enc([]interface{}{[]interface{}{rlp.RawValue(mustRaw(rc.SignedRoundChangePayload)), rc.Signature()}})
	_, err = messages.Decode(messages.RoundChangeCode, onlySigned)
	fmt.Printf("RC with 1 outer element decode err=%v\n", err)
	// PRE-PREPARE round 0
	pp := messages.NewPreprepare(seq, round, blk)
	spp, _ := pp.EncodePayloadForSigning()
	pp.SetSignature(sign(keys[0], spp))
	ppw := enc(&pp)
	fmt.Printf("PREPREPARE signing payload len=%d prefix=%x\nPREPREPARE wire len=%d\n", len(spp), spp[:16], len(ppw))
	fmt.Printf("PREPREPARE wire=%x\n", ppw)
	_, err = messages.Decode(messages.PreprepareCode, ppw)
	fmt.Printf("PP decode err=%v\n", err)
	// Pre-prepare with nil proposal
	ppn := messages.NewPreprepare(seq, round, nil)
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("PP nil proposal encode panic=%v\n", r)
			}
		}()
		b, e := rlp.EncodeToBytes(&ppn)
		fmt.Printf("PP nil proposal wire=%x err=%v\n", b, e)
		if e == nil {
			_, e2 := messages.Decode(messages.PreprepareCode, b)
			fmt.Printf("PP nil proposal decode err=%v\n", e2)
		}
	}()
	// View
	fmt.Printf("View{Round:1,Sequence:2}=%x\n", enc(&wbft.View{Round: big.NewInt(1), Sequence: big.NewInt(2)}))
	// Decode prepare with nil sequence (0x80) -> 0
	pz := messages.NewPrepare(big.NewInt(0), big.NewInt(0), digest, nil)
	pz.SetSignature([]byte{})
	pzw := enc(&pz)
	mz, err := messages.Decode(messages.PrepareCode, pzw)
	fmt.Printf("PREPARE seq0 round0 empty seal/sig wire=%x err=%v seq=%v\n", pzw, err, mz.View().Sequence)
	// wrong code mapping
	_, err = messages.Decode(messages.PrepareCode, []byte{0xc0})
	fmt.Printf("PREPARE garbage err=%v\n", err)
	_, err = messages.Decode(0x11, pw)
	fmt.Printf("code 0x11 decode err=%v\n", err)
	// legacy wrapping
	fmt.Printf("0x11 payload for PREPARE = rlp(bytes)=%x... (len %d)\n", enc(pw)[:8], len(enc(pw)))
}

func mustRaw(p messages.SignedRoundChangePayload) []byte {
	b, err := rlp.EncodeToBytes([]interface{}{p.Sequence, p.Round, []interface{}{}})
	must(err)
	return b
}
