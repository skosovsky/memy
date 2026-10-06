# Task03 — независимая проверка корректности

Итоговый вердикт: **принято; ошибок в проверенном объёме не обнаружено**.
Все пять подтверждённых замечаний исправлены и проверены. Финальный полный suite
race завершён успешно на проверенном source snapshot. Открытых подтверждённых
ошибок и незавершённых обязательных проверок корректности нет.

Проверяющий агент не участвовал в реализации. Аудит охватывает core Recall /
RecallProjected, reference Index / weighted RRF / JSONPacking, контракт API,
conformance, миграцию, BYOT и adversarial callback scenarios.

## Закрытые замечания

1. **All-unavailable ошибочно возвращал visibility pending при Minimum.**
   В `recallCoverage` minimum проверялся до общей доступности. Исправление сначала
   определяет наличие доступного backend; все unavailable дают ErrUnavailable.
   `TestRetrievalBudgetUnavailableCoverageCannotReportAbsence` проверяет режимы
   без minimum и с minimum, включая отсутствие ErrVisibilityPending.

2. **Cancellation терялась при пустом child error result.**
   Composite валидировал coverage прежде context.Canceled/DeadlineExceeded и
   возвращал ErrInvalid. Исправление сохраняет cancellation/deadline до проверки
   metadata. `TestCompositeCancellationPrecedesChildMetadataValidation` покрывает
   пустой result, отмену внутри callback и joined visibility deadline.
   Независимый standalone backend, отменяющий context внутри Search, также
   подтвердил context.Canceled без ErrInvalid.

3. **Fixture ожидал прежний порядок backend coverage.**
   Первый независимый targeted race выявил failing assertion в
   `TestCompositePartialFailureRequiresExplicitDegradedMode`. Production порядок
   (`failed`, `index/v1`) соответствует stable identity order. Assertion исправлен
   и проходит race; fusion algorithm менять для этого не потребовалось.

4. **Final projected body мог сохранить устаревший State.**
   Независимый временный go-overlay probe воспроизвёл Active→Conflicted во время
   Select при IncludeConflicts=true: returned state=active, final canonical=conflicted.
   Исправление фиксирует projected State всех selected/omitted refs и возвращает
   ErrStaleInput при mismatch вместо изменения уже измеренного body. Независимый
   probe подтвердил исправление. `TestRecallProjectedRejectsChangedEligibleState`
   покрывает memory/SQLite × Select/Measure × selected/omitted. Historical AsOf
   сохраняет законный state; обычный Superseded revision остаётся скрытым.

5. **Provided backend raw score заменялся ranking score.**
   Composite пересоздавал SearchSignal.Score из Candidate.Score, теряя явно
   переданный raw evidence. Исправление сохраняет provided single signal.Score;
   отсутствие сигнала использует Candidate.Score. Distinct ranks вычисляются
   после dedup согласно контракту. `TestCompositePreservesProvidedRawScoreWithDistinctRanks`
   проверяет raw 99/-99, другие ranking inputs, duplicates, permutation, stable
   tie, distinct ranks и fallback. Contract описывает это поведение явно.

## Проверка финального снимка

Независимая команда после всех исправлений:

```sh
GOCACHE=/private/tmp/memy-task03-review-cache go test -race -count=1 -timeout=5m -run 'TestRecallProjected|TestComposite|TestIndex|TestJSONPacking|TestRetrievalBudget' ./...
```

**PASS** всех packages; root 6.541s. Проверены rejection до canonical decode
(DecodedDocs=0), finite/malformed metadata, unsupported bound, отдельные progress
stages, signal/payload mutation isolation, foreign/duplicate identities,
точная историческая projection, uint64 boundaries, exact/estimated receipt,
полный JSON body, oversized/all-omitted behavior и cancellation/marshal errors.
Source replacement, authority changes, expiry, Forget и cancel после Select/Measure
приводят к пустому ошибочному результату; concurrent Forget проходит при удержанном
policy callback. Revalidation включает omitted identities.

Независимо сверены **110 SHA-256** из `source-hashes.json`: mismatches=[].
SHA-256 manifest: `fab292eabe40114e3b0bedc5c6acda040f0cadb5606e30523d2de7b34bdcd83e`.
В финальном `final-checks.json` все exit=0: vet, lifecycle/quality/retrieval
examples, diff check и gofmt. Reference JSONPacking учитывает provenance,
projection envelope, coverage/progress и omissions; out-of-band receipt исключён
из JSON. BYOT подтверждён разными payload/source/authority типами и string /
structured query. Core не требует vendor SDK, tokenizer или embedding, RRF
находится в adapter и weights привязаны к backend identity.

## Финальная приёмка

Полный `go test -race -count=1 -timeout=20m ./...` после последнего production
изменения: **PASS**, exit=0, root 682.555s, все packages прошли. Проверены
`final-race.txt` и `final-race-result.json`; затем повторно сверены все 110 source
hashes — mismatches=[]. Финальные vet/format/три examples также PASS.
Прерванные промежуточные запуски не использованы как доказательство успеха.

Проверка корректности задачи03 завершена и принята на этом снимке. Поддержка
произвольных production adapters, remote billing и доменное качество retrieval
не входят в объём проверки и не обещаются этим отчётом.


Перед коммитом отдельно проверена финальная status-only documentation delta:
RT03-01..08 помечены verified, heading/intro согласованы с acceptance, добавлены
ссылки на два независимых review и final race result. Локальная task03 содержит
восемь выполненных checks и accepted/commit pending. После обновления только
acceptance documentation повторно сверены все 110 hashes — mismatches=[].
Go source, runtime contract и ранее принятые проверки не изменены; окончательный
вердикт «принято, открытых подтверждённых ошибок нет» подтверждён.
