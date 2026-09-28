# vectorgen: conformance test vector generator

`vectorgen` 은 `A-11` §3 의 test vector 를 `../../vectors/` 에 쓴다. 모든 기댓값은 reference 구현(go-stablenet `740526d03`)을 불러서 계산한다. 이 directory 의 다른 도구처럼 `go.mod` 의 `replace` directive 로 reference 를 연결한다 ("replace 를 쓰는 별도 module" 방식). reference repository 는 수정하지 않는다.

> 해설: 프로그램 파일은 영문판 디렉터리 `../../../spec/tools/vectorgen/` 에만 있다. 아래 명령은 그 디렉터리에서 실행한다.

1단계(이 디렉터리의 main 프로그램)는 가장 아래 층(`A-01`, `A-02`, `A-03`), 즉 runner `crypto` 와 `encoding` 을 다룬다. 2단계(`stage2/` 의 프로그램)는 runner `validators`, `timers/round_timeout`, `chain/config_at`, runner `header`(묶음 검증 `verify_headers` 포함), runner `execution`(`p256_verify`, `gas_tip_enforcement`, `fee_delegation`, `process_finalize`), runner `source`(`candidates_at_epoch`), runner `governance`(`scenarios`)를 다룬다. 3단계(`stage3/` 의 프로그램)는 runner `state_machine`(`check_message`, `is_justified`, 단계형 `rounds`), `network/receive_outcome`, `timers/build_wait`, `chain/fork_schedule`, `chain/genesis` 를 다룬다.

## 실행

```
cd tools/vectorgen
GOTOOLCHAIN=go1.23.12 go run . -out ../../vectors
```

다음 경우에 프로그램은 실행을 거부한다. 첫째, toolchain 이 `go1.23.12` 가 아닐 때다. 둘째, replace 한 module 이 `HEAD` 가 `740526d03` 으로 시작하는 git checkout 이 아닐 때다. 셋째, 그 checkout 에 로컬 변경이 있을 때다. `meta.yaml` 에는 checkout 에서 읽은 전체 commit hash 를 적는다. 다른 머신에서는 먼저 `replace` 경로를 바꾼다 (`../README.md` 참조).

2단계도 같은 방법으로 실행하고, 같은 검사를 한다.

```
GOTOOLCHAIN=go1.23.12 go run ./stage2 -out ../../vectors
```

2단계 프로그램은 reference checkout 에서 `go test` 도 실행하며 ("2단계: reference 를 실행하는 방법" 참조), 실행이 끝난 뒤 checkout 이 여전히 깨끗한지 검사한다.

3단계도 같다 ("3단계: reference 를 실행하는 방법" 참조).

```
GOTOOLCHAIN=go1.23.12 go run ./stage3 -out ../../vectors
```

각 프로그램은 자기가 만드는 handler 디렉터리(예: `vectors/crypto/<handler>/`, `vectors/encoding/<handler>/`)만 지우고 다시 쓴다. 다른 handler 의 vector 는 건드리지 않는다. 실행이 끝나면 handler 마다 case 수를 출력한다.

`-skip-ref-check` 는 toolchain 과 checkout 검사를 끈다. 이 선택지는 개발할 때만 쓴다. 이 선택지로 쓴 vector 는 commit 을 `unchecked` 로 적으며, 유효한 vector 가 아니다 (WBFT-VEC-011, WBFT-VEC-020).

판에서 뺀 requirement. 명세의 어떤 판은 일부 requirement 를 뺄 수 있고, 그 판의 vector 는 그 requirement 를 적지 않는다. generator 소스는 그런 requirement 를 별칭(requirement 목록에서는 `"@r01"`, 설명에서는 `{@r01}`)으로 가리키고, `internal/vecfile/requirements.tsv` 가 별칭을 requirement ID 로 바꾼다. 어떤 판과 함께 공개하는 이 표에서는, 그 판에서 뺀 requirement 의 별칭이 모두 `-` 이다. 세 프로그램은 쓰기 전에 별칭을 푼다. `-` 인 requirement 는 `requirements:` 에서 빼고, 설명이 그 requirement 를 가리키거나 requirement 가 하나도 남지 않은 case 는 쓰지 않는다. `-withheld FILE` 을 주면 `FILE` 에 적힌 requirement ID 도 같은 방법으로 뺀다 (한 줄에 ID 하나, `#` 뒤는 주석이다). 전체 표를 쓰고 `-withheld` 를 주지 않으면 아무것도 빼지 않는다. 프로그램은 뺀 requirement 참조와 case 의 수를 출력한다.

## 검사

결정성: generator 를 두 번 돌려 결과를 비교한다.

```
GOTOOLCHAIN=go1.23.12 go run . -out /tmp/v1 && GOTOOLCHAIN=go1.23.12 go run . -out /tmp/v2 && diff -r /tmp/v1 /tmp/v2
GOTOOLCHAIN=go1.23.12 go run ./stage2 -out /tmp/w1 && GOTOOLCHAIN=go1.23.12 go run ./stage2 -out /tmp/w2 && diff -r /tmp/w1 /tmp/w2
GOTOOLCHAIN=go1.23.12 go run ./stage3 -out /tmp/s1 && GOTOOLCHAIN=go1.23.12 go run ./stage3 -out /tmp/s2 && diff -r /tmp/s1 /tmp/s2
```

독립 교차 검사: `xcheck/xcheck.py` 는 go-stablenet 없이 기댓값의 일부를 다시 계산한다. 이때 pycryptodome 의 Keccak-256, python-ecdsa, pyrlp, 그리고 순수 Python 으로 쓴 secp256k1 recovery, BLS key 유도, BLS12-381 G1 연산을 쓴다. 이 script 에는 `pycryptodome`, `ecdsa`, `rlp`, `pyyaml` 이 필요하다.

```
python3 xcheck/xcheck.py ../../vectors
```

