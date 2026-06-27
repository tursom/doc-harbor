package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func findOrCreateDocument(ctx context.Context, tx *sql.Tx, repoID int64, scanPath, docKey, branch, commit, title, filePath string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM documents WHERE repo_id = ? AND doc_key = ?`, repoID, docKey).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	now := nowString()
	res, err := tx.ExecContext(ctx, `INSERT INTO documents
		(repo_id, scan_path, doc_key, current_title, current_path, status, created_from_branch, created_from_commit, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)`,
		repoID, scanPath, docKey, title, filePath, branch, commit, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func findDocumentByPath(ctx context.Context, tx *sql.Tx, repoID int64, filePath string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT document_id FROM doc_versions
		WHERE repo_id = ? AND file_path = ? ORDER BY updated_at DESC LIMIT 1`, repoID, filePath).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM documents WHERE repo_id = ? AND doc_key = ?`, repoID, filePath).Scan(&id)
	if err == nil {
		return id, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return 0, err
}

func upsertDocVersion(ctx context.Context, tx *sql.Tx, v DocVersion) (int64, error) {
	now := nowString()
	res, err := tx.ExecContext(ctx, `INSERT INTO doc_versions
		(repo_id, document_id, branch, head_commit_sha, scan_path, file_path, previous_path, dir_path, file_name,
		 extension, mime_type, file_size, blob_sha, status, title, previewable, download_enabled, last_commit_sha,
		 last_commit_time, delete_commit_sha, delete_commit_time, rename_score, participates_latest, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, branch, document_id) DO UPDATE SET
		head_commit_sha = excluded.head_commit_sha,
		scan_path = excluded.scan_path,
		file_path = excluded.file_path,
		previous_path = excluded.previous_path,
		dir_path = excluded.dir_path,
		file_name = excluded.file_name,
		extension = excluded.extension,
		mime_type = excluded.mime_type,
		file_size = excluded.file_size,
		blob_sha = excluded.blob_sha,
		status = excluded.status,
		title = excluded.title,
		previewable = excluded.previewable,
		download_enabled = excluded.download_enabled,
		last_commit_sha = excluded.last_commit_sha,
		last_commit_time = excluded.last_commit_time,
		delete_commit_sha = excluded.delete_commit_sha,
		delete_commit_time = excluded.delete_commit_time,
		rename_score = excluded.rename_score,
		participates_latest = excluded.participates_latest,
		updated_at = excluded.updated_at`,
		v.RepoID, v.DocumentID, v.Branch, v.HeadCommitSHA, v.ScanPath, v.FilePath, v.PreviousPath, v.DirPath,
		v.FileName, v.Extension, v.MimeType, v.FileSize, v.BlobSHA, v.Status, v.Title, boolInt(v.Previewable),
		boolInt(v.DownloadEnabled), v.LastCommitSHA, v.LastCommitTime, v.DeleteCommitSHA, v.DeleteCommitTime,
		v.RenameScore, boolInt(v.ParticipatesLatest), now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err == nil && id > 0 {
		return id, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM doc_versions WHERE repo_id = ? AND branch = ? AND document_id = ?`,
		v.RepoID, v.Branch, v.DocumentID).Scan(&id)
	return id, err
}

func insertPathEvent(ctx context.Context, tx *sql.Tx, event PathEvent) error {
	now := nowString()
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO doc_path_events
		(repo_id, document_id, branch, event_type, old_path, new_path, commit_sha, commit_time, rename_score, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RepoID, event.DocumentID, event.Branch, event.EventType, event.OldPath, event.NewPath,
		event.CommitSHA, event.CommitTime, event.RenameScore, now)
	return err
}

