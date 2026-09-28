# B-07 Anzeon transaction rules

- Part: B (StableNet block validity)
- Area code: `TX`
- Status: draft
- Reference: go-stablenet `740526d03`

This chapter specifies how a StableNet node executes transactions when the Anzeon rule set is active. It is written as a delta against upstream go-ethereum execution (the London rule set with the upstream code this fork is based on): everything not mentioned here behaves as in upstream go-ethereum at the same code base. The chapter covers the per-account flag bits (blacklist, authorized), the governance gas-tip override, fee accounting per transaction, the fee-delegation transaction type, receipts and logs added by Anzeon, and the other EVM differences that change state, receipts or logs.

Block-level fee handling (distribution of the base fee to validators, the gas-tip header check) is in `B-06`. The system contracts that set the account bits (GovCouncil) and the gas tip (GovValidator) are in `B-04` and `B-05`. Header and body validity (gas limit, base fee, forbidden fields) are in `B-03`. Fork activation and the Anzeon configuration object are in `B-01`.

---

## 1. Scope and conformance

### 1.1 What is normative

A transaction's validity, and the state, receipt and logs it produces, are normative, because they are committed through `Header.Root`, `Header.ReceiptHash`, `Header.Bloom` and `Header.GasUsed` (`B-03`). A block that contains a transaction that is invalid under this chapter is invalid, and every conforming node MUST reject it (see `SNET-TX-060`).

Error identities and error strings are not normative for block validity: a conforming node only has to reject the block. They are listed in §13 because they are observable through `eth_call`, `eth_estimateGas` and `eth_sendRawTransaction`.

Transaction ordering inside a block and transaction-pool admission are local policy (§11, §12, informative).

### 1.2 Rule-set gating

| Rule set | Condition in the reference implementation | Used for |
|---|---|---|
| Anzeon | `ChainConfig.Anzeon != nil` (configuration presence, not a block number). `Rules.IsAnzeon` is true at every height of such a chain | Every rule in this chapter unless stated otherwise |
| Applepie | `number >= ChainConfig.ApplepieBlock` | Fee delegation with a fee payer different from the sender (§5) |
| Boho | Anzeon and `number >= ChainConfig.BohoBlock` | P-256 precompile (§9.4); GovMinter v2 upgrade (`B-04`) |

Source: params/config.go:1037-1044 (IsApplepie, IsBoho), params/config.go:1085-1087 (AnzeonEnabled), params/config.go:1512-1540 (Rules)

On a valid WBFT chain the header difficulty is 1 (`A-08`), so `Rules.IsMerge` is false and therefore `Rules.IsShanghai`, `Rules.IsCancun` and `Rules.IsPrague` are false at every height, whatever the configured timestamps. WBFT header verification also rejects any header for which `ChainConfig.IsShanghai` or `ChainConfig.IsCancun` is true. Upstream features that Anzeon needs are switched on by `IsAnzeon` directly (§9).

Source: params/config.go:1518-1539, consensus/wbft/engine/engine.go:220-228

[SNET-TX-001] On a chain whose configuration contains the Anzeon object, a node MUST apply the Anzeon rules of this chapter at every block height, including height 1.
Source: params/config.go:1519-1533
Observable: state

### 1.3 Notation

Pseudocode follows `README.md §2.4`. The following helpers are used in this chapter.

```python
BLACKLISTED = 1 << 63
AUTHORIZED  = 1 << 62

def account_extra(state, addr) -> uint64:        # 0 if the account does not exist
def is_blacklisted(state, addr) -> bool: return account_extra(state, addr) & BLACKLISTED != 0
def is_authorized(state, addr)  -> bool: return account_extra(state, addr) & AUTHORIZED  != 0

def header_gas_tip(header) -> Optional[uint256]:
    # WBFTExtra.GasTip of the block being executed (A-03). None if the extra does
    # not decode as WBFTExtra. (The reference also tests for a nil GasTip field, which
    # cannot occur: a decoded GasTip is never absent, A-03 WBFT-ENC-008.)
```

`state` always means the state at the moment of the check: the state after all earlier transactions of the same block and after all earlier steps of the same transaction, unless stated otherwise.

Source: core/types/block.go:111-117 (Header.GasTip), core/state/statedb.go:300-326

---

## 2. Account extra bits

### 2.1 Encoding

The state account gains a fifth field `Extra`, a `uint64`, encoded as an optional trailing RLP element: `[nonce, balance, storageRoot, codeHash]` when `Extra == 0`, and `[nonce, balance, storageRoot, codeHash, Extra]` otherwise. The same optional field exists in the snapshot "slim" account. The storage layout of the account trie is otherwise unchanged; the owning chapter for the trie encoding is `B-04` §11.

[SNET-TX-002] The account `Extra` field MUST be encoded as defined in `B-04` SNET-SYS-070 (optional trailing RLP element, omitted when zero). This requirement is a reference; `B-04` holds the normative statement.
Source: core/types/state_account.go:31-38, core/types/state_account.go:73
Observable: state, header

[SNET-TX-003] A node MUST interpret the `Extra` flags as `B-04` SNET-SYS-071 defines (bit 63 *blacklisted*, bit 62 *authorized*) and MUST reject a genesis allocation that sets one of the undefined bits 0-61, as `B-04` SNET-SYS-013 defines. This requirement is a reference; `B-04` holds the normative statements.
Source: core/types/state_account_extra.go:31-46, core/types/state_account_extra.go:101-107, core/genesis.go:257
Observable: state

[SNET-TX-004] A node MUST treat an account whose only non-default field is a non-zero `Extra` as non-empty for EIP-161 state clearing, as `B-04` SNET-SYS-072 defines. This requirement is a reference; `B-04` holds the normative statement.
Source: core/state/state_object.go:94-96
Observable: state

[SNET-TX-005] Clearing a flag that is not set on an empty account MUST touch the account, so that EIP-161 clearing removes it at the end of the transaction; clearing or setting a flag MUST otherwise change only the targeted bit.
Source: core/state/state_object.go:524-564
Observable: state

### 2.2 Semantics summary

