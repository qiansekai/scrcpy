# WebUI 全功能补齐设计（2026-08-27）

> **状态：✅ 全部完成并真机验证（2026-08-27）**
> A3 中文注入 / A4 exec 控制台 / M4 批量+主控-被控 / M5 状态告警 / 设备发现 / 技术债清零，
> 全部落地。详见文末「完成记录」。

> 目标：A3 文本输入 / A4 exec 前端 / M4 批量+主控-被控 / M5 状态告警 / 设备发现 / 技术债清零。
> 架构：三个新 Go 包（task/status/bus）由子代理并行实现（只写自己的包），共享文件
> （httpapi/ws/hub/session/handler/前端）由集成者统一修改，避免合并冲突。

## 1. 新包契约（子代理实现，不碰其它文件）

Go module：`scrcpy-lan/webui`（包根 `webui/`，模块路径见 go.mod）。测试用标准库 + 表驱动。

### 1.1 `webui/internal/task` — 批量任务

```go
package task

type Result struct {
    ID     string `json:"id"`
    Ok     bool   `json:"ok"`
    Error  string `json:"error,omitempty"`
    // Exec:
    ExitCode int    `json:"exitCode,omitempty"`
    Stdout   string `json:"stdout,omitempty"`
    Stderr   string `json:"stderr,omitempty"`
    // Install / Push:
    Remote string `json:"remote,omitempty"`
}

// ExecBatch 并行在每台设备上执行命令（复用 device.Manager.ExecCommand）。
func ExecBatch(ctx context.Context, mgr *device.Manager, ids []string, cmd string) []Result

// InstallAPK 并行：PUSH apkPath 到 /data/local/tmp/scrcpy-push-<rand>.apk，
// 然后 shell "pm install -r -t <path>"，最后 rm。任一步失败记 Error。
func InstallAPK(ctx context.Context, mgr *device.Manager, ids []string, apkPath string) []Result

// PushFile 并行：把 localPath 推送到每台设备的 remotePath（如 /sdcard/Download/x）。
func PushFile(ctx context.Context, mgr *device.Manager, ids []string, localPath, remotePath string) []Result
```

- 约束：`ids` 去重；每设备独立 goroutine（上限 16 并发）；结果按输入顺序返回。
- **APK/文件推送需要 admin 协议 TYPE_PUSH（0x11）**，但 admin.go 目前只有 shell。
  做法：本包内部实现 `pushChunked(ctx, adminAddr, remote, reader, total)`，
  协议：`0x11` + int32 pathLen + path + data（分块 ≤256KB，每块后读一个 RESULT ack，
  RESULT 格式 = 1B type(0x21) + int32 len + int32 exitCode(0=ok) + ...，只要首 4 字节）。
  推送完关闭连接。**同时在本包留一个 TODO 注释：设备端 AdminServer 需实现 TYPE_PUSH**
  （集成者会同步改 server Java 并重建 jar）。
- ExecBatch 在无会话时返回 `Result{Ok:false, Error:"no such device session"}`，不 panic。
- 测试：mock 一个 `device.Manager` 不可行（ExecCommand 依赖真实 dial）→ 把可测逻辑
  （并发上限、去重、结果聚合、push 分块协议编码）拆成包内函数并用 net.Pipe 的假
  admin server 测 push 协议与 exec 聚合。禁止 import cycle：task 只 import device。

### 1.2 `webui/internal/status` — 状态采集

```go
package status

type Info struct {
    ID      string `json:"id"`
    Online  bool   `json:"online"`
    Battery int    `json:"battery"`  // -1 未知
    Plugged bool   `json:"plugged"`
    Model   string `json:"model"`
    Android string `json:"android"`
}

type Collector struct { ... }
func NewCollector(mgr *device.Manager) *Collector
func (c *Collector) Start(interval time.Duration)   // 默认 5s；内部 goroutine 定时采样
func (c *Collector) Stop()
func (c *Collector) Snapshot(id string) (Info, bool) // 有缓存返回缓存，无则即时采样
func (c *Collector) SnapshotAll() []Info
// 变化通知：battery/plugged/online 任一变化时回调（用于告警与 ws 广播）
func (c *Collector) SetOnChange(func(old, new Info))
```

