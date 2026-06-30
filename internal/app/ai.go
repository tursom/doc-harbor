package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const aiChunkerVersion = "local-keyword-v1"

type aiConfigRowScanner interface {
	Scan(dest ...any) error
}

type aiAskRequest struct {
	Question      string          `json:"question"`
	ScopeOverride AIQuestionScope `json:"scope_override"`
}

type aiSessionRequest struct {
	Title string          `json:"title"`
	Scope AIQuestionScope `json:"scope"`
}

type aiSessionPatchRequest struct {
	Title      string          `json:"title"`
	Scope      AIQuestionScope `json:"scope"`
	Archived   *bool           `json:"archived"`
	ArchivedAt string          `json:"archived_at"`
}

type aiSecretRequest struct {
	Name       string `json:"name"`
	SecretType string `json:"secret_type"`
	Value      string `json:"value"`
}

type aiProviderTestRequest struct {
	ProviderKey    string `json:"provider_key"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key"`
	APIKeySecretID int64  `json:"api_key_secret_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type aiDefaultProviderSettingsRequest struct {
	ProviderKey    string `json:"provider_key"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key"`
	Enable         bool   `json:"enable"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxRPM         int    `json:"max_rpm"`
	Priority       int    `json:"priority"`
	CostTier       string `json:"cost_tier"`
}

type aiSettingsEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

type aiSettingsApplyRequest struct {
	Enabled    *bool  `json:"enabled"`
	TestPolicy string `json:"test_policy"`
}

type aiProviderMutationRequest struct {
	Name           *string `json:"name"`
	BaseURL        *string `json:"base_url"`
	Model          *string `json:"model"`
	APIKey         *string `json:"api_key"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
	MaxRPM         *int    `json:"max_rpm"`
	Priority       *int    `json:"priority"`
	CostTier       *string `json:"cost_tier"`
	MakeDefault    *bool   `json:"make_default"`
	TestBeforeSave *bool   `json:"test_before_save"`
}

type aiSettingsResponse struct {
	Enabled                 bool                        `json:"enabled"`
	Status                  string                      `json:"status"`
	DefaultProviderKey      string                      `json:"default_provider_key"`
	DefaultProviderName     string                      `json:"default_provider_name"`
	DefaultModel            string                      `json:"default_model"`
	RouteProviderKeys       []string                    `json:"route_provider_keys"`
	RouteProviders          []aiSettingsRouteProvider   `json:"route_providers"`
	ActiveRouteProviderKeys []string                    `json:"active_route_provider_keys"`
	HasUnappliedChanges     bool                        `json:"has_unapplied_changes"`
	EncryptionReady         bool                        `json:"encryption_ready"`
	EditableStatus          string                      `json:"editable_status"`
	LastTest                *aiProviderTestSummary      `json:"last_test,omitempty"`
	Providers               []aiSettingsProviderSummary `json:"providers"`
}

type aiSettingsRouteProvider struct {
	ProviderKey string `json:"provider_key"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	Priority    int    `json:"priority"`
}

type aiSettingsProviderSummary struct {
	ProviderKey       string `json:"provider_key"`
	Name              string `json:"name"`
	ProviderType      string `json:"provider_type"`
	BaseURL           string `json:"base_url"`
	Model             string `json:"model"`
	APIKeyConfigured  bool   `json:"api_key_configured"`
	APIKeyLast4       string `json:"api_key_last4,omitempty"`
	IsDefault         bool   `json:"is_default"`
	RouteOrder        int    `json:"route_order"`
	Usable            bool   `json:"usable"`
	LastTestStatus    string `json:"last_test_status,omitempty"`
	LastTestMessage   string `json:"last_test_message,omitempty"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	MaxRPM            int    `json:"max_rpm"`
	Priority          int    `json:"priority"`
	CostTier          string `json:"cost_tier"`
	RequestTimeoutSec int    `json:"request_timeout_seconds"`
}

type aiProviderTestSummary struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	TestedAt  string `json:"tested_at,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

type aiProviderTestResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Model     string `json:"model"`
	LatencyMS int64  `json:"latency_ms"`
	SafeError string `json:"safe_error,omitempty"`
}

type aiSettingsSaveResponse struct {
	Enabled  bool                      `json:"enabled"`
	Provider aiSettingsProviderSummary `json:"provider"`
	Settings aiSettingsResponse        `json:"settings"`
	Message  string                    `json:"message"`
}

type aiSettingsApplyResponse struct {
	Enabled  bool               `json:"enabled"`
	Settings aiSettingsResponse `json:"settings"`
	Message  string             `json:"message"`
}

type aiProviderMutationResponse struct {
	Provider aiSettingsProviderSummary `json:"provider"`
	Settings aiSettingsResponse        `json:"settings"`
	Message  string                    `json:"message"`
}

type aiProviderDeleteResponse struct {
	DeletedProviderKey string             `json:"deleted_provider_key"`
	Settings           aiSettingsResponse `json:"settings"`
	Message            string             `json:"message"`
}

type AINotice struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type aiQuestionResult struct {
	Run               AIAgentRun           `json:"run"`
	Message           AIMessage            `json:"message"`
	ServiceCandidates []AIServiceCandidate `json:"service_candidates"`
	Citations         []AIMessageCitation  `json:"citations"`
	Notice            *AINotice            `json:"notice,omitempty"`
}

type aiMessagesResponse struct {
	Items             []AIMessage          `json:"items"`
	ServiceCandidates []AIServiceCandidate `json:"service_candidates"`
	Citations         []AIMessageCitation  `json:"citations"`
}

type aiHistorySessionsResponse struct {
	Items      []AISession `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

type aiHistorySessionDetailResponse struct {
	Session           AISession            `json:"session"`
	Messages          []AIMessage          `json:"messages"`
	ServiceCandidates []AIServiceCandidate `json:"service_candidates"`
	Citations         []AIMessageCitation  `json:"citations"`
}

type aiHistorySessionCursor struct {
	UpdatedAt string `json:"updated_at"`
	ID        int64  `json:"id"`
}

type aiDiagnosticsRunsResponse struct {
	Items      []aiDiagnosticsRunSummary `json:"items"`
	NextCursor string                    `json:"next_cursor,omitempty"`
}

type aiDiagnosticsRunSummary struct {
	Run                AIAgentRun `json:"run"`
	Session            AISession  `json:"session"`
	UserMessageID      int64      `json:"user_message_id"`
	UserQuestion       string     `json:"user_question"`
	AssistantMessageID int64      `json:"assistant_message_id"`
	AssistantStatus    string     `json:"assistant_status"`
	DurationMS         int64      `json:"duration_ms"`
}

type aiDiagnosticsRunDetailResponse struct {
	Session           AISession            `json:"session"`
	UserMessage       AIMessage            `json:"user_message"`
	AssistantMessage  AIMessage            `json:"assistant_message"`
	Run               AIAgentRun           `json:"run"`
	TaskFrame         any                  `json:"task_frame,omitempty"`
	EvidenceContract  any                  `json:"evidence_contract,omitempty"`
	ContractCoverage  any                  `json:"contract_coverage,omitempty"`
	AgentWorkflow     any                  `json:"agent_workflow,omitempty"`
	Steps             []aiDiagnosticsStep  `json:"steps"`
	DataSources       aiDiagnosticsSources `json:"data_sources"`
	ServiceCandidates []AIServiceCandidate `json:"service_candidates"`
	Citations         []AIMessageCitation  `json:"citations"`
}

type aiDiagnosticsSourcesResponse struct {
	DataSources aiDiagnosticsSources `json:"data_sources"`
}

type aiDiagnosticsSources struct {
	Scope        AIQuestionScope                 `json:"scope"`
	Indexing     aiDiagnosticsIndexingSummary    `json:"indexing"`
	Repositories []aiDiagnosticsRepositorySource `json:"repositories"`
	CurrentFile  *AICurrentFileScope             `json:"current_file,omitempty"`
}

type aiDiagnosticsIndexingSummary struct {
	DefaultScanRoots []string `json:"default_scan_roots"`
	ExcludeGlobs     []string `json:"exclude_globs"`
	MaxFileSize      int64    `json:"max_file_size"`
}

type aiDiagnosticsRepositorySource struct {
	ID                    int64                       `json:"id"`
	Name                  string                      `json:"name"`
	Slug                  string                      `json:"slug"`
	Enabled               bool                        `json:"enabled"`
	DefaultBranch         string                      `json:"default_branch"`
	TrackedBranches       []string                    `json:"tracked_branches"`
	LatestIncludeBranches []string                    `json:"latest_include_branches"`
	LatestExcludeBranches []string                    `json:"latest_exclude_branches"`
	StaleBranchDays       int                         `json:"stale_branch_days"`
	BranchPriority        []string                    `json:"branch_priority"`
	SyncIntervalSeconds   int                         `json:"sync_interval_seconds"`
	MaxFileSizeBytes      int64                       `json:"max_file_size_bytes"`
	ScanPaths             []aiDiagnosticsScanPath     `json:"scan_paths"`
	DefaultTarget         *aiDiagnosticsBranchTarget  `json:"default_target,omitempty"`
	CandidateTargets      []aiDiagnosticsBranchTarget `json:"candidate_targets"`
	LatestScan            *aiDiagnosticsScanRun       `json:"latest_scan,omitempty"`
	CreatedAt             string                      `json:"created_at"`
	UpdatedAt             string                      `json:"updated_at"`
}

type aiDiagnosticsScanPath struct {
	ID           int64    `json:"id"`
	Path         string   `json:"path"`
	IncludeGlobs []string `json:"include_globs"`
	ExcludeGlobs []string `json:"exclude_globs"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type aiDiagnosticsBranchTarget struct {
	Branch        string `json:"branch"`
	CommitSHA     string `json:"commit_sha"`
	CommitTime    string `json:"commit_time"`
	LastScannedAt string `json:"last_scanned_at"`
	SourceScope   string `json:"source_scope"`
}

type aiDiagnosticsScanRun struct {
	ID           int64  `json:"id"`
	TriggerType  string `json:"trigger_type"`
	Status       string `json:"status"`
	BranchCount  int    `json:"branch_count"`
	FileCount    int    `json:"file_count"`
	SkippedCount int    `json:"skipped_count"`
	ErrorCount   int    `json:"error_count"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
}

type aiDiagnosticsStep struct {
	ID                  int64          `json:"id"`
	RunID               int64          `json:"run_id"`
	ParentStepID        int64          `json:"parent_step_id"`
	AgentName           string         `json:"agent_name"`
	StepType            string         `json:"step_type"`
	Status              string         `json:"status"`
	ToolName            string         `json:"tool_name"`
	TaskClass           string         `json:"task_class"`
	Model               string         `json:"model"`
	ProviderName        string         `json:"provider_name"`
	ModelRouteReason    string         `json:"model_route_reason"`
	EscalatedFromStepID int64          `json:"escalated_from_step_id"`
	TokenInput          int            `json:"token_input"`
	TokenOutput         int            `json:"token_output"`
	EstimatedCost       string         `json:"estimated_cost"`
	LatencyMS           int            `json:"latency_ms"`
	ErrorMessage        string         `json:"error_message"`
	Input               any            `json:"input,omitempty"`
	Output              any            `json:"output,omitempty"`
	Summary             map[string]any `json:"summary,omitempty"`
	CreatedAt           string         `json:"created_at"`
	FinishedAt          string         `json:"finished_at"`
}

type aiDiagnosticsRunQuery struct {
	Limit         int
	Cursor        string
	Viewer        string
	SessionID     int64
	Status        string
	Q             string
	StartedAfter  string
	StartedBefore string
}

type aiDiagnosticsRunCursor struct {
	StartedAt string `json:"started_at"`
	ID        int64  `json:"id"`
}

type aiStreamStageEvent struct {
	Stage          string `json:"stage"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	EvidenceCount  int    `json:"evidence_count,omitempty"`
	CandidateCount int    `json:"candidate_count,omitempty"`
}

type aiStreamProviderAttemptEvent struct {
	Attempt     int    `json:"attempt"`
	TaskClass   string `json:"task_class,omitempty"`
	ProviderKey string `json:"provider_key"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

type aiStreamAnswerDeltaEvent struct {
	MessageID int64  `json:"message_id"`
	Delta     string `json:"delta"`
}

type aiStreamEmitFunc func(event string, data any) error

type aiStreamModelCallbacks struct {
	ProviderAttempt func(aiStreamProviderAttemptEvent) error
	AnswerDelta     func(delta string) error
}

type aiRetrievalResult struct {
	Intent              string
	Scope               AIQuestionScope
	Plan                map[string]any
	Evidence            []aiEvidence
	ServiceCandidates   []AIServiceCandidate
	Conversation        aiConversationContext
	TaskFrame           *aiTaskFrame              `json:"task_frame,omitempty"`
	Contract            *aiEvidenceContract       `json:"contract,omitempty"`
	EvidenceBundle      *aiEvidenceBundle         `json:"evidence_bundle,omitempty"`
	Coverage            *aiContractCoverageReport `json:"coverage,omitempty"`
	ContractCoverage    *aiContractCoverageReport `json:"contract_coverage,omitempty"`
	AnswerPolicy        *aiAnswerPolicy           `json:"answer_policy,omitempty"`
	AnswerComposer      *aiAnswerComposerSummary  `json:"answer_composer,omitempty"`
	Rounds              []aiRetrievalRoundPlan    `json:"rounds,omitempty"`
	Curation            *aiEvidenceCurationResult `json:"-"`
	RetrievalRoundSteps []AIAgentStep             `json:"-"`
}

type aiConversationContext struct {
	FollowUp                 bool     `json:"follow_up"`
	PreviousUserQuestion     string   `json:"previous_user_question,omitempty"`
	PreviousAssistantSummary string   `json:"previous_assistant_summary,omitempty"`
	PreviousCitationPaths    []string `json:"previous_citation_paths,omitempty"`
	FocusRepoIDs             []int64  `json:"focus_repo_ids,omitempty"`
}

type aiQuestionPreparation struct {
	SearchQuestion       string
	Scope                AIQuestionScope
	Conversation         aiConversationContext
	GeneratedSearchTerms []string
	TaskFrame            *aiTaskFrame
	Contract             *aiEvidenceContract
	EvidenceBundle       *aiEvidenceBundle
	Coverage             *aiContractCoverageReport
	ContractCoverage     *aiContractCoverageReport
	AnswerPolicy         *aiAnswerPolicy
	AnswerComposer       *aiAnswerComposerSummary
}

type aiEvidence struct {
	Citation          AIMessageCitation
	Repo              Repository
	Content           string
	MatchedTerms      []string
	Score             float64
	EvidenceType      string   `json:"evidence_type,omitempty"`
	SourceReliability string   `json:"source_reliability,omitempty"`
	ContractKeys      []string `json:"contract_keys,omitempty"`
	ExcludedReason    string   `json:"excluded_reason,omitempty"`
	GroupKey          string   `json:"group_key,omitempty"`
}

type aiRefTarget struct {
	Branch      string
	CommitSHA   string
	SourceScope string
}

type aiFileCandidate struct {
	Entry    treeEntry
	PreScore float64
	Terms    []string
}

type aiModelResult struct {
	Content          string
	ProviderName     string
	Model            string
	ModelRouteJSON   string
	PromptTokens     int
	CompletionTokens int
	LatencyMS        int
	FailoverJSON     string
}

type aiRouteError struct {
	Message      string
	AttemptOrder []string
	Failures     []map[string]any
}

func (e aiRouteError) Error() string {
	return e.Message
}

type aiStreamPartialError struct {
	Message string
}

func (e aiStreamPartialError) Error() string {
	return e.Message
}

type aiChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []aiChatMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type aiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiChatCompletionResponse struct {
	Choices []struct {
		Message aiChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type aiChatCompletionStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type aiChatCompletionStreamResult struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

func defaultAIConfig() AIConfigData {
	return AIConfigData{
		Enabled: false,
		Viewer:  AIViewerConfig{HeaderCandidates: []string{"Remote-Email", "Remote-User", "X-Forwarded-User", "Remote-Name"}},
		History: AIHistoryConfig{
			RetentionDays: 0,
		},
		Chat: AIChatConfig{
			TimeoutSeconds:   60,
			MaxContextChunks: 24,
			Routing: AIRouting{
				DefaultTaskClass: "standard",
				TaskClasses: map[string]AITaskRoute{
					"standard": {Providers: []string{}, FallbackTaskClass: ""},
				},
				Escalation: map[string]string{},
			},
			Providers: []AIProvider{},
		},
		Indexing: AIIndexConfig{
			DefaultScanRoots: []string{"."},
			ExcludeGlobs:     []string{".git/**", "node_modules/**", "vendor/**", "dist/**", "build/**", ".cache/**", "tmp/**", "*.min.js", "package-lock.json"},
			MaxFileSize:      1024 * 1024,
		},
		Memory: AIMemoryConfig{
			Enabled:         false,
			Use:             true,
			Generate:        false,
			ReviewRequired:  true,
			MaxContextItems: 8,
			MinConfidence:   0.75,
			RetentionDays:   365,
		},
	}
}

func normalizeAIConfig(cfg AIConfigData) AIConfigData {
	defaults := defaultAIConfig()
	if cfg.Viewer.HeaderCandidates == nil {
		cfg.Viewer.HeaderCandidates = defaults.Viewer.HeaderCandidates
	}
	if cfg.Chat.TimeoutSeconds <= 0 {
		cfg.Chat.TimeoutSeconds = defaults.Chat.TimeoutSeconds
	}
	if cfg.Chat.MaxContextChunks <= 0 {
		cfg.Chat.MaxContextChunks = defaults.Chat.MaxContextChunks
	}
	if cfg.Chat.Routing.DefaultTaskClass == "" {
		cfg.Chat.Routing.DefaultTaskClass = defaults.Chat.Routing.DefaultTaskClass
	}
	if cfg.Chat.Routing.TaskClasses == nil {
		cfg.Chat.Routing.TaskClasses = defaults.Chat.Routing.TaskClasses
	}
	if cfg.Chat.Routing.Escalation == nil {
		cfg.Chat.Routing.Escalation = defaults.Chat.Routing.Escalation
	}
	if cfg.Chat.Providers == nil {
		cfg.Chat.Providers = defaults.Chat.Providers
	}
	routeNameToKey := map[string]string{}
	existingKeys := map[string]struct{}{}
	for i := range cfg.Chat.Providers {
		provider := &cfg.Chat.Providers[i]
		oldName := strings.TrimSpace(provider.Name)
		provider.Name = strings.TrimSpace(provider.Name)
		provider.BaseURL = strings.TrimSpace(provider.BaseURL)
		provider.Model = strings.TrimSpace(provider.Model)
		if provider.ProviderKey == "" {
			provider.ProviderKey = generateProviderKey(provider.Name, existingKeys)
		} else {
			provider.ProviderKey = strings.TrimSpace(provider.ProviderKey)
			if !validProviderKey(provider.ProviderKey) {
				provider.ProviderKey = generateProviderKey(provider.Name, existingKeys)
			} else if _, exists := existingKeys[provider.ProviderKey]; exists {
				provider.ProviderKey = generateProviderKey(provider.ProviderKey, existingKeys)
			}
		}
		existingKeys[provider.ProviderKey] = struct{}{}
		if oldName != "" {
			routeNameToKey[oldName] = provider.ProviderKey
		}
		routeNameToKey[provider.ProviderKey] = provider.ProviderKey
		if provider.ProviderType == "" {
			provider.ProviderType = "openai_compatible"
		}
		if provider.RequestTimeoutSeconds <= 0 {
			provider.RequestTimeoutSeconds = cfg.Chat.TimeoutSeconds
		}
		if provider.CostTier == "" {
			provider.CostTier = "medium"
		}
		if provider.MaxRPM <= 0 {
			provider.MaxRPM = 60
		}
		if provider.Priority <= 0 {
			provider.Priority = 10
		}
	}
	for className, route := range cfg.Chat.Routing.TaskClasses {
		for i, name := range route.Providers {
			if providerKey, ok := routeNameToKey[name]; ok {
				route.Providers[i] = providerKey
			}
		}
		cfg.Chat.Routing.TaskClasses[className] = route
	}
	if cfg.Indexing.DefaultScanRoots == nil {
		cfg.Indexing.DefaultScanRoots = defaults.Indexing.DefaultScanRoots
	}
	if cfg.Indexing.ExcludeGlobs == nil {
		cfg.Indexing.ExcludeGlobs = defaults.Indexing.ExcludeGlobs
	}
	if cfg.Indexing.MaxFileSize <= 0 {
		cfg.Indexing.MaxFileSize = defaults.Indexing.MaxFileSize
	}
	if cfg.Memory.MaxContextItems <= 0 {
		cfg.Memory.MaxContextItems = defaults.Memory.MaxContextItems
	}
	if cfg.Memory.MinConfidence <= 0 {
		cfg.Memory.MinConfidence = defaults.Memory.MinConfidence
	}
	if cfg.Memory.RetentionDays == 0 {
		cfg.Memory.RetentionDays = defaults.Memory.RetentionDays
	}
	return cfg
}

var providerKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

func validProviderKey(value string) bool {
	return providerKeyPattern.MatchString(value)
}

func generateProviderKey(name string, existing map[string]struct{}) string {
	base := providerKeyBase(name)
	if base == "" || !validProviderKey(base) {
		base = "provider"
	}
	if _, ok := existing[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		candidateBase := base
		if len(candidateBase)+len(suffix) > 64 {
			candidateBase = strings.TrimRight(candidateBase[:64-len(suffix)], "-")
			if candidateBase == "" {
				candidateBase = "provider"
			}
		}
		candidate := candidateBase + suffix
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
}

func providerKeyBase(name string) string {
	clean := strings.TrimSpace(name)
	switch strings.ToLower(clean) {
	case "deepseek":
		return "deepseek-main"
	case "openai":
		return "openai-main"
	case "硅基流动", "siliconflow":
		return "siliconflow-main"
	case "通义千问兼容接口", "qwen", "dashscope":
		return "qwen-main"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(clean) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
		if b.Len() >= 64 {
			break
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 64 {
		base = strings.TrimRight(base[:64], "-")
	}
	if len(base) < 3 {
		return "provider"
	}
	return base
}

func (s *Server) handleAIRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/ai" {
		writeError(w, errNotFound("not found"))
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.handleAIStatus(w, r)
}

func (s *Server) handleAISubroutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/ai/"))
	if len(parts) == 0 {
		s.handleAIRoot(w, r)
		return
	}
	switch parts[0] {
	case "status":
		s.handleAIStatus(w, r)
	case "settings":
		if len(parts) == 1 {
			s.handleAISettings(w, r)
			return
		}
		switch parts[1] {
		case "default-provider":
			s.handleAIDefaultProviderSettings(w, r)
		case "apply":
			s.handleAISettingsApply(w, r)
		case "enabled":
			s.handleAISettingsEnabled(w, r)
		default:
			writeError(w, errNotFound("not found"))
		}
	case "config":
		s.handleAIConfig(w, r, parts[1:])
	case "providers":
		if len(parts) == 2 && parts[1] == "test" {
			s.handleAIProviderTest(w, r)
			return
		}
		s.handleAIProviders(w, r, parts[1:])
	case "secrets":
		s.handleAISecrets(w, r, parts[1:])
	case "sessions":
		s.handleAISessions(w, r, parts[1:])
	case "runs":
		s.handleAIRuns(w, r, parts[1:])
	default:
		writeError(w, errNotFound("not found"))
	}
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	active, err := ensureActiveAIConfig(r.Context(), s.db)
	if err != nil {
		writeError(w, err)
		return
	}
	active, _ = s.withSecretMetadata(r.Context(), active)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":           active.Config.Enabled,
		"config_version":    active.Version,
		"config_hash":       active.ConfigHash,
		"validation_status": active.ValidationStatus,
		"published_at":      active.PublishedAt,
		"providers":         active.Config.Chat.Providers,
		"routing":           active.Config.Chat.Routing,
		"memory":            active.Config.Memory,
		"encryption_ready":  s.aiEncryptionReady(),
	})
}

func (s *Server) handleAISettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	settings, err := s.buildAISettingsResponse(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleAIDefaultProviderSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req aiDefaultProviderSettingsRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请求 JSON 无效", err.Error())
		return
	}
	resp, fields, err := s.saveAIDefaultProviderSettings(r.Context(), req, s.viewerKey(r))
	if len(fields) > 0 {
		writeFieldErrors(w, fields)
		return
	}
	if err != nil {
		var structured structuredAppError
		if errors.As(err, &structured) {
			writeStructuredError(w, structured.Status, structured.Code, structured.Message, structured.Detail)
			return
		}
		var appErr appError
		if errors.As(err, &appErr) {
			writeStructuredError(w, appErr.Status, aiErrorCode(appErr.Status), appErr.Message, "")
			return
		}
		writeStructuredError(w, http.StatusInternalServerError, "internal_error", "保存 AI 配置失败", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAISettingsEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req aiSettingsEnabledRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请求 JSON 无效", err.Error())
		return
	}
	if req.Enabled {
		writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请使用保存并启用完成连接测试后启用 AI", "")
		return
	}
	if err := publishAIEnabled(r.Context(), s.db, false, s.viewerKey(r)); err != nil {
		writeError(w, err)
		return
	}
	settings, err := s.buildAISettingsResponse(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleAISettingsApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req aiSettingsApplyRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请求 JSON 无效", err.Error())
		return
	}
	resp, providerErrors, err := s.applyAISettings(r.Context(), req, s.viewerKey(r))
	if len(providerErrors) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"code":    "provider_test_failed",
				"message": "供应商连接测试失败",
			},
			"provider_errors": providerErrors,
			"settings":        resp.Settings,
		})
		return
	}
	if err != nil {
		writeAISettingsError(w, err, "应用 AI 配置失败")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAIProviders(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 0 {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req aiProviderMutationRequest
		if err := decodeBody(r.Body, &req); err != nil {
			writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请求 JSON 无效", err.Error())
			return
		}
		resp, fields, err := s.createAIProviderSettings(r.Context(), req, s.viewerKey(r))
		if len(fields) > 0 {
			writeFieldErrors(w, fields)
			return
		}
		if err != nil {
			writeAISettingsError(w, err, "保存供应商失败")
			return
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}
	if len(parts) != 1 {
		writeError(w, errNotFound("not found"))
		return
	}
	providerKey := strings.TrimSpace(parts[0])
	switch r.Method {
	case http.MethodPatch:
		var req aiProviderMutationRequest
		if err := decodeBody(r.Body, &req); err != nil {
			writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请求 JSON 无效", err.Error())
			return
		}
		resp, fields, err := s.updateAIProviderSettings(r.Context(), providerKey, req, s.viewerKey(r))
		if len(fields) > 0 {
			writeFieldErrors(w, fields)
			return
		}
		if err != nil {
			writeAISettingsError(w, err, "保存供应商失败")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodDelete:
		resp, fields, err := s.deleteAIProviderSettings(r.Context(), providerKey, strings.TrimSpace(r.URL.Query().Get("replacement_provider_key")), s.viewerKey(r))
		if len(fields) > 0 {
			writeFieldErrors(w, fields)
			return
		}
		if err != nil {
			writeAISettingsError(w, err, "删除供应商失败")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeAISettingsError(w http.ResponseWriter, err error, fallbackMessage string) {
	var structured structuredAppError
	if errors.As(err, &structured) {
		writeStructuredError(w, structured.Status, structured.Code, structured.Message, structured.Detail)
		return
	}
	var appErr appError
	if errors.As(err, &appErr) {
		writeStructuredError(w, appErr.Status, aiErrorCode(appErr.Status), appErr.Message, "")
		return
	}
	writeStructuredError(w, http.StatusInternalServerError, "internal_error", fallbackMessage, err.Error())
}

func (s *Server) handleAIConfig(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 0 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		active, err := ensureActiveAIConfig(r.Context(), s.db)
		if err != nil {
			writeError(w, err)
			return
		}
		draft, _, _, err := loadEditableAIConfig(r.Context(), s.db)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, err)
			return
		}
		active, _ = s.withSecretMetadata(r.Context(), active)
		if draft != nil {
			*draft, _ = s.withSecretMetadata(r.Context(), *draft)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"active":           active,
			"draft":            draft,
			"encryption_ready": s.aiEncryptionReady(),
		})
		return
	}
	if parts[0] != "drafts" {
		writeError(w, errNotFound("not found"))
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		draft, err := createAIConfigDraft(r.Context(), s.db, s.viewerKey(r))
		if err != nil {
			writeError(w, err)
			return
		}
		draft, _ = s.withSecretMetadata(r.Context(), draft)
		writeJSON(w, http.StatusCreated, draft)
		return
	}
	version, err := parseID(parts[1])
	if err != nil {
		writeError(w, err)
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			cfg, err := getAIConfigVersion(r.Context(), s.db, int(version))
			if err != nil {
				writeError(w, err)
				return
			}
			cfg, _ = s.withSecretMetadata(r.Context(), cfg)
			writeJSON(w, http.StatusOK, cfg)
		case http.MethodPut:
			var data AIConfigData
			if err := decodeBody(r.Body, &data); err != nil {
				writeError(w, err)
				return
			}
			cfg, err := updateAIConfigDraft(r.Context(), s.db, int(version), data)
			if err != nil {
				writeError(w, err)
				return
			}
			cfg, _ = s.withSecretMetadata(r.Context(), cfg)
			writeJSON(w, http.StatusOK, cfg)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 3 {
		switch parts[2] {
		case "validate":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			cfg, err := validateAIConfigDraft(r.Context(), s.db, int(version), false)
			if err != nil {
				writeError(w, err)
				return
			}
			cfg, _ = s.withSecretMetadata(r.Context(), cfg)
			writeJSON(w, http.StatusOK, cfg)
		case "publish":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			cfg, err := publishAIConfigDraft(r.Context(), s.db, int(version), s.viewerKey(r))
			if err != nil {
				writeError(w, err)
				return
			}
			cfg, _ = s.withSecretMetadata(r.Context(), cfg)
			writeJSON(w, http.StatusOK, cfg)
		default:
			writeError(w, errNotFound("not found"))
		}
		return
	}
	writeError(w, errNotFound("not found"))
}

func (s *Server) handleAISecrets(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case len(parts) == 0 && r.Method == http.MethodPost:
		var req aiSecretRequest
		if err := decodeBody(r.Body, &req); err != nil {
			writeError(w, err)
			return
		}
		secret, err := s.createOrUpdateAISecret(r.Context(), 0, req, s.viewerKey(r))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, secret)
	case len(parts) == 1 && r.Method == http.MethodPatch:
		id, err := parseID(parts[0])
		if err != nil {
			writeError(w, err)
			return
		}
		var req aiSecretRequest
		if err := decodeBody(r.Body, &req); err != nil {
			writeError(w, err)
			return
		}
		secret, err := s.createOrUpdateAISecret(r.Context(), id, req, s.viewerKey(r))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, secret)
	default:
		if len(parts) == 0 {
			w.WriteHeader(http.StatusMethodNotAllowed)
		} else {
			writeError(w, errNotFound("not found"))
		}
	}
}

func (s *Server) handleAIProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req aiProviderTestRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "validation_failed", "请求 JSON 无效", err.Error())
		return
	}
	normalizeAIProviderTestRequest(&req)
	fields := validateAIProviderTestRequest(req)
	if len(fields) > 0 {
		writeFieldErrors(w, fields)
		return
	}
	provider := AIProvider{
		ProviderKey:           req.ProviderKey,
		Name:                  req.Name,
		ProviderType:          "openai_compatible",
		BaseURL:               req.BaseURL,
		Model:                 req.Model,
		RequestTimeoutSeconds: req.TimeoutSeconds,
	}
	apiKey, err := s.aiProviderTestAPIKey(r.Context(), req)
	if err != nil {
		var appErr appError
		if errors.As(err, &appErr) {
			writeStructuredError(w, appErr.Status, aiErrorCode(appErr.Status), appErr.Message, "")
			return
		}
		writeStructuredError(w, http.StatusInternalServerError, "internal_error", "读取 API Key 失败", err.Error())
		return
	}
	result, err := s.testOpenAICompatibleProvider(r.Context(), provider, apiKey, req.TimeoutSeconds)
	if err != nil {
		safeErr := sanitizeProviderError(err.Error())
		writeJSON(w, http.StatusOK, aiProviderTestResponse{
			Status:    "fail",
			Message:   providerTestFailureMessage(safeErr),
			Model:     provider.Model,
			LatencyMS: result.LatencyMS,
			SafeError: safeErr,
		})
		return
	}
	writeJSON(w, http.StatusOK, aiProviderTestResponse{
		Status:    "pass",
		Message:   "连接正常",
		Model:     provider.Model,
		LatencyMS: result.LatencyMS,
	})
}

