package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 使用真实 Git 临时仓库与 SQLite 触发器，验证失败后的持久状态，而非仅测试统计函数。
func newScannerRegressionRepo(t *testing.T) (*Server, Repository) {
	t.Helper()
	requireGit(t)
	source, _ := createScanPathGitRepo(t)
	server := newWebhookTestServer(t)
	repo, err := createRepository(context.Background(), server.db, Repository{
		Name: "Scan regression", Slug: "scan-regression", RepoURL: source,
		DefaultBranch: "main", TrackedBranches: []string{"main"},
		LatestIncludeBranches: []string{"main"}, ScanPaths: []ScanPath{{Path: ".", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 时间戳精度为秒；固定配置时间避免快速连续扫描被判定为同秒配置变更。
	scannerSQL(t, server, `UPDATE repo_scan_paths SET updated_at = '2000-01-01T00:00:00Z'`)
	return server, repo
}

func scannerSQL(t *testing.T, server *Server, query string) {
	t.Helper()
	if _, err := server.db.ExecContext(context.Background(), query); err != nil {
		t.Fatal(err)
	}
}

func regressionScan(t *testing.T, server *Server, repo Repository, trigger string) (ScanRun, branchScanStat) {
	t.Helper()
	run, err := server.scanner.Scan(context.Background(), repo.ID, trigger)
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Branches map[string]branchScanStat `json:"branches"`
	}
	if err := json.Unmarshal([]byte(run.DetailJSON), &detail); err != nil {
		t.Fatal(err)
	}
	// 回读数据库，确保最终统计和详情确实持久化，而不只是函数返回值正确。
	stored, err := latestScanRun(context.Background(), server.db, repo.ID)
	if err != nil || stored == nil || stored.FileCount != run.FileCount || stored.ErrorCount != run.ErrorCount || stored.DetailJSON != run.DetailJSON {
		t.Fatalf("stored scan = %+v, err = %v; returned = %+v", stored, err, run)
	}
	return run, detail.Branches["main"]
}

func TestScannerPartialCountsAndSameHeadRetry(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	scannerSQL(t, server, `CREATE TRIGGER reject_two_files BEFORE INSERT ON doc_versions
		WHEN NEW.file_path IN ('README.md', 'src/app.go')
		BEGIN SELECT RAISE(ABORT, 'injected version failure'); END`)
	for _, trigger := range []string{"github_webhook", "scheduled"} {
		run, stat := regressionScan(t, server, repo, trigger)
		if run.Status != "partial_success" || run.FileCount != 1 || run.ErrorCount != 2 || stat.Files != 1 || stat.Errors != 2 || stat.SkippedByHead {
			t.Fatalf("partial scan = %+v, branch = %+v", run, stat)
		}
		if len(stat.ErrorDetails) != 2 || stat.ErrorDetails[0].Stage != "写入文件版本" || stat.ErrorDetails[0].Path != "README.md" || !strings.Contains(run.ErrorMessage, "injected version failure") {
			t.Fatalf("missing useful errors: %+v", run)
		}
	}
	scannerSQL(t, server, `DROP TRIGGER reject_two_files`)
	run, stat := regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" || run.FileCount != 3 || stat.SkippedByHead {
		t.Fatalf("same HEAD did not recover: %+v", run)
	}
	run, stat = regressionScan(t, server, repo, "scheduled")
	if run.FileCount != 0 || !stat.SkippedByHead {
		t.Fatalf("unchanged successful HEAD should skip: %+v", run)
	}
	stored, err := getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 3 {
		t.Fatalf("document count after skip = %d, err = %v", stored.DocumentCount, err)
	}
	repos, err := listRepositories(context.Background(), server.db)
	if err != nil || len(repos) != 1 || repos[0].DocumentCount != 3 {
		t.Fatalf("repository list = %+v, err = %v", repos, err)
	}
	run, stat = regressionScan(t, server, repo, "manual")
	if run.FileCount != 3 || stat.SkippedByHead {
		t.Fatalf("manual scan should recheck even a legacy successful checkpoint: %+v", run)
	}
	// 目录变更立即影响可见总数，不需要等待下次扫描清理旧索引。
	stored, err = updateRepository(context.Background(), server.db, repo.ID, Repository{ScanPaths: []ScanPath{{Path: "doc", Enabled: true}}})
	if err != nil || stored.DocumentCount != 1 {
		t.Fatalf("scoped document count = %d, err = %v", stored.DocumentCount, err)
	}
}

func TestScannerRolledBackFilesAreNotCountedAndRetry(t *testing.T) {
	for _, previousSuccess := range []bool{false, true} {
		t.Run(fmt.Sprintf("previous_success_%t", previousSuccess), func(t *testing.T) {
			server, repo := newScannerRegressionRepo(t)
			if previousSuccess {
				regressionScan(t, server, repo, "manual")
			}
			// 文件已处理后，让保存分支状态失败，验证整笔索引事务回滚。
			scannerSQL(t, server, `CREATE TRIGGER reject_checkpoint BEFORE INSERT ON repo_refs
				BEGIN SELECT RAISE(ABORT, 'injected checkpoint failure'); END`)
			run, stat := regressionScan(t, server, repo, "manual")
			if run.Status != "partial_success" || run.FileCount != 0 || stat.Files != 0 || run.ErrorCount != 1 || !strings.Contains(run.ErrorMessage, "injected checkpoint failure") {
				t.Fatalf("rolled back scan = %+v", run)
			}
			var count int
			if err := server.db.QueryRow(`SELECT COUNT(*) FROM doc_versions`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (!previousSuccess && count != 0) || (previousSuccess && count != 3) {
				t.Fatalf("rollback changed existing versions: %d", count)
			}
			scannerSQL(t, server, `DROP TRIGGER reject_checkpoint`)
			run, stat = regressionScan(t, server, repo, "scheduled")
			if run.Status != "success" || run.FileCount != 3 || stat.SkippedByHead {
				t.Fatalf("rollback must invalidate old successful HEAD: %+v", run)
			}
		})
	}
}

func TestScannerGitStageFailuresKeepDocumentsAndRetry(t *testing.T) {
	for _, failure := range []struct {
		arg, stage, path string
		files, errors    int
	}{
		{"ls-tree", "枚举目录", ".", 0, 1},
		{"--format=%x1e%H%x00%cI", "回填路径历史", ".", 3, 1},
		{"-1", "读取文件版本", "README.md", 0, 3},
		{"cat-file", "读取文件版本", "README.md", 1, 2},
	} {
		t.Run(failure.stage, func(t *testing.T) {
			server, repo := newScannerRegressionRepo(t)
			regressionScan(t, server, repo, "manual")
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(t.TempDir(), "git-failure")
			// 只拦截目标只读命令，其他操作仍执行临时测试仓库上的真实 Git。
			script := "#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = '" + failure.arg + "' ]; then\n    echo 'injected git failure' >&2\n    exit 1\n  fi\ndone\nexec '" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\n"
			if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			server.scanner.git.cfg.GitBin = wrapper
			run, stat := regressionScan(t, server, repo, "manual")
			if run.Status != "partial_success" || run.FileCount != failure.files || run.ErrorCount != failure.errors || len(stat.ErrorDetails) != failure.errors || stat.ErrorDetails[0].Stage != failure.stage || stat.ErrorDetails[0].Path != failure.path {
				t.Fatalf("git failure = %+v", run)
			}
			stored, err := getRepository(context.Background(), server.db, repo.ID)
			if err != nil || stored.DocumentCount != 3 {
				t.Fatalf("failure removed existing documents: %+v, err = %v", stored, err)
			}
			server.scanner.git.cfg.GitBin = realGit
			run, stat = regressionScan(t, server, repo, "scheduled")
			if run.Status != "success" || run.FileCount != 3 || stat.SkippedByHead {
				t.Fatalf("git failure did not retry: %+v", run)
			}
		})
	}
}

func TestScannerManualRescanKeepsRenamedDocumentIdentity(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	original, err := getScanPathTestVersion(context.Background(), server, repo.ID, "doc/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo.RepoURL, "doc/a.md"), filepath.Join(repo.RepoURL, "doc/b.md")); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo.RepoURL, "add", "-A")
	runTestGit(t, repo.RepoURL, "commit", "-m", "rename regression document")
	// 先处理真实重命名，再多次重扫相同 HEAD：每次都应复用最初的文档身份。
	for _, trigger := range []string{"scheduled", "manual", "manual"} {
		run, stat := regressionScan(t, server, repo, trigger)
		if run.Status != "success" || run.FileCount != 3 || stat.SkippedByHead {
			t.Fatalf("rename/rescan = %+v", run)
		}
		renamed, err := getScanPathTestVersion(context.Background(), server, repo.ID, "doc/b.md")
		if err != nil || renamed.DocumentID != original.DocumentID {
			t.Fatalf("document identity changed: original=%d, renamed=%+v, err=%v", original.DocumentID, renamed, err)
		}
		stored, err := getRepository(context.Background(), server.db, repo.ID)
		if err != nil || stored.DocumentCount != 3 {
			t.Fatalf("rename/rescan created duplicate latest documents: %+v, err=%v", stored, err)
		}
		var count int
		if err := server.db.QueryRow(`SELECT COUNT(*) FROM doc_versions WHERE repo_id = ? AND branch = 'main' AND file_path = 'doc/b.md'`, repo.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("duplicate branch versions = %d, err = %v", count, err)
		}
	}
}

func TestScannerSkippedFilesAndLatestFailurePreserveBranchErrors(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	scannerSQL(t, server, `UPDATE repo_scan_paths SET exclude_globs = '["README.md"]'`)
	scannerSQL(t, server, `CREATE TRIGGER reject_source BEFORE INSERT ON doc_versions
		WHEN NEW.file_path = 'src/app.go' BEGIN SELECT RAISE(ABORT, 'injected source failure'); END`)
	scannerSQL(t, server, `CREATE TRIGGER reject_latest BEFORE INSERT ON doc_latest
		BEGIN SELECT RAISE(ABORT, 'injected latest failure'); END`)
	run, err := server.scanner.Scan(context.Background(), repo.ID, "manual")
	if err == nil || run.Status != "partial_success" || run.FileCount != 1 || run.SkippedCount != 1 || run.ErrorCount != 2 {
		t.Fatalf("branch and latest failures = %+v, err = %v", run, err)
	}
	for _, message := range []string{"injected source failure", "injected latest failure"} {
		if !strings.Contains(run.ErrorMessage, message) || !strings.Contains(run.DetailJSON, message) {
			t.Fatalf("failure detail was overwritten: %+v", run)
		}
	}
	// 最新视图重建整体回滚，失败期间保留先前的可读视图。
	stored, err := getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 3 || stored.LatestScan == nil || stored.LatestScan.ErrorCount != 2 {
		t.Fatalf("latest view after failure = %+v, err = %v", stored, err)
	}
	scannerSQL(t, server, `DROP TRIGGER reject_source`)
	scannerSQL(t, server, `DROP TRIGGER reject_latest`)
	run, stat := regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" || run.FileCount != 2 || run.SkippedCount != 1 || stat.SkippedByHead {
		t.Fatalf("failed latest/branch did not recover: %+v", run)
	}
	stored, err = getRepository(context.Background(), server.db, repo.ID)
	if err != nil || stored.DocumentCount != 2 {
		t.Fatalf("recovered document count = %d, err = %v", stored.DocumentCount, err)
	}
}
