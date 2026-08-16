# 双模式音频（全局捕获 + 双端出声）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 设备端支持通过 daemon 参数在"旧方案（REMOTE_SUBMIX 仅转发）"与"新方案（playback 全局捕获 + 设备出声）"间切换，webui 与桌面客户端零改动。

**Architecture:** 复用 `Server.java` 已存在的 `audio_source` 分流逻辑。旧方案（默认 `output`）走 `AudioDirectCapture`，行为完全不变；新方案传 `audio_source=playback audio_dup=true` 走 `AudioPlaybackCapture`，把 `AudioMixingRule` 从单条 USAGE_MEDIA 扩展为 MEDIA+GAME+UNKNOWN 实现全局捕获，`ROUTE_FLAG_LOOP_BACK_RENDER` 让设备在捕获时继续出声。daemon 脚本通过第二参数 `AUDIO_MODE` 决定是否追加这两个 server 参数。

**Tech Stack:** Android Java（反射 AudioPolicy API）、POSIX shell（app_process 启动脚本）、gradle/d8 构建。

## Global Constraints

- Android 设备必须为 **Android 13+（API 33+）**，否则 `AudioPlaybackCapture.checkCompatibility()` 抛异常，音频降级禁用（走现有 disable-stream 逻辑）。
- 平台硬限制：App 通过 `setAllowedCapturePolicy` opt-out 或播放 DRM 内容时无法捕获，任何方案均不可绕。
- `AudioMixingRule.Builder.build()` 对空规则抛 `IllegalArgumentException`，捕获规则必须显式枚举 usage。
- 平台允许捕获的 usage 仅为 **`USAGE_MEDIA` / `USAGE_GAME` / `USAGE_UNKNOWN`**。
- daemon 默认（不传第二参数）必须与原行为**完全一致**。
- 本仓库无 Android 单测框架，验证方式：代码走查 + 编译通过 + `sh -n`/dry-run + 真机实测（见各任务）。

---

### Task 1: 扩展 `AudioPlaybackCapture` 捕获规则为全局 + 修复 voice 开关顺序

**Files:**
- Modify: `server/src/main/java/com/genymobile/scrcpy/audio/AudioPlaybackCapture.java:44-58`

**Interfaces:**
- Consumes: 无（本任务是独立修改，`Server.java` 已在 `Server.java:126` 调用 `new AudioPlaybackCapture(options.getAudioDup())`，不改签名）。
- Produces: 行为变更——`AudioPlaybackCapture` 捕获 MEDIA/GAME/UNKNOWN 三类 usage，且 `voiceCommunicationCaptureAllowed(true)` 真正生效。

- [ ] **Step 1: 阅读当前方法，定位待改区域**

打开 `AudioPlaybackCapture.java`，确认 `createAudioRecord()` 内第 44-58 行的当前结构：`setTargetMixRole` → `addMixRule(USAGE_MEDIA)` → `build()` → `voiceCommunicationCaptureAllowed(true)`（注意 build 先于 voice 调用，是 bug）。

- [ ] **Step 2: 替换捕获规则 + 调整 voice 调用顺序**

把 `createAudioRecord()` 第 44-58 行替换为以下代码。关键差异：`addMixRule` 循环匹配三种 usage；`voiceCommunicationCaptureAllowed(true)` 移到 `build()` 之前。

```java
            // audioMixingRuleBuilder.setTargetMixRole(AudioMixingRule.MIX_ROLE_PLAYERS);
            int mixRolePlayersConstant = audioMixingRuleClass.getField("MIX_ROLE_PLAYERS").getInt(null);
            Method setTargetMixRoleMethod = audioMixingRuleBuilderClass.getMethod("setTargetMixRole", int.class);
            setTargetMixRoleMethod.invoke(audioMixingRuleBuilder, mixRolePlayersConstant);

            // 全局捕获：匹配平台允许捕获的全部 usage（MEDIA / GAME / UNKNOWN）
            int ruleMatchAttributeUsageConstant = audioMixingRuleClass.getField("RULE_MATCH_ATTRIBUTE_USAGE").getInt(null);
            Method addMixRuleMethod = audioMixingRuleBuilderClass.getMethod("addMixRule", int.class, Object.class);
            int[] usages = {AudioAttributes.USAGE_MEDIA, AudioAttributes.USAGE_GAME, AudioAttributes.USAGE_UNKNOWN};
            for (int usage : usages) {
                AudioAttributes attributes = new AudioAttributes.Builder().setUsage(usage).build();
                addMixRuleMethod.invoke(audioMixingRuleBuilder, ruleMatchAttributeUsageConstant, attributes);
            }

            // voiceCommunicationCaptureAllowed 必须在 build() 之前设置才生效
            Method voiceCommunicationCaptureAllowedMethod = audioMixingRuleBuilderClass.getMethod("voiceCommunicationCaptureAllowed", boolean.class);
            voiceCommunicationCaptureAllowedMethod.invoke(audioMixingRuleBuilder, true);

            // AudioMixingRule audioMixingRule = builder.build();
            Object audioMixingRule = audioMixingRuleBuilderClass.getMethod("build").invoke(audioMixingRuleBuilder);
```

