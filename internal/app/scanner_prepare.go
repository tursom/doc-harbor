package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// scanRulesFingerprint 只包含影响索引和最新版本选择的规则，不使用秒级更新时间猜测变化。
// 版本号使旧实现写出的成功点在升级后先完整校验一次，避免继承历史漏扫。
func scanRulesFingerprint(repo Repository, paths []ScanPath) string {
	type rule struct {
		Path    string
		Include []string
		Exclude []string
	}
	rules := make([]rule, 0, len(paths))
	for _, p := range paths {
		if p.Enabled {
			rules = append(rules, rule{normalizeRepoPath(p.Path), p.IncludeGlobs, p.ExcludeGlobs})
		}
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Path < rules[j].Path })
	data, _ := json.Marshal(struct {
		Version                             int
		URL                                 string
		MaxSize                             int64
		Rules                               []rule
		Include, Exclude, Priority, Tracked []string
		StaleDays                           int
	}{2, repo.RepoURL, repo.MaxFileSizeBytes, rules, repo.LatestIncludeBranches,
		repo.LatestExcludeBranches, repo.BranchPriority, repo.TrackedBranches, repo.StaleBranchDays})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type preparedScanEntry struct {
	entry treeEntry
	scope ScanPath
}

// scanPreparedBranch 分成准备和提交两段。准备阶段不占用写事务，失败的文件仍保留旧版本。
// 事务中只做索引写入、路径事件归属及成功检查点更新，不调用 Git 或外部网络。
func (s *Scanner) scanPreparedBranch(ctx context.Context, repo Repository, ref gitRef, paths []ScanPath, force bool, runID int64) (stat branchScanStat, resultErr error) {
	stat.Head, stat.Mode = ref.CommitSHA, "full"
	stat.TimingsMS = make(map[string]int64)
	defer func() {
		if resultErr != nil {
			stat.addError("分支扫描", "", resultErr)
		}
	}()
	oldRef, err := getRepoRef(ctx, s.reader(), repo.ID, "branch", ref.Name)
	if err != nil {
		return stat, fmt.Errorf("读取上次分支扫描: %w", err)
	}
	fingerprint := scanRulesFingerprint(repo, paths)
	previousSuccess := oldRef != nil && oldRef.LastScannedAt != "" && oldRef.ScanConfigHash == fingerprint
	if !force && previousSuccess && oldRef.CommitSHA == ref.CommitSHA {
		stat.SkippedByHead, stat.Mode = true, "unchanged"
		return stat, nil
	}
	repoPath := s.git.repoPath(repo.ID)
	hasBase := false
	if oldRef != nil && oldRef.CommitSHA != "" {
		hasBase, err = s.git.hasCommit(ctx, repoPath, oldRef.CommitSHA)
		if err != nil {
			return stat, fmt.Errorf("核对上次提交对象: %w", err)
		}
	}
	ancestor := false
	if previousSuccess && hasBase && !force {
		ancestor, err = s.git.isAncestor(ctx, repoPath, oldRef.CommitSHA, ref.CommitSHA)
		if err != nil {
			return stat, fmt.Errorf("核对提交连续性: %w", err)
		}
	}
	incremental := previousSuccess && ancestor && !force
	if incremental {
		stat.Mode = "incremental"
	}
	// 在耗时准备前失效旧成功标记。即使准备被取消或事务失败，下次也会重试，不能按旧 HEAD 跳过。
	if oldRef != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE repo_refs SET last_scanned_at = '' WHERE id = ?`, oldRef.ID); err != nil {
			return stat, fmt.Errorf("清除上次分支扫描成功标记: %w", err)
		}
	}
	existing, err := existingBranchVersions(ctx, s.reader(), repo.ID, ref.Name)
	if err != nil {
		return stat, fmt.Errorf("读取已有分支索引: %w", err)
	}
	report := func(stage string, processed, total int) {
		s.reportProgress(ctx, runID, ScanProgress{Stage: stage, Branch: ref.Name, ProcessedFiles: processed, TotalFiles: total})
	}
	report("enumerating", 0, 0)
	started := time.Now()
	entries := make([]preparedScanEntry, 0)
	currentPaths := make(map[string]bool)
	inventoryComplete := true
	for _, scope := range paths {
		files, err := s.git.lsTree(ctx, repoPath, ref.CommitSHA, scope.Path)
		if err != nil {
			stat.addError("枚举目录", scope.Path, err)
			inventoryComplete = false
			continue
		}
		for _, entry := range files {
			if entry.Type != "blob" {
				continue
			}
			if s.shouldSkip(entry, repo, scope) {
				stat.Skipped++
				continue
			}
			// 重叠扫描目录只处理一次，同一路径保留第一条命中的规则。
			if !currentPaths[entry.Path] {
				entries = append(entries, preparedScanEntry{entry, scope})
				currentPaths[entry.Path] = true
			}
		}
	}
	stat.TimingsMS["enumerating"] = time.Since(started).Milliseconds()
	if err := ctx.Err(); err != nil {
		return stat, err
	}
	filePaths := make([]string, len(entries))
	for i, item := range entries {
		filePaths[i] = item.entry.Path
	}
	report("metadata", 0, len(entries))
	started = time.Now()
	lastCommits, err := s.git.lastCommitsForPaths(ctx, repoPath, ref.CommitSHA, filePaths)
	if errors.Is(err, errGitBatchMergeHistory) {
		// 合并历史必须遵循逐路径 TREESAME 规则，不能直接套用多路径日志的最近提交。
		lastCommits, err = s.git.lastCommitsForMergePaths(ctx, repoPath, ref.CommitSHA, filePaths)
	}
	stat.TimingsMS["metadata"] = time.Since(started).Milliseconds()
	if err != nil {
		stat.addError("读取文件版本", "", err)
		return stat, nil
	}
	// diff 只用于文档重命名身份匹配。是否需要更新文件还比较批量最近提交，覆盖内容修改后又还原的提交。
	renames := make(map[string]CommitFileChange)
	if hasBase && oldRef.CommitSHA != ref.CommitSHA {
		for _, scope := range paths {
			changes, err := s.git.diffNameStatus(ctx, repoPath, oldRef.CommitSHA, ref.CommitSHA, scope.Path)
			if err != nil {
				// 对象存在时，diff 失败不能按“没有重命名”继续并写成功点，否则会丢失文档身份。
				stat.addError("读取重命名差异", scope.Path, err)
				return stat, nil
			}
			for _, change := range changes {
				// Copy 必须创建独立身份，只有 Rename 才能继承源文档。
				if strings.HasPrefix(change.Status, "R") && change.OldPath != "" && change.NewPath != "" {
					renames[change.NewPath] = change
				}
			}
		}
	}
	report("content", 0, len(entries))
	started = time.Now()
	versions := make([]DocVersion, 0, len(entries))
	reusedIDs := make([]int64, 0)
	successfulPaths := make(map[string]bool)
	// 每批最多 8MiB/64 个对象，只缓存标题，避免把几万个正文同时驻留内存。
	const maxBatchBytes = 8 * 1024 * 1024
	batch := make([]preparedScanEntry, 0, 64)
	batchBytes, processed := int64(0), 0
	titles := make(map[string]string)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		blobs := make([]string, len(batch))
		for i, item := range batch {
			blobs[i] = item.entry.BlobSHA
		}
		contents, readErr := s.git.catFiles(ctx, repoPath, blobs)
		for _, item := range batch {
			if readErr != nil {
				stat.addError("读取文件版本", item.entry.Path, readErr)
				continue
			}
			contentsForBlob, ok := contents[item.entry.BlobSHA]
			if !ok {
				stat.addError("读取文件版本", item.entry.Path, fmt.Errorf("Git 批量读取未返回对象 %s", item.entry.BlobSHA))
				continue
			}
			// 标题通常是正文字符串的切片；单独复制后，批次结束即可释放整篇正文。
			heading := strings.Clone(titleFromMarkdown(contentsForBlob, ""))
			titles[item.entry.BlobSHA] = heading
			versions = append(versions, preparedVersion(repo, ref, item, lastCommits[item.entry.Path], renames[item.entry.Path], heading))
		}
		batch = batch[:0]
		batchBytes = 0
	}
	for _, item := range entries {
		if err := ctx.Err(); err != nil {
			return stat, err
		}
		entry := item.entry
		last, ok := lastCommits[entry.Path]
		if !ok {
			stat.addError("读取文件版本", entry.Path, fmt.Errorf("Git 历史中缺少文件最近提交"))
			continue
		}
		old, hadOld := existing[entry.Path]
		if incremental && hadOld && old.Status != "deleted" && old.BlobSHA == entry.BlobSHA && old.LastCommitSHA == last.SHA {
			stat.Reused++
			reusedIDs = append(reusedIDs, old.ID)
			successfulPaths[entry.Path] = true
			processed++
			if processed%200 == 0 {
				report("content", processed, len(entries))
			}
			continue
		}
		if isMarkdown(entry.Path) && entry.Size <= repo.MaxFileSizeBytes {
			if hadOld && old.Status != "deleted" && old.BlobSHA == entry.BlobSHA && !force {
				v := preparedVersion(repo, ref, item, last, renames[entry.Path], "")
				v.Title = old.Title
				versions = append(versions, v)
			} else if title, found := titles[entry.BlobSHA]; found {
				versions = append(versions, preparedVersion(repo, ref, item, last, renames[entry.Path], title))
			} else {
				if len(batch) >= 64 || (batchBytes+entry.Size > maxBatchBytes && len(batch) > 0) {
					flush()
				}
				batch = append(batch, item)
				batchBytes += entry.Size
			}
		} else {
			versions = append(versions, preparedVersion(repo, ref, item, last, renames[entry.Path], ""))
		}
		processed++
		if processed%200 == 0 {
			report("content", processed, len(entries))
		}
	}
	flush()
	sort.SliceStable(versions, func(i, j int) bool { return versions[i].FilePath < versions[j].FilePath })
	stat.TimingsMS["content"] = time.Since(started).Milliseconds()
	report("history", 0, len(entries))
	started = time.Now()
	events := make([]historyPathEvent, 0)
	historyBase := ""
	if incremental {
		historyBase = oldRef.CommitSHA
	}
	for _, scope := range paths {
		history, historyErr := s.git.pathHistoryRange(ctx, repoPath, ref.Name, scope.Path, historyBase, ref.CommitSHA)
		if historyErr != nil {
			stat.addError("回填路径历史", scope.Path, historyErr)
			continue
		}
		events = append(events, history...)
	}
	stat.HistoryEvents = len(events)
	stat.TimingsMS["history"] = time.Since(started).Milliseconds()
	if err := ctx.Err(); err != nil {
		return stat, err
	}
	report("writing", processed, len(entries))
	started = time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stat, fmt.Errorf("开始索引事务: %w", err)
	}
	defer rollback(tx)
	if err := checkScanRulesTx(ctx, tx, repo, fingerprint); err != nil {
		return stat, err
	}
	// 先为本轮重命名预留源身份，再处理文件，避免“a 改名 b，同时新建 a”受到遍历顺序影响。
	reserved := make(map[int64]string)
	renameIDs := make(map[string]int64)
	byDocument := make(map[int64]DocVersion, len(existing))
	for _, version := range existing {
		byDocument[version.DocumentID] = version
	}
	for target, rename := range renames {
		id := int64(0)
		if source, ok := existing[rename.OldPath]; ok {
			id = source.DocumentID
		} else {
			// 旧版本可能已在上次部分成功中移动；原始文档键仍可恢复身份。
			id, err = findDocumentForBranchPath(ctx, tx, repo.ID, ref.Name, rename.OldPath)
			if err != nil {
				return stat, err
			}
			if previous, ok := byDocument[id]; ok && previous.FilePath != target && currentPaths[previous.FilePath] {
				id = 0 // 该身份仍被本轮其它实际文件使用，不能覆盖。
			}
		}
		if id != 0 {
			reserved[id], renameIDs[target] = target, id
		}
	}
	committedFiles := 0
	for _, v := range versions {
		docID := renameIDs[v.FilePath]
		if docID == 0 {
			if previous, ok := existing[v.FilePath]; ok {
				if target, assigned := reserved[previous.DocumentID]; !assigned || target == v.FilePath {
					docID = previous.DocumentID
				}
			}
		}
		if err == nil && docID == 0 {
			docID, err = findOrCreateDocument(ctx, tx, repo.ID, v.ScanPath, buildDocKey(v.ScanPath, v.FilePath), ref.Name, ref.CommitSHA, v.Title, v.FilePath, reserved)
		}
		if err != nil {
			stat.addError("创建文档索引", v.FilePath, err)
			err = nil
			continue
		}
		v.DocumentID = docID
		if _, err := upsertDocVersion(ctx, tx, v); err != nil {
			stat.addError("写入文件版本", v.FilePath, err)
			continue
		}
		// 净 diff 只匹配身份；历史事件由真实 Git 历史提供，不能把最近内容修改提交伪造成重命名提交。
		successfulPaths[v.FilePath] = true
		committedFiles++
	}
	if err := s.applyPathHistory(ctx, tx, repo, ref, events); err != nil {
		stat.addError("回填路径历史", "", err)
	}
	if inventoryComplete {
		// 目标读取/写入失败时保留旧源版本，等待下次重试；不能用“源路径不在新树”删除它。
		for target, rename := range renames {
			if currentPaths[target] && !successfulPaths[target] {
				currentPaths[rename.OldPath] = true
			}
		}
		if _, err := markDeletedVersions(ctx, tx, repo.ID, ref.Name, ref.CommitSHA, ref.CommitTime, currentPaths); err != nil {
			stat.addError("标记已删除文件", "", err)
		}
	}
	// 只推进已确认复用成功的版本。失败文件仍指向旧 blob 和旧 HEAD，保持引用快照真实。
	for offset := 0; offset < len(reusedIDs); offset += 200 {
		end := min(offset+200, len(reusedIDs))
		placeholders := make([]string, end-offset)
		args := []any{ref.CommitSHA, boolInt(participatesLatest(repo, ref.Name, ref.CommitTime))}
		for i, id := range reusedIDs[offset:end] {
			placeholders[i] = "?"
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE doc_versions SET head_commit_sha=?,participates_latest=? WHERE id IN (`+
			strings.Join(placeholders, ",")+`)`, args...); err != nil {
			return stat, err
		}
	}
	checkpoint := RepoRef{RepoID: repo.ID, RefType: "branch", RefName: ref.Name, CommitSHA: ref.CommitSHA, CommitTime: ref.CommitTime}
	if stat.Errors == 0 && inventoryComplete {
		checkpoint.LastScannedAt, checkpoint.ScanConfigHash = nowString(), fingerprint
	} else if oldRef != nil {
		checkpoint = *oldRef
		checkpoint.LastScannedAt = ""
	}
	if err := upsertRepoRef(ctx, tx, checkpoint); err != nil {
		return stat, fmt.Errorf("保存分支扫描状态: %w", err)
	}
	// 分支版本会原地更新，必须同时发布最新视图；否则分支提交后取消会留下旧目录与新内容混合。
	if err := recomputeLatest(ctx, tx, repo); err != nil {
		return stat, fmt.Errorf("重建最新文档索引: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return stat, fmt.Errorf("提交索引事务: %w", err)
	}
	stat.Files = committedFiles
	stat.LatestPublished = true
	stat.TimingsMS["writing"] = time.Since(started).Milliseconds()
	return stat, nil
}

