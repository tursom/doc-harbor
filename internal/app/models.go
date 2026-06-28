package app

type Repository struct {
	ID                    int64      `json:"id"`
	Name                  string     `json:"name"`
	Slug                  string     `json:"slug"`
	RepoURL               string     `json:"repo_url"`
	DefaultBranch         string     `json:"default_branch"`
	TrackedBranches       []string   `json:"tracked_branches"`
	LatestIncludeBranches []string   `json:"latest_include_branches"`
	LatestExcludeBranches []string   `json:"latest_exclude_branches"`
	StaleBranchDays       int        `json:"stale_branch_days"`
	BranchPriority        []string   `json:"branch_priority"`
	CredentialRef         string     `json:"credential_ref"`
	Enabled               bool       `json:"enabled"`
	SyncIntervalSeconds   int        `json:"sync_interval_seconds"`
	MaxFileSizeBytes      int64      `json:"max_file_size_bytes"`
	CreatedAt             string     `json:"created_at"`
	UpdatedAt             string     `json:"updated_at"`
	ScanPaths             []ScanPath `json:"scan_paths"`
	LatestScan            *ScanRun   `json:"latest_scan,omitempty"`
}

type ScanPath struct {
	ID           int64    `json:"id"`
	RepoID       int64    `json:"repo_id"`
	Path         string   `json:"path"`
	IncludeGlobs []string `json:"include_globs"`
	ExcludeGlobs []string `json:"exclude_globs"`
	Enabled      bool     `json:"enabled"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type RepoRef struct {
	ID            int64  `json:"id"`
	RepoID        int64  `json:"repo_id"`
	RefType       string `json:"ref_type"`
	RefName       string `json:"ref_name"`
	CommitSHA     string `json:"commit_sha"`
	CommitTime    string `json:"commit_time"`
	LastScannedAt string `json:"last_scanned_at"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type Document struct {
	ID                int64  `json:"id"`
	RepoID            int64  `json:"repo_id"`
	ScanPath          string `json:"scan_path"`
	DocKey            string `json:"doc_key"`
	CurrentTitle      string `json:"current_title"`
	CurrentPath       string `json:"current_path"`
	Status            string `json:"status"`
	CreatedFromBranch string `json:"created_from_branch"`
	CreatedFromCommit string `json:"created_from_commit"`
	LatestVersionID   int64  `json:"latest_version_id"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type DocVersion struct {
	ID                 int64  `json:"id"`
	RepoID             int64  `json:"repo_id"`
	DocumentID         int64  `json:"document_id"`
	Branch             string `json:"branch"`
	HeadCommitSHA      string `json:"head_commit_sha"`
	ScanPath           string `json:"scan_path"`
	FilePath           string `json:"file_path"`
	PreviousPath       string `json:"previous_path"`
	DirPath            string `json:"dir_path"`
	FileName           string `json:"file_name"`
	Extension          string `json:"extension"`
	MimeType           string `json:"mime_type"`
	FileSize           int64  `json:"file_size"`
	BlobSHA            string `json:"blob_sha"`
	Status             string `json:"status"`
	Title              string `json:"title"`
	Previewable        bool   `json:"previewable"`
	DownloadEnabled    bool   `json:"download_enabled"`
	LastCommitSHA      string `json:"last_commit_sha"`
	LastCommitTime     string `json:"last_commit_time"`
	DeleteCommitSHA    string `json:"delete_commit_sha"`
	DeleteCommitTime   string `json:"delete_commit_time"`
	RenameScore        int    `json:"rename_score"`
	ParticipatesLatest bool   `json:"participates_latest"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	SourceBranch       string `json:"source_branch,omitempty"`
	SourceCommitSHA    string `json:"source_commit_sha,omitempty"`
	SelectionReason    string `json:"selection_reason,omitempty"`
}

type DocLatest struct {
	ID              int64  `json:"id"`
	RepoID          int64  `json:"repo_id"`
	DocumentID      int64  `json:"document_id"`
	VersionID       int64  `json:"version_id"`
	SourceBranch    string `json:"source_branch"`
	SourceCommitSHA string `json:"source_commit_sha"`
	FilePath        string `json:"file_path"`
	DirPath         string `json:"dir_path"`
	FileName        string `json:"file_name"`
	LastCommitTime  string `json:"last_commit_time"`
	SelectionReason string `json:"selection_reason"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type PathEvent struct {
	ID          int64  `json:"id"`
	RepoID      int64  `json:"repo_id"`
	DocumentID  int64  `json:"document_id"`
	Branch      string `json:"branch"`
	EventType   string `json:"event_type"`
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	CommitSHA   string `json:"commit_sha"`
	CommitTime  string `json:"commit_time"`
	RenameScore int    `json:"rename_score"`
	CreatedAt   string `json:"created_at"`
}

type ScanRun struct {
	ID           int64  `json:"id"`
	RepoID       int64  `json:"repo_id"`
	TriggerType  string `json:"trigger_type"`
	Status       string `json:"status"`
	BranchCount  int    `json:"branch_count"`
	FileCount    int    `json:"file_count"`
	SkippedCount int    `json:"skipped_count"`
	ErrorCount   int    `json:"error_count"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	ErrorMessage string `json:"error_message"`
	DetailJSON   string `json:"detail_json"`
}

type FileEntry struct {
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	DocumentID      int64  `json:"document_id,omitempty"`
	VersionID       int64  `json:"version_id,omitempty"`
	Title           string `json:"title,omitempty"`
	Extension       string `json:"extension,omitempty"`
	FileSize        int64  `json:"file_size,omitempty"`
	Status          string `json:"status,omitempty"`
	SourceBranch    string `json:"source_branch,omitempty"`
	SourceCommitSHA string `json:"source_commit_sha,omitempty"`
	LastCommitTime  string `json:"last_commit_time,omitempty"`
	Previewable     bool   `json:"previewable,omitempty"`
	DownloadEnabled bool   `json:"download_enabled,omitempty"`
	SelectionReason string `json:"selection_reason,omitempty"`
}

type FileContent struct {
	VersionID       int64        `json:"version_id"`
	DocumentID      int64        `json:"document_id"`
	RepoID          int64        `json:"repo_id"`
	Branch          string       `json:"branch"`
	FilePath        string       `json:"file_path"`
	Title           string       `json:"title"`
	Extension       string       `json:"extension"`
	MimeType        string       `json:"mime_type"`
	FileSize        int64        `json:"file_size"`
	BlobSHA         string       `json:"blob_sha"`
	SourceCommitSHA string       `json:"source_commit_sha"`
	LastCommitTime  string       `json:"last_commit_time"`
	Previewable     bool         `json:"previewable"`
	DownloadEnabled bool         `json:"download_enabled"`
	Content         string       `json:"content,omitempty"`
	TooLarge        bool         `json:"too_large"`
	Versions        []DocVersion `json:"versions,omitempty"`
}

type CommitSummary struct {
	SHA         string   `json:"sha"`
	Parents     []string `json:"parents"`
	Author      string   `json:"author"`
	AuthorEmail string   `json:"author_email"`
	CommitTime  string   `json:"commit_time"`
	Decorations string   `json:"decorations"`
	Message     string   `json:"message"`
}

type CommitFileChange struct {
	Status      string `json:"status"`
	Path        string `json:"path"`
	OldPath     string `json:"old_path,omitempty"`
	NewPath     string `json:"new_path,omitempty"`
	RenameScore int    `json:"rename_score,omitempty"`
}

type CommitDetail struct {
	CommitSummary
	Files []CommitFileChange `json:"files"`
}

type AISecret struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	SecretType      string `json:"secret_type"`
	Fingerprint     string `json:"fingerprint"`
	Last4           string `json:"last4"`
	CreatedByViewer string `json:"created_by_viewer"`
	UpdatedByViewer string `json:"updated_by_viewer"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type AIConfigVersion struct {
	ID                   int64        `json:"id"`
	Version              int          `json:"version"`
	Status               string       `json:"status"`
	ConfigHash           string       `json:"config_hash"`
	Config               AIConfigData `json:"config"`
	SecretRefs           []int64      `json:"secret_refs"`
	ValidationStatus     string       `json:"validation_status"`
	ValidationReportJSON string       `json:"validation_report_json"`
	CreatedByViewer      string       `json:"created_by_viewer"`
	PublishedByViewer    string       `json:"published_by_viewer"`
	CreatedAt            string       `json:"created_at"`
	UpdatedAt            string       `json:"updated_at"`
	PublishedAt          string       `json:"published_at"`
	SupersededAt         string       `json:"superseded_at"`
	ErrorMessage         string       `json:"error_message"`
}

type AIConfigData struct {
	Enabled  bool            `json:"enabled"`
	Viewer   AIViewerConfig  `json:"viewer"`
	History  AIHistoryConfig `json:"history"`
	Chat     AIChatConfig    `json:"chat"`
	Indexing AIIndexConfig   `json:"indexing"`
	Memory   AIMemoryConfig  `json:"memory"`
}

type AIViewerConfig struct {
	HeaderCandidates []string `json:"header_candidates"`
}

type AIHistoryConfig struct {
	RetentionDays int `json:"retention_days"`
}

type AIChatConfig struct {
	TimeoutSeconds   int          `json:"timeout_seconds"`
	MaxContextChunks int          `json:"max_context_chunks"`
	Routing          AIRouting    `json:"routing"`
	Providers        []AIProvider `json:"providers"`
}

type AIRouting struct {
	DefaultTaskClass string                 `json:"default_task_class"`
	TaskClasses      map[string]AITaskRoute `json:"task_classes"`
	Escalation       map[string]string      `json:"escalation"`
}

type AITaskRoute struct {
	Providers         []string `json:"providers"`
	FallbackTaskClass string   `json:"fallback_task_class"`
}

type AIProvider struct {
	ProviderKey           string `json:"provider_key"`
	Name                  string `json:"name"`
	Priority              int    `json:"priority"`
	ProviderType          string `json:"provider_type"`
	BaseURL               string `json:"base_url"`
	APIKeySecretID        int64  `json:"api_key_secret_id"`
	Model                 string `json:"model"`
	CostTier              string `json:"cost_tier"`
	RequestTimeoutSeconds int    `json:"request_timeout_seconds"`
	MaxRPM                int    `json:"max_rpm"`
	SecretConfigured      bool   `json:"secret_configured,omitempty"`
	SecretLast4           string `json:"secret_last4,omitempty"`
	SecretFingerprint     string `json:"secret_fingerprint,omitempty"`
}

type AIIndexConfig struct {
	DefaultScanRoots []string `json:"default_scan_roots"`
	ExcludeGlobs     []string `json:"exclude_globs"`
	MaxFileSize      int64    `json:"max_file_size"`
}

type AIMemoryConfig struct {
	Enabled         bool    `json:"enabled"`
	Use             bool    `json:"use"`
	Generate        bool    `json:"generate"`
	ReviewRequired  bool    `json:"review_required"`
	MaxContextItems int     `json:"max_context_items"`
	MinConfidence   float64 `json:"min_confidence"`
	RetentionDays   int     `json:"retention_days"`
}

type AISession struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	ViewerKey  string `json:"viewer_key"`
	ScopeJSON  string `json:"scope_json"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	ArchivedAt string `json:"archived_at"`
}

