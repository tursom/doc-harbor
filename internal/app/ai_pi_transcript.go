package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errPiSessionVersion 提示调用方改用 switch_session + get_messages。
// 使用 errors.Is 判断；解析器不自行借进程，避免侵入主会话的进程池生命周期。
var errPiSessionVersion = errors.New("unsupported pi session version")

// piTranscriptTurn 是历史接口与运行记录共用的展示结构。解析器不猜数据库 ID，
// RunID/Feedback 以及数据库保存的完整 Scope、Status 由 API 层按 ai_runs 补齐。
type piTranscriptTurn struct {
	// EntryID 用于与 ai_runs 稳定关联，同题重试不能按问题文本错配。
	EntryID  string `json:"-"`
	RunID    int64  `json:"run_id"`
	Question string `json:"question"`
	// Supplements 保存同轮后续 user 消息，重新打开历史时仍能看到实际补充的条件。
	Supplements []string              `json:"supplements,omitempty"`
	Role        string                `json:"role"`
	Scope       AIQuestionScope       `json:"scope"`
	Status      string                `json:"status"`
	Steps       []piTranscriptStep    `json:"steps"`
	Answer      string                `json:"answer"`
	Citations   []piCitation          `json:"citations"`
	Usage       piUsage               `json:"usage"`
	Feedback    *piTranscriptFeedback `json:"feedback"`
	// 前端按运行元数据展示用量和失败原因，不需要额外申请诊断能力。
	Model        string `json:"model,omitempty"`
	ProviderKey  string `json:"provider_key,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	ToolCalls    int    `json:"tool_calls,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	// ScopeLabel 保留 context 中的仓库名称；JSONL 本身没有对应的数据库 ID。
	ScopeLabel string `json:"scope_label,omitempty"`
	// Messages 保存原始消息（含补充说明），仅诊断使用，普通历史不输出原始 context。
	Messages []PiTranscriptMessage `json:"-"`
}

// PiTranscriptMessage 对应 pi 1.0.0 的 AgentMessage。Content 同时接受 user 的
// 字符串和 assistant/toolResult 的 block 数组，不把 thinking 当最终回答。
type PiTranscriptMessage struct {
	// Raw 保存未知字段及工具 details，诊断输出时不丢失原始协议记录。
	Raw          json.RawMessage `json:"-"`
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	ToolName     string          `json:"toolName,omitempty"`
	IsError      bool            `json:"isError,omitempty"`
	StopReason   string          `json:"stopReason,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
	Provider     string          `json:"provider,omitempty"`
	Model        string          `json:"model,omitempty"`
	Timestamp    int64           `json:"timestamp"`
	Usage        piMessageUsage  `json:"usage,omitempty"`
}

// PiTranscriptStep 将 assistant.toolCall 和 toolResult 按 call ID 配成一步。
// Result 始终保存完整纯文本；preview 只在展示时按 Unicode 字符数截取。
type PiTranscriptStep struct {
	CallID  string         `json:"call_id"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Summary string         `json:"summary"`
	IsError bool           `json:"is_error"`
	Preview string         `json:"preview"`
	Result  string         `json:"-"`
}

// 包内别名避免运行/API 层再定义一套字段相同的类型。
type piTranscriptStep = PiTranscriptStep

// 运行记录层已经定义反馈结构，展示层直接复用，避免赋值时出现不同命名类型。
type piTranscriptFeedback = piRunFeedback

// piUsage 使用 ai_runs 的字段口径；reasoning 已包含在 output 中，不重复累加。
type piUsage struct {
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost"`
}

// piMessageUsage 保持 pi 原生 camelCase 和嵌套 cost，不能直接用 piUsage 解码。
type piMessageUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	TotalTokens int64 `json:"totalTokens"`
	Cost        struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

type piSessionEntry struct {
	Type     string              `json:"type"`
	ID       string              `json:"id"`
	ParentID *string             `json:"parentId"`
	Version  int                 `json:"version"`
	Message  PiTranscriptMessage `json:"message"`
	Usage    piMessageUsage      `json:"usage"`
}

type piContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// parsePiSession 读取 pi 1.0.0 使用的 v3（兼容同树结构的 v2）JSONL。
// 文件中的最后一个 entry 是本接口约定的当前叶子；如客户端仅在内存 branch()
// 而未追加 entry，文件无法表达该位置，应由运行层通过 RPC get_entries 获取 leafId。
// compaction/context_edit 只改变模型上下文，历史展示必须继续使用原始 message。
func parsePiSession(path string) ([]piTranscriptTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parsePiSessionReader(f)
}

func parsePiSessionReader(input io.Reader) ([]piTranscriptTurn, error) {
	reader := bufio.NewReader(input)
	entries := make(map[string]piSessionEntry)
	var leaf string
	headerSeen := false
	for lineNo := 1; ; lineNo++ {
		// ReadBytes 没有 Scanner 的 64KB 单行上限，且只认 LF，保留 U+2028/U+2029。
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if len(bytes.TrimSpace(line)) != 0 {
			var entry piSessionEntry
			if err := json.Unmarshal(line, &entry); err != nil {
				return nil, fmt.Errorf("pi session line %d: %w", lineNo, err)
			}
			if !headerSeen {
				if entry.Type != "session" {
					return nil, fmt.Errorf("pi session missing header")
				}
				if entry.Version != 2 && entry.Version != 3 {
					return nil, fmt.Errorf("%w: %d", errPiSessionVersion, entry.Version)
				}
				headerSeen = true
			} else {
				if entry.Type == "session" || entry.ID == "" {
					return nil, fmt.Errorf("pi session line %d: invalid entry", lineNo)
				}
				if _, duplicate := entries[entry.ID]; duplicate {
					return nil, fmt.Errorf("pi session line %d: duplicate entry %s", lineNo, entry.ID)
				}
				if entry.Type == "message" {
					var raw struct {
						Message json.RawMessage `json:"message"`
					}
					_ = json.Unmarshal(line, &raw)
					entry.Message.Raw = raw.Message
				}
				entries[entry.ID] = entry
				leaf = entry.ID
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if !headerSeen {
		return nil, fmt.Errorf("pi session missing header")
	}
	branch := make([]piSessionEntry, 0)
	visited := make(map[string]bool)
	for leaf != "" {
		if visited[leaf] {
			return nil, fmt.Errorf("pi session parent cycle at %s", leaf)
		}
		visited[leaf] = true
		entry, ok := entries[leaf]
		if !ok {
			return nil, fmt.Errorf("pi session missing parent %s", leaf)
		}
		branch = append(branch, entry)
		leaf = ""
		if entry.ParentID != nil {
			leaf = *entry.ParentID
		}
	}
	for i, j := 0, len(branch)-1; i < j; i, j = i+1, j-1 {
		branch[i], branch[j] = branch[j], branch[i]
	}
	return piBuildTranscript(branch), nil
}

// parsePiMessages 接受 get_messages 的完整 response、data 对象或裸 messages 数组。
// 它与 JSONL 共用分组逻辑；get_messages 本身不含被 compaction 省略的历史，
// 不应把 compactionSummary 假装成用户问题，也不应补造原文。
func parsePiMessages(data json.RawMessage) ([]piTranscriptTurn, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty pi get_messages")
	}
	if data[0] != '[' {
		var envelope struct {
			Type     string          `json:"type"`
			Command  string          `json:"command"`
			Success  bool            `json:"success"`
			Error    string          `json:"error"`
			Data     json.RawMessage `json:"data"`
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, err
		}
		if envelope.Type == "response" {
			if envelope.Command != "get_messages" || !envelope.Success {
				return nil, fmt.Errorf("pi get_messages failed: %s", envelope.Error)
			}
			return parsePiMessages(envelope.Data)
		}
		if len(envelope.Messages) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Messages), []byte("null")) {
			return nil, fmt.Errorf("pi get_messages missing messages")
		}
		return parsePiMessages(envelope.Messages)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	entries := make([]piSessionEntry, 0, len(messages))
	for _, raw := range messages {
		var msg PiTranscriptMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, err
		}
		msg.Raw = raw
		entries = append(entries, piSessionEntry{Type: "message", Message: msg})
	}
	return piBuildTranscript(entries), nil
}

// piParseContent 只展示文本块，图片和 thinking 原样保存在 Raw 供诊断。
func piParseContent(content json.RawMessage) (string, []piContentBlock) {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text, nil
	}
	var blocks []piContentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return "", nil
	}
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n"), blocks
}

