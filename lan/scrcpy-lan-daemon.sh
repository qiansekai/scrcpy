#!/system/bin/sh
# scrcpy-lan daemon: keep the scrcpy server alive
PORT="${1:-27183}"
while true; do
    # if already listening, do nothing
    if ! ss -ltn 2>/dev/null | grep -q ":$PORT "; then
        CLASSPATH=/data/local/tmp/scrcpy-server.jar \
          app_process / com.genymobile.scrcpy.Server \
          3.3.4 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT
    fi
    sleep 2
done
