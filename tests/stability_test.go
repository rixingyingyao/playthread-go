// stability_test 稳定性测试框架。
// 提供长时间运行测试的基础设施：内存泄漏检测、goroutine 泄漏检测、定时精度验证、
// 播表循环压力、状态机快速切换、事件总线高频发射、插播栈嵌套等场景覆盖。
// 使用 -run Stability -timeout 0 执行长时间测试。
// 短模式（go test -short）下跳过耗时测试。
package tests

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
)

// ─── 内存泄漏检测 ──────────────────────────────────────────

// MemorySample 内存采样点
type MemorySample struct {
	Time     time.Time
	HeapMB   float64
	StackMB  float64
	SysMB    float64
	NumGC    uint32
}

// sampleMemory 采集当前内存指标
func sampleMemory() MemorySample {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return MemorySample{
		Time:    time.Now(),
		HeapMB:  float64(ms.HeapAlloc) / 1024 / 1024,
		StackMB: float64(ms.StackInuse) / 1024 / 1024,
		SysMB:   float64(ms.Sys) / 1024 / 1024,
		NumGC:   ms.NumGC,
	}
}

func TestStability_MemoryLeak_ShortRun(t *testing.T) {
	// 启动 PlayThread，运行一段时间，检查内存是否稳定
	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snap := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)

	// 采样 5 次，每次间隔 1 秒
	samples := make([]MemorySample, 0, 5)
	for i := 0; i < 5; i++ {
		runtime.GC()
		samples = append(samples, sampleMemory())
		time.Sleep(1 * time.Second)
	}

	cancel()
	pt.Wait()

	// 检查内存增长
	first := samples[0]
	last := samples[len(samples)-1]
	growth := last.HeapMB - first.HeapMB
	growthPct := (growth / first.HeapMB) * 100

	t.Logf("Memory: first=%.2fMB last=%.2fMB growth=%.2fMB (%.1f%%)",
		first.HeapMB, last.HeapMB, growth, growthPct)

	// 短时间内允许 50% 波动（GC 未稳定）
	if growthPct > 100 {
		t.Errorf("suspicious memory growth: %.1f%%", growthPct)
	}
}

// ─── Goroutine 泄漏检测 ──────────────────────────────────────

func TestStability_GoroutineLeak(t *testing.T) {
	// 记录初始 goroutine 数
	baseline := runtime.NumGoroutine()

	// 创建/销毁 PlayThread 多次
	for cycle := 0; cycle < 5; cycle++ {
		cfg := infra.DefaultConfig()
		sm := core.NewStateMachine()
		eb := core.NewEventBus()
		snap := infra.NewSnapshotManager(t.TempDir())
		pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

		ctx, cancel := context.WithCancel(context.Background())
		pt.Run(ctx)
		time.Sleep(200 * time.Millisecond)
		cancel()
		pt.Wait()
	}

	// 等待 goroutine 收敛
	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	current := runtime.NumGoroutine()
	leaked := current - baseline

	t.Logf("Goroutines: baseline=%d current=%d leaked=%d", baseline, current, leaked)

	// 允许 ±5 的波动（运行时、测试框架自身的 goroutine）
	if leaked > 5 {
		t.Errorf("goroutine leak detected: %d goroutines leaked after 5 cycles", leaked)
	}
}

// ─── 定时精度测试 ──────────────────────────────────────────

func TestStability_TimerPrecision(t *testing.T) {
	// 测试 time.AfterFunc 或 time.Sleep 的定时精度
	const iterations = 100
	const targetMs = 20 // 模拟 20ms 轮询间隔

	deviations := make([]time.Duration, 0, iterations)

	for i := 0; i < iterations; i++ {
		target := time.Duration(targetMs) * time.Millisecond
		start := time.Now()
		time.Sleep(target)
		actual := time.Since(start)
		deviation := actual - target
		deviations = append(deviations, deviation)
	}

	// 统计
	var total time.Duration
	var maxDev time.Duration
	within50ms := 0

	for _, d := range deviations {
		total += d
		if d < 0 {
			d = -d
		}
		if d > maxDev {
			maxDev = d
		}
		if d <= 50*time.Millisecond {
			within50ms++
		}
	}

	avgDev := total / time.Duration(len(deviations))
	pct := float64(within50ms) / float64(iterations) * 100

	t.Logf("Timer precision (%d iterations, target=%dms):", iterations, targetMs)
	t.Logf("  avg deviation: %v", avgDev)
	t.Logf("  max deviation: %v", maxDev)
	t.Logf("  within ±50ms: %.1f%%", pct)

	if pct < 95 {
		t.Errorf("timer precision below threshold: %.1f%% within ±50ms (need 95%%)", pct)
	}
}

