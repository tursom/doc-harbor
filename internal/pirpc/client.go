// Package pirpc 实现 pi 1.0.0 的 JSONL 子进程协议和 FIFO 进程池。
// 本包只管理传输及进程生命周期；run 完成条件、abort 和会话切换由调用方负责。
package pirpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	startupTimeout = 15 * time.Second
	eventCapacity  = 256
	stderrLines    = 200
)

var (
	ErrClosed        = errors.New("pi RPC client closed")
	ErrUnhealthy     = errors.New("pi MCP unhealthy")
	ErrEventOverflow = errors.New("pi event subscriber is not keeping up")
)

// Spec 由 app 生成。Env 是完整的白名单环境，不是对宿主环境的增量覆盖。
// Version 由 app 在配置改变时更新，池仅按 Version 判断是否需要滚动替换。
type Spec struct {
	Bin     string
	Args    []string
	Dir     string
	Env     []string
	Version string
}

func (s Spec) clone() Spec {
	s.Args = append([]string(nil), s.Args...)
	s.Env = append([]string{}, s.Env...)
	return s
}

type writeRequest struct {
	ctx  context.Context
	line []byte
}

// Client 允许并发 Command，但 Events 只能由当前独占该进程的 run 消费。
// stop 表示正在停止，done 表示进程和所有 I/O goroutine 都已完成收尾。
type Client struct {
	provider   string
	model      string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *os.File
	stderr     *os.File
	events     chan map[string]any
	writes     chan writeRequest
	stop       chan struct{}
	done       chan struct{}
	writerDone chan struct{}
	stopOnce   sync.Once
	nextID     atomic.Uint64
	readers    sync.WaitGroup

	mu      sync.Mutex
	pending map[string]chan map[string]any
	reason  error
	exitErr error
	tail    [stderrLines]string
	tailAt  int
	tailLen int
}

// Start 启动进程并用 get_state 检测 RPC 可用性，默认最多等待 15 秒。
// ctx 只约束启动：成功返回后取消 ctx 不会终止被池复用的进程。
func Start(ctx context.Context, spec Spec) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	spec = spec.clone()
	bin, err := resolveBin(spec)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, spec.Args...)
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("pi stdin: %w", err)
	}
	// 自己持有读管道，避免 Cmd.Wait 关闭 StdoutPipe 时丢掉最后的事件或 stderr。
	stdout, outWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	stderr, errWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		outWriter.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outWriter, errWriter
	err = cmd.Start()
	outWriter.Close()
	errWriter.Close()
	if err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start pi: %w", err)
	}
	c := &Client{
		cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr,
		events: make(chan map[string]any, eventCapacity), writes: make(chan writeRequest),
		stop: make(chan struct{}), done: make(chan struct{}), writerDone: make(chan struct{}),
		pending: make(map[string]chan map[string]any),
	}
	for i, arg := range spec.Args {
		if i+1 < len(spec.Args) {
			if arg == "--provider" {
				c.provider = spec.Args[i+1]
			}
			if arg == "--model" {
				c.model = spec.Args[i+1]
			}
		}
	}
	c.readers.Add(2)
	go c.readStdout()
	go c.readStderr()
	go c.writeLoop()
	go c.reap()
	healthCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if _, err := c.Command(healthCtx, "get_state", nil); err != nil {
		c.Close()
		return nil, fmt.Errorf("pi startup get_state: %w; %v", err, c.Err())
	}
	if !c.Healthy() {
		c.Close()
		return nil, c.Err()
	}
	return c, nil
}

// Selection 返回该进程实际启动时的默认模型，排队请求不能沿用入队时的旧配置。
func (c *Client) Selection() (provider, model string) { return c.provider, c.model }

