# webui — scrcpy-lan 单设备 Web 控制台（M2）

无 adb 的 scrcpy 分支配套 Web 控制中心。M2 目标：把一台设备的画面推到浏览器，
并在浏览器里回传触控、按键、快捷指令，实现局域网内远程控制。

## 架构

```
Android 设备 (scrcpy-lan daemon, TCP 27183)
   │  三条 TCP：video / audio(丢弃) / control
   ▼
Go 服务 (:8080)
   ├─ REST  /api/devices   设备增删查（添加时探测 27183 读设备名）
   ├─ WS    /ws/{id}       视频帧 + session/clipboard 消息 → 浏览器
   │                       控制消息（touch/key/text/快捷键）→ 设备
   └─ 静态  web/dist       生产前端
   ▼
浏览器 (WebCodecs 解码 H.264 → canvas)
```

- 设备侧：scrcpy-lan daemon 监听 27183，按 video → audio → control 顺序接受三条
  连接，三条连接齐了才写设备名。Go 侧 probe 和会话连接都按同样的顺序建立三条连接。
- 音频：M2 范围内只建立 audio 连接并丢弃数据，防止设备背压，不播放不转发。
- WS 中继：Hub 缓存 session 元数据与 H.264 配置帧（SPS/PPS），后加入的浏览器
  也能直接开始解码，不用等下一个关键帧。视频帧以 0x01 前缀二进制帧发出，
  其余为 JSON 文本消息。
- 浏览器解码：`VideoDecoder`（avc1.640028）+ canvas。触控/按键坐标已按设备分辨率
  缩放，浏览器直接回传设备像素坐标。

## 启动步骤

前置：设备端 scrcpy-lan daemon 已在运行（本机已确认 TCP 27183 可达）。

### 生产模式

```powershell
cd D:\Kita-Tools\scrcpy-lan\webui\web
npm install          # 首次
npm run build        # 产出 web/dist
cd ..
go build ./cmd/webui # 产出 webui.exe
.\webui.exe          # 浏览器访问 http://localhost:8080
```

`webui.exe` 同时服务静态页面、`/api`、`/ws`。可用 `-addr :8081` 改监听端口，
`-config devices.json` 指定设备配置文件。

### 开发模式

```powershell
# 终端 1：Go 后端（8080）
cd D:\Kita-Tools\scrcpy-lan\webui
go run ./cmd/webui

# 终端 2：Vite 前端（5173，/api 与 /ws 代理到 8080）
cd D:\Kita-Tools\scrcpy-lan\webui\web
npm install
npm run dev
```

浏览器访问 `http://localhost:5173`。

## 添加设备

设备列表页点「添加设备」，填设备 IP（可带端口，如 `192.168.1.18:27183`）。
Go 端会探测 27183 并读取设备名，探测成功即加入列表并落盘 `devices.json`
（该文件为运行时数据，不入库）。设备在线状态由「该设备的会话是否在跑」决定：
添加即建立会话并自动重连，离线/在线在列表中实时显示。

## 支持的控制

- 触控：画布上点击 / 滑动（pointerdown/move/up，pointercancel 视为抬起）
- 按键：字母、数字、方向键、Enter、Backspace、Tab、Space、Escape、Delete、
  Home/End/PageUp/PageDown、Shift/Ctrl/Alt（按住组合键生效）
- 快捷按钮：HOME / BACK / RECENTS / 电源 / 旋转
- 剪贴板：设备主动推送时在控制台接收（不实现浏览器→设备粘贴）

## M2 边界

- 单设备控制台：无批量列表操作、无多设备网格/墙、无录像、无回放。
- 无 adb：运行时只连设备 27183，所有代码不调用 adb。scrcpy-lan daemon 的
  初始部署（推 jar / 启动）不属于本项目范围。
- 无认证：M2 不做登录鉴权，WS 只按同源（localhost/127.0.0.1）校验 Origin。
  仅适合可信局域网，公网部署需自行加反向代理鉴权。
- 设备端口安全提示：Go 服务反连设备 27183 时不做设备指纹校验，局域网内任何
  机器开着 27183 都可能被误认成目标设备。请确保仅连接可信设备。