func preparedVersion(repo Repository, ref gitRef, item preparedScanEntry, last lastCommit, rename CommitFileChange, heading string) DocVersion {
	entry := item.entry
	title := heading
	if title == "" {
		title = baseName(entry.Path)
	}
	status, previousPath := "active", ""
	if rename.OldPath != "" && rename.NewPath == entry.Path {
		previousPath = rename.OldPath
		status = pathEventType(previousPath, entry.Path)
	}
	return DocVersion{RepoID: repo.ID, Branch: ref.Name, HeadCommitSHA: ref.CommitSHA, ScanPath: item.scope.Path,
		FilePath: entry.Path, PreviousPath: previousPath, DirPath: dirName(entry.Path), FileName: baseName(entry.Path),
		Extension: extension(entry.Path), MimeType: mimeType(entry.Path), FileSize: entry.Size, BlobSHA: entry.BlobSHA,
		Status: status, Title: title, Previewable: isPreviewable(entry.Path) && entry.Size <= repo.MaxFileSizeBytes,
		DownloadEnabled: true, LastCommitSHA: last.SHA, LastCommitTime: last.Time, RenameScore: rename.RenameScore,
		ParticipatesLatest: participatesLatest(repo, ref.Name, ref.CommitTime)}
}

// checkScanRulesTx 在同一写事务内核对规则，避免准备期间用户修改范围后仍发布旧范围的成功检查点。
func checkScanRulesTx(ctx context.Context, tx *sql.Tx, repo Repository, expected string) error {
	var include, exclude, priority, tracked string
	var enabled int
	if err := tx.QueryRowContext(ctx, `SELECT repo_url,max_file_size_bytes,latest_include_branches,
		latest_exclude_branches,branch_priority,tracked_branches,stale_branch_days,enabled FROM repositories WHERE id = ?`, repo.ID).
		Scan(&repo.RepoURL, &repo.MaxFileSizeBytes, &include, &exclude, &priority, &tracked, &repo.StaleBranchDays, &enabled); err != nil {
		return err
	}
	if enabled == 0 {
		return errConflict("扫描期间仓库已停用，请重新扫描")
	}
	repo.LatestIncludeBranches = decodeStringList(include, nil)
	repo.LatestExcludeBranches = decodeStringList(exclude, nil)
	repo.BranchPriority = decodeStringList(priority, nil)
	repo.TrackedBranches = decodeStringList(tracked, nil)
	rows, err := tx.QueryContext(ctx, `SELECT path,include_globs,exclude_globs FROM repo_scan_paths WHERE repo_id = ? AND enabled = 1 ORDER BY path`, repo.ID)
	if err != nil {
		return err
	}
	paths := []ScanPath{}
	for rows.Next() {
		var p ScanPath
		var inc, exc string
		if err := rows.Scan(&p.Path, &inc, &exc); err != nil {
			rows.Close()
			return err
		}
		p.Enabled = true
		p.IncludeGlobs, p.ExcludeGlobs = decodeStringList(inc, nil), decodeStringList(exc, nil)
		paths = append(paths, p)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return readErr
	}
	if len(paths) == 0 {
		paths = []ScanPath{{Path: ".", Enabled: true}}
	}
	if scanRulesFingerprint(repo, paths) != expected {
		return errConflict("扫描期间规则发生变化，请重新扫描")
	}
	return nil
}
