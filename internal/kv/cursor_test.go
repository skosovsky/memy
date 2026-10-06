package kv

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/skosovsky/memy"
)

func cursorFixture() (Binding, memy.ScanOptions) {
	scope := memy.Scope{
		Tenant:    strings.Repeat("<", 1024),
		Namespace: strings.Repeat("&", 1024),
		Subject:   strings.Repeat("\"", 1024),
	}
	return Binding{Secret: []byte("fixture-secret"), Scope: scope.Key(), Generation: memy.MaxVersion},
		memy.ScanOptions{Limit: 1, MaxBytes: 10000, Plan: strings.Repeat("\x00", 1024)}
}

func TestCursorMaximumWireBound(t *testing.T) {
	// Arrange.
	binding, options := cursorFixture()
	options.Prefix = strings.Repeat("\xff", maximumKeyBytes-1)
	options.After = options.Prefix + "\x01"
	last := options.Prefix + "\xff"
	// Act.
	cursor, err := binding.Next(options, last)
	if err != nil {
		t.Fatal(err)
	}
	options.Cursor = cursor
	resumed, err := binding.Resume(options)
	// Assert: the derived formula and emitted worst case agree exactly.
	if maxCursorBytes != 5563 || len(cursor) != maxCursorBytes || err != nil || resumed != last {
		t.Fatalf("bound=%d size=%d resumed=%v err=%v", maxCursorBytes, len(cursor), resumed == last, err)
	}
}

func signedFixtureCursor(binding Binding, raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(
		raw,
	) + "." + base64.RawURLEncoding.EncodeToString(
		binding.signCursor(raw),
	)
}

func TestCursorRejectsExternalAndOldFormats(t *testing.T) {
	// Arrange: even authenticated obsolete formats are stale, never permissively read.
	binding, options := cursorFixture()
	for name, fixture := range map[string]struct {
		cursor string
		want   error
	}{
		"old":             {signedFixtureCursor(binding, []byte(`{"scope":"old-format"}`)), memy.ErrStaleCursor},
		"unknown_version": {signedFixtureCursor(binding, []byte{3}), memy.ErrStaleCursor},
		"oversized_old":   {signedFixtureCursor(binding, []byte(strings.Repeat("a", maxCursorBytes))), memy.ErrInvalid},
		"empty_payload":   {signedFixtureCursor(binding, nil), memy.ErrInvalid},
		"short_payload":   {signedFixtureCursor(binding, []byte{cursorVersion}), memy.ErrInvalid},
		"no_separator":    {"bad", memy.ErrInvalid}, "bad_base64": {"?.?", memy.ErrInvalid},
		"short_mac": {"YQ.YQ", memy.ErrInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			// Act.
			options.Cursor = fixture.cursor
			_, err := binding.Resume(options)
			// Assert.
			if !errors.Is(err, fixture.want) {
				t.Fatalf("got %v want %v", err, fixture.want)
			}
		})
	}
}

func TestCursorRejectsForgeryAndInvalidContinuation(t *testing.T) {
	// Arrange.
	binding, options := cursorFixture()
	cursor, err := binding.Next(options, "last")
	if err != nil {
		t.Fatal(err)
	}
	// Act / Assert: an intact MAC binds both data and the generation.
	options.Cursor = cursor
	other := binding
	other.Generation--
	if _, err = other.Resume(options); !errors.Is(err, memy.ErrStaleCursor) {
		t.Fatalf("generation: %v", err)
	}
	other = binding
	other.Secret = []byte("different-instance")
	if _, err = other.Resume(options); !errors.Is(err, memy.ErrStaleCursor) {
		t.Fatalf("MAC: %v", err)
	}
	for _, last := range []string{"", strings.Repeat("a", maximumKeyBytes+1), "\x00"} {
		if issued, issueErr := binding.Next(options, last); !errors.Is(issueErr, memy.ErrInvalid) || issued != "" {
			t.Fatalf("issued invalid last key: %v", issueErr)
		}
	}
}

func FuzzIssuedCursorRoundtrip(f *testing.F) {
	for _, seed := range [][]byte{{0xff}, []byte("binary"), []byte(strings.Repeat("<", maximumKeyBytes)), {0x00, 0xfe, 0xff}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		// Arrange: arbitrary admitted binary key, bounded independently from scope/plan.
		last := append([]byte(nil), input[:min(len(input), maximumKeyBytes)]...)
		if len(last) == 0 {
			last = []byte{1}
		}
		for i := range last {
			if last[i] == 0 {
				last[i] = 1
			}
		}
		binding, options := cursorFixture()
		options.Prefix = string(last[:len(last)/2])
		// Act.
		cursor, err := binding.Next(options, string(last))
		if err != nil {
			t.Fatal(err)
		}
		options.Cursor = cursor
		options.Limit = 1024
		options.MaxBytes++
		resumed, err := binding.Resume(options)
		// Assert: every issued cursor stays bounded and resumes exact bytes.
		if err != nil || resumed != string(last) || len(cursor) > maxCursorBytes {
			t.Fatalf("roundtrip: %v size=%d", err, len(cursor))
		}
	})
}
