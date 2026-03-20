package core

import (
	"sync"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// EventBus 核心事件总线，使用有缓冲 channel 进行 goroutine 间通信。
// 所有 channel 不 close，靠 GC 回收。
type EventBus struct {
	PlayFinished chan PlayFinishedEvent // 播完事件（IPC 子进程 → playbackLoop）
	ChannelEmpty chan ChannelEmptyEvent // 通道空闲（IPC 子进程 → workLoop）
	StatusChange chan StatusChangeCmd   // 状态迁移指令（API/UDP → workLoop）
	FixTimeArrive chan FixTimeEvent     // 定时任务到达（FixTimeManager → workLoop）
	IntercutArrive chan IntercutEvent   // 插播到达（FixTimeManager → workLoop）
	BlankFinished chan struct{}         // 垫乐播完（垫乐通道 → BlankManager）
	Broadcast     chan models.BroadcastEvent // 向外推送（core → api/ws）

	subscribers []Subscriber
	mu          sync.RWMutex
}

// Subscriber 事件订阅者接口
type Subscriber interface {
	OnBroadcast(event models.BroadcastEvent)
}

// NewEventBus 创建事件总线
func NewEventBus() *EventBus {
	return &EventBus{
		PlayFinished:   make(chan PlayFinishedEvent, 16),
		ChannelEmpty:   make(chan ChannelEmptyEvent, 8),
		StatusChange:   make(chan StatusChangeCmd, 8),
		FixTimeArrive:  make(chan FixTimeEvent, 8),
		IntercutArrive: make(chan IntercutEvent, 4),
		BlankFinished:  make(chan struct{}, 4),
		Broadcast:      make(chan models.BroadcastEvent, 64),
	}
}

// Subscribe 注册广播事件订阅者
func (eb *EventBus) Subscribe(sub Subscriber) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.subscribers = append(eb.subscribers, sub)
}

// Emit 发送广播事件
func (eb *EventBus) Emit(event models.BroadcastEvent) {
	select {
	case eb.Broadcast <- event:
	default:
		log.Warn().Str("type", string(event.Type)).Msg("广播事件丢弃：channel 满")
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, sub := range eb.subscribers {
		go sub.OnBroadcast(event)
	}
}

// --- 内部事件结构 ---

// PlayFinishedEvent 播完事件
type PlayFinishedEvent struct {
	Channel int // 通道索引
}

// ChannelEmptyEvent 通道空闲事件
type ChannelEmptyEvent struct {
	Channel int
}

// StatusChangeCmd 状态迁移指令（外部 → workLoop）
type StatusChangeCmd struct {
	Target models.Status
	Reason string
	Result chan error // 返回迁移结果
}

// FixTimeEvent 定时任务到达事件
type FixTimeEvent struct {
	BlockID   string
	TaskType  models.TaskType
	StartTime int           // 计划触发时间(ms of day)
	DelayMs   int           // 淡出延时(ms)
}

// IntercutEvent 插播到达事件
type IntercutEvent struct {
	ID        string
	Type      models.IntercutType
	DelayMs   int
	SectionID string              // 所属栏目 ArrangeID
	Programs  []*models.Program   // 插播素材列表
}

// ChannelHoldReturnEvent 通道保持返回事件
type ChannelHoldReturnEvent struct {
	ManualCancel bool
}
