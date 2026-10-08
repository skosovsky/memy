package tools_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func validateInventory(actual, expected, published []string) error {
	actual = slices.Clone(actual)
	expected = slices.Clone(expected)
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("tracked modules %v != development inventory %v", actual, expected)
	}
	if !slices.Equal(published, []string{"."}) {
		return fmt.Errorf("unsupported publishable modules: %v", published)
	}
	return nil
}

func TestModuleInventory(t *testing.T) {
	// Arrange: Make defines inventory; Git supplies tracked manifests.
	root := rootDir(t)
	expected := strings.Fields(mustRun(t, root, nil, "make", "--no-print-directory", "print-development-modules"))
	published := strings.Fields(mustRun(t, root, nil, "make", "--no-print-directory", "print-publishable-modules"))
	tracked := strings.Fields(mustRun(t, root, nil, "git", "ls-files", "--", "go.mod", "**/go.mod"))
	actual := make([]string, 0, len(tracked))
	for _, path := range tracked {
		actual = append(actual, filepath.ToSlash(filepath.Dir(path)))
	}
	// Act / Assert
	if err := validateInventory(actual, expected, published); err != nil {
		t.Fatal(err)
	}
	for _, module := range expected {
		if _, err := os.Stat(filepath.Join(root, module, "go.mod")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInventoryRejectsMissingAndPublishedTooling(t *testing.T) {
	for _, test := range []struct {
		name                        string
		actual, expected, published []string
	}{
		{"missing", []string{".", "tools", "forgotten"}, []string{".", "tools"}, []string{"."}},
		{"tooling-published", []string{".", "tools"}, []string{".", "tools"}, []string{".", "tools"}},
		{"duplicate", []string{".", "tools"}, []string{".", "tools", "tools"}, []string{"."}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange / Act
			err := validateInventory(test.actual, test.expected, test.published)
			// Assert
			if err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}
