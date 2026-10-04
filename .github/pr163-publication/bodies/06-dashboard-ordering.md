# feat(dashboard): add saved pins, ordering and keyboard navigation

Related to #163; focused PR 6 of 8. Submit after holiday compatibility is merged.

## Summary

- Save pins, sorting and custom calendar/group ordering per authenticated user.
- Add keyboard reordering, stable boundary focus and screen-reader announcements.
- Prune obsolete preferences only after an authoritative successful load, not a transient request failure.
- Supply an isolated dashboard preview that mocks quota reads as well as calendar/feed data and rejects unexpected network requests.

## Verification

Own-branch Go 1.27.1 race tests pass for both modules/builds. Frontend type-check, lint, formatting and all 838 unit tests pass. All 10 dashboard keyboard browser cases pass; the final 72-case desktop/mobile harness also passes with a live backend running, proving preview isolation.

No schema migration. Preferences are browser-local `localStorage` state, not server-synchronized settings.
