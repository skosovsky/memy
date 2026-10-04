# Независимая adversarial приёмка: корректность, round 3

Дата: 2026-10-04. Независимый correctness reviewer. Отчёты completeness reviewer не читались и не использовались.

**Вердикт:** C1–C8 закрыты в повторно выполненных независимых scenarios; primary-origin C9 independently closed в проверенных malformed time cases. Приёмка остаётся заблокирована C10 (P2, expired acceptance authority lease) и C11 (P1, self-invalidating transitive target update). C10 найден этим reviewer; C11 первоначально найден primary и независимо подтверждён на обеих stores.

## Frozen state и отсутствие изменений

Snapshot `docs/reviews/snapshot-wire-deadlines.json`: **059590f6e2e6daa958eec8e6f19a39aa432bba46835b5838631d60cb48065292**, 91 файл. Каждый file SHA256/размер проверен; aggregate пересчитан из compact JSON списка files с sorted keys и совпал. Финально changed files = [].

Production, штатные tests, acceptance matrix, README/design и lint config этим reviewer не менялись. Новый output — только этот отчёт. Scratch `/private/tmp/memy-correctness-round3/audit_test.go` и `repro.log`; отдельный `lineage_target_test.go` в том же scratch создан primary, явно помечен как primary evidence. Его существование не подменяет собственный independent C11 case. Никаких commits/releases/publication.

Прочитаны изменённые deadline_gate/wire_shape и gate placements в engine/commit/recall/consolidation; новые deadline regressions, публичные temporal/retention/source contracts. Primary C9 источник `docs/reviews/wire-time-primary-finding.md` прочитан с разрешения parent; происхождение находки не приписывается независимому reviewer.

## C7–C9 closure и проверки затронутых гарантий

- **C7:** внешний `TestNumericBytesWireCorruption` PASS для payload/reference numeric arrays × memory/SQLite. Runtime возвращает ErrSchema. requiredBase64 отдельно проверен по коду: strict decode + exact re-encode comparison запрещают newline/noncanonical padding; json.RawMessage исключён, zero tombstone paths сохраняются. Свежий штатный `TestByteWireRepresentationFailsClosed` дополнительно исполнил array/newline cases для обеих fields/stores.
- **C8:** внешний `TestExpiryDuringSourceRead` PASS для Get/Snapshot × обе stores; `TestAcceptanceExpiryDuringSourceIO` PASS; `TestCommitDependencyExpiryDuringSourceIO` PASS на обеих stores. Подтверждено закрытие исходного expired-content/expired-proposal/expired-ancestor defect, а не лишь его early rejection до I/O.
- **Multi-result expiry:** новый independent `TestMultiResultDeadlineIndependentClosure` PASS на memory/SQLite. Fixture проверяет реальный порядок history entries: finite `a` обработан раньше unlimited `b`; только later `fresh` source I/O сдвигает Clock. Финальный batch gate подавляет ранее выбранную expired запись.
- **C9, primary-origin:** новый независимый `TestWireTimePrimaryClosure` PASS: comma fraction, +00:60 и +24:00 отклоняются runtime. Свежий штатный `TestTimestampWireFormatMatchesExecutableSchema` выполнен вместе со всей suite: одновременно actual JSON Schema validator и runtime rejects. requiredTime проверяет lexical profile до Go parser. Это closure конкретных malformed time representations, а не доказательство эквивалентности любых возможных date-time implementations.
- **Rollback/CAS/replay новой final gate:** новый independent `TestFinalDeadlineFailureRollsBackReconcileAndReplay` PASS на обеих stores. Authority callback пересекает proposal deadline только после внутренних supersede writes. ErrStaleInput откатывает transition старой записи, новый head и operation/receipt; старый record остаётся Active. После fixture clock reset тот же request/op ID успешно создаёт revision 1; replay возвращает ровно тот же receipt. Это active проверка атомарности хвостового отказа, а не только отсутствие output payload.
- Предыдущие независимые C1–C6 cases снова PASS, включая stale direct/transitive source cases, post-rank/post-project source mutation, monotonic Acceptance.Equal, physical key binding, case-sensitive inventory, annotation budget с provider invoked=true и no-min visibility coverage.
- Independent SQLite BeforeCommit rollback и AfterCommit unknown outcome/reopen снова PASS.

## C10 — P2: Accept успешно возвращает уже expired первоначальный authority lease

**Контракт:** Acceptance связывает host actor/policy/expiry. Успех отдельного host review должен возвращать пригодное ещё действующее acceptance; конкретный TTL принадлежит host, а не caller payload. Повторная проверка может подтвердить текущие права, но не должна создавать успешный token с уже прошедшим ExpiresAt.

**Code paths:** `engine.go:445` final acceptance deadlineGate включает только `proposalDeadline(p)`. `engine.go:448` возвращает/сохраняет ExpiresAt из первоначального `decision`, полученного до validation I/O. `reauthorize` проверяет новый decision на validity и Actor/PolicyVersion; актуальный renewed expiry не заменяет старое поле и старый deadline не проверяется final gate.

**Independent AAA repro:** `TestAcceptanceExpiredInitialAuthorityDecision`, memory и SQLite:

1. Stable Authority Actor/PolicyVersion, но каждый Check выдаёт renewable `ExpiresAt = injected Clock.Now().Add(1h)`.
2. Proposal имеет unlimited retention/evidence expiry. До Accept Clock = 00:00, поэтому initial authority lease истекает в 01:00.
3. Cooperative Sources.Validate во время Accept сдвигает Clock точно на 01:00 и подтверждает unchanged source.
4. Reauthorize выдаёт **новый valid lease** до 02:00 с теми же Actor/PolicyVersion, поэтому проходит.
5. Accept возвращает nil error и Acceptance.ExpiresAt = **01:00**, уже expired. Немедленный Commit с этим exact returned object даёт ErrStaleAcceptance.

