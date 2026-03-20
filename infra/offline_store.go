package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// OfflineEntryType 离线条目类型
type OfflineEntryType string

const (
	OfflineHeartbeat OfflineEntryType = "heartbeat" // 心跳
	OfflineLog       OfflineEntryType = "log"       // 关键日志
	OfflineStatus    OfflineEntryType = "status"    // 状态变更
	OfflineEvent     OfflineEntryType = "event"     // 播出事件
)

// OfflineEntry 离线暂存条目
type OfflineEntry struct {
	ID        int64            `json:"id"`        // 自增序号
	Type      OfflineEntryType `json:"type"`
	Timestamp time.Time        `json:"timestamp"` // 产生时间
	Payload   json.RawMessage  `json:"payload"`   // 具体数据
}

// OfflineStoreConfig 离线存储配置
type OfflineStoreConfig struct {
	Dir           string        `yaml:"dir"`             // 存储目录
	MaxEntries    int           `yaml:"max_entries"`     // 最大条目数，默认 10000
	FlushInterval time.Duration `yaml:"flush_interval"` // 落盘间隔，默认 10s
	MaxAgeDays    int           `yaml:"max_age_days"`    // 最大保留天数，默认 7
}

// DefaultOfflineStoreConfig 默认配置
func DefaultOfflineStoreConfig() OfflineStoreConfig {
	return OfflineStoreConfig{
		Dir:           "data/offline",
		MaxEntries:    10000,
		FlushInterval: 10 * time.Second,
		MaxAgeDays:    7,
	}
}

// OfflineStore 断网暂存管理器
// 断网期间将心跳、日志、状态变更暂存到本地文件。
// 联网后按时间顺序补传。
type OfflineStore struct {
	mu       sync.Mutex
	cfg      OfflineStoreConfig
	entries  []OfflineEntry
	nextID   int64
	dirty    bool
	filePath string
}

// NewOfflineStore 创建离线存储管理器
func NewOfflineStore(cfg OfflineStoreConfig) *OfflineStore {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", cfg.Dir).Msg("创建离线存储目录失败")
	}

	store := &OfflineStore{
		cfg:      cfg,
		filePath: filepath.Join(cfg.Dir, "offline_queue.json"),
	}

	// 启动时加载已有条目
	if err := store.load(); err != nil {
		log.Warn().Err(err).Msg("加载离线条目失败")
	}

	return store
}

// Add 添加一条离线记录
func (os_ *OfflineStore) Add(entryType OfflineEntryType, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 payload 失败: %w", err)
	}

	os_.mu.Lock()
	defer os_.mu.Unlock()

	// 容量保护
	if len(os_.entries) >= os_.cfg.MaxEntries {
		// 丢弃最旧的 10%
		dropCount := os_.cfg.MaxEntries / 10
		if dropCount < 1 {
			dropCount = 1
		}
		log.Warn().
			Int("drop_count", dropCount).
			Int("total", len(os_.entries)).
			Msg("离线条目满，丢弃最旧记录")
		os_.entries = os_.entries[dropCount:]
	}

	os_.nextID++
	os_.entries = append(os_.entries, OfflineEntry{
		ID:        os_.nextID,
		Type:      entryType,
		Timestamp: time.Now(),
		Payload:   data,
	})
	os_.dirty = true

	return nil
}

// Len 返回当前暂存条目数
func (os_ *OfflineStore) Len() int {
	os_.mu.Lock()
	defer os_.mu.Unlock()
	return len(os_.entries)
}

// Drain 取出所有条目（按时间顺序）并清空存储
// 用于联网后补传
func (os_ *OfflineStore) Drain() []OfflineEntry {
	os_.mu.Lock()
	defer os_.mu.Unlock()

	if len(os_.entries) == 0 {
		return nil
	}

	result := make([]OfflineEntry, len(os_.entries))
	copy(result, os_.entries)

	// 按时间顺序排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	os_.entries = os_.entries[:0]
	os_.dirty = true

	log.Info().Int("count", len(result)).Msg("Drain 离线条目")
	return result
}

// Peek 查看最旧的 N 条记录（不删除）
func (os_ *OfflineStore) Peek(n int) []OfflineEntry {
	os_.mu.Lock()
	defer os_.mu.Unlock()

	if n > len(os_.entries) {
		n = len(os_.entries)
	}
	result := make([]OfflineEntry, n)
	copy(result, os_.entries[:n])
	return result
}

