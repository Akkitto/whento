# fix(availability): preserve holiday compatibility with offline coverage

Related to #163; focused PR 5 of 8. Submit after durable reminders is merged.

## Summary

- Provide a bundled 44-country offline holiday dataset with bounded/cached Nager fallback outside bundled coverage.
- Preserve `ignoreHolidays` behavior and fail open for unknown country/year data or provider failure.
- Expose supported-country/coverage metadata and holiday API routes without replacing upstream migration history.
- Cover observed public holidays independently of ordinary weekday restrictions; both July 3 and July 4 are checked with holiday enforcement on/off, plus an ordinary allowed Friday.

## Verification

The publication gate reruns both modules/build variants with database-backed Go race tests, frontend static checks, coverage and four shuffle seeds, browser suites and deployment builds on the actual submission base. The successful verification run is linked below. Tests cover the cold-process country-copy race and observed-day positive/negative controls; route and limiter contracts include the new endpoints.

No schema migration. Bundled coverage is not universal; unknown coverage is disclosed rather than silently blocking dates. The client holiday library is lazy-loaded, but its approximately 1.42 MB minified vendor chunk remains a disclosed size cost, not a claimed tiny bundle.
