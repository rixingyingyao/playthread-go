// playthread 主控进程入口。
// 纯 Go 构建（CGO_ENABLED=0），负责业务编排、定时调度和 API 服务。
// 通过 ProcessManager 拉起 audio-service 子进程进行音频播放。
package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rixingyingyao/playthread-go/infra/platform"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/lumberjack.v2"
)

func main() {
	// 初始化日志
	initLogger()

	// Windows 高精度定时器
	platform.SetHighResTimer()
	defer platform.RestoreTimer()

	log.Info().Msg("Playthread 主控进程启动")

	// 主上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 计算播放服务子进程路径
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal().Err(err).Msg("获取可执行文件路径失败")
	}
	audioExePath := filepath.Join(filepath.Dir(exePath), "audio-service.exe")

	// 启动子进程管理器
	pm := bridge.NewProcessManager(audioExePath, 5*time.Second)
	pm.SetEventHandler(func(evt *bridge.IPCEvent) {
		log.Info().Str("event", evt.Event).Msg("收到子进程事件")
		// TODO: Phase 2 分发到业务层
	})

	if err := pm.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("启动播放服务子进程失败")
	}
	defer pm.Stop()

	log.Info().Msg("播放服务子进程已启动")

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("收到退出信号，开始优雅关闭")

	cancel()
	log.Info().Msg("Playthread 主控进程退出")
}

func initLogger() {
	fileWriter := &lumberjack.Logger{
		Filename:   "logs/playthread.log",
		MaxSize:    50,
		MaxBackups: 30,
		MaxAge:     30,
		Compress:   true,
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}

	multi := zerolog.MultiLevelWriter(consoleWriter, fileWriter)
	log.Logger = zerolog.New(multi).With().Timestamp().Caller().Logger()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}
