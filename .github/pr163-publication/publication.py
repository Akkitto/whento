#!/usr/bin/env python3
"""Fork-only, creation-only PR163 publication. No PR mutations or force updates."""

import argparse
import difflib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

FORK = "Akkitto/whento"
UPSTREAM = "When-To/whento"
FORK_URL = "https://github.com/Akkitto/whento.git"
UPSTREAM_URL = "https://github.com/When-To/whento.git"
INITIAL = "2ea39819d2c2c7b4c808a1cbc4ec4751fd304315"
# PR 173 was reviewed and squash-merged with additional security fixes. Topic 3
# must start from that accepted tree, not the original pre-review source-2 tip.
REVIEWED_BACKEND_MERGE = "f33ec7d916804610706bd589d95193fb0cc3fc22"
# PR 180 and the maintainer's #183/#184 follow-ups are the accepted base for
# topic 4. Separate reviewed source refs retain the original frozen snapshots.
REVIEWED_BOOTSTRAP_FOLLOWUPS = "a1e698d05ac2cb03bdac676832c348b75e35d636"
TOPICS = [
    ("ci-compose", "6d928b6080f037767eb5507a82adc4a199363704"),
    ("backend-hardening", "963b270ecc0559ae93909f3b2a3c3d4ed35c7cee"),
    ("bootstrap-password", "06e4a94ba76c771db2e09bee945d122ed17ca3a2"),
    ("durable-reminders", "af307512a7cb4e6c92892c816b2f7c42a390dd58"),
    ("holiday-compatibility", "14a5c88b566fc19902302cd6cd8a415dc4841d35"),
    ("dashboard-ordering", "d0d087f4252f2573723e314613aaa341f73991ae"),
    ("session-coordination", "f8ac2360e75473d44658b1a49ee949340f761b0c"),
    ("safe-migrations", "9a6cedf9549ba397adf185d24cb6b15f186111ca"),
]
ROOT = Path(__file__).resolve().parent
SHA = re.compile(r"^[0-9a-f]{40}$")


def git(repo, *args, input=None, env=None, check=True, raw=False):
    result = subprocess.run(
        ["git", "-C", str(repo), *args], input=input, capture_output=True,
        text=True, env=env, timeout=180,
    )
    if check and result.returncode:
        # No credentials are passed as command-line arguments.
        raise RuntimeError(f"git {args[0]} failed: {result.stderr.strip()}")
    return (result.stdout if raw else result.stdout.strip()) if check else result


def get_json(path):
    """Read PUBLIC upstream state. Never treat network/API failure as no PR."""
    request = urllib.request.Request(
        "https://api.github.com" + path,
        headers={"Accept": "application/vnd.github+json",
                 "User-Agent": "whento-pr163-publication",
                 "X-GitHub-Api-Version": "2022-11-28"},
    )
    for attempt in range(3):
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            if error.code not in (429, 500, 502, 503, 504) or attempt == 2:
                raise RuntimeError(f"GitHub GET failed ({error.code}); no publication") from error
        except (urllib.error.URLError, TimeoutError):
            if attempt == 2:
                raise RuntimeError("GitHub unavailable; no publication")
        time.sleep(2 ** attempt)
    raise AssertionError("unreachable")


def read_pulls(api=get_json):
    pulls = []
    for page in range(1, 101):
        query = urllib.parse.urlencode({"state": "all", "base": "main",
                                       "per_page": 100, "page": page})
        batch = api(f"/repos/{UPSTREAM}/pulls?{query}")
        if not isinstance(batch, list):
            raise RuntimeError("Invalid GitHub PR response")
        pulls.extend(batch)
        if len(batch) < 100:
            return pulls
    raise RuntimeError("PR pagination limit exceeded; no publication")


def topic_pulls(pulls, slug):
    refs = {f"codex/pr163-{slug}", f"codex/pr163-submit-{slug}"}
    return [p for p in pulls
            if p.get("head", {}).get("ref") in refs
            and (p.get("head", {}).get("repo") or {}).get("full_name", "").lower() == FORK.lower()
            and p.get("base", {}).get("ref") == "main"
            and (p.get("base", {}).get("repo") or {}).get("full_name", "").lower() == UPSTREAM.lower()]


