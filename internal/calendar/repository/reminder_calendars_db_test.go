// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package repository_test

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/whento/whento/internal/calendar/models"
	"github.com/whento/whento/internal/calendar/repository"
	"github.com/whento/whento/internal/testutil/dbtest"
)

func TestListForReminderScanPreservesRowsAndFiltersEnabledFlags(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	owner := newOwner(t, pool)
	repo := repository.NewCalendarRepository(pool)
	withoutConfig := newCalendar(owner.ID)
	if err := repo.Create(ctx, withoutConfig); err != nil {
		t.Fatal(err)
	}
	want := make(map[uuid.UUID]*models.Calendar)
	excluded := map[uuid.UUID]bool{withoutConfig.ID: true}
	for _, config := range []string{`{}`, `{"enabled":false,"reminders":{"enabled":true}}`, `{"enabled":true,"reminders":{"enabled":false}}`, `{"enabled":"true","reminders":{"enabled":true}}`, `{"enabled":true,"reminders":{"enabled":true}}`} {
		calendar := newCalendar(owner.ID, func(c *models.Calendar) {
			c.NotifyConfig = strPtr(config)
			c.Description = "reminder scan fixture"
			c.LockParticipants = true
		})
		if err := repo.Create(ctx, calendar); err != nil {
			t.Fatal(err)
		}
		stored, err := repo.GetByID(ctx, calendar.ID)
		if err != nil {
			t.Fatal(err)
		}
		if config == `{"enabled":true,"reminders":{"enabled":true}}` {
			want[calendar.ID] = stored
		} else {
			excluded[calendar.ID] = true
		}
	}
	rows, err := repo.ListForReminderScan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	for _, got := range rows {
		id := got.ID.String()
		if previous != "" && previous >= id {
			t.Fatalf("scan is not ordered by calendar ID: %s >= %s", previous, id)
		}
		previous = id
		if excluded[got.ID] {
			t.Fatal("calendar without both enabled boolean flags was included")
		}
		if stored, ok := want[got.ID]; ok {
			if !reflect.DeepEqual(got, stored) {
				t.Fatalf("scan did not preserve stored calendar fields: got %+v, want %+v", got, stored)
			}
			delete(want, got.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("notification-configured calendars missing from scan: %v", want)
	}
}
