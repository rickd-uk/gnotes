#!/bin/sh
set -eu

project_directory="$(cd "$(dirname -- "$0")/../.." && pwd)"
restorer="$project_directory/deploy/scripts/restore-gnotes-podman"
test_directory="$(mktemp -d /tmp/gnotes-podman-restore-test.XXXXXX)"
mock_directory="$test_directory/mock-bin"
database_path="$test_directory/live/gnotes.db"
mkdir -p "$mock_directory" "$test_directory/live"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
fail() { echo "restore-gnotes-podman-test: $*" >&2; exit 1; }

cat > "$mock_directory/systemctl" <<'EOF'
#!/bin/sh
[ "$1" = --user ] || exit 2
shift
printf '%s\n' "$*" >> "$TEST_SYSTEMCTL_LOG"
case "$1" in
    stop) printf 'stopped\n' > "$TEST_SERVICE_STATE" ;;
    start) printf 'active\n' > "$TEST_SERVICE_STATE" ;;
    *) exit 2 ;;
esac
EOF
cat > "$mock_directory/curl" <<'EOF'
#!/bin/sh
[ "$(cat "$TEST_SERVICE_STATE")" = active ] || exit 22
marker="$(python3 - "$TEST_DATABASE_PATH" <<'PY'
import sqlite3
import sys
print(sqlite3.connect(sys.argv[1]).execute("SELECT title FROM notes LIMIT 1").fetchone()[0])
PY
)" || exit 22
[ "$marker" != unhealthy ] || exit 22
EOF
cat > "$mock_directory/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0755 "$mock_directory"/*

python3 - "$database_path" "$test_directory/good.db" "$test_directory/unhealthy.db" <<'PY'
import sqlite3
import sys

for path, marker in zip(sys.argv[1:], ("original", "restored", "unhealthy")):
    connection = sqlite3.connect(path)
    connection.executescript("""
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
    """)
    connection.execute("INSERT INTO notes VALUES (1, ?, 'body')", (marker,))
    connection.commit()
    connection.close()
PY
gzip -c "$test_directory/good.db" > "$test_directory/good.db.gz"
printf 'broken gzip\n' > "$test_directory/corrupt.db.gz"
printf 'active\n' > "$test_directory/service.state"
: > "$test_directory/systemctl.log"

run_restorer() {
    PATH="$mock_directory:/usr/bin:/bin" \
    TEST_SYSTEMCTL_LOG="$test_directory/systemctl.log" \
    TEST_SERVICE_STATE="$test_directory/service.state" \
    TEST_DATABASE_PATH="$database_path" \
    DATABASE_PATH="$database_path" \
    GNOTES_HEALTH_URL=http://health \
        "$restorer" "$@"
}
note_title() {
    python3 - "$database_path" <<'PY'
import sqlite3
import sys
print(sqlite3.connect(sys.argv[1]).execute("SELECT title FROM notes LIMIT 1").fetchone()[0])
PY
}

run_restorer --verify "$test_directory/good.db.gz" >/dev/null
[ ! -s "$test_directory/systemctl.log" ] || fail "verification touched the service"
if run_restorer --verify "$test_directory/corrupt.db.gz" >/dev/null 2>&1; then fail "accepted corrupt backup"; fi
run_restorer --restore "$test_directory/good.db" >/dev/null
[ "$(note_title)" = restored ] || fail "raw backup did not restore"
saved_database="$(find "$test_directory/live" -name 'gnotes-before-restore-*.db' -print -quit)"
[ -n "$saved_database" ] || fail "previous database was not saved"
if run_restorer --restore "$test_directory/unhealthy.db" >/dev/null 2>&1; then fail "accepted unhealthy restore"; fi
[ "$(note_title)" = restored ] || fail "failed restore did not roll back"
[ "$(cat "$test_directory/service.state")" = active ] || fail "service was not restarted"

echo "restore-gnotes-podman integration tests passed"