| Handler | 다시 계산하는 방법 |
|---|---|
| `keccak256`, `randao_data`, `randao_mix`, `dedup_key` | Keccak-256 |
| `ecdsa_sign` | python-ecdsa, SHA-256 을 쓰는 RFC 6979, low S |
| `ecdsa_recover` | `A-02` §3.3 의 cgo 수락 규칙을 따르는 secp256k1 recovery |
| `bls_derive` | HKDF KeyGen (`A-02` §5.1) 과 G1 scalar 곱셈 |
| `aggregate_public_keys` | WBFT-CRYPTO-023 과 WBFT-CRYPTO-056 의 검사를 하는 G1 decompression, 그 뒤 point 덧셈 |
| `seal_data`, `block_hash`, `hash_with_round`, `filtered_header` | pyrlp 와 Keccak-256 |
| `signing_payload` | 서명된 필드를 pyrlp 로 다시 인코딩하고 secp256k1 recovery 를 한다. 메시지 안에 들어 있는 payload 도 같게 검사한다 |

교차 검사하지 않는 것은 BLS signing, verify, signature aggregate 다. 이 연산에는 G2 위의 hash-to-curve 와 pairing 이 필요하기 때문이다. `extra_codec` 과 `message_codec` 이 디코드한 필드도 교차 검사하지 않는다. 이 값들의 기댓값은 reference 에서만 나온다.

2단계에는 따로 교차 검사 `xcheck/xcheck_stage2.py` 가 있다 (필요한 package 는 같다). 이 script 는 go-stablenet 없이 명세의 의사코드로 2단계 값을 다시 계산한다.

```
python3 xcheck/xcheck_stage2.py ../../vectors
```

| Handler | 다시 계산하는 방법 |
|---|---|
| `quorum` | binary64(Python float)로 계산한 `A-04` §2.1, 그리고 `(2n)//3 + 1` |
| `proposer` | `uint64` seed 로 계산한 `A-04` §5.1 `calc_proposer` |
| `epoch_boundary` | `A-04` §4.1 |
| `shuffle` | `A-04` §6.4 `compute_shuffled_index` (Keccak-256) |
| `validators_at` | 디코드한 fixture 위의 `A-04` §3.1 `epoch_info_for`. canonical 을 먼저 보고, 없으면 거슬러 올라간다 |
| `next_epoch_info` | candidate 순서(diligence 기준 stable sort)와 위의 shuffle 을 쓰는 `A-04` §6.2 `compute_next_epoch_info`. `A-04` §6.8 의 세 epoch 은 거기에 적힌 diligence 값과도 비교한다 |
| `config_at` | `A-01` §6.5. transition 은 block 순서로 정렬한다(stable sort) |
| `round_timeout` | `A-06` §4.1 과 WBFT-TIMER-007, WBFT-TIMER-008 의 Warn 기록 |
| `build_proposal_header` | `A-08` §3.2. 고정 필드, `Time`, builder 가 정한 필드가 그대로인지, gas tip, 이전 seal bitmap(병합했거나 그대로인지), `MixDigest`, randao reveal 을 검사한다. reveal 은 python-ecdsa 로 SHA-256 을 쓰는 RFC 6979, low S 로 다시 만든다 |
| `verify_headers` | `A-08` §6.7 의 묶음 규칙만 검사한다. header 마다 판정이 하나 있는지, 처음으로 수락되지 않은 header 뒤의 판정이 모두 `reject` 인지, 번호가 묶음 안의 앞 header 다음 번호가 아닌 header 가 `accept` 가 아닌지 본다 |

2단계에서 교차 검사하지 않는 것은 `verify_header`, `verify_light`, 그리고 `verify_headers` 의 개별 판정이다. 그 판정에는 BLS pairing 이 필요하기 때문이다. `build_proposal_header` 가 병합한 aggregate signature 도 교차 검사하지 않는다.

runner `execution`, `source`, `governance` 에는 따로 교차 검사 `xcheck/xcheck_execution.py` 가 있다 (필요한 package 는 같다).

```
python3 xcheck/xcheck_execution.py ../../vectors
```

| Handler | 다시 계산하는 방법 |
|---|---|
| `p256_verify` | `B-07` SNET-TX-089. 길이 규칙, 좌표와 범위 검사, curve 위의 점인지 검사하고, 순수 Python 으로 NIST P-256 ECDSA 검증을 한다. `BohoBlock` 부터는 gas 6 900, 그 전에는 gas 0 이다 |
| `gas_tip_enforcement`, `fee_delegation` | 트랜잭션을 pyrlp 로 디코드하고, `B-07` §4.2 의 signing hash 로 sender 와 fee payer 를 복원한다. receipt 마다 SNET-TX-010/011 의 tip 으로 SNET-TX-012 의 gas price 를 계산하고 `AuthorizedTxExecuted` log 를 확인한다. receipt 의 gas used 로 `B-07` §7.1 의 흐름을 따라 block 뒤의 잔액을 계산한다. 실패 case 에서는 `B-07` §3 ~ §6 의 규칙 가운데 하나가 트랜잭션을 거부하는지 본다 |
| `process_finalize` | fixture 의 `EpochInfo` 로 `B-06` §3 의 base fee 분배(몫과, coinbase 로 가는 나머지)를 계산해 잔액과 비교한다. gas tip 비교와 `EpochInfo` 유무 규칙도 본다 |
| `candidates_at_epoch` | 입력 storage 위에서 `B-04` §8.2 의 reader(`0x33` 의 `AddressSet`, mapping `0x37` 의 Solidity `bytes`, `0x39` 의 word)를 다시 계산한다 |
| `scenarios` | call data 와 log 로 움직이는 `B-05` §8, §10 의 모형으로 단계마다 투영(swap-and-pop 을 하는 validator 목록, key, gas tip, blacklist 와 authorized 집합)을 계산하고, revert 마다 `B-05` 의 custom error 인지 본다 |

교차 검사하지 않는 것은 state root, epoch block 의 `EpochInfo`(그 diligence 에는 `A-04` §6 의 seal 이력이 필요하다), 트랜잭션의 gas used, 거버넌스 단계의 log 와 쓰기 집합이다.

