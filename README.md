# memy

Go-библиотека для lifecycle знаний: typed proposal → отдельное host acceptance →
conditional canonical commit → scoped recall → durable revoke и managed purge.

Payload, source reference, extraction input, query, projection output и
authenticated authority — типы потребителя. Core содержит небольшие envelopes,
ports и lifecycle; работает без сети, API keys и соседних ai-libs.
Знание остаётся данными. Запись о разрешении платежа не выдаёт разрешение tool,
а запись о процедуре не меняет исполняемый prompt.

## Запуск

Toolchain: Go 1.27.1. SQLite adapter требует CGO и C compiler; root package и
in-memory adapter используют только standard library. Driver pinned:
`github.com/mattn/go-sqlite3 v1.14.52`.

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
make lint
make test
make test-integration
make test-e2e
```

`make test` выполняет свежий race-прогон обычных тестов всех найденных модулей.
Integration и e2e запускаются отдельными целями с соответствующими build tags
и префиксами тестов; их timeout — 30 минут. Unit-тесты используют стандартный
timeout Go. Инструменты берутся из PATH; версии закреплены в CI.
`make fix` запускает go fix, форматирование и автоматические lint fixes.
Все команды используют GOWORK=off. См. [verification](docs/verification.md).

Offline quality protocol v2 проверяет два typed domain: preferences и знания о
процедурах/наблюдаемых результатах. Candidate, host review, фактические revisions,
canonical state и полный JSON-контекст оцениваются отдельно; итог вычисляется
после всех проверок. [Контракт, exit codes и mutation probes](docs/quality.md):
`go run ./examples/quality -out docs/quality-report.json`.
`go run ./examples/quality-integration` заменяет consumer ports и выполняет те же
checkpoints. Оба примера scripted; результат не является LLM benchmark.

Examples компилируются при `make test`; запускать их можно штатным `go run`.
`go run ./examples/lifecycle` запускает lifecycle.
Lifecycle выполняет полный offline сценарий на временном SQLite-файле:
extraction, acceptance одного proposal, commit, corrected revision, close/reopen,
canonical read в новой сессии, search visibility, typed projection и forget
со сбоем одного sink и восстановлением через bounded Sweep. Данные и файл удаляются после примера.
Никаких credentials не нужно.

`go run ./examples/retrieval` запускает отдельный offline пример: два backend
с разными ranks и масштабами scores, RRF, stale candidate, partial failure и
точный бюджет JSON-выдачи. Query — структура потребителя. Пример проверяет
контракт и размер выдачи, а не качество production retrieval.

## Типы и ports

```go
type Preference struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}
type SourceRef struct { URI string `json:"uri"` }
type AuthenticatedUser struct { ID string }

