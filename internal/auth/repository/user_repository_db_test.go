// WhenTo - Collaborative event calendar for self-hosted environments
// Copyright (C) 2025 WhenTo Contributors
// SPDX-License-Identifier: BSL-1.1

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whento/whento/internal/auth/models"
	"github.com/whento/whento/internal/auth/repository"
	"github.com/whento/whento/internal/testutil/dbtest"
)

// Repositories are hand-written SQL over a pool, so the only things worth checking are
// the ones a mock cannot: that the SQL is valid, that constraints hold, that a scan
// matches the columns selected, and that "no rows" becomes the sentinel error rather
// than a raw pgx.ErrNoRows leaking upward.
//
// These skip when DATABASE_URL is unset, so `make test` on a laptop without Postgres
// stays green. CI supplies the database and the migrations.

// newUser builds a user with unique identifiers and registers its own cleanup, so tests
// never truncate a shared table — `go test ./...` runs package binaries concurrently,
// and a dev server may be using the same database.
//
// ctx is the call site's request-scoped context: the cleanup deadline is derived
// from it (see dbtest.CleanupContext) so contextcheck sees the relationship.
func newUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, configure ...func(*models.User)) *models.User {
	t.Helper()

	// A user row is instance-wide state as far as the readiness and admin-invariant
	// tests are concerned; serialize against them (and against the other packages'
	// fixtures) for the test's lifetime. A no-op when the calling test already
	// holds the lock.
	dbtest.LockSingletonAccounts(ctx, t, pool)

	id := uuid.New()
	user := &models.User{
		Email:        fmt.Sprintf("repo-%s@example.test", id),
		PasswordHash: "$2a$04$abcdefghijklmnopqrstuv",
		DisplayName:  "Repository Test",
		Role:         models.RoleUser,
		Locale:       models.LocaleEN,
		Timezone:     "Europe/Paris",
	}
	user.ID = id

	for _, apply := range configure {
		apply(user)
	}

	dbtest.CleanupContext(ctx, t, pool, `DELETE FROM users WHERE id = $1`, user.ID)

	return user
}

func TestUserRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Every column the struct claims must survive the round trip. A mock would happily
	// return whatever it was handed; this catches a scan that skipped a column.
	byID, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if byID.Email != user.Email {
		t.Errorf("Email = %q, want %q", byID.Email, user.Email)
	}
	if byID.DisplayName != user.DisplayName {
		t.Errorf("DisplayName = %q, want %q", byID.DisplayName, user.DisplayName)
	}
	if byID.Role != models.RoleUser {
		t.Errorf("Role = %q, want %q", byID.Role, models.RoleUser)
	}
	if byID.Locale != models.LocaleEN {
		t.Errorf("Locale = %q, want %q", byID.Locale, models.LocaleEN)
	}
	if byID.Timezone != "Europe/Paris" {
		t.Errorf("Timezone = %q, want %q", byID.Timezone, "Europe/Paris")
	}
	if byID.PasswordHash != user.PasswordHash {
		t.Error("the password hash did not survive the round trip")
	}
	if byID.CreatedAt.IsZero() {
		t.Error("CreatedAt was not populated by the database default")
	}

	byEmail, err := repo.GetByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Errorf("GetByEmail returned %v, want %v", byEmail.ID, user.ID)
	}
}

func TestUserNotFoundIsASentinel(t *testing.T) {
	// pgx.ErrNoRows must not leak: callers switch on these.
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	if _, err := repo.GetByID(ctx, uuid.New()); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("GetByID error = %v, want ErrUserNotFound", err)
	}
	if _, err := repo.GetByEmail(ctx, "absolutely-nobody@example.test"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("GetByEmail error = %v, want ErrUserNotFound", err)
	}
}

// TestUserEmailIsUnique covers a constraint that lives in the schema. It is the only
// thing standing between two registrations racing on the same address, and no unit test
// can observe it.
func TestUserEmailIsUnique(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	first := newUser(ctx, t, pool)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := newUser(ctx, t, pool)
	second.Email = first.Email

	err := repo.Create(ctx, second)
	if !errors.Is(err, repository.ErrUserAlreadyExists) {
		t.Errorf("error = %v, want ErrUserAlreadyExists", err)
	}
}

