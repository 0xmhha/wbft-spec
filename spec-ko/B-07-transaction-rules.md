# B-07 Anzeon transaction 규칙

- Part: B (StableNet block 유효성)
- Area code: `TX`
- Status: draft
- Reference: go-stablenet `740526d03`

이 장은 Anzeon rule set 이 켜진 StableNet 노드가 transaction 을 어떻게 실행하는지 정한다. 이 장은 upstream go-ethereum 의 실행과 다른 점만 적는다. 여기서 upstream 실행은 이 포크가 기반으로 삼은 upstream 코드의 London rule set 을 뜻한다. 여기서 언급하지 않은 동작은 같은 코드 기반의 upstream go-ethereum 과 똑같다. 이 장이 다루는 것은 계정별 flag 비트(blacklist, authorized), 거버넌스 gas tip override, transaction 별 수수료 계산, fee delegation transaction type, Anzeon 이 추가한 receipt 와 log, 그리고 state, receipt, log 를 바꾸는 그 밖의 EVM 차이다.

block 수준의 수수료 처리(base fee 를 validator 에게 분배하는 일과 gas tip header 검사)는 `B-06` 에 있다. 계정 비트를 설정하는 system contract(GovCouncil)와 gas tip 을 설정하는 system contract(GovValidator)는 `B-04` 와 `B-05` 에 있다. header 와 body 의 유효성(gas limit, base fee, 금지된 필드)은 `B-03` 에 있다. fork 활성화와 Anzeon 설정 객체는 `B-01` 에 있다.

---

## 1. 범위와 conformance

### 1.1 무엇이 규범인가

transaction 의 유효성과, 그 transaction 이 만드는 state, receipt, log 는 규범이다. 이 값들이 `Header.Root`, `Header.ReceiptHash`, `Header.Bloom`, `Header.GasUsed` 를 통해 commit 되기 때문이다 (`B-03`). 이 장의 규칙으로 무효인 transaction 을 담은 block 은 무효이고, conforming 노드는 모두 반드시 그 block 을 거부해야 한다 (`SNET-TX-060` 참고).

오류의 종류와 오류 문자열은 block 유효성에 대해서는 규범이 아니다. conforming 노드는 block 을 거부하기만 하면 된다. 그래도 이 장은 §13 에 오류를 나열한다. 오류가 `eth_call`, `eth_estimateGas`, `eth_sendRawTransaction` 으로 관찰되기 때문이다.

block 안에서 transaction 을 어떤 순서로 놓을지와 transaction pool 이 어떤 transaction 을 받아들일지는 각 노드의 로컬 정책이 정한다. 이 두 가지는 informative 절인 §11 과 §12 에서 설명한다.

### 1.2 Rule set 적용 조건

| Rule set | 참조 구현에서의 조건 | 적용 대상 |
|---|---|---|
| Anzeon | `ChainConfig.Anzeon != nil` 이다. 이 조건은 block 번호가 아니라 설정 객체가 있는지를 본다. 이런 체인에서는 모든 height 에서 `Rules.IsAnzeon` 이 참이다 | 따로 적지 않은 한 이 장의 모든 규칙 |
| Applepie | `number >= ChainConfig.ApplepieBlock` | sender 와 다른 fee payer 를 쓰는 fee delegation (§5) |
| Boho | Anzeon 이 켜져 있고 `number >= ChainConfig.BohoBlock` 이다 | P-256 precompile (§9.4), GovMinter v2 upgrade (`B-04`) |

Source: params/config.go:1037-1044 (IsApplepie, IsBoho), params/config.go:1085-1087 (AnzeonEnabled), params/config.go:1512-1540 (Rules)

유효한 WBFT 체인에서는 header difficulty 가 1 이다 (`A-08`). 그래서 `Rules.IsMerge` 가 거짓이고, 설정한 timestamp 와 상관없이 모든 height 에서 `Rules.IsShanghai`, `Rules.IsCancun`, `Rules.IsPrague` 도 거짓이다. WBFT header 검증은 `ChainConfig.IsShanghai` 나 `ChainConfig.IsCancun` 이 참인 header 도 거부한다. Anzeon 에 필요한 upstream 기능은 `IsAnzeon` 이 직접 켠다 (§9).

Source: params/config.go:1518-1539, consensus/wbft/engine/engine.go:220-228

[SNET-TX-001] 설정에 Anzeon 객체가 있는 체인에서 노드는 height 1 을 포함한 모든 block height 에서 반드시 이 장의 Anzeon 규칙을 적용해야 한다.
Source: params/config.go:1519-1533
Observable: state

### 1.3 표기

의사코드는 `README.md §2.4` 를 따른다. 이 장은 다음 helper 를 쓴다.

```python
BLACKLISTED = 1 << 63
AUTHORIZED  = 1 << 62

def account_extra(state, addr) -> uint64:        # 계정이 없으면 0
def is_blacklisted(state, addr) -> bool: return account_extra(state, addr) & BLACKLISTED != 0
def is_authorized(state, addr)  -> bool: return account_extra(state, addr) & AUTHORIZED  != 0

def header_gas_tip(header) -> Optional[uint256]:
    # 실행 중인 block 의 WBFTExtra.GasTip (A-03). extra 가 WBFTExtra 로 decode 되지
    # 않으면 None 이다. (참조 구현은 GasTip 필드가 nil 인지도 검사하지만, 그런 경우는
    # 생기지 않는다. decode 된 GasTip 은 비어 있지 않다, A-03 WBFT-ENC-008.)
```

따로 적지 않은 한 `state` 는 항상 검사하는 순간의 state 를 뜻한다. 곧 같은 block 의 앞선 transaction 과 같은 transaction 의 앞선 단계를 모두 적용한 뒤의 state 다.

Source: core/types/block.go:111-117 (Header.GasTip), core/state/statedb.go:300-326

---

## 2. 계정 Extra 비트

### 2.1 인코딩

state 계정에는 다섯 번째 필드 `Extra` 가 추가된다. `Extra` 는 `uint64` 이고, 선택적인 마지막 RLP 원소로 인코딩된다. `Extra == 0` 이면 계정은 `[nonce, balance, storageRoot, codeHash]` 로 인코딩되고, 그렇지 않으면 `[nonce, balance, storageRoot, codeHash, Extra]` 로 인코딩된다. snapshot 의 "slim" 계정에도 같은 선택 필드가 있다. 그 밖의 account trie storage 배치는 바뀌지 않는다. trie 인코딩을 정하는 장은 `B-04` §11 이다.

[SNET-TX-002] 계정의 `Extra` 필드는 반드시 `B-04` SNET-SYS-070 이 정한 대로 인코딩해야 한다. 곧 `Extra` 는 선택적인 마지막 RLP 원소이고, 값이 0 이면 인코딩에서 빠진다. 이 requirement 는 참조이며, 규범 문장은 `B-04` 에 있다.
Source: core/types/state_account.go:31-38, core/types/state_account.go:73
Observable: state, header

[SNET-TX-003] 노드는 반드시 `B-04` SNET-SYS-071 이 정한 대로 `Extra` flag 를 해석해야 한다. 비트 63 은 *blacklisted* flag 이고 비트 62 는 *authorized* flag 다. 노드는 또 `B-04` SNET-SYS-013 이 정한 대로, 정의되지 않은 비트 0-61 가운데 하나라도 켠 genesis allocation 을 반드시 거부해야 한다. 이 requirement 는 참조이며, 규범 문장은 `B-04` 에 있다.
Source: core/types/state_account_extra.go:31-46, core/types/state_account_extra.go:101-107, core/genesis.go:257
Observable: state

[SNET-TX-004] 노드는 반드시 `B-04` SNET-SYS-072 가 정한 대로, 기본값이 아닌 필드가 0 이 아닌 `Extra` 하나뿐인 계정을 EIP-161 state clearing 에서 비어 있지 않은 계정으로 다뤄야 한다. 이 requirement 는 참조이며, 규범 문장은 `B-04` 에 있다.
Source: core/state/state_object.go:94-96
Observable: state

[SNET-TX-005] 빈 계정에서 켜져 있지 않은 flag 를 끄는 호출은 반드시 그 계정을 touch 해야 한다. 그래야 EIP-161 clearing 이 transaction 끝에서 그 계정을 지운다. 그 밖의 경우 flag 를 끄거나 켜는 호출은 반드시 대상 비트만 바꿔야 한다.
Source: core/state/state_object.go:524-564
Observable: state

