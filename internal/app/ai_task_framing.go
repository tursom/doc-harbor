package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	aiTaskIntentAPIIntegration              = "api_integration"
	aiTaskIntentDatabaseDirectUpdateForTest = "database_direct_update_for_test"
	aiTaskIntentCodePathExplanation         = "code_path_explanation"
	// business_value_change is selected by the model-driven planner for generic
	// "how do I change this runtime value" questions. It is intentionally not a
	// database-direct intent.
	aiTaskIntentBusinessValueChange = "business_value_change"
	aiTaskIntentCrossServiceImpact  = "cross_service_impact"
	aiTaskIntentBranchLookup        = "branch_lookup"
	aiTaskIntentDocumentQA          = "document_qa"
	aiTaskIntentDiagnostics         = "diagnostics"
)

type aiTaskFrameSupplement struct {
	Intent          string   `json:"intent"`
	IntentReason    string   `json:"intent_reason"`
	UserGoal        string   `json:"user_goal"`
	AnswerShape     string   `json:"answer_shape"`
	TargetArtifacts []string `json:"target_artifacts"`
	GeneratedTerms  []string `json:"generated_terms"`
}

func (s *Server) frameAITask(ctx context.Context, cfg AIConfigData, question string, prepared aiQuestionPreparation) (aiTaskFrame, AIAgentStep) {
	start := time.Now()
	frame := deterministicAITaskFrame(question, prepared)
	step := AIAgentStep{
		AgentName:  "task_framer",
		StepType:   "deterministic",
		Status:     "success",
		InputJSON:  encodeJSON(map[string]any{"question": truncate(question, 500), "effective_question": truncate(prepared.SearchQuestion, 800), "conversation": prepared.Conversation}),
		OutputJSON: encodeJSON(frame),
		CreatedAt:  nowString(),
		FinishedAt: nowString(),
	}
	if !cfg.Enabled {
		return frame, step
	}

	step.StepType = "model_call"
	result, supplement, err := s.generateAITaskFrameSupplement(ctx, cfg, question, prepared, frame)
	step.LatencyMS = int(time.Since(start).Milliseconds())
	step.FinishedAt = nowString()
	if result.Model != "" {
		step.Model = result.Model
		step.ProviderName = result.ProviderName
		step.ModelRouteReason = result.ModelRouteJSON
		step.TokenInput = result.PromptTokens
		step.TokenOutput = result.CompletionTokens
	}
	if err != nil {
		step.Status = "failed"
		step.ErrorMessage = aiTaskFrameSafeText(err.Error(), 500)
		if step.ErrorMessage == "" {
			step.ErrorMessage = "model supplement failed"
		}
		step.OutputJSON = encodeJSON(frame)
		return frame, step
	}
	supplement.GeneratedTerms = filterAIQueryPlannerTerms(supplement.GeneratedTerms, strings.TrimSpace(question+"\n"+prepared.SearchQuestion), &frame)
	mergeAITaskFrameSupplement(&frame, supplement)
	step.Status = "success"
	step.OutputJSON = encodeJSON(frame)
	return frame, step
}

func deterministicAITaskFrame(question string, prepared aiQuestionPreparation) aiTaskFrame {
	intent := classifyAITaskIntent(question)
	ambiguousValueChange := aiQuestionAsksAmbiguousDataValueChange(question)
	intentSource := "deterministic"
	intentReason := "matched deterministic task intent rules"
	if ambiguousValueChange && intent == aiTaskIntentCodePathExplanation {
		intentSource = "deterministic_default"
		intentReason = "ambiguous business value change defaults to code path until task framer or evidence proves direct database update"
	}
	frame := aiTaskFrame{
		Intent:               intent,
		IntentSource:         intentSource,
		IntentReason:         intentReason,
		AmbiguousValueChange: ambiguousValueChange,
		UserGoal:             aiTaskFrameDefaultUserGoal(intent),
		AnswerShape:          aiTaskFrameDefaultAnswerShape(intent),
		ScopeStrategy:        aiTaskFrameScopeStrategy(prepared),
		TargetArtifacts:      aiTaskFrameDefaultArtifacts(intent),
		MustNot: []string{
			"execute_sql",
			"execute_shell_or_git",
			"invent_business_names",
			"expose_secrets",
			"treat_test_fixtures_as_runtime_fact",
		},
		KnownTerms:     aiTaskFrameTermsFromQuestion(question),
		GeneratedTerms: aiTaskFrameCleanList(prepared.GeneratedSearchTerms, 16),
	}
	if prepared.Conversation.FollowUp {
		frame.FollowUp = &aiTaskFrameFollowUp{
			IsFollowUp:           true,
			PreviousPaths:        aiTaskFrameCleanList(prepared.Conversation.PreviousCitationPaths, 8),
			PreviousTopicSummary: aiTaskFrameSafeText(aiTaskFramePreviousTopicSummary(prepared.Conversation), 500),
		}
	}
	return frame
}