3단계에는 따로 교차 검사 `xcheck/xcheck_stage3.py` 가 있다 (필요한 package 는 같다). 이 script 는 명세의 의사코드와 표로 다시 계산한다.

```
python3 xcheck/xcheck_stage3.py ../../vectors
```

| Handler | 다시 계산하는 방법 |
|---|---|
| `check_message` | `A-05` §15 `check_message` |
| `is_justified` | `A-05` §11.6 `is_justified`. 판정과, 설명에 적힌 실패 단계를 비교한다 |
| `rounds`, `receive_outcome` | message, backlog, frame 단계마다 앞 단계의 snapshot 과 payload 에서 디코드한 view(pyrlp)로 `check_message` 를 계산해 `check` 와 비교한다. snapshot 마다 `proposer` 를 `calc_proposer`(`A-04` §5.1)와 마지막 view 변경이 읽은 head 로 계산한다. 시작한 timer 마다 view 를 비교한다. `timer` 가 있는 timer 단계마다, `A-06` 의 취소 규칙(round timer 는 걸려 있는 timer 를 모두 취소하고, retry 와 future timer 는 같은 종류의 앞 timer 를 취소하며, stop 은 모두 취소한다)의 모형으로 그 단계가 효과를 가질 수 있는지 본다. engine 이 멈춘 동안에는 아무 일도 없는지 본다. `encoded` 가 있는 보낸 메시지마다 `A-03` 의 signing payload 로 secp256k1 recovery 를 해서 signer 를 비교한다. finalize 의 sealer 를 저장된 PREPARE 와 COMMIT 과 비교한다 |
| `receive_outcome` | dedup key(Keccak-256)를 계산한다. frame 마다 `A-07` §5 와 §8 의 모형(크기 상한, code, 빈 payload, 첫 RLP item 으로 `0x11` 풀기, engine 정지, known cache)으로 부류를 계산한다. relay 와 자기 broadcast 가 노드 자신과 보낸 peer 를 뺀 연결된 validator 에게만 가는지 검사한다 |
| `fork_schedule` | `B-01` §3 의 술어, SNET-CFG-005 와 SNET-CFG-006 의 rule flag, 적용 중인 system contract 와 upgrade(`B-01` §7), EIP-2124 fork ID(preset genesis hash 또는 같은 설정의 `chain/genesis` vector hash 위의 CRC32) |
| `genesis` | header 의 Keccak-256, `B-02` §2 의 header 필드, `B-01` §11.2 의 초기 validator 로 다시 만든 genesis extra(`B-02` §3), `B-01` §11.1 의 preset hash |

3단계에서 교차 검사하지 않는 것은 genesis block 의 state root(`B-02` §4 의 system contract storage 가 필요하다), BLS seal, 그리고 위 검사를 넘는 state snapshot 의 내용이다.

변조 시험: `vectors/` 복사본에서 3단계 handler 여섯 개의 기댓값을 하나씩 바꾸면(class, 판정, `check`, retry round, outcome, fork ID, genesis extra) `xcheck_stage3.py` 가 일곱 변경을 모두 보고한다. 파일 세 개에 따옴표 없는 숫자, 주석 줄, 닫히지 않은 flow sequence 를 쓰면 `check_yaml_subset.py` 가 세 문제를 모두 보고한다.

## Adapter (WBFT-VEC-062)

`adapter/` 는 `A-11` §3.4 의 adapter 프로토콜 `wbft-vector/1` 로 이 vector 의 case 에 답한다. 시작할 때 생성기 프로그램 세 개를 임시 디렉터리에 돌린다(toolchain 과 reference 검사도 같다). 그래서 모든 답은 이 실행에서 reference 로 계산한 값이다. adapter 는 그 case 들을 runner, handler, 입력으로 찾아 둔다. runner 가 쓰는 vector 파일은 읽지 않는다 (WBFT-VEC-041). 생성기가 쓰지 않은 입력의 case 에는 `unsupported` 로 답한다. adapter 는 자기 생성기의 vector 를 다룰 뿐 임의의 입력을 다루지 않기 때문이다. 실패 case 에는 reference 오류를 `message` 에 담아 `error` 로 답한다. `check_adapter.py` 는 vector 디렉터리의 모든 case 를 보내고 WBFT-VEC-047, WBFT-VEC-048 대로 판정하는 최소 runner 다.

```
GOTOOLCHAIN=go1.23.12 go build -o /tmp/vg-adapter ./adapter
GOTOOLCHAIN=go1.23.12 python3 check_adapter.py ../../vectors -- /tmp/vg-adapter
```

`-from DIR` 을 주면 생성하지 않고 `DIR` 에 있는 이전 생성 결과로 답한다. 기댓값을 바꾸면 그 case 는 실패로 드러나고, 입력을 바꾸면 unsupported 로 드러난다.

## 파일

case 하나는 디렉터리 `vectors/<runner>/<handler>/<case>/` 이다. 이 디렉터리에는 `meta.yaml`, `input.yaml` 이 있고, 연산이 실패해야 하는 case 가 아니면 `expected.yaml` 도 있다 (WBFT-VEC-010). 형식 규칙은 `A-11` §3.1 (WBFT-VEC-013 부터 WBFT-VEC-016 까지) 에 적었다. 그 규칙은 JSON 으로 일대일 변환되는 YAML 부분집합, 10진 문자열로 쓰는 정수, `0x` 16진수로 쓰는 바이트, 없는 값을 뜻하는 `null`, informative 인 `expected_error` 를 정한다.

