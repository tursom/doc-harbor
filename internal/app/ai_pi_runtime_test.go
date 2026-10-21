package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"doc-harbor/internal/pirpc"
)

// TestPiRunHelper 在子进程中扮演 pi。只有显式参数才启用，正常 go test 不产生进程副作用。
func TestPiRunHelper(t *testing.T) {
	helper := false
	for _, arg := range os.Args {
		if arg == "--dh-fake-pi" {
			helper = true
		}
	}
	if !helper {
		return
	}
	var sessionDir string
	var selectedModel string
	for i, arg := range os.Args {
		if arg == "--session-dir" && i+1 < len(os.Args) {
			sessionDir = os.Args[i+1]
		}
		if arg == "--model" && i+1 < len(os.Args) {
			selectedModel = os.Args[i+1]
		}
	}
	var mu sync.Mutex
	var file string
	var entries []map[string]any
	var messages []any
	var abort chan struct{}
	var abortOnce sync.Once
	var tokens float64
	emit := func(value any) { mu.Lock(); defer mu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(value) }
	appendMessage := func(message map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		if file == "" {
			file = filepath.Join(sessionDir, fmt.Sprintf("%d.jsonl", time.Now().UnixNano()))
			header, _ := json.Marshal(map[string]any{"type": "session", "version": 3, "id": "fixture", "cwd": "/fixture", "timestamp": time.Now().UTC().Format(time.RFC3339Nano)})
			_ = os.WriteFile(file, append(header, '\n'), 0o600)
		}
		id := fmt.Sprintf("entry-%d", len(entries)+1)
		var parent any
		if len(entries) > 0 {
			parent = entries[len(entries)-1]["id"]
		}
		entry := map[string]any{"type": "message", "id": id, "parentId": parent, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": message}
		entries = append(entries, entry)
		messages = append(messages, message)
		f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
		if f != nil {
			_ = json.NewEncoder(f).Encode(entry)
			f.Close()
		}
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			os.Exit(0)
		}
		var command map[string]any
		if json.Unmarshal(line, &command) != nil {
			continue
		}
		typ := piString(command["type"])
		data := map[string]any{}
		switch typ {
		case "get_state":
			mu.Lock()
			data["sessionFile"] = file
			mu.Unlock()
		case "get_session_stats":
			mu.Lock()
			count := tokens
			mu.Unlock()
			data = map[string]any{"tokens": map[string]any{"input": count, "output": count / 5, "cacheRead": count / 10, "total": count * 1.3}, "cost": count / 10000}
		case "get_messages":
			mu.Lock()
			data["messages"] = append([]any(nil), messages...)
			mu.Unlock()
		case "new_session":
			if selectedModel == "slow-setup" {
				time.Sleep(150 * time.Millisecond)
			}
			mu.Lock()
			file = ""
			entries = nil
			messages = nil
			tokens = 0
			mu.Unlock()
			data["cancelled"] = false
		case "switch_session":
			if selectedModel == "slow-switch" {
				_ = os.WriteFile("switch-started", []byte("started"), 0o600)
				time.Sleep(150 * time.Millisecond)
			}
			mu.Lock()
			file = piString(command["sessionPath"])
			entries = nil
			messages = nil
			tokens = 0
			if raw, err := os.ReadFile(file); err == nil {
				for _, line := range bytes.Split(raw, []byte{'\n'}) {
					var entry map[string]any
					if json.Unmarshal(line, &entry) == nil && entry["type"] == "message" {
						entries = append(entries, entry)
						messages = append(messages, entry["message"])
						msg, _ := entry["message"].(map[string]any)
						if msg["role"] == "assistant" && msg["stopReason"] != "toolUse" {
							tokens += 100
						}
					}
				}
			}
			mu.Unlock()
			data["cancelled"] = false
		case "steer":
			appendMessage(map[string]any{"role": "user", "content": command["message"], "timestamp": time.Now().UnixMilli()})
			data["disposition"] = "queued"
		case "prompt":
			if command["streamingBehavior"] == "steer" {
				appendMessage(map[string]any{"role": "user", "content": command["message"], "timestamp": time.Now().UnixMilli()})
				data["disposition"] = "queued"
				break
			}
			text := piString(command["message"])
			if strings.Contains(text, "crash") {
				fmt.Fprintln(os.Stderr, "fixture process crash")
				os.Exit(9)
			}
			abort = make(chan struct{})
			abortOnce = sync.Once{}
			appendMessage(map[string]any{"role": "user", "content": text, "timestamp": time.Now().UnixMilli()})
			data["disposition"] = "started"
			emit(map[string]any{"id": command["id"], "type": "response", "success": true, "command": typ, "data": data})
			currentAbort := abort
			go func() {
				emit(map[string]any{"type": "agent_start"})
				call := map[string]any{"type": "toolCall", "id": "read-1", "name": "mcp__docharbor__read", "arguments": map[string]any{"repo": "sample", "path": "doc/测试.md"}}
				appendMessage(map[string]any{"role": "assistant", "content": []any{call}, "stopReason": "toolUse", "timestamp": time.Now().UnixMilli()})
				emit(map[string]any{"type": "tool_execution_start", "toolCallId": "read-1", "toolName": "mcp__docharbor__read", "args": call["arguments"]})
				toolText := "读取第 1-2 行，共 2 行\n== sample@main:doc/测试.md#L1-2\n    1\t业务规则\n    2\t具体限制"
				result := map[string]any{"content": []any{map[string]any{"type": "text", "text": toolText}}}
				appendMessage(map[string]any{"role": "toolResult", "toolCallId": "read-1", "toolName": "mcp__docharbor__read", "content": result["content"], "timestamp": time.Now().UnixMilli()})
				emit(map[string]any{"type": "tool_execution_end", "toolCallId": "read-1", "toolName": "mcp__docharbor__read", "result": result, "isError": false})
				if strings.Contains(text, "tools") {
					emit(map[string]any{"type": "tool_execution_start", "toolCallId": "read-2", "toolName": "mcp__docharbor__read"})
				}
				delay := 5 * time.Millisecond
				if strings.Contains(text, "slow") {
					delay = 250 * time.Millisecond
				}
				if strings.Contains(text, "hang") || strings.Contains(text, "tools") {
					delay = time.Hour
				}
				stopReason := "stop"
				select {
				case <-time.After(delay):
				case <-currentAbort:
					stopReason = "aborted"
				}
				answer := "已读到业务规则。[sample@main:doc/测试.md#L1-2]"
				message := map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": answer}}, "stopReason": stopReason, "timestamp": time.Now().UnixMilli()}
				appendMessage(message)
				mu.Lock()
				tokens += 100
				mu.Unlock()
				emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": answer}})
				emit(map[string]any{"type": "message_end", "message": message})
				emit(map[string]any{"type": "agent_settled"})
			}()
			continue
		case "abort":
			if abort != nil {
				abortOnce.Do(func() { close(abort) })
			}
		}
		emit(map[string]any{"id": command["id"], "type": "response", "success": true, "command": typ, "data": data})
	}
}

