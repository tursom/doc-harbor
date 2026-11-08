package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 使用真实 Git 子进程，在 clone/fetch 前通过 FIFO 阻塞。取消必须真正终止子进程，
// 不能依赖模拟 scanRepository 立即返回，也不会在测试结束后遗留 sleep 子进程。
func newBlockedScanJob(t *testing.T) (*Server, Repository, func(), func()) {
	t.Helper()
	requireGit(t)
	server := newWebhookTestServer(t)
	source := createTestGitRepo(t)
	repo, err := createRepository(context.Background(), server.db, Repository{
		Name: "background jobs", RepoURL: source, DefaultBranch: "main", TrackedBranches: []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gate, marker := filepath.Join(dir, "gate"), filepath.Join(dir, "started")
	if err := syscall.Mkfifo(gate, 0o600); err != nil {
		t.Fatal(err)
	}
	// O_RDWR 避免释放动作在任务已被取消时等待读端；正常情况下读取方只有 Git wrapper。
	fifo, err := os.OpenFile(gate, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fifo.Close() })
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nfor arg in \"$@\"; do\ncase \"$arg\" in clone|fetch)\n" +
		"printf started > " + quote(marker) + "\nIFS= read -r released < " + quote(gate) + "\nbreak;;\nesac\ndone\nexec " + quote(realGit) + " \"$@\"\n"
	wrapper := filepath.Join(dir, "git-blocked")
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	server.scanner.git.cfg.GitBin = wrapper
	waitStarted := func() {
		t.Helper()
		waitScanJob(t, func() bool { _, err := os.Stat(marker); return err == nil })
	}
	release := func() {
		t.Helper()
		if _, err := fifo.WriteString("continue\n"); err != nil {
			t.Fatal(err)
		}
	}
	return server, repo, waitStarted, release
}

func waitScanJob(t *testing.T, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for scan job")
		}
	}
}

func waitFinishedScanJob(t *testing.T, server *Server, repoID int64) ScanRun {
	t.Helper()
	var result ScanRun
	waitScanJob(t, func() bool {
		run, err := latestScanRun(context.Background(), server.reader(), repoID)
		if err != nil {
			t.Fatal(err)
		}
		if run == nil || run.Status == "running" {
			return false
		}
		result = *run
		return true
	})
	if result.FinishedAt == "" {
		t.Fatalf("missing finished_at: %+v", result)
	}
	return result
}

func TestScanJobHTTPDisconnectAndExplicitForce(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"force":true}`} {
		t.Run(fmt.Sprintf("body_%s", body), func(t *testing.T) {
			server, repo, waitStarted, release := newBlockedScanJob(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorder := httptest.NewRecorder()
			server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/repos/%d/scan", repo.ID), strings.NewReader(body)).WithContext(ctx))
			if recorder.Code != http.StatusAccepted {
				t.Fatalf("scan admission = %d %s", recorder.Code, recorder.Body.String())
			}
			var accepted ScanRun
			if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == 0 || accepted.Status != "running" {
				t.Fatalf("accepted run=%+v err=%v", accepted, err)
			}
			waitStarted()
			cancel() // HTTP 已结束，但 Git 仍被阻塞；任务只能由 Server 生命周期控制。
			stored, err := latestScanRun(context.Background(), server.reader(), repo.ID)
			if err != nil || stored == nil || stored.Status != "running" {
				t.Fatalf("request cancellation stopped background scan: %+v err=%v", stored, err)
			}
			server.scanner.mu.Lock()
			jobErr := server.scanner.active.ctx.Err()
			server.scanner.mu.Unlock()
			if jobErr != nil {
				t.Fatalf("background context inherited HTTP cancellation: %v", jobErr)
			}
			release()
			finished := waitFinishedScanJob(t, server, repo.ID)
			if finished.Status != "success" || finished.ID != accepted.ID {
				t.Fatalf("disconnected HTTP run did not succeed: %+v", finished)
			}
			var detail struct {
				Force bool `json:"force"`
			}
			if err := json.Unmarshal([]byte(finished.DetailJSON), &detail); err != nil || detail.Force != strings.Contains(body, "true") {
				t.Fatalf("explicit force did not reach core: detail=%s err=%v", finished.DetailJSON, err)
			}
		})
	}
}

func TestScanJobGlobalBusyAndCancelAPI(t *testing.T) {
	server, repo, waitStarted, _ := newBlockedScanJob(t)
	other, err := createRepository(context.Background(), server.db, Repository{Name: "other", RepoURL: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := server.scanner.Start(context.Background(), repo.ID, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	waitStarted()
	for _, repoID := range []int64{repo.ID, other.ID} {
		if _, err := server.scanner.Start(context.Background(), repoID, "manual", false); !isAppErrorStatus(err, http.StatusConflict) {
			t.Fatalf("Start while busy: %v", err)
		}
		if _, err := server.scanner.Scan(context.Background(), repoID, "scheduled"); !isAppErrorStatus(err, http.StatusConflict) {
			t.Fatalf("Scan while busy: %v", err)
		}
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/repos/%d/scan/cancel", repo.ID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("cancel = %d %s", recorder.Code, recorder.Body.String())
	}
	var cancelled ScanRun
	if err := json.Unmarshal(recorder.Body.Bytes(), &cancelled); err != nil || cancelled.ID != run.ID {
		t.Fatalf("cancel snapshot=%+v err=%v", cancelled, err)
	}
	finished := waitFinishedScanJob(t, server, repo.ID)
	if finished.Status != "failed" || !strings.Contains(finished.ErrorMessage, "canceled") {
		t.Fatalf("cancel did not persist terminal failure: %+v", finished)
	}
	// 繁忙请求不应创建多余的 running 行。
	var count int
	if err := server.db.QueryRow(`SELECT count(*) FROM scan_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("busy admission created runs: count=%d err=%v", count, err)
	}
}

