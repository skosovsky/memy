# Round 1 findings: implementation changes for re-review

Status: addressed locally, independent closure pending. Original findings and
snapshot remain preserved in correctness-initial.md and completeness-initial.md.

| Finding | Change | Added behavioral evidence |
|---|---|---|
| C1 | Sources.Validate on canonical delivery and every transitive dependency; repeated after rank/project callbacks | TestCanonicalReadsRevalidateSource (memory/SQLite, replacement/remove/outage); TestSourceRevalidatedAfterReadCallbacks; TestFreshDirectSourceCannotValidateStaleTransitiveLineage (accept/commit) |
| C2 | Acceptance binding uses scalar identity fields and ExpiresAt.Equal, including repeat Accept | TestAcceptanceDeadlineSurvivesPersistence (memory/SQLite, monotonic expiry, repeat Accept) |
| C3 | Get verifies physical history key against envelope ID/revision | TestHistoryKeyAndExactWireFieldBinding/revision |
| C4 | Exact field-name inventory rejects leftovers, including root and nested case aliases | TestHistoryKeyAndExactWireFieldBinding/root_alias,nested_alias |
| C5 | Encoded complete input transfer envelopes and stamped suggestion content count toward budget, including codec-encoded references and all annotations; overflow-safe accumulation | TestConsolidationBudgetIncludesProviderAnnotations (provider invoked, 1MiB Evidence rejected by 2048B output budget, originals retained) |
| C6 | Index without a minimum token reports eventual or known pending coverage; complete is scoped to an acknowledged requested minimum | conformance.SearchSuite/scope_coverage,minimum_and_cancel on Index, Eventual, Composite |
| BOOT-13 | Added common Search, Sink, Codec, Authority, Sources, Clock and typed callback suites with consumer-owned fixtures | reference.TestSearchConformance; TestSinkConformance; TestHostPortConformance; TestTypedCallbackConformance |

The external round 1 audit module was rerun after the six functional changes:
`go test -mod=mod -count=1 ./...` in /private/tmp/memy-correctness-round1 passed.
That local rerun does not replace independent re-review. Its original tiny input
budget can now reject before the provider; the new output-budget regression
separately verifies the provider is invoked before rejecting oversized evidence.
The new lineage regression exercises acceptance and commit with independent
fresh direct evidence, so an early input budget rejection cannot mask C1.

The original 84 rows are preserved. BOOT-13 comes from task line 15 and raises the
denominator to 85. No final percentage is asserted before independent reports.