func newPiRuntimeTestServer(t *testing.T, timeout time.Duration, toolLimit int) (*Server, *httptest.Server) {
	t.Helper()
	server := newWebhookTestServer(t)
	bin := filepath.Join(t.TempDir(), "fake-pi")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// shell 仅用于测试启动本 Go 测试二进制，生产启动始终直接 exec PI_BIN。
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestPiRunHelper$ -- --dh-fake-pi \"$@\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	server.cfg.PIBin = bin
	server.cfg.HTTPAddr = ":8080"
	server.cfg.AIEngine = "pi"
	server.cfg.AIRunTimeout = timeout
	server.cfg.AIMaxToolCalls = toolLimit
	// 测试只需一个进程，便于确定性覆盖排队与取消。
	server.pi.pool.Close()
	server.pi.pool = pirpc.NewPool(1, 100)
	secret, err := server.createOrUpdateAISecret(context.Background(), 0, aiSecretRequest{Name: "fixture", Value: "private-fixture-key"}, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = []AIProvider{{ProviderKey: "fixture", Model: "fixture-model", BaseURL: "http://model.invalid/v1/chat/completions", APIKeySecretID: secret.ID}}
	cfg.Chat.Routing = AIRouting{DefaultTaskClass: "standard", TaskClasses: map[string]AITaskRoute{"standard": {Providers: []string{"fixture"}}}}
	raw, hash, err := aiConfigJSONAndHash(normalizeAIConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.db.Exec(`INSERT INTO ai_config_versions(version,status,config_hash,config_json,created_at,updated_at) VALUES(1,'active',?,?,?,?)`, hash, raw, nowString(), nowString())
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Routes())
	t.Cleanup(httpServer.Close)
	return server, httpServer
}

func createPiTestSession(t *testing.T, url string) AISession {
	t.Helper()
	response, err := http.Post(url+"/api/ai/sessions", "application/json", strings.NewReader(`{"title":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session AISession
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 201 || session.Engine != "pi" {
		t.Fatalf("session: %d %#v", response.StatusCode, session)
	}
	return session
}

func piTestRun(t *testing.T, url string, id int64, question string) string {
	t.Helper()
	raw, _ := json.Marshal(piRunRequest{Question: question, Role: "开发"})
	response, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/runs", url, id), "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("run: %d %s", response.StatusCode, body)
	}
	return string(body)
}

func TestPiRunPersistsTranscriptCitationsUsageAndFeedback(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, 3*time.Second, 40)
	session := createPiTestSession(t, httpServer.URL)
	body := piTestRun(t, httpServer.URL, session.ID, "业务规则")
	for _, want := range []string{"event: started", "event: tool_start", "event: tool_end", "event: text", "event: done", `"status":"succeeded"`, `"status":"verified"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s: %s", want, body)
		}
	}
	stored, err := getAISession(context.Background(), server.db, session.ID)
	if err != nil || stored.PiSessionFile == "" {
		t.Fatalf("session file: %#v %v", stored, err)
	}
	turns, err := server.piSessionTranscript(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || len(turns[0].Steps) != 1 || turns[0].Usage.InputTokens != 100 || turns[0].Role != "开发" || turns[0].Question != "业务规则" || turns[0].Model != "fixture-model" || turns[0].ToolCalls != 1 || turns[0].StartedAt == "" || turns[0].FinishedAt == "" {
		t.Fatalf("turns: %#v", turns)
	}
	feedback, err := http.Post(fmt.Sprintf("%s/api/ai/runs/%d/feedback", httpServer.URL, turns[0].RunID), "application/json", strings.NewReader(`{"rating":-1,"comment":"需要更多字段"}`))
	if err != nil {
		t.Fatal(err)
	}
	feedback.Body.Close()
	if feedback.StatusCode != 200 {
		t.Fatal(feedback.StatusCode)
	}
	run, err := server.getPiRun(context.Background(), turns[0].RunID)
	if err != nil || run.Feedback == nil || run.Feedback.Rating != -1 {
		t.Fatalf("feedback: %#v %v", run, err)
	}
	var count int
	if err := server.db.QueryRow(`SELECT count(*) FROM ai_messages WHERE session_id=?`, session.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("pi messages duplicated: %d %v", count, err)
	}
	// 第二轮验证 switch_session 与 session stats 增量，不能把第一轮 token 再计一次。
	piTestRun(t, httpServer.URL, session.ID, "继续说明")
	runs, err := server.listPiRuns(context.Background(), session.ID)
	if err != nil || len(runs) != 2 || runs[1].InputTokens != 100 {
		t.Fatalf("usage: %#v %v", runs, err)
	}
	files, err := os.ReadDir(filepath.Join(server.cfg.DataDir, "ai", "pi-agent"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, _ := os.ReadFile(filepath.Join(server.cfg.DataDir, "ai", "pi-agent", file.Name()))
		if bytes.Contains(raw, []byte("private-fixture-key")) {
			t.Fatal("secret written to config")
		}
	}
}

func TestPiRunTimeoutToolLimitAndCrashRecovery(t *testing.T) {
	for _, tc := range []struct {
		question, status string
		timeout          time.Duration
		limit            int
	}{{"hang", "timeout", 60 * time.Millisecond, 40}, {"tools", "aborted", time.Second, 1}, {"crash", "failed", time.Second, 40}} {
		t.Run(tc.question, func(t *testing.T) {
			server, httpServer := newPiRuntimeTestServer(t, tc.timeout, tc.limit)
			session := createPiTestSession(t, httpServer.URL)
			body := piTestRun(t, httpServer.URL, session.ID, tc.question)
			if !strings.Contains(body, `"status":"`+tc.status+`"`) {
				t.Fatal(body)
			}
			runs, err := server.listPiRuns(context.Background(), session.ID)
			if err != nil || len(runs) != 1 || runs[0].Status != tc.status {
				t.Fatalf("run status: %#v %v", runs, err)
			}
			next := createPiTestSession(t, httpServer.URL)
			body = piTestRun(t, httpServer.URL, next.ID, "下一轮正常")
			if !strings.Contains(body, `"status":"succeeded"`) {
				t.Fatal(body)
			}
		})
	}
}

func TestPiRunDisconnectSteerConflictAndAbort(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, 3*time.Second, 40)
	session := createPiTestSession(t, httpServer.URL)
	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ai/sessions/%d/runs", httpServer.URL, session.ID), strings.NewReader(`{"question":"hang"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "event: tool_start") {
			break
		}
	}
	conflict, err := http.Post(request.URL.String(), "application/json", strings.NewReader(`{"question":"第二个问题"}`))
	if err != nil {
		t.Fatal(err)
	}
	conflict.Body.Close()
	if conflict.StatusCode != 409 {
		t.Fatal(conflict.StatusCode)
	}
	steer, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/steer", httpServer.URL, session.ID), "application/json", strings.NewReader(`{"text":"补充限制条件"}`))
	if err != nil {
		t.Fatal(err)
	}
	steer.Body.Close()
	if steer.StatusCode != 202 {
		t.Fatal(steer.StatusCode)
	}
	response.Body.Close()
	// 断开 SSE 后 active 仍在，停止接口应能正常控制该运行。
	abort, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/abort", httpServer.URL, session.ID), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	abort.Body.Close()
	if abort.StatusCode != 202 {
		t.Fatal(abort.StatusCode)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runs, err := server.listPiRuns(context.Background(), session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].Status == "aborted" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("aborted run was not persisted")
}

func TestPiRunDiagnosticsRequiresCapabilityAndHonorsViewerScope(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, time.Second, 40)
	session := createPiTestSession(t, httpServer.URL)
	piTestRun(t, httpServer.URL, session.ID, "业务规则")
	for _, tc := range []struct {
		caps   []string
		viewer string
		status int
		items  int
	}{{[]string{accessTokenCapabilityAIHistoryRead}, "", 403, 0}, {[]string{accessTokenCapabilityAIDiagnosticsRead}, "other-viewer", 200, 0}, {[]string{accessTokenCapabilityAIDiagnosticsRead}, "", 200, 1}} {
		token, err := server.issueAccessToken(accessTokenRequest{Capabilities: tc.caps, Scope: accessTokenScope{ViewerKey: tc.viewer}})
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/access/ai/runs", nil)
		req.Header.Set("Authorization", "Bearer "+token.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("status %d: %s", resp.StatusCode, raw)
		}
		if tc.status == 200 {
			var result struct {
				Items []piRunRow `json:"items"`
			}
			_ = json.Unmarshal(raw, &result)
			if len(result.Items) != tc.items {
				t.Fatal(string(raw))
			}
			if tc.items > 0 {
				detail, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/access/ai/runs/"+strconv.FormatInt(result.Items[0].ID, 10), nil)
				detail.Header = req.Header
				res, err := http.DefaultClient.Do(detail)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode != 200 || !bytes.Contains(body, []byte(`"messages":`)) {
					t.Fatal(string(body))
				}
			}
		}
	}
}

