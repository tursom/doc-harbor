package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 复制与重命名都有 old/new path，但只有重命名可以继承身份。特意同时修改源文件，
// 使真实 Git 的 --find-copies 稳定输出 C100，而不是只得到一个普通新增文件。
func TestScannerIdentityCopyDoesNotConsumeSource(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	original := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	oldContent := runTestGitOutput(t, repo.RepoURL, "show", "HEAD:doc/a.md")
	writeScanPathTestFile(t, repo.RepoURL, "doc/b.md", oldContent)
	writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Source after copy\n\nA completely different source document.\n")
	head := scannerIdentityCommit(t, repo, "copy old a to b and modify a")
	diff := runTestGitOutput(t, repo.RepoURL, "diff", "--name-status", "--find-renames", "--find-copies", original.HeadCommitSHA, head)
	if !strings.Contains(diff, "C100\tdoc/a.md\tdoc/b.md") {
		t.Fatalf("fixture must exercise a real C100: %s", diff)
	}
	run, _ := regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" {
		t.Fatalf("copy scan = %+v", run)
	}
	a := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	b := scannerIdentityVersion(t, server, repo.ID, "main", "doc/b.md")
	if a.DocumentID != original.DocumentID || b.DocumentID == a.DocumentID || b.PreviousPath != "" {
		t.Errorf("copy stole source identity: original=%+v a=%+v b=%+v", original, a, b)
	}
	if a.HeadCommitSHA != head || b.HeadCommitSHA != head {
		t.Errorf("copy snapshots do not match HEAD: a=%+v b=%+v", a, b)
	}
	scannerIdentityAssertContent(t, server, a, runTestGitOutput(t, repo.RepoURL, "show", "HEAD:doc/a.md"))
	scannerIdentityAssertContent(t, server, b, oldContent)
	for _, view := range []string{"latest", "branch&branch=main"} {
		files := scannerIdentityFiles(t, server, repo.ID, view, "doc")
		if len(files) != 2 || files["doc/a.md"].DocumentID != a.DocumentID || files["doc/b.md"].DocumentID != b.DocumentID {
			t.Errorf("copy lost an indexed file in %s: %+v", view, files)
		}
	}
	scannerIdentityAssertDocumentCount(t, server, repo.ID, 4)
}

