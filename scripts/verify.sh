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

echo "--- Running Security Check (govulncheck) ---"
if command -v govulncheck >/dev/null 2>&1; then
    govulncheck ./...
else
    echo "govulncheck is not installed. Installing..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
fi

echo "--- Running Security Audit (gosec) ---"
if command -v gosec >/dev/null 2>&1; then
    gosec ./...
else
    echo "gosec is not installed. Installing..."
    go install github.com/securego/gosec/v2/cmd/gosec@latest
    gosec ./...
fi

echo "--- Running tests (Postgres + Oracle) ---"
go test -v ./...

echo "--- Verification complete! ---"
