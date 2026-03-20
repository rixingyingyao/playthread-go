package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelHold_StartAndTimeout(t *testing.T) {
	var returned atomic.Bool
	ch := NewChannelHold(func() {
		returned.Store(true)
	})

	data := &ChannelHoldData{
		DurationMs:  200,
		SignalID:    1,
		SignalName:  "央广新闻",
		ProgramID:   100,
		ProgramName: "新闻联播",
	}

	require.NoError(t, ch.Start(data))
	assert.True(t, ch.IsActive())
	assert.False(t, ch.IsManualCancel())

	rem := ch.RemainingMs()
	assert.Greater(t, rem, 0)
	assert.LessOrEqual(t, rem, 200)

	elapsed := ch.ElapsedMs()
	assert.GreaterOrEqual(t, elapsed, 0)

	time.Sleep(300 * time.Millisecond)

	assert.True(t, returned.Load(), "超时后应触发回调")
	assert.False(t, ch.IsActive(), "超时后应变为不活跃")
}

func TestChannelHold_ManualStop(t *testing.T) {
	var returned atomic.Bool
	ch := NewChannelHold(func() {
		returned.Store(true)
	})

	data := &ChannelHoldData{
		DurationMs: 5000,
		SignalID:   2,
		SignalName: "体育转播",
	}

	require.NoError(t, ch.Start(data))
	assert.True(t, ch.IsActive())

	ch.Stop()

	assert.False(t, ch.IsActive())
	assert.True(t, ch.IsManualCancel())

	time.Sleep(100 * time.Millisecond)
	assert.False(t, returned.Load(), "手动停止不应触发回调")
}

func TestChannelHold_Reset(t *testing.T) {
	ch := NewChannelHold(func() {})

	require.NoError(t, ch.Start(&ChannelHoldData{DurationMs: 5000}))
	assert.True(t, ch.IsActive())

	ch.Reset()
	assert.False(t, ch.IsActive())
	assert.False(t, ch.IsManualCancel())
	assert.Nil(t, ch.Data())
}

func TestChannelHold_RestartOverwrite(t *testing.T) {
	var callCount atomic.Int32
	ch := NewChannelHold(func() {
		callCount.Add(1)
	})

	require.NoError(t, ch.Start(&ChannelHoldData{
		DurationMs: 5000,
		SignalName: "第一次",
	}))

	require.NoError(t, ch.Start(&ChannelHoldData{
		DurationMs: 200,
		SignalName: "第二次",
	}))

	assert.True(t, ch.IsActive())

	time.Sleep(300 * time.Millisecond)

	assert.Equal(t, int32(1), callCount.Load(), "只有第二个 timer 应触发回调")
}

func TestChannelHold_InactiveOperations(t *testing.T) {
	ch := NewChannelHold(func() {})

	assert.False(t, ch.IsActive())
	assert.False(t, ch.IsManualCancel())
	assert.Nil(t, ch.Data())
	assert.Equal(t, 0, ch.RemainingMs())
	assert.Equal(t, 0, ch.ElapsedMs())

	ch.Stop()
	ch.Reset()
}

func TestChannelHold_RemainingElapsed(t *testing.T) {
	ch := NewChannelHold(func() {})

	require.NoError(t, ch.Start(&ChannelHoldData{DurationMs: 1000}))

	time.Sleep(100 * time.Millisecond)

	remaining := ch.RemainingMs()
	elapsed := ch.ElapsedMs()

	assert.Greater(t, remaining, 0)
	assert.Less(t, remaining, 1000)
	assert.Greater(t, elapsed, 50)
	assert.Less(t, elapsed, 300)

	total := remaining + elapsed
	assert.InDelta(t, 1000, total, 100, "remaining + elapsed ≈ duration")

	ch.Stop()
}

func TestChannelHold_DataAccess(t *testing.T) {
	ch := NewChannelHold(func() {})

	data := &ChannelHoldData{
		DurationMs:  3000,
		SignalID:    42,
		SignalName:  "测试信号",
		ProgramID:   99,
		ProgramName: "测试节目",
		IsAIDelay:   true,
	}

	require.NoError(t, ch.Start(data))

	got := ch.Data()
	require.NotNil(t, got)
	assert.Equal(t, 42, got.SignalID)
	assert.Equal(t, "测试信号", got.SignalName)
	assert.Equal(t, 99, got.ProgramID)
	assert.True(t, got.IsAIDelay)

	ch.Stop()
	assert.Nil(t, ch.Data())
}
