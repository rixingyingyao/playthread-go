# CGO + BASS 音频引擎技能

适用于 playthread-go 项目中 audio 服务（CGO_ENABLED=1）的开发，涵盖 CGO 基础、BASS 音频库绑定、回调机制、双进程 IPC 架构和构建要求。

## 触发条件

当任务涉及以下内容时使用本技能：
- 修改 `audio/` 目录下的任何文件
- CGO 相关开发（C 库绑定、头文件、LDFLAGS）
- BASS 音频 API 调用
- 音频服务进程（`cmd/audio-service/`）
- bridge/IPC 通信协议
- 构建配置（CGO_ENABLED, gcc, mingw-w64）

---

## 1. CGO 基础

### 1.1 导入和 preamble

```go
/*
#cgo windows LDFLAGS: -L${SRCDIR}/libs/windows -lbass
#cgo linux LDFLAGS: -L${SRCDIR}/libs/linux -lbass -Wl,-rpath,$ORIGIN
#include "libs/bass.h"
#include <stdlib.h>
*/
import "C"
```

**关键规则：**
- `import "C"` 必须紧跟 preamble 注释，中间不能有空行
- `${SRCDIR}` 是 `.go` 文件所在目录的绝对路径
- `#cgo` 指令可携带 build constraint：`#cgo windows` / `#cgo linux`

### 1.2 类型映射

| Go 类型 | C 类型 | 说明 |
|---------|--------|------|
| `C.int` | `int` | 默认 32-bit |
| `C.uint` | `unsigned int` | |
| `C.DWORD` | `DWORD` (uint32) | BASS 常用 |
| `C.BOOL` | `BOOL` (int) | 0=FALSE, 非0=TRUE |
| `C.float` | `float` | BASS 属性值 |
| `C.double` | `double` | |
| `unsafe.Pointer` | `void*` | 通用指针 |
| `C.HSTREAM` | `DWORD` | BASS 流句柄 |
| `C.HSYNC` | `DWORD` | BASS 同步回调句柄 |

### 1.3 字符串转换

```go
// Go → C（需手动释放！）
cStr := C.CString(path)
defer C.free(unsafe.Pointer(cStr))

// C → Go（安全，Go 管理内存）
goStr := C.GoString(cPtr)
goBytes := C.GoBytes(unsafe.Pointer(cPtr), C.int(length))
```

**⚠️ C.CString 分配 C 堆内存，忘记 free 就是内存泄漏。**

### 1.4 指针传递规则

Go 1.21+ 的指针传递规则：
1. Go 可以向 C 传递指向 Go 内存的指针，但该内存不得包含指向 Go 的指针
2. C 不得持有 Go 指针超过调用返回
3. 使用 `runtime.Pinner` 固定需长期传给 C 的 Go 对象

```go
// ✅ 安全：传基础类型指针
var result C.int
C.some_func(&result)

// ❌ 危险：传包含 Go 指针的结构体
// C 不能持有指向 Go GC 管理对象的指针
```

---

## 2. 回调机制

### 2.1 //export 声明

```go
//export goSyncEndCallback
func goSyncEndCallback(handle C.HSYNC, channel C.DWORD, data C.DWORD, user unsafe.Pointer) {
    h := cgo.Handle(user)
    callback := h.Value().(func(uint32))
    callback(uint32(channel))
}
```

**规则：**
- `//export` 和函数名之间没有空格
- 包含 `//export` 的文件的 preamble 中不能有 C 函数定义（只能有声明）
- 使用 `extern` 在 preamble 中声明回调，实际定义在 Go 中

### 2.2 runtime/cgo.Handle

用于安全地在 Go 和 C 之间传递 Go 值：

```go
import "runtime/cgo"

// 创建 handle（Go 值 → 不透明句柄）
h := cgo.NewHandle(myGoCallback)
defer h.Delete() // 必须手动释放！

// 传给 C（作为 void* / user data）
C.BASS_ChannelSetSync(channel, syncType, param, 
    C.SYNCPROC(C.goSyncEndCallback), unsafe.Pointer(h))

// 在回调中恢复
func goSyncEndCallback(..., user unsafe.Pointer) {
    h := cgo.Handle(user)
    val := h.Value() // 恢复原始 Go 值
}
```

**注意：** `cgo.Handle` 持有 Go 对象的引用，阻止 GC。必须调用 `h.Delete()` 释放。

### 2.3 文件流回调

BASS 支持通过回调函数自定义文件读取（用于加密音频文件）：

