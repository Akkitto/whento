# fix(auth): harden partial updates and credential transitions

Related to #163; focused PR 2 of 8. Independent of PR 1; based on upstream main.

## Summary

- Apply calendar settings PATCH updates atomically, preserving omitted fields. Keep the last administrator protected under concurrent user changes.
- Add refresh-family tracking and security-generation fences so rotation, password/security changes, and stale credential operations have explicit transactional boundaries.
- Require a confirmed, origin-checked POST for magic-link verification. Legacy GET verification is read-only and returns 405.
- Enforce MFA for mailbox login/password-reset credential issuance and persist single-use pending-MFA nonce state.
- Install client sessions only after successful credential operations; failed login/reset attempts preserve an existing valid session.
- Update route and rate-limiter contracts on this owning branch.

## Migrations

Additive common migrations: 016 `refresh_token_family`, 017 `security_generation`, 018 `mfa_pending_nonce`. Migration 015 already exists upstream and is not replaced. The family index is non-unique: legitimate rotated ancestors/successors can coexist.

## Verification

Own-branch Go 1.27.1 race tests pass for both workspace modules and both build variants against migrated disposable PostgreSQL databases. Frontend type-check, lint, formatting and all 695 unit tests pass. Coverage includes concurrent administrator changes, partial-update rollback, refresh-family transitions, stale security operations, mailbox/MFA flows, and route contracts.

Security behavior changes: old magic-link GET clients must use the explicit POST flow. MFA enforcement does not retroactively invalidate every preexisting authenticated session merely because MFA is enabled.
