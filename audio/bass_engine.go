package audio

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/rs/zerolog/log"
)

// bassCommand 派发到 BASS 线程的命令
type bassCommand struct {
	fn       func() interface{}
	resultCh chan interface{}
}

// BassEngine BASS 引擎，所有 BASS 操作通过双通道派发到专用 OS 线程。
// ctrlCh: 低延迟控制命令（Play/Stop/Pause/SetVolume），优先处理
// ioCh:   可能耗时的 IO 操作（StreamCreateFile/StreamFree）
type BassEngine struct {
	ctrlCh chan bassCommand
	ioCh   chan bassCommand

	server    *IPCServer // 用于推送事件
	channels  *VirtualChannelManager
	cancelCtx context.CancelFunc

	mu       sync.Mutex
	inited   bool
	deviceID int
}

// NewBassEngine 创建 BASS 引擎
func NewBassEngine() *BassEngine {
	return &BassEngine{
		ctrlCh: make(chan bassCommand, 64),
		ioCh:   make(chan bassCommand, 32),
	}
}

// SetServer 设置 IPC 服务端引用，用于推送事件
func (be *BassEngine) SetServer(server *IPCServer) {
	be.server = server
}

// Run 启动 BASS 专用 goroutine，阻塞直到 ctx 取消。
// 必须在调用任何 BASS API 之前调用。
func (be *BassEngine) Run(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 启动播完回调监听
	go be.watchCallbacks(ctx)

	log.Info().Msg("BASS 引擎 goroutine 启动，已锁定 OS 线程")

	for {
		// 双通道优先级路由：控制命令优先
		select {
		case cmd := <-be.ctrlCh:
			result := cmd.fn()
			cmd.resultCh <- result
		default:
			select {
			case cmd := <-be.ctrlCh:
				result := cmd.fn()
				cmd.resultCh <- result
			case cmd := <-be.ioCh:
				result := cmd.fn()
				cmd.resultCh <- result
			case <-ctx.Done():
				log.Info().Msg("BASS 引擎 goroutine 退出")
				be.freeAll()
				return
			}
		}
	}
}

// execCtrl 在 BASS 线程执行控制命令（低延迟）
func (be *BassEngine) execCtrl(fn func() interface{}) interface{} {
	ch := make(chan interface{}, 1)
	be.ctrlCh <- bassCommand{fn: fn, resultCh: ch}
	return <-ch
}

// execIO 在 BASS 线程执行 IO 命令（可能耗时）
func (be *BassEngine) execIO(fn func() interface{}) interface{} {
	ch := make(chan interface{}, 1)
	be.ioCh <- bassCommand{fn: fn, resultCh: ch}
	return <-ch
}

// ==================== IPC 接口实现 ====================

