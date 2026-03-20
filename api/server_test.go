package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/infra"
	"github.com/rixingyingyao/playthread-go/models"
)

func newTestServer(t *testing.T) (*Server, context.CancelFunc) {
	t.Helper()
	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snapMgr := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snapMgr)

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)

	srv := NewServer(cfg, pt)
	return srv, func() {
		cancel()
		pt.Wait()
	}
}

func doJSON(t *testing.T, router http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func decodeResp(t *testing.T, rr *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解码失败: %v, body=%s", err, rr.Body.String())
	}
	return resp
}

// --- 查询类测试 ---

func TestGetStatus_Initial(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "GET", "/api/v1/status", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d: %s", rr.Code, rr.Body.String())
	}
	resp := decodeResp(t, rr)
	if resp.Code != 0 {
		t.Fatalf("期望 code=0，得到 %d", resp.Code)
	}

	data, _ := json.Marshal(resp.Data)
	var status StatusResponse
	json.Unmarshal(data, &status)
	if status.Status != "Stopped" {
		t.Errorf("初始状态应为 Stopped，得到 %s", status.Status)
	}
	if status.PlaylistLen != 0 {
		t.Errorf("初始播表长度应为 0，得到 %d", status.PlaylistLen)
	}
}

func TestGetProgress_NoProgram(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "GET", "/api/v1/progress", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d", rr.Code)
	}
	resp := decodeResp(t, rr)
	if resp.Code != 0 {
		t.Fatalf("期望 code=0，得到 %d", resp.Code)
	}
}

func TestGetPlaylist_Empty(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "GET", "/api/v1/playlist", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d", rr.Code)
	}
}

// --- 播表管理测试 ---

func TestLoadPlaylist(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	pl := models.Playlist{
		ID:   "test-pl-001",
		Date: time.Now(),
		Blocks: []models.TimeBlock{
			{
				ID: "block-1", Name: "测试时间块",
				StartTime: "08:00:00", EndTime: "09:00:00",
				Programs: []models.Program{
					{ID: "p1", Name: "测试节目1", Duration: 180000},
					{ID: "p2", Name: "测试节目2", Duration: 240000},
				},
			},
		},
	}

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/playlist/load", pl)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d: %s", rr.Code, rr.Body.String())
	}
	resp := decodeResp(t, rr)
	if resp.Code != 0 {
		t.Fatalf("加载播表失败: %s", resp.Message)
	}

	rr2 := doJSON(t, srv.Router(), "GET", "/api/v1/status", nil)
	resp2 := decodeResp(t, rr2)
	data, _ := json.Marshal(resp2.Data)
	var status StatusResponse
	json.Unmarshal(data, &status)
	if status.PlaylistLen != 2 {
		t.Errorf("播表长度应为 2，得到 %d", status.PlaylistLen)
	}
}

// --- 控制类测试 ---

func TestControlPause(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/pause", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d", rr.Code)
	}

	rr2 := doJSON(t, srv.Router(), "GET", "/api/v1/status", nil)
	resp2 := decodeResp(t, rr2)
	data, _ := json.Marshal(resp2.Data)
	var status StatusResponse
	json.Unmarshal(data, &status)
	if !status.Suspended {
		t.Error("暂停后 Suspended 应为 true")
	}
}

func TestControlPauseThenPlay(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	// 先 pause
	doJSON(t, srv.Router(), "POST", "/api/v1/control/pause", nil)

	// 再 play — 应清除 suspended 标志
	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/play", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d", rr.Code)
	}

	rr2 := doJSON(t, srv.Router(), "GET", "/api/v1/status", nil)
	resp2 := decodeResp(t, rr2)
	data, _ := json.Marshal(resp2.Data)
	var status StatusResponse
	json.Unmarshal(data, &status)
	if status.Suspended {
		t.Error("play 后 Suspended 应为 false")
	}
}

func TestControlNext(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/next", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d", rr.Code)
	}
}

