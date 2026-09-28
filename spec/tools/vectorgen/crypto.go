// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

package main

// Runner `crypto` (A-02): keccak256, ecdsa_sign, ecdsa_recover, bls_derive,
// bls_sign, bls_verify, bls_aggregate, aggregate_public_keys, seal_data,
// randao_data, randao_mix.

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	wbftcore "github.com/ethereum/go-ethereum/consensus/wbft/core"
	wbftengine "github.com/ethereum/go-ethereum/consensus/wbft/engine"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
)

var (
	secpN = crypto.S256().Params().N
	// BLS12-381 base field modulus p and group order r (A-02 §1).
	blsP, _ = new(big.Int).SetString("1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab", 16)
	blsR, _ = new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
)

func cryptoCases() []Case {
	var cs []Case
	cs = append(cs, keccakCases()...)
	cs = append(cs, ecdsaSignCases()...)
	cs = append(cs, ecdsaRecoverCases()...)
	cs = append(cs, blsDeriveCases()...)
	cs = append(cs, blsSignCases()...)
	cs = append(cs, blsVerifyCases()...)
	cs = append(cs, blsAggregateCases()...)
	cs = append(cs, aggregatePublicKeysCases()...)
	cs = append(cs, sealDataCases()...)
	cs = append(cs, randaoDataCases()...)
	cs = append(cs, randaoMixCases()...)
	return cs
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// ---------------------------------------------------------------- keccak256

func keccakCases() []Case {
	type in struct {
		name, desc string
		data       []byte
	}
	ins := []in{
		{"empty", "empty input", nil},
		{"ascii_wbft", "ASCII \"wbft\" (A-02 §8.2)", []byte("wbft")},
		{"one_zero_byte", "single byte 0x00", []byte{0}},
		{"len32", "32 bytes 00..1f", pattern(32)},
		{"len135", "135 bytes (rate - 1)", pattern(135)},
		{"len136", "136 bytes (exactly one Keccak-256 rate block)", pattern(136)},
		{"len137", "137 bytes (rate + 1)", pattern(137)},
		{"len1000", "1000 bytes", pattern(1000)},
		{"rlp_empty_list", "0xc0 (keccak256 of an absent header, A-03 WBFT-ENC-081)", []byte{0xc0}},
	}
	var cs []Case
	for _, x := range ins {
		cs = append(cs, Case{
			Runner: "crypto", Handler: "keccak256", Name: x.name, Kind: "pure",
			Desc:     "keccak256 (original Keccak padding, not SHA3-256): " + x.desc,
			Reqs:     []string{"WBFT-CRYPTO-001"},
			Input:    M{{"data", hexs(x.data)}},
			Expected: M{{"hash", hexs(crypto.Keccak256(x.data))}},
		})
	}
	return cs
}

// ---------------------------------------------------------------- ECDSA

func ecdsaSignCases() []Case {
	type in struct {
		name, desc string
		key        int
		data       []byte
	}
	var ins []in
	for i := 0; i < numKeys; i++ {
		ins = append(ins, in{fmt.Sprintf("key%d_wbft", i), "ASCII \"wbft\"", i, []byte("wbft")})
	}
	ins = append(ins,
		in{"key0_empty", "empty data", 0, nil},
		in{"key0_randao_8282_1", "randao_data(8282, 1): the randao reveal of A-02 §8.5", 0, randaoData(big.NewInt(8282), big.NewInt(1))},
		in{"key1_len200", "200 bytes", 1, pattern(200)},
	)
	var cs []Case
	for _, x := range ins {
		k := fx.keys[x.key]
		sig := ecdsaSign(k, x.data)
		reqs := []string{"WBFT-CRYPTO-010", "WBFT-CRYPTO-011", "@r02", "WBFT-CRYPTO-017"}
		if x.name == "key0_randao_8282_1" {
			reqs = append(reqs, "@r03")
		}
		if new(big.Int).SetBytes(sig[32:64]).Cmp(new(big.Int).Rsh(secpN, 1)) > 0 {
			panic("reference produced high-S")
		}
		cs = append(cs, Case{
			Runner: "crypto", Handler: "ecdsa_sign", Name: x.name, Kind: "pure",
			Desc: "ecdsa_sign(key, data) = sign(keccak256(data)) with RFC 6979 nonce and low S; R||S||V with V in {0,1}; address = keccak256(X||Y)[12:]. Data: " + x.desc,
			Reqs: reqs,
			Input: M{
				{"private_key", hexs(crypto.FromECDSA(k))},
				{"data", hexs(x.data)},
			},
			Expected: M{
				{"signature", hexs(sig)},
				{"address", hexs(fx.addrs[x.key].Bytes())},
			},
		})
	}
	return cs
}

func pad32(x *big.Int) []byte { return common.LeftPadBytes(x.Bytes(), 32) }

func withRSV(sig []byte, r, s *big.Int, v byte) []byte {
	out := make([]byte, 65)
	copy(out, sig)
	if r != nil {
		copy(out[0:32], pad32(r))
	}
	if s != nil {
		copy(out[32:64], pad32(s))
	}
	out[64] = v
	return out
}

func ecdsaRecoverCases() []Case {
	data := []byte("wbft")
	sig := ecdsaSign(fx.keys[0], data)
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := sig[64]
	highS := withRSV(sig, nil, new(big.Int).Sub(secpN, s), v^1)
	nMinus1 := new(big.Int).Sub(secpN, big.NewInt(1))

	type in struct {
		name, desc string
		reqs       []string
		data, sig  []byte
	}
	base := []string{"WBFT-CRYPTO-013", "WBFT-TYPE-003"}
	ins := []in{
		{"valid_key0", "valid low-S signature of key 0 over \"wbft\" (A-02 §8.2)", base, data, sig},
		{"high_s_twin", "(R, n-S, V^1): the high-S twin is accepted and recovers the same signer (A-02 §8.2)", append(base, "WBFT-CRYPTO-014"), data, highS},
		{"v_flipped", "V^1 with the same R, S: recovers a different key", base, data, withRSV(sig, nil, nil, v^1)},
		{"v2", "V = 2 (R + n is not a valid x): recovery fails", base, data, withRSV(sig, nil, nil, 2)},
		{"v3", "V = 3: recovery fails", base, data, withRSV(sig, nil, nil, 3)},
		{"v4", "V = 4: invalid recovery id in a cgo build (a no-cgo build maps V-4 and accepts; the cgo behaviour is normative, A-02 §3.3 note)", base, data, withRSV(sig, nil, nil, v+4)},
		{"v27", "V = 27 (Ethereum transaction style) is rejected", base, data, withRSV(sig, nil, nil, 27)},
		{"v28", "V = 28 is rejected", base, data, withRSV(sig, nil, nil, 28)},
		{"v255", "V = 255 is rejected", base, data, withRSV(sig, nil, nil, 255)},
		{"len0", "empty signature", base, data, nil},
		{"len64", "64-byte signature (V missing)", base, data, sig[:64]},
		{"len66", "66-byte signature (extra byte)", base, data, append(append([]byte{}, sig...), 0)},
		{"r_zero", "R = 0", base, data, withRSV(sig, big.NewInt(0), nil, v)},
		{"s_zero", "S = 0", base, data, withRSV(sig, nil, big.NewInt(0), v)},
		{"r_eq_n", "R = n", base, data, withRSV(sig, secpN, nil, v)},
		{"s_eq_n", "S = n", base, data, withRSV(sig, nil, secpN, v)},
		{"r_max", "R = 2^256 - 1", base, data, withRSV(sig, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)), nil, v)},
		{"s_n_minus_1", "S = n - 1 (largest S in range): accepted, recovers some key", base, data, withRSV(sig, nil, nMinus1, v)},
		{"other_data", "valid signature presented with different data: recovers a different key", base, []byte("wbfu"), sig},
	}
	_ = r
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "crypto", Handler: "ecdsa_recover", Name: x.name, Kind: "pure",
			Desc:  "ecdsa_recover_address(data, sig) as in a cgo build: " + x.desc,
			Reqs:  x.reqs,
			Input: M{{"data", hexs(x.data)}, {"signature", hexs(x.sig)}},
		}
		addr, err := wbft.GetSignatureAddress(x.data, x.sig)
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = M{{"address", hexs(addr.Bytes())}}
		}
		cs = append(cs, c)
	}
	return cs
}

