package core

import (
	"math/rand"
	"sync"
	"time"

	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// BlankState 垫乐三态（对齐 C# Prepare/Play/Stop 生命周期）
type BlankState int

const (
	BlankStopped  BlankState = iota // 已停止
	BlankPrepared                    // 已预卷（尚未播放）
	BlankPlaying                     // 播放中
)

var blankStateNames = [...]string{"Stopped", "Prepared", "Playing"}

func (s BlankState) String() string {
	if int(s) < len(blankStateNames) {
		return blankStateNames[s]
	}
	return "Unknown"
}

// BlankManager 垫乐管理器（对齐 C# SlaBlankTaskManager + SlaBlankPlayInfo）。
// 当播表无下条素材时自动启动垫乐填充，定时到达时自动让位。
type BlankManager struct {
	mu sync.Mutex

	state    BlankState
	enabled  bool
	crntClip *models.Program

	clips    []*models.Program // 常规垫乐素材列表
	clipsIdl []*models.Program // 轻音乐素材列表（AI 智能垫乐短时段用）

	history *infra.BlankHistory
	bridge  *bridge.AudioBridge

	enableAI      bool
	aiThresholdMs int

	fadeOutMs int // 垫乐停止淡出时长(ms)
	cueRetry  int // 预卷重试次数

	// 回调：返回距下一个定时任务的毫秒数，-1 表示无后续任务
	getPaddingTimeMs func() int

	eventBus *EventBus
}

// BlankManagerConfig 垫乐管理器配置
type BlankManagerConfig struct {
	EnableAI      bool
	AIThresholdMs int
	FadeOutMs     int
	CueRetry      int
}

// NewBlankManager 创建垫乐管理器
func NewBlankManager(
	cfg BlankManagerConfig,
	eb *EventBus,
	ab *bridge.AudioBridge,
	history *infra.BlankHistory,
	getPaddingTimeMs func() int,
) *BlankManager {
	return &BlankManager{
		state:            BlankStopped,
		enableAI:         cfg.EnableAI,
		aiThresholdMs:    cfg.AIThresholdMs,
		fadeOutMs:        cfg.FadeOutMs,
		cueRetry:         cfg.CueRetry,
		eventBus:         eb,
		bridge:           ab,
		history:          history,
		getPaddingTimeMs: getPaddingTimeMs,
	}
}

// SetClips 设置常规垫乐素材列表
func (bm *BlankManager) SetClips(clips []*models.Program) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.clips = clips
	log.Info().Int("count", len(clips)).Msg("常规垫乐列表已更新")
}

// SetIdleClips 设置轻音乐素材列表（AI 短时段垫乐）
func (bm *BlankManager) SetIdleClips(clips []*models.Program) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.clipsIdl = clips
	log.Info().Int("count", len(clips)).Msg("轻音乐垫乐列表已更新")
}

// --- 三态管理 ---

// Prepare 预卷垫乐（选曲 → 加载到 FillBlank 通道）
func (bm *BlankManager) Prepare() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.state == BlankPlaying {
		return true
	}

	clip := bm.selectClip()
	if clip == nil {
		log.Warn().Msg("无可用垫乐素材")
		return false
	}

	for attempt := 0; attempt <= bm.cueRetry; attempt++ {
		if attempt > 0 {
			log.Debug().Int("attempt", attempt).Str("name", clip.Name).Msg("垫乐预卷重试")
			bm.markPlayed(clip)
			clip = bm.selectClip()
			if clip == nil {
				log.Warn().Msg("垫乐预卷重试: 无更多可用素材")
				return false
			}
		}

		if bm.bridge == nil {
			bm.crntClip = clip
			bm.state = BlankPrepared
			return true
		}

		err := bm.bridge.Load(
			int(models.ChanFillBlank),
			clip.FilePath,
			clip.IsEncrypt,
			clip.Volume,
			clip.FadeIn,
		)
		if err != nil {
			log.Warn().Err(err).Str("name", clip.Name).Int("attempt", attempt).Msg("垫乐预卷失败")
			continue
		}

		bm.crntClip = clip
		bm.state = BlankPrepared
		log.Info().Str("name", clip.Name).Msg("垫乐已预卷")
		return true
	}

	log.Error().Msg("垫乐预卷重试耗尽")
	return false
}

