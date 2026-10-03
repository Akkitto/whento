// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package service

import (
	"time"

	"github.com/google/uuid"
)

// pendingCustomTokenIssuer is what minting a pending-MFA token needs of the JWT
// manager. Both the concrete *jwt.Manager and the narrower interfaces the
// magic-link and password-reset services already hold satisfy it, so every
// half-completed sign-in can share the claim shape below.
type pendingCustomTokenIssuer interface {
	GenerateCustomToken(claims map[string]interface{}) (string, error)
}

// pendingMFAToken mints the five-minute temporary token a half-completed
// sign-in hands to the MFA form.
//
// Login, passkey, magic link and password reset all end in the same MFA page, so
// they must all mint the same shape of token: the pending marker, the captured
// security generation (finalization refuses a token minted on a stale one) and
// the user. Keeping the claim shape in one place is what makes the whole set
// consistent instead of four hand-rolled copies.
func pendingMFAToken(issuer pendingCustomTokenIssuer, userID uuid.UUID, securityGeneration int64) (string, error) {
	expiresAt := time.Now().Add(5 * time.Minute)
	token, err := issuer.GenerateCustomToken(map[string]interface{}{
		"user_id":     userID.String(),
		"mfa_pending": true,
		"sec_gen":     securityGeneration,
		"exp":         expiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}

	return token, nil
}
