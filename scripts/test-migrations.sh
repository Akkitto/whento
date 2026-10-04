#!/bin/bash
# WhenTo - Collaborative event calendar for self-hosted environments
# Copyright (C) 2025 WhenTo Contributors
# SPDX-License-Identifier: BSL-1.1

# test-migrations.sh — script-level acceptance for the migration wrapper.
#
# Uses FAKE `migrate`/`psql` executables on a temporary PATH to verify the
# safety contract without touching a real database: one canonical DATABASE_URL,
# one valid scratch -path with correct contents, no shared-build cleanup, and
# consent-before-destructive behavior. Run with:
#
#   bash scripts/test-migrations.sh
#
# A real integration section runs when DISPOSABLE_DB_URL is set. It requires
# psql, migrate, and CREATE DATABASE permission on that disposable server. Four
# randomly named databases are created and dropped; the supplied DB is untouched.
#
#   MIGRATE_BIN=/path/to/migrate \
#   DISPOSABLE_DB_URL='postgres://...' bash scripts/test-migrations.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/whento-migtest.XXXXXX")"
created_databases=()
cleanup() {
    local database
    for database in "${created_databases[@]}"; do
        psql "$DISPOSABLE_DB_URL" -v ON_ERROR_STOP=1 -qc "DROP DATABASE \"$database\" WITH (FORCE)" || true
    done
    rm -rf -- "$WORK"
}
trap cleanup EXIT
mkdir -p "$WORK/bin"

PASS=0
FAIL=0
failures=()

ok() { PASS=$((PASS + 1)); }
bad() { FAIL=$((FAIL + 1)); failures+=("$1"); }

check() { # check <description> <command...>
    local desc="$1"
    shift
    local output="$WORK/check-$((PASS + FAIL + 1)).log"
    if "$@" >"$output" 2>&1; then
        ok
    else
        bad "FAIL: $desc"
        echo "  $desc" >&2
        cat "$output" >&2
    fi
}

# ---------------------------------------------------------------- fake tools

# Fake migrate records every invocation and enforces the wrapper contract:
# exactly one -path/-database pair, -path pointing at an assembled chain.
FAKE_LOG="$WORK/fake-migrate.log"
: > "$FAKE_LOG"

cat > "$WORK/bin/migrate" <<'EOF'
#!/bin/bash
echo "migrate $*" >> "$FAKE_LOG"
orig_args="$*"   # keep before the shift loop below empties $@
path=""
database=""
while [ $# -gt 0 ]; do
    case "$1" in
        -path) path="$2"; shift 2 ;;
        -database) database="$2"; shift 2 ;;
        *) shift ;;
    esac
done
[ -n "$path" ] || { echo "fake migrate: missing -path" >&2; exit 9; }
[ -n "$database" ] || { echo "fake migrate: missing -database" >&2; exit 9; }

# -path must hold an assembled chain: common + at most one variant (never a mix).
common=$(find "$path" -maxdepth 1 -name '*.sql' ! -name '005_*' ! -name '011_*' ! -name '013_*' 2>/dev/null | wc -l)
cloud=$(ls "$path"/005_ecommerce.up.sql "$path"/005_ecommerce.down.sql "$path"/011_order_shop_session.up.sql "$path"/013_drop_billing.up.sql 2>/dev/null | wc -l)
self=$(ls "$path"/005_licenses.up.sql "$path"/005_licenses.down.sql "$path"/013_drop_licensing.up.sql 2>/dev/null | wc -l)
# Count files that would only exist if BOTH variants were copied.
mix=0
for f in 005_licenses.up.sql 005_ecommerce.up.sql; do
    [ -f "$path/$f" ] && mix=$((mix + 1))
done
[ "$mix" -ge 2 ] && { echo "fake migrate: mixed variant chain" >&2; exit 9; }
[ "$common" -gt 0 ] || { echo "fake migrate: empty chain" >&2; exit 9; }

# Simulated behaviors used by the reset/status tests. `version` is detected from
# the whole argument list (migrate is invoked as `-path P -database D version`).
case "${FAKE_MIGRATE_MODE:-normal}" in
    no-version)
        printf '%s' "$orig_args" | grep -q -e "version" && { echo "error: no migration" >&2; exit 1; }
        ;;
    version-error)
        printf '%s' "$orig_args" | grep -q -e "version" && { echo "error: boom" >&2; exit 2; }
        ;;
    down-fail)
        printf '%s\n' "$orig_args" | grep -q 'down' && { echo "error: down failed" >&2; exit 1; }
        ;;
