# Независимая adversarial приёмка: корректность, round 2

Дата: 2026-10-04. Ревьюер: тот же независимый correctness agent. Completeness reports другого ревьюера не читались; его результаты при выводах не использовались.

**Вердикт:** первоначальные C1–C6 закрыты в воспроизведённых сценариях. Приёмка по корректности ещё не пройдена: подтверждены новые C7 (P2, wire byte shape) и C8 (P1, expiry во время port I/O), включая успешный canonical commit с уже expired dependency. Зелёные штатные tests/race этого не опровергают.

## Snapshot и ограничения изменений

`docs/reviews/snapshot-source-integrity.json`: SHA256 **9e13403fe095478a6d41167f769391674d5dc025b53eb395e0e4df6613aa4e14**, 89 файлов. Проверены размер и SHA256 каждого файла, агрегат compact/sorted-keys JSON списка files совпадает. После active repro ни одного изменения в snapshot files не обнаружено.

Production source, штатные tests, lint config, матрица, README/design не менялись этим ревьюером. Создан только этот отчёт. Repro module: `/private/tmp/memy-correctness-round2`; полный AAA source: `audit_test.go`; execution log: `repro.log`. Модуль использует replace на проверяемый repo. Никаких commits/releases/publication.

## Closure C1–C6

Проверка включала чтение изменённых implementation paths и независимые внешние executions, а не только новые regressions в source snapshot.

| Finding round 1 | Независимая проверка | Результат |
|---|---|---|
| C1: source read/lineage bypass | `TestSourceChangesAfterCommit`: revision/remove/outage × memory/SQLite × Get/Snapshot/Recall/Project; `TestDirectLineageClosure`: fresh direct source/stale ancestor отдельно на Accept и Commit × обе stores; `TestSourceCallbackClosure`: source удаляется внутри rank/project callback × обе stores | Все cases PASS: stale/outage gates suppress payload; Accept/Commit не обходят ancestor. Consolidate с stale input отдельно отклоняется на раннем gate. |
| C2: monotonic acceptance deadline | `TestAcceptanceMonotonicExpiry`: Authority даёт `time.Now().Add(1h)`, точный returned Acceptance передаётся в Commit, обе stores | PASS: успешный commit; implementation сравнивает expiry через Equal. |
| C3: history key/revision mismatch | `TestRecordRevisionKeyCorruption`: history key rev1 содержит envelope rev7 | PASS: Get/Snapshot возвращают ErrSchema. |
| C4: unknown case alias | `TestSchemaRejectsCaseAliasUnknownField`: root SCHEMA=99 при schema=1 | PASS: runtime теперь rejects alias, exact field inventory проверяется отдельно от Go decoder. Это closure исходного case-alias defect, не утверждение о любых scalar representations (см. C7). |
| C5: annotations обходят budget | `TestConsolidationOutputBudget`: InputBytes=10000, OutputBytes=2048, Evidence=1MiB; отдельно asserted provider invoked=true | PASS: ErrBudget после provider, без output proposal. Не подменено ранним input-budget rejection. |
| C6: empty lagged search Complete=true | `TestPendingIndexCoverage`: canonical commit и managed Stage без Acknowledge, Recall без minimum | PASS: без minimum result не certifies completeness; implementation различает pending/eventual. |

Новые port suites `conformance/{ports,search,sink}.go` и `reference/conformance_test.go` прочитаны и выполнены свежими tests/race. Они расширяют reference port coverage, однако не являются универсальным доказательством соответствия произвольных внешних adapters.

## C7 — P2: numeric byte arrays проходят runtime вопреки base64 string schema

**Ожидаемый контракт:** wire contracts `schemas/document-v1.schema.json:243` (reference) и `:301` (payload) требуют JSON string с base64 shape/contentEncoding. Design требует schema corruption fail closed. RawMessage документа — отдельный structural JSON объект, не consumer byte field.

