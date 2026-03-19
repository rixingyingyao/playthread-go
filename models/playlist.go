package models

import (
	"fmt"
	"time"
)

// Playlist 日播单
type Playlist struct {
	ID       string      `json:"id"`
	Date     time.Time   `json:"date"`
	Version  int         `json:"version"`
	Blocks   []TimeBlock `json:"blocks"`
	FlatList []*Program  `json:"-"` // 展开后的平铺素材列表（运行时生成）
}

// Flatten 将播表展开为平铺素材列表，设置 FlatList 并返回。
// 每个 Program 会被标注其所属 TimeBlock 的索引和任务类型。
func (pl *Playlist) Flatten() []*Program {
	pl.FlatList = pl.FlatList[:0]
	for bi := range pl.Blocks {
		block := &pl.Blocks[bi]
		for pi := range block.Programs {
			prog := &block.Programs[pi]
			prog.BlockIndex = bi
			prog.BlockTaskType = block.TaskType
			pl.FlatList = append(pl.FlatList, prog)
		}
	}
	return pl.FlatList
}

// FindNext 从 pos 开始查找下一条可播素材。
// 返回 nil 表示播表已到末尾。
func (pl *Playlist) FindNext(pos int) *Program {
	if pl.FlatList == nil {
		pl.Flatten()
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= len(pl.FlatList) {
		return nil
	}
	return pl.FlatList[pos]
}

// Len 返回平铺列表长度
func (pl *Playlist) Len() int {
	if pl.FlatList == nil {
		pl.Flatten()
	}
	return len(pl.FlatList)
}

// TimeBlock 时间块（对应 C# 的 TimeBlock）
type TimeBlock struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	StartTime string   `json:"start_time"` // HH:MM:SS
	EndTime   string   `json:"end_time"`
	Programs  []Program `json:"programs"`
	TaskType  TaskType `json:"task_type"`
	EQName    string   `json:"eq_name,omitempty"`
}

// ParseStartTime 解析时间块的开始时间（相对于 baseDate）
func (tb *TimeBlock) ParseStartTime(baseDate time.Time) (time.Time, error) {
	return parseHHMMSS(tb.StartTime, baseDate)
}

// ParseEndTime 解析时间块的结束时间（相对于 baseDate）
func (tb *TimeBlock) ParseEndTime(baseDate time.Time) (time.Time, error) {
	return parseHHMMSS(tb.EndTime, baseDate)
}

func parseHHMMSS(s string, baseDate time.Time) (time.Time, error) {
	var h, m, sec int
	_, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec)
	if err != nil {
		return time.Time{}, fmt.Errorf("时间格式错误 %q: %w", s, err)
	}
	return time.Date(
		baseDate.Year(), baseDate.Month(), baseDate.Day(),
		h, m, sec, 0, baseDate.Location(),
	), nil
}
