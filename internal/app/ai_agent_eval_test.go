package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type aiAgentEvalCase struct {
	Name                      string          `json:"name"`
	Question                  string          `json:"question"`
	Scope                     AIQuestionScope `json:"scope"`
	Conversation              aiConversationContext
	ExpectedIntent            string   `json:"expected_intent"`
	ExpectedRequiredKeys      []string `json:"expected_required_keys"`
	ExpectedCoveredKeys       []string `json:"expected_covered_keys"`
	ExpectedMissingKeys       []string `json:"expected_missing_keys,omitempty"`
	ExpectedAnswerPatterns    []string `json:"expected_answer_patterns,omitempty"`
	ForbiddenAnswerPatterns   []string `json:"forbidden_answer_patterns"`
	ExpectedDiagnosticsFields []string `json:"expected_diagnostics_fields"`
	ExpectExcludedTestFixture bool
	ExpectPlaceholderSQL      bool
	ExpectBranchCandidate     bool
	ExpectFollowUpContext     bool
}

func TestAIAgentEvalQualityGate(t *testing.T) {
	requireGit(t)

	server := newWebhookTestServer(t)
	ctx := context.Background()

	databaseMissingRepo := createAIAgentEvalDatabaseRepo(t, server, false)
	databaseReadyRepo := createAIAgentEvalDatabaseRepo(t, server, true)
	apiRepo := createAIAgentEvalAPIAndBranchRepo(t, server)
	modelServer := newAIAgentEvalMockProvider(t)
	modelCfg := newAIAgentEvalMockConfig(t, server, modelServer.URL)
	retrievalCfg := defaultAIConfig()
	retrievalCfg.Chat.MaxContextChunks = 12

	databaseRequired := []string{"table_identity", "update_fields", "field_units", "where_conditions", "read_path", "verification_method", "side_effects"}
	apiRequired := []string{"service_candidate", "route_or_rpc", "request_fields", "response_fields", "error_codes", "branch_status"}
	branchRequired := []string{"branch_candidates", "source_scope", "commit_evidence", "default_branch_baseline"}
	diagnosticsRequired := []string{"run_identity", "run_steps", "retrieval_plan", "citations", "provider_failover", "checkpoint"}
	commonDiagnostics := []string{"task_frame.intent", "evidence_contract.required_keys", "retrieval_rounds", "contract_coverage.coverage", "verification_report.status"}

	cases := []aiAgentEvalCase{
		{
			Name:                      "database_missing_field_units_blocks_update",
			Question:                  "我想在数据库里直接修改 widget_metric 的 metric_value 用于测试，读取链路是 LoadWidgetMetric，WHERE 用 tenant_id 和 widget_id",
			Scope:                     aiEvalSelectedScope(databaseMissingRepo.ID),
			ExpectedIntent:            aiTaskIntentDatabaseDirectUpdateForTest,
			ExpectedRequiredKeys:      databaseRequired,
			ExpectedCoveredKeys:       []string{"table_identity", "update_fields", "where_conditions", "read_path", "verification_method", "side_effects"},
			ExpectedMissingKeys:       []string{"field_units"},
			ForbiddenAnswerPatterns:   []string{`(?i)\bupdate\s+[a-z0-9_]+\s+set\b`},
			ExpectedDiagnosticsFields: commonDiagnostics,
			ExpectExcludedTestFixture: true,
		},
		{
			Name:                      "database_sufficient_evidence_allows_placeholder_sql",
			Question:                  "我想在数据库里直接修改 widget_metric 的 metric_cents 用于测试，读取链路是 LoadWidgetMetric，WHERE 用 tenant_id 和 widget_id",
			Scope:                     aiEvalSelectedScope(databaseReadyRepo.ID),
			ExpectedIntent:            aiTaskIntentDatabaseDirectUpdateForTest,
			ExpectedRequiredKeys:      databaseRequired,
			ExpectedCoveredKeys:       databaseRequired,
			ExpectedAnswerPatterns:    []string{`(?i)UPDATE\s+widget_metric\s+SET\s+metric_cents\s*=\s*\?\s+WHERE\s+tenant_id\s*=\s*\?\s+AND\s+widget_id\s*=\s*\?`},
			ForbiddenAnswerPatterns:   []string{`(?i)SET\s+metric_cents\s*=\s*[0-9]+`, `(?i)WHERE\s+tenant_id\s*=\s*[0-9]+`},
			ExpectedDiagnosticsFields: commonDiagnostics,
			ExpectExcludedTestFixture: true,
			ExpectPlaceholderSQL:      true,
		},
		{
			Name:                      "api_integration_requires_route_request_response_citations",
			Question:                  "下单页面需要接 /api/widget/orders 哪些接口？请求参数和返回字段是什么？",
			Scope:                     aiEvalSelectedScope(apiRepo.ID),
			ExpectedIntent:            aiTaskIntentAPIIntegration,
			ExpectedRequiredKeys:      apiRequired,
			ExpectedCoveredKeys:       []string{"route_or_rpc", "request_fields", "response_fields"},
			ExpectedAnswerPatterns:    []string{`/api/widget/orders[^\n]*\[C[0-9]+\]`, `tenant_id[^\n]*\[C[0-9]+\]`, `order_id[^\n]*\[C[0-9]+\]`},
			ForbiddenAnswerPatterns:   []string{`(?i)invented`, `未引用字段`},
			ExpectedDiagnosticsFields: commonDiagnostics,
		},
		{
			Name:                      "branch_lookup_displays_branch_commit_and_source_scope",
			Question:                  "QuantumDelivery 新接口现在在哪个分支？",
			Scope:                     aiEvalSelectedBranchScope(apiRepo.ID),
			ExpectedIntent:            aiTaskIntentBranchLookup,
			ExpectedRequiredKeys:      branchRequired,
			ExpectedCoveredKeys:       []string{"branch_candidates", "source_scope", "commit_evidence"},
			ExpectedAnswerPatterns:    []string{`功能分支候选[^\n]*feat/quantum-delivery[^\n]*\[C[0-9]+\]`},
			ForbiddenAnswerPatterns:   []string{`已上线`, `已合入`, `当前最新事实`},
			ExpectedDiagnosticsFields: commonDiagnostics,
			ExpectBranchCandidate:     true,
		},
		{
			Name:     "follow_up_context_keeps_previous_api_scope",
			Question: "参数是什么？",
			Scope:    aiEvalSelectedScope(apiRepo.ID),
			Conversation: aiConversationContext{
				FollowUp:                 true,
				PreviousUserQuestion:     "下单页面需要接 /api/widget/orders 哪些接口？",
				PreviousAssistantSummary: "上一轮围绕 widget order 接口给出了路由、请求和响应字段。",
				PreviousCitationPaths:    []string{"router/widget_order.go"},
				FocusRepoIDs:             []int64{apiRepo.ID},
			},
			ExpectedIntent:            aiTaskIntentAPIIntegration,
			ExpectedRequiredKeys:      apiRequired,
			ExpectedCoveredKeys:       []string{"route_or_rpc", "request_fields", "response_fields"},
			ExpectedAnswerPatterns:    []string{`tenant_id[^\n]*\[C[0-9]+\]`, `sku_id[^\n]*\[C[0-9]+\]`, `order_id[^\n]*\[C[0-9]+\]`},
			ForbiddenAnswerPatterns:   []string{`其他服务`, `无关仓库`},
			ExpectedDiagnosticsFields: append(commonDiagnostics, "task_frame.follow_up.previous_paths"),
			ExpectFollowUpContext:     true,
		},
		{
			Name:                      "diagnostics_question_frames_replay_inputs",
			Question:                  "为什么上次回答错了？请排查检索、Coverage 和 verifier 结果",
			Scope:                     aiEvalSelectedScope(apiRepo.ID),
			ExpectedIntent:            aiTaskIntentDiagnostics,
			ExpectedRequiredKeys:      diagnosticsRequired,
			ExpectedCoveredKeys:       nil,
			ForbiddenAnswerPatterns:   []string{`(?i)api[_ -]?key`, `Authorization`, `Bearer`},
			ExpectedDiagnosticsFields: []string{"task_frame.intent", "evidence_contract.required_keys", "retrieval_rounds", "contract_coverage.missing_required", "verification_report.next_action"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			prepared := aiQuestionPreparation{
				SearchQuestion: tc.Question,
				Scope:          tc.Scope,
				Conversation:   tc.Conversation,
			}
			frame, frameStep := server.frameAITask(ctx, defaultAIConfig(), tc.Question, prepared)
			if frameStep.Status != "success" {
				t.Fatalf("task frame step status = %s: %+v", frameStep.Status, frameStep)
			}
			if frame.Intent != tc.ExpectedIntent {
				t.Fatalf("intent = %q, want %q; frame=%+v", frame.Intent, tc.ExpectedIntent, frame)
			}
			if tc.ExpectFollowUpContext && (frame.FollowUp == nil || !frame.FollowUp.IsFollowUp || len(frame.FollowUp.PreviousPaths) == 0) {
				t.Fatalf("follow-up context missing from task frame: %+v", frame)
			}

			contract := buildAIEvidenceContract(frame)
			assertAIStringSetContains(t, "required contract keys", aiEvidenceRequirementKeys(contract.Required), tc.ExpectedRequiredKeys)

			retrieval, err := server.retrieveAIEvidenceWithTaskFrame(ctx, tc.Question, tc.Scope, retrievalCfg, &frame, &contract)
			if err != nil {
				t.Fatalf("retrieve evidence: %v", err)
			}
			if retrieval.ContractCoverage == nil {
				t.Fatalf("retrieval missing contract coverage: %+v", retrieval)
			}
			assertAICoverageContains(t, *retrieval.ContractCoverage, tc.ExpectedCoveredKeys)
			assertAIMissingCoverageContains(t, *retrieval.ContractCoverage, tc.ExpectedMissingKeys)

			if tc.ExpectExcludedTestFixture {
				assertAIEvalExcludedTestFixture(t, retrieval)
			}
			if tc.ExpectBranchCandidate {
				assertAIEvalBranchCandidateDisplayed(t, tc.Question, retrieval)
			}

			composer := prepareAIAnswerComposer(tc.Question, &retrieval)
			result, err := server.callRoutedAIModel(ctx, modelCfg, tc.Question, retrieval)
			if err != nil {
				t.Fatalf("mock model call: %v", err)
			}
			if result.ProviderName != "EvalMock" || result.Model != "eval-model" {
				t.Fatalf("unexpected mock provider result: %+v", result)
			}
			assertAITextMatches(t, "answer", result.Content, tc.ExpectedAnswerPatterns)
			assertAITextDoesNotMatch(t, "answer", result.Content, tc.ForbiddenAnswerPatterns)

			report := verifyAIAnswerWithContext(composer.Frame, composer.Contract, composer.Coverage, composer.Bundle, result.Content, aiAnswerVerificationContext{
				EvidenceCount:  len(retrieval.Evidence),
				RetrievalRound: len(retrieval.Rounds),
				ServiceNames:   aiVerificationServiceNames(retrieval.ServiceCandidates),
			})
			if report.Status == aiAnswerVerificationStatusFailed || report.Status == aiAnswerVerificationStatusVerificationFailed {
				t.Fatalf("verification failed: report=%+v answer=%s", report, result.Content)
			}
			if tc.ExpectPlaceholderSQL && !testStringSliceContains(report.PassedChecks, "sql_placeholders") {
				t.Fatalf("placeholder SQL was not accepted by verifier: report=%+v answer=%s", report, result.Content)
			}

			diagnostics := aiEvalDiagnosticsSnapshot(frame, contract, retrieval, composer, report)
			assertAIDiagnosticsFields(t, diagnostics, tc.ExpectedDiagnosticsFields)
		})
	}

	if calls := modelServer.calls.Load(); calls == 0 {
		t.Fatal("eval suite did not call the mock provider")
	}
}

