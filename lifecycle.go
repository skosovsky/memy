package memy

import (
	"context"
	"time"
)

// Action is a host authorization operation, unrelated to executable tools.
type Action string

const (
	ActionRead        Action = "read"
	ActionPropose     Action = "propose"
	ActionAccept      Action = "accept"
	ActionCommit      Action = "commit"
	ActionForget      Action = "forget"
	ActionConsolidate Action = "consolidate"
)

// Access contains only host-owned authorization inputs.
type Access struct {
	Scope   Scope
	Action  Action
	Purpose string
}

// Decision is an authenticated host policy result. Fields nil means full-record
// access; field-restricted profiles require a safe projection capability.
type Decision struct {
	Allowed       bool
	Actor         string
	PolicyVersion string
	Scope         Scope
	Fields        []string
	ExpiresAt     time.Time
}

// Authority validates a consumer-owned authenticated context. A decision must
// be valid for the operation at its check; failures are fail-closed.
type Authority[A any] interface {
	Check(context.Context, A, Access) (Decision, error)
}

// Clock owns recorded-time and expiry evaluation.
type Clock interface{ Now() time.Time }

// Source is a consumer-owned reference with stable host-supplied identity.
type Source[R any] struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Reference R      `json:"reference"`
}

// Sources checks existence/current revision and evidence through consumer types.
type Sources[R any] interface {
	Validate(context.Context, Scope, Source[R]) error
}

// Interval distinguishes unknown validity from a known open-ended interval.
// Endpoints of known intervals form [From,To). Zero endpoints mean unbounded.
type Interval struct {
	Known bool      `json:"known"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

// Validate rejects endpoints on an unknown interval and empty known intervals.
func (i Interval) Validate() error {
	if !i.Known && (!i.From.IsZero() || !i.To.IsZero()) {
		return ErrInterval
	}
	if i.Known && !i.From.IsZero() && !i.To.IsZero() && !i.From.Before(i.To) {
		return ErrInterval
	}
	return nil
}

// Contains never treats unknown as universally applicable.
func (i Interval) Contains(at time.Time) bool {
	return i.Known && (i.From.IsZero() || !at.Before(i.From)) && (i.To.IsZero() || at.Before(i.To))
}

// Retention is a versioned host decision for payload expiry and tombstones.
type Retention struct {
	PolicyVersion string    `json:"policy_version"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// RetentionPolicy returns a concrete stable decision for the payload and policy
// version. Commit/reads revalidate it; Sweep is triggered by the host.
type RetentionPolicy[P any] interface {
	Evaluate(context.Context, Scope, P) (Retention, error)
}

// ProposalState is separate from canonical record state.
type ProposalState string

const (
	Proposed ProposalState = "proposed"
	Accepted ProposalState = "accepted"
	Rejected ProposalState = "rejected"
	Expired  ProposalState = "expired"
)

// RecordState is the canonical lifecycle state.
type RecordState string

const (
	Active     RecordState = "active"
	Superseded RecordState = "superseded"
	Conflicted RecordState = "conflicted"
	Revoked    RecordState = "revoked"
)

// RevisionRef is a content-free lineage reference to exact canonical input.
type RevisionRef struct {
	RecordID string  `json:"record_id"`
	Revision Version `json:"revision"`
}

// ProposalRef binds replay to an immutable proposal content revision.
type ProposalRef struct {
	ID       string  `json:"id"`
	Revision Version `json:"revision"`
}

// Suggestion is provider output; scope and authority are stamped by the engine.
type Suggestion[P, R any] struct {
	Payload       P
	Sources       []Source[R]
	Evidence      string
	Extractor     string
	ObservedAt    time.Time
	Valid         Interval
	ExpiresAt     time.Time
	Lineage       []RevisionRef
	Losses        []string
	Uncertainties []string
}

// Proposal is an independently reviewable typed knowledge candidate.
type Proposal[P, R any] struct {
	ID         string
	Revision   Version
	Digest     string
	Scope      Scope
	State      ProposalState
	Suggestion Suggestion[P, R]
	CreatedAt  time.Time
	Epoch      Version
	Retention  Retention
}

// Acceptance binds a single proposal and a host identity/policy decision.
type Acceptance struct {
	ProposalID       string    `json:"proposal_id"`
	ProposalRevision Version   `json:"proposal_revision"`
	Digest           string    `json:"digest"`
	Actor            string    `json:"actor"`
	Scope            Scope     `json:"scope"`
	PolicyVersion    string    `json:"policy_version"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// Provenance preserves sources, extraction identity and rationale as data.
type Provenance[R any] struct {
	Sources       []Source[R]   `json:"sources"`
	Extractor     string        `json:"extractor"`
	Evidence      string        `json:"evidence"`
	Lineage       []RevisionRef `json:"lineage"`
	Losses        []string      `json:"losses"`
	Uncertainties []string      `json:"uncertainties"`
}

// Record is a detached typed canonical revision. Knowledge is always data.
type Record[P, R any] struct {
	ID            string        `json:"id"`
	Revision      Version       `json:"revision"`
	Scope         Scope         `json:"scope"`
	Payload       P             `json:"payload"`
	State         RecordState   `json:"state"`
	Provenance    Provenance[R] `json:"provenance"`
	ObservedAt    time.Time     `json:"observed_at"`
	RecordedAt    time.Time     `json:"recorded_at"`
	Valid         Interval      `json:"valid"`
	Retention     Retention     `json:"retention"`
	ExpiresAt     time.Time     `json:"expires_at"`
	PolicyVersion string        `json:"policy_version"`
	Epoch         Version       `json:"epoch"`
	Related       []RevisionRef `json:"related"`
}

// VisibilityToken identifies an exact committed revision for index visibility.
type VisibilityToken struct {
	Scope    Scope   `json:"scope"`
	RecordID string  `json:"record_id"`
	Revision Version `json:"revision"`
}

// CommitReceipt certifies canonical state only; it is content-free and durable.
type CommitReceipt struct {
	OperationID        string          `json:"operation_id"`
	RecordID           string          `json:"record_id"`
	Revision           Version         `json:"revision"`
	CanonicalCommitted bool            `json:"canonical_committed"`
	Visibility         VisibilityToken `json:"visibility"`
}

// ReconcileMode is supplied by the domain resolver and accepted host policy.
type ReconcileMode string

const (
	Append    ReconcileMode = "append"
	Duplicate ReconcileMode = "duplicate"
	Supersede ReconcileMode = "supersede"
	Conflict  ReconcileMode = "conflict"
)

// Reconciliation binds related revisions and a versioned resolution basis.
type Reconciliation struct {
	Mode          ReconcileMode
	Related       []RevisionRef
	PolicyVersion string
	Basis         string
}

// CommitRequest links an accepted proposal to a conditional canonical write.
type CommitRequest struct {
	OperationID string
	ProposalID  string
	Acceptance  Acceptance
	RecordID    string
	Expected    Version
	Reconcile   Reconciliation
}

// ReadOptions expresses temporal predicates separately from current privacy.
type ReadOptions struct {
	Purpose          string
	ValidAsOf        time.Time
	RecordedAsOf     time.Time
	IncludeUnknown   bool
	IncludeConflicts bool
}

// Extractor produces typed candidates; it cannot make canonical commits.
type Extractor[I, P, R any] interface {
	Extract(context.Context, I) ([]Suggestion[P, R], error)
}