> 说明：`AudioAttributes` 已在文件顶部 import（`AudioPlaybackCapture.java:11`），`addMixRule` 的反射 `Method` 只需 `getMethod` 一次、循环 `invoke` 多次。移除原第 54-58 行 `build()` 先行 + 事后无效调用 voice 的代码。

- [ ] **Step 3: 代码走查核对**

确认三点：① 三条 usage 均在 `build()` 前 `addMixRule`；② `voiceCommunicationCaptureAllowed(true)` 在 `build()` 之前；③ 无残留的原 `USAGE_MEDIA` 单条 `addMixRule` 调用。`setTargetMixRole` 仍在所有 `addMixRule` 之前（API 要求）。

- [ ] **Step 4: 编译验证**

在仓库根目录执行 gradle 编译（Windows）：

```powershell
.\gradlew.bat :server:assembleRelease
```

预期：BUILD SUCCESSFUL，无编译错误。若本机 gradle 构建不可用，改用 `server/build_without_gradle.sh`（需 bash + ANDROID_HOME，见该脚本头部说明），并确认 `AudioPlaybackCapture.java` 在 `SRC` 的 `audio/*.java` 覆盖范围内（`build_without_gradle.sh:62`）。

- [ ] **Step 5: Commit**

```bash
git add server/src/main/java/com/genymobile/scrcpy/audio/AudioPlaybackCapture.java
git commit -m "feat(server): capture all usages (MEDIA/GAME/UNKNOWN) + fix voice flag order"
```

---

### Task 2: daemon 与启动脚本新增 `AUDIO_MODE` 第二参数

**Files:**
- Modify: `lan/scrcpy-lan-daemon.sh:14`（PORT 行）与 `lan/scrcpy-lan-daemon.sh:35-38`（app_process 启动块）
- Modify: `lan/scrcpy-lan.sh:7`（PORT 行）与 `lan/scrcpy-lan.sh:8-10`（app_process 启动块）

**Interfaces:**
- Consumes: Task 1 的 server jar 支持 `audio_source=playback audio_dup=true`（`Options.java:382-390` 已解析）。
- Produces: `scrcpy-lan-daemon.sh [PORT] [AUDIO_MODE]` / `scrcpy-lan.sh [PORT] [AUDIO_MODE]`，`AUDIO_MODE` 取 `output`（默认）或 `playback`。

- [ ] **Step 1: 修改 `scrcpy-lan-daemon.sh`**

在第 14 行 `PORT="${1:-27183}"` 之后加一行：

```sh
AUDIO_MODE="${2:-output}"
```

把第 35-38 行的启动块替换为（`AUDIO_ARGS` 只在 `playback` 时非空）：

```sh
        AUDIO_ARGS=""
        if [ "$AUDIO_MODE" = "playback" ]; then
            AUDIO_ARGS="audio_source=playback audio_dup=true"
        fi
        CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
          app_process / com.genymobile.scrcpy.Server \
          4.0 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false $AUDIO_ARGS \
          >> "$LOG" 2>&1 &
```

- [ ] **Step 2: 修改 `scrcpy-lan.sh`**

在第 7 行 `PORT="${1:-27183}"` 之后加一行：

```sh
AUDIO_MODE="${2:-output}"
```

把第 8-10 行替换为：

```sh
AUDIO_ARGS=""
if [ "$AUDIO_MODE" = "playback" ]; then
    AUDIO_ARGS="audio_source=playback audio_dup=true"
fi
CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
  app_process / com.genymobile.scrcpy.Server \
  4.0 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false $AUDIO_ARGS
```

- [ ] **Step 3: 语法检查（sh -n）**