| Flag | Set by | Effect in execution | Section |
|---|---|---|---|
| blacklisted | `AccountManager.blacklist(addr)`, called only by GovCouncil; genesis | The account cannot send, receive at top level, pay fees, be called or call inside the EVM, create contracts, or be a SELFDESTRUCT beneficiary | §6 |
| authorized | `AccountManager.authorize(addr)`, called only by GovCouncil; genesis | The account's own `maxPriorityFeePerGas` is used instead of the governance gas tip; its transactions emit `AuthorizedTxExecuted` | §3, §8.2 |

The flags are independent: an account can carry both. Nothing in execution gives a blacklisted-and-authorized account any exemption.

---

## 3. Gas-tip override

### 3.1 Message construction

A transaction is turned into an execution message before it runs. Anzeon changes how the tip cap of that message is chosen.

```python
def to_message(tx, header, state, signer) -> Message:
    sender = recover_sender(signer, tx)                 # upstream rules; §4 for type 0x16
    tip_cap = tx.gas_tip_cap                            # legacy/2930: = tx.gas_price
    g = header_gas_tip(header)
    if g is not None and not is_authorized(state, sender):
        tip_cap = g                                     # governance tip replaces the user's value
    gas_price = min(tip_cap + header.base_fee, tx.gas_fee_cap)   # legacy/2930: gas_fee_cap = tx.gas_price
    fee_payer = None
    if tx.type == FEE_DELEGATE_DYNAMIC_FEE_TX_TYPE:     # 0x16
        fee_payer = recover_fee_payer(tx, chain_id)     # §4.2 (fee payer signature); raises
    return Message(from_=sender, gas_tip_cap=tip_cap, gas_fee_cap=tx.gas_fee_cap,
                   gas_price=gas_price, fee_payer=fee_payer, ...)
```

Source: core/state_transition.go:159-205, core/state_processor.go:84

[SNET-TX-010] For every transaction, if `header_gas_tip(header)` is not `None` and the sender is not authorized in the state immediately before the transaction, the node MUST replace the transaction's tip cap by `header_gas_tip(header)` for all subsequent checks and fee computations of that transaction.
Source: core/state_transition.go:165-172, core/state_processor.go:84
Observable: state

[SNET-TX-011] If the sender is authorized in the state immediately before the transaction, the node MUST use the transaction's own tip cap (for legacy and access-list transactions, the gas price).
Source: core/state_transition.go:165-172
Observable: state

[SNET-TX-012] The message gas price MUST be `min(tip_cap + base_fee, gas_fee_cap)` where `tip_cap` is the value chosen by `SNET-TX-010`/`SNET-TX-011`. This applies to legacy and access-list transactions too, with `gas_fee_cap` equal to the transaction's gas price.
Source: core/state_transition.go:189-192, core/types/tx_legacy.go:101-102
Observable: state, rpc

The override is keyed on "the header's extra decodes and carries a gas tip", not on `IsAnzeon`. On a valid WBFT chain the gas tip is always present because header verification requires it to equal the GovValidator value (`B-06`), so the two conditions coincide.

Upstream go-ethereum charges a legacy transaction its full gas price after London, because `tip_cap = fee_cap = gas_price`. Under `SNET-TX-012` a non-authorized legacy transaction pays `min(gas_tip + base_fee, gas_price)`, which is usually much less than its declared gas price. See example E-4 in §10.

### 3.2 Checks that see the overridden tip

The upstream London pre-checks run on the message, that is, after the override.

[SNET-TX-013] A transaction MUST be invalid if its fee cap is lower than the tip cap chosen by `SNET-TX-010`/`SNET-TX-011` (`ErrTipAboveFeeCap`). For a non-authorized sender this means `gas_fee_cap >= header_gas_tip(header)` is required even if the transaction's own tip cap is lower.
Source: core/state_transition.go:385-388
Observable: state

### 3.3 Effective tip

```python
effective_tip = min(msg.gas_tip_cap, msg.gas_fee_cap - base_fee)    # >= 0 (gas_fee_cap >= base_fee)
# note: msg.gas_price == base_fee + effective_tip
```

Source: core/state_transition.go:563-567

[SNET-TX-015] The effective tip of a transaction MUST be computed from the message tip cap (after the override), as `min(tip_cap, gas_fee_cap - base_fee)`.
Source: core/state_transition.go:563-567
Observable: state

The EVM `GASPRICE` opcode returns the message gas price, so a contract observes the overridden price. `BASEFEE` returns the header base fee as upstream.

Source: core/evm.go:81-92 (NewEVMTxContext: GasPrice = msg.GasPrice)

---

## 4. Transaction types and signatures

### 4.1 Accepted types

The signer used for block execution on an Anzeon chain is the *Anzeon signer* (the Cancun signer is selected only when `IsCancun`, which WBFT headers forbid).

| Type byte | Name | Accepted in a block | Sender signature |
|---|---|---|---|
| (none, RLP list) | Legacy | yes (EIP-155 protected or unprotected) | upstream |
| `0x01` | EIP-2930 access list | yes | upstream |
| `0x02` | EIP-1559 dynamic fee | yes | upstream |
| `0x03` | EIP-4844 blob | no: sender recovery fails with `ErrTxTypeNotSupported` | — |
| `0x04` | EIP-7702 set code | yes (Anzeon enables it without Prague) | SNET-TX-091 (§4.3) |
| `0x16` | Fee-delegated dynamic fee | yes; fee payer different from sender only from Applepie (§5.1) | §4.2 |

Source: core/types/transaction_signing.go:46-65 (MakeSigner), core/types/transaction_signing.go:286-339 (anzeonSigner), core/types/transaction_signing.go:403-415 (londonSigner handles 0x02 and 0x16), core/types/transaction_signing.go:468-484 (eip2930Signer rejects other types), core/types/transaction.go:48-53

[SNET-TX-020] A block on an Anzeon chain MUST be invalid if it contains a transaction of type `0x03`.
Source: core/types/transaction_signing.go:51-52, core/types/transaction_signing.go:298-301, core/types/transaction_signing.go:468-479, core/state_processor.go:84-87
Observable: header

