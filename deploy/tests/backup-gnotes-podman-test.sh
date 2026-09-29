#!/bin/sh
set -eu

project_directory="$(cd "$(dirname -- "$0")/../.." && pwd)"
test_directory="$(mktemp -d /tmp/gnotes-podman-backup-test.XXXXXX)"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
fail() { echo "backup-gnotes-podman-test: $*" >&2; exit 1; }

python3 - "$test_directory/good.db" "$test_directory/incomplete.db" <<'PY'
import sqlite3
import sys

good = sqlite3.connect(sys.argv[1])
good.executescript("""
    CREATE TABLE users (username TEXT, password_hash BLOB);
    CREATE TABLE notes (user_id INTEGER, title TEXT, content TEXT);
    CREATE TABLE sessions (token_hash TEXT);
    CREATE TABLE drafts (user_id INTEGER);
    CREATE TABLE settings (key TEXT);
    CREATE TABLE rate_limits (scope TEXT);
    CREATE TABLE login_cooldowns (username_hash TEXT);
    CREATE TABLE signup_events (user_id INTEGER);
    CREATE TABLE invitations (id INTEGER);
    CREATE TABLE security_daily (day TEXT);
    INSERT INTO notes VALUES (1, 'snapshot', 'body');
""")
good.close()
incomplete = sqlite3.connect(sys.argv[2])
incomplete.execute("CREATE TABLE notes (content TEXT)")
incomplete.close()
PY

mkdir "$test_directory/backups"
DATABASE_PATH="$test_directory/good.db" \
BACKUP_DIRECTORY="$test_directory/backups" \
GNOTES_RESTORER="$project_directory/deploy/scripts/restore-gnotes-podman" \
    "$project_directory/deploy/scripts/backup-gnotes-podman" >/dev/null
archive="$(find "$test_directory/backups" -maxdepth 1 -name 'gnotes-daily-*.db.gz' -print -quit)"
[ -n "$archive" ] || fail "no completed backup"
"$project_directory/deploy/scripts/restore-gnotes-podman" --verify "$archive" >/dev/null

if DATABASE_PATH="$test_directory/incomplete.db" \
    BACKUP_DIRECTORY="$test_directory/backups" \
    GNOTES_RESTORER="$project_directory/deploy/scripts/restore-gnotes-podman" \
        "$project_directory/deploy/scripts/backup-gnotes-podman" >/dev/null 2>&1; then
    fail "accepted incomplete database"
fi
[ "$(find "$test_directory/backups" -maxdepth 1 -name 'gnotes-daily-*.db.gz' | wc -l)" -eq 1 ] || fail "invalid backup was published"
[ "$(find "$test_directory/backups" -maxdepth 1 -name '.gnotes-*' | wc -l)" -eq 0 ] || fail "temporary backup was left behind"
echo "backup-gnotes-podman integration tests passed"
