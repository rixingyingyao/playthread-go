package infra

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

// PlayingInfo 当前播出状态快照（每 N 秒原子写入 JSON 文件）
type PlayingInfo struct {
	ProgramID    string        `json:"program_id"`
	ProgramName  string        `json:"program_name"`
	Position     int           `json:"position"`       // 播放位置(ms)
	Duration     int           `json:"duration"`        // 总时长(ms)
	SystemTime   time.Time     `json:"system_time"`     // 快照时间戳
	Status       models.Status `json:"status"`
	SignalID     int           `json:"signal_id"`
	IsCutPlaying bool          `json:"is_cut_playing"` // 是否在插播中
	PlaylistID   string        `json:"playlist_id"`
	BlockIndex   int           `json:"block_index"`    // 当前时间块索引
	ProgramIndex int           `json:"program_index"`  // FlatList 中的索引
}

// SnapshotManager 快照管理器
type SnapshotManager struct {
	filePath string
}

// NewSnapshotManager 创建快照管理器
func NewSnapshotManager(dir string) *SnapshotManager {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("创建快照目录失败")
	}
	return &SnapshotManager{
		filePath: filepath.Join(dir, "playing_info.json"),
	}
}

// Save 原子写入快照文件（先写临时文件再 rename，防止写入中断导致文件损坏）
func (sm *SnapshotManager) Save(info *PlayingInfo) error {
	info.SystemTime = time.Now()

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化快照失败: %w", err)
	}

	tmpPath := sm.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("写入临时快照失败: %w", err)
	}

	// Windows 上 os.Rename 在目标已存在时会失败，先删除目标
	_ = os.Remove(sm.filePath)
	if err := os.Rename(tmpPath, sm.filePath); err != nil {
		return fmt.Errorf("重命名快照文件失败: %w", err)
	}

	return nil
}

// Load 加载快照文件
func (sm *SnapshotManager) Load() (*PlayingInfo, error) {
	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取快照文件失败: %w", err)
	}

	var info PlayingInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("解析快照文件失败: %w", err)
	}

	return &info, nil
}

// CalcRecoveryPosition 计算冷启动恢复的播放位置。
// 根据快照时间和当前时间计算偏移量，应用入点容错。
func CalcRecoveryPosition(info *PlayingInfo) int {
	elapsed := time.Since(info.SystemTime)
	pos := info.Position + int(elapsed.Milliseconds())

	if pos > info.Duration {
		return -1
	}

	if pos < 1000 {
		pos = 0
	} else if pos < 2000 {
		pos = 1000
	}

	return pos
}

// Delete 删除快照文件
func (sm *SnapshotManager) Delete() error {
	if err := os.Remove(sm.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除快照文件失败: %w", err)
	}
	return nil
}
