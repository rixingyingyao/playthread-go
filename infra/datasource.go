package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// SourceType 数据源类型
type SourceType int

const (
	SourceCloud  SourceType = iota // 云端（SAAS）
	SourceCenter                   // 本地中心
)

func (s SourceType) String() string {
	if s == SourceCloud {
		return "Cloud"
	}
	return "Center"
}

// DegradeState 降级状态
type DegradeState int

const (
	DegradeNone      DegradeState = iota // 正常（使用云端）
	DegradeFallback                      // 已降级（使用本地中心）
	DegradeConfirm                       // 待确认回切
)

func (d DegradeState) String() string {
	switch d {
	case DegradeNone:
		return "Normal"
	case DegradeFallback:
		return "Fallback"
	case DegradeConfirm:
		return "PendingConfirm"
	default:
		return fmt.Sprintf("DegradeState(%d)", d)
	}
}

// DataSourceConfig 数据源配置
type DataSourceConfig struct {
	CloudURL             string        `yaml:"cloud_url"`              // 云端 API 地址
	CenterURL            string        `yaml:"center_url"`             // 本地中心 API 地址
	HeartbeatInterval    time.Duration `yaml:"heartbeat_interval"`     // 心跳间隔，默认 5s
	PlaylistPollInterval time.Duration `yaml:"playlist_poll_interval"` // 播单轮询间隔，默认 60s
	FailThreshold        int           `yaml:"fail_threshold"`         // 连续失败阈值触发降级，默认 3
	RecoverThreshold     int           `yaml:"recover_threshold"`      // 连续成功阈值可回切，默认 5
	HeartbeatTimeout     time.Duration `yaml:"heartbeat_timeout"`      // 心跳请求超时，默认 3s
	ClockDriftMaxMs      int64         `yaml:"clock_drift_max_ms"`     // 时钟偏差阈值(ms)，默认 5000
}

// DefaultDataSourceConfig 默认配置
func DefaultDataSourceConfig() DataSourceConfig {
	return DataSourceConfig{
		HeartbeatInterval:    5 * time.Second,
		PlaylistPollInterval: 60 * time.Second,
		FailThreshold:        3,
		RecoverThreshold:     5,
		HeartbeatTimeout:     3 * time.Second,
		ClockDriftMaxMs:      5000,
	}
}

// HeartbeatResponse 心跳响应
type HeartbeatResponse struct {
	OK         bool   `json:"ok"`
	ServerTime int64  `json:"server_time"` // 服务端时间戳(ms)
	Version    string `json:"version"`
}

// PlaylistResponse 播单获取响应
type PlaylistResponse struct {
	Playlist *models.Playlist `json:"playlist"`
	Version  int              `json:"version"`
	Checksum string           `json:"checksum"`
}

// DataSourceManager 双数据源管理器
// 云端为主，本地中心为备。自动检测心跳，连续失败自动降级，恢复后支持手动回切。
type DataSourceManager struct {
	mu sync.RWMutex

	cfg    DataSourceConfig
	active SourceType   // 当前活跃数据源
	state  DegradeState // 降级状态

	cloudFailCount  int // 云端连续心跳失败次数
	cloudOKCount    int // 云端连续心跳成功次数（降级后计数）
	centerFailCount int // 中心连续失败次数

	lastCloudHB  time.Time // 上次云端心跳成功时间
	lastCenterHB time.Time

	client *http.Client

	// 播单版本跟踪（避免重复推送相同播单）
	lastPlaylistID       string
	lastPlaylistVersion  int
	lastPlaylistChecksum string

	// 基础设施组件引用
	fileCache    *FileCache
	offlineStore *OfflineStore

	// 回调
	onDegrade   func(from, to SourceType)    // 降级/回切时通知
	onPlaylist  func(pl *models.Playlist)    // 收到新播单时通知
	onHeartbeat func(src SourceType, ok bool) // 每次心跳结果
}

// NewDataSourceManager 创建数据源管理器
func NewDataSourceManager(cfg DataSourceConfig) *DataSourceManager {
	return &DataSourceManager{
		cfg:    cfg,
		active: SourceCloud,
		state:  DegradeNone,
		client: &http.Client{Timeout: cfg.HeartbeatTimeout},
	}
}

