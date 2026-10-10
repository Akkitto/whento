// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package repository

import (
	"context"
	"fmt"

	"github.com/whento/whento/internal/calendar/models"
)

// ListForReminderScan enumerates calendars with notifications and reminders on.
// Reminder eligibility is checked by the scheduler and rechecked at delivery;
// this background-only query must not be used as an owner-authorized API list.
func (r *CalendarRepository) ListForReminderScan(ctx context.Context) ([]*models.Calendar, error) {
	query := `
		SELECT ` + calendarColumns + `
		FROM calendars
		WHERE notify_config->'enabled' = 'true'::jsonb
		  AND notify_config->'reminders'->'enabled' = 'true'::jsonb
		ORDER BY id`

	rows, err := r.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars with notify config: %w", err)
	}
	defer rows.Close()

	var calendars []*models.Calendar
	for rows.Next() {
		calendar, err := scanCalendar(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan calendar: %w", err)
		}
		calendars = append(calendars, calendar)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating calendars: %w", err)
	}

	return calendars, nil
}
