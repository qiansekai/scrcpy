package task

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scrcpy-lan/webui/internal/device"
)

// TestExecBatch_Aggregation 验证结果按输入顺序返回，且各字段正确映射。
func TestExecBatch_Aggregation(t *testing.T) {
	old := execCommandFunc
	defer func() { execCommandFunc = old }()

	var mu sync.Mutex
	calls := make(map[string]int)
	execCommandFunc = func(id, cmd string) (device.ExecResult, error) {
		mu.Lock()
		calls[id]++
		mu.Unlock()
		switch id {
		case "a":
			return device.ExecResult{ExitCode: 0, Stdout: "A-out", Stderr: ""}, nil
		case "b":
			return device.ExecResult{ExitCode: 1, Stdout: "", Stderr: "B-err"}, nil
		default:
			return device.ExecResult{}, errors.New("boom:" + id)
		}
	}

	got := ExecBatch(context.Background(), nil, []string{"a", "b", "c"}, "ls")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	// a：成功
	if got[0].ID != "a" || !got[0].Ok || got[0].ExitCode != 0 || got[0].Stdout != "A-out" {
		t.Fatalf("a = %+v", got[0])
	}
	// b：exit code 非 0 但仍 Ok=true（shell 正常执行，只是退出码）
	if got[1].ID != "b" || !got[1].Ok || got[1].ExitCode != 1 || got[1].Stderr != "B-err" {
		t.Fatalf("b = %+v", got[1])
	}
	// c：exec 错误
	if got[2].ID != "c" || got[2].Ok || got[2].Error != "boom:c" {
		t.Fatalf("c = %+v", got[2])
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != 1 || calls["b"] != 1 || calls["c"] != 1 {
		t.Fatalf("calls = %v", calls)
	}
}

// TestExecBatch_Dedup 验证输入去重且保持顺序。
func TestExecBatch_Dedup(t *testing.T) {
	old := execCommandFunc
	defer func() { execCommandFunc = old }()

	var mu sync.Mutex
	var seen []string
	execCommandFunc = func(id, cmd string) (device.ExecResult, error) {
		mu.Lock()
		seen = append(seen, id)
		mu.Unlock()
		return device.ExecResult{ExitCode: 0}, nil
	}

	got := ExecBatch(context.Background(), nil, []string{"x", "y", "x", "z", "y"}, "id")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (deduped)", len(got))
	}
	if got[0].ID != "x" || got[1].ID != "y" || got[2].ID != "z" {
		t.Fatalf("order = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("exec called %d times, want 3", len(seen))
	}
}

// TestExecBatch_Empty 验证空 ids 与 nil 输入安全。
func TestExecBatch_Empty(t *testing.T) {
	if got := ExecBatch(context.Background(), nil, nil, "ls"); got == nil || len(got) != 0 {
		t.Fatalf("empty ids = %#v, want empty slice", got)
	}
	if got := ExecBatch(context.Background(), nil, []string{}, "ls"); got == nil || len(got) != 0 {
		t.Fatalf("nil ids = %#v, want empty slice", got)
	}
}

// TestExecBatch_ConcurrencyLimit 验证并发上限 16：用慢 exec 观察瞬时并发峰值。
func TestExecBatch_ConcurrencyLimit(t *testing.T) {
	old := execCommandFunc
	defer func() { execCommandFunc = old }()

	var cur, peak int64
	total := 64
	execCommandFunc = func(id, cmd string) (device.ExecResult, error) {
		c := atomic.AddInt64(&cur, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if c <= p || atomic.CompareAndSwapInt64(&peak, p, c) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt64(&cur, -1)
		return device.ExecResult{ExitCode: 0}, nil
	}

	ids := make([]string, total)
	for i := range ids {
		ids[i] = string(rune('a'+i%26)) + string(rune('0'+i))
	}
	ExecBatch(context.Background(), nil, ids, "ls")
	if got := atomic.LoadInt64(&peak); got > maxConcurrency {
		t.Fatalf("peak concurrency = %d, want ≤ %d", got, maxConcurrency)
	}
	if got := atomic.LoadInt64(&peak); got == 0 {
		t.Fatal("peak concurrency stayed 0, exec never ran")
	}
}

// TestExecBatch_ContextCancel 验证 ctx 取消能中断等待中的任务。
func TestExecBatch_ContextCancel(t *testing.T) {
	old := execCommandFunc
	defer func() { execCommandFunc = old }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	started := int32(0)
	execCommandFunc = func(id, cmd string) (device.ExecResult, error) {
		atomic.AddInt32(&started, 1)
		return device.ExecResult{ExitCode: 0}, nil
	}

	got := ExecBatch(ctx, nil, []string{"a", "b", "c"}, "ls")
	for i, r := range got {
		if r.Ok || r.Error == "" {
			t.Fatalf("item %d = %+v, want cancelled error", i, r)
		}
	}
	if atomic.LoadInt32(&started) != 0 {
		t.Fatalf("exec ran %d times on cancelled ctx, want 0", started)
	}
}

// TestRunBatch_Order 直接验证 runBatch 的输入顺序稳定（用快 fn）。
func TestRunBatch_Order(t *testing.T) {
	ids := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
	got := runBatch(context.Background(), ids, func(ctx context.Context, id string) Result {
		return Result{ID: id, Ok: true}
	})
	if len(got) != len(ids) {
		t.Fatalf("len = %d, want %d", len(got), len(ids))
	}
	for i, r := range got {
		if r.ID != ids[i] {
			t.Fatalf("result[%d].ID = %q, want %q", i, r.ID, ids[i])
		}
	}
}

// TestRunBatch_NoLeakOnCancel 验证取消时信号量被正确释放（可再跑一批）。
func TestRunBatch_NoLeakOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	ids := make([]string, 40)
	for i := range ids {
		ids[i] = string(rune('a'+i/10)) + string(rune('0'+i%10))
	}
	// 第一批阻塞在 exec 上，然后取消。
	block := make(chan struct{})
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = runBatch(ctx, ids, func(ctx context.Context, id string) Result {
			select {
			case <-block:
				return Result{ID: id, Ok: true}
			case <-ctx.Done():
				return Result{ID: id, Ok: false, Error: ctx.Err().Error()}
			}
		})
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	wg.Wait()
	// 第二批在正常 ctx 下应能完整跑完（信号量不泄漏）。
	ids2 := []string{"a", "b", "c"}
	got := runBatch(context.Background(), ids2, func(ctx context.Context, id string) Result {
		return Result{ID: id, Ok: true}
	})
	if len(got) != 3 {
		t.Fatalf("second batch len = %d, want 3 (semaphore leak?)", len(got))
	}
	// 释放阻塞的 goroutine（它们已在 cancel 前退出，此处仅为安全清理）。
	close(block)
}
