// monitor 提供运行时监控、崩溃日志收集和子进程崩溃统计。
// 周期性采样 CPU/内存/goroutine 指标，异常时记录告警日志。
package infra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Monitor 运行时监控器
type Monitor struct {
	cfg       *MonitorConfig
	crashLog  string // 崩溃日志文件路径

	mu          sync.Mutex
	crashStats  map[string]*CrashStat // key = 子进程名
	lastMetrics RuntimeMetrics
}

// CrashStat 崩溃统计
type CrashStat struct {
	ProcessName    string    `json:"process_name"`
	TotalCrashes   int       `json:"total_crashes"`
	LastCrashTime  time.Time `json:"last_crash_time"`
	LastExitCode   int       `json:"last_exit_code"`
	ConsecutiveNum int       `json:"consecutive_num"` // 连续崩溃次数
}

// RuntimeMetrics 运行时指标
type RuntimeMetrics struct {
	Timestamp     time.Time `json:"timestamp"`
	HeapAllocMB   float64   `json:"heap_alloc_mb"`
	HeapSysMB     float64   `json:"heap_sys_mb"`
	StackInUseMB  float64   `json:"stack_in_use_mb"`
	NumGoroutine  int       `json:"num_goroutine"`
	NumGC         uint32    `json:"num_gc"`
	GCPauseTotalMs float64  `json:"gc_pause_total_ms"`
}

// NewMonitor 创建监控器
func NewMonitor(cfg *MonitorConfig, dataDir string) *Monitor {
	return &Monitor{
		cfg:        cfg,
		crashLog:   filepath.Join(dataDir, "crash.log"),
		crashStats: make(map[string]*CrashStat),
	}
}

// Run 启动周期性监控（阻塞，直到 ctx 取消）
func (m *Monitor) Run(ctx context.Context) {
	interval := time.Duration(m.cfg.MemoryCheckIntervalS) * time.Second
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collectMetrics()
		}
	}
}

// collectMetrics 采集运行时指标
func (m *Monitor) collectMetrics() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	metrics := RuntimeMetrics{
		Timestamp:      time.Now(),
		HeapAllocMB:    float64(ms.HeapAlloc) / 1024 / 1024,
		HeapSysMB:      float64(ms.HeapSys) / 1024 / 1024,
		StackInUseMB:   float64(ms.StackInuse) / 1024 / 1024,
		NumGoroutine:   runtime.NumGoroutine(),
		NumGC:          ms.NumGC,
		GCPauseTotalMs: float64(ms.PauseTotalNs) / 1e6,
	}

	m.mu.Lock()
	m.lastMetrics = metrics
	m.mu.Unlock()

	log.Debug().
		Float64("heap_mb", metrics.HeapAllocMB).
		Int("goroutines", metrics.NumGoroutine).
		Uint32("gc_count", metrics.NumGC).
		Msg("运行时指标")

	// 内存告警
	if m.cfg.MemoryWarnThresholdMB > 0 && metrics.HeapAllocMB > float64(m.cfg.MemoryWarnThresholdMB) {
		log.Warn().
			Float64("heap_mb", metrics.HeapAllocMB).
			Int("threshold_mb", m.cfg.MemoryWarnThresholdMB).
			Msg("内存使用超过告警阈值")
	}
}

// Metrics 返回最近一次采集的运行时指标
func (m *Monitor) Metrics() RuntimeMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMetrics
}

// RecordCrash 记录子进程崩溃事件
func (m *Monitor) RecordCrash(processName string, exitCode int) {
	now := time.Now()

	m.mu.Lock()
	stat, ok := m.crashStats[processName]
	if !ok {
		stat = &CrashStat{ProcessName: processName}
		m.crashStats[processName] = stat
	}
	stat.TotalCrashes++
	stat.LastCrashTime = now
	stat.LastExitCode = exitCode
	stat.ConsecutiveNum++
	m.mu.Unlock()

	log.Error().
		Str("process", processName).
		Int("exit_code", exitCode).
		Int("total_crashes", stat.TotalCrashes).
		Int("consecutive", stat.ConsecutiveNum).
		Msg("子进程崩溃")

	// 追加写入崩溃日志文件
	m.appendCrashLog(now, processName, exitCode)
}

// ResetConsecutive 重置连续崩溃计数（子进程稳定运行后调用）
func (m *Monitor) ResetConsecutive(processName string) {
	m.mu.Lock()
	if stat, ok := m.crashStats[processName]; ok {
		stat.ConsecutiveNum = 0
	}
	m.mu.Unlock()
}

// CrashStats 返回所有崩溃统计（快照）
func (m *Monitor) CrashStats() map[string]CrashStat {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]CrashStat, len(m.crashStats))
	for k, v := range m.crashStats {
		result[k] = *v
	}
	return result
}

// appendCrashLog 追加崩溃记录到日志文件
func (m *Monitor) appendCrashLog(when time.Time, processName string, exitCode int) {
	dir := filepath.Dir(m.crashLog)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Error().Err(err).Msg("创建崩溃日志目录失败")
		return
	}

	f, err := os.OpenFile(m.crashLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Error().Err(err).Msg("打开崩溃日志文件失败")
		return
	}
	defer f.Close()

	line := fmt.Sprintf("%s process=%s exit_code=%d\n",
		when.Format("2006-01-02 15:04:05.000"),
		processName, exitCode)
	f.WriteString(line)
}
