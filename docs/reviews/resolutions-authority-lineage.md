# Round 4 local remediation

C10/C11 closure is independently documented in correctness-authority-lineage.md.
C12 (P2), independently discovered: persisted canonical runtime lineage could
lose reviewed proposal dependencies and then bypass removed ancestor sources.

Contract clarified before remediation in design.md. validRecord now requires
all digest-bound proposal lineage references as an exact subset of canonical
lineage. Additional Duplicate dependencies and revoked tombstones retain their
existing contracts. TestCanonicalCannotDropOrReplaceReviewedLineage covers
drop/record substitution/revision substitution on both stores and Get, Snapshot,
Recall and Project with no delivered payload/provider invocation.

Status: addressed locally; independent closure pending. Full final make validate
and external adversarial module passed, logs checks-reviewed-lineage.log and external-reviewed-lineage.log.
