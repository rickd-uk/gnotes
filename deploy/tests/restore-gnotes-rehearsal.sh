#!/bin/sh
set -eu

project_directory="$(cd "$(dirname -- "$0")/../.." && pwd)"
test_directory="$(mktemp -d /tmp/gnotes-restore-rehearsal.XXXXXX)"
mock_directory="$test_directory/mock-bin"
database_path="$test_directory/gnotes.db"
port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
health_url="http://127.0.0.1:$port/api/health"

cleanup() {
    if [ -f "$test_directory/server.pid" ]; then
        kill "$(cat "$test_directory/server.pid")" 2>/dev/null || true
    fi
    rm -rf "$test_directory"
}
trap cleanup EXIT HUP INT TERM

fail() { echo "restore-gnotes-rehearsal: $*" >&2; exit 1; }

mkdir -p "$mock_directory"
(cd "$project_directory" && go build -o "$test_directory/gnotes" ./cmd/server)

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
case "$1" in
    is-active)
        [ -f "$TEST_SERVER_PID" ] && kill -0 "$(cat "$TEST_SERVER_PID")" 2>/dev/null
        ;;
    stop)
        if [ -f "$TEST_SERVER_PID" ]; then
            kill "$(cat "$TEST_SERVER_PID")" 2>/dev/null || true
            attempt=1
            while [ "$attempt" -le 40 ] && curl -fsS --max-time 1 "$TEST_HEALTH_URL" >/dev/null 2>&1; do
                sleep 0.1
                attempt=$((attempt + 1))
            done
            rm -f "$TEST_SERVER_PID"
        fi
        ;;
    start)
        (cd "$TEST_PROJECT_DIRECTORY" && DATABASE_PATH="$TEST_DATABASE_PATH" HOST=127.0.0.1 PORT="$TEST_PORT" "$TEST_BINARY" > "$TEST_SERVER_LOG" 2>&1 & echo $! > "$TEST_SERVER_PID")
        ;;
    *) exit 2 ;;
esac
EOF
chmod 0755 "$mock_directory"/*

run_service() {
    PATH="$mock_directory:/usr/bin:/bin" \
    TEST_SERVER_PID="$test_directory/server.pid" \
    TEST_SERVER_LOG="$test_directory/server.log" \
    TEST_HEALTH_URL="$health_url" \
    TEST_PROJECT_DIRECTORY="$project_directory" \
    TEST_DATABASE_PATH="$database_path" \
    TEST_BINARY="$test_directory/gnotes" \
    TEST_PORT="$port" \
        systemctl "$@"
}

wait_for_health() {
    attempt=1
    while [ "$attempt" -le 40 ]; do
        if curl -fsS --max-time 1 "$health_url" >/dev/null 2>&1; then return 0; fi
        sleep 0.1
        attempt=$((attempt + 1))
    done
    return 1
}

run_restorer() {
    PATH="$mock_directory:/usr/bin:/bin" \
    TEST_SERVER_PID="$test_directory/server.pid" \
    TEST_SERVER_LOG="$test_directory/server.log" \
    TEST_HEALTH_URL="$health_url" \
    TEST_PROJECT_DIRECTORY="$project_directory" \
    TEST_DATABASE_PATH="$database_path" \
    TEST_BINARY="$test_directory/gnotes" \
    TEST_PORT="$port" \
    DATABASE_PATH="$database_path" \
    GNOTES_HEALTH_URL="$health_url" \
    GNOTES_DATABASE_USER="$(id -un)" \
    GNOTES_DATABASE_GROUP="$(id -gn)" \
        "$project_directory/deploy/scripts/restore-gnotes" "$@"
}

run_service start
wait_for_health || fail "temporary server did not start"
sqlite3 "$database_path" "INSERT INTO notes (title, content, created_at) VALUES ('rehearsal-backup', 'saved', CURRENT_TIMESTAMP);"
sqlite3 -batch "$database_path" ".backup '$test_directory/backup.db'"
gzip -c "$test_directory/backup.db" > "$test_directory/backup.db.gz"
sqlite3 "$database_path" "UPDATE notes SET title='newer' WHERE title='rehearsal-backup';"

run_restorer --verify "$test_directory/backup.db.gz" >/dev/null
run_restorer --restore "$test_directory/backup.db.gz" >/dev/null
wait_for_health || fail "restored server is not healthy"
[ "$(sqlite3 -noheader -cmd '.mode list' "$database_path" "SELECT title FROM notes WHERE content='saved';")" = rehearsal-backup ] || fail "restored note is missing"

echo "restore-gnotes real-server rehearsal passed"
