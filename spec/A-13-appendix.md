# A-13. Appendix: error catalog, log catalog, lineage

- Status: draft
- Reference implementation: go-stablenet `740526d03`
- Section 1 lists error values and is partly normative only through the chapters that cite it: an error value is never sent on the wire, so conformance never depends on the *identity* of an error, only on the outcome (message dropped, block rejected, RPC error). Sections 2 and 3 are informative.

---

## 1. Error catalog

### 1.1 How to read the tables

- Paths are relative to `consensus/wbft/` unless they start with another top-level directory.
- "Returned at" lists every non-test site in `consensus/wbft/**` that returns or wraps the value, with the enclosing function. The list was produced by walking the Go syntax tree of every non-test file (identifier uses of each package-level error variable, and every `errors.New` / `fmt.Errorf` call), then checked by hand.
- "Observable" says how another party can see the outcome:
  - **header**: a block or header carrying the defect is rejected; peers see the node refuse the block (and the reference logs the import error text).
  - **network**: a consensus message is dropped and not relayed; there is no reply to the sender.
  - **rpc**: the error text is returned by an RPC method (`istanbul_*` API, `B-09`).
  - **log**: only visible in the node's log.
  - **local**: returned to a local caller (start/stop, engine-specific calls); no external effect.
  - **unused**: defined but never returned at this commit.

### 1.2 Package `wbft` (`errors.go`, `utils.go`)

| Error value | Message | Returned at | Observable |
|---|---|---|---|
| `ErrUnauthorizedAddress` | `unauthorized address` | `utils.go:72` (CheckValidatorSignature): recovered signer not in the validator set; `backend/backend.go:161` (Broadcast): the local node is not a validator | network, log |
| `ErrStoppedEngine` | `stopped engine` | `backend/engine.go:292` (Stop), `backend/handler.go:75` (HandleMsg: consensus message while the core is stopped), `backend/handler.go:142` (NewChainHead) | network (A-07: peer handling), local |
| `ErrStartedEngine` | `started engine` | `backend/engine.go:250` (Start) | local |
| `ErrGasTipContractUnavailable` | `gas tip contract unavailable` | `engine/engine.go:642` (getGasTip), propagated as fatal at `engine/engine.go:332` (verifyCascadingFields); unreachable, because `GetGasTip` never returns `nil` (`B-06` SNET-FIN-017, `A-08` WBFT-HDR-111) | unused (unreachable) |
| `GasTipMismatchError{Have, Want}` | `invalid gas tip: have %d, want %d` | `engine/engine.go:1292` (verifyGasTip), fatal at `engine/engine.go:335-338` | header |

### 1.3 Package `wbftcommon` (`common/errors.go`)

| Error value | Message | Returned at | Observable |
|---|---|---|---|
| `ErrInvalidProposal` | `invalid proposal` | `backend/backend.go:218` (Commit), `backend/backend.go:263` (Verify): proposal is not a block | network |
| `ErrInvalidSignature` | `invalid signature` | `backend/backend.go:299` (CheckSignature): recovered address differs from the expected one (randao reveal, A-02 §7.2) | header |
| `ErrUnknownBlock` | `unknown block` | `engine/engine.go:198` (verifyHeader: nil number), `engine/engine.go:356` (verifySigner: genesis), `engine/engine.go:466` and `backend/engine.go:126` (VerifySeal: genesis), `backend/api.go:96`, `backend/api.go:106`, `backend/api.go:142`, `backend/api.go:155` (RPC: block not found) | header, rpc |
| `ErrUnauthorized` | `unauthorized` | `engine/engine.go:367` (verifySigner: coinbase not a validator) | header |
| `ErrInvalidDifficulty` | `invalid difficulty` | `engine/engine.go:214` (verifyHeader), `engine/engine.go:471` (VerifySeal) | header |
| `ErrInvalidExtraDataFormat` | `invalid extra data format` | `engine/engine.go:303` (verifyCascadingFields: extra does not decode) | header |
| `ErrInvalidUncleHash` | `non empty uncle hash` | `engine/engine.go:169` (VerifyBlockProposal), `engine/engine.go:209` (verifyHeader), `engine/engine.go:455` (VerifyUncles) | header, network |
| `ErrBlacklistedHash` | `blacklisted hash` | `backend/backend.go:269` (Verify: proposal is a known bad block) | network |
| `ErrInvalidTimestamp` | `invalid timestamp` | `engine/engine.go:271` (verifyCascadingFields: `parent.Time + block_period > Time`) | header |
| `ErrInvalidPreparedSeals` | `invalid prepared seals` | `engine/engine.go:104` (writePreparedSeals: no seals to write), `engine/engine.go:432` (verifySeals) | header, local |
| `ErrInvalidPrevPreparedSeals` | `invalid prev prepared seals` | `engine/engine.go:397` (verifyPrevSeals) | header |
| `ErrEmptyPreparedSeals` | `zero prepared seals` | `engine/engine.go:425` (verifySeals), `engine/engine.go:525` (Prepare: the canonical header at `number − 1` has a nil prepared seal), `backend/engine.go:357` (InheritExtra) | header, local |
| `ErrEmptyPrevPreparedSeals` | `zero prev prepared seals` | `engine/engine.go:392` (verifyPrevSeals) | header |
| `ErrInvalidCommittedSeals` | `invalid committed seals` | `engine/engine.go:119` (writeCommittedSeals), `engine/engine.go:445` (verifySeals) | header, local |
| `ErrInvalidPrevCommittedSeals` | `invalid prev committed seals` | `engine/engine.go:407` (verifyPrevSeals) | header |
| `ErrEmptyCommittedSeals` | `zero committed seals` | `engine/engine.go:438` (verifySeals), `engine/engine.go:529` (Prepare: the canonical header at `number − 1` has a nil committed seal), `backend/engine.go:360` (InheritExtra) | header, local |
| `ErrEmptyPrevCommittedSeals` | `zero prev committed seals` | `engine/engine.go:403` (verifyPrevSeals) | header |
| `ErrInvalidSeal` | `invalid seal` | `engine/engine.go:135` (aggregateSeal: a seal is not 96 bytes), `engine/engine.go:1365` (verifyAggregatedSeal: BLS verification false) | header (replaced, not wrapped, by the four seal errors above; the original text appears only in log entries L140 – L143), local (`aggregateSeal`) |
| `ErrEmptySeals` | `zero seals` | `engine/engine.go:1324` (getSignerAddress: absent seal) | rpc, header (epoch computation, A-04) |
| `ErrMismatchTxhashes` | `mismatch transactions hashes` | `engine/engine.go:164` (VerifyBlockProposal) | network |
| `ErrInvalidMessage` | `invalid message` | `messages/decode.go:58` (Decode: unknown code); unreachable, because the only caller `core/handler.go:197` is preceded by the code check at `core/handler.go:191-193` | unused (unreachable) |
| `ErrFailedDecodePreprepare` | `failed to decode PRE-PREPARE message` | `messages/decode.go:32` (Decode, code 0x12); `messages/roundchange.go:289` (RoundChange.DecodeRLP: prepared block hash ≠ prepared digest; masked by the caller as `ErrFailedDecodeRoundChange`) | network, log |
| `ErrFailedDecodeCommit` | `failed to decode COMMIT message` | `messages/decode.go:39` (code 0x13, PREPARE, sic), `messages/decode.go:46` (code 0x14) | network, log |
| `ErrFailedDecodeRoundChange` | `failed to decode ROUND-CHANGE message` | `messages/decode.go:53` (code 0x15) | network, log |
| `ErrInvalidSpecificCall` | `invalid method name for engine specific function` | `backend/engine.go:307`, `:311`, `:315`, `:319`, `:323`, `:331`, `:338`, `:342`, `:346`, `:381`, `:385`, `:389`, `:405`, `:411` (CallEngineSpecific: wrong method or arguments) | local |
| `ErrIsNotWBFTBlock` | `block is not a wbft block` | `backend/api.go:420` (GetWbftExtraInfo on a non-Anzeon chain) | rpc |
| `ErrEpochInfoIsNotNil` | `epoch info should be nil for non-epoch block` | `engine/engine.go:959` (processFinalize) | header |
| `ErrStateUnavailable` | `state unavailable for verification` | `engine/engine.go:374` (verifySigner: parent state missing), tolerated at `engine/engine.go:292` | log (Trace) |
| `ErrBlacklistedSigner` | `blacklisted signer` | `engine/engine.go:377` (verifySigner) | header |
| `ErrInvalidMixDigest`, `ErrInvalidNonce`, `ErrInvalidVotingChain`, `ErrInvalidVote`, `ErrInconsistentSubject`, `ErrNotFromProposer`, `ErrIgnored`, `ErrOldMessage`, `ErrInvalidSigner`, `ErrInvalidGenesis`, `ErrFailedDecodePrepare` | (see `common/errors.go:47-139`) | never returned | unused |