[SNET-TX-021] A node MUST accept transactions of type `0x04` on an Anzeon chain and execute them with EIP-7702 semantics (authorization processing, delegation designators, one-level code resolution), independently of `PragueTime`; the rules are SNET-TX-091 to SNET-TX-094 (§4.3).
Source: core/types/transaction_signing.go:286-339, core/state_transition.go:529-548, core/vm/evm.go:628-656, core/vm/jump_table.go:141
Observable: state

[SNET-TX-095] The transaction encodings that decode in a block body MUST be exactly legacy (an RLP list) and the typed transactions `0x01`, `0x02`, `0x03`, `0x04` and `0x16`. A block whose body contains any other type byte does not decode and MUST be rejected without being executed. A decoded type-`0x03` transaction makes the block invalid (SNET-TX-020).
Source: core/types/transaction.go:219-241 (decodeTyped)
Observable: header

### 4.2 Fee-delegated dynamic fee transaction (type `0x16`)

#### Encoding

The payload is the RLP list

```
0x16 || rlp([
    [chain_id, nonce, max_priority_fee_per_gas, max_fee_per_gas, gas, to, value, data, access_list, v, r, s],   # SenderTx
    fee_payer,        # 20-byte address; the empty string decodes to "not set"
    fv, fr, fs        # fee payer signature
])
```

The inner list is exactly the field sequence of a type-`0x02` transaction including its signature. `to` follows the `rlp:"nil"` convention (empty string for contract creation).

Source: core/types/tx_fee_delegation.go:27-34, core/types/tx_fee_delegation.go:152-157, core/types/tx_dynamic_fee.go (DynamicFeeTx field order)

The transaction hash is `keccak256(0x16 || rlp(payload))`, the standard typed-transaction rule applied to the whole structure, so it covers both signatures.

Source: core/types/transaction.go:561-574

The accessors expose the inner fields: `gas_tip_cap`, `gas_fee_cap`, `gas`, `to`, `value`, `data`, `access_list`, `nonce`, `chain_id` are those of `SenderTx`; `gas_price()` returns `max_fee_per_gas`.

Source: core/types/tx_fee_delegation.go:117-131

#### Sender signature

#### Fee payer signature

The fee payer signs the type-`0x16` signing hash:

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
    # recover_from_digest: secp256k1 recovery from an already-hashed 32-byte digest, with the
    # homestead low-S rule; unlike A-02 ecdsa_recover_address it does not hash its input
    addr = recover_from_digest(fee_payer_sighash(tx, chain_id), tx.fr, tx.fs, tx.fv + 27)
    if addr is error or addr != tx.fee_payer:
        raise ErrInvalidFeePayer                   # "fee delegation: invalid feePayer"
    return addr
```

Source: core/types/transaction_signing.go:187-198, core/types/transaction_signing.go:348-360

### 4.3 Set-code transaction (type `0x04`)

The base code of this fork (go-ethereum v1.13.15) has no EIP-7702, so "as upstream" does not define type `0x04`. go-stablenet carries the implementation of go-ethereum v1.15 (Prague, final EIP-7702) and enables it with `IsAnzeon` instead of `IsPrague`. The rules below fix that version; the other Prague rules (EIP-7623 calldata floor, EIP-2537, EIP-2935, EIP-7685) are not active (`B-01` SNET-CFG-029).

[SNET-TX-091] Type-`0x04` transactions MUST be encoded, signed and validated as in the final EIP-7702 (the version implemented by go-ethereum v1.15): the payload is `rlp([chain_id, nonce, max_priority_fee_per_gas, max_fee_per_gas, gas, to, value, data, access_list, authorization_list, y_parity, r, s])` with a 20-byte `to` (contract creation is not possible), and each authorization is `rlp([chain_id, address, nonce, y_parity, r, s])` with `chain_id < 2^256`, `nonce < 2^64` and `y_parity < 2^8` (otherwise the transaction does not decode); the sender signs `keccak256(0x04 || rlp([chain_id, …, authorization_list]))` with `v ∈ {0, 1}`, the low-S rule and `chain_id` equal to the chain id; a transaction whose `authorization_list` is empty MUST be invalid (`ErrEmptyAuthList`).
Source: core/types/tx_setcode.go:51-79, core/types/tx_setcode.go:234-250
Source: core/types/transaction_signing.go:298-310 (anzeonSigner.Sender)
Source: core/state_transition.go:429-437 (ErrSetCodeTxCreate, ErrEmptyAuthList)
Observable: state

[SNET-TX-092] The intrinsic gas of a type-`0x04` transaction MUST be the London intrinsic gas plus 25 000 per authorization tuple. The EIP-7623 calldata floor MUST NOT be applied to any transaction on an Anzeon chain.
Source: core/state_transition.go:71-121 (IntrinsicGas), core/state_transition.go:482 (called with IsShanghai = false)
Observable: state

[SNET-TX-093] A node MUST process a type-`0x04` transaction in this order: intrinsic gas and the checks of SNET-TX-040/041; access-list preparation (SNET-TX-084); increment of the sender nonce; then each authorization in list order as below; then warming the delegation target of `to` (if the code of `to` is a designator after all authorizations) without charging gas; then the call. An authorization that fails a step is skipped and does not make the transaction invalid. The steps are: (1) `chain_id` is 0 or equals the chain id; (2) `nonce < 2^64 − 1`; (3) the signature over `keccak256(0x05 || rlp([chain_id, address, nonce]))` has `y_parity ∈ {0, 1}`, `1 ≤ r, s < n` and `s ≤ n/2`, and recovers an authority; (4) the authority is added to the EIP-2929 access list (it stays there if a later step fails); (5) the authority's code is empty or a delegation designator `0xef0100 || addr`; (6) the authority's nonce equals `nonce`. For an authorization that passes, the node MUST add 12 500 to the refund counter if the authority exists in the state, set the authority's nonce to `nonce + 1`, and set its code to empty if `address` is the zero address and to `0xef0100 || address` otherwise. The refund counter, including these credits, is capped by EIP-3529 (`gas_used / 5`). Because the sender nonce is incremented first, an authorization signed by the sender itself passes only with `nonce = tx.nonce + 1`.
Source: core/state_transition.go:505-551, core/state_transition.go:609-665
Source: params/protocol_params.go:35, params/protocol_params.go:96 (CallNewAccountGas, TxAuthTupleGas)
Observable: state

[SNET-TX-094] Delegation designators MUST be resolved as in the final EIP-7702: the EIP-3607 sender check MUST accept a sender whose code is a designator; `CALL`, `CALLCODE`, `DELEGATECALL`, `STATICCALL` and the top-level call MUST execute the code of the designated address, following one level only (a designator found there is executed as code and halts on `0xef`), so a designated precompile or native manager runs no code; `EXTCODESIZE`, `EXTCODECOPY` and `EXTCODEHASH` MUST operate on the 23-byte designator itself; each of the four call opcodes MUST charge, in addition to the EIP-2929 cost of the target, 100 gas if the designated address is warm and 2 600 if it is cold, and then warm it, both charges being deducted before the EIP-150 63/64 rule is applied.
Source: core/state_transition.go:365-370
Source: core/vm/evm.go:267, core/vm/evm.go:275, core/vm/evm.go:337, core/vm/evm.go:386, core/vm/evm.go:444, core/vm/evm.go:628-656 (resolveCode, resolveCodeHash)
Source: core/vm/operations_acl.go:252-317, core/vm/eips.go:325-330, core/vm/instructions.go:345-349
Observable: state

---

## 5. Fee payment and balance checks

### 5.1 Who pays

```python
def payer(msg):
    delegated = msg.fee_payer is not None and msg.fee_payer != msg.from_
    return msg.fee_payer if delegated else msg.from_