def select_topic(pulls, exists, api=get_json):
    """Return at most one new topic; never overwrite an existing submission."""
    rows = []
    ready = 0
    prerequisite = None
    for index, (slug, _) in enumerate(TOPICS, 1):
        matches = topic_pulls(pulls, slug)
        merged = [p for p in matches if p.get("merged_at")]
        opened = [p for p in matches if p.get("state") == "open"]
        if merged:
            status = "merged"
        elif opened:
            status = "PR open — not modified"
        elif matches:
            status = "closed without merge — requires your decision"
        elif exists(f"codex/pr163-submit-{slug}"):
            status = "branch exists — not modified"
        elif index <= 2:
            raise RuntimeError("An initial submission branch is missing; restore it deliberately")
        else:
            previous = topic_pulls(pulls, TOPICS[index - 2][0])
            previous_merged = [p for p in previous if p.get("merged_at")]
            if previous_merged and not ready:
                detail = api(f"/repos/{UPSTREAM}/pulls/{previous_merged[0]['number']}")
                if (detail.get("merged") is not True
                        or not topic_pulls([detail], TOPICS[index - 2][0])
                        or not SHA.fullmatch(detail.get("merge_commit_sha") or "")):
                    raise RuntimeError("Prerequisite merge could not be verified")
                ready, prerequisite = index, detail
                status = "ready to prepare and verify"
            else:
                status = "waiting for prerequisite merge"
        rows.append({"number": index, "slug": slug, "status": status})
    return ready, prerequisite, rows


def remote_head(branch):
    output = git(".", "ls-remote", "--heads", FORK_URL, "refs/heads/" + branch)
    if not output:
        return ""
    sha, ref = output.split()
    if not SHA.fullmatch(sha) or ref != "refs/heads/" + branch:
        raise RuntimeError("Unexpected fork ref response")
    return sha


def validate_plan(plan):
    number = plan.get("number")
    if type(number) is not int or not 3 <= number <= 8:
        raise RuntimeError("Invalid publication topic")
    if not SHA.fullmatch(plan.get("upstream") or ""):
        raise RuntimeError("Invalid upstream commit")
    slug, source = TOPICS[number - 1]
    if plan.get("slug") != slug or plan.get("source") != source:
        raise RuntimeError("Frozen source mismatch")
    if plan.get("target") != "codex/pr163-submit-" + slug:
        raise RuntimeError("Invalid destination branch")
    if not SHA.fullmatch(plan.get("prerequisite_merge") or ""):
        raise RuntimeError("Invalid prerequisite merge")
    return number, slug, source


def source_base(number):
    if number <= 2:
        return INITIAL
    if number == 3:
        return REVIEWED_BACKEND_MERGE
    if number == 4:
        return REVIEWED_BOOTSTRAP_FOLLOWUPS
    return TOPICS[number - 2][1]


def source_ref(number):
    """Select the trusted frozen ref, never a ref supplied by a plan artifact."""
    prefix = "codex/pr163-reviewed-20261007-" if number >= 4 else "codex/pr163-"
    return prefix + TOPICS[number - 1][0]


def replay_changelog(base, source, current):
    """Replay frozen insertion-only notes by Unreleased subsection, not union merge.

    This preserves upstream notes and never resurrects deleted text. Unexpected
    modifications/deletions fail closed instead of choosing ours/theirs.
    """
    old, new = base.splitlines(keepends=True), source.splitlines(keepends=True)
    additions = {}
    for operation, start, end, new_start, new_end in difflib.SequenceMatcher(
            None, old, new, autojunk=False).get_opcodes():
        if operation == "equal":
            continue
        if operation != "insert":
            raise RuntimeError("Changelog source is not insertion-only; review required")
        section = None
        unreleased = False
        for line in old[:start]:
            if line.startswith("## "):
                unreleased = line.strip() == "## [Unreleased]"
                section = None
            elif unreleased and line.startswith("### "):
                section = line.strip()
        if not unreleased or section is None:
            raise RuntimeError("Changelog addition is outside an Unreleased subsection")
        additions.setdefault(section, []).extend(new[new_start:new_end])
    lines = current.splitlines(keepends=True)
    try:
        begin = next(i for i, line in enumerate(lines) if line.strip() == "## [Unreleased]")
    except StopIteration as error:
        raise RuntimeError("Upstream has no Unreleased section") from error
    for section, notes in additions.items():
        finish = next((i for i in range(begin + 1, len(lines)) if lines[i].startswith("## ")), len(lines))
        heading = next((i for i in range(begin + 1, finish) if lines[i].strip() == section), None)
        if heading is None:
            lines[finish:finish] = [section + "\n", "\n", *notes, "\n"]
        else:
            insertion = next((i for i in range(heading + 1, finish)
                              if lines[i].startswith("### ")), finish)
            while insertion > heading + 1 and not lines[insertion - 1].strip():
                insertion -= 1
            lines[insertion:insertion] = notes
    return "".join(lines)


