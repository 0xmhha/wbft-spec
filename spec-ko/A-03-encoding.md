# A-03. 인코딩

- Status: draft
- Areas: `ENC` (RLP 규칙, header extra data, block hash), `MSG` (합의 메시지의 wire 형식과 signing payload)
- Reference implementation: go-stablenet `740526d03` 과, 그 안에 포함된(vendored) `rlp` 패키지

WBFT 가 wire 에 싣거나 header 에 넣는 byte string 은 모두 RLP 인코딩이다. 이 장은 먼저 reference 가 적용하는 RLP 규칙을 정한다. 그 규칙은 go-ethereum 의 규칙이며, 없는 값을 표현하는 go-ethereum 의 관례도 포함한다. 이어서 이 장은 `Header.Extra` 에 들어가는 합의 데이터(`WBFTExtra`), 현재 block 의 seal 을 빼고 계산하는 block hash 규칙, 그리고 네 가지 합의 메시지를 정한다. 합의 메시지마다 wire 형식, 서명하는 바이트, decode 하면서 하는 검사, 중복을 알아보는 데 쓰는 key 를 정한다. reference 코드로 계산한 바이트 단위 예제는 §9 에 있다.

노드가 decode 한 메시지를 받아들이는지, 그리고 그 메시지로 state 를 어떻게 바꾸는지는 `A-05` 가 정한다. `istanbul/100` 에서 메시지를 frame 에 담고 relay 하는 방법은 `A-07` 이 정하고, 어떤 header 필드를 어떤 순서로 검사하는지는 `A-08` 이 정한다.

---

## 1. 표기법

- `rlp_encode(v)` 와 `rlp_decode(b, T)` 는 §2 와 §3 의 규칙을 schema 타입 `T` 의 값에 적용하는 함수이다.
- schema 는 대괄호 안의 list 로 쓴다. `[a: T1, b: T2]` 는 첫 항목이 `a` 를 `T1` 으로 encode 하고, 둘째 항목이 `b` 를 `T2` 로 encode 하는 RLP list 이다.
- 항목 타입은 다음과 같다. `bytes` 는 길이에 제한이 없는 RLP string 이다. `bytesN` 은 정확히 `N` 바이트인 RLP string 이다. `uint32`, `uint64`, `bigint`(§3)는 정수이다. `list[T]` 는 모든 항목이 `T` 인 RLP list 이다. `opt[T]` 는 없을 수도 있는 값이다(§2.4).
- `0x80` 은 빈 string 이고, `0xc0` 은 빈 list 이다.

---

## 2. RLP 규칙

### 2.1 항목과 canonical form

[WBFT-ENC-001] 이 명세의 모든 인코딩은 반드시 Ethereum Yellow Paper 부록 B 가 정의하는 RLP 여야 한다.
- Source: `rlp/encode.go:77-93` (EncodeToBytes), `rlp/decode.go:1038-1097` (readKind)
- Observable: header, network

[WBFT-ENC-002] decoder 는 canonical form 이 아닌 입력을 반드시 거부해야 한다. 거부해야 하는 입력은 구체적으로 다음과 같다.
1. `0x00 … 0x7f` 범위의 한 바이트를 한 바이트짜리 string(`0x81 xx`)으로 encode 한 입력이다.
2. 긴 형식의 길이(prefix `0xb8 … 0xbf` 또는 `0xf8 … 0xff`)를 썼는데 그 길이 값이 56 보다 작은 입력이다.
3. 긴 형식의 길이가 0 바이트로 시작하는 입력이다.
4. 적힌 길이가 남은 입력보다 긴 입력이다.
- Source: `rlp/decode.go:1038-1096` (readKind), `rlp/decode.go:1098-1121` (readUint), `rlp/decode.go:1011-1036` (Kind: element/value too large), `rlp/decode.go:635-658` (Bytes), `rlp/decode.go:369-399` (decodeByteArray), `rlp/decode.go:742-773` (uint), `rlp/decode.go:848-889` (decodeBigInt)
- Observable: header, network

[WBFT-ENC-003] header extra, 메시지 payload 처럼 byte string 전체를 하나의 값으로 decode 할 때, 첫 번째 완전한 항목 뒤에 바이트가 남아 있으면 decoder 는 반드시 그 입력을 거부해야 한다. PRE-PREPARE round-change justification 의 항목도 같은 함수로 decode 하지만, 각 항목은 먼저 정확히 RLP 항목 하나로 잘려 나오므로 이 규칙은 그 항목에서 효과가 없다.
- Source: `rlp/decode.go:92-106` (DecodeBytes, `ErrMoreThanOneValue`), `rlp/decode.go:690-716` (Raw), `consensus/wbft/messages/preprepare.go:84`, `consensus/wbft/messages/preprepare.go:96`
- Observable: header, network

### 2.2 구조체

[WBFT-ENC-004] 구조체는 반드시 필드를 선언한 순서대로 나열한 list 로 encode 해야 한다. decoder 는 항목 수가 필드 수보다 적은 list("too few elements")와 필드 수보다 많은 list("input list has too many elements")를 반드시 거부해야 한다. 이 장의 WBFT 구조체에는 optional 필드나 tail 필드가 없다. 이 장이 사용하는 go-ethereum 구조체 가운데 뒤쪽에 optional 필드를 가진 것은 `Header`(§5.1)와 block list `extblock`(§5.2, 뒤쪽의 `withdrawals`)이다. 같은 `rlp:"optional"` 규칙은 state account 의 `Extra` 필드에도 적용된다(`B-04` SNET-SYS-070). 이 규칙에서 encoder 는 값이 0 인 뒤쪽 optional 필드를 생략하고, decoder 는 빠진 optional 필드를 0 으로 읽는다.
- Source: `rlp/decode.go:404-435` (makeStructDecoder), `core/types/block.go:84-97` (the `rlp:"optional"` fields of `Header`), `core/types/block.go:234` (`extblock.Withdrawals`), `core/types/state_account.go:37` (`StateAccount.Extra`)
- Observable: header, network

### 2.3 Byte string 과 고정 길이 배열

[WBFT-ENC-005] `bytes` 타입의 필드는 길이 0 을 포함해 어떤 길이든 반드시 받아들여야 한다. `bytesN` 타입의 필드(`Address` = `bytes20`, `Hash` = `bytes32`, `Bloom` = `bytes256`, `BlockNonce` = `bytes8`)는 길이가 `N` 이 아닌 string 을 반드시 거부해야 하고, list 도 반드시 거부해야 한다.
- Source: `rlp/decode.go:369-399` (decodeByteArray), `rlp/encode.go:253-265`
- Observable: header, network

### 2.4 없는 값

reference 는 optional 값을 Go pointer 로 표현하고, 없는 값을 nil pointer 로 표현한다. wire 에서 없는 값은 빈 항목으로 쓴다. 어떤 빈 항목을 쓰는지는 값의 타입이 정한다.

| optional 값의 타입 | "없음" 의 인코딩 | 그 자리에 빈 항목이 왔을 때의 decode 결과 |
|---|---|---|
| `rlp:"nil"` 태그가 붙은 구조체 (`AggregatedSeal`, `EpochInfo`) | `0xc0` | `0xc0` 은 없음으로 읽는다. `0x80` 은 오류 "wrong kind of empty value" 이다 |
| `rlp:"nil"` 태그가 없는 구조체 | `0xc0` | 구조체 자신의 decoder 가 빈 list 를 읽다가 실패한다("too few elements") |
| `bigint` (`*big.Int`), `rlp:"nil"` 태그의 유무와 무관 | `0x80` | `0x80` 은 **값 0 으로 존재한다**. `0xc0` 은 오류 "expected input string or byte" 이다 |
| `bytes` (nil slice) | `0x80` | `0x80` 은 빈 값으로 존재한다 |
| `list[T]` (nil slice) | `0xc0` | `0xc0` 은 빈 list 로 존재한다 |

`Block` 에는 `rlp:"nil"` 태그가 붙지 않는다. 없는 block 은 `0xc0` 으로 encode 한다 (둘째 행). ROUND-CHANGE 의 `prepared_block` 자리에서 빈 항목을 어떻게 decode 하는지는 [WBFT-MSG-032] 의 4 단계가 정한다. PRE-PREPARE 의 `proposal` 에는 태그가 없으므로, 그 자리의 `0xc0` 은 decode 에 실패한다 ([WBFT-MSG-041]).

