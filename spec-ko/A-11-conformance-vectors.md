# A-11. Conformance 와 test vector

이 장은 누가 어떤 requirement 를 지켜야 하는지(conformance class), 언어에 독립적인 test vector 로 conformance 를 어떻게 시험하는지, 그리고 vector 를 어떻게 만들고 버전을 관리하는지를 정한다. 이 장은 WBFT inspector 와 두 번째 구현(Rust port)의 출발점이다.

---

## 1. Conformance class

class 는 구현이 만족한다고 주장하는 requirement 의 집합이다. 구현은 둘 이상의 class 를 주장해도 된다.

| Class | 코드 | Requirement | 참고 |
|---|---|---|---|
| 합의 참여자 | `CP` | Part A 와 Part B 의 모든 requirement | 합의 참여자는 validator 다. 합의 참여자는 합의 메시지를 보내고 받으며, block 을 만들고 seal 한다 |
| 검증 노드 | `VN` | Part A 에서 `A-05`, `A-06`, `A-07` 의 송신 조건(주어가 "노드는 반드시 send / broadcast / relay 해야 한다" 인 requirement)을 뺀 것, 그리고 Part B 전부 | 검증 노드는 block 을 import 하고 완전히 검증하지만 seal 하지 않는 full node 다. 검증 노드가 `istanbul/100` 을 듣는다면 합의 메시지를 디코드하기는 한다 |
| Light verifier | `LV` | `A-01`, `A-02`, `A-03`, `A-04` 의 validator 집합과 epoch 에 관한 절(다음 epoch 계산은 제외), `A-08` 의 light verification (`verify_light`) | light verifier 는 state 없이 header chain 의 finality 를 검사한다. light verifier 는 실행 state 가 필요한 requirement(epoch info 재계산, gas tip, blacklist)를 검사할 수 없으며, `A-08` 이 그런 requirement 를 나열한다 |
| Observer | `OB` | 디코딩(`A-02`, `A-03`)과 `Observable:` 태그가 붙은 모든 requirement 의 검사 | observer 는 inspector 다. observer 는 자기가 노드처럼 동작한다고 주장하지 않는다. observer 는 다른 노드가 관측 가능한 requirement 를 어긴 것을 검출한다고 주장한다 |

> 해설: observer class 는 성격이 다르다. observer 는 노드를 구현하지 않고도 conformance 를 주장할 수 있다. `A-00` §3 의 요약표는 light verifier 가 `A-01`–`A-04` 를 구현한다고 적지만, 이 장은 `A-04` 에서 다음 epoch 계산을 뺀다. class 를 정의하는 장은 이 장이므로 이 장의 정의를 따른다.

[WBFT-VEC-001] 어떤 class 를 주장하는 구현은 반드시 그 class 의 규범 requirement 를 모두 만족해야 한다. 다만 `(withdrawn)` 으로 표시된 requirement 는 예외다.
Source: this chapter (definition)

---

## 2. 관측 가능한 requirement

requirement 에는 바깥에서 어떤 데이터 원천으로 그 requirement 를 검사할 수 있는지 알려 주는 `Observable:` 태그가 붙는다:

| 태그 | 데이터 원천 | 전형적인 검사 |
|---|---|---|
| `header` | RPC 나 p2p 로 얻은 block header | extra 디코딩, seal, 이전 seal, randao, epoch info 존재, 시각 규칙 |
| `state` | 어떤 block 에서의 account/contract state, receipt, log | gas tip 값, base fee 분배, candidate 목록, governance 결과 |
| `network` | 링크에서 잡은 `istanbul/100` frame | 메시지 인코딩, signature, 송신 조건, relay |
| `log` | 노드 로그 | wire 흔적이 없는 내부 event 의 타이밍 (timer 만료, round 시작) |
| `rpc` | 노드 RPC 응답 | `istanbul_*` 결과, `finalized`/`safe` 태그 |

`Observable:` 줄에는 위 태그를 쉼표로 구분한 목록만 둔다. 정확히 무엇을 관측하는지(로그 메시지, RPC method)는 그다음 줄의 `Observed as:` 에 적는다.

[WBFT-VEC-003] `Observable: <tag>` 태그가 붙은 requirement 는 반드시 그 태그의 데이터로 판정할 수 있어야 한다. 이때 설정처럼 결정적인, 번호가 더 작은 장의 데이터를 함께 써도 된다. requirement 가 참조 commit 에서 바깥 observer 가 얻을 수 없는 state 를 필요로 하면 (예: archive 가 아닌 노드에서 오래된 block 의 contract storage), requirement 본문이 반드시 그렇다고 밝혀야 하고, 대신 vector 카탈로그가 반드시 그 requirement 의 vector 를 제공해야 한다.
Source: this chapter (editorial rule)

---

## 3. Test vector

### 3.1 배치

vector 는 Lighthouse 의 `ef_tests` 가 돌리는 Ethereum consensus-spec 테스트의 배치를 따른다:

```
vectors/
  <runner>/
    <handler>/
      <case>/
        meta.yaml        # requirement IDs covered, reference commit, generator, description
        input.yaml       # inputs
        expected.yaml    # expected output; absent => the operation MUST fail
```

case 하나는 위 세 파일을 가진 디렉터리다. 시험 대상 연산이 실패해야 하는 case 에는 `expected.yaml` 이 없다. case 에는 다른 파일이 들어가지 않는다. header 와 block 은 따로 파일로 두지 않고 `input.yaml` 안에 RLP 인코딩으로 싣는다 (WBFT-VEC-014).

[WBFT-VEC-010] test runner 는 반드시 `expected` 파일이 없는 case 를, 시험 대상 연산이 반드시 실패해야 하는 case 로 다뤄야 한다. 여기서 실패는 거부, 오류, 또는 panic 에 해당하는 결과를 말한다. runner 는 그런 case 가 성공하면 반드시 실패로 보고해야 한다.
Source: this chapter (convention adopted from Lighthouse `compare_result`)

> 해설: 이 규칙은 거부 규칙을 시험하는 case 를 따로 표시하지 않고 파일의 부재로 나타낸다. 단점은 `expected` 파일을 실수로 빠뜨려도 "실패해야 하는 case" 로 해석된다는 것이다. 그래서 WBFT-VEC-011 과 WBFT-VEC-012 가 함께 필요하다.

[WBFT-VEC-011] 모든 case 는 반드시 `meta.yaml` 에 다음 네 가지를 적어야 한다.

1. 그 case 가 다루는 requirement ID
2. 그 case 를 만든 참조 commit
3. 사용한 toolchain. 참조 구현의 toolchain 은 `go1.23.12` 다.
4. 생성 프로그램과 그 버전
Source: this chapter (convention)

[WBFT-VEC-012] runner 는 자기가 연 vector 파일을 기록하는 것이 좋고 (SHOULD), `vectors/` 아래에 열지 않은 파일이 있으면 실패하는 것이 좋다. 그래야 새로 넣은 vector 가 조용히 건너뛰어지지 않는다.
Source: this chapter (convention adopted from Lighthouse `check_all_files_accessed.py`)

[WBFT-VEC-013] 모든 vector 파일은 반드시 JSON 으로 일대일 변환되는 다음 YAML 부분집합을 써야 한다. 그 부분집합은 공백 두 칸씩 들여 쓴 block 형식의 mapping 과 sequence, `a-z`, `0-9`, `_` 로 된 key, JSON escaping 을 쓰는 큰따옴표 문자열, `true`, `false`, `null`, 빈 collection `[]` 와 `{}` 로 이루어진다. 다른 YAML 구성 요소는 쓸 수 없다. 주석, 따옴표 없는 scalar, 작은따옴표 scalar, 숫자, anchor, alias, tag, 비어 있지 않은 flow collection, 여러 줄 scalar, 한 파일 안의 여러 document, 중복 key 가 모두 금지된다. 부분집합을 벗어난 파일은 유효한 vector 가 아니며, runner 는 반드시 그런 파일을 해석하지 말고 거부해야 한다. `tools/vectorgen/check_yaml_subset.py` 가 이 검사를 한다.
Source: this chapter (vector format)

[WBFT-VEC-014] vector 파일에서 모든 정수는 반드시 10진 문자열로 써야 한다. `code`, `round`, `seal_type` 처럼 작은 정수도 같다. address 를 포함한 모든 바이트 문자열은 반드시 `0x` 를 붙인 소문자 16진수로 써야 한다. address 에는 checksum 대소문자를 쓰지 않고, 빈 바이트 문자열은 `"0x"` 다. 없는 선택 값(seal, `epoch_info`, prepared round, prepared block)은 반드시 `null` 이어야 한다. header 와 block 은 반드시 RLP 인코딩으로 넘겨야 한다.
Source: this chapter (vector format)

