package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckStages(t *testing.T) {
	for _, environmentFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "aggregate", true: "environment"}[environmentFails], func(t *testing.T) {
			// Arrange: override tools with failure fixtures, while exercising the real check recipe.
			dir := t.TempDir()
			prepareCheckFixture(t, dir, environmentFails)
			// Act
			out, runErr := command(t, dir, nil, "make", "-j8", "check", "TEST_FLAGS=-run=Nothing")
			// Assert
			if runErr == nil {
				t.Fatal("failed checks returned success")
			}
			if environmentFails {
				if strings.Contains(out, "fixture-lint") {
					t.Fatal("stages started after prerequisite failure")
				}
				return
			}
			assertCheckStages(t, out)
		})
	}
}

func prepareCheckFixture(t *testing.T, dir string, environmentFails bool) {
	t.Helper()
	makefile, err := os.ReadFile(filepath.Join(rootDir(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "Makefile"), string(makefile), 0o644)
	environmentCommand := "true"
	if environmentFails {
		environmentCommand = "false"
	}
	stub := "\n.PHONY: environment inventory lint test examples test-integration\nenvironment:\n\t@" + environmentCommand + "\ninventory:\n\t@true\nlint:\n\t@echo fixture-lint; false\ntest:\n\t@echo fixture-test $(TEST_FLAGS); false\nexamples:\n\t@echo fixture-examples\ntest-integration:\n\t@echo fixture-integration; false\n"
	file, err := os.OpenFile(filepath.Join(dir, "Makefile"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(stub)
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func assertCheckStages(t *testing.T, out string) {
	t.Helper()
	markers := []string{"fixture-lint", "fixture-test", "fixture-examples", "fixture-integration"}
	last := -1
	for _, marker := range markers {
		index := strings.Index(out, marker)
		if index <= last {
			t.Fatalf("missing or unordered stage %q\n%s", marker, out)
		}
		last = index
	}
	for _, marker := range []string{"fixture-test -race -count=1", "check: exit 1"} {
		if !strings.Contains(out, marker) {
			t.Fatalf("missing strict flags or exit %q\n%s", marker, out)
		}
	}
}
