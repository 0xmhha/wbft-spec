# Specification tools

Programs used to compute the worked examples and test vectors in this specification, and to extract the catalogs in `A-13`. They run against the reference implementation (go-stablenet `740526d03`) and are the seed of the vector generator described in `A-11`.

All Go modules here point to the reference repository with a `replace` directive. The directive holds the path of a local go-stablenet checkout at `740526d03`; in the published copy of these tools it is the placeholder `/path/to/go-stablenet`. Set it to your checkout first, in every module you run:

```
go mod edit -replace github.com/ethereum/go-ethereum=<path to go-stablenet at 740526d03>
```

Use the toolchain the reference repository builds with (`toolchain go1.23.12` in its `go.mod`).

| Directory | Used by | What it does | Run |
|---|---|---|---|
| `vectors-a01-a03/` | A-01, A-02, A-03 | Computes hashes, ECDSA/BLS key derivation and signatures, seal data, randao, `WBFTExtra` and message encodings with the reference code. `t2/`, `t3/` are follow-up probes (encoding edge cases) | `go run .` |
| `vectorgen/` | A-11 | The conformance test-vector generator. Stage 1 writes the `crypto` and `encoding` vectors of `A-11` §3.3 into `../vectors/` from the reference code; stage 2 (`stage2/`) writes the `validators`, `timers`, `chain/config_at` and `header` vectors, running unexported functions through generator tests injected with `go test -overlay`; stage 3 (`stage3/`) writes the `state_machine`, `network/receive_outcome`, `chain/fork_schedule` and `chain/genesis` vectors, driving the reference consensus core step by step in injected generator tests; `xcheck/xcheck.py`, `xcheck/xcheck_stage2.py` and `xcheck/xcheck_stage3.py` recompute a subset of them without the reference; `check_yaml_subset.py` rejects vector files outside the YAML subset of `A-11` §3.1. See `vectorgen/README.md` | `GOTOOLCHAIN=go1.23.12 go run . -out ../../vectors`; `GOTOOLCHAIN=go1.23.12 go run ./stage2 -out ../../vectors`; `GOTOOLCHAIN=go1.23.12 go run ./stage3 -out ../../vectors`; `python3 check_yaml_subset.py ../../vectors` |
| `logcat/` | A-13 | Extracts every `"WBFT: "` log call with level, message and key/value fields from the reference source (stdlib only, no replace) | `GO_STABLENET=<path to go-stablenet> go run . [logs|errors]` |
| `epoch-a04/` | A-04 | `zz_spec_a04_test.go` is injected into `consensus/wbft/engine` with `go test -overlay overlay.json` (the repository is not modified) to run `buildEpochInfo`, `computeShuffledIndex`, `sortCandidates`, `IsEpochBlockNumber`, `CalcProposer`. `overlay.json` contains absolute paths (placeholders `/path/to/go-stablenet` and `/path/to/wbft-spec` in the published copy): edit both paths before use. | `GOTOOLCHAIN=go1.23.12 go test -overlay overlay.json -run SpecA04 ./consensus/wbft/engine` from the go-stablenet root |
| `genesis-b02/` | B-02, B-06 | Builds the 8282/8283 preset genesis blocks and checks hashes, state roots, extra bytes, base fee examples | `go run .` |
| `slots-b04/` | B-04, B-08 | Reads GovValidator storage with the Go slot readers and cross-checks against the embedded contract bytecode executed in the reference EVM (`xcheck/`) | `go run .`, `go run ./xcheck` |

Except for `vectorgen/`, these are drafting tools, not the vector generator. `A-11` defines the generator's output format; the drafting programs were the starting point for `vectorgen/`.