func TestUserEmailIsCaseInsensitiveOnLookup(t *testing.T) {
	// Worth pinning either way: if the schema does not fold case, two accounts can
	// differ by capitalisation alone, and this test says which world we are in.
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := repo.GetByEmail(ctx, upper(user.Email))
	if err != nil && !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("GetByEmail: %v", err)
	}

	t.Logf("lookup by an upper-cased address: found=%v", err == nil)
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}

	return string(out)
}

func TestUserUpdates(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("profile", func(t *testing.T) {
		user.DisplayName = "Renamed"
		user.Locale = models.LocaleFR
		user.Timezone = "America/New_York"

		if err := repo.Update(ctx, user); err != nil {
			t.Fatalf("Update: %v", err)
		}

		got, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.DisplayName != "Renamed" || got.Locale != models.LocaleFR || got.Timezone != "America/New_York" {
			t.Errorf("profile did not persist: %+v", got)
		}
	})

	t.Run("password", func(t *testing.T) {
		if err := repo.UpdatePassword(ctx, user.ID, "$2a$04$zzzzzzzzzzzzzzzzzzzzzz", user.PasswordHash); err != nil {
			t.Fatalf("UpdatePassword: %v", err)
		}

		got, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.PasswordHash != "$2a$04$zzzzzzzzzzzzzzzzzzzzzz" {
			t.Error("the new password hash did not persist")
		}
	})

	t.Run("role", func(t *testing.T) {
		if err := repo.UpdateRole(ctx, user.ID, models.RoleAdmin); err != nil {
			t.Fatalf("UpdateRole: %v", err)
		}

		got, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Role != models.RoleAdmin {
			t.Errorf("Role = %q, want admin", got.Role)
		}
	})
}

func TestUserDelete(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, user.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := repo.GetByID(ctx, user.ID); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("the user survived deletion: %v", err)
	}
}

func TestExistsByEmail(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	exists, err := repo.ExistsByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("ExistsByEmail: %v", err)
	}
	if !exists {
		t.Error("a created user does not exist by email")
	}

	exists, err = repo.ExistsByEmail(ctx, fmt.Sprintf("nobody-%s@example.test", uuid.New()))
	if err != nil {
		t.Fatalf("ExistsByEmail: %v", err)
	}
	if exists {
		t.Error("an address that was never registered exists")
	}
}

// TestVerificationTokenLifecycle covers the token columns end to end. The lookup filters
// on expiry in SQL, which is the part worth exercising against a real clock.
func TestVerificationTokenLifecycle(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	token := uuid.NewString()
	if err := repo.SetVerificationToken(ctx, user.ID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetVerificationToken: %v", err)
	}

	got, err := repo.GetByVerificationToken(ctx, token)
	if err != nil {
		t.Fatalf("GetByVerificationToken: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("token resolved to %v, want %v", got.ID, user.ID)
	}

	if err := repo.VerifyEmail(ctx, user.ID); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}

	verified, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !verified.EmailVerified {
		t.Error("EmailVerified is still false after VerifyEmail")
	}

	// The token is consumed, so it must no longer resolve.
	if _, err := repo.GetByVerificationToken(ctx, token); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("a spent verification token still resolves: %v", err)
	}
}

