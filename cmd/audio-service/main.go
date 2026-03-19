// audio-service 播放服务子进程入口。
// CGO_ENABLED=1 构建，负责 BASS 音频引擎操作。
// 通过 stdin/stdout JSON Line 协议与主控进程通信。
// stdout 独占用于 IPC，所有日志必须输出到 stderr。
package main

import (
	"context"
	"os"

	"github.com/rixingyingyao/playthread-go/audio"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	log.Logger = zerolog.New(os.Stderr).With().
		Timestamp().
		Str("proc", "audio").
		Logger()

	log.Info().Str("version", Version).Msg("audio-service 子进程启动")

	engine := audio.NewBassEngine()
	server := audio.NewIPCServer(engine)
	engine.SetServer(server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	log.Info().Msg("IPC 服务端启动，等待命令")

	go func() {
		<-server.ShutdownCh()
		cancel()
	}()

	server.Run()

	cancel()
	log.Info().Msg("audio-service 子进程退出")
}