[WBFT-VEC-015] `meta.yaml` 은 반드시 다음 필드를 가져야 한다. `runner`, `handler`, `case`, `kind`, `description`, `requirements`(requirement ID 목록), `reference`(`implementation`, 전체 hash 인 `commit`, `toolchain`, `build`. 참조 구현의 `build` 는 `cgo` 다), `generator`(`name`, `version`). 이 필드들이 WBFT-VEC-011 을 채운다. 실패 case 는 참조 구현의 오류 문구인 `expected_error` 를 더 가져도 된다. 오류 문구는 규범이 아니므로 이 필드는 informative 이고, runner 는 이 필드를 비교해서는 안 된다.
Source: this chapter (vector format)

[WBFT-VEC-016] runner, handler, case 디렉터리 이름은 반드시 `a-z`, `0-9`, `_` 로만 이루어져야 한다.
Source: this chapter (vector format)

Chain fixture. 이름이 `chain` 인 입력 필드는 chain fixture 를 담는다. chain fixture 는 handler 가 읽는 header 들과 그 header 들을 만들 때 쓴 설정을 데이터로 준 것이며, 다음 세 필드와 선택 필드 하나를 가진 mapping 이다.

1. `config`: chain 설정이다. `preset` 은 `B-01` §11 의 network preset 하나를 가리키고(`"8282"`), 나머지 필드는 그 preset 의 일부를 바꾼다. `init` 은 `anzeon.init` 을 바꾸고(필드 `validators` 와 `bls_public_keys`), `wbft` 는 `anzeon.wbft` 를, `transitions` 는 `transitions` 를 바꾼다. `shanghai_time` 과 `cancun_time` 은 두 fork 시각을 바꾸며, `null` 이면 preset 의 값을 그대로 둔다. preset 에는 두 값이 없다. 선택 필드 `boho_block` 은 값을 정할 때만 적으며, preset 의 `BohoBlock`(Boho upgrade 의 block, `B-01` SNET-CFG-011)을 바꾼다. `wbft` 와 `transitions` 의 각 원소는 genesis 필드 이름을 snake case 로 쓴다. 그 이름은 `request_timeout_seconds`, `block_period_seconds`, `epoch_length`, `allowed_future_block_time`, `proposer_policy`, `max_request_timeout_seconds` 이고, transition 에는 `block` 이 더 있다. pointer 필드인 `proposer_policy` 와 `max_request_timeout_seconds` 는 값이 없으면 `null` 이다. 나머지 필드는 항상 적는다. 그 필드들은 0 과 값이 없는 것이 같은 뜻이기 때문이다. 설정을 받는 pure handler(`chain/config_at`, `validators/epoch_boundary`)는 chain 없이 `wbft` 와 `transitions` 필드만 가진 입력 `config` 를 받는다.
2. `genesis`: block 0 header 의 RLP 다. genesis header 는 직접 주어지며, `B-02` 가 설정으로 만드는 header 와 같지 않아도 된다.
3. `headers`: 나머지 저장된 header 들의 RLP 를 번호가 커지는 순서로 적은 목록이다. 이 목록의 header 는 모두 자기 번호의 canonical header 다. 목록은 번호가 이어지지 않아도 된다. header 가 없는 번호는 노드가 그 번호에 canonical header 를 저장하지 않았다는 뜻이며, case 는 이렇게 해서 없는 ancestor 를 표현한다.
4. `non_canonical`(선택 필드이며, 비어 있지 않을 때만 적는다): 저장되어 있지만 자기 번호의 canonical header 가 아닌 header 들의 RLP 다. handler 는 이 header 들을 hash 로만 찾고(부모 조회나 `A-04` §3.1 의 거슬러 올라가기), 번호로는 찾지 않는다. case 는 이렇게 해서 fork 를 표현한다. `A-08` WBFT-HDR-071 의 조회가 그 예다.

chain fixture 에는 state 가 없다. 부모 state 를 읽는 handler 는 그 state 를 얻을 수 없는 것으로 다루므로, header 검증은 단계 H15b 와 H21 을 건너뛴다 (`A-08` §6.6). state 의 값이 필요한 handler 는 그 값을 따로 입력으로 받는다 (`header/build_proposal_header` 의 `gas_tip`, `validators/next_epoch_info` 의 `candidates`). fixture 의 header 들은 header 검증을 통과한다. canonical 이 아닌 header 는 자기 branch 위에서 통과한다. 예외는 `validators/next_epoch_info` 다. 그 계산은 seal 을 검사하지 않으므로 그 fixture 는 자리만 채운 seal 을 싣고, case 설명이 그렇다고 밝힌다. 입력에 chain fixture 가 들어 있는 case 의 종류는 `chain` 이다.

실행 state. chain fixture 에는 state 가 없고, 다른 입력에도 없다. 계정 state 를 읽는 handler 는 읽는 계정을 입력 `accounts` 로 받는다. `accounts` 는 `address`, `balance`, `nonce`, `extra`(`B-04` SNET-SYS-071 의 flag word), `code`, `storage`(`key`, `value` 의 목록이며 각각 32 바이트)의 목록이다. state 에는 정확히 이 계정들만 있다. 목록의 계정은 비어 있어도 state 에 있고, 적지 않은 slot 은 0 이다. runner `execution`(`p256_verify` 는 빼고)과 runner `source` 가 이 입력을 쓴다. 이 handler 들의 `config` 는 preset 과 `applepie_block`, `boho_block` 을 바꾸는 `overrides` 이며(`null` 이면 preset 의 값을 그대로 둔다), `chain/fork_schedule` 의 모양에서 두 fork 시각을 뺀 것이다.

Steps. 종류가 `steps` 인 case 는 노드 하나의 합의 코어에 event 목록을 차례로 넣고, event 마다 노드가 한 일을 기록한다. 아래 형식과 WBFT-VEC-058 ~ WBFT-VEC-061 은 runner `state_machine` 과 `network` 의 steps handler(`state_machine/rounds`, `network/receive_outcome`)에만 적용한다. handler `governance/scenarios` 도 종류가 `steps` 이지만, 합의 코어 대신 거버넌스 연산의 순서를 실행하므로 자기 steps 형식을 따로 쓴다. 그 형식은 아래 "거버넌스 시나리오" 에 있다.

`input.yaml` 은 두 필드를 가진다.

1. `initial`: 노드와 그 애플리케이션이다.
   - `validators`: validator 집합이며, `address` 와 `bls_public_key` 의 목록을 집합 순서대로 적는다. case 의 모든 height 에서 집합이 같다. 그래서 case 는 epoch 변경을 넘지 않는다.
   - `proposer_policy`: `validators/proposer` 와 같은 policy 식별자다.
   - `node_key`: 노드의 secp256k1 key(32 바이트)다. 노드의 address, BLS key(`A-02` §5.1), 노드가 만드는 모든 signature 가 이 key 에서 나온다. ECDSA signature 는 결정적이고(RFC 6979, `A-02`) BLS signature 도 결정적이므로, 노드 자신의 메시지는 바이트 단위로 비교한다.
   - `head`: `app.head()` 가 돌려주는 head block 의 RLP 다 (`A-05` §1.3). 그 `Coinbase` 가 마지막 proposer 다.
   - `app`: 애플리케이션의 나머지 답이다. `invalid_proposals` 는 `app.validate_proposal` 이 실패하는 block hash 의 목록이다. `future_proposals` 는 `app.validate_proposal` 이 `FUTURE(d)` 로 답하는 block hash 의 목록이다. 그 proposal 을 위해 시작한 timer 의 `future_timeout` 단계를 처리하기 전까지는 `FUTURE(d)` 이고, 그 뒤로는 `VALID` 다. 기간 `d` 는 case 에 들어 있지 않다. 다른 proposal 은 모두 `VALID` 다. `bad_blocks` 는 `app.is_bad_block` 이 참인 block hash 의 목록이다. `finalize` 는 모든 `app.finalize` 호출의 결과이며 `"ok"` 또는 `"fail"` 이다.
   - network runner 에만 있는 필드: `engine`(`"running"` 또는 `"stopped"`), `synchronising`(bool), `peers`(연결된 peer 의 address 목록).

   노드는 `Start` 직후의 state 에서 시작한다 (WBFT-SM-004, WBFT-SM-008). view 는 `(head.number + 1, 0)` 이고 state 는 `AcceptRequest` 이며, lock 이 없고 backlog 가 비어 있다. 그 밖의 state(lock, 뒤의 round, 저장된 extra seal)는 참조 구현이 그 state 에 이르는 것과 같이 단계로 만든다.
