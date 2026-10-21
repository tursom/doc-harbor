package pirpc

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrPoolClosed = errors.New("pi process pool closed")

// Stats 是同一把锁下取得的快照。Total 包含正在启动或正在退出的进程，
// 因此即使滚动替换时也不会在旧进程退出前突破配置容量。
type Stats struct {
	Total    int `json:"total"`
	Busy     int `json:"busy"`
	Idle     int `json:"idle"`
	Starting int `json:"starting"`
	Queued   int `json:"queued"`
}

type acquireResult struct {
	client *Client
	err    error
}

type waiter struct {
	ctx     context.Context
	result  chan acquireResult
	changed chan struct{}
}

type poolEntry struct {
	client   *Client
	version  string
	cancel   context.CancelFunc
	busy     bool
	retiring bool
	runs     int
}

// Pool 使用一条 FIFO 等待队列和一个调度循环，不在 Acquire 调用栈里启动进程。
// 这样取消排队不必等待其他进程冷启动，慢启动也不会改变请求的分配顺序。
type Pool struct {
	mu         sync.Mutex
	size       int
	maxRuns    int
	spec       Spec
	hasSpec    bool
	closed     bool
	entries    map[*poolEntry]struct{}
	queue      []*waiter
	retryAt    time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	wake       chan struct{}
	workerDone chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
	workers    sync.WaitGroup
}

// NewPool 本身不启动任何进程。首次提供 Spec 后按需创建，并在容量允许时
// 预留一个空闲进程。无效的 size/maxRuns 分别使用 1/100。
func NewPool(size, maxRuns int) *Pool {
	if size <= 0 {
		size = 1
	}
	if maxRuns <= 0 {
		maxRuns = 100
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		size: size, maxRuns: maxRuns, entries: make(map[*poolEntry]struct{}),
		ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
		workerDone: make(chan struct{}), done: make(chan struct{}),
	}
	go p.loop()
	return p
}

// UpdateSpec 在 app 重写隔离配置目录后调用，使空闲进程立即滚动替换。
// 忙进程继续当前借用，归还后再替换；排队请求始终使用最新 Spec。
func (p *Pool) UpdateSpec(spec Spec) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.updateSpecLocked(spec)
}

func (p *Pool) updateSpecLocked(spec Spec) error {
	if p.closed {
		return ErrPoolClosed
	}
	if spec.Bin == "" {
		return errors.New("pi binary is empty")
	}
	if !p.hasSpec || p.spec.Version != spec.Version {
		p.spec, p.hasSpec = spec.clone(), true
		p.retryAt = time.Time{}
		// 尚未通过健康检测的旧配置进程也要立即取消，不能让它占满槽位
		// 直到旧 get_state 超时，阻塞新配置的排队请求。
		for entry := range p.entries {
			if entry.client == nil && entry.version != spec.Version {
				entry.cancel()
			}
		}
		p.signal()
	}
	return nil
}

// Acquire 按入队顺序分配健康进程。onQueue 在调用 Acquire 的 goroutine 中执行，
// 不持有池锁；位置从 1 开始，连续变化可以合并，回调应及时返回。
func (p *Pool) Acquire(ctx context.Context, spec Spec, onQueue func(int)) (*Client, error) {
	return p.acquire(ctx, &spec, onQueue)
}

// AcquireLatest 使用 UpdateSpec 已发布的最新配置，避免迟到的请求回写旧版本。
func (p *Pool) AcquireLatest(ctx context.Context, onQueue func(int)) (*Client, error) {
	return p.acquire(ctx, nil, onQueue)
}

func (p *Pool) acquire(ctx context.Context, spec *Spec, onQueue func(int)) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &waiter{ctx: ctx, result: make(chan acquireResult, 1), changed: make(chan struct{}, 1)}
	p.mu.Lock()
	if spec != nil {
		if err := p.updateSpecLocked(*spec); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	} else if p.closed || !p.hasSpec {
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return nil, ErrPoolClosed
		}
		return nil, errors.New("pi process pool has no configuration")
	}
	p.queue = append(p.queue, w)
	p.queueChangedLocked()
	p.signal()
	p.mu.Unlock()
	lastPosition := 0
	for {
		select {
		case result := <-w.result:
			// 分配和 ctx 取消可能同时发生；取消方负责归还已交付的进程。
			if err := ctx.Err(); err != nil {
				if result.client != nil {
					p.Release(result.client)
				}
				return nil, err
			}
			return result.client, result.err
		case <-ctx.Done():
			p.cancelWaiter(w)
			return nil, ctx.Err()
		case <-w.changed:
			position := p.position(w)
			if onQueue != nil && position > 0 && position != lastPosition {
				lastPosition = position
				onQueue(position)
			}
		}
	}
}

func (p *Pool) position(w *waiter) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, candidate := range p.queue {
		if candidate == w {
			return i + 1
		}
	}
	return 0
}

func (p *Pool) cancelWaiter(w *waiter) {
	p.mu.Lock()
	for i, candidate := range p.queue {
		if candidate == w {
			p.removeWaiterLocked(i)
			p.mu.Unlock()
			p.signal()
			return
		}
	}
	// 不在队列说明调度器已向缓冲通道交付结果，不再由取消方删除队列。
	select {
	case result := <-w.result:
		p.mu.Unlock()
		if result.client != nil {
			p.Release(result.client)
		}
	default:
		p.mu.Unlock()
	}
}

