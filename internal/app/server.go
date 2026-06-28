package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	cfg     Config
	db      *sql.DB
	git     *Git
	scanner *Scanner
}

func NewServer(cfg Config) (*Server, error) {
	db, err := openDB(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	git := newGit(cfg)
	return &Server{
		cfg:     cfg,
		db:      db,
		git:     git,
		scanner: newScanner(db, git),
	}, nil
}

func (s *Server) Close() error {
	return s.db.Close()
}

func (s *Server) StartScheduler(ctx context.Context) {
	s.scanner.StartScheduler(ctx, s.cfg.SchedulerPollInterval)
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/repos", s.handleRepos)
	mux.HandleFunc("/api/repos/", s.handleRepoSubroutes)
	mux.HandleFunc("/api/ai", s.handleAIRoot)
	mux.HandleFunc("/api/ai/", s.handleAISubroutes)
	mux.HandleFunc("/api/webhooks/github/secret", s.handleGitHubWebhookSecret)
	mux.HandleFunc("/api/webhooks/github/", s.handleGitHubWebhook)
	mux.Handle("/", s.webHandler())
	return recoverMiddleware(mux)
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": "DocHarbor"})
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		repos, err := listRepositories(r.Context(), s.db)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": repos})
	case http.MethodPost:
		var repo Repository
		if err := decodeBody(r.Body, &repo); err != nil {
			writeError(w, err)
			return
		}
		if err := validateRepository(repo); err != nil {
			writeError(w, err)
			return
		}
		if err := s.git.validateRepoURL(repo.RepoURL); err != nil {
			writeError(w, err)
			return
		}
		created, err := createRepository(r.Context(), s.db, repo)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRepoSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/repos/"))
	if len(parts) == 0 {
		writeError(w, errNotFound("not found"))
		return
	}
	repoID, err := parseID(parts[0])
	if err != nil {
		writeError(w, err)
		return
	}
	if len(parts) == 1 {
		s.handleRepo(w, r, repoID)
		return
	}

	switch parts[1] {
	case "scan":
		s.handleScan(w, r, repoID)
	case "scan-runs":
		s.handleScanRuns(w, r, repoID)
	case "branches":
		s.handleBranches(w, r, repoID)
	case "tree":
		s.handleTree(w, r, repoID)
	case "files":
		s.handleFiles(w, r, repoID)
	case "search":
		s.handleSearch(w, r, repoID)
	case "documents":
		s.handleDocuments(w, r, repoID, parts[2:])
	case "versions":
		s.handleVersions(w, r, repoID, parts[2:])
	case "history":
		s.handleHistory(w, r, repoID)
	case "commits":
		s.handleCommits(w, r, repoID, parts[2:])
	case "path-events":
		s.handlePathEvents(w, r, repoID)
	case "blob":
		s.handleBlob(w, r, repoID, len(parts) > 2 && parts[2] == "download")
	default:
		writeError(w, errNotFound("not found"))
	}
}

func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request, repoID int64) {
	switch r.Method {
	case http.MethodGet:
		repo, err := getRepository(r.Context(), s.db, repoID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, repo)
	case http.MethodPatch:
		var repo Repository
		if err := decodeBody(r.Body, &repo); err != nil {
			writeError(w, err)
			return
		}
		if repo.RepoURL != "" {
			if err := s.git.validateRepoURL(repo.RepoURL); err != nil {
				writeError(w, err)
				return
			}
		}
		updated, err := updateRepository(r.Context(), s.db, repoID, repo)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := disableRepository(r.Context(), s.db, repoID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	run, err := s.scanner.Scan(r.Context(), repoID, "manual")
	if err != nil && run.ID == 0 {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleGitHubWebhookSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	secret := s.cfg.GitHubWebhookSecret
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": secret != "",
		"secret":     secret,
	})
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.GitHubWebhookSecret == "" {
		writeError(w, errUnavailable("github webhook secret is not configured"))
		return
	}

	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/webhooks/github/"))
	if len(parts) != 1 {
		writeError(w, errNotFound("not found"))
		return
	}
	repoID, err := parseID(parts[0])
	if err != nil {
		writeError(w, err)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeError(w, errBadRequest("invalid request body: "+err.Error()))
		return
	}
	defer r.Body.Close()

	if !verifyGitHubSignature(s.cfg.GitHubWebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeError(w, errUnauthorized("invalid github webhook signature"))
		return
	}

	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	switch event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"event":   "ping",
			"repo_id": repoID,
		})
	case "push":
		repo, err := getRepository(r.Context(), s.db, repoID)
		if err != nil {
			writeError(w, err)
			return
		}
		if !repo.Enabled {
			writeError(w, errBadRequest("repository disabled"))
			return
		}
		go s.runGitHubWebhookScan(repoID, deliveryID)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted": true,
			"event":    "push",
			"repo_id":  repoID,
		})
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted": true,
			"ignored":  true,
			"event":    event,
			"repo_id":  repoID,
		})
	}
}

