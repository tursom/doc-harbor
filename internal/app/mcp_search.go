package app

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type mcpSearchDocsArgs struct {
	Query string   `json:"query" jsonschema:"搜索词，默认不区分大小写的字面匹配"`
	Repos []string `json:"repos,omitempty" jsonschema:"仓库 slug、名称或数字 ID 列表，省略则全部启用仓库"`
	Regex bool     `json:"regex,omitempty" jsonschema:"启用 Git 扩展正则表达式，默认 false"`
	Limit int      `json:"limit,omitempty" jsonschema:"匹配行数上限，默认 40，最多 300"`
}

type mcpSearchCodeArgs struct {
	Query    string   `json:"query" jsonschema:"搜索词，默认不区分大小写的字面匹配"`
	Repos    []string `json:"repos,omitempty" jsonschema:"仓库 slug、名称或数字 ID 列表，省略则全部启用仓库"`
	Ref      string   `json:"ref,omitempty" jsonschema:"分支名或 commit SHA，默认各仓库默认分支"`
	PathGlob string   `json:"path_glob,omitempty" jsonschema:"路径 glob，例如 **/*.go、internal/**；省略则整棵树"`
	Regex    bool     `json:"regex,omitempty" jsonschema:"启用 Git 扩展正则表达式，默认 false"`
	Limit    int      `json:"limit,omitempty" jsonschema:"匹配行数上限，默认 60，最多 300"`
}

type mcpSearchFile struct {
	Entry  treeEntry
	Target mcpResolvedRef
	Title  string
}

type mcpSearchHit struct {
	Repo      Repository
	File      mcpSearchFile
	Lines     map[int]string
	LineCount int
	Matches   []int
}

func (s *Server) mcpSearchDocs(ctx context.Context, args mcpSearchDocsArgs) (string, error) {
	limit, err := mcpLimit(args.Limit, 40, 300)
	if err != nil {
		return "", err
	}
	return s.mcpSearch(ctx, mcpSearchCodeArgs{Query: args.Query, Repos: args.Repos, Regex: args.Regex}, limit, true)
}

func (s *Server) mcpSearchCode(ctx context.Context, args mcpSearchCodeArgs) (string, error) {
	limit, err := mcpLimit(args.Limit, 60, 300)
	if err != nil {
		return "", err
	}
	if args.PathGlob != "" {
		if err := mcpValidatePath(args.PathGlob, false); err != nil {
			return "", err
		}
		for _, segment := range strings.Split(args.PathGlob, "/") {
			if _, err := path.Match(segment, ""); err != nil {
				return "", fmt.Errorf("path_glob 无效：%w", err)
			}
		}
	}
	return s.mcpSearch(ctx, args, limit, false)
}

