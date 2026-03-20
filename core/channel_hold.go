package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ChannelHoldData 通道保持请求参数（对齐 C# Delay_Data）
type ChannelHoldData struct {
	ReturnTime  time.Time // 计划返回时间（单调时钟不受 NTP 影响，此处记录 wall clock 供日志使用）
	DurationMs  int       // 保持时长(ms)
	ProgramID   int       // 接播节目 ID（0=不指定）
	ProgramName string    // 接播节目名称
	SignalID    int       // 信号源 ID
	SignalName  string    // 信号源名称
	IsAIDelay   bool      // 是否 AI 智能转播
}

// ChannelHold 通道保持管理（对齐 C# ChannelHoldTask）。
// 使用 time.Timer + 单调时钟保证超时精度不受系统时间调整影响。
type ChannelHold struct {
	mu            sync.Mutex
	active        bool
	data          *ChannelHoldData
	manualCancel  bool
	startMono     time.Time     // 启动时的单调时钟基点
	durationMono  time.Duration // 保持时长（基于单调时钟）
	timer         *time.Timer
	cancel        context.CancelFunc
	onReturn      func()        // 超时回调
}

// NewChannelHold 创建通道保持管理器
func NewChannelHold(onReturn func()) *ChannelHold {
	return &ChannelHold{
		onReturn: onReturn,
	}
}

// Start 启动通道保持
func (ch *ChannelHold) Start(data *ChannelHoldData) error {
	if data == nil {
		return fmt.Errorf("通道保持数据不能为空")
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.active {
		ch.stopLocked()
	}

	ch.data = data
	ch.active = true
	ch.manualCancel = false
	ch.startMono = time.Now()
	ch.durationMono = time.Duration(data.DurationMs) * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	ch.cancel = cancel

	ch.timer = time.NewTimer(ch.durationMono)
	timer := ch.timer

	go ch.waitReturn(ctx, timer)

	log.Info().
		Int("duration_ms", data.DurationMs).
		Str("signal", data.SignalName).
		Int("program_id", data.ProgramID).
		Bool("ai_delay", data.IsAIDelay).
		Msg("通道保持已启动")

	return nil
}

// waitReturn 等待超时或取消。timer 作为参数传入避免竞态访问 ch.timer。
func (ch *ChannelHold) waitReturn(ctx context.Context, timer *time.Timer) {
	select {
	case <-timer.C:
		ch.mu.Lock()
		if !ch.active {
			ch.mu.Unlock()
			return
		}
		ch.active = false
		ch.mu.Unlock()

		log.Info().Msg("通道保持超时，自动返回")
		if ch.onReturn != nil {
			ch.onReturn()
		}

	case <-ctx.Done():
		timer.Stop()
	}
}

// Stop 手动停止通道保持（标记为手动取消）
func (ch *ChannelHold) Stop() {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if !ch.active {
		return
	}

	ch.manualCancel = true
	ch.stopLocked()
	log.Info().Msg("通道保持手动取消")
}

// stopLocked 内部停止（调用前需持有锁）
func (ch *ChannelHold) stopLocked() {
	ch.active = false
	if ch.cancel != nil {
		ch.cancel()
		ch.cancel = nil
	}
	if ch.timer != nil {
		ch.timer.Stop()
		ch.timer = nil
	}
}

// IsActive 判断是否正在通道保持
func (ch *ChannelHold) IsActive() bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.active
}

// IsManualCancel 判断上次停止是否为手动取消
func (ch *ChannelHold) IsManualCancel() bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.manualCancel
}

// Data 返回当前通道保持数据（nil 表示未激活）
func (ch *ChannelHold) Data() *ChannelHoldData {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if !ch.active {
		return nil
	}
	return ch.data
}

// RemainingMs 返回剩余时间（毫秒），不活跃时返回 0
func (ch *ChannelHold) RemainingMs() int {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if !ch.active {
		return 0
	}

	elapsed := time.Since(ch.startMono)
	remaining := ch.durationMono - elapsed
	if remaining < 0 {
		return 0
	}
	return int(remaining.Milliseconds())
}

// ElapsedMs 返回已持续时间（毫秒），不活跃时返回 0
func (ch *ChannelHold) ElapsedMs() int {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if !ch.active {
		return 0
	}

	return int(time.Since(ch.startMono).Milliseconds())
}

// Reset 重置通道保持状态（Stop 时调用）
func (ch *ChannelHold) Reset() {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.stopLocked()
	ch.data = nil
	ch.manualCancel = false
}
