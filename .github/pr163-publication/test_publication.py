"""Safety tests; optional real-history replay tests never contact a remote."""

import importlib.util
import itertools
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("publication", Path(__file__).with_name("publication.py"))
pub = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pub)


def pull(number, topic, *, merged=False, state="closed", fork=pub.FORK, base=pub.UPSTREAM):
    return {"number": number, "state": state, "merged_at": "today" if merged else None,
            "merged": merged, "merge_commit_sha": "a" * 40,
            "head": {"ref": "codex/pr163-submit-" + pub.TOPICS[topic - 1][0],
                     "repo": {"full_name": fork}},
            "base": {"ref": "main", "repo": {"full_name": base}}}


def plan(number=3):
    slug, source = pub.TOPICS[number - 1]
    return {"number": number, "slug": slug, "source": source, "upstream": "b" * 40,
            "target": "codex/pr163-submit-" + slug, "prerequisite_merge": "a" * 40}


class SelectionTests(unittest.TestCase):
    def exists(self, branch):
        return branch in {"codex/pr163-submit-" + pub.TOPICS[i][0] for i in (0, 1)}

    def test_initial_state_waits(self):
        ready, prerequisite, rows = pub.select_topic([], self.exists)
        self.assertEqual(ready, 0)
        self.assertIsNone(prerequisite)
        self.assertEqual(len(rows), 8)

    def test_pr1_does_not_unlock_pr3(self):
        self.assertEqual(pub.select_topic([pull(1, 1, merged=True)], self.exists)[0], 0)

    def test_pr2_merge_unlocks_only_pr3(self):
        merged = pull(2, 2, merged=True)
        ready, _, rows = pub.select_topic([merged], self.exists, lambda _: merged)
        self.assertEqual(ready, 3)
        self.assertEqual(sum(r["status"].startswith("ready") for r in rows), 1)

    def test_open_pr_is_never_modified(self):
        merged = pull(2, 2, merged=True)
        self.assertEqual(pub.select_topic([merged, pull(3, 3, state="open")], self.exists)[0], 0)

    def test_closed_unmerged_is_never_reopened(self):
        self.assertEqual(pub.select_topic([pull(2, 2, merged=True), pull(3, 3)], self.exists)[0], 0)

    def test_existing_destination_is_never_replaced(self):
        exists = lambda branch: self.exists(branch) or branch.endswith("bootstrap-password")
        self.assertEqual(pub.select_topic([pull(2, 2, merged=True)], exists)[0], 0)

    def test_other_fork_cannot_unlock(self):
        self.assertEqual(pub.select_topic([pull(2, 2, merged=True, fork="someone/whento")], self.exists)[0], 0)

    def test_other_base_cannot_unlock(self):
        self.assertEqual(pub.select_topic([pull(2, 2, merged=True, base=pub.FORK)], self.exists)[0], 0)

    def test_other_branch_cannot_unlock(self):
        merged = pull(2, 2, merged=True)
        merged["head"]["ref"] = "unrelated"
        self.assertEqual(pub.select_topic([merged], self.exists)[0], 0)

    def test_detail_must_confirm_correct_merge_and_repository(self):
        merged = pull(2, 2, merged=True)
        for detail in (pull(2, 2), pull(2, 2, merged=True, base=pub.FORK),
                       pull(2, 2, merged=True, fork="someone/whento")):
            with self.subTest(detail=detail), self.assertRaises(RuntimeError):
                pub.select_topic([merged], self.exists, lambda _: detail)

    def test_missing_initial_branch_stops(self):
        with self.assertRaises(RuntimeError):
            pub.select_topic([], lambda _: False)

    def test_next_topic_after_squash_merge(self):
        merged = [pull(i, i, merged=True) for i in range(2, 6)]
        self.assertEqual(pub.select_topic(merged, self.exists, lambda _: merged[-1])[0], 6)

    def test_all_merged_finishes(self):
        self.assertEqual(pub.select_topic([pull(i, i, merged=True) for i in range(1, 9)], lambda _: False)[0], 0)

    def test_pagination_and_failure_are_not_silently_ignored(self):
        requests = []
        def api(path):
            requests.append(path)
            return [{}] * 100 if len(requests) == 1 else [pull(2, 2, merged=True)]
        self.assertEqual(len(pub.read_pulls(api)), 101)
        self.assertIn("page=2", requests[-1])
        with self.assertRaises(RuntimeError):
            pub.read_pulls(lambda _: {"message": "error"})
        with self.assertRaises(OSError):
            pub.read_pulls(lambda _: (_ for _ in ()).throw(OSError("offline")))


