package memy

import (
	"context"
	"errors"
)

// SweepRequest starts or resumes one durable maintenance pass.
type SweepRequest struct {
	OperationID     string
	Limit, MaxBytes int
}

// SweepResult reports this call only; the host owns aggregate progress.
type SweepResult struct {
	Records          []PurgeReceipt
	ExpiredProposals int
	Work             int
	Complete         bool
}
type sweepJobDisk struct {
	Scope          Scope  `json:"scope"`
	OperationID    string `json:"operation_id"`
	Digest         string `json:"digest"`
	Phase          string `json:"phase"`
	After          string `json:"after"`
	ResumeAfter    string `json:"resume_after"`
	Origin         string `json:"origin"`
	PurgeOperation string `json:"purge_operation"`
}

func activeSweepGate(b Bucket, scope Scope) error {
	var active activePurgeDisk
	_, err := readDocument(b, "active_sweep", "sweep-active", &active)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if active.Scope != scope {
		return ErrSchema
	}
	return ErrRevoked
}
func boundActiveSweep(b Bucket, job sweepJobDisk) (Version, error) {
	var active activePurgeDisk
	version, err := readDocument(b, "active_sweep", "sweep-active", &active)
	if errors.Is(err, ErrNotFound) {
		return 0, ErrSchema
	}
	if err != nil {
		return 0, err
	}
	if active.Scope != job.Scope || active.OperationID != job.OperationID {
		return 0, ErrSchema
	}
	return version, nil
}