func existingBranchVersions(ctx context.Context, tx *sql.Tx, repoID int64, branch string) (map[string]DocVersion, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, repo_id, document_id, branch, head_commit_sha, scan_path, file_path,
		previous_path, dir_path, file_name, extension, mime_type, file_size, blob_sha, status, title, previewable,
		download_enabled, last_commit_sha, last_commit_time, delete_commit_sha, delete_commit_time, rename_score,
		participates_latest, created_at, updated_at
		FROM doc_versions WHERE repo_id = ? AND branch = ?`, repoID, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := map[string]DocVersion{}
	for rows.Next() {
		v, err := scanDocVersion(rows)
		if err != nil {
			return nil, err
		}
		versions[v.FilePath] = v
	}
	return versions, rows.Err()
}

func markDeletedVersions(ctx context.Context, tx *sql.Tx, repoID int64, branch, headCommit, commitTime string, current map[string]bool) (int, error) {
	existing, err := existingBranchVersions(ctx, tx, repoID, branch)
	if err != nil {
		return 0, err
	}
	deleted := 0
	now := nowString()
	for filePath, v := range existing {
		if current[filePath] || v.Status == "deleted" {
			continue
		}
		_, err := tx.ExecContext(ctx, `UPDATE doc_versions SET status = 'deleted', delete_commit_sha = ?, delete_commit_time = ?,
			participates_latest = 0, updated_at = ? WHERE id = ?`, headCommit, commitTime, now, v.ID)
		if err != nil {
			return deleted, err
		}
		if err := insertPathEvent(ctx, tx, PathEvent{
			RepoID: v.RepoID, DocumentID: v.DocumentID, Branch: branch, EventType: "deleted",
			OldPath: filePath, CommitSHA: headCommit, CommitTime: commitTime,
		}); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func recomputeLatest(ctx context.Context, tx *sql.Tx, repo Repository) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM doc_latest WHERE repo_id = ?`, repo.ID); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, repo_id, document_id, branch, head_commit_sha, scan_path, file_path,
		previous_path, dir_path, file_name, extension, mime_type, file_size, blob_sha, status, title, previewable,
		download_enabled, last_commit_sha, last_commit_time, delete_commit_sha, delete_commit_time, rename_score,
		participates_latest, created_at, updated_at
		FROM doc_versions
		WHERE repo_id = ? AND status IN ('active','renamed','moved') AND participates_latest = 1`, repo.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byDoc := map[int64][]DocVersion{}
	for rows.Next() {
		v, err := scanDocVersion(rows)
		if err != nil {
			return err
		}
		byDoc[v.DocumentID] = append(byDoc[v.DocumentID], v)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	now := nowString()
	for documentID, candidates := range byDoc {
		sort.SliceStable(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if a.LastCommitTime != b.LastCommitTime {
				return a.LastCommitTime > b.LastCommitTime
			}
			ra := branchRank(repo.BranchPriority, a.Branch)
			rb := branchRank(repo.BranchPriority, b.Branch)
			if ra != rb {
				return ra < rb
			}
			if a.Branch != b.Branch {
				return a.Branch < b.Branch
			}
			return a.HeadCommitSHA < b.HeadCommitSHA
		})
		winner := candidates[0]
		reason := "latest_file_commit"
		if len(candidates) > 1 && candidates[0].LastCommitTime == candidates[1].LastCommitTime {
			reason = "branch_priority_tiebreak"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO doc_latest
			(repo_id, document_id, version_id, source_branch, source_commit_sha, file_path, dir_path, file_name,
			 last_commit_time, selection_reason, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			repo.ID, documentID, winner.ID, winner.Branch, winner.HeadCommitSHA, winner.FilePath, winner.DirPath,
			winner.FileName, winner.LastCommitTime, reason, now, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET current_title = ?, current_path = ?, status = 'active',
			latest_version_id = ?, updated_at = ? WHERE id = ?`, winner.Title, winner.FilePath, winner.ID, now, documentID); err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `UPDATE documents SET status = 'deleted', latest_version_id = 0, updated_at = ?
		WHERE repo_id = ? AND id NOT IN (SELECT document_id FROM doc_latest WHERE repo_id = ?)`, now, repo.ID, repo.ID)
	return err
}

