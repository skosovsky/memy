package memy_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestByteWireRepresentationFailsClosed(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, field := range []string{"payload", "reference"} {
			for _, representation := range []string{"array", "newline"} {
				t.Run(backend+"/"+field+"/"+representation, func(t *testing.T) {
					// Arrange: only the byte representation is corrupted; decoded content is identical.
					f := newFixture(t, raceStore(t, backend))
					p := f.propose(t, "remember", "private", memy.Interval{})
					f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
					if err := f.config.Store.Update(
						context.Background(),
						f.scope,
						func(b memy.Bucket) error { return corruptByteRepresentation(b, field, representation) },
					); err != nil {
						t.Fatal(err)
					}
					// Act.
					record, err := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
					// Assert: schema shape is checked before normalization and digest comparison.
					if !errors.Is(err, memy.ErrSchema) || record.Payload.Value != "" {
						t.Fatalf("record=%+v err=%v", record, err)
					}
				})
			}
		}
	}
}

func corruptByteRepresentation(b memy.Bucket, field, representation string) error {
	entries, err := listEntries(b, "record/")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var doc map[string]json.RawMessage
		if err = json.Unmarshal(entry.Value.Data, &doc); err != nil {
			return err
		}
		var record map[string]json.RawMessage
		if err = json.Unmarshal(doc["data"], &record); err != nil {
			return err
		}
		var proposal map[string]json.RawMessage
		if err = json.Unmarshal(record["proposal"], &proposal); err != nil {
			return err
		}
		if err = mutateByteField(proposal, field, representation); err != nil {
			return err
		}
		record["proposal"], err = json.Marshal(proposal)
		if err != nil {
			return err
		}
		doc["data"], err = json.Marshal(record)
		if err != nil {
			return err
		}
		raw, encodeErr := json.Marshal(doc)
		if encodeErr != nil {
			return encodeErr
		}
		if _, err = b.Put(entry.Key, entry.Value.Version, raw); err != nil {
			return err
		}
	}
	return nil
}

func mutateByteField(proposal map[string]json.RawMessage, field, representation string) error {
	if field == "payload" {
		raw, err := alternateBytes(proposal["payload"], representation)
		if err != nil {
			return err
		}
		proposal["payload"] = raw
		return nil
	}
	var sources []map[string]json.RawMessage
	if err := json.Unmarshal(proposal["sources"], &sources); err != nil {
		return err
	}
	raw, err := alternateBytes(sources[0]["reference"], representation)
	if err != nil {
		return err
	}
	sources[0]["reference"] = raw
	proposal["sources"], err = json.Marshal(sources)
	return err
}

func alternateBytes(raw []byte, representation string) ([]byte, error) {
	var decoded []byte
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	if representation == "newline" {
		encoded := base64.StdEncoding.EncodeToString(decoded)
		return json.Marshal(encoded + "\n")
	}
	numbers := make([]int, len(decoded))
	for i, value := range decoded {
		numbers[i] = int(value)
	}
	return json.Marshal(numbers)
}

type advancingSources struct {
	registry *reference.Registry[sourceRef]
	clock    *reference.Clock
	deadline time.Time
	target   string
	armed    bool
}

func (s *advancingSources) Validate(ctx context.Context, scope memy.Scope, source memy.Source[sourceRef]) error {
	if s.armed && (s.target == "" || source.ID == s.target) {
		s.clock.Set(s.deadline)
	}
	return s.registry.Validate(ctx, scope, source)
}

func advancingFixture(t *testing.T, backend string) (fixture, *advancingSources) {
	t.Helper()
	f := newFixture(t, raceStore(t, backend))
	sources := &advancingSources{registry: f.sources, clock: f.clock, deadline: f.clock.Now().Add(time.Hour)}
	f.config.Sources = sources
	return f, sources
}

func reloadEngine(t *testing.T, f fixture) fixture {
	t.Helper()
	var err error
	f.engine, err = memy.New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDeadlineCrossedDuringCanonicalDelivery(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, method := range []string{"get", "snapshot"} {
			t.Run(backend+"/"+method, func(t *testing.T) {
				// Arrange: source I/O starts before the effective retention deadline.
				f, sources := advancingFixture(t, backend)
				f.config.Retention = reference.Retain[preference]{Version: "retention/v1", ExpiresAt: sources.deadline}
				f = reloadEngine(t, f)
				p := f.propose(t, "remember", "private", memy.Interval{})
				f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
				sources.armed = true
				// Act: the source succeeds, but the clock reaches the deadline during its call.
				if method == "get" {
					record, err := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
					// Assert.
					if !errors.Is(err, memy.ErrStaleInput) || record.Payload.Value != "" {
						t.Fatalf("record=%+v err=%v", record, err)
					}
					return
				}
				records, err := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
				// Assert.
				if !errors.Is(err, memy.ErrStaleInput) || len(records) != 0 {
					t.Fatalf("records=%+v err=%v", records, err)
				}
			})
		}
	}
}

func TestDeadlineCrossedDuringAcceptance(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: the proposal has a finite evidence deadline, independently of authority.
			f, sources := advancingFixture(t, backend)
			f = reloadEngine(t, f)
			suggestion := f.suggestion("private", memy.Interval{})
			suggestion.ExpiresAt = sources.deadline
			p, err := f.engine.Remember(context.Background(), f.actor, f.scope, "remember", "assist", suggestion)
			if err != nil {
				t.Fatal(err)
			}
			sources.armed = true
			// Act.
			acceptance, err := f.engine.Accept(
				context.Background(),
				f.actor,
				f.scope,
				p.ID,
				p.Digest,
				p.Revision,
				"assist",
			)
			// Assert: no acceptance survives the end of the actual evidence I/O.
			if !errors.Is(err, memy.ErrStaleInput) || acceptance.ProposalID != "" {
				t.Fatalf("acceptance=%+v err=%v", acceptance, err)
			}
		})
	}
}

