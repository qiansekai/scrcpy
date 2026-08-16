# 双模式音频捕获：全局捕获 + 双端出声（audio-dup）

> 日期：2026-08-16
> 分支：v4.0-lan

## 背景与目标

scrcpy-lan 当前音频走 `AudioDirectCapture` + `REMOTE_SUBMIX`（`--audio-source=output`），
捕获设备整个音频输出并转发给客户端，但**设备自身静音**（REMOTE_SUBMIX 把音频路由到虚拟
子混音，不回放）。

目标：实现"音频多路复制/镜像"——客户端出声**且**设备（server 端）同时出声。设备为
Android 13+。webui 浏览器与桌面客户端均在使用。

约束：**默认保持旧方案行为不变**，通过启动参数显式切换到新方案。

## 技术背景（调研结论）

| | 旧方案（默认） | 新方案（playback） |
|---|---|---|
| 实现类 | `AudioDirectCapture` | `AudioPlaybackCapture` |
| API | `AudioRecord` + `REMOTE_SUBMIX` | 反射 `AudioPolicy` / `AudioMix` / `AudioMixingRule` |
| 权限 | 需 shell/root 访问受限源 | `MODIFY_AUDIO_ROUTING`（shell uid 已具备，stock scrcpy 已验证） |
| 设备出声 | 无（静音） | 有（`ROUTE_FLAG_LOOP_BACK_RENDER`） |
| 版本要求 | Android 11+ | **Android 13+** |

`Server.java` 已按 `audio_source` 分流到两个捕获类（`Server.java:123-127`），旧行为无需改动。

平台硬限制：App 通过 `setAllowedCapturePolicy` 主动 opt-out、或播放 DRM 内容时，任何方式都
无法捕获（Android 强制规则）。AOSP 确认 `AudioMixingRule.Builder.build()` 对空规则抛
`IllegalArgumentException`，因此"全局捕获"必须显式枚举 usage。平台允许捕获的 usage 恰为
`MEDIA` / `GAME` / `UNKNOWN` 三类。

## 设计

### 方案：保留旧方案，`audio_source=playback audio_dup=true` 启用新方案

- 旧方案（`output`，默认）：`AudioDirectCapture` + REMOTE_SUBMIX —— 完全不变。
- 新方案（`playback` + `audio_dup`）：`AudioPlaybackCapture` +
  `ROUTE_FLAG_LOOP_BACK_RENDER` —— 设备出声 + 捕获转发，捕获规则扩展为全局。

### 数据流（新方案）

```
设备 App 音频 ──► AudioPolicy Mix（ROUTE_FLAG_LOOP_BACK_RENDER）
                     ├──► 设备扬声器 出声
                     └──► AudioRecord ─► Opus 编码 ─► TCP 27183
                                                    └─► webui/桌面 ─► 客户端出声
```

### 改动点

1. **`server/.../audio/AudioPlaybackCapture.java`** `createAudioRecord()`
   - 捕获规则：单条 `USAGE_MEDIA` → `USAGE_MEDIA` + `USAGE_GAME` + `USAGE_UNKNOWN` 三条。
   - 修复 bug：`build()` 在 `voiceCommunicationCaptureAllowed(true)` 之前调用，导致该开关
     失效（`AudioPlaybackCapture.java:54-58`）；调整为先设开关再 `build()`。
2. **`lan/scrcpy-lan-daemon.sh`** 新增第二参数 `AUDIO_MODE`（默认 `output`）
   - 值为 `playback` 时向 `app_process` 启动参数追加 `audio_source=playback audio_dup=true`。
   - 默认不传 → 行为与现状完全一致。
3. **`lan/scrcpy-lan.sh`** 同步新增第二参数（调试/手动启动用）。
4. **`lan/autostart.sh`** 保持默认（不传 playback）；如需开机即双端出声，按需改为传
   `playback`（属部署选择，不在代码改动内）。

### 边界与已知风险

- **Android < 13**：`AudioPlaybackCapture.checkCompatibility()` 抛异常 → 音频按现有逻辑
  降级禁用（写 disable 消息），不影响视频。
- **Android 15 已知上游 bug**（Genymobile/scrcpy#5781，部分机型 `audio-dup` 捕获失败但
  设备出声）→ 若命中需另行处理，本设计不解决。
- **杂音**：捕获 GAME/UNKNOWN 可能带入系统提示音，属全局捕获正常范围。
- **回声**：无。Mix 只捕获 App 播放流，回放走物理输出，不形成反馈环。
- **客户端参数不生效**：server 由 daemon 预启动，webui/桌面端仅连接 27183 不启动 server，
  因此它们各自的 `--audio-source` 在 LAN 模式下对 server 无效，音频模式只能由 daemon 决定。
  若将来需 webui 动态切换，需加控制通道消息（超出本次范围）。

## 验证

- 默认部署：`output` 模式行为与现状一致（设备静音、仅客户端出声）。
- `playback` 模式：设备放歌/开游戏，设备与浏览器**同时**出声；桌面客户端连 27183 同样生效。
- 确认 webui 当前音频链路（Opus 转发/播放）无需改动。

## 部署步骤（非代码）

1. 重建 `scrcpy-lan-server.jar`（`server/` gradle 构建）。
2. 重推 jar + `scrcpy-lan-daemon.sh`/`scrcpy-lan.sh` 到设备 `/data/local/tmp/`。
3. 重启 daemon。启用新方案：`sh scrcpy-lan-daemon.sh 27183 playback`。