// ---------------------------------------------------------------- BLS keys, sign

func blsDeriveCases() []Case {
	type in struct {
		name, desc string
		priv       []byte
	}
	var ins []in
	for i := 0; i < numKeys; i++ {
		ins = append(ins, in{fmt.Sprintf("key%d", i), fmt.Sprintf("vector key %d", i), keySeed(i)})
	}
	ins = append(ins,
		in{"priv_one", "node private key d = 1", pad32(big.NewInt(1))},
		in{"priv_n_minus_1", "node private key d = n - 1 (secp256k1 order minus one)", pad32(new(big.Int).Sub(secpN, big.NewInt(1)))},
	)
	var cs []Case
	for _, x := range ins {
		k, err := crypto.ToECDSA(x.priv)
		must(err)
		sk, err := bls.DeriveFromECDSA(k)
		must(err)
		cs = append(cs, Case{
			Runner: "crypto", Handler: "bls_derive", Name: x.name, Kind: "pure",
			Desc:     "derive_bls_key(node_key): IETF KeyGen with IKM = 32-byte node key, empty key_info, L = 48; public key = sk*G1 compressed: " + x.desc,
			Reqs:     []string{"WBFT-CRYPTO-020", "WBFT-CRYPTO-021", "WBFT-CRYPTO-022", "WBFT-TYPE-004"},
			Input:    M{{"private_key", hexs(x.priv)}},
			Expected: M{{"secret_key", hexs(sk.Marshal())}, {"public_key", hexs(sk.PublicKey().Marshal())}},
		})
	}
	return cs
}

