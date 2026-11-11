#!/usr/bin/env bash
# Smoke test for the Pisar spike: starts the real application (a GUI window),
# lets it open a file in Neovim, and proves from the trace file and from
# Neovim's own RPC socket that the terminal works. Run from anywhere:
#   bash spike/scripts/smoke.sh
#
# Everything temporary lives in one mktemp directory. The script only ever
# signals the application it started and the Neovim processes that are its
# descendants; it never matches processes by name.
#
# Written for the bash 3.2 that macOS ships (no mapfile, no associative arrays).
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BIN="$ROOT/spike/bin/spike"

TMP=""
APP_PID=""
FAILS=0

ok() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s\n' "$1"
  FAILS=$((FAILS + 1))
}
# check <name> <command...>: one ok/FAIL line, never aborts the script.
check() {
  local name=$1
  shift
  if "$@"; then ok "$name"; else fail "$name"; fi
}

# is_running <pid>: true for a live process. A zombie (exited, not yet reaped
# by wait) does not count, because kill -0 would still succeed on it.
is_running() {
  local st
  st=$(ps -o stat= -p "$1" 2>/dev/null || true)
  [[ -n "$st" && "$st" != Z* ]]
}

# descendants <pid>: all children, grandchildren and so on, one pid per line.
descendants() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null || true); do
    echo "$child"
    descendants "$child"
  done
}

# stop_app: stops the application this script started, and its descendants,
# if it still runs. Only ever signals pids it recorded.
stop_app() {
  [[ -n "$APP_PID" ]] || return 0
  if is_running "$APP_PID"; then
    local kids i k
    kids=$(descendants "$APP_PID")
    kill -TERM "$APP_PID" 2>/dev/null || true
    for i in $(seq 1 20); do
      is_running "$APP_PID" || break
      sleep 0.25
    done
    if is_running "$APP_PID"; then kill -KILL "$APP_PID" 2>/dev/null || true; fi
    for k in $kids; do
      if is_running "$k"; then kill -KILL "$k" 2>/dev/null || true; fi
    done
  fi
  wait "$APP_PID" 2>/dev/null || true
  APP_PID=""
}

