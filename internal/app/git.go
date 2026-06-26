package app

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Git struct {
	cfg Config
}

type gitRef struct {
	Name       string
	CommitSHA  string
	CommitTime string
}

type treeEntry struct {
	Mode    string
	Type    string
	BlobSHA string
	Size    int64
	Path    string
}

type lastCommit struct {
	SHA  string
	Time string
}

type historyPathEvent struct {
	ScanPath    string
	Branch      string
	EventType   string
	OldPath     string
	NewPath     string
	CommitSHA   string
	CommitTime  string
	RenameScore int
}

func newGit(cfg Config) *Git {
	return &Git{cfg: cfg}
}

func (g *Git) repoPath(repoID int64) string {
	return filepath.Join(g.cfg.DataDir, "repos", fmt.Sprintf("%d.git", repoID))
}

func (g *Git) ensureMirror(ctx context.Context, repo Repository) error {
	if err := g.validateRepoURL(repo.RepoURL); err != nil {
		return err
	}
	repoPath := g.repoPath(repo.ID)
	if _, err := os.Stat(filepath.Join(repoPath, "HEAD")); err == nil {
		_, err := g.run(ctx, repoPath, "remote", "update", "--prune")
		return err
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return err
	}
	_, err := g.run(ctx, "", "clone", "--mirror", repo.RepoURL, repoPath)
	return err
}

func (g *Git) validateRepoURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errBadRequest("repo_url is required")
	}
	if strings.HasPrefix(raw, "file://") || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
		if g.cfg.AllowLocalGit {
			return nil
		}
		return errBadRequest("local git urls are disabled; set ALLOW_LOCAL_GIT=1 to enable")
	}
	if len(g.cfg.AllowedGitHosts) == 0 {
		return nil
	}
	host := gitURLHost(raw)
	if host == "" {
		return errBadRequest("cannot determine git url host")
	}
	for _, allowed := range g.cfg.AllowedGitHosts {
		if strings.EqualFold(host, strings.TrimSpace(allowed)) {
			return nil
		}
	}
	return errBadRequest("git url host is not allowed")
}

func gitURLHost(raw string) string {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		host := u.Hostname()
		if host != "" {
			return host
		}
		return u.Host
	}
	if at := strings.Index(raw, "@"); at >= 0 {
		rest := raw[at+1:]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			return rest[:colon]
		}
	}
	host, _, err := net.SplitHostPort(raw)
	if err == nil {
		return host
	}
	return ""
}

