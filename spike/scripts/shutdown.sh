#!/usr/bin/env bash
# Shutdown check for the Pisar spike: starts the real application (a GUI
# window), lets it start Neovim, ends the application with a signal and proves
# that no Neovim process survives. Run from anywhere:
#   bash spike/scripts/shutdown.sh
#
# Cases: SIGTERM and SIGINT (the application shuts down in order), SIGHUP and
# SIGKILL (it dies without running its shutdown code, like a crash). The normal
# Quit path is covered by scripts/bench.sh, where the application quits itself.
#
# The script only signals the application it started and the Neovim processes
# that belong to it (found through the application's own socket path, which
# contains the application's pid). It never matches processes by name alone.
#
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BIN="$ROOT/spike/bin/spike"

TMP=""
APP_PID=""
NVIM_PIDS=""
FAILS=0

ok() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s\n' "$1"
  FAILS=$((FAILS + 1))
}
note() { printf '     %s\n' "$1"; }

# is_running <pid>: true for a live process; a zombie does not count.
is_running() {
  local st
  st=$(ps -o stat= -p "$1" 2>/dev/null || true)
  [[ -n "$st" && "$st" != Z* ]]
}

descendants() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null || true); do
    echo "$child"
    descendants "$child"
  done
}

# kill_ours: last resort for what this script started and could not stop.
kill_ours() {
  local p
  for p in $APP_PID $NVIM_PIDS; do
    if is_running "$p"; then kill -KILL "$p" 2>/dev/null || true; fi
  done
  if [[ -n "$APP_PID" ]]; then wait "$APP_PID" 2>/dev/null || true; fi
  APP_PID=""
  NVIM_PIDS=""
}

cleanup() {
  local status=$?
  trap - EXIT
  kill_ours
  if [[ -n "$TMP" && "$TMP" == */pisar-shutdown.* ]]; then rm -rf "$TMP"; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

wait_for() {
  local limit=$(($1 * 10)) n=0
  shift
  while ! "$@"; do
    n=$((n + 1))
    if [[ $n -ge $limit ]]; then return 1; fi
    sleep 0.1
  done
}

# run_case <signal> <expects ordered shutdown: yes|no>
run_case() {
  local sig=$1 ordered=$2
  local trace="$TMP/$sig.trace"
  echo "--- case: SIG$sig"

  PISAR_SPIKE_TRACE="$trace" "$BIN" "$TMP/sample.md" >"$TMP/$sig.app.log" 2>&1 &
  APP_PID=$!
  app_up() {
    is_running "$APP_PID" || return 1
    grep -Eq ' start cols=[0-9]+ rows=[0-9]+ err=<nil>$' "$trace" 2>/dev/null
  }
  if ! wait_for 20 app_up; then
    fail "SIG$sig: application started Neovim within 20 s"
    kill_ours
    return 0
  fi

  # The socket path is unique to this application (it holds its pid), so it
  # identifies both Neovim processes: the TUI, and the server, which lives in
  # a session of its own and would be the one to survive.
  local sock="" pid cmd
  for pid in $(descendants "$APP_PID"); do
    cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
    if [[ "$cmd" =~ --listen\ ([^\ ]+) ]]; then
      sock=${BASH_REMATCH[1]}
      break
    fi
  done
  if [[ -z "$sock" || "$sock" != *"nvim-$APP_PID-"* ]]; then
    fail "SIG$sig: found the socket of this application's Neovim"
    kill_ours
    return 0
  fi
  NVIM_PIDS=$(pgrep -f -- "$sock" | tr '\n' ' ')
  local count
  count=$(wc -w <<<"$NVIM_PIDS" | tr -d ' ')
  if [[ "$count" -ge 2 ]]; then
    ok "SIG$sig: Neovim runs as $count processes (TUI and server): $NVIM_PIDS"
  else
    fail "SIG$sig: expected the TUI and the server process, found: $NVIM_PIDS"
  fi

  local started
  started=$(date +%s)
  kill "-$sig" "$APP_PID"

  all_gone() {
    local p
    for p in $APP_PID $NVIM_PIDS; do
      if is_running "$p"; then return 1; fi
    done
  }
  if wait_for 10 all_gone; then
    ok "SIG$sig: application and every Neovim process are gone within $(($(date +%s) - started + 1)) s"
  else
    local p
    for p in $NVIM_PIDS; do
      if is_running "$p"; then fail "SIG$sig: Neovim process $p survived: $(ps -o command= -p "$p")"; fi
    done
    if is_running "$APP_PID"; then fail "SIG$sig: the application itself is still running"; fi
  fi
  wait "$APP_PID" 2>/dev/null || true

  if [[ -e "$sock" ]]; then fail "SIG$sig: the RPC socket is gone"; else ok "SIG$sig: the RPC socket is gone"; fi

  # The private directory (init.lua and the socket) is removed by the ordered
  # shutdown only. After a crash it stays behind; that is reported, not failed.
  local dir
  dir=$(dirname "$sock")
  if [[ "$ordered" == yes ]]; then
    if [[ -d "$dir" ]]; then fail "SIG$sig: the private directory was removed"; else ok "SIG$sig: the private directory was removed"; fi
    if tail -n 1 "$trace" | grep -q ' stats '; then ok "SIG$sig: shutdown code ran (trace ends with stats)"; else fail "SIG$sig: shutdown code ran (trace ends with stats)"; fi
  elif [[ -d "$dir" ]]; then
    note "left behind after the crash: $dir ($(ls -A "$dir" | tr '\n' ' '))"
  fi
  # Remove a leftover directory of ours, only if it is the expected one with
  # nothing but init.lua in it.
  if [[ -d "$dir" && "$dir" == */pisar-spike-* && "$(ls -A "$dir")" == "init.lua" ]]; then
    rm "$dir/init.lua" && rmdir "$dir"
  fi
  APP_PID=""
  NVIM_PIDS=""
}

TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-shutdown.XXXXXX")
if (cd "$ROOT" && task -d spike build >"$TMP/build.log" 2>&1); then
  ok "task -d spike build"
else
  fail "task -d spike build"
  tail -n 30 "$TMP/build.log"
  exit 1
fi
printf '# Probni dokument\n\nJedan redak.\n' >"$TMP/sample.md"

run_case TERM yes
run_case INT yes
run_case HUP no
run_case KILL no

echo
if [[ "$FAILS" -eq 0 ]]; then
  echo "shutdown: all checks passed"
else
  echo "shutdown: $FAILS check(s) FAILED"
fi
exit "$FAILS"