// Engine[Preference, SourceRef, AuthenticatedUser] принимает эти типы напрямую.
// Authority[AuthenticatedUser] проверяет права host identity.
// Sources[SourceRef] проверяет source identity/revision и typed reference.
// Extractor[string, Preference, SourceRef] создаёт предложения, не commits.
```

Рабочая сборка находится в [examples/lifecycle](examples/lifecycle/main.go).
Все host ports задаются явно через `memy.Config`: transactional store, authority,
source validation, clock, retention и versioned payload/reference codecs.
Опциональный список `Sinks` определяет managed deletion boundary. Опциональный `Reintroduction` разрешает
host-controlled повторный ввод после scope/source tombstone; по умолчанию
tombstones сохраняются бессрочно. Retired record IDs не переиспользуются.

`reference` содержит offline policy grants, source registry, clock, deterministic
index с manual acknowledgement, summary/cache sink, typed extractor/merge/
projection scripts, RRF composition и JSON packing. Это reference adapters,
не vendor integrations.

## Retrieval и бюджет выдачи

Каждый вызов `Recall` задаёт `SearchOptions.MaxCandidates` от 1 до 10 000
и отдельный `RecallOptions.Limit` для выбранных результатов. Search adapter
объявляет `BoundedCandidates`, возвращает не больше запрошенного количества
и отмечает намеренное усечение через `CandidatesTruncated`. Oversized result
отклоняется с `ErrBudget` до декодирования canonical records. Этот bound
ограничивает возвращённые metadata и работу core; внутренние расходы backend
определяет host.

`Coverage` описывает availability и index visibility; `ready` не обещает
полноту релевантных знаний. `MinimumSatisfied` относится к exact visibility
token. `RecallProgress` отдельно считает candidates, canonical filtering,
ranking omissions и truncation. Поле `RecallResult.Complete` удалено.

Candidate содержит отдельный optional ranking `Score` и typed `SearchSignal`:
backend identity, native rank и optional native score. `Score{}` означает отсутствие;
`ScoreOf(0)` — наблюдавшийся ноль. `reference.Composite[Q]` принимает именованные `Backend[Q]` и явный
`RRFConfig{K: 60}`; положительные weights привязаны к identity. RRF использует
rank, удаляет дубликаты внутри backend и сохраняет каждый независимый сигнал.
Отсутствующий native score не выводится из ranking score. Native ranks сохраняются,
RRF использует отдельные ordinal positions. Порядок backend не влияет на fusion; ties разрешаются по ID и revision.
Каждый child возвращает одну coverage identity, совпадающую с его именем;
nested composites этот reference adapter не поддерживает. `AllowDegraded`
разрешает partial failure, но не ослабляет minimum visibility. Если все backend
недоступны, возвращается `ErrUnavailable`.

`RecallProjected` соединяет Search → canonical checks → Ranker → exact-revision
Projector → optional output policy → final canonical checks. Для бюджетирования
host передаёт `ProjectionBudget[O,R]`: положительный `Max`, output `Codec[O]`
и versioned `OutputPolicy[O,R]`. Policy выбирает только разрешённые exact refs
и объясняет каждый пропуск (`budget` или `oversized`); изменить canonical payload,
provenance или trust через selection нельзя. Изменение source, authority,
expiry или revoke во время callback отклоняет всю выдачу, включая metadata
пропущенных projections.

Policy измеряет полный финальный `ProjectedRecallResult`: output, provenance,
projection envelopes, coverage, progress и omissions. `BudgetUsage` — отдельный
receipt, исключённый из JSON через `json:"-"`; transport envelope с receipt
требует своего измерения. `reference.JSONPacking[O,R]` считает точные bytes
`json.Marshal(result)` и сохраняет ranked order. `RejectOversized` выбирает
ошибку вместо omission для слишком большой projection. Если финальный body
или metadata-only body не помещается, результат — `ErrBudget`. Host policy
может использовать estimated units; `Exact=false` не обещает точный размер
байтов или model-specific tokens. Tokenizer и модель стоимости принадлежат host.

## Гарантии

- `store/memory`: atomic transactions и CAS внутри процесса, без durability.
- `store/sqlite`: real transactions, WAL/FULL, CAS, content-free durable receipts.
  Оба adapter проходят общую [StoreSuite](conformance/store.go).
  Для остальных ports поставляются reusable [conformance suites](conformance/ports.go):
  Search, Sink, Codec, Authority, Sources, Clock и typed callbacks. Примеры
  consumer-owned fixtures исполняются в [reference tests](reference/conformance_test.go).
- Повтор operation ID с тем же input восстанавливает результат; другой input
  конфликтует. `ErrUnknownOutcome` требует повторить исходную operation identity.
- Proposal и acceptance хранятся отдельно от canonical records. Digest, source
  revisions, scope epoch и host policy проверяются перед commit.
- `Get` читает canonical state. `Recall` ищет IDs/revisions и перечитывает их
  канонически перед выдачей. Search acknowledgement — отдельная гарантия.
- Valid-time и recorded-time — разные predicates. Unknown valid-time не
  означает «всегда». Перекрывающиеся активные revisions требуют resolution.
- Forget сначала фиксирует revoke и закрывает reads, затем очищает sinks.
  Pending receipt означает незавершённую очистку; retry не создаёт revisions.
- Late jobs используют epoch fences. Managed derived writes выполняются через
  `WithDerivedWrite`, который исключает гонку с durable revoke.
- Consolidation opt-in: exact dedup, typed domain merge, semantic provider.
  Outputs проходят обычные acceptance/commit; автоматического применения нет.

## Границы

Host отвечает за authentication, provisioning, scheduler, backup restore и
deployment. Field-restricted profiles в v3 отклоняются как unsupported;
потребитель может разделить запись. Это предотвращает использование private
поля при создании публичного summary.

Purge гарантирует логическое удаление из canonical API и зарегистрированных
sinks. Уже доставленные ответы, unmanaged backups, filesystem snapshots и
forensic disk erasure не входят в receipt. Перед обслуживанием восстановленной
canonical базы host должен восстановить актуальный revocation ledger.
Direct store/index access является privileged adapter API и не обходит
authority для безопасного использования через engine.

Внешние IAM/source/provider решения не участвуют в распределённой транзакции:
их порт должен давать valid decision/lease на момент проверки. Произвольные
внешние эффекты не получают exactly-once гарантию.

## Storage и восстановление операций

`Snapshot` возвращает одну `SnapshotPage`: host задаёт `SnapshotOptions.Read`,
`Limit` и `MaxBytes`, затем явно продолжает `Cursor` до `Complete`. Бюджет
ограничивает просмотренные revisions, включая не прошедшие read predicates.
Изменение данных или read policy отклоняет продолжение; старый cursor не
означает полный актуальный обход.

Store использует `Scan` вместо `List`. `View` даёт consistent read,
`FencedView` исключает изменения в точном scope до завершения callback.
SQLite fencing работает между процессами на поддерживаемых локальных Unix
файловых системах; directory `.memy-fences` сохраняется, пока база используется.
Текущий persisted формат — schema 3; переход описан в [migration](docs/migration.md).

Ошибки, ownership и поддерживаемые платформы — в
[API recovery guide](docs/api-recovery.md).

Контракт описан в [design](docs/design.md), версии и импорт — в
[migration](docs/migration.md). Полные wire-контракты лежат в [schemas](schemas/).
Фиксированный scope и evidence matrix — в [acceptance](docs/acceptance.md).
Реализован полный lifecycle знаний. Историческая независимая приёмка исходной реализации
(до remediation; [frozen manifest](docs/reviews/snapshot-final.json),
SHA256 `b39c2bda626b751dfbd035669d6e67852d8e25898ce547819e2ed82c5e9880e1`) подтверждала 85/85 требований
(100%): [полнота](docs/reviews/completeness-final.md),
[корректность](docs/reviews/correctness-final.md). Подтверждённые дефекты C1–C13
закрыты; результаты проверок и границы аудита — в
[итоговом отчёте](docs/reviews/final-audit.md).

Актуальные исправления F01–F10 и решения D01–D70 описаны в
[remediation](docs/reviews/remediation-final.md). Исторические проценты выше
не заменяют эту приёмку.

Обновление toolchain, зависимостей и конфигов описано в
[maintenance](docs/maintenance.md); прежние audit snapshots относятся к
состоянию до этого обновления.

## Установка и релиз

```sh
go get github.com/skosovsky/memy@v0.3.1
```

Для проверки и публикации committed source:

```sh
make lint
make test
make test-integration
make test-e2e
make release-patch
# Для несовместимого изменения API до v1:
make release-break
```

Release выполняет lint, unit, integration и e2e в отдельном checkout выбранного
source. После подтверждения один atomic push публикует source в main и candidate
в теги всех модулей. Пользовательский checkout остаётся неизменным.
См. [release runbook](docs/release/runbook.md) и [verification](docs/verification.md).

## Managed projections

`Projection.Retrieval` сохраняет score presence, native signals и ranking explanation
при `RecallProjected`; обычный `Project` оставляет его nil. Evidence включается в
точный JSON budget. См. [контракт и host ownership](docs/managed-projections.md).

`go run ./examples/managed-projections` демонстрирует два отдельных durable store,
явный host acceptance, checkpoint reopen, pending cleanup и retry. Checkpoints
обслуживаются только через повторную canonical lineage validation. Пример не
предоставляет универсальный production storage backend.

Consumer compatibility выполняется через `make test-integration`, lifecycle и
published baseline — через `make test-e2e`. Peers закреплены в отдельном модуле;
core не получает их импортов. Все три обнаруженных модуля публикуются.

Проверки используют временные artifacts и изолированный module cache.
Published baseline — v0.3.1; `MEMY_REF` выбирает другую точную версию.