def read_blob(repo, commit, path):
    result = subprocess.run(["git", "-C", str(repo), "show", f"{commit}:{path}"],
                            capture_output=True, text=True, check=True)
    return result.stdout


def replay_makefile(base, source, current):
    """Reconcile ONLY additive .PHONY names; three-way merge all real recipes.

    PR 1 and PR 3 both insert a target in one long declaration. Target order has
    no meaning. Keep upstream's declaration, add only this topic's new names,
    and never restore any name upstream deleted. Other conflicts fail closed.
    """
    old, new, live = [text.splitlines(keepends=True) for text in (base, source, current)]
    token_lists = []
    for lines in (old, new, live):
        if not lines or not re.fullmatch(r"\.PHONY: [a-zA-Z0-9_ -]+\n", lines[0]):
            raise RuntimeError("Unexpected Makefile declaration; review required")
        tokens = lines[0].split()[1:]
        if len(set(tokens)) != len(tokens):
            raise RuntimeError("Duplicate .PHONY names; review required")
        token_lists.append(tokens)
    before, after, retained = token_lists
    if [token for token in after if token in before] != before:
        raise RuntimeError("Topic removes or reorders .PHONY names; review required")
    for token in after:
        if token not in before and token not in retained:
            retained.append(token)
    declaration = ".PHONY: " + " ".join(retained) + "\n"
    # Identical first lines remove only the mechanical declaration conflict.
    contents = [declaration + "".join(lines[1:]) for lines in (live, old, new)]
    with tempfile.TemporaryDirectory(prefix="pr163-makefile-") as temporary:
        paths = [Path(temporary) / name for name in ("current", "base", "topic")]
        for path, content in zip(paths, contents):
            path.write_text(content)
        result = subprocess.run(["git", "merge-file", "--stdout", "--diff3", *map(str, paths)],
                                capture_output=True, text=True, timeout=30)
    if result.returncode:
        raise RuntimeError("Makefile recipe conflict; review required")
    return result.stdout


