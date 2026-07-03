package app

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"path"
	"strings"
	"sync"
	"time"
)

type Scanner struct {
	db    *sql.DB
	git   *Git
	locks sync.Map
}

func newScanner(db *sql.DB, git *Git) *Scanner {
	return &Scanner{db: db, git: git}
}

func (s *Scanner) Scan(ctx context.Context, repoID int64, trigger string) (result ScanRun, resultErr error) {
	lockValue, _ := s.locks.LoadOrStore(repoID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	if !lock.TryLock() {
		return ScanRun{}, errConflict("scan already running")
	}
	defer lock.Unlock()

	repo, err := getRepository(ctx, s.db, repoID)
	if err != nil {
		return ScanRun{}, err
	}
	run, err := createScanRun(ctx, s.db, repo.ID, trigger)
	if err != nil {
		return ScanRun{}, err
	}
	result = run

	run.Status = "success"
	details := map[string]any{}
	defer func() {
		run.DetailJSON = humanScanDetail(details)
		run.FinishedAt = nowString()
		result = run
		if err := finishScanRun(context.Background(), s.db, result); err != nil {
			log.Printf("finish scan run %d: %v", result.ID, err)
		}
	}()

	if !repo.Enabled {
		run.Status = "failed"
		run.ErrorMessage = "repository disabled"
		return run, errBadRequest("repository disabled")
	}
	if err := s.git.ensureMirror(ctx, repo); err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return run, err
	}

	repoPath := s.git.repoPath(repo.ID)
	refs, err := s.git.branches(ctx, repoPath)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return run, err
	}

	scanPaths, err := listScanPaths(ctx, s.db, repo.ID, true)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return run, err
	}
	if len(scanPaths) == 0 {
		scanPaths = []ScanPath{{RepoID: repo.ID, Path: ".", Enabled: true}}
	}

	branchStats := map[string]any{}
	for _, ref := range refs {
		if !branchTracked(repo, ref.Name) {
			continue
		}
		run.BranchCount++
		stat, err := s.scanBranch(ctx, repo, ref, scanPaths)
		branchStats[ref.Name] = stat
		if err != nil {
			run.ErrorCount++
			run.Status = "partial_success"
			continue
		}
		run.FileCount += stat.Files
		run.SkippedCount += stat.Skipped
		run.ErrorCount += stat.Errors
	}
	details["branches"] = branchStats

	if run.BranchCount == 0 {
		run.Status = "failed"
		run.ErrorMessage = "no tracked branches found"
		return run, errBadRequest("no tracked branches found")
	}
	if err := s.recomputeLatestTx(ctx, repo); err != nil {
		run.Status = "partial_success"
		run.ErrorMessage = err.Error()
		return run, err
	}
	if run.ErrorCount > 0 && run.Status == "success" {
		run.Status = "partial_success"
	}
	return run, nil
}

type branchScanStat struct {
	Files         int    `json:"files"`
	Skipped       int    `json:"skipped"`
	Errors        int    `json:"errors"`
	Head          string `json:"head"`
	SkippedByHead bool   `json:"skipped_by_head"`
	Error         string `json:"error,omitempty"`
}

