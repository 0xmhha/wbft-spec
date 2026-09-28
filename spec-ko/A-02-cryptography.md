# A-02. 암호

- 상태: 초안이다.
- 영역: `CRYPTO`
- 레퍼런스 구현: go-stablenet `740526d03`. BLS 는 `github.com/supranational/blst v0.3.16` (`go.mod:63`) 으로 구현하고, secp256k1 은 cgo 빌드에서 함께 들어 있는 libsecp256k1 (`crypto/secp256k1`) 로 구현한다.

WBFT 는 세 가지 암호 기본 연산을 쓴다. Keccak-256 은 BLS 내부를 뺀 모든 hash 를 계산한다 (§2). secp256k1 ECDSA 는 node key 로 합의 메시지와 randao reveal 에 서명한다. BLS12-381 signature 는 node key 에서 유도한 키로 만들며, header 에 aggregate 되는 seal 을 이룬다. 이 장은 레퍼런스가 쓰는 방식 그대로 각 기본 연산을 정의하고, 서명 대상인 바이트 문자열 (`seal_data`, `randao_data`) 과 randao mix 를 정의하며, 레퍼런스 코드로 계산한 test vector 를 제시한다.

이 값들을 담는 인코딩 (메시지 필드, `WBFTExtra`) 은 `A-03` 에 있다. 어느 validator 집합으로 signature 를 검사하는지, 그리고 검사에 실패한 메시지나 block 을 어떻게 처리하는지는 `A-05` 와 `A-08` 에 있다.

> 해설: 두 서명 방식은 서로 다른 것을 증명한다. ECDSA signature 는 "이 주소가 이 메시지를 보냈다" 는 것을 증명하며, 공개 키 목록 없이 signature 에서 주소를 recover 할 수 있다. BLS seal 은 "이 validator 가 이 block 을 이 round 에 prepare 또는 commit 했다" 는 것을 증명한다. 한 block 에는 quorum 만큼의 서명이 필요한데, ECDSA signature 를 quorum 만큼 header 에 넣으면 header 크기가 quorum 에 비례해 커진다. BLS signature 는 더해서 96 바이트 하나로 만들 수 있고, 누가 서명했는지는 bitmap 으로 적는다. 그래서 PREPARE 와 COMMIT 은 두 서명을 모두 담는다. 메시지 전체에는 ECDSA signature 가 붙고, 메시지 안의 seal 은 BLS signature 다 (`A-03 §8.2`, `§8.3`).

---

## 1. 표기법

- `keccak256(x)` 는 32 바이트를 돌려준다 (§2).
- `n` 은 secp256k1 군의 위수다: `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141`.
- `r_bls` 는 BLS12-381 군의 위수다: `0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001` (10진수 값은 `crypto/bls/constants.go:11` 에 있다).
- `be_min(x)`, `‖`, `bytesN` 은 `A-01 §1` 에서 정의한다.

---

## 2. Keccak-256

[WBFT-CRYPTO-001] `keccak256(x)` 는 반드시 Keccak 패딩을 쓰는 원래의 Keccak-256 (Ethereum 이 쓰는 hash) 이어야 하며, FIPS-202 SHA3-256 이어서는 안 된다. `keccak256("")` 은 `0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470` 이다.
- Source: `crypto/crypto.go:85-93`, `consensus/wbft/utils.go:31-36` (sha3.NewLegacyKeccak256)
- Observable: header, network

이 명세의 hash 는 BLS 키 유도(§5.1)와 BLS hash-to-curve(§5.4) 안에서 쓰는 SHA-256 을 빼면 모두 `keccak256` 이다. 여기에는 block hash, `seal_data`, `randao_data`, 메시지 서명 digest, `A-03 §8.8` 의 dedup key, `A-04` 의 shuffle hash 가 모두 포함된다.

> 해설: 많은 라이브러리가 "sha3" 라는 이름으로 FIPS 버전을 제공한다. 구현을 시작할 때 `keccak256("")` 값으로 먼저 확인한다.

---

## 3. secp256k1 위의 ECDSA

### 3.1 키와 주소

[WBFT-CRYPTO-010] 노드의 신원은 반드시 node key 의 주소여야 한다. 주소는 `address = keccak256(X ‖ Y)[12:32]` 이며, 여기서 `X ‖ Y` 는 `0x04` 접두사를 뺀 64 바이트 비압축 public key 다.
- Source: `crypto/crypto.go:287-290` (PubkeyToAddress), `consensus/wbft/backend/backend.go:75`
- Observable: header, network

### 3.2 서명

```python
def ecdsa_sign(node_key, data: bytes) -> bytes65:
    """Hash-then-sign as done by Backend.Sign."""
    digest = keccak256(data)
    (r, s, recid) = secp256k1_sign_recoverable(node_key, digest)   # RFC 6979 nonce, low-S
    return be32(r) + be32(s) + bytes([recid])                    # recid 는 {0, 1} 가운데 하나다
```

[WBFT-CRYPTO-011] 노드는 이 프로토콜의 모든 ECDSA signature 를 반드시 `ecdsa_sign(node_key, data)` 로 만들어야 한다. 즉 signature 는 `keccak256(data)` 에 대한 서명이고, 출력은 65 바이트 `R ‖ S ‖ V` 이다. `R` 과 `S` 는 32 바이트 big-endian 정수이고, `V ∈ {0, 1}` 은 recovery id 다 (`27`/`28` 이 아니다).
- Source: `consensus/wbft/backend/backend.go:281-284` (Sign), `crypto/signature_cgo.go:53-60`, `crypto/secp256k1/secp256.go:62-91`
- Observable: network, header

[WBFT-CRYPTO-017] 노드는 레퍼런스처럼 결정적 (RFC 6979) ECDSA signature 를 만드는 것이 좋다 (SHOULD). 검증하는 노드는 nonce 를 확인할 수 없다.
- Source: `crypto/secp256k1/secp256.go:76-79` (secp256k1_nonce_function_rfc6979)
- Observable: network, header

> 해설: 결정적 서명은 권고 이상의 의미를 갖는다. ROUND-CHANGE 를 재전송할 때 바이트가 똑같아지는 것이 결정적 서명 덕분이며, `A-06` 의 재전송 동작이 이 성질에 기댄다.

ECDSA 로 서명하는 데이터는 다음과 같다.

| 대상 | `data` | 정의한 곳 |
|---|---|---|
| 합의 메시지 | `message_signing_payload(msg)` | `A-03 §8` |
| randao reveal | `randao_data(chain_id, number)` | §7 |

### 3.3 Recovery

