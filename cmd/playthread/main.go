// playthread 主控进程入口。
// 纯 Go 构建（CGO_ENABLED=0），负责业务编排、定时调度和 API 服务。
// 通过 ProcessManager 拉起 audio-service 子进程进行音频播放。
package main

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 单例检查：防止多开
	lockHandle, err := platform.AcquireSingletonLock("playthread-go")
	if err != nil {
		if err == platform.ErrAlreadyRunning {
			fmt.Fprintln(os.Stderr, "错误: Playthread 已在运行，不允许多开")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "单例检查失败: %v\n", err)
		os.Exit(1)
	}
	defer platform.ReleaseSingletonLock(lockHandle)

	// 检测是否以 Windows 服务模式运行
	interactive, err := platform.IsInteractiveSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "检测运行模式失败: %v\n", err)
		interactive = true // 回退到控制台模式
	}

	if !interactive {
		// Windows 服务模式：由 SCM 管理生命周期
		cfgPath := *configPath
		if svcErr := platform.RunAsService("PlaythreadGo", func(ctx context.Context) {
			runApp(ctx, cfgPath)
		}); svcErr != nil {
			fmt.Fprintf(os.Stderr, "Windows 服务运行失败: %v\n", svcErr)
			os.Exit(1)
		}
		return
	}

	// 控制台模式：信号驱动生命周期
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runApp(ctx, *configPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("收到退出信号，开始优雅关闭")
	cancel()

	// 等待 runApp 中的组件退出
	time.Sleep(2 * time.Second)
	log.Info().Msg("Playthread 主控进程退出")
}

// runApp 实际业务逻辑入口，ctx 取消时优雅退出
func runApp(ctx context.Context, configPath string) {
	cfg, err := infra.LoadConfig(configPath)
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

	// 运行时监控
	monitor := infra.NewMonitor(&cfg.Monitor, "data")
	go monitor.Run(ctx)

	// 数据源管理器（云端/中心双数据源 + 心跳降级）
	dsMgr := infra.NewDataSourceManager(cfg.DataSource)
	dsMgr.OnDegrade(func(from, to infra.SourceType) {
		log.Warn().Str("from", from.String()).Str("to", to.String()).Msg("数据源降级切换")
	})
	dsMgr.OnHeartbeat(func(src infra.SourceType, ok bool) {
		log.Debug().Str("source", src.String()).Bool("ok", ok).Msg("数据源心跳")
	})

	// 素材文件缓存
	fileCache := infra.NewFileCache(cfg.FileCache)

	// 断网暂存
	offlineStore := infra.NewOfflineStore(cfg.Offline)
	go offlineStore.RunFlush(ctx)

	// 注入基础设施组件后再启动数据源管理器
	dsMgr.SetInfra(fileCache, offlineStore)
	go dsMgr.Run(ctx)

	// 自升级管理器
	updater := infra.NewUpdater(Version)

	// 核心编排器
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	pt := core.NewPlayThread(cfg, sm, eb, pm.Bridge(), snapshotMgr)

	// 播单接收回调：数据源 → PlayThread
	dsMgr.OnPlaylist(func(pl *models.Playlist) {
		log.Info().Str("id", pl.ID).Msg("从数据源收到新播单")
		pt.SetPlaylist(pl)
	})

	pt.Run(ctx)

	// API 服务（HTTP + WebSocket + UDP）
	apiSrv := api.NewServer(cfg, pt)
	apiSrv.SetInfra(dsMgr, fileCache, offlineStore, updater, monitor)
	go func() {
		if err := apiSrv.Start(ctx); err != nil {
			log.Error().Err(err).Msg("API 服务异常退出")
		}
	}()

	// 等待 ctx 取消（由 main 或 Windows Service 触发）
	<-ctx.Done()
	pt.Wait()
	log.Info().Msg("Playthread 业务逻辑已退出")
}