func (s *Server) runGitHubWebhookScan(repoID int64, deliveryID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if _, err := s.scanner.Scan(ctx, repoID, "github_webhook"); err != nil {
		if isAppErrorStatus(err, http.StatusConflict) {
			log.Printf("github webhook scan skipped: repo_id=%d delivery=%s error=%v", repoID, deliveryID, err)
			return
		}
		log.Printf("github webhook scan failed: repo_id=%d delivery=%s error=%v", repoID, deliveryID, err)
	}
}

func verifyGitHubSignature(secret string, body []byte, signature string) bool {
	const prefix = "sha256="
	if secret == "" || !strings.HasPrefix(signature, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

func isAppErrorStatus(err error, status int) bool {
	var appErr appError
	return errors.As(err, &appErr) && appErr.Status == status
}

func (s *Server) handleScanRuns(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	limit := cleanLimit(r.URL.Query().Get("limit"), 50, 200)
	runs, err := listScanRuns(r.Context(), s.db, repoID, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	branches, err := listBranches(r.Context(), s.db, repoID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": branches})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "latest"
	}
	entries, err := treeForView(r.Context(), s.db, repoID, view, r.URL.Query().Get("branch"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "latest"
	}
	dir := normalizeRepoPath(r.URL.Query().Get("dir"))
	if dir == "" {
		dir = "."
	}
	var entries []FileEntry
	var err error
	if view == "branch" {
		branch := r.URL.Query().Get("branch")
		if branch == "" {
			writeError(w, errBadRequest("branch is required"))
			return
		}
		entries, err = listBranchFiles(r.Context(), s.db, repoID, branch, dir)
	} else {
		entries, err = listLatestFiles(r.Context(), s.db, repoID, dir)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := searchDocuments(r.Context(), s.db, repoID, r.URL.Query().Get("q"), cleanLimit(r.URL.Query().Get("limit"), 30, 100))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDocuments(w http.ResponseWriter, r *http.Request, repoID int64, parts []string) {
	if len(parts) == 0 {
		writeError(w, errNotFound("not found"))
		return
	}
	documentID, err := parseID(parts[0])
	if err != nil {
		writeError(w, err)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		doc, err := getDocument(r.Context(), s.db, repoID, documentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, doc)
		return
	}
	switch parts[1] {
	case "versions":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		versions, err := listDocumentVersions(r.Context(), s.db, repoID, documentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": versions})
	case "history":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		history, err := docHistory(r.Context(), s.db, repoID, documentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, history)
	default:
		writeError(w, errNotFound("not found"))
	}
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request, repoID int64, parts []string) {
	if len(parts) < 2 {
		writeError(w, errNotFound("not found"))
		return
	}
	versionID, err := parseID(parts[0])
	if err != nil {
		writeError(w, err)
		return
	}
	switch parts[1] {
	case "content":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.writeVersionContent(w, r, repoID, versionID)
	case "download":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.writeVersionDownload(w, r, repoID, versionID)
	default:
		writeError(w, errNotFound("not found"))
	}
}

func (s *Server) writeVersionContent(w http.ResponseWriter, r *http.Request, repoID, versionID int64) {
	v, err := getVersion(r.Context(), s.db, repoID, versionID)
	if err != nil {
		writeError(w, err)
		return
	}
	versions, _ := listDocumentVersions(r.Context(), s.db, repoID, v.DocumentID)
	content := FileContent{
		VersionID:       v.ID,
		DocumentID:      v.DocumentID,
		RepoID:          v.RepoID,
		Branch:          v.Branch,
		FilePath:        v.FilePath,
		Title:           v.Title,
		Extension:       v.Extension,
		MimeType:        v.MimeType,
		FileSize:        v.FileSize,
		BlobSHA:         v.BlobSHA,
		SourceCommitSHA: v.HeadCommitSHA,
		LastCommitTime:  v.LastCommitTime,
		Previewable:     v.Previewable,
		DownloadEnabled: v.DownloadEnabled,
		TooLarge:        !v.Previewable,
		Versions:        versions,
	}
	if v.Previewable {
		data, err := s.git.catFile(r.Context(), s.git.repoPath(repoID), v.BlobSHA)
		if err != nil {
			writeError(w, err)
			return
		}
		content.Content = string(data)
	}
	writeJSON(w, http.StatusOK, content)
}

func (s *Server) writeVersionDownload(w http.ResponseWriter, r *http.Request, repoID, versionID int64) {
	v, err := getVersion(r.Context(), s.db, repoID, versionID)
	if err != nil {
		writeError(w, err)
		return
	}
	data, err := s.git.catFile(r.Context(), s.git.repoPath(repoID), v.BlobSHA)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", v.MimeType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFileName(v.FileName)+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := getRepository(r.Context(), s.db, repoID); err != nil {
		writeError(w, err)
		return
	}
	branch := r.URL.Query().Get("branch")
	limit := cleanLimit(r.URL.Query().Get("limit"), 80, 300)
	commits, err := s.git.commitLog(r.Context(), s.git.repoPath(repoID), branch, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": commits})
}

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request, repoID int64, parts []string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if len(parts) == 0 {
		writeError(w, errNotFound("not found"))
		return
	}
	sha := parts[0]
	summary, err := s.git.commitSummary(r.Context(), s.git.repoPath(repoID), sha)
	if err != nil {
		writeError(w, err)
		return
	}
	files, err := s.git.commitFiles(r.Context(), s.git.repoPath(repoID), sha)
	if err != nil {
		writeError(w, err)
		return
	}
	scanPaths, err := currentScanPaths(r.Context(), s.db, repoID)
	if err != nil {
		writeError(w, err)
		return
	}
	files = filterCommitChanges(files, scanPaths)
	writeJSON(w, http.StatusOK, CommitDetail{CommitSummary: summary, Files: files})
}

func (s *Server) handlePathEvents(w http.ResponseWriter, r *http.Request, repoID int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var documentID int64
	if raw := r.URL.Query().Get("document_id"); raw != "" {
		id, err := parseID(raw)
		if err != nil {
			writeError(w, err)
			return
		}
		documentID = id
	}
	events, err := listPathEvents(r.Context(), s.db, repoID, documentID, r.URL.Query().Get("branch"), r.URL.Query().Get("event_type"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request, repoID int64, download bool) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	commit := r.URL.Query().Get("commit_sha")
	filePath := normalizeRepoPath(r.URL.Query().Get("path"))
	if !gitSafeCommit(commit) || filePath == "" {
		writeError(w, errBadRequest("commit_sha and valid path are required"))
		return
	}
	data, err := s.git.showFile(r.Context(), s.git.repoPath(repoID), commit, filePath)
	if err != nil {
		writeError(w, err)
		return
	}
	if download {
		w.Header().Set("Content-Type", mimeType(filePath))
		disposition := "attachment"
		if r.URL.Query().Get("inline") == "1" {
			disposition = "inline"
		}
		w.Header().Set("Content-Disposition", disposition+`; filename="`+sanitizeFileName(filePath)+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"commit_sha": commit,
		"path":       filePath,
		"mime_type":  mimeType(filePath),
		"content":    string(data),
	})
}

func decodeBody(body io.ReadCloser, v any) error {
	defer body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nilResponseWriter{}, body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadRequest("invalid json: " + err.Error())
	}
	return nil
}

type nilResponseWriter struct{}

func (nilResponseWriter) Header() http.Header       { return http.Header{} }
func (nilResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (nilResponseWriter) WriteHeader(int)           {}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (s *Server) webHandler() http.Handler {
	fileSystem := os.DirFS(s.cfg.WebDir)
	fileServer := http.FileServer(http.FS(fileSystem))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, errNotFound("not found"))
			return
		}
		cleanPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if cleanPath == "." {
			cleanPath = "index.html"
		}
		if !fileExists(fileSystem, cleanPath) {
			cleanPath = "index.html"
		}
		if cleanPath == "index.html" {
			http.ServeFile(w, r, filepath.Join(s.cfg.WebDir, "index.html"))
			return
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = cloneURL(r.URL)
		r2.URL.Path = "/" + cleanPath
		fileServer.ServeHTTP(w, r2)
	})
}

func cloneURL(u *url.URL) *url.URL {
	copy := *u
	return &copy
}

func fileExists(fileSystem fs.FS, name string) bool {
	info, err := fs.Stat(fileSystem, name)
	return err == nil && !info.IsDir()
}
