package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestScanPathChangeRestrictsVisibleFilesAndForcesRescan(t *testing.T) {
	requireGit(t)

	ctx := context.Background()
	sourceRepo, headSHA := createScanPathGitRepo(t)
	server := newWebhookTestServer(t)
	repo, err := createRepository(ctx, server.db, Repository{
		Name:                  "Scan Path Repo",
		Slug:                  "scan-path-repo",
		RepoURL:               sourceRepo,
		DefaultBranch:         "main",
		TrackedBranches:       []string{"main"},
		LatestIncludeBranches: []string{"main"},
		ScanPaths:             []ScanPath{{Path: ".", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := server.scanner.Scan(ctx, repo.ID, "manual"); err != nil {
		t.Fatalf("initial scan: %v", err)
	}

	repo, err = updateRepository(ctx, server.db, repo.ID, Repository{
		ScanPaths: []ScanPath{{Path: "doc", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("update scan paths: %v", err)
	}

	latestRoot, err := listLatestFiles(ctx, server.db, repo.ID, ".")
	if err != nil {
		t.Fatalf("list latest root: %v", err)
	}
	requireEntryPaths(t, latestRoot, "doc")

	latestDoc, err := listLatestFiles(ctx, server.db, repo.ID, "doc")
	if err != nil {
		t.Fatalf("list latest doc: %v", err)
	}
	requireEntryPaths(t, latestDoc, "doc/a.md")

	branchRoot, err := listBranchFiles(ctx, server.db, repo.ID, "main", ".")
	if err != nil {
		t.Fatalf("list branch root: %v", err)
	}
	requireEntryPaths(t, branchRoot, "doc")

	readmeResults, err := searchDocuments(ctx, server.db, repo.ID, "README", 30)
	if err != nil {
		t.Fatalf("search README: %v", err)
	}
	requireEntryPaths(t, readmeResults)

	docResults, err := searchDocuments(ctx, server.db, repo.ID, "Alpha", 30)
	if err != nil {
		t.Fatalf("search Alpha: %v", err)
	}
	requireEntryPaths(t, docResults, "doc/a.md")

	tree, err := treeForView(ctx, server.db, repo.ID, "latest", "")
	if err != nil {
		t.Fatalf("tree latest: %v", err)
	}
	requireEntryPaths(t, tree, "doc/a.md")

	detail := getCommitDetail(t, server, repo.ID, headSHA)
	requireCommitPaths(t, detail.Files, "doc/a.md")

	run, err := server.scanner.Scan(ctx, repo.ID, "manual")
	if err != nil {
		t.Fatalf("rescan after scan path update: %v", err)
	}
	if run.FileCount != 1 {
		t.Fatalf("rescan file count = %d, want 1", run.FileCount)
	}
	if strings.Contains(run.DetailJSON, `"skipped_by_head":true`) {
		t.Fatalf("rescan should not skip unchanged head after scan path update: %s", run.DetailJSON)
	}

	var activeOutside int
	if err := server.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM doc_versions
		WHERE repo_id = ? AND branch = 'main' AND status IN ('active','renamed','moved')
		AND NOT (file_path = 'doc' OR file_path LIKE 'doc/%')`, repo.ID).Scan(&activeOutside); err != nil {
		t.Fatalf("count active outside scan path: %v", err)
	}
	if activeOutside != 0 {
		t.Fatalf("active outside scan path = %d, want 0", activeOutside)
	}

	var latestOutside int
	if err := server.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM doc_latest
		WHERE repo_id = ? AND NOT (file_path = 'doc' OR file_path LIKE 'doc/%')`, repo.ID).Scan(&latestOutside); err != nil {
		t.Fatalf("count latest outside scan path: %v", err)
	}
	if latestOutside != 0 {
		t.Fatalf("latest outside scan path = %d, want 0", latestOutside)
	}
}

func createScanPathGitRepo(t *testing.T) (string, string) {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	runTestGit(t, "", "init", repoDir)
	runTestGit(t, repoDir, "config", "user.name", "DocHarbor Test")
	runTestGit(t, repoDir, "config", "user.email", "doc-harbor@example.invalid")
	writeScanPathTestFile(t, repoDir, "README.md", "# Project README\n")
	writeScanPathTestFile(t, repoDir, "doc/a.md", "# Alpha Guide\n\nv1\n")
	writeScanPathTestFile(t, repoDir, "src/app.go", "package main\n")
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "initial project")

	writeScanPathTestFile(t, repoDir, "doc/a.md", "# Alpha Guide\n\nv2\n")
	writeScanPathTestFile(t, repoDir, "src/app.go", "package main\n\nfunc main() {}\n")
	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "update docs and source")
	runTestGit(t, repoDir, "branch", "-M", "main")

	headSHA := strings.TrimSpace(runTestGitOutput(t, repoDir, "rev-parse", "HEAD"))
	return repoDir, headSHA
}

func writeScanPathTestFile(t *testing.T, repoDir, name, content string) {
	t.Helper()
	target := filepath.Join(repoDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runTestGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func getCommitDetail(t *testing.T, server *Server, repoID int64, sha string) CommitDetail {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/repos/1/commits/"+sha, nil)
	recorder := httptest.NewRecorder()
	server.handleCommits(recorder, req, repoID, []string{sha})
	if recorder.Code != http.StatusOK {
		t.Fatalf("commit detail status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var detail CommitDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode commit detail: %v", err)
	}
	return detail
}

func requireEntryPaths(t *testing.T, entries []FileEntry, want ...string) {
	t.Helper()
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Path)
	}
	requireStringSet(t, got, want)
}

func requireCommitPaths(t *testing.T, changes []CommitFileChange, want ...string) {
	t.Helper()
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Path != "" {
			got = append(got, change.Path)
			continue
		}
		if change.NewPath != "" {
			got = append(got, change.NewPath)
			continue
		}
		got = append(got, change.OldPath)
	}
	requireStringSet(t, got, want)
}

func requireStringSet(t *testing.T, got []string, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}