func blsSignCases() []Case {
	type in struct {
		name, desc string
		key        int
		msg        []byte
	}
	ins := []in{
		{"key0_wbft_bls", "m = keccak256(\"wbft-bls\") (A-02 §8.3)", 0, crypto.Keccak256([]byte("wbft-bls"))},
		{"key1_prepare_seal_h", "seal_data(H, 0, PREPARE_SEAL) (A-02 §8.4)", 1, wbftcore.PrepareSeal(fx.H, 0, wbftcore.SealTypePrepare)},
		{"key2_commit_seal_h", "seal_data(H, 0, COMMIT_SEAL) (A-02 §8.4)", 2, wbftcore.PrepareSeal(fx.H, 0, wbftcore.SealTypeCommit)},
		{"key3_empty", "empty message (not used by WBFT; checks hash_to_G2 of the empty string)", 3, nil},
		{"key4_pop", "the 48-byte compressed public key of key 4 (proof of possession, A-02 §5.8)", 4, fx.blsPubs[4]},
	}
	var cs []Case
	for _, x := range ins {
		cs = append(cs, Case{
			Runner: "crypto", Handler: "bls_sign", Name: x.name, Kind: "pure",
			Desc: "bls_sign(sk, msg) = sk * hash_to_G2(msg, BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_), no pre-hash: " + x.desc,
			Reqs: []string{"WBFT-CRYPTO-022", "WBFT-CRYPTO-025", "WBFT-CRYPTO-042", "WBFT-TYPE-004"},
			Input: M{
				{"secret_key", hexs(fx.blsKeys[x.key].Marshal())},
				{"message", hexs(x.msg)},
			},
			Expected: M{{"signature", hexs(fx.blsKeys[x.key].Sign(x.msg).Marshal())}},
		})
	}
	return cs
}

// ---------------------------------------------------------------- BLS encodings

func flipSign(b []byte) []byte      { o := append([]byte{}, b...); o[0] ^= 0x20; return o }
func clearCompress(b []byte) []byte { o := append([]byte{}, b...); o[0] &^= 0x80; return o }

func infinity(n int) []byte { o := make([]byte, n); o[0] = 0xc0; return o }

// addP returns the 48-byte field element at b[off:off+48] (flag bits of the
// first byte cleared when flags is true) plus p, with the flags restored.
// ok is false when the result does not fit.
func addP(b []byte, off int, flags bool) ([]byte, bool) {
	o := append([]byte{}, b...)
	part := append([]byte{}, o[off:off+48]...)
	var fl byte
	if flags {
		fl = part[0] & 0xe0
		part[0] &^= 0xe0
	}
	x := new(big.Int).Add(new(big.Int).SetBytes(part), blsP)
	limit := 384
	if flags {
		limit = 381
	}
	if x.BitLen() > limit {
		return nil, false
	}
	nb := common.LeftPadBytes(x.Bytes(), 48)
	nb[0] |= fl
	copy(o[off:off+48], nb)
	return o, true
}

func isQR(a *big.Int) bool {
	a = new(big.Int).Mod(a, blsP)
	if a.Sign() == 0 {
		return true
	}
	e := new(big.Int).Rsh(new(big.Int).Sub(blsP, big.NewInt(1)), 1)
	return new(big.Int).Exp(a, e, blsP).Cmp(big.NewInt(1)) == 0
}