type AIMessage struct {
	ID               int64  `json:"id"`
	SessionID        int64  `json:"session_id"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	Model            string `json:"model"`
	ProviderName     string `json:"provider_name"`
	ModelRouteJSON   string `json:"model_route_json"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	LatencyMS        int    `json:"latency_ms"`
	Status           string `json:"status"`
	ErrorMessage     string `json:"error_message"`
	CreatedAt        string `json:"created_at"`
}

type AIAgentRun struct {
	ID                     int64  `json:"id"`
	SessionID              int64  `json:"session_id"`
	UserMessageID          int64  `json:"user_message_id"`
	AssistantMessageID     int64  `json:"assistant_message_id"`
	Status                 string `json:"status"`
	CurrentState           string `json:"current_state"`
	Intent                 string `json:"intent"`
	ScopeJSON              string `json:"scope_json"`
	RetrievalPlanJSON      string `json:"retrieval_plan_json"`
	ServiceCandidateCount  int    `json:"service_candidate_count"`
	EvidenceCount          int    `json:"evidence_count"`
	CodeEvidenceCount      int    `json:"code_evidence_count"`
	MemoryCount            int    `json:"memory_count"`
	UnconfirmedCount       int    `json:"unconfirmed_count"`
	VerificationStatus     string `json:"verification_status"`
	VerificationReportJSON string `json:"verification_report_json"`
	CheckpointJSON         string `json:"checkpoint_json"`
	IndexSnapshotID        int64  `json:"index_snapshot_id"`
	ConfigVersion          int    `json:"config_version"`
	ConfigHash             string `json:"config_hash"`
	Model                  string `json:"model"`
	ProviderName           string `json:"provider_name"`
	ProviderFailoverJSON   string `json:"provider_failover_json"`
	ModelRouteJSON         string `json:"model_route_json"`
	EscalationCount        int    `json:"escalation_count"`
	EstimatedCostJSON      string `json:"estimated_cost_json"`
	StartedAt              string `json:"started_at"`
	FinishedAt             string `json:"finished_at"`
	ErrorMessage           string `json:"error_message"`
}

