# WebUI 后续开发交接计划（2026-08-16）

> 此文档是会话交接点。上下文压缩/新会话启动时，先读本文件 + `docs/superpowers/specs/2026-08-15-webui-group-control-design.md` + 分支 `git log` 恢复全貌。

## 1. 当前状态（全部已完成并提交到 v4.0-lan）

webui/ 目录从零建成，真机（Redmi M2012K11AC, 192.168.1.18, Android 13）全部验证通过：

- **M2 单设备上屏**：Go 直连设备 27183（无 adb），WS 转发，浏览器 WebCodecs 解 H.264 + Opus 音频
- **M3 网格 + 缩略图 + 选中放大**：一页式，网格缩略图 + 选中出大预览区（可触控/键盘/快捷键控制）
- **音频全链路**：OpusHead 解析、interleaved 播放、jitter 缓冲 + 可调滑块（10-300ms）
- **原生协议代理**：:27182 让原生 scrcpy.exe 同时连（tee 分流设备字节给 web + 原生）
- 控制：触控 / 键盘（字母/数字/方向/Shift）/ HOME/BACK/RECENTS/电源/旋转

关键提交（git log 从早到晚）：
`54476cc`(M2 起点) → M2 各任务 → `e68070a6`(音频) → `6ba3300b`/`6c1c4cda`(音频修复/低延迟) → `fe9ffff8`(缓冲+noAudio) → `fced6df2`(jitter) → `e939cd05`(缓冲滑块) → `3a542dc3`(原生代理)

## 2. 环境 / 服务 / 启动（新会话必备）

- **设备**：`192.168.1.18`，root daemon 常驻 27183（`scrcpy-lan-daemon.sh` + `scrcpy-lan-server.jar` 在 /data/local/tmp）
- **设备 adb**：serial `9e82d8ed`（USB 连接）；`svc power stayon true` 已设（屏幕常亮，测试方便，想还原就 `adb shell svc power stayon false`）
- **Go 中控**：`webui/webui.exe`（服务 8080 web + 27182 原生代理）。重建：`cd webui; go build -o webui.exe ./cmd/webui`
- **原生客户端**：`build-win-v4\app\scrcpy.exe`（缺 DLL，需 `$env:PATH="D:\msys64\ucrt64\bin;"+$env:PATH`）
  - 连代理：`scrcpy.exe --no-adb --tunnel-host 127.0.0.1 --tunnel-port 27182`
  - 直连设备（不用 web 时）：`--tunnel-host 192.168.1.18 --tunnel-port 27183`
- **前端 dev**：`webui/web` 下 `npm run dev`（5173，HMR）；生产 `npx vite build` 出 dist（webui.exe 读磁盘）
- **Go 测试**：`cd webui; go test ./...`（全绿）
- **端口**：8080(web REST+WS) / 27182(原生代理) / 5173(vite dev)

## 3. 下一批功能（用户点名，按序）

### A1. 右键返回
- web 预览区/控制区 canvas 上 `contextmenu` → 发 BACK（keycode 4 或 `{type:'back'}`）。`useControl` 里加右键处理，`e.preventDefault()` 挡浏览器菜单。

### A2. 快捷键复制粘贴（剪贴板同步）
- scrcpy 控制协议有 GET_CLIPBOARD(0x08)/SET_CLIPBOARD(0x09)，设备消息有 CLIPBOARD/ACK_CLIPBOARD（Go `control` 包已解析，`DevMsgClipboard` 已转发到 hub → web `{"type":"clipboard"}`）。
- 前端：Ctrl+C → 发 GET_CLIPBOARD，收到设备剪贴板 → 写 PC 剪贴板（`navigator.clipboard`）；Ctrl+Shift+V / Ctrl+V → 读 PC 剪贴板 → 发 SET_CLIPBOARD（含 paste 标志）。
- Go `control.Writer` 已有 InjectKey 但没 SetClipboard/GetClipboard 编码——需加。

