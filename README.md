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
make test
make race
make vet
make examples
make validate
```

Фиксированный quality protocol и сохранённый baseline/consolidation report:
[docs/quality.md](docs/quality.md). Для новой локальной выборки измерений:
`go run ./examples/quality -out docs/quality-report.json`.

`make examples` запускает полный offline сценарий на временном SQLite-файле:
extraction, acceptance одного proposal, commit, corrected revision, close/reopen,
canonical read в новой сессии, search visibility, typed projection и forget
со сбоем одного sink и повтором очистки. Данные и файл удаляются после примера.
Никаких credentials не нужно.

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
Список `Sinks` определяет managed deletion boundary. `Reintroduction` разрешает
host-controlled повторный ввод после scope/source tombstone; по умолчанию
tombstones сохраняются бессрочно. Retired record IDs не переиспользуются.

`reference` содержит offline policy grants, source registry, clock, deterministic
index с manual acknowledgement, summary/cache sink, typed extractor/merge/
projection scripts. Это reference adapters, не vendor integrations.

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
deployment. Field-restricted profiles в v2 отклоняются как unsupported;
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

Контракт описан в [design](docs/design.md), версии и импорт — в
[migration](docs/migration.md). Полные wire-контракты лежат в [schemas](schemas/).
Фиксированный scope и evidence matrix — в [acceptance](docs/acceptance.md).
Реализован полный lifecycle знаний. Независимая приёмка исходной реализации подтверждает 85/85 требований
(100%): [полнота](docs/reviews/completeness-final.md),
[корректность](docs/reviews/correctness-final.md). Подтверждённые дефекты C1–C13
закрыты; результаты проверок и границы аудита — в
[итоговом отчёте](docs/reviews/final-audit.md).

Обновление toolchain, зависимостей и конфигов описано в
[maintenance](docs/maintenance.md); прежние audit snapshots относятся к
состоянию до этого обновления.

## Установка и релиз

```sh
go get github.com/skosovsky/memy@v0.1.0
```

Для публикации из чистой ветки `main` с настроенным GitHub remote:

```sh
make release VERSION=v0.1.0
```

Target выполняет полную проверку, публикует ветку и annotated tag одним
atomic push, затем создаёт GitHub Release. Повтор с тем же тегом допустим
только для того же коммита; существующие теги не перезаписываются.
