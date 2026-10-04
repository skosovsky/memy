# Independent correctness acceptance — round 5

Вердикт: **C12 закрыт; новый C13 P2 подтверждён на memory и SQLite.** Round5 завершён для remediation. Это ограниченная adversarial проверка описанных ниже случаев, а не доказательство отсутствия всех ошибок.

## Проверенный snapshot и независимость

Frozen `docs/reviews/snapshot-reviewed-lineage.json`: 93 included файла, aggregate SHA256 `863c9770fa22308bea10300150d0b7afdd846b6e130abd0c10d3daa7f097ce0f`. До проверки и после записи отчёта сверены SHA256 и размеры каждого файла, затем пересчитан SHA256 sorted compact JSON inventory. Source/tests/design/matrix/lint не изменялись. Scratch находится только в `/private/tmp/memy-correctness-round5`; `docs/reviews` исключены из inventory.

Completeness reports не читались, координации с другим reviewer не было. C13 найдена независимо этим correctness reviewer и сразу сообщена primary; primary приняла дефект. Отчёт фиксирует поведение исходного frozen snapshot, до remediation.

## C12: закрытие подтверждено

Contract: `docs/design.md:410` требует сохранения каждого exact RevisionRef из digest-bound accepted Proposal.Lineage в canonical Record.Lineage. Дополнительные dependencies допустимы; tombstone имеет отдельный content-free контракт.

Implementation: `validation.go:70–80` проверяет exact subset через `slices.Contains`; проверка выполнена после валидности embedded proposal/digest и refs. Общий `decodeDocument` вызывает `validDocument`, поэтому malformed canonical metadata не проходит обычное чтение или transitive traversal.

Внешний исходный adversarial repro `TestRecordCannotDropReviewedMandatoryLineage` прошёл на обеих stores; в round5 assertion усилен до `errors.Is(err, ErrSchema)` и пустого payload. Fixture сохраняет независимый fresh direct source, удаляет только runtime Lineage в history/head при неизменных reviewed proposal/digest, затем отзывает источник mandatory ancestor. Get возвращает schema error, контент не выдаётся.

Новый независимый positive case `TestDuplicateAdditionalDependencyIndependent` также прошёл на обеих stores:

- proposal требует a1, Duplicate reconciliation добавляет b1 и повторно указывает a1;
- canonical lineage содержит обе exact dependencies без лишнего повторения a1; commit и exact receipt replay успешны;
- Get, Snapshot, Recall с acknowledged managed index и Project доставляют корректный payload; Snapshot содержит три canonical records;
- независимое обновление b до revision2 делает derived недоступным с ErrStaleInput и пустым payload. Дополнительная Duplicate dependency остаётся обязательной.

Штатный `TestCanonicalCannotDropOrReplaceReviewedLineage` также выполнен свежими source tests и race tests: drop/RecordID substitution/revision substitution × memory/SQLite; Get/Snapshot/Recall/Project отвергают envelope с ErrSchema, projector не вызывается, после удаления ancestor source content по-прежнему отсутствует. Отдельная ветка revoked tombstone не требует reviewed subset и существующие lifecycle/purge tests проходят.

## C13 — P2: managed write запускается после expiry lineage во время финальной авторизации

**Статус:** подтверждён независимо, обе stores. Внешний repro: `/private/tmp/memy-correctness-round5/audit_test.go`, `TestManagedWriteExpiryDuringReauthorization` (AAA); точный failing output в `pinned-repro.log`.

Минимальная последовательность:

1. Clock = `2026-06-01T00:00:00Z`. Retention возвращает стабильный policy v1 с deadline `2026-06-01T01:00:00Z`; canonical input revision1 успешно committed.
2. Получен действующий EpochFence. Authority всегда возвращает Allowed с теми же actor/scope/policy; после arm только второй ActionRead check внутри WithDerivedWrite переводит Clock ровно на deadline. Исходная авторизация и lineage validation проходят до deadline. Это детерминированная fault injection задержки host I/O, без concurrency scheduler.
3. WithDerivedWrite вызывается с exact input1 и callback, который фиксирует своё исполнение и возвращает nil.
4. **Факт:** callback исполняется, WithDerivedWrite возвращает nil; Clock уже равен deadline. Для memory и SQLite одинаковый output:

```text
expired lineage reached managed callback: called=true err=<nil>
now=2026-06-01 01:00:00 +0000 UTC
 deadline=2026-06-01 01:00:00 +0000 UTC checks=2
```

**Ожидание/assertion:** `errors.Is(err, memy.ErrStaleInput)` и `called == false` перед новым внешним managed effect. Equality с deadline считается expired (`now.Before(deadline)` false), а не ещё разрешённым instant.

**Code path:** `forget.go:145` выполняет validateReadLineage; её callback-free deadlineGate находится в `engine.go:716`. Затем `forget.go:148` вызывает reauthorize — это новое синхронное host I/O после clock sample. `forget.go:151` сразу вызывает fn без повторного deadlineGate. Transaction/epoch защищают от canonical revoke, но не останавливают Clock во время host call.

**Контракт и последствия:** managed boundary допускает только живую exact lineage; retention lifetime не продлевается host reauthorization (`docs/design.md:243–249`), итоговая проверка deadlines должна следовать host I/O (`docs/design.md:377–385`). Конкретное нарушение — разрешён новый managed index/projection artifact на обязательном input, который уже истёк до callback. Canonical delivery всё ещё может скрыть artifact и последующий Sweep может его удалить; repro не утверждает bypass обычного Get/Recall и не обещает отмену уже начавшегося external effect. Ошибка расположена перед началом callback, где отказ ещё может предотвратить effect.

**Направление исправления:** callback-free traversal/clock gate после reauthorize непосредственно перед fn. Не приписывать этому проверку срока, истёкшего уже во время fn, или rollback внешнего эффекта. Исправление в frozen snapshot не вносилось.

## Выполненные проверки

Toolchain был закреплён явным PATH `/opt/homebrew/Cellar/go/1.26.5/libexec/bin`; проверено `go version go1.26.5 darwin/arm64`. Общая среда: `GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local`.

- Source `go test -count=1 ./...`: exit0.
- Source `go test -race -count=1 ./...`: exit0.
- Внешний модуль `go test -count=1 -v ./...`: exit1 **только** `TestManagedWriteExpiryDuringReauthorization` на memory и SQLite; все остальные top-level cases прошли, включая original C12 и positive Duplicate case. Лог: `/private/tmp/memy-correctness-round5/pinned-repro.log`.
- Предыдущие внешние adversarial cases C1–C11 исполнены заново: current source replacement/remove/outage, direct/transitive source reads/accept/commit, post-rank/project source change, monotonic acceptance expiry, history physical revision key, exact wire fields, numeric byte shape, invalid date-time lexical forms, full output budget with invoked provider, pending index coverage, read/accept/dependency-commit expiry during I/O, initial authority lease expiry, prospective transitive target rejection, multi-record deadline crossing, final-gate reconcile rollback with identical retry/replay, SQLite beforecommit rollback/aftercommit unknown outcome + recovery. Они прошли в рамках текущих fixtures.

Initial команды через default Homebrew Go были прерваны после сообщения primary, что default теперь Go1.27.1; они не использованы как evidence. Все результаты выше получены повторно с pinned1.26.5. Первая версия нового positive fixture случайно повторно использовала Remember operation ID для Commit и получила ожидаемый ledger conflict; fixture исправлен на отдельный commit operation ID до финального запуска, production defect из этого не заявлен.

`docs/reviews/checks-reviewed-lineage.log` — supporting primary make validate, не замена независимым запускам; собственный make validate/vet/strict lint/examples runtime отдельно не повторялся. Source tests компилируют и исполняют имеющиеся example tests, но это не утверждение полного повторения всех primary DoD команд.

## Границы аудита

Проверены изменённый subset invariant и позитивные extras, общая malformed-record ветка, ранее найденные fail-closed paths и соседняя managed-write deadline boundary. После подтверждения C13 поиск новых случаев не расширялся по поручению primary; текущий pinned набор доведён до конца. Не проводились exhaustive interleaving/model checking, fuzzing всех schemas, proof arbitrary privileged rewrite integrity или проверки всех сторонних адаптеров. Зелёные тесты не доказывают полноту пространства состояний. Для снятия C13 нужен следующий frozen snapshot и независимое закрытие original repro.