2. `steps`: event 의 목록이다. 단계마다 `kind` 와 `label` 이 있다. `label` 은 informative 이며 runner 는 쓰지 않는다. kind 별 필드는 다음과 같다.
   - `message`(`code`, `payload`): `A-05` §4.1 의 event `Message(code, payload)` 다. peer 에게서 받은 메시지이거나, 노드가 broadcast 한 메시지의 self-delivery 다. network runner 에서는 self-delivery 만 이 kind 를 쓰며, self-delivery 는 dedup cache 를 거치지 않는다 (`A-07` WBFT-NET-030).
   - `backlog`(`code`, `payload`): 앞 단계가 예약한 replay 가운데 `rlp_encode(m)` 이 `payload` 인 메시지 `m` 의 event `Backlog(m)` 이다.
   - `request`(`block`): block RLP 를 담은 event `Request(p)` 다.
   - `round_timeout`(`timer` 는 선택): round timer 의 만료다. `timer` 가 없으면 마지막으로 시작한 round timer 의 만료이며, case 는 그 timer 를 취소하지 않은 상태에서만 이 단계를 준다. `timer` 가 있으면 그 번호(아래)의 round timer 의 만료다. 그 timer 가 이미 취소되었거나 새 timer 로 대체되었거나 engine 이 멈춰 있으면 이 단계는 아무 효과가 없다 (`A-06` WBFT-TIMER-014).
   - `retry_timeout`(`round`, `timer` 는 선택): `timer` 가 없으면 event `RetryTimeout(round)` 다. timer 가 취소되기 전에 만료되어 queue 에 들어가 있던 event 도 이것으로 나타낸다 (`A-05` WBFT-SM-081). `timer` 가 있으면 그 번호의 retry timer 의 만료이며, 그 timer 는 `round` 를 기억한다. 단계 전에 그 timer 가 취소되었거나 engine 이 멈춰 있으면 timer 는 만료되지 않고 이 단계는 아무 효과가 없다. 그렇지 않으면 이 단계는 event `RetryTimeout(round)` 다.
   - `future_timeout`(`timer`): 그 번호의 미래 PRE-PREPARE timer 의 만료다 (`A-06` §7). 이 단계부터 `app.validate_proposal` 은 그 timer 의 proposal 에 `VALID` 로 답한다. timer 가 취소되지 않았고 engine 이 돌고 있으면 timer 는 자기 PRE-PREPARE 의 `Backlog(m)` 을 예약하고, 기록의 `scheduled` 에 그것이 나온다. 그렇지 않으면 이 단계는 아무 효과가 없다.
   - `stop`: engine 이 멈춘다 (`A-05` Stop). 어떤 단계도 처리하지 않은 예약 event 는 사라진다. 다음 `start` 단계까지 case 는 `notify` 가 거짓인 `head` 단계와 `timer` 가 있는 timer 단계만 준다.
   - `start`: engine 이 애플리케이션의 현재 head 에서 다시 시작한다. case 처음과 같이 새 코어가 view `(head.number + 1, 0)` 에서 lock 없이, backlog 가 빈 채로 시작한다 (WBFT-SM-004).
   - `head`(`block`, `notify`): 애플리케이션의 head 가 `block` 이 된다. `notify` 가 참이면 그다음에 event `NewHead` 를 처리한다.
   - `frame`(`peer`, `code`, `payload`): network runner 에만 있다. 그 peer address 에게서 받은, 그 code 와 payload 의 `istanbul` 메시지다.

timer 는 종류(`round`, `retry`, `future`)마다 case 가 시작한 순서대로 0 부터 번호를 붙인다. 번호는 `timers` 기록이 적는 순서를 따르며, `start` 의 기록에서 시작해 `stop` 과 `start` 단계를 지나서도 이어진다. `timer` 가 있는 timer 단계는 이 번호로 timer 를 가리킨다.

예약된 event(`A-05` WBFT-SM-002 의 self-delivery, backlog replay, request replay)는 저절로 처리되지 않는다. case 가 처리하는 예약 event 는 모두 case 가 고른 자리에 놓인 단계 하나다. 어떤 단계도 처리하지 않는 예약 event 는 network 에서 잃어버린 메시지처럼 사라진다. 그래서 참조 구현이 동시에 도착한 event 를 처리하는 순서(`event.TypeMux` 위의 Go `select`)가 vector 에 남지 않는다.

`expected.yaml` 은 `Start` 의 기록인 `start`(engine 이 멈춘 case 에서는 `null`)와, 단계마다 기록 하나를 적은 `steps` 를 가진다. 기록의 필드는 다음과 같다.

1. `check`: `message`, `backlog`, `frame` 단계의 `check_message` 결과(`A-05` §5.2)다. 메시지가 그 전에 버려졌으면(알 수 없는 code, 디코드 실패, 잘못된 signature) 또는 다른 kind 의 단계이면 `null` 이다.
2. `relay`: 처리 결과가 `OK` 여서 메시지가 relay 되었는지를 적는다 (WBFT-SM-011, WBFT-SM-012). 메시지를 처리하지 않는 단계에서는 `null` 이다.
3. `sent`: 노드가 broadcast 한 메시지를 보낸 순서대로 적는다. 메시지마다 `encoding/message_codec` 의 필드로 적고, justification 목록은 원소 encoding 의 바이트 값 순서로 정렬한다. `encoded` 에는 바이트를 적는다. 다만 원소가 둘 이상인 목록이 있으면 그 순서가 정해져 있지 않으므로(WBFT-SM-035) `null` 로 둔다. 뒤의 단계가 그런 메시지를 self-delivery 할 때는 목록을 정렬한 encoding 을 `payload` 로 쓴다. 목록은 서명 대상이 아니므로(`A-03`) signature 는 그대로 유효하다.
4. `timers`: 그 단계에서 시작한 timer 를 순서대로 적는다. view 의 round timer 는 `{kind: "round", sequence, round}`, retry timer 는 `{kind: "retry", round}`, 미래 PRE-PREPARE timer 는 미뤄 둔 PRE-PREPARE 의 view 로 `{kind: "future", sequence, round}` 라고 적는다. 길이는 적지 않는다. 길이는 `timers/round_timeout` 과 `timers/build_wait` 가 다룬다. 취소도 적지 않는다. 취소는 `timer` 가 있는 timer 단계로 관측한다.
5. `new_round`: 그 단계에서 `app.notify_new_round` 에 넘긴 round 들이다.
6. `finalized`: `null` 이거나 `app.finalize` 호출이다. 필드는 `proposal`(block hash), `round`, `prepared_seals` 와 `committed_seals`(`sealer`, `seal` 의 목록이며 sealer 순서로 정렬한다), `result` 다.
7. `scheduled`: 그 단계가 예약한 event 다. `{kind: "backlog", code, payload}` 를 source address 순서로(한 source 안에서는 꺼내는 순서로) 적고, 그 뒤에 `{kind: "request", block}` 을 적는다.
8. `state`: 단계가 끝난 뒤 `A-05` §3.1 의 state 변수다. 필드는 `view`, `state`, `proposer`, `locked_round`, `locked_block`, `preprepare`(`round`, `proposal`), `pending_request`, `preprepare_sent`, `prepares` 와 `commits`(source address), `certificate`(`null` 이거나 원소마다 `source`, `sequence`, `round`, `digest`), `round_changes`(round key 마다, 메시지가 없는 key 도 포함해서 `round`, `sources`, `prepared_round`, `prepared_block`), `backlog`(대기 중인 메시지마다 `source`, `code`, `sequence`, `round`), `prior`(`round`, `proposal`), `extra_prepare_seals` 와 `extra_commit_seals`(`source`, `sequence`, `round`)다. block 은 block hash 로 적고, 집합은 모두 정렬한다.

engine 이 멈춰 있는 동안(`stop` 단계의 기록과, 다음 `start` 단계 전까지의 모든 단계의 기록) `state` 는 `null` 이고 나머지 필드는 `null` 이거나 비어 있다.

network 기록은 앞에 세 필드를 더 가진다. `outcome` 은 `frame` 단계의 `A-07` §8 부류(`ACCEPT`, `IGNORE`, `DROP_SILENT`, `DISCONNECT`)이며 다른 단계에서는 `null` 이다. `dedup_key` 는 engine 이 돌 때 code 가 `0x11` 부터 `0x15` 이고 payload 가 비어 있지 않으며 크기 상한(`A-07` WBFT-NET-013)을 넘지 않는 frame 의 key 다 (`A-07` WBFT-NET-022). `0x11` 의 payload 는 먼저 풀어낸다. `relay_to` 는 relay 를 보낸 peer 의 address 를 정렬한 목록이며, relay 한 것이 없으면 `null` 이다. `sent` 의 원소마다 필드 `to` 가 더 있으며, 그 메시지를 보낸 peer 의 address 를 정렬해 적는다. engine 이 멈춘 case 의 기록은 이 세 network 필드만 가진다.

거버넌스 시나리오. `governance/scenarios` 의 case 는 genesis 의 거버넌스 contract 에 호출을 차례로 보내고, 호출마다 일어난 일을 기록한다 (`B-05` §13 "시나리오 test vector"). `input.yaml` 은 네 필드를 가진다.

