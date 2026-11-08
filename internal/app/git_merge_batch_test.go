package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 复用 git_batch_test.go 的临时真实仓库工具。所有测试写操作仅发生在
// t.TempDir，比较基准始终是实际执行 git log -1 的既有接口。
func mergeBatchCompare(t *testing.T, r *batchGitRepo, head string, paths []string) map[string]lastCommit {
	t.Helper()
	counted, commands, _ := r.countedGit(false)
	got, err := counted.lastCommitsForMergePaths(context.Background(), r.dir, head, paths)
	if err != nil {
		t.Fatal(err)
	}
	if lines := batchReadLines(t, commands); len(lines) != 2 {
		t.Fatalf("merge batch launched %d processes, want 2: %q", len(lines), lines)
	}
	for _, path := range paths {
		want, err := r.git.lastCommitForPath(context.Background(), r.dir, head, path)
		// 没有任何路径历史时，旧接口返回调用时的 nowString；两次调用跨秒
		// 不构成历史算法差异。只有 Git 确认无记录时才允许这个时间差。
		if err == nil && got[path] != want && got[path].SHA == head && want.SHA == head && got[path].Time != "" && r.run("--literal-pathspecs", "log", "-1", "--format=%H", head, "--", path) == "" {
			continue
		}
		if err != nil || got[path] != want {
			t.Errorf("head=%s path=%q: batch=%+v single=%+v err=%v", head, path, got[path], want, err)
		}
	}
	return got
}

// shell 脚本本身不能包含 NUL；用八进制转义让 printf 在输出阶段还原协议字节。
func mergeBatchPrintf(value string) string {
	var escaped strings.Builder
	for _, b := range []byte(value) {
		fmt.Fprintf(&escaped, "\\%03o", b)
	}
	return "printf '%b' " + batchShellQuote(escaped.String()) + "\n"
}

func TestGitMergeBatchOrdinaryAndResolvedMerges(t *testing.T) {
	for _, mode := range []string{"adopt", "ours", "resolved", "resolved-first", "resolved-second", "same-both"} {
		t.Run(mode, func(t *testing.T) {
			r := newBatchGitRepo(t)
			paths := []string{"file.md", "main.md", "side.md", "stable.md", "\nfirst\t.md", "tab\tname.md", " space .md ", "back\\slash.md", "\x1eheader.md", "*.md", ":(glob)*.md", "-dash.md"}
			for _, path := range paths {
				r.write(path, []byte("base\n"))
			}
			base := r.commit()
			r.run("checkout", "-b", "side")
			r.write("file.md", []byte("side\n"))
			r.write("side.md", []byte("side\n"))
			for _, path := range paths[4:] {
				r.write(path, []byte("side\n"))
			}
			side := r.commit()
			r.run("checkout", "main")
			r.write("main.md", []byte("main\n"))
			if strings.HasPrefix(mode, "resolved") {
				r.write("file.md", []byte("main\n"))
			} else if mode == "same-both" {
				r.write("file.md", []byte("side\n"))
			}
			main := r.commit()
			r.step++
			switch {
			case mode == "ours":
				r.run("merge", "--no-ff", "-s", "ours", "side", "-m", "ours")
			case strings.HasPrefix(mode, "resolved"):
				if err := r.command("merge", "--no-ff", "side", "-m", "conflict").Run(); err == nil {
					t.Fatal("expected a real merge conflict")
				}
				content := "resolved\n"
				if mode == "resolved-first" {
					content = "main\n"
				} else if mode == "resolved-second" {
					content = "side\n"
				}
				r.write("file.md", []byte(content))
				r.commit()
			default:
				r.run("merge", "--no-ff", "side", "-m", "merge")
			}
			head := r.run("rev-parse", "HEAD")
			got := mergeBatchCompare(t, r, head, append(paths, paths...))
			want := side
			switch mode {
			case "ours":
				want = base
			case "resolved":
				want = head
			case "resolved-first", "same-both":
				want = main
			}
			if got["file.md"].SHA != want {
				t.Fatalf("file SHA=%s want=%s", got["file.md"].SHA, want)
			}
		})
	}
}

