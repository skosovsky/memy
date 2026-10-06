# Task 02 — независимая приёмка полноты, round 1

Результат: **6/10 ST-требований полностью подтверждены — 60%**. Задача не принята.
Частично выполненные и непроверенные строки дают ноль, а не дробный балл.
Это предварительная приёмка работающего дерева, не заключение об окончательном
состоянии. Автор отчёта не участвовал в реализации и не менял production/tests.

Проверены `.cursor/task/02-bounded-storage-and-concurrency.md`, обязательные
правила `.cursor/task/README.md` и неизменённый inventory ST-01–ST-10 из
`docs/acceptance.md`. Историческая bootstrap-приёмка не использована как новая
приёмка task02. HEAD: `9f7b50fa354e6fd74a9def6886850bf50d3aec78`.
Хеши наблюдавшегося состояния сохранены в `completeness-round1-snapshot.json`.
Основной агент продолжал изменения во время review; любые последующие изменения
требуют повторной проверки, а snapshot не объявляется frozen final state.

## Матрица ST-01–ST-10

| ID | Зачёт | Код/контракт | Проверенное доказательство и предел |
| --- | --- | --- | --- |
| ST-01 | да | `Store`, `memory.run`, `sqlite.run`, `sqlBucket`, CAS/tombstones | Свежий shared StoreSuite на обоих adapters: rollback/error/panic/cancel, CAS race, ABA, detached values, escaped bucket/scan, close. SQLite `TestFaultRecovery` отдельно проверяет before/after commit и `ErrUnknownOutcome`; memory не заявляет durable unknown outcome. Собственный adapter run прошёл. |
| ST-02 | да | `ScanOptions`, `ScanPage`, `kv.Binding`, AVL live-key index, SQLite partial live-key index | `bounded_scan`, `scan_cursor_binding` обоих adapters прошли: byte/entry limits, order, oversize ErrBudget, rollback generation, stale generation, scope/prefix, escaped lifetime. SQLite fresh test проверяет reopen, database copy rejection, mutation; context checks есть до/в scan и SQL context cancellation. Ключи/позиции кодируются byte-preserving []byte. |
| ST-03 | да | Consolidate читает exact inputs через recallCandidate; memory addressed overlay; SQLite addressed SQL; `workcost` | Собственные `TestAddressedOperationsDoNotDecodeOrCopyUnrelatedHistory` и `TestConsolidationReadsOnlySelectedEvidence` прошли. Равные decode/byte/blob counts 1k→10k, structural seek bound, unrelated source removal. Warm 1k/10k/100k counters дополняют, но не заменяют structural assertion. |
| ST-04 | да | Recall/Project callbacks вне transaction; WithDerivedWrite использует scoped raw FencedView; SQLite deferred WAL reads | Собственные callback tests обоих adapters проверяют B write и A Forget до release rank/project. Memory isolation и SQLite actual subprocess FencedView test проверяют B write во время held A exclusion. Managed-write code и старый race test используют тот же boundary, без global BEGIN IMMEDIATE reservation. Это доказательство изоляции границы; distribution contention report ещё не готов (ST-09). |
| ST-05 | нет | Final Recall/Project revalidation, explicit derived-write fence, authority/source/retention/deadline gates | Existing+targeted tests подтверждают revoke, source changes, retention/deadline и managed-write exclusion. Actual subprocess Store fencing прошёл. Однако явное требование task: **cross-handle fencing и idempotency, не только общий Engine**, пока не полностью доказано: idempotency test использует один Engine до reopen; независимые writer handles проверяют CAS, не повтор одного lifecycle operation. Нужен synchronized independently opened Engine/handle replay и конфликт changed request с durable receipts. |
| ST-06 | да | `beginPurge`, durable active pointer/job, `advancePurge`, exact operation/epoch/chunk acknowledgement | Собственные Limit=1 native memory/SQLite tests подтверждают bound, active fence, interruption/reopen, no premature completion, old derived fence rejection, same identity replay. Durable fault before/after initial fence и pending sink reopen прошли. Corrupt/missing active pointer/progress fail closed. Failed/invalid Chunk acknowledgement fixture passed. Новая проверка concurrent chunk retries может выявить дефекты, но отсутствие такого теста само по себе не заменяет обнаруженный дефект. |
| ST-07 | нет | Historical membership atomic with Commit; queue/selected traversal; validation canonical authority | Native production graph10k evidence `native-graph-tests.txt`: chain, branches/cycle, historical-only edge, full erasure, exactly one tombstone, unrelated heads survive, replay no extra chunk. Historical source correction fixture также проходит. **Линейная стоимость для общего графа не выполнена/не доказана:** local per-invocation evidence cache перечитывает full high-fan-in child в каждом bounded invocation; подробности F-01 ниже. Long chain degree≈1 этого не закрывает. |
| ST-08 | да | Bounded SnapshotPage and SweepRequest/Result; durable suffix/frozen scope, current retention per visited revision | Собственные Snapshot empty-eligible progress/read-plan change, Limit=1 Sweep reopen/cancel/ownership, shortened current retention and draft cleanup tests прошли. MaxBytes bounds page, а required lineage/canonical point reads остаются input-dependent, что описано. Complete Sweep означает завершение данного pass; новая policy после уже посещённой записи требует нового pass — контракт это явно говорит. |
| ST-09 | нет | Fixed warm corpus/private counters и extended harnesses есть | Warm baseline+after terminal 90 samples each, allocations/counters/128-byte corpus/1k–100k/5 ops; censored observations честно сохранены. Extended after terminal PASS 175.214s, 3 repetitions growing fan-in + handle-cold Get, теперь сохранён `extended-raw.txt`; это полезная stage evidence. Нет завершённого contention distribution/report; нет before growing-lineage/concurrency corpus; пока нет общего final before/after cost report с полным требуемым scope. Baseline public request counts не private decode rows; report должен сохранить это различие. |
| ST-10 | нет | Общие проверки и две отдельные final reviews обязательны | Мои targeted adapter/root/schema runs зелёные. Свежие финальные `go test -race ./...`, vet, format и executable examples на final snapshot ещё не представлены. Две final independent reviews не завершены; этот round1 не заменяет их. |