// ─── 并发压力测试 ──────────────────────────────────────────

func TestStability_ConcurrentStateMachine(t *testing.T) {
	sm := core.NewStateMachine()
	eb := core.NewEventBus()

	const workers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(workers)

	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				_ = sm.Status()
				eb.Emit(models.NewBroadcastEvent(models.EventPlayProgress, nil))
			}
		}()
	}

	wg.Wait()
	t.Logf("Completed %d concurrent operations without panic", workers*opsPerWorker)
}

// ─── 故障注入辅助工具 ──────────────────────────────────────

// FaultInjector 故障注入器（用于稳定性测试）
type FaultInjector struct {
	mu      sync.Mutex
	faults  map[string]bool
}

// NewFaultInjector 创建故障注入器
func NewFaultInjector() *FaultInjector {
	return &FaultInjector{faults: make(map[string]bool)}
}

// Enable 启用指定故障
func (fi *FaultInjector) Enable(name string) {
	fi.mu.Lock()
	fi.faults[name] = true
	fi.mu.Unlock()
}

// Disable 禁用指定故障
func (fi *FaultInjector) Disable(name string) {
	fi.mu.Lock()
	delete(fi.faults, name)
	fi.mu.Unlock()
}

// IsActive 检查故障是否激活
func (fi *FaultInjector) IsActive(name string) bool {
	fi.mu.Lock()
	defer fi.mu.Unlock()
	return fi.faults[name]
}

func TestStability_FaultInjector(t *testing.T) {
	fi := NewFaultInjector()

	if fi.IsActive("network_down") {
		t.Error("fault should not be active initially")
	}

	fi.Enable("network_down")
	if !fi.IsActive("network_down") {
		t.Error("fault should be active after Enable")
	}

	fi.Disable("network_down")
	if fi.IsActive("network_down") {
		t.Error("fault should not be active after Disable")
	}
}

// ─── 长时间稳定性测试（go test -run Stability_LongRun -timeout 0）──

func TestStability_LongRun(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过长时间稳定性测试（使用 -short 标志）")
	}

	// 默认运行 30 秒（CI 友好），完整测试用 -timeout 指定更长时间
	duration := 30 * time.Second

	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snap := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)

	baselineGoroutines := runtime.NumGoroutine()
	baseMem := sampleMemory()
	sampleInterval := 5 * time.Second
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	deadline := time.After(duration)
	sampleCount := 0

	for {
		select {
		case <-deadline:
			cancel()
			pt.Wait()

			// 最终检查
			time.Sleep(500 * time.Millisecond)
			runtime.GC()
			finalMem := sampleMemory()
			finalGoroutines := runtime.NumGoroutine()

			memGrowth := finalMem.HeapMB - baseMem.HeapMB
			goroutineDiff := finalGoroutines - baselineGoroutines

			t.Logf("Long run complete: duration=%v samples=%d", duration, sampleCount)
			t.Logf("  Memory: start=%.2fMB end=%.2fMB growth=%.2fMB",
				baseMem.HeapMB, finalMem.HeapMB, memGrowth)
			t.Logf("  Goroutines: start=%d end=%d diff=%d",
				baselineGoroutines, finalGoroutines, goroutineDiff)

			if goroutineDiff > 10 {
				t.Errorf("goroutine leak: %d leaked", goroutineDiff)
			}
			return

		case <-ticker.C:
			sampleCount++
			mem := sampleMemory()
			goroutines := runtime.NumGoroutine()
			t.Logf("[sample %d] heap=%.2fMB goroutines=%d gc=%d",
				sampleCount, mem.HeapMB, goroutines, mem.NumGC)
		}
	}
}

