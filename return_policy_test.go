package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies(t *testing.T) {
	// Arrange: the visible index refers to private content; safe field projection is unsupported.
	f, index, summary := withSinks(t)
	receipt, _ := staged(t, f, index, summary)
	if ackErr := index.Acknowledge(context.Background(), receipt.Visibility); ackErr != nil {
		t.Fatal(ackErr)
	}
	f.policy.RestrictFields(f.actor.actor, f.scope, []string{"key"})
	called := false
	ranker := rankerFunc[preference, sourceRef](
		func(_ context.Context, records []memy.Ranked[preference, sourceRef]) ([]memy.Ranked[preference, sourceRef], error) {
			called = true
			return records, nil
		},
	)
	projector := reference.ProjectorFunc[preference, sourceRef, string]{PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, record memy.Record[preference, sourceRef]) (string, error) {
			called = true
			return record.Payload.Value, nil
		}}
	// Act.
	recalled, recallErr := memy.Recall(context.Background(), f.engine, f.actor, f.scope, "timezone", index, ranker,
		memy.RecallOptions{Limit: 1})
	projected, projectionErr := memy.Project(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
		projector,
	)
	exported, marshalErr := json.Marshal(struct {
		Recall     memy.RecallResult[preference, sourceRef] `json:"recall"`
		Projection memy.Projection[string, sourceRef]       `json:"projection"`
	}{Recall: recalled, Projection: projected})
	// Assert: closed fields cannot affect ranking, summary or serialized output.
	if !errors.Is(recallErr, memy.ErrUnsupported) || !errors.Is(projectionErr, memy.ErrUnsupported) ||
		called || marshalErr != nil || strings.Contains(string(exported), "secret preference") {
		t.Fatalf(
			"recall=%v projection=%v called=%t export=%s err=%v",
			recallErr,
			projectionErr,
			called,
			exported,
			marshalErr,
		)
	}
}

func TestProjectionRechecksAuthorityAfterConsumerCallback(t *testing.T) {
	// Arrange: the external policy changes while the trusted projection runs.
	f := newFixture(t, nil)
	proposal := f.propose(t, "remember", "private fact", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, proposal, memy.Append))
	projector := reference.ProjectorFunc[preference, sourceRef, string]{PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, record memy.Record[preference, sourceRef]) (string, error) {
			f.policy.Grant(f.actor.actor, f.scope, "authority/v2", memy.ActionRead)
			return record.Payload.Value, nil
		}}
	// Act.
	result, projectionErr := memy.Project(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
		projector,
	)
	// Assert: the outdated decision cannot publish the provider's otherwise valid output.
	if !errors.Is(projectionErr, memy.ErrStaleAcceptance) || result.Output != "" || result.CacheKey != "" {
		t.Fatalf("projection=%+v err=%v", result, projectionErr)
	}
}

func TestProjectionCacheIdentityBindsReadContext(t *testing.T) {
	// Arrange: two authorized subjects share the record but have distinct host decisions.
	f := newFixture(t, nil)
	proposal := f.propose(t, "remember", "fact", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, proposal, memy.Append))
	f.policy.Grant("bob", f.scope, "authority/v1", memy.ActionRead)
	projector := reference.ProjectorFunc[preference, sourceRef, string]{PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, record memy.Record[preference, sourceRef]) (string, error) {
			return record.Payload.Value, nil
		}}
	// Act.
	keys := make(map[string]bool)
	for _, read := range []struct {
		actor      principal
		purpose    string
		projection string
		policy     string
	}{
		{f.actor, "assist", "projection/v1", "authority/v1"},
		{principal{actor: "bob"}, "assist", "projection/v1", "authority/v1"},
		{f.actor, "export", "projection/v1", "authority/v1"},
		{f.actor, "assist", "projection/v2", "authority/v1"},
		{f.actor, "assist", "projection/v1", "authority/v2"},
	} {
		f.policy.Grant(read.actor.actor, f.scope, read.policy, memy.ActionRead)
		projector.PolicyVersion = read.projection
		projected, projectionErr := memy.Project(context.Background(), f.engine, read.actor, f.scope, "timezone",
			memy.ReadOptions{Purpose: read.purpose}, projector)
		// Assert: differing actor, purpose, host policy or representation cannot share cached authority.
		if projectionErr != nil || projected.CacheKey == "" || keys[projected.CacheKey] || projected.Output != "fact" {
			t.Fatalf("projection=%+v err=%v", projected, projectionErr)
		}
		keys[projected.CacheKey] = true
	}
}