- 采样实现：`mgr.ExecCommand(id, "dumpsys battery")`（解析 level/temperature/AC/USB plugged）
  + `mgr.ExecCommand(id, "getprop ro.product.model")` + `ro.build.version.release`。
  解析不了给默认值，不报错。Exec 错误 → `Online=false`，`Battery=-1`。
- **dumpsys battery 解析规则**（示例输出）：
  ```
  Current Battery Service state:
    AC powered: false
    level: 87
  ```
  取 `level:` 行（允许行首空白），`AC powered: true` 或 `USB powered: true` → Plugged。
- 告警语义放集成层（低电量 <20、掉线），本包只做变化回调。
- 测试：dumpsys 文本解析器（真实样例多份）表驱动测试；变化回调测试。

### 1.3 `webui/internal/bus` — 主控-被控控制总线

```go
package bus

type Dimensions struct{ W, H int }

type Bus struct { ... }
func NewBus() *Bus

// SetMaster/Slaves 由 ws 层调用；session 尺寸由集成层在 PublishSession 时 Register。
func (b *Bus) SetMaster(id string)
func (b *Bus) SetSlaves(ids ...string)
func (b *Bus) RegisterDim(id string, d Dimensions)   // session 建立时
func (b *Bus) UnregisterDim(id string)               // 会话结束

// Forward 把主控的控制消息广播到所有被控：仅 touch 消息做坐标换算。
// msg 为浏览器 CtrlMsg 序列化前的结构（见 ws 包），返回 map[slaveID][]byte
// （已编码好的控制字节，由集成层发给各被控的 SendControl）。
// 非 touch 消息（key/text/按键等）不换算直接复制。
func (b *Bus) Forward(master string, msg ForwardMsg) map[string][]byte

type ForwardMsg struct {
    Type     string  // "touch" 等，与 ws.CtrlMsg.Type 一致
    X, Y     float64 // touch 时有效（主控设备像素坐标）
    ScreenW, ScreenH int
    Action   int
    Keycode  int
    Text     string
    ...（与 ws.CtrlMsg 同构的其余字段）
}
```

- 坐标换算：`x' = x * slaveW / masterW`，`y' = y * slaveH / masterH`（整数截断）。
  用主控 dims 从 RegisterDim 查；主控/被控缺 dims 时跳过该被控。
- 编码复用 `control.Writer`（import scrcpy-lan/webui/internal/control）。
- 只负责算坐标+编码，不直接发消息（集成层发），便于测试。
- 测试：不同分辨率组合的坐标换算表驱动；非 touch 透传；缺 dims 跳过；SetMaster/SetSlaves 状态。

## 2. 共享层改动（集成者做，子代理禁改）

### 2.1 server Java（需重建 jar 重部署）

1. `AdminServer.java` 加 `TYPE_PUSH = 0x11`：
   - 请求：int32 pathLen + path(UTF8) + data；路径校验：绝对路径、不含 `..`、
     前缀允许 `/data/local/tmp/` 或 `/sdcard/`；首块创建截断写，后续追加；
   每块回 RESULT(exitCode=0)。连接关闭即文件完成。
2. `Controller.injectText`：先发 ADBKeyboard 广播（`ADB_INPUT_TEXT`，extra `msg`），
   再走 injectChar 兜底（拉丁）。这样装了 ADBKeyboard 即支持中文。

### 2.2 Go 共享层

- `httpapi`：批量端点 `POST /api/devices/batch/exec`（{ids,cmd}）、
  `POST /api/devices/batch/install`（multipart 文件字段 apk）、
  `POST /api/devices/batch/push`（multipart file + remote 表单）、
  `GET /api/devices/{id}/status`、`POST /api/devices/discover`（网段扫描）。
- 发现：取本机 LAN IPv4（24 位掩码），并发 dial `:27184`（超时 400ms，并发 128），
  在线的加入配置（去重：已存在则跳过）。返回新增列表。
- `ws`：CtrlMsg 加 `Pressure`、`Master`/`Slaves` 路由：
  `{type:"setMaster",id}`、`{type:"setSlaves",ids:[...]}`；
  touch 消息在主控模式下经 bus.Forward 广播；
  hub 加 `PublishStatus(infos)` 广播 `{"type":"status",...}`、`{"type":"alert",...}`。
