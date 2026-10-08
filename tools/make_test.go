//go:build !integration && !e2e

package tools_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func makeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{"Makefile"} {
		write(t, filepath.Join(root, path), read(t, filepath.Join(rootDir(t), path)))
	}
	write(t, filepath.Join(root, "go.mod"), []byte("module example.invalid/root\n"))
	return root
}

func TestMakeDiscoversModules(t *testing.T) {
	// Arrange: new modules need no registry or Git staging; hidden/vendor trees are excluded.
	root := makeFixture(t)
	for _, dir := range []string{"adapters/new", "examples/new", ".cache/ignored", "vendor/ignored", "adapters/new/vendor/ignored"} {
		write(t, filepath.Join(root, dir, "go.mod"), []byte("module example.invalid/fixture\n"))
	}
	// Act.
	actual := mustRun(t, root, nil, "make", "--no-print-directory", "-s", "modules")
	// Assert.
	if actual != ".\nadapters/new\nexamples/new" {
		t.Fatalf("discovered modules: %q", actual)
	}
}

func TestMakePropagatesToolFailures(t *testing.T) {
	for _, target := range []string{"test", "lint"} {
		t.Run(target, func(t *testing.T) {
			// Arrange: use the actual Make recipes with a failing tool.
			root := makeFixture(t)
			cmd := exec.CommandContext(t.Context(), "make", target)
			cmd.Dir = root
			cmd.Env = makeToolPath(t, root, "#!/bin/sh\nexit 1\n")
			// Act.
			out, err := cmd.CombinedOutput()
			// Assert.
			if err == nil {
				t.Fatalf("%s swallowed failure: %s", target, out)
			}
		})
	}
}

func TestMakeLintRejectsFormattingDiff(t *testing.T) {
	// Arrange: the pinned formatter returns a nonzero status when formatting differs.
	root := makeFixture(t)
	tool := filepath.Join(root, "golangci-lint")
	write(t, tool, []byte("#!/bin/sh\nif [ \"$1\" = fmt ]; then echo 'formatting differs'; exit 1; fi\n"))
	if err := os.Chmod(tool, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "make", "lint")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Act.
	out, err := cmd.CombinedOutput()
	// Assert.
	if err == nil || !strings.Contains(string(out), "formatting differs") {
		t.Fatalf("format gate: %v %s", err, out)
	}
}

func TestFuzzSelectsIndividualGoTargets(t *testing.T) {
	// Arrange: Go list output includes two fuzz targets, Unicode and tool diagnostics.
	root := makeFixture(t)
	tool := filepath.Join(root, "go")
	log := filepath.Join(root, "calls")
	write(t, tool, []byte(`#!/bin/sh
case "$1" in
list) echo example.invalid/fuzz ;;
test) case " $* " in
*' -list='*) printf 'FuzzOne\nFuzzТекст\nok example.invalid/fuzz\n' ;;
*) printf '%s\n' "$*" >> "$CALLS" ;;
esac ;;
esac
`))
	if err := os.Chmod(tool, 0700); err != nil {
		t.Fatal(err)
	}
	// Act: discover targets using the normal Go executable lookup.
	mustRun(
		t,
		root,
		[]string{"PATH=" + root + string(os.PathListSeparator) + os.Getenv("PATH"), "CALLS=" + log},
		"make",
		"fuzz",
	)
	// Assert: one anchored target per invocation, no broad pattern or diagnostic line.
	calls := string(read(t, log))
	if strings.Count(calls, "-fuzz=") != 2 || !strings.Contains(calls, "-fuzz=^FuzzOne$") ||
		!strings.Contains(calls, "-fuzz=^FuzzТекст$") || strings.Count(calls, "-fuzztime=30s") != 2 {
		t.Fatalf("incorrect fuzz selection: %s", calls)
	}
}