// g1X finds the smallest x >= start for which x^3 + 4 is (or is not) a
// square in Fp, and returns its compressed encoding with the compression flag.
func g1X(start int64, square bool) []byte {
	for x := big.NewInt(start); ; x.Add(x, big.NewInt(1)) {
		rhs := new(big.Int).Add(new(big.Int).Exp(x, big.NewInt(3), nil), big.NewInt(4))
		if isQR(rhs) == square {
			b := common.LeftPadBytes(x.Bytes(), 48)
			b[0] |= 0x80
			return b
		}
	}
}

// g2X finds the smallest k >= 1 such that x = k (x.c1 = 0) gives a point on
// the G2 curve y^2 = x^3 + 4(1+u) (square) or none (not square). In
// Fp2 = Fp[u]/(u^2+1), a = a0 + a1*u is a square iff a0^2 + a1^2 is a square in Fp.
func g2X(square bool) []byte {
	for k := big.NewInt(1); ; k.Add(k, big.NewInt(1)) {
		a0 := new(big.Int).Add(new(big.Int).Exp(k, big.NewInt(3), nil), big.NewInt(4))
		norm := new(big.Int).Add(new(big.Int).Mul(a0, a0), big.NewInt(16))
		if isQR(norm) == square {
			b := make([]byte, 96)
			b[0] = 0x80                                      // x.c1 = 0, compressed
			copy(b[48:], common.LeftPadBytes(k.Bytes(), 48)) // x.c0 = k
			return b
		}
	}
}

type encCase struct {
	name, desc string
	b          []byte
}

// pkEncodings returns public-key encodings exercising A-02 §5.3 and
// WBFT-CRYPTO-056 (decoded through aggregate_public_keys of one key).
func pkEncodings() []encCase {
	pk := fx.blsPubs[0]
	var plusP []byte
	var plusPKey int
	// Search the vector keys (and further keys of the same seed) for a key
	// whose x is small enough that x + p still fits in 381 bits.
	for i := 0; i < 256 && plusP == nil; i++ {
		k, err := crypto.ToECDSA(keySeed(i))
		must(err)
		sk, err := bls.DeriveFromECDSA(k)
		must(err)
		if b, ok := addP(sk.PublicKey().Marshal(), 0, true); ok {
			plusP, plusPKey = b, i
		}
	}
	if plusP == nil {
		panic("no vector public key with x + p < 2^381")
	}
	zeroX := make([]byte, 48)
	zeroX[0] = 0x80
	infBit := infinity(48)
	infBit[47] = 1
	inf40 := make([]byte, 48)
	inf40[0] = 0x40
	return []encCase{
		{"pk_valid_key0", "valid public key of key 0", pk},
		{"pk_sign_flipped", "valid public key with the sign flag flipped: decodes to the negated point", flipSign(pk)},
		{"pk_compression_cleared", "valid public key with the compression flag cleared", clearCompress(pk)},
		{"pk_infinity", "point at infinity c0||47x00 (rejected by KeyValidate)", infinity(48)},
		{"pk_infinity_sign_flag", "infinity with the sign flag, e0||47x00", func() []byte { b := infinity(48); b[0] = 0xe0; return b }()},
		{"pk_infinity_nonzero_bit", "infinity with a non-zero trailing bit, c0||46x00||01", infBit},
		{"pk_infinity_no_compression", "infinity flag without the compression flag, 40||47x00", inf40},
		{"pk_zero_bytes", "48 zero bytes (compression flag clear)", make([]byte, 48)},
		{"pk_x_plus_p", fmt.Sprintf("valid public key of key %d with p added to x (fits in 381 bits)", plusPKey), plusP},
		{"pk_x_zero", "compressed x = 0 with only the compression flag (0x80||47x00): rejected by the reference at decompression", zeroX},
		{"pk_not_on_curve", "smallest x for which x^3 + 4 is not a square: no curve point", g1X(1, false)},
		{"pk_not_in_subgroup", "smallest x >= 1 for which x^3 + 4 is a square: a curve point outside the r-subgroup", g1X(1, true)},
		{"pk_len47", "47 bytes", pk[:47]},
		{"pk_len49", "49 bytes", append(append([]byte{}, pk...), 0)},
	}
}

