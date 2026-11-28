"""Offline tests: real throwaway git repos, fake upstream/install/gh boundaries."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
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
                    out = Path(args[0][-1])
                    for name in ("get_teams.jsonl", "get_team_members.jsonl", "get_team_messages.jsonl"):
                        shutil.copy(ROOT / "internal/agmsg/testdata/agmsg-v1.2.0" / name, out / name)
                    messages = out / "get_team_messages.jsonl"
                    messages.write_text(messages.read_text().replace("01a02760-c340", "01a11111-aaaa").replace("2026-08-22T02:50:48Z", "2026-10-31T01:02:03Z"))
                    (out / "VERSION.txt").write_text("v2.0.0\n")
                    (out / "COMMIT.txt").write_text(sha + "\n")
                    return ""
                return original_run(*args, **kwargs)
            original_tests = (root / "internal/agmsg/agmsg_fixture_test.go").read_bytes()
            with patch.object(automation, "run", side_effect=capture):
                automation.prepare(root, "v2.0.0", sha, source)
            self.assertEqual(automation.pins(root), ("2.0.0", sha))
            self.assertEqual(original_tests, (root / "internal/agmsg/agmsg_fixture_test.go").read_bytes())
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
            mktemp.write_text("#!/bin/sh\ntouch \"$MKTEMP_SENTINEL\"\nexit 1\n")
            mktemp.chmod(0o755)
            sqlite = binary / "sqlite3"
            sqlite.write_text("#!/bin/sh\nexit 99\n")
            sqlite.chmod(0o755)
            capture = ROOT / "internal/agmsg/testdata/agmsg-v1.2.0/capture.sh"
            env = {**os.environ, "PATH": str(binary) + os.pathsep + os.environ["PATH"], "INSTALL_SENTINEL": str(root / "installed"), "MKTEMP_SENTINEL": str(root / "mktemp-called")}
            result = subprocess.run(["bash", str(capture), str(source), sha], env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root / "installed").exists())
            self.assertTrue((root / "mktemp-called").exists())
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
            binary = root / "bin"
            binary.mkdir()
            sqlite = binary / "sqlite3"
            sqlite.write_text("#!/bin/sh\nexit 99\n")
            sqlite.chmod(0o755)
            env["PATH"] = str(binary) + os.pathsep + env["PATH"]
            subprocess.run(["bash", str(out / "capture.sh"), str(source), sha], env=env, check=True, capture_output=True)
            self.assertEqual(json.loads((out / "get_team_members.jsonl").read_text())["project"], "/tmp/sample-project")
            self.assertEqual(list(caller_home.iterdir()), [caller_home / "untouched"])
            self.assertEqual((out / "COMMIT.txt").read_text().strip(), sha)

    def test_bundle_rejects_code_paths_metadata_mismatch_and_symlinks(self):
        files = {name: (ROOT / "internal/agmsg/testdata/agmsg-v1.2.0" / name).read_text()
                 for name in automation.DATA_FILES if name != "COMMIT.txt"}
        files["COMMIT.txt"] = SHA + "\n"
        files["VERSION.txt"] = "v2.0.0\n"
        bundle = {"tag": "v2.0.0", "sha": SHA, "files": files}
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-artifact-") as tmp:
            directory = Path(tmp)
            path = directory / "bundle.json"
            path.write_text(json.dumps(bundle))
            self.assertEqual(automation.read_bundle(directory, "v2.0.0", SHA), files)
            for key, value in (("../tool.go", "malicious"), ("capture.sh", "malicious"), ("README.md", "malicious")):
                changed = {**bundle, "files": {**files, key: value}}
                path.write_text(json.dumps(changed))
                with self.subTest(key=key), self.assertRaises(ValueError):
                    automation.read_bundle(directory, "v2.0.0", SHA)
            path.write_text(json.dumps({**bundle, "sha": "b" * 40}))
            with self.assertRaises(ValueError):
                automation.read_bundle(directory, "v2.0.0", SHA)
            target = directory / "other"
            path.rename(target)
            path.symlink_to(target)
            with self.assertRaises(ValueError):
                automation.read_bundle(directory, "v2.0.0", SHA)

    def test_capture_data_rejects_nul_but_accepts_literal_escape_text(self):
        files = {name: (ROOT / "internal/agmsg/testdata/agmsg-v1.2.0" / name).read_text()
                 for name in automation.DATA_FILES}
        tag, sha = automation.pins(ROOT)
        files["VERSION.txt"] = "v" + tag + "\n"
        files["COMMIT.txt"] = sha + "\n"
        for name in automation.DATA_FILES:
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "invalid or oversized capture data"):
                automation.validate_files({**files, name: files[name] + "\x00"}, "v" + tag, sha)
        files["get_teams.jsonl"] = json.dumps({"name": "literal \\x00 text"}) + "\n"
        self.assertEqual(automation.validate_files(files, "v" + tag, sha), files)

    def test_preflight_skips_existing_pr_and_resumes_branch_without_capture(self):
        for existing, remote, mode in (("[]", "", "new"), ('[{"number":1}]', "", "skip"), ("[]", SHA, "resume")):
            def fake(args, **kwargs):
                return existing if args[0] == "gh" else remote
            with self.subTest(mode=mode), patch.object(automation, "run", side_effect=fake):
                self.assertEqual(automation.proposal_mode(ROOT, "v99.0.0", SHA, "owner/repo"), mode)

    def test_workflow_keeps_upstream_execution_away_from_app_credentials(self):
        workflow = (ROOT / ".github/workflows/agmsg-contract.yml").read_text()
        capture, publish = workflow.split("\n  capture:\n")[1].split("\n  propose-bump:\n")
        self.assertNotIn("secrets.", capture)
        self.assertNotIn("create-github-app-token", capture)
        self.assertNotIn("git clone", publish)
        self.assertNotIn("./install.sh", publish)
        self.assertIn("scripts/agmsg_contract.py apply", publish)
        self.assertLess(publish.index("scripts/agmsg_contract.py apply"), publish.index("create-github-app-token"))

    def test_cli_routes_data_operations_without_entering_publication(self):
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-cli-") as tmp:
            directory = Path(tmp)
            env = {"AGMSG_TAG": "v2.0.0", "AGMSG_SHA": SHA, "BUNDLE_DIR": str(directory / "artifact"),
                   "AGMSG_SOURCE": str(directory), "GITHUB_OUTPUT": str(directory / "output"), "GITHUB_REPOSITORY": "owner/repo"}
            with patch.dict(os.environ, env), patch.object(automation, "open_pr", side_effect=AssertionError("unexpected privileged operation")):
                with patch.object(sys, "argv", ["automation", "capture"]), patch.object(automation, "capture_files", return_value={"data": "value"}):
                    automation.main()
                    self.assertEqual(json.loads((directory / "artifact/bundle.json").read_text())["sha"], SHA)
                with patch.object(sys, "argv", ["automation", "apply"]), patch.object(automation, "read_bundle", return_value={}) as read, patch.object(automation, "apply_files") as apply:
                    automation.main()
                    read.assert_called_once()
                    apply.assert_called_once()
                with patch.object(sys, "argv", ["automation", "preflight"]), patch.object(automation, "proposal_mode", return_value="skip"):
                    automation.main()
                    self.assertEqual((directory / "output").read_text(), "mode=skip\n")

    def test_open_pr_labels_same_major_only_and_skips_major(self):
        for tag, base_version, expect_label in (
            ("v1.9.3", "1.9.2", True),
            ("v1.10.0", "1.9.2", True),
            ("v2.0.0", "1.9.2", False),
        ):
            created = []
            def fake(args, **kwargs):
                if args[:3] == ["gh", "pr", "list"]:
                    return "[]"
                if args[:3] == ["git", "diff", "--name-only"]:
                    return "internal/agmsg/tool.go\n"
                if args[:3] == ["gh", "pr", "create"]:
                    created.append((args, Path(args[args.index("--body-file") + 1]).read_text()))
                return ""
            with self.subTest(tag=tag), patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", side_effect=fake):
                automation.open_pr(ROOT, tag, SHA, "owner/repo", base_version=base_version)
            self.assertEqual(len(created), 1)
            args, body = created[0]
            if expect_label:
                self.assertIn("--label", args)
                self.assertEqual(args[args.index("--label") + 1], automation.AUTOMERGE_LABEL)
                self.assertIn(automation.AUTOMERGE_LABEL, body)
            else:
                self.assertNotIn("--label", args)
                self.assertIn("manual review", body.lower())

    def test_open_pr_resume_labels_same_major(self):
        created = []
        def fake(args, **kwargs):
            if args[:3] == ["gh", "pr", "list"]:
                return "[]"
            if args[:3] == ["git", "ls-remote", "--heads"]:
                return f'{SHA}\trefs/heads/automation/agmsg-v1.9.3'
            if args[:2] == ["git", "show"]:
                return f'const TestedAgmsgCommit = "{SHA}"' if args[-1].endswith("tool.go") else 'const TestedAgmsgVersion = "1.9.3"'
            if args[:3] == ["gh", "pr", "create"]:
                created.append(args)
            return ""
        with patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", side_effect=fake):
            automation.open_pr(ROOT, "v1.9.3", SHA, "owner/repo", base_version="1.9.2")
        self.assertEqual(len(created), 1)
        self.assertIn("--label", created[0])
        self.assertEqual(created[0][created[0].index("--label") + 1], automation.AUTOMERGE_LABEL)

    def _automerge_fixture(self, tag="v1.9.3", agmsg_sha=SHA, head_sha="b" * 40, base_version="1.9.2"):
        ver = tag[1:]
        pr = {
            "number": 700,
            "state": "open",
            "draft": False,
            "mergeable": True,
            "mergeable_state": "clean",
            "base": {"ref": "main"},
            "head": {"ref": f"automation/agmsg-{tag}", "sha": head_sha, "repo": {"full_name": "owner/repo"}},
            "user": {"login": "panemux-agmsg-bump[bot]", "type": "Bot"},
            "labels": [{"name": automation.AUTOMERGE_LABEL}, {"name": "backend"}],
        }
        files = [
            {"filename": "internal/agmsg/agmsg_version.go", "status": "modified", "additions": 1, "deletions": 1},
            {"filename": "internal/agmsg/tool.go", "status": "modified", "additions": 1, "deletions": 1},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/COMMIT.txt", "status": "added", "additions": 1, "deletions": 0},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/README.md", "status": "added", "additions": 5, "deletions": 0},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/VERSION.txt", "status": "added", "additions": 1, "deletions": 0},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/capture.sh", "status": "added", "additions": 68, "deletions": 0},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/get_team_members.jsonl", "status": "added", "additions": 2, "deletions": 0},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/get_team_messages.jsonl", "status": "added", "additions": 4, "deletions": 0},
            {"filename": f"internal/agmsg/testdata/agmsg-{tag}/get_teams.jsonl", "status": "added", "additions": 1, "deletions": 0},
        ]
        contents = {
            "internal/agmsg/agmsg_version.go": f'package agmsg\nconst TestedAgmsgVersion = "{ver}"\n',
            "internal/agmsg/tool.go": f'package agmsg\nconst TestedAgmsgCommit = "{agmsg_sha}"\n',
            f"internal/agmsg/testdata/agmsg-{tag}/VERSION.txt": f"{tag}\n",
            f"internal/agmsg/testdata/agmsg-{tag}/COMMIT.txt": f"{agmsg_sha}\n",
        }
        check_runs = [
            {"id": idx + 10, "name": name, "status": "completed", "conclusion": "success"}
            for idx, name in enumerate(sorted(automation.REQUIRED_CHECKS))
        ]
        check_runs.extend([
            {"id": 1, "name": "ci", "status": "completed", "conclusion": "failure"},
            {"id": 100, "name": "capture", "status": "completed", "conclusion": "skipped"},
            {"id": 101, "name": "propose-bump", "status": "completed", "conclusion": "skipped"},
            {"id": 102, "name": "scope", "status": "completed", "conclusion": "success"},
            {"id": 103, "name": "agmsg-automerge-verify", "status": "in_progress", "conclusion": None},
        ])
        status = {"state": "pending", "statuses": []}
        return {"pr": pr, "files": files, "contents": contents, "check_runs": check_runs, "status": status, "base_version": base_version}

    def _run_evaluate(self, state):
        head_sha = state["pr"]["head"]["sha"]
        def fake(args, **kwargs):
            endpoint = args[-1]
            if endpoint.endswith(f"/commits/{head_sha}/pulls"):
                return json.dumps([state["pr"]])
            if endpoint.endswith(f"/pulls/{state['pr']['number']}"):
                return json.dumps(state["pr"])
            if f"/pulls/{state['pr']['number']}/files" in endpoint:
                return json.dumps([state["files"]])
            if "/contents/" in endpoint:
                path = endpoint.split("/contents/", 1)[1].split("?", 1)[0]
                return state["contents"][path]
            if f"/commits/{head_sha}/check-runs" in endpoint:
                return json.dumps([{"check_runs": state["check_runs"]}])
            if endpoint.endswith(f"/commits/{head_sha}/status"):
                return json.dumps(state["status"])
            raise AssertionError(f"unexpected gh call: {args}")
        with patch.object(automation, "pins", return_value=(state["base_version"], "c" * 40)), patch.object(automation, "run", side_effect=fake):
            return automation.evaluate_automerge(ROOT, "owner/repo", head_sha)

    def test_evaluate_automerge_accepts_verified_same_major_pr(self):
        state = self._automerge_fixture()
        result = self._run_evaluate(state)
        self.assertEqual(result["eligible"], "true")
        self.assertEqual(result["pr_number"], "700")
        self.assertEqual(result["head_sha"], "b" * 40)
        self.assertEqual(result["tag"], "v1.9.3")

    def test_evaluate_automerge_rejects_unsafe_or_incomplete_prs(self):
        def mutate(fn):
            state = self._automerge_fixture()
            fn(state)
            return state

        cases = {
            "draft": mutate(lambda s: s["pr"].update(draft=True)),
            "missing_label": mutate(lambda s: s["pr"].update(labels=[{"name": "backend"}])),
            "non_bot_author": mutate(lambda s: s["pr"].update(user={"login": "human", "type": "User"})),
            "fork_pr": mutate(lambda s: s["pr"]["head"].update(repo={"full_name": "evil/repo"})),
            "non_main_base": mutate(lambda s: s["pr"]["base"].update(ref="develop")),
            "dirty_mergeable": mutate(lambda s: s["pr"].update(mergeable=False, mergeable_state="dirty")),
            "major_bump": self._automerge_fixture(tag="v2.0.0", base_version="1.9.2"),
            "not_newer": self._automerge_fixture(tag="v1.9.2", base_version="1.9.2"),
            "extra_file": mutate(lambda s: s["files"].append({"filename": "main.go", "status": "modified", "additions": 1, "deletions": 0})),
            "extra_go_lines": mutate(lambda s: s["files"][1].update(additions=2)),
            "version_mismatch": mutate(lambda s: s["contents"].update({"internal/agmsg/testdata/agmsg-v1.9.3/VERSION.txt": "v1.9.4\n"})),
            "commit_mismatch": mutate(lambda s: s["contents"].update({"internal/agmsg/testdata/agmsg-v1.9.3/COMMIT.txt": "d" * 40 + "\n"})),
            "missing_required_check": mutate(lambda s: s["check_runs"].__setitem__(0, {"id": 999, "name": "other", "status": "completed", "conclusion": "success"})),
            "failed_required_check": mutate(lambda s: s["check_runs"].append({"id": 999, "name": "ci", "status": "completed", "conclusion": "failure"})),
            "pending_required_check": mutate(lambda s: s["check_runs"].append({"id": 999, "name": "contract", "status": "in_progress", "conclusion": None})),
            "failed_other_check": mutate(lambda s: s["check_runs"].append({"id": 999, "name": "Analyze (go)", "status": "completed", "conclusion": "failure"})),
            "pending_other_check": mutate(lambda s: s["check_runs"].append({"id": 999, "name": "Analyze (go)", "status": "queued", "conclusion": None})),
            "failed_commit_status": mutate(lambda s: s["status"].update(state="failure", statuses=[{"context": "ext", "state": "failure"}])),
        }
        for name, state in cases.items():
            with self.subTest(case=name):
                result = self._run_evaluate(state)
                self.assertEqual(result["eligible"], "false", f"{name} should be rejected: {result}")

    def test_merge_pr_uses_match_head_commit_and_validates_inputs(self):
        calls = []
        def fake(args, **kwargs):
            calls.append(args)
            return ""
        with patch.dict(os.environ, {"GH_TOKEN": "fake"}), patch.object(automation, "run", side_effect=fake):
            automation.merge_pr(ROOT, "owner/repo", "700", SHA)
        self.assertEqual(calls, [[
            "gh", "pr", "merge", "700", "--repo", "owner/repo",
            "--squash", "--delete-branch", "--match-head-commit", SHA,
        ]])
        for bad_num, bad_sha in (("abc", SHA), ("0", SHA), ("700", "not-a-sha")):
            with self.subTest(bad_num=bad_num, bad_sha=bad_sha), patch.dict(os.environ, {"GH_TOKEN": "fake"}), self.assertRaises(ValueError):
                automation.merge_pr(ROOT, "owner/repo", bad_num, bad_sha)
        with patch.dict(os.environ, {"GH_TOKEN": ""}), self.assertRaises(ValueError):
            automation.merge_pr(ROOT, "owner/repo", "700", SHA)

    def test_automerge_workflow_separates_verification_from_app_credentials(self):
        workflow = (ROOT / ".github/workflows/agmsg-automerge.yml").read_text()
        verify, merge = workflow.split("\n  agmsg-automerge-verify:\n")[1].split("\n  agmsg-automerge-merge:\n")
        self.assertNotIn("secrets.", verify)
        self.assertNotIn("create-github-app-token", verify)
        self.assertNotIn("github.event.pull_request", verify)
        self.assertIn("scripts/agmsg_contract.py verify-automerge", verify)
        self.assertIn("needs: agmsg-automerge-verify", merge)
        self.assertIn("needs.agmsg-automerge-verify.outputs.eligible == 'true'", merge)
        self.assertIn("create-github-app-token", merge)
        self.assertIn("scripts/agmsg_contract.py automerge", merge)

    def test_cli_routes_verify_automerge_and_automerge(self):
        with tempfile.TemporaryDirectory(prefix="panemux-agmsg-automerge-cli-") as tmp:
            directory = Path(tmp)
            output = directory / "output"
            env = {"HEAD_SHA": SHA, "PR_NUMBER": "700", "GITHUB_OUTPUT": str(output), "GITHUB_REPOSITORY": "owner/repo"}
            with patch.dict(os.environ, env), patch.object(automation, "merge_pr") as merge:
                with patch.object(sys, "argv", ["automation", "verify-automerge"]), patch.object(
                    automation, "evaluate_automerge", return_value={"eligible": "true", "pr_number": "700", "head_sha": SHA, "tag": "v1.9.3", "reason": "eligible"}
                ):
                    automation.main()
                    merge.assert_not_called()
                    self.assertIn("eligible=true\n", output.read_text())
                with patch.object(sys, "argv", ["automation", "automerge"]):
                    automation.main()
                    merge.assert_called_once_with(ROOT, "owner/repo", "700", SHA)


if __name__ == "__main__":
    unittest.main()