> 해설: `Extra` 만 0 이 아닌 계정이 비어 있지 않은 계정이므로, 존재하지 않던 주소를 blacklist 하면 그 주소의 계정이 새로 생긴다. 반대로 켜지지 않은 비트를 지우는 호출은 계정을 touch 하므로, 그 계정이 비어 있으면 transaction 끝에서 지워진다. state root 에 계정이 있는지 없는지가 이 규칙에 달려 있다. 그래서 계정 storage 계층을 새로 쓰는 구현은 "빈 계정" 판정에 `Extra == 0` 조건을 넣어야 한다.

### 2.2 의미 요약

| Flag | 설정하는 주체 | 실행에서의 효과 | 절 |
|---|---|---|---|
| blacklisted | GovCouncil 만 부르는 `AccountManager.blacklist(addr)`, 그리고 genesis | 그 계정은 transaction 을 보낼 수 없고, 최상위 수신자가 될 수 없고, 수수료를 낼 수 없고, EVM 안에서 호출되거나 호출할 수 없고, contract 를 만들 수 없고, SELFDESTRUCT 수혜자가 될 수 없다 | §6 |
| authorized | GovCouncil 만 부르는 `AccountManager.authorize(addr)`, 그리고 genesis | 거버넌스 gas tip 대신 그 계정 자신의 `maxPriorityFeePerGas` 를 쓴다. 그 계정의 transaction 은 `AuthorizedTxExecuted` 를 낸다 | §3, §8.2 |

두 flag 는 서로 독립이므로 한 계정이 둘 다 가질 수 있다. 실행 규칙 가운데 blacklisted 이면서 authorized 인 계정에 예외를 주는 것은 없다.

---

## 3. Gas tip override

### 3.1 Message 구성

transaction 은 실행되기 전에 실행 message 로 바뀐다. Anzeon 은 그 message 의 tip cap 을 고르는 방식을 바꾼다.

```python
def to_message(tx, header, state, signer) -> Message:
    sender = recover_sender(signer, tx)                 # upstream 규칙; type 0x16 은 §4
    tip_cap = tx.gas_tip_cap                            # legacy/2930: = tx.gas_price
    g = header_gas_tip(header)
    if g is not None and not is_authorized(state, sender):
        tip_cap = g                                     # 거버넌스 tip 이 사용자 값을 대신한다
    gas_price = min(tip_cap + header.base_fee, tx.gas_fee_cap)   # legacy/2930: gas_fee_cap = tx.gas_price
    fee_payer = None
    if tx.type == FEE_DELEGATE_DYNAMIC_FEE_TX_TYPE:     # 0x16
        fee_payer = recover_fee_payer(tx, chain_id)     # §4.2 (fee payer signature); 실패하면 예외
    return Message(from_=sender, gas_tip_cap=tip_cap, gas_fee_cap=tx.gas_fee_cap,
                   gas_price=gas_price, fee_payer=fee_payer, ...)
```

Source: core/state_transition.go:159-205, core/state_processor.go:84

[SNET-TX-010] 모든 transaction 에 대해, `header_gas_tip(header)` 가 `None` 이 아니고 sender 가 transaction 직전 state 에서 authorized 가 아니면, 노드는 반드시 그 transaction 의 tip cap 을 `header_gas_tip(header)` 로 바꾸고, 그 transaction 의 이후 모든 검사와 수수료 계산에 바꾼 값을 써야 한다.
Source: core/state_transition.go:165-172, core/state_processor.go:84
Observable: state

[SNET-TX-011] sender 가 transaction 직전 state 에서 authorized 이면, 노드는 반드시 그 transaction 자신의 tip cap 을 써야 한다. legacy transaction 과 access-list transaction 에서는 gas price 가 tip cap 역할을 한다.
Source: core/state_transition.go:165-172
Observable: state

[SNET-TX-012] message gas price 는 반드시 `min(tip_cap + base_fee, gas_fee_cap)` 이어야 한다. 여기서 `tip_cap` 은 `SNET-TX-010`/`SNET-TX-011` 이 고른 값이다. 이 규칙은 legacy transaction 과 access-list transaction 에도 적용되며, 이때 `gas_fee_cap` 은 transaction 의 gas price 다.
Source: core/state_transition.go:189-192, core/types/tx_legacy.go:101-102
Observable: state, rpc

override 조건은 `IsAnzeon` 이 아니라 "header 의 extra 가 decode 되고 gas tip 을 담고 있다" 는 것이다. 유효한 WBFT 체인에서는 header 검증이 gas tip 을 GovValidator 값과 같도록 요구하므로 (`B-06`) gas tip 이 항상 있고, 그래서 두 조건은 같은 결과를 낸다.

upstream go-ethereum 은 London 이후 legacy transaction 에 선언한 gas price 전부를 청구한다. legacy transaction 에서는 `tip_cap = fee_cap = gas_price` 이기 때문이다. `SNET-TX-012` 아래에서 authorized 가 아닌 legacy transaction 은 `min(gas_tip + base_fee, gas_price)` 를 내고, 이 값은 대개 선언한 gas price 보다 훨씬 작다. §10 의 예 E-4 를 참고한다.

### 3.2 Override 된 tip 을 보는 검사

upstream London 사전 검사는 message 위에서, 곧 override 뒤에 실행된다.

[SNET-TX-013] fee cap 이 `SNET-TX-010`/`SNET-TX-011` 이 고른 tip cap 보다 작으면 그 transaction 은 반드시 무효여야 한다 (`ErrTipAboveFeeCap`). authorized 가 아닌 sender 에게 이 규칙은, transaction 자신의 tip cap 이 더 작더라도 `gas_fee_cap >= header_gas_tip(header)` 가 필요하다는 뜻이다.
Source: core/state_transition.go:385-388
Observable: state

### 3.3 Effective tip

```python
effective_tip = min(msg.gas_tip_cap, msg.gas_fee_cap - base_fee)    # >= 0 (gas_fee_cap >= base_fee)
# 참고: msg.gas_price == base_fee + effective_tip
```

Source: core/state_transition.go:563-567

[SNET-TX-015] transaction 의 effective tip 은 반드시 override 뒤의 message tip cap 으로 `min(tip_cap, gas_fee_cap - base_fee)` 와 같이 계산해야 한다.
Source: core/state_transition.go:563-567
Observable: state

EVM 의 `GASPRICE` opcode 는 message gas price 를 돌려준다. 그래서 contract 는 override 된 가격을 본다. `BASEFEE` 는 upstream 과 같이 header base fee 를 돌려준다.

Source: core/evm.go:81-92 (NewEVMTxContext: GasPrice = msg.GasPrice)

---

## 4. Transaction type 과 signature

### 4.1 받아들이는 type

Anzeon 체인에서 block 을 실행할 때 쓰는 signer 는 *Anzeon signer* 다. Cancun signer 는 `IsCancun` 일 때만 선택되는데, WBFT header 는 `IsCancun` 을 금지한다.

| Type 바이트 | 이름 | block 에서 받아들이는가 | Sender signature |
|---|---|---|---|
| (없음, RLP list) | Legacy | 받아들인다 (EIP-155 보호 여부와 무관하다) | upstream |
| `0x01` | EIP-2930 access list | 받아들인다 | upstream |
| `0x02` | EIP-1559 dynamic fee | 받아들인다 | upstream |
| `0x03` | EIP-4844 blob | 받아들이지 않는다. sender recover 가 `ErrTxTypeNotSupported` 로 실패한다 | — |
| `0x04` | EIP-7702 set code | 받아들인다 (Anzeon 이 Prague 없이 켠다) | SNET-TX-091 (§4.3) |
| `0x16` | Fee-delegated dynamic fee | 받아들인다. sender 와 다른 fee payer 는 Applepie 부터만 허용한다 (§5.1) | §4.2 |

Source: core/types/transaction_signing.go:46-65 (MakeSigner), core/types/transaction_signing.go:286-339 (anzeonSigner), core/types/transaction_signing.go:403-415 (londonSigner handles 0x02 and 0x16), core/types/transaction_signing.go:468-484 (eip2930Signer rejects other types), core/types/transaction.go:48-53

