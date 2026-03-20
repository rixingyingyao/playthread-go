package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// UDPListener 本地紧急控制 UDP 监听器
type UDPListener struct {
	addr string
	pt   *core.PlayThread
	conn net.PacketConn
}

// NewUDPListener 创建 UDP 监听器
func NewUDPListener(addr string, pt *core.PlayThread) *UDPListener {
	return &UDPListener{addr: addr, pt: pt}
}

// Run 启动 UDP 监听（阻塞，需在 goroutine 中调用）
func (u *UDPListener) Run(ctx context.Context) error {
	conn, err := net.ListenPacket("udp", u.addr)
	if err != nil {
		return fmt.Errorf("UDP 监听失败 %s: %w", u.addr, err)
	}
	u.conn = conn
	log.Info().Str("addr", u.addr).Msg("UDP 监听已启动")

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 512)
	for {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				log.Info().Msg("UDP 监听已关闭")
				return nil
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Warn().Err(err).Msg("UDP 读取错误")
			continue
		}

		cmd := strings.TrimSpace(string(buf[:n]))
		log.Debug().Str("cmd", cmd).Str("from", remoteAddr.String()).Msg("UDP 收到命令")

		resp := u.handleCommand(cmd)
		conn.WriteTo([]byte(resp), remoteAddr)
	}
}

func (u *UDPListener) handleCommand(cmd string) string {
	switch strings.ToLower(cmd) {
	case "stop":
		if err := u.pt.ChangeStatus(models.StatusStopped, "UDP stop"); err != nil {
			return fmt.Sprintf(`{"code":1,"message":"%s"}`, err.Error())
		}
		return `{"code":0,"message":"stopped"}`

	case "play":
		if err := u.pt.ChangeStatus(models.StatusAuto, "UDP play"); err != nil {
			return fmt.Sprintf(`{"code":1,"message":"%s"}`, err.Error())
		}
		return `{"code":0,"message":"playing"}`

	case "padding":
		u.pt.StartBlank()
		return `{"code":0,"message":"padding started"}`

	case "status":
		prog := u.pt.CurrentProgram()
		progName := ""
		if prog != nil {
			progName = prog.Name
		}
		resp := map[string]interface{}{
			"status":  u.pt.Status().String(),
			"playing": progName,
		}
		data, _ := json.Marshal(resp)
		return string(data)

	case "next":
		u.pt.Next()
		return `{"code":0,"message":"next"}`

	default:
		return fmt.Sprintf(`{"code":1,"message":"unknown command: %s"}`, cmd)
	}
}
