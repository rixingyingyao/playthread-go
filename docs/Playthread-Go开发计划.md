# Playthread-Go 开发计划

> 版本：v1.0  
> 基于 C# 原版 Playthread 模块（12 个源文件、约 10,000 行代码）的全功能 Go 重写。  
> 本计划与《Playthread-Go 开发规范》配套使用。

---

## 一、项目总览

### 1.1 核心约束

| 约束 | 要求 |
|------|------|
| **运行时长** | 7×24×365 不关机 |
| **定时精度** | 硬定时 ±50ms（95%） |
| **崩溃恢复** | < 5 秒恢复播出 |
| **平台** | Windows 主平台，Linux 兼容 |
| **协议兼容** | 与现有云端 SAAS / 本地中心接口兼容 |
| **部署形态** | 主控 exe + 播放服务 exe + BASS DLL + 配置文件（双进程架构） |

### 1.2 功能范围

**原版 C# 12 项核心功能**：

| # | 功能 | 对应 C# 文件 |
|---|------|-------------|
| 1 | 主播出线程编排 | SlvcPlayThread.cs |
| 2 | 六状态状态机 | SlvcStateController.cs |
| 3 | 硬/软定时任务 | SlaFixTimeTaskManager.cs |
| 4 | 垫乐填充与选曲 | SlaBlankTaskManager.cs |
| 5 | 垫乐播放信息（单例） | SlaBlankPlayInfo.cs |
| 6 | 播卡音频适配器 | SlaCardPlayerAdapter.cs |
| 7 | BASS 虚拟通道管理 | VirtualChannelManage.cs |
| 8 | BASS 播放控制 | BassPlayControll.cs |
| 9 | 音频电平表 | AudioLevelMeter.cs |
| 10 | 音频录制 | BassRecord.cs |
| 11 | 通道矩阵 | ChannelMatrix.cs |
| 12 | 自定义事件 | SlvcThreadEvent.cs |

**新增 8 项增强功能**：

| # | 功能 | 说明 |
|---|------|------|
| 13 | REST API | 播出控制/状态查询/播表管理 |
| 14 | WebSocket 实时推送 | 播出进度/状态变更/音量电平 |
| 15 | 云端/本地中心双数据源 | 自动降级 + 手动回切 |
| 16 | 素材缓存管理 | 断点续传 + LRU 清理 |
| 17 | 冷启动断点恢复 | PlayingInfo 快照 + 时间偏移计算 |
| 18 | 结构化日志 | zerolog + 文件轮转 |
| 19 | 内存/资源监控 | pprof + 定期快照 |
| 20 | Windows 服务注册 | golang.org/x/sys/windows/svc |

---

## 二、技术栈

| 领域 | 技术选择 | 说明 |
|------|---------|------|
| **语言** | Go 1.22+ | 泛型、GOMEMLIMIT、增强 GC |
| **音频 FFI** | cgo + BASS（仅在播放服务子进程中） | 主控纯 Go，通过 IPC 调用子进程 |
| **HTTP 路由** | chi v5 | 轻量级、标准 net/http 兼容 |
| **WebSocket** | gorilla/websocket | 成熟可靠 |
| **日志** | zerolog + lumberjack v2 | 零分配结构化日志 + 文件滚动 |
| **配置** | gopkg.in/yaml.v3 | YAML 配置 + Go struct 映射 |
| **数据库** | modernc.org/sqlite (纯 Go) | WAL 模式 + 单写入连接，无 cgo 依赖 |
| **测试** | testing + testify + goleak | `-race` 竞态检测 |
| **构建** | go build | 主控 CGO_ENABLED=0 交叉编译；播放服务需目标平台 |
| **CI** | GitHub Actions | lint + test -race + build |

---

## 三、项目目录结构

```
playthread-go/
├── cmd/                           # 可执行入口
│   ├── playthread/                # 主控进程（纯 Go）
│   │   └── main.go               # 入口：信号处理 + 组件编排 + 子进程管理
│   └── audio-service/             # 播放服务子进程（Go + cgo）
│       └── main.go               # 入口：IPC 服务端 + BASS 引擎
│
├── config.yaml                    # 运行时配置
├── go.mod / go.sum                # 依赖管理
│
├── core/                          # 业务调度层（主控进程）
│   ├── play_thread.go             # 主编排 goroutine（对应 SlvcPlayThread.cs）
│   ├── play_thread_test.go
│   ├── state_machine.go           # 六状态状态机（对应 SlvcStateController.cs）
│   ├── state_machine_test.go
│   ├── fix_time_manager.go        # 定时任务调度（对应 SlaFixTimeTaskManager.cs）
│   ├── fix_time_manager_test.go
│   ├── blank_manager.go           # 垫乐管理（对应 SlaBlankTaskManager.cs + SlaBlankPlayInfo.cs）
│   ├── blank_manager_test.go
│   ├── intercut_manager.go        # 插播管理
│   ├── intercut_manager_test.go
│   ├── channel_hold.go            # 通道保持
│   └── events.go                  # 事件定义（对应 SlvcThreadEvent.cs）
│
├── bridge/                        # 音频桥接层（IPC 客户端，主控进程使用）
│   ├── audio_bridge.go            # IPC 客户端封装
│   ├── audio_bridge_test.go
│   ├── process_manager.go         # 子进程生命周期管理（启动/重启/watchdog）
│   ├── process_manager_test.go
│   └── protocol.go                # IPC 协议定义（请求/响应/事件）
│
├── audio/                         # 音频引擎层（播放服务子进程，Go + cgo）
│   ├── ipc_server.go              # IPC 服务端（stdin 读命令，stdout 写结果）
│   ├── bass_bindings.go           # BASS cgo 低层绑定（对应 BassPlayControll.cs）
│   ├── bass_engine.go             # BASS 引擎生命周期 + LockOSThread + 双通道派发
│   ├── bass_engine_test.go
│   ├── virtual_channel.go         # 虚拟通道管理（对应 VirtualChannelManage.cs）
│   ├── virtual_channel_test.go
│   ├── adapter.go                 # 播卡适配器（对应 SlaCardPlayerAdapter.cs）
│   ├── adapter_test.go
│   ├── level_meter.go             # 音频电平表（对应 AudioLevelMeter.cs）
│   ├── recorder.go                # 音频录制（对应 BassRecord.cs）
│   ├── channel_matrix.go          # 通道矩阵（对应 ChannelMatrix.cs）
│   └── libs/                      # BASS 动态库 + 头文件
│       ├── bass.h
│       ├── windows/
│       │   └── bass.dll
│       └── linux/
│           └── libbass.so
│
├── models/                        # 数据模型（主控 + 子进程共享）
│   ├── playlist.go                # 播表/时间块
│   ├── program.go                 # 节目/素材
│   ├── enums.go                   # 枚举（状态/任务类型/信号类型等）
│   └── events.go                  # 事件数据结构
│
├── api/                           # HTTP/WebSocket/UDP 层（主控进程）
│   ├── server.go                  # HTTP 服务器 + chi 路由
│   ├── server_test.go
│   ├── handlers.go                # REST 处理器
│   ├── handlers_test.go
│   ├── udp.go                     # UDP 紧急控制监听
│   ├── ws.go                      # WebSocket 连接管理 + 推送
│   ├── ws_test.go
│   └── middleware.go              # 中间件（日志/CORS/认证）
│
├── infra/                         # 基础设施（主控进程；platform/ 子包为双进程共享）
│   ├── config.go                  # 配置模型 + 加载
│   ├── logger.go                  # zerolog + lumberjack 初始化
│   ├── file_cache.go              # 素材下载 + LRU 清理
│   ├── snapshot.go                # PlayingInfo 冷启动快照
│   ├── blank_history.go           # 垫乐播放历史持久化
│   ├── monitor.go                 # 内存/CPU/磁盘/goroutine 监控
│   ├── datasource.go              # 云端/本地中心双数据源
│   └── platform/                  # 跨平台适配
│       ├── platform.go            # 接口定义
│       ├── windows.go             # Windows 特定实现
│       └── linux.go               # Linux 特定实现
│
├── db/                            # 数据库层（主控进程）
│   ├── sqlite.go                  # SQLite 连接管理
│   ├── migrations.go              # Schema 迁移
│   └── repos.go                   # 数据仓库（历史/设置）
│
└── tests/                         # 集成测试
    ├── integration_test.go
    ├── stability_test.go          # 长时间稳定性测试
    └── testdata/
        ├── test_playlist.json
        └── test_audio.mp3
```

