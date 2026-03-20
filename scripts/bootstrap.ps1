# Playthread-Go 环境引导脚本
# 用法: .\scripts\bootstrap.ps1
# 功能: 检测并配置 Go amd64 + MinGW gcc + BASS 环境，设置当前会话环境变量
#
# 前提条件:
#   - Go 1.25+ (amd64) 安装在 C:\go_amd64\go 或系统 PATH
#   - MinGW-w64 gcc 安装在 C:\mingw64\bin 或系统 PATH
#   - BASS 库已放置在 audio/libs/windows/ (仓库自带)

$ErrorActionPreference = "Stop"

Write-Host "=== Playthread-Go 环境检测 ===" -ForegroundColor Cyan

# ---------- 1. 定位 Go amd64 ----------
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
    Write-Host "[FAIL] 未找到 Go。请安装 Go 1.25+ amd64 到 C:\go_amd64\go 或加入 PATH" -ForegroundColor Red
    exit 1
}

# 验证架构
$goEnv = & $goExe env GOARCH GOROOT
$goArch = $goEnv[0]
$goRoot = $goEnv[1]

if ($goArch -ne "amd64") {
    Write-Host "[FAIL] Go 架构为 $goArch，需要 amd64。请安装 64 位 Go" -ForegroundColor Red
    exit 1
}

$goVersion = & $goExe version
Write-Host "[OK] Go: $goVersion" -ForegroundColor Green
Write-Host "     GOROOT: $goRoot" -ForegroundColor Gray

# 设置 GOROOT 和 PATH
$env:GOROOT = $goRoot
$goBin = Split-Path $goExe
if ($env:PATH -notlike "*$goBin*") {
    $env:PATH = "$goBin;$env:PATH"
}

# ---------- 2. 定位 MinGW gcc ----------
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
    Write-Host "[WARN] 未找到 gcc。audio-service 构建需要 MinGW-w64 gcc" -ForegroundColor Yellow
    Write-Host "       主控进程 (CGO_ENABLED=0) 仍可正常构建" -ForegroundColor Yellow
} else {
    $gccVersion = & $gccExe --version | Select-Object -First 1
    Write-Host "[OK] gcc: $gccVersion" -ForegroundColor Green

    $gccBin = Split-Path $gccExe
    if ($env:PATH -notlike "*$gccBin*") {
        $env:PATH = "$gccBin;$env:PATH"
    }
}

# ---------- 3. 检查 BASS 库 ----------
$bassDir = Join-Path $PSScriptRoot "..\audio\libs\windows"
$bassLib = Join-Path $bassDir "libbass.a"
$bassDll = Join-Path $bassDir "bass.dll"

if ((Test-Path $bassLib) -and (Test-Path $bassDll)) {
    Write-Host "[OK] BASS 库: libbass.a + bass.dll" -ForegroundColor Green
} else {
    Write-Host "[WARN] BASS 库不完整 ($bassDir)" -ForegroundColor Yellow
    Write-Host "       需要 libbass.a 和 bass.dll (x64)" -ForegroundColor Yellow
}

# ---------- 4. 设置 CGO 环境变量 ----------
$env:CGO_ENABLED = "1"
$env:GOARCH = "amd64"
$env:GOOS = "windows"

Write-Host ""
Write-Host "=== 环境变量已设置 ===" -ForegroundColor Cyan
Write-Host "  CGO_ENABLED = $env:CGO_ENABLED"
Write-Host "  GOARCH      = $env:GOARCH"
Write-Host "  GOOS        = $env:GOOS"
Write-Host "  GOROOT      = $env:GOROOT"

# ---------- 5. 验证构建 ----------
Write-Host ""
Write-Host "=== 验证构建 ===" -ForegroundColor Cyan

try {
    $buildOut = & $goExe build ./... 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] go build ./... 通过" -ForegroundColor Green
    } else {
        Write-Host "[FAIL] go build 失败:" -ForegroundColor Red
        $buildOut | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
    }
} catch {
    Write-Host "[FAIL] go build 异常: $_" -ForegroundColor Red
}

# ---------- 6. 快速测试提示 ----------
Write-Host ""
Write-Host "=== 可用命令 ===" -ForegroundColor Cyan
Write-Host "  go build ./...                                         # 全量构建"
Write-Host "  go test -race -count=1 ./api/... ./core/... ./infra/... ./tests/...  # 全量测试"
Write-Host "  go test -race ./core/...                               # 核心逻辑测试"
Write-Host "  go run ./cmd/playthread/ -config config.yaml           # 启动主控进程"
Write-Host "  浏览器打开 http://localhost:18800/dashboard            # 可视化仪表盘"
Write-Host ""
Write-Host "环境就绪！" -ForegroundColor Green
