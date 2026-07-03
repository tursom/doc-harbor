package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	aiPlannerToolSearchCodeEvidence = "search_code_evidence"
	aiPlannerToolViewFileSlice      = "view_file_slice"
	aiPlannerToolReadDiagnostics    = "read_ai_diagnostics"
	aiPlannerToolAssessContract     = "assess_evidence_contract"
	aiPlannerActionFinish           = "finish_planning"

	aiPlannerMaxRounds           = 6
	aiPlannerMaxFileSliceBytes   = 120 * 1024
	aiPlannerMaxObservationBytes = 700 * 1024
	aiPlannerMaxViewLines        = 500
)

type aiPlannerAction struct {
	Action             string                        `json:"action"`
	Reason             string                        `json:"reason,omitempty"`
	Intent             string                        `json:"intent,omitempty"`
	IntentReason       string                        `json:"intent_reason,omitempty"`
	UseFollowUpContext *bool                         `json:"use_follow_up_context,omitempty"`
	UsePreviousScope   *bool                         `json:"use_previous_scope,omitempty"`
	FollowUpReason     string                        `json:"follow_up_reason,omitempty"`
	ExplicitDBRequest  *bool                         `json:"explicit_database_request,omitempty"`
	ExplicitDBReason   string                        `json:"explicit_database_request_reason,omitempty"`
	UserGoal           string                        `json:"user_goal,omitempty"`
	AnswerShape        string                        `json:"answer_shape,omitempty"`
	TargetArtifacts    []string                      `json:"target_artifacts,omitempty"`
	GeneratedTerms     []string                      `json:"generated_terms,omitempty"`
	ContractKeys       []string                      `json:"contract_keys,omitempty"`
	Query              string                        `json:"query,omitempty"`
	Terms              []string                      `json:"terms,omitempty"`
	FileTypes          []string                      `json:"file_types,omitempty"`
	PathHints          []string                      `json:"path_hints,omitempty"`
	RepoID             int64                         `json:"repo_id,omitempty"`
	Branch             string                        `json:"branch,omitempty"`
	CommitSHA          string                        `json:"commit_sha,omitempty"`
	FilePath           string                        `json:"file_path,omitempty"`
	LineStart          int                           `json:"line_start,omitempty"`
	LineEnd            int                           `json:"line_end,omitempty"`
	DiagnosticsMode    string                        `json:"diagnostics_mode,omitempty"`
	RunID              int64                         `json:"run_id,omitempty"`
	Limit              int                           `json:"limit,omitempty"`
	Q                  string                        `json:"q,omitempty"`
	AnswerGuidance     string                        `json:"answer_guidance,omitempty"`
	Assessments        []aiPlannerContractAssessment `json:"assessments,omitempty"`
}

type aiPlannerContractAssessment struct {
	Key            string   `json:"key"`
	Status         string   `json:"status"`
	SupportingRefs []string `json:"supporting_refs,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

type aiPlannerContractAssessmentDecision struct {
	Key             string   `json:"key"`
	Status          string   `json:"status,omitempty"`
	SupportingRefs  []string `json:"supporting_refs,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Accepted        bool     `json:"accepted"`
	RejectionReason string   `json:"rejection_reason,omitempty"`
	Source          string   `json:"source"`
}

type aiPlannerObservation struct {
	Round         int                                   `json:"round"`
	Tool          string                                `json:"tool"`
	Status        string                                `json:"status"`
	Reason        string                                `json:"reason,omitempty"`
	EvidenceCount int                                   `json:"evidence_count,omitempty"`
	NewEvidence   []aiPlannerEvidenceSummary            `json:"new_evidence,omitempty"`
	Coverage      any                                   `json:"coverage,omitempty"`
	Assessments   []aiPlannerContractAssessmentDecision `json:"assessments,omitempty"`
	Error         string                                `json:"error,omitempty"`
}

type aiPlannerEvidenceSummary struct {
	Ref               string            `json:"ref"`
	Repo              string            `json:"repo"`
	RepoID            int64             `json:"repo_id"`
	Scope             string            `json:"scope"`
	Branch            string            `json:"branch"`
	Commit            string            `json:"commit"`
	FilePath          string            `json:"file_path"`
	LineStart         int               `json:"line_start"`
	LineEnd           int               `json:"line_end"`
	EvidenceType      string            `json:"evidence_type,omitempty"`
	ContractKeys      []string          `json:"contract_keys,omitempty"`
	ContractKeyStatus map[string]string `json:"contract_key_status,omitempty"`
	Snippet           string            `json:"snippet,omitempty"`
}

type aiPlannerRunState struct {
	Question            string                                `json:"question"`
	EffectiveQuestion   string                                `json:"effective_question,omitempty"`
	Scope               AIQuestionScope                       `json:"scope"`
	Conversation        aiConversationContext                 `json:"conversation,omitempty"`
	ToolRegistry        []map[string]any                      `json:"tool_registry"`
	SafetyPolicy        []string                              `json:"safety_policy"`
	Frame               aiTaskFrame                           `json:"task_frame"`
	Contract            aiEvidenceContract                    `json:"evidence_contract"`
	Observations        []aiPlannerObservation                `json:"observations,omitempty"`
	ContractAssessments []aiPlannerContractAssessmentDecision `json:"contract_assessments,omitempty"`
	ObservationBytes    int                                   `json:"observation_bytes"`
}