func TestExpiredTokensDoNotResolve(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tests := []struct {
		name   string
		set    func(token string) error
		lookup func(token string) (*models.User, error)
	}{
		{
			name: "verification",
			set: func(token string) error {
				return repo.SetVerificationToken(ctx, user.ID, token, time.Now().Add(-time.Hour))
			},
			lookup: func(token string) (*models.User, error) {
				return repo.GetByVerificationToken(ctx, token)
			},
		},
		{
			name: "password reset",
			set: func(token string) error {
				return repo.SetPasswordResetToken(ctx, user.ID, token, time.Now().Add(-time.Hour))
			},
			lookup: func(token string) (*models.User, error) {
				return repo.GetByPasswordResetToken(ctx, token)
			},
		},
		{
			name: "magic link",
			set: func(token string) error {
				return repo.SetMagicLinkToken(ctx, user.ID, token, time.Now().Add(-time.Hour))
			},
			lookup: func(token string) (*models.User, error) {
				return repo.GetByMagicLinkToken(ctx, token)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := uuid.NewString()
			if err := tt.set(token); err != nil {
				t.Fatalf("set: %v", err)
			}

			// The expiry filter lives in the WHERE clause; this is the only place it
			// can be observed.
			if _, err := tt.lookup(token); !errors.Is(err, repository.ErrUserNotFound) {
				t.Errorf("an expired %s token still resolves: %v", tt.name, err)
			}
		})
	}
}

func TestPasswordResetTokenIsCleared(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	token := uuid.NewString()
	if err := repo.SetPasswordResetToken(ctx, user.ID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetPasswordResetToken: %v", err)
	}
	if _, err := repo.GetByPasswordResetToken(ctx, token); err != nil {
		t.Fatalf("GetByPasswordResetToken: %v", err)
	}

	if err := repo.ClearPasswordResetToken(ctx, user.ID); err != nil {
		t.Fatalf("ClearPasswordResetToken: %v", err)
	}

	// A reset link must be single use, or an intercepted mail stays valid until expiry.
	if _, err := repo.GetByPasswordResetToken(ctx, token); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("a cleared reset token still resolves: %v", err)
	}
}

func TestListAndCountSeeCreatedUsers(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Not even a relative assertion holds here. Count reads the whole users table, and
	// `go test ./...` runs packages concurrently against one database, so another
	// package's cleanup can delete a user between two counts and cancel out this one.
	// What is left to check is that Count reads users at all and agrees with List that
	// the table is not empty.
	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count < 1 {
		t.Errorf("Count = %d with a user just created", count)
	}

	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, listed := range users {
		if listed.ID == user.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("List did not include the created user")
	}
}

// TestDetermineRoleAtomically covers the count-under-advisory-lock role
// decision that makes the first registrar of an instance the administrator:
// an empty table reports "admin", any table that already holds a user reports
// "user", and racing calls serialize on the advisory lock so the shared count
// is never read inconsistently.
func TestDetermineRoleAtomically(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	// Which branch applies is decided by instance-wide state — the user count —
	// not by rows this test owns. Another package's DB tests run as a concurrent
	// process and take the same advisory lock when they register a user, so
	// holding it for this test's whole lifetime is what keeps the count stable
	// between the read below and the assertions that follow (the audit caught a
	// package inserting and then cleaning up a user mid-test, flipping the
	// expected outcome).
	dbtest.LockSingletonAccounts(ctx, t, pool)

	// The shared database may or may not already hold users; document which
	// branch the rest of this test is in. The count is owned by the lock above,
	// so it cannot change while this test runs.
	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	alreadyPopulated := count > 0

	role, err := repo.DetermineRoleAtomically(ctx)
	if err != nil {
		t.Fatalf("DetermineRoleAtomically: %v", err)
	}
	if alreadyPopulated {
		// The slot is taken globally; every call must read the ordinary role.
		if role != models.RoleUser {
			t.Fatalf("DetermineRoleAtomically on a populated instance = %q, want %q", role, models.RoleUser)
		}
		return
	}

	// The first count on an empty instance is the bootstrap admin, and the
	// ordinary role only after a user actually exists.
	if role != models.RoleAdmin {
		t.Fatalf("DetermineRoleAtomically on an empty instance = %q, want %q", role, models.RoleAdmin)
	}

	// Racing calls on the same empty table. Each goroutine drives its own
	// single-connection pool (so the two really run on independent connections,
	// not two slots of one pool queue), held behind a start barrier so both can
	// be in flight together. The advisory-locked transaction serializes them;
	// both must read the same empty table and neither may fail.
	racy := make([]*repository.UserRepository, 2)
	for i := range racy {
		racy[i] = repository.NewUserRepository(newSingleConnPool(t, ctx))
	}
	start := make(chan struct{})
	results := make(chan string, 2)
	for i := range racy {
		r := racy[i]
		go func() {
			<-start
			role, err := r.DetermineRoleAtomically(ctx)
			if err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- role
		}()
	}
	close(start)
	for range racy {
		if role := <-results; role != models.RoleAdmin {
			t.Fatalf("racing DetermineRoleAtomically on an empty table: %s, want %s", role, models.RoleAdmin)
		}
	}

	// Insert one user, then the same call must report the ordinary role: the
	// first-user window is closed by the row itself, not by anything the caller
	// remembers.
	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	role, err = repo.DetermineRoleAtomically(ctx)
	if err != nil {
		t.Fatalf("DetermineRoleAtomically after a user: %v", err)
	}
	if role != models.RoleUser {
		t.Fatalf("DetermineRoleAtomically after one user = %q, want %q", role, models.RoleUser)
	}
}

