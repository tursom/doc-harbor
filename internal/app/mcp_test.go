package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 所有 Git 写入仅构造 t.TempDir 中的 fixture，不触碰开发工作区或实际镜像。
func newMCPFixture(t *testing.T) (*Server, Repository, string) {
	t.Helper()
	requireGit(t)
	s := newWebhookTestServer(t)
	s.cfg.MCPEnabled = true
	if s.mcpHandler == nil {
		if err := s.initMCP(); err != nil {
			t.Fatal(err)
		}
	}
	source := createTestGitRepo(t)
	write := func(name, content string) {
		t.Helper()
		filename := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("doc/支付 接口:说明.md", "# 支付接口\n旧版说明\nmain-only\n")
	write("doc/binary.md", "Order.Create\x00binary\n")
	write("doc/large.md", strings.Repeat("Order.Create 大文件\n", 20_000))
	write("code/订单.go", "package order\n// before\nconst Route = \"/api/Order.Create\"\n// after\nconst Secret = \"MAIN_CODE\"\n")
	write("code/empty.txt", "")
	write("code/trailing.txt", "first  \n\n")
	write("code/single-wide.txt", strings.Repeat("长", 20_000)+"\nnext\n")
	write("code/binary.bin", "Order.Create\x00binary\n")
	write("code/large.txt", strings.Repeat("Order.Create 大文件\n", 20_000))
	write("code/late-binary.txt", strings.Repeat("a", 9000)+"\x00Order.Create\n")
	write("code/long.txt", strings.Repeat("中文字段", 500)+" Order.Create\n")
	var many strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&many, "第%d行 Order.Create\n", i)
	}
	write("code/many.txt", many.String())
	write("code/wide.txt", strings.Repeat(strings.Repeat("宽", 200)+"\n", 120))
	for i := 0; i < 302; i++ {
		write(fmt.Sprintf("crowded/dir%03d/file.txt", i), "leaf\n")
	}
	runTestGit(t, source, "add", ".")
	runTestGit(t, source, "commit", "-m", "main MCP fixture")
	mainSHA := strings.TrimSpace(runTestGitOutput(t, source, "rev-parse", "HEAD"))
	runTestGit(t, source, "checkout", "-b", "feature/docs")
	write("doc/支付 接口:说明.md", "# 支付接口新版\n前置规则\n回调 notify_url\n后置规则\n")
	write("doc/feature-only.md", "# Feature Only\n功能分支文档\n")
	write("code/订单.go", "package order\nconst Secret = \"FEATURE_CODE\"\n")
	runTestGit(t, source, "add", ".")
	// 明确晚于 main 的提交时间，避免同一秒提交导致智能最新的默认分支优先规则掩盖用例。
	cmd := exec.Command("git", "commit", "-m", "feature MCP fixture")
	cmd.Dir = source
	stamp := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture commit: %v: %s", err, output)
	}
	repo, err := createRepository(context.Background(), s.db, Repository{
		Name: "支付服务", Slug: "payments", RepoURL: source, DefaultBranch: "main", Enabled: true,
		TrackedBranches: []string{"*"}, LatestIncludeBranches: []string{"*"},
		MaxFileSizeBytes: 128 << 10, ScanPaths: []ScanPath{{Path: "doc", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.scanner.Scan(context.Background(), repo.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	disabled, err := createRepository(context.Background(), s.db, Repository{
		Name: "已关闭", Slug: "disabled", RepoURL: source, DefaultBranch: "main", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// createRepository 的默认值会启用新仓库；显式走停用接口覆盖该默认行为。
	if err := disableRepository(context.Background(), s.db, disabled.ID); err != nil {
		t.Fatal(err)
	}
	return s, repo, mainSHA
}

type mcpTestTransport struct {
	token string
	base  http.RoundTripper
}

func (tr mcpTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+tr.token)
	return tr.base.RoundTrip(r)
}

func connectMCPTestClient(t *testing.T, s *Server, token string) *mcp.ClientSession {
	t.Helper()
	host := httptest.NewServer(s.restrictClientNetworks(http.HandlerFunc(s.handleMCP)))
	t.Cleanup(host.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   host.URL + "/api/mcp",
		HTTPClient: &http.Client{Transport: mcpTestTransport{token: token, base: http.DefaultTransport}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if session.InitializeResult().Instructions != mcpInstructions {
		t.Fatalf("initialize instructions: %+v", session.InitializeResult())
	}
	return session
}

func callMCPTestTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, wantError bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s protocol error: %v", name, err)
	}
	if result.IsError != wantError {
		t.Fatalf("%s isError=%v, expected %v: %+v", name, result.IsError, wantError, result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("%s expected one text result: %+v", name, result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || strings.TrimSpace(text.Text) == "" {
		t.Fatalf("%s expected nonempty text: %+v", name, result)
	}
	return text.Text
}

func TestMCPP0Tools(t *testing.T) {
	s, repo, mainSHA := newMCPFixture(t)
	// 通过对外签发的 90 天 docs.read 令牌调用全部工具，覆盖实际外部客户端的接入方式。
	issued, err := s.issueAccessToken(accessTokenRequest{Capabilities: []string{"docs.read"}, TTLSeconds: 90 * 86400})
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCPTestClient(t, s, issued.Token)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 7 {
		t.Fatalf("tools: %+v, %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.OutputSchema != nil {
			t.Fatalf("read-only text tool contract: %+v", tool)
		}
	}
	cases := []struct {
		name, tool string
		args       map[string]any
		contains   []string
		absent     []string
	}{
		{"repos", "list_repos", map[string]any{}, []string{"找到 1 个启用仓库", "payments", "支付服务", "默认分支=main", "扫描目录=doc", "追踪分支数=2", "状态=success"}, []string{"disabled"}},
		{"branches", "list_branches", map[string]any{"repo": "支付服务"}, []string{"feature/docs", "默认", "参与智能最新", "功能分支候选"}, nil},
		{"branch-limit", "list_branches", map[string]any{"repo": "payments", "limit": 1}, []string{"feature/docs", "截断到 1 条"}, nil},
		{"docs", "list_docs", map[string]any{"repo": strconv.FormatInt(repo.ID, 10), "query": "支付 新版"}, []string{"共匹配 1 篇", "payments@feature/docs:doc/支付 接口:说明.md", "支付接口新版"}, []string{"#L"}},
		{"docs-and", "list_docs", map[string]any{"query": "支付 missing"}, []string{"共匹配 0 篇"}, nil},
		{"docs-case", "list_docs", map[string]any{"query": "FEATURE ONLY"}, []string{"feature-only.md"}, nil},
		{"docs-limit", "list_docs", map[string]any{"limit": 1}, []string{"截断到 1 条"}, nil},
		{"search-docs", "search_docs", map[string]any{"query": "notify_url"}, []string{"找到 1 处匹配，分布在 1 个文件", "payments@feature/docs:doc/支付 接口:说明.md#L2-4", "L2- 前置规则", "L3: 回调 notify_url", "L4- 后置规则"}, nil},
		{"docs-no-old", "search_docs", map[string]any{"query": "main-only"}, []string{"找到 0 处匹配"}, nil},
		{"docs-skip-files", "list_docs", map[string]any{}, []string{"共匹配 2 篇"}, []string{"binary.md", "large.md"}},
		{"search-docs-skip-files", "search_docs", map[string]any{"query": "Order.Create"}, []string{"找到 0 处匹配"}, nil},
		{"search-code-skip-files", "search_code", map[string]any{"query": "Order.Create", "path_glob": "code/*.txt", "limit": 300}, []string{"找到 300 处匹配"}, []string{"large.txt", "late-binary.txt", "binary.bin"}},
		{"search-code", "search_code", map[string]any{"query": "order.create", "path_glob": "**/*.go"}, []string{"payments@main:code/订单.go#L2-4", "L2- // before", "L3: const Route", "L4- // after"}, []string{"binary.bin", "large.txt"}},
		{"search-regex", "search_code", map[string]any{"query": "Order[.]Create", "regex": true, "path_glob": "code/*.go"}, []string{"找到 1 处匹配"}, nil},
		{"search-literal", "search_code", map[string]any{"query": "Order[.]Create", "path_glob": "**/*.go"}, []string{"找到 0 处匹配"}, nil},
		{"search-branch", "search_code", map[string]any{"query": "FEATURE_CODE", "ref": "feature/docs"}, []string{"payments@feature/docs:code/订单.go"}, nil},
		{"search-limit", "search_code", map[string]any{"query": "Order.Create", "limit": 2, "path_glob": "code/many.txt"}, []string{"找到 2 处匹配", "截断到 2 条"}, nil},
		{"search-exact-limit", "search_code", map[string]any{"query": "Order.Create", "limit": 1, "path_glob": "**/*.go"}, []string{"找到 1 处匹配"}, []string{"截断到"}},
		{"read-doc", "read", map[string]any{"repo": "payments", "path": "doc/支付 接口:说明.md"}, []string{"读取第 1-4 行，共 4 行", "== payments@feature/docs:doc/支付 接口:说明.md#L1-4", "回调 notify_url"}, nil},
		{"read-explicit", "read", map[string]any{"repo": "payments", "path": "doc/支付 接口:说明.md", "ref": "main", "start_line": 2, "end_line": 3}, []string{"#L2-3", "旧版说明", "main-only"}, []string{"新版"}},
		{"read-code", "read", map[string]any{"repo": "payments", "path": "code/订单.go"}, []string{"payments@main:code/订单.go", "MAIN_CODE"}, []string{"FEATURE_CODE"}},
		{"read-commit", "read", map[string]any{"repo": "payments", "path": "code/订单.go", "ref": mainSHA[:12]}, []string{"payments@" + mainSHA[:12] + ":code/订单.go", "MAIN_CODE"}, nil},
		{"read-lines", "read", map[string]any{"repo": "payments", "path": "code/many.txt"}, []string{"读取第 1-400 行，共 500 行", "start_line=401", "#L1-400"}, []string{"第401行"}},
		{"read-continue", "read", map[string]any{"repo": "payments", "path": "code/many.txt", "start_line": 401}, []string{"第 401-500 行", "#L401-500"}, []string{"继续"}},
		{"read-empty", "read", map[string]any{"repo": "payments", "path": "code/empty.txt"}, []string{"共 0 行", "payments@main:code/empty.txt"}, []string{"#L"}},
		{"read-trailing", "read", map[string]any{"repo": "payments", "path": "code/trailing.txt"}, []string{"共 2 行", "    1\tfirst  \n    2\t"}, nil},
		{"dir-root", "list_dir", map[string]any{"repo": "payments"}, []string{"dir\tcode\t-", "dir\tdoc\t-", "payments@main"}, nil},
		{"dir-filter", "list_dir", map[string]any{"repo": "payments", "path": "code"}, []string{"file\tcode/订单.go", "跳过"}, []string{"binary.bin", "large.txt", "late-binary.txt"}},
		{"dir-limit", "list_dir", map[string]any{"repo": "payments", "path": "crowded"}, []string{"列出 300 项", "截断到 300 条", "更深的 path"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := callMCPTestTool(t, session, tc.tool, tc.args, false)
			for _, want := range tc.contains {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q:\n%s", want, text)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(text, unwanted) {
					t.Fatalf("unexpected %q:\n%s", unwanted, text)
				}
			}
		})
	}
	t.Run("read-byte-limit", func(t *testing.T) {
		text := callMCPTestTool(t, session, "read", map[string]any{"repo": "payments", "path": "code/wide.txt"}, false)
		if len(text) > 40<<10 || !strings.Contains(text, "start_line=") || !strings.Contains(text, "还有") {
			t.Fatalf("40KB bound/continuation: %d: %.200s", len(text), text)
		}
	})
	t.Run("search-long-line", func(t *testing.T) {
		text := callMCPTestTool(t, session, "search_code", map[string]any{"query": "Order.Create", "path_glob": "code/long.txt"}, false)
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "L1:") && len([]rune(strings.TrimPrefix(line, "L1: "))) > 300 {
				t.Fatalf("search line exceeds 300 chars: %d", len([]rune(line)))
			}
		}
	})
	t.Run("read-single-wide-line", func(t *testing.T) {
		text := callMCPTestTool(t, session, "read", map[string]any{"repo": "payments", "path": "code/single-wide.txt"}, false)
		if len(text) > 40<<10 || !strings.Contains(text, "本行已截断") || !strings.Contains(text, "start_line=2") {
			t.Fatalf("single wide line bound/continuation: %d: %.200s", len(text), text)
		}
	})
	t.Run("ref-helper", func(t *testing.T) {
		target, err := s.resolveMCPRef(context.Background(), repo, "latest", "doc/支付 接口:说明.md")
		if err != nil || target.Ref != "feature/docs" || target.VersionID == 0 || target.Commit == mainSHA {
			t.Fatalf("citation target: %+v, %v", target, err)
		}
	})
	t.Run("errors", func(t *testing.T) {
		for _, tc := range []struct {
			tool string
			args map[string]any
		}{
			{"list_repos", map[string]any{"unexpected": true}},
			{"list_branches", map[string]any{"repo": "missing"}},
			{"list_branches", map[string]any{"repo": "payments", "limit": -1}},
			{"list_docs", map[string]any{"repo": "disabled"}},
			{"list_docs", map[string]any{"limit": 301}},
			{"search_docs", map[string]any{"query": " "}},
			{"search_code", map[string]any{"query": "Order", "repos": []string{"missing"}}},
			{"search_code", map[string]any{"query": "[", "regex": true}},
			{"search_code", map[string]any{"query": "[", "regex": true, "path_glob": "missing/**"}},
			{"search_code", map[string]any{"query": "Order", "path_glob": "code/["}},
			{"search_code", map[string]any{"query": "Order", "ref": "main~1"}},
			{"read", map[string]any{"repo": "payments", "path": "code/no-file.go"}},
			{"read", map[string]any{"repo": "payments", "path": "code/binary.bin"}},
			{"read", map[string]any{"repo": "payments", "path": "code/late-binary.txt"}},
			{"read", map[string]any{"repo": "payments", "path": "code/large.txt"}},
			{"read", map[string]any{"repo": "payments", "path": "code/订单.go", "start_line": 999}},
			{"read", map[string]any{"repo": "payments", "path": "code/订单.go", "start_line": 3, "end_line": 1}},
			{"list_dir", map[string]any{"repo": "payments", "path": "missing"}},
		} {
			callMCPTestTool(t, session, tc.tool, tc.args, true)
		}
		for _, bad := range []string{"../secret", "/etc/passwd", "code/../订单.go", ":(exclude)code", "code\\file"} {
			callMCPTestTool(t, session, "read", map[string]any{"repo": "payments", "path": bad}, true)
			callMCPTestTool(t, session, "list_dir", map[string]any{"repo": "payments", "path": bad}, true)
			callMCPTestTool(t, session, "search_code", map[string]any{"query": "Order", "path_glob": bad}, true)
		}
	})
	t.Run("git-output-limit", func(t *testing.T) {
		_, err := s.mcpGit(context.Background(), repo, 32, "show", mainSHA+":code/wide.txt")
		if err == nil || !strings.Contains(err.Error(), "结果过大") {
			t.Fatalf("Git output must be bounded: %v", err)
		}
	})
	t.Run("multi-repo-and-live-disable", func(t *testing.T) {
		other, err := createRepository(context.Background(), s.db, Repository{
			Name: "统计服务", Slug: "analytics", RepoURL: repo.RepoURL, DefaultBranch: "main", Enabled: true,
			ScanPaths: []ScanPath{{Path: "doc", Enabled: true}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.scanner.Scan(context.Background(), other.ID, "manual"); err != nil {
			t.Fatal(err)
		}
		for _, args := range []map[string]any{
			{"query": "MAIN_CODE"},
			{"query": "MAIN_CODE", "repos": []string{"支付服务", strconv.FormatInt(other.ID, 10)}},
		} {
			text := callMCPTestTool(t, session, "search_code", args, false)
			if !strings.Contains(text, "找到 2 处匹配，分布在 2 个文件") || !strings.Contains(text, "analytics@main:") || !strings.Contains(text, "payments@main:") {
				t.Fatalf("multi-repo results: %s", text)
			}
		}
		if err := disableRepository(context.Background(), s.db, repo.ID); err != nil {
			t.Fatal(err)
		}
		callMCPTestTool(t, session, "read", map[string]any{"repo": "payments", "path": "code/订单.go"}, true)
		text := callMCPTestTool(t, session, "list_repos", map[string]any{}, false)
		if strings.Contains(text, "payments") || !strings.Contains(text, "analytics") {
			t.Fatalf("disabled repository still exposed in initialized MCP session: %s", text)
		}
	})
}

func TestMCPGitCancellation(t *testing.T) {
	s := newWebhookTestServer(t)
	// 用可执行假 Git 验证 CommandContext 会终止耗时读取，不能只测到入口参数上的 deadline。
	filename := filepath.Join(t.TempDir(), "slow-git")
	if err := os.WriteFile(filename, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.cfg.GitBin = filename
	s.git = newGit(s.cfg)
	if err := os.MkdirAll(s.git.repoPath(1), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.mcpGit(ctx, Repository{ID: 1}, 4096, "show")
	if err != context.DeadlineExceeded || time.Since(start) > 2*time.Second {
		t.Fatalf("Git did not respect cancellation: %v, %s", err, time.Since(start))
	}
}

func TestMCPAuthenticationAndDocsTTL(t *testing.T) {
	s := newWebhookTestServer(t)
	s.cfg.MCPEnabled = true
	if err := s.initMCP(); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s.mcpToken)
	if err != nil || len(raw) != 32 {
		t.Fatalf("internal token entropy: %d, %v", len(raw), err)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.DataDir, "secrets", "access-token.key")); !os.IsNotExist(err) {
		t.Fatalf("initMCP should not persist token/key: %v", err)
	}
	for _, tc := range []struct {
		name string
		caps []string
		ttl  int
		ok   bool
	}{
		{"docs90d", []string{"docs.read"}, 90 * 86400, true},
		{"docs-dedup", []string{"docs.read", "docs.read"}, 30 * 86400, true},
		{"docs-too-long", []string{"docs.read"}, 90*86400 + 1, false},
		{"mixed24h", []string{"docs.read", accessTokenCapabilityAIHistoryRead}, 86400, true},
		{"mixed-too-long", []string{"docs.read", accessTokenCapabilityAIHistoryRead}, 86401, false},
		{"history-too-long", []string{accessTokenCapabilityAIHistoryRead}, 86401, false},
		{"short", []string{"docs.read"}, 299, false},
		{"overflow", []string{"docs.read"}, int(^uint(0) >> 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.issueAccessToken(accessTokenRequest{Capabilities: tc.caps, TTLSeconds: tc.ttl})
			if (err == nil) != tc.ok {
				t.Fatalf("TTL result: %v", err)
			}
		})
	}
	docs, err := s.issueAccessToken(accessTokenRequest{Capabilities: []string{"docs.read"}, TTLSeconds: 90 * 86400})
	if err != nil {
		t.Fatal(err)
	}
	history, err := s.issueAccessToken(accessTokenRequest{Capabilities: []string{accessTokenCapabilityAIHistoryRead}})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := s.signAccessToken(accessTokenPayload{ExpiresAt: time.Now().Add(-time.Minute).Unix(), Capabilities: []string{"docs.read"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token string
		enabled     bool
		want        int
	}{
		{"missing", "", true, 401}, {"invalid", "invalid", true, 401}, {"expired", expired, true, 401},
		{"missing-capability", history.Token, true, 403}, {"external", docs.Token, true, 200},
		{"internal", s.mcpToken, true, 200}, {"internal-disabled", s.mcpToken, false, 200},
		{"external-disabled", docs.Token, false, 404}, {"missing-disabled", "", false, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.cfg.MCPEnabled = tc.enabled
			r := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			s.handleMCP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	s.cfg.MCPEnabled = false
	session := connectMCPTestClient(t, s, s.mcpToken)
	callMCPTestTool(t, session, "list_repos", map[string]any{}, false)
	s.allowedClientPrefixes, err = parseAllowedClientCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/mcp", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+s.mcpToken)
	w := httptest.NewRecorder()
	s.restrictClientNetworks(http.HandlerFunc(s.handleMCP)).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("internal token bypassed CIDR: %d", w.Code)
	}
}