[WBFT-ENC-006] encoder 는 반드시 없는 구조체를 `0xc0` 으로 encode 하고, 없는 `bigint` 나 `bytes` 를 `0x80` 으로 encode 해야 한다.
- Source: `rlp/encode.go:413-432` (makePtrWriter), `rlp/encode.go:215-226` (writeBigIntPtr), `rlp/internal/rlpstruct/rlpstruct.go:49-55` (DefaultNilValue), `rlp/typecache.go:215-232`
- Observable: header, network

[WBFT-ENC-007] `rlp:"nil"` 태그가 붙은 구조체 필드에서 decoder 는 `0xc0` 을 반드시 없음으로 decode 해야 하고, `0x80` 을 반드시 거부해야 한다. 비어 있지 않은 항목은 반드시 그 구조체로 decode 해야 한다.
- Source: `rlp/decode.go:476-513` (makeNilPtrDecoder)
- Observable: header, network

[WBFT-ENC-008] `bigint` 필드는 `rlp:"nil"` 태그가 붙어 있더라도 없음으로 decode 해서는 안 된다. `0x80` 은 값 0 으로 decode 된다. 그러므로 encoder 가 쓰는 "없음" 과 "0" 은 같은 바이트이고, decode 한 `bigint` 는 결코 없음이 되지 않는다.
- Source: `rlp/decode.go:156-163` (the `*big.Int` case precedes the pointer case), `rlp/decode.go:231-243` (decodeBigInt)
- Observable: header

---

## 3. Scalar 인코딩

### 3.1 고정 폭 부호 없는 정수

[WBFT-ENC-010] `uint32` 나 `uint64` 는 반드시 RLP string `be_min(x)` 로 encode 해야 한다. 따라서 `0` 은 `0x80` 이 되고, `1 … 127` 은 한 바이트가 된다. decoder 는 다음 입력을 반드시 거부해야 한다.
1. 0 바이트로 시작하는 string 이다. 한 바이트 `0x00` 도 여기에 들어간다("non-canonical integer").
2. 타입 폭보다 긴 string 이다. decoder 는 `uint32` 에서는 4바이트를, `uint64` 에서는 8바이트를 넘는 string 을 거부한다.
3. RLP list 이다.
- Source: `rlp/encode.go:205-213` (writeUint), `rlp/decode.go:742-773` (Stream.uint)
- Observable: header, network

### 3.2 크기 제한이 없는 정수

[WBFT-ENC-011] `bigint` 는 반드시 RLP string `be_min(x)` 로 encode 해야 한다. 음수는 encode 할 수 없다. decoder 는 어떤 길이든 반드시 받아들여야 하고, 0 바이트로 시작하는 string, 한 바이트 `0x00`, list 를 반드시 거부해야 한다. 그러므로 decode 한 값은 결코 음수가 아니다.
- Source: `rlp/encode.go:215-235`, `rlp/decode.go:848-889` (decodeBigInt)
- Observable: header, network

---

## 4. `WBFTExtra`

### 4.1 배치

모든 WBFT block 의 `Header.Extra` 는 정확히 `WBFTExtra` list 하나를 RLP 로 encode 한 값이다. RLP list 바깥에는 prefix 바이트가 없고, vanity 바이트는 list 의 첫 항목이다.

```
WBFTExtra = [
    vanity_data:         bytes,
    randao_reveal:       bytes,                    # 65-byte ECDSA signature (A-02 §7.2)
    prev_round:          uint32,
    prev_prepared_seal:  opt[AggregatedSeal],      # rlp:"nil": absent = 0xc0
    prev_committed_seal: opt[AggregatedSeal],      # rlp:"nil"
    round:               uint32,
    prepared_seal:       opt[AggregatedSeal],      # rlp:"nil"
    committed_seal:      opt[AggregatedSeal],      # rlp:"nil"
    gas_tip:             bigint,                   # absent on encode = 0x80; decodes to 0 (WBFT-ENC-008)
    epoch_info:          opt[EpochInfo],           # rlp:"nil"
]
AggregatedSeal = [ sealers: bytes, signature: bytes ]
EpochInfo      = [ candidates: list[Candidate], validators: list[uint32], bls_public_keys: list[bytes] ]
Candidate      = [ addr: bytes20, diligence: uint64 ]
```

[WBFT-ENC-020] `WBFTExtra` 는 반드시 위의 열 개 항목만으로 이루어진 list 로, 위의 순서와 위의 항목 타입대로 encode 해야 한다.
- Source: `core/types/istanbul.go:80-92`, `core/types/istanbul.go:105-119` (EncodeRLP)
- Observable: header

[WBFT-ENC-021] `decode_extra(header)` 는 반드시 §2 와 §3 의 규칙을 적용한 `rlp_decode(header.Extra, WBFTExtra)` 여야 한다. 즉 항목은 정확히 열 개여야 하고, 네 seal 필드와 `epoch_info` 에는 `rlp:"nil"` 의미를, `gas_tip` 에는 `bigint` 의미를 적용하며, list 뒤에 남는 바이트가 없어야 한다.
- Source: `core/types/istanbul.go:121-149` (DecodeRLP), `core/types/istanbul.go:248-258` (ExtractWBFTExtra)
- Observable: header

[WBFT-ENC-022] `encode_extra(extra)` 는 반드시 [WBFT-ENC-020] 에 따른 `rlp_encode(extra)` 여야 한다. `decode_extra` 가 받아들이는 모든 byte string `b` 에 대해 `encode_extra(decode_extra(b)) = b` 가 성립한다. 받아들여지는 입력은 모두 canonical 이고, §4.1 의 각 자리에서 받아들여지는 빈 항목은 decode 한 뒤 다시 encode 하면 같은 빈 항목이 되기 때문이다.
- Source: `core/types/istanbul.go:105-149`; checked in §9.1 (`ca808080c0c080c0c080c0` re-encodes to itself)
- Observable: header

verifier 는 extra 를 decode 할 수 없는 header 를 `ErrInvalidExtraDataFormat`("invalid extra data format")으로 거부한다 (`consensus/wbft/engine/engine.go:300-304`, `A-08`).

> 해설: `vanity_data` 가 RLP list 앞에 붙은 32바이트 prefix 라고 가정하면 틀린다. vanity 는 list 의 첫 항목이다. 열 개 항목은 세 무리로 나누어 읽으면 이해하기 쉽다.
>
> 1. 이 block 을 만든 노드의 정보는 `vanity_data`, `randao_reveal`, `gas_tip`, `epoch_info` 이다. proposer 가 block 을 만들 때 이 필드를 쓰고, 이 필드는 block hash 에 들어간다.
> 2. parent block 의 증거는 `prev_round`, `prev_prepared_seal`, `prev_committed_seal` 이다. proposer 는 자기가 가진 parent block 의 seal 에 늦게 도착한 seal(extra seal)을 합쳐 이 필드를 쓴다. 이 필드는 block hash 에 들어가므로, 한 번 쓰이면 모든 노드에서 같다.
> 3. 이 block 자신의 증거는 `round`, `prepared_seal`, `committed_seal` 이다. 이 값은 합의가 끝난 뒤에야 알 수 있으므로 block hash 에서 뺀다(§6). 그래서 이 값은 노드마다 다를 수 있다(`A-08` WBFT-HDR-053).

### 4.2 `vanity_data`

[WBFT-ENC-030] verifier 는 `vanity_data` 의 길이와 내용이 무엇이든 반드시 받아들여야 한다. 노드는 `vanity_data` 를 해석하지 않는다.
- Source: `consensus/wbft/engine/engine.go:196-346` (no check), `core/types/istanbul.go:121-149`
- Observable: header

[WBFT-ENC-031] reference 를 따르는 proposer 는 새 header 에 미리 들어 있던 `Extra` 에서 새 extra 를 만든다. 미리 들어 있던 `Extra` 는 miner 에 설정한 extra data 이고, 길이는 최대 `MAXIMUM_EXTRA_DATA_SIZE = 32` 바이트이다. proposer 는 다음과 같이 새 extra 를 만든다.
1. `Extra` 가 32바이트보다 짧으면 proposer 는 `vanity_data = Extra ‖ 0x00 × (32 − len(Extra))`, 빈 `randao_reveal`, `prev_round = round = 0` 으로 새 extra 를 시작한다. 이때 모든 seal 과 `epoch_info`, `gas_tip` 은 없음으로 둔다.
2. `Extra` 가 32바이트 이상이면 proposer 는 그 값을 `WBFTExtra` 로 decode 하고, decode 한 필드에서 시작한다. decode 에 실패하면 proposer 는 header 를 준비하지 못한다.

