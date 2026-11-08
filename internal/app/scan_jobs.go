package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ScanProgress 是扫描中可查询的即时进度。最终详情由扫描核心替换为分支统计和阶段耗时。
type ScanProgress struct {
	Stage          string `json:"stage"`
	Branch         string `json:"branch"`
	ProcessedFiles int    `json:"processed_files"`
	TotalFiles     int    `json:"total_files"`
}

// Scanner 统一拥有同步、后台和 Webhook 扫描的生命周期。
// 所有仓库共享一个执行槽位：不排队、不为繁忙请求创建 goroutine，避免多个大仓库争抢资源。
type Scanner struct {
	db     *sql.DB
	readDB *sql.DB
	git    *Git

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	active *scanJob
	wg     sync.WaitGroup
}

type scanJob struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    ScanRun // mu 保护，供取消接口返回已接受的任务。

	// 进度独立串行化，持久化等待 writer 时不阻塞 Cancel 和 Close 的取消动作。
	progressMu sync.Mutex
	progress   ScanProgress
	lastReport time.Time
}

func newScanner(db *sql.DB, git *Git) *Scanner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scanner{db: db, git: git, ctx: ctx, cancel: cancel}
}

func (s *Scanner) reader() *sql.DB {
	if s.readDB != nil {
		return s.readDB
	}
	return s.db
}

// Scan 的普通手动触发也使用增量路径；显式全量只由 force 决定。
func (s *Scanner) Scan(ctx context.Context, repoID int64, trigger string) (ScanRun, error) {
	return s.ScanWithOptions(ctx, repoID, trigger, false)
}

func (s *Scanner) ScanWithOptions(ctx context.Context, repoID int64, trigger string, force bool) (ScanRun, error) {
	job, repo, stopCaller, err := s.begin(ctx, repoID, trigger)
	if err != nil {
		return ScanRun{}, err
	}
	defer stopCaller()
	return s.execute(job, repo, force)
}

// Start 仅在接受任务前使用调用方 context；返回后任务归 Server 根生命周期所有。
// 因此 HTTP 客户端断开不会中止已接受的扫描，Close 则一定能取消并等待它。
func (s *Scanner) Start(ctx context.Context, repoID int64, trigger string, force bool) (ScanRun, error) {
	job, repo, stopCaller, err := s.begin(ctx, repoID, trigger)
	if err != nil {
		return ScanRun{}, err
	}
	stopCaller()
	run := job.run
	go func() {
		if _, err := s.execute(job, repo, force); err != nil {
			log.Printf("scan run %d: %v", run.ID, err)
		}
	}()
	return run, nil
}

// begin 在查询数据库前占用槽位并登记等待计数，Close 与任务接纳之间没有空窗。
func (s *Scanner) begin(ctx context.Context, repoID int64, trigger string) (*scanJob, Repository, func() bool, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, Repository{}, nil, errUnavailable("scanner is closed")
	}
	if s.active != nil {
		s.mu.Unlock()
		return nil, Repository{}, nil, errConflict("服务已有扫描任务正在运行，请稍后再试")
	}
	deadlineCtx, stopDeadline := context.WithTimeout(s.ctx, 30*time.Minute)
	jobCtx, cancelCause := context.WithCancelCause(deadlineCtx)
	cancel := func() {
		cancelCause(context.Canceled)
		stopDeadline()
	}
	job := &scanJob{ctx: jobCtx, cancel: cancel}
	s.active = job
	s.wg.Add(1)
	s.mu.Unlock()
	// 保留调用方超时的原因，避免桥接取消后只剩 context.Canceled，无法区分主动取消和超时。
	stopCaller := context.AfterFunc(ctx, func() { cancelCause(ctx.Err()) })
	if ctx.Err() != nil {
		cancelCause(ctx.Err())
	}
	repo, err := getRepository(jobCtx, s.reader(), repoID)
	if err == nil && !repo.Enabled {
		err = errBadRequest("repository disabled")
	}
	var run ScanRun
	if err == nil {
		run, err = createScanRun(jobCtx, s.db, repoID, trigger)
	}
	if err != nil {
		stopCaller()
		s.release(job)
		return nil, Repository{}, nil, err
	}
	s.mu.Lock()
	job.run = run
	s.mu.Unlock()
	return job, repo, stopCaller, nil
}

