# Database Migrations

WhenTo uses a conditional migration system to support both Cloud (SaaS) and Self-hosted deployments.

## Structure

```
migrations/
├── common/          # Migrations applied to both cloud and selfhosted
│   └── 001_init.*   # Initial schema (users, calendars, etc.)
├── cloud/           # Cloud-only migrations
│   ├── 005_ecommerce.*
│   ├── 011_order_shop_session.*
│   └── 013_drop_billing.*
└── selfhosted/      # Self-hosted only migrations
    ├── 005_licenses.*
    └── 013_drop_licensing.*
```

Both variant directories now exist largely to undo themselves. Licensing, subscriptions
and payments were removed from the product, and `013_drop_billing` / `013_drop_licensing`
are what take the tables out of a database that already ran the earlier ones. Neither
variant has anything left to add on top of `common/`.

## How It Works

### Docker Builds

During Docker build, the `scripts/build-migrations.sh` script combines the appropriate migrations:

- **Cloud build**: `common` + `cloud` migrations
- **Self-hosted build**: `common` + `selfhosted` migrations

The result is placed in `/app/migrations` inside the container.

### Local Development

Use Makefile commands with `BUILD_TYPE` environment variable:

```bash
# Self-hosted migrations (default)
make migrate-up

# Cloud migrations
BUILD_TYPE=cloud make migrate-up

# Check status
BUILD_TYPE=cloud make migrate-status

# Rollback
BUILD_TYPE=selfhosted make migrate-down
```

### All Migrations (Legacy)

If you need to apply all migrations (not recommended for production):

```bash
make migrate-up-all
make migrate-status-all
```

## Adding New Migrations

### Common Migration (both builds)

```bash
# Create migration files
bash scripts/migrate.sh create common migration_name
```

### Cloud-only Migration

```bash
# Create migration files
bash scripts/migrate.sh create cloud migration_name
```

### Self-hosted-only Migration

```bash
# Create migration files
bash scripts/migrate.sh create selfhosted migration_name
```

## Migration Naming

- **Common**: `001_init`, `008_notification_log`, `012_unified_ics_feed`, `014_availability_index_cleanup`, `015_refresh_token_grace_window`, `016_refresh_token_family`, `017_security_generation`, `018_mfa_pending_nonce`, `019_app_state`, `020_reminder_jobs`
- **Cloud**: `005_ecommerce`, `011_order_shop_session`, `013_drop_billing`
- **Self-hosted**: `005_licenses`, `013_drop_licensing`

> **Note**: the numbering space is shared, but only one variant directory is ever copied
> into a build, so a number may be reused between `cloud/` and `selfhosted/` — `005` and
> `013` both are. It must stay unique against `common/`.

## Choosing the next migration number

Never hard-code "the next version" by hand: compute it from the actual assembled
chain, so the answer stays correct as the inventory grows.

```bash
# Highest number across all source directories (decimal, even with zero padding).
next_migration_num() {
    find migrations/common migrations/selfhosted migrations/cloud \
        -name '*.sql' -printf '%f\n' 2>/dev/null \
        | sed -E 's/^([0-9]+).*/\1/' \
        | sort -n | tail -1
}
highest=$(next_migration_num)
echo "Next free version: $((10#$highest + 1))"
```
`scripts/migrate.sh create <variant> <name>` also picks the next number for you
by seeding a private staging directory with the global numeric maximum before
calling golang-migrate's `-seq`. Calling `migrate create -seq` directly in a
variant directory is unsafe: it ignores the common chain. The wrapper serializes
creation and publishes the new pair without overwriting existing files or
symlinks. If a creator is forcibly killed, inspect and remove the empty
`migrations/.create-lock` directory before retrying. The check above is just a
sanity read for reviews.

> These numbers apply to **this repository's chain**. The fork that preceded this
> split used its own renumbered history; do not copy fork numbers into this
> README or expect them to match upstream's published chain.

## Testing

Test the build script manually (it refuses to write into a non-empty directory;
use a fresh scratch dir):

```bash
# Test cloud build into a fresh scratch dir
scratch=$(mktemp -d)
bash scripts/build-migrations.sh cloud "$scratch"

# Test selfhosted build into another fresh scratch dir
scratch2=$(mktemp -d)
bash scripts/build-migrations.sh selfhosted "$scratch2"
```
The safe path to actually apply (or reset) migrations is the guarded wrapper:
`scripts/migrate.sh up|status|down|reset` (see its `--help`), which owns its own
scratch directory and requires typed database confirmation (or matching
noninteractive flags) for `reset`. `down [N]` is an explicitly destructive
rollback command; it does not prompt for confirmation.

Run the script-level and dotenv regression checks with:

```bash
bash scripts/test-migrations.sh
```

For real PostgreSQL acceptance, use a disposable server whose connection user
can create databases. Both `psql` and `migrate` must be on PATH (or supply
`MIGRATE_BIN`). Missing tools fail rather than skip when integration is requested:

```bash
DISPOSABLE_DB_URL='postgres://test:test@localhost:5432/whento_test?sslmode=disable' \
  bash scripts/test-migrations.sh
```

The harness creates four randomly named databases and drops only those databases
on exit; it never resets the database named in `DISPOSABLE_DB_URL`. Each build
variant is checked at version 5 to prove the selected licensing/billing schema,
then migrated to the current version. Upgrade cases start at version 15 with
sentinel user/calendar records and verify preservation and bootstrap backfill.

## Local environment syntax

Migration scripts read `.env` from the working directory as data, not executable
shell code. Existing environment variables take precedence. File assignments
may have leading whitespace, optional `export`, or spaces around `=`; duplicate
keys use the last assignment. Single-quoted values are literal. Double-quoted
values support `\n`, `\r`, `\t`, `\"`, `\\`, and `\$`; other escapes are preserved.
Unquoted values may have a whitespace-prefixed `#` comment. Quoted values may be
followed by whitespace and a comment. Variable/command expansion and multiline
values are deliberately unsupported. Malformed input fails without printing
values or partially exporting the file.

An explicit `DATABASE_URL` is passed unchanged; otherwise credentials and the
database name from `DB_*` are UTF-8 byte-wise percent-encoded. Logged PostgreSQL
URIs redact userinfo and query-string `password`/`sslpassword` values.