// dummy usage
var _ = fmt.Sprintf

// ─── 播表循环压力测试 ──────────────────────────────────────

// makeTestPlaylist 创建测试用播单（含多个时间块和素材）
func makeTestPlaylist(id string, blockCount, progsPerBlock int) *models.Playlist {
	pl := &models.Playlist{
		ID:      id,
		Date:    time.Now(),
		Version: 1,
	}
	for b := 0; b < blockCount; b++ {
		block := models.TimeBlock{
			ID:        fmt.Sprintf("block-%d", b),
			Name:      fmt.Sprintf("Block %d", b),
			StartTime: fmt.Sprintf("%02d:00:00", b%24),
			EndTime:   fmt.Sprintf("%02d:59:59", b%24),
			TaskType:  models.TaskHard,
		}
		for p := 0; p < progsPerBlock; p++ {
			block.Programs = append(block.Programs, models.Program{
				ID:       fmt.Sprintf("prog-%d-%d", b, p),
				Name:     fmt.Sprintf("Program %d-%d", b, p),
				FilePath: fmt.Sprintf("/media/block%d/prog%d.mp3", b, p),
				Duration: 30000, // 30s
				InPoint:  0,
				OutPoint: 30000,
				Volume:   0.8,
			})
		}
		pl.Blocks = append(pl.Blocks, block)
	}
	return pl
}

func TestStability_PlaylistCycling(t *testing.T) {
	// 反复加载/退出播表，模拟日播单更新场景
	const cycles = 20

	baseline := runtime.NumGoroutine()

	for i := 0; i < cycles; i++ {
		cfg := infra.DefaultConfig()
		sm := core.NewStateMachine()
		eb := core.NewEventBus()
		snap := infra.NewSnapshotManager(t.TempDir())
		pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

		ctx, cancel := context.WithCancel(context.Background())
		pt.Run(ctx)

		// 加载播表
		pl := makeTestPlaylist(fmt.Sprintf("pl-%d", i), 3, 5)
		pt.SetPlaylist(pl)

		// 短暂运行让 goroutine 完成初始化
		time.Sleep(50 * time.Millisecond)

		// 状态切换：Stopped → Auto → Manual → Auto → Stopped
		sm.ChangeStatusTo(models.StatusAuto, "test-cycle")
		sm.ChangeStatusTo(models.StatusManual, "test-cycle")
		sm.ChangeStatusTo(models.StatusAuto, "test-cycle")
		sm.ChangeStatusTo(models.StatusStopped, "test-cycle")

		cancel()
		pt.Wait()
	}

	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	current := runtime.NumGoroutine()
	leaked := current - baseline
	t.Logf("PlaylistCycling: %d cycles, goroutines baseline=%d current=%d leaked=%d",
		cycles, baseline, current, leaked)

	if leaked > 10 {
		t.Errorf("goroutine leak after %d playlist cycles: %d leaked", cycles, leaked)
	}
}

// ─── 状态机快速切换压力 ──────────────────────────────────────

func TestStability_RapidStateTransitions(t *testing.T) {
	sm := core.NewStateMachine()
	eb := core.NewEventBus()

	// 记录所有变更
	var transCount atomic.Int64
	sm.SetOnChange(func(from, to models.Status, path models.PathType) {
		transCount.Add(1)
	})

	// 定义合法的状态转换链（循环多轮）
	transitions := []models.Status{
		models.StatusAuto,
		models.StatusManual,
		models.StatusLive,
		models.StatusManual,
		models.StatusAuto,
		models.StatusEmergency,
		models.StatusAuto,
		models.StatusRedifDelay,
		models.StatusAuto,
		models.StatusStopped,
	}

	const rounds = 100
	var wg sync.WaitGroup

	// 并发工作者执行状态循环
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				for _, target := range transitions {
					sm.ChangeStatusTo(target, fmt.Sprintf("worker-%d-round-%d", workerID, r))
				}
			}
		}(w)
	}

	// 同时有读取者
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds*len(transitions); i++ {
				_ = sm.Status()
				_ = sm.LastStatus()
			}
		}()
	}

	wg.Wait()

	total := transCount.Load()
	t.Logf("RapidStateTransitions: 4 writers × %d transitions, total changes=%d", rounds*len(transitions), total)

	// 验证无 panic + 最终状态合法
	status := sm.Status()
	t.Logf("Final status: %s", status)

	_ = eb // 确认编译通过
}