func classifyAITaskIntent(question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	switch {
	case aiQuestionAsksDatabaseChange(q):
		return aiTaskIntentCodePathExplanation
	case aiQuestionAsksBranchLookup(q):
		return aiTaskIntentBranchLookup
	case aiQuestionAsksDiagnostics(q):
		return aiTaskIntentDiagnostics
	case aiQuestionAsksAPIIntegration(q):
		return aiTaskIntentAPIIntegration
	case aiQuestionAsksCrossServiceImpact(q):
		return aiTaskIntentCrossServiceImpact
	case aiQuestionAsksCodePathExplanation(q):
		return aiTaskIntentCodePathExplanation
	default:
		return aiTaskIntentFromLegacy(classifyAIIntent(question))
	}
}

func aiTaskIntentFromLegacy(intent string) string {
	switch intent {
	case "database_change":
		return aiTaskIntentCodePathExplanation
	case "api_integration":
		return aiTaskIntentAPIIntegration
	case "cross_service":
		return aiTaskIntentCrossServiceImpact
	case "branch_lookup":
		return aiTaskIntentBranchLookup
	case "code_path":
		return aiTaskIntentCodePathExplanation
	case "test_lookup", "document_qa":
		return aiTaskIntentDocumentQA
	default:
		return aiTaskIntentDocumentQA
	}
}

func aiTaskIntentForRetrieval(question string, frame *aiTaskFrame) string {
	if frame == nil || strings.TrimSpace(frame.Intent) == "" {
		return classifyAIIntent(question)
	}
	return normalizeAITaskIntent(frame.Intent)
}

func normalizeAITaskIntent(intent string) string {
	switch intent {
	case aiTaskIntentAPIIntegration,
		aiTaskIntentDatabaseDirectUpdateForTest,
		aiTaskIntentCodePathExplanation,
		aiTaskIntentBusinessValueChange,
		aiTaskIntentCrossServiceImpact,
		aiTaskIntentBranchLookup,
		aiTaskIntentDiagnostics,
		aiTaskIntentDocumentQA:
		return intent
	default:
		return aiTaskIntentFromLegacy(intent)
	}
}

func aiIntentIsAPIIntegration(intent string) bool {
	return intent == aiTaskIntentAPIIntegration || intent == "api_integration"
}

func aiIntentIsDatabaseDirectUpdate(intent string) bool {
	return intent == aiTaskIntentDatabaseDirectUpdateForTest || intent == "database_change"
}

func aiIntentIsCrossService(intent string) bool {
	return intent == aiTaskIntentCrossServiceImpact || intent == "cross_service"
}

func aiIntentIsCodePathExplanation(intent string) bool {
	return intent == aiTaskIntentCodePathExplanation || intent == aiTaskIntentBusinessValueChange || intent == "code_path"
}

func aiQuestionAsksBranchLookup(q string) bool {
	hasBranch := strings.Contains(q, "分支") || strings.Contains(q, "branch")
	hasLookup := strings.Contains(q, "在哪") || strings.Contains(q, "哪里") || strings.Contains(q, "哪个") ||
		strings.Contains(q, "现在") || strings.Contains(q, "合入") || strings.Contains(q, "开发中") ||
		strings.Contains(q, "新接口") || strings.Contains(q, "new api")
	return hasBranch && hasLookup
}

