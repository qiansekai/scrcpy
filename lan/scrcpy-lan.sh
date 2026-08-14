#!/system/bin/sh
# scrcpy-lan server launcher (root, app_process)
# Usage: sh scrcpy-lan.sh [port]
PORT="${1:-27183}"
CLASSPATH=/data/local/tmp/scrcpy-server.jar \
  app_process / com.genymobile.scrcpy.Server \
  3.3.4 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT
