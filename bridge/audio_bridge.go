package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AudioBridge 音频桥接客户端，主控进程通过此组件与播放服务子进程通信。
// 所有方法线程安全。
type AudioBridge struct {
	mu       sync.Mutex    // 保护 stdin 写入的串行化
	stdin    io.Writer     // 子进程 stdin
	pending  sync.Map      // map[string]chan *IPCResponse — 等待响应的请求
	eventCh  chan *IPCEvent // 子进程推送的异步事件
	timeout  time.Duration // 请求超时
	closedCh chan struct{}  // readLoop 退出后关闭，通知所有等待者
}

// NewAudioBridge 创建音频桥接客户端
func NewAudioBridge(stdin io.Writer, stdout io.Reader, timeout time.Duration) *AudioBridge {
	ab := &AudioBridge{
		stdin:    stdin,
		eventCh:  make(chan *IPCEvent, 64),
		timeout:  timeout,
		closedCh: make(chan struct{}),
	}
	go ab.readLoop(stdout)
	return ab
}

// EventCh 返回事件通道，外部通过此通道接收子进程推送的异步事件
func (ab *AudioBridge) EventCh() <-chan *IPCEvent {
	return ab.eventCh
}

// Call 发送 IPC 请求并等待响应
func (ab *AudioBridge) Call(method string, params interface{}) (*IPCResponse, error) {
	// 序列化参数
	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("序列化参数失败: %w", err)
		}
		rawParams = data
	}

	id := uuid.New().String()
	req := &IPCRequest{
		ID:     id,
		Method: method,
		Params: rawParams,
	}

	// 注册等待通道
	respCh := make(chan *IPCResponse, 1)
	ab.pending.Store(id, respCh)
	defer ab.pending.Delete(id)

	// 序列化并写入 stdin（加锁保证写入原子性）
	line, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}
	line = append(line, '\n')

	ab.mu.Lock()
	_, err = ab.stdin.Write(line)
	ab.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("写入 stdin 失败: %w", err)
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ab.closedCh:
		return nil, fmt.Errorf("IPC 连接已断开: method=%s, id=%s", method, id)
	case <-time.After(ab.timeout):
		return nil, fmt.Errorf("IPC 请求超时: method=%s, id=%s, timeout=%v", method, id, ab.timeout)
	}
}

// readLoop 持续读取子进程 stdout，分发响应和事件。
// 当 stdout 关闭（子进程退出/管道断裂）时关闭 closedCh 通知所有等待者。
func (ab *AudioBridge) readLoop(stdout io.Reader) {
	defer close(ab.closedCh)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// 尝试解析为响应（有 id 字段）
		var resp IPCResponse
		if err := json.Unmarshal(line, &resp); err == nil && resp.ID != "" {
			if ch, ok := ab.pending.LoadAndDelete(resp.ID); ok {
				respCh := ch.(chan *IPCResponse)
				respCh <- &resp
			}
			continue
		}

		// 尝试解析为事件（有 event 字段）
		var evt IPCEvent
		if err := json.Unmarshal(line, &evt); err == nil && evt.Event != "" {
			select {
			case ab.eventCh <- &evt:
			default:
				// 事件通道满，丢弃最旧事件
			}
			continue
		}

		// 无法解析的行，忽略（可能是子进程的非 JSON 输出）
	}
}

// --- 便捷方法 ---

// Init 初始化 BASS 引擎
func (ab *AudioBridge) Init(device, freq int) error {
	resp, err := ab.Call(MethodInit, &InitParams{Device: device, Freq: freq})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("BASS 初始化失败: %s", resp.Error)
	}
	return nil
}

// Load 加载音频文件到指定通道
func (ab *AudioBridge) Load(channel int, filePath string, isEncrypt bool, volume float64, fadeIn int) error {
	resp, err := ab.Call(MethodLoad, &LoadParams{
		Channel:   channel,
		FilePath:  filePath,
		IsEncrypt: isEncrypt,
		Volume:    volume,
		FadeIn:    fadeIn,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("加载失败: %s", resp.Error)
	}
	return nil
}

// Play 播放指定通道
func (ab *AudioBridge) Play(channel int, restart bool) error {
	resp, err := ab.Call(MethodPlay, &PlayParams{Channel: channel, Restart: restart})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("播放失败: %s", resp.Error)
	}
	return nil
}

// Stop 停止指定通道
func (ab *AudioBridge) Stop(channel int, fadeOutMs int) error {
	resp, err := ab.Call(MethodStop, &StopParams{Channel: channel, FadeOut: fadeOutMs})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("停止失败: %s", resp.Error)
	}
	return nil
}

// SetVolume 设置通道音量
func (ab *AudioBridge) SetVolume(channel int, volume float64, fadeMs int) error {
	resp, err := ab.Call(MethodSetVolume, &VolumeParams{Channel: channel, Volume: volume, FadeMs: fadeMs})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("设置音量失败: %s", resp.Error)
	}
	return nil
}

// GetPosition 获取播放位置
func (ab *AudioBridge) GetPosition(channel int) (*PositionResult, error) {
	resp, err := ab.Call(MethodPosition, &ChannelParams{Channel: channel})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("获取位置失败: %s", resp.Error)
	}
	var result PositionResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析位置数据失败: %w", err)
	}
	return &result, nil
}

// Pause 暂停指定通道（支持淡出暂停）
func (ab *AudioBridge) Pause(channel int, fadeMs int) error {
	resp, err := ab.Call(MethodPause, &PauseParams{Channel: channel, FadeMs: fadeMs})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("暂停失败: %s", resp.Error)
	}
	return nil
}

// Resume 恢复指定通道
func (ab *AudioBridge) Resume(channel int) error {
	resp, err := ab.Call(MethodResume, &ChannelParams{Channel: channel})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("恢复失败: %s", resp.Error)
	}
	return nil
}

// SetEQ 设置均衡器
func (ab *AudioBridge) SetEQ(channel int, preset string, bands []EQBand) error {
	resp, err := ab.Call(MethodSetEQ, &EQParams{Channel: channel, Preset: preset, Bands: bands})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("设置均衡器失败: %s", resp.Error)
	}
	return nil
}

// FadePause 淡出暂停：降到目标音量后暂停流（不释放通道）。
// 用于硬定时切换时保留当前流，定时节目播完后可恢复。
func (ab *AudioBridge) FadePause(channel int, targetVol float64, fadeMs int) error {
	if err := ab.SetVolume(channel, targetVol, fadeMs); err != nil {
		return fmt.Errorf("FadePause 降音量失败: %w", err)
	}
	return ab.Pause(channel, 0)
}

// Seek 跳转到指定播放位置
func (ab *AudioBridge) Seek(channel int, positionMs int) error {
	resp, err := ab.Call(MethodSeek, &SeekParams{Channel: channel, PositionMs: positionMs})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("Seek 失败: %s", resp.Error)
	}
	return nil
}

// SwitchSignal 切换信号源（切换器指令）
func (ab *AudioBridge) SwitchSignal(signalID int, signalName string) error {
	resp, err := ab.Call(MethodSwitchSignal, &SwitchSignalParams{SignalID: signalID, SignalName: signalName})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("信号切换失败: %s", resp.Error)
	}
	return nil
}

// Ping 心跳检测
func (ab *AudioBridge) Ping() error {
	resp, err := ab.Call(MethodPing, nil)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("ping 失败: %s", resp.Error)
	}
	return nil
}

// Shutdown 优雅关闭子进程
func (ab *AudioBridge) Shutdown() error {
	resp, err := ab.Call(MethodShutdown, nil)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("关闭失败: %s", resp.Error)
	}
	return nil
}
