package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestScannerStateNoTrackedBranches(t *testing.T) {
	s, repo := newScannerRegressionRepo(t)
	regressionScan(t, s, repo, "manual")
	if _, err := updateRepository(context.Background(), s.db, repo.ID, Repository{TrackedBranches: []string{"missing"}}); err != nil {
		t.Fatal(err)
	}
	run, _ := s.scanner.Scan(context.Background(), repo.ID, "manual")
	files := scannerIdentityFiles(t, s, repo.ID, "latest", "doc")
	var candidates int
	if err := s.db.QueryRow(`SELECT count(*) FROM doc_versions WHERE repo_id=? AND participates_latest=1`, repo.ID).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || candidates != 0 || run.Status != "failed" {
		t.Fatalf("untracked versions remain published: run=%+v files=%+v candidates=%d", run, files, candidates)
	}
}

func TestScannerStateRecreatedOriginalPath(t *testing.T) {
	for _, modifyRenamed := range []bool{false, true} {
		t.Run(strconv.FormatBool(modifyRenamed), func(t *testing.T) {
			s, repo := newScannerRegressionRepo(t)
			regressionScan(t, s, repo, "manual")
			original := scannerIdentityVersion(t, s, repo.ID, "main", "doc/a.md")
			runTestGit(t, repo.RepoURL, "mv", "doc/a.md", "doc/b.md")
			scannerIdentityCommit(t, repo, "rename a to b")
			regressionScan(t, s, repo, "manual")
			writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Independent replacement at original path\n")
			if modifyRenamed {
				writeScanPathTestFile(t, repo.RepoURL, "doc/b.md", "# Changed renamed document\n")
			}
			scannerIdentityCommit(t, repo, "new file at original a")
			run, _ := regressionScan(t, s, repo, "manual")
			files := scannerIdentityFiles(t, s, repo.ID, "branch&branch=main", "doc")
			if run.Status != "success" || len(files) != 2 || files["doc/b.md"].DocumentID != original.DocumentID || files["doc/a.md"].DocumentID == original.DocumentID {
				t.Fatalf("original path reused a different current file identity: %+v / %+v", run, files)
			}
			// 成功检查点之后再次刷新不能掩盖已经丢失的文件。
			regressionScan(t, s, repo, "manual")
			if len(scannerIdentityFiles(t, s, repo.ID, "latest", "doc")) != 2 {
				t.Fatal("unchanged refresh lost recreated original path")
			}
		})
	}
}