---

## 四、分阶段路线图

```
Phase 1 ─→ Phase 2 ─→ Phase 3 ─→ Phase 4 ─→ Phase 5 ─→ Phase 6 ─→ Phase 7 ─→ Phase 8 ─→ Phase 9
 音频引擎     数据模型    状态机+编排   定时+垫乐    插播+通道保持   API层       云端对接     打包部署     联调压测
 (IPC+子进程  (struct)   (goroutine)  (Ticker)   (栈+等待)     (chi+ws+   (降级/缓存)  (双进程)    (168h)
  +cgo/BASS)                                                  udp)
```

**依赖关系**：每个 Phase 依赖前一个 Phase 的产出。Phase 1-3 为基础层，Phase 4-6 为功能层，Phase 7-9 为集成层。

---

## 五、Phase 1 — 双进程架构与音频引擎

### 5.1 目标

搭建双进程架构骨架（主控 + 播放服务），完成 IPC 协议、子进程生命周期管理和 BASS cgo 绑定，验证整体技术栈可行性。

### 5.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 1.1 | IPC 协议定义（请求/响应/事件） | `bridge/protocol.go` |
| 1.2 | AudioBridge IPC 客户端（主控侧） | `bridge/audio_bridge.go` |
| 1.3 | 子进程生命周期管理（启动/重启/watchdog/指数退避） | `bridge/process_manager.go` |
| 1.4 | IPC 服务端（播放服务子进程侧，stdin/stdout JSON 行） | `audio/ipc_server.go` |
| 1.5 | BASS cgo 绑定（核心 API ~20 个） | `audio/bass_bindings.go` |
| 1.6 | BASS 引擎生命周期（Init/Free/SetDevice） | `audio/bass_engine.go` |
| 1.7 | LockOSThread + **双通道派发**模型（ctrlCh + ioCh） | `audio/bass_engine.go` |
| 1.8 | **12 通道虚拟通道管理**（见 5.3.1） | `audio/virtual_channel.go` |
| 1.9 | **主备设备绑定**（每通道 Main + Standby 设备） | `audio/virtual_channel.go` |
| 1.10 | **自定义声卡通道路由**（CustomChannelIndex 解析） | `audio/virtual_channel.go` |
| 1.11 | 播完回调（cgo export → channel → IPC 事件推送） | `audio/bass_bindings.go` |
| 1.12 | 加密文件流回调（BassFileUser/cgo.Handle + sync.Once） | `audio/bass_bindings.go` |
| 1.13 | 音频电平表 | `audio/level_meter.go` |
| 1.14 | 通道矩阵（输出路由管理） | `audio/channel_matrix.go` |
| 1.15 | 音频录制 | `audio/recorder.go` |
| 1.16 | 播卡适配器（封装虚拟通道组合逻辑） | `audio/adapter.go` |
| 1.17 | Windows 高精度定时器 `timeBeginPeriod(1)` | `infra/platform/windows.go` |
| 1.18 | 主控入口 + 播放服务入口 | `cmd/playthread/main.go` + `cmd/audio-service/main.go` |
| 1.19 | 单元测试 + 集成测试 + IPC 往返基准测试 | `*_test.go` |

### 5.3 关键设计

#### 5.3.1 虚拟通道拓扑（对齐 C# VirtualChannelManage.ChananelName）

```
通道索引  名称        用途              设备绑定
  0      MainOut     主播出输出         vrchannelset1 (独立设备组)
  1      Preview1    预听通道 1         vrchannelset2
  2      Preview2    预听通道 2         vrchannelset3
  3      Preview3    预听通道 3         vrchannelset4
  4      Preview4    预听通道 4         vrchannelset5
  5      Preview5    预听通道 5         vrchannelset6
  6      Preview6    预听通道 6         vrchannelset7
  7      Preview7    预听通道 7         vrchannelset8
  8      FillBlank   垫乐/补白          共用 vrchannelset1 (与 MainOut 同设备)
  9      TellTime    报时               共用 vrchannelset1
 10      Effect      音效               共用 vrchannelset1
 11      TempList    临时播表           共用 vrchannelset1
```

> **关键**：FillBlank、TellTime、Effect、TempList 共用 MainOut 的物理设备（vrchannelset1[0]），
> 这意味着它们的输出混合到同一声卡输出。不是简单的"双虚拟通道"。

#### 5.3.2 主备设备模型

每个虚拟通道绑定**主设备 + 备用设备**：

```go
// VirtualChannel 虚拟通道
type VirtualChannel struct {
    Name              ChannelName
    DeviceName        string // 主设备名
    DeviceIndex       int    // 主设备 BASS 索引
    CustomChannelIdx  int    // 自定义声卡通道路由索引
    
    StandbyDeviceName string // 备用设备名
    StandbyDeviceIdx  int    // 备用设备 BASS 索引
    StandbyCustomIdx  int    // 备用声卡通道路由索引
    
    // ... 播放状态
}

// ChannelName 通道名称枚举
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
```

#### 5.3.3 自定义声卡路由

当设备名包含 `CustomSoundCard` 时，从设备名解析通道索引：

```go
// 示例：设备名 "CustomSoundCard_0_3" → CustomChannelIdx = 3
func parseCustomChannelIndex(deviceName string) int {
    if !strings.Contains(deviceName, "CustomSoundCard") {
        return -1
    }
    parts := strings.Split(deviceName, "_")
    if len(parts) > 0 {
        idx, _ := strconv.Atoi(parts[len(parts)-1])
        return idx
    }
    return -1
}
```

#### BASS cgo 绑定（播放服务子进程内部）

```go
package audio

/*
#cgo windows LDFLAGS: -L${SRCDIR}/libs/windows -lbass
#cgo linux LDFLAGS: -L${SRCDIR}/libs/linux -lbass -Wl,-rpath,${SRCDIR}/libs/linux
#include "libs/bass.h"
#include <stdlib.h>
*/
import "C"
import (
    "fmt"
    "runtime"
    "runtime/cgo"
    "unsafe"
)

// Init 初始化 BASS 引擎。必须在 LockOSThread 的 goroutine 中调用。
func Init(device int, freq int) error {
    if C.BASS_Init(C.int(device), C.DWORD(freq), 0, nil, nil) == 0 {
        return fmt.Errorf("BASS_Init 失败: errCode=%d", C.BASS_ErrorGetCode())
    }
    return nil
}

// StreamCreateFile 从文件创建音频流
func StreamCreateFile(path string, flags C.DWORD) (C.HSTREAM, error) {
    cpath := C.CString(path)
    defer C.free(unsafe.Pointer(cpath))
    
    handle := C.BASS_StreamCreateFile(0, unsafe.Pointer(cpath), 0, 0, flags)
    if handle == 0 {
        return 0, fmt.Errorf("BASS_StreamCreateFile 失败: path=%s, errCode=%d", path, C.BASS_ErrorGetCode())
    }
    return handle, nil
}
```

#### LockOSThread + 双通道派发（播放服务子进程内部）