func aiErrorCode(status int) string {
	switch status {
	case http.StatusNotFound:
		return "provider_not_found"
	case http.StatusServiceUnavailable:
		return "encryption_unavailable"
	case http.StatusBadRequest:
		return "validation_failed"
	default:
		return "internal_error"
	}
}

func normalizeAIProviderTestRequest(req *aiProviderTestRequest) {
	req.ProviderKey = strings.TrimSpace(req.ProviderKey)
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "test"
	}
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 20
	}
}

func validateAIProviderTestRequest(req aiProviderTestRequest) map[string]string {
	fields := map[string]string{}
	if req.BaseURL == "" {
		fields["base_url"] = "Base URL 必须是有效的 HTTP 地址"
	} else if _, err := openAICompatibleChatURL(req.BaseURL); err != nil {
		fields["base_url"] = "Base URL 必须是有效的 HTTP 地址"
	}
	if req.Model == "" || len([]rune(req.Model)) > 120 {
		fields["model"] = "模型不能为空，且不能超过 120 个字符"
	}
	if req.ProviderKey != "" && !validProviderKey(req.ProviderKey) {
		fields["provider_key"] = "Provider Key 格式不正确或已存在"
	}
	if req.TimeoutSeconds < 5 || req.TimeoutSeconds > 60 {
		fields["timeout_seconds"] = "超时时间必须在 5 到 60 秒之间"
	}
	return fields
}

func normalizeAIDefaultProviderSettingsRequest(req *aiDefaultProviderSettingsRequest) {
	req.ProviderKey = strings.TrimSpace(req.ProviderKey)
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.CostTier = strings.TrimSpace(req.CostTier)
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 60
	}
	if req.MaxRPM <= 0 {
		req.MaxRPM = 60
	}
	if req.Priority <= 0 {
		req.Priority = 10
	}
	if req.CostTier == "" {
		req.CostTier = "medium"
	}
}

func validateAIDefaultProviderSettingsRequest(req aiDefaultProviderSettingsRequest) map[string]string {
	fields := map[string]string{}
	if req.Name == "" || len([]rune(req.Name)) > 80 {
		fields["name"] = "供应商名称不能为空，且不能超过 80 个字符"
	}
	if req.ProviderKey != "" && !validProviderKey(req.ProviderKey) {
		fields["provider_key"] = "Provider Key 格式不正确或已存在"
	}
	if req.BaseURL == "" {
		fields["base_url"] = "Base URL 必须是有效的 HTTP 地址"
	} else if _, err := openAICompatibleChatURL(req.BaseURL); err != nil {
		fields["base_url"] = "Base URL 必须是有效的 HTTP 地址"
	}
	if req.Model == "" || len([]rune(req.Model)) > 120 {
		fields["model"] = "模型不能为空，且不能超过 120 个字符"
	}
	if req.TimeoutSeconds < 5 || req.TimeoutSeconds > 300 {
		fields["timeout_seconds"] = "超时时间必须在 5 到 300 秒之间"
	}
	if req.MaxRPM < 1 || req.MaxRPM > 10000 {
		fields["max_rpm"] = "RPM 必须在 1 到 10000 之间"
	}
	if req.Priority < 1 || req.Priority > 10000 {
		fields["priority"] = "优先级必须在 1 到 10000 之间"
	}
	switch req.CostTier {
	case "low", "medium", "high":
	default:
		fields["cost_tier"] = "成本等级必须是 low、medium 或 high"
	}
	return fields
}

func (s *Server) buildAISettingsResponse(ctx context.Context) (aiSettingsResponse, error) {
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return aiSettingsResponse{}, err
	}
	active, _ = s.withSecretMetadata(ctx, active)
	editable, editableStatus, hasEditable, err := loadEditableAIConfig(ctx, s.db)
	if err != nil {
		return aiSettingsResponse{}, err
	}
	display := active
	if hasEditable && editable != nil {
		display = *editable
		display, _ = s.withSecretMetadata(ctx, display)
	}
	routeProviderKeys := aiSettingsRouteProviderKeys(display.Config)
	activeRouteProviderKeys := aiSettingsRouteProviderKeys(active.Config)
	defaultProvider, hasDefault := aiSettingsDefaultProvider(display.Config, routeProviderKeys)
	tests := aiProviderTestsFromReport(display.ValidationReportJSON)
	if len(tests) == 0 && display.Version != active.Version {
		tests = aiProviderTestsFromReport(active.ValidationReportJSON)
	}
	defaultKey := ""
	if hasDefault {
		defaultKey = defaultProvider.ProviderKey
	}
	providers := buildAISettingsProviderSummaries(display.Config, defaultKey, routeProviderKeys, tests)
	var lastTest *aiProviderTestSummary
	if test, ok := tests[defaultKey]; ok {
		copy := test
		lastTest = &copy
	}
	encryptionReady := s.aiEncryptionReady()
	status := aiSettingsStatus(active, len(activeRouteProviderKeys) > 0, len(routeProviderKeys) > 0, editableStatus, lastTest, encryptionReady)
	return aiSettingsResponse{
		Enabled:                 active.Config.Enabled,
		Status:                  status,
		DefaultProviderKey:      defaultKey,
		DefaultProviderName:     aiProviderDisplayName(defaultProvider),
		DefaultModel:            defaultProvider.Model,
		RouteProviderKeys:       routeProviderKeys,
		RouteProviders:          aiSettingsRouteProviders(display.Config, routeProviderKeys),
		ActiveRouteProviderKeys: activeRouteProviderKeys,
		HasUnappliedChanges:     hasEditable,
		EncryptionReady:         encryptionReady,
		EditableStatus:          editableStatus,
		LastTest:                lastTest,
		Providers:               providers,
	}, nil
}

func aiSettingsStatus(active AIConfigVersion, hasActiveRoute bool, hasDisplayRoute bool, editableStatus string, lastTest *aiProviderTestSummary, encryptionReady bool) string {
	if editableStatus == "failed" {
		return "error"
	}
	if lastTest != nil && lastTest.Status == "fail" {
		return "error"
	}
	if !encryptionReady {
		return "error"
	}
	if active.Config.Enabled {
		if hasActiveRoute {
			return "enabled"
		}
		return "error"
	}
	if !hasDisplayRoute {
		return "not_configured"
	}
	if lastTest != nil && lastTest.Status == "pass" {
		return "ready_disabled"
	}
	return "ready_to_test"
}

func buildAISettingsProviderSummaries(cfg AIConfigData, defaultKey string, routeKeys []string, tests map[string]aiProviderTestSummary) []aiSettingsProviderSummary {
	providers := append([]AIProvider(nil), cfg.Chat.Providers...)
	routeOrder := map[string]int{}
	for i, key := range routeKeys {
		routeOrder[key] = i + 1
	}
	sort.SliceStable(providers, func(i, j int) bool {
		leftOrder := routeOrder[providers[i].ProviderKey]
		rightOrder := routeOrder[providers[j].ProviderKey]
		if leftOrder > 0 || rightOrder > 0 {
			if leftOrder == 0 {
				return false
			}
			if rightOrder == 0 {
				return true
			}
			return leftOrder < rightOrder
		}
		if providers[i].Priority == providers[j].Priority {
			return providers[i].ProviderKey < providers[j].ProviderKey
		}
		return providers[i].Priority < providers[j].Priority
	})
	summaries := make([]aiSettingsProviderSummary, 0, len(providers))
	for _, provider := range providers {
		test := tests[provider.ProviderKey]
		summaries = append(summaries, aiSettingsProviderSummary{
			ProviderKey:       provider.ProviderKey,
			Name:              aiProviderDisplayName(provider),
			ProviderType:      provider.ProviderType,
			BaseURL:           provider.BaseURL,
			Model:             provider.Model,
			APIKeyConfigured:  provider.APIKeySecretID > 0 && provider.SecretConfigured,
			APIKeyLast4:       provider.SecretLast4,
			IsDefault:         provider.ProviderKey == defaultKey,
			RouteOrder:        routeOrder[provider.ProviderKey],
			Usable:            aiProviderUsable(provider),
			LastTestStatus:    emptyDefault(test.Status, "not_run"),
			LastTestMessage:   test.Message,
			TimeoutSeconds:    provider.RequestTimeoutSeconds,
			MaxRPM:            provider.MaxRPM,
			Priority:          provider.Priority,
			CostTier:          provider.CostTier,
			RequestTimeoutSec: provider.RequestTimeoutSeconds,
		})
	}
	return summaries
}

func aiSettingsRouteProviderKeys(cfg AIConfigData) []string {
	cfg = normalizeAIConfig(cfg)
	providersByKey := map[string]AIProvider{}
	for _, provider := range cfg.Chat.Providers {
		providersByKey[provider.ProviderKey] = provider
	}
	taskClass := cfg.Chat.Routing.DefaultTaskClass
	if taskClass == "" {
		taskClass = "standard"
	}
	if route, ok := cfg.Chat.Routing.TaskClasses[taskClass]; ok {
		keys := make([]string, 0, len(route.Providers))
		seen := map[string]struct{}{}
		for _, key := range route.Providers {
			provider, ok := providersByKey[key]
			if !ok || !aiProviderUsable(provider) {
				continue
			}
			if _, exists := seen[provider.ProviderKey]; exists {
				continue
			}
			keys = append(keys, provider.ProviderKey)
			seen[provider.ProviderKey] = struct{}{}
		}
		if len(keys) > 0 {
			return keys
		}
	}
	providers := append([]AIProvider(nil), cfg.Chat.Providers...)
	sort.SliceStable(providers, func(i, j int) bool {
		if providers[i].Priority == providers[j].Priority {
			return providers[i].ProviderKey < providers[j].ProviderKey
		}
		return providers[i].Priority < providers[j].Priority
	})
	keys := []string{}
	for _, provider := range providers {
		if aiProviderUsable(provider) {
			keys = append(keys, provider.ProviderKey)
		}
	}
	return keys
}

func aiSettingsRouteProviders(cfg AIConfigData, routeKeys []string) []aiSettingsRouteProvider {
	providersByKey := map[string]AIProvider{}
	for _, provider := range cfg.Chat.Providers {
		providersByKey[provider.ProviderKey] = provider
	}
	out := make([]aiSettingsRouteProvider, 0, len(routeKeys))
	for _, key := range routeKeys {
		provider, ok := providersByKey[key]
		if !ok {
			continue
		}
		out = append(out, aiSettingsRouteProvider{
			ProviderKey: provider.ProviderKey,
			Name:        aiProviderDisplayName(provider),
			Model:       provider.Model,
			Priority:    provider.Priority,
		})
	}
	return out
}

func aiSettingsDefaultProvider(cfg AIConfigData, routeKeys []string) (AIProvider, bool) {
	providersByKey := map[string]AIProvider{}
	for _, provider := range cfg.Chat.Providers {
		providersByKey[provider.ProviderKey] = provider
	}
	if len(routeKeys) > 0 {
		if provider, ok := providersByKey[routeKeys[0]]; ok {
			return provider, true
		}
	}
	return pickDefaultAIProvider(cfg)
}

func aiProviderDisplayName(provider AIProvider) string {
	name := strings.TrimSpace(provider.Name)
	key := strings.ToLower(strings.TrimSpace(provider.ProviderKey))
	lowerName := strings.ToLower(name)
	if key == "deepseek-fast" || key == "deepseek-quality" || lowerName == "deepseek-fast" || lowerName == "deepseek-quality" {
		return "DeepSeek"
	}
	if name == "" && strings.Contains(strings.ToLower(provider.BaseURL), "deepseek") {
		return "DeepSeek"
	}
	if name == "" {
		return provider.ProviderKey
	}
	return name
}

func aiProviderUsable(provider AIProvider) bool {
	return strings.TrimSpace(provider.BaseURL) != "" && strings.TrimSpace(provider.Model) != "" &&
		provider.APIKeySecretID > 0 && provider.SecretConfigured
}

func aiProviderRoutable(provider AIProvider) bool {
	return strings.TrimSpace(provider.BaseURL) != "" && strings.TrimSpace(provider.Model) != "" && provider.APIKeySecretID > 0
}

func pickDefaultAIProvider(cfg AIConfigData) (AIProvider, bool) {
	providersByKey := map[string]AIProvider{}
	for _, provider := range cfg.Chat.Providers {
		providersByKey[provider.ProviderKey] = provider
		if provider.Name != "" {
			providersByKey[provider.Name] = provider
		}
	}
	taskClass := cfg.Chat.Routing.DefaultTaskClass
	if taskClass == "" {
		taskClass = "standard"
	}
	if route, ok := cfg.Chat.Routing.TaskClasses[taskClass]; ok {
		for _, key := range route.Providers {
			if provider, ok := providersByKey[key]; ok && aiProviderUsable(provider) {
				return provider, true
			}
		}
		for _, key := range route.Providers {
			if provider, ok := providersByKey[key]; ok && provider.Model == "deepseek-v4-flash" {
				return provider, true
			}
		}
		for _, key := range route.Providers {
			if provider, ok := providersByKey[key]; ok {
				return provider, true
			}
		}
	}
	providers := append([]AIProvider(nil), cfg.Chat.Providers...)
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].Priority < providers[j].Priority })
	for _, provider := range providers {
		if aiProviderUsable(provider) {
			return provider, true
		}
	}
	for _, provider := range providers {
		if provider.Model == "deepseek-v4-flash" {
			return provider, true
		}
	}
	if len(providers) > 0 {
		return providers[0], true
	}
	return AIProvider{}, false
}

func aiProviderTestsFromReport(raw string) map[string]aiProviderTestSummary {
	type report struct {
		ProviderTests map[string]aiProviderTestSummary `json:"provider_tests"`
	}
	var parsed report
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil || parsed.ProviderTests == nil {
		return map[string]aiProviderTestSummary{}
	}
	return parsed.ProviderTests
}

func loadEditableAIConfig(ctx context.Context, db *sql.DB) (*AIConfigVersion, string, bool, error) {
	var activeVersion int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM ai_config_versions WHERE status = 'active'`).Scan(&activeVersion)
	if err != nil {
		return nil, "", false, err
	}
	row := db.QueryRowContext(ctx, `SELECT id, version, status, config_hash, config_json, secret_refs_json,
		validation_status, validation_report_json, created_by_viewer, published_by_viewer, created_at, updated_at,
		published_at, superseded_at, error_message
		FROM ai_config_versions WHERE status IN ('draft', 'failed') AND version > ? ORDER BY version DESC LIMIT 1`, activeVersion)
	cfg, err := scanAIConfigVersion(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	return &cfg, cfg.Status, true, nil
}

func loadAIConfigEditBase(ctx context.Context, db *sql.DB) (AIConfigVersion, AIConfigVersion, bool, error) {
	active, err := ensureActiveAIConfig(ctx, db)
	if err != nil {
		return AIConfigVersion{}, AIConfigVersion{}, false, err
	}
	editable, _, hasEditable, err := loadEditableAIConfig(ctx, db)
	if err != nil {
		return AIConfigVersion{}, AIConfigVersion{}, false, err
	}
	if hasEditable && editable != nil {
		return active, *editable, true, nil
	}
	return active, active, false, nil
}

func (s *Server) createAIProviderSettings(ctx context.Context, req aiProviderMutationRequest, viewer string) (aiProviderMutationResponse, map[string]string, error) {
	normalizeAIProviderMutationRequest(&req)
	_, base, hasEditable, err := loadAIConfigEditBase(ctx, s.db)
	if err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	cfg := normalizeAIConfig(base.Config)
	apiKey := stringPtrValue(req.APIKey)
	provider := AIProvider{
		Name:                  stringPtrValue(req.Name),
		ProviderType:          "openai_compatible",
		BaseURL:               stringPtrValue(req.BaseURL),
		Model:                 stringPtrValue(req.Model),
		RequestTimeoutSeconds: intPtrValue(req.TimeoutSeconds, 60),
		MaxRPM:                intPtrValue(req.MaxRPM, 60),
		Priority:              intPtrValue(req.Priority, nextAIProviderPriority(cfg.Chat.Providers)),
		CostTier:              stringPtrDefault(req.CostTier, "medium"),
	}
	provider.ProviderKey = generateProviderKey(provider.Name, existingAIProviderKeys(cfg.Chat.Providers))
	if fields := validateAIProviderMutation(provider, true, apiKey); len(fields) > 0 {
		return aiProviderMutationResponse{}, fields, nil
	}
	providerTests := map[string]aiProviderTestSummary{}
	if boolPtrValue(req.TestBeforeSave) {
		test, err := s.testOpenAICompatibleProvider(ctx, provider, apiKey, provider.RequestTimeoutSeconds)
		if err != nil {
			return aiProviderMutationResponse{}, nil, structuredAppError{
				Status:  http.StatusBadRequest,
				Code:    "provider_test_failed",
				Message: "供应商连接测试失败",
				Detail:  sanitizeProviderError(err.Error()),
			}
		}
		providerTests[provider.ProviderKey] = test
	}
	secret, err := s.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: provider.ProviderKey + "-api-key", SecretType: "api_key", Value: apiKey}, viewer)
	if err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	provider.APIKeySecretID = secret.ID
	provider.SecretConfigured = true
	provider.SecretLast4 = secret.Last4
	cfg.Chat.Providers = append(cfg.Chat.Providers, provider)
	cfg.Chat.Routing = buildDefaultRouting(ctx, s.db, cfg.Chat.Providers)
	report := aiConfigEditReport(ctx, s.db, cfg, false, providerTests)
	if _, err := saveEditableAIConfig(ctx, s.db, base, hasEditable, cfg, viewer, "not_run", encodeJSON(report), ""); err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	settings, err := s.buildAISettingsResponse(ctx)
	if err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	return aiProviderMutationResponse{
		Provider: pickSettingsProvider(settings.Providers, provider.ProviderKey),
		Settings: settings,
		Message:  "供应商已保存，尚未应用到 AI 问答",
	}, nil, nil
}

func (s *Server) updateAIProviderSettings(ctx context.Context, providerKey string, req aiProviderMutationRequest, viewer string) (aiProviderMutationResponse, map[string]string, error) {
	providerKey = strings.TrimSpace(providerKey)
	if !validProviderKey(providerKey) {
		return aiProviderMutationResponse{}, nil, errNotFound("provider not found")
	}
	normalizeAIProviderMutationRequest(&req)
	if !aiProviderMutationHasChange(req) {
		return aiProviderMutationResponse{}, map[string]string{"provider_key": "至少提交一个供应商字段"}, nil
	}
	_, base, hasEditable, err := loadAIConfigEditBase(ctx, s.db)
	if err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	cfg := normalizeAIConfig(base.Config)
	index := findAIProviderIndex(cfg.Chat.Providers, providerKey)
	if index < 0 {
		return aiProviderMutationResponse{}, nil, errNotFound("provider not found")
	}
	provider := cfg.Chat.Providers[index]
	if req.Name != nil {
		provider.Name = stringPtrValue(req.Name)
	}
	if req.BaseURL != nil {
		provider.BaseURL = stringPtrValue(req.BaseURL)
	}
	if req.Model != nil {
		provider.Model = stringPtrValue(req.Model)
	}
	if req.TimeoutSeconds != nil {
		provider.RequestTimeoutSeconds = intPtrValue(req.TimeoutSeconds, 60)
	}
	if req.MaxRPM != nil {
		provider.MaxRPM = intPtrValue(req.MaxRPM, 60)
	}
	if req.Priority != nil {
		provider.Priority = intPtrValue(req.Priority, 10)
	}
	if req.CostTier != nil {
		provider.CostTier = stringPtrDefault(req.CostTier, "medium")
	}
	provider.ProviderType = "openai_compatible"
	newAPIKey := ""
	if req.APIKey != nil {
		newAPIKey = stringPtrValue(req.APIKey)
	}
	requireKey := boolPtrValue(req.MakeDefault) || boolPtrValue(req.TestBeforeSave)
	if fields := validateAIProviderMutation(provider, requireKey, newAPIKey); len(fields) > 0 {
		return aiProviderMutationResponse{}, fields, nil
	}
	testAPIKey := newAPIKey
	var providerTests map[string]aiProviderTestSummary
	if boolPtrValue(req.TestBeforeSave) {
		if testAPIKey == "" {
			testAPIKey, err = s.decryptAISecret(ctx, provider.APIKeySecretID)
			if err != nil {
				return aiProviderMutationResponse{}, nil, err
			}
		}
		test, err := s.testOpenAICompatibleProvider(ctx, provider, testAPIKey, provider.RequestTimeoutSeconds)
		if err != nil {
			return aiProviderMutationResponse{}, nil, structuredAppError{
				Status:  http.StatusBadRequest,
				Code:    "provider_test_failed",
				Message: "供应商连接测试失败",
				Detail:  sanitizeProviderError(err.Error()),
			}
		}
		providerTests = map[string]aiProviderTestSummary{provider.ProviderKey: test}
	}
	if newAPIKey != "" {
		secret, err := s.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: provider.ProviderKey + "-api-key", SecretType: "api_key", Value: newAPIKey}, viewer)
		if err != nil {
			return aiProviderMutationResponse{}, nil, err
		}
		provider.APIKeySecretID = secret.ID
		provider.SecretConfigured = true
		provider.SecretLast4 = secret.Last4
	}
	cfg.Chat.Providers[index] = provider
	if boolPtrValue(req.MakeDefault) {
		cfg.Chat.Providers = makeAIProviderDefault(cfg.Chat.Providers, provider.ProviderKey)
	}
	cfg.Chat.Routing = buildDefaultRouting(ctx, s.db, cfg.Chat.Providers)
	report := aiConfigEditReport(ctx, s.db, cfg, false, providerTests)
	if _, err := saveEditableAIConfig(ctx, s.db, base, hasEditable, cfg, viewer, "not_run", encodeJSON(report), ""); err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	settings, err := s.buildAISettingsResponse(ctx)
	if err != nil {
		return aiProviderMutationResponse{}, nil, err
	}
	return aiProviderMutationResponse{
		Provider: pickSettingsProvider(settings.Providers, provider.ProviderKey),
		Settings: settings,
		Message:  "供应商已保存，尚未应用到 AI 问答",
	}, nil, nil
}

func (s *Server) deleteAIProviderSettings(ctx context.Context, providerKey, replacementProviderKey, viewer string) (aiProviderDeleteResponse, map[string]string, error) {
	providerKey = strings.TrimSpace(providerKey)
	replacementProviderKey = strings.TrimSpace(replacementProviderKey)
	if !validProviderKey(providerKey) {
		return aiProviderDeleteResponse{}, nil, errNotFound("provider not found")
	}
	active, base, hasEditable, err := loadAIConfigEditBase(ctx, s.db)
	if err != nil {
		return aiProviderDeleteResponse{}, nil, err
	}
	cfg := normalizeAIConfig(base.Config)
	index := findAIProviderIndex(cfg.Chat.Providers, providerKey)
	if index < 0 {
		return aiProviderDeleteResponse{}, nil, errNotFound("provider not found")
	}
	currentRoute := buildDefaultRouting(ctx, s.db, cfg.Chat.Providers).TaskClasses["standard"].Providers
	if active.Config.Enabled && len(currentRoute) > 0 && currentRoute[0] == providerKey {
		if replacementProviderKey == "" || replacementProviderKey == providerKey {
			return aiProviderDeleteResponse{}, map[string]string{"replacement_provider_key": "请先指定新的默认供应商，或停用 AI 问答"}, nil
		}
		replacementIndex := findAIProviderIndex(cfg.Chat.Providers, replacementProviderKey)
		if replacementIndex < 0 || !aiProviderRoutableWithSecret(ctx, s.db, cfg.Chat.Providers[replacementIndex]) {
			return aiProviderDeleteResponse{}, map[string]string{"replacement_provider_key": "替代供应商必须可路由"}, nil
		}
		cfg.Chat.Providers = makeAIProviderDefault(cfg.Chat.Providers, replacementProviderKey)
	}
	cfg.Chat.Providers = append(cfg.Chat.Providers[:index], cfg.Chat.Providers[index+1:]...)
	cfg.Chat.Routing = buildDefaultRouting(ctx, s.db, cfg.Chat.Providers)
	report := aiConfigEditReport(ctx, s.db, cfg, false, nil)
	if _, err := saveEditableAIConfig(ctx, s.db, base, hasEditable, cfg, viewer, "not_run", encodeJSON(report), ""); err != nil {
		return aiProviderDeleteResponse{}, nil, err
	}
	settings, err := s.buildAISettingsResponse(ctx)
	if err != nil {
		return aiProviderDeleteResponse{}, nil, err
	}
	return aiProviderDeleteResponse{
		DeletedProviderKey: providerKey,
		Settings:           settings,
		Message:            "供应商已删除，尚未应用到 AI 问答",
	}, nil, nil
}

func (s *Server) applyAISettings(ctx context.Context, req aiSettingsApplyRequest, viewer string) (aiSettingsApplyResponse, map[string]string, error) {
	testPolicy := strings.TrimSpace(req.TestPolicy)
	if testPolicy == "" {
		testPolicy = "default_only"
	}
	switch testPolicy {
	case "default_only", "changed_routable", "all_routable":
	default:
		return aiSettingsApplyResponse{}, nil, structuredAppError{Status: http.StatusBadRequest, Code: "validation_failed", Message: "测试策略无效"}
	}
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return aiSettingsApplyResponse{}, nil, err
	}
	editable, _, hasEditable, err := loadEditableAIConfig(ctx, s.db)
	if err != nil {
		return aiSettingsApplyResponse{}, nil, err
	}
	if !hasEditable || editable == nil {
		settings, err := s.buildAISettingsResponse(ctx)
		if err != nil {
			return aiSettingsApplyResponse{}, nil, err
		}
		return aiSettingsApplyResponse{Enabled: active.Config.Enabled, Settings: settings, Message: "当前 AI 配置已是最新"}, nil, nil
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	cfg := normalizeAIConfig(editable.Config)
	cfg.Enabled = enabled
	cfg.Chat.Routing = buildDefaultRouting(ctx, s.db, cfg.Chat.Providers)
	routeProviders := cfg.Chat.Routing.TaskClasses["standard"].Providers
	if enabled && len(routeProviders) == 0 {
		settings, _ := s.buildAISettingsResponse(ctx)
		return aiSettingsApplyResponse{Enabled: false, Settings: settings}, map[string]string{"routing": "默认路由必须按优先级包含至少一个可用供应商"}, nil
	}
	providerTests := map[string]aiProviderTestSummary{}
	if enabled {
		tests, providerErrors := s.testAIProvidersForApply(ctx, cfg, testPolicy)
		if len(providerErrors) > 0 {
			settings, _ := s.buildAISettingsResponse(ctx)
			return aiSettingsApplyResponse{Enabled: false, Settings: settings}, providerErrors, nil
		}
		providerTests = tests
	}
	report := aiConfigEditReport(ctx, s.db, cfg, true, providerTests)
	if !report["ok"].(bool) {
		_, _ = saveEditableAIConfig(ctx, s.db, *editable, true, cfg, viewer, "fail", encodeJSON(report), firstAIValidationError(report))
		return aiSettingsApplyResponse{}, nil, structuredAppError{
			Status:  http.StatusBadRequest,
			Code:    "validation_failed",
			Message: "AI 配置校验失败",
			Detail:  firstAIValidationError(report),
		}
	}
	if _, err := publishAIConfig(ctx, s.db, *editable, true, cfg, viewer, encodeJSON(report)); err != nil {
		return aiSettingsApplyResponse{}, nil, err
	}
	settings, err := s.buildAISettingsResponse(ctx)
	if err != nil {
		return aiSettingsApplyResponse{}, nil, err
	}
	return aiSettingsApplyResponse{Enabled: enabled, Settings: settings, Message: "当前 AI 配置已应用"}, nil, nil
}

func (s *Server) saveAIDefaultProviderSettings(ctx context.Context, req aiDefaultProviderSettingsRequest, viewer string) (aiSettingsSaveResponse, map[string]string, error) {
	normalizeAIDefaultProviderSettingsRequest(&req)
	if fields := validateAIDefaultProviderSettingsRequest(req); len(fields) > 0 {
		return aiSettingsSaveResponse{}, fields, nil
	}
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return aiSettingsSaveResponse{}, nil, err
	}
	editable, _, hasEditable, err := loadEditableAIConfig(ctx, s.db)
	if err != nil {
		return aiSettingsSaveResponse{}, nil, err
	}
	base := active
	if hasEditable && editable != nil {
		base = *editable
	}
	cfg, provider, fields := mergeDefaultProviderSettings(base.Config, req)
	if len(fields) > 0 {
		return aiSettingsSaveResponse{}, fields, nil
	}
	if req.APIKey == "" && provider.APIKeySecretID <= 0 {
		return aiSettingsSaveResponse{}, map[string]string{"api_key": "首次启用必须填写 API Key"}, nil
	}
	if req.Enable {
		apiKey := req.APIKey
		if apiKey == "" {
			apiKey, err = s.decryptAISecret(ctx, provider.APIKeySecretID)
			if err != nil {
				return aiSettingsSaveResponse{}, nil, err
			}
		}
		test, err := s.testOpenAICompatibleProvider(ctx, provider, apiKey, req.TimeoutSeconds)
		if err != nil {
			return aiSettingsSaveResponse{}, nil, structuredAppError{
				Status:  http.StatusBadRequest,
				Code:    "provider_test_failed",
				Message: "供应商连接测试失败",
				Detail:  sanitizeProviderError(err.Error()),
			}
		}
		if req.APIKey != "" {
			secret, err := s.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: provider.ProviderKey + "-api-key", SecretType: "api_key", Value: req.APIKey}, viewer)
			if err != nil {
				return aiSettingsSaveResponse{}, nil, err
			}
			provider.APIKeySecretID = secret.ID
			provider.SecretConfigured = true
			provider.SecretLast4 = secret.Last4
			cfg = replaceAIProvider(cfg, provider)
		}
		cfg.Enabled = true
		cfg.Chat.Routing = buildDefaultRouting(ctx, s.db, cfg.Chat.Providers)
		report := validateAIConfigData(ctx, s.db, cfg, true)
		report["provider_tests"] = map[string]aiProviderTestSummary{provider.ProviderKey: test}
		report["route_provider_keys"] = cfg.Chat.Routing.TaskClasses["standard"].Providers
		if !report["ok"].(bool) {
			_, _ = saveEditableAIConfig(ctx, s.db, base, hasEditable, cfg, viewer, "fail", encodeJSON(report), firstAIValidationError(report))
			return aiSettingsSaveResponse{}, map[string]string{"routing": firstAIValidationError(report)}, nil
		}
		published, err := publishAIConfig(ctx, s.db, base, hasEditable, cfg, viewer, encodeJSON(report))
		if err != nil {
			return aiSettingsSaveResponse{}, nil, err
		}
		_ = published
		settings, err := s.buildAISettingsResponse(ctx)
		if err != nil {
			return aiSettingsSaveResponse{}, nil, err
		}
		return aiSettingsSaveResponse{
			Enabled:  true,
			Provider: pickSettingsProvider(settings.Providers, provider.ProviderKey),
			Settings: settings,
			Message:  "AI 问答已启用",
		}, nil, nil
	}
	if req.APIKey != "" {
		secret, err := s.createOrUpdateAISecret(ctx, 0, aiSecretRequest{Name: provider.ProviderKey + "-api-key", SecretType: "api_key", Value: req.APIKey}, viewer)
		if err != nil {
			return aiSettingsSaveResponse{}, nil, err
		}
		provider.APIKeySecretID = secret.ID
		provider.SecretConfigured = true
		provider.SecretLast4 = secret.Last4
		cfg = replaceAIProvider(cfg, provider)
	}
	cfg.Enabled = active.Config.Enabled
	cfg.Chat.Routing = buildDefaultRouting(ctx, s.db, cfg.Chat.Providers)
	if _, err := saveEditableAIConfig(ctx, s.db, base, hasEditable, cfg, viewer, "not_run", "", ""); err != nil {
		return aiSettingsSaveResponse{}, nil, err
	}
	settings, err := s.buildAISettingsResponse(ctx)
	if err != nil {
		return aiSettingsSaveResponse{}, nil, err
	}
	return aiSettingsSaveResponse{
		Enabled:  active.Config.Enabled,
		Provider: pickSettingsProvider(settings.Providers, provider.ProviderKey),
		Settings: settings,
		Message:  "AI 配置已保存",
	}, nil, nil
}

func mergeDefaultProviderSettings(cfg AIConfigData, req aiDefaultProviderSettingsRequest) (AIConfigData, AIProvider, map[string]string) {
	cfg = normalizeAIConfig(cfg)
	fields := map[string]string{}
	providerIndex := -1
	if req.ProviderKey != "" {
		for i, provider := range cfg.Chat.Providers {
			if provider.ProviderKey == req.ProviderKey {
				providerIndex = i
				break
			}
		}
		if providerIndex < 0 {
			fields["provider_key"] = "Provider Key 格式不正确或已存在"
			return cfg, AIProvider{}, fields
		}
	} else if defaultProvider, ok := pickDefaultAIProvider(cfg); ok {
		for i, provider := range cfg.Chat.Providers {
			if provider.ProviderKey == defaultProvider.ProviderKey {
				providerIndex = i
				break
			}
		}
	}
	provider := AIProvider{}
	if providerIndex >= 0 {
		provider = cfg.Chat.Providers[providerIndex]
	} else {
		existing := map[string]struct{}{}
		for _, item := range cfg.Chat.Providers {
			existing[item.ProviderKey] = struct{}{}
		}
		provider.ProviderKey = generateProviderKey(req.Name, existing)
		providerIndex = len(cfg.Chat.Providers)
		cfg.Chat.Providers = append(cfg.Chat.Providers, provider)
	}
	provider.Name = req.Name
	provider.ProviderType = "openai_compatible"
	provider.BaseURL = req.BaseURL
	provider.Model = req.Model
	provider.RequestTimeoutSeconds = req.TimeoutSeconds
	provider.MaxRPM = req.MaxRPM
	provider.Priority = req.Priority
	provider.CostTier = req.CostTier
	cfg.Chat.Providers[providerIndex] = provider
	return normalizeAIConfig(cfg), provider, fields
}

func replaceAIProvider(cfg AIConfigData, provider AIProvider) AIConfigData {
	for i := range cfg.Chat.Providers {
		if cfg.Chat.Providers[i].ProviderKey == provider.ProviderKey {
			cfg.Chat.Providers[i] = provider
			return normalizeAIConfig(cfg)
		}
	}
	cfg.Chat.Providers = append(cfg.Chat.Providers, provider)
	return normalizeAIConfig(cfg)
}

func buildDefaultRouting(ctx context.Context, db *sql.DB, providers []AIProvider) AIRouting {
	sorted := append([]AIProvider(nil), providers...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority == sorted[j].Priority {
			return sorted[i].ProviderKey < sorted[j].ProviderKey
		}
		return sorted[i].Priority < sorted[j].Priority
	})
	routeProviders := []string{}
	seen := map[string]struct{}{}
	for _, provider := range sorted {
		if _, ok := seen[provider.ProviderKey]; ok {
			continue
		}
		if aiProviderRoutableWithSecret(ctx, db, provider) {
			routeProviders = append(routeProviders, provider.ProviderKey)
			seen[provider.ProviderKey] = struct{}{}
		}
	}
	return AIRouting{
		DefaultTaskClass: "standard",
		TaskClasses: map[string]AITaskRoute{
			"standard": {Providers: routeProviders, FallbackTaskClass: ""},
		},
		Escalation: map[string]string{},
	}
}

func aiProviderRoutableWithSecret(ctx context.Context, db *sql.DB, provider AIProvider) bool {
	if provider.ProviderType != "" && provider.ProviderType != "openai_compatible" {
		return false
	}
	if !aiProviderRoutable(provider) {
		return false
	}
	return aiSecretExists(ctx, db, provider.APIKeySecretID)
}

func normalizeAIProviderMutationRequest(req *aiProviderMutationRequest) {
	trimStringPointer(req.Name)
	trimStringPointer(req.BaseURL)
	trimStringPointer(req.Model)
	trimStringPointer(req.APIKey)
	trimStringPointer(req.CostTier)
}

func trimStringPointer(value *string) {
	if value == nil {
		return
	}
	*value = strings.TrimSpace(*value)
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPtrDefault(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func intPtrValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func boolPtrValue(value *bool) bool {
	return value != nil && *value
}

func aiProviderMutationHasChange(req aiProviderMutationRequest) bool {
	return req.Name != nil || req.BaseURL != nil || req.Model != nil || req.APIKey != nil ||
		req.TimeoutSeconds != nil || req.MaxRPM != nil || req.Priority != nil || req.CostTier != nil ||
		req.MakeDefault != nil || req.TestBeforeSave != nil
}

func existingAIProviderKeys(providers []AIProvider) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, provider := range providers {
		if provider.ProviderKey != "" {
			keys[provider.ProviderKey] = struct{}{}
		}
	}
	return keys
}

func nextAIProviderPriority(providers []AIProvider) int {
	maxPriority := 0
	for _, provider := range providers {
		if provider.Priority > maxPriority {
			maxPriority = provider.Priority
		}
	}
	if maxPriority <= 0 {
		return 10
	}
	next := maxPriority + 10
	if next > 10000 {
		return 10000
	}
	return next
}

func findAIProviderIndex(providers []AIProvider, providerKey string) int {
	for i, provider := range providers {
		if provider.ProviderKey == providerKey {
			return i
		}
	}
	return -1
}

func validateAIProviderMutation(provider AIProvider, requireAPIKey bool, apiKey string) map[string]string {
	fields := map[string]string{}
	if provider.Name == "" || len([]rune(provider.Name)) > 80 {
		fields["name"] = "供应商名称不能为空，且不能超过 80 个字符"
	}
	if provider.BaseURL == "" {
		fields["base_url"] = "Base URL 必须是有效的 HTTP 地址"
	} else if _, err := openAICompatibleChatURL(provider.BaseURL); err != nil {
		fields["base_url"] = "Base URL 必须是有效的 HTTP 地址"
	}
	if provider.Model == "" || len([]rune(provider.Model)) > 120 {
		fields["model"] = "模型不能为空，且不能超过 120 个字符"
	}
	if requireAPIKey && strings.TrimSpace(apiKey) == "" && provider.APIKeySecretID <= 0 {
		fields["api_key"] = "首次启用必须填写 API Key"
	}
	if provider.RequestTimeoutSeconds < 5 || provider.RequestTimeoutSeconds > 300 {
		fields["timeout_seconds"] = "超时时间必须在 5 到 300 秒之间"
	}
	if provider.MaxRPM < 1 || provider.MaxRPM > 10000 {
		fields["max_rpm"] = "RPM 必须在 1 到 10000 之间"
	}
	if provider.Priority < 1 || provider.Priority > 10000 {
		fields["priority"] = "优先级必须在 1 到 10000 之间"
	}
	switch provider.CostTier {
	case "low", "medium", "high":
	default:
		fields["cost_tier"] = "成本等级必须是 low、medium 或 high"
	}
	return fields
}

func makeAIProviderDefault(providers []AIProvider, providerKey string) []AIProvider {
	order := make([]int, 0, len(providers))
	targetIndex := findAIProviderIndex(providers, providerKey)
	if targetIndex >= 0 {
		order = append(order, targetIndex)
	}
	remaining := make([]int, 0, len(providers))
	for i := range providers {
		if i != targetIndex {
			remaining = append(remaining, i)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		left := providers[remaining[i]]
		right := providers[remaining[j]]
		if left.Priority == right.Priority {
			return left.ProviderKey < right.ProviderKey
		}
		return left.Priority < right.Priority
	})
	order = append(order, remaining...)
	for i, index := range order {
		providers[index].Priority = (i + 1) * 10
	}
	return providers
}

func aiConfigEditReport(ctx context.Context, db *sql.DB, cfg AIConfigData, publishing bool, providerTests map[string]aiProviderTestSummary) map[string]any {
	report := validateAIConfigData(ctx, db, cfg, publishing)
	if route, ok := cfg.Chat.Routing.TaskClasses["standard"]; ok {
		report["route_provider_keys"] = route.Providers
	} else {
		report["route_provider_keys"] = []string{}
	}
	if len(providerTests) > 0 {
		report["provider_tests"] = providerTests
	}
	return report
}

func (s *Server) testAIProvidersForApply(ctx context.Context, cfg AIConfigData, policy string) (map[string]aiProviderTestSummary, map[string]string) {
	route := cfg.Chat.Routing.TaskClasses["standard"]
	providerKeys := append([]string(nil), route.Providers...)
	if policy == "default_only" && len(providerKeys) > 1 {
		providerKeys = providerKeys[:1]
	}
	providersByKey := map[string]AIProvider{}
	for _, provider := range cfg.Chat.Providers {
		providersByKey[provider.ProviderKey] = provider
	}
	tests := map[string]aiProviderTestSummary{}
	providerErrors := map[string]string{}
	for _, providerKey := range providerKeys {
		provider, ok := providersByKey[providerKey]
		if !ok {
			continue
		}
		apiKey, err := s.decryptAISecret(ctx, provider.APIKeySecretID)
		if err != nil {
			providerErrors[providerKey] = sanitizeProviderError(err.Error())
			continue
		}
		test, err := s.testOpenAICompatibleProvider(ctx, provider, apiKey, provider.RequestTimeoutSeconds)
		if err != nil {
			providerErrors[providerKey] = sanitizeProviderError(err.Error())
			continue
		}
		tests[providerKey] = test
	}
	return tests, providerErrors
}

func saveEditableAIConfig(ctx context.Context, db *sql.DB, base AIConfigVersion, hasEditable bool, cfg AIConfigData, viewer, validationStatus, validationReport, errorMessage string) (AIConfigVersion, error) {
	cfg = normalizeAIConfig(cfg)
	raw, hash, err := aiConfigJSONAndHash(cfg)
	if err != nil {
		return AIConfigVersion{}, err
	}
	now := nowString()
	status := "draft"
	if validationStatus == "fail" {
		status = "failed"
	}
	if hasEditable {
		if _, err := db.ExecContext(ctx, `UPDATE ai_config_versions SET status = ?, config_hash = ?, config_json = ?,
			secret_refs_json = ?, validation_status = ?, validation_report_json = ?, error_message = ?, updated_at = ?
			WHERE version = ? AND status IN ('draft', 'failed')`,
			status, hash, raw, encodeJSON(aiSecretRefs(cfg)), validationStatus, validationReport, errorMessage, now, base.Version); err != nil {
			return AIConfigVersion{}, err
		}
		return getAIConfigVersion(ctx, db, base.Version)
	}
	var maxVersion int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM ai_config_versions`).Scan(&maxVersion); err != nil {
		return AIConfigVersion{}, err
	}
	version := maxVersion + 1
	if _, err := db.ExecContext(ctx, `INSERT INTO ai_config_versions
		(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
		 created_by_viewer, created_at, updated_at, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version, status, hash, raw, encodeJSON(aiSecretRefs(cfg)), validationStatus, validationReport, viewer, now, now, errorMessage); err != nil {
		return AIConfigVersion{}, err
	}
	return getAIConfigVersion(ctx, db, version)
}

func publishAIConfig(ctx context.Context, db *sql.DB, base AIConfigVersion, hasEditable bool, cfg AIConfigData, viewer, validationReport string) (AIConfigVersion, error) {
	cfg = normalizeAIConfig(cfg)
	raw, hash, err := aiConfigJSONAndHash(cfg)
	if err != nil {
		return AIConfigVersion{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AIConfigVersion{}, err
	}
	defer rollback(tx)
	now := nowString()
	version := base.Version
	if hasEditable {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'draft', config_hash = ?, config_json = ?,
			secret_refs_json = ?, validation_status = 'pass', validation_report_json = ?, error_message = '', updated_at = ?
			WHERE version = ? AND status IN ('draft', 'failed')`,
			hash, raw, encodeJSON(aiSecretRefs(cfg)), validationReport, now, version); err != nil {
			return AIConfigVersion{}, err
		}
	} else {
		var maxVersion int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM ai_config_versions`).Scan(&maxVersion); err != nil {
			return AIConfigVersion{}, err
		}
		version = maxVersion + 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_config_versions
			(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
			 created_by_viewer, created_at, updated_at)
			VALUES (?, 'draft', ?, ?, ?, 'pass', ?, ?, ?, ?)`,
			version, hash, raw, encodeJSON(aiSecretRefs(cfg)), validationReport, viewer, now, now); err != nil {
			return AIConfigVersion{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'superseded', superseded_at = ?, updated_at = ?
		WHERE status = 'active'`, now, now); err != nil {
		return AIConfigVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'active', published_by_viewer = ?,
		published_at = ?, updated_at = ?, error_message = ''
		WHERE version = ?`, viewer, now, now, version); err != nil {
		return AIConfigVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return AIConfigVersion{}, err
	}
	return getAIConfigVersion(ctx, db, version)
}