class ReplayTests(unittest.TestCase):
    base = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- Old note.\n\n## [1.0]\n\n### Added\n\n- Released.\n"
    source = base.replace("- Old note.\n", "- Old note.\n- Topic note.\n")

    def test_preserves_current_upstream_notes(self):
        current = self.base.replace("- Old note.\n", "- Upstream new note.\n")
        revised = pub.replay_changelog(self.base, self.source, current)
        self.assertIn("- Upstream new note.", revised)
        self.assertIn("- Topic note.", revised)
        self.assertNotIn("- Old note.", revised)
        self.assertEqual(revised.count("- Released."), 1)

    def test_missing_subsection_created_before_release(self):
        current = self.base.replace("### Added\n\n- Old note.\n\n", "### Fixed\n\n- New fix.\n\n", 1)
        revised = pub.replay_changelog(self.base, self.source, current)
        self.assertLess(revised.index("- Topic note."), revised.index("## [1.0]"))
        self.assertIn("- New fix.", revised)

    def test_multiple_subsections(self):
        old = self.base.replace("## [1.0]", "### Fixed\n\n- Earlier fix.\n\n## [1.0]", 1)
        new = old.replace("- Old note.", "- Old note.\n- Added topic.").replace("- Earlier fix.", "- Earlier fix.\n- Fixed topic.")
        revised = pub.replay_changelog(old, new, old)
        self.assertIn("- Added topic.", revised)
        self.assertIn("- Fixed topic.", revised)

    def test_modifications_fail_closed(self):
        with self.assertRaises(RuntimeError):
            pub.replay_changelog(self.base, self.base.replace("Old note", "Changed note"), self.base)

    def test_deletions_fail_closed(self):
        with self.assertRaises(RuntimeError):
            pub.replay_changelog(self.base, self.base.replace("- Old note.\n", ""), self.base)

    def test_released_additions_fail_closed(self):
        with self.assertRaises(RuntimeError):
            pub.replay_changelog(self.base, self.base + "- Unexpected released note.\n", self.base)

    def test_missing_unreleased_fails_closed(self):
        with self.assertRaises(RuntimeError):
            pub.replay_changelog(self.base, self.source, "# No Unreleased\n")


