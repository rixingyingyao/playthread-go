package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock AudioBridge via pipe I/O ---

// mockIPC 模拟 IPC 子进程：读取请求并回写成功响应
// 对 position 请求返回指定位置；其余方法返回空 data。
type mockIPC struct {
	posMs int
	durMs int
}

func (m *mockIPC) serve(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req bridge.IPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		var resp bridge.IPCResponse
		resp.ID = req.ID

		if req.Method == "position" {
			data, _ := json.Marshal(bridge.PositionResult{
				PositionMs: m.posMs,
				DurationMs: m.durMs,
			})
			resp.Data = data
		}

		out, _ := json.Marshal(resp)
		out = append(out, '\n')
		_, _ = w.Write(out)
	}
}

// newMockAudioBridge 创建基于管道的 mock AudioBridge。
// 返回 bridge 实例和关闭函数。
func newMockAudioBridge(posMs, durMs int) (*bridge.AudioBridge, func()) {
	// stdinR: mock 从中读取请求; stdinW: bridge 写入请求
	stdinR, stdinW := io.Pipe()
	// stdoutR: bridge 从中读取响应; stdoutW: mock 写入响应
	stdoutR, stdoutW := io.Pipe()

	mock := &mockIPC{posMs: posMs, durMs: durMs}
	go mock.serve(stdinR, stdoutW)

	ab := bridge.NewAudioBridge(stdinW, stdoutR, 2*time.Second)

	cleanup := func() {
		stdinW.Close()
		stdoutW.Close()
		stdinR.Close()
		stdoutR.Close()
	}
	return ab, cleanup
}

// --- 测试辅助 ---

func testConfig() *infra.Config {
	cfg := infra.DefaultConfig()
	cfg.Audio.FadeOutMs = 10    // 测试中缩短淡出
	cfg.Audio.FadeInMs = 0
	cfg.Playback.PollingIntervalMs = 20
	cfg.Playback.TaskExpireMs = 3000
	cfg.Playback.HardFixAdvanceMs = 50
	cfg.Playback.SoftFixAdvanceMs = 0
	cfg.Playback.CueRetryMax = 0    // 测试不重试
	cfg.Playback.SnapshotIntervalS = 600 // 不频繁写快照
	cfg.Playback.CutReturnMs = 100
	return cfg
}

func testPlaylist(n int) *models.Playlist {
	progs := make([]models.Program, n)
	for i := range progs {
		progs[i] = models.Program{
			ID:       fmt.Sprintf("prog-%d", i),
			Name:     fmt.Sprintf("素材%d", i),
			FilePath: fmt.Sprintf("/audio/test%d.mp3", i),
			Duration: 30000,
			Volume:   1.0,
		}
	}
	pl := &models.Playlist{
		ID:   "test-playlist",
		Date: time.Now(),
		Blocks: []models.TimeBlock{{
			ID:       "block-0",
			Name:     "测试块",
			Programs: progs,
			TaskType: models.TaskHard,
		}},
	}
	pl.Flatten()
	return pl
}

// startPlayThread 创建并启动 PlayThread，返回 pt、cancel、cleanup。
func startPlayThread(t *testing.T, pl *models.Playlist) (*PlayThread, context.CancelFunc, func()) {
	t.Helper()
	ab, abCleanup := newMockAudioBridge(5000, 30000)
	cfg := testConfig()
	sm := NewStateMachine()
	eb := NewEventBus()

	pt := NewPlayThread(cfg, sm, eb, ab, nil)
	if pl != nil {
		pt.SetPlaylist(pl)
	}

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)

	cleanup := func() {
		cancel()
		pt.Wait()
		abCleanup()
	}
	return pt, cancel, cleanup
}

// drainBroadcast 消费广播事件，防止 channel 满塞住发送者。
func drainBroadcast(ctx context.Context, eb *EventBus) {
	go func() {
		for {
			select {
			case <-eb.Broadcast:
			case <-ctx.Done():
				return
			}
		}
	}()
}

// waitForStatus 等待状态机迁移到指定状态（最多 timeout）。
func waitForStatus(t *testing.T, sm *StateMachine, target models.Status, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if sm.Status() == target {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("等待状态 %s 超时，当前: %s", target, sm.Status())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// ==================== 测试用例 ====================

// TestPlayThread_AutoPlay_PlayFinished 自动播出 → 播完 → 播下一条
func TestPlayThread_AutoPlay_PlayFinished(t *testing.T) {
	pl := testPlaylist(3)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	// 排空广播
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	// 启动自动播出
	err := pt.ChangeStatus(models.StatusAuto, "test")
	require.NoError(t, err)

	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)

	// 等待 playNextClip 完成（初始播出）
	time.Sleep(200 * time.Millisecond)
	assert.NotNil(t, pt.CurrentProgram())

	// 发送播完事件
	pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}
	time.Sleep(200 * time.Millisecond)

	// 应该已经播到第二条
	prog := pt.CurrentProgram()
	require.NotNil(t, prog)
	assert.Equal(t, "prog-1", prog.ID, "播完后应切到下一条")
}

