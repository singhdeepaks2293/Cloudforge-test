#!/usr/bin/env bash
# CloudForge — PostgreSQL backup (Milestone 9, Part 3).
#
# Dumps the CloudForge database with pg_dump (custom format, -Fc)
# to a timestamped file under a configurable directory.
#
# Usage:
#   scripts/backup.sh [--help] [--dry-run] [--dir DIR] [--db DATABASE_URL]
#
# Environment:
#   DATABASE_URL   Postgres DSN (default: the local dev DSN —
#                  postgres://cloudforge:cloudforge@127.0.0.1:5432/cloudforge).
#                  Prefer passing it explicitly; the password is never
#                  echoed (see --dry-run redaction below).
#   BACKUP_DIR     Default output directory (default: ./backups).
#   BACKUP_KEEP    How many recent backups to keep (default: 7).
#                  Older files are pruned after a successful dump.
#                  Set to 0 to disable pruning.
#
# Requirements: pg_dump on PATH (PostgreSQL client tools).
# The dump is a plain pg_dump -Fc file; restore with scripts/restore.sh.
set -euo pipefail

PROG="$(basename "$0")"
DRY_RUN=0
DIR="${BACKUP_DIR:-./backups}"
DB_URL="${DATABASE_URL:-postgres://cloudforge:cloudforge@127.0.0.1:5432/cloudforge}"
KEEP="${BACKUP_KEEP:-7}"

usage() {
	cat <<EOF
Usage: $PROG [--help] [--dry-run] [--dir DIR] [--db DATABASE_URL]

Back up the CloudForge PostgreSQL database with pg_dump -Fc.

Options:
  --help            Show this help and exit.
  --dry-run         Print the commands that would run (with the
                    password redacted) without dumping anything.
  --dir DIR         Output directory (default: \$BACKUP_DIR or ./backups).
  --db DATABASE_URL Postgres DSN (default: \$DATABASE_URL or the local
                    dev DSN postgres://cloudforge:cloudforge@127.0.0.1:5432/cloudforge).

Environment: DATABASE_URL, BACKUP_DIR, BACKUP_KEEP (default 7, 0 = keep all).
EOF
}

# redact_url masks the password in a DSN for --dry-run output.
redact_url() {
	local url="$1"
	# postgres://user:password@host -> postgres://user:<redacted>@host
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
		--dir) [ $# -ge 2 ] || { echo "$PROG: --dir needs a value" >&2; exit 2; }; DIR="$2"; shift 2 ;;
		--db) [ $# -ge 2 ] || { echo "$PROG: --db needs a value" >&2; exit 2; }; DB_URL="$2"; shift 2 ;;
		*) echo "$PROG: unknown argument: $1" >&2; usage >&2; exit 2 ;;
	esac
done

command -v pg_dump >/dev/null 2>&1 || { echo "$PROG: pg_dump not found on PATH (install postgresql-client)" >&2; exit 1; }

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
FILE="${DIR}/cloudforge-${STAMP}.dump"

run() {
	if [ "$DRY_RUN" -eq 1 ]; then
		echo "[dry-run] $*"
	else
		"$@"
	fi
}

if [ "$DRY_RUN" -eq 1 ]; then
	echo "[dry-run] would create directory: $DIR"
	echo "[dry-run] pg_dump --dbname=\"$(redact_url "$DB_URL")\" --format=custom --no-owner --file=\"$FILE\""
	echo "[dry-run] would prune backups older than the newest $KEEP in $DIR"
	exit 0
fi

mkdir -p "$DIR"
pg_dump --dbname="$DB_URL" --format=custom --no-owner --file="$FILE"
echo "backup written: $FILE ($(du -h "$FILE" | cut -f1))"

if [ "$KEEP" -gt 0 ]; then
	# Prune oldest, keeping the newest $KEEP dumps.
	ls -t "${DIR}"/cloudforge-*.dump 2>/dev/null | tail -n +"$((KEEP + 1))" | while read -r old; do
		rm -f -- "$old"
		echo "pruned old backup: $old"
	done
fi