func TestAIAgentEvalMockProviderIsUsed(t *testing.T) {
	server := newWebhookTestServer(t)
	modelServer := newAIAgentEvalMockProvider(t)
	cfg := newAIAgentEvalMockConfig(t, server, modelServer.URL)
	retrieval := aiRetrievalResult{
		TaskFrame: &aiTaskFrame{Intent: aiTaskIntentDocumentQA},
		Contract:  &aiEvidenceContract{ContractID: "document_qa.v1", Intent: aiTaskIntentDocumentQA},
	}
	result, err := server.callRoutedAIModel(context.Background(), cfg, "普通文档问题", retrieval)
	if err != nil {
		t.Fatalf("mock model call: %v", err)
	}
	if result.ProviderName != "EvalMock" || result.Content == "" {
		t.Fatalf("mock provider did not return a routed answer: %+v", result)
	}
}

func TestAIAgentEvalDiagnosticsRunReplayReproducesFailureReason(t *testing.T) {
	server := newWebhookTestServer(t)
	ctx := context.Background()
	runID := seedAIAgentEvalDiagnosticsReplayRun(t, server)

	detail, err := server.getAIDiagnosticsRunDetail(ctx, runID, "")
	if err != nil {
		t.Fatalf("diagnostics detail: %v", err)
	}
	detailJSON := encodeJSON(detail)
	for _, forbidden := range []string{"api_key", "sk-test", "Authorization", "Bearer"} {
		if strings.Contains(detailJSON, forbidden) {
			t.Fatalf("diagnostics detail leaked %s: %s", forbidden, detailJSON)
		}
	}

	replay := replayAIAgentEvalDiagnosticsRun(t, detail, aiEvalReplayMocks{
		Compose: func(aiTaskFrame, aiEvidenceContract, aiContractCoverageReport, []any) string {
			return "UPDATE widget_metric SET metric_value = 1 WHERE tenant_id = 2; [C1]"
		},
		Verify: func(frame aiTaskFrame, contract aiEvidenceContract, coverage aiContractCoverageReport, bundle aiEvidenceBundle, answer string, rounds []any) aiAnswerVerificationReport {
			roundNumber := aiEvalReplayRoundNumber(rounds)
			if aiRetrievalHasCompletedGaps(&coverage) && roundNumber < aiRetrievalMaxRounds {
				roundNumber = aiRetrievalMaxRounds
			}
			return verifyAIAnswerWithContext(frame, contract, coverage, bundle, answer, aiAnswerVerificationContext{
				EvidenceCount:  1,
				RetrievalRound: roundNumber,
			})
		},
	})
	if replay.Frame.Intent != aiTaskIntentDatabaseDirectUpdateForTest {
		t.Fatalf("replayed frame intent = %q", replay.Frame.Intent)
	}
	assertAIStringSetContains(t, "replayed contract required keys", aiEvidenceRequirementKeys(replay.Contract.Required), []string{"table_identity", "field_units", "where_conditions"})
	if len(replay.Rounds) == 0 {
		t.Fatalf("replay did not read retrieval rounds from diagnostics detail: %+v", detail.AgentWorkflow)
	}
	if !testStringSliceContains(replay.Coverage.MissingRequired, "field_units") {
		t.Fatalf("replayed coverage missing field_units gap: %+v", replay.Coverage)
	}
	if !testStringSliceContains(replay.Report.FailedChecks, "missing_required_deterministic_steps") {
		t.Fatalf("replay did not reproduce deterministic-step failure: %+v", replay.Report)
	}
	if replay.Report.NextAction != aiAnswerVerificationNextActionCompleteWithGaps {
		t.Fatalf("replay next_action = %s, want %s; report=%+v", replay.Report.NextAction, aiAnswerVerificationNextActionCompleteWithGaps, replay.Report)
	}
}

