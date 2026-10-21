package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

// mcpResolvedRef 同时保留展示用 ref 和不可变 commit，引用核实和 blobURL 必须使用同一次解析结果。
type mcpResolvedRef struct {
	Ref       string
	Commit    string
	VersionID int64
}

// resolveMCPRepository 接受 slug、名称、数字 ID，只暴露启用的仓库。
func (s *Server) resolveMCPRepository(ctx context.Context, selector string) (Repository, error) {
	repos, err := s.mcpRepositories(ctx, nil)
	if err != nil {
		return Repository{}, err
	}
	var available []string
	for _, repo := range repos {
		if repo.Slug == selector || repo.Name == selector || strconv.FormatInt(repo.ID, 10) == selector {
			return repo, nil
		}
		available = append(available, repo.Slug)
	}
	return Repository{}, fmt.Errorf("仓库不存在或未启用。可用：%s", strings.Join(available, "、"))
}

func (s *Server) mcpRepositories(ctx context.Context, selectors []string) ([]Repository, error) {
	all, err := listRepositories(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var repos []Repository
	found := make(map[string]bool)
	var available []string
	for _, repo := range all {
		if !repo.Enabled {
			continue
		}
		available = append(available, repo.Slug)
		include := len(selectors) == 0
		for _, selector := range selectors {
			if selector == repo.Slug || selector == repo.Name || selector == strconv.FormatInt(repo.ID, 10) {
				found[selector], include = true, true
			}
		}
		if include {
			repos = append(repos, repo)
		}
	}
	for _, selector := range selectors {
		if !found[selector] {
			return nil, fmt.Errorf("仓库 %q 不存在或未启用。可用：%s", selector, strings.Join(available, "、"))
		}
	}
	return repos, nil
}

// resolveMCPRef 可供引用核实调用。latest 对已索引文档固定到选中版本的 HEAD；
// 其他文件及目录走默认分支，显式分支走镜像中的当前 HEAD，显式 SHA 仅接受真实提交。
func (s *Server) resolveMCPRef(ctx context.Context, repo Repository, ref, filePath string) (mcpResolvedRef, error) {
	if filePath != "" && filePath != "." {
		if err := mcpValidatePath(filePath, false); err != nil {
			return mcpResolvedRef{}, err
		}
	}
	if ref == "" || ref == "latest" {
		if filePath != "" && filePath != "." {
			var result mcpResolvedRef
			err := s.db.QueryRowContext(ctx, `SELECT v.branch, v.head_commit_sha, v.id
				FROM doc_latest l JOIN doc_versions v ON v.id = l.version_id
				WHERE l.repo_id = ? AND l.file_path = ? AND v.status IN ('active','renamed','moved')`, repo.ID, filePath).
				Scan(&result.Ref, &result.Commit, &result.VersionID)
			if err == nil {
				return result, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return mcpResolvedRef{}, err
			}
		}
		ref = repo.DefaultBranch
	}
	// 分支名始终放在 refs/heads 下解析，不允许修订表达式、选项或任意对象类型。
	out, err := s.mcpGit(ctx, repo, 4096, "show-ref", "--verify", "--hash", "refs/heads/"+ref)
	result := mcpResolvedRef{Ref: ref}
	if err != nil && gitSafeCommit(ref) {
		out, err = s.mcpGit(ctx, repo, 4096, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if err == nil {
			result.Ref = strings.TrimSpace(string(out))[:min(12, len(strings.TrimSpace(string(out))))]
		}
	}
	if err != nil {
		refs, _ := listBranches(ctx, s.db, repo.ID)
		names := make([]string, 0, len(refs))
		for _, branch := range refs {
			names = append(names, branch.RefName)
		}
		return mcpResolvedRef{}, fmt.Errorf("ref %q 不存在。可用分支：%s；也可传 commit SHA", ref, strings.Join(names, "、"))
	}
	result.Commit = strings.TrimSpace(string(out))
	if filePath != "" && filePath != "." {
		// 显式 commit 只关联同一快照的文档，避免把旧提交跳转到不同版本。
		err = s.db.QueryRowContext(ctx, `SELECT id FROM doc_versions WHERE repo_id = ? AND file_path = ?
			AND head_commit_sha = ? AND status IN ('active','renamed','moved') ORDER BY id DESC LIMIT 1`, repo.ID, filePath, result.Commit).Scan(&result.VersionID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return mcpResolvedRef{}, err
		}
	}
	return result, nil
}

// mcpValidatePath 在任何规范化前拒绝危险路径，包括 Git pathspec 的魔法前缀。
// path_glob 只允许普通 glob，不能注入 :(exclude) 等改变搜索边界的参数。
func mcpValidatePath(value string, allowDot bool) error {
	if value == "." && allowDot {
		return nil
	}
	if value == "" || strings.HasPrefix(value, "/") || strings.HasPrefix(value, ":") ||
		strings.Contains(value, "..") || strings.ContainsAny(value, "\\\x00\r\n") || path.IsAbs(value) {
		return fmt.Errorf("路径 %q 无效：必须是仓库相对路径，不能包含 ..、反斜杠或 Git pathspec 前缀", value)
	}
	return nil
}

func mcpReference(repo Repository, target mcpResolvedRef, filePath string, first, last int) string {
	key := repo.Slug + "@" + target.Ref + ":" + filePath
	if first > 0 {
		key += fmt.Sprintf("#L%d-%d", first, last)
	}
	return key
}

// mcpOutputBuffer 对 Git 输出做硬限制，避免一次 grep 或错误输出挤满进程内存。
type mcpOutputBuffer struct {
	// 不嵌入 bytes.Buffer：否则它的 ReadFrom 会被 io.Copy 优先调用，绕过受限 Write。
	buffer   bytes.Buffer
	max      int64
	overflow bool
}

func (b *mcpOutputBuffer) Write(p []byte) (int, error) {
	if int64(b.buffer.Len())+int64(len(p)) > b.max {
		b.overflow = true
		return 0, io.ErrShortBuffer
	}
	return b.buffer.Write(p)
}

func (s *Server) mcpGit(ctx context.Context, repo Repository, maxBytes int64, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, s.cfg.GitBin, args...)
	cmd.Dir = s.git.repoPath(repo.ID)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	stdout := &mcpOutputBuffer{max: maxBytes}
	stderr := &mcpOutputBuffer{max: 32 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.overflow {
		return nil, fmt.Errorf("Git 结果过大，请缩小仓库或 path_glob 范围")
	}
	if err != nil {
		// grep 的退出码 1 表示无匹配，而不是工具失败。
		var exitErr *exec.ExitError
		if len(args) > 0 && args[0] == "grep" && errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("Git 读取失败：%s (%w)", strings.TrimSpace(stderr.buffer.String()), err)
	}
	return stdout.buffer.Bytes(), nil
}

// mcpTree 用 NUL 分隔记录，保留中文、空格、冒号等路径原文。dir 只用于非递归目录访问。
func (s *Server) mcpTree(ctx context.Context, repo Repository, commit, dir string, recursive bool) ([]treeEntry, error) {
	args := []string{"ls-tree", "-l", "-z"}
	if recursive {
		args = append(args, "-r")
	}
	object := commit
	if dir != "" && dir != "." {
		object += ":" + strings.TrimSuffix(dir, "/")
	}
	args = append(args, object)
	out, err := s.mcpGit(ctx, repo, 16<<20, args...)
	if err != nil {
		return nil, err
	}
	var entries []treeEntry
	for _, record := range bytes.Split(out, []byte{0}) {
		header, name, ok := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(header))
		if !ok || len(fields) != 4 {
			continue
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		filePath := string(name)
		if dir != "" && dir != "." {
			filePath = path.Join(dir, filePath)
		}
		entries = append(entries, treeEntry{Mode: fields[0], Type: fields[1], BlobSHA: fields[2], Size: size, Path: filePath})
	}
	return entries, nil
}

func mcpMaxFileSize(repo Repository) int64 {
	if repo.MaxFileSizeBytes > 0 {
		return repo.MaxFileSizeBytes
	}
	return 2 << 20
}

func mcpSkipEntry(repo Repository, entry treeEntry) bool {
	return entry.Type != "blob" || entry.Size > mcpMaxFileSize(repo) ||
		aiShouldSkipPath(entry.Path, AIIndexConfig{}) || mcpValidatePath(entry.Path, false) != nil
}

func (s *Server) mcpFileBytes(ctx context.Context, repo Repository, entry treeEntry) ([]byte, error) {
	if mcpSkipEntry(repo, entry) {
		return nil, fmt.Errorf("文件 %q 被跳过：路径受限、非普通文件或超过 max_file_size_bytes=%d", entry.Path, mcpMaxFileSize(repo))
	}
	data, err := s.mcpGit(ctx, repo, mcpMaxFileSize(repo)+1, "cat-file", "blob", entry.BlobSHA)
	if err != nil {
		return nil, err
	}
	// 复用现有文本启发式，同时检查整个 blob 的 NUL，避免前 8 KB 是文本的二进制漏过采样。
	if bytes.IndexByte(data, 0) >= 0 || !aiLooksText(data) {
		return nil, fmt.Errorf("文件 %q 是二进制文件，已跳过", entry.Path)
	}
	return data, nil
}

func mcpLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}
