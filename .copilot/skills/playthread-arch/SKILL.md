# Playthread-Go 架构技能

适用于 playthread-go 项目的整体架构理解，涵盖双进程架构、状态机、事件系统、播出编排、定时系统、数据模型和开发规范。

## 触发条件

当任务涉及以下内容时使用本技能：
- PlayThread 播出逻辑（播放、切换、返回）
- 状态机（状态转换、路径验证）
- 事件系统（EventBus 发布/订阅）
- 定时任务（硬定时、软定时、插播）
- 节目单/节目模型
- 垫片/空白管理
- 快照与恢复
- 项目整体架构设计

---

## 1. 整体架构

```
┌──────────────────────────────────────────────────────────┐
│                    主控进程 (playthread)                   │
│  CGO_ENABLED=0  ·  纯 Go  ·  业务逻辑 + API              │
│                                                          │
│  ┌──────────┐  ┌──────────┐  ┌────────────┐             │
│  │PlayThread│  │StateMachine│ │  EventBus  │             │
│  │ 播出编排 │  │  状态机   │  │  事件总线  │             │
│  └────┬─────┘  └──────────┘  └────────────┘             │
│       │                                                  │
│  ┌────┴─────┐  ┌───────────┐  ┌────────────┐           │
│  │FixTimeMgr│  │BlankMgr   │  │IntercutMgr │           │
│  │ 定时管理 │  │ 垫片管理  │  │  插播管理  │           │
│  └──────────┘  └───────────┘  └────────────┘           │
│       │                                                  │
│  ┌────┴──────────────────────┐                          │
│  │     AudioBridge (IPC客户端) │                          │
│  │  stdin/stdout JSON-Line    │                          │
│  └────────────┬───────────────┘                          │
└───────────────┼──────────────────────────────────────────┘
                │ stdin/stdout
┌───────────────┼──────────────────────────────────────────┐
│               ▼                                          │
│  ┌────────────────────────────┐                          │
│  │     IPC Server (协议解析)    │                          │
│  └────────────┬───────────────┘                          │
│               │                                          │
│  ┌────────────┴───────────────┐                          │
│  │  BassEngine (双通道调度)     │  ← runtime.LockOSThread │
│  └────────────┬───────────────┘                          │
│               │                                          │
│  ┌────────────┴───────────────┐                          │
│  │     BASS DLL (C 库)         │                          │
│  └────────────────────────────┘                          │
│                                                          │
│              音频服务进程 (audio-service)                  │
│           CGO_ENABLED=1  ·  Go + cgo/BASS                │
└──────────────────────────────────────────────────────────┘
```

### 1.1 为什么双进程？

1. **BASS → LockOSThread** 影响 Go 调度器
2. **CGO 编译** 需要 mingw-w64 → 主控不需要
3. **故障隔离** → 音频崩溃不影响主控
4. **测试独立** → 主控测试无需音频设备

### 1.2 目录结构

```
cmd/
├── playthread/main.go        # 主控进程入口
└── audio-service/main.go     # 音频服务入口

core/           # 核心播出逻辑（纯 Go）
├── play_thread.go            # PlayThread 播出编排器 (~1200行)
├── state_machine.go          # 6 状态状态机
├── events.go                 # EventBus 事件总线
├── fix_time_manager.go       # 硬/软定时调度
├── blank_manager.go          # 垫片管理
├── intercut_manager.go       # 插播管理（LIFO 栈）
└── channel_hold.go           # 通道保持（单调时钟）

audio/          # 音频引擎（CGO + BASS）
├── bass_bindings.go          # 底层 C 绑定
├── bass_engine.go            # 双通道派发引擎
├── adapter.go                # 业务语义适配器
├── ipc_server.go             # IPC 服务端
├── virtual_channel.go        # 虚拟通道
├── channel_matrix.go         # 通道矩阵
├── level_meter.go            # 音量计
└── recorder.go               # 录音

bridge/         # IPC 通信
├── audio_bridge.go           # IPC 客户端
├── protocol.go               # 协议定义
└── process_manager.go        # 子进程管理

models/         # 数据模型
├── enums.go                  # 状态/类型枚举
├── playlist.go               # 节目单模型
├── program.go                # 节目模型
├── events.go                 # 事件模型
└── device_config.go          # 设备配置

infra/          # 基础设施
├── config.go                 # YAML 配置
├── logger.go                 # zerolog 日志
├── snapshot.go               # 快照持久化
└── platform/                 # 跨平台适配

db/             # 数据库
├── sqlite.go                 # SQLite 连接（单写者模式）
├── migrations.go             # 迁移
└── repos.go                  # 仓库层
```

