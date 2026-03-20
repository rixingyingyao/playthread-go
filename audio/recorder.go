package audio

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/rs/zerolog/log"
)

const (
	recSampleRate       = 48000
	recChannels         = 2
	recBitrate          = 256
	recFileDurationSec  = 3600 // 文件滚动间隔（秒）
	recProgressInterval = 200  // 进度推送间隔（ms）
)

// RecordLevelData 录音电平数据
type RecordLevelData struct {
	PeakL float32
	PeakR float32
	RmsL  float32
	RmsR  float32
	DbL   float32
	DbR   float32
}

// RecordProgress 录音进度
type RecordProgress struct {
	DurationSec float64         `json:"duration"`
	Status      int             `json:"status"` // 0=未录制 1=录制中 2=暂停
	Level       RecordLevelData `json:"level"`
}

// Recorder 音频录制器（对齐 C# BassRecord）。
// 从 BASS 录音设备采集 PCM 数据，通过 LAME 编码为 MP3。
// LAME 库在运行时动态加载（libmp3lame.dll / libmp3lame.so）。
type Recorder struct {
	mu        sync.Mutex
	recording bool
	paused    bool
	device    int

	// LAME 编码器
	encoder *LameEncoder

	// 文件管理
	mp3File      *os.File
	baseName     string // 不含扩展名的基本路径
	fileIndex    int
	fileSec      float64 // 当前文件已录制秒数
	totalSamples float64 // 总录制秒数

	// BASS 录音句柄
	recordHandle uint32

	// 电平数据（原子更新，回调写 / 主线程读）
	level atomic.Value // *RecordLevelData

	// 进度推送
	progressCh chan RecordProgress
	lastPush   int64 // 上次推送时间（ms）
}

// globalRecorder 全局录制器指针，供 BASS 回调访问
var globalRecorder atomic.Pointer[Recorder]

// NewRecorder 创建录制器
func NewRecorder() *Recorder {
	r := &Recorder{
		device:     -1,
		progressCh: make(chan RecordProgress, 32),
	}
	r.level.Store(&RecordLevelData{})
	return r
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
		r.paused = false
		log.Info().Msg("录音恢复")
		return nil
	}

	// 确保目录存在
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("创建录音目录失败: %v", err)
	}

	// 初始化 LAME 编码器
	enc, err := NewLameEncoder(recSampleRate, recChannels, recBitrate)
	if err != nil {
		log.Warn().Err(err).Msg("LAME 编码器不可用，无法录音")
		return fmt.Errorf("LAME 编码器初始化失败: %v", err)
	}

	// 创建 MP3 文件
	f, err := os.Create(filename)
	if err != nil {
		enc.Close()
		return fmt.Errorf("创建录音文件失败: %v", err)
	}

	r.encoder = enc
	r.mp3File = f
	r.baseName = filepath.Join(
		filepath.Dir(filename),
		fileNameWithoutExt(filepath.Base(filename)),
	)
	r.fileIndex = 1
	r.fileSec = 0
	r.totalSamples = 0
	r.recording = true
	r.paused = false
	r.lastPush = time.Now().UnixMilli()

	// 注册全局回调
	globalRecorder.Store(r)

	// 启动 BASS 录音（RecordPause 标志：先暂停状态启动，再 Play 开始）
	handle, err := BassRecordStart(recSampleRate, recChannels, 0)
	if err != nil {
		r.cleanup()
		return fmt.Errorf("BASS 录音启动失败: %v", err)
	}
	r.recordHandle = handle

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

// Stop 停止录音并释放资源
func (r *Recorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return nil
	}

	r.recording = false
	r.paused = false

	// 停止 BASS 录音
	if r.recordHandle != 0 {
		BassChannelStop(r.recordHandle)
		r.recordHandle = 0
	}

	// 刷出 LAME 残余数据
	if r.encoder != nil {
		if data, err := r.encoder.Flush(); err == nil && len(data) > 0 {
			if r.mp3File != nil {
				r.mp3File.Write(data)
			}
		}
		r.encoder.Close()
		r.encoder = nil
	}

	if r.mp3File != nil {
		r.mp3File.Close()
		r.mp3File = nil
	}

	BassRecordFree()
	globalRecorder.Store(nil)

	log.Info().Float64("total_sec", r.totalSamples).Msg("录音停止")
	return nil
}

// IsRecording 是否正在录音
func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording && !r.paused
}

// GetStatus 获取录制状态 0=未录制 1=录制中 2=暂停中
func (r *Recorder) GetStatus() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.recording {
		return 0
	}
	if r.paused {
		return 2
	}
	return 1
}

// GetProgress 获取录音进度
func (r *Recorder) GetProgress() RecordProgress {
	r.mu.Lock()
	status := 0
	dur := r.totalSamples
	if r.recording {
		if r.paused {
			status = 2
		} else {
			status = 1
		}
	}
	r.mu.Unlock()

	lvl := r.level.Load().(*RecordLevelData)
	return RecordProgress{
		DurationSec: dur,
		Status:      status,
		Level:       *lvl,
	}
}

// ProgressCh 返回进度通道（只读），录音回调每 200ms 推送一次
func (r *Recorder) ProgressCh() <-chan RecordProgress {
	return r.progressCh
}

// Run 录音监控循环（占位，实际数据处理在 BASS 回调中完成）
func (r *Recorder) Run(ctx context.Context) {
	<-ctx.Done()
	r.Stop()
}

