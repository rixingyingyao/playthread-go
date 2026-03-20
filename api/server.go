package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rs/zerolog/log"
)

// Server API 服务器，聚合 HTTP + WebSocket + UDP
type Server struct {
	cfg      *infra.ServerConfig
	apiToken string // 缓存 APIToken 供安全端点校验
	pt       *core.PlayThread
	hub      *WSHub
	router   chi.Router
	httpSrv  *http.Server
	udp      *UDPListener

	// 基础设施组件（Phase 7/8 接入）
	dsMgr     *infra.DataSourceManager
	fileCache *infra.FileCache
	offline   *infra.OfflineStore
	updater   *infra.Updater
	monitor   *infra.Monitor
}

// NewServer 创建 API 服务器
func NewServer(cfg *infra.Config, pt *core.PlayThread) *Server {
	s := &Server{
		cfg:      &cfg.Server,
		apiToken: cfg.Server.APIToken,
		pt:       pt,
		hub:      NewWSHub(),
	}

	pt.EventBus().Subscribe(s.hub)

	s.router = s.buildRouter(&cfg.Server)
	return s
}

func (s *Server) buildRouter(cfg *infra.ServerConfig) chi.Router {
	r := chi.NewRouter()

	r.Use(Recoverer)
	r.Use(RequestID)
	r.Use(Logger)
	r.Use(CORSWithOrigins(cfg.AllowedOrigins))

	if cfg.RateLimitRPS > 0 {
		r.Use(NewRateLimiter(cfg.RateLimitRPS).Handler)
	}
	if cfg.APIToken != "" {
		r.Use(TokenAuth(cfg.APIToken))
	}

	r.Route("/api/v1", func(r chi.Router) {
		// 查询
		r.Get("/status", s.handleGetStatus)
		r.Get("/progress", s.handleGetProgress)
		r.Get("/playlist", s.handleGetPlaylist)

		// 播出控制
		r.Post("/control/play", s.handlePlay)
		r.Post("/control/pause", s.handlePause)
		r.Post("/control/stop", s.handleStop)
		r.Post("/control/next", s.handleNext)
		r.Post("/control/jump", s.handleJump)
		r.Post("/control/status", s.handleChangeStatus)

		// 垫乐
		r.Post("/control/blank/start", s.handleBlankStart)
		r.Post("/control/blank/stop", s.handleBlankStop)

		// 通道保持
		r.Post("/control/delay/start", s.handleDelayStart)
		r.Post("/control/delay/stop", s.handleDelayStop)

		// 插播
		r.Post("/intercut/start", s.handleIntercutStart)
		r.Post("/intercut/stop", s.handleIntercutStop)

		// 播表
		r.Post("/playlist/load", s.handleLoadPlaylist)
	})

	// WebSocket
	wsPath := cfg.WSPath
	if wsPath == "" {
		wsPath = "/ws/playback"
	}
	r.Get(wsPath, s.hub.ServeWS)

	// pprof 调试端点（仅限 localhost 访问）
	r.Route("/debug/pprof", func(r chi.Router) {
		r.Use(LocalhostOnly)
		r.Get("/", pprof.Index)
		r.Get("/cmdline", pprof.Cmdline)
		r.Get("/profile", pprof.Profile)
		r.Get("/symbol", pprof.Symbol)
		r.Get("/trace", pprof.Trace)
		r.Handle("/goroutine", pprof.Handler("goroutine"))
		r.Handle("/heap", pprof.Handler("heap"))
		r.Handle("/allocs", pprof.Handler("allocs"))
		r.Handle("/block", pprof.Handler("block"))
		r.Handle("/mutex", pprof.Handler("mutex"))
		r.Handle("/threadcreate", pprof.Handler("threadcreate"))
	})

	// 基础设施诊断端点
	r.Route("/api/v1/infra", func(r chi.Router) {
		r.Get("/datasource", s.handleGetDatasource)
		r.Get("/monitor", s.handleGetMonitor)
		r.Post("/update", s.handleTriggerUpdate)
		r.Get("/system", s.handleSystemInfo)
		r.Get("/goroutines", s.handleGoroutines)
	})

	// 可视化监控仪表盘
	r.Get("/dashboard", s.handleDashboard)

	return r
}

// Router 返回 chi.Router（测试用）
func (s *Server) Router() http.Handler {
	return s.router
}

// Start 启动 HTTP + WebSocket Hub + UDP
func (s *Server) Start(ctx context.Context) error {
	go s.hub.Run(ctx)

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if s.cfg.UDPAddr != "" {
		s.udp = NewUDPListener(s.cfg.UDPAddr, s.pt)
		go func() {
			if err := s.udp.Run(ctx); err != nil {
				log.Error().Err(err).Msg("UDP 监听异常退出")
			}
		}()
	}

	log.Info().Str("addr", addr).Str("ws", s.cfg.WSPath).Msg("API 服务启动")

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpSrv.Shutdown(shutCtx); err != nil {
			log.Error().Err(err).Msg("HTTP 服务关闭异常")
		}
	}()

	if err := s.httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP 服务异常: %w", err)
	}
	return nil
}

// Hub 返回 WebSocket Hub（测试用）
func (s *Server) Hub() *WSHub {
	return s.hub
}

// SetInfra 注入基础设施组件（数据源/缓存/断网暂存/升级/监控）
func (s *Server) SetInfra(ds *infra.DataSourceManager, fc *infra.FileCache, off *infra.OfflineStore, upd *infra.Updater, mon *infra.Monitor) {
	s.dsMgr = ds
	s.fileCache = fc
	s.offline = off
	s.updater = upd
	s.monitor = mon
}