```go
// BassEngine BASS 引擎，所有 BASS 操作通过双 channel 派发到专用 OS 线程
// ctrlCh: 低延迟控制命令（Play/Stop/Pause），优先处理
// ioCh:   可能耗时的 IO 操作（StreamCreateFile/StreamFree）
type BassEngine struct {
    ctrlCh chan bassCommand // 控制通道（优先）
    ioCh   chan bassCommand // IO 通道
    stopCh chan struct{}
}

type bassCommand struct {
    fn       func() interface{}
    resultCh chan interface{}
}

// Run 启动 BASS 专用 goroutine（在播放服务子进程中调用）
func (be *BassEngine) Run(ctx context.Context) {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    
    // 在此 OS 线程初始化 BASS
    if err := Init(-1, 44100); err != nil {
        panic(fmt.Sprintf("BASS 初始化失败: %v", err))
    }
    defer C.BASS_Free()
    
    for {
        // 控制命令优先处理
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
                return
            }
        }
    }
}

// exec 在 BASS 线程执行函数并返回结果（控制命令走 ctrlCh，IO 命令走 ioCh）
func (be *BassEngine) execCtrl(fn func() interface{}) interface{} {
    ch := make(chan interface{}, 1)
    be.ctrlCh <- bassCommand{fn: fn, resultCh: ch}
    return <-ch
}

func (be *BassEngine) execIO(fn func() interface{}) interface{} {
    ch := make(chan interface{}, 1)
    be.ioCh <- bassCommand{fn: fn, resultCh: ch}
    return <-ch
}
```

#### 播完回调

```go
// C 回调 → Go channel 通知
//export goSyncEndCallback
func goSyncEndCallback(handle C.HSYNC, channel C.DWORD, data C.DWORD, user unsafe.Pointer) {
    h := cgo.Handle(uintptr(user))
    ch := h.Value().(chan C.DWORD)
    select {
    case ch <- channel:
    default:
        // channel 满，丢弃旧值（不阻塞 BASS C 线程）
    }
}
```

### 5.4 风险与应对

| 风险 | 应对 |
|------|------|
| IPC 延迟影响播出实时性 | 基准测试验证 IPC 往返 < 5ms（实际预期 1-2ms） |
| 子进程崩溃导致短暂无声 | watchdog 自动重启 < 2s + 日志记录崩溃原因 |
| cgo 调用开销影响实时性 | 基准测试验证 < 1μs/调用 |
| BASS 线程亲和性失败 | 子进程内所有 BASS 操作走 ctrlCh/ioCh 派发 |
| 加密文件 cgo.Handle 泄漏 | BassFileUser + sync.Once 防双重释放 |
| Windows/Linux BASS 行为差异 | 两平台各跑一遍集成测试 |

### 5.5 验收标准

- 主控进程启动后可自动拉起播放服务子进程
- IPC 往返延迟 < 5ms（基准测试验证）
- 子进程崩溃后 < 2s 自动重启，主控不受影响
- cgo 绑定覆盖 20+ 个核心 BASS API
- 可播放 mp3/wav 文件，淡入淡出正常
- 播完事件通过 IPC 正确传递到主控
- 加密文件流可正常播放
- `go test -race ./...` 全部通过
- LockOSThread 模型下 BASS 通道操作无崩溃

---

## 六、Phase 2 — 数据模型与基础设施

### 6.1 目标

定义所有业务数据结构、配置加载、日志初始化、数据库连接——为 Phase 3 核心调度提供基础。

### 6.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 2.1 | 播表数据模型（Playlist/TimeBlock/Program） | `models/playlist.go` |
| 2.2 | 素材数据模型（PlayClip/PlayItem） | `models/program.go` |
| 2.3 | 枚举定义（Status/TaskType/SignalType/PlayMode） | `models/enums.go` |
| 2.4 | 事件数据结构 | `models/events.go` |
| 2.5 | 配置模型 + YAML 加载 + 校验 | `infra/config.go` |
| 2.6 | zerolog + lumberjack 日志初始化 | `infra/logger.go` |
| 2.7 | SQLite 连接管理（WAL + 单写入） | `db/sqlite.go` |
| 2.8 | Schema 迁移 | `db/migrations.go` |
| 2.9 | 数据仓库（播放历史/设置） | `db/repos.go` |
| 2.10 | PlayingInfo 快照读写 | `infra/snapshot.go` |
| 2.11 | 跨平台适配接口 | `infra/platform/` |
| 2.12 | 歌曲预览数据模型（type=17 合并片段） | `models/program.go` |
| 2.13 | 串词参数模型（link_damping/fadein/fadeout） | `models/program.go` |
| 2.14 | 4 种淡变模式枚举（FadeIn_Out/FadeIn/FadeOut/None） | `models/enums.go` |
| 2.15 | 垫乐历史持久化（BlankPadding.his XML 格式 + 2 天清理） | `infra/blank_history.go` |
| 2.16 | 自定义声卡配置解析（VirtualChannelManage 设备映射） | `models/device_config.go` |
| 2.17 | 单元测试 | `*_test.go` |

### 6.3 关键设计

#### 枚举定义

```go
package models

// Status 播出状态
type Status int

const (
    StatusStopped    Status = iota // 停止
    StatusAuto                     // 自动播出
    StatusManual                   // 手动播出
    StatusLive                     // 直播
    StatusRedifDelay               // 转播延时
    StatusEmergency                // 应急
)

func (s Status) String() string {
    names := [...]string{"Stopped", "Auto", "Manual", "Live", "RedifDelay", "Emergency"}
    if int(s) < len(names) {
        return names[s]
    }
    return fmt.Sprintf("Status(%d)", s)
}

// TaskType 定时任务类型
type TaskType int

const (
    TaskHard      TaskType = iota // 硬定时——到时间强制切播
    TaskSoft                      // 软定时——等当前素材播完再切
    TaskIntercut                  // 插播
)
```

#### 播表数据模型

```go
package models

import "time"

// Playlist 日播单
type Playlist struct {
    ID        string      `json:"id"`
    Date      time.Time   `json:"date"`       // 播出日期
    Version   int         `json:"version"`    // 版本号（云端下发）
    Blocks    []TimeBlock `json:"blocks"`     // 时间块列表
    FlatList  []*Program  `json:"-"`          // 展开后的平铺素材列表（运行时生成）
}

// TimeBlock 时间块（对应 C# 的 TimeBlock）
type TimeBlock struct {
    ID        string     `json:"id"`
    Name      string     `json:"name"`      // 时间块名称
    StartTime string     `json:"start_time"` // HH:MM:SS
    EndTime   string     `json:"end_time"`
    Programs  []Program  `json:"programs"`
    TaskType  TaskType   `json:"task_type"`  // 硬定时/软定时
}

// Program 节目/素材
type Program struct {
    ID         string  `json:"id"`
    Name       string  `json:"name"`
    FilePath   string  `json:"file_path"`
    Duration   int     `json:"duration"`    // 总时长(ms)
    InPoint    int     `json:"in_point"`    // 入点(ms)
    OutPoint   int     `json:"out_point"`   // 出点(ms)
    Volume     float64 `json:"volume"`      // 音量(0-1)
    FadeIn     int     `json:"fade_in"`     // 淡入(ms)
    FadeOut    int     `json:"fade_out"`    // 淡出(ms)
    FadeMode   int     `json:"fade_mode"`   // 淡变模式: 0=FadeIn_Out 1=FadeIn 2=FadeOut 3=None
    IsEncrypt  bool    `json:"is_encrypt"`  // 是否加密
    SignalID   int     `json:"signal_id"`   // 信号源ID
    Type       int     `json:"type"`        // 素材类型, 17=歌曲预览(合并片段)
    LinkDamping float64 `json:"link_damping"` // 串词压低量(dB)
    LinkFadeIn  int    `json:"link_fadein"`  // 串词淡入(ms)
    LinkFadeOut int    `json:"link_fadeout"` // 串词淡出(ms)
}
```

### 6.4 验收标准

- 所有数据模型可正确 JSON 序列化/反序列化
- YAML 配置加载成功，校验规则生效
- SQLite 数据库创建 + 迁移 + CRUD 正常
- zerolog 日志输出到文件 + 控制台
- `go test -race ./models/... ./infra/... ./db/...` 全部通过

