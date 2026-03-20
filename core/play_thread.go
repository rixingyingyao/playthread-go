package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// CueError 预卷/播出失败专用错误（区别于 TransitionError）
type CueError struct {
	Name    string
	Reason  string
	Attempt int
}

func (e *CueError) Error() string {
	return fmt.Sprintf("预卷失败 %s: %s (重试 %d 次)", e.Name, e.Reason, e.Attempt)
}

// PlayThread 主播出编排器（对齐 C# SlvcPlayThread）。
// 使用两个 goroutine（playbackLoop + workLoop）处理不同事件集。
type PlayThread struct {
	cfg          *infra.Config
	stateMachine *StateMachine
	eventBus     *EventBus
	audioBridge  *bridge.AudioBridge
	snapshotMgr  *infra.SnapshotManager

	// Phase 4 组件
	fixTimeMgr *FixTimeManager
	blankMgr   *BlankManager

	// Phase 5 组件
	intercutMgr  *IntercutManager
	channelHold  *ChannelHold

	// 播出状态（mu 保护，多 goroutine 访问）
	mu          sync.RWMutex
	playlist    *models.Playlist
	currentPos  int // FlatList 中当前播出索引
	currentProg *models.Program

	// 互斥标志（定时任务 vs PlayNext）
	inPlayNext atomic.Bool
	inFixTime  atomic.Bool

	// 重入防护：channel 超时锁（替代 C# Monitor.TryEnter）
	playNextLock chan struct{}

	// 软定时等待
	softFixWaiting atomic.Bool
	cancelSoftFix  context.CancelFunc

	// 插播状态（双标记：cutPlaying 标志 + IntercutManager 栈深度）
	cutPlaying atomic.Bool

	// 挂起标志
	suspended atomic.Bool

	// EQ 均衡器当前名称
	currentEQ string

	// 信号切换去重时间戳（对齐 C# _LastSwitchTime）
	lastSwitchTime atomic.Value

	// 应急插播状态
	emrgStatus    models.Status
	emrgReturnPos *models.PlaybackSnapshot

	// goroutine 生命周期跟踪
	wg sync.WaitGroup
}

// NewPlayThread 创建播出编排器
func NewPlayThread(
	cfg *infra.Config,
	sm *StateMachine,
	eb *EventBus,
	ab *bridge.AudioBridge,
	snapMgr *infra.SnapshotManager,
) *PlayThread {
	pt := &PlayThread{
		cfg:          cfg,
		stateMachine: sm,
		eventBus:     eb,
		audioBridge:  ab,
		snapshotMgr:  snapMgr,
		playNextLock: make(chan struct{}, 1),
	}

	pt.fixTimeMgr = NewFixTimeManager(
		eb,
		cfg.Playback.PollingIntervalMs,
		cfg.Playback.TaskExpireMs,
		cfg.Playback.HardFixAdvanceMs,
		cfg.Playback.SoftFixAdvanceMs,
	)

	pt.blankMgr = NewBlankManager(
		BlankManagerConfig{
			EnableAI:      cfg.Padding.EnableAI,
			AIThresholdMs: cfg.Padding.AIThresholdMs,
			FadeOutMs:     cfg.Audio.FadeOutMs,
			CueRetry:      cfg.Playback.CueRetryMax,
		},
		eb,
		ab,
		infra.NewBlankHistory(cfg.Padding.Directory, cfg.Padding.HistoryKeepDays),
		pt.fixTimeMgr.GetPaddingTimeMs,
	)

	pt.intercutMgr = NewIntercutManager(eb, cfg.Playback.CutReturnMs)

	pt.channelHold = NewChannelHold(func() {
		pt.eventBus.StatusChange <- StatusChangeCmd{
			Target: models.StatusAuto,
			Reason: "通道保持超时自动返回",
		}
	})

	pt.lastSwitchTime.Store(time.Now().Add(-1 * time.Hour))

	return pt
}

// SetPlaylist 设置当前播表
func (pt *PlayThread) SetPlaylist(pl *models.Playlist) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.playlist = pl
	pl.Flatten()
	pt.currentPos = 0
	log.Info().Str("id", pl.ID).Int("programs", pl.Len()).Msg("播表已加载")
}