func TestPiQueuedDisconnectCancelsButStartedDisconnectCompletes(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, 3*time.Second, 40)
	first := createPiTestSession(t, httpServer.URL)
	second := createPiTestSession(t, httpServer.URL)
	start := func(sessionID int64, question string, until string) *http.Response {
		t.Helper()
		raw, _ := json.Marshal(piRunRequest{Question: question})
		response, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/runs", httpServer.URL, sessionID), "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(response.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(line, until) {
				break
			}
		}
		return response
	}
	running := start(first.ID, "slow", "event: tool_start")
	queued := start(second.ID, "排队时断开", "event: queued")
	queued.Body.Close()
	running.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a, err := server.listPiRuns(context.Background(), first.ID)
		if err != nil {
			t.Fatal(err)
		}
		b, err := server.listPiRuns(context.Background(), second.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) == 1 && a[0].Status == "succeeded" && len(b) == 1 && b[0].Status == "aborted" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("排队取消或运行断开后的完成状态未保存")
}

func TestPiConfigEnvironmentIsolatedAndLegacySessionsReadOnly(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, time.Second, 40)
	t.Setenv("DH_UNRELATED_SECRET", "must-not-inherit")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:8080")
	t.Setenv("NO_PROXY", "example.internal")
	spec, provider, err := server.piRuntimeSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, item := range spec.Env {
		key, value, _ := strings.Cut(item, "=")
		env[key] = value
	}
	if env["DH_UNRELATED_SECRET"] != "" || env["PI_WEB_SESSION"] != "" || env["DH_PROVIDER_KEY_0"] != "private-fixture-key" {
		t.Fatal("环境白名单不符合要求")
	}
	if !strings.Contains(env["NO_PROXY"], "127.0.0.1,localhost") || !strings.Contains(env["NO_PROXY"], "example.internal") || env["HTTPS_PROXY"] != "http://proxy.invalid:8080" {
		t.Fatal("代理配置不完整")
	}
	if provider.ProviderKey != "fixture" || strings.Contains(strings.Join(spec.Args, " "), "--no-extensions") {
		t.Fatal("pi 启动参数不符合要求")
	}
	for _, flag := range []string{"--no-tools", "--no-context-files", "--no-skills"} {
		if !strings.Contains(strings.Join(spec.Args, " "), flag) {
			t.Fatal(flag)
		}
	}
	models, err := os.ReadFile(filepath.Join(server.cfg.DataDir, "ai", "pi-agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(models, []byte(`"baseUrl": "http://model.invalid/v1"`)) {
		t.Fatal(string(models))
	}
	legacy, err := createAISession(context.Background(), server.db, "旧会话", "", AIQuestionScope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/messages", "/messages/stream", "/runs"} {
		resp, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d%s", httpServer.URL, legacy.ID, suffix), "application/json", strings.NewReader(`{"question":"旧会话提问"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 409 {
			t.Fatalf("legacy %s: %d", suffix, resp.StatusCode)
		}
	}
}

