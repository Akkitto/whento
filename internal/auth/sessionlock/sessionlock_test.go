// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package sessionlock

import (
	"encoding/binary"
	"testing"

	"github.com/google/uuid"
)

func TestLockIDUsesEveryUUIDWord(t *testing.T) {
	const word uint32 = 0x01234567
	for offset := 0; offset < len(uuid.UUID{}); offset += 4 {
		var userID uuid.UUID
		binary.BigEndian.PutUint32(userID[offset:offset+4], word)
		if got := lockID(userID); got != int32(word) {
			t.Fatalf("word at offset %d ignored: got %d", offset, got)
		}
	}
}

func TestLockIDIsStableAndPreservesSignedBits(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-0000ffffffff")
	for range 3 {
		if got := lockID(userID); got != -1 {
			t.Fatalf("unexpected lock ID: %d", got)
		}
	}
}
