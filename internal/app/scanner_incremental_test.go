package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 使用真实 Git 与 SQLite 检查最终索引和命令数量，避免只验证某个布尔分支。
func TestScannerIncrementalOnlyWritesChangedFiles(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	for i := 0; i < 120; i++ {
		writeScanPathTestFile(t, repo.RepoURL, fmt.Sprintf("doc/extra-%03d.md", i), fmt.Sprintf("# Extra %d\n", i))
	}
	runTestGit(t, repo.RepoURL, "add", ".")
	runTestGit(t, repo.RepoURL, "commit", "-m", "many documents")
	trace := filepath.Join(t.TempDir(), "git-commands")
	installScannerGitWrapper(t, server, "printf '%s\\n' \"$*\" >> "+scannerShellQuote(trace)+"\n")
	started := time.Now()
	run, stat := regressionScan(t, server, repo, "manual")
	if run.FileCount != 123 || stat.Mode != "full" || stat.Errors != 0 {
		t.Fatalf("initial scan = %+v", run)
	}
	commands, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	fullCommands := strings.Count(string(commands), "\n")
	if fullCommands > 15 || strings.Contains(string(commands), "log -1 ") || strings.Contains(string(commands), "cat-file -p ") {
		t.Fatalf("full scan should use bounded Git batches, %d commands:\n%s", fullCommands, commands)
	}
	t.Logf("首次全量：123 个文件，%d 条 Git 命令，%s", fullCommands, time.Since(started))
	if err := os.WriteFile(trace, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	run, stat = regressionScan(t, server, repo, "manual")
	if run.FileCount != 0 || !stat.SkippedByHead {
		t.Fatalf("unchanged ordinary refresh = %+v", run)
	}
	commands, _ = os.ReadFile(trace)
	if strings.Contains(string(commands), "ls-tree") || strings.Contains(string(commands), " log ") || strings.Contains(string(commands), "cat-file") {
		t.Fatalf("unchanged refresh did work:\n%s", commands)
	}
	t.Logf("无变化刷新：0 个文件，%d 条 Git 命令，%s", strings.Count(string(commands), "\n"), time.Since(started))
	writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Changed Alpha\n")
	runTestGit(t, repo.RepoURL, "add", ".")
	runTestGit(t, repo.RepoURL, "commit", "-m", "one changed document")
	started = time.Now()
	run, stat = regressionScan(t, server, repo, "manual")
	if run.FileCount != 1 || stat.Mode != "incremental" || stat.Reused != 122 || stat.HistoryEvents != 0 {
		t.Fatalf("single-file increment = %+v, %+v", run, stat)
	}
	content, err := getScanPathTestVersion(context.Background(), server, repo.ID, "doc/a.md")
	if err != nil || content.Title != "Changed Alpha" {
		t.Fatalf("changed title = %+v, %v", content, err)
	}
	unchanged, err := getScanPathTestVersion(context.Background(), server, repo.ID, "doc/extra-000.md")
	if err != nil || unchanged.HeadCommitSHA != stat.Head || unchanged.Title != "Extra 0" {
		t.Fatalf("reused snapshot = %+v, %v", unchanged, err)
	}
	t.Logf("单文件增量：写入 1 个文件，复用 122 个文件，%s", time.Since(started))
	// 净 diff 为空并不代表最近提交未变：修改再还原仍必须更新文件最近提交。
	writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Temporary\n")
	runTestGit(t, repo.RepoURL, "add", ".")
	runTestGit(t, repo.RepoURL, "commit", "-m", "temporary change")
	writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Changed Alpha\n")
	runTestGit(t, repo.RepoURL, "add", ".")
	runTestGit(t, repo.RepoURL, "commit", "-m", "restore content")
	run, stat = regressionScan(t, server, repo, "manual")
	if run.FileCount != 1 || stat.Reused != 122 {
		t.Fatalf("reverted content must refresh last commit = %+v", run)
	}
	content, err = getScanPathTestVersion(context.Background(), server, repo.ID, "doc/a.md")
	if err != nil || content.LastCommitSHA != stat.Head {
		t.Fatalf("reverted last commit = %+v, %v", content, err)
	}
}

func TestScannerIncrementalDeletionAndChangedRules(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	if err := os.Remove(filepath.Join(repo.RepoURL, "README.md")); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo.RepoURL, "add", "-A")
	runTestGit(t, repo.RepoURL, "commit", "-m", "remove readme")
	run, stat := regressionScan(t, server, repo, "manual")
	if run.Status != "success" || stat.Mode != "incremental" || run.FileCount != 0 || stat.Reused != 2 || stat.HistoryEvents != 1 {
		t.Fatalf("deletion increment = %+v, %+v", run, stat)
	}
	stored, err := getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 2 {
		t.Fatalf("deleted document still visible: %+v, %v", stored, err)
	}
	// 不依赖 updated_at 的秒精度；同 HEAD 下修改全局预览上限也必须重新索引。
	scannerSQL(t, server, `UPDATE repositories SET max_file_size_bytes=1`)
	run, stat = regressionScan(t, server, repo, "manual")
	if stat.Mode != "full" || stat.SkippedByHead || run.FileCount != 1 || run.SkippedCount != 1 {
		t.Fatalf("changed rules must rebuild = %+v", run)
	}
	doc, err := getScanPathTestVersion(context.Background(), server, repo.ID, "doc/a.md")
	if err != nil || doc.Previewable {
		t.Fatalf("new preview rules not applied = %+v, %v", doc, err)
	}
}

