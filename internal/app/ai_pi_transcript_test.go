package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPiTranscriptNativeGolden(t *testing.T) {
	turns, err := parsePiSession(filepath.Join("testdata", "pi-transcript", "pi-1.0.0-v3.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "pi-transcript", "transcript.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(turns)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("golden differs:\n%s", got)
	}
	if len(turns[0].Messages) != 5 {
		t.Fatalf("工具、补充说明、压缩前原文应保留：%d", len(turns[0].Messages))
	}
	if string(turns[0].Messages[3].Content) != `[{"type":"text","text":"也说明异常情况。"}]` {
		t.Fatalf("steer 没有归入原轮：%s", turns[0].Messages[3].Content)
	}
	if !reflect.DeepEqual(turns[0].Supplements, []string{"也说明异常情况。"}) {
		t.Fatalf("普通历史丢失补充说明：%+v", turns[0].Supplements)
	}
	if strings.Contains(string(got), "压缩摘要") || strings.Contains(string(got), "放弃") || strings.Contains(string(got), "thinking") {
		t.Fatalf("展示混入摘要、弃分支或 thinking：%s", got)
	}
	// 原文 context_edit(null) 不应让历史中首个 assistant 的工具调用消失。
	if len(turns[0].Steps) != 1 || !strings.Contains(string(turns[0].Messages[1].Raw), "thinking") {
		t.Fatal("context_edit 不能改变原始历史")
	}
}

func TestPiTranscriptRPCSameShape(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "pi-transcript", "get-messages.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Messages json.RawMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{data, envelope.Data.Messages, append(append([]byte(`{"messages":`), envelope.Data.Messages...), '}')} {
		turns, err := parsePiMessages(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) != 2 || turns[0].Question != "秒杀库存有哪些限制？" || len(turns[0].Steps) != 1 || turns[1].Status != "aborted" {
			t.Fatalf("RPC 分组错误：%+v", turns)
		}
		if turns[0].Usage.InputTokens != 20 {
			t.Fatal("RPC 不含独立 usage entry，不能补造该用量")
		}
		if !reflect.DeepEqual(turns[0].Supplements, []string{"也说明异常情况。"}) {
			t.Fatal("RPC 历史应保留补充说明")
		}
		if turns[0].Citations[0].Status != "verified" {
			t.Fatal("RPC 应共享引用核实")
		}
	}
	for _, input := range []string{``, `{}`, `{"messages":null}`, `{"type":"response","command":"get_messages","success":false,"error":"boom"}`, `{"type":"response","command":"get_state","success":true,"data":{}}`} {
		if _, err := parsePiMessages(json.RawMessage(input)); err == nil {
			t.Fatalf("应拒绝无效 RPC：%s", input)
		}
	}
}

