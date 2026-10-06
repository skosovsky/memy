package memy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

type activePurgeDisk struct {
	Scope       Scope  `json:"scope"`
	OperationID string `json:"operation_id"`
}
type purgeJobDisk struct {
	Scope       Scope  `json:"scope"`
	OperationID string `json:"operation_id"`
	Phase       string `json:"phase"`
	Resume      string `json:"resume"`
	After       string `json:"after"`
	Queued      uint64 `json:"queued"`
	Next        uint64 `json:"next"`
	Clean       uint64 `json:"clean"`
	Part        string `json:"part"`
	Origin      string `json:"origin"`
}
type purgeItemDisk struct {
	Scope       Scope   `json:"scope"`
	OperationID string  `json:"operation_id"`
	ID          string  `json:"id"`
	Revision    Version `json:"revision"`
	Kind        string  `json:"kind"`
}

// lifecycleStore enforces the durable temporary scope fence on Engine canonical
// access. Raw Store access remains privileged; purge maintenance uses Engine.raw.
type lifecycleStore struct{ Store }

func (s lifecycleStore) View(ctx context.Context, scope Scope, fn func(Bucket) error) error {
	return s.Store.View(ctx, scope, func(b Bucket) error {
		if err := activePurgeGate(b, scope); err != nil {
			return err
		}
		if err := activeSweepGate(b, scope); err != nil {
			return err
		}
		return fn(b)
	})
}
func (s lifecycleStore) FencedView(ctx context.Context, scope Scope, fn func(Bucket) error) error {
	return s.Store.FencedView(ctx, scope, func(b Bucket) error {
		if err := activePurgeGate(b, scope); err != nil {
			return err
		}
		if err := activeSweepGate(b, scope); err != nil {
			return err
		}
		return fn(b)
	})
}
func (s lifecycleStore) Update(ctx context.Context, scope Scope, fn func(Bucket) error) error {
	return s.Store.Update(ctx, scope, func(b Bucket) error {
		if err := activePurgeGate(b, scope); err != nil {
			return err
		}
		if err := activeSweepGate(b, scope); err != nil {
			return err
		}
		return fn(b)
	})
}
func activePurgeGate(b Bucket, scope Scope) error {
	var active activePurgeDisk
	_, err := readDocument(b, "active_purge", "purge-active", &active)
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
func jobPrefix(id string) string             { return objectKey("purge_work", id) + "/" }
func selectedJobKey(op, id string) string    { return jobPrefix(op) + objectKey("selected", id) }
func queueJobKey(op string, n uint64) string { return fmt.Sprintf("%squeue/%020d", jobPrefix(op), n) }
func originJobKey(op, id string) string      { return jobPrefix(op) + objectKey("origins", id) }

func (e *Engine[P, R, A]) beginPurge(ctx context.Context, b Bucket, authority A, scope Scope, purpose string, decision Decision, request ForgetRequest, requestDigest string) error {
	_, opVersion, exists, err := operation(b, request.OperationID, "forget", requestDigest)
	if err != nil {
		return err
	}
	if exists {
		return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
	}
	if err := activePurgeGate(b, scope); err != nil {
		return err
	}
	if err := expectedForgetRevisions(b, scope, request.Expected); err != nil {
		return err
	}
	epoch, epochVersion, err := currentEpoch(b)
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
	if err := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); err != nil {
		return err
	}
	if err := writeDocument(b, "epoch", "epoch", epochVersion, epoch); err != nil {
		return err
	}
	if err := persistRevocation(b, request, epoch.Value); err != nil {
		return err
	}
	activeVersion, err := b.Get("active_purge")
	if err != nil {
		return err
	}
	if err := writeDocument(b, "active_purge", "purge-active", activeVersion.Version, activePurgeDisk{scope, request.OperationID}); err != nil {
		return err
	}
	job := purgeJobDisk{Scope: scope, OperationID: request.OperationID, Phase: "seed", Next: 1, Clean: 1, Part: "history"}
	if err := writeDocument(b, objectKey("purge_job", request.OperationID), "purge-job", 0, job); err != nil {
		return err
	}
	receipt := PurgeReceipt{Batch: PurgeBatch{OperationID: request.OperationID, Scope: scope, Epoch: epoch.Value, Selector: request.Selector, Records: []string{}}, State: RevocationCommitted, RevokedAt: epoch.RecordedAt, Sinks: []SinkResult{}}
	for _, sink := range e.config.Sinks {
		receipt.Sinks = append(receipt.Sinks, SinkResult{Name: sink.Name()})
	}
	if err := writeDocument(b, objectKey("purge", request.OperationID), "purge", 0, receipt); err != nil {
		return err
	}
	return writeDocument(b, objectKey("operation", request.OperationID), "operation", opVersion, operationDisk{Action: "forget", Digest: requestDigest})
}

