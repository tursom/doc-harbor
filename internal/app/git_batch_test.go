package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 所有写入均限于 t.TempDir 创建的真实仓库；固定提交时间使 merge 默认日期排序
// 可复现。测试不使用网络、业务配置、数据库或当前工作区的 Git 写操作。
type batchGitRepo struct {
	t    *testing.T
	dir  string
	git  *Git
	step int
}

func newBatchGitRepo(t *testing.T) *batchGitRepo {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	r := &batchGitRepo{t: t, dir: t.TempDir(), git: newGit(Config{GitBin: bin, GitCommandTimeout: 10 * time.Second})}
	r.run("init", "-b", "main")
	r.run("config", "user.name", "Git Batch Test")
	r.run("config", "user.email", "git-batch@example.invalid")
	r.run("config", "commit.gpgsign", "false")
	return r
}

func (r *batchGitRepo) command(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	date := time.Date(2025, 1, 1, 0, 0, r.step, 0, time.UTC).Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	return cmd
}

func (r *batchGitRepo) run(args ...string) string {
	r.t.Helper()
	out, err := r.command(args...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %q: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *batchGitRepo) write(name string, data []byte) {
	r.t.Helper()
	target := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *batchGitRepo) commit() string {
	r.t.Helper()
	r.step++
	r.run("add", "--all")
	r.run("commit", "--allow-empty", "-m", fmt.Sprintf("step %d", r.step))
	return r.run("rev-parse", "HEAD")
}

func batchShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// wrapper 记录真实 Git 调用数；tee 只在对象测试中记录实际批量请求，验证去重。
func (r *batchGitRepo) countedGit(recordRequests bool) (*Git, string, string) {
	r.t.Helper()
	dir := r.t.TempDir()
	commands, requests := filepath.Join(dir, "commands"), filepath.Join(dir, "requests")
	bin := r.git.cfg.GitBin
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + batchShellQuote(commands) + "\n"
	if recordRequests {
		script += "if [ \"$1\" = cat-file ]; then\n tee " + batchShellQuote(requests) + " | " + batchShellQuote(bin) + " \"$@\"\n exit $?\nfi\n"
	}
	script += "exec " + batchShellQuote(bin) + " \"$@\"\n"
	wrapper := filepath.Join(dir, "git-wrapper")
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		r.t.Fatal(err)
	}
	cfg := r.git.cfg
	cfg.GitBin = wrapper
	return newGit(cfg), commands, requests
}

func batchReadLines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestGitBatchLastCommitsLinearAndLiteralPaths(t *testing.T) {
	r := newBatchGitRepo(t)
	paths := []string{"plain.md", "\nfirst\t.md", "tab\tname.md", " space .md ", "back\\slash.md", "\x1eheader.md", "*.md", ":(glob)*.md", "-dash.md", "old.md"}
	for _, p := range paths {
		r.write(p, []byte("initial "+p))
	}
	base := r.commit()
	r.write("plain.md", []byte("updated"))
	updated := r.commit()
	r.run("mv", "old.md", "renamed\n\t.md")
	renamed := r.commit()
	head := r.commit() // 空提交不应变成任一路径的最后提交。
	paths = append(paths, "renamed\n\t.md")
	counted, commands, _ := r.countedGit(false)
	input := append(append([]string{}, paths...), paths...)
	got, err := counted.lastCommitsForPaths(context.Background(), r.dir, head, input)
	if err != nil {
		t.Fatal(err)
	}
	if lines := batchReadLines(t, commands); len(lines) != 1 {
		t.Fatalf("batch last commits launched %d commands: %q", len(lines), lines)
	}
	for _, p := range paths {
		want := base
		if p == "plain.md" {
			want = updated
		}
		if p == "old.md" || p == "renamed\n\t.md" {
			want = renamed
		}
		legacy, err := r.git.lastCommitForPath(context.Background(), r.dir, head, p)
		if err != nil || got[p] != legacy || got[p].SHA != want {
			t.Errorf("path %q: batch=%+v single=%+v want SHA=%s err=%v", p, got[p], legacy, want, err)
		}
	}
	missing, err := r.git.lastCommitsForPaths(context.Background(), r.dir, head, []string{"missing.md"})
	if err != nil || missing["missing.md"].SHA != head || missing["missing.md"].Time == "" {
		t.Fatalf("missing path fallback = %+v, %v", missing, err)
	}
	entries, err := r.git.lsTree(context.Background(), r.dir, head, ".")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := make(map[string]bool)
	for _, p := range paths {
		if p != "old.md" {
			wantPaths[p] = true
		}
	}
	for _, entry := range entries {
		if !wantPaths[entry.Path] {
			t.Errorf("lsTree changed filename %q", entry.Path)
		}
		delete(wantPaths, entry.Path)
	}
	if len(wantPaths) != 0 {
		t.Fatalf("lsTree lost paths: %v", wantPaths)
	}
}

