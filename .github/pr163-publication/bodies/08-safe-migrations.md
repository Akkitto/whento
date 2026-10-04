# build(migrate): make migration tooling single-target and non-destructive by default

Related to #163; focused PR 8 of 8. Submit after session coordination is merged.

## Summary

- Resolve one canonical PostgreSQL URL, preserving environment precedence, UTF-8 credential encoding, valid literal dotenv syntax and credential-redacted diagnostics.
- Assemble common plus one variant in invocation-owned scratch space. Reject invalid build types and non-empty output directories without deleting existing data.
- Make initialization non-destructive; require exact connected-database confirmation for reset. Stop after a failed down phase and distinguish uninitialized status from real errors.
- Create migration pairs above the numeric maximum of the entire inventory, under a repository-local lock, without overwriting files or symlinks.
- Install the missing pinned CLI without requiring a database or existing CLI, using verified private download staging and exclusive publication.
- Test fresh and populated upstream-version-15 upgrades in four separate databases, verifying actual variant schemas and sentinel preservation.

## Verification

All 48 acceptance checks pass, including real CLI creation and four real PostgreSQL fresh/upgrade cases. The real v4.19.1 installer download passes its pinned SHA256 check. Shellcheck, workflow lint, both Go 1.27.1 module/build-variant race matrices, frontend checks and all 946 unit tests pass.

No new application migration in this tooling PR. `reset` requires confirmation; `down [N]` is an explicit destructive rollback and does not prompt. Real acceptance requires a disposable server with CREATE DATABASE permission and drops only its own random databases. The installer currently targets Linux AMD64, matching the documented CI binary.
