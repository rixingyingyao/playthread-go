package api

import (
	"bufio"
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// RequestID 为每个请求生成唯一 ID，写入响应头和 context
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()[:8]
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// responseWriter 包装 http.ResponseWriter 以捕获状态码。
// 内嵌 ResponseWriter 保证 Hijacker/Flusher 等接口透传。
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Hijack 透传底层连接的 Hijack（WebSocket 升级需要）
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// Logger 使用 zerolog 记录每个请求
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rw.status).
			Dur("latency", time.Since(start)).
			Str("remote", r.RemoteAddr).
			Msg("HTTP")
	})
}

// CORSWithOrigins 添加跨域头，支持配置允许的源
func CORSWithOrigins(allowed []string) func(http.Handler) http.Handler {
	allowAll := len(allowed) == 0
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[strings.ToLower(o)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if _, ok := set[strings.ToLower(origin)]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			} else {
				// 非允许源：不设 CORS 头，浏览器会拒绝
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recoverer 从 panic 中恢复，返回 500
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Interface("panic", rec).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Msg("HTTP panic recovered")
				http.Error(w, `{"code":500,"message":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// TokenAuth 基于 Bearer Token 的认证中间件
func TokenAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				http.Error(w, `{"code":401,"message":"missing authorization"}`, http.StatusUnauthorized)
				return
			}
			got := auth[len(prefix):]
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, `{"code":401,"message":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ipLimiter 单个 IP 的滑动窗口计数器
type ipLimiter struct {
	count    int
	windowAt time.Time
}

const rateLimiterTTL = 60 * time.Second // IP 记录过期时间
const rateLimiterMaxEntries = 10000     // 最大 IP 记录数

// RateLimiter 基于 IP 的每秒请求限流中间件
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*ipLimiter
	rps     int
}

// NewRateLimiter 创建限流器
func NewRateLimiter(rps int) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*ipLimiter),
		rps:     rps,
	}
}

// Handler 返回限流中间件
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		rl.mu.Lock()
		now := time.Now()

		// 惰性清理：淘汰超过 TTL 的旧记录
		if len(rl.clients) > rateLimiterMaxEntries/2 {
			for k, v := range rl.clients {
				if now.Sub(v.windowAt) > rateLimiterTTL {
					delete(rl.clients, k)
				}
			}
		}

		lim, ok := rl.clients[ip]
		if !ok || now.Sub(lim.windowAt) >= time.Second {
			rl.clients[ip] = &ipLimiter{count: 1, windowAt: now}
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		lim.count++
		if lim.count > rl.rps {
			rl.mu.Unlock()
			http.Error(w, `{"code":429,"message":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		rl.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// Size 返回当前记录的 IP 数量（测试用）
func (rl *RateLimiter) Size() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.clients)
}
