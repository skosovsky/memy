package reference_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/conformance"
	"github.com/skosovsky/memy/reference"
)

func portScope() memy.Scope { return memy.Scope{Tenant: "A", Namespace: "prefs", Subject: "user"} }

func TestSearchConformance(t *testing.T) {
	for _, profile := range []string{"index", "eventual", "composite"} {
		t.Run(profile, func(t *testing.T) {
			conformance.SearchSuite(t, func(_ *testing.T) conformance.SearchFixture[string] {
				index := reference.NewIndex[string]("index", nil)
				var adapter memy.Search[string] = index
				switch profile {
				case "eventual":
					adapter = reference.Eventual[string]{Index: index}
				case "composite":
					adapter = reference.Composite[string]{Backends: []reference.Backend[string]{{ID: "index", Search: index}}, RRF: reference.RRFConfig{K: 60}, AllowDegraded: false}
				}
				return conformance.SearchFixture[string]{
					Adapter: adapter,
					Query:   "all",
					Seed:    index.Stage,
					Publish: index.Acknowledge,
					Fail:    index.Fail,
				}
			})
		})
	}
}

func TestSinkConformance(t *testing.T) {
	t.Run("index", func(t *testing.T) {
		conformance.SinkSuite(t, func(_ *testing.T) conformance.SinkFixture {
			index := reference.NewIndex[string]("index", nil)
			return conformance.SinkFixture{Adapter: index, Fail: index.FailPurge,
				Seed: func(ctx context.Context, _ string, sc memy.Scope, ref memy.RevisionRef) error {
					if err := index.Stage(
						ctx,
						sc,
						memy.Candidate{RecordID: ref.RecordID, Revision: ref.Revision, Score: 1},
					); err != nil {
						return err
					}
					return index.Acknowledge(
						ctx,
						memy.VisibilityToken{Scope: sc, RecordID: ref.RecordID, Revision: ref.Revision},
					)
				},
				Contains: func(ctx context.Context, _ string, sc memy.Scope, ref memy.RevisionRef) (bool, error) {
					result, err := index.Search(ctx, sc, "all", memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates})
					if err != nil {
						return false, err
					}
					for _, candidate := range result.Candidates {
						if candidate.RecordID == ref.RecordID && candidate.Revision == ref.Revision {
							return true, nil
						}
					}
					return false, nil
				},
			}
		})
	})
	t.Run("projection", func(t *testing.T) {
		conformance.SinkSuite(t, func(_ *testing.T) conformance.SinkFixture {
			sink := reference.NewProjectionSink("projection")
			return conformance.SinkFixture{Adapter: sink, Fail: sink.FailPurge,
				Seed: func(ctx context.Context, handle string, sc memy.Scope, ref memy.RevisionRef) error {
					return sink.Put(ctx, handle, sc, []memy.RevisionRef{ref}, []byte("private"))
				},
				Contains: func(_ context.Context, handle string, _ memy.Scope, _ memy.RevisionRef) (bool, error) {
					return sink.Contains(handle), nil
				},
			}
		})
	})
}

func TestHostPortConformance(t *testing.T) {
	t.Run("codec", func(t *testing.T) {
		conformance.CodecSuite(
			t,
			memy.JSONCodec[string]{},
			[]string{"host", ""},
			func(a, b string) bool { return a == b },
			[][]byte{[]byte(`{`), []byte(`"x" "y"`)},
		)
	})
	t.Run("authority", func(t *testing.T) {
		conformance.AuthoritySuite(t, func(_ *testing.T) conformance.AuthorityFixture[string] {
			policy := reference.NewPolicy(func(a string) string { return a })
			access := memy.Access{Scope: portScope(), Action: memy.ActionRead, Purpose: "assist"}
			policy.Grant("owner", access.Scope, "v1", memy.ActionRead)
			return conformance.AuthorityFixture[string]{
				Adapter: policy,
				Allowed: "owner",
				Denied:  "foreign",
				Access:  access,
				Actor:   "owner",
				Fail:    policy.Fail,
			}
		})
	})
	t.Run("sources", func(t *testing.T) {
		conformance.SourcesSuite(t, func(_ *testing.T) conformance.SourcesFixture[string] {
			registry := reference.NewRegistry[string](memy.JSONCodec[string]{})
			return conformance.SourcesFixture[string]{
				Adapter:     registry,
				Scope:       portScope(),
				Original:    memy.Source[string]{ID: "source", Revision: "r1", Reference: "host://source"},
				Replacement: memy.Source[string]{ID: "source", Revision: "r2", Reference: "host://source"},
				Put:         registry.Put,
				Remove:      registry.Remove,
				Fail:        registry.Fail,
			}
		})
	})
	t.Run("clock", func(t *testing.T) {
		clock := reference.NewClock(time.Time{})
		conformance.ClockSuite(t, clock, clock.Set)
	})
}

