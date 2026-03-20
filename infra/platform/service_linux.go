// 构建约束：非 Windows 系统
//go:build !windows

package platform

import "context"

// RunAsService Linux 下不支持 Windows 服务模式，直接返回 nil（由 systemd 管理）。
func RunAsService(_ string, _ func(ctx context.Context)) error {
	return nil
}

// IsInteractiveSession Linux 下始终返回 true（交互式会话）。
// 服务模式由 systemd 管理，不需要代码层面区分。
func IsInteractiveSession() (bool, error) {
	return true, nil
}
