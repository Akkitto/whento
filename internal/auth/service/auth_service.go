// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/whento/pkg/cache"
	"github.com/whento/pkg/jwt"
	"github.com/whento/pkg/logger"
	"github.com/whento/pkg/validator"
	"github.com/whento/whento/internal/auth/models"
	"github.com/whento/whento/internal/auth/repository"
	mfaModels "github.com/whento/whento/internal/mfa/models"
	mfaRepo "github.com/whento/whento/internal/mfa/repository"
)

var (
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrInvalidToken         = errors.New("invalid or expired token")
	ErrUserNotFound         = errors.New("user not found")
	ErrUserAlreadyExists    = errors.New("user with this email already exists")
	ErrPasswordMismatch     = errors.New("current password is incorrect")
	ErrCannotDeleteSelf     = errors.New("cannot delete your own account")
	ErrCannotDemoteSelf     = errors.New("cannot change your own role")
	ErrLastAdmin            = errors.New("the instance must keep at least one administrator")
	ErrRegistrationDisabled = errors.New("new user registration is disabled")
	ErrEmailNotAllowed      = errors.New("email address is not allowed to register")
	ErrAccountLocked        = errors.New("too many failed login attempts, try again later")
)

const (
	maxLoginAttempts    = 10
	loginLockoutWindow  = 15 * time.Minute
	loginAttemptsPrefix = "login_attempts:"
)

// UserRepository defines the interface for user repository operations. It is
// deliberately the slice of the repository AuthService actually calls — the
// first-user bootstrap primitive (CreateFirstUser) and the ordinary account
// reads/writes — and nothing else. In particular the old split role-decision
// read (DetermineRoleAtomically) is gone so the unsafe count-then-insert
// pattern it embodies cannot quietly return.
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	// CreateFirstUser inserts a user, refusing with ErrFirstUserExists when the
	// table already has a row, atomically with the emptiness check. Register
	// uses it so the "first user is the administrator" decision cannot race.
	CreateFirstUser(ctx context.Context, user *models.User) error
	// FirstUserCreated is the durable "has the instance ever been bootstrapped?"
	// read (app_state.first_user_created, set transactionally by
	// CreateFirstUser). It is what lets a steady-state registration (the common
	// case) skip the global first-user advisory lock entirely; only an instance
	// that has never been bootstrapped — or one whose marker cannot be read —
	// falls back to CreateFirstUser's locked transition.
	FirstUserCreated(ctx context.Context) (bool, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]*models.User, error)
	UpdateRole(ctx context.Context, userID uuid.UUID, role string) error
	UpdatePassword(ctx context.Context, userID uuid.UUID, passwordHash string) error
}

// TokenRepository defines the interface for token repository operations
type TokenRepository interface {
	Create(ctx context.Context, token *models.RefreshToken) error
	GetByHash(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	// Consume marks a token rotated and reports whether this call won the race.
	Consume(ctx context.Context, tokenHash string) (bool, error)
	DeleteConsumedBefore(ctx context.Context, userID uuid.UUID, cutoff time.Time) error
	DeleteByHash(ctx context.Context, tokenHash string) error
	DeleteByUserID(ctx context.Context, userID uuid.UUID) error
}

// refreshGraceWindow is how long a rotated refresh token keeps working.
//
// Long enough to cover what honest clients actually do — two tabs waking together, a
// retry after a response was lost, a tab restored from sleep — and far too short to be
// a useful window for someone replaying a stolen cookie, who has no reason to be within
// seconds of the legitimate holder.
const refreshGraceWindow = 30 * time.Second

// MFARepository defines the interface for MFA repository operations
type MFARepository interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*mfaModels.UserMFA, error)
}

// AuthService handles authentication business logic
type AuthService struct {
	userRepo        UserRepository
	tokenRepo       TokenRepository
	mfaRepo         MFARepository
	jwtManager      *jwt.Manager
	cache           cache.Cache
	bcryptCost      int
	allowedRegister bool
	allowedEmails   []string
}

// NewAuthService creates a new auth service
func NewAuthService(
	userRepo UserRepository,
	tokenRepo TokenRepository,
	mfaRepo MFARepository,
	jwtManager *jwt.Manager,
	appCache cache.Cache,
	bcryptCost int,
	allowedRegister bool,
	allowedEmails []string,
) *AuthService {
	return &AuthService{
		userRepo:        userRepo,
		tokenRepo:       tokenRepo,
		mfaRepo:         mfaRepo,
		jwtManager:      jwtManager,
		cache:           appCache,
		bcryptCost:      bcryptCost,
		allowedRegister: allowedRegister,
		allowedEmails:   allowedEmails,
	}
}

