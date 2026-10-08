//go:build integration

package tools_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

const modulePath = "github.com/skosovsky/memy"

func consumerCopy(t *testing.T, root, version string) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(root, "integration", "consumer"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name != "go.mod" && name != "go.sum" && !strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "integration", "consumer", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "go.mod" {
			data = consumerManifest(t, data, version)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func verifyConsumer(ctx context.Context, t *testing.T, root, version, proxy string, local bool) {
	t.Helper()
	dir := consumerCopy(t, root, version)
	env := []string{
		"GOWORK=off", "GOENV=off", "GOFLAGS=-modcacherw",
		"GOPROXY=" + proxy,
		"GOMODCACHE=" + t.TempDir(),
		"GOSUMDB=sum.golang.org",
		"GOPRIVATE=none",
		"GONOPROXY=none",
		"GONOSUMDB=none",
	}
	if local {
		env = append(env, "GONOSUMDB="+modulePath)
	}
	mustGoContext(ctx, t, dir, env, "mod", "tidy")
	checkResolvedGraph(ctx, t, dir, env, version)
	out := mustGoContext(ctx, t, dir, env, "test", "-mod=readonly", "-race", "-count=1", "-timeout=2m", "./...")
	t.Log(out)
}

func TestPublishedConsumer(t *testing.T) {
	// Arrange
	version := os.Getenv("MEMY_REF")
	if version == "" {
		version = "v0.3.1"
	}
	if err := module.Check(modulePath, version); err != nil {
		t.Fatal(err)
	}
	// Act / Assert: public proxy only, exact identity, checksums and actual semantics.
	deadline := 2 * time.Minute
	if value := os.Getenv("MEMY_PUBLIC_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			t.Fatal("invalid MEMY_PUBLIC_TIMEOUT")
		}
		deadline = parsed
	}
	ctx, cancel := context.WithTimeout(t.Context(), deadline)
	defer cancel()
	verifyConsumer(ctx, t, rootDir(t), version, "https://proxy.golang.org", false)
}

func TestCandidateArtifacts(t *testing.T) {
	// Arrange
	root := rootDir(t)
	version := os.Getenv("MEMY_CANDIDATE_VERSION")
	if version == "" {
		version = "v0.3.2"
	}
	if err := module.Check(modulePath, version); err != nil {
		t.Fatal(err)
	}
	source := candidateSource(t, root)
	data, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Module.Mod.Path != modulePath || len(manifest.Replace) != 0 {
		t.Fatal("invalid publishable manifest")
	}
	proxy := t.TempDir()
	artifacts := filepath.Join(proxy, "github.com", "skosovsky", "memy", "@v")
	if mkdirErr := os.MkdirAll(artifacts, 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// Act
	zipPath := filepath.Join(artifacts, version+".zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zipErr := modzip.CreateFromDir(file, module.Version{Path: modulePath, Version: version}, source)
	closeErr := file.Close()
	if zipErr != nil {
		t.Fatal(zipErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(artifacts, version+".mod"), data, 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	info := fmt.Sprintf("{\"Version\":%q,\"Time\":%q}\n", version, time.Unix(0, 0).UTC().Format(time.RFC3339))
	writeFile(t, filepath.Join(artifacts, version+".info"), info, 0o644)
	writeFile(t, filepath.Join(artifacts, "list"), version+"\n", 0o644)
	// Assert: nested development modules must be omitted by actual Go ZIP rules.
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	prefix := modulePath + "@" + version + "/"
	for _, entry := range reader.File {
		name := strings.TrimPrefix(entry.Name, prefix)
		if strings.HasPrefix(name, "tools/") || strings.HasPrefix(name, "integration/consumer/") {
			t.Fatalf("development module published: %s", name)
		}
	}
	verifyConsumer(t.Context(), t, root, version, "file://"+filepath.ToSlash(proxy)+",https://proxy.golang.org", true)
}

func consumerManifest(t *testing.T, data []byte, version string) []byte {
	t.Helper()
	parsed, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range parsed.Replace {
		if dropErr := parsed.DropReplace(replacement.Old.Path, replacement.Old.Version); dropErr != nil {
			t.Fatal(dropErr)
		}
	}
	if addErr := parsed.AddRequire(modulePath, version); addErr != nil {
		t.Fatal(addErr)
	}
	data, err = parsed.Format()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func candidateSource(t *testing.T, root string) string {
	t.Helper()
	source := t.TempDir()
	if commit := os.Getenv("MEMY_CANDIDATE_SOURCE"); commit != "" {
		archive := filepath.Join(t.TempDir(), "source.tar")
		mustRun(t, root, nil, "git", "archive", "--format=tar", "--output="+archive, commit)
		mustRun(t, source, nil, "tar", "-xf", archive)
		return source
	}
	// Current tracked bytes only: build outputs and private untracked files are excluded.
	paths := mustRun(t, root, nil, "git", "ls-files", "-z")
	for name := range strings.SplitSeq(paths, "\x00") {
		if name == "" {
			continue
		}
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("unsupported tracked file: %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(source, name)
		if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o755); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		writeFile(t, target, string(data), info.Mode().Perm())
	}
	return source
}

func checkResolvedGraph(ctx context.Context, t *testing.T, dir string, env []string, version string) {
	t.Helper()
	pinned := pinnedVersions(t, version)
	graph := mustGoContext(ctx, t, dir, env, "list", "-m", "-json", "all")
	decoder := json.NewDecoder(strings.NewReader(graph))
	found := false
	for {
		var item struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Dir     string `json:"Dir"`
			Replace any    `json:"Replace"`
		}
		err := decoder.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if expected, ok := pinned[item.Path]; ok {
			if item.Version != expected {
				t.Fatalf("%s resolved %s instead of pinned %s", item.Path, item.Version, expected)
			}
			delete(pinned, item.Path)
		}
		if item.Replace != nil {
			t.Fatalf("published graph includes replacement: %s", item.Path)
		}
		if item.Path == modulePath {
			found = true
			mustGoContext(ctx, t, item.Dir, env, "build", "-mod=readonly", "./...")
			if item.Version != version {
				t.Fatalf("resolved %s instead of %s", item.Version, version)
			}
		}
	}
	if len(pinned) != 0 {
		t.Fatalf("pinned modules missing from resolved graph: %v", pinned)
	}
	if !found {
		t.Fatal("memy missing from consumer graph")
	}
}

func pinnedVersions(t *testing.T, version string) map[string]string {
	t.Helper()
	data, readErr := os.ReadFile(filepath.Join(rootDir(t), "integration", "consumer", "go.mod"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	manifest, parseErr := modfile.Parse("go.mod", data, nil)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	pinned := make(map[string]string)
	for _, requirement := range manifest.Require {
		pinned[requirement.Mod.Path] = requirement.Mod.Version
	}
	pinned[modulePath] = version
	return pinned
}
