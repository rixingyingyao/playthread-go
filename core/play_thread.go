package core

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// PlayThread 主播出编排器（对齐 C# SlvcPlayThread）。
// 使用两个 goroutine（playbackLoop + workLoop）处理不同事件集。
type PlayThread struct {
	cfg          *infra.Config
	stateMachine *StateMachine
	eventBus     *EventBus
	audioBridge  *bridge.AudioBridge
	snapshotMgr  *infra.SnapshotManager

	playlist   *models.Playlist
	currentPos int // FlatList 中当前播出索引
	currentProg *models.Program

	// 互斥标志（定时任务 vs PlayNext）
	inPlayNext atomic.Bool
	inFixTime  atomic.Bool

	// 重入防护：channel 超时锁（替代 C# Monitor.TryEnter）
	playNextLock chan struct{}

	// 软定时等待
	softFixWaiting atomic.Bool
	cancelSoftFix  context.CancelFunc

	// 插播状态
	cutPlaying atomic.Bool

	// 挂起标志
	suspended atomic.Bool
}

// NewPlayThread 创建播出编排器
func NewPlayThread(
	cfg *infra.Config,
	sm *StateMachine,
	eb *EventBus,
	ab *bridge.AudioBridge,
	snapMgr *infra.SnapshotManager,
) *PlayThread {
	return &PlayThread{
		cfg:          cfg,
		stateMachine: sm,
		eventBus:     eb,
		audioBridge:  ab,
		snapshotMgr:  snapMgr,
		playNextLock: make(chan struct{}, 1),
	}
}

// SetPlaylist 设置当前播表
func (pt *PlayThread) SetPlaylist(pl *models.Playlist) {
	pt.playlist = pl
	pl.Flatten()
	pt.currentPos = 0
	log.Info().Str("id", pl.ID).Int("programs", pl.Len()).Msg("播表已加载")
}

// Run 启动播出编排器（启动两个事件循环 goroutine）
func (pt *PlayThread) Run(ctx context.Context) {
	go pt.playbackLoop(ctx)
	go pt.workLoop(ctx)
	go pt.snapshotLoop(ctx)

	log.Info().Msg("PlayThread 已启动")
}

// --- playbackLoop: 处理播完事件 ---
// 对应 C# PlaybackThread (Priority=Highest)
func (pt *PlayThread) playbackLoop(ctx context.Context) {
	for {
		select {
		case evt := <-pt.eventBus.PlayFinished:
			pt.handlePlayFinished(evt)
		case <-pt.eventBus.BlankFinished:
			pt.handleBlankFinished()
		case <-ctx.Done():
			log.Info().Msg("playbackLoop 退出")
			return
		}
	}
}

func (pt *PlayThread) handlePlayFinished(evt PlayFinishedEvent) {
	if evt.Channel != int(models.ChanMainOut) {
		return
	}

	status := pt.stateMachine.Status()
	log.Debug().Int("channel", evt.Channel).Str("status", status.String()).Msg("播完事件")

	switch status {
	case models.StatusAuto:
		pt.playNextClip(false)
	case models.StatusEmergency:
		pt.playNextEmrgClip()
	case models.StatusManual:
		pt.eventBus.Emit(models.NewBroadcastEvent(models.EventPlayFinished, models.PlayProgressEvent{
			ProgramID: pt.currentProgID(),
		}))
	}
}

func (pt *PlayThread) handleBlankFinished() {
	log.Debug().Msg("垫乐播完，尝试下一首垫乐或切回正常播出")
}

// --- workLoop: 处理状态迁移和外部指令 ---
// 对应 C# WorkThread (Priority=AboveNormal)
func (pt *PlayThread) workLoop(ctx context.Context) {
	for {
		select {
		case cmd := <-pt.eventBus.StatusChange:
			pt.handleStatusChange(cmd)
		case evt := <-pt.eventBus.ChannelEmpty:
			pt.handleChannelEmpty(evt)
		case evt := <-pt.eventBus.FixTimeArrive:
			pt.handleFixTimeArrived(evt)
		case evt := <-pt.eventBus.IntercutArrive:
			pt.handleIntercutArrived(evt)
		case <-ctx.Done():
			log.Info().Msg("workLoop 退出")
			return
		}
	}
}

