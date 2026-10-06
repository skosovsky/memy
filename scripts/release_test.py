#!/usr/bin/env python3
"""Local-only AAA release regression fixtures."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("release.sh").resolve()
GIT = shutil.which("git")


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        self.env.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull)
        self.temp = tempfile.TemporaryDirectory(prefix="memy-release-test-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.repo = self.base / "repo"
        self.remote = self.base / "remote.git"
        self.repo.mkdir()
        self.git("init", "-q", "--bare", str(self.remote))
        self.git("init", "-q", str(self.repo))
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        (self.repo / "go.mod").write_text("module example.invalid/memy\n\ngo 1.27\n")
        self.git("add", "go.mod")
        self.git("commit", "-qm", "fixture")
        self.git("remote", "add", "origin", str(self.remote))
        self.env["TMPDIR"] = str(self.base)
        self.before = self.snapshot()

    def git(self, *args):
        return subprocess.check_output([GIT, *args], cwd=self.repo, env=self.env, text=True).strip()

    def snapshot(self):
        return (
            self.git("rev-parse", "HEAD"),
            self.git("symbolic-ref", "-q", "HEAD") if not (self.repo / ".git/HEAD").read_text().startswith(tuple("0123456789abcdef")) else "detached",
            self.git("status", "--porcelain", "--untracked-files=all"),
            self.git("show-ref"),
            (self.repo / ".git/index").read_bytes(),
            (self.repo / "go.mod").read_bytes(),
        )

    def release(self, kind="patch", modules="."):
        return subprocess.run(["bash", str(SCRIPT), kind, modules], cwd=self.repo,
                              env=self.env, input="y\n", text=True, capture_output=True)

    def tag(self):
        return self.git("ls-remote", "--tags", "--refs", str(self.remote))

    def wrapper(self, mode):
        folder = self.base / "bin"
        folder.mkdir()
        wrapper = folder / "git"
        wrapper.write_text('''#!/bin/bash
for arg in "$@"; do
    if [[ "$arg" == tag && "$FIXTURE_MODE" == before-tag ]]; then exit 1; fi
    if [[ "$arg" == push ]]; then
        touch "$FIXTURE_MARKER"
        if [[ "$FIXTURE_MODE" == unknown ]]; then exit 1; fi
        "$REAL_GIT" "$@"
        exit 1
    fi
done
if [[ "$1" == ls-remote && "$FIXTURE_MODE" == unknown && -f "$FIXTURE_MARKER" ]]; then exit 1; fi
exec "$REAL_GIT" "$@"
''')
        wrapper.chmod(0o755)
        self.env.update(PATH=str(folder) + os.pathsep + self.env["PATH"],
                        REAL_GIT=GIT, FIXTURE_MODE=mode,
                        FIXTURE_MARKER=str(self.base / "pushed"))

    def test_happy_excludes_untracked_and_unrelated_tags(self):
        # Arrange
        (self.repo / "private-untracked.txt").write_text("synthetic marker")
        self.git("tag", "scratch-local")
        self.git("tag", "v0.9.9")  # Unpublished versions cannot drive the bump.
        before = self.snapshot()
        # Act
        result = self.release()
        # Assert
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("State: published", result.stdout)
        self.assertEqual(self.tag(), before[0] + "\trefs/tags/v0.0.1")
        self.assertEqual(self.snapshot(), before)
        tree = self.git("--git-dir=" + str(self.remote), "ls-tree", "--name-only", "v0.0.1")
        self.assertEqual(tree, "go.mod")

    def test_clean_break_and_published_version(self):
        # Arrange
        self.git("tag", "v0.2.8")
        self.git("push", "-q", "origin", "refs/tags/v0.2.8:refs/tags/v0.2.8")
        before = self.snapshot()
        # Act
        result = self.release("break")
        # Assert
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("refs/tags/v0.3.0", self.tag())
        self.assertEqual(self.snapshot(), before)

    def test_rejected_push_retry_keeps_same_version(self):
        # Arrange
        hook = self.remote / "hooks/pre-receive"
        hook.write_text("#!/bin/sh\nexit 1\n")
        hook.chmod(0o755)
        # Act
        first = self.release()
        hook.unlink()
        second = self.release()
        # Assert
        self.assertNotEqual(first.returncode, 0)
        self.assertIn("State: rejected", first.stderr)
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(self.tag(), self.before[0] + "\trefs/tags/v0.0.1")
        self.assertEqual(self.snapshot(), self.before)
        self.assertFalse(list(self.base.glob("memy-release.*")))

    def test_detached_start(self):
        # Arrange
        self.git("checkout", "-q", "--detach", "HEAD")
        before = self.snapshot()
        # Act
        result = self.release()
        # Assert
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.snapshot(), before)

    def test_failure_before_tag(self):
        # Arrange
        self.wrapper("before-tag")
        # Act
        result = self.release()
        # Assert
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.tag(), "")
        self.assertEqual(self.snapshot(), self.before)
        self.assertFalse(list(self.base.glob("memy-release.*")))

    def test_lost_push_response_is_reconciled(self):
        # Arrange
        self.wrapper("lost-response")
        # Act
        result = self.release()
        # Assert
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("confirmed after push failure", result.stdout)
        self.assertEqual(self.snapshot(), self.before)

    def test_unknown_preserves_recovery_refs(self):
        # Arrange
        self.wrapper("unknown")
        # Act
        result = self.release()
        # Assert
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("State: unknown", result.stderr)
        prepared = list(self.base.glob("memy-release.*/repo"))
        self.assertEqual(len(prepared), 1)
        oid = subprocess.check_output([GIT, "-C", str(prepared[0]), "rev-parse", "v0.0.1"], env=self.env, text=True).strip()
        self.assertEqual(oid, self.before[0])
        self.assertIn(str(prepared[0]), result.stderr)
        self.assertEqual(self.snapshot(), self.before)

    def test_staged_and_unstaged_rejected(self):
        # Arrange
        (self.repo / "go.mod").write_text("module changed.invalid/memy\n")
        before = self.snapshot()
        # Act / Assert
        self.assertNotEqual(self.release().returncode, 0)
        self.assertEqual(self.snapshot(), before)
        self.git("add", "go.mod")
        before = self.snapshot()
        self.assertNotEqual(self.release().returncode, 0)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(self.tag(), "")

    def test_major_import_path_mismatch(self):
        # Arrange
        self.git("tag", "v2.0.0")
        self.git("push", "-q", "origin", "refs/tags/v2.0.0:refs/tags/v2.0.0")
        before = self.snapshot()
        # Act
        result = self.release()
        # Assert
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("semantic import path", result.stderr)
        self.assertEqual(self.snapshot(), before)

    def test_unsupported_modules_and_major_break(self):
        # Arrange / Act / Assert
        self.assertNotEqual(self.release(modules=". ./other").returncode, 0)
        self.git("tag", "v1.0.0")
        self.git("push", "-q", "origin", "refs/tags/v1.0.0:refs/tags/v1.0.0")
        before = self.snapshot()
        result = self.release("break")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("semantic import-version", result.stderr)
        self.assertEqual(self.snapshot(), before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