proposer 는 32바이트 `vanity_data` 를 만드는 것이 좋다 (SHOULD).
- Source: `consensus/wbft/engine/engine.go:1162-1182` (getExtra), `consensus/wbft/engine/apply_extra.go:38-50` (ApplyHeaderWBFTExtra), `consensus/wbft/engine/engine.go:486-550` (Prepare), `miner/worker.go:1150-1152`, `eth/backend.go:307-321`
- Observable: header

### 4.3 그 밖의 scalar 필드

[WBFT-ENC-032] `prev_round` 와 `round` 는 반드시 `uint32` 여야 한다([WBFT-ENC-010]). 2^32 이상인 round 는 표현할 수 없으므로 2^32 로 나눈 나머지로 저장된다(`A-01` [WBFT-TYPE-012]).
- Source: `core/types/istanbul.go:84`, `core/types/istanbul.go:87`, `consensus/wbft/engine/engine.go:153-158`
- Observable: header

[WBFT-ENC-033] `gas_tip` 은 반드시 wei 단위의 `bigint` 여야 한다. decode 한 뒤에는 `gas_tip` 이 항상 존재한다. encode 된 `0x80` 은 값 0 이기 때문이다. proposer 는 `number ≥ 1` 인 모든 block 에 `gas_tip` 을 쓰고(`B-06`), genesis extra 에는 초기 gas tip 이 들어간다(§4.7).
- Source: `core/types/istanbul.go:90`, `core/types/istanbul.go:132`, `consensus/wbft/engine/engine.go:599-605` (WriteGasTip)
- Observable: header

인코딩 계층에서 `randao_reveal` 은 임의의 `bytes` 이다. 그 길이와 의미는 `A-02 §7.2` 가 정한다. 어떤 height 에서 `prev_*` seal 과 `epoch_info` 가 반드시 있어야 하는지는 `A-08` 과 `A-04` 가 정한다.

### 4.4 `AggregatedSeal` 과 `SealerSet`

```python
def sealer_indices(sealers: bytes) -> list[int]:
    """SealerSet.GetSealers: ascending indices; bit b of byte k is index 8*k + b (b = 0 is the least significant bit)."""
    return [8 * k + b for k in range(len(sealers)) for b in range(8) if sealers[k] >> b & 1]

def make_sealer_set(indices) -> bytes:
    """SealerSet.SetSealer applied to each index: minimal length."""
    out = bytearray()
    for i in indices:
        if len(out) <= i // 8:
            out += bytes(i // 8 + 1 - len(out))
        out[i // 8] |= 1 << (i % 8)
    return bytes(out)
```

[WBFT-ENC-040] `AggregatedSeal` 은 반드시 두 항목 list `[sealers, signature]` 로 encode 해야 하며, 두 항목은 모두 `bytes` 이다.
- Source: `core/types/istanbul.go:52-74`
- Observable: header

[WBFT-ENC-041] `sealers` 에서 sealer 인덱스 `i` 는 반드시 바이트 `i div 8` 의 비트 `i mod 8` 로 표현해야 한다. 비트는 최하위 비트부터 센다. bitmap 이 가리키는 인덱스는 오름차순으로 나열한 `sealer_indices(sealers)` 이다.
- Source: `core/types/istanbul.go:290-325` (SealerSet)
- Observable: header

[WBFT-ENC-043] `signature` 는 96바이트 compressed BLS aggregate 이다(`A-02 §5.6`). decoder 는 그 길이를 검사하지 않는다. `AggregatedSeal` 이 존재하지만 `signature` 가 빈 경우(`0xc2 0x80 0x80`)와 `AggregatedSeal` 이 없는 경우(`0xc0`)는 서로 다른 인코딩이다. header 검증은 두 경우를 모두 반드시 "seal 없음" 으로 다뤄야 한다(`A-08`).
- Source: `core/types/istanbul.go:52-74`, `consensus/wbft/engine/engine.go:390-392`, `consensus/wbft/engine/engine.go:422-425`
- Observable: header

> 해설: header 검증은 두 인코딩을 같게 다루지만, 두 인코딩은 바이트가 다르다. 그래서 block hash 에 들어가는 `prev_prepared_seal` 과 `prev_committed_seal` 자리에서는 두 인코딩이 서로 다른 block hash 를 만든다.

### 4.5 `Candidate` 와 `EpochInfo`

[WBFT-ENC-050] `Candidate` 는 반드시 `[addr: bytes20, diligence: uint64]` 로 encode 해야 한다. `EpochInfo` 는 반드시 §4.1 의 원소 타입을 가진 `[candidates, validators, bls_public_keys]` 로 encode 해야 한다. `candidates` 의 각 원소는 반드시 비어 있지 않은 `Candidate` list 여야 한다. 원소로 빈 list `0xc0` 이 오면 decoder 는 그 입력을 거부한다.
- Source: `core/types/istanbul.go:94-103`, `core/types/istanbul.go:204-246`
- Observable: header

[WBFT-ENC-051] decoder 는 decode 하는 동안 `EpochInfo` 의 세 list 사이의 일관성을 검사해서는 안 된다. 이 일관성에는 `validators` 와 `bls_public_keys` 의 길이가 같은지, `validators[i] < len(candidates)` 인지, 값이 중복되지 않는지, key 길이가 맞는지가 들어간다. 이 성질들은 `A-04` 와 `B-06` 이 유효한 epoch block 에 요구하는 것이고, 검사도 그 두 장에서 한다.
- Source: `core/types/istanbul.go:213-225`, `consensus/wbft/engine/engine.go:1212-1263` (verifyEpoch)
- Observable: header

### 4.6 `WBFTExtra` decode 실패 요약

| 입력의 결함 | 결과 |
|---|---|
| RLP list 가 아니거나, canonical 이 아닌 RLP 이거나, 뒤에 바이트가 남아 있다 | 실패한다 |
| 항목이 10개보다 적거나 많다 | 실패한다 |
| `prev_round` 나 `round` 가 4바이트보다 길거나, 0 바이트로 시작하거나, `0x00` 이다 | 실패한다 |
| seal 필드나 `epoch_info` 가 `0x80` 이다 | 실패한다 ("wrong kind of empty value") |
| `gas_tip` 이 `0xc0` 이다 | 실패한다 ("expected input string or byte") |
| `AggregatedSeal` 이 두 항목 list 가 아니거나, `Candidate` 가 두 항목 list 가 아니거나, `EpochInfo` 가 세 항목 list 가 아니다 | 실패한다 |
| `Candidate.addr` 가 20바이트가 아니거나, `diligence` 가 8바이트보다 길다 | 실패한다 |
| `vanity_data`, `randao_reveal`, `sealers`, `signature`, BLS key 의 길이가 무엇이든 상관없다 | decode 는 받아들인다 |

Source: computed with the reference decoder, §9.1.

### 4.7 Genesis extra

[WBFT-ENC-060] genesis header 의 `Extra` 는 반드시 `encode_extra(WBFTExtra{ vanity_data = empty, randao_reveal = empty, prev_round = 0, seals absent, round = 0, gas_tip = G, epoch_info = E })` 여야 한다. 여기서 `E` 와 `G` 는 다음과 같다.
1. `E = EpochInfo{ candidates = [(init.validators[i], DEFAULT_DILIGENCE)], validators = [0, 1, …, k−1], bls_public_keys = [init.blsPublicKeys[i]] }` 이고, 원소는 `init.validators` 의 순서를 따른다.
2. `G` 는 `govValidator.params.gasTip` 을 부호를 허용하는 10진 정수로 읽은 값이다 (Go `big.Int.SetString(s, 10)`). 그 parameter 가 없거나, 비어 있거나, 읽을 수 없으면 `G` 는 `INITIAL_GAS_TIP` 이다. 음수 값이면 extra 생성이 실패한다 (`rlp: cannot encode negative big.Int`).

genesis 를 만드는 방법은 `B-02` 가 정한다. parameter 가 존재하는데 비어 있거나 10진 정수가 아니면 genesis 생성이 실패한다(`B-02` SNET-GEN-010). 그러므로 유효한 genesis 에서 `INITIAL_GAS_TIP` 을 쓰는 경우는 parameter 가 없을 때뿐이다.
- Source: `consensus/wbft/config.go:227-281` (CreateInitialExtraData, CreateInitialEpochInfo)
- Observable: header

---

## 5. Header 와 block

### 5.1 Header

