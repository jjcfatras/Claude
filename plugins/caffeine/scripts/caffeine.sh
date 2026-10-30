#!/usr/bin/env bash
# Hook: keep macOS awake while a Claude Code session is working.
#   caffeine.sh on   UserPromptSubmit/PreToolUse (async): refresh this session's heartbeat; start its watcher if absent.
#   caffeine.sh off  Notification/Stop/SessionEnd: drop the heartbeat and stop the watcher.
# The watcher is `caffeinate -i` around a loop that lives while the heartbeat is under 15 min old,
# so a session that dies without `off` still releases the assertion.
set -u

# ponytail: macOS only; add a systemd-inhibit branch when Linux matters
command -v caffeinate > /dev/null || exit 0

sid=$(sed -n 's/.*"session_id" *: *"\([^"]*\)".*/\1/p')
[ -n "$sid" ] || exit 0

dir="${CLAUDE_PLUGIN_DATA:-${TMPDIR:-/tmp}/claude-caffeine}"
mkdir -p "$dir"
beat="$dir/$sid.beat"
pidf="$dir/$sid.pid"

# `caffeinate -i <loop>` execs the loop in its own pid and asserts from a forked child that exits
# with it, so the recorded pid is the loop. Print it only while its argv still names this heartbeat,
# so a recycled pid is never signalled.
watcher() {
  local pid
  pid=$(cat "$pidf" 2> /dev/null) || return 1
  case "$(ps -ww -o args= -p "$pid" 2> /dev/null)" in
    *"$beat"*) echo "$pid" ;;
    *) return 1 ;;
  esac
}

case "${1:-}" in
  on)
    touch "$beat"
    watcher > /dev/null && exit 0
    # ponytail: two parallel first calls can start two watchers; the extra one ends 15 min after the last heartbeat
    caffeinate -i sh -c 'while [ -n "$(find "$1" -mmin -15 2>/dev/null)" ]; do sleep 60; done' _ "$beat" \
      < /dev/null > /dev/null 2>&1 &
    echo $! > "$pidf"
    ;;
  off)
    rm -f "$beat"
    if pid=$(watcher); then kill "$pid"; fi # the asserting caffeinate child exits with the loop
    rm -f "$pidf"
    ;;
esac
exit 0
