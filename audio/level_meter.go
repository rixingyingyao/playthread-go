package audio

import (
	"context"
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
	engine   *BassEngine
	interval time.Duration
	dataCh   chan LevelData
	channels []string // 监控的通道列表
}

// NewLevelMeter 创建电平表
func NewLevelMeter(engine *BassEngine, interval time.Duration) *LevelMeter {
	return &LevelMeter{
		engine:   engine,
		interval: interval,
		dataCh:   make(chan LevelData, 128),
		channels: []string{"MainOut"}, // 默认只监控主播出
	}
}

// SetChannels 设置需要监控的通道列表
func (m *LevelMeter) SetChannels(channels []string) {
	m.channels = channels
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
	for _, ch := range m.channels {
		left, right, err := m.engine.GetLevel(ch)
		if err != nil {
			continue
		}
		if left == 0 && right == 0 {
			continue // 跳过静音通道
		}
		select {
		case m.dataCh <- LevelData{Channel: ch, Left: left, Right: right}:
		default:
			// 消费者跟不上，丢弃旧数据
		}
	}
}