[SNET-TX-020] Anzeon 체인에서 type `0x03` transaction 을 담은 block 은 반드시 무효여야 한다.
Source: core/types/transaction_signing.go:51-52, core/types/transaction_signing.go:298-301, core/types/transaction_signing.go:468-479, core/state_processor.go:84-87
Observable: header

[SNET-TX-021] Anzeon 체인에서 노드는 `PragueTime` 과 상관없이 반드시 type `0x04` transaction 을 받아들이고, EIP-7702 의미(authorization 처리, delegation designator, 한 단계 code 해석)로 실행해야 한다. 그 규칙은 SNET-TX-091 부터 SNET-TX-094 (§4.3) 이다.
Source: core/types/transaction_signing.go:286-339, core/state_transition.go:529-548, core/vm/evm.go:628-656, core/vm/jump_table.go:141
Observable: state

[SNET-TX-095] block body 에서 decode 되는 transaction 인코딩은 반드시 정확히 legacy (RLP list) 와 typed transaction `0x01`, `0x02`, `0x03`, `0x04`, `0x16` 이어야 한다. body 에 그 밖의 type 바이트가 있는 block 은 decode 되지 않으므로, 노드는 반드시 그 block 을 실행하지 않고 거부해야 한다. decode 된 type-`0x03` transaction 은 block 을 무효로 만든다 (SNET-TX-020).
Source: core/types/transaction.go:219-241 (decodeTyped)
Observable: header

### 4.2 Fee-delegated dynamic fee transaction (type `0x16`)

#### 인코딩

payload 는 다음 RLP list 다.

```
0x16 || rlp([
    [chain_id, nonce, max_priority_fee_per_gas, max_fee_per_gas, gas, to, value, data, access_list, v, r, s],   # SenderTx
    fee_payer,        # 20 바이트 주소; 빈 문자열은 "설정 안 됨" 으로 decode 된다
    fv, fr, fs        # fee payer signature
])
```

안쪽 list 는 signature 를 포함한 type-`0x02` transaction 의 필드 순서와 정확히 같다. `to` 는 `rlp:"nil"` 관례를 따른다 (contract 생성이면 빈 문자열이다).

Source: core/types/tx_fee_delegation.go:27-34, core/types/tx_fee_delegation.go:152-157, core/types/tx_dynamic_fee.go (DynamicFeeTx field order)

transaction hash 는 `keccak256(0x16 || rlp(payload))` 다. 이 hash 는 typed transaction 의 표준 규칙을 전체 구조에 적용해 얻으므로, 두 signature 를 모두 덮는다.

Source: core/types/transaction.go:561-574

접근자는 안쪽 필드를 드러낸다. `gas_tip_cap`, `gas_fee_cap`, `gas`, `to`, `value`, `data`, `access_list`, `nonce`, `chain_id` 는 `SenderTx` 의 값이고, `gas_price()` 는 `max_fee_per_gas` 를 돌려준다.

Source: core/types/tx_fee_delegation.go:117-131

#### Sender signature

#### Fee payer signature

fee payer 는 type-`0x16` signing hash 에 서명한다.

```python
def fee_payer_sighash(tx, chain_id):
    s = tx.SenderTx
    return keccak256(b"\x16" + rlp([
        [chain_id, s.nonce, s.max_priority_fee_per_gas, s.max_fee_per_gas, s.gas, s.to,
         s.value, s.data, s.access_list, s.v, s.r, s.s],
        tx.fee_payer,
    ]))
```

Source: core/types/tx_fee_delegation.go:159-179

```python
def recover_fee_payer(tx, chain_id) -> Address:
    if tx.fee_payer is None:
        raise ErrFeePayerNotSet                    # "fee delegation: feePayer not set"
    if tx.SenderTx.chain_id != chain_id:
        raise ErrInvalidFeePayer
    # recover_from_digest: 이미 hash 된 32 바이트 digest 에서 secp256k1 recover 를 하며,
    # homestead low-S 규칙을 적용한다. A-02 ecdsa_recover_address 와 달리 입력을 hash 하지 않는다
    addr = recover_from_digest(fee_payer_sighash(tx, chain_id), tx.fr, tx.fs, tx.fv + 27)
    if addr is error or addr != tx.fee_payer:
        raise ErrInvalidFeePayer                   # "fee delegation: invalid feePayer"
    return addr
```

Source: core/types/transaction_signing.go:187-198, core/types/transaction_signing.go:348-360

### 4.3 Set-code transaction (type `0x04`)

이 fork 의 기준 코드(go-ethereum v1.13.15)에는 EIP-7702 가 없다. 그래서 "upstream 과 같다" 는 문장으로는 type `0x04` 가 정의되지 않는다. go-stablenet 은 go-ethereum v1.15 (Prague, 최종 EIP-7702)의 구현을 가져와서 `IsPrague` 대신 `IsAnzeon` 으로 켠다. 아래 규칙은 그 판본을 고정한다. Prague 의 다른 규칙(EIP-7623 calldata floor, EIP-2537, EIP-2935, EIP-7685)은 켜지지 않는다 (`B-01` SNET-CFG-029).

[SNET-TX-091] type-`0x04` transaction 은 반드시 최종 EIP-7702 (go-ethereum v1.15 가 구현한 판본)대로 인코딩되고 서명되고 검사되어야 한다. payload 는 `rlp([chain_id, nonce, max_priority_fee_per_gas, max_fee_per_gas, gas, to, value, data, access_list, authorization_list, y_parity, r, s])` 이고 `to` 는 20 바이트다. 그래서 contract 생성은 할 수 없다. authorization 하나는 `rlp([chain_id, address, nonce, y_parity, r, s])` 이고, `chain_id < 2^256`, `nonce < 2^64`, `y_parity < 2^8` 을 어기면 transaction 이 decode 되지 않는다. sender 는 `keccak256(0x04 || rlp([chain_id, …, authorization_list]))` 에 `v ∈ {0, 1}`, low-S 규칙, chain id 와 같은 `chain_id` 로 서명한다. `authorization_list` 가 빈 transaction 은 반드시 무효여야 한다 (`ErrEmptyAuthList`).
Source: core/types/tx_setcode.go:51-79, core/types/tx_setcode.go:234-250
Source: core/types/transaction_signing.go:298-310 (anzeonSigner.Sender)
Source: core/state_transition.go:429-437 (ErrSetCodeTxCreate, ErrEmptyAuthList)
Observable: state

[SNET-TX-092] type-`0x04` transaction 의 intrinsic gas 는 반드시 London intrinsic gas에 authorization tuple 하나당 25 000 을 더한 값이어야 한다. Anzeon 체인에서는 어떤 transaction 에도 EIP-7623 calldata floor 를 적용해서는 안 된다.
Source: core/state_transition.go:71-121 (IntrinsicGas), core/state_transition.go:482 (called with IsShanghai = false)
Observable: state

[SNET-TX-093] 노드는 반드시 type-`0x04` transaction 을 다음 순서로 처리해야 한다. 먼저 intrinsic gas 와 SNET-TX-040/041 의 검사를 하고, access list 를 준비하고 (SNET-TX-084), sender nonce 를 올린다. 그 뒤 authorization 을 목록 순서대로 아래와 같이 처리하고, 모든 authorization 뒤에 `to` 의 code 가 designator 이면 그 위임 대상을 gas 없이 warm 한 다음, call 을 실행한다. 한 단계에서 실패한 authorization 은 건너뛰며, 그 때문에 transaction 이 무효가 되지는 않는다. 단계는 다음과 같다. (1) `chain_id` 가 0 이거나 chain id 와 같다. (2) `nonce < 2^64 − 1` 이다. (3) `keccak256(0x05 || rlp([chain_id, address, nonce]))` 에 대한 서명이 `y_parity ∈ {0, 1}`, `1 ≤ r, s < n`, `s ≤ n/2` 를 지키고 authority 를 복원한다. (4) authority 를 EIP-2929 access list 에 넣는다. 뒤 단계가 실패해도 authority 는 access list 에 남는다. (5) authority 의 code 가 비어 있거나 delegation designator `0xef0100 || addr` 이다. (6) authority 의 nonce 가 `nonce` 와 같다. 모든 단계를 통과한 authorization 에 대해, 노드는 반드시 authority 가 state 에 있으면 refund counter 에 12 500 을 더하고, authority 의 nonce 를 `nonce + 1` 로 두고, `address` 가 zero address 이면 code 를 비우고 아니면 code 를 `0xef0100 || address` 로 두어야 한다. 이 12 500 을 포함한 refund counter 는 EIP-3529 상한(`gas_used / 5`)을 받는다. sender nonce 를 먼저 올리므로, sender 자신이 서명한 authorization 은 `nonce = tx.nonce + 1` 일 때만 통과한다.
Source: core/state_transition.go:505-551, core/state_transition.go:609-665
Source: params/protocol_params.go:35, params/protocol_params.go:96 (CallNewAccountGas, TxAuthTupleGas)
Observable: state

