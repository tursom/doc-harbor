package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type mcpNoArgs struct{}

type mcpBranchesArgs struct {
	Repo  string `json:"repo" jsonschema:"仓库 slug、名称或数字 ID"`
	Limit int    `json:"limit,omitempty" jsonschema:"默认 50，最多 300"`
}

type mcpDocsArgs struct {
	Repo  string `json:"repo,omitempty" jsonschema:"省略则列出所有启用仓库"`
	Query string `json:"query,omitempty" jsonschema:"按空格拆词，对标题和路径做不区分大小写的 AND 子串匹配"`
	Limit int    `json:"limit,omitempty" jsonschema:"默认 100，最多 300"`
}

type mcpReadArgs struct {
	Repo      string `json:"repo" jsonschema:"仓库 slug、名称或数字 ID"`
	Path      string `json:"path" jsonschema:"仓库内文件相对路径"`
	Ref       string `json:"ref,omitempty" jsonschema:"分支或 commit SHA，默认 latest：文档取智能最新，代码取默认分支"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"起始行，默认 1"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"结束行（含），单次最多 400 行或 40 KB"`
}

type mcpDirArgs struct {
	Repo string `json:"repo" jsonschema:"仓库 slug、名称或数字 ID"`
	Path string `json:"path,omitempty" jsonschema:"目录相对路径，默认 ."`
	Ref  string `json:"ref,omitempty" jsonschema:"分支或 commit SHA，默认分支 HEAD"`
}

func mcpLimit(value, fallback, maximum int) (int, error) {
	if value == 0 {
		return fallback, nil
	}
	if value < 1 || value > maximum {
		return 0, fmt.Errorf("limit 必须在 1-%d 之间", maximum)
	}
	return value, nil
}

func (s *Server) mcpListRepos(ctx context.Context, _ mcpNoArgs) (string, error) {
	repos, err := s.mcpRepositories(ctx, nil)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "找到 %d 个启用仓库\n", len(repos))
	for _, repo := range repos {
		branches, err := listBranches(ctx, s.db, repo.ID)
		if err != nil {
			return "", err
		}
		paths := []string{}
		for _, scan := range repo.ScanPaths {
			if scan.Enabled {
				paths = append(paths, scan.Path)
			}
		}
		tracked := 0
		for _, branch := range branches {
			if branchTracked(repo, branch.RefName) {
				tracked++
			}
		}
		scanTime, status := "未扫描", "未扫描"
		if repo.LatestScan != nil {
			scanTime, status = repo.LatestScan.StartedAt, repo.LatestScan.Status
		}
		fmt.Fprintf(&out, "%s\t名称=%s\t默认分支=%s\t扫描目录=%s\t追踪分支数=%d\t最近扫描=%s\t状态=%s\n",
			repo.Slug, repo.Name, repo.DefaultBranch, strings.Join(paths, ","), tracked, scanTime, status)
	}
	return strings.TrimSpace(out.String()), nil
}

func (s *Server) mcpListBranches(ctx context.Context, args mcpBranchesArgs) (string, error) {
	limit, err := mcpLimit(args.Limit, 50, 300)
	if err != nil {
		return "", err
	}
	repo, err := s.resolveMCPRepository(ctx, args.Repo)
	if err != nil {
		return "", err
	}
	branches, err := listBranches(ctx, s.db, repo.ID)
	if err != nil {
		return "", err
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].CommitTime == branches[j].CommitTime {
			return branches[i].RefName < branches[j].RefName
		}
		return branches[i].CommitTime > branches[j].CommitTime
	})
	var out strings.Builder
	fmt.Fprintf(&out, "列出 %d 个分支，共 %d 个（%s）\n", min(len(branches), limit), len(branches), repo.Slug)
	for _, branch := range branches[:min(len(branches), limit)] {
		marks := []string{}
		if branch.RefName == repo.DefaultBranch {
			marks = append(marks, "默认")
		}
		if participatesLatest(repo, branch.RefName, branch.CommitTime) {
			marks = append(marks, "参与智能最新")
		}
		if branch.RefName != repo.DefaultBranch && aiBranchCandidate(repo, branch) {
			marks = append(marks, "功能分支候选")
		}
		fmt.Fprintf(&out, "%s\t%s\t%s\t%s\n", branch.RefName, branch.CommitSHA[:min(12, len(branch.CommitSHA))], branch.CommitTime, strings.Join(marks, " / "))
	}
	if len(branches) > limit {
		fmt.Fprintf(&out, "结果已截断到 %d 条，可增大 limit（最多 300）。\n", limit)
	}
	return strings.TrimSpace(out.String()), nil
}

