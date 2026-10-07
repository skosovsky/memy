// Package host is a typed offline consumer of canonical memory and checkpoint ports.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/examples/managed-projections/checkpoint"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/sqlite"
)

const Purpose = "assist"
const purgeBytes = 64 << 20
const purgeLimit = 256

// Query is a consumer-owned structured query, not a core text requirement.
type Query struct{ Refs []memy.RevisionRef }

// Session owns two independent persistent databases and explicit host ports.
type Session struct {
	Engine      *memy.Engine[string, string, string]
	Canonical   *sqlite.Store
	Checkpoints *checkpoint.Store
	Policy      *reference.Policy[string]
	Sources     *reference.Registry[string]
	Clock       *reference.Clock
}

// Open recovers the databases; host grants and source registry are provisioned separately.
func Open(ctx context.Context, dir string) (*Session, error) {
	canonical, err := sqlite.Open(ctx, filepath.Join(dir, "canonical.db"), sqlite.Options{Fault: nil})
	if err != nil {
		return nil, err
	}
	checkpoints, err := checkpoint.Open(ctx, filepath.Join(dir, "checkpoints.db"), "checkpoint")
	if err != nil {
		_ = canonical.Close()
		return nil, err
	}
	policy := reference.NewPolicy(func(identity string) string { return identity })
	sources := reference.NewRegistry[string](memy.JSONCodec[string]{})
	clock := reference.NewClock(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	e, err := memy.New(
		memy.Config[string, string, string]{
			Store:          canonical,
			Authority:      policy,
			Sources:        sources,
			Clock:          clock,
			Retention:      reference.Retain[string]{Version: "retain", ExpiresAt: time.Time{}},
			PayloadCodec:   memy.JSONCodec[string]{},
			ReferenceCodec: memy.JSONCodec[string]{},
			Sinks:          []memy.Sink{checkpoints},
			Reintroduction: nil,
		},
	)
	if err != nil {
		_ = canonical.Close()
		_ = checkpoints.Close()
		return nil, err
	}
	return &Session{
		Engine:      e,
		Canonical:   canonical,
		Checkpoints: checkpoints,
		Policy:      policy,
		Sources:     sources,
		Clock:       clock,
	}, nil
}

// Close closes both databases without deleting their durable files.
func (s *Session) Close() error { return errors.Join(s.Canonical.Close(), s.Checkpoints.Close()) }

// Provision grants distinct writer, reviewer and reader responsibilities.
func (s *Session) Provision(scope memy.Scope) error {
	s.Policy.Grant("writer", scope, "host-policy", memy.ActionPropose)
	s.Policy.Grant("reviewer", scope, "host-policy", memy.ActionAccept, memy.ActionCommit)
	s.Policy.Grant("reader", scope, "host-policy", memy.ActionRead, memy.ActionForget)
	return s.Sources.Put(scope, memy.Source[string]{ID: "source", Revision: "current", Reference: "host://source"})
}

// Seed explicitly remembers, separately reviews and conditionally commits one input.
func (s *Session) Seed(ctx context.Context, scope memy.Scope, id, value string) (memy.RevisionRef, error) {
	proposal, err := s.Engine.Remember(
		ctx,
		"writer",
		scope,
		"remember-"+id,
		Purpose,
		memy.Suggestion[string, string]{
			Payload:       value,
			Sources:       []memy.Source[string]{{ID: "source", Revision: "current", Reference: "host://source"}},
			Evidence:      "explicit host input",
			Extractor:     "host",
			ObservedAt:    s.Clock.Now(),
			Valid:         memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
			ExpiresAt:     time.Time{},
			Lineage:       nil,
			Losses:        nil,
			Uncertainties: nil,
		},
	)
	if err != nil {
		return memy.RevisionRef{}, err
	}
	accepted, err := s.Engine.Accept(ctx, "reviewer", scope, proposal.ID, proposal.Digest, proposal.Revision, Purpose)
	if err != nil {
		return memy.RevisionRef{}, err
	}
	receipt, err := s.Engine.Commit(
		ctx,
		"reviewer",
		scope,
		Purpose,
		memy.CommitRequest{
			OperationID: "commit-" + id,
			ProposalID:  proposal.ID,
			Acceptance:  accepted,
			RecordID:    id,
			Expected:    0,
			Reconcile: memy.Reconciliation{
				Mode:          memy.Append,
				Related:       nil,
				PolicyVersion: "resolver",
				Basis:         "reviewed explicit input",
			},
		},
	)
	return memy.RevisionRef{RecordID: receipt.RecordID, Revision: receipt.Revision}, err
}

// Search returns IDs/revisions and honest rank-only native evidence.
type Search struct{}

func (Search) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: false, BoundedCandidates: true}
}