func sigEncodings() []encCase {
	m := crypto.Keccak256([]byte("wbft-bls"))
	s := fx.blsKeys[0].Sign(m).Marshal()
	c0, ok0 := addP(s, 48, false)
	var c1 []byte
	c1Key := -1
	for i := 0; i < numKeys && c1 == nil; i++ {
		if b, ok := addP(fx.blsKeys[i].Sign(m).Marshal(), 0, true); ok {
			c1, c1Key = b, i
		}
	}
	if c1 == nil || !ok0 {
		panic("no vector signature with x.c1 + p < 2^381")
	}
	infBit := infinity(96)
	infBit[95] = 1
	inf40 := make([]byte, 96)
	inf40[0] = 0x40
	return []encCase{
		{"sig_valid", "valid signature of key 0 over keccak256(\"wbft-bls\")", s},
		{"sig_sign_flipped", "valid signature with the sign flag flipped: decodes to the negated point", flipSign(s)},
		{"sig_compression_cleared", "valid signature with the compression flag cleared", clearCompress(s)},
		{"sig_infinity", "point at infinity c0||95x00: decodes (not rejected at decoding)", infinity(96)},
		{"sig_infinity_sign_flag", "infinity with the sign flag, e0||95x00", func() []byte { b := infinity(96); b[0] = 0xe0; return b }()},
		{"sig_infinity_nonzero_bit", "infinity with a non-zero trailing bit, c0||94x00||01", infBit},
		{"sig_infinity_no_compression", "infinity flag without the compression flag, 40||95x00", inf40},
		{"sig_zero_bytes", "96 zero bytes", make([]byte, 96)},
		{"sig_c1_plus_p", fmt.Sprintf("valid signature of key %d over keccak256(\"wbft-bls\") with p added to the first half (x.c1)", c1Key), c1},
		{"sig_c0_plus_p", "valid signature with p added to the second half (x.c0)", c0},
		{"sig_not_on_curve", "x = k (x.c1 = 0) with no curve point", g2X(false)},
		{"sig_not_in_subgroup", "x = k (x.c1 = 0) on the curve but outside the r-subgroup", g2X(true)},
		{"sig_len95", "95 bytes", s[:95]},
		{"sig_len97", "97 bytes", append(append([]byte{}, s...), 0)},
	}
}

// ---------------------------------------------------------------- BLS verify, aggregate

func listHex(bs ...[]byte) []any {
	out := make([]any, len(bs))
	for i, b := range bs {
		out[i] = hexs(b)
	}
	return out
}

// cancelTriple returns secret keys a, b, c with a + b + c = 0 mod r.
func cancelTriple() []bls.SecretKey {
	a := new(big.Int).SetBytes(fx.blsKeys[0].Marshal())
	b := new(big.Int).SetBytes(fx.blsKeys[1].Marshal())
	c := new(big.Int).Neg(new(big.Int).Add(a, b))
	c.Mod(c, blsR)
	skc, err := bls.SecretKeyFromBytes(pad32(c))
	must(err)
	return []bls.SecretKey{fx.blsKeys[0], fx.blsKeys[1], skc}
}

// blsVerifyRef is the bls_verify handler: aggregate_public_keys(public_keys)
// (a single key is the plain case), decode the signature, then Verify.
func blsVerifyRef(pks [][]byte, msg, sig []byte) (bool, error) {
	apk, err := bls.AggregatePublicKeys(pks)
	if err != nil {
		return false, err
	}
	s, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return false, err
	}
	return s.Verify(apk, msg), nil
}