## Прослеживаемость каждого пункта задания

Ниже source inventory не переопределяет ST denominator. Каждый numbered/bullet
пункт task02 имеет свою строку; внутри перечислены его отдельные обязанности.

| Source item | Состояние / evidence |
| --- | --- |
| Contract 1: consistent read / atomic mutation / managed exclusion | Определены в Store и design §§Transaction and exclusion; память допускает stronger within-scope serialization. |
| Contract 2: authority/source/retention, delivery point, trusted callback already delivered payload | Initial preparation + final fenced validation в Recall/Project; managed entry deadline gate; docs прямо ограничивают already handed host payload. |
| Contract 3: order, cursor owner/scope, page work, cancellation, snapshot mutation/stale | Scan contract, HMAC binding, generation, page limits, context; проверен ST-02. Snapshot additionally actor/policy/options plan. |
| Contract 4: durable initial fence before cleanup, truthful pending/complete, no partial complete | beginPurge no enumeration, active scope pointer; CanonicalComplete separate final chunk acknowledgements; ST-06. |
| Contract 5: supported adapter minimum fencing, independent handles/processes, reject unsupported before effects | flock per resolved local database/scope, kernel cleanup, non-Unix Open early ErrUnsupported; process tests. Hard links/network FS/fence removal ограничения документированы. |
| Implementation 1: SQLite addressed CAS/ABA/detachment/lifetime/rollback/unknown, prefix indexes, remove load/clone/persist | `sqlBucket`, indexed length metadata then accepted value Get; shared suite + fault/reopen; old path отсутствует. |
| Implementation 2: isolated memory scopes/read vs exclusion, no history clone, honest SQLite writer limit | Registry mutex только lookup; per-scope gate; immutable AVL root overlay; touched commits. SQLite one writer описан, remote read callbacks не держат его. |
| Implementation 3: exact selected Consolidate inputs + required lineage, unrelated source unavailable | Exact lookup instead of Snapshot; independent source regression + private counters passed. |
| Implementation 4: adjacency/reverse historical indexes atomic, canonical remains truth, linear visited graph | Atomic membership + canonical validation verified; линейность общего графа **не зачтена**, F-01. |
| Implementation 5: bounded Snapshot/Sweep/mass cleanup, explicit full scope operations, continuation, current retention | Host-driven APIs; frozen active jobs; full scans of selected revisions/proposal revisions объяснены. Remaining `scanAll` only selected-record historical lookup and reintroduction receipt check, not old Snapshot/Sweep/revoke compatibility path. |
| Implementation 6: injected rank/project/other callback preparation/final revalidation; fenced side effects; no old writer lock | Ranker, Projector, merger вне transaction; final validation before core delivery. Authority/source/retention themselves trusted synchronous validation callbacks; slow host callback obligations в docs. |
| Implementation 7: trusted callback latency/cancel, no forced goroutine, no timeout absence-of-effect/exactly-once claim | Synchronous implementation, context-aware lock waits, docs boundaries and external-effect unknown; no hidden unbounded worker/scheduler. |
| Correctness 1: shared StoreSuite all listed invariants | ST-01 passes fresh independent run; unknown outcome tested separately by SQLite-specific hook. |
| Correctness 2: Get/fixed Consolidate private decode/request work proof | ST-03 passes; no unrelated history decode. |
| Correctness 3: slow A callback, B works, short SQLite writers allowed | ST-04 code/tests substantiate ordering; wall-clock noisy threshold не используется как scalability CI gate. |
| Correctness 4: Recall/Project/managed vs Forget/policy/source/expiry, no newly delivered forbidden payload/artifact | Targeted tests + return/retention/deadline regressions and scoped protocol; no claim of retracting callback input. Final whole-suite race still required. |
| Correctness 5: cross-handle SQLite fencing **and idempotency** | Partial, ST-05 needs independent simultaneous lifecycle replay evidence. |
| Correctness 6: interrupted batch reopen same identity, required acks, late jobs/stale cursors | ST-06; independent Scan generation/copy proof and active job guard; no new revision on replay. |
| Correctness 7: historical source/long chain/branch removal, repeated purge no new revision | Functional portion verified in native graph and historical source fixture. Complexity obligation separate I4/ST-07 remains open. |
| Correctness 8: retain old regression observable guarantees | Tracked diff removes no historical `func Test...`; existing tests migrated to consumer-owned full-page collectors. New temporary synthetic graph helper deleted after production-path test added, not substituted as evidence. |
| Performance corpus: before/after memory+SQLite, 1k/10k/100k, fixed size, growing lineage, five ops | Fixed corpus yes; extended after yes; before growing-lineage evidence missing. Censored large purge/Sweep still incomplete samples, not claimed completion. |
| Performance concurrency: multiple scopes and controlled callback delay | Harness B+C and channel hold 50ms exists; completed run and comparison/report not yet available. |
| Performance measurements: alloc/bytes, scanned/decoded rows, latency distribution/contention | Warm raw alloc/counters; private instrumentation actual copies/value rows/metadata/decode calls; distribution needs aggregation of repeated samples, small-N limits; contention pending. |
| Performance reproducibility: hardware/toolchain/corpus/warm-cold/repetitions | Fixed baseline documented Apple M1 Max, darwin/arm64, Go1.27.1, 3 reps, uncontrolled workstation; handle-cold accurately excludes disk-cold claim. Final report consolidation pending. |
| Performance structural tests, no unstable wall threshold as CI gate | Private addressed test, scan/page and AVL assertions; fresh targeted green. General graph complexity test still required by F-01. |
| Performance honest SQLite limits, no distributed scale claim | One physical writer/local Unix/advisory scope-lock boundary explicitly documented; warm throughput regressions/censored tests не скрыты. |
| Exclusions: no Postgres/Redis/distributed scheduler/new DB/unbounded workers/forensic wipe/tombstone GC/weaker revoke/dual Store API | No excluded additions found. Store List/load/clone path removed; schema3 direct break, no aliases/dual read. |
| General BYOT and library boundaries | Consumer payload/reference/authority/query/policies unchanged; no Agent/Message/tokenizer/vendorSDK/network/IAM. `go list -deps .` has only stdlib + own `memy/internal/workcost` + root, no external telemetry/eval dependency. |
| General executable schemas/current docs/examples | schema3 compiled + real lifecycle instances for all kinds passed independently; old v2 schemas removed, incompatible v1/v2/v4 adapter rejection. Offline consumers migrated; final executable example run still required. |
| General delivery: independent reviewers, commit after accepted task, preserve user changes/no push | Final gates pending; no task02 commit. Makefile/iCloud duplicate files outside review-owned changes; report doesn't authorize staging them. |

