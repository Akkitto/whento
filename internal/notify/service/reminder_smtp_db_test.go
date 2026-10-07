// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package service

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whento/pkg/email"
	availabilityModels "github.com/whento/whento/internal/availability/models"
	calendarModels "github.com/whento/whento/internal/calendar/models"
	calendarRepo "github.com/whento/whento/internal/calendar/repository"
	"github.com/whento/whento/internal/notify/models"
	"github.com/whento/whento/internal/notify/repository"
	"github.com/whento/whento/internal/testutil/dbtest"
)

// A real SMTP server: Bob's RCPT can reject, while accepted DATA is counted.
// This pins partial-fanout retries against the actual durable ledger, not a mock.
type reminderSMTPSink struct {
	mu     sync.Mutex
	reject string
	counts map[string]int
	bodies []string
	port   int
}

func newReminderSMTPSink(t *testing.T) *reminderSMTPSink {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		t.Fatal("SMTP listener did not return a TCP address")
	}
	s := &reminderSMTPSink{counts: make(map[string]int), port: addr.Port}
	done := make(chan struct{})
	var workers sync.WaitGroup
	var connections []net.Conn
	var connectionsMu sync.Mutex
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connectionsMu.Lock()
			connections = append(connections, conn)
			connectionsMu.Unlock()
			workers.Add(1)
			go func() { defer workers.Done(); s.serve(conn) }()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		connectionsMu.Lock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		connectionsMu.Unlock()
		workers.Wait()
	})
	return s
}

func (s *reminderSMTPSink) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprint(conn, "220 WhenTo test SMTP\r\n")
	r := bufio.NewScanner(conn)
	var recipient string
	for r.Scan() {
		line := r.Text()
		switch {
		case strings.HasPrefix(line, "RCPT TO:"):
			recipient = strings.Trim(line[len("RCPT TO:"):], "<>")
			s.mu.Lock()
			rejected := recipient == s.reject
			s.mu.Unlock()
			if rejected {
				_, _ = fmt.Fprint(conn, "550 mailbox rejected PRIVATE-REJECTION-SECRET\r\n")
				return
			}
		case line == "DATA":
			_, _ = fmt.Fprint(conn, "354 send message\r\n")
			var body strings.Builder
			for r.Scan() && r.Text() != "." {
				body.WriteString(r.Text())
				body.WriteByte('\n')
			}
			s.mu.Lock()
			s.counts[recipient]++
			s.bodies = append(s.bodies, body.String())
			s.mu.Unlock()
		case line == "QUIT":
			_, _ = fmt.Fprint(conn, "221 bye\r\n")
			return
		}
		_, _ = fmt.Fprint(conn, "250 OK\r\n")
	}
}

func smtpReminderFixture(t *testing.T) (*reminderFixture, *pgxpool.Pool, *reminderSMTPSink, models.ReminderJob) {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	dbtest.LockSingletonAccounts(ctx, t, pool)
	f := newReminderFixture(t, reminderConfig())
	f.owner.Email = fmt.Sprintf("owner-%s@example.test", f.owner.ID)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, display_name, role, locale, timezone)
		VALUES ($1, $2, 'test-hash', 'Reminder owner', 'user', 'en', 'UTC')`, f.owner.ID, f.owner.Email); err != nil {
		t.Fatal(err)
	}
	dbtest.Cleanup(t, pool, `DELETE FROM users WHERE id = $1`, f.owner.ID)
	f.calendar.PublicToken = "pub-" + f.calendar.ID.String()
	f.calendar.ICSToken = "ics-" + f.calendar.ID.String()
	f.calendar.AllowedWeekdays = []int{0, 1, 2, 3, 4, 5, 6}
	f.calendar.HolidaysPolicy = "ignore"
	calendars := calendarRepo.NewCalendarRepository(pool)
	if err := calendars.Create(ctx, f.calendar); err != nil {
		t.Fatal(err)
	}
	f.scheduler.calendarRepo = calendars
	f.scheduler.jobs = repository.NewReminderJobRepository(pool)
	f.scheduler.notificationLog = repository.NewNotificationLogRepository(pool)
	f.scheduler.now = time.Now
	sink := newReminderSMTPSink(t)
	f.scheduler.emailService = email.NewService(email.Config{Host: "127.0.0.1", Port: sink.port, FromAddress: "no-reply@example.test"}, quietLogger())
	now := time.Now().UTC()
	date := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	job := models.ReminderJob{CalendarID: f.calendar.ID, EventDate: date, RecipientType: models.ReminderRecipientParticipants,
		Channel: "email", ScheduledAt: date.Add(-24 * time.Hour), NextAttemptAt: now.Add(-time.Second), MaxAttempts: 5}
	return f, pool, sink, job
}

func TestReminderSMTPBouncingRecipientNeverResendsCompletedRecipientAfterTwoHours(t *testing.T) {
	f, pool, sink, job := smtpReminderFixture(t)
	ctx := dbtest.Context(t)
	f.participant.Email = strptr("ann@example.test")
	bob := f.participant
	bob.ID, bob.Email = uuid.New(), strptr("bob@example.test")
	f.people.verified = []calendarModels.Participant{f.participant, bob}
	f.slots.available = []availabilityModels.AvailableParticipant{{ID: f.participant.ID}, {ID: bob.ID}}
	sink.reject = *bob.Email
	var logs bytes.Buffer
	f.scheduler.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || !worked {
		t.Fatalf("first pass: %v, %v", worked, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_log SET sent_at = now() - interval '2 hours' WHERE calendar_id = $1`, job.CalendarID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET next_attempt_at = now() WHERE calendar_id = $1`, job.CalendarID); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	sink.reject = ""
	sink.mu.Unlock()
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || !worked {
		t.Fatalf("retry: %v, %v", worked, err)
	}
	// A fresh process/restart and another ordinary enqueue must also not resend.
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || worked {
		t.Fatalf("restart duplicate: %v, %v", worked, err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.counts["ann@example.test"] != 1 || sink.counts["bob@example.test"] != 1 {
		t.Fatalf("recipient sends: %v", sink.counts)
	}
	var status string
	var cleared bool
	if err := pool.QueryRow(ctx, `SELECT status, last_error IS NULL FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&status, &cleared); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || !cleared {
		t.Fatalf("recovered job: status=%s cleared=%v", status, cleared)
	}
	if strings.Contains(logs.String(), "PRIVATE-REJECTION-SECRET") {
		t.Fatal("SMTP response secret was logged")
	}
	for _, body := range sink.bodies {
		if !strings.Contains(body, "?cancel="+job.EventDate.Format("2006-01-02")) {
			t.Fatal("real SMTP reminder omitted participation cancellation")
		}
	}
}

