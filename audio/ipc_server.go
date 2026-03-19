package audio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/rixingyingyao/playthread-go/bridge"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// IPCServer IPC 服务端，运行在播放服务子进程中。
// 从 stdin 读取 JSON Line 请求，通过 stdout 返回响应和推送事件。
// stdout 独占用于 IPC 通信，所有日志输出到 stderr。
type IPCServer struct {
	mu     sync.Mutex   // 保护 stdout 写入
	engine *BassEngine  // BASS 引擎
	stdin  io.Reader
	stdout io.Writer
}

// NewIPCServer 创建 IPC 服务端
func NewIPCServer(engine *BassEngine) *IPCServer {
	return &IPCServer{
		engine: engine,
		stdin:  os.Stdin,
		stdout: os.Stdout,
	}
}

// InitLogging 初始化子进程日志——必须在最早时机调用。
// 确保所有日志输出到 stderr，不污染 stdout IPC 通道。
func InitLogging() {
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}

// Run 启动 IPC 服务端主循环，阻塞直到 stdin 关闭
func (s *IPCServer) Run() {
	scanner := bufio.NewScanner(s.stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req bridge.IPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Error().Err(err).Str("line", string(line)).Msg("IPC 请求解析失败")
			continue
		}

		// 处理请求
		resp := s.handleRequest(&req)
		s.writeResponse(resp)
	}

	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("stdin 读取错误")
	}

	log.Info().Msg("stdin 关闭，IPC 服务端退出")
}

// handleRequest 分发请求到对应处理器
func (s *IPCServer) handleRequest(req *bridge.IPCRequest) *bridge.IPCResponse {
	switch req.Method {
	case bridge.MethodPing:
		return s.success(req.ID, "pong")

	case bridge.MethodInit:
		return s.handleInit(req)

	case bridge.MethodLoad:
		return s.handleLoad(req)

	case bridge.MethodPlay:
		return s.handlePlay(req)

	case bridge.MethodStop:
		return s.handleStop(req)

	case bridge.MethodPause:
		return s.handlePause(req)

	case bridge.MethodResume:
		return s.handleResume(req)

	case bridge.MethodSetVolume:
		return s.handleSetVolume(req)

	case bridge.MethodPosition:
		return s.handlePosition(req)

	case bridge.MethodLevel:
		return s.handleLevel(req)

	case bridge.MethodFreeChannel:
		return s.handleFreeChannel(req)

	case bridge.MethodFreeAll:
		return s.handleFreeAll(req)

	case bridge.MethodShutdown:
		return s.handleShutdown(req)

	default:
		return s.fail(req.ID, fmt.Sprintf("未知方法: %s", req.Method))
	}
}

// --- 请求处理器 ---

func (s *IPCServer) handleInit(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.InitParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.Init(params.Device, params.Freq); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handleLoad(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.LoadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.Load(params.Channel, params.FilePath, params.IsEncrypt, params.Volume); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handlePlay(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.PlayParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.Play(params.Channel, params.Restart); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handleStop(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.StopParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.Stop(params.Channel, params.FadeOut); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handlePause(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.ChannelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.Pause(params.Channel); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handleResume(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.ChannelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.Resume(params.Channel); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handleSetVolume(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.VolumeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	if err := s.engine.SetVolume(params.Channel, params.Volume); err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, nil)
}

func (s *IPCServer) handlePosition(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.ChannelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	pos, dur, err := s.engine.GetPosition(params.Channel)
	if err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, &bridge.PositionResult{PositionMs: pos, DurationMs: dur})
}

func (s *IPCServer) handleLevel(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.ChannelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	left, right, err := s.engine.GetLevel(params.Channel)
	if err != nil {
		return s.fail(req.ID, err.Error())
	}
	return s.success(req.ID, &bridge.LevelResult{Left: left, Right: right})
}

func (s *IPCServer) handleFreeChannel(req *bridge.IPCRequest) *bridge.IPCResponse {
	var params bridge.ChannelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.fail(req.ID, fmt.Sprintf("参数解析失败: %v", err))
	}
	s.engine.FreeChannel(params.Channel)
	return s.success(req.ID, nil)
}

func (s *IPCServer) handleFreeAll(req *bridge.IPCRequest) *bridge.IPCResponse {
	s.engine.FreeAll()
	return s.success(req.ID, nil)
}

func (s *IPCServer) handleShutdown(req *bridge.IPCRequest) *bridge.IPCResponse {
	log.Info().Msg("收到 shutdown 命令，准备退出")
	resp := s.success(req.ID, nil)
	// 响应发送后退出（由 main 处理 os.Exit）
	go func() {
		s.engine.Shutdown()
		os.Exit(0)
	}()
	return resp
}

// --- 响应辅助 ---

func (s *IPCServer) success(id string, data interface{}) *bridge.IPCResponse {
	resp := &bridge.IPCResponse{ID: id}
	if data != nil {
		raw, _ := json.Marshal(data)
		resp.Data = raw
	}
	return resp
}

func (s *IPCServer) fail(id string, errMsg string) *bridge.IPCResponse {
	return &bridge.IPCResponse{ID: id, Error: errMsg}
}

func (s *IPCServer) writeResponse(resp *bridge.IPCResponse) {
	line, err := json.Marshal(resp)
	if err != nil {
		log.Error().Err(err).Msg("序列化响应失败")
		return
	}
	line = append(line, '\n')

	s.mu.Lock()
	_, writeErr := s.stdout.Write(line)
	s.mu.Unlock()

	if writeErr != nil {
		log.Error().Err(writeErr).Msg("写入 stdout 失败")
	}
}

// PushEvent 向主控推送异步事件
func (s *IPCServer) PushEvent(event string, data interface{}) {
	evt := bridge.IPCEvent{Event: event}
	if data != nil {
		raw, _ := json.Marshal(data)
		evt.Data = raw
	}

	line, err := json.Marshal(evt)
	if err != nil {
		log.Error().Err(err).Msg("序列化事件失败")
		return
	}
	line = append(line, '\n')

	s.mu.Lock()
	_, writeErr := s.stdout.Write(line)
	s.mu.Unlock()

	if writeErr != nil {
		log.Error().Err(writeErr).Msg("推送事件失败")
	}
}