1. `genesis`: `preset`(`"8282"`), 두 `GovBase` 인스턴스의 파라미터인 `gov_validator` 와 `gov_council`(`B-05` §12 의 `members`, `quorum`, `expiry`, `max_proposals`. `gov_validator` 에는 `validators`, `bls_public_keys`, `gas_tip` 이 더 있다. member version 은 1 이다), 잔액을 준 계정인 `alloc`(`address`, `balance`)이다. genesis 는 이 파라미터를 넣은 preset 의 genesis 이며, `anzeon.init` 은 `gov_validator` 의 validator 와 key 로 정하고(`B-02`), 나머지 system contract 는 preset 의 것이다.
2. `genesis_time`: genesis block 의 timestamp 다.
3. `named_accounts`: 투영이 flag 를 보고하는 계정들이다.
4. `steps`: 호출의 목록이다. 호출마다 `label`(informative), `sender`, `to`, `data`(call data), `gas`, `time`(block timestamp), `number`(block 번호)가 있다. 단계마다 그 호출이 자기 block 의 유일한 트랜잭션이며, 앞 단계가 남긴 state(첫 단계는 genesis state)에 적용된다. 호출은 `sender` 가 보낸 value 0 의 메시지이며, gas 한도는 `gas`, nonce 는 `sender` 의 nonce, gas price 는 base fee 20 000 gwei 와 단계 전 GovValidator gas tip 인 block 에서의 `B-07` §3 값이고, coinbase 는 첫 validator 다. 서명은 쓰지 않는다. 하위 호출(proof-of-possession precompile, `AccountManager`)은 호출 안에서 실행되며, 그 결과는 입력이 아니다.

`expected.yaml` 은 단계마다 기록 하나를 적은 `steps` 를 가진다.

1. `status`: `success`, `revert`(호출이 revert 됨), `failure`(그 밖의 예외 정지) 가운데 하나다.
2. `revert_data`: revert 된 호출의 revert 데이터(`B-05` 의 custom error)이며, 그 밖에는 `"0x"` 다.
3. `logs`: 호출의 log 를 순서대로 적으며, 각각 `address`, `topics`, `data` 다.
4. `write_set`: 호출이 값을 바꾼 storage slot 과 새 값(`address`, `key`, `value`)을 address 와 key 순서로 적는다. 건드린 인스턴스의 `GovBase` state 와 `B-05` §8, §10 의 파생 state 가 어떻게 바뀌었는지를 `B-04` 와 `B-05` §2 의 storage 배치로 준다.
5. `projection`: 단계 뒤의 합의 투영이다. `validators`(목록 순서), `bls_public_keys`(같은 순서로 validator 마다의 key), `gas_tip`, 그리고 `named_accounts` 로 좁힌 `blacklisted` 와 `authorized`(정렬)다.

잔액과 gas 는 적지 않는다. 그것은 runner `execution` 이 다룬다.

> 해설: 부분집합을 좁게 잡은 이유는 YAML 파서마다 해석이 다른 구성 요소(따옴표 없는 `0x10` 이나 `no`, 숫자의 정밀도)를 없애기 위해서다. 이 부분집합이면 어느 언어의 YAML 파서로 읽어도, 아주 작은 변환기로 JSON 으로 바꿔도 같은 객체가 나온다. 단점은 사람이 vector 를 손으로 고치기 불편하다는 것이다. 그래서 vector 는 생성기로만 만들고, 검사 스크립트가 손으로 고친 파일을 걸러 낸다.

### 3.2 Vector 의 출처

[WBFT-VEC-020] 기댓값은 반드시 참조 구현을 실행해서 만들어야 한다. 여기서 참조 구현은 기록된 commit 의 go-stablenet 을 기록된 toolchain 으로 빌드한 것이다. 두 번째 구현이 계산한 값을 기댓값으로 써서는 안 된다. 시험 대상 구현이 만든 vector 로는 그 구현을 시험할 수 없기 때문이다. 다만 참조 구현이 표현할 수 없는 case 는 예외이며, 그 경우 case 가 `meta.yaml` 에 그렇다고 밝힌다.
Source: this chapter; lesson from tendermint-rs

[WBFT-VEC-021] vector 에서 쓰는 키는 반드시 문서화된 seed 에서 결정적으로 끌어내야 한다. 그래야 어떤 case 든 다시 만들 수 있다. 이 명세의 vector 는 `A-02` §8.1 의 seed `key_i = keccak256(ASCII("wbft-spec-vector-key-" ‖ decimal(i)))` 를 쓴다. 그래서 vector 가 `A-02` 와 `A-03` 의 계산 예를 그대로 재현한다.
Source: this chapter (convention adopted from tendermint-rs `testgen`)

[WBFT-VEC-022] ECDSA signer 복원의 기댓값은 반드시 참조 구현의 cgo 빌드를 따라야 한다 (`A-02` §3.3). no-cgo 빌드는 받아들이지만 cgo 빌드는 거부하는 signature 의 case(예: recovery byte `V = 4`)는 실패 case 이고, 그 case 의 설명이 그렇다고 밝힌다.
Source: `A-02` §3.3

`tools/` 의 초안 도구(`vectors-a01-a03`, `epoch-a04`, `genesis-b02`, `slots-b04`, `logcat`)는 이 명세의 계산 예를 계산했으며, 생성기의 출발점이다. 생성기는 `tools/vectorgen` 이다 (그 디렉터리의 `README.md` 참조). 생성기는 이 장 옆의 `vectors/` 에 vector 를 쓴다.

### 3.3 카탈로그

아래 표는 runner 와 handler 를 나열한다. "종류" 는 case 의 형태를 말한다. `pure` 는 입력만의 함수다. `steps` 는 어떤 state 에 차례로 적용하는 event 의 순서이며, Ethereum fork-choice 테스트처럼 초기 state 다음에 단계와 단계마다의 기록이 온다. `state_machine` 과 `network` handler 의 형식은 §3.1 "Steps" 에 있고, `governance/scenarios` 는 자기 형식을 따로 쓴다. `chain` 은 chain fixture 이며, genesis 와 block 목록으로 이루어진다.

