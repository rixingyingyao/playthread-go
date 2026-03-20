# Go 7×24 长运行稳定性技能

适用于 playthread-go 项目的长期不间断运行场景。本系统是 **7×24×365 全年无休的广播播出客户端**，对稳定性、实时性和资源管理有最高要求。

**核心原则：每一行代码都要假设它将连续运行数月不重启。**

## 触发条件

在编写任何代码时都应考虑本技能的规则，尤其是：
- 创建/修改 goroutine
- 使用定时器、ticker、channel
- 分配资源（文件句柄、数据库连接、C 内存、BASS 句柄）
- 编写 Close/Stop/Shutdown 方法
- 处理错误和恢复逻辑
- 任何涉及进程生命周期的代码

---

## 1. 资源泄漏零容忍

### 1.1 Goroutine 泄漏

**每个 goroutine 必须有退出路径。** 没有例外。

```go
// ✅ 必须：每个 goroutine 都有 ctx.Done() 退出
func (pt *PlayThread) playbackLoop(ctx context.Context) {
    defer pt.wg.Done()
    ticker := time.NewTicker(20 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            pt.pollPosition()
        case <-ctx.Done():
            return  // ← 必须有这条路径
        }
    }
}

// ❌ 致命：goroutine 永远不退出
go func() {
    for range ch {}  // 如果 ch 永远不关闭→泄漏
}()

// ❌ 致命：依赖 time.Sleep 的循环没有退出条件
go func() {
    for {
        time.Sleep(time.Second)
        doWork()
    }
}()
```

**检查清单**（每次写 `go func()` 或 `go xxx()` 时回答）：
1. 这个 goroutine 如何退出？（ctx.Done / channel close / return）
2. 谁负责触发退出？（parent cancel / Close 方法）
3. 退出前是否清理了所有资源？（defer）
4. 是否纳入 WaitGroup 跟踪？

### 1.2 Timer / Ticker 泄漏

```go
// ✅ 正确：Ticker 必须 Stop
ticker := time.NewTicker(interval)
defer ticker.Stop()

// ✅ 正确：Timer 必须 Stop（也要排干通道）
timer := time.NewTimer(duration)
defer func() {
    if !timer.Stop() {
        select {
        case <-timer.C:
        default:
        }
    }
}()

// ⚠️ 注意：time.After 在 select 循环中
for {
    select {
    case <-time.After(5 * time.Second):  // ← 每次循环分配新 Timer！
        doWork()
    }
}
// ✅ 改用 Ticker 或手动 Reset：
ticker := time.NewTicker(5 * time.Second)
defer ticker.Stop()
for {
    select {
    case <-ticker.C:
        doWork()
    }
}
```

**关键规则：**
- `time.After()` 在 for-select 循环中 = 内存泄漏（每次迭代创建新 Timer，旧的要等超时才被 GC）
- 在循环中必须使用 `time.NewTicker` 或 `time.NewTimer` + `Reset()`
- `time.After()` 只适用于一次性等待（如 select 超时分支）

### 1.3 Channel 泄漏

```go
// ❌ 危险：发送者和接收者生命周期不匹配
ch := make(chan int)
go produce(ch)   // producer 可能先退出
go consume(ch)   // consumer 在等什么？

// ✅ 正确：用 ctx 控制双方，close 通知结束
ch := make(chan int, 64)
go func() {
    defer close(ch)  // ← producer 退出时关闭
    for {
        select {
        case ch <- val:
        case <-ctx.Done():
            return
        }
    }
}()
```

### 1.4 C 内存泄漏（CGO）

