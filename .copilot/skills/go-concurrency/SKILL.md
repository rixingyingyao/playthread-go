# Go 并发编程技能

适用于 playthread-go 项目中的所有 Go 并发模式，涵盖 goroutine 生命周期管理、channel 模式、同步原语、Context 传播和竞态检测。

## 触发条件

当任务涉及以下内容时使用本技能：
- 启动/管理 goroutine
- 使用 channel 进行通信
- 使用 sync 包的原语（Mutex/RWMutex/WaitGroup/atomic/sync.Map）
- Context 传播与取消
- 并发测试或竞态排查

---

## 1. Goroutine 生命周期管理

### 1.1 必须可退出

每个 goroutine 都必须有明确的退出路径。**绝不允许"发射即忘"的无限循环 goroutine。**

```go
// ✅ 正确：通过 ctx.Done() 退出
func (pt *PlayThread) playbackLoop(ctx context.Context) {
    defer pt.wg.Done()
    ticker := time.NewTicker(time.Duration(pt.cfg.Playback.PollingIntervalMs) * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            pt.pollPosition()
        case <-ctx.Done():
            return
        }
    }
}

// ❌ 错误：无退出路径
go func() {
    for {
        time.Sleep(time.Second)
        doSomething()
    }
}()
```

### 1.2 WaitGroup 跟踪

每个由结构体管理的 goroutine 都应纳入 `sync.WaitGroup` 跟踪：

```go
pt.wg.Add(1)
go pt.playbackLoop(ctx)

// 在 Stop/Close 中等待
cancel()    // 先取消 context
pt.wg.Wait() // 再等待所有 goroutine 退出
```

### 1.3 fade-out 等耗时 goroutine 的退出边界

短生命期的 goroutine（如 fade-out 后停止播放）也必须感知 context 取消：

```go
// ✅ 正确：select 感知 ctx 取消
go func() {
    select {
    case <-time.After(fadeOut + 50*time.Millisecond):
        be.execCtrl(func() interface{} {
            BassChannelStop(handle)
            return nil
        })
    case <-be.ctx.Done():
        return // 引擎已关闭，不再执行
    }
}()

// ❌ 错误：time.Sleep 不感知 ctx
go func() {
    time.Sleep(fadeOut + 50*time.Millisecond)
    be.execCtrl(...) // 如果 ctx 已取消，execCtrl 发送到 ctrlCh 无人消费 → 死锁
}()
```

### 1.4 runtime.LockOSThread

用于绑定 C 库（如 BASS）到固定 OS 线程的场景：

```go
func (be *BassEngine) Run(ctx context.Context) {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    // 所有 BASS API 调用都通过 channel 派发到此 goroutine
}
```

**关键规则：**
- 锁线程的 goroutine 一旦启动就不应退出直到组件生命周期结束
- 通过 channel 派发所有需要在该线程执行的操作
- defer UnlockOSThread() 确保退出时释放

---

## 2. Channel 模式

### 2.1 双通道优先级路由

本项目的核心模式：控制命令优先于 IO 命令。

```go
for {
    // 第一级 select：仅检查高优先级
    select {
    case cmd := <-be.ctrlCh:
        result := cmd.fn()
        cmd.resultCh <- result
    default:
        // 第二级 select：同时监听两个通道 + 退出信号
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
```

**注意事项：**
- 外层 default 确保 ctrlCh 有消息时始终优先处理
- 内层 select 在无控制命令时才会处理 IO 命令
- 必须包含 ctx.Done() 退出路径

### 2.2 命令-响应模式（同步 RPC over channel）

```go
type bassCommand struct {
    fn       func() interface{}
    resultCh chan interface{} // buffered(1) 防止发送方 goroutine 泄漏
}

func (be *BassEngine) execCtrl(fn func() interface{}) interface{} {
    ch := make(chan interface{}, 1) // ← buffer=1 很关键
    be.ctrlCh <- bassCommand{fn: fn, resultCh: ch}
    return <-ch
}
```

**关键：** `resultCh` 必须 buffered(1)，否则如果调用方因 timeout 放弃等待，发送到 `resultCh` 的结果会导致 BASS 线程阻塞。

### 2.3 事件广播（EventBus 模式）

```go
type EventBus struct {
    subscribers sync.Map // map[EventType][]chan Event
}

func (eb *EventBus) Broadcast(evt Event) {
    // 使用 select + default 防止慢消费者阻塞广播
    select {
    case ch <- evt:
    default:
        log.Warn().Msg("事件通道已满，丢弃事件")
    }
}
```

### 2.4 超时选择

```go
select {
case resp := <-respCh:
    return resp, nil
case <-time.After(ab.timeout):
    return nil, fmt.Errorf("IPC 超时: %s", method)
case <-ab.closedCh:
    return nil, fmt.Errorf("连接已关闭")
}
```

### 2.5 信号量模式（tryLock）

用 buffered channel 实现非阻塞互斥：

