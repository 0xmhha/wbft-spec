# 한국어판 작성 지침

`spec-ko/` 는 영문 규범 명세(`../spec/`)의 한국어판이다. 파일 이름, 장·절 번호, 요구사항 ID, 표와 의사코드의 배치를 영문판과 똑같이 맞춘다. 규범은 영문판이며, 두 판이 다르면 영문판이 맞다. 이 지침은 한국어판을 쓰거나 고칠 때 지킨다.

---

## 1. 파일과 구조

- 영문판 `../spec/X.md` 마다 같은 이름의 `X.md` 를 둔다. 영문판에 없는 파일은 이 지침(`STYLE.md`)뿐이다.
- 장·절 제목, 번호, 순서를 영문판과 같게 둔다. 제목은 한국어로 쓰되, 전문 용어는 영어로 남긴다 (예: `## 5. Proposer selection` → `## 5. Proposer 선택`).
- 요구사항은 영문판과 같은 ID 로 시작한다: `[WBFT-SM-012] ...`. 요구사항 하나를 한국어 문장으로 옮기고, `Source:`, `Observable:`, `Improvement:` 줄은 영문 그대로 둔다.
- 의사코드, 코드 블록, 바이트 예제, 표의 수치는 영문판을 그대로 옮긴다. 의사코드 안의 주석만 한국어로 바꿀 수 있다.
- 영문판에 없는 설명을 덧붙일 때는 `> 해설:` 로 시작하는 인용 단락에 쓴다. 해설은 규범이 아니며, 왜 그런 규칙인지, 무엇을 조심해야 하는지를 설명한다.

---

## 2. 규범어

| 영문 | 한국어 | 쓰는 법 |
|---|---|---|
| MUST, SHALL, REQUIRED | 반드시 ~해야 한다 | "노드는 반드시 ~해야 한다" |
| MUST NOT, SHALL NOT | ~해서는 안 된다 | "노드는 ~해서는 안 된다" |
| SHOULD, RECOMMENDED | ~하는 것이 좋다 (권고) | 처음 나올 때 "(SHOULD)" 를 붙여도 된다 |
| SHOULD NOT | ~하지 않는 것이 좋다 | |
| MAY, OPTIONAL | ~해도 된다 | |

규범어를 빼거나 다른 강도로 바꾸지 않는다. 한국어판은 영문과 규범의 강도와 적용 범위가 같아야 한다. 영문의 MUST 하나가 여러 동작에 함께 걸리면, 한국어에서는 동작마다 "반드시" 를 붙여 그 범위를 드러내도 된다. 그래서 "반드시" 의 개수는 영문 MUST 의 개수보다 많을 수 있지만, 영문에 없는 의무를 새로 만들어서는 안 된다.

---

## 3. 용어: 억지로 번역하지 않는다

아래 용어는 영어 그대로 쓴다. 한국어 조사는 영어 뒤에 붙인다 (예: "proposer 가", "quorum 에", "seal 을").

| 분야 | 영어 그대로 쓰는 용어 |
|---|---|
| 합의 | validator, proposer, round, sequence, height, view, quorum, round change, ROUND-CHANGE, PRE-PREPARE, PREPARE, COMMIT, justification, prepared block, prepared round, prepared certificate, lock, locked, decide, decision, commit, finalize, extra seal, backlog, catch-up, liveness, safety, amnesia, byzantine |
| 블록·헤더 | block, header, body, block hash, digest, parent, head, canonical, epoch, epoch block, EpochInfo, candidate, diligence, shuffle, seal, prepared seal, committed seal, aggregated seal, sealer, bitmap, randao, reveal, mix, gas tip, base fee, coinbase, vanity, extra |
| 구현 | event, handler, timer, timeout, retry, goroutine, channel, lock (뮤텍스 뜻일 때도), cache, queue, buffer, WAL, replay, snapshot, import, insert, fetcher, downloader, block builder, proposer path, state, state root, trace, storage, slot, mapping, precompile, callback, adapter, profile |
| 네트워크 | peer, gossip, relay, broadcast, frame, payload, wire, dedup, handshake, subprotocol |
| 암호 | signature, signing payload, recover, public key, secret key, aggregate, verify, high-S, low-S, DST, PoP |
| 명세 | requirement, conformance, vector, observer, inspector, improvement item |
| 거버넌스 | member, proposal, vote, voter, approve, disapprove, quorum, operator, blacklist, authorized, expiry, attempt, soft failure, hard failure, revert |

번역해도 되는 말: 노드, 네트워크, 메시지, 규칙, 검사, 요구, 조건, 설정, 계산, 저장, 전송(send), 수신(receive), 순서, 값, 목록, 집합, 오류, 거부, 수락.