// Run 启动播出编排器（启动两个事件循环 goroutine + 定时管理器）
func (pt *PlayThread) Run(ctx context.Context) {
	pt.wg.Add(3)
	go func() {
		defer pt.wg.Done()
		pt.playbackLoop(ctx)
	}()
	go func() {
		defer pt.wg.Done()
		pt.workLoop(ctx)
	}()
	go func() {
		defer pt.wg.Done()
		pt.snapshotLoop(ctx)
	}()

	pt.fixTimeMgr.Run(ctx)

	log.Info().Msg("PlayThread 已启动")
}

// Wait 等待所有 goroutine 退出（优雅关闭时调用）
func (pt *PlayThread) Wait() {
	pt.wg.Wait()
	pt.fixTimeMgr.Wait()
}

// FixTimeManager 返回定时任务管理器
func (pt *PlayThread) FixTimeManager() *FixTimeManager {
	return pt.fixTimeMgr
}

// BlankManager 返回垫乐管理器
func (pt *PlayThread) BlankMgr() *BlankManager {
	return pt.blankMgr
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
		if evt.Channel == int(models.ChanFillBlank) {
			pt.handleBlankFinished()
		}
		return
	}

	status := pt.stateMachine.Status()
	log.Debug().Int("channel", evt.Channel).Str("status", status.String()).Msg("播完事件")

	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventPlayFinished, models.PlayProgressEvent{
		ProgramID: pt.currentProgID(),
	}))

	// 插播中：播完当前素材后继续播插播列表或返回
	if pt.cutPlaying.Load() && pt.intercutMgr.IsActive() {
		if pt.intercutMgr.HasMorePrograms() {
			nextProg := pt.intercutMgr.NextProgram()
			if nextProg != nil {
				if err := pt.cueAndPlay(nextProg); err != nil {
					log.Warn().Err(err).Str("name", nextProg.Name).Msg("插播后续素材失败")
					pt.returnFromIntercut()
					return
				}
				pt.mu.Lock()
				pt.currentProg = nextProg
				pt.mu.Unlock()
				return
			}
		}
		pt.returnFromIntercut()
		return
	}

	// 软定时等待中：当前素材播完即触发切换
	if pt.softFixWaiting.Load() {
		pt.softFixWaiting.Store(false)
		pt.playNextClip(true)
		return
	}

	switch status {
	case models.StatusAuto, models.StatusLive, models.StatusRedifDelay:
		pt.playNextClip(false)
	case models.StatusEmergency:
		pt.playNextEmrgClip()
	case models.StatusManual:
	}
}

func (pt *PlayThread) handleBlankFinished() {
	if pt.blankMgr.IsEnabled() {
		log.Debug().Msg("垫乐播完，切换到下一首")
		pt.blankMgr.FadeToNext()
	}
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
		pt.onEnterAuto()

	case models.Auto2Stop, models.Manual2Stop:
		pt.stopPlayback()

	case models.Auto2Manual, models.Stop2Manual:
		pt.onEnterManual()

	case models.Manual2Auto, models.Live2Auto, models.Emerg2Auto, models.Delay2Auto:
		pt.onReturnAuto()

	case models.Auto2Live, models.Stop2Live, models.Manual2Live, models.Delay2Live:
		pt.enterLiveMode()

	case models.Auto2Delay, models.Stop2Delay, models.Manual2Delay, models.Live2Delay:
		pt.enterDelayMode()

	case models.Auto2Emerg:
		pt.enterEmergencyMode()

	case models.Live2Manual, models.Delay2Manual:
		pt.onEnterManual()
	}
}

