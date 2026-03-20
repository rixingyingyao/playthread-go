package core

import (
	"fmt"
	"sync"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

const defaultMaxIntercutDepth = 3

// IntercutEntry 插播栈条目
type IntercutEntry struct {
	ID          string                  // 插播栏目 ID
	Type        models.IntercutType     // 定时/紧急
	Programs    []*models.Program       // 插播素材列表
	ReturnSnap  *models.PlaybackSnapshot // 返回点快照（已含 CutReturn 补偿）
	SectionID   string                  // 所属栏目 ArrangeID
	CurrentIdx  int                     // 当前播出到第几条
}

// IntercutManager 插播栈管理器（对齐 C# m_CutPlaying + InterCut 逻辑）。
// LIFO 栈结构支持嵌套插播，最大深度可配置。
type IntercutManager struct {
	mu          sync.Mutex
	stack       []*IntercutEntry
	maxDepth    int
	cutReturnMs int
	eventBus    *EventBus
}

// NewIntercutManager 创建插播管理器
func NewIntercutManager(eb *EventBus, cutReturnMs int) *IntercutManager {
	return &IntercutManager{
		maxDepth:    defaultMaxIntercutDepth,
		cutReturnMs: cutReturnMs,
		eventBus:    eb,
	}
}

// Push 开始插播：保存当前播出快照到栈，切换到插播素材。
// currentSnap 为调用方传入的当前播出状态（位置已由调用方减去 CutReturn 补偿）。
func (im *IntercutManager) Push(entry *IntercutEntry) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) >= im.maxDepth {
		return fmt.Errorf("插播栈已满: depth=%d, max=%d", len(im.stack), im.maxDepth)
	}
	if entry == nil {
		return fmt.Errorf("插播条目不能为空")
	}
	if len(entry.Programs) == 0 {
		return fmt.Errorf("插播素材列表为空: id=%s", entry.ID)
	}

	entry.CurrentIdx = 0
	im.stack = append(im.stack, entry)

	log.Info().
		Str("id", entry.ID).
		Int("depth", len(im.stack)).
		Int("programs", len(entry.Programs)).
		Msg("插播入栈")

	return nil
}

// Pop 插播结束：弹出栈顶，返回恢复快照。
// 返回 nil 表示栈为空（无需恢复）。
func (im *IntercutManager) Pop() *models.PlaybackSnapshot {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) == 0 {
		return nil
	}

	top := im.stack[len(im.stack)-1]
	im.stack = im.stack[:len(im.stack)-1]

	log.Info().
		Str("id", top.ID).
		Int("remaining_depth", len(im.stack)).
		Msg("插播出栈，准备返回")

	return top.ReturnSnap
}

// Depth 返回当前栈深度
func (im *IntercutManager) Depth() int {
	im.mu.Lock()
	defer im.mu.Unlock()
	return len(im.stack)
}

// IsActive 判断是否正在插播中
func (im *IntercutManager) IsActive() bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	return len(im.stack) > 0
}

// Current 返回栈顶的插播条目（只读），nil 表示未在插播
func (im *IntercutManager) Current() *IntercutEntry {
	im.mu.Lock()
	defer im.mu.Unlock()
	if len(im.stack) == 0 {
		return nil
	}
	return im.stack[len(im.stack)-1]
}

// NextProgram 返回当前插播条目中的下一条素材。
// 返回 nil 表示插播素材已播完。
func (im *IntercutManager) NextProgram() *models.Program {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) == 0 {
		return nil
	}

	top := im.stack[len(im.stack)-1]
	if top.CurrentIdx >= len(top.Programs) {
		return nil
	}

	prog := top.Programs[top.CurrentIdx]
	top.CurrentIdx++
	return prog
}

// PeekNextProgram 预览下一条素材但不推进索引
func (im *IntercutManager) PeekNextProgram() *models.Program {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) == 0 {
		return nil
	}

	top := im.stack[len(im.stack)-1]
	if top.CurrentIdx >= len(top.Programs) {
		return nil
	}

	return top.Programs[top.CurrentIdx]
}

// HasMorePrograms 判断当前插播是否还有未播素材
func (im *IntercutManager) HasMorePrograms() bool {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) == 0 {
		return false
	}

	top := im.stack[len(im.stack)-1]
	return top.CurrentIdx < len(top.Programs)
}

// ClearOnFixTime 定时任务到达时清理插播状态。
// 对齐 C#：定时控件到达时重置 m_CutPlaying + 清除 IntCut_BackProgram。
func (im *IntercutManager) ClearOnFixTime() *models.PlaybackSnapshot {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) == 0 {
		return nil
	}

	top := im.stack[len(im.stack)-1]
	snap := top.ReturnSnap

	im.stack = im.stack[:0]

	log.Info().Msg("定时任务到达，插播栈已清空")
	return snap
}

// MakeReturnSnapshot 创建插播返回快照（含 CutReturn 补偿）。
// positionMs: 当前播放位置；cutReturnMs 由配置提供。
func (im *IntercutManager) MakeReturnSnapshot(
	programIdx int,
	programID string,
	positionMs int,
	status models.Status,
	signalID int,
	volume float64,
) *models.PlaybackSnapshot {
	compensated := positionMs - im.cutReturnMs
	if compensated < 0 {
		compensated = 0
	}

	return &models.PlaybackSnapshot{
		ProgramIndex: programIdx,
		ProgramID:    programID,
		PositionMs:   compensated,
		Status:       status,
		SignalID:     signalID,
		Volume:       volume,
		IsCutReturn:  true,
	}
}

// ResolveNestedReturn 处理嵌套插播返回：
// 如果当前是嵌套插播（栈深度 > 0），继承外层的返回信息。
// 对齐 C# 中 IntCut_BackProgram 和 InterCut_Back 的嵌套传递。
func (im *IntercutManager) ResolveNestedReturn(currentSnap *models.PlaybackSnapshot) *models.PlaybackSnapshot {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.stack) == 0 {
		return currentSnap
	}

	outerEntry := im.stack[len(im.stack)-1]
	if outerEntry.ReturnSnap != nil {
		log.Debug().
			Int("depth", len(im.stack)).
			Str("inherit_from", outerEntry.ID).
			Msg("嵌套插播：继承外层返回信息")
		return outerEntry.ReturnSnap
	}

	return currentSnap
}

// Reset 清空插播栈（用于状态机 Stop 时）
func (im *IntercutManager) Reset() {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.stack = im.stack[:0]
	log.Debug().Msg("插播栈已重置")
}