func (pt *PlayThread) handleStatusChange(cmd StatusChangeCmd) {
	path, err := pt.stateMachine.ChangeStatusTo(cmd.Target, cmd.Reason)
	if cmd.Result != nil {
		cmd.Result <- err
	}
	if err != nil {
		return
	}

	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventStatusChanged, models.StatusChangeEvent{
		OldStatus: pt.stateMachine.LastStatus(),
		NewStatus: cmd.Target,
		Path:      path,
		Reason:    cmd.Reason,
	}))

	switch path {
	case models.Stop2Auto:
		pt.playNextClip(true)
	case models.Auto2Stop, models.Manual2Stop:
		pt.stopPlayback()
	case models.Auto2Live, models.Stop2Live, models.Manual2Live:
		pt.enterLiveMode()
	}
}

func (pt *PlayThread) handleChannelEmpty(evt ChannelEmptyEvent) {
	if pt.stateMachine.Status() == models.StatusAuto {
		log.Info().Int("channel", evt.Channel).Msg("通道空闲，启动垫乐")
		pt.eventBus.Emit(models.NewBroadcastEvent(models.EventChannelEmpty, models.ChannelEmptyEvent{
			Channel: models.ChannelName(evt.Channel),
		}))
	}
}

func (pt *PlayThread) handleFixTimeArrived(evt FixTimeEvent) {
	// 等待 PlayNext 完成，最多 500ms（对齐 C# 50×10ms 循环）
	for i := 0; i < 50 && pt.inPlayNext.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	pt.inFixTime.Store(true)
	defer pt.inFixTime.Store(false)

	// 取消软定时等待
	if pt.softFixWaiting.Load() && pt.cancelSoftFix != nil {
		pt.cancelSoftFix()
	}

	log.Info().
		Str("block_id", evt.BlockID).
		Str("task_type", evt.TaskType.String()).
		Int("delay_ms", evt.DelayMs).
		Msg("定时任务到达")

	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventFixTimeArrived, models.FixTimeArrivedEvent{
		BlockID:  evt.BlockID,
		TaskType: evt.TaskType,
		DelayMs:  evt.DelayMs,
	}))

	switch evt.TaskType {
	case models.TaskHard:
		pt.executeHardFix(evt)
	case models.TaskSoft:
		pt.executeSoftFix(evt)
	}
}

func (pt *PlayThread) handleIntercutArrived(evt IntercutEvent) {
	log.Info().Str("id", evt.ID).Str("type", "intercut").Msg("插播任务到达")
	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventIntercutStarted, models.IntercutArrivedEvent{
		ID:      evt.ID,
		Type:    evt.Type,
		DelayMs: evt.DelayMs,
	}))
}

// --- PlayNextClip 决策树 ---

// playNextClip 播出下一条素材（对齐 C# PlayNextClip）。
// 使用 channel 超时锁实现重入防护（替代 C# Monitor.TryEnter 500ms）。
func (pt *PlayThread) playNextClip(force bool) bool {
	if pt.suspended.Load() {
		return false
	}
	if pt.inFixTime.Load() && !force {
		return false
	}

	// 重入防护：500ms 超时锁
	select {
	case pt.playNextLock <- struct{}{}:
	case <-time.After(500 * time.Millisecond):
		log.Warn().Msg("PlayNextClip 重入超时，跳过")
		return false
	}
	pt.inPlayNext.Store(true)
	defer func() {
		pt.inPlayNext.Store(false)
		<-pt.playNextLock
	}()

	if pt.playlist == nil {
		log.Warn().Msg("播表为空")
		return false
	}

	program := pt.playlist.FindNext(pt.currentPos)
	if program == nil {
		log.Info().Msg("播表已到末尾，启动垫乐")
		pt.eventBus.Emit(models.NewBroadcastEvent(models.EventBlankStarted, nil))
		return false
	}

	if err := pt.cueAndPlay(program); err != nil {
		log.Warn().Err(err).Str("name", program.Name).Msg("预卷/播出失败，尝试下条")
		pt.currentPos++
		return pt.playNextClip(force) // 递归尝试下条
	}

	pt.currentProg = program
	pt.currentPos++

	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventPlayStarted, models.PlayingClipEvent{
		Program:  program,
		LengthMs: program.EffectiveDuration(),
		Channel:  models.ChanMainOut,
	}))

	return true
}

// playNextEmrgClip Emergency 模式的独立播放方法（对齐 C# PlayNextEmrgClip）
func (pt *PlayThread) playNextEmrgClip() {
	log.Debug().Msg("Emergency 模式播出下一条")
	// Emergency 模式使用独立的应急播表逻辑
	// Phase 5 实现完整的应急播出编排
}

