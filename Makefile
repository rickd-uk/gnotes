.PHONY: run build clean seed seed-clean test check test-browser test-encryption test-operations

# SQLCipher uses OpenSSL and native thread-local memory protection. FTS5 is
# required by gnotes search. Keep these flags consistent with release CI.
CGO_CFLAGS ?= -O2 -g
export CGO_CFLAGS := $(CGO_CFLAGS) -ftls-model=initial-exec
export GOFLAGS ?= -tags=sqlite_fts5

SEED_DATABASE ?= gnotes.db
SEED_USER ?= rick
SEED_COUNT ?= 500
BROWSER_TEST ?= .

run:
	go run ./cmd/server
build:
	go build -o gnotes ./cmd/server
clean:
	rm -f gnotes
seed:
	go run ./cmd/seed -database "$(SEED_DATABASE)" -user "$(SEED_USER)" -count "$(SEED_COUNT)" -confirm-test-data
seed-clean:
	go run ./cmd/seed -database "$(SEED_DATABASE)" -user "$(SEED_USER)" -clean -confirm-test-data

test:
	go test ./...
check: test
	go test -race ./cmd/server ./internal/storage
	go vet ./...
test-browser:
	GNOTES_BROWSER_CHECK=1 go test ./... -run '$(BROWSER_TEST)' -count=1
test-encryption:
	go test ./internal/storage ./cmd/server -run 'Test(Encrypted|DatabaseKey|DatabaseCommand|KeyFiles)' -count=1 -v
	deploy/tests/encryption-rehearsal.sh
test-operations:
	python3 -m unittest discover -s deploy/tests -p 'backup_control_test.py'
	python3 -m unittest discover -s deploy/tests -p 'volatile_key_test.py'
	shellcheck -s sh deploy/scripts/backup-gnotes deploy/scripts/backup-gnotes-podman deploy/scripts/replicate-gnotes-wasabi deploy/scripts/check-gnotes deploy/scripts/restore-gnotes deploy/scripts/restore-gnotes-podman deploy/scripts/update-gnotes deploy/scripts/update-gnotes-podman deploy/tests/*.sh
	deploy/tests/backup-gnotes-podman-test.sh
	deploy/tests/replicate-gnotes-wasabi-test.sh
	deploy/tests/update-gnotes-test.sh
	deploy/tests/restore-gnotes-test.sh
	deploy/tests/restore-gnotes-podman-test.sh
	deploy/tests/restore-gnotes-rehearsal.sh
	deploy/tests/encryption-rehearsal.sh