func (pt *PlayThread) handleChannelEmpty(evt ChannelEmptyEvent) {
	if pt.stateMachine.Status() == models.StatusAuto {
		log.Info().Int("channel", evt.Channel).Msg("通道空闲，启动垫乐")
		pt.eventBus.Emit(models.NewBroadcastEvent(models.EventChannelEmpty, models.ChannelEmptyEvent{
			Channel: models.ChannelName(evt.Channel),
		}))
		pt.setPaddingPlay(true)
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

	// 定时到达时清理插播状态（对齐 C# FixTimeArrived 中重置 m_CutPlaying）
	if pt.cutPlaying.Load() {
		pt.intercutMgr.ClearOnFixTime()
		pt.cutPlaying.Store(false)
		log.Info().Msg("定时到达，插播状态已清除")
	}

	switch evt.TaskType {
	case models.TaskHard:
		pt.executeHardFix(evt)
	case models.TaskSoft:
		pt.executeSoftFix(evt)
	}
}

func (pt *PlayThread) handleIntercutArrived(evt IntercutEvent) {
	if pt.fixTimeMgr.IsNearFixTask(1000) {
		log.Info().Str("id", evt.ID).Msg("插播放弃：1 秒内有定时任务")
		return
	}

	// 等待 PlayNext 完成
	for i := 0; i < 50 && pt.inPlayNext.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	pt.inFixTime.Store(true)
	defer pt.inFixTime.Store(false)

	log.Info().Str("id", evt.ID).Str("type", "intercut").Msg("插播任务到达")

	pt.mu.RLock()
	prog := pt.currentProg
	pos := pt.currentPos
	pl := pt.playlist
	pt.mu.RUnlock()

	// 保存当前播出状态作为返回快照
	var returnSnap *models.PlaybackSnapshot
	if prog != nil {
		posMs := 0
		if pt.audioBridge != nil {
			if result, err := pt.audioBridge.GetPosition(int(models.ChanMainOut)); err == nil && result != nil {
				posMs = result.PositionMs
			}
		}
		snap := pt.intercutMgr.MakeReturnSnapshot(
			pos-1, prog.ID, posMs,
			pt.stateMachine.Status(),
			prog.SignalID, prog.Volume,
		)

		// 嵌套插播：如果已在插播中，继承外层返回信息
		returnSnap = pt.intercutMgr.ResolveNestedReturn(snap)
	}

	entry := &IntercutEntry{
		ID:         evt.ID,
		Type:       evt.Type,
		Programs:   evt.Programs,
		ReturnSnap: returnSnap,
		SectionID:  evt.SectionID,
	}

	if err := pt.intercutMgr.Push(entry); err != nil {
		log.Error().Err(err).Str("id", evt.ID).Msg("插播入栈失败")
		return
	}

	// 停止垫乐
	pt.setPaddingPlay(false)

	// 播出插播的第一条素材
	nextProg := pt.intercutMgr.NextProgram()
	if nextProg == nil {
		log.Warn().Str("id", evt.ID).Msg("插播素材为空")
		pt.intercutMgr.Pop()
		return
	}

	if err := pt.cueAndPlay(nextProg); err != nil {
		log.Error().Err(err).Str("name", nextProg.Name).Msg("插播素材预卷失败")
		pt.intercutMgr.Pop()
		return
	}

	// 设置插播双标记
	pt.cutPlaying.Store(true)
	pt.mu.Lock()
	pt.currentProg = nextProg
	pt.mu.Unlock()

	// 标记被插播节目状态
	if prog != nil && pl != nil {
		pt.eventBus.Emit(models.NewBroadcastEvent(models.EventIntercutStarted, models.IntercutStartedEvent{
			ID:              evt.ID,
			Type:            evt.Type,
			Depth:           pt.intercutMgr.Depth(),
			InterruptedProg: prog.ID,
		}))
	}
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

	pt.mu.Lock()
	if pt.playlist == nil {
		pt.mu.Unlock()
		log.Warn().Msg("播表为空")
		return false
	}

	// Step 1: 临近硬定时检查（对齐 C# 决策树 Step 1）
	if !force && pt.fixTimeMgr.IsNearFixTask(pt.cfg.Playback.HardFixAdvanceMs) {
		pt.mu.Unlock()
		log.Debug().Msg("临近硬定时，不切换素材")
		return false
	}

	// 循环查找可播素材（替代递归，避免深调用栈）
	const maxSkip = 20 // 连续跳过上限，防止播表全损坏时空转
	skipped := 0
	for pt.currentPos < pt.playlist.Len() && skipped < maxSkip {
		program := pt.playlist.FindNext(pt.currentPos)
		if program == nil {
			break
		}

		pt.mu.Unlock()
		err := pt.cueAndPlay(program)
		pt.mu.Lock()

		if err != nil {
			log.Warn().Err(err).Str("name", program.Name).Int("pos", pt.currentPos).Msg("预卷/播出失败，尝试下条")
			pt.currentPos++
			skipped++
			continue
		}

		pt.currentProg = program
		pt.currentPos++
		pt.mu.Unlock()

		// 垫乐让位：主播出开始时停止垫乐
		if pt.blankMgr.IsPlaying() {
			pt.blankMgr.YieldTo(pt.cfg.Audio.FadeOutMs)
		}

		// EQ 均衡器切换
		pt.switchEQ(program)

		pt.eventBus.Emit(models.NewBroadcastEvent(models.EventPlayStarted, models.PlayingClipEvent{
			Program:  program,
			LengthMs: program.EffectiveDuration(),
			Channel:  models.ChanMainOut,
		}))

		return true
	}

	if skipped >= maxSkip {
		log.Error().Int("skipped", skipped).Msg("连续跳过素材过多，停止搜索")
	}

	pt.mu.Unlock()
	log.Info().Msg("播表已到末尾，启动垫乐")
	pt.setPaddingPlay(true)
	return false
}

// returnFromIntercut 插播结束，恢复到被插播位置
func (pt *PlayThread) returnFromIntercut() {
	snap := pt.intercutMgr.Pop()
	pt.cutPlaying.Store(pt.intercutMgr.IsActive())

	if snap == nil {
		log.Info().Msg("插播结束，无返回快照，继续播出下一条")
		pt.playNextClip(false)
		return
	}

	log.Info().
		Str("program_id", snap.ProgramID).
		Int("position_ms", snap.PositionMs).
		Bool("cut_return", snap.IsCutReturn).
		Msg("插播结束，恢复到被插播位置")

	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventIntercutEnded, models.IntercutEndedEvent{
		ReturnProg:  snap.ProgramID,
		ReturnPosMs: snap.PositionMs,
	}))

	// 恢复播出位置
	pt.mu.Lock()
	if pt.playlist != nil && snap.ProgramIndex >= 0 && snap.ProgramIndex < pt.playlist.Len() {
		pt.currentPos = snap.ProgramIndex
		prog := pt.playlist.FlatList[snap.ProgramIndex]
		pt.mu.Unlock()

		if err := pt.cueAndPlayAt(prog, snap.PositionMs); err != nil {
			log.Warn().Err(err).Msg("恢复被插播节目失败，播下一条")
			pt.playNextClip(true)
			return
		}
		pt.mu.Lock()
		pt.currentProg = prog
		pt.currentPos = snap.ProgramIndex + 1
		pt.mu.Unlock()
	} else {
		pt.mu.Unlock()
		pt.playNextClip(false)
	}
}