| Runner | Handler | 종류 | 장 / requirement | 참고 |
|---|---|---|---|---|
| `crypto` | `keccak256`, `ecdsa_sign`, `ecdsa_recover` | pure | A-02 | high-S 쌍둥이와 `V` 경계값을 포함한다. cgo 빌드와 no-cgo 빌드의 수락 결과가 다르다 (A-02) |
| `crypto` | `bls_derive`, `bls_sign`, `bls_verify`, `bls_aggregate`, `aggregate_public_keys` | pure | A-02 | infinity 입력과 subgroup 밖의 입력을 실패 case 로 둔다. WBFT-CRYPTO-056 의 비정규 압축 encoding(flag 변형, `p` 보다 작지 않은 좌표 성분)도 모두 실패 case 로 두고, sign flag 를 뒤집은 입력은 decode 되는 case 로 둔다. 합이 무한원점인 public key 들도 둔다. 그 aggregate signature `c0 ‖ 95 × 00` 은 반드시 `bls_verify` 에서 실패해야 한다 (WBFT-CRYPTO-055, `A-08` WBFT-HDR-100). `A-02` §8.3 이 이 case 들을 적는다 |
| `crypto` | `seal_data`, `randao_data`, `randao_mix` | pure | A-02 | |
| `encoding` | `extra_codec` | pure | A-03 `ENC` | round trip 과 모든 거부 규칙 (`0x80` 과 `0xc0`, 짧은/긴 필드, 뒤에 남는 바이트) |
| `encoding` | `block_hash`, `hash_with_round`, `filtered_header` | pure | A-03 | extra 를 디코드할 수 없을 때의 상수 case 를 포함한다 |
| `encoding` | `message_codec`, `signing_payload`, `dedup_key` | pure | A-03 `MSG`, A-07 | 메시지 네 종류, ROUND-CHANGE 의 "absent" 변형, 다시 인코딩했을 때의 동일성 |
| `validators` | `quorum` | pure | A-04 | `N = 0..22`, float64 동작. binary64 반올림이 결과를 바꾸는 크기(`A-10` §2)는 집합을 만들기에 너무 커서 case 가 없다 |
| `validators` | `proposer` | pure | A-04 `PROP` | 두 policy, 마지막 proposer 가 0 인 경우, index 를 찾지 못하는 경우 |
| `validators` | `epoch_boundary` | pure | A-04 `EPOCH` | transition, 한 block 짜리 epoch (epoch 경계 계산 자체는 실패하지 않는다. 기본 `epochLength` ≤ 1 은 설정 단계에서 거부되므로 한 block 짜리 epoch 은 transition 으로만 생긴다). |
| `validators` | `validators_at` | chain | A-04 §3 | height 0 은 설정에서, 그 뒤 height 는 그 height 를 다스리는 epoch header 에서 얻는다. policy 는 `config_at` 에서 얻는다. epoch header 가 없는 경우와 `EpochInfo` 가 없는 epoch header 는 실패 case 로 둔다 |
| `validators` | `shuffle` | pure | A-04 | 33 round, keccak256 |
| `validators` | `sort_candidates` | pure | A-04 | diligence 내림차순 순서. |
| `validators` | `next_epoch_info` | chain | A-04 | 입력은 세 가지다. 첫째는 epoch block 의 부모에서 끝나는 chain fixture 이며, epoch header 이력과 이전 epoch info 를 담는다. 둘째는 `EpochInfo` 가 없는 epoch header 이며, 그 mix digest 가 seed 다. 셋째는 BLS key 를 함께 적은 candidate 목록이다 (`A-09` `candidates`) |
| `timers` | `round_timeout` | pure | A-06 | round 0 에서는 상한을 적용하지 않는다. overflow 와 clamp case |
| `timers` | `build_wait` | pure | A-06 WBFT-TIMER-040, WBFT-TIMER-041 | block builder 가 시작하기 전의 대기다. round 0 에서는 head 시각, block 주기, 시계로 정하며, 시계가 head 보다 늦은 경우와 대기가 음수인 경우를 넣는다. round 1 부터는 대기가 없다 |
| `state_machine` | `check_message` | pure | A-05 | 모든 state × code × view 관계 |
| `state_machine` | `is_justified` | pure | A-05 | dedup, 오래된 view, prepared round 규칙 |
| `state_machine` | `rounds` | steps | A-05, A-06 | 정상 높이, prepared block 이 있을 때와 없을 때의 round change, F+1 skip, 늦은 timeout 의 catch-up, extra seal, backlog replay 순서. 미래 proposal (미루기, 대체, round timer 에 의한 취소), 취소된 timer 와 살아 있는 timer 의 만료, engine 정지와 다음 height 에서의 재시작 |
| `network` | `receive_outcome` | steps | A-07 | 크기 상한을 포함한 조건별 DROP-SILENT / IGNORE / ACCEPT / DISCONNECT, dedup 선점 |
| `header` | `build_proposal_header` | chain | A-08 | 이전 seal, extra seal 병합, vanity 규칙 |
| `header` | `verify_header` | chain | A-08, B-03 | 거부 단계 H1–H21 마다 case 하나와 기대 판정 (WBFT-VEC-031). sealer 들의 key 합이 무한원점인 aggregated seal 도 둔다 (`ErrInvalidSeal`, WBFT-HDR-100). fixture 에는 state 가 없으므로 H15b 와 H21 은 건너뛴다. H1(`Number` 없음)은 RLP 로 쓸 수 없다. H9 에 이르는 header 는 RLP 에서 `WithdrawalsHash` 를 가져야 하므로 먼저 H7 에서 거부된다. 그래서 이 두 단계에는 따로 case 가 없다 |
| `header` | `verify_headers` | chain | A-08 §6.7 | 묶음 검증. 입력 순서, 묶음 안의 앞선 header 를 부모로 씀 (epoch header 포함), 첫 실패나 지연 뒤의 모든 header 에 `ErrUnknownAncestor` |
| `header` | `verify_light` | chain | A-08 | `VALID`, `INVALID`, `CANNOT_DECIDE` |
| `chain` | `config_at`, `fork_schedule` | pure | A-01, B-01 | preset 8282 와 8283. block 이 같은 transition 들을 넣는다. |
| `chain` | `genesis` | pure | B-02 | preset genesis hash, extra 바이트, state root |
| `execution` | `gas_tip_enforcement`, `fee_delegation` | pure | B-07 | block 의 트랜잭션 전 state 를 입력으로 받는 B-07 의 계산 예 E-1 … E-9. authorized 가 아닌 sender 와 authorized sender 의 gas tip 대체, legacy 트랜잭션, fee cap 경계, fee payer 의 잔액과 서명 규칙, Applepie 조건, 환불, `AuthorizedTxExecuted` 와 `Transfer` log |
| `execution` | `p256_verify` | pure | B-07 SNET-TX-089 | 레퍼런스 파일 `core/vm/testdata/precompiles/p256Verify.json` 의 782 개 case (EIP-7951. 출력이 `0x…01` 인 것 567 개, 빈 출력 215 개, 모든 case 의 gas 6 900). 레퍼런스 commit 에서 `TestPrecompiledP256Verify` 가 이 case 를 모두 통과한다. 여기에 160 바이트가 아닌 입력들 (빈 출력, gas 6 900) 과 `BohoBlock` 이전의 `0x…0100` 호출 (precompile 없음, gas 없음) 을 더한다 |
| `execution` | `process_finalize` | chain | B-06 | `BohoBlock` 의 upgrade 적용, 나머지가 있을 때와 없을 때의 base fee 분배(diligence 가 0 인 경우와 blacklist 된 validator 포함), 부모 state 에 대한 gas tip 검사, epoch block 이 아닌 block 의 `EpochInfo`, epoch 쓰기(proposer 경로)와 검증(import 경로) |
| `governance` | `scenarios` | steps | B-05 §13 | 시나리오 V-01 … V-10, V-12 … V-24, V-26 (연산 순서와 log, 쓰기 집합, 합의 투영). 자기 steps 형식을 쓴다 (§3.1 "거버넌스 시나리오"). |
| `source` | `candidates_at_epoch` | pure | B-08 | genesis 초기화 함수가 쓴 GovValidator storage 위의 `B-04` §8.2 reader. V-SRC-001, V-SRC-007, V-SRC-008, V-SRC-012 의 후보 부분, 12 개보다 많은 후보, 검사하지 않는 key. 동작 vector V-SRC-002 … 006, 009, 011 은 거버넌스 트랜잭션이 필요해 아직 만들지 않았다. |

지금까지 만든 vector 는 아래와 같다 (2026-09-28, 참조 `740526d03`. 1단계는 `tools/vectorgen` 0.1.1, 2단계는 `tools/vectorgen/stage2` 0.3.0, 3단계는 `tools/vectorgen/stage3` 0.3.1 이 만들었다). 여기에 없는 handler 는 아직 vector 가 없다. 거부를 기대하는 `header/verify_header`, `header/verify_headers`, `header/verify_light` case 는 실패 case 가 아니다. 그 case 의 `expected.yaml` 에 판정이 들어 있다.

| Runner | Handler | Case 수 | 그중 실패 case |
|---|---|---|---|
| `crypto` | `keccak256` | 9 | 0 |
| `crypto` | `ecdsa_sign` | 11 | 0 |
| `crypto` | `ecdsa_recover` | 19 | 14 |
| `crypto` | `bls_derive` | 10 | 0 |
| `crypto` | `bls_sign` | 5 | 0 |
| `crypto` | `bls_verify` | 13 | 4 |
| `crypto` | `bls_aggregate` | 21 | 12 |
| `crypto` | `aggregate_public_keys` | 20 | 14 |
| `crypto` | `seal_data` | 10 | 0 |
| `crypto` | `randao_data` | 9 | 0 |
| `crypto` | `randao_mix` | 5 | 0 |
| `encoding` | `extra_codec` | 41 | 28 |
| `encoding` | `block_hash` | 9 | 0 |
| `encoding` | `hash_with_round` | 27 | 0 |
| `encoding` | `filtered_header` | 18 | 6 |
| `encoding` | `message_codec` | 34 | 19 |
| `encoding` | `signing_payload` | 12 | 2 |
| `encoding` | `dedup_key` | 9 | 0 |
| `validators` | `quorum` | 32 | 0 |
| `validators` | `proposer` | 52 | 0 |
| `validators` | `epoch_boundary` | 58 | 0 |
| `validators` | `validators_at` | 11 | 2 |
| `validators` | `shuffle` | 14 | 3 |
| `validators` | `sort_candidates` | 30 | 0 |
| `validators` | `next_epoch_info` | 14 | 3 |
| `timers` | `round_timeout` | 77 | 0 |
| `timers` | `build_wait` | 7 | 0 |
| `chain` | `config_at` | 34 | 0 |
| `chain` | `fork_schedule` | 16 | 0 |
| `chain` | `genesis` | 10 | 1 |
| `state_machine` | `check_message` | 336 | 0 |
| `state_machine` | `is_justified` | 27 | 0 |
| `state_machine` | `rounds` | 26 | 0 |
| `network` | `receive_outcome` | 8 | 0 |
| `header` | `build_proposal_header` | 16 | 4 |
| `header` | `verify_header` | 47 | 0 |
| `header` | `verify_headers` | 9 | 0 |
| `header` | `verify_light` | 18 | 0 |
| `execution` | `p256_verify` | 787 | 0 |
| `execution` | `gas_tip_enforcement` | 11 | 3 |
| `execution` | `fee_delegation` | 10 | 6 |
| `execution` | `process_finalize` | 17 | 6 |
| `source` | `candidates_at_epoch` | 8 | 0 |
| `governance` | `scenarios` | 24 | 0 |

`EpochInfo` 인코딩 case 는 `encoding/extra_codec` 안에 있다. WBFT-CRYPTO-056 의 key 하나, signature 하나를 디코드하는 case 는 원소가 하나인 목록으로 `crypto/aggregate_public_keys` 와 `crypto/bls_aggregate` 안에 있다.

handler 의 모양. handler 마다의 입력·출력 필드는 `tools/vectorgen/README.md` 의 "Handler inputs and outputs" 표에 있으며, 그 표가 이 명세의 handler 스키마다. 다음 스무 모양은 이유가 필요하다.

