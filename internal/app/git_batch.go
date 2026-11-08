package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// errGitBatchMergeHistory 表示批量最后提交不能严格复现逐文件历史简化。
// 调用方必须使用逐路径 TREESAME 的合并历史批量算法，不能把失败当作没有文档。
var errGitBatchMergeHistory = errors.New("batch last commits requires linear history; use lastCommitForPath for merge history")

// lastCommitsForPaths 接收 lsTree 返回的原样文件名，不接收目录或 Git pathspec。
// 在线性历史中，每个文件第一次出现的提交与 log -1 -- 文件相同；不跟随重命名，
// 因而重命名目标的最后提交是重命名提交，旧名字仍保留删除历史。
//
// 不能把多路径 log 的结果直接用于 merge：Git 默认按每个查询的 TREESAME 状态
// 选择父历史，多路径与单路径可能选择不同父提交；普通 merge 也不输出 name-only。
// 本实现遍历不加路径过滤的完整可达日志，检查真实父列表；只要有 merge 就返回
// 可 errors.Is 判断的限制错误及 nil map，避免静默给出错误版本。进程数始终为一。
func (g *Git) lastCommitsForPaths(ctx context.Context, repoPath, commit string, paths []string) (map[string]lastCommit, error) {
	result := make(map[string]lastCommit, len(paths))
	if len(paths) == 0 {
		return result, nil
	}
	wanted := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if p == "" || strings.ContainsRune(p, 0) {
			return nil, fmt.Errorf("invalid empty or NUL file path")
		}
		wanted[p] = struct{}{}
	}
	// 不加 -- paths：既避免命令行长度限制，又确保不被路径简化隐藏 merge。
	out, err := g.runBytes(ctx, repoPath, "log", "--name-only", "-z", "--no-renames", "--format=%x00%H%x00%cI%x00%P%x00", commit, "--")
	if err != nil {
		return nil, err
	}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields)-1; {
		// 空路径不合法，因此空字段可无歧义地界定提交头；头后的额外 NUL
		// 来自 log -z，首个文件名前还有 Git 添加的一枚换行，只去掉这一枚。
		if i+4 >= len(fields) || len(fields[i]) != 0 || len(fields[i+4]) != 0 {
			return nil, fmt.Errorf("invalid batch log header")
		}
		last := lastCommit{SHA: string(fields[i+1]), Time: normalizeGitTime(string(fields[i+2]))}
		if len(bytes.Fields(fields[i+3])) > 1 {
			return nil, errGitBatchMergeHistory
		}
		i += 5
		first := true
		for i < len(fields) && len(fields[i]) != 0 {
			p := fields[i]
			if first {
				p = bytes.TrimPrefix(p, []byte{'\n'})
				first = false
			}
			name := string(p)
			if _, ok := wanted[name]; ok {
				if _, found := result[name]; !found {
					result[name] = last
				}
			}
			i++
		}
	}
	// 与旧接口的无历史回退一致，且同批缺失路径共用一个时间值。
	fallback := lastCommit{SHA: commit, Time: nowString()}
	for p := range wanted {
		if _, ok := result[p]; !ok {
			result[p] = fallback
		}
	}
	return result, nil
}