// cueAndPlayAt 预卷并从指定位置播出（用于插播返回）
func (pt *PlayThread) cueAndPlayAt(program *models.Program, positionMs int) error {
	maxRetry := pt.cfg.Playback.CueRetryMax

	for attempt := 0; attempt <= maxRetry; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
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

		if positionMs > 0 {
			if err := pt.audioBridge.Seek(int(models.ChanMainOut), positionMs); err != nil {
				log.Warn().Err(err).Int("pos_ms", positionMs).Msg("Seek 失败，从头播放")
			}
		}

		if err := pt.audioBridge.Play(int(models.ChanMainOut), true); err != nil {
			log.Warn().Err(err).Str("name", program.Name).Msg("播出失败")
			return err
		}
		return nil
	}

	return &CueError{Name: program.Name, Reason: "重试耗尽", Attempt: maxRetry}
}

// playNextEmrgClip Emergency 模式的独立播放方法（对齐 C# PlayNextEmrgClip）
func (pt *PlayThread) playNextEmrgClip() {
	log.Debug().Msg("Emergency 模式播出下一条")
	// 应急模式下播出下一条素材（由外部通过 EmrgCutStart 设置的播表控制）
	pt.playNextClip(true)
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
			return err
		}

		return nil
	}

	return &CueError{
		Name:    program.Name,
		Reason:  "重试耗尽",
		Attempt: maxRetry,
	}
}