func aiQuestionAsksDiagnostics(q string) bool {
	return strings.Contains(q, "诊断") || strings.Contains(q, "排查") || strings.Contains(q, "为什么回答") ||
		strings.Contains(q, "回答错") || strings.Contains(q, "检索") || strings.Contains(q, "没有命中") ||
		strings.Contains(q, "命中") || strings.Contains(q, "diagnos")
}

func aiQuestionAsksAPIIntegration(q string) bool {
	return strings.Contains(q, "接口") || strings.Contains(q, "参数") || strings.Contains(q, "返回") ||
		strings.Contains(q, "错误码") || strings.Contains(q, "api") || strings.Contains(q, "rpc") ||
		strings.Contains(q, "route") || strings.Contains(q, "endpoint")
}

func aiQuestionAsksCrossServiceImpact(q string) bool {
	return strings.Contains(q, "影响哪些服务") || strings.Contains(q, "哪些服务") ||
		strings.Contains(q, "跨服务") || strings.Contains(q, "跨仓库") || strings.Contains(q, "上下游") ||
		strings.Contains(q, "影响范围") || strings.Contains(q, "service impact")
}

func aiQuestionAsksCodePathExplanation(q string) bool {
	if aiQuestionAsksChangeGuidance(q) {
		return true
	}
	return strings.Contains(q, "代码") || strings.Contains(q, "在哪里实现") || strings.Contains(q, "在哪实现") ||
		strings.Contains(q, "调用链") || strings.Contains(q, "链路") || strings.Contains(q, "入口") ||
		strings.Contains(q, "实现路径") || strings.Contains(q, "code path")
}

func aiTaskFrameDefaultUserGoal(intent string) string {
	switch intent {
	case aiTaskIntentAPIIntegration:
		return "梳理接口接入方式、请求参数、返回字段和约束"
	case aiTaskIntentDatabaseDirectUpdateForTest:
		return "给出测试用途的数据库直接修改方案和风险边界"
	case aiTaskIntentCodePathExplanation:
		return "解释代码入口、调用链和关键实现位置"
	case aiTaskIntentBusinessValueChange:
		return "解释运行时业务值来源、优先级、写入点和派生关系"
	case aiTaskIntentCrossServiceImpact:
		return "分析跨服务影响范围和证据链"
	case aiTaskIntentBranchLookup:
		return "确认功能所在分支和候选证据"
	case aiTaskIntentDiagnostics:
		return "排查一次 AI 问答运行的检索、证据和模型调用过程"
	default:
		return "基于已扫描证据回答文档或代码事实问题"
	}
}

func aiTaskFrameDefaultAnswerShape(intent string) string {
	switch intent {
	case aiTaskIntentAPIIntegration:
		return "interface_table"
	case aiTaskIntentDatabaseDirectUpdateForTest:
		return "sql_steps_with_risk"
	case aiTaskIntentCodePathExplanation:
		return "call_chain"
	case aiTaskIntentBusinessValueChange:
		return "value_flow"
	case aiTaskIntentCrossServiceImpact:
		return "service_grouped_chain"
	case aiTaskIntentBranchLookup:
		return "branch_candidates"
	case aiTaskIntentDiagnostics:
		return "run_analysis"
	default:
		return "evidence_summary"
	}
}

