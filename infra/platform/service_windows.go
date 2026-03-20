// 构建约束：仅 Windows 编译
//go:build windows

package platform

import (
	"context"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

// PlaythreadService 实现 svc.Handler 接口，用于 Windows 服务模式运行。
type PlaythreadService struct {
	// RunFunc 是实际业务逻辑入口，接收 ctx（服务停止时取消）。
	RunFunc func(ctx context.Context)
}

// Execute 实现 svc.Handler 接口。
// 由 Windows SCM 调用，负责处理服务控制请求。
func (s *PlaythreadService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const acceptCmds = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动业务逻辑
	done := make(chan struct{})
	go func() {
		s.RunFunc(ctx)
		close(done)
	}()

	status <- svc.Status{State: svc.Running, Accepts: acceptCmds}

	for {
		select {
		case cr := <-req:
			switch cr.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				// 等待业务逻辑退出，最多 10 秒
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
				return false, 0

			case svc.Interrogate:
				status <- cr.CurrentStatus
			}

		case <-done:
			// 业务逻辑自行退出
			return false, 0
		}
	}
}

// RunAsService 以 Windows 服务模式运行。
// serviceName 是注册到 SCM 的服务名称。
// runFunc 是实际业务入口，ctx 在服务停止时取消。
func RunAsService(serviceName string, runFunc func(ctx context.Context)) error {
	elog, err := eventlog.Open(serviceName)
	if err == nil {
		defer elog.Close()
		elog.Info(1, serviceName+" service starting")
	}

	err = svc.Run(serviceName, &PlaythreadService{RunFunc: runFunc})
	if err != nil {
		if elog != nil {
			elog.Error(1, serviceName+" service failed: "+err.Error())
		}
		return err
	}
	return nil
}

// IsInteractiveSession 检测当前进程是否运行在交互式会话（控制台模式）。
// 返回 true 表示控制台模式，false 表示可能是服务模式。
func IsInteractiveSession() (bool, error) {
	return svc.IsAnInteractiveSession()
}
