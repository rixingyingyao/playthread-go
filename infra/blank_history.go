package infra

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// BlankHistoryItem 垫乐播放记录条目（对齐 C# BlankHistoryItem）
type BlankHistoryItem struct {
	ClipID         int       `xml:"ClipId" json:"clip_id"`
	RealPlayTime   time.Time `xml:"RealPlayTime" json:"real_play_time"`
	RealPlayLength int       `xml:"RealPlayLength" json:"real_play_length"` // 实际播放时长(ms)
}

// BlankHistory 垫乐播放历史管理器（对齐 C# SlaBlankPlayInfo 单例）
type BlankHistory struct {
	XMLName  xml.Name           `xml:"SlaBlankPlayInfo"`
	mu       sync.Mutex         `xml:"-"`
	Items    []BlankHistoryItem  `xml:"BlankPlayHistory>BlankHistoryItem" json:"items"`
	dir      string             `xml:"-"`
	keepDays int                `xml:"-"`
}

// NewBlankHistory 创建垫乐历史管理器
func NewBlankHistory(dir string, keepDays int) *BlankHistory {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("创建垫乐历史目录失败")
	}
	return &BlankHistory{
		dir:      dir,
		keepDays: keepDays,
	}
}

// Add 添加播放记录并持久化
func (bh *BlankHistory) Add(clipID int, playTime time.Time, playLength int) {
	bh.mu.Lock()
	defer bh.mu.Unlock()

	bh.Items = append(bh.Items, BlankHistoryItem{
		ClipID:         clipID,
		RealPlayTime:   playTime,
		RealPlayLength: playLength,
	})

	if err := bh.saveLocked(); err != nil {
		log.Error().Err(err).Msg("保存垫乐历史失败")
	}
}

// GetLastPlayTime 获取指定曲目最后播放时间。
// 未找到时返回零值时间和 false。
func (bh *BlankHistory) GetLastPlayTime(clipID int) (time.Time, bool) {
	bh.mu.Lock()
	defer bh.mu.Unlock()

	for i := len(bh.Items) - 1; i >= 0; i-- {
		if bh.Items[i].ClipID == clipID {
			return bh.Items[i].RealPlayTime, true
		}
	}
	return time.Time{}, false
}

// HasPlayed 判断指定曲目是否在历史中存在记录
func (bh *BlankHistory) HasPlayed(clipID int) bool {
	_, found := bh.GetLastPlayTime(clipID)
	return found
}

// Load 从 XML 文件加载历史并清理过期记录
func (bh *BlankHistory) Load() error {
	bh.mu.Lock()
	defer bh.mu.Unlock()

	path := bh.filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			bh.Items = nil
			return nil
		}
		return fmt.Errorf("读取垫乐历史失败: %w", err)
	}

	var loaded BlankHistory
	if err := xml.Unmarshal(data, &loaded); err != nil {
		log.Warn().Err(err).Msg("解析垫乐历史 XML 失败，重置为空")
		bh.Items = nil
		return nil
	}

	bh.Items = loaded.Items
	bh.deleteExpiredLocked()

	log.Info().Int("count", len(bh.Items)).Msg("垫乐历史加载完成")
	return nil
}

// Save 持久化到 XML 文件
func (bh *BlankHistory) Save() error {
	bh.mu.Lock()
	defer bh.mu.Unlock()
	return bh.saveLocked()
}

func (bh *BlankHistory) saveLocked() error {
	data, err := xml.MarshalIndent(bh, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化垫乐历史失败: %w", err)
	}

	path := bh.filePath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("写入垫乐历史失败: %w", err)
	}
	// Windows 上 os.Rename 在目标已存在时会失败，先删除目标
	_ = os.Remove(path)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("重命名垫乐历史文件失败: %w", err)
	}
	return nil
}

// deleteExpiredLocked 清理超过 keepDays 天的历史记录（需在锁内调用）
func (bh *BlankHistory) deleteExpiredLocked() {
	cutoff := time.Now().AddDate(0, 0, -bh.keepDays)
	n := 0
	for _, item := range bh.Items {
		if item.RealPlayTime.After(cutoff) {
			bh.Items[n] = item
			n++
		}
	}
	removed := len(bh.Items) - n
	bh.Items = bh.Items[:n]
	if removed > 0 {
		log.Debug().Int("removed", removed).Int("keep_days", bh.keepDays).Msg("清理过期垫乐历史")
	}
}

// Cleanup 手动触发过期清理并保存
func (bh *BlankHistory) Cleanup() error {
	bh.mu.Lock()
	defer bh.mu.Unlock()
	bh.deleteExpiredLocked()
	return bh.saveLocked()
}

// Count 返回当前历史记录数量
func (bh *BlankHistory) Count() int {
	bh.mu.Lock()
	defer bh.mu.Unlock()
	return len(bh.Items)
}

func (bh *BlankHistory) filePath() string {
	return filepath.Join(bh.dir, "BlankPadding.his")
}
