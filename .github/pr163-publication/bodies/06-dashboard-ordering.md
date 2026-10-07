# feat(dashboard): add saved pins, ordering and keyboard navigation

Related to #163; focused PR 6 of 8. Submit after holiday compatibility is merged.

## Summary

- Save pins, sorting and custom calendar/group ordering per authenticated user.
- Add keyboard reordering, stable boundary focus and screen-reader announcements.
- Prune obsolete preferences only after an authoritative successful load, not a transient request failure.
- Supply an isolated dashboard preview that mocks quota reads as well as calendar/feed data and rejects unexpected network requests.

## Verification

The publication gate reruns frontend type/lint/format checks, coverage and four shuffle seeds, dashboard keyboard and desktop/mobile browser tests, real-backend tests, both Go module/build-variant race matrices and deployment builds. The successful verification run is linked below. The isolated preview rejects unexpected network requests; keyboard tests cover reordering, boundary focus and announcements.

No schema migration. Preferences are browser-local `localStorage` state, not server-synchronized settings.
