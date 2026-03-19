// audio-service 播放服务子进程入口。
// CGO_ENABLED=1 构建，负责 BASS 音频引擎操作。
// 通过 stdin/stdout JSON Line 协议与主控进程通信。
// ★ stdout 独占用于 IPC，所有日志必须输出到 stderr。
package main

import (
	"context"
	"os"

	"github.com/rixingyingyao/playthread-go/audio"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// ★ 最早时机：将日志输出重定向到 stderr，保护 stdout IPC 通道
	audio.InitLogging()
	log.Info().Msg("audio-service 子进程启动")

	// 初始化 BASS 引擎
	engine := audio.NewBassEngine()

	// 创建 IPC 服务端
	server := audio.NewIPCServer(engine)
	engine.SetServer(server)

	// 启动 BASS 引擎 goroutine（LockOSThread）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	// 初始化文件日志（stderr）
	initStderrLogger()

	log.Info().Msg("IPC 服务端启动，等待命令")

	// 阻塞运行 IPC 服务端主循环（读 stdin）
	server.Run()

	// stdin 关闭后清理
	cancel()
	log.Info().Msg("audio-service 子进程退出")
	os.Exit(0)
}

func initStderrLogger() {
	// 子进程日志仅输出到 stderr
	// 主控通过 ProcessManager.drainStderr 读取并转发到主控日志
	log.Logger = zerolog.New(os.Stderr).With().
		Timestamp().
		Str("proc", "audio").
		Logger()
}