func TestScannerForcePushFallsBackToFull(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	// 操作完全独立的临时测试源仓库，模拟没有祖先关系的强推。
	runTestGit(t, repo.RepoURL, "checkout", "--orphan", "replacement")
	runTestGit(t, repo.RepoURL, "rm", "-rf", ".")
	writeScanPathTestFile(t, repo.RepoURL, "doc/new.md", "# Replacement\n")
	runTestGit(t, repo.RepoURL, "add", ".")
	runTestGit(t, repo.RepoURL, "commit", "-m", "unrelated history")
	runTestGit(t, repo.RepoURL, "branch", "-M", "main")
	run, stat := regressionScan(t, server, repo, "manual")
	if run.Status != "success" || stat.Mode != "full" || run.FileCount != 1 {
		t.Fatalf("unrelated history = %+v", run)
	}
	stored, err := getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 1 {
		t.Fatalf("old force-pushed files remain visible = %+v, %v", stored, err)
	}
}

func TestScannerUnchangedRefreshDoesNotRewriteLatest(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	scannerSQL(t, server, `CREATE TRIGGER reject_unchanged_latest BEFORE INSERT ON doc_latest
		BEGIN SELECT RAISE(ABORT, 'unchanged latest must not be rebuilt'); END`)
	run, stat := regressionScan(t, server, repo, "manual")
	if run.Status != "success" || !stat.SkippedByHead || run.FileCount != 0 {
		t.Fatalf("unchanged refresh rewrote latest view: %+v", run)
	}
}

func TestScannerRemovedBranchLeavesLatestView(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	scannerSQL(t, server, `UPDATE repositories SET tracked_branches='["*"]',latest_include_branches='["*"]'`)
	runTestGit(t, repo.RepoURL, "checkout", "-b", "side")
	writeScanPathTestFile(t, repo.RepoURL, "doc/side-only.md", "# Side Only\n")
	runTestGit(t, repo.RepoURL, "add", ".")
	runTestGit(t, repo.RepoURL, "commit", "-m", "side-only document")
	runTestGit(t, repo.RepoURL, "checkout", "main")
	run, _ := regressionScan(t, server, repo, "manual")
	stored, err := getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 4 || run.BranchCount != 2 {
		t.Fatalf("initial multi-branch view = %+v, %v", stored, err)
	}
	runTestGit(t, repo.RepoURL, "branch", "-D", "side")
	run, stat := regressionScan(t, server, repo, "manual")
	stored, err = getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 3 || !stat.SkippedByHead || run.FileCount != 0 || run.BranchCount != 1 {
		t.Fatalf("removed branch kept latest documents: run=%+v repo=%+v err=%v", run, stored, err)
	}
}

func TestScannerGitPreparationDoesNotHoldWriter(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	dir := t.TempDir()
	entered, release := filepath.Join(dir, "entered"), filepath.Join(dir, "release")
	installScannerGitWrapper(t, server, "for arg in \"$@\"; do\n if [ \"$arg\" = '--name-only' ]; then\n  touch "+scannerShellQuote(entered)+"\n  while [ ! -f "+scannerShellQuote(release)+" ]; do sleep 0.01; done\n fi\ndone\n")
	done := make(chan error, 1)
	go func() {
		_, err := server.scanner.Scan(context.Background(), repo.ID, "manual")
		done <- err
	}()
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not enter blocked metadata preparation")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := server.db.ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatalf("Git preparation occupied writer connection: %v", err)
	}
	r := httptest.NewRecorder()
	server.Routes().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/repos", nil).WithContext(ctx))
	if r.Code != http.StatusOK {
		t.Fatalf("repository read during preparation: %d %s", r.Code, r.Body.String())
	}
	var body struct {
		Items []Repository `json:"items"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Items[0].LatestScan == nil || body.Items[0].LatestScan.Status != "running" {
		t.Fatalf("running progress unavailable: %s, %v", r.Body.String(), err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released scan did not finish")
	}
}

func TestReassignPathEventsIsIdempotent(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	ctx := context.Background()
	tx, err := server.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	from, err := findOrCreateDocument(ctx, tx, repo.ID, ".", "old.md", "main", "commit", "Old", "old.md")
	if err != nil {
		t.Fatal(err)
	}
	to, err := findOrCreateDocument(ctx, tx, repo.ID, ".", "new.md", "main", "commit", "New", "new.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{from, to} {
		if err := insertPathEvent(ctx, tx, PathEvent{RepoID: repo.ID, DocumentID: id, Branch: "main", EventType: "renamed", OldPath: "old.md", NewPath: "new.md", CommitSHA: "commit", CommitTime: nowString()}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := reassignPathEvents(ctx, tx, repo.ID, "main", from, to); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doc_path_events WHERE repo_id=? AND document_id=?`, repo.ID, to).Scan(&count); err != nil || count != 1 {
		t.Fatalf("events after repeat reassignment = %d, %v", count, err)
	}
}

func scannerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func installScannerGitWrapper(t *testing.T, server *Server, prelude string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "git-scanner-wrapper")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"+prelude+"exec "+scannerShellQuote(realGit)+" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	server.scanner.git.cfg.GitBin = wrapper
}
