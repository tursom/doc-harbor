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

func TestAIAgentWorkflowPolicyDefaultsToShadowAndKeepsCompatibility(t *testing.T) {
	policy := defaultAIAgentWorkflowPolicy()
	if policy.Version != aiAgentWorkflowVersionV2Shadow || policy.Mode != "shadow" || policy.AnswerMode != "legacy" {
		t.Fatalf("default workflow policy should be v2 shadow legacy, got %+v", policy)
	}
	active := newAIAgentWorkflowPolicy(aiAgentWorkflowVersionV2Active)
	if active.Version != aiAgentWorkflowVersionV2Active || active.Mode != "active" || active.AnswerMode != "agent_workflow" {
		t.Fatalf("active workflow policy cannot express v2-active: %+v", active)
	}

	checkpoint := buildAIAgentRunCheckpoint(AIQuestionScope{RepoMode: "global"}, "standard", aiQuestionPreparation{
		SearchQuestion:       "接口参数是什么？",
		GeneratedSearchTerms: []string{"接口", "参数"},
	})
	if checkpoint["agent_workflow_version"] != aiAgentWorkflowVersionV2Shadow || checkpoint["answer_mode"] != "legacy" {
		t.Fatalf("checkpoint should default to shadow legacy: %+v", checkpoint)
	}
	failurePolicy, ok := checkpoint["failure_policy"].(map[string]aiAgentWorkflowFailurePolicy)
	if !ok {
		t.Fatalf("checkpoint failure policy has unexpected type: %#v", checkpoint["failure_policy"])
	}
	for node, wantFallback := range map[string]string{
		"task_frame":       "legacy_intent_and_retrieval",
		"contract_builder": "legacy_answer",
		"evidence_curator": "legacy_answer",
		"contract_checker": "legacy_answer",
		"answer_verifier":  "conservative_answer_or_local_evidence_summary",
	} {
		if failurePolicy[node].Fallback != wantFallback || failurePolicy[node].Record == "" {
			t.Fatalf("fallback for %s = %+v, want fallback %s", node, failurePolicy[node], wantFallback)
		}
	}
	events, ok := checkpoint["sse_compatibility"].([]string)
	if !ok {
		t.Fatalf("checkpoint sse compatibility has unexpected type: %#v", checkpoint["sse_compatibility"])
	}
	for _, event := range []string{"run_started", "task_frame", "contract", "stage", "provider_attempt", "verification", "citations", "answer_delta", "message_done"} {
		if !testStringSliceContains(events, event) {
			t.Fatalf("checkpoint missing legacy SSE event %s: %+v", event, events)
		}
	}
}

func TestAIAgentTaskFrameDeterministicIntents(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	cfg := defaultAIConfig()
	cases := []struct {
		name     string
		question string
		want     string
	}{
		{name: "database direct update", question: "我想在数据库里直接修改游戏的价格", want: aiTaskIntentDatabaseDirectUpdateForTest},
		{name: "api integration", question: "下单页面需要接哪些接口？请求参数和返回字段是什么？", want: aiTaskIntentAPIIntegration},
		{name: "branch lookup", question: "库存锁定的新接口现在在哪个分支？", want: aiTaskIntentBranchLookup},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, step := server.frameAITask(ctx, cfg, tc.question, aiQuestionPreparation{
				SearchQuestion: tc.question,
				Scope:          AIQuestionScope{RepoMode: "global"},
			})
			if frame.Intent != tc.want {
				t.Fatalf("intent = %q, want %q; frame=%+v", frame.Intent, tc.want, frame)
			}
			if step.AgentName != "task_framer" || step.StepType != "deterministic" || step.Status != "success" {
				t.Fatalf("unexpected framing step: %+v", step)
			}
			if !strings.Contains(step.OutputJSON, `"intent":"`+tc.want+`"`) {
				t.Fatalf("step output missing task frame intent: %s", step.OutputJSON)
			}
		})
	}
}

func TestAIAgentTaskFrameModelSupplementFailureKeepsDeterministicFrame(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	modelServer := newAIAnswerModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"not json"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	secret, err := server.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: "framer-api-key", SecretType: "api_key", Value: "sk-test-framer-secret"}, "test")
	if err != nil {
		t.Fatalf("create framer secret: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = []AIProvider{{
		ProviderKey:           "framer",
		Name:                  "Framer",
		Priority:              10,
		ProviderType:          "openai_compatible",
		BaseURL:               modelServer.URL,
		Model:                 "frame-model",
		APIKeySecretID:        secret.ID,
		RequestTimeoutSeconds: 5,
		MaxRPM:                60,
	}}
	cfg.Chat.Routing = buildDefaultRouting(ctx, server.db, cfg.Chat.Providers)

	frame, step := server.frameAITask(ctx, cfg, "下单页面需要接哪些接口？请求参数和返回字段是什么？", aiQuestionPreparation{
		SearchQuestion: "下单页面需要接哪些接口？请求参数和返回字段是什么？",
		Scope:          AIQuestionScope{RepoMode: "global"},
	})
	if frame.Intent != aiTaskIntentAPIIntegration {
		t.Fatalf("intent = %q, want deterministic api intent", frame.Intent)
	}
	if step.StepType != "model_call" || step.Status != "failed" || !strings.Contains(step.ErrorMessage, "JSON") {
		t.Fatalf("model supplement failure not recorded as expected: %+v", step)
	}
	if !strings.Contains(step.OutputJSON, `"intent":"api_integration"`) {
		t.Fatalf("failed model supplement did not preserve deterministic frame: %s", step.OutputJSON)
	}
	if strings.Contains(step.OutputJSON, "sk-test") || strings.Contains(step.ErrorMessage, "sk-test") {
		t.Fatalf("task frame step leaked secret: %+v", step)
	}
}

func TestAIRunCheckpointDefaultsToShadowAndSurvivesFailedFinish(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	session, err := createAISession(ctx, server.db, "checkpoint", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	userMsg, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: session.ID, Role: "user", Content: "question"})
	if err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	run, err := createAIRun(ctx, server.db, session.ID, userMsg.ID, AIConfigVersion{
		Version:    1,
		Config:     defaultAIConfig(),
		ConfigHash: "checkpoint-test",
	}, AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	initial := decodeAICheckpointForTest(t, run.CheckpointJSON)
	if initial["agent_workflow_version"] != aiAgentWorkflowVersionV2Shadow || initial["answer_mode"] != "legacy" {
		t.Fatalf("initial checkpoint missing shadow workflow: %+v", initial)
	}

	if err := finishAIRun(ctx, server.db, run.ID, AIAgentRun{
		Status:       "failed",
		CurrentState: "task_frame",
		ScopeJSON:    encodeJSON(AIQuestionScope{RepoMode: "global"}),
		ErrorMessage: "task frame failed",
	}); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	finished, err := getAIRun(ctx, server.db, run.ID)
	if err != nil {
		t.Fatalf("get finished run: %v", err)
	}
	preserved := decodeAICheckpointForTest(t, finished.CheckpointJSON)
	if preserved["agent_workflow_version"] != aiAgentWorkflowVersionV2Shadow || preserved["answer_mode"] != "legacy" {
		t.Fatalf("failed finish should preserve initial workflow checkpoint: %+v", preserved)
	}
}

