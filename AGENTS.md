# scrcpy-lan

**上游 scrcpy 的 fork**（Genymobile/scrcpy v5.0）+ 自研「局域网投屏 / Web 控制台」模块。

## 关键边界：哪些是本项目的，哪些是上游的
**上游代码尽量不动**：`app\`（Android 客户端）、`server\`（Android 端服务）、`doc\`（官方文档）、
`meson.build` / `build.gradle` / `settings.gradle` / `gradle\` / `gradlew*`、`assets\`。

**本 fork 的改动集中在这三处**：
    lan\        局域网发现与连接（自研）
    webui\      Web 控制台（自研）
    docs\       fork 自己的文档（v5.0-upgrade.md 记录最近升级，v4.1-upgrade.md 为历史）

## 构建
    .\gradlew.bat assembleRelease                            # Android 端 (app + server)
    meson setup build-win-v5.0 ; ninja -C build-win-v5.0     # Windows 原生客户端
    .\install_release.sh                                     # 发布安装

## 不入库（.gitignore 已覆盖）
    build\  build-win-v5.0\  release\  .gradle\  dist\  scrcpy-server  client-*.log

## 注意
- **根 `README.md` 是上游原文**，不含本 fork 说明 —— 接管时先看 `docs\` 与 git log 判断改动范围
- 上游升级流程见 `doc\develop.md`；本 fork 最近一次升级记录在 `docs\v5.0-upgrade.md`（v4.1-upgrade.md 为历史）
- 版本串约束：`lan\*.sh` 启动 server 时的第一个参数（当前 `5.0`）必须等于 `server/build.gradle` 的 `versionName`，否则 server 启动即报 version mismatch
- `.superpowers\` 是本地工具目录，非项目内容