func (s *Scanner) scanBranch(ctx context.Context, repo Repository, ref gitRef, scanPaths []ScanPath) (branchScanStat, error) {
	stat := branchScanStat{Head: ref.CommitSHA}
	repoPath := s.git.repoPath(repo.ID)
	oldRef, _ := getRepoRef(ctx, s.db, repo.ID, "branch", ref.Name)
	if oldRef != nil && oldRef.CommitSHA == ref.CommitSHA && !scanPathsChangedSince(scanPaths, oldRef.LastScannedAt) {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return stat, err
		}
		defer rollback(tx)
		if err := upsertRepoRef(ctx, tx, RepoRef{
			RepoID: repo.ID, RefType: "branch", RefName: ref.Name, CommitSHA: ref.CommitSHA,
			CommitTime: ref.CommitTime, LastScannedAt: nowString(),
		}); err != nil {
			return stat, err
		}
		if err := tx.Commit(); err != nil {
			return stat, err
		}
		stat.SkippedByHead = true
		return stat, nil
	}

	renameMap := map[string]CommitFileChange{}
	if oldRef != nil && oldRef.CommitSHA != "" {
		for _, sp := range scanPaths {
			changes, err := s.git.diffNameStatus(ctx, repoPath, oldRef.CommitSHA, ref.CommitSHA, sp.Path)
			if err != nil {
				stat.Errors++
				continue
			}
			for _, change := range changes {
				if change.OldPath != "" && change.NewPath != "" {
					renameMap[change.NewPath] = change
				}
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stat, err
	}
	defer rollback(tx)

	currentPaths := map[string]bool{}
	for _, sp := range scanPaths {
		entries, err := s.git.lsTree(ctx, repoPath, ref.CommitSHA, sp.Path)
		if err != nil {
			stat.Errors++
			stat.Error = err.Error()
			continue
		}
		for _, entry := range entries {
			if entry.Type != "blob" {
				continue
			}
			if s.shouldSkip(entry, repo, sp) {
				stat.Skipped++
				continue
			}
			currentPaths[entry.Path] = true
			v, err := s.versionFromEntry(ctx, repo, ref, sp, entry, renameMap[entry.Path])
			if err != nil {
				stat.Errors++
				continue
			}
			docID := int64(0)
			if v.PreviousPath != "" {
				docID, err = findDocumentByPath(ctx, tx, repo.ID, v.PreviousPath)
				if err != nil {
					stat.Errors++
					continue
				}
			}
			if docID == 0 {
				docID, err = findOrCreateDocument(ctx, tx, repo.ID, sp.Path, buildDocKey(sp.Path, entry.Path), ref.Name, ref.CommitSHA, v.Title, entry.Path)
				if err != nil {
					stat.Errors++
					continue
				}
			}
			v.DocumentID = docID
			versionID, err := upsertDocVersion(ctx, tx, v)
			if err != nil {
				stat.Errors++
				continue
			}
			v.ID = versionID
			if v.PreviousPath != "" {
				if err := insertPathEvent(ctx, tx, PathEvent{
					RepoID: repo.ID, DocumentID: docID, Branch: ref.Name, EventType: pathEventType(v.PreviousPath, v.FilePath),
					OldPath: v.PreviousPath, NewPath: v.FilePath, CommitSHA: v.LastCommitSHA,
					CommitTime: v.LastCommitTime, RenameScore: v.RenameScore,
				}); err != nil {
					stat.Errors++
				}
			}
			stat.Files++
		}
		if err := s.backfillPathHistory(ctx, tx, repo, ref, sp); err != nil {
			stat.Errors++
		}
	}
	if _, err := markDeletedVersions(ctx, tx, repo.ID, ref.Name, ref.CommitSHA, ref.CommitTime, currentPaths); err != nil {
		stat.Errors++
	}
	if err := upsertRepoRef(ctx, tx, RepoRef{
		RepoID: repo.ID, RefType: "branch", RefName: ref.Name, CommitSHA: ref.CommitSHA,
		CommitTime: ref.CommitTime, LastScannedAt: nowString(),
	}); err != nil {
		return stat, err
	}
	if err := tx.Commit(); err != nil {
		return stat, err
	}
	if stat.Errors > 0 {
		return stat, errors.New("branch scanned with file errors")
	}
	return stat, nil
}

func (s *Scanner) backfillPathHistory(ctx context.Context, tx *sql.Tx, repo Repository, ref gitRef, sp ScanPath) error {
	events, err := s.git.pathHistory(ctx, s.git.repoPath(repo.ID), ref.Name, sp.Path)
	if err != nil {
		return err
	}
	for _, event := range events {
		filePath := event.NewPath
		if filePath == "" {
			filePath = event.OldPath
		}
		if filePath == "" {
			continue
		}
		docID, err := findDocumentByPath(ctx, tx, repo.ID, filePath)
		if err != nil {
			return err
		}
		if docID == 0 && event.OldPath != "" {
			oldDocID, oldErr := findDocumentByPath(ctx, tx, repo.ID, event.OldPath)
			if oldErr != nil {
				return oldErr
			}
			if docID != 0 && oldDocID != 0 && oldDocID != docID {
				if err := reassignPathEvents(ctx, tx, repo.ID, ref.Name, oldDocID, docID); err != nil {
					return err
				}
			}
			if docID == 0 {
				docID = oldDocID
			}
		} else if docID != 0 && event.OldPath != "" {
			oldDocID, oldErr := findDocumentByPath(ctx, tx, repo.ID, event.OldPath)
			if oldErr != nil {
				return oldErr
			}
			if oldDocID != 0 && oldDocID != docID {
				if err := reassignPathEvents(ctx, tx, repo.ID, ref.Name, oldDocID, docID); err != nil {
					return err
				}
			}
		}
		if err != nil {
			return err
		}
		if docID == 0 && event.OldPath != "" {
			docID, err = findDocumentByPath(ctx, tx, repo.ID, event.OldPath)
			if err != nil {
				return err
			}
		}
		if docID == 0 {
			docID, err = findOrCreateDocument(ctx, tx, repo.ID, sp.Path, buildDocKey(sp.Path, filePath), ref.Name, event.CommitSHA, baseName(filePath), filePath)
			if err != nil {
				return err
			}
		}
		if err := insertPathEvent(ctx, tx, PathEvent{
			RepoID: repo.ID, DocumentID: docID, Branch: ref.Name, EventType: event.EventType,
			OldPath: event.OldPath, NewPath: event.NewPath, CommitSHA: event.CommitSHA,
			CommitTime: event.CommitTime, RenameScore: event.RenameScore,
		}); err != nil {
			return err
		}
		if (event.EventType == "renamed" || event.EventType == "moved") && event.NewPath != "" {
			if err := markCurrentPathLifecycle(ctx, tx, repo.ID, ref.Name, event.NewPath, event.OldPath, event.EventType, event.RenameScore); err != nil {
				return err
			}
		}
	}
	return nil
}

func reassignPathEvents(ctx context.Context, tx *sql.Tx, repoID int64, branch string, fromDocID, toDocID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE doc_path_events SET document_id = ?
		WHERE repo_id = ? AND branch = ? AND document_id = ?`, toDocID, repoID, branch, fromDocID)
	return err
}

func markCurrentPathLifecycle(ctx context.Context, tx *sql.Tx, repoID int64, branch, filePath, previousPath, status string, renameScore int) error {
	_, err := tx.ExecContext(ctx, `UPDATE doc_versions SET status = ?, previous_path = ?, rename_score = ?, updated_at = ?
		WHERE repo_id = ? AND branch = ? AND file_path = ? AND status IN ('active','renamed','moved')`,
		status, previousPath, renameScore, nowString(), repoID, branch, filePath)
	return err
}

func (s *Scanner) versionFromEntry(ctx context.Context, repo Repository, ref gitRef, sp ScanPath, entry treeEntry, rename CommitFileChange) (DocVersion, error) {
	repoPath := s.git.repoPath(repo.ID)
	last, err := s.git.lastCommitForPath(ctx, repoPath, ref.CommitSHA, entry.Path)
	if err != nil {
		last = lastCommit{SHA: ref.CommitSHA, Time: ref.CommitTime}
	}
	title := baseName(entry.Path)
	if isMarkdown(entry.Path) && entry.Size <= repo.MaxFileSizeBytes {
		if content, err := s.git.catFile(ctx, repoPath, entry.BlobSHA); err == nil {
			title = titleFromMarkdown(content, title)
		}
	}
	previousPath := ""
	renameScore := 0
	status := "active"
	if rename.OldPath != "" && rename.NewPath == entry.Path {
		previousPath = rename.OldPath
		renameScore = rename.RenameScore
		if pathEventType(rename.OldPath, rename.NewPath) == "moved" {
			status = "moved"
		} else {
			status = "renamed"
		}
	}
	return DocVersion{
		RepoID:             repo.ID,
		Branch:             ref.Name,
		HeadCommitSHA:      ref.CommitSHA,
		ScanPath:           sp.Path,
		FilePath:           entry.Path,
		PreviousPath:       previousPath,
		DirPath:            dirName(entry.Path),
		FileName:           baseName(entry.Path),
		Extension:          extension(entry.Path),
		MimeType:           mimeType(entry.Path),
		FileSize:           entry.Size,
		BlobSHA:            entry.BlobSHA,
		Status:             status,
		Title:              title,
		Previewable:        isPreviewable(entry.Path) && entry.Size <= repo.MaxFileSizeBytes,
		DownloadEnabled:    true,
		LastCommitSHA:      last.SHA,
		LastCommitTime:     last.Time,
		RenameScore:        renameScore,
		ParticipatesLatest: participatesLatest(repo, ref.Name, ref.CommitTime),
	}, nil
}

func (s *Scanner) shouldSkip(entry treeEntry, repo Repository, sp ScanPath) bool {
	filePath := normalizeRepoPath(entry.Path)
	if filePath == "" {
		return true
	}
	parts := strings.Split(filePath, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".") && part != "." && part != ".well-known" {
			if part == ".git" || part == ".DS_Store" {
				return true
			}
		}
	}
	if entry.Size > repo.MaxFileSizeBytes && isPreviewable(filePath) {
		// Oversized previewable documents remain downloadable but not previewable.
		return false
	}
	if entry.Size > repo.MaxFileSizeBytes*20 {
		return true
	}
	if len(sp.IncludeGlobs) > 0 && !matchAny(sp.IncludeGlobs, filePath) {
		return true
	}
	if matchAny(sp.ExcludeGlobs, filePath) {
		return true
	}
	// path.Match treats a bare directory name as a file match; keep common build dirs out as a final guard.
	for _, part := range parts {
		switch part {
		case ".git", "node_modules", "vendor", "dist", "build", "tmp", ".cache":
			return true
		}
	}
	return false
}

func (s *Scanner) recomputeLatestTx(ctx context.Context, repo Repository) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := recomputeLatest(ctx, tx, repo); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Scanner) StartScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.scanEnabled(ctx, "startup")
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.scanEnabled(ctx, "scheduled")
			}
		}
	}()
}

func (s *Scanner) scanEnabled(ctx context.Context, trigger string) {
	repos, err := listRepositories(ctx, s.db)
	if err != nil {
		log.Printf("list repos for scheduler: %v", err)
		return
	}
	for _, repo := range repos {
		if !repo.Enabled {
			continue
		}
		if trigger == "scheduled" {
			latest := repo.LatestScan
			if latest != nil && latest.StartedAt != "" {
				if started, err := time.Parse(timeLayout, latest.StartedAt); err == nil {
					interval := time.Duration(repo.SyncIntervalSeconds) * time.Second
					if interval <= 0 {
						interval = time.Hour
					}
					if time.Since(started) < interval {
						continue
					}
				}
			}
		}
		scanCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		_, err := s.Scan(scanCtx, repo.ID, trigger)
		cancel()
		if err != nil {
			log.Printf("scheduled scan repo %d: %v", repo.ID, err)
		}
	}
}

func scanPathsChangedSince(scanPaths []ScanPath, lastScannedAt string) bool {
	lastScannedAt = strings.TrimSpace(lastScannedAt)
	if lastScannedAt == "" {
		return true
	}
	lastScan, err := time.Parse(timeLayout, lastScannedAt)
	if err != nil {
		return true
	}
	for _, scanPath := range scanPaths {
		updatedAt := strings.TrimSpace(scanPath.UpdatedAt)
		if updatedAt == "" {
			return true
		}
		updated, err := time.Parse(timeLayout, updatedAt)
		if err != nil {
			if updatedAt >= lastScannedAt {
				return true
			}
			continue
		}
		if !updated.Before(lastScan) {
			return true
		}
	}
	return false
}

func scanPathContains(scanPath, filePath string) bool {
	scanPath = normalizeRepoPath(scanPath)
	filePath = normalizeRepoPath(filePath)
	if scanPath == "." {
		return true
	}
	return filePath == scanPath || strings.HasPrefix(filePath, strings.TrimSuffix(scanPath, "/")+"/")
}

func relativeToScanPath(scanPath, filePath string) string {
	scanPath = normalizeRepoPath(scanPath)
	filePath = normalizeRepoPath(filePath)
	if scanPath == "." {
		return filePath
	}
	return strings.TrimPrefix(filePath, strings.TrimSuffix(scanPath, "/")+"/")
}

func joinRepoPath(dir, name string) string {
	if dir == "." || dir == "" {
		return normalizeRepoPath(name)
	}
	return normalizeRepoPath(path.Join(dir, name))
}