func TestAIQuestionEndpointKeepsLegacyFieldsAndWritesShadowCheckpoint(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	session, err := createAISession(ctx, server.db, "新的 AI 问答", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	recorder := doJSON(t, server, http.MethodPost, "/api/ai/sessions/"+strconv.FormatInt(session.ID, 10)+"/messages", map[string]any{
		"question":       "没有证据的问题",
		"scope_override": AIQuestionScope{RepoMode: "global"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, required := range []string{`"run":`, `"message":`, `"service_candidates":[]`, `"citations":[]`} {
		if !strings.Contains(body, required) {
			t.Fatalf("legacy response missing %s: %s", required, body)
		}
	}
	if strings.Contains(body, "api_key") || strings.Contains(body, "sk-test") {
		t.Fatalf("question response leaked secret-like content: %s", body)
	}
	var result aiQuestionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode question response: %v", err)
	}
	checkpoint := decodeAICheckpointForTest(t, result.Run.CheckpointJSON)
	if checkpoint["agent_workflow_version"] != aiAgentWorkflowVersionV2Shadow || checkpoint["answer_mode"] != "legacy" {
		t.Fatalf("run checkpoint should use shadow legacy workflow: %+v", checkpoint)
	}
	taskFrame, ok := checkpoint["task_frame"].(map[string]any)
	if !ok || taskFrame["intent"] != aiTaskIntentDocumentQA {
		t.Fatalf("run checkpoint missing task frame: %+v", checkpoint)
	}
	evidenceContract, ok := checkpoint["evidence_contract"].(map[string]any)
	if !ok || evidenceContract["contract_id"] != "document_qa.v1" {
		t.Fatalf("run checkpoint missing evidence contract: %+v", checkpoint)
	}
	evidenceBundle, ok := checkpoint["evidence_bundle"].(map[string]any)
	if !ok || evidenceBundle["bundle_id"] == "" {
		t.Fatalf("run checkpoint missing evidence bundle: %+v", checkpoint)
	}
	curatorCoverage, ok := checkpoint["curator_coverage"].(map[string]any)
	if !ok || curatorCoverage["contract_id"] != "document_qa.v1" {
		t.Fatalf("run checkpoint missing curator coverage: %+v", checkpoint)
	}
	if result.Run.Status != "insufficient_evidence" || result.Run.VerificationStatus != "fail" {
		t.Fatalf("no-evidence run status = %s/%s, want insufficient_evidence/fail", result.Run.Status, result.Run.VerificationStatus)
	}
	if !strings.Contains(result.Run.VerificationReportJSON, `"agent_workflow_version":"v2-shadow"`) {
		t.Fatalf("verification report missing workflow version: %s", result.Run.VerificationReportJSON)
	}
	steps, err := listAIRunSteps(ctx, server.db, result.Run.ID)
	if err != nil {
		t.Fatalf("list run steps: %v", err)
	}
	var foundCoordinator, foundTaskFramer, foundContractBuilder, foundEvidenceCurator bool
	for _, step := range steps {
		if step.AgentName == "coordinator" && strings.Contains(step.OutputJSON, `"agent_workflow_version":"v2-shadow"`) {
			foundCoordinator = true
		}
		if step.AgentName == "task_framer" && strings.Contains(step.OutputJSON, `"intent":"document_qa"`) {
			foundTaskFramer = true
		}
		if step.AgentName == "contract_builder" && step.StepType == "deterministic" {
			var contract aiEvidenceContract
			if err := json.Unmarshal([]byte(step.OutputJSON), &contract); err != nil {
				t.Fatalf("contract_builder output should be direct contract JSON: %v; output=%s", err, step.OutputJSON)
			}
			if contract.ContractID != "document_qa.v1" || !testStringSliceContains(aiEvidenceRequirementKeys(contract.Required), "cited_documents") {
				t.Fatalf("unexpected contract_builder output: %+v", contract)
			}
			foundContractBuilder = true
		}
		if step.AgentName == "evidence_curator" && step.StepType == "deterministic" {
			if !strings.Contains(step.OutputJSON, `"evidence_bundle"`) || !strings.Contains(step.OutputJSON, `"coverage"`) {
				t.Fatalf("evidence_curator step missing bundle or coverage: %s", step.OutputJSON)
			}
			foundEvidenceCurator = true
		}
	}
	if !foundCoordinator {
		t.Fatalf("coordinator checkpoint step missing workflow version: %+v", steps)
	}
	if !foundTaskFramer {
		t.Fatalf("task_framer step missing task frame: %+v", steps)
	}
	if !foundContractBuilder {
		t.Fatalf("contract_builder step missing evidence contract: %+v", steps)
	}
	if !foundEvidenceCurator {
		t.Fatalf("evidence_curator step missing curator output: %+v", steps)
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
	diagnostics := doJSON(t, server, http.MethodPost, "/api/tokens", map[string]any{
		"capabilities": []string{accessTokenCapabilityAIDiagnosticsRead},
	})
	if diagnostics.Code != http.StatusOK {
		t.Fatalf("diagnostics capability status = %d, want %d; body=%s", diagnostics.Code, http.StatusOK, diagnostics.Body.String())
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

func TestAccessTokenAIDiagnosticsRuns(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "doc-harbor",
		Slug:                  "doc-harbor-diagnostics",
		RepoURL:               "https://example.test/doc-harbor-diagnostics.git",
		DefaultBranch:         "main",
		TrackedBranches:       []string{"main", "release/*"},
		LatestIncludeBranches: []string{"main", "release/*"},
		LatestExcludeBranches: []string{"archive/*"},
		BranchPriority:        []string{"main", "release/*"},
		CredentialRef:         "repo-secret-ref",
		Enabled:               true,
		ScanPaths: []ScanPath{{
			Path:         "docs",
			IncludeGlobs: []string{"**/*.md"},
			ExcludeGlobs: []string{"private/**"},
			Enabled:      true,
		}, {
			Path:    "disabled",
			Enabled: false,
		}},
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if _, err := server.db.ExecContext(ctx, `UPDATE repo_scan_paths SET enabled = 0 WHERE repo_id = ? AND path = 'disabled'`, repo.ID); err != nil {
		t.Fatalf("disable scan path: %v", err)
	}
	repo, err = getRepository(ctx, server.db, repo.ID)
	if err != nil {
		t.Fatalf("reload repo: %v", err)
	}
	secondaryRepo, err := createRepository(ctx, server.db, Repository{
		Name:          "secondary",
		Slug:          "secondary-diagnostics",
		RepoURL:       "https://example.test/secondary-diagnostics.git",
		DefaultBranch: "main",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create secondary repo: %v", err)
	}
	tx, err := server.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin refs tx: %v", err)
	}
	for _, ref := range []RepoRef{
		{RepoID: repo.ID, RefType: "branch", RefName: "main", CommitSHA: "mainsha", CommitTime: time.Now().UTC().Add(-30 * time.Minute).Format(timeLayout), LastScannedAt: time.Now().UTC().Format(timeLayout)},
		{RepoID: repo.ID, RefType: "branch", RefName: "release/v1", CommitSHA: "releasesha", CommitTime: time.Now().UTC().Add(-20 * time.Minute).Format(timeLayout), LastScannedAt: time.Now().UTC().Format(timeLayout)},
		{RepoID: secondaryRepo.ID, RefType: "branch", RefName: "main", CommitSHA: "secondsha", CommitTime: time.Now().UTC().Add(-25 * time.Minute).Format(timeLayout), LastScannedAt: time.Now().UTC().Format(timeLayout)},
	} {
		if err := upsertRepoRef(ctx, tx, ref); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert ref: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit refs: %v", err)
	}
	scanRun, err := createScanRun(ctx, server.db, repo.ID, "manual")
	if err != nil {
		t.Fatalf("create scan run: %v", err)
	}
	scanRun.Status = "success"
	scanRun.BranchCount = 2
	scanRun.FileCount = 10
	scanRun.SkippedCount = 1
	scanRun.ErrorCount = 0
	scanRun.DetailJSON = `{"internal":"not exposed"}`
	scanRun.ErrorMessage = "do not expose"
	if err := finishScanRun(ctx, server.db, scanRun); err != nil {
		t.Fatalf("finish scan run: %v", err)
	}
	alice, err := createAISession(ctx, server.db, "alpha diagnostic", "alice", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create alice session: %v", err)
	}
	bob, err := createAISession(ctx, server.db, "beta diagnostic", "bob", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create bob session: %v", err)
	}
	aliceUser, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: alice.ID, Role: "user", Content: "为什么 alpha 调用失败"})
	if err != nil {
		t.Fatalf("insert alice user: %v", err)
	}
	aliceAssistant, err := insertAIMessage(ctx, server.db, AIMessage{
		SessionID:        alice.ID,
		Role:             "assistant",
		Content:          "alpha answer",
		Model:            "diag-model",
		ProviderName:     "DiagProvider",
		ModelRouteJSON:   `{"route":"diag"}`,
		PromptTokens:     11,
		CompletionTokens: 7,
		LatencyMS:        123,
	})
	if err != nil {
		t.Fatalf("insert alice assistant: %v", err)
	}
	bobUser, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: bob.ID, Role: "user", Content: "beta question"})
	if err != nil {
		t.Fatalf("insert bob user: %v", err)
	}
	bobAssistant, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: bob.ID, Role: "assistant", Content: "beta answer"})
	if err != nil {
		t.Fatalf("insert bob assistant: %v", err)
	}
	aliceStarted := time.Now().UTC().Add(-time.Hour).Format(timeLayout)
	aliceFinished := time.Now().UTC().Add(-time.Hour + time.Minute).Format(timeLayout)
	bobStarted := time.Now().UTC().Add(-2 * time.Hour).Format(timeLayout)
	bobFinished := time.Now().UTC().Add(-2*time.Hour + time.Minute).Format(timeLayout)
	aliceRunID := insertDiagnosticsRunForTest(t, server, alice.ID, aliceUser.ID, aliceAssistant.ID, "failed", aliceStarted, aliceFinished, AIQuestionScope{
		RepoMode:   "selected",
		RepoIDs:    []int64{repo.ID},
		SourceMode: "smart_latest",
		CurrentFile: &AICurrentFileScope{
			RepoID:    repo.ID,
			VersionID: 12,
			Branch:    "main",
			CommitSHA: "mainsha",
			FilePath:  "docs/README.md",
		},
	})
	bobRunID := insertDiagnosticsRunForTest(t, server, bob.ID, bobUser.ID, bobAssistant.ID, "succeeded", bobStarted, bobFinished)
	if err := insertAIStep(ctx, server.db, AIAgentStep{
		RunID:            aliceRunID,
		AgentName:        "coordinator",
		StepType:         "checkpoint",
		Status:           "failed",
		ToolName:         "retrieval",
		TaskClass:        "standard",
		Model:            "diag-model",
		ProviderName:     "DiagProvider",
		ModelRouteReason: "primary",
		InputJSON:        `{"prompt":"secret prompt"}`,
		OutputJSON:       `{"raw":"secret output"}`,
		TokenInput:       11,
		TokenOutput:      7,
		EstimatedCost:    `{"usd":0.001}`,
		LatencyMS:        123,
		ErrorMessage:     "provider 500 sk-test-step-secret",
		CreatedAt:        aliceStarted,
		FinishedAt:       aliceFinished,
	}); err != nil {
		t.Fatalf("insert step: %v", err)
	}
	taskFrame := aiTaskFrame{
		Intent:          aiTaskIntentDiagnostics,
		UserGoal:        "排查一次 AI 问答运行的检索、证据和模型调用过程",
		AnswerShape:     "run_analysis",
		ScopeStrategy:   "selected_repositories",
		TargetArtifacts: []string{"run_steps", "retrieval_plan", "citations"},
		MustNot:         []string{"expose_secrets"},
		KnownTerms:      []string{"alpha"},
	}
	if err := insertAIStep(ctx, server.db, AIAgentStep{
		RunID:      aliceRunID,
		AgentName:  "task_framer",
		StepType:   "deterministic",
		Status:     "success",
		OutputJSON: encodeJSON(taskFrame),
		CreatedAt:  aliceStarted,
		FinishedAt: aliceFinished,
	}); err != nil {
		t.Fatalf("insert task framer step: %v", err)
	}
	contract := buildAIEvidenceContract(taskFrame)
	if err := insertAIStep(ctx, server.db, AIAgentStep{
		RunID:      aliceRunID,
		AgentName:  "contract_builder",
		StepType:   "deterministic",
		Status:     "success",
		OutputJSON: encodeJSON(contract),
		CreatedAt:  aliceStarted,
		FinishedAt: aliceFinished,
	}); err != nil {
		t.Fatalf("insert contract builder step: %v", err)
	}
	contractCoverage := aiContractCoverageReport{
		ContractID: contract.ContractID,
		Status:     "missing_required",
		Coverage: map[string]string{
			"run_identity": aiEvidenceCoverageCovered,
			"run_steps":    aiEvidenceCoveragePartial,
			"citations":    aiEvidenceCoverageMissing,
		},
		Items: []aiContractCoverageItem{
			{
				Key:           "run_identity",
				Requirement:   aiEvidenceCheckerRequirementRequired,
				Status:        aiEvidenceCoverageCovered,
				EvidenceIDs:   []int64{1},
				Reason:        "run record exists",
				MissingDetail: "",
				Confidence:    0.95,
			},
			{
				Key:           "run_steps",
				Requirement:   aiEvidenceCheckerRequirementRequired,
				Status:        aiEvidenceCoveragePartial,
				EvidenceIDs:   []int64{2},
				Reason:        "only partial step evidence is available",
				MissingDetail: "Authorization: Bearer raw-diagnostics-token api_key=sk-test-checker-secret token=raw-diagnostics-token secret raw-secret",
				Confidence:    0.55,
			},
			{
				Key:           "citations",
				Requirement:   aiEvidenceCheckerRequirementRequired,
				Status:        aiEvidenceCoverageMissing,
				EvidenceIDs:   []int64{},
				Reason:        "missing accepted evidence",
				MissingDetail: "need citation evidence for diagnostics",
				Confidence:    0,
			},
		},
		Covered:         []string{"run_identity"},
		Partial:         []string{"run_steps"},
		MissingRequired: []string{"run_steps", "citations"},
		NextAction:      aiEvidenceCheckerNextActionRetrieve,
		Details: map[string]string{
			"run_steps": "Authorization: Bearer raw-diagnostics-token api_key=sk-test-checker-secret token=raw-diagnostics-token secret raw-secret",
			"citations": "need citation evidence for diagnostics",
		},
	}
	if err := insertAIStep(ctx, server.db, AIAgentStep{
		RunID:     aliceRunID,
		AgentName: "contract_checker",
		StepType:  "deterministic",
		Status:    "success",
		InputJSON: encodeJSON(map[string]any{
			"input_summary": map[string]any{
				"contract": summarizeAIEvidenceContract(contract),
				"api_key":  "sk-test-checker-secret",
				"token":    "raw-diagnostics-token",
			},
		}),
		OutputJSON: encodeJSON(map[string]any{
			"contract_coverage": contractCoverage,
			"summary": map[string]any{
				"missing_required": []string{"run_steps", "citations"},
				"next_action":      aiEvidenceCheckerNextActionRetrieve,
				"token":            "raw-diagnostics-token",
			},
		}),
		CreatedAt:  aliceStarted,
		FinishedAt: aliceFinished,
	}); err != nil {
		t.Fatalf("insert contract checker step: %v", err)
	}
	curatorOutput := map[string]any{
		"evidence_bundle": map[string]any{
			"bundle_id": "curated-document_qa.v1",
			"coverage":  map[string]string{"cited_documents": aiEvidenceCoveragePartial},
			"excluded": []map[string]any{{
				"evidence_id":        3,
				"reason":             aiEvidenceExcludedReasonTestFixtureNonTestTask,
				"evidence_type":      "test_fixture",
				"source_reliability": aiEvidenceReliabilityExcludedTestFixtureNonTest,
				"repo_name":          "doc-harbor",
				"file_path":          "internal/app/alpha_test.go",
				"source_scope":       "smart_latest",
				"redaction_exercise": "Authorization: Bearer raw-diagnostics-token api_key=sk-test-curator-secret token=raw-diagnostics-token secret raw-secret",
				"authorization_note": "Bearer raw-diagnostics-token",
				"api_key":            "sk-test-curator-secret",
				"access_token":       "raw-diagnostics-token",
			}},
		},
		"coverage": map[string]any{
			"contract_id": "document_qa.v1",
			"status":      aiEvidenceCoveragePartial,
			"next_action": "contract_checker",
		},
		"annotations": []map[string]any{{
			"evidence_id":        3,
			"repo_name":          "doc-harbor",
			"file_path":          "internal/app/alpha_test.go",
			"source_scope":       "smart_latest",
			"evidence_type":      "test_fixture",
			"source_reliability": aiEvidenceReliabilityExcludedTestFixtureNonTest,
			"excluded_reason":    aiEvidenceExcludedReasonTestFixtureNonTestTask,
			"reason_detail":      "Authorization: Bearer raw-diagnostics-token api_key=sk-test-curator-secret token=raw-diagnostics-token secret raw-secret",
			"api_key":            "sk-test-curator-secret",
			"token":              "raw-diagnostics-token",
		}},
		"included_count": 1,
		"excluded_count": 1,
		"api_key":        "sk-test-curator-secret",
		"Authorization":  "Bearer raw-diagnostics-token",
		"access_token":   "raw-diagnostics-token",
	}
	if err := insertAIStep(ctx, server.db, AIAgentStep{
		RunID:      aliceRunID,
		AgentName:  "evidence_curator",
		StepType:   "deterministic",
		Status:     "success",
		OutputJSON: encodeJSON(curatorOutput),
		CreatedAt:  aliceStarted,
		FinishedAt: aliceFinished,
	}); err != nil {
		t.Fatalf("insert evidence curator step: %v", err)
	}
	if _, err := insertAIServiceCandidate(ctx, server.db, AIServiceCandidate{
		RunID:         aliceRunID,
		MessageID:     aliceAssistant.ID,
		RepoID:        repo.ID,
		ServiceName:   "doc-harbor",
		MatchedTerms:  []string{"alpha"},
		Confidence:    "high",
		Reason:        "matched alpha",
		Score:         9,
		EvidenceCount: 1,
	}); err != nil {
		t.Fatalf("insert candidate: %v", err)
	}
	if _, err := insertAICitation(ctx, server.db, AIMessageCitation{
		MessageID:   aliceAssistant.ID,
		RepoID:      repo.ID,
		VersionID:   1,
		SourceScope: "smart_latest",
		Branch:      "main",
		CommitSHA:   "abcdef",
		FilePath:    "README.md",
		LineStart:   1,
		LineEnd:     2,
		QuoteText:   "diagnostic quote",
		Score:       8,
	}); err != nil {
		t.Fatalf("insert citation: %v", err)
	}

	diagnosticsToken := issueAccessTokenForTest(t, server, accessTokenScope{}, time.Hour, accessTokenCapabilityAIDiagnosticsRead)
	list := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs?limit=1", diagnosticsToken, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("diagnostics list status = %d, want %d; body=%s", list.Code, http.StatusOK, list.Body.String())
	}
	var firstPage aiDiagnosticsRunsResponse
	if err := json.Unmarshal(list.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode diagnostics list: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.NextCursor == "" || firstPage.Items[0].Run.ID != aliceRunID || firstPage.Items[0].DurationMS != int64(time.Minute/time.Millisecond) {
		t.Fatalf("diagnostics first page = %+v", firstPage)
	}
	second := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs?limit=1&cursor="+firstPage.NextCursor, diagnosticsToken, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("diagnostics second page status = %d, want %d; body=%s", second.Code, http.StatusOK, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), `"id":`+strconv.FormatInt(bobRunID, 10)) {
		t.Fatalf("diagnostics second page missing bob run: %s", second.Body.String())
	}
	filtered := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs?status=failed&q=alpha&started_after="+url.QueryEscape(bobStarted)+"&started_before="+url.QueryEscape(aliceFinished), diagnosticsToken, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("diagnostics filtered status = %d, want %d; body=%s", filtered.Code, http.StatusOK, filtered.Body.String())
	}
	if !strings.Contains(filtered.Body.String(), `"viewer_key":"alice"`) || strings.Contains(filtered.Body.String(), `"viewer_key":"bob"`) {
		t.Fatalf("diagnostics filters not applied: %s", filtered.Body.String())
	}
	detail := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs/"+strconv.FormatInt(aliceRunID, 10), diagnosticsToken, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("diagnostics detail status = %d, want %d; body=%s", detail.Code, http.StatusOK, detail.Body.String())
	}
	body := detail.Body.String()
	for _, required := range []string{`"user_message"`, `"assistant_message"`, `"task_frame"`, `"evidence_contract"`, `"contract_coverage"`, `"steps"`, `"service_candidates"`, `"citations"`, `"model_route_json"`, `"provider_failover_json"`, `"contract_checker"`, `"missing_required":["run_steps","citations"]`, "sk-[redacted]"} {
		if !strings.Contains(body, required) {
			t.Fatalf("diagnostics detail missing %s: %s", required, body)
		}
	}
	for _, forbidden := range []string{"input_json", "output_json", "secret prompt", "secret output", "api_key", "api_key_secret_id", "sk-test", "raw-diagnostics-token", "raw-secret", "Authorization", "Bearer"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("diagnostics detail leaked %s: %s", forbidden, body)
		}
	}
	var detailResp aiDiagnosticsRunDetailResponse
	if err := json.Unmarshal(detail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("decode diagnostics detail: %v", err)
	}
	if len(detailResp.DataSources.Repositories) != 1 || detailResp.DataSources.Repositories[0].ID != repo.ID {
		t.Fatalf("run data sources were not narrowed by scope: %+v", detailResp.DataSources.Repositories)
	}
	if detailResp.TaskFrame == nil || detailResp.EvidenceContract == nil || detailResp.ContractCoverage == nil {
		t.Fatalf("diagnostics detail missing top-level agent artifacts: %+v", detailResp)
	}
	var foundTaskFramer, foundEvidenceCurator, foundContractChecker bool
	for _, step := range detailResp.Steps {
		if step.AgentName == "task_framer" && step.StepType == "deterministic" {
			foundTaskFramer = true
		}
		if step.AgentName == "contract_checker" && step.StepType == "deterministic" {
			foundContractChecker = true
			summaryJSON, err := json.Marshal(step.Summary)
			if err != nil {
				t.Fatalf("marshal contract checker summary: %v", err)
			}
			summary := string(summaryJSON)
			for _, required := range []string{`"contract_coverage"`, `"items"`, `"key":"citations"`, `"status":"missing"`, `"evidence_ids":[]`, `"next_action":"retrieve_missing_contract_keys"`} {
				if !strings.Contains(summary, required) {
					t.Fatalf("contract checker summary missing %s: %s", required, summary)
				}
			}
			for _, forbidden := range []string{"api_key", "sk-test-checker-secret", "raw-diagnostics-token", "raw-secret", "Authorization", "Bearer", `"token"`} {
				if strings.Contains(summary, forbidden) {
					t.Fatalf("contract checker summary leaked %s: %s", forbidden, summary)
				}
			}
		}
		if step.AgentName == "evidence_curator" && step.StepType == "deterministic" {
			foundEvidenceCurator = true
			summaryJSON, err := json.Marshal(step.Summary)
			if err != nil {
				t.Fatalf("marshal curator summary: %v", err)
			}
			summary := string(summaryJSON)
			for _, required := range []string{`"evidence_bundle"`, `"coverage"`, `"annotations"`, `"excluded_count":1`, aiEvidenceExcludedReasonTestFixtureNonTestTask, `"file_path":"internal/app/alpha_test.go"`} {
				if !strings.Contains(summary, required) {
					t.Fatalf("curator summary missing %s: %s", required, summary)
				}
			}
			for _, forbidden := range []string{"api_key", "sk-test-curator-secret", "raw-diagnostics-token", "raw-secret", "Authorization", "Bearer", `"token"`} {
				if strings.Contains(summary, forbidden) {
					t.Fatalf("curator summary leaked %s: %s", forbidden, summary)
				}
			}
		}
	}
	if !foundTaskFramer {
		t.Fatalf("diagnostics detail missing task_framer step: %+v", detailResp.Steps)
	}
	if !foundContractChecker {
		t.Fatalf("diagnostics detail missing contract_checker step: %+v", detailResp.Steps)
	}
	if !foundEvidenceCurator {
		t.Fatalf("diagnostics detail missing evidence_curator step: %+v", detailResp.Steps)
	}
	if detailResp.DataSources.CurrentFile == nil || detailResp.DataSources.CurrentFile.FilePath != "docs/README.md" {
		t.Fatalf("run data sources missing current file: %+v", detailResp.DataSources.CurrentFile)
	}
	if len(detailResp.DataSources.Repositories[0].CandidateTargets) != 0 {
		t.Fatalf("run data sources should not include branch candidates for smart_latest scope: %+v", detailResp.DataSources.Repositories[0].CandidateTargets)
	}
	dataSources := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/data-sources", diagnosticsToken, nil)
	if dataSources.Code != http.StatusOK {
		t.Fatalf("diagnostics data sources status = %d, want %d; body=%s", dataSources.Code, http.StatusOK, dataSources.Body.String())
	}
	var dataSourcesResp aiDiagnosticsSourcesResponse
	if err := json.Unmarshal(dataSources.Body.Bytes(), &dataSourcesResp); err != nil {
		t.Fatalf("decode diagnostics data sources: %v", err)
	}
	if len(dataSourcesResp.DataSources.Repositories) != 2 {
		t.Fatalf("data sources repo count = %d, want 2: %+v", len(dataSourcesResp.DataSources.Repositories), dataSourcesResp.DataSources.Repositories)
	}
	var primarySource aiDiagnosticsRepositorySource
	for _, source := range dataSourcesResp.DataSources.Repositories {
		if source.ID == repo.ID {
			primarySource = source
			break
		}
	}
	if primarySource.ID == 0 {
		t.Fatalf("data sources missing primary repo: %+v", dataSourcesResp.DataSources.Repositories)
	}
	if len(primarySource.ScanPaths) != 1 || primarySource.ScanPaths[0].Path != "docs" {
		t.Fatalf("data sources scan paths = %+v, want only enabled docs path", primarySource.ScanPaths)
	}
	if primarySource.DefaultTarget == nil || primarySource.DefaultTarget.Branch != "main" {
		t.Fatalf("data sources default target = %+v, want main", primarySource.DefaultTarget)
	}
	if len(primarySource.CandidateTargets) != 1 || primarySource.CandidateTargets[0].Branch != "release/v1" {
		t.Fatalf("data sources candidate targets = %+v, want release/v1", primarySource.CandidateTargets)
	}
	if primarySource.LatestScan == nil || primarySource.LatestScan.FileCount != 10 {
		t.Fatalf("data sources latest scan = %+v, want file_count 10", primarySource.LatestScan)
	}
	if dataSourcesResp.DataSources.Indexing.MaxFileSize <= 0 {
		t.Fatalf("data sources missing indexing summary: %+v", dataSourcesResp.DataSources.Indexing)
	}
	for _, forbidden := range []string{"repo_url", "credential_ref", "repo-secret-ref", "api_key", "secret", "input_json", "output_json", "detail_json", "do not expose"} {
		if strings.Contains(dataSources.Body.String(), forbidden) {
			t.Fatalf("diagnostics data sources leaked %s: %s", forbidden, dataSources.Body.String())
		}
	}
	historyToken := issueAccessTokenForTest(t, server, accessTokenScope{}, time.Hour, accessTokenCapabilityAIHistoryRead)
	forbidden := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs", historyToken, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("history token diagnostics status = %d, want %d; body=%s", forbidden.Code, http.StatusForbidden, forbidden.Body.String())
	}
	forbiddenDataSources := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/data-sources", historyToken, nil)
	if forbiddenDataSources.Code != http.StatusForbidden {
		t.Fatalf("history token data sources status = %d, want %d; body=%s", forbiddenDataSources.Code, http.StatusForbidden, forbiddenDataSources.Body.String())
	}
	scopedToken := issueAccessTokenForTest(t, server, accessTokenScope{ViewerKey: "alice"}, time.Hour, accessTokenCapabilityAIDiagnosticsRead)
	scopedList := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs", scopedToken, nil)
	if scopedList.Code != http.StatusOK {
		t.Fatalf("scoped diagnostics status = %d, want %d; body=%s", scopedList.Code, http.StatusOK, scopedList.Body.String())
	}
	if !strings.Contains(scopedList.Body.String(), `"viewer_key":"alice"`) || strings.Contains(scopedList.Body.String(), `"viewer_key":"bob"`) {
		t.Fatalf("diagnostics viewer scope not enforced: %s", scopedList.Body.String())
	}
	bobDetail := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs/"+strconv.FormatInt(bobRunID, 10), scopedToken, nil)
	if bobDetail.Code != http.StatusNotFound {
		t.Fatalf("scoped diagnostics detail status = %d, want %d; body=%s", bobDetail.Code, http.StatusNotFound, bobDetail.Body.String())
	}
	missing := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs", "", nil)
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing diagnostics token status = %d, want %d; body=%s", missing.Code, http.StatusUnauthorized, missing.Body.String())
	}
	missingDataSources := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/data-sources", "", nil)
	if missingDataSources.Code != http.StatusUnauthorized {
		t.Fatalf("missing data sources token status = %d, want %d; body=%s", missingDataSources.Code, http.StatusUnauthorized, missingDataSources.Body.String())
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

func TestAIRefRetrievalKeepsContentMatchedCodePastPathNoise(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()
	sourceRepo := createTestGitRepo(t)
	for i := 0; i < 220; i++ {
		writeScanPathTestFile(t, sourceRepo, "doc/价格补偿-"+strconv.Itoa(i)+".md", "# 价格补偿\n\n这里只描述 Steam 价格补偿和价格表同步。\n")
	}
	writeScanPathTestFile(t, sourceRepo, "core/handle/inventory.go", `package handle

import "strings"

type AddGameCdkInventoryRequest struct {
	Price string
}

type UpdateGameInventoryStatusRequest struct {
	InventoryIds []int64
	SellStatus   string
	Price        string
}

func AddGameCdkInventory(req AddGameCdkInventoryRequest) {
	// 基础参数校验：这里的价格仅用于添加 CDK 库存，不是修改已售卖库存的基础价格。
	priceText := strings.TrimSpace(req.Price)
	if priceText == "" {
		return
	}
	if !strings.Contains(priceText, ".") {
		return
	}
}

`+strings.Repeat("// 价格补偿噪声：Steam 价格表同步失败后会重试，不涉及卖家库存改价。\n", 80)+`
func UpdateGameInventoryStatus(req UpdateGameInventoryStatusRequest) {
	targetStatus := strings.ToLower(strings.TrimSpace(req.SellStatus))
	hasStatusUpdate := targetStatus != ""
	priceText := strings.TrimSpace(req.Price)
	hasPriceUpdate := priceText != ""
	targetPrice := priceText
	if !hasStatusUpdate && !hasPriceUpdate {
		return
	}
	priceUpdateInventoryIDs := make([]int64, 0, len(req.InventoryIds))
	for _, inventoryID := range req.InventoryIds {
		if hasPriceUpdate {
			priceUpdateInventoryIDs = append(priceUpdateInventoryIDs, inventoryID)
		}
	}
	if hasPriceUpdate {
		priceTargets := ListSellerGoodsSKUPriceTargetsByInventoryIDs(priceUpdateInventoryIDs)
		BatchUpdateSellerGoodsSKUPriceByTargets(targetPrice, priceTargets)
	}
}

func ListSellerGoodsSKUPriceTargetsByInventoryIDs(inventoryIDs []int64) []string {
	return nil
}

func BatchUpdateSellerGoodsSKUPriceByTargets(price string, targets []string) {
}
`)
	runTestGit(t, sourceRepo, "add", ".")
	runTestGit(t, sourceRepo, "commit", "-m", "add noisy price docs and inventory price update")

	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "game-service",
		Slug:                  "game-service",
		RepoURL:               sourceRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"*"},
		LatestIncludeBranches: []string{"*"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, repo.ID, "manual"); err != nil {
		t.Fatalf("scan repository: %v", err)
	}

	plannerServer := newAIAnswerModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"{\"terms\":[\"UpdateGameInventoryStatus\",\"inventory\",\"Price\",\"handler\",\"BatchUpdateSellerGoodsSKUPriceByTargets\"]}"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	secret, err := server.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: "query-planner-api-key", SecretType: "api_key", Value: "sk-test-query-planner"}, "test")
	if err != nil {
		t.Fatalf("create planner secret: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = []AIProvider{{
		ProviderKey:           "query-planner",
		Name:                  "QueryPlanner",
		Priority:              10,
		ProviderType:          "openai_compatible",
		BaseURL:               plannerServer.URL,
		Model:                 "query-planner-model",
		APIKeySecretID:        secret.ID,
		CostTier:              "low",
		RequestTimeoutSeconds: 5,
		MaxRPM:                60,
	}}
	cfg.Chat.Routing = buildDefaultRouting(ctx, server.db, cfg.Chat.Providers)
	cfg.Chat.MaxContextChunks = 8
	prepared, plannerStep := server.expandAIQuestionForRetrieval(ctx, cfg, aiQuestionPreparation{
		SearchQuestion: "如何修改游戏售卖 inventory price 的基础价格",
		Scope: AIQuestionScope{
			RepoMode:   "global",
			SourceMode: "smart_latest_with_branch_candidates",
		},
	})
	if plannerStep.Status != "success" {
		t.Fatalf("query planner status = %s, want success: step=%+v", plannerStep.Status, plannerStep)
	}
	for _, forbidden := range []string{"updategameinventorystatus", "batchupdatesellergoodsskupricebytargets"} {
		if testStringSliceContains(prepared.GeneratedSearchTerms, forbidden) {
			t.Fatalf("query planner accepted unsupported concrete code term %q: step=%+v prepared=%+v", forbidden, plannerStep, prepared)
		}
	}
	for _, want := range []string{"inventory", "price", "handler"} {
		if !testStringSliceContains(prepared.GeneratedSearchTerms, want) {
			t.Fatalf("query planner filtered supported term %q: step=%+v prepared=%+v", want, plannerStep, prepared)
		}
	}
	retrieval, err := server.retrieveAIEvidence(ctx, prepared.SearchQuestion, AIQuestionScope{
		RepoMode:   "global",
		SourceMode: "smart_latest_with_branch_candidates",
	}, cfg)
	if err != nil {
		t.Fatalf("retrieve evidence: %v", err)
	}

	for _, evidence := range retrieval.Evidence {
		if evidence.Citation.FilePath == "core/handle/inventory.go" &&
			strings.Contains(evidence.Content, "UpdateGameInventoryStatus") &&
			strings.Contains(evidence.Content, "BatchUpdateSellerGoodsSKUPriceByTargets") {
			return
		}
	}
	t.Fatalf("retrieval missed inventory price update code: %+v", retrieval.Evidence)
}

