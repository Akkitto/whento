# fix(web): coordinate sessions and fence replaced accounts across tabs

Related to #163; focused PR 7 of 8. Submit after dashboard ordering is merged.

## Summary

- Coordinate credential issuance, refresh and logout under the cookie-operation lock; install successful credentials before releasing it.
- Separate same-family token rotation from account replacement. Fence superseded families, including delayed tokens in cold tabs and storage-only coordination without BroadcastChannel.
- Propagate replacement/logout, hydrate the active account through the HTTP-only cookie, and clear/reload owner-scoped state once without wiping public cached data.
- Keep access/refresh JWTs out of browser storage; shared metadata contains only non-secret family/coordination information.
- Refresh only when due, including rearming callbacks that fire just before the due boundary.
- Add an actual short-TTL backend CI project and mandatory family-scoped PostgreSQL row assertions; successful rotations may retain legitimate ancestors.
- Isolate browser artifacts and unit-test message queues so concurrent/shuffled runs do not contaminate one another.

## Verification

Own-branch frontend checks and all 946 unit tests pass. The final stack passes all four shuffled seeds plus coverage, all 27 real-backend browser cases (including production-origin two-minute rotation), and all 72 desktop/mobile harness cases. Go 1.27.1 race tests pass for both workspace modules and both build variants.

No schema migration beyond the prerequisite stack. Without Web Locks, fallback coordination cannot guarantee cross-tab HTTP Set-Cookie ordering; this limitation is retained explicitly. Logout may redirect a protected route; ordinary rotation/account replacement does not use reload loops.