**Code path:** `wire_shape.go:29` исключает slices с elem uint8 из recursive shape checks. Следующий Go json.Decoder умеет декодировать `[]byte` не только из base64 string, но и из numeric JSON array. `validation.go` проверяет decoded byte content/digest, а не wire scalar shape.

**Минимальный fault repro:**

1. Remember → Accept → Commit record `r`, revision 1, payload `"fact"`, source reference `"host://source"`.
2. Trusted Store.Update имитирует schema corruption в history document: заменить `proposal.payload` base64 string на numeric array тех же decoded bytes. Отдельный case делает то же для `proposal.sources[0].reference`.
3. Не менять proposal digest и остальные поля. Вызвать Get.

`TestNumericBytesWireCorruption`, четыре subcases (payload/reference × memory/SQLite), каждый FAIL на контрактном assertion:

```text
payload numeric array accepted: payload="fact" revision=1 err=<nil>
reference numeric array accepted: payload="fact" revision=1 err=<nil>
```

**Механизм:** после decode bytes совпадают с оригиналом; recomputed proposal digest сериализует их назад как base64, поэтому digest check не выявляет исходную wire corruption. Exact field inventory не решает эту несовместимость типов.

**Последствие:** runtime читает документы, запрещённые executable JSON Schema, и молча нормализует некорректное представление вместо schema error. Подтверждена contract/schema integrity ошибка; успешного обхода ACL или изменения payload bytes этим repro не утверждается. Проверка должна отличать consumer []byte от json.RawMessage, требовать допустимый base64 string, сохраняя предусмотренный null только в content-free tombstone.

## C8 — P1: expiry проверяется до I/O, а после I/O выдача/acceptance/commit остаются успешными

**Ожидаемый контракт:** proposal expiry проверяется по injected Clock; expired proposal не принимается. Current reads и full transitive lineage проверяют effective deadline; retention/evidence deadline ограничивает существование canonical знания. Commit обязан revalidate source/lineage fences и не фиксировать знание с уже expired dependency.

**Code paths:**

- `engine.go:639` canonicalCandidate и `consolidation.go:105` snapshotCandidate вызывают readable/expiry **до** Sources.Validate, lineage и retention I/O; после этих вызовов возвращают typed payload без финального effective-deadline gate.
- `engine.go:252` validateInputs проверяет direct proposal expiry до source/retention calls, затем `engine.go:270` возвращает lineage result без финального direct-expiry check. Accept (`engine.go:436`) после этого сохраняет acceptance.
- `engine.go:731` liveLineageInput проверяет ancestor expiry до retention и Sources.Validate (`:737`, `:740`), после их выполнения возвращает ancestor без последнего deadline check. Commit final check в `commit_state.go:57` относится только к самому proposal; direct derived proposal может иметь unlimited deadline, поэтому он не ловит expired ancestor.
- Project/Recall имеют дополнительные direct result checks, но вся transitive chain всё равно зависит от указанного liveLineageInput порядка.

**Active fault adapter:** stable Registry и cooperative Sources.Validate. При armed=true он сдвигает injected Clock с `2026-06-01T00:00Z` точно на finite deadline `2026-06-01T01:00Z`, затем успешно подтверждает неизменённые evidence bytes. Это deterministic модель обычного host/source I/O, который начался до expiry и завершился после него. Authority неизменна; context не canceled; deadline не продлевается.

**Repro A — Get/Snapshot:** fixed retention deadline; committed record жив в начале read. Истекает во время его Sources.Validate. `TestExpiryDuringSourceRead`: Get и Snapshot × memory/SQLite возвращают `"secret"` с nil error при `Clock.Now() == ExpiresAt`.

```text
expired content returned: payload="secret"
clock=2026-06-01 01:00:00 +0000 UTC deadline=2026-06-01 01:00:00 +0000 UTC
```

