package audio

import (
	"sync"

	"github.com/rs/zerolog/log"
)

// PlayerState 播放器状态
type PlayerState int

const (
	StateStopped PlayerState = iota
	StatePlaying
	StatePaused
)

// String 返回状态字符串
func (s PlayerState) String() string {
	switch s {
	case StateStopped:
		return "Stopped"
	case StatePlaying:
		return "Playing"
	case StatePaused:
		return "Paused"
	default:
		return "Unknown"
	}
}

// CardPlayerAdapter 播卡适配器。
// 封装虚拟通道播放控制，提供事件通知。
// 对应 C# SlaCardPlayerAdapter。
type CardPlayerAdapter struct {
	mu      sync.Mutex
	channel string      // 绑定的通道名
	state   PlayerState // 当前状态
	engine  *BassEngine

	// 事件通道——不 close，靠 GC 回收
	FinishedCh chan struct{} // 播完通知
	EmptyCh    chan struct{} // 通道空闲通知
}

// NewCardPlayerAdapter 创建播卡适配器
func NewCardPlayerAdapter(channel string, engine *BassEngine) *CardPlayerAdapter {
	return &CardPlayerAdapter{
		channel:    channel,
		state:      StateStopped,
		engine:     engine,
		FinishedCh: make(chan struct{}, 1),
		EmptyCh:    make(chan struct{}, 1),
	}
}

// State 获取当前状态
func (a *CardPlayerAdapter) State() PlayerState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// Channel 获取绑定的通道名
func (a *CardPlayerAdapter) Channel() string {
	return a.channel
}

// Load 加载文件到通道
func (a *CardPlayerAdapter) Load(filePath string, isEncrypt bool, volume float32) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 重置事件
	a.drainCh(a.FinishedCh)
	a.drainCh(a.EmptyCh)

	if err := a.engine.Load(a.channel, filePath, isEncrypt, volume); err != nil {
		return err
	}
	log.Debug().Str("channel", a.channel).Str("file", filePath).Msg("适配器加载完成")
	return nil
}

// Play 播放
func (a *CardPlayerAdapter) Play() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.engine.Play(a.channel, true); err != nil {
		return err
	}
	a.state = StatePlaying
	return nil
}

// Pause 暂停
func (a *CardPlayerAdapter) Pause() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.engine.Pause(a.channel); err != nil {
		return err
	}
	a.state = StatePaused
	return nil
}

// Resume 恢复
func (a *CardPlayerAdapter) Resume() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.engine.Resume(a.channel); err != nil {
		return err
	}
	a.state = StatePlaying
	return nil
}

// Stop 停止
func (a *CardPlayerAdapter) Stop(fadeOut int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.engine.Stop(a.channel, fadeOut); err != nil {
		return err
	}
	a.state = StateStopped
	return nil
}

// SetVolume 设置音量
func (a *CardPlayerAdapter) SetVolume(volume float32) error {
	return a.engine.SetVolume(a.channel, volume)
}

// GetPosition 获取播放位置（毫秒）和总时长（毫秒）
func (a *CardPlayerAdapter) GetPosition() (int64, int64, error) {
	return a.engine.GetPosition(a.channel)
}

// Free 释放通道资源
func (a *CardPlayerAdapter) Free() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.engine.FreeChannel(a.channel)
	a.state = StateStopped
}

// OnPlayFinished 播完回调处理
func (a *CardPlayerAdapter) OnPlayFinished() {
	a.mu.Lock()
	a.state = StateStopped
	a.mu.Unlock()

	select {
	case a.FinishedCh <- struct{}{}:
	default:
	}
}

// OnChannelEmpty 通道空闲回调处理
func (a *CardPlayerAdapter) OnChannelEmpty() {
	select {
	case a.EmptyCh <- struct{}{}:
	default:
	}
}

// drainCh 清空通知通道中的待处理事件
func (a *CardPlayerAdapter) drainCh(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