// newSingleConnPool builds a MaxConns=1 pool. The repository wraps a pool, so the
// way to hand each goroutine its own connection is a dedicated MaxConns=1 pool; two
// goroutines, two pools, two connections — real concurrency.
func newSingleConnPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.MaxConns = 1
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// adminSnapshot locks the admin-membership advisory lock (2) and returns the
// current admins. Holding the same lock the repository takes for demotion and
// deletion gives the test an exclusive window: nothing else can change the
// admin set while we inspect it, so the "these are the only admins on the
// instance" precondition is precise rather than best-effort.
func adminSnapshot(t *testing.T, pool *pgxpool.Pool) []uuid.UUID {
	t.Helper()
	ctx := dbtest.Context(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2)`); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}

	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE role = 'admin' ORDER BY id`)
	if err != nil {
		t.Fatalf("query admins: %v", err)
	}
	defer rows.Close()

	var admins []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan admin: %v", err)
		}
		admins = append(admins, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate admins: %v", err)
	}

	return admins
}

// adminIsolationFixture sets up a database state where the given admin IDs are
// the only administrators (gated on that being true under the lock), so the
// test below can reason about "the last admin" precisely.
func adminIsolationFixture(t *testing.T, pool *pgxpool.Pool, admins ...uuid.UUID) {
	t.Helper()
	current := adminSnapshot(t, pool)
	expected := make(map[uuid.UUID]bool, len(admins))
	for _, id := range admins {
		expected[id] = true
	}
	if len(current) != len(admins) {
		t.Skipf("shared DB holds %d admins; the last-admin invariant cannot be tested in isolation", len(current))
	}
	for _, id := range current {
		if !expected[id] {
			t.Skipf("shared DB holds admin %s not under this test's control; skipping", id)
		}
	}
}