func createAIAgentEvalDatabaseRepo(t *testing.T, server *Server, includeUnits bool) Repository {
	t.Helper()
	ctx := context.Background()
	repoDir := createTestGitRepo(t)
	fieldName := "metric_value"
	unitComment := ""
	if includeUnits {
		fieldName = "metric_cents"
		unitComment = " // stored in cents"
	}
	writeScanPathTestFile(t, repoDir, "internal/db/widget_metric_reader.go", `package db

type WidgetMetric struct{}

func LoadWidgetMetric(tenantID, widgetID int64) {
	db.Model(&WidgetMetric{}).
		Where("tenant_id = ?", tenantID).
		Where("widget_id = ?", widgetID).
		First(&WidgetMetric{})
}
`)
	writeScanPathTestFile(t, repoDir, "models/widget_metric.go", `package models

func (WidgetMetric) TableName() string {
	return "widget_metric"
}

type WidgetMetric struct {
	TenantID int64 `+"`"+`gorm:"column:tenant_id"`+"`"+`
	WidgetID int64 `+"`"+`gorm:"column:widget_id"`+"`"+`
	`+aiEvalExportedFieldName(fieldName)+` int64 `+"`"+`gorm:"column:`+fieldName+`"`+"`"+unitComment+`
}
`)
	writeScanPathTestFile(t, repoDir, "internal/db/widget_metric_test.go", `package db

func TestNoisyDirectUpdateFixture(t *testing.T) {
	_ = "UPDATE widget_metric SET `+fieldName+` = 100 WHERE tenant_id = 1 AND widget_id = 2"
}
`)
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "add widget metric eval fixture")
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "widget-metric-service",
		Slug:                  "widget-metric-service-" + strconv.FormatBool(includeUnits),
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

