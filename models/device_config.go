package models

import (
	"strconv"
	"strings"
)

const CustomSoundCardPrefix = "CustomSoundCard"

// ChannelLocalSet 频道本地设置（对齐 C# ChannelLocalSet）
// 每个 vrchannelsetN 是一个 [2]string 数组：[0]=主设备名, [1]=备用设备名
type ChannelLocalSet struct {
	VRChannelSet1 [2]string `json:"vrchannelset1"` // MainOut + FillBlank/TellTime/Effect/TempList
	VRChannelSet2 [2]string `json:"vrchannelset2"` // Preview1
	VRChannelSet3 [2]string `json:"vrchannelset3"` // Preview2
	VRChannelSet4 [2]string `json:"vrchannelset4"` // Preview3
	VRChannelSet5 [2]string `json:"vrchannelset5"` // Preview4
	VRChannelSet6 [2]string `json:"vrchannelset6"` // Preview5
	VRChannelSet7 [2]string `json:"vrchannelset7"` // Preview6
	VRChannelSet8 [2]string `json:"vrchannelset8"` // Preview7
}

// GetDeviceSet 按通道索引（0-7，对应 8 组 vrchannelset）获取设备配对
func (cls *ChannelLocalSet) GetDeviceSet(setIndex int) [2]string {
	switch setIndex {
	case 0:
		return cls.VRChannelSet1
	case 1:
		return cls.VRChannelSet2
	case 2:
		return cls.VRChannelSet3
	case 3:
		return cls.VRChannelSet4
	case 4:
		return cls.VRChannelSet5
	case 5:
		return cls.VRChannelSet6
	case 6:
		return cls.VRChannelSet7
	case 7:
		return cls.VRChannelSet8
	default:
		return [2]string{}
	}
}

// VirtualChannelDeviceMap 虚拟通道与物理设备的映射表
type VirtualChannelDeviceMap struct {
	Channel          ChannelName
	MainDeviceName   string
	MainDeviceIndex  int
	MainCustomIndex  int // -1 表示非自定义声卡
	StandbyDeviceName  string
	StandbyDeviceIndex int
	StandbyCustomIndex int
}

// BuildDeviceMap 根据 ChannelLocalSet 构建 12 通道的设备映射。
// 共用 MainOut 设备的通道（FillBlank/TellTime/Effect/TempList）自动绑定到 vrchannelset1。
func BuildDeviceMap(cls *ChannelLocalSet) []VirtualChannelDeviceMap {
	channelToSet := map[ChannelName]int{
		ChanMainOut:   0,
		ChanPreview1:  1,
		ChanPreview2:  2,
		ChanPreview3:  3,
		ChanPreview4:  4,
		ChanPreview5:  5,
		ChanPreview6:  6,
		ChanPreview7:  7,
		ChanFillBlank: 0, // 共用 MainOut 设备
		ChanTellTime:  0,
		ChanEffect:    0,
		ChanTempList:  0,
	}

	maps := make([]VirtualChannelDeviceMap, int(ChanCount))
	for ch := ChannelName(0); ch < ChanCount; ch++ {
		setIdx := channelToSet[ch]
		devPair := cls.GetDeviceSet(setIdx)

		dm := VirtualChannelDeviceMap{
			Channel:            ch,
			MainDeviceName:     devPair[0],
			MainDeviceIndex:    -1,
			MainCustomIndex:    ParseCustomChannelIndex(devPair[0]),
			StandbyDeviceName:  devPair[1],
			StandbyDeviceIndex: -1,
			StandbyCustomIndex: ParseCustomChannelIndex(devPair[1]),
		}
		maps[int(ch)] = dm
	}
	return maps
}

// ParseCustomChannelIndex 从设备名解析自定义声卡通道索引。
// 示例："CustomSoundCard_0_3" → 3，非自定义声卡返回 -1。
func ParseCustomChannelIndex(deviceName string) int {
	if !strings.Contains(deviceName, CustomSoundCardPrefix) {
		return -1
	}
	parts := strings.Split(deviceName, "_")
	if len(parts) > 0 {
		idx, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil {
			return idx
		}
	}
	return -1
}

// IsCustomSoundCard 判断设备名是否为自定义声卡
func IsCustomSoundCard(deviceName string) bool {
	return strings.Contains(deviceName, CustomSoundCardPrefix)
}

// PlayDeviceInfo 播放设备信息（对齐 C# PlayDeviceInfo）
type PlayDeviceInfo struct {
	DeviceName  string `json:"device_name"`
	DeviceIndex int    `json:"device_index"`
	Initialized bool   `json:"initialized"`
}

// EQAudioEffect 均衡器预设（对齐 C# EqAudioEffect）
type EQAudioEffect struct {
	ID      int       `json:"id"`
	Name    string    `json:"name"`
	Content []float32 `json:"content"` // 10 段均衡器增益值
}
