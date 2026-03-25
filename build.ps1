$ErrorActionPreference = "Stop"

Write-Host "Building VRShare..." -ForegroundColor Cyan

# Build frontend
Write-Host "  Building frontend..." -ForegroundColor Gray
Push-Location frontend
$ErrorActionPreference = "Continue"
npm install --silent 2>&1 | Out-Null
npm run build 2>&1 | Out-Null
$ErrorActionPreference = "Stop"
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    Write-Host "Frontend build failed." -ForegroundColor Red
    exit 1
}
Pop-Location

# Build Go binary
Write-Host "  Building binary..." -ForegroundColor Gray
go build -tags desktop,production -ldflags "-s -w" -o vrshare.exe ./cmd/vrshare/
if ($LASTEXITCODE -ne 0) {
    Write-Host "Go build failed." -ForegroundColor Red
    exit 1
}

$size = [math]::Round((Get-Item vrshare.exe).Length / 1MB, 1)
Write-Host "Build complete: vrshare.exe (${size} MB)" -ForegroundColor Green