// mcpSearch 并发搜索仓库，但保持输出顺序稳定。每个仓库最多收集 limit+1 条匹配，
// 额外一条只用于判断是否截断，避免恰好等于 limit 时错误地宣称还有结果。
func (s *Server) mcpSearch(ctx context.Context, args mcpSearchCodeArgs, limit int, docsOnly bool) (string, error) {
	if strings.TrimSpace(args.Query) == "" || strings.ContainsRune(args.Query, 0) {
		return "", fmt.Errorf("query 不能为空或包含 NUL 字符")
	}
	repos, err := s.mcpRepositories(ctx, args.Repos)
	if err != nil {
		return "", err
	}
	type result struct {
		hits []mcpSearchHit
		err  error
	}
	results := make([]result, len(repos))
	var workers sync.WaitGroup
	// 限制同时启动的 Git 子进程数；等待也计入整个工具的 10 秒预算。
	semaphore := make(chan struct{}, 4)
	for i, repo := range repos {
		workers.Add(1)
		go func(i int, repo Repository) {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			files, err := s.mcpSearchFiles(ctx, repo, args, docsOnly)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].hits, results[i].err = s.mcpGrepFiles(ctx, repo, files, args.Query, args.Regex, limit+1)
		}(i, repo)
	}
	workers.Wait()
	var hits []mcpSearchHit
	var warnings []string
	succeeded := 0
	for i, result := range results {
		if result.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s：%s", repos[i].Slug, result.err))
		} else {
			succeeded++
			hits = append(hits, result.hits...)
		}
	}
	if len(warnings) > 0 && succeeded == 0 {
		return "", fmt.Errorf("搜索失败：%s", strings.Join(warnings, "；"))
	}
	var body strings.Builder
	count, fileCount, truncated := 0, 0, false
	for _, hit := range hits {
		remaining := limit - count
		if remaining <= 0 {
			truncated = true
			break
		}
		matches := hit.Matches
		if len(matches) > remaining {
			matches, truncated = matches[:remaining], true
		}
		if len(matches) == 0 {
			continue
		}
		count += len(matches)
		fileCount++
		seen, matched := map[int]bool{}, map[int]bool{}
		for _, line := range matches {
			matched[line] = true
			for n := max(1, line-1); n <= min(hit.LineCount, line+1); n++ {
				seen[n] = true
			}
		}
		var numbers []int
		for number := range seen {
			numbers = append(numbers, number)
		}
		sort.Ints(numbers)
		fmt.Fprintf(&body, "== %s （%s）\n", mcpReference(hit.Repo, hit.File.Target, hit.File.Entry.Path, numbers[0], numbers[len(numbers)-1]), hit.File.Title)
		previous := 0
		for _, n := range numbers {
			if previous != 0 && n > previous+1 {
				body.WriteString("…\n")
			}
			marker := "-"
			if matched[n] {
				marker = ":"
			}
			line := hit.Lines[n]
			fmt.Fprintf(&body, "L%d%s %s\n", n, marker, line)
			previous = n
		}
	}
	text := fmt.Sprintf("找到 %d 处匹配，分布在 %d 个文件\n%s", count, fileCount, body.String())
	if truncated {
		text += fmt.Sprintf("结果已截断到 %d 条，请缩小 repos / path_glob 或使用更具体的 query。\n", limit)
	}
	if len(warnings) > 0 {
		text += "部分仓库未完成：" + strings.Join(warnings, "；")
	}
	return strings.TrimSuffix(text, "\n"), nil
}

func (s *Server) mcpSearchFiles(ctx context.Context, repo Repository, args mcpSearchCodeArgs, docsOnly bool) ([]mcpSearchFile, error) {
	var files []mcpSearchFile
	if docsOnly {
		docs, err := s.mcpLatestDocs(ctx, repo)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			files = append(files, mcpSearchFile{Entry: doc.Entry, Target: doc.Target, Title: doc.Title})
		}
		return files, nil
	}
	target, err := s.resolveMCPRef(ctx, repo, args.Ref, "")
	if err != nil {
		return nil, err
	}
	entries, err := s.mcpTree(ctx, repo, target.Commit, ".", true)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if mcpSkipEntry(repo, entry) || (args.PathGlob != "" && !mcpMatchGlob(args.PathGlob, entry.Path)) {
			continue
		}
		files = append(files, mcpSearchFile{Entry: entry, Target: target, Title: entry.Path})
	}
	return files, nil
}

