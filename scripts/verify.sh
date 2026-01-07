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

echo "--- Running tests ---"
go test -v ./...

echo "--- Verification complete! ---"
