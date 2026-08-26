// Package status 采集设备运行状态（电量 / 充电 / 型号 / 系统版本 / 在线）。
//
// Collector 通过 device.Manager.ExecCommand 在设备 admin 通道（27184）上执行
// `dumpsys battery` 与 `getprop` 命令获得原始文本，并在内部解析为 Info。
//
// 关键约束：
//   - Collector 不知道设备配置清单，设备集合由集成者通过 Track/Untrack 维护
//     （集成者通常在 store 配置加载 / 变更时同步调用）。
//   - Start 启动一个后台 goroutine 定时对全部已知设备串行采样；Stop 幂等，
//     调用后 goroutine 会退出。
//   - SetOnChange 注册的回调在采样 goroutine 中串行调用（同一时刻仅一个回调
//     在跑，且不会与采样并发），因此回调内应避免长阻塞以免拖慢采样节奏。
package status

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"scrcpy-lan/webui/internal/device"
)

// Info 是一台设备的一次状态采样结果。
type Info struct {
	ID      string `json:"id"`
	Online  bool   `json:"online"`
	Battery int    `json:"battery"` // -1 未知
	Plugged bool   `json:"plugged"`
	Model   string `json:"model"`
	Android string `json:"android"`
}

// defaultInterval 是 Start 未指定间隔（interval <= 0）时的默认采样周期。
const defaultInterval = 5 * time.Second

// execFn 对单台设备执行一条 shell 命令。测试中可替换以注入假输出。
type execFn func(id, cmd string) (device.ExecResult, error)

// onChange 封装一个变化回调及其稳定标识，便于按注册时的闭包解除注册。
type onChange struct {
	fn func(old, new Info)
}

// Collector 定时采集并缓存设备状态。
type Collector struct {
	mgr    *device.Manager
	exec   execFn // 默认走 mgr.ExecCommand；测试可注入
	mu     sync.Mutex
	known  map[string]struct{} // 已知设备集合
	cache  map[string]Info     // 最近一次采样缓存
	wake   chan struct{}       // 触发立即采样的信号（Snapshot 无缓存时用）
	done   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once // 保证 single goroutine 只启动一次
	onCh   []*onChange
	onChMu sync.Mutex
}

// NewCollector 构造采集器，不启动 goroutine。
func NewCollector(mgr *device.Manager) *Collector {
	return &Collector{
		mgr:   mgr,
		known: make(map[string]struct{}),
		cache: make(map[string]Info),
		wake:  make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
}

// Track 把 ids 合并进已知设备集合。已存在的设备不重置其缓存，旧设备
// （不在 ids 中）缓存与离线状态保留，需显式 Untrack 删除。
func (c *Collector) Track(ids []string) {
	c.mu.Lock()
	for _, id := range ids {
		if id == "" {
			continue
		}
		c.known[id] = struct{}{}
	}
	c.mu.Unlock()
}

// Untrack 从已知集合移除设备并删除其缓存。
func (c *Collector) Untrack(id string) {
	c.mu.Lock()
	delete(c.known, id)
	delete(c.cache, id)
	c.mu.Unlock()
}

// execCommand 返回实际执行的 exec 函数，默认走 mgr.ExecCommand。
func (c *Collector) execCommand() execFn {
	if c.exec != nil {
		return c.exec
	}
	return c.mgr.ExecCommand
}

// Start 启动后台采样 goroutine。interval <= 0 时使用默认间隔。重复调用
// Start 不会启动第二个 goroutine。
func (c *Collector) Start(interval time.Duration) {
	if interval <= 0 {
		interval = defaultInterval
	}
	c.once.Do(func() {
		c.wg.Add(1)
		go c.run(interval)
	})
}

// Stop 停止后台采样 goroutine。幂等；可在 Start 前安全调用（此时无实际效果）。
func (c *Collector) Stop() {
	select {
	case <-c.done:
		return
	default:
	}
	close(c.done)
	c.wg.Wait()
}

// run 是采样主循环：每到 interval 或收到唤醒信号，对全部已知设备串行采样。
func (c *Collector) run(interval time.Duration) {
	defer c.wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			c.sampleAll()
		case <-c.wake:
			c.sampleAll()
		}
	}
}

// scanIDs 返回当前已知设备 id 的快照。
func (c *Collector) scanIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(c.known))
	for id := range c.known {
		ids = append(ids, id)
	}
	return ids
}

// sampleAll 串行采样全部已知设备（单 goroutine，天然避免同台并发 exec）。
func (c *Collector) sampleAll() {
	for _, id := range c.scanIDs() {
		c.sampleOne(id)
	}
}

