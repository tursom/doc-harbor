package app

import (
	"context"
	"database/sql"
	"log"
	"path"
	"strings"
	"time"
)

// scanRepository 只执行扫描；创建任务、互斥、取消和最终持久化统一由任务生命周期管理。
// Git 的所有耗时工作均在分支写事务外准备，避免网络或子进程持有 SQLite 写连接。
func (s *Scanner) scanRepository(ctx context.Context, repo Repository, run ScanRun, force bool) (result ScanRun, resultErr error) {
	run.Status = "success"
	details := map[string]any{}
	started := time.Now()
	defer func() {
		details["elapsed_ms"] = time.Since(started).Milliseconds()
		details["force"] = force
		run.DetailJSON = humanScanDetail(details)
		result = run
	}()

	if !repo.Enabled {
		run.Status = "failed"
		run.ErrorMessage = "repository disabled"
		return run, errBadRequest("repository disabled")
	}
	s.reportProgress(ctx, run.ID, ScanProgress{Stage: "fetching"})
	fetchStarted := time.Now()
	if err := s.git.ensureMirror(ctx, repo); err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return run, err
	}
	details["fetch_ms"] = time.Since(fetchStarted).Milliseconds()

	repoPath := s.git.repoPath(repo.ID)
	refs, err := s.git.branches(ctx, repoPath)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return run, err
	}

	scanPaths, err := listScanPaths(ctx, s.reader(), repo.ID, true)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return run, err
	}
	if len(scanPaths) == 0 {
		scanPaths = []ScanPath{{RepoID: repo.ID, Path: ".", Enabled: true}}
	}

	branchStats := map[string]any{}
	// 上轮未完整成功时仍修复最新视图；正常无变化刷新不反复重写全部 doc_latest。
	needsLatest := force || repo.LatestScan == nil || repo.LatestScan.Status != "success"
	latestPublished := false
	for _, ref := range refs {
		if !branchTracked(repo, ref.Name) {
			continue
		}
		run.BranchCount++
		stat, err := s.scanPreparedBranch(ctx, repo, ref, scanPaths, force, run.ID)
		latestPublished = latestPublished || stat.LatestPublished
		branchStats[ref.Name] = stat
		// 部分失败的分支仍可能提交了文件索引，必须先累计统计，不能被错误分支跳过。
		run.FileCount += stat.Files
		run.SkippedCount += stat.Skipped
		run.ErrorCount += stat.Errors
		s.reportCommitted(ctx, run)
		if err != nil || stat.Errors > 0 {
			run.Status = "partial_success"
			run.ErrorMessage = strings.TrimSpace(run.ErrorMessage + "\n" + ref.Name + ": " + stat.Error)
			if err != nil && !strings.Contains(stat.Error, err.Error()) {
				run.ErrorMessage += "\n" + err.Error()
			}
		}
		if ctx.Err() != nil {
			run.Status = "failed"
			run.ErrorMessage = strings.TrimSpace(run.ErrorMessage + "\n扫描已中断: " + ctx.Err().Error())
			details["branches"] = branchStats
			return run, ctx.Err()
		}
	}
	details["branches"] = branchStats

	s.reportProgress(ctx, run.ID, ScanProgress{Stage: "latest"})
	latestStarted := time.Now()
	if err := s.recomputeLatestTx(ctx, repo, refs, scanPaths, needsLatest && !latestPublished); err != nil {
		run.Status = "partial_success"
		run.ErrorCount++
		run.ErrorMessage = strings.TrimSpace(run.ErrorMessage + "\n重建最新文档索引: " + err.Error())
		details["latest_error"] = err.Error()
		return run, err
	}
	details["latest_ms"] = time.Since(latestStarted).Milliseconds()
	if run.BranchCount == 0 {
		run.Status = "failed"
		run.ErrorMessage = "no tracked branches found"
		return run, errBadRequest("no tracked branches found")
	}
	if run.ErrorCount > 0 && run.Status == "success" {
		run.Status = "partial_success"
	}
	return run, nil
}

type branchScanStat struct {
	Files         int               `json:"files"`
	Skipped       int               `json:"skipped"`
	Errors        int               `json:"errors"`
	Head          string            `json:"head"`
	SkippedByHead bool              `json:"skipped_by_head"`
	Error         string            `json:"error,omitempty"`
	ErrorDetails  []branchScanError `json:"error_details,omitempty"`
	// Files 仅统计已提交的新增或更新；Reused 表示直接复用的当前文件。
	Reused          int              `json:"reused_files"`
	Mode            string           `json:"mode"`
	HistoryEvents   int              `json:"history_events"`
	TimingsMS       map[string]int64 `json:"timings_ms,omitempty"`
	LatestPublished bool             `json:"-"`
}

