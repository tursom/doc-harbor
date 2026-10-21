package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"doc-harbor/internal/pirpc"
)

// piRuntime 的根 context 独立于浏览器连接，已开始的问答在 SSE 断开后仍落盘。
type piRuntime struct {
	ctx           context.Context
	cancel        context.CancelFunc
	pool          *pirpc.Pool
	mu            sync.Mutex
	active        map[int64]*piActiveRun
	wg            sync.WaitGroup
	configMu      sync.Mutex
	configVersion string
}

type piActiveRun struct {
	mu sync.Mutex
	// 控制命令与进程归还互斥，避免迟到的 steer/abort 影响下一个借用者。
	controlMu    sync.Mutex
	client       *pirpc.Client
	cancelQueue  context.CancelFunc
	stopStatus   string
	stopMessage  string
	finished     bool
	sessionReady bool
}

type piStreamEvent struct {
	Name string
	Data any
}

func (s *Server) initPiRuntime() error {
	// 服务重启时旧子进程已不存在，不能让持久化的 running 永远占住会话。
	if _, err := s.db.Exec(`UPDATE ai_runs SET status='failed', error_message='DocHarbor 已重启，运行被中断', finished_at=? WHERE status IN ('queued','running')`, nowString()); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pi = &piRuntime{ctx: ctx, cancel: cancel, pool: pirpc.NewPool(s.cfg.AIPIPoolSize, s.cfg.AIPIMaxRunsPerProcess), active: map[int64]*piActiveRun{}}
	// 配置发布不依赖新问题触发：有进程后每秒检查版本，闲进程立即回收、忙进程归还时更新。
	s.pi.wg.Add(1)
	go func() {
		defer s.pi.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pi.configMu.Lock()
				initialized := s.pi.configVersion != ""
				s.pi.configMu.Unlock()
				if initialized {
					_, _, _ = s.piRuntimeSpec(ctx)
				}
			}
		}
	}()
	return nil
}

func (p *piRuntime) close() {
	p.mu.Lock()
	p.cancel()
	p.mu.Unlock()
	p.pool.Close()
	p.wg.Wait()
}

func piResponseData(response map[string]any) map[string]any {
	data, _ := response["data"].(map[string]any)
	return data
}

func piString(value any) string { text, _ := value.(string); return text }

// piSessionPath 只接受本服务保存的相对 JSONL 文件名，不能通过会话元数据打开其他文件。
func (s *Server) piSessionPath(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || filepath.Base(name) != name || !strings.HasSuffix(name, ".jsonl") {
		return "", errBadRequest("无效的 pi 会话文件名")
	}
	return filepath.Abs(filepath.Join(s.cfg.DataDir, "ai", "pi-sessions", name))
}

