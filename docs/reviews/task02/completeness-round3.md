# Task 02 — независимая промежуточная приёмка полноты, round 3

**80% — 8/10 ST-требований; финальная приёмка не выполнена.**
ST denominator и полный source inventory из round1 сохранены. Implementation
не изменялась этим reviewer; benchmarks не запускались. Observed changed-source
hashes: `completeness-round3-snapshot.json`.

## Проверенные изменения

`persistSinkResult` теперь до любого shortcut читает exact bound persisted job
и вызывает `validateStoredPurge`. Проверка предшествует веткам different Chunk,
PurgeComplete и RevocationCommitted. `validateStoredPurge` проверяет
stage↔receipt binding и active operation ownership: незавершённый job требует
своего active pointer; completed job допускает отсутствие pointer либо unrelated
later active operation, но отвергает still-owned pointer. `readPurge` использует
тот же guard перед выдачей receipt. `advancePurge` и Sweep дополнительно сохраняют
соответствующие phase/receipt/ownership checks перед completed replay/adoption.

Это закрывает полноту ранее пропущенной retry branch: первоначальная проверка
перед external sink не заменяет повторную проверку после callback. Смена receipt
во время callback больше не может дать false Complete при job=ack и живом fence.

Новый permanent `TestPurgeRejectsCompletedReceiptDuringSinkCallback` создаёт
privileged metadata fault во время настоящего sink callback, требует ErrSchema,
не возвращает Complete и подтверждает оставшийся durable fence. Свежий
independent targeted race run прошёл. Test использует memory; общая branch в core
также проверяется вместе с SQLite independent Engine/fault/reopen fixtures.
Это не заявление, что untrusted sink способен сам менять raw canonical store:
raw Store остаётся privileged host boundary.

Documentation уточнена: root+private helpers имеют только standard-library
dependencies; transaction-local Authority/Sources/Retention/Clock/codec ports
должны быть bounded/local/context-aware, без remote I/O и recursion в Store.
Дорогие Ranker/Projector/merge/sink effects отделены от writer transactions;
core не обещает forceful callback termination или exactly-once effect.
Round2 wording mismatch по root imports закрыт.

## Текущая матрица

| Requirement | Зачёт | Изменение / остающаяся работа |
| --- | --- | --- |
| ST-01 | да | Transaction/CAS/ABA/shared conformance guarantees не изменены. |
| ST-02 | да | Bounded scan/cursor/cancel contract не изменён. |
| ST-03 | да | Addressed read/consolidation path не изменён, private structural evidence сохраняется. |
| ST-04 | да | Scoped callback isolation сохранена; locality obligations теперь сформулированы точнее. |
| ST-05 | да | Independent handles/Engine lifecycle idempotency и final fences; свежий cross-handle race included. |
| ST-06 | да | Correlation после callback теперь закрывает все shortcuts в persistSinkResult; новая permanent regression зелёная. |
| ST-07 | да | Durable proof/evidence architecture не изменена, prior fan-in and historical graph evidence remains relevant. |
| ST-08 | да | Sweep refuses incomplete job/complete receipt; fresh targeted race included. |
| ST-09 | нет | Текущая aftermatrix относится к предыдущему source snapshot. Нужны terminal results, повтор affected purge measurements на новом guard, актуальный after contention и итоговый сравнительный отчёт с hashes/условиями/censored boundaries. |
| ST-10 | нет | Final whole race/vet/format/examples и обе final independent reviews на одном окончательном source snapshot остаются pending. Targeted green не заменяет общий gate. |

Свежая команда:

```
GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -race -count=1 -run '^(TestPurgeRejectsCompletedReceiptDuringSinkCallback|TestIndependentSQLiteEnginesShareLifecycleIdempotency|TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen|TestSweepRejectsCompletedReceiptForUnfinishedPurge|TestPurgeContinuationRejectsCorruptFenceAndProgress)$' .
```

PASS; output сохранён `completeness-round3-targeted.txt`. Сохраняю только
промежуточный80% verdict: текущие исправления полны в проверенном scope, но
running/устаревшие performance и final checks не считаются выполненными.
