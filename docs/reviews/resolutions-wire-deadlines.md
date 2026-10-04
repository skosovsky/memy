# Round 3 findings: local remediation for round 4

| Finding | Origin | Remediation and evidence | Status |
| --- | --- | --- | --- |
| C10 | Independent correctness reviewer | Accept checks the bound initial decision expiry at its final callback-free deadline gate. TestAcceptanceCannotReturnExpiredInitialAuthorityLease exercises expiry, no acceptance persisted, fresh review and successful commit on both stores. | Addressed locally; independent closure pending |
| C11 | Primary; independently confirmed by correctness reviewer | Commit rejects its target at every transitive lineage level before persisting. TestTransitiveTargetCannotInvalidateItsOwnCommit exercises Append/Supersede rollback, preservation of a1/b1/c1, a safe alternate target and exact replay on both stores. | Addressed locally; independent closure pending |

Full local make validate and the external adversarial module passed. See
checks-authority-lineage.log and external-authority-lineage.log. No final independent closure is
asserted here.