func publishAIEnabled(ctx context.Context, db *sql.DB, enabled bool, viewer string) error {
	active, err := ensureActiveAIConfig(ctx, db)
	if err != nil {
		return err
	}
	cfg := normalizeAIConfig(active.Config)
	cfg.Enabled = enabled
	raw, hash, err := aiConfigJSONAndHash(cfg)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := nowString()
	var maxVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM ai_config_versions`).Scan(&maxVersion); err != nil {
		return err
	}
	version := maxVersion + 1
	if _, err := tx.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'superseded', superseded_at = ?, updated_at = ?
		WHERE status = 'active'`, now, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_config_versions
		(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
		 created_by_viewer, published_by_viewer, created_at, updated_at, published_at)
		VALUES (?, 'active', ?, ?, ?, 'pass', ?, ?, ?, ?, ?, ?)`,
		version, hash, raw, encodeJSON(aiSecretRefs(cfg)), encodeJSON(map[string]any{"ok": true, "enabled": enabled}),
		viewer, viewer, now, now, now); err != nil {
		return err
	}
	return tx.Commit()
}

func pickSettingsProvider(providers []aiSettingsProviderSummary, providerKey string) aiSettingsProviderSummary {
	for _, provider := range providers {
		if provider.ProviderKey == providerKey {
			return provider
		}
	}
	if len(providers) > 0 {
		return providers[0]
	}
	return aiSettingsProviderSummary{}
}

func (s *Server) handleAISessions(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			limit := cleanLimit(r.URL.Query().Get("limit"), 50, 200)
			sessions, err := listAISessions(r.Context(), s.db, limit, r.URL.Query().Get("viewer"), r.URL.Query().Get("q"), r.URL.Query().Get("archived"))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": sessions})
		case http.MethodPost:
			var req aiSessionRequest
			if err := decodeBody(r.Body, &req); err != nil {
				writeError(w, err)
				return
			}
			session, err := createAISession(r.Context(), s.db, req.Title, s.viewerKey(r), req.Scope)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, session)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	sessionID, err := parseID(parts[0])
	if err != nil {
		writeError(w, err)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			session, err := getAISession(r.Context(), s.db, sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, session)
		case http.MethodPatch:
			var req aiSessionPatchRequest
			if err := decodeBody(r.Body, &req); err != nil {
				writeError(w, err)
				return
			}
			session, err := updateAISession(r.Context(), s.db, sessionID, req)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, session)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if parts[1] != "messages" {
		writeError(w, errNotFound("not found"))
		return
	}
	if len(parts) == 3 && parts[2] == "stream" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleAIMessageStream(w, r, sessionID)
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			messages, err := listAIMessages(r.Context(), s.db, sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			candidates, err := listAIServiceCandidatesForSession(r.Context(), s.db, sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			citations, err := listAICitationsForSession(r.Context(), s.db, sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, aiMessagesResponse{
				Items:             messages,
				ServiceCandidates: candidates,
				Citations:         citations,
			})
		case http.MethodPost:
			var req aiAskRequest
			if err := decodeBody(r.Body, &req); err != nil {
				writeError(w, err)
				return
			}
			result, err := s.askAIQuestion(r.Context(), sessionID, strings.TrimSpace(req.Question), req.ScopeOverride, s.viewerKey(r))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 4 && parts[3] == "feedback" {
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "memory_candidate": false})
		return
	}
	writeError(w, errNotFound("not found"))
}

func (s *Server) handleAccessAIHistory(w http.ResponseWriter, r *http.Request, payload accessTokenPayload, parts []string) {
	if !payload.hasCapability(accessTokenCapabilityAIHistoryRead) {
		writeError(w, errForbidden("access token missing ai.history.read capability"))
		return
	}
	if len(parts) == 1 && parts[0] == "sessions" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		limit := cleanLimit(r.URL.Query().Get("limit"), 50, 200)
		resp, err := listAIHistorySessions(r.Context(), s.db, aiHistorySessionQuery{
			Limit:         limit,
			Cursor:        r.URL.Query().Get("cursor"),
			Viewer:        payload.Scope.ViewerKey,
			Q:             r.URL.Query().Get("q"),
			Archived:      r.URL.Query().Get("archived"),
			UpdatedAfter:  r.URL.Query().Get("updated_after"),
			UpdatedBefore: r.URL.Query().Get("updated_before"),
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if len(parts) == 2 && parts[0] == "sessions" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		sessionID, err := parseID(parts[1])
		if err != nil {
			writeError(w, err)
			return
		}
		resp, err := getAIHistorySessionDetail(r.Context(), s.db, sessionID, payload.Scope.ViewerKey)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeError(w, errNotFound("not found"))
}

func (s *Server) handleAccessAIDiagnostics(w http.ResponseWriter, r *http.Request, payload accessTokenPayload, parts []string) {
	if !payload.hasCapability(accessTokenCapabilityAIDiagnosticsRead) {
		writeError(w, errForbidden("access token missing ai.diagnostics.read capability"))
		return
	}
	if len(parts) == 1 && parts[0] == "data-sources" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		resp, err := s.getAIDiagnosticsDataSources(r.Context(), AIQuestionScope{})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if len(parts) == 1 && parts[0] == "runs" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var sessionID int64
		if raw := strings.TrimSpace(r.URL.Query().Get("session_id")); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed <= 0 {
				writeError(w, errBadRequest("invalid session_id"))
				return
			}
			sessionID = parsed
		}
		resp, err := listAIDiagnosticsRuns(r.Context(), s.db, aiDiagnosticsRunQuery{
			Limit:         cleanLimit(r.URL.Query().Get("limit"), 50, 200),
			Cursor:        r.URL.Query().Get("cursor"),
			Viewer:        payload.Scope.ViewerKey,
			SessionID:     sessionID,
			Status:        r.URL.Query().Get("status"),
			Q:             r.URL.Query().Get("q"),
			StartedAfter:  r.URL.Query().Get("started_after"),
			StartedBefore: r.URL.Query().Get("started_before"),
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if len(parts) == 2 && parts[0] == "runs" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		runID, err := parseID(parts[1])
		if err != nil {
			writeError(w, err)
			return
		}
		resp, err := s.getAIDiagnosticsRunDetail(r.Context(), runID, payload.Scope.ViewerKey)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeError(w, errNotFound("not found"))
}

func (s *Server) handleAIMessageStream(w http.ResponseWriter, r *http.Request, sessionID int64) {
	var req aiAskRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, errInternal("streaming is not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	streamOK := true
	emit := func(event string, data any) error {
		if event == "error" {
			streamOK = false
		}
		return writeSSE(w, flusher, event, data)
	}
	if err := s.askAIQuestionStream(r.Context(), sessionID, strings.TrimSpace(req.Question), req.ScopeOverride, s.viewerKey(r), emit); err != nil {
		streamOK = false
		_ = emit("error", map[string]any{"message": sanitizeProviderError(err.Error())})
	}
	_ = emit("done", map[string]any{"ok": streamOK})
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func (s *Server) handleAIRuns(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 0 {
		writeError(w, errNotFound("not found"))
		return
	}
	runID, err := parseID(parts[0])
	if err != nil {
		writeError(w, err)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		run, err := getAIRun(r.Context(), s.db, runID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
		return
	}
	switch parts[1] {
	case "steps":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		steps, err := listAIRunSteps(r.Context(), s.db, runID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": steps})
	case "cancel":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "status": "cancel_unsupported_for_sync_runs"})
	case "retry":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeError(w, errBadRequest("retry is not available for completed synchronous runs yet"))
	default:
		writeError(w, errNotFound("not found"))
	}
}

func ensureActiveAIConfig(ctx context.Context, db *sql.DB) (AIConfigVersion, error) {
	active, err := latestAIConfigByStatus(ctx, db, "active")
	if err == nil && active != nil {
		return *active, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AIConfigVersion{}, err
	}
	cfg := normalizeAIConfig(defaultAIConfig())
	raw, hash, err := aiConfigJSONAndHash(cfg)
	if err != nil {
		return AIConfigVersion{}, err
	}
	now := nowString()
	_, err = db.ExecContext(ctx, `INSERT INTO ai_config_versions
		(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
		 created_at, updated_at, published_at)
		VALUES (1, 'active', ?, ?, '[]', 'pass', ?, ?, ?, ?)`,
		hash, raw, `{"ok":true,"default":true}`, now, now, now)
	if err != nil {
		return AIConfigVersion{}, err
	}
	active, err = latestAIConfigByStatus(ctx, db, "active")
	if err != nil {
		return AIConfigVersion{}, err
	}
	return *active, nil
}

func latestAIConfigByStatus(ctx context.Context, db *sql.DB, status string) (*AIConfigVersion, error) {
	row := db.QueryRowContext(ctx, `SELECT id, version, status, config_hash, config_json, secret_refs_json,
		validation_status, validation_report_json, created_by_viewer, published_by_viewer, created_at, updated_at,
		published_at, superseded_at, error_message
		FROM ai_config_versions WHERE status = ? ORDER BY version DESC LIMIT 1`, status)
	cfg, err := scanAIConfigVersion(row)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func getAIConfigVersion(ctx context.Context, db *sql.DB, version int) (AIConfigVersion, error) {
	row := db.QueryRowContext(ctx, `SELECT id, version, status, config_hash, config_json, secret_refs_json,
		validation_status, validation_report_json, created_by_viewer, published_by_viewer, created_at, updated_at,
		published_at, superseded_at, error_message
		FROM ai_config_versions WHERE version = ?`, version)
	cfg, err := scanAIConfigVersion(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AIConfigVersion{}, errNotFound("ai config version not found")
		}
		return AIConfigVersion{}, err
	}
	return cfg, nil
}

func scanAIConfigVersion(row aiConfigRowScanner) (AIConfigVersion, error) {
	var cfg AIConfigVersion
	var raw, refs string
	if err := row.Scan(&cfg.ID, &cfg.Version, &cfg.Status, &cfg.ConfigHash, &raw, &refs, &cfg.ValidationStatus,
		&cfg.ValidationReportJSON, &cfg.CreatedByViewer, &cfg.PublishedByViewer, &cfg.CreatedAt, &cfg.UpdatedAt,
		&cfg.PublishedAt, &cfg.SupersededAt, &cfg.ErrorMessage); err != nil {
		return AIConfigVersion{}, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg.Config); err != nil {
		return AIConfigVersion{}, err
	}
	cfg.Config = normalizeAIConfig(cfg.Config)
	cfg.SecretRefs = decodeInt64List(refs)
	return cfg, nil
}

func createAIConfigDraft(ctx context.Context, db *sql.DB, viewer string) (AIConfigVersion, error) {
	active, err := ensureActiveAIConfig(ctx, db)
	if err != nil {
		return AIConfigVersion{}, err
	}
	var maxVersion int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM ai_config_versions`).Scan(&maxVersion); err != nil {
		return AIConfigVersion{}, err
	}
	raw, hash, err := aiConfigJSONAndHash(active.Config)
	if err != nil {
		return AIConfigVersion{}, err
	}
	now := nowString()
	_, err = db.ExecContext(ctx, `INSERT INTO ai_config_versions
		(version, status, config_hash, config_json, secret_refs_json, validation_status, validation_report_json,
		 created_by_viewer, created_at, updated_at)
		VALUES (?, 'draft', ?, ?, ?, 'not_run', '', ?, ?, ?)`,
		maxVersion+1, hash, raw, encodeJSON(aiSecretRefs(active.Config)), viewer, now, now)
	if err != nil {
		return AIConfigVersion{}, err
	}
	return getAIConfigVersion(ctx, db, maxVersion+1)
}

func updateAIConfigDraft(ctx context.Context, db *sql.DB, version int, data AIConfigData) (AIConfigVersion, error) {
	current, err := getAIConfigVersion(ctx, db, version)
	if err != nil {
		return AIConfigVersion{}, err
	}
	if current.Status != "draft" && current.Status != "failed" {
		return AIConfigVersion{}, errBadRequest("only draft or failed config can be updated")
	}
	data = normalizeAIConfig(data)
	raw, hash, err := aiConfigJSONAndHash(data)
	if err != nil {
		return AIConfigVersion{}, err
	}
	_, err = db.ExecContext(ctx, `UPDATE ai_config_versions SET config_hash = ?, config_json = ?, secret_refs_json = ?,
		validation_status = 'not_run', validation_report_json = '', status = 'draft', error_message = '', updated_at = ?
		WHERE version = ?`, hash, raw, encodeJSON(aiSecretRefs(data)), nowString(), version)
	if err != nil {
		return AIConfigVersion{}, err
	}
	return getAIConfigVersion(ctx, db, version)
}

func validateAIConfigDraft(ctx context.Context, db *sql.DB, version int, publishing bool) (AIConfigVersion, error) {
	cfg, err := getAIConfigVersion(ctx, db, version)
	if err != nil {
		return AIConfigVersion{}, err
	}
	if cfg.Status != "draft" && cfg.Status != "failed" {
		return AIConfigVersion{}, errBadRequest("only draft config can be validated")
	}
	report := validateAIConfigData(ctx, db, cfg.Config, publishing)
	status := "pass"
	if !report["ok"].(bool) {
		status = "fail"
	}
	reportJSON := encodeJSON(report)
	_, err = db.ExecContext(ctx, `UPDATE ai_config_versions SET validation_status = ?, validation_report_json = ?,
		status = CASE WHEN ? = 'fail' THEN 'failed' ELSE 'draft' END, error_message = ?, updated_at = ?
		WHERE version = ?`, status, reportJSON, status, firstAIValidationError(report), nowString(), version)
	if err != nil {
		return AIConfigVersion{}, err
	}
	return getAIConfigVersion(ctx, db, version)
}

func publishAIConfigDraft(ctx context.Context, db *sql.DB, version int, viewer string) (AIConfigVersion, error) {
	cfg, err := validateAIConfigDraft(ctx, db, version, true)
	if err != nil {
		return AIConfigVersion{}, err
	}
	if cfg.ValidationStatus != "pass" {
		return AIConfigVersion{}, errBadRequest("ai config validation failed: " + cfg.ErrorMessage)
	}
	now := nowString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AIConfigVersion{}, err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'superseded', superseded_at = ?, updated_at = ?
		WHERE status = 'active'`, now, now); err != nil {
		return AIConfigVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_config_versions SET status = 'active', published_by_viewer = ?,
		published_at = ?, updated_at = ?, error_message = ''
		WHERE version = ?`, viewer, now, now, version); err != nil {
		return AIConfigVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return AIConfigVersion{}, err
	}
	return getAIConfigVersion(ctx, db, version)
}

func validateAIConfigData(ctx context.Context, db *sql.DB, cfg AIConfigData, publishing bool) map[string]any {
	errorsList := []string{}
	providers := map[string]AIProvider{}
	for _, provider := range cfg.Chat.Providers {
		if provider.ProviderKey == "" || !validProviderKey(provider.ProviderKey) {
			errorsList = append(errorsList, "provider_key is invalid: "+provider.Name)
			continue
		}
		if provider.Name == "" {
			errorsList = append(errorsList, "provider name is required")
			continue
		}
		if _, exists := providers[provider.ProviderKey]; exists {
			errorsList = append(errorsList, "provider_key must be unique: "+provider.ProviderKey)
		}
		providers[provider.ProviderKey] = provider
		if provider.BaseURL == "" {
			errorsList = append(errorsList, "provider base_url is required: "+provider.Name)
		} else if _, err := openAICompatibleChatURL(provider.BaseURL); err != nil {
			errorsList = append(errorsList, "provider base_url is invalid: "+provider.Name)
		}
		if provider.Model == "" {
			errorsList = append(errorsList, "provider model is required: "+provider.Name)
		}
		if provider.APIKeySecretID > 0 && !aiSecretExists(ctx, db, provider.APIKeySecretID) {
			errorsList = append(errorsList, fmt.Sprintf("secret not found: %d", provider.APIKeySecretID))
		}
	}
	if cfg.Enabled && len(cfg.Chat.Providers) == 0 {
		errorsList = append(errorsList, "at least one chat provider is required when AI is enabled")
	}
	for className, route := range cfg.Chat.Routing.TaskClasses {
		if len(route.Providers) == 0 && cfg.Enabled && publishing {
			errorsList = append(errorsList, "task class has no providers: "+className)
		}
		for _, name := range route.Providers {
			provider, ok := providers[name]
			if !ok {
				errorsList = append(errorsList, "routing references missing provider: "+name)
				continue
			}
			if cfg.Enabled && publishing && provider.APIKeySecretID <= 0 {
				errorsList = append(errorsList, "provider api_key_secret_id is required when AI is enabled: "+provider.Name)
			}
		}
		if route.FallbackTaskClass != "" {
			if _, ok := cfg.Chat.Routing.TaskClasses[route.FallbackTaskClass]; !ok {
				errorsList = append(errorsList, "routing references missing fallback task class: "+route.FallbackTaskClass)
			}
		}
	}
	if publishing && cfg.Enabled {
		hasUsable := false
		for _, provider := range cfg.Chat.Providers {
			if provider.APIKeySecretID > 0 {
				hasUsable = true
				break
			}
		}
		if !hasUsable {
			errorsList = append(errorsList, "no provider has a saved API key")
		}
	}
	return map[string]any{
		"ok":             len(errorsList) == 0,
		"errors":         errorsList,
		"provider_count": len(cfg.Chat.Providers),
		"chunker":        aiChunkerVersion,
	}
}

func firstAIValidationError(report map[string]any) string {
	values, ok := report["errors"].([]string)
	if ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

func aiConfigJSONAndHash(cfg AIConfigData) (string, string, error) {
	cfg = normalizeAIConfig(cfg)
	cfg = stripAISecretMetadata(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:])[:16], nil
}

func stripAISecretMetadata(cfg AIConfigData) AIConfigData {
	for i := range cfg.Chat.Providers {
		cfg.Chat.Providers[i].SecretConfigured = false
		cfg.Chat.Providers[i].SecretLast4 = ""
		cfg.Chat.Providers[i].SecretFingerprint = ""
	}
	return cfg
}

func aiSecretRefs(cfg AIConfigData) []int64 {
	seen := map[int64]struct{}{}
	var refs []int64
	for _, provider := range cfg.Chat.Providers {
		if provider.APIKeySecretID <= 0 {
			continue
		}
		if _, ok := seen[provider.APIKeySecretID]; ok {
			continue
		}
		seen[provider.APIKeySecretID] = struct{}{}
		refs = append(refs, provider.APIKeySecretID)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i] < refs[j] })
	return refs
}

func decodeInt64List(raw string) []int64 {
	var values []int64
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &values); err != nil {
		return nil
	}
	return values
}