```powershell
sh -n lan/scrcpy-lan-daemon.sh; sh -n lan/scrcpy-lan.sh; if ($LASTEXITCODE -eq 0) { Write-Output "syntax OK" }
```

预期：输出 `syntax OK`，无语法错误。

- [ ] **Step 4: dry-run 验证参数拼接**

用 bash 在本地模拟两种模式拼接出的 server 参数（不真正启动）：

```bash
for MODE in output playback; do
  PORT=27183; AUDIO_MODE=$MODE; AUDIO_ARGS=""
  if [ "$AUDIO_MODE" = "playback" ]; then AUDIO_ARGS="audio_source=playback audio_dup=true"; fi
  echo "MODE=$MODE -> 4.0 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false $AUDIO_ARGS"
done
```

预期：
```
MODE=output  -> 4.0 ... tunnel_port=27183 cleanup=false
MODE=playback-> 4.0 ... tunnel_port=27183 cleanup=false audio_source=playback audio_dup=true
```
（`output` 模式 `AUDIO_ARGS` 为空，与现状参数完全一致。）

- [ ] **Step 5: Commit**

```bash
git add lan/scrcpy-lan-daemon.sh lan/scrcpy-lan.sh
git commit -m "feat(lan): audio mode arg - playback enables capture+device output"
```

---

### Task 3: 构建 jar、部署并真机验证两种模式

**Files:**
- 产物：`scrcpy-lan-server.jar`（构建后改名，daemon 引用名）
- 部署目标：设备 `/data/local/tmp/`（root，Magisk/KernelSU）

**Interfaces:**
- Consumes: Task 1 的 server 代码、Task 2 的两个脚本。
- Produces: 真机验证通过——`output` 模式行为不变；`playback` 模式设备与客户端同时出声。

- [ ] **Step 1: 构建并改名 jar**

用本机既有构建流程产出 server 产物（gradle 或 `server/build_without_gradle.sh`），改名为 `scrcpy-lan-server.jar`。参考既有部署路径（设备 `/data/local/tmp/scrcpy-lan-server.jar`，见 `lan/scrcpy-lan.sh:8`）。

- [ ] **Step 2: 推送到设备并重启 daemon**

推 jar + `scrcpy-lan-daemon.sh` + `scrcpy-lan.sh` 到 `/data/local/tmp/`，重启 daemon（root）。默认启动保持旧行为：

```sh
sh /data/local/tmp/scrcpy-lan-daemon.sh 27183
```

- [ ] **Step 3: 真机验证旧方案不受影响（回归）**

设备放一段媒体，webui 浏览器确认能听到声音，设备静音（现状行为）。桌面客户端连 27183 同样验证。

- [ ] **Step 4: 真机验证新方案双端出声**

重启 daemon 启用新方案：

```sh
sh /data/local/tmp/scrcpy-lan-daemon.sh 27183 playback
```

设备放歌/开游戏/播视频，确认：① 设备扬声器出声；② webui 浏览器同时出声；③ 桌面客户端连 27183 同时出声。再用 `logcat`/daemon 日志确认无 `registerAudioPolicy` 失败（`AudioPlaybackCapture.java:96-99` 失败会抛 RuntimeException 并走 disable-stream）。

- [ ] **Step 5: 记录结果**

若 `playback` 模式捕获失败（尤其 Android 15，见设计文档"已知风险"），记录设备型号/系统版本与现象，供后续处理。若通过，本计划完成。

---

## Self-Review 记录

- **Spec 覆盖**：设计文档 4 项改动点——① `AudioPlaybackCapture.java` 规则扩展 + voice 顺序修复 → Task 1；② daemon `AUDIO_MODE` 参数 → Task 2；③ `scrcpy-lan.sh` 同步 → Task 2；④ autostart 保持默认 → 不涉及代码（文档明确为非代码部署选择），由 Task 3 部署步骤覆盖。边界（Android<13 降级、Android 15 bug、客户端参数不生效）均已在 Global Constraints 与 Task 3 记录。
- **占位符扫描**：无 TBD/TODO；每个代码步骤含完整可粘贴代码；验证步骤含实际命令与预期输出。
- **类型一致性**：`AUDIO_MODE`/`AUDIO_ARGS` 变量名在 Task 2 两个脚本中一致；server 参数 `audio_source=playback audio_dup=true` 与 `Options.java` 解析名（`audio_source`/`audio_dup`）一致。
