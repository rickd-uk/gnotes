#!/bin/sh
set -eu

script_directory="$(dirname -- "$0")"
project_directory="$(cd "$script_directory/../.." && pwd)"
updater="$project_directory/deploy/scripts/update-gnotes"
test_directory="$(mktemp -d /tmp/gnotes-updater-test.XXXXXX)"
fixture_directory="$test_directory/fixture"
mock_directory="$test_directory/mock-bin"
app_directory="$test_directory/app"
asset="gnotes_v1.2.3_linux_amd64.tar.gz"

cleanup() {
    rm -rf "$test_directory"
}
trap cleanup EXIT HUP INT TERM

fail() {
    echo "update-gnotes-test: $*" >&2
    exit 1
}

install -d "$fixture_directory/package/bin" "$fixture_directory/package/public" "$mock_directory"
printf '#!/bin/sh\necho new-binary\n' > "$fixture_directory/package/gnotes"
chmod 0755 "$fixture_directory/package/gnotes"
printf 'new-assets\n' > "$fixture_directory/package/public/index.html"
cp "$updater" "$fixture_directory/package/bin/update-gnotes"
cp "$project_directory/deploy/scripts/check-gnotes" "$fixture_directory/package/bin/check-gnotes"
chmod 0755 "$fixture_directory/package/bin/update-gnotes" "$fixture_directory/package/bin/check-gnotes"
printf 'v1.2.3\n' > "$fixture_directory/package/VERSION"
tar -C "$fixture_directory/package" -czf "$fixture_directory/$asset" .
(cd "$fixture_directory" && sha256sum "$asset" > SHA256SUMS)

cat > "$mock_directory/id" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "-u" ]; then
    echo 0
else
    /usr/bin/id "$@"
fi
EOF

cat > "$mock_directory/uname" <<'EOF'
#!/bin/sh
echo x86_64
EOF

cat > "$mock_directory/install" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "-o" ]; then
    shift 4
fi
exec /usr/bin/install "$@"
EOF

cat > "$mock_directory/chown" <<'EOF'
#!/bin/sh
exit 0
EOF

cat > "$mock_directory/systemctl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$TEST_SYSTEMCTL_LOG"
exit 0
EOF

cat > "$mock_directory/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF

cat > "$mock_directory/curl" <<'EOF'
#!/bin/sh
output=""
url=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o)
            shift
            output="$1"
            ;;
        -*) ;;
        *) url="$1" ;;
    esac
    shift
done

case "$url" in
    */SHA256SUMS)
        cp "$TEST_FIXTURE/SHA256SUMS" "$output"
        ;;
    */gnotes_v1.2.3_linux_amd64.tar.gz)
        cp "$TEST_FIXTURE/gnotes_v1.2.3_linux_amd64.tar.gz" "$output"
        ;;
    http://health)
        if [ "${TEST_FAIL_AFTER_INSTALL:-0}" = "1" ] && grep -q new-binary "$TEST_APP/gnotes"; then
            exit 22
        fi
        printf 'healthy\n'
        ;;
    *)
        echo "unexpected curl URL: $url" >&2
        exit 2
        ;;
esac
EOF
chmod 0755 "$mock_directory/id" "$mock_directory/uname" "$mock_directory/install" "$mock_directory/chown" "$mock_directory/systemctl" "$mock_directory/sleep" "$mock_directory/curl"

setup_old_release() {
    rm -rf "$app_directory"
    install -d "$app_directory/bin" "$app_directory/public"
    printf '#!/bin/sh\necho old-binary\n' > "$app_directory/gnotes"
    chmod 0755 "$app_directory/gnotes"
    printf 'old-assets\n' > "$app_directory/public/index.html"
}

run_updater() {
    PATH="$mock_directory:/usr/bin:/bin" \
    TEST_FIXTURE="$fixture_directory" \
    TEST_APP="$app_directory" \
    TEST_SYSTEMCTL_LOG="$test_directory/systemctl.log" \
    TEST_FAIL_AFTER_INSTALL="${TEST_FAIL_AFTER_INSTALL:-0}" \
    GNOTES_APP_DIRECTORY="$app_directory" \
    GNOTES_HEALTH_URL="http://health" \
        "$updater" v1.2.3
}

setup_old_release
: > "$test_directory/systemctl.log"
run_updater >/dev/null
grep -q new-binary "$app_directory/gnotes" || fail "successful update did not install the new binary"
grep -q new-assets "$app_directory/public/index.html" || fail "successful update did not install the new assets"
grep -q old-binary "$app_directory/gnotes.rollback" || fail "successful update did not retain the old binary"
grep -q old-assets "$app_directory/public.rollback/index.html" || fail "successful update did not retain the old assets"
[ "$(cat "$app_directory/VERSION")" = "v1.2.3" ] || fail "successful update did not record its version"

setup_old_release
: > "$test_directory/systemctl.log"
if TEST_FAIL_AFTER_INSTALL=1 run_updater >/dev/null 2>&1; then
    fail "update unexpectedly succeeded when the post-install health check failed"
fi
grep -q old-binary "$app_directory/gnotes" || fail "rollback did not restore the old binary"
grep -q old-assets "$app_directory/public/index.html" || fail "rollback did not restore the old assets"
grep -q "stop gnotes.service" "$test_directory/systemctl.log" || fail "rollback did not stop the unhealthy service"
grep -q "start gnotes.service" "$test_directory/systemctl.log" || fail "rollback did not restart the service"

echo "update-gnotes integration tests passed"