func TestAIQueryPlannerFiltersUnsupportedConcreteTerms(t *testing.T) {
	frame := &aiTaskFrame{
		KnownTerms:     []string{"widget metric", "tenant_id"},
		GeneratedTerms: []string{"LoadWidgetMetric"},
	}
	terms := filterAIQueryPlannerTerms(parseAIQueryPlannerTerms(`{"terms":["UpdateGameInventoryStatus","BatchUpdateSellerGoodsSKUPriceByTargets","widget_metric","WidgetMetricRequest","handler","tenant_id","LoadWidgetMetric","request_response","response"]}`), "How does widget metric request use tenant_id?", frame)
	for _, forbidden := range []string{"updategameinventorystatus", "batchupdatesellergoodsskupricebytargets"} {
		if testStringSliceContains(terms, forbidden) {
			t.Fatalf("unsupported concrete code term %q was accepted: %v", forbidden, terms)
		}
	}
	for _, want := range []string{"widget_metric", "widgetmetricrequest", "handler", "tenant_id", "loadwidgetmetric", "request_response", "response"} {
		if !testStringSliceContains(terms, want) {
			t.Fatalf("supported planner term %q was filtered: %v", want, terms)
		}
	}
}

func TestAIDatabaseChangeRetrievalPrefersSchemaAndReadPathOverTestFixtures(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()

	noisyRepo := createTestGitRepo(t)
	writeScanPathTestFile(t, noisyRepo, "internal/app/ai_settings_test.go", `package app

`+strings.Repeat("// 数据库 修改 游戏 价格 update price sql 如何修改游戏售卖的基础价格\n", 50)+`
func TestNoisyGamePriceFixture(t *testing.T) {
	_ = "fake fixture only"
}
`)
	runTestGit(t, noisyRepo, "add", ".")
	runTestGit(t, noisyRepo, "commit", "-m", "add noisy ai fixture")
	noisy, err := createRepository(ctx, server.db, Repository{
		Name:                  "文档服务",
		Slug:                  "doc-harbor",
		RepoURL:               noisyRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"*"},
		LatestIncludeBranches: []string{"*"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create noisy repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, noisy.ID, "manual"); err != nil {
		t.Fatalf("scan noisy repository: %v", err)
	}

	businessRepo := createTestGitRepo(t)
	writeScanPathTestFile(t, businessRepo, "models/steam_game_price.go", `package models

func (SteamGamePrice) TableName() string {
	return "steam_game_price"
}

// SteamGamePrice steam游戏价格表
type SteamGamePrice struct {
	SteamAppID               int    `+"`"+`gorm:"column:steam_app_id" json:"steam_app_id"`+"`"+`
	CountryCode              string `+"`"+`gorm:"column:country_code" json:"country_code"`+"`"+`
	IsSell                   int    `+"`"+`gorm:"column:is_sell" json:"is_sell"`+"`"+`
	PackageID                int    `+"`"+`gorm:"column:package_id" json:"package_id"`+"`"+`
	PriceInCentsWithDiscount int    `+"`"+`gorm:"column:price_in_cents_with_discount" json:"price_in_cents_with_discount"`+"`"+`
	Type                     string `+"`"+`gorm:"column:type" json:"type"`+"`"+`
	BundleID                 int    `+"`"+`gorm:"column:bundle_id" json:"bundle_id"`+"`"+`
}
`)
	writeScanPathTestFile(t, businessRepo, "core/db/mysql_methods/steam_game_current_version_price.go", `package mysql_methods

func findCurrentSteamGameVersionPrice(versionType string, targetID int, countryCode string) {
	db := global.DB.
		Model(&models.SteamGamePrice{}).
		Where("country_code = ?", countryCode).
		Where("type = ?", versionType).
		Where("is_sell = ?", 1)

	switch versionType {
	case models.SteamGamePriceType.Game, models.SteamGamePriceType.DLC:
		db = db.Where("package_id = ?", targetID)
	case models.SteamGamePriceType.Bundle:
		db = db.Where("bundle_id = ?", targetID)
	}
	db.Order("last_modified DESC, id DESC").Limit(1).Find(&price)
}
`)
	writeScanPathTestFile(t, businessRepo, "version/mysql/v2.1.2.game.sql", `ALTER TABLE steam_game_price
    ADD INDEX idx_sgp_pkg_country_sell_mod_id (
    package_id,
    country_code,
    is_sell,
    last_modified,
    id
);
`)
	runTestGit(t, businessRepo, "add", ".")
	runTestGit(t, businessRepo, "commit", "-m", "add steam game price schema")
	business, err := createRepository(ctx, server.db, Repository{
		Name:                  "游戏交易服务",
		Slug:                  "game-trade",
		RepoURL:               businessRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"*"},
		LatestIncludeBranches: []string{"*"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create business repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, business.ID, "manual"); err != nil {
		t.Fatalf("scan business repository: %v", err)
	}

	cfg := defaultAIConfig()
	cfg.Chat.MaxContextChunks = 8
	retrieval, err := server.retrieveAIEvidence(ctx, "我想在数据库里直接修改游戏的价格 steam_game_prices SteamGamePrice steam_game_price price_in_cents_with_discount package_id country_code is_sell SELECT UPDATE", AIQuestionScope{
		RepoMode:   "global",
		SourceMode: "smart_latest_with_branch_candidates",
	}, cfg)
	if err != nil {
		t.Fatalf("retrieve evidence: %v", err)
	}
	if retrieval.Intent != "database_change" {
		t.Fatalf("intent = %q, want database_change", retrieval.Intent)
	}
	if len(retrieval.Evidence) == 0 {
		t.Fatal("retrieval returned no evidence")
	}
	if retrieval.EvidenceBundle == nil || retrieval.Coverage == nil {
		t.Fatalf("retrieval missing curator bundle or coverage: %+v", retrieval)
	}
	for i, evidence := range retrieval.Evidence {
		if evidence.Repo.Name == "文档服务" && aiPathLooksTest(evidence.Citation.FilePath) {
			t.Fatalf("test fixture remained as core evidence at %d: %+v", i, evidence)
		}
	}

	var foundModel, foundReadPath bool
	for _, evidence := range retrieval.Evidence {
		switch evidence.Citation.FilePath {
		case "models/steam_game_price.go":
			if strings.Contains(evidence.Content, "steam_game_price") && strings.Contains(evidence.Content, "price_in_cents_with_discount") {
				if evidence.EvidenceType != "orm_model" {
					t.Fatalf("model evidence type = %q, want orm_model: %+v", evidence.EvidenceType, evidence)
				}
				foundModel = true
			}
		case "core/db/mysql_methods/steam_game_current_version_price.go":
			if strings.Contains(evidence.Content, "country_code") && strings.Contains(evidence.Content, "package_id") && strings.Contains(evidence.Content, "is_sell") {
				if evidence.EvidenceType != "read_path" {
					t.Fatalf("read path evidence type = %q, want read_path: %+v", evidence.EvidenceType, evidence)
				}
				foundReadPath = true
			}
		}
	}
	if !foundModel || !foundReadPath {
		t.Fatalf("retrieval missed model/read-path evidence: model=%v readPath=%v evidence=%+v", foundModel, foundReadPath, retrieval.Evidence)
	}

	messages := buildAIChatMessages("我想在数据库里直接修改游戏的价格", retrieval)
	if !strings.Contains(messages[0].Content, "SELECT/UPDATE") || !strings.Contains(messages[0].Content, "不要仅因为证据里没有现成 UPDATE 语句") {
		t.Fatalf("system prompt should allow evidence-backed SQL examples: %s", messages[0].Content)
	}
}

func TestAIAgentRetrievalOrchestratorAddsSecondRoundForMissingSchemaEvidence(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()
	createAIRetrievalOrchestratorRepo(t, server, true)

	frame := aiTaskFrame{
		Intent:          aiTaskIntentDatabaseDirectUpdateForTest,
		UserGoal:        "confirm test-only direct data update evidence",
		AnswerShape:     "sql_steps_with_risk",
		ScopeStrategy:   "selected_repositories",
		TargetArtifacts: []string{"table", "orm_model", "update_fields", "read_path", "field_units"},
		KnownTerms:      []string{"loadwidgetmetric", "widgetmetric", "metric_cents", "tenant_id"},
	}
	contract := buildAIEvidenceContract(frame)
	cfg := defaultAIConfig()
	cfg.Chat.MaxContextChunks = 8
	retrieval, err := server.retrieveAIEvidenceWithTaskFrame(ctx, "LoadWidgetMetric metric_cents tenant_id", AIQuestionScope{
		RepoMode:   "global",
		SourceMode: "smart_latest",
	}, cfg, &frame, &contract)
	if err != nil {
		t.Fatalf("retrieve orchestrated evidence: %v", err)
	}
	if len(retrieval.Rounds) < 2 {
		t.Fatalf("retrieval rounds = %+v, want at least two rounds", retrieval.Rounds)
	}
	if got := retrieval.Rounds[0].CoverageDelta["read_path"]; !strings.Contains(got, "covered") {
		t.Fatalf("round 1 should cover read_path, delta=%+v", retrieval.Rounds[0].CoverageDelta)
	}
	if got := retrieval.Rounds[0].CoverageDelta["table_identity"]; !strings.Contains(got, "missing") {
		t.Fatalf("round 1 should still miss table_identity, delta=%+v", retrieval.Rounds[0].CoverageDelta)
	}
	if retrieval.Rounds[1].NewEvidenceCount <= 0 {
		t.Fatalf("round 2 should add evidence: %+v", retrieval.Rounds[1])
	}
	if got := retrieval.Rounds[1].CoverageDelta["table_identity"]; !strings.Contains(got, "covered") {
		t.Fatalf("round 2 should cover table_identity, delta=%+v", retrieval.Rounds[1].CoverageDelta)
	}
	if retrieval.ContractCoverage == nil || retrieval.ContractCoverage.Coverage["table_identity"] != aiEvidenceCoverageCovered || retrieval.ContractCoverage.Coverage["field_units"] != aiEvidenceCoverageCovered {
		t.Fatalf("final coverage did not include schema/field-unit evidence: %+v", retrieval.ContractCoverage)
	}
	var foundModelOrMigration bool
	for _, evidence := range retrieval.Evidence {
		switch evidence.EvidenceType {
		case "orm_model", "migration_sql":
			foundModelOrMigration = true
		}
	}
	if !foundModelOrMigration {
		t.Fatalf("orchestrator did not retain ORM or migration evidence: %+v", retrieval.Evidence)
	}
	for _, search := range retrieval.Rounds[1].Searches {
		for _, hint := range search.PathHints {
			if !testStringSliceContains([]string{"models", "migration", "db", "router", "proto", "handler", "client", "docs"}, hint) {
				t.Fatalf("round 2 used non-generic path hint %q in search %+v", hint, search)
			}
		}
	}
	if len(retrieval.RetrievalRoundSteps) != len(retrieval.Rounds) {
		t.Fatalf("round steps = %d, rounds = %d", len(retrieval.RetrievalRoundSteps), len(retrieval.Rounds))
	}
	for _, step := range retrieval.RetrievalRoundSteps {
		if step.StepType != "retrieval_round" || !strings.Contains(step.OutputJSON, `"coverage_delta"`) || !strings.Contains(step.OutputJSON, `"new_evidence_count"`) || !strings.Contains(step.OutputJSON, `"searches"`) {
			t.Fatalf("retrieval round step missing diagnostics fields: %+v", step)
		}
	}
}

func TestAIAgentRetrievalOrchestratorCompletedWithGapsDiagnostics(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()
	createAIRetrievalOrchestratorRepo(t, server, false)
	session, err := createAISession(ctx, server.db, "orchestrator gaps", "", AIQuestionScope{RepoMode: "global"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	recorder := doJSON(t, server, http.MethodPost, "/api/ai/sessions/"+strconv.FormatInt(session.ID, 10)+"/messages", map[string]any{
		"question": "我想在数据库里直接修改 widget metric_cents，读取链路是 LoadWidgetMetric，WHERE 需要 tenant_id 和 widget_id",
		"scope_override": AIQuestionScope{
			RepoMode:   "global",
			SourceMode: "smart_latest",
		},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("question status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var result aiQuestionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode question response: %v", err)
	}
	if result.Run.Status != aiWorkflowStatusCompletedWithGaps || result.Run.VerificationStatus != aiWorkflowStatusCompletedWithGaps {
		t.Fatalf("run status = %s/%s, want completed_with_gaps; report=%s; answer=%s", result.Run.Status, result.Run.VerificationStatus, result.Run.VerificationReportJSON, result.Message.Content)
	}
	if strings.Contains(result.Message.Content, "UPDATE ") {
		t.Fatalf("completed_with_gaps local answer should not include deterministic UPDATE: %s", result.Message.Content)
	}
	checkpoint := decodeAICheckpointForTest(t, result.Run.CheckpointJSON)
	coverage, ok := checkpoint["contract_coverage"].(map[string]any)
	if !ok || coverage["status"] != aiWorkflowStatusCompletedWithGaps || coverage["next_action"] != aiWorkflowStatusCompletedWithGaps {
		t.Fatalf("checkpoint coverage missing completed_with_gaps: %+v", checkpoint["contract_coverage"])
	}
	if !strings.Contains(result.Run.VerificationReportJSON, `"workflow_status":"completed_with_gaps"`) {
		t.Fatalf("verification report missing completed_with_gaps: %s", result.Run.VerificationReportJSON)
	}
	steps, err := listAIRunSteps(ctx, server.db, result.Run.ID)
	if err != nil {
		t.Fatalf("list run steps: %v", err)
	}
	var roundSteps int
	for _, step := range steps {
		if step.StepType != "retrieval_round" {
			continue
		}
		roundSteps++
		if !strings.Contains(step.OutputJSON, `"reason"`) || !strings.Contains(step.OutputJSON, `"searches"`) || !strings.Contains(step.OutputJSON, `"coverage_delta"`) {
			t.Fatalf("retrieval_round step missing output detail: %s", step.OutputJSON)
		}
	}
	if roundSteps != aiRetrievalMaxRounds {
		t.Fatalf("retrieval_round step count = %d, want %d", roundSteps, aiRetrievalMaxRounds)
	}

	diagnosticsToken := issueAccessTokenForTest(t, server, accessTokenScope{}, time.Hour, accessTokenCapabilityAIDiagnosticsRead)
	detail := doAuthorizedJSON(t, server, http.MethodGet, "/api/access/ai/diagnostics/runs/"+strconv.FormatInt(result.Run.ID, 10), diagnosticsToken, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("diagnostics detail status = %d, want %d; body=%s", detail.Code, http.StatusOK, detail.Body.String())
	}
	var detailResp aiDiagnosticsRunDetailResponse
	if err := json.Unmarshal(detail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("decode diagnostics detail: %v", err)
	}
	var foundRoundSummary bool
	for _, step := range detailResp.Steps {
		if step.StepType != "retrieval_round" {
			continue
		}
		summaryJSON, err := json.Marshal(step.Summary)
		if err != nil {
			t.Fatalf("marshal retrieval round summary: %v", err)
		}
		summary := string(summaryJSON)
		if strings.Contains(summary, `"reason"`) && strings.Contains(summary, `"searches"`) && strings.Contains(summary, `"coverage_delta"`) {
			foundRoundSummary = true
		}
	}
	if !foundRoundSummary {
		t.Fatalf("diagnostics detail missing retrieval_round summary: %+v", detailResp.Steps)
	}
}

func TestAIAnswerVerificationRewriteAndSecretFallback(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	rewriteServer := newAIAnswerModelTestServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"表 steam_game_price、字段 price_in_cents_with_discount 和 WHERE id 来自证据 [C1]。\nUPDATE steam_game_price SET price_in_cents_with_discount = ? WHERE id = ?; [C1]"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	secret, err := server.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: "rewrite-api-key", SecretType: "api_key", Value: "sk-test-rewrite-secret-1234"}, "test")
	if err != nil {
		t.Fatalf("create rewrite secret: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = []AIProvider{{
		ProviderKey:           "rewrite-main",
		Name:                  "RewriteProvider",
		Priority:              10,
		ProviderType:          "openai_compatible",
		BaseURL:               rewriteServer.URL,
		Model:                 "rewrite-model",
		APIKeySecretID:        secret.ID,
		CostTier:              "low",
		RequestTimeoutSeconds: 5,
		MaxRPM:                60,
	}}
	cfg.Chat.Routing = buildDefaultRouting(ctx, server.db, cfg.Chat.Providers)
	active := AIConfigVersion{Version: 1, ConfigHash: "rewrite-test", Config: cfg}

	frame := aiTaskFrame{Intent: aiTaskIntentDatabaseDirectUpdateForTest, UserGoal: "测试环境直接改价"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	retrieval := aiAnswerComposerRetrievalForTest(frame, contract, coverage, []aiEvidence{{
		Repo: Repository{Name: "game-service"},
		Citation: AIMessageCitation{
			RepoID:      1,
			SourceScope: "smart_latest",
			Branch:      "main",
			CommitSHA:   "abc123",
			FilePath:    "models/steam_game_price.go",
			LineStart:   1,
			LineEnd:     20,
		},
		Content:           "func (SteamGamePrice) TableName() string { return \"steam_game_price\" }\nPriceInCentsWithDiscount int `gorm:\"column:price_in_cents_with_discount\"`\nfunc Find(id int64) { db.Where(\"id = ?\", id).Find(&price) }",
		EvidenceType:      "orm_model",
		SourceReliability: aiEvidenceReliabilityHighSmartLatest,
		ContractKeys:      []string{"table_identity", "update_fields", "where_conditions"},
	}})
	retrieval.Rounds = []aiRetrievalRoundPlan{{Round: aiRetrievalMaxRounds}}

	outcome := server.verifyAndMaybeRewriteAIAnswer(ctx, active, "怎么直接修改测试价格？", retrieval, aiModelResult{
		Content:      "没有官方 UPDATE 示例，无法提供 SQL。[C1]",
		ProviderName: "DraftProvider",
		Model:        "draft-model",
	}, time.Now())
	if outcome.Report.Status != aiAnswerVerificationStatusPass || !outcome.Report.RewriteAttempted {
		t.Fatalf("rewrite outcome report = %+v content=%s", outcome.Report, outcome.Result.Content)
	}
	if !strings.Contains(outcome.Result.Content, "UPDATE steam_game_price SET price_in_cents_with_discount = ? WHERE id = ?") {
		t.Fatalf("rewrite did not keep verified SQL placeholders: %s", outcome.Result.Content)
	}

	secretOutcome := server.verifyAndMaybeRewriteAIAnswer(ctx, active, "怎么直接修改测试价格？", retrieval, aiModelResult{
		Content:      "供应商错误 api_key=raw-answer-api-key API key: raw-answer-spaced-key token=raw-answer-token secret raw-answer-secret Authorization: Basic cmF3LWFuc3dlcg== [C1]",
		ProviderName: "DraftProvider",
		Model:        "draft-model",
		FailoverJSON: `{"error":"api_key=raw-answer-api-key token=raw-answer-token secret raw-answer-secret Authorization: Basic cmF3LWFuc3dlcg=="}`,
	}, time.Now())
	reportJSON := encodeJSON(secretOutcome.Report)
	if secretOutcome.Report.NextAction != aiAnswerVerificationNextActionBlockAnswer || secretOutcome.Result.ProviderName != "local-verifier" {
		t.Fatalf("secret fallback outcome = %+v result=%+v", secretOutcome.Report, secretOutcome.Result)
	}
	for _, forbidden := range []string{"raw-answer-api-key", "raw-answer-spaced-key", "raw-answer-token", "raw-answer-secret", "cmF3LWFuc3dlcg"} {
		if strings.Contains(secretOutcome.Result.Content, forbidden) || strings.Contains(reportJSON, forbidden) {
			t.Fatalf("secret leaked after verifier fallback: forbidden=%s report=%s content=%s", forbidden, reportJSON, secretOutcome.Result.Content)
		}
	}

	failingRewriteServer := newAIAnswerModelTestServer(t, http.StatusTooManyRequests, `{"error":"api_key=raw-rewrite-api-key API key: raw-rewrite-spaced-key token=raw-rewrite-token secret raw-rewrite-secret Authorization: Basic cmF3LXJld3JpdGU="}`)
	failCfg := defaultAIConfig()
	failCfg.Enabled = true
	failCfg.Chat.Providers = []AIProvider{{
		ProviderKey:           "rewrite-failing",
		Name:                  "RewriteFailingProvider",
		Priority:              10,
		ProviderType:          "openai_compatible",
		BaseURL:               failingRewriteServer.URL,
		Model:                 "rewrite-failing-model",
		APIKeySecretID:        secret.ID,
		CostTier:              "low",
		RequestTimeoutSeconds: 5,
		MaxRPM:                60,
	}}
	failCfg.Chat.Routing = buildDefaultRouting(ctx, server.db, failCfg.Chat.Providers)
	rewriteFailure := server.verifyAndMaybeRewriteAIAnswer(ctx, AIConfigVersion{Version: 1, ConfigHash: "rewrite-failure-test", Config: failCfg}, "怎么直接修改测试价格？", retrieval, aiModelResult{
		Content:      "没有官方 UPDATE 示例，无法提供 SQL。[C1]",
		ProviderName: "DraftProvider",
		Model:        "draft-model",
	}, time.Now())
	rewriteFailureReport := encodeJSON(rewriteFailure.Report)
	if rewriteFailure.Report.Status != aiAnswerVerificationStatusVerificationFailed || !rewriteFailure.Report.RewriteAttempted || rewriteFailure.Result.ProviderName != "local-verifier" {
		t.Fatalf("rewrite failure outcome = %+v result=%+v", rewriteFailure.Report, rewriteFailure.Result)
	}
	for _, forbidden := range []string{"raw-rewrite-api-key", "raw-rewrite-spaced-key", "raw-rewrite-token", "raw-rewrite-secret", "cmF3LXJld3JpdGU"} {
		if strings.Contains(rewriteFailureReport, forbidden) || strings.Contains(rewriteFailure.Result.Content, forbidden) {
			t.Fatalf("rewrite failure leaked %s: report=%s content=%s", forbidden, rewriteFailureReport, rewriteFailure.Result.Content)
		}
	}
}

func TestAIRetrievalQueryPlannerFailureFallsBackToDeterministicTerms(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()
	repoDir := createTestGitRepo(t)
	writeScanPathTestFile(t, repoDir, "docs/widget-notes.md", "# Widget Notes\n\nAlphaWidget supports deterministic lookup through LoadWidgetMetric.\n")
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "add widget notes")
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "widget-docs",
		Slug:                  "widget-docs",
		RepoURL:               repoDir,
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

	plannerServer := newAIAnswerModelTestServer(t, http.StatusTooManyRequests, `{"error":"rate limit sk-test-query-planner-fail-12345678"}`)
	secret, err := server.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: "failing-query-planner-key", SecretType: "api_key", Value: "sk-test-query-planner-fail"}, "test")
	if err != nil {
		t.Fatalf("create planner secret: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.Providers = []AIProvider{{
		ProviderKey:           "query-planner",
		Name:                  "QueryPlanner",
		Priority:              10,
		ProviderType:          "openai_compatible",
		BaseURL:               plannerServer.URL,
		Model:                 "query-planner-model",
		APIKeySecretID:        secret.ID,
		CostTier:              "low",
		RequestTimeoutSeconds: 5,
		MaxRPM:                60,
	}}
	cfg.Chat.Routing = buildDefaultRouting(ctx, server.db, cfg.Chat.Providers)
	prepared, plannerStep := server.expandAIQuestionForRetrieval(ctx, cfg, aiQuestionPreparation{
		SearchQuestion: "AlphaWidget LoadWidgetMetric lookup",
		Scope: AIQuestionScope{
			RepoMode:   "global",
			SourceMode: "smart_latest",
		},
	})
	if plannerStep.Status != "failed" {
		t.Fatalf("planner step status = %s, want failed", plannerStep.Status)
	}
	if strings.Contains(plannerStep.ErrorMessage, "sk-test-query-planner-fail") {
		t.Fatalf("planner error leaked API key: %s", plannerStep.ErrorMessage)
	}
	retrieval, err := server.retrieveAIEvidence(ctx, prepared.SearchQuestion, prepared.Scope, cfg)
	if err != nil {
		t.Fatalf("retrieval after planner failure: %v", err)
	}
	if len(retrieval.Evidence) == 0 || len(retrieval.Rounds) == 0 {
		t.Fatalf("deterministic fallback retrieval found no evidence: %+v", retrieval)
	}
	if retrieval.Rounds[0].PlannerStatus != "deterministic" || !strings.Contains(retrieval.Rounds[0].QuerySource, "task_frame_known_terms") {
		t.Fatalf("round 1 did not record deterministic fallback source: %+v", retrieval.Rounds[0])
	}
}

func createAIRetrievalOrchestratorRepo(t *testing.T, server *Server, includeSchema bool) Repository {
	t.Helper()
	ctx := context.Background()
	repoDir := createTestGitRepo(t)
	writeScanPathTestFile(t, repoDir, "internal/db/widget_metric_reader.go", `package db

type WidgetMetric struct{}

func LoadWidgetMetric(tenantID, widgetID int64) {
	db.Model(&WidgetMetric{}).
		Where("tenant_id = ?", tenantID).
		Where("widget_id = ?", widgetID).
		First(&WidgetMetric{})
}
`)
	if includeSchema {
		writeScanPathTestFile(t, repoDir, "models/widget_metric.go", `package models

func (WidgetMetric) TableName() string {
	return "widget_metric"
}

type WidgetMetric struct {
	TenantID    int64 `+"`"+`gorm:"column:tenant_id"`+"`"+`
	WidgetID    int64 `+"`"+`gorm:"column:widget_id"`+"`"+`
	MetricCents int64 `+"`"+`gorm:"column:metric_cents"`+"`"+` // stored in cents
}
`)
		writeScanPathTestFile(t, repoDir, "migrations/001_widget_metric.sql", `CREATE TABLE widget_metric (
    tenant_id BIGINT NOT NULL,
    widget_id BIGINT NOT NULL,
    metric_cents BIGINT NOT NULL COMMENT 'stored in cents',
    UNIQUE KEY uk_widget_metric (tenant_id, widget_id)
);
`)
	}
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "add widget metric evidence")
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "widget-service",
		Slug:                  "widget-service",
		RepoURL:               repoDir,
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

	priceTerms := strings.Join(aiQueryTerms("如何修改游戏售卖的基础价格"), ",")
	for _, forbidden := range []string{"update", "price", "base", "游戏服务", "通用商户"} {
		if strings.Contains(priceTerms, forbidden) {
			t.Fatalf("terms should not contain static semantic aliases or business dictionary entries: %q", priceTerms)
		}
	}
}