```python
def ecdsa_recover_address(data: bytes, sig: bytes) -> Address | Error:
    """GetSignatureAddress (cgo build)."""
    if len(sig) != 65:                 return Error("invalid signature length")
    if sig[64] >= 4:                   return Error("invalid signature recovery id")
    r, s, v = int(sig[0:32]), int(sig[32:64]), sig[64]
    # libsecp256k1 compact parse: r 와 s 는 [1, n-1] 안에 있어야 하고, recovery 가 성공해야 한다
    Q = secp256k1_recover(keccak256(data), r, s, v)   # 실패하면 Error("recovery failed")
    return address(Q)
```

[WBFT-CRYPTO-013] 검증하는 노드는 `data` 에 대한 `sig` 의 서명자를 반드시 `ecdsa_recover_address(data, sig)` 로 recover 해야 한다. 검증하는 노드는 다음 가운데 하나에 해당하는 `sig` 를 반드시 거부해야 한다.
1. 길이가 65 가 아니다.
2. recovery 바이트가 `≥ 4` 이다.
3. `R` 이나 `S` 가 `0` 이거나 `≥ n` 이다.
4. public key recovery 가 실패한다.

그 밖의 경우에 서명자는 recover 한 키의 주소다.
- Source: `consensus/wbft/utils.go:39-48` (GetSignatureAddress), `crypto/signature_cgo.go:32-43`, `crypto/secp256k1/secp256.go:97-114`, `crypto/secp256k1/secp256.go:163-171`
- Observable: network, header

[WBFT-CRYPTO-014] 검증하는 노드는 `S` 가 `n/2` 보다 큰 signature (high-S) 라도 recovery 가 성공하면 반드시 받아들여야 한다. 레퍼런스는 이런 signature 를 정규화하지도 거부하지도 않는다. 그러므로 주어진 `(data, signer)` 에 대해 유효한 인코딩이 적어도 두 개, 즉 `(R, S, V)` 와 `(R, n − S, V ⊕ 1)` 이 있다.
- Source: `crypto/signature_cgo.go:32-43` (no call to ValidateSignatureValues on this path), `consensus/wbft/utils.go:39-48`
- Observable: network, header

Implementation note (informative): cgo 없이 빌드하면 (`crypto/signature_nocgo.go`, build tag `nacl || js || !cgo || gofuzz`) recovery 는 `decred/dcrd/dcrec/secp256k1/v4 v4.0.1` 을 거친다. 이 라이브러리는 recovery 바이트 검사에서 `0 … 7` 을 거부하지 않는다. 이 라이브러리가 `V + 27` 을 자신의 compact signature 범위 `27 … 34` 로 옮기고 (`crypto/signature_nocgo.go:42-53`), `V ∈ {4 … 7}` 을 `V − 4` 로 처리하기 때문이다. 그러므로 `V − 4` 가 복구되는 `V ∈ {4, 5}` 인 signature 는 cgo 없는 빌드에서는 받아들여지고 cgo 빌드에서는 거부된다. 규범 규칙은 cgo 빌드의 동작이다 ([WBFT-CRYPTO-013]).

### 3.4 validator signature 검사

```python
def check_validator_signature(validators: ValidatorSet, data: bytes, sig: bytes) -> Address | Error:
    signer = ecdsa_recover_address(data, sig)       # recovery 오류를 그대로 전달한다
    if signer in validators.addresses:
        return signer
    return Error(ErrUnauthorizedAddress)            # "unauthorized address"
```

[WBFT-CRYPTO-015] `check_validator_signature(V, data, sig)` 는 recover 한 서명자가 `V` 의 구성원이면 반드시 그 서명자를 돌려주어야 한다. recovery 가 실패하면 반드시 recovery 오류로 실패해야 한다. 그 밖의 경우에는 반드시 `ErrUnauthorizedAddress` 로 실패해야 한다.
- Source: `consensus/wbft/utils.go:59-73` (CheckValidatorSignature)
- Observable: network

노드가 메시지를 어느 validator 집합으로 검사하는지는 `A-05` 에서 정한다 (`consensus/wbft/core/core.go:452-462`). 노드는 현재 validator 집합을 쓰며, 한 경우에만 이전 높이의 집합을 쓴다.

[WBFT-CRYPTO-016] 노드가 signature 를 주어진 주소가 만들었는지 검사할 때는 반드시 `ecdsa_recover_address(data, sig) == address` 로 검사해야 한다. 이 검사는 §7.2 의 randao reveal 에 쓴다. 두 값이 다르면 검사는 `ErrInvalidSignature` ("invalid signature") 로 실패한다.
- Source: `consensus/wbft/backend/backend.go:292-303` (CheckSignature)
- Observable: header

---

## 4. 서명 데이터의 domain 분리

이 절은 참고용이다. 이 프로토콜은 네 종류의 바이트 문자열에 서명한다. 네 종류는 서로 혼동될 수 없다.

| 서명하는 문자열 | 키 | 사전 hash | 첫 바이트 / 길이 |
|---|---|---|---|
| 메시지 signing payload | node key (ECDSA) | `keccak256` | 첫 원소가 메시지 코드 `0x12 … 0x15` 인 RLP list (`A-03 §8`) |
| randao 데이터 | node key (ECDSA) | `keccak256` | 정확히 32 바이트 (그 자체가 hash 다) |
| seal 데이터 | BLS 키 | 없다 (`BLS_DST` 로 hash-to-curve 한다) | 정확히 32 바이트 |
| proof of possession (`GovValidator` 가 쓴다. precompile 은 `B-04` §12.3 에, 등록 절차는 `B-05` 에 있다) | BLS 키 | 없다 (`BLS_DST` 로 hash-to-curve 한다) | 정확히 48 바이트 (public key 자체) |

메시지 signing payload 도 길이가 32 바이트일 수 있다 (예: sequence 와 round 가 각각 13 바이트인 ROUND-CHANGE, `df 15 dd …`). 그러므로 두 ECDSA 용도를 가르는 것은 길이만이 아니다. `randao_data` 값은 `keccak256` 출력이어서 메시지 signing payload 와 같아질 확률이 무시할 만큼 작다는 점이 두 용도를 가른다.

ECDSA signing payload 에는 chain id 가 들어 있지 않다. 메시지는 자신이 담은 digest 와 높이를 통해서만 chain 에 묶인다.

---

## 5. BLS12-381

이 방식은 IETF BLS signature 방식 (draft-irtf-cfrg-bls-signature) 의 "minimal-pubkey-size" 변형이다. public key 는 G1 에 있고 signature 는 G2 에 있다. ciphersuite 는 proof-of-possession ciphersuite `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` 이며, Ethereum consensus layer 와 같은 ciphersuite 다.

