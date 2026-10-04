# Независимая adversarial приёмка: корректность, round 4

Дата: 2026-10-04. Correctness reviewer. Completeness reports другого reviewer не читались и не использовались.

**Вердикт:** C10 и primary-origin C11 закрыты в повторно выполненных независимых cases; прежние C1–C9 не регрессировали в проверенном corpus. Новая подтверждённая C12 (P2, mandatory lineage corruption) блокирует приёмку frozen round 4. Отсутствие иных новых findings ограничено реально выполненными checks.

## Проверяемое состояние

`docs/reviews/snapshot-authority-lineage.json`: **7d8068241180dfb86595fec145094f50f976a742729a839dc134487c9b27f864**, 92 файла. SHA256 и byte size всех included files сверены с manifest, aggregate пересчитан из compact sorted-keys JSON списка files. До сохранения отчёта changed=[], aggregate совпадает; после сохранения выполнена повторная проверка.

Reviewer не менял production source, штатные tests, docs/design/README/matrix или lint configuration. Новый repository artifact — только этот отчёт. Scratch: `/private/tmp/memy-correctness-round4/audit_test.go`, module replace на frozen repo; реальный execution log `/private/tmp/memy-correctness-round4/repro.log`. Никаких commits/releases/publication.

## Closure C10/C11 и затронутые гарантии

- **C10:** independent `TestAcceptanceExpiredInitialAuthorityDecision` PASS на memory/SQLite. Initial expiring authority lease, который истёк во время Sources I/O, больше не produces successful expired acceptance. Code review подтверждает inclusion `decision.ExpiresAt` в final Accept deadline gate. Свежий штатный `TestAcceptanceCannotReturnExpiredInitialAuthorityLease` также проверил Proposed state после rejection, fresh review retry и успешный commit на обеих stores.
- **C11:** independent `TestTransitiveTargetIndependentAudit` PASS на memory/SQLite. `a1 -> b1(lineage a1)`, conditional `a2(lineage b1)` теперь отклоняется до publication. `commitDeadlineGate` передаёт target ID в transitive `lineageDeadlines`; forbidden target проверяется на каждом ref, включая непрямой уровень.
- Свежий штатный `TestTransitiveTargetCannotInvalidateItsOwnCommit` проверил Append и Supersede на обеих stores: a1/b1/c1 сохраняются, staged related transitions откатываются; alternate safe target с unconsumed operation identity succeeds, receipt replay стабилен. Tests прочитаны и действительно выполнены в fresh full normal/race commands.
- Independent `TestFinalDeadlineFailureRollsBackReconcileAndReplay` снова PASS на обеих stores: tail rejection после staged supersede откатывает состояние/receipt/operation/CAS; exact retry и receipt replay работают.
- Independent multi-result Snapshot expiry, exact direct/transitive sources, rank/project source mutation, C7 bytes arrays, C9 malformed date-time, lagged-index coverage, annotation-budget и original monotonic acceptance cases снова PASS.
- Independent SQLite BeforeCommit rollback и AfterCommit unknown outcome/reopen снова PASS. Это coverage конкретных transactional boundaries, не любое физическое failure recovery.

## C12 — P2: runtime record lineage может потерять reviewed mandatory dependencies без schema error

**Контракт:** proposal digest связывает reviewed lineage; canonical lineage включает обязательные proposal/consolidation inputs. Current reads проверяют полный source/dependency chain и fail closed при corruption. Дополнительные duplicate dependencies могут расширить canonical lineage, но уже reviewed dependencies не должны исчезать.

**Code paths:**

- `validation.go:66` валидирует Proposal, scope/state и intact digest.
- `validation.go:69` валидирует Record.Lineage только через validRevisionRefs; каждый ref имеет форму, но отсутствует проверка inclusion reviewed `Record.Proposal.Lineage`.
- `engine.go:660` canonical read traverses только `candidate.Lineage`; `engine.go:544` отдаёт тот же runtime list в typed Provenance.Lineage.
- Snapshot (`consolidation.go:115`), full transitive traversal (`engine.go:714`) и new callback-free gate (`deadline_gate.go:47`) также следуют runtime record lineage, не восстанавливая/rejecting утраченные digest-bound proposal refs.

