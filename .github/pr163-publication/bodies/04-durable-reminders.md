# feat(notify): add durable reminder jobs and preserve owner consent

Related to #163; focused PR 4 of 8. Submit after operator bootstrap is merged.

## Summary

- Add SQL-backed reminder jobs with due-time rearming, restart recovery, retry/backoff and leased claims.
- Add the scheduler-owned calendar scan, with database coverage for configuration filtering, ordering and complete row mapping.
- Fence queue-state updates by claim identity so an expired worker cannot complete a newer worker's claim.
- Use calendar-local dates/timezones, bounded catch-up, and distinct handling of transient query failures versus definitive cancellation.
- Recheck owner consent, SMTP capability and participant verification at delivery.
- Preserve a saved email preference through SMTP outages, failed probes and recovery; only an explicit owner edit changes that preference.
- Add delivery and operational release notes without an exactly-once claim.

## Migration and delivery semantics

Additive common migration 020 `reminder_jobs`. External email delivery is **at least once**: a crash after SMTP acceptance but before recording completion can cause a retry. Claim fencing protects queue state, not an external provider's side effects.

## Verification

The publication gate requires database-backed Go race tests for both modules and build variants, frontend type/lint/format/coverage checks and four shuffle seeds, both browser suites, Compose/bootstrap contracts and all three Docker builds. Successful publication runs record the verification evidence. Backend tests exercise claims, stale-worker fencing, concurrent schedulers, retries, cancellation/consent and date transitions; frontend tests cover SMTP outage/recovery and explicit disable.
