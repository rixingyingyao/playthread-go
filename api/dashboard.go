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
<title>Playthread-Go 监控仪表盘</title>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: #0f172a; color: #e2e8f0; min-height: 100vh;
}
.header {
  background: linear-gradient(135deg, #1e293b, #334155);
  padding: 20px 32px; border-bottom: 1px solid #475569;
  display: flex; justify-content: space-between; align-items: center;
}
.header h1 { font-size: 24px; font-weight: 600; color: #38bdf8; }
.header .meta { font-size: 13px; color: #94a3b8; }
.container { max-width: 1400px; margin: 0 auto; padding: 24px; }
.grid { display: grid; gap: 20px; }
.grid-2 { grid-template-columns: 1fr 1fr; }
.grid-3 { grid-template-columns: 1fr 1fr 1fr; }
.grid-4 { grid-template-columns: 1fr 1fr 1fr 1fr; }
.card {
  background: #1e293b; border-radius: 12px; padding: 20px;
  border: 1px solid #334155; transition: border-color 0.2s;
}
.card:hover { border-color: #475569; }
.card h2 { font-size: 14px; color: #94a3b8; margin-bottom: 16px; text-transform: uppercase; letter-spacing: 1px; }
.metric-value { font-size: 36px; font-weight: 700; color: #f1f5f9; }
.metric-unit { font-size: 14px; color: #64748b; margin-left: 4px; }
.metric-label { font-size: 12px; color: #64748b; margin-top: 4px; }
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
.tab-bar { display: flex; gap: 8px; margin-bottom: 16px; }
.tab-btn {
  padding: 6px 16px; border-radius: 6px; border: 1px solid #334155;
  background: transparent; color: #94a3b8; cursor: pointer; font-size: 13px;
}
.tab-btn.active { background: #38bdf8; color: #0f172a; border-color: #38bdf8; }
.progress-bar {
  height: 8px; border-radius: 4px; background: #334155; overflow: hidden; margin-top: 8px;
}
.progress-fill { height: 100%; border-radius: 4px; transition: width 0.5s; }
.sys-info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.sys-info-item { padding: 8px 12px; background: #0f172a; border-radius: 6px; }
.sys-info-label { font-size: 11px; color: #64748b; }
.sys-info-value { font-size: 14px; color: #e2e8f0; margin-top: 2px; }
@media (max-width: 900px) { .grid-2, .grid-3, .grid-4 { grid-template-columns: 1fr; } }
#goroutineFilter { width: 100%; padding: 8px 12px; border-radius: 6px; border: 1px solid #334155;
  background: #0f172a; color: #e2e8f0; font-size: 13px; margin-bottom: 12px; }
</style>
</head>
<body>

<div class="header">
  <h1>🎵 Playthread-Go 监控仪表盘</h1>
  <div class="meta">
    <span id="headerStatus"><span class="status-dot status-ok"></span>运行中</span>
    &nbsp;|&nbsp; 运行时间: <span id="headerUptime">-</span>
    &nbsp;|&nbsp; 刷新间隔: 2s
  </div>
</div>

<div class="container">
  <!-- 概览指标卡 -->
  <div class="grid grid-4" style="margin-bottom:20px">
    <div class="card">
      <h2>Goroutines</h2>
      <div class="metric-value" id="metricGoroutines">-</div>
      <div class="metric-label">活跃协程数</div>
    </div>
    <div class="card">
      <h2>堆内存</h2>
      <div><span class="metric-value" id="metricHeap">-</span><span class="metric-unit">MB</span></div>
      <div class="progress-bar"><div class="progress-fill" id="heapBar" style="width:0;background:#38bdf8"></div></div>
      <div class="metric-label">HeapAlloc / HeapSys</div>
    </div>
    <div class="card">
      <h2>栈内存</h2>
      <div><span class="metric-value" id="metricStack">-</span><span class="metric-unit">MB</span></div>
      <div class="metric-label">StackInUse</div>
    </div>
    <div class="card">
      <h2>GC 统计</h2>
      <div><span class="metric-value" id="metricGC">-</span><span class="metric-unit">次</span></div>
      <div class="metric-label">累计暂停: <span id="metricGCPause">-</span> ms</div>
    </div>
  </div>

  <!-- 架构图 + 系统信息 -->
  <div class="grid grid-2" style="margin-bottom:20px">
    <div class="card">
      <h2>系统架构</h2>
      <div class="arch-diagram">
        <pre class="mermaid">
graph TB
  subgraph 主控进程["🖥️ playthread.exe (CGO_ENABLED=0)"]
    direction TB
    Main["main.go<br/>入口/信号/服务模式"]
    Core["core/<br/>PlayThread 编排器"]
    SM["StateMachine<br/>六状态机"]
    FTM["FixTimeManager<br/>定时任务"]
    BM["BlankManager<br/>垫乐填充"]
    IM["IntercutManager<br/>插播管理"]
    CH["ChannelHold<br/>通道保持"]
    API["api/<br/>HTTP+WS+UDP"]
    Bridge["bridge/<br/>ProcessManager+IPC"]
    DSM["infra/<br/>DataSourceManager"]
    FC["FileCache<br/>素材缓存"]
    OS_["OfflineStore<br/>断网暂存"]
    MON["Monitor<br/>运行时监控"]
    DB["db/<br/>SQLite"]

    Main --> Core
    Core --> SM
    Core --> FTM
    Core --> BM
    Core --> IM
    Core --> CH
    Main --> API
    Core --> Bridge
    Main --> DSM
    DSM --> FC
    DSM --> OS_
    Main --> MON
    Core --> DB
  end

  subgraph 播放服务["🎵 audio-service.exe (CGO_ENABLED=1)"]
    direction TB
    IPC["IPC Server<br/>JSON Line"]
    BASS["BASS Engine<br/>LockOSThread"]
    VC["VirtualChannel<br/>12通道拓扑"]
    LM["LevelMeter<br/>音频电平"]
    REC["Recorder<br/>音频录制"]
    CM["ChannelMatrix<br/>通道矩阵"]

    IPC --> BASS
    BASS --> VC
    BASS --> LM
    BASS --> REC
    BASS --> CM
  end

  Bridge -- "stdin/stdout<br/>JSON Line" --> IPC
  DSM -- "HTTP/WS" --> Cloud["☁️ 云端 SAAS"]
  DSM -- "HTTP" --> Center["🏢 本地中心"]
  API -- "REST/WS" --> Client["📱 工作中心前端"]

  style 主控进程 fill:#1e293b,stroke:#38bdf8,color:#e2e8f0
  style 播放服务 fill:#1e293b,stroke:#22c55e,color:#e2e8f0
  style Cloud fill:#0f172a,stroke:#a78bfa,color:#e2e8f0
  style Center fill:#0f172a,stroke:#fb923c,color:#e2e8f0
  style Client fill:#0f172a,stroke:#38bdf8,color:#e2e8f0
        </pre>
      </div>
    </div>

    <div class="card">
      <h2>系统信息</h2>
      <div class="sys-info-grid" id="sysInfoGrid">
        <div class="sys-info-item"><div class="sys-info-label">Go 版本</div><div class="sys-info-value" id="siGoVer">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">平台</div><div class="sys-info-value" id="siPlatform">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">CPU 核心</div><div class="sys-info-value" id="siCPU">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">编译器</div><div class="sys-info-value" id="siCompiler">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">运行时间</div><div class="sys-info-value" id="siUptime">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">Goroutines</div><div class="sys-info-value" id="siGoroutines">-</div></div>
      </div>

      <h2 style="margin-top:24px">内存分布</h2>
      <div id="memoryBars" style="margin-top:8px"></div>

      <h2 style="margin-top:24px">数据源状态</h2>
      <div class="sys-info-grid" id="dsInfoGrid">
        <div class="sys-info-item"><div class="sys-info-label">活跃源</div><div class="sys-info-value" id="dsActive">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">降级状态</div><div class="sys-info-value" id="dsState">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">云端失败</div><div class="sys-info-value" id="dsFail">-</div></div>
        <div class="sys-info-item"><div class="sys-info-label">可回切</div><div class="sys-info-value" id="dsSwitch">-</div></div>
      </div>
    </div>
  </div>

  <!-- Goroutine 趋势 + 内存趋势 -->
  <div class="grid grid-2" style="margin-bottom:20px">
    <div class="card">
      <h2>Goroutine 趋势 (最近 60 次采样)</h2>
      <div class="chart-container">
        <canvas id="goroutineChart"></canvas>
      </div>
    </div>
    <div class="card">
      <h2>堆内存趋势 (最近 60 次采样)</h2>
      <div class="chart-container">
        <canvas id="memoryChart"></canvas>
      </div>
    </div>
  </div>

  <!-- Goroutine 栈详情 -->
  <div class="card full-width">
    <h2>Goroutine 栈详情</h2>
    <input type="text" id="goroutineFilter" placeholder="🔍 搜索 goroutine（输入关键字过滤）">
    <pre class="goroutine-stack" id="goroutineStacks">加载中...</pre>
  </div>
</div>

<script>
// Mermaid 初始化
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
    fontSize: '13px'
  }
});

// 简易 Canvas 折线图
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
    const c = this.canvas;
    const ctx = this.ctx;
    const dpr = window.devicePixelRatio || 1;
    const rect = c.getBoundingClientRect();
    c.width = rect.width * dpr;
    c.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    const w = rect.width, h = rect.height;

    ctx.clearRect(0, 0, w, h);

    if (this.data.length < 2) return;
    const max = Math.max(...this.data) * 1.2 || 1;
    const min = 0;
    const stepX = w / (this.maxPoints - 1);

    // 网格线
    ctx.strokeStyle = '#1e293b';
    ctx.lineWidth = 1;
    for (let i = 0; i < 4; i++) {
      const y = h * (i / 4);
      ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
    }

    // 数据线
    ctx.strokeStyle = this.color;
    ctx.lineWidth = 2;
    ctx.beginPath();
    for (let i = 0; i < this.data.length; i++) {
      const x = i * stepX;
      const y = h - ((this.data[i] - min) / (max - min)) * h;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.stroke();

    // 填充
    ctx.lineTo((this.data.length - 1) * stepX, h);
    ctx.lineTo(0, h);
    ctx.closePath();
    ctx.fillStyle = this.color + '20';
    ctx.fill();

    // 当前值标签
    const last = this.data[this.data.length - 1];
    ctx.fillStyle = '#e2e8f0';
    ctx.font = '12px sans-serif';
    ctx.fillText(this.label + ': ' + last.toFixed(1), 8, 16);
    ctx.fillText('max: ' + max.toFixed(1), 8, 30);
  }
}

const goroutineChart = new MiniChart('goroutineChart', '#38bdf8', 'goroutines');
const memoryChart = new MiniChart('memoryChart', '#22c55e', 'heap MB');

let goroutineStacksRaw = '';

function updateFilter() {
  const filter = document.getElementById('goroutineFilter').value.toLowerCase();
  const el = document.getElementById('goroutineStacks');
  if (!filter) { el.textContent = goroutineStacksRaw; return; }
  const blocks = goroutineStacksRaw.split('\n\n');
  const filtered = blocks.filter(b => b.toLowerCase().includes(filter));
  el.textContent = filtered.length ? filtered.join('\n\n') : '没有匹配的 goroutine';
}
document.getElementById('goroutineFilter').addEventListener('input', updateFilter);

async function fetchJSON(url) {
  try { const r = await fetch(url); return await r.json(); } catch(e) { return null; }
}

async function refresh() {
  // Goroutine + 内存详情
  const gr = await fetchJSON('/api/v1/infra/goroutines');
  if (gr && gr.data) {
    const d = gr.data;
    document.getElementById('metricGoroutines').textContent = d.num_goroutine;
    goroutineStacksRaw = d.stacks;
    updateFilter();

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

      // 内存条
      const bars = [
        { label: 'Heap Alloc', value: m.heap_alloc_mb, color: '#38bdf8' },
        { label: 'Heap Idle', value: m.heap_idle_mb, color: '#334155' },
        { label: 'Stack', value: m.stack_inuse_mb, color: '#22c55e' },
        { label: 'Sys Total', value: m.sys_mb, color: '#a78bfa' },
      ];
      const maxMem = Math.max(...bars.map(b => b.value)) || 1;
      document.getElementById('memoryBars').innerHTML = bars.map(b =>
        '<div style="margin-bottom:6px">' +
        '<div style="font-size:11px;color:#94a3b8;margin-bottom:2px">' + b.label + ': ' + b.value.toFixed(1) + ' MB</div>' +
        '<div class="progress-bar"><div class="progress-fill" style="width:' + (b.value/maxMem*100).toFixed(0) + '%;background:' + b.color + '"></div></div></div>'
      ).join('');
    }
  }

  // 系统信息
  const si = await fetchJSON('/api/v1/infra/system');
  if (si && si.data) {
    const d = si.data;
    document.getElementById('siGoVer').textContent = d.go_version;
    document.getElementById('siPlatform').textContent = d.goos + '/' + d.goarch;
    document.getElementById('siCPU').textContent = d.num_cpu;
    document.getElementById('siCompiler').textContent = d.compiler;
    document.getElementById('siUptime').textContent = d.uptime;
    document.getElementById('siGoroutines').textContent = d.num_goroutine;
    document.getElementById('headerUptime').textContent = d.uptime;
  }

  // 数据源状态
  const ds = await fetchJSON('/api/v1/infra/datasource');
  if (ds && ds.data) {
    const d = ds.data;
    document.getElementById('dsActive').textContent = d.active || '-';
    document.getElementById('dsState').textContent = d.state || '-';
    document.getElementById('dsFail').textContent = d.cloud_fail_count ?? '-';
    document.getElementById('dsSwitch').textContent = d.can_switch_back ? '是' : '否';
  }
}

// 每 2 秒刷新
setInterval(refresh, 2000);
refresh();
</script>
</body>
</html>`
