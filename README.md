# WBFT Specification

WBFT is the Byzantine fault tolerant consensus protocol of StableNet. It decides one block per height among a set of validators, gives immediate finality, and records the evidence of each decision (aggregated BLS seals) in the block header. It descends from Istanbul BFT / QBFT but has its own message rules, seal format, epochs, validator selection and randomness.

This repository holds the specification of WBFT and of the StableNet block-validity rules a WBFT node needs in order to interoperate with go-stablenet, together with conformance test vectors and the programs that generate them.

- Status: draft, first public edition.
- Reference implementation: [go-stablenet](https://github.com/stable-net/go-stablenet) at commit `740526d03` (branch `dev`). Every requirement cites the code it was derived from in `Source:` lines. Differences from the network release `v1.1.0` are recorded in `spec/A-12-version-notes.md`.
- Some requirements are not yet included in this edition; they will be added in a later edition, so requirement numbers are not contiguous.

## Layout

| Path | Content |
|---|---|
| `spec/` | The normative specification (English). Start with `spec/README.md` (document map and conventions) and `spec/A-00-overview.md` |
| `spec-ko/` | Korean edition (informative). It mirrors `spec/` file by file and requirement by requirement and adds explanatory paragraphs; if the two disagree, `spec/` is correct |
| `spec/vectors/` | Conformance test vectors |
| `spec/tools/` | The vector generator (`vectorgen/`) and the drafting tools used to compute the worked examples and catalogs; see `spec/tools/README.md` |

## Part A and Part B

**Part A — WBFT consensus** (`spec/A-*.md`) defines what a node must do to take part in, or verify, WBFT consensus: types and parameters, cryptography (ECDSA, BLS12-381, randao), byte-level encodings, validator sets, epochs and proposer selection, the consensus state machine, timers, the `istanbul/100` network sub-protocol, header rules, the interface to the execution layer, security considerations, conformance and vectors, version notes and catalogs. `A-14` walks through the protocol as a message exchange and is the recommended second chapter after `A-00`.

**Part B — StableNet block validity** (`spec/B-*.md`) defines the rules outside consensus that a node must apply to produce and accept exactly the blocks go-stablenet produces and accepts: chain configuration and forks, genesis, execution-side header and body rules, system contracts and their storage, governance semantics, block finalization, Anzeon transaction rules, the binding of Part A's application interface to contract state, and synchronisation and RPC.

The boundary between the parts is the application interface of `A-09`.

## Conformance classes

`spec/A-11-conformance-vectors.md` defines four classes. An implementation may claim more than one.

| Class | Code | Typical implementation | Requirements |
|---|---|---|---|
| Consensus participant | `CP` | a validator node | all of Part A and Part B |
| Verifying node | `VN` | a full node that does not seal | Part A except the send conditions of `A-05`, `A-06`, `A-07`; all of Part B |
| Light verifier | `LV` | a bridge or light client that checks finality from headers | `A-01`, `A-02`, `A-03`, the validator-set and epoch sections of `A-04`, light verification of `A-08` |
| Observer | `OB` | an inspector | decoding (`A-02`, `A-03`) and the checks of every requirement tagged `Observable:` |

## Test vectors

Vectors follow the layout of the Ethereum consensus-spec tests (`A-11` §3.1):

```
spec/vectors/
  <runner>/
    <handler>/
      <case>/
        meta.yaml        # requirement IDs covered, reference commit, generator, description
        input.yaml       # inputs
        expected.yaml    # expected output; absent => the operation must fail
```

All files use a strict YAML subset that converts one to one to JSON (`A-11` WBFT-VEC-013): integers are decimal strings, byte strings are lowercase `0x` hex, headers and blocks are given as RLP. The catalog of runners and handlers, and the input and output fields of each handler, are in `A-11` §3.3 and `spec/tools/vectorgen/README.md`.

Check that a vector tree stays inside the subset (Python 3, standard library only):

```
python3 spec/tools/vectorgen/check_yaml_subset.py spec/vectors
```

### Regenerating the vectors

Every expected value is computed by calling the reference implementation through a `replace` directive in `go.mod`. You need a clean checkout of go-stablenet at `740526d03` and the toolchain `go1.23.12`.

```
cd spec/tools/vectorgen
go mod edit -replace github.com/ethereum/go-ethereum=<path to go-stablenet at 740526d03>
GOTOOLCHAIN=go1.23.12 go run .        -out <empty dir>   # stage 1: crypto, encoding
GOTOOLCHAIN=go1.23.12 go run ./stage2 -out <empty dir>   # stage 2: validators, timers, chain/config_at, header, execution, source, governance
GOTOOLCHAIN=go1.23.12 go run ./stage3 -out <empty dir>   # stage 3: state_machine, network, timers/build_wait, chain/fork_schedule, chain/genesis
diff -r <empty dir> ../../vectors
```

The generator refuses to run with another toolchain or with a reference checkout that is not at `740526d03` or has local changes; stages 2 and 3 inject generator tests into the reference with `go test -overlay` and do not modify the checkout. The three stages reproduce `spec/vectors/` byte for byte. `xcheck/` recomputes a subset of the values without the reference (see `spec/tools/vectorgen/README.md`).

### Running an implementation against the vectors

A runner and the implementation under test are separate processes that speak the adapter protocol `wbft-vector/1` (`A-11` §3.4, WBFT-VEC-032 to WBFT-VEC-062): one JSON object per line over the adapter's standard input and output. The runner sends `hello`, then one `case` per vector with `input.yaml` converted to JSON; the adapter answers each with a `result` (`ok` with an `output`, `error`, or `unsupported`); the runner compares `output` with `expected.yaml` and ends with `bye`. A case without `expected.yaml` passes only if the adapter reports `error`.

`spec/tools/vectorgen/check_adapter.py` is a minimal runner, and `spec/tools/vectorgen/adapter/` is an adapter that answers from the reference implementation. To try the protocol end to end:

```
cd spec/tools/vectorgen
GOTOOLCHAIN=go1.23.12 go build -o /tmp/vg-adapter ./adapter
GOTOOLCHAIN=go1.23.12 python3 check_adapter.py ../../vectors -- /tmp/vg-adapter
```

To test your own implementation, write an adapter for it that follows `A-11` §3.4 and pass its command after `--`.

## Korean edition

`spec-ko/` is the Korean edition. File names, section numbers, requirement IDs, tables and pseudocode match the English edition; the Korean edition adds explanatory "해설" paragraphs. Writing rules are in `spec-ko/STYLE.md`.

## License

The repository is licensed by path (`LICENSE`, machine-readable in `REUSE.toml`):

| Path | License |
|---|---|
| `README.md`, `spec/` (except `spec/tools/`), `spec-ko/`, `spec/vectors/` | [CC-BY-4.0](LICENSES/CC-BY-4.0.txt) |
| `spec/tools/` | [LGPL-3.0-or-later](LICENSES/LGPL-3.0-or-later.txt); the LGPL builds on the [GPL-3.0](LICENSES/GPL-3.0-or-later.txt), whose text is included |

Go and Python sources under `spec/tools/` carry an `SPDX-License-Identifier` header. The tools build against go-stablenet, which is not part of this repository and has its own license.