---

## 2. 状态机

### 2.1 六种状态

| 状态 | 说明 | 典型场景 |
|------|------|----------|
| `Stopped` | 已停止 | 初始状态、手动停止 |
| `Auto` | 自动播出 | 正常按节目单顺序播出 |
| `Manual` | 手动播出 | 操作员手动插入节目 |
| `Live` | 直播 | 切入直播信号源 |
| `RedifDelay` | 延时转播 | 延时直播信号 |
| `Emergency` | 应急 | 紧急插播 |

### 2.2 合法转换路径 (21 条)

```
Stopped → Auto      (启动自动播出)
Stopped → Manual    (启动手动播出)
Stopped → Live      (启动直播)
Stopped → Emergency (启动应急)

Auto → Stopped      (停止)
Auto → Manual       (切手动)
Auto → Live         (切直播)
Auto → Emergency    (切应急)
Auto → Auto         (自动播出内跳转)

Manual → Stopped    (停止)
Manual → Auto       (切自动)
Manual → Live       (切直播)
Manual → Emergency  (切应急)

Live → Stopped      (停止)
Live → Auto         (切自动)
Live → Manual       (切手动)
Live → Emergency    (切应急)

RedifDelay → Stopped
RedifDelay → Auto
RedifDelay → Emergency

Emergency → Stopped (停止应急)
```

### 2.3 特殊规则

- `Stopped → Stopped → Auto`：停止状态下再次停止会切到自动
- `ChangeStatusTo()` 验证路径合法性，非法转换返回错误
- `onChange` 回调在状态变更后同步触发

---

## 3. EventBus 事件系统

### 3.1 事件类型与通道容量

```go
type EventBus struct {
    PlayFinished  chan PlayFinishedEvent   // cap: 16
    ChannelEmpty  chan ChannelEmptyEvent   // cap: 8
    StatusChange  chan StatusChangeCmd     // cap: 8
    FixTimeArrive chan FixTimeEvent        // cap: 8
    IntercutArrive chan IntercutEvent      // cap: 4
    BlankFinished chan struct{}            // cap: 4
    Broadcast     chan BroadcastEvent      // cap: 64
}
```

### 3.2 内部事件流

```
PlayFinished  ──→ playbackLoop ──→ handlePlayFinished ──→ playNextClip
ChannelEmpty  ──→ workLoop     ──→ BlankManager.Takeover
StatusChange  ──→ workLoop     ──→ handleStatusChange
FixTimeArrive ──→ workLoop     ──→ handleFixTimeArrived
IntercutArrive──→ workLoop     ──→ handleIntercutArrived
BlankFinished ──→ workLoop     ──→ BlankManager 状态重置
```

### 3.3 Broadcast 外发事件

所有状态变更、节目切换、错误等都通过 `Broadcast` 通道外发给 API 层的订阅者。

---

## 4. PlayThread 播出编排器

### 4.1 核心字段

