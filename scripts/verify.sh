#!/bin/bash
set -e

echo "--- Running go mod tidy ---"
go mod tidy

echo "--- Running go fmt ---"
if [ "$(gofmt -s -l . | wc -l)" -gt 0 ]; then
    echo "Files need formatting:"
    gofmt -s -l .
    exit 1
fi

echo "--- Running golangci-lint ---"
if command -v golangci-lint >/dev/null 2>&1; then
    golangci-lint --version
    golangci-lint run
else
    echo "golangci-lint is not installed. Skipping linting."
fi

echo "--- Running Security Check (govulncheck) ---"
if command -v govulncheck >/dev/null 2>&1; then
    govulncheck ./...
else
    echo "govulncheck not in PATH. Running via go run..."
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...
fi

echo "--- Running Security Audit (gosec) ---"
if command -v gosec >/dev/null 2>&1; then
    gosec ./...
else
    echo "gosec not in PATH. Running via go run..."
    go run github.com/securego/gosec/v2/cmd/gosec@latest ./...
fi

echo "--- Running tests (with coverage) ---"
if ! command -v docker >/dev/null 2>&1; then
    echo "Docker is not installed or not in PATH. Cannot run tests in container."
    exit 1
fi

if ! docker info >/dev/null 2>&1; then
    echo "Docker daemon is not running. Cannot run tests in container."
    exit 1
fi

CACHE_ROOT="${XDG_CACHE_HOME:-$HOME/.cache}/go"
MOD_CACHE="$CACHE_ROOT/pkg/mod"
BUILD_CACHE="$CACHE_ROOT/build-cache"

mkdir -p "$MOD_CACHE" "$BUILD_CACHE"

docker run --rm \
    -v "$(pwd):/app" \
    -v "$MOD_CACHE:/go/pkg/mod" \
    -v "$BUILD_CACHE:/root/.cache/go-build" \
    -w /app golang:1.25 go test -v -covermode=atomic -coverprofile coverage.out ./...

echo "--- Coverage summary (excluding generated) ---"
grep -E '^mode:|^[^:]+\.go:' coverage.out | grep -vE 'openapi_generated\.go|_generated\.go' > coverage.filtered.out || true
# Ensure mode line is present
if ! grep -q '^mode:' coverage.filtered.out; then
    head -1 coverage.out > coverage.filtered.out.tmp
    cat coverage.filtered.out >> coverage.filtered.out.tmp
    mv coverage.filtered.out.tmp coverage.filtered.out
fi

COVER_OUT=$(docker run --rm \
    -v "$(pwd):/app" \
    -v "$MOD_CACHE:/go/pkg/mod" \
    -v "$BUILD_CACHE:/root/.cache/go-build" \
    -w /app golang:1.25 go tool cover -func coverage.filtered.out)

echo "$COVER_OUT"

TOTAL_LINE=$(echo "$COVER_OUT" | grep '^total:')
if [ -z "$TOTAL_LINE" ]; then
    echo "Could not find total coverage line."
    exit 1
fi

TOTAL=$(echo "$TOTAL_LINE" | grep -oE '[0-9]+\.[0-9]+%' | tr -d '%')
if [ -z "$TOTAL" ]; then
    echo "Could not parse total coverage percentage."
    exit 1
fi

if [ "$(echo "$TOTAL < 100.0" | bc -l)" -eq 1 ]; then
    echo "Coverage below 100% for non-generated files: ${TOTAL}%"
    exit 1
fi

echo "--- Verification complete! ---"