func (s *Server) planAndRetrieveAIEvidence(ctx context.Context, cfg AIConfigData, question string, scope AIQuestionScope, prepared aiQuestionPreparation, viewer string) (aiRetrievalResult, aiQuestionPreparation, []AIAgentStep, error) {
	scope = normalizeAIScope(scope)
	baseScope := scope
	steps := []AIAgentStep{buildAIPlannerToolRegistryStep()}
	if !cfg.Enabled {
		retrieval, prepared, fallbackSteps, err := s.runAIPlannerFallbackSearch(ctx, cfg, question, scope, prepared, "ai_disabled")
		steps = append(steps, fallbackSteps...)
		return retrieval, prepared, steps, err
	}

	frame := aiPlannerDefaultFrame(question, prepared, "model_planner_pending", "waiting for planner action")
	contract := buildAIEvidenceContract(frame)
	state := aiPlannerRunState{
		Question:          question,
		EffectiveQuestion: prepared.SearchQuestion,
		Scope:             scope,
		Conversation:      prepared.Conversation,
		ToolRegistry:      aiPlannerToolRegistry(),
		SafetyPolicy:      aiPlannerSafetyPolicy(),
		Frame:             frame,
		Contract:          contract,
	}
	rawEvidence := []aiEvidence{}
	rounds := []aiRetrievalRoundPlan{}
	var curation aiEvidenceCurationResult
	var contractCoverage *aiContractCoverageReport
	var plannerErr error
	finished := false

	for round := 1; round <= aiPlannerMaxRounds; round++ {
		start := time.Now()
		result, action, err := s.generateAIPlannerAction(ctx, cfg, state)
		plannerStep := buildAIPlannerActionStep(round, state, result, action, err, time.Since(start))
		steps = append(steps, plannerStep)
		if err != nil {
			plannerErr = err
			break
		}
		if err := validateAIPlannerAction(action); err != nil {
			steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "failed", map[string]any{"error": sanitizeProviderError(err.Error())}))
			state.Observations = append(state.Observations, aiPlannerObservation{Round: round, Tool: action.Action, Status: "failed", Error: sanitizeProviderError(err.Error())})
			continue
		}

		prepared, scope = applyAIPlannerConversationDecision(question, prepared, scope, baseScope, action)
		state.Scope = scope
		state.Conversation = prepared.Conversation
		state.EffectiveQuestion = prepared.SearchQuestion
		frame = aiPlannerFrameFromAction(question, prepared, frame, action)
		contract = buildAIEvidenceContract(frame)
		state.Frame = frame
		state.Contract = contract

		switch action.Action {
		case aiPlannerActionFinish:
			finished = true
		case aiPlannerToolSearchCodeEvidence:
			found, plan, err := s.executeAIPlannerSearch(ctx, action, frame, scope, cfg, round)
			if err != nil {
				steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "failed", map[string]any{"error": sanitizeProviderError(err.Error())}))
				state.Observations = append(state.Observations, aiPlannerObservation{Round: round, Tool: action.Action, Status: "failed", Error: sanitizeProviderError(err.Error())})
				continue
			}
			before := aiEvidenceIdentitySet(rawEvidence)
			rawEvidence = mergeAIEvidence(rawEvidence, found.Evidence)
			plan.NewEvidenceCount = aiCountNewEvidence(before, rawEvidence)
			curation = curateAIEvidence(&frame, &contract, rawEvidence)
			coverage := checkAIEvidenceContract(contract, curation.Bundle)
			plan.CoverageDelta = aiContractCoverageDelta(contractCoverage, &coverage)
			rounds = append(rounds, plan)
			contractCoverage = &coverage
			steps = append(steps, buildAIRetrievalRoundStep(&frame, &contract, nil, nil, plan, coverage))
			steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "success", map[string]any{"evidence_count": len(found.Evidence), "total_evidence_count": len(rawEvidence)}))
			state.Observations = append(state.Observations, aiPlannerObservation{
				Round:         round,
				Tool:          action.Action,
				Status:        "success",
				Reason:        action.Reason,
				EvidenceCount: len(rawEvidence),
				NewEvidence:   summarizeAIPlannerEvidence(curation.Evidence, 8),
				Coverage:      summarizeAIContractCoverageReport(coverage),
			})
		case aiPlannerToolViewFileSlice:
			evidence, err := s.executeAIPlannerViewFile(ctx, action, scope)
			if err != nil {
				steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "failed", map[string]any{"error": sanitizeProviderError(err.Error())}))
				state.Observations = append(state.Observations, aiPlannerObservation{Round: round, Tool: action.Action, Status: "failed", Error: sanitizeProviderError(err.Error())})
				continue
			}
			before := aiEvidenceIdentitySet(rawEvidence)
			rawEvidence = mergeAIEvidence(rawEvidence, []aiEvidence{evidence})
			curation = curateAIEvidence(&frame, &contract, rawEvidence)
			coverage := checkAIEvidenceContract(contract, curation.Bundle)
			contractCoverage = &coverage
			steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "success", map[string]any{"new_evidence_count": aiCountNewEvidence(before, rawEvidence), "file_path": evidence.Citation.FilePath}))
			state.Observations = append(state.Observations, aiPlannerObservation{
				Round:         round,
				Tool:          action.Action,
				Status:        "success",
				Reason:        action.Reason,
				EvidenceCount: len(rawEvidence),
				NewEvidence:   summarizeAIPlannerEvidence(curation.Evidence, 8),
				Coverage:      summarizeAIContractCoverageReport(coverage),
			})
		case aiPlannerToolReadDiagnostics:
			summary, err := s.executeAIPlannerDiagnostics(ctx, action, viewer)
			status := "success"
			if err != nil {
				status = "failed"
				summary = map[string]any{"error": sanitizeProviderError(err.Error())}
			}
			steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, status, summary))
			state.Observations = append(state.Observations, aiPlannerObservation{Round: round, Tool: action.Action, Status: status, Reason: action.Reason, Error: aiPlannerObservationError(summary)})
		case aiPlannerToolAssessContract:
			if curation.Bundle.BundleID == "" {
				curation = curateAIEvidence(&frame, &contract, rawEvidence)
			}
			updatedRawEvidence, decisions, err := applyAIPlannerContractAssessments(contract, rawEvidence, curation.Evidence, action.Assessments)
			if err != nil {
				steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "failed", map[string]any{"error": sanitizeProviderError(err.Error())}))
				state.Observations = append(state.Observations, aiPlannerObservation{Round: round, Tool: action.Action, Status: "failed", Error: sanitizeProviderError(err.Error())})
				continue
			}
			rawEvidence = updatedRawEvidence
			state.ContractAssessments = append(state.ContractAssessments, decisions...)
			curation = curateAIEvidence(&frame, &contract, rawEvidence)
			coverage := checkAIEvidenceContract(contract, curation.Bundle)
			contractCoverage = &coverage
			summary := summarizeAIPlannerContractAssessmentDecisions(decisions)
			summary["coverage"] = summarizeAIContractCoverageReport(coverage)
			steps = append(steps, buildAIPlannerToolResultStep(round, action.Action, "success", summary))
			steps = append(steps, buildAIPlannerContractAssessmentStep(round, decisions, coverage))
			state.Observations = append(state.Observations, aiPlannerObservation{
				Round:         round,
				Tool:          action.Action,
				Status:        "success",
				Reason:        action.Reason,
				EvidenceCount: len(rawEvidence),
				NewEvidence:   summarizeAIPlannerEvidence(curation.Evidence, 8),
				Coverage:      summarizeAIContractCoverageReport(coverage),
				Assessments:   decisions,
			})
		}
		state.ObservationBytes = len(encodeJSON(state.Observations))
		if state.ObservationBytes > aiPlannerMaxObservationBytes {
			plannerErr = fmt.Errorf("planner observation budget exceeded")
			break
		}
		if finished {
			break
		}
	}
	if plannerErr != nil && len(rawEvidence) == 0 {
		retrieval, prepared, fallbackSteps, err := s.runAIPlannerFallbackSearch(ctx, cfg, question, scope, prepared, plannerErr.Error())
		steps = append(steps, fallbackSteps...)
		return retrieval, prepared, steps, err
	}
	if !finished && frame.IntentSource == "model_planner_pending" {
		frame.IntentSource = "fallback_generic"
		frame.IntentReason = "planner did not finish; using conservative read-only evidence summary"
	}
	if len(rawEvidence) == 0 && plannerErr == nil {
		retrieval, prepared, fallbackSteps, err := s.runAIPlannerFallbackSearch(ctx, cfg, question, scope, prepared, "no_planner_evidence")
		steps = append(steps, fallbackSteps...)
		return retrieval, prepared, steps, err
	}
	if curation.Bundle.BundleID == "" {
		curation = curateAIEvidence(&frame, &contract, rawEvidence)
	}
	coverage := checkAIEvidenceContract(contract, curation.Bundle)
	if aiContractCoverageStillNeedsRetrieval(&coverage) {
		markAIContractCoverageCompletedWithGaps(&coverage)
	}
	contractCoverage = &coverage
	repos, err := listRepositories(ctx, s.db)
	if err != nil {
		return aiRetrievalResult{}, prepared, steps, err
	}
	repos = filterAIRepos(repos, scope)
	candidates := buildAIServiceCandidates(repos, curation.Evidence)
	prepared.TaskFrame = &frame
	prepared.Contract = &contract
	prepared.EvidenceBundle = &curation.Bundle
	prepared.Coverage = &curation.Coverage
	prepared.ContractCoverage = contractCoverage
	retrieval := aiRetrievalResult{
		Intent:              aiLegacyIntentForTaskFrame(frame),
		Scope:               scope,
		Plan:                buildAIPlannerRetrievalPlan(frame, contract, scope, state, rounds, curation, contractCoverage),
		Evidence:            curation.Evidence,
		ServiceCandidates:   candidates,
		Conversation:        prepared.Conversation,
		TaskFrame:           &frame,
		Contract:            &contract,
		EvidenceBundle:      &curation.Bundle,
		Coverage:            &curation.Coverage,
		ContractCoverage:    contractCoverage,
		Rounds:              rounds,
		Curation:            &curation,
		RetrievalRoundSteps: nil,
	}
	return retrieval, prepared, steps, nil
}

