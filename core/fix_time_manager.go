package core

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// FixTimeTask 定时任务条目（对齐 C# SlaFixTimeTaskManager 中的任务项）
type FixTimeTask struct {
	ID        string          // 任务唯一标识
	BlockID   string          // 关联的 TimeBlock ID
	ArrangeID int             // 编排ID，同时间排序依据
	TaskType  models.TaskType // 硬定时 / 软定时
	StartTime time.Time       // 计划触发时间（绝对时间）
	FadeOutMs int             // 淡出时间(ms)
	Triggered bool            // 是否已触发
}

// IntercutTask 插播任务条目
type IntercutTask struct {
	ID        string              // 插播栏目 ID
	ArrangeID int                 // 编排ID
	Type      models.IntercutType // 定时/紧急
	StartTime time.Time           // 计划触发时间
	FadeOutMs int                 // 淡出时间(ms)
	Triggered bool
}

// FixTimeManager 定时任务管理器（对齐 C# SlaFixTimeTaskManager）。
// 维护两个任务列表，各由 20ms Ticker 独立轮询：
//   - fixTasks:     定时任务（硬/软定时）
//   - intercutTasks: 插播任务
//
// 线程安全。
type FixTimeManager struct {
	mu             sync.Mutex
	fixTasks       []*FixTimeTask
	intercutTasks  []*IntercutTask
	paused         bool

	pollingMs      int // 轮询间隔(ms)
	taskExpireMs   int // 过期容忍(ms)
	hardAdvanceMs  int // 硬定时提前量(ms)
	softAdvanceMs  int // 软定时提前量(ms)
	intercutAdvanceMs int // 插播提前量(ms)

	eventBus *EventBus

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewFixTimeManager 创建定时任务管理器
func NewFixTimeManager(eb *EventBus, pollingMs, taskExpireMs, hardAdvanceMs, softAdvanceMs int) *FixTimeManager {
	intercutAdv := 100
	if hardAdvanceMs+10 > intercutAdv {
		intercutAdv = hardAdvanceMs + 10
	}

	return &FixTimeManager{
		pollingMs:         pollingMs,
		taskExpireMs:      taskExpireMs,
		hardAdvanceMs:     hardAdvanceMs,
		softAdvanceMs:     softAdvanceMs,
		intercutAdvanceMs: intercutAdv,
		eventBus:          eb,
		paused:            true,
	}
}

// Run 启动两个轮询 goroutine（对齐 C# fixThread_Elapsed + intercutThread_Elapsed）
func (fm *FixTimeManager) Run(ctx context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	fm.cancel = cancel

	fm.wg.Add(2)
	go func() {
		defer fm.wg.Done()
		fm.fixLoop(childCtx)
	}()
	go func() {
		defer fm.wg.Done()
		fm.intercutLoop(childCtx)
	}()

	log.Info().Int("polling_ms", fm.pollingMs).Msg("FixTimeManager 已启动")
}

// Stop 停止轮询
func (fm *FixTimeManager) Stop() {
	if fm.cancel != nil {
		fm.cancel()
	}
}

// Wait 等待所有轮询 goroutine 退出（优雅关闭时调用）
func (fm *FixTimeManager) Wait() {
	fm.wg.Wait()
}

// Pause 暂停定时检测（手动模式 / 通道保持时调用）
func (fm *FixTimeManager) Pause() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.paused = true
	log.Debug().Msg("FixTimeManager 已暂停")
}

// Start 恢复定时检测
func (fm *FixTimeManager) Start() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.paused = false
	log.Debug().Msg("FixTimeManager 已恢复")
}

// IsPaused 是否暂停中
func (fm *FixTimeManager) IsPaused() bool {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return fm.paused
}

// --- 定时任务管理 ---

// SetFixTasks 批量设置定时任务（初始化或刷新时调用）
func (fm *FixTimeManager) SetFixTasks(tasks []*FixTimeTask) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.fixTasks = tasks
	fm.sortFixTasks()
	log.Info().Int("count", len(tasks)).Msg("定时任务列表已更新")
}

// AddFixTask 添加单个定时任务
func (fm *FixTimeManager) AddFixTask(task *FixTimeTask) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.fixTasks = append(fm.fixTasks, task)
	fm.sortFixTasks()
}

// RemoveFixTask 移除指定 ID 的定时任务
func (fm *FixTimeManager) RemoveFixTask(id string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	for i, t := range fm.fixTasks {
		if t.ID == id {
			fm.fixTasks = append(fm.fixTasks[:i], fm.fixTasks[i+1:]...)
			return
		}
	}
}

// ClearFixTasks 清空定时任务
func (fm *FixTimeManager) ClearFixTasks() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.fixTasks = fm.fixTasks[:0]
}