func (Search) Search(
	ctx context.Context,
	_ memy.Scope,
	q Query,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{}, err
	}
	if options.Minimum != nil {
		return memy.SearchResult{}, memy.ErrUnsupported
	}
	if options.MaxCandidates < 1 || options.MaxCandidates > memy.MaxSearchCandidates {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	result := memy.SearchResult{
		Candidates:          nil,
		Coverage:            []memy.Coverage{{Backend: "host-search", Status: "ready", MinimumSatisfied: false}},
		CandidatesTruncated: len(q.Refs) > options.MaxCandidates,
	}
	for i, ref := range q.Refs[:min(len(q.Refs), options.MaxCandidates)] {
		result.Candidates = append(
			result.Candidates,
			memy.Candidate{
				RecordID: ref.RecordID,
				Revision: ref.Revision,
				Score:    memy.Score{Present: false, Value: 0},
				Signals: []memy.SearchSignal{
					{Backend: "host-search", Rank: i + 1, Score: memy.Score{Present: false, Value: 0}},
				},
			},
		)
	}
	return result, nil
}

// Prepare captures a pre-projection fence and a detached canonical result.
func (s *Session) Prepare(
	ctx context.Context,
	scope memy.Scope,
	refs []memy.RevisionRef,
) (memy.EpochFence, memy.ProjectedRecallResult[string, string], error) {
	fence, err := s.Engine.Fence(ctx, "reader", scope, Purpose)
	if err != nil {
		return fence, memy.ProjectedRecallResult[string, string]{}, err
	}
	result, err := memy.RecallProjected(
		ctx,
		s.Engine,
		"reader",
		scope,
		Query{Refs: refs},
		Search{},
		memy.ScoreRanker[string, string]{},
		memy.RecallOptions{
			Read: memy.ReadOptions{
				Purpose:          Purpose,
				ValidAsOf:        time.Time{},
				RecordedAsOf:     time.Time{},
				IncludeUnknown:   false,
				IncludeConflicts: false,
			},
			Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: nil},
			Limit:  memy.MaxSearchCandidates,
		},
		reference.ProjectorFunc[string, string, string]{
			PolicyVersion: "host-projection",
			Apply:         func(ctx context.Context, r memy.Record[string, string]) (string, error) { return r.Payload, ctx.Err() },
		},
		nil,
	)
	return fence, result, err
}

// Persist materializes the complete envelope and all lineage within a synchronous gate.
func (s *Session) Persist(
	ctx context.Context,
	fence memy.EpochFence,
	handle string,
	result memy.ProjectedRecallResult[string, string],
) (string, error) {
	refs := make([]memy.RevisionRef, 0, len(result.Projections))
	for _, p := range result.Projections {
		if p.Scope != fence.Scope {
			return "", memy.ErrInvalid
		}
		refs = append(refs, memy.RevisionRef{RecordID: p.RecordID, Revision: p.Revision})
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	var key string
	err = s.Engine.WithDerivedWrite(ctx, "reader", fence, Purpose, refs, func(ctx context.Context) error {
		var writeErr error
		key, writeErr = s.Checkpoints.Put(
			ctx,
			fence,
			checkpoint.Artifact{Scope: fence.Scope, Handle: handle, Lineage: refs, Data: raw},
		)
		return writeErr
	})
	return key, err
}

// Forget resumes the same durable operation until each bounded canonical page is covered.
// A pending sink failure is returned to the host scheduler, never called complete.
func (s *Session) Forget(ctx context.Context, scope memy.Scope, ref memy.RevisionRef) (memy.PurgeReceipt, error) {
	request := memy.ForgetRequest{
		OperationID:   "forget-" + ref.RecordID,
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: ref.RecordID},
		Expected:      []memy.RevisionRef{ref},
		Reason:        "host revoked",
		PolicyVersion: "delete",
		Limit:         purgeLimit,
		MaxBytes:      purgeBytes,
	}
	for {
		receipt, err := s.Engine.Forget(ctx, "reader", scope, Purpose, request)
		if err != nil {
			return receipt, err
		}
		unattempted := false
		for _, sink := range receipt.Sinks {
			if !sink.Acknowledged && sink.ErrorCode == "" {
				unattempted = true
			}
		}
		if receipt.State != memy.RevocationCommitted && (receipt.State != memy.PurgePending || !unattempted) {
			return receipt, nil
		}
	}
}