### A3. 中文输入（ADBKeyboard IME）
- 方案：设备端装 **ADBKeyboard** apk（GitHub senzhk/ADBKeyBoard），切 IME 为它；web 发 scrcpy `INJECT_TEXT`(0x01) 走 ADBKeyboard 的文本注入。原生 scrcpy 也是这个路子。
- 需要：设备端装 apk（adb 推 + pm install + 切默认输入法）、Go 已有 InjectText 编码、前端文本框把输入发 `{type:'text'}`。
- 替代：scrcpy `--keyboard=uhid`（硬件键盘模拟，中文要特殊处理）。ADBKeyboard 更简单直接。

### A4. 命令执行（shell 通道）
- 依赖 **M1 设备端 Admin 进程**（见下）。设备端开 root 管理通道（独立端口 27184 或管理协议），Go 下发 shell/装 APK/文件，回传 stdout/exitCode。spec 里有完整设计（2026-08-15 spec 第 3 节）。

## 4. M 系列里程碑（spec 已定义，未实现）

- **M1 设备端 Admin 进程**：root 管理通道 + 命令/装APK/文件 + UDP 广播设备发现（`lan/scrcpy-lan-daemon.sh` 改双进程）。**A4 命令执行的前置**。
- **M4 批量 + 主控-被控**：多选设备批量 shell/装APK；一台主控操作同步到被控（坐标按各自分辨率换算，Go 端）。
- **M5 状态监控**：电量/在线/掉线告警（走 Admin 通道 getprop/dumpsys）。

## 5. Technical debt / deferred minors（最终评审分流，未修）

- Task 5: backoff 上限 32s（应为 30s）；`Stop()` 不幂等；`SendControl` 断线时可能阻塞
- Task 6: 电源键只发 DOWN（应 DOWN+UP）；触控 MOVE 压力恒 0xFFFF；`connWrapper.Write` 持 hub RLock 写通道（暂安全）
- Task 7: `store.Save` 错误被吞；重复 IP POST 追加重复配置
- Task 8: `api.ts` listDevices 不查 `r.ok`（后端挂时静默空页）
- Task 9: 状态指示 `stream?.connected` 永远 truthy；`configData` 重连未重置（已部分修：session 消息重置）
- Task 10: 聚焦快捷键按钮 Enter 双击发
- 其它：`DeviceConsole.vue` 已删除（被预览区取代）；`webui/webui.exe`/`devices.json`/日志均已 gitignore

## 6. Superpowers 流程还剩什么

已用：brainstorming / writing-plans / subagent-driven-development / TDD / systematic-debugging / requesting-code-review（最终评审 With fixes，4 Important 已修）。

**未做/可选**：
- **finishing-a-development-branch**：分支收尾（review/merge/清理）。当前持续迭代中，可在某个里程碑后做。若做，需先跑 `verify-quality`/`simplify`。
- **verification-before-completion**：M 系列交付前跑。
- **M3-M5 各自的 planning**：按 brainstorming → writing-plans 流程（M3 是直接做的没走完整流程，M4 起建议按流程）。
- **每个新 milestone 的 subagent-driven-development 执行**（M2 用了，M3 直接做了）。

## 7. 测试注意（真机）

- 设备 server 一次只接受一个客户端：Go 中控必须独占；原生客户端走 27182 代理，不能直连 27183
- 音频默认 Opus；改音频测试前确认设备在播声（`adb shell "media player /system/media/audio/ringtones/AcousticGuitar.ogg &"`，循环用 `while true`）
- 视频 codec 必须 H.264（h265 浏览器不解）；设备端 `tunnel_forward=true tunnel_port=27183`
- Go 重启会断开设备会话 → 浏览器 WS 断，需刷新（无自动重连）；原生代理的 tee 会 Discard（不影响 web）

## 8. 其它

- 切生产版给用户试：重建 dist + webui.exe，浏览器 8080；对比延迟用原生 27182
- 音频延迟现状：web 播放缓冲 ~60ms（滑块可调），端到端含设备捕获 ~200-300ms，投屏不适合音游