// resolveBin 不调用 exec.LookPath，因为它会读取宿主进程的 PATH。
// 裸名称仅在 app 显式传入的 PATH 内查找，绝对/相对路径按工作目录解析。
func resolveBin(spec Spec) (string, error) {
	if spec.Bin == "" {
		return "", errors.New("pi binary is empty")
	}
	if strings.ContainsRune(spec.Bin, os.PathSeparator) {
		if filepath.IsAbs(spec.Bin) {
			return spec.Bin, nil
		}
		return filepath.Abs(filepath.Join(spec.Dir, spec.Bin))
	}
	var path string
	for _, entry := range spec.Env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(spec.Dir, dir)
		}
		candidate := filepath.Join(dir, spec.Bin)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("pi binary %q not found in Spec.Env PATH", spec.Bin)
}

// Command 返回完整 response。失败响应也保留返回值，同时返回错误。
// 取消 ctx 只取消命令等待，不等于 abort 已受理的 run。
func (c *Client) Command(ctx context.Context, typ string, fields map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	command := make(map[string]any, len(fields)+2)
	for key, value := range fields {
		command[key] = value
	}
	command["type"], command["id"] = typ, id
	line, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("encode pi command: %w", err)
	}
	response := make(chan map[string]any, 1)
	c.mu.Lock()
	if c.reason != nil {
		c.mu.Unlock()
		<-c.done
		return nil, c.Err()
	}
	c.pending[id] = response
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	select {
	case c.writes <- writeRequest{ctx, append(line, '\n')}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.stop:
		<-c.done
		return nil, c.Err()
	}
	select {
	case record := <-response:
		if success, _ := record["success"].(bool); !success {
			return record, fmt.Errorf("pi %s: %v", typ, record["error"])
		}
		return record, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.stop:
		<-c.done
		return nil, c.Err()
	}
}

// 单独的写循环保证并发命令不会交错。阻塞写入期间 ctx 取消会杀进程，
// 从而解除管道背压；已写完、仅等待 response 时取消不会杀进程。
func (c *Client) writeLoop() {
	defer close(c.writerDone)
	for {
		select {
		case <-c.stop:
			return
		case request := <-c.writes:
			if request.ctx.Err() != nil {
				continue
			}
			stopCancel := context.AfterFunc(request.ctx, func() {
				c.terminate(fmt.Errorf("pi stdin write cancelled: %w", request.ctx.Err()))
			})
			n, err := c.stdin.Write(request.line)
			stopCancel()
			if err == nil && n != len(request.line) {
				err = io.ErrShortWrite
			}
			if err != nil {
				c.terminate(fmt.Errorf("pi stdin write: %w", err))
				return
			}
		}
	}
}

func (c *Client) readStdout() {
	defer c.readers.Done()
	reader := bufio.NewReader(c.stdout)
	for {
		// ReadBytes 没有 Scanner 的 64 KB 默认限制；仅 LF 分隔，保留 U+2028/U+2029。
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte{'\n'}), []byte{'\r'})
			if len(line) > 0 {
				var record map[string]any
				if decodeErr := json.Unmarshal(line, &record); decodeErr != nil || record == nil {
					c.terminate(fmt.Errorf("invalid pi stdout JSON record: %v", decodeErr))
					return
				}
				if !c.handleRecord(record) {
					return
				}
			}
		}
		if err != nil {
			c.terminate(fmt.Errorf("pi stdout closed: %w", err))
			return
		}
	}
}

func (c *Client) handleRecord(record map[string]any) bool {
	switch record["type"] {
	case "response":
		id, _ := record["id"].(string)
		c.mu.Lock()
		response := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if response != nil {
			response <- record
		}
		// 无 id 的 parse 错误及已取消命令的迟到响应不属于 run 事件。
		return true
	case "extension_ui_request":
		encoded, _ := json.Marshal(record)
		c.appendTail("[extension_ui_request] " + string(encoded))
		if mcpFailure(record) {
			c.terminate(fmt.Errorf("%w: %v", ErrUnhealthy, record["message"]))
			return false
		}
		return true
	}
	select {
	case c.events <- record:
		return true
	default:
		// 无法丢掉 tool/settled 事件，也不能阻塞 stdout 让 abort/get_state 饿死。
		c.terminate(ErrEventOverflow)
		return false
	}
}

