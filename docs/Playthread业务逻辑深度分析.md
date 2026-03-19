# Playthread 广播播出控制系统 — 深度业务逻辑分析

> 本文档基于对 Playthread 模块全部 7 个源文件（约 4000+ 行 C# 代码）以及 3 个 XMind 架构文档的逐行分析，全面阐述 V8.2 广播播出系统的业务逻辑、架构设计和运行机制。

---

## 目录

- [一、系统全景定位](#一系统全景定位)
  - [1.1 V8.2 广播播出系统整体架构](#11-v82-广播播出系统整体架构)
  - [1.2 Playthread 模块在系统中的位置](#12-playthread-模块在系统中的位置)
  - [1.3 多频率星形部署架构](#13-多频率星形部署架构)
- [二、模块架构与组件关系](#二模块架构与组件关系)
  - [2.1 核心组件关系图](#21-核心组件关系图)
  - [2.2 数据流向](#22-数据流向)
  - [2.3 线程模型](#23-线程模型)
- [三、编排数据模型](#三编排数据模型)
  - [3.1 播表层级结构](#31-播表层级结构)
  - [3.2 定时控件类型](#32-定时控件类型)
  - [3.3 节目与素材模型](#33-节目与素材模型)
  - [3.4 信号控件](#34-信号控件)
- [四、状态机体系](#四状态机体系)
  - [4.1 六种播出状态](#41-六种播出状态)
  - [4.2 状态转换图](#42-状态转换图)
  - [4.3 状态转换详细操作](#43-状态转换详细操作)
- [五、核心播出流程](#五核心播出流程)
  - [5.1 系统启动流程](#51-系统启动流程)
  - [5.2 正常自动播出循环](#52-正常自动播出循环)
  - [5.3 PlayNextClip 核心决策树](#53-playnextclip-核心决策树)
  - [5.4 素材预卷与播出](#54-素材预卷与播出)
- [六、定时任务调度系统](#六定时任务调度系统)
  - [6.1 定时控件触发机制](#61-定时控件触发机制)
  - [6.2 硬定时触发流程](#62-硬定时触发流程)
  - [6.3 软定时触发流程](#63-软定时触发流程)
  - [6.4 插播任务触发流程](#64-插播任务触发流程)
  - [6.5 定时任务初始化](#65-定时任务初始化)
- [七、垫乐填充系统](#七垫乐填充系统)
  - [7.1 垫乐业务场景](#71-垫乐业务场景)
  - [7.2 垫乐选择算法](#72-垫乐选择算法)
  - [7.3 垫乐播出生命周期](#73-垫乐播出生命周期)
  - [7.4 垫乐开启条件](#74-垫乐开启条件)
  - [7.5 垫乐播放历史](#75-垫乐播放历史)
- [八、插播控制系统](#八插播控制系统)
  - [8.1 定时插播流程](#81-定时插播流程)
  - [8.2 嵌套插播处理](#82-嵌套插播处理)
  - [8.3 紧急插播流程](#83-紧急插播流程)
- [九、通道保持(转播延时)系统](#九通道保持转播延时系统)
  - [9.1 通道保持业务场景](#91-通道保持业务场景)
  - [9.2 通道保持启动流程](#92-通道保持启动流程)
  - [9.3 通道保持返回流程](#93-通道保持返回流程)
  - [9.4 AI 智能转播](#94-ai-智能转播)
- [十、信号切换体系](#十信号切换体系)
  - [10.1 信号类型](#101-信号类型)
  - [10.2 切换逻辑](#102-切换逻辑)
  - [10.3 主备机信号处理](#103-主备机信号处理)
- [十一、音频播放适配器](#十一音频播放适配器)
  - [11.1 SlaCardPlayerAdapter 职责](#111-slacardplayeradapter-职责)
  - [11.2 串词(Link Audio)系统](#112-串词link-audio系统)
  - [11.3 歌曲预告(type=17)](#113-歌曲预告type17)
  - [11.4 音频效果器切换](#114-音频效果器切换)
- [十二、多播出源协调](#十二多播出源协调)
- [十三、主备同步机制](#十三主备同步机制)
- [十四、事件系统](#十四事件系统)
- [十五、线程安全设计](#十五线程安全设计)
- [十六、关键业务场景时序](#十六关键业务场景时序)
- [十七、AI 能力矩阵](#十七ai-能力矩阵)
- [十八、架构评估与特点](#十八架构评估与特点)

---

## 一、系统全景定位

### 1.1 V8.2 广播播出系统整体架构

V8.2 广播播出系统是一个完整的广播电台技术平台，包含从素材制作到播出控制的全链路：

```
V8.2 广播播出系统
│
├── 制作系统（制作网）
│   ├── 素材管理（我的素材/公共素材/收藏/回收站）
│   ├── 任务管理（入库任务）
│   ├── 音频编辑（素材调用/录音编辑/导出入库）
│   └── 系统设置（分类/计审/角色/参数）
│
├── 工作中心（播出网）— Vue2/3 + Webpack + Electron
│   ├── 节目管理
│   │   ├── 播出库（频率分库/公共分库）
│   │   ├── 歌曲库（频率分库/公共分库）
│   │   ├── 云节目 / 云歌曲
│   │   └── AI 创作
│   ├── 编排管理
│   │   ├── 模板管理（快速框架引导 + AI串词/AI主持）
│   │   ├── 策略编排 / 最小间隔
│   │   ├── 日编排 / Jingle单
│   │   └── 叠加报时
│   ├── 计划管理
│   │   ├── 栏目计划
│   │   ├── 节目计划（AI资讯：天气/路况/新闻；剧集计划；新闻计划）
│   │   └── 广告计划
│   └── 系统管理（播出设置/频道管理/日志/角色）
│
├── 播出系统（播出网）★ 当前代码所在
│   ├── 前端播控页面
│   └── 播控后台服务（Playthread 模块）
│       └── 多路虚拟通道 → 物理声卡
│           └── 基带信号 + IP信号 同时输出
│
├── 播出服务（底层服务进程）
│   ├── 通信模块（TCP/IP 协议/端口监听/消息队列/超时处理）
│   ├── 播出模块
│   │   ├── 通道管理（初始化/释放播出通道）
│   │   ├── 播放器（预卷/播放/暂停/停止/入出点/音量/进度）
│   │   └── 本地播表（自动播放/缓存）
│   ├── 声卡管理（物理声卡/AOIP声卡）
│   ├── 服务状态（Windows 服务管理）
│   └── 日志管理
│
└── 同步服务
    ├── 内容商店内容同步
    ├── 频率授权信息同步
    └── AI 点位同步 / 增量同步
```

### 1.2 Playthread 模块在系统中的位置

**Playthread = 播出系统的"播控后台服务"核心**

它位于「工作中心」和「播出服务」之间的中间层：

```
工作中心（编排层）
    ↓ 日播单/播表数据
Playthread（调度控制层）★ 本模块
    ↓ AddClip/Next/Pause/Stop 指令
播出服务（音频执行层）
    ↓ 音频信号
声卡硬件 → 发射机/IP流
```

- **上层输入**：工作中心编排好的日播单，通过 `SlaPlaylist` 对象传入，包含全天的定时控件、栏目、信号控件和具体节目素材
- **本层职责**：接收播表后，按定时控件精确调度播出顺序，管理所有播出状态（自动/手动/直播/插播/通道保持等），处理垫乐填充、信号切换、主备同步
- **下层输出**：通过 `VirtualChannelManage`（BASS 音频引擎封装）控制实际音频播放，通过 `ISlvSwitcher` 控制硬件信号切换器

### 1.3 多频率星形部署架构

XMind 显示系统采用**「星形布局，频率文件存储分开部署」**模式：

```
                    ┌──── 频率A 播出工作站（主）
                    │     频率A 播出工作站（备）
中央服务器 ─────────├──── 频率B 播出工作站（主）
（工作中心+数据库）   │     频率B 播出工作站（备）
                    └──── 频率C 播出工作站（主）
                          频率C 播出工作站（备）
```

每个频率：
- 独立的播出库和歌曲库文件存储
- 独立的主备播出工作站
- 独立的信号切换器控制
- 代码中 `ChannelId`、`StationId`、`UtilsData._SelChannel` 反映了多频率隔离设计

工作站角色（代码中的 `GlobalValue.StationName`）：
| 常量 | 角色 |
|------|------|
| `STATION_NAME_RECORD_MASTER` | 录播主机 |
| `STATION_NAME_RECORD_SLAVE` | 录播备机 |
| `STATION_NAME_LIVE_SLAVE` | 直播备机 |

---

## 二、模块架构与组件关系

### 2.1 核心组件关系图

```
SlvcPlayThread (主编排线程 - 约3700行)
│
├─ SlaCardPlayerAdapter (播卡适配器 - 约400行)
│   └── VirtualChannelManage (BASS引擎虚拟通道管理)
│       ├── VirtualChannelPlayStop 事件 → evt_play_finished
│       ├── VirtualChannelEmpty 事件 → evt_channel_empty
│       └── VirtualChannelPlayClipEvent 事件 → EQ效果设置
│
├─ SlaFixTimeTaskManager (定时任务管理器 - 约300行)
│   ├── m_tasklist (定时任务列表)
│   ├── m_intercut_list (插播任务列表)
│   ├── fixThread_Elapsed() (20ms轮询定时任务)
│   ├── intercutThread_Elapsed() (20ms轮询插播任务)
│   ├── FixTimeArrived 事件 → Fix_time_mgr_FixTimeArrived()
│   ├── BeforeFixTimeArrived 事件
│   └── InterCutArrived 事件 → Fix_time_mgr_InterCutArrived()
│
├─ SlaBlankTaskManager (垫乐填充管理器 - 约400行)
│   ├── m_clips (常规垫乐列表)
│   ├── m_clips_idl (轻音乐垫乐列表)
│   ├── _GetOldestClip() (LRU/智能选曲)
│   ├── VirtualChannelManage.FillBlank 通道
│   └── StateChanged / Stopped 事件
│
├─ SlvcStatusController (状态控制器 - 约180行)
│   ├── m_status (当前状态)
│   ├── m_lastStatus (上一状态)
│   ├── m_destStatus (目标状态)
│   ├── ChangeStatusTo() (执行状态切换)
│   └── GetPath() (计算合法迁移路径)
│
├─ SlaBlankPlayInfo (垫乐播放记录 - 单例 - 约130行)
│   ├── BlankPlayHistory (播放记录列表)
│   ├── Save() / Load() (XML序列化)
│   └── GetClipLastPlayTime() (查询最后播放时间)
│
├─ ISlvSwitcher (信号切换器接口)
│   ├── SwitchPgm() (PGM切换)
│   └── SwitchPst() (PST切换)
│
├─ ChannelHoldTask (通道保持任务)
│   ├── Start() / Stop() / EditDelay()
│   └── DoTask 事件
│
└─ SlvcThreadEvent (事件定义 - 约140行)
    └── 12个事件委托 + 7个EventArgs类
```

### 2.2 数据流向

```
工作中心编排
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ SlaPlaylist (播表对象)                                    │
│  ├── m_flatlist (扁平节目列表)                            │
│  ├── FindProgramByFix() / FindProgramWithSection()       │
│  ├── GetNextListPosition() / GetCrntListPosition()       │
│  ├── FindAllFixByTime() / FindAllFixByProgram()          │
│  ├── FindSignalControlByTime() / FindFixByID()           │
│  └── FindSongPreview() / FindAllInterCutSection()        │
└──────────────────────┬──────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────┐
│ SlvcPlayThread (播出调度)                                 │
│  ├── m_crntPostion (正播节目)                             │
│  ├── m_nextPostion (待播节目)                             │
│  ├── m_nextsignalControl (下一个信号切换)                  │
│  ├── m_CutPlaying (是否正在插播)                          │
│  └── m_EmrgCutPostion / m_EmrgRetPostion (紧急插播位置)    │
└──────────────────────┬──────────────────────────────────┘
                       │
              ┌────────┼────────┐
              ▼        ▼        ▼
        SlaCard   SlaBlank   ISlvSwitcher
        Player    Task       (信号切换)
        Adapter   Manager
         │          │
         ▼          ▼
     VirtualChannelManage (BASS引擎)
         │
         ▼
     物理声卡 / AOIP声卡 → 发射机
```

### 2.3 线程模型

```
┌─────────────────────────────────────────────────────────┐
│ PlaybackThread (ThreadPriority.Highest)                   │
│ 职责：响应播完事件，触发播放切换                             │
│ 等待事件：evt_play_finished, evt_exit                     │
│ 关键方法：PlayNextClip(), PlayNextEmrgClip()              │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ WorkThread (ThreadPriority.AboveNormal)                   │
│ 职责：处理状态迁移，预卷下条素材                             │
│ 等待事件：evt_GlobalStateChanged, evt_channel_empty, exit │
│ 关键方法：CueNextProgram(), 状态迁移switch-case            │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ fixThread_Elapsed (Task.Run, async)                       │
│ 职责：20ms精度轮询定时任务列表                              │
│ 触发：FixTimeArrived 事件                                 │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ intercutThread_Elapsed (Task.Run, async)                  │
│ 职责：20ms精度轮询插播任务列表                              │
│ 触发：InterCutArrived 事件                                │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ VirtualChannel 回调线程 (BASS引擎)                         │
│ 触发：PlayStop, ChannelEmpty, PlayClipEvent               │
└─────────────────────────────────────────────────────────┘
```

---

## 三、编排数据模型

### 3.1 播表层级结构

```
日播单 (SlaPlaylist)
├── 时间块 (SlaTimeBlock) — 对应一个播出时段
│   │  例如: 06:00-09:00 早间节目、09:00-12:00 上午节目
│   │
│   ├── 定时控件 (SlaFixControl) — 时间锚点
│   │   │  控制何时开始播出、如何衔接
│   │   ├── ArrangeId (编排ID)
│   │   ├── SetTime (设定时间)
│   │   ├── PlayTime (实际触发时间)
│   │   ├── TimeControlType (定时类型)
│   │   └── timeItem.is_padding (是否允许垫乐)
│   │
│   ├── 栏目控件 (SlaCategoryControl) — 节目栏目容器
│   │   ├── ArrangeId
│   │   ├── InterCut (是否是插播栏目)
│   │   ├── IntCut_BackProgram (被插播的节目引用)
│   │   ├── InterCut_Back (被插播的播放位置)
│   │   └── PlayMode (FixedCutin / RetrodictCutin / ...)
│   │
│   ├── 信号控件 (SlaSignalControl) — 信号源切换点
│   │   ├── Signal_ID (信号源ID)
│   │   ├── AI_Delay (是否AI智能转播)
│   │   ├── AIDelay_Set (AI转播参数)
│   │   └── timeItem.ai_return_close (AI返回是否关闭)
│   │
│   └── 节目 (SlaProgram / TimeItem) — 具体素材
│       ├── ArrangeId (编排ID)
│       ├── Clip (SlaClip - 素材信息)
│       │   ├── FileName / PlayUrl
│       │   ├── FadeMode (淡入淡出模式)
│       │   └── EQEffect (EQ效果)
│       ├── PlayIn / PlayOut (入点/出点)
│       ├── PlayLength (播出时长)
│       ├── PlayState (播出状态)
│       ├── LinkMode (连续/断开)
│       ├── PlayMode (播出模式)
│       ├── InterCut (是否属于插播栏目)
│       ├── timeItem.link_audio (串词音频)
│       ├── timeItem.type (节目类型)
│       ├── timeItem.program (节目详情)
│       │   ├── filename / audit_status
│       │   ├── category_id / top_category_id
│       │   └── dots[] (打点信息, 如"前奏结束")
│       └── PreviewClips (歌曲预告片段, type=17)
```

### 3.2 定时控件类型

| 类型 | 枚举值 | 行为 | 典型场景 |
|------|--------|------|----------|
| **硬定时 (Fixed)** | `TimeType.Fixed` | 精确时间到达时立即淡出当前素材，开始新节目 | 整点新闻、广告时段精确开始 |
| **软定时 (SoftFixed)** | `TimeType.SoftFixed` | 等当前播放素材自然结束后再执行 | 非紧急时段过渡，避免截断节目 |
| **定点前 (FixedBefore)** | `TimeType.FixedBefore` | 从定时点向前倒推计算开播时间 | 需要在某时间点之前播放完毕的节目 |
| **定点后 (FixedAfter)** | `TimeType.FixedAfter` | 定时点之后开始播出 | 延后执行的节目安排 |
| **顺延 (Prolong)** | `TimeType.Prolong` | 接上一条节目播完顺序接续 | 连续播出不需要定时精确控制的内容 |

**定时精度**：
- 轮询周期：20ms（`Task.Delay(TimeSpan.FromMilliseconds(20))`）
- 过期容忍：3000ms（超过3秒的任务自动丢弃）
- 硬定时提前量：50ms（避免广告吃字问题）
- 软定时提前量：0ms（等自然结束）
- 时间基准：`SlvcUtil.GetOriginTime()` 统一处理分割时间点

### 3.3 节目与素材模型

| timeItem.type | 节目类型 | 特殊处理 |
|---------------|----------|----------|
| 7 | 普通节目 | 标准播出流程 |
| 12 | 广告节目 | 效果器切换到广告EQ |
| 13 | 专项节目 | 独立效果器配置 |
| 17 | 歌曲预告 | 多片段合成播出，不记录播放日志 |

**节目播出状态流转 (EPlayState)**：

```
Ready (就绪)
  → Cued (已预卷)
    → Playing (正在播出)
      → Played (已播完)
  → Cut (被插播打断)
  → CueFailed (预卷失败)
  → Skip (被跳过)
```

### 3.4 信号控件

信号控件定义了在播表的某个时间点切换到特定信号源：

```
SlaSignalControl
├── Signal_ID — 信号源标识
├── Name — 显示名称
├── AI_Delay — 是否启用AI智能转播
├── AIDelay_Set — AI转播参数
│   └── check_before — 提前检测时间(ms)
└── timeItem
    ├── type=2 — 信号控件类型标记
    ├── target.name — 目标信号名
    ├── duration — 信号控件有效时长
    └── ai_return_close — AI返回是否关闭(1=关闭)
```

---

## 四、状态机体系

### 4.1 六种播出状态

| 状态 | 枚举 | 含义 | 定时任务 | 垫乐 | 操作人员 |
|------|------|------|----------|------|----------|
| **Stopped** | `EBCStatus.Stopped` | 停播状态 | 停止 | 停止 | 无人 |
| **Auto** | `EBCStatus.Auto` | 自动播出 | 运行 | 允许（受is_padding控制） | 无人值守 |
| **Manual** | `EBCStatus.Manual` | 手动模式 | 暂停 | 手动控制 | DJ 手动操作 |
| **Live** | `EBCStatus.Live` | 直播辅助 | 运行 | 不主动开启 | 主持人直播 |
| **RedifDelay** | `EBCStatus.RedifDelay` | 通道保持/转播延时 | 暂停 | 视情况 | 自动/人工 |
| **Emergency** | `EBCStatus.Emergency` | 紧急插播 | 暂停 | 停止 | 紧急操作 |

### 4.2 状态转换图

```
                    ┌──────────┐
                    │ Stopped  │
                    └────┬─────┘
                  ┌──────┼──────────┐
                  ▼      ▼          ▼
            ┌─────┴──┐ ┌┴──────┐ ┌──┴───────┐
            │  Auto  │ │Manual │ │   Live   │
            └──┬──┬──┘ └───┬───┘ └────┬─────┘
               │  │        │          │
               │  ├────────┤──────────┘
               │  │        │
               │  ▼        ▼
               │ ┌─────────────────┐
               │ │   RedifDelay    │
               │ └─────────────────┘
               │
               ▼
          ┌──────────┐
          │Emergency │ ──→ 只能返回 Auto
          └──────────┘
```

**合法转换路径（EPath枚举定义的所有路径）**：

| 源状态 | 可到达状态 |
|--------|-----------|
| Stopped | Auto, Manual, Live, RedifDelay |
| Auto | Stopped, Manual, Live, Emergency, RedifDelay |
| Manual | Auto, Stopped, Live, RedifDelay |
| Live | Auto, Manual, RedifDelay |
| Emergency | Auto（唯一出路） |
| RedifDelay | Auto, Live, Manual |

### 4.3 状态转换详细操作

#### Stop → Auto (`TryChangeStatus_Stop2Auto`)

```
1. 检查播表是否存在
2. 如果跟随远端 (bFollowRemote=true):
   a. 读取 PlayingInfo (主备同步信息)
   b. 从断点恢复播出 (InitByLastPlayPosition)
3. 否则:
   a. 套入当前时间点 (SeekToCrntPlayPoint)
   b. 定位正播节目
   c. 切换信号源
   d. 预卷并播放
4. 初始化全量定时任务 (_InitFixTimeTask)
5. 恢复切换器状态 (RecoverSwitch)
6. 启动定时任务轮询
```

#### Auto → Manual (`TryChangeStatus_Auto2Manual`)

```
1. 暂停定时任务 (m_fix_time_mgr.Pause())
2. 不停止当前播放（播放器继续运行）
3. 提示 "进入手动状态，请注意返回！"
```

#### Manual → Auto (`TryChangeStatus_Manual2Auto`)

```
1. 重新初始化定时任务 (_InitFixTimeTask)
2. 恢复定时轮询 (m_fix_time_mgr.Start())
3. 如果当前没有在播节目 → 开启垫乐
```

#### Auto → Live (`TryChangeStatus_Auto2Live`)

```
1. 不改变播放状态
2. 定时任务保持运行
（直播辅助模式：定时仍然触发，主持人可以手动操作）
```

#### Auto → Emergency (`TryChangeStatus_Auto2Emerg`)

```
1. 暂停定时任务
2. 停止垫乐
3. 创建紧急信号控件
4. 插入到播表中
5. 主线程接管播出控制
```

#### Auto → RedifDelay (`TryChangeStatus_Auto2Delay`)

```
1. 暂停定时任务
（ChannelHold 对象接管通道保持定时）
```

#### RedifDelay → Auto (`TryChangeStatus_Delay2Auto`)

```
1. 如果是手动取消:
   a. 直接重启定时任务
2. 如果是自动返回:
   a. 停止垫乐和主播单
   b. 淡出 Jingle/临时单
   c. 根据保持信号控件查找返回位置
   d. 如果在当前信号范围内 → 切回本地信号
   e. 如果跨了信号范围 → 切到新信号
   f. 如果有指定接播节目 → 播放指定节目
   g. 否则根据定时控件的 is_padding 决定是否垫乐
   h. 重启定时任务
```

---

## 五、核心播出流程

### 5.1 系统启动流程

```
调用 Start(targetStatus, bFollowRemote, remotePlayingStatus)
│
├── 启动 PlaybackThread (最高优先级线程)
│   └── 进入 PlaybackThread() 无限循环
│
├── 启动 WorkThread (高优先级线程)
│   └── 进入 WorkThread() 无限循环
│
├── 设置目标状态 m_statCtlr.DestStatus
├── 保存启动参数 (m_start_info_bFollowRemote, m_start_info_remotePlayingStatus)
├── 重置通道空闲事件
└── 触发 m_evt_GlobalStateChanged → WorkThread 开始处理状态迁移
```

### 5.2 正常自动播出循环

系统进入自动播出后，由 **3 个核心事件** 驱动循环运转：

```
┌───────────────────────────────────────────────────────┐
│ PlaybackThread 主循环                                   │
│                                                         │
│ while(true) {                                           │
│   if (evt_exit) break;                                  │
│                                                         │
│   HeartbeatService.ReportPlayThreadState(); // 心跳上报  │
│                                                         │
│   if (evt_play_finished) {             // 素材播完       │
│     标记正播节目为已播                                    │
│     switch(当前状态) {                                   │
│       Auto/Live/Manual:                                 │
│         if (!正在执行定时任务)                             │
│           PlayNextClip(false, FinishEvent)               │
│       Emergency:                                        │
│         PlayNextEmrgClip()                               │
│       RedifDelay:                                       │
│         PlayNextClip(false, FinishEvent)                 │
│     }                                                   │
│   }                                                     │
│ }                                                       │
└───────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────┐
│ WorkThread 工作循环                                     │
│                                                         │
│ while(true) {                                           │
│   if (evt_exit) break;                                  │
│                                                         │
│   if (evt_GlobalStateChanged) {        // 状态变更       │
│     lock(m_statCtlr) {                                  │
│       根据 GetPath() 执行对应状态迁移逻辑                  │
│     }                                                   │
│   }                                                     │
│                                                         │
│   if (evt_channel_empty) {             // 通道空闲       │
│     Thread.Sleep(500);                 // 延迟500ms      │
│     switch(当前状态) {                                   │
│       Auto/Manual:                                      │
│         CueNextProgram() // 预卷下条素材                  │
│     }                                                   │
│   }                                                     │
│ }                                                       │
└───────────────────────────────────────────────────────┘
```

### 5.3 PlayNextClip 核心决策树

`PlayNextClip` 是整个系统最复杂的核心方法（约200行），处理所有播出切换场景。完整决策流程：

```
PlayNextClip(bForce, taskType, fixControl=null)
│
├── 设置 m_in_play_next_process = true (锁定状态)
│
├── 处理信号控件切换
│   if (m_nextsignalControl != null && 非通道保持状态)
│     → SwitchSignal() 执行硬件切换
│
├── [Case 1] 待播为空 (m_nextPostion == null)
│   ├── 如果定时事件即将到达(1秒内) → 直接返回等待
│   ├── 定时/手动/插播触发 → 淡出当前播放
│   ├── 播完触发 → 静默暂停
│   ├── 开启垫乐 SetPaddingPlay(true)
│   ├── 清除正播位置
│   └── return true
│
├── [Case 3] 重复请求检查
│   if (距上次播出<500ms && 非垫乐 && 非强制 && 是播完事件)
│     → return false (忽略重复)
│
├── [Case 4] 定时事件即将到达(1秒内)
│   → return false (让定时优先)
│
├── 当前非垫乐状态:
│   ├── 标记正播节目为已播
│   ├── 二次检查待播是否为空 → 为空则垫乐
│   │
│   ├── 检查素材是否已预卷 (最多重试2次)
│   │   ├── NextFileReady() 确认
│   │   └── 否则 DoCueClip() 重新预卷
│   │
│   ├── 素材已预卷:
│   │   ├── 定时/手动/插播任务 → 更新淡出时间
│   │   ├── Auto模式 或 录播工作站 → DoNextClip() 直接播
│   │   ├── 强制播 → DoNextClip()
│   │   ├── LinkMode=Link → DoNextClip()
│   │   ├── LinkMode=断开 + 未开自动垫乐 → 暂停等待
│   │   └── 播出失败 → 重试1次
│   │
│   ├── 播出成功:
│   │   ├── 更新 m_crntPostion (正播位置)
│   │   ├── 标记 PlayState = Playing
│   │   ├── 计算实际播出时间:
│   │   │   ├── 定时任务 → 用定时控件的时间
│   │   │   ├── 插播任务 → 用插播任务的时间
│   │   │   ├── 状态切换 → 减去入点补偿
│   │   │   └── 插播返回 → 减去入点
│   │   ├── 更新播出时间 CheckPlayTime()
│   │   └── 通知 UI: PlayingClipUpdate 事件
│   │
│   └── 播出失败:
│       ├── 标记 CueFailed
│       ├── 延迟300ms触发播完事件(跳下条)
│       └── return false
│
├── _FindNextProgram() 查找下一条待播
└── return true/false
```

### 5.4 素材预卷与播出

**预卷 (DoCueClip)**：
```
DoCueClip(program, playin, playout)
│
├── 空节目检查
├── 文件名为空?
│   ├── type=17 → FindSongPreview() 获取歌曲预告片段
│   └── 其他 → return false
├── 入点不为0 → 设置 FadeMode = FadeIn
├── m_audio_player.AddClip() 提交到播放器
├── 成功 → 标记 Cued, 重置重试计数
├── 失败 → 标记 CueFailed, 重试(最多3次)
└── 记录 ShowPlayIn
```

**播出 (DoNextClip)**：
```
DoNextClip(program, playType)
│
├── 生成唯一 logid
├── m_audio_player.Next(logid) 执行播出
├── 成功:
│   ├── _LogPlayStart() 记录播出日志
│   ├── 更新播出状态
│   └── 异步切换效果器 DoSwitchEffect()
├── 失败:
│   ├── 提示错误信息
│   └── 记录错误日志
└── return 播出结果
```

---

## 六、定时任务调度系统

### 6.1 定时控件触发机制

`SlaFixTimeTaskManager` 维护两个任务列表，各由独立的异步线程以 20ms 精度轮询：

```
┌────────────────────────────────────────────┐
│ fixThread_Elapsed (定时任务线程)              │
│                                              │
│ while(true) {                                │
│   await Task.Delay(20ms);                    │
│   if (pause_flg) continue;                   │
│                                              │
│   if (FirstTask != null) {                   │
│     nowtime = 当前毫秒时间戳;                  │
│                                              │
│     if (nowtime - StartTime > 3000)          │
│       → 移除过期任务                           │
│                                              │
│     if (nowtime + fade_length >= StartTime)  │
│       → Task.Run(() => FixTimeArrived(...))  │
│       → 移除已执行任务                         │
│   }                                          │
│ }                                            │
└────────────────────────────────────────────┘

┌────────────────────────────────────────────┐
│ intercutThread_Elapsed (插播任务线程)         │
│                                              │
│ while(true) {                                │
│   await Task.Delay(20ms);                    │
│   if (pause_flg) continue;                   │
│                                              │
│   if (First_InterTask != null) {             │
│     nowtime = 当前毫秒时间戳;                  │
│                                              │
│     if (nowtime + fade_length >= StartTime)  │
│       → InterCutArrived(...)                  │  ← 注意：插播是同步调用
│       → 移除已执行任务                         │
│                                              │
│     if (nowtime - StartTime > 3000)          │
│       → 移除过期任务                           │
│   }                                          │
│ }                                            │
└────────────────────────────────────────────┘
```

**注意**：定时任务触发使用 `Task.Run()` 异步执行（改修01），而插播任务触发是同步执行。这是因为定时任务涉及复杂的播出切换，需要在独立线程中处理以避免阻塞轮询。

### 6.2 硬定时触发流程

```
Fix_time_mgr_FixTimeArrived(FixTimeArrivedEventArgs args)
│
├── AI智能转播检测中 + 延时状态? → 直接返回
│
├── 等待 PlayNextClip 进程结束 (最多500ms)
├── 设置 s_in_fixtime_task = true (锁定)
│
├── 查找定时节目: m_playlist.FindProgramByFix()
├── 查找信号控件: m_playlist.FindSignalByFix()
│
├── 非Auto模式 + 非Link节目 → 跳过 (program=null)
│
├── 处理插播冲突:
│   if (正在插播中)
│     → 清理插播状态
│     → 将被插播节目标记为已播
│
├── program != null:
│   ├── 软定时:
│   │   ├── 垫乐中 → 停垫乐 → 直接播
│   │   └── 正在播 → 设置SoftFixWaiting → 等播完 → 播
│   └── 硬定时:
│       ├── Jingle正播 → 淡出Jingle
│       ├── 临时单正播 → 淡出临时单
│       ├── 停垫乐
│       ├── 等待淡出完成 (delay循环)
│       └── PlayNextClip(true, FixTime)
│
├── program == null (定时后无节目):
│   ├── 软定时 → 等播完
│   └── 硬定时 → 淡出 → 停播 → PlayNextClip(true, FixTime, null)
│
├── 异常处理: goto agin 重试(最多5次)
└── s_in_fixtime_task = false (解锁)
```

### 6.3 软定时触发流程

软定时的核心区别在于等待当前素材播完：

```
if (SoftFixed) {
  if (垫乐中) {
    停止垫乐;
    直接播下一条;
  } else {
    GlobalValue.SoftFixWaiting = true;
    while (播放器正在播) {
      if (!SoftFixWaiting) return;  // 被外部取消
      Thread.Sleep(20);
    }
    GlobalValue.SoftFixWaiting = false;
    播放定时节目;
  }
}
```

**软定时取消条件**：
- 播放 Jingle 单时取消当前软定时
- 播放临时单时取消当前软定时
- 后续新的定时任务到达时取消之前的软定时
- 用户手动立即播放时退出软定时等待

### 6.4 插播任务触发流程

```
Fix_time_mgr_InterCutArrived(InterCutArrivedEventArgs args)
│
├── 定时事件即将到达(1秒内)? → 放弃插播
│
├── 等待 PlayNextClip 进程结束 (最多500ms)
├── 设置 s_in_fixtime_task = true
│
├── 查找插播节目: m_playlist.FindProgramWithSection()
├── 查找栏目控件: m_playlist.m_flatlist.Find()
│
├── 设置栏目为插播模式 (PlayMode = FixedCutin)
├── 记录被插播位置:
│   ├── InterCut_Back = 当前播放位置 - 插播返回补偿时间
│   ├── 垫乐中 → IntCut_BackProgram = null (返回垫乐)
│   ├── 主播单没在播 → IntCut_BackProgram = null
│   ├── 被插播节目也是插播栏目 → 传递上层返回信息
│   └── 正常播出 → IntCut_BackProgram = 当前节目克隆
│
├── 停止垫乐
├── 预卷插播节目 DoCueClip()
├── PlayNextClip(true, InterCut)
│   → 成功: m_CutPlaying = true
│   → 将被插播节目标记为 Cut 状态
│
├── 异常: goto agin 重试(最多5次)
└── s_in_fixtime_task = false
```

### 6.5 定时任务初始化

系统在多个时机初始化定时任务：

1. **首次启动 (Stop→Auto)**：根据当前时间找到之后的所有定时控件
2. **手动返回自动 (Manual→Auto)**：根据当前时间重新初始化
3. **通道保持返回 (Delay→Auto)**：重新初始化
4. **刷单 (UpdateFixTask)**：单个定时任务的更新或移除

```
_InitFixTimeTask(时间点或正播节目)
│
├── 从播表查找所有后续定时控件
│   FindAllFixByProgram() 或 FindAllFixByTime()
├── 清空当前任务列表
├── 过滤已过期的任务 (StartTime < nowtime)
├── 添加到 m_tasklist
├── 初始化插播任务列表 _InitInterCutTask()
│   └── FindAllInterCutSection() → 过滤当前时间之后的
├── 非 RedifDelay/Manual 状态 → 启动轮询 Start()
```

---

## 七、垫乐填充系统

### 7.1 垫乐业务场景

垫乐（也叫"填充音乐"、"Padding"）相当于广播电台的"背景音乐"，在以下场景启用：

- 栏目之间的空隙时段
- 定时控件设置了允许垫乐但没有编排节目
- 所有节目播完等待下一个定时
- 主持人直播中需要背景音乐
- 手动触发垫乐填充

### 7.2 垫乐选择算法

`_GetOldestClip()` 实现了LRU与智能选择的双模式：

```
_GetOldestClip()
│
├── 智能垫乐模式 (EnableAIPadding = true):
│   │
│   ├── 计算垫乐时长: CallbackBeforePadding()
│   │   └── GetPaddingTime() → m_playlist.GetPaddingTime(当前时间)
│   │
│   ├── padding_time < AIPaddingTime (短间隙):
│   │   └── 从 m_clips_idl (轻音乐列表) 随机选一首
│   │       → Random.Next(0, count) 随机索引
│   │
│   └── padding_time >= AIPaddingTime (长间隙):
│       └── 从 m_clips (主垫乐列表) 选最久没播过的
│           → LRU 算法（见下方）
│
└── 常规模式 (EnableAIPadding = false):
    └── 从 m_clips 选最久没播过的
        │
        ├── 遍历所有垫乐素材
        ├── 查询 SlaBlankPlayInfo.GetClipLastPlayTime()
        ├── 找到播放时间最早的那首
        ├── 如果某首从未播过 → 直接选它 (break)
        └── 返回最久未播的素材
```

**智能垫乐的意义**：
- 短间隙（如两条广告之间1-2分钟）→ 播轻柔的纯音乐，不喧宾夺主
- 长间隙（如栏目间15分钟）→ 播正式的垫乐曲目，保持电台风格

### 7.3 垫乐播出生命周期

```
┌──────────┐    ┌──────────┐    ┌──────────┐
│ Prepare  │ → │   Play   │ → │   Stop   │
│ 预卷首曲  │    │ 开始播出  │    │ 淡出停止  │
└──────────┘    └────┬─────┘    └──────────┘
                     │
                     ▼
              ┌─────────────┐
              │ 自动连续播放  │ ← VirtualChannelPlayStop 事件
              │ AddNextClip  │
              │ + Next()     │
              └─────────────┘
```

**Prepare(mode)**：
- 调用 `_GetOldestClip()` 选曲
- 通过 `DoAddClip()` 构建 PlayClip 提交到 FillBlank 虚拟通道
- 设置文件路径（本地缓存优先，否则远程URL）
- 配置淡入淡出参数

**Play()**：
- 设置 `Enabled = true`
- 生成唯一 logid
- 调用 `_vChannelManage.Next()` 开始播放
- 设置 `PlayingStatus = BlankPadding`
- 记录播出日志
- 添加播放历史 `SlaBlankPlayInfo.AddBlankPlayInfo()`
- 触发 `StateChanged` 事件

**自动连续**：
- BASS 引擎播完当前曲目时触发 `VirtualChannelPlayStop` 事件
- 如果仍为 Enabled 状态 → `AddNextClip()` → `Next()`
- 继续播下一首，实现无缝循环

**Stop()**：
- 设置 `Enabled = false`
- 淡出暂停（`_vChannelManage.Pause(FadeOutTime)`）
- 清空 `m_crnt_clip`
- 恢复 `PlayingStatus = Paused`
- 触发 `StateChanged` 和 `Stopped` 事件

### 7.4 垫乐开启条件

`SetPaddingPlay(true)` 中的完整判断逻辑：

```
SetPaddingPlay(true):
│
├── 当前必须是 Auto 模式
│
├── 查找最近的定时控件:
│   fix = m_playlist.FindNextFixByTime(nowtime)
│
├── fix != null:
│   ├── is_padding == 1 (编排允许垫乐):
│   │   ├── 淡出当前播放 FadePause()
│   │   ├── 通知停止 Jingle 播放
│   │   └── 如果垫乐未启用 → Prepare() + Play()
│   └── is_padding != 1 (不允许垫乐):
│       └── 如果垫乐正在播 → Stop()
│
└── fix == null (当天日播单已播完):
    └── 仅淡出，不垫乐
```

### 7.5 垫乐播放历史

`SlaBlankPlayInfo` 单例负责持久化垫乐的播放记录：

```
SlaBlankPlayInfo (Singleton)
│
├── BlankPlayHistory: List<BlankHistoryItem>
│   └── BlankHistoryItem { ClipId, RealPlayTime, RealPlayLength }
│
├── AddBlankPlayInfo(clipId, playTime, playLength)
│   → 添加记录 + 立即保存
│
├── Save()
│   → XML序列化到 PlayHistory/BlankPadding.his
│   → FileMode.Create，WriteThrough 确保写入
│
├── Load()
│   → XML反序列化
│   → 自动清理2天前的记录 (DeleteItemBeforeDays(2))
│
└── GetClipLastPlayTime(clipId, out DateTime t)
    → 从后向前遍历，找最近一次播放时间
    → 未找到返回 2000-01-01 (确保首次被选中)
```

---

## 八、插播控制系统

### 8.1 定时插播流程

定时插播用于在指定时间自动打断当前节目，播出广告或特定栏目：

```
编排阶段:
  工作中心为某栏目设置 InterCut = true + 定时触发时间

运行阶段:
  _InitInterCutTask() 初始化:
    → FindAllInterCutSection() 查找所有插播栏目
    → 过滤当前时间之后的
    → 添加到 m_intercut_list

  intercutThread_Elapsed() 轮询:
    → 时间到达 → InterCutArrived 事件

  Fix_time_mgr_InterCutArrived() 处理:
    → 记录被插播信息
    → 播放插播内容
    → 插播完毕 → 从被插播位置恢复
```

### 8.2 嵌套插播处理

代码处理了"插播中被再次插播"的嵌套场景：

```
场景: 正在播节目A → 被插播栏目X打断播广告 → 广告播到一半被定时控件打断

代码处理:
  if (当前正在插播 m_CutPlaying) {
    // 判断被插播的节目是否也在插播栏目内
    SlaTimeItem timeItem = FindTimeItem(CrntPostion.parent_id);
    if (timeItem is SlaCategoryControl) {
      if (!timeItem.InterCut) {
        // 非嵌套插播，记录当前节目为返回目标
        IntCut_BackProgram = CrntPostion.Clone();
      } else {
        // 嵌套插播！传递上层的返回信息
        IntCut_BackProgram = timeItem.IntCut_BackProgram;  // 继承原始被插播节目
        InterCut_Back = timeItem.InterCut_Back;            // 继承原始播放位置
      }
    }
  }
```

**插播返回时的位置补偿**：
```
InterCut_Back = m_audio_player.CrntPosition - GlobalValue.Cut_Return
```
`Cut_Return` 是一个配置的补偿时间，确保返回时有少许重叠，避免听感上出现内容跳跃。

### 8.3 紧急插播流程

紧急插播是一种特殊模式，将系统切入 Emergency 状态：

```
EmrgCutStart (未在当前代码中，但逻辑可推断):
│
├── 记录被插播位置 m_EmrgCutPostion
│   ├── PlayTime = 当前时间
│   ├── Program = 正播节目
│   ├── PlayIn = 当前播放位置
│   └── Signal = 紧急信号源
│
├── 记录返回位置 m_EmrgRetPostion
├── 记录当前状态 m_emrgStatus
├── 切换信号源到紧急信号
├── 暂停当前播放
└── ChangeStatus(Emergency)

EmrgCutStop(programid):
│
├── 标记正播为已播
├── 创建返回信号控件
│   → 名称: "[紧急转播]xxx"
│   → 插入到播表
├── 记录紧急插播历史 (EmrgHistoryItem)
├── 调用 PlayProgramNow(返回节目)
└── ChangeStatus(Auto)
```

---

## 九、通道保持(转播延时)系统

### 9.1 通道保持业务场景

通道保持是广播电台的常用功能，主要场景：

1. **转播央广/省台新闻**：在指定时段切到外部转播信号，保持通道直到转播结束
2. **体育赛事转播**：比赛时间不确定，需要AI智能检测结束
3. **重大活动直播**：保持信号通道，备好返回方案

### 9.2 通道保持启动流程

```
DelayStart(delay_data)
│
├── 状态检查:
│   ├── Stopped → 拒绝 ("播出已停止")
│   └── Emergency → 拒绝 ("紧急插播时无法启动通道保持")
│
├── 时间检查:
│   └── 返回时间 < 当前时间 → 拒绝
│
├── 记录当前状态 m_emrgStatus = Status
├── ChannelHold.Start(delay_data)
│   ├── 设置返回时间
│   ├── 设置接播节目 (Program_ID/Program_Name)
│   ├── 设置信号源 (Signal_ID)
│   └── 设置是否AI转播 (Is_AIDelay)
│
├── ChannelHold.DoTask += _ChannelHold_Back (注册返回回调)
├── ChangeStatus(RedifDelay)
└── 记录操作日志
```

### 9.3 通道保持返回流程

```
_ChannelHold_Back (自动返回时间到达)
│
└── ChangeStatus(Auto)
    └── TryChangeStatus_Delay2Auto()
        │
        ├── 手动取消 (Manual_Cancel = true):
        │   ├── 重新初始化定时任务
        │   ├── 启动定时轮询
        │   └── 不改变当前播出内容
        │
        └── 自动返回:
            ├── 停止垫乐 SetPaddingPlay(false)
            ├── 停止主播单 FadePause(100)
            ├── 停止Jingle StopJinglePlayer(淡出)
            │
            ├── 查找通道保持信号控件:
            │   _hold = FindSignalControlByArrangeId(保持时的ArrangeId)
            │   _near_signal = FindSignalControlByTime(当前时间)
            │
            ├── 信号控件判断:
            │   if (_hold == _near_signal)
            │     → 在当前信号范围内 → _SwitchLocal() 切回本地
            │     → 从保持信号后找第一条节目
            │   else
            │     → 跨了信号范围 → 切到新信号
            │     → 从新信号后找节目
            │
            ├── 有指定接播节目?
            │   → FindProgram(Program_ID)
            │
            ├── 找到接播节目:
            │   → PlayNextClip(false, StatusChanged)
            │
            ├── 未找到接播节目:
            │   ├── Auto模式 → 检查定时控件的is_padding
            │   │   → is_padding=1 → 开启垫乐
            │   └── 切换信号（如果有）
            │
            └── 重启定时任务 + Start()
```

### 9.4 AI 智能转播

当信号控件标记了 `AI_Delay = true` 时，系统会启动 AI 终场检测：

```
SwitchSignal() 中的 AI 逻辑:
│
├── signal.AI_Delay = true?
├── signal.timeItem.ai_return_close != 1? (AI返回未被关闭)
│
├── 计算有效时间窗口:
│   delay_end_time = PlayTime + duration
│   now 在 [PlayTime, delay_end_time] 范围内?
│
├── 异步启动检测:
│   Task.Factory.StartNew(async () => {
│     // 等待上一个转播状态结束
│     while (Status == RedifDelay) {
│       await Task.Delay(50);  // 最长2秒
│     }
│     AIEndingDetector.Instance.StartDetect(signal);
│   })
│
└── AIEndingDetector 检测到节目结束:
    → 触发通道保持返回
```

**AI 智能转播的典型场景**：转播足球比赛，比赛结束时间不确定，AI 通过音频特征（如终场哨声、主持人总结语等）识别比赛结束，自动切回本地播出。

---

## 十、信号切换体系

### 10.1 信号类型

| 类型 | type值 | 说明 | 代码中的处理 |
|------|--------|------|------------|
| 录播 (Recorded) | 1 | 播出本地音频文件 | 正常播出流程 |
| 直播 (Live) | 2 | 播出现场直播信号 | 效果器切到直播效果 |
| 转播 (Relay) | 3 | 播出外部转播信号 | 效果器切到转播效果，可能触发AI转播 |

### 10.2 切换逻辑

```
SwitchSignal(signalid, signalname, type, signal)
│
├── 停止AI检测 (AIEndingDetector.StopDetect)
├── Thread.Sleep(500) 防止切换跳变
│
├── AI智能转播检查:
│   if (signal.AI_Delay && 在有效时间窗口内)
│     → 异步启动 AIEndingDetector
│
└── DoAsyncSwitch() → Task.Run(() => _DoSwitch())
    │
    ├── 查找信号源详情 (type/name)
    ├── PlayHistory 记录切换历史
    ├── 异步切换效果器
    │
    ├── 切换器未定义 → 仅记录，不执行
    │
    ├── 备机特殊处理:
    │   ├── LIVE_SLAVE + 录播信号 → 切 LocalSignalId
    │   └── RECORD_SLAVE + 录播信号 → 切 LocalSignalId
    │
    ├── 切换器延时等待 (DelayInMs)
    │
    └── 执行切换:
        ├── m_switcher.SwitchPgm(signalId)
        ├── 结果处理:
        │   ├── 0 = 主备都失败
        │   ├── 1 = 成功
        │   ├── 2 = 主失败备成功
        │   └── 3 = 主成功备失败
        ├── 失败 → 提示错误
        └── 成功 + SyncSwitchPgmPst → SwitchPst(同步预监)
```

### 10.3 主备机信号处理

```
主机 (RECORD_MASTER / LIVE_MASTER):
  → 正常切换到指定信号源

备机 (LIVE_SLAVE):
  → 直播信号: 正常切到直播通道（备机有独立直播输入）
  → 录播信号: 切到 LocalSignalId（本地回放，不抢主机通道）

备机 (RECORD_SLAVE):
  → 录播信号: 切到 LocalSignalId
```

---

## 十一、音频播放适配器

### 11.1 SlaCardPlayerAdapter 职责

`SlaCardPlayerAdapter` 封装了 BASS 音频引擎的 `VirtualChannelManage`，提供统一的播放控制接口：

```
SlaCardPlayerAdapter
│
├── Init(channelName, bassController)
│   └── 注册3个BASS事件回调
│
├── AddClip(program, playin, playout)
│   ├── 构建 PlayClip 对象:
│   │   ├── name, filename (本地缓存优先)
│   │   ├── playin, playout (入出点)
│   │   ├── fadeintime, fadeoutime, fadecrosstime
│   │   ├── fadetype (FadeIn/Out/InOut/None)
│   │   ├── userdata = program (回传引用)
│   │   └── logid (唯一标识)
│   │
│   ├── 下载管理:
│   │   ├── JSON波形文件: 异步下载到 cachepath/playfile/
│   │   └── 音频文件: 本地无 → 用远程URL + 异步下载
│   │
│   ├── 串词处理 (见11.2)
│   ├── 歌曲预告处理 (见11.3)
│   └── bassController.AddClip() 提交
│
├── Next(logid) → 切到下条素材播出
├── Play()      → 开始播放
├── Pause(ms)   → 淡出暂停
├── Stop()      → 立即停止
├── FadePause(ms) → 带淡出的暂停
│
├── CrntPosition → 当前播放位置(ms)
├── State → 播出状态 (Playing/Paused/Stopped)
├── BassState → BASS引擎底层状态
├── NextFileReady(name) → 检查预卷状态
└── UpdateCrntClipFadeOut(ms) → 更新淡出时间
```

### 11.2 串词(Link Audio)系统

串词是广播电台的特色功能：DJ 预录制的一段导语/串词音频，在歌曲播出的同时叠加播放，模拟主持人现场主持效果。

```
AddClip() 中的串词处理:
│
├── 条件: program.timeItem.link_audio_id != 0
│         program.timeItem.link_audio != null
│         PlayIn == 0 (从头开始播的节目)
│
├── 设置串词参数:
│   ├── link_file = link_audio.local_url (串词文件路径)
│   ├── link_fadein = GlobalValue.Link_FadeIn
│   ├── link_fadeout = GlobalValue.Link_FadeOut
│   ├── link_damping = GlobalValue.Link_Daming (歌曲压低量)
│   └── link_in = 5000 (默认: 播放5秒后开始串词)
│
├── 智能串词位置:
│   if (有"前奏结束"打点信息) {
│     pos = 前奏结束时间 - 串词时长
│     if (pos < 0) → link_in = 5000 (退回默认)
│     else → link_in = pos + Link_FadeOut
│   }
│
├── 入点播出的特殊处理:
│   if (PlayIn != 0 && enter == PlayIn) {
│     // 日播单打点入点播出
│     if (剩余时长 > 2分钟)
│       → link_in = PlayIn + Link_FadeOut
│   }
│
└── BASS 引擎播出时自动叠加:
    歌曲到 link_in 位置 → 压低歌曲音量(damping)
    → 淡入播串词 → 串词结束 → 恢复歌曲音量
```

### 11.3 歌曲预告(type=17)

歌曲预告是一种特殊节目类型，将即将播出的多首歌曲的开头片段拼接成预告：

```
AddClip() 中 type=17 处理:
│
├── program.PreviewClips = m_playlist.FindSongPreview(ArrangeId)
│
├── 构建多片段 PlayClip:
│   ├── 主 clip.clips = new List<PlayClip>()
│   ├── 遍历 PreviewClips:
│   │   ├── 如果是字符串 → 直接作为文件名
│   │   └── 如果是 SlaProgram → 提取文件+入出点
│   │       └── 本地无文件 → 异步下载
│   │
│   └── bassController.AddClips() 批量提交
│
└── 注意: 歌曲预告不记录播出日志
```

### 11.4 音频效果器切换

系统可根据不同播出内容自动切换音频处理效果（EQ/压限等）：

```
DoSwitchEffect(program, @switch)
│
├── 前提: SwitcherEnabled = true + Effect_Device4000 可用
│
├── 效果ID确定:
│   ├── 直播信号 (SignaType=2) → liveEffect
│   ├── 转播信号 (SignaType=3) → relayEffect
│   ├── 录播信号:
│   │   ├── program == null:
│   │   │   ├── 是切换事件 (@switch=true) → 保持当前效果
│   │   │   └── 否则 → defaultEffect (直通)
│   │   ├── type=7 或 type=12 → customEffects[category=-100]
│   │   ├── type=13 → customEffects[category=-101]
│   │   └── 其他 → 按 category_id/top_category_id 匹配 customEffects
│   │       → 无匹配 → effect_id=0 (直通)
│
└── Effect_Device4000.SetEffectMode(effect_id)
```

---

## 十二、多播出源协调

系统存在 4 种独立的播出源，通过不同的虚拟通道管理：

| 播出源 | 虚拟通道 | 状态标识 | 管理位置 |
|--------|----------|----------|----------|
| **主播单** | 主通道 (m_channel) | `ListPlaying` | SlaCardPlayerAdapter |
| **垫乐** | FillBlank 通道 | `BlankPadding` | SlaBlankTaskManager |
| **Jingle单** | Jingle通道 | `JinglePlaying` | 外部模块 |
| **临时单** | TempList通道 | `TempPlaying` | 外部模块 |

**播出源优先级规则**：

```
定时触发 > 主播单 > Jingle单/临时单 > 垫乐

1. 定时触发时:
   → 淡出 Jingle 单 (StopJinglePlayer + JingleFadeOutTime)
   → 淡出临时单 (TempListFadeOutTime)
   → 停止垫乐 (SetPaddingPlay(false))
   → 播出定时节目

2. 主播单播完时:
   → 查找下条待播
   → 有 → 预卷并播出
   → 无 → 开启垫乐（如果条件允许）

3. Jingle/临时单播放时:
   → 取消软定时等待 (SoftFixWaiting = false)
   → 垫乐不受影响

4. 所有源都停止时:
   → PlayingStatus = Paused
```

---

## 十三、主备同步机制

系统支持主备机热备份，通过 `PlayingInfo` 持久化播出状态，实现秒级切换：

```
PlayingInfo (播出状态快照)
│
├── ProgramId — 正播节目编排ID
├── Position — 当前播放位置(ms)
├── system_time — 快照时的系统时间(ms)
├── SignalId / SignalName — 当前信号源
├── NextBlockId / NextSectionId / NextProgramId
├── NextPosition — 下条待播位置
│
├── CutSectionId — 插播栏目ID
├── CutPosition — 被插播位置
├── CutProgramId — 被插播节目ID
│
├── DelayBackTime — 通道保持返回时间
├── DelayBackProgram — 通道保持接播节目
├── DelayBackProgramName — 接播节目名称
├── IsAIDelay — 是否AI转播
│
├── Read() — 从持久化读取
├── Write() — 定期写入
└── EnableWrite — 控制写入开关

主备同步恢复流程:
│
├── PlayingInfo.Read() 读取主机最后状态
├── FindProgram(pi.ProgramId) 在播表中定位
├── 计算时间偏移: PlayIn = Position + (now - system_time)
│   → 补偿主机停止到备机启动的时间差
├── 恢复信号源: SwitchSignal(pi.SignalId, ...)
├── 恢复插播状态:
│   if (InterCut) → m_CutPlaying = true
│   → 恢复 IntCut_BackProgram 和 InterCut_Back
├── 入点容错: < 2秒入点从头开始（防吃字）
├── 最后剩余 < 1秒 → 强制播1秒（防重复判断失效）
│
├── 根据远端播出状态决定行为:
│   ├── ListPlaying → PlayNextClip(StatusChanged) 直接播
│   ├── BlankPadding → Prepare + Play 垫乐
│   ├── Paused → 等待（不主动播出）
│   ├── JinglePlaying/TempPlaying → 等外部触发
│   └── 未登录 → 直接 PlayNextClip 套入
│
└── 启动定时任务，进入正常循环
```

---

## 十四、事件系统

### 对外暴露事件

| 事件 | 作用 | 触发时机 |
|------|------|----------|
| `PlayStatusChanged` | 播出状态变更通知 | 状态机迁移完成后 |
| `CountDownUpdate` | 倒计时更新 | 停播时重置 |
| `PromptErrorMsg` | 错误/提示消息 | 播出失败、切换失败等 |
| `OperationDone` | 操作完成通知 | - |
| `EmrgPlayPositionUpdated` | 紧急插播位置更新 | 插播状态变化 |
| `BlankFillStateChanged` | 垫乐状态变更 | 垫乐开启/停止 |
| `PlayingClipUpdate` | 正播节目更新 | 节目切换后 |
| `Next1ClipUpdate` | 待播1更新 | 预卷完成 |
| `Next2ClipUpdate` | 待播2更新 | - |
| `StopJinglePlayer` | 停止Jingle播放 | 定时触发、垫乐控制 |
| `ClipPlayFinished` | 节目播完事件 | 素材自然结束 |
| `OralClipProgressUpdate` | 口播进度更新 | - |
| `OnPlay` / `OnPause` | 播放/暂停状态 | 对应操作时 |

### 内部事件(ManualResetEvent)

| 事件 | 作用 | 生产者 → 消费者 |
|------|------|----------------|
| `evt_exit` | 通知退出 | Stop() → 所有线程 |
| `evt_play_finished` | 素材播完 | BASS回调 → PlaybackThread |
| `evt_GlobalStateChanged` | 状态迁移请求 | ChangeStatus() → WorkThread |
| `evt_channel_empty` | 通道空闲 | BASS回调 → WorkThread |

---

## 十五、线程安全设计

| 同步机制 | 保护对象 | 使用位置 |
|----------|----------|----------|
| `ManualResetEvent × 4` | 线程间事件通信 | PlaybackThread / WorkThread |
| `lock(m_statCtlr)` | 状态迁移操作 | WorkThread 状态切换 |
| `lock(m_tasklist)` | 定时任务列表 | AddTask/RemoveTask/ClearTask |
| `lock(m_intercut_list)` | 插播任务列表 | 插播线程 |
| `lock(lock_play)` | 垫乐播放/停止 | Play()/Stop()/FadeToNext() |
| `m_in_play_next_process` | 播放切换进程标志 | 防止定时与播完冲突 |
| `s_in_fixtime_task` | 定时任务执行中标志 | 抑制 PlaybackThread 的播完事件 |
| `GlobalValue.SoftFixWaiting` | 软定时等待标志 | 跨组件同步 |

**关键竞争场景处理**：

```
场景1: 定时触发 vs 播完事件
  定时线程: 设置 s_in_fixtime_task = true
  播出线程: if (!s_in_fixtime_task) 才处理播完事件
  → 定时任务优先

场景2: 定时触发 vs PlayNextClip 进程
  定时线程: while(m_in_play_next_process && nWait < 50) 等待
  → 最多等500ms，然后强制接管

场景3: 插播 vs 即将到达的定时
  插播时: if (IsNearFixTask(1000)) return
  → 1秒内有定时则放弃插播
```

---

## 十六、关键业务场景时序

### 场景1：正常一天的播出

```
06:00 操作员启动系统
  ├── Start(Auto, false, 0)
  ├── Stop→Auto: SeekToCrntPlayPoint(06:00)
  ├── 找到06:00应播节目
  ├── 切换信号到录播通道
  ├── 预卷并播出第一首歌
  └── 初始化当天所有定时任务(约20-30个)

06:05 第一首歌播完
  ├── evt_play_finished 触发
  ├── PlayNextClip(false, FinishEvent)
  ├── 播第二首
  └── 预卷第三首

06:30 硬定时触发（新闻时段）
  ├── fixThread_Elapsed 检测到时间到达
  ├── FixTimeArrived 事件
  ├── 淡出当前歌曲(50ms提前量)
  ├── 切信号到"新闻转播"通道
  └── 播放新闻节目

07:00 硬定时触发（音乐时段）
  ├── 切回录播信号
  ├── 播音乐节目
  └── 歌曲有串词 → 前奏结束时叠加播出

07:15 栏目间空隙
  ├── 所有节目播完，m_nextPostion = null
  ├── PlayNextClip: 待播为空
  ├── 检查 is_padding = 1 → 允许垫乐
  ├── Prepare(Auto) → 选最久没播的垫乐
  ├── Play() → 开始垫乐
  └── 自动循环播放

07:30 硬定时触发（广告时段）
  ├── 停止垫乐 SetPaddingPlay(false)
  └── 播出广告节目

08:00 插播任务触发
  ├── intercutThread_Elapsed 检测到
  ├── 记录被插播位置(当前歌曲+播放时间)
  ├── 播放插播广告
  ├── 广告播完 → 从被插播位置恢复
  └── 继续播被打断的歌曲(从断点-补偿时间处)

15:00 DJ上线直播
  ├── ChangeStatus(Live)
  ├── Auto→Live: 不停播
  ├── DJ 手动播 Jingle
  ├── DJ 立即播 PlayProgramNow()
  └── 定时任务仍在运行

17:00 DJ下线
  ├── ChangeStatus(Auto)
  ├── Live→Auto: 重新初始化定时任务
  ├── 当前无节目 → 开启垫乐
  └── 等待下一个定时触发

22:00 转播央广节目
  ├── 信号控件触发: AI_Delay=true
  ├── 切到转播信号 SwitchSignal()
  ├── DelayStart() → 进入通道保持
  ├── Auto→RedifDelay
  ├── AIEndingDetector 开始监听
  ├── 检测到节目结束
  ├── _ChannelHold_Back → ChangeStatus(Auto)
  ├── 切回录播信号 _SwitchLocal()
  └── 播放接播节目

00:00 日播单结束
  ├── 最后一条节目播完
  ├── FindNextFixByTime() 返回 null
  └── 不垫乐，等待新的日播单
```

### 场景2：主备切换

```
主机正常运行中...
  PlayingInfo 持续写入:
  ├── ProgramId = 当前节目
  ├── Position = 播放进度
  └── system_time = 当前系统时间

10:30:00 主机故障!

10:30:02 备机检测到心跳丢失
  ├── Start(Auto, bFollowRemote=true, ListPlaying)
  ├── Stop→Auto
  ├── PlayingInfo.Read() 读取:
  │   ├── ProgramId = 12345
  │   ├── Position = 45000ms
  │   └── system_time = 10:30:00的毫秒值
  │
  ├── InitByLastPlayPosition():
  │   ├── FindProgram(12345)
  │   ├── 计算偏移: PlayIn = 45000 + (10:30:02 - 10:30:00) = 47000ms
  │   ├── 从47秒处预卷
  │   ├── 无缝续播(听众无感知)
  │   └── 初始化定时任务
  │
  └── 进入正常播出循环
```

### 场景3：复杂插播嵌套

```
正在播歌曲A (07:15:30)
  │
  ├── 08:00 插播栏目X 触发
  │   ├── 记录: IntCut_BackProgram = 歌曲A
  │   ├── 记录: InterCut_Back = 歌曲A播放位置 - 补偿
  │   ├── m_CutPlaying = true
  │   └── 开始播广告1
  │
  ├── 08:00:30 播广告2
  │
  ├── 08:01:00 硬定时触发!
  │   ├── 检测到 m_CutPlaying = true
  │   ├── 清理插播状态:
  │   │   └── 标记歌曲A为已播(不再返回)
  │   ├── 播定时节目
  │   └── m_CutPlaying = false
  │
  └── 08:30:00 下一个定时...
```

---

## 十七、AI 能力矩阵

结合 XMind 和代码分析，V8.2 系统的 AI 能力：

| AI 功能 | 所在模块 | 代码中的体现 | 业务价值 |
|---------|----------|------------|----------|
| **AI 智能转播** | Playthread | `AIEndingDetector` + `AI_Delay` | 自动检测转播节目结束，无需人工盯播 |
| **AI 智能垫乐** | Playthread | `EnableAIPadding` + `AIPaddingTime` | 短间隙播轻音乐，长间隙播正式垫乐 |
| **AI 串词** | 工作中心/编排 | `link_audio` 系统 | AI 生成DJ导语，减少人工录制 |
| **AI 主持** | 工作中心/编排 | - | AI 生成完整主持稿 |
| **AI 资讯** | 工作中心/计划 | 天气/路况/新闻节目 | 自动生成资讯类节目内容 |
| **AI 创作** | 工作中心/节目 | - | AI 辅助节目创作 |
| **AI 点位同步** | 同步服务 | - | 同步 AI 标注的节目打点信息 |

---

## 十八、架构评估与特点

### 优势

| 方面 | 设计特点 | 评价 |
|------|----------|------|
| **实时性** | 20ms 定时轮询 + ManualResetEvent 事件驱动 | 达到广播级精度要求 |
| **可靠性** | 多层重试 + 失败跳下条 + 主备热备 | 保证不断播，7×24运行 |
| **音频专业性** | BASS 引擎 + 虚拟通道 + 淡入淡出 + 串词叠加 | 专业广播级音频处理 |
| **灵活性** | 6状态机 + 4种播出源 + 5种定时模式 | 覆盖广播电台各种复杂场景 |
| **运维** | 主备自动切换 + 心跳监控 + 完善日志 | 支持无人值守运行 |
| **扩展** | AI智能转播/智能垫乐已集成 | 向智能化演进 |

### 已知局限

| 方面 | 现状 | 影响 |
|------|------|------|
| **代码规模** | SlvcPlayThread 单文件 3700+ 行 | 维护难度较高 |
| **重试模式** | goto agin 模式 | 可读性不佳，可用循环替代 |
| **耦合度** | GlobalValue 全局状态共享 | 多模块间隐式依赖 |
| **测试性** | 无可见单元测试 | 难以验证修改的正确性 |
| **注释** | 中文注释为主 | 适合中文团队，限制国际协作 |

---

> **文档版本**: v1.0  
> **分析日期**: 2026年3月18日  
> **分析范围**: Playthread 模块 7 个源文件 + 3 个 XMind 架构文档  
> **代码总行数**: 约 4000+ 行 C#