// 目标文件准备失败和 upsert 失败发生在不同阶段；两者都不能把源版本当成真实删除。
// 内容接口按旧 blob 读取，下载接口也必须继续可用，且失败版本不能声称已属于新 HEAD。
func TestScannerIdentityFailedRenamePreservesSourceAndRetries(t *testing.T) {
	for _, failure := range []string{"markdown_read", "upsert"} {
		t.Run(failure, func(t *testing.T) {
			server, repo := newScannerRegressionRepo(t)
			regressionScan(t, server, repo, "manual")
			original := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
			oldContent := runTestGitOutput(t, repo.RepoURL, "show", "HEAD:doc/a.md")
			runTestGit(t, repo.RepoURL, "mv", "doc/a.md", "doc/b.md")
			head := scannerIdentityCommit(t, repo, "rename with a failing target")
			realGit := server.scanner.git.cfg.GitBin
			if failure == "markdown_read" {
				// 只拒绝扫描标题使用的 batch；内容/下载 API 的 cat-file -p 仍走真实 Git。
				installScannerGitWrapper(t, server, "for arg in \"$@\"; do\n if [ \"$arg\" = '--batch' ]; then\n  echo 'injected rename markdown read failure' >&2\n  exit 1\n fi\ndone\n")
			} else {
				scannerSQL(t, server, `CREATE TRIGGER reject_identity_rename BEFORE INSERT ON doc_versions
					WHEN NEW.file_path = 'doc/b.md'
					BEGIN SELECT RAISE(ABORT, 'injected rename upsert failure'); END`)
			}
			run, stat := regressionScan(t, server, repo, "scheduled")
			wantStage := "读取文件版本"
			if failure == "upsert" {
				wantStage = "写入文件版本"
			}
			// batch 协议可能先读到 EOF，不能依赖包装脚本 stderr 的原样透传；
			// 用持久化的阶段和路径确认确实在目标 b 上触发了本轮故障。
			if run.Status != "partial_success" || stat.Errors != 1 || len(stat.ErrorDetails) != 1 || stat.ErrorDetails[0].Stage != wantStage || stat.ErrorDetails[0].Path != "doc/b.md" {
				t.Errorf("rename failure was not reported: run=%+v stat=%+v", run, stat)
			}
			stored, err := getVersion(context.Background(), server.db, repo.ID, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status == "deleted" || stored.FilePath != original.FilePath || stored.BlobSHA != original.BlobSHA || stored.HeadCommitSHA != original.HeadCommitSHA {
				t.Errorf("failed rename changed the source snapshot: before=%+v after=%+v", original, stored)
			}
			scannerIdentityAssertContent(t, server, stored, oldContent)
			for _, view := range []string{"latest", "branch&branch=main"} {
				files := scannerIdentityFiles(t, server, repo.ID, view, "doc")
				if len(files) != 1 || files["doc/a.md"].DocumentID != original.DocumentID {
					t.Errorf("failed rename hid old source in %s: %+v", view, files)
				}
			}
			scannerIdentityAssertInvalidCheckpoint(t, server, repo.ID, original.HeadCommitSHA)
			// 不修改 HEAD，仅撤销故障；普通定时扫描必须重试，并把同一身份移至 b。
			server.scanner.git.cfg.GitBin = realGit
			if failure == "upsert" {
				scannerSQL(t, server, `DROP TRIGGER reject_identity_rename`)
			}
			run, stat = regressionScan(t, server, repo, "scheduled")
			if run.Status != "success" || stat.SkippedByHead {
				t.Fatalf("same HEAD rename retry = %+v, %+v", run, stat)
			}
			renamed := scannerIdentityVersion(t, server, repo.ID, "main", "doc/b.md")
			if renamed.DocumentID != original.DocumentID || renamed.HeadCommitSHA != head || renamed.BlobSHA != original.BlobSHA {
				t.Errorf("rename retry did not preserve identity: original=%+v renamed=%+v", original, renamed)
			}
			scannerIdentityAssertContent(t, server, renamed, oldContent)
			scannerIdentityAssertDocumentCount(t, server, repo.ID, 3)
			scannerIdentityAssertSuccessfulCheckpoint(t, server, repo.ID, head)
		})
	}
}

// 一个文件写失败不能污染其它文件的成功快照，也不能通过批量 UPDATE 把失败文件的
// 旧 blob 搭配新 head 发布。README 没有修改，用来单独覆盖“确认复用”的推进路径。
func TestScannerIdentityPartialUpsertAdvancesOnlySuccessfulSnapshots(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	failedBefore := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	reusedBefore := scannerIdentityVersion(t, server, repo.ID, "main", "README.md")
	oldContent := runTestGitOutput(t, repo.RepoURL, "show", "HEAD:doc/a.md")
	writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# Rejected update\n")
	writeScanPathTestFile(t, repo.RepoURL, "src/app.go", "package main\n\nfunc main() { println(\"updated\") }\n")
	head := scannerIdentityCommit(t, repo, "one rejected and one accepted update")
	scannerSQL(t, server, `CREATE TRIGGER reject_identity_update BEFORE INSERT ON doc_versions
		WHEN NEW.file_path = 'doc/a.md'
		BEGIN SELECT RAISE(ABORT, 'injected ordinary upsert failure'); END`)
	run, stat := regressionScan(t, server, repo, "manual")
	if run.Status != "partial_success" || run.FileCount != 1 || stat.Reused != 1 || stat.Errors != 1 || !strings.Contains(run.ErrorMessage, "injected ordinary upsert failure") {
		t.Errorf("partial update = %+v, %+v", run, stat)
	}
	failed := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	if failed.DocumentID != failedBefore.DocumentID || failed.BlobSHA != failedBefore.BlobSHA || failed.HeadCommitSHA != failedBefore.HeadCommitSHA || failed.LastCommitSHA != failedBefore.LastCommitSHA {
		t.Errorf("rejected update advanced an old snapshot: before=%+v after=%+v", failedBefore, failed)
	}
	scannerIdentityAssertContent(t, server, failed, oldContent)
	successful := scannerIdentityVersion(t, server, repo.ID, "main", "src/app.go")
	wantBlob := strings.TrimSpace(runTestGitOutput(t, repo.RepoURL, "rev-parse", "HEAD:src/app.go"))
	if successful.HeadCommitSHA != head || successful.LastCommitSHA != head || successful.BlobSHA != wantBlob {
		t.Errorf("accepted update did not advance: %+v", successful)
	}
	scannerIdentityAssertContent(t, server, successful, runTestGitOutput(t, repo.RepoURL, "show", "HEAD:src/app.go"))
	reused := scannerIdentityVersion(t, server, repo.ID, "main", "README.md")
	if reused.HeadCommitSHA != head || reused.BlobSHA != reusedBefore.BlobSHA || reused.LastCommitSHA != reusedBefore.LastCommitSHA {
		t.Errorf("unchanged file was not reused at current HEAD: before=%+v after=%+v", reusedBefore, reused)
	}
	scannerIdentityAssertContent(t, server, reused, "# Project README\n")
	scannerIdentityAssertInvalidCheckpoint(t, server, repo.ID, failedBefore.HeadCommitSHA)
	scannerSQL(t, server, `DROP TRIGGER reject_identity_update`)
	run, stat = regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" || stat.SkippedByHead {
		t.Fatalf("ordinary update retry = %+v, %+v", run, stat)
	}
	failed = scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	if failed.DocumentID != failedBefore.DocumentID || failed.HeadCommitSHA != head {
		t.Errorf("ordinary retry changed identity or retained old HEAD: %+v", failed)
	}
	scannerIdentityAssertContent(t, server, failed, "# Rejected update\n")
	scannerIdentityAssertSuccessfulCheckpoint(t, server, repo.ID, head)
}

// 先让 topic 删除 a，再把另一个已索引文档 rename 到 a。这样同一路径在两个分支
// 确实属于不同身份，不需要手工伪造 document_id。仅固定更新时间，稳定复现错误的
// 跨分支 ORDER BY updated_at DESC；main 后续重命名必须查 main 自己的源身份。
func TestScannerIdentityRenamePrefersSourceBranch(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	scannerSQL(t, server, `UPDATE repositories SET tracked_branches='["main","topic"]',latest_include_branches='["main","topic"]'`)
	runTestGit(t, repo.RepoURL, "checkout", "-b", "topic")
	writeScanPathTestFile(t, repo.RepoURL, "doc/other.md", "# Independent topic document\n")
	scannerIdentityCommit(t, repo, "create a distinct topic identity")
	runTestGit(t, repo.RepoURL, "checkout", "main")
	regressionScan(t, server, repo, "manual")
	mainBefore := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	topicBefore := scannerIdentityVersion(t, server, repo.ID, "topic", "doc/other.md")
	if mainBefore.DocumentID == topicBefore.DocumentID {
		t.Fatal("fixture did not create independent document identities")
	}
	runTestGit(t, repo.RepoURL, "checkout", "topic")
	runTestGit(t, repo.RepoURL, "rm", "doc/a.md")
	scannerIdentityCommit(t, repo, "delete topic original a")
	regressionScan(t, server, repo, "scheduled")
	runTestGit(t, repo.RepoURL, "mv", "doc/other.md", "doc/a.md")
	scannerIdentityCommit(t, repo, "move independent topic identity to a")
	regressionScan(t, server, repo, "scheduled")
	topicA := scannerIdentityVersion(t, server, repo.ID, "topic", "doc/a.md")
	if topicA.DocumentID != topicBefore.DocumentID {
		t.Fatalf("fixture topic rename lost its own identity: before=%+v after=%+v", topicBefore, topicA)
	}
	scannerSQL(t, server, `UPDATE doc_versions SET updated_at = CASE branch
		WHEN 'topic' THEN '2099-01-01T00:00:00Z' ELSE '2000-01-01T00:00:00Z' END`)
	runTestGit(t, repo.RepoURL, "checkout", "main")
	runTestGit(t, repo.RepoURL, "mv", "doc/a.md", "doc/b.md")
	head := scannerIdentityCommit(t, repo, "rename main own a to b")
	run, _ := regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" {
		t.Fatalf("main rename = %+v", run)
	}
	mainB := scannerIdentityVersion(t, server, repo.ID, "main", "doc/b.md")
	if mainB.DocumentID != mainBefore.DocumentID || mainB.DocumentID == topicA.DocumentID {
		t.Errorf("main inherited the newer topic identity: mainBefore=%+v topicA=%+v mainB=%+v", mainBefore, topicA, mainB)
	}
	topicA = scannerIdentityVersion(t, server, repo.ID, "topic", "doc/a.md")
	if topicA.DocumentID != topicBefore.DocumentID {
		t.Errorf("main rename changed topic identity: %+v", topicA)
	}
	scannerIdentityAssertContent(t, server, mainB, "# Alpha Guide\n\nv2\n")
	scannerIdentityAssertContent(t, server, topicA, "# Independent topic document\n")
	var renameEvents []PathEvent
	for _, event := range scannerIdentityEvents(t, server, repo.ID, "main") {
		if event.CommitSHA == head && event.OldPath == "doc/a.md" && event.NewPath == "doc/b.md" {
			renameEvents = append(renameEvents, event)
		}
	}
	if len(renameEvents) != 1 || renameEvents[0].DocumentID != mainBefore.DocumentID {
		t.Errorf("main rename history missing or attached to topic document: %+v", renameEvents)
	}
}