### 5.1 node key 로부터 키 유도

```python
def derive_bls_key(node_key) -> int:
    """bls.DeriveFromECDSA -> blst.KeyGen (IETF KeyGen, draft >= 04; identical to EIP-2333 derive_master_SK)."""
    ikm  = be32(node_key.d)                  # crypto.FromECDSA: 32 바이트 big-endian 비밀 scalar
    salt = b"BLS-SIG-KEYGEN-SALT-"
    while True:
        salt = sha256(salt)
        prk  = hkdf_extract(salt, ikm + b"\x00")                 # HMAC-SHA256
        okm  = hkdf_expand(prk, b"" + (48).to_bytes(2, "big"), 48)  # key_info = "" 
        sk   = int.from_bytes(okm, "big") % r_bls
        if sk != 0:
            return sk
```

[WBFT-CRYPTO-020] 노드의 BLS secret key 는 반드시 위에서 정의한 `derive_bls_key(node_key)` 여야 한다. IKM 은 32 바이트 big-endian node 비밀 키다. salt 는 `SHA-256("BLS-SIG-KEYGEN-SALT-")` 에서 시작하며, 결과가 0 이면 salt 를 다시 hash 한다. `key_info` 는 비어 있고, `L = 48` 이다.
- Source: `crypto/bls/bls.go:89-93` (DeriveFromECDSA), `crypto/crypto.go:168-174` (FromECDSA), `crypto/bls/blst/secret_key.go:36-46` (GenerateKey), blst `bindings/go/blst.go:243-258` (KeyGen), blst `src/keygen.c:135-216`
- Observable: header, network

[WBFT-CRYPTO-021] validator 의 BLS public key 는 반드시 `sk · G1` 을 48 바이트 압축 형식 (§5.2) 으로 인코딩한 값이어야 한다. validator 에 등록된 키 (genesis `init.blsPublicKeys`, `GovValidator` state, `EpochInfo.bls_public_keys`) 는 [WBFT-CRYPTO-020] 에 따라 그 validator 의 node key 에서 유도한 public key 와 같다고 가정한다. 유도한 키가 등록된 키와 다른 노드는 verify 를 통과하는 seal 을 만들 수 없다.
- Source: `crypto/bls/blst/secret_key.go:64-67`, `consensus/wbft/core/core.go:471-488`
- Observable: header

Implementation note (informative): 이 유도 과정은 blst 와 독립인 Python HKDF 구현으로 §8.1 의 키에 대해 다시 계산해 보았고, 결과가 일치했다.

### 5.2 인코딩

[WBFT-CRYPTO-022] BLS 값은 반드시 다음과 같이 인코딩해야 한다. secret key 는 32 바이트 big-endian 이다. public key 는 48 바이트 압축 G1 점이다. signature 는 96 바이트 압축 G2 점이다. 압축은 ZCash BLS12-381 직렬화를 따른다. 이 직렬화에서 첫 바이트의 최상위 세 비트는 차례로 압축 flag, 무한원점 flag, 부호 flag 다. 무한원점은 `0xc0` 뒤에 0 바이트가 이어지는 값이다.
- Source: `crypto/bls/blst/secret_key.go:92-96`, `crypto/bls/blst/public_key.go:81-84`, `crypto/bls/blst/signature.go:277-280`, `crypto/bls/common/constants.go:14-21`
- Observable: header, network

### 5.3 디코딩과 유효성 검사

```python
def bls_public_key_from_bytes(b: bytes) -> G1 | Error:
    if len(b) != 48:                  return Error("public key must be 48 bytes")
    P = g1_uncompress(b)              # flag, x < p, 곡선 위의 점인지 검사한다
    if P is None:                     return Error("could not unmarshal bytes into public key")
    if P.is_infinity() or not P.in_subgroup():
        return Error(ErrInfinitePubKey)   # "received an infinite public key" (subgroup 검사 실패에도 쓴다)
    return P

def bls_signature_from_bytes(b: bytes) -> G2 | Error:
    if len(b) != 96:                  return Error("could not create signature from byte slice: signature must be 96 bytes")
    S = g2_uncompress(b)
    if S is None:                     return Error("could not create signature from byte slice: could not unmarshal bytes into signature")
    if not S.in_subgroup():           return Error("signature not in group")
    return S                          # 여기서는 무한원점을 받아들인다
```

[WBFT-CRYPTO-023] 노드는 다음 가운데 하나에 해당하는 public key 를 반드시 거부해야 한다.
1. 길이가 48 이 아니다.
2. 압축을 풀었을 때 곡선 위의 점이 되지 않는다.
3. 무한원점이다.
4. 소수 위수 subgroup 에 속하지 않는다.
- Source: `crypto/bls/blst/public_key.go:29-58` (publicKeyFromBytes, KeyValidate)
- Observable: header

[WBFT-CRYPTO-024] 노드는 다음 가운데 하나에 해당하는 signature 를 반드시 거부해야 한다.
1. 길이가 96 이 아니다.
2. 압축을 풀었을 때 곡선 위의 점이 되지 않는다.
3. 소수 위수 subgroup 에 속하지 않는다.

노드는 무한원점 signature 를 디코딩 단계에서 거부해서는 안 된다. 무한원점 signature 는 디코딩 대신 verify 단계에서 실패한다 ([WBFT-CRYPTO-026]).
- Source: `crypto/bls/blst/signature.go:32-43` (signatureFromBytesNoValidation), `crypto/bls/blst/signature.go:55-67` (SignatureFromBytes, SigValidate(false))
- Observable: header, network

> 해설: 무한원점 처리는 public key 와 signature 사이에서 비대칭이다. public key 가 무한원점이면 디코딩에서 거부된다. signature 가 무한원점이면 디코딩은 통과하고 verify 에서 false 가 된다. BLS 라이브러리를 바꿔 구현할 때 이 경계에서 결과가 달라지기 쉬우므로, §8.3 의 vector 로 먼저 맞춘다.

[WBFT-CRYPTO-056] 노드는 public key (48 바이트, G1) 나 signature (96 바이트, G2) 의 압축을 풀 때 다음 가운데 하나에 해당하는 입력을 반드시 거부해야 한다.
1. 압축 flag (byte 0 의 bit 7) 가 0 이다.
2. 무한원점 flag (byte 0 의 bit 6) 가 1 인데, 부호 flag (byte 0 의 bit 5) 나 입력의 다른 비트 가운데 하나가 1 이다.
3. 무한원점 flag 가 0 인데, 좌표 성분 하나가 field modulus `p` 보다 작지 않다. G1 에서 좌표 성분은 flag 세 비트를 지운 48 바이트의 값이다. G2 에서 좌표 성분은 두 개다. 하나는 `x.c1` 을 담은 앞 48 바이트에서 flag 세 비트를 지운 값이고, 다른 하나는 `x.c0` 을 담은 뒤 48 바이트의 값이다.
4. 곡선 위에 그 `x` 를 가진 점이 없다.

