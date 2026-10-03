-- WhenTo - Collaborative event calendar for self-hosted environments
-- Copyright (C) 2025 WhenTo Contributors
-- SPDX-License-Identifier: BSL-1.1

-- Reminder delivery jobs.
--
-- A row is one scheduled delivery of one reminder (channel x recipient class x
-- event date). It exists so a reminder can survive a redeploy mid-window, be
-- claimed by exactly one delivery at a time, be retried with backoff, and be
-- held idempotently: the unique constraint is the delivery key.
--
-- The claim fence is a per-acquisition UUID (claim_token) plus a lease
-- (lease_until), not an instance id: a single instance can claim the same job
-- twice across a lease expiry, and only a token distinguishes the worker that
-- actually owns the current delivery from one that lost the claim. Every
-- worker side-effect (MarkSent, MarkFailed, per-job cancel, lease renewal)
-- therefore re-checks both the 'pending' status and the token it was handed by
-- ClaimDue. A row that is 'sent' can never be turned into another state, which
-- is what lets a late worker's failure report be a recognizable no-op instead
-- of a resurrected reminder.
--
-- canceled_at and sent_at are kept for operators tracing what happened to a
-- delivery; last_error carries the reason a retried job is where it is.
CREATE TABLE reminder_jobs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    calendar_id    UUID NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    event_date     DATE NOT NULL,
    recipient_type TEXT NOT NULL, -- 'owner' or 'participants'
    channel        TEXT NOT NULL, -- 'email', 'discord', 'slack', 'telegram'
    scheduled_at   TIMESTAMPTZ NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'sent', 'failed', 'canceled'
    attempt        INTEGER NOT NULL DEFAULT 0,
    max_attempts   INTEGER NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    claim_token    UUID,
    lease_until    TIMESTAMPTZ,
    locked_by      TEXT,
    locked_at      TIMESTAMPTZ,
    canceled_at    TIMESTAMPTZ,
    sent_at        TIMESTAMPTZ,
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reminder_jobs_delivery_key UNIQUE (calendar_id, event_date, recipient_type, channel)
);

CREATE INDEX reminder_jobs_due_idx ON reminder_jobs (status, next_attempt_at) WHERE status = 'pending';