// 真实 merge 的三个形态覆盖：采用侧分支、丢弃侧分支、冲突后产生新内容。
// 尤其 ours merge 的全仓日志仍可列出被丢弃侧分支文件，单路径默认历史却跳过它。
func TestGitBatchLastCommitsMergeRequiresFallback(t *testing.T) {
	for _, mode := range []string{"adopt", "ours", "resolved"} {
		t.Run(mode, func(t *testing.T) {
			r := newBatchGitRepo(t)
			r.write("file.md", []byte("base\n"))
			r.write("other.md", []byte("base\n"))
			base := r.commit()
			r.run("checkout", "-b", "side")
			r.write("file.md", []byte("side\n"))
			side := r.commit()
			r.run("checkout", "main")
			r.write("other.md", []byte("main\n"))
			if mode == "resolved" {
				r.write("file.md", []byte("main\n"))
			}
			r.commit()
			r.step++
			switch mode {
			case "ours":
				r.run("merge", "--no-ff", "-s", "ours", "side", "-m", "discard side")
			case "resolved":
				if err := r.command("merge", "--no-ff", "side", "-m", "conflict").Run(); err == nil {
					t.Fatal("expected a real merge conflict")
				}
				r.write("file.md", []byte("resolved\n"))
				r.commit()
			default:
				r.run("merge", "--no-ff", "side", "-m", "adopt side")
			}
			head := r.run("rev-parse", "HEAD")
			want := side
			if mode == "ours" {
				want = base
			} else if mode == "resolved" {
				want = head
			}
			counted, commands, _ := r.countedGit(false)
			got, err := counted.lastCommitsForPaths(context.Background(), r.dir, head, []string{"file.md", "other.md"})
			if got != nil || !errors.Is(err, errGitBatchMergeHistory) {
				t.Fatalf("merge must return explicit limitation, got=%+v err=%v", got, err)
			}
			if len(batchReadLines(t, commands)) != 1 {
				t.Fatal("merge detection must still use one command")
			}
			fallback, err := r.git.lastCommitForPath(context.Background(), r.dir, head, "file.md")
			if err != nil || fallback.SHA != want {
				t.Fatalf("single-path fallback = %+v err=%v want=%s", fallback, err, want)
			}
			fallbackGit, fallbackCommands, _ := r.countedGit(false)
			fallbacks, err := fallbackGit.lastCommitsForMergePaths(context.Background(), r.dir, head, []string{"file.md", "other.md", "file.md"})
			if err != nil || fallbacks["file.md"] != fallback || len(fallbacks) != 2 || len(batchReadLines(t, fallbackCommands)) != 2 {
				t.Fatalf("merge fallback must preserve semantics and deduplicate paths: %+v err=%v", fallbacks, err)
			}
			if mode == "ours" {
				naive := r.run("log", "--name-only", "-z", "--no-renames", "--format=%H", head, "--")
				if !strings.Contains(naive, side+"\x00\nfile.md\x00") {
					t.Fatalf("expected discarded side commit in naive unfiltered log: %q", naive)
				}
			}
		})
	}
}

