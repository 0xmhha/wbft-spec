# 명세 tools

이 디렉터리의 프로그램은 이 명세의 worked example 과 test vector 를 계산하고, `A-13` 의 catalog 를 추출하는 데 쓴다. 프로그램은 reference 구현(go-stablenet `740526d03`)을 대상으로 실행하며, `A-11` 에서 설명하는 vector generator 의 출발점이다.

> 해설: 프로그램 파일은 영문판 디렉터리 `../../spec/tools/` 에만 있다. 이 한국어판 디렉터리에는 설명 문서만 둔다. 아래 명령은 `../../spec/tools/` 아래에 있는 각 프로그램의 디렉터리에서 실행한다.

여기 있는 모든 Go module 은 `replace` directive 로 reference repository 를 가리킨다. 그 directive 에는 `740526d03` 의 go-stablenet 을 checkout 한 로컬 경로가 들어 있고, 공개판에 실린 도구에서는 이 경로가 placeholder `/path/to/go-stablenet` 이다. 그러므로 실행하는 module 마다 먼저 경로를 자기 checkout 으로 다음과 같이 바꾼다.

```
go mod edit -replace github.com/ethereum/go-ethereum=<path to go-stablenet at 740526d03>
```

toolchain 은 reference repository 가 빌드에 쓰는 것(`go.mod` 의 `toolchain go1.23.12`)을 쓴다.

| 디렉터리 | 쓰는 곳 | 하는 일 | 실행 |
|---|---|---|---|
| `vectors-a01-a03/` | A-01, A-02, A-03 | reference 코드로 hash, ECDSA/BLS key 유도와 signature, seal data, randao, `WBFTExtra`, 메시지 encoding 을 계산한다. `t2/`, `t3/` 는 encoding edge case 를 추가로 확인하는 probe 다 | `go run .` |
| `vectorgen/` | A-11 | conformance test vector generator 다. 1단계는 reference 코드로 `A-11` §3.3 의 `crypto` 와 `encoding` vector 를 `../vectors/` 에 쓴다. 2단계(`stage2/`)는 `validators`, `timers`, `chain/config_at`, `header` vector 를 쓰며, export 되지 않은 함수는 `go test -overlay` 로 주입한 생성 test 안에서 실행한다. 3단계(`stage3/`)는 `state_machine`, `network/receive_outcome`, `chain/fork_schedule`, `chain/genesis` vector 를 쓰며, 주입한 생성 test 안에서 reference 합의 코어를 단계별로 돌린다. `xcheck/xcheck.py`, `xcheck/xcheck_stage2.py`, `xcheck/xcheck_stage3.py` 는 그 일부를 reference 없이 다시 계산한다. `check_yaml_subset.py` 는 `A-11` §3.1 의 YAML 부분집합을 벗어난 vector 파일을 거부한다. `vectorgen/README.md` 를 본다 | `GOTOOLCHAIN=go1.23.12 go run . -out ../../vectors`; `GOTOOLCHAIN=go1.23.12 go run ./stage2 -out ../../vectors`; `GOTOOLCHAIN=go1.23.12 go run ./stage3 -out ../../vectors`; `python3 check_yaml_subset.py ../../vectors` |
| `logcat/` | A-13 | reference 소스에서 모든 `"WBFT: "` log 호출을 level, message, key/value field 와 함께 추출한다. 표준 라이브러리만 쓰므로 replace 가 필요 없다 | `GO_STABLENET=<path to go-stablenet> go run . [logs|errors]` |
| `epoch-a04/` | A-04 | `go test -overlay overlay.json` 으로 `zz_spec_a04_test.go` 를 `consensus/wbft/engine` 에 주입해서 `buildEpochInfo`, `computeShuffledIndex`, `sortCandidates`, `IsEpochBlockNumber`, `CalcProposer` 를 실행한다. 이때 repository 는 수정하지 않는다. `overlay.json` 에는 절대 경로가 들어 있으므로(공개판에서는 placeholder `/path/to/go-stablenet` 과 `/path/to/wbft-spec`) 쓰기 전에 두 경로를 모두 고친다. | go-stablenet root 에서 `GOTOOLCHAIN=go1.23.12 go test -overlay overlay.json -run SpecA04 ./consensus/wbft/engine` |
| `genesis-b02/` | B-02, B-06 | 8282/8283 preset genesis block 을 만들고 hash, state root, extra byte, base fee 예제를 확인한다 | `go run .` |
| `slots-b04/` | B-04, B-08 | Go slot reader 로 GovValidator storage 를 읽고, 그 결과를 reference EVM 에서 실행한 내장 contract bytecode 의 결과와 대조한다 (`xcheck/`) | `go run .`, `go run ./xcheck` |

`vectorgen/` 을 뺀 프로그램들은 명세를 작성하는 동안 쓴 도구이며 vector generator 가 아니다. generator 의 출력 형식은 `A-11` 이 정의하고, 이 초안 프로그램들은 `vectorgen/` 을 만드는 출발점이 되었다.