```

[SNET-TX-030] If a message has a fee payer different from the sender and the block number is below `ApplepieBlock`, the transaction MUST be invalid (`ErrTxTypeNotSupported: fee delegation type not supported`).
Source: core/state_transition.go:268-271
Observable: state

A type-`0x16` transaction whose fee payer equals its sender is not "delegated" in this sense: it is accepted before Applepie and is charged exactly like a type-`0x02` transaction.

### 5.2 Balance checks (buyGas)

Let `gas_limit` be the transaction gas, `gp` the message gas price (`SNET-TX-012`), `fc` the fee cap and `v` the value.

| Case | Account | Required balance | Error |
|---|---|---|---|
| not delegated | sender | `gas_limit * fc + v` | `ErrInsufficientFunds` (sender) |
| delegated | fee payer | `gas_limit * gp` | `ErrInsufficientFunds` (feePayer) |
| delegated | sender | `v` | `ErrInsufficientFunds` (sender) |

The debit is always `gas_limit * gp`, taken from the payer.

Source: core/state_transition.go:278-346

[SNET-TX-031] For a delegated message the node MUST require the fee payer's balance to be at least `gas_limit * gas_price` (the effective price, not the fee cap) and the sender's balance to be at least `value`, checking them separately; the transaction MUST be invalid if either check fails.
Source: core/state_transition.go:281-287, core/state_transition.go:315-336
Observable: state

[SNET-TX-032] For a non-delegated message the node MUST require the sender's balance to be at least `gas_limit * gas_fee_cap + value`, as upstream.
Source: core/state_transition.go:281-287, core/state_transition.go:304-314
Observable: state

### 5.3 Refund

[SNET-TX-034] At the end of execution the node MUST credit `gas_remaining * gas_price` (after the EIP-3529 refund cap) to the fee payer if the message has a fee payer (including a fee payer equal to the sender), and to the sender otherwise.
Source: core/state_transition.go:667-691
Observable: state

---

## 6. Blacklist enforcement

### 6.1 Transaction level

The checks below run inside the state transition after nonce, fee and balance checks and after intrinsic gas, and before the EVM runs. Their failure makes the transaction invalid (it cannot be included).

[SNET-TX-040] A transaction MUST be invalid if its sender is blacklisted in the state immediately before the transaction.
Source: core/state_transition.go:505-510
Observable: state

[SNET-TX-041] A transaction with a non-nil `to` MUST be invalid if `to` is blacklisted in the state immediately before the transaction. For a contract-creation transaction there is no recipient check.
Source: core/state_transition.go:512-515
Observable: state

### 6.2 EVM level

Inside the EVM the checks are not transaction-invalidating: they make the individual frame fail.

[SNET-TX-043] On `CALL`, `CALLCODE`, `DELEGATECALL` and `STATICCALL` (including the top-level call of a transaction), if the caller address or the target address is blacklisted, the call MUST fail with `ErrBlacklistedAccount` before any value transfer and without consuming the gas passed to it (the full gas is returned to the calling frame, which sees a failure status 0).
Source: core/vm/evm.go:195-202, core/vm/evm.go:302-309, core/vm/evm.go:355-362, core/vm/evm.go:403-410, core/vm/instructions.go:653-685 (opCall)
Observable: state

[SNET-TX-044] On `CREATE`, `CREATE2` and a contract-creation transaction, if the creating address is blacklisted, the creation MUST fail with `ErrBlacklistedAccount` before the nonce increment and without consuming the gas passed to it.
Source: core/vm/evm.go:479-482
Observable: state

For a top-level call the sender and target were already checked by `SNET-TX-040`/`SNET-TX-041`, so `SNET-TX-043` matters for nested calls. The `DELEGATECALL` check uses the calling contract's address as "caller", not the original sender. The delegation target of an EIP-7702 account is not checked: its code is loaded by `resolveCode` and runs in the context of the delegating account, without a separate call.

### 6.3 Failure modes

| Where it fails | Result | Block containing the tx |
|---|---|---|
| `SNET-TX-040`, `-041`, `-042` | transaction invalid | invalid |
| `SNET-TX-043`, `-044`, `-045` in a nested frame | frame fails; the transaction continues | valid; receipt status depends on the outer frames |
| `SNET-TX-044` at depth 0 (contract-creation transaction) | not reachable: the sender was checked by `SNET-TX-040` and nothing runs between the two checks | — |

[SNET-TX-060] A block MUST be invalid if executing any of its transactions in order returns a transaction-invalidating error from this chapter or from upstream (nonce, balance, fee, intrinsic gas, signature, type, blacklist, fee payer).
Source: core/state_processor.go:83-92, core/blockchain.go:1779-1782
Observable: header

The body is not executed when a PRE-PREPARE is accepted (`A-05`, `A-09`); it is executed after the block is committed. A block that violates this chapter can therefore gather a COMMIT quorum and still be rejected by every conforming node at insertion.

---

## 7. Fee accounting

### 7.1 Per-transaction flows

For a transaction with `gas_used` (after refunds), message gas price `gp`, base fee `b`, effective tip `t = gp - b`:

| Flow | Amount | From | To |
|---|---|---|---|
| Gas purchase | `gas_limit * gp` | payer | removed |
| Refund | `(gas_limit - gas_used) * gp` | — | payer |
| Tip | `gas_used * t` | — | `header.Coinbase` |
| Base fee | `gas_used * b` | — | not credited at transaction level |

Source: core/state_transition.go:344-345, core/state_transition.go:563-577, core/state_transition.go:667-691, consensus/wbft/engine/engine.go:86-88 (Author = header.Coinbase)

[SNET-TX-050] After execution the node MUST credit `gas_used * effective_tip` to `header.Coinbase`.
Source: core/state_transition.go:569-577, core/evm.go:49-54
Observable: state

[SNET-TX-051] The base-fee portion `gas_used * base_fee` MUST NOT be credited to any account during transaction execution. At block level, the finalization step credits `header.GasUsed * header.BaseFee` to the validators (`B-06`), so the base fee is not burned over the whole block.
Source: core/state_transition.go:563-577, consensus/wbft/engine/engine.go:941-946, consensus/wbft/engine/engine.go:972-1048
Observable: state

Because every transaction pays exactly `gas_used * b` of base fee and `header.GasUsed` is the sum of `gas_used`, the amount distributed by `B-06` equals the amount debited across the block. The coinbase receives the tips of all transactions plus its share and the dust of the base-fee distribution.

### 7.2 Summary formula

```python
def fee_effects(msg, gas_used, base_fee):
    gp  = msg.gas_price                                # SNET-TX-012
    tip = min(msg.gas_tip_cap, msg.gas_fee_cap - base_fee)
    assert gp == base_fee + tip
    payer_net_debit = gas_used * gp                    # plus value from sender
    coinbase_credit = gas_used * tip
    pending_base    = gas_used * base_fee              # credited in finalization (B-06)
