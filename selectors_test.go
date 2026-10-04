package memy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
)

func TestForgetSelectorsRespectExactAuthorityBoundary(t *testing.T) {
	for _, kind := range []memy.SelectorKind{memy.SelectRecord, memy.SelectSource, memy.SelectSubject, memy.SelectScope} {
		t.Run(string(kind), func(t *testing.T) { forgetSelectorCase(t, kind) })
	}
}

func forgetSelectorCase(t *testing.T, kind memy.SelectorKind) {
	t.Helper()
	// Arrange: A has two independent sources; B shares the physical store.
	f := newFixture(t, nil)
	first := f.propose(t, "remember-first", "first private fact", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-first", "first", 0, first, memy.Append))
	originalSource := f.source.ID
	f.source = memy.Source[sourceRef]{ID: "source-2", Revision: "r1", Reference: sourceRef{URI: "host://source-2"}}
	if putErr := f.sources.Put(f.scope, f.source); putErr != nil {
		t.Fatal(putErr)
	}
	second := f.propose(t, "remember-second", "second private fact", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-second", "second", 0, second, memy.Append))
	foreign := seedForeignRecord(t, f)
	selector := forgetSelector(f.scope, kind, originalSource)
	// Act.
	receipt, forgetErr := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", memy.ForgetRequest{
		OperationID: "forget", Selector: selector, Reason: "host-authorized deletion", PolicyVersion: "deletion/v1",
		Expected: []memy.RevisionRef{{RecordID: "first", Revision: 1}},
	})
	_, firstErr := f.engine.Get(context.Background(), f.actor, f.scope, "first", memy.ReadOptions{})
	secondRecord, secondErr := f.engine.Get(context.Background(), f.actor, f.scope, "second", memy.ReadOptions{})
	foreignRecord, foreignErr := foreign.engine.Get(
		context.Background(),
		foreign.actor,
		foreign.scope,
		"first",
		memy.ReadOptions{},
	)
	_, crossTenantErr := f.engine.Get(context.Background(), f.actor, foreign.scope, "first", memy.ReadOptions{})
	// Assert: the requested boundary closes A; B remains unchanged and concealed.
	if forgetErr != nil || receipt.State != memy.PurgeComplete || !errors.Is(firstErr, memy.ErrNotFound) ||
		foreignErr != nil || foreignRecord.Payload.Value != "B fact" || !errors.Is(crossTenantErr, memy.ErrUnauthorized) {
		t.Fatalf(
			"receipt=%+v err=%v first=%v foreign=%+v err=%v cross=%v",
			receipt,
			forgetErr,
			firstErr,
			foreignRecord,
			foreignErr,
			crossTenantErr,
		)
	}
	allSelected := kind == memy.SelectScope || kind == memy.SelectSubject
	if allSelected && !errors.Is(secondErr, memy.ErrNotFound) {
		t.Fatalf("broad selector left second record: %+v %v", secondRecord, secondErr)
	}
	if !allSelected && (secondErr != nil || secondRecord.Payload.Value != "second private fact") {
		t.Fatalf("narrow selector erased independent source: %+v %v", secondRecord, secondErr)
	}
}

func seedForeignRecord(t *testing.T, original fixture) fixture {
	t.Helper()
	f := original
	f.scope = memy.Scope{Tenant: "B", Namespace: original.scope.Namespace, Subject: "bob"}
	f.actor = principal{actor: "bob"}
	f.policy.Grant(
		"bob",
		f.scope,
		"authority/v1",
		memy.ActionRead,
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
	)
	if putErr := f.sources.Put(f.scope, f.source); putErr != nil {
		t.Fatal(putErr)
	}
	proposal := f.propose(t, "remember-B", "B fact", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-B", "first", 0, proposal, memy.Append))
	return f
}

func forgetSelector(scope memy.Scope, kind memy.SelectorKind, sourceID string) memy.Selector {
	switch kind {
	case memy.SelectRecord:
		return memy.Selector{Kind: kind, ID: "first"}
	case memy.SelectSource:
		return memy.Selector{Kind: kind, ID: sourceID}
	case memy.SelectSubject:
		return memy.Selector{Kind: kind, ID: scope.Subject}
	case memy.SelectScope:
		return memy.Selector{Kind: kind, ID: ""}
	}
	return memy.Selector{}
}
