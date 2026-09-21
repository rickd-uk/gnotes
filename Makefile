.PHONY: run build clean seed seed-clean

SEED_DATABASE ?= gnotes.db
SEED_USER ?= rick
SEED_COUNT ?= 500

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