// mcpGrepFiles 按 commit 分组后只搜索允许的路径。先检查树元数据，grep 不会打开超大文件；
// 使用 -z 正确处理中文、空格和冒号路径，命中后再读取安全 blob 生成前后各一行的上下文。
func (s *Server) mcpGrepFiles(ctx context.Context, repo Repository, files []mcpSearchFile, query string, regex bool, limit int) ([]mcpSearchHit, error) {
	if len(files) == 0 && regex {
		// 即使 path_glob 或文档索引筛选后为空，也让 Git 校验自己的 ERE 语法。
		// 不使用 Go regexp 代替，因为 GNU Git 的正则扩展与 Go 并不完全一致。
		target, err := s.resolveMCPRef(ctx, repo, "", "")
		if err != nil {
			return nil, err
		}
		_, err = s.mcpGit(ctx, repo, 4096, "grep", "-E", "-i", "--no-textconv", "-e", query, target.Commit, "--", ":(exclude,glob)**")
		return nil, err
	}
	groups := map[string][]mcpSearchFile{}
	var commits []string
	for _, file := range files {
		if _, ok := groups[file.Target.Commit]; !ok {
			commits = append(commits, file.Target.Commit)
		}
		groups[file.Target.Commit] = append(groups[file.Target.Commit], file)
	}
	sort.Strings(commits)
	var hits []mcpSearchHit
	count := 0
	for _, commit := range commits {
		group := groups[commit]
		for offset := 0; offset < len(group); offset += 64 {
			batch := group[offset:min(offset+64, len(group))]
			mode := "-F"
			if regex {
				mode = "-E"
			}
			args := []string{"grep", "-n", "-z", "-I", "-i", "-m", strconv.Itoa(limit - count), "--no-heading", "--no-color", "--no-textconv", mode, "-e", query, commit, "--"}
			for _, file := range batch {
				args = append(args, ":(literal)"+file.Entry.Path)
			}
			out, err := s.mcpGit(ctx, repo, 8<<20, args...)
			if err != nil {
				return nil, err
			}
			matches, err := mcpParseGrep(out, commit)
			if err != nil {
				return nil, err
			}
			for _, file := range batch {
				numbers := matches[file.Entry.Path]
				if len(numbers) == 0 {
					continue
				}
				data, err := s.mcpFileBytes(ctx, repo, file.Entry)
				if err != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					continue
				}
				lines := mcpLines(data)
				var valid []int
				for _, number := range numbers {
					if number > 0 && number <= len(lines) {
						valid = append(valid, number)
					}
				}
				valid = valid[:min(len(valid), limit-count)]
				if len(valid) > 0 {
					// 仅保留真正展示的上下文，不让结果引用整份文件的底层字符串。
					contexts := map[int]string{}
					for _, number := range valid {
						for n := max(1, number-1); n <= min(len(lines), number+1); n++ {
							runes := []rune(lines[n-1])
							if len(runes) > 300 {
								runes = append(runes[:299], '…')
							}
							contexts[n] = string(runes)
						}
					}
					hits = append(hits, mcpSearchHit{Repo: repo, File: file, Lines: contexts, LineCount: len(lines), Matches: valid})
					count += len(valid)
				}
				if count >= limit {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}

func mcpParseGrep(out []byte, commit string) (map[string][]int, error) {
	matches := map[string][]int{}
	for len(out) > 0 {
		file, rest, ok := bytes.Cut(out, []byte{0})
		if !ok {
			return nil, fmt.Errorf("Git 搜索结果缺少路径分隔符")
		}
		number, rest, ok := bytes.Cut(rest, []byte{0})
		if !ok {
			return nil, fmt.Errorf("Git 搜索结果缺少行号分隔符")
		}
		_, out, _ = bytes.Cut(rest, []byte{'\n'})
		n, err := strconv.Atoi(string(number))
		if err != nil {
			return nil, fmt.Errorf("Git 搜索结果行号无效：%q", number)
		}
		filePath := strings.TrimPrefix(string(file), commit+":")
		matches[filePath] = append(matches[filePath], n)
	}
	return matches, nil
}

// mcpMatchGlob 支持按目录段匹配的 **，避免现有 matchGlob 把 **/*.go 当作字面后缀。
func mcpMatchGlob(pattern, value string) bool {
	patterns, names := strings.Split(pattern, "/"), strings.Split(value, "/")
	type position struct{ p, n int }
	cache, visited := map[position]bool{}, map[position]bool{}
	var match func(int, int) bool
	match = func(p, n int) bool {
		key := position{p, n}
		if visited[key] {
			return cache[key]
		}
		visited[key] = true
		var result bool
		switch {
		case p == len(patterns):
			result = n == len(names)
		case patterns[p] == "**":
			result = match(p+1, n) || (n < len(names) && match(p, n+1))
		case n < len(names):
			ok, _ := path.Match(patterns[p], names[n])
			result = ok && match(p+1, n+1)
		}
		cache[key] = result
		return result
	}
	return match(0, 0)
}
