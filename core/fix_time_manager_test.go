package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEventBus() *EventBus {
	return NewEventBus()
}

func newTestFixTimeManager(eb *EventBus) *FixTimeManager {
	return NewFixTimeManager(eb, 20, 3000, 50, 0)
}

// --- 任务管理 ---

func TestFixTimeManager_SetAndClear(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	tasks := []*FixTimeTask{
		{ID: "t1", BlockID: "b1", TaskType: models.TaskHard, StartTime: time.Now().Add(10 * time.Second)},
		{ID: "t2", BlockID: "b2", TaskType: models.TaskSoft, StartTime: time.Now().Add(20 * time.Second)},
	}
	fm.SetFixTasks(tasks)
	assert.Equal(t, 2, fm.FixTaskCount())

	fm.ClearFixTasks()
	assert.Equal(t, 0, fm.FixTaskCount())
}

func TestFixTimeManager_AddAndRemove(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	fm.AddFixTask(&FixTimeTask{ID: "a1", StartTime: time.Now().Add(5 * time.Second)})
	fm.AddFixTask(&FixTimeTask{ID: "a2", StartTime: time.Now().Add(3 * time.Second)})
	assert.Equal(t, 2, fm.FixTaskCount())

	fm.RemoveFixTask("a1")
	assert.Equal(t, 1, fm.FixTaskCount())

	fm.RemoveFixTask("nonexistent")
	assert.Equal(t, 1, fm.FixTaskCount())
}

// --- 排序（StartTime + ArrangeID） ---

func TestFixTimeManager_SortByTimeAndArrangeID(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	baseTime := time.Now().Add(10 * time.Second)
	fm.SetFixTasks([]*FixTimeTask{
		{ID: "c", ArrangeID: 3, StartTime: baseTime},
		{ID: "a", ArrangeID: 1, StartTime: baseTime},
		{ID: "b", ArrangeID: 2, StartTime: baseTime},
		{ID: "d", ArrangeID: 0, StartTime: baseTime.Add(-5 * time.Second)},
	})

	fm.mu.Lock()
	assert.Equal(t, "d", fm.fixTasks[0].ID)
	assert.Equal(t, "a", fm.fixTasks[1].ID)
	assert.Equal(t, "b", fm.fixTasks[2].ID)
	assert.Equal(t, "c", fm.fixTasks[3].ID)
	fm.mu.Unlock()
}

// --- 硬定时触发 ---

func TestFixTimeManager_HardFixTrigger(t *testing.T) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 10, 3000, 0, 0) // 0ms 提前量

	triggerTime := time.Now().Add(100 * time.Millisecond)
	fm.SetFixTasks([]*FixTimeTask{
		{ID: "h1", BlockID: "b1", TaskType: models.TaskHard, StartTime: triggerTime, FadeOutMs: 50},
	})
	fm.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Run(ctx)

	select {
	case evt := <-eb.FixTimeArrive:
		assert.Equal(t, "b1", evt.BlockID)
		assert.Equal(t, models.TaskHard, evt.TaskType)
		assert.Equal(t, 50, evt.DelayMs)
	case <-time.After(2 * time.Second):
		t.Fatal("硬定时未触发（超时 2 秒）")
	}
}

// --- 软定时触发 ---

func TestFixTimeManager_SoftFixTrigger(t *testing.T) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 10, 3000, 50, 0)

	triggerTime := time.Now().Add(100 * time.Millisecond)
	fm.SetFixTasks([]*FixTimeTask{
		{ID: "s1", BlockID: "b2", TaskType: models.TaskSoft, StartTime: triggerTime},
	})
	fm.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Run(ctx)

	select {
	case evt := <-eb.FixTimeArrive:
		assert.Equal(t, "b2", evt.BlockID)
		assert.Equal(t, models.TaskSoft, evt.TaskType)
	case <-time.After(2 * time.Second):
		t.Fatal("软定时未触发")
	}
}

// --- 过期任务丢弃 ---

func TestFixTimeManager_ExpiredTaskDiscarded(t *testing.T) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 10, 3000, 0, 0)

	// 任务已过期 5 秒（超过 taskExpireMs=3000ms）
	fm.SetFixTasks([]*FixTimeTask{
		{ID: "exp1", BlockID: "b1", TaskType: models.TaskHard, StartTime: time.Now().Add(-5 * time.Second)},
	})
	fm.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Run(ctx)

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, fm.FixTaskCount(), "过期任务应被清理")

	select {
	case <-eb.FixTimeArrive:
		t.Fatal("过期任务不应触发事件")
	default:
	}
}

// --- IsNearFixTask ---

func TestFixTimeManager_IsNearFixTask(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	fm.SetFixTasks([]*FixTimeTask{
		{ID: "near1", StartTime: time.Now().Add(500 * time.Millisecond)},
	})

	assert.True(t, fm.IsNearFixTask(1000), "500ms 内有任务，应为 true")
	assert.False(t, fm.IsNearFixTask(200), "200ms 内无任务，应为 false")
}

func TestFixTimeManager_IsNearFixTask_NoTasks(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	assert.False(t, fm.IsNearFixTask(1000))
}

// --- NextFixTaskTime ---

func TestFixTimeManager_NextFixTaskTime(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	t1 := time.Now().Add(5 * time.Second)
	t2 := time.Now().Add(10 * time.Second)
	fm.SetFixTasks([]*FixTimeTask{
		{ID: "n1", StartTime: t1},
		{ID: "n2", StartTime: t2},
	})

	next, ok := fm.NextFixTaskTime()
	assert.True(t, ok)
	assert.WithinDuration(t, t1, next, time.Millisecond)
}