// Play 开始播放垫乐
func (bm *BlankManager) Play() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.state == BlankPlaying {
		return true
	}

	if bm.state != BlankPrepared {
		bm.mu.Unlock()
		if !bm.Prepare() {
			return false
		}
		bm.mu.Lock()
	}

	if bm.bridge != nil {
		if err := bm.bridge.Play(int(models.ChanFillBlank), true); err != nil {
			log.Error().Err(err).Msg("垫乐播出失败")
			bm.state = BlankStopped
			bm.crntClip = nil
			return false
		}
	}

	bm.state = BlankPlaying
	bm.enabled = true

	if bm.crntClip != nil {
		bm.addHistory(bm.crntClip)
		log.Info().Str("name", bm.crntClip.Name).Msg("垫乐开始播出")
	}

	bm.mu.Unlock()
	bm.eventBus.Emit(models.NewBroadcastEvent(models.EventBlankStarted, nil))
	bm.mu.Lock()

	return true
}

// Stop 停止垫乐（带淡出）
func (bm *BlankManager) Stop() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.state == BlankStopped {
		return
	}

	bm.enabled = false

	if bm.bridge != nil && bm.state == BlankPlaying {
		_ = bm.bridge.Stop(int(models.ChanFillBlank), bm.fadeOutMs)
	}

	clipName := ""
	if bm.crntClip != nil {
		clipName = bm.crntClip.Name
	}

	bm.state = BlankStopped
	bm.crntClip = nil

	log.Info().Str("name", clipName).Msg("垫乐已停止")

	bm.mu.Unlock()
	bm.eventBus.Emit(models.NewBroadcastEvent(models.EventBlankStopped, nil))
	bm.mu.Lock()
}

// StartIfNeeded 启动垫乐（如果未播放则预卷+播放）
func (bm *BlankManager) StartIfNeeded() bool {
	bm.mu.Lock()
	if bm.state == BlankPlaying {
		bm.mu.Unlock()
		return true
	}
	bm.mu.Unlock()

	if !bm.Prepare() {
		return false
	}
	return bm.Play()
}

// --- 播出联动 ---

// YieldTo 垫乐让位（定时到达时调用）。
// 淡出停止垫乐，为定时播出让路。
func (bm *BlankManager) YieldTo(fadeOutMs int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.state != BlankPlaying {
		return
	}

	bm.enabled = false

	if bm.bridge != nil {
		_ = bm.bridge.Stop(int(models.ChanFillBlank), fadeOutMs)
	}

	log.Info().Int("fade_ms", fadeOutMs).Msg("垫乐让位")

	bm.state = BlankStopped
	bm.crntClip = nil
}