// 聚合 diff 仍是 a->b，但 b 的最近提交已经是 M。路径事件必须来自真实历史 R，
// 不能用文件版本的 last_commit_sha 再合成一个发生在 M 的重命名事件。
func TestScannerIdentityRenameHistoryUsesActualRenameCommit(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	original := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	runTestGit(t, repo.RepoURL, "mv", "doc/a.md", "doc/b.md")
	renameCommit := scannerIdentityCommit(t, repo, "R rename a to b")
	// 原文件很短，追加少量文字，确保聚合相似度仍超过 Git 的 50% rename 阈值。
	writeScanPathTestFile(t, repo.RepoURL, "doc/b.md", "# Alpha Guide\n\nv2\n\nv3\n")
	modifyCommit := scannerIdentityCommit(t, repo, "M modify b after rename")
	if diff := runTestGitOutput(t, repo.RepoURL, "diff", "--name-status", "--find-renames", original.HeadCommitSHA, modifyCommit); !strings.Contains(diff, "\tdoc/a.md\tdoc/b.md") {
		t.Fatalf("fixture aggregate diff must remain a rename: %s", diff)
	}
	run, _ := regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" {
		t.Fatalf("rename then modification scan = %+v", run)
	}
	version := scannerIdentityVersion(t, server, repo.ID, "main", "doc/b.md")
	if version.DocumentID != original.DocumentID || version.LastCommitSHA != modifyCommit {
		t.Errorf("version must retain identity and record M as last change: %+v", version)
	}
	var renames []PathEvent
	for _, event := range scannerIdentityEvents(t, server, repo.ID, "main") {
		if event.OldPath == "doc/a.md" && event.NewPath == "doc/b.md" {
			renames = append(renames, event)
		}
	}
	if len(renames) != 1 || renames[0].CommitSHA != renameCommit || renames[0].DocumentID != original.DocumentID || renames[0].EventType != "renamed" {
		t.Errorf("API must expose only the actual R event, R=%s M=%s events=%+v", renameCommit, modifyCommit, renames)
	}
	var count int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM doc_path_events WHERE repo_id=? AND branch='main'
		AND event_type IN ('renamed','moved') AND commit_sha=?`, repo.ID, modifyCommit).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("SQLite contains %d synthetic rename events at modification commit M", count)
	}
	scannerIdentityAssertContent(t, server, version, runTestGitOutput(t, repo.RepoURL, "show", "HEAD:doc/b.md"))
}

// 强制 full 仍依赖 diff 判断身份，命令失败不能被当作“没有 rename”而写成功检查点。
// 这里仅修改普通文件，将故障与真实删除隔离，并检查既有文档仍能列举、读取和重试。
func TestScannerIdentityFullDiffFailureCannotCheckpoint(t *testing.T) {
	server, repo := newScannerRegressionRepo(t)
	regressionScan(t, server, repo, "manual")
	original := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	before := []DocVersion{original,
		scannerIdentityVersion(t, server, repo.ID, "main", "README.md"),
		scannerIdentityVersion(t, server, repo.ID, "main", "src/app.go"),
	}
	writeScanPathTestFile(t, repo.RepoURL, "src/app.go", "package main\n\nfunc main() { println(\"full rebuild\") }\n")
	head := scannerIdentityCommit(t, repo, "update before forced full diff failure")
	realGit := server.scanner.git.cfg.GitBin
	installScannerGitWrapper(t, server, "for arg in \"$@\"; do\n if [ \"$arg\" = 'diff' ]; then\n  echo 'injected full diff failure' >&2\n  exit 1\n fi\ndone\n")
	run, stat := regressionScan(t, server, repo, "rebuild")
	if (run.Status != "partial_success" && run.Status != "failed") || run.ErrorCount == 0 || stat.Mode != "full" || !strings.Contains(run.ErrorMessage, "injected full diff failure") {
		t.Errorf("full rebuild swallowed diff failure: run=%+v stat=%+v", run, stat)
	}
	scannerIdentityAssertInvalidCheckpoint(t, server, repo.ID, original.HeadCommitSHA)
	stored := scannerIdentityVersion(t, server, repo.ID, "main", "doc/a.md")
	if stored.DocumentID != original.DocumentID {
		t.Errorf("failed full rebuild replaced existing identity: before=%+v after=%+v", original, stored)
	}
	scannerIdentityAssertContent(t, server, stored, "# Alpha Guide\n\nv2\n")
	// 除总数外逐个核对原来的三个身份，防止“丢一份又新建一份”恰好抵消计数。
	// 部分成功允许普通文件已更新，也允许保留旧快照，二者都必须能按持久 blob 读取。
	for _, old := range before {
		current := scannerIdentityVersion(t, server, repo.ID, "main", old.FilePath)
		if current.DocumentID != old.DocumentID {
			t.Errorf("full diff failure lost an existing document: before=%+v after=%+v", old, current)
		}
		scannerIdentityAssertContent(t, server, current, runTestGitOutput(t, repo.RepoURL, "show", current.BlobSHA))
	}
	scannerIdentityAssertDocumentCount(t, server, repo.ID, 3)
	if files := scannerIdentityFiles(t, server, repo.ID, "latest", "doc"); len(files) != 1 || files["doc/a.md"].DocumentID != original.DocumentID {
		t.Errorf("full diff failure lost latest documents: %+v", files)
	}
	server.scanner.git.cfg.GitBin = realGit
	run, stat = regressionScan(t, server, repo, "scheduled")
	if run.Status != "success" || stat.SkippedByHead {
		t.Fatalf("full diff failure did not retry at unchanged HEAD: %+v, %+v", run, stat)
	}
	scannerIdentityAssertSuccessfulCheckpoint(t, server, repo.ID, head)
}

// 第一分支写入后才阻塞第二分支 metadata：只在分支写事务核对规则仍不够，最终
// doc_latest 发布事务也必须核对。通过 HTTP 修改真实配置，比较 SQLite latest 快照，
// 避免仅凭 Scan 返回错误就误判“没有发布旧规则”。两个分支的文档提交时间固定相同，
// 因而正常重试时 branch_priority 一定实际决定赢家。
func TestScannerIdentityConfigurationChangeCannotPublishStaleLatest(t *testing.T) {
	for _, change := range []string{"branch_priority", "scan_rules"} {
		t.Run(change, func(t *testing.T) {
			server, repo := newScannerRegressionRepo(t)
			scannerSQL(t, server, `UPDATE repositories SET tracked_branches='["main","topic"]',
				latest_include_branches='["main","topic"]',branch_priority='["main","topic"]'`)
			runTestGit(t, repo.RepoURL, "branch", "topic")
			regressionScan(t, server, repo, "manual")
			t.Setenv("GIT_AUTHOR_DATE", "2030-01-01T00:00:00Z")
			t.Setenv("GIT_COMMITTER_DATE", "2030-01-01T00:00:00Z")
			writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# New main content\n")
			mainHead := scannerIdentityCommit(t, repo, "main update before config change")
			runTestGit(t, repo.RepoURL, "checkout", "topic")
			writeScanPathTestFile(t, repo.RepoURL, "doc/a.md", "# New topic content\n")
			topicHead := scannerIdentityCommit(t, repo, "topic update before config change")
			runTestGit(t, repo.RepoURL, "checkout", "main")
			dir := t.TempDir()
			entered, release := filepath.Join(dir, "entered"), filepath.Join(dir, "release")
			installScannerGitWrapper(t, server, "is_metadata=0\nis_topic=0\nfor arg in \"$@\"; do\n"+
				" [ \"$arg\" != '--name-only' ] || is_metadata=1\n [ \"$arg\" != "+scannerShellQuote(topicHead)+" ] || is_topic=1\ndone\n"+
				"if [ \"$is_metadata$is_topic\" = '11' ]; then\n touch "+scannerShellQuote(entered)+"\n while [ ! -f "+scannerShellQuote(release)+" ]; do sleep 0.01; done\nfi\n")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type outcome struct {
				run ScanRun
				err error
			}
			done := make(chan outcome, 1)
			go func() {
				run, err := server.scanner.Scan(ctx, repo.ID, "manual")
				done <- outcome{run, err}
			}()
			// 即使断言提前终止，也先释放 wrapper，避免 Server.Close 等待挂起子进程。
			t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
			waitScanJob(t, func() bool { _, err := os.Stat(entered); return err == nil })
			scannerIdentityAssertSuccessfulCheckpoint(t, server, repo.ID, mainHead)
			// 已提交的第一分支现在原子发布 latest；配置变化后不能再覆盖这一已发布快照。
			before := scannerIdentityLatestSnapshot(t, server, repo.ID)
			patch := `{"branch_priority":["topic","main"]}`
			if change == "scan_rules" {
				patch = `{"scan_paths":[{"path":".","enabled":true,"exclude_globs":["doc/**"]}]}`
			}
			recorder := httptest.NewRecorder()
			server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/repos/%d", repo.ID), strings.NewReader(patch)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("config patch = %d %s", recorder.Code, recorder.Body.String())
			}
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			var result outcome
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal("released configuration-conflict scan did not finish")
			}
			if result.run.Status == "success" || result.run.ErrorCount == 0 || !strings.Contains(result.run.ErrorMessage, "规则") {
				t.Errorf("configuration conflict was not reported: run=%+v err=%v", result.run, result.err)
			}
			stored, err := latestScanRun(context.Background(), server.db, repo.ID)
			if err != nil || stored == nil || stored.Status != result.run.Status || stored.ErrorCount != result.run.ErrorCount || stored.FinishedAt == "" {
				t.Errorf("configuration-conflict result not persisted: stored=%+v err=%v returned=%+v", stored, err, result.run)
			}
			if after := scannerIdentityLatestSnapshot(t, server, repo.ID); after != before {
				t.Errorf("scan published latest using stale %s: before=%s after=%s", change, before, after)
			}
			// 无需回滚 Git 或再改配置：下一轮使用最新配置重建并完成发布。
			run, _ := regressionScan(t, server, repo, "scheduled")
			if run.Status != "success" {
				t.Fatalf("configuration retry = %+v", run)
			}
			files := scannerIdentityFiles(t, server, repo.ID, "latest", "doc")
			if change == "scan_rules" {
				if len(files) != 0 {
					t.Errorf("retry ignored new exclusion rule: %+v", files)
				}
				scannerIdentityAssertDocumentCount(t, server, repo.ID, 2)
			} else if len(files) != 1 || files["doc/a.md"].SourceBranch != "topic" || files["doc/a.md"].SourceCommitSHA != topicHead {
				t.Errorf("retry ignored new branch priority: %+v", files)
			}
		})
	}
}

// 以下 helper 只封装测试夹具和只读观察，不调用生产身份分配/路径历史写函数，
// 因而不会把待验证的身份提前写成预期结果。
func scannerIdentityCommit(t *testing.T, repo Repository, message string) string {
	t.Helper()
	runTestGit(t, repo.RepoURL, "add", "-A")
	runTestGit(t, repo.RepoURL, "commit", "-m", message)
	return strings.TrimSpace(runTestGitOutput(t, repo.RepoURL, "rev-parse", "HEAD"))
}

func scannerIdentityVersion(t *testing.T, server *Server, repoID int64, branch, path string) DocVersion {
	t.Helper()
	// 一个路径可能保留另一个身份的 deleted 历史，必须明确选择当前可读的有效版本。
	var id int64
	if err := server.db.QueryRow(`SELECT id FROM doc_versions WHERE repo_id=? AND branch=? AND file_path=?
		AND status IN ('active','renamed','moved') ORDER BY updated_at DESC,id DESC LIMIT 1`, repoID, branch, path).Scan(&id); err != nil {
		t.Fatalf("active version %s:%s: %v", branch, path, err)
	}
	v, err := getVersion(context.Background(), server.db, repoID, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func scannerIdentityGetJSON(t *testing.T, server *Server, path string, target any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
}

func scannerIdentityAssertContent(t *testing.T, server *Server, version DocVersion, expected string) {
	t.Helper()
	var content FileContent
	scannerIdentityGetJSON(t, server, fmt.Sprintf("/api/repos/%d/versions/%d/content", version.RepoID, version.ID), &content)
	if content.DocumentID != version.DocumentID || content.BlobSHA != version.BlobSHA || content.SourceCommitSHA != version.HeadCommitSHA || content.FilePath != version.FilePath || (version.Previewable && content.Content != expected) {
		t.Errorf("API content inconsistent with stored version: version=%+v content=%+v expected=%q", version, content, expected)
	}
	// 普通源文件可能不提供预览，下载 API 仍必须返回准确 blob；Markdown 同时验证两条路。
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/repos/%d/versions/%d/download", version.RepoID, version.ID), nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != expected {
		t.Errorf("download version %d = %d %q, want %q", version.ID, recorder.Code, recorder.Body.String(), expected)
	}
}

func scannerIdentityFiles(t *testing.T, server *Server, repoID int64, view, dir string) map[string]FileEntry {
	t.Helper()
	var body struct {
		Items []FileEntry `json:"items"`
	}
	scannerIdentityGetJSON(t, server, fmt.Sprintf("/api/repos/%d/files?view=%s&dir=%s", repoID, view, dir), &body)
	files := make(map[string]FileEntry)
	for _, item := range body.Items {
		if item.Kind == "file" {
			if _, duplicate := files[item.Path]; duplicate {
				t.Errorf("duplicate API file entry: %+v", item)
			}
			files[item.Path] = item
		}
	}
	return files
}

func scannerIdentityEvents(t *testing.T, server *Server, repoID int64, branch string) []PathEvent {
	t.Helper()
	var body struct {
		Items []PathEvent `json:"items"`
	}
	scannerIdentityGetJSON(t, server, fmt.Sprintf("/api/repos/%d/path-events?branch=%s", repoID, branch), &body)
	return body.Items
}

func scannerIdentityAssertDocumentCount(t *testing.T, server *Server, repoID int64, expected int) {
	t.Helper()
	var repo Repository
	scannerIdentityGetJSON(t, server, fmt.Sprintf("/api/repos/%d", repoID), &repo)
	var latestCount int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM doc_latest WHERE repo_id=?`, repoID).Scan(&latestCount); err != nil {
		t.Fatal(err)
	}
	if repo.DocumentCount != expected || latestCount != expected {
		t.Errorf("document count: API=%d SQLite latest=%d, want %d", repo.DocumentCount, latestCount, expected)
	}
}

