// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotificationLogRepository handles notification log database operations
type NotificationLogRepository struct {
	pool *pgxpool.Pool
}

// NewNotificationLogRepository creates a new notification log repository
func NewNotificationLogRepository(pool *pgxpool.Pool) *NotificationLogRepository {
	return &NotificationLogRepository{pool: pool}
}

// WasReminderSent checks durable recipient completion for this event. Unlike
// threshold transitions, retrying a reminder must never age a successful
// recipient out of deduplication while the event is still upcoming.
func (r *NotificationLogRepository) WasReminderSent(ctx context.Context, calendarID uuid.UUID, date time.Time, recipientID uuid.UUID, channel string) (bool, error) {
	var sent bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM notification_log WHERE calendar_id = $1 AND date = $2
		AND event_type = 'reminder' AND recipient_id = $3 AND channel = $4
	)`, calendarID, date, recipientID, channel).Scan(&sent)
	return sent, err
}

// CleanupReminderLogs retains completion until thirty days after both delivery
// and the event, so a long retry can never lose another recipient's completion.
func (r *NotificationLogRepository) CleanupReminderLogs(ctx context.Context, before time.Time, limit int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notification_log WHERE id IN (
		SELECT id FROM notification_log WHERE event_type = 'reminder'
		AND date < ($1::timestamptz AT TIME ZONE 'UTC')::date AND sent_at < $1::timestamptz
		ORDER BY sent_at, id LIMIT $2 FOR UPDATE SKIP LOCKED
	)`, before.UTC(), limit)
	return tag.RowsAffected(), err
}

// WasNotificationSentRecently checks if a similar notification was sent in the last hour
func (r *NotificationLogRepository) WasNotificationSentRecently(
	ctx context.Context,
	calendarID uuid.UUID,
	date time.Time,
	eventType string,
	recipientID uuid.UUID,
	channel string,
) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM notification_log
			WHERE calendar_id = $1
			  AND date = $2
			  AND event_type = $3
			  AND recipient_id = $4
			  AND channel = $5
			  AND sent_at > NOW() - INTERVAL '1 hour'
		)`

	var exists bool
	err := r.pool.QueryRow(ctx, query, calendarID, date, eventType, recipientID, channel).Scan(&exists)
	return exists, err
}

// LogNotification records a sent notification
func (r *NotificationLogRepository) LogNotification(
	ctx context.Context,
	calendarID uuid.UUID,
	date time.Time,
	eventType string,
	recipientType string,
	recipientID uuid.UUID,
	channel string,
) error {
	query := `
		INSERT INTO notification_log
			(calendar_id, date, event_type, recipient_type, recipient_id, channel)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := r.pool.Exec(ctx, query, calendarID, date, eventType, recipientType, recipientID, channel)
	return err
}

// CleanupOldLogs deletes logs older than 30 days
func (r *NotificationLogRepository) CleanupOldLogs(ctx context.Context) error {
	query := `DELETE FROM notification_log WHERE sent_at < NOW() - INTERVAL '30 days'
		AND (event_type <> 'reminder' OR date < CURRENT_DATE - 30)`
	_, err := r.pool.Exec(ctx, query)
	return err
}