func updatePiTestModel(t *testing.T, server *Server, model string) {
	t.Helper()
	active, err := ensureActiveAIConfig(context.Background(), server.db)
	if err != nil {
		t.Fatal(err)
	}
	active.Config.Chat.Providers[0].Model = model
	raw, hash, err := aiConfigJSONAndHash(active.Config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.db.Exec(`UPDATE ai_config_versions SET version=version+1,config_json=?,config_hash=? WHERE id=?`, raw, hash, active.ID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPiSameQuestionRetryAssociatesWithUserEntry(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, time.Second, 40)
	session := createPiTestSession(t, httpServer.URL)
	_, err := server.db.Exec(`INSERT INTO ai_runs(session_id,status,question,started_at,finished_at,error_message) VALUES(?,'failed','相同问题',?,?,'准备阶段失败')`, session.ID, nowString(), nowString())
	if err != nil {
		t.Fatal(err)
	}
	piTestRun(t, httpServer.URL, session.ID, "相同问题")
	stored, err := getAISession(context.Background(), server.db, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	turns, err := server.piSessionTranscript(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].Answer != "" || turns[1].Answer == "" || turns[1].EntryID == "" {
		t.Fatalf("重试错配: %#v", turns)
	}
}

func TestPiPreparationAbortDoesNotSubmitPromptAndTimeoutRecycles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		abort   bool
		status  string
	}{{"abort", time.Second, true, "aborted"}, {"timeout", 30 * time.Millisecond, false, "timeout"}, {"abort-and-timeout", 30 * time.Millisecond, true, "aborted"}} {
		t.Run(tc.name, func(t *testing.T) {
			server, httpServer := newPiRuntimeTestServer(t, tc.timeout, 40)
			updatePiTestModel(t, server, "slow-setup")
			session := createPiTestSession(t, httpServer.URL)
			resp, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/runs", httpServer.URL, session.ID), "application/json", strings.NewReader(`{"question":"准备阶段停止"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(line, "event: started") {
					break
				}
			}
			if tc.abort {
				result, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/abort", httpServer.URL, session.ID), "application/json", nil)
				if err != nil {
					t.Fatal(err)
				}
				result.Body.Close()
				if result.StatusCode != 202 {
					t.Fatal(result.StatusCode)
				}
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(body, []byte(`"status":"`+tc.status+`"`)) || bytes.Contains(body, []byte("event: tool_start")) {
				t.Fatal(string(body))
			}
		})
	}
}

