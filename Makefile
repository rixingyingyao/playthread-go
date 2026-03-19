# Playthread-Go 构建 Makefile
# 双进程架构：playthread.exe (纯 Go) + audio-service.exe (cgo/BASS)

VERSION ?= dev
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 输出目录
OUT_DIR := bin

.PHONY: all clean playthread audio-service test vet

all: playthread audio-service

# 主控进程（纯 Go，CGO_ENABLED=0）
playthread:
	@echo "=== 构建主控进程 ==="
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/playthread.exe ./cmd/playthread/

# 播放服务子进程（cgo，需要 gcc + bass.dll）
audio-service:
	@echo "=== 构建播放服务子进程 ==="
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/audio-service.exe ./cmd/audio-service/

# 仅构建主控进程（开发机无 gcc 时使用）
dev:
	@echo "=== 开发模式：仅构建主控进程 ==="
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(OUT_DIR)/playthread.exe ./cmd/playthread/

test:
	CGO_ENABLED=0 go test ./bridge/... ./infra/...
	CGO_ENABLED=1 go test ./audio/...

vet:
	CGO_ENABLED=0 go vet ./bridge/... ./infra/... ./cmd/playthread/...
	CGO_ENABLED=1 go vet ./audio/... ./cmd/audio-service/...

clean:
	rm -rf $(OUT_DIR)
