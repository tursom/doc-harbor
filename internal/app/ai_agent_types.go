package app

type aiTaskFrame struct {
	Intent          string               `json:"intent"`
	UserGoal        string               `json:"user_goal"`
	AnswerShape     string               `json:"answer_shape"`
	ScopeStrategy   string               `json:"scope_strategy"`
	TargetArtifacts []string             `json:"target_artifacts"`
	MustNot         []string             `json:"must_not"`
	KnownTerms      []string             `json:"known_terms"`
	GeneratedTerms  []string             `json:"generated_terms"`
	FollowUp        *aiTaskFrameFollowUp `json:"follow_up,omitempty"`
}

type aiTaskFrameFollowUp struct {
	IsFollowUp           bool     `json:"is_follow_up"`
	PreviousPaths        []string `json:"previous_paths,omitempty"`
	PreviousTopicSummary string   `json:"previous_topic_summary,omitempty"`
}

type aiEvidenceContract struct {
	ContractID  string                  `json:"contract_id"`
	Intent      string                  `json:"intent,omitempty"`
	Required    []aiEvidenceRequirement `json:"required"`
	Recommended []aiEvidenceRequirement `json:"recommended"`
	Forbidden   []string                `json:"forbidden"`
}

type aiEvidenceRequirement struct {
	Key                   string   `json:"key"`
	Description           string   `json:"description"`
	AcceptedEvidenceTypes []string `json:"accepted_evidence_types,omitempty"`
}

type aiEvidenceBundle struct {
	BundleID string                `json:"bundle_id"`
	Coverage map[string]string     `json:"coverage"`
	Groups   []aiEvidenceGroup     `json:"groups"`
	Excluded []aiEvidenceExclusion `json:"excluded,omitempty"`
}

type aiEvidenceGroup struct {
	Key               string  `json:"key"`
	GroupKey          string  `json:"group_key,omitempty"`
	EvidenceIDs       []int64 `json:"evidence_ids"`
	Summary           string  `json:"summary,omitempty"`
	EvidenceType      string  `json:"evidence_type,omitempty"`
	SourceReliability string  `json:"source_reliability,omitempty"`
}

type aiEvidenceExclusion struct {
	EvidenceID        int64  `json:"evidence_id"`
	Reason            string `json:"reason"`
	GroupKey          string `json:"group_key,omitempty"`
	EvidenceType      string `json:"evidence_type,omitempty"`
	SourceReliability string `json:"source_reliability,omitempty"`
	RepoID            int64  `json:"repo_id,omitempty"`
	RepoName          string `json:"repo_name,omitempty"`
	FilePath          string `json:"file_path,omitempty"`
	SourceScope       string `json:"source_scope,omitempty"`
}

type aiContractCoverageItem struct {
	Key           string  `json:"key"`
	Requirement   string  `json:"requirement"`
	Status        string  `json:"status"`
	EvidenceIDs   []int64 `json:"evidence_ids"`
	Reason        string  `json:"reason"`
	MissingDetail string  `json:"missing_detail"`
	Confidence    float64 `json:"confidence"`
}

type aiContractCoverageReport struct {
	ContractID         string                   `json:"contract_id,omitempty"`
	Status             string                   `json:"status"`
	Coverage           map[string]string        `json:"coverage,omitempty"`
	Items              []aiContractCoverageItem `json:"items,omitempty"`
	Covered            []string                 `json:"covered"`
	Partial            []string                 `json:"partial"`
	MissingRequired    []string                 `json:"missing_required"`
	MissingRecommended []string                 `json:"missing_recommended,omitempty"`
	ForbiddenMatched   []string                 `json:"forbidden_matched,omitempty"`
	NextAction         string                   `json:"next_action"`
	Details            map[string]string        `json:"details,omitempty"`
}

type aiAnswerVerificationReport struct {
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
	Details      []string `json:"details"`
	PassedChecks []string `json:"passed_checks,omitempty"`
	FailedChecks []string `json:"failed_checks,omitempty"`
	NextAction   string   `json:"next_action"`
}

type aiRetrievalRoundPlan struct {
	Round               int                      `json:"round"`
	Intent              string                   `json:"intent,omitempty"`
	Reason              string                   `json:"reason"`
	MissingContractKeys []string                 `json:"missing_contract_keys,omitempty"`
	Searches            []aiRetrievalRoundSearch `json:"searches"`
	QuerySource         string                   `json:"query_source,omitempty"`
	PlannerStatus       string                   `json:"planner_status,omitempty"`
	NewEvidenceCount    int                      `json:"new_evidence_count"`
	CoverageDelta       map[string]string        `json:"coverage_delta"`
	NextAction          string                   `json:"next_action,omitempty"`
}

type aiRetrievalRoundSearch struct {
	Tool      string   `json:"tool"`
	Query     string   `json:"query"`
	FileTypes []string `json:"file_types,omitempty"`
	PathHints []string `json:"path_hints,omitempty"`
	Terms     []string `json:"terms,omitempty"`
}
