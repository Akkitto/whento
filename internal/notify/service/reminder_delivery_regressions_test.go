// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/whento/pkg/email"
	availabilityModels "github.com/whento/whento/internal/availability/models"
	"github.com/whento/whento/internal/notify/models"
)

type leaseCheckingMailer struct {
	*fakeMailer
	now       *time.Time
	deadlines []time.Duration
}

func (m *leaseCheckingMailer) SendContext(ctx context.Context, msg email.Email) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("reminder send has no deadline")
	}
	m.deadlines = append(m.deadlines, time.Until(deadline))
	if err := ctx.Err(); err != nil {
		return err
	}
	*m.now = m.now.Add(25 * time.Second)
	return m.Send(msg)
}

type lostFanoutClaim struct {
	*fakeReminderJobStore
	checks int
}

func (s *lostFanoutClaim) RenewLease(ctx context.Context, id, token uuid.UUID, now time.Time, ttl time.Duration) error {
	s.checks++
	if s.checks > 1 {
		return models.ErrClaimLost
	}
	return s.fakeReminderJobStore.RenewLease(ctx, id, token, now, ttl)
}

func participantFanout(t *testing.T) (*reminderFixture, models.ReminderJob) {
	t.Helper()
	f := newReminderFixture(t, reminderConfig())
	for range 2 {
		p := f.participant
		p.ID = uuid.New()
		f.people.verified = append(f.people.verified, p)
		f.slots.available = append(f.slots.available, availabilityModels.AvailableParticipant{ID: p.ID})
	}
	job := enqueueDueJob()
	job.CalendarID = f.calendar.ID
	job.RecipientType = models.ReminderRecipientParticipants
	job.ClaimToken = uuid.New()
	return f, job
}

func TestReminderFanoutRenewsBeforeEveryBoundedSend(t *testing.T) {
	f, job := participantFanout(t)
	now := fixedReminderNow()
	f.scheduler.now = func() time.Time { return now }
	mailer := &leaseCheckingMailer{fakeMailer: f.mailer, now: &now}
	f.scheduler.emailService = mailer
	f.scheduler.deliverOne(t.Context(), job)
	if len(mailer.deadlines) != 3 || len(f.jobs.renewed) != 3 {
		t.Fatalf("context sends=%d lease renewals=%d, want one of each per recipient", len(mailer.deadlines), len(f.jobs.renewed))
	}
	for _, deadline := range mailer.deadlines {
		if deadline <= 0 || deadline >= preSendLease {
			t.Fatalf("send deadline %s does not fit inside lease", deadline)
		}
	}
	if len(f.jobs.sent) != 1 {
		t.Fatal("successful fanout was not completed")
	}
}

func TestReminderFanoutStopsImmediatelyWhenClaimIsLost(t *testing.T) {
	f, job := participantFanout(t)
	lost := &lostFanoutClaim{fakeReminderJobStore: f.jobs}
	f.scheduler.jobs = lost
	f.scheduler.deliverOne(t.Context(), job)
	if len(f.mailer.messages()) != 1 {
		t.Fatalf("sent %d emails after losing claim, want only first", len(f.mailer.messages()))
	}
	if len(f.jobs.sent) != 0 || len(f.jobs.failed) != 0 || len(f.jobs.canceledEvent) != 0 {
		t.Fatal("lost worker mutated the queue or canceled another worker's event")
	}
}

func TestReminderCancelledContextDoesNotSend(t *testing.T) {
	f := newReminderFixture(t, reminderConfig())
	job := enqueueDueJob()
	job.CalendarID = f.calendar.ID
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f.scheduler.deliverOne(ctx, job)
	if len(f.mailer.messages()) != 0 {
		t.Fatal("shutdown/cancelled context sent mail")
	}
}