// Init 初始化 BASS 引擎
func (be *BassEngine) Init(device int, freq int) error {
	result := be.execIO(func() interface{} {
		be.mu.Lock()
		defer be.mu.Unlock()

		if be.inited {
			// 已初始化，先释放再重新初始化
			BassFree()
		}
		if err := BassInit(device, freq); err != nil {
			return err
		}
		be.inited = true
		be.deviceID = device
		be.channels = NewVirtualChannelManager()
		log.Info().Int("device", device).Int("freq", freq).Msg("BASS 引擎初始化成功")
		return nil
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// Load 加载音频文件到指定通道
func (be *BassEngine) Load(channel int, filePath string, isEncrypt bool, volume float64) error {
	result := be.execIO(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}

		// 释放旧流
		if ch.StreamHandle != 0 {
			if ch.SyncHandle != 0 {
				BassChannelRemoveSync(ch.StreamHandle, ch.SyncHandle)
				ch.SyncHandle = 0
			}
			BassStreamFree(ch.StreamHandle)
			if ch.FileUser != nil {
				ch.FileUser.Dispose()
				ch.FileUser = nil
			}
			ch.StreamHandle = 0
		}

		var handle uint32
		var fileUser *BassFileUser
		var err error

		if isEncrypt {
			handle, fileUser, err = be.loadEncryptedFile(filePath)
		} else {
			handle, err = BassStreamCreateFile(filePath, StreamPrescan)
		}
		if err != nil {
			return fmt.Errorf("加载文件失败: %s, %v", filePath, err)
		}

		if volume >= 0 {
			BassChannelSetAttribute(handle, AttribVol, float32(volume))
		}

		syncHandle, syncErr := BassChannelSetSyncEnd(handle, ch.Index)
		if syncErr != nil {
			log.Warn().Err(syncErr).Int("channel", channel).Msg("设置播完回调失败")
		}

		ch.StreamHandle = handle
		ch.SyncHandle = syncHandle
		ch.FileUser = fileUser
		ch.FilePath = filePath

		log.Debug().Int("channel", channel).Str("path", filePath).Uint32("handle", handle).Msg("加载完成")
		return nil
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// Play 播放指定通道
func (be *BassEngine) Play(channel int, restart bool) error {
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return fmt.Errorf("通道 %d 未加载文件", channel)
		}
		return BassChannelPlay(ch.StreamHandle, restart)
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// Stop 停止指定通道
func (be *BassEngine) Stop(channel int, fadeOut int) error {
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return nil // 未播放，不算错误
		}

		// TODO: fadeOut 淡出效果（通过定时器逐步降低音量）
		// 当前阶段直接停止
		return BassChannelStop(ch.StreamHandle)
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// Pause 暂停指定通道
func (be *BassEngine) Pause(channel int) error {
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return nil
		}
		return BassChannelPause(ch.StreamHandle)
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// Resume 恢复指定通道
func (be *BassEngine) Resume(channel int) error {
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return nil
		}
		return BassChannelPlay(ch.StreamHandle, false)
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// SetVolume 设置通道音量
func (be *BassEngine) SetVolume(channel int, volume float64) error {
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return nil
		}
		return BassChannelSetAttribute(ch.StreamHandle, AttribVol, float32(volume))
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// GetPosition 获取通道播放位置（毫秒）和总时长（毫秒）
func (be *BassEngine) GetPosition(channel int) (int, int, error) {
	type posResult struct {
		pos int
		dur int
		err error
	}
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return posResult{err: fmt.Errorf("未知通道索引: %d", channel)}
		}
		if ch.StreamHandle == 0 {
			return posResult{}
		}

		posBytes := BassChannelGetPosition(ch.StreamHandle)
		lenBytes := BassChannelGetLength(ch.StreamHandle)

		posSec := BassChannelBytes2Seconds(ch.StreamHandle, posBytes)
		durSec := BassChannelBytes2Seconds(ch.StreamHandle, lenBytes)

		return posResult{
			pos: int(posSec * 1000),
			dur: int(durSec * 1000),
		}
	})
	r := result.(posResult)
	return r.pos, r.dur, r.err
}

// GetLevel 获取通道电平
func (be *BassEngine) GetLevel(channel int) (float64, float64, error) {
	type levelResult struct {
		left, right float64
		err         error
	}
	result := be.execCtrl(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return levelResult{err: fmt.Errorf("未知通道索引: %d", channel)}
		}
		if ch.StreamHandle == 0 {
			return levelResult{}
		}
		left, right := BassChannelGetLevel(ch.StreamHandle)
		return levelResult{left: float64(left), right: float64(right)}
	})
	r := result.(levelResult)
	return r.left, r.right, r.err
}

// FreeChannel 释放指定通道资源
func (be *BassEngine) FreeChannel(channel int) {
	be.execIO(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return nil
		}
		be.freeChannelImpl(ch)
		return nil
	})
}

// FreeAll 释放所有通道资源
func (be *BassEngine) FreeAll() {
	be.execIO(func() interface{} {
		be.freeAll()
		return nil
	})
}

// Shutdown 关闭引擎（通过 execIO 派发到 BASS 线程执行）
func (be *BassEngine) Shutdown() {
	be.execIO(func() interface{} {
		be.mu.Lock()
		defer be.mu.Unlock()

		if !be.inited {
			return nil
		}

		be.freeAll()
		BassFree()
		be.inited = false
		log.Info().Msg("BASS 引擎已关闭")
		return nil
	})
}

