package status

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"scrcpy-lan/webui/internal/device"
)

// ---- parseBattery 表驱动（真实风格样例） ----

func TestParseBattery(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want int
		plug bool
	}{
		{
			name: "标准样例(行首空格)",
			out:  "Current Battery Service state:\n  AC powered: false\n  level: 87\n",
			want: 87,
			plug: false,
		},
		{
			name: "满电 AC 充电",
			out:  "Current Battery Service state:\n  AC powered: true\n  USB powered: true\n  level: 100\n",
			want: 100,
			plug: true,
		},
		{
			name: "CRLF 行尾 + 行首空白",
			out:  "Current Battery Service state:\r\n    AC powered: false\r\n    USB powered: false\r\n    level: 42\r\n",
			want: 42,
			plug: false,
		},
		{
			name: "仅 USB 供电",
			out:  "  USB powered: true\n  AC powered: false\n  level: 63\n",
			want: 63,
			plug: true,
		},
		{
			name: "缺 level 行",
			out:  "Current Battery Service state:\n  AC powered: true\n  USB powered: false\n",
			want: -1,
			plug: true,
		},
		{
			name: "空输出",
			out:  "",
			want: -1,
			plug: false,
		},
		{
			name: "乱码",
			out:  "\x00\xff\xfe garbage \x01",
			want: -1,
			plug: false,
		},
		{
			name: "level 值为非数字",
			out:  "  level: unknown\n  AC powered: false\n",
			want: -1,
			plug: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, p := parseBattery(tc.out)
			if b != tc.want || p != tc.plug {
				t.Fatalf("parseBattery(%q) = (%d, %v), want (%d, %v)",
					tc.out, b, p, tc.want, tc.plug)
			}
		})
	}
}

// ---- 注入 exec 的采样与缓存语义 ----

// fakeExec 用命令字符串映射假输出，支持按 id 区分。
type fakeExec struct {
	mu   sync.Mutex
	seen map[string]int // id -> 采样次数
	acts map[string]func(id, cmd string) (device.ExecResult, error)
}

func newFakeExec() *fakeExec {
	return &fakeExec{seen: make(map[string]int), acts: make(map[string]func(id, cmd string) (device.ExecResult, error))}
}

func (f *fakeExec) set(id, cmd string, res device.ExecResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acts[id+"|"+cmd] = func(string, string) (device.ExecResult, error) { return res, err }
}

func (f *fakeExec) run(id, cmd string) (device.ExecResult, error) {
	f.mu.Lock()
	f.seen[id]++
	act := f.acts[id+"|"+cmd]
	f.mu.Unlock()

	// 单独统计 dumpsys 次数用于验证采样轮数，用通用计数即可。
	if act == nil {
		res := device.ExecResult{}
		switch cmd {
		case "dumpsys battery":
			res.Stdout = "  AC powered: false\n  level: 80\n"
		case "getprop ro.product.model":
			res.Stdout = "Pixel 7a"
		case "getprop ro.build.version.release":
			res.Stdout = "14"
		}
		return res, nil
	}
	return act(id, cmd)
}

func (f *fakeExec) dumpsysCount(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[id]
}

// newTestCollector 构造一个用 fakeExec 的采集器（不经真实网络）。
func newTestCollector(fe *fakeExec) *Collector {
	c := NewCollector(nil)
	c.exec = fe.run
	return c
}

func TestTrackSnapshotAllUntrack(t *testing.T) {
	fe := newFakeExec()
	c := newTestCollector(fe)

	// 未 Track 前 Snapshot 返回不存在。
	if _, ok := c.Snapshot("a"); ok {
		t.Fatalf("未 Track 的设备应返回 ok=false")
	}

	c.Track([]string{"a", "b"})

	// 新语义：Snapshot 只读缓存，采样需显式触发（后台循环或 sampleOne）。
	c.sampleOne("a")
	c.sampleOne("b")
	info, ok := c.Snapshot("a")
	if !ok {
		t.Fatalf("已 Track 的设备应返回 ok=true")
	}
	if info.ID != "a" || !info.Online || info.Battery != 80 {
		t.Fatalf("Snapshot(a) = %+v, 预期在线且电量 80", info)
	}
	if info.Model != "Pixel 7a" || info.Android != "14" {
		t.Fatalf("Snapshot(a) 型号/版本解析错误: %+v", info)
	}

	// SnapshotAll 包含两台（均有缓存）。
	all := c.SnapshotAll()
	if len(all) != 2 {
		t.Fatalf("SnapshotAll 长度 = %d, 预期 2", len(all))
	}

	// Track 合并不删除旧设备：Track 新设备 c（未采样，无缓存 → 不出现），
	// 旧设备 a/b 仍在。采样 c 后应为 3。
	c.Track([]string{"c"})
	c.sampleOne("c")
	all = c.SnapshotAll()
	if len(all) != 3 {
		t.Fatalf("Track 合并后 SnapshotAll 长度 = %d, 预期 3", len(all))
	}

	// 缓存命中：再次 Snapshot 不增加 dumpsys 次数。
	before := fe.dumpsysCount("a")
	_, _ = c.Snapshot("a")
	if fe.dumpsysCount("a") != before {
		t.Fatalf("缓存命中不应重新采样")
	}

	// Untrack 移除设备及其缓存。
	c.Untrack("a")
	if _, ok := c.Snapshot("a"); ok {
		t.Fatalf("Untrack 后 Snapshot 应返回 ok=false")
	}
	all = c.SnapshotAll()
	if len(all) != 2 {
		t.Fatalf("Untrack 后 SnapshotAll 长度 = %d, 预期 2", len(all))
	}
}

