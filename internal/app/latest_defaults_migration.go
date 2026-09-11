package app

import (
	"context"
	"database/sql"
	"errors"
)

const backupLatestDefaultsMigration = "exclude_backup_from_default_latest_v1"

// migrateBackupLatestDefaults 只升级仍使用旧默认排除规则的仓库，自定义规则保持不变。
// 同时刷新已索引版本的参与标记并重算智能最新，因此禁用自动拉取的仓库也无需重新拉取
// Git 就能移除备份分支来源。所有变更与迁移标记在同一事务提交，避免部分生效。
func migrateBackupLatestDefaults(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var applied string
	err = tx.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE name = ?`, backupLatestDefaultsMigration).Scan(&applied)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, latest_exclude_branches, branch_priority FROM repositories`)
	if err != nil {
		return err
	}
	var repos []Repository
	for rows.Next() {
		var repo Repository
		var exclude, priority string
		if err := rows.Scan(&repo.ID, &exclude, &priority); err != nil {
			rows.Close()
			return err
		}
		rules := decodeStringList(exclude, nil)
		// 按规则集合识别旧默认值，兼容前端保存时调整顺序及 JSON 空格的情况。
		if !usesLegacyLatestExcludes(rules) {
			continue
		}
		repo.LatestExcludeBranches = append(rules, "backup/*")
		repo.BranchPriority = decodeStringList(priority, []string{"main", "master", "release/*", "develop", "feature/*"})
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, repo := range repos {
		if _, err := tx.ExecContext(ctx, `UPDATE repositories SET latest_exclude_branches = ?, updated_at = ? WHERE id = ?`,
			encodeJSON(repo.LatestExcludeBranches), nowString(), repo.ID); err != nil {
			return err
		}
		// 使用大小写敏感的 GLOB 匹配 backup/ 前缀，与分支规则的匹配语义保持一致。
		if _, err := tx.ExecContext(ctx, `UPDATE doc_versions SET participates_latest = 0, updated_at = ?
			WHERE repo_id = ? AND branch GLOB 'backup/*'`, nowString(), repo.ID); err != nil {
			return err
		}
		if err := recomputeLatest(ctx, tx, repo); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
		backupLatestDefaultsMigration, nowString()); err != nil {
		return err
	}
	return tx.Commit()
}

// usesLegacyLatestExcludes 不把额外的排除规则或明确的自定义配置当作默认值覆盖。
func usesLegacyLatestExcludes(rules []string) bool {
	if len(rules) != 3 {
		return false
	}
	seen := make(map[string]bool, 3)
	for _, rule := range rules {
		seen[rule] = true
	}
	return seen["archive/*"] && seen["tmp/*"] && seen["dependabot/*"]
}
