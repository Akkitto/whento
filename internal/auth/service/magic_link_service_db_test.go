// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package service

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/whento/whento/internal/auth/models"
	"github.com/whento/whento/internal/auth/repository"
	"github.com/whento/whento/internal/config"
	mfaModels "github.com/whento/whento/internal/mfa/models"
	"github.com/whento/whento/internal/testutil/dbtest"
)

func TestMagicLinkMFALookupFailureLeavesProofRetryable(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary login", true: "MFA login"}[enabled], func(t *testing.T) {
			pool := dbtest.Pool(t)
			ctx := dbtest.Context(t)
			dbtest.LockSingletonAccounts(ctx, t, pool)
			users := repository.NewUserRepository(pool)
			user := &models.User{Email: uuid.NewString() + "@example.test", PasswordHash: "hash", DisplayName: "Mailbox", Role: models.RoleUser, Locale: models.LocaleEN, Timezone: "UTC"}
			user.ID = uuid.New()
			if err := users.Create(ctx, user); err != nil {
				t.Fatal(err)
			}
			dbtest.CleanupContext(ctx, t, pool, `DELETE FROM users WHERE id = $1`, user.ID)
			proof := uuid.NewString()
			if err := users.SetMagicLinkToken(ctx, user.ID, proof, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			mfa := &fakeMFARepo{err: errStore}
			if enabled {
				mfa.mfa = &mfaModels.UserMFA{Enabled: true}
			}
			svc := NewMagicLinkService(users, mfa, nil, testJWT(t), &config.Config{JWTAccessExpiry: time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if _, err := svc.VerifyMagicLink(ctx, proof); !errors.Is(err, errStore) {
				t.Fatalf("lookup failure=%v", err)
			}
			if _, err := users.GetByMagicLinkToken(ctx, proof); err != nil {
				t.Fatalf("lookup failure burned link: %v", err)
			}
			mfa.err = nil
			response, err := svc.VerifyMagicLink(ctx, proof)
			if err != nil {
				t.Fatalf("retry: %v", err)
			}
			if response.RequireMFA != enabled {
				t.Fatalf("wrong MFA gate: %+v", response)
			}
			if enabled && (response.TempToken == "" || response.AccessToken != "" || response.RefreshToken != "") {
				t.Fatal("mailbox proof bypassed MFA")
			}
			if !enabled && (response.AccessToken == "" || response.RefreshToken == "") {
				t.Fatal("retry did not create session")
			}
			if _, err := svc.VerifyMagicLink(ctx, proof); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("successful proof was replayable: %v", err)
			}
			var sessions int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1`, user.ID).Scan(&sessions); err != nil {
				t.Fatal(err)
			}
			if (enabled && sessions != 0) || (!enabled && sessions != 1) {
				t.Fatalf("unexpected sessions: %d", sessions)
			}
		})
	}
}
