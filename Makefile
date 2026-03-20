# Playthread-Go 构建 Makefile
# 双进程架构：playthread.exe (纯 Go) + audio-service.exe (cgo/BASS) + watchdog.exe

VERSION ?= dev
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 输出目录
OUT_DIR := bin

.PHONY: all clean playthread audio-service watchdog build-release build-linux-master dev test test-race test-bench vet

all: playthread audio-service

# 主控进程（纯 Go，CGO_ENABLED=0）
playthread:
	@echo "=== 构建主控进程 ==="
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/playthread.exe ./cmd/playthread/

# 播放服务子进程（cgo，需要 gcc + bass.dll）
audio-service:
	@echo "=== 构建播放服务子进程 ==="
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/audio-service.exe ./cmd/audio-service/

# 守护进程（纯 Go）
watchdog:
	@echo "=== 构建守护进程 ==="
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/watchdog.exe ./cmd/watchdog/

# 生产构建（全部三个二进制）
build-release: playthread audio-service watchdog

# Linux 交叉编译（仅主控，播放服务需要目标平台 gcc+BASS）
build-linux-master:
	@echo "=== Linux 交叉编译主控进程 ==="
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/playthread ./cmd/playthread/

# 仅构建主控进程（开发机无 gcc 时使用）
dev:
	@echo "=== 开发模式：仅构建主控进程 ==="
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(OUT_DIR)/playthread.exe ./cmd/playthread/

test:
	CGO_ENABLED=0 go test ./models/... ./bridge/... ./infra/... ./db/...
	CGO_ENABLED=1 go test ./audio/...

test-race:
	go test -race -count=1 ./...

test-bench:
	go test -bench=. -benchmem ./core/... ./bridge/...

vet:
	CGO_ENABLED=0 go vet ./models/... ./bridge/... ./infra/... ./db/... ./cmd/playthread/...
	CGO_ENABLED=1 go vet ./audio/... ./cmd/audio-service/...

clean:
	rm -rf $(OUT_DIR)
