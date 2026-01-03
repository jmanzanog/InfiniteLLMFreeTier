# Verification script for Windows (PowerShell)

Write-Host "--- Running go mod tidy ---" -ForegroundColor Cyan
go mod tidy

Write-Host "--- Running go fmt ---" -ForegroundColor Cyan
$fmtFiles = gofmt -s -l .
if ($fmtFiles) {
    Write-Host "Files need formatting:" -ForegroundColor Red
    $fmtFiles
    exit 1
}

Write-Host "--- Running golangci-lint ---" -ForegroundColor Cyan
if (Get-Command golangci-lint -ErrorAction SilentlyContinue) {
    golangci-lint run
}
else {
    Write-Host "golangci-lint is not installed. Skipping linting." -ForegroundColor Yellow
}

Write-Host "--- Running Security Check (govulncheck) ---" -ForegroundColor Cyan
if (Get-Command govulncheck -ErrorAction SilentlyContinue) {
    govulncheck ./...
} else {
    Write-Host "govulncheck is not installed. Installing..." -ForegroundColor Yellow
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
}

Write-Host "--- Running Security Audit (gosec) ---" -ForegroundColor Cyan
if (Get-Command gosec -ErrorAction SilentlyContinue) {
    gosec ./...
} else {
    Write-Host "gosec is not installed. Installing..." -ForegroundColor Yellow
    go install github.com/securego/gosec/v2/cmd/gosec@latest
    gosec ./...
}

Write-Host "--- Running tests (Postgres + Oracle) ---" -ForegroundColor Cyan
go test -v ./...

Write-Host "--- Verification complete! ---" -ForegroundColor Green