def prepare(repo, plan, directory):
    number, slug, source = validate_plan(plan)
    git(repo, "fetch", "--no-tags", UPSTREAM_URL, "main")
    if git(repo, "rev-parse", "FETCH_HEAD") != plan["upstream"]:
        raise RuntimeError("Upstream moved; wait for the next scheduled run")
    git(repo, "fetch", "--no-tags", FORK_URL, "refs/heads/" + source_ref(number))
    if git(repo, "rev-parse", "FETCH_HEAD") != source:
        raise RuntimeError("Audited topic changed; review and update the frozen source")
    base = source_base(number)
    git(repo, "merge-base", "--is-ancestor", plan["prerequisite_merge"], plan["upstream"])
    git(repo, "merge-base", "--is-ancestor", base, source)
    git(repo, "worktree", "add", "--detach", str(directory), plan["upstream"])
    patch = git(repo, "diff", "--binary", "--no-ext-diff", "--no-textconv", base, source,
                "--", ".", ":(exclude)CHANGELOG.md", ":(exclude)Makefile", raw=True)
    if patch:
        git(directory, "apply", "--3way", "--index", input=patch)
    changelog = Path(directory) / "CHANGELOG.md"
    revised = replay_changelog(read_blob(repo, base, "CHANGELOG.md"),
                               read_blob(repo, source, "CHANGELOG.md"), changelog.read_text())
    changelog.write_text(revised)
    git(directory, "add", "CHANGELOG.md")
    expected = set(git(repo, "diff", "--name-only", base, source).splitlines())
    if "Makefile" in expected:
        makefile = Path(directory) / "Makefile"
        makefile.write_text(replay_makefile(read_blob(repo, base, "Makefile"),
                            read_blob(repo, source, "Makefile"), makefile.read_text()))
        git(directory, "add", "Makefile")
    actual = set(git(directory, "diff", "--cached", "--name-only").splitlines())
    if not actual or not actual <= expected:
        raise RuntimeError("Replay changed files outside the owning topic")
    draft = (ROOT / "bodies" / f"{number:02d}-{slug}.md").read_text()
    title = draft.splitlines()[0].removeprefix("# ")
    git(directory, "diff", "--cached", "--check")
    git(directory, "-c", "user.name=github-actions[bot]", "-c",
        "user.email=41898282+github-actions[bot]@users.noreply.github.com", "commit",
        "-m", title, "-m", f"Replay audited topic {base}..{source} on {plan['upstream']}.\n"
        "Publication is gated by the fork-only verification workflow.")
    plan["candidate"] = git(directory, "rev-parse", "HEAD")
    return plan


def summary(rows, secret_configured):
    lines = ["# PR163 publication status", "", "No upstream PRs are created or merged.", ""]
    if not secret_configured:
        lines += ["Publishing setup required: add the repository-only secret "
                  "`WHENTO_PR163_PUBLISH_TOKEN` (Contents + Workflows write).", ""]
    lines += ["| Topic | Status |", "| --- | --- |"]
    for row in rows:
        lines.append(f"| {row['number']} — {row['slug']} | {row['status']} |")
    for row in rows:
        if row["status"] == "branch exists — not modified":
            lines.append(f"\n[Open PR {row['number']}]({pr_url(row['number'])})\n")
    return "\n".join(lines) + "\n"


def pr_url(number, validation=""):
    slug = TOPICS[number - 1][0]
    draft = (ROOT / "bodies" / f"{number:02d}-{slug}.md").read_text()
    title, body = draft.splitlines()[0].removeprefix("# "), "\n".join(draft.splitlines()[2:]).strip()
    if validation:
        body += f"\n\nAutomated replay verification: {validation}"
    query = urllib.parse.urlencode({"quick_pull": "1", "title": title, "body": body})
    ref = urllib.parse.quote("codex/pr163-submit-" + slug, safe="")
    return f"https://github.com/{UPSTREAM}/compare/main...Akkitto:{ref}?{query}"


def plan_live(output, report):
    pulls = read_pulls()
    ready, prerequisite, rows = select_topic(pulls, remote_head)
    report.write_text(summary(rows, os.environ.get("PUBLISH_TOKEN_CONFIGURED") == "true"))
    payload = {"number": ready, "rows": rows}
    if ready:
        upstream = get_json(f"/repos/{UPSTREAM}/commits/main")["sha"]
        slug, source = TOPICS[ready - 1]
        payload.update(slug=slug, source=source, upstream=upstream,
                       target="codex/pr163-submit-" + slug,
                       prerequisite_merge=prerequisite["merge_commit_sha"])
        validate_plan(payload)
    output.write_text(json.dumps(payload, indent=2) + "\n")
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a") as stream:
            stream.write(f"ready={'true' if ready else 'false'}\n")
    print(f"Next eligible topic: {ready or 'none; waiting for upstream merges'}")


