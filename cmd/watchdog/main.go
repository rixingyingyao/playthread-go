// watchdog 外部守护进程。
// 纯 Go 构建（CGO_ENABLED=0），监控主控进程(playthread)存活。
// 主控崩溃时 1 秒内重新拉起，连续崩溃 5 次后等待 30 秒再重试。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

const (
	maxImmediateRestarts = 5        // 立即重启的最大次数
	cooldownDelay        = 30 * time.Second // 连续崩溃后的冷却等待
	checkInterval        = 1 * time.Second  // 进程存活检查间隔
	stableThreshold      = 60 * time.Second // 运行超过此时长则重置崩溃计数
)

func main() {
	targetPath := flag.String("target", "", "被守护进程的路径")
	logPath := flag.String("log", "watchdog.log", "守护进程日志文件路径")
	flag.Parse()

	if *targetPath == "" {
		// 默认：同目录下的 playthread 可执行文件
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "获取可执行文件路径失败: %v\n", err)
			os.Exit(1)
		}
		name := "playthread"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		defaultTarget := filepath.Join(filepath.Dir(exePath), name)
		targetPath = &defaultTarget
	}

	logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开日志文件失败: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	w := &watchdog{
		targetPath: *targetPath,
		targetArgs: flag.Args(),
		logFile:    logFile,
	}

	w.logf("watchdog 启动 version=%s target=%s", Version, *targetPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		w.logf("收到退出信号 %s，停止守护", sig)
		w.stop()
		os.Exit(0)
	}()

	w.run()
}

type watchdog struct {
	targetPath string
	targetArgs []string
	logFile    *os.File

	mu         sync.Mutex
	cmd        *exec.Cmd
	stopping   bool
	crashCount int
}

func (w *watchdog) run() {
	for {
		w.mu.Lock()
		if w.stopping {
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()

		startTime := time.Now()
		exitCode := w.runOnce()
		elapsed := time.Since(startTime)

		w.mu.Lock()
		if w.stopping {
			w.mu.Unlock()
			return
		}

		// 运行时间足够长，重置崩溃计数
		if elapsed >= stableThreshold {
			w.crashCount = 0
		}
		w.crashCount++
		count := w.crashCount
		w.mu.Unlock()

		w.logf("目标进程退出 code=%d elapsed=%v crash_count=%d", exitCode, elapsed, count)

		if count > maxImmediateRestarts {
			w.logf("连续崩溃 %d 次，冷却等待 %v", count, cooldownDelay)
			time.Sleep(cooldownDelay)
			// 冷却后重置计数
			w.mu.Lock()
			w.crashCount = 0
			w.mu.Unlock()
		} else {
			// 1 秒内重新拉起
			time.Sleep(checkInterval)
		}
	}
}

// runOnce 启动并等待目标进程退出，返回退出码
func (w *watchdog) runOnce() int {
	cmd := exec.Command(w.targetPath, w.targetArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	w.mu.Lock()
	w.cmd = cmd
	w.mu.Unlock()

	w.logf("启动目标进程: %s", w.targetPath)

	if err := cmd.Start(); err != nil {
		w.logf("启动失败: %v", err)
		return -1
	}

	w.logf("目标进程 PID=%d", cmd.Process.Pid)

	err := cmd.Wait()

	w.mu.Lock()
	w.cmd = nil
	w.mu.Unlock()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return -1
	}
	return 0
}

func (w *watchdog) stop() {
	w.mu.Lock()
	w.stopping = true
	cmd := w.cmd
	w.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// 先发送 SIGTERM / 优雅关闭信号
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
}

func (w *watchdog) logf(format string, args ...interface{}) {
	msg := fmt.Sprintf("%s [watchdog] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, args...))
	w.logFile.WriteString(msg)
	fmt.Print(msg)
}