func enqueuePurge(b Bucket, job *purgeJobDisk, id string) error {
	key := selectedJobKey(job.OperationID, id)
	value, err := b.Get(key)
	if err != nil {
		return err
	}
	if value.Data != nil {
		return nil
	}
	var head recordDisk
	_, err = readDocument(b, objectKey("head", id), "record", &head)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if head.Scope != job.Scope || head.ID != id {
		return ErrSchema
	}
	if job.Queued >= uint64(MaxVersion) {
		return ErrConflict
	}
	job.Queued++
	item := purgeItemDisk{Scope: job.Scope, OperationID: job.OperationID, ID: id, Revision: head.Revision, Kind: "record"}
	if err := writeDocument(b, key, "purge-item", value.Version, item); err != nil {
		return err
	}
	return writeDocument(b, queueJobKey(job.OperationID, job.Queued), "purge-item", 0, item)
}
func jobItem(b Bucket, job purgeJobDisk, n uint64) (purgeItemDisk, error) {
	var item purgeItemDisk
	_, err := readDocument(b, queueJobKey(job.OperationID, n), "purge-item", &item)
	if err == nil && (item.Scope != job.Scope || item.OperationID != job.OperationID || item.Kind != "record") {
		err = ErrSchema
	}
	return item, err
}
func jobScan(b Bucket, job *purgeJobDisk, prefix string, remaining *int, maxBytes int) (ScanPage, error) {
	page, err := b.Scan(ScanOptions{Prefix: prefix, After: job.After, Limit: *remaining, MaxBytes: maxBytes})
	if err != nil {
		return page, err
	}
	*remaining -= max(1, len(page.Entries))
	if len(page.Entries) > 0 {
		job.After = page.Entries[len(page.Entries)-1].Key
	}
	return page, nil
}
func addPurgeOrigin(b Bucket, job purgeJobDisk, id string) error {
	key := originJobKey(job.OperationID, id)
	value, err := b.Get(key)
	if err != nil {
		return err
	}
	if value.Data != nil {
		return nil
	}
	return writeDocument(b, key, "purge-item", value.Version, purgeItemDisk{Scope: job.Scope, OperationID: job.OperationID, ID: id, Kind: "proposal"})
}
func readyPurgeChunk(job *purgeJobDisk, receipt *PurgeReceipt, records []string, done bool) error {
	if receipt.Batch.Chunk >= uint64(MaxVersion) {
		return ErrConflict
	}
	receipt.Batch.Chunk++
	receipt.Batch.Records = records
	receipt.CanonicalComplete = done
	receipt.State = PurgePending
	for i := range receipt.Sinks {
		receipt.Sinks[i] = SinkResult{Name: receipt.Sinks[i].Name}
	}
	job.Resume = job.Phase
	job.Phase = "ack"
	return nil
}

