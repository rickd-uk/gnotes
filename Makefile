.PHONY: run build clean seed seed-clean test check test-browser test-encryption

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