Note: the randao mix mismatch is reported with an inline error (`invalid randao mix: have %x, want %x`, `engine/engine.go:318`), not with `ErrInvalidMixDigest`.

### 1.4 Package `core` (`core/errors.go`)

These values never leave the node. A message that yields one of them is dropped and is not relayed, with two exceptions. `errFutureMessage` stores it in the backlog; it is relayed later if backlog processing succeeds. `errExtraSealMessage` routes it to the extra-seal store; a message stored there, or silently ignored by it, counts as processed without error and **is relayed** (`A-05` WBFT-SM-011, `A-07` WBFT-NET-042). Only a digest mismatch, a seal-verification failure or `errInvalidExtraSealMessage` in the extra-seal store prevents that relay.

| Error value | Message | Returned at | Observable |
|---|---|---|---|
| `errNotFromProposer` | `message does not come from proposer` | `core/preprepare.go:125` (handlePreprepareMsg) | network, log |
| `errFutureMessage` | `future message` | `core/backlog.go:142`, `:152`, `:171`, `:180` (checkMessage); `core/request.go:75` (checkRequestMsg); tested at `core/request.go:106` (processPendingRequests), `core/backlog.go:288`, `core/handler.go:124`, `core/handler.go:215` | network (delayed processing) |
| `errOldMessage` | `old message` | `core/backlog.go:144`, `:163` (checkMessage); `core/request.go:73` | network |
| `errInvalidMessage` | `invalid message` | `core/backlog.go:127`, `:178`, `:189`, `:199` (checkMessage); `core/commit.go:98`, `:105`, `:111`; `core/prepare.go:95`, `:102`, `:108`; `core/extraseal.go:56`, `:72`; `core/handler.go:244` (deliverMessage); `core/request.go:65`; tested at `core/request.go:39` (and passed through by handleRequest) | network, log |
| `errInvalidSeal` | `invalid seal` | `core/core.go:481` (verifySeal: seal does not decode) | network |
| `errInvalidSigner` | `message not signed by the sender` | `core/core.go:485` (verifySeal: BLS verification false); `core/handler.go:281` (verifySignatures: recovery or membership failure) | network, log |
| `errInvalidPreparedBlock` | `invalid prepared block in round change messages` | `core/preprepare.go:131` (sequence ≠ proposal number), `core/preprepare.go:143` (justification fails) | network, log |
| `errExtraSealMessage` | `extra seal message` | `core/backlog.go:161`, `:187`, `:196` (checkMessage); tested at `core/backlog.go:287`, `core/handler.go:218` | network |
| `errInvalidExtraSealMessage` | `invalid extra seal message` | `core/extraseal.go:84` (addToExtraSeal: wrong code) | network (the message is not relayed; no log record is written) |
| `errCurrentIsNil` | `current is nil` | `core/request.go:69` (checkRequestMsg) | local |
| `errFutureViewTooFar` | `future view too far ahead: sequence or round difference too large` | `core/backlog.go:133` (checkMessage) | network |
| `justificationError` (type) | `round-change message view does not match target view` / `prepare message round mismatch` / `prepare message digest mismatch` | `core/justification.go:61`, `:77`, `:81` (isJustified) | network, log |

### 1.5 Package `backend` (`backend/handler.go`)

| Error value | Message | Returned at | Observable |
|---|---|---|---|
| `errDecodeFailed` | `fail to decode wbft message` | `backend/handler.go:58` (decode: `0x11` payload is not an RLP string), `backend/handler.go:80` (HandleMsg) | network (A-07: returned to the protocol handler) |
| `errPayloadReadFailed` | `unable to read payload from message` | `backend/handler.go:63` (decode); replaced by `errDecodeFailed` in `HandleMsg` (`backend/handler.go:80`), so the value itself never reaches the protocol handler | network (as `errDecodeFailed`, A-07) |

### 1.6 Errors created in place

| Location | Function | Message | Observable |
|---|---|---|---|
| `backend/api.go:211`, `:215`, `:235`, `:245`, `:248`, `:254` | calculateBlockRange | block-range errors of `istanbul_status` (`pass the end block number`, `pass the start block number`, `unsupported block number: %d`, `start block number should be less than end block number`, `end block number should be less than or equal to current block height`, `requested range too large: %d blocks (max %d)`) | rpc |
| `backend/api.go:264`, `:269`, `:276` | analyzeBlock | `block %d not found`, `block %d: failed to extract WBFT extra: %w`, `block %d: failed to get validators: %w` | rpc |
| `backend/api.go:425` | GetWbftExtraInfo | `block %d not found` | rpc |
| `core/handler.go:193` | handleEncodedMsg | `invalid message event code %v` | log |
| `core/justification.go:54`, `:68`, `:107`, `:128`, `:139`, `:144`, `:147` | isJustified and helpers | `number of roundchange messages is less than required quorum of messages`; `number of prepared messages is less than required quorum of messages`; `quorum of roundchange messages with nil prepared round not found`; `quorum of roundchange messages with prepared round and proposal not found`; `number of prepare messages is less than quorum of messages`; `prepared message digest does not match roundchange prepared digest`; `round number in prepared message does not match prepared round in roundchange` | network, log |
| `core/roundchange.go:125`, `:134`, `:184` | handleRoundChangeMsg | `prepared block number %v does not match current sequence %v`; `prepared block hash %s does not match prepared digest %s in ROUND-CHANGE message`; `no proposal as pending request is nil` | network, log |
| `engine/engine.go:182` | VerifyBlockProposal | `unknown parent hash` | network |
| `engine/engine.go:218`, `:221`, `:225`, `:228`, `:233`, `:235`, `:237` | verifyHeader | `invalid gasLimit: have %v, max %v`; `wbft does not support shanghai fork`; `invalid withdrawalsHash: have %x, expected nil`; `wbft does not support cancun fork`; `invalid excessBlobGas …`; `invalid blobGasUsed …`; `invalid parentBeaconRoot …` | header |
| `engine/engine.go:275`, `:280` | verifyCascadingFields | `invalid gasUsed: have %d, gasLimit %d`; `invalid baseFee before fork: have %d, want <nil>` | header |
| `engine/engine.go:314`, `:318` | verifyCascadingFields | `failed to verify randao reveal signature: %w`; `invalid randao mix: have %x, want %x` | header |
| `engine/engine.go:374`, `:377` | verifySigner | wraps `ErrStateUnavailable` / `ErrBlacklistedSigner` | header, log |
| `engine/engine.go:545`, `:555` | Prepare, WriteRandao | `failed to write wbft extra: %w`; `failed to sign randao reveal: %w` | local |
| `engine/engine.go:624`, `:632` | getGasTip | `WBFT: GovValidator contract is not enabled`; `WBFT: parent state root is empty` | header, local |
| `engine/engine.go:783`, `:839`, `:865` | buildEpochInfo | `failed to find valid proposer`; `seal count exceed the range for non validator in prior epoch`; `WBFT: Invalid Diligence %d exceeds maximum` | header |
| `engine/engine.go:977`, `:995`, `:1000`, `:1031`, `:1038`, `:1043` | distributeBaseFee | `WBFT: baseFee is nil …`; `WBFT: validator candidate index out of range …`; `WBFT: nil candidate at index …`; `WBFT: %s share overflows uint256 …`; `WBFT: negative dust …`; `WBFT: dust overflows uint256 …` | header |
| `engine/engine.go:1223`, `:1228`, `:1232`, `:1237`, `:1244`, `:1248`, `:1254`, `:1258` | verifyEpoch | `WBFT: epochInfo is nil`; `WBFT: mismatch in candidate sizes`; `WBFT: The two candidates do not match at index %d …`; `WBFT: Diligence mismatch at index %d …`; `WBFT: mismatch in validator sizes`; `WBFT: The two validators do not match`; `WBFT: mismatch in BLS public key sizes`; `WBFT: The two BLS public keys do not match` | header |
| `engine/engine.go:1331` | getSignerAddress | `validator address is zero` | rpc, header |
| `engine/engine.go:1342`, `:1349` | verifyAggregatedSeal | `lack of seal count`; `sealer is not validator` | header (replaced by the seal errors of §1.3; text visible only in L140 – L143), log |
| `engine/engine.go:1443` | extractEpochInfo | `WBFT: epochInfo is nil` | header |
| `engine/engine.go:1467` | computeShuffledIndex | `input index %d out of bounds: %d` | header |
| `messages/preprepare.go:90`, `:97` | Preprepare.DecodeRLP | `failed to decode preprepare: %w`; `failed to decode SignedRoundChange[%d]: %w` (both replaced by `ErrFailedDecodePreprepare` in `messages.Decode`) | network |
| `testutil.go:64` | SetConfigFromChainConfig (test copy) | `hardfork transition block already exists` | unused in production (the production copy is `eth/ethconfig/config.go:248`) |

