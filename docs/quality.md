# Offline memory quality protocol v2

`internal/quality` exercises consumer-owned synthetic memory fixtures through the
public lifecycle APIs. It evaluates actual canonical state, exact recalled
revisions and payloads, and the final projected context. Host review is a separate
observation, not the experiment verdict. All default extractor/search/answer ports
are scripted and run without network access, API keys or commercial models.

```sh
go run ./examples/quality -out docs/quality-report.json
go run ./examples/quality-integration
```

The input is `testdata/quality-v2.json` (`memy-quality-corpus/v2`); report identity
is `memy-quality/v2`. Executable shapes are
`schemas/quality-corpus-v2.schema.json` and `schemas/quality-report-v2.schema.json`.
Manifest validation also checks registered scenario/port versions and coverage of
all required groups before a store is allocated. The old v1 quality manifest and
report fields have no compatibility reader. `testdata/consolidation-v1.json`
remains only a domain fixture for separate consolidation tests.

## What the report evaluates

Every scenario/repeat has six independent stages and a final verdict:

| Stage | Observation |
| --- | --- |
| `candidate` | Proposed facts, negation/qualifiers, validity, scope, sources and lineage before review |
| `host_review` | The expected host acceptance/rejection and checked apply or preserved baseline |
| `effective` | Actual recalled exact revisions, typed payloads, validity and provenance against the fixture oracle |
| `canonical` | Separate public canonical reads of originals/history, scope, sources, lineage and current revoke/retention |
| `rendered` | Actual `RecallProjected` body, selected identities, omissions, data trust and full JSON context budget |
| `execution` | Required events/checkpoints executed and allowed failure classes observed |

Each check has `pass`, `fail` or `unknown`, a mandatory flag and safe evidence.
An optional unused stage is explicitly `not_applicable`. Missing mandatory evidence
and unexpected adapter/evaluator errors make execution unknown; they cannot stand
for zero violations or an expected host rejection. The runner enforces each plan's
required check IDs and recorded port versions before finalization.

The final scenario verdict is computed after all observations, including privacy
and rendered-output checks. Matching record count alone cannot establish success:
revision, payload and independent provenance/lineage comparisons must agree.
Aggregate unknown execution takes precedence over a known mandatory failure.

A deliberately bad semantic proposal has failed descriptive candidate quality,
is explicitly rejected, and leaves the baseline effective memory intact. Its
protocol verdict can pass because this is the expected safe review outcome. That
pass does not mean the semantic candidate was accurate or useful.

The poisoning suite runs both guarded and permissive host policies. Under the
permissive policy, a false instruction-bearing rule is accepted and remains
`Trust=data`. The report preserves its failed factual-quality evidence and checks
that authority grants and authorized harness events did not change. This boundary
scenario can pass its protocol expectations while showing bad memory quality.
There is no library classifier that discovers false claims from provenance, and
no stored procedure/instruction is dispatched as executable behavior.

## Typed suites and scenario coverage

The manifest selects versioned Go fixture plans with setup, events, typed query,
checkpoints and expected outcomes. It is not a public evaluation DSL. Domain
meaning and equality remain fixture-owned:

- `Preference` contains typed facts with explicit negation. Its suite covers new
  sessions and corrections, unknown/conflict options, valid versus recorded time,
  current historical-read revocation/retention, provider/policy failure and
  baseline/exact/domain/semantic consolidation.
- `ProcedureObservation` contains service, procedure description, observed result
  and preconditions. It uses typed `DocumentRef`, `ProcedureInput` and
  `ProcedureQuery`. Its suite covers relevance and distractors, duplicate backend
  hits, stale revisions, exact full-context packing, no-match/conflict/outage
  distinctions, guarded/permissive poisoning, controlled failures, an actual
  foreign-scope record in the shared store, and interrupted managed purge/retry
  on memory and SQLite. Cross-scope checkpoints independently verify denied
  Get/Snapshot, unchanged foreign canonical state, canonical filtering of a
  foreign search candidate, and absence from the final projected context.

The eight required groups are sessions, temporal, retrieval, abstention,
poisoning, forget, consolidation and evaluator failures. The procedure abstention
case checks actual projected healthy-empty, conflict and unavailable outcomes
separately; an unavailable backend never becomes successful empty context. Forget checkpoints also
cover stale derived writes, source revision changes/removal and stale index
results. Historical read options do not bypass current security or retention.
SQLite cases use isolated temporary storage; the report makes no new durability
or performance claim beyond their checked lifecycle outcomes.

The manifest pins corpus/scenario, provider/model, review/retention/resolver,
consolidation, search, projector, packing and grader identities, plus budgets,
seed and repeat count. Scenario versions describe the actual executed ports;
top-level versions identify the registered protocol configuration. The default
manifest runs two repeats. Derived seed and variation are recorded per run and
change fixture execution, such as insertion order or synthetic payload variants.
Repeats do not turn scripted observations into a statistical LLM benchmark.

## Exit codes and evaluator probes

The CLI serializes the safe report before returning its exit code. `-out` also
writes it to a file with mode 0600. Diagnostics use fixed classes/codes and never
print raw provider errors, query text, payloads, source references or model output.
A malformed manifest produces a minimal unknown report if the destination is
writable. Report serialization/write failure exits 2 and cannot claim a saved run.

