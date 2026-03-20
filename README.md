# Playthread-Go 播出控制系统

> C# SlvcPlayThread → Go 全量重构，面向广播电台 7×24 不间断自动播出场景。

## 目录

- [系统概述](#系统概述)
- [架构设计](#架构设计)
  - [双进程架构](#双进程架构)
  - [进程间通信（IPC）](#进程间通信ipc)
  - [并发模型](#并发模型)
- [目录结构](#目录结构)
- [核心业务逻辑](#核心业务逻辑)
  - [播出编排引擎 PlayThread](#播出编排引擎-playthread)
  - [状态机](#状态机)
  - [播出决策树 playNextClip](#播出决策树-playnextclip)
  - [垫片管理器 BlankManager](#垫片管理器-blankmanager)
  - [定时管理器 FixTimeManager](#定时管理器-fixtimemanager)
  - [插播管理器 IntercutManager](#插播管理器-intercutmanager)
  - [通道保持 ChannelHold](#通道保持-channelhold)
- [音频引擎层](#音频引擎层)
  - [BASS 引擎](#bass-引擎)
  - [虚拟通道](#虚拟通道)
  - [录音模块](#录音模块)
- [数据模型](#数据模型)
  - [节目 Program](#节目-program)
  - [播表 Playlist](#播表-playlist)
  - [播出快照 PlaybackSnapshot](#播出快照-playbacksnapshot)
  - [状态枚举](#状态枚举)
- [对外接口](#对外接口)
  - [HTTP API](#http-api)
  - [WebSocket 实时推送](#websocket-实时推送)
  - [UDP 控制](#udp-控制)
  - [事件体系](#事件体系)
- [配置说明](#配置说明)
- [基础设施层](#基础设施层)
- [构建与运行](#构建与运行)
- [依赖项](#依赖项)

---

## 系统概述

Playthread-Go 是一套广播电台播出自动化系统，负责按照编排好的播出表（播表），自动、连续地播放音频节目。系统需要 7×24 小时不间断运行，在各种异常情况下（设备断开、文件缺失、网络中断）都能自动恢复，保证播出不中断。

**核心能力：**

- **自动播出**：按播表顺序自动播放节目，一条播完自动切下一条
- **定时播出**：支持精确到秒的定时任务，到点自动切播（硬定时：强制切；软定时：等当前播完）
- **插播**：支持紧急插播（最高优先级立即中断）和定时插播，多层嵌套，插播结束后恢复原来的播出位置
- **垫片填充**：当出现播出空白时（节目播完、下一条没准备好），自动插入垫片音乐保证不会"开天窗"
- **通道保持**：延时播出时保持当前通道不中断，超时自动恢复
- **录音**：实时录制播出内容为 MP3 文件，按时长自动分割
- **多协议控制**：HTTP API、WebSocket 实时推送、UDP 指令接收，对接上位机和监控系统

---

## 架构设计

### 双进程架构

系统采用**两个独立进程**运行，这是最核心的架构决策：

```
┌──────────────────────────────┐       ┌─────────────────────────────┐
│      playthread.exe          │       │     audio-service.exe       │
│      （主控进程）             │  IPC  │     （播放服务进程）         │
│                              │◄─────►│                             │
│  · 业务编排 (PlayThread)     │ JSON  │  · BASS 音频引擎            │
│  · 状态机                    │ Line  │  · 音频设备管理              │
│  · 定时/插播/垫片管理        │ stdin │  · 文件解码与播放            │
│  · HTTP/WS/UDP 接口          │ stdout│  · 录音与电平检测            │
│  · 数据库/配置/日志          │       │  · 均衡器/淡变              │
│  · 纯 Go（无 CGO 依赖）      │       │  · CGO + BASS 原生库        │
└──────────────────────────────┘       └─────────────────────────────┘
```

**为什么要分两个进程？**

1. **隔离崩溃风险**：BASS 音频库是 C 语言编写的，如果 C 代码崩溃（比如非法内存访问），只会导致 `audio-service.exe` 崩溃，主控进程不受影响，可以自动重启子进程继续播出。
2. **编译解耦**：主控进程 `CGO_ENABLED=0`（纯 Go 编译），不依赖任何 C 编译器，部署更简单。只有音频服务进程需要 CGO（C 编译器 + BASS 库）。
3. **资源隔离**：音频处理是 CPU 密集型任务，独立进程可以独立占用 OS 线程，不影响主控的事件处理。

**子进程生命周期管理**（`bridge/process_manager.go`）：

- 主控启动时自动启动子进程
- 子进程崩溃时自动重启（指数退避：1s → 2s → 4s → ...，最长 30s）
- 有最大重试次数限制，防止无限重启循环
- 子进程的标准错误（stderr）被主控捕获并记录为日志

### 进程间通信（IPC）

两个进程通过**标准输入/输出管道**通信，协议是 JSON Line 格式（每行一个 JSON）：

```
主控 → 子进程（stdin）：发送请求
子进程 → 主控（stdout）：返回响应和事件推送
子进程 → 主控（stderr）：日志输出
```

**三种消息类型：**

| 类型 | 方向 | 说明 |
|------|------|------|
| **请求** (Request) | 主控→子进程 | 主控发起操作指令，包含唯一 ID |
| **响应** (Response) | 子进程→主控 | 子进程返回请求结果，ID 与请求对应 |
| **事件** (Event) | 子进程→主控 | 子进程主动推送（播放完成、设备断开等） |

请求-响应是**一对一**的（通过 ID 匹配），有超时保护（默认 5 秒）。事件是**单向推送**，子进程随时可以发。

**IPC 方法清单（19 个）：**

| 分类 | 方法 | 用途 |
|------|------|------|
| 加载播放 | `load` | 加载音频文件到通道 |
| | `play` | 播放指定通道 |
| | `stop` | 停止指定通道（可淡出） |
| | `pause` | 暂停通道（可淡出） |
| | `resume` | 恢复暂停的通道 |
| | `seek` | 跳转到指定位置 |
| 参数控制 | `set_volume` | 设置音量（可渐变） |
| | `set_eq` | 设置均衡器 |
| | `set_device` | 切换输出设备 |
| 信息查询 | `position` | 查询播放位置 |
| | `level` | 查询音频电平 |
| | `device_info` | 查询设备列表 |
| 通道管理 | `free_channel` | 释放通道 |
| | `free_all` | 释放所有通道 |
| | `remove_sync` | 移除同步回调 |
| 初始化 | `init` | 初始化 BASS 引擎 |
| | `switch_signal` | 切换信号源 |
| | `ping` | 心跳检测 |
| 录音 | `record_start/stop/pause/status` | 录音控制 |

**IPC 事件清单（7 个）：**

| 事件 | 触发时机 |
|------|---------|
| `play_finished` | 一个通道播放完毕 |
| `play_started` | 通道开始播放 |
| `device_lost` | 音频设备断开 |
| `device_restored` | 音频设备恢复 |
| `level` | 音频电平更新 |
| `error` | 子进程出错 |
| `record_progress` | 录音进度更新 |

### 并发模型

Go 语言的并发通过 **goroutine**（轻量级线程）和 **channel**（通道，线程安全的消息传递）实现。可以理解为：goroutine 是一个独立执行的任务，channel 是任务之间传递数据的管道。

系统中关键的 goroutine：

| goroutine | 职责 | 通过什么 channel 通信 |
|-----------|------|---------------------|
| `playbackLoop` | 处理"播放完成"事件，决定下一步 | `playFinishCh` |
| `workLoop` | 处理所有外部指令和定时触发 | `workCh` |
| `emitProgress` | 每秒发送播出进度和倒计时 | 直接调用事件总线 |
| `fixTimeLoop` | 每 20ms 扫描定时任务列表 | `hardTimerCh` / `softTimerCh` |
| `BassEngine.run` | 处理所有 BASS 音频操作（锁定 OS 线程） | `ctrlCh` / `ioCh` |
| `forwardEvents` | 转发子进程推送的事件 | `eventCh` |
| `watchProcess` | 监控子进程存活 | 进程退出信号 |
| `EventBus.run` | 分发内部事件给所有订阅者 | `eventCh` |
| `WSHub.run` | 广播 WebSocket 消息给所有客户端 | `broadcastCh` |

**为什么用 channel 而不是加锁？**

在 Go 中，推荐"通过通信来共享内存"，而不是"通过共享内存来通信"。channel 避免了传统锁带来的死锁风险，每个 goroutine 只处理自己的 channel 消息，逻辑更清晰。

---

## 目录结构

```
playthread-go/
├── cmd/                          # ← 程序入口点
│   ├── playthread/main.go        #    主控进程入口
│   ├── audio-service/main.go     #    音频服务进程入口
│   └── watchdog/main.go          #    守护进程（监控主控是否存活）
│
├── core/                         # ← 核心业务逻辑（纯 Go，不依赖音频库）
│   ├── play_thread.go            #    播出编排引擎（最核心的文件）
│   ├── state_machine.go          #    播出状态机（6 种状态、20 条合法路径）
│   ├── blank_manager.go          #    垫片管理器（防"开天窗"）
│   ├── fix_time_manager.go       #    定时任务管理器（硬定时/软定时）
│   ├── intercut_manager.go       #    插播管理器（紧急/定时插播）
│   ├── channel_hold.go           #    通道保持（延时播出超时管理）
│   ├── events.go                 #    内部事件总线
│   └── *_test.go                 #    单元测试（约 70 个用例）
│
├── audio/                        # ← 音频引擎层（CGO，运行在子进程中）
│   ├── bass_engine.go            #    BASS 引擎包装（独立线程运行）
│   ├── bass_bindings.go          #    BASS C 库的 Go 绑定（CGO 调用）
│   ├── lame_bindings.go          #    LAME MP3 编码器动态加载
│   ├── ipc_server.go             #    IPC 服务端（处理主控的请求）
│   ├── adapter.go                #    通道适配器（管理虚拟通道分配）
│   ├── virtual_channel.go        #    虚拟通道（一个通道 = 一路独立音频）
│   ├── channel_matrix.go         #    通道矩阵（多通道音量分配）
│   ├── recorder.go               #    录音器（PCM → LAME → MP3 文件）
│   ├── level_meter.go            #    音频电平计算（峰值/均方根/分贝）
│   └── libs/                     #    BASS 原生库文件（.dll/.so/.h）
│
├── bridge/                       # ← IPC 桥接层（两个进程之间的通信）
│   ├── protocol.go               #    IPC 协议定义（消息结构、方法名）
│   ├── audio_bridge.go           #    IPC 客户端（主控侧，发请求给子进程）
│   └── process_manager.go        #    子进程管理器（启动/重启/监控）
│
├── api/                          # ← 对外接口层
│   ├── server.go                 #    HTTP 服务器（路由配置）
│   ├── handlers.go               #    API 请求处理函数
│   ├── ws.go                     #    WebSocket 广播 Hub
│   ├── udp.go                    #    UDP 监听器
│   ├── middleware.go             #    中间件（认证/限流/CORS/日志）
│   ├── dashboard.go              #    可视化监控仪表盘页面
│   └── server_test.go            #    API 测试
│
├── models/                       # ← 数据模型定义
│   ├── program.go                #    节目/素材模型
│   ├── playlist.go               #    播表模型（包含时间块展开逻辑）
│   ├── events.go                 #    广播事件类型定义
│   ├── enums.go                  #    枚举常量（状态、衔接模式等）
│   └── device_config.go          #    设备配置模型
│
├── infra/                        # ← 基础设施层
│   ├── config.go                 #    配置文件加载（YAML）
│   ├── logger.go                 #    日志系统（按大小轮转）
│   ├── datasource.go             #    数据源管理器（云端/中心双数据源）
│   ├── file_cache.go             #    文件缓存（LRU 淘汰）
│   ├── offline_store.go          #    离线暂存（断网时存储，恢复后补传）
│   ├── snapshot.go               #    运行状态快照（崩溃恢复用）
│   ├── blank_history.go          #    垫片播出历史（去重用）
│   ├── monitor.go                #    健康监控（内存/协程数）
│   ├── updater.go                #    自更新管理
│   └── platform/                 #    跨平台适配（Windows/Linux）
│
├── db/                           # ← 数据库层
│   ├── sqlite.go                 #    SQLite 连接管理
│   ├── repos.go                  #    数据访问仓库
│   └── migrations.go             #    数据库表迁移
│
├── tests/                        # ← 测试
│   ├── integration_test.go       #    集成测试（API 端点、权限）
│   └── stability_test.go         #    稳定性测试（长跑、压力）
│
├── scripts/                      # ← 运维脚本
│   ├── run.cmd                   #    一键构建运行脚本
│   ├── bootstrap.cmd/.ps1        #    环境初始化脚本
│   └── config.default.yaml       #    默认配置模板
│
├── reference/                    # ← C# 原版源码（参考用，不参与编译）
├── docs/                         # ← 开发文档
├── Makefile                      # ← 构建脚本
└── go.mod                        # ← Go 依赖管理
```

---

## 核心业务逻辑

### 播出编排引擎 PlayThread

`core/play_thread.go` 是整个系统的"大脑"，对应 C# 原版的 `SlvcPlayThread.cs`。它的核心职责：

1. **管理播表**：加载播表、维护当前播出位置、计算下一条播什么
2. **驱动播出**：调用音频桥接层加载和播放音频文件
3. **处理事件**：响应播放完成、定时到达、外部控制指令等
4. **协调子系统**：调度垫片管理器、定时管理器、插播管理器协同工作

**三个核心 goroutine：**

```
┌──────────────┐  播放完成事件  ┌──────────────────────────────────────┐
│ playbackLoop │◄──────────── │ 子进程推送 play_finished 事件           │
│ （最高优先级）│               └──────────────────────────────────────┘
│              │
│ 职责：       │  ·收到"播放完成" → 决定下一步
│              │   - 如果是插播中 → 播插播的下一条，或返回常规播出
│              │   - 如果在等软定时 → 立即切到定时节目
│              │   - 如果是自动播出 → 播下一条
│              │   - 如果是手动/停止 → 不做操作
└──────────────┘

┌──────────────┐  控制指令     ┌──────────────────────────────────────┐
│   workLoop   │◄──────────── │ 外部控制（API / UDP / 定时触发）       │
│ （高优先级）  │               └──────────────────────────────────────┘
│              │
│ 职责：       │  · 状态变更请求（播放/暂停/停止/切换模式）
│              │  · 定时到达通知
│              │  · 插播开始/结束
│              │  · 通道保持开始/取消
└──────────────┘

┌──────────────┐
│ emitProgress │ 每秒发送一次播出进度（当前时间/总时长/倒计时）
└──────────────┘
```

### 状态机

播出系统有 **6 种状态**，状态之间的切换必须走预定义的合法路径：

```
            ┌─────────┐
      ┌────►│ Stopped │◄────┐
      │     └────┬────┘     │
      │          │          │
  ┌───┴──┐  ┌───▼──┐  ┌───┴────┐
  │Manual│◄►│ Auto │◄►│  Live  │
  └───┬──┘  └───┬──┘  └───┬───┘
      │         │          │
      │    ┌────▼────┐     │
      └───►│  Delay  │◄───┘
           └────┬────┘
                │
           ┌────▼──────┐
           │ Emergency │ ← 只能由 Auto 进入，只能回到 Auto
           └───────────┘
```

| 状态 | 含义 | 说明 |
|------|------|------|
| **Stopped** | 停止 | 系统空闲，不播出任何内容 |
| **Auto** | 自动播出 | 按播表顺序自动播放，一条完了自动下一条 |
| **Manual** | 手动播出 | 播完当前条就停，不会自动播下一条 |
| **Live** | 直播 | 切到外部信号源（如直播间话筒），文件播出暂停 |
| **Delay** | 延时播出 | 通道保持状态（通常配合AI延时使用） |
| **Emergency** | 紧急播出 | 紧急插播触发，最高优先级 |

状态机的作用是**防止非法状态跳转**。比如不能从 Stopped 直接跳到 Emergency，不能从 Emergency 跳到 Manual。每次状态变更都会验证路径合法性。

### 播出决策树 playNextClip

`playNextClip` 是播表推进的核心函数，决定"下一条播什么"。它的逻辑对应 C# 的同名方法：

```
playNextClip(是否强制播出)
│
├── 1. 检查 1 秒内是否有硬定时
│   └── 有 → 不播了，等定时到来再切（避免播了一秒又被打断）
│
├── 2. 信号源切换
│   └── 如果下一条是信号源节目 → 切换硬件信号
│
├── 3. 从播表取下一条
│   └── 没有下一条？ → 启动垫片填充（防止"开天窗"）
│
├── 4. 重复触发防护
│   └── 500ms 内同一条不重复播出
│
├── 5. 预卷（提前加载文件到内存）
│   ├── 已经预卷好了？ → 直接用
│   └── 没有？ → 调用子进程加载文件，失败则重试
│
├── 6. 播出
│   ├── 自动模式 → 直接播
│   ├── 强制模式 → 直接播
│   ├── 衔接模式 → 直接播
│   └── 其他 → 停止
│
└── 7. 播出成功后处理
    ├── 更新当前播出位置
    ├── 发送 NextClip1/NextClip2 预告事件
    └── 发送 PlayStarted 事件
```

### 垫片管理器 BlankManager

当播出出现"空白"时（播表播完了、下一条文件有问题），垫片管理器自动插入垫片音乐，**保证播出永不中断**。

**三个状态：**

```
Stopped ──Prepare()──► Prepared ──Play()──► Playing
   ▲                                          │
   └────────────Stop()─────────────────────────┘
```

- **Prepare**：从垫片目录中选一首（支持 AI 智能选曲，根据时间段选择合适的音乐），加载到音频通道
- **Play**：开始播放
- **FadeToNext**：当前垫片播完，淡出并加载下一首

垫片选曲有历史去重功能，避免短时间重复播同一首。

### 定时管理器 FixTimeManager

每 20 毫秒检查一次是否有定时任务到达：

- **硬定时**（TaskHard）：到时间立即强制切播，不管当前正在播什么。提前量 50ms。
- **软定时**（TaskSoft）：如果当前节目快播完了（剩余时间 < 某阈值），就等它播完再切；否则在节目结束时切。

定时任务可以被暂停（比如通道保持期间不检查定时），恢复后继续。

### 插播管理器 IntercutManager

管理插播节目的栈式调度。支持最多 3 层嵌套插播：

```
常规播出  →  插播A 开始  →  插播B 打断插播A  →  插播C 打断插播B
                                                    │
                                              插播C 播完
                                                    │
                                              恢复插播B
                                                    │
                                              插播B 播完
                                                    │
                                              恢复插播A
                                                    │
                                              插播A 播完
                                                    │
                                              恢复常规播出
```

每次插播开始时，保存当前播出状态的**快照**（播到哪一条、播到什么位置、音量多少）。插播结束后，从快照恢复，回到被打断的位置继续播出。

### 通道保持 ChannelHold

用于"延时播出"场景：保持当前通道的音频不中断，设置一个超时时间。超时后自动发送事件，由 PlayThread 决定是否恢复自动播出。

---

## 音频引擎层

### BASS 引擎

BASS 是一个成熟的跨平台音频库（C 语言），支持播放几乎所有音频格式。Go 代码通过 CGO 调用 BASS 的 C API。

**关键设计：BASS 专用 OS 线程**

BASS 的很多操作不是线程安全的，必须在创建 BASS 的同一个线程上调用。Go 的 goroutine 默认会被调度到不同的 OS 线程上。解决方案：

```go
func (e *BassEngine) run() {
    runtime.LockOSThread()  // 锁定当前 goroutine 到当前 OS 线程
    // 之后所有 BASS 调用都在这个线程上执行
    for {
        select {
        case cmd := <-e.ctrlCh:  // 优先处理控制命令
            cmd.execute()
        case cmd := <-e.ioCh:    // 处理 IO 操作（加载文件等）
            cmd.execute()
        }
    }
}
```

所有 BASS 操作都通过 channel 发送给这个专用 goroutine，保证线程安全。`ctrlCh` 优先级高于 `ioCh`，确保控制命令（播放/停止）不会被慢速 IO 操作（加载文件）阻塞。

### 虚拟通道

一个虚拟通道代表一路独立的音频播放。系统使用多个虚拟通道实现平滑切换：

| 通道 | 用途 |
|------|------|
| 主通道 A | 当前正在播出的节目 |
| 主通道 B | 预卷下一条（实现无缝衔接） |
| 垫片通道 | 垫片音乐专用 |
| 插播通道 | 插播节目专用 |

A/B 通道交替使用：A 通道播出时，B 通道预先加载下一条文件。A 播完后立即切到 B，同时 A 开始加载再下一条。这样实现**无缝衔接**（gapless playback）。

### 录音模块

录音流程：

```
BASS 录音设备 ──PCM 数据──► 录音回调函数 ──────► LAME 编码器 ──MP3──► 文件
(48KHz/2ch)               │                   (256kbps)
                          │
                          └──► 电平分析（峰值/RMS/分贝）──► 进度推送
```

- **采样率**：48000 Hz，双声道
- **编码**：LAME MP3，256 kbps
- **文件滚动**：每 3600 秒（1 小时）自动切换到新文件
- **LAME 动态加载**：运行时查找 `libmp3lame.dll`，不需要编译时链接。如果 DLL 不存在，录音功能不可用但不影响其他功能

---

## 数据模型

### 节目 Program

一个 Program 代表播表中的一条节目/素材：

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | 字符串 | 唯一标识 |
| Name | 字符串 | 节目名称 |
| FilePath | 字符串 | 音频文件路径 |
| Duration | 整数(ms) | 总时长 |
| InPoint | 整数(ms) | 入点（从哪里开始播） |
| OutPoint | 整数(ms) | 出点（播到哪里结束） |
| Volume | 小数(0-1) | 音量 |
| FadeIn | 整数(ms) | 淡入时长 |
| FadeOut | 整数(ms) | 淡出时长 |
| FadeMode | 枚举 | 淡变模式（淡入淡出/仅淡入/仅淡出/无） |
| SignalID | 整数 | 信号源 ID（0=文件播出，非0=外部信号） |

### 播表 Playlist

```
播表 Playlist
├── ID: 播表唯一标识
├── Date: 日期
├── Version: 版本号
├── Blocks: 时间块列表
│   ├── TimeBlock[0]
│   │   ├── StartTime: 开始时间
│   │   ├── TaskType: 硬定时/软定时
│   │   └── Programs: [节目1, 节目2, ...]
│   ├── TimeBlock[1]
│   │   └── ...
│   └── ...
└── FlatList: 展开后的扁平列表（运行时生成）
    └── [节目1, 节目2, 节目3, ...] ← 按播出顺序排列
```

`Flatten()` 方法将嵌套的时间块结构展开为一维列表，方便按索引顺序播出。每个节目会被标记它来自哪个时间块、是什么类型的定时任务。

### 播出快照 PlaybackSnapshot

快照用于插播返回时恢复播出状态：

| 字段 | 说明 |
|------|------|
| ProgramIndex | 播到播表的第几条 |
| PositionMs | 播到该条的什么位置（毫秒） |
| Status | 被打断时的播出状态 |
| Volume | 被打断时的音量 |
| IsCutReturn | 标记这是由插播返回产生的（补偿 500ms 位置偏差） |

### 状态枚举

```
Status（播出状态）：
  Stopped    = 0  停止
  Auto       = 1  自动
  Manual     = 2  手动
  Live       = 3  直播
  RedifDelay = 4  延时播出
  Emergency  = 5  紧急

TaskType（定时类型）：
  TaskHard     = 0  硬定时（强制切）
  TaskSoft     = 1  软定时（等播完）
  TaskIntercut = 2  插播

FadeMode（淡变模式）：
  FadeInOut = 0  淡入+淡出
  FadeIn    = 1  仅淡入
  FadeOut   = 2  仅淡出
  FadeNone  = 3  无淡变
```

---

## 对外接口

### HTTP API

所有 API 以 `/api/v1/` 为前缀，使用 JSON 格式。需要在请求头带 `Authorization: Bearer <token>` 进行认证（token 在配置文件中设定，如果配置为空则不启用认证）。

#### 播出控制

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/control/play` | 开始自动播出 |
| POST | `/api/v1/control/pause` | 暂停播出（音频淡出） |
| POST | `/api/v1/control/stop` | 停止播出 |
| POST | `/api/v1/control/next` | 跳到下一条 |
| POST | `/api/v1/control/jump` | 跳到指定位置 `{position: 5}` |
| POST | `/api/v1/control/status` | 切换状态 `{status: "auto", reason: "..."}` |

#### 垫片控制

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/control/blank/start` | 手动启动垫片 |
| POST | `/api/v1/control/blank/stop` | 停止垫片 |

#### 通道保持（延时播出）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/control/delay/start` | 开始通道保持 |
| POST | `/api/v1/control/delay/stop` | 手动结束通道保持 |

#### 插播

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/intercut/start` | 开始插播 `{type: "emergency", programs: [...]}` |
| POST | `/api/v1/intercut/stop` | 结束插播 |

#### 录音

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/record/start` | 开始录音 `{filename: "路径/文件.mp3"}` |
| POST | `/api/v1/record/stop` | 停止录音 |
| POST | `/api/v1/record/pause` | 暂停录音 |
| GET | `/api/v1/record/status` | 查询录音状态 |

#### 查询

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/status` | 获取当前播出状态 |
| GET | `/api/v1/progress` | 获取当前播出进度 |
| GET | `/api/v1/playlist` | 获取当前播表 |
| POST | `/api/v1/playlist/load` | 加载新播表 |

#### 系统诊断（仅限本机访问）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/dashboard` | 可视化监控页面 |
| GET | `/api/v1/infra/system` | 系统信息 |
| GET | `/api/v1/infra/goroutines` | 协程详情 |
| GET | `/api/v1/infra/datasource` | 数据源状态 |
| GET | `/api/v1/infra/monitor` | 健康指标 |

### WebSocket 实时推送

连接地址：`ws://主机:18800/ws/playback`

连接后自动接收所有广播事件，消息格式：
```json
{
  "type": "play_progress",
  "data": {
    "position": 15000,
    "duration": 180000,
    "progress": 8.33,
    "program_name": "新闻联播"
  },
  "time": "2026-03-20T08:30:15Z"
}
```

### UDP 控制

监听地址：`127.0.0.1:18820`（仅本机），接收 JSON 格式控制指令。主要用于对接本地上位机。

### 事件体系

系统内部通过 **EventBus**（事件总线）进行模块间通信。事件产生后，通过 WebSocket 广播给所有已连接的客户端。

| 事件类型 | 推送频率 | 数据内容 |
|---------|---------|---------|
| `status_changed` | 状态变化时 | 新状态、旧状态、原因 |
| `play_started` | 播出开始时 | 节目信息 |
| `play_finished` | 播出结束时 | 节目信息 |
| `play_progress` | 每秒 | 位置、时长、进度百分比 |
| `countdown` | 每秒 | 剩余秒数、总秒数 |
| `audio_level` | 约每 200ms | 左右声道峰值/RMS/分贝 |
| `blank_started` | 垫片开始 | 垫片名称 |
| `blank_stopped` | 垫片停止 | — |
| `intercut_started` | 插播开始 | 插播 ID、类型 |
| `intercut_ended` | 插播结束 | — |
| `next_clip_1` | 播出新条时 | 下一条预告 |
| `next_clip_2` | 播出新条时 | 下下条预告 |
| `record_progress` | 约每 200ms | 录音时长、电平 |
| `device_lost` | 设备断开时 | 设备名 |
| `heartbeat` | 每分钟 | 系统运行状态 |

---

## 配置说明

配置文件为 YAML 格式（默认 `config.yaml`），分为以下几块：

### 播出配置 (playback)

```yaml
playback:
  polling_interval_ms: 20       # 定时轮询间隔（毫秒），越小定时越精确，CPU开销越大
  task_expire_ms: 3000          # 定时任务过期容差（毫秒），过了这个时间的任务直接跳过
  hard_fix_advance_ms: 50       # 硬定时提前量，提前50ms切播，补偿系统延迟
  soft_fix_advance_ms: 0        # 软定时提前量
  cue_retry_max: 3              # 预卷（提前加载文件）失败重试次数
  play_retry_max: 1             # 播放失败重试次数
  snapshot_interval_s: 5        # 状态快照写入周期（秒），用于崩溃恢复
  cut_return_ms: 500            # 插播返回位置补偿（毫秒），恢复时提前500ms避免漏音
  signal_switch_delay_ms: 500   # 信号源切换硬件延迟（毫秒）
```

### 音频配置 (audio)

```yaml
audio:
  sample_rate: 44100        # 采样率 (44100 或 48000)
  device_id: -1             # 输出设备 (-1 = 系统默认设备)
  fade_in_ms: 200           # 默认淡入时长
  fade_out_ms: 200          # 默认淡出时长
```

### 服务器配置 (server)

```yaml
server:
  host: "0.0.0.0"                # 监听地址
  port: 18800                     # HTTP 端口
  ws_path: "/ws/playback"        # WebSocket 路径
  udp_addr: "127.0.0.1:18820"   # UDP 监听地址（仅本机）
  api_token: "your-secret-token" # API 认证 Token（为空则不启用认证）
  allowed_origins: []            # CORS 允许的源（空=全部允许）
  rate_limit_rps: 0              # 每 IP 每秒请求限流（0=不限）
```

### 日志配置 (log)

```yaml
log:
  level: "info"            # 日志级别：debug/info/warn/error
  dir: "./logs"            # 日志目录
  max_size_mb: 100         # 单个日志文件最大 MB
  max_backups: 5           # 保留最大备份数
  max_age_days: 30         # 保留最长天数
  compress: true           # 老日志是否压缩
```

### 其他配置

```yaml
db:
  path: "./data.db"        # SQLite 数据库路径

monitor:
  memory_check_interval_s: 30    # 内存检查间隔
  memory_warn_threshold_mb: 512  # 内存告警阈值
  heartbeat_interval_s: 60       # 心跳间隔

padding:
  directory: "./padding"   # 垫片文件目录
  enable_ai: false         # 是否启用 AI 智能选曲
  history_keep_days: 7     # 播出历史保留天数
```

---

## 基础设施层

### 数据源管理器 (DataSourceManager)

支持从云端和本地中心两个数据源获取播表：

- **定时轮询**：按配置的间隔向数据源请求最新播表
- **变更检测**：通过版本号和三重校验去重（ID + 版本 + 内容哈希），避免重复加载
- **断线自愈**：网络断开时自动使用本地缓存的播表
- **素材预缓存**：收到播表后自动下载需要的音频文件到本地

### 离线暂存 (OfflineStore)

断网时，心跳数据和播出日志不会丢失：

- 暂存到本地 JSON 文件
- 网络恢复后批量补传
- 单飞保护（singleflight）：避免多个协程同时补传

### 状态快照 (SnapshotManager)

每 5 秒保存一次播出状态到文件，用于崩溃恢复：

- 当前播到哪一条、播到什么位置
- 当前状态（自动/手动/等）
- 插播栈状态

如果系统意外重启，从快照恢复后可以继续播出。

### 健康监控 (Monitor)

定时采集运行指标：

- 内存使用量（超过阈值告警）
- Goroutine 数量
- 系统运行时长
- 子进程崩溃次数

可通过 `/dashboard` 页面实时查看。

---

## 构建与运行

### 前置要求

- **Go 1.21+**（推荐 amd64 版本）
- **MinGW-w64 GCC**（用于编译音频服务进程，需要在系统 PATH 中）
- **BASS 库**（`bass.dll` 已包含在 `audio/libs/windows/` 中）
- **LAME 库**（可选，`libmp3lame.dll`，录音功能需要，放在程序目录或 PATH 中）

### 一键构建运行

```bat
scripts\run.cmd
```

这个脚本会：
1. 编译 `playthread.exe`（纯 Go，不需要 GCC）
2. 编译 `audio-service.exe`（CGO，需要 GCC + BASS）
3. 复制 `bass.dll` 到运行目录
4. 创建必要的目录结构
5. 生成默认配置文件
6. 启动服务

### 手动构建

```bat
REM 编译主控进程（纯 Go）
set CGO_ENABLED=0
go build -o bin\playthread.exe ./cmd/playthread/

REM 编译音频服务进程（CGO）
set CGO_ENABLED=1
go build -o bin\audio-service.exe ./cmd/audio-service/

REM 复制 BASS 库到运行目录
copy audio\libs\windows\bass.dll bin\
```

### 运行测试

```bat
set CGO_ENABLED=1
go test -race ./api/... ./core/... ./infra/... ./tests/...
```

`-race` 参数启用竞态检测，确保并发安全。

### 运行后的访问地址

| 服务 | 地址 |
|------|------|
| HTTP API | `http://localhost:18800` |
| 监控仪表盘 | `http://localhost:18800/dashboard` |
| WebSocket | `ws://localhost:18800/ws/playback` |
| UDP | `127.0.0.1:18820` |

---

## 依赖项

| 依赖包 | 用途 |
|--------|------|
| `github.com/go-chi/chi/v5` | HTTP 路由框架 |
| `github.com/gorilla/websocket` | WebSocket 通信 |
| `github.com/rs/zerolog` | 高性能 JSON 日志 |
| `gopkg.in/lumberjack.v2` | 日志文件轮转 |
| `gopkg.in/yaml.v2` | YAML 配置文件解析 |
| `modernc.org/sqlite` | 纯 Go 的 SQLite 驱动（不需要 CGO） |
| `github.com/google/uuid` | UUID 生成（IPC 请求 ID） |
| `golang.org/x/sys` | Windows 系统调用（服务注册等） |
| `golang.org/x/time` | 限流器 |
| `github.com/stretchr/testify` | 测试断言库 |
| **BASS**（外部 C 库） | 音频播放引擎 |
| **LAME**（外部 C 库，可选） | MP3 编码器（录音用） |
