package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestPiCitationsOnlyActualLinesAndMerge(t *testing.T) {
	seen := make(piSeenRanges)
	seen.AddToolResult("读取第 1-400 行，共 800 行\n== 游戏 服务@feature/库存:doc/中文 空格.md#L1-400\n    1\t第一行\n    2\t第二行\n  400\t最后一行\n还有 400 行，用 start_line=401 继续")
	seen.AddToolResult("找到 3 处匹配\n== 游戏 服务@feature/库存:doc/中文 空格.md#L3-10 （库存方案）\nL3- 上下文\nL4: 匹配行\nL10: 匹配行\n== 游戏 服务@feature/库存:doc/只有 文件.md （没有输出编号行）")
	key := piCitationFile{Repo: "游戏 服务", Ref: "feature/库存", Path: "doc/中文 空格.md"}
	if want := []piLineRange{{Start: 1, End: 4}, {Start: 10, End: 10}, {Start: 400, End: 400}}; !reflect.DeepEqual(seen[key], want) {
		t.Fatalf("实际可见行未正确合并：%+v", seen[key])
	}
	answer := strings.Join([]string{
		"[游戏 服务@feature/库存:doc/中文 空格.md#L4-9]",
		"[游戏 服务@feature/库存:doc/中文 空格.md#L5-9]",
		"[游戏 服务@feature/库存:doc/只有 文件.md#L1]",
		"[游戏 服务@main:doc/中文 空格.md#L1]",
		"[其他仓库@feature/库存:doc/中文 空格.md#L1]",
		"[游戏 服务@feature/库存:doc/没看过.md#L1]",
		"[游戏 服务@feature/库存:doc/中文 空格.md#L4-9]",
	}, "\n")
	citations := verifyPiCitations(answer, seen)
	want := []string{"verified", "path_only", "path_only", "unverified", "unverified", "unverified"}
	if len(citations) != len(want) {
		t.Fatalf("重复键应去重：%+v", citations)
	}
	for i, citation := range citations {
		if citation.Status != want[i] {
			t.Errorf("%s: got %s, want %s", citation.Key, citation.Status, want[i])
		}
	}
	// 无效组头不能把后续编号行计入前一个文件。
	seen.AddToolResult("== 游戏 服务@feature/库存:doc/中文 空格.md#L1-1\n== 无效组头\nL99: 不是之前文件的内容")
	if result := verifyPiCitations("[游戏 服务@feature/库存:doc/中文 空格.md#L99]", seen); result[0].Status != "path_only" {
		t.Fatal("无效组头污染了之前文件的范围")
	}
}

func TestPiCitationsInvalidRanges(t *testing.T) {
	seen := make(piSeenRanges)
	seen.AddToolResult("== r@main:a.go#L1-100\nL3: 看过的行")
	for _, reference := range []string{"r@main:a.go#L0", "r@main:a.go#L4-2", "r@main:a.go#L999999999999999999999999"} {
		result := verifyPiCitations("["+reference+"]", seen)
		if len(result) != 1 || result[0].Status == "verified" {
			t.Fatalf("无效行号被核实：%+v", result)
		}
	}
	if result := verifyPiCitations("没有引用以及 [r@main:a.go]", seen); len(result) != 0 {
		t.Fatalf("应只处理文档约定的最终引用：%+v", result)
	}
}

func TestPiCitationURLsDocumentBlobAndUnknown(t *testing.T) {
	ctx := context.Background()
	server := newWebhookTestServer(t)
	const commit = "1234567890abcdef1234567890abcdef12345678"
	// GitBin 使用确定性只读替身，既核实 ref 调用格式，也避免测试执行 git 写操作。
	fakeGit := filepath.Join(t.TempDir(), "git-read-only")
	script := "#!/bin/sh\n[ \"$2\" = --verify ] || exit 2\ncase \"$1:$3:$4\" in\n 'show-ref:--hash:refs/heads/main'|'rev-parse:--end-of-options:" + commit + "^{commit}') printf '%s\\n' '" + commit + "';;\n *) exit 1;;\nesac\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	server.cfg.GitBin = fakeGit
	repo, err := createRepository(ctx, server.db, Repository{Name: "游戏 服务", Slug: "game-service", RepoURL: "https://example.invalid/game.git", DefaultBranch: "main", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(server.git.repoPath(repo.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = server.db.ExecContext(ctx, `INSERT INTO documents(id,repo_id,scan_path,doc_key,created_at,updated_at) VALUES(1,?,'.','库存','now','now')`, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.db.ExecContext(ctx, `INSERT INTO doc_versions(id,repo_id,document_id,branch,head_commit_sha,scan_path,file_path,created_at,updated_at)
		VALUES(7,?,1,'main',?,'.','doc/中文 空格.md','now','now')`, repo.ID, commit)
	if err != nil {
		t.Fatal(err)
	}
	input := []piCitation{
		{Repo: "game-service", Ref: "main", Path: "doc/中文 空格.md", Status: "verified"},
		{Repo: "game-service", Ref: "main", Path: "core/中文 路径.go", Status: "path_only"},
		{Repo: "game-service", Ref: commit, Path: "core/中文 路径.go", Status: "unverified"},
		{Repo: "game-service", Ref: "missing", Path: "core/a.go", Status: "unverified"},
		{Repo: "unknown", Ref: "main", Path: "core/a.go", Status: "unverified"},
	}
	result, err := server.resolvePiCitationURLs(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].URL != fmt.Sprintf("/?repo=%d&version=7", repo.ID) || result[0].VersionID != 7 || result[0].CommitSHA != commit {
		t.Fatalf("文档链接错误：%+v", result[0])
	}
	for _, index := range []int{1, 2} {
		parsed, err := url.Parse(result[index].URL)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Path != "/api/repos/"+strconv.FormatInt(repo.ID, 10)+"/blob/download" || parsed.Query().Get("path") != "core/中文 路径.go" || parsed.Query().Get("commit_sha") != commit || parsed.Query().Get("inline") != "1" {
			t.Fatalf("blob URL 编码或 commit 解析错误：%s", result[index].URL)
		}
	}
	if result[3].URL != "" || result[4].URL != "" {
		t.Fatal("不能为无法解析的引用编造 URL")
	}
	for i := range result {
		if result[i].Status != input[i].Status || input[i].URL != "" {
			t.Fatal("链接解析不能修改核实状态或原始切片")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := server.resolvePiCitationURLs(canceled, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("没有传递取消：%v", err)
	}
}
