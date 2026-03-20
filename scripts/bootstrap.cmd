@echo off
REM Playthread-Go Environment Bootstrap (cmd version)
REM Usage: scripts\bootstrap.cmd
REM Sets Go amd64 + MinGW gcc env vars and verifies build

echo === Playthread-Go Environment Bootstrap ===

REM ---------- Locate Go amd64 ----------
set "GO_EXE="
if exist "C:\go_amd64\go\bin\go.exe" (
    set "GO_EXE=C:\go_amd64\go\bin\go.exe"
    set "GOROOT=C:\go_amd64\go"
    set "PATH=C:\go_amd64\go\bin;%PATH%"
) else (
    where go >nul 2>&1
    if %ERRORLEVEL%==0 (
        for /f "tokens=*" %%i in ('where go') do set "GO_EXE=%%i"
    ) else (
        echo [FAIL] Go not found. Install Go 1.25+ amd64
        exit /b 1
    )
)

echo [OK] Go:
%GO_EXE% version

REM ---------- Locate gcc ----------
if exist "C:\mingw64\bin\gcc.exe" (
    set "PATH=C:\mingw64\bin;%PATH%"
    echo [OK] gcc:
    gcc --version 2>&1 | findstr /n "^" | findstr "^1:"
) else (
    where gcc >nul 2>&1
    if %ERRORLEVEL%==0 (
        echo [OK] gcc found in PATH
    ) else (
        echo [WARN] gcc not found, audio-service build requires MinGW-w64
    )
)

REM ---------- Set env vars ----------
set CGO_ENABLED=1
set GOARCH=amd64
set GOOS=windows

echo.
echo === Environment Variables ===
echo   CGO_ENABLED=%CGO_ENABLED%
echo   GOARCH=%GOARCH%
echo   GOOS=%GOOS%
echo   GOROOT=%GOROOT%

REM ---------- Verify build ----------
echo.
echo === Verifying Build ===
%GO_EXE% build ./...
if %ERRORLEVEL%==0 (
    echo [OK] go build ./... passed
) else (
    echo [FAIL] go build failed
    exit /b 1
)

echo.
echo === Available Commands ===
echo   go build ./...                                                    # Full build
echo   go test -race -count=1 ./api/... ./core/... ./infra/... ./tests/... # Full test
echo   go run ./cmd/playthread/ -config config.yaml                      # Start server
echo   Open http://localhost:18800/dashboard                             # Dashboard
echo.
echo Environment ready!