esac
exit 0
EOF
chmod +x "$WORK/bin/migrate"

# Fake psql answers the read-only consent/identity probes on STDOUT, which is what
# the wrapper captures. PASS_PSQL_AS_DB / PASS_PSQL_AS_IDENTITY override the
# answers so the fake can emulate "the connected db is X".
cat > "$WORK/bin/psql" <<'EOF'
#!/bin/bash
url="$1"
dbname="${url##*/}"
dbname="${dbname%%\?*}"
[ -n "${PASS_PSQL_AS_DB:-}" ] && dbname="$PASS_PSQL_AS_DB"
# If the SQL text asks for current_database() just echo the dbname.
if printf '%s' "$*" | grep -q 'current_database() ||'; then
    printf '%s\n' "${PASS_PSQL_AS_IDENTITY:-$dbname|whento|5432}"
elif printf '%s' "$*" | grep -q 'current_database'; then
    printf '%s\n' "$dbname"
fi
exit 0
EOF
chmod +x "$WORK/bin/psql"

MIG="./scripts/migrate.sh"
cd "$REPO_ROOT"

# ------------------------------------------------------------- bash -n + tabs
for f in scripts/migrate.sh scripts/init-db.sh scripts/build-migrations.sh scripts/migration-common.sh; do
    check "bash -n $f" bash -n "$f"
done
check "build-migrations.sh is sh -n clean" sh -n scripts/build-migrations.sh
check "dotenv, encoding and credential-redaction regressions" bash scripts/test-migration-helpers.sh

# ------------------------------------------------------ one canonical URL
FAKE_URL="postgres://u:pass@host:5432/db?sslmode=disable"

# wrap runs the wrapper through env -i so no test env leaks in, while still
# handing the fake tools the two variables they need (FAKE_LOG, WORK) and the
# canonical URL. Extra VAR=VAL pairs may be passed as leading arguments.
wrap() {
    env -i \
        PATH="$WORK/bin:/usr/bin:/bin" \
        HOME="$HOME" \
        FAKE_LOG="$FAKE_LOG" \
        WORK="$WORK" \
        DATABASE_URL="$FAKE_URL" \
        "$@"
}

# 1. up passes exactly one -path/-database and a valid chain
: > "$FAKE_LOG"
wrap bash "$MIG" up >/dev/null 2>&1
check "up produced one migrate invocation with -path" test "$(grep -c 'migrate -path' "$FAKE_LOG")" = "1"
check "up passes one -database" test "$(grep -o -- '-database[^ ]*' "$FAKE_LOG")" != ""

# 2. status clean run exits 0
: > "$FAKE_LOG"
set +e
wrap bash "$MIG" status >/dev/null 2>&1
s_rc=$?
set -e
check "status exits 0 on a readable version" test "$s_rc" = "0"

# 3. reset without consent (non-TTY) makes zero down -all
: > "$FAKE_LOG"
set +e
wrap bash "$MIG" reset </dev/null >/dev/null 2>&1
rc=$?
set -e
check "reset without consent rejects" test "$rc" != "0"
check "reset without consent made zero down -all" test "$(grep -c 'down -all' "$FAKE_LOG")" = "0"

# 4. reset with --yes but wrong confirm-database is rejected
: > "$FAKE_LOG"
set +e
wrap PASS_PSQL_AS_DB=db bash "$MIG" reset --yes --confirm-database wrong </dev/null >/dev/null 2>&1
rc=$?
set -e
check "reset with wrong confirm-database rejects" test "$rc" != "0"
check "reset with wrong confirm-database made zero down -all" test "$(grep -c 'down -all' "$FAKE_LOG")" = "0"

# 5. reset with matching --yes runs down -all then up
: > "$FAKE_LOG"
wrap PASS_PSQL_AS_DB=db bash "$MIG" reset --yes --confirm-database db </dev/null >/dev/null 2>&1 \
    || true
check "authorized reset invokes down -all" test "$(grep -c 'down -all' "$FAKE_LOG")" = "1"
check "authorized reset invokes up" test "$(grep -c ' up$' "$FAKE_LOG")" = "1"