func (s *Server) runAIPlannerFallbackSearch(ctx context.Context, cfg AIConfigData, question string, scope AIQuestionScope, prepared aiQuestionPreparation, reason string) (aiRetrievalResult, aiQuestionPreparation, []AIAgentStep, error) {
	frame := aiPlannerDefaultFrame(question, prepared, "fallback_generic", "model planner unavailable: "+sanitizeProviderError(reason))
	contract := buildAIEvidenceContract(frame)
	retrieval, err := s.runAIRetrievalOrchestrator(ctx, &frame, &contract, scope, cfg)
	if err != nil {
		return aiRetrievalResult{}, prepared, nil, err
	}
	steps := []AIAgentStep{
		buildAIPlannerToolResultStep(1, aiPlannerToolSearchCodeEvidence, "success", map[string]any{"mode": "fallback_generic", "reason": sanitizeProviderError(reason), "evidence_count": len(retrieval.Evidence)}),
	}
	steps = append(steps, retrieval.RetrievalRoundSteps...)
	prepared.TaskFrame = &frame
	prepared.Contract = &contract
	prepared.EvidenceBundle = retrieval.EvidenceBundle
	prepared.Coverage = retrieval.Coverage
	prepared.ContractCoverage = retrieval.ContractCoverage
	retrieval.Conversation = prepared.Conversation
	if retrieval.Plan == nil {
		retrieval.Plan = map[string]any{}
	}
	retrieval.Plan["tool_registry"] = aiPlannerToolRegistry()
	retrieval.Plan["safety_policy"] = aiPlannerSafetyPolicy()
	retrieval.Plan["intent_source"] = frame.IntentSource
	retrieval.Plan["intent_reason"] = frame.IntentReason
	retrieval.Plan["explicit_database_request"] = frame.ExplicitDBRequest
	return retrieval, prepared, steps, nil
}