func TestTypedCallbackConformance(t *testing.T) {
	for _, port := range []string{"extractor", "resolver", "ranker", "projector", "consolidator", "reintroduction", "retention"} {
		t.Run(port, func(t *testing.T) {
			conformance.CallbackSuite(
				t,
				func(_ *testing.T) conformance.CallbackFixture { return callbackFixture(port) },
			)
		})
	}
}

func callbackFixture(port string) conformance.CallbackFixture {
	var failure error
	valid := false
	provider := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return failure
	}
	invoke := callbackInvocation(port, provider, func(value bool) { valid = value })
	var fail func(error)
	if port != "ranker" && port != "retention" {
		fail = func(err error) { failure = err }
	}
	return conformance.CallbackFixture{Invoke: invoke, Fail: fail, Verify: func(t *testing.T) {
		t.Helper()
		if !valid {
			t.Fatal("typed port result mismatch")
		}
	}}
}

func callbackInvocation(
	port string,
	provider func(context.Context) error,
	verify func(bool),
) func(context.Context) error {
	switch port {
	case "extractor":
		adapter := reference.ExtractorFunc[string, string, string](
			func(ctx context.Context, input string) ([]memy.Suggestion[string, string], error) {
				if err := provider(ctx); err != nil {
					return nil, err
				}
				return []memy.Suggestion[string, string]{{Payload: input}}, nil
			},
		)
		return func(ctx context.Context) error {
			result, err := adapter.Extract(ctx, "typed")
			verify(len(result) == 1 && result[0].Payload == "typed")
			return err
		}
	case "resolver":
		adapter := reference.ResolverFunc[string, string, string](
			func(ctx context.Context, key string, _ memy.Proposal[string, string], _ []memy.Record[string, string]) (memy.CommitTarget, error) {
				if err := provider(ctx); err != nil {
					return memy.CommitTarget{}, err
				}
				return memy.CommitTarget{RecordID: key}, nil
			},
		)
		return func(ctx context.Context) error {
			result, err := adapter.Resolve(ctx, "claim", memy.Proposal[string, string]{}, nil)
			verify(result.RecordID == "claim")
			return err
		}
	case "projector":
		adapter := reference.ProjectorFunc[string, string, string]{
			PolicyVersion: "v1",
			Apply: func(ctx context.Context, r memy.Record[string, string]) (string, error) {
				if err := provider(ctx); err != nil {
					return "", err
				}
				return r.Payload, nil
			},
		}
		return func(ctx context.Context) error {
			result, err := adapter.Project(ctx, memy.Record[string, string]{Payload: "typed"})
			verify(result == "typed" && adapter.Version() == "v1")
			return err
		}
	case "consolidator":
		return mergeInvocation(provider, verify)
	case "reintroduction":
		adapter := reference.ReintroductionFunc[string](
			func(ctx context.Context, a string, sc memy.Scope, r memy.ReintroductionRequest) error {
				if err := provider(ctx); err != nil {
					return err
				}
				verify(a == "host" && sc == portScope() && r.PolicyVersion == "v1")
				return nil
			},
		)
		return func(ctx context.Context) error {
			return adapter.Allow(ctx, "host", portScope(), memy.ReintroductionRequest{PolicyVersion: "v1"})
		}
	case "ranker":
		adapter := memy.ScoreRanker[string, string]{}
		return func(ctx context.Context) error {
			result, err := adapter.Rank(
				ctx,
				[]memy.Ranked[string, string]{
					{Record: memy.Record[string, string]{ID: "low"}, Score: 1},
					{Record: memy.Record[string, string]{ID: "high"}, Score: 2},
				},
			)
			verify(len(result) == 2 && result[0].Record.ID == "high" && result[0].Explanation != "")
			return err
		}
	case "retention":
		adapter := reference.Retain[string]{Version: "v1"}
		return func(ctx context.Context) error {
			result, err := adapter.Evaluate(ctx, portScope(), "typed")
			verify(result.PolicyVersion == "v1")
			return err
		}
	}
	return func(_ context.Context) error { return errors.New("unknown callback port") }
}

func mergeInvocation(provider func(context.Context) error, verify func(bool)) func(context.Context) error {
	adapter := reference.MergeFunc[string, string](
		func(ctx context.Context, inputs []memy.Record[string, string], _ memy.Budget) (memy.MergeResult[string, string], error) {
			if err := provider(ctx); err != nil {
				return memy.MergeResult[string, string]{}, err
			}
			return memy.MergeResult[string, string]{
				Suggestions: []memy.Suggestion[string, string]{{Payload: inputs[0].Payload}},
				Utility:     1,
			}, nil
		},
	)
	return func(ctx context.Context) error {
		result, err := adapter.Merge(ctx, []memy.Record[string, string]{{Payload: "typed"}}, memy.Budget{})
		verify(len(result.Suggestions) == 1 && result.Suggestions[0].Payload == "typed")
		return err
	}
}
