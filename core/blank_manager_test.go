package core

import (
	"testing"
	"time"

	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBlankManagerWithHistory(eb *EventBus, history *infra.BlankHistory) *BlankManager {
	return NewBlankManager(
		BlankManagerConfig{
			EnableAI:      false,
			AIThresholdMs: 60000,
			FadeOutMs:     500,
			CueRetry:      3,
		},
		eb,
		nil,
		history,
		func() int { return -1 },
	)
}

func makeBlankClips(n int) []*models.Program {
	clips := make([]*models.Program, n)
	for i := range clips {
		clips[i] = &models.Program{
			ID:        string(rune('A' + i)),
			Name:      "垫乐_" + string(rune('A'+i)),
			ProgramID: i + 1,
			FilePath:  "/audio/blank_" + string(rune('a'+i)) + ".mp3",
			Duration:  180000,
			Volume:    0.8,
		}
	}
	return clips
}

// --- 三态生命周期 ---

func TestBlankManager_InitialState(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	assert.Equal(t, BlankStopped, bm.State())
	assert.False(t, bm.IsPlaying())
	assert.False(t, bm.IsEnabled())
	assert.Nil(t, bm.CurrentClip())
}

func TestBlankManager_PrepareWithClips(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	bm.SetClips(makeBlankClips(3))

	ok := bm.Prepare()
	assert.True(t, ok)
	assert.Equal(t, BlankPrepared, bm.State())
	assert.NotNil(t, bm.CurrentClip())
}

func TestBlankManager_PrepareNoClips(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	ok := bm.Prepare()
	assert.False(t, ok)
	assert.Equal(t, BlankStopped, bm.State())
}

func TestBlankManager_PlayLifecycle(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)
	bm.SetClips(makeBlankClips(3))

	ok := bm.Play()
	assert.True(t, ok)
	assert.Equal(t, BlankPlaying, bm.State())
	assert.True(t, bm.IsPlaying())
	assert.True(t, bm.IsEnabled())

	bm.Stop()
	assert.Equal(t, BlankStopped, bm.State())
	assert.False(t, bm.IsPlaying())
	assert.False(t, bm.IsEnabled())
}

func TestBlankManager_StartIfNeeded(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)
	bm.SetClips(makeBlankClips(3))

	ok := bm.StartIfNeeded()
	assert.True(t, ok)
	assert.True(t, bm.IsPlaying())

	ok = bm.StartIfNeeded()
	assert.True(t, ok, "重复调用应幂等")
}

func TestBlankManager_StopIdempotent(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	bm.Stop()
	bm.Stop()
	assert.Equal(t, BlankStopped, bm.State())
}

// --- YieldTo ---

func TestBlankManager_YieldTo(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)
	bm.SetClips(makeBlankClips(3))

	bm.Play()
	require.True(t, bm.IsPlaying())

	bm.YieldTo(100)
	assert.Equal(t, BlankStopped, bm.State())
	assert.Nil(t, bm.CurrentClip())
}

func TestBlankManager_YieldToWhenNotPlaying(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	bm.YieldTo(100)
	assert.Equal(t, BlankStopped, bm.State())
}

// --- FadeToNext ---

func TestBlankManager_FadeToNext(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)
	bm.SetClips(makeBlankClips(5))

	bm.Play()
	require.True(t, bm.IsPlaying())

	firstClip := bm.CurrentClip()
	require.NotNil(t, firstClip)

	ok := bm.FadeToNext()
	assert.True(t, ok)
	assert.True(t, bm.IsPlaying())
	assert.NotNil(t, bm.CurrentClip())
}

func TestBlankManager_FadeToNextWhenNotPlaying(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	ok := bm.FadeToNext()
	assert.False(t, ok)
}

// --- LRU 选曲去重 ---

func TestBlankManager_LRUSelection(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)

	clips := makeBlankClips(3)
	bm.SetClips(clips)

	// 第一首应选未播放过的
	bm.Prepare()
	first := bm.CurrentClip()
	require.NotNil(t, first)

	// 标记第一首为已播
	history.Add(first.ProgramID, time.Now(), 1000)

	bm.Stop()
	bm.Prepare()
	second := bm.CurrentClip()
	require.NotNil(t, second)

	// 第二首应与第一首不同（从未播放优先）
	if len(clips) > 1 {
		assert.NotEqual(t, first.ProgramID, second.ProgramID, "LRU 应选不同的素材")
	}
}