func TestPiQueuedRunUsesUpdatedModelAndImmutableConfiguration(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, 3*time.Second, 40)
	oldSpec, _, err := server.piRuntimeSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := createPiTestSession(t, httpServer.URL)
	second := createPiTestSession(t, httpServer.URL)
	start := func(id int64, question, until string) (*http.Response, *bufio.Reader) {
		t.Helper()
		raw, _ := json.Marshal(piRunRequest{Question: question})
		resp, err := http.Post(fmt.Sprintf("%s/api/ai/sessions/%d/runs", httpServer.URL, id), "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(line, until) {
				break
			}
		}
		return resp, reader
	}
	busy, busyReader := start(first.ID, "slow", "event: tool_start")
	defer busy.Body.Close()
	queued, queuedReader := start(second.ID, "配置变更后排队", "event: queued")
	defer queued.Body.Close()
	updatePiTestModel(t, server, "replacement-model")
	newSpec, _, err := server.piRuntimeSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if oldSpec.Version == newSpec.Version || oldSpec.Env[0] == "" {
		t.Fatal("配置版本没有改变")
	}
	var oldAgentDir, newAgentDir string
	for _, item := range oldSpec.Env {
		if strings.HasPrefix(item, "PI_CODING_AGENT_DIR=") {
			oldAgentDir = strings.TrimPrefix(item, "PI_CODING_AGENT_DIR=")
		}
	}
	for _, item := range newSpec.Env {
		if strings.HasPrefix(item, "PI_CODING_AGENT_DIR=") {
			newAgentDir = strings.TrimPrefix(item, "PI_CODING_AGENT_DIR=")
		}
	}
	oldRaw, _ := os.ReadFile(filepath.Join(oldAgentDir, "models.json"))
	newRaw, _ := os.ReadFile(filepath.Join(newAgentDir, "models.json"))
	if oldAgentDir == newAgentDir || bytes.Contains(oldRaw, []byte("replacement-model")) || !bytes.Contains(newRaw, []byte("replacement-model")) {
		t.Fatal("忙进程的配置快照被覆盖")
	}
	_, _ = io.Copy(io.Discard, busyReader)
	_, _ = io.Copy(io.Discard, queuedReader)
	runs, err := server.listPiRuns(context.Background(), second.ID)
	if err != nil || len(runs) != 1 || runs[0].Model != "replacement-model" || runs[0].Status != "succeeded" {
		t.Fatalf("排队配置: %#v %v", runs, err)
	}
}

