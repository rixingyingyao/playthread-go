# Playthread-Go 项目开发规范

> 本规范适用于 Playthread 广播播出控制系统的 Go 重构项目。  
> 系统为 **7×24×365 长年累月不关机运行**的客户端程序，对稳定性、实时性和资源管理有最高要求。  
> 系统架构为**云端（SAAS）+ 本地中心 + 本地播出机**三级架构，本模块为本地播出机的播出执行引擎。  
> 采用**双进程架构**：Go 主控进程（纯 Go，无 cgo）+ 播放服务子进程（Go + cgo/BASS），通过 IPC 通信。

---

## 目录

- [一、资源管理规范](#一资源管理规范)
- [二、错误处理规范](#二错误处理规范)
- [三、空值与边界检查规范](#三空值与边界检查规范)
- [四、并发与线程安全规范](#四并发与线程安全规范)
- [五、内存管理与长运行稳定性规范](#五内存管理与长运行稳定性规范)
- [六、CGO 与 BASS FFI 规范](#六cgo-与-bass-ffi-规范)
- [七、日志规范](#七日志规范)
- [八、配置管理规范](#八配置管理规范)
- [九、优雅退出规范](#九优雅退出规范)
- [十、心跳与自愈规范](#十心跳与自愈规范)
- [十一、代码风格规范](#十一代码风格规范)
- [十二、测试规范](#十二测试规范)
- [十三、构建与部署规范](#十三构建与部署规范)
- [十四、架构设计规范](#十四架构设计规范)
- [十五、状态机严格规范](#十五状态机严格规范)
- [十六、背压与限流规范](#十六背压与限流规范)
- [十七、磁盘与文件系统安全规范](#十七磁盘与文件系统安全规范)
- [十八、音频引擎隔离与硬件防护规范](#十八音频引擎隔离与硬件防护规范)
- [十九、冷启动恢复与幂等性规范](#十九冷启动恢复与幂等性规范)
- [二十、多平台适配规范](#二十多平台适配规范)
- [二十一、数据库规范](#二十一数据库规范)

---

## 一、资源管理规范

### 1.1 原则

所有外部资源（文件句柄、网络连接、BASS 音频通道、锁、定时器）必须**显式释放**。Go 的 `defer` 机制是核心工具，但需谨慎使用以避免延迟释放导致的资源积压。

### 1.2 规则

| 资源类型 | 释放方式 | 示例 |
|----------|----------|------|
| 文件句柄 | `defer f.Close()` | `f, err := os.Open(path); defer f.Close()` |
| 网络连接 | `defer resp.Body.Close()` | HTTP 响应体必须关闭 |
| BASS 音频通道 | 播完/停止后立即调用 `C.BASS_StreamFree(handle)` | 不用 defer，需精确控制时机 |
| goroutine | `context.WithCancel` + `select` 退出 | 每个 goroutine 必须有退出路径 |
| Ticker/Timer | `defer ticker.Stop()` | `time.NewTicker(20 * time.Millisecond)` |
| sync.Mutex | `defer mu.Unlock()`（仅简单场景） | 复杂场景手动 Lock/Unlock |
| CGO 分配内存 | `defer C.free(unsafe.Pointer(cstr))` | `C.CString()` 必须配对 `C.free()` |
| SQLite 连接 | `defer db.Close()` | 应用退出时关闭 |

### 1.3 BASS 资源释放的特殊要求

BASS Stream Handle 是最频繁创建/释放的资源（每天约 4,320 次切歌）：

```go
// ✅ 正确：播完后立即释放，不用 defer
func (vc *VirtualChannel) freeBassStream(item *PlayListItem) {
    if item.linkHandle != 0 {
        C.BASS_ChannelStop(item.linkHandle)
        C.BASS_StreamFree(item.linkHandle)
        item.linkHandle = 0
    }
    if item.streamHandle != 0 {
        C.BASS_ChannelStop(item.streamHandle)
        C.BASS_StreamFree(item.streamHandle)
        item.streamHandle = 0
    }
}

// ❌ 禁止：用 defer 释放 BASS 流（会在函数返回时才释放，此时可能已经创建了新流）
func (vc *VirtualChannel) addClip(file string) {
    handle := C.BASS_StreamCreateFile(...)
    defer C.BASS_StreamFree(handle)  // ← 错误！defer 会过早释放
}
```

### 1.4 禁止事项

- ❌ 打开文件后不关闭
- ❌ 创建 BASS 通道后不释放
- ❌ 使用 `C.CString()` 后不 `C.free()`
- ❌ 启动 goroutine 后无退出机制
- ❌ 创建 Ticker/Timer 后不 Stop

---

## 二、错误处理规范

### 2.1 原则

Go 的错误处理是显式的（`if err != nil`），这是系统可靠性的核心保障。**禁止忽略错误**（除明确标注为安全忽略的场景）。

### 2.2 规则

```go
// ✅ 正确：检查错误并处理
func (e *BassEngine) loadClip(path string) (C.HSTREAM, error) {
    cpath := C.CString(path)
    defer C.free(unsafe.Pointer(cpath))
    
    handle := C.BASS_StreamCreateFile(0, unsafe.Pointer(cpath), 0, 0, 0)
    if handle == 0 {
        errCode := C.BASS_ErrorGetCode()
        return 0, fmt.Errorf("BASS_StreamCreateFile 失败: path=%s, errCode=%d", path, errCode)
    }
    return handle, nil
}

// ✅ 正确：明确忽略错误时使用注释
_ = os.Remove(tempFile) // 临时文件删除失败不影响播出

// ❌ 禁止：忽略错误
handle := C.BASS_StreamCreateFile(...)  // 没检查返回值
```

### 2.3 错误处理层级

| 层级 | 处理方式 |
|------|----------|
| 底层（BASS/文件/网络） | 返回 error，调用者决定处理方式 |
| 中层（业务逻辑） | 检查 error，执行降级逻辑（跳下条/开垫乐/重试） |
| 顶层（goroutine 入口） | `recover()` 捕获 panic，记录日志，触发自愈 |

### 2.4 领域错误类型

定义业务领域的 error 类型，避免所有错误都是 `fmt.Errorf`：

```go
// 播出错误
type PlayError struct {
    Op      string // 操作名
    ClipID  string // 素材ID
    Cause   error  // 原始错误
}

func (e *PlayError) Error() string {
    return fmt.Sprintf("播出错误[%s] clip=%s: %v", e.Op, e.ClipID, e.Cause)
}

func (e *PlayError) Unwrap() error { return e.Cause }

// 状态转换错误
type TransitionError struct {
    From    Status
    To      Status
    Reason  string
}

func (e *TransitionError) Error() string {
    return fmt.Sprintf("非法状态转换: %s → %s, 原因: %s", e.From, e.To, e.Reason)
}
```

### 2.5 重试策略（业务对齐 C# 原版）

| 操作 | 最大重试 | 间隔 | 降级方案 | 业务原因 |
|------|---------|------|----------|----------|
| **预卷(Cue)素材** | 3 次 | 200ms | ①重试预卷 ②文件不存在则触发下载重试 ③仍失败→标记 CueFailed→跳过此素材→递归找下条 | 文件可能正在下载中 |
| **播出(Play)** | 1 次 | 100ms | 标记失败 → 延迟300ms注入PlayFinish事件 → 跳下条 | 播出失败通常是素材问题 |
| **信号切换** | 2 次 | 500ms | 每次切换后强制等待 500ms（给切换器硬件反应时间）→ 失败则记录错误日志 + 通知前端 | 切换器硬件可能短暂忙碌 |
| **文件下载** | 3 次 | 指数退避(1s/2s/4s) | 使用远程 URL 直接播放 | 网络波动常见 |
| **心跳上报** | 不限次 | 10s-30s自然周期 | 本地暂存，联网后补传 | 心跳失败不影响播出 |
| **日播单同步** | 3 次 | 指数退避(5s/15s/30s) | 使用本地缓存播单 | 播单是核心数据 |
| **插播指令执行** | 2 次 | 100ms | 记录失败 + 通知云端 | 插播有时效性 |

### 2.6 panic 恢复

每个独立 goroutine 入口必须有 recover：

```go
func (pt *PlayThread) playbackLoop() {
    defer func() {
        if r := recover(); r != nil {
            log.Error().Interface("panic", r).Str("stack", string(debug.Stack())).
                Msg("PlaybackThread panic 恢复")
            // 触发自愈：延迟1秒后重启
            time.AfterFunc(time.Second, func() {
                pt.restartPlaybackThread()
            })
        }
    }()
    // ... 主循环
}
```

---

## 三、空值与边界检查规范

### 3.1 原则

Go 的零值语义（nil pointer, 空 slice, 空 map）比 Python 更安全，但仍需对以下场景做防御性检查：

### 3.2 必须检查的场景

```go
// ✅ 指针类型返回值
program := pt.playlist.FindNext()
if program == nil {
    pt.startPadding()
    return
}

// ✅ slice 长度检查
if len(tasks) > 0 {
    firstTask = tasks[0]
}

// ✅ map 访问
value, ok := config["key"]
if !ok {
    value = defaultValue
}

// ✅ interface 类型断言
if ctrl, ok := item.(FixControl); ok {
    // 安全使用 ctrl
}

// ✅ channel 关闭检查
select {
case task, ok := <-ch:
    if !ok {
        return // channel 已关闭
    }
    process(task)
}
```

### 3.3 播表位置导航安全

```go
// ✅ 导航前验证
func (pl *Playlist) FindNextProgram(currentPos int) *Program {
    nextPos := currentPos + 1
    if nextPos >= len(pl.flatList) {
        return nil // 已到播表末尾
    }
    return pl.flatList[nextPos]
}
```

---

## 四、并发与线程安全规范

### 4.1 原则

Go 的 goroutine 模型无 GIL 限制，所有 goroutine 真正并行执行，因此线程安全更加关键。**共享状态访问必须加锁或通过 channel 通信**。

### 4.2 线程模型

系统采用双进程架构，主控进程和播放服务子进程各有独立的 goroutine 模型。

**主控进程（纯 Go）：**

```
主 goroutine (HTTP 服务 + 信号处理)
├── HTTP Server (net/http)
├── WebSocket 推送
├── UDP 监听
└── 信号处理 (SIGTERM/SIGINT)

PlaybackGoroutine (普通 goroutine)
├── 响应播完事件（通过 AudioBridge IPC 接收子进程通知）
└── PlayNextClip 决策

WorkGoroutine (普通 goroutine)
├── 状态迁移
└── 预卷下条素材（通过 AudioBridge 发送 IPC 命令）

FixTimeGoroutine (普通 goroutine)
├── 20ms Ticker 轮询定时任务
└── 20ms Ticker 轮询插播任务

AudioBridgeGoroutine (普通 goroutine)
├── 读取子进程 stdout，分发响应和事件
├── 管理 pending 请求映射
└── 检测子进程崩溃并触发重启
```

**播放服务子进程（Go + cgo）：**

```
主 goroutine (IPC 服务端)
├── stdin 读取命令
├── stdout 写入响应/事件
└── 命令分发到 BASS goroutine

BASS 专用 goroutine (runtime.LockOSThread) ★ 唯一需要绑定 OS 线程的 goroutine
├── 所有 BASS API 调用通过 channel 派发到此 goroutine 执行
├── BASS_Init / BASS_Free 在此线程执行
├── 双通道设计（ctrlCh + ioCh）提供指令优先级路由
└── 保证 BASS 线程亲和性

> **注意**：只有播放服务子进程中的 BASS 专用 goroutine 需要 `runtime.LockOSThread()`。
> 主控进程完全不需要 `runtime.LockOSThread()`。
```

### 4.3 BASS 线程亲和性（播放服务子进程内部设计）

> 以下设计仅存在于播放服务子进程内部。主控进程通过 AudioBridge IPC 间接调用，不涉及 BASS 线程。

BASS 库要求通道操作在创建通道的同一 OS 线程执行。Go 的 goroutine 默认会在 OS 线程间漂移，因此 BASS 操作必须集中到一个 `runtime.LockOSThread()` 的专用 goroutine。

为避免**队头阻塞**（一个慢操作如 StreamCreateFile 阻塞整个 BASS 线程），使用**双通道**设计：
- **ctrlCh**（控制通道）：Play / Stop / Pause / SetVolume 等**低延迟操作**，优先处理
- **ioCh**（IO 通道）：StreamCreateFile / StreamFree 等**可能耗时的操作**

> **注意**：双通道提供的是**指令优先级路由**——控制命令不会被 IO 命令在队列中挤占。
> 但由于 BASS 是单线程，当一个耗时 IO 命令正在执行时，控制命令仍需等待其完成。
> 对于紧急停止等场景，可在 C 层通过置标志位让当前操作尽早返回。

```go
// ✅ BASS 专用 goroutine — 双通道提供指令优先级路由
func (be *BassEngine) bassLoop(ctx context.Context) {
    runtime.LockOSThread() // 锁定到当前 OS 线程，永不解锁
    defer runtime.UnlockOSThread()
    
    for {
        select {
        case cmd := <-be.ctrlCh: // 控制命令优先
            result := be.executeCommand(cmd)
            cmd.resultCh <- result
        default:
            select {
            case cmd := <-be.ctrlCh:
                result := be.executeCommand(cmd)
                cmd.resultCh <- result
            case cmd := <-be.ioCh: // IO 命令次优先
                result := be.executeCommand(cmd)
                cmd.resultCh <- result
            case <-ctx.Done():
                return
            }
        }
    }
}

// 调用方通过 channel 发送命令
type BassCommand struct {
    Type     BassCommandType
    Args     interface{}
    resultCh chan BassResult
}

// Play 播出（走控制通道，低延迟）
func (be *BassEngine) Play(handle C.HSTREAM) error {
    result := be.sendCtrl(BassCommand{
        Type: CmdPlay,
        Args: handle,
    })
    return result.Err
}

// LoadClip 加载素材（走 IO 通道，可能耗时）
func (be *BassEngine) LoadClip(path string) (C.HSTREAM, error) {
    result := be.sendIO(BassCommand{
        Type: CmdStreamCreate,
        Args: path,
    })
    return result.Handle, result.Err
}
```

### 4.4 锁的使用规则

```go
// ✅ 锁粒度尽量小
pt.taskMu.Lock()
task := pt.tasks[0]
pt.tasks = pt.tasks[1:]
pt.taskMu.Unlock()
// 在锁外执行耗时操作
pt.executeTask(task)

// ❌ 禁止在锁内做 I/O
pt.mu.Lock()
data, _ := http.Get(url) // 网络阻塞
pt.cache = data
pt.mu.Unlock()
```

### 4.5 全局锁顺序

为防止死锁，所有地方获取多把锁时必须按以下顺序：

```
1. stateMu     (状态机锁)
2. taskMu      (定时任务锁)
3. intercutMu  (插播任务锁)
4. playMu      (播放控制锁, RWMutex)
5. blankMu     (垫乐控制锁)
6. logMu       (日志写入锁 — 通常不需要，zerolog 自身线程安全)
```

### 4.6 goroutine 间通信

| 通信方式 | 适用场景 | 示例 |
|---------|---------|------|
| `chan struct{}` | 事件通知（等价 ManualResetEvent） | `evtPlayFinished chan struct{}` |
| `chan T` | 数据传递 | `cmdCh chan BassCommand` |
| `sync.Mutex` | 保护共享状态 | `var playMu sync.RWMutex` |
| `sync.Cond` | 复杂条件等待 | 等待多个条件之一满足 |
| `atomic` | 简单标志位 | `atomic.Bool` 替代 `s_in_fixtime_task` |

### 4.7 select 多路复用（替代 WaitAny）

```go
// ✅ Go 的 select 天然支持等待多个事件
func (pt *PlayThread) playbackLoop(ctx context.Context) {
    for {
        select {
        case <-pt.evtPlayFinished:
            pt.playNextClip()
        case <-ctx.Done():
            return
        }
    }
}
```

### 4.8 禁止事项

- ❌ 不加锁直接读写共享变量（Go 无 GIL，数据竞争必崩）
- ❌ 在锁内调用可能阻塞的方法（channel、网络、文件）
- ❌ 启动 goroutine 后无退出机制
- ❌ 在主控进程中使用 `import "C"` 或直接调用 BASS API（必须通过 AudioBridge IPC）
- ❌ 在播放服务子进程的 BASS 回调中执行复杂 Go 逻辑（应只发 channel 信号）
- ❌ 在主控进程使用 `runtime.LockOSThread()`（纯 Go 不需要）
- ❌ 在播放服务子进程的业务 goroutine 上使用 `runtime.LockOSThread()`（仅 BASS goroutine 需要）
- ❌ 使用 `sync.Mutex` 做超时锁（Go 标准库 Mutex 不支持 TryLock 超时，这是 C# Monitor.TryEnter 的习惯）

> **带超时的重入防护**（替代 C# 的 `Monitor.TryEnter(500)`）：
> ```go
> // 使用带缓冲 channel 当超时锁
> playNextLock := make(chan struct{}, 1)
> playNextLock <- struct{}{} // 初始化：放入令牌
>
> func (pt *PlayThread) playNextClip() {
>     select {
>     case <-pt.playNextLock: // 取锁（取出令牌）
>         defer func() { pt.playNextLock <- struct{}{} }() // 释放
>         // ... 执行逻辑
>     case <-time.After(500 * time.Millisecond):
>         log.Warn().Msg("PlayNextClip 500ms 超时，拒绝重入")
>         return
>     }
> }
> ```

### 4.9 竞态检测

开发阶段所有测试必须使用 `-race` 标志：

```bash
go test -race ./...
```

CI 中也必须开启 race detector。生产环境不开启（有性能开销）。

---

## 五、内存管理与长运行稳定性规范

### 5.1 原则

Go 的 GC（STW < 0.5ms）天然适合长运行系统，但仍需防止 goroutine 泄漏、CGO 内存泄漏和容器无限增长。

### 5.2 容器管理

| 容器 | 上限策略 | 清理时机 |
|------|----------|----------|
| 播放历史 | 保留最近 2 天 | 每次启动 + 每天 0 点 |
| 定时任务 slice | 无上限，按时间过期 | 执行后立即移除，超 3 秒自动丢弃 |
| 播出日志缓冲 | 最多 1000 条 | 写入文件后清空 |
| 文件下载缓存 | 按云端策略 | 每天凌晨 LRU 清理 |
| WebSocket 连接池 | 最多 50 个 | 断开后立即移除 |

### 5.3 CGO 内存管理

CGO 分配的内存不受 Go GC 管理，必须手动释放：

```go
// ✅ 每次 C.CString 必须配对 C.free
func bassStreamCreate(file string) (C.HSTREAM, error) {
    cfile := C.CString(file)
    defer C.free(unsafe.Pointer(cfile))
    
    handle := C.BASS_StreamCreateFile(0, unsafe.Pointer(cfile), 0, 0, 0)
    if handle == 0 {
        return 0, fmt.Errorf("创建流失败")
    }
    return handle, nil
}
```

### 5.4 goroutine 泄漏检测

```go
// 使用 pprof 监控 goroutine 数量
import _ "net/http/pprof"

func init() {
    go http.ListenAndServe(":6060", nil)
}

// 测试中使用 goleak
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

### 5.5 GC 调优

```go
// GOGC 控制 GC 触发频率（默认100，即堆增长100%时触发）
// 对于本系统，适当降低以减少内存峰值
// runtime/debug.SetGCPercent(80)

// GOMEMLIMIT 设置软内存上限（Go 1.19+）
// 防止 365 天运行中内存缓慢增长
// runtime/debug.SetMemoryLimit(512 * 1024 * 1024) // 512MB
```

### 5.6 内存监控

```go
// 每小时记录一次内存快照
func (m *MemoryMonitor) check() {
    var stats runtime.MemStats
    runtime.ReadMemStats(&stats)
    log.Info().
        Uint64("alloc_mb", stats.Alloc/1024/1024).
        Uint64("sys_mb", stats.Sys/1024/1024).
        Uint32("num_gc", stats.NumGC).
        Uint64("goroutines", uint64(runtime.NumGoroutine())).
        Msg("内存状态")
}
```

### 5.7 禁止事项

- ❌ 向 slice/map 无限追加数据不清理
- ❌ 启动 goroutine 不提供退出路径（goroutine 泄漏）
- ❌ `C.CString()` 不配对 `C.free()`
- ❌ 全局变量持有不再使用的大对象
- ❌ 使用 `C.malloc()` 不配对 `C.free()`

---

## 六、CGO 与 BASS FFI 规范（仅适用于播放服务子进程）

> **注意**：本章全部规范仅适用于**播放服务子进程**（Go + cgo 编译）。
> Go 主控进程为纯 Go，不使用 cgo，不导入 `"C"` 包。

### 6.1 原则

所有 BASS C 库函数通过 cgo 调用。cgo 调用有 ~50-100ns 开销（比纯 Go 函数高），但对本项目（日均 200-400 万次调用）完全可接受。

### 6.2 cgo 绑定规范

```go
package bass

/*
#cgo windows LDFLAGS: -L${SRCDIR}/libs -lbass
#cgo linux LDFLAGS: -L${SRCDIR}/libs -lbass -Wl,-rpath,${SRCDIR}/libs
#include "bass.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// 所有函数必须声明完整的 Go 签名和注释
// Init 初始化 BASS 引擎
// device: 设备索引（-1=默认）
// freq: 输出采样率
func Init(device int, freq int) error {
    if C.BASS_Init(C.int(device), C.DWORD(freq), 0, nil, nil) == 0 {
        return fmt.Errorf("BASS_Init 失败: errCode=%d", C.BASS_ErrorGetCode())
    }
    return nil
}
```

### 6.3 回调函数（从 C 到 Go）

**所有 `//export` 回调函数的第一行必须是 `defer recover()`**，因为 cgo 回调中的 panic 无法被上层 Go 代码捕获，会直接导致 SIGABRT 闪退：

```go
// 使用 //export 导出回调给 C 层
//export goPlayFinishedCallback
func goPlayFinishedCallback(handle C.HSYNC, channel C.DWORD, data C.DWORD, user unsafe.Pointer) {
    // ★ 强制规则：cgo 回调的绝对第一行必须是 recover
    defer func() {
        if r := recover(); r != nil {
            // 在 C 线程中，无法使用 zerolog，用 fmt 写 stderr
            fmt.Fprintf(os.Stderr, "BASS 回调 panic: %v\n", r)
        }
    }()
    
    // ⚠️ 此函数在 BASS C 线程中执行
    // 禁止在此处做复杂 Go 操作
    // 只通过 channel 发送通知
    id := int(uintptr(user))
    select {
    case callbackCh <- CallbackEvent{Type: PlayFinished, ChannelID: id}:
    default:
        // channel 满，丢弃（不阻塞 C 线程）
    }
}
```

### 6.4 加密文件流回调（FileUser）

```go
// C 端的文件回调结构体
/*
extern void    goFileCloseProc(void* user);
extern QWORD   goFileLenProc(void* user);
extern DWORD   goFileReadProc(void* buffer, DWORD length, void* user);
extern BOOL    goFileSeekProc(QWORD offset, void* user);
*/

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

func createUserStream(item *EncryptedFileItem) (C.HSTREAM, *BassFileUser, error) {
    fu := newBassFileUser(item)
    // ... 创建流
    // 注意：必须在流释放时调用 fu.Dispose()
    return handle, fu, nil
}
```

### 6.5 禁止事项

- ❌ 在 cgo 回调中执行复杂 Go 逻辑（分配内存、获取锁等）
- ❌ 在非 LockOSThread 的 goroutine 中直接调用 BASS API（必须通过 cmdCh 派发）
- ❌ 忘记释放 `C.CString()` / `C.malloc()` 分配的内存
- ❌ 忘记释放 `cgo.Handle`（使用 `BassFileUser.Dispose()` + `sync.Once` 防双重释放）
- ❌ 在 cgo 回调中不写 `defer recover()`（会导致整个进程崩溃，无任何日志）
- ❌ 关闭事件 channel（退出时先 RemoveSync，然后停止读取即可；channel 不 close，靠 GC 回收，防止 C 层遗留回调 send on closed channel）

---

## 七、日志规范

### 7.1 日志级别

| 级别 | 用途 | 示例 |
|------|------|------|
| `Debug` | 变量值、流程分支（仅开发时开启） | `PlayNextClip: nextPos=5` |
| `Info` | 播出切换、状态变更、信号切换、定时触发 | `状态变更: Auto → Manual` |
| `Warn` | 预卷失败重试、软定时取消、缓存未命中 | `预卷失败，第2次重试: test.mp3` |
| `Error` | 播出失败、切换失败、异常捕获 | `播出失败: test.mp3, err: file not found` |
| `Fatal` | 系统级不可恢复故障（仅用于启动失败） | `BASS 引擎初始化失败，退出` |

### 7.2 日志框架

使用 **zerolog**（高性能结构化日志，零分配）：

```go
import (
    "github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
    "gopkg.in/natefinern/lumberjack.v2"
)

func initLogger() {
    // 文件输出 + 自动滚动
    fileWriter := &lumberjack.Logger{
        Filename:   "logs/playthread.log",
        MaxSize:    50,  // MB
        MaxBackups: 30,
        MaxAge:     30,  // 天
        Compress:   true,
    }
    
    // 错误日志独立文件
    errorWriter := &lumberjack.Logger{
        Filename:   "logs/error.log",
        MaxSize:    50,
        MaxBackups: 30,
        MaxAge:     90,
        Compress:   true,
    }
    
    // 多输出
    multi := zerolog.MultiLevelWriter(fileWriter, os.Stdout)
    log.Logger = zerolog.New(multi).With().
        Timestamp().
        Str("service", "playthread").
        Logger()
}
```

### 7.3 必须记录日志的操作

- 每条素材的播出开始/结束
- 每次状态机迁移（旧状态 → 新状态 + 触发原因）
- 每次信号切换
- 每次定时任务触发
- 每次插播开始/结束
- 每次垫乐开启/停止
- 每次错误及其恢复措施
- 每次主备切换
- 内存使用（每小时）
- 慢操作（耗时 > 50ms 的同步操作）

### 7.4 慢操作日志

```go
func slowOpLog(opName string, thresholdMs float64, fn func()) {
    start := time.Now()
    fn()
    elapsed := time.Since(start)
    if elapsed.Milliseconds() > int64(thresholdMs) {
        log.Warn().Str("op", opName).Dur("elapsed", elapsed).Msg("慢操作")
    }
}

// 使用
slowOpLog("LoadAudioFile", 50, func() {
    handle, err = be.streamCreateFile(path)
})
```

---

## 八、配置管理规范

### 8.1 原则

所有阈值、超时、路径等参数必须可配置，**禁止硬编码魔数**。

### 8.2 配置文件格式

使用 YAML 配置 + Go struct 映射：

```yaml
# config.yaml
playback:
  polling_interval_ms: 20
  task_expire_ms: 3000
  hard_fix_advance_ms: 50
  soft_fix_advance_ms: 0
  cue_retry_max: 3
  play_retry_max: 1

audio:
  sample_rate: 44100
  device_id: -1
  fade_in_ms: 500
  fade_out_ms: 500
  fade_cross_ms: 300

padding:
  enable_ai: true
  ai_threshold_ms: 60000
  history_keep_days: 2

server:
  host: "0.0.0.0"
  port: 3036
  ws_path: "/ws/playback"

monitor:
  memory_check_interval_s: 3600
  memory_warn_threshold_mb: 500
  heartbeat_interval_s: 5
```

### 8.3 配置模型

```go
type Config struct {
    Playback PlaybackConfig `yaml:"playback"`
    Audio    AudioConfig    `yaml:"audio"`
    Padding  PaddingConfig  `yaml:"padding"`
    Server   ServerConfig   `yaml:"server"`
    Monitor  MonitorConfig  `yaml:"monitor"`
}

type PlaybackConfig struct {
    PollingIntervalMs  int `yaml:"polling_interval_ms" validate:"min=5,max=100"`
    TaskExpireMs       int `yaml:"task_expire_ms" validate:"min=1000"`
    HardFixAdvanceMs   int `yaml:"hard_fix_advance_ms" validate:"min=0"`
    CueRetryMax        int `yaml:"cue_retry_max" validate:"min=1,max=10"`
}

// 加载配置
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("读取配置文件失败: %w", err)
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("解析配置文件失败: %w", err)
    }
    return &cfg, nil
}
```

---

## 九、优雅退出规范

### 9.1 退出顺序

**主控进程**关闭时按以下顺序：

```
1. 收到 SIGTERM/SIGINT → context.Cancel()
2. 停止定时任务轮询 goroutine
3. 通过 IPC 发送 stop 命令停止垫乐播放（淡出）
4. 通过 IPC 发送 stop 命令淡出并停止主播放
5. 通过 IPC 发送 shutdown 命令，通知播放服务子进程优雅退出
6. 等待子进程退出（最多 5s），超时强杀
7. 保存播放历史到文件
8. 等待所有 goroutine 退出（context + WaitGroup）
9. 关闭 WebSocket 连接
10. 关闭 HTTP Server（graceful shutdown）
11. 关闭 SQLite 连接
12. 关闭日志
```

**播放服务子进程**收到 shutdown 命令后按以下顺序：

```
1. 停止接受新的 IPC 命令
2. 移除所有 BASS Sync 回调（BASS_ChannelRemoveSync）
3. 释放所有 BASS 通道 (freeAll)
4. BASS_Free() 释放引擎
5. 关闭 stdin/stdout
6. 进程退出
```

> **重要**：播放服务子进程必须先移除所有 BASS 回调（步骤 2），再释放流（步骤 3）。
> 否则释放流的瞬间 BASS 可能触发 SYNCPROC 回调，而此时事件 channel 已无消费者，
> 遗留回调可能命中已释放资源。事件 channel 不要 close，靠 GC 回收即可（见 §6.5）。

### 9.2 实现

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    var wg sync.WaitGroup
    
    // 信号处理
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    
    // 启动各组件...
    
    // 等待退出信号
    <-sigCh
    log.Info().Msg("收到退出信号，开始优雅关闭...")
    cancel()
    
    // 等待所有 goroutine 退出，最多 10 秒
    done := make(chan struct{})
    go func() { wg.Wait(); close(done) }()
    
    select {
    case <-done:
        log.Info().Msg("所有组件已安全关闭")
    case <-time.After(10 * time.Second):
        log.Warn().Msg("部分组件未能在 10 秒内退出")
    }
}
```

---

## 十、心跳与自愈规范

### 10.1 goroutine 监控

```go
type GoroutineMonitor struct {
    watched map[string]*watchedRoutine
    mu      sync.Mutex
}

type watchedRoutine struct {
    lastHeartbeat time.Time
    restartFn     func()
}

// 被监控的 goroutine 定期报告心跳
func (m *GoroutineMonitor) Heartbeat(name string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if w, ok := m.watched[name]; ok {
        w.lastHeartbeat = time.Now()
    }
}

// 检测线程定期检查
func (m *GoroutineMonitor) checkAll() {
    m.mu.Lock()
    defer m.mu.Unlock()
    for name, w := range m.watched {
        if time.Since(w.lastHeartbeat) > 30*time.Second {
            log.Error().Str("goroutine", name).Msg("心跳超时，尝试重启")
            go w.restartFn()
        }
    }
}
```

### 10.2 自愈规则

- PlaybackGoroutine panic → recover + 1 秒后重启 + 从当前断点恢复
- FixTimeGoroutine panic → recover + 重启 + 重新初始化定时任务
- BASS 引擎异常 → 尝试重新 Init
- 所有自愈操作记录 Error 级别日志并通知前端

---

## 十一、代码风格规范

### 11.1 命名规范

遵循 Go 标准命名约定：

| 类型 | 格式 | 示例 |
|------|------|------|
| 包名 | 小写单词 | `core`, `audio`, `models` |
| 导出类型 | PascalCase | `PlayThread`, `FixTimeManager` |
| 非导出类型 | camelCase | `playListItem`, `taskEntry` |
| 导出方法 | PascalCase | `PlayNextClip()`, `Start()` |
| 非导出方法 | camelCase | `findNextProgram()`, `doPlay()` |
| 常量 | PascalCase 或 UPPER_SNAKE | `MaxRetryCount`, `StatusAuto` |
| 接口 | 以 -er 结尾（单方法）| `Switcher`, `Player` |
| 枚举 | 类型名 + 值 | `StatusAuto`, `StatusManual` |

### 11.2 注释语言

**所有注释使用中文**，与原始 C# 代码保持一致：

```go
// PlayNextClip 播放下一条素材
// 决策树：检查定时任务 → 查找下条 → 预卷 → 播出
func (pt *PlayThread) PlayNextClip(force bool, taskType TaskType) bool {
    // 检查是否临近定时事件
    if pt.isNearFixTask(500) {
        return false // 临近定时，不切换
    }
    // ...
}
```

### 11.3 项目结构

```
playthread/
├── cmd/                       # 可执行入口
│   ├── playthread/            # 主控进程
│   │   └── main.go            # 主控入口
│   └── audio-service/         # 播放服务子进程
│       └── main.go            # 播放服务入口
│
├── config.yaml                # 配置文件
├── go.mod / go.sum            # 依赖管理
│
├── core/                      # 业务调度层（主控进程）
│   ├── play_thread.go         # 主编排 goroutine
│   ├── state_machine.go       # 六状态状态机
│   ├── fix_time_manager.go    # 定时任务调度
│   ├── blank_manager.go       # 垫乐管理
│   ├── intercut_manager.go    # 插播管理
│   ├── channel_hold.go        # 通道保持
│   └── event_bus.go           # 事件总线
│
├── bridge/                    # 音频桥接层（IPC 客户端，主控进程使用）
│   ├── audio_bridge.go        # IPC 客户端封装
│   ├── audio_bridge_test.go
│   ├── process_manager.go     # 子进程生命周期管理
│   ├── process_manager_test.go
│   └── protocol.go            # IPC 协议定义（请求/响应/事件）
│
├── audio/                     # 音频引擎层（播放服务子进程使用，Go + cgo）
│   ├── ipc_server.go          # IPC 服务端（stdin/stdout）
│   ├── bass_bindings.go       # BASS cgo 低层绑定
│   ├── bass_engine.go         # BASS 引擎生命周期 + LockOSThread + 双通道派发
│   ├── bass_engine_test.go
│   ├── virtual_channel.go     # 12 虚拟通道管理（MainOut/Preview1-7/FillBlank/TellTime/Effect/TempList）
│   ├── virtual_channel_test.go
│   ├── adapter.go             # 播卡适配器（主备设备绑定 + 自定义声卡路由）
│   ├── adapter_test.go
│   ├── level_meter.go         # 音频电平表
│   ├── recorder.go            # 音频录制
│   ├── channel_matrix.go      # 通道矩阵（输出路由管理）
│   ├── effect.go              # 音频效果（EQ/淡入淡出/响度）
│   └── libs/                  # BASS 动态库 + 头文件
│       ├── bass.h
│       ├── windows/
│       │   └── bass.dll       # Windows
│       └── linux/
│           └── libbass.so     # Linux
│
├── models/                    # 数据模型（共享）
│   ├── playlist.go            # 播表/时间块
│   ├── program.go             # 节目/素材
│   ├── enums.go               # 枚举
│   └── events.go              # 事件类型
│
├── api/                       # HTTP/WebSocket/UDP 层（主控进程）
│   ├── server.go              # HTTP 服务器
│   ├── routes.go              # 路由注册
│   ├── udp.go                 # UDP 紧急控制监听
│   ├── handlers/              # 请求处理器
│   │   ├── playback.go
│   │   ├── status.go
│   │   └── intercut.go
│   └── ws.go                  # WebSocket 推送
│
├── infra/                     # 基础设施（主控进程；platform/ 子包为双进程共享）
│   ├── config.go              # 配置加载
│   ├── logger.go              # zerolog + lumberjack
│   ├── file_cache.go          # 素材缓存
│   ├── snapshot.go            # 播放快照（冷启动恢复）
│   ├── blank_history.go       # 垫乐播放历史持久化
│   ├── monitor.go             # 内存/CPU/磁盘监控
│   └── platform/              # 跨平台适配
│       ├── platform.go        # 接口定义
│       ├── windows.go         # Windows 特定实现
│       └── linux.go           # Linux 特定实现
│
├── db/                        # 数据库层（主控进程）
│   ├── sqlite.go              # SQLite 连接管理
│   ├── migrations.go          # Schema 迁移
│   └── repos.go               # 数据仓库
│
└── tests/                     # 集成测试（单元测试与源码同目录 _test.go）
    ├── integration_test.go
    └── testdata/
```

### 11.4 文件组织原则

- 每个 `.go` 文件不超过 800 行
- 单元测试文件与源码同目录（`xxx_test.go`）
- 集成测试放 `tests/` 目录
- CGO 绑定集中在 `audio/` 包，仅在播放服务子进程中编译
- 主控进程的所有包不得导入 `"C"`，确保纯 Go 编译
- `bridge/` 包是主控进程访问音频能力的唯一入口

---

## 十二、测试规范

### 12.1 测试层级

| 层级 | 工具 | 覆盖范围 |
|------|------|---------|
| 单元测试 | `testing` + `testify` | 每个包的核心方法 |
| 竞态检测 | `go test -race` | 所有并发代码 |
| 基准测试 | `testing.B` | 热路径性能验证 |
| 集成测试 | `tests/` 目录 | 跨组件端到端 |
| 泄漏检测 | `goleak` | goroutine 泄漏 |

### 12.2 测试规范

```go
func TestStateMachine_StopToAuto(t *testing.T) {
    sm := NewStateMachine()
    path := sm.ChangeStatusTo(StatusAuto, "测试启动")
    
    assert.Equal(t, PathStop2Auto, path)
    assert.Equal(t, StatusAuto, sm.Status())
    assert.Equal(t, StatusStopped, sm.LastStatus())
}

func TestStateMachine_InvalidTransition(t *testing.T) {
    sm := NewStateMachine()
    sm.ChangeStatusTo(StatusEmergency, "")
    // 不可能直接从 Stopped 到 Emergency
    // Emergency 只能从 Auto 进入
}
```

### 12.3 基准测试（热路径）

```go
func BenchmarkFixTimeCheck(b *testing.B) {
    mgr := NewFixTimeManager()
    mgr.AddTask(newTestTask())
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        mgr.checkTasks(time.Now())
    }
}
// 目标：单次检查 < 1μs
```

---

## 十三、构建与部署规范

### 13.1 构建

```bash
# === 主控进程（纯 Go，可交叉编译） ===
# 开发构建
go build -o playthread ./cmd/playthread/

# 生产构建（去除调试符号，减小体积）
go build -ldflags="-s -w" -o playthread ./cmd/playthread/

# 交叉编译（主控进程无 cgo，可在任何平台编译）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o playthread ./cmd/playthread/
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o playthread ./cmd/playthread/

# === 播放服务子进程（Go + cgo，需要目标平台编译器） ===
CGO_ENABLED=1 go build -ldflags="-s -w" -o audio-service ./cmd/audio-service/
```

### 13.2 部署

| 平台 | 部署内容 | 说明 |
|------|---------|------|
| Windows | playthread.exe + audio-service.exe + bass.dll + config.yaml | 主控可注册为 Windows 服务 |
| Linux | playthread + audio-service + libbass.so + config.yaml | systemd 管理 |

### 13.3 Windows 服务

```go
import "golang.org/x/sys/windows/svc"

// 实现 svc.Handler 接口
type playthreadService struct{}

func (s *playthreadService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
    changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
    // 启动播出引擎...
    
    for {
        select {
        case c := <-r:
            switch c.Cmd {
            case svc.Stop, svc.Shutdown:
                changes <- svc.Status{State: svc.StopPending}
                // 优雅关闭...
                return false, 0
            }
        }
    }
}
```

---

## 十四、架构设计规范

### 14.1 双进程架构

系统采用**主控进程 + 播放服务子进程**的双进程架构。主控进程为纯 Go（无 cgo），播放服务子进程为 Go + cgo（BASS FFI）。

```
┌─────────────────────────────────────────────────┐
│           Go 主控进程（纯 Go，无 cgo）              │
│                                                 │
│ ┌──────────────────────────────────────────────┐ │
│ │ API 层 (net/http + gorilla/websocket + UDP) │ │
│ │ REST API / WebSocket / UDP 监听              │ │
│ ├──────────────────────────────────────────────┤ │
│ │ Core 层 (业务调度)                            │ │
│ │ PlayThread / StateMachine / FixTime          │ │
│ │ BlankManager / IntercutManager               │ │
│ ├──────────────────────────────────────────────┤ │
│ │ AudioBridge（IPC 客户端）                      │ │
│ │ 通过 JSON 行协议与播放服务通信                   │ │
│ ├──────────────────────────────────────────────┤ │
│ │ Infra 层 (基础设施)                           │ │
│ │ Config / Logger / FileCache / Monitor        │ │
│ ├──────────────────────────────────────────────┤ │
│ │ DB 层 (SQLite)                               │ │
│ │ Connection / Repos / Migrations              │ │
│ └──────────────────────────────────────────────┘ │
│                     │ IPC（stdin/stdout JSON 行） │
└─────────────────────┼───────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────┐
│        播放服务子进程（Go + cgo）                   │
│                                                 │
│ ┌──────────────────────────────────────────────┐ │
│ │ IPC 服务端（stdin 读命令，stdout 写结果）       │ │
│ ├──────────────────────────────────────────────┤ │
│ │ Audio 层 (BASS cgo)                          │ │
│ │ BassEngine / VirtualChannel / Effect         │ │
│ │ 12 通道管理 / 主备设备 / 加密流回调             │ │
│ └──────────────────────────────────────────────┘ │
│                                                 │
│ 崩溃 → 主控自动重启（< 2s），主控业务不中断         │
└─────────────────────────────────────────────────┘
```

**为什么必须双进程：**

- **稳定性**：BASS 是闭源 C 库，段错误无法被 Go recover 拦截。子进程崩溃不影响主控
- **跨平台**：主控进程纯 Go，可交叉编译到任何平台；只有播放服务需要平台特定编译
- **可替换**：未来替换音频引擎（BASS → GStreamer / 国产化）只需替换子进程实现
- **内存隔离**：BASS 内存泄漏不影响主控进程；定期重启子进程可清零泄漏

### 14.2 依赖方向

```
API → Core → AudioBridge（IPC 客户端）
API → Core → Models
Core → Infra
Core → DB
AudioBridge → Infra (仅 Config/Logger)
```

**禁止反向依赖（AudioBridge 不能依赖 Core，Core 不能依赖 API）**。

### 14.3 IPC 协议规范

主控进程与播放服务子进程通过 **stdin/stdout + JSON 行协议**通信：

#### 14.3.1 通信方式

```
主控进程 ──(stdin write)──→ 播放服务子进程
主控进程 ←──(stdout read)── 播放服务子进程
主控进程 ←──(stderr read)── 播放服务子进程（仅日志/崩溃信息）
```

- 每条消息为一行 JSON，以 `\n` 结尾
- 请求-响应模式：每个请求包含 `id` 字段，响应用相同 `id` 匹配
- 子进程可主动推送事件（`id` 为空）

**⚠️ 子进程 stdout 独占规则**：
- 播放服务子进程的 `os.Stdout` **只能被 IPC 引擎独占写入**
- 所有日志（zerolog）必须输出到 `os.Stderr` 或文件，**禁止写入 stdout**
- panic/recover 的输出也必须重定向到 stderr
- 子进程启动时首先执行 `log.Logger = log.Output(os.Stderr)` 确保日志不污染 IPC 通道
- 违反此规则会导致非 JSON 内容混入 stdout，主控解析失败判定为协议断开

#### 14.3.2 请求格式

```go
// 主控 → 播放服务
type IPCRequest struct {
    ID     string      `json:"id"`     // 请求 ID（UUID）
    Method string      `json:"method"` // 方法名
    Params interface{} `json:"params"` // 参数
}

// 播放服务 → 主控（响应）
type IPCResponse struct {
    ID     string      `json:"id"`     // 对应请求 ID
    Result interface{} `json:"result"` // 成功结果
    Error  *IPCError   `json:"error"`  // 错误信息（null 表示成功）
}

// 播放服务 → 主控（主动推送事件）
type IPCEvent struct {
    Event string      `json:"event"` // 事件类型
    Data  interface{} `json:"data"`  // 事件数据
    Time  time.Time   `json:"time"`  // 事件时间
}
```

#### 14.3.3 方法列表

| 方法 | 参数 | 说明 |
|------|------|------|
| `init` | `{device, freq, channels}` | 初始化 BASS 引擎 + 12 通道 |
| `load` | `{channel, path, in, out, fade_mode, encrypted}` | 加载素材到指定通道 |
| `play` | `{channel}` | 播放 |
| `stop` | `{channel, fade_ms}` | 停止（可淡出） |
| `pause` | `{channel, fade_ms}` | 暂停 |
| `resume` | `{channel}` | 恢复 |
| `set_volume` | `{channel, volume}` | 设置音量 |
| `set_eq` | `{channel, preset}` | 设置 EQ 预设 |
| `position` | `{channel}` | 查询播放位置 |
| `level` | `{channel}` | 查询音频电平 |
| `device_info` | `{}` | 查询设备列表 |
| `set_device` | `{channel, main_device, backup_device}` | 设置通道主备设备 |
| `remove_sync` | `{channel}` | 移除通道回调 |
| `free_channel` | `{channel}` | 释放通道 |
| `free_all` | `{}` | 释放所有资源 |
| `shutdown` | `{}` | 优雅退出 |
| `ping` | `{}` | 心跳检测 |

#### 14.3.4 事件列表

| 事件 | 数据 | 说明 |
|------|------|------|
| `play_finished` | `{channel}` | 素材播放完成 |
| `play_started` | `{channel, duration}` | 素材开始播放 |
| `device_lost` | `{device}` | 设备丢失 |
| `device_restored` | `{device}` | 设备恢复 |
| `level` | `{channel, left, right}` | 定时推送音频电平（5次/秒） |
| `error` | `{code, message}` | 错误事件 |

#### 14.3.5 AudioBridge 客户端

```go
// AudioBridge 封装与播放服务子进程的 IPC 通信
type AudioBridge struct {
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    stdout   *bufio.Scanner
    pending  sync.Map // id → chan *IPCResponse
    eventCh  chan *IPCEvent
    mu       sync.Mutex
}

// Call 发送请求并等待响应（超时 5s）
func (ab *AudioBridge) Call(method string, params interface{}) (*IPCResponse, error) {
    id := uuid.New().String()
    req := IPCRequest{ID: id, Method: method, Params: params}
    
    ch := make(chan *IPCResponse, 1)
    ab.pending.Store(id, ch)
    defer ab.pending.Delete(id)
    
    ab.mu.Lock()
    data, _ := json.Marshal(req)
    ab.stdin.Write(append(data, '\n'))
    ab.mu.Unlock()
    
    select {
    case resp := <-ch:
        return resp, nil
    case <-time.After(5 * time.Second):
        return nil, fmt.Errorf("IPC 调用超时: %s", method)
    }
}
```

### 14.4 子进程生命周期管理

```go
// 子进程管理器
type AudioProcessManager struct {
    execPath   string       // 播放服务可执行文件路径
    bridge     *AudioBridge
    restartCh  chan struct{}
    crashCount int          // 连续崩溃计数
    mu         sync.Mutex
}

// Start 启动播放服务子进程
func (m *AudioProcessManager) Start(ctx context.Context) error {
    cmd := exec.CommandContext(ctx, m.execPath)
    // stdin/stdout 管道通信
    // stderr 重定向到日志文件
}

// watchdog 监控子进程状态，崩溃自动重启
func (m *AudioProcessManager) watchdog(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-m.restartCh:
            m.mu.Lock()
            m.crashCount++
            if m.crashCount > 5 {
                // 连续崩溃 5 次以上，增加重启间隔（指数退避）
                delay := time.Duration(m.crashCount-5) * 2 * time.Second
                if delay > 30*time.Second {
                    delay = 30 * time.Second
                }
                time.Sleep(delay)
            }
            m.restartSubprocess(ctx)
            m.mu.Unlock()
        }
    }
}
```

### 14.5 接口隔离

核心组件之间通过接口交互，便于测试和替换：

```go
// 播放器接口（Core 层定义，AudioBridge 实现）
type Player interface {
    Load(channel ChannelType, clip *models.PlayClip) error
    Play(channel ChannelType) error
    Stop(channel ChannelType, fadeMs int) error
    Pause(channel ChannelType, fadeMs int) error
    Resume(channel ChannelType) error
    Position(channel ChannelType) (int, error)
    SetVolume(channel ChannelType, vol float64) error
    SetEQ(channel ChannelType, preset string) error
    Level(channel ChannelType) (float64, float64, error)
    Ping() error // 心跳检测
}

// 信号切换器接口（Core 层定义，Infra 层实现）
type Switcher interface {
    SwitchPgm(signalID int) error
    SwitchPst(signalID int) error
}
```

---

## 十五、状态机严格规范

### 15.1 唯一入口

状态变更只能通过 `StateMachine.ChangeStatusTo()` 方法执行。**禁止直接修改状态字段**。

### 15.2 合法路径验证

**重要：校验责任在调用者（PlayThread），不在 StateMachine 本身**。

C# 原版 `SlvcStatusController.ChangeStatusTo()` 是**直接赋值**，不做任何合法性校验。校验逻辑在 `SlvcPlayThread.WorkThread` 中通过两步完成：

1. 先调用 `GetPath()` 获取路径枚举
2. 判断路径是否为 `ErrPath`，是则拒绝迁移

Go 实现中 `StateMachine` 的设计选择：
- **方案 A（推荐）**：`ChangeStatusTo()` 内置路径校验，非法路径返回错误——简化调用者逻辑，但与 C# 行为不完全一致
- **方案 B（严格对齐）**：`ChangeStatusTo()` 直接赋值（与 C# 一致），校验由 `PlayThread` 在调用前执行

**当前采用方案 A**，因为 Go 的错误处理范式天然支持这种"校验+变更原子化"的模式，且可避免遗漏校验。但必须注意：C# 中某些场景（如 `Stopped → Stopped`）会映射为 `Stop2Auto`（兜底行为），Go 实现必须保留此兜底逻辑。

### 15.3 状态变更日志

每次合法变更必须记录 Info 级别日志：
```
状态变更: Auto → Manual, 路径: Auto2Manual, 原因: 操作员手动切换
```

### 15.4 事件通知

状态变更后必须通知所有订阅者（通过 channel 或回调）。事件通知在锁外执行，防止死锁。

### 15.5 双 goroutine 事件循环模型（对齐 C# PlaybackThread + WorkThread）

C# 原版使用两个独立线程处理不同事件集，Go 版用两个独立 goroutine 对齐：

| goroutine | 对应 C# 线程 | 职责 | 事件源 |
|-----------|-------------|------|--------|
| `playbackLoop` | PlaybackThread (Highest) | 处理 play_finished → 按状态分发 PlayNextClip | IPC play_finished 事件 |
| `workLoop` | WorkThread (AboveNormal) | 处理状态迁移 + 通道空闲 + 定时到达 | API 指令 / 定时管理器 / 通道空闲事件 |

**关键约束**：
- 两个 goroutine 各自独立运行，通过 channel 接收事件
- `playbackLoop` 在 Emergency 状态下调用独立的 `playNextEmrgClip()`（而非通用的 `playNextClip()`）
- 两者之间通过共享的 `inPlayNext` 原子标志互斥（见下方 §15.6）

### 15.6 定时任务与 PlayNext 互斥机制

定时任务到达时，需要等待当前 PlayNextClip 完成（防止同时操作播出状态）：

```go
// 定时任务到达处理
func (pt *PlayThread) onFixTimeArrived(task *FixTimeTask) {
    // 等待 PlayNext 完成，最多 500ms
    for i := 0; i < 50 && pt.inPlayNext.Load(); i++ {
        time.Sleep(10 * time.Millisecond)
    }
    pt.inFixTime.Store(true)
    defer pt.inFixTime.Store(false)
    // ... 执行定时切换逻辑
}

// PlayNextClip 中检查定时标志
func (pt *PlayThread) playNextClip(force bool) bool {
    if pt.inFixTime.Load() {
        return false // 定时任务正在执行，不抢占
    }
    pt.inPlayNext.Store(true)
    defer pt.inPlayNext.Store(false)
    // ... 播出逻辑
}
```

### 15.7 软定时可被打断（SoftFixWaiting）

软定时等待期间设置 `SoftFixWaiting = true`。如果此时用户播放 Jingle、临时单或手动切换状态，需取消当前软定时等待：

```go
if pt.softFixWaiting.Load() {
    pt.cancelSoftFix() // 取消软定时等待
}
```

### 15.8 淡出暂停（FadePause）

定时任务切换时，当前播放不一定是"淡出停止"，可能是"淡出暂停"——降到目标音量后暂停流（不释放），定时节目播完后可恢复继续播放。Go 版通过 IPC 的 volume + pause 组合实现。

---

## 十六、背压与限流规范

### 16.1 channel 缓冲

所有 channel 使用有限缓冲，防止生产者过快导致内存无限增长：

```go
evtPlayFinished := make(chan struct{}, 1) // 缓冲1，防连续播完丢失
cmdCh := make(chan BassCommand, 16)       // BASS 命令队列
progressCh := make(chan ProgressUpdate, 1) // 进度更新，允许丢弃旧值
```

### 16.2 WebSocket 推送限流

进度更新频率限制在 1 次/秒，状态变更立即推送。

---

## 十七、磁盘与文件系统安全规范

### 17.1 原子写入

配置文件、快照文件使用「写临时文件 → 原子重命名」模式：

```go
func atomicWriteFile(path string, data []byte) error {
    tmpPath := path + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmpPath, path)
}
```

### 17.2 磁盘空间监控

每小时检查磁盘可用空间，低于阈值时告警并停止下载。

---

## 十八、音频引擎隔离与硬件防护规范

### 18.1 进程级隔离

BASS 音频引擎运行在**独立的播放服务子进程**中，与 Go 主控进程通过 IPC 通信（见 §14.3）。
子进程内部使用 `runtime.LockOSThread()` + 双通道（ctrlCh + ioCh）设计，确保 BASS 线程亲和性和命令优先级。

**隔离收益：**
- BASS C 层段错误 → 子进程崩溃，主控存活，< 2s 自动重启
- BASS 内存泄漏 → 定期重启子进程即可清零
- 声卡驱动异常 → 重启子进程重新初始化设备

### 18.2 子进程崩溃恢复

```go
// 主控进程中的恢复流程
func (m *AudioProcessManager) onSubprocessCrash() {
    // 1. 记录崩溃日志（含 stderr 输出）
    // 2. 递增 crashCount
    // 3. 指数退避重启（前 5 次立即重启，之后间隔递增，最大 30s）
    // 4. 重新初始化 AudioBridge
    // 5. 重新发送 init 命令（设备列表 + 通道配置）
    // 6. 如果有正在播放的素材，重新加载并 seek 到崩溃前位置
}
```

### 18.3 设备热拔插

```go
// 播放服务子进程内部：定期检测声卡设备状态
func (be *BassEngine) checkDeviceHealth() error {
    // BASS_GetDeviceInfo 检查设备是否仍然可用
    // 设备丢失时推送 device_lost 事件到主控
    // 主控决定是否重新初始化或切换备用设备
}
```

---

## 十九、冷启动恢复与幂等性规范

### 19.1 PlayingInfo 快照

每 5 秒原子写入当前播出状态（JSON 文件）：

```go
type PlayingInfo struct {
    ProgramID    string    `json:"program_id"`
    Position     int       `json:"position"`      // 播放位置(ms)
    SystemTime   time.Time `json:"system_time"`    // 快照时间戳
    Status       Status    `json:"status"`
    SignalID     int       `json:"signal_id"`
    IsCutPlaying bool      `json:"is_cut_playing"`
}
```

### 19.2 恢复流程

```
1. 读取 PlayingInfo 快照
2. 计算时间偏移 = Position + (now - SystemTime)
3. 入点容错（< 2s 从头，< 1s 强制 1s）
4. 预卷到计算后的位置
5. 恢复状态机到快照状态
6. 开始播出
```

---

## 二十、多平台适配规范

### 20.1 构建标签

```go
// infra/platform/windows.go
//go:build windows

package platform

func setHighPriority() {
    // Windows: SetPriorityClass + SetThreadPriority
}

func preventSleep() {
    // SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED)
}

// enableHighResTimer 启用 Windows 高精度定时器（1ms 分辨率）
// Windows 默认定时器精度为 15.6ms，会导致 20ms Ticker 实际偏差到 31.25ms
// 必须在程序启动时调用，退出时调用 disableHighResTimer 恢复
func enableHighResTimer() {
    winmm := syscall.NewLazyDLL("winmm.dll")
    timeBeginPeriod := winmm.NewProc("timeBeginPeriod")
    timeBeginPeriod.Call(uintptr(1)) // 设置系统定时器精度为 1ms
}

func disableHighResTimer() {
    winmm := syscall.NewLazyDLL("winmm.dll")
    timeEndPeriod := winmm.NewProc("timeEndPeriod")
    timeEndPeriod.Call(uintptr(1))
}
```

```go
// infra/platform/linux.go
//go:build linux

package platform

func setHighPriority() {
    // nice -10 / sched_setscheduler
}

func preventSleep() {
    // systemd-inhibit 或 DBus
}
```

### 20.2 时间校准策略

定时任务调度对系统时间敏感，需处理以下时间问题：

```go
// infra/timesync.go

// 时间间隔计算必须使用单调时钟（monotonic clock）
// time.Now() 返回的 Time 同时携带 wall clock 和 monotonic reading
// time.Since() / time.Until() 自动使用 monotonic，不受 NTP 跳变影响
elapsed := time.Since(startTime) // ✅ 使用 monotonic
deadline := time.Until(taskTime) // ✅ 使用 monotonic

// 绝对时间比较（定时任务触发判断）使用 wall clock
// 此时需容忍 NTP 调时导致的跳变
if now.After(task.TriggerTime) {
    // 触发，但检查是否因 NTP 回拨导致已过期任务重复触发
    if now.Sub(task.TriggerTime) > 5*time.Minute {
        log.Warn().Time("trigger", task.TriggerTime).Msg("任务已过期超过5分钟，跳过")
        return
    }
}
```

**规则**：
- 间隔/超时：用 `time.Since()` / `time.Until()`（单调时钟）
- 定时触发：用 `time.Now()` wall clock 比较，但加过期保护（> 5min 跳过）
- 通道保持超时：用单调时钟，NTP 跳变不影响超时判断

### 20.3 BASS 库路径

通过构建标签和 cgo LDFLAGS 指令自动切换平台库：

```go
// bass_windows.go
//go:build windows

/*
#cgo LDFLAGS: -L${SRCDIR}/libs/windows -lbass
*/
import "C"
```

---

## 二十一、数据库规范

### 21.1 SQLite 配置

```go
import _ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无 cgo 依赖

func openDB(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
    if err != nil {
        return nil, err
    }
    db.SetMaxOpenConns(1) // SQLite 单写入连接
    return db, nil
}
```

### 21.2 写入串行化

SQLite 不支持并发写入。所有写操作通过单一写入 channel 串行化：

```go
type DBWriter struct {
    writeCh chan func(*sql.DB) error
}

func (w *DBWriter) Write(fn func(*sql.DB) error) error {
    errCh := make(chan error, 1)
    w.writeCh <- func(db *sql.DB) error {
        err := fn(db)
        errCh <- err
        return err
    }
    return <-errCh
}
```

### 21.3 Schema 迁移

使用 `user_version` PRAGMA 驱动迁移：

```go
func migrate(db *sql.DB) error {
    var version int
    db.QueryRow("PRAGMA user_version").Scan(&version)
    
    migrations := []string{
        `CREATE TABLE IF NOT EXISTS play_history (...)`,
        `CREATE TABLE IF NOT EXISTS settings (...)`,
        // ...
    }
    
    for i := version; i < len(migrations); i++ {
        if _, err := db.Exec(migrations[i]); err != nil {
            return fmt.Errorf("迁移 v%d 失败: %w", i+1, err)
        }
        db.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1))
    }
    return nil
}
```

---

> **总结**：本规范针对 Go 语言特性（goroutine、cgo、gc、interface、channel）和广播播出系统的 7×24×365 运行要求量身定制。核心差异点：
> - **cgo + BASS 线程亲和性** → 专用 LockOSThread goroutine + channel 派发
> - **无 GIL** → 真正并行，锁纪律更重要
> - **Go GC (STW < 0.5ms)** → 对 20ms 定时器透明，无需 gc.disable() 等技巧
> - **编译期类型安全** → 减少运行时崩溃风险
> - **单二进制部署** → 简化运维