func TestReminderPastEventDoesNotSend(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Tokyo", "Pacific/Honolulu"} {
		t.Run(zone, func(t *testing.T) {
			f := newReminderFixture(t, reminderConfig())
			f.calendar.Timezone = zone
			job := enqueueDueJob()
			job.CalendarID = f.calendar.ID
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			job.ScheduledAt = f.scheduler.eventMidnight(job.EventDate, loc).Add(-24 * time.Hour)
			f.scheduler.now = func() time.Time { return f.scheduler.eventMidnight(job.EventDate, loc) }
			f.scheduler.deliverOne(t.Context(), job)
			if len(f.mailer.messages()) != 0 || len(f.jobs.canceledClaim) != 1 {
				t.Fatal("overdue event was not canceled under its own claim")
			}
		})
	}
}

func TestReminderDisqualifiedEventOnlyCancelsOwnedClaim(t *testing.T) {
	f := newReminderFixture(t, reminderConfig())
	f.slots.count = 0
	job := enqueueDueJob()
	job.CalendarID = f.calendar.ID
	f.scheduler.deliverOne(t.Context(), job)
	if len(f.jobs.canceledClaim) != 1 || len(f.jobs.canceledEvent) != 0 {
		t.Fatal("event snapshot used an unfenced whole-event cancellation")
	}
}

func TestReminderBackoffSaturatesBeforeOverflow(t *testing.T) {
	s := &ReminderScheduler{backoffBase: time.Minute, backoffMax: 24 * time.Hour}
	for _, attempt := range []int{25, 40, 100, int(^uint(0) >> 1)} {
		if got := s.backoff(attempt); got != s.backoffMax {
			t.Fatalf("attempt %d backoff=%s want cap=%s", attempt, got, s.backoffMax)
		}
	}
}

func TestReminderFailureUsesPersistedAttemptBudget(t *testing.T) {
	f := newReminderFixture(t, reminderConfig())
	job := enqueueDueJob()
	job.MaxAttempts = 2
	f.scheduler.recordFailure(t.Context(), job, errReminderSend)
	if f.jobs.failed[0].max != job.MaxAttempts {
		t.Fatal("delivery ignored persisted attempt budget")
	}
}

func TestReminderEmailEscapesCalendarNameAndURL(t *testing.T) {
	f := newReminderFixture(t, reminderConfig())
	f.calendar.Name = `<img src=x onerror=alert(1)>`
	body := f.scheduler.buildReminderEmail(f.calendar, fixedReminderNow(), `https://whento.test/" onmouseover="evil`, "en")
	if strings.Contains(body, `<img`) || strings.Contains(body, `href="https://whento.test/" onmouseover=`) {
		t.Fatal("reminder HTML contains unescaped data")
	}
}

type canceledReminderMailer struct {
	*fakeMailer
	started chan struct{}
}

func (m *canceledReminderMailer) SendContext(ctx context.Context, _ email.Email) error {
	close(m.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestReminderShutdownInterruptsInFlightSend(t *testing.T) {
	f := newReminderFixture(t, reminderConfig())
	mailer := &canceledReminderMailer{fakeMailer: f.mailer, started: make(chan struct{})}
	f.scheduler.emailService = mailer
	job := enqueueDueJob()
	job.CalendarID = f.calendar.ID
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); f.scheduler.deliverOne(ctx, job) }()
	select {
	case <-mailer.started:
	case <-time.After(time.Second):
		t.Fatal("send did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("send ignored shutdown")
	}
	if len(f.jobs.sent) != 0 || len(f.jobs.failed) != 0 {
		t.Fatal("shutdown recorded a send or burned retry budget")
	}
}

type failedReminderLedger struct{ *fakeNotificationLog }

func (l *failedReminderLedger) LogNotification(context.Context, uuid.UUID, time.Time, string, string, uuid.UUID, string) error {
	return errors.New("ledger unavailable")
}

func TestReminderChatLedgerFailureIsRetried(t *testing.T) {
	f := newReminderFixture(t, reminderConfig())
	f.scheduler.notificationLog = &failedReminderLedger{fakeNotificationLog: f.log}
	job := enqueueDueJob()
	job.CalendarID, job.Channel = f.calendar.ID, "discord"
	f.scheduler.deliverOne(t.Context(), job)
	if len(f.external.calls) != 1 || len(f.jobs.failed) != 1 || len(f.jobs.sent) != 0 {
		t.Fatal("chat delivery ignored a failed ledger write")
	}
}
