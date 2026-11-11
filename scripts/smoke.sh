#!/usr/bin/env bash
# Smoke test for Pisar: starts the real application (a GUI window) with its
# home in a temporary folder and a seeded project, and proves from the debug
# log that the frontend loaded the project tree, that a file created from
# outside refreshes the tree, and that quitting leaves nothing behind.
#   bash scripts/smoke.sh
#
# No keys or clicks are sent. Everything temporary lives in one mktemp
# directory; the script only signals the application it started. The user's
# real configuration (~/Library/Application Support/Pisar) is never touched.
#
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN="$ROOT/bin/pisar"
REAL_HOME_DIR="$HOME/Library/Application Support/Pisar"

TMP=""
APP_PID=""
FAILS=0

ok() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s\n' "$1"
  FAILS=$((FAILS + 1))
}
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

stop_app() {
  [[ -n "$APP_PID" ]] || return 0
  if is_running "$APP_PID"; then
    kill -TERM "$APP_PID" 2>/dev/null || true
    local i
    for i in $(seq 1 20); do
      is_running "$APP_PID" || break
      sleep 0.25
    done
    if is_running "$APP_PID"; then kill -KILL "$APP_PID" 2>/dev/null || true; fi
  fi
  wait "$APP_PID" 2>/dev/null || true
  APP_PID=""
}

cleanup() {
  local status=$?
  trap - EXIT
  stop_app
  if [[ -n "$TMP" && "$TMP" == */pisar-smoke.* ]]; then rm -rf "$TMP"; fi
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

# tree_calls <log> <top>: number of Tree calls that returned <top> top-level nodes.
tree_calls() {
  grep -c "method=Tree project=smoke-project top=$2\$" "$1" 2>/dev/null || true
}

REAL_HOME_BEFORE=absent
if [[ -e "$REAL_HOME_DIR" ]]; then REAL_HOME_BEFORE=$(ls -la "$REAL_HOME_DIR" | shasum | cut -d' ' -f1); fi

TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-smoke.XXXXXX")
if (cd "$ROOT" && task build >"$TMP/build.log" 2>&1); then
  ok "task build"
else
  fail "task build"
  tail -n 30 "$TMP/build.log"
  exit 1
fi

# A project with two visible entries (a document and a folder that holds one)
# and two things the tree must not show.
PROJ="$TMP/Probni projekt"
mkdir -p "$PROJ/mapa" "$TMP/home"
printf '# Bilješke\n' >"$PROJ/Bilješke.md"
printf 'x' >"$PROJ/mapa/Članak.docx"
printf 'skriveno\n' >"$PROJ/.skriveno.md"
printf 'x' >"$PROJ/slika.png"
CONFIG="$TMP/home/config.json"
cat >"$CONFIG" <<JSON
{
  "version": 1,
  "projects": [
    {
      "id": "smoke-project",
      "name": "Probni projekt",
      "path": "$PROJ"
    }
  ]
}
JSON
cp "$CONFIG" "$TMP/config.before"

LOG="$TMP/app.log"
PISAR_HOME="$TMP/home" PISAR_LOG=debug "$BIN" >"$LOG" 2>&1 &
APP_PID=$!

frontend_loaded() {
  is_running "$APP_PID" || return 1
  [[ "$(tree_calls "$LOG" 2)" -ge 1 ]]
}
if wait_for 20 frontend_loaded; then
  ok "frontend started and asked for the tree: 2 top-level entries (hidden file and picture left out)"
else
  fail "frontend started and asked for the tree with 2 top-level entries within 20 s"
  sed 's/^/     log: /' "$LOG" | tail -n 20
fi
check "frontend asked for the project list" grep -q 'method=Projects projects=1$' "$LOG"

# A change made from outside must reach the tree without any action in the app.
printf '# Novo\n' >"$PROJ/Novo.md"
refreshed() { [[ "$(tree_calls "$LOG" 3)" -ge 1 ]]; }
if wait_for 10 refreshed; then
  ok "a document created from outside refreshed the tree (3 top-level entries)"
else
  fail "a document created from outside refreshed the tree within 10 s"
  grep 'method=Tree' "$LOG" | sed 's/^/     log: /' | tail -n 5
fi

# A change the tree does not show must not cause a refresh.
before=$(grep -c 'method=Tree' "$LOG" || true)
printf 'x' >"$PROJ/druga-slika.png"
printf 'x' >"$PROJ/.DS_Store"
sleep 2
after=$(grep -c 'method=Tree' "$LOG" || true)
check "a picture and a hidden file did not refresh the tree ($before calls before, $after after)" test "$before" = "$after"

kill -TERM "$APP_PID"
app_gone() { ! is_running "$APP_PID"; }
if wait_for 10 app_gone; then
  ok "application exited within 10 s of SIGTERM"
else
  fail "application exited within 10 s of SIGTERM"
fi
wait "$APP_PID" 2>/dev/null || true
APP_PID=""

check "config.json is unchanged" cmp -s "$CONFIG" "$TMP/config.before"
check "nothing but config.json in the temporary home" test "$(ls -A "$TMP/home")" = "config.json"
REAL_HOME_AFTER=absent
if [[ -e "$REAL_HOME_DIR" ]]; then REAL_HOME_AFTER=$(ls -la "$REAL_HOME_DIR" | shasum | cut -d' ' -f1); fi
check "the real configuration folder was not touched ($REAL_HOME_BEFORE)" test "$REAL_HOME_BEFORE" = "$REAL_HOME_AFTER"

echo
if [[ "$FAILS" -eq 0 ]]; then
  echo "smoke: all checks passed"
else
  echo "smoke: $FAILS check(s) FAILED"
fi
exit "$FAILS"