// TestPlayThread_SoftFix_WaitAndCancel 软定时等待 + 取消
func TestPlayThread_SoftFix_WaitAndCancel(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	// 进入自动播出
	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 软定时到达
	pt.eventBus.FixTimeArrive <- FixTimeEvent{
		BlockID:  "block-0",
		TaskType: models.TaskSoft,
	}
	time.Sleep(100 * time.Millisecond)

	// 确认进入等待
	assert.True(t, pt.softFixWaiting.Load(), "软定时未进入等待状态")

	// 取消软定时
	pt.CancelSoftFix()
	assert.False(t, pt.softFixWaiting.Load(), "取消后应退出等待")
}

// TestPlayThread_SoftFix_PlayFinishedTriggersSwitch 软定时等待 → 播完触发切换
func TestPlayThread_SoftFix_PlayFinishedTriggersSwitch(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 记住当前位置
	pos0 := pt.CurrentPosition()

	// 软定时到达
	pt.eventBus.FixTimeArrive <- FixTimeEvent{
		BlockID:  "block-0",
		TaskType: models.TaskSoft,
	}
	time.Sleep(100 * time.Millisecond)
	assert.True(t, pt.softFixWaiting.Load())

	// 播完事件 → 应立即切下一条
	pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}
	time.Sleep(200 * time.Millisecond)

	assert.False(t, pt.softFixWaiting.Load(), "播完后软定时等待应清除")
	assert.Greater(t, pt.CurrentPosition(), pos0, "播完后应推进到下一条")
}

// TestPlayThread_EmrgCutStartStop 紧急插播 → 结束 → 返回
func TestPlayThread_EmrgCutStartStop(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	// 进入自动播出，先播第一条
	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 紧急插播开始
	err := pt.EmrgCutStart(99, "紧急信号")
	require.NoError(t, err)

	waitForStatus(t, pt.stateMachine, models.StatusEmergency, time.Second)
	assert.NotNil(t, pt.emrgReturnPos, "应保存返回快照")

	// 紧急插播结束 → 返回自动
	err = pt.EmrgCutStop()
	require.NoError(t, err)

	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(300 * time.Millisecond)

	// 应恢复播出
	assert.NotNil(t, pt.CurrentProgram(), "返回后应恢复播出")
}

// TestPlayThread_ChannelHold_Timeout 通道保持超时自动返回
func TestPlayThread_ChannelHold_Timeout(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 通道保持 300ms 超时
	holdData := &ChannelHoldData{
		SignalID:    1,
		SignalName:  "测试信号",
		DurationMs:  300,
		ReturnTime:  time.Now().Add(300 * time.Millisecond),
		ProgramName: "测试转播",
	}

	err := pt.DelayStart(holdData)
	require.NoError(t, err)

	waitForStatus(t, pt.stateMachine, models.StatusRedifDelay, time.Second)
	assert.True(t, pt.channelHold.IsActive())

	// 等待超时回调触发
	time.Sleep(500 * time.Millisecond)

	// channelHold 回调发 StatusChange → Auto
	waitForStatus(t, pt.stateMachine, models.StatusAuto, 2*time.Second)
}

// TestPlayThread_ChannelHold_ManualCancel 手动取消通道保持
func TestPlayThread_ChannelHold_ManualCancel(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	holdData := &ChannelHoldData{
		SignalID:    1,
		SignalName:  "测试信号",
		DurationMs:  5000, // 长时间保持
		ReturnTime:  time.Now().Add(5 * time.Second),
		ProgramName: "测试转播",
	}

	require.NoError(t, pt.DelayStart(holdData))
	waitForStatus(t, pt.stateMachine, models.StatusRedifDelay, time.Second)

	// 手动取消
	err := pt.DelayCancelManual()
	require.NoError(t, err)

	waitForStatus(t, pt.stateMachine, models.StatusAuto, 2*time.Second)
}

// TestPlayThread_IntercutArrived 插播到达 → 播出 → 播完 → 返回
func TestPlayThread_IntercutArrived(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 发送插播事件（1条素材）
	cutProgs := []*models.Program{{
		ID:       "cut-prog-0",
		Name:     "插播素材",
		FilePath: "/audio/cut.mp3",
		Duration: 10000,
		Volume:   1.0,
	}}

	pt.eventBus.IntercutArrive <- IntercutEvent{
		ID:        "cut-1",
		Type:      models.IntercutTimed,
		Programs:  cutProgs,
		SectionID: "sec-1",
	}
	time.Sleep(200 * time.Millisecond)

	assert.True(t, pt.IsCutPlaying(), "应进入插播状态")
	assert.True(t, pt.intercutMgr.IsActive(), "插播栈应有条目")

	// 插播素材播完 → 自动返回
	pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}
	time.Sleep(300 * time.Millisecond)

	assert.False(t, pt.IsCutPlaying(), "插播应结束")
	assert.False(t, pt.intercutMgr.IsActive(), "插播栈应为空")
	assert.NotNil(t, pt.CurrentProgram(), "应恢复播出")
}