# 6. invalid build type must not wipe a caller-supplied broad output
mkdir -p "$WORK/precious"
echo kept > "$WORK/precious/data"
set +e
env -i PATH="$WORK/bin:/usr/bin:/bin" bash scripts/build-migrations.sh bogus "$WORK/precious" >/dev/null 2>&1
rc=$?
set -e
check "invalid build type rejected" test "$rc" != "0"
check "invalid build type did not wipe output dir" test -f "$WORK/precious/data"

# 7. builder refuses a non-empty output dir (never deletes)
set +e
env -i PATH="$WORK/bin:/usr/bin:/bin" bash scripts/build-migrations.sh selfhosted "$WORK/precious" >/dev/null 2>&1
rc=$?
set -e
check "builder refuses non-empty output dir" test "$rc" != "0"
check "non-empty output dir left intact" test -f "$WORK/precious/data"

# 8. valid type assembles a cloud chain with no mixed variants
cscratch="$(mktemp -d "$WORK/cloud.XXXXXX")"
env -i PATH="$WORK/bin:/usr/bin:/bin" bash scripts/build-migrations.sh cloud "$cscratch" >/dev/null 2>&1
check "cloud build assembles" test -f "$cscratch/001_init.up.sql"
check "cloud build has no selfhosted-only files" test ! -f "$cscratch/005_licenses.up.sql"
rm -rf -- "$cscratch"

# 9. concurrent wrapper invocations use distinct scratch dirs (no shared build)
: > "$FAKE_LOG"
( wrap bash "$MIG" up >/dev/null 2>&1 ) & J1=$!
( wrap bash "$MIG" up >/dev/null 2>&1 ) & J2=$!
wait "$J1" || true
wait "$J2" || true
paths="$(grep -oE -- '-path [^ ]*' "$FAKE_LOG" | sort -u)"
count="$(printf '%s\n' "$paths" | wc -l)"
check "two concurrent invocations use distinct scratch dirs" test "$count" = "2"

# 10. create passes the right variant directory and name to golang-migrate
: > "$FAKE_LOG"
wrap bash "$MIG" create selfhosted smoke_test >/dev/null 2>&1 || true
check "create passes -dir migrations/selfhosted" grep -q 'create -ext sql -dir migrations/selfhosted -seq smoke_test' "$FAKE_LOG"

# 11. unset DEVCONTAINER and explicit DATABASE_URL vs contradictory DB_*: the
# wrapper must use the explicit URL, never build from DB_*.
: > "$FAKE_LOG"
wrap DB_HOST=contradictory DB_NAME=wrong bash "$MIG" up >/dev/null 2>&1
check "explicit DATABASE_URL wins over DB_*" grep -q "$FAKE_URL" "$FAKE_LOG"

# 12. create variant validation
set +e
wrap bash "$MIG" create invalid name >/dev/null 2>&1
rc=$?
set -e
check "create rejects a bad variant" test "$rc" != "0"

# 13. reset on an instance that was never initialized skips down -all
: > "$FAKE_LOG"
set +e
wrap PASS_PSQL_AS_DB=db FAKE_MIGRATE_MODE=no-version \
    bash "$MIG" reset --yes --confirm-database db </dev/null >/dev/null 2>&1
rc=$?
set -e
check "reset on never-initialized DB exits 0" test "$rc" = "0"
check "reset on never-initialized DB made zero down -all" test "$(grep -c 'down -all' "$FAKE_LOG")" = "0"
check "reset on never-initialized DB still runs up" test "$(grep -c ' up$' "$FAKE_LOG")" = "1"

# 14. status reports no-version with exit 0 but propagates a real version error
: > "$FAKE_LOG"
set +e
wrap FAKE_MIGRATE_MODE=no-version bash "$MIG" status >"$WORK/status-nov.log" 2>&1
s_rc=$?
set -e
check "status no-version exits 0" test "$s_rc" = "0"
check "status no-version message mentions nil" grep -q "No migrations applied yet" "$WORK/status-nov.log"

: > "$FAKE_LOG"
set +e
wrap FAKE_MIGRATE_MODE=version-error bash "$MIG" status >"$WORK/status-err.log" 2>&1
s_rc=$?
set -e
check "status real error exits nonzero" test "$s_rc" != "0"