[WBFT-ENC-070] header 는 반드시 go-ethereum 의 header list 로 encode 해야 한다. 앞쪽 필드는 `[ParentHash: bytes32, UncleHash: bytes32, Coinbase: bytes20, Root: bytes32, TxHash: bytes32, ReceiptHash: bytes32, Bloom: bytes256, Difficulty: bigint, Number: bigint, GasLimit: uint64, GasUsed: uint64, Time: uint64, Extra: bytes, MixDigest: bytes32, Nonce: bytes8]` 이다. 그 뒤에 optional 필드 `BaseFee: bigint, WithdrawalsHash: bytes32, BlobGasUsed: uint64, ExcessBlobGas: uint64, ParentBeaconRoot: bytes32` 가 온다. encoder 는 optional 필드 자신이나 그보다 뒤에 있는 optional 필드 가운데 하나라도 존재하면 그 optional 필드를 출력한다. 출력해야 하는 optional 필드의 값이 없으면 `0x80` 으로 쓴다.
- Source: `core/types/block.go:67-97` (Header), `core/types/gen_header_rlp.go:8-89` (EncodeRLP)
- Observable: header

WBFT header 는 `BaseFee` 를 가진다. StableNet preset 에서 London 이 block 0 부터 활성이기 때문이다. WBFT header 는 그 뒤의 optional 필드를 하나도 갖지 않는다. `A-08` 이 그런 필드를 가진 header 를 거부하기 때문이다. 그러므로 WBFT header 는 16개 항목으로 된 list 이다.

### 5.2 Block (proposal)

[WBFT-ENC-071] `Proposal` 은 반드시 go-ethereum 의 block list `[header, transactions: list, uncles: list[header]]` 로 encode 해야 한다. block 에 withdrawals list 가 있을 때만 넷째 항목 `withdrawals` 를 붙인다. decoder 는 항목이 셋인 list 와 넷인 list 를 모두 받아들인다.
- Source: `core/types/block.go:230-235` (extblock), `core/types/block.go:331-350`
- Observable: network

---

## 6. Block hash

### 6.1 Filtered header

```python
def filtered_header(header, round: uint32) -> Header | None:
    """WBFTFilteredHeaderWithRound."""
    extra = decode_extra(header)          # decode 에 실패하면 None
    if extra is None:
        return None
    extra.prepared_seal  = None
    extra.committed_seal = None
    extra.round          = round
    h = copy(header)
    h.Extra = encode_extra(extra)
    return h
```

[WBFT-ENC-080] `filtered_header(header, round)` 는 반드시 `Extra` 를 다른 값으로 바꾼 header 여야 한다. 바꾼 값은 decode 한 `WBFTExtra` 에서 `prepared_seal` 과 `committed_seal` 을 없음으로 두고 `round` 를 인자 `round` 로 바꾼 뒤 다시 encode 한 값이다. 그 밖의 모든 header 필드와 모든 extra 필드는 반드시 바꾸지 않고 두어야 한다. 바꾸지 않는 extra 필드에는 `vanity_data`, `randao_reveal`, `prev_round`, `prev_prepared_seal`, `prev_committed_seal`, `gas_tip`, `epoch_info` 가 들어간다. extra 를 decode 할 수 없으면 `filtered_header` 는 정의되지 않는다.
- Source: `core/types/istanbul.go:260-288`
- Observable: header

### 6.2 Round 를 넣은 hash

[WBFT-ENC-081] `hash_with_round(header, round)` 는 반드시 `keccak256(rlp_encode(filtered_header(header, round)))` 여야 한다. extra 를 decode 할 수 없으면 reference 는 다른 필드와 무관하게 `keccak256(0xc0)`(없는 header 의 RLP)을 돌려준다. 이 값을 seal 대상으로 써서는 안 된다([WBFT-CRYPTO-044]). `hash_with_round` 는 `Difficulty` 에 의존하지 않는다.
- Source: `core/types/block.go:169-172` (WBFTHashWithRoundNumber), `core/types/hashing.go:58-64` (rlpHash), `rlp/encode.go:413-432`
- Observable: header, network

### 6.3 `block_hash`

```python
def block_hash(header) -> Hash:
    """Header.Hash."""
    if header.Difficulty == 1:
        fh = filtered_header(header, 0)
        if fh is not None:
            return keccak256(rlp_encode(fh))
    return keccak256(rlp_encode(header))
```

[WBFT-ENC-082] `header.Difficulty = 1` 이고 extra 를 decode 할 수 있으면 `block_hash(header)` 는 반드시 `keccak256(rlp_encode(filtered_header(header, 0)))` 여야 하고, 그렇지 않으면 반드시 `keccak256(rlp_encode(header))` 여야 한다. 이 값은 모든 곳에서 쓰는 block hash 이다. child 의 `ParentHash`, block 조회, RPC, 합의 digest 가 모두 이 값을 쓴다.
- Source: `core/types/block.go:119-131` (Header.Hash), `core/types/block.go:507-514` (Block.Hash)
- Observable: header, rpc

[WBFT-ENC-084] extra 를 decode 할 수 없는 WBFT header 에서는 `block_hash` 가 `keccak256(rlp_encode(header))`(Ethereum 규칙)로 돌아간다. 그런 header 는 유효하지 않지만([WBFT-ENC-021], `A-08`), 노드는 그 hash 를 여전히 이 규칙으로 계산하는 것이 좋다(SHOULD). 예를 들어 노드는 그 header 를 조회하거나 bad block 으로 보고할 때 이 hash 를 쓴다.
- Source: `core/types/block.go:121-131`; checked in §9.3
- Observable: header

> 해설: `Difficulty = 1` 은 WBFT hash 규칙을 켜는 스위치이다. block 1 이상은 모두 `Difficulty` 가 1 이어야 한다. genesis 의 difficulty 는 genesis 파일에서 제약 없이 복사되는데, testnet preset 의 값은 0 이다. 그래서 testnet genesis hash 는 Ethereum 규칙으로 계산된다. hash 함수를 `Difficulty` 를 보지 않고 구현하면 testnet genesis hash 가 틀린다.

---

## 7. 다시 encode 하기

reference 는 메시지와 extra 를 구조체로 decode 해 두었다가, 바이트가 다시 필요할 때 구조체를 다시 encode 한다. 바이트가 다시 필요한 때는 signing payload 를 계산할 때, header 의 hash 를 계산할 때, backlog 에서 꺼낸 메시지를 relay 할 때이다. canonical 입력에서는 다시 encode 한 결과가 원래 바이트와 같다. 예외는 아래의 경우뿐이다.

[WBFT-ENC-090] 구현은 메시지 signing payload(§8.1)와 `filtered_header` 를 반드시 받은 바이트가 아니라 decode 한 필드 값으로 계산해야 한다. 이 절에 나열한 경우처럼 decode 한 뒤 다시 encode 한 결과가 원래 바이트와 다르면, reference 는 다시 encode 한 바이트로 signing payload 와 `filtered_header` 를 계산한다.
- Source: `consensus/wbft/core/handler.go:268-317` (verifySignatures uses EncodePayloadForSigning), `core/types/istanbul.go:280-285`
- Observable: network, header

| 구조 | 받은 값 | 다시 encode 한 값 |
|---|---|---|
| ROUND-CHANGE `prepared_block` / `justification` | `0x80` 이나 한 바이트 `0x00 … 0x7f` (없음으로 받아들인다, [WBFT-MSG-032]) | `0xc0` |
| ROUND-CHANGE `prepared` | `[pr, 0x00…00]` (받아들인다) | `[]` (signing payload 가 서명된 바이트와 더 이상 일치하지 않는다) |
| PRE-PREPARE justification list | 받은 그대로 | list 를 항목 단위로 다시 encode 한다. canonical 항목이면 원래 바이트와 같다. 다만 `prepared = [pr, 0x00…00]` 인 round-change 항목은 바로 위 행처럼 `prepared = []` 로 다시 encode 된다 |

---

## 8. 합의 메시지

### 8.1 공통 구조

모든 합의 메시지는 `sequence: bigint`, `round: bigint`, 그리고 65바이트 ECDSA `signature` 필드를 가진다. 메시지 코드(`A-01 §4.1`)와 보낸 사람의 주소는 encode 된 메시지에 들어가지 않는다. 메시지 코드는 devp2p 메시지 코드(`A-07`)가 맡고, 보낸 사람(`source`)은 signature 에서 recover 한다.

```python
def message_signing_payload(msg) -> bytes:
    """EncodePayloadForSigning."""
    return rlp_encode([msg.code, signed_fields(msg)])     # code as uint64 (0x12 .. 0x15)

def message_signature(node_key, msg) -> bytes65:
    return ecdsa_sign(node_key, message_signing_payload(msg))       # A-02 §3.2
```

