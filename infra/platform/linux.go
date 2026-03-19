// 构建约束：非 Windows 系统（Linux 等）
//go:build !windows

package platform

// SetHighResTimer Linux 下无需设置，内核默认支持高精度定时器
func SetHighResTimer() {}

// RestoreTimer Linux 下无需恢复
func RestoreTimer() {}