// branchScanError 保留每个失败的阶段、文件或目录及原始错误，避免只有计数却无法排障。
type branchScanError struct {
	Stage   string `json:"stage"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (stat *branchScanStat) addError(stage, filePath string, err error) {
	stat.Errors++
	stat.ErrorDetails = append(stat.ErrorDetails, branchScanError{Stage: stage, Path: filePath, Message: err.Error()})
	if stat.Error == "" {
		stat.Error = strings.TrimSpace(stage+" "+filePath) + ": " + err.Error()
	}
}

// applyPathHistory 只应用已准备好的历史，不在写事务中启动 Git。
func (s *Scanner) applyPathHistory(ctx context.Context, tx *sql.Tx, repo Repository, ref gitRef, events []historyPathEvent) error {
	// 同一路径可能在完整历史中出现几百次；事务内缓存身份，避免重复 SQL 查询。
	identities := make(map[string]int64)
	lookup := func(filePath string) (int64, error) {
		if id, ok := identities[filePath]; ok {
			return id, nil
		}
		id, err := findDocumentForBranchPath(ctx, tx, repo.ID, ref.Name, filePath)
		if err == nil {
			identities[filePath] = id
		}
		return id, err
	}
	for _, event := range events {
		filePath := event.NewPath
		if filePath == "" {
			filePath = event.OldPath
		}
		if filePath == "" {
			continue
		}
		docID, err := lookup(filePath)
		if err != nil {
			return err
		}
		if event.OldPath != "" {
			oldDocID, oldErr := lookup(event.OldPath)
			if oldErr != nil {
				return oldErr
			}
			if docID == 0 {
				docID = oldDocID
			} else if oldDocID != 0 && oldDocID != docID {
				if err := reassignPathEvents(ctx, tx, repo.ID, ref.Name, oldDocID, docID); err != nil {
					return err
				}
			}
		}
		if docID == 0 {
			docID, err = findOrCreateDocument(ctx, tx, repo.ID, event.ScanPath, buildDocKey(event.ScanPath, filePath), ref.Name, event.CommitSHA, baseName(filePath), filePath)
			if err != nil {
				return err
			}
		}
		identities[filePath] = docID
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
	if fromDocID == toDocID {
		return nil
	}
	// 旧文档与目标文档可能已经各自保存同一事件。先删除完全等价的旧副本，
	// 再迁移剩余事件，既保留历史又避免重扫触发唯一键冲突而永远无法推进检查点。
	if _, err := tx.ExecContext(ctx, `DELETE FROM doc_path_events WHERE id IN (
		SELECT old.id FROM doc_path_events old JOIN doc_path_events target
		ON target.repo_id = old.repo_id AND target.branch = old.branch
		AND target.event_type = old.event_type AND target.old_path = old.old_path
		AND target.new_path = old.new_path AND target.commit_sha = old.commit_sha
		WHERE old.repo_id = ? AND old.branch = ? AND old.document_id = ? AND target.document_id = ?
	)`, repoID, branch, fromDocID, toDocID); err != nil {
		return err
	}
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
	// 超限可预览文件仍可下载，但必须先满足上面的目录和 glob 规则。
	if entry.Size > repo.MaxFileSizeBytes && isPreviewable(filePath) {
		return false
	}
	if entry.Size > repo.MaxFileSizeBytes*20 {
		return true
	}
	return false
}

func (s *Scanner) recomputeLatestTx(ctx context.Context, repo Repository, refs []gitRef, paths []ScanPath, rebuild bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	// 跳过的分支也必须在最终发布时核对规则，防止用任务开始时的旧优先级覆盖新配置。
	if err := checkScanRulesTx(ctx, tx, repo, scanRulesFingerprint(repo, paths)); err != nil {
		return err
	}
	// 即使 HEAD 不变，分支可能被移除或不再参与最新视图；只更新实际变化的标志。
	placeholders := make([]string, 0, len(refs))
	args := []any{repo.ID}
	for _, ref := range refs {
		participates := boolInt(branchTracked(repo, ref.Name) && participatesLatest(repo, ref.Name, ref.CommitTime))
		result, err := tx.ExecContext(ctx, `UPDATE doc_versions SET participates_latest=?
			WHERE repo_id=? AND branch=? AND participates_latest<>?`, participates, repo.ID, ref.Name, participates)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		rebuild = rebuild || changed > 0
		placeholders = append(placeholders, "?")
		args = append(args, ref.Name)
	}
	if len(placeholders) > 0 {
		result, err := tx.ExecContext(ctx, `UPDATE doc_versions SET participates_latest=0
			WHERE repo_id=? AND participates_latest<>0 AND branch NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		rebuild = rebuild || changed > 0
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE doc_versions SET participates_latest=0 WHERE repo_id=? AND participates_latest<>0`, repo.ID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		rebuild = rebuild || changed > 0
	}
	if rebuild {
		if err := recomputeLatest(ctx, tx, repo); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Scanner) StartScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	// 调度器同时服从调用方和 Scanner 根生命周期，Close 后不再访问已关闭的数据库。
	schedulerCtx, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(ctx, cancel)
	go func() {
		defer cancel()
		defer stop()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.scanEnabled(schedulerCtx, "startup")
		for {
			select {
			case <-schedulerCtx.Done():
				return
			case <-ticker.C:
				s.scanEnabled(schedulerCtx, "scheduled")
			}
		}
	}()
}

func (s *Scanner) scanEnabled(ctx context.Context, trigger string) {
	repos, err := listRepositories(ctx, s.reader())
	if err != nil {
		log.Printf("list repos for scheduler: %v", err)
		return
	}
	for _, repo := range repos {
		// 关闭自动拉取的仓库同时跳过启动扫描和周期扫描，手动扫描及 Webhook
		// 仍直接调用 Scan，不受此设置影响，也不会隐藏已经索引的文档。
		if !repo.Enabled || repo.SyncIntervalSeconds == -1 {
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
