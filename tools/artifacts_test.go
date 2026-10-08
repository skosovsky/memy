//go:build integration || e2e

package tools_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

const modulePath = "github.com/skosovsky/memy"

func artifactProxy(t *testing.T, source, version, proxy string) []string {
	t.Helper()
	dirs := strings.Fields(mustRun(t, source, nil, "make", "--no-print-directory", "-s", "modules"))
	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		base := filepath.Join(source, dir)
		data := read(t, filepath.Join(base, "go.mod"))
		manifest, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			t.Fatal(err)
		}
		path := manifest.Module.Mod.Path
		paths = append(paths, path)
		for _, r := range manifest.Replace {
			if strings.HasPrefix(r.Old.Path, "github.com/skosovsky/memy") {
				t.Fatalf("development replacement in %s", path)
			}
		}
		for _, r := range manifest.Require {
			if strings.HasPrefix(r.Mod.Path, "github.com/skosovsky/memy") && r.Mod.Version != version {
				t.Fatalf("wrong candidate dependency %s", r.Mod)
			}
		}
		escaped, err := module.EscapePath(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(proxy, escaped, "@v")
		var archive bytes.Buffer
		if err := modzip.CreateFromDir(&archive, module.Version{Path: path, Version: version}, base); err != nil {
			t.Fatal(err)
		}
		checkModuleArchive(t, archive.Bytes(), path, version, dir, dirs)
		write(t, filepath.Join(target, version+".zip"), archive.Bytes())
		write(t, filepath.Join(target, version+".mod"), data)
		write(
			t,
			filepath.Join(target, version+".info"),
			fmt.Appendf(nil, `{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version),
		)
		write(t, filepath.Join(target, "list"), []byte(version+"\n"))
	}
	return paths
}

func consume(t *testing.T, source, version string, env []string, paths []string) {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, env, "go", "mod", "init", "example.invalid/releaseconsumer")
	for _, path := range paths {
		mustRun(t, dir, env, "go", "mod", "edit", "-require="+path+"@"+version)
	}
	imports := map[string]bool{}
	for _, path := range paths {
		// Compile library, executable and test-only modules without running repository tests.
		mustRun(t, dir, env, "go", "test", "-mod=mod", "-run=^$", path+"/...")
		packages := strings.FieldsSeq(
			mustRun(
				t,
				dir,
				env,
				"go",
				"list",
				"-mod=mod",
				"-f",
				`{{if and (ne .Name "main") (or .GoFiles .CgoFiles)}}{{.ImportPath}}{{end}}`,
				path+"/...",
			),
		)
		for p := range packages {
			if !strings.Contains(p, "/internal/") {
				imports[p] = true
			}
		}
	}
	var code strings.Builder
	code.WriteString("package consumer\nimport (\n")
	for path := range imports {
		fmt.Fprintf(&code, "_ %q\n", path)
	}
	code.WriteString(")\n")
	write(t, filepath.Join(dir, "consumer.go"), []byte(code.String()))
	mustRun(t, dir, env, "go", "mod", "tidy")
	// Tidy removes modules containing only commands/tests; still verify their exact versions.
	for _, path := range paths {
		mustRun(t, dir, env, "go", "mod", "edit", "-require="+path+"@"+version)
	}
	mustRun(t, dir, env, "go", "mod", "download")
	checkResolvedModules(t, dir, env, paths, version)
	mustRun(t, dir, env, "go", "test", "-race", "-count=1", "./...")
	mustRun(t, dir, env, "go", "build", "./...")
	// Execute the repository onboarding against artifacts, not local source replacements.
	example := read(t, filepath.Join(source, "examples/lifecycle/main.go"))
	write(t, filepath.Join(dir, "cmd/demo/main.go"), example)
	mustRun(t, dir, env, "go", "run", "./cmd/demo")
}

func checkResolvedModules(t *testing.T, dir string, env []string, paths []string, version string) {
	t.Helper()
	listed := mustRun(t, dir, env, "go", "list", "-m", "-json", "all")
	decoder := json.NewDecoder(strings.NewReader(listed))
	seen := map[string]bool{}
	for decoder.More() {
		var m struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Replace any    `json:"Replace"`
		}
		if err := decoder.Decode(&m); err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if m.Path == path {
				if m.Version != version || m.Replace != nil {
					t.Fatalf("wrong resolved dependency %+v", m)
				}
				seen[path] = true
			}
		}
	}
	if len(seen) != len(paths) {
		t.Fatal("missing published modules")
	}
}

func TestIntegrationReleaseArtifacts(t *testing.T) {
	// By default, validate a disposable candidate; environment inputs allow manual candidate inspection.
	source := os.Getenv("MEMY_CANDIDATE")
	version := os.Getenv("MEMY_RELEASE_VERSION")
	proxy := os.Getenv("MEMY_ARTIFACT_PROXY")
	if source == "" {
		source = sourceCandidate(t, rootDir(t))
		version = "v0.0.999"
	}
	if version == "" {
		t.Fatal("candidate version missing")
	}
	if proxy == "" {
		proxy = filepath.Join(t.TempDir(), "proxy")
	}
	// Arrange: standard x/mod artifact construction; no homemade ZIP format.
	paths := artifactProxy(t, source, version, proxy)
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String()
	env := []string{
		"GOENV=off",
		"GOFLAGS=-modcacherw",
		"GOPROXY=" + uri + ",https://proxy.golang.org",
		"GONOPROXY=none",
		"GONOSUMDB=github.com/skosovsky/memy,github.com/skosovsky/memy/*",
		"GOSUMDB=sum.golang.org",
		"GOMODCACHE=" + filepath.Join(t.TempDir(), "modcache"),
	}
	// Act / Assert: actual external Go resolution, compilation and executable example.
	consume(t, source, version, env, paths)
	verifyConsumer(t.Context(), t, source, version, uri+",https://proxy.golang.org", true)
}

func sourceCandidate(t *testing.T, root string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "source")
	// Export tracked current files, preserving edits under test but never ignored/untracked payload.
	for name := range strings.SplitSeq(mustRun(t, root, nil, "git", "ls-files", "-z"), "\x00") {
		if name == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(target, name), data)
	}
	for dir := range strings.FieldsSeq(mustRun(t, target, nil, "make", "--no-print-directory", "-s", "modules")) {
		rewriteFixtureManifest(t, filepath.Join(target, dir))
	}

	return target
}

func TestE2EPublishedRelease(t *testing.T) {
	version := os.Getenv("MEMY_REF")
	baseline := version == "" || version == "v0.3.1"
	if version == "" {
		version = "v0.3.1"
	} // Explicit published baseline; an environment override selects another version.
	root := rootDir(t)
	dirs := strings.Fields(mustRun(t, root, nil, "make", "--no-print-directory", "-s", "modules"))
	if baseline {
		// v0.3.1 predates publication of tooling and consumer modules.
		dirs = dirs[:1]
	}

	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		m, err := modfile.Parse("go.mod", read(t, filepath.Join(root, dir, "go.mod")), nil)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m.Module.Mod.Path)
	}
	env := []string{
		"GOENV=off",
		"GOFLAGS=-modcacherw",
		"GOPROXY=https://proxy.golang.org",
		"GONOPROXY=none",
		"GONOSUMDB=",
		"GOSUMDB=sum.golang.org",
		"GOMODCACHE=" + filepath.Join(t.TempDir(), "modcache"),
	}
	consume(t, root, version, env, paths)
	verifyConsumer(t.Context(), t, root, version, "https://proxy.golang.org", false)
}

func rewriteFixtureManifest(t *testing.T, base string) {
	t.Helper()
	path := filepath.Join(base, "go.mod")
	m, err := modfile.Parse(path, read(t, path), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range m.Require {
		if strings.HasPrefix(r.Mod.Path, "github.com/skosovsky/memy") {
			if err = m.AddRequire(r.Mod.Path, "v0.0.999"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, r := range append([]*modfile.Replace(nil), m.Replace...) {
		if strings.HasPrefix(r.Old.Path, "github.com/skosovsky/memy") {
			if err = m.DropReplace(r.Old.Path, r.Old.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := m.Format()
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, data)
}

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
		env = append(env, "GONOSUMDB="+modulePath+","+modulePath+"/*")
	}
	mustGoContext(ctx, t, dir, env, "mod", "tidy")
	checkResolvedGraph(ctx, t, dir, env, version)
	out := mustGoContext(
		ctx,
		t,
		dir,
		env,
		"test",
		"-json",
		"-tags=integration,e2e",
		"-run=^(TestIntegration|TestE2E)",
		"-mod=readonly",
		"-race",
		"-count=1",
		"-timeout=2m",
		"./...",
	)
	assertConsumerTests(t, out)
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

func mustGoContext(ctx context.Context, t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := commandContext(ctx, t, dir, env, "go", args...)
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

func checkModuleArchive(t *testing.T, data []byte, path, version, dir string, dirs []string) {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	prefix := path + "@" + version + "/"
	for _, entry := range archive.File {
		if !strings.HasPrefix(entry.Name, prefix) {
			t.Fatalf("unexpected artifact path: %s", entry.Name)
		}
		name := strings.TrimPrefix(entry.Name, prefix)
		for _, child := range dirs {
			relative, err := filepath.Rel(dir, child)
			if err != nil {
				t.Fatal(err)
			}
			if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
				continue
			}
			if strings.HasPrefix(name, filepath.ToSlash(relative)+"/") {
				t.Fatalf("nested module included in %s: %s", path, name)
			}
		}
	}
}

func assertConsumerTests(t *testing.T, output string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	passed := 0
	for {
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Action == "pass" &&
			(strings.HasPrefix(event.Test, "TestIntegration") || strings.HasPrefix(event.Test, "TestE2E")) &&
			!strings.Contains(event.Test, "/") {
			passed++
		}
	}
	if passed != 8 {
		t.Fatalf("consumer scenarios passed: %d, want 8", passed)
	}
	t.Logf("all %d consumer scenarios passed", passed)
}
