# ci: validate Compose files and order clean-checkout tests

Related to #163; focused PR 1 of 8.

## Summary

- Align Compose PID-limit declarations and validate all three checked-in Compose files in CI.
- Make root tests depend on the generated Swagger package and an embedded-frontend placeholder, including direct `test-root` and parallel Make invocations.
- Preserve upstream toolchain, action pins, dependency versions, and application behavior.

## Verification

All three `docker compose config -q` checks and workflow lint pass. Clean archived checkouts pass serial cloud and parallel selfhosted Make tests using Go 1.27.1; both Go workspace modules are tested. A conflicting PID-limit fixture is rejected.

No database migration. Configuration parsing is tested; this does not claim a production deployment was exercised.