```go
// 回调函数签名
//export goFileCloseProc
func goFileCloseProc(user unsafe.Pointer) { ... }

//export goFileLenProc  
func goFileLenProc(user unsafe.Pointer) C.QWORD { ... }

//export goFileReadProc
func goFileReadProc(buffer unsafe.Pointer, length C.DWORD, user unsafe.Pointer) C.DWORD { ... }

//export goFileSeekProc
func goFileSeekProc(offset C.QWORD, user unsafe.Pointer) C.BOOL { ... }
```

---

## 3. BASS 音频 API

### 3.1 初始化与释放

```go
func BassInit(device int, freq int) bool {
    return C.BASS_Init(C.int(device), C.DWORD(freq), 0, 0, nil) != 0
}

func BassFree() {
    C.BASS_Free()
}
```

- `BASS_Init` 必须在使用其他 BASS 函数前调用
- `device = -1` 表示默认输出设备
- Windows Vista+ 默认使用 WASAPI，旧系统用 DirectSound

### 3.2 流创建与播放

```go
func BassStreamCreateFile(path string, offset, length uint64, flags uint32) uint32 {
    cPath := C.CString(path)
    defer C.free(unsafe.Pointer(cPath))
    return uint32(C.BASS_StreamCreateFile(
        C.BOOL(0),               // mem = FALSE，从文件读取
        unsafe.Pointer(cPath),
        C.QWORD(offset),
        C.QWORD(length),
        C.DWORD(flags),
    ))
}

func BassChannelPlay(handle uint32, restart bool) bool {
    r := C.BOOL(0)
    if restart { r = 1 }
    return C.BASS_ChannelPlay(C.DWORD(handle), r) != 0
}

func BassChannelStop(handle uint32) bool {
    return C.BASS_ChannelStop(C.DWORD(handle)) != 0
}
```

### 3.3 属性与淡变

```go
// 直接设置属性
func BassChannelSetAttribute(handle uint32, attr uint32, value float32) bool {
    return C.BASS_ChannelSetAttribute(C.DWORD(handle), C.DWORD(attr), C.float(value)) != 0
}

// 渐变滑动属性（如音量淡入淡出）
func BassChannelSlideAttribute(handle uint32, attr uint32, value float32, timeMs uint32) bool {
    return C.BASS_ChannelSlideAttribute(
        C.DWORD(handle), 
        C.DWORD(attr), 
        C.float(value), 
        C.DWORD(timeMs),
    ) != 0
}
```

**淡变标志位：**
- `BASS_SLIDE_LOG`：对数曲线（更自然的音量变化）
- 不设此标志则为线性滑动
- 音量范围：0.0（静音） → 1.0（最大）

### 3.4 同步回调

```go
// 播放结束回调
BassChannelSetSync(handle, BASS_SYNC_END, 0, endCallback, userData)

// 位置回调
BassChannelSetSync(handle, BASS_SYNC_POS, position, posCallback, userData)

// BASS_SYNC_MIXTIME：回调在混音线程中触发（更精确但限制更多）
```

### 3.5 设备管理

```go
func BassSetDevice(device int) bool {
    return C.BASS_SetDevice(C.DWORD(device)) != 0
}

func BassGetDevice() int {
    return int(C.BASS_GetDevice())
}

func BassEnumDevices() []DeviceInfo {
    var info C.BASS_DEVICEINFO
    var devices []DeviceInfo
    for i := 0; C.BASS_GetDeviceInfo(C.DWORD(i), &info) != 0; i++ {
        devices = append(devices, DeviceInfo{
            Name:    C.GoString(info.name),
            Driver:  C.GoString(info.driver),
            Flags:   uint32(info.flags),
        })
    }
    return devices
}
```

---

## 4. 双进程 IPC 架构

### 4.1 架构概述

```
┌──────────────────────┐          ┌──────────────────────┐
│   主控进程 (playthread) │  stdin/  │   音频服务 (audio-service) │
│   CGO_ENABLED=0      │ ──JSON── │   CGO_ENABLED=1        │
│   纯 Go 逻辑         │  stdout  │   BASS + CGO           │
└──────────────────────┘          └──────────────────────┘
```

**分离原因：**
1. BASS 库要求 LockOSThread → 影响 Go 调度器
2. CGO 有编译要求（mingw-w64）→ 主控进程不需要
3. 音频服务崩溃不影响主控进程
4. 测试主控逻辑时不需要真实音频设备

### 4.2 JSON-Line IPC 协议

请求格式（主控 → 音频服务，一行一条 JSON）：
```json
{"id":"uuid","method":"Play","params":{"path":"/audio/test.mp3","fadeInMs":500}}
```

响应格式（音频服务 → 主控）：
```json
{"id":"uuid","result":{"handle":12345},"error":""}
```

事件格式（音频服务 → 主控，无 id）：
```json
{"event":"PlayFinished","data":{"handle":12345}}
```

