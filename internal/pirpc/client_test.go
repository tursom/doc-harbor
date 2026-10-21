package pirpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFakePi 把 Go 测试可执行文件自身作为假 pi，真正跨进程传输 JSONL。
// 只有 app 显式提供的测试环境标记存在时才进入子进程分支。
func TestFakePi(t *testing.T) {
	mode := os.Getenv("PIRPC_FAKE")
	if mode == "" {
		return
	}
	writer := bufio.NewWriter(os.Stdout)
	emit := func(record map[string]any) {
		line, _ := json.Marshal(record)
		// 故意使用 CRLF 和未转义的 Unicode 分隔符，验证严格 LF framing。
		line = []byte(strings.ReplaceAll(strings.ReplaceAll(string(line), `\u2028`, "\u2028"), `\u2029`, "\u2029"))
		writer.Write(line)
		writer.WriteString("\r\n")
		writer.Flush()
	}
	response := func(command map[string]any, data any) {
		emit(map[string]any{"type": "response", "id": command["id"], "command": command["type"], "success": true, "data": data})
	}
	notify := func(message, level string) {
		emit(map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": "notify", "message": message, "notifyType": level})
	}
	decoder := json.NewDecoder(os.Stdin)
	var reversed []map[string]any
	for {
		var command map[string]any
		if decoder.Decode(&command) != nil {
			os.Exit(0)
		}
		switch command["type"] {
		case "get_state":
			switch mode {
			case "startup_hang":
				continue
			case "startup_fail":
				emit(map[string]any{"type": "response", "id": command["id"], "success": false, "error": "health rejected"})
				continue
			case "startup_exit":
				fmt.Fprintln(os.Stderr, "startup exploded")
				os.Exit(23)
			case "startup_mcp":
				notify("MCP servers need attention:\n  docharbor: failed to connect: fetch failed\nRun /mcp to fix.", "warning")
			}
			response(command, map[string]any{"pid": os.Getpid(), "version": os.Getenv("PIRPC_VERSION")})
			if mode == "blocked_stdin" {
				for {
					time.Sleep(time.Hour)
				}
			}
		case "echo":
			response(command, command["value"])
		case "environment":
			cwd, _ := os.Getwd()
			response(command, map[string]any{"env": os.Environ(), "cwd": cwd})
		case "reverse":
			reversed = append(reversed, command)
			if len(reversed) == 2 {
				emit(map[string]any{"type": "bash_execution_update", "id": command["id"], "delta": "event with id"})
				response(reversed[1], reversed[1]["value"])
				response(reversed[0], reversed[0]["value"])
				reversed = nil
			}
		case "long_event":
			emit(map[string]any{"type": "message_update", "delta": strings.Repeat("长", 400000) + "\u2028\u2029"})
			response(command, nil)
		case "stderr":
			for i := 0; i < 250; i++ {
				fmt.Fprintf(os.Stderr, "diagnostic-%03d\r\n", i)
			}
			response(command, nil)
		case "exit":
			fmt.Fprint(os.Stderr, "final diagnostic without newline")
			os.Exit(17)
		case "fail":
			emit(map[string]any{"type": "response", "id": command["id"], "success": false, "error": "bad model"})
		case "hang":
			// 继续接受其他命令，用来验证取消等待不会阻塞 abort/后续命令。
		case "late":
			response(command, nil)
		case "notify":
			notify(command["message"].(string), command["level"].(string))
			response(command, nil)
		case "flood":
			for i := 0; i <= eventCapacity; i++ {
				emit(map[string]any{"type": "turn_start"})
			}
			response(command, nil)
		case "malformed":
			writer.WriteString("not json\n")
			writer.Flush()
		case "prompt":
			response(command, map[string]any{"disposition": "started"})
			emit(map[string]any{"type": "agent_start"})
		case "abort":
			emit(map[string]any{"type": "agent_end", "willRetry": false})
			emit(map[string]any{"type": "agent_settled"})
			response(command, nil)
		default:
			response(command, nil)
		}
	}
}

func fakeSpec(t *testing.T, mode string) Spec {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Bin: bin, Args: []string{"-test.run=^TestFakePi$"}, Dir: t.TempDir(), Env: []string{"PIRPC_FAKE=" + mode}, Version: "v1"}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func startFake(t *testing.T, mode string) *Client {
	t.Helper()
	c, err := Start(testContext(t), fakeSpec(t, mode))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func commandOK(t *testing.T, c *Client, typ string, fields map[string]any) map[string]any {
	t.Helper()
	result, err := c.Command(testContext(t), typ, fields)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitDone(t *testing.T, c *Client) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pi process did not finish")
	}
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestClientResponseCorrelationAndIDEvent(t *testing.T) {
	c := startFake(t, "normal")
	var wg sync.WaitGroup
	for _, value := range []string{"first", "second"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			response, err := c.Command(testContext(t), "reverse", map[string]any{"value": value, "id": "forged", "type": "forged"})
			if err != nil || response["data"] != value {
				t.Errorf("response = %v, error = %v, want %s", response, err, value)
			}
		}(value)
	}
	wg.Wait()
	select {
	case event := <-c.Events():
		if event["type"] != "bash_execution_update" || event["id"] == nil {
			t.Fatalf("event with id lost: %v", event)
		}
	case <-testContext(t).Done():
		t.Fatal("event missing")
	}
	result, err := c.Command(testContext(t), "fail", nil)
	if err == nil || result["success"] != false || !c.Healthy() {
		t.Fatalf("command failure should retain response and process: %v %v", result, err)
	}
}

func TestClientLargeRecordsAndFraming(t *testing.T) {
	c := startFake(t, "normal")
	large := strings.Repeat("x", 2<<20)
	response := commandOK(t, c, "echo", map[string]any{"value": large})
	if response["data"] != large {
		t.Fatal("large response truncated")
	}
	commandOK(t, c, "long_event", nil)
	select {
	case event := <-c.Events():
		if delta, _ := event["delta"].(string); delta != strings.Repeat("长", 400000)+"\u2028\u2029" {
			t.Fatal("large/Unicode event corrupted")
		}
	case <-testContext(t).Done():
		t.Fatal("event missing")
	}
}

func TestClientEnvironmentIsolationAndPath(t *testing.T) {
	t.Setenv("PIRPC_PARENT_SECRET", "must not be inherited")
	spec := fakeSpec(t, "normal")
	spec.Env = append(spec.Env, "EXPLICIT_ALLOWED=value")
	// 验证裸命令只使用 app 指定的 PATH，且允许测试目录内的可执行文件。
	link := filepath.Join(t.TempDir(), "fake-pi")
	if err := os.Symlink(spec.Bin, link); err != nil {
		t.Fatal(err)
	}
	spec.Bin = "fake-pi"
	if _, err := Start(testContext(t), spec); err == nil {
		t.Fatal("binary resolved without explicit PATH")
	}
	spec.Env = append(spec.Env, "PATH="+filepath.Dir(link))
	c, err := Start(testContext(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	data := commandOK(t, c, "environment", nil)["data"].(map[string]any)
	if data["cwd"] != spec.Dir {
		t.Fatalf("cwd = %v", data["cwd"])
	}
	encoded, _ := json.Marshal(data["env"])
	if strings.Contains(string(encoded), "PIRPC_PARENT_SECRET") || !strings.Contains(string(encoded), "EXPLICIT_ALLOWED=value") {
		t.Fatalf("environment isolation failed: %s", encoded)
	}
}

func TestClientStderrExitAndClose(t *testing.T) {
	c := startFake(t, "normal")
	commandOK(t, c, "stderr", nil)
	eventually(t, func() bool { return strings.HasSuffix(c.StderrTail(), "diagnostic-249") })
	lines := strings.Split(c.StderrTail(), "\n")
	if len(lines) != 200 || lines[0] != "diagnostic-050" {
		t.Fatalf("stderr ring = %d lines starting %q", len(lines), lines[0])
	}
	_, err := c.Command(testContext(t), "exit", nil)
	if err == nil || !strings.Contains(err.Error(), "final diagnostic without newline") {
		t.Fatalf("exit error missing stderr: %v", err)
	}
	waitDone(t, c)
	if c.Healthy() {
		t.Fatal("exited client is healthy")
	}
	c.Close()
	c.Close()
	if _, ok := <-c.Events(); ok {
		t.Fatal("events not closed")
	}
	if _, err := c.Command(testContext(t), "echo", nil); err == nil {
		t.Fatal("exited process accepted command")
	}
}

func TestClientStartupHealthFailures(t *testing.T) {
	for _, mode := range []string{"startup_fail", "startup_hang", "startup_exit", "startup_mcp"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			c, err := Start(ctx, fakeSpec(t, mode))
			if err == nil || c != nil {
				t.Fatalf("unhealthy startup = %v %v", c, err)
			}
			if mode == "startup_hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost context error: %v", err)
			}
			if mode == "startup_exit" && !strings.Contains(err.Error(), "startup exploded") {
				t.Fatalf("lost startup stderr: %v", err)
			}
		})
	}
}

func TestClientStartupContextDoesNotOwnProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(testContext(t))
	c, err := Start(ctx, fakeSpec(t, "normal"))
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	commandOK(t, c, "echo", map[string]any{"value": "still alive"})
}

func TestClientExitFailsAllPendingCommands(t *testing.T) {
	c := startFake(t, "normal")
	ctx := testContext(t)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := c.Command(ctx, "hang", nil)
			results <- err
		}()
	}
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.pending) == 2 })
	_, _ = c.Command(ctx, "exit", nil)
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err == nil || !strings.Contains(err.Error(), "final diagnostic without newline") {
				t.Fatalf("pending command lost exit diagnostic: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("pending command not failed on exit")
		}
	}
}

func TestClientCancellationAndAbort(t *testing.T) {
	c := startFake(t, "normal")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Command(ctx, "hang", nil)
	if !errors.Is(err, context.DeadlineExceeded) || !c.Healthy() {
		t.Fatalf("cancel = %v, healthy = %v", err, c.Healthy())
	}
	commandOK(t, c, "prompt", map[string]any{"message": "question"})
	commandOK(t, c, "abort", nil)
	for _, typ := range []string{"agent_start", "agent_end", "agent_settled"} {
		select {
		case event := <-c.Events():
			if event["type"] != typ {
				t.Fatalf("want %s, got %v", typ, event)
			}
		case <-testContext(t).Done():
			t.Fatal("abort event missing")
		}
	}
}

