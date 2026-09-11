package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 使用真实 Git 扫描建立旧索引，验证升级后无需重新扫描就能排除 backup 文档，
// 同时保留分支视图、自定义仓库规则及用户在迁移之后作出的配置决定。
func TestMigrateBackupLatestDefaults(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	source := createTestGitRepo(t)
	runTestGit(t, source, "checkout", "-b", "backup/terminal-order-status-detailed-20260903")
	if err := os.WriteFile(filepath.Join(source, "backup-only.md"), []byte("# Backup only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, source, "add", "backup-only.md")
	runTestGit(t, source, "commit", "-m", "backup docs")
	server := newWebhookTestServer(t)
	legacy := []string{"tmp/*", "dependabot/*", "archive/*"}
	repo, err := createRepository(ctx, server.db, Repository{
		Name: "Legacy", RepoURL: source, LatestExcludeBranches: legacy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.scanner.Scan(ctx, repo.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	entries, err := listLatestFiles(ctx, server.db, repo.ID, ".")
	if err != nil {
		t.Fatal(err)
	}
	requireEntryPaths(t, entries, "README.md", "backup-only.md")
	custom, err := createRepository(ctx, server.db, Repository{
		Name: "Custom", RepoURL: source, LatestExcludeBranches: []string{"tmp/*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 测试库初始化时已登记迁移；移除标记模拟升级前的数据库状态。
	if _, err := server.db.Exec(`DELETE FROM schema_migrations WHERE name = ?`, backupLatestDefaultsMigration); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, server.db); err != nil {
		t.Fatal(err)
	}
	reloaded, err := getRepository(ctx, server.db, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !matchBranchRules(reloaded.LatestExcludeBranches, "backup/test", false) {
		t.Fatal("legacy defaults did not exclude backup")
	}
	entries, err = listLatestFiles(ctx, server.db, repo.ID, ".")
	if err != nil {
		t.Fatal(err)
	}
	requireEntryPaths(t, entries, "README.md")
	entries, err = listBranchFiles(ctx, server.db, repo.ID, "backup/terminal-order-status-detailed-20260903", ".")
	if err != nil {
		t.Fatal(err)
	}
	requireEntryPaths(t, entries, "README.md", "backup-only.md")
	custom, err = getRepository(ctx, server.db, custom.ID)
	if err != nil || len(custom.LatestExcludeBranches) != 1 || custom.LatestExcludeBranches[0] != "tmp/*" {
		t.Fatal("migration changed custom exclusions")
	}
	// 管理员之后可显式恢复旧规则；重复启动不能再次覆盖这一决定。
	if _, err := updateRepository(ctx, server.db, repo.ID, Repository{LatestExcludeBranches: legacy}); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, server.db); err != nil {
		t.Fatal(err)
	}
	reloaded, err = getRepository(ctx, server.db, repo.ID)
	if err != nil || !usesLegacyLatestExcludes(reloaded.LatestExcludeBranches) {
		t.Fatal("migration reapplied and overwrote user configuration")
	}
}

func TestNewRepositoryExcludesBackupByDefault(t *testing.T) {
	server := newWebhookTestServer(t)
	repo, err := createRepository(context.Background(), server.db, Repository{Name: "Default", RepoURL: "https://example.invalid/repo.git"})
	if err != nil {
		t.Fatal(err)
	}
	if participatesLatest(repo, "backup/test", nowString()) {
		t.Fatal("backup branch participates in latest by default")
	}
	if !participatesLatest(repo, "main", nowString()) {
		t.Fatal("main branch was excluded")
	}
}
