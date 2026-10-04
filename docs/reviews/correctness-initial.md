# Независимая приёмка memy: корректность, round 1

Дата: 2026-10-04. Ревьюер: независимый adversarial correctness agent.

**Вердикт:** приёмка по корректности не пройдена. Подтверждены 6 дефектов: 2 P1 и 4 P2. Зелёные штатные тесты не покрывают воспроизведённые случаи. Проверка полноты другого ревьюера не использовалась.

## Проверенный snapshot и неизменность

- `docs/reviews/snapshot-initial.json`: aggregate SHA256 **bca4d7119843f283d89f1672606016031e54e94b8f800c10755d9f3cf6be741d**.
- Проверены все 84 перечисленных файла: размер и SHA256 каждого совпадают; агрегат пересчитан как SHA256 compact JSON списка files с sort_keys=True. Повторная проверка после repro также не обнаружила изменений.
- Production Go, source tests, lint config, acceptance matrix, README/design не менялись. Создан только этот отчёт; весь внешний repro-модуль и лог находятся в `/private/tmp/memy-correctness-round1`.
- Прочитаны исходное задание `.cursor/tasks/task1.md`, `docs/design.md`, wire schemas `schemas/*-v1.schema.json`, основные lifecycle/recall/consolidation/retention/revocation paths, memory/SQLite stores и reference adapters.

## Воспроизведение

Полный AAA repro: `/private/tmp/memy-correctness-round1/audit_test.go`; Go module содержит replace на этот исходный snapshot. Лог: `/private/tmp/memy-correctness-round1/repro.log`.

```sh
cd /private/tmp/memy-correctness-round1
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -mod=mod -v -count=1 ./...
```

Exit 1 здесь ожидаемый результат **контрактных assertions**, выявляющих дефекты, а не ошибка запуска/сборки. Семь failing top-level tests воспроизводят шесть дефектов; `TestSQLiteFaultRecoveryAudit` с двумя fault boundaries проходит.

## C1 — P1: текущая выдача и transitive lineage не проверяют source port

**Контракт:** design «Canonical lineage»: reads проверяют всю dependency chain, включая exact source revision; «Recall» требует sources/lineage revalidation; «Consolidation» обещает invalidation при source revision change. Sources port имеет `Validate` для существования/current revision/evidence; unavailable не должно трактоваться как valid evidence.

**Code paths:** `engine.go:625` canonicalCandidate, `consolidation.go:85` snapshotCandidate (используется Recall), `recall_state.go:128` projectionRecord и `engine.go:715` liveLineageInput. Они проверяют canonical revocation/expiry/retention, но не вызывают `Sources.Validate` для собственных sources записи. В отличие от них `engine.go:273` validateSourceInputs используется для новых proposals. Transitive lineage при acceptance/commit также ограничивается liveLineageInput.

**Минимальный repro:** принять и commit record с `source/r1`; зарегистрировать `source/r2` в том же Registry (или Remove source, или Fail source registry); вызвать обычные Get, Snapshot, Recall и Project без исторических time predicates. На memory **и** SQLite все четыре операции возвращают старый payload без ошибок:

```text
Get="secret"/<nil> Snapshot=1/<nil> Recall=1/<nil> Project="secret"/<nil>
```

Тест: `TestSourceChangesAfterCommit`, 6 subcases (2 stores × revision/remove/outage).

**Усиленный repro:** `TestDerivedLineageIgnoresCurrentSourceRevision`: два inputs основаны на source/r1, Registry уже содержит r2. Semantic provider выдаёт suggestion с актуальной **direct** source/r2 и mandatory lineage на старые inputs. Consolidate, Accept и Commit успешно создают derived record: `committed=true`. Старые r1 inputs не были повторно проверены через Sources.

**Последствие:** outage или изменение evidence не останавливают текущую выдачу; можно принять новый summary из inputs, чьи источники больше не удовлетворяют выбранному host validator. Это обход заявленного fail-closed evidence gate, не потеря одной лишь аннотации provenance.

