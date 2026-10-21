package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpInstructions = "DocHarbor 只读访问公司 Git 仓库中的文档和代码。先用 list_repos / list_docs / search_docs 定位，再用 read 读原文。ref 默认取智能最新（文档）或默认分支（代码）。"

// initMCP 在启动接线时调用一次。内部令牌仅保存在 Server 内存，交由运行时通过环境变量传给子进程。
// Server 需提供 mcpToken string、mcpHandler http.Handler，Config 需提供 MCPEnabled bool。
func (s *Server) initMCP() error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("生成 MCP 内部令牌: %w", err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "DocHarbor", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
		Capabilities: &mcp.ServerCapabilities{},
	})
	registerMCPTool(server, "list_repos", "列出可访问仓库、默认分支、扫描目录、追踪分支数和最近扫描状态。", s.mcpListRepos)
	registerMCPTool(server, "list_branches", "查看仓库分支，按 HEAD 时间倒序，标明默认分支、参与智能最新和功能分支候选。", s.mcpListBranches)
	registerMCPTool(server, "list_docs", "列出仓库的设计文档、接口文档（智能最新版本）。不确定从哪里找时先用它。", s.mcpListDocs)
	registerMCPTool(server, "search_docs", "在文档中全文搜索。适合查业务规则、流程、接口说明。中文关键词用词语，不要拆成单字。", s.mcpSearchDocs)
	registerMCPTool(server, "search_code", "在代码中搜索。适合查接口路由、结构体字段、错误码、表名。可以同时搜多个仓库。默认字面匹配、不区分大小写。", s.mcpSearchCode)
	registerMCPTool(server, "read", "读取文件原文。回答前必须读到支撑结论的原文，不要只凭搜索片段下结论。", s.mcpRead)
	registerMCPTool(server, "list_dir", "浏览指定分支或提交中的目录，最多返回 300 条。先用它确认文件路径。", s.mcpListDir)
	s.mcpToken = base64.RawURLEncoding.EncodeToString(raw)
	// 工具均无会话状态，无需保留 SDK 会话和长连接；标准 streamable HTTP 客户端仍可正常初始化和调用。
	s.mcpHandler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
	})
	return nil
}

// handleMCP 每个 HTTP 请求都检查令牌，避免初始化后绕过过期检查。
// 来源网段限制仍由 Routes 外层的 restrictClientNetworks 统一执行。
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	internal := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && s.mcpToken != "" &&
		subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.mcpToken)) == 1
	if !internal {
		if !s.cfg.MCPEnabled {
			http.NotFound(w, r)
			return
		}
		payload, err := s.verifyAccessTokenBearer(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, err)
			return
		}
		if !payload.hasCapability(accessTokenCapabilityDocsRead) {
			writeError(w, errForbidden("MCP 需要 docs.read 能力"))
			return
		}
	}
	if s.mcpHandler == nil {
		writeError(w, errUnavailable("MCP 尚未初始化"))
		return
	}
	s.mcpHandler.ServeHTTP(w, r)
}

// registerMCPTool 统一文本结果和整个工具调用的超时；下游 Git/SQL 共用此上下文，不能按文件重新计时。
func registerMCPTool[In any](server *mcp.Server, name, description string, fn func(context.Context, In) (string, error)) {
	closed := false
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &closed, OpenWorldHint: &closed,
	}}, func(ctx context.Context, _ *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		text, err := fn(ctx, args)
		if ctx.Err() != nil && (err != nil || text == "") {
			err = fmt.Errorf("工具调用已取消或超过 10 秒: %w", ctx.Err())
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})
}