func TestScannerStatePartialRenameThenRenameAgain(t *testing.T) {
	s, repo := newScannerRegressionRepo(t)
	regressionScan(t, s, repo, "manual")
	original := scannerIdentityVersion(t, s, repo.ID, "main", "doc/a.md")
	runTestGit(t, repo.RepoURL, "mv", "doc/a.md", "doc/b.md")
	writeScanPathTestFile(t, repo.RepoURL, "src/app.go", "package main\n// changed\n")
	scannerIdentityCommit(t, repo, "rename with unrelated failure")
	scannerSQL(t, s, `CREATE TRIGGER reject_state_update BEFORE INSERT ON doc_versions
		WHEN NEW.file_path='src/app.go' BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	run, _ := regressionScan(t, s, repo, "manual")
	if run.Status != "partial_success" {
		t.Fatalf("expected partial commit: %+v", run)
	}
	scannerSQL(t, s, `DROP TRIGGER reject_state_update`)
	runTestGit(t, repo.RepoURL, "mv", "doc/b.md", "doc/c.md")
	scannerIdentityCommit(t, repo, "rename again before retry")
	run, _ = regressionScan(t, s, repo, "manual")
	files := scannerIdentityFiles(t, s, repo.ID, "branch&branch=main", "doc")
	if run.Status != "success" || files["doc/c.md"].DocumentID != original.DocumentID {
		t.Fatalf("partial retry lost document identity: %+v / %+v", run, files)
	}
}

func TestScannerStateOversizedFilesObeyExclusions(t *testing.T) {
	s, repo := newScannerRegressionRepo(t)
	regressionScan(t, s, repo, "manual")
	if _, err := updateRepository(context.Background(), s.db, repo.ID, Repository{MaxFileSizeBytes: 1,
		ScanPaths: []ScanPath{{Path: ".", Enabled: true, ExcludeGlobs: []string{"doc/**"}}}}); err != nil {
		t.Fatal(err)
	}
	run, _ := regressionScan(t, s, repo, "manual")
	if files := scannerIdentityFiles(t, s, repo.ID, "latest", "doc"); run.Status != "success" || len(files) != 0 {
		t.Fatalf("oversized file bypassed exclude rules: %+v / %+v", run, files)
	}
}

func TestScannerStateAllBranchesRemoved(t *testing.T) {
	s, repo := newScannerRegressionRepo(t)
	regressionScan(t, s, repo, "manual")
	// 只删除临时测试源的唯一分支，不操作用户仓库或远端。
	bare := filepath.Join(t.TempDir(), "source.git")
	runTestGit(t, repo.RepoURL, "clone", "--bare", repo.RepoURL, bare)
	runTestGit(t, bare, "update-ref", "-d", "refs/heads/main")
	runTestGit(t, s.scanner.git.repoPath(repo.ID), "remote", "set-url", "origin", bare)
	_, _ = s.scanner.Scan(context.Background(), repo.ID, "manual")
	if files := scannerIdentityFiles(t, s, repo.ID, "latest", "doc"); len(files) != 0 {
		t.Fatalf("removed sole branch remains visible: %+v", files)
	}
}

func TestScannerStateCancelAfterCommittedBranch(t *testing.T) {
	s, repo := newScannerRegressionRepo(t)
	scannerSQL(t, s, `UPDATE repositories SET tracked_branches='["main","topic"]',latest_include_branches='["main","topic"]'`)
	runTestGit(t, repo.RepoURL, "branch", "topic")
	regressionScan(t, s, repo, "manual")
	if err := os.MkdirAll(filepath.Join(repo.RepoURL, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo.RepoURL, "mv", "doc/a.md", "other/b.md")
	mainHead := scannerIdentityCommit(t, repo, "move main document")
	runTestGit(t, repo.RepoURL, "checkout", "topic")
	writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Topic update\n")
	topicHead := scannerIdentityCommit(t, repo, "change topic")
	runTestGit(t, repo.RepoURL, "checkout", "main")
	marker := filepath.Join(t.TempDir(), "entered")
	installScannerGitWrapper(t, s, "is_metadata=0\nis_topic=0\nfor arg in \"$@\"; do\n [ \"$arg\" != '--name-only' ] || is_metadata=1\n [ \"$arg\" != "+scannerShellQuote(topicHead)+" ] || is_topic=1\ndone\nif [ \"$is_metadata$is_topic\" = '11' ]; then\n touch "+scannerShellQuote(marker)+"\n while :; do sleep 0.01; done\nfi\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan ScanRun, 1)
	go func() { run, _ := s.scanner.Scan(ctx, repo.ID, "manual"); done <- run }()
	waitScanJob(t, func() bool { _, err := os.Stat(marker); return err == nil })
	ref, err := getRepoRef(context.Background(), s.db, repo.ID, "branch", "main")
	if err != nil || ref.CommitSHA != mainHead {
		t.Fatalf("first branch did not commit: %+v / %v", ref, err)
	}
	if _, err := s.scanner.Cancel(repo.ID); err != nil {
		t.Fatal(err)
	}
	<-done
	target := scannerIdentityFiles(t, s, repo.ID, "latest", "other")
	if len(target) != 1 {
		t.Fatalf("committed renamed document not in target directory after cancel: %+v", target)
	}
	for _, file := range target {
		var content FileContent
		scannerIdentityGetJSON(t, s, "/api/repos/1/versions/"+strconv.FormatInt(file.VersionID, 10)+"/content", &content)
		if file.Path != content.FilePath || file.SourceCommitSHA != content.SourceCommitSHA {
			t.Fatalf("latest and version diverged after cancel: %+v / %+v", file, content)
		}
	}
}
