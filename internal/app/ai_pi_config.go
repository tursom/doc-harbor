package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"doc-harbor/internal/pirpc"
)

// piSystemPrompt 与实施规格保持一致；变更此内容时应重新归档评估基线。
const piSystemPrompt = `你是 DocHarbor 问答助手，帮助公司的开发、产品和测试人员理解各服务的文档和代码。
你只能通过 docharbor 工具只读访问 Git 仓库，不能修改任何内容，也看不到数据库、日志和线上环境。

工作方式
- 先定位再回答：用 list_docs / search_docs 找设计和接口文档，用 search_code 找实现，用 read 读到能支撑结论的原文后再回答。不要只凭搜索片段下结论。
- 文档和代码不一致时，以默认分支代码为准，并指出文档哪里过时。
- 默认口径是智能最新。来自功能分支的内容要写明分支名，并说明尚未合入默认分支。
- 测试代码、mock、fixtures 不是业务事实，除非用户问的就是测试。
- 找不到时，说明搜了哪些关键词和范围，给出下一步建议，例如可能相关的服务或文档。不要编造接口、字段、表名或错误码。
- 问题有歧义时，先按上下文做最合理的假设并写明假设；只有无法继续时才反问。

按提问人调整回答（看用户消息开头 <context> 里的 role；没有时根据问题判断）
- 开发：给出接口路径和方法、鉴权、请求参数、返回字段、错误码，字段多时用表格，必要时给请求示例。
- 测试：从现象出发，列出可能原因、对应代码位置、如何判断、如何复现；涉及错误码时说明在哪里抛出。
- 产品：用业务语言讲流程、规则、状态和边界条件，少贴代码；流程用编号步骤，状态用表格。

引用
- 每个关键结论后标注来源，格式与工具结果中的引用键一致，用方括号括起来，例如 [游戏服务后端@main:core/handle/order.go#L40-88]。
- 只引用你实际通过 read 或搜索结果看到过的行。
- 不要在结尾另列参考资料。

格式
- 用中文回答，先给结论再展开细节。不要用“根据现有证据”之类的套话开头。
- 代码、路径、字段名用行内代码。
`

