// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{name: "valid"},
		{name: "missing definition", mutate: func(definitions map[string]any) { delete(definitions, limits[0][0]) }, want: "model models.BootstrapRequest is missing"},
		{name: "missing property", mutate: func(definitions map[string]any) {
			definitions[limits[0][0]] = map[string]any{"properties": map[string]any{}}
		}, want: "models.BootstrapRequest.password is missing"},
		{name: "missing length", mutate: func(definitions map[string]any) {
			definitions[limits[0][0]] = map[string]any{"properties": map[string]any{"password": map[string]any{}}}
		}, want: "got: <nil>"},
		{name: "wrong length", mutate: func(definitions map[string]any) {
			definitions[limits[0][0]] = map[string]any{"properties": map[string]any{"password": map[string]any{"maxLength": 73}}}
		}, want: "got: 73"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definitions := map[string]any{}
			for _, limit := range limits {
				definitions[limit[0]] = map[string]any{"properties": map[string]any{limit[1]: map[string]any{"maxLength": 72}}}
			}
			if tc.mutate != nil {
				tc.mutate(definitions)
			}
			raw, err := json.Marshal(map[string]any{"definitions": definitions})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "swagger.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			err = check(path)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("check = %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		if err := check(filepath.Join(t.TempDir(), "absent.json")); err == nil {
			t.Fatal("missing file accepted")
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "swagger.json")
		if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := check(path); err == nil || !strings.Contains(err.Error(), "parse") {
			t.Fatalf("check = %v, want parse failure", err)
		}
	})
}
