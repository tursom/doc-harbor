package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 用真实 HTTP/SSE 而非模型验证两版协议，避免评估工具自身污染评估结论。
func TestEvaluateProtocols(t *testing.T) {
	for _, engine := range []string{"legacy", "pi"} {
		t.Run(engine, func(t *testing.T) {
			asks := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodGet && req.URL.Path == "/api/ai/sessions/7/transcript" {
					io.WriteString(w, `{"turns":[{"run_id":9,"answer":"最终回答"}]}`)
					return
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if req.URL.Path == "/api/ai/sessions" {
					if _, ok := body["engine"]; ok {
						t.Error("新建会话不得向原协议添加 engine 字段")
					}
					// 老版响应没有 engine；新版必须正确核对 pi。
					if engine == "legacy" {
						io.WriteString(w, `{"id":7}`)
					} else {
						io.WriteString(w, `{"id":7,"engine":"pi"}`)
					}
					return
				}
				asks++
				w.Header().Set("Content-Type", "text/event-stream")
				if engine == "legacy" {
					if req.URL.Path != "/api/ai/sessions/7/messages/stream" || body["scope_override"] == nil || body["role"] != nil {
						t.Errorf("旧版请求不兼容：%s %v", req.URL.Path, body)
					}
					io.WriteString(w, "event: answer_delta\ndata: {\"delta\":\"旧草稿\"}\n\n")
					io.WriteString(w, "event: message_done\ndata: {\"run\":{\"id\":9},\"message\":{\"content\":\"最终回答\"},\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8}}\n\n")
					io.WriteString(w, "event: done\ndata: {\"ok\":true}\n\n")
				} else {
					if req.URL.Path != "/api/ai/sessions/7/runs" || body["scope"] == nil || string(body["role"]) != `"测试"` {
						t.Errorf("pi 请求不符合契约：%s %v", req.URL.Path, body)
					}
					io.WriteString(w, "event: started\ndata: {\"run_id\":9}\n\nevent: tool_start\ndata: {\"tool\":\"read\"}\n\n")
					// 覆盖超过 1MB 的单行工具结果，避免 Scanner 长度限制回归。
					fmt.Fprintf(w, "event: tool_end\ndata: {\"preview\":%q}\n\n", strings.Repeat("x", 1024*1024+1))
					io.WriteString(w, "event: text\ndata: {\"delta\":\"先查阅文件。\"}\n\nevent: text\ndata: {\"delta\":\"最终回答\"}\n\n")
					io.WriteString(w, "event: done\ndata: {\"status\":\"succeeded\",\"usage\":{\"input_tokens\":20,\"output_tokens\":9,\"cost\":0.01}}\n\n")
				}
			}))
			defer server.Close()
			c := client{base: server.URL, engine: engine, http: server.Client()}
			dir := t.TempDir()
			r, err := c.evaluate(context.Background(), Question{ID: "qa", Role: "测试", Question: "参数？", Expect: "有字段", Context: []string{"哪个接口？"}}, dir)
			if err != nil || r.Answer != "最终回答" || r.RunID != 9 || asks != 2 || string(r.Usage) == "null" {
				t.Fatalf("采集结果：%+v asks=%d err=%v", r, asks, err)
			}
			if engine == "pi" && r.ToolCalls != 1 {
				t.Fatalf("tool calls=%d", r.ToolCalls)
			}
			for _, name := range []string{"events.jsonl", "context-01.jsonl"} {
				if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() == 0 {
					t.Fatalf("事件文件缺失：%s %v", name, err)
				}
			}
		})
	}
}

func TestEvaluateRejectsEngineMismatchBeforeAsk(t *testing.T) {
	asks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/sessions" {
			asks++
		}
		io.WriteString(w, `{"id":1,"engine":"legacy"}`)
	}))
	defer server.Close()
	c := client{base: server.URL, engine: "pi", http: server.Client()}
	_, err := c.evaluate(context.Background(), Question{ID: "x"}, t.TempDir())
	if err == nil || asks != 0 {
		t.Fatalf("错误引擎不应提问：asks=%d err=%v", asks, err)
	}
}

func TestIncompleteAndFailedStreamsRetainAnswer(t *testing.T) {
	for _, tc := range []struct{ name, engine, ending string }{
		{"truncated", "pi", ""},
		{"timeout", "pi", "event: done\ndata: {\"status\":\"timeout\"}\n\n"},
		{"error_then_done", "legacy", "event: error\ndata: {\"message\":\"失败\"}\n\nevent: done\ndata: {\"ok\":true}\n\n"},
		{"legacy_missing_final", "legacy", "event: done\ndata: {\"ok\":true}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "event: text\ndata: {\"delta\":\"部分回答\"}\n\n"+tc.ending)
			}))
			defer server.Close()
			c := client{base: server.URL, engine: tc.engine, http: server.Client()}
			r := result{Usage: json.RawMessage("null")}
			err := c.ask(context.Background(), 1, "问题", "开发", json.RawMessage(`{}`), filepath.Join(t.TempDir(), "events.jsonl"), &r)
			if err == nil || r.Answer != "部分回答" {
				t.Fatalf("失败流误判：%+v %v", r, err)
			}
		})
	}
}

func TestSSEMultilineCRLF(t *testing.T) {
	var events []event
	err := readSSE(strings.NewReader(": heartbeat\r\nevent: text\r\ndata: {\r\ndata: \"delta\":\"中文\"}\r\n\r\nevent: done\ndata: {}"), func(e event) error {
		events = append(events, e)
		return nil
	})
	if err != nil || len(events) != 2 || events[0].Name != "text" || !json.Valid(events[0].Data) || events[1].Name != "done" {
		t.Fatalf("SSE framing: %v %v", events, err)
	}
}

func TestQuestionSetAndValidation(t *testing.T) {
	questions, err := loadQuestions("../../testdata/ai-eval/questions.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, q := range questions {
		counts[q.Role]++
		if !strings.Contains(q.Source, "ai_agent_eval_test.go#") {
			t.Errorf("缺少来源：%s", q.ID)
		}
	}
	for _, role := range []string{"开发", "产品", "测试"} {
		if counts[role] < 10 {
			t.Errorf("%s 只有 %d 题", role, counts[role])
		}
	}
	for _, content := range []string{"", `{"id":"../escape","role":"测试","question":"q","expect":"e"}`, "{bad json}"} {
		path := filepath.Join(t.TempDir(), "bad.jsonl")
		os.WriteFile(path, []byte(content), 0600)
		if _, err := loadQuestions(path); err == nil {
			t.Fatalf("应拒绝题集 %q", content)
		}
	}
}

func TestRunWritesFailureArtifactsAndScorecard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	dir := t.TempDir()
	file := filepath.Join(dir, "question.jsonl")
	if err := os.WriteFile(file, []byte(`{"id":"test-1","role":"开发","question":"字段|是什么？","expect":"给出字段"}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"-url", server.URL, "-file", file, "-out", filepath.Join(dir, "out")}, io.Discard)
	if err == nil {
		t.Fatal("采集失败应返回非零")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "out", "*", "scores.md"))
	if len(files) != 1 {
		t.Fatalf("缺少人工评分表：%v", files)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil || !strings.Contains(string(raw), "字段\\|是什么") || !strings.Contains(string(raw), "failed") {
		t.Fatalf("评分表没有保留错误：%s %v", raw, err)
	}
	for _, name := range []string{"answer.md", "result.json"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(files[0]), "001-test-1", name)); err != nil {
			t.Fatal(err)
		}
	}
}
