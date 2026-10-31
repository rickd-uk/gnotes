#!/bin/sh
# Real binary + real SQLite/SQLCipher, isolated files and port. No production IO.
set -eu
umask 077

project_directory="$(cd "$(dirname -- "$0")/../.." && pwd)"
test_directory="$(mktemp -d /tmp/gnotes-encryption-rehearsal.XXXXXX)"
cleanup() {
    if [ -f "$test_directory/server.pid" ]; then
        kill "$(cat "$test_directory/server.pid")" 2>/dev/null || true
    fi
    rm -rf "$test_directory"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
fail() { echo "encryption-rehearsal: $*" >&2; exit 1; }

export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g -ftls-model=initial-exec}"
export GOFLAGS="${GOFLAGS:--tags=sqlite_fts5}"
go build -o "$test_directory/gnotes" "$project_directory/cmd/server"
port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
export TEST_PROJECT_DIRECTORY="$project_directory"
export TEST_BINARY="$test_directory/gnotes"
export TEST_SERVER_PID="$test_directory/server.pid"
export TEST_SERVER_LOG="$test_directory/server.log"
export DATABASE_PATH="$test_directory/gnotes.db"
export HOST=127.0.0.1 PORT="$port"
export GNOTES_HEALTH_URL="http://127.0.0.1:$port/api/health"
export GNOTES_APP_DIRECTORY="$test_directory"
export GNOTES_STORAGE_TOOL="$test_directory/gnotes"
export GNOTES_RESTORER="$project_directory/deploy/scripts/restore-gnotes-podman"
export BACKUP_DIRECTORY="$test_directory/backups"
unset GNOTES_DATABASE_KEY_FILE GNOTES_REQUIRE_ENCRYPTION BREVO_API_KEY
mkdir -p "$test_directory/mock-bin" "$BACKUP_DIRECTORY"

cat > "$test_directory/mock-bin/systemctl" <<'SH'
#!/bin/sh
[ "${1:-}" != --user ] || shift
case "$1" in
    stop)
        if [ -f "$TEST_SERVER_PID" ]; then
            kill "$(cat "$TEST_SERVER_PID")" 2>/dev/null || true
            attempt=0
            while [ "$attempt" -lt 60 ] && curl -fsS --max-time 1 "$GNOTES_HEALTH_URL" >/dev/null 2>&1; do
                sleep 0.1
                attempt=$((attempt+1))
            done
            rm -f "$TEST_SERVER_PID"
        fi
        ;;
    start)
        (cd "$TEST_PROJECT_DIRECTORY" && exec "$TEST_BINARY") > "$TEST_SERVER_LOG" 2>&1 &
        echo $! > "$TEST_SERVER_PID"
        ;;
    *) exit 2 ;;
esac
SH
chmod 0755 "$test_directory/mock-bin/systemctl"
export PATH="$test_directory/mock-bin:$PATH"
wait_for_health() {
    attempt=0
    while [ "$attempt" -lt 80 ]; do
        if curl -fsS --max-time 1 "$GNOTES_HEALTH_URL" >/dev/null 2>&1; then return 0; fi
        sleep 0.1
        attempt=$((attempt+1))
    done
    return 1
}

cat > "$test_directory/account.py" <<'PY'
import http.cookiejar, json, os, sys, urllib.request
base = os.environ['GNOTES_HEALTH_URL'].removesuffix('/api/health')
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ''
def request(path, body=None, method=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header('Content-Type', 'application/json')
    req.add_header('Origin', base)
    if csrf:
        req.add_header('X-CSRF-Token', csrf)
    with client.open(req, timeout=10) as response:
        raw = response.read()
        return json.loads(raw) if raw else None
action = sys.argv[1]
credentials = {'username': 'rick', 'password': 'rehearsal password only'}
auth = request('/api/auth/' + ('register' if action == 'create' else 'login'), credentials)
csrf = auth['csrf_token']
if action == 'create':
    request('/api/notes/create', {'title': 'PrivateOrchidSignal', 'content': 'Encrypted snapshot rehearsal'})
elif action == 'change':
    request('/api/notes/update?id=1', {'title': 'ChangedAfterBackup', 'content': 'later change', 'revision': request('/api/notes/get?id=1')['revision']}, 'PUT')
else:
    data = request('/api/notes/search-page?q=PrivateOrchidSignal')
    assert 'Encrypted snapshot rehearsal' in json.dumps(data), data
PY

systemctl start gnotes.service
wait_for_health || fail "plaintext fixture server failed to start"
python3 "$test_directory/account.py" create
systemctl stop gnotes.service

"$TEST_BINARY" db keygen --output "$test_directory/owner.key" >/dev/null
"$TEST_BINARY" db encrypt --source "$DATABASE_PATH" --output "$test_directory/encrypted.db" --key-file "$test_directory/owner.key" >/dev/null
"$TEST_BINARY" db verify --source "$test_directory/encrypted.db" --key-file "$test_directory/owner.key" >/dev/null
# The source remains available until this isolated rehearsal explicitly swaps it.
mv "$DATABASE_PATH" "$test_directory/original.db"
mv "$test_directory/encrypted.db" "$DATABASE_PATH"
export GNOTES_DATABASE_KEY_FILE="$test_directory/owner.key" GNOTES_REQUIRE_ENCRYPTION=1
systemctl start gnotes.service
wait_for_health || fail "encrypted server failed to start"
python3 "$test_directory/account.py" check

"$project_directory/deploy/scripts/backup-gnotes-podman" >/dev/null
archive="$(find "$BACKUP_DIRECTORY" -name 'gnotes-daily-*.db.gz' -print -quit)"
[ -n "$archive" ] || fail "encrypted daily backup missing"
"$GNOTES_RESTORER" --verify "$archive" >/dev/null
python3 - "$DATABASE_PATH" "$archive" <<'PY'
import gzip, pathlib, sys
for data in [pathlib.Path(sys.argv[1]).read_bytes(), gzip.open(sys.argv[2], 'rb').read()]:
    assert not data.startswith(b'SQLite format 3')
    assert b'PrivateOrchidSignal' not in data
    assert b'Encrypted snapshot rehearsal' not in data
PY
python3 "$test_directory/account.py" change
"$GNOTES_RESTORER" --restore "$archive" >/dev/null
python3 "$test_directory/account.py" check

"$TEST_BINARY" db keygen --output "$test_directory/wrong.key" >/dev/null
if GNOTES_DATABASE_KEY_FILE="$test_directory/wrong.key" "$GNOTES_RESTORER" --restore "$archive" >/dev/null 2>&1; then
    fail "wrong key accepted for restoration"
fi
python3 "$test_directory/account.py" check
systemctl stop gnotes.service
if GNOTES_DATABASE_KEY_FILE='' "$TEST_BINARY" > "$test_directory/missing-key.log" 2>&1; then
    fail "server started without its required key"
fi
if GNOTES_DATABASE_KEY_FILE="$test_directory/wrong.key" "$TEST_BINARY" > "$test_directory/wrong-key.log" 2>&1; then
    fail "server started with wrong key"
fi
systemctl start gnotes.service
wait_for_health || fail "correct key did not recover startup"
python3 "$test_directory/account.py" check

echo "Encrypted migration, login/search/edit, daily backup, restore, wrong-key rejection and restart rehearsal passed."