[WBFT-MSG-001] 코드가 `c ∈ {0x12, 0x13, 0x14, 0x15}` 인 devp2p 메시지의 payload 는 반드시 코드 `c` 에 해당하는 메시지 타입의 `rlp_encode(msg)` 와 정확히 같아야 하며, 다른 것으로 한 번 더 감싸지 않는다.
- Source: `consensus/wbft/backend/backend.go:202-206`, `eth/protocols/eth/peer.go:519-525` (SendWithNoEncoding), `consensus/wbft/backend/handler.go:54-67`
- Observable: network

[WBFT-MSG-002] 메시지의 signature 는 반드시 `ecdsa_sign(node_key, message_signing_payload(msg))` 여야 한다. 여기서 `message_signing_payload(msg) = rlp_encode([code, signed_fields])` 이고, `signed_fields` 는 §8.2 – §8.5 가 메시지마다 정하는 list 이다. 코드가 첫 항목이므로, 한 메시지 타입에 대한 signature 는 다른 메시지 타입에서 결코 유효하지 않다.
- Source: `consensus/wbft/messages/prepare.go:61-67`, `consensus/wbft/messages/commit.go:49-55`, `consensus/wbft/messages/roundchange.go:170-182`, `consensus/wbft/messages/preprepare.go:50-56`, `consensus/wbft/core/prepare.go:52-62`
- Observable: network

[WBFT-MSG-003] 받는 노드는 메시지의 보낸 사람을 반드시 `ecdsa_recover_address(message_signing_payload(decoded_msg), signature)` 로 정해야 하고([WBFT-CRYPTO-013], [WBFT-ENC-090]), 메시지 안에 들어 있는 모든 서명된 payload 에 대해서도 반드시 같은 방법을 써야 한다. 메시지 안의 서명된 payload 는 ROUND-CHANGE justification 의 PREPARE 들과, PRE-PREPARE justification 의 round-change payload 들 및 PREPARE 들이다. 받는 노드는 그 뒤 보낸 사람이 validator set 에 속하는지를 `A-05` 에 따라 검사한다.
- Source: `consensus/wbft/core/handler.go:268-317` (verifySignatures)
- Observable: network

[WBFT-MSG-004] 받는 노드는 반드시 메시지 코드에 맞는 decoder 로 payload 를 decode 해야 한다. 코드가 `{0x12, 0x13, 0x14, 0x15}` 밖에 있으면 합의 메시지로 decode 해서는 안 된다. 이때 reference 는 `invalid message event code` 를 보고하고, `messages.Decode` 자신은 `ErrInvalidMessage` 를 돌려준다.
- Source: `consensus/wbft/core/handler.go:188-201`, `consensus/wbft/messages/decode.go:27-59`
- Observable: network, log

> 해설: 메시지 코드를 signing payload 의 첫 항목에 넣는 설계가 필요한 이유는 PREPARE 와 COMMIT 의 wire 형식이 완전히 같기 때문이다(§8.3). PREPARE 바이트를 코드 `0x14` 로 보내면 COMMIT 으로 decode 는 되지만, signing payload 의 코드가 달라지므로 다른 주소가 recover 된다. 그래서 그 메시지는 서명 검사에서 떨어진다. 이 성질은 §8.8 의 dedup key 문제와 맞물린다.

### 8.2 PREPARE (`0x13`)

```
PREPARE        = [ [sequence: bigint, round: bigint, digest: bytes32, prepare_seal: bytes], signature: bytes ]
signed_fields  =   [sequence, round, digest, prepare_seal]
signing payload = rlp_encode([0x13, [sequence, round, digest, prepare_seal]])
```

[WBFT-MSG-010] PREPARE 는 반드시 위와 같이 encode 해야 한다. `digest` 는 proposal 의 `block_hash` 이고(`A-05`), `prepare_seal` 은 96바이트 prepare seal 이다([WBFT-CRYPTO-042]).
- Source: `consensus/wbft/messages/prepare.go:31-36`, `consensus/wbft/messages/prepare.go:61-80`
- Observable: network

[WBFT-MSG-011] PREPARE decoder 는 두 항목 list 가 아닌 입력과, 첫 항목이 위의 항목 타입을 가진 네 항목 list 가 아닌 입력을 반드시 거부해야 한다([WBFT-ENC-004], [WBFT-ENC-005], [WBFT-ENC-011]). decoder 는 `prepare_seal` 과 `signature` 의 길이를 검사하지 않는다. reference 는 PREPARE decode 실패를 `ErrFailedDecodeCommit`("failed to decode COMMIT message")으로 보고한다.
- Source: `consensus/wbft/messages/prepare.go:82-102`, `consensus/wbft/messages/decode.go:36-42`
- Observable: log

### 8.3 COMMIT (`0x14`)

```
COMMIT         = [ [sequence: bigint, round: bigint, digest: bytes32, commit_seal: bytes], signature: bytes ]
signing payload = rlp_encode([0x14, [sequence, round, digest, commit_seal]])
```

[WBFT-MSG-020] COMMIT 은 반드시 위와 같이 encode 해야 한다. `commit_seal` 은 96바이트 commit seal 이다([WBFT-CRYPTO-042]). COMMIT decoder 는 [WBFT-MSG-011] 과 같은 규칙을 따르고, 실패를 `ErrFailedDecodeCommit` 으로 보고한다.
- Source: `consensus/wbft/messages/commit.go:30-83`, `consensus/wbft/messages/decode.go:43-49`
- Observable: network

PREPARE 와 COMMIT 의 wire 형식은 같다. 두 메시지는 코드와, seal 안에 들어 있는 seal type 만 다르다. 코드 `0x14` 로 전달된 PREPARE payload 는 COMMIT 으로 decode 되고, signature 검사([WBFT-MSG-002])에서 실패한다.

### 8.4 ROUND-CHANGE (`0x15`)

```
rc_payload     = [ sequence: bigint, round: bigint, prepared ]
prepared       = [] | [ prepared_round: bigint, prepared_digest: bytes32 ]
SignedRoundChangePayload = [ rc_payload, signature: bytes ]
ROUND-CHANGE   = [ SignedRoundChangePayload, prepared_block, justification ]
prepared_block = <empty item> | Block (§5.2)
justification  = <empty item> | list[PREPARE]
signing payload = rlp_encode([0x15, rc_payload])
```

[WBFT-MSG-030] encoder 는 `prepared_round` 가 존재하고 `prepared_digest` 가 0 바이트 32개가 아닌 경우에 한해 반드시 `prepared = [prepared_round, prepared_digest]` 를 써야 하고, 그 밖의 경우에는 `prepared = []` 를 써야 한다. 특히 prepared round 가 0 이고 digest 가 0 이 아니면 `[0x80, digest]` 로 쓴다.
- Source: `consensus/wbft/messages/roundchange.go:158-168` (encodePayloadInternal)
- Observable: network

[WBFT-MSG-031] ROUND-CHANGE 는 반드시 위의 세 항목 list 로 encode 해야 한다. 없는 `prepared_block` 과, 없거나 비어 있는 `justification` 은 `0xc0` 으로 encode 한다. 보내는 노드가 prepared block 을 가지고 있으면 `prepared_digest` 는 반드시 `block_hash(prepared_block.header)` 와 같아야 한다.
- Source: `consensus/wbft/messages/roundchange.go:43-62` (NewRoundChange), `consensus/wbft/messages/roundchange.go:184-200` (EncodeRLP)
- Observable: network

[WBFT-MSG-032] ROUND-CHANGE decoder 는 반드시 다음 단계를 이 순서대로 수행해야 한다.
1. 바깥 list 와 `SignedRoundChangePayload` list 를 읽는다.
2. `rc_payload` 를 `sequence: bigint`, `round: bigint`, `prepared` 로 된 list 로 decode 한다. 이때 `prepared` 는 반드시 항목이 0개인 list 이거나 정확히 두 항목 `(prepared_round: bigint, prepared_digest: bytes32)` 로 된 list 여야 한다. 항목 수가 그 밖의 값이면 decode 가 실패한다.
3. `signature: bytes` 를 decode 하고, `SignedRoundChangePayload` list 가 거기서 끝나는지 확인한다.
4. 다음 항목을 읽는다. 그 항목이 빈 string, 빈 list, 또는 한 바이트 `0x00 … 0x7f` 이면 `prepared_block` 을 없음으로 다룬다. 그렇지 않으면 그 항목을 Block 으로 decode 하고, `block_hash(prepared_block.header) = prepared_digest` 가 아니면 실패한다.
5. 다음 항목을 읽는다. 그 항목이 4단계와 같은 뜻으로 비어 있으면 `justification` 을 없음으로 다룬다. 그렇지 않으면 그 항목을 `list[PREPARE]` 로 decode 한다.
6. 바깥 list 가 거기서 끝나는지 확인한다. 항목이 모자라거나 남으면 실패한다.

