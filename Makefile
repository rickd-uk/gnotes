run:
	go run ./cmd/server/main.go
build:
	go build -o gnotes ./cmd/server/main.go
clean:
	rm -f gnotes