[SNET-TX-094] 노드는 반드시 delegation designator 를 최종 EIP-7702 대로 해석해야 한다. EIP-3607 의 sender 검사는 code 가 designator 인 sender 를 받아들여야 한다. `CALL`, `CALLCODE`, `DELEGATECALL`, `STATICCALL` 과 최상위 call 은 위임된 주소의 code 를 실행하되 한 단계만 따라가야 한다. 그 주소에서 다시 designator 를 만나면 그 바이트를 code 로 실행하므로 `0xef` 에서 멈춘다. 그래서 precompile 이나 native manager 로 위임하면 아무 code 도 실행되지 않는다. `EXTCODESIZE`, `EXTCODECOPY`, `EXTCODEHASH` 는 23 바이트 designator 자체를 대상으로 동작해야 한다. 네 call opcode 는 대상에 대한 EIP-2929 비용에 더해, 위임된 주소가 warm 이면 100, cold 이면 2 600 gas 를 청구하고 그 주소를 warm 해야 한다. 두 비용은 모두 EIP-150 63/64 규칙을 적용하기 전에 뺀다.
Source: core/state_transition.go:365-370
Source: core/vm/evm.go:267, core/vm/evm.go:275, core/vm/evm.go:337, core/vm/evm.go:386, core/vm/evm.go:444, core/vm/evm.go:628-656 (resolveCode, resolveCodeHash)
Source: core/vm/operations_acl.go:252-317, core/vm/eips.go:325-330, core/vm/instructions.go:345-349
Observable: state

---

## 5. 수수료 지불과 잔액 검사

### 5.1 누가 내는가

```python
def payer(msg):
    delegated = msg.fee_payer is not None and msg.fee_payer != msg.from_
    return msg.fee_payer if delegated else msg.from_
```

[SNET-TX-030] message 에 sender 와 다른 fee payer 가 있고 block 번호가 `ApplepieBlock` 보다 작으면, 그 transaction 은 반드시 무효여야 한다 (`ErrTxTypeNotSupported: fee delegation type not supported`).
Source: core/state_transition.go:268-271
Observable: state

fee payer 가 sender 와 같은 type-`0x16` transaction 은 이 의미에서 "delegated" 가 아니다. 그런 transaction 은 Applepie 이전에도 받아들여지고, type-`0x02` transaction 과 정확히 똑같이 청구된다.

### 5.2 잔액 검사 (buyGas)

`gas_limit` 은 transaction 의 gas, `gp` 는 message gas price (`SNET-TX-012`), `fc` 는 fee cap, `v` 는 value 다.

| 경우 | 검사하는 계정 | 필요한 잔액 | 오류 |
|---|---|---|---|
| delegated 가 아님 | sender | `gas_limit * fc + v` | `ErrInsufficientFunds` (sender) |
| delegated | fee payer | `gas_limit * gp` | `ErrInsufficientFunds` (feePayer) |
| delegated | sender | `v` | `ErrInsufficientFunds` (sender) |

노드는 어느 경우든 지불자에게서 `gas_limit * gp` 를 뺀다.

Source: core/state_transition.go:278-346

[SNET-TX-031] delegated message 에 대해 노드는 반드시 fee payer 의 잔액이 `gas_limit * gas_price` 이상인지와 sender 의 잔액이 `value` 이상인지를 따로 검사해야 한다. 여기서 `gas_price` 는 fee cap 이 아니라 effective price 다. 둘 중 하나라도 실패하면 그 transaction 은 반드시 무효여야 한다.
Source: core/state_transition.go:281-287, core/state_transition.go:315-336
Observable: state

[SNET-TX-032] delegated 가 아닌 message 에 대해 노드는 upstream 과 같이 반드시 sender 의 잔액이 `gas_limit * gas_fee_cap + value` 이상이도록 요구해야 한다.
Source: core/state_transition.go:281-287, core/state_transition.go:304-314
Observable: state

### 5.3 환불

[SNET-TX-034] 실행이 끝나면 노드는 반드시 `gas_remaining * gas_price` 를 돌려줘야 한다. 이때 `gas_remaining` 은 EIP-3529 환불 상한을 적용한 뒤의 값이다. message 에 fee payer 가 있으면 노드는 fee payer 에게 돌려주고, 없으면 sender 에게 돌려준다. fee payer 가 sender 와 같아도 message 에 fee payer 가 있는 경우로 본다.
Source: core/state_transition.go:667-691
Observable: state

---

## 6. Blacklist 적용

### 6.1 Transaction 수준

아래 검사는 state transition 안에서 nonce, 수수료, 잔액 검사와 intrinsic gas 계산 뒤, EVM 실행 전에 돈다. 이 검사가 실패하면 transaction 은 무효이므로 block 에 들어갈 수 없다.

[SNET-TX-040] sender 가 transaction 직전 state 에서 blacklisted 이면 그 transaction 은 반드시 무효여야 한다.
Source: core/state_transition.go:505-510
Observable: state

[SNET-TX-041] `to` 가 nil 이 아닌 transaction 은 `to` 가 transaction 직전 state 에서 blacklisted 이면 반드시 무효여야 한다. contract 생성 transaction 에는 수신자 검사가 없다.
Source: core/state_transition.go:512-515
Observable: state

### 6.2 EVM 수준

EVM 안의 검사는 transaction 을 무효로 만들지 않는다. EVM 안의 검사가 실패하면 그 검사를 한 frame 만 실패한다.

[SNET-TX-043] `CALL`, `CALLCODE`, `DELEGATECALL`, `STATICCALL` (transaction 의 최상위 호출을 포함한다)에서 호출자 주소나 대상 주소가 blacklisted 이면, 그 호출은 반드시 값 이동 전에 `ErrBlacklistedAccount` 로 실패해야 하며, 넘겨받은 gas 를 소비하지 않아야 한다. 이때 gas 전부가 호출한 frame 으로 돌아가고, 호출한 frame 은 실패 status 0 을 본다.
Source: core/vm/evm.go:195-202, core/vm/evm.go:302-309, core/vm/evm.go:355-362, core/vm/evm.go:403-410, core/vm/instructions.go:653-685 (opCall)
Observable: state

[SNET-TX-044] `CREATE`, `CREATE2`, contract 생성 transaction 에서 생성하는 주소가 blacklisted 이면, 생성은 반드시 nonce 를 올리기 전에 `ErrBlacklistedAccount` 로 실패해야 하며, 넘겨받은 gas 를 소비하지 않아야 한다.
Source: core/vm/evm.go:479-482
Observable: state

최상위 호출의 sender 와 대상은 이미 `SNET-TX-040`/`SNET-TX-041` 이 검사했으므로, `SNET-TX-043` 은 중첩 호출에서 의미가 있다. `DELEGATECALL` 검사는 원래 sender 가 아니라 호출하는 contract 의 주소를 "호출자" 로 쓴다. EIP-7702 계정의 delegation 대상은 검사하지 않는다. 그 대상의 code 는 `resolveCode` 가 불러오고, 별도 호출 없이 위임한 계정의 문맥에서 실행되기 때문이다.

### 6.3 실패 방식

| 실패하는 곳 | 결과 | 그 tx 를 담은 block |
|---|---|---|
| `SNET-TX-040`, `-041`, `-042` | transaction 이 무효다 | 무효다 |
| 중첩 frame 의 `SNET-TX-043`, `-044`, `-045` | frame 이 실패하고 transaction 은 계속된다 | 유효하다. receipt status 는 바깥 frame 에 달려 있다 |
| depth 0 의 `SNET-TX-044` (contract 생성 transaction) | 도달할 수 없다. sender 를 `SNET-TX-040` 이 이미 검사했고, 두 검사 사이에 실행되는 것이 없다 | — |

