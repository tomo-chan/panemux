#!/usr/bin/env python3
"""agmsg contract CI operations. External values are operands, never shell code."""
import argparse
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


DATA_FILES = ("get_teams.jsonl", "get_team_members.jsonl", "get_team_messages.jsonl", "VERSION.txt", "COMMIT.txt")


def validate_files(files, tag, sha):
    validate(tag, sha)
    if not isinstance(files, dict) or set(files) != set(DATA_FILES):
        raise ValueError("artifact must contain only the five captured data files")
    for name, value in files.items():
        if not isinstance(value, str) or len(value.encode()) > 1024 * 1024 or "\\x00" in value:
            raise ValueError("invalid or oversized capture data")
        if name.endswith(".jsonl"):
            rows = [json.loads(line) for line in value.splitlines()]
            if not rows or not all(isinstance(row, dict) for row in rows):
                raise ValueError("capture must contain JSON objects")
    messages = [json.loads(line) for line in files["get_team_messages.jsonl"].splitlines()]
    if len(messages) != 4 or any(not isinstance(row.get("id"), str) for row in messages):
        raise ValueError("capture must contain four opaque message IDs")
    if len({row["id"] for row in messages}) != 4:
        raise ValueError("capture must contain four distinct message IDs")
    if files["COMMIT.txt"].strip() != sha or files["VERSION.txt"].strip().removeprefix("v") != tag[1:]:
        raise ValueError("capture provenance differs from trusted scope outputs")
    return files


def read_bundle(directory, tag, sha):
    path = directory / "bundle.json"
    if set(directory.iterdir()) != {path} or path.is_symlink() or not path.is_file() or path.stat().st_size > 6 * 1024 * 1024:
        raise ValueError("expected one bounded regular bundle.json file")
    bundle = json.loads(path.read_text())
    if set(bundle) != {"tag", "sha", "files"} or bundle["tag"] != tag or bundle["sha"] != sha:
        raise ValueError("artifact metadata differs from trusted scope outputs")
    return validate_files(bundle["files"], tag, sha)


def capture_files(root, tag, sha, source):
    validate(tag, sha)
    if run(["git", "rev-parse", "HEAD"], cwd=source) != sha:
        raise ValueError("fixture source is not the contract-tested commit")
    version, _ = pins(root)
    with tempfile.TemporaryDirectory(prefix="panemux-agmsg-data-") as tmp:
        out = Path(tmp)
        script = root / f"internal/agmsg/testdata/agmsg-v{version}/capture.sh"
        # The trusted script accepts an explicit output directory. No generated
        # script or source code crosses into the privileged publication job.
        run(["bash", str(script), str(source), sha, str(out)], cwd=root)
        return validate_files({name: (out / name).read_text() for name in DATA_FILES}, tag, sha)


def apply_files(root, tag, sha, files):
    validate_files(files, tag, sha)
    version, _ = pins(root)
    if tuple(map(int, tag[1:].split("."))) <= tuple(map(int, version.split("."))):
        print("latest release is already pinned or older; no bump")
        return
    out = root / f"internal/agmsg/testdata/agmsg-{tag}"
    out.mkdir()
    # Copy only trusted checkout code. Artifacts never supply executable files,
    # Go source, diffs, paths, hooks, modes, or documentation.
    shutil.copyfile(root / f"internal/agmsg/testdata/agmsg-v{version}/capture.sh", out / "capture.sh")
    (out / "README.md").write_text(
        f"# agmsg {tag} fixtures\n\nCaptured at verified commit {sha}.\n\n"
        "capture.sh <source-checkout> <commit-sha> installs into a throwaway HOME, "
        "joins a team, sends four messages and captures api.sh JSONL. It requires "
        "sqlite3 and Python 3. VERSION.txt and COMMIT.txt record provenance. "
        "Project paths are sanitized to /tmp/sample-project. The tests derive IDs "
        "and timestamps from this capture; the original v1.2.0 directory retains "
        "the historical ID-order regression.\n")
    for name, value in files.items():
        (out / name).write_text(value)
    replace(root / "internal/agmsg/agmsg_version.go", r'^const TestedAgmsgVersion = ".*"$', f'const TestedAgmsgVersion = "{tag[1:]}"')
    replace(root / "internal/agmsg/tool.go", r'^const TestedAgmsgCommit = ".*"$', f'const TestedAgmsgCommit = "{sha}"')


def prepare(root, tag, sha, source):
    apply_files(root, tag, sha, capture_files(root, tag, sha, source))


def proposal_mode(root, tag, sha, repo):
    validate(tag, sha)
    version, _ = pins(root)
    if tuple(map(int, tag[1:].split("."))) <= tuple(map(int, version.split("."))):
        return "skip"
    branch = f"automation/agmsg-{tag}"
    if json.loads(run(["gh", "pr", "list", "--repo", repo, "--head", branch, "--base", "main", "--state", "all", "--json", "number"], cwd=root)):
        return "skip"
    if run(["git", "ls-remote", "--heads", "origin", f"refs/heads/{branch}"], cwd=root):
        return "resume"
    return "new"


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
                        "were recaptured in an isolated HOME; pin-independent fixture assertions were tested. "
                        "PR CI rechecks the recorded SHA.\n\n"
                        "Review the upstream release, fixtures and CI before merging. Older release PRs "
                        "remain open for human review; close superseded proposals manually. "
                        "The draft panemux release still requires human publication.\n\nRefs #437, #438.\n")
        run(["gh", "pr", "create", "--repo", repo, "--head", branch, "--base", "main", "--title", title, "--body-file", str(body)], cwd=root)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["scope", "prepare", "capture", "apply", "preflight", "pr"])
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
        elif args.operation == "capture":
            files = capture_files(root, tag, sha, Path(os.environ["AGMSG_SOURCE"]))
            directory = Path(os.environ["BUNDLE_DIR"])
            directory.mkdir()
            (directory / "bundle.json").write_text(json.dumps({"tag": tag, "sha": sha, "files": files}))
        elif args.operation == "apply":
            apply_files(root, tag, sha, read_bundle(Path(os.environ["BUNDLE_DIR"]), tag, sha))
        elif args.operation == "preflight":
            mode = proposal_mode(root, tag, sha, os.environ["GITHUB_REPOSITORY"])
            with open(os.environ["GITHUB_OUTPUT"], "a") as output:
                output.write(f"mode={mode}\n")
        else:
            open_pr(root, tag, sha, os.environ["GITHUB_REPOSITORY"])


if __name__ == "__main__":
    main()