| 소스 파일 | 내용 |
|---|---|
| `main.go` | 1단계 명령행 처리 |
| `internal/vecfile/` | 두 단계가 함께 쓴다. 결정적인 YAML 출력기, case 형식, reference 검사, case 디렉터리와 `meta.yaml` 작성 |
| `yaml.go` | 1단계 소스가 쓰는, 공유 출력기 형식의 별칭 |
| `fixtures.go` | 키 (WBFT-VEC-021) 와 header `H`, commit 된 `H`, block 3 |
| `crypto.go` | runner `crypto` |
| `encoding.go` | `extra_codec`, `block_hash`, `hash_with_round`, `filtered_header` |
| `messages.go` | `message_codec`, `signing_payload`, `dedup_key` |
| `xcheck/xcheck.py` | 1단계 독립 교차 검사 |
| `check_yaml_subset.py` | YAML 부분집합과 case 배치 검사 (`A-11` WBFT-VEC-013 부터 WBFT-VEC-016 까지) |
| `stage2/main.go` | 2단계 명령행 처리 |
| `stage2/common.go` | 키, 설정의 모양, chain fixture (reader 와 builder) |
| `stage2/validators.go` | `quorum`, `proposer`, `epoch_boundary` |
| `stage2/config.go` | `chain/config_at` |
| `stage2/header.go` | chain fixture, `validators_at`, `verify_header`, `verify_light`, `build_proposal_header`, `verify_headers` |
| `stage2/execution.go` | runner `execution`: `p256_verify`, `gas_tip_enforcement`, `fee_delegation`, 입력 state |
| `stage2/finalize.go` | `execution/process_finalize` |
| `stage2/source.go` | runner `source`: `candidates_at_epoch` |
| `stage2/governance.go`, `stage2/governance_cases.go` | runner `governance`: `scenarios` (단계 실행기와 `B-05` §13 의 시나리오) |
| `xcheck/xcheck_execution.py` | runner `execution`, `source`, `governance` 의 독립 교차 검사 |
| `stage2/overlay.go` | 주입한 생성 test 를 실행하고 그 case 를 읽는다 |
| `stage2/overlay/engine/zz_vectorgen_stage2_test.go` | `consensus/wbft/engine` 에 주입한다. `shuffle`, `sort_candidates`, `next_epoch_info` |
| `stage2/overlay/core/zz_vectorgen_stage2_test.go`, `zz_vectorgen_stage2_hook.go` | `consensus/wbft/core` 에 주입한다. `round_timeout` |
| `xcheck/xcheck_stage2.py` | 2단계 독립 교차 검사 |
| `internal/vecfile/jsonl.go` | 주입한 생성 test 가 쓴 JSON-lines case 를 읽는다 (2단계와 3단계) |
| `stage3/main.go` | 3단계 명령행 처리 |
| `stage3/chain.go` | `chain/fork_schedule`, `chain/genesis` |
| `stage3/overlay.go` | overlay(더하는 파일과 "3단계: reference 를 실행하는 방법" 의 한 줄 수정)를 쓰고, 주입한 생성 test 를 실행하고, 그 case 를 읽는다 |
| `stage3/overlay/core/zz_vectorgen_stage3.go` | `consensus/wbft/core` 에 더하는 test 가 아닌 파일이다. hook, 가짜 애플리케이션 `VGApp`, 단계 driver `VGDriver`, vector 키로 block 과 메시지를 만드는 함수를 담는다 |
| `stage3/overlay/core/zz_vectorgen_stage3_test.go` | `consensus/wbft/core` 에 주입한다. `check_message`, `is_justified`, `rounds` |
| `stage3/overlay/backend/zz_vectorgen_stage3_hook.go`, `zz_vectorgen_stage3_test.go` | `consensus/wbft/backend` 에 주입한다. `receive_outcome`, `build_wait` |
| `xcheck/xcheck_stage3.py` | 3단계 독립 교차 검사 |
| `adapter/main.go` | WBFT-VEC-062 의 adapter (`wbft-vector/1`) |
| `internal/vecfile/parse.go` | YAML 부분집합 reader (adapter 가 쓴다) |
| `check_adapter.py` | vector 를 adapter 와 대조하는 `wbft-vector/1` 최소 runner |

키: `i = 0 … 7` 에 대해 (2단계는 `i = 0 … 15`) `key_i = keccak256(ASCII("wbft-spec-vector-key-" ‖ decimal(i)))` 이며, `A-02` §8.1 의 키와 같다. case 하나(`aggregate_public_keys/decode_pk_x_plus_p`)는 같은 seed 의 키를 더 찾아서, `x + p` 가 381 bit 안에 들어가는 BLS public key 를 쓴다. 그 case 의 설명에 키 번호를 적었다.

## Handler 의 입력과 출력

