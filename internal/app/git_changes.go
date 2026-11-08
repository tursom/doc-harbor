package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// hasCommit 仅把 quiet rev-parse 的退出码 1 当成对象已缺失。
// 仓库损坏、命令超时等异常不能伪装成强推后需要全量重建的正常情况。
func (g *Git) hasCommit(ctx context.Context, repoPath, commit string) (bool, error) {
	_, err := g.runBytes(ctx, repoPath, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// parseNameStatusNUL 按 Git 的 NUL 协议读取路径，避免空白、制表符和引号改变重命名身份。
func parseNameStatusNUL(out []byte) ([]CommitFileChange, error) {
	changes := []CommitFileChange{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields)-1; {
		status := string(fields[i])
		i++
		if status == "" {
			return nil, fmt.Errorf("empty Git diff status")
		}
		count := 1
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			count = 2
		}
		if i+count > len(fields)-1 {
			return nil, fmt.Errorf("truncated Git diff paths")
		}
		change := CommitFileChange{Status: status, Path: string(fields[i])}
		if count == 2 {
			change.OldPath, change.NewPath = string(fields[i]), string(fields[i+1])
			change.Path = change.NewPath
			change.RenameScore, _ = strconv.Atoi(status[1:])
		}
		changes = append(changes, change)
		i += count
	}
	return changes, nil
}
