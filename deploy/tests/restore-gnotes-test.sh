#!/bin/sh
set -eu

project_directory="$(cd "$(dirname -- "$0")/../.." && pwd)"
restorer="$project_directory/deploy/scripts/restore-gnotes"
test_directory="$(mktemp -d /tmp/gnotes-restore-test.XXXXXX)"
mock_directory="$test_directory/mock-bin"
database_path="$test_directory/live/gnotes.db"
mkdir -p "$mock_directory" "$test_directory/live"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM

fail() { echo "restore-gnotes-test: $*" >&2; exit 1; }

cat > "$mock_directory/id" <<'EOF'
#!/bin/sh
if [ "${1:-}" = -u ]; then echo 0; else /usr/bin/id "$@"; fi
EOF
cat > "$mock_directory/install" <<'EOF'
#!/bin/sh
if [ "${1:-}" = -o ]; then shift 4; fi
exec /usr/bin/install "$@"
EOF
cat > "$mock_directory/systemctl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$TEST_SYSTEMCTL_LOG"
case "$1" in
    is-active) [ "$(cat "$TEST_SERVICE_STATE")" = active ] ;;
    stop) printf 'stopped\n' > "$TEST_SERVICE_STATE" ;;
    start) printf 'active\n' > "$TEST_SERVICE_STATE" ;;
    *) exit 2 ;;
esac
EOF
cat > "$mock_directory/curl" <<'EOF'
#!/bin/sh
[ "$(cat "$TEST_SERVICE_STATE")" = active ] || exit 22
marker="$(sqlite3 -readonly -noheader -cmd '.mode list' "$TEST_DATABASE_PATH" 'SELECT title FROM notes LIMIT 1;' 2>/dev/null)" || exit 22
[ "$marker" != unhealthy ] || exit 22
EOF
cat > "$mock_directory/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0755 "$mock_directory"/*

create_database() {
    path="$1"
    marker="$2"
    sqlite3 "$path" "
        CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, password_hash BLOB);
        CREATE TABLE notes (id INTEGER PRIMARY KEY, user_id INTEGER, title TEXT, content TEXT);
        CREATE TABLE sessions (token_hash TEXT);
        CREATE TABLE drafts (user_id INTEGER);
        CREATE TABLE settings (key TEXT);
        CREATE TABLE rate_limits (scope TEXT);
        CREATE TABLE login_cooldowns (username_hash TEXT);
        CREATE TABLE signup_events (user_id INTEGER);
        CREATE TABLE invitations (id INTEGER);
        CREATE TABLE security_daily (day TEXT);
        INSERT INTO notes (user_id, title, content) VALUES (1, '$marker', 'body');
    "
}

run_restorer() {
    PATH="$mock_directory:/usr/bin:/bin" \
    TEST_SYSTEMCTL_LOG="$test_directory/systemctl.log" \
    TEST_SERVICE_STATE="$test_directory/service.state" \
    TEST_DATABASE_PATH="$database_path" \
    DATABASE_PATH="$database_path" \
    GNOTES_HEALTH_URL=http://health \
    GNOTES_DATABASE_USER="$(id -un)" \
    GNOTES_DATABASE_GROUP="$(id -gn)" \
        "$restorer" "$@"
}

create_database "$database_path" original
create_database "$test_directory/good.db" restored
create_database "$test_directory/unhealthy.db" unhealthy
create_database "$test_directory/incomplete.db" incomplete
sqlite3 "$test_directory/incomplete.db" 'ALTER TABLE notes DROP COLUMN content;'
gzip -c "$test_directory/good.db" > "$test_directory/good.db.gz"
gzip -c "$test_directory/unhealthy.db" > "$test_directory/unhealthy.db.gz"
gzip -c "$test_directory/incomplete.db" > "$test_directory/incomplete.db.gz"
printf 'invalid archive\n' > "$test_directory/corrupt.db.gz"
printf 'active\n' > "$test_directory/service.state"
: > "$test_directory/systemctl.log"

run_restorer --verify "$test_directory/good.db.gz" >/dev/null
[ ! -s "$test_directory/systemctl.log" ] || fail "verification touched the service"
if run_restorer --verify "$test_directory/corrupt.db.gz" >/dev/null 2>&1; then fail "accepted corrupt gzip"; fi
if run_restorer --verify "$test_directory/incomplete.db.gz" >/dev/null 2>&1; then fail "accepted incomplete schema"; fi

run_restorer --restore "$test_directory/good.db.gz" >/dev/null
[ "$(sqlite3 -noheader -cmd '.mode list' "$database_path" 'SELECT title FROM notes LIMIT 1;')" = restored ] || fail "restore did not install backup"
saved_database="$(find "$test_directory/live" -name 'gnotes-before-restore-*.db' -print -quit)"
[ -n "$saved_database" ] || fail "restore did not save previous database"
[ "$(sqlite3 -noheader -cmd '.mode list' "$saved_database" 'SELECT title FROM notes LIMIT 1;')" = original ] || fail "saved database is incorrect"

if run_restorer --restore "$test_directory/unhealthy.db.gz" >/dev/null 2>&1; then fail "accepted unhealthy restored database"; fi
[ "$(sqlite3 -noheader -cmd '.mode list' "$database_path" 'SELECT title FROM notes LIMIT 1;')" = restored ] || fail "failed restore did not roll back"
[ "$(cat "$test_directory/service.state")" = active ] || fail "service was not restarted"

printf 'stopped\n' > "$test_directory/service.state"
printf 'damaged database\n' > "$database_path"
run_restorer --restore "$test_directory/good.db.gz" >/dev/null 2>&1
[ "$(sqlite3 -noheader -cmd '.mode list' "$database_path" 'SELECT title FROM notes LIMIT 1;')" = restored ] || fail "could not restore while original database was damaged"
[ "$(cat "$test_directory/service.state")" = active ] || fail "restore did not start inactive service"

echo "restore-gnotes integration tests passed"