func createAIAgentEvalAPIAndBranchRepo(t *testing.T, server *Server) Repository {
	t.Helper()
	ctx := context.Background()
	repoDir := createTestGitRepo(t)
	writeScanPathTestFile(t, repoDir, "router/widget_order.go", `package router

type CreateWidgetOrderRequest struct {
	TenantID string `+"`"+`json:"tenant_id" binding:"required"`+"`"+`
	SKUID    string `+"`"+`json:"sku_id" binding:"required"`+"`"+`
	Quantity int    `+"`"+`json:"quantity" binding:"min=1"`+"`"+`
}

type CreateWidgetOrderResponse struct {
	OrderID string `+"`"+`json:"order_id"`+"`"+`
	Status  string `+"`"+`json:"status"`+"`"+`
}

func RegisterWidgetOrderRoutes(r *Engine) {
	r.POST("/api/widget/orders", CreateWidgetOrderHandler)
}

func CreateWidgetOrderHandler(c *Context) {
	var req CreateWidgetOrderRequest
	if req.TenantID == "" {
		c.JSON(400, map[string]string{"code": "WIDGET_ORDER_INVALID"})
		return
	}
	c.JSON(200, CreateWidgetOrderResponse{OrderID: "placeholder", Status: "created"})
}
`)
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "add widget order api")
	runTestGit(t, repoDir, "checkout", "-b", "feat/quantum-delivery")
	writeScanPathTestFile(t, repoDir, "router/quantum_delivery.go", `package router

const QuantumDeliveryFeature = "QuantumDelivery"

func RegisterQuantumDeliveryRoutes(r *Engine) {
	r.POST("/api/quantum/delivery", QuantumDeliveryHandler)
}

func QuantumDeliveryHandler(c *Context) {
	c.JSON(202, map[string]string{"status": "candidate"})
}
`)
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "add quantum delivery branch candidate")
	runTestGit(t, repoDir, "checkout", "main")
	writeScanPathTestFile(t, repoDir, "router/quantum_delivery.go", `package router

func RegisterQuantumDeliveryPlaceholder(r *Engine) {
	// Baseline placeholder intentionally has no candidate handler.
}
`)
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "add main branch baseline")
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "widget-api-service",
		Slug:                  "widget-api-service",
		RepoURL:               repoDir,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"*"},
		LatestIncludeBranches: []string{"*"},
		BranchPriority:        []string{"main"},
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