```

---

## 8. Receipts and logs

### 8.1 Receipt encoding

Type-`0x16` and type-`0x04` receipts are typed receipts: consensus encoding `type || rlp([status, cumulative_gas_used, bloom, logs])`, included in the receipt trie like other typed receipts. The base code (go-ethereum v1.13.15) writes nothing for an unknown receipt type, so the `0x04` encoding is not defined by "as upstream".

[SNET-TX-070] A node MUST encode the receipt of a type-`0x16` or type-`0x04` transaction as an EIP-2718 typed receipt, the type byte followed by `rlp([status, cumulative_gas_used, bloom, logs])`, in the receipt trie.
Source: core/types/receipt.go:130-148, core/types/receipt.go:210-226, core/types/receipt.go:321-337
Observable: header

The storage encoding (`ReceiptForStorage`) appends `EffectiveGasPrice` as an optional trailing element when present. This is a database format, not consensus data.

Source: core/types/receipt.go:104-110, core/types/receipt.go:274-312

[SNET-TX-071] On an Anzeon chain the receipt's `effectiveGasPrice` (as returned by RPC) MUST equal the message gas price of `SNET-TX-012`, that is, it reflects the governance-tip override for non-authorized senders.
Source: core/state_processor.go:157-159, internal/ethapi/api.go:1883
Observable: rpc

### 8.2 `AuthorizedTxExecuted` log

When the sender is authorized, the transaction appends one log after everything else:

| Field | Value |
|---|---|
| address | `AccountManagerAddress` = `0x0000000000000000000000000000000000B00003` |
| topics | `[0x40e728a89c7f5b192cf1c1b747fb64d51d81c7a2b3ed4607b94d3a1e6a3e0373]` = `keccak256("AuthorizedTxExecuted()")` |
| data | empty |

Source: core/state_transition.go:587-599, params/protocol_params.go:219-223

The log is part of the receipt, so it affects `Header.ReceiptHash` and `Header.Bloom`.

Its purpose is to let a node reconstruct `effectiveGasPrice` without state. Receipts obtained by snap sync carry no `EffectiveGasPrice`; `DeriveFields` then recomputes it:

```python
def derive_effective_gas_price(receipt, tx, header):
    tip = tx.gas_tip_cap
    if header_gas_tip(header) is not None and not last_log_is_authorized_tx_executed(receipt):
        tip = header_gas_tip(header)
    return min(tip + header.base_fee, tx.gas_fee_cap)
