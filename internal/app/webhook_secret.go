package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// githubWebhookSecret 每次读取持久化的当前密钥，使刷新后收到的请求立即使用新值，
// 多个 Server 实例也不会因各自缓存而继续接受旧密钥。只有尚未刷新时才回退环境变量；
// 数据库读取失败必须返回错误，不能回退到可能已经泄漏的旧密钥。
func (s *Server) githubWebhookSecret(ctx context.Context) (string, error) {
	if s.db == nil {
		return s.cfg.GitHubWebhookSecret, nil
	}
	var secret string
	err := s.db.QueryRowContext(ctx, `SELECT secret FROM github_webhook_settings WHERE id = 1`).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return s.cfg.GitHubWebhookSecret, nil
	}
	return secret, err
}

// rotateGitHubWebhookSecret 使用密码学随机数生成 256 位密钥，原子替换唯一的有效值。
// 先持久化再返回新密钥：写入失败时保留旧值，成功后重启也不会重新使用环境变量中的旧值。
func (s *Server) rotateGitHubWebhookSecret(ctx context.Context) (string, error) {
	if s.db == nil {
		return "", errUnavailable("webhook settings storage is unavailable")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(random[:])
	_, err := s.db.ExecContext(ctx, `INSERT INTO github_webhook_settings (id, secret, updated_at)
		VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET secret = excluded.secret, updated_at = excluded.updated_at`, secret, nowString())
	if err != nil {
		return "", err
	}
	return secret, nil
}