**Граница temporal semantics:** repro не использует RecordedAsOf/ValidAsOf и не требует удалять историческую provenance. Host вправе разрешить retained immutable r1 при историческом чтении — для этого его Sources.Validate должен подтверждать именно эту revision. Здесь reference Registry явно ответил бы stale/unavailable, однако core вообще не обращается к нему. Исправление должно согласовать валидность exact revision и temporal reads, сохранив отдельную present privacy revocation.

## C2 — P1: обычное expiring acceptance отклоняется после JSON roundtrip

**Контракт:** отдельно принятое host acceptance связывает digest/revision/actor/policy/expiry; неизменённое и ещё действующее acceptance пригодно для Commit. Codec/storage metadata не должны менять его смысл.

**Code paths:** `commit_state.go:121`: `stored == requested`; `proposal_state.go:80` также использует struct equality для повторного Accept. В Acceptance есть `time.Time ExpiresAt`. JSON persistence убирает monotonic clock metadata, а Go `==` сравнивает внутреннее представление времени, включая monotonic/location.

**Минимальный repro:** Authority возвращает стабильный expiry `time.Now().Add(time.Hour)`; используется SystemClock. Remember → Accept возвращает действующий Acceptance. Передать **этот точный объект** в Commit. На memory и SQLite: `memy: stale acceptance`, хотя actor/policy/digest/revision/deadline не изменились. Тест `TestAcceptanceMonotonicExpiry`.

**Последствие:** стандартный production способ задать TTL authority (`time.Now().Add`) делает нормальный accepted commit невозможным. Это не настоящее изменение политики. Сравнение expiry должно опираться на равенство instant, либо API обязано нормализовать представление до возврата и хранения.

## C3 — P2: Get принимает phantom revision при несовпадении record key/envelope

**Контракт:** canonical revision identity явно связана с persisted history; runtime corruption должна приводить к schema error. Snapshot уже проверяет binding `entry.Key == revisionKey(stored.ID, stored.Revision)`.

**Code path:** `engine.go:630` canonicalCandidate проверяет ID/scope, но не key/revision binding. `consolidation.go:96` snapshotCandidate содержит необходимую проверку.

**Fault injection:** commit revision 1; через trusted Store.Update изменить только `data.revision` record-history envelope на 7, оставив физический ключ `/.../00000000000000000001`. Proposal digest не затронут. Тест `TestRecordRevisionKeyCorruption`.

**Факт:** `Get revision=7 payload="fact" err=<nil>`, тогда как Snapshot для того же состояния возвращает ErrSchema (`memy: unsupported schema`).

**Последствие:** Get подтверждает несуществующую canonical revision и расходится с snapshot/recall validation. Host может использовать phantom ref в следующих CAS/lineage решениях. Требуется строгая проверка history key binding и в Get.

## C4 — P2: runtime принимает unknown case-alias поля, запрещённые JSON Schema

**Контракт:** design «Errors, cancellation and migration»: every field required, unknown fields fail closed. `schemas/document-v1.schema.json` устанавливает `additionalProperties: false`; разрешено `schema`, но не `SCHEMA`.

**Code paths:** `wire_shape.go:43` requiredJSONObject проверяет наличие обязательных полей, не запрещая остальные. `wire.go:99` / `wire.go:111` полагаются на json.Decoder.DisallowUnknownFields, который сопоставляет struct fields без учёта регистра. Canonicalization сортирует uppercase alias перед lowercase original, после чего original overwrites alias и semantic checks проходят.

**Fault injection:** к корректному history document добавить root `"SCHEMA":99`, сохранив `"schema":1`. `TestSchemaRejectsCaseAliasUnknownField` получает успешный Get с payload и revision 1.

**Последствие:** executable schema и runtime decoder допускают разные wire domains; неизвестные поля, способные обозначать иную версию/профиль, тихо игнорируются вместо reject. Не утверждается успешное повышение прав; подтверждён именно fail-closed schema contract breach. Та же стратегия applies к case aliases nested struct fields.

## C5 — P2: consolidation byte budget не ограничивает возвращаемое предложение

**Контракт:** Budget документирован как предел input/output bytes, design обещает budget exhaustion без изменения originals. Публичного определения «только consumer payload bytes» нет.

