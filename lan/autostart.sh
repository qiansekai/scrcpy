#!/system/bin/sh
# scrcpy-lan autostart (Magisk/KernelSU service.d, runs as root at late_start)
nohup sh /data/local/tmp/scrcpy-lan-daemon.sh 27183 >/data/local/tmp/scrcpy-lan-daemon.log 2>&1 &
