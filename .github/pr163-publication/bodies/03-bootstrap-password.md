# feat(auth)!: require operator bootstrap and enforce password bounds

Related to #163; focused PR 3 of 8. Submit after backend hardening is merged.

## Summary

- Separate first-administrator creation from public registration using operator-authorized bootstrap in both builds.
- Persist and transactionally lock the first-user marker; deleting all users does not reopen bootstrap.
- Backfill existing installations, expose setup status, and add the bootstrap UI and matching API contracts.
- Support configured/generated operator keys and `BOOTSTRAP_KEY_FILE`; do not expose the key through public setup status.
- Enforce a 12-code-point password minimum and bcrypt's 72-byte maximum consistently on creation/change/reset paths.
- Seed fresh real-backend browser fixtures through status/bootstrap, with an explicit disposable test key; fail clearly on missing setup authorization.

## Migrations and compatibility

Additive common migration 019 `app_state`. Existing-user installations are marked initialized during upgrade. Operator bootstrap is a deliberate breaking change for unattended first-user registration. Key minimums are 16 characters in development and 32 in production; these are separate from the password limits.

## Verification

Own-branch Go 1.27.1 race tests pass for both modules and both builds. Frontend checks and all 750 unit tests pass, including bootstrap fixture branches. Tests cover bootstrap races, registration gating, existing-install backfill, state-read failures, Unicode/password byte limits, and client setup routing. The final stack's fresh-instance backend browser setup also passes.
