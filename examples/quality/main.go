// Command quality measures the fixed offline corpus and optionally saves JSON.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/skosovsky/memy/internal/quality"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	corpusPath := flag.String("corpus", "testdata/consolidation-v1.json", "versioned synthetic corpus")
	outputPath := flag.String("out", "", "optional JSON report path")
	flag.Parse()
	corpus, operationErr := quality.Load(*corpusPath)
	if operationErr != nil {
		return operationErr
	}
	report, operationErr := quality.Run(context.Background(), corpus)
	if operationErr != nil {
		return operationErr
	}
	raw, operationErr := json.MarshalIndent(report, "", "  ")
	if operationErr != nil {
		return operationErr
	}
	if *outputPath != "" {
		if err := os.WriteFile(*outputPath, append(raw, '\n'), 0o600); err != nil {
			return err
		}
	}
	fmt.Println(string(raw))
	return nil
}