```go
type PlayThread struct {
    cfg          *infra.Config
    stateMachine *StateMachine
    eventBus     *EventBus
    audioBridge  *bridge.AudioBridge
    snapshotMgr  *infra.SnapshotManager

    // 子管理器
    fixTimeMgr   *FixTimeManager
    blankMgr     *BlankManager
    intercutMgr  *IntercutManager
    channelHold  *ChannelHold

    // 播出状态（RWMutex 保护）
    mu           sync.RWMutex
    playlist     *models.Playlist
    currentPos   int
    currentProg  *models.Program

    // 原子标志
    inPlayNext    atomic.Bool   // playNext 执行中
    inFixTime     atomic.Bool   // 定时任务处理中
    softFixWaiting atomic.Bool  // 软定时等待中
    cutPlaying    atomic.Bool   // 插播播放中
    suspended     atomic.Bool   // 暂停广播

    // 并发控制
    playNextLock  chan struct{} // cap=1, TryLock 语义
    emrgReturnPos *models.PlaybackSnapshot
    wg            sync.WaitGroup
}
```

### 4.2 主要 goroutine

1. **playbackLoop**：轮询播放位置（20ms），检测播放结束
2. **workLoop**：消费 EventBus 事件，调度各种处理
3. **fixTimeLoop**（在 FixTimeManager 内）：20ms 轮询定时任务到达

### 4.3 playNextClip 决策树

```
playNextClip()
├── TryLock(playNextLock) 失败 → return
├── 检查 inFixTime → 等待
├── 检查 cutPlaying → 处理插播返回
├── 检查 channelHold → 启动通道保持
├── currentPos >= len(FlatList) → 
│   ├── 状态 Auto → ChannelEmpty 事件 → BlankManager
│   └── 其他状态 → Stopped
└── 正常播放下一节目
    ├── cueAndPlayAt(pos)
    ├── 保存快照
    └── 广播事件
```

### 4.4 播出控制方法

| 方法 | 说明 |
|------|------|
| `DelayStart(playlist)` | 加载节目单，延时启动 |
| `EmrgCutStart(programs)` | 紧急插播（保存返回点） |
| `EmrgCutStop()` | 结束紧急插播（恢复返回点） |
| `playNextClip()` | 播放下一节目 |
| `cueAndPlayAt(pos)` | 准备并播放指定位置节目 |
| `returnFromIntercut()` | 从插播返回主节目单 |
| `onReturnAuto()` | 恢复自动播出 |

---

## 5. 定时系统

### 5.1 三种定时类型

| 类型 | 说明 | 行为 |
|------|------|------|
| **Hard** | 硬定时 | 立即中断当前节目，强制切换 |
| **Soft** | 软定时 | 等待当前节目播完再切换 |
| **Intercut** | 定时插播 | 在指定时间插入播放，播完返回 |

### 5.2 FixTimeManager

```go
type FixTimeManager struct {
    mu              sync.Mutex
    fixTasks        []FixTimeTask
    intercutTasks   []IntercutTask
    hardAdvanceMs   int64  // 硬定时提前量
    softAdvanceMs   int64  // 软定时提前量
    intercutAdvanceMs int64
    paused          bool
    onFixTime       func(task FixTimeTask)
    onIntercut      func(task IntercutTask)
}
```

- 20ms 轮询间隔检查任务到达
- 按 StartTime 排序，到达时间 = StartTime - AdvanceMs
- 已触发的任务标记 `Triggered = true`
- 支持暂停/恢复

---

## 6. 插播管理

### 6.1 LIFO 栈模式

```go
type IntercutManager struct {
    mu    sync.Mutex
    stack []IntercutEntry  // LIFO 栈，最大深度 3
}

type IntercutEntry struct {
    ID         string
    Type       IntercutType  // Timed / Emergency
    Programs   []*Program
    ReturnSnap *PlaybackSnapshot
    SectionID  string
    CurrentIdx int
}
```

### 6.2 工作流

```
正常播出 A
    ↓ Push(插播1)
保存 A 的快照 → 播放插播1
    ↓ Push(插播2, 嵌套)
保存插播1快照 → 播放插播2
    ↓ 插播2 播完
Pop() → 恢复插播1快照
    ↓ 插播1 播完  
Pop() → 恢复 A 的快照，继续播出
```

---

## 7. 垫片管理

