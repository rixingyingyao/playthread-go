package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rixingyingyao/playthread-go/core"
	"github.com/rixingyingyao/playthread-go/models"
)

// Response 统一 API 响应格式
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// 写入失败时 header 已发，无法再变更状态码，仅记录日志
		_ = err // 底层 io.Writer 失败通常是连接已断
	}
}

func writeOK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: data})
}

func writeErr(w http.ResponseWriter, httpStatus int, msg string) {
	writeJSON(w, httpStatus, Response{Code: httpStatus, Message: msg})
}

func decodeBody(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("request body is empty")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// --- 查询类 ---

// StatusResponse 播出状态快照
type StatusResponse struct {
	Status       string           `json:"status"`
	Program      *models.Program  `json:"program,omitempty"`
	Position     int              `json:"position"`
	PlaylistLen  int              `json:"playlist_len"`
	IsCutPlaying bool             `json:"is_cut_playing"`
	Suspended    bool             `json:"suspended"`
}

// handleGetStatus GET /api/v1/status
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	pl := s.pt.Playlist()
	plLen := 0
	if pl != nil {
		plLen = pl.Len()
	}
	writeOK(w, StatusResponse{
		Status:       s.pt.Status().String(),
		Program:      s.pt.CurrentProgram(),
		Position:     s.pt.CurrentPosition(),
		PlaylistLen:  plLen,
		IsCutPlaying: s.pt.IsCutPlaying(),
		Suspended:    s.pt.IsSuspended(),
	})
}

// handleGetProgress GET /api/v1/progress
func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	prog := s.pt.GetProgress()
	if prog == nil {
		writeOK(w, map[string]interface{}{
			"playing": false,
		})
		return
	}
	writeOK(w, prog)
}

// handleGetPlaylist GET /api/v1/playlist
func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	pl := s.pt.Playlist()
	if pl == nil {
		writeOK(w, map[string]interface{}{"playlist": nil})
		return
	}
	writeOK(w, pl)
}

// --- 控制类 ---

// handlePlay POST /api/v1/control/play
func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	if err := s.pt.ChangeStatus(models.StatusAuto, "API play"); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeOK(w, nil)
}

// handlePause POST /api/v1/control/pause
func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	s.pt.Suspend()
	writeOK(w, nil)
}

// handleStop POST /api/v1/control/stop
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := s.pt.ChangeStatus(models.StatusStopped, "API stop"); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleNext POST /api/v1/control/next
func (s *Server) handleNext(w http.ResponseWriter, r *http.Request) {
	ok := s.pt.Next()
	writeOK(w, map[string]bool{"advanced": ok})
}

// JumpRequest POST /api/v1/control/jump 请求体
type JumpRequest struct {
	Position int `json:"position"`
}

// handleJump POST /api/v1/control/jump
func (s *Server) handleJump(w http.ResponseWriter, r *http.Request) {
	var req JumpRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	ok := s.pt.JumpTo(req.Position)
	if !ok {
		writeErr(w, http.StatusBadRequest, "跳转失败：位置超出范围或播表为空")
		return
	}
	writeOK(w, nil)
}

// ChangeStatusRequest POST /api/v1/control/status 请求体
type ChangeStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// handleChangeStatus POST /api/v1/control/status
func (s *Server) handleChangeStatus(w http.ResponseWriter, r *http.Request) {
	var req ChangeStatusRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	target, err := parseStatus(req.Status)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	reason := req.Reason
	if reason == "" {
		reason = "API status change"
	}
	if err := s.pt.ChangeStatus(target, reason); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeOK(w, nil)
}

// --- 插播类 ---

// IntercutStartRequest POST /api/v1/intercut/start 请求体
type IntercutStartRequest struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"` // "timed" | "emergency"
	Programs  []models.Program  `json:"programs"`
	FadeOutMs int               `json:"fade_out_ms"`
}