func TestControlJump_NoPlaylist(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/jump", JumpRequest{Position: 5})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d: %s", rr.Code, rr.Body.String())
	}
}

func TestControlChangeStatus(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/status",
		ChangeStatusRequest{Status: "auto", Reason: "test"})
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d: %s", rr.Code, rr.Body.String())
	}

	rr2 := doJSON(t, srv.Router(), "GET", "/api/v1/status", nil)
	resp2 := decodeResp(t, rr2)
	data, _ := json.Marshal(resp2.Data)
	var status StatusResponse
	json.Unmarshal(data, &status)
	if status.Status != "Auto" {
		t.Errorf("状态应为 Auto，得到 %s", status.Status)
	}
}

func TestControlChangeStatus_InvalidStatus(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/status",
		ChangeStatusRequest{Status: "invalid"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d", rr.Code)
	}
}

func TestControlChangeStatus_IllegalTransition(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	// Stopped → Emergency 是非法路径
	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/status",
		ChangeStatusRequest{Status: "emergency", Reason: "test"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("期望 409，得到 %d: %s", rr.Code, rr.Body.String())
	}
}

// --- 垫乐控制测试 ---

func TestBlankControl(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/control/blank/start", nil)
	if rr.Code != 200 {
		t.Fatalf("垫乐启动失败: %d", rr.Code)
	}

	rr2 := doJSON(t, srv.Router(), "POST", "/api/v1/control/blank/stop", nil)
	if rr2.Code != 200 {
		t.Fatalf("垫乐停止失败: %d", rr2.Code)
	}
}

// --- 插播测试 ---

func TestIntercutStop_NotEmergency(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "POST", "/api/v1/intercut/stop", nil)
	if rr.Code != 200 {
		t.Fatalf("期望 200，得到 %d", rr.Code)
	}
}

// --- 中间件测试 ---

func TestMiddleware_RequestID(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	rr := doJSON(t, srv.Router(), "GET", "/api/v1/status", nil)
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("应返回 X-Request-ID 头")
	}
}

func TestMiddleware_CORS(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("OPTIONS", "/api/v1/status", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS 期望 204，得到 %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS 头缺失")
	}
}

func TestMiddleware_Recoverer(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 500 {
		t.Fatalf("panic 恢复后期望 500，得到 %d", rr.Code)
	}
}

// --- WebSocket 测试 ---

func TestWebSocket_ConnectAndReceive(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.hub.Run(ctx)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/playback"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer ws.Close()

	time.Sleep(100 * time.Millisecond)
	if srv.hub.ClientCount() != 1 {
		t.Errorf("期望 1 个 WS 客户端，得到 %d", srv.hub.ClientCount())
	}

	// 发送一个广播事件
	srv.hub.OnBroadcast(models.NewBroadcastEvent(models.EventStatusChanged, models.StatusChangeEvent{
		OldStatus: models.StatusStopped,
		NewStatus: models.StatusAuto,
		Reason:    "test",
	}))

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("WS 读取消息失败: %v", err)
	}

	var evt models.BroadcastEvent
	if err := json.Unmarshal(msg, &evt); err != nil {
		t.Fatalf("WS 消息解析失败: %v", err)
	}
	if evt.Type != models.EventStatusChanged {
		t.Errorf("期望事件类型 status_changed，得到 %s", evt.Type)
	}
}

// --- UDP 测试 ---

