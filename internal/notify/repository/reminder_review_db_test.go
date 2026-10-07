// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package repository_test

import (
	"testing"
	"time"

	"github.com/whento/whento/internal/notify/repository"
	"github.com/whento/whento/internal/testutil/dbtest"
)

func TestReminderRenewalCannotShortenLeaseOrAllowSecondReplica(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	repo := repository.NewReminderJobRepository(pool)
	job := newReminderJob(t, pool)
	now := time.Date(2027, 4, 9, 12, 0, 0, 0, time.UTC)
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimDue(ctx, "replica-a", now, 10*time.Minute, 50)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v, %v", claimed, err)
	}
	a := claimed[0]
	// Even an accidental short renewal must preserve the longer live lease.
	if err := repo.RenewLease(ctx, a.ID, a.ClaimToken, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	other, err := repo.ClaimDue(ctx, "replica-b", now.Add(61*time.Second), 10*time.Minute, 50)
	if err != nil || len(other) != 0 {
		t.Fatalf("replica B stole renewed fanout: %v, %v", other, err)
	}
	if err := repo.MarkSent(ctx, a.ID, a.ClaimToken); err != nil {
		t.Fatal(err)
	}
}

func TestReminderMarkSentClearsPreviousError(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	repo := repository.NewReminderJobRepository(pool)
	job := newReminderJob(t, pool)
	now := time.Date(2027, 4, 9, 12, 0, 0, 0, time.UTC)
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimDue(ctx, "worker", now, 10*time.Minute, 50)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v, %v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET last_error = 'email_delivery_failed' WHERE id = $1`, claimed[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkSent(ctx, claimed[0].ID, claimed[0].ClaimToken); err != nil {
		t.Fatal(err)
	}
	var cleared bool
	if err := pool.QueryRow(ctx, `SELECT last_error IS NULL FROM reminder_jobs WHERE id = $1`, claimed[0].ID).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("successful delivery kept previous error")
	}
}

func TestReminderMissedRecordIsIdempotentAndDoesNotStealPendingJob(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	repo := repository.NewReminderJobRepository(pool)
	job := newReminderJob(t, pool)
	for attempt := range 2 {
		inserted, err := repo.RecordMissed(ctx, job)
		if err != nil || inserted != (attempt == 0) {
			t.Fatalf("record missed #%d: %v, %v", attempt, inserted, err)
		}
	}
	var status, reason string
	if err := pool.QueryRow(ctx, `SELECT status, last_error FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "canceled" || reason != "catch_up_window_missed" {
		t.Fatalf("missed state: %s, %s", status, reason)
	}
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	inserted, err := repo.RecordMissed(ctx, job)
	if err != nil || inserted {
		t.Fatalf("recording missed overwrote pending job: %v, %v", inserted, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("pending job changed to %s", status)
	}
}

func TestReminderCleanupIsBoundedAndPreservesFutureEventsAndClaims(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	repo := repository.NewReminderJobRepository(pool)
	before := time.Now().UTC().Add(-30 * 24 * time.Hour)
	for _, status := range []string{"pending", "sent", "failed", "canceled"} {
		job := newReminderJob(t, pool)
		job.EventDate = before.Add(-48 * time.Hour)
		if err := repo.Enqueue(ctx, job); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET status = $2, updated_at = $3 WHERE calendar_id = $1`, job.CalendarID, status, before.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	future := newReminderJob(t, pool)
	future.EventDate = time.Now().UTC().Add(7 * 24 * time.Hour)
	if err := repo.Enqueue(ctx, future); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET status = 'sent', updated_at = $2 WHERE calendar_id = $1`, future.CalendarID, before.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	live := newReminderJob(t, pool)
	live.EventDate = before.Add(-48 * time.Hour)
	if err := repo.Enqueue(ctx, live); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET updated_at = $2, lease_until = now() + interval '10 minutes', claim_token = gen_random_uuid() WHERE calendar_id = $1`, live.CalendarID, before.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{2, 1000} {
		deleted, err := repo.Cleanup(ctx, before, limit)
		if err != nil || deleted != 2 {
			t.Fatalf("cleanup limit=%d deleted=%d: %v", limit, deleted, err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reminder_jobs WHERE calendar_id IN ($1, $2)`, future.CalendarID, live.CalendarID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("cleanup removed future-event tombstone or live claim")
	}
}

func TestReminderMigrationRejectsInvalidDeliveryState(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	repo := repository.NewReminderJobRepository(pool)
	job := newReminderJob(t, pool)
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []string{"status = 'bogus'", "channel = 'bogus'", "recipient_type = 'bogus'", "attempt = -1", "max_attempts = 0"} {
		if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET `+assignment+` WHERE calendar_id = $1`, job.CalendarID); err == nil {
			t.Fatalf("database accepted %s", assignment)
		}
	}
}

func TestReminderRecipientDedupOutlivesHourAndRetentionProtectsFutureDate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	repo := repository.NewNotificationLogRepository(pool)
	calendar := newCalendar(t, pool)
	date := time.Now().UTC().Add(7 * 24 * time.Hour)
	for _, event := range []string{"reminder", "threshold_reached"} {
		if err := repo.LogNotification(ctx, calendar.ID, date, event, "owner", calendar.OwnerID, "email"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_log SET sent_at = now() - interval '2 hours' WHERE calendar_id = $1`, calendar.ID); err != nil {
		t.Fatal(err)
	}
	reminder, err := repo.WasReminderSent(ctx, calendar.ID, date, calendar.OwnerID, "email")
	if err != nil || !reminder {
		t.Fatalf("aged reminder completion lost: %v, %v", reminder, err)
	}
	threshold, err := repo.WasNotificationSentRecently(ctx, calendar.ID, date, "threshold_reached", calendar.OwnerID, "email")
	if err != nil || threshold {
		t.Fatalf("threshold one-hour policy changed: %v, %v", threshold, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_log SET sent_at = now() - interval '40 days' WHERE calendar_id = $1`, calendar.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.CleanupOldLogs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CleanupReminderLogs(ctx, time.Now().Add(-30*24*time.Hour), 1000); err != nil {
		t.Fatal(err)
	}
	reminder, err = repo.WasReminderSent(ctx, calendar.ID, date, calendar.OwnerID, "email")
	if err != nil || !reminder {
		t.Fatalf("cleanup lost future-event completion: %v, %v", reminder, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_log SET date = current_date - 40 WHERE calendar_id = $1`, calendar.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := repo.CleanupReminderLogs(ctx, time.Now().Add(-30*24*time.Hour), 1)
	if err != nil || deleted != 1 {
		t.Fatalf("old reminder completion was not cleaned: %d, %v", deleted, err)
	}
}
