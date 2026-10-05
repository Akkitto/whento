// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

// Package sessionlock serializes session issuance and credential transitions.
// Always acquire this lock before user, MFA, or refresh-token row locks.
package sessionlock

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Two-int advisory locks are disjoint from bigint locks (including cloud
// quotas). The class also keeps these locks separate from future two-int users.
const class int32 = 0x57485353 // WHSS: WhenTo session security

// Acquire holds the user's lock until the transaction commits or rolls back.
// A hash collision only serializes unrelated users; it never weakens the lock.
func Acquire(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	digest := sha256.Sum256(userID[:])
	id := int32(binary.BigEndian.Uint32(digest[:4]))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, class, id); err != nil {
		return fmt.Errorf("acquire session lock: %w", err)
	}
	return nil
}
