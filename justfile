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

# Build release archives into dist/
dist:
    scripts/build-release.sh