[SNET-TX-060] block 의 transaction 을 순서대로 실행하다가 어느 하나가 이 장이나 upstream 의 transaction 무효 오류(nonce, 잔액, 수수료, intrinsic gas, signature, type, blacklist, fee payer)를 돌려주면, 그 block 은 반드시 무효여야 한다.
Source: core/state_processor.go:83-92, core/blockchain.go:1779-1782
Observable: header

노드는 PRE-PREPARE 를 받아들일 때 body 를 실행하지 않고 (`A-05`, `A-09`), block 이 commit 된 뒤에 실행한다. 그래서 이 장을 위반한 block 도 COMMIT quorum 을 모을 수 있고, 그런 뒤 모든 conforming 노드가 insert 단계에서 그 block 을 거부할 수 있다.

---

## 7. 수수료 계산

### 7.1 Transaction 별 흐름

transaction 의 `gas_used` (환불 뒤의 값), message gas price `gp`, base fee `b`, effective tip `t = gp - b` 에 대해 흐름은 다음과 같다.

| 흐름 | 금액 | 보내는 쪽 | 받는 쪽 |
|---|---|---|---|
| gas 구매 | `gas_limit * gp` | 지불자 | 없어진다 |
| 환불 | `(gas_limit - gas_used) * gp` | — | 지불자 |
| Tip | `gas_used * t` | — | `header.Coinbase` |
| Base fee | `gas_used * b` | — | transaction 수준에서는 누구에게도 넣지 않는다 |

Source: core/state_transition.go:344-345, core/state_transition.go:563-577, core/state_transition.go:667-691, consensus/wbft/engine/engine.go:86-88 (Author = header.Coinbase)

[SNET-TX-050] 실행 뒤에 노드는 반드시 `gas_used * effective_tip` 을 `header.Coinbase` 에 넣어야 한다.
Source: core/state_transition.go:569-577, core/evm.go:49-54
Observable: state

[SNET-TX-051] transaction 실행 중에 base fee 몫 `gas_used * base_fee` 를 어느 계정에도 넣어서는 안 된다. block 수준에서는 finalization 단계가 `header.GasUsed * header.BaseFee` 를 validator 들에게 넣는다 (`B-06`). 그래서 block 전체로 보면 base fee 는 burn 되지 않는다.
Source: core/state_transition.go:563-577, consensus/wbft/engine/engine.go:941-946, consensus/wbft/engine/engine.go:972-1048
Observable: state

모든 transaction 은 base fee 로 정확히 `gas_used * b` 를 내고, `header.GasUsed` 는 `gas_used` 의 합이다. 그래서 `B-06` 이 분배하는 금액은 block 전체에서 차감된 금액과 같다. coinbase 는 모든 transaction 의 tip 에 더해 base fee 분배의 자기 몫과 나머지(dust)를 받는다.

### 7.2 요약 식

```python
def fee_effects(msg, gas_used, base_fee):
    gp  = msg.gas_price                                # SNET-TX-012
    tip = min(msg.gas_tip_cap, msg.gas_fee_cap - base_fee)
    assert gp == base_fee + tip
    payer_net_debit = gas_used * gp                    # 여기에 sender 의 value 가 더해진다
    coinbase_credit = gas_used * tip
    pending_base    = gas_used * base_fee              # finalization 에서 넣는다 (B-06)
```

---

## 8. Receipt 와 log

### 8.1 Receipt 인코딩

type-`0x16` 과 type-`0x04` receipt 는 typed receipt 다. 합의 인코딩은 `type || rlp([status, cumulative_gas_used, bloom, logs])` 이고, 다른 typed receipt 와 같이 receipt trie 에 들어간다. 기준 코드(go-ethereum v1.13.15)는 모르는 receipt type 에 아무것도 쓰지 않는다. 그래서 "upstream 과 같다" 는 문장으로는 `0x04` 인코딩이 정의되지 않는다.

[SNET-TX-070] 노드는 반드시 type-`0x16` 또는 type-`0x04` transaction 의 receipt 를 receipt trie 에서 EIP-2718 typed receipt, 곧 type 바이트 뒤에 `rlp([status, cumulative_gas_used, bloom, logs])` 를 붙인 형태로 인코딩해야 한다.
Source: core/types/receipt.go:130-148, core/types/receipt.go:210-226, core/types/receipt.go:321-337
Observable: header

저장 인코딩(`ReceiptForStorage`)은 `EffectiveGasPrice` 가 있으면 그 값을 선택적인 마지막 원소로 덧붙인다. 저장 인코딩은 합의 데이터가 아니라 데이터베이스 형식이다.

Source: core/types/receipt.go:104-110, core/types/receipt.go:274-312

[SNET-TX-071] Anzeon 체인에서 receipt 의 `effectiveGasPrice` (RPC 가 돌려주는 값)는 반드시 `SNET-TX-012` 의 message gas price 와 같아야 한다. 곧 이 값은 authorized 가 아닌 sender 에 대해 거버넌스 tip override 를 반영한다.
Source: core/state_processor.go:157-159, internal/ethapi/api.go:1883
Observable: rpc

### 8.2 `AuthorizedTxExecuted` log

sender 가 authorized 이면, transaction 은 다른 모든 것이 끝난 뒤에 log 하나를 덧붙인다.

| 필드 | 값 |
|---|---|
| address | `AccountManagerAddress` = `0x0000000000000000000000000000000000B00003` |
| topics | `[0x40e728a89c7f5b192cf1c1b747fb64d51d81c7a2b3ed4607b94d3a1e6a3e0373]` = `keccak256("AuthorizedTxExecuted()")` |
| data | 비어 있다 |

Source: core/state_transition.go:587-599, params/protocol_params.go:219-223

이 log 는 receipt 의 일부이므로 `Header.ReceiptHash` 와 `Header.Bloom` 에 영향을 준다.

이 log 의 목적은 노드가 state 없이 `effectiveGasPrice` 를 다시 만들 수 있게 하는 것이다. snap sync 로 받은 receipt 에는 `EffectiveGasPrice` 가 없으므로, `DeriveFields` 가 그 값을 다시 계산한다.

```python
def derive_effective_gas_price(receipt, tx, header):
    tip = tx.gas_tip_cap
    if header_gas_tip(header) is not None and not last_log_is_authorized_tx_executed(receipt):
        tip = header_gas_tip(header)
    return min(tip + header.base_fee, tx.gas_fee_cap)
```

Source: core/types/receipt.go:396-411, core/types/receipt.go:452-466, core/rawdb/accessors_chain.go:661

> 해설: 이 차이가 생겨도 receipt root 는 두 노드에서 같다. 달라지는 것은 full sync 노드와 snap sync 노드가 RPC 로 돌려주는 `effectiveGasPrice` 값뿐이다. RPC 의 transaction 객체에 있는 `gasPrice` 필드는 또 다른 방식으로 계산된다 (`B-09` SNET-RPC-031). 그래서 authorized sender 와 legacy transaction 에서는 그 필드가 실제로 청구된 가격과 다르다. 청구된 가격은 receipt 의 `effectiveGasPrice` 이므로, 관찰 도구는 가격을 transaction 객체가 아니라 receipt 에서 읽어야 한다.

어떤 contract 도 이 log 를 위조할 수 없다. log 의 address 는 실행 중인 contract 의 주소인데, `AccountManagerAddress` 는 native manager 여서 EVM code 가 없기 때문이다 (§9.5).

### 8.3 Native `Transfer` log

Anzeon 체인에서는 EVM 이 일으킨 0 이 아닌 native coin 이동마다 ERC-20 형식의 log 가 나온다. 그래서 native coin 은 NativeCoinAdapter 주소의 token 처럼 보인다.

| 필드 | 값 |
|---|---|
| address | `ChainConfig.Anzeon.SystemContracts.NativeCoinAdapter.Address` (genesis 설정 값) |
| topics | `[0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef, pad32(from), pad32(to)]` |
| data | big-endian `pad32(amount)` |