### 7.1 双列表智能选择

```go
type BlankManager struct {
    mu            sync.Mutex
    state         BlankState  // Stopped → Prepared → Playing
    clips         []*Program  // 常规垫片
    clipsIdl      []*Program  // 环境音乐（短间隙用）
    aiThresholdMs int64       // AI 阈值：< 此值用 clipsIdl
    getPaddingTimeMs func() int64  // 回调：获取距下次任务的毫秒数
}
```

### 7.2 选择逻辑

```
getPaddingTimeMs() 返回距下一任务时间
├── < aiThresholdMs → 使用 clipsIdl（环境音乐，短）
└── >= aiThresholdMs → 使用 clips（常规垫片）
```

---

## 8. 通道保持

### 8.1 单调时钟

```go
type ChannelHold struct {
    mu          sync.Mutex
    active      bool
    data        *ChannelHoldData
    startMono   int64  // 单调时钟起点
    durationMono int64 // 单调时钟时长
    cancel      context.CancelFunc
    onReturn    func()
}
```

使用单调时钟（`time.Now().UnixMono()` 等效）而非墙上时钟，避免 NTP 时间跳变影响。

---

## 9. 数据模型

### 9.1 节目单

```go
type Playlist struct {
    ID       string
    Date     string       // YYYY-MM-DD
    Version  int
    Blocks   []TimeBlock
    FlatList []*Program   // Flatten() 生成的扁平列表
}

type TimeBlock struct {
    ID         string
    Name       string
    StartTime  string      // HH:MM:SS
    EndTime    string
    Programs   []*Program
    TaskType   TaskType    // Hard / Soft
    EQName     string
    Intercuts  []IntercutSection
}
```

`Flatten()` 将嵌套结构展平为线性列表，附加 `BlockIndex` 和 `BlockTaskType` 运行时标注。

### 9.2 节目

```go
type Program struct {
    ID        string
    Name      string
    FilePath  string
    Duration  int64    // 毫秒
    InPoint   int64    // 入点（毫秒）
    OutPoint  int64    // 出点（毫秒）
    Volume    float32  // 0.0 - 1.0
    FadeIn    int64
    FadeOut   int64
    FadeMode  FadeMode
    SignalID  string   // 信号源 ID（直播/转播）
    // ... 更多字段
}
```

---

## 10. IPC 协议

### 10.1 方法清单

| 方法 | 方向 | 说明 |
|------|------|------|
| `init` | → | 初始化音频设备 |
| `load` | → | 加载音频文件到通道 |
| `play` | → | 播放通道 |
| `stop` | → | 停止通道 |
| `pause` | → | 暂停通道 |
| `resume` | → | 恢复通道 |
| `seek` | → | 跳转到指定位置 |
| `set_volume` | → | 设置音量 |
| `set_eq` | → | 设置均衡器 |
| `position` | → | 查询播放位置 |
| `level` | → | 查询音量电平 |
| `device_info` | → | 查询设备信息 |
| `set_device` | → | 切换输出设备 |
| `switch_signal` | → | 切换信号源 |
| `remove_sync` | → | 移除同步回调 |
| `free_channel` | → | 释放通道资源 |
| `ping` | → | 心跳检测 |
| `shutdown` | → | 关闭音频服务 |

### 10.2 事件推送

| 事件 | 方向 | 说明 |
|------|------|------|
| `play_finished` | ← | 播放结束 |
| `play_started` | ← | 播放开始 |
| `device_lost` | ← | 设备丢失 |
| `device_restored` | ← | 设备恢复 |
| `level` | ← | 电平数据 |
| `error` | ← | 错误信息 |

---

## 11. 持久化

### 11.1 快照（原子写入）

```go
type SnapshotManager struct {
    path string
}

// Save：写临时文件 → rename（原子操作）
func (sm *SnapshotManager) Save(info PlayingInfo) error {
    tmpPath := sm.path + ".tmp"
    // 写入 tmpPath
    // os.Rename(tmpPath, sm.path) ← 原子操作
}

// CalcRecoveryPosition：根据持续时间计算恢复位置
// 考虑入点偏移和容差（0ms / 1s / 2s 阈值）
```

