# Task 02 — независимая приёмка полноты, round 2

**8/10 ST-требований полностью подтверждены — 80%. Задача пока не принята.**
Denominator ST-01–ST-10 сохранён; partial/unverified считаются нулём. Этот review
не участвовал в реализации, не изменял production/tests и не повторял benchmarks.
Round1 source inventory остаётся полным перечнем пунктов задания, а этот отчёт
обновляет evidence и закрытие gaps. Snapshot текущего наблюдавшегося состояния:
`completeness-round2-snapshot.json`, HEAD task01 `9f7b50f`. Финальные performance,
contention и race ещё требуют terminal evidence на окончательном состоянии.

## Обновлённая ST-матрица

| ID | Полностью проверено | Evidence / остаток |
| --- | --- | --- |
| ST-01 | да | Shared conformance memory/SQLite и independent round1 checks сохраняются. Proof-stage whole race прошёл; новая membership architecture не меняет transaction/CAS/ABA/lifetime contract. |
| ST-02 | да | Bounded Scan order/limits/cursor/generation/physical database binding, rollback/cancel и escaped lifetime остаются прежними; AVL/direct SQL paths просмотрены повторно. No List/full-scope clone fallback. |
| ST-03 | да | Exact Consolidate/Get loading и private structural unrelated history test unchanged. Private decode/copy counters сохранены; новые internal proof helpers не возвращают scope-wide reads в адресные операции. |
| ST-04 | да | Rank/Project remain outside transaction, managed effect under explicit scoped FencedView. Memory scope gate / SQLite Unix scoped locks and WAL deferred reads сохраняют B progress во время A external callback; round1 synchronized evidence remains applicable. Финальные contention цифры относятся также к ST-09. |
| ST-05 | да | Новый `TestIndependentSQLiteEnginesShareLifecycleIdempotency`: два одновременно открытых SQLite handles/Engine, concurrent exact Commit и Limit1 Forget, one canonical revision + one tombstone revision2, same epoch/chunk, no history/resurrection. Мой fresh `-race -count=1` прошёл вместе с schema/proof/corrupt-job fixtures. Process fencing, authority/source/retention/deadline regressions дополняют; это больше не только shared Engine. |
| ST-06 | да | Durable purge operation/fence/page/chunk ownership/reopen evidence сохраняется; дополнены stage↔receipt/clean/queue consistency и final ack ResumeDone/Next guards. Correctness round2 отдельно воспроизвёл/закрыл premature final ack, corrupt proof/root/scope и reopen evidence. Own fresh corrupted-stage race passed. |
| ST-07 | да | Historical source + production graph10k functional proof preserved. `validateMembership` теперь вычисляет canonical root один раз и сохраняет content-free exact operation/scope/ref evidence; subsequent edges use bounded-depth inclusion proofs. Cache durable across bounded calls/reopen. Native Limit1 fan-in structural test и независимый correctness probe 100/200/400 подтверждают near-linear decoded/copied bytes; F-01 закрыт в этом объёме. |
| ST-08 | да | Bounded Snapshot and durable Sweep/current retention/cancel/reopen contract сохранён. Новые exact revision-key binding, false completed job/receipt/fence probes усиливают fail-closed поведение; foreign canonical key race test passed. |
| ST-09 | нет | Missing before extended/contention теперь закрыт terminal raw: 99 samples/33 cases growing lineage+handle-cold и 20x3 samples per adapter controlled50ms contention. Старый warm after — stage snapshot до proof changes; final after63cases×3 в работе, followed by same-harness after contention и итоговый before/after report. Нельзя зачесть ещё running измерения или назвать старые hashes final. |
| ST-10 | нет | Proof-stage full race terminal root514.879s полезен, но после него source/test guards changed. Fresh final whole race, vet, format, both executable examples и оба independent final reports на одном source snapshot ещё pending. Мой targeted fresh race green не заменяет whole final gate. |

## Round1 findings

