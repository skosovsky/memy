# Независимое correctness review задачи 01

Дата: 2026-10-05. Роль: независимый reviewer без изменений implementation. Проверены README задания, 01-identity-and-decision-contracts.md и окончательный незакоммиченный diff; пользовательский Makefile исключён.

**Итоговый verdict: ошибок в проверенном объёме не обнаружено.** Все три подтверждённых замечания исправлены реализацией и независимо перепроверены. История findings ниже сохраняет evidence, но открытых findings нет.

## Закрытые findings

### P1 — Basis сохранялся в related transition после Forget

Исходное место: `reconciliation.go` при добавлении transition с `Basis: policy.Basis`; `forget.go:250` выбирает зависимости по Proposal/Lineage.

Reproduction: old revision 1, отдельная new revision 1 с Conflict, related=[old/1], Basis="REVIEW-SENSITIVE-BASIS"; Forget(new). Первоначально получено PurgeComplete records=[new], но old head и record/old/1 сохраняли Basis в transitions. Ошибка сообщена родителю сразу.

Исправление: `reconciliation.go:74` хранит content-free `Decision: newRef`, без копии Basis/policy. Независимый `TestReviewForgetRelatedBasis` проверяет все живые persisted documents после Forget(new): PASS. Обновлённый behavioral test Forget покрывает Append/Duplicate/Supersede/Conflict, surviving related records и удаление Basis. Для Conflicted lineage managed write корректно запрещён ErrStaleInput; fixture проверяет это ограничение.

### P2 — Record IDs в persisted purge receipt обходили identity validation

Исходное место: `validation.go:145`, `identity.go:33`.

Reproduction: Remember → Accept → Commit(fact), Forget(fact), privileged fixture заменить `purge.data.batch.records` на `["x\u0000y"]`, повторить исходный Forget. Первоначально возвращалось `state=complete records=["x\x00y"] err=<nil>` вместо ErrSchema. Внешняя эскалация привилегий не доказана.

Исправление: `validation.go:152` проверяет каждый Records ID; `identity.go:33` применяет полный identifier contract к Records elements. Schema records items использует memy-identifier. Независимый `TestReviewMalformedPurgeRecordIDs` с ожидаемым ErrSchema: PASS. Добавленный runtime test также покрывает empty, NUL, oversized ASCII/Unicode и Unicode whitespace.

### P2 — Executable schema не исполняла контракт идентификаторов

Исходные места: `schemas/document-v2.schema.json` authority_policy_version/reconciliation.policy_version, `schema_test.go:19`.

Reproduction: schema принимала authority_policy_version `"x\u0000y"`, `"  "` и 513 символов `я` (1026 bytes), а Get возвращал ErrSchema. Одного неизвестного custom format либо стандартного maxLength недостаточно: maxLength считает code points, контракт — bytes.

Исправление: custom memy-identifier формат распространён на identifier fields и зарегистрирован в executable validator через точную Scope validation с AssertFormat (`schema_test.go:19`). Consumer requirement документирован в design/migration. Независимый `TestReviewSchemaRuntimeIdentityMismatch` адаптирован к reject с обеих сторон: все три случая PASS. Добавленный TestExecutableSchemaIdentityAssertions проверяет positive/negative identity corpus.

## Evidence и проверки окончательного snapshot

Независимые repro находятся только в `/tmp/memy-correctness-test.go`; overlay `/tmp/memy-correctness-overlay.json` подключает их без изменения source. Команда: `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -overlay /tmp/memy-correctness-overlay.json -run '^TestReview(ForgetRelatedBasis|SchemaRuntimeIdentityMismatch|MalformedPurgeRecordIDs|ValidSurrogateWirePair)$' -v .` — PASS всех четырёх. Дополнительный positive test заменяет literal emoji в wire на корректную escaped UTF-16 surrogate pair, Get сохраняет exact emoji ID: PASS.

- Независимый полный `go test ./...` окончательного кода — PASS.
- Оба Store `TestConformance` отдельно — PASS, включая malformed scope identity.
- FuzzScopeIdentity: 3 секунды, 2 workers, 26 805 executions — PASS; кириллица, emoji, кавычки, разделители, U+FFFD, Unicode spellings и malformed seeds присутствуют.
- Последний parent `/tmp/memy-task01-race.log` прочитан: `go test -race ./...` — PASS, root 9.197s.
- Parent `/tmp/memy-task01-quality.json` прочитан: baseline/exact_dedup/domain_merge accepted, semantic_merge rejected. Harness outcome не использовался как доказательство полноты privacy/identity контракта.

## Проверенный scope и limits

Проверены scope Validate/Key и isolation, Unicode exact identity без normalization, public lifecycle identity gates и operationDigest, source/lineage/receipt IDs, metadata reflection boundary и opaque payload/reference bytes, strict raw UTF-8 и escaped UTF-16 surrogate scanner, required persisted JSON fields, document/database v2 rejection и отсутствие compatibility fallback, initial reconciliation persistence для четырёх modes, authority/resolver policy separation, replay digest binding, recorded-as-of selection, typed decision detachment, authorization gates Get/Recall/Project и Forget history/tombstone/managed cleanup.

Выборочные существующие behavioral tests identity/reopen/replay/history/forget/malformed persisted input и весь package suite прошли. Review включает исходники инвариантов, а не только зелёные tests. Implementation не изменялась reviewer; сохранён только этот отчёт. Продолжительная fuzz campaign, forensic SQLite/WAL erasure и unmanaged consumer responses вне проверенного объёма. Verdict ограничен указанным scope и не является доказательством отсутствия любых будущих ошибок.