type aiAgentEvalMockProvider struct {
	*httptest.Server
	calls atomic.Int64
}

func newAIAgentEvalMockProvider(t *testing.T) *aiAgentEvalMockProvider {
	t.Helper()
	provider := &aiAgentEvalMockProvider{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.calls.Add(1)
		payload := requireAIModelRequest(t, r)
		prompt := aiEvalPromptFromPayload(payload)
		answer := aiEvalMockAnswer(prompt)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": answer}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 8},
		})
	}))
	provider.Server = server
	t.Cleanup(server.Close)
	return provider
}

func newAIAgentEvalMockConfig(t *testing.T, server *Server, baseURL string) AIConfigData {
	t.Helper()
	secret, err := server.createOrUpdateAISecret(context.Background(), 0, aiSecretRequest{Name: "eval-mock-api-key", SecretType: "api_key", Value: "sk-test-eval-mock"}, "test")
	if err != nil {
		t.Fatalf("create eval mock secret: %v", err)
	}
	cfg := defaultAIConfig()
	cfg.Enabled = true
	cfg.Chat.MaxContextChunks = 12
	cfg.Chat.Providers = []AIProvider{{
		ProviderKey:           "eval-mock",
		Name:                  "EvalMock",
		Priority:              10,
		ProviderType:          "openai_compatible",
		BaseURL:               baseURL,
		Model:                 "eval-model",
		APIKeySecretID:        secret.ID,
		CostTier:              "low",
		RequestTimeoutSeconds: 5,
		MaxRPM:                120,
	}}
	cfg.Chat.Routing = buildDefaultRouting(context.Background(), server.db, cfg.Chat.Providers)
	return cfg
}

