package audio

/*
#cgo windows LDFLAGS: -L${SRCDIR}/libs/windows -lbass
#cgo linux LDFLAGS: -L${SRCDIR}/libs/linux -lbass -Wl,-rpath,${SRCDIR}/libs/linux
#include "libs/bass.h"
#include <stdlib.h>

// 播完回调——C 端桩函数，转发到 Go export
extern void goSyncEndCallback(HSYNC handle, DWORD channel, DWORD data, void* user);

// 加密文件流回调
extern void  goFileCloseProc(void* user);
extern QWORD goFileLenProc(void* user);
extern DWORD goFileReadProc(void* buffer, DWORD length, void* user);
extern BOOL  goFileSeekProc(QWORD offset, void* user);
*/
import "C"
import (
	"fmt"
	"os"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// ==================== 初始化与设备 ====================

// BassInit 初始化 BASS 引擎。必须在 LockOSThread 的 goroutine 中调用。
func BassInit(device int, freq int) error {
	if C.BASS_Init(C.int(device), C.DWORD(freq), 0, nil, nil) == 0 {
		return fmt.Errorf("BASS_Init 失败: device=%d, freq=%d, errCode=%d",
			device, freq, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassFree 释放 BASS 引擎
func BassFree() {
	C.BASS_Free()
}

// BassSetDevice 设置当前设备
func BassSetDevice(device int) error {
	if C.BASS_SetDevice(C.DWORD(device)) == 0 {
		return fmt.Errorf("BASS_SetDevice 失败: device=%d, errCode=%d",
			device, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassGetDevice 获取当前设备
func BassGetDevice() int {
	return int(C.BASS_GetDevice())
}

// BassDeviceInfo 设备信息
type BassDeviceInfo struct {
	Name   string
	Driver string
	Flags  uint32
}

// BassGetDeviceInfo 获取设备信息
func BassGetDeviceInfo(device int) (*BassDeviceInfo, error) {
	var info C.BASS_DEVICEINFO
	if C.BASS_GetDeviceInfo(C.DWORD(device), &info) == 0 {
		return nil, fmt.Errorf("BASS_GetDeviceInfo 失败: device=%d, errCode=%d",
			device, C.BASS_ErrorGetCode())
	}
	return &BassDeviceInfo{
		Name:   C.GoString(info.name),
		Driver: C.GoString(info.driver),
		Flags:  uint32(info.flags),
	}, nil
}

// BassEnumDevices 枚举所有可用播放设备
func BassEnumDevices() []BassDeviceInfo {
	var devices []BassDeviceInfo
	for i := 0; ; i++ {
		info, err := BassGetDeviceInfo(i)
		if err != nil {
			break
		}
		devices = append(devices, *info)
	}
	return devices
}

// BassErrorGetCode 获取最近一次错误码
func BassErrorGetCode() int {
	return int(C.BASS_ErrorGetCode())
}

// ==================== 流操作 ====================

// BassStreamCreateFile 从文件创建音频流
func BassStreamCreateFile(path string, flags uint32) (uint32, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	handle := C.BASS_StreamCreateFile(0, unsafe.Pointer(cpath), 0, 0, C.DWORD(flags))
	if handle == 0 {
		return 0, fmt.Errorf("BASS_StreamCreateFile 失败: path=%s, errCode=%d",
			path, C.BASS_ErrorGetCode())
	}
	return uint32(handle), nil
}

// BassStreamFree 释放音频流
func BassStreamFree(handle uint32) {
	C.BASS_StreamFree(C.HSTREAM(handle))
}

// ==================== 通道控制 ====================

// BassChannelPlay 播放通道
func BassChannelPlay(handle uint32, restart bool) error {
	var r C.BOOL
	if restart {
		r = C.TRUE
	}
	if C.BASS_ChannelPlay(C.DWORD(handle), r) == 0 {
		return fmt.Errorf("BASS_ChannelPlay 失败: handle=%d, errCode=%d",
			handle, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassChannelStop 停止通道
func BassChannelStop(handle uint32) error {
	if C.BASS_ChannelStop(C.DWORD(handle)) == 0 {
		return fmt.Errorf("BASS_ChannelStop 失败: handle=%d, errCode=%d",
			handle, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassChannelPause 暂停通道
func BassChannelPause(handle uint32) error {
	if C.BASS_ChannelPause(C.DWORD(handle)) == 0 {
		return fmt.Errorf("BASS_ChannelPause 失败: handle=%d, errCode=%d",
			handle, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassChannelIsActive 获取通道状态
func BassChannelIsActive(handle uint32) int {
	return int(C.BASS_ChannelIsActive(C.DWORD(handle)))
}

// BassChannelSetDevice 将通道绑定到指定设备
func BassChannelSetDevice(handle uint32, device int) error {
	if C.BASS_ChannelSetDevice(C.DWORD(handle), C.DWORD(device)) == 0 {
		return fmt.Errorf("BASS_ChannelSetDevice 失败: handle=%d, device=%d, errCode=%d",
			handle, device, C.BASS_ErrorGetCode())
	}
	return nil
}

// ==================== 位置 ====================

// BassChannelGetPosition 获取当前播放位置（字节）
func BassChannelGetPosition(handle uint32) int64 {
	return int64(C.BASS_ChannelGetPosition(C.DWORD(handle), C.BASS_POS_BYTE))
}

// BassChannelSetPosition 设置播放位置（字节）
func BassChannelSetPosition(handle uint32, pos int64) error {
	if C.BASS_ChannelSetPosition(C.DWORD(handle), C.QWORD(pos), C.BASS_POS_BYTE) == 0 {
		return fmt.Errorf("BASS_ChannelSetPosition 失败: handle=%d, errCode=%d",
			handle, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassChannelGetLength 获取总长度（字节）
func BassChannelGetLength(handle uint32) int64 {
	return int64(C.BASS_ChannelGetLength(C.DWORD(handle), C.BASS_POS_BYTE))
}

// BassChannelBytes2Seconds 字节位置转秒数
func BassChannelBytes2Seconds(handle uint32, pos int64) float64 {
	return float64(C.BASS_ChannelBytes2Seconds(C.DWORD(handle), C.QWORD(pos)))
}

// BassChannelSeconds2Bytes 秒数转字节位置
func BassChannelSeconds2Bytes(handle uint32, secs float64) int64 {
	return int64(C.BASS_ChannelSeconds2Bytes(C.DWORD(handle), C.double(secs)))
}

// ==================== 属性 ====================

// BassChannelSetAttribute 设置通道属性
func BassChannelSetAttribute(handle uint32, attrib uint32, value float32) error {
	if C.BASS_ChannelSetAttribute(C.DWORD(handle), C.DWORD(attrib), C.float(value)) == 0 {
		return fmt.Errorf("BASS_ChannelSetAttribute 失败: handle=%d, attrib=%d, errCode=%d",
			handle, attrib, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassChannelGetAttribute 获取通道属性
func BassChannelGetAttribute(handle uint32, attrib uint32) (float32, error) {
	var value C.float
	if C.BASS_ChannelGetAttribute(C.DWORD(handle), C.DWORD(attrib), &value) == 0 {
		return 0, fmt.Errorf("BASS_ChannelGetAttribute 失败: handle=%d, attrib=%d, errCode=%d",
			handle, attrib, C.BASS_ErrorGetCode())
	}
	return float32(value), nil
}

// BASS 属性常量
const (
	AttribFreq    = C.BASS_ATTRIB_FREQ
	AttribVol     = C.BASS_ATTRIB_VOL
	AttribPan     = C.BASS_ATTRIB_PAN
	AttribTempo   = C.BASS_ATTRIB_TEMPO
)

// BASS 通道状态常量
const (
	ActiveStopped = C.BASS_ACTIVE_STOPPED
	ActivePlaying = C.BASS_ACTIVE_PLAYING
	ActiveStalled = C.BASS_ACTIVE_STALLED
	ActivePaused  = C.BASS_ACTIVE_PAUSED
)

// BASS 流创建标志
const (
	StreamPrescan  = C.BASS_STREAM_PRESCAN
	StreamDecode   = C.BASS_STREAM_DECODE
	StreamAutoFree = C.BASS_STREAM_AUTOFREE
	SampleFloat    = C.BASS_SAMPLE_FLOAT
	Unicode        = C.BASS_UNICODE
)

// ==================== 同步/回调 ====================

// callbackCh 播完回调通知通道，不 close，靠 GC 回收
// 容量 64 防止 C 回调线程阻塞
var callbackCh = make(chan CallbackEvent, 64)

// CallbackEvent 回调事件
type CallbackEvent struct {
	Type      CallbackType
	ChannelID int
	Handle    uint32
}

// CallbackType 回调类型
type CallbackType int

const (
	PlayFinished CallbackType = iota
)

// CallbackCh 返回回调事件通道（只读）
func CallbackCh() <-chan CallbackEvent {
	return callbackCh
}

//export goSyncEndCallback
func goSyncEndCallback(handle C.HSYNC, channel C.DWORD, data C.DWORD, user unsafe.Pointer) {
	// ★ cgo 回调绝对第一行：recover
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "BASS 播完回调 panic: %v\n", r)
		}
	}()

	id := int(uintptr(user))
	select {
	case callbackCh <- CallbackEvent{Type: PlayFinished, ChannelID: id, Handle: uint32(channel)}:
	default:
		// channel 满，丢弃，不阻塞 C 线程
	}
}

// BassChannelSetSyncEnd 设置播完同步回调
func BassChannelSetSyncEnd(handle uint32, channelID int) (uint32, error) {
	syncHandle := C.BASS_ChannelSetSync(
		C.DWORD(handle),
		C.BASS_SYNC_END|C.BASS_SYNC_MIXTIME,
		0,
		(*C.SYNCPROC)(C.goSyncEndCallback),
		unsafe.Pointer(uintptr(channelID)),
	)
	if syncHandle == 0 {
		return 0, fmt.Errorf("BASS_ChannelSetSync 失败: handle=%d, errCode=%d",
			handle, C.BASS_ErrorGetCode())
	}
	return uint32(syncHandle), nil
}

// BassChannelRemoveSync 移除同步回调
func BassChannelRemoveSync(handle uint32, syncHandle uint32) {
	C.BASS_ChannelRemoveSync(C.DWORD(handle), C.HSYNC(syncHandle))
}

// ==================== 电平 ====================

// BassChannelGetLevel 获取通道电平（左/右）
func BassChannelGetLevel(handle uint32) (float32, float32) {
	level := C.BASS_ChannelGetLevel(C.DWORD(handle))
	left := float32(level&0xFFFF) / 32768.0
	right := float32((level>>16)&0xFFFF) / 32768.0
	return left, right
}

// ==================== 效果/EQ ====================

// BassChannelSetFXParamEQ 添加参数均衡器效果
func BassChannelSetFXParamEQ(handle uint32) (uint32, error) {
	fx := C.BASS_ChannelSetFX(C.DWORD(handle), C.BASS_FX_DX8_PARAMEQ, 0)
	if fx == 0 {
		return 0, fmt.Errorf("BASS_ChannelSetFX 失败: handle=%d, errCode=%d",
			handle, C.BASS_ErrorGetCode())
	}
	return uint32(fx), nil
}

// BassChannelRemoveFX 移除效果
func BassChannelRemoveFX(handle uint32, fx uint32) {
	C.BASS_ChannelRemoveFX(C.DWORD(handle), C.HFX(fx))
}

// BassFXSetParamEQ 设置 EQ 参数
func BassFXSetParamEQ(fx uint32, center float32, bandwidth float32, gain float32) error {
	params := C.BASS_DX8_PARAMEQ{
		fCenter:    C.float(center),
		fBandwidth: C.float(bandwidth),
		fGain:      C.float(gain),
	}
	if C.BASS_FXSetParameters(C.HFX(fx), unsafe.Pointer(&params)) == 0 {
		return fmt.Errorf("BASS_FXSetParameters 失败: fx=%d, errCode=%d",
			fx, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassFXGetParamEQ 获取 EQ 参数
func BassFXGetParamEQ(fx uint32) (center, bandwidth, gain float32, err error) {
	var params C.BASS_DX8_PARAMEQ
	if C.BASS_FXGetParameters(C.HFX(fx), unsafe.Pointer(&params)) == 0 {
		return 0, 0, 0, fmt.Errorf("BASS_FXGetParameters 失败: fx=%d, errCode=%d",
			fx, C.BASS_ErrorGetCode())
	}
	return float32(params.fCenter), float32(params.fBandwidth), float32(params.fGain), nil
}

// ==================== 加密文件流 ====================

// fileReadPool 复用加密文件读取缓冲区，减少高频回调的 GC 压力
var fileReadPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 8192)
		return &buf
	},
}

// EncryptedFileItem 加密文件信息
type EncryptedFileItem struct {
	File   *os.File
	Size   int64
	XorKey byte
}

// BassFileUser 封装 cgo.Handle，使用 sync.Once 防止双重释放
type BassFileUser struct {
	handle cgo.Handle
	free   sync.Once
}

func newBassFileUser(item *EncryptedFileItem) *BassFileUser {
	return &BassFileUser{
		handle: cgo.NewHandle(item),
	}
}

// Dispose 释放 cgo.Handle，可安全多次调用
func (f *BassFileUser) Dispose() {
	f.free.Do(func() { f.handle.Delete() })
}

// BassStreamCreateFileUser 创建加密文件流
func BassStreamCreateFileUser(item *EncryptedFileItem, flags uint32) (uint32, *BassFileUser, error) {
	fu := newBassFileUser(item)

	procs := C.BASS_FILEPROCS{
		close:  C.FILECLOSEPROC(C.goFileCloseProc),
		length: C.FILELENPROC(C.goFileLenProc),
		read:   C.FILEREADPROC(C.goFileReadProc),
		seek:   C.FILESEEKPROC(C.goFileSeekProc),
	}

	handle := C.BASS_StreamCreateFileUser(
		0, // STREAMFILE_NOBUFFER
		C.DWORD(flags),
		&procs,
		unsafe.Pointer(fu.handle),
	)
	if handle == 0 {
		fu.Dispose()
		return 0, nil, fmt.Errorf("BASS_StreamCreateFileUser 失败: errCode=%d",
			C.BASS_ErrorGetCode())
	}
	return uint32(handle), fu, nil
}

//export goFileCloseProc
func goFileCloseProc(user unsafe.Pointer) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "goFileCloseProc panic: %v\n", r)
		}
	}()

	h := cgo.Handle(uintptr(user))
	item := h.Value().(*EncryptedFileItem)
	item.File.Close()
	// 注意：不在 close 中 Delete handle，由 BassFileUser.Dispose() 统一管理
}

//export goFileLenProc
func goFileLenProc(user unsafe.Pointer) C.QWORD {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "goFileLenProc panic: %v\n", r)
		}
	}()

	h := cgo.Handle(uintptr(user))
	item := h.Value().(*EncryptedFileItem)
	return C.QWORD(item.Size)
}

//export goFileReadProc
func goFileReadProc(buffer unsafe.Pointer, length C.DWORD, user unsafe.Pointer) C.DWORD {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "goFileReadProc panic: %v\n", r)
		}
	}()

	h := cgo.Handle(uintptr(user))
	item := h.Value().(*EncryptedFileItem)

	reqLen := int(length)
	bufPtr := fileReadPool.Get().(*[]byte)
	buf := *bufPtr
	if len(buf) < reqLen {
		buf = make([]byte, reqLen)
		*bufPtr = buf
	}
	defer fileReadPool.Put(bufPtr)

	n, _ := item.File.Read(buf[:reqLen])
	if n <= 0 {
		return 0
	}

	if item.XorKey != 0 {
		for i := 0; i < n; i++ {
			buf[i] ^= item.XorKey
		}
	}

	C.memcpy(buffer, unsafe.Pointer(&buf[0]), C.size_t(n))
	return C.DWORD(n)
}

//export goFileSeekProc
func goFileSeekProc(offset C.QWORD, user unsafe.Pointer) C.BOOL {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "goFileSeekProc panic: %v\n", r)
		}
	}()

	h := cgo.Handle(uintptr(user))
	item := h.Value().(*EncryptedFileItem)

	_, err := item.File.Seek(int64(offset), 0)
	if err != nil {
		return C.FALSE
	}
	return C.TRUE
}

// ==================== 录音 ====================

// BassRecordInit 初始化录音设备
func BassRecordInit(device int) error {
	if C.BASS_RecordInit(C.int(device)) == 0 {
		return fmt.Errorf("BASS_RecordInit 失败: device=%d, errCode=%d",
			device, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassRecordFree 释放录音资源
func BassRecordFree() {
	C.BASS_RecordFree()
}

// BassRecordEnumDevices 枚举录音设备
func BassRecordEnumDevices() []BassDeviceInfo {
	var devices []BassDeviceInfo
	for i := 0; ; i++ {
		var info C.BASS_DEVICEINFO
		if C.BASS_RecordGetDeviceInfo(C.DWORD(i), &info) == 0 {
			break
		}
		devices = append(devices, BassDeviceInfo{
			Name:   C.GoString(info.name),
			Driver: C.GoString(info.driver),
			Flags:  uint32(info.flags),
		})
	}
	return devices
}

// ==================== 配置 ====================

// BassSetConfig 设置 BASS 全局配置
func BassSetConfig(option uint32, value uint32) error {
	if C.BASS_SetConfig(C.DWORD(option), C.DWORD(value)) == 0 {
		return fmt.Errorf("BASS_SetConfig 失败: option=%d, errCode=%d",
			option, C.BASS_ErrorGetCode())
	}
	return nil
}

// BassGetConfig 获取 BASS 全局配置
func BassGetConfig(option uint32) uint32 {
	return uint32(C.BASS_GetConfig(C.DWORD(option)))
}
