# Independent correctness acceptance — round 6

Вердикт: **C13 закрыт независимо на memory и SQLite. Новых подтверждённых дефектов в выполненном ограниченном наборе нет.** Round6 terminal. Этот вывод не доказывает абсолютную корректность библиотеки и не заменяет проверки неизвестных interleavings или сторонних адаптеров.

## Frozen snapshot

`docs/reviews/snapshot-managed-writes.json`: 94 included файла, SHA256 `f2b053c972421994ae426bd88faccc1e866decb1b558f7d20a244f948cdf31a5`. До запусков и после сохранения отчёта сверены размер и SHA256 каждого included файла, затем aggregate SHA256 sorted compact JSON inventory. Included source/tests/design/matrix/lint не менялись; scratch только `/private/tmp/memy-correctness-round6`. Reviews excluded из hash.

Completeness reports не читались; независимый reviewer не координировался с другим reviewer. Primary supporting logs не подменяли собственные свежие проверки.

## C13 closure

Contract `docs/design.md:421–427` прямо требует проверки exact transitive deadlines после финального host reauthorization и до старта callback. Не обещается rollback уже начавшегося external effect или остановка времени внутри callback.

`forget.go:148–154`: после reauthorize добавлен callback-free `deadlineGate(b, fence.Scope, nil, lineage)`, непосредственно перед fn. Helper читает exact live heads, проходит транзитивные dependencies, собирает effective proposal/retention deadlines и сравнивает их с одним последним Clock sample. После этого до fn нет дополнительных host/source/retention calls. Store.View и прежняя epoch fence остаются на месте.

Исходный внешний `TestManagedWriteExpiryDuringReauthorization` из round5 повторён с неизменным fault trigger на обеих stores:

- canonical input имеет стабильный deadline 01:00 при начальном Clock00:00;
- initial authorization и lineage validation проходят; второй Authority.Check(ActionRead) переводит Clock ровно на01:00, возвращая тот же Allowed actor/scope/policy;
- теперь `errors.Is(err, ErrStaleInput)` true, callback не вызывается. Ранее именно этот repro возвращал nil и called=true.

Новый независимый AAA case `TestManagedTransitiveDeadlineBoundaryAndCleanup` проверил более широкий affected boundary на memory/SQLite:

1. Mixed roots: unlimited record и unlimited derived с finite transitive ancestor, плюс повторный exact derived ref. На здоровом Clock callback проходит один раз.
2. Final reauthorization пересекает только ancestor deadline; gate возвращает ErrStaleInput без нового callback. Unlimited direct root и duplicate refs не обходят transitive minimum.
3. После ошибки gate тестовый Clock возвращён в исходное положение только для проверки fixture/resource lifecycle; тот же fence допускает здоровую операцию. Callback с sentinel error также возвращает исходную ошибку, и следующая операция успешна. Store transaction gate не остаётся захваченным после обоих видов отказа.
4. Отдельный fault отменяет Context во время второго Authority.Check; callback не вызывается, `errors.Is(err, context.Canceled)` true. Затем обычный Get через fresh Context успешен: ресурсы освобождены.

Штатный `TestManagedWriteRejectsExpiryDuringFinalReauthorization` выполнен обычными и race source tests: direct/transitive × memory/SQLite, здоровый callback успешен, exact-expiry fault запрещает callback. `TestManagedWriteCannotRacePurgeToResurrectArtifacts` также выполнен на обоих backend: начатый callback удерживает canonical exclusion, конкурирующий revoke достигает timeout; после callback revoke удаляет derived artifacts; late acknowledge/write отвергаются. Новая проверка deadline не изменила эту границу.

## Предыдущие findings и affected guarantees

Весь внешний набор предыдущих rounds прошёл заново, включая C1–C12 и original C13 closure. Это конкретные проверенные fixtures:

- current exact source revision/remove/outage и direct/transitive source validation на delivery/accept/commit; изменение источника внутри rank/project callbacks;
- acceptance instant equality с monotonic timestamp, physical history key binding, case-sensitive wire fields, canonical byte representation и invalid date-time lexical forms;
- full output budget с реально invoked provider и Evidence1MiB; no-min pending index coverage;
- read/accept/ancestor-commit expiry crossing, initial authority lease expiry, prospective transitive-target rejection;
- multi-result final deadline и atomic reconcile rollback с identical retry/receipt replay;
- SQLite beforecommit rollback/aftercommit unknown outcome/reopen recovery;
- C12 mandatory reviewed subset с удалённым runtime Lineage и отозванным ancestor source, ErrSchema/empty payload;
- положительный Duplicate case: reviewed a1 плюс additional b1 и повтор a1; все delivery paths и exact replay успешны, независимое обновление дополнительного b1 делает derived stale.

Source regressions, в том числе revoked content-free tombstones, выполнены заново. Это не утверждение, что все возможные provider mutations, схемы, CAS interleavings или resource failures исчерпаны.

## Собственные выполненные команды

Explicit pinned PATH `/opt/homebrew/Cellar/go/1.26.5/libexec/bin:/opt/homebrew/bin:/usr/bin:/bin`; проверено `go version go1.26.5 darwin/arm64`. Для всех запусков: `GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local`.

- Source `go test -count=1 ./...`: exit0.
- Source `go test -race -count=1 ./...`: exit0.
- Внешний scratch module `go test -count=1 -v ./...`: exit0; свежий полный лог `/private/tmp/memy-correctness-round6/pinned-repro.log`. Original suite выполнена до добавления нового boundary case и прошла; финальный полный повтор с новым case также exit0.
- Before/after snapshot inventory verification: все94 included файла неизменны; aggregate совпадает с frozen SHA256 выше.

`docs/reviews/checks-managed-writes.log` и `external-managed-writes.log` — supporting primary evidence. Собственный make validate/vet/strict lint/examples runtime отдельно не повторялся; source tests компилируют/исполняют имеющиеся example tests, но это не полная замена primary DoD.

## Границы заключения

Аудит round6 ограничен closure C13, соседними multi-root/transitive deadlines, cancellation/resource-release paths и регрессией предыдущих находок. Exhaustive model checking/fuzzing, внешние backend adapters и доказательство integrity против произвольной privileged store rewrite не выполнялись. После callback entry external effect остаётся ответственностью callback, как заявлено контрактом. В выполненном наборе новых confirmed defects нет; correctness review terminal, финальные documentation bookkeeping и exact confirmation остаются у primary.
