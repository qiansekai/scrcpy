#!/system/bin/sh
# scrcpy-lan daemon: keep the scrcpy server alive
# Liveness check via ps -A -o CMDLINE, with the [c] trick so the grep process
# itself does not match (its own cmdline contains the literal pattern).
# cleanup=false: the server must not delete its own jar (it stays resident).
PORT="${1:-27183}"
while true; do
    if ! ps -A -o CMDLINE 2>/dev/null | grep -q "[c]om.genymobile.scrcpy.Server"; then
        CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
          app_process / com.genymobile.scrcpy.Server \
          3.3.4 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false
    fi
    sleep 2
done