// OnDegrade 设置降级回调
func (dm *DataSourceManager) OnDegrade(fn func(from, to SourceType)) {
	dm.mu.Lock()
	dm.onDegrade = fn
	dm.mu.Unlock()
}

// OnPlaylist 设置播单接收回调
func (dm *DataSourceManager) OnPlaylist(fn func(pl *models.Playlist)) {
	dm.mu.Lock()
	dm.onPlaylist = fn
	dm.mu.Unlock()
}

// OnHeartbeat 设置心跳回调
func (dm *DataSourceManager) OnHeartbeat(fn func(src SourceType, ok bool)) {
	dm.mu.Lock()
	dm.onHeartbeat = fn
	dm.mu.Unlock()
}

// SetInfra 注入基础设施组件（FileCache + OfflineStore）
func (dm *DataSourceManager) SetInfra(fc *FileCache, os_ *OfflineStore) {
	dm.mu.Lock()
	dm.fileCache = fc
	dm.offlineStore = os_
	dm.mu.Unlock()
}

// Run 启动心跳检测 + 播单轮询循环（阻塞，需在 goroutine 中调用）
func (dm *DataSourceManager) Run(ctx context.Context) {
	hbTicker := time.NewTicker(dm.cfg.HeartbeatInterval)
	defer hbTicker.Stop()

	plInterval := dm.cfg.PlaylistPollInterval
	if plInterval <= 0 {
		plInterval = 60 * time.Second
	}
	plTicker := time.NewTicker(plInterval)
	defer plTicker.Stop()

	log.Info().
		Str("cloud", dm.cfg.CloudURL).
		Str("center", dm.cfg.CenterURL).
		Dur("hb_interval", dm.cfg.HeartbeatInterval).
		Dur("pl_interval", plInterval).
		Msg("DataSourceManager 已启动")

	// 启动时立即拉取一次播单
	dm.playlistPoll(ctx)

	for {
		select {
		case <-hbTicker.C:
			dm.heartbeatCycle(ctx)
		case <-plTicker.C:
			dm.playlistPoll(ctx)
		case <-ctx.Done():
			log.Info().Msg("DataSourceManager 已停止")
			return
		}
	}
}

// playlistPoll 从当前活跃数据源拉取今天的播单
func (dm *DataSourceManager) playlistPoll(ctx context.Context) {
	date := time.Now().Format("2006-01-02")
	plResp, err := dm.fetchPlaylistWithMeta(ctx, date)
	if err != nil {
		log.Debug().Err(err).Str("date", date).Msg("播单拉取失败")
		return
	}

	pl := plResp.Playlist

	dm.mu.Lock()
	// 检查是否与上次相同（ID + Version + Checksum 三重判断）
	same := pl.ID == dm.lastPlaylistID &&
		plResp.Version == dm.lastPlaylistVersion &&
		plResp.Checksum == dm.lastPlaylistChecksum
	if same {
		dm.mu.Unlock()
		return
	}
	dm.lastPlaylistID = pl.ID
	dm.lastPlaylistVersion = plResp.Version
	dm.lastPlaylistChecksum = plResp.Checksum
	fn := dm.onPlaylist
	fc := dm.fileCache
	dm.mu.Unlock()

	log.Info().
		Str("id", pl.ID).
		Int("version", plResp.Version).
		Str("date", date).
		Msg("收到新播单")

	// 触发素材预缓存
	if fc != nil {
		go dm.preCachePlaylist(ctx, pl, fc)
	}

	if fn != nil {
		fn(pl)
	}
}

