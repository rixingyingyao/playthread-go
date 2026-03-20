package infra

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ─── Monitor Tests ──────────────────────────────────────────

func TestMonitor_RecordCrash(t *testing.T) {
	m := NewMonitor(&MonitorConfig{MemoryCheckIntervalS: 3600, MemoryWarnThresholdMB: 500}, t.TempDir())

	m.RecordCrash("audio-service", -1)
	m.RecordCrash("audio-service", 1)
	m.RecordCrash("other-proc", 2)

	stats := m.CrashStats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(stats))
	}

	audio := stats["audio-service"]
	if audio.TotalCrashes != 2 {
		t.Errorf("expected 2 crashes, got %d", audio.TotalCrashes)
	}
	if audio.ConsecutiveNum != 2 {
		t.Errorf("expected consecutive=2, got %d", audio.ConsecutiveNum)
	}
	if audio.LastExitCode != 1 {
		t.Errorf("expected last exit_code=1, got %d", audio.LastExitCode)
	}
}

func TestMonitor_ResetConsecutive(t *testing.T) {
	m := NewMonitor(&MonitorConfig{MemoryCheckIntervalS: 3600}, t.TempDir())

	m.RecordCrash("audio-service", -1)
	m.RecordCrash("audio-service", -1)
	m.RecordCrash("audio-service", -1)

	m.ResetConsecutive("audio-service")

	stats := m.CrashStats()
	if stats["audio-service"].ConsecutiveNum != 0 {
		t.Errorf("expected consecutive=0 after reset, got %d", stats["audio-service"].ConsecutiveNum)
	}
	if stats["audio-service"].TotalCrashes != 3 {
		t.Errorf("total should still be 3, got %d", stats["audio-service"].TotalCrashes)
	}
}

func TestMonitor_CrashLog_Written(t *testing.T) {
	dir := t.TempDir()
	m := NewMonitor(&MonitorConfig{MemoryCheckIntervalS: 3600}, dir)

	m.RecordCrash("test-proc", 42)

	logPath := filepath.Join(dir, "crash.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("crash.log not created: %v", err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Fatal("crash.log is empty")
	}
	if !contains(content, "test-proc") || !contains(content, "exit_code=42") {
		t.Errorf("crash.log missing expected fields: %s", content)
	}
}

func TestMonitor_CollectMetrics(t *testing.T) {
	m := NewMonitor(&MonitorConfig{MemoryCheckIntervalS: 1, MemoryWarnThresholdMB: 500}, t.TempDir())

	m.collectMetrics()

	metrics := m.Metrics()
	if metrics.NumGoroutine == 0 {
		t.Error("expected non-zero goroutine count")
	}
	if metrics.HeapAllocMB <= 0 {
		t.Error("expected non-zero heap allocation")
	}
	if metrics.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestMonitor_Run_Cancellable(t *testing.T) {
	m := NewMonitor(&MonitorConfig{MemoryCheckIntervalS: 1}, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Monitor.Run did not exit after cancel")
	}
}

// ─── Updater Tests ──────────────────────────────────────────

func TestUpdater_Apply_DownloadAndReplace(t *testing.T) {
	// 创建一个模拟下载服务器
	content := []byte("new-binary-v2")
	h := md5.Sum(content)
	expectedMD5 := hex.EncodeToString(h[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	// 创建旧的 "playthread" 文件
	oldPath := filepath.Join(dir, "playthread")
	os.WriteFile(oldPath, []byte("old-binary-v1"), 0755)

	updater := NewUpdater("v1")
	// 替换 exePath 获取逻辑：直接用 downloadAndReplace
	err := updater.downloadAndReplace(context.Background(), server.URL+"/playthread", expectedMD5, oldPath)
	if err != nil {
		t.Fatalf("downloadAndReplace failed: %v", err)
	}

	// 验证新文件内容
	got, _ := os.ReadFile(oldPath)
	if string(got) != "new-binary-v2" {
		t.Errorf("expected new content, got: %s", string(got))
	}

	// 验证备份文件
	bakPath := oldPath + ".bak"
	bakContent, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if string(bakContent) != "old-binary-v1" {
		t.Errorf("backup content mismatch: %s", string(bakContent))
	}
}

func TestUpdater_Apply_MD5Mismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("some content"))
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "binary")
	os.WriteFile(target, []byte("old"), 0755)

	updater := NewUpdater("v1")
	err := updater.downloadAndReplace(context.Background(), server.URL+"/file", "wrong_md5_hash", target)
	if err == nil {
		t.Fatal("expected MD5 mismatch error")
	}
	if !contains(err.Error(), "MD5 校验失败") {
		t.Errorf("unexpected error: %v", err)
	}

	// 原文件不应被修改
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Errorf("original file should be preserved, got: %s", string(got))
	}
}

func TestUpdater_Apply_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "binary")
	os.WriteFile(target, []byte("old"), 0755)

	updater := NewUpdater("v1")
	err := updater.downloadAndReplace(context.Background(), server.URL+"/file", "", target)
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}

func TestUpdater_IsUpdating(t *testing.T) {
	updater := NewUpdater("v1")
	if updater.IsUpdating() {
		t.Error("should not be updating initially")
	}
}

func TestUpdater_ConcurrentApply(t *testing.T) {
	// 慢速服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte("data"))
	}))
	defer server.Close()

	updater := NewUpdater("v1")
	info := &UpdateInfo{
		Version:       "v2",
		PlaythreadURL: server.URL + "/playthread",
	}

	// 第一次升级在后台运行
	done := make(chan *UpdateResult, 1)
	go func() {
		done <- updater.Apply(context.Background(), info)
	}()

	// 等待第一次升级开始
	time.Sleep(100 * time.Millisecond)

	// 第二次应该被拒绝
	result2 := updater.Apply(context.Background(), info)
	if result2.Error == "" {
		t.Error("concurrent apply should be rejected")
	}

	<-done // 等待第一次完成
}

func TestFileMD5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	content := []byte("hello world")
	os.WriteFile(path, content, 0644)

	h := md5.Sum(content)
	expected := hex.EncodeToString(h[:])

	got, err := fileMD5(path)
	if err != nil {
		t.Fatalf("fileMD5 failed: %v", err)
	}
	if got != expected {
		t.Errorf("md5 mismatch: expected=%s got=%s", expected, got)
	}
}

// contains 检查字符串包含关系（避免导入 strings 包）
func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchContains(s, sub)
}

func searchContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func init() {
	// 确保 fmt 被使用
	_ = fmt.Sprintf
}
