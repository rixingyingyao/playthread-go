# Go 编码纪律技能

适用于 playthread-go 项目中所有 Go 代码的编写。本技能定义了面向 7×24×365 长运行广播系统的编码标准和防御性编程要求。

**核心理念：代码必须在无人值守的生产环境中安全运行数月。每个函数都可能在凌晨 3 点被调用。**

## 触发条件

编写或修改任何 Go 代码时都应遵守本技能。

---

## 1. 错误处理

### 1.1 绝不忽略错误

```go
// ❌ 致命：忽略错误
data, _ := json.Marshal(obj)
os.WriteFile(path, data, 0o644)

// ✅ 正确：检查并处理每个错误
data, err := json.Marshal(obj)
if err != nil {
    return fmt.Errorf("序列化失败: %w", err)
}
if err := os.WriteFile(path, data, 0o644); err != nil {
    return fmt.Errorf("写文件失败 %s: %w", path, err)
}
```

**唯一允许忽略错误的场景：**
```go
// 1. 清理路径上的删除（失败也无影响）
_ = os.Remove(tmpFile)

// 2. 日志写入失败（无法记录"日志失败"）
// 3. defer Close 中的错误（已有主错误返回）
defer func() { _ = f.Close() }()
```

### 1.2 错误包装（Error Wrapping）

```go
// ✅ 使用 %w 包装，保留错误链
if err := db.Write(fn); err != nil {
    return fmt.Errorf("保存节目 %s: %w", prog.ID, err)
}

// 调用链追踪：
// "保存节目 P001: 写入失败: database is locked"
//   ↑ 业务层      ↑ 数据层      ↑ 底层驱动
```

**规则：**
- 每层添加当前操作的上下文
- 包含关键标识符（ID、名称、路径）
- 使用 `%w` 而非 `%v`（保留 errors.Is/As 链）

### 1.3 错误日志

```go
// ✅ 结构化日志（zerolog）
log.Error().
    Err(err).
    Str("method", method).
    Str("program_id", prog.ID).
    Int64("position_ms", posMs).
    Msg("播放失败")

// ❌ 不要：裸打
log.Error().Msg(err.Error())  // 没有上下文
fmt.Println("error:", err)     // stdout 是 IPC 通道！
```

**日志规则：**
- **永远不向 stdout 写日志**（stdout 用于 IPC 通信）
- 所有日志走 zerolog → stderr + 文件
- CGO 回调内用 `fmt.Fprintf(os.Stderr, ...)` （zerolog 可能 panic）
- 包含足够的调试上下文（ID、状态、位置）

---

## 2. 函数设计

### 2.1 Context 第一参数

```go
// ✅ 正确
func (pt *PlayThread) Run(ctx context.Context) error { ... }
func (be *BassEngine) execCtrl(ctx context.Context, fn func() interface{}) interface{} { ... }

// ❌ 错误
func (pt *PlayThread) Run(timeout int) { ... }  // 应该用 ctx 管理超时
```

### 2.2 返回值而非修改参数

```go
// ✅ 返回新值
func buildRequest(method string, params interface{}) (*IPCRequest, error) {
    return &IPCRequest{ID: uuid.New().String(), Method: method, ...}, nil
}

// ❌ 通过指针参数修改
func buildRequest(req *IPCRequest, method string) { ... }
```

### 2.3 方法接收者

```go
// 值接收者：无状态、小对象、只读操作
func (s Status) String() string { ... }

// 指针接收者：有状态、大对象、需要修改
func (pt *PlayThread) playNextClip() { ... }

// ⚠️ 规则：同一类型的方法要么全用值接收者，要么全用指针接收者
```

### 2.4 Defer 使用

```go
// ✅ 习惯用法：资源获取后立即 defer 释放
mu.Lock()
defer mu.Unlock()

f, err := os.Open(path)
if err != nil { return err }
defer f.Close()

// ⚠️ 注意 defer 在循环中的行为
for _, path := range paths {
    f, err := os.Open(path)
    if err != nil { continue }
    // defer f.Close()  ← 不会在循环迭代结束时执行！
    // 应该：
    processFile(f)  // 在子函数中 defer
    f.Close()       // 或者手动关闭
}
```

---

## 3. 并发编码纪律

### 3.1 结构体字段保护声明

在结构体定义中用注释标明保护策略：

```go
type PlayThread struct {
    cfg          *infra.Config      // 不变量，无需保护
    stateMachine *StateMachine      // 内部自带锁
    audioBridge  *bridge.AudioBridge // 线程安全

    // mu 保护以下字段
    mu          sync.RWMutex
    playlist    *models.Playlist
    currentPos  int
    currentProg *models.Program

    // 原子操作
    inPlayNext     atomic.Bool
    softFixWaiting atomic.Bool
}
```

