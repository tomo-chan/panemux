"""Offline tests: real throwaway git repos, fake upstream/install/gh boundaries."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("automation", Path(__file__).with_name("agmsg_contract.py"))
automation = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(automation)
ROOT = Path(__file__).resolve().parent.parent
SHA = "a" * 40


class AutomationTests(unittest.TestCase):
    def test_latest_release_and_annotated_commit(self):
        refs = "\n".join([
            f'{"b" * 40}\trefs/tags/v1.9.0',
            f'{"c" * 40}\trefs/tags/v1.10.0',
            f'{SHA}\trefs/tags/v1.10.0^{{}}',
            f'{SHA}\trefs/tags/app-v99.0.0',
            f'{SHA}\trefs/tags/v99.0.0-rc1',
        ])
        self.assertEqual(automation.latest_release(refs), ("v1.10.0", SHA))
        self.assertEqual(automation.latest_release(f'{SHA}\trefs/tags/v2.0.0'), ("v2.0.0", SHA))
        for bad in ("", f'bad\trefs/tags/v1.0.0', f'{SHA}\trefs/tags/v1.0.0-rc'):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                automation.latest_release(bad)

    def test_scope_pin_sha_and_paths(self):
        with patch.object(automation, "run", return_value="docs/overview.md"):
            self.assertEqual(automation.scope(ROOT, "pull_request", "main", "")["run"], "false")
        for path in ("internal/agmsg/tool.go", "scripts/agmsg_contract.py", "Makefile", ".github/workflows/agmsg-contract.yml"):
            with self.subTest(path=path), patch.object(automation, "run", return_value=path):
                result = automation.scope(ROOT, "pull_request", "main", "")
                self.assertEqual(result["agmsg_ref"], automation.pins(ROOT)[1])
                self.assertEqual(result["run"], "true")
        with patch.object(automation, "run", return_value=f'{SHA}\trefs/tags/v2.0.0'):
            result = automation.scope(ROOT, "schedule", "", "")
            self.assertEqual(result["agmsg_ref"], SHA)
            self.assertIn(SHA, result["cache_key"])
        self.assertEqual(automation.scope(ROOT, "workflow_dispatch", "", "")["agmsg_ref"], automation.pins(ROOT)[1])
        with patch.object(automation, "run", return_value=f'{SHA}\trefs/tags/v2.0.0^{{}}'):
            self.assertEqual(automation.scope(ROOT, "workflow_dispatch", "", "v2.0.0")["agmsg_ref"], SHA)
        with self.assertRaises(ValueError):
            automation.scope(ROOT, "workflow_dispatch", "", "$(touch unsafe)")

    def test_prepare_updates_pair_capture_and_assertions(self):
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-test-") as tmp:
            root = Path(tmp)
            shutil.copytree(ROOT / "internal/agmsg", root / "internal/agmsg")
            source = root / "source"
            source.mkdir()
            subprocess.run(["git", "init", "-q", str(source)], check=True)
            subprocess.run(["git", "-C", str(source), "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "fixture"], check=True)
            sha = subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip()
            original_run = automation.run
            def capture(*args, **kwargs):
                if args[0][0] == "bash":
                    out = root / "internal/agmsg/testdata/agmsg-v2.0.0"
                    for name in ("get_teams.jsonl", "get_team_members.jsonl", "get_team_messages.jsonl"):
                        shutil.copy(ROOT / "internal/agmsg/testdata/agmsg-v1.2.0" / name, out / name)
                    messages = out / "get_team_messages.jsonl"
                    messages.write_text(messages.read_text().replace("01a02760-c340", "01a11111-aaaa").replace("2026-08-22T02:50:48Z", "2026-10-31T01:02:03Z"))
                    (out / "VERSION.txt").write_text("v2.0.0\n")
                    return ""
                return original_run(*args, **kwargs)
            with patch.object(automation, "run", side_effect=capture):
                automation.prepare(root, "v2.0.0", sha, source)
            self.assertEqual(automation.pins(root), ("2.0.0", sha))
            tests = (root / "internal/agmsg/agmsg_fixture_test.go").read_text()
            self.assertIn('fixtureDir = "testdata/agmsg-v2.0.0"', tests)
            self.assertIn('fixtureID1 = "01a11111-aaaa', tests)
            self.assertIn("time.Date(2026, 10, 31, 1, 2, 3, 0, time.UTC)", tests)
            self.assertIn('os.ReadFile("testdata/agmsg-v1.2.0/get_team_messages.jsonl")', tests)
            with self.assertRaises(ValueError):
                automation.prepare(root, "v2.0.1", SHA, source)

    def test_pr_deduplication_and_missing_credentials(self):
        with patch.dict(os.environ, {"GH_TOKEN": ""}), patch.object(automation, "run") as run:
            automation.open_pr(ROOT, "v2.0.0", SHA, "owner/repo")
            run.assert_not_called()
        with patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", return_value='[{"number": 12}]') as run:
            automation.open_pr(ROOT, "v2.0.0", SHA, "owner/repo")
            self.assertEqual(run.call_count, 1)
            self.assertIn("--state", run.call_args.args[0])
            self.assertIn("all", run.call_args.args[0])

    def test_new_pr_uses_fixed_branch_and_body_file(self):
        calls = []
        def fake(args, **kwargs):
            calls.append(args)
            if args[:3] == ["gh", "pr", "list"]:
                return "[]"
            if args[:3] == ["git", "diff", "--name-only"]:
                return "internal/agmsg/tool.go\n"
            if args[:3] == ["gh", "pr", "create"]:
                self.assertIn("fix:", args[args.index("--title") + 1])
                body = Path(args[args.index("--body-file") + 1]).read_text()
                self.assertIn(SHA, body)
            return ""
        with patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", side_effect=fake):
            automation.open_pr(ROOT, "v2.0.0", SHA, "owner/repo")
        push = next(args for args in calls if args[:2] == ["git", "push"])
        self.assertIn("HEAD:refs/heads/automation/agmsg-v2.0.0", push)
        self.assertFalse(any("--force" in args for args in calls))
        with self.assertRaises(ValueError):
            automation.open_pr(ROOT, "v2.0.0;echo bad", SHA, "owner/repo")

    def test_retry_after_push_creates_pr_without_rewriting_remote(self):
        calls = []
        def fake(args, **kwargs):
            calls.append(args)
            if args[:3] == ["gh", "pr", "list"]:
                return "[]"
            if args[:3] == ["git", "ls-remote", "--heads"]:
                return f'{SHA}\trefs/heads/automation/agmsg-v2.0.0'
            if args[:2] == ["git", "show"]:
                return f'const TestedAgmsgCommit = "{SHA}"' if args[-1].endswith("tool.go") else 'const TestedAgmsgVersion = "2.0.0"'
            return "internal/agmsg/tool.go"
        with patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", side_effect=fake):
            automation.open_pr(ROOT, "v2.0.0", SHA, "owner/repo")
        self.assertTrue(any(args[:3] == ["gh", "pr", "create"] for args in calls))
        self.assertFalse(any(args[:2] == ["git", "push"] for args in calls))
        def moved(args, **kwargs):
            if args[:2] == ["git", "show"]:
                return 'const TestedAgmsgCommit = "' + "b" * 40 + '"'
            return fake(args, **kwargs)
        with patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", side_effect=moved), self.assertRaises(ValueError):
            automation.open_pr(ROOT, "v2.0.0", SHA, "owner/repo")

    def test_real_git_push_and_fake_gh_deliver_only_one_proposal(self):
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-delivery-") as tmp:
            root = Path(tmp)
            repo, remote, binary = root / "repo", root / "remote.git", root / "bin"
            binary.mkdir()
            subprocess.run(["git", "init", "-q", "--bare", str(remote)], check=True)
            subprocess.run(["git", "init", "-q", str(repo)], check=True)
            for key, value in (("user.name", "Test"), ("user.email", "test@example.invalid")):
                automation.run(["git", "config", key, value], cwd=repo)
            (repo / "internal/agmsg").mkdir(parents=True)
            pin = repo / "internal/agmsg/pin"
            pin.write_text("old")
            automation.run(["git", "add", "."], cwd=repo)
            automation.run(["git", "commit", "-qm", "base"], cwd=repo)
            automation.run(["git", "remote", "add", "origin", str(remote)], cwd=repo)
            pin.write_text(SHA)
            gh = binary / "gh"
            gh.write_text('#!/usr/bin/env python3\nimport json, os, pathlib, sys\n'
                          'state = pathlib.Path(os.environ["FAKE_GH_STATE"])\n'
                          'if sys.argv[1:3] == ["pr", "list"]:\n'
                          ' print("[{\\"number\\":1}]" if state.exists() else "[]")\n'
                          'elif sys.argv[1:3] == ["pr", "create"]:\n'
                          ' assert "--body-file" in sys.argv\n'
                          ' state.write_text(pathlib.Path(sys.argv[sys.argv.index("--body-file")+1]).read_text())\n'
                          'else: sys.exit(9)\n')
            gh.chmod(0o755)
            with patch.dict(os.environ, {"PATH": str(binary) + os.pathsep + os.environ["PATH"], "GH_TOKEN": "fake", "FAKE_GH_STATE": str(root / "pr")}):
                automation.open_pr(repo, "v2.0.0", SHA, "owner/repo")
                before = automation.run(["git", "rev-parse", "HEAD"], cwd=repo)
                automation.open_pr(repo, "v2.0.0", SHA, "owner/repo")
                self.assertEqual(before, automation.run(["git", "rev-parse", "HEAD"], cwd=repo))
            self.assertEqual(automation.run(["git", "show", "automation/agmsg-v2.0.0:internal/agmsg/pin"], cwd=remote), SHA)
            self.assertIn(SHA, (root / "pr").read_text())

    def test_capture_stops_on_tmpdir_failure_before_installing(self):
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-guard-") as tmp:
            root = Path(tmp)
            source, binary = root / "source", root / "bin"
            source.mkdir()
            binary.mkdir()
            subprocess.run(["git", "init", "-q", str(source)], check=True)
            (source / "install.sh").write_text('touch "$INSTALL_SENTINEL"\n')
            automation.run(["git", "add", "."], cwd=source)
            automation.run(["git", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture"], cwd=source)
            sha = automation.run(["git", "rev-parse", "HEAD"], cwd=source)
            mktemp = binary / "mktemp"
            mktemp.write_text("#!/bin/sh\nexit 1\n")
            mktemp.chmod(0o755)
            capture = ROOT / "internal/agmsg/testdata/agmsg-v1.2.0/capture.sh"
            env = {**os.environ, "PATH": str(binary) + os.pathsep + os.environ["PATH"], "INSTALL_SENTINEL": str(root / "installed")}
            result = subprocess.run(["bash", str(capture), str(source), sha], env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root / "installed").exists())
            self.assertEqual(automation.run(["git", "status", "--porcelain"], cwd=source), "")

    def test_capture_isolates_home_and_sanitizes_normalized_paths(self):
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-capture-test-") as tmp:
            root = Path(tmp)
            source, out, caller_home = root / "source", root / "agmsg-v1.2.0", root / "caller"
            for directory in (source, out, caller_home):
                directory.mkdir()
            (caller_home / "untouched").write_text("marker")
            shutil.copyfile(ROOT / "internal/agmsg/testdata/agmsg-v1.2.0/capture.sh", out / "capture.sh")
            (source / "install.sh").write_text('''#!/bin/bash
set -eu
test ! -e "$HOME/untouched"
test -z "${AGMSG_STORAGE_PATH:-}"
dest="$HOME/.agents/skills/agmsg"
mkdir -p "$dest/scripts"
for script in join send api version; do cp fake-tool.sh "$dest/scripts/$script.sh"; done
echo v1.2.0 > "$dest/VERSION"
''')
            (source / "fake-tool.sh").write_text('''#!/bin/bash
set -eu
case "$(basename "$0")" in
  join.sh) python3 -c 'import os,sys; print(os.path.normpath(sys.argv[1]))' "$4" > "$HOME/project" ;;
  api.sh) python3 -c 'import json,os,pathlib; print(json.dumps({"project":pathlib.Path(os.environ["HOME"],"project").read_text().strip()}))' ;;
  version.sh) echo v1.2.0 ;;
esac
''')
            for name in ("install.sh", "fake-tool.sh"):
                (source / name).chmod(0o755)
            automation.run(["git", "init", "-q", str(source)])
            automation.run(["git", "add", "."], cwd=source)
            automation.run(["git", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture"], cwd=source)
            sha = automation.run(["git", "rev-parse", "HEAD"], cwd=source)
            env = {**os.environ, "HOME": str(caller_home), "TMPDIR": str(root) + "/", "AGMSG_STORAGE_PATH": str(caller_home / "forbidden")}
            subprocess.run(["bash", str(out / "capture.sh"), str(source), sha], env=env, check=True, capture_output=True)
            self.assertEqual(json.loads((out / "get_team_members.jsonl").read_text())["project"], "/tmp/sample-project")
            self.assertEqual(list(caller_home.iterdir()), [caller_home / "untouched"])
            self.assertEqual((out / "COMMIT.txt").read_text().strip(), sha)


if __name__ == "__main__":
    unittest.main()