부호 flag 는 같은 `x` 를 가진 두 점 가운데 하나를 고른다. 그래서 유효한 인코딩에서 부호 flag 를 뒤집으면 음의 점의 인코딩이 되고, 이 인코딩은 디코딩된다. 따라서 모든 점은 받아들여지는 인코딩을 정확히 하나 가진다.
- Source: `crypto/bls/blst/public_key.go:43-46` (Uncompress), `crypto/bls/blst/signature.go:32-43` (Uncompress), `go.mod:63` (blst v0.3.16), checked by execution in §8.3
- Observable: header, network

> 해설: ZCash 직렬화 문서를 정확히 따르면 결과는 같다. 그러나 flag 검사를 느슨하게 하거나 `x ≥ p` 를 mod `p` 로 줄이는 라이브러리를 쓰면, 같은 점을 뜻하는 다른 바이트열을 받아들이게 된다. `prev_prepared_seal` 과 `prev_committed_seal` 은 block hash 에 들어가므로, 그런 구현은 레퍼런스가 거부하는 header 를 받아들인다.

### 5.4 서명

[WBFT-CRYPTO-025] `bls_sign(sk, msg)` 는 반드시 `sk · hash_to_G2(msg, BLS_DST)` 여야 한다. 여기서 `hash_to_G2` 는 suite `BLS12381G2_XMD:SHA-256_SSWU_RO_` 에 대한 RFC 9380 의 `hash_to_curve` 이며, `msg` 자체에 적용한다. 즉 `msg` 를 미리 hash 하지 않고, augmentation 도 하지 않는다. WBFT 에서 `msg` 는 언제나 32 바이트 `seal_data` 다 (§6).
- Source: `consensus/wbft/backend/backend.go:286-289` (SignWithoutHashing), `crypto/bls/blst/secret_key.go:87-90`, `crypto/bls/blst/signature.go:22`
- Observable: network, header

### 5.5 signature 하나의 검증

[WBFT-CRYPTO-026] `bls_verify(pk, msg, sig)` 는 `e(pk, hash_to_G2(msg, BLS_DST)) = e(G1, sig)` 일 때, 그리고 그때에만 반드시 true 를 돌려주어야 한다. 이때 `pk` 와 `sig` 는 [WBFT-CRYPTO-023] 과 [WBFT-CRYPTO-024] 를 통과한 값이다. `pk` 가 유효하고 무한원점이 아니면, 무한원점과 같은 signature 는 반드시 false 로 verify 되어야 한다.
- Source: `crypto/bls/blst/signature.go:119-122` (Verify), computed check in §8.3
- Observable: network, header

[WBFT-CRYPTO-055] `bls_verify(pk, msg, sig)` 는 `pk` 가 무한원점이면 `msg` 와 `sig` 가 무엇이든 반드시 false 를 돌려주어야 한다. [WBFT-CRYPTO-023] 을 통과한 키는 무한원점이 아니다. 그러나 aggregate key ([WBFT-CRYPTO-028]) 는 고른 키들의 합이 0 이면 무한원점이 된다. 이 경우 노드는 aggregated seal 의 signature 도 무한원점이어서 [WBFT-CRYPTO-026] 의 pairing 등식이 자명하게 성립하더라도, 그 aggregated seal 을 반드시 `ErrInvalidSeal` 로 거부해야 한다. 이 규칙은 IETF `CoreVerify` 의 `KeyValidate` 단계를 aggregate key 에 적용한 것이다. blst 는 pairing 을 계산하기 전에 `BLST_PK_IS_INFINITY` 를 돌려준다.
- Source: `consensus/wbft/engine/engine.go:1355-1366` (verifyAggregatedSeal), `crypto/bls/blst/public_key.go:61-79` (AggregatePublicKeys: no check on the sum), `crypto/bls/blst/signature.go:119-122` (Verify), blst v0.3.16 `src/aggregate.c:294-297`, checked by execution in §8.3
- Observable: header

> 해설: 합이 0 인 키 집합을 만들려면 그 키들의 비밀키를 모두 알아야 한다. 등록되는 키는 proof of possession 을 통과해야 하기 때문이다. bitmap 은 quorum 이상의 index 를 가리켜야 하므로, 이 경우는 quorum 크기 이상의 validator key 를 한 주체가 쥐었을 때에만 생긴다. 그 주체는 서명 없이 무한원점 하나로 어떤 header 에도 seal 을 만들 수 있다. pairing 등식만 구현한 노드는 그 seal 을 받아들이고 레퍼런스는 거부하므로, 두 노드의 chain 이 갈라진다.

### 5.6 Aggregation

```python
def bls_aggregate(sigs: list[bytes96]) -> bytes96 | Error:
    """AggregateCompressedSignatures(sigs, groupcheck=True)."""
    acc = G2_INFINITY
    for b in sigs:
        S = g2_uncompress(b)
        if S is None or not S.in_subgroup():
            return Error("provided signatures fail the group check and cannot be compressed")
        acc = acc + S
    return g2_compress(acc)          # 빈 목록 -> 0xc0 || 0x00 * 95
```

[WBFT-CRYPTO-027] `bls_aggregate(sigs)` 는 반드시 압축을 풀고 subgroup 검사를 통과한 signature 들의 합을 압축한 값이어야 한다. 입력 가운데 하나라도 압축 풀기나 subgroup 검사에 실패하면 `bls_aggregate` 는 반드시 실패해야 한다. 빈 목록을 aggregate 한 값은 무한원점이다. `bls_aggregate` 는 무한원점과 같은 입력을 받아들인다.
- Source: `crypto/bls/blst/signature.go:69-77`, computed checks in §8.3
- Observable: header

[WBFT-CRYPTO-028] public key 는 반드시 [WBFT-CRYPTO-023] 으로 각각 검사한 키들의 합으로 aggregate 해야 한다. 빈 목록을 aggregate 하면 실패한다.
- Source: `crypto/bls/blst/public_key.go:60-79` (AggregatePublicKeys)
- Observable: header

### 5.7 aggregated seal 의 검증