func aiEvalPromptFromPayload(payload map[string]any) string {
	messages, _ := payload["messages"].([]any)
	parts := make([]string, 0, len(messages))
	for _, item := range messages {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if content, ok := fields["content"].(string); ok {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

func aiEvalMockAnswer(prompt string) string {
	switch {
	case strings.Contains(prompt, `"intent":"diagnostics"`):
		return "这类回答错误排查需要历史 run 的 Task Frame、Contract、Retrieval Rounds、Coverage 和 verifier report；当前问题没有明确 run 身份时只能列缺口 [C1]。"
	case strings.Contains(prompt, "当前用户问题：\nQuantumDelivery 新接口"):
		return "功能分支候选 feat/quantum-delivery，commit 以引用中的 commit 为准 [C1]。"
	case strings.Contains(prompt, "/api/widget/orders") || strings.Contains(prompt, "widget_order.go"):
		return "POST /api/widget/orders 使用 CreateWidgetOrderHandler [C1]。\n请求字段 tenant_id、sku_id、quantity 来自 CreateWidgetOrderRequest [C1]。\n响应字段 order_id、status 来自 CreateWidgetOrderResponse [C1]。"
	case strings.Contains(prompt, `"intent":"database_direct_update_for_test"`) && strings.Contains(prompt, `"field_units":"covered"`) && strings.Contains(prompt, "metric_cents"):
		return "表 widget_metric、字段 metric_cents、WHERE tenant_id/widget_id 来自证据 [C1]。\nSELECT metric_cents FROM widget_metric WHERE tenant_id = ? AND widget_id = ?; [C1]\nUPDATE widget_metric SET metric_cents = ? WHERE tenant_id = ? AND widget_id = ?; [C1]"
	case strings.Contains(prompt, `"intent":"database_direct_update_for_test"`):
		return "已确认 widget_metric 表和 tenant_id/widget_id 读取条件见 [C1]；字段单位等 required 证据仍缺失，只能列缺口，不能给确定写入语句。"
	default:
		return "只能基于当前证据回答；关键事实需要引用。"
	}
}

func aiEvalSelectedScope(repoID int64) AIQuestionScope {
	return AIQuestionScope{RepoMode: "selected", RepoIDs: []int64{repoID}, SourceMode: "smart_latest"}
}

func aiEvalSelectedBranchScope(repoID int64) AIQuestionScope {
	return AIQuestionScope{RepoMode: "selected", RepoIDs: []int64{repoID}, SourceMode: "smart_latest_with_branch_candidates"}
}

func aiEvalExportedFieldName(fieldName string) string {
	parts := strings.Split(fieldName, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func assertAIStringSetContains(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	for _, key := range want {
		if !testStringSliceContains(got, key) {
			t.Fatalf("%s missing %s: got=%v want=%v", label, key, got, want)
		}
	}
}

func assertAICoverageContains(t *testing.T, coverage aiContractCoverageReport, keys []string) {
	t.Helper()
	for _, key := range keys {
		if coverage.Coverage[key] != aiEvidenceCoverageCovered {
			t.Fatalf("coverage[%s] = %q, want covered; coverage=%+v", key, coverage.Coverage[key], coverage)
		}
	}
}

func assertAIMissingCoverageContains(t *testing.T, coverage aiContractCoverageReport, keys []string) {
	t.Helper()
	for _, key := range keys {
		if !testStringSliceContains(coverage.MissingRequired, key) {
			t.Fatalf("missing_required does not contain %s: %+v", key, coverage)
		}
	}
}

func assertAITextMatches(t *testing.T, label, text string, patterns []string) {
	t.Helper()
	for _, pattern := range patterns {
		if !regexp.MustCompile(pattern).MatchString(text) {
			t.Fatalf("%s missing pattern %q:\n%s", label, pattern, text)
		}
	}
}

func assertAITextDoesNotMatch(t *testing.T, label, text string, patterns []string) {
	t.Helper()
	for _, pattern := range patterns {
		if regexp.MustCompile(pattern).MatchString(text) {
			t.Fatalf("%s matched forbidden pattern %q:\n%s", label, pattern, text)
		}
	}
}

func assertAIEvalExcludedTestFixture(t *testing.T, retrieval aiRetrievalResult) {
	t.Helper()
	if retrieval.Curation == nil {
		t.Fatalf("retrieval missing curation: %+v", retrieval)
	}
	var excluded bool
	for _, evidence := range retrieval.Curation.ExcludedEvidence {
		if evidence.EvidenceType == "test_fixture" && evidence.ExcludedReason == aiEvidenceExcludedReasonTestFixtureNonTestTask {
			excluded = true
		}
	}
	if !excluded {
		t.Fatalf("non-test database task did not exclude test fixture: %+v", retrieval.Curation.ExcludedEvidence)
	}
	for _, evidence := range retrieval.Evidence {
		if aiPathLooksTest(evidence.Citation.FilePath) {
			t.Fatalf("test fixture remained in core evidence: %+v", evidence)
		}
	}
}

func assertAIEvalBranchCandidateDisplayed(t *testing.T, question string, retrieval aiRetrievalResult) {
	t.Helper()
	var found bool
	for _, evidence := range retrieval.Evidence {
		if evidence.Citation.SourceScope == "branch_candidate" &&
			evidence.Citation.Branch == "feat/quantum-delivery" &&
			evidence.Citation.CommitSHA != "" &&
			strings.Contains(evidence.Content, "QuantumDelivery") {
			found = true
		}
	}
	if !found {
		t.Fatalf("branch candidate evidence missing branch/source/commit: %+v", retrieval.Evidence)
	}
	composer := prepareAIAnswerComposer(question, &retrieval)
	if len(composer.Messages) < 2 {
		t.Fatalf("composer messages missing: %+v", composer.Messages)
	}
	prompt := composer.Messages[1].Content
	for _, want := range []string{"scope=branch_candidate", "label=功能分支候选", "branch=feat/quantum-delivery", "commit="} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("composer prompt missing branch display %q:\n%s", want, prompt)
		}
	}
}

func aiEvalDiagnosticsSnapshot(frame aiTaskFrame, contract aiEvidenceContract, retrieval aiRetrievalResult, composer aiAnswerComposerPreparation, report aiAnswerVerificationReport) map[string]any {
	coverage := aiContractCoverageReport{}
	if retrieval.ContractCoverage != nil {
		coverage = *retrieval.ContractCoverage
	}
	return map[string]any{
		"task_frame":          frame,
		"evidence_contract":   summarizeAIEvidenceContract(contract),
		"retrieval_rounds":    retrieval.Rounds,
		"contract_coverage":   coverage,
		"answer_policy":       composer.Policy,
		"answer_composer":     composer.Summary,
		"verification_report": report,
	}
}

func assertAIDiagnosticsFields(t *testing.T, diagnostics map[string]any, fields []string) {
	t.Helper()
	for _, field := range fields {
		if !aiDiagnosticsFieldPresent(diagnostics, field) {
			t.Fatalf("diagnostics missing field %q: %s", field, encodeJSON(diagnostics))
		}
	}
}

func aiDiagnosticsFieldPresent(value any, path string) bool {
	var current any
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(raw, &current); err != nil {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		fields, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = fields[part]
		if !ok {
			return false
		}
	}
	switch typed := current.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func seedAIAgentEvalDiagnosticsReplayRun(t *testing.T, server *Server) int64 {
	t.Helper()
	ctx := context.Background()
	scope := AIQuestionScope{RepoMode: "global", SourceMode: "smart_latest"}
	session, err := createAISession(ctx, server.db, "eval diagnostics replay", "", scope)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	userMessage, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: session.ID, Role: "user", Content: "为什么数据库直改回答错了？"})
	if err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	assistantMessage, err := insertAIMessage(ctx, server.db, AIMessage{SessionID: session.ID, Role: "assistant", Content: "UPDATE widget_metric SET metric_value = 1 WHERE tenant_id = 2; [C1]", Status: aiWorkflowStatusCompletedWithGaps})
	if err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}
	cfg := AIConfigVersion{Version: 1, ConfigHash: "eval-replay", Config: defaultAIConfig()}
	run, err := createAIRun(ctx, server.db, session.ID, userMessage.ID, cfg, scope)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	frame := aiTaskFrame{
		Intent:          aiTaskIntentDatabaseDirectUpdateForTest,
		UserGoal:        "replay failed direct data update answer",
		AnswerShape:     "sql_steps_with_risk",
		ScopeStrategy:   "global_first",
		TargetArtifacts: []string{"table", "update_fields", "read_path", "field_units"},
		KnownTerms:      []string{"widget_metric", "metric_value", "tenant_id"},
	}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, map[string]string{"field_units": aiEvidenceCoverageMissing}, nil)
	markAIContractCoverageCompletedWithGaps(&coverage)
	bundle := aiEvalBundleFromCoverage(coverage)
	round := aiRetrievalRoundPlan{
		Round:               aiRetrievalMaxRounds,
		Intent:              aiLegacyIntentForTaskFrame(frame),
		Reason:              "strengthen_weak_or_missing_evidence: field_units",
		MissingContractKeys: []string{"field_units"},
		Searches:            []aiRetrievalRoundSearch{{Tool: "content_search", Query: "widget_metric metric_value field_units", Terms: []string{"widget_metric", "metric_value", "field_units"}}},
		QuerySource:         "diagnostics_replay_fixture",
		PlannerStatus:       "mock",
		NewEvidenceCount:    1,
		CoverageDelta:       map[string]string{"field_units": aiEvidenceCoverageMissing},
		NextAction:          aiWorkflowStatusCompletedWithGaps,
	}
	report := verifyAIAnswerWithContext(frame, contract, coverage, bundle, assistantMessage.Content, aiAnswerVerificationContext{
		EvidenceCount:  1,
		RetrievalRound: aiRetrievalMaxRounds,
	})

	steps := []AIAgentStep{
		{RunID: run.ID, AgentName: "task_framer", StepType: "deterministic", Status: "success", InputJSON: encodeJSON(map[string]any{"question": userMessage.Content}), OutputJSON: encodeJSON(frame), CreatedAt: nowString(), FinishedAt: nowString()},
		buildAIEvidenceContractStep(frame, contract),
		buildAIRetrievalRoundStep(&frame, &contract, nil, nil, round, coverage),
		buildAIEvidenceCuratorStep(&frame, &contract, aiEvidenceCurationResult{Bundle: bundle, Coverage: coverage, Evidence: []aiEvidence{}, ExcludedEvidence: []aiEvidence{}}, 1),
		buildAIContractCheckerStep(&contract, &bundle, coverage),
		buildAIAnswerVerifierStep(frame, contract, coverage, bundle, report, assistantMessage.Content),
	}
	for _, step := range steps {
		step.RunID = run.ID
		if err := insertAIStep(ctx, server.db, step); err != nil {
			t.Fatalf("insert step %s/%s: %v", step.AgentName, step.StepType, err)
		}
	}

	checkpoint := buildAIAgentRunCheckpoint(scope, "standard", aiQuestionPreparation{
		SearchQuestion:       userMessage.Content,
		Scope:                scope,
		TaskFrame:            &frame,
		Contract:             &contract,
		EvidenceBundle:       &bundle,
		Coverage:             &coverage,
		ContractCoverage:     &coverage,
		GeneratedSearchTerms: []string{"widget_metric", "metric_value"},
	})
	checkpoint["retrieval_rounds"] = []aiRetrievalRoundPlan{round}
	if err := finishAIRun(ctx, server.db, run.ID, AIAgentRun{
		AssistantMessageID:     assistantMessage.ID,
		Status:                 aiWorkflowStatusCompletedWithGaps,
		CurrentState:           "verify_answer",
		Intent:                 frame.Intent,
		ScopeJSON:              encodeJSON(scope),
		RetrievalPlanJSON:      encodeJSON(map[string]any{"retrieval_rounds": []aiRetrievalRoundPlan{round}}),
		ServiceCandidateCount:  0,
		EvidenceCount:          1,
		CodeEvidenceCount:      1,
		UnconfirmedCount:       aiContractCoverageUnconfirmedRequiredCount(&coverage),
		VerificationStatus:     report.Status,
		VerificationReportJSON: encodeJSON(report),
		CheckpointJSON:         encodeJSON(checkpoint),
		Model:                  "eval-model",
		ProviderName:           "EvalMock",
		ModelRouteJSON:         encodeJSON(map[string]any{"provider": "EvalMock", "model": "eval-model"}),
		FinishedAt:             nowString(),
	}); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	return run.ID
}

