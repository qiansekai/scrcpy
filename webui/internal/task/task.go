// Package task 提供批量 shell 执行、批量安装 APK 与批量推送文件。
//
// 本包只依赖 device 包与标准库，禁止 import cycle。所有导出函数对 nil
// 与空参数都安全（返回空切片、不 panic）。真正的推送走 admin 协议
// TYPE_PUSH（0x11），依赖设备端 AdminServer 支持（见 docs/superpowers/specs
// 下 2026-08-27-webui-full-stack.md 的 2.1 节），集成者负责补齐该协议与
// device.Manager.AdminAddr。
package task

import (
	"context"
	"sync"

	"scrcpy-lan/webui/internal/device"
)

// maxConcurrency 是批量任务并发上限：每设备一个 goroutine，最多同时跑 16 个。
const maxConcurrency = 16

// Result 是单台设备上的一次任务结果。
type Result struct {
	ID       string `json:"id"`
	Ok       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Remote   string `json:"remote,omitempty"`
}

// execFunc 是执行单条 shell 命令的函数签名，批量任务用它做批处理。
type execFunc func(id, cmd string) (device.ExecResult, error)

// execCommandFunc 是可注入的 shell 执行函数。生产环境由 ExecBatch/InstallAPK
// 内部绑定到 mgr.ExecCommand；测试时改写它以 mock，避免依赖真实设备拨号。
var execCommandFunc execFunc

// execFor 返回针对某个 Manager 的执行函数。优先使用注入的 mock（execCommandFunc），
// 否则回退到 mgr.ExecCommand；mgr 为 nil 时返回 nil。
func execFor(mgr *device.Manager) execFunc {
	if execCommandFunc != nil {
		return execCommandFunc
	}
	if mgr == nil {
		return nil
	}
	return mgr.ExecCommand
}

// dedupe 按输入顺序去掉重复 id，返回去重后的切片。
func dedupe(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// runBatch 在每台设备上并发执行 fn，并用信号量把并发限制在 maxConcurrency。
// 结果严格按 ids（去重后）的输入顺序返回。ctx 取消时剩余任务快速退出。
func runBatch(ctx context.Context, ids []string, fn func(ctx context.Context, id string) Result) []Result {
	ids = dedupe(ids)
	if len(ids) == 0 {
		return []Result{}
	}

	results := make([]Result, len(ids))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i, id := range ids {
		// ctx 已取消就不再启动新任务，直接记为取消错误。
		if ctx.Err() != nil {
			results[i] = Result{ID: id, Ok: false, Error: ctx.Err().Error()}
			continue
		}

		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()

			// 获取信号量令牌；ctx 取消可中断等待，避免 goroutine 泄漏。
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = Result{ID: id, Ok: false, Error: ctx.Err().Error()}
				return
			}

			results[i] = fn(ctx, id)
		}(i, id)
	}

	wg.Wait()
	return results
}

// ExecBatch 并行在每台设备上执行同一条 shell 命令（复用 device.Manager.ExecCommand）。
// 设备无会话时 ExecCommand 已返回 error，这里记 Result{Ok:false, Error:...}。
// mgr 为 nil 且未注入 execCommandFunc 时返回空切片（与空 ids 一致）。
func ExecBatch(ctx context.Context, mgr *device.Manager, ids []string, cmd string) []Result {
	exec := execFor(mgr)
	if exec == nil {
		// 无 manager 且无注入的 exec，按空结果处理，不 panic。
		return []Result{}
	}
	return runBatch(ctx, ids, func(ctx context.Context, id string) Result {
		if ctx.Err() != nil {
			return Result{ID: id, Ok: false, Error: ctx.Err().Error()}
		}
		res, err := exec(id, cmd)
		if err != nil {
			return Result{ID: id, Ok: false, Error: err.Error()}
		}
		return Result{
			ID:       id,
			Ok:       true,
			ExitCode: res.ExitCode,
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
		}
	})
}