| Handler | `input.yaml` | `expected.yaml` |
|---|---|---|
| `crypto/keccak256` | `data` | `hash` |
| `crypto/ecdsa_sign` | `private_key`, `data` | `signature` (65 바이트), `address` |
| `crypto/ecdsa_recover` | `data`, `signature` | `address` |
| `crypto/bls_derive` | `private_key` (node key) | `secret_key`, `public_key` |
| `crypto/bls_sign` | `secret_key`, `message` | `signature` |
| `crypto/bls_verify` | `public_keys` (목록이며 먼저 aggregate 한다), `message`, `signature` | `valid` (bool) |
| `crypto/bls_aggregate` | `signatures` (목록) | `signature` |
| `crypto/aggregate_public_keys` | `public_keys` (목록) | `public_key` |
| `crypto/seal_data` | `header` (RLP), `round`, `seal_type` | `seal_data` |
| `crypto/randao_data` | `chain_id`, `number` | `randao_data` |
| `crypto/randao_mix` | `parent_mix`, `reveal` | `mix` |
| `encoding/extra_codec` | `extra` | 디코드한 필드 열 개 (없는 seal 과 `epoch_info` 는 `null`) 와 `encoded` |
| `encoding/block_hash` | `header` (RLP) | `hash` |
| `encoding/hash_with_round` | `header`, `round` | `hash` |
| `encoding/filtered_header` | `header`, `round` | `header` (RLP) |
| `encoding/message_codec` | `code`, `payload` | `type`, 디코드한 필드, `encoded` |
| `encoding/signing_payload` | `code`, `payload` | `signing_payload`, `signer`, `embedded` (`signing_payload`, `signer` 의 목록) |
| `encoding/dedup_key` | `payload` | `key` |
| `validators/quorum` | `n` | `f_float64_bits` (F 를 big-endian binary64 8 바이트로 적은 값), `quorum`, `f_plus_one` |
| `validators/proposer` | `validators` (address), `policy` (식별자), `last_proposer`, `round` | `index`, `address` (집합이 비면 둘 다 `null`) |
| `validators/epoch_boundary` | `config` (`wbft`, `transitions`), `number` | `is_epoch_block` (bool), `last_epoch_block` |
| `validators/validators_at` | `chain`, `number`, `parent_hash` | `validators` (`address`, `bls_public_key` 의 목록), `proposer_policy` |
| `validators/shuffle` | `seed`, `count`, `indices` (목록) | `shuffled` (같은 순서의 목록) |
| `validators/sort_candidates` | `diligences` (목록. power 는 모두 1) | `order` (candidate index) |
| `validators/next_epoch_info` | `chain` (epoch block 의 부모에서 끝난다), `header` (`EpochInfo` 가 없는 epoch header), `candidates` (`address`, `bls_public_key` 의 목록. key 가 없으면 `"0x"`) | `epoch_info` (`address`, `diligence` 로 적은 `candidates`, `validators`, `bls_public_keys`). `header` 가 epoch block 이 아니면 `null` |
| `timers/round_timeout` | `request_timeout` (`config_at` 이 주는 ms 값), `max_request_timeout_seconds`, `round` | `timeout` (ns, 부호 있음), `warning` (`none`, `cap_overflow_guard`, `max_int64_clamp`) |
| `timers/build_wait` | `block_period` (`config_at` 이 새 height 에 주는 초 값), `head_time` (Unix 초), `round`, `now` (Unix nanosecond) | `wait` (ns, 0 이상) |
| `chain/config_at` | `config` (`wbft`, `transitions`), `number` | `request_timeout` (ms), `block_period`, `epoch`, `proposer_policy` (식별자 또는 `null`), `max_request_timeout_seconds`, `allowed_future_block_time` |
| `header/build_proposal_header` | `chain`, `header` (builder 가 만든 뼈대, RLP), `node_key`, `gas_tip` (`null` 이면 조회가 실패한다), `extra_prepared`, `extra_committed` (`sealer`, `seal` 의 목록), `now` | `header` (RLP) |
| `header/verify_header` | `chain`, `header` (RLP), `now` (Unix 초) | `verdict` (`accept`, `reject`, `deferred`) |
| `header/verify_headers` | `chain`, `headers` (입력 순서의 묶음, RLP 목록), `now` (Unix 초) | `verdicts` (header 마다 `accept`, `reject`, `deferred` 가운데 하나를 적은 목록) |
| `header/verify_light` | `chain`, `header` (RLP) | `result` (`VALID`, `INVALID`, `CANNOT_DECIDE`) |
| `state_machine/check_message` | `view` (`sequence`, `round`), `state` (`AcceptRequest`, `Preprepared`, `Prepared`, `Committed`), `prior_round`, `code`, `message_view` | `result` (`PROCESS`, `FUTURE`, `OLD`, `INVALID`, `TOO_FAR`, `EXTRA_SEAL`) |
| `state_machine/is_justified` | `proposal` (block hash), `target_view`, `round_changes` (`source`, `sequence`, `round`, `prepared_round`, `prepared_digest` 의 목록), `prepares` (`source`, `sequence`, `round`, `digest` 의 목록), `quorum` | `justified` (bool) |
| `state_machine/rounds` | `initial` (`app` 에 `invalid_proposals`, `future_proposals`, `bad_blocks`, `finalize`), `steps` (`A-11` §3.1 "Steps". `timer` 가 있는 timer 단계, `future_timeout`, `stop`, `start` 포함) | `start`, `steps` (단계마다 기록 하나. `timers` 의 종류는 `round`, `retry`, `future`) |
| `network/receive_outcome` | `initial` (`engine`, `synchronising`, `peers` 를 더한다), `steps` (`frame` 을 더한다) | `start`, `steps` (`outcome`, `dedup_key`, `relay_to` 를 더한 기록) |
| `chain/fork_schedule` | `config` (`preset`, `overrides`: `applepie_block`, `boho_block`, `shanghai_time`, `cancun_time`. `null` 이면 preset 값을 그대로 둔다), `number`, `time` | `forks` (`B-01` §3.1 의 술어와 `anzeon`), `rules` (WBFT block 의 flag), `system_contracts` (적용 중인 것, `B-01` §7.2), `upgrades_at` (`number` 의 항목), `fork_id` (`hash`, `next`) |
| `execution/p256_verify` | `config` (`preset`, `overrides`: `applepie_block`, `boho_block`), `number`, `input` | `output`, `gas` |
| `execution/gas_tip_enforcement`, `execution/fee_delegation` | `config`, `header` (RLP), `accounts` (`address`, `balance`, `nonce`, `extra`, `code`, `storage` 의 목록), `transactions` (binary encoding) | `receipts` (`status`, `gas_used`, `cumulative_gas_used`, `effective_gas_price`, `logs` 의 목록. log 는 `address`, `topics`, `data`), `accounts` (block 의 트랜잭션 뒤의 `address`, `balance`, `nonce`) |
| `execution/process_finalize` | `chain`, `header` (RLP), `parent_gas_tip` (`null` 이면 부모 state 를 얻을 수 없다), `accounts` (트랜잭션 뒤), `path` (`import`, `propose`) | `root`, `epoch_info` (proposer 경로가 쓴 것. 그 밖에는 `null`), `accounts` (`address`, `balance`, `code_hash`) |
| `source/candidates_at_epoch` | `config`, `number`, `accounts` | `candidates` (`address`, `bls_public_key` 의 목록), `gas_tip` |
| `governance/scenarios` | `genesis` (`preset`, `gov_validator`, `gov_council`, `alloc`), `genesis_time`, `named_accounts`, `steps` (`label`, `sender`, `to`, `data`, `gas`, `time`, `number`) | `steps` (`status`, `revert_data`, `logs`, `address`, `key`, `value` 로 적은 `write_set`, `validators`, `bls_public_keys`, `gas_tip`, `blacklisted`, `authorized` 로 적은 `projection`) |
| `chain/genesis` | `genesis` (`preset`, `overrides`: `timestamp`, `gas_limit`, `difficulty`, `number`, `extra_data`, `boho_block`, `alloc_add` (`address`, `balance` 의 목록)) | `header` (RLP), `hash`, `state_root`, `extra` |

