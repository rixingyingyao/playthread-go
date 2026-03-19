package models

import "time"

// EventType 事件类型常量
type EventType string

const (
	EventStatusChanged   EventType = "status_changed"
	EventPlayStarted     EventType = "play_started"
	EventPlayFinished    EventType = "play_finished"
	EventPlayProgress    EventType = "progress"
	EventAudioLevel      EventType = "level"
	EventBlankStarted    EventType = "blank_started"
	EventBlankStopped    EventType = "blank_stopped"
	EventIntercutStarted EventType = "intercut_started"
	EventIntercutEnded   EventType = "intercut_ended"
	EventFixTimeArrived  EventType = "fix_time_arrived"
	EventCountDown       EventType = "countdown"
	EventChannelEmpty    EventType = "channel_empty"
	EventDeviceLost      EventType = "device_lost"
	EventDeviceRestored  EventType = "device_restored"
	EventError           EventType = "error"
	EventHeartbeat       EventType = "heartbeat"
	EventJingleControl   EventType = "jingle_control"
)

// StatusChangeEvent 状态变更事件
type StatusChangeEvent struct {
	OldStatus Status   `json:"old_status"`
	NewStatus Status   `json:"new_status"`
	Path      PathType `json:"path"`
	Reason    string   `json:"reason"`
}

// PlayingClipEvent 正在播出的素材更新事件
type PlayingClipEvent struct {
	Program  *Program `json:"program"`
	LengthMs int      `json:"length_ms"`
	Channel  ChannelName `json:"channel"`
}

// PlayProgressEvent 播出进度事件
type PlayProgressEvent struct {
	ProgramID  string  `json:"program_id"`
	PositionMs int     `json:"position_ms"` // 当前位置(ms)
	DurationMs int     `json:"duration_ms"` // 总时长(ms)
	Progress   float64 `json:"progress"`    // 0.0-1.0
}

// CountDownEvent 倒计时事件
type CountDownEvent struct {
	Value int `json:"value"` // 当前倒计时(秒)
	Total int `json:"total"` // 总倒计时(秒)
}

// FixTimeArrivedEvent 定时到达事件
type FixTimeArrivedEvent struct {
	BlockID    string   `json:"block_id"`
	TaskType   TaskType `json:"task_type"`
	DelayMs    int      `json:"delay_ms"` // 淡出延时(ms)
}

// IntercutArrivedEvent 插播到达事件
type IntercutArrivedEvent struct {
	ID      string       `json:"id"`
	Type    IntercutType `json:"type"`
	DelayMs int          `json:"delay_ms"`
}

// ChannelEmptyEvent 通道空闲事件（播完后无下条素材）
type ChannelEmptyEvent struct {
	Channel      ChannelName `json:"channel"`
	CountDownSec int         `json:"countdown_sec"`
}

// JingleControlEvent Jingle 播放控制事件
type JingleControlEvent struct {
	IsFadeOut  bool `json:"is_fade_out"`
	FadeOutLen int  `json:"fade_out_len"` // 淡出时长(ms)
}

// AudioLevelEvent 音频电平事件
type AudioLevelEvent struct {
	Channel ChannelName `json:"channel"`
	Left    float64     `json:"left"`  // 左声道 0.0-1.0
	Right   float64     `json:"right"` // 右声道 0.0-1.0
}

// ErrorEvent 错误事件
type ErrorEvent struct {
	Message   string `json:"message"`
	AutoClose bool   `json:"auto_close"`
	HoldSec   int    `json:"hold_sec"`
}

// BroadcastEvent 统一广播事件包装（用于 WebSocket 推送）
type BroadcastEvent struct {
	Type EventType   `json:"type"`
	Data interface{} `json:"data"`
	Time time.Time   `json:"time"`
}

// NewBroadcastEvent 创建广播事件
func NewBroadcastEvent(eventType EventType, data interface{}) BroadcastEvent {
	return BroadcastEvent{
		Type: eventType,
		Data: data,
		Time: time.Now(),
	}
}