func aiPlannerDefaultFrame(question string, prepared aiQuestionPreparation, source, reason string) aiTaskFrame {
	intent := aiTaskIntentDocumentQA
	frame := aiTaskFrame{
		Intent:          intent,
		IntentSource:    source,
		IntentReason:    reason,
		UserGoal:        aiTaskFrameDefaultUserGoal(intent),
		AnswerShape:     aiTaskFrameDefaultAnswerShape(intent),
		ScopeStrategy:   aiTaskFrameScopeStrategy(prepared),
		TargetArtifacts: aiTaskFrameDefaultArtifacts(intent),
		MustNot:         aiPlannerSafetyPolicy(),
		KnownTerms:      aiTaskFrameTermsFromQuestion(question),
		GeneratedTerms:  aiTaskFrameCleanList(prepared.GeneratedSearchTerms, 16),
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

func aiPlannerFrameFromAction(question string, prepared aiQuestionPreparation, current aiTaskFrame, action aiPlannerAction) aiTaskFrame {
	frame := current
	if strings.TrimSpace(frame.Intent) == "" {
		frame = aiPlannerDefaultFrame(question, prepared, "model_planner", "planner selected task frame")
	}
	intent := normalizeAITaskIntent(action.Intent)
	if intent == "" {
		intent = frame.Intent
	}
	if action.ExplicitDBRequest != nil {
		frame.ExplicitDBRequest = *action.ExplicitDBRequest
		frame.ExplicitDBSource = "model_planner"
		if reason := aiTaskFrameSafeText(action.ExplicitDBReason, 300); reason != "" {
			frame.ExplicitDBReason = reason
		} else if frame.ExplicitDBRequest {
			frame.ExplicitDBReason = "planner asserted the user explicitly requested database or SQL direct update"
		} else {
			frame.ExplicitDBReason = "planner did not find an explicit database or SQL direct update request"
		}
	}
	if intent == aiTaskIntentDatabaseDirectUpdateForTest && !frame.ExplicitDBRequest {
		intent = aiTaskIntentBusinessValueChange
		frame.IntentSource = "safety_blocked"
		frame.IntentReason = "planner selected database direct update without structured explicit_database_request=true; downgraded to business value flow"
	} else {
		frame.IntentSource = "model_planner"
		if reason := aiTaskFrameSafeText(action.IntentReason, 300); reason != "" {
			frame.IntentReason = reason
		} else if reason := aiTaskFrameSafeText(action.Reason, 300); reason != "" {
			frame.IntentReason = reason
		}
	}
	frame.Intent = intent
	frame.UserGoal = aiTaskFrameDefaultUserGoal(intent)
	frame.AnswerShape = aiTaskFrameDefaultAnswerShape(intent)
	frame.ScopeStrategy = aiTaskFrameScopeStrategy(prepared)
	frame.TargetArtifacts = aiTaskFrameDefaultArtifacts(intent)
	if prepared.Conversation.FollowUp {
		frame.FollowUp = &aiTaskFrameFollowUp{
			IsFollowUp:           true,
			PreviousPaths:        aiTaskFrameCleanList(prepared.Conversation.PreviousCitationPaths, 8),
			PreviousTopicSummary: aiTaskFrameSafeText(aiTaskFramePreviousTopicSummary(prepared.Conversation), 500),
		}
	} else {
		frame.FollowUp = nil
	}
	if value := aiTaskFrameSafeText(action.UserGoal, 300); value != "" {
		frame.UserGoal = value
	}
	if value := aiTaskFrameSafeIdentifier(action.AnswerShape, 64); value != "" {
		frame.AnswerShape = value
	}
	if values := aiTaskFrameCleanList(action.TargetArtifacts, 12); len(values) > 0 {
		frame.TargetArtifacts = values
	}
	frame.GeneratedTerms = aiTaskFrameCleanList(append(frame.GeneratedTerms, action.GeneratedTerms...), 24)
	frame.GeneratedTerms = aiTaskFrameCleanList(append(frame.GeneratedTerms, action.Terms...), 24)
	frame.MustNot = aiPlannerSafetyPolicy()
	return frame
}

func applyAIPlannerConversationDecision(question string, prepared aiQuestionPreparation, currentScope, baseScope AIQuestionScope, action aiPlannerAction) (aiQuestionPreparation, AIQuestionScope) {
	if action.UseFollowUpContext == nil && action.UsePreviousScope == nil {
		return prepared, currentScope
	}
	conversation := prepared.Conversation
	if !conversation.PreviousContextAvailable {
		prepared.Conversation = conversation
		return prepared, currentScope
	}
	if action.UseFollowUpContext != nil {
		conversation.FollowUp = *action.UseFollowUpContext
		conversation.FollowUpSource = "model_planner"
		conversation.FollowUpReason = aiTaskFrameSafeText(action.FollowUpReason, 300)
		if conversation.FollowUpReason == "" {
			if conversation.FollowUp {
				conversation.FollowUpReason = "planner selected previous conversation context"
			} else {
				conversation.FollowUpReason = "planner selected current question as standalone"
			}
		}
	}
	usePreviousScope := conversation.FollowUp && action.UsePreviousScope != nil && *action.UsePreviousScope
	conversation.UsePreviousScope = false
	scope := baseScope
	if conversation.FollowUp {
		prepared.SearchQuestion = buildAIFollowUpSearchQuestion(question, conversation)
		if usePreviousScope && !aiScopeHasUserSelectedRepositoryBoundary(baseScope) && len(conversation.FocusRepoIDs) > 0 {
			scope.RepoIDs = append([]int64(nil), conversation.FocusRepoIDs...)
			scope.RepoMode = "follow_up_context"
			conversation.UsePreviousScope = true
		}
	} else {
		prepared.SearchQuestion = question
	}
	prepared.Scope = scope
	prepared.Conversation = conversation
	return prepared, scope
}

func aiScopeHasUserSelectedRepositoryBoundary(scope AIQuestionScope) bool {
	return scope.CurrentFile != nil && scope.CurrentFile.RepoID > 0 || len(scope.RepoIDs) > 0
}

func (s *Server) generateAIPlannerAction(ctx context.Context, cfg AIConfigData, state aiPlannerRunState) (aiModelResult, aiPlannerAction, error) {
	messages := buildAIPlannerMessages(state)
	result, err := s.callRoutedAIChat(ctx, cfg, messages, 0, 1024)
	if err != nil {
		return result, aiPlannerAction{}, err
	}
	action, err := parseAIPlannerAction(result.Content)
	if err != nil {
		return result, aiPlannerAction{}, err
	}
	return result, action, nil
}

func buildAIPlannerMessages(state aiPlannerRunState) []aiChatMessage {
	system := strings.Join([]string{
		"你是 DocHarbor 的只读 AI Planner。你不回答用户问题，只返回一个 JSON action。",
		"你必须自己判断下一步需要哪个只读工具；程序只提供安全边界和工具执行。",
		"允许 action 只有 search_code_evidence、view_file_slice、read_ai_diagnostics、assess_evidence_contract、finish_planning。",
		"禁止请求 SQL 执行、shell、git 写入、网络写入、缓存刷新或密钥读取。",
		"如果 conversation.previous_context_available=true，你必须判断当前问题是否依赖上一轮；需要沿用时返回 use_follow_up_context=true，否则返回 false。",
		"只有当前问题确实沿用上一轮主题且不要求更大范围时，才返回 use_previous_scope=true；程序只做 scope 边界校验。",
		"当问题是泛业务值修改时，先检索读取链路、写入链路、持久化对象、缓存/同步链路和值来源优先级；不要仅凭表结构字段下结论。",
		"当 current_contract 的 value-flow key 已有证据时，必须用 assess_evidence_contract 明确评估 key/status/supporting_refs；不要让程序替你猜业务语义。",
		"只有用户明确要求测试库/数据库/SQL/表字段直改时，finish_planning 才能选择 database_direct_update_for_test。",
		"只返回 JSON，不要输出 Markdown。",
	}, "\n")
	payload := map[string]any{
		"question":             state.Question,
		"effective_question":   state.EffectiveQuestion,
		"scope":                state.Scope,
		"conversation":         state.Conversation,
		"tool_registry":        state.ToolRegistry,
		"safety_policy":        state.SafetyPolicy,
		"current_task_frame":   state.Frame,
		"current_contract":     summarizeAIEvidenceContract(state.Contract),
		"observations":         state.Observations,
		"contract_assessments": state.ContractAssessments,
		"response_schema": map[string]any{
			"action":                           "search_code_evidence | view_file_slice | read_ai_diagnostics | assess_evidence_contract | finish_planning",
			"intent":                           "document_qa | business_value_change | code_path_explanation | api_integration | cross_service_impact | branch_lookup | diagnostics | database_direct_update_for_test",
			"use_follow_up_context":            "boolean; set true only when the current question depends on conversation.previous_* context",
			"use_previous_scope":               "boolean; set true only when previous citation repos should constrain retrieval scope",
			"follow_up_reason":                 "short reason when use_follow_up_context is present",
			"explicit_database_request":        "boolean; true only when the user explicitly asked for database/SQL/table-field/test-data direct update",
			"explicit_database_request_reason": "short reason when explicit_database_request is present",
			"query":                            "search query for search_code_evidence",
			"terms":                            []string{"short", "search", "terms"},
			"repo_id/file_path":                "required for view_file_slice",
			"line_start/line_end":              "optional bounded line window for view_file_slice",
			"assessments":                      []map[string]any{{"key": "contract key from current_contract", "status": "covered | partial | missing | conflict", "supporting_refs": []string{"[C1]"}, "reason": "short evidence-based reason"}},
		},
	}
	return []aiChatMessage{{Role: "system", Content: system}, {Role: "user", Content: encodeJSON(payload)}}
}

func parseAIPlannerAction(content string) (aiPlannerAction, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	}
	if !strings.HasPrefix(content, "{") {
		start := strings.Index(content, "{")
		end := strings.LastIndex(content, "}")
		if start >= 0 && end > start {
			content = content[start : end+1]
		}
	}
	var action aiPlannerAction
	if err := json.Unmarshal([]byte(content), &action); err != nil {
		return aiPlannerAction{}, fmt.Errorf("planner action must be JSON: %w", err)
	}
	action.Action = strings.TrimSpace(action.Action)
	if action.Action == "" && strings.Contains(content, `"terms"`) {
		action.Action = aiPlannerToolSearchCodeEvidence
	}
	if action.Action == "" {
		return aiPlannerAction{}, fmt.Errorf("planner action is required")
	}
	return action, nil
}

func validateAIPlannerAction(action aiPlannerAction) error {
	switch action.Action {
	case aiPlannerToolSearchCodeEvidence, aiPlannerToolViewFileSlice, aiPlannerToolReadDiagnostics, aiPlannerToolAssessContract, aiPlannerActionFinish:
		return nil
	default:
		return fmt.Errorf("unsupported planner tool/action: %s", action.Action)
	}
}

func (s *Server) executeAIPlannerSearch(ctx context.Context, action aiPlannerAction, frame aiTaskFrame, scope AIQuestionScope, cfg AIConfigData, round int) (aiSearchPlanResult, aiRetrievalRoundPlan, error) {
	terms := mergeTerms(action.Terms, aiQueryTerms(action.Query), frame.KnownTerms, frame.GeneratedTerms)
	if len(terms) > 32 {
		terms = terms[:32]
	}
	if len(terms) == 0 {
		terms = aiQueryTerms(action.Query)
	}
	search := aiRetrievalRoundSearch{
		Tool:      aiPlannerToolSearchCodeEvidence,
		Query:     aiTaskFrameSafeText(action.Query, 500),
		FileTypes: sanitizeAIPlannerFileTypes(action.FileTypes),
		PathHints: sanitizeAIRetrievalPathHints(action.PathHints),
		Terms:     terms,
	}
	plan := aiRetrievalRoundPlan{
		Round:               round,
		Intent:              aiLegacyIntentForTaskFrame(frame),
		Reason:              aiTaskFrameSafeText(action.Reason, 300),
		MissingContractKeys: aiTaskFrameCleanList(action.ContractKeys, 16),
		Searches:            []aiRetrievalRoundSearch{search},
		QuerySource:         "model_planner_action",
		PlannerStatus:       "model_planner",
		CoverageDelta:       map[string]string{},
		NextAction:          "execute_search_plan",
	}
	found, err := s.executeAISearchPlan(ctx, plan, scope, cfg)
	return found, plan, err
}

func (s *Server) executeAIPlannerViewFile(ctx context.Context, action aiPlannerAction, scope AIQuestionScope) (aiEvidence, error) {
	if action.RepoID <= 0 {
		return aiEvidence{}, errBadRequest("repo_id is required")
	}
	if !aiPlannerRepoAllowed(action.RepoID, scope) {
		return aiEvidence{}, errForbidden("repo is outside current AI scope")
	}
	repo, err := getRepository(ctx, s.db, action.RepoID)
	if err != nil {
		return aiEvidence{}, err
	}
	filePath := normalizeRepoPath(action.FilePath)
	if filePath == "" {
		return aiEvidence{}, errBadRequest("file_path is required")
	}
	commit := strings.TrimSpace(action.CommitSHA)
	branch := strings.TrimSpace(action.Branch)
	sourceScope := "smart_latest"
	if commit == "" {
		target, err := s.resolveAIPlannerRef(ctx, repo, branch)
		if err != nil {
			return aiEvidence{}, err
		}
		commit = target.CommitSHA
		branch = target.Branch
		sourceScope = target.SourceScope
	}
	data, err := s.git.showFile(ctx, s.git.repoPath(repo.ID), commit, filePath)
	if err != nil {
		return aiEvidence{}, err
	}
	if !aiLooksText(data) {
		return aiEvidence{}, errBadRequest("file is not text")
	}
	lines := strings.Split(string(data), "\n")
	startLine, endLine := action.LineStart, action.LineEnd
	if startLine > 0 && endLine > 0 && endLine-startLine+1 > aiPlannerMaxViewLines {
		return aiEvidence{}, errBadRequest("line window is too large")
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > len(lines) {
		endLine = min(len(lines), startLine+aiPlannerMaxViewLines-1)
	}
	if endLine < startLine {
		return aiEvidence{}, errBadRequest("line_end must be >= line_start")
	}
	if endLine-startLine+1 > aiPlannerMaxViewLines {
		return aiEvidence{}, errBadRequest("line window is too large")
	}
	var b strings.Builder
	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("%d: %s", i+1, strings.TrimRight(lines[i], "\r")))
		if b.Len() > aiPlannerMaxFileSliceBytes {
			return aiEvidence{}, errBadRequest("file slice is too large")
		}
	}
	return aiEvidence{
		Repo: repo,
		Citation: AIMessageCitation{
			RepoID:      repo.ID,
			VersionID:   findAIVersionID(ctx, s.db, repo.ID, branch, filePath),
			SourceScope: sourceScope,
			Branch:      branch,
			CommitSHA:   commit,
			FilePath:    filePath,
			LineStart:   startLine,
			LineEnd:     endLine,
			QuoteText:   truncate(b.String(), 700),
			Score:       120,
		},
		Content:      b.String(),
		MatchedTerms: mergeTerms(aiQueryTerms(action.Query), action.Terms),
		Score:        120,
	}, nil
}