// --- 垫乐控制 ---

// setPaddingPlay 控制垫乐开启/关闭（对齐 C# SetPaddingPlay）。
// 开启条件：Auto 模式 + 最近定时控件允许垫乐。
func (pt *PlayThread) setPaddingPlay(enable bool) {
	if enable {
		if pt.stateMachine.Status() != models.StatusAuto {
			return
		}
		// 淡出暂停主播出
		pt.fadePause(pt.cfg.Audio.FadeOutMs)
		// 启动垫乐
		pt.blankMgr.StartIfNeeded()
	} else {
		pt.blankMgr.Stop()
	}
}

// --- EQ 均衡器 ---

// switchEQ 根据所属时间块动态切换 EQ 均衡器（Phase 4 任务 4.13）。
// 当素材的 BlockIndex 对应 TimeBlock 的 EQName 变化时，通过 IPC 设置新 EQ。
func (pt *PlayThread) switchEQ(program *models.Program) {
	if program == nil || pt.playlist == nil {
		return
	}

	if program.BlockIndex < 0 || program.BlockIndex >= len(pt.playlist.Blocks) {
		return
	}

	eqName := pt.playlist.Blocks[program.BlockIndex].EQName
	if eqName == "" || eqName == pt.currentEQ {
		return
	}

	pt.currentEQ = eqName
	log.Info().Str("eq", eqName).Str("program", program.Name).Msg("EQ 均衡器切换")

	if pt.audioBridge != nil {
		if err := pt.audioBridge.SetEQ(int(models.ChanMainOut), eqName, nil); err != nil {
			log.Warn().Err(err).Str("eq", eqName).Msg("设置 EQ 均衡器失败")
		}
	}
}

// --- FadePause 淡出暂停 ---

// fadePause 淡出到目标音量后暂停流，不释放（Phase 4 任务 4.14）。
// 对齐 C# FadePause，用于垫乐接管前平滑暂停当前播放。
// 通道保持暂停状态，定时节目播完后可通过 Resume 恢复。
func (pt *PlayThread) fadePause(fadeOutMs int) {
	pt.mu.RLock()
	hasProg := pt.currentProg != nil
	pt.mu.RUnlock()

	if pt.audioBridge == nil || !hasProg {
		return
	}

	if err := pt.audioBridge.FadePause(int(models.ChanMainOut), 0, fadeOutMs); err != nil {
		log.Warn().Err(err).Int("fade_ms", fadeOutMs).Msg("主播出淡出暂停失败，改用 Stop")
		_ = pt.audioBridge.Stop(int(models.ChanMainOut), fadeOutMs)
	} else {
		log.Debug().Int("fade_ms", fadeOutMs).Msg("主播出淡出暂停")
	}
}

// --- 辅助方法 ---

func (pt *PlayThread) executeHardFix(evt FixTimeEvent) {
	// 停止垫乐
	if pt.blankMgr.IsPlaying() {
		pt.blankMgr.YieldTo(evt.DelayMs)
	}

	// 淡出当前播出
	if pt.audioBridge != nil {
		_ = pt.audioBridge.Stop(int(models.ChanMainOut), evt.DelayMs)
	}

	// 等待淡出完成
	if evt.DelayMs > 0 {
		time.Sleep(time.Duration(evt.DelayMs) * time.Millisecond)
	}

	pt.playNextClip(true)
}

func (pt *PlayThread) executeSoftFix(evt FixTimeEvent) {
	// 软定时：如果垫乐中，直接停垫乐并播定时节目
	if pt.blankMgr.IsPlaying() {
		pt.blankMgr.YieldTo(pt.cfg.Audio.FadeOutMs)
		pt.playNextClip(true)
		return
	}

	// 设置等待标志和取消句柄。当 playbackLoop 中 handlePlayFinished 检测到
	// softFixWaiting=true 时，会自动调用 playNextClip(true) 执行定时切换。
	// cancelSoftFix 可被 CancelSoftFix() 或 executeHardFix() 调用以取消等待。
	ctx, cancel := context.WithCancel(context.Background())
	pt.cancelSoftFix = cancel
	pt.softFixWaiting.Store(true)

	go func() {
		<-ctx.Done()
		// 取消时清除等待标志（被外部取消而非播完触发时需要）
		pt.softFixWaiting.Store(false)
	}()

	log.Info().Str("block_id", evt.BlockID).Msg("软定时等待当前素材播完")
}

