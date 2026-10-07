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
-- claim_token and lease_until fence worker writes; see ReminderJobRepository.
-- last_error contains only bounded error categories, never provider errors.
CREATE TABLE reminder_jobs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    calendar_id    UUID NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    event_date     DATE NOT NULL,
    recipient_type TEXT NOT NULL CHECK (recipient_type IN ('owner', 'participants')),
    channel        TEXT NOT NULL CHECK (channel IN ('email', 'discord', 'slack', 'telegram')),
    scheduled_at   TIMESTAMPTZ NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'canceled')),
    attempt        INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts   INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
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

CREATE INDEX reminder_jobs_due_idx ON reminder_jobs (next_attempt_at) WHERE status = 'pending';

CREATE INDEX calendars_reminder_scan_idx ON calendars (id)
    WHERE notify_config->'enabled' = 'true'::jsonb
      AND notify_config->'reminders'->'enabled' = 'true'::jsonb;