// TestPlayThread_IntercutMultiProgram 多素材插播
func TestPlayThread_IntercutMultiProgram(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 3 条素材的插播
	cutProgs := make([]*models.Program, 3)
	for i := range cutProgs {
		cutProgs[i] = &models.Program{
			ID:       fmt.Sprintf("cut-prog-%d", i),
			Name:     fmt.Sprintf("插播%d", i),
			FilePath: fmt.Sprintf("/audio/cut%d.mp3", i),
			Duration: 5000,
			Volume:   1.0,
		}
	}

	pt.eventBus.IntercutArrive <- IntercutEvent{
		ID:        "cut-multi",
		Type:      models.IntercutTimed,
		Programs:  cutProgs,
		SectionID: "sec-2",
	}
	time.Sleep(200 * time.Millisecond)

	assert.True(t, pt.IsCutPlaying())

	// 第一条播完 → 应继续播第二条
	pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}
	time.Sleep(200 * time.Millisecond)
	assert.True(t, pt.IsCutPlaying(), "3 条素材中第 1 条完，应继续插播")

	// 第二条播完 → 播第三条
	pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}
	time.Sleep(200 * time.Millisecond)
	assert.True(t, pt.IsCutPlaying(), "3 条素材中第 2 条完，应继续插播")

	// 第三条播完 → 返回
	pt.eventBus.PlayFinished <- PlayFinishedEvent{Channel: int(models.ChanMainOut)}
	time.Sleep(300 * time.Millisecond)
	assert.False(t, pt.IsCutPlaying(), "全部播完应返回")
}

// TestPlayThread_FixTimeClearsIntercut 定时到达清除插播
func TestPlayThread_FixTimeClearsIntercut(t *testing.T) {
	pl := testPlaylist(5)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	// 开始插播
	pt.eventBus.IntercutArrive <- IntercutEvent{
		ID:       "cut-fix",
		Type:     models.IntercutTimed,
		Programs: []*models.Program{{
			ID: "c1", Name: "c1", FilePath: "/c1.mp3", Duration: 10000, Volume: 1.0,
		}},
		SectionID: "sec-3",
	}
	time.Sleep(200 * time.Millisecond)
	assert.True(t, pt.IsCutPlaying())

	// 硬定时到达 → 应清除插播
	pt.eventBus.FixTimeArrive <- FixTimeEvent{
		BlockID:  "block-0",
		TaskType: models.TaskHard,
		DelayMs:  10,
	}
	time.Sleep(300 * time.Millisecond)

	assert.False(t, pt.IsCutPlaying(), "定时到达应清除插播标记")
}

// TestPlayThread_StopPlayback 停止播出
func TestPlayThread_StopPlayback(t *testing.T) {
	pl := testPlaylist(3)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)
	assert.NotNil(t, pt.CurrentProgram())

	// 停止
	require.NoError(t, pt.ChangeStatus(models.StatusStopped, "test stop"))
	waitForStatus(t, pt.stateMachine, models.StatusStopped, time.Second)
	time.Sleep(100 * time.Millisecond)

	assert.Nil(t, pt.CurrentProgram(), "停止后当前素材应清空")
}

// TestPlayThread_Suspend_Resume 挂起 / 恢复
func TestPlayThread_Suspend_Resume(t *testing.T) {
	pl := testPlaylist(3)
	pt, _, cleanup := startPlayThread(t, pl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt.eventBus)

	require.NoError(t, pt.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(200 * time.Millisecond)

	pt.Suspend()
	assert.True(t, pt.suspended.Load())

	// 挂起后 playNextClip 应返回 false
	result := pt.playNextClip(false)
	assert.False(t, result, "挂起时 playNextClip 应失败")

	pt.Resume()
	assert.False(t, pt.suspended.Load())
}

// TestPlayThread_EmrgCutErrors 紧急插播错误边界
func TestPlayThread_EmrgCutErrors(t *testing.T) {
	pt, _, cleanup := startPlayThread(t, nil)
	defer cleanup()

	// 停止状态下不能启动紧急插播
	err := pt.EmrgCutStart(1, "test")
	assert.Error(t, err)

	// 非紧急状态不能结束
	err = pt.EmrgCutStop()
	assert.Error(t, err)
}

// TestPlayThread_DelayStartErrors 通道保持错误边界
func TestPlayThread_DelayStartErrors(t *testing.T) {
	pt, _, cleanup := startPlayThread(t, nil)
	defer cleanup()

	// nil 参数
	err := pt.DelayStart(nil)
	assert.Error(t, err)

	// 停止状态
	err = pt.DelayStart(&ChannelHoldData{ReturnTime: time.Now().Add(time.Hour)})
	assert.Error(t, err)

	// 过期时间
	pt2, _, cleanup2 := startPlayThread(t, testPlaylist(1))
	defer cleanup2()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drainBroadcast(ctx, pt2.eventBus)

	require.NoError(t, pt2.ChangeStatus(models.StatusAuto, "test"))
	waitForStatus(t, pt2.stateMachine, models.StatusAuto, time.Second)
	time.Sleep(100 * time.Millisecond)

	err = pt2.DelayStart(&ChannelHoldData{
		ReturnTime: time.Now().Add(-1 * time.Hour),
	})
	assert.Error(t, err, "过期时间应报错")
}