func piBuildTranscript(entries []piSessionEntry) []piTranscriptTurn {
	turns := make([]piTranscriptTurn, 0)
	var turn *piTranscriptTurn
	calls := make(map[string]int)
	for _, entry := range entries {
		if entry.Type != "message" {
			if turn != nil && (entry.Type == "usage" || entry.Type == "compaction" || entry.Type == "branch_summary") {
				piAddUsage(&turn.Usage, entry.Usage)
			}
			continue
		}
		msg := entry.Message
		text, blocks := piParseContent(msg.Content)
		if turn == nil && (msg.Role == "assistant" || msg.Role == "toolResult") {
			// RPC 的压缩保留点可能在 assistant 中间，问题已不在消息列表中。
			// 用空问题表达缺失，保留实际返回的后缀，不能把摘要猜成原问题。
			turns = append(turns, piTranscriptTurn{Role: "未指定", Status: "running", Steps: []piTranscriptStep{}, Citations: []piCitation{}})
			turn = &turns[len(turns)-1]
		}
		switch msg.Role {
		case "user":
			question, role, label, scope, wrapped := piStripQuestionContext(text)
			// DocHarbor 的每次新提问都有完整包装；steer 没有。pi 没有持久化
			// streamingBehavior，因此绝不能以每条 user 消息作为新 run。
			if turn == nil || wrapped {
				turns = append(turns, piTranscriptTurn{EntryID: entry.ID, Question: question, Role: role, Scope: scope,
					ScopeLabel: label, Status: "running", Steps: []piTranscriptStep{}, Citations: []piCitation{}})
				turn = &turns[len(turns)-1]
				calls = make(map[string]int)
			} else {
				turn.Status = "running"
				turn.Supplements = append(turn.Supplements, text)
			}
			turn.Messages = append(turn.Messages, msg)
		case "assistant":
			if turn == nil {
				continue
			}
			turn.Messages = append(turn.Messages, msg)
			piAddUsage(&turn.Usage, msg.Usage)
			if text != "" {
				turn.Answer = text
			}
			for _, block := range blocks {
				if block.Type != "toolCall" {
					continue
				}
				calls[block.ID] = len(turn.Steps)
				args := block.Arguments
				if args == nil {
					args = map[string]any{}
				}
				turn.Steps = append(turn.Steps, piTranscriptStep{CallID: block.ID, Tool: block.Name, Args: args})
			}
			switch msg.StopReason {
			case "stop", "length":
				turn.Status = "succeeded"
			case "error":
				turn.Status = "failed"
			case "aborted":
				turn.Status = "aborted"
			default:
				turn.Status = "running"
			}
		case "toolResult":
			if turn == nil {
				continue
			}
			turn.Messages = append(turn.Messages, msg)
			piAddUsage(&turn.Usage, msg.Usage)
			index, found := calls[msg.ToolCallID]
			if !found {
				// RPC 压缩后的上下文可能只剩 result；保留它，不丢可核实原文。
				index = len(turn.Steps)
				calls[msg.ToolCallID] = index
				turn.Steps = append(turn.Steps, piTranscriptStep{CallID: msg.ToolCallID, Tool: msg.ToolName, Args: map[string]any{}})
			}
			step := &turn.Steps[index]
			step.Result, step.IsError = text, msg.IsError
			step.Summary = strings.TrimSuffix(strings.SplitN(text, "\n", 2)[0], "\r")
			step.Preview = piTextPreview(text)
		}
	}
	for i := range turns {
		seen := make(piSeenRanges)
		for _, step := range turns[i].Steps {
			seen.AddToolResult(step.Result)
		}
		turns[i].Citations = verifyPiCitations(turns[i].Answer, seen)
	}
	return turns
}

func piAddUsage(total *piUsage, usage piMessageUsage) {
	total.InputTokens += usage.Input
	total.OutputTokens += usage.Output
	total.CacheReadTokens += usage.CacheRead
	total.CacheWriteTokens += usage.CacheWrite
	total.TotalTokens += usage.TotalTokens
	total.Cost += usage.Cost.Total
}

