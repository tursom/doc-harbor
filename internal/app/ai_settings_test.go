package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAIProviderTestDoesNotPersistSecret(t *testing.T) {
	server := newWebhookTestServer(t)
	modelServer := newAIModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)

	before := countAISecrets(t, server)
	recorder := doJSON(t, server, http.MethodPost, "/api/ai/providers/test", map[string]any{
		"name":     "DeepSeek",
		"base_url": modelServer.URL,
		"model":    "test-model",
		"api_key":  "sk-test-provider-test-1234",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"status":"pass"`) {
		t.Fatalf("expected pass response: %s", recorder.Body.String())
	}
	if after := countAISecrets(t, server); after != before {
		t.Fatalf("ai_secrets count = %d, want unchanged %d", after, before)
	}
}

func TestAIProviderTestAcceptsEmptyChoiceContent(t *testing.T) {
	server := newWebhookTestServer(t)
	modelServer := newAIModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)

	recorder := doJSON(t, server, http.MethodPost, "/api/ai/providers/test", map[string]any{
		"name":     "DeepSeek",
		"base_url": modelServer.URL,
		"model":    "deepseek-v4-pro",
		"api_key":  "sk-test-empty-content-1234",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"status":"pass"`) {
		t.Fatalf("expected pass response for empty test content: %s", body)
	}
	if strings.Contains(body, "sk-test-empty-content") {
		t.Fatalf("test response leaked API key: %s", body)
	}
}

func TestAIDefaultProviderSaveAndEnable(t *testing.T) {
	server := newWebhookTestServer(t)
	modelServer := newAIModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)

	recorder := doJSON(t, server, http.MethodPut, "/api/ai/settings/default-provider", map[string]any{
		"name":            "DeepSeek",
		"base_url":        modelServer.URL,
		"model":           "deepseek-v4-flash",
		"api_key":         "sk-test-enable-5678",
		"enable":          true,
		"timeout_seconds": 5,
		"max_rpm":         60,
		"priority":        10,
		"cost_tier":       "medium",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"status":"enabled"`) {
		t.Fatalf("expected enabled settings: %s", body)
	}
	if !strings.Contains(body, `"route_provider_keys":["deepseek-main"]`) ||
		!strings.Contains(body, `"active_route_provider_keys":["deepseek-main"]`) ||
		!strings.Contains(body, `"has_unapplied_changes":false`) ||
		!strings.Contains(body, `"route_order":1`) {
		t.Fatalf("settings response missing route summary fields: %s", body)
	}
	if strings.Contains(body, "api_key_secret_id") || strings.Contains(body, "sk-test-enable") {
		t.Fatalf("settings response leaked secret internals: %s", body)
	}
	if count := countAISecrets(t, server); count != 1 {
		t.Fatalf("ai_secrets count = %d, want 1", count)
	}
	active, err := ensureActiveAIConfig(context.Background(), server.db)
	if err != nil {
		t.Fatalf("active config: %v", err)
	}
	if !active.Config.Enabled {
		t.Fatal("active config should be enabled")
	}

	settings := doJSON(t, server, http.MethodGet, "/api/ai/settings", nil)
	if settings.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, want %d; body=%s", settings.Code, http.StatusOK, settings.Body.String())
	}
	if strings.Contains(settings.Body.String(), "api_key_secret_id") || strings.Contains(settings.Body.String(), "sk-test-enable") {
		t.Fatalf("GET settings leaked secret internals: %s", settings.Body.String())
	}
}

func TestAIDefaultProviderEnableFailureDoesNotWriteSecretOrActive(t *testing.T) {
	server := newWebhookTestServer(t)
	modelServer := newAIModelTestServer(t, http.StatusUnauthorized, `{"error":"unauthorized sk-test-failure-0000"}`)
	activeBefore, err := ensureActiveAIConfig(context.Background(), server.db)
	if err != nil {
		t.Fatalf("active config before: %v", err)
	}
	secretsBefore := countAISecrets(t, server)

	recorder := doJSON(t, server, http.MethodPut, "/api/ai/settings/default-provider", map[string]any{
		"name":            "DeepSeek",
		"base_url":        modelServer.URL,
		"model":           "deepseek-v4-flash",
		"api_key":         "sk-test-failure-0000",
		"enable":          true,
		"timeout_seconds": 5,
		"max_rpm":         60,
		"priority":        10,
		"cost_tier":       "medium",
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"code":"provider_test_failed"`) {
		t.Fatalf("expected provider_test_failed: %s", body)
	}
	if strings.Contains(body, "sk-test-failure") {
		t.Fatalf("failure response leaked API key: %s", body)
	}
	if after := countAISecrets(t, server); after != secretsBefore {
		t.Fatalf("ai_secrets count = %d, want unchanged %d", after, secretsBefore)
	}
	activeAfter, err := ensureActiveAIConfig(context.Background(), server.db)
	if err != nil {
		t.Fatalf("active config after: %v", err)
	}
	if activeAfter.Version != activeBefore.Version {
		t.Fatalf("active version = %d, want unchanged %d", activeAfter.Version, activeBefore.Version)
	}
}