func (s *Server) resolveAIPlannerRef(ctx context.Context, repo Repository, branch string) (aiRefTarget, error) {
	targets, err := s.aiRefTargets(ctx, repo, "smart_latest_with_branch_candidates")
	if err != nil {
		return aiRefTarget{}, err
	}
	if branch != "" {
		for _, target := range targets {
			if target.Branch == branch {
				return target, nil
			}
		}
		return aiRefTarget{}, errBadRequest("branch is not indexed for repo")
	}
	if len(targets) == 0 {
		return aiRefTarget{}, errNotFound("no indexed ref for repo")
	}
	return targets[0], nil
}

func (s *Server) executeAIPlannerDiagnostics(ctx context.Context, action aiPlannerAction, viewer string) (map[string]any, error) {
	mode := strings.TrimSpace(action.DiagnosticsMode)
	if mode == "" {
		mode = "data_sources"
	}
	switch mode {
	case "data_sources":
		resp, err := s.getAIDiagnosticsDataSources(ctx, AIQuestionScope{})
		if err != nil {
			return nil, err
		}
		return map[string]any{"mode": mode, "data_sources": sanitizeAIDiagnosticsArtifact(resp)}, nil
	case "runs":
		resp, err := listAIDiagnosticsRuns(ctx, s.db, aiDiagnosticsRunQuery{
			Limit:  cleanLimit(strconv.Itoa(action.Limit), 10, 50),
			Q:      action.Q,
			Viewer: viewer,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"mode": mode, "runs": sanitizeAIDiagnosticsArtifact(resp)}, nil
	case "run_detail":
		if action.RunID <= 0 {
			return nil, errBadRequest("run_id is required for run_detail")
		}
		resp, err := s.getAIDiagnosticsRunDetail(ctx, action.RunID, viewer)
		if err != nil {
			return nil, err
		}
		return map[string]any{"mode": mode, "run": sanitizeAIDiagnosticsArtifact(resp)}, nil
	default:
		return nil, errBadRequest("unsupported diagnostics_mode")
	}
}

func applyAIPlannerContractAssessments(contract aiEvidenceContract, rawEvidence []aiEvidence, displayedEvidence []aiEvidence, assessments []aiPlannerContractAssessment) ([]aiEvidence, []aiPlannerContractAssessmentDecision, error) {
	if len(assessments) == 0 {
		return rawEvidence, nil, errBadRequest("assessments are required")
	}
	updated := make([]aiEvidence, len(rawEvidence))
	for i, item := range rawEvidence {
		updated[i] = item
		updated[i].ContractKeyStatus = aiCopyStringMap(item.ContractKeyStatus)
	}
	rawIndexByIdentity := map[string]int{}
	for i, item := range updated {
		rawIndexByIdentity[aiEvidenceIdentityKey(item)] = i
	}
	decisions := make([]aiPlannerContractAssessmentDecision, 0, len(assessments))
	for _, assessment := range assessments {
		decision, refIndexes := validateAIPlannerContractAssessment(contract, displayedEvidence, rawIndexByIdentity, assessment)
		decisions = append(decisions, decision)
		if !decision.Accepted {
			continue
		}
		for i := range updated {
			if len(updated[i].ContractKeyStatus) > 0 {
				delete(updated[i].ContractKeyStatus, decision.Key)
			}
		}
		if decision.Status == aiEvidenceCoverageMissing {
			continue
		}
		for _, rawIndex := range refIndexes {
			if updated[rawIndex].ContractKeyStatus == nil {
				updated[rawIndex].ContractKeyStatus = map[string]string{}
			}
			updated[rawIndex].ContractKeyStatus[decision.Key] = decision.Status
		}
	}
	return updated, decisions, nil
}

func validateAIPlannerContractAssessment(contract aiEvidenceContract, displayedEvidence []aiEvidence, rawIndexByIdentity map[string]int, assessment aiPlannerContractAssessment) (aiPlannerContractAssessmentDecision, []int) {
	key := strings.TrimSpace(assessment.Key)
	status := normalizeAIContractCoverageStatus(assessment.Status)
	decision := aiPlannerContractAssessmentDecision{
		Key:            key,
		Status:         status,
		Reason:         aiTaskFrameSafeText(assessment.Reason, 500),
		Accepted:       false,
		Source:         "model_planner",
		SupportingRefs: []string{},
	}
	fail := func(reason string) (aiPlannerContractAssessmentDecision, []int) {
		decision.RejectionReason = sanitizeProviderError(reason)
		return decision, nil
	}
	if key == "" {
		return fail("assessment key is required")
	}
	if !aiEvidenceContractHasKey(contract, key) {
		return fail("assessment key is not in current contract")
	}
	if !aiContractKeyRequiresModelAssessment(key) {
		return fail("assessment key is deterministic-only or does not require model assessment")
	}
	switch status {
	case aiEvidenceCoverageCovered, aiEvidenceCoveragePartial, aiEvidenceCoverageMissing, aiEvidenceCoverageConflict:
	default:
		return fail("assessment status must be covered, partial, missing, or conflict")
	}
	refNumbers, err := aiPlannerAssessmentRefNumbers(assessment.SupportingRefs)
	if err != nil {
		return fail(err.Error())
	}
	if status != aiEvidenceCoverageMissing && len(refNumbers) == 0 {
		return fail("supporting_refs are required for covered, partial, or conflict assessment")
	}
	refIndexes := make([]int, 0, len(refNumbers))
	for _, ref := range refNumbers {
		if ref <= 0 || ref > len(displayedEvidence) {
			return fail("supporting ref is not present in displayed evidence")
		}
		displayed := displayedEvidence[ref-1]
		if aiPlannerAssessmentRejectsSchemaOnlyRef(key, displayed) {
			return fail("runtime value-flow keys require runtime evidence, not schema-only evidence")
		}
		rawIndex, ok := rawIndexByIdentity[aiEvidenceIdentityKey(displayed)]
		if !ok {
			return fail("supporting ref cannot be mapped to raw evidence")
		}
		refIndexes = append(refIndexes, rawIndex)
		decision.SupportingRefs = append(decision.SupportingRefs, fmt.Sprintf("[C%d]", ref))
	}
	decision.Accepted = true
	return decision, refIndexes
}

func aiPlannerAssessmentRefNumbers(values []string) ([]int, error) {
	seen := map[int]struct{}{}
	out := []int{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		trimmed = strings.TrimPrefix(strings.TrimSuffix(trimmed, "]"), "[")
		trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "C"), "c")
		if trimmed == "" {
			continue
		}
		number, err := strconv.Atoi(trimmed)
		if err != nil || number <= 0 {
			return nil, fmt.Errorf("supporting_refs must use [C#] references")
		}
		if _, ok := seen[number]; ok {
			continue
		}
		seen[number] = struct{}{}
		out = append(out, number)
	}
	return out, nil
}