// FixTaskCount 返回当前定时任务数量
func (fm *FixTimeManager) FixTaskCount() int {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return len(fm.fixTasks)
}

// --- 插播任务管理 ---

// SetIntercutTasks 批量设置插播任务
func (fm *FixTimeManager) SetIntercutTasks(tasks []*IntercutTask) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.intercutTasks = tasks
	fm.sortIntercutTasks()
	log.Info().Int("count", len(tasks)).Msg("插播任务列表已更新")
}

// AddIntercutTask 添加单个插播任务
func (fm *FixTimeManager) AddIntercutTask(task *IntercutTask) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.intercutTasks = append(fm.intercutTasks, task)
	fm.sortIntercutTasks()
}

// ClearIntercutTasks 清空插播任务
func (fm *FixTimeManager) ClearIntercutTasks() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.intercutTasks = fm.intercutTasks[:0]
}

// --- 查询接口 ---

// IsNearFixTask 判断指定毫秒内是否有定时任务即将到达。
// 插播触发前调用此方法，若 1 秒内有定时则放弃插播（对齐 C# IsNearFixTask）。
func (fm *FixTimeManager) IsNearFixTask(withinMs int) bool {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if len(fm.fixTasks) == 0 {
		return false
	}

	now := time.Now()
	deadline := now.Add(time.Duration(withinMs) * time.Millisecond)

	for _, t := range fm.fixTasks {
		if !t.Triggered && t.StartTime.Before(deadline) && t.StartTime.After(now.Add(-time.Second)) {
			return true
		}
	}
	return false
}

// NextFixTaskTime 返回下一个定时任务的触发时间。
// 无任务时返回零值。供垫乐 AI 选曲计算间隙时长。
func (fm *FixTimeManager) NextFixTaskTime() (time.Time, bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	now := time.Now()
	for _, t := range fm.fixTasks {
		if !t.Triggered && t.StartTime.After(now) {
			return t.StartTime, true
		}
	}
	return time.Time{}, false
}

// GetPaddingTimeMs 返回距下一个定时任务的毫秒数（供 AI 选曲使用）。
// 无后续任务时返回 -1。
func (fm *FixTimeManager) GetPaddingTimeMs() int {
	next, ok := fm.NextFixTaskTime()
	if !ok {
		return -1
	}
	ms := int(time.Until(next).Milliseconds())
	if ms < 0 {
		return 0
	}
	return ms
}

// --- 轮询循环 ---

// fixLoop 20ms 定时任务轮询（对齐 C# fixThread_Elapsed）
func (fm *FixTimeManager) fixLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(fm.pollingMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fm.checkFixTasks()
		case <-ctx.Done():
			log.Debug().Msg("fixLoop 退出")
			return
		}
	}
}

// intercutLoop 20ms 插播任务轮询（对齐 C# intercutThread_Elapsed）
func (fm *FixTimeManager) intercutLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(fm.pollingMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fm.checkIntercutTasks()
		case <-ctx.Done():
			log.Debug().Msg("intercutLoop 退出")
			return
		}
	}
}

func (fm *FixTimeManager) checkFixTasks() {
	fm.mu.Lock()

	if fm.paused || len(fm.fixTasks) == 0 {
		fm.mu.Unlock()
		return
	}

	now := time.Now()
	var toFire *FixTimeTask
	var toRemove []int

	for i, task := range fm.fixTasks {
		if task.Triggered {
			continue
		}

		elapsed := now.Sub(task.StartTime).Milliseconds()

		// 过期清理：超过 taskExpireMs 的任务丢弃（对齐 C# nowtime - StartTime > 3000）
		if elapsed > int64(fm.taskExpireMs) {
			toRemove = append(toRemove, i)
			log.Warn().Str("id", task.ID).Str("block_id", task.BlockID).Msg("定时任务已过期，丢弃")
			continue
		}

		// 提前量检测
		advance := fm.hardAdvanceMs
		if task.TaskType == models.TaskSoft {
			advance = fm.softAdvanceMs
		}

		triggerAt := task.StartTime.Add(-time.Duration(advance) * time.Millisecond)
		if now.Before(triggerAt) {
			break // 任务已按时间排序，后续更不会触发
		}

		toFire = task
		task.Triggered = true
		toRemove = append(toRemove, i)
		break // 每轮只触发一个（先到先触发）
	}

	// 反向移除已处理的任务
	for j := len(toRemove) - 1; j >= 0; j-- {
		idx := toRemove[j]
		fm.fixTasks = append(fm.fixTasks[:idx], fm.fixTasks[idx+1:]...)
	}

	fm.mu.Unlock()

	if toFire != nil {
		fm.fireFixEvent(toFire)
	}
}

