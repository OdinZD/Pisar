#!/usr/bin/env bash
# Shutdown check for Pisar: starts the real application (a GUI window) with a
# document, so that Neovim runs, ends the application with a signal and proves
# that no Neovim process survives.
#   bash scripts/shutdown.sh
#
# Cases: SIGTERM and SIGINT (the application shuts down in order), SIGHUP and
# SIGKILL (it dies without running its shutdown code, like a crash). After the
# crashes the socket folder stays behind; the last case starts the application
# once more and proves that it removes the folders of dead runs.
#
# No keys or clicks are sent. The application's home is a temporary folder; the
# user's real configuration is never touched. The script only signals the
# application it started and the Neovim processes that belong to it (found
# through the socket path, which contains the application's pid).
#
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN="$ROOT/bin/pisar"
REAL_HOME_DIR="$HOME/Library/Application Support/Pisar"

TMP=""
APP_PID=""
NVIM_PIDS=""
SOCK=""
FAILS=0

ok() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s\n' "$1"
  FAILS=$((FAILS + 1))
}
note() { printf '     %s\n' "$1"; }
check() {
  local name=$1
  shift
  if "$@"; then ok "$name"; else fail "$name"; fi
}

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
  # Socket folders of the runs this script crashed on purpose.
  local d
  for d in $STALE; do
    if [[ -d "$d" && "$d" == */pisar-run-* ]]; then rm -rf "$d"; fi
  done
  if [[ -n "$TMP" && "$TMP" == */pisar-shutdown.* ]]; then rm -rf "$TMP"; fi
  exit "$status"
}
STALE=""
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

# start_app <label>: starts the application with the sample document and waits
# until Neovim shows it. Sets APP_PID, SOCK and NVIM_PIDS. Returns 1 on failure.
start_app() {
  local label=$1 log="$TMP/$1.log" pid cmd
  PISAR_HOME="$TMP/home" PISAR_LOG=debug "$BIN" "$TMP/sample.md" >"$log" 2>&1 &
  APP_PID=$!
  SOCK=""
  NVIM_PIDS=""
  editor_up() {
    is_running "$APP_PID" || return 1
    grep -q 'event=document:active' "$log" 2>/dev/null
  }
  if ! wait_for 20 editor_up; then
    fail "$label: application started Neovim within 20 s"
    kill_ours
    return 1
  fi
  for pid in $(descendants "$APP_PID"); do
    cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
    if [[ "$cmd" =~ --listen\ ([^\ ]+) ]]; then
      SOCK=${BASH_REMATCH[1]}
      break
    fi
  done
  # The socket path holds the application's pid twice (folder and file), so it
  # identifies both Neovim processes: the TUI, and the server, which lives in a
  # session of its own and would be the one to survive.
  if [[ -z "$SOCK" || "$SOCK" != *"pisar-run-$APP_PID-"* ]]; then
    fail "$label: found the socket of this application's Neovim"
    kill_ours
    return 1
  fi
  NVIM_PIDS=$(pgrep -f -- "$SOCK" | tr '\n' ' ')
  local count
  count=$(wc -w <<<"$NVIM_PIDS" | tr -d ' ')
  if [[ "$count" -ge 2 ]]; then
    ok "$label: Neovim runs as $count processes (TUI and server): $NVIM_PIDS"
  else
    fail "$label: expected the TUI and the server process, found: $NVIM_PIDS"
  fi
}

all_gone() {
  local p
  for p in $APP_PID $NVIM_PIDS; do
    if is_running "$p"; then return 1; fi
  done
}

# run_case <signal> <ordered shutdown: yes|no>
run_case() {
  local sig=$1 ordered=$2 label="SIG$1" dir p
  echo "--- case: $label"
  start_app "$label" || return 0
  dir=$(dirname "$SOCK")

  kill "-$sig" "$APP_PID"
  if wait_for 10 all_gone; then
    ok "$label: application and every Neovim process are gone within 10 s"
  else
    for p in $NVIM_PIDS; do
      if is_running "$p"; then fail "$label: Neovim process $p survived: $(ps -o command= -p "$p")"; fi
    done
    if is_running "$APP_PID"; then fail "$label: the application itself is still running"; fi
  fi
  wait "$APP_PID" 2>/dev/null || true

  if [[ "$ordered" == yes ]]; then
    check "$label: the RPC socket and its folder were removed" test ! -e "$SOCK" -a ! -e "$dir"
  else
    check "$label: the RPC socket is gone" test ! -e "$SOCK"
    if [[ -d "$dir" ]]; then
      note "left behind after the crash: $dir"
      STALE="$STALE $dir"
    fi
  fi
  APP_PID=""
  NVIM_PIDS=""
}

REAL_HOME_BEFORE=absent
if [[ -e "$REAL_HOME_DIR" ]]; then REAL_HOME_BEFORE=$(ls -la "$REAL_HOME_DIR" | shasum | cut -d' ' -f1); fi

TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-shutdown.XXXXXX")
TMP=${TMP//\/\//\/}
if (cd "$ROOT" && task build >"$TMP/build.log" 2>&1); then
  ok "task build"
else
  fail "task build"
  tail -n 30 "$TMP/build.log"
  exit 1
fi
mkdir -p "$TMP/home"
printf '# Probni dokument\n\nJedan redak.\n' >"$TMP/sample.md"

run_case TERM yes
run_case INT yes
run_case HUP no
run_case KILL no

# The next start removes the socket folders that the two crashes left behind.
echo "--- case: restart after the crashes"
if [[ -z "${STALE// /}" ]]; then
  note "no socket folder was left behind, nothing to clean"
elif start_app restart; then
  left=""
  for d in $STALE; do
    if [[ -e "$d" ]]; then left="$left $d"; fi
  done
  check "restart: socket folders of the crashed runs were removed (${left:-none left})" test -z "$left"
  check "restart: the socket folder of the running application is still there" test -d "$(dirname "$SOCK")"
  kill -TERM "$APP_PID"
  check "restart: application and Neovim exited" wait_for 10 all_gone
  wait "$APP_PID" 2>/dev/null || true
  APP_PID=""
  NVIM_PIDS=""
fi

REAL_HOME_AFTER=absent
if [[ -e "$REAL_HOME_DIR" ]]; then REAL_HOME_AFTER=$(ls -la "$REAL_HOME_DIR" | shasum | cut -d' ' -f1); fi
check "the real configuration folder was not touched ($REAL_HOME_BEFORE)" test "$REAL_HOME_BEFORE" = "$REAL_HOME_AFTER"

echo
if [[ "$FAILS" -eq 0 ]]; then
  echo "shutdown: all checks passed"
else
  echo "shutdown: $FAILS check(s) FAILED"
fi
exit "$FAILS"