func TestReminderSMTPPastEventIsCanceledWithoutAnyMail(t *testing.T) {
	f, pool, sink, job := smtpReminderFixture(t)
	ctx := dbtest.Context(t)
	job.EventDate = job.EventDate.AddDate(0, 0, -4)
	job.ScheduledAt = job.EventDate.Add(-24 * time.Hour)
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || !worked {
		t.Fatalf("past-event pass: %v, %v", worked, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "canceled" {
		t.Fatalf("past event status=%s", status)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.bodies) != 0 {
		t.Fatal("past event produced reminder mail")
	}
}

func TestReminderSMTPUnconfiguredCancelsWithoutConsumingAttempts(t *testing.T) {
	f, pool, sink, job := smtpReminderFixture(t)
	ctx := dbtest.Context(t)
	f.scheduler.emailService = &fakeMailer{configured: false}
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || !worked {
		t.Fatalf("SMTP suppression: %v, %v", worked, err)
	}
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status, attempt FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "canceled" || attempts != 0 {
		t.Fatalf("absent SMTP burned retries: %s, %d", status, attempts)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.bodies) != 0 {
		t.Fatal("unconfigured SMTP produced mail")
	}
}

func TestReminderSMTPDisabledCalendarNeitherEnqueuesNorSends(t *testing.T) {
	f, pool, sink, job := smtpReminderFixture(t)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `UPDATE calendars SET notify_config = jsonb_set(notify_config, '{reminders,enabled}', 'false') WHERE id = $1`, job.CalendarID); err != nil {
		t.Fatal(err)
	}
	f.scheduler.enqueueDue(ctx)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("reminders-off calendar was enqueued")
	}
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || !worked {
		t.Fatalf("disabled reminder: %v, %v", worked, err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.bodies) != 0 {
		t.Fatal("disabled reminder produced mail")
	}
}

func TestReminderSMTPConfiguredRejectionExhaustsStoredBudget(t *testing.T) {
	f, pool, sink, job := smtpReminderFixture(t)
	ctx := dbtest.Context(t)
	job.MaxAttempts = 3 // A changed process default must not rewrite this job's budget.
	sink.reject = *f.participant.Email
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= job.MaxAttempts; attempt++ {
		if worked, err := f.scheduler.deliverDue(ctx); err != nil || !worked {
			t.Fatalf("attempt %d: %v, %v", attempt, worked, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE reminder_jobs SET next_attempt_at = now() WHERE calendar_id = $1 AND status = 'pending'`, job.CalendarID); err != nil {
			t.Fatal(err)
		}
	}
	var status, reason string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status, attempt, last_error FROM reminder_jobs WHERE calendar_id = $1`, job.CalendarID).Scan(&status, &attempts, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 3 || reason != "email_delivery_failed" {
		t.Fatalf("exhausted state: %s, %d, %s", status, attempts, reason)
	}
	sink.mu.Lock()
	sink.reject = ""
	sink.mu.Unlock()
	if err := f.scheduler.jobs.Enqueue(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if worked, err := f.scheduler.deliverDue(ctx); err != nil || worked {
		t.Fatalf("exhausted job restarted after SMTP recovery: %v, %v", worked, err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.bodies) != 0 {
		t.Fatal("permanently failed job sent after recovery")
	}
}