Source: core/vm/evm.go:589-611, params/protocol_params.go:222

[SNET-TX-073] 노드는 다음 경우에 반드시 위 필드를 가진 `Transfer` log 를 이동 시점에, frame 의 journal 안에서 덧붙여야 한다. log 가 journal 안에 있으므로 frame 이 revert 하면 log 도 사라진다.
1. value 가 0 이 아닌 `CALL` 이나 최상위 호출이 실행된다. 이 경우 노드는 이동 뒤에 log 를 덧붙인다.
2. value 가 0 이 아닌 `CREATE`, `CREATE2`, 생성 transaction 이 실행된다.
3. contract 잔액이 0 이 아닌 상태에서 `SELFDESTRUCT` 가 실행된다. 이 경우 log 의 `to` 는 수혜자다.
Source: core/vm/evm.go:240-243, core/vm/evm.go:507-510, core/vm/instructions.go:812-815, core/vm/instructions.go:834-837
Observable: state, rpc

---

## 9. 그 밖의 실행 차이

### 9.1 값 이동 제한

[SNET-TX-080] Anzeon 체인에서 value 가 0 이 아닌 `CALL` 이나 최상위 호출은 대상이 zero address 이면 반드시 `ErrZeroAddressTransfer` 로 실패해야 하고, 대상이 활성 precompile 이나 native manager 이면 반드시 `ErrValueTransferToPrecompile` 로 실패해야 한다. 이 실패는 이동 전에 일어나며, 넘겨받은 gas 는 호출자에게 돌아간다. depth 0 에서는 transaction 이 status 0 으로 block 에 들어가고 intrinsic gas 만 소비한다.
Source: core/vm/evm.go:203-220
Observable: state

### 9.2 PREVRANDAO

[SNET-TX-081] difficulty 가 0 이나 1 인 block 을 실행할 때 opcode `0x44` (`PREVRANDAO`)가 돌려주는 값은 반드시 `header.MixDigest` 여야 한다. WBFT 체인에서 이 값은 randao mix 다 (`A-02`). Anzeon 체인에서 `0x44` 에는 `DIFFICULTY` 의미가 없다.
Source: core/evm.go:59-62, core/vm/jump_table.go:117-124, core/vm/instructions.go:478-481
Observable: state

### 9.3 Instruction set

Anzeon instruction set 은 London instruction set 에 다음을 더한 것이다.

- `0x44` 의 `PREVRANDAO`
- `PUSH0` (EIP-3855)
- `CREATE`/`CREATE2` 의 initcode 계량 (EIP-3860)
- `TLOAD`/`TSTORE` (EIP-1153)
- `MCOPY` (EIP-5656)
- 같은 transaction 에서 만든 contract 에만 동작하는 `SELFDESTRUCT` (EIP-6780)
- EIP-7702 호출 gas 규칙 (SNET-TX-094)

`BLOBHASH` 와 `BLOBBASEFEE` 는 켜지지 않는다.

Source: core/vm/jump_table.go:117-143, core/vm/interpreter.go:59-68

[SNET-TX-082] 노드는 반드시 Anzeon block 을 정확히 위 instruction set 으로 실행해야 한다. 특히 `0x49` 와 `0x4a` 는 반드시 무효 opcode 여야 한다.
Source: core/vm/jump_table.go:131-133
Observable: state

참조 구현은 "같은 transaction 에서 만들었다" 를 v1.13.15 state object 의 transaction 별 flag 로 판정한다. 이 flag 는 contract 생성뿐 아니라 계정 객체가 새로 만들어질 때마다 켜진다. EIP-7702 이전에는 두 조건이 실제로 같았지만, EIP-7702 가 들어오면서 달라졌다.

> 해설: 결과적으로 EVM 안의 `CREATE` 는 initcode 를 계량하지만 최상위 생성 transaction 은 계량하지 않는다. upstream Shanghai 코드를 그대로 가져오면 여기서 gas 가 달라진다.

[SNET-TX-084] 모든 transaction 이 시작할 때 native manager 주소는 반드시 precompile 과 함께 EIP-2929 access list 에 들어가야 한다.
Source: core/state/statedb.go:1368-1394, core/state_transition.go:521
Observable: state

### 9.4 Precompile

| 주소 | Contract | 활성 조건 |
|---|---|---|
| `0x01`-`0x09` | upstream Istanbul/Berlin 집합 | Anzeon |
| `0x0a` | KZG point evaluation | Anzeon. blob transaction 은 거부하지만 이 precompile 은 켜져 있다 |
| `0x…B00001` | BLS proof-of-possession verifier, 45 000 gas | Anzeon |
| `0x0100` | P-256 verify (EIP-7951: gas 6 900, RIP-7212 의 3 450 이 아니다; SNET-TX-089) | Boho 이후 |

Source: core/vm/contracts.go:127-155, core/vm/contracts.go:197-204, params/protocol_params.go:208-209

[SNET-TX-085] 노드는 반드시 `BohoBlock` 이전에는 Anzeon precompile 집합을 쓰고, `BohoBlock` 부터는 Boho 집합(Anzeon 집합에 `0x0100` 의 P-256 을 더한 것)을 써야 한다.
Source: core/vm/evm.go:40-60, core/vm/contracts.go:141-155
Observable: state

[SNET-TX-089] `BohoBlock` 부터 `0x0000000000000000000000000000000000000100` 의 P-256 precompile 호출은 반드시 모든 입력에 대해 정확히 `P256_VERIFY_GAS = 6 900` gas (`B-01` §10, SNET-CFG-010)를 써야 하고, 실패해서는 안 된다. 입력이 정확히 160 바이트 `hash(32) ‖ r(32) ‖ s(32) ‖ x(32) ‖ y(32)` (big-endian)이고, `(x, y)` 가 NIST P-256 곡선 위의 점(좌표가 field prime 보다 작고 무한원점이 아님)이고, `1 ≤ r, s ≤ n − 1` 이며, 32 바이트 `hash` 에 대한 `(r, s)` 와 `(x, y)` 의 ECDSA 검증이 성공하면 (low-S 규칙은 없다) 출력은 반드시 32 바이트 word `0x00…01` 이어야 한다. 입력 길이가 다른 경우를 포함해 그 밖의 모든 경우에 출력은 반드시 비어 있어야 한다. 그래서 검증 실패는 오류가 아니다. 호출은 성공하고, 넘겨받은 gas 가운데 6 900 을 넘는 부분은 돌려준다. 이것은 EIP-7951 의 동작이며, RIP-7212 의 gas 3 450 을 써서는 안 된다.
Source: core/vm/contracts.go:1225-1251 (p256Verify), params/protocol_params.go:171 (P256VerifyGas)
Source: crypto/secp256r1/verifier.go:27-33 (IsOnCurve, then crypto/ecdsa.Verify)
Source: core/vm/testdata/precompiles/p256Verify.json (782 vectors, all with gas 6 900)
Observable: state

BLS PoP precompile 의 입력 형식과 출력은 `B-04` 에 있다. GovValidator 가 이 precompile 을 쓴다.

### 9.5 Native manager

두 주소는 4 바이트 selector 로 method 를 고르는 내장 contract 처럼 동작한다. 두 주소에는 state 에 code 가 없다.

| 주소 | Method | 인자 수 | 필요한 호출자 | 호출 종류 | Gas |
|---|---|---|---|---|---|
| `0x…B00002` NativeCoinManager | `mint(address,uint256)` | 2 | NativeCoinAdapter (genesis 주소) | `CALL` | `amount > 0` 이면 4 500 (`to` 가 없으면 +25 000), `amount = 0` 이면 0 |
| | `burn(address,uint256)` | 2 | NativeCoinAdapter | `CALL` | 4 500 |
| | `transfer(address,address,uint256)` | 3 | NativeCoinAdapter | `CALL` | `amount > 0` 이면 9 000 (`to` 가 없으면 +25 000), `amount = 0` 이면 0. 잔액 검사가 gas 검사보다 먼저다 |
| `0x…B00003` AccountManager | `blacklist(address)` | 1 | GovCouncil (genesis 주소) | `CALL` | 4 500 (새 계정이면 +25 000) |
| | `unBlacklist(address)` | 1 | GovCouncil | `CALL` | 4 500 |
| | `authorize(address)` | 1 | GovCouncil | `CALL` | 4 500 (새 계정이면 +25 000) |
| | `unAuthorize(address)` | 1 | GovCouncil | `CALL` | 4 500 |
| | `isBlacklisted(address)` | 1 | 누구나 | 모든 종류 | 0. 32 바이트 bool 을 돌려준다 |
| | `isAuthorized(address)` | 1 | 누구나 | 모든 종류 | 0. 32 바이트 bool 을 돌려준다 |