### 3.2 一个字段一种保护

```go
// ❌ 混用保护机制
type Bad struct {
    mu sync.Mutex
    count atomic.Int64  // 到底用 mu 还是 atomic？
}
func (b *Bad) Inc() {
    b.mu.Lock()
    b.count.Add(1)  // ← atomic 和 mu 都在保护同一个值？
    b.mu.Unlock()
}

// ✅ 清晰
type Good struct {
    count atomic.Int64  // 只用 atomic
}
func (g *Good) Inc() {
    g.count.Add(1)
}
```

### 3.3 锁的最小范围

```go
// ✅ 只锁必要操作，拿到数据后释放
pt.mu.RLock()
prog := pt.currentProg
pos := pt.currentPos
pt.mu.RUnlock()

// 在锁外处理
if prog != nil {
    pt.audioBridge.Call("play", prog.ToParams())
}

// ❌ 在锁内做重活
pt.mu.Lock()
resp, _ := pt.audioBridge.Call("position", nil)  // IPC 可能耗时 5s
pt.currentPos = resp.Position
pt.mu.Unlock()
```

---

## 4. 类型和接口

### 4.1 优先使用具体类型

```go
// ✅ 本项目的选择：AudioBridge 是具体 struct，不是 interface
type AudioBridge struct { ... }

// 测试 mock 不是通过接口，而是通过 io.Pipe() 替换底层通信
// 这避免了接口膨胀，且生产代码零开销
```

### 4.2 接口发现原则

```go
// Go 接口原则："接受接口，返回具体类型"
// 只在确实需要多态行为时定义接口
// 不要为了"可测试性"给每个 struct 抽接口

// ✅ 用接口的好场景
type io.Writer interface { Write([]byte) (int, error) }  // 标准库级别的抽象

// ❌ 不必要的接口
type AudioBridgeInterface interface {
    Call(method string, params interface{}) (*IPCResponse, error)
    Shutdown() error
    Ping() error
    // ... 1:1 映射 AudioBridge 的所有方法
}
```

### 4.3 枚举模式

```go
// 使用 iota + String() 方法
type Status int
const (
    StatusStopped Status = iota
    StatusAuto
    StatusManual
    StatusLive
    StatusRedifDelay
    StatusEmergency
)

func (s Status) String() string {
    switch s {
    case StatusStopped: return "Stopped"
    case StatusAuto: return "Auto"
    // ...
    default: return fmt.Sprintf("Unknown(%d)", int(s))
    }
}
```

---

## 5. 测试编码

### 5.1 测试文件组织

```go
// 文件命名：xxx_test.go
// 函数命名：Test_结构体_方法_场景

func TestStateMachine_ChangeStatusTo_ValidPath(t *testing.T) { ... }
func TestStateMachine_ChangeStatusTo_InvalidPath(t *testing.T) { ... }
func TestPlayThread_Integration_AutoPlaySequence(t *testing.T) { ... }
```

### 5.2 测试辅助函数

```go
// 辅助函数以 test/new/make 前缀开头，接受 *testing.T
func testConfig() *infra.Config { ... }
func testPlaylist(n int) *models.Playlist { ... }
func newMockAudioBridge(posMs, durMs int) (*bridge.AudioBridge, *mockIPC) { ... }

// 使用 t.Helper() 标记辅助函数（错误行号追溯到调用方）
func assertStatus(t *testing.T, sm *StateMachine, expected Status) {
    t.Helper()
    assert.Equal(t, expected, sm.Status())
}
```

### 5.3 测试时间参数

```go
// ✅ 测试中缩短所有定时参数
func testConfig() *infra.Config {
    cfg := infra.DefaultConfig()
    cfg.Playback.PollingIntervalMs = 10   // 生产 20ms → 测试 10ms
    cfg.Playback.TaskExpireMs = 200       // 生产 5000ms → 测试 200ms
    cfg.Playback.HardFixAdvanceMs = 5     // 生产 1000ms → 测试 5ms
    cfg.Audio.FadeOutMs = 10              // 生产 500ms → 测试 10ms
    return cfg
}
```

### 5.4 异步断言

```go
// ❌ 不要用 time.Sleep 等待异步操作
time.Sleep(100 * time.Millisecond)
assert.Equal(t, StatusAuto, sm.Status())

// ✅ 使用轮询 + 超时
func waitForStatus(sm *StateMachine, target Status, timeout time.Duration) bool {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if sm.Status() == target {
            return true
        }
        time.Sleep(5 * time.Millisecond)
    }
    return false
}

require.True(t, waitForStatus(sm, StatusAuto, 2*time.Second), "状态未变为 Auto")
```

