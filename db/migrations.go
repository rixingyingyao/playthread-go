package db

import (
	"database/sql"
	"fmt"

	"github.com/rs/zerolog/log"
)

// migrations 数据库迁移脚本列表，按版本号顺序追加
var migrations = []string{
	// v1: 播放历史表
	`CREATE TABLE IF NOT EXISTS play_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		program_id TEXT NOT NULL,
		program_name TEXT NOT NULL,
		file_path TEXT,
		channel INTEGER NOT NULL DEFAULT 0,
		started_at DATETIME NOT NULL,
		finished_at DATETIME,
		duration_ms INTEGER NOT NULL DEFAULT 0,
		played_ms INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'completed',
		playlist_id TEXT,
		block_id TEXT
	)`,

	// v2: 系统设置表
	`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	// v3: 播放历史索引
	`CREATE INDEX IF NOT EXISTS idx_play_history_started_at ON play_history(started_at)`,

	// v4: 定时任务执行记录
	`CREATE TABLE IF NOT EXISTS fix_time_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		block_id TEXT NOT NULL,
		task_type INTEGER NOT NULL,
		scheduled_at DATETIME NOT NULL,
		triggered_at DATETIME NOT NULL,
		deviation_ms INTEGER NOT NULL DEFAULT 0,
		result TEXT NOT NULL DEFAULT 'success'
	)`,

	// v5: 错误日志表
	`CREATE TABLE IF NOT EXISTS error_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		occurred_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		level TEXT NOT NULL,
		component TEXT NOT NULL,
		message TEXT NOT NULL,
		detail TEXT
	)`,
}

// Migrate 执行数据库迁移。
// 使用 PRAGMA user_version 跟踪已应用的迁移版本。
func Migrate(d *DB) error {
	return d.Write(func(conn *sql.DB) error {
		var version int
		if err := conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			return fmt.Errorf("读取数据库版本失败: %w", err)
		}

		if version >= len(migrations) {
			log.Debug().Int("version", version).Msg("数据库已是最新版本")
			return nil
		}

		for i := version; i < len(migrations); i++ {
			if _, err := conn.Exec(migrations[i]); err != nil {
				return fmt.Errorf("迁移 v%d 失败: %w", i+1, err)
			}
			if _, err := conn.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
				return fmt.Errorf("更新数据库版本失败: %w", err)
			}
			log.Info().Int("version", i+1).Msg("数据库迁移完成")
		}

		return nil
	})
}
