# Playthread-Go 前端对接文档

> 本文档面向前端开发人员，覆盖所有 HTTP API、WebSocket 实时推送、数据模型定义。  
> 基础地址：`http://<host>:18800`（端口可配置）

---

## 目录

- [通用约定](#通用约定)
- [认证方式](#认证方式)
- [API 端点](#api-端点)
  - [状态查询](#1-状态查询)
  - [播出进度](#2-播出进度)
  - [获取播表](#3-获取播表)
  - [开始播出](#4-开始播出-play)
  - [暂停播出](#5-暂停播出-pause)
  - [停止播出](#6-停止播出-stop)
  - [播放下一条](#7-播放下一条-next)
  - [跳转到指定位置](#8-跳转到指定位置-jump)
  - [切换播出状态](#9-切换播出状态)
  - [垫片控制](#10-垫片控制)
  - [通道保持（延时）](#11-通道保持延时播出)
  - [插播控制](#12-插播控制)
  - [录音控制](#13-录音控制)
  - [加载播表](#14-加载播表)
- [WebSocket 实时推送](#websocket-实时推送)
  - [连接方式](#连接方式)
  - [事件列表](#事件列表)
  - [事件数据结构详解](#事件数据结构详解)
- [数据模型](#数据模型)
  - [Program 节目](#program-节目)
  - [Playlist 播表](#playlist-播表)
  - [TimeBlock 时间块](#timeblock-时间块)
  - [枚举值](#枚举值)
- [错误处理](#错误处理)

---

## 通用约定

### 请求格式

- `Content-Type: application/json; charset=utf-8`
- 所有 POST 请求体为 JSON 对象
- **不允许传入未知字段**（服务端会返回 400 错误）

### 响应格式

所有 API 返回统一格式：

```json
{
  "code": 0,
  "message": "ok",
  "data": { ... }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | int | 0=成功，其他值=HTTP 状态码 |
| `message` | string | 描述信息 |
| `data` | object/null | 业务数据（无数据时省略该字段） |

### 错误响应示例

```json
{
  "code": 400,
  "message": "参数错误: json: unknown field \"xxx\""
}
```

```json
{
  "code": 409,
  "message": "非法状态迁移 Stopped → Emergency: 路径不存在"
}
```

---

## 认证方式

### Token 认证

如果服务端配置了 `api_token`，所有业务 API 需要在请求头携带：

```
Authorization: Bearer <your-token>
```

**例外**：以下端点不需要认证（仅允许本机访问）：
- `GET /dashboard`
- `GET /api/v1/infra/*`

**WebSocket** 连接可通过 URL 参数传递 Token：
```
ws://localhost:18800/ws/playback?token=<your-token>
```
> 注意：`token` 参数仅用于 WebSocket 握手，普通 HTTP 请求不支持此方式。

如果未配置 `api_token`（值为空字符串），则不启用认证，所有请求直接放行。

---

## API 端点

### 1. 状态查询

**`GET /api/v1/status`**

获取当前播出系统的整体状态快照。

**响应 data：**

```json
{
  "status": "Auto",
  "program": {
    "id": "p001",
    "name": "新闻联播",
    "file_path": "/media/news.mp3",
    "duration": 180000,
    "volume": 0.8
  },
  "position": 5,
  "playlist_len": 30,
  "is_cut_playing": false,
  "suspended": false
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 当前状态：`Stopped`/`Auto`/`Manual`/`Live`/`RedifDelay`/`Emergency` |
| `program` | object \| null | 当前正在播的节目（未播出时为 null） |
| `position` | int | 当前播表位置索引（0 起始） |
| `playlist_len` | int | 播表总条数 |
| `is_cut_playing` | bool | 是否正在插播 |
| `suspended` | bool | 是否已暂停（pause 状态） |

---

### 2. 播出进度

**`GET /api/v1/progress`**

获取当前正在播出的节目的实时进度。

**正在播出时的响应 data：**

```json
{
  "program_id": "p001",
  "position_ms": 15000,
  "duration_ms": 180000,
  "progress": 0.0833
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `program_id` | string | 节目 ID |
| `position_ms` | int | 当前播放位置（毫秒） |
| `duration_ms` | int | 总时长（毫秒） |
| `progress` | float | 播放进度（0.0 - 1.0） |

**未播出时的响应 data：**

```json
{
  "playing": false
}
```

---

### 3. 获取播表

**`GET /api/v1/playlist`**

获取当前加载的完整播表。

**响应 data：**

```json
{
  "id": "pl-20260320",
  "date": "2026-03-20T00:00:00Z",
  "version": 3,
  "blocks": [
    {
      "id": "blk001",
      "name": "早间播出",
      "start_time": "06:00:00",
      "end_time": "09:00:00",
      "task_type": 0,
      "programs": [ ... ]
    }
  ]
}
```

**未加载播表时的响应 data：**

```json
{
  "playlist": null
}
```

---

### 4. 开始播出 (Play)

**`POST /api/v1/control/play`**

将系统切换到自动播出状态，如果之前是暂停状态则同时恢复播出。

**请求体：** 无（空 body 或 `{}`）

**失败场景：**
- 当前状态不允许切换到 Auto（如从 Emergency 不能直接 Play）→ 409

---

### 5. 暂停播出 (Pause)

**`POST /api/v1/control/pause`**

暂停当前播出（音频淡出），不改变播出状态。

**请求体：** 无

**效果：**
- 当前音频淡出暂停
- `status` 不变（仍为 Auto），但 `suspended` 变为 `true`
- 调用 Play 恢复

---

### 6. 停止播出 (Stop)

**`POST /api/v1/control/stop`**

停止所有播出，系统进入 Stopped 状态。

**请求体：** 无

**失败场景：**
- 当前状态不允许切换到 Stopped → 409

---

### 7. 播放下一条 (Next)

**`POST /api/v1/control/next`**

强制跳到播表的下一条节目。

**请求体：** 无

**响应 data：**

```json
{
  "advanced": true
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `advanced` | bool | 是否成功跳转（播表到末尾时为 false） |

---

### 8. 跳转到指定位置 (Jump)

**`POST /api/v1/control/jump`**

跳转到播表的指定位置（索引）。

**请求体：**

```json
{
  "position": 10
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `position` | int | 是 | 目标位置索引（0 起始），范围 0 ~ playlist_len-1 |

**失败场景：**
- 位置超出范围或播表为空 → 400

---

### 9. 切换播出状态

**`POST /api/v1/control/status`**

切换播出状态（更灵活的控制，覆盖所有合法状态迁移）。

**请求体：**

```json
{
  "status": "manual",
  "reason": "用户手动切换"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `status` | string | 是 | 目标状态（见下表） |
| `reason` | string | 否 | 切换原因（日志记录用），默认 "API status change" |

**status 可选值：**

| 值 | 含义 |
|----|------|
| `stopped` | 停止 |
| `auto` | 自动播出 |
| `manual` | 手动播出 |
| `live` | 直播 |
| `delay` 或 `redifdelay` | 延时播出 |
| `emergency` | 紧急播出 |

**失败场景：**
- 非法状态迁移路径 → 409（如 Stopped → Emergency）
- 未知状态值 → 400

---

### 10. 垫片控制

#### 启动垫片

**`POST /api/v1/control/blank/start`**

手动启动垫片播出。

**请求体：** 无

#### 停止垫片

**`POST /api/v1/control/blank/stop`**

停止垫片播出。

**请求体：** 无

---

### 11. 通道保持（延时播出）

#### 开始通道保持

**`POST /api/v1/control/delay/start`**

进入延时播出模式，保持当前通道不中断，设定超时时长。

**请求体：**

```json
{
  "signal_id": 1,
  "signal_name": "直播间A",
  "duration_ms": 30000,
  "program_name": "早间新闻",
  "is_ai_delay": false
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `signal_id` | int | 是 | 信号源 ID |
| `signal_name` | string | 是 | 信号源名称 |
| `duration_ms` | int | 是 | 保持时长（毫秒） |
| `program_name` | string | 否 | 当前节目名称 |
| `is_ai_delay` | bool | 否 | 是否为 AI 延时（默认 false） |

**失败场景：**
- 当前状态不允许切换到延时 → 409

#### 结束通道保持

**`POST /api/v1/control/delay/stop`**

手动结束通道保持，恢复自动播出。

**请求体：** 无

---

### 12. 插播控制

#### 开始插播

**`POST /api/v1/intercut/start`**

启动一次插播，系统会保存当前播出快照，切换到插播节目。

**请求体：**

```json
{
  "id": "cut-001",
  "type": "timed",
  "programs": [
    {
      "id": "ad001",
      "name": "广告1",
      "file_path": "/media/ad1.mp3",
      "duration": 30000,
      "volume": 0.9
    },
    {
      "id": "ad002",
      "name": "广告2",
      "file_path": "/media/ad2.mp3",
      "duration": 15000,
      "volume": 0.9
    }
  ],
  "fade_out_ms": 500
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | string | 是 | 插播唯一标识 |
| `type` | string | 是 | `"timed"`=定时插播，`"emergency"`=紧急插播 |
| `programs` | array | 是 | 插播节目列表（Program 对象数组） |
| `fade_out_ms` | int | 否 | 淡出时长（毫秒） |

**紧急插播特殊行为：**
- 立即中断当前播出
- 状态变为 Emergency
- 需要调用 intercut/stop 手动结束

**定时插播特殊行为：**
- 插播节目按顺序播完后自动恢复原播出位置

#### 结束插播

**`POST /api/v1/intercut/stop`**

结束当前插播（主要用于紧急插播的手动结束）。

**请求体：** 无

**响应（非紧急插播时）：**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "note": "非紧急插播由播完自动返回"
  }
}
```

---

### 13. 录音控制

#### 开始录音

**`POST /api/v1/record/start`**

开始录制系统音频到 MP3 文件。

**请求体：**

```json
{
  "filename": "D:/recordings/2026-03-20.mp3",
  "device": 0
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `filename` | string | 是 | 输出文件路径（目录不存在会自动创建） |
| `device` | int | 否 | 录音设备，0 或 -1 为默认设备 |

**注意事项：**
- 每 3600 秒（1 小时）自动生成新文件（文件名自动加序号后缀）
- 编码格式：MP3 256kbps / 48kHz / 双声道
- 需要系统安装 `libmp3lame.dll`

**失败场景：**
- filename 为空 → 400
- 音频服务不可用 → 503
- LAME 库未安装 → 500

#### 停止录音

**`POST /api/v1/record/stop`**

停止录音并关闭文件。

**请求体：** 无

#### 暂停录音

**`POST /api/v1/record/pause`**

暂停录音（不关闭文件，可恢复）。恢复录音请调用 `record/start`（传相同 filename）。

**请求体：** 无

#### 查询录音状态

**`GET /api/v1/record/status`**

**响应 data：**

```json
{
  "status": 1,
  "duration_sec": 125.5
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | int | 0=未录制，1=录制中，2=暂停 |
| `duration_sec` | float | 已录制总时长（秒） |

---

### 14. 加载播表

**`POST /api/v1/playlist/load`**

加载一个新的播表。加载后系统会将时间块展开为扁平节目列表。

**请求体：**

```json
{
  "id": "pl-20260320",
  "date": "2026-03-20T00:00:00Z",
  "version": 3,
  "blocks": [
    {
      "id": "blk001",
      "name": "早间播出",
      "start_time": "06:00:00",
      "end_time": "09:00:00",
      "task_type": 0,
      "eq_name": "标准",
      "programs": [
        {
          "id": "p001",
          "name": "新闻联播",
          "file_path": "/media/news.mp3",
          "duration": 180000,
          "in_point": 0,
          "out_point": 180000,
          "volume": 1.0,
          "fade_in": 200,
          "fade_out": 200,
          "fade_mode": 0,
          "signal_id": 0,
          "type": 0
        }
      ],
      "intercuts": [
        {
          "id": "icut001",
          "start_time": "07:30:00",
          "programs": [
            {
              "id": "ad001",
              "name": "广告",
              "file_path": "/media/ad.mp3",
              "duration": 30000,
              "volume": 0.9
            }
          ],
          "fade_out_ms": 500
        }
      ]
    }
  ]
}
```

**响应 data：**

```json
{
  "id": "pl-20260320",
  "programs": 30
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 播表 ID |
| `programs` | int | 展开后的总节目数 |

---

## WebSocket 实时推送

### 连接方式

```
ws://<host>:18800/ws/playback
```

如果启用了 Token 认证：
```
ws://<host>:18800/ws/playback?token=<your-token>
```

**连接参数：**
- 心跳间隔：30 秒（服务端发送 Ping，客户端需回复 Pong）
- 超时时间：60 秒无 Pong 回复则断开
- 写超时：10 秒

**前端示例：**

```javascript
const ws = new WebSocket('ws://localhost:18800/ws/playback?token=xxx');

ws.onmessage = function(event) {
  const msg = JSON.parse(event.data);
  console.log('事件类型:', msg.type);
  console.log('事件数据:', msg.data);
  console.log('时间戳:', msg.time);
};
```

### 消息格式

所有推送消息为 JSON，统一格式：

```json
{
  "type": "事件类型",
  "data": { ... },
  "time": "2026-03-20T08:30:15.123456789+08:00"
}
```

### 事件列表

| 事件类型 | 推送频率 | 说明 |
|---------|---------|------|
| `status_changed` | 状态变化时 | 播出状态迁移 |
| `play_started` | 开始播出时 | 新节目开始播放 |
| `play_finished` | 播出结束时 | 节目播放完毕 |
| `progress` | 每秒 1 次 | 播出进度更新 |
| `countdown` | 每秒 1 次 | 倒计时 |
| `level` | 约每 200ms | 音频电平 |
| `blank_started` | 垫片开始时 | 垫片音乐启动 |
| `blank_stopped` | 垫片结束时 | 垫片音乐停止 |
| `intercut_started` | 插播开始时 | 开始插播 |
| `intercut_ended` | 插播结束时 | 插播完毕/恢复 |
| `fix_time_arrived` | 定时到达时 | 定时任务触发 |
| `countdown` | 每秒 1 次 | 当前节目倒计时 |
| `channel_empty` | 播表播完时 | 通道空闲 |
| `device_lost` | 设备断开时 | 音频设备丢失 |
| `device_restored` | 设备恢复时 | 音频设备重连 |
| `error` | 异常时 | 系统错误 |
| `heartbeat` | 每分钟 | 系统心跳 |
| `next_clip_1` | 播出新条时 | 下一条节目预告 |
| `next_clip_2` | 播出新条时 | 下下条节目预告 |
| `record_progress` | 约每 200ms | 录音进度/电平 |

---

### 事件数据结构详解

#### `status_changed` — 状态变更

```json
{
  "type": "status_changed",
  "data": {
    "old_status": 0,
    "new_status": 1,
    "path": 1,
    "reason": "API play"
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `old_status` | int | 旧状态编号（见枚举表） |
| `new_status` | int | 新状态编号 |
| `path` | int | 迁移路径编号（见枚举表） |
| `reason` | string | 迁移原因 |

---

#### `play_started` — 开始播出

```json
{
  "type": "play_started",
  "data": {
    "program": {
      "id": "p001",
      "name": "新闻联播",
      "file_path": "/media/news.mp3",
      "duration": 180000
    },
    "length_ms": 180000,
    "channel": 0
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `program` | object | 完整的 Program 对象 |
| `length_ms` | int | 播出有效时长（毫秒） |
| `channel` | int | 通道编号（0=主播出） |

---

#### `play_finished` — 播出结束

结构同 `play_started`。

---

#### `progress` — 播出进度

```json
{
  "type": "progress",
  "data": {
    "program_id": "p001",
    "position_ms": 15000,
    "duration_ms": 180000,
    "progress": 0.0833
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `program_id` | string | 节目 ID |
| `position_ms` | int | 当前位置（毫秒） |
| `duration_ms` | int | 总时长（毫秒） |
| `progress` | float | 0.0 ~ 1.0 |

---

#### `countdown` — 倒计时

```json
{
  "type": "countdown",
  "data": {
    "value": 45,
    "total": 180
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `value` | int | 剩余秒数 |
| `total` | int | 总秒数 |

---

#### `level` — 音频电平

```json
{
  "type": "level",
  "data": {
    "channel": 0,
    "left": 0.75,
    "right": 0.68
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `channel` | int | 通道编号 |
| `left` | float | 左声道音量（0.0 ~ 1.0） |
| `right` | float | 右声道音量（0.0 ~ 1.0） |

---

#### `blank_started` — 垫片开始

播出空白时自动填充垫片音乐。

```json
{
  "type": "blank_started",
  "data": {
    "program": {
      "id": "pad001",
      "name": "轻音乐01",
      "file_path": "/padding/light01.mp3",
      "duration": 240000
    },
    "length_ms": 240000,
    "channel": 8
  },
  "time": "..."
}
```

---

#### `blank_stopped` — 垫片停止

```json
{
  "type": "blank_stopped",
  "data": {},
  "time": "..."
}
```

---

#### `intercut_started` — 插播开始

```json
{
  "type": "intercut_started",
  "data": {
    "id": "cut-001",
    "type": 0,
    "depth": 1,
    "interrupted_prog": "新闻联播"
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 插播 ID |
| `type` | int | 0=定时插播，1=紧急插播 |
| `depth` | int | 嵌套深度（最大 3） |
| `interrupted_prog` | string | 被中断的节目名 |

---

#### `intercut_ended` — 插播结束

```json
{
  "type": "intercut_ended",
  "data": {
    "id": "cut-001",
    "return_prog": "新闻联播",
    "return_pos_ms": 95000
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 插播 ID |
| `return_prog` | string | 恢复到的节目名 |
| `return_pos_ms` | int | 恢复到的位置（毫秒） |

---

#### `fix_time_arrived` — 定时到达

```json
{
  "type": "fix_time_arrived",
  "data": {
    "block_id": "blk002",
    "task_type": 0,
    "delay_ms": 500
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `block_id` | string | 时间块 ID |
| `task_type` | int | 0=硬定时，1=软定时 |
| `delay_ms` | int | 淡出延时（毫秒） |

---

#### `channel_empty` — 通道空闲

```json
{
  "type": "channel_empty",
  "data": {
    "channel": 0,
    "countdown_sec": 0
  },
  "time": "..."
}
```

---

#### `next_clip_1` / `next_clip_2` — 节目预告

```json
{
  "type": "next_clip_1",
  "data": {
    "id": "p002",
    "name": "天气预报",
    "duration": 60000
  },
  "time": "..."
}
```

空预告（无下一条）：
```json
{
  "type": "next_clip_1",
  "data": {},
  "time": "..."
}
```

---

#### `record_progress` — 录音进度

```json
{
  "type": "record_progress",
  "data": {
    "duration": 125.5,
    "status": 1,
    "level": {
      "PeakL": 0.85,
      "PeakR": 0.82,
      "RmsL": 0.45,
      "RmsR": 0.43,
      "DbL": -6.9,
      "DbR": -7.3
    }
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `duration` | float | 已录制总时长（秒） |
| `status` | int | 0=未录制，1=录制中，2=暂停 |
| `level.PeakL` | float | 左声道峰值（0.0 ~ 1.0） |
| `level.PeakR` | float | 右声道峰值 |
| `level.RmsL` | float | 左声道 RMS（均方根，反映平均音量） |
| `level.RmsR` | float | 右声道 RMS |
| `level.DbL` | float | 左声道分贝值（负数，-∞ ~ 0） |
| `level.DbR` | float | 右声道分贝值 |

---

#### `device_lost` — 设备断开

```json
{
  "type": "device_lost",
  "data": {
    "device_index": 0,
    "device_name": "扬声器 (Realtek Audio)"
  },
  "time": "..."
}
```

---

#### `device_restored` — 设备恢复

结构同 `device_lost`。

---

#### `error` — 系统错误

```json
{
  "type": "error",
  "data": {
    "message": "文件加载失败: /media/missing.mp3",
    "auto_close": true,
    "hold_sec": 5
  },
  "time": "..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `message` | string | 错误信息 |
| `auto_close` | bool | 前端是否可自动关闭提示 |
| `hold_sec` | int | 建议显示时长（秒） |

---

#### `heartbeat` — 系统心跳

```json
{
  "type": "heartbeat",
  "data": {},
  "time": "..."
}
```

---

## 数据模型

### Program 节目

一个节目/素材的完整字段定义：

```json
{
  "id": "p001",
  "name": "新闻联播",
  "file_path": "/media/news.mp3",
  "duration": 180000,
  "in_point": 0,
  "out_point": 180000,
  "volume": 1.0,
  "fade_in": 200,
  "fade_out": 200,
  "fade_mode": 0,
  "is_encrypt": false,
  "signal_id": 0,
  "type": 0,
  "link_damping": 0.0,
  "link_fadein": 0,
  "link_fadeout": 0,
  "clips": [],
  "category_id": 0,
  "category_name": "",
  "program_id": 0,
  "play_url": ""
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 唯一标识 |
| `name` | string | 名称 |
| `file_path` | string | 音频文件路径 |
| `duration` | int | 总时长（毫秒） |
| `in_point` | int | 入点（从该位置开始播） |
| `out_point` | int | 出点（播到该位置结束） |
| `volume` | float | 音量 0.0 ~ 1.0 |
| `fade_in` | int | 淡入时长（毫秒） |
| `fade_out` | int | 淡出时长（毫秒） |
| `fade_mode` | int | 0=淡入淡出，1=仅淡入，2=仅淡出，3=无 |
| `is_encrypt` | bool | 是否为加密文件 |
| `signal_id` | int | 信号源 ID（0=文件播放，>0=外部信号） |
| `type` | int | 素材类型（0=普通，17=歌曲预告） |
| `link_damping` | float | 串词压低量（dB） |
| `link_fadein` | int | 串词淡入（毫秒） |
| `link_fadeout` | int | 串词淡出（毫秒） |
| `clips` | array | 歌曲预告子片段（type=17 时使用） |
| `category_id` | int | 垫片分类 ID |
| `category_name` | string | 垫片分类名 |
| `program_id` | int | 素材数字 ID（垫片去重用） |
| `play_url` | string | 远程播放 URL（降级用） |

### Playlist 播表

```json
{
  "id": "pl-20260320",
  "date": "2026-03-20T00:00:00Z",
  "version": 3,
  "blocks": [ TimeBlock... ]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 播表唯一 ID |
| `date` | string (ISO 8601) | 播出日期 |
| `version` | int | 版本号（用于变更检测） |
| `blocks` | array | 时间块列表 |

### TimeBlock 时间块

```json
{
  "id": "blk001",
  "name": "早间播出",
  "start_time": "06:00:00",
  "end_time": "09:00:00",
  "task_type": 0,
  "eq_name": "标准",
  "programs": [ Program... ],
  "intercuts": [ IntercutSection... ]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 时间块 ID |
| `name` | string | 时间块名称 |
| `start_time` | string | 开始时间 `HH:MM:SS` |
| `end_time` | string | 结束时间 `HH:MM:SS` |
| `task_type` | int | 定时类型：0=硬定时，1=软定时 |
| `eq_name` | string | 均衡器预设名（可选） |
| `programs` | array | 该时间块内的节目列表 |
| `intercuts` | array | 插播栏目（可选） |

### IntercutSection 插播栏目

```json
{
  "id": "icut001",
  "start_time": "07:30:00",
  "programs": [ Program... ],
  "fade_out_ms": 500
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 插播栏目 ID |
| `start_time` | string | 插播触发时间 `HH:MM:SS` |
| `programs` | array | 插播节目列表 |
| `fade_out_ms` | int | 淡出时长（毫秒） |

---

## 枚举值

### Status 播出状态

| 值 | 名称 | API 字符串 | 说明 |
|----|------|-----------|------|
| 0 | Stopped | `stopped` | 停止 |
| 1 | Auto | `auto` | 自动播出 |
| 2 | Manual | `manual` | 手动播出 |
| 3 | Live | `live` | 直播 |
| 4 | RedifDelay | `delay` / `redifdelay` | 延时播出 |
| 5 | Emergency | `emergency` | 紧急播出 |

> 注意：`GET /api/v1/status` 返回的是 **字符串**（如 `"Auto"`），WebSocket 事件 `status_changed` 中的 `old_status`/`new_status` 是 **数字**。

### TaskType 定时类型

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | Hard | 硬定时（到时强制切播） |
| 1 | Soft | 软定时（等当前播完再切） |
| 2 | Intercut | 插播 |

### FadeMode 淡变模式

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | FadeIn_Out | 淡入 + 淡出 |
| 1 | FadeIn | 仅淡入 |
| 2 | FadeOut | 仅淡出 |
| 3 | None | 无淡变 |

### IntercutType 插播类型

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | Timed | 定时插播 |
| 1 | Emergency | 紧急插播 |

### ChannelName 通道编号

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | MainOut | 主播出 |
| 1-7 | Preview1~7 | 预听通道 |
| 8 | FillBlank | 垫片通道 |
| 9 | TellTime | 报时通道 |
| 10 | Effect | 音效通道 |
| 11 | TempList | 临时播表通道 |

### PathType 状态迁移路径

| 值 | 名称 | 说明 |
|----|------|------|
| 1 | Stop2Auto | 停止→自动 |
| 2 | Auto2Stop | 自动→停止 |
| 3 | Auto2Manual | 自动→手动 |
| 4 | Manual2Auto | 手动→自动 |
| 5 | Auto2Emerg | 自动→紧急 |
| 6 | Emerg2Auto | 紧急→自动 |
| 7 | Auto2Delay | 自动→延时 |
| 8 | Delay2Auto | 延时→自动 |
| 9 | Stop2Manual | 停止→手动 |
| 10 | Manual2Stop | 手动→停止 |
| 11 | Auto2Live | 自动→直播 |
| 12 | Live2Auto | 直播→自动 |
| 13 | Live2Manual | 直播→手动 |
| 14 | Manual2Live | 手动→直播 |
| 15 | Stop2Live | 停止→直播 |
| 17 | Live2Delay | 直播→延时 |
| 18 | Delay2Live | 延时→直播 |
| 19 | Stop2Delay | 停止→延时 |
| 20 | Manual2Delay | 手动→延时 |
| 21 | Delay2Manual | 延时→手动 |

---

## 错误处理

### HTTP 状态码说明

| 状态码 | 含义 | 典型场景 |
|--------|------|---------|
| 200 | 成功 | 正常响应 |
| 400 | 请求参数错误 | JSON 格式错、缺少必填字段、位置越界 |
| 403 | 禁止访问 | 自升级端点未配置 token |
| 409 | 冲突 | 非法状态迁移（如 Stopped → Emergency） |
| 500 | 内部错误 | 录音启动失败、暂停恢复失败 |
| 503 | 服务不可用 | 音频服务未启动、管理器未初始化 |

### 状态迁移错误

当请求的状态变更路径不合法时，返回 409：

```json
{
  "code": 409,
  "message": "非法状态迁移 Stopped → Emergency: 路径不存在"
}
```

合法路径请参考 [PathType 枚举表](#pathtype-状态迁移路径)。

### 前端建议

1. **WebSocket 重连**：断开后建议 3 秒后自动重连
2. **Token 过期**：收到 401 时刷新 Token 或提示重新登录
3. **进度显示**：用 `progress` 事件的 `progress` 字段（0-1）驱动进度条
4. **倒计时显示**：用 `countdown` 事件的 `value` 字段直接显示秒数
5. **电平表**：用 `level` 事件的 `left`/`right` 驱动音量柱
6. **状态徽章**：监听 `status_changed` 事件实时更新状态显示
7. **节目预告**：用 `next_clip_1` 和 `next_clip_2` 显示接下来 1~2 条节目
