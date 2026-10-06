package memy_test

import (
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
)

func Example() {
	// Consumer-owned domain and authenticated identity types.
	type Preference struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	type User struct{ ID string }
	ctx := context.Background()
	store := memory.New()
	defer func() { _ = store.Close() }()
	scope := memy.Scope{Tenant: "A", Namespace: "preferences", Subject: "alice"}
	actor := User{ID: "alice"}
	policy := reference.NewPolicy(func(user User) string { return user.ID })
	policy.Grant(
		actor.ID,
		scope,
		"policy/v1",
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionRead,
	)
	sources := reference.NewRegistry[string](memy.JSONCodec[string]{})
	source := memy.Source[string]{ID: "source", Revision: "v1", Reference: "host://source"}
	if err := sources.Put(scope, source); err != nil {
		panic(err)
	}
	engine, operationErr := memy.New(memy.Config[Preference, string, User]{
		Store:     store,
		Authority: policy,
		Sources:   sources,
		Clock: reference.NewClock(
			time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		),
		Retention:      reference.Retain[Preference]{Version: "retention/v1"},
		PayloadCodec:   memy.JSONCodec[Preference]{},
		ReferenceCodec: memy.JSONCodec[string]{},
	})
	if operationErr != nil {
		panic(operationErr)
	}
	proposal, operationErr := engine.Remember(
		ctx,
		actor,
		scope,
		"remember-1",
		"assist",
		memy.Suggestion[Preference, string]{Payload: Preference{Key: "timezone", Value: "UTC+7"},
			Sources: []memy.Source[string]{source}, Evidence: "explicit user input", Extractor: "manual/v1"},
	)
	if operationErr != nil {
		panic(operationErr)
	}
	acceptance, operationErr := engine.Accept(
		ctx,
		actor,
		scope,
		proposal.ID,
		proposal.Digest,
		proposal.Revision,
		"assist",
	)
	if operationErr != nil {
		panic(operationErr)
	}
	receipt, operationErr := engine.Commit(
		ctx,
		actor,
		scope,
		"assist",
		memy.CommitRequest{
			OperationID: "commit-1",
			ProposalID:  proposal.ID,
			Acceptance:  acceptance,
			RecordID:    "timezone",
			Reconcile: memy.Reconciliation{
				Mode:          memy.Append,
				PolicyVersion: "resolver/v1",
				Basis:         "first explicit preference",
			},
		},
	)
	if operationErr != nil {
		panic(operationErr)
	}
	record, operationErr := engine.Get(ctx, actor, scope, receipt.RecordID, memy.ReadOptions{Purpose: "assist"})
	if operationErr != nil {
		panic(operationErr)
	}
	fmt.Printf("%s=%s revision=%d\n", record.Payload.Key, record.Payload.Value, record.Revision)
	// Output: timezone=UTC+7 revision=1
}

func ExampleJSONCodec() {
	// A consumer owns the concrete payload and its canonical representation.
	codec := memy.JSONCodec[map[string]string]{}
	encoded, encodeErr := codec.Encode(map[string]string{"value": "UTC+7", "key": "timezone"})
	if encodeErr != nil {
		panic(encodeErr)
	}
	decoded, decodeErr := codec.Decode(encoded)
	if decodeErr != nil {
		panic(decodeErr)
	}
	fmt.Println(codec.Version())
	fmt.Println(string(encoded))
	fmt.Println(decoded["key"], decoded["value"])
	// Output:
	// json/v2
	// {"key":"timezone","value":"UTC+7"}
	// timezone UTC+7
}