// onRecordData 由 BASS RECORDPROC 回调调用，处理 PCM 数据
// 在 C 线程上运行，必须快速返回
func onRecordData(buffer unsafe.Pointer, length int) {
	r := globalRecorder.Load()
	if r == nil || length <= 0 {
		return
	}

	// 推送进度（每 200ms）
	now := time.Now().UnixMilli()
	if now-atomic.LoadInt64(&r.lastPush) > recProgressInterval {
		atomic.StoreInt64(&r.lastPush, now)
		select {
		case r.progressCh <- r.GetProgress():
		default:
		}
	}

	r.mu.Lock()
	if !r.recording || r.paused || r.encoder == nil || r.mp3File == nil {
		r.mu.Unlock()
		return
	}

	// PCM 电平分析
	r.processLevelMeter(buffer, length)

	// LAME 编码
	mp3Data, err := r.encoder.Encode(buffer, length)
	if err != nil {
		r.mu.Unlock()
		return
	}
	if len(mp3Data) > 0 {
		r.mp3File.Write(mp3Data)
	}

	// 累计时长
	bytesPerSample := 2 * recChannels // 16bit * 2ch
	durationSec := float64(length) / float64(bytesPerSample) / float64(recSampleRate)
	r.totalSamples += durationSec
	r.fileSec += durationSec

	// 文件滚动
	if r.fileSec >= recFileDurationSec {
		r.switchFile()
	}

	r.mu.Unlock()
}

// processLevelMeter 从 PCM 缓冲区计算峰值/RMS 电平（对齐 C# AudioLevelMeter.ProcessBuffer）
func (r *Recorder) processLevelMeter(buffer unsafe.Pointer, length int) {
	samples := length / 2 // 16bit = 2 bytes per sample
	frames := samples / 2 // stereo: 2 samples per frame

	if frames <= 0 {
		return
	}

	pcm := unsafe.Slice((*int16)(buffer), samples)

	var peakL, peakR int
	var sumL, sumR float64

	for i := 0; i < frames; i++ {
		l := int(pcm[i*2])
		rv := int(pcm[i*2+1])

		absL := l
		if absL < 0 {
			absL = -absL
		}
		if l == -32768 {
			absL = 32768
		}
		absR := rv
		if absR < 0 {
			absR = -absR
		}
		if rv == -32768 {
			absR = 32768
		}

		if absL > peakL {
			peakL = absL
		}
		if absR > peakR {
			peakR = absR
		}

		sumL += float64(l) * float64(l)
		sumR += float64(rv) * float64(rv)
	}

	const inv32768 = 1.0 / 32768.0
	pL := float32(peakL) * inv32768
	pR := float32(peakR) * inv32768
	rL := float32(math.Sqrt(sumL/float64(frames))) * inv32768
	rR := float32(math.Sqrt(sumR/float64(frames))) * inv32768

	r.level.Store(&RecordLevelData{
		PeakL: float32(peakL >> 8),
		PeakR: float32(peakR >> 8),
		RmsL:  rL,
		RmsR:  rR,
		DbL:   toDb(pL),
		DbR:   toDb(pR),
	})
}

func toDb(v float32) float32 {
	if v < 0.000001 {
		return -90.0
	}
	return 20.0 * float32(math.Log10(float64(v)))
}

// switchFile 文件滚动（对齐 C# SwitchRecordFile / CreateNewRecordFile）
// 每个 MP3 文件都是独立完整流：关闭旧编码器 → 关闭旧文件 → 创建新编码器 → 创建新文件
func (r *Recorder) switchFile() {
	// 刷出并关闭当前编码器（确保 MP3 流完整收尾）
	if r.encoder != nil {
		if data, err := r.encoder.Flush(); err == nil && len(data) > 0 {
			if r.mp3File != nil {
				r.mp3File.Write(data)
			}
		}
		r.encoder.Close()
		r.encoder = nil
	}

	// 关闭旧文件
	if r.mp3File != nil {
		r.mp3File.Sync()
		r.mp3File.Close()
		r.mp3File = nil
	}

	// 创建新 LAME 编码器（新文件 = 新独立 MP3 流）
	enc, err := NewLameEncoder(recSampleRate, recChannels, recBitrate)
	if err != nil {
		log.Error().Err(err).Msg("录音文件滚动：重建 LAME 编码器失败，停止录音")
		r.recording = false
		globalRecorder.Store(nil)
		return
	}
	r.encoder = enc

	// 创建新文件
	newName := fmt.Sprintf("%s_%d.mp3", r.baseName, r.fileIndex)
	f, err := os.Create(newName)
	if err != nil {
		log.Error().Err(err).Str("file", newName).Msg("录音文件滚动失败")
		r.encoder.Close()
		r.encoder = nil
		r.recording = false
		globalRecorder.Store(nil)
		return
	}

	r.mp3File = f
	r.fileIndex++
	r.fileSec = 0

	log.Info().Str("file", newName).Msg("录音文件滚动")
}

// cleanup 清理异常状态
func (r *Recorder) cleanup() {
	if r.encoder != nil {
		r.encoder.Close()
		r.encoder = nil
	}
	if r.mp3File != nil {
		r.mp3File.Close()
		r.mp3File = nil
	}
	r.recording = false
	globalRecorder.Store(nil)
}

func fileNameWithoutExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)]
}