func aiPlannerAssessmentRejectsSchemaOnlyRef(key string, evidence aiEvidence) bool {
	if !aiContractKeyRequiresRuntimeAssessmentEvidence(key) {
		return false
	}
	return evidence.EvidenceType == "orm_model" || evidence.EvidenceType == "migration_sql"
}

func aiPlannerRepoAllowed(repoID int64, scope AIQuestionScope) bool {
	scope = normalizeAIScope(scope)
	if scope.CurrentFile != nil && scope.CurrentFile.RepoID > 0 && scope.CurrentFile.RepoID != repoID {
		return false
	}
	if len(scope.RepoIDs) == 0 || scope.RepoMode == "global" {
		return true
	}
	for _, allowed := range scope.RepoIDs {
		if allowed == repoID {
			return true
		}
	}
	return false
}

func sanitizeAIPlannerFileTypes(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
		if value == "" || value == "all" {
			return nil
		}
		if len(value) > 12 {
			continue
		}
		ok := true
		for _, r := range value {
			if r < 'a' || r > 'z' {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, value)
		}
	}
	return uniqueStrings(out)
}

func aiPlannerToolRegistry() []map[string]any {
	return []map[string]any{
		{"name": aiPlannerToolSearchCodeEvidence, "mode": "read_only", "description": "Search indexed Git code/docs and return cited snippets."},
		{"name": aiPlannerToolViewFileSlice, "mode": "read_only", "description": "Read a bounded slice of an indexed file by repo/ref/path/lines."},
		{"name": aiPlannerToolReadDiagnostics, "mode": "read_only", "description": "Read sanitized DocHarbor AI diagnostics only."},
		{"name": aiPlannerToolAssessContract, "mode": "read_only", "description": "Assess displayed evidence against current contract keys with cited refs."},
		{"name": aiPlannerActionFinish, "mode": "planner", "description": "Finish planning after enough evidence or known gaps."},
	}
}

