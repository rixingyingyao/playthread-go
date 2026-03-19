package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// PlayHistoryRecord 播放历史记录
type PlayHistoryRecord struct {
	ID          int64     `json:"id"`
	ProgramID   string    `json:"program_id"`
	ProgramName string    `json:"program_name"`
	FilePath    string    `json:"file_path"`
	Channel     int       `json:"channel"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	DurationMs  int       `json:"duration_ms"`
	PlayedMs    int       `json:"played_ms"`
	Status      string    `json:"status"` // completed/interrupted/skipped
	PlaylistID  string    `json:"playlist_id"`
	BlockID     string    `json:"block_id"`
}

// PlayHistoryRepo 播放历史仓库
type PlayHistoryRepo struct {
	db *DB
}

// NewPlayHistoryRepo 创建播放历史仓库
func NewPlayHistoryRepo(d *DB) *PlayHistoryRepo {
	return &PlayHistoryRepo{db: d}
}

// Insert 插入播放历史
func (r *PlayHistoryRepo) Insert(rec *PlayHistoryRecord) (int64, error) {
	var id int64
	err := r.db.Write(func(conn *sql.DB) error {
		result, err := conn.Exec(
			`INSERT INTO play_history (program_id, program_name, file_path, channel, started_at, finished_at, duration_ms, played_ms, status, playlist_id, block_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.ProgramID, rec.ProgramName, rec.FilePath, rec.Channel,
			rec.StartedAt, rec.FinishedAt, rec.DurationMs, rec.PlayedMs,
			rec.Status, rec.PlaylistID, rec.BlockID,
		)
		if err != nil {
			return err
		}
		id, _ = result.LastInsertId()
		return nil
	})
	return id, err
}

// UpdateFinished 更新播放完成状态
func (r *PlayHistoryRepo) UpdateFinished(id int64, finishedAt time.Time, playedMs int, status string) error {
	return r.db.Write(func(conn *sql.DB) error {
		_, err := conn.Exec(
			`UPDATE play_history SET finished_at = ?, played_ms = ?, status = ? WHERE id = ?`,
			finishedAt, playedMs, status, id,
		)
		return err
	})
}

// QueryRecent 查询最近 N 条播放历史
func (r *PlayHistoryRepo) QueryRecent(limit int) ([]PlayHistoryRecord, error) {
	rows, err := r.db.Conn().Query(
		`SELECT id, program_id, program_name, file_path, channel, started_at, finished_at, duration_ms, played_ms, status, playlist_id, block_id
		 FROM play_history ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("查询播放历史失败: %w", err)
	}
	defer rows.Close()

	var records []PlayHistoryRecord
	for rows.Next() {
		var rec PlayHistoryRecord
		if err := rows.Scan(
			&rec.ID, &rec.ProgramID, &rec.ProgramName, &rec.FilePath,
			&rec.Channel, &rec.StartedAt, &rec.FinishedAt, &rec.DurationMs,
			&rec.PlayedMs, &rec.Status, &rec.PlaylistID, &rec.BlockID,
		); err != nil {
			return nil, fmt.Errorf("扫描播放历史失败: %w", err)
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// SettingsRepo 系统设置仓库
type SettingsRepo struct {
	db *DB
}

// NewSettingsRepo 创建设置仓库
func NewSettingsRepo(d *DB) *SettingsRepo {
	return &SettingsRepo{db: d}
}

// Get 获取设置值
func (r *SettingsRepo) Get(key string) (string, error) {
	var value string
	err := r.db.Conn().QueryRow(
		`SELECT value FROM settings WHERE key = ?`, key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("获取设置 %q 失败: %w", key, err)
	}
	return value, nil
}

// Set 设置值（upsert）
func (r *SettingsRepo) Set(key, value string) error {
	return r.db.Write(func(conn *sql.DB) error {
		_, err := conn.Exec(
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
			key, value,
		)
		return err
	})
}

// Delete 删除设置
func (r *SettingsRepo) Delete(key string) error {
	return r.db.Write(func(conn *sql.DB) error {
		_, err := conn.Exec(`DELETE FROM settings WHERE key = ?`, key)
		return err
	})
}

// FixTimeLogRepo 定时任务执行记录仓库
type FixTimeLogRepo struct {
	db *DB
}

// NewFixTimeLogRepo 创建定时任务日志仓库
func NewFixTimeLogRepo(d *DB) *FixTimeLogRepo {
	return &FixTimeLogRepo{db: d}
}

// Insert 记录定时任务触发
func (r *FixTimeLogRepo) Insert(blockID string, taskType int, scheduledAt, triggeredAt time.Time, deviationMs int, result string) error {
	return r.db.Write(func(conn *sql.DB) error {
		_, err := conn.Exec(
			`INSERT INTO fix_time_log (block_id, task_type, scheduled_at, triggered_at, deviation_ms, result)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			blockID, taskType, scheduledAt, triggeredAt, deviationMs, result,
		)
		return err
	})
}

// QueryDeviationStats 查询定时偏差统计（用于验收测试）
func (r *FixTimeLogRepo) QueryDeviationStats() (avg, p95, max int, err error) {
	err = r.db.Conn().QueryRow(`
		SELECT
			COALESCE(AVG(ABS(deviation_ms)), 0),
			COALESCE((SELECT ABS(deviation_ms) FROM fix_time_log ORDER BY ABS(deviation_ms) DESC LIMIT 1 OFFSET (SELECT COUNT(*) * 5 / 100 FROM fix_time_log)), 0),
			COALESCE(MAX(ABS(deviation_ms)), 0)
		FROM fix_time_log
	`).Scan(&avg, &p95, &max)
	if err != nil {
		log.Warn().Err(err).Msg("查询定时偏差统计失败")
	}
	return
}
