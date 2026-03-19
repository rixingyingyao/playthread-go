package models

// Program 节目/素材（对齐 C# SlaProgram / PlayClip）
type Program struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	FilePath string  `json:"file_path"`
	Duration int     `json:"duration"`  // 总时长(ms)
	InPoint  int     `json:"in_point"`  // 入点(ms)
	OutPoint int     `json:"out_point"` // 出点(ms)
	Volume   float64 `json:"volume"`    // 音量(0.0-1.0)

	FadeIn  int      `json:"fade_in"`   // 淡入(ms)
	FadeOut int      `json:"fade_out"`  // 淡出(ms)
	FadeMode FadeMode `json:"fade_mode"` // 淡变模式

	IsEncrypt bool `json:"is_encrypt"` // 是否加密
	SignalID  int  `json:"signal_id"`  // 信号源ID，0=文件播放
	Type      ProgramType `json:"type"` // 素材类型, 17=歌曲预告

	// 串词参数（link_damping：串词时主播出音量压低量）
	LinkDamping float64 `json:"link_damping"` // 串词压低量(dB)
	LinkFadeIn  int     `json:"link_fadein"`  // 串词淡入(ms)
	LinkFadeOut int     `json:"link_fadeout"` // 串词淡出(ms)

	// 歌曲预告（type=17）合并片段
	Clips []ProgramClip `json:"clips,omitempty"`

	// 垫乐分类（垫乐素材使用）
	CategoryID   int    `json:"category_id,omitempty"`   // 分类ID
	CategoryName string `json:"category_name,omitempty"` // 分类名
	ProgramID    int    `json:"program_id,omitempty"`    // 素材ID（用于垫乐去重）
	PlayUrl      string `json:"play_url,omitempty"`      // 远程播放URL（文件不存在时降级使用）

	// 运行时标注（Flatten 时设置，不参与 JSON 序列化）
	BlockIndex    int      `json:"-"` // 所属 TimeBlock 索引
	BlockTaskType TaskType `json:"-"` // 所属 TimeBlock 任务类型
}

// EffectiveDuration 返回有效播放时长（考虑入点和出点）
func (p *Program) EffectiveDuration() int {
	if p.OutPoint > 0 && p.InPoint >= 0 {
		d := p.OutPoint - p.InPoint
		if d > 0 {
			return d
		}
	}
	return p.Duration
}

// IsSongPreview 判断是否为歌曲预告类型（type=17，含多个合并片段）
func (p *Program) IsSongPreview() bool {
	return p.Type == ProgramSongPreview && len(p.Clips) > 0
}

// IsSignalSource 判断是否为信号源（直播/转播/采集）
func (p *Program) IsSignalSource() bool {
	return p.SignalID > 0
}

// ProgramClip 歌曲预告的子片段（type=17 时使用）
type ProgramClip struct {
	FilePath string `json:"file_path"`
	InPoint  int    `json:"in_point"`  // 入点(ms)
	OutPoint int    `json:"out_point"` // 出点(ms)
	Duration int    `json:"duration"`  // 片段时长(ms)
}

// PlaybackSnapshot 播出状态快照（用于插播返回、冷启动恢复）
type PlaybackSnapshot struct {
	ProgramIndex int     `json:"program_index"` // FlatList 中的索引
	ProgramID    string  `json:"program_id"`
	PositionMs   int     `json:"position_ms"`   // 播放位置(ms)，插播时已减去 CutReturn 补偿
	Status       Status  `json:"status"`
	SignalID     int     `json:"signal_id"`
	Volume       float64 `json:"volume"`
	IsCutReturn  bool    `json:"is_cut_return"` // 是否由插播返回产生
}
