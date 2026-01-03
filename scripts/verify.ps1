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

Write-Host "--- Running tests (Postgres + Oracle) ---" -ForegroundColor Cyan
go test -v ./...

Write-Host "--- Verification complete! ---" -ForegroundColor Green