// SetEQ 设置通道均衡器
func (be *BassEngine) SetEQ(channel int, bands []EQBandParam) error {
	result := be.execIO(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return fmt.Errorf("通道 %d 未加载文件", channel)
		}
		for _, band := range bands {
			fx, err := BassChannelSetFXParamEQ(ch.StreamHandle)
			if err != nil {
				return err
			}
			if err := BassFXSetParamEQ(fx, band.Center, band.Bandwidth, band.Gain); err != nil {
				return err
			}
		}
		return nil
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// SetDevice 设置通道的输出设备
func (be *BassEngine) SetDevice(channel int, deviceIndex int) error {
	result := be.execIO(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return fmt.Errorf("未知通道索引: %d", channel)
		}
		if ch.StreamHandle == 0 {
			return nil
		}
		return BassChannelSetDevice(ch.StreamHandle, deviceIndex)
	})
	if result == nil {
		return nil
	}
	return result.(error)
}

// RemoveSync 移除通道同步回调
func (be *BassEngine) RemoveSync(channel int) {
	be.execIO(func() interface{} {
		ch := be.channels.GetByIndex(channel)
		if ch == nil {
			return nil
		}
		if ch.SyncHandle != 0 && ch.StreamHandle != 0 {
			BassChannelRemoveSync(ch.StreamHandle, ch.SyncHandle)
			ch.SyncHandle = 0
		}
		return nil
	})
}

// GetDeviceInfo 获取设备信息列表
func (be *BassEngine) GetDeviceInfo() ([]BassDeviceInfo, error) {
	result := be.execIO(func() interface{} {
		return BassEnumDevices()
	})
	devices := result.([]BassDeviceInfo)
	return devices, nil
}

// EQBandParam 均衡器频段参数
type EQBandParam struct {
	Center    float32
	Bandwidth float32
	Gain      float32
}

// ==================== 内部方法 ====================

// freeChannelImpl 释放单通道资源（必须在 BASS 线程调用）
func (be *BassEngine) freeChannelImpl(ch *VirtualChannel) {
	if ch.SyncHandle != 0 {
		BassChannelRemoveSync(ch.StreamHandle, ch.SyncHandle)
		ch.SyncHandle = 0
	}
	if ch.StreamHandle != 0 {
		BassStreamFree(ch.StreamHandle)
		ch.StreamHandle = 0
	}
	if ch.FileUser != nil {
		ch.FileUser.Dispose()
		ch.FileUser = nil
	}
	ch.FilePath = ""
}

// freeAll 释放所有通道（必须在 BASS 线程调用）
func (be *BassEngine) freeAll() {
	if be.channels == nil {
		return
	}
	for i := 0; i < int(ChanCount); i++ {
		ch := be.channels.GetByIndex(i)
		if ch != nil {
			be.freeChannelImpl(ch)
		}
	}
}

// loadEncryptedFile 加载加密文件
func (be *BassEngine) loadEncryptedFile(filePath string) (uint32, *BassFileUser, error) {
	// 打开文件
	f, err := os.Open(filePath)
	if err != nil {
		return 0, nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return 0, nil, fmt.Errorf("获取文件信息失败: %v", err)
	}

	item := &EncryptedFileItem{
		File:   f,
		Size:   stat.Size(),
		XorKey: 0x56, // SLA 加密密钥
	}

	handle, fileUser, err := BassStreamCreateFileUser(item, StreamPrescan)
	if err != nil {
		f.Close()
		return 0, nil, err
	}

	return handle, fileUser, nil
}

// watchCallbacks 监听播完回调，推送 IPC 事件
func (be *BassEngine) watchCallbacks(ctx context.Context) {
	for {
		select {
		case evt := <-CallbackCh():
			if evt.Type == PlayFinished && be.server != nil {
				ch := be.channels.GetByIndex(evt.ChannelID)
				channelName := ""
				if ch != nil {
					channelName = ch.Name.String()
				}
				be.server.PushEvent("play_finished", map[string]interface{}{
					"channel": channelName,
				})
				log.Info().Str("channel", channelName).Msg("播放完成")
			}
		case <-ctx.Done():
			return
		}
	}
}