// mcpDoc 保存智能最新索引中的实际版本，而不是把所有文档都错误地归到默认分支。
type mcpDoc struct {
	Path, Title, Modified string
	Target                mcpResolvedRef
	Entry                 treeEntry
}

func (s *Server) mcpLatestDocs(ctx context.Context, repo Repository) ([]mcpDoc, error) {
	scans, err := currentScanPaths(ctx, s.db, repo.ID)
	if err != nil {
		return nil, err
	}
	filter, params := scanPathSQLFilter("l.file_path", scans)
	params = append([]any{repo.ID}, params...)
	rows, err := s.db.QueryContext(ctx, `SELECT l.file_path, v.title, v.last_commit_time, v.branch, v.head_commit_sha, v.id, v.blob_sha, v.file_size
		FROM doc_latest l JOIN doc_versions v ON v.id = l.version_id
		WHERE l.repo_id = ? AND v.status IN ('active','renamed','moved') AND `+filter+` ORDER BY l.file_path`, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []mcpDoc
	for rows.Next() {
		var doc mcpDoc
		if err := rows.Scan(&doc.Path, &doc.Title, &doc.Modified, &doc.Target.Ref, &doc.Target.Commit, &doc.Target.VersionID, &doc.Entry.BlobSHA, &doc.Entry.Size); err != nil {
			return nil, err
		}
		doc.Entry.Path, doc.Entry.Type = doc.Path, "blob"
		if !mcpSkipEntry(repo, doc.Entry) {
			docs = append(docs, doc)
		}
	}
	return docs, rows.Err()
}

func (s *Server) mcpListDocs(ctx context.Context, args mcpDocsArgs) (string, error) {
	limit, err := mcpLimit(args.Limit, 100, 300)
	if err != nil {
		return "", err
	}
	var selectors []string
	if args.Repo != "" {
		selectors = []string{args.Repo}
	}
	repos, err := s.mcpRepositories(ctx, selectors)
	if err != nil {
		return "", err
	}
	terms := strings.Fields(strings.ToLower(args.Query))
	lines := []string{}
	total := 0
	for _, repo := range repos {
		docs, err := s.mcpLatestDocs(ctx, repo)
		if err != nil {
			return "", err
		}
		for _, doc := range docs {
			haystack := strings.ToLower(doc.Title + " " + doc.Path)
			matched := true
			for _, term := range terms {
				if !strings.Contains(haystack, term) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
			if _, err := s.mcpFileBytes(ctx, repo, doc.Entry); err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				continue
			}
			total++
			if len(lines) < limit {
				lines = append(lines, fmt.Sprintf("%s\t%s\t%s", mcpReference(repo, doc.Target, doc.Path, 0, 0), doc.Title, doc.Modified))
			}
		}
	}
	text := fmt.Sprintf("列出 %d 篇智能最新文档，共匹配 %d 篇\n%s", len(lines), total, strings.Join(lines, "\n"))
	if total > limit {
		text += fmt.Sprintf("\n结果已截断到 %d 条，请缩小 repo / query，或增大 limit（最多 300）。", limit)
	}
	return strings.TrimSpace(text), nil
}

func (s *Server) mcpRead(ctx context.Context, args mcpReadArgs) (string, error) {
	if err := mcpValidatePath(args.Path, false); err != nil {
		return "", err
	}
	if args.StartLine < 0 || args.EndLine < 0 {
		return "", fmt.Errorf("行号必须是正整数")
	}
	start := args.StartLine
	if start == 0 {
		start = 1
	}
	if args.EndLine != 0 && args.EndLine < start {
		return "", fmt.Errorf("end_line 必须大于或等于 start_line")
	}
	repo, err := s.resolveMCPRepository(ctx, args.Repo)
	if err != nil {
		return "", err
	}
	target, err := s.resolveMCPRef(ctx, repo, args.Ref, args.Path)
	if err != nil {
		return "", err
	}
	// 从父目录树取元数据，先检查大小再读 blob，避免 show 把大文件读入内存。
	entries, err := s.mcpTree(ctx, repo, target.Commit, pathDir(args.Path), false)
	if err != nil {
		return "", fmt.Errorf("文件或父目录不存在：%s", args.Path)
	}
	var entry *treeEntry
	for i := range entries {
		if entries[i].Path == args.Path {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("文件不存在：%s", args.Path)
	}
	data, err := s.mcpFileBytes(ctx, repo, *entry)
	if err != nil {
		return "", err
	}
	lines := mcpLines(data)
	if len(lines) == 0 {
		return "读取空文件，共 0 行\n== " + mcpReference(repo, target, args.Path, 0, 0), nil
	}
	if start > len(lines) {
		return "", fmt.Errorf("start_line=%d 超出文件范围，可选 1-%d", start, len(lines))
	}
	end := len(lines)
	if args.EndLine > 0 {
		end = min(end, args.EndLine)
	}
	end = min(end, start+399)
	var body strings.Builder
	last, cutLine := start-1, false
	// 摘要、引用键和续读提示预留空间，整个工具文本保持在 40 KiB 内。
	budget := 40*1024 - len(mcpReference(repo, target, args.Path, start, end)) - 512
	for i := start; i <= end; i++ {
		line := fmt.Sprintf("%5d\t%s\n", i, lines[i-1])
		if body.Len()+len(line) > budget {
			if last < start {
				line = mcpTruncateBytes(line, max(0, budget-40)) + "…（本行已截断）\n"
				body.WriteString(line)
				last, cutLine = i, true
			}
			break
		}
		body.WriteString(line)
		last = i
	}
	text := fmt.Sprintf("读取第 %d-%d 行，共 %d 行\n== %s\n%s", start, last, len(lines), mcpReference(repo, target, args.Path, start, last), body.String())
	if cutLine {
		text += "单行超过 40 KB，本行仅返回前缀；可用 search_code 按关键词定位本行片段。\n"
	}
	if last < len(lines) {
		text += fmt.Sprintf("还有 %d 行，用 start_line=%d 继续。", len(lines)-last, last+1)
	}
	// 只移除结构性的最后一个换行，保留原文行尾空格和空白行的制表符行号分隔。
	return strings.TrimSuffix(text, "\n"), nil
}

func pathDir(filePath string) string {
	if i := strings.LastIndex(filePath, "/"); i >= 0 {
		return filePath[:i]
	}
	return "."
}

func mcpTruncateBytes(value string, size int) string {
	if len(value) <= size {
		return value
	}
	for size > 0 && !utf8.RuneStart(value[size]) {
		size--
	}
	return value[:size]
}

func (s *Server) mcpListDir(ctx context.Context, args mcpDirArgs) (string, error) {
	if args.Path == "" {
		args.Path = "."
	}
	args.Path = strings.TrimSuffix(args.Path, "/")
	if err := mcpValidatePath(args.Path, true); err != nil {
		return "", err
	}
	repo, err := s.resolveMCPRepository(ctx, args.Repo)
	if err != nil {
		return "", err
	}
	target, err := s.resolveMCPRef(ctx, repo, args.Ref, "")
	if err != nil {
		return "", err
	}
	entries, err := s.mcpTree(ctx, repo, target.Commit, args.Path, false)
	if err != nil {
		return "", fmt.Errorf("目录不存在：%s（%w）", args.Path, err)
	}
	var lines []string
	skipped, truncated := 0, false
	for _, entry := range entries {
		if entry.Type == "tree" && mcpValidatePath(entry.Path, false) == nil && !aiShouldSkipPath(entry.Path, AIIndexConfig{}) {
			lines = append(lines, fmt.Sprintf("dir\t%s\t-\t%s", entry.Path, mcpReference(repo, target, entry.Path, 0, 0)))
		} else if mcpSkipEntry(repo, entry) {
			skipped++
			continue
		} else {
			if _, err := s.mcpFileBytes(ctx, repo, entry); err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				skipped++
				continue
			}
			lines = append(lines, fmt.Sprintf("file\t%s\t%d\t%s", entry.Path, entry.Size, mcpReference(repo, target, entry.Path, 0, 0)))
		}
		if len(lines) > 300 {
			lines, truncated = lines[:300], true
			break
		}
	}
	text := fmt.Sprintf("列出 %d 项（%s@%s:%s），跳过 %d 项\n%s", len(lines), repo.Slug, target.Ref, args.Path, skipped, strings.Join(lines, "\n"))
	if truncated {
		text += "\n结果已截断到 300 条，请指定更深的 path 浏览子目录。"
	}
	return strings.TrimSpace(text), nil
}