func TestFixTimeManager_NextFixTaskTime_Empty(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	_, ok := fm.NextFixTaskTime()
	assert.False(t, ok)
}

// --- GetPaddingTimeMs ---

func TestFixTimeManager_GetPaddingTimeMs(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	assert.Equal(t, -1, fm.GetPaddingTimeMs(), "无任务时返回 -1")

	fm.SetFixTasks([]*FixTimeTask{
		{ID: "p1", StartTime: time.Now().Add(30 * time.Second)},
	})

	ms := fm.GetPaddingTimeMs()
	assert.True(t, ms > 29000 && ms <= 30000, "距离下一定时约 30 秒，实际: %d", ms)
}

// --- Pause/Start ---

func TestFixTimeManager_PauseStart(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	assert.True(t, fm.IsPaused(), "初始应为暂停状态")

	fm.Start()
	assert.False(t, fm.IsPaused())

	fm.Pause()
	assert.True(t, fm.IsPaused())
}

func TestFixTimeManager_PausedNoTrigger(t *testing.T) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 10, 3000, 0, 0)

	fm.SetFixTasks([]*FixTimeTask{
		{ID: "paused1", BlockID: "b1", TaskType: models.TaskHard, StartTime: time.Now().Add(50 * time.Millisecond)},
	})
	// 不调用 Start()，保持 paused

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Run(ctx)

	time.Sleep(300 * time.Millisecond)

	select {
	case <-eb.FixTimeArrive:
		t.Fatal("暂停状态不应触发事件")
	default:
	}
}

// --- 插播任务 ---

func TestFixTimeManager_IntercutTrigger(t *testing.T) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 10, 3000, 50, 0)

	triggerTime := time.Now().Add(100 * time.Millisecond)
	fm.SetIntercutTasks([]*IntercutTask{
		{ID: "ic1", Type: models.IntercutTimed, StartTime: triggerTime, FadeOutMs: 100},
	})
	fm.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Run(ctx)

	select {
	case evt := <-eb.IntercutArrive:
		assert.Equal(t, "ic1", evt.ID)
		assert.Equal(t, models.IntercutTimed, evt.Type)
		assert.Equal(t, 100, evt.DelayMs)
	case <-time.After(2 * time.Second):
		t.Fatal("插播任务未触发")
	}
}

// --- 多任务按序触发 ---

func TestFixTimeManager_MultipleTasksSequential(t *testing.T) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 10, 3000, 0, 0)

	now := time.Now()
	fm.SetFixTasks([]*FixTimeTask{
		{ID: "m1", BlockID: "b1", TaskType: models.TaskHard, StartTime: now.Add(100 * time.Millisecond)},
		{ID: "m2", BlockID: "b2", TaskType: models.TaskHard, StartTime: now.Add(300 * time.Millisecond)},
	})
	fm.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Run(ctx)

	evt1 := <-eb.FixTimeArrive
	assert.Equal(t, "b1", evt1.BlockID)

	evt2 := <-eb.FixTimeArrive
	assert.Equal(t, "b2", evt2.BlockID)
}

// --- 并发安全 ---

func TestFixTimeManager_ConcurrentAccess(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fm.Start()
	fm.Run(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fm.AddFixTask(&FixTimeTask{
				ID:        "concurrent_" + string(rune('a'+idx)),
				StartTime: time.Now().Add(time.Duration(idx+1) * time.Second),
			})
			fm.IsNearFixTask(1000)
			fm.GetPaddingTimeMs()
			fm.FixTaskCount()
		}(i)
	}
	wg.Wait()
}

// --- 基准测试 ---

func BenchmarkFixTimeManager_CheckFixTasks(b *testing.B) {
	eb := newTestEventBus()
	fm := NewFixTimeManager(eb, 20, 3000, 50, 0)
	fm.Start()

	// 添加 50 个未来任务
	tasks := make([]*FixTimeTask, 50)
	for i := range tasks {
		tasks[i] = &FixTimeTask{
			ID:        "bench_" + string(rune('a'+i)),
			StartTime: time.Now().Add(time.Duration(i+1) * time.Hour),
			TaskType:  models.TaskHard,
		}
	}
	fm.SetFixTasks(tasks)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fm.checkFixTasks()
	}
}

func BenchmarkFixTimeManager_IsNearFixTask(b *testing.B) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	fm.SetFixTasks([]*FixTimeTask{
		{ID: "bench1", StartTime: time.Now().Add(1 * time.Second)},
		{ID: "bench2", StartTime: time.Now().Add(2 * time.Second)},
		{ID: "bench3", StartTime: time.Now().Add(3 * time.Second)},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fm.IsNearFixTask(1000)
	}
}

// --- InitFromPlaylist ---

func TestFixTimeManager_InitFromPlaylist(t *testing.T) {
	eb := newTestEventBus()
	fm := newTestFixTimeManager(eb)

	baseDate := time.Now()
	futureTime := baseDate.Add(1 * time.Minute)

	playlist := &models.Playlist{
		Blocks: []models.TimeBlock{
			{
				ID:        "tb1",
				Name:      "未来时段",
				StartTime: futureTime.Format("15:04:05"),
				TaskType:  models.TaskHard,
			},
		},
	}

	fm.InitFromPlaylist(playlist, baseDate)
	require.Equal(t, 1, fm.FixTaskCount(), "应有 1 个定时任务")
}