func TestClientBlockedWriteAndClose(t *testing.T) {
	for _, cancelWrite := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelWrite), func(t *testing.T) {
			c := startFake(t, "blocked_stdin")
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := c.Command(ctx, "echo", map[string]any{"value": strings.Repeat("x", 2<<20)})
				finished <- err
			}()
			// 等待写请求进入 writer（pending 注册在提交写请求之前）。
			eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.pending) == 1 })
			if cancelWrite {
				// writer 已经取走命令后再取消，覆盖真正的阻塞写路径。
				time.Sleep(20 * time.Millisecond)
				cancel()
			} else {
				c.Close()
			}
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("blocked write unexpectedly succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked write did not unblock")
			}
			waitDone(t, c)
		})
	}
}

func TestClientMCPNotifications(t *testing.T) {
	for _, message := range []string{
		"MCP servers need attention:\n  docharbor: failed to connect: fetch failed\nRun /mcp to fix.",
		"MCP failed to load: broken module",
	} {
		t.Run(message, func(t *testing.T) {
			c := startFake(t, "normal")
			_, _ = c.Command(testContext(t), "notify", map[string]any{"message": message, "level": "warning"})
			waitDone(t, c)
			if c.Healthy() || !errors.Is(c.Err(), ErrUnhealthy) || !strings.Contains(c.StderrTail(), "extension_ui_request") {
				t.Fatalf("MCP failure not recorded: %v", c.Err())
			}
		})
	}
	c := startFake(t, "normal")
	commandOK(t, c, "notify", map[string]any{"message": "MCP servers are still connecting; their tools become available once connected.", "level": "info"})
	if !c.Healthy() || len(c.events) != 0 {
		t.Fatal("normal UI notification affected events or health")
	}
}

func TestClientOverflowAndMalformedOutput(t *testing.T) {
	for _, typ := range []string{"flood", "malformed"} {
		t.Run(typ, func(t *testing.T) {
			c := startFake(t, "normal")
			_, _ = c.Command(testContext(t), typ, nil)
			waitDone(t, c)
			if c.Healthy() || c.Err() == nil {
				t.Fatal("protocol failure kept client healthy")
			}
			if typ == "flood" && !errors.Is(c.Err(), ErrEventOverflow) {
				t.Fatalf("overflow error = %v", c.Err())
			}
		})
	}
}
