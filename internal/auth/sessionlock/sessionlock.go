// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

// Package sessionlock serializes session issuance and credential transitions.
// Always acquire this lock before user, MFA, or refresh-token row locks.
package sessionlock

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Two-int advisory locks are disjoint from bigint locks (including cloud
// quotas). The class also keeps these locks separate from future two-int users.
const class int32 = 0x57485353 // WHSS: WhenTo session security

// Acquire holds the user's lock until the transaction commits or rolls back.
// An ID collision only serializes unrelated users; it never weakens the lock.
func Acquire(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, class, lockID(userID)); err != nil {
		return fmt.Errorf("acquire session lock: %w", err)
	}
	return nil
}

// lockID folds all UUID words into the advisory lock's int32 identifier. This
// is only a deterministic bucket, not a credential hash or an authenticator.
// Using every word avoids grouping UUIDs that share a prefix (such as UUIDv7).
func lockID(userID uuid.UUID) int32 {
	var id uint32
	for offset := 0; offset < len(userID); offset += 4 {
		id ^= binary.BigEndian.Uint32(userID[offset : offset+4])
	}
	return int32(id)
}