| Exit | Meaning |
| --- | --- |
| 0 | Every mandatory protocol check passed; expected semantic rejection is included |
| 1 | Execution is known and at least one mandatory check failed |
| 2 | Execution/evaluation is unknown or invalid, or report delivery failed |

Use the compiled command when inspecting exact process status; `go run` may wrap
the program's exit code:

```sh
go build -o /tmp/memy-quality ./examples/quality
/tmp/memy-quality -out /tmp/memy-quality-report.json
/tmp/memy-quality -probe wrong_payload -out /tmp/memy-wrong-payload.json
/tmp/memy-quality -probe wrong_revision
/tmp/memy-quality -probe missing_lineage
/tmp/memy-quality -probe late_privacy
/tmp/memy-quality -probe false_empty_outage
/tmp/memy-quality -probe checker_error
/tmp/memy-quality -probe adapter_error
```

The first five mutation probes intentionally produce a failed mandatory verdict
(exit 1); checker/adapter error probes produce unknown execution (exit 2). Each
starts from actual public-API observations and an independent fixture oracle,
then changes the observation without changing the oracle. Wrong payload/revision
retains the record count; missing lineage and post-review privacy violations
must still change the final verdict. The outage probe cannot pass a fabricated
healthy-empty result.

The normal `evaluator-self-check` scenario saves these non-passing child probe
outcomes and requires their detection. Only that detection makes the parent
self-check pass. Running a probe directly uses the ordinary final aggregator;
there is no expected-failure override that could hide a real mandatory violation.

## Replacing consumer ports

`examples/quality-integration` supplies a consumer-owned typed extractor/search
wrapper and optional exact answer grader through `ProcedurePorts`, then calls
`RunProcedureCase` for `procedure-retrieval`. It executes the same exact-revision,
canonical and full-context checkpoints as the registered fixture; it is not a
separate evaluator. Custom port identities appear in the scenario report. The
shipped integration remains scripted and is labelled accordingly.

A consumer can replace `memy.Extractor[ProcedureInput, ProcedureObservation,
DocumentRef]`, the `memy.Search[ProcedureQuery]` factory and optional typed `Grade`
callback. Network/model integration, tokenizer, credentials and answer rubric
belong to that consumer. A configured grader's error means unknown execution;
an absent optional grader is not a successful model-answer evaluation. These
internal fixtures illustrate the seam; they do not add a public framework or
vendor dependencies to core.

## Measurements and interpretation

Measurements include explicit availability status, unit and optional value.
Unavailable or errored measurements have no measured zero. Inspect each recorded
unit; measurements are distinct:

- `payload_bytes`: encoded consumer payload only, excluding provenance/envelopes.
- `context_json_bytes`: the actual complete final `RecallProjected` JSON body,
  including projection envelopes, provenance, coverage, progress and omissions.
  Its out-of-band `BudgetUsage` receipt is excluded; a host transport envelope
  needs separate measurement.
- `canonical_serialized_bytes`: unavailable in the current harness. A future
  measurement of serialized canonical documents would be a proxy excluding
  adapter/index/database/WAL overhead, not physical storage footprint. No payload
  savings are presented as canonical storage savings.
- `provider_abstract_cost`: scripted units where actually measured; these have
  no model-token or monetary conversion.
- `real_provider_cost`: unavailable for scripted runs; real tokens/currency are
  not inferred from payload size or abstract units.

The saved [quality-report.json](quality-report.json) contains 16 registered plans
with two repeats: 32 normal scenario reports, all with final protocol verdict
`pass`. It also preserves 14 nested negative evaluator probes: 10 `fail` and four
`unknown`. Those negative outcomes are the expected evidence of evaluator
self-checks, not successful memory-quality results.

The compiled CLI's 14-case validation records expected exits, saved reports with
mode 0600 and byte-identical normal reruns in
[cli-validation.json](reviews/task04/cli-validation.json). The current vet,
format, diff and lifecycle/retrieval/port-integration checks are recorded in
[final-checks.json](reviews/task04/final-checks.json). These are local measured
results. The final full repository race suite passed (exit 0; root package
915.557s), and [independent completeness](reviews/task04/completeness.md) is 100%;
[independent correctness](reviews/task04/correctness.md) has no unclosed confirmed
errors. [Validation](reviews/task04/validation.md) records the final gates. No v1 151→104 payload comparison is carried forward as a v2 result.
Throughput, speedup and population quality are not conclusions of this harness.
Storage/performance evidence remains in the task02 review artifacts and
methodology; this protocol does not duplicate that benchmark framework.

For a consumer rollout, opt in on a separate namespace and run shadow proposals
against a versioned domain corpus before enabling a host automatic-review rule.
Retain bad candidate/rejection evidence and compare actual selected revisions,
canonical state and rendered output. Rollback restores the prior host
selection/review policy, while preserving originals and the current durable
revocation ledger; restoring an old index/cache must not resurrect revoked data.

The deterministic invariants and retrieval behavior are established only for the
versioned synthetic corpus and selected adapters. Benefit of memory for a real
LLM task requires a consumer-owned external experiment with reference judgments,
provider configuration and answer evaluation.
