#!/system/bin/sh
# scrcpy-lan daemon: keep the scrcpy server alive on TCP 27183
# Liveness check via ps -A -o CMDLINE, with the [c] trick so the grep process
# itself does not match (its own cmdline contains the literal pattern).
# cleanup=false: the server must not delete its own jar (it stays resident).
# Behavior:
#   - session ended normally (server previously ran) -> restart immediately
#   - repeated launch failures (server never stays up) -> exponential backoff
#     (1s, 2s, ... up to 30s cap), so a broken jar does not busy-loop or flood
#     the log
#   - log is truncated before every launch, so it never grows unbounded
PORT="${1:-27183}"
FAILS=0
WAS_RUNNING=0
LOG=/data/local/tmp/scrcpy-lan-daemon.log
while true; do
    if ps -A -o CMDLINE 2>/dev/null | grep -q "[c]om.genymobile.scrcpy.Server"; then
        WAS_RUNNING=1
        FAILS=0
        sleep 2
    else
        : > "$LOG"
        if [ "$WAS_RUNNING" = "1" ]; then
            WAS_RUNNING=0
            CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
              app_process / com.genymobile.scrcpy.Server \
              3.3.4 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false \
              >> "$LOG" 2>&1
            sleep 2
        else
            FAILS=$((FAILS+1))
            DELAY=$(( FAILS > 15 ? 30 : FAILS ))
            CLASSPATH=/data/local/tmp/scrcpy-lan-server.jar \
              app_process / com.genymobile.scrcpy.Server \
              3.3.4 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false \
              >> "$LOG" 2>&1
            sleep "$DELAY"
        fi
    fi
done