---

## 七、Phase 3 — 状态机与核心播出编排

### 7.1 目标

实现六状态状态机和主播出 goroutine（PlayThread），完成核心 PlayNextClip 决策树。

### 7.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 3.1 | 状态机（6 状态 + 合法路径验证） | `core/state_machine.go` |
| 3.2 | 事件定义（PlayFinished/StatusChanged/...） | `core/events.go` |
| 3.3 | PlayThread 主 goroutine | `core/play_thread.go` |
| 3.4 | PlayNextClip 决策树 | `core/play_thread.go` |
| 3.5 | 播表导航（FindNext/JumpTo） | `core/play_thread.go` |
| 3.6 | 预卷（Cue）逻辑 + 重试 | `core/play_thread.go` |
| 3.7 | 信号切换协调（FindSignalByFix + SwitchSignal + 去重） | `core/play_thread.go` |
| 3.8 | goroutine 间事件通信 | `core/play_thread.go` |
| 3.9 | PlayNextClip 重入防护（m_in_play_next + 500ms 超时） | `core/play_thread.go` |
| 3.10 | Cue 失败 300ms 延迟注入 PlayFinish 事件 | `core/play_thread.go` |
| 3.11 | 双重试机制（AddClip 失败后 goto agin 模式） | `core/play_thread.go` |
| 3.12 | 单元测试（状态机全路径 + 决策树） | `core/*_test.go` |

### 7.3 关键设计

#### 状态迁移矩阵（20 条合法路径 — 严格对齐 C# SlvcStateController.GetPath()）

```
From＼To   → Stopped  Auto  Manual  Live  RedifDelay  Emergency
Stopped       —       ✅     ✅      ✅      ✅          ✗
Auto          ✅       —      ✅      ✅      ✅          ✅
Manual        ✅      ✅       —      ✅      ✅          ✗
Live          ✗       ✅      ✅       —      ✅          ✗
RedifDelay    ✗       ✅      ✅      ✅       —          ✗
Emergency     ✗       ✅      ✗       ✗       ✗          —
```

**关键约束（源自 C# GetPath() 精确验证）**：
- **Stopped** 可进入 Auto / Manual / Live / RedifDelay，但**不能**直接进入 Emergency
- **Emergency** 只能从 **Auto** 进入（唯一入口），只能退回 **Auto**（唯一出口）
- **Live / RedifDelay** 不能直接回到 **Stopped**（必须先回到 Auto/Manual）
- **Manual** 不能直接进入 **Emergency**

完整路径枚举（对应 C# EPath）：
```
Stop2Auto, Auto2Stop, Auto2Manual, Manual2Auto,
Auto2Emerg, Emerg2Auto,
Auto2Delay, Delay2Auto,
Stop2Manual, Manual2Stop,
Auto2Live, Live2Auto,
Live2Manual, Manual2Live,
Stop2Live,
Live2Delay, Delay2Live,
Stop2Delay, Manual2Delay, Delay2Manual
```

**注意**：Stopped 可进入 Auto / Manual / Live / RedifDelay，但**不能**直接进入 Emergency。

#### 状态机实现

```go
type StateMachine struct {
    mu         sync.RWMutex
    status     models.Status
    lastStatus models.Status
    paths      map[[2]models.Status]PathType
    onChange   func(from, to models.Status, path PathType)
}

// ChangeStatusTo 状态变更唯一入口
func (sm *StateMachine) ChangeStatusTo(target models.Status, reason string) (PathType, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    
    key := [2]models.Status{sm.status, target}
    path, ok := sm.paths[key]
    if !ok {
        return ErrPath, &models.TransitionError{
            From:   sm.status,
            To:     target,
            Reason: fmt.Sprintf("非法路径, 触发原因: %s", reason),
        }
    }
    
    old := sm.status
    sm.lastStatus = sm.status
    sm.status = target
    
    log.Info().
        Str("from", old.String()).
        Str("to", target.String()).
        Str("path", path.String()).
        Str("reason", reason).
        Msg("状态变更")
    
    // 在锁外通知（通过 goroutine 或 channel）
    if sm.onChange != nil {
        go sm.onChange(old, target, path)
    }
    
    return path, nil
}
```

#### PlayNextClip 决策树

```go
// PlayNextClip 决策树（对齐 C# SlvcPlayThread.PlayNextClip）
//
// 1. 检查是否临近硬定时（< HardFixAdvanceMs）→ 不切换，等定时触发
// 2. 查找下一条素材（playlist.FindNext）
// 3. 没有下条 → 开垫乐
// 4. 有下条 → 预卷（Cue）
// 5. 预卷成功 → 播出（Play）
// 6. 预卷失败 → 重试 N 次 → 仍失败则跳过 → 递归 PlayNextClip
func (pt *PlayThread) playNextClip(force bool, taskType models.TaskType) bool {
    // 步骤 1：检查定时
    if !force && pt.fixTimeMgr.IsNearFixTask(pt.cfg.Playback.HardFixAdvanceMs) {
        log.Debug().Msg("临近硬定时，不切换素材")
        return false
    }
    
    // 步骤 2：查找下条
    program := pt.playlist.FindNext(pt.currentPos)
    if program == nil {
        // 步骤 3：没有下条，开垫乐
        pt.blankMgr.Start()
        return false
    }
    
    // 步骤 4-6：预卷 + 播出
    if err := pt.cueAndPlay(program); err != nil {
        log.Warn().Err(err).Str("name", program.Name).Msg("预卷/播出失败，尝试下条")
        pt.currentPos++
        return pt.playNextClip(force, taskType) // 递归尝试下条
    }
    
    return true
}
```

### 7.4 测试重点

- 状态机全 20 条合法路径测试
- 非法路径拒绝测试（Stopped → Emergency 等）
- PlayNextClip 决策树各分支覆盖
- 并发状态变更安全性（`go test -race`）

### 7.5 边界情况（必须覆盖）

- **PlayNextClip 重入**：500ms 超时锁防止递归调用期间被二次触发
- **Cue 失败补偿**：预卷失败后延迟 300ms 注入 `PlayFinish` 事件，确保播出链不中断
- **信号切换时间漂移**：`FindSignalByFix` 需容忍 ±200ms 时间偏差
- **播表失效**：PlayNextClip 执行期间播表被云端更新，需检测版本号变化
- **文件访问竞态**：素材文件可能正在被下载模块写入，Cue 时需检测文件完整性

### 7.6 验收标准

- 状态机通过全部 20 条路径测试 + 非法路径测试
- PlayNextClip 完成播表遍历 + 垫乐启动 + 定时跳过
- 播完事件正确触发 PlayNextClip
- `go test -race ./core/...` 全部通过

---

## 八、Phase 4 — 定时任务与垫乐系统

### 8.1 目标

实现硬定时/软定时任务调度和垫乐自动填充，精度达到 ±50ms。

### 8.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 4.1 | FixTimeManager（20ms Ticker 轮询） | `core/fix_time_manager.go` |
| 4.2 | 硬定时触发（到时间强制切播） | `core/fix_time_manager.go` |
| 4.3 | 软定时触发（等当前素材播完） | `core/fix_time_manager.go` |
| 4.4 | 提前量计算（淡出时间 + 预卷时间） | `core/fix_time_manager.go` |
| 4.5 | BlankManager（垫乐选曲 + 播放控制） | `core/blank_manager.go` |
| 4.6 | 垫乐让位（定时到达时淡出垫乐） | `core/blank_manager.go` |
| 4.7 | AI 选曲（按节目类型匹配垫乐） | `core/blank_manager.go` |
| 4.8 | 垫乐历史去重（LRU 策略 + "从未播放"快速返回） | `core/blank_manager.go` |
| 4.9 | 垫乐三态管理（Prepare/Play/Stop） | `core/blank_manager.go` |
| 4.10 | 软定时协作取消（SoftFixWaiting 标志位） | `core/fix_time_manager.go` |
| 4.11 | EQ 均衡器动态切换（按时间块/节目类型） | `core/play_thread.go` |
| 4.12 | 单元测试 + 精度基准测试 | `core/*_test.go` |