key 하나와 signature 하나를 디코드하는 case (WBFT-CRYPTO-056) 는 원소가 하나인 목록을 받는 `aggregate_public_keys` 와 `bls_aggregate` 로 표현한다. 이때 결과는 디코드한 point 를 다시 압축한 값이다.

`chain` (선택 필드 `non_canonical` 을 포함한 chain fixture) 과 `config` 의 모양은 `A-11` §3.1 ("Chain fixture") 에, steps case 의 모양은 `A-11` §3.1 ("Steps") 에 정의했다. 시각에 달린 case 는 `now = 2000000000` 을 쓴다. 생성기는 그 시각이 지나면 실행을 거부한다. 생성기의 chain 은 시각을 맞춰 두어서, 생성할 때의 실제 시계로 계산한 결과가 `now` 로 계산한 결과와 같다. 검증할 header 는 과거에 있고, proposal 의 부모는 `now` 보다 뒤에 있다.

## 2단계: reference 를 실행하는 방법

export 된 함수는 2단계 프로세스 안에서 부른다. 부르는 함수는 `validator.NewSet` (`F`, `QuorumSize`, `CalcProposer`), `Engine.IsEpochBlockNumber`, `eth/ethconfig.SetConfigFromChainConfig` 와 `Config.GetConfig` (노드가 쓰는 경로이며 transition 을 정렬한다), `Engine.GetValidators`, `backend.New` 와 `Backend.Prepare` (fixture chain 을 만들 때), `Engine.Prepare` (`build_proposal_header` 에서 extra seal 을 인자로 넘길 때), `Engine.CommitHeader`, `Backend.VerifyHeader`, `Backend.VerifyHeaders` 다. `Backend.VerifyHeader` 는 validator 집합 조회 V0a/V0b 를 한 뒤 `check_seals` 를 켜고 header 를 검증한다. `Backend.VerifyHeaders` 는 묶음을 검증하며, 생성기는 결과를 입력 순서대로 모두 읽으므로 abort channel 을 쓰지 않는다. fixture chain 은 vector 에 쓴 RLP 를 다시 디코드해서 검증한다.

실행 handler 는 입력 `accounts` 로 메모리에 `state.StateDB` 를 만든다. 그리고 `vm.EVM.Call`(`p256_verify`. checkout 의 `core/vm/testdata/precompiles/p256Verify.json` 에 있는 782 항목을 읽고, 결과를 그 파일과 하나씩 비교한다), `StateProcessor.Process` 가 block 의 트랜잭션을 적용하는 것처럼 `SetTxContext` 와 함께 트랜잭션 순서대로 부르는 `core.ApplyTransaction`(`gas_tip_enforcement`, `fee_delegation`), 부모 gas tip 에 답하는 `StateAt` 을 가진 fixture chain 위의 `Engine.Finalize` 나 `Engine.FinalizeAndAssemble`(`process_finalize`)을 부른다. `candidates_at_epoch` 는 `Engine.GetGovCandidates`, `wbft.Config.GetSystemContracts` 의 address 에서의 `systemcontracts.GetBLSPublicKey`, `systemcontracts.GetGasTip` 을 부른다. case 의 GovValidator storage 는 reference 초기화 함수 `systemcontracts.GetSystemContractsTransition`(genesis 경로)이 쓴다. 두 case 에서는 그 뒤에 설명이 밝힌 변경 하나를 손으로 더 한다. 트랜잭션은 vector key 로 서명한다(`types.SignTx`, Anzeon signer, fee payer 에는 `types.NewFeeDelegateSigner`).

`governance/scenarios` 는 case 의 거버넌스 파라미터를 넣은 mainnet preset genesis 로 `core.SetupGenesisBlock` 을 불러 genesis 를 만든다. 그리고 `core.ApplyTransaction` 처럼 단계마다 자기 block 안에서 `core.ApplyMessage` 로 호출을 적용한다. 이때 EVM logger(`vm.Config.Tracer`)가 모든 `SSTORE` slot 과 호출 시작 때의 값을 적어 두고, 쓰기 집합에는 값이 바뀐 slot 만 남긴다. 투영은 `systemcontracts.ValidatorList`, `GetBLSPublicKey`, `GetGasTip`, `StateDB.GetExtra` 로 읽는다. `systemcontracts/test` 의 harness 는 쓰지 않는다. 그 harness 는 solc 0.8.14 와 OpenZeppelin 소스로 contract 를 compile 하는데, checkout 에 그 소스가 없기 때문이다. 어느 쪽이든 실행되는 contract 는 genesis 의 artifact 다.

export 되지 않은 함수는 `go test -overlay` 로 reference package 에 주입한 생성 test 안에서 실행한다. 이것은 `../epoch-a04` 의 방법이다. overlay 파일은 절대 경로로 임시 디렉터리에 만들어지므로 손으로 고칠 것이 없다.