### 11.2 SQLite 单写者模式

```go
type DB struct {
    conn    *sql.DB
    writeCh chan writeJob  // 所有写操作序列化
    wg      sync.WaitGroup
}

// WAL 模式 + 单写者 goroutine
// 读操作可并发，写操作通过 writeCh 排队
```

---

## 12. 配置结构

```go
type Config struct {
    Playback PlaybackConfig  // 播出参数
    Audio    AudioConfig     // 音频参数
    Padding  PaddingConfig   // 垫片参数
    Server   ServerConfig    // API 服务
    Monitor  MonitorConfig   // 监控
    Log      LogConfig       // 日志
    DB       DBConfig        // 数据库
}

type PlaybackConfig struct {
    PollingIntervalMs  int   // 位置轮询间隔（默认20ms）
    TaskExpireMs       int   // 任务过期容差
    HardFixAdvanceMs   int   // 硬定时提前量
    SoftFixAdvanceMs   int   // 软定时提前量
    CueRetryMax        int   // Cue 重试次数
}

type AudioConfig struct {
    SampleRate  int     // 采样率
    DeviceID    int     // 输出设备
    FadeInMs    int     // 默认淡入
    FadeOutMs   int     // 默认淡出
    FadeCrossMs int     // 交叉淡变
}
```

---

## 13. 开发规范

### 13.1 构建命令

```bash
# 主控进程
CGO_ENABLED=0 go build -o playthread ./cmd/playthread/

# 音频服务
CGO_ENABLED=1 go build -o audio-service ./cmd/audio-service/

# 核心测试（不需要 CGO）
CGO_ENABLED=0 go test ./core/...

# 竞态检测（需要 gcc）
go test -race ./core/...
```

### 13.2 测试约定

- 使用 `testify` 断言库
- Mock AudioBridge 使用 `io.Pipe()` 模式（不引入接口）
- 测试配置缩短所有时间参数（PollingInterval=10ms, TaskExpire=200ms）
- `drainBroadcast()` 消费事件防止 channel 阻塞
- `waitForStatus()` 轮询+超时代替 sleep

### 13.3 代码组织原则

- `core/` 包不依赖 `audio/`（通过 bridge 间接交互）
- `models/` 包是纯数据结构，不含业务逻辑
- `infra/` 提供基础设施，不依赖业务层
- 每个管理器（BlankMgr, IntercutMgr, FixTimeMgr）独立可测试

### 13.4 并发约定

参见 `go-concurrency` 技能获取详细规范：
- RWMutex 保护读多写少的状态
- atomic 保护独立标志位
- channel 用于事件通信和互斥（TryLock）
- WaitGroup 跟踪所有 goroutine

---

## 14. 9 阶段开发计划

| 阶段 | 内容 | 状态 |
|------|------|------|
| Phase 1 | 双进程架构基础设施 | ✅ 完成 |
| Phase 2 | 数据模型与基础设施 | ✅ 完成 |
| Phase 3 | 状态机 + 核心播出编排 | ✅ 完成 |
| Phase 4 | FixTimeManager + BlankManager | ✅ 完成 |
| Phase 5 | 插播管理 + 通道保持 | ✅ 完成 |
| Phase 6 | API + 通信层 | 🔜 下一步 |
| Phase 7 | 音频引擎完善 | 待定 |
| Phase 8 | 监控与运维 | 待定 |
| Phase 9 | 集成测试与部署 | 待定 |

### 14.1 Git 提交规范

```
<type>: <description>

type:
  fix:    修复
  feat:   新功能
  refactor: 重构
  test:   测试
  docs:   文档

示例:
  fix: 淡出goroutine退出边界 + PlayThread集成测试(12用例)
  feat: Phase 5: 插播管理器 + 通道保持 + 58测试
```