// sampleOne 采样单台设备并更新缓存、触发变化回调。执行期间持有 mu，
// 与 Snapshot 等读并发安全。
func (c *Collector) sampleOne(id string) {
	exec := c.execCommand()
	info := Info{ID: id, Battery: -1}

	batt, battErr := exec(id, "dumpsys battery")
	model, _ := exec(id, "getprop ro.product.model")
	android, _ := exec(id, "getprop ro.build.version.release")

	c.mu.Lock()
	prev, had := c.cache[id]

	// 在线以 dumpsys battery 是否成功为准；失败则 Battery=-1，其余保留旧值。
	if battErr != nil {
		info.Online = false
		info.Battery = -1
		if had {
			info.Model = prev.Model
			info.Android = prev.Android
		}
	} else {
		info.Online = true
		info.Battery, info.Plugged = parseBattery(batt.Stdout)
		info.Model = strings.TrimSpace(model.Stdout)
		info.Android = strings.TrimSpace(android.Stdout)
	}

	c.cache[id] = info
	c.mu.Unlock()

	if !had || changed(prev, info) {
		c.fireChange(prev, info)
	}
}

// changed 判断 battery/plugged/online 三个字段是否变化（回调触发条件）。
func changed(old, new Info) bool {
	return old.Battery != new.Battery || old.Plugged != new.Plugged || old.Online != new.Online
}

// fireChange 串行调用全部已注册回调。回调在采样 goroutine 中执行。
func (c *Collector) fireChange(old, new Info) {
	c.onChMu.Lock()
	callbacks := make([]*onChange, len(c.onCh))
	copy(callbacks, c.onCh)
	c.onChMu.Unlock()

	for _, cb := range callbacks {
		cb.fn(old, new)
	}
}

// SetOnChange 注册变化回调：当 battery/plugged/online 任一字段在两次采样间
// 变化时触发。回调在采样 goroutine 中串行调用（同一时刻仅一个回调在跑，
// 且不与采样并发）；回调内请勿长阻塞。返回取消函数以解除该回调注册。
func (c *Collector) SetOnChange(fn func(old, new Info)) func() {
	if fn == nil {
		return func() {}
	}
	cb := &onChange{fn: fn}
	c.onChMu.Lock()
	c.onCh = append(c.onCh, cb)
	c.onChMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.onChMu.Lock()
			for i, e := range c.onCh {
				if e == cb {
					c.onCh = append(c.onCh[:i], c.onCh[i+1:]...)
					break
				}
			}
			c.onChMu.Unlock()
		})
	}
}

// Snapshot 返回某设备的最近缓存状态（只读缓存、不触发采样，保证 HTTP 路径
// 快速返回）。无缓存但设备已知时返回 Offline 占位（Battery=-1）。返回的 bool
// 表示该设备是否在已知集合中。
func (c *Collector) Snapshot(id string) (Info, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, ok := c.cache[id]; ok {
		return info, true
	}
	if _, known := c.known[id]; known {
		return Info{ID: id, Battery: -1}, true
	}
	return Info{}, false
}

// SnapshotAll 返回全部已知设备最近一次采样缓存（含离线设备）。**只读缓存、
// 不触发即时采样**：HTTP 列表路径必须快速返回，无缓存（后台首轮采样尚未完成）
// 的设备直接跳过，由 5s 采样循环补齐。返回顺序不保证稳定。
func (c *Collector) SnapshotAll() []Info {
	c.mu.Lock()
	out := make([]Info, 0, len(c.known))
	for id := range c.known {
		if info, ok := c.cache[id]; ok {
			out = append(out, info)
		}
	}
	c.mu.Unlock()
	return out
}

// parseBattery 解析 `dumpsys battery` 输出，返回 (电量, 是否充电)。
// 电量为 -1 表示解析失败；Plugged 在 AC/USB powered 任一为 true 时为 true。
func parseBattery(out string) (int, bool) {
	battery := -1
	plugged := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// 匹配 `level: N`（允许行首空白）。
		if after, ok := cutPrefixFold(trimmed, "level:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(after)); err == nil {
				battery = n
			}
			continue
		}
		// 充电判断：AC/USB powered 任一 true。
		if after, ok := cutPrefixFold(trimmed, "AC powered:"); ok {
			plugged = plugged || strings.TrimSpace(after) == "true"
			continue
		}
		if after, ok := cutPrefixFold(trimmed, "USB powered:"); ok {
			plugged = plugged || strings.TrimSpace(after) == "true"
		}
	}
	return battery, plugged
}

// cutPrefixFold 在前缀匹配不敏感空白但保留关键字大小写敏感的前提下切前缀。
// 实际按键名（level/AC powered/USB powered）大小写敏感，但允许关键字前有空白
// 由调用方提前 TrimSpace。
func cutPrefixFold(s, prefix string) (string, bool) {
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