```python
def verify_aggregated_seal(V: ValidatorSet, header, round: uint32,
                           agg: AggregatedSeal, seal_type: SealType) -> None | Error:
    """verifyAggregatedSeal: FastAggregateVerify over the validators named by the bitmap."""
    idx = sealer_indices(agg.sealers)              # A-03 §4.4, 오름차순
    if len(idx) < quorum_size(len(V)):             # A-04
        return Error("lack of seal count")
    pks = []
    for i in idx:
        if i >= len(V):
            return Error("sealer is not validator")
        pks.append(V[i].bls_public_key)
    msg = seal_data(header, round, seal_type)      # §6
    apk = aggregate_public_keys(pks)               # WBFT-CRYPTO-028 (오류를 그대로 전달한다)
    sig = bls_signature_from_bytes(agg.signature)  # WBFT-CRYPTO-024 (오류를 그대로 전달한다)
    if not bls_verify(apk, msg, sig):              # apk 가 무한원점이면 false 다 (WBFT-CRYPTO-055)
        return Error(ErrInvalidSeal)                # "invalid seal"
    return None
```

[WBFT-CRYPTO-030] 노드는 aggregated seal 을 검증할 때 반드시 `verify_aggregated_seal` 의 검사를 위에 적힌 순서대로 수행하고, 처음 실패한 검사에서 실패해야 한다. 호출하는 쪽 (`A-08`) 은 어떤 실패든 필드에 맞는 오류 (`ErrInvalidPreparedSeals`, `ErrInvalidCommittedSeals`, `ErrInvalidPrevPreparedSeals`, `ErrInvalidPrevCommittedSeals`) 로 바꾼다.
- Source: `consensus/wbft/engine/engine.go:1338-1369` (verifyAggregatedSeal), `consensus/wbft/engine/engine.go:384-450`
- Observable: header

[WBFT-CRYPTO-031] sealer index 하나는 bitmap 의 비트 하나이므로, validator 하나는 aggregated seal 에 많아야 한 번 기여할 수 있다. aggregate 를 만드는 노드는 같은 sealer index 의 seal 두 개를 한 aggregate 에 넣어서는 안 된다. 레퍼런스는 그런 경우 비트는 한 번만 켜지만 signature 는 두 번 더하므로, 결과 aggregate 가 verify 에 실패한다.
- Source: `consensus/wbft/engine/engine.go:130-151` (aggregateSeal), `core/types/istanbul.go:292-298` (SetSealer is idempotent)
- Observable: header

> 해설: 그러므로 구현은 seal 을 aggregate 하기 전에 sealer index 별로 중복을 없애야 한다.

Implementation note (informative): aggregated seal 검사는 IETF `FastAggregateVerify` 를 "public key 를 aggregate 한 뒤 `Verify` 한다" 는 형태로 표현한 것이다. rogue-key 공격에 대한 저항은 등록된 모든 키가 proof of possession 을 통과했다는 사실에 기댄다 (§5.8). 레퍼런스는 디코딩한 public key 를 항목 2,000,000 개짜리 LRU 에 cache 한다 (`crypto/bls/blst/public_key.go:16-58`).

### 5.8 Proof of possession (참고)

`GovValidator` contract 는 주소 `0x0000000000000000000000000000000000B00001` 의 precompile 을 통해 등록되는 BLS 키를 검사한다. precompile 은 정확히 144 바이트인 입력 `pubkey(48) ‖ signature(96)` 을 요구하며, 두 값을 §5.3 에 따라 디코딩한다. 길이가 틀리거나 두 값 가운데 하나라도 디코딩에 실패하면 precompile 은 실패하고 (`invalid input length`, `invalid BLS public key`, `invalid BLS signature`), `GovValidator` 는 `FailedToVerifyBlsKey` 로 revert 한다. 그렇지 않으면 precompile 은 `bls_verify(pk, pubkey_bytes, sig)` 가 true 일 때, 그리고 그때에만 마지막 바이트가 `1` 인 32 바이트를 돌려준다. 즉 proof of possession 은 48 바이트 압축 public key 에 대해 `BLS_DST` 로 만든 일반 signature 이며, IETF `PopProve` tag `BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` 를 쓰지 않는다. 자세한 내용은 `B-04` §12.3 (precompile) 과 `B-05` (등록) 에 있다.
- Source: `core/vm/contracts.go:1189-1221` (blsPoP), `params/protocol_params.go:208`, `systemcontracts/solidity/v1/GovValidator.sol:157-171`

---

## 6. Seal 데이터

```python
def seal_data(header: Header, round: uint32, seal_type: SealType) -> bytes32:
    """core.PrepareSeal."""
    h = hash_with_round(header, round)             # A-03 §6.2 (WBFTHashWithRoundNumber)
    return keccak256(h + bytes([seal_type]))       # 33 바이트 preimage
```

[WBFT-CRYPTO-040] `seal_data(header, round, seal_type)` 는 반드시 33 바이트 preimage 에 대한 `keccak256(hash_with_round(header, round) ‖ seal_type)` 이어야 한다. 여기서 `hash_with_round` 는 `A-03 §6.2` 에서 정의하고, `round` 는 `A-01 [WBFT-TYPE-012]` 의 `uint32` 다.
- Source: `consensus/wbft/core/core.go:464-469` (PrepareSeal), `core/types/block.go:169-172` (WBFTHashWithRoundNumber)
- Observable: header, network

[WBFT-CRYPTO-041] `seal_data` 는 반드시 현재 block 의 seal 을 뺀 header 로 계산해야 한다. `hash_with_round` 는 `prepared_seal` 과 `committed_seal` 을 빼고 `round` 를 바꾸며, 그 밖의 모든 header 필드와 그 밖의 모든 `WBFTExtra` 필드 (`vanity_data`, `randao_reveal`, `prev_round`, `prev_prepared_seal`, `prev_committed_seal`, `gas_tip`, `epoch_info`) 는 그대로 둔다. `header.Difficulty = 1` 이고 extra 가 디코딩되면, `round = 0` 일 때 `hash_with_round(header, 0) = block_hash(header)` 이다. 따라서 `seal_data(header, 0, t) = keccak256(block_hash(header) ‖ t)` 이다.
- Source: `core/types/istanbul.go:269-288`, `core/types/block.go:121-131`
- Observable: header, network

> 해설: 기억할 점은 세 가지다. 첫째, 현재 block 의 seal 두 개는 서명 대상에서 빠진다. signature 가 자기 자신을 포함할 수는 없기 때문이다. 둘째, round 는 서명 대상에 들어간다. 그래서 같은 block 이라도 round 1 에서 만든 seal 과 round 2 에서 만든 seal 은 다르다. 셋째, `seal_type` 이 서명 대상에 들어가므로 prepare seal 을 commit seal 로 쓸 수 없다.

