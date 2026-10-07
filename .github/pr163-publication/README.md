# Fork-only PR163 publication

Installed only on `Akkitto/whento` default `main`. This is fork infrastructure,
not an upstream change and not a ninth PR in the series.

## One-time activation

1. In the fork's **Actions** tab, enable Actions if GitHub has disabled them on
   the fork. Keep unrelated inherited workflows disabled if they were disabled;
   in particular, enabling all inherited schedules could activate image cleanup.
   The installed fork-main copy additionally guards both inherited cleanup jobs
   as upstream-only, so this setup cannot delete your fork's container images.
2. Create a fine-grained personal access token restricted to **Akkitto/whento
   only**, with repository **Contents: Read and write** and **Workflows: Read and
   write**. Metadata read is implicit. No upstream access or pull-request write
   permission is needed. Choose an expiry that covers the PR series.
3. Add it under the fork's **Settings → Secrets and variables → Actions** as
   `WHENTO_PR163_PUBLISH_TOKEN`. Never paste the value in chat, a commit, or a PR.
4. Enable **PR163 focused branch publication** and use **Run workflow** on `main`
   once. Read its summary to confirm the plan and configuration. Configure GitHub
   Actions failure notifications so a genuine conflict or expired token is seen.

The built-in `GITHUB_TOKEN` cannot authorize creating/modifying workflow files;
some frozen topics legitimately change `.github/workflows/ci.yml`. The separate,
fork-scoped token is therefore required. It appears only in the final write job,
which never executes candidate application code.

## What happens

- The first two main-based branches already exist; open their PRs using the
  status summary links. Never open duplicates for their source copies.
- At minutes 17 and 47 each hour, read actual upstream PR merge state, including
  squash merges. GitHub can delay scheduled runs; this is not a timing guarantee.
- A publication-helper/workflow update pushed to fork `main` also starts the same
  `plan → verify → publish` path immediately. This removes the need to manually
  start a run after reviewed source-pin repairs. Application CI skips only these
  publication-only main pushes; all application-changing pushes, PR checks, and
  security scans remain enabled. Do not use `[skip ci]` on a refresh intended to
  start this push-triggered publication. Manual and scheduled runs remain available.
  A helper update supersedes/cancels an older publication run, so obsolete stalled
  setup cannot hold the queue. Manual/scheduled polling still queues without
  cancelling active work. Cancellation cannot overwrite an existing branch; a
  subsequent plan discovers any submission already created before cancellation.
- For topic 3–8, wait for its immediately preceding topic to merge into upstream
  `main`. PR 1 is independent. Replay **only that topic's owning patch**, preserving
  upstream notes in the Unreleased Changelog. Produce one Conventional Commit on
  the verified current upstream main, not a cumulative PR.
- Reconcile only the known additive `.PHONY` target-name declaration overlap
  between CI/Compose and bootstrap. Never restore upstream-deleted target names;
  actual Makefile recipe conflicts fail closed like other code conflicts.
- Verify both Go workspace modules/builds with database-backed race tests,
  coverage floors, Go formatting/lint and builds; frontend API types, formatting, lint, type
  checks, coverage, four shuffle seeds, both builds; both browser suites against
  disposable services, including the short-TTL session server when applicable;
  the migration acceptance suite when present; all three Docker build paths and
  Compose parsing and bootstrap-key forwarding after the independent CI/Compose
  and bootstrap PRs are present.
- Repeat merge/ref checks after testing. Create the missing
  `codex/pr163-submit-<topic>` branch only. An empty creation lease makes even an
  intervening owner-created branch impossible to overwrite.
- The publish job summary provides the ready-to-open PR link and verification
  run. You review the form and click **Create pull request**. Nothing creates,
  closes, merges, comments on, or edits an upstream PR for you.

## Fail-closed conditions and limits

API/network errors, closed-unmerged PRs, changed frozen source tips, patch
conflicts, missing prerequisites, missing credentials, changed upstream main,
failing tests, unrelated candidate files, and pre-existing destinations never
authorize an overwrite or a publication. Re-run after transient failures; real
conflicts require an explicit code review, not automatic ours/theirs resolution.
An already published/open PR is intentionally not rebased by this workflow.
If upstream requires changes to that PR, they remain a separately reviewed task.