### 8.3 关键设计

#### 定时轮询

```go
// FixTimeManager 定时任务管理器
type FixTimeManager struct {
    mu       sync.Mutex
    tasks    []*FixTimeTask
    ticker   *time.Ticker
    onArrive func(task *FixTimeTask)
}

func (fm *FixTimeManager) Run(ctx context.Context) {
    fm.ticker = time.NewTicker(20 * time.Millisecond) // 20ms 轮询
    defer fm.ticker.Stop()
    
    for {
        select {
        case now := <-fm.ticker.C:
            fm.checkTasks(now)
        case <-ctx.Done():
            return
        }
    }
}

func (fm *FixTimeManager) checkTasks(now time.Time) {
    fm.mu.Lock()
    defer fm.mu.Unlock()
    
    for _, task := range fm.tasks {
        if task.shouldTrigger(now) {
            // 触发任务
            go fm.onArrive(task) // 在新 goroutine 中执行，不阻塞轮询
            task.triggered = true
        }
    }
    
    // 清理已过期的任务
    fm.removeExpired(now)
}
```

#### 垫乐管理

```go
// BlankManager 垫乐管理器
// 当播表无下条素材时自动启动垫乐填充，定时到达时自动让位
type BlankManager struct {
    mu          sync.Mutex
    playing     bool
    history     []string          // 最近播放历史（去重用）
    maxHistory  int               // 历史上限
    blankFiles  []string          // 可用垫乐文件列表
    adapter     *audio.Adapter    // 音频适配器
}

func (bm *BlankManager) Start() {
    bm.mu.Lock()
    defer bm.mu.Unlock()
    
    if bm.playing {
        return
    }
    
    file := bm.selectNext() // 选曲（去重 + AI 匹配）
    if file == "" {
        log.Warn().Msg("无可用垫乐文件")
        return
    }
    
    bm.playing = true
    log.Info().Str("file", file).Msg("垫乐开始")
    // ...
}

// YieldTo 垫乐让位（定时到达时调用）
func (bm *BlankManager) YieldTo(fadeOutMs int) {
    bm.mu.Lock()
    defer bm.mu.Unlock()
    
    if !bm.playing {
        return
    }
    
    bm.playing = false
    log.Info().Int("fade_ms", fadeOutMs).Msg("垫乐让位")
    // 淡出停止...
}
```

### 8.4 边界情况（必须覆盖）

- **Jingle 淡出 vs 硬定时冲突**：Jingle 正在淡出时硬定时到达，需立即强制停止而非等待淡出完成
- **已过期任务静默丢弃**：定时任务加载时已过期（< now）不触发，仅记录日志
- **软定时协作取消**：用户手动切换状态时需清除 SoftFixWaiting 标志
- **垫乐三态**：Prepare→Play→Stop 状态机，避免在 Prepare 阶段被意外触发播放
- **NTP 时间跳变**：间隔/超时用 `time.Since()` 单调时钟；绝对定时触发加 > 5min 过期保护（见规范 20.2 节）

### 8.5 验收标准

- 硬定时触发偏差 95% 在 ±50ms 内（基准测试验证）
- 软定时等当前素材播完后正确触发
- 垫乐在播表空隙自动启动
- 垫乐在定时到达时淡出让位
- 垫乐选曲去重正确（连续 10 首不重复）
- `go test -race ./core/...` 全部通过

---

## 九、Phase 5 — 插播与通道保持

### 9.1 目标

实现定时插播、紧急插播、嵌套插播栈以及通道保持（直播/转播延时）功能。

### 9.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 5.1 | IntercutManager（插播栈管理） | `core/intercut_manager.go` |
| 5.2 | 定时插播（到时间自动触发） | `core/intercut_manager.go` |
| 5.3 | 紧急插播（外部指令触发） | `core/intercut_manager.go` |
| 5.4 | 嵌套插播（插播中再插播） | `core/intercut_manager.go` |
| 5.5 | 插播结束返回（pop 栈顶 + Cut_Return 位置补偿） | `core/intercut_manager.go` |
| 5.6 | 通道保持（Live/RedifDelay） | `core/channel_hold.go` |
| 5.7 | 通道保持超时自动返回 | `core/channel_hold.go` |
| 5.8 | 插播双标记（m_CutPlaying + PlayState.Cut） | `core/intercut_manager.go` |
| 5.9 | Jingle/TempList 生命周期（专用淡出时序） | `core/play_thread.go` |
| 5.10 | 信号切换去重（_LastSwitchTime 防抖） | `core/play_thread.go` |
| 5.11 | 挂起标志（m_bSuspended 暂停/恢复） | `core/play_thread.go` |
| 5.12 | 单元测试 | `core/*_test.go` |

### 9.3 关键设计

#### 插播栈

> **边界情况（必须覆盖）**：
> - **嵌套插播位置丢失**：多层插播返回时，原播出位置可能已过期（被定时任务推进），需检测时间有效性
> - **Cut_Return 补偿**：插播返回后的位置需加上 Cut_Return 常量偏移，避免重叠播出
> - **插播双标记一致性**：m_CutPlaying 和 PlayState.Cut 必须同步设置/清除
> - **通道保持超时精度**：超时判断使用单调时钟，不受 NTP 调时影响

```go
type IntercutManager struct {
    mu       sync.Mutex
    stack    []*IntercutEntry   // 插播栈（后进先出）
    maxDepth int                // 最大嵌套深度
}

type IntercutEntry struct {
    ID       string
    Type     IntercutType       // 定时/紧急
    Programs []models.Program   // 插播素材列表
    ReturnTo *PlaybackSnapshot  // 返回点快照
}

// Push 开始插播（保存当前播出状态 → 切换到插播素材）
func (im *IntercutManager) Push(entry *IntercutEntry) error {
    im.mu.Lock()
    defer im.mu.Unlock()
    
    if len(im.stack) >= im.maxDepth {
        return fmt.Errorf("插播栈已满: depth=%d", im.maxDepth)
    }
    
    im.stack = append(im.stack, entry)
    log.Info().Str("id", entry.ID).Int("depth", len(im.stack)).Msg("插播开始")
    return nil
}

// Pop 插播结束（恢复到返回点）
func (im *IntercutManager) Pop() *PlaybackSnapshot {
    im.mu.Lock()
    defer im.mu.Unlock()
    
    if len(im.stack) == 0 {
        return nil
    }
    
    top := im.stack[len(im.stack)-1]
    im.stack = im.stack[:len(im.stack)-1]
    log.Info().Str("id", top.ID).Int("depth", len(im.stack)).Msg("插播结束，返回")
    return top.ReturnTo
}
```

### 9.4 验收标准

- 定时插播按时间准确触发
- 紧急插播立即打断当前播出
- 嵌套插播（至少 3 层）正确入栈出栈
- 插播结束后正确返回原播出位置
- 通道保持（Live/RedifDelay）正确进入和超时返回
- `go test -race ./core/...` 全部通过

---

## 十、Phase 6 — API 与通信层

### 10.1 目标

实现 REST API、WebSocket 实时推送和 UDP 监听，提供完整的外部控制接口。

### 10.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 6.1 | HTTP 服务器（chi 路由 + 中间件） | `api/server.go` |
| 6.2 | 播出控制 API（play/pause/stop/next/jump） | `api/handlers.go` |
| 6.3 | 状态查询 API（status/progress/playlist） | `api/handlers.go` |
| 6.4 | 插播控制 API（start/stop intercut） | `api/handlers.go` |
| 6.5 | WebSocket 连接管理 + 推送 | `api/ws.go` |
| 6.6 | UDP 监听与命令映射 | `api/udp.go` |
| 6.7 | 中间件（日志/CORS/认证/限流） | `api/middleware.go` |
| 6.8 | 单元测试 + 集成测试 | `api/*_test.go` |