func TestGitBatchCatFilesStreamsAndDeduplicates(t *testing.T) {
	r := newBatchGitRepo(t)
	// 请求总长度超过常见管道容量，同时响应更大；先全写请求再读响应会死锁。
	for i := 0; i < 2200; i++ {
		r.write(fmt.Sprintf("objects/%04d", i), []byte(fmt.Sprintf("%04d", i)+strings.Repeat("x", 256)))
	}
	r.write("binary", bytes.Repeat([]byte{0, '\n', '\t', 255}, 256*1024))
	r.write("empty", nil)
	head := r.commit()
	entries, err := r.git.lsTree(context.Background(), r.dir, head, ".")
	if err != nil {
		t.Fatal(err)
	}
	var blobs []string
	want := make(map[string][]byte)
	for _, entry := range entries {
		blobs = append(blobs, entry.BlobSHA, entry.BlobSHA)
		data, err := os.ReadFile(filepath.Join(r.dir, entry.Path))
		if err != nil {
			t.Fatal(err)
		}
		want[entry.BlobSHA] = data
	}
	counted, commands, requests := r.countedGit(true)
	got, err := counted.catFiles(context.Background(), r.dir, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blob bytes differ: got %d objects, want %d", len(got), len(want))
	}
	if len(batchReadLines(t, commands)) != 1 {
		t.Fatal("catFiles must launch exactly one Git command")
	}
	if n := len(batchReadLines(t, requests)); n != len(want) {
		t.Fatalf("sent %d requests, want %d unique SHA requests", n, len(want))
	}
	for _, bad := range []string{strings.Repeat("0", 40), head, "not-a-sha\nHEAD"} {
		if got, err := r.git.catFiles(context.Background(), r.dir, []string{blobs[0], bad}); err == nil || got != nil {
			t.Fatalf("object error %q: got=%v err=%v", bad, got != nil, err)
		}
	}
}

func batchFakeGit(t *testing.T, script string, timeout time.Duration) *Git {
	t.Helper()
	name := filepath.Join(t.TempDir(), "git-fake")
	if err := os.WriteFile(name, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return newGit(Config{GitBin: name, GitCommandTimeout: timeout})
}

func TestGitBatchCatFilesProtocolErrors(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for name, response := range map[string]string{
		"missing":      sha + " missing\n",
		"wrong-object": strings.Repeat("b", 40) + " blob 0\n\n",
		"wrong-type":   sha + " tree 0\n\n",
		"bad-size":     sha + " blob -1\n",
		"truncated":    sha + " blob 10\nx",
		"terminator":   sha + " blob 1\nxx",
	} {
		t.Run(name, func(t *testing.T) {
			g := batchFakeGit(t, "read request\nprintf '%s' "+batchShellQuote(response)+"\n", time.Second)
			if got, err := g.catFiles(context.Background(), "", []string{sha}); err == nil || got != nil {
				t.Fatalf("malformed protocol: got=%v err=%v", got, err)
			}
		})
	}
	// 正常响应后异常退出不能被当作成功；仍需检查 Wait 的退出状态。
	g := batchFakeGit(t, "read request\nprintf '%s' "+batchShellQuote(sha+" blob 0\n\n")+"\nexit 7\n", time.Second)
	if got, err := g.catFiles(context.Background(), "", []string{sha}); err == nil || got != nil {
		t.Fatalf("nonzero process exit: got=%v err=%v", got, err)
	}
}

// 响应头、正文、进程退出三个阶段分别阻塞，确保取消不仅覆盖首次 Read。
func TestGitBatchCatFilesCancellationAtProtocolStages(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for name, response := range map[string]string{
		"header": "",
		"body":   sha + " blob 4096\nx",
		"wait":   sha + " blob 0\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			g := batchFakeGit(t, "read request\nprintf '%s' "+batchShellQuote(response)+"\nexec sleep 30\n", 100*time.Millisecond)
			start := time.Now()
			got, err := g.catFiles(context.Background(), "", []string{sha})
			if got != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
				t.Fatalf("stage=%s got=%v err=%v elapsed=%s", name, got, err, time.Since(start))
			}
		})
	}
}