// handleIntercutStart POST /api/v1/intercut/start
func (s *Server) handleIntercutStart(w http.ResponseWriter, r *http.Request) {
	var req IntercutStartRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if strings.EqualFold(req.Type, "emergency") {
		signalID := 0
		signalName := "API 紧急插播"
		if len(req.Programs) > 0 {
			signalID = req.Programs[0].SignalID
			signalName = req.Programs[0].Name
		}
		if err := s.pt.EmrgCutStart(signalID, signalName); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeOK(w, nil)
		return
	}

	programs := make([]*models.Program, len(req.Programs))
	for i := range req.Programs {
		programs[i] = &req.Programs[i]
	}
	evt := core.IntercutEvent{
		ID:       req.ID,
		Type:     models.IntercutTimed,
		DelayMs:  req.FadeOutMs,
		Programs: programs,
	}
	select {
	case s.pt.EventBus().IntercutArrive <- evt:
		writeOK(w, nil)
	case <-time.After(5 * time.Second):
		writeErr(w, http.StatusServiceUnavailable, "插播事件投递超时")
	}
}

// handleIntercutStop POST /api/v1/intercut/stop
func (s *Server) handleIntercutStop(w http.ResponseWriter, r *http.Request) {
	if s.pt.Status() == models.StatusEmergency {
		if err := s.pt.EmrgCutStop(); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeOK(w, nil)
		return
	}
	writeOK(w, map[string]string{"note": "非紧急插播由播完自动返回"})
}

// --- 播表管理 ---

// handleLoadPlaylist POST /api/v1/playlist/load
func (s *Server) handleLoadPlaylist(w http.ResponseWriter, r *http.Request) {
	var pl models.Playlist
	if err := decodeBody(r, &pl); err != nil {
		writeErr(w, http.StatusBadRequest, "播表格式错误: "+err.Error())
		return
	}
	s.pt.SetPlaylist(&pl)
	writeOK(w, map[string]interface{}{
		"id":       pl.ID,
		"programs": pl.Len(),
	})
}

// --- 垫乐控制 ---

// handleBlankStart POST /api/v1/control/blank/start
func (s *Server) handleBlankStart(w http.ResponseWriter, r *http.Request) {
	s.pt.StartBlank()
	writeOK(w, nil)
}

// handleBlankStop POST /api/v1/control/blank/stop
func (s *Server) handleBlankStop(w http.ResponseWriter, r *http.Request) {
	s.pt.StopBlank()
	writeOK(w, nil)
}

// --- 通道保持 ---

// DelayStartRequest POST /api/v1/control/delay/start 请求体
type DelayStartRequest struct {
	SignalID    int    `json:"signal_id"`
	SignalName  string `json:"signal_name"`
	DurationMs  int    `json:"duration_ms"`
	ProgramName string `json:"program_name"`
	IsAIDelay   bool   `json:"is_ai_delay"`
}

// handleDelayStart POST /api/v1/control/delay/start
func (s *Server) handleDelayStart(w http.ResponseWriter, r *http.Request) {
	var req DelayStartRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	data := &core.ChannelHoldData{
		ReturnTime:  time.Now().Add(time.Duration(req.DurationMs) * time.Millisecond),
		DurationMs:  req.DurationMs,
		SignalID:    req.SignalID,
		SignalName:  req.SignalName,
		ProgramName: req.ProgramName,
		IsAIDelay:   req.IsAIDelay,
	}
	if err := s.pt.DelayStart(data); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleDelayStop POST /api/v1/control/delay/stop
func (s *Server) handleDelayStop(w http.ResponseWriter, r *http.Request) {
	if err := s.pt.DelayCancelManual(); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeOK(w, nil)
}

// --- 工具函数 ---

func parseStatus(s string) (models.Status, error) {
	switch strings.ToLower(s) {
	case "stopped":
		return models.StatusStopped, nil
	case "auto":
		return models.StatusAuto, nil
	case "manual":
		return models.StatusManual, nil
	case "live":
		return models.StatusLive, nil
	case "redifdelay", "delay":
		return models.StatusRedifDelay, nil
	case "emergency":
		return models.StatusEmergency, nil
	default:
		return 0, fmt.Errorf("未知状态: %s（可选: stopped/auto/manual/live/redifdelay/emergency）", s)
	}
}