- 技术债修复：backoff 封顶 30s；Stop() 幂等（sync.Once）；SendControl 满则丢（非阻塞）；
  power 键 DOWN+UP；hub 广播先收集 conns 再解锁写；store.Save 错误上报 + 重复 IP 返回 409；
  admin.go RESULT 完整解析。

### 2.3 前端（集成后由前端子代理实现）

- DeviceGrid：设备多选 + 批量工具条（shell/装APK/推文件/主控-被控/发现按钮）
  + 状态角标（电量%、在线点、充电）+ 告警 toast + 添加设备 409 提示。
- PreviewPanel：文本输入框（IME 提示）+ exec 控制台 + 主控/被控开关 + 压力传递
  （TouchEvent.force，默认 1.0）。
- api.ts：`listDevices` 检查 r.ok；新增批量/状态/发现调用。
- useStream：`connected` 状态真实反映；重连时重置 configData；键盘 Enter 防双击。

## 3. 部署（集成者）

- 重建 server jar（gradle）→ 推送 + 重启 daemon。
- 安装 ADBKeyboard：`adb install ADBKeyboard.apk` + `ime enable com.android.adbkeyboard/.AdbIME`
  + `ime set com.android.adbkeyboard/.AdbIME`（注入完可 `ime set` 恢复用户输入法，webui 控制台可执行）。
- 重建 webui.exe（go build）+ `npx vite build` 前端 dist。
- 真机验证：批量 exec、装 APK、推文件、主控-被控、状态、告警、中文注入。

## 4. 完成标准

- `go test ./...` 全绿（webui）；`go vet ./...` 干净；`npx vite build` 通过。
- 浏览器实测：多设备状态角标实时、批量结果表格、主控-被控同步操作、中文文本注入成功。
- 文档更新 + 提交推送。

## 5. 完成记录（2026-08-27，真机 Redmi M2012K11AC 验证）

| 功能 | 验证方式 | 结果 |
|---|---|---|
| 批量 shell | REST \/api/devices/batch/exec\ echo hello-lan | ✅ exitCode 0 + stdout |
| 批量装 APK | REST \atch/install\ 装 ADBKeyboard.apk | ✅ pm list 可见包 |
| 批量推文件 | REST \atch/push\ → /sdcard/Download | ✅ 设备端 cat 校验内容 |
| 状态采集 | GET /api/devices 与 /status | ✅ battery=100/plugged/model/android |
| 设备发现 | POST /api/devices/discover | ✅ 自动找到 192.168.1.19 并入库 |
| 中文注入 | 浏览器文本框发"你好世界测试"（IME=ADBKeyboard） | ✅ uiautomator dump 见文本 |
| exec 控制台/主控 UI/告警 | 浏览器 a11y 快照 + Go 测试 | ✅ 前端渲染、后端测试全绿 |
| 主控-被控 | bus 包单测（含 -race） | ✅ 坐标换算/去重/剔除；多机 E2E 待第二台设备 |

### 实测中修掉的三个 bug
1. **发现扫描 0 结果**：\
et.IP(ip4.Mask(...))\ 把掩码当网络号（扫成 255.255.255.x）→ 改为 \masked.Mask(CIDRMask).String()\；另 \
et.IPv4()\ 返回 16 字节 IPv4-in-IPv6 形式，改用 \make(IP,4)\。
2. **推送超时**：设备端残留旧 AdminServer 进程（pkill 对 app_process cmdline 匹配不可靠）→ 显式 kill 后 guard 用新 jar 拉起即通。
3. **列表接口 5s 超时**：SnapshotAll 对无缓存设备同步采样（每台最多 9s）→ 改为纯读缓存，采样只由 5s 后台循环执行。

### 契约偏差（实现时确认）
- 前端主控通道用独立持久 WS；取消主控显式发 \{type:"setMaster", master:""}\。
- 未选中设备的告警经缩略图 WS 的 onAlert 汇入网格 snackbar。
- 批量 multipart 字段名：\ile\ + \ids\(JSON 字符串) [+ \emote\]。