func (e *Engine[P, R, A]) advancePurge(ctx context.Context, authority A, scope Scope, purpose string, request ForgetRequest) (PurgeReceipt, int, error) {
	decision, err := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if err != nil {
		return PurgeReceipt{}, 0, err
	}
	var receipt PurgeReceipt
	used := 0
	err = e.raw.Update(ctx, scope, func(b Bucket) error {
		receiptVersion, err := readDocument(b, objectKey("purge", request.OperationID), "purge", &receipt)
		if err != nil {
			return err
		}
		if err := validatePurgeBinding(receipt, scope, request.OperationID); err != nil {
			return err
		}
		var job purgeJobDisk
		jobVersion, err := readDocument(b, objectKey("purge_job", request.OperationID), "purge-job", &job)
		if err != nil {
			return err
		}
		if job.Scope != scope || job.OperationID != request.OperationID {
			return ErrSchema
		}
		if err := validatePurgeStage(job, receipt); err != nil {
			return err
		}
		if receipt.State == PurgeComplete {
			var active activePurgeDisk
			_, err := readDocument(b, "active_purge", "purge-active", &active)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			if err == nil && (active.Scope != scope || active.OperationID == job.OperationID) {
				return ErrSchema
			}
			return nil
		}
		if _, err := boundActivePurge(b, job); err != nil {
			return err
		}
		if job.Phase == "ack" {
			return nil
		}
		receipt.Batch.Records = []string{}
		remaining := request.Limit
		records := make([]string, 0)
		for remaining > 0 && job.Phase != "ack" && job.Phase != "done" {
			if err := ctx.Err(); err != nil {
				return err
			}
			switch job.Phase {
			case "seed":
				if receipt.Batch.Selector.Kind == SelectRecord {
					remaining--
					if err := enqueuePurge(b, &job, receipt.Batch.Selector.ID); err != nil {
						return err
					}
					job.Phase = "expand"
					job.After = ""
					continue
				}
				prefix := "head/"
				if receipt.Batch.Selector.Kind == SelectSource {
					prefix = membershipPrefix("source", receipt.Batch.Selector.ID)
				}
				page, err := jobScan(b, &job, prefix, &remaining, request.MaxBytes)
				if err != nil {
					return err
				}
				for _, entry := range page.Entries {
					id := ""
					if receipt.Batch.Selector.Kind == SelectSource {
						m, err := readMembership(b, scope, entry, "source", receipt.Batch.Selector.ID)
						if err != nil {
							return err
						}
						if err := validateMembership(b, scope, m, job); err != nil {
							return err
						}
						id = m.Ref.RecordID
					} else {
						var head recordDisk
						if err := decodeDocument(entry.Value.Data, "record", &head); err != nil {
							return err
						}
						if head.Scope != scope || entry.Key != objectKey("head", head.ID) {
							return ErrSchema
						}
						id = head.ID
					}
					if err := enqueuePurge(b, &job, id); err != nil {
						return err
					}
				}
				if page.Complete {
					job.Phase = "expand"
					job.After = ""
				}
			case "expand":
				if job.Next > job.Queued {
					job.Phase = "records"
					job.After = ""
					continue
				}
				item, err := jobItem(b, job, job.Next)
				if err != nil {
					return err
				}
				page, err := jobScan(b, &job, membershipPrefix("lineage", item.ID), &remaining, request.MaxBytes)
				if err != nil {
					return err
				}
				for _, entry := range page.Entries {
					m, err := readMembership(b, scope, entry, "lineage", item.ID)
					if err != nil {
						return err
					}
					if err := validateMembership(b, scope, m, job); err != nil {
						return err
					}
					if err := enqueuePurge(b, &job, m.Ref.RecordID); err != nil {
						return err
					}
				}
				if page.Complete {
					job.Next++
					job.After = ""
				}
			case "records":
				if job.Clean > job.Queued {
					job.Phase = "proposals"
					job.After = ""
					continue
				}
				item, err := jobItem(b, job, job.Clean)
				if err != nil {
					return err
				}
				switch job.Part {
				case "history":
					page, err := jobScan(b, &job, objectKey("record", item.ID)+"/", &remaining, request.MaxBytes)
					if err != nil {
						return err
					}
					for _, entry := range page.Entries {
						var r recordDisk
						if err := decodeDocument(entry.Value.Data, "record", &r); err != nil {
							return err
						}
						if r.Scope != scope || r.ID != item.ID || entry.Key != revisionKey(r.ID, r.Revision) {
							return ErrSchema
						}
						if err := addPurgeOrigin(b, job, r.Proposal.ID); err != nil {
							return err
						}
						if _, err := b.Delete(entry.Key, entry.Value.Version); err != nil {
							return err
						}
					}
					if page.Complete {
						job.Part = "members"
						job.After = ""
					}
				case "members":
					page, err := jobScan(b, &job, membershipPrefix("record", item.ID), &remaining, request.MaxBytes)
					if err != nil {
						return err
					}
					for _, entry := range page.Entries {
						var m membershipDisk
						if err := decodeDocument(entry.Value.Data, "membership", &m); err != nil {
							return err
						}
						if m.Scope != scope || m.Ref.RecordID != item.ID || entry.Key != membershipRecordKey(m) {
							return ErrSchema
						}
						value, err := b.Get(membershipKey(m))
						if err != nil {
							return err
						}
						if !bytes.Equal(value.Data, entry.Value.Data) {
							return ErrSchema
						}
						if _, err := b.Delete(membershipKey(m), value.Version); err != nil {
							return err
						}
						if _, err := b.Delete(entry.Key, entry.Value.Version); err != nil {
							return err
						}
					}
					if page.Complete {
						job.Part = "finish"
						job.After = ""
					}
				case "finish":
					remaining--
					var head recordDisk
					version, err := readDocument(b, objectKey("head", item.ID), "record", &head)
					if err != nil {
						return err
					}
					if head.Scope != scope || head.ID != item.ID || head.Revision != item.Revision {
						return ErrSchema
					}
					if head.State != Revoked {
						if head.Revision >= MaxVersion {
							return ErrConflict
						}
						tombstone := recordDisk{ID: item.ID, Revision: head.Revision + 1, Scope: scope, State: Revoked, InitialState: Revoked, RecordedAt: receipt.RevokedAt}
						if err := writeDocument(b, objectKey("head", item.ID), "record", version, tombstone); err != nil {
							return err
						}
					}
					job.Clean++
					job.Part = "history"
					job.After = ""
				default:
					return ErrSchema
				}
			case "proposals":
				page, err := jobScan(b, &job, "proposal_revision/", &remaining, request.MaxBytes)
				if err != nil {
					return err
				}
				for _, entry := range page.Entries {
					var p proposalDisk
					if err := decodeDocument(entry.Value.Data, "proposal", &p); err != nil {
						return err
					}
					if p.Scope != scope || entry.Key != proposalRevisionKey(ProposalRef{ID: p.ID, Revision: p.Revision}) {
						return ErrSchema
					}
					selected := selectedProposal(p, receipt.Batch.Selector, nil)
					for _, ref := range p.Lineage {
						value, err := b.Get(selectedJobKey(job.OperationID, ref.RecordID))
						if err != nil {
							return err
						}
						selected = selected || value.Data != nil
					}
					if selected {
						if err := addPurgeOrigin(b, job, p.ID); err != nil {
							return err
						}
					}
				}
				if page.Complete {
					job.Phase = "origins"
					job.After = ""
				}
			case "origins":
				if job.Origin == "" {
					page, err := b.Scan(ScanOptions{Prefix: jobPrefix(job.OperationID) + "origins/", Limit: 1, MaxBytes: request.MaxBytes})
					if err != nil {
						return err
					}
					remaining--
					if len(page.Entries) == 0 {
						job.Phase = "emit"
						job.Next = 1
						receipt.CanonicalComplete = true
						continue
					}
					var item purgeItemDisk
					if err := decodeDocument(page.Entries[0].Value.Data, "purge-item", &item); err != nil {
						return err
					}
					if item.Scope != scope || item.OperationID != job.OperationID || item.Kind != "proposal" || page.Entries[0].Key != originJobKey(job.OperationID, item.ID) {
						return ErrSchema
					}
					job.Origin = item.ID
					job.After = ""
					continue
				}
				page, err := jobScan(b, &job, objectKey("proposal_revision", job.Origin)+"/", &remaining, request.MaxBytes)
				if err != nil {
					return err
				}
				for _, entry := range page.Entries {
					var proposal proposalDisk
					if err := decodeDocument(entry.Value.Data, "proposal", &proposal); err != nil {
						return err
					}
					if proposal.Scope != scope || proposal.ID != job.Origin || entry.Key != proposalRevisionKey(ProposalRef{ID: proposal.ID, Revision: proposal.Revision}) {
						return ErrSchema
					}
					if _, err := b.Delete(entry.Key, entry.Value.Version); err != nil {
						return err
					}
				}
				if page.Complete {
					for _, kind := range []string{"proposal", "acceptance"} {
						key := objectKey(kind, job.Origin)
						value, err := b.Get(key)
						if err != nil {
							return err
						}
						if value.Data != nil {
							if _, err := b.Delete(key, value.Version); err != nil {
								return err
							}
						}
					}
					key := originJobKey(job.OperationID, job.Origin)
					value, err := b.Get(key)
					if err != nil {
						return err
					}
					if _, err := b.Delete(key, value.Version); err != nil {
						return err
					}
					job.Origin = ""
					job.After = ""
				}
			case "emit":
				for remaining > 0 && job.Next <= job.Queued {
					item, err := jobItem(b, job, job.Next)
					if err != nil {
						return err
					}
					records = append(records, item.ID)
					job.Next++
					remaining--
				}
				if job.Next > job.Queued {
					job.Phase = "done"
				}
				if err := readyPurgeChunk(&job, &receipt, records, true); err != nil {
					return err
				}
				records = nil

			default:
				return ErrSchema
			}
		}
		used = request.Limit - remaining
		if len(records) > 0 {
			if err := readyPurgeChunk(&job, &receipt, records, false); err != nil {
				return err
			}
		}
		if err := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); err != nil {
			return err
		}
		if err := writeDocument(b, objectKey("purge_job", job.OperationID), "purge-job", jobVersion, job); err != nil {
			return err
		}
		return writeDocument(b, objectKey("purge", job.OperationID), "purge", receiptVersion, receipt)
	})
	if err != nil {
		return PurgeReceipt{}, 0, err
	}
	return receipt, used, nil
}

