# Проверки задачи 01

Дата: 2026-10-05. Исходный baseline: feat: implement typed knowledge lifecycle and release workflow.
Пользовательское изменение Makefile исключено из задачи.

SHA-256 дерева Go source/tests, schemas, go.mod/go.sum и design/acceptance/migration:
`e6d32ac48a651ac1353e91651d9aae6cc7707d1a2677a78f39af0dcb4662245a`. Файл source.sha256 фиксирует тот же snapshot; отчёты review не входят в hash.

- `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -race ./...` — PASS после исправлений review.
- `go vet ./...` с теми же cache variables — PASS.
- `go run ./examples/lifecycle` — PASS: extraction, correction, SQLite reopen, сохранённый reconciliation decision, projection, purge retry.
- `go run ./examples/quality -out /tmp/memy-task01-quality.json` — PASS: старый synthetic protocol продолжает работать на memory/v2; его ограничения исправляет задача 04.
- `go test . -run '^$' -fuzz '^FuzzScopeIdentity$' -fuzztime=3s -parallel=2` — PASS, 44 570 executions; более долгий fuzz не заявлен.
- `git diff --check` и `gofmt -l .` — PASS / пустой вывод.

Полный race output:

```text
ok  	github.com/skosovsky/memy	9.197s
?   	github.com/skosovsky/memy/conformance	[no test files]
ok  	github.com/skosovsky/memy/examples/lifecycle	(cached)
?   	github.com/skosovsky/memy/examples/quality	[no test files]
?   	github.com/skosovsky/memy/internal/kv	[no test files]
ok  	github.com/skosovsky/memy/internal/quality	(cached)
ok  	github.com/skosovsky/memy/reference	(cached)
ok  	github.com/skosovsky/memy/store/memory	(cached)
ok  	github.com/skosovsky/memy/store/sqlite	(cached)
```

Offline lifecycle output:

```text
Extracted 2 proposals; accepted timezone only; canonical revision 1.
Decision: supersede, resolver=resolver/v1, authority=authority/v1, basis=explicit preference correction.
New session, no transcript: timezone=UTC+7, revision 2.
Search acknowledged revision 2; projection (data): Your timezone is UTC+7.
Forget: one sink failed, retry reached complete at epoch 1.
```

Benchmarks для задачи 01 не требуются; baseline/scaling experiment входит в 02.
Независимые acceptance reports находятся в completeness.md и correctness.md.
