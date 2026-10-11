// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package config

import (
	"os"
	"strings"
	"testing"
)

func TestHolidayFallbackEnvironmentIsOptIn(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false}, {"false", false}, {"true", true},
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("HOLIDAYS_NAGER_FALLBACK_ENABLED", tc.value)
			cfg := Load()
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			if cfg.HolidaysNagerFallbackEnabled != tc.want {
				t.Fatalf("fallback enabled = %t; want %t", cfg.HolidaysNagerFallbackEnabled, tc.want)
			}
		})
	}
}

func TestHolidayFallbackRejectsMalformedEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("HOLIDAYS_NAGER_FALLBACK_ENABLED", "enabled")
	if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "HOLIDAYS_NAGER_FALLBACK_ENABLED") {
		t.Fatalf("invalid privacy setting accepted: %v", err)
	}
}

func TestHostBackendLoadsHolidayFallbackFromDotenv(t *testing.T) {
	clearEnv(t)
	t.Chdir(t.TempDir())
	if err := os.Unsetenv("HOLIDAYS_NAGER_FALLBACK_ENABLED"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte("HOLIDAYS_NAGER_FALLBACK_ENABLED=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg := Load(); !cfg.HolidaysNagerFallbackEnabled {
		t.Fatal("host backend ignored holiday fallback setting in .env")
	}
}
