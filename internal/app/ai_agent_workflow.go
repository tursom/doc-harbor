package app

const (
	aiAgentWorkflowVersionV2Shadow = "v2-shadow"
	aiAgentWorkflowVersionV2Active = "v2-active"
)

type aiAgentWorkflowFailurePolicy struct {
	Record   string `json:"record"`
	Fallback string `json:"fallback"`
}

type aiAgentWorkflowPolicy struct {
	Version                string                                  `json:"agent_workflow_version"`
	Mode                   string                                  `json:"agent_workflow_mode"`
	AnswerMode             string                                  `json:"answer_mode"`
	ProtectedMainPath      []string                                `json:"protected_main_path"`
	SSECompatibilityEvents []string                                `json:"sse_compatibility_events"`
	SafetyBoundaries       []string                                `json:"safety_boundaries"`
	FailurePolicy          map[string]aiAgentWorkflowFailurePolicy `json:"failure_policy"`
}

func defaultAIAgentWorkflowPolicy() aiAgentWorkflowPolicy {
	return newAIAgentWorkflowPolicy(aiAgentWorkflowVersionV2Shadow)
}

func newAIAgentWorkflowPolicy(version string) aiAgentWorkflowPolicy {
	version = normalizeAIAgentWorkflowVersion(version)
	mode := "shadow"
	answerMode := "legacy"
	if version == aiAgentWorkflowVersionV2Active {
		mode = "active"
		answerMode = "agent_workflow"
	}
	return aiAgentWorkflowPolicy{
		Version:    version,
		Mode:       mode,
		AnswerMode: answerMode,
		ProtectedMainPath: []string{
			"askAIQuestion",
			"askAIQuestionStream",
			"retrieveAIEvidence",
			"searchRepoSmartLatestEvidence",
			"searchRepoRefEvidence",
			"generateAIAnswer",
			"callRoutedAIModel",
			"callRoutedAIModelStream",
			"getAIDiagnosticsRunDetail",
			"sanitizeAIDiagnosticsSteps",
		},
		SSECompatibilityEvents: []string{
			"run_started",
			"stage",
			"provider_attempt",
			"citations",
			"answer_delta",
			"message_done",
		},
		SafetyBoundaries: []string{
			"read_only_tools",
			"no_shell_execution",
			"no_sql_execution",
			"no_git_writes",
			"no_external_network_tools",
			"no_provider_secret_exposure",
		},
		FailurePolicy: map[string]aiAgentWorkflowFailurePolicy{
			"task_frame":       {Record: "step_error", Fallback: "legacy_intent_and_retrieval"},
			"contract_builder": {Record: "step_error", Fallback: "legacy_answer"},
			"evidence_curator": {Record: "diagnostics", Fallback: "legacy_answer"},
			"contract_checker": {Record: "diagnostics", Fallback: "legacy_answer"},
			"answer_verifier":  {Record: "step_error", Fallback: "conservative_answer_or_local_evidence_summary"},
		},
	}
}

func normalizeAIAgentWorkflowVersion(version string) string {
	if version == aiAgentWorkflowVersionV2Active {
		return aiAgentWorkflowVersionV2Active
	}
	return aiAgentWorkflowVersionV2Shadow
}

func buildInitialAIAgentRunCheckpoint(scope AIQuestionScope, taskClass string) map[string]any {
	return buildAIAgentRunCheckpoint(scope, taskClass, aiQuestionPreparation{Scope: scope})
}

func buildAIAgentRunCheckpoint(scope AIQuestionScope, taskClass string, prepared aiQuestionPreparation) map[string]any {
	policy := defaultAIAgentWorkflowPolicy()
	if taskClass == "" {
		taskClass = "standard"
	}
	return map[string]any{
		"agent_workflow_version": policy.Version,
		"agent_workflow_mode":    policy.Mode,
		"answer_mode":            policy.AnswerMode,
		"task_class":             taskClass,
		"scope":                  scope,
		"conversation":           prepared.Conversation,
		"effective_question":     truncate(prepared.SearchQuestion, 800),
		"generated_terms":        prepared.GeneratedSearchTerms,
		"protected_main_path":    policy.ProtectedMainPath,
		"sse_compatibility":      policy.SSECompatibilityEvents,
		"safety_boundaries":      policy.SafetyBoundaries,
		"failure_policy":         policy.FailurePolicy,
	}
}