func (fm *FixTimeManager) checkIntercutTasks() {
	fm.mu.Lock()

	if fm.paused || len(fm.intercutTasks) == 0 {
		fm.mu.Unlock()
		return
	}

	now := time.Now()
	var toFire *IntercutTask
	var toRemove []int

	for i, task := range fm.intercutTasks {
		if task.Triggered {
			continue
		}

		elapsed := now.Sub(task.StartTime).Milliseconds()

		if elapsed > int64(fm.taskExpireMs) {
			toRemove = append(toRemove, i)
			log.Warn().Str("id", task.ID).Msg("插播任务已过期，丢弃")
			continue
		}

		triggerAt := task.StartTime.Add(-time.Duration(fm.intercutAdvanceMs) * time.Millisecond)
		if now.Before(triggerAt) {
			break
		}

		toFire = task
		task.Triggered = true
		toRemove = append(toRemove, i)
		break
	}

	for j := len(toRemove) - 1; j >= 0; j-- {
		idx := toRemove[j]
		fm.intercutTasks = append(fm.intercutTasks[:idx], fm.intercutTasks[idx+1:]...)
	}

	fm.mu.Unlock()

	if toFire != nil {
		fm.fireIntercutEvent(toFire)
	}
}

// --- 事件触发 ---

func (fm *FixTimeManager) fireFixEvent(task *FixTimeTask) {
	log.Info().
		Str("id", task.ID).
		Str("block_id", task.BlockID).
		Str("type", task.TaskType.String()).
		Msg("定时任务触发")

	evt := FixTimeEvent{
		BlockID:   task.BlockID,
		TaskType:  task.TaskType,
		StartTime: int(task.StartTime.UnixMilli()),
		DelayMs:   task.FadeOutMs,
	}

	select {
	case fm.eventBus.FixTimeArrive <- evt:
	default:
		log.Error().Str("id", task.ID).Msg("FixTimeArrive channel 满，事件丢失")
	}
}

func (fm *FixTimeManager) fireIntercutEvent(task *IntercutTask) {
	log.Info().
		Str("id", task.ID).
		Str("type", "intercut").
		Msg("插播任务触发")

	evt := IntercutEvent{
		ID:      task.ID,
		Type:    task.Type,
		DelayMs: task.FadeOutMs,
	}

	select {
	case fm.eventBus.IntercutArrive <- evt:
	default:
		log.Error().Str("id", task.ID).Msg("IntercutArrive channel 满，事件丢失")
	}
}

// --- 内部排序 ---

func (fm *FixTimeManager) sortFixTasks() {
	sort.Slice(fm.fixTasks, func(i, j int) bool {
		if fm.fixTasks[i].StartTime.Equal(fm.fixTasks[j].StartTime) {
			return fm.fixTasks[i].ArrangeID < fm.fixTasks[j].ArrangeID
		}
		return fm.fixTasks[i].StartTime.Before(fm.fixTasks[j].StartTime)
	})
}

func (fm *FixTimeManager) sortIntercutTasks() {
	sort.Slice(fm.intercutTasks, func(i, j int) bool {
		if fm.intercutTasks[i].StartTime.Equal(fm.intercutTasks[j].StartTime) {
			return fm.intercutTasks[i].ArrangeID < fm.intercutTasks[j].ArrangeID
		}
		return fm.intercutTasks[i].StartTime.Before(fm.intercutTasks[j].StartTime)
	})
}

// --- 从播表初始化 ---

// InitFromPlaylist 从播表中提取定时任务和插播任务。
// 过滤掉已过期的任务（StartTime < now）。
func (fm *FixTimeManager) InitFromPlaylist(playlist *models.Playlist, baseDate time.Time) {
	now := time.Now()
	var fixTasks []*FixTimeTask
	var intercutTasks []*IntercutTask

	for bi := range playlist.Blocks {
		block := &playlist.Blocks[bi]

		startTime, err := block.ParseStartTime(baseDate)
		if err != nil {
			log.Warn().Err(err).Str("block", block.Name).Msg("解析时间块开始时间失败")
			continue
		}

		if startTime.Before(now.Add(-time.Duration(fm.taskExpireMs) * time.Millisecond)) {
			continue
		}

		fadeOut := fm.hardAdvanceMs
		if block.TaskType == models.TaskSoft {
			fadeOut = 0
		}

		fixTasks = append(fixTasks, &FixTimeTask{
			ID:        block.ID,
			BlockID:   block.ID,
			ArrangeID: bi,
			TaskType:  block.TaskType,
			StartTime: startTime,
			FadeOutMs: fadeOut,
		})
	}

	fm.SetFixTasks(fixTasks)
	fm.SetIntercutTasks(intercutTasks)

	log.Info().
		Int("fix_tasks", len(fixTasks)).
		Int("intercut_tasks", len(intercutTasks)).
		Msg("从播表初始化定时任务完成")
}
