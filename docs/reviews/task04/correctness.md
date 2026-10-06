# Task04 correctness acceptance

**Accepted. Confirmed unclosed errors: 0.** Reviewer did not participate in implementation and did not edit production code, repository tests or commit changes.

## Scope and contract

Reviewed the task04 specification, v2 design/report contracts, all `internal/quality` implementation and tests, executable schemas and manifest, both command examples, final golden report and quality/migration documentation. The harness remains a consumer of public APIs, with two fixture-owned payload/source/query domains and no evaluation API, model SDK, executable procedure dispatcher or classifier added to core.

Candidate quality, host review, actual exact-revision recall, independent canonical observations and actual projected output are separately checked. Required checkpoints and version pins are enforced; meaningful seeds/repeats change actual fixture execution. Unknown adapter/evaluator outcomes cannot become successful absence or rejection. The eight required groups run offline. Real foreign canonical state, adversarial foreign candidates and preservation after memory/SQLite forget are checked. Diagnostics expose fixed safe evidence without raw payload, query, source reference or provider error text. Scripted results retain explicit limits and do not claim LLM quality or universal poisoning protection.

## Confirmed findings and closure

| Initial finding | Correction and independent evidence |
| --- | --- |
| Small valid context budgets caused an out-of-range access to the first packed projection and lost the CLI report | Payload metrics iterate over actual projections; metadata-only delivery produces safe failed/unknown evidence. Compiled low-budget CLI runs save complete reports. |
| Poisoning could report success when its second search returned an empty projected body | Rendered cardinality, exact identities and contents have an independent oracle. A temporary Go overlay reproduces the changed projected search while retaining successful effective recall; the mandatory rendered gate now fails. |
| A grader could mutate the delivered projection slice after exact-output and budget checks | Grader input is deeply detached; cancellation and current canonical state are checked after the callback. An independent race-enabled overlay mutates output, nested preconditions and source reference; delivered measurements remain identical to the baseline. |
| Abstention rendered checks inferred output from Recall | Actual RecallProjected observations independently distinguish healthy empty, withheld conflict and outage. Required projected checkpoints are enforced. |
| Evaluator mutations indexed a missing projection when their real baseline could not fit the budget | Readiness requires independently verified exact record, projected body, byte receipt, canonical state and successful candidate/review gates. An insufficient baseline preserves observations and unknown/not-attempted probe evidence; RunProbe retains that safe parent report. |
| An errored projection's zero-value result was reported as measured context | ContextDelivered distinguishes a real successful body from a projection error. Errored baselines have unavailable context measurement; metadata-only real bodies and later mutations of previously delivered bodies retain honest known measurements. |
| Reusing a 0644 report retained its public permissions | Atomic same-directory replacement publishes a fully written private file with mode 0600. Independent replacement/error tests and CLI report/stdout equality checks pass. |
| The new corpus lacked actual foreign canonical state | A dedicated principal/scope/source fixture now checks denied Get/Snapshot, foreign search filtering, exact authorized output, independently preserved foreign canonical data and preservation after forget in both stores. |

These were closed before final acceptance. Independent overlays were confined to `/private/tmp`; they did not alter repository tests.

## Final verification

- Full repository gate: `go test -race -count=1 -timeout=20m ./...` passes, exit 0, including the unchanged 10,000-node graph regression. Root package: 915.557s. Evidence: `race.txt`, `final-race-result.json` and `validation.md`.
- Root final quality/CLI/integration race passes: quality 129.795s, CLI 36.361s. Independent complete targeted race also passes: quality 124.611s, CLI 33.342s. Elapsed times reflect concurrent local execution and are not performance assertions.
- Vet, format, diff and lifecycle/retrieval/consumer-integration example checks all pass; vet/format/diff output is empty. Evidence: `final-checks.json` and referenced outputs.
- Independent compiled normal output is byte-identical to the saved golden report. It contains 16 plans / 32 passing parent reports and 14 nested negative probes: ten failed, four unknown. Actual scenario versions match the registered manifest.
- All seven independent direct mutation commands have the documented real exits: payload/revision/lineage/privacy/false-empty violations exit 1; checker/adapter uncertainty exits 2. The 14-case root CLI matrix also checks malformed input, low budgets, private persistence and equal normal reruns.
- Independent final compiled budget matrix verifies complete saved JSON equal to stdout and mode 0600: budgets 1/200/500/700/900 exit 2; 1100/1300 exit 1; the default budget exits 0. Budgets 1/200 correctly have unavailable context measurements. Evidence: `correctness-cli-checks.json`.
- SHA256 independently matches all 127 files in `source-hashes.json` and all 121 Go/module/schema/manifest files in `correctness-final-snapshot.json`; no drift exists between the reviewed implementation and successful full gate.

Acceptance applies to this source snapshot and the stated synthetic corpus. The final status-only changes to `docs/acceptance.md` and `docs/quality.md` were independently rechecked: QH04 is accepted, the full gate and reviewer results are linked accurately, and the local ignored task checklist is complete with commit pending. All 127 current hashes match; all 121 implementation/module/schema/corpus snapshot hashes remain unchanged. This final documentation delta requires no new implementation or broad test run. User edits to Makefile and scripts/release.sh are outside the accepted change.