reference 는 모든 실패를 `ErrFailedDecodeRoundChange`("failed to decode ROUND-CHANGE message")로 보고한다. 4단계의 digest 불일치는 `WBFT: Error m.PreparedDigest.Hash() != digest` 로 log 에 남는다.
- Source: `consensus/wbft/messages/roundchange.go:202-317` (RoundChange.DecodeRLP), `consensus/wbft/messages/decode.go:50-56`; edge cases checked in §9.6
- Observable: network, log

[WBFT-MSG-033] decoder 는 `prepared_digest` 가 0 이 아닐 때 `prepared_block` 이 존재하기를 요구해서는 안 되고, `prepared_block` 이 없을 때 `prepared` list 가 비어 있지 않기를 요구해서는 안 된다. prepared round, digest, block, justification 사이의 일관성은 `A-05` 가 검사한다.
- Source: `consensus/wbft/messages/roundchange.go:239-291`
- Observable: network

[WBFT-MSG-034] PRE-PREPARE justification 에서 쓰는 것처럼 `SignedRoundChangePayload` 가 단독으로 쓰일 때, 그 값은 반드시 `[rc_payload, signature]` 로 encode 해야 하고, 두 항목 list 에 [WBFT-MSG-032] 의 1 – 3 단계를 적용해 decode 해야 한다.
- Source: `consensus/wbft/messages/roundchange.go:75-156`
- Observable: network

> 해설: ROUND-CHANGE decoder 는 몇 군데에서 관대하다. `prepared_block` 과 `justification` 자리에 빈 string(`0x80`), 빈 list(`0xc0`), 한 바이트 값(`0x00 … 0x7f`)이 오면 decoder 는 모두 없음으로 받아들인다. `prepared = [pr, 0x00…00]` 도 받아들인다. 이런 입력은 다시 encode 하면 바이트가 달라진다(§7). 또한 decoder 는 `prepared_block` 이 있을 때 `block_hash(prepared_block.header) == prepared_digest` 를 확인하지만, digest 가 0 이 아닌데 block 이 없는 입력은 거부하지 않는다. 그 일관성은 `A-05` 가 판단한다.

### 8.5 PRE-PREPARE (`0x12`)

```
PRE-PREPARE    = [ [ [sequence: bigint, round: bigint, proposal: Block], signature: bytes ],
                   [ justification_round_changes: list[SignedRoundChangePayload],
                     justification_prepares:      list[PREPARE] ] ]
signing payload = rlp_encode([0x12, [sequence, round, proposal]])
```

[WBFT-MSG-040] PRE-PREPARE 는 반드시 위와 같이 encode 해야 한다. signing payload 는 proposal block 의 RLP 인코딩 전체를 포함한다. justification list 는 PRE-PREPARE signature 가 덮지 않는다. 대신 각 항목이 자기 signature 를 가진다([WBFT-MSG-003]). 빈 justification list 는 `0xc0` 으로 encode 한다.
- Source: `consensus/wbft/messages/preprepare.go:32-71`
- Observable: network

[WBFT-MSG-041] PRE-PREPARE decoder 는 schema 와 맞지 않는 입력을 반드시 거부해야 한다. 거부해야 하는 입력에는 다음이 들어간다.
1. `proposal` 이 비어 있거나 block 이 아닌 입력이다. 예를 들어 `0xc0` 으로 encode 된 proposal 은 decode 에 실패한다.
2. justification 이 두 항목 list 가 아닌 입력이다.
3. [WBFT-MSG-034] 에 따라 decode 되지 않는 round-change 항목이다. decoder 는 각 항목을 RLP 항목 하나로 잘라 따로 decode 하므로, 항목 list 에 원소가 빠지거나 더 있으면 그 항목은 decode 에 실패한다.
4. [WBFT-MSG-011] 에 따라 decode 되지 않는 PREPARE 항목이다.

reference 는 모든 실패를 `ErrFailedDecodePreprepare`("failed to decode PRE-PREPARE message")로 보고한다.
- Source: `consensus/wbft/messages/preprepare.go:73-110`, `consensus/wbft/messages/decode.go:29-35`; nil-proposal case checked in §9.6
- Observable: network, log

`sequence = proposal.Number` 검사는 decode 규칙이 아니다. 그 검사는 `A-05` 가 한다.

> 해설: justification list 가 PRE-PREPARE signature 에 들어가지 않으므로, relay 하는 노드는 justification 을 바꿔 붙일 수 있다. 그러나 각 항목은 자기 signature 로 검증되므로, 바꿔 붙일 수 있는 것은 signature 가 맞는 항목뿐이다. 어떤 항목을 붙여야 PRE-PREPARE 가 정당한지는 `A-05` 의 `is_justified` 가 정한다.

### 8.6 Legacy 코드 `0x11`

[WBFT-MSG-050] 노드는 코드가 `ISTANBUL_MSG = 0x11` 인 메시지를 받으면, 반드시 devp2p payload 를 RLP byte string `data` 하나로 decode 해야 하며, 그 뒤에 오는 바이트는 무시한다. decode 에 실패하면 그 메시지는 `errDecodeFailed` 로 거부된다. 이 거부가 peer 에게 가져오는 결과는 `A-07` 이 정한다. decode 에 성공하면 노드는 `data` 에 대해 dedup key 를 계산하고([WBFT-MSG-060]), 그 key 가 이미 알려진 key 가 아니면(`A-07`) `(0x11, data)` 를 합의 코어에 넘긴다. 합의 코어는 그 메시지를 처리해서는 안 된다([WBFT-MSG-004]). `0x11` 메시지는 결코 relay 되지 않는다.
- Source: `consensus/wbft/backend/handler.go:54-67` (decode), `p2p/message.go:56-62` (Msg.Decode, no trailing-byte check), `consensus/wbft/backend/handler.go:70-101` (HandleMsg), `consensus/wbft/core/handler.go:128-135`, `consensus/wbft/core/handler.go:188-194`
- Observable: network, log

[WBFT-MSG-051] 노드는 각 합의 메시지를 반드시 그 메시지 자신의 코드 `0x12 … 0x15` 로 보내야 한다. reference 는 그 밖의 코드를 받으면 payload 를 RLP string 으로 감싸지 않은 채 `0x11` 로 보내는 경로를 가지고 있지만, 그런 메시지를 실제로 만들지는 않는다.
- Source: `consensus/wbft/backend/backend.go:202-206`
- Observable: network

### 8.7 `View` (informative)

reference 는 `View` 의 RLP 인코딩을 `[round, sequence]` 로 정의한다. 즉 round 가 앞에 온다(`consensus/wbft/types.go:68-85`). 이 장의 어떤 메시지도 `View` 를 포함하지 않으며, 메시지는 `sequence` 를 `round` 보다 앞에 싣는다. 이 인코딩은 혼동을 막기 위해서만 여기에 기록한다.

### 8.8 Dedup key

```python
def dedup_key(payload: bytes) -> Hash:
    """wbft.RLPHash(data): keccak256 of the RLP string encoding of the payload bytes."""
    return keccak256(rlp_encode(payload))           # payload 를 RLP *string* 으로 encode 한다
```

[WBFT-MSG-060] 노드가 합의 메시지를 "이미 보았다" 고 기록하는 key 는 반드시 `dedup_key(payload) = keccak256(rlp_encode_string(payload))` 여야 한다. 노드는 이 key 를 메시지 payload 바이트만으로 계산하고, 코드가 `0x11` 이면 안쪽의 `data` 로 계산한다. key 에는 메시지 코드가 들어가지 않는다. 노드는 받은 메시지와 보낸 메시지에 같은 key 를 쓴다.
- Source: `consensus/wbft/utils.go:31-36` (RLPHash), `consensus/wbft/backend/handler.go:66`, `consensus/wbft/backend/backend.go:177`
- Observable: network

길이가 `L` 인 payload `p` 에 대해 `rlp_encode_string(p)` 는 다음과 같다. `L = 1` 이고 `p[0] < 0x80` 이면 `p` 자신이다. `L ≤ 55` 이면 `0x80+L ‖ p` 이다. 그 밖의 경우에는 `0xb7+len(be_min(L)) ‖ be_min(L) ‖ p` 이다. 그러므로 어떤 메시지의 `dedup_key` 는 같은 메시지를 코드 `0x11` 로 보냈을 때의 devp2p payload 에 대한 `keccak256` 과 같다.