// piTextPreview 计数 Unicode 码点而不是 UTF-8 字节，避免在中文字符中间截断。
func piTextPreview(text string) string {
	count := 0
	for offset := range text {
		if count == 2000 {
			return text[:offset]
		}
		count++
	}
	return text
}

// piStripQuestionContext 只识别 DocHarbor 固定的完整前缀，拒绝缺字段、未知
// 字段、非法角色和非法范围；不搜索正文里的 context，也只剥一次外层。
// 与服务包装逐字相同的用户自写前缀，仅凭原生 JSONL 无法区分，真实 scope
// 应始终以 ai_runs 保存的元数据为准，Messages.Raw 则始终保留用户原文。
func piStripQuestionContext(text string) (question, role, label string, scope AIQuestionScope, wrapped bool) {
	question, role = text, "未指定"
	if !strings.HasPrefix(text, "<context>\n") && !strings.HasPrefix(text, "<context>\r\n") {
		return
	}
	end := strings.Index(text, "</context>")
	if end < 0 {
		return
	}
	separator := "</context>\n\n"
	if strings.HasPrefix(text[end:], "</context>\r\n\r\n") {
		separator = "</context>\r\n\r\n"
	}
	if !strings.HasPrefix(text[end:], separator) {
		return
	}
	// 仅规范化元数据换行，用户问题部分按原字节保留（包括 CRLF）。
	header := strings.ReplaceAll(text[:end], "\r\n", "\n")
	if !strings.HasSuffix(header, "\n") {
		return
	}
	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(header, "<context>\n"), "\n"), "\n")
	if len(lines) < 2 || len(lines) > 3 || !strings.HasPrefix(lines[0], "role: ") || !strings.HasPrefix(lines[1], "scope: ") {
		return
	}
	candidateRole := strings.TrimPrefix(lines[0], "role: ")
	switch candidateRole {
	case "开发", "产品", "测试", "未指定":
	default:
		return
	}
	candidateLabel := strings.TrimPrefix(lines[1], "scope: ")
	candidateScope := AIQuestionScope{SourceMode: "smart_latest_with_branch_candidates"}
	switch {
	case candidateLabel == "全部仓库":
		candidateScope.RepoMode = "global"
	case candidateLabel == "当前文件":
		candidateScope.RepoMode = "current_file"
	case strings.HasPrefix(candidateLabel, "当前仓库 ") && strings.TrimSpace(strings.TrimPrefix(candidateLabel, "当前仓库 ")) != "":
		candidateScope.RepoMode = "selected"
	default:
		return
	}
	if len(lines) == 3 {
		if candidateLabel != "当前文件" || !strings.HasPrefix(lines[2], "current_file: ") {
			return
		}
		file, ok := piParseCitationFile(strings.TrimPrefix(lines[2], "current_file: "))
		if !ok {
			return
		}
		candidateScope.CurrentFile = &AICurrentFileScope{Branch: file.Ref, FilePath: file.Path}
	}
	question = text[end+len(separator):]
	role, label, scope, wrapped = candidateRole, candidateLabel, candidateScope, true
	return
}

type piTranscriptDiagnosticTurn struct {
	piTranscriptTurn
	Messages []json.RawMessage `json:"messages"`
}

// piTranscriptDiagnostics 创建独立展示副本，工具 preview 改为全文；不修改普通
// 历史对象。原始 messages 同时保留 context、steer、thinking 和工具 details。
func piTranscriptDiagnostics(turns []piTranscriptTurn) []piTranscriptDiagnosticTurn {
	result := make([]piTranscriptDiagnosticTurn, 0, len(turns))
	for _, turn := range turns {
		copyTurn := turn
		copyTurn.Steps = append([]piTranscriptStep{}, turn.Steps...)
		for i := range copyTurn.Steps {
			copyTurn.Steps[i].Preview = copyTurn.Steps[i].Result
		}
		diagnostic := piTranscriptDiagnosticTurn{piTranscriptTurn: copyTurn, Messages: []json.RawMessage{}}
		for _, message := range turn.Messages {
			raw := message.Raw
			if len(raw) == 0 {
				raw, _ = json.Marshal(message)
			}
			diagnostic.Messages = append(diagnostic.Messages, raw)
		}
		result = append(result, diagnostic)
	}
	return result
}