// fetchPlaylistWithMeta 获取播单及元数据（版本、校验值）
func (dm *DataSourceManager) fetchPlaylistWithMeta(ctx context.Context, date string) (*PlaylistResponse, error) {
	baseURL := dm.ActiveURL()
	if baseURL == "" {
		return nil, fmt.Errorf("活跃数据源 URL 未配置")
	}

	url := baseURL + "/api/v1/playlist?date=" + date
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := dm.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求播单失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("播单接口返回 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取播单响应体失败: %w", err)
	}

	var plResp PlaylistResponse
	if err := json.Unmarshal(body, &plResp); err != nil {
		return nil, fmt.Errorf("解析播单响应失败: %w", err)
	}

	if plResp.Playlist == nil {
		return nil, fmt.Errorf("播单为空")
	}

	return &plResp, nil
}

// preCachePlaylist 预缓存播单中所有素材文件
func (dm *DataSourceManager) preCachePlaylist(ctx context.Context, pl *models.Playlist, fc *FileCache) {
	var total, cached, failed int
	for _, block := range pl.Blocks {
		for _, prog := range block.Programs {
			if prog.FilePath == "" || prog.IsSignalSource() {
				continue
			}
			total++
			if fc.Exists(prog.FilePath) {
				fc.Touch(prog.FilePath)
				cached++
				continue
			}
			// 需要下载：使用 PlayUrl 或构造下载 URL
			downloadURL := prog.PlayUrl
			if downloadURL == "" {
				downloadURL = dm.ActiveURL() + "/api/v1/media/" + prog.FilePath
			}
			if err := fc.Download(ctx, downloadURL, prog.FilePath, ""); err != nil {
				log.Warn().Err(err).Str("file", prog.FilePath).Msg("素材预缓存失败")
				failed++
			}
		}
	}
	if total > 0 {
		log.Info().
			Int("total", total).
			Int("cached", cached).
			Int("downloaded", total-cached-failed).
			Int("failed", failed).
			Msg("播单素材预缓存完成")
	}

	// 下载结束后尝试 LRU 清理
	if removed, err := fc.CleanupLRU(); err != nil {
		log.Warn().Err(err).Msg("LRU 清理失败")
	} else if removed > 0 {
		log.Info().Int("removed", removed).Msg("LRU 清理完成")
	}
}

// ActiveSource 返回当前活跃数据源
func (dm *DataSourceManager) ActiveSource() SourceType {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.active
}

// State 返回当前降级状态
func (dm *DataSourceManager) State() DegradeState {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.state
}

// CanSwitchBack 判断是否可以手动回切到云端
func (dm *DataSourceManager) CanSwitchBack() bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.state == DegradeConfirm
}

// SwitchBack 手动回切到云端（运维触发）
// 需要满足: state == DegradeConfirm + 云端心跳连续成功 >= RecoverThreshold
func (dm *DataSourceManager) SwitchBack() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.state != DegradeConfirm {
		return fmt.Errorf("当前状态不允许回切: state=%s", dm.state)
	}

	old := dm.active
	dm.active = SourceCloud
	dm.state = DegradeNone
	dm.cloudFailCount = 0
	dm.cloudOKCount = 0

	log.Info().
		Str("from", old.String()).
		Str("to", SourceCloud.String()).
		Msg("手动回切到云端")

	if dm.onDegrade != nil {
		go dm.onDegrade(old, SourceCloud)
	}

	return nil
}

// ActiveURL 返回当前活跃数据源的 base URL
func (dm *DataSourceManager) ActiveURL() string {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	if dm.active == SourceCloud {
		return dm.cfg.CloudURL
	}
	return dm.cfg.CenterURL
}