func aiTaskFrameDefaultArtifacts(intent string) []string {
	switch intent {
	case aiTaskIntentAPIIntegration:
		return []string{"route_or_rpc", "request_fields", "response_fields", "error_codes"}
	case aiTaskIntentDatabaseDirectUpdateForTest:
		return []string{"table", "orm_model", "update_fields", "read_path", "field_units", "side_effects"}
	case aiTaskIntentCodePathExplanation:
		return []string{"entrypoint", "call_chain", "implementation_file", "branch"}
	case aiTaskIntentBusinessValueChange:
		return []string{"value_sources", "source_precedence", "primary_write_path", "persistence_target", "derived_value_handling", "side_effects"}
	case aiTaskIntentCrossServiceImpact:
		return []string{"service_candidates", "upstream_downstream_calls", "shared_models", "risk_points"}
	case aiTaskIntentBranchLookup:
		return []string{"branch_candidates", "merge_status", "commit_evidence", "source_scope"}
	case aiTaskIntentDiagnostics:
		return []string{"run_steps", "retrieval_plan", "citations", "provider_failover", "gaps"}
	default:
		return []string{"cited_documents", "current_fact", "constraints"}
	}
}

func aiQuestionAsksAmbiguousDataValueChange(question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	return aiQuestionAsksDataValueChange(q) && !aiQuestionAsksDatabaseChange(q)
}

func aiTaskFrameScopeStrategy(prepared aiQuestionPreparation) string {
	switch {
	case prepared.Conversation.FollowUp:
		return "follow_up_context"
	case prepared.Scope.CurrentFile != nil:
		return "current_file_first"
	case len(prepared.Scope.RepoIDs) > 0:
		return "selected_repositories"
	default:
		return "global_first"
	}
}

func aiTaskFrameTermsFromQuestion(question string) []string {
	return aiTaskFrameCleanList(aiQueryTerms(question), 16)
}

func aiTaskFrameLooksChangeGuidance(frame aiTaskFrame) bool {
	values := []string{frame.Intent, frame.UserGoal, frame.AnswerShape, frame.ScopeStrategy}
	values = append(values, frame.TargetArtifacts...)
	values = append(values, frame.KnownTerms...)
	values = append(values, frame.GeneratedTerms...)
	if frame.FollowUp != nil {
		values = append(values, frame.FollowUp.PreviousTopicSummary)
		values = append(values, frame.FollowUp.PreviousPaths...)
	}
	text := strings.ToLower(strings.Join(values, " "))
	return aiQuestionAsksChangeGuidance(text) ||
		strings.Contains(text, "entrypoint") ||
		strings.Contains(text, "call_chain") ||
		strings.Contains(text, "write_path")
}

func aiTaskFramePreviousTopicSummary(conversation aiConversationContext) string {
	if conversation.PreviousAssistantSummary != "" {
		return conversation.PreviousAssistantSummary
	}
	return conversation.PreviousUserQuestion
}

func (s *Server) generateAITaskFrameSupplement(ctx context.Context, cfg AIConfigData, question string, prepared aiQuestionPreparation, frame aiTaskFrame) (aiModelResult, aiTaskFrameSupplement, error) {
	allowedFields := []string{"intent", "intent_reason", "user_goal", "answer_shape", "target_artifacts", "generated_terms"}
	allowedIntents := []string{frame.Intent}
	if frame.AmbiguousValueChange {
		allowedIntents = []string{aiTaskIntentBusinessValueChange, aiTaskIntentCodePathExplanation, aiTaskIntentDocumentQA}
	}
	prompt := map[string]any{
		"intent":                 frame.Intent,
		"intent_source":          frame.IntentSource,
		"intent_reason":          frame.IntentReason,
		"ambiguous_value_change": frame.AmbiguousValueChange,
		"allowed_intents":        allowedIntents,
		"question":               truncate(question, 800),
		"effective_question":     truncate(prepared.SearchQuestion, 1200),
		"conversation":           prepared.Conversation,
		"allowed_fields":         allowedFields,
	}
	rawPrompt, _ := json.Marshal(prompt)
	messages := []aiChatMessage{
		{Role: "system", Content: "你是检索前的任务结构化助手。只返回 JSON 对象，不要回答用户问题。intent 只能从 allowed_intents 里选择；如果 allowed_intents 只有一个值，必须保持该 intent。只能补充 intent、intent_reason、user_goal、answer_shape、target_artifacts、generated_terms。数据库直改意图必须由 planner tool loop 的 finish_planning action 产生，本助手不得授予 SQL 许可。不要加入输入中没有依据的具体服务、仓库、模块或接口名。不要输出密钥、token、Authorization 或 API key。"},
		{Role: "user", Content: string(rawPrompt)},
	}
	result, err := s.callRoutedAIChat(ctx, cfg, messages, 0, 384)
	if err != nil {
		return result, aiTaskFrameSupplement{}, err
	}
	supplement, err := parseAITaskFrameSupplement(result.Content)
	if err != nil {
		return result, aiTaskFrameSupplement{}, err
	}
	return result, supplement, nil
}