func TestGitMergeBatchOctopusRevertModesAndRoot(t *testing.T) {
	r := newBatchGitRepo(t)
	paths := []string{"one.md", "two.md", "three.md", "stable.md", "mode.md", "kind.md", "old.md"}
	for _, path := range paths {
		r.write(path, []byte("base\n"))
	}
	root := r.commit()
	mergeBatchCompare(t, r, root, paths)
	for _, branch := range []string{"one", "two", "three"} {
		r.run("checkout", "-b", branch, root)
		r.write(branch+".md", []byte(branch+"\n"))
		r.commit()
	}
	r.run("checkout", "main")
	r.write("stable.md", []byte("main\n"))
	r.commit()
	r.step++
	r.run("merge", "--no-ff", "one", "two", "three", "-m", "octopus")
	octopus := r.run("rev-parse", "HEAD")
	mergeBatchCompare(t, r, octopus, paths)
	r.write("one.md", []byte("temporary\n"))
	change := r.commit()
	r.step++
	r.run("revert", "--no-edit", change)
	if err := os.Chmod(filepath.Join(r.dir, "mode.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.commit()
	if err := os.Remove(filepath.Join(r.dir, "kind.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("stable.md", filepath.Join(r.dir, "kind.md")); err != nil {
		t.Fatal(err)
	}
	r.run("mv", "old.md", "new\t\n.md")
	head := r.commit()
	mergeBatchCompare(t, r, head, append(paths, "new\t\n.md"))
	// 空 root 也必须有可校验的 diff-tree 响应；缺失路径沿用旧接口回退。
	empty := newBatchGitRepo(t)
	emptyHead := empty.commit()
	got, err := empty.git.lastCommitsForMergePaths(context.Background(), empty.dir, emptyHead, []string{"missing"})
	if err != nil || got["missing"].SHA != emptyHead || got["missing"].Time == "" {
		t.Fatalf("empty root fallback=%+v err=%v", got, err)
	}
}

// commit-tree 可准确控制父顺序和结果树，覆盖多个父都 TREESAME、merge 自身
// 产生新模式、嵌套 merge 及非单调时间。此处不会依赖 merge 策略自动选出的树。
func mergeBatchCommitTree(r *batchGitRepo, tree string, parents ...string) string {
	r.t.Helper()
	r.step++
	args := []string{"commit-tree", tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", fmt.Sprintf("DAG %d", r.step))
	return r.run(args...)
}

func TestGitMergeBatchSyntheticDAGMatchesSinglePath(t *testing.T) {
	r := newBatchGitRepo(t)
	paths := []string{"a.md", "b.md", "c.md", "dir/file.md", "\n\todd.md"}
	var trees []string
	// 小型有限状态树反复出现，保证大量“内容改回”和多个相同父树。
	for state := 0; state < 24; state++ {
		for i, path := range paths {
			if i == 4 && state >= 16 {
				if err := os.Remove(filepath.Join(r.dir, path)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				continue
			}
			r.write(path, []byte(fmt.Sprintf("value %d\n", (state>>i)%2)))
		}
		r.run("add", "--all")
		trees = append(trees, r.run("write-tree"))
	}
	rng := rand.New(rand.NewSource(637))
	commits := []string{mergeBatchCommitTree(r, trees[0])}
	for i := 1; i <= 65; i++ {
		parents := []string{commits[i-1]}
		if i > 2 && i%2 == 0 {
			parents = append(parents, commits[rng.Intn(i-1)])
		}
		if i > 4 && i%5 == 0 {
			parents = append(parents, commits[rng.Intn(i-2)])
		}
		// 日期倒退会暴露错误依赖日期排序的实现；拓扑与父顺序保持不变。
		r.step = 200 - i*2
		sha := mergeBatchCommitTree(r, trees[rng.Intn(len(trees))], parents...)
		commits = append(commits, sha)
		if i%4 == 0 || i == 65 {
			mergeBatchCompare(t, r, sha, paths)
		}
	}
}

func TestGitMergeBatchDirectoryFileTransitionsAndDisconnectedRoots(t *testing.T) {
	r := newBatchGitRepo(t)
	r.write("path/child", []byte("initial"))
	r.write("stable", []byte("stable"))
	root := r.commit()
	r.run("rm", "-r", "path")
	r.write("path", []byte("now a file"))
	file := r.commit()
	mergeBatchCompare(t, r, file, []string{"path", "stable", "path/child"})
	r.run("rm", "path")
	r.write("path/child", []byte("directory again"))
	dir := r.commit()
	mergeBatchCompare(t, r, dir, []string{"path", "stable", "path/child"})
	tree := r.run("rev-parse", root+"^{tree}")
	otherRoot := mergeBatchCommitTree(r, tree)
	head := mergeBatchCommitTree(r, tree, dir, otherRoot)
	mergeBatchCompare(t, r, head, []string{"path", "stable", "path/child"})
}

func TestGitMergeBatchAbsentPathParentSelection(t *testing.T) {
	r := newBatchGitRepo(t)
	r.write("file", []byte("present"))
	r.write("stable", []byte("stable"))
	present := r.commit()
	presentTree := r.run("rev-parse", "HEAD^{tree}")
	r.run("rm", "file")
	deleted := r.commit()
	absentTree := r.run("rev-parse", "HEAD^{tree}")
	// 独立 root 从未含有 file；选择这个 TREESAME 父时应得到“无历史”回退，
	// 而不是另一个父上的添加或删除记录。
	neverPresent := mergeBatchCommitTree(r, absentTree)
	for _, parents := range [][]string{{present, deleted}, {deleted, present}, {present, neverPresent}, {neverPresent, present}} {
		for _, tree := range []string{presentTree, absentTree} {
			head := mergeBatchCommitTree(r, tree, parents...)
			mergeBatchCompare(t, r, head, []string{"file", "stable"})
		}
	}
}

// 使用生产中同样常见的 shallow 边界，确认 rev-list 与 diff-tree 对 root
// 的认识一致；不得越过边界读取 clone 尚未取得的父对象。
func TestGitMergeBatchShallowBoundary(t *testing.T) {
	r := newBatchGitRepo(t)
	r.write("file", []byte("base"))
	r.commit()
	r.write("other", []byte("other"))
	r.commit()
	dst := filepath.Join(t.TempDir(), "shallow")
	r.run("clone", "--depth=1", "file://"+r.dir, dst)
	shallow := &batchGitRepo{t: t, dir: dst, git: r.git}
	mergeBatchCompare(t, shallow, "HEAD", []string{"file", "other"})
}

func TestGitMergeBatchDiffTreeStreamingAndTreePairProtocol(t *testing.T) {
	r := newBatchGitRepo(t)
	r.write("\nfile\t.md", []byte("before"))
	r.write("unwanted", []byte("before"))
	r.commit()
	before := r.run("rev-parse", "HEAD^{tree}")
	r.write("\nfile\t.md", []byte("after"))
	r.write("unwanted", []byte("after"))
	r.commit()
	after := r.run("rev-parse", "HEAD^{tree}")
	// 请求与响应均超过普通管道容量。重复 tree 对还验证了 stdin 不会被误当
	// 成“commit + 替代父”协议，且无需为每条请求额外启动 Git。
	line := before + " " + after + "\n"
	requests := make([]mergeTreeRequest, 1200)
	for i := range requests {
		requests[i] = mergeTreeRequest{line: line, header: line, changed: make(map[string]struct{})}
	}
	counted, commands, _ := r.countedGit(false)
	err := counted.readMergeTreeChanges(context.Background(), r.dir, requests, map[string]struct{}{"\nfile\t.md": {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batchReadLines(t, commands)) != 1 {
		t.Fatal("all tree pairs must share one process")
	}
	for i, request := range requests {
		if _, ok := request.changed["\nfile\t.md"]; !ok || len(request.changed) != 1 {
			t.Fatalf("request %d retained wrong changes: %q", i, request.changed)
		}
	}
}

func TestGitMergeBatchProtocolAndDAGErrors(t *testing.T) {
	sha, tree := strings.Repeat("a", 40), strings.Repeat("b", 40)
	dag := sha + "\x00" + tree + "\x002025-01-01T00:00:00+00:00\x00\n"
	for name, badDAG := range map[string]string{
		"empty": "", "truncated": strings.TrimSuffix(dag, "\n"), "duplicate": dag + dag,
		"missing-parent": strings.TrimSuffix(dag, "\n") + strings.Repeat("c", 40) + "\n",
		"bad-time":       strings.ReplaceAll(dag, "2025-01-01T00:00:00+00:00", "bad"),
		"cycle":          strings.TrimSuffix(dag, "\n") + sha + "\n" + dag,
		"self-parent":    strings.TrimSuffix(dag, "\n") + sha + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseMergeHistory([]byte(badDAG)); err == nil {
				t.Fatal("invalid DAG must fail")
			}
		})
	}
	for name, response := range map[string]string{
		"missing-header": "", "wrong-header": tree + "\x00", "extra": sha + "\x00extra",
		"truncated-raw": sha + "\x00:100644", "invalid-raw": sha + "\x00:bad\x00path\x00",
		"rename":         sha + "\x00:100644 100644 " + sha + " " + tree + " R100\x00old\x00new\x00",
		"truncated-path": sha + "\x00:100644 100644 " + sha + " " + tree + " M\x00path",
	} {
		t.Run(name, func(t *testing.T) {
			script := "if [ \"$1\" = rev-list ]; then\n" + mergeBatchPrintf(dag) + "else\nread request\n" + mergeBatchPrintf(response) + "fi\n"
			g := batchFakeGit(t, script, time.Second)
			if got, err := g.lastCommitsForMergePaths(context.Background(), "", sha, []string{"path"}); err == nil || got != nil {
				t.Fatalf("invalid protocol: got=%v err=%v", got, err)
			}
		})
	}
	// 已读完合法响应仍需 Wait，不能吞掉进程的非零退出码。
	g := batchFakeGit(t, "read request\n"+mergeBatchPrintf(sha+"\x00")+"exit 7\n", time.Second)
	if err := g.readMergeTreeChanges(context.Background(), "", []mergeTreeRequest{{line: sha + "\n", header: sha + "\x00", changed: map[string]struct{}{}}}, map[string]struct{}{}); err == nil {
		t.Fatal("nonzero exit must fail")
	}
}

func TestGitMergeBatchCancellationAndEmptyInput(t *testing.T) {
	g := newGit(Config{GitBin: "/does/not/exist", GitCommandTimeout: time.Second})
	if got, err := g.lastCommitsForMergePaths(context.Background(), "", "HEAD", nil); err != nil || len(got) != 0 {
		t.Fatalf("empty batch=%v err=%v", got, err)
	}
	for _, path := range []string{"", "bad\x00path"} {
		if got, err := g.lastCommitsForMergePaths(context.Background(), "", "HEAD", []string{path}); err == nil || got != nil {
			t.Fatalf("bad path accepted: %q", path)
		}
	}
	for _, stage := range []string{"dag", "diff"} {
		for _, cause := range []string{"cancel", "timeout"} {
			t.Run(stage+"/"+cause, func(t *testing.T) {
				sha, tree := strings.Repeat("a", 40), strings.Repeat("b", 40)
				dag := sha + "\x00" + tree + "\x002025-01-01T00:00:00+00:00\x00\n"
				script := "exec sleep 30\n"
				if stage == "diff" {
					script = "if [ \"$1\" = rev-list ]; then\n" + mergeBatchPrintf(dag) + "exit 0\nfi\n" + script
				}
				g := batchFakeGit(t, script, 5*time.Second)
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
				got, err := g.lastCommitsForMergePaths(ctx, "", sha, []string{"path"})
				if got != nil || !errors.Is(err, want) || time.Since(start) > 3*time.Second {
					t.Fatalf("cancel/timeout: got=%v err=%v elapsed=%s", got, err, time.Since(start))
				}
			})
		}
	}
}