[WBFT-CRYPTO-042] seal 은 반드시 `bls_sign(sk, seal_data(header, round, seal_type))` 여야 한다. PREPARE 의 prepare seal 은 `seal_type = PREPARE_SEAL` 을 쓰고, COMMIT 의 commit seal 은 `COMMIT_SEAL` 을 쓴다. 두 경우 모두 `header` 는 proposal 의 header 이고, `round` 는 메시지의 round 를 2^32 로 나눈 나머지다.
- Source: `consensus/wbft/core/prepare.go:47`, `consensus/wbft/core/commit.go:49`
- Observable: network

[WBFT-CRYPTO-043] 노드는 validator `a` 에게서 받은 개별 seal 을 반드시 다음 단계로 검사해야 한다.
1. validator 집합에서 `a` 의 BLS 키를 찾는다.
2. [WBFT-CRYPTO-023] 에 따라 키를 검사하고, 실패하면 그 오류를 그대로 전달한다.
3. [WBFT-CRYPTO-024] 에 따라 seal 을 검사하고, 실패하면 `errInvalidSeal` 로 거부한다.
4. `bls_verify(pk_a, seal_data(...), seal)` 을 검사하고, false 이면 `errInvalidSigner` 로 거부한다.
- Source: `consensus/wbft/core/core.go:471-488` (verifySeal)
- Observable: network

[WBFT-CRYPTO-044] header 의 extra 가 디코딩되지 않으면, 레퍼런스에서 `hash_with_round(header, round)` 는 그런 header 모두에 대해 `keccak256(0xc0)` 이 된다. 그래서 `seal_data` 는 상수 `keccak256(keccak256(0xc0) ‖ seal_type)` 이 된다. 노드는 extra 가 디코딩되지 않는 header 에 대해 seal 을 서명해서는 안 된다.
- Source: `core/types/istanbul.go:269-275` (returns nil), `core/types/block.go:169-172` (rlpHash of a nil header), computed in §8.4
- Observable: network

---

## 7. Randao

### 7.1 Randao 데이터

```python
def randao_data(chain_id: bigint, number: bigint) -> bytes32:
    return keccak256(be_min(chain_id) + be_min(RANDAO_VERSION) + be_min(number))   # be_min(1) = 0x01
```

[WBFT-CRYPTO-050] `randao_data(chain_id, number)` 는 반드시 `keccak256(be_min(chain_id) ‖ 0x01 ‖ be_min(number))` 여야 한다. `chain_id` 는 설정된 chain id 이고, `number` 는 header 번호다. 구성 요소는 길이 접두사 없이 이어 붙인다. `be_min(0)` 은 빈 문자열이다.
- Source: `consensus/wbft/engine/engine.go:563-573` (makeRandaoData)
- Observable: header

> 해설: 그래서 block 0 의 preimage 는 chain 8282 와 버전 바이트로 이루어진 `205a01` 뿐이다 (§8.5). 구현이 흔히 틀리는 경계이므로 vector 로 확인한다.

### 7.2 Randao reveal

### 7.3 Randao mix

```python
def randao_mix(parent_mix: bytes32, reveal: bytes) -> bytes32:
    x = int.from_bytes(parent_mix, "big") ^ int.from_bytes(keccak256(reveal), "big")
    return x.to_bytes(32, "big")        # 왼쪽을 0 으로 채운다: 두 32 바이트 값의 바이트 단위 XOR 과 같다
```

[WBFT-CRYPTO-053] `randao_mix(parent_mix, reveal)` 는 반드시 `parent_mix` 와 `keccak256(reveal)` 의 바이트 단위 XOR 이어야 하며, 32 바이트로 돌려주어야 한다. 결과 앞쪽의 0 바이트는 반드시 유지해야 한다. 레퍼런스는 big integer 를 거쳐 변환한 뒤 왼쪽을 0 으로 채워 32 바이트로 만들며, 이 방법은 바이트 단위 XOR 과 같은 결과를 낸다.
- Source: `consensus/wbft/engine/engine.go:575-581` (CalculateRandaoMix)
- Observable: header

epoch block 의 randao mix 는 다음 epoch 의 validator 순서를 정하는 shuffle 의 seed 다 (`A-04`, `consensus/wbft/engine/engine.go:1265-1277`). 또한 모든 block 의 mix 는 그 block 안에서 EVM opcode `PREVRANDAO` (`0x44`) 가 돌려주는 값이다 (`core/evm.go:61-64`, `core/vm/jump_table.go:117-124`). 이 opcode 의 동작은 Part B 에서 정의한다.

---

## 8. Test vector

이 절의 vector 는 모두 레퍼런스 코드를 실행해서 계산했다. 계산에는 별도 module 의 Go 프로그램을 썼다. 이 프로그램은 `github.com/ethereum/go-ethereum` 을 import 하고 `replace` 지시어로 go-stablenet `740526d03` 을 가리키게 한 뒤, 각 표에 적힌 함수 (`crypto.Sign`, `bls.DeriveFromECDSA`, `core.PrepareSeal`, `engine.CalculateRandaoMix`, `Engine.CommitHeader`, …) 를 호출한다. keccak 만 쓰는 행, §8.1 의 키 유도, round 0 의 `seal_data` 는 Python (pycryptodome Keccak, hashlib HMAC) 으로 다시 계산했고 결과가 일치했다. 생성 프로그램은 이 명세에 포함되지 않는다. 초안 작성 도구 `tools/vectors-a01-a03` 이 그 프로그램의 출발점이다 (`A-11` §3.2).

### 8.1 키

node key 는 `key_i = keccak256(ASCII("wbft-spec-vector-key-" ‖ decimal(i)))` 이다.