type ancestorRetention struct{ deadline time.Time }

func (r ancestorRetention) Evaluate(_ context.Context, _ memy.Scope, p preference) (memy.Retention, error) {
	result := memy.Retention{PolicyVersion: "retention/v1"}
	if p.Value == "original" {
		result.ExpiresAt = r.deadline
	}
	return result, nil
}

func TestDependencyDeadlineCrossedDuringCommit(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: only an ancestor expires; the candidate has independent unlimited evidence.
			f, sources := advancingFixture(t, backend)
			sources.target = f.source.ID
			f.config.Retention = ancestorRetention{deadline: sources.deadline}
			f = reloadEngine(t, f)
			p := f.propose(t, "remember-original", "original", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, p, memy.Append))
			fresh := f.source
			fresh.ID = "fresh"
			fresh.Reference.URI = "host://fresh"
			if err := f.sources.Put(f.scope, fresh); err != nil {
				t.Fatal(err)
			}
			suggestion := f.suggestion("derived", memy.Interval{})
			suggestion.Sources = []memy.Source[sourceRef]{fresh}
			suggestion.Lineage = []memy.RevisionRef{{RecordID: "original", Revision: 1}}
			derived, err := f.engine.Remember(
				context.Background(),
				f.actor,
				f.scope,
				"remember-derived",
				"assist",
				suggestion,
			)
			if err != nil {
				t.Fatal(err)
			}
			request := f.acceptedRequest(t, "commit-derived", "derived", 0, derived, memy.Append)
			sources.armed = true
			// Act: ancestor source validation crosses its deadline during commit.
			receipt, err := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
			// Assert: no canonical record or receipt is published; acceptance alone is insufficient.
			if !errors.Is(err, memy.ErrStaleInput) || receipt.CanonicalCommitted {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "derived", memy.ReadOptions{})
			if !errors.Is(readErr, memy.ErrNotFound) {
				t.Fatalf("failed commit published: %v", readErr)
			}
		})
	}
}

func TestSnapshotRechecksEarlierRecordAfterLaterSourceIO(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: a finite record is visited before a later unlimited record.
			f, sources := advancingFixture(t, backend)
			sources.target = "fresh"
			f.config.Retention = ancestorRetention{deadline: sources.deadline}
			f = reloadEngine(t, f)
			original := f.propose(t, "remember-a", "original", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit-a", "a", 0, original, memy.Append))
			fresh := f.source
			fresh.ID = "fresh"
			fresh.Reference.URI = "host://fresh"
			if err := f.sources.Put(f.scope, fresh); err != nil {
				t.Fatal(err)
			}
			suggestion := f.suggestion("unlimited", memy.Interval{})
			suggestion.Sources = []memy.Source[sourceRef]{fresh}
			later, err := f.engine.Remember(context.Background(), f.actor, f.scope, "remember-b", "assist", suggestion)
			if err != nil {
				t.Fatal(err)
			}
			f.commit(t, f.acceptedRequest(t, "commit-b", "b", 0, later, memy.Append))
			assertFirstHistoryID(t, f, "a")
			sources.armed = true
			// Act: only the later record's source call advances the clock.
			records, err := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
			// Assert: a final batch gate withholds the earlier, now expired record.
			if !errors.Is(err, memy.ErrStaleInput) || len(records) != 0 {
				t.Fatalf("snapshot=%+v err=%v", records, err)
			}
		})
	}
}

func assertFirstHistoryID(t *testing.T, f fixture, want string) {
	t.Helper()
	if err := f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
		entries, err := listEntries(b, "record/")
		if err != nil {
			return err
		}
		var envelope struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err = json.Unmarshal(entries[0].Value.Data, &envelope); err != nil {
			return err
		}
		if envelope.Data.ID != want {
			t.Fatalf("fixture order: first=%s want=%s", envelope.Data.ID, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTimestampWireFormatMatchesExecutableSchema(t *testing.T) {
	schemas := compileSchemas(t)
	for _, timestamp := range []string{"2026-06-01T00:00:00,0Z", "2026-06-01T01:00:00+00:60", "2026-06-02T00:00:00+24:00"} {
		t.Run(timestamp, func(t *testing.T) {
			timestampWireCase(t, timestamp, func(raw []byte) error {
				_, err := validateWireDocument(raw, schemas)
				return err
			})
		})
	}
}

func replaceRecordedTimestamp(raw []byte, timestamp string) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(doc["data"], &record); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(timestamp)
	if err != nil {
		return nil, err
	}
	record["recorded_at"] = encoded
	doc["data"], err = json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

func timestampWireCase(t *testing.T, timestamp string, rejectSchema func([]byte) error) {
	t.Helper()
	// Arrange: representation normalizes to the original instant in Go, but violates RFC3339.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "private", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
	err := f.config.Store.Update(context.Background(), f.scope, func(b memy.Bucket) error {
		entries, listErr := listEntries(b, "record/")
		if listErr != nil {
			return listErr
		}
		for _, entry := range entries {
			raw, mutationErr := replaceRecordedTimestamp(entry.Value.Data, timestamp)
			if mutationErr != nil {
				return mutationErr
			}
			if schemaErr := rejectSchema(raw); schemaErr == nil {
				t.Fatal("fixture unexpectedly accepted by executable schema")
			}
			if _, writeErr := b.Put(entry.Key, entry.Value.Version, raw); writeErr != nil {
				return writeErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	record, err := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	// Assert: runtime and executable schema both reject these representations.
	if !errors.Is(err, memy.ErrSchema) || record.Payload.Value != "" {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}
