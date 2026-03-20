package audio

/*
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
typedef HMODULE lib_handle_t;
static lib_handle_t load_lib(const char* name) { return LoadLibraryA(name); }
static void* get_func(lib_handle_t lib, const char* name) { return (void*)GetProcAddress(lib, name); }
static void free_lib(lib_handle_t lib) { FreeLibrary(lib); }
#else
#include <dlfcn.h>
typedef void* lib_handle_t;
static lib_handle_t load_lib(const char* name) { return dlopen(name, RTLD_LAZY); }
static void* get_func(lib_handle_t lib, const char* name) { return dlsym(lib, name); }
static void free_lib(lib_handle_t lib) { dlclose(lib); }
#endif

// LAME function pointer types
typedef void* lame_t;
typedef lame_t (*fn_lame_init_t)(void);
typedef int (*fn_lame_set_int_t)(lame_t, int);
typedef int (*fn_lame_init_params_t)(lame_t);
typedef int (*fn_lame_encode_interleaved_t)(lame_t, short*, int, unsigned char*, int);
typedef int (*fn_lame_encode_flush_t)(lame_t, unsigned char*, int);
typedef void (*fn_lame_close_t)(lame_t);

// LAME function pointers (global, loaded once)
static lib_handle_t g_lame_lib = NULL;
static fn_lame_init_t              g_lame_init = NULL;
static fn_lame_set_int_t           g_lame_set_in_samplerate = NULL;
static fn_lame_set_int_t           g_lame_set_num_channels = NULL;
static fn_lame_set_int_t           g_lame_set_brate = NULL;
static fn_lame_init_params_t       g_lame_init_params = NULL;
static fn_lame_encode_interleaved_t g_lame_encode = NULL;
static fn_lame_encode_flush_t      g_lame_flush = NULL;
static fn_lame_close_t             g_lame_close = NULL;

// load_lame 动态加载 LAME 库，返回 1=成功 0=失败
static int load_lame() {
	if (g_lame_lib != NULL) return 1;

#ifdef _WIN32
	g_lame_lib = load_lib("libmp3lame.dll");
	if (!g_lame_lib) g_lame_lib = load_lib("lame_enc.dll");
#else
	g_lame_lib = load_lib("libmp3lame.so");
	if (!g_lame_lib) g_lame_lib = load_lib("libmp3lame.so.0");
#endif
	if (!g_lame_lib) return 0;

	g_lame_init              = (fn_lame_init_t)get_func(g_lame_lib, "lame_init");
	g_lame_set_in_samplerate = (fn_lame_set_int_t)get_func(g_lame_lib, "lame_set_in_samplerate");
	g_lame_set_num_channels  = (fn_lame_set_int_t)get_func(g_lame_lib, "lame_set_num_channels");
	g_lame_set_brate         = (fn_lame_set_int_t)get_func(g_lame_lib, "lame_set_brate");
	g_lame_init_params       = (fn_lame_init_params_t)get_func(g_lame_lib, "lame_init_params");
	g_lame_encode            = (fn_lame_encode_interleaved_t)get_func(g_lame_lib, "lame_encode_buffer_interleaved");
	g_lame_flush             = (fn_lame_encode_flush_t)get_func(g_lame_lib, "lame_encode_flush");
	g_lame_close             = (fn_lame_close_t)get_func(g_lame_lib, "lame_close");

	if (!g_lame_init || !g_lame_set_in_samplerate || !g_lame_set_num_channels ||
		!g_lame_set_brate || !g_lame_init_params || !g_lame_encode ||
		!g_lame_flush || !g_lame_close) {
		free_lib(g_lame_lib);
		g_lame_lib = NULL;
		return 0;
	}
	return 1;
}

// C wrapper functions for Go to call

static lame_t c_lame_init(int samplerate, int channels, int bitrate) {
	if (!g_lame_init) return NULL;
	lame_t lame = g_lame_init();
	if (!lame) return NULL;
	g_lame_set_in_samplerate(lame, samplerate);
	g_lame_set_num_channels(lame, channels);
	g_lame_set_brate(lame, bitrate);
	g_lame_init_params(lame);
	return lame;
}

static int c_lame_encode(lame_t lame, short* pcm, int samples, unsigned char* mp3buf, int mp3buf_size) {
	if (!g_lame_encode) return -1;
	return g_lame_encode(lame, pcm, samples, mp3buf, mp3buf_size);
}

static int c_lame_flush(lame_t lame, unsigned char* mp3buf, int size) {
	if (!g_lame_flush) return -1;
	return g_lame_flush(lame, mp3buf, size);
}

static void c_lame_close(lame_t lame) {
	if (g_lame_close && lame) g_lame_close(lame);
}
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

var lameLoadOnce sync.Once
var lameAvailable bool

// LameLoad 尝试加载 LAME 库（只执行一次）
func LameLoad() bool {
	lameLoadOnce.Do(func() {
		lameAvailable = C.load_lame() == 1
	})
	return lameAvailable
}

// LameAvailable 检查 LAME 库是否可用
func LameAvailable() bool {
	return lameAvailable
}

// LameEncoder LAME MP3 编码器封装
type LameEncoder struct {
	handle C.lame_t // lame_t
	mp3Buf []byte   // MP3 输出缓冲区
}

// NewLameEncoder 创建 LAME 编码器（samplerate=48000, channels=2, bitrate=256kbps）
func NewLameEncoder(samplerate, channels, bitrate int) (*LameEncoder, error) {
	if !LameLoad() {
		return nil, fmt.Errorf("LAME 库不可用（未找到 libmp3lame.dll）")
	}
	handle := C.c_lame_init(C.int(samplerate), C.int(channels), C.int(bitrate))
	if handle == C.lame_t(nil) {
		return nil, fmt.Errorf("lame_init 失败")
	}
	return &LameEncoder{
		handle: handle,
		mp3Buf: make([]byte, 1024*1024), // 1MB 缓冲区
	}, nil
}

// Encode 将 PCM 交织数据编码为 MP3。
// pcm: 16-bit 交织立体声 PCM 数据, length: 字节数
// 返回编码后的 MP3 数据切片（引用内部缓冲区，需在下次 Encode 前消费）
func (e *LameEncoder) Encode(pcm unsafe.Pointer, lengthBytes int) ([]byte, error) {
	bytesPerSample := 2 * 2 // 16bit * 2ch
	samples := lengthBytes / bytesPerSample

	encoded := C.c_lame_encode(
		e.handle,
		(*C.short)(pcm),
		C.int(samples),
		(*C.uchar)(unsafe.Pointer(&e.mp3Buf[0])),
		C.int(len(e.mp3Buf)),
	)
	if encoded < 0 {
		return nil, fmt.Errorf("lame_encode 失败: %d", int(encoded))
	}
	return e.mp3Buf[:int(encoded)], nil
}

// Flush 刷出编码器内残余数据
func (e *LameEncoder) Flush() ([]byte, error) {
	flushed := C.c_lame_flush(
		e.handle,
		(*C.uchar)(unsafe.Pointer(&e.mp3Buf[0])),
		C.int(len(e.mp3Buf)),
	)
	if flushed < 0 {
		return nil, fmt.Errorf("lame_flush 失败: %d", int(flushed))
	}
	return e.mp3Buf[:int(flushed)], nil
}

// Close 释放编码器资源
func (e *LameEncoder) Close() {
	if e.handle != C.lame_t(nil) {
		C.c_lame_close(e.handle)
		e.handle = C.lame_t(nil)
	}
}