---

## 6. 包和导入

### 6.1 包依赖方向

```
models ← infra ← db ← core ← bridge ← api
       （数据流从左到右，依赖方向从右到左）

规则：
- models 不依赖任何项目包（纯数据）
- infra 只依赖 models
- core 依赖 models, infra, bridge（不依赖 audio）
- audio 独立运行于子进程
```

### 6.2 导入分组

```go
import (
    // 标准库
    "context"
    "fmt"
    "sync"

    // 第三方库
    "github.com/rs/zerolog/log"
    "github.com/stretchr/testify/assert"

    // 项目内部
    "playthread-go/models"
    "playthread-go/infra"
)
```

---

## 7. 命名约定

### 7.1 本项目命名规范

| 类别 | 规则 | 示例 |
|------|------|------|
| goroutine 函数 | Loop/Watch/Serve 后缀 | `playbackLoop`, `workLoop`, `watchProcess` |
| channel 字段 | Ch 后缀 | `ctrlCh`, `ioCh`, `eventCh`, `closedCh`, `writeCh` |
| cancel 函数 | cancel 前缀 | `cancelCtx`, `cancelSoftFix` |
| 互斥锁 | `mu` (单一) / `xxxMu` (多个) | `mu`, `writeMu` |
| 原子标志 | in/is 前缀 | `inPlayNext`, `inFixTime`, `cutPlaying` |
| 管理器 | Mgr 后缀 | `fixTimeMgr`, `blankMgr`, `intercutMgr`, `snapshotMgr` |
| 配置 | Config 后缀 | `PlaybackConfig`, `AudioConfig` |
| 事件 | Event 后缀 | `PlayFinishedEvent`, `StatusChangeCmd` |
| IPC 方法 | snake_case 字符串 | `"play_finished"`, `"set_volume"` |
| 常量 | 驼峰式 | `StatusAuto`, `TaskTypeHard` |

### 7.2 变量作用域与命名

```go
// 短作用域用短名
for i, p := range programs { ... }
if err := doSomething(); err != nil { ... }

// 长作用域用描述性名称
currentProgram := pt.getCurrentProg()
recoveryPosition := snapshot.CalcRecoveryPosition(info)
```

---

## 8. 文件操作

### 8.1 原子写入模式

```go
// 长运行系统中写文件必须原子操作，防止写到一半宕机导致数据损坏
func atomicWrite(path string, data []byte) error {
    tmpPath := path + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
        return err
    }
    // Windows 特殊处理：Rename 前必须先删目标
    _ = os.Remove(path)
    return os.Rename(tmpPath, path)
}
```

### 8.2 路径处理

```go
// ✅ 使用 filepath 包处理路径
fullPath := filepath.Join(baseDir, "data", filename)

// ❌ 不要手动拼接
fullPath := baseDir + "/" + filename  // Windows 下 \ 和 / 混乱
```

---

## 9. 代码审查检查清单

每次提交前快速检查：

- [ ] 所有 `go func()` 有退出路径且纳入 WaitGroup？
- [ ] 所有资源（文件/连接/句柄/C 内存）有 defer 释放？
- [ ] 所有错误都已处理（无 `_ = potentially_failing_call()`）？
- [ ] 锁内没有 I/O 操作？
- [ ] 没有 time.After 在 for 循环中？
- [ ] 日志不输出到 stdout？
- [ ] 测试使用轮询断言而非 time.Sleep？
- [ ] 文件写入使用原子模式？
- [ ] panic recovery 覆盖所有 CGO 回调和顶层 goroutine？

---

## 10. 禁止事项

| 禁止 | 原因 | 替代方案 |
|------|------|----------|
| `fmt.Println()` / `log.Println()` | stdout 是 IPC 通道 | zerolog → stderr + file |
| `os.Exit()` 在非 main | 跳过 defer 清理 | 返回 error，main 处理 |
| `panic()` 在非初始化 | 进程崩溃 | 返回 error |
| `go func() { for { ... } }()` 无退出 | goroutine 泄漏 | select + ctx.Done() |
| `time.Sleep()` 在生产代码 | 无法响应取消 | select + time.After + ctx.Done |
| 全局 mutable 变量 | 并发不安全 | 结构体字段 + 锁 |
| `init()` 函数 | 隐式副作用、测试困难 | 显式初始化函数 |
| `sync.Mutex` 值拷贝 | 锁失效 | 始终用指针传递 |
| `interface{}` / `any` 滥用 | 类型不安全 | 具体类型或泛型 |
