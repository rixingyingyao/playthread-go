package audio

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LevelData 电平数据
type LevelData struct {
	Channel string  `json:"channel"`
	Left    float32 `json:"left"`
	Right   float32 `json:"right"`
}

// LevelMeter 音频电平表，定期采集通道电平
type LevelMeter struct {
	mu       sync.Mutex
	engine   *BassEngine
	interval time.Duration
	dataCh   chan LevelData
	channels []int // 监控的通道索引列表
}

// NewLevelMeter 创建电平表
func NewLevelMeter(engine *BassEngine, interval time.Duration) *LevelMeter {
	return &LevelMeter{
		engine:   engine,
		interval: interval,
		dataCh:   make(chan LevelData, 128),
		channels: []int{0}, // 默认只监控主播出（索引 0）
	}
}

// SetChannels 设置需要监控的通道索引列表（线程安全）
func (m *LevelMeter) SetChannels(channels []int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst := make([]int, len(channels))
	copy(dst, channels)
	m.channels = dst
}

// DataCh 返回电平数据通道（只读）
func (m *LevelMeter) DataCh() <-chan LevelData {
	return m.dataCh
}

// Run 启动电平采集循环
func (m *LevelMeter) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.sample()
		case <-ctx.Done():
			log.Debug().Msg("电平表停止")
			return
		}
	}
}

// sample 采集一次所有监控通道的电平
func (m *LevelMeter) sample() {
	m.mu.Lock()
	channels := m.channels
	m.mu.Unlock()

	for _, ch := range channels {
		left, right, err := m.engine.GetLevel(ch)
		if err != nil {
			continue
		}
		if left == 0 && right == 0 {
			continue
		}
		name := ""
		if ch >= 0 && ch < int(ChanCount) {
			name = ChannelName(ch).String()
		}
		select {
		case m.dataCh <- LevelData{Channel: name, Left: float32(left), Right: float32(right)}:
		default:
		}
	}
}