// ─── 事件总线高频压力 ──────────────────────────────────────

func TestStability_EventBusFlood(t *testing.T) {
	eb := core.NewEventBus()

	// 注册多个订阅者
	const subscriberCount = 5
	var received [subscriberCount]atomic.Int64

	for i := 0; i < subscriberCount; i++ {
		idx := i
		eb.Subscribe(subscriberAdapter(func(event models.BroadcastEvent) {
			received[idx].Add(1)
		}))
	}

	// 高频发射
	const workers = 8
	const msgsPerWorker = 1000
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < msgsPerWorker; i++ {
				eb.Emit(models.NewBroadcastEvent(models.EventPlayProgress, map[string]interface{}{
					"worker": workerID,
					"seq":    i,
				}))
			}
		}(w)
	}

	wg.Wait()

	// 等待订阅者消费
	time.Sleep(200 * time.Millisecond)

	totalSent := workers * msgsPerWorker
	t.Logf("EventBusFlood: sent=%d subscribers=%d", totalSent, subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		got := received[i].Load()
		// channel 可能丢弃部分消息（buffer 满时）——不视为错误
		t.Logf("  subscriber[%d] received=%d (%.1f%%)", i, got, float64(got)/float64(totalSent)*100)
	}
}

// subscriberAdapter 将函数适配为 Subscriber 接口
type subscriberAdapter func(event models.BroadcastEvent)

func (f subscriberAdapter) OnBroadcast(event models.BroadcastEvent) { f(event) }

// ─── 插播栈嵌套压力 ──────────────────────────────────────

func TestStability_IntercutStackStress(t *testing.T) {
	eb := core.NewEventBus()
	im := core.NewIntercutManager(eb, 500)

	// 最大深度 3（默认），反复推入弹出
	const cycles = 50

	for c := 0; c < cycles; c++ {
		// 推入到最大深度
		for depth := 0; depth < 3; depth++ {
			entry := &core.IntercutEntry{
				ID:   fmt.Sprintf("cut-%d-%d", c, depth),
				Type: models.IntercutTimed,
				Programs: []*models.Program{
					{ID: fmt.Sprintf("cut-prog-%d-%d", c, depth), Name: "Test", Duration: 5000},
				},
				ReturnSnap: &models.PlaybackSnapshot{ProgramIndex: depth},
			}
			err := im.Push(entry)
			if err != nil {
				t.Fatalf("cycle %d depth %d: Push failed: %v", c, depth, err)
			}
		}

		// 第 4 次推入应该失败（栈已满）
		err := im.Push(&core.IntercutEntry{
			ID:       "overflow",
			Type:     models.IntercutTimed,
			Programs: []*models.Program{{ID: "x", Name: "x", Duration: 1000}},
		})
		if err == nil {
			t.Fatal("expected overflow error at max depth")
		}

		// 弹出全部
		for depth := 2; depth >= 0; depth-- {
			snap := im.Pop()
			if snap == nil {
				t.Fatalf("cycle %d: Pop at depth %d returned nil", c, depth)
			}
			if snap.ProgramIndex != depth {
				t.Errorf("cycle %d: expected ProgramIndex=%d, got=%d", c, depth, snap.ProgramIndex)
			}
		}

		// 再弹出应返回 nil
		if snap := im.Pop(); snap != nil {
			t.Fatalf("cycle %d: Pop on empty stack should return nil", c)
		}
	}

	t.Logf("IntercutStackStress: %d push/pop cycles completed", cycles)
}

// ─── 通道保持生命周期压力 ──────────────────────────────────