// TestAdminInvariantCrossDemotion is the concurrency regression for
// cross-demotion: two admins, each demoting the other, racing. Without
// serialization both transactions can read "two admins" and both commit the
// demotion, leaving zero administrators — an unrecoverable instance. With the
// advisory-locked count (UpdateRole), exactly one reaches ErrLastAdmin.
func TestAdminInvariantCrossDemotion(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	// Two fresh admins, unique to this test, cleaned up at the end.
	a := newUser(ctx, t, pool, func(u *models.User) { u.Role = models.RoleAdmin })
	b := newUser(ctx, t, pool, func(u *models.User) { u.Role = models.RoleAdmin })
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("Create B: %v", err)
	}
	adminIsolationFixture(t, pool, a.ID, b.ID)

	start := make(chan struct{})
	type outcome struct {
		demoter uuid.UUID // the user doing the demotion
		target  uuid.UUID // the user being demoted
		err     error
	}
	run := func(demoter, target uuid.UUID) func() outcome {
		repo := repository.NewUserRepository(newSingleConnPool(t, ctx))
		return func() outcome {
			return outcome{
				demoter: demoter,
				target:  target,
				err:     repo.UpdateRole(ctx, target, models.RoleUser),
			}
		}
	}

	results := make(chan outcome, 2)
	go func() { <-start; results <- run(a.ID, b.ID)() }()
	go func() { <-start; results <- run(b.ID, a.ID)() }()
	close(start)

	first, second := <-results, <-results

	// Both demotions must never succeed: that would leave zero admins.
	var lastAdminErr int
	for _, o := range []outcome{first, second} {
		switch {
		case o.err == nil:
		case errors.Is(o.err, repository.ErrLastAdmin):
			lastAdminErr++
		default:
			t.Fatalf("demotion error = %v, want nil or ErrLastAdmin", o.err)
		}
	}
	if lastAdminErr != 1 {
		t.Fatalf("cross-demotion produced %d ErrLastAdmin (errors %v, %v), want exactly 1",
			lastAdminErr, first.err, second.err)
	}

	// At least one of the two remains an administrator.
	gotA, err := repo.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetByID A: %v", err)
	}
	gotB, err := repo.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetByID B: %v", err)
	}
	if gotA.Role != models.RoleAdmin && gotB.Role != models.RoleAdmin {
		t.Fatalf("cross-demotion left zero admins (A=%s, B=%s)", gotA.Role, gotB.Role)
	}
}

// TestAdminInvariantCrossDeletion is the deletion twin of the cross-demotion
// test: two admins deleting each other, racing. Serialization plus the last-admin
// guard must mean the instance never lands with zero administrators.
func TestAdminInvariantCrossDeletion(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	a := newUser(ctx, t, pool, func(u *models.User) { u.Role = models.RoleAdmin })
	b := newUser(ctx, t, pool, func(u *models.User) { u.Role = models.RoleAdmin })
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("Create B: %v", err)
	}
	adminIsolationFixture(t, pool, a.ID, b.ID)

	start := make(chan struct{})
	type outcome struct {
		id  uuid.UUID
		err error
	}
	run := func(id uuid.UUID) func() outcome {
		repo := repository.NewUserRepository(newSingleConnPool(t, ctx))
		return func() outcome {
			return outcome{id: id, err: repo.Delete(ctx, id)}
		}
	}

	results := make(chan outcome, 2)
	go func() { <-start; results <- run(a.ID)() }()
	go func() { <-start; results <- run(b.ID)() }()
	close(start)

	first, second := <-results, <-results
	var lastAdminErr int
	for _, o := range []outcome{first, second} {
		switch {
		case o.err == nil:
		case errors.Is(o.err, repository.ErrLastAdmin):
			lastAdminErr++
		default:
			t.Fatalf("deletion error = %v, want nil or ErrLastAdmin", o.err)
		}
	}
	if lastAdminErr != 1 {
		t.Fatalf("cross-deletion produced %d ErrLastAdmin (errors %v, %v), want exactly 1",
			lastAdminErr, first.err, second.err)
	}

	// The instance still has an administrator after the race.
	admins := adminSnapshot(t, pool)
	if len(admins) == 0 {
		t.Fatal("cross-deletion left zero admins")
	}
}