// Sweep freezes canonical mutation for a bounded, durable retention pass.
func (e *Engine[P, R, A]) Sweep(ctx context.Context, authority A, scope Scope, purpose string, request SweepRequest) (SweepResult, error) {
	if !validIdentifier(request.OperationID) || request.Limit < 1 || request.Limit > 1024 || request.MaxBytes <= 0 {
		return SweepResult{}, ErrInvalid
	}
	decision, err := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if err != nil {
		return SweepResult{}, err
	}
	plan, err := operationDigest(scope, decision.Actor, purpose, struct{ OperationID string }{request.OperationID})
	if err != nil {
		return SweepResult{}, err
	}
	var result SweepResult
	for result.Work < request.Limit && !result.Complete {
		var pending string
		err = e.raw.Update(ctx, scope, func(b Bucket) error {
			key := objectKey("sweep_job", request.OperationID)
			var job sweepJobDisk
			version, readErr := readDocument(b, key, "sweep-job", &job)
			if errors.Is(readErr, ErrNotFound) {
				if err := activeSweepGate(b, scope); err != nil {
					return err
				}
				epoch, ev, err := currentEpoch(b)
				if err != nil {
					return err
				}
				if epoch.Value >= MaxVersion {
					return ErrConflict
				}
				epoch.Value++
				epoch.RecordedAt = recordedTime(e.config.Clock.Now(), epoch.RecordedAt)
				if epoch.RecordedAt.IsZero() {
					return ErrInvalid
				}
				if err = writeDocument(b, "epoch", "epoch", ev, epoch); err != nil {
					return err
				}
				active, err := b.Get("active_sweep")
				if err != nil {
					return err
				}
				if err = writeDocument(b, "active_sweep", "sweep-active", active.Version, activePurgeDisk{scope, request.OperationID}); err != nil {
					return err
				}
				job = sweepJobDisk{Scope: scope, OperationID: request.OperationID, Digest: plan, Phase: "records"}
			} else if readErr != nil {
				return readErr
			}
			if job.Scope != scope || job.OperationID != request.OperationID {
				return ErrSchema
			}
			if job.Digest != plan {
				return ErrConflict
			}
			if job.Phase == "done" {
				var active activePurgeDisk
				_, pointerErr := readDocument(b, "active_sweep", "sweep-active", &active)
				if pointerErr != nil && !errors.Is(pointerErr, ErrNotFound) {
					return pointerErr
				}
				if pointerErr == nil && (active.Scope != scope || active.OperationID == job.OperationID) {
					return ErrSchema
				}
				result.Complete = true
				return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
			}
			activeVersion, err := boundActiveSweep(b, job)
			if err != nil {
				return err
			}
			var active activePurgeDisk
			_, err = readDocument(b, "active_purge", "purge-active", &active)
			if err == nil {
				if active.Scope != scope {
					return ErrSchema
				}
				job.PurgeOperation = active.OperationID
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			if job.PurgeOperation != "" {
				var receipt PurgeReceipt
				_, err := readDocument(b, objectKey("purge", job.PurgeOperation), "purge", &receipt)
				if err != nil {
					return err
				}
				if err := validatePurgeBinding(receipt, scope, job.PurgeOperation); err != nil {
					return err
				}
				var purgeJob purgeJobDisk
				if _, err := readDocument(b, objectKey("purge_job", job.PurgeOperation), "purge-job", &purgeJob); err != nil {
					return err
				}
				if err := validatePurgeStage(purgeJob, receipt); err != nil {
					return err
				}
				if receipt.State == PurgeComplete {
					if active.OperationID == job.PurgeOperation {
						return ErrSchema
					}
					job.PurgeOperation = ""
				} else {
					pending = job.PurgeOperation
				}
			}
			if pending == "" {
				prefix := "record/"
				if job.Phase == "proposals" {
					prefix = "proposal_revision/"
				}
				if job.Phase == "origin" {
					prefix = objectKey("proposal_revision", job.Origin) + "/"
				}
				page, err := b.Scan(ScanOptions{Prefix: prefix, After: job.After, Limit: 1, MaxBytes: request.MaxBytes})
				if err != nil {
					return err
				}
				result.Work++
				if len(page.Entries) > 0 {
					entry := page.Entries[0]
					switch job.Phase {
					case "records":
						target, expired, err := e.expiredRecord(ctx, b, scope, e.config.Clock.Now(), entry)
						if err != nil {
							return err
						}
						if expired {
							op, err := digest(struct {
								Pass  string
								Scope Scope
								Ref   RevisionRef
							}{request.OperationID, scope, target.Ref})
							if err != nil {
								return err
							}
							req := ForgetRequest{OperationID: op, Selector: Selector{Kind: SelectRecord, ID: target.Ref.RecordID}, Expected: []RevisionRef{target.Ref}, Reason: "retention expiry", PolicyVersion: target.PolicyVersion}
							dg, err := operationDigest(scope, decision.Actor, purpose, req)
							if err != nil {
								return err
							}
							if err = e.beginPurge(ctx, b, authority, scope, purpose, decision, req, dg); err != nil {
								return err
							}
							job.PurgeOperation = op
							pending = op
						}
						job.After = entry.Key
					case "proposals":
						var proposal proposalDisk
						if err = decodeDocument(entry.Value.Data, "proposal", &proposal); err != nil {
							return err
						}
						if proposal.Scope != scope || entry.Key != proposalRevisionKey(ProposalRef{ID: proposal.ID, Revision: proposal.Revision}) {
							return ErrSchema
						}
						current, err := e.currentRetention(ctx, scope, proposal)
						if err != nil {
							return err
						}
						if proposalExpired(proposal, e.config.Clock.Now()) || retentionExpired(current, e.config.Clock.Now()) {
							job.ResumeAfter = entry.Key
							job.Origin = proposal.ID
							job.Phase = "origin"
							job.After = ""
						} else {
							job.After = entry.Key
						}
					case "origin":
						var proposal proposalDisk
						if err = decodeDocument(entry.Value.Data, "proposal", &proposal); err != nil {
							return err
						}
						if proposal.Scope != scope || proposal.ID != job.Origin || entry.Key != proposalRevisionKey(ProposalRef{ID: proposal.ID, Revision: proposal.Revision}) {
							return ErrSchema
						}
						if _, err = b.Delete(entry.Key, entry.Value.Version); err != nil {
							return err
						}
						job.After = entry.Key
					default:
						return ErrSchema
					}
				}
				// Fresh suffix scans consume empty pages, so transitions do not hide work.
				if len(page.Entries) == 0 {
					switch job.Phase {
					case "records":
						job.Phase = "proposals"
						job.After = ""
					case "proposals":
						job.Phase = "done"
						result.Complete = true
						if _, err = b.Delete("active_sweep", activeVersion); err != nil {
							return err
						}
					case "origin":
						for _, kind := range []string{"proposal", "acceptance"} {
							key := objectKey(kind, job.Origin)
							value, err := b.Get(key)
							if err != nil {
								return err
							}
							if value.Data != nil {
								if _, err = b.Delete(key, value.Version); err != nil {
									return err
								}
							}
						}
						result.ExpiredProposals++
						job.Phase = "proposals"
						job.After = job.ResumeAfter
						job.ResumeAfter = ""
						job.Origin = ""
					default:
						return ErrSchema
					}
				}
			}
			if err = e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); err != nil {
				return err
			}
			return writeDocument(b, key, "sweep-job", version, job)
		})
		if err != nil {
			return SweepResult{}, err
		}
		if pending != "" {
			if result.Work >= request.Limit {
				break
			}
			receipt, used, err := e.advancePurge(ctx, authority, scope, purpose, ForgetRequest{OperationID: pending, Limit: request.Limit - result.Work, MaxBytes: request.MaxBytes})
			if err != nil {
				return result, err
			}
			result.Work += used
			if receipt.State != RevocationCommitted {
				budget := request.Limit - result.Work
				receipt, err = e.finishPurge(ctx, authority, scope, purpose, pending, budget)
				if err != nil {
					return result, err
				}
				// Conservatively charge the supplied callback budget; no hidden retry loop.
				result.Work += budget
			}
			result.Records = append(result.Records, receipt)
			if receipt.State == PurgeFailed || receipt.State == PurgePending {
				break
			}
		}
	}
	return result, nil
}