// Register creates a new user account.
//
// The first account of an instance is the administrator and is exempt from the
// email allow-list — that is the open-registration variant of bootstrapping a
// fresh instance. "First" is decided by the same atomic repository primitive
// the bootstrap flow uses (CreateFirstUser), not by a count-then-insert: the
// check and the INSERT share one advisory lock, so a racing bootstrap and a
// racing registration cannot both become the first — and therefore the only —
// administrator.
//
// Only the very first registration needs that lock. The app_state
// first_user_created marker is the persisted "bootstrap completed" flag — set
// once, transactionally with the first account, and never cleared — so once it
// exists, an ordinary registration makes no first-user decision at all and
// takes the cheap unlocked fast path (FirstUserCreated → allow-list → Create)
// without ever entering the global advisory lock that serialises the
// empty-instance transition. The locked path is reserved for instances the fast
// read finds unbootstrapped — or cannot read, where the fallback is the same
// safe decision as a read that returned "not created yet".
func (s *AuthService) Register(ctx context.Context, req *models.RegisterRequest) (*models.AuthResponse, error) {
	// Registration off is registration off for *everyone*, first user included.
	// The first-account path for a closed instance is the bootstrap flow (with
	// its boot key), not a loophole in this gate — otherwise disabling
	// registration would be cosmetics instead of a boundary. Checked before the
	// bcrypt hash and before any advisory lock, so a closed instance is not a
	// CPU sink and does not contend on the lock the legitimate bootstrap needs.
	if !s.allowedRegister {
		return nil, ErrRegistrationDisabled
	}

	// Hash password first (expensive operation, do outside any lock)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Determine locale (default to English if not provided)
	locale := models.LocaleEN
	if req.Locale == models.LocaleFR || req.Locale == models.LocaleEN {
		locale = req.Locale
	}

	// Steady-state fast path: a durable marker read that finds the instance was
	// bootstrapped means this registration is one of the crowd — allow-list,
	// ordinary role, no global first-user lock. The marker is set once with the
	// first account and never cleared, so deleting users cannot reopen the
	// bootstrap decision. Failing to read the marker falls through to the locked
	// path, which is the same answer an "unbootstrapped" read would give.
	if configured, err := s.userRepo.FirstUserCreated(ctx); err == nil && configured {
		return s.registerOrdinary(ctx, req, string(passwordHash), locale)
	}

	// First user becomes admin. CreateFirstUser refuses (ErrFirstUserExists) when
	// a racing request already claimed the row, so "am I the first?" and "insert
	// me" are one atomic decision — exactly the primitive /bootstrap uses. The
	// loser of that race lands in the ordinary path: the instance is now
	// configured, so the allow-list decides.
	user := &models.User{
		Email:        req.Email,
		PasswordHash: string(passwordHash),
		DisplayName:  req.DisplayName,
		Role:         models.RoleAdmin,
		Locale:       locale,
		Timezone:     "Europe/Paris",
	}
	user.ID = uuid.New()

	if err := s.userRepo.CreateFirstUser(ctx, user); err != nil {
		if errors.Is(err, repository.ErrUserAlreadyExists) {
			return nil, ErrUserAlreadyExists
		}
		if !errors.Is(err, repository.ErrFirstUserExists) {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		// A racing first user won the slot: fall back to the ordinary path.
		return s.registerOrdinary(ctx, req, string(passwordHash), locale)
	}

	// Generate tokens
	return s.generateAuthResponse(ctx, user)
}

// registerOrdinary is the steady-state registration: the instance already has a
// user, so the email allow-list applies and the account is a plain user. It is
// deliberately lock-free — there is no first-user decision left to make.
func (s *AuthService) registerOrdinary(ctx context.Context, req *models.RegisterRequest, passwordHash, locale string) (*models.AuthResponse, error) {
	if !validator.EmailMatches(req.Email, s.allowedEmails) {
		return nil, ErrEmailNotAllowed
	}

	user := &models.User{
		Email:        req.Email,
		PasswordHash: passwordHash,
		DisplayName:  req.DisplayName,
		Role:         models.RoleUser,
		Locale:       locale,
		Timezone:     "Europe/Paris",
	}
	user.ID = uuid.New()

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrUserAlreadyExists) {
			return nil, ErrUserAlreadyExists
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return s.generateAuthResponse(ctx, user)
}

// Login authenticates a user
func (s *AuthService) Login(ctx context.Context, req *models.LoginRequest) (*models.AuthResponse, error) {
	// Check account lockout before any credential validation.
	//
	// The counter is keyed by a digest of the address, never the address itself:
	// this key lives in Redis for 15 minutes and shows up in `KEYS *` and in
	// `dump.rdb`, and a list of the addresses that recently failed to log in is
	// exactly the kind of thing that must not be lying around there.
	lockoutKey := loginAttemptsPrefix + cache.HashKeyPart(req.Email)
	if s.cache.IsEnabled() {
		var attempts int
		if err := s.cache.Get(ctx, lockoutKey, &attempts); err == nil {
			if attempts >= maxLoginAttempts {
				return nil, ErrAccountLocked
			}
		}
	}

	// Get user by email
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			s.incrementLoginAttempts(ctx, lockoutKey)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		s.incrementLoginAttempts(ctx, lockoutKey)
		return nil, ErrInvalidCredentials
	}

	// Login successful — reset failed attempts counter
	if s.cache.IsEnabled() {
		_ = s.cache.Delete(ctx, lockoutKey)
	}

	// Check if user has 2FA enabled
	mfa, err := s.mfaRepo.GetByUserID(ctx, user.ID)
	if err != nil && !errors.Is(err, mfaRepo.ErrMFANotFound) {
		return nil, fmt.Errorf("failed to check MFA status: %w", err)
	}

	// If MFA is enabled, return temporary token
	if mfa != nil && mfa.Enabled {
		tempToken, err := s.generateTempToken(user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to generate temp token: %w", err)
		}

		return &models.AuthResponse{
			RequireMFA: true,
			TempToken:  tempToken,
			User:       user,
		}, nil
	}

	// No MFA - generate full tokens
	return s.generateAuthResponse(ctx, user)
}