func (pt *PlayThread) onEnterAuto() {
	log.Info().Msg("进入自动播出模式")

	pt.mu.RLock()
	pl := pt.playlist
	pt.mu.RUnlock()

	if pl != nil {
		pt.fixTimeMgr.InitFromPlaylist(pl, pl.Date)
		pt.fixTimeMgr.Start()
	}

	pt.playNextClip(true)
}

func (pt *PlayThread) onReturnAuto() {
	log.Info().Msg("返回自动播出模式")

	// 通道保持返回处理
	if pt.channelHold.IsActive() || pt.channelHold.IsManualCancel() {
		pt.channelHold.Stop()
	}

	// 停止垫乐和当前播出
	pt.setPaddingPlay(false)
	pt.fadePause(pt.cfg.Audio.FadeOutMs)

	pt.mu.RLock()
	pl := pt.playlist
	pt.mu.RUnlock()

	if pl != nil {
		pt.fixTimeMgr.InitFromPlaylist(pl, pl.Date)
		pt.fixTimeMgr.Start()
	}

	// 恢复被插播/通道保持前的播出位置
	if pt.emrgReturnPos != nil {
		snap := pt.emrgReturnPos
		pt.emrgReturnPos = nil

		log.Info().
			Str("program_id", snap.ProgramID).
			Int("position_ms", snap.PositionMs).
			Msg("从应急/通道保持返回，恢复原播出位置")

		pt.mu.Lock()
		if pl != nil && snap.ProgramIndex >= 0 && snap.ProgramIndex < pl.Len() {
			pt.currentPos = snap.ProgramIndex
			prog := pl.FlatList[snap.ProgramIndex]
			pt.mu.Unlock()

			if err := pt.cueAndPlayAt(prog, snap.PositionMs); err != nil {
				log.Warn().Err(err).Msg("恢复原播出位置失败，播下一条")
				pt.playNextClip(true)
				return
			}
			pt.mu.Lock()
			pt.currentProg = prog
			pt.currentPos = snap.ProgramIndex + 1
			pt.mu.Unlock()
			return
		}
		pt.mu.Unlock()
	}

	pt.playNextClip(true)
}

func (pt *PlayThread) onEnterManual() {
	log.Info().Msg("进入手动模式")
	pt.fixTimeMgr.Pause()
}

func (pt *PlayThread) stopPlayback() {
	pt.fixTimeMgr.Pause()
	pt.blankMgr.Stop()
	pt.softFixWaiting.Store(false)
	pt.cutPlaying.Store(false)
	pt.intercutMgr.Reset()
	pt.channelHold.Reset()

	if pt.audioBridge != nil {
		_ = pt.audioBridge.Stop(int(models.ChanMainOut), pt.cfg.Audio.FadeOutMs)
	}

	pt.mu.Lock()
	pt.currentProg = nil
	pt.mu.Unlock()
	log.Info().Msg("播出已停止")
}

func (pt *PlayThread) enterLiveMode() {
	log.Info().Msg("进入直播模式")
	// 直播模式：定时任务保持运行，主持人可手动操作
}

func (pt *PlayThread) enterDelayMode() {
	log.Info().Msg("进入延时转播模式")
	pt.fixTimeMgr.Pause()
	// 通道保持由 DelayStart 外部接口触发 channelHold.Start
}

func (pt *PlayThread) enterEmergencyMode() {
	log.Info().Msg("进入应急模式")
	pt.fixTimeMgr.Pause()
	pt.blankMgr.Stop()
	pt.playNextEmrgClip()
}

