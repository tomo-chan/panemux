#!/usr/bin/env python3
"""agmsg contract CI operations. External values are operands, never shell code."""
import argparse
from datetime import datetime
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

UPSTREAM = "https://github.com/fujibee/agmsg.git"
TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\Z")
SHA = re.compile(r"[0-9a-f]{40}\Z")


def run(args, cwd=None):
    return subprocess.run(args, cwd=cwd, check=True, text=True, stdout=subprocess.PIPE).stdout.strip()


def validate(tag, sha):
    if not TAG.fullmatch(tag) or not SHA.fullmatch(sha):
        raise ValueError("expected a stable vN.N.N tag and full lowercase commit SHA")


def latest_release(refs):
    tags, peeled = {}, {}
    for line in refs.splitlines():
        sha, ref = line.split()
        tag = ref.removeprefix("refs/tags/")
        if tag.endswith("^{}"):
            peeled[tag[:-3]] = sha
        elif TAG.fullmatch(tag):
            tags[tag] = sha
    if not tags:
        raise ValueError("no stable agmsg release tags found")
    tag = max(tags, key=lambda value: tuple(map(int, TAG.fullmatch(value).groups())))
    sha = peeled.get(tag, tags[tag])
    validate(tag, sha)
    return tag, sha


def pins(root):
    version = re.search(r'^const TestedAgmsgVersion = "([^"]+)"$', (root / "internal/agmsg/agmsg_version.go").read_text(), re.M)[1]
    sha = re.search(r'^const TestedAgmsgCommit = "([^"]+)"$', (root / "internal/agmsg/tool.go").read_text(), re.M)[1]
    validate("v" + version, sha)
    return version, sha


def scope(root, event, base, dispatch):
    version, sha = pins(root)
    result = {"run": "true", "agmsg_ref": sha, "agmsg_tag": "v" + version, "cache_key": ""}
    if event == "schedule":
        tag, sha = latest_release(run(["git", "ls-remote", "--tags", UPSTREAM, "v*"]))
        result.update(agmsg_tag=tag, agmsg_ref=sha, cache_key=f"agmsg-contract-v2-{tag}-{sha}")
    elif event == "workflow_dispatch":
        if dispatch:
            if SHA.fullmatch(dispatch):
                result["agmsg_ref"] = dispatch
            elif TAG.fullmatch(dispatch):
                refs = run(["git", "ls-remote", "--tags", UPSTREAM, f"refs/tags/{dispatch}", f"refs/tags/{dispatch}^{{}}"])
                entries = dict(line.split()[::-1] for line in refs.splitlines())
                sha = entries.get(f"refs/tags/{dispatch}^{{}}", entries.get(f"refs/tags/{dispatch}", ""))
                validate(dispatch, sha)
                result.update(agmsg_tag=dispatch, agmsg_ref=sha)
            else:
                raise ValueError("dispatch ref must be a stable release tag or a full commit SHA")
    elif event == "pull_request":
        # base comes from GitHub's PR base, passed as an argv operand.
        run(["git", "fetch", "--quiet", "origin", base], cwd=root)
        changed = run(["git", "diff", "--name-only", f"origin/{base}...HEAD"], cwd=root)
        relevant = re.compile(r"^(internal/agmsg/|scripts/agmsg_contract|Makefile$|\.github/workflows/agmsg-contract\.yml$)")
        result["run"] = str(any(relevant.match(path) for path in changed.splitlines())).lower()
    else:
        raise ValueError("unsupported workflow event")
    return result


def replace(path, pattern, value):
    text, count = re.subn(pattern, lambda _: value, path.read_text(), flags=re.M)
    if count != 1:
        raise ValueError(f"expected one update location in {path.name}, found {count}")
    path.write_text(text)