// --- AI 智能选曲 ---

func TestBlankManager_AISelection_ShortGap(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := NewBlankManager(
		BlankManagerConfig{
			EnableAI:      true,
			AIThresholdMs: 60000,
			FadeOutMs:     500,
			CueRetry:      3,
		},
		eb,
		nil,
		history,
		func() int { return 30000 }, // 30 秒 < 60000ms 阈值
	)

	normalClips := makeBlankClips(3)
	idleClips := []*models.Program{
		{ID: "idle1", Name: "轻音乐1", ProgramID: 100, FilePath: "/audio/idle1.mp3", Duration: 120000, Volume: 0.5},
		{ID: "idle2", Name: "轻音乐2", ProgramID: 101, FilePath: "/audio/idle2.mp3", Duration: 120000, Volume: 0.5},
	}

	bm.SetClips(normalClips)
	bm.SetIdleClips(idleClips)

	bm.Prepare()
	clip := bm.CurrentClip()
	require.NotNil(t, clip)

	isIdle := clip.ProgramID == 100 || clip.ProgramID == 101
	assert.True(t, isIdle, "间隙短于阈值时应选轻音乐，实际选了: %s", clip.Name)
}

func TestBlankManager_AISelection_LongGap(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := NewBlankManager(
		BlankManagerConfig{
			EnableAI:      true,
			AIThresholdMs: 60000,
			FadeOutMs:     500,
			CueRetry:      3,
		},
		eb,
		nil,
		history,
		func() int { return 120000 }, // 120 秒 > 60000ms 阈值
	)

	normalClips := makeBlankClips(3)
	idleClips := []*models.Program{
		{ID: "idle1", Name: "轻音乐1", ProgramID: 100, FilePath: "/audio/idle1.mp3"},
	}

	bm.SetClips(normalClips)
	bm.SetIdleClips(idleClips)

	bm.Prepare()
	clip := bm.CurrentClip()
	require.NotNil(t, clip)

	isNormal := clip.ProgramID >= 1 && clip.ProgramID <= 3
	assert.True(t, isNormal, "间隙长于阈值时应选常规垫乐，实际选了: %s", clip.Name)
}

func TestBlankManager_AISelection_NoNextTask(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := NewBlankManager(
		BlankManagerConfig{
			EnableAI:      true,
			AIThresholdMs: 60000,
			FadeOutMs:     500,
			CueRetry:      3,
		},
		eb,
		nil,
		history,
		func() int { return -1 }, // 无后续任务
	)

	bm.SetClips(makeBlankClips(3))
	bm.SetIdleClips([]*models.Program{
		{ID: "idle1", Name: "轻音乐1", ProgramID: 100, FilePath: "/audio/idle1.mp3"},
	})

	bm.Prepare()
	clip := bm.CurrentClip()
	require.NotNil(t, clip)

	isNormal := clip.ProgramID >= 1 && clip.ProgramID <= 3
	assert.True(t, isNormal, "无后续任务时应选常规垫乐")
}

// --- 事件发送 ---

func TestBlankManager_EmitsBlankStartedEvent(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)
	bm.SetClips(makeBlankClips(3))

	bm.Play()

	select {
	case evt := <-eb.Broadcast:
		assert.Equal(t, models.EventBlankStarted, evt.Type)
	case <-time.After(1 * time.Second):
		t.Fatal("未收到 BlankStarted 广播事件")
	}
}

func TestBlankManager_EmitsBlankStoppedEvent(t *testing.T) {
	eb := newTestEventBus()
	history := infra.NewBlankHistory(t.TempDir(), 2)
	bm := newTestBlankManagerWithHistory(eb, history)
	bm.SetClips(makeBlankClips(3))

	bm.Play()
	<-eb.Broadcast // 消费 BlankStarted

	bm.Stop()

	select {
	case evt := <-eb.Broadcast:
		assert.Equal(t, models.EventBlankStopped, evt.Type)
	case <-time.After(1 * time.Second):
		t.Fatal("未收到 BlankStopped 广播事件")
	}
}
