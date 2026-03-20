// dashboard 提供内嵌的可视化监控仪表盘。
// 访问 /dashboard 即可查看系统架构、运行时指标和 goroutine 详情。
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"
)

// SystemInfo 系统信息
type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	Compiler     string `json:"compiler"`
	Version      string `json:"version"`
	BuildTime    string `json:"build_time"`
	Uptime       string `json:"uptime"`
}

var startTime = time.Now()

// handleDashboard 提供仪表盘 HTML 页面
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

// handleSystemInfo 返回系统信息
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(startTime)
	info := SystemInfo{
		GoVersion:    runtime.Version(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		Compiler:     runtime.Compiler,
		Uptime:       formatDuration(uptime),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 0,
		"data": info,
	})
}

// handleGoroutines 返回 goroutine 栈信息摘要
func (s *Server) handleGoroutines(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 1<<20) // 1MB
	n := runtime.Stack(buf, true)
	stacks := string(buf[:n])

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	var gcInfo debug.GCStats
	debug.ReadGCStats(&gcInfo)

	data := map[string]interface{}{
		"num_goroutine": runtime.NumGoroutine(),
		"stacks":        stacks,
		"memory": map[string]interface{}{
			"alloc_mb":          float64(ms.Alloc) / 1024 / 1024,
			"total_alloc_mb":    float64(ms.TotalAlloc) / 1024 / 1024,
			"sys_mb":            float64(ms.Sys) / 1024 / 1024,
			"heap_alloc_mb":     float64(ms.HeapAlloc) / 1024 / 1024,
			"heap_sys_mb":       float64(ms.HeapSys) / 1024 / 1024,
			"heap_idle_mb":      float64(ms.HeapIdle) / 1024 / 1024,
			"heap_inuse_mb":     float64(ms.HeapInuse) / 1024 / 1024,
			"stack_inuse_mb":    float64(ms.StackInuse) / 1024 / 1024,
			"num_gc":            ms.NumGC,
			"gc_pause_total_ms": float64(ms.PauseTotalNs) / 1e6,
			"last_gc":           time.Unix(0, int64(ms.LastGC)).Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 0,
		"data": data,
	})
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Playthread-Go 播出服务 · 监控面板</title>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  font-family: -apple-system, BlinkMacSystemFont, 'Microsoft YaHei', 'Segoe UI', sans-serif;
  background: #0f172a; color: #e2e8f0; min-height: 100vh;
}
.header {
  background: linear-gradient(135deg, #1e293b, #334155);
  padding: 20px 32px; border-bottom: 1px solid #475569;
  display: flex; justify-content: space-between; align-items: center;
}
.header h1 { font-size: 22px; font-weight: 600; color: #38bdf8; }
.header .subtitle { font-size: 13px; color: #64748b; margin-top: 2px; }
.header .meta { font-size: 13px; color: #94a3b8; text-align: right; }
.container { max-width: 1400px; margin: 0 auto; padding: 24px; }
.grid { display: grid; gap: 20px; }
.grid-2 { grid-template-columns: 1fr 1fr; }
.grid-4 { grid-template-columns: 1fr 1fr 1fr 1fr; }
.card {
  background: #1e293b; border-radius: 12px; padding: 20px;
  border: 1px solid #334155; transition: border-color 0.2s;
}
.card:hover { border-color: #475569; }
.card h2 { font-size: 14px; color: #94a3b8; margin-bottom: 12px; letter-spacing: 0.5px; }
.metric-value { font-size: 36px; font-weight: 700; color: #f1f5f9; }
.metric-unit { font-size: 14px; color: #64748b; margin-left: 4px; }
.metric-label { font-size: 12px; color: #64748b; margin-top: 4px; }
.metric-hint { font-size: 11px; color: #475569; margin-top: 6px; line-height: 1.4; }
.status-dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; margin-right: 6px; }
.status-ok { background: #22c55e; box-shadow: 0 0 6px #22c55e40; }
.status-warn { background: #eab308; box-shadow: 0 0 6px #eab30840; }
.status-err { background: #ef4444; box-shadow: 0 0 6px #ef444440; }
.full-width { grid-column: 1 / -1; }
.arch-diagram { background: #0f172a; border-radius: 8px; padding: 16px; overflow-x: auto; }
.arch-diagram .mermaid { text-align: center; }
pre.goroutine-stack {
  background: #0f172a; color: #a5f3fc; padding: 16px; border-radius: 8px;
  font-size: 12px; line-height: 1.5; max-height: 500px; overflow: auto;
  white-space: pre-wrap; word-break: break-all;
}
.chart-container { position: relative; height: 200px; }
canvas { width: 100% !important; height: 100% !important; }
.progress-bar {
  height: 8px; border-radius: 4px; background: #334155; overflow: hidden; margin-top: 8px;
}
.progress-fill { height: 100%; border-radius: 4px; transition: width 0.5s; }
.info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.info-item { padding: 8px 12px; background: #0f172a; border-radius: 6px; }
.info-label { font-size: 11px; color: #64748b; }
.info-value { font-size: 14px; color: #e2e8f0; margin-top: 2px; }
.help-section {
  background: #1e293b; border: 1px solid #334155; border-radius: 12px;
  padding: 20px; margin-bottom: 20px;
}
.help-section summary {
  cursor: pointer; font-size: 14px; color: #38bdf8; font-weight: 500;
  list-style: none; display: flex; align-items: center; gap: 8px;
}
.help-section summary::before { content: '💡'; }
.help-section .help-content { margin-top: 12px; }
.help-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-top: 8px; }
.help-item { padding: 10px 14px; background: #0f172a; border-radius: 8px; border-left: 3px solid #38bdf8; }
.help-item.green { border-left-color: #22c55e; }
.help-item.purple { border-left-color: #a78bfa; }
.help-item.orange { border-left-color: #fb923c; }
.help-item dt { font-size: 13px; color: #e2e8f0; font-weight: 500; margin-bottom: 4px; }
.help-item dd { font-size: 12px; color: #94a3b8; line-height: 1.5; }
.ds-status-label { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 500; }
.ds-normal { background: #22c55e20; color: #22c55e; }
.ds-fallback { background: #eab30820; color: #eab308; }
.ds-offline { background: #ef444420; color: #ef4444; }
.section-desc { font-size: 12px; color: #64748b; margin-bottom: 12px; }
@media (max-width: 900px) { .grid-2, .grid-4, .help-grid { grid-template-columns: 1fr; } }
#goroutineFilter { width: 100%; padding: 8px 12px; border-radius: 6px; border: 1px solid #334155;
  background: #0f172a; color: #e2e8f0; font-size: 13px; margin-bottom: 12px; }
</style>
</head>
<body>

<div class="header">
  <div>
    <h1>🎵 广播播出服务 · 监控面板</h1>
    <div class="subtitle">Playthread-Go — 轻量化广播播出端后台服务</div>
  </div>
  <div class="meta">
    <div><span id="headerStatus"><span class="status-dot status-ok"></span>服务运行中</span></div>
    <div style="margin-top:4px">已运行: <span id="headerUptime">-</span> &nbsp;|&nbsp; 每 2 秒自动刷新</div>
  </div>
</div>

<div class="container">

  <!-- 数据说明（可折叠） -->
  <details class="help-section" open>
    <summary>如何看这些数据？点击展开/收起说明</summary>
    <div class="help-content">
      <p style="font-size:13px;color:#94a3b8;margin-bottom:8px">此面板实时显示播出服务的运行状态。下方是各指标的含义，方便您判断服务是否正常：</p>
      <div class="help-grid">
        <dl class="help-item">
          <dt>📊 并发任务数（Goroutines）</dt>
          <dd>当前服务内同时运行的任务数量。正常范围 20~100。如果持续增长不下降，可能存在任务泄漏，需要关注。</dd>
        </dl>
        <dl class="help-item green">
          <dt>💾 堆内存（Heap）</dt>
          <dd>服务当前使用的主要内存。正常播出状态一般在 5~50 MB。如果持续增长超过 200 MB 且不回落，可能存在内存泄漏。</dd>
        </dl>
        <dl class="help-item purple">
          <dt>📚 栈内存（Stack）</dt>
          <dd>每个并发任务独立使用的内存空间。通常很小（&lt;5 MB），不需要特别关注。</dd>
        </dl>
        <dl class="help-item orange">
          <dt>♻️ 垃圾回收（GC）</dt>
          <dd>系统自动清理不再使用的内存的次数。次数增长是正常的。关注"累计暂停"时间——如果超过 1000 ms 说明 GC 压力较大。</dd>
        </dl>
        <dl class="help-item">
          <dt>🌐 数据源状态</dt>
          <dd>显示当前从哪里获取播单数据。正常时使用"云端"，当云端不可达时自动降级到"本地中心"。如果两者都未配置 URL，则显示未配置。</dd>
        </dl>
        <dl class="help-item green">
          <dt>📈 趋势图表</dt>
          <dd>显示最近 2 分钟（60 次采样 × 2 秒间隔）的变化趋势。趋势平稳说明服务运行稳定，波动大或持续上升需要关注。</dd>
        </dl>
      </div>
    </div>
  </details>

  <!-- 概览指标卡 -->
  <div class="grid grid-4" style="margin-bottom:20px">
    <div class="card">
      <h2>📊 并发任务数</h2>
      <div class="metric-value" id="metricGoroutines">-</div>
      <div class="metric-label">当前活跃任务</div>
      <div class="metric-hint" id="goroutineHint">正常范围: 20~100</div>
    </div>
    <div class="card">
      <h2>💾 堆内存使用</h2>
      <div><span class="metric-value" id="metricHeap">-</span><span class="metric-unit">MB</span></div>
      <div class="progress-bar"><div class="progress-fill" id="heapBar" style="width:0;background:#38bdf8"></div></div>
      <div class="metric-label">已用 / 已申请</div>
      <div class="metric-hint">蓝色条越短越好，表示内存利用率</div>
    </div>
    <div class="card">
      <h2>📚 栈内存使用</h2>
      <div><span class="metric-value" id="metricStack">-</span><span class="metric-unit">MB</span></div>
      <div class="metric-label">各任务的独立内存</div>
      <div class="metric-hint">通常 &lt; 5 MB，无需关注</div>
    </div>
    <div class="card">
      <h2>♻️ 垃圾回收</h2>
      <div><span class="metric-value" id="metricGC">-</span><span class="metric-unit">次</span></div>
      <div class="metric-label">累计暂停: <span id="metricGCPause">-</span> ms</div>
      <div class="metric-hint">暂停 &lt; 100ms 为正常</div>
    </div>
  </div>

  <!-- 架构图（独占一行，横向布局） -->
  <div class="card full-width" style="margin-bottom:20px">
    <h2>🏗️ 系统架构图</h2>
    <div class="section-desc">展示播出服务的整体架构：左侧为主控进程负责业务逻辑，右侧为音频进程负责实际播放，两者通过进程通信连接。</div>
    <div class="arch-diagram">
      <pre class="mermaid">
graph LR
  Client["📱 前端工作站"]
  Cloud["☁️ 云端平台"]
  Center["🏢 本地中心"]

  subgraph 主控["🖥️ 主控进程 playthread.exe"]
    API["🌐 接口层\nHTTP · WebSocket · UDP"]
    Core["🎯 播出编排器\n状态机 · 定时播出\n垫乐 · 插播 · 通道保持"]
    DSM["📡 数据源管理\n云端/本地自动切换\n素材缓存 · 离线暂存"]
    DB["💿 本地数据库\nSQLite"]
    MON["📊 运行监控"]
  end

  subgraph 音频["🎵 音频进程 audio-service.exe"]
    BASS["🔊 BASS 音频引擎\n12路虚拟通道\n音频电平 · 录音 · 混音"]
  end

  Client -- "REST / WS" --> API
  Cloud -- "HTTP / WS" --> DSM
  Center -- "HTTP" --> DSM
  API --> Core
  Core --> DB
  DSM --> Core
  Core -- "JSON Line\n进程通信" --> BASS
  Core --> MON

  style 主控 fill:#1e293b,stroke:#38bdf8,color:#e2e8f0
  style 音频 fill:#1e293b,stroke:#22c55e,color:#e2e8f0
  style Cloud fill:#0f172a,stroke:#a78bfa,color:#e2e8f0
  style Center fill:#0f172a,stroke:#fb923c,color:#e2e8f0
  style Client fill:#0f172a,stroke:#38bdf8,color:#e2e8f0
      </pre>
    </div>
  </div>

  <!-- 系统信息 -->
  <div class="grid grid-2" style="margin-bottom:20px">
    <div class="card">
      <h2>⚙️ 系统信息</h2>
      <div class="section-desc">当前服务的运行环境和基本参数</div>
      <div class="info-grid" id="sysInfoGrid">
        <div class="info-item"><div class="info-label">Go 版本</div><div class="info-value" id="siGoVer">-</div></div>
        <div class="info-item"><div class="info-label">运行平台</div><div class="info-value" id="siPlatform">-</div></div>
        <div class="info-item"><div class="info-label">CPU 核心数</div><div class="info-value" id="siCPU">-</div></div>
        <div class="info-item"><div class="info-label">编译器</div><div class="info-value" id="siCompiler">-</div></div>
        <div class="info-item"><div class="info-label">已运行时间</div><div class="info-value" id="siUptime">-</div></div>
        <div class="info-item"><div class="info-label">当前任务数</div><div class="info-value" id="siGoroutines">-</div></div>
      </div>

      <h2 style="margin-top:24px">💾 内存分布详情</h2>
      <div class="section-desc">各类内存的使用量一览，越短表示用量越少</div>
      <div id="memoryBars" style="margin-top:8px"></div>
    </div>

    <div class="card">
      <h2>🌐 数据源状态</h2>
      <div class="section-desc">播单数据的获取来源及连接状态</div>
      <div class="info-grid" id="dsInfoGrid">
        <div class="info-item"><div class="info-label">当前数据源</div><div class="info-value" id="dsActive">-</div></div>
        <div class="info-item"><div class="info-label">连接状态</div><div class="info-value" id="dsState">-</div></div>
        <div class="info-item"><div class="info-label">云端连接失败次数</div><div class="info-value" id="dsFail">-</div></div>
        <div class="info-item"><div class="info-label">可回切到云端</div><div class="info-value" id="dsSwitch">-</div></div>
      </div>
    </div>
  </div>

  <!-- 趋势图 -->
  <div class="grid grid-2" style="margin-bottom:20px">
    <div class="card">
      <h2>📈 并发任务数趋势（最近 2 分钟）</h2>
      <div class="section-desc">趋势平稳 = 正常 | 持续上升 = 可能有任务泄漏</div>
      <div class="chart-container">
        <canvas id="goroutineChart"></canvas>
      </div>
    </div>
    <div class="card">
      <h2>📈 堆内存趋势（最近 2 分钟）</h2>
      <div class="section-desc">趋势平稳 = 正常 | 持续上升不回落 = 可能有内存泄漏</div>
      <div class="chart-container">
        <canvas id="memoryChart"></canvas>
      </div>
    </div>
  </div>

  <!-- Goroutine 栈详情 -->
  <div class="card full-width">
    <h2>🔍 任务详情（Goroutine 栈 — 供开发人员排查用）</h2>
    <div class="section-desc">列出所有正在运行的并发任务及其调用栈，可搜索关键字过滤</div>
    <input type="text" id="goroutineFilter" placeholder="🔍 输入关键字搜索任务（例如：playthread, bass, http）">
    <pre class="goroutine-stack" id="goroutineStacks">加载中...</pre>
  </div>
</div>

<script>
mermaid.initialize({
  startOnLoad: true,
  theme: 'dark',
  themeVariables: {
    primaryColor: '#1e293b',
    primaryTextColor: '#e2e8f0',
    primaryBorderColor: '#38bdf8',
    lineColor: '#64748b',
    secondaryColor: '#334155',
    tertiaryColor: '#0f172a',
    fontSize: '12px'
  }
});

class MiniChart {
  constructor(canvasId, color, label) {
    this.canvas = document.getElementById(canvasId);
    this.ctx = this.canvas.getContext('2d');
    this.data = [];
    this.maxPoints = 60;
    this.color = color;
    this.label = label;
  }
  push(value) {
    this.data.push(value);
    if (this.data.length > this.maxPoints) this.data.shift();
    this.draw();
  }
  draw() {
    const c = this.canvas, ctx = this.ctx;
    const dpr = window.devicePixelRatio || 1;
    const rect = c.getBoundingClientRect();
    c.width = rect.width * dpr;
    c.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    const w = rect.width, h = rect.height;
    ctx.clearRect(0, 0, w, h);
    if (this.data.length < 2) return;
    const max = Math.max(...this.data) * 1.2 || 1;
    const stepX = w / (this.maxPoints - 1);
    ctx.strokeStyle = '#1e293b'; ctx.lineWidth = 1;
    for (let i = 0; i < 4; i++) { const y = h*(i/4); ctx.beginPath(); ctx.moveTo(0,y); ctx.lineTo(w,y); ctx.stroke(); }
    ctx.strokeStyle = this.color; ctx.lineWidth = 2; ctx.beginPath();
    for (let i = 0; i < this.data.length; i++) {
      const x = i*stepX, y = h - (this.data[i]/max)*h;
      if (i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
    }
    ctx.stroke();
    ctx.lineTo((this.data.length-1)*stepX, h); ctx.lineTo(0, h); ctx.closePath();
    ctx.fillStyle = this.color + '20'; ctx.fill();
    const last = this.data[this.data.length-1];
    ctx.fillStyle = '#e2e8f0'; ctx.font = '12px sans-serif';
    ctx.fillText('当前: ' + last.toFixed(1) + '  |  峰值: ' + max.toFixed(1), 8, 16);
  }
}

const goroutineChart = new MiniChart('goroutineChart', '#38bdf8', '任务数');
const memoryChart = new MiniChart('memoryChart', '#22c55e', '堆内存 MB');

let goroutineStacksRaw = '';

function updateFilter() {
  const filter = document.getElementById('goroutineFilter').value.toLowerCase();
  const el = document.getElementById('goroutineStacks');
  if (!filter) { el.textContent = goroutineStacksRaw; return; }
  const blocks = goroutineStacksRaw.split('\n\n');
  const filtered = blocks.filter(b => b.toLowerCase().includes(filter));
  el.textContent = filtered.length ? filtered.join('\n\n') : '没有匹配的任务';
}
document.getElementById('goroutineFilter').addEventListener('input', updateFilter);

async function fetchJSON(url) {
  try { const r = await fetch(url); return await r.json(); } catch(e) { return null; }
}

// 数据源状态翻译
function translateDS(active) {
  const map = { Cloud: '云端', Center: '本地中心' };
  return map[active] || active || '-';
}
function translateState(state) {
  const map = { Normal: '正常连接', Fallback: '已降级到本地中心', PendingConfirm: '等待确认回切' };
  return map[state] || state || '-';
}
function stateClass(state) {
  if (state === 'Normal') return 'ds-normal';
  if (state === 'Fallback') return 'ds-fallback';
  return 'ds-offline';
}

async function refresh() {
  const gr = await fetchJSON('/api/v1/infra/goroutines');
  if (gr && gr.data) {
    const d = gr.data;
    document.getElementById('metricGoroutines').textContent = d.num_goroutine;
    goroutineStacksRaw = d.stacks;
    updateFilter();

    // 动态提示
    const ghint = document.getElementById('goroutineHint');
    if (d.num_goroutine > 100) {
      ghint.textContent = '⚠️ 任务数偏高，请关注是否有泄漏';
      ghint.style.color = '#eab308';
    } else {
      ghint.textContent = '✅ 正常范围（20~100）';
      ghint.style.color = '#475569';
    }

    if (d.memory) {
      const m = d.memory;
      document.getElementById('metricHeap').textContent = m.heap_alloc_mb.toFixed(1);
      document.getElementById('metricStack').textContent = m.stack_inuse_mb.toFixed(1);
      document.getElementById('metricGC').textContent = m.num_gc;
      document.getElementById('metricGCPause').textContent = m.gc_pause_total_ms.toFixed(1);

      const pct = m.heap_sys_mb > 0 ? (m.heap_alloc_mb / m.heap_sys_mb * 100) : 0;
      document.getElementById('heapBar').style.width = pct.toFixed(0) + '%';

      goroutineChart.push(d.num_goroutine);
      memoryChart.push(m.heap_alloc_mb);

      const bars = [
        { label: '堆内存 - 已用（程序正在使用的内存）', value: m.heap_alloc_mb, color: '#38bdf8' },
        { label: '堆内存 - 空闲（已申请但暂未使用）', value: m.heap_idle_mb, color: '#334155' },
        { label: '栈内存（各任务的独立空间）', value: m.stack_inuse_mb, color: '#22c55e' },
        { label: '系统总占用（进程向操作系统申请的总量）', value: m.sys_mb, color: '#a78bfa' },
      ];
      const maxMem = Math.max(...bars.map(b => b.value)) || 1;
      document.getElementById('memoryBars').innerHTML = bars.map(b =>
        '<div style="margin-bottom:6px">' +
        '<div style="font-size:11px;color:#94a3b8;margin-bottom:2px">' + b.label + ': ' + b.value.toFixed(1) + ' MB</div>' +
        '<div class="progress-bar"><div class="progress-fill" style="width:' + (b.value/maxMem*100).toFixed(0) + '%;background:' + b.color + '"></div></div></div>'
      ).join('');
    }
  }

  const si = await fetchJSON('/api/v1/infra/system');
  if (si && si.data) {
    const d = si.data;
    document.getElementById('siGoVer').textContent = d.go_version;
    document.getElementById('siPlatform').textContent = d.goos + '/' + d.goarch;
    document.getElementById('siCPU').textContent = d.num_cpu + ' 核';
    document.getElementById('siCompiler').textContent = d.compiler;
    document.getElementById('siUptime').textContent = d.uptime;
    document.getElementById('siGoroutines').textContent = d.num_goroutine;
    document.getElementById('headerUptime').textContent = d.uptime;
  }

  const ds = await fetchJSON('/api/v1/infra/datasource');
  if (ds && ds.data) {
    const d = ds.data;
    document.getElementById('dsActive').textContent = translateDS(d.active);
    const stateEl = document.getElementById('dsState');
    stateEl.innerHTML = '<span class="ds-status-label ' + stateClass(d.state) + '">' + translateState(d.state) + '</span>';
    document.getElementById('dsFail').textContent = (d.cloud_fail_count ?? 0) + ' 次';
    document.getElementById('dsSwitch').textContent = d.can_switch_back ? '✅ 可以' : '—';
  }
}

setInterval(refresh, 2000);
refresh();
</script>
</body>
</html>`