The scheduler leaves audited source branches untouched. After PR 173's reviewed
merge, topics 3–8 were explicitly refreshed and their frozen IDs updated together;
topic 3 now uses the accepted merge `f33ec7d` as its patch base. Original source
history is preserved by the `codex/pr163-before-refresh-20261005-safe-migrations`
archive branch; do not open a PR for that cumulative archive.

After PR 180 merged, topics 4–8 were reviewed again on accepted upstream
`a1e698d` (including #183/#184 and dependency updates #181/#182). Their frozen
sources now use separate `codex/pr163-reviewed-20261007-<topic>` refs. Original
source refs and all previously published submission branches remain untouched.
Topic 4's owning patch starts at that accepted upstream commit; topics 5–8 start
at the preceding refreshed source. The session topic resolves the bootstrap
interceptor overlap while retaining the merged wrong-key regression tests.
Full browser verification also exposed upstream's shared Redis breaker treating
caller cancellation as an outage. The reviewed session fix isolates caller errors
while retaining fail-closed transport handling, and the rotation assertion now
requires zero additional identity reads during rotation itself. Topics 7–8 use
`codex/pr163-reviewed-20261007b-<topic>` snapshots; the earlier reviewed snapshots
are retained too.
PR185's later review removed the no-op SMTP config adapter. The topic-7 snapshot
also carried an obsolete comment-only correction beside that adapter call, which
conflicted with PR185's replacement during replay. Topics 7–8 now use separate
`codex/pr163-reviewed-20261007c-<topic>` snapshots: that unrelated topic-7 correction
is left to PR185, and topic 8 is anchored on the revised topic-7 source. The owning
account-coordination and migration deltas are otherwise unchanged. All older
snapshots remain available; the workflow still refuses to overwrite submissions.
These reviewed source refs are snapshots, **not branches to open PRs for**.
Only `codex/pr163-submit-<topic>` branches get the ready-to-open links.

Run `37668832226` stopped correctly when upstream merged #183/#184 between
planning and preparation. The current-main equality guard is deliberate: a new
run plans against the new main rather than publishing an untested or stale base.
Another upstream change during verification likewise requires a fresh run.
Tool setup skips package operations when the hosted tools already exist; needed
downloads use bounded retries/timeouts and the setup step has a ten-minute limit.
The isolated runner also replaces only the known stalled Azure Ubuntu mirror
with Ubuntu's official HTTPS archive. APT retry/time limits cover subsequent
Playwright dependency setup as well; signing keys and package verification stay
unchanged. The setup refuses operator machines and upstream runners.

Submission branches are created one at a time as
prerequisites merge. No force-update of an existing branch is possible. Disable
this workflow and revoke its token after all eight PRs merge. GitHub can disable
inactive repository schedules after 60 days; re-enable if the series takes that
long. Do not interpret installing workflow code as proof that GitHub has enabled
it or that a token is configured.

## Local verification

```sh
python3 -m unittest discover -s .github/pr163-publication -v
PR163_REPLAY_REPO="$PWD" python3 -m unittest discover -s .github/pr163-publication -v
actionlint .github/workflows/pr163-publication.yml
shellcheck .github/pr163-publication/askpass.sh .github/pr163-publication/verify.sh
```

The optional replay tests need all refreshed and archived topic objects locally.
They exercise each remaining prerequisite being squash-merged, both with and
without an extra upstream Changelog note, and check every candidate's parent,
owning file list, audited source contents (ignoring `.PHONY` name order only),
and bundle. They also replay bootstrap directly on the actual reviewed PR 173
merge and verify its security files are preserved, and reproduce the original
four conflicts as a negative regression. Fixtures use private temporary
repositories and never push remotely.
They additionally reproduce the upstream-moved stop and original session
interceptor conflict, and verify preservation of accepted bootstrap files,
request-ID logging, manual recovery transactions, and dependency updates in
every remaining source. The publisher tests check the new reviewed source ref
without changing the normal creation-only submission destination.
The real application gate deliberately refuses to run outside GitHub Actions to
avoid mistaking an operator's database for disposable test infrastructure.
