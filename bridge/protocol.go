// Package bridge 提供主控进程与播放服务子进程之间的 IPC 通信。
// 本包属于主控进程，通过 stdin/stdout JSON Line 协议与子进程通信。
package bridge

import "encoding/json"

// IPCRequest 主控 → 播放服务子进程的请求
type IPCRequest struct {
	ID     string          `json:"id"`     // 请求 ID（UUID）
	Method string          `json:"method"` // 方法名
	Params json.RawMessage `json:"params"` // 方法参数（JSON）
}

// IPCResponse 播放服务子进程 → 主控的响应
type IPCResponse struct {
	ID    string          `json:"id"`              // 对应请求 ID
	Data  json.RawMessage `json:"data,omitempty"`  // 成功时的返回数据
	Error string          `json:"error,omitempty"` // 失败时的错误信息
}

// IPCEvent 播放服务子进程 → 主控的异步事件推送
type IPCEvent struct {
	Event string          `json:"event"` // 事件名称
	Data  json.RawMessage `json:"data"`  // 事件数据
}

// --- IPC 方法名常量 ---

const (
	MethodInit         = "init"          // 初始化 BASS 引擎
	MethodLoad         = "load"          // 加载音频文件到通道
	MethodPlay         = "play"          // 播放通道
	MethodStop         = "stop"          // 停止通道
	MethodPause        = "pause"         // 暂停通道
	MethodResume       = "resume"        // 恢复通道
	MethodSetVolume    = "set_volume"    // 设置音量
	MethodSetEQ        = "set_eq"        // 设置均衡器
	MethodPosition     = "position"      // 获取播放位置
	MethodLevel        = "level"         // 获取音频电平
	MethodDeviceInfo   = "device_info"   // 获取设备信息
	MethodSetDevice    = "set_device"    // 设置设备
	MethodRemoveSync   = "remove_sync"   // 移除同步回调
	MethodFreeChannel  = "free_channel"  // 释放通道
	MethodFreeAll      = "free_all"      // 释放所有通道
	MethodShutdown     = "shutdown"      // 关闭子进程
	MethodPing         = "ping"          // 心跳检测
)

// --- IPC 事件名常量 ---

const (
	EventPlayFinished  = "play_finished"  // 播放完成
	EventPlayStarted   = "play_started"   // 播放开始
	EventDeviceLost    = "device_lost"    // 设备断开
	EventDeviceRestored = "device_restored" // 设备恢复
	EventLevel         = "level"           // 音频电平数据
	EventError         = "error"           // 子进程错误
)

// --- 常用参数结构 ---

// LoadParams 加载音频文件参数
type LoadParams struct {
	Channel   int    `json:"channel"`    // 通道索引（0-11）
	FilePath  string `json:"file_path"`  // 文件路径
	IsEncrypt bool   `json:"is_encrypt"` // 是否加密
	Volume    float64 `json:"volume"`    // 音量（0.0-1.0）
	FadeIn    int    `json:"fade_in"`    // 淡入时长（ms）
}

// PlayParams 播放参数
type PlayParams struct {
	Channel int  `json:"channel"`  // 通道索引
	Restart bool `json:"restart"`  // 是否从头播放
}

// StopParams 停止参数
type StopParams struct {
	Channel int `json:"channel"` // 通道索引
	FadeOut int `json:"fade_out"` // 淡出时长（ms），0=立即停止
}

// VolumeParams 音量参数
type VolumeParams struct {
	Channel int     `json:"channel"` // 通道索引
	Volume  float64 `json:"volume"`  // 音量（0.0-1.0）
	FadeMs  int     `json:"fade_ms"` // 渐变时长（ms），0=立即
}

// ChannelParams 通道参数（通用）
type ChannelParams struct {
	Channel int `json:"channel"` // 通道索引
}

// PositionResult 播放位置结果
type PositionResult struct {
	PositionMs int `json:"position_ms"` // 当前位置（ms）
	DurationMs int `json:"duration_ms"` // 总时长（ms）
}

// LevelResult 音频电平结果
type LevelResult struct {
	Left  float64 `json:"left"`  // 左声道电平（0.0-1.0）
	Right float64 `json:"right"` // 右声道电平（0.0-1.0）
}

// PlayFinishedData 播放完成事件数据
type PlayFinishedData struct {
	Channel int `json:"channel"` // 完成播放的通道索引
}

// DeviceLostData 设备断开事件数据
type DeviceLostData struct {
	DeviceIndex int    `json:"device_index"` // 设备索引
	DeviceName  string `json:"device_name"`  // 设备名称
}

// DeviceInfo 设备信息
type DeviceInfo struct {
	Index int    `json:"index"` // 设备索引
	Name  string `json:"name"`  // 设备名称
}

// InitParams 初始化参数
type InitParams struct {
	Device int `json:"device"` // 设备索引，-1 为默认
	Freq   int `json:"freq"`   // 采样率
}

// SetDeviceParams 设置设备参数
type SetDeviceParams struct {
	Channel     int `json:"channel"`      // 通道索引
	DeviceIndex int `json:"device_index"` // 设备索引
}

// EQParams 均衡器参数
type EQParams struct {
	Channel int       `json:"channel"` // 通道索引
	Bands   []EQBand  `json:"bands"`   // 均衡器频段
}

// EQBand 均衡器频段
type EQBand struct {
	Center    float64 `json:"center"`    // 中心频率（Hz）
	Bandwidth float64 `json:"bandwidth"` // 带宽
	Gain      float64 `json:"gain"`      // 增益（dB）
}