type AIAgentStep struct {
	ID                  int64  `json:"id"`
	RunID               int64  `json:"run_id"`
	ParentStepID        int64  `json:"parent_step_id"`
	AgentName           string `json:"agent_name"`
	StepType            string `json:"step_type"`
	Status              string `json:"status"`
	ToolName            string `json:"tool_name"`
	TaskClass           string `json:"task_class"`
	Model               string `json:"model"`
	ProviderName        string `json:"provider_name"`
	ModelRouteReason    string `json:"model_route_reason"`
	EscalatedFromStepID int64  `json:"escalated_from_step_id"`
	InputJSON           string `json:"input_json"`
	OutputJSON          string `json:"output_json"`
	TokenInput          int    `json:"token_input"`
	TokenOutput         int    `json:"token_output"`
	EstimatedCost       string `json:"estimated_cost"`
	LatencyMS           int    `json:"latency_ms"`
	ErrorMessage        string `json:"error_message"`
	CreatedAt           string `json:"created_at"`
	FinishedAt          string `json:"finished_at"`
}

type AIServiceCandidate struct {
	ID               int64    `json:"id"`
	RunID            int64    `json:"run_id"`
	MessageID        int64    `json:"message_id"`
	ServiceProfileID int64    `json:"service_profile_id"`
	RepoID           int64    `json:"repo_id"`
	RepoName         string   `json:"repo_name,omitempty"`
	ServiceName      string   `json:"service_name"`
	MatchedTerms     []string `json:"matched_terms"`
	Confidence       string   `json:"confidence"`
	Reason           string   `json:"reason"`
	Score            float64  `json:"score"`
	EvidenceCount    int      `json:"evidence_count"`
	CreatedAt        string   `json:"created_at"`
}