func scanDocVersion(row repoScanner) (DocVersion, error) {
	var v DocVersion
	var previewable, downloadEnabled, participates int
	if err := row.Scan(&v.ID, &v.RepoID, &v.DocumentID, &v.Branch, &v.HeadCommitSHA, &v.ScanPath, &v.FilePath,
		&v.PreviousPath, &v.DirPath, &v.FileName, &v.Extension, &v.MimeType, &v.FileSize, &v.BlobSHA, &v.Status,
		&v.Title, &previewable, &downloadEnabled, &v.LastCommitSHA, &v.LastCommitTime, &v.DeleteCommitSHA,
		&v.DeleteCommitTime, &v.RenameScore, &participates, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return DocVersion{}, err
	}
	v.Previewable = scanBool(previewable)
	v.DownloadEnabled = scanBool(downloadEnabled)
	v.ParticipatesLatest = scanBool(participates)
	return v, nil
}

func getVersion(ctx context.Context, db *sql.DB, repoID, versionID int64) (DocVersion, error) {
	row := db.QueryRowContext(ctx, `SELECT id, repo_id, document_id, branch, head_commit_sha, scan_path, file_path,
		previous_path, dir_path, file_name, extension, mime_type, file_size, blob_sha, status, title, previewable,
		download_enabled, last_commit_sha, last_commit_time, delete_commit_sha, delete_commit_time, rename_score,
		participates_latest, created_at, updated_at
		FROM doc_versions WHERE repo_id = ? AND id = ?`, repoID, versionID)
	v, err := scanDocVersion(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DocVersion{}, errNotFound("version not found")
		}
		return DocVersion{}, err
	}
	return v, nil
}

