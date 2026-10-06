package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skosovsky/memy/internal/quality"
)

func TestCommandSavesReportBeforeNegativeExit(t *testing.T) {
	for _, tc := range []struct {
		probe string
		code  int
	}{
		{"wrong_payload", 1},
		{"late_privacy", 1},
		{"false_empty_outage", 1},
		{"checker_error", 2},
		{"adapter_error", 2},
	} {
		t.Run(tc.probe, func(t *testing.T) {
			// Arrange: each probe corrupts an observation of the same real fixture.
			out := filepath.Join(t.TempDir(), "report.json")
			var stdout, stderr bytes.Buffer
			// Act.
			code := execute(t.Context(), []string{"-corpus", "../../testdata/quality-v2.json", "-probe", tc.probe, "-out", out}, &stdout, &stderr)
			raw, err := os.ReadFile(out)
			// Assert: failures remain machine-readable evidence, never a missing report.
			if err != nil || code != tc.code || !bytes.Equal(raw, stdout.Bytes()) {
				t.Fatalf("code=%d read=%v diagnostic=%q", code, err, stderr.String())
			}
			var report quality.Report
			if err := json.Unmarshal(raw, &report); err != nil || report.ExitCode() != tc.code {
				t.Fatalf("report classification mismatch: %v", err)
			}
		})
	}
}

func TestCommandExpectedRejectionPassesProtocol(t *testing.T) {
	// Arrange.
	var stdout, stderr bytes.Buffer
	// Act.
	code := execute(t.Context(), []string{"-corpus", "../../testdata/quality-v2.json"}, &stdout, &stderr)
	var report quality.Report
	err := json.Unmarshal(stdout.Bytes(), &report)
	// Assert: a bad candidate rejected by its host is not a failed protocol.
	if code != 0 || err != nil || report.Final != quality.VerdictPass || stderr.Len() != 0 {
		t.Fatalf("code=%d decode=%v diagnostic=%q", code, err, stderr.String())
	}
}

func TestCommandMalformedInputProducesSafeUnknownReport(t *testing.T) {
	// Arrange: malformed user text must never become an error diagnostic.
	const secret = "PRIVATE_FIXTURE_SECRET_DO_NOT_PRINT"
	input := filepath.Join(t.TempDir(), "corpus.json")
	out := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(input, []byte(`{"version":"`+secret+`","unknown":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// Act.
	code := execute(t.Context(), []string{"-corpus", input, "-out", out}, &stdout, &stderr)
	raw, err := os.ReadFile(out)
	// Assert.
	if code != 2 || err != nil || strings.Contains(stdout.String()+stderr.String()+string(raw), secret) {
		t.Fatalf("code=%d read=%v unsafe diagnostics", code, err)
	}
	var report quality.Report
	if json.Unmarshal(raw, &report) != nil || report.Final != quality.VerdictUnknown {
		t.Fatal("invalid input was published as a successful run")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("private writer details") }

func TestCommandOutputFailureIsUnknownAndRedacted(t *testing.T) {
	// Arrange: report creation succeeds, but its requested destination cannot.
	var stdout, stderr bytes.Buffer
	// Act.
	code := execute(t.Context(), []string{"-corpus", "missing-input", "-out", filepath.Join(t.TempDir(), "missing", "report.json")}, &stdout, &stderr)
	streamCode := execute(t.Context(), []string{"-corpus", "missing-input"}, failedWriter{}, &stderr)
	// Assert: no claim of saved evidence or raw error text is printed.
	if code != 2 || streamCode != 2 || strings.Contains(stderr.String(), "private writer details") || !strings.Contains(stderr.String(), "destination unavailable") {
		t.Fatalf("codes=%d,%d diagnostics=%q", code, streamCode, stderr.String())
	}
}

func TestCommandReplacesPublicReportWithPrivateCompleteFile(t *testing.T) {
	// Arrange: a previous report has broader permissions than this command allows.
	out := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(out, []byte("old report"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(out, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// Act: even invalid execution must preserve a safe complete report.
	code := execute(t.Context(), []string{"-corpus", "missing-input", "-out", out}, &stdout, &stderr)
	info, statErr := os.Stat(out)
	raw, readErr := os.ReadFile(out)
	// Assert.
	if code != 2 || statErr != nil || readErr != nil || info.Mode().Perm() != 0o600 || !bytes.Equal(raw, stdout.Bytes()) {
		t.Fatalf("code=%d stat=%v read=%v", code, statErr, readErr)
	}
	var report quality.Report
	if err := json.Unmarshal(raw, &report); err != nil || report.Final != quality.VerdictUnknown {
		t.Fatal("private destination did not contain the complete failure report")
	}
}