// execute 是唯一终结入口。扫描核心不持有执行槽位，也不负责创建或完成 run。
// 终结写入使用独立 context，避免取消、超时和停机使 running 永久留在数据库。
func (s *Scanner) execute(job *scanJob, repo Repository, force bool) (run ScanRun, resultErr error) {
	s.mu.Lock()
	run = job.run
	s.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			resultErr = fmt.Errorf("scan panic: %v", recovered)
		}
		if err := context.Cause(job.ctx); err != nil {
			resultErr = errors.Join(resultErr, err)
			run.Status = "failed"
		}
		if resultErr != nil {
			if run.Status == "" || run.Status == "running" || run.Status == "success" {
				run.Status = "failed"
			}
			if run.ErrorMessage == "" {
				run.ErrorMessage = resultErr.Error()
			} else if job.ctx.Err() != nil {
				run.ErrorMessage += "\n" + context.Cause(job.ctx).Error()
			}
		} else if run.Status == "" || run.Status == "running" {
			run.Status = "success"
		}
		run.FinishedAt = nowString()
		// 与进度写串行化，保证任何迟到的进度都不能覆盖最终详情。
		job.progressMu.Lock()
		if run.DetailJSON == "" && !job.lastReport.IsZero() {
			run.DetailJSON = encodeJSON(map[string]any{"progress": job.progress})
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := finishScanRun(finishCtx, s.db, run); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("finish scan run %d: %w", run.ID, err))
		}
		cancel()
		job.progressMu.Unlock()
		s.release(job)
	}()
	computed, err := s.scanRepository(job.ctx, repo, run, force)
	// 身份和起始时间由生命周期层拥有，扫描核心只返回统计和最终详情。
	computed.ID, computed.RepoID = run.ID, run.RepoID
	computed.TriggerType, computed.StartedAt = run.TriggerType, run.StartedAt
	return computed, err
}

func (s *Scanner) release(job *scanJob) {
	job.cancel()
	s.mu.Lock()
	if s.active == job {
		s.active = nil
	}
	s.mu.Unlock()
	s.wg.Done()
}

// Cancel 返回取消请求发出时的任务快照；实际终态通过 scan-runs 查询。
// 不等待 Git/数据库退出，保持 HTTP 接口及时响应，最终状态统一为 failed。
func (s *Scanner) Cancel(repoID int64) (ScanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.run.ID == 0 || s.active.run.RepoID != repoID {
		return ScanRun{}, errConflict("no scan running for repository")
	}
	s.active.cancel()
	return s.active.run, nil
}

func (s *Scanner) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

// reportCommitted 在每个分支事务完成后更新已提交统计，不能把准备文件数冒充提交数。
// 进度 JSON 保留当前阶段；取消接口也能返回最近已提交的任务快照。
func (s *Scanner) reportCommitted(ctx context.Context, run ScanRun) {
	if _, err := s.db.ExecContext(ctx, `UPDATE scan_runs SET branch_count=?,file_count=?,skipped_count=?,error_count=?
		WHERE id=? AND status='running'`, run.BranchCount, run.FileCount, run.SkippedCount, run.ErrorCount, run.ID); err != nil {
		if ctx.Err() == nil {
			log.Printf("report scan counters %d: %v", run.ID, err)
		}
		return
	}
	s.mu.Lock()
	if s.active != nil && s.active.run.ID == run.ID {
		s.active.run.BranchCount = run.BranchCount
		s.active.run.FileCount = run.FileCount
		s.active.run.SkippedCount = run.SkippedCount
		s.active.run.ErrorCount = run.ErrorCount
	}
	s.mu.Unlock()
}

// reportProgress 必须在扫描写事务之外调用：writer 只有一个连接，事务内调用会自等。
// 阶段或分支切换立即落库，同阶段每秒最多一次；最终 run 总会单独写入。
func (s *Scanner) reportProgress(ctx context.Context, runID int64, progress ScanProgress) {
	s.mu.Lock()
	job := s.active
	if job == nil || job.run.ID != runID {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	job.progressMu.Lock()
	defer job.progressMu.Unlock()
	if progress.Stage == job.progress.Stage && progress.Branch == job.progress.Branch && time.Since(job.lastReport) < time.Second {
		return
	}
	detail := encodeJSON(map[string]any{"progress": progress})
	if _, err := s.db.ExecContext(ctx, `UPDATE scan_runs SET detail_json = ? WHERE id = ? AND status = 'running'`, detail, runID); err != nil {
		if ctx.Err() == nil {
			log.Printf("report scan progress %d: %v", runID, err)
		}
		return
	}
	job.progress, job.lastReport = progress, time.Now()
	s.mu.Lock()
	job.run.DetailJSON = detail
	s.mu.Unlock()
}
