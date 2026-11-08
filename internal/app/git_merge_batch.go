package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// mergeHistoryNode 保留 Git 原始父顺序。changed[i] 是第 i 个父树与当前树的
// 差异；根节点的 changed[0] 是空树与根树的差异。集合只保存本批目标路径。
type mergeHistoryNode struct {
	last    lastCommit
	tree    string
	parents []string
	changed []map[string]struct{}
}

type mergeTreeRequest struct {
	line    string
	header  string
	changed map[string]struct{}
}

// lastCommitsForMergePaths 复现单路径 log -1 的默认历史简化，而不是对多路径
// log 的输出再做分组。每个路径在 merge 上独立选择第一个 TREESAME 父，因此
// 即使同批文件采用不同分支，也不会把被历史简化舍弃的侧分支误认为最后修改。
// 非空批次只启动两个 Git 进程：完整 DAG 和所有父边的批量 diff-tree。
func (g *Git) lastCommitsForMergePaths(ctx context.Context, repoPath, commit string, paths []string) (map[string]lastCommit, error) {
	result := make(map[string]lastCommit, len(paths))
	if len(paths) == 0 {
		return result, nil
	}
	wanted := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" || strings.ContainsRune(path, 0) {
			return nil, fmt.Errorf("merge history: invalid empty or NUL file path")
		}
		wanted[path] = struct{}{}
	}
	// %P 已提供有序父列表；不能另加 --parents，否则 rev-list 会在自定义
	// 格式外追加父 SHA。无路径过滤，避免 DAG 本身先被多路径 TREESAME 简化。
	out, err := g.runBytes(ctx, repoPath, "rev-list", "--topo-order", "--no-commit-header", "--format=%H%x00%T%x00%cI%x00%P", commit, "--")
	if err != nil {
		return nil, err
	}
	nodes, order, err := parseMergeHistory(out)
	if err != nil {
		return nil, err
	}
	requests := make([]mergeTreeRequest, 0, len(order))
	for _, sha := range order {
		node := nodes[sha]
		if len(node.parents) == 0 {
			changes := make(map[string]struct{})
			node.changed = []map[string]struct{}{changes}
			requests = append(requests, mergeTreeRequest{line: sha + "\n", header: sha + "\x00", changed: changes})
			continue
		}
		for _, parent := range node.parents {
			changes := make(map[string]struct{})
			node.changed = append(node.changed, changes)
			// stdin 双 tree 语法输出原样的 "oldtree newtree\n"；双 commit
			// 语法却会把第二个 commit 当作替代父，不能混用这两种协议。
			line := nodes[parent].tree + " " + node.tree + "\n"
			requests = append(requests, mergeTreeRequest{line: line, header: line, changed: changes})
		}
	}
	if err := g.readMergeTreeChanges(ctx, repoPath, requests, wanted); err != nil {
		return nil, err
	}
	fallback := lastCommit{SHA: commit, Time: nowString()}
	for path := range wanted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sha := order[0]
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			node := nodes[sha]
			next := ""
			// 单父也适用：与父相同则继续，不同则当前提交就是结果。
			for i, parent := range node.parents {
				if _, changed := node.changed[i][path]; !changed {
					next = parent
					break
				}
			}
			if next != "" {
				sha = next
				continue
			}
			if len(node.parents) == 0 {
				if _, exists := node.changed[0][path]; !exists {
					result[path] = fallback
					break
				}
			}
			result[path] = node.last
			break
		}
	}
	return result, nil
}

// parseMergeHistory 严格检查拓扑顺序与父闭包；遇到截断、格式差异或未取得的
// 父提交必须报错，不能把一个不完整 DAG 当成 root 而返回看似有效的 SHA。
func parseMergeHistory(out []byte) (map[string]*mergeHistoryNode, []string, error) {
	nodes := make(map[string]*mergeHistoryNode)
	var order []string
	if len(out) == 0 || out[len(out)-1] != '\n' {
		return nil, nil, fmt.Errorf("merge history: empty or truncated DAG")
	}
	for _, line := range bytes.Split(out[:len(out)-1], []byte{'\n'}) {
		fields := strings.Split(string(line), "\x00")
		if len(fields) != 4 || !mergeObjectID(fields[0]) || !mergeObjectID(fields[1]) {
			return nil, nil, fmt.Errorf("merge history: invalid DAG record")
		}
		if _, err := time.Parse(time.RFC3339, fields[2]); err != nil {
			return nil, nil, fmt.Errorf("merge history: invalid commit time: %w", err)
		}
		if _, duplicate := nodes[fields[0]]; duplicate {
			return nil, nil, fmt.Errorf("merge history: duplicate commit %s", fields[0])
		}
		parents := strings.Fields(fields[3])
		for _, parent := range parents {
			if !mergeObjectID(parent) || parent == fields[0] {
				return nil, nil, fmt.Errorf("merge history: invalid parent SHA")
			}
			if _, visited := nodes[parent]; visited {
				return nil, nil, fmt.Errorf("merge history: non-topological DAG")
			}
		}
		nodes[fields[0]] = &mergeHistoryNode{last: lastCommit{SHA: fields[0], Time: normalizeGitTime(fields[2])}, tree: fields[1], parents: parents}
		order = append(order, fields[0])
	}
	for _, node := range nodes {
		for _, parent := range node.parents {
			if nodes[parent] == nil {
				return nil, nil, fmt.Errorf("merge history: missing parent %s", parent)
			}
		}
	}
	return nodes, order, nil
}

func mergeObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// readMergeTreeChanges 流式丢弃非目标变化，避免保留整个仓库全部历史的 diff。
// stdin 由 os/exec 并发写入，stdout 同时解析，避免两个管道都写满后互相等待。
func (g *Git) readMergeTreeChanges(ctx context.Context, repoPath string, requests []mergeTreeRequest, wanted map[string]struct{}) (retErr error) {
	cmdCtx, cancel := context.WithTimeout(ctx, g.cfg.GitCommandTimeout)
	defer cancel()
	cmd := g.command(cmdCtx, repoPath, "diff-tree", "--stdin", "--raw", "-z", "-r", "--root", "--always", "--no-renames", "--no-abbrev")
	var input strings.Builder
	for _, request := range requests {
		input.WriteString(request.line)
	}
	cmd.Stdin = strings.NewReader(input.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		stdout.Close()
		return err
	}
	stop := context.AfterFunc(cmdCtx, func() { stdout.Close() })
	defer func() {
		ctxErr := cmdCtx.Err()
		protocolFailed := retErr != nil
		if protocolFailed || ctxErr != nil {
			cancel()
		}
		stdout.Close()
		waitErr := cmd.Wait()
		stop()
		if !protocolFailed {
			ctxErr = cmdCtx.Err()
		}
		if ctxErr != nil {
			retErr = errors.Join(retErr, ctxErr)
		} else if waitErr != nil && retErr == nil {
			retErr = fmt.Errorf("git merge diff-tree: %s: %w", strings.TrimSpace(stderr.String()), waitErr)
		}
	}()
	reader := bufio.NewReader(stdout)
	for _, request := range requests {
		header := make([]byte, len(request.header))
		if _, err := io.ReadFull(reader, header); err != nil {
			return fmt.Errorf("merge diff-tree header: %w", err)
		}
		if string(header) != request.header {
			return fmt.Errorf("merge diff-tree: unexpected response header %q", header)
		}
		for {
			prefix, err := reader.Peek(1)
			if err == io.EOF || err == nil && prefix[0] != ':' {
				break
			}
			if err != nil {
				return fmt.Errorf("merge diff-tree record: %w", err)
			}
			raw, err := reader.ReadString(0)
			if err != nil {
				return fmt.Errorf("merge diff-tree raw header: %w", err)
			}
			fields := strings.Fields(strings.TrimSuffix(raw, "\x00"))
			if len(fields) != 5 || !mergeRawMode(strings.TrimPrefix(fields[0], ":")) || !mergeRawMode(fields[1]) || !mergeObjectID(fields[2]) || !mergeObjectID(fields[3]) || len(fields[4]) != 1 || !strings.Contains("ADMT", fields[4]) {
				return fmt.Errorf("merge diff-tree: invalid raw header %q", raw)
			}
			path, err := reader.ReadString(0)
			if err != nil || len(path) <= 1 {
				return fmt.Errorf("merge diff-tree: invalid path: %w", errors.Join(err, errors.New("expected nonempty NUL-terminated path")))
			}
			path = path[:len(path)-1] // tab、换行、反斜线均属于路径本身，不能清理。
			for {
				if _, ok := wanted[path]; ok {
					request.changed[path] = struct{}{}
				}
				// Git 的 literal pathspec 也匹配目录后代。某路径在旧版本中
				// 曾是目录时，子文件变化同样决定这个路径的 TREESAME 状态。
				i := strings.LastIndexByte(path, '/')
				if i < 0 {
					break
				}
				path = path[:i]
			}
		}
	}
	if _, err := reader.Peek(1); err != io.EOF {
		return fmt.Errorf("merge diff-tree: trailing output or read error: %v", err)
	}
	return nil
}

func mergeRawMode(mode string) bool {
	if len(mode) != 6 {
		return false
	}
	for _, c := range mode {
		if c < '0' || c > '7' {
			return false
		}
	}
	return true
}