```go
// ✅ 所有 C.CString 必须配对 defer C.free
cStr := C.CString(path)
defer C.free(unsafe.Pointer(cStr))

// ✅ cgo.Handle 必须 Delete，用 sync.Once 防双重释放
type BassFileUser struct {
    handle cgo.Handle
    free   sync.Once
}
func (bfu *BassFileUser) Dispose() {
    bfu.free.Do(func() {
        bfu.handle.Delete()
    })
}

// ✅ BASS 流句柄必须 Free
handle := BassStreamCreateFile(path, 0, 0, flags)
// ... 使用 ...
BassStreamFree(handle)  // ← 不 Free = BASS 内部内存泄漏
```

### 1.5 文件句柄泄漏

```go
// ✅ 必须 defer Close
f, err := os.Open(path)
if err != nil {
    return err
}
defer f.Close()

// ❌ 忘记 Close 或提前 return 跳过 Close
f, _ := os.Open(path)
data, _ := io.ReadAll(f)
// return data ← f 永远不会被关闭
```

---

## 2. 优雅关闭（Graceful Shutdown）

### 2.1 关闭顺序原则

**从外到内，从上到下。先停止接收新任务，再等待现有任务完成，最后释放资源。**

```
信号捕获 (SIGINT/SIGTERM)
  ↓
cancel() — 通知所有 goroutine 开始退出
  ↓
ProcessManager.Stop()
  ├── bridge.Shutdown()   — 发送 IPC shutdown 命令
  ├── 等待子进程退出（3s 超时后 Kill）
  └── 等待子进程 goroutine 完成
  ↓
PlayThread.Wait()         — 等待所有工作 goroutine 退出
  ↓
database.Close()
  ├── close(writeCh)      — 停止接收新写入
  └── wg.Wait()           — 等待排队写入完成
  ↓
platform.RestoreTimer()   — 恢复系统定时器
  ↓
logCloser.Close()         — 最后关闭日志（确保所有日志都刷出）
```

### 2.2 组件 Close 模板

```go
func (c *Component) Close() error {
    c.mu.Lock()
    if c.closed {
        c.mu.Unlock()
        return nil  // 幂等
    }
    c.closed = true
    c.mu.Unlock()

    c.cancel()       // 1. 通知 goroutine 退出
    c.wg.Wait()      // 2. 等待所有 goroutine 退出
    
    // 3. 释放资源（顺序重要）
    close(c.eventCh)
    return c.db.Close()
}
```

**关键规则：**
- Close 必须幂等（多次调用安全）
- 使用 `closed` 标志 + mutex 防止竞态
- 先 cancel()，再 Wait()，最后释放资源
- 子进程关闭要有超时兜底（3s graceful → Kill）

### 2.3 子进程优雅关闭

```go
func (pm *ProcessManager) Stop() {
    pm.mu.Lock()
    pm.stopping = true
    cmd := pm.cmd
    waitDone := pm.waitDone
    pm.mu.Unlock()

    // 1. 发送 shutdown 指令
    _ = pm.bridge.Shutdown()
    pm.cancel()

    // 2. 等待优雅退出或超时
    select {
    case <-waitDone:
        // 正常退出
    case <-time.After(3 * time.Second):
        _ = cmd.Process.Kill()  // 超时强杀
        <-waitDone              // 等待 Kill 完成
    }
}
```

---

## 3. 崩溃恢复

### 3.1 子进程崩溃重启（指数退避）

```go
func (pm *ProcessManager) watchProcess(parentCtx context.Context) {
    err := <-pm.waitDone
    if pm.stopping { return }  // 主动停止不重启

    pm.crashCount++
    delay := pm.backoffDelay(pm.crashCount)
    
    log.Error().Err(err).Int("crash_count", pm.crashCount).
        Dur("restart_delay", delay).Msg("子进程崩溃，准备重启")
    
    select {
    case <-time.After(delay):
        pm.startLocked(parentCtx)  // 重启
    case <-parentCtx.Done():
        return
    }
}

func (pm *ProcessManager) backoffDelay(count int) time.Duration {
    if count <= 5 {
        return 0  // 前 5 次立即重启
    }
    d := time.Duration(count-5) * 2 * time.Second
    if d > 30*time.Second {
        return 30 * time.Second  // 上限 30s
    }
    return d
}
```

