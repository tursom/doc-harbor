package pirpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func testPool(t *testing.T, size, maxRuns int) *Pool {
	t.Helper()
	p := NewPool(size, maxRuns)
	t.Cleanup(p.Close)
	return p
}

func acquireOK(t *testing.T, p *Pool, spec Spec) *Client {
	t.Helper()
	c, err := p.Acquire(testContext(t), spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPoolLazyCapacityAndSpare(t *testing.T) {
	p := testPool(t, 3, 100)
	if stats := p.Stats(); stats.Total != 0 {
		t.Fatalf("pool not lazy: %+v", stats)
	}
	spec := fakeSpec(t, "normal")
	first := acquireOK(t, p, spec)
	eventually(t, func() bool { return p.Stats().Idle >= 1 })
	if stats := p.Stats(); stats.Busy != 1 || stats.Total != 2 {
		t.Fatalf("unexpected warm pool: %+v", stats)
	}
	second := acquireOK(t, p, spec)
	third := acquireOK(t, p, spec)
	if first == second || first == third || second == third {
		t.Fatal("process leased to concurrent callers")
	}
	if stats := p.Stats(); stats.Busy != 3 || stats.Total != 3 || stats.Idle != 0 {
		t.Fatalf("pool exceeded capacity or missed lease: %+v", stats)
	}
	p.Release(first)
	p.Release(second)
	p.Release(third)
	eventually(t, func() bool { return p.Stats().Idle == 3 })
}

func TestPoolFIFOAndQueueCancellation(t *testing.T) {
	p := testPool(t, 1, 100)
	spec := fakeSpec(t, "normal")
	held := acquireOK(t, p, spec)
	results := make(chan struct {
		index  int
		client *Client
		err    error
	}, 3)
	positions := make(chan int, 10)
	var cancelMiddle context.CancelFunc
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		if i == 1 {
			cancelMiddle = cancel
		}
		go func(i int, ctx context.Context) {
			c, err := p.Acquire(ctx, spec, func(position int) {
				if i == 2 {
					positions <- position
				}
			})
			results <- struct {
				index  int
				client *Client
				err    error
			}{i, c, err}
		}(i, ctx)
		want := i + 1
		eventually(t, func() bool { return p.Stats().Queued == want })
	}
	select {
	case pos := <-positions:
		if pos != 3 {
			t.Fatalf("initial position = %d", pos)
		}
	case <-testContext(t).Done():
		t.Fatal("queue callback missing")
	}
	cancelMiddle()
	cancelled := <-results
	if cancelled.index != 1 || !errors.Is(cancelled.err, context.Canceled) {
		t.Fatalf("middle cancellation = %+v", cancelled)
	}
	select {
	case pos := <-positions:
		if pos != 2 {
			t.Fatalf("position after cancellation = %d", pos)
		}
	case <-testContext(t).Done():
		t.Fatal("updated queue position missing")
	}
	p.Release(held)
	first := <-results
	if first.index != 0 || first.err != nil || first.client != held {
		t.Fatalf("FIFO first = %+v", first)
	}
	p.Release(first.client)
	last := <-results
	if last.index != 2 || last.err != nil || last.client != held {
		t.Fatalf("FIFO last = %+v", last)
	}
	p.Release(last.client)
}

func TestPoolMaxRunsAndEventDrain(t *testing.T) {
	p := testPool(t, 1, 2)
	spec := fakeSpec(t, "normal")
	first := acquireOK(t, p, spec)
	commandOK(t, first, "prompt", map[string]any{"message": "question"})
	commandOK(t, first, "abort", nil)
	p.Release(first)
	p.Release(first) // 重复归还不得重复累计 runs。
	second := acquireOK(t, p, spec)
	if first != second || len(second.events) != 0 {
		t.Fatal("first release recycled too early or retained events")
	}
	p.Release(second)
	third := acquireOK(t, p, spec)
	if third == first {
		t.Fatal("maxRuns did not recycle client")
	}
	waitDone(t, first)
	p.Release(third)
}

func TestPoolConfigurationRollingRestart(t *testing.T) {
	p := testPool(t, 2, 100)
	spec := fakeSpec(t, "normal")
	busy := acquireOK(t, p, spec)
	eventually(t, func() bool { return p.Stats().Idle == 1 })
	var oldIdle *Client
	p.mu.Lock()
	for entry := range p.entries {
		if !entry.busy {
			oldIdle = entry.client
		}
	}
	p.mu.Unlock()
	spec.Version = "v2"
	spec.Env = append(spec.Env, "PIRPC_VERSION=v2")
	if err := p.UpdateSpec(spec); err != nil {
		t.Fatal(err)
	}
	waitDone(t, oldIdle)
	if !busy.Healthy() {
		t.Fatal("configuration change terminated busy process")
	}
	// 旧 busy 未归还时，新的空闲槽位已替换成 v2。
	fresh := acquireOK(t, p, spec)
	if data := commandOK(t, fresh, "get_state", nil)["data"].(map[string]any); data["version"] != "v2" {
		t.Fatalf("new lease did not use v2: %v", data)
	}
	p.Release(busy)
	waitDone(t, busy)
	p.Release(fresh)
	eventually(t, func() bool { return p.Stats().Idle >= 1 })
	if stats := p.Stats(); stats.Total > 2 {
		t.Fatalf("rolling restart exceeded capacity: %+v", stats)
	}
}

func TestPoolExitReplacementWithoutRelease(t *testing.T) {
	p := testPool(t, 1, 100)
	spec := fakeSpec(t, "normal")
	first := acquireOK(t, p, spec)
	_, err := first.Command(testContext(t), "exit", nil)
	if err == nil {
		t.Fatal("exit command succeeded")
	}
	// 即使 run 还未执行 Release，已退出进程也不再占据池容量。
	second := acquireOK(t, p, spec)
	if first == second {
		t.Fatal("pool reused exited process")
	}
	p.Release(first)
	p.Release(second)
}

func TestPoolCloseQueuedBusyAndStarting(t *testing.T) {
	for _, mode := range []string{"normal", "startup_hang"} {
		t.Run(mode, func(t *testing.T) {
			p := testPool(t, 1, 100)
			spec := fakeSpec(t, mode)
			var busy *Client
			if mode == "normal" {
				busy = acquireOK(t, p, spec)
			}
			result := make(chan error, 1)
			go func() { _, err := p.Acquire(testContext(t), spec, nil); result <- err }()
			eventually(t, func() bool { return p.Stats().Queued == 1 })
			p.Close()
			p.Close()
			if err := <-result; !errors.Is(err, ErrPoolClosed) {
				t.Fatalf("queued close error = %v", err)
			}
			if busy != nil {
				waitDone(t, busy)
			}
			if stats := p.Stats(); stats.Total != 0 || stats.Queued != 0 {
				t.Fatalf("close leaked resources: %+v", stats)
			}
			if _, err := p.Acquire(testContext(t), spec, nil); !errors.Is(err, ErrPoolClosed) {
				t.Fatalf("closed Acquire = %v", err)
			}
		})
	}
}

func TestPoolStartupFailureAndRecovery(t *testing.T) {
	p := testPool(t, 1, 100)
	spec := fakeSpec(t, "startup_fail")
	if c, err := p.Acquire(testContext(t), spec, nil); c != nil || err == nil {
		t.Fatalf("bad startup acquired: %v %v", c, err)
	}
	spec.Env = []string{"PIRPC_FAKE=normal"}
	spec.Version = "recovered"
	c := acquireOK(t, p, spec)
	p.Release(c)
}

func TestPoolConfigurationCancelsOldStartup(t *testing.T) {
	p := testPool(t, 1, 100)
	spec := fakeSpec(t, "startup_hang")
	if err := p.UpdateSpec(spec); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return p.Stats().Starting == 1 })
	spec.Version = "v2"
	spec.Env = []string{"PIRPC_FAKE=normal", "PIRPC_VERSION=v2"}
	ctx, cancel := context.WithTimeout(testContext(t), time.Second)
	defer cancel()
	c, err := p.Acquire(ctx, spec, nil)
	if err != nil {
		t.Fatalf("old startup blocked new configuration: %v", err)
	}
	p.Release(c)
}

func TestPoolConcurrentCancellationDoesNotLeakLeases(t *testing.T) {
	p := testPool(t, 3, 10)
	spec := fakeSpec(t, "normal")
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(testContext(t), time.Duration(i%5+1)*time.Millisecond)
			defer cancel()
			c, err := p.Acquire(ctx, spec, nil)
			if err == nil {
				p.Release(c)
			}
		}(i)
	}
	wg.Wait()
	eventually(t, func() bool { stats := p.Stats(); return stats.Busy == 0 && stats.Queued == 0 && stats.Idle >= 1 })
	if stats := p.Stats(); stats.Total > 3 {
		t.Fatal(fmt.Sprintf("capacity exceeded: %+v", stats))
	}
}
