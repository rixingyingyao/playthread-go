// playthread 主控进程入口。
// 纯 Go 构建（CGO_ENABLED=0），负责业务编排、定时调度和 API 服务。
// 通过 ProcessManager 拉起 audio-service 子进程进行音频播放。
package main

import (
	"context"
	"flag"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/rixingyingyao/playthread-go/api"
	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/db"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/infra/platform"
	"github.com/rs/zerolog/log"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := infra.LoadConfig(*configPath)
	if err != nil {
		cfg = infra.DefaultConfig()
		log.Warn().Err(err).Msg("加载配置文件失败，使用默认配置")
	}

	logCloser := infra.InitLogger(cfg.Log)
	defer func() {
		if c, ok := logCloser.(io.Closer); ok {
			c.Close()
		}
	}()

	platform.SetHighResTimer()
	defer platform.RestoreTimer()

	log.Info().Str("version", Version).Msg("Playthread 主控进程启动")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		log.Fatal().Err(err).Msg("打开数据库失败")
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatal().Err(err).Msg("数据库迁移失败")
	}

	snapshotMgr := infra.NewSnapshotManager("data")
	if snapInfo, err := snapshotMgr.Load(); err == nil && snapInfo != nil {
		log.Info().
			Str("program", snapInfo.ProgramID).
			Int("position_ms", snapInfo.Position).
			Str("status", snapInfo.Status.String()).
			Msg("检测到冷启动快照，后续 Phase 3 实现恢复逻辑")
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Fatal().Err(err).Msg("获取可执行文件路径失败")
	}
	audioExeName := "audio-service"
	if runtime.GOOS == "windows" {
		audioExeName += ".exe"
	}
	audioExePath := filepath.Join(filepath.Dir(exePath), audioExeName)

	pm := bridge.NewProcessManager(audioExePath, 5*time.Second)
	pm.SetEventHandler(func(evt *bridge.IPCEvent) {
		log.Info().Str("event", evt.Event).Msg("收到子进程事件")
	})

	if err := pm.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("启动播放服务子进程失败")
	}
	defer pm.Stop()

	log.Info().Msg("播放服务子进程已启动")

	// 核心编排器
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	pt := core.NewPlayThread(cfg, sm, eb, pm.Bridge(), snapshotMgr)
	pt.Run(ctx)

	// API 服务（HTTP + WebSocket + UDP）
	apiSrv := api.NewServer(cfg, pt)
	go func() {
		if err := apiSrv.Start(ctx); err != nil {
			log.Error().Err(err).Msg("API 服务异常退出")
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("收到退出信号，开始优雅关闭")

	cancel()
	pt.Wait()
	log.Info().Msg("Playthread 主控进程退出")
}
