// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Fork pull requests cannot publish upstream packages. Guard only the registry
// steps: skipping the entire job would silently remove their image/security gate.
// These assertions complement actionlint and run in the normal backend suite.
func TestCIImagePublicationPolicy(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	const jobHeader = "\n  build-dev-image:\n"
	start := strings.Index(source, jobHeader)
	if start < 0 {
		t.Fatal("CI has no build-dev-image job")
	}
	job := source[start:]
	if end := regexp.MustCompile(`(?m)^  [a-zA-Z0-9_-]+:\s*$`).FindStringIndex(job[len(jobHeader):]); end != nil {
		job = job[:end[0]+len(jobHeader)]
	}
	stepsStart := strings.Index(job, "\n    steps:\n")
	if stepsStart < 0 {
		t.Fatal("image job has no steps")
	}
	if !strings.Contains(job[:stepsStart], "\n    if: github.actor != 'dependabot[bot]'\n") {
		t.Error("image job must remain enabled for non-Dependabot fork PRs")
	}
	if regexp.MustCompile(`(?m)^  pull_request_target:`).MatchString(source) {
		t.Error("untrusted PR code must not run with pull_request_target privileges")
	}

	const publishGuard = "github.event_name == 'push' || github.event_name == 'workflow_dispatch' || (github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name == github.repository)"
	stepStarts := regexp.MustCompile(`(?m)^      - name: ([^\n]+)\n`).FindAllStringSubmatchIndex(job, -1)
	steps := make(map[string]string, len(stepStarts))
	for i, match := range stepStarts {
		end := len(job)
		if i+1 < len(stepStarts) {
			end = stepStarts[i+1][0]
		}
		name := job[match[2]:match[3]]
		if _, exists := steps[name]; exists {
			t.Fatalf("duplicate image step %q", name)
		}
		steps[name] = job[match[0]:end]
	}
	for _, tt := range []struct {
		name  string
		guard string
		want  []string
	}{
		{"Login to GitHub Container Registry", publishGuard, []string{"password: ${{ secrets.GITHUB_TOKEN }}"}},
		{"Build image for scanning", "", []string{"push: false", "load: true", "tags: whento-scan:candidate"}},
		{"Scan image for vulnerabilities", "", []string{"image-ref: whento-scan:candidate", "exit-code: '1'"}},
		{"Scan the bundled migrate binary (informational)", "", []string{"image-ref: whento-scan:candidate", "exit-code: '0'"}},
		{"Build and push Docker image", publishGuard, []string{"push: true"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			step, exists := steps[tt.name]
			if !exists {
				t.Fatal("required image step is missing")
			}
			var guard string
			if match := regexp.MustCompile(`(?m)^        if: ([^\n]+)$`).FindStringSubmatch(step); match != nil {
				guard = match[1]
			} else if strings.Contains(step, "\n        if:") {
				t.Fatal("guard must be an inspectable one-line expression")
			}
			if guard != tt.guard {
				t.Errorf("if = %q, want %q", guard, tt.guard)
			}
			if tt.name != "Scan the bundled migrate binary (informational)" && strings.Contains(step, "continue-on-error:") {
				t.Error("image gate/publication failures must not be hidden")
			}
			for _, want := range tt.want {
				if !strings.Contains(step, want) {
					t.Errorf("step is missing %q", want)
				}
			}
		})
	}
}
