// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package main

import (
	"context"
	"testing"
	"time"
)

func TestReminderWorkerStopWaitsForCompletionBeforePoolClose(t *testing.T) {
	canceled := make(chan struct{})
	allowWrite := make(chan struct{})
	written := make(chan struct{})
	stop := startReminderWorker(t.Context(), func(ctx context.Context) {
		<-ctx.Done()
		close(canceled)
		<-allowWrite // Stand in for recording an accepted send with the pool still open.
		close(written)
	})
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("worker did not receive shutdown")
	}
	select {
	case <-stopped:
		t.Fatal("stop returned before completion write")
	default:
	}
	close(allowWrite)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("worker was not joined")
	}
	select {
	case <-written:
	default:
		t.Fatal("completion write was not awaited")
	}
	stop() // The deferred stop on an already stopped worker is safe.
}
