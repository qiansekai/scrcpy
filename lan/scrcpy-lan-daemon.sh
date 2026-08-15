#!/system/bin/sh
# scrcpy-lan daemon: keep the scrcpy server alive on TCP 27183
# Liveness check via ps -A -o CMDLINE, with the [c] trick so the grep process
# itself does not match (its own cmdline contains the literal pattern).
# cleanup=false: the server must not delete its own jar (it stays resident).
# Behavior:
#   - app_process is started in the BACKGROUND (>&), so this loop keeps polling
#     with ps instead of being blocked inside the server process. This makes the
#     WAS_RUNNING flag meaningful: a normal session end (server was seen up,
#     then down) restarts immediately with no backoff.
#   - if the server never stays up (startup failures), exponential backoff
#     1s..30s cap is applied so a broken jar does not busy-loop or flood logs.
#   - the log is truncated before every launch, so it never grows unbounded.
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
          app_process / com.genymobile.scrcpy.Server \
          4.0 scid=-1 log_level=info tunnel_forward=true tunnel_port=$PORT cleanup=false \
          >> "$LOG" 2>&1 &
        sleep 1
    fi
done