## Open findings / required follow-up

**F-01 — high-fan-in traversal repeats canonical-size work.**
`purge_job.go:257` allocates membership evidence cache inside each advancePurge
transaction. For every parent, expand calls validateMembership (`:329`), which
loads/decodeDocument the full child revision and builds all parent/source sets
(`membership.go:68–85`). A graph of N parents and one child having N lineage
refs has O(N) vertices/edges. With valid Limit=1, N invocations each decode/copy
the same O(N) child, so total evidence work is Ω(N²). With any fixed entry budget,
the asymptotic repeated work remains superlinear as N grows. A bounded page alone
does not make this full-transitive cost linear. Needed: canonical-authoritative
bounded validation state that is reusable durably without treating an index as
knowledge, or another implementation that removes the repeated full child work;
plus private decoded/copied work assertions over growing fan-in and interruption.

Extended fan-in after samples (default Limit256) finish successfully and report
183 / 1549 / 15219 decoded docs and 99,224 / 843,547 / 8,878,324 decoded bytes for
10/100/1000 parents. These finite points do not falsify the Limit1/code-derived
asymptotic issue. Bench result itself is not declared incorrect; its scope is
insufficient to certify all bounded budgets/general graph complexity.

**F-02 — cross-handle lifecycle idempotency evidence missing.** Need independently
opened SQLite engines reusing exact operation identity while canonical/receipt
updates race, plus changed-request conflict and reopen receipt recovery. Current
CAS test and one-engine replay test prove different subsets.

