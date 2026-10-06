// Command quality runs the versioned offline protocol and saves safe evidence.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/quality"
)

const commandTimeout = 5 * time.Minute

func main() { os.Exit(runMain()) }

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("quality", flag.ContinueOnError)
	// Parse errors can include supplied values; diagnostics are fixed below.
	flags.SetOutput(io.Discard)
	corpusPath := flags.String("corpus", "testdata/quality-v2.json", "versioned fixture manifest")
	outputPath := flags.String("out", "", "optional safe JSON report path")
	probe := flags.String("probe", "", "isolated evaluator mutation probe")
	parseErr := flags.Parse(args)
	var report quality.Report
	if parseErr != nil || flags.NArg() != 0 {
		report = quality.FailureReport(memy.ErrInvalid)
	} else {
		corpus, err := quality.Load(*corpusPath)
		if err != nil {
			report = quality.FailureReport(err)
		} else {
			switch {
			case *probe != "":
				report, _ = quality.RunProbe(ctx, corpus, *probe)
			default:
				report, _ = quality.Run(ctx, corpus)
			}
		}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "quality: report serialization failed")
		return 2
	}
	raw = append(raw, '\n')
	writeFailed := false
	if *outputPath != "" {
		if err := saveReport(*outputPath, raw); err != nil {
			fmt.Fprintln(stderr, "quality: report destination unavailable")
			writeFailed = true
		}
	}
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "quality: report stream unavailable")
		return 2
	}
	if writeFailed {
		return 2
	}
	code := report.ExitCode()
	switch code {
	case 1:
		fmt.Fprintln(stderr, "quality: mandatory checkpoint failed")
	case 2:
		fmt.Fprintln(stderr, "quality: execution or evaluation unknown")
	}
	return code
}

// Replace the destination with a private, fully written file. Reusing an older
// report must not retain its broader permissions or publish truncated JSON.
func saveReport(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".quality-report-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
