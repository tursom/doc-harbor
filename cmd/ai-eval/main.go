// ai-eval 通过 DocHarbor 的公开 HTTP 协议采集评估材料，正确性由维护者人工评分。
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Question 保留来源和前置提问，避免把脱离上下文的追问当成独立问题评估。
type Question struct {
	ID       string          `json:"id"`
	Role     string          `json:"role"`
	Question string          `json:"question"`
	Expect   string          `json:"expect"`
	Source   string          `json:"source,omitempty"`
	Context  []string        `json:"context,omitempty"`
	Scope    json.RawMessage `json:"scope,omitempty"`
}

type event struct {
	Name string          `json:"event"`
	Data json.RawMessage `json:"data"`
}

// result 的 usage 保留后端原始字段；不存在的成本或 token 不伪造为零。
type result struct {
	Question
	Engine       string          `json:"engine"`
	SessionID    int64           `json:"session_id"`
	RunID        int64           `json:"run_id,omitempty"`
	Status       string          `json:"status"`
	Answer       string          `json:"answer"`
	Usage        json.RawMessage `json:"usage"`
	Final        json.RawMessage `json:"final,omitempty"`
	ElapsedMS    int64           `json:"elapsed_ms"`
	RunElapsedMS int64           `json:"run_elapsed_ms"`
	ToolCalls    int             `json:"tool_calls"`
	Error        string          `json:"error,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
	Output       string          `json:"output"`
}

type client struct {
	base   string
	engine string
	http   *http.Client
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("ai-eval", flag.ContinueOnError)
	engine := flags.String("engine", "legacy", "评估引擎：pi 或 legacy")
	file := flags.String("file", "testdata/ai-eval/questions.jsonl", "JSONL 题集")
	base := flags.String("url", "http://127.0.0.1:14220", "DocHarbor 根地址，不含 /api")
	out := flags.String("out", "out/ai-eval", "输出根目录，自动创建时间子目录")
	timeout := flags.Duration("timeout", 6*time.Minute, "每轮提问超时，含排队")
	scope := flags.String("scope", "", "覆盖所有题目的 scope JSON，例如指定 fixture 仓库 ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*engine != "legacy" && *engine != "pi") || *timeout <= 0 {
		return errors.New("参数错误：engine 必须为 pi|legacy，timeout 必须大于零，不接受位置参数")
	}
	u, err := url.Parse(*base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("url 必须是 HTTP(S) 服务根地址，不含凭据、查询参数或 fragment")
	}
	questions, err := loadQuestions(*file)
	if err != nil {
		return err
	}
	if *scope != "" {
		if !json.Valid([]byte(*scope)) || !strings.HasPrefix(strings.TrimSpace(*scope), "{") {
			return errors.New("scope 必须是 JSON 对象")
		}
		for i := range questions {
			questions[i].Scope = json.RawMessage(*scope)
		}
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(*out, time.Now().Format("20060102-150405")+"-"+*engine+"-")
	if err != nil {
		return err
	}
	// 固化实际题集（含 scope 覆盖），便于两次引擎评估核对输入是否一致。
	if err := saveJSON(filepath.Join(dir, "questions.json"), questions); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c := client{base: strings.TrimRight(*base, "/"), engine: *engine, http: &http.Client{Timeout: *timeout}}
	var results []result
	failed := 0
	for i, q := range questions {
		if ctx.Err() != nil {
			break
		}
		name := fmt.Sprintf("%03d-%s", i+1, q.ID)
		r, err := c.evaluate(ctx, q, filepath.Join(dir, name))
		r.Output = name
		if err != nil {
			r.Status, r.Error = "failed", err.Error()
			failed++
		}
		if err := saveJSON(filepath.Join(dir, name, "result.json"), r); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name, "answer.md"), []byte(r.Answer+"\n"), 0644); err != nil {
			return err
		}
		results = append(results, r)
		// 每题更新汇总，长批次中断后仍能人工检查已完成的题目。
		if err := saveJSON(filepath.Join(dir, "results.json"), results); err != nil {
			return err
		}
		if err := writeScorecard(filepath.Join(dir, "scores.md"), *engine, results); err != nil {
			return err
		}
		fmt.Fprintf(output, "[%d/%d] %s %s (%dms)\n", i+1, len(questions), q.ID, r.Status, r.ElapsedMS)
	}
	fmt.Fprintf(output, "评估材料：%s\n", dir)
	if ctx.Err() != nil {
		return errors.New("评估已中断；已有材料已保存，服务端 pi run 可能继续运行，请按会话 ID 检查")
	}
	if failed > 0 {
		return fmt.Errorf("%d 道题采集失败，详见逐题 result.json 和 scores.md", failed)
	}
	return nil
}

func loadQuestions(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var questions []Question
	seen := map[string]bool{}
	for line := 1; ; line++ {
		raw, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			var q Question
			if e := json.Unmarshal(raw, &q); e != nil {
				return nil, fmt.Errorf("题集第 %d 行：%w", line, e)
			}
			// ID 用作目录名，只允许简单标识，防止意外写到输出目录之外。
			validID := q.ID != "" && strings.IndexFunc(q.ID, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
			}) == -1
			if !validID || seen[q.ID] || strings.TrimSpace(q.Question) == "" || strings.TrimSpace(q.Expect) == "" || (q.Role != "开发" && q.Role != "产品" && q.Role != "测试") {
				return nil, fmt.Errorf("题集第 %d 行：id 须唯一且仅含字母数字-_，role 须为开发/产品/测试，question/expect 不可为空", line)
			}
			if len(q.Scope) > 0 && !strings.HasPrefix(strings.TrimSpace(string(q.Scope)), "{") {
				return nil, fmt.Errorf("题集第 %d 行：scope 必须是对象", line)
			}
			seen[q.ID] = true
			questions = append(questions, q)
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if len(questions) == 0 {
		return nil, errors.New("题集为空")
	}
	return questions, nil
}

func (c client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		// 不读取错误正文，避免不受控的代理错误页意外包含凭据。
		return nil, fmt.Errorf("%s 返回 HTTP %d；检查 API 版本、引擎设置及服务端日志", path, resp.StatusCode)
	}
	return resp, nil
}

func (c client) evaluate(ctx context.Context, q Question, dir string) (r result, err error) {
	r = result{Question: q, Engine: c.engine, Status: "succeeded", Usage: json.RawMessage("null")}
	start := time.Now()
	defer func() { r.ElapsedMS = time.Since(start).Milliseconds() }()
	if err = os.MkdirAll(dir, 0755); err != nil {
		return r, err
	}
	// 即使新建会话失败，也保留空事件文件，使评分表中的产物链接始终有效。
	if err = os.WriteFile(filepath.Join(dir, "events.jsonl"), nil, 0644); err != nil {
		return r, err
	}
	scope := q.Scope
	if len(scope) == 0 {
		scope = json.RawMessage(`{"repo_mode":"global","source_mode":"smart_latest_with_branch_candidates"}`)
	}
	body := map[string]any{"title": "ai-eval " + q.ID, "scope": scope}
	// 两版 decodeBody 均拒绝未知字段；engine 由服务端 AI_ENGINE 决定。
	// CLI 只核对响应，评估另一引擎时指向相应本地实例，不改服务器默认值。
	resp, err := c.post(ctx, "/api/ai/sessions", body)
	if err != nil {
		return r, err
	}
	var session struct {
		ID     int64  `json:"id"`
		Engine string `json:"engine"`
	}
	err = json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	if err != nil {
		return r, fmt.Errorf("解析会话：%w", err)
	}
	r.SessionID = session.ID
	if session.Engine == "" {
		session.Engine = "legacy"
	}
	if session.ID <= 0 || session.Engine != c.engine {
		return r, fmt.Errorf("会话引擎为 %s，要求 %s（或会话 ID 无效）；未发起模型调用", session.Engine, c.engine)
	}
	for i, previous := range q.Context {
		prior := result{Usage: json.RawMessage("null")}
		if err := c.ask(ctx, session.ID, previous, q.Role, scope, filepath.Join(dir, fmt.Sprintf("context-%02d.jsonl", i+1)), &prior); err != nil {
			return r, fmt.Errorf("前置问题 %d 失败：%w", i+1, err)
		}
	}
	runStart := time.Now()
	err = c.ask(ctx, session.ID, q.Question, q.Role, scope, filepath.Join(dir, "events.jsonl"), &r)
	r.RunElapsedMS = time.Since(runStart).Milliseconds()
	if err == nil && c.engine == "pi" {
		err = c.finalTranscript(ctx, session.ID, dir, &r)
	}
	return r, err
}

// pi 的 text 事件也包含工具循环中间的说明；用持久化 transcript 取最后助手消息，
// 避免把中间说明或重试草稿拼进人工评分答案。原始事件仍完整保留。
func (c client) finalTranscript(ctx context.Context, sessionID int64, dir string, r *result) error {
	path := fmt.Sprintf("/api/ai/sessions/%d/transcript", sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("获取最终 transcript：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("获取最终 transcript 返回 HTTP %d", resp.StatusCode)
	}
	var transcript struct {
		Turns []struct {
			RunID  int64  `json:"run_id"`
			Answer string `json:"answer"`
		} `json:"turns"`
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &transcript); err != nil {
		return fmt.Errorf("解析最终 transcript：%w", err)
	}
	if err := saveJSON(filepath.Join(dir, "transcript.json"), json.RawMessage(raw)); err != nil {
		return err
	}
	for _, turn := range transcript.Turns {
		if turn.RunID == r.RunID && strings.TrimSpace(turn.Answer) != "" {
			r.Answer = turn.Answer
			return nil
		}
	}
	return errors.New("最终 transcript 缺少当前 run 的回答；保留流式内容供排查")
}

func (c client) ask(ctx context.Context, sessionID int64, text, role string, scope json.RawMessage, path string, r *result) error {
	stream, err := os.Create(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	endpoint := fmt.Sprintf("/api/ai/sessions/%d/runs", sessionID)
	body := map[string]any{"question": text, "role": role, "scope": scope}
	if c.engine == "legacy" {
		endpoint = fmt.Sprintf("/api/ai/sessions/%d/messages/stream", sessionID)
		// 旧协议没有 role；将相同角色作为用户提示发送，原题仍单独存档。
		body = map[string]any{"question": "请按" + role + "角色回答。\n\n" + text, "scope_override": scope}
	}
	resp, err := c.post(ctx, endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return errors.New("提问接口未返回 text/event-stream")
	}
	done, final := false, false
	var runErr error
	err = readSSE(resp.Body, func(e event) error {
		if err := json.NewEncoder(stream).Encode(e); err != nil {
			return err
		}
		var data struct {
			Delta   string          `json:"delta"`
			Message json.RawMessage `json:"message"`
			Run     struct {
				ID int64 `json:"id"`
			} `json:"run"`
			RunID  int64           `json:"run_id"`
			Usage  json.RawMessage `json:"usage"`
			Status string          `json:"status"`
			OK     *bool           `json:"ok"`
		}
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return fmt.Errorf("SSE %s 内容无效：%w", e.Name, err)
		}
		switch e.Name {
		case "started":
			r.RunID = data.RunID
		case "text", "answer_delta":
			r.Answer += data.Delta
		case "tool_start":
			r.ToolCalls++
		case "message_done":
			// legacy 的最终消息可能经过 verifier 重写，必须覆盖累计草稿。
			var message struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(data.Message, &message); err != nil {
				return err
			}
			r.Answer, r.Final, r.RunID = message.Content, e.Data, data.Run.ID
			final = true
		case "error":
			runErr = errors.New("服务端发送 error 事件，详见 events.jsonl")
		case "done":
			done = true
			if data.RunID > 0 {
				r.RunID = data.RunID
			}
			if c.engine == "pi" && data.Status != "succeeded" {
				runErr = fmt.Errorf("pi run 结束状态：%s", data.Status)
			}
			if c.engine == "legacy" && (data.OK == nil || !*data.OK) {
				runErr = errors.New("legacy done.ok 未成功")
			}
			if c.engine == "pi" {
				r.Final = e.Data
			}
		}
		if len(data.Usage) > 0 && string(data.Usage) != "null" {
			r.Usage = data.Usage
		}
		return nil
	})
	if err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	if !done || (c.engine == "legacy" && !final) {
		return errors.New("事件流提前结束，缺少 done 或 legacy message_done；保留已收到的回答与过程")
	}
	if strings.TrimSpace(r.Answer) == "" {
		return errors.New("事件流完成但回答为空")
	}
	if string(r.Usage) == "null" {
		r.Warnings = append(r.Warnings, "API 未提供 usage，不能按零用量统计")
	}
	if err := stream.Sync(); err != nil {
		return err
	}
	return nil
}

// readSSE 使用 Reader 支持大工具结果、多行 data、CRLF、注释和文件末尾事件。
// 不使用默认 Scanner，避免 64 KB 上限使较长工具结果被静默截断。
func readSSE(input io.Reader, emit func(event) error) error {
	reader := bufio.NewReader(input)
	name := "message"
	var data []string
	flush := func() error {
		if len(data) == 0 {
			name = "message"
			return nil
		}
		err := emit(event{Name: name, Data: json.RawMessage(strings.Join(data, "\n"))})
		name, data = "message", nil
		return err
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if e := flush(); e != nil {
				return e
			}
		} else if !strings.HasPrefix(line, ":") {
			key, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch key {
			case "event":
				name = value
			case "data":
				data = append(data, value)
			}
		}
		if errors.Is(err, io.EOF) {
			return flush()
		}
	}
}

func saveJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0644)
}

func writeScorecard(path, engine string, results []result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# DocHarbor 人工评估（%s）\n\n", engine)
	b.WriteString("人工填写正确性：1=正确，0.5=部分正确，0=错误；另填引用核实、角色适配及备注。采集成功不代表回答正确。\n\n")
	b.WriteString("| ID | 角色 | 问题 | 判定要点 | 状态 | 主轮耗时(ms，含排队) | 回答与过程 | 正确性 | 引用核实 | 角色适配 | 备注 |\n| --- | --- | --- | --- | --- | ---: | --- | --- | --- | --- | --- |\n")
	var times []int64
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d | [回答](%s/answer.md) · [过程](%s/events.jsonl) · [用量](%s/result.json) | | | | %s |\n", r.ID, r.Role, cell(r.Question.Question), cell(r.Expect), r.Status, r.RunElapsedMS, r.Output, r.Output, r.Output, cell(r.Error))
		if r.Status == "succeeded" {
			times = append(times, r.RunElapsedMS)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	if len(times) > 0 {
		fmt.Fprintf(&b, "\n采集成功 %d/%d；成功题主轮耗时 P90（nearest-rank，含排队、不含前置轮）：%d ms。失败题须单独评审；不得仅凭成功子集判断切换达标。\n", len(times), len(results), times[int(math.Ceil(float64(len(times))*0.9))-1])
	}
	b.WriteString("\n两引擎对照请按 ID 对齐，核对 questions.json 中的 scope 和 context 一致。legacy usage 仅含最终回答调用的 prompt_tokens/completion_tokens；不代表整个检索与规划链路成本。pi usage 以 API 返回为准。前置轮事件单独保存在 context-*.jsonl，评分回答和 usage 只对应末轮。\n\n人工确认：评审人______ 日期______ 正确率______ P90是否可接受______ 是否进入步骤3______\n")
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\n", "<br>")
}