// piRuntimeSpec 为每个可用供应商生成自定义 provider，密钥只进入 Env。
// 生成与版本切换在同一把锁内执行，排队请求总是使用最新 Spec，防止旧配置覆盖新配置。
func (s *Server) piRuntimeSpec(ctx context.Context) (pirpc.Spec, AIProvider, error) {
	s.pi.configMu.Lock()
	defer s.pi.configMu.Unlock()
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return pirpc.Spec{}, AIProvider{}, err
	}
	if !active.Config.Enabled {
		return pirpc.Spec{}, AIProvider{}, errUnavailable("AI 问答未启用，请到 AI 供应商页启用")
	}
	provider, ok := pickDefaultAIProvider(active.Config)
	if !ok || !aiProviderRoutableWithSecret(ctx, s.db, provider) {
		return pirpc.Spec{}, AIProvider{}, errUnavailable("默认 AI 供应商不可用，请到 AI 供应商页配置")
	}
	root, err := filepath.Abs(filepath.Join(s.cfg.DataDir, "ai"))
	if err != nil {
		return pirpc.Spec{}, AIProvider{}, err
	}
	agentDir := filepath.Join(root, "pi-agent")
	sessionsDir := filepath.Join(root, "pi-sessions")
	cwd := filepath.Join(root, "pi-cwd")
	for _, dir := range []string{agentDir, sessionsDir, cwd} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return pirpc.Spec{}, AIProvider{}, err
		}
	}
	providers := append([]AIProvider(nil), active.Config.Chat.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderKey < providers[j].ProviderKey })
	models := map[string]any{}
	envValues := map[string]string{}
	for _, item := range providers {
		if !aiProviderRoutableWithSecret(ctx, s.db, item) {
			continue
		}
		secret, err := s.decryptAISecret(ctx, item.APIKeySecretID)
		if err != nil {
			return pirpc.Spec{}, AIProvider{}, errUnavailable("无法读取 AI 供应商密钥")
		}
		keyName := fmt.Sprintf("DH_PROVIDER_KEY_%d", len(models))
		envValues[keyName] = secret
		models["dh-"+item.ProviderKey] = map[string]any{
			"baseUrl": strings.TrimSuffix(strings.TrimRight(item.BaseURL, "/"), "/chat/completions"),
			"api":     "openai-completions", "apiKey": "$" + keyName,
			"models": []any{map[string]any{"id": item.Model}},
		}
	}
	_, port, err := net.SplitHostPort(s.cfg.HTTPAddr)
	if err != nil || port == "" || port == "0" {
		return pirpc.Spec{}, AIProvider{}, errUnavailable("pi 需要固定的 HTTP_ADDR 端口访问本机 MCP")
	}
	mcp := map[string]any{"mcpServers": map[string]any{"docharbor": map[string]any{
		"url": "http://127.0.0.1:" + port + "/api/mcp", "headers": map[string]string{"Authorization": "Bearer ${DH_MCP_TOKEN}"},
		"exposure": "direct", "description": "DocHarbor：公司 Git 仓库文档和代码的只读访问",
	}}, "autoEnableCodemode": false}
	settings := map[string]any{
		"extensions":             []string{"-builtin:llama.cpp", "-builtin:codemode", "-builtin:tool-search"},
		"enableInstallTelemetry": false, "enableAnalytics": false, "cacheWarming": "off", "defaultProjectTrust": "never",
	}
	files := map[string]any{"models.json": map[string]any{"providers": models}, "mcp.json": mcp, "settings.json": settings}
	version := fmt.Sprintf("%d:%s", active.Version, active.ConfigHash)
	// 密钥原地更新也须回收进程；仅用摘要区分代际，不把密钥写入配置。
	keyHash := sha256.Sum256([]byte(encodeJSON(envValues)))
	version += ":" + hex.EncodeToString(keyHash[:])
	// 每代进程读取不可变快照。发布新版时，仍在启动/切会话的旧进程不会
	// 把旧环境变量与新版 provider 索引混用；根目录保留最新配置便于检查。
	snapshotDir := filepath.Join(agentDir, fmt.Sprintf("config-%d-%s", active.Version, hex.EncodeToString(keyHash[:8])))
	if s.pi.configVersion != version {
		if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
			return pirpc.Spec{}, AIProvider{}, err
		}
		for name, value := range files {
			raw, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				return pirpc.Spec{}, AIProvider{}, err
			}
			if err := writePiConfigFile(filepath.Join(agentDir, name), raw); err != nil {
				return pirpc.Spec{}, AIProvider{}, err
			}
			if err := writePiConfigFile(filepath.Join(snapshotDir, name), raw); err != nil {
				return pirpc.Spec{}, AIProvider{}, err
			}
		}
		if err := writePiConfigFile(filepath.Join(agentDir, "system-prompt.md"), []byte(piSystemPrompt)); err != nil {
			return pirpc.Spec{}, AIProvider{}, err
		}
		if err := writePiConfigFile(filepath.Join(snapshotDir, "system-prompt.md"), []byte(piSystemPrompt)); err != nil {
			return pirpc.Spec{}, AIProvider{}, err
		}
	}
	envValues["PATH"] = os.Getenv("PATH")
	envValues["HOME"] = root
	envValues["PI_CODING_AGENT_DIR"] = snapshotDir
	envValues["PI_OFFLINE"] = "1"
	envValues["PI_SKIP_VERSION_CHECK"] = "1"
	envValues["DH_MCP_TOKEN"] = s.mcpToken
	noProxy := "127.0.0.1,localhost"
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			noProxy += "," + value
		}
	}
	envValues["NO_PROXY"], envValues["no_proxy"] = noProxy, noProxy
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if value := os.Getenv(key); value != "" {
			envValues[key] = value
		}
	}
	envKeys := make([]string, 0, len(envValues))
	for key := range envValues {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	childEnv := make([]string, 0, len(envKeys))
	for _, key := range envKeys {
		childEnv = append(childEnv, key+"="+envValues[key])
	}
	spec := pirpc.Spec{Bin: s.cfg.PIBin, Dir: cwd, Env: childEnv, Version: version, Args: []string{
		"--mode", "rpc", "--no-tools", "--no-context-files", "--no-skills", "--no-prompt-templates", "--no-themes",
		"--session-dir", sessionsDir, "--system-prompt", filepath.Join(snapshotDir, "system-prompt.md"),
		"--provider", "dh-" + provider.ProviderKey, "--model", provider.Model,
	}}
	if err := s.pi.pool.UpdateSpec(spec); err != nil {
		return pirpc.Spec{}, AIProvider{}, err
	}
	s.pi.configVersion = version
	return spec, provider, nil
}

// 同目录 rename 使 pi 只能读到完整配置，文件始终使用仅服务账户可读的权限。
func writePiConfigFile(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pi-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Server) piUserMessage(ctx context.Context, role string, scope AIQuestionScope, question string) string {
	label := "全部仓库"
	current := ""
	if scope.CurrentFile != nil {
		label = "当前文件"
		file := scope.CurrentFile
		if repo, err := getRepository(ctx, s.db, file.RepoID); err == nil {
			ref := file.Branch
			if ref == "" {
				ref = file.CommitSHA
			}
			if ref == "" {
				ref = "latest"
			}
			current = "\ncurrent_file: " + repo.Slug + "@" + ref + ":" + file.FilePath
		}
	} else if len(scope.RepoIDs) > 0 && scope.RepoMode != "global" {
		names := []string{}
		for _, id := range scope.RepoIDs {
			if repo, err := getRepository(ctx, s.db, id); err == nil {
				names = append(names, repo.Name)
			}
		}
		if len(names) > 0 {
			label = "当前仓库 " + strings.Join(names, "、")
		}
	}
	return "<context>\nrole: " + role + "\nscope: " + label + current + "\n</context>\n\n" + question
}