### 1.7 Errors from outside `consensus/wbft` that WBFT paths return

| Error | Where it arises | Observable |
|---|---|---|
| `consensus.ErrUnknownAncestor` | parent header missing (`engine/engine.go:264`, `engine/engine.go:476`, `engine/engine.go:495`, `engine/engine.go:629`, `engine/engine.go:1428`, `backend/engine.go:95`, `backend/engine.go:426`, `backend/engine.go:439`) | header |
| `consensus.ErrFutureBlock` | `Header.Time > now + allowed_future_block_time` (`engine/engine.go:202-205`); converted to a wait in `VerifyBlockProposal` (`engine/engine.go:174-175`) | header, network |
| RLP decode errors (`rlp: …`) | replaced by the extra / message errors above (the API paths, `backend/api.go:269`, wrap them with `%w`) | log |
| BLS errors (`public key must be 48 bytes`, `received an infinite public key`, `signature not in group`, …) | `crypto/bls/blst/*.go`; replaced by the seal errors on the engine path; returned unwrapped by `core.verifySeal` for a bad public key (`core/core.go:474-477`) | log |
| secp256k1 errors (`invalid signature length`, `invalid signature recovery id`, `recovery failed`) | `crypto/secp256k1/secp256.go`; replaced by `errInvalidSigner` on the message path (`core/handler.go:279-281`); wrapped by `failed to verify randao reveal signature: %w` on the header path (`engine/engine.go:314`) | log |
| `types.ErrInvalidIstanbulHeaderExtra` (`core/types/istanbul.go:49`) | defined, never returned | unused |

---

## 2. Log catalog (informative)

### 2.1 Scope and method

The table lists every log call in the non-test files of `consensus/wbft/**` whose message is a string literal (or a `fmt.Sprintf` format) beginning with `WBFT: `. It was generated by walking the Go syntax tree of each file, then spot-checked by hand. 184 statements were found. (The v0.1 draft listed 178; it lacked `config.go:277`, `core/commit.go:174`, `core/core.go:280`, `core/core.go:302`, `core/final_committed.go:28` and `engine/engine.go:908`.)

Columns:

- **Location**: path relative to `consensus/wbft/` and line of the call.
- **Level**: the geth log level (`Trace` < `Debug` < `Info` < `Warn` < `Error`).
- **Message**: the literal. `fmt: …` marks a `fmt.Sprintf` format string.
- **Call fields**: the key/value pairs passed in the call, as `key=expression`. `x...` means the pairs are spread from `x`.
- **Inherited context**: keys attached to the logger before the call (`logger.New(...)`, `currentLogger`, `withMsg`). The core's base logger carries `address` (the node address, `core/core.go:62`). `currentLogger(state, msg)` adds `current.round`, `current.sequence` (when a round state exists), `state` (when `state = true`) and `msg.code`, `msg.source`, `msg.round`, `msg.sequence` (when a message is given) (`core/handler.go:319-343`). When a logger variable is assigned on alternative branches, the context of the last assignment in source order is shown. `—` means no inherited keys (package-level `log` or the backend logger).

geth renders a record as `LVL [date|time] message key=value …`; the keys above appear in that order after the inherited context.

Role of this catalog. This catalog is the source of the log phrases that requirements tagged `Observable: log` refer to. The inspector's implementation profile for the reference (the table that turns log records into events, for example `gstable@740526d03`) is generated from this table, never edited by hand, and is regenerated whenever the reference commit changes (`tools/logcat`). A log phrase is therefore defined in one place only, and the profile cannot drift from the specification.

Value rendering. Values that are not strings are rendered as follows in the terminal and logfmt formats: `state` (inherited from `currentLogger`) as `"Accept request"`, `Preprepared`, `Prepared` or `Committed`, quoted when it contains a space (`old.state` and `new.state` are passed as the same strings); a validator (`next.proposer`, `old.proposer`) as its checksummed address; a validator list (`next.valSet`) as `"[addr addr …]"`; big integers and uint64 values as decimal numbers; `F` (L131, a float) as `1.000` in the terminal format and `1` in logfmt. In the json format (`--log.format json`) big integers are JSON strings, uint64 values and floats are JSON numbers, and a validator list is a list of empty objects (`[{},{},…]`), so the addresses of the new validator set are not visible in json logs. A view (L019) is passed as the string `{Round: r, Sequence: s}`, both numbers truncated to uint64. Sources: `core/types.go:44-56` (State.String), `types.go:87-89` (View.String), `validator/default.go:45-47` (validator String), `core/core.go:215`, `:273`, `:302`, `core/handler.go:319-343`.

Notes on individual entries:

- L147 (`engine/engine.go:690`) passes its fields (`"err", "number", it.Number, err`), so the record shows `err=number` followed by a key made from the block number.
- L160 – L167 and L169 – L183 (`messages/roundchange.go`) log at level Error while decoding untrusted input; L168 is a Debug record of a successful decode.

### 2.2 Table