| i | node 비밀 키 | 주소 | BLS secret key (`derive_bls_key`) | BLS public key |
|---|---|---|---|---|
| 0 | `b96c8bb76f8818a11de0403b189877f6524f4392a3faa9660de7a219670ef59d` | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` | `35de7bc5d48c484e7b87e7b96ebf61d15ae225d6dd3fea0663c9291b5ac10f5a` | `8eaaca1bbb29c07cb653b346e8434bb6b3bc63a4bb7c8dfea5c46bae2473646fe8041e3ff81da70bba7effda935a4576` |
| 1 | `385a3623913dc8f0191c5e3779605215dcce5b25342ede6afdc8d8e8d172de39` | `0x1111F6010b9b757b30e00A7D519cd98094b7A610` | `488c292335a8c97b4c228b4ba2d3e43c875ad2445e3331ff173b8630b47d65bc` | `8a8f913a1bac306b3b1661fc78ce376a650341ce314f7c0f615fc7abf3722eb6c256046483c266d3e03ce82edbed13a3` |
| 2 | `991019e805bf05d3ab0e738b4e7d695c16ae4a9bb56967dfca66611887a05a09` | `0x503578B72bf5c96AB41508E2AC70303691dfcdDb` | `3d57e6be7c50f146643b268d185c00c1bf67708a590c8662ce976b14ee16fc64` | `976a4cd50f07ad6fb3fc0a7a27a803212be7cf84b779d9a5562af81316187dec8ee319a0ce421c020dc1bf1faa494abe` |
| 3 | `07450e791971fe429ceabb5a10a8f826127c4981190057413efaf9b07809f681` | `0x084AC57c61cd5Dd6244eB0b4c42cA72d073A6c14` | `4dbb674eac4df7166edbdca4ef7a7f96a3e73be6935164e276a57a48360f8a47` | `aa6b0cda839b65a5e2e556484ccb3d67fac59c1cc66ab2bf1b9160ecee30a0deb6dbbad04836ca99ceb74c225a0c184e` |

### 8.2 ECDSA

| 경우 | 입력 | 출력 |
|---|---|---|
| `keccak256("wbft")` | ASCII `wbft` | `154d94908e42308ff21897b1445bd5001dedb9fa41095ad342cb656fffe2c55f` |
| `ecdsa_sign(key0, "wbft")` | — | `20a777b74c9e9838257309f91fd348d53485fb0bedbdcabf4a41faeb17b8ce35 65182fbdf4d2e6a7f69491e35053b331f003523e36f6acddf45b22bbedb3757c 00` (R ‖ S ‖ V) |
| recover | 위의 signature | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` |
| high-S 짝: `S' = n − S`, `V' = V ⊕ 1` | `…b8ce35 9ae7d0420b2d1958096b6e1cafac4ccccaab8aa87851f35dcb773bd0e282cbc5 01` | 같은 주소를 recover 한다 (받아들인다, [WBFT-CRYPTO-014]) |
| `V = 27` | 같은 R, S | 오류 `invalid signature recovery id` |
| `V = 2` | 같은 R, S | 오류 `recovery failed` |
| 64 바이트 signature | 앞의 64 바이트 | 오류 `invalid signature length` |

### 8.3 BLS

| 경우 | 입력 | 출력 |
|---|---|---|
| `bls_sign(sk0, m)` | `m = keccak256("wbft-bls") = d95400c38384460a9d671ec5ead86860b0dd0955756275a96b01266a72df305b` | `99578d7f3a838333dfeeb2a7ade9fa0e150e913fb852dac9606c06099b929079e2a0a89ed229fe40eb23d82c9511be2b161297f1a426a8fc5dcedd81f20783288897330bf58ad2e94098f6bc63a638547b72f35fb906e273f84f48e77c59066f` |
| `bls_verify(pk0, m, ·)` | 위의 signature | true |
| public key = 무한원점 | `c0` ‖ 47 × `00` | 거부된다: `received an infinite public key` |
| public key = 0 바이트 48 개 | — | 거부된다: `could not unmarshal bytes into public key` |
| 47 바이트 public key | — | 거부된다: `public key must be 48 bytes` |
| signature = 무한원점 | `c0` ‖ 95 × `00` | 디코딩된다. `bls_verify(pk0, m, ·)` = false |
| signature = 0 바이트 96 개 | — | 디코딩에서 거부된다 |
| `bls_aggregate([])` | — | `c0` ‖ 95 × `00` (오류가 없다) |
| `bls_aggregate([infinity])` | — | 오류가 없다 |
| signature = 부호 flag 가 켜진 무한원점 | `e0` ‖ 95 × `00` | 디코딩에서 거부된다 ([WBFT-CRYPTO-056]) |
| signature = 0 이 아닌 비트가 있는 무한원점 | `c0` ‖ 94 × `00` ‖ `01` | 디코딩에서 거부된다 |
| 압축 flag 없이 무한원점 flag 만 켠 값 | `40` ‖ 95 × `00` | 디코딩에서 거부된다 |
| 유효한 signature 나 public key 에서 압축 flag 를 끈 값 | — | 디코딩에서 거부된다 |
| 유효한 public key 의 `x` 에 `p` 를 더한 값 (381 비트 안에 들어간다) | — | 거부된다: `could not unmarshal bytes into public key` |
| 유효한 signature 의 앞 절반 (`x.c1`) 이나 뒤 절반 (`x.c0`) 에 `p` 를 더한 값 | — | 디코딩에서 거부된다 (두 절반을 따로 검사한다) |
| 유효한 signature 나 public key 에서 부호 flag 를 뒤집은 값 | — | 디코딩된다 (음의 점이다). 원래 메시지에 대한 `bls_verify` = false |
| secret key `a`, `b`, `(−a−b) mod r` 의 public key 를 aggregate 한 값 (`a`, `b` 는 아무 값) | — | `c0` ‖ 47 × `00` (오류가 없다) |
| 그 세 키로 같은 메시지에 서명한 signature 를 aggregate 한 값 | — | `c0` ‖ 95 × `00`. 위의 aggregate key 로 `bls_verify` 하면 false 다 ([WBFT-CRYPTO-055]) |

### 8.4 Seal 데이터

header `H` 는 chain 8282, block 2, proposer key 0 으로 만든다. `H` 는 hash 계산만을 위한 vector 이며 유효한 chain block 이 아니다. parent hash, state root, mix digest 에는 자리 채움 값을 넣었다.