func aiPlannerSafetyPolicy() []string {
	return []string{
		"read_only_tools",
		"no_sql_execution",
		"no_shell_execution",
		"no_git_writes",
		"no_external_network_tools",
		"no_cache_refresh",
		"no_secret_exposure",
	}
}

func buildAIPlannerToolRegistryStep() AIAgentStep {
	now := nowString()
	return AIAgentStep{
		AgentName:  "planner",
		StepType:   "tool_registry",
		Status:     "success",
		InputJSON:  encodeJSON(map[string]any{"safety_policy": aiPlannerSafetyPolicy()}),
		OutputJSON: encodeJSON(map[string]any{"tools": aiPlannerToolRegistry()}),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func buildAIPlannerActionStep(round int, state aiPlannerRunState, result aiModelResult, action aiPlannerAction, err error, latency time.Duration) AIAgentStep {
	status := "success"
	errText := ""
	if err != nil {
		status = "failed"
		errText = sanitizeProviderError(err.Error())
	}
	return AIAgentStep{
		AgentName:        "planner",
		StepType:         "model_call",
		Status:           status,
		ToolName:         action.Action,
		Model:            result.Model,
		ProviderName:     result.ProviderName,
		ModelRouteReason: result.ModelRouteJSON,
		InputJSON:        encodeJSON(map[string]any{"round": round, "state": summarizeAIPlannerState(state)}),
		OutputJSON:       encodeJSON(map[string]any{"round": round, "action": action}),
		TokenInput:       result.PromptTokens,
		TokenOutput:      result.CompletionTokens,
		LatencyMS:        int(latency.Milliseconds()),
		ErrorMessage:     errText,
		CreatedAt:        nowString(),
		FinishedAt:       nowString(),
	}
}

func buildAIPlannerToolResultStep(round int, tool, status string, output map[string]any) AIAgentStep {
	now := nowString()
	return AIAgentStep{
		AgentName:  "planner",
		StepType:   "tool_call",
		Status:     status,
		ToolName:   tool,
		InputJSON:  encodeJSON(map[string]any{"round": round, "tool": tool}),
		OutputJSON: encodeJSON(sanitizeAIDiagnosticsArtifact(output)),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func buildAIPlannerContractAssessmentStep(round int, decisions []aiPlannerContractAssessmentDecision, coverage aiContractCoverageReport) AIAgentStep {
	now := nowString()
	return AIAgentStep{
		AgentName:  "planner",
		StepType:   "contract_assessment",
		Status:     "success",
		ToolName:   aiPlannerToolAssessContract,
		InputJSON:  encodeJSON(map[string]any{"round": round, "source": "model_planner"}),
		OutputJSON: encodeJSON(sanitizeAIDiagnosticsArtifact(map[string]any{"assessments": decisions, "coverage": summarizeAIContractCoverageReport(coverage)})),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func summarizeAIPlannerContractAssessmentDecisions(decisions []aiPlannerContractAssessmentDecision) map[string]any {
	accepted := 0
	rejected := 0
	for _, decision := range decisions {
		if decision.Accepted {
			accepted++
		} else {
			rejected++
		}
	}
	return map[string]any{
		"accepted_count": accepted,
		"rejected_count": rejected,
		"assessments":    decisions,
	}
}

func summarizeAIPlannerState(state aiPlannerRunState) map[string]any {
	return map[string]any{
		"scope":                state.Scope,
		"task_frame":           state.Frame,
		"contract":             summarizeAIEvidenceContract(state.Contract),
		"observation_count":    len(state.Observations),
		"observation_bytes":    state.ObservationBytes,
		"contract_assessments": state.ContractAssessments,
	}
}

func summarizeAIPlannerEvidence(evidence []aiEvidence, limit int) []aiPlannerEvidenceSummary {
	if limit <= 0 || limit > len(evidence) {
		limit = len(evidence)
	}
	out := make([]aiPlannerEvidenceSummary, 0, limit)
	for i := 0; i < limit; i++ {
		item := evidence[i]
		c := item.Citation
		out = append(out, aiPlannerEvidenceSummary{
			Ref:               fmt.Sprintf("[C%d]", i+1),
			Repo:              item.Repo.Name,
			RepoID:            c.RepoID,
			Scope:             c.SourceScope,
			Branch:            c.Branch,
			Commit:            shortSHA(c.CommitSHA),
			FilePath:          c.FilePath,
			LineStart:         c.LineStart,
			LineEnd:           c.LineEnd,
			EvidenceType:      item.EvidenceType,
			ContractKeys:      append([]string(nil), item.ContractKeys...),
			ContractKeyStatus: aiCopyStringMap(item.ContractKeyStatus),
			Snippet:           truncate(item.Content, 500),
		})
	}
	return out
}

func buildAIPlannerRetrievalPlan(frame aiTaskFrame, contract aiEvidenceContract, scope AIQuestionScope, state aiPlannerRunState, rounds []aiRetrievalRoundPlan, curation aiEvidenceCurationResult, coverage *aiContractCoverageReport) map[string]any {
	plan := map[string]any{
		"intent":                    aiLegacyIntentForTaskFrame(frame),
		"planner_intent":            frame.Intent,
		"intent_source":             frame.IntentSource,
		"intent_reason":             frame.IntentReason,
		"explicit_database_request": frame.ExplicitDBRequest,
		"explicit_database_source":  frame.ExplicitDBSource,
		"explicit_database_reason":  frame.ExplicitDBReason,
		"source_mode":               scope.SourceMode,
		"repo_mode":                 scope.RepoMode,
		"terms":                     frame.KnownTerms,
		"generated_terms":           frame.GeneratedTerms,
		"chunker_version":           aiChunkerVersion,
		"candidate_branches":        strings.Contains(scope.SourceMode, "branch"),
		"raw_evidence_count":        len(curation.Annotations),
		"curated_evidence_count":    len(curation.Evidence),
		"excluded_evidence_count":   len(curation.ExcludedEvidence),
		"evidence_bundle":           curation.Bundle,
		"curator_coverage":          curation.Coverage,
		"retrieval_rounds":          rounds,
		"task_frame":                &frame,
		"evidence_contract":         summarizeAIEvidenceContract(contract),
		"tool_registry":             state.ToolRegistry,
		"safety_policy":             state.SafetyPolicy,
		"planner_observations":      state.Observations,
		"contract_assessments":      state.ContractAssessments,
	}
	if coverage != nil {
		plan["contract_coverage"] = summarizeAIContractCoverageReport(*coverage)
	}
	return plan
}

func aiPlannerObservationError(summary map[string]any) string {
	if value, ok := summary["error"].(string); ok {
		return value
	}
	return ""
}

func sortedAIPlannerActionNames() []string {
	names := []string{aiPlannerToolSearchCodeEvidence, aiPlannerToolViewFileSlice, aiPlannerToolReadDiagnostics, aiPlannerToolAssessContract, aiPlannerActionFinish}
	sort.Strings(names)
	return names
}