func (pt *PlayThread) currentProgID() string {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
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
	if pt.snapshotMgr == nil {
		return
	}

	pt.mu.RLock()
	prog := pt.currentProg
	pos := pt.currentPos
	pl := pt.playlist
	pt.mu.RUnlock()

	if prog == nil {
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
		ProgramID:    prog.ID,
		ProgramName:  prog.Name,
		Position:     posMs,
		Duration:     durMs,
		Status:       pt.stateMachine.Status(),
		SignalID:     prog.SignalID,
		IsCutPlaying: pt.cutPlaying.Load(),
	}
	if pl != nil {
		info.PlaylistID = pl.ID
		info.ProgramIndex = pos - 1
	}

	if err := pt.snapshotMgr.Save(info); err != nil {
		log.Warn().Err(err).Msg("保存播出快照失败")
	}
}

// --- 信号切换 ---

// switchSignal 切换信号源（含去重防抖）。
// 对齐 C# _LastSwitchTime 防抖：500ms 内重复切换同一信号则跳过。
func (pt *PlayThread) switchSignal(signalID int, signalName string) {
	lastSwitch := pt.lastSwitchTime.Load().(time.Time)
	if time.Since(lastSwitch) < time.Duration(pt.cfg.Playback.SignalSwitchDelayMs)*time.Millisecond {
		log.Debug().
			Int("signal_id", signalID).
			Str("signal_name", signalName).
			Msg("信号切换跳过：防抖窗口内")
		return
	}

	pt.lastSwitchTime.Store(time.Now())

	log.Info().
		Int("signal_id", signalID).
		Str("signal_name", signalName).
		Msg("信号切换")

	if pt.audioBridge != nil {
		if err := pt.audioBridge.SwitchSignal(signalID, signalName); err != nil {
			log.Error().Err(err).Int("signal_id", signalID).Msg("信号切换失败")
		}
	}
}

// --- 外部控制接口 ---

// ChangeStatus 请求状态变更（API/UDP 调用），带 5s 超时保护
func (pt *PlayThread) ChangeStatus(target models.Status, reason string) error {
	result := make(chan error, 1)
	cmd := StatusChangeCmd{
		Target: target,
		Reason: reason,
		Result: result,
	}
	select {
	case pt.eventBus.StatusChange <- cmd:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("状态变更超时：目标 %s, 原因: %s", target, reason)
	}
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("状态变更等待结果超时：目标 %s", target)
	}
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

// CurrentProgram 获取当前正在播出的素材（线程安全）
func (pt *PlayThread) CurrentProgram() *models.Program {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.currentProg
}

// CurrentPosition 获取当前播出位置（线程安全）
func (pt *PlayThread) CurrentPosition() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.currentPos
}

// CancelSoftFix 取消软定时等待（Jingle/临时单/手动操作时调用）
func (pt *PlayThread) CancelSoftFix() {
	if pt.softFixWaiting.Load() {
		pt.softFixWaiting.Store(false)
		if pt.cancelSoftFix != nil {
			pt.cancelSoftFix()
		}
		log.Info().Msg("软定时等待已取消")
	}
}

// StartBlank 手动启动垫乐
func (pt *PlayThread) StartBlank() {
	pt.setPaddingPlay(true)
}

// StopBlank 手动停止垫乐
func (pt *PlayThread) StopBlank() {
	pt.setPaddingPlay(false)
}

// IntercutMgr 返回插播管理器
func (pt *PlayThread) IntercutMgr() *IntercutManager {
	return pt.intercutMgr
}

// ChannelHoldMgr 返回通道保持管理器
func (pt *PlayThread) ChannelHoldMgr() *ChannelHold {
	return pt.channelHold
}