// FadeToNext 淡出当前垫乐并切到下一首（不是停止垫乐，对齐 C# FadeToNext）。
// 播完事件触发时调用，实现垫乐无缝循环。
func (bm *BlankManager) FadeToNext() bool {
	bm.mu.Lock()
	if !bm.enabled || bm.state != BlankPlaying {
		bm.mu.Unlock()
		return false
	}

	nextClip := bm.selectClip()
	if nextClip == nil {
		bm.mu.Unlock()
		log.Warn().Msg("垫乐 FadeToNext: 无下一首可选")
		return false
	}

	if bm.bridge != nil {
		_ = bm.bridge.Stop(int(models.ChanFillBlank), bm.fadeOutMs)
	}

	bm.state = BlankStopped
	bm.crntClip = nil
	bm.mu.Unlock()

	bm.mu.Lock()
	for attempt := 0; attempt <= bm.cueRetry; attempt++ {
		if attempt > 0 {
			bm.markPlayed(nextClip)
			nextClip = bm.selectClip()
			if nextClip == nil {
				bm.mu.Unlock()
				return false
			}
		}

		if bm.bridge == nil {
			break
		}

		err := bm.bridge.Load(
			int(models.ChanFillBlank),
			nextClip.FilePath,
			nextClip.IsEncrypt,
			nextClip.Volume,
			nextClip.FadeIn,
		)
		if err != nil {
			log.Warn().Err(err).Str("name", nextClip.Name).Msg("垫乐 FadeToNext 预卷失败")
			continue
		}

		if err := bm.bridge.Play(int(models.ChanFillBlank), true); err != nil {
			log.Error().Err(err).Msg("垫乐 FadeToNext 播出失败")
			continue
		}

		bm.crntClip = nextClip
		bm.state = BlankPlaying
		bm.addHistory(nextClip)
		log.Info().Str("name", nextClip.Name).Msg("垫乐已切换到下一首")
		bm.mu.Unlock()
		return true
	}

	bm.mu.Unlock()
	log.Error().Msg("垫乐 FadeToNext 全部重试失败")
	return false
}

// --- 查询接口 ---

// IsPlaying 垫乐是否正在播放
func (bm *BlankManager) IsPlaying() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.state == BlankPlaying
}

// IsEnabled 垫乐是否启用
func (bm *BlankManager) IsEnabled() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.enabled
}

// State 获取当前垫乐状态
func (bm *BlankManager) State() BlankState {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.state
}

// CurrentClip 获取当前垫乐曲目
func (bm *BlankManager) CurrentClip() *models.Program {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.crntClip
}

// --- 选曲算法（需在锁内调用） ---

// selectClip 智能选曲（对齐 C# _GetOldestClip）。
//  1. 启用 AI 时：距下一定时 < AIThresholdMs → 随机选轻音乐；否则 LRU 选常规垫乐
//  2. 未启用 AI → 直接 LRU 选常规垫乐
//  3. "从未播放" 的素材优先返回
func (bm *BlankManager) selectClip() *models.Program {
	if bm.enableAI && bm.getPaddingTimeMs != nil {
		paddingTime := bm.getPaddingTimeMs()
		if paddingTime >= 0 && paddingTime < bm.aiThresholdMs && len(bm.clipsIdl) > 0 {
			idx := rand.Intn(len(bm.clipsIdl))
			return bm.clipsIdl[idx]
		}
	}

	return bm.lruSelect(bm.clips)
}

// lruSelect LRU 选曲：选最久未播放的素材。
// "从未播放"的素材优先返回（对齐 C# break 快速返回）。
func (bm *BlankManager) lruSelect(clips []*models.Program) *models.Program {
	if len(clips) == 0 {
		return nil
	}

	var oldest *models.Program
	var oldestTime time.Time
	first := true

	for _, clip := range clips {
		if bm.history == nil {
			return clip
		}

		lastPlay, found := bm.history.GetLastPlayTime(clip.ProgramID)
		if !found {
			return clip // 从未播放过 → 立即返回
		}

		if first || lastPlay.Before(oldestTime) {
			oldest = clip
			oldestTime = lastPlay
			first = false
		}
	}

	return oldest
}

// markPlayed 标记素材为"已播放"（预卷失败时排到末尾，对齐 C# goto agin 模式）
func (bm *BlankManager) markPlayed(clip *models.Program) {
	if bm.history != nil && clip != nil {
		bm.history.Add(clip.ProgramID, time.Now(), 0)
	}
}

// addHistory 添加垫乐播放历史
func (bm *BlankManager) addHistory(clip *models.Program) {
	if bm.history != nil && clip != nil {
		bm.history.Add(clip.ProgramID, time.Now(), clip.EffectiveDuration())
	}
}