func TestAISettingsIgnoresFailedConfigOlderThanActive(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	active, err := ensureActiveAIConfig(ctx, server.db)
	if err != nil {
		t.Fatalf("active config: %v", err)
	}
	failedCfg := normalizeAIConfig(active.Config)
	failedCfg.Enabled = true
	raw, hash, err := aiConfigJSONAndHash(failedCfg)
	if err != nil {
		t.Fatalf("config json: %v", err)
	}
	if _, err := server.db.ExecContext(ctx, `INSERT INTO ai_config_versions
		(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
		 created_at, updated_at, error_message)
		VALUES (?, 'failed', ?, ?, '[]', 'fail', ?, ?, ?, ?)`,
		active.Version+1, hash, raw, `{"ok":false,"errors":["old failed config"]}`, nowString(), nowString(), "old failed config"); err != nil {
		t.Fatalf("insert failed config: %v", err)
	}
	if err := publishAIEnabled(ctx, server.db, false, "test"); err != nil {
		t.Fatalf("publish newer active: %v", err)
	}

	recorder := doJSON(t, server, http.MethodGet, "/api/ai/settings", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, `"editable_status":"failed"`) ||
		strings.Contains(body, `"has_unapplied_changes":true`) ||
		strings.Contains(body, `"status":"error"`) {
		t.Fatalf("settings should ignore stale failed config: %s", body)
	}
}