// 1.0.0 的 MCP 扩展使用 warning + "servers need attention" 汇总连接错误，
// 并非总是 notifyType=error。连接中等正常通知不影响健康。
func mcpFailure(record map[string]any) bool {
	if record["method"] != "notify" {
		return false
	}
	message, _ := record["message"].(string)
	message = strings.ToLower(message)
	if !strings.Contains(message, "mcp") {
		return false
	}
	for _, marker := range []string{"need attention", "failed", "failure", "disconnected", "cannot connect", "connection error"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return record["notifyType"] == "error"
}

func (c *Client) readStderr() {
	defer c.readers.Done()
	reader := bufio.NewReader(c.stderr)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			c.appendTail(strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"))
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) appendTail(line string) {
	c.mu.Lock()
	c.tail[c.tailAt] = line
	c.tailAt = (c.tailAt + 1) % stderrLines
	if c.tailLen < stderrLines {
		c.tailLen++
	}
	c.mu.Unlock()
}

// terminate 只发出停止信号，不能等待 done：读写 goroutine 自身也会调用它。
func (c *Client) terminate(reason error) {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.reason = reason
		c.mu.Unlock()
		close(c.stop)
		c.cmd.Process.Kill()
		c.stdin.Close()
	})
}

func (c *Client) reap() {
	err := c.cmd.Wait()
	c.mu.Lock()
	c.exitErr = err
	c.mu.Unlock()
	c.terminate(errors.New("pi process exited"))
	readDone := make(chan struct{})
	go func() { c.readers.Wait(); close(readDone) }()
	// 极端情况下子进程的后代仍持有管道，不能让 Close 永久等待 EOF。
	timer := time.NewTimer(time.Second)
	select {
	case <-readDone:
	case <-timer.C:
		c.stdout.Close()
		c.stderr.Close()
		<-readDone
	}
	timer.Stop()
	c.stdout.Close()
	c.stderr.Close()
	<-c.writerDone
	close(c.events)
	close(c.done)
}

// Events 的通道在收尾后关闭；应同时检查 Done 和 Err，防止将退出当作正常完成。
func (c *Client) Events() <-chan map[string]any { return c.events }
func (c *Client) Done() <-chan struct{}         { return c.done }

// Healthy 在开始停止时立即变为 false，比 Done 关闭更早。
func (c *Client) Healthy() bool {
	select {
	case <-c.stop:
		return false
	default:
		return true
	}
}

// StderrTail 返回按时间排列的最近 200 行 stderr/扩展 UI 诊断。
func (c *Client) StderrTail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines := make([]string, c.tailLen)
	for i := range lines {
		lines[i] = c.tail[(c.tailAt-c.tailLen+i+stderrLines)%stderrLines]
	}
	return strings.Join(lines, "\n")
}

// Err 在正常运行时为 nil；Done 关闭后诊断尾部和退出状态已完整。
func (c *Client) Err() error {
	c.mu.Lock()
	reason, exitErr := c.reason, c.exitErr
	c.mu.Unlock()
	if reason == nil {
		return nil
	}
	return fmt.Errorf("%w (process: %v); stderr tail:\n%s", reason, exitErr, c.StderrTail())
}

// Close 幂等，强制杀进程并等待所有协议 I/O 收尾。
func (c *Client) Close() {
	c.terminate(ErrClosed)
	<-c.done
}

// drainEvents 只在调用方已停止事件消费者、归还独占进程后由池调用。
func (c *Client) drainEvents() {
	for {
		select {
		case _, ok := <-c.events:
			if !ok {
				return
			}
		default:
			return
		}
	}
}
