package bridge

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ProcessManager 管理播放服务子进程的生命周期。
// 负责启动、监控、崩溃重启（指数退避）。
type ProcessManager struct {
	exePath     string        // 子进程可执行文件路径
	mu          sync.Mutex
	cmd         *exec.Cmd
	bridge      *AudioBridge
	cancel      context.CancelFunc
	crashCount  int           // 连续崩溃计数
	ipcTimeout  time.Duration // IPC 请求超时
	onEvent     func(*IPCEvent) // 事件回调
	waitDone    chan error     // cmd.Wait() 结果通知
	stopping    bool          // 是否正在主动停止
}

// NewProcessManager 创建子进程管理器
func NewProcessManager(exePath string, ipcTimeout time.Duration) *ProcessManager {
	return &ProcessManager{
		exePath:    exePath,
		ipcTimeout: ipcTimeout,
	}
}

// SetEventHandler 设置事件处理回调
func (pm *ProcessManager) SetEventHandler(handler func(*IPCEvent)) {
	pm.onEvent = handler
}

// Bridge 返回当前 AudioBridge 实例（可能为 nil）
func (pm *ProcessManager) Bridge() *AudioBridge {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.bridge
}

// Start 启动子进程并建立 IPC 连接
func (pm *ProcessManager) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.startLocked(ctx)
}

func (pm *ProcessManager) startLocked(ctx context.Context) error {
	childCtx, cancel := context.WithCancel(ctx)
	pm.cancel = cancel

	cmd := exec.CommandContext(childCtx, pm.exePath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("创建 stdin 管道失败: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}

	// stderr 独立读取用于日志
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("启动子进程失败: %w", err)
	}

	pm.cmd = cmd
	pm.bridge = NewAudioBridge(stdin, stdout, pm.ipcTimeout)
	pm.waitDone = make(chan error, 1)
	pm.stopping = false

	// 单一 Wait() goroutine，防止双重调用
	go func() {
		pm.waitDone <- cmd.Wait()
	}()

	go pm.drainStderr(stderr)
	go pm.forwardEvents(childCtx)
	go pm.watchProcess(ctx)

	log.Info().Str("path", pm.exePath).Int("pid", cmd.Process.Pid).Msg("播放服务子进程已启动")
	return nil
}

// Stop 优雅停止子进程
func (pm *ProcessManager) Stop() {
	pm.mu.Lock()
	pm.stopping = true

	if pm.bridge != nil {
		_ = pm.bridge.Shutdown()
	}

	if pm.cancel != nil {
		pm.cancel()
	}

	waitDone := pm.waitDone
	cmd := pm.cmd
	pm.mu.Unlock()

	if cmd != nil && cmd.Process != nil && waitDone != nil {
		select {
		case <-waitDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-waitDone
		}
	}

	pm.mu.Lock()
	pm.bridge = nil
	pm.cmd = nil
	pm.mu.Unlock()
	log.Info().Msg("播放服务子进程已停止")
}

// watchProcess 监控子进程退出，崩溃时自动重启
func (pm *ProcessManager) watchProcess(parentCtx context.Context) {
	if pm.waitDone == nil {
		return
	}

	var err error
	select {
	case err = <-pm.waitDone:
	case <-parentCtx.Done():
		return
	}

	pm.mu.Lock()
	if pm.stopping {
		pm.mu.Unlock()
		return
	}
	pm.mu.Unlock()

	// 子进程意外退出，执行重启
	pm.mu.Lock()
	pm.crashCount++
	count := pm.crashCount
	pm.bridge = nil
	pm.cmd = nil
	pm.mu.Unlock()

	log.Error().Err(err).Int("crash_count", count).Msg("播放服务子进程崩溃")

	// 计算退避延迟
	delay := pm.backoffDelay(count)
	log.Info().Dur("delay", delay).Int("crash_count", count).Msg("等待后重启子进程")

	select {
	case <-time.After(delay):
	case <-parentCtx.Done():
		return
	}

	pm.mu.Lock()
	if err := pm.startLocked(parentCtx); err != nil {
		log.Error().Err(err).Msg("重启子进程失败")
	} else {
		log.Info().Int("crash_count", count).Msg("子进程重启成功")
	}
	pm.mu.Unlock()
}

// backoffDelay 计算指数退避延迟
// 前 5 次立即重启，之后每次 +2s，上限 30s
func (pm *ProcessManager) backoffDelay(crashCount int) time.Duration {
	if crashCount <= 5 {
		return 0
	}
	delay := time.Duration(crashCount-5) * 2 * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

// ResetCrashCount 重置崩溃计数（子进程稳定运行一段时间后调用）
func (pm *ProcessManager) ResetCrashCount() {
	pm.mu.Lock()
	pm.crashCount = 0
	pm.mu.Unlock()
}

// drainStderr 读取子进程 stderr 并记录日志
func (pm *ProcessManager) drainStderr(stderr io.ReadCloser) {
	buf := make([]byte, 4096)
	for {
		n, err := stderr.Read(buf)
		if n > 0 {
			log.Debug().Str("source", "audio-service").Str("stderr", string(buf[:n])).Msg("子进程日志")
		}
		if err != nil {
			return
		}
	}
}

// forwardEvents 将子进程事件转发给注册的处理器
func (pm *ProcessManager) forwardEvents(ctx context.Context) {
	bridge := pm.Bridge()
	if bridge == nil {
		return
	}
	for {
		select {
		case evt := <-bridge.EventCh():
			if pm.onEvent != nil {
				pm.onEvent(evt)
			}
		case <-ctx.Done():
			return
		}
	}
}