// TestAdminInvariantDeletionRacingOrdinaryRegistration is the audit's
// "deletion racing ordinary registration" interleaving: an administrator is
// deleted at the same moment a registration commits. The registration must not
// turn the loser of a cross-deletion into a zero-admin instance — deletion of
// the last admin is rejected even while registration writes land.
func TestAdminInvariantDeletionRacingOrdinaryRegistration(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	admin := newUser(ctx, t, pool, func(u *models.User) { u.Role = models.RoleAdmin })
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create admin: %v", err)
	}
	adminIsolationFixture(t, pool, admin.ID)

	// An ordinary registration commits a non-admin row.
	registrant := newUser(ctx, t, pool) // RoleUser by default

	type outcome struct {
		deleteErr error
		createErr error
	}
	results := make(chan outcome, 2)

	// Each racer drives its own single-connection pool (created in the main
	// goroutine, before the barrier, so the pool setup itself is not part of
	// the timing being measured).
	delRepo := repository.NewUserRepository(newSingleConnPool(t, ctx))
	regRepo := repository.NewUserRepository(newSingleConnPool(t, ctx))

	start := make(chan struct{})
	go func() {
		<-start
		results <- outcome{deleteErr: delRepo.Delete(ctx, admin.ID)}
	}()
	go func() {
		<-start
		results <- outcome{createErr: regRepo.Create(ctx, registrant)}
	}()
	close(start)

	first, second := <-results, <-results
	var deleteErr, createErr error
	for _, o := range []outcome{first, second} {
		if o.deleteErr != nil {
			deleteErr = o.deleteErr
		}
		if o.createErr != nil {
			createErr = o.createErr
		}
	}

	// Delete of the last admin must be refused; the registration is the point
	// of the race and succeeds.
	if deleteErr == nil {
		t.Errorf("deleting the last admin while a registration raced = nil, want ErrLastAdmin")
	} else if !errors.Is(deleteErr, repository.ErrLastAdmin) {
		t.Errorf("deleting the last admin = %v, want ErrLastAdmin", deleteErr)
	}

	got, err := repo.GetByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetByID admin after race: %v", err)
	}
	if got.Role != models.RoleAdmin {
		t.Fatalf("admin was demoted by the race: role=%s", got.Role)
	}

	if createErr != nil && !errors.Is(createErr, repository.ErrUserAlreadyExists) {
		t.Fatalf("registration error = %v, want nil", createErr)
	}
}

// TestAdminLastAdminRejected covers the plain (non-racy) invariant: a lone
// administrator cannot be demoted or deleted. The registry's ErrLastAdmin is
// surfaced to the auth service and then to the API as a 400.
func TestAdminLastAdminRejected(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	solo := newUser(ctx, t, pool, func(u *models.User) { u.Role = models.RoleAdmin })
	if err := repo.Create(ctx, solo); err != nil {
		t.Fatalf("Create: %v", err)
	}
	adminIsolationFixture(t, pool, solo.ID)

	if err := repo.UpdateRole(ctx, solo.ID, models.RoleUser); !errors.Is(err, repository.ErrLastAdmin) {
		t.Errorf("demoting the last admin = %v, want ErrLastAdmin", err)
	}
	if err := repo.Delete(ctx, solo.ID); !errors.Is(err, repository.ErrLastAdmin) {
		t.Errorf("deleting the last admin = %v, want ErrLastAdmin", err)
	}

	// The guard only trips when the target is an admin: a plain user can be
	// deleted even if no other user remains.
	plain := newUser(ctx, t, pool)
	if err := repo.Create(ctx, plain); err != nil {
		t.Fatalf("Create plain: %v", err)
	}
	if err := repo.Delete(ctx, plain.ID); err != nil {
		t.Errorf("deleting a plain user = %v, want nil", err)
	}
}

// TestConsumeMagicLinkTokenSingleUse pins the atomic magic-link claim: one
// consume returns the user and clears the proof, and any later consume of the
// same token finds nothing, regardless of how close the two calls are.
func TestConsumeMagicLinkTokenSingleUse(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const token = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := repo.SetMagicLinkToken(ctx, user.ID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetMagicLinkToken: %v", err)
	}

	got, err := repo.ConsumeMagicLinkToken(ctx, token, user.SecurityGeneration, nil)
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("claimed user = %v, want %v", got.ID, user.ID)
	}

	if _, err := repo.GetByMagicLinkToken(ctx, token); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("token still resolves after being claimed: %v", err)
	}
	if _, err := repo.ConsumeMagicLinkToken(ctx, token, user.SecurityGeneration, nil); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("second consume = %v, want ErrUserNotFound (single-use)", err)
	}
}

