@echo off
REM Playthread-Go 环境引导 (cmd 版本)
REM 用法: scripts\bootstrap.cmd
REM 设置 Go amd64 + MinGW gcc 环境变量后验证构建

echo === Playthread-Go 环境引导 ===

REM ---------- 定位 Go amd64 ----------
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
        echo [FAIL] 未找到 Go。请安装 Go 1.25+ amd64
        exit /b 1
    )
)

echo [OK] Go: 
%GO_EXE% version

REM ---------- 定位 gcc ----------
if exist "C:\mingw64\bin\gcc.exe" (
    set "PATH=C:\mingw64\bin;%PATH%"
    echo [OK] gcc:
    gcc --version 2>&1 | findstr /n "^" | findstr "^1:"
) else (
    where gcc >nul 2>&1
    if %ERRORLEVEL%==0 (
        echo [OK] gcc found in PATH
    ) else (
        echo [WARN] gcc 未找到，audio-service 构建需要 MinGW-w64
    )
)

REM ---------- 设置环境变量 ----------
set CGO_ENABLED=1
set GOARCH=amd64
set GOOS=windows

echo.
echo === 环境变量 ===
echo   CGO_ENABLED=%CGO_ENABLED%
echo   GOARCH=%GOARCH%
echo   GOOS=%GOOS%
echo   GOROOT=%GOROOT%

REM ---------- 验证构建 ----------
echo.
echo === 验证构建 ===
%GO_EXE% build ./...
if %ERRORLEVEL%==0 (
    echo [OK] go build ./... 通过
) else (
    echo [FAIL] go build 失败
    exit /b 1
)

echo.
echo === 可用命令 ===
echo   go build ./...                                                    # 全量构建
echo   go test -race -count=1 ./api/... ./core/... ./infra/... ./tests/...  # 全量测试
echo   go run ./cmd/playthread/ -config config.yaml                      # 启动主控
echo   浏览器访问 http://localhost:18800/dashboard                        # 可视化仪表盘
echo.
echo 环境就绪！
