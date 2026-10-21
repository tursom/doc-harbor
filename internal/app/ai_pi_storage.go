package app

import (
	"context"
	"database/sql"
	"fmt"
)

// migratePiAI 保留旧表与旧会话，只为新的运行元数据添加存储。消息正文仍由 pi JSONL 保存。
func migratePiAI(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(ai_sessions)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, col := range []struct{ name, definition string }{
		{"engine", "TEXT NOT NULL DEFAULT 'legacy'"},
		{"pi_session_file", "TEXT NOT NULL DEFAULT ''"},
	} {
		if !columns[col.name] {
			if _, err := db.ExecContext(ctx, "ALTER TABLE ai_sessions ADD COLUMN "+col.name+" "+col.definition); err != nil {
				return err
			}
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS ai_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL,
			pi_entry_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL, question TEXT NOT NULL, role TEXT NOT NULL DEFAULT '未指定', scope_json TEXT NOT NULL DEFAULT '{}',
			provider_key TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', tool_calls INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cost REAL NOT NULL DEFAULT 0, citations_json TEXT NOT NULL DEFAULT '[]', error_message TEXT NOT NULL DEFAULT '',
			stderr_tail TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(session_id) REFERENCES ai_sessions(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_runs_session ON ai_runs(session_id, id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_runs_active_session ON ai_runs(session_id) WHERE status IN ('queued', 'running')`,
		`CREATE TABLE IF NOT EXISTS ai_feedback (
			run_id INTEGER PRIMARY KEY, rating INTEGER NOT NULL CHECK(rating IN (1, -1)), comment TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES ai_runs(id) ON DELETE CASCADE)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// 兼容已运行过早期步骤2构建的测试数据；稳定关联重试与 JSONL 的 user entry。
	var hasEntryID int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('ai_runs') WHERE name='pi_entry_id'`).Scan(&hasEntryID); err != nil {
		return err
	}
	if hasEntryID == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE ai_runs ADD COLUMN pi_entry_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

// piRunRow 仅存储运行元数据。过程与回答从当前会话的 JSONL 回放，避免两份正文不一致。
type piRunRow struct {
	PiEntryID       string         `json:"pi_entry_id,omitempty"`
	ID              int64          `json:"id"`
	SessionID       int64          `json:"session_id"`
	Status          string         `json:"status"`
	Question        string         `json:"question"`
	Role            string         `json:"role"`
	ScopeJSON       string         `json:"scope_json"`
	ProviderKey     string         `json:"provider_key"`
	Model           string         `json:"model"`
	ToolCalls       int            `json:"tool_calls"`
	InputTokens     int            `json:"input_tokens"`
	OutputTokens    int            `json:"output_tokens"`
	CacheReadTokens int            `json:"cache_read_tokens"`
	Cost            float64        `json:"cost"`
	CitationsJSON   string         `json:"citations_json"`
	ErrorMessage    string         `json:"error_message"`
	StderrTail      string         `json:"stderr_tail,omitempty"`
	StartedAt       string         `json:"started_at"`
	FinishedAt      string         `json:"finished_at"`
	Feedback        *piRunFeedback `json:"feedback,omitempty"`
}

type piRunFeedback struct {
	Rating    int    `json:"rating"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at"`
}

const piRunColumns = `r.pi_entry_id, r.id, r.session_id, r.status, r.question, r.role, r.scope_json, r.provider_key, r.model,
	r.tool_calls, r.input_tokens, r.output_tokens, r.cache_read_tokens, r.cost, r.citations_json, r.error_message,
	r.stderr_tail, r.started_at, r.finished_at, f.rating, f.comment, f.created_at`

func scanPiRun(row repoScanner) (piRunRow, error) {
	var run piRunRow
	var rating sql.NullInt64
	var comment, created sql.NullString
	err := row.Scan(&run.PiEntryID, &run.ID, &run.SessionID, &run.Status, &run.Question, &run.Role, &run.ScopeJSON, &run.ProviderKey, &run.Model,
		&run.ToolCalls, &run.InputTokens, &run.OutputTokens, &run.CacheReadTokens, &run.Cost, &run.CitationsJSON, &run.ErrorMessage,
		&run.StderrTail, &run.StartedAt, &run.FinishedAt, &rating, &comment, &created)
	if rating.Valid {
		run.Feedback = &piRunFeedback{int(rating.Int64), comment.String, created.String}
	}
	return run, err
}

func (s *Server) listPiRuns(ctx context.Context, sessionID int64) ([]piRunRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+piRunColumns+` FROM ai_runs r LEFT JOIN ai_feedback f ON f.run_id=r.id WHERE r.session_id=? ORDER BY r.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []piRunRow{}
	for rows.Next() {
		run, err := scanPiRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Server) getPiRun(ctx context.Context, id int64) (piRunRow, error) {
	run, err := scanPiRun(s.db.QueryRowContext(ctx, `SELECT `+piRunColumns+` FROM ai_runs r LEFT JOIN ai_feedback f ON f.run_id=r.id WHERE r.id=?`, id))
	if err == sql.ErrNoRows {
		return run, errNotFound("AI 运行记录不存在")
	}
	return run, err
}

func (s *Server) savePiRun(ctx context.Context, run piRunRow) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ai_runs SET status=?, provider_key=?, model=?, tool_calls=?, input_tokens=?, output_tokens=?,
		cache_read_tokens=?, cost=?, citations_json=?, error_message=?, stderr_tail=?, finished_at=? WHERE id=?`,
		run.Status, run.ProviderKey, run.Model, run.ToolCalls, run.InputTokens, run.OutputTokens, run.CacheReadTokens, run.Cost,
		run.CitationsJSON, run.ErrorMessage, run.StderrTail, run.FinishedAt, run.ID)
	if err != nil {
		return fmt.Errorf("保存 AI 运行记录: %w", err)
	}
	return nil
}