func (p *Pool) removeWaiterLocked(i int) *waiter {
	w := p.queue[i]
	copy(p.queue[i:], p.queue[i+1:])
	p.queue[len(p.queue)-1] = nil
	p.queue = p.queue[:len(p.queue)-1]
	p.queueChangedLocked()
	return w
}

func (p *Pool) queueChangedLocked() {
	for _, w := range p.queue {
		select {
		case w.changed <- struct{}{}:
		default:
		}
	}
}

// Release 只处理本池当前处于借出状态的 Client；重复归还或外来 Client 忽略。
// 调用方必须已等待 run 完成并停止消费事件，池不隐式发 abort。
func (p *Pool) Release(client *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for entry := range p.entries {
		if entry.client == client && entry.busy && !entry.retiring {
			client.drainEvents()
			entry.busy = false
			entry.runs++
			p.signal()
			return
		}
	}
}

func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := Stats{Total: len(p.entries), Queued: len(p.queue)}
	for entry := range p.entries {
		if entry.client == nil {
			stats.Starting++
		} else if entry.busy {
			stats.Busy++
		} else if !entry.retiring && entry.client.Healthy() {
			stats.Idle++
		}
	}
	return stats
}

func (p *Pool) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Pool) loop() {
	defer close(p.workerDone)
	// 定时器只用于启动失败后的补空闲重试，正常调度由 wake 立即唤醒。
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.wake:
		case <-ticker.C:
		}
		p.reconcile()
	}
}

func (p *Pool) reconcile() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !p.hasSpec {
		return
	}
	// 先清除取消请求，防止它占据队首或收到一次无意义的进程借用。
	for i := 0; i < len(p.queue); {
		if err := p.queue[i].ctx.Err(); err != nil {
			p.removeWaiterLocked(i).result <- acquireResult{err: err}
		} else {
			i++
		}
	}
	for entry := range p.entries {
		if entry.client == nil || entry.retiring || entry.busy {
			continue
		}
		if entry.version != p.spec.Version || entry.runs >= p.maxRuns || !entry.client.Healthy() {
			entry.retiring = true
			entry.client.terminate(ErrClosed)
			continue
		}
		if len(p.queue) > 0 {
			entry.busy = true
			p.removeWaiterLocked(0).result <- acquireResult{client: entry.client}
		}
	}
	if time.Now().Before(p.retryAt) {
		return
	}
	busy := 0
	for entry := range p.entries {
		if entry.busy {
			busy++
		}
	}
	// 一个请求占一个进程，再尽量预留一个空闲；正在退出的槽位也占容量。
	target := min(p.size, busy+len(p.queue)+1)
	for len(p.entries) < target {
		ctx, cancel := context.WithCancel(p.ctx)
		entry := &poolEntry{version: p.spec.Version, cancel: cancel}
		p.entries[entry] = struct{}{}
		p.workers.Add(1)
		go p.startEntry(ctx, entry, p.spec.clone())
	}
}

// 每个槽位只有一个生命周期 goroutine，负责从启动到退出以及从池里移除。
// Start 返回失败不会泄漏子进程；成功后监听 Done，忙进程异常退出也能立即补位。
func (p *Pool) startEntry(ctx context.Context, entry *poolEntry, spec Spec) {
	defer p.workers.Done()
	defer entry.cancel()
	client, err := Start(ctx, spec)
	p.mu.Lock()
	if err != nil {
		delete(p.entries, entry)
		if !p.closed && entry.version == p.spec.Version {
			p.retryAt = time.Now().Add(time.Second)
			if len(p.queue) > 0 {
				p.removeWaiterLocked(0).result <- acquireResult{err: err}
			}
		}
		p.mu.Unlock()
		p.signal()
		return
	}
	entry.client = client
	if p.closed || entry.version != p.spec.Version {
		entry.retiring = true
		client.terminate(ErrClosed)
	}
	p.mu.Unlock()
	p.signal()
	<-client.Done()
	p.mu.Lock()
	delete(p.entries, entry)
	// MCP 在后台连接，可能先通过 get_state，随后尚未借出就失败。
	// 将这种失败等同启动失败处理，避免空闲补位无限快速重启。
	if !p.closed && !entry.retiring && !entry.busy && entry.runs == 0 && entry.version == p.spec.Version {
		p.retryAt = time.Now().Add(time.Second)
		if len(p.queue) > 0 {
			p.removeWaiterLocked(0).result <- acquireResult{err: client.Err()}
		}
	}
	p.mu.Unlock()
	p.signal()
}

// Close 先阻止新调度并取消启动，再杀所有已启动进程；等待所有槽位收尾后返回。
// 不持有池锁等待 Client.Close，避免退出监听和 Release 形成互相等待。
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.cancel()
		for _, w := range p.queue {
			w.result <- acquireResult{err: ErrPoolClosed}
		}
		p.queue = nil
		for entry := range p.entries {
			if entry.client != nil {
				entry.client.terminate(ErrClosed)
			}
		}
		p.mu.Unlock()
		<-p.workerDone
		p.workers.Wait()
		close(p.done)
	})
	<-p.done
}
