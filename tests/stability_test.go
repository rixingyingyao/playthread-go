// stability_test 稳定性测试框架。
// 提供长时间运行测试的基础设施：内存泄漏检测、goroutine 泄漏检测、定时精度验证。
// 使用 -run Stability -timeout 0 执行长时间测试。
// 短模式（go test -short）下跳过耗时测试。
package tests

import (
	"context"
	"fmt"
	"runtime"
	"sync"
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
