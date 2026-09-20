run:
	go run ./cmd/server
build:
	go build -o gnotes ./cmd/server
clean:
	rm -f gnotes