**F-01 закрыт.** `membershipproof` hashes bind exact scoped child revision,
relation/match ID, count and witness position; separate leaf/branch/count domains,
odd-size duplication and at most64 levels validated. On first hit Engine loads
the canonical immutable revision, derives its relation root itself, verifies
the edge and persists exact scoped operation/ref root under active fence.
Subsequent calls verify against that root; an arbitrary index cannot define
its own authoritative relation. Original per-call full child decode cache removed.

The proof's cost is bounded per edge by64 hashes; root/witness construction and
storage include tree depth overhead, so the report must not promise zero edge
metadata overhead. It removes the prior Ω(N²) body decoding from valid Limit1
fan-in. The permanent fixture `TestPurgeFanInDoesNotRedecodeChildPerParent` uses
SelectScope to isolate expand; independent correctness probe uses SourceForget
and measures body/copy work as well. Independent measurements:

| Parents | Decoded bytes | Copied bytes |
| --- | ---: | ---: |
| 100 | 2,118,113 | 2,345,218 |
| 200 | 4,260,180 | 4,738,319 |
| 400 | 8,597,846 | 9,604,719 |

Reopen evidence survives, corrupted relation witness/reused root/foreign scope
reject with ErrSchema without changing history. The model treats raw Store
changes as privileged; this is not a proof that arbitrary missing indexes after
privileged corruption can be detected without canonical enumeration. No new
unbounded host cache, scheduler or mandatory external dependency was added.

**F-02 закрыт.** Cross-handle lifecycle idempotency now explicitly exercises two
Engine instances, not only cross-handle raw CAS. Both exact requests can complete
but one canonical revision and one revoke identity persist. Changed-request
conflict and durable reopen recovery continue to be covered by existing request
digest/replay tests; central scoped operation ledger is the shared authority.

**F-03 частично закрыт, ещё открыт.** Before growing-lineage and controlled
contention baseline raw are terminal and preserved; ordinary old production
without private counters is accurately distinct from final instrumentation.
Final aftermatrix/contention and comparison report still required. Existing
handle-cold baseline/after setup explicitly does not flush OS cache; no physical
disk-cold claim made. Historical censored fixed samples must remain censored.

**F-04 открыт.** Need all final checks and both final exact-source reviews.

## Remaining documentation / evidence work

- `docs/design.md` still literally states root imports only standard library,
  although it now imports private `internal/workcost` and `internal/membershipproof`.
  The intended boundary holds (both helpers use stdlib, no external runtime/
  telemetry/eval dependency); make the current wording precise, without rewriting
  historical bootstrap audits. This is documentation bookkeeping, not a demand
  to remove safe private helpers.
- Before/after reports must identify harness/production hashes, condition/repetition
  counts, successful vs censored latency samples, fixed vs lineage/private counter
  metrics, allocations, row/decode boundaries, independent-scope contention and
  SQLite one physical writer/local filesystem limits. Final measurements must
  refer to the proof-based implementation, not interim pre-proof warm outputs.
- Final checks must include new tombstone revision2 assertion and final ack
  Resume/Next consistency guard. Current proof-stage full race predates some
  final guards; no final acceptance inferred from that output.

## Independent fresh command

```
GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -race -count=1 -run '^(TestIndependentSQLiteEnginesShareLifecycleIdempotency|TestPurgeContinuationRejectsCorruptFenceAndProgress|TestSweepRejectsForeignCanonicalRevisionKey|TestProofBindsEveryRelationAndTreeShape|TestPersistedLifecycleMatchesWireSchemas)$' . ./internal/membershipproof
```

PASS, root1.913s, proof1.558s, preserved output
`completeness-round2-targeted.txt`. `git diff --check` also clean. This fresh run
includes exact tombstone revision assertion, executable schema instances and
current stage guards. Inspected round2 correctness independent probes/report,
before extended/contention terminal raw/summary/harnesses, full proof-stage race
and final source measurement manifest; did not launch duplicate cost runs.

Decision: completeness improved to80%; production/functionality gaps F-01/F-02
closed, F-03/F-04 remain. This report does not claim no unknown bugs or final
global correctness; that remains the separate independent reviewer gate.
