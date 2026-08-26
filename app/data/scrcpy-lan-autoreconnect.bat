@echo off
rem scrcpy-lan 桌面端自动重连启动器（双击运行）
rem 参数透传，例如: scrcpy-lan-autoreconnect.bat -HostIp 192.168.1.50 -TunnelPort 27183
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scrcpy-lan-autoreconnect.ps1" %*