func TestAIProviderCRUDApplyRebuildsRouteByPriority(t *testing.T) {
	server := newWebhookTestServer(t)
	deepseekServer := newAIModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	openAIServer := newAIModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)

	enable := doJSON(t, server, http.MethodPut, "/api/ai/settings/default-provider", map[string]any{
		"name":            "DeepSeek",
		"base_url":        deepseekServer.URL,
		"model":           "deepseek-v4-flash",
		"api_key":         "sk-test-deepseek-1111",
		"enable":          true,
		"timeout_seconds": 5,
		"max_rpm":         60,
		"priority":        10,
		"cost_tier":       "medium",
	})
	if enable.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want %d; body=%s", enable.Code, http.StatusOK, enable.Body.String())
	}

	create := doJSON(t, server, http.MethodPost, "/api/ai/providers", map[string]any{
		"name":            "OpenAI",
		"base_url":        openAIServer.URL,
		"model":           "gpt-4.1-mini",
		"api_key":         "sk-test-openai-2222",
		"timeout_seconds": 5,
		"max_rpm":         60,
		"priority":        20,
		"cost_tier":       "medium",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", create.Code, http.StatusCreated, create.Body.String())
	}
	createBody := create.Body.String()
	if !strings.Contains(createBody, `"has_unapplied_changes":true`) ||
		!strings.Contains(createBody, `"route_provider_keys":["deepseek-main","openai-main"]`) ||
		!strings.Contains(createBody, `"active_route_provider_keys":["deepseek-main"]`) {
		t.Fatalf("create response missing editable/active route split: %s", createBody)
	}
	if strings.Contains(createBody, "api_key_secret_id") || strings.Contains(createBody, "sk-test-openai") {
		t.Fatalf("create response leaked secret internals: %s", createBody)
	}

	makeDefault := doJSON(t, server, http.MethodPatch, "/api/ai/providers/openai-main", map[string]any{
		"make_default": true,
	})
	if makeDefault.Code != http.StatusOK {
		t.Fatalf("make default status = %d, want %d; body=%s", makeDefault.Code, http.StatusOK, makeDefault.Body.String())
	}
	if !strings.Contains(makeDefault.Body.String(), `"route_provider_keys":["openai-main","deepseek-main"]`) {
		t.Fatalf("make default did not rebuild editable route: %s", makeDefault.Body.String())
	}

	apply := doJSON(t, server, http.MethodPost, "/api/ai/settings/apply", map[string]any{
		"enabled":     true,
		"test_policy": "all_routable",
	})
	if apply.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want %d; body=%s", apply.Code, http.StatusOK, apply.Body.String())
	}
	body := apply.Body.String()
	if !strings.Contains(body, `"has_unapplied_changes":false`) ||
		!strings.Contains(body, `"active_route_provider_keys":["openai-main","deepseek-main"]`) ||
		!strings.Contains(body, `"default_provider_key":"openai-main"`) {
		t.Fatalf("apply response missing active route update: %s", body)
	}
}

func TestAIRoutedModelFailoverRecordsAttemptOrder(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	failingProvider := newAIAnswerModelTestServer(t, http.StatusTooManyRequests, `{"error":"rate limit"}`)
	successProvider := newAIAnswerModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"second provider answer"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	firstSecret, err := server.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: "deepseek-main-api-key", SecretType: "api_key", Value: "sk-test-first-3333"}, "test")
	if err != nil {
		t.Fatalf("create first secret: %v", err)
	}
	secondSecret, err := server.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: "openai-main-api-key", SecretType: "api_key", Value: "sk-test-second-4444"}, "test")
	if err != nil {
		t.Fatalf("create second secret: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = []AIProvider{
		{
			ProviderKey:           "deepseek-main",
			Name:                  "DeepSeek",
			Priority:              10,
			ProviderType:          "openai_compatible",
			BaseURL:               failingProvider.URL,
			Model:                 "deepseek-v4-flash",
			APIKeySecretID:        firstSecret.ID,
			CostTier:              "medium",
			RequestTimeoutSeconds: 5,
			MaxRPM:                60,
		},
		{
			ProviderKey:           "openai-main",
			Name:                  "OpenAI",
			Priority:              20,
			ProviderType:          "openai_compatible",
			BaseURL:               successProvider.URL,
			Model:                 "gpt-4.1-mini",
			APIKeySecretID:        secondSecret.ID,
			CostTier:              "medium",
			RequestTimeoutSeconds: 5,
			MaxRPM:                60,
		},
	}
	cfg.Chat.Routing = buildDefaultRouting(ctx, server.db, cfg.Chat.Providers)

	result, err := server.callRoutedAIModel(ctx, cfg, "哪些服务处理订单？", aiRetrievalResult{})
	if err != nil {
		t.Fatalf("call routed model: %v", err)
	}
	if result.Content != "second provider answer" || result.ProviderName != "OpenAI" || result.Model != "gpt-4.1-mini" {
		t.Fatalf("unexpected routed result: %+v", result)
	}
	var failover struct {
		AttemptOrder         []string         `json:"attempt_order"`
		SucceededProviderKey string           `json:"succeeded_provider_key"`
		Failures             []map[string]any `json:"failures"`
	}
	if err := json.Unmarshal([]byte(result.FailoverJSON), &failover); err != nil {
		t.Fatalf("decode failover json: %v; raw=%s", err, result.FailoverJSON)
	}
	if strings.Join(failover.AttemptOrder, ",") != "deepseek-main,openai-main" {
		t.Fatalf("attempt order = %#v, want deepseek-main/openai-main", failover.AttemptOrder)
	}
	if failover.SucceededProviderKey != "openai-main" {
		t.Fatalf("succeeded provider = %q, want openai-main", failover.SucceededProviderKey)
	}
	if len(failover.Failures) != 1 || failover.Failures[0]["provider_key"] != "deepseek-main" {
		t.Fatalf("failures = %#v, want one deepseek-main failure", failover.Failures)
	}
	if strings.Contains(result.FailoverJSON, "sk-test") {
		t.Fatalf("failover json leaked secret: %s", result.FailoverJSON)
	}
}

