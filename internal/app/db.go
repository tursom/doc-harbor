package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const timeLayout = time.RFC3339

func openDB(ctx context.Context, cfg Config) (*sql.DB, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "repos"), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", cfg.DBDSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS repositories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE,
			repo_url TEXT NOT NULL,
			default_branch TEXT NOT NULL DEFAULT 'main',
			tracked_branches TEXT NOT NULL DEFAULT '["*"]',
			latest_include_branches TEXT NOT NULL DEFAULT '["*"]',
			latest_exclude_branches TEXT NOT NULL DEFAULT '["archive/*","tmp/*","dependabot/*"]',
			stale_branch_days INTEGER NOT NULL DEFAULT 180,
			branch_priority TEXT NOT NULL DEFAULT '["main","master","release/*","develop","feature/*"]',
			credential_ref TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			sync_interval_seconds INTEGER NOT NULL DEFAULT 3600,
			max_file_size_bytes INTEGER NOT NULL DEFAULT 2097152,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS repo_scan_paths (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			path TEXT NOT NULL,
			include_globs TEXT NOT NULL DEFAULT '[]',
			exclude_globs TEXT NOT NULL DEFAULT '[".git/**","node_modules/**","vendor/**","dist/**","build/**",".DS_Store","*.tmp","*.swp"]',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(repo_id, path),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS repo_refs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			ref_type TEXT NOT NULL,
			ref_name TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			commit_time TEXT NOT NULL,
			last_scanned_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(repo_id, ref_type, ref_name),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			scan_path TEXT NOT NULL,
			doc_key TEXT NOT NULL,
			current_title TEXT NOT NULL DEFAULT '',
			current_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_from_branch TEXT NOT NULL DEFAULT '',
			created_from_commit TEXT NOT NULL DEFAULT '',
			latest_version_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(repo_id, doc_key),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS doc_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			document_id INTEGER NOT NULL,
			branch TEXT NOT NULL,
			head_commit_sha TEXT NOT NULL,
			scan_path TEXT NOT NULL,
			file_path TEXT NOT NULL,
			previous_path TEXT NOT NULL DEFAULT '',
			dir_path TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL DEFAULT '',
			extension TEXT NOT NULL DEFAULT '',
			mime_type TEXT NOT NULL DEFAULT '',
			file_size INTEGER NOT NULL DEFAULT 0,
			blob_sha TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			title TEXT NOT NULL DEFAULT '',
			previewable INTEGER NOT NULL DEFAULT 0,
			download_enabled INTEGER NOT NULL DEFAULT 1,
			last_commit_sha TEXT NOT NULL DEFAULT '',
			last_commit_time TEXT NOT NULL DEFAULT '',
			delete_commit_sha TEXT NOT NULL DEFAULT '',
			delete_commit_time TEXT NOT NULL DEFAULT '',
			rename_score INTEGER NOT NULL DEFAULT 0,
			participates_latest INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(repo_id, branch, document_id),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE,
			FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_versions_repo_branch_dir ON doc_versions(repo_id, branch, dir_path)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_versions_repo_document_branch ON doc_versions(repo_id, document_id, branch)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_versions_repo_last_commit ON doc_versions(repo_id, last_commit_time)`,
		`CREATE TABLE IF NOT EXISTS doc_latest (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			document_id INTEGER NOT NULL,
			version_id INTEGER NOT NULL,
			source_branch TEXT NOT NULL,
			source_commit_sha TEXT NOT NULL,
			file_path TEXT NOT NULL,
			dir_path TEXT NOT NULL,
			file_name TEXT NOT NULL,
			last_commit_time TEXT NOT NULL,
			selection_reason TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(repo_id, document_id),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE,
			FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE,
			FOREIGN KEY(version_id) REFERENCES doc_versions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_latest_repo_dir ON doc_latest(repo_id, dir_path)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_latest_repo_time ON doc_latest(repo_id, last_commit_time)`,
		`CREATE TABLE IF NOT EXISTS doc_path_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			document_id INTEGER NOT NULL,
			branch TEXT NOT NULL,
			event_type TEXT NOT NULL,
			old_path TEXT NOT NULL DEFAULT '',
			new_path TEXT NOT NULL DEFAULT '',
			commit_sha TEXT NOT NULL,
			commit_time TEXT NOT NULL,
			rename_score INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(repo_id, document_id, branch, event_type, old_path, new_path, commit_sha),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE,
			FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_path_events_document ON doc_path_events(repo_id, document_id, branch, commit_time)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_path_events_commit ON doc_path_events(repo_id, branch, commit_sha)`,
		`CREATE TABLE IF NOT EXISTS scan_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			trigger_type TEXT NOT NULL,
			status TEXT NOT NULL,
			branch_count INTEGER NOT NULL DEFAULT 0,
			file_count INTEGER NOT NULL DEFAULT 0,
			skipped_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			detail_json TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func nowString() string {
	return time.Now().UTC().Format(timeLayout)
}

func encodeJSON(v any) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeStringList(raw string, fallback []string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cloneStrings(fallback)
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return cloneStrings(fallback)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 && len(fallback) > 0 {
		return cloneStrings(fallback)
	}
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func scanBool(v int) bool {
	return v != 0
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func listRepositories(ctx context.Context, db *sql.DB) ([]Repository, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, slug, repo_url, default_branch, tracked_branches,
		latest_include_branches, latest_exclude_branches, stale_branch_days, branch_priority, credential_ref,
		enabled, sync_interval_seconds, max_file_size_bytes, created_at, updated_at
		FROM repositories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	repos := []Repository{}
	for rows.Next() {
		repo, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range repos {
		repos[i].ScanPaths, _ = listScanPaths(ctx, db, repos[i].ID, false)
		run, _ := latestScanRun(ctx, db, repos[i].ID)
		repos[i].LatestScan = run
	}
	return repos, nil
}

func getRepository(ctx context.Context, db *sql.DB, id int64) (Repository, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, slug, repo_url, default_branch, tracked_branches,
		latest_include_branches, latest_exclude_branches, stale_branch_days, branch_priority, credential_ref,
		enabled, sync_interval_seconds, max_file_size_bytes, created_at, updated_at
		FROM repositories WHERE id = ?`, id)
	repo, err := scanRepository(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repository{}, errNotFound("repository not found")
		}
		return Repository{}, err
	}
	repo.ScanPaths, _ = listScanPaths(ctx, db, repo.ID, false)
	run, _ := latestScanRun(ctx, db, repo.ID)
	repo.LatestScan = run
	return repo, nil
}

type repoScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row repoScanner) (Repository, error) {
	var repo Repository
	var tracked, include, exclude, priority string
	var enabled int
	if err := row.Scan(&repo.ID, &repo.Name, &repo.Slug, &repo.RepoURL, &repo.DefaultBranch, &tracked,
		&include, &exclude, &repo.StaleBranchDays, &priority, &repo.CredentialRef, &enabled,
		&repo.SyncIntervalSeconds, &repo.MaxFileSizeBytes, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
		return Repository{}, err
	}
	repo.TrackedBranches = decodeStringList(tracked, []string{"*"})
	repo.LatestIncludeBranches = decodeStringList(include, []string{"*"})
	repo.LatestExcludeBranches = decodeStringList(exclude, []string{"archive/*", "tmp/*", "dependabot/*"})
	repo.BranchPriority = decodeStringList(priority, []string{"main", "master", "release/*", "develop", "feature/*"})
	repo.Enabled = scanBool(enabled)
	return repo, nil
}

func createRepository(ctx context.Context, db *sql.DB, repo Repository) (Repository, error) {
	repo = withRepositoryDefaults(repo)
	now := nowString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Repository{}, err
	}
	defer rollback(tx)

	res, err := tx.ExecContext(ctx, `INSERT INTO repositories
		(name, slug, repo_url, default_branch, tracked_branches, latest_include_branches, latest_exclude_branches,
		 stale_branch_days, branch_priority, credential_ref, enabled, sync_interval_seconds, max_file_size_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		repo.Name, repo.Slug, repo.RepoURL, repo.DefaultBranch, encodeJSON(repo.TrackedBranches),
		encodeJSON(repo.LatestIncludeBranches), encodeJSON(repo.LatestExcludeBranches), repo.StaleBranchDays,
		encodeJSON(repo.BranchPriority), repo.CredentialRef, boolInt(repo.Enabled), repo.SyncIntervalSeconds,
		repo.MaxFileSizeBytes, now, now)
	if err != nil {
		return Repository{}, err
	}
	repoID, err := res.LastInsertId()
	if err != nil {
		return Repository{}, err
	}

	if len(repo.ScanPaths) == 0 {
		repo.ScanPaths = []ScanPath{{Path: ".", Enabled: true}}
	}
	for _, path := range repo.ScanPaths {
		path = withScanPathDefaults(path)
		if _, err := tx.ExecContext(ctx, `INSERT INTO repo_scan_paths
			(repo_id, path, include_globs, exclude_globs, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			repoID, path.Path, encodeJSON(path.IncludeGlobs), encodeJSON(path.ExcludeGlobs), boolInt(path.Enabled), now, now); err != nil {
			return Repository{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, err
	}
	return getRepository(ctx, db, repoID)
}

func updateRepository(ctx context.Context, db *sql.DB, id int64, repo Repository) (Repository, error) {
	current, err := getRepository(ctx, db, id)
	if err != nil {
		return Repository{}, err
	}
	merged := mergeRepository(current, repo)
	merged = withRepositoryDefaults(merged)
	now := nowString()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Repository{}, err
	}
	defer rollback(tx)

	res, err := tx.ExecContext(ctx, `UPDATE repositories SET
		name = ?, slug = ?, repo_url = ?, default_branch = ?, tracked_branches = ?, latest_include_branches = ?,
		latest_exclude_branches = ?, stale_branch_days = ?, branch_priority = ?, credential_ref = ?, enabled = ?,
		sync_interval_seconds = ?, max_file_size_bytes = ?, updated_at = ?
		WHERE id = ?`,
		merged.Name, merged.Slug, merged.RepoURL, merged.DefaultBranch, encodeJSON(merged.TrackedBranches),
		encodeJSON(merged.LatestIncludeBranches), encodeJSON(merged.LatestExcludeBranches), merged.StaleBranchDays,
		encodeJSON(merged.BranchPriority), merged.CredentialRef, boolInt(merged.Enabled), merged.SyncIntervalSeconds,
		merged.MaxFileSizeBytes, now, id)
	if err != nil {
		return Repository{}, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return Repository{}, errNotFound("repository not found")
	}

	if repo.ScanPaths != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM repo_scan_paths WHERE repo_id = ?`, id); err != nil {
			return Repository{}, err
		}
		if len(repo.ScanPaths) == 0 {
			repo.ScanPaths = []ScanPath{{Path: ".", Enabled: true}}
		}
		for _, path := range repo.ScanPaths {
			path = withScanPathDefaults(path)
			if _, err := tx.ExecContext(ctx, `INSERT INTO repo_scan_paths
				(repo_id, path, include_globs, exclude_globs, enabled, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, path.Path, encodeJSON(path.IncludeGlobs), encodeJSON(path.ExcludeGlobs), boolInt(path.Enabled), now, now); err != nil {
				return Repository{}, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return Repository{}, err
	}
	return getRepository(ctx, db, id)
}

func disableRepository(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `UPDATE repositories SET enabled = 0, updated_at = ? WHERE id = ?`, nowString(), id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return errNotFound("repository not found")
	}
	return nil
}

func withRepositoryDefaults(repo Repository) Repository {
	repo.Name = strings.TrimSpace(repo.Name)
	repo.Slug = slugify(repo.Slug)
	if repo.Slug == "" {
		repo.Slug = slugify(repo.Name)
	}
	repo.RepoURL = strings.TrimSpace(repo.RepoURL)
	repo.DefaultBranch = strings.TrimSpace(repo.DefaultBranch)
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}
	if len(repo.TrackedBranches) == 0 {
		repo.TrackedBranches = []string{"*"}
	}
	if len(repo.LatestIncludeBranches) == 0 {
		repo.LatestIncludeBranches = []string{"*"}
	}
	if len(repo.LatestExcludeBranches) == 0 {
		repo.LatestExcludeBranches = []string{"archive/*", "tmp/*", "dependabot/*"}
	}
	if repo.StaleBranchDays <= 0 {
		repo.StaleBranchDays = 180
	}
	if len(repo.BranchPriority) == 0 {
		repo.BranchPriority = []string{"main", "master", "release/*", "develop", "feature/*"}
	}
	if repo.SyncIntervalSeconds <= 0 {
		repo.SyncIntervalSeconds = 3600
	}
	if repo.MaxFileSizeBytes <= 0 {
		repo.MaxFileSizeBytes = 2 * 1024 * 1024
	}
	repo.Enabled = true
	return repo
}

func mergeRepository(current, patch Repository) Repository {
	if patch.Name != "" {
		current.Name = patch.Name
	}
	if patch.Slug != "" {
		current.Slug = patch.Slug
	}
	if patch.RepoURL != "" {
		current.RepoURL = patch.RepoURL
	}
	if patch.DefaultBranch != "" {
		current.DefaultBranch = patch.DefaultBranch
	}
	if patch.TrackedBranches != nil {
		current.TrackedBranches = patch.TrackedBranches
	}
	if patch.LatestIncludeBranches != nil {
		current.LatestIncludeBranches = patch.LatestIncludeBranches
	}
	if patch.LatestExcludeBranches != nil {
		current.LatestExcludeBranches = patch.LatestExcludeBranches
	}
	if patch.StaleBranchDays > 0 {
		current.StaleBranchDays = patch.StaleBranchDays
	}
	if patch.BranchPriority != nil {
		current.BranchPriority = patch.BranchPriority
	}
	if patch.CredentialRef != "" {
		current.CredentialRef = patch.CredentialRef
	}
	current.Enabled = patch.Enabled || current.Enabled
	if patch.SyncIntervalSeconds > 0 {
		current.SyncIntervalSeconds = patch.SyncIntervalSeconds
	}
	if patch.MaxFileSizeBytes > 0 {
		current.MaxFileSizeBytes = patch.MaxFileSizeBytes
	}
	return current
}

func withScanPathDefaults(path ScanPath) ScanPath {
	path.Path = normalizeRepoPath(path.Path)
	if path.Path == "" {
		path.Path = "."
	}
	if path.ExcludeGlobs == nil {
		path.ExcludeGlobs = []string{".git/**", "node_modules/**", "vendor/**", "dist/**", "build/**", ".DS_Store", "*.tmp", "*.swp"}
	}
	path.Enabled = true
	return path
}

func listScanPaths(ctx context.Context, db *sql.DB, repoID int64, onlyEnabled bool) ([]ScanPath, error) {
	query := `SELECT id, repo_id, path, include_globs, exclude_globs, enabled, created_at, updated_at
		FROM repo_scan_paths WHERE repo_id = ?`
	if onlyEnabled {
		query += ` AND enabled = 1`
	}
	query += ` ORDER BY path`
	rows, err := db.QueryContext(ctx, query, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []ScanPath
	for rows.Next() {
		var path ScanPath
		var include, exclude string
		var enabled int
		if err := rows.Scan(&path.ID, &path.RepoID, &path.Path, &include, &exclude, &enabled, &path.CreatedAt, &path.UpdatedAt); err != nil {
			return nil, err
		}
		path.IncludeGlobs = decodeStringList(include, nil)
		path.ExcludeGlobs = decodeStringList(exclude, []string{".git/**", "node_modules/**", "vendor/**", "dist/**", "build/**", ".DS_Store", "*.tmp", "*.swp"})
		path.Enabled = scanBool(enabled)
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func upsertRepoRef(ctx context.Context, tx *sql.Tx, ref RepoRef) error {
	now := nowString()
	_, err := tx.ExecContext(ctx, `INSERT INTO repo_refs
		(repo_id, ref_type, ref_name, commit_sha, commit_time, last_scanned_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, ref_type, ref_name) DO UPDATE SET
		commit_sha = excluded.commit_sha,
		commit_time = excluded.commit_time,
		last_scanned_at = excluded.last_scanned_at,
		updated_at = excluded.updated_at`,
		ref.RepoID, ref.RefType, ref.RefName, ref.CommitSHA, ref.CommitTime, ref.LastScannedAt, now, now)
	return err
}

func getRepoRef(ctx context.Context, db *sql.DB, repoID int64, refType, refName string) (*RepoRef, error) {
	row := db.QueryRowContext(ctx, `SELECT id, repo_id, ref_type, ref_name, commit_sha, commit_time, last_scanned_at, created_at, updated_at
		FROM repo_refs WHERE repo_id = ? AND ref_type = ? AND ref_name = ?`, repoID, refType, refName)
	var ref RepoRef
	if err := row.Scan(&ref.ID, &ref.RepoID, &ref.RefType, &ref.RefName, &ref.CommitSHA, &ref.CommitTime, &ref.LastScannedAt, &ref.CreatedAt, &ref.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

func listBranches(ctx context.Context, db *sql.DB, repoID int64) ([]RepoRef, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, repo_id, ref_type, ref_name, commit_sha, commit_time, last_scanned_at, created_at, updated_at
		FROM repo_refs WHERE repo_id = ? AND ref_type = 'branch' ORDER BY ref_name`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []RepoRef{}
	for rows.Next() {
		var ref RepoRef
		if err := rows.Scan(&ref.ID, &ref.RepoID, &ref.RefType, &ref.RefName, &ref.CommitSHA, &ref.CommitTime, &ref.LastScannedAt, &ref.CreatedAt, &ref.UpdatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func createScanRun(ctx context.Context, db *sql.DB, repoID int64, trigger string) (ScanRun, error) {
	start := nowString()
	res, err := db.ExecContext(ctx, `INSERT INTO scan_runs (repo_id, trigger_type, status, started_at)
		VALUES (?, ?, 'running', ?)`, repoID, trigger, start)
	if err != nil {
		return ScanRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ScanRun{}, err
	}
	return ScanRun{ID: id, RepoID: repoID, TriggerType: trigger, Status: "running", StartedAt: start}, nil
}

func finishScanRun(ctx context.Context, db *sql.DB, run ScanRun) error {
	run.FinishedAt = nowString()
	_, err := db.ExecContext(ctx, `UPDATE scan_runs SET status = ?, branch_count = ?, file_count = ?, skipped_count = ?,
		error_count = ?, finished_at = ?, error_message = ?, detail_json = ? WHERE id = ?`,
		run.Status, run.BranchCount, run.FileCount, run.SkippedCount, run.ErrorCount, run.FinishedAt, run.ErrorMessage, run.DetailJSON, run.ID)
	return err
}

func listScanRuns(ctx context.Context, db *sql.DB, repoID int64, limit int) ([]ScanRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `SELECT id, repo_id, trigger_type, status, branch_count, file_count, skipped_count,
		error_count, started_at, finished_at, error_message, detail_json
		FROM scan_runs WHERE repo_id = ? ORDER BY id DESC LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []ScanRun{}
	for rows.Next() {
		var run ScanRun
		if err := rows.Scan(&run.ID, &run.RepoID, &run.TriggerType, &run.Status, &run.BranchCount, &run.FileCount,
			&run.SkippedCount, &run.ErrorCount, &run.StartedAt, &run.FinishedAt, &run.ErrorMessage, &run.DetailJSON); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func latestScanRun(ctx context.Context, db *sql.DB, repoID int64) (*ScanRun, error) {
	row := db.QueryRowContext(ctx, `SELECT id, repo_id, trigger_type, status, branch_count, file_count, skipped_count,
		error_count, started_at, finished_at, error_message, detail_json
		FROM scan_runs WHERE repo_id = ? ORDER BY id DESC LIMIT 1`, repoID)
	var run ScanRun
	if err := row.Scan(&run.ID, &run.RepoID, &run.TriggerType, &run.Status, &run.BranchCount, &run.FileCount,
		&run.SkippedCount, &run.ErrorCount, &run.StartedAt, &run.FinishedAt, &run.ErrorMessage, &run.DetailJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func txOrDBExec(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	if tx == nil {
		return nil, fmt.Errorf("nil transaction")
	}
	return tx.ExecContext(ctx, query, args...)
}
