#!/system/bin/sh
# scrcpy-lan server launcher (root, app_process)
# Usage: sh scrcpy-lan.sh [port] [audio_mode=output|playback]
# Uses scrcpy-lan-server.jar (not scrcpy-server.jar) to avoid being clobbered
# by a stock scrcpy client pushing its own server over the adb path.
# cleanup=false: the server must not delete its own jar (it stays resident).
PORT="${1:-27183}"
AUDIO_MODE="${2:-output}"
AUDIO_ARGS=""
if [ "$AUDIO_MODE" = "playback" ]; then
    # playback 源：全局捕获（MEDIA/GAME/UNKNOWN），设备默认静音；
    # 设备出声由客户端控制消息 audioDup 动态开启。
    AUDIO_ARGS="audio_source=playback"
fi
CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
  app_process / com.genymobile.scrcpy.Server \
  4.0 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false $AUDIO_ARGS