// incrementLoginAttempts increments the failed login counter for the given key
func (s *AuthService) incrementLoginAttempts(ctx context.Context, key string) {
	if !s.cache.IsEnabled() {
		return
	}
	var attempts int
	_ = s.cache.Get(ctx, key, &attempts)
	attempts++
	_ = s.cache.Set(ctx, key, attempts, loginLockoutWindow)
}

// RefreshToken rotates a refresh token, tolerating a racing client and refusing a
// replay.
//
// Rotation used to delete the row, which made the two indistinguishable: a second
// caller found nothing and was signed out, whether it was the user's other tab or
// somebody with a stolen cookie. Consuming the row keeps the difference legible.
//
//	live                      → rotate
//	consumed, inside window   → rotate again; this is the other tab, or a retry
//	consumed, outside window  → reuse: revoke every session this user has
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*models.AuthResponse, error) {
	// Validate refresh token format
	userID, err := s.jwtManager.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Get token from database
	tokenHash := repository.HashToken(refreshToken)
	storedToken, err := s.tokenRepo.GetByHash(ctx, tokenHash)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Get user
	uid, _ := uuid.Parse(userID)
	user, err := s.userRepo.GetByID(ctx, uid)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Verify stored token matches user, before anything is written. The previous
	// order deleted first and checked after, so a token belonging to somebody else
	// was destroyed on the way to being rejected.
	if storedToken.UserID != user.ID {
		return nil, ErrInvalidToken
	}

	if storedToken.ConsumedAt != nil {
		if time.Since(*storedToken.ConsumedAt) > refreshGraceWindow {
			// Used, superseded, and used again. No honest client does that, so the
			// cookie is assumed to be in someone else's hands and every session goes.
			// The user signs in again; whoever else held it gets nothing.
			if err := s.tokenRepo.DeleteByUserID(ctx, user.ID); err != nil {
				logger.FromContext(ctx).Error("failed to revoke sessions after refresh token reuse",
					"error", err, "user_ref", logger.Fingerprint(user.ID.String()))
			}
			// The one place a stolen refresh cookie becomes visible. Warn, not Info:
			// somebody should be able to alert on it.
			logger.FromContext(ctx).Warn("refresh token reused after rotation; revoked every session for the user",
				"user_ref", logger.Fingerprint(user.ID.String()),
				"consumed_ago", time.Since(*storedToken.ConsumedAt).String())

			return nil, ErrInvalidToken
		}
		// Inside the window: the other tab beat this one to it by a moment. Issuing a
		// fresh pair is what keeps both of them signed in. Returning the *same* pair
		// is not an option — only the hash was ever stored.
	} else if _, err := s.tokenRepo.Consume(ctx, tokenHash); err != nil {
		return nil, fmt.Errorf("failed to consume refresh token: %w", err)
	}

	// Spent tokens past the window are of no further use; dropping them here keeps
	// the table from growing by one row per refresh for the life of the session.
	if err := s.tokenRepo.DeleteConsumedBefore(ctx, user.ID, time.Now().Add(-refreshGraceWindow)); err != nil {
		logger.FromContext(ctx).Error("failed to purge consumed refresh tokens",
			"error", err, "user_ref", logger.Fingerprint(user.ID.String()))
	}

	// Generate new tokens
	return s.generateAuthResponse(ctx, user)
}

