// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package repository

import (
	"context"
	"fmt"

	"github.com/whento/whento/internal/calendar/models"
)

// ListForReminderScan enumerates calendars with notification configuration.
// Reminder eligibility is checked by the scheduler and rechecked at delivery;
// this background-only query must not be used as an owner-authorized API list.
func (r *CalendarRepository) ListForReminderScan(ctx context.Context) ([]*models.Calendar, error) {
	query := `
		SELECT id, owner_id, name, description, public_token, ics_token, threshold, allowed_weekdays, min_duration_hours, timezone, holidays_policy, allow_holiday_eves, allowed_hours, notify_on_threshold, notify_config, lock_participants, allow_anonymous_participants, start_date, end_date, created_at, updated_at
		FROM calendars
		WHERE notify_config IS NOT NULL
		ORDER BY id`

	rows, err := r.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars with notify config: %w", err)
	}
	defer rows.Close()

	var calendars []*models.Calendar
	for rows.Next() {
		calendar := &models.Calendar{}
		err := rows.Scan(
			&calendar.ID,
			&calendar.OwnerID,
			&calendar.Name,
			&calendar.Description,
			&calendar.PublicToken,
			&calendar.ICSToken,
			&calendar.Threshold,
			&calendar.AllowedWeekdays,
			&calendar.MinDurationHours,
			&calendar.Timezone,
			&calendar.HolidaysPolicy,
			&calendar.AllowHolidayEves,
			&calendar.AllowedHours,
			&calendar.NotifyOnThreshold,
			&calendar.NotifyConfig,
			&calendar.LockParticipants,
			&calendar.AllowAnonymousParticipants,
			&calendar.StartDate,
			&calendar.EndDate,
			&calendar.CreatedAt,
			&calendar.UpdatedAt,
		)
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
