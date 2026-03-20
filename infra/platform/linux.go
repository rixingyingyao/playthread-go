// 构建约束：非 Windows 系统（Linux 等）
//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// SetHighResTimer Linux 下无需设置，内核默认支持高精度定时器
func SetHighResTimer() {}

// RestoreTimer Linux 下无需恢复
func RestoreTimer() {}

// ErrAlreadyRunning 表示已有实例在运行
var ErrAlreadyRunning = fmt.Errorf("已有实例在运行")

// singletonFd 保存文件锁的文件描述符
var singletonFd *os.File

// AcquireSingletonLock 通过文件锁确保单例运行（Linux 版本）。
// name 用于生成锁文件路径 /tmp/<name>.lock。
// 返回 0 和 nil 表示成功获取锁；若已有实例运行返回 ErrAlreadyRunning。
func AcquireSingletonLock(name string) (uintptr, error) {
	lockPath := filepath.Join(os.TempDir(), name+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, fmt.Errorf("创建锁文件失败: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return 0, ErrAlreadyRunning
	}

	// 写入 PID
	_ = f.Truncate(0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
	singletonFd = f
	return 0, nil
}

// ReleaseSingletonLock 释放单例文件锁（Linux 版本）
func ReleaseSingletonLock(_ uintptr) {
	if singletonFd != nil {
		singletonFd.Close()
		singletonFd = nil
	}
}