func listDocumentVersions(ctx context.Context, db *sql.DB, repoID, documentID int64) ([]DocVersion, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, repo_id, document_id, branch, head_commit_sha, scan_path, file_path,
		previous_path, dir_path, file_name, extension, mime_type, file_size, blob_sha, status, title, previewable,
		download_enabled, last_commit_sha, last_commit_time, delete_commit_sha, delete_commit_time, rename_score,
		participates_latest, created_at, updated_at
		FROM doc_versions WHERE repo_id = ? AND document_id = ? ORDER BY branch`, repoID, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []DocVersion{}
	for rows.Next() {
		v, err := scanDocVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func getDocument(ctx context.Context, db *sql.DB, repoID, documentID int64) (Document, error) {
	row := db.QueryRowContext(ctx, `SELECT id, repo_id, scan_path, doc_key, current_title, current_path, status,
		created_from_branch, created_from_commit, latest_version_id, created_at, updated_at
		FROM documents WHERE repo_id = ? AND id = ?`, repoID, documentID)
	var doc Document
	if err := row.Scan(&doc.ID, &doc.RepoID, &doc.ScanPath, &doc.DocKey, &doc.CurrentTitle, &doc.CurrentPath, &doc.Status,
		&doc.CreatedFromBranch, &doc.CreatedFromCommit, &doc.LatestVersionID, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Document{}, errNotFound("document not found")
		}
		return Document{}, err
	}
	return doc, nil
}

func listPathEvents(ctx context.Context, db *sql.DB, repoID int64, documentID int64, branch, eventType string) ([]PathEvent, error) {
	query := `SELECT id, repo_id, document_id, branch, event_type, old_path, new_path, commit_sha, commit_time, rename_score, created_at
		FROM doc_path_events WHERE repo_id = ?`
	args := []any{repoID}
	if documentID > 0 {
		query += ` AND document_id = ?`
		args = append(args, documentID)
	}
	if branch != "" {
		query += ` AND branch = ?`
		args = append(args, branch)
	}
	if eventType != "" {
		query += ` AND event_type = ?`
		args = append(args, eventType)
	}
	query += ` ORDER BY commit_time DESC, id DESC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []PathEvent{}
	for rows.Next() {
		var event PathEvent
		if err := rows.Scan(&event.ID, &event.RepoID, &event.DocumentID, &event.Branch, &event.EventType,
			&event.OldPath, &event.NewPath, &event.CommitSHA, &event.CommitTime, &event.RenameScore, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func listFiles(ctx context.Context, db *sql.DB, repoID int64, view, branch, dir string) ([]FileEntry, error) {
	dir = normalizeRepoPath(dir)
	if dir == "" {
		return nil, errBadRequest("invalid dir")
	}
	if view == "" {
		view = "latest"
	}
	if view == "branch" {
		if branch == "" {
			return nil, errBadRequest("branch is required")
		}
		return listBranchFiles(ctx, db, repoID, branch, dir)
	}
	return listLatestFiles(ctx, db, repoID, dir)
}

func listLatestFiles(ctx context.Context, db *sql.DB, repoID int64, dir string) ([]FileEntry, error) {
	scanPaths, err := currentScanPaths(ctx, db, repoID)
	if err != nil {
		return nil, err
	}
	pathFilter, pathArgs := scanPathSQLFilter("l.file_path", scanPaths)
	args := append([]any{repoID, dir}, pathArgs...)
	rows, err := db.QueryContext(ctx, `SELECT v.id, v.repo_id, v.document_id, v.branch, v.head_commit_sha, v.scan_path, v.file_path,
			v.previous_path, v.dir_path, v.file_name, v.extension, v.mime_type, v.file_size, v.blob_sha, v.status, v.title, v.previewable,
			v.download_enabled, v.last_commit_sha, v.last_commit_time, v.delete_commit_sha, v.delete_commit_time, v.rename_score,
			v.participates_latest, v.created_at, v.updated_at, l.source_branch, l.source_commit_sha, l.selection_reason
			FROM doc_latest l
			JOIN doc_versions v ON v.id = l.version_id
			WHERE l.repo_id = ? AND l.dir_path = ? AND `+pathFilter+`
			ORDER BY l.file_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []FileEntry{}
	for rows.Next() {
		var v DocVersion
		var previewable, downloadEnabled, participates int
		if err := rows.Scan(&v.ID, &v.RepoID, &v.DocumentID, &v.Branch, &v.HeadCommitSHA, &v.ScanPath, &v.FilePath,
			&v.PreviousPath, &v.DirPath, &v.FileName, &v.Extension, &v.MimeType, &v.FileSize, &v.BlobSHA, &v.Status,
			&v.Title, &previewable, &downloadEnabled, &v.LastCommitSHA, &v.LastCommitTime, &v.DeleteCommitSHA,
			&v.DeleteCommitTime, &v.RenameScore, &participates, &v.CreatedAt, &v.UpdatedAt,
			&v.SourceBranch, &v.SourceCommitSHA, &v.SelectionReason); err != nil {
			return nil, err
		}
		v.Previewable = scanBool(previewable)
		v.DownloadEnabled = scanBool(downloadEnabled)
		v.ParticipatesLatest = scanBool(participates)
		entries = append(entries, versionFileEntry(v))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return withDirectories(ctx, db, repoID, "latest", "", dir, entries)
}

func listBranchFiles(ctx context.Context, db *sql.DB, repoID int64, branch, dir string) ([]FileEntry, error) {
	scanPaths, err := currentScanPaths(ctx, db, repoID)
	if err != nil {
		return nil, err
	}
	pathFilter, pathArgs := scanPathSQLFilter("file_path", scanPaths)
	args := append([]any{repoID, branch, dir}, pathArgs...)
	rows, err := db.QueryContext(ctx, `SELECT id, repo_id, document_id, branch, head_commit_sha, scan_path, file_path,
			previous_path, dir_path, file_name, extension, mime_type, file_size, blob_sha, status, title, previewable,
			download_enabled, last_commit_sha, last_commit_time, delete_commit_sha, delete_commit_time, rename_score,
			participates_latest, created_at, updated_at
			FROM doc_versions
			WHERE repo_id = ? AND branch = ? AND status IN ('active','renamed','moved') AND dir_path = ? AND `+pathFilter+`
			ORDER BY file_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []FileEntry{}
	for rows.Next() {
		v, err := scanDocVersion(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, versionFileEntry(v))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return withDirectories(ctx, db, repoID, "branch", branch, dir, entries)
}

func versionFileEntry(v DocVersion) FileEntry {
	sourceBranch := v.SourceBranch
	if sourceBranch == "" {
		sourceBranch = v.Branch
	}
	sourceCommit := v.SourceCommitSHA
	if sourceCommit == "" {
		sourceCommit = v.HeadCommitSHA
	}
	return FileEntry{
		Kind:            "file",
		Name:            v.FileName,
		Path:            v.FilePath,
		DocumentID:      v.DocumentID,
		VersionID:       v.ID,
		Title:           v.Title,
		Extension:       v.Extension,
		FileSize:        v.FileSize,
		Status:          v.Status,
		SourceBranch:    sourceBranch,
		SourceCommitSHA: sourceCommit,
		LastCommitTime:  v.LastCommitTime,
		Previewable:     v.Previewable,
		DownloadEnabled: v.DownloadEnabled,
		SelectionReason: v.SelectionReason,
	}
}

func withDirectories(ctx context.Context, db *sql.DB, repoID int64, view, branch, dir string, entries []FileEntry) ([]FileEntry, error) {
	scanPaths, err := currentScanPaths(ctx, db, repoID)
	if err != nil {
		return nil, err
	}
	dirs, err := childDirectories(ctx, db, repoID, view, branch, dir, scanPaths)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(dirs)+len(entries))
	for _, child := range dirs {
		out = append(out, FileEntry{Kind: "dir", Name: baseName(child), Path: child})
	}
	for _, entry := range entries {
		if entry.Kind == "file" && !fileVisibleInScanPaths(entry.Path, scanPaths) {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "dir"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func childDirectories(ctx context.Context, db *sql.DB, repoID int64, view, branch, dir string, scanPaths []ScanPath) ([]string, error) {
	var rows *sql.Rows
	var err error
	if view == "branch" {
		pathFilter, pathArgs := scanPathSQLFilter("file_path", scanPaths)
		args := append([]any{repoID, branch}, pathArgs...)
		rows, err = db.QueryContext(ctx, `SELECT DISTINCT dir_path FROM doc_versions
			WHERE repo_id = ? AND branch = ? AND status IN ('active','renamed','moved') AND `+pathFilter, args...)
	} else {
		pathFilter, pathArgs := scanPathSQLFilter("file_path", scanPaths)
		args := append([]any{repoID}, pathArgs...)
		rows, err = db.QueryContext(ctx, `SELECT DISTINCT dir_path FROM doc_latest WHERE repo_id = ? AND `+pathFilter, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]struct{}{}
	prefix := ""
	if dir != "." {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}
	for rows.Next() {
		var full string
		if err := rows.Scan(&full); err != nil {
			return nil, err
		}
		if dir == "." {
			if full == "." {
				continue
			}
			parts := strings.Split(full, "/")
			if len(parts) > 0 {
				set[parts[0]] = struct{}{}
			}
			continue
		}
		if !strings.HasPrefix(full, prefix) {
			continue
		}
		rest := strings.TrimPrefix(full, prefix)
		if rest == "" || strings.Contains(rest, "/") {
			if slash := strings.Index(rest, "/"); slash >= 0 {
				set[prefix+rest[:slash]] = struct{}{}
			}
			continue
		}
		set[prefix+rest] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(set))
	for dir := range set {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func searchDocuments(ctx context.Context, db *sql.DB, repoID int64, q string, limit int) ([]FileEntry, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	scanPaths, err := currentScanPaths(ctx, db, repoID)
	if err != nil {
		return nil, err
	}
	like := "%" + q + "%"
	pathFilter, pathArgs := scanPathSQLFilter("l.file_path", scanPaths)
	args := append([]any{repoID}, pathArgs...)
	args = append(args, like, like, limit)
	rows, err := db.QueryContext(ctx, `SELECT v.id, v.repo_id, v.document_id, v.branch, v.head_commit_sha, v.scan_path, v.file_path,
			v.previous_path, v.dir_path, v.file_name, v.extension, v.mime_type, v.file_size, v.blob_sha, v.status, v.title, v.previewable,
			v.download_enabled, v.last_commit_sha, v.last_commit_time, v.delete_commit_sha, v.delete_commit_time, v.rename_score,
			v.participates_latest, v.created_at, v.updated_at, l.source_branch, l.source_commit_sha, l.selection_reason
			FROM doc_latest l
			JOIN doc_versions v ON v.id = l.version_id
			WHERE l.repo_id = ? AND `+pathFilter+` AND (v.file_path LIKE ? OR v.title LIKE ?)
			ORDER BY v.last_commit_time DESC
			LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []FileEntry
	for rows.Next() {
		var v DocVersion
		var previewable, downloadEnabled, participates int
		if err := rows.Scan(&v.ID, &v.RepoID, &v.DocumentID, &v.Branch, &v.HeadCommitSHA, &v.ScanPath, &v.FilePath,
			&v.PreviousPath, &v.DirPath, &v.FileName, &v.Extension, &v.MimeType, &v.FileSize, &v.BlobSHA, &v.Status,
			&v.Title, &previewable, &downloadEnabled, &v.LastCommitSHA, &v.LastCommitTime, &v.DeleteCommitSHA,
			&v.DeleteCommitTime, &v.RenameScore, &participates, &v.CreatedAt, &v.UpdatedAt,
			&v.SourceBranch, &v.SourceCommitSHA, &v.SelectionReason); err != nil {
			return nil, err
		}
		v.Previewable = scanBool(previewable)
		v.DownloadEnabled = scanBool(downloadEnabled)
		v.ParticipatesLatest = scanBool(participates)
		entries = append(entries, versionFileEntry(v))
	}
	return entries, rows.Err()
}

func docHistory(ctx context.Context, db *sql.DB, repoID, documentID int64) (map[string]any, error) {
	doc, err := getDocument(ctx, db, repoID, documentID)
	if err != nil {
		return nil, err
	}
	versions, err := listDocumentVersions(ctx, db, repoID, documentID)
	if err != nil {
		return nil, err
	}
	events, err := listPathEvents(ctx, db, repoID, documentID, "", "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"document": doc,
		"versions": versions,
		"events":   events,
	}, nil
}

func treeForView(ctx context.Context, db *sql.DB, repoID int64, view, branch string) ([]FileEntry, error) {
	var rows *sql.Rows
	var err error
	scanPaths, err := currentScanPaths(ctx, db, repoID)
	if err != nil {
		return nil, err
	}
	if view == "branch" {
		if branch == "" {
			return nil, errBadRequest("branch is required")
		}
		pathFilter, pathArgs := scanPathSQLFilter("file_path", scanPaths)
		args := append([]any{repoID, branch}, pathArgs...)
		rows, err = db.QueryContext(ctx, `SELECT file_path, document_id, id, title, extension, file_size, status,
				branch, head_commit_sha, last_commit_time, previewable, download_enabled
				FROM doc_versions WHERE repo_id = ? AND branch = ? AND status IN ('active','renamed','moved')
				AND `+pathFilter+` ORDER BY file_path`, args...)
	} else {
		pathFilter, pathArgs := scanPathSQLFilter("l.file_path", scanPaths)
		args := append([]any{repoID}, pathArgs...)
		rows, err = db.QueryContext(ctx, `SELECT l.file_path, v.document_id, v.id, v.title, v.extension, v.file_size, v.status,
				l.source_branch, l.source_commit_sha, l.last_commit_time, v.previewable, v.download_enabled
				FROM doc_latest l JOIN doc_versions v ON v.id = l.version_id
				WHERE l.repo_id = ? AND `+pathFilter+` ORDER BY l.file_path`, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []FileEntry
	for rows.Next() {
		var entry FileEntry
		var preview, download int
		if err := rows.Scan(&entry.Path, &entry.DocumentID, &entry.VersionID, &entry.Title, &entry.Extension, &entry.FileSize,
			&entry.Status, &entry.SourceBranch, &entry.SourceCommitSHA, &entry.LastCommitTime, &preview, &download); err != nil {
			return nil, err
		}
		entry.Kind = "file"
		entry.Name = baseName(entry.Path)
		entry.Previewable = scanBool(preview)
		entry.DownloadEnabled = scanBool(download)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func currentScanPaths(ctx context.Context, db *sql.DB, repoID int64) ([]ScanPath, error) {
	scanPaths, err := listScanPaths(ctx, db, repoID, true)
	if err != nil {
		return nil, err
	}
	if len(scanPaths) == 0 {
		scanPaths = []ScanPath{{RepoID: repoID, Path: ".", Enabled: true}}
	}
	return scanPaths, nil
}

func scanPathSQLFilter(column string, scanPaths []ScanPath) (string, []any) {
	clauses := make([]string, 0, len(scanPaths))
	args := make([]any, 0, len(scanPaths)*2)
	for _, scanPath := range scanPaths {
		p := normalizeRepoPath(scanPath.Path)
		if p == "" {
			continue
		}
		if p == "." {
			return "1=1", nil
		}
		clauses = append(clauses, fmt.Sprintf("(%s = ? OR %s LIKE ?)", column, column))
		args = append(args, p, strings.TrimSuffix(p, "/")+"/%")
	}
	if len(clauses) == 0 {
		return "1=0", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

func fileVisibleInScanPaths(filePath string, scanPaths []ScanPath) bool {
	for _, scanPath := range scanPaths {
		if scanPathContains(scanPath.Path, filePath) {
			return true
		}
	}
	return false
}

func filterCommitChanges(changes []CommitFileChange, scanPaths []ScanPath) []CommitFileChange {
	filtered := changes[:0]
	for _, change := range changes {
		if fileVisibleInScanPaths(change.Path, scanPaths) ||
			fileVisibleInScanPaths(change.OldPath, scanPaths) ||
			fileVisibleInScanPaths(change.NewPath, scanPaths) {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

func buildDocKey(scanPath, filePath string) string {
	filePath = normalizeRepoPath(filePath)
	return filePath
}

func titleFromMarkdown(content []byte, fallback string) string {
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if title != "" {
				return title
			}
		}
	}
	return fallback
}

func pathEventType(oldPath, newPath string) string {
	oldDir, newDir := dirName(oldPath), dirName(newPath)
	oldName, newName := baseName(oldPath), baseName(newPath)
	if oldDir != newDir && oldName == newName {
		return "moved"
	}
	if oldDir != newDir {
		return "moved"
	}
	if oldName != newName {
		return "renamed"
	}
	return "renamed"
}

func participatesLatest(repo Repository, branch string, commitTime string) bool {
	if !matchBranchRules(repo.LatestIncludeBranches, branch, true) {
		return false
	}
	if matchBranchRules(repo.LatestExcludeBranches, branch, false) {
		return false
	}
	return true
}

func branchTracked(repo Repository, branch string) bool {
	return matchBranchRules(repo.TrackedBranches, branch, true)
}

func matchBranchRules(patterns []string, branch string, emptyMeansAll bool) bool {
	if len(patterns) == 0 {
		return emptyMeansAll
	}
	for _, pattern := range patterns {
		if pattern == "*" || matchGlob(pattern, branch) {
			return true
		}
	}
	return false
}

func humanScanDetail(branchStats map[string]any) string {
	if len(branchStats) == 0 {
		return "{}"
	}
	return encodeJSON(branchStats)
}

func validateRepository(repo Repository) error {
	if strings.TrimSpace(repo.Name) == "" {
		return errBadRequest("name is required")
	}
	if strings.TrimSpace(repo.RepoURL) == "" {
		return errBadRequest("repo_url is required")
	}
	if slugify(repo.Slug) == "" && slugify(repo.Name) == "" {
		return errBadRequest("slug is required")
	}
	for _, p := range repo.ScanPaths {
		if !validRepoPath(p.Path) {
			return errBadRequest(fmt.Sprintf("invalid scan path: %s", p.Path))
		}
	}
	return nil
}
