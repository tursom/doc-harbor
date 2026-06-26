package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifyGitHubSignature(t *testing.T) {
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	signature := signGitHubWebhook("secret", body)

	if !verifyGitHubSignature("secret", body, signature) {
		t.Fatal("expected valid signature")
	}
	if verifyGitHubSignature("secret", body, signature+"0") {
		t.Fatal("expected signature with wrong length to be rejected")
	}
	if verifyGitHubSignature("wrong", body, signature) {
		t.Fatal("expected wrong secret to be rejected")
	}
	if verifyGitHubSignature("secret", body, "sha1=abc") {
		t.Fatal("expected unsupported signature prefix to be rejected")
	}
	if verifyGitHubSignature("", body, signature) {
		t.Fatal("expected empty secret to be rejected")
	}
}

func TestGitHubWebhookDisabledWithoutSecret(t *testing.T) {
	server := &Server{}
	recorder := postGitHubWebhook(t, server, 1, "ping", []byte(`{}`), signGitHubWebhook("secret", []byte(`{}`)))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func TestGitHubWebhookRejectsMissingSignature(t *testing.T) {
	server := &Server{cfg: Config{GitHubWebhookSecret: "secret"}}
	recorder := postGitHubWebhook(t, server, 1, "ping", []byte(`{}`), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestGitHubWebhookRejectsBadSignature(t *testing.T) {
	server := &Server{cfg: Config{GitHubWebhookSecret: "secret"}}
	recorder := postGitHubWebhook(t, server, 1, "ping", []byte(`{"ok":true}`), signGitHubWebhook("wrong", []byte(`{"ok":true}`)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestGitHubWebhookPing(t *testing.T) {
	body := []byte(`{"hook_id":1}`)
	server := &Server{cfg: Config{GitHubWebhookSecret: "secret"}}
	recorder := postGitHubWebhook(t, server, 1, "ping", body, signGitHubWebhook("secret", body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"event":"ping"`) {
		t.Fatalf("response body does not contain ping event: %s", recorder.Body.String())
	}
}

func TestGitHubWebhookIgnoresUnsupportedEvents(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	server := &Server{cfg: Config{GitHubWebhookSecret: "secret"}}
	recorder := postGitHubWebhook(t, server, 42, "pull_request", body, signGitHubWebhook("secret", body))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"ignored":true`) {
		t.Fatalf("response body does not mark event ignored: %s", recorder.Body.String())
	}
}

func TestGitHubWebhookSecretConfigured(t *testing.T) {
	server := &Server{cfg: Config{GitHubWebhookSecret: "secret"}}
	recorder := getGitHubWebhookSecret(t, server)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Configured bool   `json:"configured"`
		Secret     string `json:"secret"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Configured || response.Secret != "secret" {
		t.Fatalf("response = %+v, want configured secret", response)
	}
}

func TestGitHubWebhookSecretEmpty(t *testing.T) {
	server := &Server{}
	recorder := getGitHubWebhookSecret(t, server)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Configured bool   `json:"configured"`
		Secret     string `json:"secret"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Configured || response.Secret != "" {
		t.Fatalf("response = %+v, want unconfigured empty secret", response)
	}
}

func TestGitHubWebhookPushCreatesScanRun(t *testing.T) {
	requireGit(t)

	sourceRepo := createTestGitRepo(t)
	server := newWebhookTestServer(t)
	repo, err := createRepository(context.Background(), server.db, Repository{
		Name:                  "Webhook Repo",
		Slug:                  "webhook-repo",
		RepoURL:               sourceRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"main"},
		LatestIncludeBranches: []string{"main"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	body := []byte(`{"ref":"refs/heads/main"}`)
	recorder := postGitHubWebhook(t, server, repo.ID, "push", body, signGitHubWebhook("secret", body))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := listScanRuns(context.Background(), server.db, repo.ID, 10)
		if err != nil {
			t.Fatalf("list scan runs: %v", err)
		}
		for _, run := range runs {
			if run.TriggerType == "github_webhook" && run.Status != "running" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected github_webhook scan run to finish")
}

func TestGitHubWebhookPushRejectsUnknownRepo(t *testing.T) {
	server := newWebhookTestServer(t)
	body := []byte(`{"ref":"refs/heads/main"}`)
	recorder := postGitHubWebhook(t, server, 404, "push", body, signGitHubWebhook("secret", body))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func signGitHubWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postGitHubWebhook(t *testing.T, server *Server, repoID int64, event string, body []byte, signature string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/"+strconv.FormatInt(repoID, 10), bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	recorder := httptest.NewRecorder()
	server.handleGitHubWebhook(recorder, req)
	return recorder
}

func getGitHubWebhookSecret(t *testing.T, server *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/webhooks/github/secret", nil)
	recorder := httptest.NewRecorder()
	server.handleGitHubWebhookSecret(recorder, req)
	return recorder
}

func newWebhookTestServer(t *testing.T) *Server {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	server, err := NewServer(Config{
		DataDir:             dataDir,
		DBDSN:               filepath.Join(dataDir, "doc-harbor.db"),
		GitBin:              "git",
		AllowLocalGit:       true,
		GitCommandTimeout:   5 * time.Second,
		GitHubWebhookSecret: "secret",
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Fatalf("close server: %v", err)
		}
	})
	return server
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git command is required")
	}
}

func createTestGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	runTestGit(t, "", "init", repoDir)
	runTestGit(t, repoDir, "config", "user.name", "DocHarbor Test")
	runTestGit(t, repoDir, "config", "user.email", "doc-harbor@example.invalid")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write test document: %v", err)
	}
	runTestGit(t, repoDir, "add", "README.md")
	runTestGit(t, repoDir, "commit", "-m", "initial docs")
	runTestGit(t, repoDir, "branch", "-M", "main")
	return repoDir
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