### 3.2 Panic 恢复

**所有顶层 goroutine 和 CGO 回调必须 recover。**

```go
// CGO 回调中的 panic 恢复（关键！CGO 回调中 panic 会导致整个进程崩溃）
//export goSyncEndCallback
func goSyncEndCallback(...) {
    defer func() {
        if r := recover(); r != nil {
            fmt.Fprintf(os.Stderr, "BASS 回调 panic: %v\n%s", r, debug.Stack())
        }
    }()
    // ... 回调逻辑
}

// 顶层 goroutine 的 panic 恢复
go func() {
    defer func() {
        if r := recover(); r != nil {
            log.Error().Interface("panic", r).Str("stack", string(debug.Stack())).
                Msg("goroutine panic recovered")
        }
    }()
    defer pt.wg.Done()
    pt.workLoop(ctx)
}()
```

**注意：** CGO 回调中不能使用 zerolog（可能触发另一个 panic），使用 `fmt.Fprintf(os.Stderr, ...)` 。

### 3.3 冷启动恢复（快照）

```go
// 程序启动时检查是否有快照
info, err := snapshotMgr.Load()
if err == nil && info != nil {
    // 计算恢复位置（补偿停机时间）
    recoveryPos := snapshotMgr.CalcRecoveryPosition(info)
    // 从上次位置继续播出
}
```

**原子写入保护快照完整性：**
```go
func (sm *SnapshotManager) Save(info *PlayingInfo) error {
    data, _ := json.Marshal(info)
    tmpPath := sm.filePath + ".tmp"
    
    os.WriteFile(tmpPath, data, 0o644)  // 写临时文件
    os.Remove(sm.filePath)              // Windows: rename 前先删
    return os.Rename(tmpPath, sm.filePath)  // 原子替换
}
```

---

## 4. 内存管理

### 4.1 避免内存持续增长

```go
// ❌ 危险：无限增长的 slice
var history []Event
func record(e Event) {
    history = append(history, e)  // 永远不释放
}

// ✅ 正确：环形缓冲区或定期清理
type RingBuffer struct {
    buf  []Event
    pos  int
    size int
}
func (rb *RingBuffer) Add(e Event) {
    rb.buf[rb.pos%rb.size] = e
    rb.pos++
}

// ✅ 或者使用定期清理
func (m *Manager) cleanup() {
    m.mu.Lock()
    defer m.mu.Unlock()
    cutoff := time.Now().Add(-24 * time.Hour)
    // 删除 cutoff 之前的记录
}
```

### 4.2 sync.Map 内存管理

```go
// sync.Map 条目不会自动过期！必须手动 Delete
ab.pending.Store(id, respCh)
defer ab.pending.Delete(id)  // ← 请求完成后立即删除

// 定期清理过期条目
func (ab *AudioBridge) cleanStale() {
    ab.pending.Range(func(key, val interface{}) bool {
        // 检查是否过期，过期则 Delete
        return true
    })
}
```

### 4.3 大对象及时释放

```go
// ✅ 处理完大数据后置 nil 帮助 GC
func processPlaylist(data []byte) {
    pl := parsePlaylist(data)
    data = nil  // ← 释放原始数据的引用
    
    // 使用 pl 继续处理...
}

// ✅ 切片截断后释放底层数组
items = items[:0]      // 保留底层数组（适合复用场景）
items = nil            // 完全释放底层数组（适合一次性场景）
```

### 4.4 日志轮转防磁盘满

```go
fileWriter := &lumberjack.Logger{
    Filename:   "playthread.log",
    MaxSize:    50,   // 每文件最大 50MB
    MaxBackups: 30,   // 最多 30 个备份
    MaxAge:     30,   // 保留 30 天
    Compress:   true, // gzip 压缩旧文件
}
```

