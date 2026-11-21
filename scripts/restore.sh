#!/usr/bin/env bash
# CloudForge — PostgreSQL restore (Milestone 9, Part 3).
#
# Restores a pg_dump -Fc file (made by scripts/backup.sh) into a
# database with pg_restore. The restore is DESTRUCTIVE for the
# target database's CloudForge data: by default it refuses to run
# unless the target database is empty (no tables) or --force is
# given, so a typo cannot wipe a live database.
#
# Usage:
#   scripts/restore.sh [--help] [--dry-run] [--force] [--db DATABASE_URL] FILE
#
# Environment:
#   DATABASE_URL   Target Postgres DSN (default: the local dev DSN).
#                  The target database must already exist (createdb).
#
# Requirements: pg_restore and psql on PATH.
set -euo pipefail

PROG="$(basename "$0")"
DRY_RUN=0
FORCE=0
DB_URL="${DATABASE_URL:-postgres://cloudforge:cloudforge@127.0.0.1:5432/cloudforge}"
FILE=""

usage() {
	cat <<EOF
Usage: $PROG [--help] [--dry-run] [--force] [--db DATABASE_URL] FILE

Restore a CloudForge backup (pg_dump -Fc file from scripts/backup.sh).

Options:
  --help            Show this help and exit.
  --dry-run         Print the commands that would run (with the
                    password redacted) without restoring anything.
  --force           Allow restoring into a non-empty database. Without
                    --force the target must contain no tables.
  --db DATABASE_URL Target Postgres DSN (default: \$DATABASE_URL or the
                    local dev DSN). The database must already exist.

The restore drops and recreates CloudForge objects in the target
database (pg_restore --clean --if-exists); it never touches other
databases. Stop the api and worker before restoring into a live
database, and re-run migrations afterwards if the backup predates
them (the api applies migrations at startup).
EOF
}

redact_url() {
	local url="$1"
	case "$url" in
		*"://"*"@"*)
			local scheme="${url%%://*}"
			local rest="${url#*://}"
			local userinfo="${rest%%@*}"
			local hostpart="${rest#*@}"
			local user="${userinfo%%:*}"
			echo "${scheme}://${user}:<redacted>@${hostpart}"
			;;
		*) echo "$url" ;;
	esac
}

while [ $# -gt 0 ]; do
	case "$1" in
		--help|-h) usage; exit 0 ;;
		--dry-run) DRY_RUN=1; shift ;;
		--force) FORCE=1; shift ;;
		--db) [ $# -ge 2 ] || { echo "$PROG: --db needs a value" >&2; exit 2; }; DB_URL="$2"; shift 2 ;;
		-*) echo "$PROG: unknown argument: $1" >&2; usage >&2; exit 2 ;;
		*) [ -z "$FILE" ] || { echo "$PROG: only one FILE argument is allowed" >&2; exit 2; }; FILE="$1"; shift ;;
	esac
done

[ -n "$FILE" ] || { echo "$PROG: a backup FILE is required" >&2; usage >&2; exit 2; }
[ -f "$FILE" ] || { echo "$PROG: backup file not found: $FILE" >&2; exit 1; }

command -v pg_restore >/dev/null 2>&1 || { echo "$PROG: pg_restore not found on PATH (install postgresql-client)" >&2; exit 1; }
command -v psql >/dev/null 2>&1 || { echo "$PROG: psql not found on PATH (install postgresql-client)" >&2; exit 1; }

if [ "$DRY_RUN" -eq 1 ]; then
	echo "[dry-run] would check the target database is empty (or --force): $(redact_url "$DB_URL")"
	echo "[dry-run] pg_restore --dbname=\"$(redact_url "$DB_URL")\" --clean --if-exists --no-owner \"$FILE\""
	exit 0
fi

# Refuse a non-empty target without --force: count tables in the
# public schema. A typo in --db must not wipe a live database.
TABLES="$(psql --dbname="$DB_URL" --tuples-only --no-align --command="SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")"
TABLES="$(echo "$TABLES" | tr -d '[:space:]')"
if [ "$TABLES" != "0" ] && [ "$FORCE" -eq 0 ]; then
	echo "$PROG: target database already has $TABLES table(s); refusing to restore without --force." >&2
	echo "$PROG: re-run with --force to overwrite, or restore into an empty database." >&2
	exit 1
fi

pg_restore --dbname="$DB_URL" --clean --if-exists --no-owner "$FILE"
echo "restore complete: $FILE -> $(redact_url "$DB_URL")"
