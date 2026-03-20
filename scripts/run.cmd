@echo off
REM Playthread-Go one-click build and run script
REM Builds both executables, creates config and directories, then starts the service.

setlocal enabledelayedexpansion

REM --- Ensure working directory is correct (admin mode may change cwd) ---
cd /d "%~dp0.."
set "ROOT=%CD%"
set "BIN_DIR=%ROOT%\bin"
set "GO_EXE=C:\go_amd64\go\bin\go.exe"

REM --- Check Go ---
if not exist "%GO_EXE%" (
    where go >nul 2>&1
    if errorlevel 1 (
        echo [ERROR] Go compiler not found. Install Go amd64 or set GO_EXE path.
        goto :fail
    )
    set "GO_EXE=go"
)

echo === Playthread-Go Build and Run ===
echo    Root: %ROOT%
echo.

REM --- Create output directory ---
if not exist "%BIN_DIR%" mkdir "%BIN_DIR%"

REM --- Build playthread.exe (pure Go, CGO_ENABLED=0) ---
echo [1/4] Building playthread.exe ...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
"%GO_EXE%" build -ldflags "-s -w" -o "%BIN_DIR%\playthread.exe" ./cmd/playthread/
if errorlevel 1 (
    echo [ERROR] Failed to build playthread.exe
    goto :fail
)
echo       OK

REM --- Build audio-service.exe (CGO, needs gcc + BASS) ---
echo [2/4] Building audio-service.exe ...
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64
"%GO_EXE%" build -ldflags "-s -w" -o "%BIN_DIR%\audio-service.exe" ./cmd/audio-service/
if errorlevel 1 (
    echo [ERROR] Failed to build audio-service.exe (needs MinGW-w64 gcc + BASS libs)
    goto :fail
)
echo       OK

REM --- Copy bass.dll ---
echo [3/4] Preparing runtime files ...
if exist "%ROOT%\audio\libs\windows\bass.dll" (
    copy /Y "%ROOT%\audio\libs\windows\bass.dll" "%BIN_DIR%\bass.dll" >nul
    echo       bass.dll copied
) else (
    echo [WARN] bass.dll not found in audio\libs\windows\
)

REM --- Create directories ---
if not exist "%BIN_DIR%\data" mkdir "%BIN_DIR%\data"
if not exist "%BIN_DIR%\data\offline" mkdir "%BIN_DIR%\data\offline"
if not exist "%BIN_DIR%\logs" mkdir "%BIN_DIR%\logs"
if not exist "%BIN_DIR%\cache" mkdir "%BIN_DIR%\cache"
if not exist "%BIN_DIR%\cache\media" mkdir "%BIN_DIR%\cache\media"
if not exist "%BIN_DIR%\padding" mkdir "%BIN_DIR%\padding"
echo       Directories ready

REM --- Create default config.yaml if not exists ---
if not exist "%BIN_DIR%\config.yaml" (
    echo       Creating default config.yaml ...
    copy /Y "%ROOT%\scripts\config.default.yaml" "%BIN_DIR%\config.yaml" >nul 2>&1
    if errorlevel 1 (
        echo [WARN] config.default.yaml not found, service will use built-in defaults
    ) else (
        echo       config.yaml created
    )
) else (
    echo       config.yaml already exists, skipping
)

REM --- Launch ---
echo.
echo [4/4] Starting Playthread ...
echo ============================================
echo   HTTP API:    http://localhost:18800
echo   Dashboard:   http://localhost:18800/dashboard
echo   WebSocket:   ws://localhost:18800/ws/playback
echo   UDP:         127.0.0.1:18820
echo ============================================
echo   Press Ctrl+C to stop
echo.

cd /d "%BIN_DIR%"
"%BIN_DIR%\playthread.exe" -config config.yaml

echo.
echo Playthread has exited.
pause
goto :eof

:fail
echo.
echo === Build or startup failed. See errors above. ===
pause
