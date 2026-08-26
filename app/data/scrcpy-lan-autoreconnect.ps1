# scrcpy-lan 桌面端自动重连看门狗（PowerShell）
# 用法: 双击 scrcpy-lan-autoreconnect.bat，或
#   powershell -NoProfile -ExecutionPolicy Bypass -File scrcpy-lan-autoreconnect.ps1 [-HostIp <ip>] [-TunnelPort <port>]
# 行为:
#   - 设备服务（tunnel 端口）在线才启动 scrcpy；离线时每 3 秒探测一次。
#   - scrcpy 退出码为 0（窗口正常关闭）时看门狗退出，不会反复弹窗。
#   - 退出码非 0（连接失败 / 设备断开）时 3 秒后自动重新拉起。
# Ctrl+C 停止。
param(
    [string]$HostIp = "192.168.1.19",
    [int]$TunnelPort = 27183
)
$ErrorActionPreference = "SilentlyContinue"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
$scrcpy = Join-Path $dir "scrcpy.exe"
$scrcpyArgs = @("--no-adb", "--tunnel-host=$HostIp", "--tunnel-port=$TunnelPort", "--reconnect")

function Test-Tunnel {
    # TCP 探测 tunnel 端口：通了说明设备端 server 在线（不建会话，仅连接即关）。
    try {
        $c = New-Object Net.Sockets.TcpClient
        $task = $c.ConnectAsync($HostIp, $TunnelPort)
        $null = $task.Wait(1500)
        $ok = $c.Connected
        $c.Close()
        return $ok
    } catch {
        return $false
    }
}

Write-Host "=== scrcpy-lan 自动重连 ==="
Write-Host ("目标: {0}:{1}    Ctrl+C 停止" -f $HostIp, $TunnelPort)
Write-Host ""

while ($true) {
    if (-not (Test-Tunnel)) {
        Write-Host ("[{0}] 设备服务离线，等待中..." -f (Get-Date -Format "HH:mm:ss"))
        Start-Sleep -Seconds 3
        continue
    }
    Write-Host ("[{0}] 启动 scrcpy..." -f (Get-Date -Format "HH:mm:ss"))
    $p = Start-Process -FilePath $scrcpy -ArgumentList $scrcpyArgs -PassThru
    $p.WaitForExit()
    if ($p.ExitCode -eq 0) {
        Write-Host "窗口已正常关闭，自动重连退出。"
        break
    }
    Write-Host ("[{0}] 连接断开 (exit={1})，3 秒后自动重连..." -f (Get-Date -Format "HH:mm:ss"), $p.ExitCode)
    Start-Sleep -Seconds 3
}
