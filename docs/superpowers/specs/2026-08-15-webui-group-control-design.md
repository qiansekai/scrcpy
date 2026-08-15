# scrcpy-lan WebUI 群控系统设计

日期：2026-08-15
分支：v4.0-lan
状态：已评审（用户认可）

## 1. 背景与目标

在现有 `scrcpy-lan`（无 adb 架构的 scrcpy v4.0 fork）基础上，新增一个 **WebUI + 群控** 前端，替代原生 SDL 桌面客户端成为主入口，支持一台机器同时监控、控制多台 Android 设备。

**核心约束（无 adb）**：设备端 `scrcpy-lan-server.jar` 由 root daemon 常驻，监听 `0.0.0.0:27183`，客户端 `--no-adb` 直连。整个系统**不依赖 adb**，投屏、控制、批量操作、状态采集全部走设备端常驻进程的 TCP 通道。

**已确认需求**：
- 群控能力：监控网格（多屏同看）、批量操作（装 APK / shell 命令）、主控-被控同步操作、设备状态监控与告警
- 部署形态：**暂跑通局域网**（不做公网穿透 / P2P）
- 并发模型：**单操作员**（个人 / 小团队），无需多用户会话锁
- 技术栈：Go + Vue 3 + **Vuetify 3**

**明确不做的（YAGNI）**：WebRTC/P2P 直连、公网穿透、多用户权限体系、mDNS 发现、H.265 支持。

## 2. 架构总览

```
┌─ 设备端 (Android, root, daemon 常驻) ──────────────────────────┐
│  app_process (root)                                          │
│   ├─ scrcpy Server → 0.0.0.0:27183   投屏三流, 内部降 shell   │
│   └─ scrcpy Admin   → 0.0.0.0:27184   管理, 保持 root         │
└──────────────────────────────────────────────────────────────┘
        │ TCP 27183                     │ TCP 27184
┌─────── Go 中控 (局域网单机, 单二进制) ─────────────────────────┐
│  DeviceManager   设备发现(UDP广播)/心跳/在线状态              │
│  StreamSession   每设备一条投屏连接, 解析帧元数据, 订阅分发     │
│  AdminSession    每设备一条管理连接, 命令/文件/状态           │
│  ControlBus      控制消息路由 (单控 + 主控-被控多播)           │
│  TaskScheduler   批量命令/批量装APK/定时状态采集               │
│  WebServer       REST + WS + Vue dist                         │
└──────────────────────────────────────────────────────────────┘
        │ WebSocket
┌─ Vue 3 + Vuetify 3 浏览器 ────────────────────────────────────┐
│  设备网格(缩略) | 单台控制台(WebCodecs+触控) | 批量面板 | 状态告警 │
└──────────────────────────────────────────────────────────────┘
```

## 3. 设备端扩展（唯一改动 Java 的部分）

### 3.1 Admin 进程

同 jar 新增 `com.genymobile.scrcpy.admin.AdminServer` 入口，监听 `0.0.0.0:27184`（`--admin-port` 可配）。

**关键点：不进 `dropRootPrivileges()`，保持 root**。原因（见 `Server.java` 的 `dropRootPrivileges`，为剪贴板正常而降级 uid 至 shell 2000）：投屏进程注定 shell 权限，装系统应用、重启、改系统设置等 root 操作它干不了。管理进程独立保持 root，二者权限不冲突。

**独立管理进程而非扩展 ControlChannel 的原因**：
1. root 权限隔离——投屏进程是 shell，管理需要 root
2. 生命周期解耦——投屏进程随观看连接存在；管理/状态监控要在无人观看时也运行
3. 数据面/控制面分离，语义清晰

### 3.2 daemon 双进程

`scrcpy-lan-daemon.sh` 由 root 拉起，现改为同时启动两个 app_process：

```
app_process → com.genymobile.scrcpy.Server    投屏: 27183 (内部降 shell)
app_process → com.genymobile.scrcpy.admin.AdminServer  管理: 27184 (保持 root)
```

沿用现有 `scrcpy-lan-daemon.sh` 的存活保活逻辑（ps 轮询 + 指数退避），对两个进程分别守护。投屏进程仍按原样管理；管理进程同样 `cleanup=false` 常驻。

### 3.3 管理协议

复用 scrcpy 消息帧风格，支持二进制 payload：

```
请求 (Go → 设备):  [type:1B][payloadLen:4B][payload]
响应 (设备 → Go):  [type:1B][payloadLen:4B][payload]
```

消息类型：
- `SHELL`(0x10)：执行任意 shell 命令，流式回传 stdout/stderr 块，结束回 exitCode
- `PUSH`(0x11)：二进制文件分块写入 `/data/local/tmp`
- `INSTALL`(0x12)：`pm install`（PUSH 完成后）
- `REBOOT`(0x13)
- `GETPROP`(0x14)
- `DUMPSYS`(0x15)

流式响应分帧：
```
[type:STREAM][stdout块]            // 命令实时输出
[type:RESULT][exitCode:4B][stdoutLen:4B][stdout][stderrLen:4B][stderr]
```

命令执行：设备端 `ProcessBuilder("/system/bin/sh", "-c", cmd)`（管理进程是 root，直接 sh 无需 su）；流式读 stdout/stderr 边读边发；命令结束后发 `RESULT`；Go 断连 → `Process.destroy()` 杀子进程（防僵尸、防长跑卡死）。

### 3.4 UDP 广播自动发现

管理进程启动后，每 5 秒向 UDP `255.255.255.255:27185`（默认广播端口，可配）广播一条：

```
scrcpy-lan v1 deviceName=%s serial=%s model=%s videoPort=27183 adminPort=27184
```

