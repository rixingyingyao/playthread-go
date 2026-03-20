// integration_test 集成测试框架。
// 测试完整的 API → core → bridge 链路（使用 mock AudioBridge）。
// 需要 -tags=integration 或 -run Integration 执行。
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rixingyingyao/playthread-go/api"
	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
)

var _ = fmt.Sprintf // avoid unused import

// TestEnv 集成测试环境
type TestEnv struct {
	Config  *infra.Config
	SM      *core.StateMachine
	EB      *core.EventBus
	PT      *core.PlayThread
	Server  *api.Server
	Handler http.Handler
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewTestEnv 创建集成测试环境
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snap := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snap) // nil bridge for mock

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)

	srv := api.NewServer(cfg, pt)

	return &TestEnv{
		Config:  cfg,
		SM:      sm,
		EB:      eb,
		PT:      pt,
		Server:  srv,
		Handler: srv.Router(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Close 清理测试环境
func (e *TestEnv) Close() {
	e.cancel()
	e.PT.Wait()
}

// DoRequest 发送 HTTP 请求并返回响应
func (e *TestEnv) DoRequest(method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.Handler.ServeHTTP(w, req)
	return w
}

// ─── 集成测试 ──────────────────────────────────────────

func TestIntegration_GetStatus(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	w := env.DoRequest("GET", "/api/v1/status", nil)
	if w.Code != http.StatusOK {
		t.Errorf("GET /status expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
}

func TestIntegration_GetProgress(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	w := env.DoRequest("GET", "/api/v1/progress", nil)
	if w.Code != http.StatusOK {
		t.Errorf("GET /progress expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_GetPlaylist(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	w := env.DoRequest("GET", "/api/v1/playlist", nil)
	if w.Code != http.StatusOK {
		t.Errorf("GET /playlist expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_LoadPlaylist_Then_Status(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	// 加载播表
	playlist := models.Playlist{
		ID:   "test-pl-001",
		Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),
		Blocks: []models.TimeBlock{
			{
				ID:        "block-1",
				StartTime: "08:00:00",
				EndTime:   "09:00:00",
				Programs: []models.Program{
					{ID: "item-1", Name: "测试节目1", FilePath: "test1.mp3", Duration: 60000},
					{ID: "item-2", Name: "测试节目2", FilePath: "test2.mp3", Duration: 120000},
				},
			},
		},
	}
	w := env.DoRequest("POST", "/api/v1/playlist/load", playlist)
	if w.Code != http.StatusOK {
		t.Errorf("POST /playlist/load expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 再查询状态
	w2 := env.DoRequest("GET", "/api/v1/status", nil)
	if w2.Code != http.StatusOK {
		t.Errorf("GET /status expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestIntegration_ControlFlow_Play_Pause_Stop(t *testing.T) {
	// 注意：不加载播表就 play 会因为没有素材而失败（400），
	// 但不应触发 nil bridge panic，因为 Recoverer 中间件会捕获。
	env := NewTestEnv(t)
	defer env.Close()

	// Play without playlist → should return error, not panic
	w := env.DoRequest("POST", "/api/v1/control/play", nil)
	// 200 or 400 都可以，不应 500
	if w.Code == http.StatusInternalServerError {
		t.Errorf("play should not return 500: %s", w.Body.String())
	}

	// Pause without active playback
	w = env.DoRequest("POST", "/api/v1/control/pause", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("pause should not return 500: %s", w.Body.String())
	}

	// Stop
	w = env.DoRequest("POST", "/api/v1/control/stop", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("stop should not return 500: %s", w.Body.String())
	}
}

func TestIntegration_Intercut(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	// 尝试插播（可能因状态不对而返回 400，但不应 panic 或 500）
	body := map[string]interface{}{
		"file_path": "emergency.mp3",
		"duration":  30000,
	}
	w := env.DoRequest("POST", "/api/v1/intercut/start", body)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("intercut/start should not return 500: %s", w.Body.String())
	}

	w = env.DoRequest("POST", "/api/v1/intercut/stop", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("intercut/stop should not return 500: %s", w.Body.String())
	}
}

func TestIntegration_BlankControl(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	w := env.DoRequest("POST", "/api/v1/control/blank/start", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("blank/start should not return 500: %s", w.Body.String())
	}

	w = env.DoRequest("POST", "/api/v1/control/blank/stop", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("blank/stop should not return 500: %s", w.Body.String())
	}
}

func TestIntegration_DelayControl(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	w := env.DoRequest("POST", "/api/v1/control/delay/start", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("delay/start should not return 500: %s", w.Body.String())
	}

	w = env.DoRequest("POST", "/api/v1/control/delay/stop", nil)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("delay/stop should not return 500: %s", w.Body.String())
	}
}

func TestIntegration_Pprof_Endpoints(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	endpoints := []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine",
		"/debug/pprof/heap",
		"/debug/pprof/allocs",
	}

	for _, ep := range endpoints {
		// pprof 限制为 localhost 访问，设置 RemoteAddr
		req := httptest.NewRequest("GET", ep, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		env.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s expected 200, got %d", ep, w.Code)
		}
	}

	// 非 localhost 应被拒绝
	req := httptest.NewRequest("GET", "/debug/pprof/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	env.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /debug/pprof/ from non-localhost expected 403, got %d", w.Code)
	}
}

func TestIntegration_Dashboard_LocalhostOnly(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	diagnosticEndpoints := []string{
		"/dashboard",
		"/api/v1/infra/system",
		"/api/v1/infra/goroutines",
	}

	// localhost should be allowed
	for _, ep := range diagnosticEndpoints {
		req := httptest.NewRequest("GET", ep, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		env.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s from localhost expected 200, got %d", ep, w.Code)
		}
	}

	// non-localhost should be rejected
	for _, ep := range diagnosticEndpoints {
		req := httptest.NewRequest("GET", ep, nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		env.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s from non-localhost expected 403, got %d", ep, w.Code)
		}
	}
}

func TestIntegration_Dashboard_BypassesTokenAuth(t *testing.T) {
	// When api_token is configured, dashboard must still work from localhost
	// without providing a token (it bypasses TokenAuth entirely)
	cfg := infra.DefaultConfig()
	cfg.Server.APIToken = "test-secret-token"

	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snap := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); pt.Wait() }()
	pt.Run(ctx)

	srv := api.NewServer(cfg, pt)
	handler := srv.Router()

	// Dashboard from localhost should be accessible even without token
	// (503 from /datasource is expected since dsMgr is nil in test, but proves no 401)
	for _, ep := range []string{"/dashboard", "/api/v1/infra/system", "/api/v1/infra/goroutines", "/api/v1/infra/datasource"} {
		req := httptest.NewRequest("GET", ep, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized {
			t.Errorf("GET %s from localhost (token configured) got 401 — should bypass TokenAuth", ep)
		}
	}

	// Business API without token should get 401
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/status without token expected 401, got %d", w.Code)
	}

	// Business API with token should get 200
	req = httptest.NewRequest("GET", "/api/v1/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /api/v1/status with token expected 200, got %d", w.Code)
	}

	// Business API with ?token= query param (non-WS) should be REJECTED (401)
	// query-token is only allowed for WebSocket upgrade requests
	req = httptest.NewRequest("GET", "/api/v1/status?token=test-secret-token", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/status?token=... (non-WS) expected 401, got %d — query token must only work for WebSocket", w.Code)
	}

	// WebSocket upgrade with ?token= query param should NOT get 401
	req = httptest.NewRequest("GET", "/ws/playback?token=test-secret-token", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Errorf("WS /ws/playback?token=... expected non-401 (WS upgrade), got 401 — query token should work for WebSocket")
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	env := NewTestEnv(t)
	defer env.Close()

	const concurrency = 20
	done := make(chan struct{}, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			env.DoRequest("GET", "/api/v1/status", nil)
			env.DoRequest("GET", "/api/v1/progress", nil)
		}()
	}

	for i := 0; i < concurrency; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent requests timed out")
		}
	}
}

// ─── 性能基线测量 ──────────────────────────────────────────

func BenchmarkIntegration_GetStatus(b *testing.B) {
	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snap := infra.NewSnapshotManager(b.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pt.Run(ctx)
	defer pt.Wait()

	srv := api.NewServer(cfg, pt)
	handler := srv.Router()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", w.Code)
		}
	}
}


