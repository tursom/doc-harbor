package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type piRunRequest struct {
	Question string          `json:"question"`
	Role     string          `json:"role"`
	Scope    AIQuestionScope `json:"scope"`
}

func (s *Server) handlePiSessionSubroute(w http.ResponseWriter, r *http.Request, id int64, parts []string) bool {
	if len(parts) != 1 {
		return false
	}
	switch parts[0] {
	case "runs", "transcript", "steer", "abort":
	default:
		return false
	}
	session, err := getAISession(r.Context(), s.db, id)
	if err != nil {
		writeError(w, err)
		return true
	}
	if parts[0] == "transcript" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		turns, err := s.piSessionTranscript(r.Context(), session)
		if err != nil {
			writeError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"turns": turns})
		return true
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	if session.Engine != "pi" {
		writeError(w, errConflict("旧会话不能使用 pi 运行接口，请新建会话"))
		return true
	}
	switch parts[0] {
	case "runs":
		s.handlePiRunStream(w, r, session)
	case "abort":
		s.pi.mu.Lock()
		active := s.pi.active[id]
		s.pi.mu.Unlock()
		if active == nil {
			writeError(w, errConflict("当前会话没有运行中的问题"))
			return true
		}
		active.stop("aborted", "已停止")
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
	case "steer":
		var req struct {
			Text string `json:"text"`
		}
		if err := decodeBody(r.Body, &req); err != nil {
			writeError(w, err)
			return true
		}
		if strings.TrimSpace(req.Text) == "" {
			writeError(w, errBadRequest("补充说明不能为空"))
			return true
		}
		s.pi.mu.Lock()
		active := s.pi.active[id]
		s.pi.mu.Unlock()
		if active == nil {
			writeError(w, errConflict("当前会话没有运行中的问题"))
			return true
		}
		active.controlMu.Lock()
		defer active.controlMu.Unlock()
		active.mu.Lock()
		client, finished, stopping := active.client, active.finished || !active.sessionReady, active.stopStatus != ""
		active.mu.Unlock()
		if client == nil || finished || stopping {
			writeError(w, errConflict("问题尚未开始或正在停止，请稍后重试"))
			return true
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		// 原生 steer 只入队；prompt 在恰好 settled 的边界会启动另一次 run，
		// 因此使用 pi 1.0.0 的等价队列命令避免补充说明意外变成新提问。
		if _, err := client.Command(ctx, "steer", map[string]any{"message": req.Text}); err != nil {
			writeError(w, err)
			return true
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
	}
	return true
}

func (s *Server) handlePiRunStream(w http.ResponseWriter, r *http.Request, session AISession) {
	var req piRunRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		writeError(w, errBadRequest("问题不能为空"))
		return
	}
	if req.Role == "" {
		req.Role = "未指定"
	}
	switch req.Role {
	case "未指定", "开发", "产品", "测试":
	default:
		writeError(w, errBadRequest("role 必须是开发、产品、测试或未指定"))
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeError(w, errInternal("当前连接不支持 SSE"))
		return
	}
	if _, _, err := s.piRuntimeSpec(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	queueCtx, cancelQueue := context.WithCancel(r.Context())
	s.pi.mu.Lock()
	if s.pi.ctx.Err() != nil {
		s.pi.mu.Unlock()
		cancelQueue()
		writeError(w, errUnavailable("DocHarbor 正在关闭"))
		return
	}
	if s.pi.active[session.ID] != nil {
		s.pi.mu.Unlock()
		cancelQueue()
		writeError(w, errConflict("同一会话已有运行中的问题，请使用补充说明"))
		return
	}
	// 在会话互斥锁内重新读位置，上一轮刚保存的 JSONL 文件名不能被旧快照覆盖。
	var err error
	session, err = getAISession(r.Context(), s.db, session.ID)
	if err != nil {
		s.pi.mu.Unlock()
		cancelQueue()
		writeError(w, err)
		return
	}
	if session.ArchivedAt != "" {
		s.pi.mu.Unlock()
		cancelQueue()
		writeError(w, errConflict("已归档会话不能继续提问"))
		return
	}
	if req.Scope.RepoMode == "" && len(req.Scope.RepoIDs) == 0 && req.Scope.CurrentFile == nil {
		req.Scope = aiScopeFromJSON(session.ScopeJSON)
	}
	req.Scope = normalizeAIScope(req.Scope)
	run := piRunRow{SessionID: session.ID, Status: "queued", Question: req.Question, Role: req.Role, ScopeJSON: encodeJSON(req.Scope), CitationsJSON: "[]", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	result, err := s.db.ExecContext(r.Context(), `INSERT INTO ai_runs (session_id,status,question,role,scope_json,started_at) VALUES (?,?,?,?,?,?)`, run.SessionID, run.Status, run.Question, run.Role, run.ScopeJSON, run.StartedAt)
	if err != nil {
		s.pi.mu.Unlock()
		cancelQueue()
		writeError(w, err)
		return
	}
	run.ID, err = result.LastInsertId()
	if err != nil {
		s.pi.mu.Unlock()
		cancelQueue()
		writeError(w, err)
		return
	}
	active := &piActiveRun{cancelQueue: cancelQueue}
	s.pi.active[session.ID] = active
	s.pi.wg.Add(1)
	s.pi.mu.Unlock()
	defer cancelQueue()
	stream := make(chan piStreamEvent, 256)
	finished := make(chan struct{})
	emit := func(name string, data any) {
		// 浏览器断开后停止推送，不阻塞后台进程和数据库落盘。
		select {
		case <-r.Context().Done():
			return
		default:
		}
		select {
		case stream <- piStreamEvent{name, data}:
		case <-r.Context().Done():
		}
	}
	go func() { defer close(finished); s.executePiRun(queueCtx, active, session, run, emit) }()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	controller := http.NewResponseController(w)
	writeEvent := func(event piStreamEvent) error {
		raw, err := json.Marshal(event.Data)
		if err != nil {
			return err
		}
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, raw); err != nil {
			return err
		}
		return controller.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-stream:
			if writeEvent(event) != nil {
				return
			}
			if event.Name == "done" {
				return
			}
		case <-finished:
			// finished 与最后的 done 同时就绪时，先排空尾部事件。
			for {
				select {
				case event := <-stream:
					if writeEvent(event) != nil {
						return
					}
				default:
					return
				}
			}
		case <-ticker.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		}
	}
}