// DelayStart 外部启动通道保持（对齐 C# DelayStart）。
// 调用后系统进入 RedifDelay 状态，到期自动返回 Auto。
func (pt *PlayThread) DelayStart(data *ChannelHoldData) error {
	if data == nil {
		return fmt.Errorf("通道保持参数不能为空")
	}
	status := pt.stateMachine.Status()
	if status == models.StatusStopped {
		return fmt.Errorf("播出已停止，无法启动通道保持")
	}
	if status == models.StatusEmergency {
		return fmt.Errorf("紧急插播时无法启动通道保持")
	}

	if data.ReturnTime.Before(time.Now()) {
		return fmt.Errorf("返回时间已过期: %s", data.ReturnTime.Format("15:04:05"))
	}

	pt.mu.RLock()
	prog := pt.currentProg
	pt.mu.RUnlock()

	// 保存当前状态用于返回
	pt.emrgStatus = status
	if prog != nil {
		posMs := 0
		if pt.audioBridge != nil {
			if result, err := pt.audioBridge.GetPosition(int(models.ChanMainOut)); err == nil && result != nil {
				posMs = result.PositionMs
			}
		}
		pt.emrgReturnPos = pt.intercutMgr.MakeReturnSnapshot(
			pt.CurrentPosition()-1, prog.ID, posMs, status, prog.SignalID, prog.Volume,
		)
	}

	if err := pt.channelHold.Start(data); err != nil {
		return fmt.Errorf("通道保持启动失败: %w", err)
	}

	// 切换信号
	if data.SignalID > 0 {
		pt.switchSignal(data.SignalID, data.SignalName)
	}

	// 请求状态迁移到 RedifDelay
	if err := pt.ChangeStatus(models.StatusRedifDelay, "通道保持"); err != nil {
		pt.channelHold.Stop()
		return fmt.Errorf("状态迁移失败: %w", err)
	}

	pt.eventBus.Emit(models.NewBroadcastEvent(models.EventType("channel_hold_started"), models.ChannelHoldEvent{
		SignalID:    data.SignalID,
		SignalName:  data.SignalName,
		DurationMs:  data.DurationMs,
		ProgramName: data.ProgramName,
		IsAIDelay:   data.IsAIDelay,
	}))

	log.Info().
		Int("signal_id", data.SignalID).
		Int("duration_ms", data.DurationMs).
		Str("program_name", data.ProgramName).
		Msg("通道保持已启动")

	return nil
}

// DelayCancelManual 手动取消通道保持
func (pt *PlayThread) DelayCancelManual() error {
	if !pt.channelHold.IsActive() {
		return fmt.Errorf("当前不在通道保持中")
	}

	pt.channelHold.Stop()

	return pt.ChangeStatus(models.StatusAuto, "手动取消通道保持")
}

// EmrgCutStart 紧急插播开始（对齐 C# EmrgCutStart）
func (pt *PlayThread) EmrgCutStart(signalID int, signalName string) error {
	status := pt.stateMachine.Status()
	if status == models.StatusStopped {
		return fmt.Errorf("播出已停止，无法启动紧急插播")
	}

	pt.mu.RLock()
	prog := pt.currentProg
	pt.mu.RUnlock()

	// 保存被插播状态
	pt.emrgStatus = status
	if prog != nil {
		posMs := 0
		if pt.audioBridge != nil {
			if result, err := pt.audioBridge.GetPosition(int(models.ChanMainOut)); err == nil && result != nil {
				posMs = result.PositionMs
			}
		}
		pt.emrgReturnPos = pt.intercutMgr.MakeReturnSnapshot(
			pt.CurrentPosition()-1, prog.ID, posMs, status, prog.SignalID, prog.Volume,
		)
	}

	// 切换到紧急信号
	pt.switchSignal(signalID, signalName)

	// 暂停当前播放
	if pt.audioBridge != nil {
		_ = pt.audioBridge.FadePause(int(models.ChanMainOut), 0, pt.cfg.Audio.FadeOutMs)
	}

	return pt.ChangeStatus(models.StatusEmergency, fmt.Sprintf("紧急插播: %s", signalName))
}

// EmrgCutStop 紧急插播结束，返回自动播出（对齐 C# EmrgCutStop）
func (pt *PlayThread) EmrgCutStop() error {
	if pt.stateMachine.Status() != models.StatusEmergency {
		return fmt.Errorf("当前不在紧急插播状态")
	}

	return pt.ChangeStatus(models.StatusAuto, "紧急插播结束")
}

// IsCutPlaying 判断是否正在插播
func (pt *PlayThread) IsCutPlaying() bool {
	return pt.cutPlaying.Load()
}