func TestScanJobSynchronousCancellationAndDeadline(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprintf("timeout_%t", timeout), func(t *testing.T) {
			server, repo, waitStarted, _ := newBlockedScanJob(t)
			ctx, cancel := context.WithCancel(context.Background())
			if timeout {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
			}
			defer cancel()
			type result struct {
				run ScanRun
				err error
			}
			done := make(chan result, 1)
			go func() {
				run, err := server.scanner.ScanWithOptions(ctx, repo.ID, "manual", true)
				done <- result{run, err}
			}()
			waitStarted()
			if !timeout {
				cancel()
			}
			select {
			case result := <-done:
				if result.err == nil || result.run.Status != "failed" || result.run.FinishedAt == "" {
					t.Fatalf("cancelled sync result=%+v err=%v", result.run, result.err)
				}
				if timeout && !errors.Is(result.err, context.DeadlineExceeded) {
					t.Fatalf("deadline lost: %v", result.err)
				}
				stored := waitFinishedScanJob(t, server, repo.ID)
				if stored.Status != result.run.Status || stored.FinishedAt != result.run.FinishedAt {
					t.Fatalf("sync return not persisted: stored=%+v result=%+v", stored, result.run)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("synchronous cancellation did not finish")
			}
		})
	}
}

func TestScanJobCloseCancelsWaitsAndPersists(t *testing.T) {
	server, repo, waitStarted, _ := newBlockedScanJob(t)
	if _, err := server.scanner.Start(context.Background(), repo.ID, "manual", false); err != nil {
		t.Fatal(err)
	}
	waitStarted()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	// 直接 openDB，避免 NewServer 的重启修复掩盖 Close 没有完成终态写入的问题。
	db, err := openDB(context.Background(), server.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, err := latestScanRun(context.Background(), db, repo.ID)
	if err != nil || run == nil || run.Status != "failed" || run.FinishedAt == "" || !strings.Contains(run.ErrorMessage, "canceled") {
		t.Fatalf("Close left incomplete run: %+v err=%v", run, err)
	}
	if _, err := server.scanner.Start(context.Background(), repo.ID, "manual", false); !isAppErrorStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("closed scanner admitted job: %v", err)
	}
}

func TestScanJobProgressThrottleAndFinalDetail(t *testing.T) {
	server, repo, waitStarted, release := newBlockedScanJob(t)
	run, err := server.scanner.Start(context.Background(), repo.ID, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	waitStarted()
	ctx := context.Background()
	check := func(want ScanProgress) {
		t.Helper()
		stored, err := latestScanRun(ctx, server.reader(), repo.ID)
		if err != nil || stored == nil {
			t.Fatalf("progress query: %+v err=%v", stored, err)
		}
		var envelope struct {
			Progress ScanProgress `json:"progress"`
		}
		if err := json.Unmarshal([]byte(stored.DetailJSON), &envelope); err != nil || envelope.Progress != want {
			t.Fatalf("progress=%+v want=%+v raw=%s err=%v", envelope.Progress, want, stored.DetailJSON, err)
		}
	}
	first := ScanProgress{Stage: "files", Branch: "main", ProcessedFiles: 1, TotalFiles: 10}
	server.scanner.reportProgress(ctx, run.ID, first)
	check(first)
	second := first
	second.ProcessedFiles = 2
	server.scanner.reportProgress(ctx, run.ID, second)
	check(first) // 同阶段、同分支一秒内合并，避免每个文件一次数据库写入。
	time.Sleep(1050 * time.Millisecond)
	server.scanner.reportProgress(ctx, run.ID, second)
	check(second)
	second.Stage = "persisting"
	server.scanner.reportProgress(ctx, run.ID, second)
	check(second) // 阶段变化不受一秒节流限制。
	release()
	finished := waitFinishedScanJob(t, server, repo.ID)
	server.scanner.reportProgress(ctx, run.ID, ScanProgress{Stage: "late"})
	stored, err := latestScanRun(ctx, server.reader(), repo.ID)
	if err != nil || stored == nil || stored.DetailJSON != finished.DetailJSON || !strings.Contains(stored.DetailJSON, "branches") {
		t.Fatalf("late progress overwrote final detail: %+v err=%v", stored, err)
	}
}