1. `bls_verify` 는 `public_keys` 목록을 받아 먼저 aggregate 한다 (WBFT-CRYPTO-028). 그래야 합이 무한원점인 key 들의 case(WBFT-CRYPTO-055)를 표현할 수 있다. 무한원점 key 하나는 이미 디코딩에서 거부된다.
2. WBFT-CRYPTO-056 의 디코딩 규칙은 따로 디코딩 handler 를 두지 않고, 원소가 하나인 `aggregate_public_keys` 와 `bls_aggregate` 로 시험한다.
3. `message_codec` 은 디코드한 필드와 다시 인코딩한 바이트를 돌려준다. `signing_payload` 는 메시지의 signing payload 와 signer, 그리고 메시지에 들어 있는 모든 서명된 payload 의 signing payload 와 signer 를 돌려준다 (WBFT-MSG-003).
4. `filtered_header` 는 extra 를 디코드할 수 없으면 실패한다. 참조 구현이 이때 header 를 돌려주지 않기 때문이다.
5. `quorum` 은 F 의 binary64 값을 big-endian 8 바이트로 적은 `f_float64_bits` 를 돌려준다. 그래서 WBFT-VAL-001 의 반올림을 float 의 10진 표기 없이 정확히 비교한다.
6. `round_timeout` 은 nanosecond 단위의 `timeout` 을 부호 있는 10진수로 돌려준다. base 가 wrap 되면 이 값은 음수이고, 그러면 timer 가 바로 만료된다. `warning` 은 `A-06` WBFT-TIMER-007 의 Warn 기록(`cap_overflow_guard`)이나 WBFT-TIMER-008 의 Warn 기록(`max_int64_clamp`)을 가리키며, Warn 기록이 없으면 `none` 이다.
7. `verify_header` 는 `verdict` 만 돌려준다 (WBFT-VEC-031). H2 가 검증하는 노드의 시계에 달려 있으므로 입력에 그 시계를 Unix 초로 적은 `now` 가 있다. 거부된 header 에 대한 참조 구현의 오류는 case 설명에 인용한다.
8. `verify_light` 는 `result` 를 돌려준다. 참조 구현에는 light verifier 가 없으므로 기대 결과는 참조 구현의 함수 호출들을 조합해서 만든다. `V` 와 `Vp` 는 `validators_at` 으로 얻고, header 검사는 `check_seals` 를 켜고 부모를 hash 로 찾아 참조 구현의 header 검증으로 하며, WBFT-HDR-130 은 `is_epoch_block` 으로 확인한다. `CANNOT_DECIDE` 는 `A-08` §8 의 정의를 따른다. 이것은 WBFT-VEC-020 의 예외이며, case 마다 그렇다고 밝힌다.
9. `check_message` 는 분류가 읽는 state 변수 세 개(`view`, `state`, `prior_round`)와 메시지의 `code`, `view` 를 직접 받는다. 단계로 만든 state 를 받지 않으므로 모든 조합을 적을 수 있다.
10. `is_justified` 는 ROUND-CHANGE 와 PREPARE 를 디코드한 필드로 받으며, 원소마다 `source` 를 함께 받는다. `source` 는 WBFT-SM-017 이 검사 전에 복원한 signer 다. proposal 은 검사가 읽는 유일한 성질인 block hash 로 받는다. `quorum` 도 입력이다.
11. `rounds` 와 `receive_outcome` 는 §3.1 "Steps" 의 단계 형식을 쓴다. `receive_outcome` 는 참조 구현의 `HandleMsg`, dedup cache, `Gossip` 을 코어와 함께 돌린다. `HandleMsg` 의 어떤 오류가 연결을 끊는지는 `eth` handler(`eth/handler_istanbul.go:118-127`)가 정하며, 생성기는 그 규칙을 적용한다. 같은 handler 가 `HandleMsg` 전에 검사하는 크기 상한(`:138-140`)도 생성기가 적용한다. 이것은 WBFT-VEC-020 의 예외이며, case 마다 그렇다고 밝힌다.
12. `fork_schedule` 은 preset 과 그 fork 필드를 바꾸는 값, block 번호, 시각을 받는다. `fork_id` 에는 그 설정의 genesis block 이 필요하며, 참조 구현이 그 block 을 만든다 (`B-02`).
13. `genesis` 는 preset genesis 명세와, 그 header 필드와 `bohoBlock` 을 바꾸는 값, 더할 잔액 계정을 받는다. 그리고 header RLP, `hash`, `state_root`, `extra` 를 돌려준다.
14. `verify_headers` 는 `headers` 목록을 받아 `verdicts` 를 돌려준다. 판정은 입력 순서대로 header 마다 하나이며, 각각 `verify_header` 의 판정처럼 비교한다 (WBFT-VEC-031). 처음으로 수락되지 않은 header 뒤의 header 는 모두 `reject` 다. 그 첫 header 가 지연된 경우도 같다. 참조 구현이 그 뒤 header 를 검증하지 않고 `ErrUnknownAncestor` 를 보고하기 때문이다 (`A-08` WBFT-HDR-120).
15. `build_wait` 는 `config_at` 이 주는 새 height 의 block 주기, head 의 시각, round, Unix nanosecond 단위의 시계 `now` 를 받아 nanosecond 단위의 `wait` 를 돌려준다. 참조 구현은 builder 에 부호 있는 기간을 넘기고 builder 는 그만큼 잠든다. 0 이하의 기간으로 잠들면 바로 돌아오므로, `wait` 는 그 기간과 0 가운데 큰 값이다 (`A-06` WBFT-TIMER-040). 참조 구현은 `time.Until` 로 시계를 읽는다. 생성기는 `now` 를 넣으려고 그 호출을 바꾼다.
16. `p256_verify` 는 `config`, block 번호 `number`, precompile 입력 `input` 을 받아 `output` 과 `gas` 를 돌려준다. `gas` 는 100 000 gas 로 `0x…0100` 을 호출했을 때 쓴 gas 다. `BohoBlock` 전에는 그 address 가 code 없는 계정이므로 출력이 비고 gas 를 쓰지 않는다.
17. `gas_tip_enforcement` 와 `fee_delegation` 은 `config`, block 의 `header`(실행이 읽는 `GasTip`, `BaseFee`, `Coinbase`, 번호가 그 header 의 값이다), 첫 트랜잭션 전의 `accounts`(§3.1 "실행 state"), 서명된 `transactions`(binary encoding)를 받는다. 그리고 트랜잭션마다 receipt(`status`, `gas_used`, `cumulative_gas_used`, `effective_gas_price`, `logs`)와, 입력 계정마다 실행 뒤의 `balance` 와 `nonce` 를 돌려준다. 유효하지 않은 트랜잭션이 있는 block(SNET-TX-060)은 실패 case 다. 참조 구현은 `StateProcessor.Process` 처럼 `core.ApplyTransaction` 으로 트랜잭션을 차례로 적용한다. finalization 은 `process_finalize` 가 다룬다.
18. `process_finalize` 는 block 의 부모에서 끝나는 chain fixture, block 의 `header`, `parent_gas_tip`(부모 state 에서 genesis GovValidator 의 slot `0x39`. 부모 state 를 얻을 수 없으면 `null`), block 의 트랜잭션 뒤의 `accounts`(빈 계정 없음), `path`(`verify_epoch` 로 `Finalize` 하는 `import`, `write_epoch` 로 `FinalizeAndAssemble` 하는 `propose`, `B-06` §6.1)를 받는다. 그리고 state `root`, proposer 경로가 쓴 `epoch_info`(import 경로이거나 쓴 것이 없으면 `null`), 입력 계정과 upgrade 가 건드린 계정의 `balance` 와 `code_hash` 를 돌려준다. finalization 이 거부하는 block 은 실패 case 다.
19. `candidates_at_epoch` 는 `config`, epoch block 번호 `number`, `accounts`(storage 를 가진 GovValidator 계정과, `extra` 가 의미 있는 다른 계정)를 받아 `candidates`(목록 순서의 `address`, `bls_public_key`. 선택 단계를 뺀 `B-08` §2.4)와 `gas_tip`(genesis GovValidator address 의 slot `0x39` 값, `B-08` §4)을 돌려준다.
20. `scenarios` 는 §3.1 "거버넌스 시나리오" 의 형식을 쓴다. 참조 구현은 `core.SetupGenesisBlock` 으로 genesis 를 만들고, `core.ApplyTransaction` 처럼 `core.ApplyMessage` 로 단계마다 호출을 적용한다. 쓰기 집합은 호출의 모든 `SSTORE` slot 을 적어 두고 값이 바뀐 slot 만 남겨서 얻는다.

[WBFT-VEC-031] `header/verify_header` 의 runner 는 반드시 case 의 최종 판정만 비교해야 한다. 최종 판정은 수락, 거부, 지연 가운데 하나다. 지연은 header 를 거부하지 않고 나중을 위해 남겨 두는 것이며, 미래 시각의 header 가 그 예다 (`A-08` §10.6). 참조 구현이 거부하거나 지연한 header 에 대해 보고하는 오류 종류는 informative 이고, runner 는 그것을 비교해서는 안 된다. header 가 지연되는지는 `A-08` 의 검사 순서에 달려 있으므로, 그 순서는 판정을 통해 계속 관측된다.
Source: `A-08` §10.6