def publish(repo, payload, artifact):
    number, _, source = validate_plan(payload)
    candidate = payload.get("candidate") or ""
    if not SHA.fullmatch(candidate):
        raise RuntimeError("Invalid candidate commit")
    if not os.environ.get("PUBLISH_TOKEN"):
        raise RuntimeError("Set WHENTO_PR163_PUBLISH_TOKEN in fork repository secrets; no branch published")
    # Re-check live state after testing. Existing branches are NEVER overwritten.
    ready, prerequisite, _ = select_topic(read_pulls(), remote_head)
    if ready != number or prerequisite["merge_commit_sha"] != payload["prerequisite_merge"]:
        raise RuntimeError("PR state changed during testing; no branch published")
    if get_json(f"/repos/{UPSTREAM}/commits/main")["sha"] != payload["upstream"]:
        raise RuntimeError("Upstream moved during verification; retry next run")
    if remote_head(source_ref(number)) != source:
        raise RuntimeError("Audited source changed during testing; no branch published")
    git(repo, "fetch", "--no-tags", UPSTREAM_URL, "main")
    if git(repo, "rev-parse", "FETCH_HEAD") != payload["upstream"]:
        raise RuntimeError("Upstream moved after the API check; retry next run")
    git(repo, "fetch", "--no-tags", FORK_URL, "refs/heads/" + source_ref(number))
    if git(repo, "rev-parse", "FETCH_HEAD") != source:
        raise RuntimeError("Audited source moved after the API check; no publication")
    git(repo, "bundle", "verify", str(artifact / "candidate.bundle"))
    git(repo, "fetch", str(artifact / "candidate.bundle"), "HEAD")
    if git(repo, "rev-parse", "FETCH_HEAD") != candidate:
        raise RuntimeError("Bundle does not match the verified candidate")
    if git(repo, "rev-list", "--parents", "-n", "1", candidate).split() != [candidate, payload["upstream"]]:
        raise RuntimeError("Candidate is not a single-topic commit on verified upstream")
    expected = set(git(repo, "diff", "--name-only", source_base(number), source).splitlines())
    actual = set(git(repo, "diff", "--name-only", payload["upstream"], candidate).splitlines())
    if not actual or not actual <= expected:
        raise RuntimeError("Candidate contains unrelated changes")
    git(repo, "diff", "--check", payload["upstream"], candidate)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("GIT_TRACE") or key == "GIT_CURL_VERBOSE":
            del env[key]
    env.update(GIT_ASKPASS=str(ROOT / "askpass.sh"), GIT_TERMINAL_PROMPT="0",
               GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null")
    # Empty lease = target MUST still be absent. No existing ref can be replaced,
    # including a branch created by the owner between the read and this push.
    target = "refs/heads/" + payload["target"]
    git(repo, "-c", "http.extraHeader=", "push", f"--force-with-lease={target}:", FORK_URL,
        f"{candidate}:{target}", env=env)
    run = os.environ.get("GITHUB_RUN_ID", "")
    validation = f"https://github.com/{FORK}/actions/runs/{run}" if run.isdecimal() else ""
    link = pr_url(number, validation)
    print(f"Published PR {number} branch. Open its PR: {link}")
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as stream:
            stream.write(f"\n## Ready\n\n[Open PR {number}]({link})\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("plan", "prepare", "package", "publish"))
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--directory", type=Path)
    parser.add_argument("--summary", type=Path)
    args = parser.parse_args()
    if os.environ.get("GITHUB_REPOSITORY", FORK).lower() != FORK.lower():
        raise RuntimeError("This automation runs only in Akkitto/whento")
    if args.mode == "plan":
        if args.summary is None:
            parser.error("plan requires --summary")
        plan_live(args.plan, args.summary)
        return
    if args.directory is None:
        parser.error("this mode requires --directory")
    payload = json.loads(args.plan.read_text())
    if args.mode == "prepare":
        prepare(args.repo, payload, args.directory)
        args.plan.write_text(json.dumps(payload, indent=2) + "\n")
    elif args.mode == "package":
        validate_plan(payload)
        if git(args.directory, "rev-parse", "HEAD") != payload.get("candidate"):
            raise RuntimeError("Candidate changed during verification")
        git(args.directory, "diff", "--exit-code")
        if git(args.directory, "status", "--porcelain"):
            raise RuntimeError("Verification left source changes")
        bundle = args.plan.parent / "candidate.bundle"
        git(args.directory, "bundle", "create", str(bundle), "HEAD", "--not", payload["upstream"])
    else:
        publish(args.repo, payload, args.directory)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.SubprocessError, ValueError, OSError) as error:
        print(f"::error::{error}", file=sys.stderr)
        sys.exit(1)
