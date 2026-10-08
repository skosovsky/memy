//go:build !integration && !e2e

package tools_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestPublishableModuleInventory(t *testing.T) {
	// Arrange: tracked source is the inventory authority, not ignored build artifacts.
	root := rootDir(t)
	publish := strings.Fields(mustRun(t, root, nil, "make", "--no-print-directory", "-s", "modules"))
	paths := strings.Split(
		mustRun(
			t,
			root,
			nil,
			"git",
			"ls-files",
			"--cached",
			"--",
			"go.mod",
			"**/go.mod",
		),
		"\n",
	)
	// Act.
	found := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			found = append(found, filepath.ToSlash(filepath.Dir(path)))
		}
	}
	wantPublish := found
	// Assert.
	slices.Sort(wantPublish)
	slices.Sort(publish)
	if !slices.Equal(publish, wantPublish) {
		t.Fatalf("publishable inventory: listed %v; expected %v", publish, wantPublish)
	}
}

func TestGoWorkspaceIsolated(t *testing.T) {
	// Arrange: an ambient workspace must not decide module resolution for tooling.
	t.Setenv("GOWORK", "/nonexistent/ambient.go.work")
	// Act.
	actual := mustRun(t, t.TempDir(), []string{"GOENV=off"}, "go", "env", "GOWORK")
	// Assert.
	if actual != "off" {
		t.Fatalf("ambient workspace leaked: %s", actual)
	}
}

func TestModulePathsMatchDirectories(t *testing.T) {
	// Arrange.
	root := rootDir(t)
	dirs := strings.Fields(mustRun(t, root, nil, "make", "-s", "modules"))
	rootManifest, err := modfile.Parse("go.mod", read(t, filepath.Join(root, "go.mod")), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Act / Assert.
	for _, dir := range dirs {
		manifest, err := modfile.Parse("go.mod", read(t, filepath.Join(root, dir, "go.mod")), nil)
		if err != nil {
			t.Fatal(err)
		}
		want := rootManifest.Module.Mod.Path
		if dir != "." {
			want += "/" + dir
		}
		if manifest.Module.Mod.Path != want {
			t.Fatalf("module %s: got %s, want %s", dir, manifest.Module.Mod.Path, want)
		}
	}
}