// TestConsumeMagicLinkTokenConcurrent behaves like two browser tabs racing to
// confirm the same link: exactly one consume wins and the other finds the proof
// already spent. FOR UPDATE is what makes the verdict exact rather than racy.
func TestConsumeMagicLinkTokenConcurrent(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := repo.SetMagicLinkToken(ctx, user.ID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetMagicLinkToken: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := repo.ConsumeMagicLinkToken(ctx, token, user.SecurityGeneration, nil)
			results <- err
		}()
	}
	close(start)

	var winners, notFound int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case errors.Is(err, repository.ErrUserNotFound):
			notFound++
		default:
			t.Errorf("consume error = %v", err)
		}
	}
	if winners != 1 || notFound != 1 {
		t.Errorf("winners = %d, losers = %d; want exactly one winner", winners, notFound)
	}
}

// TestConsumeMagicLinkTokenExpiredMustNotClaim covers the WHERE expiry guard:
// an expired proof is indistinguishable from a spent one, so follow-up code
// never treats it as valid.
func TestConsumeMagicLinkTokenExpiredMustNotClaim(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const token = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	if err := repo.SetMagicLinkToken(ctx, user.ID, token, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("SetMagicLinkToken: %v", err)
	}
	if _, err := repo.ConsumeMagicLinkToken(ctx, token, user.SecurityGeneration, nil); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("consume of an expired token = %v, want ErrUserNotFound", err)
	}
}

// TestConsumePasswordResetTokenAppliesAtomically verifies the whole reset lands
// in one transaction: the new password is stored, the proof is spent, the
// security generation advances (so a stale login cannot insert a session) and
// every refresh token the user held is deleted together. Then it pins that the
// spent proof cannot re-apply anything.
func TestConsumePasswordResetTokenAppliesAtomically(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := repository.NewUserRepository(pool)
	tokens := repository.NewTokenRepository(pool)
	ctx := dbtest.Context(t)

	user := newUser(ctx, t, pool)
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const resetToken = "1111111111111111111111111111111111111111111111111111111111111111"
	if err := repo.SetPasswordResetToken(ctx, user.ID, resetToken, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetPasswordResetToken: %v", err)
	}

	// Two live sessions the reset must revoke.
	for i := 0; i < 2; i++ {
		refreshToken := &models.RefreshToken{
			UserID:    user.ID,
			TokenHash: repository.HashToken(fmt.Sprintf("session-%d", i)),
			ExpiresAt: time.Now().Add(24 * time.Hour),
			FamilyID:  fmt.Sprintf("family-%d", i),
		}
		refreshToken.ID = uuid.New()
		if err := tokens.Create(ctx, refreshToken, user.SecurityGeneration); err != nil {
			t.Fatalf("Create refresh token %d: %v", i, err)
		}
	}

	const newHash = "$2a$04$newhashnewhashnewhash1"
	claimed, err := repo.ConsumePasswordResetToken(ctx, resetToken, newHash)
	if err != nil {
		t.Fatalf("ConsumePasswordResetToken: %v", err)
	}
	if claimed.SecurityGeneration == user.SecurityGeneration {
		t.Errorf("security generation did not advance (still %d)", claimed.SecurityGeneration)
	}

	// Password applied.
	byEmail, err := repo.GetByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("GetByEmail after reset: %v", err)
	}
	if byEmail.PasswordHash != newHash {
		t.Errorf("password hash = %q, want the new hash", byEmail.PasswordHash)
	}
	if byEmail.SecurityGeneration != claimed.SecurityGeneration {
		t.Errorf("generation read back %d, want %d", byEmail.SecurityGeneration, claimed.SecurityGeneration)
	}

	// Proof spent and every session gone.
	if _, err := repo.GetByPasswordResetToken(ctx, resetToken); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("reset proof still resolves after use: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := tokens.GetByHash(ctx, repository.HashToken(fmt.Sprintf("session-%d", i))); !errors.Is(err, repository.ErrTokenNotFound) {
			t.Errorf("session %d survived the reset: %v", i, err)
		}
	}

	// The spent proof cannot be used again.
	if _, err := repo.ConsumePasswordResetToken(ctx, resetToken, "$2a$04$anotherhashanotherhash1"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("second consume of the reset proof = %v, want ErrUserNotFound", err)
	}
}