**50MB × 30 ≈ 1.5GB 日志上限。** 对于 7×24 长运行，这防止磁盘被日志填满。

### 4.5 SQLite WAL 模式

```go
// WAL (Write-Ahead Log) 模式对长运行至关重要：
// - 读写不阻塞
// - 写入失败不损坏数据库
// - WAL 文件自动 checkpoint
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;  // 平衡性能和安全
PRAGMA busy_timeout=5000;   // 5s 等待锁
```

---

## 5. 实时性保证

### 5.1 Windows 高精度定时器

```go
// 必须在程序启动时调用！否则 Go 的 time.Sleep/Ticker 精度为 ~15ms
platform.SetHighResTimer()   // timeBeginPeriod(1) → 1ms 精度
defer platform.RestoreTimer() // timeEndPeriod(1) → 恢复默认

// 不调用 SetHighResTimer 的后果：
// - time.NewTicker(20ms) 实际触发间隔可能在 15~30ms 波动
// - 播放位置轮询不精确 → 定时任务触发抖动
```

### 5.2 锁的持有时间最小化

```go
// ✅ 正确：拿数据 → 释放锁 → 处理数据
pt.mu.RLock()
prog := pt.currentProg
pos := pt.currentPos
pt.mu.RUnlock()
// 在锁外处理
processProgram(prog, pos)

// ❌ 致命：在锁内做耗时操作
pt.mu.Lock()
result, err := pt.audioBridge.Call("position", nil)  // ← IPC 调用可能阻塞 5s！
pt.mu.Unlock()
```

**规则：锁内绝不做以下操作：**
- Channel 发送/接收
- 网络/IPC 调用
- 文件 I/O
- time.Sleep
- 其他 mutex 的 Lock（除非有严格的锁序）

### 5.3 Buffered Channel 防阻塞

```go
// 事件通道必须有足够容量
PlayFinished:  make(chan PlayFinishedEvent, 16)   // 不会积压
ChannelEmpty:  make(chan ChannelEmptyEvent, 8)
Broadcast:     make(chan BroadcastEvent, 64)      // 外发事件量大

// 广播时用 select + default 防阻塞
select {
case ch <- evt:
default:
    log.Warn().Msg("事件通道已满，丢弃")  // 告警但不阻塞
}
```

### 5.4 单调时钟（抗 NTP 跳变）

```go
// ❌ 危险：墙上时钟可能被 NTP 回拨
deadline := time.Now().Add(30 * time.Second)
// 如果此时 NTP 回调 10 秒 → 实际等待 40 秒

// ✅ 正确：使用单调时钟
// Go 的 time.Now() 包含 monotonic reading
// time.Since() 和 time.Until() 自动使用单调部分
start := time.Now()
elapsed := time.Since(start)  // ← 使用 mono clock，不受 NTP 影响

// ✅ 通道保持使用单调时钟
type ChannelHold struct {
    startMono    int64  // 单调时钟起点
    durationMono int64  // 持续时间
}
```

---

## 6. 进程生命周期管理

### 6.1 信号处理

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 信号捕获
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        sig := <-sigCh
        log.Info().Str("signal", sig.String()).Msg("收到退出信号")
        cancel()  // 触发级联关闭
    }()
    
    // ... 启动组件 ...
    
    <-ctx.Done()  // 等待退出信号
    // ... 优雅关闭 ...
}
```

### 6.2 组件启动顺序

```
1. 解析配置 (flag + YAML)
2. 初始化日志 → defer Close
3. 设置高精度定时器 → defer Restore
4. ctx, cancel 创建 → defer cancel
5. 打开数据库 → defer Close
6. 执行数据库迁移
7. 加载快照（冷启动恢复）
8. 启动子进程管理器 → defer Stop
9. 启动 PlayThread
10. 等待信号
```

**原则：** 越早启动的组件越晚关闭（栈式 defer）。

### 6.3 健康检查

```go
// 心跳监控模式
type GoroutineMonitor struct {
    mu         sync.Mutex
    heartbeats map[string]time.Time
}