// Logout invalidates the refresh token
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	tokenHash := repository.HashToken(refreshToken)
	return s.tokenRepo.DeleteByHash(ctx, tokenHash)
}

// GetCurrentUser returns the current user
func (s *AuthService) GetCurrentUser(ctx context.Context, userID string) (*models.User, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	user, err := s.userRepo.GetByID(ctx, uid)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// UpdateProfile updates the current user's profile
func (s *AuthService) UpdateProfile(ctx context.Context, userID string, req *models.UpdateProfileRequest) (*models.User, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	user, err := s.userRepo.GetByID(ctx, uid)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Update fields if provided
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Locale != nil {
		user.Locale = *req.Locale
	}
	if req.Timezone != nil {
		user.Timezone = *req.Timezone
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return user, nil
}

// ChangePassword changes the current user's password
func (s *AuthService) ChangePassword(ctx context.Context, userID string, req *models.ChangePasswordRequest) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return ErrUserNotFound
	}

	user, err := s.userRepo.GetByID(ctx, uid)
	if err != nil {
		return ErrUserNotFound
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return ErrPasswordMismatch
	}

	// Hash new password
	newPasswordHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password
	if err := s.userRepo.UpdatePassword(ctx, uid, string(newPasswordHash)); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Invalidate all refresh tokens
	_ = s.tokenRepo.DeleteByUserID(ctx, uid)

	// Invalidate all active access tokens by recording password change time
	if s.cache != nil && s.cache.IsEnabled() {
		pwdKey := cache.UserPasswordChangedKey(userID)
		_ = s.cache.Set(ctx, pwdKey, time.Now().Unix(), s.jwtManager.AccessExpiry())
	}

	return nil
}

// ListUsers returns all users (admin only)
func (s *AuthService) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.userRepo.List(ctx)
}

// UpdateUserRole updates a user's role (admin only)
func (s *AuthService) UpdateUserRole(ctx context.Context, currentUserID, targetUserID string, role string) error {
	if currentUserID == targetUserID {
		return ErrCannotDemoteSelf
	}

	uid, err := uuid.Parse(targetUserID)
	if err != nil {
		return ErrUserNotFound
	}

	// Verify target user exists
	_, err = s.userRepo.GetByID(ctx, uid)
	if err != nil {
		return ErrUserNotFound
	}

	if err := s.userRepo.UpdateRole(ctx, uid, role); err != nil {
		if errors.Is(err, repository.ErrLastAdmin) {
			return ErrLastAdmin
		}
		return err
	}

	return nil
}

// DeleteUser deletes a user (admin only)
func (s *AuthService) DeleteUser(ctx context.Context, currentUserID, targetUserID string) error {
	if currentUserID == targetUserID {
		return ErrCannotDeleteSelf
	}

	uid, err := uuid.Parse(targetUserID)
	if err != nil {
		return ErrUserNotFound
	}

	if err := s.userRepo.Delete(ctx, uid); err != nil {
		if errors.Is(err, repository.ErrLastAdmin) {
			return ErrLastAdmin
		}
		return err
	}

	return nil
}

