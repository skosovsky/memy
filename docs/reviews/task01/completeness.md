# Проверка полноты задачи 01

Дата: 2026-10-05. Независимый reviewer не участвовал в реализации; implementation не менялся. Источники требований: `.cursor/task/README.md` и `.cursor/task/01-identity-and-decision-contracts.md`. Из review исключён существующий пользовательский diff Makefile. Фактический source, schemas и результаты запусков имеют приоритет над историческими заявлениями.

Итоговый verdict: **23/23 обязательств = 100%**. Проверены все 8 пунктов работы, 8 критериев приёмки и 7 общих правил README, включая исключения scope в R07. Открытых gaps нет. Частично выполненные и непроверенные обязательства не засчитывались; ранее найденные G1/G2 закрыты повторной проверкой фактического кода и regression tests.

## Матрица всех обязательств

| ID | Требование | Evidence file:line / проверка | Результат |
| --- | --- | --- | --- |
| W01 | UTF-8, пустота/размер/NUL, exact equality без normalization/case/trim, literal U+FFFD | store.go:18,38; docs/design.md:427; identity_decision_test.go:19,85 | Выполнено |
| W02 | Одинаковая валидация публичного ввода и persisted state; invalid до записи, corrupted wire ErrSchema | engine.go:79,131,388,433,484,521,586; wire.go:92,118; identity.go:57; identity_decision_test.go:40,227; identity_decision_test.go:256 | Выполнено: validPurge проверяет каждый Records ID |
| W03 | Все framework IDs в digest/lookup/selectors/receipts; payload/reference только consumer codecs | wire.go:180,195; validation.go:19,129,138; forget.go:190; recall_state.go:41; engine.go:141,154; identity.go:31; identity_decision_test.go:256 | Выполнено |
| W04 | Initial decision во всех mode, exact related revision, отделение Authority; atomic вместе с revision/receipt | lifecycle.go:175,220; commit_state.go:189,228; reconciliation.go:25; identity_decision_test.go:105 | Выполнено |
| W05 | Один typed authorized путь Get/history/example, без общего event/audit service | engine.go:543,578,610; lifecycle.go:175; examples/lifecycle/main.go:160 | Выполнено |
| W06 | Initial decision immutable; state transitions recorded-time; нет конфликтующих policy fields | engine.go:689; reconciliation.go:53; wire.go:48,61; identity_decision_test.go:141 | Выполнено |
| W07 | Basis content-bearing, forget history/managed projection; content-free permanent receipts; нет CommitRequest ledger | revocation_selection.go:80; commit_state.go:244,258; identity_decision_test.go:167; docs/migration.md:16 | Выполнено |
| W08 | Wire/schema v2, strict fields, docs/examples согласованы; старые структуры удалены; v1 не читается | store.go:34; wire.go:110; wire_shape.go:49; schemas/document-v2.schema.json:14; schema_test.go:16; store/sqlite/sqlite.go:93; schema_test.go:19; identity_decision_test.go:305 | Выполнено: executable custom format совпадает с runtime |
| A01 | Conformance двух Store отвергает malformed scope во всех компонентах без мутаций | conformance/store.go:30,336; store/memory/memory_test.go:11; store/sqlite/sqlite_test.go:59; общий test/race PASS | Выполнено |
| A02 | Property/fuzz scope tuple keys/round-trip; кириллица/emoji/quotes/delimiters/U+FFFD | identity_decision_test.go:19; отдельный fuzz 45 858 inputs, PASS | Выполнено |
| A03 | Remember→Accept→Commit допустимых Unicode IDs и reject malformed input/persisted wire | identity_decision_test.go:40,85,212; malformed_runtime_test.go:13 | Выполнено |
| A04 | SQLite reopen всех Append/Duplicate/Supersede/Conflict; exact decision и две policies | identity_decision_test.go:105–138; общий test/race PASS | Выполнено |
| A05 | Exact operation replay без revision/Basis replacement; другой input ErrConflict | identity_decision_test.go:127–136; commit_state.go:27,69; wire.go:180 | Выполнено |
| A06 | Historical recorded-time без future decision; unauthorized/cross-scope audit недоступен | identity_decision_test.go:141; engine.go:79,610,651,689; engine_test.go:164,249,414; selectors_test.go:11 | Выполнено |
| A07 | Forget удаляет Basis/history/managed projections; raw trusted fixture documents и public reads проверены | identity_decision_test.go:167–224; revocation_selection.go:86–102; recall_forget_test.go:276 | Выполнено |
| A08 | Executable schemas/schema tests/acceptance matrix/lifecycle example; explicit incompatible version | schema_test.go:16,56,124; docs/acceptance.md:197; store/sqlite/sqlite_test.go:174; examples/lifecycle/main.go:160; schema_test.go:19; identity_decision_test.go:305 | Выполнено |
| R01 | Clear break, без aliases/migrator/dual paths/reset; clean storage/host-owned import и актуальный revoke | удалён schemas/*-v1; docs/migration.md:1–20; store/sqlite/sqlite.go:74–103; TestRejectIncompatibleSchema | Выполнено |
| R02 | Contract-first конечный контракт: states/invariants/errors/ownership/cancellation/concurrency/design/acceptance/schema | docs/design.md:22–111,286–319,427; docs/acceptance.md:197; wire_shape.go:13; общие проверки | Выполнено по конечному состоянию; порядок промежуточных edits из diff не доказывается |
| R03 | BYOT/универсальность, core не навязывает доменные claims/payload/reference/query/authority | lifecycle.go:39,48,53,130,175; recall.go:43,185; engine.go:15; root deps stdlib-only | Выполнено |
| R04 | Нет agent loop/planner/scheduler/IAM/tokenizer/SDK/vector DB/network/обязательного telemetry/eval core | root go list -deps содержит только stdlib; go.mod:1; offline lifecycle PASS | Выполнено |
| R05 | Proposal ≠ accepted truth/permissions/instructions; canonical revalidation/exact scope/source/lineage/time/retention/revoke/no resurrection | engine.go:245,283,610,716; commit_state.go:31–65; managed_race_test.go:17; deadline/lease/lineage tests PASS | Выполнено |
| R06 | AAA/adversarial/fault injection/реальные adapters/common conformance/race/vet/format/offline | identity_decision_test.go Arrange/Act/Assert; durable_lifecycle_test.go:37,91; conformance/store.go:22; запуски ниже | Выполнено: повторный финальный race/vet/format PASS |
| R07 | Код+schemas+conformance+tests+examples+docs, без neighboring libs/release/IAM/signatures/truth classifiers/federation/event bus/old format | git status/diff; scope paths внутри memy; root graph; lifecycle API; old schemas удалены | Выполнено |

## Закрытие найденных gaps

**G1 закрыт — executable identity schema согласован с runtime.** Ранее выявленное расхождение: `schemas/document-v2.schema.json:116` задаёт maxLength в codepoints вместо 1024 bytes; pattern `\\S` не соответствует Unicode `strings.TrimSpace`. До исправления custom format следующие экземпляры schema принимает, runtime отклоняет: Tenant=600×«я» (1200 bytes), Tenant=NBSP. `revision-ref.record_id` (`:159`) принимает NUL и 1025 ASCII, потому что был ограничен только minLength. Repro `/tmp/memy-completeness-schema.go` напечатал четыре успешных schema validation при runtime-invalid IDs. Финальное исправление: schema_test.go:19 регистрирует memy-identifier через публичную Scope.Validate, AssertFormat включён; соответствующие schema identifier fields и items Records используют format. Docs/design.md документирует обязательную consumer registration. TestExecutableSchemaIdentityAssertions проходит для empty/NUL/ASCII overflow/Unicode byte overflow/NBSP.

**G2 закрыт — persisted PurgeBatch.Records валидируются как framework IDs.** До исправления: `identity.go:31–40` не рассматривает field Records как identifier; `validation.go:145–166` проверяет batch operation/scope/selector и sinks, но не Records. В копии source `/tmp/memy-completeness-repro` isolated internal TestCompletenessPurgeIdentity подтвердил: `decodeDocument` принимает Records=[""], ["a\\x00b"], [1025×x]. Все три значения `validIdentifier` отвергает. Финальное исправление validation.go:152–156 проверяет каждый Records элемент через validIdentifier; TestPurgeReceiptRejectsMalformedRecordIdentity подтверждает ErrSchema на публичном replay persisted corrupted purge для всех пяти boundary cases. Targeted и общий race PASS.

## Выполненные проверки

- Прочитаны task README и task 01 целиком; diff/source/schema/API/docs/examples проверены независимо.
- `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test ./...`: PASS всех пакетов.
- `go vet ./...` с теми же cache paths: PASS.
- `gofmt -l` всех Go файлов: пустой вывод.
- `go run ./examples/lifecycle`: PASS, напечатаны resolver/authority/Basis, reopen recall и forget retry complete.
- `go test -run '^$' -fuzz '^FuzzScopeIdentity$' -fuzztime=3s -parallel=2 .`: PASS, 45 858 inputs.
- Финальный `go test -race ./...` и `go vet ./...` после всех repairs: PASS. Повторный gofmt -l: пусто. До исправления конфликтный test setup действительно падал; финальный setup проверяет ErrStaleInput для managed write conflicted lineage, сохраняя core restriction, а Forget/raw checks выполняет для всех четырёх режимов.
- Прочитан `/tmp/memy-task01-quality.json`: baseline/exact_dedup/domain_merge accepted; semantic_merge rejected, negation/interval loss выявлены; memory/v2, no credentials. Это regression smoke, не приёмка задачи 04.
- Root `go list -deps .`: только standard library graph.

## Ограничения verdict

Completeness измеряет выполнение перечисленного контракта, не доказывает отсутствие неизвестных дефектов. Исторические audit snapshots не использованы как свидетельство новых проверок. Итоговая chronology spec-first не восстанавливается по конечному diff; проверены конечные executable контракты. Reports не являются новым API или расширением task scope.