```

Source: core/types/receipt.go:396-411, core/types/receipt.go:452-466, core/rawdb/accessors_chain.go:661

No contract can forge this log: a log's address is the executing contract, and `AccountManagerAddress` has no EVM code (it is a native manager, §9.5).

### 8.3 Native `Transfer` log

On an Anzeon chain every non-zero native-coin movement caused by the EVM emits an ERC-20 style log so that the native coin looks like a token at the NativeCoinAdapter address.

| Field | Value |
|---|---|
| address | `ChainConfig.Anzeon.SystemContracts.NativeCoinAdapter.Address` (the genesis configuration value) |
| topics | `[0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef, pad32(from), pad32(to)]` |
| data | `pad32(amount)` big-endian |

Source: core/vm/evm.go:589-611, params/protocol_params.go:222

[SNET-TX-073] A node MUST append a `Transfer` log with the fields above, at the point of the transfer and inside the frame's journal (so that it is removed if the frame reverts), for: a `CALL` or top-level call with non-zero value (after the transfer), a `CREATE`/`CREATE2`/creation transaction with non-zero value, and a `SELFDESTRUCT` whose contract balance is non-zero (`to` = beneficiary).
Source: core/vm/evm.go:240-243, core/vm/evm.go:507-510, core/vm/instructions.go:812-815, core/vm/instructions.go:834-837
Observable: state, rpc

---

## 9. Other execution differences

### 9.1 Value transfer restrictions

[SNET-TX-080] On an Anzeon chain a `CALL` (or top-level call) with non-zero value MUST fail with `ErrZeroAddressTransfer` if the target is the zero address, and with `ErrValueTransferToPrecompile` if the target is an active precompile or native manager. The failure happens before the transfer and returns the supplied gas to the caller; at depth 0 the transaction is included with status 0 and consumes only intrinsic gas.
Source: core/vm/evm.go:203-220
Observable: state

### 9.2 PREVRANDAO

[SNET-TX-081] When executing a block whose difficulty is 0 or 1, the value returned by opcode `0x44` (`PREVRANDAO`) MUST be `header.MixDigest`, which on a WBFT chain is the randao mix (`A-02`). There is no `DIFFICULTY` semantics for `0x44` on an Anzeon chain.
Source: core/evm.go:59-62, core/vm/jump_table.go:117-124, core/vm/instructions.go:478-481
Observable: state

### 9.3 Instruction set

The Anzeon instruction set is London plus: `PREVRANDAO` at `0x44`, `PUSH0` (EIP-3855), initcode metering for `CREATE`/`CREATE2` (EIP-3860), `TLOAD`/`TSTORE` (EIP-1153), `MCOPY` (EIP-5656), `SELFDESTRUCT` restricted to same-transaction creations (EIP-6780), and EIP-7702 call-gas rules (SNET-TX-094). `BLOBHASH` and `BLOBBASEFEE` are not enabled.

Source: core/vm/jump_table.go:117-143, core/vm/interpreter.go:59-68

[SNET-TX-082] A node MUST execute Anzeon blocks with exactly the instruction set above; in particular `0x49` and `0x4a` MUST be invalid opcodes.
Source: core/vm/jump_table.go:131-133
Observable: state

The reference decides "created in the same transaction" with the per-transaction flag of the v1.13.15 state object, which is set whenever an account object is created, not only by contract creation. Before EIP-7702 the two coincided in practice; with EIP-7702 they differ.

[SNET-TX-084] At the start of every transaction the native manager addresses MUST be added to the EIP-2929 access list together with the precompiles.
Source: core/state/statedb.go:1368-1394, core/state_transition.go:521
Observable: state

### 9.4 Precompiles

| Address | Contract | Active |
|---|---|---|
| `0x01`-`0x09` | upstream Istanbul/Berlin set | Anzeon |
| `0x0a` | KZG point evaluation | Anzeon (present even though blob transactions are rejected) |
| `0x…B00001` | BLS proof-of-possession verifier, 45 000 gas | Anzeon |
| `0x0100` | P-256 verify (EIP-7951: gas 6 900, not the 3 450 of RIP-7212; SNET-TX-089) | Boho and later |

Source: core/vm/contracts.go:127-155, core/vm/contracts.go:197-204, params/protocol_params.go:208-209

[SNET-TX-085] A node MUST use the Anzeon precompile set before `BohoBlock` and the Boho set (Anzeon set plus P-256 at `0x0100`) from `BohoBlock` on.
Source: core/vm/evm.go:40-60, core/vm/contracts.go:141-155
Observable: state

[SNET-TX-089] From `BohoBlock` on, a call to the P-256 precompile at `0x0000000000000000000000000000000000000100` MUST cost exactly `P256_VERIFY_GAS = 6 900` gas (`B-01` §10, SNET-CFG-010) for every input and MUST NOT fail. If the input is exactly 160 bytes `hash(32) ‖ r(32) ‖ s(32) ‖ x(32) ‖ y(32)` (big-endian), `(x, y)` is a point of the NIST P-256 curve (coordinates below the field prime, not the point at infinity), `1 ≤ r, s ≤ n − 1`, and ECDSA verification of the 32-byte `hash` with `(r, s)` and `(x, y)` succeeds (there is no low-S rule), the output MUST be the 32-byte word `0x00…01`; in every other case, including an input of another length, the output MUST be empty. A failed verification is therefore not an error: the call succeeds and the gas passed to it beyond 6 900 is returned. This is the behaviour of EIP-7951; the gas cost 3 450 of RIP-7212 MUST NOT be used.
Source: core/vm/contracts.go:1225-1251 (p256Verify), params/protocol_params.go:171 (P256VerifyGas)
Source: crypto/secp256r1/verifier.go:27-33 (IsOnCurve, then crypto/ecdsa.Verify)
Source: core/vm/testdata/precompiles/p256Verify.json (782 vectors, all with gas 6 900)
Observable: state

The BLS PoP precompile's input format and output are specified in `B-04` (it is used by GovValidator).

### 9.5 Native managers

Two addresses behave as built-in contracts with a 4-byte selector dispatch. They have no code in state.

| Address | Method | Params | Caller required | Call kind | Gas |
|---|---|---|---|---|---|
| `0x…B00002` NativeCoinManager | `mint(address,uint256)` | 2 | NativeCoinAdapter (genesis address) | `CALL` | 4 500 (+25 000 if `to` does not exist) when `amount > 0`; 0 when `amount = 0` |
| | `burn(address,uint256)` | 2 | NativeCoinAdapter | `CALL` | 4 500 |
| | `transfer(address,address,uint256)` | 3 | NativeCoinAdapter | `CALL` | 9 000 (+25 000 if `to` does not exist) when `amount > 0`; 0 when `amount = 0`; the balance check precedes the gas check |
| `0x…B00003` AccountManager | `blacklist(address)` | 1 | GovCouncil (genesis address) | `CALL` | 4 500 (+25 000 if new) |
| | `unBlacklist(address)` | 1 | GovCouncil | `CALL` | 4 500 |
| | `authorize(address)` | 1 | GovCouncil | `CALL` | 4 500 (+25 000 if new) |
| | `unAuthorize(address)` | 1 | GovCouncil | `CALL` | 4 500 |
| | `isBlacklisted(address)` | 1 | any | any | 0; returns 32-byte bool |
| | `isAuthorized(address)` | 1 | any | any | 0; returns 32-byte bool |

Source: core/vm/native_manager.go:82-131, core/vm/native_manager.go:195-457, core/vm/native_manager.go:463-489, params/protocol_params.go:212-220

[SNET-TX-086] A call to a native manager MUST fail and consume all supplied gas (the error is not a revert, so the EVM keeps none of it, `B-04` SNET-SYS-080) when the input is shorter than 4 bytes (`ErrInvalidSelectorLength`), the selector is unknown (`ErrInvalidMethod`), the call kind or caller is not allowed (`ErrInvalidCallContext`, `ErrUnauthorized`), or the argument length is not exactly `32 * ParamCount` (`ErrInvalidInputLength`), checked in that order.
Source: core/vm/native_manager.go:137-156
Observable: state

### 9.6 Code size limits

[SNET-TX-088] On an Anzeon chain the maximum size of deployed contract code (the EIP-170 limit) MUST be `MAX_CODE_SIZE = 253 952` bytes (`B-01` §10, SNET-CFG-007), not 24 576. A `CREATE`, `CREATE2` or contract-creation transaction whose returned code is longer than `MAX_CODE_SIZE` MUST fail with an exceptional halt that consumes all gas of the creating frame. The initcode limit that `CREATE` and `CREATE2` apply (EIP-3860, enabled by the Anzeon instruction set, §9.3) MUST be `MAX_INITCODE_SIZE = MAX_CODE_SIZE = 253 952` bytes (`B-01` §10, SNET-CFG-029), not 49 152, and exceeding it also consumes all gas of the frame; the initcode word gas stays 2 and the code deposit cost stays 200 gas per byte.
Source: params/protocol_params.go:140-141 (MaxCodeSize, MaxInitCodeSize)
Source: core/vm/evm.go:526-529 (EIP-170 check)
Source: core/vm/gas_table.go:306-332 (EIP-3860 check in the CREATE/CREATE2 gas functions)
Observable: state

---

## 10. Worked fee examples

Common parameters (the preset values of `params/protocol_params.go:135-138`): base fee `B = 20 000 gwei` (`2·10^13` wei, the Anzeon minimum), governance gas tip `T = 27 600 gwei` (`InitialGasTip`). 1 coin = `10^18` wei = `10^9` gwei. All transfers use `gas_used = 21 000` unless stated.

**E-1 Non-authorized EIP-1559 transfer.** `max_fee = 100 000 gwei`, `max_priority_fee = 1 gwei`, value 5 coin.
- Tip cap after override: `T = 27 600`. `gp = min(27 600 + 20 000, 100 000) = 47 600 gwei`.
- Balance check (sender): `21 000 × 100 000 gwei + 5 coin = 2.1 + 5 = 7.1 coin`.
- Sender net debit: `21 000 × 47 600 gwei = 0.9996 coin` plus 5 coin value.
- Coinbase: `21 000 × 27 600 gwei = 0.5796 coin`. Base fee pending for `B-06`: `0.42 coin`.
- Receipt `effectiveGasPrice = 47 600 gwei`; no `AuthorizedTxExecuted` log; one `Transfer` log (5 coin).

**E-2 Fee cap between max(B, T) and B + T.** Same as E-1 with `max_fee = 30 000 gwei`.
- `30 000 >= max(20 000, 27 600)`: valid. `gp = min(47 600, 30 000) = 30 000`. `tip = 10 000 gwei`.
- Coinbase: `0.21 coin`, less than the governance tip would give. Receipt `effectiveGasPrice = 30 000 gwei`.

**E-3 Fee cap below the governance tip.** `max_fee = 25 000 gwei` (above `B`).
- After override `tip_cap = 27 600 > fee_cap = 25 000`: `ErrTipAboveFeeCap`. The transaction is invalid; a block containing it is invalid (`SNET-TX-013`, `SNET-TX-060`).

**E-4 Non-authorized legacy transaction.** `gas_price = 1 000 000 gwei`.
- `tip_cap = 27 600`, `fee_cap = 1 000 000`. `gp = 47 600 gwei` (upstream would charge 1 000 000).
- Balance check: `21 000 × 1 000 000 gwei = 21 coin` (upstream check on fee cap); net debit `0.9996 coin`.

**E-5 Authorized sender, zero tip.** `max_fee = 20 000 gwei`, `max_priority_fee = 0`.
- No override. `gp = 20 000`, `tip = 0`. Coinbase `0`. Base `0.42 coin` pending.
- Receipt ends with the `AuthorizedTxExecuted` log; `effectiveGasPrice = 20 000 gwei`.

**E-6 Authorized sender, high tip.** `max_fee = 200 000`, `max_priority_fee = 100 000`. `gp = 120 000`, `tip = 100 000`: coinbase `2.1 coin`. Authorized senders can pay more than `T`; non-authorized senders cannot.

**E-7 Fee-delegated transfer (after Applepie).** Sender `S` (non-authorized), fee payer `P ≠ S`, value 1 coin, `gas = 50 000`, `gas_used = 30 000` (a contract call), `max_fee = 100 000 gwei`.
- `gp = 47 600 gwei`.
- Checks: `P.balance >= 50 000 × 47 600 gwei = 2.38 coin` (not `50 000 × 100 000 = 5 coin`); `S.balance >= 1 coin`.
- `P` debited `2.38`, refunded `20 000 × 47 600 gwei = 0.952`; net `1.428 coin`. `S` pays only the value.
- Coinbase: `30 000 × 27 600 gwei = 0.828 coin`. Base pending: `0.6 coin`.
- If `S` were authorized, the tip would be `S`'s own `max_priority_fee` (authorization is looked up for the sender, never the fee payer).

**E-8 Blacklisted fee payer.** As E-7 but `P` blacklisted: the EVM call runs, then `ErrBlacklistedAccount{P}` is returned. The transaction is invalid; the block is invalid.

**E-9 Block totals.** A block with E-1, E-5 and E-7: `header.GasUsed = 21 000 + 21 000 + 30 000 = 72 000`. Base fee distributed in finalization: `72 000 × 20 000 gwei = 1.44 coin` (= `0.42 + 0.42 + 0.6`). Coinbase tips: `0.5796 + 0 + 0.828 = 1.4076 coin`, plus its base-fee share and dust from `B-06`.

---

## 11. Block-building order (informative)

This section describes what a go-stablenet proposer does. Other orders produce valid blocks.

0. Before ordering, the proposer drops from each *remote* account's pending list the first transaction whose effective tip `min(cached_tip_cap, fee_cap − base_fee)` is below the current governance gas tip, together with all later transactions of that account; local accounts are not filtered. `cached_tip_cap` is fixed when the transaction enters the pool: the header gas tip at that time for a non-authorized sender, the transaction's own tip cap for an authorized sender. A non-authorized remote transaction with `fee_cap < base_fee + gas_tip` is therefore never proposed by the reference implementation, although it is valid (SNET-TX-013, example E-2).
1. Pending transactions from the pool are split into *local* accounts first and *remote* accounts second; each group is committed separately, locals first.
2. Within a group, the head transaction of each account competes in a heap. If Anzeon is enabled:
   - an authorized sender (by the block-building state) always precedes a non-authorized one;
   - between two authorized senders: higher miner tip first, then earlier pool arrival;
   - between two non-authorized senders: earlier pool arrival first, fees are ignored (FIFO).
3. Within an account, nonce order.
4. A transaction that fails with a nonce-too-low error is skipped (shift); any other error drops the rest of that account's transactions from this block.

Source: miner/worker.go:1255-1286, miner/ordering.go:68-101, miner/ordering.go:134-160, miner/worker.go:1068-1084
Source: miner/worker.go:1230-1240, core/txpool/legacypool/legacypool.go:579-605, core/txpool/validation.go:411-416, core/types/transaction.go:388-402 (item 0)

The miner tip used for sorting authorized senders is `min(gas_tip_cap, fee_cap - base_fee)`. For a non-authorized sender whose tip cap exceeds the header gas tip, the sort key is the header gas tip itself (the `fee_cap - base_fee` bound is not applied); for any other non-authorized sender it is `min(gas_tip_cap, fee_cap - base_fee)`. Both can differ from the tip actually charged (`min(header_gas_tip, fee_cap - base_fee)`); this does not matter because non-authorized senders are sorted by time only.

Source: miner/ordering.go:40-60

Because authorization is read once from the block-building state before the block is filled, a transaction in the same block that changes a sender's authorization does not reorder that block.

The proposer's `totalFees` log value adds the base fee to each transaction's tip, reflecting that the base fee is distributed rather than burned.

Source: miner/worker.go:1470-1485

---

## 12. Transaction-pool policy (informative)

These rules decide what a go-stablenet node gossips and proposes; they are not block-validity rules.

| Rule | Behaviour | Source |
|---|---|---|
| Type gates | `0x16` rejected before Applepie; `0x04` rejected without Anzeon; `0x03` rejected without Cancun | core/txpool/validation.go:72-80 |
| Accepted types (legacy pool) | `0x00`, `0x01`, `0x02`, `0x16`, `0x04` | core/txpool/legacypool/legacypool.go:655-661 |
| Minimum fee | `fee_cap >= MinBaseFee + MinTip`, where `MinTip` is the pool gas tip for remote transactions and 0 for local ones (legacy: gas price) | core/txpool/validation.go:121-131, core/txpool/legacypool/legacypool.go:662-666 |
| Minimum tip | `tx.gas_tip_cap >= pool gas tip` (remote only; the pool gas tip follows the GovValidator value) | core/txpool/validation.go:119-121, miner/worker.go:361-388 |
| Fee payer signature | verified with `RecoverFeePayer` | core/txpool/validation.go:133-138 |
| Blacklist | sender, `to` and fee payer rejected | core/txpool/validation.go:250-259, core/txpool/validation.go:284-286 |
| Balances | fee-delegated: sender `>= value`, fee payer `>= gas × fee_cap`; cumulative pending expenditure per account across both roles (PR #116) | core/txpool/validation.go:268-293 |
| Gas-tip increase | remote transactions whose cached Anzeon tip (the header gas tip at admission for non-authorized senders) is below the new tip are dropped | core/txpool/legacypool/legacypool.go:474-497, core/txpool/legacypool/legacypool.go:2113-2130 |
| `miner_setGasPrice` | returns `false` and does nothing on Anzeon | eth/api_miner.go:61-71 |

The pool's fee-payer balance check uses the fee cap, which is stricter than the execution check `SNET-TX-031`.

---

## 13. Error catalog

| Error | Text | Raised by | Invalidates tx |
|---|---|---|---|
| `ErrBlacklistedAccount` (core) | `blacklisted account: <addr>` | `SNET-TX-040`, `-041`, `-042` | yes |
| `ErrBlacklistedAccount` (vm) | `blacklisted account: <addr>` | `SNET-TX-043`, `-044`, `-045` | no (frame fails) |
| `ErrTxTypeNotSupported` | `transaction type not supported[: fee delegation type not supported]` | `SNET-TX-020`, `-030` | yes |
| `ErrInsufficientFunds` | `insufficient funds for gas * price + value: {sender\|feePayer} <addr> have <x> want <y>` | `SNET-TX-031`, `-032` | yes |
| `ErrTipAboveFeeCap` | `max priority fee per gas higher than max fee per gas` | `SNET-TX-013` | yes |
| `ErrEmptyAuthList` | `EIP-7702 transaction with empty auth list` | `SNET-TX-091` | yes |
| `ErrZeroAddressTransfer` | `transfer to zero address not allowed` | `SNET-TX-080` | no |
| `ErrValueTransferToPrecompile` | `value transfer to precompiled contract disallowed` | `SNET-TX-080` | no |
| native manager errors | `native manager: …` | `SNET-TX-086` | no |

Source: core/error.go:64-101, core/error.go:121-122, core/error.go:142-147, core/vm/errors.go:44-47, core/vm/errors.go:83-89, core/types/transaction_signing.go:33-36, core/vm/native_manager.go:42-57