func aiEvalBundleFromCoverage(coverage aiContractCoverageReport) aiEvidenceBundle {
	bundle := aiEvidenceBundle{
		BundleID: "eval-replay-bundle",
		Coverage: coverage.Coverage,
		Groups:   []aiEvidenceGroup{},
	}
	for _, item := range coverage.Items {
		if item.Status != aiEvidenceCoverageCovered {
			continue
		}
		bundle.Groups = append(bundle.Groups, aiEvidenceGroup{
			Key:               item.Key,
			GroupKey:          "eval-replay:" + item.Key,
			EvidenceIDs:       []int64{1},
			Summary:           "replay covered evidence",
			EvidenceType:      "code",
			SourceReliability: aiEvidenceReliabilityHighSmartLatest,
		})
	}
	return bundle
}

type aiEvalReplayMocks struct {
	Compose func(aiTaskFrame, aiEvidenceContract, aiContractCoverageReport, []any) string
	Verify  func(aiTaskFrame, aiEvidenceContract, aiContractCoverageReport, aiEvidenceBundle, string, []any) aiAnswerVerificationReport
}

type aiEvalReplayResult struct {
	Frame    aiTaskFrame
	Contract aiEvidenceContract
	Coverage aiContractCoverageReport
	Rounds   []any
	Report   aiAnswerVerificationReport
}