key 를 어떻게 쓰는지, 즉 peer 별 cache 와 전역 cache, 그리고 메시지를 언제 본 것으로 표시하는지는 `A-07` 이 정한다.

> 해설: dedup key 는 `keccak256(wire)` 와 다르다. §9.6 의 PREPARE 는 204바이트이므로 앞에 `0xb8 0xcc` 가 붙고, key 는 `7e20…4aec` 이지만 `keccak256(wire)` 는 `8494…7d16` 이다. 구현자는 흔히 `keccak256(wire)` 로 구현하는데, 그러면 reference 노드와 cache 판단이 달라진다. cache 는 노드 내부 동작이므로 호환성이 직접 깨지지는 않지만, 같은 메시지를 다시 보낼지 판단하는 결과가 달라지므로 전송량이 달라진다.

### 8.9 Decode 결과 요약

| 코드 | Decoder | reference 가 보고하는 실패 |
|---|---|---|
| `0x12` | [WBFT-MSG-041] | `ErrFailedDecodePreprepare` |
| `0x13` | [WBFT-MSG-011] | `ErrFailedDecodeCommit` (sic) |
| `0x14` | [WBFT-MSG-020] | `ErrFailedDecodeCommit` |
| `0x15` | [WBFT-MSG-032] | `ErrFailedDecodeRoundChange` |
| `0x11` | [WBFT-MSG-050] | 바이트로 decode 된 뒤 합의 코어가 거부한다 |
| 그 밖의 코드 | 합의 메시지가 아니다 | `ErrInvalidMessage` / `invalid message event code` |

decode 에 실패한 메시지는 버려지고 relay 되지 않는다. 노드는 그 메시지를 Error 수준으로 log 에 남긴다(`WBFT: invalid message`, `consensus/wbft/core/handler.go:197-201`). 노드는 오류 값을 peer 에게 보내지 않는다.

---

## 9. 계산 예

아래의 hex 문자열은 모두 go-stablenet `740526d03` 에 link 한 Go 프로그램에서 reference encoder 와 decoder 를 실행해 만들었다(`A-02 §8` 참조). §9.3 의 block hash 와 §9.6 의 dedup key 는 Python 으로 따로 다시 계산했고, 결과가 일치했다. key 와 header `H` 는 `A-02 §8` 의 것을 쓴다.

### 9.1 `WBFTExtra` 의 없는 값과 decode 실패

| 입력 (hex) | 뜻 | 결과 |
|---|---|---|
| `ca808080c0c080c0c080c0` | `WBFTExtra{}` (모든 값이 없거나 0) | decode 된다. `gas_tip` 은 0 으로 존재한다. seal 과 `epoch_info` 는 없고, `vanity_data` 는 비어 있다. 다시 encode 하면 같은 바이트가 된다 |
| `cc808080c0c080c28080c080c0` | `prepared_seal` 이 존재하고, `sealers` 와 `signature` 가 비어 있다 | decode 된다 (존재하며 비어 있다) |
| `ca808080808080c0c080c0` | `prev_prepared_seal` 자리에 `0x80` 이 있다 | 실패한다: wrong kind of empty value (got String, want List) |
| `ca808080c0c080c0c0c0c0` | `gas_tip` 자리에 `0xc0` 이 있다 | 실패한다: expected input string or byte for `*big.Int` |
| `ca808000c0c080c0c080c0` | `prev_round` 가 `0x00` 이다 | 실패한다: non-canonical integer |
| `ce8080850100000000c0c080c0c080c0` | `prev_round` 가 2^32 이다 | 실패한다: input string too long for uint32 |
| `ca808080c0c080c0c000c0` | `gas_tip` 이 `0x00` 이다 | 실패한다: non-canonical integer |
| `cb808080c0c080c0c08100c0` | `gas_tip` 이 `0x8100` 이다 | 실패한다: non-canonical size information |
| `c9808080c0c080c0c080` | 항목이 9개이다 | 실패한다: too few elements |
| `cb808080c0c080c0c080c080` | 항목이 11개이다 | 실패한다: input list has too many elements |
| `ca808080c0c080c0c080c000` | 유효한 list 뒤에 `00` 이 붙어 있다 | 실패한다: input contains more than one value |

### 9.2 `SealerSet`

| Sealer 인덱스 | `sealers` 바이트 | RLP 항목 | `sealer_indices` |
|---|---|---|---|
| {} | (비어 있음) | `80` | [] |
| {0} | `01` | `01` | [0] |
| {0, 1, 2} | `07` | `07` | [0, 1, 2] |
| {1, 3} | `0a` | `0a` | [1, 3] |
| {0, 8} | `0101` | `820101` | [0, 8] |
| {9} | `0002` | `820002` | [9] |
| {15} | `0080` | `820080` | [15] |
| {16} | `000001` | `83000001` | [16] |

### 9.3 `H` 의 extra, header, block hash

seal 하기 전 proposal `H` 의 extra 는 다음과 같고, 길이는 116바이트이다.

```
f872                                                              list, 114 bytes
  a0 0000000000000000000000000000000000000000000000000000000000000000   vanity_data (32 bytes)
  b841 6243139f0e6b881248e468a6566eb5ed9c278bb1b54d8bd46ebe50055bcb92a5
       435af24253453da96c927a47f9d23f4c19eb2f5b53ead248e93d0182be94c448
       00                                                            randao_reveal (65 bytes, V = 0)
  80                                                                prev_round = 0
  c0                                                                prev_prepared_seal absent
  c0                                                                prev_committed_seal absent
  80                                                                round = 0
  c0                                                                prepared_seal absent
  c0                                                                committed_seal absent
  86 191a20322000                                                   gas_tip = 27600000000000
  c0                                                                epoch_info absent
```

header `H` 의 RLP 는 다음과 같다. 항목은 16개이고, 길이는 628바이트이다.

```
f90271
a0 1111111111111111111111111111111111111111111111111111111111111111   ParentHash
a0 1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347   UncleHash
94 376ab524a47492d1c8222c67c0cff8d5fec34112                           Coinbase
a0 2222222222222222222222222222222222222222222222222222222222222222   Root
a0 56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421   TxHash
a0 56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421   ReceiptHash
b90100 00…00 (256 bytes)                                             Bloom
01                                                                   Difficulty = 1
02                                                                   Number = 2
84 06422c40                                                          GasLimit = 105000000
80                                                                   GasUsed = 0
84 6553f102                                                          Time = 1700000002
b874 f872…c0 (the 116-byte extra above)                              Extra
a0 0000000000000000000000000000000000000000000000000000000000000000   MixDigest
88 0000000000000000                                                  Nonce
86 12309ce54000                                                      BaseFee = 20000000000000
```

`block_hash(H) = keccak256(header RLP) = 6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e` 이다. 아직 seal 하지 않은 round 0 proposal 의 filtered header 는 header 자신과 같기 때문이다.

round 0 에서 validator 0, 1, 2 의 seal 로 `CommitHeader` 를 적용한 뒤 `H` 의 extra 는 다음과 같고, 길이는 317바이트이다.

```
f9013a
  a0 00…00                  vanity_data
  b841 6243…c448 00         randao_reveal (unchanged)
  80 c0 c0                  prev_round, prev seals (unchanged)
  80                        round = 0
  f863 07 b860 895488e1…88164359     prepared_seal: sealers = 07, 96-byte aggregate (A-02 §8.4)
  f863 07 b860 b1d4051b…5fb341       committed_seal
  86 191a20322000           gas_tip
  c0                        epoch_info
```

commit 된 header 의 block hash 도 `6284d1a2…0a1e` 이다. `filtered_header(committed, 0).Extra` 는 proposal 의 extra 와 바이트 단위로 같다.

같은 header 를 바꾸어 가며 한 다른 검사의 결과는 다음과 같다.

| `H` 에 준 변경 | Hash |
|---|---|
| `Difficulty = 2` | `170754e9c004b76a6670c61ac483945b4e247ab089331ecd3c168421340cc9f3` = `keccak256(rlp(header))` (Ethereum 규칙) |
| `Extra` 끝에 `00` 바이트를 붙여 decode 할 수 없게 하고 `Difficulty = 1` 로 둔다 | `f54bd294c2e969aeae6134f0733e394379523e4f0f38136ab48a267cb59fd8c4` = `keccak256(rlp(header))` ([WBFT-ENC-084]) |
| `Extra` 를 비우고 `Difficulty = 1` 로 둔다 (나머지 필드는 0) | `keccak256(rlp(header))` 와 같다 |

### 9.4 `EpochInfo` 와 genesis extra