func finishPurgeJob(b Bucket, receipt *PurgeReceipt) error {
	var job purgeJobDisk
	version, err := readDocument(b, objectKey("purge_job", receipt.Batch.OperationID), "purge-job", &job)
	if err != nil {
		return err
	}
	if job.Scope != receipt.Batch.Scope || job.OperationID != receipt.Batch.OperationID || job.Phase != "ack" {
		return ErrSchema
	}
	activeVersion, err := boundActivePurge(b, job)
	if err != nil {
		return err
	}
	if receipt.CanonicalComplete && job.Resume == "done" {
		job.Phase = "done"
		receipt.State = PurgeComplete
		if _, err := b.Delete("active_purge", activeVersion); err != nil {
			return err
		}
	} else {
		job.Phase = job.Resume
		receipt.State = RevocationCommitted
	}
	job.Resume = ""
	return writeDocument(b, objectKey("purge_job", job.OperationID), "purge-job", version, job)
}

func boundActivePurge(b Bucket, job purgeJobDisk) (Version, error) {
	var active activePurgeDisk
	version, err := readDocument(b, "active_purge", "purge-active", &active)
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

func validatePurgeStage(job purgeJobDisk, receipt PurgeReceipt) error {
	if job.Scope != receipt.Batch.Scope || job.OperationID != receipt.Batch.OperationID {
		return ErrSchema
	}
	switch job.Phase {
	case "done":
		if receipt.State != PurgeComplete || !receipt.CanonicalComplete || job.Clean != job.Queued+1 || job.Next != job.Queued+1 {
			return ErrSchema
		}
	case "emit", "ack":
		if !receipt.CanonicalComplete || job.Clean != job.Queued+1 {
			return ErrSchema
		}
		if job.Phase == "emit" && receipt.State != RevocationCommitted {
			return ErrSchema
		}
		if job.Phase == "ack" && receipt.State != PurgePending && receipt.State != PurgeFailed {
			return ErrSchema
		}
	default:
		if receipt.CanonicalComplete || receipt.State != RevocationCommitted {
			return ErrSchema
		}
	}
	return nil
}