**Code paths:** `consolidation.go:243` считает InputBytes только по encoded payload; `consolidation.go:319`–325 считает OutputBytes только по encoded payload. Evidence, Sources/Reference, Losses и Uncertainties провайдера не входят в расчёт, хотя сериализуются и сохраняются. Стампинг core lineage/sources также происходит после этого расчёта.

**Минимальный adversarial provider:** inputs `"one"`, `"two"`; output payload `"x"`, Evidence = 1 MiB, достаточная Utility, нулевая CostUnits. OutputBytes = 32. `TestConsolidationOutputBudget`:

```text
outputBudget=32 evidenceBytes=1048576 proposals=1 err=<nil>
```

**Последствие:** недоверенный provider обходит лимит объёма результата, и core persist-ит большой review content при маленьком budget. Это может расширить storage/serialization cost. Не заявляется контроль памяти внутри произвольного provider: даже при исправлении библиотека не может отменить уже сделанную provider allocation. Требуется определить и исполнить byte accounting для фактически переданного/принятого content. Если намеренный контракт — payload-only, это должно быть явно названо и supplemented envelope bound; нынешний общий bytes limit такого исключения не описывает.

## C6 — P2: reference index выдаёт Complete=true при заведомом visibility lag

**Контракт:** task MEM-004 различает canonical commit и search visibility; empty result не подтверждает отсутствие только что принятого знания; result включает completeness status. Design запрещает заменять unknown completeness пустой выдачей.

**Code paths:** `reference/index.go:135` устанавливает minimumSatisfied=true без token; `reference/index.go:171` всегда выдаёт coverage complete. `recall_state.go:14` переводит это в RecallResult.Complete=true.

**Минимальный repro:** commit canonical record → Stage через WithDerivedWrite → не Acknowledge → Recall без minimum token. Тест `TestPendingIndexCoverage`:

```text
records=0 Complete=true coverage=[{Backend:index Status:complete MinimumSatisfied:true}] err=<nil>
```

**Последствие:** adapter знает про pending revision, но публичный результат certifies completeness пустого поиска. Token+deadline путь отдельно реализован корректно и этот finding его не оспаривает. Без token возможно обещать snapshot только visible index; тогда статус должен явно объявлять более слабую гарантию (как Eventual adapter), а не выглядеть canonical absence/completeness. Внешнего watermark для не staged canonical commits индекс не имеет — такую completeness он также не может устанавливать самостоятельно.

## Выполненные проверки и пределы вывода

- `env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test ./...` — pass (первый запуск с cache).
- Та же команда с `-count=1` — pass, свежий запуск всех штатных tests, включая обе store conformance suites, lifecycle example и quality tests.
- `go test -race ./...` — pass (cache), затем `go test -race -count=1 ./...` — pass, свежий race run.
- Внешний AAA suite `go test -mod=mod -v -count=1 ./...` — ожидаемый fail на перечисленных assertions, compilation и execution успешны. При повторном запуске все findings воспроизвелись.
- Отдельные active SQLite BeforeCommit и AfterCommit fault injections, close/reopen: before-commit rollback убирает **и** record **и** receipt; after-commit даёт ErrUnknownOutcome и сохраняет обе записи — pass. Это подтверждает только проверенные transaction boundaries, не любые физические disk failures.
- Изучены managed-write/purge exclusion, epoch checks late extraction, scoped authority/field profile rejection, caller-policy input cloning, replay ledger/digest, valid/recorded-time transition logic, retention/sweep/reintroduction и mixed search coverage. Штатные adversarial tests этих веток также прошли свежие normal/race runs. Независимых новых дефектов там сверх C1–C6 не подтверждено; это не доказательство их отсутствия.
- `docs/reviews/checks-initial.log` задан orchestrator как evidence успешного make validate (format/vet/tests/race/strict lint0/both examples); независимый reviewer не перезапускал make validate и не приписывает его себе.
- macOS arm64, CGO, Go 1.26.5. Проверка host IAM lease/distributed atomicity, физического erasure SQLite journals/backups, arbitrary provider implementation и production workload performance не проводилась. Никаких commits/releases/publication.