### 10.3 REST API 设计

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/status` | 获取播出状态 |
| GET | `/api/v1/progress` | 获取当前播出进度 |
| GET | `/api/v1/playlist` | 获取当前播表 |
| POST | `/api/v1/control/play` | 播出 |
| POST | `/api/v1/control/pause` | 暂停 |
| POST | `/api/v1/control/stop` | 停止 |
| POST | `/api/v1/control/next` | 下一条 |
| POST | `/api/v1/control/jump` | 跳转到指定位置 |
| POST | `/api/v1/control/status` | 切换播出状态 |
| POST | `/api/v1/intercut/start` | 开始插播 |
| POST | `/api/v1/intercut/stop` | 停止插播 |
| POST | `/api/v1/playlist/load` | 加载播表 |
| ws | `/ws/playback` | WebSocket 实时推送 |

### 10.4 UDP 监听协议

本地紧急控制接口，当 HTTP/WebSocket 层不可用时提供最后的控制手段。

| 参数 | 说明 |
|------|------|
| 监听地址 | `127.0.0.1:18820`（仅本机访问） |
| 协议 | UDP，UTF-8 纯文本命令 |

**命令格式与映射：**

| 命令字符串 | 说明 | 调用边界 |
|-----------|------|---------|
| `stop` | 紧急停止播出 | → `PlayThread.Stop()` |
| `play` | 恢复播出 | → `PlayThread.Play()` |
| `padding` | 切换垫乐 | → `PlayThread.StartBlank()` |
| `status` | 查询播出状态 | 返回 JSON `{"status":"Auto","playing":"..."}` |

```go
// api/udp.go — 结构示意
func StartUDPListener(ctx context.Context, addr string, pt *playthread.PlayThread) error {
    conn, err := net.ListenPacket("udp", addr)
    if err != nil {
        return fmt.Errorf("UDP 监听失败: %w", err)
    }
    go func() {
        defer conn.Close()
        buf := make([]byte, 256)
        for {
            select {
            case <-ctx.Done():
                return
            default:
            }
            conn.SetReadDeadline(time.Now().Add(1 * time.Second))
            n, remoteAddr, err := conn.ReadFrom(buf)
            if err != nil {
                continue
            }
            cmd := strings.TrimSpace(string(buf[:n]))
            resp := handleUDPCommand(cmd, pt)
            conn.WriteTo([]byte(resp), remoteAddr)
        }
    }()
    return nil
}
```

### 10.5 WebSocket 推送事件

```go
type WSEvent struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
    Time time.Time   `json:"time"`
}

// 推送事件类型：
// "status_changed"  — 状态变更
// "play_started"    — 素材开始播出
// "play_finished"   — 素材播完
// "progress"        — 播出进度（1次/秒）
// "level"           — 音频电平（5次/秒）
// "blank_started"   — 垫乐开始
// "intercut_started" — 插播开始
// "error"           — 错误告警
// "heartbeat"       — 心跳
```

### 10.6 验收标准

- 所有 REST API 正常响应，响应时间 < 100ms
- WebSocket 连接稳定，支持 10 个并发客户端
- 状态变更、播出事件通过 WebSocket 实时推送
- UDP 监听正常响应 stop/play/padding/status 四种命令
- API 中间件正常工作（日志/CORS）
- `go test -race ./api/...` 全部通过

---

## 十一、Phase 7 — 云端/中心对接与数据源降级

### 11.1 目标

实现云端/本地中心双数据源管理，支持自动降级和手动回切；实现素材下载和断网暂存。

### 11.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 7.1 | DataSourceManager（双数据源管理） | `infra/datasource.go` |
| 7.2 | 心跳检测 + 自动降级 | `infra/datasource.go` |
| 7.3 | 手动回切 + 校验 | `infra/datasource.go` |
| 7.4 | 素材下载器（断点续传 + MD5 校验） | `infra/file_cache.go` |
| 7.5 | 下载限速（令牌桶） | `infra/file_cache.go` |
| 7.6 | 素材 LRU 清理 | `infra/file_cache.go` |
| 7.7 | 断网暂存（心跳/日志/状态） | `infra/offline_store.go` |
| 7.8 | 联网补传 | `infra/offline_store.go` |
| 7.9 | 播单同步接收（WebSocket 回调） | `api/ws.go` |

### 11.3 关键设计

#### 数据源降级状态机

```
正常（云端）──→ 降级中（本地中心）──→ 待确认（本地中心）──→ 回切（云端）
     ↑                                                    │
     └────────────────────────────────────────────────────┘

降级触发: 连续 N 次心跳失败
回切条件: 连续 N 次心跳成功 + 播单版本一致 + 素材 MD5 通过 + 时钟偏差 < 阈值
回切方式: 运维手动触发（安全第一）
```

#### 令牌桶限速

```go
import "golang.org/x/time/rate"

// 下载限速器（默认 5MB/s）
limiter := rate.NewLimiter(rate.Limit(5*1024*1024), 1024*1024)
```

### 11.4 验收标准

- 云端断开后 30 秒内自动切到本地中心
- 切换过程播出不中断
- 断网期间日志/心跳暂存到本地
- 联网后暂存数据按时间顺序补传
- 素材下载限速正确

---

## 十二、Phase 8 — 构建部署与守护进程

### 12.1 目标

完成双进程构建（主控 + 播放服务）、Windows 服务注册、守护进程和自升级。

> **架构决策**：系统从 Phase 1 开始即采用双进程架构（主控纯 Go + 播放服务 Go+cgo），
> 确保 BASS 崩溃不影响主控进程。本 Phase 完善构建打包、外部守护和自升级。

### 12.2 任务清单

| # | 任务 | 产出文件 |
|---|------|----------|
| 8.1 | 构建脚本（Windows/Linux，双二进制） | `Makefile` |
| 8.2 | Windows 服务实现 | `cmd/playthread/main.go` + `infra/platform/windows.go` |
| 8.3 | systemd 服务文件 | `deploy/playthread.service` |
| 8.4 | 外部守护进程 | `deploy/watchdog.go`（编译为独立二进制） |
| 8.5 | 崩溃日志收集 + 子进程崩溃统计 | `infra/monitor.go` |
| 8.6 | 自升级逻辑（下载新版 + 热替换，主控 + 播放服务） | `infra/updater.go` |
| 8.7 | 单例检查（防止多开） | `cmd/playthread/main.go` |
| 8.8 | pprof 端点（调试用） | `api/server.go` |

### 12.3 守护进程设计

```
第一层守护：操作系统级
  Windows: sc.exe / NSSM 注册 playthread.exe 为 Windows Service，Recovery=Restart
  Linux:   systemd, Restart=always, RestartSec=1s

第二层守护：主控进程内置 ProcessManager
  监控播放服务子进程（audio-service.exe）存活
  子进程崩溃后 < 2s 自动重启，指数退避（首 5 次立即，之后每次 +2s，上限 30s）
  崩溃时保存现场（当前播出位置）并在新子进程中恢复

第三层守护：watchdog（独立二进制）
  监控主控进程存活（每秒检查）
  崩溃后 1 秒内重新拉起
  连续崩溃 5 次 → 等待 30 秒 → 重试
  记录崩溃日志（时间 + 退出码）
```

### 12.4 构建命令

```makefile
# Makefile
.PHONY: build build-release clean

# 开发构建（双二进制）
build:
	CGO_ENABLED=0 go build -o bin/playthread.exe ./cmd/playthread/
	CGO_ENABLED=1 go build -o bin/audio-service.exe ./cmd/audio-service/

# 生产构建
build-release:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o bin/playthread.exe ./cmd/playthread/
	CGO_ENABLED=1 go build -ldflags="-s -w -X main.version=$(VERSION)" -o bin/audio-service.exe ./cmd/audio-service/