func TestAIQueryTermsSplitsMixedChineseAndIdentifierTokens(t *testing.T) {
	terms := strings.Join(aiQueryTerms("alpha模块签发的token格式是什么样的？"), ",")
	for _, want := range []string{"alpha", "签发", "token", "格式"} {
		if !strings.Contains(terms, want) {
			t.Fatalf("terms %q missing %q", terms, want)
		}
	}
	tableTerms := strings.Join(aiQueryTerms("steam_game_prices 表怎么 update？"), ",")
	for _, want := range []string{"steam_game_prices", "steam_game_price", "prices", "price"} {
		if !strings.Contains(tableTerms, want) {
			t.Fatalf("table terms %q missing %q", tableTerms, want)
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
	parameterFollowUp, err := server.prepareAIQuestion(ctx, session.ID, "参数是什么？", AIQuestionScope{
		RepoMode:   "global",
		SourceMode: "smart_latest_with_branch_candidates",
		FileTypes:  []string{"all"},
	})
	if err != nil {
		t.Fatalf("prepare parameter follow-up: %v", err)
	}
	followFrame, _ := server.frameAITask(ctx, defaultAIConfig(), "参数是什么？", parameterFollowUp)
	if followFrame.FollowUp == nil || strings.Join(followFrame.FollowUp.PreviousPaths, ",") != "src/security/session_token.ts" {
		t.Fatalf("follow-up task frame did not inherit previous path: %+v", followFrame)
	}
	if !strings.Contains(followFrame.FollowUp.PreviousTopicSummary, "session_token.ts") || !strings.Contains(followFrame.FollowUp.PreviousTopicSummary, "token") {
		t.Fatalf("follow-up task frame did not inherit topic summary: %+v", followFrame.FollowUp)
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
	failingProvider := newAIStreamHTTPErrorTestServer(t, http.StatusTooManyRequests, `{"error":"rate limit sk-test-stream-leak-12345678 api_key=raw-stream-api-key API key: raw-stream-spaced-key token=raw-stream-token secret raw-stream-secret Authorization: Basic cmF3LXN0cmVhbQ=="}`)
	successProvider := newAIStreamModelTestServer(t, []string{"第一段 [C1]", "第二段 [C1]"}, true)
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
	for _, event := range []string{"event: user_message", "event: run_started", "event: task_frame", "event: contract", "event: stage", "event: provider_attempt", "event: verification", "event: answer_delta", "event: message_done", "event: done"} {
		if !strings.Contains(body, event) {
			t.Fatalf("stream body missing %s: %s", event, body)
		}
	}
	if strings.Index(body, "event: answer_delta") < strings.Index(body, "event: verification") {
		t.Fatalf("answer_delta was sent before verification: %s", body)
	}
	if !strings.Contains(body, `"contract_id":"document_qa.v1"`) || !strings.Contains(body, `"required_keys"`) {
		t.Fatalf("stream body missing contract summary: %s", body)
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
	for _, forbidden := range []string{"raw-stream-api-key", "raw-stream-spaced-key", "raw-stream-token", "raw-stream-secret", "cmF3LXN0cmVhbQ"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("stream response leaked provider failure secret %s: %s", forbidden, body)
		}
	}
	if strings.Contains(body, "第一段 [C1]") || strings.Contains(body, "第二段 [C1]") {
		t.Fatalf("stream response exposed pre-verification model chunks: %s", body)
	}
	var content, status, provider, model string
	if err := server.db.QueryRowContext(ctx, `SELECT content, status, provider_name, model
		FROM ai_messages WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`, session.ID).
		Scan(&content, &status, &provider, &model); err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if !strings.Contains(content, "DocHarbor 后端根据只读 Git 检索得到的候选证据摘要") || status != "success" || provider != "local-verifier" || model != "none" {
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
	if strings.Contains(body, "event: error") || strings.Contains(body, `"partial_message_id"`) {
		t.Fatalf("stream should not expose partial model error after buffered delta: %s", body)
	}
	if !strings.Contains(body, "event: verification") || !strings.Contains(body, "event: answer_delta") {
		t.Fatalf("stream should release verified fallback answer: %s", body)
	}
	if strings.Contains(body, `"provider_key":"openai-main"`) || strings.Contains(body, "不应出现") || strings.Contains(body, "部分回答") {
		t.Fatalf("stream should not expose failed partial delta or fail over after first buffered delta: %s", body)
	}
	var content, status, errorMessage string
	if err := server.db.QueryRowContext(ctx, `SELECT content, status, error_message
		FROM ai_messages WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`, session.ID).
		Scan(&content, &status, &errorMessage); err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if strings.Contains(content, "部分回答") || status != "success" || errorMessage != "" {
		t.Fatalf("assistant fallback message = content=%q status=%q error=%q", content, status, errorMessage)
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
		if event == "provider_attempt" {
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
	if content != "" || status != "failed" || errorMessage == "" {
		t.Fatalf("assistant canceled buffered stream = content=%q status=%q error=%q", content, status, errorMessage)
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
		payload := requireAIModelRequest(t, r)
		if payload["stream"] != true {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"terms\":[]}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
			return
		}
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
		requireAIModelRequest(t, r)
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
		payload := requireAIModelRequest(t, r)
		if payload["stream"] != true {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"terms\":[]}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
			return
		}
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
	payload := requireAIModelRequest(t, r)
	if payload["stream"] != true {
		t.Fatalf("stream = %v, want true", payload["stream"])
	}
}

func requireAIModelRequest(t *testing.T, r *http.Request) map[string]any {
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
	if payload["model"] == "" {
		t.Fatalf("model is required in model request")
	}
	return payload
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

func issueAccessTokenForTest(t *testing.T, server *Server, scope accessTokenScope, ttl time.Duration, capabilities ...string) string {
	t.Helper()
	now := time.Now().UTC()
	if len(capabilities) == 0 {
		capabilities = []string{accessTokenCapabilityAIHistoryRead}
	}
	token, err := server.signAccessToken(accessTokenPayload{
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(ttl).Unix(),
		Capabilities: capabilities,
		Scope:        scope,
		JTI:          "test-token",
	})
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	return token
}

func insertDiagnosticsRunForTest(t *testing.T, server *Server, sessionID, userMessageID, assistantMessageID int64, status, startedAt, finishedAt string, scopes ...AIQuestionScope) int64 {
	t.Helper()
	scopeJSON := "{}"
	if len(scopes) > 0 {
		scopeJSON = encodeJSON(normalizeAIScope(scopes[0]))
	}
	res, err := server.db.ExecContext(context.Background(), `INSERT INTO ai_agent_runs
		(session_id, user_message_id, assistant_message_id, status, current_state, intent, scope_json,
		 retrieval_plan_json, service_candidate_count, evidence_count, code_evidence_count, verification_status,
		 verification_report_json, checkpoint_json, index_snapshot_id, config_version, config_hash, model,
		 provider_name, provider_failover_json, model_route_json, escalation_count, estimated_cost_json,
		 started_at, finished_at, error_message)
		VALUES (?, ?, ?, ?, 'verify_answer', 'diagnostic', ?, '{"plan":"diag"}', 1, 1, 1, 'fail',
		 '{"ok":false}', '{"checkpoint":"diag"}', 7, 3, 'diag-hash', 'diag-model', 'DiagProvider',
		 '{"attempts":[{"provider":"DiagProvider","status":"failed"}]}', '{"route":"diag"}', 1,
		 '{"usd":0.001}', ?, ?, 'diagnostic failure')`,
		sessionID, userMessageID, assistantMessageID, status, scopeJSON, startedAt, finishedAt)
	if err != nil {
		t.Fatalf("insert diagnostics run: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("diagnostics run id: %v", err)
	}
	return id
}

func decodeAICheckpointForTest(t *testing.T, raw string) map[string]any {
	t.Helper()
	var checkpoint map[string]any
	if err := json.Unmarshal([]byte(raw), &checkpoint); err != nil {
		t.Fatalf("decode ai checkpoint %q: %v", raw, err)
	}
	return checkpoint
}

func testStringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func countAISecrets(t *testing.T, server *Server) int {
	t.Helper()
	var count int
	if err := server.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ai_secrets`).Scan(&count); err != nil {
		t.Fatalf("count ai secrets: %v", err)
	}
	return count
}