def prepare(root, tag, sha, source):
    validate(tag, sha)
    if run(["git", "rev-parse", "HEAD"], cwd=source) != sha:
        raise ValueError("fixture source is not the contract-tested commit")
    version, _ = pins(root)
    if tuple(map(int, tag[1:].split("."))) <= tuple(map(int, version.split("."))):
        print("latest release is already pinned or older; no bump")
        return
    old = root / f"internal/agmsg/testdata/agmsg-v{version}"
    out = root / f"internal/agmsg/testdata/agmsg-{tag}"
    # Keep the old capture: it proves that lexicographic ID order can differ
    # from API order. A fresh capture need not happen within one millisecond.
    out.mkdir()
    shutil.copyfile(old / "capture.sh", out / "capture.sh")
    (out / "README.md").write_text(
        f"# agmsg {tag} fixtures\n\n"
        f"Captured from release `{tag}` at commit `{sha}`, after the real-install contract passed.\n"
        "`capture.sh <source-checkout> <commit-sha>` installs into a throwaway HOME, joins a team, "
        "sends four messages and records raw api.sh JSONL. It requires sqlite3 and Python 3; "
        "check out the supplied SHA first. Project paths are sanitized to `/tmp/sample-project`.\n\n"
        "VERSION.txt records installer provenance; COMMIT.txt records the verified source SHA. "
        "get_teams.jsonl, get_team_members.jsonl and get_team_messages.jsonl are captured through "
        "the public API. Exact IDs and timestamps in the fixture test come from this capture. "
        "The original v1.2.0 directory remains the historical ID-order regression fixture.\n")
    run(["bash", str(out / "capture.sh"), str(source), sha], cwd=root)
    rows = [json.loads(line) for line in (out / "get_team_messages.jsonl").read_text().splitlines()]
    if len(rows) != 4 or len({row["id"] for row in rows}) != 4:
        raise ValueError("capture must contain four distinct message IDs")
    if (out / "VERSION.txt").read_text().strip().removeprefix("v") != tag[1:]:
        raise ValueError("captured installation has unexpected provenance")
    tests = root / "internal/agmsg/agmsg_fixture_test.go"
    replace(tests, r'^const fixtureDir = ".*"$', f'const fixtureDir = "testdata/agmsg-{tag}"')
    for i, row in enumerate(rows, 1):
        replace(tests, rf'^\s*fixtureID{i} = ".*"$', f'\tfixtureID{i} = {json.dumps(row["id"])}')
    at = datetime.fromisoformat(rows[0]["at"].replace("Z", "+00:00"))
    replace(tests, r'\tassert.Equal\(t, time.Date\(.*\), rows\[0\].At\)',
            f'\tassert.Equal(t, time.Date({at.year}, {at.month}, {at.day}, {at.hour}, {at.minute}, {at.second}, {at.microsecond * 1000}, time.UTC), rows[0].At)')
    text = tests.read_text()
    start = text.index("func TestAgmsgFixture_TeamMessages_LexicographicOrderDiffersFromReturnedOrder")
    before, historic = text[:start], text[start:]
    historic = historic.replace('data := readFixture(t, "get_team_messages.jsonl")',
                                'data, err := os.ReadFile("testdata/agmsg-v1.2.0/get_team_messages.jsonl")\n\trequire.NoError(t, err)', 1)
    tests.write_text(before + historic)
    replace(root / "internal/agmsg/agmsg_version.go", r'^const TestedAgmsgVersion = ".*"$', f'const TestedAgmsgVersion = "{tag[1:]}"')
    replace(root / "internal/agmsg/tool.go", r'^const TestedAgmsgCommit = ".*"$', f'const TestedAgmsgCommit = "{sha}"')
    run(["gofmt", "-w", str(tests)], cwd=root)


def open_pr(root, tag, sha, repo):
    validate(tag, sha)
    if not os.environ.get("GH_TOKEN"):
        print("GitHub App credentials unavailable; no PR created")
        return
    branch = f"automation/agmsg-{tag}"
    # All states prevent reopening a deliberately closed/merged proposal.
    existing = json.loads(run(["gh", "pr", "list", "--repo", repo, "--head", branch, "--base", "main", "--state", "all", "--json", "number"], cwd=root))
    if existing:
        print(f"{branch} already has a PR; leaving it unchanged")
        return
    remote = run(["git", "ls-remote", "--heads", "origin", f"refs/heads/{branch}"], cwd=root)
    if remote:
        run(["git", "fetch", "--quiet", "origin", f"refs/heads/{branch}"], cwd=root)
        for file, constant, expected in (("tool.go", "TestedAgmsgCommit", sha),
                                         ("agmsg_version.go", "TestedAgmsgVersion", tag[1:])):
            content = run(["git", "show", f"FETCH_HEAD:internal/agmsg/{file}"], cwd=root)
            if f'const {constant} = "{expected}"' not in content.splitlines():
                raise ValueError("existing proposal branch has a different pin; inspect upstream retagging manually")
    if not remote and not run(["git", "diff", "--name-only"], cwd=root):
        print("pin unchanged; no PR created")
        return
    title = f"fix: bump pinned agmsg to {tag}"
    if not remote:
        run(["git", "checkout", "-b", branch], cwd=root)
        run(["git", "add", "internal/agmsg"], cwd=root)
        run(["git", "-c", "user.name=agmsg compatibility bot", "-c", "user.email=agmsg-bot@users.noreply.github.com", "commit", "-m", title], cwd=root)
        run(["git", "push", "origin", f"HEAD:refs/heads/{branch}"], cwd=root)
    with tempfile.TemporaryDirectory(prefix="panemux-agmsg-pr-") as tmp:
        body = Path(tmp) / "body.md"
        body.write_text(f"Bump the tested agmsg release to `{tag}` at commit `{sha}`.\n\n"
                        "The scheduled real-install contract passed at this exact SHA. Tier 1 fixtures "
                        "were recaptured in an isolated HOME; fixture assertions were updated and tested. "
                        "PR CI rechecks the recorded SHA.\n\n"
                        "Review the upstream release, fixtures and CI before merging. Older release PRs "
                        "remain open for human review; close superseded proposals manually. "
                        "The draft panemux release still requires human publication.\n\nRefs #437, #438.\n")
        run(["gh", "pr", "create", "--repo", repo, "--head", branch, "--base", "main", "--title", title, "--body-file", str(body)], cwd=root)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["scope", "prepare", "pr"])
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    if args.operation == "scope":
        result = scope(root, os.environ["EVENT"], os.environ.get("BASE_REF", ""), os.environ.get("DISPATCH_REF", ""))
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            output.writelines(f"{key}={value}\n" for key, value in result.items())
    else:
        tag, sha = os.environ["AGMSG_TAG"], os.environ["AGMSG_SHA"]
        if args.operation == "prepare":
            prepare(root, tag, sha, Path(os.environ["AGMSG_SOURCE"]))
        else:
            open_pr(root, tag, sha, os.environ["GITHUB_REPOSITORY"])


if __name__ == "__main__":
    main()