`Candidate(key0 address, DEFAULT_DILIGENCE)` 는 `d9 94 376ab524a47492d1c8222c67c0cff8d5fec34112 83 1cfde0` 이다(`1_900_000 = 0x1cfde0`).

모든 list 가 비어 있는 `EpochInfo{}` 는 `c3c0c0c0` 이다.

validator key0 … key3 과 `gas_tip = INITIAL_GAS_TIP` 으로 만든 genesis extra 는 다음과 같고, 길이는 330바이트이다.

```
f90147
  80 80 80 c0 c0 80 c0 c0          vanity, randao_reveal, prev_round, prev seals, round, seals: empty/absent
  86 191a20322000                  gas_tip = 27600000000000
  f90135                           epoch_info
    f868                             candidates (4 × 26 bytes)
      d994 376ab524a47492d1c8222c67c0cff8d5fec34112 831cfde0
      d994 1111f6010b9b757b30e00a7d519cd98094b7a610 831cfde0
      d994 503578b72bf5c96ab41508e2ac70303691dfcddb 831cfde0
      d994 084ac57c61cd5dd6244eb0b4c42ca72d073a6c14 831cfde0
    c4 80 01 02 03                   validators = [0, 1, 2, 3]
    f8c4                             bls_public_keys (4 × 49 bytes)
      b0 8eaaca1bbb29c07cb653b346e8434bb6b3bc63a4bb7c8dfea5c46bae2473646fe8041e3ff81da70bba7effda935a4576
      b0 8a8f913a1bac306b3b1661fc78ce376a650341ce314f7c0f615fc7abf3722eb6c256046483c266d3e03ce82edbed13a3
      b0 976a4cd50f07ad6fb3fc0a7a27a803212be7cf84b779d9a5562af81316187dec8ee319a0ce421c020dc1bf1faa494abe
      b0 aa6b0cda839b65a5e2e556484ccb3d67fac59c1cc66ab2bf1b9160ecee30a0deb6dbbad04836ca99ceb74c225a0c184e
```

### 9.5 View

`View{round = 1, sequence = 2}` 는 `c20102` 로 encode 된다. round 가 앞에 오며, 이 인코딩은 wire 에서 쓰이지 않는다.

### 9.6 메시지

아래 예제는 모두 sequence 2 와 digest `block_hash(H) = 6284d1a2…0a1e` 를 쓴다.

**PREPARE**: key 1 이 round 0 에서 보낸다. `prepare_seal` 은 `A-02 §8.4` 의 prepare seal 1 이다.

```
signing payload (138 bytes):
f888 13 f885 02 80 a0 6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e
          b860 b9ed2c8ca8ef593fa5b1749082a5bc211460b913a9c5a3eec8770d5acff6bf6501dfa61735f6cdd045e5e75a9489801a
               0bca5554be5abae320e5d883dae61838ad5765c1427cb4a5672dabd5649969760505ff7aaee60aa2e2e5a623af1726a1
signature = ecdsa_sign(key1, payload):
210a2b4c90e9be64976a7b81aab05aad51146d6abc3bd0061f7a35af8290e6ae31038a77129d7441c68ade4ea87372a481e343a794b711f3cb8ff5b4d6b7bd5501
wire (204 bytes):
f8ca f885 02 80 a0 6284…0a1e b860 b9ed…26a1  b841 210a…bd55 01
dedup_key = keccak256(b8cc ‖ wire) = 7e20c27fc270ed22dfe8b8ee9dbc97f2b9672cf8fd6dc360fbb0321e7f4d4aec
keccak256(wire)                    = 849415fd2fa5eac21fe4db0460f5d78f70ec241f027a15a41f9fbd3ceaa97d16   (not the dedup key)
recovered sender = 0x1111F6010b9b757b30e00A7D519cd98094b7A610
```

wire 전체의 hex 는 다음과 같다: `f8caf8850280a06284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1eb860b9ed2c8ca8ef593fa5b1749082a5bc211460b913a9c5a3eec8770d5acff6bf6501dfa61735f6cdd045e5e75a9489801a0bca5554be5abae320e5d883dae61838ad5765c1427cb4a5672dabd5649969760505ff7aaee60aa2e2e5a623af1726a1b841210a2b4c90e9be64976a7b81aab05aad51146d6abc3bd0061f7a35af8290e6ae31038a77129d7441c68ade4ea87372a481e343a794b711f3cb8ff5b4d6b7bd5501`

**COMMIT**: key 2 가 round 0 에서 보낸다. `commit_seal` 은 `A-02 §8.4` 의 commit seal 2 이다.

```
signing payload: f888 14 f885 02 80 a0 6284…0a1e b860 a0b37939…23164
wire: f8caf8850280a06284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1eb860a0b379390308e1abca3dd3fda9b8ca2bb97bec9d21a273d715f98e0d9571ca25872c717ec95185377bf4ff56ad37a273163b0429116df980359598028a79e7dbadec4e9580d8a6111e2106c2e8081de928cd898e1db0757fb65f98a264323164b84117e43ecb53f6a2b39b150e4b3e9604f74f16a6540b9bb65383fd98e401323baf27e323bf21ecd5e0df8503a5eeea973e7e92f96a0271d4edf832163963ead9a101
```

**ROUND-CHANGE**: key 3 이 round 1 에 대해 보내며, prepared 된 것이 없다.

```
signing payload: c5 15 c3 02 01 c0
wire: f84b f847 c30201c0 b841 789095e9d1a82b67dccb9490c515938a6e26fa3af3ba7484f287604b7e32e5c931fea427d585be99670a799d12ae59c22b1eb5bb337ceef815aa3108912417dc00 c0 c0
```

**ROUND-CHANGE**: key 3 이 round 1 에 대해 보낸다. round 0 에서 block `H` 가 prepared 되었고, 위의 PREPARE 를 justification 으로 붙인다.

```
signing payload: e7 15 e5 02 01 e2 80 a0 6284d1a2d0a027e77a8249b3ab24207ef68a2f5b98236cc4b044b6aaf5a10a1e
wire: 949 bytes = [ [rc_payload, sig], block(H, no txs), [PREPARE] ]; decodes without error
```

ROUND-CHANGE decode 경계 사례는 다음과 같다. 서명된 payload 는 `c6 c30201c0 81aa` 이다. 즉 `rc_payload = [2, 1, []]` 이고 signature 는 `aa` 이다.

| Wire | 결과 |
|---|---|
| `c9 c6c30201c081aa c0 c0` | 받아들인다. 다시 encode 하면 같은 바이트가 된다 |
| `c9 c6c30201c081aa 80 80` | 받아들인다 (둘 다 없음으로 본다). 다시 encode 하면 `c0 c0` 이 된다 |
| `c9 c6c30201c081aa 05 00` | 받아들인다 (한 바이트 값을 없음으로 본다). 다시 encode 하면 `c0 c0` 이 된다 |
| `c8 c6c30201c081aa c0` | 실패한다 (셋째 항목이 없다) |
| `ca c6c30201c081aa c0 c0 c0` | 실패한다 (항목이 하나 더 있다) |
| `prepared = c180` (항목 하나) | 실패한다 |
| `prepared = [0, 0x00…00, 1]` (항목 셋) | 실패한다 |
| `prepared = [0, 0x00…00]` | 받아들인다. signing payload 는 `c515c30201c0` 으로 다시 encode 된다 ([WBFT-ENC-090]) |
| `prepared = [1, 0x00…01]` | 받아들인다. signing payload 는 `e715e50201e201a0…01` 이다 |
| prepared block 이 있고 `prepared_digest ≠ block_hash` 이다 | 실패한다 (`ErrFailedDecodeRoundChange`) |

**PRE-PREPARE**: key 0 이 `(2, 0)` 에 대해 보낸다. proposal 은 transaction 이 없는 block `H` 이고, justification 은 비어 있다. 전체 길이는 714바이트이다.

```
f902c7
  f902c1                       [ [seq, round, proposal], signature ]
    f9027b 02 80 f90276 <header H: f90271…> c0 c0      signed fields: 2, 0, block = [header, [], []]
    b841 3a5f2bea6f18e89d704efb5ac54790314b24bdd0ef1121de7ad5ec7f68f1199f7da8d2981c15a295929cdd93792580dd29076e34f0e25b8f1b154654c57b13df01
  c2 c0 c0                     justification: no round changes, no prepares
signing payload (642 bytes) starts f9027f 12 f9027b 02 80 f90276 f90271 a0…
```

proposal 이 없는 PRE-PREPARE 는 `c9 c5 c3 02 80 c0 80 c2 c0 c0` 로 encode 되며, decode 에 실패한다(`ErrFailedDecodePreprepare`).