**F-03 — complete performance deliverable missing.** Preserve fixed baseline;
provide before growing-lineage evidence against prechange production, completed
controlled contention corpus, final aggregation/counters/latency conditions and
honest comparisons. A physical disk-cold experiment is not demanded: handle-cold
must remain correctly labelled, as it is now. No benchmark was restarted here.

**F-04 — final exact-source verification and both reviews pending.** After fixes,
run final format/vet/race/examples and repeat both independent acceptances on
the same frozen source state. This round cannot be promoted to final100%.

## Independent command evidence

All commands use `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath`.

- `go test -count=1 ./store/memory ./store/sqlite ./reference ./conformance ./internal/kv`
  passed; output `/tmp/memy-task02-review-completeness-adapters.txt`.
- Targeted root run of addressed/consolidation/callback/managed-write/bounded-purge/
  corruption/snapshot/sweep/retention/source/failed-purge/durable-fault/deadline
  fixtures passed, root 8.974s; exact command not narrowed to claiming all tests.
  Output `/tmp/memy-task02-review-completeness-root.txt`.
- `go test -count=1 -run 'TestPersistedLifecycleMatchesWireSchemas|TestWireSchemaRejectsMalformedEnvelope|TestRejectIncompatibleSchema' ./...`
  passed; output `/tmp/memy-task02-review-completeness-schemas.txt`.
- Inspected completed `native-graph-tests.txt`, `baseline-raw.txt`, `warm-raw.txt`,
  warm summary/method/source hashes, extended terminal raw output; did not run
  another benchmark alongside the root measurement process.

No assertion here means absence of all unknown bugs. Correctness is a separate
independent gate; completeness is limited to the requirements and evidence above.
