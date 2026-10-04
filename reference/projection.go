package reference

import (
	"bytes"
	"context"
	"sync"

	"github.com/skosovsky/memy"
)

type artifact struct {
	scope   memy.Scope
	lineage []memy.RevisionRef
	data    []byte
}

// ProjectionSink stores offline summary/cache artifacts with lineage and
// deletion handles. Writes must execute inside Engine.WithDerivedWrite.
type ProjectionSink struct {
	mu        sync.Mutex
	name      string
	artifacts map[string]artifact
	failure   error
}

// NewProjectionSink creates an empty managed projection participant.
func NewProjectionSink(name string) *ProjectionSink {
	return &ProjectionSink{name: name, artifacts: make(map[string]artifact), mu: sync.Mutex{}, failure: nil}
}

// Name identifies the sink in durable purge receipts.
func (s *ProjectionSink) Name() string { return s.name }

// Put stores a detached artifact under a host-owned deletion handle.
func (s *ProjectionSink) Put(
	ctx context.Context,
	handle string,
	scope memy.Scope,
	lineage []memy.RevisionRef,
	data []byte,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handle == "" || len(lineage) == 0 || len(data) == 0 {
		return memy.ErrInvalid
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[handle] = artifact{
		scope:   scope,
		lineage: append([]memy.RevisionRef(nil), lineage...),
		data:    bytes.Clone(data),
	}
	return nil
}

// Contains inspects metadata only; cached payload must be revalidated by core
// before serving. This adapter never exposes a raw payload read API.
func (s *ProjectionSink) Contains(handle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.artifacts[handle]
	return ok
}

// FailPurge injects a deletion failure independently from canonical revoke.
func (s *ProjectionSink) FailPurge(err error) { s.mu.Lock(); defer s.mu.Unlock(); s.failure = err }

// Purge acknowledges only after all selected/dependent artifacts are removed.
func (s *ProjectionSink) Purge(ctx context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	if err := ctx.Err(); err != nil {
		return memy.PurgeAck{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return memy.PurgeAck{}, s.failure
	}
	selected := make(map[string]bool, len(batch.Records))
	for _, id := range batch.Records {
		selected[id] = true
	}
	for handle, value := range s.artifacts {
		if value.scope != batch.Scope {
			continue
		}
		remove := batch.Selector.Kind == memy.SelectScope || batch.Selector.Kind == memy.SelectSubject
		for _, ref := range value.lineage {
			if selected[ref.RecordID] {
				remove = true
			}
		}
		if remove {
			delete(s.artifacts, handle)
		}
	}
	return memy.PurgeAck{Sink: s.name, OperationID: batch.OperationID, Epoch: batch.Epoch}, nil
}

// ProjectorFunc binds an explicit version to a typed offline projection script.
type ProjectorFunc[P, R, O any] struct {
	PolicyVersion string
	Apply         func(context.Context, memy.Record[P, R]) (O, error)
}

// Version identifies the projection/cache representation.
func (p ProjectorFunc[P, R, O]) Version() string { return p.PolicyVersion }

// Project executes the script with already authorized canonical data.
func (p ProjectorFunc[P, R, O]) Project(ctx context.Context, record memy.Record[P, R]) (O, error) {
	if err := ctx.Err(); err != nil {
		var zero O
		return zero, err
	}
	if p.Apply == nil {
		var zero O
		return zero, memy.ErrInvalid
	}
	return p.Apply(ctx, record)
}
