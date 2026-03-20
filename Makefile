# Playthread-Go 构建 Makefile
# 双进程架构：playthread.exe (纯 Go) + audio-service.exe (cgo/BASS)
#
# 环境要求:
#   Go 1.25+ amd64   — 默认 C:\go_amd64\go\bin\go.exe 或 PATH 中的 go
#   MinGW-w64 gcc     — 默认 C:\mingw64\bin\gcc.exe 或 PATH 中的 gcc
#   BASS x64          — audio/libs/windows/libbass.a + bass.dll（仓库自带）
#
# Windows 快速入口:
#   powershell -File scripts\bootstrap.ps1   # 检测+配置环境+验证构建
#
# 或手动设置后使用 make:
#   set CGO_ENABLED=1 && set GOARCH=amd64 && make all

VERSION ?= dev
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go 可执行文件（支持自定义路径）
GO ?= go

# 输出目录
OUT_DIR := bin

.PHONY: all clean playthread audio-service build-release build-linux-master dev test test-race test-bench vet check-env

# ---------- 环境检查 ----------
check-env:
	@echo "=== 环境检查 ==="
	@$(GO) version
	@$(GO) env GOARCH GOOS CGO_ENABLED
	@echo "---"
	@gcc --version 2>/dev/null | head -1 || echo "[WARN] gcc 未找到，audio-service 构建需要 MinGW-w64"
	@test -f audio/libs/windows/libbass.a && echo "[OK] BASS 库存在" || echo "[WARN] BASS 库缺失"

all: playthread audio-service

# 主控进程（纯 Go，CGO_ENABLED=0）
playthread:
	@echo "=== 构建主控进程 ==="
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(OUT_DIR)/playthread.exe ./cmd/playthread/

# 播放服务子进程（cgo，需要 gcc + bass.dll）
audio-service:
	@echo "=== 构建播放服务子进程 ==="
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(OUT_DIR)/audio-service.exe ./cmd/audio-service/

# 生产构建
build-release: playthread audio-service

# Linux 交叉编译（仅主控，播放服务需要目标平台 gcc+BASS）
build-linux-master:
	@echo "=== Linux 交叉编译主控进程 ==="
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(OUT_DIR)/playthread ./cmd/playthread/

# 仅构建主控进程（开发机无 gcc 时使用）
dev:
	@echo "=== 开发模式：仅构建主控进程 ==="
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o $(OUT_DIR)/playthread.exe ./cmd/playthread/

# ---------- 测试 ----------
# 全量测试（需要 CGO_ENABLED=1 + gcc）
test:
	CGO_ENABLED=1 $(GO) test -count=1 ./api/... ./core/... ./infra/... ./tests/...

# 全量测试 + 竞态检测
test-race:
	CGO_ENABLED=1 $(GO) test -race -count=1 ./api/... ./core/... ./infra/... ./tests/...

# 基准测试
test-bench:
	$(GO) test -bench=. -benchmem ./core/... ./bridge/...

# ---------- 代码检查 ----------
vet:
	CGO_ENABLED=1 $(GO) vet ./...

# ---------- 全量构建验证（CI 友好） ----------
ci: check-env vet test-race build-release
	@echo "=== CI 验证通过 ==="

clean:
	rm -rf $(OUT_DIR)