// cueAndPlay 预卷 + 播出（含重试）
func (pt *PlayThread) cueAndPlay(program *models.Program) error {
	maxRetry := pt.cfg.Playback.CueRetryMax

	for attempt := 0; attempt <= maxRetry; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
			log.Debug().Int("attempt", attempt).Str("name", program.Name).Msg("预卷重试")
		}

		err := pt.audioBridge.Load(
			int(models.ChanMainOut),
			program.FilePath,
			program.IsEncrypt,
			program.Volume,
			program.FadeIn,
		)
		if err != nil {
			log.Warn().Err(err).Str("name", program.Name).Int("attempt", attempt).Msg("预卷失败")
			continue
		}

		err = pt.audioBridge.Play(int(models.ChanMainOut), true)
		if err != nil {
			log.Warn().Err(err).Str("name", program.Name).Msg("播出失败")
			// 播出失败：延迟 300ms 注入 PlayFinish 事件
			go func() {
				time.Sleep(300 * time.Millisecond)
				select {
				case pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}:
				default:
				}
			}()
			return err
		}

		return nil
	}

	return &models.TransitionError{
		Reason: "预卷重试耗尽: " + program.Name,
	}
}

// --- 辅助方法 ---

func (pt *PlayThread) executeHardFix(evt FixTimeEvent) {
	if pt.audioBridge == nil {
		return
	}
	_ = pt.audioBridge.Stop(int(models.ChanMainOut), evt.DelayMs)
	pt.playNextClip(true)
}

func (pt *PlayThread) executeSoftFix(evt FixTimeEvent) {
	pt.softFixWaiting.Store(true)
	defer pt.softFixWaiting.Store(false)

	// 软定时：等待当前素材播完再切（通过 playbackLoop 的 PlayFinished 自然触发）
	log.Info().Str("block_id", evt.BlockID).Msg("软定时等待当前素材播完")
}

func (pt *PlayThread) stopPlayback() {
	if pt.audioBridge == nil {
		return
	}
	_ = pt.audioBridge.Stop(int(models.ChanMainOut), pt.cfg.Audio.FadeOutMs)
	log.Info().Msg("播出已停止")
}

func (pt *PlayThread) enterLiveMode() {
	log.Info().Msg("进入直播模式")
}

func (pt *PlayThread) currentProgID() string {
	if pt.currentProg != nil {
		return pt.currentProg.ID
	}
	return ""
}

// snapshotLoop 定期保存播出快照
func (pt *PlayThread) snapshotLoop(ctx context.Context) {
	interval := time.Duration(pt.cfg.Playback.SnapshotIntervalS) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pt.saveSnapshot()
		case <-ctx.Done():
			pt.saveSnapshot()
			return
		}
	}
}

func (pt *PlayThread) saveSnapshot() {
	if pt.snapshotMgr == nil || pt.currentProg == nil {
		return
	}

	posResult, err := pt.audioBridge.GetPosition(int(models.ChanMainOut))
	posMs := 0
	durMs := 0
	if err == nil && posResult != nil {
		posMs = posResult.PositionMs
		durMs = posResult.DurationMs
	}

	info := &infra.PlayingInfo{
		ProgramID:    pt.currentProg.ID,
		ProgramName:  pt.currentProg.Name,
		Position:     posMs,
		Duration:     durMs,
		Status:       pt.stateMachine.Status(),
		SignalID:     pt.currentProg.SignalID,
		IsCutPlaying: pt.cutPlaying.Load(),
	}
	if pt.playlist != nil {
		info.PlaylistID = pt.playlist.ID
		info.ProgramIndex = pt.currentPos - 1
	}

	if err := pt.snapshotMgr.Save(info); err != nil {
		log.Warn().Err(err).Msg("保存播出快照失败")
	}
}

// --- 外部控制接口 ---

// ChangeStatus 请求状态变更（API/UDP 调用）
func (pt *PlayThread) ChangeStatus(target models.Status, reason string) error {
	result := make(chan error, 1)
	pt.eventBus.StatusChange <- StatusChangeCmd{
		Target: target,
		Reason: reason,
		Result: result,
	}
	return <-result
}

// Next 手动切下一条
func (pt *PlayThread) Next() bool {
	return pt.playNextClip(true)
}

// Suspend 挂起播出
func (pt *PlayThread) Suspend() {
	pt.suspended.Store(true)
	log.Info().Msg("播出已挂起")
}

// Resume 恢复播出
func (pt *PlayThread) Resume() {
	pt.suspended.Store(false)
	log.Info().Msg("播出已恢复")
}

// CurrentProgram 获取当前正在播出的素材
func (pt *PlayThread) CurrentProgram() *models.Program {
	return pt.currentProg
}

// CurrentPosition 获取当前播出位置
func (pt *PlayThread) CurrentPosition() int {
	return pt.currentPos
}