func blsVerifyCases() []Case {
	m := crypto.Keccak256([]byte("wbft-bls"))
	sig0 := fx.blsKeys[0].Sign(m).Marshal()
	pk0 := fx.blsPubs[0]
	sdc := wbftcore.PrepareSeal(fx.H, 0, wbftcore.SealTypeCommit)
	hcExtra, err := types.ExtractWBFTExtra(fx.Hc)
	must(err)
	tri := cancelTriple()
	var triPks, triSigs [][]byte
	for _, k := range tri {
		triPks = append(triPks, k.PublicKey().Marshal())
		triSigs = append(triSigs, k.Sign(sdc).Marshal())
	}
	triAgg, err := bls.AggregateCompressedSignatures(triSigs)
	must(err)

	type in struct {
		name, desc string
		reqs       []string
		pks        [][]byte
		msg, sig   []byte
	}
	base := []string{"WBFT-CRYPTO-023", "WBFT-CRYPTO-024", "WBFT-CRYPTO-026"}
	ins := []in{
		{"valid", "signature of key 0 over m = keccak256(\"wbft-bls\")", base, [][]byte{pk0}, m, sig0},
		{"wrong_message", "same signature, message keccak256(\"wbft-blt\")", base, [][]byte{pk0}, crypto.Keccak256([]byte("wbft-blt")), sig0},
		{"wrong_key", "same signature, public key of key 1", base, [][]byte{fx.blsPubs[1]}, m, sig0},
		{"sig_infinity", "signature = point at infinity decodes and verifies as false", base, [][]byte{pk0}, m, infinity(96)},
		{"sig_sign_flipped", "negated signature decodes and verifies as false", append(base, "WBFT-CRYPTO-056"), [][]byte{pk0}, m, flipSign(sig0)},
		{"pk_sign_flipped", "negated public key decodes and verifies as false", append(base, "WBFT-CRYPTO-056"), [][]byte{flipSign(pk0)}, m, sig0},
		{"pk_infinity", "public key = point at infinity is rejected at decoding", base, [][]byte{infinity(48)}, m, sig0},
		{"sig_zero_bytes", "96 zero bytes are rejected at decoding", base, [][]byte{pk0}, m, make([]byte, 96)},
		{"sig_not_in_subgroup", "signature outside the r-subgroup is rejected at decoding", base, [][]byte{pk0}, m, g2X(true)},
		{"aggregate_committed_h", "committed aggregate of H (validators 0,1,2) against the aggregate key and seal_data(H, 0, COMMIT_SEAL) (A-02 §8.4)", append(base, "WBFT-CRYPTO-027", "WBFT-CRYPTO-028", "WBFT-CRYPTO-030"), [][]byte{fx.blsPubs[0], fx.blsPubs[1], fx.blsPubs[2]}, sdc, hcExtra.CommittedSeal.Signature},
		{"aggregate_missing_signer", "same aggregate against validators 0,1,3", append(base, "WBFT-CRYPTO-028"), [][]byte{fx.blsPubs[0], fx.blsPubs[1], fx.blsPubs[3]}, sdc, hcExtra.CommittedSeal.Signature},
		{"aggregate_key_infinity", "keys of a, b, (-a-b) mod r sum to infinity; their aggregate signature is c0||95x00 and MUST verify as false", append(base, "WBFT-CRYPTO-055", "WBFT-HDR-100"), triPks, sdc, triAgg.Marshal()},
		{"empty_key_list", "no public key: aggregation of an empty list fails", append(base, "WBFT-CRYPTO-028"), nil, m, sig0},
	}
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "crypto", Handler: "bls_verify", Name: x.name, Kind: "pure",
			Desc: "bls_verify(aggregate_public_keys(public_keys), message, signature); decoding failures of a key or of the signature fail the operation: " + x.desc,
			Reqs: x.reqs,
			Input: M{
				{"public_keys", listHex(x.pks...)},
				{"message", hexs(x.msg)},
				{"signature", hexs(x.sig)},
			},
		}
		ok, err := blsVerifyRef(x.pks, x.msg, x.sig)
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = M{{"valid", ok}}
		}
		cs = append(cs, c)
	}
	return cs
}

func blsAggregateCases() []Case {
	m := crypto.Keccak256([]byte("wbft-bls"))
	s0 := fx.blsKeys[0].Sign(m).Marshal()
	var ps [][]byte
	for _, s := range fx.pSeals {
		ps = append(ps, s.Seal)
	}
	sdc := wbftcore.PrepareSeal(fx.H, 0, wbftcore.SealTypeCommit)
	var triSigs [][]byte
	for _, k := range cancelTriple() {
		triSigs = append(triSigs, k.Sign(sdc).Marshal())
	}

	type in struct {
		name, desc string
		reqs       []string
		sigs       [][]byte
	}
	base := []string{"WBFT-CRYPTO-024", "WBFT-CRYPTO-027"}
	ins := []in{
		{"empty", "aggregate of the empty list is the point at infinity", base, nil},
		{"one_infinity", "an input equal to the point at infinity is accepted", base, [][]byte{infinity(96)}},
		{"prepared_h", "prepare seals of validators 0,1,2 over H (A-02 §8.4 prepared aggregate)", base, ps},
		{"duplicate", "the same signature twice (sum = 2*S; WBFT-CRYPTO-031)", append(base, "WBFT-CRYPTO-031"), [][]byte{s0, s0}},
		{"with_negation", "a signature and its negation sum to infinity", base, [][]byte{s0, flipSign(s0)}},
		{"cancel_triple", "signatures of a, b, (-a-b) mod r over one message sum to infinity", append(base, "WBFT-CRYPTO-055"), triSigs},
		{"valid_then_bad", "a valid signature followed by 96 zero bytes", base, [][]byte{s0, make([]byte, 96)}},
	}
	// Single-signature decoding cases (WBFT-CRYPTO-056): aggregate of one
	// signature re-compresses the decoded point.
	for _, e := range sigEncodings() {
		ins = append(ins, in{"decode_" + e.name, "one signature: " + e.desc, append(base, "WBFT-CRYPTO-022", "WBFT-CRYPTO-056"), [][]byte{e.b}})
	}
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "crypto", Handler: "bls_aggregate", Name: x.name, Kind: "pure",
			Desc:  "bls_aggregate(signatures): compressed sum of decompressed, subgroup-checked signatures: " + x.desc,
			Reqs:  x.reqs,
			Input: M{{"signatures", listHex(x.sigs...)}},
		}
		agg, err := bls.AggregateCompressedSignatures(x.sigs)
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = M{{"signature", hexs(agg.Marshal())}}
		}
		cs = append(cs, c)
	}
	return cs
}