cleanup() {
  local status=$?
  trap - EXIT
  stop_app
  # TMP comes from our own mktemp; the pattern check keeps a bug from ever
  # pointing rm at anything else.
  if [[ -n "$TMP" && "$TMP" == */pisar-smoke.* ]]; then rm -rf "$TMP"; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# wait_for <seconds> <command...>: polls every 0.1 s until the command succeeds.
wait_for() {
  local limit=$(($1 * 10)) n=0
  shift
  while ! "$@"; do
    n=$((n + 1))
    if [[ $n -ge $limit ]]; then return 1; fi
    sleep 0.1
  done
}

# remote_expr <socket> <expression>: asks Neovim over its RPC socket. A
# watchdog keeps a stuck server from hanging the script.
remote_expr() {
  local out="$TMP/remote.out" pid i
  nvim --server "$1" --remote-expr "$2" >"$out" 2>"$TMP/remote.err" &
  pid=$!
  for i in $(seq 1 50); do
    is_running "$pid" || break
    sleep 0.1
  done
  if is_running "$pid"; then
    kill -KILL "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    return 1
  fi
  wait "$pid" || return 1
  cat "$out"
}

# last_size <trace>: "cols rows" from the newest start or resize line.
last_size() {
  sed -nE 's/^[^ ]+ (start|resize) cols=([0-9]+) rows=([0-9]+).*/\2 \3/p' "$1" | tail -n 1
}

# run_case <label> <renderer env value or empty> <required renderer>
run_case() {
  local label=$1 renderer=$2 want=$3
  local dir="$TMP/$label" trace="$TMP/$label/trace.log"
  mkdir -p "$dir"
  echo "--- case: $label (renderer=${renderer:-default})"

  # 3. start the application, with tracing on.
  local envs=("PISAR_SPIKE_TRACE=$trace")
  if [[ -n "$renderer" ]]; then envs+=("PISAR_SPIKE_RENDERER=$renderer"); fi
  env "${envs[@]}" "$BIN" "$TMP/sample.md" >"$dir/app.log" 2>&1 &
  APP_PID=$!

  # 4. wait for Neovim to be up with the file open.
  app_up() {
    is_running "$APP_PID" || return 1 # no point waiting for a dead app
    grep -Eq ' start cols=[0-9]+ rows=[0-9]+ err=<nil>$' "$trace" 2>/dev/null
  }
  if ! wait_for 20 app_up; then
    fail "$label: application started Neovim within 20 s (start err=<nil>)"
    [[ -f "$trace" ]] && sed 's/^/     trace: /' "$trace"
    sed 's/^/     app: /' "$dir/app.log"
    stop_app
    return 0
  fi
  ok "$label: application started Neovim (start err=<nil>)"

  # The frontend keeps writing after Start returns; give it a moment.
  wait_for 5 grep -q ' ui cell ' "$trace" || true

  # 5. what the frontend reported.
  check "$label: font loaded before the terminal opened" grep -q ' ui font loaded=true$' "$trace"
  check "$label: first pty data reached the frontend" grep -Eq ' ui first data bytes=[0-9]+' "$trace"
  local start_line cols rows
  start_line=$(grep -E ' start cols=[0-9]+ rows=[0-9]+ err=<nil>$' "$trace" | head -n 1)
  cols=$(sed -E 's/.* cols=([0-9]+).*/\1/' <<<"$start_line")
  rows=$(sed -E 's/.* rows=([0-9]+).*/\1/' <<<"$start_line")
  check "$label: grid is at least 100x25 (got ${cols}x${rows})" \
    test "$cols" -ge 100 -a "$rows" -ge 25
  echo "     $(grep ' ui cell ' "$trace" | head -n 1 | sed -E 's/^[^ ]+ //')"

  # 6. ask Neovim itself, over its socket.
  local sock="" pid cmd
  for pid in $(descendants "$APP_PID"); do
    cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
    if [[ "$cmd" =~ --listen\ ([^\ ]+) ]]; then
      sock=${BASH_REMATCH[1]}
      break
    fi
  done
  if [[ -z "$sock" ]]; then
    fail "$label: found Neovim's socket (--listen of a descendant process)"
  else
    ok "$label: found Neovim's socket"
    local expect_size got
    expect_size=$(last_size "$trace")
    got=$(remote_expr "$sock" 'printf("%d %d %s", &columns, &lines, fnamemodify(bufname(""), ":t"))') || got=""
    echo "     neovim says: '$got' (trace says: '$expect_size sample.md')"
    check "$label: Neovim's &columns, &lines and buffer match the trace and sample.md" \
      test "$got" = "$expect_size sample.md"
  fi
  local nvim_pids
  nvim_pids=$(descendants "$APP_PID")

  # 7. everything the terminal wrote by itself: no key was pressed.
  echo "     automatic terminal replies (trace 'write' lines, no key pressed):"
  grep ' write ' "$trace" | sed 's/^/       /' || echo "       (none)"

  # renderer
  if [[ "$want" = webgl ]]; then
    if grep -Eq ' ui cell .* renderer=webgl$' "$trace"; then
      ok "$label: renderer is webgl"
    elif grep -Eq ' ui renderer webgl (lost its context|unavailable)' "$trace"; then
      ok "$label: webgl was refused and the trace records the fall back to dom"
      grep ' ui renderer ' "$trace" | sed 's/^/       /'
    else
      fail "$label: trace says neither renderer=webgl nor a fall back"
    fi
  else
    check "$label: renderer is dom" grep -Eq ' ui cell .* renderer=dom$' "$trace"
  fi

  # 8. shut down with SIGTERM and look for leftovers.
  kill -TERM "$APP_PID"
  app_gone() { ! is_running "$APP_PID"; }
  if wait_for 10 app_gone; then
    ok "$label: application exited within 10 s of SIGTERM"
  else
    fail "$label: application exited within 10 s of SIGTERM"
    kill -KILL "$APP_PID" 2>/dev/null || true
  fi
  wait "$APP_PID" 2>/dev/null || true
  local leftover=0
  for pid in $nvim_pids; do
    if is_running "$pid"; then
      leftover=1
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
  check "$label: no Neovim process of ours is left" test "$leftover" -eq 0
  if [[ -n "$sock" ]]; then check "$label: Neovim's socket is gone" test ! -e "$sock"; fi
  APP_PID=""
  check "$label: trace ends with stats and bytesOut > 0" \
    bash -c 'tail -n 1 "$1" | grep -Eq " stats bytesOut=[1-9][0-9]* chunks=[0-9]+ largest=[0-9]+ bytesIn=[0-9]+$"' _ "$trace"
  echo "     $(tail -n 1 "$trace" | sed -E 's/^[^ ]+ //')"
}

# 1. build
TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-smoke.XXXXXX")
if (cd "$ROOT" && task -d spike build >"$TMP/build.log" 2>&1); then
  ok "task -d spike build"
else
  fail "task -d spike build"
  tail -n 30 "$TMP/build.log"
  exit 1
fi

# 2. a sample document, with the Croatian letters
cat >"$TMP/sample.md" <<'EOF'
# Probni dokument

Prvi redak teksta.
Hrvatska slova: č ć š ž đ Č Ć Š Ž Đ.
EOF

run_case dom "" dom
run_case webgl webgl webgl

echo
if [[ "$FAILS" -eq 0 ]]; then
  echo "smoke: all checks passed"
else
  echo "smoke: $FAILS check(s) FAILED"
fi
exit "$FAILS"