func (g *Git) branches(ctx context.Context, repoPath string) ([]gitRef, error) {
	out, err := g.run(ctx, repoPath, "for-each-ref", "--format=%(refname:short)%00%(objectname)%00%(committerdate:iso-strict)", "refs/heads")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	refs := make([]gitRef, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 3 {
			continue
		}
		refs = append(refs, gitRef{Name: parts[0], CommitSHA: parts[1], CommitTime: normalizeGitTime(parts[2])})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

func (g *Git) lsTree(ctx context.Context, repoPath, commit, scanPath string) ([]treeEntry, error) {
	args := []string{"ls-tree", "-r", "-l", "-z", commit}
	if scanPath != "." {
		args = append(args, "--", scanPath)
	}
	out, err := g.runBytes(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	records := bytes.Split(out, []byte{0})
	entries := make([]treeEntry, 0, len(records))
	for _, record := range records {
		if len(record) == 0 {
			continue
		}
		header, filePath, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			continue
		}
		fields := strings.Fields(string(header))
		if len(fields) < 4 {
			continue
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		entries = append(entries, treeEntry{
			Mode:    fields[0],
			Type:    fields[1],
			BlobSHA: fields[2],
			Size:    size,
			Path:    normalizeRepoPath(string(filePath)),
		})
	}
	return entries, nil
}

func (g *Git) catFile(ctx context.Context, repoPath, blobSHA string) ([]byte, error) {
	if !gitSafeCommit(blobSHA) {
		return nil, errBadRequest("invalid blob sha")
	}
	return g.runBytes(ctx, repoPath, "cat-file", "-p", blobSHA)
}

func (g *Git) showFile(ctx context.Context, repoPath, commit, filePath string) ([]byte, error) {
	if !gitSafeCommit(commit) {
		return nil, errBadRequest("invalid commit sha")
	}
	filePath = normalizeRepoPath(filePath)
	if filePath == "" {
		return nil, errBadRequest("invalid file path")
	}
	return g.runBytes(ctx, repoPath, "show", commit+":"+filePath)
}

func (g *Git) lastCommitForPath(ctx context.Context, repoPath, commit, filePath string) (lastCommit, error) {
	out, err := g.run(ctx, repoPath, "log", "-1", "--format=%H%x00%cI", commit, "--", filePath)
	if err != nil {
		return lastCommit{}, err
	}
	parts := strings.Split(strings.TrimSpace(out), "\x00")
	if len(parts) < 2 {
		return lastCommit{SHA: commit, Time: nowString()}, nil
	}
	return lastCommit{SHA: parts[0], Time: normalizeGitTime(parts[1])}, nil
}

func (g *Git) diffNameStatus(ctx context.Context, repoPath, oldCommit, newCommit, scanPath string) ([]CommitFileChange, error) {
	args := []string{"diff", "--name-status", "--find-renames", "--find-copies", oldCommit, newCommit}
	if scanPath != "." {
		args = append(args, "--", scanPath)
	}
	out, err := g.run(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func (g *Git) pathHistory(ctx context.Context, repoPath, branch, scanPath string) ([]historyPathEvent, error) {
	args := []string{"log", "--date=iso-strict", "--name-status", "--find-renames", "--format=%x1e%H%x00%cI", branch}
	if scanPath != "." {
		args = append(args, "--", scanPath)
	}
	out, err := g.run(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	chunks := strings.Split(out, "\x1e")
	events := []historyPathEvent{}
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		header := strings.Split(lines[0], "\x00")
		if len(header) < 2 {
			continue
		}
		commitSHA := header[0]
		commitTime := normalizeGitTime(header[1])
		changes := parseNameStatus(strings.Join(lines[1:], "\n"))
		for _, change := range changes {
			event := historyPathEvent{
				ScanPath:   scanPath,
				Branch:     branch,
				CommitSHA:  commitSHA,
				CommitTime: commitTime,
			}
			switch {
			case strings.HasPrefix(change.Status, "R"), strings.HasPrefix(change.Status, "C"):
				event.EventType = pathEventType(change.OldPath, change.NewPath)
				event.OldPath = change.OldPath
				event.NewPath = change.NewPath
				event.RenameScore = change.RenameScore
			case change.Status == "D":
				event.EventType = "deleted"
				event.OldPath = change.Path
			case change.Status == "A":
				event.EventType = "created"
				event.NewPath = change.Path
			default:
				continue
			}
			if event.OldPath != "" || event.NewPath != "" {
				events = append(events, event)
			}
		}
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

func (g *Git) commitLog(ctx context.Context, repoPath, branch string, limit int) ([]CommitSummary, error) {
	args := []string{"log", "--topo-order", "--date=iso-strict", "--decorate=short", "--parents",
		fmt.Sprintf("--max-count=%d", limit), "--pretty=format:%H%x00%P%x00%an%x00%ae%x00%cI%x00%D%x00%s"}
	if strings.TrimSpace(branch) == "" {
		args = append(args, "--all")
	} else {
		args = append(args, branch)
	}
	out, err := g.run(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	commits := make([]CommitSummary, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 7 {
			continue
		}
		parents := []string{}
		if strings.TrimSpace(parts[1]) != "" {
			parents = strings.Fields(parts[1])
		}
		commits = append(commits, CommitSummary{
			SHA:         parts[0],
			Parents:     parents,
			Author:      parts[2],
			AuthorEmail: parts[3],
			CommitTime:  normalizeGitTime(parts[4]),
			Decorations: parts[5],
			Message:     parts[6],
		})
	}
	return commits, nil
}

func (g *Git) commitSummary(ctx context.Context, repoPath, sha string) (CommitSummary, error) {
	if !gitSafeCommit(sha) {
		return CommitSummary{}, errBadRequest("invalid commit sha")
	}
	out, err := g.run(ctx, repoPath, "show", "-s", "--date=iso-strict", "--pretty=format:%H%x00%P%x00%an%x00%ae%x00%cI%x00%D%x00%B", sha)
	if err != nil {
		return CommitSummary{}, err
	}
	parts := strings.SplitN(out, "\x00", 7)
	if len(parts) < 7 {
		return CommitSummary{}, errNotFound("commit not found")
	}
	parents := []string{}
	if strings.TrimSpace(parts[1]) != "" {
		parents = strings.Fields(parts[1])
	}
	return CommitSummary{
		SHA:         parts[0],
		Parents:     parents,
		Author:      parts[2],
		AuthorEmail: parts[3],
		CommitTime:  normalizeGitTime(parts[4]),
		Decorations: parts[5],
		Message:     strings.TrimSpace(parts[6]),
	}, nil
}

func (g *Git) commitFiles(ctx context.Context, repoPath, sha string) ([]CommitFileChange, error) {
	if !gitSafeCommit(sha) {
		return nil, errBadRequest("invalid commit sha")
	}
	out, err := g.run(ctx, repoPath, "diff-tree", "--no-commit-id", "--name-status", "--find-renames", "-r", sha)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func parseNameStatus(out string) []CommitFileChange {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	changes := make([]CommitFileChange, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		change := CommitFileChange{Status: status}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if len(parts) >= 3 {
				change.OldPath = normalizeRepoPath(parts[1])
				change.NewPath = normalizeRepoPath(parts[2])
				change.Path = change.NewPath
			}
			score, _ := strconv.Atoi(strings.TrimLeft(status[1:], "0"))
			change.RenameScore = score
		} else {
			change.Path = normalizeRepoPath(parts[1])
		}
		changes = append(changes, change)
	}
	return changes
}

func (g *Git) run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := g.runBytes(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (g *Git) runBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, g.cfg.GitCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, g.cfg.GitBin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

func normalizeGitTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC().Format(timeLayout)
	}
	if t, err := time.Parse("2006-01-02 15:04:05 -0700", value); err == nil {
		return t.UTC().Format(timeLayout)
	}
	return value
}