func TestFuzzStopsAfterCampaignFailure(t *testing.T) {
	// Arrange: listing succeeds; the first campaign fails before a second target.
	root := makeFixture(t)
	log := filepath.Join(root, "calls")
	cmd := exec.CommandContext(t.Context(), "make", "fuzz")
	cmd.Dir = root
	cmd.Env = append(makeToolPath(t, root, `#!/bin/sh
case "$1" in
list) echo example.invalid/fuzz ;;
test) case " $* " in
*' -list='*) printf 'FuzzOne\nFuzzTwo\n' ;;
*) printf '%s\n' "$*" >> "$CALLS"; exit 7 ;;
esac ;;
esac
`), "CALLS="+log)
	// Act.
	out, err := cmd.CombinedOutput()
	// Assert: the pipeline propagates failure and does not start another campaign.
	if err == nil {
		t.Fatalf("campaign failure swallowed: %s", out)
	}
	calls := string(read(t, log))
	if strings.Count(calls, "-fuzz=") != 1 || !strings.Contains(calls, "-fuzz=^FuzzOne$") {
		t.Fatalf("unexpected campaigns: %s", calls)
	}
}

func TestMakeTaggedProfilesExcludeOrdinaryTests(t *testing.T) {
	// Arrange: an ordinary test fails if either tagged profile executes it.
	root := makeFixture(t)
	write(
		t,
		filepath.Join(root, "ordinary_test.go"),
		[]byte(
			"package fixture\nimport \"testing\"\nfunc TestOrdinary(t *testing.T) { t.Fatal(\"ordinary test executed\") }\n",
		),
	)
	for _, profile := range []struct{ tag, prefix string }{{"integration", "TestIntegration"}, {"e2e", "TestE2E"}} {
		code := "//go:build " + profile.tag + "\n\npackage fixture\nimport (\"os\"; \"testing\")\nfunc " + profile.prefix + "Marker(t *testing.T) { if err := os.WriteFile(os.Getenv(\"MARKER\"), []byte(\"executed\"),0600); err != nil { t.Fatal(err) } }\n"
		write(t, filepath.Join(root, profile.tag+"_test.go"), []byte(code))
		marker := filepath.Join(root, profile.tag+".marker")
		// Act.
		mustRun(t, root, []string{"MARKER=" + marker}, "make", "test-"+profile.tag)
		// Assert: tagged tests actually executed, without the ordinary failing test.
		if string(read(t, marker)) != "executed" {
			t.Fatal("tagged test did not execute")
		}
	}
}

func TestMakePublicationUsesAllModules(t *testing.T) {
	// Arrange: every discovered module belongs to the same inventory.
	root := makeFixture(t)
	for _, dir := range []string{"packages/new", "examples/demo", "tooling", "vendor/ignored", ".cache/ignored"} {
		write(t, filepath.Join(root, dir, "go.mod"), []byte("module example.invalid/fixture\n"))
	}
	// Act.
	actual := mustRun(t, root, nil, "make", "--no-print-directory", "-s", "modules")
	// Assert: a new library module requires no manually maintained list.
	if actual != ".\nexamples/demo\npackages/new\ntooling" {
		t.Fatalf("publication modules: %q", actual)
	}
}

func TestMakeStopsOnAnEarlierModuleFailure(t *testing.T) {
	for _, target := range []string{"test", "test-integration", "test-e2e", "lint", "bench", "cover", "fix", "test-live", "fuzz"} {
		t.Run(target, func(t *testing.T) {
			// Arrange: the first module fails, but a later module would succeed.
			root := makeFixture(t)
			write(t, filepath.Join(root, "packages/last/go.mod"), []byte("module example.invalid/last\n"))
			cmd := exec.CommandContext(t.Context(), "make", target)
			cmd.Dir = root
			cmd.Env = makeToolPath(
				t,
				root,
				"#!/bin/sh\nif [ \"$1\" = config ]; then exit 0; fi\ncase \"$PWD\" in */last) exit 0;; *) exit 1;; esac\n",
			)
			// Act.
			out, err := cmd.CombinedOutput()
			// Assert: successful later modules cannot hide an earlier failure.
			if err == nil {
				t.Fatalf("earlier module failure swallowed: %s", out)
			}
		})
	}
}

func makeToolPath(t *testing.T, root, script string) []string {
	t.Helper()
	bin := filepath.Join(root, "bin")
	for _, name := range []string{"go", "golangci-lint"} {
		path := filepath.Join(bin, name)
		write(t, path, []byte(script))
		if err := os.Chmod(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