Source: core/vm/native_manager.go:82-131, core/vm/native_manager.go:195-457, core/vm/native_manager.go:463-489, params/protocol_params.go:212-220

[SNET-TX-086] native manager 호출은 다음 경우에 반드시 실패하고 넘겨받은 gas 를 모두 소비해야 한다. 이 오류는 revert 가 아니므로 EVM 은 gas 를 하나도 남기지 않는다 (`B-04` SNET-SYS-080). 노드는 아래 순서대로 검사한다.
1. 입력이 4 바이트보다 짧다 (`ErrInvalidSelectorLength`).
2. selector 를 모른다 (`ErrInvalidMethod`).
3. 호출 종류나 호출자가 허용되지 않는다 (`ErrInvalidCallContext`, `ErrUnauthorized`).
4. 인자 길이가 정확히 `32 * ParamCount` 가 아니다 (`ErrInvalidInputLength`).
Source: core/vm/native_manager.go:137-156
Observable: state

### 9.6 Code 크기 한도

[SNET-TX-088] Anzeon 체인에서 배포된 contract code 의 최대 크기(EIP-170 한도)는 반드시 24 576 이 아니라 `MAX_CODE_SIZE = 253 952` 바이트 (`B-01` §10, SNET-CFG-007)여야 한다. `CREATE`, `CREATE2`, contract 생성 transaction 이 돌려준 code 가 `MAX_CODE_SIZE` 보다 길면, 생성은 반드시 생성 frame 의 gas 를 모두 쓰는 exceptional halt 로 실패해야 한다. `CREATE` 와 `CREATE2` 가 적용하는 initcode 한도(Anzeon instruction set 이 켜는 EIP-3860, §9.3)는 반드시 49 152 가 아니라 `MAX_INITCODE_SIZE = MAX_CODE_SIZE = 253 952` 바이트 (`B-01` §10, SNET-CFG-029)여야 하고, 이 한도를 넘어도 frame 의 gas 를 모두 쓴다. initcode word gas 는 2, code 예치 비용은 바이트당 200 gas 그대로다.
Source: params/protocol_params.go:140-141 (MaxCodeSize, MaxInitCodeSize)
Source: core/vm/evm.go:526-529 (EIP-170 check)
Source: core/vm/gas_table.go:306-332 (EIP-3860 check in the CREATE/CREATE2 gas functions)
Observable: state

---

## 10. 수수료 계산 예

공통 조건(`params/protocol_params.go:135-138` 의 preset 값)은 다음과 같다. base fee 는 `B = 20 000 gwei` (`2·10^13` wei, Anzeon 최솟값)이고, 거버넌스 gas tip 은 `T = 27 600 gwei` (`InitialGasTip`)다. 1 coin 은 `10^18` wei 이고 `10^9` gwei 다. 따로 적지 않은 한 모든 이동은 `gas_used = 21 000` 을 쓴다.

**E-1 authorized 가 아닌 EIP-1559 이동.** `max_fee = 100 000 gwei`, `max_priority_fee = 1 gwei`, value 는 5 coin 이다.
- override 뒤 tip cap 은 `T = 27 600` 이다. `gp = min(27 600 + 20 000, 100 000) = 47 600 gwei` 다.
- sender 잔액 검사는 `21 000 × 100 000 gwei + 5 coin = 2.1 + 5 = 7.1 coin` 을 요구한다.
- sender 의 순 차감액은 `21 000 × 47 600 gwei = 0.9996 coin` 에 value 5 coin 을 더한 값이다.
- coinbase 는 `21 000 × 27 600 gwei = 0.5796 coin` 을 받는다. `B-06` 으로 넘어가는 base fee 는 `0.42 coin` 이다.
- receipt 의 `effectiveGasPrice` 는 `47 600 gwei` 다. `AuthorizedTxExecuted` log 는 없고, `Transfer` log (5 coin)가 하나 있다.

**E-2 fee cap 이 max(B, T) 와 B + T 사이인 경우.** 조건은 E-1 과 같고, `max_fee` 만 `30 000 gwei` 다.
- `30 000 >= max(20 000, 27 600)` 이므로 transaction 은 유효하다. `gp = min(47 600, 30 000) = 30 000` 이고, `tip = 10 000 gwei` 다.
- coinbase 는 `0.21 coin` 을 받는다. 이 값은 거버넌스 tip 이 주었을 금액보다 적다. receipt 의 `effectiveGasPrice` 는 `30 000 gwei` 다.

**E-3 fee cap 이 거버넌스 tip 보다 작은 경우.** `max_fee = 25 000 gwei` 이고, 이 값은 `B` 보다 크다.
- override 뒤 `tip_cap = 27 600 > fee_cap = 25 000` 이므로 `ErrTipAboveFeeCap` 이 난다. 그 transaction 은 무효이고, 그 transaction 을 담은 block 도 무효다 (`SNET-TX-013`, `SNET-TX-060`).

**E-4 authorized 가 아닌 legacy transaction.** `gas_price = 1 000 000 gwei` 다.
- `tip_cap = 27 600`, `fee_cap = 1 000 000` 이다. `gp = 47 600 gwei` 다. upstream 이라면 1 000 000 gwei 를 청구한다.
- 잔액 검사는 `21 000 × 1 000 000 gwei = 21 coin` 을 요구한다. 이 검사는 upstream 과 같이 fee cap 을 기준으로 한다. 순 차감액은 `0.9996 coin` 이다.

**E-5 authorized sender, tip 0.** `max_fee = 20 000 gwei`, `max_priority_fee = 0` 이다.
- override 가 없다. `gp = 20 000`, `tip = 0` 이다. coinbase 는 `0` 을 받는다. base fee `0.42 coin` 은 finalization 으로 넘어간다.
- receipt 는 `AuthorizedTxExecuted` log 로 끝난다. `effectiveGasPrice` 는 `20 000 gwei` 다.

**E-6 authorized sender, 높은 tip.** `max_fee = 200 000`, `max_priority_fee = 100 000` 이다. `gp = 120 000`, `tip = 100 000` 이므로 coinbase 는 `2.1 coin` 을 받는다. authorized sender 는 `T` 보다 많이 낼 수 있고, authorized 가 아닌 sender 는 그럴 수 없다.

**E-7 fee delegation 이동 (Applepie 이후).** sender `S` 는 authorized 가 아니고, fee payer 는 `P ≠ S` 다. transaction 은 contract 를 호출하며, value 는 1 coin, `gas = 50 000`, `gas_used = 30 000`, `max_fee = 100 000 gwei` 다.
- `gp = 47 600 gwei` 다.
- 노드는 `P.balance >= 50 000 × 47 600 gwei = 2.38 coin` 과 `S.balance >= 1 coin` 을 검사한다. `P` 에게 `50 000 × 100 000 = 5 coin` 을 요구하지는 않는다.
- `P` 는 `2.38` 을 차감당하고 `20 000 × 47 600 gwei = 0.952` 를 돌려받아, 순 `1.428 coin` 을 낸다. `S` 는 value 만 낸다.
- coinbase 는 `30 000 × 27 600 gwei = 0.828 coin` 을 받는다. finalization 으로 넘어가는 base fee 는 `0.6 coin` 이다.
- `S` 가 authorized 였다면 tip 은 `S` 자신의 `max_priority_fee` 였을 것이다. authorization 은 항상 sender 에 대해 보고, fee payer 에 대해서는 보지 않는다.

**E-8 blacklisted fee payer.** E-7 과 같지만 `P` 가 blacklisted 다. EVM 호출이 실행된 뒤 `ErrBlacklistedAccount{P}` 가 돌아온다. 그 transaction 은 무효이고, 그 block 도 무효다.