func (s *Server) persistPiSessionFile(ctx context.Context, client *pirpc.Client, run *piRunRow) error {
	response, err := client.Command(ctx, "get_state", nil)
	if err != nil {
		return err
	}
	file := piString(piResponseData(response)["sessionFile"])
	if file == "" {
		return nil
	}
	expected, err := s.piSessionPath(filepath.Base(file))
	if err != nil {
		return err
	}
	actual, err := filepath.Abs(file)
	if err != nil || actual != expected {
		return errors.New("pi 返回了隔离目录以外的会话文件")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE ai_sessions SET pi_session_file=? WHERE id=?`, filepath.Base(file), run.SessionID)
	if err != nil {
		return err
	}
	if run.PiEntryID == "" {
		turns, parseErr := parsePiSession(actual)
		if parseErr == nil && len(turns) > 0 {
			last := turns[len(turns)-1]
			if last.Question == run.Question && last.EntryID != "" {
				run.PiEntryID = last.EntryID
				_, err = s.db.ExecContext(ctx, `UPDATE ai_runs SET pi_entry_id=? WHERE id=?`, run.PiEntryID, run.ID)
			}
		}
	}
	return err
}

func piStatsUsage(response map[string]any) piUsage {
	data := piResponseData(response)
	raw, _ := json.Marshal(data["tokens"])
	var native piMessageUsage
	_ = json.Unmarshal(raw, &native)
	usage := piUsage{}
	piAddUsage(&usage, native)
	if tokens, ok := data["tokens"].(map[string]any); ok {
		if total, ok := tokens["total"].(float64); ok {
			usage.TotalTokens = int64(total)
		}
	}
	if cost, ok := data["cost"].(float64); ok {
		usage.Cost = cost
	}
	return usage
}

func piUsageDifference(after, before piUsage) piUsage {
	return piUsage{InputTokens: max(0, after.InputTokens-before.InputTokens), OutputTokens: max(0, after.OutputTokens-before.OutputTokens),
		CacheReadTokens: max(0, after.CacheReadTokens-before.CacheReadTokens), CacheWriteTokens: max(0, after.CacheWriteTokens-before.CacheWriteTokens),
		TotalTokens: max(0, after.TotalTokens-before.TotalTokens), Cost: max(0, after.Cost-before.Cost)}
}

// stop 不持有运行锁等待 abort：abort 响应要等 agent 完全空闲，事件消费者必须继续读 stdout。
func (a *piActiveRun) stop(status, message string) {
	a.mu.Lock()
	if a.finished || a.stopStatus != "" {
		a.mu.Unlock()
		return
	}
	a.stopStatus, a.stopMessage = status, message
	client := a.client
	if client == nil {
		a.cancelQueue()
	}
	a.mu.Unlock()
	if client != nil {
		go func() {
			a.controlMu.Lock()
			defer a.controlMu.Unlock()
			a.mu.Lock()
			finished := a.finished
			a.mu.Unlock()
			if !finished {
				piAbortClient(client)
			}
		}()
	}
}

func piAbortClient(client *pirpc.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// pi 的 abort 会继续队列中的 steer，必须先清空队列。
	_, _ = client.Command(ctx, "clear_queue", nil)
	if _, err := client.Command(ctx, "abort", nil); err != nil {
		client.Close()
	}
}

// executePiRun 在独立 goroutine 内运行。排队沿用请求 context，拿到进程之后只受服务退出和运行超时影响。
func (s *Server) executePiRun(queueCtx context.Context, active *piActiveRun, session AISession, run piRunRow, emit func(string, any)) {
	defer s.pi.wg.Done()
	defer func() {
		active.mu.Lock()
		active.finished = true
		active.cancelQueue()
		active.mu.Unlock()
		s.pi.mu.Lock()
		delete(s.pi.active, session.ID)
		s.pi.mu.Unlock()
	}()
	var client *pirpc.Client
	var usage piUsage
	var citations = []piCitation{}
	var runErr error
	var runContext context.Context
	var runCancel context.CancelFunc
	var generationSettled bool
	defer func() {
		if runCancel != nil {
			defer runCancel()
		}
		// 等待已接受的控制命令完成后才能把进程交给其他会话。
		active.controlMu.Lock()
		defer active.controlMu.Unlock()
		active.mu.Lock()
		stopStatus, stopMessage := active.stopStatus, active.stopMessage
		active.finished = true
		active.mu.Unlock()
		if !generationSettled && runContext != nil && runContext.Err() != nil {
			if stopStatus == "" && errors.Is(runContext.Err(), context.DeadlineExceeded) {
				stopStatus, stopMessage = "timeout", "问答超时，已停止，可以缩小问题范围后重试"
			}
			if client != nil {
				client.Close()
			}
		}
		if stopStatus != "" {
			run.Status, run.ErrorMessage = stopStatus, stopMessage
		} else if runErr != nil {
			run.Status, run.ErrorMessage = "failed", runErr.Error()
		} else {
			run.Status = "succeeded"
		}
		run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		run.InputTokens, run.OutputTokens, run.CacheReadTokens, run.Cost = int(usage.InputTokens), int(usage.OutputTokens), int(usage.CacheReadTokens), usage.Cost
		run.CitationsJSON = encodeJSON(citations)
		if client != nil {
			run.StderrTail = client.StderrTail()
		}
		// 服务退出也要有终态，避免根 context 取消使最后一次数据库写入失败。
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.savePiRun(ctx, run); err != nil {
			runErr = err
			run.Status = "failed"
			run.ErrorMessage = err.Error()
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE ai_sessions SET title=CASE WHEN title='新的 AI 问答' THEN ? ELSE title END, updated_at=? WHERE id=?`, piTextPreviewTitle(run.Question), run.FinishedAt, session.ID)
		if client != nil {
			// 拒绝新控制命令后清空未被当前 run 消费的补充说明，防止跨会话遗留。
			if client.Healthy() {
				if _, err := client.Command(ctx, "clear_queue", nil); err != nil {
					client.Close()
				}
			}
			s.pi.pool.Release(client)
		}
		if runErr != nil {
			emit("error", map[string]any{"message": run.ErrorMessage})
		}
		emit("done", map[string]any{"run_id": run.ID, "status": run.Status, "citations": citations, "usage": usage, "message": run.ErrorMessage})
	}()
	// 取 Spec 和加入池等待必须有序；Acquire 内部会应用传入版本，因此这里以最新配置的版本固定获取。
	_, _, err := s.piRuntimeSpec(queueCtx)
	if err != nil {
		runErr = err
		return
	}
	client, err = s.pi.pool.AcquireLatest(queueCtx, func(position int) { emit("queued", map[string]any{"position": position}) })
	if err != nil {
		if queueCtx.Err() != nil {
			active.stop("aborted", "排队已取消")
		} else {
			runErr = err
		}
		return
	}
	active.mu.Lock()
	active.client = client
	stopped := active.stopStatus != ""
	active.mu.Unlock()
	if stopped {
		return
	}
	selectedProvider, selectedModel := client.Selection()
	run.ProviderKey, run.Model = strings.TrimPrefix(selectedProvider, "dh-"), selectedModel
	run.Status = "running"
	if err := s.savePiRun(s.pi.ctx, run); err != nil {
		runErr = err
		return
	}
	emit("started", map[string]any{"run_id": run.ID})
	ctx, cancel := context.WithTimeout(s.pi.ctx, s.cfg.AIRunTimeout)
	runContext = ctx
	// cancel 在最终落盘之后执行，不能把正常完成误判为运行超时。
	runCancel = cancel
	command := func(typ string, fields map[string]any) (map[string]any, error) {
		return client.Command(ctx, typ, fields)
	}
	if session.PiSessionFile == "" {
		response, err := command("new_session", nil)
		if err != nil {
			runErr = err
			return
		}
		if piResponseData(response)["cancelled"] == true {
			runErr = errors.New("pi 取消了新建会话")
			return
		}
	} else {
		path, err := s.piSessionPath(session.PiSessionFile)
		if err != nil {
			runErr = err
			return
		}
		if _, err := os.Stat(path); err != nil {
			runErr = fmt.Errorf("pi 会话文件不可用: %w", err)
			return
		}
		response, err := command("switch_session", map[string]any{"sessionPath": path})
		if err != nil {
			runErr = err
			return
		}
		if piResponseData(response)["cancelled"] == true {
			runErr = errors.New("pi 取消了切换会话")
			return
		}
	}
	if _, err := command("set_model", map[string]any{"provider": selectedProvider, "modelId": selectedModel}); err != nil {
		runErr = err
		return
	}
	beforeResponse, err := command("get_session_stats", nil)
	if err != nil {
		runErr = err
		return
	}
	before := piStatsUsage(beforeResponse)
	active.mu.Lock()
	active.sessionReady = true
	active.mu.Unlock()
	// 先订阅事件再发送 prompt。Command 的 stdout 读取器会在后台缓冲快速响应期间产生的事件。
	events := client.Events()
	// 停止与首次 prompt 的提交互斥：准备阶段接受停止后不再启动模型。
	active.controlMu.Lock()
	active.mu.Lock()
	stopped = active.stopStatus != ""
	active.mu.Unlock()
	if stopped {
		active.controlMu.Unlock()
		return
	}
	response, err := command("prompt", map[string]any{"message": s.piUserMessage(ctx, run.Role, aiScopeFromJSON(run.ScopeJSON), run.Question)})
	active.controlMu.Unlock()
	if err != nil {
		runErr = err
		client.Close()
		return
	}
	// prompt 被扩展处理时不会有 agent_settled，避免永远占着进程。
	settled := piResponseData(response)["disposition"] == "handled"
	generationSettled = settled
	seen := piSeenRanges{}
	answer := ""
	modelError := ""
	var stopTimer *time.Timer
	var stopDeadline <-chan time.Time
	defer func() {
		if stopTimer != nil {
			stopTimer.Stop()
		}
	}()
	// 收到第一条消息即保存文件名，进程随后崩溃时仍能读取已落盘的历史。
	savedFile := session.PiSessionFile != ""
	for !settled {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				active.stop("timeout", "问答超时，已停止，可以缩小问题范围后重试")
			} else {
				active.stop("failed", "DocHarbor 正在关闭")
			}
			// 取消后的 ctx 不能继续 select，否则会忙循环。给 abort 最多十秒收尾。
			ctx = context.Background()
			stopTimer = time.NewTimer(10 * time.Second)
			stopDeadline = stopTimer.C
		case <-stopDeadline:
			client.Close()
			settled = true
		case event, ok := <-events:
			if !ok {
				runErr = client.Err()
				if runErr == nil {
					runErr = errors.New("pi 进程已退出")
				}
				settled = true
				continue
			}
			typ := piString(event["type"])
			switch typ {
			case "agent_settled":
				generationSettled = true
				active.mu.Lock()
				active.finished = true
				active.mu.Unlock()
				settled = true
			case "message_end":
				raw, _ := json.Marshal(event["message"])
				var message PiTranscriptMessage
				if json.Unmarshal(raw, &message) == nil && message.Role == "assistant" {
					text, _ := piParseContent(message.Content)
					if message.StopReason != "toolUse" {
						answer = text
					}
					if message.StopReason == "error" {
						modelError = message.ErrorMessage
					} else {
						modelError = ""
					}
				}
				if !savedFile || run.PiEntryID == "" {
					stateCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
					if err := s.persistPiSessionFile(stateCtx, client, &run); err != nil {
						log.Printf("保存 pi 会话位置失败: %v", err)
					} else {
						savedFile = true
					}
					stop()
				}
			case "message_update":
				update, _ := event["assistantMessageEvent"].(map[string]any)
				if update["type"] == "text_delta" {
					emit("text", map[string]any{"delta": piString(update["delta"])})
				}
			case "tool_execution_start":
				run.ToolCalls++
				emit("tool_start", map[string]any{"call_id": event["toolCallId"], "tool": event["toolName"], "args": event["args"]})
				if run.ToolCalls > s.cfg.AIMaxToolCalls {
					active.stop("aborted", "检索步骤过多，已停止，可以缩小问题范围后重试")
				}
			case "tool_execution_end":
				result, _ := event["result"].(map[string]any)
				raw, _ := json.Marshal(result["content"])
				text, _ := piParseContent(raw)
				seen.AddToolResult(text)
				emit("tool_end", map[string]any{"call_id": event["toolCallId"], "summary": strings.SplitN(text, "\n", 2)[0], "is_error": event["isError"], "preview": piTextPreview(text)})
			case "auto_retry_start", "auto_retry_end", "compaction_start", "compaction_end", "summarization_retry_scheduled":
				message := "正在重试模型请求"
				if strings.HasPrefix(typ, "compaction") {
					message = "正在整理会话上下文"
				}
				emit("notice", map[string]any{"message": message, "type": typ})
			}
		case <-client.Done():
			runErr = client.Err()
			if runErr == nil {
				runErr = errors.New("pi 进程已退出")
			}
			settled = true
		}
	}
	if modelError != "" {
		runErr = errors.New(modelError)
	}
	if client.Healthy() {
		finishCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := s.persistPiSessionFile(finishCtx, client, &run); err != nil {
			runErr = err
		}
		if response, err := client.Command(finishCtx, "get_session_stats", nil); err == nil {
			usage = piUsageDifference(piStatsUsage(response), before)
		} else if runErr == nil {
			runErr = err
		}
	}
	verifyCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	citations = verifyPiCitations(answer, seen)
	if resolved, err := s.resolvePiCitationURLs(verifyCtx, citations); err == nil {
		citations = resolved
	} else if runErr == nil {
		runErr = err
	}
}

func piTextPreviewTitle(text string) string {
	runes := []rune(strings.ReplaceAll(text, "\n", " "))
	if len(runes) > 40 {
		runes = runes[:40]
	}
	return string(runes)
}