func TestAIMessagesResponseIncludesPersistedEvidencePanelData(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	repo, err := createRepository(ctx, server.db, Repository{
		Name:          "go-gateway",
		Slug:          "go-gateway",
		RepoURL:       "https://example.test/go-gateway.git",
		DefaultBranch: "main",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	session, err := createAISession(ctx, server.db, "接口接入", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	message, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: session.ID, Role: "assistant", Content: "回答"})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	run, err := createAIRun(ctx, server.db, session.ID, message.ID, AIConfigVersion{
		Version:    1,
		Config:     defaultAIConfig(),
		ConfigHash: "test-config",
	}, AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := insertAIServiceCandidate(ctx, server.db, AIServiceCandidate{
		RunID:         run.ID,
		MessageID:     message.ID,
		RepoID:        repo.ID,
		ServiceName:   repo.Name,
		MatchedTerms:  []string{"order"},
		Confidence:    "high",
		Reason:        "命中关键词：order",
		Score:         100,
		EvidenceCount: 1,
	}); err != nil {
		t.Fatalf("insert candidate: %v", err)
	}
	if _, err := insertAICitation(ctx, server.db, AIMessageCitation{
		MessageID:   message.ID,
		RepoID:      repo.ID,
		VersionID:   42,
		SourceScope: "smart_latest",
		Branch:      "main",
		CommitSHA:   "abcdef",
		FilePath:    "internal/api/order.go",
		LineStart:   12,
		LineEnd:     18,
		QuoteText:   "func Order",
		Score:       100,
	}); err != nil {
		t.Fatalf("insert citation: %v", err)
	}

	recorder := doJSON(t, server, http.MethodGet, "/api/ai/sessions/"+strconv.FormatInt(session.ID, 10)+"/messages", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"service_candidates":[`) || !strings.Contains(body, `"citations":[`) {
		t.Fatalf("response missing evidence panel arrays: %s", body)
	}
	if !strings.Contains(body, `"repo_name":"go-gateway"`) || !strings.Contains(body, `"file_path":"internal/api/order.go"`) {
		t.Fatalf("response missing persisted evidence data: %s", body)
	}
}

func newAIModelTestServer(t *testing.T, status int, responseBody string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode model request: %v", err)
		}
		if payload["max_tokens"] != float64(64) {
			t.Fatalf("max_tokens = %v, want 64", payload["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server
}

func newAIAnswerModelTestServer(t *testing.T, status int, responseBody string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode model request: %v", err)
		}
		if payload["model"] == "" {
			t.Fatalf("model is required in routed model request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server
}

func doJSON(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	return recorder
}

func countAISecrets(t *testing.T, server *Server) int {
	t.Helper()
	var count int
	if err := server.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ai_secrets`).Scan(&count); err != nil {
		t.Fatalf("count ai secrets: %v", err)
	}
	return count
}
