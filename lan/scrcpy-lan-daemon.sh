#!/system/bin/sh
# scrcpy-lan daemon: keep the mirroring server (27183) and the root admin
# server (27184) alive on TCP.
# Liveness check via ps -A -o CMDLINE, with the [c] trick so the grep process
# itself does not match (its own cmdline contains the literal pattern).
# cleanup=false: the servers must not delete their own jar (it stays resident).
# Behavior:
#   - app_process is started in the BACKGROUND (>&), so each guard loop keeps
#     polling with ps instead of being blocked inside the server process. This
#     makes the WAS_RUNNING flag meaningful: a normal session end (server was
#     seen up, then down) restarts immediately with no backoff.
#   - if a server never stays up (startup failures), exponential backoff
#     1s..30s cap is applied so a broken jar does not busy-loop.
#   - each server log is truncated before every launch of that server, so it
#     never grows unbounded and the two servers never clobber each other.
PORT="${1:-27183}"
AUDIO_MODE="${2:-output}"
ADMIN_PORT="${3:-27184}"
SERVER_LOG=/data/local/tmp/scrcpy-lan-daemon.log
ADMIN_LOG=/data/local/tmp/scrcpy-lan-admin.log

AUDIO_ARGS=""
if [ "$AUDIO_MODE" = "playback" ]; then
    # playback 源：全局捕获（MEDIA/GAME/UNKNOWN），设备默认静音；
    # 设备出声由客户端控制消息 audioDup 动态开启。
    AUDIO_ARGS="audio_source=playback"
fi

# guard <ps-match> <log> <app_process args...>
guard() {
    MATCH="$1"; LOG="$2"; shift 2
    FAILS=0
    WAS_RUNNING=0
    while true; do
        if ps -A -o CMDLINE 2>/dev/null | grep -q "$MATCH"; then
            WAS_RUNNING=1
            FAILS=0
            sleep 2
        else
            # the previous app_process child (if any) has exited by now (cmdline
            # gone), so wait reaps it instead of letting a zombie accumulate
            wait 2>/dev/null
            : > "$LOG"
            if [ "$WAS_RUNNING" != "1" ]; then
                FAILS=$((FAILS+1))
                DELAY=$(( FAILS > 15 ? 30 : FAILS ))
                sleep "$DELAY"
            else
                WAS_RUNNING=0
            fi
            CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
              app_process / "$@" \
              >> "$LOG" 2>&1 &
            sleep 1
        fi
    done
}

guard "[c]om.genymobile.scrcpy.Server" "$SERVER_LOG" \
    com.genymobile.scrcpy.Server \
    4.0 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false $AUDIO_ARGS &

guard "[c]om.genymobile.scrcpy.admin.AdminServer" "$ADMIN_LOG" \
    com.genymobile.scrcpy.admin.AdminServer \
    --admin-port $ADMIN_PORT &

wait
