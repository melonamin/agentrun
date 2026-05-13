# Common development commands for agentrun

set dotenv-load := false

# Show available recipes
_default:
    @just --list

# Format Go sources
fmt:
    gofmt -w cmd internal

# Run tests
test:
    go test ./...

# Run go vet
vet:
    go vet ./...

# Format, vet, and test
check: fmt vet test

# Build local binary
build:
    go build -o ./agentrun ./cmd/agentrun

# Install into GOPATH/bin / GOBIN
install:
    go install ./cmd/agentrun

# Run the CLI from source, e.g. `just run --help`
run *args:
    go run ./cmd/agentrun {{args}}

# Remove local build artifacts
clean:
    rm -f ./agentrun
    rm -rf ./dist

# Tidy module files
tidy:
    go mod tidy

# Compare drop-in behavior with real claude -p calls (uses Claude quota)
compare-claude-p: build
    scripts/compare-claude-p.sh

# Build release binaries into dist/
dist: clean
    mkdir -p dist
    GOOS=linux GOARCH=amd64 go build -o dist/agentrun-linux-amd64 ./cmd/agentrun
    GOOS=linux GOARCH=arm64 go build -o dist/agentrun-linux-arm64 ./cmd/agentrun
    GOOS=darwin GOARCH=amd64 go build -o dist/agentrun-darwin-amd64 ./cmd/agentrun
    GOOS=darwin GOARCH=arm64 go build -o dist/agentrun-darwin-arm64 ./cmd/agentrun
