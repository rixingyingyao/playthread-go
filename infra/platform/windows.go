// 构建约束：仅 Windows 编译
//go:build windows

package platform

import "syscall"

var (
	winmm              = syscall.NewLazyDLL("winmm.dll")
	procTimeBeginPeriod = winmm.NewProc("timeBeginPeriod")
	procTimeEndPeriod   = winmm.NewProc("timeEndPeriod")
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
