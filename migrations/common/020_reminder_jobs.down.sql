-- WhenTo - Collaborative event calendar for self-hosted environments
-- Copyright (C) 2025 WhenTo Contributors
-- SPDX-License-Identifier: BSL-1.1

DROP INDEX IF EXISTS calendars_reminder_scan_idx;
DROP TABLE IF EXISTS reminder_jobs;
