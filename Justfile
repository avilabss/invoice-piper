# Run unit tests
test:
    go test ./...

# Run unit tests with verbose output
test-v:
    go test -v ./...

# Run integration tests (requires config.json with real IMAP credentials)
test-integration:
    go test -tags integration -v ./...

# Run all tests with integration tag enabled (includes unit tests)
test-all:
    go test -tags integration -v ./...

# Lint
lint:
    golangci-lint run ./...

# Build invp binary
build:
    go build -o invp .

# Run invp with arguments
run *ARGS:
    go run . {{ARGS}}