// RemoveByID 移除指定 ID 的条目（补传成功后逐条确认）
func (os_ *OfflineStore) RemoveByID(ids []int64) {
	if len(ids) == 0 {
		return
	}

	idSet := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	os_.mu.Lock()
	defer os_.mu.Unlock()

	filtered := os_.entries[:0]
	for _, e := range os_.entries {
		if _, found := idSet[e.ID]; !found {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) != len(os_.entries) {
		os_.dirty = true
	}
	os_.entries = filtered
}

// RunFlush 定期落盘循环（阻塞，需在 goroutine 中调用）
func (os_ *OfflineStore) RunFlush(ctx context.Context) {
	ticker := time.NewTicker(os_.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := os_.Flush(); err != nil {
				log.Warn().Err(err).Msg("离线条目落盘失败")
			}
		case <-ctx.Done():
			// 退出前最后落盘一次
			if err := os_.Flush(); err != nil {
				log.Warn().Err(err).Msg("退出时离线条目落盘失败")
			}
			return
		}
	}
}

// Flush 将内存中的条目落盘
func (os_ *OfflineStore) Flush() error {
	os_.mu.Lock()
	defer os_.mu.Unlock()

	if !os_.dirty {
		return nil
	}

	data, err := json.MarshalIndent(os_.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化离线条目失败: %w", err)
	}

	tmpPath := os_.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	_ = os.Remove(os_.filePath)
	if err := os.Rename(tmpPath, os_.filePath); err != nil {
		return fmt.Errorf("重命名离线文件失败: %w", err)
	}

	os_.dirty = false
	return nil
}

// load 从文件加载离线条目
func (os_ *OfflineStore) load() error {
	data, err := os.ReadFile(os_.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取离线文件失败: %w", err)
	}

	var entries []OfflineEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("解析离线文件失败: %w", err)
	}

	// 清理过期条目
	cutoff := time.Now().AddDate(0, 0, -os_.cfg.MaxAgeDays)
	filtered := entries[:0]
	for _, e := range entries {
		if e.Timestamp.After(cutoff) {
			filtered = append(filtered, e)
		}
	}

	os_.entries = filtered
	if len(filtered) > 0 {
		os_.nextID = filtered[len(filtered)-1].ID
	}

	log.Info().
		Int("loaded", len(filtered)).
		Int("expired", len(entries)-len(filtered)).
		Msg("加载离线条目")

	return nil
}

// Cleanup 清理过期条目
func (os_ *OfflineStore) Cleanup() {
	os_.mu.Lock()
	defer os_.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -os_.cfg.MaxAgeDays)
	filtered := os_.entries[:0]
	removed := 0
	for _, e := range os_.entries {
		if e.Timestamp.After(cutoff) {
			filtered = append(filtered, e)
		} else {
			removed++
		}
	}

	if removed > 0 {
		os_.entries = filtered
		os_.dirty = true
		log.Info().Int("removed", removed).Msg("清理过期离线条目")
	}
}

// UploadFunc 补传回调函数类型
// 返回 nil 表示补传成功，否则停止后续补传
type UploadFunc func(entry OfflineEntry) error

// ReplayTo 按时间顺序将暂存条目补传到目标
// 每条成功后从存储中移除。遇到失败则停止（保证顺序）。
// 返回成功补传的条目数。
func (os_ *OfflineStore) ReplayTo(ctx context.Context, fn UploadFunc) (int, error) {
	entries := os_.Peek(os_.Len())
	if len(entries) == 0 {
		return 0, nil
	}

	// 按时间排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	var successIDs []int64
	for _, e := range entries {
		select {
		case <-ctx.Done():
			os_.RemoveByID(successIDs)
			return len(successIDs), ctx.Err()
		default:
		}

		if err := fn(e); err != nil {
			log.Warn().
				Err(err).
				Int64("id", e.ID).
				Str("type", string(e.Type)).
				Msg("补传失败，停止后续补传")
			os_.RemoveByID(successIDs)
			return len(successIDs), err
		}
		successIDs = append(successIDs, e.ID)
	}

	os_.RemoveByID(successIDs)
	if err := os_.Flush(); err != nil {
		log.Warn().Err(err).Msg("补传后落盘失败")
	}

	log.Info().Int("uploaded", len(successIDs)).Msg("离线条目补传完成")
	return len(successIDs), nil
}