func aggregatePublicKeysCases() []Case {
	var triPks [][]byte
	for _, k := range cancelTriple() {
		triPks = append(triPks, k.PublicKey().Marshal())
	}
	type in struct {
		name, desc string
		reqs       []string
		pks        [][]byte
	}
	base := []string{"WBFT-CRYPTO-023", "WBFT-CRYPTO-028"}
	ins := []in{
		{"empty", "aggregation of an empty list fails", base, nil},
		{"validators_012", "keys of validators 0,1,2 (A-02 §8.4)", base, [][]byte{fx.blsPubs[0], fx.blsPubs[1], fx.blsPubs[2]}},
		{"all_8", "keys 0..7", base, fx.blsPubs},
		{"duplicate", "the same key twice", base, [][]byte{fx.blsPubs[0], fx.blsPubs[0]}},
		{"cancel_triple", "keys of a, b, (-a-b) mod r: the sum is the point at infinity and no error is returned", append(base, "WBFT-CRYPTO-055"), triPks},
		{"valid_then_infinity", "a valid key followed by the point at infinity", base, [][]byte{fx.blsPubs[0], infinity(48)}},
	}
	for _, e := range pkEncodings() {
		ins = append(ins, in{"decode_" + e.name, "one key: " + e.desc, append(base, "WBFT-CRYPTO-022", "WBFT-CRYPTO-056"), [][]byte{e.b}})
	}
	var cs []Case
	for _, x := range ins {
		c := Case{
			Runner: "crypto", Handler: "aggregate_public_keys", Name: x.name, Kind: "pure",
			Desc:  "aggregate_public_keys(public_keys): sum of keys each validated by WBFT-CRYPTO-023: " + x.desc,
			Reqs:  x.reqs,
			Input: M{{"public_keys", listHex(x.pks...)}},
		}
		apk, err := bls.AggregatePublicKeys(x.pks)
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Expected = M{{"public_key", hexs(apk.Marshal())}}
		}
		cs = append(cs, c)
	}
	return cs
}

// ---------------------------------------------------------------- seal data, randao

func sealDataCases() []Case {
	type in struct {
		name, desc string
		reqs       []string
		h          *types.Header
		round      uint32
		st         wbftcore.SealType
	}
	base := []string{"WBFT-CRYPTO-040", "WBFT-CRYPTO-041", "WBFT-TYPE-005"}
	ins := []in{
		{"h_r0_prepare", "header H, round 0, PREPARE_SEAL (A-02 §8.4)", base, fx.H, 0, wbftcore.SealTypePrepare},
		{"h_r0_commit", "header H, round 0, COMMIT_SEAL", base, fx.H, 0, wbftcore.SealTypeCommit},
		{"h_r2_prepare", "header H, round 2, PREPARE_SEAL", base, fx.H, 2, wbftcore.SealTypePrepare},
		{"h_r2_commit", "header H, round 2, COMMIT_SEAL", base, fx.H, 2, wbftcore.SealTypeCommit},
		{"h_rmax_commit", "header H, round 2^32 - 1, COMMIT_SEAL", base, fx.H, 0xffffffff, wbftcore.SealTypeCommit},
		{"hc_r0_commit", "committed header of H (seals present): same value as h_r0_commit", base, fx.Hc, 0, wbftcore.SealTypeCommit},
		{"h3_r1_prepare", "block 3 with prev seals and epoch info, committed at round 1: round 1, PREPARE_SEAL", base, fx.H3, 1, wbftcore.SealTypePrepare},
		{"h3_r1_commit", "block 3, round 1, COMMIT_SEAL", base, fx.H3, 1, wbftcore.SealTypeCommit},
		{"undecodable_r0_prepare", "H with a trailing byte in Extra: the constant keccak256(keccak256(0xc0)||00)", append(base, "WBFT-CRYPTO-044"), fx.Hbad, 0, wbftcore.SealTypePrepare},
		{"undecodable_r7_commit", "H with a trailing byte in Extra, round 7: the constant keccak256(keccak256(0xc0)||01)", append(base, "WBFT-CRYPTO-044"), fx.Hbad, 7, wbftcore.SealTypeCommit},
	}
	var cs []Case
	for _, x := range ins {
		cs = append(cs, Case{
			Runner: "crypto", Handler: "seal_data", Name: x.name, Kind: "pure",
			Desc: "seal_data(header, round, seal_type) = keccak256(hash_with_round(header, round) || seal_type); header given as its RLP: " + x.desc,
			Reqs: x.reqs,
			Input: M{
				{"header", hexs(enc(x.h))},
				{"round", dec(uint64(x.round))},
				{"seal_type", dec(uint64(x.st))},
			},
			Expected: M{{"seal_data", hexs(wbftcore.PrepareSeal(x.h, x.round, x.st))}},
		})
	}
	return cs
}