func parseAITaskFrameSupplement(content string) (aiTaskFrameSupplement, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	}
	var supplement aiTaskFrameSupplement
	if err := json.Unmarshal([]byte(content), &supplement); err != nil {
		return aiTaskFrameSupplement{}, fmt.Errorf("task frame supplement must be JSON: %w", err)
	}
	return supplement, nil
}

func mergeAITaskFrameSupplement(frame *aiTaskFrame, supplement aiTaskFrameSupplement) {
	if value := aiTaskFrameSafeIdentifier(supplement.Intent, 80); value != "" {
		normalizedIntent, allowed := aiTaskFrameSupplementIntent(*frame, value)
		if allowed && normalizedIntent != "" && normalizedIntent != frame.Intent {
			frame.Intent = normalizedIntent
			frame.UserGoal = aiTaskFrameDefaultUserGoal(normalizedIntent)
			frame.AnswerShape = aiTaskFrameDefaultAnswerShape(normalizedIntent)
			frame.TargetArtifacts = aiTaskFrameDefaultArtifacts(normalizedIntent)
			frame.IntentSource = "model"
		} else if allowed && frame.IntentSource == "" {
			frame.IntentSource = "model"
		}
	}
	if value := aiTaskFrameSafeText(supplement.IntentReason, 300); value != "" {
		frame.IntentReason = value
	}
	if value := aiTaskFrameSafeText(supplement.UserGoal, 300); value != "" {
		frame.UserGoal = value
	}
	if value := aiTaskFrameSafeIdentifier(supplement.AnswerShape, 64); value != "" {
		frame.AnswerShape = value
	}
	if values := aiTaskFrameCleanList(supplement.TargetArtifacts, 12); len(values) > 0 {
		frame.TargetArtifacts = values
	}
	frame.GeneratedTerms = aiTaskFrameCleanList(append(frame.GeneratedTerms, supplement.GeneratedTerms...), 16)
}

func aiTaskFrameSupplementIntent(frame aiTaskFrame, intent string) (string, bool) {
	switch intent {
	case aiTaskIntentAPIIntegration,
		aiTaskIntentDatabaseDirectUpdateForTest,
		aiTaskIntentCodePathExplanation,
		aiTaskIntentBusinessValueChange,
		aiTaskIntentCrossServiceImpact,
		aiTaskIntentBranchLookup,
		aiTaskIntentDiagnostics,
		aiTaskIntentDocumentQA:
	default:
		return "", false
	}
	if frame.AmbiguousValueChange {
		switch intent {
		case aiTaskIntentBusinessValueChange, aiTaskIntentCodePathExplanation, aiTaskIntentDocumentQA:
			return intent, true
		default:
			return "", false
		}
	}
	return intent, intent == normalizeAITaskIntent(frame.Intent)
}

func aiTaskFrameCleanList(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = aiTaskFrameSafeText(value, 80)
		if len([]rune(value)) < 2 {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func aiTaskFrameSafeIdentifier(value string, limit int) string {
	value = aiTaskFrameSafeText(value, limit)
	value = strings.ToLower(value)
	if value == "" {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return ""
		}
	}
	return value
}

func aiTaskFrameSafeText(value string, limit int) string {
	value = sanitizeProviderError(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
	lower := strings.ToLower(value)
	if strings.Contains(lower, "api_key") || strings.Contains(lower, "api key") || strings.Contains(lower, "apikey") {
		return ""
	}
	if limit > 0 {
		value = truncate(value, limit)
	}
	return value
}