func TestUDP_Commands(t *testing.T) {
	cfg := infra.DefaultConfig()
	sm := core.NewStateMachine()
	eb := core.NewEventBus()
	snapMgr := infra.NewSnapshotManager(t.TempDir())
	pt := core.NewPlayThread(cfg, sm, eb, nil, snapMgr)

	ctx, cancel := context.WithCancel(context.Background())
	pt.Run(ctx)
	defer func() {
		cancel()
		pt.Wait()
	}()

	// 让系统自动分配端口
	listener := NewUDPListener("127.0.0.1:0", pt)
	go listener.Run(ctx)
	time.Sleep(300 * time.Millisecond)

	if listener.conn == nil {
		t.Fatal("UDP 监听器未启动")
	}
	actualAddr := listener.conn.LocalAddr().String()

	send := func(cmd string) string {
		conn, err := net.Dial("udp", actualAddr)
		if err != nil {
			t.Fatalf("UDP dial 失败: %v", err)
		}
		defer conn.Close()
		conn.Write([]byte(cmd))
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("UDP 读取 %q 响应失败: %v", cmd, err)
		}
		return string(buf[:n])
	}

	resp := send("status")
	if !strings.Contains(resp, "Stopped") {
		t.Errorf("UDP status 应包含 Stopped，得到: %s", resp)
	}

	resp = send("xyz")
	if !strings.Contains(resp, "unknown") {
		t.Errorf("UDP 未知命令应返回 unknown，得到: %s", resp)
	}

	resp = send("padding")
	if !strings.Contains(resp, `"code":0`) {
		t.Errorf("UDP padding 应返回 code=0，得到: %s", resp)
	}
}

// --- parseStatus 测试 ---

func TestParseStatus(t *testing.T) {
	tests := []struct {
		input  string
		expect models.Status
		err    bool
	}{
		{"stopped", models.StatusStopped, false},
		{"Auto", models.StatusAuto, false},
		{"MANUAL", models.StatusManual, false},
		{"live", models.StatusLive, false},
		{"redifdelay", models.StatusRedifDelay, false},
		{"delay", models.StatusRedifDelay, false},
		{"emergency", models.StatusEmergency, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		s, err := parseStatus(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseStatus(%q) 期望出错", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseStatus(%q) 意外错误: %v", tt.input, err)
			continue
		}
		if s != tt.expect {
			t.Errorf("parseStatus(%q) = %v, 期望 %v", tt.input, s, tt.expect)
		}
	}
}

// --- 认证/限流测试 ---

func TestTokenAuth_Reject(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := TokenAuth("secret123")(inner)

	// 无 token
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("无 token 期望 401，得到 %d", rr.Code)
	}

	// 错误 token
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("错误 token 期望 401，得到 %d", rr.Code)
	}
}

func TestTokenAuth_Accept(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := TokenAuth("secret123")(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("正确 token 期望 200，得到 %d", rr.Code)
	}
}

func TestRateLimiter_Exceed(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rl := NewRateLimiter(3)
	handler := rl.Handler(inner)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求期望 200，得到 %d", i+1, rr.Code)
		}
	}

	// 第 4 次应被限流
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("超限请求期望 429，得到 %d", rr.Code)
	}
}

func TestRateLimiter_ExpiryCleanup(t *testing.T) {
	rl := NewRateLimiter(100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Handler(inner)

	// 创建大量不同 IP 的请求以触发清理阈值
	for i := 0; i < rateLimiterMaxEntries/2+10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = fmt.Sprintf("10.0.%d.%d:12345", i/256, i%256)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	sizeBefore := rl.Size()
	if sizeBefore < rateLimiterMaxEntries/2 {
		t.Fatalf("期望至少 %d 条记录，实际 %d", rateLimiterMaxEntries/2, sizeBefore)
	}

	// 手动把所有记录的 windowAt 设为过期
	rl.mu.Lock()
	past := time.Now().Add(-2 * rateLimiterTTL)
	for _, v := range rl.clients {
		v.windowAt = past
	}
	rl.mu.Unlock()

	// 再发一次请求触发惰性清理
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.99.99:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	sizeAfter := rl.Size()
	if sizeAfter >= sizeBefore {
		t.Errorf("清理后记录数应减少，之前 %d，之后 %d", sizeBefore, sizeAfter)
	}
	// 旧记录应被清除，只留新请求的 1 条
	if sizeAfter > 1 {
		t.Errorf("期望清理后仅剩 1 条，实际 %d", sizeAfter)
	}
}