func TestGitBatchCancellationTimeoutAndReaping(t *testing.T) {
	r := newBatchGitRepo(t)
	r.write("file", []byte("test"))
	head := r.commit()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.git.catFiles(ctx, r.dir, []string{head}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled batch: %v", err)
	}
	for _, operation := range []string{"catFiles", "isAncestor", "lastCommits", "pathHistory"} {
		for _, cause := range []string{"cancel", "timeout"} {
			t.Run(operation+"/"+cause, func(t *testing.T) {
				pidFile := filepath.Join(t.TempDir(), "pid")
				g := batchFakeGit(t, "echo $$ > "+batchShellQuote(pidFile)+"\nexec sleep 30\n", 5*time.Second)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				want := context.Canceled
				if cause == "timeout" {
					g.cfg.GitCommandTimeout = 100 * time.Millisecond
					want = context.DeadlineExceeded
				} else {
					time.AfterFunc(100*time.Millisecond, cancel)
				}
				start := time.Now()
				var err error
				switch operation {
				case "catFiles":
					_, err = g.catFiles(ctx, "", []string{head})
				case "isAncestor":
					_, err = g.isAncestor(ctx, "", head, head)
				case "lastCommits":
					_, err = g.lastCommitsForPaths(ctx, "", head, []string{"file"})
				case "pathHistory":
					_, err = g.pathHistoryRange(ctx, "", "main", ".", "", head)
				}
				if !errors.Is(err, want) || time.Since(start) > 3*time.Second {
					t.Fatalf("operation did not stop promptly: %v after %s", err, time.Since(start))
				}
				data, err := os.ReadFile(pidFile)
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); !os.IsNotExist(err) {
					t.Fatalf("child process %d not reaped: %v", pid, err)
				}
			})
		}
	}
}

func TestGitBatchIsAncestor(t *testing.T) {
	r := newBatchGitRepo(t)
	r.write("file", []byte("initial"))
	base := r.commit()
	r.run("checkout", "-b", "side")
	r.write("file", []byte("side"))
	side := r.commit()
	r.run("checkout", "main")
	r.write("file", []byte("main"))
	head := r.commit()
	for _, tc := range []struct {
		old, new string
		want     bool
	}{{base, head, true}, {head, head, true}, {head, base, false}, {side, head, false}} {
		got, err := r.git.isAncestor(context.Background(), r.dir, tc.old, tc.new)
		if err != nil || got != tc.want {
			t.Fatalf("isAncestor(%s,%s)=%t,%v want %t", tc.old, tc.new, got, err, tc.want)
		}
	}
	if got, err := r.git.isAncestor(context.Background(), r.dir, strings.Repeat("0", 40), head); got || err == nil {
		t.Fatalf("invalid object treated as non-ancestor: %t, %v", got, err)
	}
	if _, err := r.git.isAncestor(context.Background(), t.TempDir(), base, head); err == nil {
		t.Fatal("invalid repository must fail")
	}
}

