// 构建约束：仅 Windows 编译
//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	winmm               = syscall.NewLazyDLL("winmm.dll")
	procTimeBeginPeriod  = winmm.NewProc("timeBeginPeriod")
	procTimeEndPeriod    = winmm.NewProc("timeEndPeriod")

	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex    = kernel32.NewProc("CreateMutexW")
)

// SetHighResTimer 设置 Windows 高精度定时器（1ms 分辨率）。
// 广播播出系统要求毫秒级定时精度，Windows 默认定时器分辨率 15.6ms。
// 必须在程序启动时调用，退出时调用 RestoreTimer。
func SetHighResTimer() {
	procTimeBeginPeriod.Call(1)
}

// RestoreTimer 恢复默认定时器分辨率
func RestoreTimer() {
	procTimeEndPeriod.Call(1)
}

// AcquireSingletonLock 通过 Windows 命名互斥体确保单例运行。
// 返回 mutex 句柄（程序退出时释放）和 error。
// 若已有实例运行，返回 ErrAlreadyRunning。
func AcquireSingletonLock(name string) (syscall.Handle, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("互斥体名称无效: %w", err)
	}

	handle, _, lastErr := procCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	if handle == 0 {
		return 0, fmt.Errorf("创建互斥体失败: %v", lastErr)
	}

	// ERROR_ALREADY_EXISTS = 183
	if lastErr.(syscall.Errno) == 183 {
		syscall.CloseHandle(syscall.Handle(handle))
		return 0, ErrAlreadyRunning
	}

	return syscall.Handle(handle), nil
}

// ReleaseSingletonLock 释放单例互斥体
func ReleaseSingletonLock(handle syscall.Handle) {
	if handle != 0 {
		syscall.CloseHandle(handle)
	}
}

// ErrAlreadyRunning 表示已有实例在运行
var ErrAlreadyRunning = fmt.Errorf("已有实例在运行")
