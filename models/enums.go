package models

import "fmt"

// Status 播出状态（对齐 C# EBCStatus）
type Status int

const (
	StatusStopped    Status = iota // 停止
	StatusAuto                     // 自动播出
	StatusManual                   // 手动播出
	StatusLive                     // 直播
	StatusRedifDelay               // 转播延时
	StatusEmergency                // 应急
)

var statusNames = [...]string{"Stopped", "Auto", "Manual", "Live", "RedifDelay", "Emergency"}

func (s Status) String() string {
	if int(s) < len(statusNames) {
		return statusNames[s]
	}
	return fmt.Sprintf("Status(%d)", s)
}

// PathType 状态迁移路径（对齐 C# EPath）
type PathType int

const (
	ErrPath PathType = -1

	Stop2Auto    PathType = 1
	Auto2Stop    PathType = 2
	Auto2Manual  PathType = 3
	Manual2Auto  PathType = 4
	Auto2Emerg   PathType = 5
	Emerg2Auto   PathType = 6
	Auto2Delay   PathType = 7
	Delay2Auto   PathType = 8
	Stop2Manual  PathType = 9
	Manual2Stop  PathType = 10
	Auto2Live    PathType = 11
	Live2Auto    PathType = 12
	Live2Manual  PathType = 13
	Manual2Live  PathType = 14
	Stop2Live    PathType = 15
	Live2Stop    PathType = 16 // 保留值：C# 定义但非合法路径（Live 不可直接到 Stopped）
	Live2Delay   PathType = 17
	Delay2Live   PathType = 18
	Stop2Delay   PathType = 19
	Manual2Delay PathType = 20
	Delay2Manual PathType = 21
)

var pathNames = map[PathType]string{
	ErrPath:      "ErrPath",
	Stop2Auto:    "Stop2Auto",
	Auto2Stop:    "Auto2Stop",
	Auto2Manual:  "Auto2Manual",
	Manual2Auto:  "Manual2Auto",
	Auto2Emerg:   "Auto2Emerg",
	Emerg2Auto:   "Emerg2Auto",
	Auto2Delay:   "Auto2Delay",
	Delay2Auto:   "Delay2Auto",
	Stop2Manual:  "Stop2Manual",
	Manual2Stop:  "Manual2Stop",
	Auto2Live:    "Auto2Live",
	Live2Auto:    "Live2Auto",
	Live2Manual:  "Live2Manual",
	Manual2Live:  "Manual2Live",
	Stop2Live:    "Stop2Live",
	Live2Stop:    "Live2Stop",
	Live2Delay:   "Live2Delay",
	Delay2Live:   "Delay2Live",
	Stop2Delay:   "Stop2Delay",
	Manual2Delay: "Manual2Delay",
	Delay2Manual: "Delay2Manual",
}

func (p PathType) String() string {
	if name, ok := pathNames[p]; ok {
		return name
	}
	return fmt.Sprintf("PathType(%d)", p)
}

// TaskType 定时任务类型
type TaskType int

const (
	TaskHard     TaskType = iota // 硬定时——到时间强制切播
	TaskSoft                     // 软定时——等当前素材播完再切
	TaskIntercut                 // 插播
)

var taskTypeNames = [...]string{"Hard", "Soft", "Intercut"}

func (t TaskType) String() string {
	if int(t) < len(taskTypeNames) {
		return taskTypeNames[t]
	}
	return fmt.Sprintf("TaskType(%d)", t)
}

// FadeMode 淡变模式（对齐 C# FadeType）
type FadeMode int

const (
	FadeInOut FadeMode = iota // 淡入+淡出
	FadeIn                    // 仅淡入
	FadeOut                   // 仅淡出
	FadeNone                  // 无淡变
)

var fadeModeNames = [...]string{"FadeIn_Out", "FadeIn", "FadeOut", "None"}

func (f FadeMode) String() string {
	if int(f) < len(fadeModeNames) {
		return fadeModeNames[f]
	}
	return fmt.Sprintf("FadeMode(%d)", f)
}

// PlayState 播放状态（对齐 C# EPlayState / ManagedBass.PlaybackState）
type PlayState int

const (
	PlayStateStopped PlayState = 0 // 已停止
	PlayStatePlaying PlayState = 1 // 播放中
	PlayStatePaused  PlayState = 3 // 已暂停（BASS 中 Stalled=2，Paused=3）
)

var playStateNames = map[PlayState]string{
	PlayStateStopped: "Stopped",
	PlayStatePlaying: "Playing",
	PlayStatePaused:  "Paused",
}

func (ps PlayState) String() string {
	if name, ok := playStateNames[ps]; ok {
		return name
	}
	return fmt.Sprintf("PlayState(%d)", ps)
}

// IntercutType 插播类型
type IntercutType int

const (
	IntercutTimed     IntercutType = iota // 定时插播
	IntercutEmergency                     // 紧急插播
)

// SignalType 信号源类型
type SignalType int

const (
	SignalNone    SignalType = 0 // 无信号（文件播放）
	SignalLive    SignalType = 1 // 直播信号
	SignalRelay   SignalType = 2 // 转播信号
	SignalCapture SignalType = 3 // 采集信号
)

// ProgramType 素材类型
type ProgramType int

const (
	ProgramNormal      ProgramType = 0  // 普通素材
	ProgramSongPreview ProgramType = 17 // 歌曲预告（合并片段播放）
)

// ChannelName 虚拟通道名称枚举（对齐 C# ChananelName）
type ChannelName int

const (
	ChanMainOut  ChannelName = iota // 主播出
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

var channelNames = [...]string{
	"MainOut", "Preview1", "Preview2", "Preview3",
	"Preview4", "Preview5", "Preview6", "Preview7",
	"FillBlank", "TellTime", "Effect", "TempList",
}

func (c ChannelName) String() string {
	if int(c) < len(channelNames) {
		return channelNames[c]
	}
	return fmt.Sprintf("ChannelName(%d)", c)
}

// ParseChannelName 将字符串解析为 ChannelName 枚举值
func ParseChannelName(s string) (ChannelName, bool) {
	for i, name := range channelNames {
		if name == s {
			return ChannelName(i), true
		}
	}
	return 0, false
}

// SharesMainOutDevice 判断该通道是否共用 MainOut 的物理设备
func (c ChannelName) SharesMainOutDevice() bool {
	return c == ChanFillBlank || c == ChanTellTime || c == ChanEffect || c == ChanTempList
}

// TransitionError 非法状态迁移错误
type TransitionError struct {
	From   Status
	To     Status
	Reason string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("非法状态迁移 %s → %s: %s", e.From, e.To, e.Reason)
}
