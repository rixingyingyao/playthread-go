package infra

import (
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/lumberjack.v2"
)

// InitLogger 初始化全局 zerolog 日志（文件轮转 + 控制台输出）。
// 返回 io.Closer 用于优雅退出时关闭日志文件。
func InitLogger(cfg LogConfig) io.Closer {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", cfg.Dir).Msg("创建日志目录失败")
	}

	fileWriter := &lumberjack.Logger{
		Filename:   filepath.Join(cfg.Dir, "playthread.log"),
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05.000"}

	multi := zerolog.MultiLevelWriter(consoleWriter, fileWriter)
	log.Logger = zerolog.New(multi).With().Timestamp().Caller().Logger()

	level := parseLevel(cfg.Level)
	zerolog.SetGlobalLevel(level)

	log.Info().Str("level", level.String()).Str("dir", cfg.Dir).Msg("日志系统初始化完成")

	return fileWriter
}

func parseLevel(s string) zerolog.Level {
	switch s {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
