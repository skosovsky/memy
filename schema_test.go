package memy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/skosovsky/memy"
)

func compileSchemas(t *testing.T) map[string]*jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.RegisterFormat(&jsonschema.Format{Name: "memy-identifier", Validate: func(value any) error {
		text, ok := value.(string)
		if !ok {
			return nil
		}
		return (memy.Scope{Tenant: text, Namespace: "schema", Subject: "identifier"}).Validate()
	}})
	compiler.AssertFormat()
	compiler.AssertContent()
	paths, operationErr := filepath.Glob("schemas/*-v3.schema.json")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource("https://github.com/skosovsky/memy/"+path, value); err != nil {
			t.Fatal(err)
		}
	}
	compiled := make(map[string]*jsonschema.Schema)
	for _, kind := range []string{"document", "proposal", "record", "acceptance", "operation", "epoch", "revocation", "purge", "membership", "purge-active", "purge-job", "purge-item", "purge-evidence", "sweep-active", "sweep-job"} {
		schema, err := compiler.Compile("https://github.com/skosovsky/memy/schemas/" + kind + "-v3.schema.json")
		if err != nil {
			t.Fatal(err)
		}
		compiled[kind] = schema
	}
	return compiled
}

func TestPersistedLifecycleMatchesWireSchemas(t *testing.T) {
	// Arrange: compile schemas locally; no HTTP loader is needed.
	schemas := compileSchemas(t)
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "UTC+7", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
	seen := make(map[string]bool)

	// Act: inspect content-bearing documents and content-free revocation.
	inspectWireDocuments(t, f, schemas, seen)
	request := memy.ForgetRequest{
		OperationID: "forget",
		Selector:    memy.Selector{Kind: memy.SelectSource, ID: f.source.ID},
		Expected: []memy.RevisionRef{
			{RecordID: "timezone", Revision: 1},
		},
		Reason:        "withdraw",
		PolicyVersion: "deletion/v1",
	}
	request.Limit, request.MaxBytes = 1, 10000
	_, operationErr := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	inspectWireDocuments(t, f, schemas, seen)
	request.Limit = 256
	_, operationErr = fullForget(context.Background(), f.engine, f.actor, f.scope, "assist", request)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	inspectWireDocuments(t, f, schemas, seen)
	_, operationErr = f.engine.Sweep(
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		memy.SweepRequest{OperationID: "schema-sweep", Limit: 1, MaxBytes: 10000},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	inspectWireDocuments(t, f, schemas, seen)
	// Assert: every persisted kind has a real lifecycle-produced instance.
	for _, kind := range []string{"proposal", "record", "acceptance", "operation", "epoch", "revocation", "purge", "membership", "purge-active", "purge-job", "purge-item", "purge-evidence", "sweep-active", "sweep-job"} {
		if !seen[kind] {
			t.Errorf("no lifecycle instance for %s", kind)
		}
	}
}

func inspectWireDocuments(t *testing.T, f fixture, schemas map[string]*jsonschema.Schema, seen map[string]bool) {
	t.Helper()
	viewErr := f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
		entries, listErr := listEntries(b, "")
		if listErr != nil {
			return listErr
		}
		for _, entry := range entries {
			kind, validationErr := validateWireDocument(entry.Value.Data, schemas)
			if validationErr != nil {
				return validationErr
			}
			seen[kind] = true
		}
		return nil
	})
	if viewErr != nil {
		t.Fatal(viewErr)
	}
}

func validateWireDocument(raw []byte, schemas map[string]*jsonschema.Schema) (string, error) {
	instance, decodeErr := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if decodeErr != nil {
		return "", decodeErr
	}
	if validationErr := schemas["document"].Validate(instance); validationErr != nil {
		return "", validationErr
	}
	envelope := instance.(map[string]any)
	kind := envelope["kind"].(string)
	if validationErr := schemas[kind].Validate(envelope["data"]); validationErr != nil {
		return "", validationErr
	}
	return kind, nil
}

func TestWireSchemaRejectsMalformedEnvelope(t *testing.T) {
	// Arrange.
	schema := compileSchemas(t)["document"]
	// Act / Assert: these never become success/zero-valued documents.
	for _, input := range []string{
		`{"kind":"epoch","data":{"value":1,"recorded_at":"2026-01-01T00:00:00Z"}}`,
		`{"schema":1,"kind":"epoch","data":{"value":1,"recorded_at":"2026-01-01T00:00:00Z"}}`,
		`{"schema":4,"kind":"epoch","data":{"value":1,"recorded_at":"2026-01-01T00:00:00Z"}}`,
		`{"schema":3,"kind":"epoch","data":{"value":1}}`,
		`{"schema":3,"kind":"epoch","data":{"value":-1,"recorded_at":"2026-01-01T00:00:00Z"}}`,
		`{"schema":3,"kind":"epoch","data":{"value":1,"recorded_at":"bad"}}`,
		`{"schema":3,"kind":"epoch","data":{"value":1,"recorded_at":"2026-01-01T00:00:00Z","unknown":true}}`,
	} {
		var instance any
		if err := json.Unmarshal([]byte(input), &instance); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(instance); err == nil {
			t.Errorf("accepted malformed document: %s", input)
		}
	}
}