func aiSecretExists(ctx context.Context, db *sql.DB, id int64) bool {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM ai_secrets WHERE id = ?`, id).Scan(&exists)
	return err == nil
}

func (s *Server) withSecretMetadata(ctx context.Context, cfg AIConfigVersion) (AIConfigVersion, error) {
	for i := range cfg.Config.Chat.Providers {
		secretID := cfg.Config.Chat.Providers[i].APIKeySecretID
		if secretID <= 0 {
			continue
		}
		var last4, fingerprint string
		err := s.db.QueryRowContext(ctx, `SELECT last4, fingerprint FROM ai_secrets WHERE id = ?`, secretID).Scan(&last4, &fingerprint)
		if err != nil {
			continue
		}
		cfg.Config.Chat.Providers[i].SecretConfigured = true
		cfg.Config.Chat.Providers[i].SecretLast4 = last4
		cfg.Config.Chat.Providers[i].SecretFingerprint = fingerprint
	}
	return cfg, nil
}

func (s *Server) createOrUpdateAISecret(ctx context.Context, id int64, req aiSecretRequest, viewer string) (AISecret, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "provider-api-key"
	}
	if req.SecretType == "" {
		req.SecretType = "api_key"
	}
	value := strings.TrimSpace(req.Value)
	if value == "" {
		return AISecret{}, errBadRequest("secret value is required")
	}
	masterKey, err := s.aiMasterKey()
	if err != nil {
		return AISecret{}, err
	}
	encrypted, err := encryptAISecret(masterKey, value)
	if err != nil {
		return AISecret{}, err
	}
	fingerprint := aiSecretFingerprint(value)
	last4 := last4(value)
	now := nowString()
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO ai_secrets
			(name, secret_type, encrypted_value, fingerprint, last4, created_by_viewer, updated_by_viewer, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Name, req.SecretType, encrypted, fingerprint, last4, viewer, viewer, now, now)
		if err != nil {
			return AISecret{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return AISecret{}, err
		}
	} else {
		res, err := s.db.ExecContext(ctx, `UPDATE ai_secrets SET name = ?, secret_type = ?, encrypted_value = ?,
			fingerprint = ?, last4 = ?, updated_by_viewer = ?, updated_at = ? WHERE id = ?`,
			req.Name, req.SecretType, encrypted, fingerprint, last4, viewer, now, id)
		if err != nil {
			return AISecret{}, err
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return AISecret{}, errNotFound("secret not found")
		}
	}
	return getAISecret(ctx, s.db, id)
}

func getAISecret(ctx context.Context, db *sql.DB, id int64) (AISecret, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, secret_type, fingerprint, last4,
		created_by_viewer, updated_by_viewer, created_at, updated_at FROM ai_secrets WHERE id = ?`, id)
	var secret AISecret
	if err := row.Scan(&secret.ID, &secret.Name, &secret.SecretType, &secret.Fingerprint, &secret.Last4,
		&secret.CreatedByViewer, &secret.UpdatedByViewer, &secret.CreatedAt, &secret.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AISecret{}, errNotFound("secret not found")
		}
		return AISecret{}, err
	}
	return secret, nil
}

func (s *Server) decryptAISecret(ctx context.Context, id int64) (string, error) {
	var encrypted string
	err := s.db.QueryRowContext(ctx, `SELECT encrypted_value FROM ai_secrets WHERE id = ?`, id).Scan(&encrypted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errNotFound("secret not found")
		}
		return "", err
	}
	masterKey, err := s.aiMasterKey()
	if err != nil {
		return "", err
	}
	return decryptAISecret(masterKey, encrypted)
}

func (s *Server) aiEncryptionReady() bool {
	_, err := s.aiMasterKey()
	return err == nil
}

func (s *Server) aiMasterKey() ([]byte, error) {
	keyPath := filepath.Join(s.cfg.DataDir, "secrets", "ai-master.key")
	raw, err := os.ReadFile(keyPath)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) != 32 {
			return nil, errUnavailable("AI local encryption key is invalid")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func encryptAISecret(masterKey []byte, plaintext string) (string, error) {
	key := sha256.Sum256(masterKey)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func decryptAISecret(masterKey []byte, encrypted string) (string, error) {
	payload, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", errBadRequest("invalid encrypted secret")
	}
	key := sha256.Sum256(masterKey)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) <= gcm.NonceSize() {
		return "", errBadRequest("invalid encrypted secret")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errUnauthorized("cannot decrypt AI secret")
	}
	return string(plain), nil
}

func aiSecretFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func last4(value string) string {
	runes := []rune(value)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

func (s *Server) viewerKey(r *http.Request) string {
	headers := []string{"Remote-Email", "Remote-User", "Remote-Name", "X-Forwarded-User"}
	if active, err := ensureActiveAIConfig(r.Context(), s.db); err == nil && len(active.Config.Viewer.HeaderCandidates) > 0 {
		headers = active.Config.Viewer.HeaderCandidates
	}
	for _, header := range headers {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	return ""
}

func listAISessions(ctx context.Context, db *sql.DB, limit int, viewer, q, archived string) ([]AISession, error) {
	query := `SELECT id, title, viewer_key, scope_json, created_at, updated_at, archived_at FROM ai_sessions WHERE 1=1`
	args := []any{}
	if viewer = strings.TrimSpace(viewer); viewer != "" {
		query += ` AND viewer_key = ?`
		args = append(args, viewer)
	}
	if q = strings.TrimSpace(q); q != "" {
		query += ` AND title LIKE ?`
		args = append(args, "%"+q+"%")
	}
	switch archived {
	case "1", "true":
		query += ` AND archived_at <> ''`
	default:
		query += ` AND archived_at = ''`
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []AISession{}
	for rows.Next() {
		session, err := scanAISession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

type aiHistorySessionQuery struct {
	Limit         int
	Cursor        string
	Viewer        string
	Q             string
	Archived      string
	UpdatedAfter  string
	UpdatedBefore string
}

func listAIHistorySessions(ctx context.Context, db *sql.DB, req aiHistorySessionQuery) (aiHistorySessionsResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := `SELECT id, title, viewer_key, scope_json, created_at, updated_at, archived_at FROM ai_sessions WHERE 1=1`
	args := []any{}
	if viewer := strings.TrimSpace(req.Viewer); viewer != "" {
		query += ` AND viewer_key = ?`
		args = append(args, viewer)
	}
	if q := strings.TrimSpace(req.Q); q != "" {
		query += ` AND title LIKE ?`
		args = append(args, "%"+q+"%")
	}
	switch strings.TrimSpace(req.Archived) {
	case "1", "true":
		query += ` AND archived_at <> ''`
	case "all":
	default:
		query += ` AND archived_at = ''`
	}
	if updatedAfter := strings.TrimSpace(req.UpdatedAfter); updatedAfter != "" {
		if _, err := time.Parse(timeLayout, updatedAfter); err != nil {
			return aiHistorySessionsResponse{}, errBadRequest("updated_after must be RFC3339")
		}
		query += ` AND updated_at >= ?`
		args = append(args, updatedAfter)
	}
	if updatedBefore := strings.TrimSpace(req.UpdatedBefore); updatedBefore != "" {
		if _, err := time.Parse(timeLayout, updatedBefore); err != nil {
			return aiHistorySessionsResponse{}, errBadRequest("updated_before must be RFC3339")
		}
		query += ` AND updated_at <= ?`
		args = append(args, updatedBefore)
	}
	if cursorRaw := strings.TrimSpace(req.Cursor); cursorRaw != "" {
		cursor, err := decodeAIHistorySessionCursor(cursorRaw)
		if err != nil {
			return aiHistorySessionsResponse{}, err
		}
		query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, cursor.UpdatedAt, cursor.UpdatedAt, cursor.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return aiHistorySessionsResponse{}, err
	}
	defer rows.Close()
	sessions := []AISession{}
	for rows.Next() {
		session, err := scanAISession(rows)
		if err != nil {
			return aiHistorySessionsResponse{}, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return aiHistorySessionsResponse{}, err
	}
	resp := aiHistorySessionsResponse{Items: sessions}
	if len(resp.Items) > limit {
		last := resp.Items[limit-1]
		resp.Items = resp.Items[:limit]
		resp.NextCursor = encodeAIHistorySessionCursor(aiHistorySessionCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
	}
	return resp, nil
}

func encodeAIHistorySessionCursor(cursor aiHistorySessionCursor) string {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeAIHistorySessionCursor(raw string) (aiHistorySessionCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return aiHistorySessionCursor{}, errBadRequest("invalid cursor")
	}
	var cursor aiHistorySessionCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.UpdatedAt == "" || cursor.ID <= 0 {
		return aiHistorySessionCursor{}, errBadRequest("invalid cursor")
	}
	if _, err := time.Parse(timeLayout, cursor.UpdatedAt); err != nil {
		return aiHistorySessionCursor{}, errBadRequest("invalid cursor")
	}
	return cursor, nil
}

func listAIDiagnosticsRuns(ctx context.Context, db *sql.DB, req aiDiagnosticsRunQuery) (aiDiagnosticsRunsResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := `SELECT r.id, r.session_id, r.user_message_id, r.assistant_message_id, r.status,
		r.current_state, r.intent, r.scope_json, r.retrieval_plan_json, r.service_candidate_count, r.evidence_count,
		r.code_evidence_count, r.memory_count, r.unconfirmed_count, r.verification_status, r.verification_report_json,
		r.checkpoint_json, r.index_snapshot_id, r.config_version, r.config_hash, r.model, r.provider_name,
		r.provider_failover_json, r.model_route_json, r.escalation_count, r.estimated_cost_json, r.started_at,
		r.finished_at, r.error_message,
		s.id, s.title, s.viewer_key, s.scope_json, s.created_at, s.updated_at, s.archived_at,
		COALESCE(um.id, 0), COALESCE(um.content, ''),
		COALESCE(am.id, 0), COALESCE(am.status, '')
		FROM ai_agent_runs r
		JOIN ai_sessions s ON s.id = r.session_id
		LEFT JOIN ai_messages um ON um.id = r.user_message_id
		LEFT JOIN ai_messages am ON am.id = r.assistant_message_id
		WHERE 1=1`
	args := []any{}
	if viewer := strings.TrimSpace(req.Viewer); viewer != "" {
		query += ` AND s.viewer_key = ?`
		args = append(args, viewer)
	}
	if req.SessionID > 0 {
		query += ` AND r.session_id = ?`
		args = append(args, req.SessionID)
	}
	if status := strings.TrimSpace(req.Status); status != "" {
		query += ` AND r.status = ?`
		args = append(args, status)
	}
	if q := strings.TrimSpace(req.Q); q != "" {
		query += ` AND (s.title LIKE ? OR um.content LIKE ? OR r.error_message LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if startedAfter := strings.TrimSpace(req.StartedAfter); startedAfter != "" {
		if _, err := time.Parse(timeLayout, startedAfter); err != nil {
			return aiDiagnosticsRunsResponse{}, errBadRequest("started_after must be RFC3339")
		}
		query += ` AND r.started_at >= ?`
		args = append(args, startedAfter)
	}
	if startedBefore := strings.TrimSpace(req.StartedBefore); startedBefore != "" {
		if _, err := time.Parse(timeLayout, startedBefore); err != nil {
			return aiDiagnosticsRunsResponse{}, errBadRequest("started_before must be RFC3339")
		}
		query += ` AND r.started_at <= ?`
		args = append(args, startedBefore)
	}
	if cursorRaw := strings.TrimSpace(req.Cursor); cursorRaw != "" {
		cursor, err := decodeAIDiagnosticsRunCursor(cursorRaw)
		if err != nil {
			return aiDiagnosticsRunsResponse{}, err
		}
		query += ` AND (r.started_at < ? OR (r.started_at = ? AND r.id < ?))`
		args = append(args, cursor.StartedAt, cursor.StartedAt, cursor.ID)
	}
	query += ` ORDER BY r.started_at DESC, r.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return aiDiagnosticsRunsResponse{}, err
	}
	defer rows.Close()
	items := []aiDiagnosticsRunSummary{}
	for rows.Next() {
		item, err := scanAIDiagnosticsRunSummary(rows)
		if err != nil {
			return aiDiagnosticsRunsResponse{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return aiDiagnosticsRunsResponse{}, err
	}
	resp := aiDiagnosticsRunsResponse{Items: items}
	if len(resp.Items) > limit {
		last := resp.Items[limit-1]
		resp.Items = resp.Items[:limit]
		resp.NextCursor = encodeAIDiagnosticsRunCursor(aiDiagnosticsRunCursor{StartedAt: last.Run.StartedAt, ID: last.Run.ID})
	}
	return resp, nil
}

func scanAIDiagnosticsRunSummary(row repoScanner) (aiDiagnosticsRunSummary, error) {
	var item aiDiagnosticsRunSummary
	if err := row.Scan(&item.Run.ID, &item.Run.SessionID, &item.Run.UserMessageID, &item.Run.AssistantMessageID,
		&item.Run.Status, &item.Run.CurrentState, &item.Run.Intent, &item.Run.ScopeJSON, &item.Run.RetrievalPlanJSON,
		&item.Run.ServiceCandidateCount, &item.Run.EvidenceCount, &item.Run.CodeEvidenceCount, &item.Run.MemoryCount,
		&item.Run.UnconfirmedCount, &item.Run.VerificationStatus, &item.Run.VerificationReportJSON,
		&item.Run.CheckpointJSON, &item.Run.IndexSnapshotID, &item.Run.ConfigVersion, &item.Run.ConfigHash,
		&item.Run.Model, &item.Run.ProviderName, &item.Run.ProviderFailoverJSON, &item.Run.ModelRouteJSON,
		&item.Run.EscalationCount, &item.Run.EstimatedCostJSON, &item.Run.StartedAt, &item.Run.FinishedAt,
		&item.Run.ErrorMessage, &item.Session.ID, &item.Session.Title, &item.Session.ViewerKey, &item.Session.ScopeJSON,
		&item.Session.CreatedAt, &item.Session.UpdatedAt, &item.Session.ArchivedAt, &item.UserMessageID,
		&item.UserQuestion, &item.AssistantMessageID, &item.AssistantStatus); err != nil {
		return aiDiagnosticsRunSummary{}, err
	}
	item.Session = sanitizeAIDiagnosticsSession(item.Session)
	item.UserQuestion = sanitizeAIDiagnosticsSummaryString(item.UserQuestion)
	item.Run = sanitizeAIDiagnosticsRun(item.Run)
	item.DurationMS = aiRunDurationMS(item.Run)
	return item, nil
}

func encodeAIDiagnosticsRunCursor(cursor aiDiagnosticsRunCursor) string {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeAIDiagnosticsRunCursor(raw string) (aiDiagnosticsRunCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return aiDiagnosticsRunCursor{}, errBadRequest("invalid cursor")
	}
	var cursor aiDiagnosticsRunCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.StartedAt == "" || cursor.ID <= 0 {
		return aiDiagnosticsRunCursor{}, errBadRequest("invalid cursor")
	}
	if _, err := time.Parse(timeLayout, cursor.StartedAt); err != nil {
		return aiDiagnosticsRunCursor{}, errBadRequest("invalid cursor")
	}
	return cursor, nil
}

func (s *Server) getAIDiagnosticsDataSources(ctx context.Context, scope AIQuestionScope) (aiDiagnosticsSourcesResponse, error) {
	sources, err := s.buildAIDiagnosticsSources(ctx, scope)
	if err != nil {
		return aiDiagnosticsSourcesResponse{}, err
	}
	return aiDiagnosticsSourcesResponse{DataSources: sources}, nil
}

func (s *Server) buildAIDiagnosticsSources(ctx context.Context, scope AIQuestionScope) (aiDiagnosticsSources, error) {
	scope = normalizeAIScope(scope)
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return aiDiagnosticsSources{}, err
	}
	repos, err := listRepositories(ctx, s.db)
	if err != nil {
		return aiDiagnosticsSources{}, err
	}
	repos = filterAIRepos(repos, scope)
	out := aiDiagnosticsSources{
		Scope: scope,
		Indexing: aiDiagnosticsIndexingSummary{
			DefaultScanRoots: append([]string(nil), active.Config.Indexing.DefaultScanRoots...),
			ExcludeGlobs:     append([]string(nil), active.Config.Indexing.ExcludeGlobs...),
			MaxFileSize:      active.Config.Indexing.MaxFileSize,
		},
		Repositories: []aiDiagnosticsRepositorySource{},
		CurrentFile:  scope.CurrentFile,
	}
	for _, repo := range repos {
		if !repo.Enabled {
			continue
		}
		source, err := s.aiDiagnosticsRepositorySource(ctx, repo, scope.SourceMode)
		if err != nil {
			return aiDiagnosticsSources{}, err
		}
		out.Repositories = append(out.Repositories, source)
	}
	return out, nil
}

func (s *Server) aiDiagnosticsRepositorySource(ctx context.Context, repo Repository, sourceMode string) (aiDiagnosticsRepositorySource, error) {
	source := aiDiagnosticsRepositorySource{
		ID:                    repo.ID,
		Name:                  repo.Name,
		Slug:                  repo.Slug,
		Enabled:               repo.Enabled,
		DefaultBranch:         repo.DefaultBranch,
		TrackedBranches:       append([]string(nil), repo.TrackedBranches...),
		LatestIncludeBranches: append([]string(nil), repo.LatestIncludeBranches...),
		LatestExcludeBranches: append([]string(nil), repo.LatestExcludeBranches...),
		StaleBranchDays:       repo.StaleBranchDays,
		BranchPriority:        append([]string(nil), repo.BranchPriority...),
		SyncIntervalSeconds:   repo.SyncIntervalSeconds,
		MaxFileSizeBytes:      repo.MaxFileSizeBytes,
		ScanPaths:             []aiDiagnosticsScanPath{},
		CandidateTargets:      []aiDiagnosticsBranchTarget{},
		LatestScan:            sanitizeAIDiagnosticsScanRun(repo.LatestScan),
		CreatedAt:             repo.CreatedAt,
		UpdatedAt:             repo.UpdatedAt,
	}
	for _, scanPath := range repo.ScanPaths {
		if !scanPath.Enabled {
			continue
		}
		source.ScanPaths = append(source.ScanPaths, aiDiagnosticsScanPath{
			ID:           scanPath.ID,
			Path:         scanPath.Path,
			IncludeGlobs: append([]string(nil), scanPath.IncludeGlobs...),
			ExcludeGlobs: append([]string(nil), scanPath.ExcludeGlobs...),
			CreatedAt:    scanPath.CreatedAt,
			UpdatedAt:    scanPath.UpdatedAt,
		})
	}
	refs, err := listBranches(ctx, s.db, repo.ID)
	if err != nil {
		return aiDiagnosticsRepositorySource{}, err
	}
	defaultRef := pickDefaultAIRef(repo, refs)
	if defaultRef != nil {
		target := aiDiagnosticsBranchTarget{
			Branch:        defaultRef.RefName,
			CommitSHA:     defaultRef.CommitSHA,
			CommitTime:    defaultRef.CommitTime,
			LastScannedAt: defaultRef.LastScannedAt,
			SourceScope:   "smart_latest",
		}
		source.DefaultTarget = &target
	}
	if strings.Contains(sourceMode, "branch") {
		for _, ref := range refs {
			if defaultRef != nil && ref.RefName == defaultRef.RefName {
				continue
			}
			if !aiBranchCandidate(repo, ref) {
				continue
			}
			source.CandidateTargets = append(source.CandidateTargets, aiDiagnosticsBranchTarget{
				Branch:        ref.RefName,
				CommitSHA:     ref.CommitSHA,
				CommitTime:    ref.CommitTime,
				LastScannedAt: ref.LastScannedAt,
				SourceScope:   "branch_candidate",
			})
			if len(source.CandidateTargets) >= 4 {
				break
			}
		}
	}
	return source, nil
}

func sanitizeAIDiagnosticsScanRun(run *ScanRun) *aiDiagnosticsScanRun {
	if run == nil {
		return nil
	}
	return &aiDiagnosticsScanRun{
		ID:           run.ID,
		TriggerType:  run.TriggerType,
		Status:       run.Status,
		BranchCount:  run.BranchCount,
		FileCount:    run.FileCount,
		SkippedCount: run.SkippedCount,
		ErrorCount:   run.ErrorCount,
		StartedAt:    run.StartedAt,
		FinishedAt:   run.FinishedAt,
	}
}

func (s *Server) getAIDiagnosticsRunDetail(ctx context.Context, runID int64, viewer string) (aiDiagnosticsRunDetailResponse, error) {
	run, session, err := getAIDiagnosticsRunWithSession(ctx, s.db, runID, viewer)
	if err != nil {
		return aiDiagnosticsRunDetailResponse{}, err
	}
	userMessage, err := getAIMessage(ctx, s.db, run.UserMessageID)
	if err != nil {
		return aiDiagnosticsRunDetailResponse{}, err
	}
	var assistantMessage AIMessage
	if run.AssistantMessageID > 0 {
		assistantMessage, err = getAIMessage(ctx, s.db, run.AssistantMessageID)
		if err != nil {
			return aiDiagnosticsRunDetailResponse{}, err
		}
	}
	steps, err := listAIRunSteps(ctx, s.db, run.ID)
	if err != nil {
		return aiDiagnosticsRunDetailResponse{}, err
	}
	candidates, err := listAIServiceCandidatesForRun(ctx, s.db, run.ID)
	if err != nil {
		return aiDiagnosticsRunDetailResponse{}, err
	}
	citations, err := listAICitationsForRun(ctx, s.db, run.ID)
	if err != nil {
		return aiDiagnosticsRunDetailResponse{}, err
	}
	taskFrame, evidenceContract, contractCoverage := extractAIDiagnosticsRunArtifacts(run, steps)
	dataSources, err := s.buildAIDiagnosticsSources(ctx, aiScopeFromJSON(run.ScopeJSON))
	if err != nil {
		return aiDiagnosticsRunDetailResponse{}, err
	}
	return aiDiagnosticsRunDetailResponse{
		Session:           sanitizeAIDiagnosticsSession(session),
		UserMessage:       sanitizeAIDiagnosticsMessage(userMessage),
		AssistantMessage:  sanitizeAIDiagnosticsMessage(assistantMessage),
		Run:               run,
		TaskFrame:         taskFrame,
		EvidenceContract:  evidenceContract,
		ContractCoverage:  contractCoverage,
		AgentWorkflow:     buildAIDiagnosticsAgentWorkflow(run, steps, taskFrame, evidenceContract, contractCoverage),
		Steps:             sanitizeAIDiagnosticsSteps(steps),
		DataSources:       dataSources,
		ServiceCandidates: candidates,
		Citations:         citations,
	}, nil
}

func aiScopeFromJSON(raw string) AIQuestionScope {
	var scope AIQuestionScope
	_ = json.Unmarshal([]byte(raw), &scope)
	return scope
}

func getAIDiagnosticsRunWithSession(ctx context.Context, db *sql.DB, runID int64, viewer string) (AIAgentRun, AISession, error) {
	query := `SELECT r.id, r.session_id, r.user_message_id, r.assistant_message_id, r.status,
		r.current_state, r.intent, r.scope_json, r.retrieval_plan_json, r.service_candidate_count, r.evidence_count,
		r.code_evidence_count, r.memory_count, r.unconfirmed_count, r.verification_status, r.verification_report_json,
		r.checkpoint_json, r.index_snapshot_id, r.config_version, r.config_hash, r.model, r.provider_name,
		r.provider_failover_json, r.model_route_json, r.escalation_count, r.estimated_cost_json, r.started_at,
		r.finished_at, r.error_message,
		s.id, s.title, s.viewer_key, s.scope_json, s.created_at, s.updated_at, s.archived_at
		FROM ai_agent_runs r
		JOIN ai_sessions s ON s.id = r.session_id
		WHERE r.id = ?`
	args := []any{runID}
	if viewer = strings.TrimSpace(viewer); viewer != "" {
		query += ` AND s.viewer_key = ?`
		args = append(args, viewer)
	}
	row := db.QueryRowContext(ctx, query, args...)
	var run AIAgentRun
	var session AISession
	if err := row.Scan(&run.ID, &run.SessionID, &run.UserMessageID, &run.AssistantMessageID, &run.Status,
		&run.CurrentState, &run.Intent, &run.ScopeJSON, &run.RetrievalPlanJSON, &run.ServiceCandidateCount,
		&run.EvidenceCount, &run.CodeEvidenceCount, &run.MemoryCount, &run.UnconfirmedCount, &run.VerificationStatus,
		&run.VerificationReportJSON, &run.CheckpointJSON, &run.IndexSnapshotID, &run.ConfigVersion, &run.ConfigHash,
		&run.Model, &run.ProviderName, &run.ProviderFailoverJSON, &run.ModelRouteJSON, &run.EscalationCount,
		&run.EstimatedCostJSON, &run.StartedAt, &run.FinishedAt, &run.ErrorMessage, &session.ID, &session.Title,
		&session.ViewerKey, &session.ScopeJSON, &session.CreatedAt, &session.UpdatedAt, &session.ArchivedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AIAgentRun{}, AISession{}, errNotFound("ai run not found")
		}
		return AIAgentRun{}, AISession{}, err
	}
	run = sanitizeAIDiagnosticsRun(run)
	return run, session, nil
}

func getAIMessage(ctx context.Context, db *sql.DB, id int64) (AIMessage, error) {
	row := db.QueryRowContext(ctx, `SELECT id, session_id, role, content, model, provider_name, model_route_json,
		prompt_tokens, completion_tokens, latency_ms, status, error_message, created_at
		FROM ai_messages WHERE id = ?`, id)
	msg, err := scanAIMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AIMessage{}, errNotFound("ai message not found")
		}
		return AIMessage{}, err
	}
	return msg, nil
}

func sanitizeAIDiagnosticsSteps(steps []AIAgentStep) []aiDiagnosticsStep {
	out := make([]aiDiagnosticsStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, aiDiagnosticsStep{
			ID:                  step.ID,
			RunID:               step.RunID,
			ParentStepID:        step.ParentStepID,
			AgentName:           step.AgentName,
			StepType:            step.StepType,
			Status:              step.Status,
			ToolName:            step.ToolName,
			TaskClass:           step.TaskClass,
			Model:               step.Model,
			ProviderName:        step.ProviderName,
			ModelRouteReason:    step.ModelRouteReason,
			EscalatedFromStepID: step.EscalatedFromStepID,
			TokenInput:          step.TokenInput,
			TokenOutput:         step.TokenOutput,
			EstimatedCost:       step.EstimatedCost,
			LatencyMS:           step.LatencyMS,
			ErrorMessage:        sanitizeProviderError(step.ErrorMessage),
			Input:               sanitizeAIDiagnosticsStepPayload(step.InputJSON),
			Output:              sanitizeAIDiagnosticsStepPayload(step.OutputJSON),
			Summary:             sanitizeAIDiagnosticsStepSummary(step),
			CreatedAt:           step.CreatedAt,
			FinishedAt:          step.FinishedAt,
		})
	}
	return out
}

func sanitizeAIDiagnosticsRun(run AIAgentRun) AIAgentRun {
	run.ScopeJSON = sanitizeAIDiagnosticsJSONText(run.ScopeJSON, false)
	run.ErrorMessage = sanitizeProviderError(run.ErrorMessage)
	run.RetrievalPlanJSON = sanitizeAIDiagnosticsJSONText(run.RetrievalPlanJSON, false)
	run.VerificationReportJSON = sanitizeAIDiagnosticsJSONText(run.VerificationReportJSON, false)
	run.CheckpointJSON = sanitizeAIDiagnosticsJSONText(run.CheckpointJSON, true)
	run.ProviderFailoverJSON = sanitizeAIDiagnosticsJSONText(run.ProviderFailoverJSON, false)
	run.ModelRouteJSON = sanitizeAIDiagnosticsJSONText(run.ModelRouteJSON, false)
	run.EstimatedCostJSON = sanitizeAIDiagnosticsJSONText(run.EstimatedCostJSON, false)
	return run
}

func sanitizeAIDiagnosticsSession(session AISession) AISession {
	session.Title = sanitizeAIDiagnosticsSummaryString(session.Title)
	session.ScopeJSON = sanitizeAIDiagnosticsJSONText(session.ScopeJSON, false)
	return session
}

func sanitizeAIDiagnosticsMessage(message AIMessage) AIMessage {
	message.Content = sanitizeAIDiagnosticsSummaryString(message.Content)
	message.ModelRouteJSON = sanitizeAIDiagnosticsJSONText(message.ModelRouteJSON, false)
	message.ErrorMessage = sanitizeProviderError(message.ErrorMessage)
	return message
}

func sanitizeAIDiagnosticsJSONText(raw string, checkpoint bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		return sanitizeAIDiagnosticsSummaryString(raw)
	}
	if checkpoint {
		if fields, ok := payload.(map[string]any); ok {
			if evidenceContract, exists := fields["evidence_contract"]; exists {
				fields["evidence_contract"] = summarizeAIDiagnosticsEvidenceContractArtifact(evidenceContract)
			}
		}
	}
	return encodeJSON(sanitizeAIDiagnosticsSummaryValue(payload, 0))
}

func sanitizeAIDiagnosticsStepSummary(step AIAgentStep) map[string]any {
	if step.AgentName == "evidence_curator" {
		if summary := sanitizeAIDiagnosticsEvidenceCuratorSummary(step.OutputJSON); len(summary) > 0 {
			return summary
		}
	}
	if step.AgentName == "contract_checker" {
		if summary := sanitizeAIDiagnosticsContractCheckerSummary(step.OutputJSON); len(summary) > 0 {
			return summary
		}
	}
	if step.AgentName == "answer_verifier" {
		if summary := sanitizeAIDiagnosticsAnswerVerifierSummary(step.OutputJSON); len(summary) > 0 {
			return summary
		}
	}

	summary := map[string]any{}
	if inputSummary, ok := extractAIDiagnosticsExplicitSummary(step.InputJSON); ok {
		summary["input"] = inputSummary
	}
	if outputSummary, ok := extractAIDiagnosticsExplicitSummary(step.OutputJSON); ok {
		summary["output"] = outputSummary
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}

func sanitizeAIDiagnosticsStepPayload(raw string) any {
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		if sanitized := sanitizeAIDiagnosticsSummaryString(raw); sanitized != "" {
			return sanitized
		}
		return nil
	}
	return sanitizeAIDiagnosticsSummaryValue(payload, 0)
}

func buildAIDiagnosticsAgentWorkflow(run AIAgentRun, steps []AIAgentStep, taskFrame, evidenceContract, contractCoverage any) any {
	workflow := map[string]any{}
	if fields, ok := decodeAIDiagnosticsSummaryJSON(run.CheckpointJSON); ok {
		if checkpoint, ok := fields.(map[string]any); ok {
			for _, key := range []string{"agent_workflow_version", "agent_workflow_mode", "answer_mode", "task_class"} {
				if value, exists := checkpoint[key]; exists {
					workflow[key] = sanitizeAIDiagnosticsArtifact(value)
				}
			}
			if value, exists := checkpoint["evidence_bundle"]; exists {
				workflow["evidence_bundle"] = sanitizeAIDiagnosticsArtifact(summarizeAIDiagnosticsEvidenceBundleArtifact(value))
			}
		}
	}
	if _, exists := workflow["agent_workflow_version"]; !exists {
		workflow["agent_workflow_version"] = aiAgentWorkflowVersionV2Shadow
	}
	if aiDiagnosticsArtifactPresent(taskFrame) {
		workflow["task_frame"] = taskFrame
	}
	if aiDiagnosticsArtifactPresent(evidenceContract) {
		workflow["evidence_contract"] = evidenceContract
	}
	if aiDiagnosticsArtifactPresent(contractCoverage) {
		workflow["contract_coverage"] = contractCoverage
	}
	if rounds := extractAIDiagnosticsRetrievalRounds(run, steps); len(rounds) > 0 {
		workflow["retrieval_rounds"] = rounds
	}
	if _, exists := workflow["evidence_bundle"]; !exists {
		if bundle := extractAIDiagnosticsEvidenceBundle(steps); aiDiagnosticsArtifactPresent(bundle) {
			workflow["evidence_bundle"] = bundle
		}
	}
	if report := extractAIDiagnosticsVerificationReport(run, steps); aiDiagnosticsArtifactPresent(report) {
		workflow["verification_report"] = report
	}
	if len(workflow) == 0 {
		return nil
	}
	return sanitizeAIDiagnosticsArtifact(workflow)
}

func extractAIDiagnosticsRunArtifacts(run AIAgentRun, steps []AIAgentStep) (any, any, any) {
	var taskFrame any
	var evidenceContract any
	var contractCoverage any
	if fields, ok := decodeAIDiagnosticsSummaryJSON(run.CheckpointJSON); ok {
		if checkpoint, ok := fields.(map[string]any); ok {
			taskFrame = checkpoint["task_frame"]
			evidenceContract = checkpoint["evidence_contract"]
			contractCoverage = checkpoint["contract_coverage"]
		}
	}
	for _, step := range steps {
		if !aiDiagnosticsArtifactPresent(taskFrame) && step.AgentName == "task_framer" {
			if value, ok := decodeAIDiagnosticsSummaryJSON(step.OutputJSON); ok {
				taskFrame = value
			}
		}
		if !aiDiagnosticsArtifactPresent(evidenceContract) && step.AgentName == "contract_builder" {
			if value, ok := decodeAIDiagnosticsSummaryJSON(step.OutputJSON); ok {
				evidenceContract = value
			}
		}
		if !aiDiagnosticsArtifactPresent(contractCoverage) && step.AgentName == "contract_checker" {
			if value, ok := extractAIDiagnosticsCoverageFromChecker(step.OutputJSON); ok {
				contractCoverage = value
			}
		}
	}
	return sanitizeAIDiagnosticsArtifact(taskFrame), sanitizeAIDiagnosticsArtifact(summarizeAIDiagnosticsEvidenceContractArtifact(evidenceContract)), sanitizeAIDiagnosticsArtifact(contractCoverage)
}

func aiDiagnosticsArtifactPresent(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return true
	}
}

func sanitizeAIDiagnosticsArtifact(value any) any {
	if !aiDiagnosticsArtifactPresent(value) {
		return nil
	}
	return sanitizeAIDiagnosticsSummaryValue(value, 0)
}

func summarizeAIDiagnosticsEvidenceContractArtifact(value any) any {
	fields, ok := value.(map[string]any)
	if !ok {
		return value
	}
	summary := map[string]any{}
	for _, key := range []string{"contract_id", "intent"} {
		if nested, exists := fields[key]; exists {
			summary[key] = nested
		}
	}
	for _, key := range []string{"required_keys", "recommended_keys"} {
		if nested, exists := fields[key]; exists {
			summary[key] = nested
		}
	}
	if keys := aiDiagnosticsRequirementKeysFromArtifact(fields["required"]); len(keys) > 0 {
		summary["required_keys"] = keys
	}
	if keys := aiDiagnosticsRequirementKeysFromArtifact(fields["recommended"]); len(keys) > 0 {
		summary["recommended_keys"] = keys
	}
	if len(summary) == 0 {
		return value
	}
	return summary
}

func aiDiagnosticsRequirementKeysFromArtifact(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	keys := []string{}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, ok := fields["key"].(string)
		if !ok {
			continue
		}
		if key = strings.TrimSpace(key); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func sanitizeAIDiagnosticsContractCheckerSummary(raw string) map[string]any {
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		return nil
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	summary := map[string]any{}
	for _, key := range []string{"contract_coverage", "coverage", "summary"} {
		if value, exists := fields[key]; exists {
			summary[key] = sanitizeAIDiagnosticsSummaryValue(value, 0)
		}
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}

func extractAIDiagnosticsCoverageFromChecker(raw string) (any, bool) {
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		return nil, false
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, key := range []string{"contract_coverage", "coverage"} {
		if value, exists := fields[key]; exists {
			return value, true
		}
	}
	return nil, false
}

func sanitizeAIDiagnosticsEvidenceCuratorSummary(raw string) map[string]any {
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		return nil
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	summary := map[string]any{}
	for _, key := range []string{"evidence_bundle", "coverage", "annotations", "included_count", "excluded_count"} {
		if value, exists := fields[key]; exists {
			summary[key] = sanitizeAIDiagnosticsSummaryValue(value, 0)
		}
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}

func sanitizeAIDiagnosticsAnswerVerifierSummary(raw string) map[string]any {
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		return nil
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	summary := map[string]any{}
	for _, key := range []string{"verification_report", "report", "summary"} {
		if value, exists := fields[key]; exists {
			summary[key] = sanitizeAIDiagnosticsSummaryValue(value, 0)
		}
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}

func extractAIDiagnosticsRetrievalRounds(run AIAgentRun, steps []AIAgentStep) []any {
	rounds := []any{}
	for _, step := range steps {
		if step.StepType != "retrieval_round" {
			continue
		}
		if summary, ok := extractAIDiagnosticsExplicitSummary(step.OutputJSON); ok {
			rounds = append(rounds, summary)
		}
	}
	if len(rounds) > 0 {
		return rounds
	}
	payload, ok := decodeAIDiagnosticsSummaryJSON(run.RetrievalPlanJSON)
	if !ok {
		return nil
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if value, exists := fields["retrieval_rounds"]; exists {
		if sanitized, ok := sanitizeAIDiagnosticsSummaryValue(value, 0).([]any); ok {
			return sanitized
		}
	}
	return nil
}

func extractAIDiagnosticsEvidenceBundle(steps []AIAgentStep) any {
	for _, step := range steps {
		if step.AgentName != "evidence_curator" {
			continue
		}
		summary := sanitizeAIDiagnosticsEvidenceCuratorSummary(step.OutputJSON)
		if bundle, exists := summary["evidence_bundle"]; exists {
			return summarizeAIDiagnosticsEvidenceBundleArtifact(bundle)
		}
	}
	return nil
}

func extractAIDiagnosticsVerificationReport(run AIAgentRun, steps []AIAgentStep) any {
	for _, step := range steps {
		if step.AgentName != "answer_verifier" {
			continue
		}
		summary := sanitizeAIDiagnosticsAnswerVerifierSummary(step.OutputJSON)
		if report, exists := summary["verification_report"]; exists {
			return report
		}
	}
	payload, ok := decodeAIDiagnosticsSummaryJSON(run.VerificationReportJSON)
	if !ok {
		return nil
	}
	return sanitizeAIDiagnosticsSummaryValue(payload, 0)
}

func summarizeAIDiagnosticsEvidenceBundleArtifact(value any) any {
	fields, ok := value.(map[string]any)
	if !ok {
		return value
	}
	summary := map[string]any{}
	for _, key := range []string{"bundle_id", "coverage", "group_count", "excluded_count"} {
		if nested, exists := fields[key]; exists {
			summary[key] = nested
		}
	}
	if groups, ok := fields["groups"].([]any); ok {
		summary["group_count"] = len(groups)
	}
	if excluded, ok := fields["excluded"].([]any); ok {
		summary["excluded_count"] = len(excluded)
	}
	if len(summary) == 0 {
		return value
	}
	return summary
}

func extractAIDiagnosticsExplicitSummary(raw string) (any, bool) {
	payload, ok := decodeAIDiagnosticsSummaryJSON(raw)
	if !ok {
		return nil, false
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, key := range []string{
		"input_summary_json",
		"output_summary_json",
		"summary_json",
		"payload_summary_json",
		"input_summary",
		"output_summary",
		"summary",
		"payload_summary",
	} {
		if value, exists := fields[key]; exists {
			return sanitizeAIDiagnosticsSummaryValue(decodeAIDiagnosticsNestedSummary(value), 0), true
		}
	}
	return nil, false
}

func decodeAIDiagnosticsNestedSummary(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}
	decoded, ok := decodeAIDiagnosticsSummaryJSON(trimmed)
	if !ok {
		return value
	}
	return decoded
}

func decodeAIDiagnosticsSummaryJSON(raw string) (any, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	return payload, true
}

func sanitizeAIDiagnosticsSummaryValue(value any, depth int) any {
	if depth > 8 {
		return "[truncated]"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, nested := range typed {
			if aiDiagnosticsSummaryKeySensitive(key) {
				continue
			}
			out[key] = sanitizeAIDiagnosticsSummaryValue(nested, depth+1)
		}
		return out
	case []any:
		limit := len(typed)
		if limit > 50 {
			limit = 50
		}
		out := make([]any, 0, limit+1)
		for i := 0; i < limit; i++ {
			out = append(out, sanitizeAIDiagnosticsSummaryValue(typed[i], depth+1))
		}
		if len(typed) > limit {
			out = append(out, map[string]any{"truncated_count": len(typed) - limit})
		}
		return out
	case string:
		return sanitizeAIDiagnosticsSummaryString(typed)
	default:
		return typed
	}
}

func aiDiagnosticsSummaryKeySensitive(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	if strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "api_token") ||
		strings.Contains(normalized, "apitoken") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "bearer") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential") {
		return true
	}
	switch normalized {
	case "token", "access_token", "accesstoken", "refresh_token", "refreshtoken", "id_token", "idtoken", "auth_token", "authtoken":
		return true
	default:
		return strings.HasSuffix(normalized, "_token")
	}
}

func sanitizeAIDiagnosticsSummaryString(value string) string {
	value = strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
	for _, pattern := range []*regexp.Regexp{
		diagnosticsSummaryAuthHeaderPattern,
		diagnosticsSummaryBearerPattern,
		diagnosticsSummarySensitiveKVPattern,
		diagnosticsSummaryAPIKeyWordPattern,
		diagnosticsSummarySecretPhrasePattern,
		diagnosticsSummaryJWTPattern,
		secretTokenPattern,
	} {
		value = pattern.ReplaceAllString(value, "[redacted]")
	}
	return truncate(value, 500)
}

func aiRunDurationMS(run AIAgentRun) int64 {
	started, err := time.Parse(timeLayout, run.StartedAt)
	if err != nil {
		return 0
	}
	finishedAt := run.FinishedAt
	if finishedAt == "" {
		return 0
	}
	finished, err := time.Parse(timeLayout, finishedAt)
	if err != nil || finished.Before(started) {
		return 0
	}
	return finished.Sub(started).Milliseconds()
}

func createAISession(ctx context.Context, db *sql.DB, title, viewer string, scope AIQuestionScope) (AISession, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新的 AI 问答"
	}
	scope = normalizeAIScope(scope)
	now := nowString()
	res, err := db.ExecContext(ctx, `INSERT INTO ai_sessions (title, viewer_key, scope_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, title, viewer, encodeJSON(scope), now, now)
	if err != nil {
		return AISession{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AISession{}, err
	}
	return getAISession(ctx, db, id)
}

func getAIHistorySession(ctx context.Context, db *sql.DB, id int64, viewer string) (AISession, error) {
	query := `SELECT id, title, viewer_key, scope_json, created_at, updated_at, archived_at
		FROM ai_sessions WHERE id = ?`
	args := []any{id}
	if viewer = strings.TrimSpace(viewer); viewer != "" {
		query += ` AND viewer_key = ?`
		args = append(args, viewer)
	}
	row := db.QueryRowContext(ctx, query, args...)
	session, err := scanAISession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AISession{}, errNotFound("ai session not found")
		}
		return AISession{}, err
	}
	return session, nil
}

func getAIHistorySessionDetail(ctx context.Context, db *sql.DB, sessionID int64, viewer string) (aiHistorySessionDetailResponse, error) {
	session, err := getAIHistorySession(ctx, db, sessionID, viewer)
	if err != nil {
		return aiHistorySessionDetailResponse{}, err
	}
	messages, err := listAIMessages(ctx, db, session.ID)
	if err != nil {
		return aiHistorySessionDetailResponse{}, err
	}
	candidates, err := listAIServiceCandidatesForSession(ctx, db, session.ID)
	if err != nil {
		return aiHistorySessionDetailResponse{}, err
	}
	citations, err := listAICitationsForSession(ctx, db, session.ID)
	if err != nil {
		return aiHistorySessionDetailResponse{}, err
	}
	return aiHistorySessionDetailResponse{
		Session:           session,
		Messages:          messages,
		ServiceCandidates: candidates,
		Citations:         citations,
	}, nil
}

func getAISession(ctx context.Context, db *sql.DB, id int64) (AISession, error) {
	row := db.QueryRowContext(ctx, `SELECT id, title, viewer_key, scope_json, created_at, updated_at, archived_at
		FROM ai_sessions WHERE id = ?`, id)
	session, err := scanAISession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AISession{}, errNotFound("ai session not found")
		}
		return AISession{}, err
	}
	return session, nil
}

func scanAISession(row repoScanner) (AISession, error) {
	var session AISession
	if err := row.Scan(&session.ID, &session.Title, &session.ViewerKey, &session.ScopeJSON, &session.CreatedAt,
		&session.UpdatedAt, &session.ArchivedAt); err != nil {
		return AISession{}, err
	}
	return session, nil
}

func updateAISession(ctx context.Context, db *sql.DB, id int64, req aiSessionPatchRequest) (AISession, error) {
	current, err := getAISession(ctx, db, id)
	if err != nil {
		return AISession{}, err
	}
	if strings.TrimSpace(req.Title) != "" {
		current.Title = strings.TrimSpace(req.Title)
	}
	if req.Scope.RepoMode != "" || len(req.Scope.RepoIDs) > 0 || req.Scope.SourceMode != "" || req.Scope.CurrentFile != nil {
		current.ScopeJSON = encodeJSON(normalizeAIScope(req.Scope))
	}
	if req.Archived != nil {
		if *req.Archived {
			current.ArchivedAt = nowString()
		} else {
			current.ArchivedAt = ""
		}
	}
	res, err := db.ExecContext(ctx, `UPDATE ai_sessions SET title = ?, scope_json = ?, archived_at = ?, updated_at = ?
		WHERE id = ?`, current.Title, current.ScopeJSON, current.ArchivedAt, nowString(), id)
	if err != nil {
		return AISession{}, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return AISession{}, errNotFound("ai session not found")
	}
	return getAISession(ctx, db, id)
}

func listAIMessages(ctx context.Context, db *sql.DB, sessionID int64) ([]AIMessage, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, session_id, role, content, model, provider_name, model_route_json,
		prompt_tokens, completion_tokens, latency_ms, status, error_message, created_at
		FROM ai_messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []AIMessage{}
	for rows.Next() {
		msg, err := scanAIMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func insertAIMessage(ctx context.Context, db execer, msg AIMessage) (AIMessage, error) {
	if msg.CreatedAt == "" {
		msg.CreatedAt = nowString()
	}
	if msg.Status == "" {
		msg.Status = "success"
	}
	res, err := db.ExecContext(ctx, `INSERT INTO ai_messages
		(session_id, role, content, model, provider_name, model_route_json, prompt_tokens, completion_tokens,
		 latency_ms, status, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.SessionID, msg.Role, msg.Content, msg.Model, msg.ProviderName, msg.ModelRouteJSON, msg.PromptTokens,
		msg.CompletionTokens, msg.LatencyMS, msg.Status, msg.ErrorMessage, msg.CreatedAt)
	if err != nil {
		return AIMessage{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AIMessage{}, err
	}
	msg.ID = id
	return msg, nil
}

func updateAIMessage(ctx context.Context, db execer, msg AIMessage) error {
	if msg.Status == "" {
		msg.Status = "success"
	}
	_, err := db.ExecContext(ctx, `UPDATE ai_messages SET content = ?, model = ?, provider_name = ?,
		model_route_json = ?, prompt_tokens = ?, completion_tokens = ?, latency_ms = ?, status = ?, error_message = ?
		WHERE id = ?`,
		msg.Content, msg.Model, msg.ProviderName, msg.ModelRouteJSON, msg.PromptTokens, msg.CompletionTokens,
		msg.LatencyMS, msg.Status, msg.ErrorMessage, msg.ID)
	return err
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func scanAIMessage(row repoScanner) (AIMessage, error) {
	var msg AIMessage
	if err := row.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &msg.Model, &msg.ProviderName,
		&msg.ModelRouteJSON, &msg.PromptTokens, &msg.CompletionTokens, &msg.LatencyMS, &msg.Status,
		&msg.ErrorMessage, &msg.CreatedAt); err != nil {
		return AIMessage{}, err
	}
	return msg, nil
}

func (s *Server) askAIQuestion(ctx context.Context, sessionID int64, question string, scope AIQuestionScope, viewer string) (aiQuestionResult, error) {
	if question == "" {
		return aiQuestionResult{}, errBadRequest("question is required")
	}
	session, err := getAISession(ctx, s.db, sessionID)
	if err != nil {
		return aiQuestionResult{}, err
	}
	if scope.RepoMode == "" && session.ScopeJSON != "" {
		_ = json.Unmarshal([]byte(session.ScopeJSON), &scope)
	}
	scope = normalizeAIScope(scope)
	if scope.CurrentFile != nil && scope.CurrentFile.RepoID > 0 && len(scope.RepoIDs) == 0 {
		scope.RepoIDs = []int64{scope.CurrentFile.RepoID}
		if scope.RepoMode == "" || scope.RepoMode == "global" {
			scope.RepoMode = "current_file"
		}
	}
	prepared, err := s.prepareAIQuestion(ctx, sessionID, question, scope)
	if err != nil {
		return aiQuestionResult{}, err
	}
	scope = prepared.Scope
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return aiQuestionResult{}, err
	}
	start := time.Now()
	userMsg, err := insertAIMessage(ctx, s.db, AIMessage{SessionID: sessionID, Role: "user", Content: question})
	if err != nil {
		return aiQuestionResult{}, err
	}
	run, err := createAIRun(ctx, s.db, sessionID, userMsg.ID, active, scope)
	if err != nil {
		return aiQuestionResult{}, err
	}
	prepared, plannerStep := s.expandAIQuestionForRetrieval(ctx, active.Config, prepared)
	plannerStep.RunID = run.ID
	_ = insertAIStep(ctx, s.db, plannerStep)
	taskFrame, taskFrameStep := s.frameAITask(ctx, active.Config, question, prepared)
	prepared.TaskFrame = &taskFrame
	taskFrameStep.RunID = run.ID
	_ = insertAIStep(ctx, s.db, taskFrameStep)
	contract := buildAIEvidenceContract(taskFrame)
	prepared.Contract = &contract
	contractStep := buildAIEvidenceContractStep(taskFrame, contract)
	contractStep.RunID = run.ID
	_ = insertAIStep(ctx, s.db, contractStep)
	checkpoint := buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
	checkpointJSON := encodeJSON(checkpoint)
	_ = insertAIStep(ctx, s.db, AIAgentStep{
		RunID:      run.ID,
		AgentName:  "coordinator",
		StepType:   "checkpoint",
		Status:     "success",
		InputJSON:  encodeJSON(map[string]any{"question": truncate(question, 500), "viewer": viewer}),
		OutputJSON: checkpointJSON,
		CreatedAt:  run.StartedAt,
		FinishedAt: nowString(),
	})

	retrieval, err := s.retrieveAIEvidenceWithTaskFrame(ctx, prepared.SearchQuestion, scope, active.Config, prepared.TaskFrame, prepared.Contract)
	if err != nil {
		_ = finishAIRun(ctx, s.db, run.ID, AIAgentRun{
			Status:         "failed",
			CurrentState:   "retrieve_smart_latest",
			FinishedAt:     nowString(),
			ErrorMessage:   err.Error(),
			CheckpointJSON: checkpointJSON,
		})
		return aiQuestionResult{}, err
	}
	applyAIConversationContext(&retrieval, prepared)
	prepared.EvidenceBundle = retrieval.EvidenceBundle
	prepared.Coverage = retrieval.Coverage
	prepared.ContractCoverage = retrieval.ContractCoverage
	checkpoint = buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
	checkpointJSON = encodeJSON(checkpoint)
	for _, roundStep := range retrieval.RetrievalRoundSteps {
		roundStep.RunID = run.ID
		_ = insertAIStep(ctx, s.db, roundStep)
	}
	_ = insertAIStep(ctx, s.db, AIAgentStep{
		RunID:      run.ID,
		AgentName:  "retrieval",
		StepType:   "tool_call",
		Status:     "success",
		ToolName:   "search_code_evidence",
		InputJSON:  encodeJSON(map[string]any{"source_mode": retrieval.Scope.SourceMode, "repo_ids": retrieval.Scope.RepoIDs}),
		OutputJSON: encodeJSON(map[string]any{"evidence_count": len(retrieval.Evidence), "service_candidate_count": len(retrieval.ServiceCandidates), "excluded_evidence_count": len(retrieval.EvidenceBundle.Excluded)}),
		CreatedAt:  nowString(),
		FinishedAt: nowString(),
	})
	if retrieval.Curation != nil {
		curatorStep := buildAIEvidenceCuratorStep(retrieval.TaskFrame, retrieval.Contract, *retrieval.Curation, len(retrieval.Curation.Annotations))
		curatorStep.RunID = run.ID
		_ = insertAIStep(ctx, s.db, curatorStep)
	}
	var contractCoverage *aiContractCoverageReport
	if retrieval.Contract != nil && retrieval.EvidenceBundle != nil {
		coverage := aiRetrievalContractCoverage(retrieval)
		contractCoverage = &coverage
		prepared.ContractCoverage = contractCoverage
		retrieval.Plan["contract_coverage"] = summarizeAIContractCoverageReport(coverage)
		checkerStep := buildAIContractCheckerStep(retrieval.Contract, retrieval.EvidenceBundle, coverage)
		checkerStep.RunID = run.ID
		_ = insertAIStep(ctx, s.db, checkerStep)
		checkpoint = buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
		checkpointJSON = encodeJSON(checkpoint)
	}
	composer := prepareAIAnswerComposer(question, &retrieval)
	prepared.AnswerPolicy = &composer.Policy
	prepared.AnswerComposer = &composer.Summary
	policyStep := buildAIAnswerPolicyStep(&composer.Frame, &composer.Contract, &composer.Coverage, composer.Policy)
	policyStep.RunID = run.ID
	_ = insertAIStep(ctx, s.db, policyStep)
	composerStep := buildAIAnswerComposerStep(question, retrieval, composer.Policy, composer.Summary)
	composerStep.RunID = run.ID
	_ = insertAIStep(ctx, s.db, composerStep)
	checkpoint = buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
	checkpointJSON = encodeJSON(checkpoint)

	var modelResult aiModelResult
	if len(retrieval.Evidence) == 0 {
		modelResult = aiModelResult{
			Content:      localNoEvidenceAnswer(question),
			ProviderName: "local-retrieval",
			Model:        "none",
			ModelRouteJSON: encodeJSON(map[string]any{
				"mode":   "local",
				"reason": "no_evidence",
			}),
			LatencyMS: int(time.Since(start).Milliseconds()),
		}
	} else {
		modelResult = s.generateAIAnswer(ctx, active, question, retrieval)
	}
	verificationOutcome := s.verifyAndMaybeRewriteAIAnswer(ctx, active, question, retrieval, modelResult, start)
	modelResult = verificationOutcome.Result
	verifierFrame, verifierContract, verifierCoverage, verifierBundle := aiAnswerVerifierInputs(retrieval)
	verifierStep := buildAIAnswerVerifierStep(verifierFrame, verifierContract, verifierCoverage, verifierBundle, verificationOutcome.Report, modelResult.Content)
	verifierStep.RunID = run.ID
	_ = insertAIStep(ctx, s.db, verifierStep)
	notice := aiNoticeFromModelResult(modelResult)
	assistantMsg, err := insertAIMessage(ctx, s.db, AIMessage{
		SessionID:        sessionID,
		Role:             "assistant",
		Content:          modelResult.Content,
		Model:            modelResult.Model,
		ProviderName:     modelResult.ProviderName,
		ModelRouteJSON:   modelResult.ModelRouteJSON,
		PromptTokens:     modelResult.PromptTokens,
		CompletionTokens: modelResult.CompletionTokens,
		LatencyMS:        modelResult.LatencyMS,
		Status:           "success",
	})
	if err != nil {
		return aiQuestionResult{}, err
	}
	citations := make([]AIMessageCitation, 0, len(retrieval.Evidence))
	for _, evidence := range retrieval.Evidence {
		citation := evidence.Citation
		citation.MessageID = assistantMsg.ID
		inserted, err := insertAICitation(ctx, s.db, citation)
		if err != nil {
			return aiQuestionResult{}, err
		}
		citations = append(citations, inserted)
	}
	candidates := make([]AIServiceCandidate, 0, len(retrieval.ServiceCandidates))
	for _, candidate := range retrieval.ServiceCandidates {
		candidate.RunID = run.ID
		candidate.MessageID = assistantMsg.ID
		inserted, err := insertAIServiceCandidate(ctx, s.db, candidate)
		if err != nil {
			return aiQuestionResult{}, err
		}
		candidates = append(candidates, inserted)
	}
	if err := finishAIRun(ctx, s.db, run.ID, AIAgentRun{
		AssistantMessageID:     assistantMsg.ID,
		Status:                 verificationOutcome.RunStatus,
		CurrentState:           "verify_answer",
		Intent:                 retrieval.Intent,
		ScopeJSON:              encodeJSON(retrieval.Scope),
		RetrievalPlanJSON:      encodeJSON(retrieval.Plan),
		ServiceCandidateCount:  len(candidates),
		EvidenceCount:          len(citations),
		CodeEvidenceCount:      countCodeEvidence(citations),
		UnconfirmedCount:       aiContractCoverageUnconfirmedRequiredCount(contractCoverage),
		VerificationStatus:     verificationOutcome.VerificationStatus,
		VerificationReportJSON: encodeJSON(verificationOutcome.Report),
		CheckpointJSON:         checkpointJSON,
		Model:                  modelResult.Model,
		ProviderName:           modelResult.ProviderName,
		ProviderFailoverJSON:   modelResult.FailoverJSON,
		ModelRouteJSON:         modelResult.ModelRouteJSON,
		FinishedAt:             nowString(),
	}); err != nil {
		return aiQuestionResult{}, err
	}
	run, _ = getAIRun(ctx, s.db, run.ID)
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_sessions SET title = CASE WHEN title = '新的 AI 问答' THEN ? ELSE title END,
		updated_at = ? WHERE id = ?`, aiTitleFromQuestion(question), nowString(), sessionID)
	return aiQuestionResult{Run: run, Message: assistantMsg, ServiceCandidates: candidates, Citations: citations, Notice: notice}, nil
}

func (s *Server) askAIQuestionStream(ctx context.Context, sessionID int64, question string, scope AIQuestionScope, viewer string, emit aiStreamEmitFunc) (err error) {
	if question == "" {
		return errBadRequest("question is required")
	}
	session, err := getAISession(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if scope.RepoMode == "" && session.ScopeJSON != "" {
		_ = json.Unmarshal([]byte(session.ScopeJSON), &scope)
	}
	scope = normalizeAIScope(scope)
	if scope.CurrentFile != nil && scope.CurrentFile.RepoID > 0 && len(scope.RepoIDs) == 0 {
		scope.RepoIDs = []int64{scope.CurrentFile.RepoID}
		if scope.RepoMode == "" || scope.RepoMode == "global" {
			scope.RepoMode = "current_file"
		}
	}
	prepared, err := s.prepareAIQuestion(ctx, sessionID, question, scope)
	if err != nil {
		return err
	}
	scope = prepared.Scope
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return err
	}
	start := time.Now()
	userMsg, err := insertAIMessage(ctx, s.db, AIMessage{SessionID: sessionID, Role: "user", Content: question})
	if err != nil {
		return err
	}
	if err := emit("user_message", map[string]any{"message": userMsg}); err != nil {
		return err
	}
	run, err := createAIRun(ctx, s.db, sessionID, userMsg.ID, active, scope)
	if err != nil {
		return err
	}
	checkpointJSON := run.CheckpointJSON
	assistantMsg, err := insertAIMessage(ctx, s.db, AIMessage{SessionID: sessionID, Role: "assistant", Status: "streaming"})
	if err != nil {
		return err
	}
	currentState := "queued"
	completed := false
	defer func() {
		if completed {
			return
		}
		message := "stream interrupted"
		if err != nil {
			message = sanitizeProviderError(err.Error())
		}
		status := "failed"
		if strings.TrimSpace(assistantMsg.Content) != "" {
			status = "partial"
		}
		persistCtx, cancel := aiPersistenceContext(ctx)
		defer cancel()
		assistantMsg.Status = status
		assistantMsg.ErrorMessage = message
		_ = updateAIMessage(persistCtx, s.db, assistantMsg)
		_ = finishAIRun(persistCtx, s.db, run.ID, AIAgentRun{
			AssistantMessageID:   assistantMsg.ID,
			Status:               "failed",
			CurrentState:         currentState,
			ScopeJSON:            encodeJSON(scope),
			Model:                assistantMsg.Model,
			ProviderName:         assistantMsg.ProviderName,
			ProviderFailoverJSON: run.ProviderFailoverJSON,
			ModelRouteJSON:       assistantMsg.ModelRouteJSON,
			CheckpointJSON:       checkpointJSON,
			FinishedAt:           nowString(),
			ErrorMessage:         message,
		})
	}()
	prepared, plannerStep := s.expandAIQuestionForRetrieval(ctx, active.Config, prepared)
	plannerStep.RunID = run.ID
	if err := insertAIStep(ctx, s.db, plannerStep); err != nil {
		return err
	}
	taskFrame, taskFrameStep := s.frameAITask(ctx, active.Config, question, prepared)
	prepared.TaskFrame = &taskFrame
	taskFrameStep.RunID = run.ID
	if err := insertAIStep(ctx, s.db, taskFrameStep); err != nil {
		return err
	}
	contract := buildAIEvidenceContract(taskFrame)
	prepared.Contract = &contract
	contractStep := buildAIEvidenceContractStep(taskFrame, contract)
	contractStep.RunID = run.ID
	if err := insertAIStep(ctx, s.db, contractStep); err != nil {
		return err
	}
	checkpoint := buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
	checkpointJSON = encodeJSON(checkpoint)
	run.CheckpointJSON = checkpointJSON
	if err := insertAIStep(ctx, s.db, AIAgentStep{
		RunID:      run.ID,
		AgentName:  "coordinator",
		StepType:   "checkpoint",
		Status:     "success",
		InputJSON:  encodeJSON(map[string]any{"question": truncate(question, 500), "viewer": viewer}),
		OutputJSON: checkpointJSON,
		CreatedAt:  run.StartedAt,
		FinishedAt: nowString(),
	}); err != nil {
		return err
	}
	if err := emit("run_started", map[string]any{"run": run, "assistant_message": assistantMsg}); err != nil {
		return err
	}
	if err := emit("task_frame", map[string]any{"task_frame": taskFrame}); err != nil {
		return err
	}
	if err := emit("contract", map[string]any{"contract": summarizeAIEvidenceContract(contract)}); err != nil {
		return err
	}

	currentState = "retrieve_smart_latest"
	if err := emit("stage", aiStreamStageEvent{Stage: currentState, Status: "running", Message: "检索候选服务和引用证据"}); err != nil {
		return err
	}
	retrieval, err := s.retrieveAIEvidenceWithTaskFrame(ctx, prepared.SearchQuestion, scope, active.Config, prepared.TaskFrame, prepared.Contract)
	if err != nil {
		safeErr := sanitizeProviderError(err.Error())
		assistantMsg.Status = "failed"
		assistantMsg.ErrorMessage = safeErr
		persistCtx, cancel := aiPersistenceContext(ctx)
		_ = updateAIMessage(persistCtx, s.db, assistantMsg)
		_ = finishAIRun(persistCtx, s.db, run.ID, AIAgentRun{
			AssistantMessageID: assistantMsg.ID,
			Status:             "failed",
			CurrentState:       currentState,
			ScopeJSON:          encodeJSON(scope),
			CheckpointJSON:     checkpointJSON,
			FinishedAt:         nowString(),
			ErrorMessage:       safeErr,
		})
		cancel()
		completed = true
		_ = emit("error", map[string]any{"message": safeErr, "partial_message_id": assistantMsg.ID, "assistant_message": assistantMsg})
		return nil
	}
	applyAIConversationContext(&retrieval, prepared)
	prepared.EvidenceBundle = retrieval.EvidenceBundle
	prepared.Coverage = retrieval.Coverage
	prepared.ContractCoverage = retrieval.ContractCoverage
	checkpoint = buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
	checkpointJSON = encodeJSON(checkpoint)
	run.CheckpointJSON = checkpointJSON
	for _, roundStep := range retrieval.RetrievalRoundSteps {
		roundStep.RunID = run.ID
		if err := insertAIStep(ctx, s.db, roundStep); err != nil {
			return err
		}
	}
	for _, round := range retrieval.Rounds {
		if err := emit("retrieval_round", map[string]any{"round": round}); err != nil {
			return err
		}
	}
	if err := insertAIStep(ctx, s.db, AIAgentStep{
		RunID:      run.ID,
		AgentName:  "retrieval",
		StepType:   "tool_call",
		Status:     "success",
		ToolName:   "search_code_evidence",
		InputJSON:  encodeJSON(map[string]any{"source_mode": retrieval.Scope.SourceMode, "repo_ids": retrieval.Scope.RepoIDs}),
		OutputJSON: encodeJSON(map[string]any{"evidence_count": len(retrieval.Evidence), "service_candidate_count": len(retrieval.ServiceCandidates), "excluded_evidence_count": len(retrieval.EvidenceBundle.Excluded)}),
		CreatedAt:  nowString(),
		FinishedAt: nowString(),
	}); err != nil {
		return err
	}
	if retrieval.Curation != nil {
		curatorStep := buildAIEvidenceCuratorStep(retrieval.TaskFrame, retrieval.Contract, *retrieval.Curation, len(retrieval.Curation.Annotations))
		curatorStep.RunID = run.ID
		if err := insertAIStep(ctx, s.db, curatorStep); err != nil {
			return err
		}
	}
	var contractCoverage *aiContractCoverageReport
	if retrieval.Contract != nil && retrieval.EvidenceBundle != nil {
		coverage := aiRetrievalContractCoverage(retrieval)
		contractCoverage = &coverage
		prepared.ContractCoverage = contractCoverage
		retrieval.Plan["contract_coverage"] = summarizeAIContractCoverageReport(coverage)
		checkerStep := buildAIContractCheckerStep(retrieval.Contract, retrieval.EvidenceBundle, coverage)
		checkerStep.RunID = run.ID
		if err := insertAIStep(ctx, s.db, checkerStep); err != nil {
			return err
		}
		checkpoint = buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
		checkpointJSON = encodeJSON(checkpoint)
		run.CheckpointJSON = checkpointJSON
	}
	if contractCoverage != nil {
		if err := emit("coverage", map[string]any{
			"coverage":        summarizeAIContractCoverageReport(*contractCoverage),
			"evidence_bundle": summarizeAIRetrievalBundle(retrieval.EvidenceBundle),
		}); err != nil {
			return err
		}
	}
	composer := prepareAIAnswerComposer(question, &retrieval)
	prepared.AnswerPolicy = &composer.Policy
	prepared.AnswerComposer = &composer.Summary
	policyStep := buildAIAnswerPolicyStep(&composer.Frame, &composer.Contract, &composer.Coverage, composer.Policy)
	policyStep.RunID = run.ID
	if err := insertAIStep(ctx, s.db, policyStep); err != nil {
		return err
	}
	composerStep := buildAIAnswerComposerStep(question, retrieval, composer.Policy, composer.Summary)
	composerStep.RunID = run.ID
	if err := insertAIStep(ctx, s.db, composerStep); err != nil {
		return err
	}
	checkpoint = buildAIAgentRunCheckpoint(scope, active.Config.Chat.Routing.DefaultTaskClass, prepared)
	checkpointJSON = encodeJSON(checkpoint)
	run.CheckpointJSON = checkpointJSON
	if err := emit("stage", aiStreamStageEvent{
		Stage:          currentState,
		Status:         "success",
		Message:        "证据召回完成",
		EvidenceCount:  len(retrieval.Evidence),
		CandidateCount: len(retrieval.ServiceCandidates),
	}); err != nil {
		return err
	}

	citations := make([]AIMessageCitation, 0, len(retrieval.Evidence))
	for _, evidence := range retrieval.Evidence {
		citation := evidence.Citation
		citation.MessageID = assistantMsg.ID
		inserted, err := insertAICitation(ctx, s.db, citation)
		if err != nil {
			return err
		}
		citations = append(citations, inserted)
	}
	candidates := make([]AIServiceCandidate, 0, len(retrieval.ServiceCandidates))
	for _, candidate := range retrieval.ServiceCandidates {
		candidate.RunID = run.ID
		candidate.MessageID = assistantMsg.ID
		inserted, err := insertAIServiceCandidate(ctx, s.db, candidate)
		if err != nil {
			return err
		}
		candidates = append(candidates, inserted)
	}
	if err := emit("service_candidates", map[string]any{"items": candidates}); err != nil {
		return err
	}
	if err := emit("citations", map[string]any{"items": citations}); err != nil {
		return err
	}

	var modelResult aiModelResult
	currentState = "model_call"
	lastPersistAt := time.Now()
	lastPersistLen := 0
	var streamedDraft strings.Builder
	persistStreaming := func(force bool) error {
		if !force && len(assistantMsg.Content)-lastPersistLen < 512 && time.Since(lastPersistAt) < 500*time.Millisecond {
			return nil
		}
		assistantMsg.Status = "streaming"
		if err := updateAIMessage(ctx, s.db, assistantMsg); err != nil {
			return err
		}
		lastPersistAt = time.Now()
		lastPersistLen = len(assistantMsg.Content)
		return nil
	}
	emitFullAnswer := func(result aiModelResult) error {
		assistantMsg.Content = result.Content
		if err := persistStreaming(true); err != nil {
			return err
		}
		return emit("answer_delta", aiStreamAnswerDeltaEvent{MessageID: assistantMsg.ID, Delta: result.Content})
	}
	if len(retrieval.Evidence) == 0 {
		modelResult = aiModelResult{
			Content:      localNoEvidenceAnswer(question),
			ProviderName: "local-retrieval",
			Model:        "none",
			ModelRouteJSON: encodeJSON(map[string]any{
				"mode":   "local",
				"reason": "no_evidence",
			}),
			LatencyMS: int(time.Since(start).Milliseconds()),
		}
		if err := emit("stage", aiStreamStageEvent{Stage: currentState, Status: "skipped", Message: "未找到证据，返回本地检索说明"}); err != nil {
			return err
		}
	} else if !active.Config.Enabled {
		modelResult = aiModelResult{
			Content:        localEvidenceAnswer(question, retrieval, false),
			ProviderName:   "local-retrieval",
			Model:          "none",
			ModelRouteJSON: encodeJSON(map[string]any{"mode": "local", "reason": "ai_disabled"}),
			LatencyMS:      int(time.Since(start).Milliseconds()),
		}
		if err := emit("stage", aiStreamStageEvent{Stage: currentState, Status: "skipped", Message: "AI 未启用，返回本地证据摘要"}); err != nil {
			return err
		}
	} else {
		if err := emit("stage", aiStreamStageEvent{Stage: currentState, Status: "running", Message: "调用模型生成回答"}); err != nil {
			return err
		}
		modelResult, err = s.callRoutedAIModelStream(ctx, active.Config, question, retrieval, aiStreamModelCallbacks{
			ProviderAttempt: func(event aiStreamProviderAttemptEvent) error {
				if event.Status == "started" {
					assistantMsg.ProviderName = event.Provider
					assistantMsg.Model = event.Model
					assistantMsg.ModelRouteJSON = encodeJSON(map[string]any{
						"task_class":   event.TaskClass,
						"provider_key": event.ProviderKey,
						"provider":     event.Provider,
						"model":        event.Model,
					})
				}
				return emit("provider_attempt", event)
			},
			AnswerDelta: func(delta string) error {
				streamedDraft.WriteString(delta)
				return nil
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				if modelResult.Model != "" {
					assistantMsg.Model = modelResult.Model
				}
				if modelResult.ProviderName != "" {
					assistantMsg.ProviderName = modelResult.ProviderName
				}
				if modelResult.ModelRouteJSON != "" {
					assistantMsg.ModelRouteJSON = modelResult.ModelRouteJSON
				}
				assistantMsg.PromptTokens = modelResult.PromptTokens
				assistantMsg.CompletionTokens = modelResult.CompletionTokens
				run.ProviderFailoverJSON = modelResult.FailoverJSON
				return err
			}
			var partial aiStreamPartialError
			if errors.As(err, &partial) && strings.TrimSpace(modelResult.Content) != "" {
				assistantMsg.Model = modelResult.Model
				assistantMsg.ProviderName = modelResult.ProviderName
				assistantMsg.ModelRouteJSON = modelResult.ModelRouteJSON
				assistantMsg.PromptTokens = modelResult.PromptTokens
				assistantMsg.CompletionTokens = modelResult.CompletionTokens
				assistantMsg.LatencyMS = int(time.Since(start).Milliseconds())
				modelResult = aiModelFallbackForError(question, retrieval, start, partial)
				if err := emit("stage", aiStreamStageEvent{Stage: currentState, Status: "failed", Message: "模型流中断，返回本地证据摘要"}); err != nil {
					return err
				}
			}
			if strings.TrimSpace(modelResult.Content) == "" || !errors.As(err, &partial) {
				modelResult = aiModelFallbackForError(question, retrieval, start, err)
				if err := emit("stage", aiStreamStageEvent{Stage: currentState, Status: "failed", Message: "模型调用失败，返回本地证据摘要"}); err != nil {
					return err
				}
			}
		} else {
			modelResult.LatencyMS = int(time.Since(start).Milliseconds())
			if strings.TrimSpace(modelResult.Content) == "" && streamedDraft.Len() > 0 {
				modelResult.Content = streamedDraft.String()
			}
		}
	}

	currentState = "verify_answer"
	verificationOutcome := s.verifyAndMaybeRewriteAIAnswer(ctx, active, question, retrieval, modelResult, start)
	modelResult = verificationOutcome.Result
	verifierFrame, verifierContract, verifierCoverage, verifierBundle := aiAnswerVerifierInputs(retrieval)
	verifierStep := buildAIAnswerVerifierStep(verifierFrame, verifierContract, verifierCoverage, verifierBundle, verificationOutcome.Report, modelResult.Content)
	verifierStep.RunID = run.ID
	if err := insertAIStep(ctx, s.db, verifierStep); err != nil {
		return err
	}
	if err := emit("verification", map[string]any{"report": verificationOutcome.Report}); err != nil {
		return err
	}
	if err := emitFullAnswer(modelResult); err != nil {
		return err
	}

	notice := aiNoticeFromModelResult(modelResult)
	assistantMsg.Model = modelResult.Model
	assistantMsg.ProviderName = modelResult.ProviderName
	assistantMsg.ModelRouteJSON = modelResult.ModelRouteJSON
	assistantMsg.PromptTokens = modelResult.PromptTokens
	assistantMsg.CompletionTokens = modelResult.CompletionTokens
	assistantMsg.LatencyMS = modelResult.LatencyMS
	assistantMsg.Status = "success"
	assistantMsg.ErrorMessage = ""
	if err := updateAIMessage(ctx, s.db, assistantMsg); err != nil {
		return err
	}
	if err := finishAIRun(ctx, s.db, run.ID, AIAgentRun{
		AssistantMessageID:     assistantMsg.ID,
		Status:                 verificationOutcome.RunStatus,
		CurrentState:           "verify_answer",
		Intent:                 retrieval.Intent,
		ScopeJSON:              encodeJSON(retrieval.Scope),
		RetrievalPlanJSON:      encodeJSON(retrieval.Plan),
		ServiceCandidateCount:  len(candidates),
		EvidenceCount:          len(citations),
		CodeEvidenceCount:      countCodeEvidence(citations),
		UnconfirmedCount:       aiContractCoverageUnconfirmedRequiredCount(contractCoverage),
		VerificationStatus:     verificationOutcome.VerificationStatus,
		VerificationReportJSON: encodeJSON(verificationOutcome.Report),
		CheckpointJSON:         checkpointJSON,
		Model:                  modelResult.Model,
		ProviderName:           modelResult.ProviderName,
		ProviderFailoverJSON:   modelResult.FailoverJSON,
		ModelRouteJSON:         modelResult.ModelRouteJSON,
		FinishedAt:             nowString(),
	}); err != nil {
		return err
	}
	run, _ = getAIRun(ctx, s.db, run.ID)
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_sessions SET title = CASE WHEN title = '新的 AI 问答' THEN ? ELSE title END,
		updated_at = ? WHERE id = ?`, aiTitleFromQuestion(question), nowString(), sessionID)
	if err := emit("message_done", map[string]any{
		"run":                run,
		"message":            assistantMsg,
		"service_candidates": candidates,
		"citations":          citations,
		"notice":             notice,
		"usage": map[string]int{
			"prompt_tokens":     assistantMsg.PromptTokens,
			"completion_tokens": assistantMsg.CompletionTokens,
		},
	}); err != nil {
		return err
	}
	completed = true
	return nil
}

func aiPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func aiNoticeFromModelResult(result aiModelResult) *AINotice {
	var route struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal([]byte(result.ModelRouteJSON), &route)
	switch route.Reason {
	case "ai_disabled":
		return &AINotice{Type: "ai_disabled", Message: "AI 问答尚未启用，已返回本地 Git 检索摘要。"}
	case "provider_error":
		return &AINotice{Type: "provider_error", Message: "模型调用失败，已返回本地 Git 检索摘要。"}
	case "no_evidence":
		return &AINotice{Type: "no_evidence", Message: "未找到可支撑回答的 Git 证据。"}
	default:
		return nil
	}
}

func normalizeAIScope(scope AIQuestionScope) AIQuestionScope {
	if scope.RepoMode == "" {
		scope.RepoMode = "global"
	}
	if scope.SourceMode == "" {
		scope.SourceMode = "smart_latest_with_branch_candidates"
	}
	if len(scope.FileTypes) == 0 {
		scope.FileTypes = []string{"all"}
	}
	seen := map[int64]struct{}{}
	out := scope.RepoIDs[:0]
	for _, id := range scope.RepoIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	scope.RepoIDs = out
	return scope
}

func (s *Server) prepareAIQuestion(ctx context.Context, sessionID int64, question string, scope AIQuestionScope) (aiQuestionPreparation, error) {
	prepared := aiQuestionPreparation{SearchQuestion: question, Scope: scope}
	if !aiLooksLikeFollowUpQuestion(question) {
		return prepared, nil
	}
	messages, err := recentAIMessages(ctx, s.db, sessionID, 8)
	if err != nil {
		return aiQuestionPreparation{}, err
	}
	var previousUser, previousAssistant AIMessage
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if previousUser.ID == 0 && strings.TrimSpace(msg.Content) != "" {
				previousUser = msg
			}
		case "assistant":
			if previousAssistant.ID == 0 && strings.TrimSpace(msg.Content) != "" {
				previousAssistant = msg
			}
		}
	}
	if previousUser.ID == 0 && previousAssistant.ID == 0 {
		return prepared, nil
	}
	conversation := aiConversationContext{FollowUp: true}
	if previousUser.ID > 0 {
		conversation.PreviousUserQuestion = aiCompactContextText(previousUser.Content, 500)
	}
	if previousAssistant.ID > 0 {
		conversation.PreviousAssistantSummary = aiCompactContextText(previousAssistant.Content, 900)
		anchors, err := listAIRecentCitationAnchors(ctx, s.db, previousAssistant.ID, 8)
		if err != nil {
			return aiQuestionPreparation{}, err
		}
		pathAnchors := anchors
		if mentionedAnchors := aiAnchorsMentionedInText(anchors, conversation.PreviousAssistantSummary); len(mentionedAnchors) > 0 {
			pathAnchors = mentionedAnchors
		}
		repoSeen := map[int64]struct{}{}
		pathSeen := map[string]struct{}{}
		for _, anchor := range anchors {
			if anchor.RepoID > 0 {
				if _, ok := repoSeen[anchor.RepoID]; !ok {
					repoSeen[anchor.RepoID] = struct{}{}
					conversation.FocusRepoIDs = append(conversation.FocusRepoIDs, anchor.RepoID)
				}
			}
		}
		for _, anchor := range pathAnchors {
			if anchor.FilePath != "" {
				if _, ok := pathSeen[anchor.FilePath]; !ok {
					pathSeen[anchor.FilePath] = struct{}{}
					conversation.PreviousCitationPaths = append(conversation.PreviousCitationPaths, anchor.FilePath)
				}
			}
		}
	}
	var b strings.Builder
	b.WriteString("当前问题是上一轮主题的追问，请优先沿用上一轮主题、服务和引用路径。\n")
	if conversation.PreviousUserQuestion != "" {
		b.WriteString("上一轮用户问题：")
		b.WriteString(conversation.PreviousUserQuestion)
		b.WriteByte('\n')
	}
	if conversation.PreviousAssistantSummary != "" {
		b.WriteString("上一轮回答摘要：")
		b.WriteString(conversation.PreviousAssistantSummary)
		b.WriteByte('\n')
	}
	if len(conversation.PreviousCitationPaths) > 0 {
		b.WriteString("上一轮引用路径：")
		b.WriteString(strings.Join(conversation.PreviousCitationPaths, "；"))
		b.WriteByte('\n')
	}
	b.WriteString("当前追问：")
	b.WriteString(question)
	prepared.SearchQuestion = b.String()
	prepared.Conversation = conversation
	if len(scope.RepoIDs) == 0 && scope.CurrentFile == nil && len(conversation.FocusRepoIDs) > 0 && !aiFollowUpAsksForBroaderScope(question) {
		prepared.Scope.RepoIDs = append([]int64(nil), conversation.FocusRepoIDs...)
		prepared.Scope.RepoMode = "follow_up_context"
	}
	return prepared, nil
}

func (s *Server) expandAIQuestionForRetrieval(ctx context.Context, cfg AIConfigData, prepared aiQuestionPreparation) (aiQuestionPreparation, AIAgentStep) {
	step := AIAgentStep{
		AgentName:  "query_planner",
		StepType:   "model_call",
		ToolName:   "generate_search_terms",
		InputJSON:  encodeJSON(map[string]any{"question": truncate(prepared.SearchQuestion, 800)}),
		CreatedAt:  nowString(),
		FinishedAt: nowString(),
	}
	if !cfg.Enabled {
		step.Status = "skipped"
		step.OutputJSON = encodeJSON(map[string]any{"reason": "ai_disabled"})
		return prepared, step
	}
	result, terms, err := s.generateAIQueryTerms(ctx, cfg, prepared.SearchQuestion, prepared.TaskFrame)
	if err != nil {
		step.Status = "failed"
		step.ErrorMessage = sanitizeProviderError(err.Error())
		return prepared, step
	}
	step.Status = "success"
	step.Model = result.Model
	step.ProviderName = result.ProviderName
	step.ModelRouteReason = result.ModelRouteJSON
	step.TokenInput = result.PromptTokens
	step.TokenOutput = result.CompletionTokens
	step.OutputJSON = encodeJSON(map[string]any{"terms": terms})
	if len(terms) == 0 {
		return prepared, step
	}
	prepared.GeneratedSearchTerms = terms
	prepared.SearchQuestion = strings.TrimSpace(prepared.SearchQuestion + "\n" + strings.Join(terms, " "))
	return prepared, step
}

func applyAIConversationContext(retrieval *aiRetrievalResult, prepared aiQuestionPreparation) {
	if prepared.Contract != nil {
		retrieval.Contract = prepared.Contract
		if retrieval.Plan == nil {
			retrieval.Plan = map[string]any{}
		}
		retrieval.Plan["evidence_contract"] = summarizeAIEvidenceContract(*prepared.Contract)
	}
	if len(prepared.GeneratedSearchTerms) > 0 {
		if retrieval.Plan == nil {
			retrieval.Plan = map[string]any{}
		}
		retrieval.Plan["model_generated_terms"] = prepared.GeneratedSearchTerms
		retrieval.Plan["effective_question"] = truncate(prepared.SearchQuestion, 800)
	}
	if !prepared.Conversation.FollowUp {
		return
	}
	retrieval.Conversation = prepared.Conversation
	if retrieval.Plan == nil {
		retrieval.Plan = map[string]any{}
	}
	retrieval.Plan["conversation_context"] = prepared.Conversation
	retrieval.Plan["effective_question"] = truncate(prepared.SearchQuestion, 800)
}

func recentAIMessages(ctx context.Context, db *sql.DB, sessionID int64, limit int) ([]AIMessage, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, session_id, role, content, model, provider_name, model_route_json,
		prompt_tokens, completion_tokens, latency_ms, status, error_message, created_at
		FROM ai_messages WHERE session_id = ? ORDER BY id DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []AIMessage{}
	for rows.Next() {
		msg, err := scanAIMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

type aiCitationAnchor struct {
	RepoID   int64
	FilePath string
}

func listAIRecentCitationAnchors(ctx context.Context, db *sql.DB, messageID int64, limit int) ([]aiCitationAnchor, error) {
	rows, err := db.QueryContext(ctx, `SELECT repo_id, file_path FROM ai_message_citations
		WHERE message_id = ? ORDER BY score DESC, id LIMIT ?`, messageID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	anchors := []aiCitationAnchor{}
	for rows.Next() {
		var anchor aiCitationAnchor
		if err := rows.Scan(&anchor.RepoID, &anchor.FilePath); err != nil {
			return nil, err
		}
		anchors = append(anchors, anchor)
	}
	return anchors, rows.Err()
}

func aiAnchorsMentionedInText(anchors []aiCitationAnchor, text string) []aiCitationAnchor {
	text = strings.ToLower(text)
	if text == "" {
		return nil
	}
	mentioned := []aiCitationAnchor{}
	for _, anchor := range anchors {
		filePath := strings.ToLower(anchor.FilePath)
		if filePath == "" {
			continue
		}
		fileName := strings.ToLower(filepath.Base(filePath))
		if strings.Contains(text, filePath) || fileName != "." && strings.Contains(text, fileName) {
			mentioned = append(mentioned, anchor)
		}
	}
	return mentioned
}

func aiLooksLikeFollowUpQuestion(question string) bool {
	q := strings.TrimSpace(strings.ToLower(question))
	if q == "" {
		return false
	}
	hasSignal := false
	for _, signal := range []string{"详细", "具体", "展开", "继续", "上面", "上一", "刚才", "这个", "这些", "它", "上述", "结构", "格式", "字段", "参数", "说明", "完整"} {
		if strings.Contains(q, signal) {
			hasSignal = true
			break
		}
	}
	if !hasSignal {
		return false
	}
	if aiQuestionHasExplicitServiceAnchor(q) && !aiQuestionHasFollowPronoun(q) {
		return false
	}
	return len([]rune(q)) <= 48 || aiQuestionHasFollowPronoun(q)
}

func aiQuestionHasExplicitServiceAnchor(question string) bool {
	for _, marker := range []string{"服务", "仓库", "repo", "项目", "模块", "文件", "package", "class", "function", "server", ".go", ".ts", ".js", ".md", "/", "\\", "api", "http", "grpc", "openapi"} {
		if strings.Contains(question, marker) {
			return true
		}
	}
	return false
}

func aiQuestionHasFollowPronoun(question string) bool {
	for _, marker := range []string{"这个", "这些", "它", "上述", "上面", "上一", "刚才", "继续"} {
		if strings.Contains(question, marker) {
			return true
		}
	}
	return false
}

func aiFollowUpAsksForBroaderScope(question string) bool {
	q := strings.ToLower(question)
	for _, marker := range []string{"全部仓库", "所有仓库", "所有服务", "其他服务", "别的服务", "另一个", "对比", "全局", "跨服务"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

func aiCompactContextText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	return truncate(value, limit)
}

func createAIRun(ctx context.Context, db *sql.DB, sessionID, userMessageID int64, cfg AIConfigVersion, scope AIQuestionScope) (AIAgentRun, error) {
	now := nowString()
	checkpointJSON := encodeJSON(buildInitialAIAgentRunCheckpoint(scope, cfg.Config.Chat.Routing.DefaultTaskClass))
	res, err := db.ExecContext(ctx, `INSERT INTO ai_agent_runs
		(session_id, user_message_id, status, current_state, scope_json, checkpoint_json, index_snapshot_id, config_version, config_hash, started_at)
		VALUES (?, ?, 'running', 'queued', ?, ?, 0, ?, ?, ?)`,
		sessionID, userMessageID, encodeJSON(scope), checkpointJSON, cfg.Version, cfg.ConfigHash, now)
	if err != nil {
		return AIAgentRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AIAgentRun{}, err
	}
	return getAIRun(ctx, db, id)
}

func finishAIRun(ctx context.Context, db *sql.DB, id int64, run AIAgentRun) error {
	if run.FinishedAt == "" {
		run.FinishedAt = nowString()
	}
	_, err := db.ExecContext(ctx, `UPDATE ai_agent_runs SET assistant_message_id = ?, status = ?, current_state = ?,
		intent = ?, scope_json = ?, retrieval_plan_json = ?, service_candidate_count = ?, evidence_count = ?,
		code_evidence_count = ?, memory_count = ?, unconfirmed_count = ?, verification_status = ?,
		verification_report_json = ?, checkpoint_json = CASE WHEN ? = '' THEN checkpoint_json ELSE ? END, model = ?, provider_name = ?, provider_failover_json = ?,
		model_route_json = ?, escalation_count = ?, estimated_cost_json = ?, finished_at = ?, error_message = ?
		WHERE id = ?`,
		run.AssistantMessageID, run.Status, run.CurrentState, run.Intent, emptyDefault(run.ScopeJSON, "{}"),
		run.RetrievalPlanJSON, run.ServiceCandidateCount, run.EvidenceCount, run.CodeEvidenceCount, run.MemoryCount,
		run.UnconfirmedCount, emptyDefault(run.VerificationStatus, "not_run"), run.VerificationReportJSON,
		run.CheckpointJSON, run.CheckpointJSON, run.Model, run.ProviderName, run.ProviderFailoverJSON, run.ModelRouteJSON,
		run.EscalationCount, run.EstimatedCostJSON, run.FinishedAt, run.ErrorMessage, id)
	return err
}

func getAIRun(ctx context.Context, db *sql.DB, id int64) (AIAgentRun, error) {
	row := db.QueryRowContext(ctx, `SELECT id, session_id, user_message_id, assistant_message_id, status,
		current_state, intent, scope_json, retrieval_plan_json, service_candidate_count, evidence_count,
		code_evidence_count, memory_count, unconfirmed_count, verification_status, verification_report_json,
		checkpoint_json, index_snapshot_id, config_version, config_hash, model, provider_name, provider_failover_json,
		model_route_json, escalation_count, estimated_cost_json, started_at, finished_at, error_message
		FROM ai_agent_runs WHERE id = ?`, id)
	var run AIAgentRun
	if err := row.Scan(&run.ID, &run.SessionID, &run.UserMessageID, &run.AssistantMessageID, &run.Status,
		&run.CurrentState, &run.Intent, &run.ScopeJSON, &run.RetrievalPlanJSON, &run.ServiceCandidateCount,
		&run.EvidenceCount, &run.CodeEvidenceCount, &run.MemoryCount, &run.UnconfirmedCount, &run.VerificationStatus,
		&run.VerificationReportJSON, &run.CheckpointJSON, &run.IndexSnapshotID, &run.ConfigVersion, &run.ConfigHash,
		&run.Model, &run.ProviderName, &run.ProviderFailoverJSON, &run.ModelRouteJSON, &run.EscalationCount,
		&run.EstimatedCostJSON, &run.StartedAt, &run.FinishedAt, &run.ErrorMessage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AIAgentRun{}, errNotFound("ai run not found")
		}
		return AIAgentRun{}, err
	}
	return run, nil
}

func insertAIStep(ctx context.Context, db *sql.DB, step AIAgentStep) error {
	if step.CreatedAt == "" {
		step.CreatedAt = nowString()
	}
	_, err := db.ExecContext(ctx, `INSERT INTO ai_agent_steps
		(run_id, parent_step_id, agent_name, step_type, status, tool_name, task_class, model, provider_name,
		 model_route_reason, escalated_from_step_id, input_json, output_json, token_input, token_output,
		 estimated_cost, latency_ms, error_message, created_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		step.RunID, step.ParentStepID, step.AgentName, step.StepType, step.Status, step.ToolName, step.TaskClass,
		step.Model, step.ProviderName, step.ModelRouteReason, step.EscalatedFromStepID, step.InputJSON, step.OutputJSON,
		step.TokenInput, step.TokenOutput, step.EstimatedCost, step.LatencyMS, step.ErrorMessage, step.CreatedAt,
		step.FinishedAt)
	return err
}

func listAIRunSteps(ctx context.Context, db *sql.DB, runID int64) ([]AIAgentStep, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, run_id, parent_step_id, agent_name, step_type, status, tool_name,
		task_class, model, provider_name, model_route_reason, escalated_from_step_id, input_json, output_json,
		token_input, token_output, estimated_cost, latency_ms, error_message, created_at, finished_at
		FROM ai_agent_steps WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := []AIAgentStep{}
	for rows.Next() {
		var step AIAgentStep
		if err := rows.Scan(&step.ID, &step.RunID, &step.ParentStepID, &step.AgentName, &step.StepType, &step.Status,
			&step.ToolName, &step.TaskClass, &step.Model, &step.ProviderName, &step.ModelRouteReason,
			&step.EscalatedFromStepID, &step.InputJSON, &step.OutputJSON, &step.TokenInput, &step.TokenOutput,
			&step.EstimatedCost, &step.LatencyMS, &step.ErrorMessage, &step.CreatedAt, &step.FinishedAt); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func insertAIServiceCandidate(ctx context.Context, db *sql.DB, candidate AIServiceCandidate) (AIServiceCandidate, error) {
	if candidate.CreatedAt == "" {
		candidate.CreatedAt = nowString()
	}
	res, err := db.ExecContext(ctx, `INSERT INTO ai_service_candidates
		(run_id, message_id, service_profile_id, repo_id, service_name, matched_terms, confidence, reason, score,
		 evidence_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		candidate.RunID, candidate.MessageID, candidate.ServiceProfileID, candidate.RepoID, candidate.ServiceName,
		encodeJSON(candidate.MatchedTerms), candidate.Confidence, candidate.Reason, candidate.Score,
		candidate.EvidenceCount, candidate.CreatedAt)
	if err != nil {
		return AIServiceCandidate{}, err
	}
	id, _ := res.LastInsertId()
	candidate.ID = id
	return candidate, nil
}

func listAIServiceCandidatesForSession(ctx context.Context, db *sql.DB, sessionID int64) ([]AIServiceCandidate, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.id, c.run_id, c.message_id, c.service_profile_id, c.repo_id,
		COALESCE(r.name, ''), c.service_name, c.matched_terms, c.confidence, c.reason, c.score,
		c.evidence_count, c.created_at
		FROM ai_service_candidates c
		JOIN ai_messages m ON m.id = c.message_id
		LEFT JOIN repositories r ON r.id = c.repo_id
		WHERE m.session_id = ?
		ORDER BY c.message_id DESC, c.score DESC, c.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []AIServiceCandidate{}
	for rows.Next() {
		var candidate AIServiceCandidate
		var matchedTerms string
		if err := rows.Scan(&candidate.ID, &candidate.RunID, &candidate.MessageID, &candidate.ServiceProfileID,
			&candidate.RepoID, &candidate.RepoName, &candidate.ServiceName, &matchedTerms, &candidate.Confidence,
			&candidate.Reason, &candidate.Score, &candidate.EvidenceCount, &candidate.CreatedAt); err != nil {
			return nil, err
		}
		candidate.MatchedTerms = decodeStringList(matchedTerms, nil)
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func listAIServiceCandidatesForRun(ctx context.Context, db *sql.DB, runID int64) ([]AIServiceCandidate, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.id, c.run_id, c.message_id, c.service_profile_id, c.repo_id,
		COALESCE(r.name, ''), c.service_name, c.matched_terms, c.confidence, c.reason, c.score,
		c.evidence_count, c.created_at
		FROM ai_service_candidates c
		LEFT JOIN repositories r ON r.id = c.repo_id
		WHERE c.run_id = ?
		ORDER BY c.score DESC, c.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []AIServiceCandidate{}
	for rows.Next() {
		var candidate AIServiceCandidate
		var matchedTerms string
		if err := rows.Scan(&candidate.ID, &candidate.RunID, &candidate.MessageID, &candidate.ServiceProfileID,
			&candidate.RepoID, &candidate.RepoName, &candidate.ServiceName, &matchedTerms, &candidate.Confidence,
			&candidate.Reason, &candidate.Score, &candidate.EvidenceCount, &candidate.CreatedAt); err != nil {
			return nil, err
		}
		candidate.MatchedTerms = decodeStringList(matchedTerms, nil)
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func insertAICitation(ctx context.Context, db *sql.DB, citation AIMessageCitation) (AIMessageCitation, error) {
	if citation.CreatedAt == "" {
		citation.CreatedAt = nowString()
	}
	res, err := db.ExecContext(ctx, `INSERT INTO ai_message_citations
		(message_id, index_snapshot_id, chunk_id, api_symbol_id, repo_id, version_id, source_scope, branch,
		 commit_sha, file_path, line_start, line_end, quote_text, score, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		citation.MessageID, citation.IndexSnapshotID, citation.ChunkID, citation.APISymbolID, citation.RepoID,
		citation.VersionID, citation.SourceScope, citation.Branch, citation.CommitSHA, citation.FilePath,
		citation.LineStart, citation.LineEnd, citation.QuoteText, citation.Score, citation.CreatedAt)
	if err != nil {
		return AIMessageCitation{}, err
	}
	id, _ := res.LastInsertId()
	citation.ID = id
	return citation, nil
}

func listAICitationsForSession(ctx context.Context, db *sql.DB, sessionID int64) ([]AIMessageCitation, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.id, c.message_id, c.index_snapshot_id, c.chunk_id,
		c.api_symbol_id, c.repo_id, COALESCE(r.name, ''), c.version_id, c.source_scope, c.branch,
		c.commit_sha, c.file_path, c.line_start, c.line_end, c.quote_text, c.score, c.created_at
		FROM ai_message_citations c
		JOIN ai_messages m ON m.id = c.message_id
		LEFT JOIN repositories r ON r.id = c.repo_id
		WHERE m.session_id = ?
		ORDER BY c.message_id DESC, c.score DESC, c.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	citations := []AIMessageCitation{}
	for rows.Next() {
		var citation AIMessageCitation
		if err := rows.Scan(&citation.ID, &citation.MessageID, &citation.IndexSnapshotID, &citation.ChunkID,
			&citation.APISymbolID, &citation.RepoID, &citation.RepoName, &citation.VersionID, &citation.SourceScope,
			&citation.Branch, &citation.CommitSHA, &citation.FilePath, &citation.LineStart, &citation.LineEnd,
			&citation.QuoteText, &citation.Score, &citation.CreatedAt); err != nil {
			return nil, err
		}
		citations = append(citations, citation)
	}
	return citations, rows.Err()
}

func listAICitationsForRun(ctx context.Context, db *sql.DB, runID int64) ([]AIMessageCitation, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.id, c.message_id, c.index_snapshot_id, c.chunk_id,
		c.api_symbol_id, c.repo_id, COALESCE(rp.name, ''), c.version_id, c.source_scope, c.branch,
		c.commit_sha, c.file_path, c.line_start, c.line_end, c.quote_text, c.score, c.created_at
		FROM ai_message_citations c
		JOIN ai_agent_runs ar ON ar.assistant_message_id = c.message_id
		LEFT JOIN repositories rp ON rp.id = c.repo_id
		WHERE ar.id = ?
		ORDER BY c.score DESC, c.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	citations := []AIMessageCitation{}
	for rows.Next() {
		var citation AIMessageCitation
		if err := rows.Scan(&citation.ID, &citation.MessageID, &citation.IndexSnapshotID, &citation.ChunkID,
			&citation.APISymbolID, &citation.RepoID, &citation.RepoName, &citation.VersionID, &citation.SourceScope,
			&citation.Branch, &citation.CommitSHA, &citation.FilePath, &citation.LineStart, &citation.LineEnd,
			&citation.QuoteText, &citation.Score, &citation.CreatedAt); err != nil {
			return nil, err
		}
		citations = append(citations, citation)
	}
	return citations, rows.Err()
}

func (s *Server) retrieveAIEvidence(ctx context.Context, question string, scope AIQuestionScope, cfg AIConfigData) (aiRetrievalResult, error) {
	return s.retrieveAIEvidenceWithTaskFrame(ctx, question, scope, cfg, nil, nil)
}

func (s *Server) retrieveAIEvidenceWithTaskFrame(ctx context.Context, question string, scope AIQuestionScope, cfg AIConfigData, frame *aiTaskFrame, contract *aiEvidenceContract) (aiRetrievalResult, error) {
	intent := aiTaskIntentForRetrieval(question, frame)
	terms := aiQueryTerms(question)
	effectiveFrame := aiTaskFrame{Intent: aiTaskIntentFromLegacy(intent), KnownTerms: append([]string(nil), terms...)}
	if frame != nil {
		effectiveFrame = *frame
		effectiveFrame.KnownTerms = mergeTerms(effectiveFrame.KnownTerms, terms)
	} else {
		effectiveFrame.UserGoal = aiTaskFrameDefaultUserGoal(effectiveFrame.Intent)
		effectiveFrame.AnswerShape = aiTaskFrameDefaultAnswerShape(effectiveFrame.Intent)
		effectiveFrame.TargetArtifacts = aiTaskFrameDefaultArtifacts(effectiveFrame.Intent)
	}
	if strings.TrimSpace(effectiveFrame.Intent) == "" {
		effectiveFrame.Intent = aiTaskIntentFromLegacy(intent)
	}
	effectiveContract := contract
	if effectiveContract == nil {
		generatedContract := buildAIEvidenceContract(effectiveFrame)
		effectiveContract = &generatedContract
	}
	retrieval, err := s.runAIRetrievalOrchestrator(ctx, &effectiveFrame, effectiveContract, scope, cfg)
	if err != nil {
		return aiRetrievalResult{}, err
	}
	retrieval.Intent = intent
	if retrieval.Plan != nil {
		retrieval.Plan["intent"] = intent
		retrieval.Plan["terms"] = terms
	}
	return retrieval, nil
}

func (s *Server) currentFileEvidence(ctx context.Context, current *AICurrentFileScope, terms []string) (aiEvidence, error) {
	repo, err := getRepository(ctx, s.db, current.RepoID)
	if err != nil {
		return aiEvidence{}, err
	}
	var data []byte
	var branch, commit, filePath string
	versionID := current.VersionID
	if versionID > 0 {
		version, err := getVersion(ctx, s.db, current.RepoID, versionID)
		if err != nil {
			return aiEvidence{}, err
		}
		data, err = s.git.catFile(ctx, s.git.repoPath(current.RepoID), version.BlobSHA)
		if err != nil {
			return aiEvidence{}, err
		}
		branch = version.Branch
		commit = version.HeadCommitSHA
		filePath = version.FilePath
	} else {
		branch = current.Branch
		commit = current.CommitSHA
		filePath = current.FilePath
		data, err = s.git.showFile(ctx, s.git.repoPath(current.RepoID), commit, filePath)
		if err != nil {
			return aiEvidence{}, err
		}
	}
	if !aiLooksText(data) {
		return aiEvidence{}, errBadRequest("current file is not text")
	}
	snippet, start, end := aiSnippet(string(data), terms, true)
	return aiEvidence{
		Repo: repo,
		Citation: AIMessageCitation{
			RepoID:      repo.ID,
			VersionID:   versionID,
			SourceScope: "smart_latest",
			Branch:      branch,
			CommitSHA:   commit,
			FilePath:    filePath,
			LineStart:   start,
			LineEnd:     end,
			QuoteText:   truncate(snippet, 700),
			Score:       1000,
		},
		Content:      snippet,
		MatchedTerms: terms,
		Score:        1000,
	}, nil
}

func (s *Server) aiRefTargets(ctx context.Context, repo Repository, sourceMode string) ([]aiRefTarget, error) {
	var targets []aiRefTarget
	refs, err := listBranches(ctx, s.db, repo.ID)
	if err != nil {
		return nil, err
	}
	defaultRef := pickDefaultAIRef(repo, refs)
	if defaultRef != nil {
		targets = append(targets, aiRefTarget{Branch: defaultRef.RefName, CommitSHA: defaultRef.CommitSHA, SourceScope: "smart_latest"})
	}
	if strings.Contains(sourceMode, "branch") {
		for _, ref := range refs {
			if defaultRef != nil && ref.RefName == defaultRef.RefName {
				continue
			}
			if !aiBranchCandidate(repo, ref) {
				continue
			}
			targets = append(targets, aiRefTarget{Branch: ref.RefName, CommitSHA: ref.CommitSHA, SourceScope: "branch_candidate"})
			if len(targets) >= 5 {
				break
			}
		}
	}
	return targets, nil
}

func pickDefaultAIRef(repo Repository, refs []RepoRef) *RepoRef {
	for i := range refs {
		if refs[i].RefName == repo.DefaultBranch {
			return &refs[i]
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		ri := branchRank(repo.BranchPriority, refs[i].RefName)
		rj := branchRank(repo.BranchPriority, refs[j].RefName)
		if ri != rj {
			return ri < rj
		}
		return refs[i].CommitTime > refs[j].CommitTime
	})
	if len(refs) == 0 {
		return nil
	}
	return &refs[0]
}

func aiBranchCandidate(repo Repository, ref RepoRef) bool {
	if !branchTracked(repo, ref.RefName) {
		return false
	}
	if !participatesLatest(repo, ref.RefName, ref.CommitTime) {
		return false
	}
	if ref.CommitTime == "" {
		return true
	}
	t, err := time.Parse(timeLayout, ref.CommitTime)
	if err != nil {
		return true
	}
	maxAge := 90
	if repo.StaleBranchDays > 0 && repo.StaleBranchDays < maxAge {
		maxAge = repo.StaleBranchDays
	}
	return time.Since(t) <= time.Duration(maxAge)*24*time.Hour
}

func (s *Server) searchRepoSmartLatestEvidence(ctx context.Context, repo Repository, terms []string, intent string, indexCfg AIIndexConfig) ([]aiEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT v.id, v.branch, v.head_commit_sha, v.file_path,
		v.file_size, v.blob_sha
		FROM doc_latest l
		JOIN doc_versions v ON v.id = l.version_id
		WHERE l.repo_id = ? AND v.status IN ('active','renamed','moved')`, repo.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type smartLatestCandidate struct {
		VersionID int64
		Branch    string
		CommitSHA string
		FilePath  string
		FileSize  int64
		BlobSHA   string
		PreScore  float64
		PathTerms []string
	}
	candidates := []smartLatestCandidate{}
	for rows.Next() {
		var candidate smartLatestCandidate
		if err := rows.Scan(&candidate.VersionID, &candidate.Branch, &candidate.CommitSHA, &candidate.FilePath,
			&candidate.FileSize, &candidate.BlobSHA); err != nil {
			return nil, err
		}
		if aiShouldSkipPath(candidate.FilePath, indexCfg) {
			continue
		}
		if indexCfg.MaxFileSize > 0 && candidate.FileSize > indexCfg.MaxFileSize {
			continue
		}
		score, matched := aiPathScore(candidate.FilePath, terms, intent)
		if score <= 0 && !aiIntentIsAPIIntegration(intent) && !aiIntentIsCrossService(intent) {
			continue
		}
		candidate.PreScore = score
		candidate.PathTerms = matched
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].PreScore > candidates[j].PreScore })
	if len(candidates) > 160 {
		candidates = candidates[:160]
	}

	repoPath := s.git.repoPath(repo.ID)
	evidence := []aiEvidence{}
	for _, candidate := range candidates {
		data, err := s.git.catFile(ctx, repoPath, candidate.BlobSHA)
		if err != nil || !aiLooksText(data) {
			continue
		}
		content := string(data)
		contentScore, contentTerms := aiContentScore(content, terms, intent)
		score := candidate.PreScore + contentScore
		score = aiAdjustEvidenceScore(candidate.FilePath, score, intent)
		if score <= 0 {
			continue
		}
		score += 12
		snippet, startLine, endLine := aiSnippet(content, terms, aiIntentIsAPIIntegration(intent) || aiPathLooksCode(candidate.FilePath))
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		evidence = append(evidence, aiEvidence{
			Repo: repo,
			Citation: AIMessageCitation{
				RepoID:      repo.ID,
				VersionID:   candidate.VersionID,
				SourceScope: "smart_latest",
				Branch:      candidate.Branch,
				CommitSHA:   candidate.CommitSHA,
				FilePath:    candidate.FilePath,
				LineStart:   startLine,
				LineEnd:     endLine,
				QuoteText:   truncate(snippet, 700),
				Score:       score,
			},
			Content:      snippet,
			MatchedTerms: mergeTerms(candidate.PathTerms, contentTerms),
			Score:        score,
		})
	}
	sort.SliceStable(evidence, func(i, j int) bool { return evidence[i].Score > evidence[j].Score })
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}
	return evidence, nil
}

func (s *Server) searchRepoRefEvidence(ctx context.Context, repo Repository, target aiRefTarget, terms []string, intent string, indexCfg AIIndexConfig) ([]aiEvidence, error) {
	entries, err := s.git.lsTree(ctx, s.git.repoPath(repo.ID), target.CommitSHA, ".")
	if err != nil {
		return nil, err
	}
	contentPathScores, err := s.git.grepPathScores(ctx, s.git.repoPath(repo.ID), target.CommitSHA, terms)
	if err != nil {
		contentPathScores = map[string]float64{}
	}
	candidates := make([]aiFileCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != "blob" || aiShouldSkipPath(entry.Path, indexCfg) {
			continue
		}
		if indexCfg.MaxFileSize > 0 && entry.Size > indexCfg.MaxFileSize {
			continue
		}
		score, matched := aiPathScore(entry.Path, terms, intent)
		if matchLines := contentPathScores[entry.Path]; matchLines > 0 {
			score += min(matchLines, 10) * 4
		}
		if score <= 0 && !aiIntentIsAPIIntegration(intent) && !aiIntentIsCrossService(intent) {
			continue
		}
		candidates = append(candidates, aiFileCandidate{Entry: entry, PreScore: score, Terms: matched})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].PreScore > candidates[j].PreScore })
	if len(candidates) > 160 {
		candidates = candidates[:160]
	}
	var evidence []aiEvidence
	for _, candidate := range candidates {
		data, err := s.git.showFile(ctx, s.git.repoPath(repo.ID), target.CommitSHA, candidate.Entry.Path)
		if err != nil || !aiLooksText(data) {
			continue
		}
		content := string(data)
		contentScore, contentTerms := aiContentScore(content, terms, intent)
		score := candidate.PreScore + contentScore
		score = aiAdjustEvidenceScore(candidate.Entry.Path, score, intent)
		if score <= 0 {
			continue
		}
		matchedTerms := mergeTerms(candidate.Terms, contentTerms)
		snippet, startLine, endLine := aiSnippet(content, terms, aiIntentIsAPIIntegration(intent) || aiPathLooksCode(candidate.Entry.Path))
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		versionID := findAIVersionID(ctx, s.db, repo.ID, target.Branch, candidate.Entry.Path)
		evidence = append(evidence, aiEvidence{
			Repo: repo,
			Citation: AIMessageCitation{
				RepoID:      repo.ID,
				VersionID:   versionID,
				SourceScope: target.SourceScope,
				Branch:      target.Branch,
				CommitSHA:   target.CommitSHA,
				FilePath:    candidate.Entry.Path,
				LineStart:   startLine,
				LineEnd:     endLine,
				QuoteText:   truncate(snippet, 700),
				Score:       score,
			},
			Content:      snippet,
			MatchedTerms: matchedTerms,
			Score:        score,
		})
	}
	sort.SliceStable(evidence, func(i, j int) bool { return evidence[i].Score > evidence[j].Score })
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}
	return evidence, nil
}

func findAIVersionID(ctx context.Context, db *sql.DB, repoID int64, branch, filePath string) int64 {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM doc_versions
		WHERE repo_id = ? AND branch = ? AND file_path = ? AND status IN ('active','renamed','moved') LIMIT 1`,
		repoID, branch, filePath).Scan(&id)
	if err == nil {
		return id
	}
	return 0
}

func filterAIRepos(repos []Repository, scope AIQuestionScope) []Repository {
	if len(scope.RepoIDs) == 0 || scope.RepoMode == "global" && scope.CurrentFile == nil {
		return repos
	}
	allowed := map[int64]struct{}{}
	for _, id := range scope.RepoIDs {
		allowed[id] = struct{}{}
	}
	out := repos[:0]
	for _, repo := range repos {
		if _, ok := allowed[repo.ID]; ok {
			out = append(out, repo)
		}
	}
	return out
}

func aiPathScore(filePath string, terms []string, intent string) (float64, []string) {
	lower := strings.ToLower(filePath)
	score := 0.0
	matched := []string{}
	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(term)) {
			score += 6
			matched = append(matched, term)
		}
	}
	boost := aiCodePathBoost(lower, intent)
	score += boost
	return score, matched
}

func aiCodePathBoost(filePath, intent string) float64 {
	boost := 0.0
	keywords := []string{"router", "route", "controller", "handler", "proto", "openapi", "swagger", "api", "client", "dto", "request", "response", "service", "usecase", "error", "errors", "readme", "agents.md", "claude.md"}
	for _, keyword := range keywords {
		if strings.Contains(filePath, keyword) {
			boost += 2.5
		}
	}
	if strings.HasSuffix(filePath, ".proto") || strings.HasSuffix(filePath, ".go") || strings.HasSuffix(filePath, ".ts") || strings.HasSuffix(filePath, ".js") {
		boost += 1
	}
	if aiIntentIsAPIIntegration(intent) {
		boost *= 1.7
	}
	if aiIntentIsCodePathExplanation(intent) {
		for _, keyword := range []string{"handler", "controller", "service", "usecase", "core/", "proto", "request", "response", "model", "models/", "db/", "dao", "repository", "mysql"} {
			if strings.Contains(filePath, keyword) {
				boost += 4
			}
		}
		if strings.HasSuffix(filePath, ".proto") {
			boost += 4
		}
	}
	if aiIntentIsDatabaseDirectUpdate(intent) {
		for _, keyword := range []string{"model", "models/", "schema", "migration", "migrations", "version/mysql", "sql", "mysql", "db/", "dao", "repository", "gorm"} {
			if strings.Contains(filePath, keyword) {
				boost += 5
			}
		}
		if strings.HasSuffix(filePath, ".sql") {
			boost += 8
		}
	}
	if aiIntentIsTestLookup(intent) && aiPathLooksTest(filePath) {
		boost += 8
	}
	return boost
}

func aiContentScore(content string, terms []string, intent string) (float64, []string) {
	lower := strings.ToLower(content)
	score := 0.0
	matched := []string{}
	for _, term := range terms {
		term = strings.ToLower(term)
		if term == "" {
			continue
		}
		count := strings.Count(lower, term)
		if count > 0 {
			score += float64(min(count, 6)) * 3
			matched = append(matched, term)
		}
	}
	if aiIntentIsAPIIntegration(intent) {
		for _, pattern := range []string{"router.", ".get(", ".post(", ".put(", ".delete(", "handlefunc(", "group(", "rpc ", "service ", "shouldbind", "ctx.request", "requestbody", "openapi"} {
			if strings.Contains(lower, pattern) {
				score += 4
			}
		}
	}
	if aiIntentIsCodePathExplanation(intent) {
		for _, pattern := range []string{"func ", "type ", "rpc ", "service ", "handler", ".update(", ".updates(", ".save(", ".create(", "model(&", ".where(", "where(", "sync", "index", "cache", "event"} {
			if strings.Contains(lower, pattern) {
				score += 4
			}
		}
	}
	if aiIntentIsDatabaseDirectUpdate(intent) {
		for _, pattern := range []string{"create table", "alter table", "tablename()", "table_name", "column:", "gorm:", "model(&", ".where(", "where(", " update ", " set ", " select ", " from "} {
			if strings.Contains(lower, pattern) {
				score += 5
			}
		}
	}
	return score, matched
}

func aiAdjustEvidenceScore(filePath string, score float64, intent string) float64 {
	if aiPathLooksTest(filePath) && !aiIntentIsTestLookup(intent) {
		if aiIntentIsDatabaseDirectUpdate(intent) {
			return score * 0.15
		}
		return score * 0.35
	}
	return score
}

func aiShouldSkipPath(filePath string, cfg AIIndexConfig) bool {
	filePath = normalizeRepoPath(filePath)
	for _, pattern := range cfg.ExcludeGlobs {
		if matchGlob(pattern, filePath) {
			return true
		}
	}
	if strings.Contains(filePath, "/.") && !strings.HasSuffix(filePath, "AGENTS.md") && !strings.HasSuffix(filePath, "CLAUDE.md") {
		return true
	}
	return false
}

func aiPathLooksCode(filePath string) bool {
	switch extension(filePath) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".vue", ".java", ".kt", ".py", ".rb", ".php", ".cs", ".rs", ".proto", ".graphql":
		return true
	default:
		return false
	}
}

func aiPathLooksTest(filePath string) bool {
	lower := strings.ToLower(normalizeRepoPath(filePath))
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.tsx") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, ".spec.tsx") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/testdata/") ||
		strings.Contains(lower, "/fixtures/") ||
		strings.Contains(lower, "/__tests__/")
}

func aiLooksText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	if utf8.Valid(sample) {
		return true
	}
	invalid := 0
	for _, b := range sample {
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			invalid++
		}
	}
	return float64(invalid)/float64(len(sample)) < 0.08
}

func aiSnippet(content string, terms []string, preferCode bool) (string, int, int) {
	lines := strings.Split(content, "\n")
	indexes := map[int]struct{}{}
	lowerTerms := make([]string, 0, len(terms))
	for _, term := range terms {
		if term != "" {
			lowerTerms = append(lowerTerms, strings.ToLower(term))
		}
	}
	type hit struct {
		Index int
		Score float64
	}
	hits := []hit{}
	for i, line := range lines {
		lower := strings.ToLower(line)
		score := aiSnippetLineScore(lower, lowerTerms, preferCode)
		if score <= 0 {
			continue
		}
		hits = append(hits, hit{Index: i, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Index < hits[j].Index
	})
	before, after, maxLines := 2, 3, 18
	if preferCode {
		before, after, maxLines = 3, 6, 28
	}
	for _, item := range hits {
		if preferCode {
			if declaration := aiNearestDeclarationLine(lines, item.Index); declaration >= 0 {
				for j := declaration; j <= min(len(lines)-1, declaration+2); j++ {
					indexes[j] = struct{}{}
				}
			}
		}
		for j := max(0, item.Index-before); j <= min(len(lines)-1, item.Index+after); j++ {
			indexes[j] = struct{}{}
		}
		if len(indexes) >= maxLines {
			break
		}
	}
	if len(indexes) == 0 {
		for i, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			for j := i; j < min(len(lines), i+18); j++ {
				indexes[j] = struct{}{}
			}
			break
		}
	}
	if len(indexes) == 0 {
		return "", 0, 0
	}
	selected := make([]int, 0, len(indexes))
	for index := range indexes {
		selected = append(selected, index)
	}
	sort.Ints(selected)
	var b strings.Builder
	for _, index := range selected {
		line := strings.TrimRight(lines[index], "\r")
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("%d: %s", index+1, line))
		if b.Len() > 3000 {
			break
		}
	}
	return b.String(), selected[0] + 1, selected[len(selected)-1] + 1
}

func aiSnippetLineScore(lower string, lowerTerms []string, preferCode bool) float64 {
	score := 0.0
	for _, term := range lowerTerms {
		count := strings.Count(lower, term)
		if count == 0 {
			continue
		}
		weight := 1.0 + float64(min(len([]rune(term)), 8))/8.0
		score += float64(min(count, 3)) * weight
	}
	if preferCode && aiLineLooksLikeRoute(lower) {
		score += 1.5
	}
	return score
}

func aiNearestDeclarationLine(lines []string, index int) int {
	for i := index; i >= max(0, index-120); i-- {
		if aiLineLooksLikeDeclaration(strings.ToLower(strings.TrimSpace(lines[i]))) {
			return i
		}
	}
	return -1
}

func aiLineLooksLikeDeclaration(lower string) bool {
	for _, prefix := range []string{"func ", "function ", "export function ", "async function ", "def ", "class ", "type ", "interface ", "service ", "rpc "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func aiLineLooksLikeRoute(lower string) bool {
	for _, pattern := range []string{"router.", ".get(", ".post(", ".put(", ".delete(", ".patch(", "handlefunc(", "handle(", "group(", "rpc ", "service ", "@httpmethod", "@httpcontroller", "shouldbind", "ctx.request"} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func aiQueryTerms(question string) []string {
	question = strings.ToLower(question)
	terms := []string{}
	var current []rune
	currentClass := aiTermNone
	flush := func() {
		if len(current) == 0 {
			return
		}
		value := strings.TrimSpace(string(current))
		current = current[:0]
		termClass := currentClass
		currentClass = aiTermNone
		runeLen := len([]rune(value))
		if runeLen < 2 || aiStopTerm(value) {
			return
		}
		if termClass == aiTermHan {
			if runeLen <= 8 {
				terms = append(terms, value)
			}
			return
		}
		terms = appendAIStructuredTerm(terms, value)
	}
	for _, r := range question {
		termClass := aiTermRuneClass(r)
		if termClass == aiTermNone {
			flush()
			continue
		}
		if currentClass != aiTermNone && currentClass != termClass {
			flush()
		}
		currentClass = termClass
		current = append(current, r)
	}
	flush()
	for _, run := range aiHanRuns(question) {
		runes := []rune(run)
		for n := 2; n <= 4 && n <= len(runes); n++ {
			for i := 0; i+n <= len(runes); i++ {
				value := string(runes[i : i+n])
				if !aiStopTerm(value) {
					terms = append(terms, value)
				}
			}
		}
	}
	return uniqueTerms(terms)
}

type aiTermClass int

const (
	aiTermNone aiTermClass = iota
	aiTermHan
	aiTermIdentifier
)

func aiTermRuneClass(r rune) aiTermClass {
	if unicode.In(r, unicode.Han) {
		return aiTermHan
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/' || r == '.' {
		return aiTermIdentifier
	}
	return aiTermNone
}

func appendAIStructuredTerm(terms []string, value string) []string {
	terms = append(terms, value)
	if singular := aiIdentifierSingular(value); singular != "" {
		terms = append(terms, singular)
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '.' || r == '_' || r == '-'
	}) {
		if len([]rune(part)) >= 2 && !aiStopTerm(part) {
			terms = append(terms, part)
			if singular := aiIdentifierSingular(part); singular != "" {
				terms = append(terms, singular)
			}
		}
	}
	return terms
}

func aiIdentifierSingular(value string) string {
	if len(value) < 5 || !strings.HasSuffix(value, "s") || strings.HasSuffix(value, "ss") || strings.HasSuffix(value, "us") || strings.HasSuffix(value, "is") {
		return ""
	}
	return strings.TrimSuffix(value, "s")
}

func aiHanRuns(value string) []string {
	runs := []string{}
	var current []rune
	flush := func() {
		if len(current) >= 2 {
			runs = append(runs, string(current))
		}
		current = current[:0]
	}
	for _, r := range value {
		if unicode.In(r, unicode.Han) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return runs
}

func aiStopTerm(value string) bool {
	switch value {
	case "你好", "您好", "需要", "哪些", "什么", "页面", "怎么", "如何", "the", "and", "for", "with", "in", "of", "to":
		return true
	default:
		return false
	}
}

func uniqueTerms(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

func classifyAIIntent(question string) string {
	q := strings.ToLower(question)
	switch {
	case aiQuestionAsksDatabaseChange(q):
		return "database_change"
	case strings.Contains(q, "单测") || strings.Contains(q, "测试用例") || strings.Contains(q, "unit test") || strings.Contains(q, "fixture"):
		return "test_lookup"
	case strings.Contains(q, "接口") || strings.Contains(q, "参数") || strings.Contains(q, "返回") || strings.Contains(q, "api") || strings.Contains(q, "rpc") || strings.Contains(q, "route"):
		return "api_integration"
	case strings.Contains(q, "影响") || strings.Contains(q, "链路") || strings.Contains(q, "调用"):
		return "cross_service"
	case strings.Contains(q, "分支") || strings.Contains(q, "新接口") || strings.Contains(q, "开发中"):
		return "branch_lookup"
	case aiQuestionAsksChangeGuidance(q):
		return "code_path"
	default:
		return "document_qa"
	}
}

func aiQuestionAsksDatabaseChange(q string) bool {
	hasDatabaseContext := strings.Contains(q, "数据库") || strings.Contains(q, "数据表") || strings.Contains(q, "表名") ||
		strings.Contains(q, "表结构") || strings.Contains(q, "表字段") || strings.Contains(q, "表里") ||
		strings.Contains(q, "表中") || strings.Contains(q, "哪张表") || strings.Contains(q, " 表") ||
		strings.Contains(q, "字段") || strings.Contains(q, "sql") || strings.Contains(q, "mysql") ||
		strings.Contains(q, "database") || strings.Contains(q, "db")
	hasChangeAction := strings.Contains(q, "修改") || strings.Contains(q, "直接改") || strings.Contains(q, "直接修改") ||
		strings.Contains(q, "更新") || strings.Contains(q, "改成") || strings.Contains(q, "写入") ||
		strings.Contains(q, "update")
	return hasChangeAction && (hasDatabaseContext || aiQuestionAsksDataValueChange(q))
}

func aiQuestionAsksDataValueChange(q string) bool {
	hasQuestionShape := strings.Contains(q, "如何") || strings.Contains(q, "怎么") ||
		strings.Contains(q, "怎样") || strings.Contains(q, "在哪") || strings.Contains(q, "哪里")
	hasChangeAction := strings.Contains(q, "修改") || strings.Contains(q, "调整") ||
		strings.Contains(q, "改成") || strings.Contains(q, "变更") || strings.Contains(q, "更新") ||
		strings.Contains(q, "配置") || strings.Contains(q, "update") || strings.Contains(q, "change")
	if !hasQuestionShape || !hasChangeAction {
		return false
	}
	return aiQuestionMentionsDataValue(q)
}

func aiQuestionAsksChangeGuidance(q string) bool {
	hasQuestionShape := strings.Contains(q, "如何") || strings.Contains(q, "怎么") ||
		strings.Contains(q, "怎样") || strings.Contains(q, "在哪") || strings.Contains(q, "哪里")
	hasChangeAction := strings.Contains(q, "修改") || strings.Contains(q, "调整") ||
		strings.Contains(q, "改成") || strings.Contains(q, "变更") || strings.Contains(q, "更新") ||
		strings.Contains(q, "配置") || strings.Contains(q, "update") || strings.Contains(q, "change")
	if !hasQuestionShape || !hasChangeAction {
		return false
	}
	return aiQuestionMentionsDataValue(q)
}

func aiQuestionMentionsDataValue(q string) bool {
	for _, marker := range []string{
		"价格", "价钱", "金额", "基础价", "字段", "配置", "状态", "比例", "费率",
		"price", "amount", "field", "config", "status", "rate",
	} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

func dedupeAIEvidence(items []aiEvidence) []aiEvidence {
	seen := map[string]int{}
	out := make([]aiEvidence, 0, len(items))
	for _, item := range items {
		key := fmt.Sprintf("%d:%s:%s:%d:%d", item.Citation.RepoID, item.Citation.CommitSHA, item.Citation.FilePath, item.Citation.LineStart, item.Citation.LineEnd)
		if existingIndex, ok := seen[key]; ok {
			if out[existingIndex].Citation.SourceScope == "branch_candidate" && item.Citation.SourceScope == "smart_latest" {
				out[existingIndex] = item
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	return out
}

func buildAIServiceCandidates(repos []Repository, evidence []aiEvidence) []AIServiceCandidate {
	repoByID := map[int64]Repository{}
	for _, repo := range repos {
		repoByID[repo.ID] = repo
	}
	grouped := map[int64][]aiEvidence{}
	for _, item := range evidence {
		grouped[item.Citation.RepoID] = append(grouped[item.Citation.RepoID], item)
	}
	var candidates []AIServiceCandidate
	for repoID, items := range grouped {
		repo := repoByID[repoID]
		score := 0.0
		termSet := map[string]struct{}{}
		for _, item := range items {
			score += item.Score
			for _, term := range item.MatchedTerms {
				termSet[term] = struct{}{}
			}
		}
		terms := make([]string, 0, len(termSet))
		for term := range termSet {
			terms = append(terms, term)
		}
		sort.Strings(terms)
		confidence := "low"
		if score >= 80 || len(items) >= 6 {
			confidence = "high"
		} else if score >= 30 || len(items) >= 3 {
			confidence = "medium"
		}
		candidates = append(candidates, AIServiceCandidate{
			RepoID:        repoID,
			RepoName:      repo.Name,
			ServiceName:   repo.Name,
			MatchedTerms:  terms,
			Confidence:    confidence,
			Reason:        aiCandidateReason(terms, items),
			Score:         score,
			EvidenceCount: len(items),
			CreatedAt:     nowString(),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	return candidates
}

func aiCandidateReason(terms []string, items []aiEvidence) string {
	if len(terms) > 0 {
		if len(terms) > 6 {
			terms = terms[:6]
		}
		return "命中关键词：" + strings.Join(terms, "、")
	}
	if len(items) > 0 {
		return "命中代码或文档入口：" + items[0].Citation.FilePath
	}
	return "候选服务"
}

func (s *Server) generateAIQueryTerms(ctx context.Context, cfg AIConfigData, question string, frame *aiTaskFrame) (aiModelResult, []string, error) {
	messages := []aiChatMessage{
		{Role: "system", Content: "你是代码和文档检索前的查询规划器。只返回 JSON，不要回答用户问题。JSON 格式为 {\"terms\":[\"...\"]}。terms 应该是短检索词，优先选择可能出现在代码标识符、接口名、文件名、注释或文档中的词；可以包含从用户问题现场推断出的同义表达、英文标识符写法或缩写。用户询问如何修改、配置或调整某类数据时，优先生成与原问题概念直接对应的通用技术词和字段式写法；当问题明确要求 SQL、数据库、数据表、字段或直接修改数据时，生成可能出现的表名、模型名、字段名、查询条件、单复数和 snake_case/CamelCase 写法，以及 SELECT/UPDATE 等 SQL 检索词。不要加入问题没有依据的具体服务、业务、接口或模块名。"},
		{Role: "user", Content: truncate(question, 1200)},
	}
	result, err := s.callRoutedAIChat(ctx, cfg, messages, 0, 256)
	if err != nil {
		return result, nil, err
	}
	terms := filterAIQueryPlannerTerms(parseAIQueryPlannerTerms(result.Content), question, frame)
	return result, terms, nil
}

func parseAIQueryPlannerTerms(content string) []string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	}
	var parsed struct {
		Terms []string `json:"terms"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil
	}
	cleaned := make([]string, 0, len(parsed.Terms))
	for _, term := range parsed.Terms {
		term = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(term, "\n", " "), "\t", " "))
		if len([]rune(term)) < 2 || len([]rune(term)) > 64 {
			continue
		}
		cleaned = append(cleaned, term)
	}
	terms := uniqueTerms(cleaned)
	if len(terms) > 16 {
		terms = terms[:16]
	}
	return terms
}

func filterAIQueryPlannerTerms(terms []string, question string, frame *aiTaskFrame) []string {
	if len(terms) == 0 {
		return nil
	}
	sourceTerms := aiQueryPlannerSourceTerms(question, frame)
	allowDatabaseCandidates := aiQueryPlannerAllowsDatabaseCandidates(question, frame)
	filtered := make([]string, 0, len(terms))
	for _, term := range terms {
		if aiQueryPlannerTermAllowed(term, sourceTerms) || allowDatabaseCandidates && aiQueryPlannerDatabaseCandidateTermAllowed(term, sourceTerms) {
			filtered = append(filtered, term)
		}
	}
	filtered = uniqueTerms(filtered)
	if len(filtered) > 16 {
		filtered = filtered[:16]
	}
	return filtered
}

func aiQueryPlannerAllowsDatabaseCandidates(question string, frame *aiTaskFrame) bool {
	if frame != nil && aiIntentIsDatabaseDirectUpdate(frame.Intent) {
		return true
	}
	q := strings.ToLower(question)
	return aiQuestionAsksDatabaseChange(q) || aiQuestionAsksDataValueChange(q)
}

func aiQueryPlannerSourceTerms(question string, frame *aiTaskFrame) map[string]struct{} {
	sourceTerms := map[string]struct{}{}
	aiQueryPlannerAddSourceTerms(sourceTerms, question)
	if frame == nil {
		return sourceTerms
	}
	for _, term := range frame.KnownTerms {
		aiQueryPlannerAddSourceTerms(sourceTerms, term)
	}
	for _, term := range frame.GeneratedTerms {
		aiQueryPlannerAddSourceTerms(sourceTerms, term)
	}
	return sourceTerms
}

func aiQueryPlannerAddSourceTerms(sourceTerms map[string]struct{}, value string) {
	add := func(term string) {
		term = aiQueryPlannerNormalizeTerm(term)
		if len([]rune(term)) < 2 || aiStopTerm(term) {
			return
		}
		sourceTerms[term] = struct{}{}
		if singular := aiIdentifierSingular(term); singular != "" {
			sourceTerms[singular] = struct{}{}
		}
	}
	add(value)
	for _, term := range aiQueryTerms(value) {
		add(term)
	}
	words := aiQueryPlannerIdentifierWords(value)
	for _, word := range words {
		add(word)
	}
	for n := 2; n <= 4 && n <= len(words); n++ {
		for i := 0; i+n <= len(words); i++ {
			add(strings.Join(words[i:i+n], ""))
			add(strings.Join(words[i:i+n], "_"))
		}
	}
	if len(words) > 1 && len(words) <= 6 {
		add(strings.Join(words, ""))
		add(strings.Join(words, "_"))
	}
}

func aiQueryPlannerTermAllowed(term string, sourceTerms map[string]struct{}) bool {
	normalized := aiQueryPlannerNormalizeTerm(term)
	if normalized == "" {
		return false
	}
	if _, ok := sourceTerms[normalized]; ok {
		return true
	}
	if aiQueryPlannerGenericTerm(normalized) {
		return true
	}
	components := aiQueryPlannerIdentifierWords(term)
	if len(components) > 1 {
		hasSource := false
		allGeneric := true
		for _, component := range components {
			component = aiQueryPlannerNormalizeTerm(component)
			_, isSource := sourceTerms[component]
			isGeneric := aiQueryPlannerGenericTerm(component)
			if isSource {
				hasSource = true
			}
			if !isGeneric {
				allGeneric = false
			}
			if !isSource && !isGeneric {
				return false
			}
		}
		return hasSource || allGeneric
	}
	return aiQueryPlannerHasGenericAffix(normalized, sourceTerms)
}

func aiQueryPlannerDatabaseCandidateTermAllowed(term string, sourceTerms map[string]struct{}) bool {
	components := aiQueryPlannerIdentifierWords(term)
	if len(components) == 0 {
		return false
	}
	hasDataValueComponent := false
	hasSourceComponent := false
	for _, component := range components {
		component = aiQueryPlannerNormalizeTerm(component)
		if _, ok := sourceTerms[component]; ok {
			hasSourceComponent = true
		}
		if aiQueryPlannerDatabaseDataValueComponent(component) {
			hasDataValueComponent = true
		}
		if aiQueryPlannerActionComponent(component) && !aiQueryPlannerSQLActionComponent(component) {
			return false
		}
	}
	return hasDataValueComponent || hasSourceComponent && aiQueryPlannerLooksDatabaseIdentifier(term) || aiQueryPlannerLooksDatabaseIdentifier(term)
}

func aiQueryPlannerDatabaseDataValueComponent(component string) bool {
	switch component {
	case "price", "prices", "amount", "cents", "cent", "currency", "rate", "ratio", "config", "field", "value", "status":
		return true
	default:
		return false
	}
}

func aiQueryPlannerActionComponent(component string) bool {
	switch component {
	case "batch", "update", "updates", "add", "create", "delete", "remove", "list", "find", "query", "load", "get", "set", "by", "target", "targets", "handler", "request", "response":
		return true
	default:
		return false
	}
}

func aiQueryPlannerSQLActionComponent(component string) bool {
	switch component {
	case "update", "select", "where", "set":
		return true
	default:
		return false
	}
}

func aiQueryPlannerLooksDatabaseIdentifier(term string) bool {
	normalized := aiQueryPlannerNormalizeTerm(term)
	if strings.Contains(normalized, "_") {
		return true
	}
	for _, component := range aiQueryPlannerIdentifierWords(term) {
		if aiQueryPlannerDatabaseDataValueComponent(component) {
			return true
		}
	}
	return false
}

func aiQueryPlannerHasGenericAffix(term string, sourceTerms map[string]struct{}) bool {
	for sourceTerm := range sourceTerms {
		if len([]rune(sourceTerm)) < 3 || aiQueryPlannerGenericTerm(sourceTerm) {
			continue
		}
		for genericTerm := range aiQueryPlannerGenericTermSet {
			if term == sourceTerm+genericTerm || term == genericTerm+sourceTerm {
				return true
			}
		}
	}
	return false
}

func aiQueryPlannerIdentifierWords(value string) []string {
	chunks := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		var current []rune
		flush := func() {
			part := aiQueryPlannerNormalizeTerm(string(current))
			current = current[:0]
			if len([]rune(part)) < 2 || aiStopTerm(part) {
				return
			}
			words = append(words, part)
		}
		runes := []rune(chunk)
		for i, r := range runes {
			if len(current) > 0 {
				prev := current[len(current)-1]
				nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsDigit(prev) != unicode.IsDigit(r) ||
					(unicode.IsLower(prev) && unicode.IsUpper(r)) ||
					(unicode.IsUpper(prev) && unicode.IsUpper(r) && nextIsLower) {
					flush()
				}
			}
			current = append(current, r)
		}
		if len(current) > 0 {
			flush()
		}
	}
	return words
}

