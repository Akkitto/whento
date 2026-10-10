// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package config

import (
	"strings"
	"testing"
	"time"
)

func TestReminderEnvironmentValidation(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"REMINDER_HOURS_BEFORE", "0"}, {"REMINDER_HOURS_BEFORE", "169"},
		{"REMINDER_HOURS_BEFORE", "9223372036854775807"},
		{"REMINDER_INTERVAL", "0s"}, {"REMINDER_INTERVAL", "-1s"},
		{"REMINDER_INTERVAL", "16m"},
		{"REMINDER_CATCH_UP_WINDOW", "0s"}, {"REMINDER_CATCH_UP_WINDOW", "25h"},
		{"REMINDER_MAX_ATTEMPTS", "0"}, {"REMINDER_MAX_ATTEMPTS", "-1"},
		{"REMINDER_MAX_ATTEMPTS", "2147483648"},
		{"REMINDER_RETRY_BACKOFF", "0s"}, {"REMINDER_RETRY_BACKOFF", "-1s"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.key, tc.value)
			if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("invalid reminder setting accepted: %v", err)
			}
		})
	}
}

func TestReminderEnvironmentAcceptsDocumentedValues(t *testing.T) {
	clearEnv(t)
	for key, value := range map[string]string{
		"REMINDER_HOURS_BEFORE": "168", "REMINDER_INTERVAL": "30m",
		"REMINDER_CATCH_UP_WINDOW": "24h", "REMINDER_MAX_ATTEMPTS": "100",
		"REMINDER_RETRY_BACKOFF": "48h",
	} {
		t.Setenv(key, value)
	}
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Reminders.HoursBefore != 168 || cfg.Reminders.Interval != 30*time.Minute || cfg.Reminders.CatchUpWindow != 24*time.Hour || cfg.Reminders.MaxAttempts != 100 || cfg.Reminders.RetryBackoff != 48*time.Hour {
		t.Fatalf("operator settings not loaded: %+v", cfg.Reminders)
	}
}