**Repro B — Accept:** proposal имеет future explicit ExpiresAt, authority expiry unlimited. Во время Acceptance source check Clock пересекает proposal deadline. `TestAcceptanceExpiryDuringSourceIO` возвращает успешный Acceptance для уже expired proposal. Эта проверка выполнена на memory; shared core path тот же для SQLite.

**Repro C — conditional canonical Commit:** `TestCommitDependencyExpiryDuringSourceIO`, memory и SQLite:

1. Original record `original` с payload `"old"` имеет finite retention deadline и direct source `source/r1`.
2. Derived proposal `"derived"` имеет independently fresh direct source `fresh/r1`, unlimited own retention/expiry и exact lineage `{original,1}`. Remember/Accept выполнены до deadline.
3. Только ancestor source validation во время Commit сдвигает Clock на ancestor deadline; проверка own direct source не сдвигает Clock.
4. Commit возвращает **CanonicalCommitted=true**, record `derived`, revision 1 — dependency уже expired к моменту фиксации.

```text
expired dependency committed:
receipt={OperationID:commit-derived RecordID:derived Revision:1 CanonicalCommitted:true ...}
clock=2026-06-01 01:00:00 +0000 UTC deadline=2026-06-01 01:00:00 +0000 UTC
```

**Последствия:** текущая выдача нарушает retention boundary; expired proposal может получить acceptance; durable receipt certifies новое canonical знание, чьи обязательные inputs уже не живы. Последующее чтение derived может fail closed, однако это не исправляет ошибочно committed lifecycle event. Не требуется общее distributed transaction с IAM: данный repro использует собственный injectable Clock и неизменные source/retention decisions. Это пропуск детерминированной финальной проверки времени после известных синхронных host calls.

Нужен финальный gate по direct proposal и всей dependency chain после host I/O/callbacks и перед delivery/acceptance/commit. Для Snapshot и multi-result delivery проверка должна охватывать все возвращаемые записи: более позднее I/O также может истечь срок ранее выбранной записи. Это последнее замечание выводится из порядка batch processing; отдельного multi-record expiry repro в этом раунде нет.

## Выполненные команды и evidence

```sh
# В исходном repo, свежие штатные suites
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -count=1 ./...
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -race -count=1 ./...

# Внешняя AAA приёмка frozen snapshot
cd /private/tmp/memy-correctness-round2
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -mod=mod -v -count=1 ./...
```

Обе штатные команды PASS, включая fresh reference port suites и lifecycle/quality coverage. Внешний suite ожидаемо exit 1 на C7/C8 assertions; компиляция успешна. C1–C6 closure cases PASS, independent SQLite BeforeCommit rollback и AfterCommit unknown outcome с close/reopen снова PASS. Отдельный `-run TestSourceCallbackClosure` PASS на всех четырёх store/callback cases. Полный лог внешнего suite сохранён в scratch.

`docs/reviews/checks-source-integrity.log` предоставлен orchestrator как успешный make validate (format/vet/tests/race/strict lint0/both examples); reviewer не выдаёт это за собственный запуск make validate. Strict lint configuration не менялась.

## Границы аудита

macOS arm64, CGO, Go 1.26.5. Повторно проверены изменённые source/read/lineage/acceptance/budget/wire/search пути; просмотрены затронутые store atomicity/replay/managed purge/late-job/ACL/temporal/retention contracts. Fresh normal/race tests выполняют остальные существующие fault cases. Новых подтверждённых CAS/atomic rollback/replay/managed purge race/field profile нарушений сверх C7/C8 в этом раунде нет. Отсутствие новых находок в этих областях не доказывает отсутствие ошибок.

Не проверялись физические disk failure/forensic erasure, внешние IAM lease protocols, arbitrary consumer codecs/provider side effects, production load или межпроцессные stress сценарии вне штатной SQLite conformance. Repro source использует privileged store corruption только для проверки fail-closed runtime contracts; не предполагается публичный unauthenticated доступ к Store.