func TestPiUnknownVersionTranscriptUsesTargetSession(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, time.Second, 40)
	first := createPiTestSession(t, httpServer.URL)
	second := createPiTestSession(t, httpServer.URL)
	piTestRun(t, httpServer.URL, first.ID, "目标会话问题")
	piTestRun(t, httpServer.URL, second.ID, "另一个会话问题")
	stored, err := getAISession(context.Background(), server.db, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	path, err := server.piSessionPath(stored.PiSessionFile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index := bytes.IndexByte(raw, '\n')
	if index < 0 {
		t.Fatal("missing session header")
	}
	header := bytes.Replace(raw[:index], []byte(`"version":3`), []byte(`"version":999`), 1)
	if err := os.WriteFile(path, append(append(header, '\n'), raw[index+1:]...), 0o600); err != nil {
		t.Fatal(err)
	}
	turns, err := server.piSessionTranscript(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Question != "目标会话问题" || turns[0].Answer == "" {
		t.Fatalf("fallback读取了错误会话: %#v", turns)
	}
}

func TestPiTranscriptRetainsRunsCreatedBeforeEntryIDMigration(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, time.Second, 40)
	session := createPiTestSession(t, httpServer.URL)
	piTestRun(t, httpServer.URL, session.ID, "旧 pi 历史")
	_, err := server.db.Exec(`UPDATE ai_runs SET pi_entry_id='',started_at=substr(started_at,1,19)||'Z',finished_at=substr(finished_at,1,19)||'Z' WHERE session_id=?`, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := getAISession(context.Background(), server.db, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	turns, err := server.piSessionTranscript(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Answer == "" || len(turns[0].Steps) != 1 {
		t.Fatalf("早期 pi 历史丢失: %#v", turns)
	}
}

func TestPiFallbackCancellationDiscardsSessionSwitchProcess(t *testing.T) {
	server, httpServer := newPiRuntimeTestServer(t, time.Second, 40)
	updatePiTestModel(t, server, "slow-switch")
	session := createPiTestSession(t, httpServer.URL)
	piTestRun(t, httpServer.URL, session.ID, "读取前一轮")
	stored, err := getAISession(context.Background(), server.db, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	path, err := server.piSessionPath(stored.PiSessionFile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := server.piTranscriptFallback(ctx, stored, path); done <- err }()
	marker := filepath.Join(server.cfg.DataDir, "ai", "pi-cwd", "switch-started")
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("没有开始 switch_session")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("取消没有中止读取")
	}
	// 未完成的切换必须被杀掉，不能在同一进程内迟到覆盖下一次借用。
	lease, err := server.pi.pool.AcquireLatest(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.pi.pool.Release(lease)
	response, err := lease.Command(context.Background(), "get_state", nil)
	if err != nil {
		t.Fatal(err)
	}
	if piString(piResponseData(response)["sessionFile"]) != "" {
		t.Fatal("已取消的切换进程被重新借出")
	}
}