```text
Accept success with expired authority lease:
expiry=2026-06-01 01:00:00 +0000 UTC now=2026-06-01 01:00:00 +0000 UTC
immediate Commit=memy: stale acceptance
```

**Последствие:** успешный review persist-ит Accepted state, но выдаёт непригодный token; даже при сохранённых текущих host permissions следующий обычный lifecycle шаг невозможен с returned acceptance. Privacy bypass или canonical commit с expired acceptance этим repro не утверждается: Commit correctly rejects. Это отдельный пропуск final authority deadline, C8 direct proposal/dependency fix его не покрывает.

**Ожидаемое поведение:** reject истёкший первоначальный decision или явно bind предусмотренное контрактом новое host decision. Silent success с expired bound identity недопустим. Выбор renewal semantics принадлежит контракту; reviewer не менял policy.

## C11 — P1: own target update делает новую revision и её обязательный input сразу stale

**Происхождение:** original case обнаружен primary и передан в ходе frozen round 3. Reviewer прочитал его scratch и создал отдельный `TestTransitiveTargetIndependentAudit`, расширенный на memory и SQLite и с проверкой исходной readability.

**Контракт:** canonical reads/acceptance/commit проверяют exact live dependency revisions. Core уже запрещает direct lineage на собственный RecordID (`commit_state.go:216`), поскольку update этого head не может сохранить такую dependency текущей. Conditional write не должен certify новую revision, чьи обязательные live inputs становятся недействительными от самой этой записи.

**Code paths:** `commit_state.go:206` commitLineage проверяет только непосредственный list inputs. `deadline_gate.go:11` проверяет transitive current heads **до** `commit_state.go:65` persistCommit. После проверки persistCommit заменяет target head; prospective state новой записи в traversed graph не учитывается.

**Independent minimal repro:**

1. Commit active `a1`, payload original.
2. Commit active `b1` с exact lineage `{a,1}`. Get(b) успешно читается.
3. Remember/Accept нового payload updated с direct lineage `{b,1}`.
4. Commit в **тот же RecordID a**, Expected=1, Append; новый a2 напрямую не ссылается на a, поэтому direct self guard проходит. Полная цепочка до write: `a2 -> b1 -> a1`.
5. Commit возвращает CanonicalCommitted=true, revision=2. Теперь dependency a1 больше не соответствует current head a2; immediately Get(a) **и** Get(b) возвращают ErrStaleInput.

```text
self-invalidating transitive update committed=true revision=2
Get(a)=memy: stale input Get(b)=memy: stale input
```

**Последствие:** durable successful receipt certifies новое знание, которое невозможно прочитать в current profile непосредственно после commit. Ранее доступный обязательный input b1 также стал stale. Обычное обновление ancestor вправе invalidates dependent b1; ошибка здесь в том, что новая a2 сама требует b1, invalidated её собственным update.

Это **не утверждение о цикле exact revision DAG**: a2→b1→a1 является ациклической исторической цепочкой. Ошибка возникает именно из требования current exact heads и self-invalidating target write. Historical read/provenance могут остаться доступны согласно отдельным temporal predicates; repro проверяет текущую canonical выдачу.

**Ожидаемое поведение:** reject target, встречающийся в полной mandatory transitive lineage, до успешного commit; либо контрактно поддержать иной retrospective dependency model. В текущем live-head контракте продолжать direct guard только на один уровень недостаточно. Atomic rollback должен сохранить a1/b1 при rejection.

## Точно выполненные проверки

В repo, свежие executions, оба exit 0:

```sh
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -count=1 ./...
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -race -count=1 ./...
```

В scratch, полный independent suite (с дополнительно присутствующим primary C11 case):

```sh
cd /private/tmp/memy-correctness-round3
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -mod=mod -v -count=1 ./...
```

Compilation/execution успешны; exit 1 ожидаем из-за C10/C11 contract assertions. Все C1–C9 closure tests, callback gates, multi-result expiry, final-gate rollback/retry/replay и SQLite fault recovery PASS. Три failing top-level tests относятся к двум defects: C10 independent case; C11 independent case; C11 original primary case. Полный лог `repro.log` обновлён последним запуском.

Отдельный `-run TestFinalDeadlineFailureRollsBackReconcileAndReplay` exit 0; отдельный combined `-run` для C10/C11/multi-result подтвердил те же outputs. `checks-wire-deadlines.log` предоставлен primary как successful make validate; reviewer не приписывает себе этот запуск и не менял strict config.

## Пределы вывода

macOS arm64, CGO, Go 1.26.5. Новых подтверждённых wire/expiry issues сверх C10/C11 в проверенных current cases нет. Reviewed changed deadlines gathering, transitive exact heads, authority/acceptance timing, projection/rank delivery, scalar shape, conditional commit and transactional tail failure. Existing adversarial tests остальных областей исполнились свежими normal/race runs.

Не проводились исчерпывающее перебирание всех graph/state combinations, stress under production load, physical storage failure/forensic erasure, arbitrary consumer codec implementations или внешние IAM distributed lease protocols. Отсутствие новых findings в этих областях не является доказательством абсолютной корректности. Report относится только к указанному frozen hash; будущие исправления C10/C11 потребуют отдельной closure проверки.
