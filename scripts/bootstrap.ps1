# Playthread-Go Environment Bootstrap Script
# Usage: .\scripts\bootstrap.ps1
# Detects and configures Go amd64 + MinGW gcc + BASS for the current session.
#
# Prerequisites:
#   - Go 1.25+ (amd64) installed at C:\go_amd64\go or in system PATH
#   - MinGW-w64 gcc installed at C:\mingw64\bin or in system PATH
#   - BASS libs in audio/libs/windows/ (included in repo)

$ErrorActionPreference = "Stop"

Write-Host "=== Playthread-Go Environment Bootstrap ===" -ForegroundColor Cyan

# ---------- 1. Locate Go amd64 ----------
$goExe = $null
$goCandidates = @("C:\go_amd64\go\bin\go.exe", "C:\Program Files\Go\bin\go.exe")

foreach ($c in $goCandidates) {
    if (Test-Path $c) {
        $goExe = $c
        break
    }
}

if (-not $goExe) {
    $goExe = (Get-Command go -ErrorAction SilentlyContinue).Source
}

if (-not $goExe) {
    Write-Host "[FAIL] Go not found. Install Go 1.25+ amd64 to C:\go_amd64\go or add to PATH" -ForegroundColor Red
    exit 1
}

# Verify architecture
$goEnv = & $goExe env GOARCH GOROOT
$goArch = $goEnv[0]
$goRoot = $goEnv[1]

if ($goArch -ne "amd64") {
    Write-Host "[FAIL] Go arch is $goArch, need amd64. Install 64-bit Go" -ForegroundColor Red
    exit 1
}

$goVersion = & $goExe version
Write-Host "[OK] Go: $goVersion" -ForegroundColor Green
Write-Host "     GOROOT: $goRoot" -ForegroundColor Gray

# Set GOROOT and PATH
$env:GOROOT = $goRoot
$goBin = Split-Path $goExe
if ($env:PATH -notlike "*$goBin*") {
    $env:PATH = "$goBin;$env:PATH"
}

# ---------- 2. Locate MinGW gcc ----------
$gccExe = $null
$gccCandidates = @("C:\mingw64\bin\gcc.exe", "C:\msys64\mingw64\bin\gcc.exe")

foreach ($c in $gccCandidates) {
    if (Test-Path $c) {
        $gccExe = $c
        break
    }
}

if (-not $gccExe) {
    $gccExe = (Get-Command gcc -ErrorAction SilentlyContinue).Source
}

if (-not $gccExe) {
    Write-Host "[WARN] gcc not found. audio-service build requires MinGW-w64 gcc" -ForegroundColor Yellow
    Write-Host "       Main process (CGO_ENABLED=0) can still build" -ForegroundColor Yellow
} else {
    $gccVersion = & $gccExe --version | Select-Object -First 1
    Write-Host "[OK] gcc: $gccVersion" -ForegroundColor Green

    $gccBin = Split-Path $gccExe
    if ($env:PATH -notlike "*$gccBin*") {
        $env:PATH = "$gccBin;$env:PATH"
    }
}

# ---------- 3. Check BASS libs ----------
$bassDir = Join-Path $PSScriptRoot "..\audio\libs\windows"
$bassLib = Join-Path $bassDir "libbass.a"
$bassDll = Join-Path $bassDir "bass.dll"

if ((Test-Path $bassLib) -and (Test-Path $bassDll)) {
    Write-Host "[OK] BASS libs: libbass.a + bass.dll" -ForegroundColor Green
} else {
    Write-Host "[WARN] BASS libs incomplete ($bassDir)" -ForegroundColor Yellow
    Write-Host "       Need libbass.a and bass.dll (x64)" -ForegroundColor Yellow
}

# ---------- 4. Set CGO env vars ----------
$env:CGO_ENABLED = "1"
$env:GOARCH = "amd64"
$env:GOOS = "windows"

Write-Host ""
Write-Host "=== Environment Variables Set ===" -ForegroundColor Cyan
Write-Host "  CGO_ENABLED = $env:CGO_ENABLED"
Write-Host "  GOARCH      = $env:GOARCH"
Write-Host "  GOOS        = $env:GOOS"
Write-Host "  GOROOT      = $env:GOROOT"

# ---------- 5. Verify build ----------
Write-Host ""
Write-Host "=== Verifying Build ===" -ForegroundColor Cyan

try {
    $buildOut = & $goExe build ./... 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] go build ./... passed" -ForegroundColor Green
    } else {
        Write-Host "[FAIL] go build failed:" -ForegroundColor Red
        $buildOut | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
    }
} catch {
    Write-Host "[FAIL] go build error: $_" -ForegroundColor Red
}

# ---------- 6. Available commands ----------
Write-Host ""
Write-Host "=== Available Commands ===" -ForegroundColor Cyan
Write-Host "  go build ./...                                                    # Full build"
Write-Host "  go test -race -count=1 ./api/... ./core/... ./infra/... ./tests/... # Full test"
Write-Host "  go test -race ./core/...                                          # Core tests"
Write-Host "  go run ./cmd/playthread/ -config config.yaml                      # Start server"
Write-Host "  Open http://localhost:18800/dashboard                             # Dashboard"
Write-Host ""
Write-Host "Environment ready!" -ForegroundColor Green