func aiQueryPlannerNormalizeTerm(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func aiQueryPlannerGenericTerm(value string) bool {
	value = aiQueryPlannerNormalizeTerm(value)
	if _, ok := aiQueryPlannerGenericTermSet[value]; ok {
		return true
	}
	if singular := aiIdentifierSingular(value); singular != "" {
		_, ok := aiQueryPlannerGenericTermSet[singular]
		return ok
	}
	return false
}

var aiQueryPlannerGenericTermSet = map[string]struct{}{
	"api":          {},
	"amount":       {},
	"base":         {},
	"body":         {},
	"branch":       {},
	"cache":        {},
	"client":       {},
	"code":         {},
	"column":       {},
	"commit":       {},
	"compensation": {},
	"config":       {},
	"contract":     {},
	"controller":   {},
	"cents":        {},
	"currency":     {},
	"dao":          {},
	"database":     {},
	"db":           {},
	"delete":       {},
	"discount":     {},
	"doc":          {},
	"docs":         {},
	"dto":          {},
	"endpoint":     {},
	"error":        {},
	"event":        {},
	"evidence":     {},
	"field":        {},
	"file":         {},
	"find":         {},
	"get":          {},
	"grpc":         {},
	"handler":      {},
	"http":         {},
	"index":        {},
	"insert":       {},
	"job":          {},
	"list":         {},
	"message":      {},
	"method":       {},
	"migration":    {},
	"model":        {},
	"param":        {},
	"parameter":    {},
	"path":         {},
	"post":         {},
	"price":        {},
	"proto":        {},
	"put":          {},
	"query":        {},
	"read":         {},
	"repo":         {},
	"repository":   {},
	"request":      {},
	"response":     {},
	"retry":        {},
	"route":        {},
	"router":       {},
	"rpc":          {},
	"sale":         {},
	"schema":       {},
	"select":       {},
	"sell":         {},
	"service":      {},
	"sql":          {},
	"status":       {},
	"struct":       {},
	"table":        {},
	"update":       {},
	"validation":   {},
	"where":        {},
	"write":        {},
}

func (s *Server) generateAIAnswer(ctx context.Context, cfg AIConfigVersion, question string, retrieval aiRetrievalResult) aiModelResult {
	start := time.Now()
	if !cfg.Config.Enabled {
		return aiModelResult{
			Content:        localEvidenceAnswer(question, retrieval, false),
			ProviderName:   "local-retrieval",
			Model:          "none",
			ModelRouteJSON: encodeJSON(map[string]any{"mode": "local", "reason": "ai_disabled"}),
			LatencyMS:      int(time.Since(start).Milliseconds()),
		}
	}
	result, err := s.callRoutedAIModel(ctx, cfg.Config, question, retrieval)
	if err != nil {
		return aiModelFallbackForError(question, retrieval, start, err)
	}
	result.LatencyMS = int(time.Since(start).Milliseconds())
	return result
}

func aiModelFallbackForError(question string, retrieval aiRetrievalResult, start time.Time, err error) aiModelResult {
	failover := map[string]any{"error": sanitizeProviderError(err.Error())}
	var routeErr aiRouteError
	if errors.As(err, &routeErr) {
		failover = map[string]any{
			"attempt_order":          routeErr.AttemptOrder,
			"succeeded_provider_key": "",
			"error":                  sanitizeProviderError(routeErr.Message),
			"failures":               routeErr.Failures,
		}
	}
	return aiModelResult{
		Content:        localEvidenceAnswer(question, retrieval, true),
		ProviderName:   "local-retrieval",
		Model:          "none",
		ModelRouteJSON: encodeJSON(map[string]any{"mode": "local_fallback", "reason": "provider_error"}),
		LatencyMS:      int(time.Since(start).Milliseconds()),
		FailoverJSON:   encodeJSON(failover),
	}
}

func (s *Server) callRoutedAIModel(ctx context.Context, cfg AIConfigData, question string, retrieval aiRetrievalResult) (aiModelResult, error) {
	return s.callRoutedAIChat(ctx, cfg, buildAIChatMessages(question, retrieval), 0.2, 0)
}

func (s *Server) callRoutedAIChat(ctx context.Context, cfg AIConfigData, messages []aiChatMessage, temperature float64, maxTokens int) (aiModelResult, error) {
	taskClass := cfg.Chat.Routing.DefaultTaskClass
	if taskClass == "" {
		taskClass = "standard"
	}
	failures := []map[string]any{}
	attemptOrder := []string{}
	visited := map[string]struct{}{}
	visitedProviders := map[string]struct{}{}
	for taskClass != "" {
		if _, ok := visited[taskClass]; ok {
			break
		}
		visited[taskClass] = struct{}{}
		route, ok := cfg.Chat.Routing.TaskClasses[taskClass]
		providers := aiProvidersForRoute(cfg.Chat.Providers, route.Providers)
		if !ok || len(providers) == 0 {
			providers = aiProvidersByPriority(cfg.Chat.Providers)
		}
		for _, provider := range providers {
			if _, seen := visitedProviders[provider.ProviderKey]; seen {
				continue
			}
			visitedProviders[provider.ProviderKey] = struct{}{}
			attemptOrder = append(attemptOrder, provider.ProviderKey)
			if provider.APIKeySecretID <= 0 {
				failures = append(failures, aiProviderFailure(provider, "missing_secret"))
				continue
			}
			apiKey, err := s.decryptAISecret(ctx, provider.APIKeySecretID)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return aiModelResult{}, err
				}
				failures = append(failures, aiProviderFailure(provider, sanitizeProviderError(err.Error())))
				continue
			}
			resp, err := s.callOpenAICompatibleWithOptions(ctx, provider, apiKey, messages, temperature, maxTokens)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return aiModelResult{}, err
				}
				failures = append(failures, aiProviderFailure(provider, sanitizeProviderError(err.Error())))
				continue
			}
			content := ""
			if len(resp.Choices) > 0 {
				content = strings.TrimSpace(resp.Choices[0].Message.Content)
			}
			if content == "" {
				failures = append(failures, aiProviderFailure(provider, "empty response"))
				continue
			}
			return aiModelResult{
				Content:          content,
				ProviderName:     aiProviderDisplayName(provider),
				Model:            provider.Model,
				ModelRouteJSON:   encodeJSON(map[string]any{"task_class": taskClass, "provider_key": provider.ProviderKey, "provider": aiProviderDisplayName(provider), "model": provider.Model}),
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				FailoverJSON: encodeJSON(map[string]any{
					"attempt_order":          attemptOrder,
					"succeeded_provider_key": provider.ProviderKey,
					"failures":               failures,
				}),
			}, nil
		}
		taskClass = route.FallbackTaskClass
	}
	return aiModelResult{}, aiRouteError{Message: "no AI provider completed successfully", AttemptOrder: attemptOrder, Failures: failures}
}