build-watchdog:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/watchdog.exe ./deploy/watchdog.go

# Linux 交叉编译（主控可以在任何平台编译，播放服务需要目标平台）
build-linux-master:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/playthread ./cmd/playthread/

clean:
	rm -rf bin/

test:
	go test -race -count=1 ./...

test-bench:
	go test -bench=. -benchmem ./core/... ./bridge/...
```

### 12.5 验收标准

- `make build` 生成 playthread.exe + audio-service.exe + bass.dll 即可运行
- 主控进程启动后自动拉起播放服务子进程
- 播放服务崩溃后 < 2s 自动重启
- Windows 服务注册/启动/停止正常
- 外部守护进程检测到主控退出后 1 秒内拉起
- 连续崩溃保护生效（5 次后等待 30 秒）
- 单例检查生效（第二个实例退出并提示）
- pprof 端点可访问

---

## 十三、Phase 9 — 联调、压测与上线

### 13.1 目标

端到端联调、长时间稳定性测试、故障注入测试。

### 13.2 任务清单

| # | 任务 | 验收标准 |
|---|------|----------|
| 9.1 | 与工作中心前端联调 | 前端可加载播表/控制播出/查看状态 |
| 9.2 | 与云端 SAAS 联调 | 心跳上报/播单同步/素材下载/日志上传 |
| 9.3 | 数据源降级联调 | 拉网线后自动切到本地中心，播出不中断 |
| 9.4 | 168 小时（7 天）稳定性测试 | 内存不增长、定时不漂移、无崩溃、goroutine 不泄漏 |
| 9.5 | 极端烧机测试（可选延长至 30 天） | 模拟真实播表长期连续播出 |
| 9.6 | 故障注入测试 | 随机 kill 进程/断网/磁盘满/声卡拔出，系统自愈 |
| 9.7 | 多平台验证 | Windows + Linux 各跑通联调 |
| 9.8 | 性能基线测量 | 记录 CPU/内存/延迟/goroutine 基线数据 |

### 13.3 稳定性测试项

| 测试项 | 方法 | 期望结果 |
|--------|------|----------|
| 内存泄漏 | 连续播 168h，每 10 分钟记录 Alloc | RSS 增长 < 5% |
| goroutine 泄漏 | 每小时记录 NumGoroutine | 数量稳定（±2 波动） |
| 定时精度 | 记录 1000 个硬定时的触发偏差 | 95% 在 ±50ms 内 |
| 崩溃恢复 | kill -9 主进程 100 次 | 每次 < 5s 恢复播出 |
| 声卡断开 | 拔 USB 声卡，等 10s 插回 | 自动重新初始化 |
| 网络闪断 | 每 30 分钟断网 5 分钟 | 降级正常，补传正确 |
| 磁盘写满 | 磁盘 < 500MB | 停止非关键写入，告警 |
| CGO 内存 | 监控 C 层内存分配 | 无增长趋势 |

---

## 十四、模块依赖关系

```
┌─────────────────────────────────────────────────────────────┐
│                  主控进程 (playthread.exe)                    │
│                     cmd/playthread/                          │
│                          │                                   │
│  ┌──────────┬────────────┼──────────┬──────────┐            │
│  ▼          ▼            ▼          ▼          ▼            │
│ api/      core/       bridge/     db/       infra/          │
│ │          │            │          │          │              │
│ │          ├─ play_thread ──→ bridge.AudioBridge (IPC)      │
│ │          │    │                                            │
│ │          │    ├──→ state_machine                           │
│ │          │    ├──→ fix_time_manager                        │
│ │          │    ├──→ blank_manager                           │
│ │          │    ├──→ intercut_manager                        │
│ │          │    └──→ channel_hold                            │
│ │          │                                                 │
│ │          └─ events ──→ api/ws (推送)                       │
│ │                                                            │
│ ├─ server ──→ core/play_thread (依赖注入)                    │
│ ├─ ws ──→ core.events (订阅)                                 │
│ └─ udp ──→ core/play_thread (UDP 控制)                       │
│                                                              │
│ bridge/ ──→ audio-service (stdin/stdout IPC)                 │
│ db/ ──→ infra/config (连接参数)                              │
│ infra/ ──→ models/* (数据结构)                               │
└──────────────────────────┬──────────────────────────────────┘
                           │ stdin/stdout JSON Line
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                 播放服务子进程 (audio-service.exe)            │
│                   cmd/audio-service/                         │
│                          │                                   │
│  ┌───────────────────────┼──────────────┐                   │
│  ▼                       ▼              ▼                   │
│ audio/              audio/ipc_server  infra/platform        │
│ (cgo+BASS)          (JSON 解析派发)   (BASS 库路径，共享包)  │
└─────────────────────────────────────────────────────────────┘
```

**模块间通信原则**：

| 方向 | 方式 |
|------|------|
| api → core | 直接方法调用（依赖注入） |
| core → bridge | 通过 AudioBridge.Call() 发送 IPC 请求 |
| bridge → audio-service | stdin/stdout + JSON Line 协议 |
| core → infra | 直接方法调用 |
| core → 外部 | 通过事件回调/channel 推送到 api/ws |
| audio-service → bridge | IPC 事件推送（play_finished/device_lost 等） |
| bridge → core | 通过 channel 通知（PlayFinished/ChannelEmpty） |

---

## 十五、关键技术决策

### 15.1 Go vs Python 技术选型

详见 `Go-vs-Python技术选型评估.md`。Go 在 GC 延迟、并发模型、FFI 安全性和部署简便性上全面优于 Python，综合评分 **Go 8.65 vs Python 5.55**。

### 15.2 BASS vs GStreamer

继续使用 BASS。BASS 的命令式 API 与系统架构完美匹配，GStreamer 的 Pipeline 模型需要大量架构重写。迁移成本远大于收益。

### 15.3 进程隔离决策

**Phase 1 起即采用双进程架构，BASS 运行在独立子进程中**：

| 对比项 | Python 方案 | Go 方案（已确定） |
|--------|------------|-------------------|
| BASS 崩溃影响 | 子进程崩溃，主进程存活 | 子进程崩溃，主控存活 |
| 恢复方式 | 重启子进程 | ProcessManager 自动重启子进程 |
| 恢复时间 | < 2s | < 2s（热重启 + 状态恢复） |
| IPC 方式 | multiprocessing Queue | stdin/stdout + JSON Line |
| IPC 延迟 | ~5ms | < 2ms |
| 主进程交叉编译 | 不支持 | CGO_ENABLED=0，任意平台交叉编译 |
| 复杂度 | 高 | 中（Go 子进程比 Python 更轻量） |

**决策理由**：
1. BASS 是闭源 C 库，其内部 segfault 无法被 Go 的 `recover()` 捕获，cgo 崩溃 = 整个进程崩溃
2. 广播系统 7×24×365 运行，进程级隔离是**稳定性第一**原则的唯一正确选择
3. IPC 延迟（< 2ms）相对 20ms 轮询周期可忽略不计
4. 主控进程 CGO_ENABLED=0 可在任意平台交叉编译，只有子进程需要目标平台编译环境

> 与 Python 版的关键差异：Python 版在 Phase 8 才引入 multiprocessing 隔离，Go 版 Phase 1 即为双进程架构。
> Go 的 stdin/stdout JSON Line 协议比 Python multiprocessing.Queue 更轻量、更可控。

### 15.4 定时轮询模型

使用 `time.NewTicker(20ms)` + `select`，Go 的调度器精度优于 Python 的 `Event.wait()`：

- Go Ticker 在 Windows 上精度约 1-15ms（系统定时器分辨率）
- **必须在启动时调用 `timeBeginPeriod(1)`** 提升 Windows 定时器精度到 1ms（见规范 20.1 节）
- 无 GIL 干扰，Ticker 独立 goroutine 不受其他 goroutine 影响

### 15.5 BASS 线程模型（播放服务子进程内部）

BASS 操作走**双通道**（ctrlCh + ioCh）派发到 `runtime.LockOSThread()` 的专用 goroutine：
- **ctrlCh**（控制通道）：Play/Stop/Pause/SetVolume 等低延迟操作，优先处理
- **ioCh**（IO 通道）：StreamCreateFile/StreamFree 等可能耗时操作

> **注意**：双通道提供的是**指令优先级路由**（控制命令不被 IO 命令挤占队列），但单线程 BASS 下单个耗时命令的执行仍会冻结线程。

详见《开发规范》4.3 节。

---

## 十六、风险清单与应对

| # | 风险 | 概率 | 影响 | 应对策略 |
|---|------|------|------|----------|
| R1 | IPC 延迟影响 20ms 定时精度 | 低 | 高 | Phase 1 基准测试验证 IPC 往返 < 5ms；JSON Line 协议轻量 |
| R2 | 播放服务子进程崩溃丢失播出状态 | 中 | 高 | ProcessManager 自动重启 + 崩溃前记录播出位置 + 恢复播出 |
| R3 | cgo 内存泄漏（C.CString/C.malloc）—— 子进程内部 | 中 | 中 | 每个 C.CString 配对 defer C.free，goleak 检测；即使泄漏也只影响子进程 |
| R4 | BASS C 回调中 panic 导致子进程崩溃 | 低 | 中 | 所有 //export 函数首行 defer recover()；崩溃后主控自动重启子进程 |
| R5 | goroutine 泄漏导致长期运行内存增长 | 中 | 高 | goleak 测试 + pprof 监控 + NumGoroutine 告警 |
| R6 | Windows 定时器精度不足（默认 15.6ms） | 高 | 高 | Phase 1 **必须**调用 `timeBeginPeriod(1)` 并验证 |
| R7 | cgo.Handle 泄漏或双重释放（加密文件流）—— 子进程 | 中 | 中 | BassFileUser + sync.Once 封装 Dispose()，单元测试覆盖 |
| R8 | SQLite + cgo 双重 cgo 编译链问题 | 低 | 中 | 主控用纯 Go sqlite（CGO_ENABLED=0），仅子进程用 cgo |
| R9 | 云端接口协议变更 | 中 | 中 | DataSourceClient 接口抽象，协议变更不影响业务 |
| R10 | 跨平台 BASS 行为差异 | 中 | 中 | Phase 1 Windows + Linux 都跑集成测试 |
| R11 | IPC 管道断裂（子进程 stdout 被意外关闭） | 低 | 高 | ProcessManager 检测 EOF 立即重启子进程 + 重放未完成命令 |
| R12 | NTP 时间跳变导致定时任务误触发 | 低 | 高 | 使用单调时钟（monotonic clock）计算间隔，系统时间仅用于绝对比较 |

---

## 十七、测试策略

### 17.1 测试金字塔

```
         /  端到端测试  \           ← Phase 9 联调
        / (10-15 个用例)  \
       /   集成测试         \       ← 每个 Phase 验收
      / (30-50 个用例)        \
     /    单元测试               \   ← 每个包核心逻辑
    / (100-200 个用例)            \
```

### 17.2 各 Phase 测试重点

| Phase | 测试重点 | 测试类型 |
|-------|----------|----------|
| 1 | BASS cgo 绑定、LockOSThread 模型、回调 | 集成测试（需声卡） |
| 2 | 数据模型序列化、配置加载、DB CRUD | 单元测试 |
| 3 | 状态机 20 路径、PlayNextClip 决策树 | 单元 + 竞态 |
| 4 | 定时触发精度、垫乐选曲去重 | 单元 + 基准 |
| 5 | 插播栈嵌套、通道保持返回 | 单元测试 |
| 6 | REST API + WebSocket 推送 | 集成测试 |
| 7 | 降级/回切、断网暂存/补传 | 集成测试 |
| 8 | 构建、服务注册、守护进程 | 系统测试 |
| 9 | 168h 稳定性、故障注入 | 系统测试 |

### 17.3 Mock 策略

```go
// 音频引擎 Mock（不需要声卡的单元测试）
type MockPlayer struct {
    playing    bool
    onFinished func()
}

func (m *MockPlayer) Play() error {
    m.playing = true
    // 模拟播放：设定时间后触发播完回调
    time.AfterFunc(100*time.Millisecond, m.onFinished)
    return nil
}

func (m *MockPlayer) Stop() error {
    m.playing = false
    return nil
}
```

---

## 十八、验收标准

### 18.1 功能验收

| # | 验收项 | 标准 |
|---|--------|------|
| A1 | 自动播出 | 加载日播表后全天候自动播出，定时触发准确 |
| A2 | 垫乐填充 | 节目间隙自动填充垫乐，定时到达时垫乐让位 |
| A3 | 状态切换 | 六状态全部可切换、切换操作无异常 |
| A4 | 插播 | 定时/紧急插播正常触发和返回 |
| A5 | 通道保持 | 转播延时到期后正确返回播出 |
| A6 | 冷启动 | 断电恢复后 5 秒内恢复播出 |
| A7 | 数据源降级 | 云端断开后切到本地中心，播出不中断 |
| A8 | API 可用 | 所有 REST API 正常响应 |
| A9 | WebSocket | 实时推送状态/进度，客户端掉线能重连 |
| A10 | 跨平台 | Windows 和 Linux 均通过全部功能测试 |

### 18.2 非功能验收

| # | 验收项 | 标准 |
|---|--------|------|
| B1 | 稳定性 | 连续运行 168 小时（7 天）无主控崩溃 |
| B2 | 内存 | 7 天运行后主控 + 子进程内存增长 < 5%，goroutine 数量稳定 |
| B3 | 定时精度 | 95% 硬定时在 ±50ms 内触发 |
| B4 | 子进程恢复 | ProcessManager 检测到子进程崩溃后 < 2s 重启并恢复播出 |
| B5 | 主控恢复 | 外部守护进程 1s 内拉起主控，5s 内恢复播出 |
| B6 | API 响应 | 95% 请求 < 100ms |
| B7 | 二进制体积 | playthread.exe + audio-service.exe < 25MB（不含 BASS DLL） |
| B8 | 启动时间 | 冷启动到开始播出 < 3 秒（含子进程拉起） |
| B9 | IPC 延迟 | IPC 往返 < 5ms（p99），不影响 20ms 轮询精度 |

---

> **与 Python 版计划的关键差异**：
>
> 1. **双进程架构（Phase 1 起）**：主控进程（纯 Go，CGO_ENABLED=0）+ 播放服务子进程（Go+cgo/BASS），通过 stdin/stdout JSON Line 协议通信
> 2. **BASS 线程模型（子进程内部）**：LockOSThread + **双通道**（ctrlCh + ioCh）提供指令优先级路由（Go 独有设计，注意单线程下单个耗时命令仍会冻结）
> 3. **无 GIL 问题**：无需关注 asyncio vs threading 分层，统一用 goroutine + channel
> 4. **构建双二进制**：主控 `CGO_ENABLED=0` 可任意平台交叉编译，子进程需目标平台 cgo 编译
> 5. **无 GC 调优**：Go GC STW < 0.5ms，无需 `gc.disable()` / `gc.freeze()` 等技巧
> 6. **竞态检测**：`go test -race` 内置竞态检测，替代 Python 中的人工审查
> 7. **进程级隔离**：BASS 崩溃只影响子进程，主控自动重启子进程并恢复——比 Python multiprocessing 更轻量
> 8. **cgo 回调安全（子进程）**：所有 //export 回调首行 defer recover()，防止 C→Go 回调 panic
> 9. **虚拟通道拓扑**：完整对齐 C# 的 12 通道 + 主备设备 + 自定义声卡路由（非简单"双通道"）
> 10. **Windows 定时器**：启动时必须 `timeBeginPeriod(1)` 保证 20ms Ticker 精度