func TestPiTranscriptCompactedRPCDoesNotInventHistory(t *testing.T) {
	turns, err := parsePiMessages(json.RawMessage(`[
		{"role":"compactionSummary","summary":"旧问题已被压缩"},
		{"role":"user","content":"<context>\nrole: 产品\nscope: 全部仓库\n</context>\n\n现在的问题"},
		{"role":"assistant","content":[{"type":"text","text":"只回答现在的问题"}],"stopReason":"stop"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Question != "现在的问题" {
		t.Fatalf("错误的压缩兜底：%+v", turns)
	}
	partial, err := parsePiMessages(json.RawMessage(`[
		{"role":"compactionSummary","summary":"不应当作问题"},
		{"role":"toolResult","toolCallId":"old","toolName":"read","content":[{"type":"text","text":"压缩后仍保留的工具结果"}]},
		{"role":"assistant","content":[{"type":"text","text":"部分历史中的答案"}],"stopReason":"stop"}
	]`))
	if err != nil || len(partial) != 1 || partial[0].Question != "" || len(partial[0].Steps) != 1 || partial[0].Answer != "部分历史中的答案" {
		t.Fatalf("压缩的无问题后缀必须保留且不能伪造问题：%+v %v", partial, err)
	}
}

func TestPiTranscriptContextPreservesUserText(t *testing.T) {
	validInner := "<context>\nrole: 开发\nscope: 全部仓库\n</context>\n\n请解释这个标签。"
	for _, text := range []string{
		"前文\n" + validInner,
		"<context>原文</context>\n\n问题",
		"<context>\nrole: user\nscope: 全部仓库\n</context>\n\n问题",
		"<context>\nrole: 开发\nsecret: 全部仓库\n</context>\n\n问题",
		"<context>\nrole: 开发\nscope: 任意范围\n</context>\n\n问题",
		"<context>\nrole: 开发\nscope: 全部仓库\ncurrent_file: r@main:a.go\n</context>\n\n问题",
	} {
		question, _, _, _, wrapped := piStripQuestionContext(text)
		if wrapped || question != text {
			t.Fatalf("误剥用户原文：%q => %q", text, question)
		}
	}
	outer := "<context>\nrole: 产品\nscope: 当前文件\ncurrent_file: 游戏服务@feature/test:doc/中文 空格.md\n</context>\n\n" + validInner
	question, role, label, scope, wrapped := piStripQuestionContext(outer)
	if !wrapped || question != validInner || role != "产品" || label != "当前文件" || scope.CurrentFile.FilePath != "doc/中文 空格.md" || scope.CurrentFile.Branch != "feature/test" {
		t.Fatalf("外层 context 解析错误：%q %q %+v", question, role, scope)
	}
	crlf := "<context>\r\nrole: 开发\r\nscope: 全部仓库\r\n</context>\r\n\r\n正文\r\n保留换行\r\n"
	question, _, _, _, wrapped = piStripQuestionContext(crlf)
	if !wrapped || question != "正文\r\n保留换行\r\n" {
		t.Fatalf("用户正文 CRLF 被改写：%q", question)
	}
}

func TestPiTranscriptLargeToolAndDiagnostics(t *testing.T) {
	full := "首行摘要\n" + strings.Repeat("中文😀", 400000) + "\u2028尾部\u2029"
	messages := []map[string]any{
		{"role": "user", "content": "问题"},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "toolCall", "id": "a", "name": "read", "arguments": map[string]any{"path": "中文 空格.md"}},
			map[string]any{"type": "toolCall", "id": "b", "name": "search", "arguments": map[string]any{"query": "库存"}},
		}, "stopReason": "toolUse"},
		{"role": "toolResult", "toolCallId": "b", "toolName": "search", "content": []any{map[string]any{"type": "text", "text": "搜索错误"}}, "isError": true},
		{"role": "toolResult", "toolCallId": "a", "toolName": "read", "content": []any{map[string]any{"type": "text", "text": full}}, "details": map[string]any{"diagnostic": "完整字段"}},
		{"role": "user", "content": []any{map[string]any{"type": "text", "text": "补充说明"}}},
		{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "完成"}}, "stopReason": "stop"},
	}
	var jsonl strings.Builder
	jsonl.WriteString("{\"type\":\"session\",\"version\":3}\r\n")
	for i, message := range messages {
		var parent any
		if i > 0 {
			parent = string(rune('a' + i - 1))
		}
		line, err := json.Marshal(map[string]any{"type": "message", "id": string(rune('a' + i)), "parentId": parent, "message": message})
		if err != nil {
			t.Fatal(err)
		}
		jsonl.Write(line)
		jsonl.WriteString("\r\n")
	}
	turns, err := parsePiSessionReader(strings.NewReader(strings.TrimSuffix(jsonl.String(), "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || len(turns[0].Steps) != 2 {
		t.Fatalf("steer 被拆轮或工具配对失败：%+v", turns)
	}
	step := turns[0].Steps[0]
	if step.CallID != "a" || step.Result != full || step.Summary != "首行摘要" || utf8.RuneCountInString(step.Preview) != 2000 || !utf8.ValidString(step.Preview) {
		t.Fatal("工具全文丢失或中文 preview 截断错误")
	}
	if !turns[0].Steps[1].IsError || turns[0].Steps[1].Preview != "搜索错误" {
		t.Fatal("并行结果按顺序而非 call ID 配对")
	}
	diagnostics := piTranscriptDiagnostics(turns)
	if diagnostics[0].Steps[0].Preview != full || len(diagnostics[0].Messages) != len(messages) {
		t.Fatal("诊断被截断")
	}
	if !strings.Contains(string(diagnostics[0].Messages[3]), "完整字段") {
		t.Fatal("工具 details 丢失")
	}
	if utf8.RuneCountInString(turns[0].Steps[0].Preview) != 2000 {
		t.Fatal("诊断修改了普通历史对象")
	}
}

func TestPiTranscriptMalformedSession(t *testing.T) {
	for _, version := range []string{"1", "4", "999"} {
		_, err := parsePiSessionReader(strings.NewReader(`{"type":"session","version":` + version + `}`))
		if !errors.Is(err, errPiSessionVersion) {
			t.Fatalf("未知版本必须返回 sentinel：%v", err)
		}
	}
	for _, fixture := range []string{
		"", `{}`, "{\"type\":\"session\",\"version\":3}\n{",
		"{\"type\":\"session\",\"version\":3}\n{\"type\":\"message\",\"id\":\"a\",\"parentId\":\"missing\"}",
		"{\"type\":\"session\",\"version\":3}\n{\"type\":\"message\",\"id\":\"a\",\"parentId\":\"a\"}",
		"{\"type\":\"session\",\"version\":3}\n{\"type\":\"custom\",\"id\":\"a\"}\n{\"type\":\"custom\",\"id\":\"a\"}",
	} {
		if _, err := parsePiSessionReader(strings.NewReader(fixture)); err == nil {
			t.Fatalf("应拒绝损坏会话：%s", fixture)
		}
	}
	turns, err := parsePiSessionReader(strings.NewReader(`{"type":"session","version":2}`))
	if err != nil || len(turns) != 0 {
		t.Fatalf("空的已知树版本应可读取：%v", err)
	}
}
