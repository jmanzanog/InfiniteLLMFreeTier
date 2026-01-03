#!/bin/bash
set -e

echo "--- Running go mod tidy ---"
go mod tidy

echo "--- Running go fmt ---"
if [ "$(gofmt -s -l . | wc -l)" -gt 0 ]; then
    echo "Files need formatting. Run 'go fmt ./...'"
    gofmt -s -l .
    exit 1
fi

echo "--- Running golangci-lint ---"
if command -v golangci-lint >/dev/null 2>&1; then
    golangci-lint run
else
    echo "golangci-lint is not installed. Skipping linting."
fi

echo "--- Running tests (Postgres + Oracle) ---"
go test -v ./...

echo "--- Verification complete! ---"
