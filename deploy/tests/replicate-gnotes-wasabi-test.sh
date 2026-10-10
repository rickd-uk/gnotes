#!/bin/sh
set -eu
umask 077
project_directory="$(cd "$(dirname -- "$0")/../.." && pwd)"
test_directory="$(mktemp -d /tmp/gnotes-wasabi-test.XXXXXX)"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
fail() { echo "replicate-gnotes-wasabi-test: $*" >&2; exit 1; }
mkdir "$test_directory/backups"
export RESTIC_REPOSITORY="$test_directory/repository"
export RESTIC_PASSWORD='integration-test-password-only'
export RESTIC_CACHE_DIR="$test_directory/cache"
export BACKUP_DIRECTORY="$test_directory/backups"
export GNOTES_RESTORER="$project_directory/deploy/scripts/restore-gnotes-podman"
export GNOTES_BACKUP_HOST=integration-test

python3 - "$test_directory/good.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.executescript('''
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
INSERT INTO notes VALUES (1, 'restore me', 'private fixture content');
''')
db.close()
PY
archive="$BACKUP_DIRECTORY/gnotes-daily-20261010T030000Z.db.gz"
gzip -c "$test_directory/good.db" > "$archive"
restic init >/dev/null
# Record real commands so tests catch accidental full-repository downloads.
export GNOTES_REAL_RESTIC="$(command -v restic)"
export GNOTES_RESTIC_LOG="$test_directory/restic-commands.log"
cat > "$test_directory/restic-wrapper" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$GNOTES_RESTIC_LOG"
exec "$GNOTES_REAL_RESTIC" "$@"
SH
chmod +x "$test_directory/restic-wrapper"
export GNOTES_RESTIC="$test_directory/restic-wrapper"
"$project_directory/deploy/scripts/replicate-gnotes-wasabi" > "$test_directory/run.log" 2>&1 || { cat "$test_directory/run.log"; fail 'round-trip failed'; }
restic snapshots --json > "$test_directory/snapshots.json"
python3 - "$test_directory/snapshots.json" <<'PY'
import json, sys
snapshots = json.load(open(sys.argv[1]))
assert len(snapshots) == 1
assert snapshots[0]['tags'] == ['gnotes']
assert snapshots[0]['paths'] == ['/gnotes.db.gz']
PY
if grep -q 'read-data\|^restore ' "$GNOTES_RESTIC_LOG"; then
    fail 'daily backup downloaded repository data for verification'
fi
grep -qx 'check' "$GNOTES_RESTIC_LOG" || fail 'metadata check missing'
grep -q 'repository metadata checked' "$test_directory/run.log" || fail 'wrong daily result'
GNOTES_VERIFY_RESTORE=1 "$project_directory/deploy/scripts/replicate-gnotes-wasabi" > "$test_directory/verified.log" 2>&1 || { cat "$test_directory/verified.log"; fail 'explicit restore verification failed'; }
grep -q '^restore latest ' "$GNOTES_RESTIC_LOG" || fail 'explicit verification did not restore'
grep -q 'restore-verified' "$test_directory/verified.log" || fail 'restore verification result missing'
if grep -q 'read-data' "$GNOTES_RESTIC_LOG"; then fail 'full repository download requested'; fi
RESTIC_PASSWORD=incorrect restic snapshots >/dev/null 2>&1 && fail 'incorrect password accepted'

python3 - "$archive" <<'PY'
import os, sys, time
old = time.time() - 40 * 3600
os.utime(sys.argv[1], (old, old))
PY
if "$project_directory/deploy/scripts/replicate-gnotes-wasabi" >/dev/null 2>&1; then fail 'stale archive accepted'; fi
printf 'not a gzip database' > "$archive"
if "$project_directory/deploy/scripts/replicate-gnotes-wasabi" >/dev/null 2>&1; then fail 'corrupt archive accepted'; fi
restic snapshots --json > "$test_directory/snapshots-after.json"
python3 - "$test_directory/snapshots-after.json" <<'PY'
import json, sys
assert len(json.load(open(sys.argv[1]))) == 2
PY
echo 'Wasabi replication local-repository integration tests passed'