type AIMessageCitation struct {
	ID              int64   `json:"id"`
	MessageID       int64   `json:"message_id"`
	IndexSnapshotID int64   `json:"index_snapshot_id"`
	ChunkID         int64   `json:"chunk_id"`
	APISymbolID     int64   `json:"api_symbol_id"`
	RepoID          int64   `json:"repo_id"`
	RepoName        string  `json:"repo_name,omitempty"`
	VersionID       int64   `json:"version_id"`
	SourceScope     string  `json:"source_scope"`
	Branch          string  `json:"branch"`
	CommitSHA       string  `json:"commit_sha"`
	FilePath        string  `json:"file_path"`
	LineStart       int     `json:"line_start"`
	LineEnd         int     `json:"line_end"`
	QuoteText       string  `json:"quote_text"`
	Score           float64 `json:"score"`
	CreatedAt       string  `json:"created_at"`
}

type AIQuestionScope struct {
	RepoMode    string              `json:"repo_mode"`
	RepoIDs     []int64             `json:"repo_ids"`
	SourceMode  string              `json:"source_mode"`
	FileTypes   []string            `json:"file_types"`
	CurrentFile *AICurrentFileScope `json:"current_file,omitempty"`
}

type AICurrentFileScope struct {
	RepoID    int64  `json:"repo_id"`
	VersionID int64  `json:"version_id"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commit_sha"`
	FilePath  string `json:"file_path"`
}