# ------------------------------------------------------------- real-disposable DB
if [ -n "${DISPOSABLE_DB_URL:-}" ]; then
    echo
    echo "=== Real disposable-DB integration (DISPOSABLE_DB_URL set) ==="
    export MIGRATE_BIN="${MIGRATE_BIN:-migrate}"
    command -v "$MIGRATE_BIN" >/dev/null || { echo 'migrate is required for the requested integration tests' >&2; exit 1; }
    command -v psql >/dev/null || { echo 'psql is required for the requested integration tests' >&2; exit 1; }
    [[ "$DISPOSABLE_DB_URL" =~ ^postgres(ql)?://[^/]+/[^?]+(\?.*)?$ ]] || {
        echo 'DISPOSABLE_DB_URL must be a PostgreSQL URI with a database path' >&2; exit 1;
    }
    admin_base="${DISPOSABLE_DB_URL%%\?*}"
    admin_base="${admin_base%/*}"
    admin_query=""
    [[ "$DISPOSABLE_DB_URL" != *\?* ]] || admin_query="?${DISPOSABLE_DB_URL#*\?}"
    unique="${WORK##*.}_$$"
    unique="${unique,,}"

    real_variant() {
        local variant="$1" mode="$2" database="whento_mig_${unique}_${1}_${2}" uri chain opposite expected
        # Names contain only this generated prefix, ASCII letters, digits and '_'.
        [[ "$database" =~ ^whento_mig_[a-z0-9_]+$ ]] || return 1
        psql "$DISPOSABLE_DB_URL" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE \"$database\"" || return 1
        created_databases+=("$database")
        uri="$admin_base/$database$admin_query"
        chain="$(mktemp -d "$WORK/${variant}-${mode}.XXXXXX")"
        bash scripts/build-migrations.sh "$variant" "$chain" >/dev/null || return 1
        "$MIGRATE_BIN" -path "$chain" -database "$uri" goto 5 || return 1
        if [ "$variant" = selfhosted ]; then expected=licenses; opposite=subscriptions; else expected=subscriptions; opposite=licenses; fi
        [ "$(psql "$uri" -Atqc "SELECT to_regclass('public.$expected') IS NOT NULL AND to_regclass('public.$opposite') IS NULL")" = t ] || return 1
        if [ "$mode" = upgrade ]; then
            "$MIGRATE_BIN" -path "$chain" -database "$uri" goto 15 || return 1
            psql "$uri" -v ON_ERROR_STOP=1 -qc "INSERT INTO users (id,email,password_hash,display_name) VALUES ('00000000-0000-4000-8000-000000000163','migration-sentinel@example.test','not-a-login-hash','Preserved'); INSERT INTO calendars(owner_id,name) VALUES ('00000000-0000-4000-8000-000000000163','Migration sentinel')" || return 1
        fi
        DATABASE_URL="$uri" BUILD_TYPE="$variant" bash "$MIG" up || return 1
        DATABASE_URL="$uri" BUILD_TYPE="$variant" bash "$MIG" status >"$WORK/${variant}-${mode}-status.log" 2>&1 || return 1
        [ "$(psql "$uri" -Atqc "SELECT to_regclass('public.reminder_jobs') IS NOT NULL AND NOT dirty FROM schema_migrations")" = t ] || return 1
        if [ "$mode" = upgrade ]; then
            [ "$(psql "$uri" -Atqc "SELECT first_user_created FROM app_state WHERE id=1")" = t ] || return 1
            [ "$(psql "$uri" -Atqc "SELECT count(*) FROM calendars WHERE name='Migration sentinel'")" = 1 ] || return 1
            [ "$(psql "$uri" -Atqc "SELECT display_name FROM users WHERE email='migration-sentinel@example.test'")" = Preserved ] || return 1
        else
            [ "$(psql "$uri" -Atqc "SELECT first_user_created FROM app_state WHERE id=1")" = f ] || return 1
        fi
    }
    for variant in selfhosted cloud; do
        for mode in fresh upgrade; do
            check "real $variant $mode migration and variant identity" real_variant "$variant" "$mode"
        done
    done
else
    echo
    echo "=== Real integration not requested (set DISPOSABLE_DB_URL + MIGRATE_BIN) ==="
fi

echo
echo "=== Script acceptance: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
    printf '%s\n' "${failures[@]}" >&2
    exit 1
fi
