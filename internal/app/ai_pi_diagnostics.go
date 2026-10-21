package app

import (
	"net/http"
	"strconv"
	"strings"
)

// handleAccessPiRuns 使用已有 diagnostics 能力与 viewer scope，正文只在鉴权后回放。
func (s *Server) handleAccessPiRuns(w http.ResponseWriter, r *http.Request) {
	payload, err := s.verifyAccessTokenBearer(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !payload.hasCapability(accessTokenCapabilityAIDiagnosticsRead) {
		writeError(w, errForbidden("access token missing ai.diagnostics.read capability"))
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/access/ai/runs"))
	if len(parts) > 1 {
		writeError(w, errNotFound("not found"))
		return
	}
	if len(parts) == 1 {
		id, err := parseID(parts[0])
		if err != nil {
			writeError(w, err)
			return
		}
		run, err := s.getPiRun(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		session, err := getAIHistorySession(r.Context(), s.db, run.SessionID, payload.Scope.ViewerKey)
		if err != nil {
			writeError(w, err)
			return
		}
		turns, err := s.piSessionTranscript(r.Context(), session)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, turn := range turns {
			if turn.RunID == id {
				writeJSON(w, http.StatusOK, map[string]any{"run": run, "session": session, "transcript": piTranscriptDiagnostics([]piTranscriptTurn{turn})[0]})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"run": run, "session": session, "transcript": nil})
		return
	}
	query := `SELECT ` + piRunColumns + ` FROM ai_runs r JOIN ai_sessions s ON s.id=r.session_id LEFT JOIN ai_feedback f ON f.run_id=r.id WHERE 1=1`
	args := []any{}
	if payload.Scope.ViewerKey != "" {
		query += ` AND s.viewer_key=?`
		args = append(args, payload.Scope.ViewerKey)
	}
	for _, filter := range []struct{ param, column string }{{"status", "r.status"}, {"session_id", "r.session_id"}} {
		if value := r.URL.Query().Get(filter.param); value != "" {
			query += " AND " + filter.column + "=?"
			args = append(args, value)
		}
	}
	if q := r.URL.Query().Get("q"); q != "" {
		query += ` AND r.question LIKE ?`
		args = append(args, "%"+q+"%")
	}
	if rating := r.URL.Query().Get("rating"); rating != "" {
		if rating != "1" && rating != "-1" {
			writeError(w, errBadRequest("rating 必须为 1 或 -1"))
			return
		}
		query += ` AND f.rating=?`
		args = append(args, rating)
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		id, err := parseID(cursor)
		if err != nil {
			writeError(w, errBadRequest("无效的分页游标"))
			return
		}
		query += ` AND r.id<?`
		args = append(args, id)
	}
	limit := cleanLimit(r.URL.Query().Get("limit"), 50, 200)
	query += ` ORDER BY r.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	items := []piRunRow{}
	for rows.Next() {
		run, err := scanPiRun(rows)
		if err != nil {
			writeError(w, err)
			return
		}
		// 列表不携带 stderr；详情按能力读取完整尾部。
		run.StderrTail = ""
		items = append(items, run)
	}
	if err := rows.Err(); err != nil {
		writeError(w, err)
		return
	}
	cursor := ""
	if len(items) > limit {
		items = items[:limit]
		cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": cursor})
}