"잠금", "사건", "가져오기", "블록 생성기", "준비 블록", "정족수", "봉인" 같은 억지 번역은 쓰지 않는다. 각각 lock, event, import, block builder, prepared block, quorum, seal 로 쓴다. 거버넌스의 "멤버", "제안", "투표" 도 member, proposal, vote 로 쓴다. 다만 자료 구조의 구성원(집합의 원소, struct 의 필드)을 뜻할 때는 "원소", "필드" 로 쓴다. 노드를 운영하는 사람은 "노드 운영자" 로 써서 거버넌스 operator 와 구분한다. "검증자" 는 validator 와 헷갈리므로 verifier 를 옮길 때 쓰지 않고 "검증하는 노드" 또는 verifier 로 쓴다.

---

## 4. 문장: 끝까지 쓴다

한국어판에서 가장 흔한 실패는 단어를 이어 붙여 문장을 급히 끝내는 것이다. 이렇게 쓰면 누가 무엇을 하는지, 무엇 때문에 무엇이 일어나는지가 사라져서 문장을 해석할 수 없다. 다음을 지킨다.

1. **모든 문장에 주어와 서술어를 둔다.** 주어를 생략해도 되는 것은 바로 앞 문장과 주어가 같을 때뿐이다.
2. **명사로 끝내지 않는다.** "~수락", "~송신", "~완료", "~생략" 으로 끝내지 말고 "~한다", "~했다", "~이다" 로 끝낸다.
3. **화살표(→)를 문장 안에서 쓰지 않는다.** 순서는 "~한 뒤 ~한다", "~하면 ~한다" 로 쓴다. 화살표는 의사코드와 mermaid 그림 안에서만 쓴다.
4. **괄호로 문장을 대신하지 않는다.** "(첫 메시지를 남김)" 대신 "이때 첫 메시지를 남긴다." 로 쓴다.
5. **원인과 결과를 연결어로 잇는다.** "그래서", "그러므로", "~하기 때문에", "~하면" 을 쓴다.
6. **대명사보다 명사를 쓴다.** "그것", "이것", "해당" 대신 가리키는 대상을 다시 쓴다 (예: "그 메시지" 대신 "그 PREPARE").
7. **한 문장에 한 가지만 말한다.** 조건이 셋 이상이면 번호 목록으로 나눈다.
8. **표의 칸도 뜻이 통하게 쓴다.** 표 칸에서는 명사구를 써도 되지만, 칸 하나만 읽어도 뜻이 통해야 한다.

### 고치기 전과 후

| 고치기 전 (쓰지 않는다) | 고친 후 |
|---|---|
| 정당한 PRE-PREPARE 수락 → PREPARE 송신 | 노드는 justification 이 유효한 PRE-PREPARE 를 받아들인 뒤 PREPARE 를 보낸다. |
| PREPARE Q개 → 잠금, COMMIT 송신 | PREPARE 가 quorum 만큼 모이면 노드는 그 block 에 lock 을 걸고 COMMIT 을 보낸다. |
| 블록 h 가져오기 완료, 헤드 = h | block h 의 import 가 끝나서 head 가 h 가 되었다. |
| NewHead 사건은 아직 큐에 있음 | NewHead event 는 아직 queue 에 남아 있다. |
| 인자가 0 이 아니므로 round-change 집합·certificate·extra seal 초기화 생략 | 인자가 0 이 아니므로 노드는 round change 집합, prepared certificate, extra seal 을 초기화하지 않는다. |
| notify_new_round(r+1) → 블록 생성기가 대기 없이 조립 | 노드가 `notify_new_round(r+1)` 을 부르면 block builder 는 block period 를 기다리지 않고 block 을 조립한다. |
| 보낸 사람 기준으로 중복을 없앤다(첫 메시지를 남김). | 보낸 사람이 같은 메시지가 여럿이면 첫 메시지만 남기고 나머지는 버린다. |
| 모두 is_justified 확인 후 X 에 PREPARE → COMMIT | 모든 validator 가 `is_justified` 로 PRE-PREPARE 를 확인한 뒤 block X 에 PREPARE 를 보내고, 이어서 COMMIT 을 보낸다. |

---

## 5. 영문판을 옮길 때

- 번역은 문장 단위가 아니라 뜻 단위로 한다. 먼저 영문 문장에서 주어, 동작, 대상, 조건을 확인하고, 그 관계가 드러나도록 한국어 문장을 새로 짠다. 영문의 수동태는 주체가 분명하면 능동태로 바꾼다 (예: "A message is discarded" → "노드는 그 메시지를 버린다").
- 영문판의 한 문장이 길면 한국어에서는 둘로 나눠도 된다. 다만 요구사항의 규범어와 조건은 하나도 빠뜨리지 않는다.
- 식별자, 함수 이름, 상수, 필드 이름은 백틱으로 감싸고 그대로 쓴다.
- 문체는 "~한다" 체다.
