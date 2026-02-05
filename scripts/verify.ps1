# Verification script for Windows (PowerShell)

Write-Host "--- Running go mod tidy ---" -ForegroundColor Cyan
go mod tidy
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "--- Running go fmt ---" -ForegroundColor Cyan
$fmtFiles = gofmt -s -l .
if ($fmtFiles) {
    Write-Host "Files need formatting:" -ForegroundColor Red
    $fmtFiles
    exit 1
}

Write-Host "--- Running golangci-lint ---" -ForegroundColor Cyan
if (Get-Command golangci-lint -ErrorAction SilentlyContinue) {
    golangci-lint --version
    golangci-lint run
    if ($LASTEXITCODE -ne 0) { exit 1 }
}
else {
    Write-Host "golangci-lint is not installed. Skipping linting." -ForegroundColor Yellow
}

Write-Host "--- Running Security Check (govulncheck) ---" -ForegroundColor Cyan
if (Get-Command govulncheck -ErrorAction SilentlyContinue) {
    govulncheck ./...
}
else {
    Write-Host "govulncheck not in PATH. Running via go run..." -ForegroundColor Yellow
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...
}
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "--- Running Security Audit (gosec) ---" -ForegroundColor Cyan
if (Get-Command gosec -ErrorAction SilentlyContinue) {
    gosec ./...
}
else {
    Write-Host "gosec not in PATH. Running via go run..." -ForegroundColor Yellow
    go run github.com/securego/gosec/v2/cmd/gosec@latest ./...
}
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "--- Running tests (with coverage) ---" -ForegroundColor Cyan
$dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerCmd) {
    Write-Host "Docker is not installed or not in PATH. Cannot run tests in container." -ForegroundColor Red
    exit 1
}

docker info > $null 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "Docker daemon is not running. Cannot run tests in container." -ForegroundColor Red
    exit 1
}

$cacheRoot = Join-Path $env:LOCALAPPDATA "go"
$modCache = Join-Path $cacheRoot "pkg\mod"
$buildCache = Join-Path $cacheRoot "build-cache"

New-Item -ItemType Directory -Force -Path $modCache | Out-Null
New-Item -ItemType Directory -Force -Path $buildCache | Out-Null

docker run --rm `
    -v "${PWD}:/app" `
    -v "${modCache}:/go/pkg/mod" `
    -v "${buildCache}:/root/.cache/go-build" `
    -w /app golang:1.25 go test -v -covermode=atomic -coverprofile coverage.out ./...

if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "--- Coverage summary (excluding generated) ---" -ForegroundColor Cyan
$filtered = Join-Path $PWD "coverage.filtered.out"
Get-Content coverage.out | Where-Object {
    $_ -match '^mode:' -or ($_ -notmatch 'openapi_generated\.go$' -and $_ -notmatch '_generated\.go$')
} | Set-Content $filtered

$coverOut = docker run --rm `
    -v "${PWD}:/app" `
    -v "${modCache}:/go/pkg/mod" `
    -v "${buildCache}:/root/.cache/go-build" `
    -w /app golang:1.25 go tool cover -func coverage.filtered.out

$coverOut

$totalLine = $coverOut | Select-String -Pattern '^total:'
if (-not $totalLine) {
    Write-Host "Could not find total coverage line." -ForegroundColor Red
    exit 1
}

$match = [regex]::Match($totalLine.Line, '([0-9]+\.[0-9]+)%')
if (-not $match.Success) {
    Write-Host "Could not parse total coverage percentage." -ForegroundColor Red
    exit 1
}

$total = [double]$match.Groups[1].Value
# Threshold is 99% to allow infrastructure code (e.g., http.Server) that cannot be unit tested
if ($total -lt 99.0) {
    Write-Host "Coverage below 99% for non-generated files: $total%" -ForegroundColor Red
    exit 1
}

Write-Host "--- Verification complete! ---" -ForegroundColor Green
