package infra

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// Config 全局配置
type Config struct {
	Playback PlaybackConfig `yaml:"playback"`
	Audio    AudioConfig    `yaml:"audio"`
	Padding  PaddingConfig  `yaml:"padding"`
	Server   ServerConfig   `yaml:"server"`
	Monitor  MonitorConfig  `yaml:"monitor"`
	Log      LogConfig      `yaml:"log"`
	DB       DBConfig       `yaml:"db"`
}

// PlaybackConfig 播出相关配置
type PlaybackConfig struct {
	PollingIntervalMs  int `yaml:"polling_interval_ms"`  // 轮询间隔(ms)，默认 20
	TaskExpireMs       int `yaml:"task_expire_ms"`       // 任务过期时间(ms)，默认 3000
	HardFixAdvanceMs   int `yaml:"hard_fix_advance_ms"`  // 硬定时提前量(ms)，默认 50
	SoftFixAdvanceMs   int `yaml:"soft_fix_advance_ms"`  // 软定时提前量(ms)，默认 0
	CueRetryMax        int `yaml:"cue_retry_max"`        // 预卷重试次数，默认 3
	PlayRetryMax       int `yaml:"play_retry_max"`       // 播放重试次数，默认 1
	SnapshotIntervalS  int `yaml:"snapshot_interval_s"`  // 快照写入间隔(秒)，默认 5
	CutReturnMs        int `yaml:"cut_return_ms"`        // 插播返回补偿时间(ms)，默认 500
	SignalSwitchDelayMs int `yaml:"signal_switch_delay_ms"` // 信号切换硬件延迟(ms)，默认 500
}

// AudioConfig 音频相关配置
type AudioConfig struct {
	SampleRate  int `yaml:"sample_rate"`   // 采样率，默认 44100
	DeviceID    int `yaml:"device_id"`     // 设备ID，-1=默认
	FadeInMs    int `yaml:"fade_in_ms"`    // 默认淡入(ms)
	FadeOutMs   int `yaml:"fade_out_ms"`   // 默认淡出(ms)
	FadeCrossMs int `yaml:"fade_cross_ms"` // 交叉淡变(ms)
}

// PaddingConfig 垫乐相关配置
type PaddingConfig struct {
	EnableAI       bool   `yaml:"enable_ai"`         // 是否启用 AI 选曲
	AIThresholdMs  int    `yaml:"ai_threshold_ms"`   // AI 选曲阈值(ms)
	HistoryKeepDays int   `yaml:"history_keep_days"` // 历史保留天数
	Directory      string `yaml:"directory"`         // 垫乐文件目录
}

// ServerConfig HTTP/WebSocket/UDP 配置
type ServerConfig struct {
	Host           string   `yaml:"host"`             // 监听地址
	Port           int      `yaml:"port"`             // HTTP 端口
	WSPath         string   `yaml:"ws_path"`          // WebSocket 路径
	UDPAddr        string   `yaml:"udp_addr"`         // UDP 监听地址
	APIToken       string   `yaml:"api_token"`        // API 认证令牌，为空则不启用认证
	AllowedOrigins []string `yaml:"allowed_origins"`  // CORS 允许的源，为空则全放行
	RateLimitRPS   int      `yaml:"rate_limit_rps"`   // 每 IP 每秒最大请求数，0=不限
}

// MonitorConfig 监控配置
type MonitorConfig struct {
	MemoryCheckIntervalS  int `yaml:"memory_check_interval_s"`  // 内存检查间隔(秒)
	MemoryWarnThresholdMB int `yaml:"memory_warn_threshold_mb"` // 内存告警阈值(MB)
	HeartbeatIntervalS    int `yaml:"heartbeat_interval_s"`     // 心跳间隔(秒)
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string `yaml:"level"`       // 日志级别: debug/info/warn/error
	Dir        string `yaml:"dir"`         // 日志目录
	MaxSizeMB  int    `yaml:"max_size_mb"` // 单文件最大(MB)
	MaxBackups int    `yaml:"max_backups"` // 最大备份数
	MaxAgeDays int    `yaml:"max_age_days"` // 最大保留天数
	Compress   bool   `yaml:"compress"`    // 是否压缩
}

// DBConfig 数据库配置
type DBConfig struct {
	Path string `yaml:"path"` // SQLite 数据库路径
}

// LoadConfig 加载 YAML 配置文件
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置校验失败: %w", err)
	}
	return cfg, nil
}

// DefaultConfig 返回带默认值的配置
func DefaultConfig() *Config {
	return &Config{
		Playback: PlaybackConfig{
			PollingIntervalMs:   20,
			TaskExpireMs:        3000,
			HardFixAdvanceMs:    50,
			SoftFixAdvanceMs:    0,
			CueRetryMax:         3,
			PlayRetryMax:        1,
			SnapshotIntervalS:   5,
			CutReturnMs:         500,
			SignalSwitchDelayMs: 500,
		},
		Audio: AudioConfig{
			SampleRate:  44100,
			DeviceID:    -1,
			FadeInMs:    500,
			FadeOutMs:   500,
			FadeCrossMs: 300,
		},
		Padding: PaddingConfig{
			EnableAI:        true,
			AIThresholdMs:   60000,
			HistoryKeepDays: 2,
			Directory:       "padding",
		},
		Server: ServerConfig{
			Host:    "0.0.0.0",
			Port:    18800,
			WSPath:  "/ws/playback",
			UDPAddr: "127.0.0.1:18820",
		},
		Monitor: MonitorConfig{
			MemoryCheckIntervalS:  3600,
			MemoryWarnThresholdMB: 500,
			HeartbeatIntervalS:    5,
		},
		Log: LogConfig{
			Level:      "info",
			Dir:        "logs",
			MaxSizeMB:  50,
			MaxBackups: 30,
			MaxAgeDays: 30,
			Compress:   true,
		},
		DB: DBConfig{
			Path: "data/playthread.db",
		},
	}
}

// Validate 校验配置值的合理性
func (c *Config) Validate() error {
	if c.Playback.PollingIntervalMs < 5 || c.Playback.PollingIntervalMs > 100 {
		return fmt.Errorf("playback.polling_interval_ms 必须在 5-100 范围内，当前: %d", c.Playback.PollingIntervalMs)
	}
	if c.Playback.TaskExpireMs < 1000 {
		return fmt.Errorf("playback.task_expire_ms 不能小于 1000，当前: %d", c.Playback.TaskExpireMs)
	}
	if c.Playback.CueRetryMax < 1 || c.Playback.CueRetryMax > 10 {
		return fmt.Errorf("playback.cue_retry_max 必须在 1-10 范围内，当前: %d", c.Playback.CueRetryMax)
	}
	if c.Audio.SampleRate != 44100 && c.Audio.SampleRate != 48000 {
		return fmt.Errorf("audio.sample_rate 仅支持 44100/48000，当前: %d", c.Audio.SampleRate)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 必须在 1-65535 范围内，当前: %d", c.Server.Port)
	}
	if c.Playback.SnapshotIntervalS < 1 {
		return fmt.Errorf("playback.snapshot_interval_s 不能小于 1，当前: %d", c.Playback.SnapshotIntervalS)
	}
	return nil
}
