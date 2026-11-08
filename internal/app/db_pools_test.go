package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 同时打开多条连接验证驱动级约束；只检查第一次 PRAGMA 会漏掉连接重建的问题。
func TestDBPoolsReadOnlyAndForeignKeysPerConnection(t *testing.T) {
	for _, dsn := range []string{":memory:", "file::memory:?cache=shared", "file:named-pool-test?mode=memory", "disk", "file-uri"} {
		t.Run(dsn, func(t *testing.T) {
			dir := t.TempDir()
			if dsn == "disk" {
				dsn = filepath.Join(dir, "pool.db")
			} else if dsn == "file-uri" {
				dsn = "file:" + filepath.Join(dir, "pool.db") + "?_fk=off&_query_only=off"
			}
			server, err := NewServer(Config{DataDir: dir, DBDSN: dsn})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			repo, err := createRepository(context.Background(), server.db, Repository{Name: "pools", RepoURL: "unused"})
			if err != nil {
				t.Fatal(err)
			}
			if server.db.Stats().MaxOpenConnections != 1 || server.readDB.Stats().MaxOpenConnections != 4 {
				t.Fatalf("pool limits: writer=%+v reader=%+v", server.db.Stats(), server.readDB.Stats())
			}
			// 连续两轮，第二轮强制重建所有连接，验证约束不是只在初始化时生效。
			for round := 0; round < 2; round++ {
				var connections []*sql.Conn
				for i := 0; i < 4; i++ {
					conn, err := server.readDB.Conn(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					connections = append(connections, conn)
					t.Cleanup(func() { _ = conn.Close() })
					var queryOnly, foreignKeys, count int
					if err := conn.QueryRowContext(context.Background(), `PRAGMA query_only`).Scan(&queryOnly); err != nil {
						t.Fatal(err)
					}
					if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
						t.Fatal(err)
					}
					if queryOnly != 1 || foreignKeys != 1 {
						t.Fatalf("connection %d: query_only=%d foreign_keys=%d", i, queryOnly, foreignKeys)
					}
					if err := conn.QueryRowContext(context.Background(), `SELECT count(*) FROM repositories WHERE id = ?`, repo.ID).Scan(&count); err != nil || count != 1 {
						t.Fatalf("reader is not sharing writer DB: count=%d err=%v", count, err)
					}
					if _, err := conn.ExecContext(context.Background(), `UPDATE repositories SET name = 'forbidden'`); err == nil {
						t.Fatal("reader accepted a write")
					}
				}
				server.readDB.SetMaxIdleConns(0)
				for _, conn := range connections {
					_ = conn.Close()
				}
				server.readDB.SetMaxIdleConns(4)
			}
			// reader 保持共享内存库存活，再重建 writer 连接验证外键仍生效。
			conn, err := server.readDB.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			server.db.SetMaxIdleConns(0)
			if _, err := server.db.Exec(`INSERT INTO scan_runs(repo_id, trigger_type, status, started_at) VALUES(99999, 'test', 'running', '')`); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
				t.Fatalf("writer foreign key not enforced: %v", err)
			}
		})
	}
}

func TestDBPoolsWALReadDuringWriteTransaction(t *testing.T) {
	server := newWebhookTestServer(t)
	repo, err := createRepository(context.Background(), server.db, Repository{Name: "committed", RepoURL: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	var journal string
	if err := server.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal=%s err=%v", journal, err)
	}
	tx, err := server.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE repositories SET name = 'uncommitted' WHERE id = ?`, repo.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stored, err := getRepository(ctx, server.reader(), repo.ID)
	if err != nil || stored.Name != "committed" {
		t.Fatalf("WAL snapshot while writer is held: repo=%+v err=%v", stored, err)
	}
	// 验证实际 GET 接线，避免虽然新建了 reader，handler 仍误用 writer。
	for _, route := range []string{"/api/repos", fmt.Sprintf("/api/repos/%d", repo.ID), fmt.Sprintf("/api/repos/%d/scan-runs", repo.ID), "/api/webhooks/github/secret"} {
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil).WithContext(ctx))
		if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "uncommitted") {
			t.Fatalf("GET %s while writer held: %d %s", route, recorder.Code, recorder.Body.String())
		}
	}
}

func TestScanCheckpointMigrationAndRoundTrip(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%t", legacy), func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			cfg := Config{DataDir: dir, DBDSN: filepath.Join(dir, "migration.db")}
			if legacy {
				db, err := sql.Open("sqlite3", cfg.DBDSN)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec(`CREATE TABLE repo_refs (
					id INTEGER PRIMARY KEY, repo_id INTEGER NOT NULL, ref_type TEXT NOT NULL,
					ref_name TEXT NOT NULL, commit_sha TEXT NOT NULL, commit_time TEXT NOT NULL,
					last_scanned_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
					UNIQUE(repo_id, ref_type, ref_name))`)
				_ = db.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			db, err := openDB(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := migrate(ctx, db); err != nil {
				t.Fatalf("migration not idempotent: %v", err)
			}
			repo, err := createRepository(ctx, db, Repository{Name: "migration", RepoURL: "unused"})
			if err != nil {
				t.Fatal(err)
			}
			for _, hash := range []string{"config-v1", "config-v2"} {
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := upsertRepoRef(ctx, tx, RepoRef{RepoID: repo.ID, RefType: "branch", RefName: "main", CommitSHA: "head", ScanConfigHash: hash}); err != nil {
					_ = tx.Rollback()
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				ref, err := getRepoRef(ctx, db, repo.ID, "branch", "main")
				if err != nil || ref == nil || ref.ScanConfigHash != hash {
					t.Fatalf("get ref=%+v err=%v", ref, err)
				}
				refs, err := listBranches(ctx, db, repo.ID)
				if err != nil || len(refs) != 1 || refs[0].ScanConfigHash != hash {
					t.Fatalf("list refs=%+v err=%v", refs, err)
				}
			}
			for _, index := range []struct{ name, definition string }{
				{"idx_doc_versions_repo_path_updated", "repo_id, file_path, updated_at DESC"},
				{"idx_doc_versions_repo_branch_path", "repo_id, branch, file_path"},
			} {
				var definition string
				if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, index.name).Scan(&definition); err != nil || !strings.Contains(definition, index.definition) {
					t.Fatalf("index %s = %s err=%v", index.name, definition, err)
				}
			}
		})
	}
}

func TestServerRestartFinishesAbandonedScan(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, DBDSN: filepath.Join(dir, "restart.db")}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := createRepository(context.Background(), server.db, Repository{Name: "restart", RepoURL: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := createScanRun(context.Background(), server.db, repo.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	server, err = NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	stored, err := latestScanRun(context.Background(), server.reader(), repo.ID)
	if err != nil || stored == nil || stored.ID != run.ID || stored.Status != "failed" || stored.FinishedAt == "" || !strings.Contains(stored.ErrorMessage, "重启") {
		t.Fatalf("restart recovery: run=%+v err=%v", stored, err)
	}
}
