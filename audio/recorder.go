package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
)

// Recorder 音频录制器。
// 从 BASS 录音设备采集 PCM 数据，当前阶段仅保存 PCM 原始数据。
// LAME MP3 编码在后续阶段通过 cgo 绑定 libmp3lame 实现。
type Recorder struct {
	mu        sync.Mutex
	recording bool
	paused    bool
	device    int
	file      *os.File
	filePath  string
}

// NewRecorder 创建录制器
func NewRecorder() *Recorder {
	return &Recorder{
		device: -1,
	}
}

// InitDevice 初始化录音设备
func (r *Recorder) InitDevice(device int) error {
	if err := BassRecordInit(device); err != nil {
		return fmt.Errorf("录音设备初始化失败: %v", err)
	}
	r.device = device
	log.Info().Int("device", device).Msg("录音设备初始化成功")
	return nil
}

// Start 开始录音
func (r *Recorder) Start(filename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording && !r.paused {
		return fmt.Errorf("当前正在录音中")
	}

	if r.paused {
		// 恢复录音
		r.paused = false
		log.Info().Msg("录音恢复")
		return nil
	}

	// 确保目录存在
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("创建录音目录失败: %v", err)
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("创建录音文件失败: %v", err)
	}

	r.file = f
	r.filePath = filename
	r.recording = true
	r.paused = false

	log.Info().Str("file", filename).Msg("录音开始")
	return nil
}

// Pause 暂停录音
func (r *Recorder) Pause() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return fmt.Errorf("尚未开始录音")
	}
	r.paused = true
	log.Info().Msg("录音暂停")
	return nil
}

// Stop 停止录音
func (r *Recorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return nil
	}

	r.recording = false
	r.paused = false

	if r.file != nil {
		r.file.Close()
		r.file = nil
	}

	log.Info().Str("file", r.filePath).Msg("录音停止")
	return nil
}

// IsRecording 是否正在录音
func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording && !r.paused
}

// Run 录音数据采集循环（占位）
// 实际 BASS 录音回调将在 cgo 层实现，此处预留上层协调逻辑
func (r *Recorder) Run(ctx context.Context) {
	<-ctx.Done()
	r.Stop()
}