func (s *Server) piSessionTranscript(ctx context.Context, session AISession) ([]piTranscriptTurn, error) {
	if session.Engine != "pi" {
		messages, err := listAIMessages(ctx, s.db, session.ID)
		if err != nil {
			return nil, err
		}
		turns := []piTranscriptTurn{}
		for _, msg := range messages {
			if msg.Role == "user" {
				turns = append(turns, piTranscriptTurn{Question: msg.Content, Role: "未指定", Scope: aiScopeFromJSON(session.ScopeJSON), Status: "succeeded", Steps: []piTranscriptStep{}, Citations: []piCitation{}})
			} else if msg.Role == "assistant" && len(turns) > 0 {
				turns[len(turns)-1].Answer = msg.Content
				turns[len(turns)-1].Status = msg.Status
			}
		}
		return turns, nil
	}
	parsed := []piTranscriptTurn{}
	if session.PiSessionFile != "" {
		path, err := s.piSessionPath(session.PiSessionFile)
		if err != nil {
			return nil, err
		}
		parsed, err = parsePiSession(path)
		if errors.Is(err, errPiSessionVersion) {
			log.Printf("pi 会话格式未知，使用 RPC 获取历史: session_id=%d", session.ID)
			parsed, err = s.piTranscriptFallback(ctx, session, path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	runs, err := s.listPiRuns(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	turns := make([]piTranscriptTurn, 0, len(runs))
	ownedEntries := map[string]bool{}
	usedEntries := map[string]bool{}
	for _, run := range runs {
		if run.PiEntryID != "" {
			ownedEntries[run.PiEntryID] = true
		}
	}
	for _, run := range runs {
		turn := piTranscriptTurn{Steps: []piTranscriptStep{}, Citations: []piCitation{}}
		// 以持久化的 user entry ID 配对，同题重试不会把成功回答归给前一次失败。
		for _, candidate := range parsed {
			if run.PiEntryID != "" && candidate.EntryID == run.PiEntryID {
				turn = candidate
				break
			}
			// 早期 pi 构建尚未保存 entry ID。仅匹配未被其他运行明确占用的
			// 原文，并用时间窗兼容；一次候选只消费一次，不影响新格式重试。
			if run.PiEntryID == "" && candidate.EntryID != "" && !ownedEntries[candidate.EntryID] && !usedEntries[candidate.EntryID] && candidate.Question == run.Question && piTurnWithinRun(candidate, run) {
				turn = candidate
				usedEntries[candidate.EntryID] = true
				break
			}
			// 未知版本退回 get_messages 没有 entry ID，使用精确运行时间窗。
			if candidate.EntryID == "" && (candidate.Question == run.Question || candidate.Question == "") && piTurnWithinRun(candidate, run) {
				turn = candidate
				break
			}
		}
		turn.RunID, turn.Question, turn.Role, turn.Scope, turn.Status = run.ID, run.Question, run.Role, aiScopeFromJSON(run.ScopeJSON), run.Status
		turn.Model, turn.ProviderKey, turn.StartedAt, turn.FinishedAt, turn.ToolCalls, turn.ErrorMessage = run.Model, run.ProviderKey, run.StartedAt, run.FinishedAt, run.ToolCalls, run.ErrorMessage
		if start, err := time.Parse(time.RFC3339Nano, run.StartedAt); err == nil {
			end := time.Now()
			if run.FinishedAt != "" {
				if finish, err := time.Parse(time.RFC3339Nano, run.FinishedAt); err == nil {
					end = finish
				}
			}
			turn.DurationMS = max(0, end.Sub(start).Milliseconds())
		}
		turn.Usage.InputTokens, turn.Usage.OutputTokens, turn.Usage.CacheReadTokens, turn.Usage.Cost = int64(run.InputTokens), int64(run.OutputTokens), int64(run.CacheReadTokens), run.Cost
		turn.Usage.TotalTokens = turn.Usage.InputTokens + turn.Usage.OutputTokens + turn.Usage.CacheReadTokens + turn.Usage.CacheWriteTokens
		_ = json.Unmarshal([]byte(run.CitationsJSON), &turn.Citations)
		if run.Feedback != nil {
			turn.Feedback = &piTranscriptFeedback{Rating: run.Feedback.Rating, Comment: run.Feedback.Comment, CreatedAt: run.Feedback.CreatedAt}
		}
		turns = append(turns, turn)
	}
	return turns, nil
}

func piTurnWithinRun(turn piTranscriptTurn, run piRunRow) bool {
	start, err := time.Parse(time.RFC3339Nano, run.StartedAt)
	if err != nil {
		return false
	}
	end := time.Now()
	if run.FinishedAt != "" {
		end, err = time.Parse(time.RFC3339Nano, run.FinishedAt)
		if err != nil {
			return false
		}
		// 早期运行时间只保存到秒，最后一秒内的 JSONL 毫秒时间也属于该轮。
		if !strings.Contains(run.FinishedAt, ".") {
			end = end.Add(time.Second - time.Millisecond)
		}
	}
	for _, message := range turn.Messages {
		if message.Role == "user" {
			return message.Timestamp >= start.UnixMilli() && message.Timestamp <= end.UnixMilli()
		}
	}
	for _, message := range turn.Messages {
		if message.Timestamp > 0 {
			return message.Timestamp >= start.UnixMilli() && message.Timestamp <= end.UnixMilli()
		}
	}
	return false
}

func (s *Server) piTranscriptFallback(ctx context.Context, session AISession, path string) ([]piTranscriptTurn, error) {
	s.pi.mu.Lock()
	active := s.pi.active[session.ID]
	s.pi.mu.Unlock()
	if active != nil {
		active.controlMu.Lock()
		defer active.controlMu.Unlock()
		active.mu.Lock()
		client := active.client
		ready := active.sessionReady && !active.finished
		active.mu.Unlock()
		if client != nil && ready {
			response, err := client.Command(ctx, "get_messages", nil)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(response)
			return parsePiMessages(raw)
		}
		return nil, errConflict("会话正在切换或保存，请稍后重新读取历史")
	}
	_, _, err := s.piRuntimeSpec(ctx)
	if err != nil {
		return nil, err
	}
	client, err := s.pi.pool.AcquireLatest(ctx, nil)
	if err != nil {
		return nil, err
	}
	reusable := false
	defer func() {
		if !reusable {
			client.Close()
		}
		s.pi.pool.Release(client)
	}()
	if _, err := client.Command(ctx, "switch_session", map[string]any{"sessionPath": path}); err != nil {
		return nil, err
	}
	response, err := client.Command(ctx, "get_messages", nil)
	if err != nil {
		return nil, err
	}
	reusable = true
	raw, _ := json.Marshal(response)
	return parsePiMessages(raw)
}

func (s *Server) handlePiFeedback(w http.ResponseWriter, r *http.Request, idText string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := parseID(idText)
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.getPiRun(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if err := decodeBody(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	if req.Rating != 1 && req.Rating != -1 {
		writeError(w, errBadRequest("rating 必须为 1 或 -1"))
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO ai_feedback(run_id,rating,comment,created_at) VALUES(?,?,?,?) ON CONFLICT(run_id) DO UPDATE SET rating=excluded.rating,comment=excluded.comment,created_at=excluded.created_at`, id, req.Rating, req.Comment, nowString())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}