func scannerIdentityAssertInvalidCheckpoint(t *testing.T, server *Server, repoID int64, oldHead string) {
	t.Helper()
	ref, err := getRepoRef(context.Background(), server.db, repoID, "branch", "main")
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.CommitSHA != oldHead || ref.LastScannedAt != "" {
		t.Errorf("failed scan advanced a successful checkpoint: ref=%+v, want old HEAD %s and no success marker", ref, oldHead)
	}
}

func scannerIdentityAssertSuccessfulCheckpoint(t *testing.T, server *Server, repoID int64, head string) {
	t.Helper()
	ref, err := getRepoRef(context.Background(), server.db, repoID, "branch", "main")
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || ref.CommitSHA != head || ref.LastScannedAt == "" || ref.ScanConfigHash == "" {
		t.Errorf("missing successful checkpoint at %s: %+v", head, ref)
	}
}

func scannerIdentityLatestSnapshot(t *testing.T, server *Server, repoID int64) string {
	t.Helper()
	rows, err := server.db.Query(`SELECT document_id,version_id,source_branch,source_commit_sha,file_path
		FROM doc_latest WHERE repo_id=? ORDER BY document_id`, repoID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var snapshot []string
	for rows.Next() {
		var documentID, versionID int64
		var branch, head, path string
		if err := rows.Scan(&documentID, &versionID, &branch, &head, &path); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, fmt.Sprintf("%d:%d:%s:%s:%s", documentID, versionID, branch, head, path))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(snapshot, "\n")
}