// FetchPlaylist 从当前活跃数据源获取播单
func (dm *DataSourceManager) FetchPlaylist(ctx context.Context, date string) (*models.Playlist, error) {
	baseURL := dm.ActiveURL()
	if baseURL == "" {
		return nil, fmt.Errorf("活跃数据源 URL 未配置")
	}

	url := baseURL + "/api/v1/playlist?date=" + date
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := dm.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求播单失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("播单接口返回 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB 限制
	if err != nil {
		return nil, fmt.Errorf("读取播单响应体失败: %w", err)
	}

	var plResp PlaylistResponse
	if err := json.Unmarshal(body, &plResp); err != nil {
		return nil, fmt.Errorf("解析播单响应失败: %w", err)
	}

	if plResp.Playlist == nil {
		return nil, fmt.Errorf("播单为空")
	}

	return plResp.Playlist, nil
}

// heartbeatCycle 一次心跳检测
func (dm *DataSourceManager) heartbeatCycle(ctx context.Context) {
	dm.mu.RLock()
	currentState := dm.state
	dm.mu.RUnlock()

	switch currentState {
	case DegradeNone:
		// 正常：检测云端心跳
		ok := dm.pingSource(ctx, SourceCloud)
		dm.handleCloudHeartbeat(ok)

	case DegradeFallback:
		// 已降级：同时检测云端（判断是否可回切）和中心（判断中心是否也挂了）
		cloudOK := dm.pingSource(ctx, SourceCloud)
		centerOK := dm.pingSource(ctx, SourceCenter)
		dm.handleFallbackHeartbeat(cloudOK, centerOK)

	case DegradeConfirm:
		// 待确认：继续检测云端
		cloudOK := dm.pingSource(ctx, SourceCloud)
		dm.handleConfirmHeartbeat(cloudOK)
	}
}

// pingSource 向指定数据源发送心跳
func (dm *DataSourceManager) pingSource(ctx context.Context, src SourceType) bool {
	var baseURL string
	if src == SourceCloud {
		baseURL = dm.cfg.CloudURL
	} else {
		baseURL = dm.cfg.CenterURL
	}
	if baseURL == "" {
		return false
	}

	url := baseURL + "/api/v1/heartbeat"
	reqCtx, cancel := context.WithTimeout(ctx, dm.cfg.HeartbeatTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := dm.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Str("source", src.String()).Msg("心跳请求失败")
		dm.notifyHeartbeat(src, false)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Int("status", resp.StatusCode).Str("source", src.String()).Msg("心跳响应异常")
		dm.notifyHeartbeat(src, false)
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		dm.notifyHeartbeat(src, false)
		return false
	}

	var hb HeartbeatResponse
	if err := json.Unmarshal(body, &hb); err != nil {
		dm.notifyHeartbeat(src, false)
		return false
	}

	if !hb.OK {
		dm.notifyHeartbeat(src, false)
		return false
	}

	// 时钟偏差检查
	if hb.ServerTime > 0 {
		drift := time.Now().UnixMilli() - hb.ServerTime
		if drift < 0 {
			drift = -drift
		}
		if drift > dm.cfg.ClockDriftMaxMs {
			log.Warn().
				Int64("drift_ms", drift).
				Str("source", src.String()).
				Msg("时钟偏差超阈值")
		}
	}

	dm.notifyHeartbeat(src, true)
	return true
}

func (dm *DataSourceManager) notifyHeartbeat(src SourceType, ok bool) {
	dm.mu.RLock()
	fn := dm.onHeartbeat
	dm.mu.RUnlock()
	if fn != nil {
		fn(src, ok)
	}
}

// handleCloudHeartbeat 正常状态下处理云端心跳
func (dm *DataSourceManager) handleCloudHeartbeat(ok bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if ok {
		dm.cloudFailCount = 0
		dm.lastCloudHB = time.Now()

		// 在线状态下尝试补传离线条目
		if dm.offlineStore != nil && dm.offlineStore.Len() > 0 {
			store := dm.offlineStore
			baseURL := dm.cfg.CloudURL
			go dm.replayOffline(store, baseURL)
		}
		return
	}

	dm.cloudFailCount++
	log.Warn().
		Int("fail_count", dm.cloudFailCount).
		Int("threshold", dm.cfg.FailThreshold).
		Msg("云端心跳失败")

	// 离线暂存心跳失败事件
	if dm.offlineStore != nil {
		_ = dm.offlineStore.Add(OfflineHeartbeat, map[string]interface{}{
			"source":     SourceCloud.String(),
			"fail_count": dm.cloudFailCount,
			"time":       time.Now().Format(time.RFC3339),
		})
	}

	if dm.cloudFailCount >= dm.cfg.FailThreshold {
		// 触发降级
		dm.active = SourceCenter
		dm.state = DegradeFallback
		dm.cloudOKCount = 0

		log.Error().
			Int("fail_count", dm.cloudFailCount).
			Msg("云端连续失败，降级到本地中心")

		// 暂存降级事件
		if dm.offlineStore != nil {
			_ = dm.offlineStore.Add(OfflineEvent, map[string]interface{}{
				"event": "degrade",
				"from":  SourceCloud.String(),
				"to":    SourceCenter.String(),
				"time":  time.Now().Format(time.RFC3339),
			})
		}

		if dm.onDegrade != nil {
			go dm.onDegrade(SourceCloud, SourceCenter)
		}
	}
}

// handleFallbackHeartbeat 降级状态下处理心跳
func (dm *DataSourceManager) handleFallbackHeartbeat(cloudOK, centerOK bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if !centerOK {
		dm.centerFailCount++
		log.Warn().Int("center_fail", dm.centerFailCount).Msg("本地中心也不可用")
	} else {
		dm.centerFailCount = 0
		dm.lastCenterHB = time.Now()
	}

	if cloudOK {
		dm.cloudOKCount++
		dm.cloudFailCount = 0
		dm.lastCloudHB = time.Now()

		if dm.cloudOKCount >= dm.cfg.RecoverThreshold {
			dm.state = DegradeConfirm
			log.Info().
				Int("ok_count", dm.cloudOKCount).
				Msg("云端恢复，进入待确认回切状态")
		}
	} else {
		dm.cloudOKCount = 0
		dm.cloudFailCount++
	}
}

// handleConfirmHeartbeat 待确认状态下处理心跳
func (dm *DataSourceManager) handleConfirmHeartbeat(cloudOK bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if cloudOK {
		dm.cloudOKCount++
		dm.lastCloudHB = time.Now()
		return
	}

	// 云端再次失败，退回降级状态
	dm.state = DegradeFallback
	dm.cloudOKCount = 0
	dm.cloudFailCount = 1

	log.Warn().Msg("待确认期间云端再次失败，退回降级状态")
}

// Status 返回数据源状态摘要（供 API 使用）
type DataSourceStatus struct {
	Active           string `json:"active"`
	State            string `json:"state"`
	CloudURL         string `json:"cloud_url"`
	CenterURL        string `json:"center_url"`
	CloudFailCount   int    `json:"cloud_fail_count"`
	CanSwitchBack    bool   `json:"can_switch_back"`
	LastPlaylistID   string `json:"last_playlist_id,omitempty"`
	LastCloudHB      string `json:"last_cloud_heartbeat,omitempty"`
	LastCenterHB     string `json:"last_center_heartbeat,omitempty"`
}

// StatusSnapshot 获取状态快照
func (dm *DataSourceManager) StatusSnapshot() DataSourceStatus {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	s := DataSourceStatus{
		Active:         dm.active.String(),
		State:          dm.state.String(),
		CloudURL:       dm.cfg.CloudURL,
		CenterURL:      dm.cfg.CenterURL,
		CloudFailCount: dm.cloudFailCount,
		CanSwitchBack:  dm.state == DegradeConfirm,
		LastPlaylistID: dm.lastPlaylistID,
	}
	if !dm.lastCloudHB.IsZero() {
		s.LastCloudHB = dm.lastCloudHB.Format(time.RFC3339)
	}
	if !dm.lastCenterHB.IsZero() {
		s.LastCenterHB = dm.lastCenterHB.Format(time.RFC3339)
	}
	return s
}

// replayOffline 将离线暂存条目补传到云端
func (dm *DataSourceManager) replayOffline(store *OfflineStore, baseURL string) {
	if baseURL == "" {
		return
	}

	uploadURL := baseURL + "/api/v1/offline/upload"
	count, err := store.ReplayTo(context.Background(), func(entry OfflineEntry) error {
		data, _ := json.Marshal(entry)
		resp, err := dm.client.Post(uploadURL, "application/json", io.NopCloser(bytes.NewReader(data)))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("补传返回 %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		log.Warn().Err(err).Int("uploaded", count).Msg("离线条目补传中断")
	} else if count > 0 {
		log.Info().Int("uploaded", count).Msg("离线条目补传完成")
	}
}
