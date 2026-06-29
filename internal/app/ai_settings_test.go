package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
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

	result, err := server.callRoutedAIModel(ctx, cfg, "哪些模块处理请求？", aiRetrievalResult{})
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

func TestAccessTokenDefaultTTLAndRemoteRead(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	repo, err := createRepository(ctx, server.db, Repository{
		Name:          "doc-harbor",
		Slug:          "doc-harbor",
		RepoURL:       "https://example.test/doc-harbor.git",
		DefaultBranch: "main",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	session, err := createAISession(ctx, server.db, "远程历史", "alice@example.com", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: session.ID, Role: "user", Content: "问题"}); err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	assistant, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: session.ID, Role: "assistant", Content: "回答"})
	if err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}
	run, err := createAIRun(ctx, server.db, session.ID, assistant.ID, AIConfigVersion{
		Version:    1,
		Config:     defaultAIConfig(),
		ConfigHash: "test-config",
	}, AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := insertAIServiceCandidate(ctx, server.db, AIServiceCandidate{
		RunID:         run.ID,
		MessageID:     assistant.ID,
		RepoID:        repo.ID,
		ServiceName:   "doc-harbor",
		MatchedTerms:  []string{"history"},
		Confidence:    "high",
		Score:         9,
		EvidenceCount: 1,
	}); err != nil {
		t.Fatalf("insert candidate: %v", err)
	}
	if _, err := insertAICitation(ctx, server.db, AIMessageCitation{
		MessageID:   assistant.ID,
		RepoID:      repo.ID,
		VersionID:   1,
		SourceScope: "smart_latest",
		Branch:      "main",
		CommitSHA:   "abcdef",
		FilePath:    "README.md",
		LineStart:   1,
		LineEnd:     2,
		QuoteText:   "DocHarbor",
		Score:       9,
	}); err != nil {
		t.Fatalf("insert citation: %v", err)
	}

	recorder := doJSON(t, server, http.MethodPost, "/api/tokens", map[string]any{
		"capabilities": []string{accessTokenCapabilityAIHistoryRead},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var issued accessTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("token should not be empty")
	}
	expiresAt, err := time.Parse(timeLayout, issued.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	if d := time.Until(expiresAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Fatalf("default ttl duration = %s, want about 1h", d)
	}

	list := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=all", issued.Token, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", list.Code, http.StatusOK, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"title":"远程历史"`) {
		t.Fatalf("list response missing session: %s", list.Body.String())
	}
	oldListPath := doAuthorizedJSON(t, server, http.MethodGet, "/api/ai/history/sessions?archived=all", issued.Token, nil)
	if oldListPath.Code != http.StatusNotFound {
		t.Fatalf("old history path status = %d, want %d; body=%s", oldListPath.Code, http.StatusNotFound, oldListPath.Body.String())
	}
	detail := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions/"+strconv.FormatInt(session.ID, 10), issued.Token, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body=%s", detail.Code, http.StatusOK, detail.Body.String())
	}
	body := detail.Body.String()
	if !strings.Contains(body, `"messages"`) || !strings.Contains(body, `"service_candidates"`) || !strings.Contains(body, `"citations"`) || strings.Contains(body, "api_key") {
		t.Fatalf("detail response missing history or leaked secret fields: %s", body)
	}
}

func TestAccessTokenRejectsInvalidTTLAndBearer(t *testing.T) {
	server := newWebhookTestServer(t)
	for _, ttl := range []int{299, 86401} {
		recorder := doJSON(t, server, http.MethodPost, "/api/tokens", map[string]any{
			"ttl_seconds":  ttl,
			"capabilities": []string{accessTokenCapabilityAIHistoryRead},
		})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("ttl %d status = %d, want %d; body=%s", ttl, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}
	for _, payload := range []map[string]any{
		{"capabilities": []string{}},
		{"capabilities": []string{"repo.read"}},
	} {
		recorder := doJSON(t, server, http.MethodPost, "/api/tokens", payload)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid capabilities status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}
	missing := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions", "", nil)
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want %d; body=%s", missing.Code, http.StatusUnauthorized, missing.Body.String())
	}
	issued := issueAccessTokenForTest(t, server, accessTokenScope{}, time.Hour)
	tampered := issued[:len(issued)-1] + "x"
	invalid := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions", tampered, nil)
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token status = %d, want %d; body=%s", invalid.Code, http.StatusUnauthorized, invalid.Body.String())
	}
	expiredPayload := accessTokenPayload{
		IssuedAt:     time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
		Capabilities: []string{accessTokenCapabilityAIHistoryRead},
		Scope:        accessTokenScope{},
		JTI:          "expired",
	}
	expiredToken, err := server.signAccessToken(expiredPayload)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	expired := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions", expiredToken, nil)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d, want %d; body=%s", expired.Code, http.StatusUnauthorized, expired.Body.String())
	}
	noCapabilityPayload := accessTokenPayload{
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
		Scope:     accessTokenScope{},
		JTI:       "no-capability",
	}
	noCapabilityToken, err := server.signAccessToken(noCapabilityPayload)
	if err != nil {
		t.Fatalf("sign no capability token: %v", err)
	}
	forbidden := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions", noCapabilityToken, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("missing capability status = %d, want %d; body=%s", forbidden.Code, http.StatusForbidden, forbidden.Body.String())
	}
}

func TestAccessTokenViewerScopeAndPaginationFilters(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	alice, err := createAISession(ctx, server.db, "alpha history", "alice", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create alice session: %v", err)
	}
	bob, err := createAISession(ctx, server.db, "beta history", "bob", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create bob session: %v", err)
	}
	older := time.Now().UTC().Add(-2 * time.Hour).Format(timeLayout)
	newer := time.Now().UTC().Add(-time.Hour).Format(timeLayout)
	if _, err := server.db.ExecContext(ctx, `UPDATE ai_sessions SET updated_at = ? WHERE id = ?`, newer, alice.ID); err != nil {
		t.Fatalf("update alice: %v", err)
	}
	if _, err := server.db.ExecContext(ctx, `UPDATE ai_sessions SET updated_at = ?, archived_at = ? WHERE id = ?`, older, older, bob.ID); err != nil {
		t.Fatalf("update bob: %v", err)
	}

	viewerToken := issueAccessTokenForTest(t, server, accessTokenScope{ViewerKey: "alice"}, time.Hour)
	scopedList := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=all", viewerToken, nil)
	if scopedList.Code != http.StatusOK {
		t.Fatalf("scoped list status = %d, want %d; body=%s", scopedList.Code, http.StatusOK, scopedList.Body.String())
	}
	if !strings.Contains(scopedList.Body.String(), `"viewer_key":"alice"`) || strings.Contains(scopedList.Body.String(), `"viewer_key":"bob"`) {
		t.Fatalf("viewer scope not enforced: %s", scopedList.Body.String())
	}
	bobDetail := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions/"+strconv.FormatInt(bob.ID, 10), viewerToken, nil)
	if bobDetail.Code != http.StatusNotFound {
		t.Fatalf("scoped detail status = %d, want %d; body=%s", bobDetail.Code, http.StatusNotFound, bobDetail.Body.String())
	}

	fullToken := issueAccessTokenForTest(t, server, accessTokenScope{}, time.Hour)
	page1 := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=all&limit=1", fullToken, nil)
	if page1.Code != http.StatusOK {
		t.Fatalf("page1 status = %d, want %d; body=%s", page1.Code, http.StatusOK, page1.Body.String())
	}
	var firstPage aiHistorySessionsResponse
	if err := json.Unmarshal(page1.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("page1 = %+v, want one item and next cursor", firstPage)
	}
	page2 := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=all&limit=1&cursor="+firstPage.NextCursor, fullToken, nil)
	if page2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d, want %d; body=%s", page2.Code, http.StatusOK, page2.Body.String())
	}
	var secondPage aiHistorySessionsResponse
	if err := json.Unmarshal(page2.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("page2 = %+v, should contain next session", secondPage)
	}
	filtered := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=all&q=alpha&updated_after="+url.QueryEscape(older), fullToken, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, want %d; body=%s", filtered.Code, http.StatusOK, filtered.Body.String())
	}
	if !strings.Contains(filtered.Body.String(), `"title":"alpha history"`) || strings.Contains(filtered.Body.String(), `"title":"beta history"`) {
		t.Fatalf("filters not applied: %s", filtered.Body.String())
	}
	betweenUpdates := time.Now().UTC().Add(-90 * time.Minute).Format(timeLayout)
	beforeFiltered := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=all&updated_before="+url.QueryEscape(betweenUpdates), fullToken, nil)
	if beforeFiltered.Code != http.StatusOK {
		t.Fatalf("updated_before status = %d, want %d; body=%s", beforeFiltered.Code, http.StatusOK, beforeFiltered.Body.String())
	}
	if !strings.Contains(beforeFiltered.Body.String(), `"title":"beta history"`) || strings.Contains(beforeFiltered.Body.String(), `"title":"alpha history"`) {
		t.Fatalf("updated_before filter not applied: %s", beforeFiltered.Body.String())
	}
	archivedOnly := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/history/sessions?archived=1", fullToken, nil)
	if archivedOnly.Code != http.StatusOK {
		t.Fatalf("archived status = %d, want %d; body=%s", archivedOnly.Code, http.StatusOK, archivedOnly.Body.String())
	}
	if !strings.Contains(archivedOnly.Body.String(), `"title":"beta history"`) || strings.Contains(archivedOnly.Body.String(), `"title":"alpha history"`) {
		t.Fatalf("archived filter not applied: %s", archivedOnly.Body.String())
	}
}

func TestAIRetrievalUsesSmartLatestDocsAcrossBranches(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()
	sourceRepo := createTestGitRepo(t)
	writeScanPathTestFile(t, sourceRepo, "doc/api-reference.md", "# API reference\n\nPublished endpoints: createRecord, getDetail.\n")
	runTestGit(t, sourceRepo, "add", ".")
	runTestGit(t, sourceRepo, "commit", "-m", "main api docs")
	runTestGit(t, sourceRepo, "checkout", "-b", "dev")
	writeScanPathTestFile(t, sourceRepo, "doc/integration/external-adapter.md", "# External adapter API\n\nThe alpha module supports API-based and dashboard-based adapters.\n\nAdapter endpoints: /api/alpha/external/query-detail, /api/alpha/external/query-list, /api/alpha/external/get-links, /api/alpha/external/status-callback, /api/alpha/external/delivery-failed.\n\nThe module pushes events to notify_url and reads current status from query_url.\n")
	writeScanPathTestFile(t, sourceRepo, "doc/integration/adapter-design.md", "# Adapter design\n\nExternal adapters include push, callback, query, and status notification flows.\n")
	runTestGit(t, sourceRepo, "add", ".")
	runTestGit(t, sourceRepo, "commit", "-m", "dev adapter docs")

	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "alpha-module",
		Slug:                  "alpha-module",
		RepoURL:               sourceRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"*"},
		LatestIncludeBranches: []string{"*"},
		ScanPaths:             []ScanPath{{Path: "doc", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, repo.ID, "manual"); err != nil {
		t.Fatalf("scan repository: %v", err)
	}

	cfg := defaultAIConfig()
	cfg.Chat.MaxContextChunks = 8
	retrieval, err := server.retrieveAIEvidence(ctx, "alpha module supports which external adapter APIs?", AIQuestionScope{
		RepoMode:   "global",
		SourceMode: "smart_latest_with_branch_candidates",
	}, cfg)
	if err != nil {
		t.Fatalf("retrieve evidence: %v", err)
	}

	var foundAdapterDoc bool
	for _, evidence := range retrieval.Evidence {
		if evidence.Citation.FilePath == "doc/integration/external-adapter.md" &&
			evidence.Citation.Branch == "dev" &&
			evidence.Citation.SourceScope == "smart_latest" {
			foundAdapterDoc = true
			if !strings.Contains(evidence.Content, "query-detail") || !strings.Contains(evidence.Content, "notify_url") {
				t.Fatalf("adapter evidence missing API details:\n%s", evidence.Content)
			}
		}
	}
	if !foundAdapterDoc {
		t.Fatalf("smart latest evidence did not include dev adapter docs: %+v", retrieval.Evidence)
	}
}

func TestAIQueryTermsDeriveChinesePhrasesWithoutBusinessDictionary(t *testing.T) {
	terms := strings.Join(aiQueryTerms("你好，认证模块支持哪些外部接入接口？"), ",")
	for _, want := range []string{"认证模块", "外部", "接入", "接口"} {
		if !strings.Contains(terms, want) {
			t.Fatalf("terms %q missing %q", terms, want)
		}
	}
	if strings.Contains(terms, ",游,") || strings.Contains(terms, ",戏,") {
		t.Fatalf("terms should not rely on noisy single-character matches: %q", terms)
	}
}

func TestAIQueryTermsSplitsMixedChineseAndIdentifierTokens(t *testing.T) {
	terms := strings.Join(aiQueryTerms("alpha模块签发的token格式是什么样的？"), ",")
	for _, want := range []string{"alpha", "签发", "token", "格式"} {
		if !strings.Contains(terms, want) {
			t.Fatalf("terms %q missing %q", terms, want)
		}
	}
}

func TestAIFollowUpQuestionUsesPreviousTokenContext(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()
	primaryRepoDir := createTestGitRepo(t)
	writeScanPathTestFile(t, primaryRepoDir, "src/security/session_token.ts", `export interface SessionTokenPayload {
  user_id: number
  create_time: number
  expire: number
  password: string
  type: null
}

export function signUserToken(user) {
  const payload: SessionTokenPayload = {
    user_id: user.id,
    create_time: Math.floor(Date.now() / 1000),
    expire: Math.floor(Date.now() / 1000) + 30 * 24 * 3600,
    password: user.password,
    type: null,
  }
  return jwt.sign(payload, privateKey)
}
`)
	runTestGit(t, primaryRepoDir, "add", ".")
	runTestGit(t, primaryRepoDir, "commit", "-m", "add token utility")
	primaryCommit := strings.TrimSpace(runTestGitOutput(t, primaryRepoDir, "rev-parse", "HEAD"))
	primaryRepo, err := createRepository(ctx, server.db, Repository{
		Name:                  "alpha-module",
		Slug:                  "alpha-module",
		RepoURL:               primaryRepoDir,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"main"},
		LatestIncludeBranches: []string{"main"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create primary repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, primaryRepo.ID, "manual"); err != nil {
		t.Fatalf("scan primary repository: %v", err)
	}

	secondaryRepoDir := createTestGitRepo(t)
	writeScanPathTestFile(t, secondaryRepoDir, "doc/structures.md", "# Structure notes\n\nWidgetConfig structure, HTTP response envelope, and sorting result structure.\n")
	runTestGit(t, secondaryRepoDir, "add", ".")
	runTestGit(t, secondaryRepoDir, "commit", "-m", "add unrelated structures")
	secondaryRepo, err := createRepository(ctx, server.db, Repository{
		Name:                  "beta-module",
		Slug:                  "beta-module",
		RepoURL:               secondaryRepoDir,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"main"},
		LatestIncludeBranches: []string{"main"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create secondary repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, secondaryRepo.ID, "manual"); err != nil {
		t.Fatalf("scan secondary repository: %v", err)
	}

	session, err := createAISession(ctx, server.db, "token format", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := insertAIMessage(ctx, server.db, AIMessage{
		SessionID: session.ID,
		Role:      "user",
		Content:   "alpha module 签发的 token 格式是什么样的？",
	}); err != nil {
		t.Fatalf("insert previous user message: %v", err)
	}
	assistantMsg, err := insertAIMessage(ctx, server.db, AIMessage{
		SessionID: session.ID,
		Role:      "assistant",
		Content:   "alpha module token 定义在 src/security/session_token.ts，包含 user_id、create_time、expire、password、type。",
	})
	if err != nil {
		t.Fatalf("insert previous assistant message: %v", err)
	}
	if _, err := insertAICitation(ctx, server.db, AIMessageCitation{
		MessageID:   assistantMsg.ID,
		RepoID:      primaryRepo.ID,
		SourceScope: "smart_latest",
		Branch:      "main",
		CommitSHA:   primaryCommit,
		FilePath:    "src/security/session_guard.ts",
		LineStart:   1,
		LineEnd:     20,
		QuoteText:   "guard checks old token",
		Score:       200,
	}); err != nil {
		t.Fatalf("insert previous unrelated citation: %v", err)
	}
	if _, err := insertAICitation(ctx, server.db, AIMessageCitation{
		MessageID:   assistantMsg.ID,
		RepoID:      primaryRepo.ID,
		SourceScope: "smart_latest",
		Branch:      "main",
		CommitSHA:   primaryCommit,
		FilePath:    "src/security/session_token.ts",
		LineStart:   1,
		LineEnd:     18,
		QuoteText:   "SessionTokenPayload user_id create_time expire password type",
		Score:       100,
	}); err != nil {
		t.Fatalf("insert previous citation: %v", err)
	}

	prepared, err := server.prepareAIQuestion(ctx, session.ID, "能给我详细的结构说明吗？", AIQuestionScope{
		RepoMode:   "global",
		SourceMode: "smart_latest_with_branch_candidates",
		FileTypes:  []string{"all"},
	})
	if err != nil {
		t.Fatalf("prepare follow-up question: %v", err)
	}
	if !prepared.Conversation.FollowUp {
		t.Fatal("expected follow-up context")
	}
	if prepared.Scope.RepoMode != "follow_up_context" || len(prepared.Scope.RepoIDs) != 1 || prepared.Scope.RepoIDs[0] != primaryRepo.ID {
		t.Fatalf("prepared scope = %+v, want previous primary repo only", prepared.Scope)
	}
	if strings.Join(prepared.Conversation.PreviousCitationPaths, ",") != "src/security/session_token.ts" {
		t.Fatalf("prepared citation paths = %+v, want token path mentioned by previous answer", prepared.Conversation.PreviousCitationPaths)
	}

	cfg := defaultAIConfig()
	cfg.Chat.MaxContextChunks = 8
	retrieval, err := server.retrieveAIEvidence(ctx, prepared.SearchQuestion, prepared.Scope, cfg)
	if err != nil {
		t.Fatalf("retrieve follow-up evidence: %v", err)
	}
	applyAIConversationContext(&retrieval, prepared)
	var foundTokenFile bool
	for _, evidence := range retrieval.Evidence {
		if evidence.Citation.RepoID == secondaryRepo.ID {
			t.Fatalf("follow-up retrieval should not drift to unrelated secondary repo: %+v", evidence.Citation)
		}
		if evidence.Citation.FilePath == "src/security/session_token.ts" &&
			strings.Contains(evidence.Content, "SessionTokenPayload") &&
			strings.Contains(evidence.Content, "create_time") {
			foundTokenFile = true
		}
	}
	if !foundTokenFile {
		t.Fatalf("follow-up retrieval did not include user token evidence: %+v", retrieval.Evidence)
	}
	messages := buildAIChatMessages("能给我详细的结构说明吗？", retrieval)
	if !strings.Contains(messages[1].Content, "上一轮用户问题") || !strings.Contains(messages[1].Content, "src/security/session_token.ts") {
		t.Fatalf("model prompt missing follow-up context: %s", messages[1].Content)
	}
}

func TestAIBranchCandidateFollowsRepositoryBranchRules(t *testing.T) {
	repo := Repository{TrackedBranches: []string{"*"}, LatestIncludeBranches: []string{"*"}, LatestExcludeBranches: []string{"archive/*"}}
	if !aiBranchCandidate(repo, RepoRef{RefName: "dev"}) {
		t.Fatal("dev should be a branch candidate when repository rules include all branches")
	}
	if aiBranchCandidate(repo, RepoRef{RefName: "archive/old"}) {
		t.Fatal("excluded branches should not be branch candidates")
	}
}

func TestAIQuestionStreamEndpointPersistsDeltasAndProviderFailover(t *testing.T) {
	requireGit(t)
	server := newWebhookTestServer(t)
	ctx := context.Background()
	createAIStreamEvidenceRepo(t, server)
	failingProvider := newAIStreamHTTPErrorTestServer(t, http.StatusTooManyRequests, `{"error":"rate limit sk-test-stream-leak-12345678"}`)
	successProvider := newAIStreamModelTestServer(t, []string{"第一段", "第二段"}, true)
	installAIStreamConfig(t, server, []AIProvider{
		newAIStreamProvider(t, server, "deepseek-main", "DeepSeek", failingProvider.URL, "deepseek-v4-flash", "sk-test-first-stream-1234", 10),
		newAIStreamProvider(t, server, "openai-main", "OpenAI", successProvider.URL, "gpt-4.1-mini", "sk-test-second-stream-5678", 20),
	})
	session, err := createAISession(ctx, server.db, "新的 AI 问答", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	recorder := doJSON(t, server, http.MethodPost, "/api/ai/sessions/"+strconv.FormatInt(session.ID, 10)+"/messages/stream", map[string]any{
		"question":       "Hello README",
		"scope_override": AIQuestionScope{RepoMode: "global"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, event := range []string{"event: user_message", "event: run_started", "event: stage", "event: provider_attempt", "event: answer_delta", "event: message_done", "event: done"} {
		if !strings.Contains(body, event) {
			t.Fatalf("stream body missing %s: %s", event, body)
		}
	}
	if !strings.Contains(body, `"provider_key":"deepseek-main"`) || !strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("stream body missing failed first provider: %s", body)
	}
	if !strings.Contains(body, `"provider_key":"openai-main"`) || !strings.Contains(body, `"status":"succeeded"`) {
		t.Fatalf("stream body missing successful failover provider: %s", body)
	}
	if strings.Contains(body, "sk-test") {
		t.Fatalf("stream response leaked API key: %s", body)
	}
	var content, status, provider, model string
	if err := server.db.QueryRowContext(ctx, `SELECT content, status, provider_name, model
		FROM ai_messages WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`, session.ID).
		Scan(&content, &status, &provider, &model); err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if content != "第一段第二段" || status != "success" || provider != "OpenAI" || model != "gpt-4.1-mini" {
		t.Fatalf("assistant message = content=%q status=%q provider=%q model=%q", content, status, provider, model)
	}
}

func TestAIQuestionStreamProviderFailureAfterDeltaSavesPartialWithoutFailover(t *testing.T) {
	requireGit(t)
	server := newWebhookTestServer(t)
	ctx := context.Background()
	createAIStreamEvidenceRepo(t, server)
	partialProvider := newAIStreamBrokenAfterDeltaTestServer(t, "部分回答")
	unusedProvider := newAIStreamModelTestServer(t, []string{"不应出现"}, true)
	installAIStreamConfig(t, server, []AIProvider{
		newAIStreamProvider(t, server, "deepseek-main", "DeepSeek", partialProvider.URL, "deepseek-v4-flash", "sk-test-partial-stream-1234", 10),
		newAIStreamProvider(t, server, "openai-main", "OpenAI", unusedProvider.URL, "gpt-4.1-mini", "sk-test-unused-stream-5678", 20),
	})
	session, err := createAISession(ctx, server.db, "新的 AI 问答", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	recorder := doJSON(t, server, http.MethodPost, "/api/ai/sessions/"+strconv.FormatInt(session.ID, 10)+"/messages/stream", map[string]any{
		"question":       "Hello README",
		"scope_override": AIQuestionScope{RepoMode: "global"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: error") || !strings.Contains(body, `"partial_message_id"`) {
		t.Fatalf("stream body missing partial error event: %s", body)
	}
	if strings.Contains(body, `"provider_key":"openai-main"`) || strings.Contains(body, "不应出现") {
		t.Fatalf("stream should not fail over after first delta: %s", body)
	}
	var content, status, errorMessage string
	if err := server.db.QueryRowContext(ctx, `SELECT content, status, error_message
		FROM ai_messages WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`, session.ID).
		Scan(&content, &status, &errorMessage); err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if content != "部分回答" || status != "partial" || errorMessage == "" {
		t.Fatalf("assistant partial message = content=%q status=%q error=%q", content, status, errorMessage)
	}
}

func TestAIQuestionStreamContextCancelSavesPartial(t *testing.T) {
	requireGit(t)
	server := newWebhookTestServer(t)
	ctx := context.Background()
	createAIStreamEvidenceRepo(t, server)
	modelServer := newAIStreamModelTestServer(t, []string{"取消前", "取消后"}, true)
	installAIStreamConfig(t, server, []AIProvider{
		newAIStreamProvider(t, server, "deepseek-main", "DeepSeek", modelServer.URL, "deepseek-v4-flash", "sk-test-cancel-stream-1234", 10),
	})
	session, err := createAISession(ctx, server.db, "新的 AI 问答", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	err = server.askAIQuestionStream(streamCtx, session.ID, "Hello README", AIQuestionScope{RepoMode: "global"}, "test", func(event string, data any) error {
		if event == "answer_delta" {
			cancel()
			return streamCtx.Err()
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected canceled stream error")
	}
	var content, status, errorMessage string
	if err := server.db.QueryRowContext(ctx, `SELECT content, status, error_message
		FROM ai_messages WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`, session.ID).
		Scan(&content, &status, &errorMessage); err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if content != "取消前" || status != "partial" || errorMessage == "" {
		t.Fatalf("assistant canceled partial = content=%q status=%q error=%q", content, status, errorMessage)
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

func newAIStreamModelTestServer(t *testing.T, chunks []string, includeUsage bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAIStreamRequest(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + chunk + `"}}]}` + "\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if includeUsage {
			_, _ = w.Write([]byte(`data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4}}` + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server
}

func newAIStreamHTTPErrorTestServer(t *testing.T, status int, responseBody string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAIStreamRequest(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server
}

func newAIStreamBrokenAfterDeltaTestServer(t *testing.T, firstChunk string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAIStreamRequest(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + firstChunk + `"}}]}` + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {broken-json\n\n"))
	}))
	t.Cleanup(server.Close)
	return server
}

func requireAIStreamRequest(t *testing.T, r *http.Request) {
	t.Helper()
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
	if payload["stream"] != true {
		t.Fatalf("stream = %v, want true", payload["stream"])
	}
	if payload["model"] == "" {
		t.Fatalf("model is required in stream request")
	}
}

func createAIStreamEvidenceRepo(t *testing.T, server *Server) Repository {
	t.Helper()
	ctx := context.Background()
	sourceRepo := createTestGitRepo(t)
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "stream-docs",
		Slug:                  "stream-docs",
		RepoURL:               sourceRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"main"},
		LatestIncludeBranches: []string{"main"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, repo.ID, "manual"); err != nil {
		t.Fatalf("scan repository: %v", err)
	}
	return repo
}

func newAIStreamProvider(t *testing.T, server *Server, key, name, baseURL, model, apiKey string, priority int) AIProvider {
	t.Helper()
	secret, err := server.createOrUpdateAISecret(context.Background(), 0, aiSecretRequest{Name: key + "-api-key", SecretType: "api_key", Value: apiKey}, "test")
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	return AIProvider{
		ProviderKey:           key,
		Name:                  name,
		Priority:              priority,
		ProviderType:          "openai_compatible",
		BaseURL:               baseURL,
		Model:                 model,
		APIKeySecretID:        secret.ID,
		CostTier:              "medium",
		RequestTimeoutSeconds: 5,
		MaxRPM:                60,
	}
}

func installAIStreamConfig(t *testing.T, server *Server, providers []AIProvider) {
	t.Helper()
	ctx := context.Background()
	if _, err := ensureActiveAIConfig(ctx, server.db); err != nil {
		t.Fatalf("ensure active config: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = providers
	cfg.Chat.Routing = buildDefaultRouting(ctx, server.db, cfg.Chat.Providers)
	cfg = normalizeAIConfig(cfg)
	raw, hash, err := aiConfigJSONAndHash(cfg)
	if err != nil {
		t.Fatalf("config json: %v", err)
	}
	now := nowString()
	if _, err := server.db.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'superseded', superseded_at = ?, updated_at = ?
		WHERE status = 'active'`, now, now); err != nil {
		t.Fatalf("supersede active config: %v", err)
	}
	var maxVersion int
	if err := server.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM ai_config_versions`).Scan(&maxVersion); err != nil {
		t.Fatalf("max config version: %v", err)
	}
	if _, err := server.db.ExecContext(ctx, `INSERT INTO ai_config_versions
		(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
		 created_at, updated_at, published_at)
		VALUES (?, 'active', ?, ?, ?, 'pass', ?, ?, ?, ?)`,
		maxVersion+1, hash, raw, encodeJSON(aiSecretRefs(cfg)), `{"ok":true}`, now, now, now); err != nil {
		t.Fatalf("insert active config: %v", err)
	}
}

func doJSON(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doAuthorizedJSON(t, server, method, path, "", body)
}

func doAuthorizedJSON(t *testing.T, server *Server, method, path, bearer string, body any) *httptest.ResponseRecorder {
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
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	return recorder
}

func issueAccessTokenForTest(t *testing.T, server *Server, scope accessTokenScope, ttl time.Duration) string {
	t.Helper()
	now := time.Now().UTC()
	token, err := server.signAccessToken(accessTokenPayload{
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(ttl).Unix(),
		Capabilities: []string{accessTokenCapabilityAIHistoryRead},
		Scope:        scope,
		JTI:          "test-token",
	})
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	return token
}

func countAISecrets(t *testing.T, server *Server) int {
	t.Helper()
	var count int
	if err := server.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ai_secrets`).Scan(&count); err != nil {
		t.Fatalf("count ai secrets: %v", err)
	}
	return count
}