func replayAIAgentEvalDiagnosticsRun(t *testing.T, detail aiDiagnosticsRunDetailResponse, mocks aiEvalReplayMocks) aiEvalReplayResult {
	t.Helper()
	frame := decodeAIEvalArtifact[aiTaskFrame](t, detail.TaskFrame)
	contract := aiEvalContractFromDiagnosticsArtifact(t, detail.EvidenceContract)
	coverage := decodeAIEvalArtifact[aiContractCoverageReport](t, detail.ContractCoverage)
	workflow, ok := detail.AgentWorkflow.(map[string]any)
	if !ok {
		workflow = decodeAIEvalArtifact[map[string]any](t, detail.AgentWorkflow)
	}
	rounds, _ := workflow["retrieval_rounds"].([]any)
	answer := mocks.Compose(frame, contract, coverage, rounds)
	bundle := aiEvalBundleFromCoverage(coverage)
	report := mocks.Verify(frame, contract, coverage, bundle, answer, rounds)
	return aiEvalReplayResult{Frame: frame, Contract: contract, Coverage: coverage, Rounds: rounds, Report: report}
}

func aiEvalReplayRoundNumber(rounds []any) int {
	maxRound := len(rounds)
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if round, ok := typed["round"]; ok {
				switch v := round.(type) {
				case float64:
					if int(v) > maxRound {
						maxRound = int(v)
					}
				case int:
					if v > maxRound {
						maxRound = v
					}
				}
			}
			for _, nested := range typed {
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	for _, item := range rounds {
		visit(item)
	}
	return maxRound
}

func decodeAIEvalArtifact[T any](t *testing.T, value any) T {
	t.Helper()
	var out T
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal eval artifact: %v", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode eval artifact %s: %v", raw, err)
	}
	return out
}

func aiEvalContractFromDiagnosticsArtifact(t *testing.T, value any) aiEvidenceContract {
	t.Helper()
	fields := decodeAIEvalArtifact[map[string]any](t, value)
	contract := aiEvidenceContract{
		ContractID: stringFromAny(fields["contract_id"]),
		Intent:     stringFromAny(fields["intent"]),
	}
	for _, key := range stringsFromAny(fields["required_keys"]) {
		contract.Required = append(contract.Required, aiEvidenceRequirement{Key: key})
	}
	for _, key := range stringsFromAny(fields["recommended_keys"]) {
		contract.Recommended = append(contract.Recommended, aiEvidenceRequirement{Key: key})
	}
	if len(contract.Required) == 0 {
		t.Fatalf("diagnostics contract did not expose required_keys: %+v", fields)
	}
	return contract
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func stringsFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}
