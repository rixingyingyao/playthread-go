package core

import (
	"sync"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// StateMachine 六状态状态机（对齐 C# SlvcStatusController）。
// 校验在 ChangeStatusTo 内完成（Go 方案 A），调用者无需预先调用 GetPath。
// 线程安全。
type StateMachine struct {
	mu         sync.RWMutex
	status     models.Status
	lastStatus models.Status
	paths      map[[2]models.Status]models.PathType
	onChange   func(from, to models.Status, path models.PathType)
}

// NewStateMachine 创建状态机（初始状态为 Stopped）
func NewStateMachine() *StateMachine {
	sm := &StateMachine{
		status: models.StatusStopped,
		paths:  buildPathMap(),
	}
	return sm
}

// SetOnChange 注册状态变更回调（在锁外异步调用）
func (sm *StateMachine) SetOnChange(fn func(from, to models.Status, path models.PathType)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onChange = fn
}

// Status 获取当前状态
func (sm *StateMachine) Status() models.Status {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.status
}

// LastStatus 获取上一个状态
func (sm *StateMachine) LastStatus() models.Status {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastStatus
}

// ChangeStatusTo 状态变更唯一入口。
// 验证合法路径后执行变更，非法路径返回 TransitionError。
// 特殊处理：Stopped → Stopped 映射为 Stop2Auto（C# 兜底行为）。
func (sm *StateMachine) ChangeStatusTo(target models.Status, reason string) (models.PathType, error) {
	sm.mu.Lock()

	current := sm.status

	// C# 兜底：Stopped → Stopped 视为 Stop2Auto
	if current == models.StatusStopped && target == models.StatusStopped {
		target = models.StatusAuto
	}

	if current == target {
		sm.mu.Unlock()
		return models.ErrPath, nil // 相同状态，不变更
	}

	key := [2]models.Status{current, target}
	path, ok := sm.paths[key]
	if !ok {
		sm.mu.Unlock()
		return models.ErrPath, &models.TransitionError{
			From:   current,
			To:     target,
			Reason: reason,
		}
	}

	sm.lastStatus = current
	sm.status = target
	onChange := sm.onChange

	sm.mu.Unlock()

	log.Info().
		Str("from", current.String()).
		Str("to", target.String()).
		Str("path", path.String()).
		Str("reason", reason).
		Msg("状态变更")

	if onChange != nil {
		onChange(current, target, path)
	}

	return path, nil
}

// GetPath 获取从当前状态到目标状态的路径（不执行变更）
func (sm *StateMachine) GetPath(target models.Status) models.PathType {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.status == target {
		return models.ErrPath
	}

	key := [2]models.Status{sm.status, target}
	path, ok := sm.paths[key]
	if !ok {
		return models.ErrPath
	}
	return path
}

// buildPathMap 构建 20 条合法路径映射表（对齐 C# GetPath）
func buildPathMap() map[[2]models.Status]models.PathType {
	return map[[2]models.Status]models.PathType{
		// From Stopped
		{models.StatusStopped, models.StatusAuto}:       models.Stop2Auto,
		{models.StatusStopped, models.StatusManual}:     models.Stop2Manual,
		{models.StatusStopped, models.StatusLive}:       models.Stop2Live,
		{models.StatusStopped, models.StatusRedifDelay}: models.Stop2Delay,

		// From Auto
		{models.StatusAuto, models.StatusStopped}:    models.Auto2Stop,
		{models.StatusAuto, models.StatusManual}:     models.Auto2Manual,
		{models.StatusAuto, models.StatusEmergency}:  models.Auto2Emerg,
		{models.StatusAuto, models.StatusRedifDelay}: models.Auto2Delay,
		{models.StatusAuto, models.StatusLive}:       models.Auto2Live,

		// From Manual
		{models.StatusManual, models.StatusAuto}:       models.Manual2Auto,
		{models.StatusManual, models.StatusStopped}:    models.Manual2Stop,
		{models.StatusManual, models.StatusLive}:       models.Manual2Live,
		{models.StatusManual, models.StatusRedifDelay}: models.Manual2Delay,

		// From Live
		{models.StatusLive, models.StatusAuto}:       models.Live2Auto,
		{models.StatusLive, models.StatusManual}:     models.Live2Manual,
		{models.StatusLive, models.StatusRedifDelay}: models.Live2Delay,

		// From Emergency (只能回 Auto)
		{models.StatusEmergency, models.StatusAuto}: models.Emerg2Auto,

		// From RedifDelay
		{models.StatusRedifDelay, models.StatusAuto}:   models.Delay2Auto,
		{models.StatusRedifDelay, models.StatusLive}:   models.Delay2Live,
		{models.StatusRedifDelay, models.StatusManual}: models.Delay2Manual,
	}
}