### 4.3 AudioBridge 客户端

```go
type AudioBridge struct {
    mu       sync.Mutex      // 保护 stdin 写入序列化
    stdin    io.Writer        // 写请求
    pending  sync.Map         // map[string]chan *IPCResponse
    eventCh  chan *IPCEvent   // 事件通道 (buffered)
    timeout  time.Duration
    closedCh chan struct{}     // 连接关闭信号
}
```

**模式要点：**
- `mu` 只保护 `stdin` 写入（JSON 编码 + 换行 + Write 是原子操作）
- `pending` 用 `sync.Map`（UUID 作 key，高并发读写不冲突）
- `readLoop` goroutine 持续读取 stdout，按 id 路由到 pending 或 eventCh

### 4.4 测试 Mock（io.Pipe）

```go
func newMockAudioBridge(posMs, durMs int) (*bridge.AudioBridge, *mockIPC) {
    reqR, reqW := io.Pipe()  // 模拟 stdin
    respR, respW := io.Pipe() // 模拟 stdout
    mock := &mockIPC{reader: reqR, writer: respW, posMs: posMs, durMs: durMs}
    go mock.serve()
    ab := bridge.NewAudioBridge(reqW, respR, 5*time.Second)
    return ab, mock
}
```

**关键：** 不修改生产代码（不引入接口），通过 io.Pipe() 替换底层 reader/writer 实现 mock。

---

## 5. 构建与测试

### 5.1 构建命令

```bash
# 主控进程（纯 Go，无 CGO）
CGO_ENABLED=0 go build -o playthread ./cmd/playthread/

# 音频服务（需要 CGO + mingw-w64）
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -o audio-service ./cmd/audio-service/

# Windows 本地构建（已有 gcc）
go build -o audio-service.exe ./cmd/audio-service/
```

### 5.2 测试命令

```bash
# 核心逻辑测试（不需要 CGO）
CGO_ENABLED=0 go test ./core/...

# 运行竞态检测（需要 CGO / gcc）
go test -race ./core/...

# 非 audio 包的所有测试
CGO_ENABLED=0 go test ./core/... ./bridge/... ./infra/... ./models/...
```

### 5.3 Windows 环境依赖

- Go 1.22+
- mingw-w64（x86_64-w64-mingw32-gcc v8+）
- BASS DLL：`audio/libs/windows/bass.dll`
- **`_test.go` 文件不能使用 `import "C"`** — 这是 Go 工具链的限制

### 5.4 BASS DLL 部署

运行时 `bass.dll` / `libbass.so` 必须在可执行文件同目录或系统 PATH 中。

---

## 6. 常见陷阱

| 陷阱 | 表现 | 解决方案 |
|------|------|----------|
| C.CString 未 free | 内存泄漏 | 每次 CString 后立即 defer C.free |
| 非 BASS 线程调 BASS API | 未定义行为/崩溃 | 所有调用通过 ctrlCh/ioCh 派发到 LockOSThread 线程 |
| cgo.Handle 未 Delete | Go 对象无法 GC | 确保在 sync 回调移除时 h.Delete() |
| import "C" 前有空行 | 编译失败 | preamble 注释和 import "C" 必须紧邻 |
| preamble 中定义函数 + //export | 链接错误 | //export 文件的 preamble 只能有声明(extern) |
| CGO_ENABLED=0 测试 audio 包 | 编译失败 | audio 包不做 CGO_ENABLED=0 测试 |
| 在回调中分配 Go 内存 | 性能问题 / panic | 回调中尽量轻量，重活扔到 channel |

---

## 7. 性能优化指令

- `#cgo noescape funcName`：告诉编译器该函数的参数不会逃逸到 C 堆（减少 Go 分配）
- `#cgo nocallback funcName`：告诉编译器该函数不会回调 Go（避免额外的调度开销）
- 仅用于确定安全的 C 函数（如纯计算函数）

---

## 8. 文件组织

```
audio/
├── bass_bindings.go      # 底层 CGO 绑定（C 类型 ↔ Go 类型）
├── bass_engine.go        # BassEngine：LockOSThread + 双通道调度
├── adapter.go            # 高级适配器（业务语义 → engine 命令）
├── channel_matrix.go     # 通道矩阵（多路输出管理）
├── ipc_server.go         # IPC 服务端（JSON-line 协议处理）
├── level_meter.go        # 音量计（VU meter）
├── recorder.go           # 录音功能
├── virtual_channel.go    # 虚拟通道（逻辑音频通道）
└── libs/
    ├── bass.h            # BASS C 头文件
    ├── windows/bass.dll  # Windows BASS 库
    └── linux/libbass.so  # Linux BASS 库
```