**Independent AAA fault repro:** `TestRecordCannotDropReviewedMandatoryLineage`, memory и SQLite.

1. Commit `original1` с source/r1.
2. Добавить independently valid fresh/r1. Remember/Accept/Commit `derived1` с fresh direct source и reviewed Suggestion.Lineage `{original,1}`. Proposal digest защищает именно этот mandatory ref.
3. Trusted Store.Update имитирует corruption: в derived history/head documents заменить только **record-level** `lineage` на null. Embedded proposal lineage и digest не меняются; весь schema field inventory присутствует, field type допустим.
4. Registry.Remove(source) делает original evidence unavailable; fresh direct source остаётся valid.
5. Get(derived) возвращает payload и nil error. Его Provenance.Lineage пуст, хотя intact reviewed proposal продолжает содержать original1.

```text
reviewed mandatory lineage erased: payload="derived" outputLineage=[] error=<nil>
```

Оба store subcases FAIL на контрактном assertion. Исходный reviewed proposal/digest не пересчитывался: это не атака, подделывающая весь purported host review, а локальная несогласованность между двумя уже сохранёнными representations обязательной dependency.

**Последствие:** runtime принимает structurally valid, но семантически corrupted canonical envelope и обходит обязательную source/lineage revalidation. Derived payload становится readable при unavailable ancestor evidence; export теряет reviewed provenance. Это integrity/fail-closed defect. Публичный unauthenticated доступ к Store, cryptographic tamper resistance произвольного полного envelope или доказанный ACL bypass этим repro не предполагаются.

**Ожидаемое поведение:** такой envelope возвращает ErrSchema (либо safe explicit reconciliation до использования). Минимальный проверяемый invariant — canonical Record.Lineage содержит все exact reviewed Proposal.Lineage refs; дополнительные duplicate refs допустимы. Shape/ref validation сама по себе этого не обеспечивает. Изменений implementation reviewer не делал.

## Реально выполненные команды

В исходном repo, свежие tests, обе команды exit 0:

```sh
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -count=1 ./...
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -race -count=1 ./...
```

В scratch:

```sh
cd /private/tmp/memy-correctness-round4
env GOCACHE=/private/tmp/memy-go-build GOPATH=/private/tmp/memy-gopath GOTOOLCHAIN=local go test -mod=mod -v -count=1 ./...
```

Compilation/execution успешны. Suite exit 1 ожидаем: единственный failing top-level test — C12, два store subcases. Все предыдущие independent checks PASS. Отдельный `-run TestRecordCannotDropReviewedMandatoryLineage` также reproduced C12 на обеих stores до полного execution. Полный актуальный лог сохранён в `repro.log`.

`docs/reviews/checks-authority-lineage.log` (make validate) и `external-authority-lineage.log` provided primary поддерживают его checks; reviewer не подменяет ими перечисленные собственные executions и не выдаёт их за свой запуск. Strict config этим reviewer не менялась.

## Границы аудита и terminal status

Round4 завершён на указанном frozen snapshot; further expansion этого раунда остановлен после текущего набора проверок по просьбе parent. C12 передана сразу после independent reproduction. Следующий исправленный snapshot требует отдельной closure проверки C12.

macOS arm64, CGO, Go 1.26.5. Reviewed changed original-authority lease binding и full prospective transitive target traversal; активно проверены tail rollback/replay и malformed mandatory-lineage case. Existing broader scoped authority, retention, late jobs, search, purge, durability/temporal suites исполнились свежими normal/race runs. Это не exhaustive graph/state exploration или доказательство отсутствия всех ошибок. Arbitrary host IAM lease protocols, provider/codec side effects, production performance, forensic erasure и физические storage failures не проверялись.
