package audio

import (
	"strconv"
	"strings"

	"github.com/rixingyingyao/playthread-go/models"
)

// 直接使用 models 包的 ChannelName，避免重复定义
type ChannelName = models.ChannelName

const (
	ChanMainOut   = models.ChanMainOut
	ChanPreview1  = models.ChanPreview1
	ChanPreview2  = models.ChanPreview2
	ChanPreview3  = models.ChanPreview3
	ChanPreview4  = models.ChanPreview4
	ChanPreview5  = models.ChanPreview5
	ChanPreview6  = models.ChanPreview6
	ChanPreview7  = models.ChanPreview7
	ChanFillBlank = models.ChanFillBlank
	ChanTellTime  = models.ChanTellTime
	ChanEffect    = models.ChanEffect
	ChanTempList  = models.ChanTempList
	ChanCount     = models.ChanCount
)

// ParseChannelName 转发 models.ParseChannelName
var ParseChannelName = models.ParseChannelName

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