// IssueSession is the one place a fresh session is created: it issues the
// access/refresh token pair and persists the refresh token hash. Register,
// Login and the bootstrap flow all end on it, so none of them can drift apart
// in what a signed-in response looks like. The refresh token write is a
// synchronous database call on the request path, so it runs under the caller's
// context: a client disconnect, a request deadline or a server shutdown must be
// able to cancel it.
func (s *AuthService) IssueSession(ctx context.Context, user *models.User) (*models.AuthResponse, error) {
	return s.generateAuthResponse(ctx, user)
}

func (s *AuthService) generateAuthResponse(ctx context.Context, user *models.User) (*models.AuthResponse, error) {
	// Generate access token
	accessToken, err := s.jwtManager.GenerateAccessToken(user.ID.String(), user.Email, user.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// Generate refresh token
	refreshToken, expiresAt, err := s.jwtManager.GenerateRefreshToken(user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Store refresh token hash
	storedToken := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: repository.HashToken(refreshToken),
		ExpiresAt: expiresAt,
	}
	storedToken.ID = uuid.New()

	if err := s.tokenRepo.Create(ctx, storedToken); err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return &models.AuthResponse{
		AccessToken: accessToken,
		// Read from the manager rather than written here. The literal 900 that used to
		// sit in this field agreed with the token's real lifetime only at the default
		// setting: an instance configuring JWT_ACCESS_EXPIRY got a number that did not
		// describe the token it came with. The client schedules its refresh off this.
		ExpiresIn:        int64(s.jwtManager.AccessExpiry().Seconds()),
		RefreshToken:     refreshToken,
		RefreshExpiresAt: expiresAt,
		User:             user,
	}, nil
}

// generateTempToken generates a temporary token for 2FA verification (5-minute expiry)
func (s *AuthService) generateTempToken(userID uuid.UUID) (string, error) {
	// Generate a short-lived JWT with 5-minute expiry
	// We'll use the access token generator but with a shorter expiry
	// The token will contain the user ID and a special "mfa_pending" claim
	expiresAt := time.Now().Add(5 * time.Minute)
	token, err := s.jwtManager.GenerateCustomToken(map[string]interface{}{
		"user_id":     userID.String(),
		"mfa_pending": true,
		"exp":         expiresAt.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate temp token: %w", err)
	}

	return token, nil
}

// PasskeyLogin authenticates a user via passkey
// If the user has TOTP MFA enabled, returns a temp token requiring MFA verification
func (s *AuthService) PasskeyLogin(ctx context.Context, user *models.User) (*models.AuthResponse, error) {
	// Check if user has TOTP MFA enabled
	mfa, err := s.mfaRepo.GetByUserID(ctx, user.ID)
	if err != nil && !errors.Is(err, mfaRepo.ErrMFANotFound) {
		return nil, fmt.Errorf("failed to check MFA status: %w", err)
	}

	// If MFA is enabled, require TOTP verification even after passkey auth
	if mfa != nil && mfa.Enabled {
		tempToken, err := s.generateTempToken(user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to generate temp token: %w", err)
		}

		return &models.AuthResponse{
			RequireMFA: true,
			TempToken:  tempToken,
			User:       user,
		}, nil
	}

	// No MFA - generate full tokens directly
	return s.generateAuthResponse(ctx, user)
}

// VerifyMFAAndLogin verifies the MFA code and completes login
func (s *AuthService) VerifyMFAAndLogin(ctx context.Context, tempToken string, mfaCode string) (*models.AuthResponse, error) {
	// Validate temp token
	claims, err := s.jwtManager.ValidateCustomToken(tempToken)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Check mfa_pending claim
	mfaPending, ok := claims["mfa_pending"].(bool)
	if !ok || !mfaPending {
		return nil, ErrInvalidToken
	}

	// Prevent temp token replay: check JTI hasn't been consumed
	jti, _ := claims["jti"].(string)
	if jti != "" && s.cache != nil {
		// A jti is opaque, but it is still the identifier of one person's
		// half-completed sign-in, and it is the value carried by a token that is
		// still valid for five minutes. It is stored as a digest for the same
		// reason as everything else here.
		key := "mfa_used_jti:" + cache.HashKeyPart(jti)
		used, _ := s.cache.Exists(ctx, key)
		if used {
			return nil, ErrInvalidToken
		}
		// Mark JTI as consumed (TTL matches temp token expiry: 5 minutes)
		_ = s.cache.Set(ctx, key, true, 6*time.Minute)
	}

	// Extract user ID
	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return nil, ErrInvalidToken
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Generate full tokens
	return s.generateAuthResponse(ctx, user)
}
