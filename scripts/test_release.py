from pathlib import Path
import os
import subprocess
import sys
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("release.py")
COMMIT = "1" * 40


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.source.mkdir()
        (self.source / "LICENSE").write_text("license\n")
        (self.source / "README.md").write_text("readme\n")
        self.git("init", "-q")
        self.git("config", "user.email", "test@example.com")
        self.git("config", "user.name", "Test")
        self.git("config", "commit.gpgsign", "false")
        self.git("add", ".")
        self.git("commit", "-qm", "canonical cli tree")
        tree = self.git("rev-parse", "HEAD^{tree}")
        (self.source / "canonical-source.json").write_text(
            '{"canonical_commit":"' + COMMIT + '","cli_tree":"' + tree
            + '","release_version":"v1.2.3-rc.1"}\n'
        )
        self.git("add", "canonical-source.json")
        self.git("commit", "-qm", "public snapshot")
        self.git("tag", "v1.2.3-rc.1")
        tools = self.root / "bin"
        tools.mkdir()
        goroot = self.root / "goroot"
        goroot.mkdir()
        (goroot / "LICENSE").write_text("Go license\n")
        module = self.root / "module"
        module.mkdir()
        self.module = module
        (module / "LICENSE.txt").write_text("Dependency license\n")
        fake_go = tools / "go"
        fake_go.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os, pathlib, sys\n"
            f"goroot = {str(goroot)!r}\n"
            f"module = {str(module)!r}\n"
            "if sys.argv[1:3] == ['env', 'GOROOT']:\n"
            "    print(goroot)\n"
            "elif sys.argv[1] == 'list':\n"
            "    print(json.dumps({'ImportPath': 'example.test/dependency/pkg', "
            "'Module': {'Path': 'example.test/dependency', 'Version': 'v1.0.0', 'Dir': module}}))\n"
            "else:\n"
            "    out = pathlib.Path(sys.argv[sys.argv.index('-o') + 1])\n"
            "    out.write_bytes((os.environ['GOOS'] + '/' + os.environ['GOARCH']).encode())\n"
        )
        fake_go.chmod(0o755)
        self.env["PATH"] = str(tools) + os.pathsep + self.env["PATH"]

    def tearDown(self):
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.run(
            ["git", "-C", self.source, *args], check=True, capture_output=True, text=True, env=self.env
        ).stdout.strip()

    def run_release(self, dist, tag="v1.2.3-rc.1"):
        return subprocess.run(
            [sys.executable, SCRIPT, "--source", self.source, "--dist", dist, "--tag", tag],
            env=self.env,
            capture_output=True,
            text=True,
        )

    def test_build_is_reproducible_for_all_targets(self):
        first, second = self.root / "first", self.root / "second"
        results = [self.run_release(first), self.run_release(second)]
        self.assertTrue(all(result.returncode == 0 for result in results), results)
        self.assertEqual(
            {path.name: path.read_bytes() for path in first.iterdir()},
            {path.name: path.read_bytes() for path in second.iterdir()},
        )
        self.assertEqual(len(list(first.glob("*.tar.gz"))), 4)
        self.assertEqual(len(list(first.glob("*.zip"))), 2)
        self.assertEqual(len((first / "checksums.txt").read_text().splitlines()), 6)
        archive = next(first.glob("*.tar.gz"))
        with tarfile.open(archive) as contents:
            self.assertIn("THIRD_PARTY_NOTICES.txt", contents.getnames())
            notices = contents.extractfile("THIRD_PARTY_NOTICES.txt").read().decode()
        self.assertIn("example.test/dependency v1.0.0", notices)
        self.assertNotIn(str(self.root), notices)

    def test_requires_matching_release_provenance(self):
        mismatch = self.run_release(self.root / "mismatch", "v1.2.4")
        self.assertNotEqual(mismatch.returncode, 0)
        self.assertIn("release_version does not match", mismatch.stderr)
        (self.source / "canonical-source.json").unlink()
        missing = self.run_release(self.root / "missing")
        self.assertNotEqual(missing.returncode, 0)
        self.assertIn("missing or invalid", missing.stderr)

    def test_rejects_non_object_provenance(self):
        for value in ("null", "0", "[]", '[{}]'):
            with self.subTest(value=value):
                (self.source / "canonical-source.json").write_text(value)
                result = self.run_release(self.root / "invalid")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("must contain release provenance", result.stderr)
                self.assertNotIn("Traceback", result.stderr)

    def test_rejects_local_dependency_replacement(self):
        fake_go = self.root / "bin" / "go"
        script = fake_go.read_text()
        script = script.replace("'Version': 'v1.0.0',", "'Version': 'v1.0.0', 'Replace': {'Path': '../local', 'Dir': module},")
        fake_go.write_text(script)
        result = self.run_release(self.root / "local-replacement")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dependency module identity is incomplete", result.stderr)

    def test_refuses_existing_distribution_directory(self):
        dist = self.root / "existing"
        dist.mkdir()
        result = self.run_release(dist)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already exists", result.stderr)

    def test_refuses_dependency_without_notice(self):
        (self.module / "LICENSE.txt").unlink()
        result = self.run_release(self.root / "missing-license")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dependency has no LICENSE", result.stderr)

    def test_rejects_dirty_or_tree_mismatched_source(self):
        (self.source / "untracked.txt").write_text("changed\n")
        dirty = self.run_release(self.root / "dirty")
        self.assertNotEqual(dirty.returncode, 0)
        self.assertIn("tracked or untracked changes", dirty.stderr)
        (self.source / "untracked.txt").unlink()

        (self.source / "README.md").write_text("different\n")
        self.git("add", "README.md")
        self.git("commit", "-qm", "different public source")
        self.git("tag", "-f", "v1.2.3-rc.1")
        mismatch = self.run_release(self.root / "tree-mismatch")
        self.assertNotEqual(mismatch.returncode, 0)
        self.assertIn("does not match canonical cli_tree", mismatch.stderr)


if __name__ == "__main__":
    unittest.main()