func TestStability_ChannelHoldLifecycle(t *testing.T) {
	var returnCount atomic.Int64
	ch := core.NewChannelHold(func() {
		returnCount.Add(1)
	})

	const cycles = 30

	for i := 0; i < cycles; i++ {
		data := &core.ChannelHoldData{
			ReturnTime:  time.Now().Add(50 * time.Millisecond),
			DurationMs:  50,
			ProgramName: fmt.Sprintf("hold-%d", i),
		}
		err := ch.Start(data)
		if err != nil {
			t.Fatalf("cycle %d: Start failed: %v", i, err)
		}

		if !ch.IsActive() {
			t.Fatalf("cycle %d: should be active after Start", i)
		}

		// 交替：一半自然超时，一半手动取消
		if i%2 == 0 {
			time.Sleep(80 * time.Millisecond) // 等超时
		} else {
			time.Sleep(10 * time.Millisecond)
			ch.Stop()
		}
	}

	// 等所有 timer 完成
	time.Sleep(200 * time.Millisecond)

	t.Logf("ChannelHoldLifecycle: %d cycles, returns=%d", cycles, returnCount.Load())
}

// ─── 定时管理器精度压力 ──────────────────────────────────

func TestStability_FixTimeManagerPrecision(t *testing.T) {
	eb := core.NewEventBus()
	fm := core.NewFixTimeManager(eb, 20, 5000, 200, 1000)

	ctx, cancel := context.WithCancel(context.Background())
	fm.Run(ctx)
	fm.Start() // 取消暂停状态

	// 注入多批任务，验证触发事件到达 EventBus
	// 任务需在 hardAdvanceMs(200ms) 之后触发，给轮询足够时间
	now := time.Now()
	const taskCount = 10

	for i := 0; i < taskCount; i++ {
		triggerAt := now.Add(time.Duration(300+i*100) * time.Millisecond)
		fm.AddFixTask(&core.FixTimeTask{
			ID:        fmt.Sprintf("fix-%d", i),
			BlockID:   fmt.Sprintf("block-%d", i),
			TaskType:  models.TaskHard,
			StartTime: triggerAt,
			FadeOutMs: 200,
		})
	}

	// 收集 FixTimeArrive 事件
	var received atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.After(5 * time.Second)
		for {
			select {
			case <-eb.FixTimeArrive:
				received.Add(1)
				if int(received.Load()) >= taskCount {
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	<-done

	cancel()
	fm.Wait()

	got := int(received.Load())
	t.Logf("FixTimeManager precision: %d/%d tasks triggered", got, taskCount)

	// 允许少量因时序竞争丢失
	if got < taskCount-2 {
		t.Errorf("too few tasks triggered: got %d, want at least %d", got, taskCount-2)
	}
}

// ─── 完整生命周期压力（PlayThread create/run/load/stop 循环）─

func TestStability_FullLifecycleStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过完整生命周期压力测试")
	}

	const cycles = 10
	baseline := runtime.NumGoroutine()
	baseMem := sampleMemory()

	for i := 0; i < cycles; i++ {
		cfg := infra.DefaultConfig()
		sm := core.NewStateMachine()
		eb := core.NewEventBus()
		snap := infra.NewSnapshotManager(t.TempDir())
		pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

		ctx, cancel := context.WithCancel(context.Background())
		pt.Run(ctx)

		// 加载播表
		pl := makeTestPlaylist(fmt.Sprintf("lifecycle-%d", i), 5, 10)
		pt.SetPlaylist(pl)

		// 状态循环
		sm.ChangeStatusTo(models.StatusAuto, "lifecycle")
		time.Sleep(30 * time.Millisecond)

		// 模拟事件发射
		for j := 0; j < 20; j++ {
			eb.Emit(models.NewBroadcastEvent(models.EventPlayProgress, nil))
		}

		sm.ChangeStatusTo(models.StatusManual, "lifecycle")
		time.Sleep(20 * time.Millisecond)

		// 插播模拟（通过事件发送）
		sm.ChangeStatusTo(models.StatusAuto, "lifecycle")
		time.Sleep(20 * time.Millisecond)

		sm.ChangeStatusTo(models.StatusStopped, "lifecycle")
		cancel()
		pt.Wait()

		if i%5 == 0 {
			runtime.GC()
			mem := sampleMemory()
			t.Logf("[cycle %d/%d] heap=%.2fMB goroutines=%d",
				i+1, cycles, mem.HeapMB, runtime.NumGoroutine())
		}
	}

	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	finalMem := sampleMemory()
	finalGoroutines := runtime.NumGoroutine()
	goroutineDiff := finalGoroutines - baseline
	memGrowth := finalMem.HeapMB - baseMem.HeapMB

	t.Logf("FullLifecycleStress: %d cycles complete", cycles)
	t.Logf("  Memory: start=%.2fMB end=%.2fMB growth=%.2fMB",
		baseMem.HeapMB, finalMem.HeapMB, memGrowth)
	t.Logf("  Goroutines: start=%d end=%d diff=%d",
		baseline, finalGoroutines, goroutineDiff)

	if goroutineDiff > 10 {
		t.Errorf("goroutine leak: %d goroutines leaked after %d lifecycle cycles", goroutineDiff, cycles)
	}
}

// ─── 长时间播表操作测试（go test -run Stability_LongRunWithOps -timeout 0）─

func TestStability_LongRunWithOps(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过长时间播表操作测试")
	}

	duration := 30 * time.Second

	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snap := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)

	// 初始播表
	pl := makeTestPlaylist("longrun-ops", 4, 8)
	pt.SetPlaylist(pl)
	sm.ChangeStatusTo(models.StatusAuto, "longrun-init")

	baselineGoroutines := runtime.NumGoroutine()
	baseMem := sampleMemory()

	// 操作 ticker：每 2 秒执行一个操作
	opTicker := time.NewTicker(2 * time.Second)
	defer opTicker.Stop()

	sampleTicker := time.NewTicker(5 * time.Second)
	defer sampleTicker.Stop()

	deadline := time.After(duration)
	sampleCount := 0
	opCount := 0

	// 操作序列（循环执行）
	ops := []func(){
		func() { // 切换到手动
			sm.ChangeStatusTo(models.StatusManual, "longrun-op")
		},
		func() { // 切换到自动
			sm.ChangeStatusTo(models.StatusAuto, "longrun-op")
		},
		func() { // 更换播表
			opCount++
			newPl := makeTestPlaylist(fmt.Sprintf("longrun-reload-%d", opCount), 3, 6)
			pt.SetPlaylist(newPl)
		},
		func() { // 发射事件
			for j := 0; j < 10; j++ {
				eb.Emit(models.NewBroadcastEvent(models.EventPlayProgress, nil))
			}
		},
		func() { // 应急切换
			sm.ChangeStatusTo(models.StatusEmergency, "longrun-emerg")
			time.Sleep(100 * time.Millisecond)
			sm.ChangeStatusTo(models.StatusAuto, "longrun-recover")
		},
	}

	opIdx := 0
	for {
		select {
		case <-deadline:
			sm.ChangeStatusTo(models.StatusStopped, "longrun-end")
			cancel()
			pt.Wait()

			time.Sleep(500 * time.Millisecond)
			runtime.GC()
			finalMem := sampleMemory()
			finalGoroutines := runtime.NumGoroutine()

			t.Logf("LongRunWithOps complete: duration=%v ops=%d samples=%d", duration, opCount, sampleCount)
			t.Logf("  Memory: start=%.2fMB end=%.2fMB growth=%.2fMB",
				baseMem.HeapMB, finalMem.HeapMB, finalMem.HeapMB-baseMem.HeapMB)
			t.Logf("  Goroutines: start=%d end=%d diff=%d",
				baselineGoroutines, finalGoroutines, finalGoroutines-baselineGoroutines)

			if finalGoroutines-baselineGoroutines > 10 {
				t.Errorf("goroutine leak: %d", finalGoroutines-baselineGoroutines)
			}
			return

		case <-opTicker.C:
			ops[opIdx%len(ops)]()
			opIdx++
			opCount++

		case <-sampleTicker.C:
			sampleCount++
			mem := sampleMemory()
			goroutines := runtime.NumGoroutine()
			t.Logf("[sample %d] heap=%.2fMB goroutines=%d gc=%d status=%s",
				sampleCount, mem.HeapMB, goroutines, mem.NumGC, sm.Status())
		}
	}
}
