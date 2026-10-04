#!/bin/bash
# WhenTo - Collaborative event calendar for self-hosted environments
# Copyright (C) 2025 WhenTo Contributors
# SPDX-License-Identifier: BSL-1.1

# Shared helpers for the migration scripts. Source this file, never execute it:
#
#   . "$(dirname "${BASH_SOURCE[0]}")/migration-common.sh"
#
# The helpers resolve ONE canonical DATABASE_URL (see migration_resolve_database_url),
# load the trusted local .env exactly once, redact credentials for logs, and expose
# small read-only SQL probes for confirmation prompts.

# migration_load_env sources the trusted .env once. It never clobbers a value that
# is already in the environment (real env wins), and reads the file line by line so
# values with spaces, quotes or special characters are not re-split by word
# splitting.
migration_env_loaded=0
migration_load_env() {
    if [ "$migration_env_loaded" = "1" ]; then
        return
    fi
    migration_env_loaded=1

    [ -f .env ] || return 0
    local line key value
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            '' | \#*) continue ;;
        esac
        # Optional leading "export " (discarded; we export explicitly below).
        if [[ "$line" == export[[:space:]]* ]]; then
            line="${line#export }"
        fi
        key="${line%%=*}"
        value="${line#*=}"
        # Only accept plain identifier keys; anything else is ignored silently.
        case "$key" in
            [A-Za-z_][A-Za-z0-9_]*) ;;
            *) continue ;;
        esac
        # Real environment takes precedence: do not overwrite.
        if [ -z "${!key+x}" ]; then
            export "$key=$value"
        fi
    done < .env
}

# migration_percent_encode percent-encodes a value for use in a URL. Reserving
# keep (`A-Z a-z 0-9 . _ ~ -`) stays literal; every other byte becomes %XX. The
# loop runs under LC_ALL=C so indexing counts bytes, not runes.
migration_percent_encode() {
    local s="${1:-}" c out="" h
    local i
    for ((i = 0; i < ${#s}; i++)); do
        c="${s:i:1}"
        case "$c" in
            [A-Za-z0-9_.~-]) out+="$c" ;;
            *)
                printf -v h '%%%02X' "'$c"
                out+="$h"
                ;;
        esac
    done
    printf '%s' "$out"
}

# migration_resolve_database_url sets MIGRATION_DATABASE_URL to the ONE canonical
# connection string every check and mutation uses. An explicit DATABASE_URL wins
# over the DB_* parts (and is left byte-for-byte as provided); otherwise the URL
# is constructed from DB_* with percent-encoded credentials/database, the
# configured port and sslmode. It never splits a URL on whitespace or assumes a
# password cannot contain @ : # $, spaces or slashes.
migration_resolve_database_url() {
    migration_load_env
    if [ -n "${DATABASE_URL:-}" ]; then
        MIGRATION_DATABASE_URL="$DATABASE_URL"
        return 0
    fi

    local user pass host port db ssl
    user="$(migration_percent_encode "${DB_USER:-whento}")"
    pass="$(migration_percent_encode "${DB_PASSWORD:-whento}")"
    host="${DB_HOST:-postgres}"
    port="${DB_PORT:-5432}"
    db="$(migration_percent_encode "${DB_NAME:-whento}")"
    ssl="${DB_SSLMODE:-disable}"

    MIGRATION_DATABASE_URL="postgres://${user}:${pass}@${host}:${port}/${db}?sslmode=${ssl}"
    return 0
}

# migration_redact_url strips the password out of a connection string so prompts
# and logs never carry credentials.
migration_redact_url() {
    printf '%s' "${1:-}" | sed -E 's#(postgres://[^:/@]+:)[^@]*(@)#\1*****\2#'
}

# migration_connected_identity prints "database|user|port" for the resolved URL,
# or "unknown" when the probe itself fails. Read-only.
migration_connected_identity() {
    psql "$MIGRATION_DATABASE_URL" -Atqc \
        "SELECT current_database() || '|' || current_user || '|' || COALESCE(inet_server_port()::text, '?')" \
        2>/dev/null || echo "unknown"
}

# migration_connected_db returns the connected database name from a read-only
# SQL probe, or empty when the probe itself fails (missing psql, unreachable
# server). Callers must treat empty as "unknown target", never as a real name.
migration_connected_db() {
    psql "$MIGRATION_DATABASE_URL" -Atqc "SELECT current_database()" 2>/dev/null || true
}
