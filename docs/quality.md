# Offline memory quality protocol

Corpus: `testdata/consolidation-v1.json`, tea/v1. Reproduce:
`go run ./examples/quality -out docs/quality-report.json`. This is a consumer
experiment; core has no dependency on it or evaly. Every trial starts with
isolated memory/v2 atomic/CAS state. SQLite durability has separate lifecycle
tests. The report records toolchain, extractor, resolver and projection versions.

## Comparison

Each trial runs actual typed extraction, distinct host acceptance, conditional
commit, acknowledged deterministic indexing and scoped canonical recall.
Baseline has three A records: sometimes tea, never tea in the evening and
a duplicate of sometimes tea. A fourth record is seeded by B authority into
the same physical store/index. A cannot read B.

Expected records are exact Preference values and known half-open valid
intervals. Both unique A records must survive. Independent canonical snapshots
must retain all three original revision identities; response text alone cannot
prove memory updates.

Load and Run validate the scripted corpus assumptions before creating a store:
versioned policies, unique source IDs, matching bounded intervals, an A
duplicate and negation, isolated B input, and exact unique A reference facts.
An empty or contradictory corpus returns invalid input without a partial report.

Exact dedup proposes one replacement for the duplicate pair. Typed domain
merge proposes two distinct claims. The host checks candidate quality, accepts
successful proposals through the ordinary lifecycle, and uses a separate
context index for selected revisions. Originals remain canonical. Both indices
are managed deletion sinks.

The scripted semantic provider deliberately changes the claim to always tea,
loses the evening negation and omits the valid interval. Candidate metrics are
saved, the host rejects the proposal and effective recall stays baseline.
This is a negative result for this offline script, not a commercial-model
evaluation. Model choice and external graders belong to the host.

## Gates and measurements

Acceptance requires all exact expected records, zero false memories, zero
privacy leaks, zero lost mandatory negations and zero lost valid intervals.
Savings cannot compensate for any failure. Privacy checks examine scope and
provenance and independently call the public Snapshot API against B using A
identity. Original canonical revision identities are checked after each trial.

`input_bytes` and `candidate_bytes` measure encoded consumer payload only,
excluding provenance/envelope/index overhead. `provider_cost_units` is an
abstract scripted-provider unit, not tokens or currency. `proposal_latency_ns`
covers consolidation proposal generation; baseline has no such phase and
reports zero. `recall_latency_ns` measures actual canonical recall after review.
The saved timings are from one local run, without a statistical speed claim.

## Saved run

`quality-report.json`, Go 1.27.1: baseline/exact/domain matched 2/2 expected
records, with zero deterministic privacy/constraint violations. Exact/domain
candidate payload: 104 bytes versus baseline 151, with all three originals
retained. Semantic candidate had one false memory, one lost negation and one
lost interval. Review rejected it; effective context retained baseline records.
Auto-apply remains false.

## Rollout and rollback

Opt in on a separate namespace; run shadow proposals and review against a
versioned consumer corpus before enabling a product-level automatic host rule.
Compare selected revisions, canonical state and rendered output separately.
Keep negative evaluation results when a provider/policy fails to improve quality.
Rollback restores the previous versioned context-selection/ranking policy;
originals remain available. Never roll back or discard the current durable
revocation ledger, including during stale index/cache restoration.

These gates prove the synthetic fixture results. Wider domain quality needs
the consumer's corpus, reference judgments and provider evaluation.
