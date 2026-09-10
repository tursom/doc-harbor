package app

import (
	"context"
	"testing"
)

// 验证关闭设置经过数据库读写及局部更新后仍有效，并覆盖启动、定时和手动触发路径。
func TestRepositoryDisableAutoPull(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	sourceRepo, _ := createScanPathGitRepo(t)
	server := newWebhookTestServer(t)
	repo, err := createRepository(ctx, server.db, Repository{
		Name: "Manual Repo", RepoURL: sourceRepo, SyncIntervalSeconds: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.SyncIntervalSeconds != -1 {
		t.Fatalf("disabled interval = %d", repo.SyncIntervalSeconds)
	}
	// 普通配置更新未传扫描周期时，不能意外恢复自动拉取。
	repo, err = updateRepository(ctx, server.db, repo.ID, Repository{Name: "Renamed Repo"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.SyncIntervalSeconds != -1 {
		t.Fatalf("partial update reset interval to %d", repo.SyncIntervalSeconds)
	}
	for _, trigger := range []string{"startup", "scheduled"} {
		server.scanner.scanEnabled(ctx, trigger)
		reloaded, err := getRepository(ctx, server.db, repo.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.LatestScan != nil {
			t.Fatalf("%s unexpectedly scanned disabled repository", trigger)
		}
	}
	// 自动拉取开关不禁用仓库，也不阻止手动或 Webhook 触发扫描。
	for _, trigger := range []string{"manual", "webhook"} {
		if _, err := server.scanner.Scan(ctx, repo.ID, trigger); err != nil {
			t.Fatalf("%s scan: %v", trigger, err)
		}
	}
	if _, err := updateRepository(ctx, server.db, repo.ID, Repository{SyncIntervalSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	server.scanner.scanEnabled(ctx, "startup")
	repo, err = getRepository(ctx, server.db, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if repo.LatestScan == nil || repo.LatestScan.TriggerType != "startup" {
		t.Fatalf("re-enabled repository did not scan on startup: %+v", repo.LatestScan)
	}
	// 已开启的仓库也能通过更新关闭，且不被默认值覆盖。
	repo, err = updateRepository(ctx, server.db, repo.ID, Repository{SyncIntervalSeconds: -1})
	if err != nil {
		t.Fatal(err)
	}
	if repo.SyncIntervalSeconds != -1 {
		t.Fatalf("update did not disable auto pull: %d", repo.SyncIntervalSeconds)
	}
}
