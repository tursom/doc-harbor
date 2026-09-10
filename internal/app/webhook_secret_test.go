package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 通过管理路由刷新，验证签名切换、重复刷新，以及新 Server 连接读取同一数据库的行为。
func TestGitHubWebhookSecretRotation(t *testing.T) {
	server := newWebhookTestServer(t)
	rotate := func() string {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/webhooks/github/secret", nil))
		if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("rotate status=%d cache=%q", recorder.Code, recorder.Header().Get("Cache-Control"))
		}
		var result struct {
			Configured bool   `json:"configured"`
			Secret     string `json:"secret"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		decoded, err := hex.DecodeString(result.Secret)
		if err != nil || len(decoded) != 32 || !result.Configured {
			t.Fatal("expected a configured 256-bit secret")
		}
		return result.Secret
	}
	checkSignature := func(s *Server, secret string, status int) {
		t.Helper()
		body := []byte(`{}`)
		recorder := postGitHubWebhook(t, s, 1, "ping", body, signGitHubWebhook(secret, body))
		if recorder.Code != status {
			t.Fatalf("signature response = %d, want %d", recorder.Code, status)
		}
	}
	checkSignature(server, "secret", http.StatusOK)
	first := rotate()
	checkSignature(server, "secret", http.StatusUnauthorized)
	checkSignature(server, first, http.StatusOK)
	second := rotate()
	if second == first {
		t.Fatal("rotation reused previous secret")
	}
	checkSignature(server, first, http.StatusUnauthorized)
	checkSignature(server, second, http.StatusOK)

	// 使用全新的数据库连接和不同的环境配置，证明已持久化的密钥优先于环境变量。
	cfg := server.cfg
	cfg.GitHubWebhookSecret = "another-environment-secret"
	restarted, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	checkSignature(restarted, second, http.StatusOK)
	checkSignature(restarted, cfg.GitHubWebhookSecret, http.StatusUnauthorized)
	current, err := restarted.githubWebhookSecret(context.Background())
	if err != nil || current != second {
		t.Fatal("persisted secret was not restored")
	}
	recorder := getGitHubWebhookSecret(t, restarted)
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["secret"] != second || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("GET did not return the current uncached secret")
	}
}

// 未设置环境变量的部署也可以直接生成密钥；保存失败不得替换已有有效值。
func TestGitHubWebhookSecretRotationStorageFailure(t *testing.T) {
	server := newWebhookTestServer(t)
	server.cfg.GitHubWebhookSecret = ""
	secret, err := server.rotateGitHubWebhookSecret(context.Background())
	if err != nil || secret == "" {
		t.Fatal("could not initialize secret without environment configuration")
	}
	_, err = server.db.Exec(`CREATE TRIGGER prevent_secret_update BEFORE UPDATE ON github_webhook_settings
		BEGIN SELECT RAISE(ABORT, 'test write failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.handleGitHubWebhookSecret(recorder, httptest.NewRequest(http.MethodPost, "/api/webhooks/github/secret", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed rotation status = %d", recorder.Code)
	}
	current, err := server.githubWebhookSecret(context.Background())
	if err != nil || current != secret {
		t.Fatal("failed rotation changed the active secret")
	}
	// 数据库读取出错时不能接受环境变量中的旧密钥。
	server.cfg.GitHubWebhookSecret = "old-secret"
	if _, err := server.db.Exec(`DROP TABLE github_webhook_settings`); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	recorder = postGitHubWebhook(t, server, 1, "ping", body, signGitHubWebhook("old-secret", body))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("storage failure must not fall back to old secret: status=%d", recorder.Code)
	}
}
