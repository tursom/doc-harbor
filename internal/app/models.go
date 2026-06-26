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