// catFiles 在一个 cat-file --batch 进程内逐条写请求、读完整响应，避免先写完
// 所有 SHA 后才读输出时，stdin/stdout 两个管道互相等候。请求按 SHA 去重。
// 返回键保持输入 SHA（允许旧接口支持的缩写）；内容按字节数读取，允许 NUL/换行。
func (g *Git) catFiles(ctx context.Context, repoPath string, blobs []string) (result map[string][]byte, retErr error) {
	result = make(map[string][]byte, len(blobs))
	unique := make([]string, 0, len(blobs))
	seen := make(map[string]bool, len(blobs))
	for _, sha := range blobs {
		if !gitSafeCommit(sha) {
			return nil, errBadRequest("invalid blob sha")
		}
		if !seen[sha] {
			seen[sha] = true
			unique = append(unique, sha)
		}
	}
	if len(unique) == 0 {
		return result, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, g.cfg.GitCommandTimeout)
	defer cancel()
	cmd := g.command(cmdCtx, repoPath, "cat-file", "--batch")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	// 即使包装命令的子进程继承管道，取消也直接关闭本端 FD，让阻塞读写返回。
	stop := context.AfterFunc(cmdCtx, func() {
		stdin.Close()
		stdout.Close()
	})
	defer func() {
		ctxErr := cmdCtx.Err()
		protocolFailed := retErr != nil
		if retErr != nil || ctxErr != nil {
			cancel()
		}
		stdin.Close()
		stdout.Close()
		waitErr := cmd.Wait() // 每条成功 Start 的路径都回收进程，随后才读 stderr。
		stop()
		if !protocolFailed {
			// 最后一个响应读完后，进程退出阶段仍可能超时；不能漏掉该窗口。
			ctxErr = cmdCtx.Err()
		}
		if ctxErr != nil {
			retErr = errors.Join(retErr, ctxErr)
		} else if waitErr != nil && retErr == nil {
			retErr = fmt.Errorf("git cat-file --batch: %s: %w", strings.TrimSpace(stderr.String()), waitErr)
		}
		if retErr != nil {
			result = nil // 批中任一对象失败，不暴露易被误用的部分成功结果。
		}
	}()
	reader := bufio.NewReader(stdout)
	for _, sha := range unique {
		if _, err := io.WriteString(stdin, sha+"\n"); err != nil {
			return nil, fmt.Errorf("cat-file request: %w", err)
		}
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("cat-file header: %w", err)
		}
		parts := strings.Fields(header)
		// missing/ambiguous 等对象错误只有两个字段，必须报错而非作为空内容。
		if len(parts) != 3 || parts[1] != "blob" || !strings.HasPrefix(parts[0], strings.ToLower(sha)) {
			return nil, fmt.Errorf("cat-file object %s: unexpected response %q", sha, strings.TrimSuffix(header, "\n"))
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil || size < 0 {
			return nil, fmt.Errorf("cat-file object %s: invalid size %q", sha, parts[2])
		}
		content := make([]byte, size)
		if _, err := io.ReadFull(reader, content); err != nil {
			return nil, fmt.Errorf("cat-file object %s: %w", sha, err)
		}
		if end, err := reader.ReadByte(); err != nil || end != '\n' {
			return nil, fmt.Errorf("cat-file object %s: invalid content terminator: %w", sha, errors.Join(err, errors.New("expected newline")))
		}
		result[sha] = content
	}
	return result, nil
}

// isAncestor 仅把 merge-base 的退出码 1 视为正常的“不是祖先”。
// 仓库/对象错误、取消和超时即使带有退出码，也必须向上返回。
func (g *Git) isAncestor(ctx context.Context, repoPath, oldCommit, newCommit string) (bool, error) {
	_, err := g.runBytes(ctx, repoPath, "merge-base", "--is-ancestor", oldCommit, newCommit)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// pathHistoryRange 仅遍历 newCommit 可达而 oldCommit 不可达的新增历史。
// oldCommit 为空时读取 newCommit 的完整历史；branch 只作为事件标签，不参与选版。
// 与旧 pathHistory 一样使用默认历史简化和重命名检测，不跟随目录外的历史。
func (g *Git) pathHistoryRange(ctx context.Context, repoPath, branch, scanPath, oldCommit, newCommit string) ([]historyPathEvent, error) {
	revision := newCommit
	if oldCommit != "" {
		revision = oldCommit + ".." + newCommit
	}
	args := []string{"--literal-pathspecs", "log", "--date=iso-strict", "--name-status", "-z", "--find-renames", "--format=%x1e%H%x00%cI", revision, "--"}
	if scanPath != "." {
		args = append(args, scanPath)
	}
	out, err := g.runBytes(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	return parseHistoryPathEvents(out, branch, scanPath)
}

// parseHistoryPathEvents 只在“状态字段”位置识别提交头。路径字段按 NUL 消费，
// 即使文件名含换行、tab、反斜线或提交分隔符 0x1e，也不会被拆开或清理。
func parseHistoryPathEvents(out []byte, branch, scanPath string) ([]historyPathEvent, error) {
	fields := bytes.Split(out, []byte{0})
	events := []historyPathEvent{}
	var sha, commitTime string
	for i := 0; i < len(fields); {
		field := string(fields[i])
		i++
		if field == "" {
			continue
		}
		if strings.HasPrefix(field, "\x1e") {
			if i >= len(fields) {
				return nil, fmt.Errorf("invalid path history header")
			}
			sha, commitTime = field[1:], normalizeGitTime(string(fields[i]))
			i++
			continue
		}
		status := strings.TrimPrefix(field, "\n") // 只移除 Git 在 diff 前加的换行。
		count := 1
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			count = 2
		}
		if sha == "" || i+count > len(fields)-1 {
			return nil, fmt.Errorf("invalid path history status %q", status)
		}
		event := historyPathEvent{ScanPath: scanPath, Branch: branch, CommitSHA: sha, CommitTime: commitTime}
		switch {
		case count == 2:
			event.OldPath, event.NewPath = string(fields[i]), string(fields[i+1])
			event.EventType = pathEventType(event.OldPath, event.NewPath)
			event.RenameScore, _ = strconv.Atoi(status[1:])
		case status == "D":
			event.EventType, event.OldPath = "deleted", string(fields[i])
		case status == "A":
			event.EventType, event.NewPath = "created", string(fields[i])
		}
		i += count
		if event.EventType != "" {
			events = append(events, event)
		}
	}
	// 保留旧接口逐事件反序（包括同一提交内的顺序），供历史回填顺序一致。
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}