Go 监听广播端口，收到 → upsert 设备 → 主动 TCP 验证 27183/27184 → 上线。

**离线判定不走广播**（广播消失有 ≤5s 延迟）：离线由 `AdminSession` 长连接断连即时判定。广播只负责「发现」，不负责「状态」。

## 4. Go 中控

### 4.1 设备接入

- 设备来源：UDP 广播自动发现（主）+ 配置文件/界面手动加 IP（兜底，跨子网/VLAN 场景）
- 上线流程：验证 27183 读 64 字节设备名 + 验证 27184 → 登记 {deviceId, ip, 设备名, 型号, 端口}
- 在线判定：`AdminSession` 长连接 + TCP keepalive + 30s 探活；断连指数退避重连（1s→30s 封顶，参考 daemon 脚本思路）

### 4.2 投屏数据流（StreamSession）

- 每设备一条连接（video/audio/control 三 socket，顺序连接）
- **Go 是唯一连接者**——满足设备端 server 一次只接一个客户端的限制
- 视频：读 `[12B: pts/flags + len][payload]`（`send_frame_meta` 默认 true），解析 `PACKET_FLAG_SESSION`（宽高）后按帧广播
- 控制：双向——浏览器触控 → 写设备 control socket；设备回发（剪贴板等）→ 回给订阅者
- 音频：随流转发，前端 WebCodecs 解 Opus

### 4.3 主控-被控同步（ControlBus）

- 操作者设某台为「主控」，勾选 N 台「被控」
- 触摸/键鼠事件 → 同时写入主控 + 所有被控的 control socket
- **坐标换算（关键）**：scrcpy 触控坐标为设备像素坐标，被控分辨率不同须按各会话 SESSION 宽高换算：`被控坐标 = 事件坐标 × (被控宽/主控宽, 被控高/主控高)`。按键无需换算
- 坐标换算在 Go 端依据各会话的 SESSION 元数据执行

### 4.4 批量任务（TaskScheduler）

- 批量 shell：选设备组 → 同一命令**并行**下发到各 AdminSession → 流式回传 → 汇总 `{设备: {exitCode, stdout, stderr}}`
- 批量装 APK：选设备组 → 并行 PUSH（分块写 `/data/local/tmp`）+ INSTALL → 汇总成功/失败
- 任务全局超时；设备端超时杀子进程

### 4.5 状态监控

- 每 30s 对在线设备轮询 `GETPROP` + `DUMPSYS battery` → 内存 → WS 推前端
- 告警：掉线、电量低于阈值 → 前端通知

### 4.6 对外接口

- REST：设备 CRUD、任务提交/结果查询、登录
- WS：单端点，按 `{deviceId, channel}` 路由（video/control/admin/task）

## 5. Vue 前端

- 技术：Vue 3 + Vite + TypeScript + Pinia + **Vuetify 3**
- 解码：WebCodecs `VideoDecoder`（H.264 Annex B，逐帧喂）+ `AudioDecoder`（Opus）
- 编码固定 H.264（H.265 浏览器不支持；设备端编码在 daemon 参数里定）
- 页面：
  1. **设备网格**：缩略图网格（CSS 缩放）+ 在线状态/电量角标
  2. **单台控制台**：全量视频 + 触控/键鼠注入 + HOME/BACK/RECENTS/截屏/录屏快捷按钮
  3. **批量操作**：选设备组 → shell 命令 / 装 APK（上传本地文件）/ 推文件，结果表格
  4. **状态与告警**：设备详情（电量/网络/型号）+ 掉线/低电量通知
- 主控-被控：单台控制台内勾选「同步到设备」，触控同时广播（坐标换算在 Go）

## 6. 错误处理

- 设备断连：清理会话，指数退避重连
- 命令超时：设备端超时 kill 子进程，Go 端任务级超时兜底
- WS 断开：清理订阅，不中断设备会话（单操作员：唯一控制者断开即释放）
- 广播不可达：静态配置兜底

## 7. 安全

- 局域网场景，浏览器→Go 需要登录（单操作员：单管理员账号 + token）
- Go→设备 27183/27184 明文 TCP，局域网可接受；文档注明风险（设备端口无认证、裸奔公网有风险）
- 禁止硬编码密钥

## 8. 测试

- 设备端：Admin 协议单元测试（命令执行/文件分块/流式回传）
- Go：协议解析测试、会话管理测试、主控-被控坐标换算测试（mock 设备）
- 前端：手动联调
- M2 为验证里程碑：浏览器能看能控一台设备即证明架构正确

## 9. 仓库结构

```
scrcpy-lan/
├─ app/  server/  lan/          # 现有: 原生客户端 + 设备端 server + daemon
└─ webui/
   ├─ server/                   # Go 中控 (单二进制)
   │  ├─ cmd/webui/main.go
   │  ├─ internal/device/       # 发现/会话/管理
   │  ├─ internal/bus/          # 控制路由 + 主控-被控多播
   │  ├─ internal/task/         # 批量任务/状态采集
   │  └─ internal/ws/           # WS 网关
   ├─ web/                      # Vue 前端
   └─ README.md
```

## 10. 里程碑

1. **M1 设备端**：Admin 进程 + 管理协议 + daemon 双进程 + UDP 广播
2. **M2 单设备上屏**：Go 骨架（发现 + StreamSession + WS 转发）+ Vue 单台控制台——**最小闭环：浏览器能看能控一台**
3. **M3 网格 + 多设备**：设备网格、多订阅、会话复用
4. **M4 批量 + 主控-被控**：批量命令/APK、坐标换算多播、任务结果
5. **M5 状态监控**：定时采集、告警
