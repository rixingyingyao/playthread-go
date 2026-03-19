package audio

import (
	"os"
	"strconv"
	"strings"
)

// ChannelName 通道名称枚举
type ChannelName int

const (
	ChanMainOut   ChannelName = iota // 主播出
	ChanPreview1                     // 预听1
	ChanPreview2                     // 预听2
	ChanPreview3                     // 预听3
	ChanPreview4                     // 预听4
	ChanPreview5                     // 预听5
	ChanPreview6                     // 预听6
	ChanPreview7                     // 预听7
	ChanFillBlank                    // 垫乐/补白
	ChanTellTime                     // 报时
	ChanEffect                       // 音效
	ChanTempList                     // 临时播表
	ChanCount                        // 通道总数（哨兵值）
)

// channelNames 通道名到枚举映射
var channelNames = map[string]ChannelName{
	"MainOut":   ChanMainOut,
	"Preview1":  ChanPreview1,
	"Preview2":  ChanPreview2,
	"Preview3":  ChanPreview3,
	"Preview4":  ChanPreview4,
	"Preview5":  ChanPreview5,
	"Preview6":  ChanPreview6,
	"Preview7":  ChanPreview7,
	"FillBlank": ChanFillBlank,
	"TellTime":  ChanTellTime,
	"Effect":    ChanEffect,
	"TempList":  ChanTempList,
}

// channelStrings 枚举到字符串映射
var channelStrings = [ChanCount]string{
	"MainOut", "Preview1", "Preview2", "Preview3",
	"Preview4", "Preview5", "Preview6", "Preview7",
	"FillBlank", "TellTime", "Effect", "TempList",
}

// String 返回通道名称字符串
func (cn ChannelName) String() string {
	if cn >= 0 && cn < ChanCount {
		return channelStrings[cn]
	}
	return "Unknown"
}

// ParseChannelName 解析通道名称字符串
func ParseChannelName(name string) (ChannelName, bool) {
	cn, ok := channelNames[name]
	return cn, ok
}

// VirtualChannel 虚拟通道
type VirtualChannel struct {
	Name     ChannelName
	Index    int    // 通道索引（与 ChannelName 值相同）
	FilePath string // 当前加载的文件路径

	// 播放状态
	StreamHandle uint32       // 当前 BASS 流句柄
	SyncHandle   uint32       // 播完同步句柄
	FileUser     *BassFileUser // 加密文件 cgo.Handle 持有者

	// 设备绑定
	DeviceName      string // 主设备名
	DeviceIndex     int    // 主设备 BASS 索引
	CustomChannelIdx int   // 自定义声卡通道路由索引

	StandbyDeviceName string // 备用设备名
	StandbyDeviceIdx  int    // 备用设备 BASS 索引
	StandbyCustomIdx  int    // 备用声卡通道路由索引
}

// VirtualChannelManager 虚拟通道管理器
type VirtualChannelManager struct {
	channels [ChanCount]*VirtualChannel
}

// NewVirtualChannelManager 创建虚拟通道管理器，初始化 12 个通道
func NewVirtualChannelManager() *VirtualChannelManager {
	mgr := &VirtualChannelManager{}
	for i := 0; i < int(ChanCount); i++ {
		mgr.channels[i] = &VirtualChannel{
			Name:             ChannelName(i),
			Index:            i,
			DeviceIndex:      -1,
			CustomChannelIdx: -1,
			StandbyDeviceIdx: -1,
			StandbyCustomIdx: -1,
		}
	}
	return mgr
}

// Get 按通道名称字符串获取虚拟通道
func (m *VirtualChannelManager) Get(name string) *VirtualChannel {
	cn, ok := ParseChannelName(name)
	if !ok {
		return nil
	}
	return m.channels[cn]
}

// GetByIndex 按索引获取虚拟通道
func (m *VirtualChannelManager) GetByIndex(index int) *VirtualChannel {
	if index < 0 || index >= int(ChanCount) {
		return nil
	}
	return m.channels[index]
}

// GetByName 按枚举获取虚拟通道
func (m *VirtualChannelManager) GetByName(name ChannelName) *VirtualChannel {
	if name < 0 || name >= ChanCount {
		return nil
	}
	return m.channels[name]
}

// SetDevice 为通道设置主设备
func (m *VirtualChannelManager) SetDevice(name ChannelName, deviceName string, deviceIndex int) {
	ch := m.GetByName(name)
	if ch == nil {
		return
	}
	ch.DeviceName = deviceName
	ch.DeviceIndex = deviceIndex
	ch.CustomChannelIdx = ParseCustomChannelIndex(deviceName)
}

// SetStandbyDevice 为通道设置备用设备
func (m *VirtualChannelManager) SetStandbyDevice(name ChannelName, deviceName string, deviceIndex int) {
	ch := m.GetByName(name)
	if ch == nil {
		return
	}
	ch.StandbyDeviceName = deviceName
	ch.StandbyDeviceIdx = deviceIndex
	ch.StandbyCustomIdx = ParseCustomChannelIndex(deviceName)
}

// ShareMainOutDevice 为共用 MainOut 设备的通道设置设备信息
// FillBlank/TellTime/Effect/TempList 共用 MainOut 的物理设备（vrchannelset1）
func (m *VirtualChannelManager) ShareMainOutDevice() {
	mainOut := m.GetByName(ChanMainOut)
	if mainOut == nil {
		return
	}
	shared := []ChannelName{ChanFillBlank, ChanTellTime, ChanEffect, ChanTempList}
	for _, name := range shared {
		ch := m.GetByName(name)
		if ch == nil {
			continue
		}
		ch.DeviceName = mainOut.DeviceName
		ch.DeviceIndex = mainOut.DeviceIndex
		ch.CustomChannelIdx = mainOut.CustomChannelIdx
		ch.StandbyDeviceName = mainOut.StandbyDeviceName
		ch.StandbyDeviceIdx = mainOut.StandbyDeviceIdx
		ch.StandbyCustomIdx = mainOut.StandbyCustomIdx
	}
}

// ParseCustomChannelIndex 从设备名解析自定义声卡通道索引。
// 设备名格式示例：CustomSoundCard_0_3 → 返回 3
// 非自定义声卡返回 -1
func ParseCustomChannelIndex(deviceName string) int {
	if !strings.Contains(deviceName, "CustomSoundCard") {
		return -1
	}
	parts := strings.Split(deviceName, "_")
	if len(parts) > 0 {
		idx, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return -1
		}
		return idx
	}
	return -1
}

// openFile 打开文件（封装 os.Open）
func openFile(path string) (*os.File, error) {
	return os.Open(path)
}