func randaoDataCases() []Case {
	two64 := new(big.Int).Lsh(big.NewInt(1), 64)
	type in struct {
		name          string
		chain, number *big.Int
	}
	ins := []in{
		{"c8282_n0", big.NewInt(8282), big.NewInt(0)},
		{"c8282_n1", big.NewInt(8282), big.NewInt(1)},
		{"c8282_n2", big.NewInt(8282), big.NewInt(2)},
		{"c8282_n255", big.NewInt(8282), big.NewInt(255)},
		{"c8282_n256", big.NewInt(8282), big.NewInt(256)},
		{"c8283_n1", big.NewInt(8283), big.NewInt(1)},
		{"c1_n1", big.NewInt(1), big.NewInt(1)},
		{"c8282_n2pow64", big.NewInt(8282), two64},
		{"c8282_n2pow64_plus_1", big.NewInt(8282), new(big.Int).Add(two64, big.NewInt(1))},
	}
	var cs []Case
	for _, x := range ins {
		cs = append(cs, Case{
			Runner: "crypto", Handler: "randao_data", Name: x.name, Kind: "pure",
			Desc:     fmt.Sprintf("randao_data(chain_id=%s, number=%s) = keccak256(be_min(chain_id) || 0x01 || be_min(number))", x.chain, x.number),
			Reqs:     []string{"WBFT-CRYPTO-050"},
			Input:    M{{"chain_id", decBig(x.chain)}, {"number", decBig(x.number)}},
			Expected: M{{"randao_data", hexs(randaoData(x.chain, x.number))}},
		})
	}
	return cs
}

func randaoMixCases() []Case {
	reveal := ecdsaSign(fx.keys[0], randaoData(big.NewInt(8282), big.NewInt(1)))
	kr := common.BytesToHash(crypto.Keccak256(reveal))
	type in struct {
		name, desc string
		parent     common.Hash
		reveal     []byte
	}
	ins := []in{
		{"parent_zero", "parent mix 0x00..00, reveal of key 0 for (8282, 1) (A-02 §8.5)", common.Hash{}, reveal},
		{"parent_00ff", "parent mix 0x00ff..00ff", common.HexToHash("0x00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff"), reveal},
		{"parent_self", "parent mix = keccak256(reveal): result is 32 zero bytes (leading zeros kept)", kr, reveal},
		{"parent_self_last_bit", "parent mix = keccak256(reveal) with the last bit flipped: result 0x00..01", func() common.Hash { h := kr; h[31] ^= 0x01; return h }(), reveal},
		{"reveal_empty", "empty reveal (not a valid header; keccak256 of the empty string)", common.HexToHash("0x0101010101010101010101010101010101010101010101010101010101010101"), nil},
	}
	var cs []Case
	for _, x := range ins {
		cs = append(cs, Case{
			Runner: "crypto", Handler: "randao_mix", Name: x.name, Kind: "pure",
			Desc:     "randao_mix(parent_mix, reveal) = parent_mix XOR keccak256(reveal), 32 bytes: " + x.desc,
			Reqs:     []string{"WBFT-CRYPTO-053"},
			Input:    M{{"parent_mix", hexs(x.parent.Bytes())}, {"reveal", hexs(x.reveal)}},
			Expected: M{{"mix", hexs(wbftengine.CalculateRandaoMix(x.parent, x.reveal).Bytes())}},
		})
	}
	return cs
}