| 필드 | 값 |
|---|---|
| ParentHash | `0x1111…11` (32 × `11`) |
| UncleHash | `EMPTY_UNCLE_HASH` |
| Coinbase | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` |
| Root | `0x2222…22` |
| TxHash, ReceiptHash | `0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421` |
| Bloom | 0 바이트 256 개 |
| Difficulty | 1 |
| Number | 2 |
| GasLimit | 105000000 |
| GasUsed | 0 |
| Time | 1700000002 |
| Extra | `WBFTExtra{vanity = 32 × 00, randao_reveal = ecdsa_sign(key0, randao_data(8282, 2)), prev_round = 0, prev seals = nil, round = 0, seals = nil, gas_tip = 27600000000000, epoch_info = nil}` (바이트는 `A-03 §9.3` 에 있다) |
| MixDigest | `0x00…00` |
| Nonce | `0x0000000000000000` |
| BaseFee | 20000000000000 |

| 값 | 결과 |
|---|---|
| `block_hash(H)` = `hash_with_round(H, 0)` | `6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e` |
| `hash_with_round(H, 1)` | `12fbf50df755a39e84a982f2cee1ad16ca800ee1e7a74f980cc7948b6d515752` |
| `hash_with_round(H, 2)` | `9493c5c1781d9d10df8b95545bc4272ad33a6c30b0b5319eade35b513dcf2d4e` |
| `seal_data(H, 0, PREPARE_SEAL)` | `9634606e7f65a734cd97a24d8a6458383e38f5e5c9b679864b15d0db09b94391` |
| `seal_data(H, 0, COMMIT_SEAL)` | `087eccbdf4ac7846d8255e2d0ef5e6e5f0b344fef86c625e12b9ba123eb3ee50` |
| `seal_data(H, 2, PREPARE_SEAL)` | `5e2e30b10f096d24641da9829661c6df27738aab23b7071206bcb845c2691152` |
| `seal_data(H, 2, COMMIT_SEAL)` | `364771f56f2ddcf3f80b9f9fdc5085a3d3f8cbc23c80578e5be1543c2aab2e17` |
| extra 를 디코딩할 수 없는 모든 `X` 에 대한 `seal_data(X, 0, PREPARE_SEAL)` ([WBFT-CRYPTO-044]) | `f3aac67c5fc8b34464e4fe705b4570279ead9d482adf0d2c875a970920027699` = `keccak256(keccak256(0xc0) ‖ 0x00)` |

다음 표는 validator 0, 1, 2 가 round 0 에서 `H` 에 대해 만든 seal 이다.

| i | prepare seal | commit seal |
|---|---|---|
| 0 | `948ebbf09e29ab73d442c98b7388743c7fde4fc94f346b5352cd5b70e2964ddc0cde0550fb9797a85a3955fd5f91911012c063e29dc28bf3b4a6d12a0d598ce2104599197c83cf92abc3f2406131e8f38ba7b7c282e5055525ee4867f9d11f18` | `b6e536219a2ccd63720a9f4964350c9b1d165ea490b27aaeeb8755b1958e11db6220d43910e8136d1392ac662e62d3b50cef653210638ffccf2fed38fd0b5580c0db9aaf7643833b410175602dd94ec43cdb3befe578e174b1bd73ad2e22bdf7` |
| 1 | `b9ed2c8ca8ef593fa5b1749082a5bc211460b913a9c5a3eec8770d5acff6bf6501dfa61735f6cdd045e5e75a9489801a0bca5554be5abae320e5d883dae61838ad5765c1427cb4a5672dabd5649969760505ff7aaee60aa2e2e5a623af1726a1` | `8be6ed014837a2899ec095d92355a408ee4031726639fb2a9cbf6da3337a0326f18ff10e5efb8c273bbb3796fe2b15b3128da804e78612920c0621e93a3baf3883085445dc2da093dd96e34b9b373706c801aabef8b2f698034b178cb53b8123` |
| 2 | `98d7eb5e1339363dee799b40f76c632a8bde3451458d9731ef8bcee5557ce85f96f64a228734ee776b3099b88b6ca885046e1b2c3b5764982529b5d9592cc642d9ce399e4d584584443d73b6f9bdea2826daff2be70b8a6d85159f02ee41302d` | `a0b379390308e1abca3dd3fda9b8ca2bb97bec9d21a273d715f98e0d9571ca25872c717ec95185377bf4ff56ad37a273163b0429116df980359598028a79e7dbadec4e9580d8a6111e2106c2e8081de928cd898e1db0757fb65f98a264323164` |

다음 표는 `Engine.CommitHeader` 가 header 에 기록한 aggregate 값이다.

| 값 | 결과 |
|---|---|
| prepared aggregate: sealers | `07` |
| prepared aggregate: signature | `895488e186a03aca550753cf195015b7affeecb72d78908823c3c6aec07b66b32f16101b35e309c162f05d763ec70b770842d26f3913ae6bf5123566b852c79bcfcf27e7844e513c662b736916ea81fb0b094b1215ed129e6ab3f71a88164359` |
| committed aggregate: sealers | `07` |
| committed aggregate: signature | `b1d4051be1d47483b8a553ae1d32c0a50c604c156bdda2ce050109daf4d2ac5b5c2d9c1bc71d20d7441f3e0f79c1664609db5c613953b11ba97580b068eb427f4743d3cce8853cd0e741e8fc800263ccc6d4f5ab2844be5f6c1f8b619d5fb341` |
| validator 0, 1, 2 의 aggregate public key | `ae65590bbc43826eab7e89df4307bfec7006a4bf3e735f7ef63dc707a04c73bb3fb16bdab90576f3c7419d5af9390fb0` |
| `bls_verify(apk, seal_data(H, 0, COMMIT_SEAL), committed signature)` | true |
| commit 된 header 의 `block_hash` | `6284d1a2…0a1e` (바뀌지 않는다, [WBFT-CRYPTO-041]) |
| round `2^32 + 3` 으로 `CommitHeader` 를 호출한 경우 | header 의 `round` 필드 = `3` |

### 8.5 Randao

| `chain_id` | `number` | preimage | `randao_data` |
|---|---|---|---|
| 8282 | 0 | `205a01` | `f6c98387ef170854e352073e846537b9947cfe23ddb4fa79932c3c248577ebd2` |
| 8282 | 1 | `205a0101` | `48516c21b70c7a85c71e6fe34cd724419d7803fb58bc1cf3f69a76eb85d98b3f` |
| 8282 | 256 | `205a010100` | `50ee1ee4f218972dbf488dd8f6eee54beb17e0b75169d1cdc543ac439516951c` |
| 8283 | 1 | `205b0101` | `2536b43be46ffc0db54c300982998c0476b79a0c13bc5558e84887db975a046a` |

| 경우 | 값 |
|---|---|
| reveal = `ecdsa_sign(key0, randao_data(8282, 1))` | `c2384d0a5f278af89e4a50f9cb4bab11e13b47a9326bad7dd80676f3b64a8afd5838925a1c50009cdfdfca97b92b95e483bed47591461bbab3d3efa442c8027900` |
| `keccak256(reveal)` | `02d9a1f0741587237b57b8ef79de5d93e99fbac76077a26c03ad4139f01769af` |
| `randao_mix(0x00…00, reveal)` | `02d9a1f0741587237b57b8ef79de5d93e99fbac76077a26c03ad4139f01769af` |
| `randao_mix(0x00ff00ff…00ff, reveal)` | `0226a10f74ea87dc7ba8b81079215d6ce960ba386088a293035241c6f0e86950` |
| `randao_mix(keccak256(reveal), reveal)` (앞쪽의 0 을 유지한다) | `0000000000000000000000000000000000000000000000000000000000000000` |
| `ecdsa_recover_address(randao_data(8282,1), reveal)` | `0x376Ab524a47492d1c8222C67c0cff8d5FeC34112` |