func (s *Server) callRoutedAIModelStream(ctx context.Context, cfg AIConfigData, question string, retrieval aiRetrievalResult, callbacks aiStreamModelCallbacks) (aiModelResult, error) {
	taskClass := cfg.Chat.Routing.DefaultTaskClass
	if taskClass == "" {
		taskClass = "standard"
	}
	messages := buildAIChatMessages(question, retrieval)
	failures := []map[string]any{}
	attemptOrder := []string{}
	visited := map[string]struct{}{}
	visitedProviders := map[string]struct{}{}
	attempt := 0
	for taskClass != "" {
		if _, ok := visited[taskClass]; ok {
			break
		}
		visited[taskClass] = struct{}{}
		route, ok := cfg.Chat.Routing.TaskClasses[taskClass]
		providers := aiProvidersForRoute(cfg.Chat.Providers, route.Providers)
		if !ok || len(providers) == 0 {
			providers = aiProvidersByPriority(cfg.Chat.Providers)
		}
		for _, provider := range providers {
			if _, seen := visitedProviders[provider.ProviderKey]; seen {
				continue
			}
			visitedProviders[provider.ProviderKey] = struct{}{}
			attempt++
			attemptOrder = append(attemptOrder, provider.ProviderKey)
			event := aiStreamProviderAttemptEvent{
				Attempt:     attempt,
				TaskClass:   taskClass,
				ProviderKey: provider.ProviderKey,
				Provider:    aiProviderDisplayName(provider),
				Model:       provider.Model,
				Priority:    provider.Priority,
				Status:      "started",
			}
			if callbacks.ProviderAttempt != nil {
				if err := callbacks.ProviderAttempt(event); err != nil {
					return aiModelResult{}, err
				}
			}
			if provider.APIKeySecretID <= 0 {
				failure := aiProviderFailure(provider, "missing_secret")
				failures = append(failures, failure)
				event.Status = "failed"
				event.Error = "missing_secret"
				if callbacks.ProviderAttempt != nil {
					if err := callbacks.ProviderAttempt(event); err != nil {
						return aiModelResult{}, err
					}
				}
				continue
			}
			apiKey, err := s.decryptAISecret(ctx, provider.APIKeySecretID)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return aiModelResult{}, err
				}
				safeErr := sanitizeProviderError(err.Error())
				failures = append(failures, aiProviderFailure(provider, safeErr))
				event.Status = "failed"
				event.Error = safeErr
				if callbacks.ProviderAttempt != nil {
					if err := callbacks.ProviderAttempt(event); err != nil {
						return aiModelResult{}, err
					}
				}
				continue
			}
			resp, err := s.callOpenAICompatibleStream(ctx, provider, apiKey, messages, callbacks.AnswerDelta)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return aiModelResult{
						Content:      resp.Content,
						ProviderName: aiProviderDisplayName(provider),
						Model:        provider.Model,
						ModelRouteJSON: encodeJSON(map[string]any{
							"task_class":   taskClass,
							"provider_key": provider.ProviderKey,
							"provider":     aiProviderDisplayName(provider),
							"model":        provider.Model,
						}),
						PromptTokens:     resp.PromptTokens,
						CompletionTokens: resp.CompletionTokens,
						FailoverJSON: encodeJSON(map[string]any{
							"attempt_order":          attemptOrder,
							"succeeded_provider_key": "",
							"failures":               failures,
						}),
					}, err
				}
				safeErr := sanitizeProviderError(err.Error())
				event.Status = "failed"
				event.Error = safeErr
				if callbacks.ProviderAttempt != nil {
					if emitErr := callbacks.ProviderAttempt(event); emitErr != nil {
						return aiModelResult{}, emitErr
					}
				}
				if strings.TrimSpace(resp.Content) != "" {
					return aiModelResult{
						Content:      resp.Content,
						ProviderName: aiProviderDisplayName(provider),
						Model:        provider.Model,
						ModelRouteJSON: encodeJSON(map[string]any{
							"task_class":   taskClass,
							"provider_key": provider.ProviderKey,
							"provider":     aiProviderDisplayName(provider),
							"model":        provider.Model,
						}),
						PromptTokens:     resp.PromptTokens,
						CompletionTokens: resp.CompletionTokens,
						FailoverJSON: encodeJSON(map[string]any{
							"attempt_order":          attemptOrder,
							"succeeded_provider_key": "",
							"failures":               append(failures, aiProviderFailure(provider, safeErr)),
						}),
					}, aiStreamPartialError{Message: safeErr}
				}
				failures = append(failures, aiProviderFailure(provider, safeErr))
				continue
			}
			content := strings.TrimSpace(resp.Content)
			if content == "" {
				safeErr := "empty response"
				failures = append(failures, aiProviderFailure(provider, safeErr))
				event.Status = "failed"
				event.Error = safeErr
				if callbacks.ProviderAttempt != nil {
					if err := callbacks.ProviderAttempt(event); err != nil {
						return aiModelResult{}, err
					}
				}
				continue
			}
			event.Status = "succeeded"
			if callbacks.ProviderAttempt != nil {
				if err := callbacks.ProviderAttempt(event); err != nil {
					return aiModelResult{}, err
				}
			}
			return aiModelResult{
				Content:          resp.Content,
				ProviderName:     aiProviderDisplayName(provider),
				Model:            provider.Model,
				ModelRouteJSON:   encodeJSON(map[string]any{"task_class": taskClass, "provider_key": provider.ProviderKey, "provider": aiProviderDisplayName(provider), "model": provider.Model}),
				PromptTokens:     resp.PromptTokens,
				CompletionTokens: resp.CompletionTokens,
				FailoverJSON: encodeJSON(map[string]any{
					"attempt_order":          attemptOrder,
					"succeeded_provider_key": provider.ProviderKey,
					"failures":               failures,
				}),
			}, nil
		}
		taskClass = route.FallbackTaskClass
	}
	return aiModelResult{}, aiRouteError{Message: "no AI provider completed successfully", AttemptOrder: attemptOrder, Failures: failures}
}