func (gm *GoroutineMonitor) Heartbeat(name string) {
    gm.mu.Lock()
    gm.heartbeats[name] = time.Now()
    gm.mu.Unlock()
}

func (gm *GoroutineMonitor) CheckAll() []string {
    gm.mu.Lock()
    defer gm.mu.Unlock()
    var stale []string
    for name, last := range gm.heartbeats {
        if time.Since(last) > 30*time.Second {
            stale = append(stale, name)
        }
    }
    return stale
}

// IPC 心跳（检测子进程是否存活）
func (pm *ProcessManager) heartbeatLoop(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            if err := pm.bridge.Ping(); err != nil {
                log.Error().Err(err).Msg("子进程心跳失败")
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### 6.4 内存监控

```go
func memoryWatchdog(ctx context.Context, warnMB uint64) {
    ticker := time.NewTicker(time.Hour)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            allocMB := m.Alloc / 1024 / 1024
            if allocMB > warnMB {
                log.Warn().Uint64("alloc_mb", allocMB).
                    Uint64("threshold_mb", warnMB).
                    Int("goroutines", runtime.NumGoroutine()).
                    Msg("内存使用超过告警阈值")
            }
        case <-ctx.Done():
            return
        }
    }
}
```

---

## 7. 稳定性审查清单

每次代码修改后，对照此清单检查：

### 资源生命周期
- [ ] 每个 `go func()` 都有 ctx.Done() 退出路径？
- [ ] 每个 goroutine 都纳入 WaitGroup？
- [ ] 每个 Ticker/Timer 都 defer Stop()？
- [ ] 每个 C.CString 都 defer C.free？
- [ ] 每个 cgo.Handle 都有 Delete 路径？
- [ ] 每个 os.Open/Create 都 defer Close？
- [ ] 每个 BASS stream 都有 Free 路径？

### 并发安全
- [ ] 锁内没有 I/O / channel / 网络操作？
- [ ] 没有在 for-select 循环中使用 time.After？
- [ ] Buffered channel 容量足够？
- [ ] 广播使用 select + default 防阻塞？

### 关闭路径
- [ ] Close/Stop 方法幂等？
- [ ] 关闭顺序正确（cancel → Wait → Release）？
- [ ] 子进程有超时兜底（避免永远等待）？

### 内存管理
- [ ] 没有无限增长的 slice / map？
- [ ] sync.Map 条目有删除路径？
- [ ] 大对象处理完及时释放引用？

### 错误处理
- [ ] 所有 CGO 回调有 recover？
- [ ] 错误日志包含足够上下文？
- [ ] 重试有上限和退避？

---

## 8. 常见长运行陷阱速查

| 陷阱 | 运行时间 | 结果 | 修复 |
|------|---------|------|------|
| goroutine 泄漏 | 数小时 | OOM | ctx.Done() 退出 |
| time.After 在循环中 | 数小时 | 内存增长 | 改用 Ticker |
| sync.Map 不清理 | 数天 | 内存增长 | 及时 Delete |
| 日志不轮转 | 数周 | 磁盘满 | lumberjack |
| C.CString 不 free | 数小时 | C 堆 OOM | defer C.free |
| Ticker 不 Stop | 数小时 | GC 压力 | defer Stop() |
| 文件句柄不 Close | 数天 | Too many open files | defer Close() |
| DB 不 Close | 关机时 | WAL 未 checkpoint | defer Close() |
| 墙上时钟判时 | NTP 调整时 | 任务抖动 | 单调时钟 |
| 不设高精度定时器 | 始终 | 15ms 抖动 | timeBeginPeriod(1) |
| panic 未 recover | 任意时刻 | 进程崩溃 | 顶层 goroutine + CGO 回调 recover |
| 子进程崩溃无重启 | 故障时 | 无声播出 | watchProcess + 指数退避 |
