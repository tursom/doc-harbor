# pi RPC 客户端和进程池接入约定

本包独立使用 Go 标准库，实现实施文档 §5.3/§5.4，不负责 run、会话互斥、SSE、超时 abort 或工具调用数限制。

```go
type Spec struct {
    Bin string
    Args []string
    Dir string
    Env []string
    Version string
}
func Start(ctx context.Context, spec Spec) (*Client, error)
func (c *Client) Command(ctx context.Context, typ string, fields map[string]any) (map[string]any, error)
func (c *Client) Events() <-chan map[string]any
func (c *Client) Done() <-chan struct{}
func (c *Client) Err() error
func (c *Client) StderrTail() string
func (c *Client) Healthy() bool
func (c *Client) Selection() (provider, model string)
func (c *Client) Close()

func NewPool(size, maxRuns int) *Pool
func (p *Pool) Acquire(ctx context.Context, spec Spec, onQueue func(int)) (*Client, error)
func (p *Pool) AcquireLatest(ctx context.Context, onQueue func(int)) (*Client, error)
func (p *Pool) UpdateSpec(spec Spec) error
func (p *Pool) Release(client *Client)
func (p *Pool) Stats() Stats
func (p *Pool) Close()
type Stats struct { Total, Busy, Idle, Starting, Queued int }
```

- `Spec.Args` 是完整参数，本包不追加 pi 参数；`Env` 是完整白名单环境，空值意味着空环境，绝不合并 `os.Environ()`。app 生成隔离目录、参数和白名单。建议 `Bin` 使用绝对路径；裸名称只在 `Spec.Env` 的 PATH 中查找。
- `Start` 先执行 `get_state`，默认最多等待 15 秒，也受传入 ctx 截止时间限制。ctx 只控制启动，不作为常驻进程的生命周期；启动成功后由 `Close` 负责释放。
- `Command` 返回完整 response（含 `success`、`data`），`success:false` 同时返回 error。内部覆盖 fields 的 `type`/`id`，支持并发命令和乱序响应。取消等待不会自动发 abort；若取消发生在阻塞写管道期间，会销毁进程以解除写阻塞。
- `Events()` 返回进程级单消费者通道，从启动即收集事件。主会话必须在 prompt 前开始消费，按 `agent_settled` 判断完成；prompt 的 `data.disposition=="handled"` 时无需等待该事件。带 id 的非 response 仍是事件。
- 事件通道容量为 256 条；持续不消费导致溢出时，以明确错误销毁进程，避免静默丢事件或堵住 stdout/命令响应。归还前停止当前消费者；`Release` 清除残留事件。下次借出不会收到上次已缓存的事件。
- `extension_ui_request` 仅记入诊断尾部，不回复、不作为 run 事件。pi 1.0.0 的 `MCP servers need attention`、`MCP failed to load` 等失败通知会标记 unhealthy 并终止进程；普通 MCP 连接中提示不影响健康。MCP 后台连接失败可以晚于 get_state，主会话持续监听 Done。
- `Done` 在进程已退出、stdout/stderr 已收尾、事件通道已关闭后关闭；此后 `Err()` 含退出原因和最近 200 行诊断。运行时必须同时监听 Events/Done，不能只等待 agent_settled。`Close` 可重复调用，杀进程并等待收尾。
- `NewPool` 不启动进程。第一次 Acquire 或 UpdateSpec 后开始按需创建，在最大容量允许时保留至少一个空闲进程。size<=0 按 1，maxRuns<=0 按 100。
- Acquire 按入队顺序分配，ctx 可取消；onQueue 的正整数表示队列位置（1 为队首），在该 Acquire 的调用 goroutine 内执行，应及时返回。位置变化合并通知，不调用位置 0；取得进程后的 started 由 app 推送。
- `UpdateSpec` 用于配置保存后立即通知池。Version 是配置身份，改变 Version 才触发替换；同版本参数应相同。Acquire 也会应用传入的新版本。排队请求使用池的最新配置；旧版本空闲进程立即回收，仍在启动中的进程立即取消，忙进程归还后回收。配置更新和 Acquire 应传同一份最新版本，避免过时调用覆盖配置。
- 每次有效 Release 计一次借用，达到 maxRuns 时回收。重复 Release、外来 Client 忽略。Release 不发 abort/切会话；app 必须在 run 和所有命令结束后归还。不健康或已退出进程自动替换，启动失败返回队首请求，后台补空闲进程按 1 秒间隔重试。
- Pool.Close 取消启动、唤醒排队请求、杀死所有子进程并等待后台工作结束，可重复调用。
- app 使用 `UpdateSpec` 发布、`AcquireLatest` 入队，后者不会回写配置。借出后通过 `Selection` 获取该进程实际启动时的默认 provider/model；不能沿用入队前读取的旧默认值。`Stats` 的 JSON 字段使用小写。

已完整核对本机 `@earendil-works/pi-coding-agent@1.0.0` 的 README、rpc.md、session-format.md、settings.md、models.md，以及相关 RPC commands、JSON events、extension UI、message types、CLI、configuration、MCP、environment variables 文档和已安装 dist 实现。