[WBFT-VEC-030] 카탈로그는 반드시 다음 두 종류의 requirement 마다 vector 를 적어도 하나 가져야 한다. 첫째, `Observable:` 태그가 붙었지만 참조 commit 에서 바깥 observer 가 그 데이터를 얻을 수 없는 requirement (WBFT-VEC-003). 둘째, class `LV` 와 `OB` 의 모든 requirement.
Source: this chapter (coverage rule)

### 3.4 Adapter 프로토콜과 스키마의 주인

runner 와 시험 대상 구현은 서로 다른 프로세스다. 둘은 표준 입출력으로 한 줄에 JSON 객체 하나씩 주고받으며, 그 프로토콜 이름은 `wbft-vector/1` 이다. 순서는 다음과 같다.

1. runner 가 프로토콜 이름을 담은 `hello` 를 보낸다.
2. 구현은 자기 이름, 버전, 지원하는 handler 목록을 담은 `hello` 로 답한다.
3. runner 는 vector 마다 `case` 를 하나 보내고, 변환한 `input.yaml` 을 `input` 에 싣는다.
4. 구현은 `result` 로 답한다. `status` 는 `ok`(그리고 `output`), `error`, `unsupported` 가운데 하나다.
5. runner 가 `bye` 로 끝낸다.

runner 는 파일을 JSON 으로 변환하고 (WBFT-VEC-013), `output` 을 변환한 `expected.yaml` 과 비교하며, 구현이 죽거나 시간 제한을 넘으면 연산이 실패한 것으로 다룬다. 종류가 `steps` 인 case 는 초기 state 와 모든 단계를 한 번에 보내고, 구현은 단계마다 관측 가능한 출력의 기록을 돌려준다. 이 주고받기의 필드와 규칙은 아래 WBFT-VEC-033 ~ WBFT-VEC-062 가 정한다. 주고받는 예는 다음과 같다. 이 예에서 runner 는 앞 case 의 결과를 받은 뒤에 다음 case 를 보낸다 (WBFT-VEC-042).

```text
runner -> impl   {"type":"hello","protocol":"wbft-vector/1",
                  "runner":{"name":"inspector","version":"0.1.0"},"spec_commit":"def456"}
impl -> runner   {"type":"hello","protocol":"wbft-vector/1",
                  "impl":{"name":"wbft","version":"0.1.0","commit":"abc123","lang":"go","build":"cgo"},
                  "handlers":["crypto/keccak256","encoding/extra_codec","network/receive_outcome"],
                  "improvements":[]}
runner -> impl   {"type":"case","id":17,"runner":"encoding","handler":"extra_codec",
                  "case":"trailing_byte","kind":"pure","input":{"extra":"0xf8..."}}
impl -> runner   {"type":"result","id":17,"status":"error","error_class":"decode","message":"trailing bytes"}
runner -> impl   {"type":"case","id":18,"runner":"crypto","handler":"keccak256",
                  "case":"ascii_wbft","kind":"pure","input":{"data":"0x77626674"}}
impl -> runner   {"type":"result","id":18,"status":"ok","output":{"hash":"0x154d...c55f"}}
runner -> impl   {"type":"case","id":19,"runner":"network","handler":"receive_outcome",
                  "case":"codes_and_framing","kind":"steps","input":{"initial":{...},"steps":[...]}}
impl -> runner   {"type":"result","id":19,"status":"unsupported"}
runner -> impl   {"type":"bye"}
```

`error_class` 와 `message` 는 `expected_error` 처럼 informative 다 (WBFT-VEC-015).

[WBFT-VEC-032] adapter 프로토콜 `wbft-vector/1`, §3.1 의 파일 형식, §3.3 의 handler 스키마는 이 명세의 일부이며, vector 와 함께 `wbft-spec` repository 에서 버전을 관리한다. inspector, Go 구현과 Rust 구현, 생성기는 이 프로토콜을 구현할 뿐 정의하지 않는다. 프로토콜이나 스키마를 하위 호환되지 않게 바꾸면 반드시 그것이 필요한 vector 와 같은 commit 에서 프로토콜 이름을 바꿔야 한다 (`wbft-vector/2`).
Source: this chapter (schema ownership)

> 해설: 생성기가 `wbft-spec` 에 있으므로, 생성기의 출력 형식과 그 형식을 읽는 프로토콜도 같은 저장소에 두어야 한 commit 으로 함께 바뀐다. 단점은 명세 저장소가 도구 프로토콜까지 관리한다는 것이다.

프로토콜 세부. 아래 requirement 들이 위에서 설명한 주고받기를 정한다. runner 는 vector 를 읽고 결과를 비교하는 프로그램이다 (예: inspector). adapter 는 시험 대상 구현을 돌리면서 이 프로토콜로 말하는 프로그램이다. session 은 runner 하나와 adapter 프로세스 하나 사이에서 `hello` 부터 `bye` 까지 이어지는 주고받기다.

전송.

[WBFT-VEC-033] runner 와 adapter 는 반드시 UTF-8 로 인코딩한 JSON 객체를 한 줄에 하나씩 주고받아야 하고, 각 줄은 반드시 line feed 하나로 끝나야 한다. runner 는 adapter 의 표준 입력에 쓰고, adapter 의 표준 출력을 읽는다.
Source: this chapter (adapter protocol)

[WBFT-VEC-034] adapter 는 표준 출력에 프로토콜 메시지 말고는 아무것도 써서는 안 된다. adapter 는 진단 정보를 표준 오류에 쓴다.
Source: this chapter (adapter protocol)

[WBFT-VEC-035] runner 는 adapter 가 표준 오류에 쓴 내용을 기록해도 되지만, 그 내용을 해석해서는 안 된다.
Source: this chapter (adapter protocol)

[WBFT-VEC-036] runner 는 반드시 길이가 적어도 64 MiB 인 줄을 받아들여야 한다.
Source: this chapter (adapter protocol)

메시지.

[WBFT-VEC-037] session 의 첫 메시지는 반드시 runner 의 `{"type":"hello","protocol":"wbft-vector/1","runner":{"name":…,"version":…},"spec_commit":…}` 이어야 한다. `spec_commit` 은 runner 가 읽는 vector 가 속한 명세의 commit 이다.
Source: this chapter (adapter protocol)

[WBFT-VEC-038] adapter 는 runner 의 `hello` 에 반드시 `{"type":"hello","protocol":"wbft-vector/1","impl":{"name":…,"version":…,"commit":…,"lang":…,"build":…},"handlers":[…],"improvements":[…]}` 로 답해야 한다. `handlers` 는 adapter 가 지원하는 handler 를 `"<runner>/<handler>"` 형식으로 나열한다. `build` 는 구현을 어떻게 빌드했는지 적는다 (`"cgo"`, `"nocgo"` 또는 언어 고유 값). 기댓값은 언제나 참조 구현의 cgo 빌드를 따르므로 (WBFT-VEC-022), `build` 는 informative 다. `improvements` 는 시험 대상 구현에서 켠 improvement item 의 ID 를 나열하며, ID 말고 다른 것은 싣지 않는다.
Source: this chapter (adapter protocol)

[WBFT-VEC-039] adapter 의 `hello` 에 적힌 프로토콜 이름이 runner 의 것과 다르면, runner 는 반드시 session 을 끝내고 사용 오류를 보고해야 한다.
Source: WBFT-VEC-032

[WBFT-VEC-040] runner 는 case 를 반드시 `{"type":"case","id":…,"runner":…,"handler":…,"case":…,"kind":…,"input":…}` 로 보내야 하고, 다른 것은 싣지 않아야 한다. `id` 는 session 안에서 겹치지 않는 JSON 정수이고, `runner`, `handler`, `case`, `kind` 는 `meta.yaml` 의 필드이며, `input` 은 `input.yaml` 을 JSON 으로 바꾼 것이다 (WBFT-VEC-013). runner 는 `meta.yaml` 의 다른 내용과 `expected.yaml` 의 내용을 보내지 않는다. WBFT-VEC-014 의 값 규칙은 `input` 과 `output` 에 적용되며, `id` 같은 봉투 필드에는 적용되지 않는다.
Source: this chapter (adapter protocol)

[WBFT-VEC-041] adapter 는 vector 파일을 읽어서는 안 된다. adapter 에 필요한 것은 모두 `case` 메시지에 있고, 실행이 어떤 파일을 썼는지는 runner 가 기록한다 (WBFT-VEC-012).
Source: WBFT-VEC-012

[WBFT-VEC-042] runner 는 같은 session 에서 앞 case 의 결과를 받기 전에 다음 case 를 보내서는 안 된다. case 를 병렬로 돌리는 runner 는 adapter 프로세스를 여럿 띄운다.
Source: this chapter (adapter protocol)