```go
playNextLock: make(chan struct{}, 1)

// TryLock
select {
case pt.playNextLock <- struct{}{}:
    defer func() { <-pt.playNextLock }()
    // 执行 playNext 逻辑
default:
    return // 已有 playNext 在执行，跳过
}
```

---

## 3. 同步原语最佳实践

### 3.1 sync.RWMutex

- 只读操作用 `RLock/RUnlock`（允许并发读）
- 写操作用 `Lock/Unlock`
- **绝不在持锁时执行 channel 操作或网络调用**（死锁风险）

```go
pt.mu.RLock()
prog := pt.currentProg
pt.mu.RUnlock()
// 拿到值后在锁外使用
```

### 3.2 sync/atomic

用于简单标志位，避免 mutex 开销：

```go
var inPlayNext atomic.Bool

if !pt.inPlayNext.CompareAndSwap(false, true) {
    return // 已在 playNext 中
}
defer pt.inPlayNext.Store(false)
```

`atomic.Value` 用于存储需要原子读写的复杂值：
```go
var lastSwitchTime atomic.Value // 存 time.Time
pt.lastSwitchTime.Store(time.Now())
```

### 3.3 sync.Map

用于高并发的 map 操作（读多写少场景最佳）：

```go
ab.pending.Store(id, respCh)  // 注册等待通道
defer ab.pending.Delete(id)    // 请求完成后清理
```

### 3.4 sync.Once

确保初始化代码只执行一次：
```go
var initOnce sync.Once
initOnce.Do(func() {
    // 初始化逻辑
})
```

---

## 4. Context 传播

### 4.1 规则

1. **Context 必须作为函数第一个参数传递**（`func Foo(ctx context.Context, ...)`）
2. **长生命期组件存储 context**（如 `BassEngine.ctx`）供内部 goroutine 使用
3. **子操作使用派生 context**：

```go
// 软定时等待：可取消的子 context
softCtx, cancelSoft := context.WithCancel(ctx)
pt.cancelSoftFix = cancelSoft
go pt.waitSoftFix(softCtx, task)
```

### 4.2 取消层级

```
主 ctx (Run 的参数)
├── fixTimeMgr 的内部 ctx
├── blankMgr 的内部 ctx
├── playbackLoop goroutine
├── workLoop goroutine
└── softFixCtx (随时可取消)
```

取消主 ctx 会级联取消所有子 ctx。

---

## 5. 并发测试

### 5.1 竞态检测

```bash
go test -race ./core/...
```

**注意：** `-race` 需要 CGO（mingw-w64 v8+）。仅在有 gcc 的环境中使用。

### 5.2 测试中的时间控制

缩短所有定时参数以加速测试：

```go
func testConfig() *infra.Config {
    cfg := infra.DefaultConfig()
    cfg.Playback.PollingIntervalMs = 10
    cfg.Playback.TaskExpireMs = 200
    cfg.Playback.HardFixAdvanceMs = 5
    return cfg
}
```

### 5.3 等待异步状态变更

用轮询 + 超时代替 sleep：

```go
func waitForStatus(sm *StateMachine, target models.Status, timeout time.Duration) bool {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if sm.Status() == target {
            return true
        }
        time.Sleep(5 * time.Millisecond)
    }
    return false
}
```

### 5.4 Drain 广播通道

测试中必须消费广播事件防止 channel 阻塞：

```go
func drainBroadcast(eb *EventBus) {
    ch := eb.Subscribe(AllEvents)
    go func() {
        for range ch {} // 持续消费直到通道关闭
    }()
}
```

---

## 6. 常见陷阱速查

| 陷阱 | 表现 | 解决方案 |
|------|------|----------|
| goroutine 泄漏 | 内存持续增长 | 确保每个 goroutine 有 ctx.Done() 退出路径 |
| channel 死锁 | 程序挂起 | resultCh 使用 buffered(1)；select 加 default |
| 锁内调 channel | 死锁 | 先释放锁，再操作 channel |
| time.Sleep 阻塞 | 无法响应取消 | 改用 select + time.After + ctx.Done |
| atomic 与 mutex 混用 | 数据不一致 | 一个字段只用一种保护方式 |
| 闭包捕获循环变量 | 数据竞争 | Go 1.22+ 已修复；旧版用参数传递 |
| WaitGroup Add 在 goroutine 外 | Add 未执行 | 在启动 goroutine 前调用 wg.Add(1) |
| sync.Map 的 Range | 非快照一致 | 如需一致性快照，用 RWMutex+普通 map |

---

## 7. 项目命名约定

- goroutine 函数名以 Loop/Watch/Serve 后缀：`playbackLoop`, `workLoop`, `watchCallbacks`
- channel 字段名以 Ch 后缀：`ctrlCh`, `ioCh`, `eventCh`, `closedCh`
- 取消函数名以 cancel 前缀：`cancelCtx`, `cancelSoftFix`
- 互斥锁字段名：`mu` (单一)，`xxxMu` (多个)