func TestSampleOfflineKeepsOldValues(t *testing.T) {
	fe := newFakeExec()
	// 首次在线。
	c := newTestCollector(fe)
	c.Track([]string{"x"})

	// 首次在线（显式采样后读缓存）。
	c.sampleOne("x")
	info, _ := c.Snapshot("x")
	if !info.Online || info.Model != "Pixel 7a" || info.Android != "14" {
		t.Fatalf("首采样应在线且带型号/版本: %+v", info)
	}

	// dumpsys 失败 → 离线、Battery=-1，型号/版本保留旧值（缓存未被清除时）。
	fe.set("x", "dumpsys battery", device.ExecResult{}, fmt.Errorf("dial timeout"))
	// 直接触发一次采样（不经 Snapshot 的缓存短路），保留缓存以验证"保留旧值"。
	c.sampleOne("x")
	info2, _ := c.Snapshot("x")
	if info2.Online || info2.Battery != -1 {
		t.Fatalf("dumpsys 失败应离线且电量 -1: %+v", info2)
	}
	if info2.Model != "Pixel 7a" || info2.Android != "14" {
		t.Fatalf("离线应保留旧型号/版本: %+v", info2)
	}
}

func TestSetOnChange(t *testing.T) {
	fe := newFakeExec()
	c := newTestCollector(fe)
	c.Track([]string{"d"})

	var mu sync.Mutex
	var events []Info
	cancel := c.SetOnChange(func(old, new Info) {
		mu.Lock()
		events = append(events, new)
		mu.Unlock()
	})

	// 首次采样：无旧值，应触发。
	c.sampleOne("d")
	if _, ok := c.Snapshot("d"); !ok {
		t.Fatal("Snapshot 失败")
	}
	mu.Lock()
	n := len(events)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("首次采样应触发 1 次回调, got %d", n)
	}

	// 值未变（缓存命中）不应触发。
	_, _ = c.Snapshot("d")
	mu.Lock()
	n = len(events)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("值未变不应触发回调, got %d", n)
	}

	// 变更 level → 触发（重新采样）。
	fe.set("d", "dumpsys battery", device.ExecResult{Stdout: "  AC powered: false\n  level: 50\n"}, nil)
	c.sampleOne("d")
	mu.Lock()
	n = len(events)
	mu.Unlock()
	if n != 2 {
		t.Fatalf("电量变化应再触发 1 次, got %d", n)
	}

	// 取消回调后不再触发。
	cancel()
	fe.set("d", "dumpsys battery", device.ExecResult{Stdout: "  AC powered: false\n  level: 30\n"}, nil)
	c.sampleOne("d")
	mu.Lock()
	n = len(events)
	mu.Unlock()
	if n != 2 {
		t.Fatalf("取消回调后不应触发, got %d", n)
	}
}

func TestStartStop(t *testing.T) {
	fe := newFakeExec()
	c := newTestCollector(fe)
	c.Track([]string{"s"})

	c.Start(10 * time.Millisecond)
	// 等待首次定时采样发生。
	deadline := time.Now().Add(500 * time.Millisecond)
	for fe.dumpsysCount("s") == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if fe.dumpsysCount("s") == 0 {
		t.Fatalf("Start 后未发生定时采样")
	}

	c.Stop() // 幂等
	c.Stop()

	count := fe.dumpsysCount("s")
	time.Sleep(30 * time.Millisecond)
	if fe.dumpsysCount("s") != count {
		t.Fatalf("Stop 后不应继续采样")
	}
}

func TestStartDefaultInterval(t *testing.T) {
	c := NewCollector(nil)
	// Start(<=0) 应使用默认间隔而非 panic；立即 Stop 验证幂等与退出。
	c.Start(0)
	c.Start(-time.Second)
	c.Stop()
}