[WBFT-VEC-043] adapter 는 case 마다 반드시 `{"type":"result","id":…,"status":…}` 로 답해야 한다. `id` 는 그 case 의 `id` 이고, `status` 는 `"ok"`, `"error"`, `"unsupported"` 가운데 하나다. `status` 가 `"ok"` 이면 결과는 반드시 `output` 도 가져야 한다. `output` 은 handler 스키마(§3.3)의 출력 필드를 가진 JSON 객체이며, 그 값은 WBFT-VEC-014 를 따른다. 정수는 앞에 0 을 붙이지 않은 10진 문자열로 쓰고, 빼기 부호는 handler 스키마가 부호 있는 값으로 정한 필드(`timers/round_timeout` 의 `timeout`)에만 쓴다. 바이트 문자열은 `0x` 를 붙인 짝수 길이의 소문자 16진수로 쓴다. 없는 값은 `null` 로 쓴다. `status` 가 `"error"` 이면 결과는 `error_class` 와 `message` 를 가져도 되며, 두 필드는 informative 다. `"unsupported"` 는 구현이 그 case 의 연산을 제공하지 않는다는 뜻이다.
Source: WBFT-VEC-014

[WBFT-VEC-044] runner 는 다음 세 가지를 반드시 프로토콜 오류로 다뤄야 한다. 첫째, adapter 가 보낸 줄이 JSON 객체가 아닌 경우다. 둘째, 메시지의 `type` 이 session 의 그 시점에 기대하는 것이 아닌 경우다. 셋째, 결과의 `id` 가 기다리는 case 의 것이 아닌 경우다. 프로토콜 오류가 나면 runner 는 adapter 가 종료했을 때와 똑같이 처리한다 (WBFT-VEC-048, WBFT-VEC-052).
Source: this chapter (adapter protocol)

[WBFT-VEC-045] runner 는 반드시 `{"type":"bye"}` 로 session 을 끝내고, 그 뒤에 adapter 의 표준 입력을 닫아야 한다.
Source: this chapter (adapter protocol)

[WBFT-VEC-046] adapter 는 `bye` 를 받은 뒤 반드시 5초 안에 상태 0 으로 종료해야 한다. runner 는 그때까지 종료하지 않은 adapter 를 강제로 끝내도 된다.
Source: this chapter (adapter protocol)

case 의 판정.

[WBFT-VEC-047] runner 는 반드시 `expected.yaml` 이 있는 case 를, 결과의 `status` 가 `"ok"` 이고 `output` 이 `expected.yaml` 을 JSON 으로 바꾼 것과 같을 때에만 통과로 판정해야 한다. 같다는 것은 모든 단계에서 key 가 같고, 목록의 길이와 순서가 같고, 문자열, boolean, null 이 같다는 뜻이다. `expected.yaml` 에 없는 key 를 가진 `output` 이나, `expected.yaml` 에 있는 key 가 빠진 `output` 은 실패다. `header/verify_header` 에서는 `expected.yaml` 의 유일한 필드인 `verdict` 만 비교된다 (WBFT-VEC-031).
Source: WBFT-VEC-031

[WBFT-VEC-048] runner 는 반드시 `expected.yaml` 이 없는 case 를, 연산이 실패했을 때에만 통과로 판정해야 한다. 연산이 실패했다는 것은 결과의 `status` 가 `"error"` 이거나, adapter 가 답하기 전에 종료하거나 그 case 의 시간 제한을 넘었다는 뜻이다. 그런 case 에 `status` 가 `"ok"` 인 결과가 오면 실패다.
Source: WBFT-VEC-010

[WBFT-VEC-049] runner 는 `expected.yaml` 이 없는 case 가 adapter 의 시간 초과로 통과했으면, 반드시 그 case 를 다른 통과 case 와 구분해 보고해야 한다. 그래야 실패하지 않고 멈춰 버리는 구현이 드러난다.
Source: this chapter (adapter protocol)

[WBFT-VEC-050] 그 실행의 어떤 adapter 도 case 의 handler 를 나열하지 않았거나, handler 를 나열한 모든 adapter 가 `"unsupported"` 로 답했으면, runner 는 반드시 그 case 를 통과도 실패도 아닌 unsupported 로 보고해야 한다.
Source: this chapter (adapter protocol)

session.

[WBFT-VEC-051] runner 는 반드시 모든 case 에 시간 제한을 두어야 한다.
Source: this chapter (adapter protocol)

[WBFT-VEC-052] adapter 가 종료하거나, case 가 시간 제한을 넘거나, 프로토콜 오류가 나면, runner 는 반드시 그 adapter 를 끝내고 새 adapter 프로세스를 띄운 뒤, 다음 case 를 보내기 전에 `hello` 를 다시 주고받아야 한다.
Source: this chapter (adapter protocol)

[WBFT-VEC-053] adapter 는 반드시 각 case 의 결과를 앞서 받은 case 와 무관하게 계산해야 한다.
Source: this chapter (adapter protocol)

[WBFT-VEC-054] 구현은 여러 adapter 로 나누어 시험받아도 된다. 예를 들어 합의 층의 adapter 하나와 실행 층의 adapter 하나를 쓸 수 있다. 그 경우 runner 는 반드시 각 case 를 그 handler 를 나열한 adapter 에 사용자가 준 순서대로 내놓아야 하고, `"unsupported"` 로 답한 case 는 반드시 그 다음 adapter 에 내놓아야 한다. runner 는 결과를 구현 하나의 결과로 보고한다.
Source: this chapter (adapter protocol)

steps case. `state_machine` 또는 `network` handler 의, 종류가 `steps` 인 case 의 `output` 은 §3.1 "Steps" 의 기록 형식으로 `start` 와 `steps` 를 담는다.

[WBFT-VEC-058] `state_machine` 또는 `network` handler 의, 종류가 `steps` 인 case 에서 adapter 는 반드시 `initial` 로부터 `Start` 직후의 노드를 만들어야 하며 (§3.1 "Steps"), 다른 state 에서 시작해서는 안 된다.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

[WBFT-VEC-059] `state_machine` 또는 `network` handler 의, 종류가 `steps` 인 case 에서 adapter 는 반드시 단계가 준 event 만 그 순서대로 처리해야 하고, 다른 event 는 처리해서는 안 된다. 그래서 구현의 timer 는 발화하지 않고, 예약된 event 는 단계가 주지 않으면 처리되지 않는다.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

[WBFT-VEC-060] `state_machine` 또는 `network` handler 의, 종류가 `steps` 인 case 에서 adapter 는 예약된 event 를 주는 단계(self-delivery 인 `message`, `backlog`, `request`)를, 구현이 그 event 를 예약했든 안 했든 반드시 주어진 대로 처리해야 한다. 그러려고 adapter 는 case 동안 예약된 event 를 단계가 처리할 때까지 내용으로 찾는 queue 에 둔다.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

[WBFT-VEC-061] `A-05` §3.1 의 state 변수를 보고할 수 없는 adapter 는 종류가 `steps` 인 `state_machine` 또는 `network` handler 의 모든 case 에 일부만 채운 기록을 돌려주지 말고 반드시 `"unsupported"` 로 답해야 한다.
Source: this chapter (adapter protocol)
Source: this chapter (steps format)

생성기.

[WBFT-VEC-062] 생성기는 반드시 adapter 를 제공해야 한다. 그 adapter 는 생성기가 쓴 vector 의 모든 case 에 참조 구현으로 계산한 값으로 답한다. 그래야 runner 가 vector 를 그 생성기와 대조할 수 있다.
Source: WBFT-VEC-032

> 해설: 한 번에 한 case 만 보내게 하면 adapter 가 case 사이에 state 를 가질 이유가 없고, 결과의 `id` 가 어긋나는 오류도 생기지 않는다. 단점은 프로세스 하나의 처리량이 낮다는 것이다. 엄격한 비교는 필드 이름을 잘못 쓴 adapter 를 바로 드러내지만, adapter 가 진단용 필드를 `output` 에 덧붙일 수 없게 한다. 진단 정보는 표준 오류로 내보낸다. 여러 adapter 를 순서대로 쓰는 규칙은 `network/receive_outcome` 의 DISCONNECT case 처럼 한 handler 의 일부 case 만 다른 adapter 가 맡는 경우를 처리한다. 단점은 두 adapter 가 같은 case 에 다르게 답해도 runner 가 앞의 답만 본다는 것이다.

---

## 4. 유지 관리

- 참조 commit 이 바뀌면 (`A-12` §4) 명세 관리자는 모든 vector 를 다시 만들고 기댓값의 차이를 검토한다. 바뀐 기댓값은 합의 변경이거나 생성기 버그다. 합의 변경이면 `A-12` 에 기록한다.
- requirement ID 는 안정적이므로, vector 는 다시 만든 뒤에도 같은 ID 를 가진다.
- vector 는 명세와 함께 `wbft-spec` repository 로 옮겨 간다. adapter 프로토콜과 스키마도 함께 옮겨 간다 (WBFT-VEC-032).