class BoundaryTests(unittest.TestCase):
    def test_plan_rejects_untrusted_fields(self):
        for field, value in (("number", True), ("number", 2), ("number", 9),
                             ("target", "main"), ("slug", "wrong"),
                             ("source", "c" * 40), ("upstream", "main"),
                             ("prerequisite_merge", "")):
            with self.subTest(field=field, value=value), self.assertRaises(RuntimeError):
                pub.validate_plan(dict(plan(), **{field: value}))

    def test_complete_links_and_summary_table(self):
        rows = [{"number": n, "slug": pub.TOPICS[n - 1][0],
                 "status": "branch exists — not modified"} for n in range(1, 9)]
        report = pub.summary(rows, False)
        self.assertIn("WHENTO_PR163_PUBLISH_TOKEN", report)
        self.assertGreater(report.index("[Open PR 1]"), report.index("| 8 —"))
        for n in range(1, 9):
            self.assertIn("https://github.com/When-To/whento/compare/main...Akkitto:", pub.pr_url(n))

    def test_raw_diff_preserves_whitespace(self):
        with tempfile.TemporaryDirectory() as directory:
            pub.git(directory, "init", "-q")
            pub.git(directory, "config", "user.name", "Test")
            pub.git(directory, "config", "user.email", "test@example.invalid")
            path = Path(directory) / "whitespace.txt"
            path.write_text("  before\n  \n")
            pub.git(directory, "add", ".")
            pub.git(directory, "commit", "-qm", "test: fixture")
            path.write_text("  after\n  \n")
            diff = pub.git(directory, "diff", raw=True)
            self.assertTrue(diff.endswith("   \n"))
            path.write_text("  before\n  \n")
            pub.git(directory, "apply", input=diff)
            self.assertEqual(path.read_text(), "  after\n  \n")

    def test_empty_creation_lease_cannot_replace_existing_branch(self):
        with tempfile.TemporaryDirectory() as temporary:
            repo, remote = Path(temporary) / "repo", Path(temporary) / "remote.git"
            repo.mkdir()
            pub.git(repo, "init", "-q")
            pub.git(repo, "config", "user.name", "Test")
            pub.git(repo, "config", "user.email", "test@example.invalid")
            pub.git(repo, "commit", "--allow-empty", "-qm", "test: first")
            pub.git(temporary, "init", "--bare", "-q", str(remote))
            ref = "refs/heads/codex/pr163-submit-test"
            pub.git(repo, "push", f"--force-with-lease={ref}:", str(remote), f"HEAD:{ref}")
            first = pub.git(repo, "rev-parse", "HEAD")
            pub.git(repo, "commit", "--allow-empty", "-qm", "test: later")
            with self.assertRaises(RuntimeError):
                pub.git(repo, "push", f"--force-with-lease={ref}:", str(remote), f"HEAD:{ref}")
            self.assertEqual(pub.git(temporary, "--git-dir=" + str(remote), "rev-parse", ref), first)

    def test_missing_credential_never_pushes(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(pub, "git") as git:
            with self.assertRaises(RuntimeError):
                pub.publish(Path("."), dict(plan(), candidate="c" * 40), Path("."))
            git.assert_not_called()


class MakefileTests(unittest.TestCase):
    base = ".PHONY: old removed\n\nold:\n\techo old\n"
    source = base.replace("old removed\n", "old removed topic\n") + "\ntopic:\n\techo topic\n"

    def test_only_new_names_added_and_real_recipes_preserved(self):
        current = self.base.replace("old removed\n", "old upstream\n").replace(
            "\nold:\n", "\nupstream:\n\techo upstream\n\nold:\n")
        revised = pub.replay_makefile(self.base, self.source, current)
        self.assertEqual(revised.splitlines()[0], ".PHONY: old upstream topic")
        self.assertIn("\techo topic", revised)
        self.assertIn("\techo upstream", revised)
        self.assertNotIn("removed", revised)

    def test_recipe_conflict_stops(self):
        source = self.source.replace("echo old", "echo topic edit")
        current = self.base.replace("echo old", "echo upstream edit")
        with self.assertRaises(RuntimeError):
            pub.replay_makefile(self.base, source, current)

    def test_declaration_deletions_stop(self):
        with self.assertRaises(RuntimeError):
            pub.replay_makefile(self.base, self.source.replace("old removed", "old"), self.base)

    def test_nonliteral_declarations_stop(self):
        with self.assertRaises(RuntimeError):
            pub.replay_makefile(self.base, self.source, self.base.replace("old removed", "$(TARGETS)"))


class PublisherTests(unittest.TestCase):
    def run_publisher(self, *, moved=False, parent_extra=False, unrelated=False, already_exists=False):
        payload = dict(plan(), candidate="c" * 40)
        fetched = ""
        calls = []
        def command(repo, *args, **kwargs):
            nonlocal fetched
            calls.append((args, kwargs))
            if args[0] == "fetch":
                fetched = ("d" * 40 if moved else payload["upstream"]) if args[2] == pub.UPSTREAM_URL else (
                    payload["source"] if args[2] == pub.FORK_URL else payload["candidate"])
            if args[0] == "rev-parse":
                return fetched
            if args[0] == "rev-list":
                return payload["candidate"] + " " + payload["upstream"] + (" " + "d" * 40 if parent_extra else "")
            if args[0] == "diff" and "--name-only" in args:
                return "unrelated.txt" if unrelated and args[2] == payload["upstream"] else "Makefile"
            return ""
        with patch.dict(os.environ, {"PUBLISH_TOKEN": "synthetic-only", "GIT_TRACE_CURL": "1"}, clear=True), \
                patch.object(pub, "read_pulls", return_value=[]), \
                patch.object(pub, "select_topic", return_value=(0, None, []) if already_exists else
                             (3, {"merge_commit_sha": payload["prerequisite_merge"]}, [])), \
                patch.object(pub, "remote_head", return_value=payload["source"]), \
                patch.object(pub, "get_json", return_value={"sha": payload["upstream"]}), \
                patch.object(pub, "git", side_effect=command), \
                patch("builtins.print"):
            try:
                pub.publish(Path("."), payload, Path("."))
            except RuntimeError:
                if not (moved or parent_extra or unrelated or already_exists):
                    raise
                return calls
        if moved or parent_extra or unrelated or already_exists:
            self.fail("Unsafe candidate should have been rejected")
        return calls

    def test_success_uses_fork_only_empty_lease_and_private_credential(self):
        calls = self.run_publisher()
        pushes = [(args, kwargs) for args, kwargs in calls if "push" in args]
        self.assertEqual(len(pushes), 1)
        args, kwargs = pushes[0]
        self.assertIn(pub.FORK_URL, args)
        self.assertNotIn(pub.UPSTREAM_URL, args)
        self.assertIn("--force-with-lease=refs/heads/codex/pr163-submit-bootstrap-password:", args)
        self.assertNotIn("synthetic-only", " ".join(args))
        self.assertEqual(kwargs["env"]["PUBLISH_TOKEN"], "synthetic-only")
        self.assertNotIn("GIT_TRACE_CURL", kwargs["env"])

    def test_second_upstream_race_check_stops_before_push(self):
        self.assertFalse(any("push" in args for args, _ in self.run_publisher(moved=True)))

    def test_multi_parent_candidate_stops_before_push(self):
        self.assertFalse(any("push" in args for args, _ in self.run_publisher(parent_extra=True)))

    def test_unrelated_candidate_stops_before_push(self):
        self.assertFalse(any("push" in args for args, _ in self.run_publisher(unrelated=True)))

    def test_already_existing_destination_stops_before_fetch_or_push(self):
        self.assertEqual(self.run_publisher(already_exists=True), [])


@unittest.skipUnless(os.environ.get("PR163_REPLAY_REPO"), "Set PR163_REPLAY_REPO for actual frozen Git history")
class RealHistoryTests(unittest.TestCase):
    def clone(self, temporary):
        source_repo = Path(os.environ["PR163_REPLAY_REPO"]).resolve()
        repo = Path(temporary) / "repo"
        subprocess.run(["git", "clone", "--shared", "--no-checkout", str(source_repo), str(repo)],
                       check=True, capture_output=True, text=True)
        pub.git(repo, "config", "user.name", "Test")
        pub.git(repo, "config", "user.email", "test@example.invalid")
        return repo

    def prepare_locally(self, repo, payload, candidate):
        original_git = pub.git
        def local_git(directory, *args, **kwargs):
            if args[0] == "fetch":
                fetched = payload["upstream"] if args[2] == pub.UPSTREAM_URL else payload["source"]
                return original_git(directory, "fetch", "--no-tags", str(repo), fetched)
            return original_git(directory, *args, **kwargs)
        with patch.object(pub, "git", side_effect=local_git):
            return pub.prepare(repo, payload, candidate)

    def test_all_six_topics_after_reviewed_squashed_prerequisites_and_upstream_notes(self):
        for number, extra_note in itertools.product(range(3, 9), (False, True)):
            with self.subTest(topic=number, upstream_note=extra_note), tempfile.TemporaryDirectory() as temporary:
                repo = self.clone(temporary)
                candidate = Path(temporary) / "candidate"
                base = pub.source_base(number)
                # The entire refreshed lineage must preserve the actual reviewed
                # PR 173 merge, including its security fixes and merged CI work.
                pub.git(repo, "merge-base", "--is-ancestor", pub.REVIEWED_BACKEND_MERGE, base)
                pub.git(repo, "merge-base", "--is-ancestor", base, pub.TOPICS[number - 1][1])
                # Model squash merging: the source parent ID is NOT an ancestor.
                tree = pub.git(repo, "rev-parse", base + "^{tree}")
                prerequisite = pub.git(repo, "commit-tree", tree, "-p", pub.INITIAL,
                                       input="test: squashed reviewed prerequisite\n")
                pub.git(repo, "checkout", "--detach", prerequisite)
                if extra_note:
                    changelog = repo / "CHANGELOG.md"
                    changelog.write_text(changelog.read_text().replace("## [Unreleased]\n",
                                         "## [Unreleased]\n\n### Publication test\n\n- Upstream-only note.\n", 1))
                    pub.git(repo, "add", "CHANGELOG.md")
                    pub.git(repo, "commit", "-qm", "test: upstream-only change")
                main = pub.git(repo, "rev-parse", "HEAD")
                payload = dict(plan(number), upstream=main, prerequisite_merge=prerequisite)
                self.prepare_locally(repo, payload, candidate)
                self.assertEqual(pub.git(candidate, "rev-parse", "HEAD^"), main)
                if extra_note:
                    self.assertIn("- Upstream-only note.", (candidate / "CHANGELOG.md").read_text())
                expected = set(pub.git(repo, "diff", "--name-only", base, payload["source"]).splitlines())
                actual = set(pub.git(candidate, "diff", "--name-only", main, "HEAD").splitlines())
                self.assertEqual(actual, expected)
                self.assertEqual(pub.git(candidate, "diff", payload["source"], "HEAD", "--",
                                         ".", ":(exclude)CHANGELOG.md", ":(exclude)Makefile"), "")
                expected_makefile = pub.read_blob(repo, payload["source"], "Makefile").splitlines()
                actual_makefile = (candidate / "Makefile").read_text().splitlines()
                self.assertEqual(set(actual_makefile[0].split()), set(expected_makefile[0].split()))
                self.assertEqual(actual_makefile[1:], expected_makefile[1:])
                pub.git(candidate, "bundle", "create", str(Path(temporary) / "candidate.bundle"),
                        "HEAD", "--not", main)
                pub.git(repo, "bundle", "verify", str(Path(temporary) / "candidate.bundle"))

    def test_bootstrap_replays_on_actual_reviewed_merge_without_reverting_security(self):
        with tempfile.TemporaryDirectory() as temporary:
            repo = self.clone(temporary)
            candidate = Path(temporary) / "candidate"
            main = pub.REVIEWED_BACKEND_MERGE
            self.assertEqual(pub.source_base(3), main)
            self.prepare_locally(repo, dict(plan(), upstream=main, prerequisite_merge=main), candidate)
            for path in (
                "internal/auth/sessionlock/sessionlock.go",
                "internal/auth/sessionlock/sessionlock_test.go",
                "internal/auth/repository/credential_transition_db_test.go",
                "internal/auth/handlers/password_reset_handler.go",
                "internal/auth/handlers/password_reset_handler_test.go",
                "internal/auth/service/magic_link_service.go",
                "internal/mfa/repository/mfa_repository.go",
                "internal/mfa/service/mfa_service.go",
                "internal/calendar/repository/calendar_dates_db_test.go",
                "frontend/src/views/MagicLinkVerify.vue",
                "frontend/e2e/mailbox-auth.spec.ts",
            ):
                with self.subTest(path=path):
                    self.assertEqual(pub.read_blob(candidate, "HEAD", path),
                                     pub.read_blob(repo, main, path))

    def test_original_bootstrap_patch_conflicts_on_actual_reviewed_merge(self):
        # Keep a negative regression for the exact failure from run 37374772405.
        old_base = "963b270ecc0559ae93909f3b2a3c3d4ed35c7cee"
        old_source = "2cfefb31fde89746c46ee09b9fff798adabf7b4c"
        with tempfile.TemporaryDirectory() as temporary:
            repo = self.clone(temporary)
            pub.git(repo, "checkout", "--detach", pub.REVIEWED_BACKEND_MERGE)
            old_patch = pub.git(repo, "diff", "--binary", "--no-ext-diff", "--no-textconv",
                                old_base, old_source, "--", ".", ":(exclude)CHANGELOG.md",
                                ":(exclude)Makefile", raw=True)
            result = pub.git(repo, "apply", "--3way", "--index", input=old_patch, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(set(pub.git(repo, "diff", "--name-only", "--diff-filter=U").splitlines()), {
                "cmd/wire.go", "internal/auth/service/auth_service.go",
                "internal/mfa/handlers/mfa_handler_test.go", "pkg/httputil/response.go",
            })

if __name__ == "__main__":
    unittest.main()