| ID | Location | Level | Message | Call fields | Inherited context |
|---|---|---|---|---|---|
| L001 | `backend/backend.go:155` | Error | WBFT: invalid validator | `address=sb.Address(), validator=validator, payload=hexutil.Encode(payload), code=code` | — |
| L002 | `backend/backend.go:217` | Warn | WBFT: invalid block proposal | `proposal=proposal` | — |
| L003 | `backend/backend.go:231` | Info | WBFT: block proposal committed | `author=sb.Address(), hash=proposal.Hash(), number=proposal.Number().Uint64()` | — |
| L004 | `backend/backend.go:262` | Warn | WBFT: invalid block proposal | `proposal=proposal` | — |
| L005 | `backend/backend.go:268` | Warn | WBFT: bad block proposal | `proposal=proposal` | — |
| L006 | `backend/backend.go:335` | Error | WBFT: last block proposal invalid | `err=err` | — |
| L007 | `backend/backend.go:356` | Info | WBFT: activate WBFT | `—` | — |
| L008 | `backend/backend.go:357` | Trace | WBFT: set ProposerPolicy sorter to ValidatorSortByByteFunc | `—` | — |
| L009 | `backend/backend.go:362` | Error | WBFT: failed to activate WBFT | `err=err` | — |
| L010 | `backend/backend.go:374` | Info | WBFT: deactivate | `—` | — |
| L011 | `backend/backend.go:376` | Error | WBFT: failed to deactivate | `err=err` | — |
| L012 | `backend/engine.go:265` | Info | WBFT: start | `—` | — |
| L013 | `backend/handler.go:107` | Debug | WBFT: received NewBlockMsg | `size=msg.Size, payload.type=reflect.TypeOf(msg.Payload), sender=addr` | — |
| L014 | `backend/handler.go:120` | Warn | WBFT: unable to decode the NewBlockMsg | `error=err` | — |
| L015 | `backend/handler.go:125` | Debug | WBFT: block already proposed | `hash=newRequestedBlock.Hash(), sender=addr` | — |
| L016 | `config.go:275` | Trace | WBFT: initial epoch info | `validators=epochInfo.Validators` | — |
| L017 | `config.go:277` | Trace | fmt: WBFT:   - candidates[%d] | `addr=candi.Addr, diligence=candi.Diligence` | — |
| L018 | `core/backlog.go:82` | Trace | WBFT: future message too far ahead in sequence, dropped | `msg_seq=view.Sequence.String(), curr_seq=curr.Sequence.String(), diff=seqDiff.String()` | address |
| L019 | `core/backlog.go:92` | Trace | WBFT: future sequence message too far ahead in round, dropped | `msg_view=view.String(), curr_view=curr.String(), threshold=roundThreshold` | address |
| L020 | `core/backlog.go:103` | Trace | WBFT: future message too far ahead in round, dropped | `msg_round=view.Round.String(), curr_round=curr.Round.String(), diff=roundDiff.String()` | address |
| L021 | `core/backlog.go:212` | Warn | WBFT: backlog from self | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L022 | `core/backlog.go:216` | Trace | WBFT: new backlog message | `backlogs_size=len(c.backlogs)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L023 | `core/backlog.go:233` | Trace | WBFT: duplicate backlog message, dropping | `src=src` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L024 | `core/backlog.go:238` | Warn | WBFT: backlog is full, dropping message | `src=src, size=backlog.Size()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L025 | `core/backlog.go:268` | Trace | WBFT: process backlog | `—` | address, from, state |
| L026 | `core/backlog.go:290` | Trace | WBFT: stop processing backlog | `msg=msg` | address, from, state |
| L027 | `core/backlog.go:297` | Trace | WBFT: skip backlog message | `msg=msg, err=err` | address, from, state |
| L028 | `core/backlog.go:301` | Trace | WBFT: post backlog event | `msg=msg` | address, from, state |
| L029 | `core/commit.go:56` | Error | WBFT: failed to encode payload of COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L030 | `core/commit.go:62` | Error | WBFT: failed to sign COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L031 | `core/commit.go:70` | Error | WBFT: failed to encode COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L032 | `core/commit.go:74` | Info | WBFT: broadcast COMMIT message | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L033 | `core/commit.go:75` | Trace | WBFT: COMMIT payload | `payload=hexutil.Encode(payload)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L034 | `core/commit.go:79` | Error | WBFT: failed to broadcast COMMIT message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L035 | `core/commit.go:93` | Debug | WBFT: handle COMMIT message | `commits.count=c.current.WBFTCommits.Size(), quorum=c.valSet.QuorumSize()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L036 | `core/commit.go:97` | Warn | WBFT: invalid COMMIT message digest | `digest=commit.Digest, proposal=c.current.Proposal().Hash().String()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L037 | `core/commit.go:104` | Warn | WBFT: failed to cast proposal from COMMIT message to *types.Block | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L038 | `core/commit.go:110` | Warn | WBFT: failed to verify seal from COMMIT message | `number=block.Header().Number, from=commit.Source()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L039 | `core/commit.go:116` | Error | WBFT: failed to save COMMIT message | `err=err` | address |
| L040 | `core/commit.go:124` | Info | WBFT: received quorum of COMMIT messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, commits.count, quorum |
| L041 | `core/commit.go:127` | Trace | WBFT: accepted new COMMIT messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, commits.count, quorum |
| L042 | `core/commit.go:174` | Error | WBFT: error committing proposal | `err=err` | address, current.round, current.sequence, state |
| L043 | `core/core.go:183` | Debug | WBFT: initialize new round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L044 | `core/core.go:186` | Debug | WBFT: start at the initial round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L045 | `core/core.go:195` | Debug | WBFT: catch up last block proposal | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L046 | `core/core.go:199` | Debug | WBFT: same round, no need to start new round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L047 | `core/core.go:202` | Warn | WBFT: next round is inferior to current round | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L048 | `core/core.go:207` | Warn | WBFT: next sequence is before last block proposal | `—` | address, current.round, current.sequence, target.round, lastProposal.number, lastProposal.hash |
| L049 | `core/core.go:273` | Info | WBFT: start new round | `next.round=newView.Round, next.seq=newView.Sequence, next.proposer=c.valSet.GetProposer(), next.valSet=c.valSet.List(), next.size=c.valSet.Size(), next.IsProposer=c.IsProposer()` | address, old.round, old.sequence, old.state, old.proposer |
| L050 | `core/core.go:280` | Warn | WBFT: Discarding prepared block due to bad proposal | `hash=c.current.preparedBlock.Hash()` | address, current.round, current.sequence |
| L051 | `core/core.go:302` | Debug | WBFT: changed state | `old.state=oldState.String(), new.state=state.String()` | address, current.round, current.sequence |
| L052 | `core/core.go:359` | Warn | WBFT: newRoundChangeTimer skipped: current view not initialized | `—` | address |
| L053 | `core/core.go:385` | Warn | WBFT: Possible request timeout overflow detected, setting timeout value to maxRequestTimeout | `timeout=timeout.Seconds(), max_request_timeout=maxRequestTimeout.Seconds()` | address, current.round, current.sequence |
| L054 | `core/core.go:395` | Warn | WBFT: Timeout overflow detected, setting timeout value to MaxInt64 | `adjusted_timeout=time.Duration(math.MaxInt64).Seconds()` | address, current.round, current.sequence |
| L055 | `core/core.go:404` | Trace | WBFT: start new ROUND-CHANGE timer | `timeout=timeout.Seconds()` | address, current.round, current.sequence |
| L056 | `core/core.go:442` | Trace | WBFT: set ROUND-CHANGE retry timer | `round=round.Uint64(), timeout=timeout.Seconds()` | address |
| L057 | `core/extraseal.go:55` | Error | WBFT: invalid extra PREPARE message digest | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L058 | `core/extraseal.go:61` | Error | WBFT: PREPARE seal verify failed | `round=prepareMsg.CommonPayload.Round, from=prepareMsg.Source(), err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L059 | `core/extraseal.go:71` | Error | WBFT: invalid extra COMMIT message digest | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L060 | `core/extraseal.go:77` | Error | WBFT: COMMIT seal verify failed | `round=commitMsg.CommonPayload.Round, from=commitMsg.Source(), err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L061 | `core/extraseal.go:100` | Trace | WBFT: new extra prepare seal message | `source=prepareMsg.Source(), sequence=prepareMsg.Sequence.Uint64(), round=prepareMsg.Round.Uint64()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L062 | `core/extraseal.go:114` | Trace | WBFT: new extra commit seal message | `source=commitMsg.Source(), sequence=commitMsg.Sequence.Uint64(), round=commitMsg.Round.Uint64()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L063 | `core/extraseal.go:192` | Debug | WBFT: clear extra prepare seal | `source=addr, sequence=msg.Sequence.Uint64(), round=msg.Round.Uint64(), lastNum=lastNum.Uint64()` | — |
| L064 | `core/extraseal.go:200` | Debug | WBFT: clear extra commit seal | `source=addr, sequence=msg.Sequence.Uint64(), round=msg.Round.Uint64(), lastNum=lastNum.Uint64()` | — |
| L065 | `core/final_committed.go:28` | Info | WBFT: handle final committed | `—` | address, current.round, current.sequence, state |
| L066 | `core/handler.go:36` | Info | WBFT: start | `—` | address |
| L067 | `core/handler.go:51` | Info | WBFT: stopping... | `—` | address |
| L068 | `core/handler.go:57` | Info | WBFT: stopped | `—` | address |
| L069 | `core/handler.go:145` | Error | WBFT: can not encode backlog message | `err=err` | address |
| L070 | `core/handler.go:192` | Error | WBFT: invalid message event code | `—` | address, code, data |
| L071 | `core/handler.go:199` | Error | WBFT: invalid message | `err=err` | address, code, data |
| L072 | `core/handler.go:243` | Error | WBFT: invalid messages code | `code=m.Code()` | address |
| L073 | `core/handler.go:256` | Warn | WBFT: TIMER CHANGING ROUND | `pr=c.current.preparedRound` | address, current.round, current.sequence, state |
| L074 | `core/handler.go:258` | Warn | WBFT: TIMER CHANGED ROUND | `pr=c.current.preparedRound` | address, current.round, current.sequence, state |
| L075 | `core/handler.go:275` | Error | WBFT: invalid message payload | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L076 | `core/handler.go:280` | Error | WBFT: invalid message signature | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L077 | `core/justification.go:99` | Trace | WBFT: hasQuorumOfRoundChangeMessagesForNil | `rc=m` | — |
| L078 | `core/justification.go:116` | Trace | WBFT: hasQuorumOfRoundChangeMessagesForPreparedRoundAndBlock | `rc=m` | — |
| L079 | `core/justification.go:162` | Warn | WBFT: duplicate ROUND-CHANGE from same source, skipping | `source=addr` | — |
| L080 | `core/justification.go:180` | Warn | WBFT: duplicate PREPARE from same source, skipping | `source=addr` | — |
| L081 | `core/prepare.go:54` | Error | WBFT: failed to encode payload of PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L082 | `core/prepare.go:59` | Error | WBFT: failed to sign PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L083 | `core/prepare.go:67` | Error | WBFT: failed to encode PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L084 | `core/prepare.go:71` | Info | WBFT: broadcast PREPARE message | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L085 | `core/prepare.go:72` | Trace | WBFT: PREPARE payload | `payload=hexutil.Encode(payload)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L086 | `core/prepare.go:76` | Error | WBFT: failed to broadcast PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L087 | `core/prepare.go:90` | Debug | WBFT: handle PREPARE message | `prepares.count=c.current.WBFTPrepares.Size(), quorum=c.valSet.QuorumSize()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L088 | `core/prepare.go:94` | Warn | WBFT: invalid PREPARE message digest | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L089 | `core/prepare.go:101` | Warn | WBFT: failed to cast proposal from PREPARE message to *types.Block | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L090 | `core/prepare.go:107` | Warn | WBFT: failed to verify seal from PREPARE message | `number=block.Header().Number, from=prepare.Source()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L091 | `core/prepare.go:113` | Error | WBFT: failed to save PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L092 | `core/prepare.go:122` | Info | WBFT: received quorum of PREPARE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, prepares.count, quorum |
| L093 | `core/prepare.go:135` | Trace | WBFT: PREPARE message matches proposal | `proposal=c.current.Proposal().Hash(), prepare=prepare.Digest` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, prepares.count, quorum |
| L094 | `core/prepare.go:142` | Trace | WBFT: accepted PREPARE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, prepares.count, quorum |
| L095 | `core/preprepare.go:60` | Error | WBFT: failed to encode payload of PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L096 | `core/preprepare.go:65` | Error | WBFT: failed to sign PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L097 | `core/preprepare.go:75` | Trace | WBFT: add ROUND-CHANGE justification | `rc=m.(*wbfmessage.RoundChange).SignedRoundChangePayload` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L098 | `core/preprepare.go:77` | Trace | WBFT: extended PRE-PREPARE message with ROUND-CHANGE justifications | `justifications=preprepare.JustificationRoundChanges` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L099 | `core/preprepare.go:83` | Trace | WBFT: extended PRE-PREPARE message with PREPARE justification | `justification=preprepare.JustificationPrepares` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L100 | `core/preprepare.go:89` | Error | WBFT: failed to encode PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L101 | `core/preprepare.go:95` | Info | WBFT: broadcast PRE-PREPARE message | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, block.number, block.hash |
| L102 | `core/preprepare.go:96` | Trace | WBFT: PRE-PREPARE payload | `payload=hexutil.Encode(payload)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, block.number, block.hash |
| L103 | `core/preprepare.go:100` | Error | WBFT: failed to broadcast PRE-PREPARE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, block.number, block.hash |
| L104 | `core/preprepare.go:120` | Debug | WBFT: handle PRE-PREPARE message | `—` | address |
| L105 | `core/preprepare.go:124` | Warn | WBFT: ignore PRE-PREPARE message from non proposer | `proposer=c.valSet.GetProposer().Address()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L106 | `core/preprepare.go:130` | Warn | WBFT: ignore PRE-PREPARE with mismatched sequence and proposal number | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L107 | `core/preprepare.go:139` | Warn | WBFT: invalid PRE-PREPARE message justification | `je.CtxWithErr()...` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L108 | `core/preprepare.go:141` | Warn | WBFT: invalid PRE-PREPARE message justification | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L109 | `core/preprepare.go:151` | Info | WBFT: PRE-PREPARE block proposal is in the future (will be treated again later) | `duration=duration` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L110 | `core/preprepare.go:165` | Warn | WBFT: invalid PRE-PREPARE block proposal | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, proposal.number, proposal.hash |
| L111 | `core/preprepare.go:173` | Debug | WBFT: accepted PRE-PREPARE message | `—` | address |
| L112 | `core/request.go:36` | Debug | WBFT: handle block proposal request | `—` | address, current.round, current.sequence, state |
| L113 | `core/request.go:40` | Error | WBFT: invalid request | `—` | address, current.round, current.sequence, state |
| L114 | `core/request.go:43` | Error | WBFT: unexpected request | `err=err, number=request.Proposal.Number(), hash=request.Proposal.Hash()` | address, current.round, current.sequence, state |
| L115 | `core/request.go:84` | Trace | WBFT: store block proposal request for future treatment | `—` | address, current.round, current.sequence, state, proposal.number, proposal.hash |
| L116 | `core/request.go:99` | Trace | WBFT: lookup for pending block proposal requests | `—` | address, current.round, current.sequence, state |
| L117 | `core/request.go:107` | Trace | WBFT: stop looking up for pending block proposal request | `—` | address, current.round, current.sequence, state |
| L118 | `core/request.go:111` | Trace | WBFT: skip pending invalid block proposal request | `number=r.Proposal.Number(), hash=r.Proposal.Hash(), err=err` | address, current.round, current.sequence, state |
| L119 | `core/request.go:114` | Debug | WBFT: found pending block proposal request | `proposal.number=r.Proposal.Number(), proposal.hash=r.Proposal.Hash()` | address, current.round, current.sequence, state |
| L120 | `core/roundchange.go:59` | Warn | WBFT: invalid past target round | `current=cv.Round, target=round` | address, current.round, current.sequence, state |
| L121 | `core/roundchange.go:68` | Error | WBFT: failed to encode ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L122 | `core/roundchange.go:73` | Error | WBFT: failed to sign ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L123 | `core/roundchange.go:81` | Debug | WBFT: extended ROUND-CHANGE message with PREPARE justification | `justification=roundChange.Justification` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L124 | `core/roundchange.go:87` | Error | WBFT: failed to encode ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L125 | `core/roundchange.go:91` | Info | WBFT: broadcast ROUND-CHANGE message | `payload=hexutil.Encode(data)` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L126 | `core/roundchange.go:95` | Error | WBFT: failed to broadcast ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L127 | `core/roundchange.go:115` | Info | WBFT: handle ROUND-CHANGE message | `higherRoundChanges.count=num, currentRoundChanges.count=currentRoundMessages` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L128 | `core/roundchange.go:130` | Warn | WBFT: round-change validation failed | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L129 | `core/roundchange.go:139` | Warn | WBFT: round-change validation failed | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L130 | `core/roundchange.go:148` | Warn | WBFT: failed to add ROUND-CHANGE message | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence |
| L131 | `core/roundchange.go:166` | Info | WBFT: received F+1 ROUND-CHANGE messages | `F=c.valSet.F()` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L132 | `core/roundchange.go:171` | Info | WBFT: received quorum of ROUND-CHANGE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L133 | `core/roundchange.go:183` | Warn | WBFT: round change returns an error: no proposal as pending request is nil | `—` | — |
| L134 | `core/roundchange.go:200` | Error | WBFT: invalid ROUND-CHANGE message justification | `je.CtxWithErr()...` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L135 | `core/roundchange.go:202` | Error | WBFT: invalid ROUND-CHANGE message justification | `err=err` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L136 | `core/roundchange.go:214` | Debug | WBFT: accepted ROUND-CHANGE messages | `—` | address, current.round, current.sequence, state, msg.code, msg.source, msg.round, msg.sequence, higherRoundChanges.count, currentRoundChanges.count |
| L137 | `core/roundchange.go:278` | Warn | WBFT: ROUND-CHANGE prepared justification mismatch, ignoring prepared state | `source=roundChange.Source(), round=round, err=err` | — |
| L138 | `engine/engine.go:297` | Trace | WBFT: Skipping blacklisted signer verification due to unavailable state | `number=header.Number, err=err` | — |
| L139 | `engine/engine.go:347` | Trace | WBFT: Skipping gas tip verification due to unavailable state | `number=header.Number, err=err` | — |
| L140 | `engine/engine.go:396` | Error | WBFT: failed to verify previous prepared seal | `number=parent.Number, round=extra.PrevRound, err=err` | — |
| L141 | `engine/engine.go:406` | Error | WBFT: failed to verify previous committed seal | `number=parent.Number, round=extra.PrevRound, err=err` | — |
| L142 | `engine/engine.go:431` | Error | WBFT: failed to verify prepared seal | `err=err` | — |
| L143 | `engine/engine.go:444` | Error | WBFT: failed to verify committed seal | `err=err` | — |
| L144 | `engine/engine.go:653` | Error | WBFT: failed to determine epoch block | `number=header.Number, err=err` | — |
| L145 | `engine/engine.go:673` | Error | WBFT: failed to get latest epoch info | `number=header.Number, err=err` | — |
| L146 | `engine/engine.go:684` | Error | WBFT: failed to extract wbft extra data | `number=it.Number, err=err` | — |
| L147 | `engine/engine.go:690` | Error | WBFT: failed to get proposer | `err="number", it.Number=err` | — |
| L148 | `engine/engine.go:700` | Error | WBFT: failed to get previous epoch info | `number=parent.Number, err=err` | — |
| L149 | `engine/engine.go:719` | Error | WBFT: failed to get previous prepare signers | `number=it.Number, err=err` | — |
| L150 | `engine/engine.go:731` | Error | WBFT: failed to get previous commit signers | `number=it.Number, err=err` | — |
| L151 | `engine/engine.go:739` | Trace | WBFT: Seals count | `current block number=it.Number, prepareSigners=prepareSigners, commitSigners=commitSigners` | — |
| L152 | `engine/engine.go:763` | Error | WBFT: failed to get prior epoch info | `number=parent.Number, err=err2` | — |
| L153 | `engine/engine.go:784` | Error | WBFT: Invalid round | `num=header.Number.Uint64(), err=err` | — |
| L154 | `engine/engine.go:799` | Trace | WBFT: Seals counts in epoch | `header.number=header.Number, current block number=latestEpoch, proposedSealsInEpoch=proposedSealsInEpoch, submittedSealsInEpoch=submittedSealsInEpoch` | — |
| L155 | `engine/engine.go:887` | Error | WBFT: Failed to decide validators | `err=err` | — |
| L156 | `engine/engine.go:896` | Warn | WBFT: no BLS public key for the validator | `validator=addr` | — |
| L157 | `engine/engine.go:906` | Trace | WBFT: update epoch info | `header.Number=header.Number, validators=newEpoch.Validators` | — |
| L158 | `engine/engine.go:908` | Trace | fmt: WBFT:   - candidates[%d] | `addr=candidate.Addr, diligence=candidate.Diligence` | — |
| L159 | `engine/engine.go:1272` | Error | WBFT: Failed to compute shuffled index | `index=i, err=err` | — |
| L160 | `messages/roundchange.go:90` | Error | WBFT: Error List() Signed Payload | `struct="SignedRoundChangePayload", err=err` | — |
| L161 | `messages/roundchange.go:97` | Error | WBFT: Error Raw() | `err=err` | — |
| L162 | `messages/roundchange.go:104` | Error | WBFT: Error List() Payload | `err=err` | — |
| L163 | `messages/roundchange.go:109` | Error | WBFT: Error Decode(&m.Sequence) | `err=err` | — |
| L164 | `messages/roundchange.go:113` | Error | WBFT: Error Decode(&m.Round) | `err=err` | — |
| L165 | `messages/roundchange.go:120` | Error | WBFT: Error List() Prepared | `err=err` | — |
| L166 | `messages/roundchange.go:125` | Error | WBFT: Error Decode(&m.PreparedRound) | `err=err` | — |
| L167 | `messages/roundchange.go:129` | Error | WBFT: Error Decode(&p.PreparedDigest) | `err=err` | — |
| L168 | `messages/roundchange.go:153` | Debug | WBFT: Correctly decoded SignedRoundChangePayload | `p=p` | — |
| L169 | `messages/roundchange.go:212` | Error | WBFT: Error List() Signed Payload | `err=err` | — |
| L170 | `messages/roundchange.go:219` | Error | WBFT: Error Raw() | `err=err` | — |
| L171 | `messages/roundchange.go:226` | Error | WBFT: Error List() Payload | `err=err` | — |
| L172 | `messages/roundchange.go:231` | Error | WBFT: Error Decode(&m.Sequence) | `err=err` | — |
| L173 | `messages/roundchange.go:235` | Error | WBFT: Error Decode(&m.Round) | `err=err` | — |
| L174 | `messages/roundchange.go:242` | Error | WBFT: Error List() Prepared | `err=err` | — |
| L175 | `messages/roundchange.go:247` | Error | WBFT: Error Decode(&m.PreparedRound) | `err=err` | — |
| L176 | `messages/roundchange.go:251` | Error | WBFT: Error Decode(&m.PreparedDigest) | `err=err` | — |
| L177 | `messages/roundchange.go:274` | Error | WBFT: Error Kind() | `err=err` | — |
| L178 | `messages/roundchange.go:279` | Error | WBFT: Error Raw() | `err=err` | — |
| L179 | `messages/roundchange.go:284` | Error | WBFT: Error Decode(&m.PreparedDigest) | `err=err` | — |
| L180 | `messages/roundchange.go:288` | Error | WBFT: Error m.PreparedDigest.Hash() != digest | `m.hash=m.PreparedBlock.Hash(), m.digest=m.PreparedDigest` | — |
| L181 | `messages/roundchange.go:294` | Error | WBFT: Error Kind() | `err=err` | — |
| L182 | `messages/roundchange.go:299` | Error | WBFT: Error Raw() | `err=err` | — |
| L183 | `messages/roundchange.go:304` | Error | WBFT: Error Decode(&m.Justification) | `err=err` | — |
| L184 | `utils.go:63` | Error | WBFT: Failed to get signer address | `err=err` | — |

---

## 3. Lineage from QBFT (informative)

WBFT descends from the QBFT implementation of ConsenSys Quorum ("istanbul/qbft"); most files carry the note "derived from quorum/… (2024.07.25). Modified and improved for the wemix development" (for example `consensus/wbft/config.go:18-19`, `core/types/istanbul.go:18-19`, `consensus/wbft/engine/apply_extra.go:17-18`). QBFT in turn implements the algorithm of H. Moniz, "The Istanbul BFT Consensus Algorithm", arXiv:2002.03613 v2 (2020). WBFT is a protocol of its own: where this specification and QBFT or the paper differ, this specification is correct. The differences below are design facts, not defects.

The QBFT column and every Quorum path in this section refer to ConsenSys Quorum at commit `5ffacc48` (GoQuorum 24.4.1, `consensus/istanbul/**`).

The formal-specification column of §3.2 and the whole of §3.4 refer to the ConsenSys QBFT formal specification (`github.com/Consensys/qbft-formal-spec-and-verification`, commit `1630128e7`, Dafny, `dafny/spec/L1/**`), compared with Quorum and with go-stablenet `740526d03`. Where the formal specification and Quorum disagree, the rows say which one WBFT follows. The safety properties proved about the formal specification, and the assumptions of that proof that WBFT does not meet, are in `A-05` §18.5.

Rows marked (A-05) restate the v0.1 draft's comparison of the state machine; they were not re-verified while writing this appendix, and `A-05` is normative for them.

### 3.1 What WBFT keeps from QBFT

| Item | WBFT | Where specified |
|---|---|---|
| message set | PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE with codes `0x12 … 0x15`; legacy `0x11`; devp2p capability `istanbul/100` | `A-01 §4.1`, `A-03 §8`, `A-07` |
| signed-payload form | `rlp([code, fields])`, ECDSA over its Keccak hash; justification items carry their own signatures | `A-03 §8.1` |
| ROUND-CHANGE payload | `[sequence, round, prepared]` with `prepared = [] | [prepared_round, prepared_digest]`, prepared block and PREPARE justification outside the signed part | `A-03 §8.4` |
| hash rule | the block hash excludes the current-block seals and uses round 0 in the extra data (Quorum `QBFTFilteredHeaderWithRound(h, 0)`, `core/types/istanbul.go:204-228`) | `A-03 §6` |

### 3.2 What WBFT changes or adds

| Area | QBFT (Quorum `5ffacc48`) | Formal specification (`1630128e7`) | WBFT | Where specified |
|---|---|---|---|---|
| hash-rule marker | `MixDigest == IstanbulDigest` selects the hash rule (`core/types/block.go:100-110`); `Difficulty = 1` is required but does not select it | not modelled: `digest` is abstract, and chains are compared without `commitSeals` and `roundNumber` (`dafny/spec/L1/types.dfy:31-62`) | `Difficulty = 1` selects the hash rule; `MixDigest` carries the randao mix | `A-03 §6`, `A-02 §7` |
| seals required in a header | at least `F + 1` committed seals, each from a distinct validator (`consensus/istanbul/qbft/engine/engine.go:246-287`) | at least `quorum(n)` commit seals, each from a validator (`ValidNewBlock`, `dafny/spec/L1/node_auxiliary_functions.dfy:815-820`); WBFT agrees, Quorum's `F + 1` does not | at least `Q = quorum_size(N)` sealers in each aggregated seal of the block and in the previous-block seals | `A-08 §6.3–6.5` |
| seal cryptography | ECDSA committed seals, one 65-byte signature per validator | abstract signature `signHash` over the block without its commit seals (`dafny/spec/L1/node_auxiliary_functions.dfy:88-93, 289-297`) | BLS12-381 seals; one aggregated 96-byte signature plus a bitmap per seal kind | `A-02 §5`, `A-03 §4.4` |
| seals carried | committed seals only | commit seals only (`dafny/spec/L1/types.dfy:36-42`) | prepared and committed seals of the block, plus the previous block's prepared and committed seals (extended with late "extra seals") | `A-03 §4.1`, `A-05`, `A-08` |
| PREPARE content | view and digest | height, round and digest (`dafny/spec/L1/types.dfy:83-87`) | view, digest and a BLS prepare seal | `A-03 §8.2` |
| header extra | vanity, validator list, vote, round, committed seals | proposer, round number, commit seals, height, timestamp (`dafny/spec/L1/types.dfy:36-42`) | ten fields: vanity, randao reveal, previous round and seals, round, current seals, gas tip, epoch info | `A-03 §4` |
| validator set | voted in the header extra (`Vote` field), taken from a contract, or listed in a transition; sorted by address (`core/types/istanbul.go:127-133`, `consensus/istanbul/qbft/engine/engine.go:338-360`, `consensus/istanbul/validator/default.go:61`) | an abstract function of the chain without seals, constant in the proof (`dafny/spec/L1/node_auxiliary_functions.dfy:212-225`, `dafny/ver/L1/support_lemmas/axioms.dfy:17-18`) | epochs; next set computed from contract candidates, diligence and a keccak-based shuffle, written in the epoch block | `A-04` |
| randomness | none | none | randao reveal (ECDSA) and mix in `MixDigest` | `A-02 §7` |
| fee policy | none (optional fixed block reward, `consensus/istanbul/qbft/engine/engine.go:554`) | none | governance gas tip in every header | `A-03 §4.3`, Part B |
| quorum | `ceil(2N/3)` when `Ceil2Nby3Block` is set and reached and no `2FPlus1Enabled` transition is active, otherwise `2F + 1` with `F = ceil(N/3) − 1` (`consensus/istanbul/qbft/core/core.go:306-313`; details in `A-04` §2.3); the comment at `consensus/wbft/validator/default.go:227` still names `ceil(2N/3)` | `quorum(n) = (2n − 1) div 3 + 1`, equal to `ceil(2n/3)` (`dafny/spec/L1/node_auxiliary_functions.dfy:259-262`) | `ceil(N − (N−1)/3)` computed in floating point (`consensus/wbft/validator/default.go:222-229`); equal to `ceil(2N/3)` except when `N` is a multiple of 3, where WBFT's is one larger | `A-04` |
| future-message filter | unbounded backlog (`consensus/istanbul/qbft/core/backlog.go:105-128`) | none; every received message is kept (`dafny/spec/L1/node.dfy:75-81`) | backlog plus a "too far ahead" filter (`SEQUENCE_THRESHOLD`, `ROUND_THRESHOLD`), a per-source size bound and a per-slot key | `A-05` §13.1 |
| round-change retransmission | — | none | retry timer re-creates ROUND-CHANGE every `request_timeout` (A-05); a byte-identical copy is not re-sent to peers that already have it, so it normally produces no traffic (WBFT-TIMER-024); after a failed `finalize`, the `CATCH_UP` branch or a new height the retry differs from the original and is sent (`A-14` §5.8) | `A-06` |
| PRE-PREPARE justification signatures | ROUND-CHANGE members only (`consensus/istanbul/qbft/core/handler.go:266-282`) | every ROUND-CHANGE and every PREPARE of the justification must be signed by a validator (`dafny/spec/L1/node_auxiliary_functions.dfy:486, 554-561, 726`); WBFT agrees, Quorum does not check the PREPAREs | ROUND-CHANGE members, then PREPARE members | `A-05` WBFT-SM-017 |
| ROUND-CHANGE prepared block | not checked against the sequence or the digest (`consensus/istanbul/qbft/core/roundchange.go:112-126`) | a block is used only if its height is the current height and its digest, with the round set back to the prepared round, is the prepared digest (`dafny/spec/L1/node_auxiliary_functions.dfy:542-553`) | number must equal the current sequence and hash must equal the prepared digest | `A-05` §11.2 |
| signature validator set | current set (`consensus/istanbul/qbft/core/core.go:302-304`) | the set of the node's current chain (`dafny/spec/L1/node_auxiliary_functions.dfy:486, 726, 775`) | current set, or the previous sequence's set for messages of the previous view in `AcceptRequest` | `A-05` WBFT-SM-016 |
| round-0 timer | armed by the node's own block request or by PRE-PREPARE acceptance; entering a round `> 0` arms it (`consensus/istanbul/qbft/core/core.go:210-212`) | the round-0 timeout counts from the append of the previous block (`timeLastRoundStart`, `dafny/spec/L1/node.dfy:385, 413, 429`); WBFT agrees, Quorum does not | armed on entering every view | `A-05` WBFT-SM-075, `A-06` §5.1 |
| PRE-PREPARE from a block request | in any round (`consensus/istanbul/qbft/core/request.go:48-55`) | round 0 only (`UponBlockTimeout`, `dafny/spec/L1/node.dfy:189-193`); WBFT agrees, Quorum does not | round 0 only; later rounds propose after a ROUND-CHANGE quorum | `A-05` WBFT-SM-032 |
| validator order | sorted by address | an abstract sequence (`dafny/spec/L1/types.dfy:162`) | order of `EpochInfo.validators`, unsorted | `A-04` WBFT-VAL-006 |
| block-period wait | in `Seal`, after the block is built (`consensus/istanbul/backend/engine.go:205-211`) | the round-0 proposer proposes when its local time reaches the parent timestamp plus `blockTime` (`dafny/spec/L1/node.dfy:189-193`) | before the block is built | `A-06` §8 |
| empty blocks | optional `emptyBlockPeriod` delay and check (`consensus/istanbul/qbft/core/request.go:56-97`) | not modelled | none; a block is proposed every block period with or without transactions | `A-06` §8 |

An implementation ported from Quorum has to replace Quorum's `F + 1` committed-seal check and its `MixDigest` marker: both change which headers are valid (`A-08` §6.3, `A-03` §6).

### 3.3 Comparison with the paper (A-05)

Quorum shares these differences from the paper: rounds start at 0, the value comes from the block builder, a future-dated PRE-PREPARE is deferred, the round timer is not stopped at the COMMIT quorum, there is no commit-certificate answer, `pr < r` is not checked, and the F+1 skip fires only at exactly `F + 1` (`consensus/istanbul/qbft/core/roundchange.go:136`). The formal specification, where it models the same rule, sides with the paper in requiring `pr < r` and in firing the F+1 rule at any count of at least `f + 1`, and replaces the commit-certificate answer by a `NewBlock` message that a deciding node sends to every node (§3.4). The `sequence = proposal.number` check, the prepare seals and their verification, and the aggregation into the header are WBFT's alone.

| Paper rule | WBFT | Kind |
|---|---|---|
| rounds start at 1 | rounds start at 0 | notation |
| `Start(λ, value)` with the input value | the value arrives later from the block builder (request event) | change |
| upon PRE-PREPARE: justify, then PREPARE | also checks `sequence = proposal.number`, runs the block validity check, waits for future-dated proposals; PREPARE carries a seal | change |
| upon quorum of PREPARE: COMMIT | verifies each prepare seal; keeps the quorum PREPAREs for later justification | change |
| upon quorum of COMMIT: stop timer, decide | aggregates seals into the header and hands the block to the chain; the round timer is not stopped at decision | change |
| upon `f+1` higher ROUND-CHANGE: skip to the smallest such round | the skip fires only when the number of distinct senders of higher-round ROUND-CHANGEs lies in `(F, F+1]` (`F` real-valued, `A-04`), that is exactly at `floor(F)+1`; it does not fire when that count is already larger (`core/roundchange.go:161`) | change |
| after deciding, answer ROUND-CHANGE with the commit certificate | not implemented; decided blocks (with seals) propagate through block sync | not adopted |
| ROUND-CHANGE validity requires `pr < r` | not checked; justification checks are relied on | not adopted |

### 3.4 Comparison with the QBFT formal specification

The table lists the elements of the formal specification's state machine that §3.2 does not already cover and in which WBFT, Quorum or both differ from the formal specification. In the formal column a bare `:line` refers to `dafny/spec/L1/node_auxiliary_functions.dfy`; the Quorum column refers to Quorum `5ffacc48` as in §3.2.

| Element | Formal specification (`1630128e7`) | Quorum `5ffacc48` | WBFT | Where specified |
|---|---|---|---|---|
| senders counted in a justification | distinct senders of the ROUND-CHANGEs (`getSetOfRoundChangeSenders`, `:424-427, 532`) | messages, without deduplication (`consensus/istanbul/qbft/core/justification.go:26`) | distinct sources | `A-05` WBFT-SM-061 step 1 |
| view of the justification ROUND-CHANGEs | each for the height and round of the proposal (`validRoundChange`, `:466-475, 533`) | not checked (`consensus/istanbul/qbft/core/justification.go:20-51`) | checked | `A-05` WBFT-SM-061 step 3 |
| `prepared_round < round` in a justification ROUND-CHANGE | required (`:480-483`) | not required | not required | `A-05` WBFT-SM-061 notes, §3.3 |
| unprepared justification | every ROUND-CHANGE of the set unprepared, and the block valid and built by the round leader (`:534-540`; `validateNonPreparedBlock`, `:449-459`) | `Q` unprepared ROUND-CHANGEs (`consensus/istanbul/qbft/core/justification.go:55-67`) | same as Quorum | `A-05` WBFT-SM-061 step 6 |
| prepared justification: ROUND-CHANGEs | one ROUND-CHANGE has the highest prepared round of all ROUND-CHANGEs of the set and matches the block (`isHighestPrepared`, `:494-502, 546-553`) | `Q` ROUND-CHANGEs with a prepared round at most the justified one, one of them matching (`consensus/istanbul/qbft/core/justification.go:71-88`) | same as Quorum | `A-05` WBFT-SM-061 step 7 |
| prepared justification: PREPAREs | at least `quorum(n)` PREPAREs, each for the height, the prepared round and the digest, each signed by a validator (`:545, 554-561, 714-727`) | same round and digest; counted without deduplication; signatures not verified (`consensus/istanbul/qbft/core/justification.go:31-44`, `consensus/istanbul/qbft/core/handler.go:275-281`) | same round and digest; deduplicated; signatures verified; the sequence is not checked | `A-05` WBFT-SM-017, WBFT-SM-061 steps 4-5 |
| validity of a re-proposed prepared block | not checked again; the block must be one carried by the ROUND-CHANGEs (`:542`) | proposal verification on every PRE-PREPARE (`consensus/istanbul/qbft/core/preprepare.go:130`) | same as Quorum | `A-05` WBFT-SM-037 check 4 |
| PRE-PREPARE for a higher round | accepted when the node has already accepted a proposal in its current round; the node moves to that round (`isValidProposal`, `:587-596`; `dafny/spec/L1/node.dfy:254-256`) | kept in the backlog until the node reaches that round (`consensus/istanbul/qbft/core/backlog.go:65-67`) | same as Quorum | `A-05` WBFT-SM-019 |
| round-timer restart | on entering a higher round through a proposal or through the node's own justified proposal, and on appending a block; not on a round timeout, not on the F+1 rule, not on accepting a proposal of the current round (`dafny/spec/L1/node.dfy:257-261, 385, 413, 486-490`) | on entering a round `> 0`, on the node's own block request, on every PRE-PREPARE acceptance (`consensus/istanbul/qbft/core/core.go:210-212`, `consensus/istanbul/qbft/core/request.go:52`, `consensus/istanbul/qbft/core/preprepare.go:156`) | on entering every view and on every PRE-PREPARE acceptance | `A-05` WBFT-SM-075, `A-06` WBFT-TIMER-010, WBFT-TIMER-012 |
| round-timeout length | `2^r` time units, counted from the last restart (`roundTimeout`, `:267-270`; `dafny/spec/L1/node.dfy:429`) | `request_timeout · 2^r`, capped, counted from each restart (`consensus/istanbul/qbft/core/core.go:258-300`) | same as Quorum | `A-06` §4 |
| COMMIT before the PREPARE quorum | a COMMIT quorum decides once a proposal is accepted (`UponCommit`, `dafny/spec/L1/node.dfy:341-360`) | a COMMIT is a future message until the node is `Prepared` (`consensus/istanbul/qbft/core/backlog.go:81-89`) | same as Quorum | `A-05` WBFT-SM-019 |
| COMMIT seal on receipt | checked: the seal is the COMMIT sender's signature over the proposal (`validateCommit`, `:767-776`) | not checked by the core (`consensus/istanbul/qbft/core/commit.go:88-116`) | checked (BLS) | `A-05` WBFT-SM-045 |
| decision | append the block, send `NewBlock` to every node, enter round 0 of the next height, drop the lock (`dafny/spec/L1/node.dfy:364-386`) | hand the block to the chain; the new head stops the timer and starts round 0 (`consensus/istanbul/qbft/core/commit.go:122-142`, `consensus/istanbul/qbft/core/final_committed.go:21-29`) | hand the block to the chain; stay `Committed` with the round timer running until the new head | `A-05` WBFT-SM-047, WBFT-SM-050 |
| `NewBlock` message | a consensus message to every node, accepted with a quorum of commit seals (`dafny/spec/L1/types.dfy:141-143`; `ValidNewBlock`, `:815-820`) | none; blocks travel on the `eth` protocol | same as Quorum | `A-07` §5.6, `A-09` §5 |
| F+1 rule | at least `f + 1` senders of higher-round ROUND-CHANGEs; the smallest round among `f + 1` of them; evaluated only when no proposal can be justified (`dafny/spec/L1/node.dfy:494-524`) | exactly `F + 1` senders; the smallest higher round held; evaluated before the proposal rule (`consensus/istanbul/qbft/core/roundchange.go:136-144`) | same as Quorum, with the window `(F, F + 1]`; rounds that hold no message count as held | `A-05` WBFT-SM-056, WBFT-SM-057 |
| proposer rotation | the index of the previous block's proposer in the previous block's validator set, plus 1, plus the round (`proposer`, `:304-323`) | the index in the current set (0 when absent), plus 1, plus the round (`consensus/istanbul/validator/default.go:126-150`) | same as Quorum; the Sticky policy omits the 1 | `A-04` WBFT-PROP-003, WBFT-PROP-004, WBFT-PROP-006 |
| node that is not a validator | takes only `UponNewBlock` steps (`dafny/spec/L1/node.dfy:136-147`) | `Broadcast` does not check membership (`consensus/istanbul/backend/backend.go:154-165`) | runs the consensus core but sends nothing | `A-05` WBFT-SM-003 |
| lock release inside a height | never (`dafny/spec/L1/node.dfy:379-386, 407-413`) | never (`consensus/istanbul/qbft/core/core.go:218-224`) | the bad-block rule releases it | `A-05` WBFT-SM-024 |
| PREPAREs the proposer uses as justification | PREPAREs it received itself (`isReceivedProposalJustification`, `:614-615`) | the justification carried by the highest prepared ROUND-CHANGE (`consensus/istanbul/qbft/core/roundchange.go:241-247`) | same as Quorum | `A-05` WBFT-SM-055, WBFT-SM-059 |
| what the proposal signature covers | height, round and digest; the block must match the digest (`:585-586`) | height, round and the whole block | same as Quorum | `A-03` §8 |

In the rows for the senders counted, the view of the justification ROUND-CHANGEs and the COMMIT seal, and in the §3.2 rows for the seals required in a header, the PRE-PREPARE justification signatures, the round-0 timer and the PRE-PREPARE from a block request, Quorum departs from the formal specification and WBFT follows the formal specification. In the row for the justification PREPAREs WBFT follows the formal specification except that it does not check their sequence. In the rows for the decision, the node that is not a validator and the lock release WBFT differs from both. The other rows follow Quorum. How these rows relate to the proved safety properties is stated in `A-05` §18.5.