- `consensus/wbft/engine`: `computeShuffledIndex`, `sortCandidates`, 그리고 test 안에서 만든 chain 위의 `buildEpochInfo` 를 실행한다. 그 chain 의 coinbase 는 `CalcProposer` 로 정하고, 이전 seal bitmap 에는 자리만 채운 signature 를 넣으며, `MixDigest` 는 `A-04` §6.8 처럼 `keccak256(number)` 로 둔다. candidate 목록은 reference 가 `govValidator` 파라미터로 초기화한 GovValidator storage 에서 `systemcontracts.ValidatorList` 와 `GetBLSPublicKey` 로 읽는다. 이 package 는 `eth/ethconfig` 를 import 할 수 없으므로 (import cycle), 이 test 는 reference 의 test 용 복사본 `wbft.SetConfigFromChainConfig` 를 쓴다. 그 복사본의 transition 정렬은 같다.
- `consensus/wbft/core`: `newRoundChangeTimer` 를 실행한다. 이 함수가 계산한 timeout 은 `time.AfterFunc` 에만 넘어간다. 그래서 overlay 는 `core.go` 를 한 줄만 다른 복사본으로 바꾼다. 그 줄은 `c.roundChangeTimer = vectorgenAfterFunc(timeout, func() {` 이다. overlay 는 `var vectorgenAfterFunc = time.AfterFunc` 를 담은 `zz_vectorgen_stage2_hook.go` 도 더한다. test 는 이 변수를 바꿔서 duration 을 기록하고, log handler 로 Warn 기록을 읽는다. 그 줄이 정확히 한 번 나오지 않으면 생성기는 실행을 거부한다.

test 는 case 마다 JSON 객체 하나를 쓴다. 정수는 10진 문자열, 바이트는 16진수이고, key 순서를 지킨다. 프로그램은 이 객체를 공유 writer 로 vector 파일로 바꾼다. `verify_light` 에는 reference 함수가 없다. 그래서 그 결과는 `Engine.GetValidators`, `Engine.VerifyHeader` (부모는 hash 로 찾고 `check_seals` 를 켠다), `Engine.IsEpochBlockNumber` 를 조합해서 만들며, case 마다 그렇다고 밝힌다 (`A-11` WBFT-VEC-020, handler 모양 8).

2단계는 `eth/ethconfig` 와 backend 를 import 하므로, 이 module 의 `go.mod` 에는 reference 의 의존성 전체가 필요하다. 그래서 `go.sum` 에는 reference `go.sum` 의 항목이 들어 있다.

만들지 않은 것은 다음과 같다. binary64 반올림이 결과를 바꾸는 크기의 `quorum` 은 그런 집합을 만들 수 없어서 만들지 않았다 (`A-10` §2). H1 과 H9 의 `verify_header` case 는 RLP 로 표현할 수 없다 (`A-11` §3.3 참조). H15b 와 H21 은 fixture 에 state 가 없어서 만들지 않았다. `now` 가 부모 시각과 block period 의 합보다 큰 `build_proposal_header` 는 reference 가 실제 시계를 읽어서 만들지 않았다.