**E-9 block 합계.** E-1, E-5, E-7 을 담은 block 에서 `header.GasUsed = 21 000 + 21 000 + 30 000 = 72 000` 이다. finalization 에서 분배하는 base fee 는 `72 000 × 20 000 gwei = 1.44 coin` (= `0.42 + 0.42 + 0.6`)이다. coinbase 의 tip 은 `0.5796 + 0 + 0.828 = 1.4076 coin` 이고, 여기에 `B-06` 이 주는 base fee 몫과 나머지(dust)가 더해진다.

---

## 11. Block 조립 순서 (informative)

이 절은 go-stablenet proposer 가 하는 일을 설명한다. 다른 순서로도 유효한 block 을 만들 수 있다.

0. proposer 는 정렬하기 전에 *remote* 계정마다 pending 목록에서 effective tip `min(cached_tip_cap, fee_cap − base_fee)` 이 현재 거버넌스 gas tip 보다 작은 첫 transaction 을 찾고, 그 transaction 과 그 계정의 뒤 transaction 을 모두 뺀다. local 계정은 거르지 않는다. `cached_tip_cap` 은 transaction 이 pool 에 들어올 때 정해진다. authorized 가 아닌 sender 이면 그때의 header gas tip 이고, authorized sender 이면 transaction 자신의 tip cap 이다. 그래서 `fee_cap < base_fee + gas_tip` 인 authorized 가 아닌 remote transaction 은 유효하지만 (SNET-TX-013, 예 E-2) 참조 구현은 그 transaction 을 제안하지 않는다.
1. proposer 는 pool 의 pending transaction 을 *local* 계정과 *remote* 계정으로 나눈다. 두 묶음은 따로 commit 하며, local 묶음을 먼저 commit 한다.
2. 한 묶음 안에서는 각 계정의 맨 앞 transaction 이 heap 에서 경쟁한다. Anzeon 이 켜져 있으면 다음 규칙을 쓴다.
   - authorized sender 는 항상 authorized 가 아닌 sender 보다 앞선다. authorized 인지는 block 조립 state 로 판정한다.
   - authorized sender 둘 사이에서는 miner tip 이 높은 쪽이 먼저이고, tip 이 같으면 pool 에 먼저 도착한 쪽이 먼저다.
   - authorized 가 아닌 sender 둘 사이에서는 pool 에 먼저 도착한 쪽이 먼저이고, 수수료는 보지 않는다 (FIFO).
3. 한 계정 안에서는 nonce 순서를 따른다.
4. nonce-too-low 오류로 실패한 transaction 은 건너뛴다 (shift). 다른 오류가 나면 그 계정의 나머지 transaction 을 이 block 에서 모두 뺀다.

Source: miner/worker.go:1255-1286, miner/ordering.go:68-101, miner/ordering.go:134-160, miner/worker.go:1068-1084
Source: miner/worker.go:1230-1240, core/txpool/legacypool/legacypool.go:579-605, core/txpool/validation.go:411-416, core/types/transaction.go:388-402 (item 0)

authorized sender 를 정렬할 때 쓰는 miner tip 은 `min(gas_tip_cap, fee_cap - base_fee)` 다. authorized 가 아닌 sender 의 tip cap 이 header gas tip 보다 크면, 정렬 key 는 header gas tip 그 자체다. 이때 `fee_cap - base_fee` 한도는 적용하지 않는다. 그 밖의 authorized 가 아닌 sender 에 대해서는 `min(gas_tip_cap, fee_cap - base_fee)` 다. 두 값 모두 실제로 청구되는 tip (`min(header_gas_tip, fee_cap - base_fee)`)과 다를 수 있다. 그래도 authorized 가 아닌 sender 는 시간 순서로만 정렬하므로 이 차이는 문제가 되지 않는다.

Source: miner/ordering.go:40-60

authorization 은 block 을 채우기 전에 block 조립 state 에서 한 번만 읽는다. 그래서 같은 block 안의 transaction 이 sender 의 authorization 을 바꿔도 그 block 의 순서는 바뀌지 않는다.

proposer 는 `totalFees` log 값을 계산할 때 각 transaction 의 tip 에 base fee 를 더한다. 이 계산은 base fee 가 burn 되지 않고 분배된다는 사실을 반영한다.

Source: miner/worker.go:1470-1485

---

## 12. Transaction pool 정책 (informative)

이 규칙들은 go-stablenet 노드가 무엇을 gossip 하고 제안할지를 정한다. 이 규칙들은 block 유효성 규칙이 아니다.

| 규칙 | 동작 | Source |
|---|---|---|
| Type gate | `0x16` 은 Applepie 이전에 거부한다. `0x04` 는 Anzeon 이 없으면 거부한다. `0x03` 은 Cancun 이 없으면 거부한다 | core/txpool/validation.go:72-80 |
| 받아들이는 type (legacy pool) | `0x00`, `0x01`, `0x02`, `0x16`, `0x04` | core/txpool/legacypool/legacypool.go:655-661 |
| 최소 수수료 | `fee_cap >= MinBaseFee + MinTip` 이다. `MinTip` 은 remote transaction 이면 pool gas tip 이고 local transaction 이면 0 이다 (legacy 는 gas price 로 본다) | core/txpool/validation.go:121-131, core/txpool/legacypool/legacypool.go:662-666 |
| 최소 tip | `tx.gas_tip_cap >= pool gas tip` (remote 에만 적용한다. pool gas tip 은 GovValidator 값을 따른다) | core/txpool/validation.go:119-121, miner/worker.go:361-388 |
| Fee payer signature | `RecoverFeePayer` 로 검증한다 | core/txpool/validation.go:133-138 |
| Blacklist | sender, `to`, fee payer 가 blacklisted 이면 거부한다 | core/txpool/validation.go:250-259, core/txpool/validation.go:284-286 |
| 잔액 | fee delegation 에서는 sender `>= value`, fee payer `>= gas × fee_cap` 을 요구한다. 계정별 pending 지출은 두 역할을 합쳐 누적한다 (PR #116) | core/txpool/validation.go:268-293 |
| Gas tip 인상 | 캐시된 Anzeon tip 이 새 tip 보다 낮은 remote transaction 을 버린다. authorized 가 아닌 sender 의 캐시된 tip 은 pool 이 받아들인 시점의 header gas tip 이다 | core/txpool/legacypool/legacypool.go:474-497, core/txpool/legacypool/legacypool.go:2113-2130 |
| `miner_setGasPrice` | Anzeon 에서는 `false` 를 돌려주고 아무것도 하지 않는다 | eth/api_miner.go:61-71 |

pool 의 fee payer 잔액 검사는 fee cap 을 쓰므로, 실행 단계의 검사인 `SNET-TX-031` 보다 엄격하다.

---

## 13. 오류 목록

| 오류 | 문자열 | 발생 위치 | tx 를 무효로 만드는가 |
|---|---|---|---|
| `ErrBlacklistedAccount` (core) | `blacklisted account: <addr>` | `SNET-TX-040`, `-041`, `-042` | 예 |
| `ErrBlacklistedAccount` (vm) | `blacklisted account: <addr>` | `SNET-TX-043`, `-044`, `-045` | 아니오 (frame 이 실패한다) |
| `ErrTxTypeNotSupported` | `transaction type not supported[: fee delegation type not supported]` | `SNET-TX-020`, `-030` | 예 |
| `ErrInsufficientFunds` | `insufficient funds for gas * price + value: {sender\|feePayer} <addr> have <x> want <y>` | `SNET-TX-031`, `-032` | 예 |
| `ErrTipAboveFeeCap` | `max priority fee per gas higher than max fee per gas` | `SNET-TX-013` | 예 |
| `ErrEmptyAuthList` | `EIP-7702 transaction with empty auth list` | `SNET-TX-091` | 예 |
| `ErrZeroAddressTransfer` | `transfer to zero address not allowed` | `SNET-TX-080` | 아니오 |
| `ErrValueTransferToPrecompile` | `value transfer to precompiled contract disallowed` | `SNET-TX-080` | 아니오 |
| native manager 오류 | `native manager: …` | `SNET-TX-086` | 아니오 |

Source: core/error.go:64-101, core/error.go:121-122, core/error.go:142-147, core/vm/errors.go:44-47, core/vm/errors.go:83-89, core/types/transaction_signing.go:33-36, core/vm/native_manager.go:42-57