func TestGitBatchPathHistoryRangeAndSpecialPaths(t *testing.T) {
	r := newBatchGitRepo(t)
	oldPath, newPath := "doc/ \nold\t\\.md ", "doc/\x1e\nnew\t.md "
	r.write(oldPath, []byte("unique rename content\n"))
	r.write("other.txt", []byte("other"))
	base := r.commit()
	r.run("mv", oldPath, newPath)
	rename := r.commit()
	r.write(newPath, []byte("modified unique rename content\n"))
	r.commit() // 修改事件与旧接口一致，不计入路径创建/删除/重命名历史。
	r.run("rm", "--", newPath)
	head := r.commit()
	full, err := r.git.pathHistory(context.Background(), r.dir, "main", ".")
	if err != nil {
		t.Fatal(err)
	}
	all, err := r.git.pathHistoryRange(context.Background(), r.dir, "main", ".", "", head)
	if err != nil || !reflect.DeepEqual(all, full) {
		t.Fatalf("full range differs: %+v / %+v err=%v", all, full, err)
	}
	counted, commands, _ := r.countedGit(false)
	got, err := counted.pathHistoryRange(context.Background(), r.dir, "main-label", "doc", base, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(batchReadLines(t, commands)) != 1 {
		t.Fatal("history range must use one Git command")
	}
	// 类型仍交给既有 pathEventType：它将旧名中的反斜线视为目录分隔，归为 moved。
	// 本次只保证 Git 返回的路径原样传递，不改变上层既有的移动/改名分类规则。
	if len(got) != 2 || got[0].CommitSHA != rename || got[0].OldPath != oldPath || got[0].NewPath != newPath || got[0].EventType != "moved" || got[0].RenameScore != 100 || got[1].CommitSHA != head || got[1].OldPath != newPath || got[1].EventType != "deleted" {
		t.Fatalf("special path events: %+v", got)
	}
	for _, e := range got {
		if e.ScanPath != "doc" || e.Branch != "main-label" || e.CommitTime == "" {
			t.Fatalf("event labels: %+v", e)
		}
	}
	if empty, err := r.git.pathHistoryRange(context.Background(), r.dir, "main", ".", head, head); err != nil || len(empty) != 0 {
		t.Fatalf("empty range: %+v, %v", empty, err)
	}
}

func TestGitBatchEmptyInputsDoNotStartGit(t *testing.T) {
	g := newGit(Config{GitBin: "/does/not/exist", GitCommandTimeout: time.Second})
	if got, err := g.catFiles(context.Background(), "", nil); err != nil || len(got) != 0 {
		t.Fatalf("empty blobs: %v %v", got, err)
	}
	if got, err := g.lastCommitsForPaths(context.Background(), "", "HEAD", nil); err != nil || len(got) != 0 {
		t.Fatalf("empty paths: %v %v", got, err)
	}
}

// 普通文件名使用原 pathHistory 的文本协议作为兼容性基准，覆盖 merge、目录过滤、
// 同提交多事件顺序和重命名。特殊文件名则由上面的原样字节断言单独验证。
func TestGitBatchPathHistoryMatchesLegacyOnMerges(t *testing.T) {
	for _, strategy := range []string{"normal", "ours"} {
		t.Run(strategy, func(t *testing.T) {
			r := newBatchGitRepo(t)
			r.write("doc/base.md", []byte("base"))
			r.write("doc/delete.md", []byte("delete"))
			base := r.commit()
			r.run("checkout", "-b", "side")
			r.write("doc/side.md", []byte("side"))
			r.commit()
			r.run("checkout", "main")
			r.run("mv", "doc/base.md", "doc/renamed.md")
			r.run("rm", "doc/delete.md")
			r.write("outside.md", []byte("outside"))
			r.commit()
			r.step++
			if strategy == "ours" {
				r.run("merge", "--no-ff", "-s", "ours", "side", "-m", "ours")
			} else {
				r.run("merge", "--no-ff", "side", "-m", "normal")
			}
			head := r.run("rev-parse", "HEAD")
			for _, scanPath := range []string{".", "doc"} {
				for _, old := range []string{"", base, head} {
					revision := head
					if old != "" {
						revision = old + ".." + head
					}
					args := []string{"log", "--date=iso-strict", "--name-status", "--find-renames", "--format=%x1e%H%x00%cI", revision}
					if scanPath != "." {
						args = append(args, "--", scanPath)
					}
					text := r.run(args...)
					want := []historyPathEvent{}
					for _, chunk := range strings.Split(text, "\x1e") {
						lines := strings.Split(strings.TrimSpace(chunk), "\n")
						header := strings.Split(lines[0], "\x00")
						if len(header) < 2 {
							continue
						}
						for _, change := range parseNameStatus(strings.Join(lines[1:], "\n")) {
							e := historyPathEvent{ScanPath: scanPath, Branch: "main", CommitSHA: header[0], CommitTime: normalizeGitTime(header[1])}
							switch {
							case strings.HasPrefix(change.Status, "R"), strings.HasPrefix(change.Status, "C"):
								e.EventType, e.OldPath, e.NewPath, e.RenameScore = pathEventType(change.OldPath, change.NewPath), change.OldPath, change.NewPath, change.RenameScore
							case change.Status == "A":
								e.EventType, e.NewPath = "created", change.Path
							case change.Status == "D":
								e.EventType, e.OldPath = "deleted", change.Path
							}
							if e.EventType != "" {
								want = append(want, e)
							}
						}
					}
					for i, j := 0, len(want)-1; i < j; i, j = i+1, j-1 {
						want[i], want[j] = want[j], want[i]
					}
					got, err := r.git.pathHistoryRange(context.Background(), r.dir, "main", scanPath, old, head)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("path=%s range=%s: got=%+v want=%+v err=%v", scanPath, revision, got, want, err)
					}
				}
			}
		})
	}
}