`validators_at` 과 `verify_header` 의 fork case(WBFT-HDR-071)는 fixture 필드 `non_canonical` 을 쓴다. `stage2/header.go` 의 `forkBuilder` 가 chainA 와 같은 키와 시각으로 branch 4', 5'(그리고 block 6')를 만들며, 4' 에는 다른 `EpochInfo` 를 넣는다.

## 3단계: reference 를 실행하는 방법

`fork_schedule` 과 `genesis` 는 3단계 프로세스 안에서 export 된 함수를 부른다. 부르는 함수는 `params.ChainConfig` 의 fork 술어와 `Rules`, `eth/ethconfig.SetConfigFromChainConfig` 뒤의 `wbft.Config.GetSystemContracts`(genesis 항목과 `CollectUpgrades` 를 붙인다), `forkid.NewID`, 빈 메모리 database 위의 `core.SetupGenesisBlock` 이다. preset 은 복사본을 바꿔서 쓴다.

state machine 과 network case 는 `consensus/wbft/core` 와 `consensus/wbft/backend` 에 `go test -overlay` 로 주입한 생성 test 안에서 reference `Core` 를 돌린다. 생성기는 `handleEvents` 가 하는 것처럼 코어를 동기적으로 부른다. 메시지에는 `handleEncodedMsg` 뒤에 `Gossip` 을 부르고, backlog replay 에는 `handleDecodedMessage` 뒤에 다시 인코딩한 바이트로 `Gossip` 을 부른다. request 에는 `handleRequest` 를 부르고 미래 request 이면 `storeRequestMsg` 도 부른다. round timeout 에는 `handleTimeoutMsg` 를 부른다 (번호로 가리킨 timer 이면 `handleEvents` 처럼 그 timer 의 취소 flag 가 서 있지 않을 때만 부른다). retry 에는 `broadcastRoundChange(r)` 을 부른다 (번호로 가리킨 timer 이면 코어가 그 timer 를 멈추지 않았을 때만 부른다). `future_timeout` 에는 코어가 멈추지 않은 미래 PRE-PREPARE timer 의 함수를 부른다. NewHead 에는 `handleFinalCommittedMsg`, Start 에는 `startNewRound(0)`, `stop` 단계에는 `stopTimer`, `start` 단계에는 `core.New` 뒤에 `startNewRound(0)` 을 부른다 (backend 는 시작할 때마다 새 코어를 만든다, `backend.go:355-367`). event loop 는 돌리지 않는다. 애플리케이션은 `VGApp` 이며 그 답은 vector 입력에서 나온다. network runner 에서는 `VGApp` 의 `Broadcast` 와 `Gossip` 이 실제 `backend.Backend`(`HandleMsg`, known cache 와 recent cache, 가짜 peer 집합)로 간다.

overlay 는 파일 네 개를 더하고, reference 파일 일곱 개를 한 줄씩(`preprepare.go` 와 `backend.go` 는 두 줄씩) 바꾼 복사본으로 바꾼다. 바꿀 줄이 정확히 한 번 나오지 않으면 생성기는 실행을 거부한다. 바꾼 줄은 hook 을 부르며, 생성기가 기록기를 설치하지 않았으면 hook 은 원래 줄과 똑같이 동작한다.

| 파일 | 원래 줄 | 바꾼 줄 | 기록하는 것 |
|---|---|---|---|
| `consensus/wbft/core/core.go` | `c.roundChangeTimer = time.AfterFunc(timeout, func() {` | `vgRoundTimer(c, seq, round, timeout, func() {` | (seq, round) 로 시작한 round timer 와 그 취소 flag. 만료되지 않는다 |
| `consensus/wbft/core/core.go` | `c.retrySendingRoundChangeTimer = time.AfterFunc(timeout, func() {` | `vgRetryTimer(round, timeout, func() {` | round 로 시작한 retry timer. 만료되지 않는다 |
| `consensus/wbft/core/preprepare.go` | `c.futurePreprepareTimer = time.AfterFunc(duration, func() {` | `vgFutureTimer(preprepare, duration, func() {` | PRE-PREPARE 의 view 로 시작한 미래 PRE-PREPARE timer. 스스로 만료되지 않는다 |
| `consensus/wbft/core/preprepare.go` | `c.sendEvent(backlogEvent{` (그 timer 의 함수 안) | `vgPost(c, backlogEvent{` | `future_timeout` 단계가 그 timer 의 함수를 부를 때 예약되는 backlog replay |
| `consensus/wbft/core/backlog.go` | `go c.sendEvent(event)` | `vgSchedule(c, event)` | 예약된 backlog replay (`backlog` 단계가 처리한다) |
| `consensus/wbft/core/request.go` | `go c.sendEvent(wbft.RequestEvent{` | `vgSchedule(c, wbft.RequestEvent{` | 예약된 request replay (`request` 단계가 처리한다) |
| `consensus/wbft/backend/handler.go` | `go sb.istanbulEventMux.Post(wbft.MessageEvent{` | `vgPostReceived(sb, wbft.MessageEvent{` | `HandleMsg` 가 코어에 넘긴 메시지 |
| `consensus/wbft/backend/backend.go` | `go sb.istanbulEventMux.Post(msg)` | `vgPostSelf(sb, msg)` | broadcast 의 self-delivery (`message` 단계가 처리한다) |
| `consensus/wbft/backend/backend.go` | `go p.SendWBFTConsensus(outboundCode, payload)` | `vgSend(p, outboundCode, payload)` | peer 로의 전송 (`to` 와 `relay_to` 필드) |
| `consensus/wbft/backend/engine.go` | `waitDuration = time.Until(time.Unix(int64(sb.timeForNextWork()), 0))` | `waitDuration = vgUntil(time.Unix(int64(sb.timeForNextWork()), 0))` | `NotifyNewRound` 의 시계가 `build_wait` case 의 `now` 가 된다 |

이 수정은 timer 시작, post, 전송을 goroutine 밖으로 옮기거나 시계 읽기를 바꿀 뿐이며, 합의 로직은 건드리지 않는다. `VGApp` 은 `future_proposals` 의 proposal 에 대해, 그 proposal 의 timer 에 대한 `future_timeout` 단계 전까지 `FUTURE` 로 답한다 (기간은 명목상 1 초이며 vector 에는 적지 않는다).

`network/receive_outcome` 는 `HandleMsg` 에 없는 `eth` handler 규칙 두 가지를 적용한다. 하나는 handler 오류에 대한 연결 끊기 규칙(`eth/handler_istanbul.go:118-127`)이고, 다른 하나는 `HandleMsg` 전에 검사하는 크기 상한(`:138-140`, `eth/handler.go:59` 의 `protocolMaxMsgSize`)이다. frame 의 dedup key 는 `Backend.decode` 가 돌려주는 hash 다 (`0x11` 은 payload 의 첫 RLP item 이며, 뒤의 바이트는 무시한다).

`timers/build_wait` 는 주어진 시각의 head 를 돌려주는 `currentBlock`, 기간을 기록하는 `notifyNewRound`, `now` 로 맞춘 시계 hook 을 두고 `Backend.NotifyNewRound` 를 부른다.

정규화. reference 는 명세가 순서를 정하지 않은 곳에서 Go map 을 순회한다(`processBacklog` 의 backlog source, prepared certificate 와 seal 목록과 PRE-PREPARE justification 에 쓰는 `wbftMsgSet.Values()`). 기록은 이 집합들을 정렬한다 (`A-11` §3.1 "Steps"). 원소가 둘 이상인 목록을 가진 메시지는 `encoded` 를 `null` 로 두고, 그런 메시지의 self-delivery 는 목록을 정렬한 encoding(`VGCanonical`)을 싣는다. ECDSA(libsecp256k1, RFC 6979)와 BLS signature 는 결정적이다. 생성기를 세 번 돌린 결과가 같았다.

## 유지 관리

reference commit 이 바뀌면 (`A-12` §4) `internal/vecfile/case.go` 의 `ReferenceCommit` 과 `replace` 경로를 고치고, 두 단계의 vector 를 다시 만든 뒤 기댓값의 차이를 검토한다. case 를 더하거나 바꾸면 그 단계의 `generatorVersion` (`main.go`, `stage2/main.go`, `stage3/main.go`) 을 올린다. reference commit 이 바뀌어 3단계의 한 줄 수정이 더는 맞지 않으면 3단계는 실행을 거부한다. 그때는 바꿀 줄을 새 코드와 대조한 뒤 고친다.

## 다음 단계

- 2단계와 3단계는 끝났다 (위 참조). 묶음 검증(`verify_headers`), 미래 proposal, timer 단계, 크기 상한도 만들었다. runner `execution`, `source`, `governance` 는 state 를 입력으로 받아 2단계가 만든다 (`A-11` §3.1 "실행 state", "거버넌스 시나리오"). 남은 것은 steps case 안의 validator 집합 변경, 이제 거버넌스 단계 뒤에 reader 를 두어 쓸 수 있는 `source/candidates_at_epoch` 의 동작 vector V-SRC-002 … 006, 009, 011 다.
- 3단계에서 만들지 않은 것: 받아들여지는 정확히 10 MiB 인 frame (그 크기의 유효한 PRE-PREPARE 와 21 MB 파일 하나가 더 필요하다. 상한을 넘는 case 는 만들었다), 그리고 미래 PRE-PREPARE timer 의 기간 (`A-06` WBFT-TIMER-030 은 그 기간을 `validate_proposal` 안에서 header 시각과 시계로 계산한다. 단계에 시계가 없으므로 WBFT-TIMER-030 에는 vector 가 없다).