func aiProvidersForRoute(providers []AIProvider, names []string) []AIProvider {
	byName := map[string]AIProvider{}
	for _, provider := range providers {
		byName[provider.ProviderKey] = provider
		byName[provider.Name] = provider
	}
	var out []AIProvider
	for _, name := range names {
		if provider, ok := byName[name]; ok {
			if aiProviderRoutable(provider) {
				out = append(out, provider)
			}
		}
	}
	return out
}

func aiProvidersByPriority(providers []AIProvider) []AIProvider {
	sorted := append([]AIProvider(nil), providers...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority == sorted[j].Priority {
			return sorted[i].ProviderKey < sorted[j].ProviderKey
		}
		return sorted[i].Priority < sorted[j].Priority
	})
	out := make([]AIProvider, 0, len(sorted))
	for _, provider := range sorted {
		if aiProviderRoutable(provider) {
			out = append(out, provider)
		}
	}
	return out
}

func aiProviderFailure(provider AIProvider, message string) map[string]any {
	return map[string]any{
		"provider_key": provider.ProviderKey,
		"provider":     aiProviderDisplayName(provider),
		"model":        provider.Model,
		"priority":     provider.Priority,
		"error":        sanitizeProviderError(message),
	}
}

func (s *Server) callOpenAICompatible(ctx context.Context, provider AIProvider, apiKey string, messages []aiChatMessage) (aiChatCompletionResponse, error) {
	return s.callOpenAICompatibleWithOptions(ctx, provider, apiKey, messages, 0.2, 0)
}

func (s *Server) callOpenAICompatibleWithOptions(ctx context.Context, provider AIProvider, apiKey string, messages []aiChatMessage, temperature float64, maxTokens int) (aiChatCompletionResponse, error) {
	endpoint, err := openAICompatibleChatURL(provider.BaseURL)
	if err != nil {
		return aiChatCompletionResponse{}, err
	}
	payload := aiChatCompletionRequest{Model: provider.Model, Messages: messages, Temperature: temperature, MaxTokens: maxTokens}
	raw, err := json.Marshal(payload)
	if err != nil {
		return aiChatCompletionResponse{}, err
	}
	timeout := time.Duration(provider.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return aiChatCompletionResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return aiChatCompletionResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return aiChatCompletionResponse{}, fmt.Errorf("provider returned %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var parsed aiChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return aiChatCompletionResponse{}, err
	}
	return parsed, nil
}

func (s *Server) callOpenAICompatibleStream(ctx context.Context, provider AIProvider, apiKey string, messages []aiChatMessage, onDelta func(string) error) (aiChatCompletionStreamResult, error) {
	endpoint, err := openAICompatibleChatURL(provider.BaseURL)
	if err != nil {
		return aiChatCompletionStreamResult{}, err
	}
	payload := aiChatCompletionRequest{Model: provider.Model, Messages: messages, Temperature: 0.2, Stream: true}
	raw, err := json.Marshal(payload)
	if err != nil {
		return aiChatCompletionStreamResult{}, err
	}
	timeout := time.Duration(provider.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return aiChatCompletionStreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return aiChatCompletionStreamResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return aiChatCompletionStreamResult{}, fmt.Errorf("provider returned %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var result aiChatCompletionStreamResult
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return result, nil
		}
		var chunk aiChatCompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return result, err
		}
		if chunk.Usage.PromptTokens > 0 {
			result.PromptTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			result.CompletionTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			result.Content += choice.Delta.Content
			if onDelta != nil {
				if err := onDelta(choice.Delta.Content); err != nil {
					return result, err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Server) testOpenAICompatibleProvider(ctx context.Context, provider AIProvider, apiKey string, timeoutSeconds int) (aiProviderTestSummary, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	provider.RequestTimeoutSeconds = timeoutSeconds
	start := time.Now()
	resp, err := s.callOpenAICompatibleWithOptions(ctx, provider, apiKey, []aiChatMessage{
		{Role: "system", Content: "Return only the text ok."},
		{Role: "user", Content: "ping"},
	}, 0, 64)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return aiProviderTestSummary{Status: "fail", Message: providerTestFailureMessage(err.Error()), TestedAt: nowString(), LatencyMS: latency}, err
	}
	if len(resp.Choices) == 0 {
		return aiProviderTestSummary{Status: "fail", Message: "供应商响应缺少 choices", TestedAt: nowString(), LatencyMS: latency}, errBadRequest("provider returned no choices")
	}
	return aiProviderTestSummary{Status: "pass", Message: "连接正常", TestedAt: nowString(), LatencyMS: latency}, nil
}

func (s *Server) aiProviderTestAPIKey(ctx context.Context, req aiProviderTestRequest) (string, error) {
	if req.APIKey != "" {
		return req.APIKey, nil
	}
	if req.APIKeySecretID > 0 {
		return s.decryptAISecret(ctx, req.APIKeySecretID)
	}
	if req.ProviderKey == "" {
		return "", errBadRequest("api_key is required")
	}
	active, err := ensureActiveAIConfig(ctx, s.db)
	if err != nil {
		return "", err
	}
	editable, _, hasEditable, err := loadEditableAIConfig(ctx, s.db)
	if err != nil {
		return "", err
	}
	cfg := active.Config
	if hasEditable && editable != nil {
		cfg = editable.Config
	}
	provider, ok := findAIProviderByKey(cfg, req.ProviderKey)
	if !ok {
		return "", errNotFound("provider not found")
	}
	if provider.APIKeySecretID <= 0 {
		return "", errBadRequest("api_key is required")
	}
	return s.decryptAISecret(ctx, provider.APIKeySecretID)
}

func findAIProviderByKey(cfg AIConfigData, providerKey string) (AIProvider, bool) {
	for _, provider := range cfg.Chat.Providers {
		if provider.ProviderKey == providerKey || provider.Name == providerKey {
			return provider, true
		}
	}
	return AIProvider{}, false
}

func openAICompatibleChatURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", errBadRequest("provider base_url is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", errBadRequest("invalid provider base_url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errBadRequest("invalid provider base_url")
	}
	if u.Host == "" {
		return "", errBadRequest("invalid provider base_url")
	}
	if strings.HasSuffix(u.Path, "/chat/completions") {
		return u.String(), nil
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	return u.String(), nil
}

var (
	secretTokenPattern                    = regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`)
	authHeaderPattern                     = regexp.MustCompile(`(?i)authorization[^\n\r,]*`)
	bearerTokenPattern                    = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	whitespacePattern                     = regexp.MustCompile(`\s+`)
	diagnosticsSummaryAuthHeaderPattern   = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*["']?(?:(?:bearer|basic|token)\s+)?[A-Za-z0-9._~+/=:-]+`)
	diagnosticsSummaryBearerPattern       = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	diagnosticsSummaryAPIKeyWordPattern   = regexp.MustCompile(`(?i)api[\s_-]*key`)
	diagnosticsSummarySensitiveKVPattern  = regexp.MustCompile(`(?i)\b(?:api[\s_-]*key|secret|access[\s_-]*token|refresh[\s_-]*token|id[\s_-]*token|auth[\s_-]*token|token)\s*[:=]\s*["']?[^"',\s}]+`)
	diagnosticsSummarySecretPhrasePattern = regexp.MustCompile(`(?i)\bsecret\s+[A-Za-z0-9._~+/=-]+`)
	diagnosticsSummaryJWTPattern          = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

func sanitizeProviderError(message string) string {
	for _, pattern := range []*regexp.Regexp{
		diagnosticsSummaryAuthHeaderPattern,
		diagnosticsSummaryBearerPattern,
		diagnosticsSummarySensitiveKVPattern,
		diagnosticsSummaryAPIKeyWordPattern,
		diagnosticsSummarySecretPhrasePattern,
		diagnosticsSummaryJWTPattern,
	} {
		message = pattern.ReplaceAllString(message, "[redacted]")
	}
	message = authHeaderPattern.ReplaceAllString(message, "[redacted]")
	message = bearerTokenPattern.ReplaceAllString(message, "[redacted]")
	message = secretTokenPattern.ReplaceAllString(message, "sk-[redacted]")
	message = strings.TrimSpace(whitespacePattern.ReplaceAllString(message, " "))
	return truncate(message, 500)
}

func providerTestFailureMessage(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") {
		return "供应商返回 401，请检查 API Key"
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") {
		return "连接供应商超时"
	}
	return "供应商连接测试失败"
}

func localNoEvidenceAnswer(question string) string {
	return "未在已扫描的 Git 内容中找到可以支撑回答的依据。\n\n问题：" + question + "\n\n建议先确认相关仓库已经在 DocHarbor 中启用并完成扫描；如果问题涉及源码接口，请确保该仓库的 bare mirror 已完成同步。"
}

func localEvidenceAnswer(question string, retrieval aiRetrievalResult, modelFailed bool) string {
	var b strings.Builder
	b.WriteString("结论\n\n")
	if modelFailed {
		b.WriteString("下面是 DocHarbor 后端根据只读 Git 检索得到的候选证据摘要。")
	} else {
		b.WriteString("下面是 DocHarbor 后端根据只读 Git 检索得到的候选证据摘要。")
	}
	b.WriteString("这些内容可用于继续追问或辅助确认代码事实。\n\n")
	b.WriteString("可能涉及的服务\n\n")
	if len(retrieval.ServiceCandidates) == 0 {
		b.WriteString("- 未形成明确候选服务。\n")
	} else {
		for _, candidate := range retrieval.ServiceCandidates {
			fmt.Fprintf(&b, "- %s：%s，证据 %d 条，%s。\n", candidate.ServiceName, candidate.Confidence, candidate.EvidenceCount, candidate.Reason)
		}
	}
	if len(retrieval.Evidence) > 0 {
		b.WriteString("\n高信号证据摘录\n\n")
		limit := min(len(retrieval.Evidence), 5)
		for i := 0; i < limit; i++ {
			evidence := retrieval.Evidence[i]
			c := evidence.Citation
			label := "智能最新"
			if c.SourceScope == "branch_candidate" {
				label = "功能分支候选"
			}
			snippet := strings.TrimSpace(evidence.Content)
			if snippet == "" {
				snippet = strings.TrimSpace(c.QuoteText)
			}
			snippet = truncate(snippet, 900)
			if snippet == "" {
				continue
			}
			fmt.Fprintf(&b, "- [C%d] %s / %s，%s，%s:%d-%d\n\n```text\n%s\n```\n\n",
				i+1, evidence.Repo.Name, c.Branch, label, c.FilePath, c.LineStart, c.LineEnd, snippet)
		}
	}
	b.WriteString("\n引用来源\n\n")
	for i, evidence := range retrieval.Evidence {
		c := evidence.Citation
		label := "智能最新"
		if c.SourceScope == "branch_candidate" {
			label = "功能分支候选"
		}
		fmt.Fprintf(&b, "- [C%d] %s / %s @ %s，%s，%s:%d-%d\n", i+1, evidence.Repo.Name, c.Branch, shortSHA(c.CommitSHA), label, c.FilePath, c.LineStart, c.LineEnd)
	}
	if retrieval.ContractCoverage != nil && len(retrieval.ContractCoverage.MissingRequired) > 0 {
		b.WriteString("\n未确认项\n\n")
		fmt.Fprintf(&b, "- 已达到当前检索轮次上限后仍缺 required 证据：%s。当前本地摘要不把操作步骤或接口结论写成确定结论。\n", strings.Join(retrieval.ContractCoverage.MissingRequired, "、"))
	}
	if retrieval.Intent == "api_integration" {
		b.WriteString("\n未确认项\n\n")
		b.WriteString("- 请求参数、响应字段、错误码和领域约束需要模型读取代码证据链后再归并；当前本地摘要不把这些字段写成确定结论。\n")
	}
	return b.String()
}

func countCodeEvidence(citations []AIMessageCitation) int {
	count := 0
	for _, citation := range citations {
		ext := extension(citation.FilePath)
		switch ext {
		case ".go", ".ts", ".js", ".proto", ".java", ".py", ".tsx", ".jsx":
			count++
		}
	}
	return count
}

func aiTitleFromQuestion(question string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return "新的 AI 问答"
	}
	runes := []rune(question)
	if len(runes) > 24 {
		return string(runes[:24]) + "..."
	}
	return question
}

func mergeTerms(values ...[]string) []string {
	var merged []string
	for _, list := range values {
		merged = append(merged, list...)
	}
	return uniqueTerms(merged)
}

func truncate(value string, n int) string {
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n]) + "..."
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
