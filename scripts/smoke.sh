#!/usr/bin/env bash
# Smoke test for Pisar: starts the real application (a GUI window) with its
# home in a temporary folder, a seeded project and a document named on the
# command line. It proves from the debug log and from Neovim's own RPC socket
# that the frontend loaded the tree and started the editor, that Neovim shows
# the document with the generated colorscheme, that buffer and state changes
# reach the application, that a file created from outside refreshes the tree,
# and that quitting leaves nothing behind.
#   bash scripts/smoke.sh
#
# No keys or clicks are sent: Neovim is driven through its socket. Everything temporary lives in one mktemp
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

# descendants <pid>: all children, grandchildren and so on, one pid per line.
descendants() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null || true); do
    echo "$child"
    descendants "$child"
  done
}

# remote <socket> <flag> <argument>: one call to Neovim's server, with a
# watchdog so a stuck server cannot hang the script.
remote() {
  local out="$TMP/remote.out" pid i
  nvim --server "$1" "$2" "$3" >"$out" 2>"$TMP/remote.err" &
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

# logged <pattern>: the debug log has a line matching the extended regex.
logged() { grep -Eq "$1" "$LOG" 2>/dev/null; }

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
# $TMPDIR ends in a slash on macOS; drop the double slash so paths compare as text.
TMP=${TMP//\/\//\/}
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
printf '# Uvod\n' >"$PROJ/uvod.md"
printf '# Poglavlje\n\nčćšžđ ČĆŠŽĐ\n' >"$PROJ/mapa/poglavlje.md"
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
PISAR_HOME="$TMP/home" PISAR_LOG=debug "$BIN" "$PROJ/mapa/poglavlje.md" >"$LOG" 2>&1 &
APP_PID=$!

frontend_loaded() {
  is_running "$APP_PID" || return 1
  [[ "$(tree_calls "$LOG" 3)" -ge 1 ]]
}
if wait_for 20 frontend_loaded; then
  ok "frontend started and asked for the tree: 3 top-level entries (hidden file and picture left out)"
else
  fail "frontend started and asked for the tree with 3 top-level entries within 20 s"
  sed 's/^/     log: /' "$LOG" | tail -n 20
fi
check "frontend asked for the project list" grep -q 'method=Projects projects=1$' "$LOG"

# A change made from outside must reach the tree without any action in the app.
printf '# Novo\n' >"$PROJ/Novo.md"
refreshed() { [[ "$(tree_calls "$LOG" 4)" -ge 1 ]]; }
if wait_for 10 refreshed; then
  ok "a document created from outside refreshed the tree (4 top-level entries)"
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

# ---- the editor -------------------------------------------------------------

# The frontend mounted the terminal and started Neovim with the size it measured;
# Go then opened the document named on the command line.
editor_up() {
  is_running "$APP_PID" || return 1
  logged 'method=Start cols=[0-9]+ rows=[0-9]+' &&
    logged 'event=document:active project=smoke-project rel=mapa/poglavlje\.md'
}
SOCK=""
NVIM_PIDS=""
if wait_for 20 editor_up; then
  ok "editor started and the document from the command line is active (project and path resolved)"
  COLS=$(sed -nE 's/.*method=Start cols=([0-9]+) rows=([0-9]+).*/\1/p' "$LOG" | head -n 1)
  ROWS=$(sed -nE 's/.*method=Start cols=([0-9]+) rows=([0-9]+).*/\2/p' "$LOG" | head -n 1)
  check "terminal grid is at least 100x30 (got ${COLS}x${ROWS})" test "$COLS" -ge 100 -a "$ROWS" -ge 30
  for pid in $(descendants "$APP_PID"); do
    cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
    if [[ "$cmd" =~ --listen\ ([^\ ]+) ]]; then
      SOCK=${BASH_REMATCH[1]}
      break
    fi
  done
else
  fail "editor started and opened the document from the command line within 20 s"
  grep -E 'method=Start|editor' "$LOG" | sed 's/^/     log: /' | tail -n 10
fi

if [[ -z "$SOCK" ]]; then
  fail "found the socket of this application's Neovim"
else
  ok "found the socket of this application's Neovim"
  NVIM_PIDS=$(pgrep -f -- "$SOCK" | tr '\n' ' ')

  got=$(remote "$SOCK" --remote-expr 'printf("%d %d %s %s", &columns, &lines, fnamemodify(bufname(""), ":t"), get(g:, "colors_name", ""))') || got=""
  echo "     neovim says: '$got'"
  check "Neovim has the size the frontend sent, the document, and the colorscheme pisar" \
    test "$got" = "$COLS $ROWS poglavlje.md pisar"

  # The colours of Normal must be the tokens text and bg.editor; read them from
  # the token file instead of repeating them here.
  token() { sed -nE "s/^ *'?$1'?: '#([0-9A-Fa-f]{6})',.*/\1/p" "$ROOT/frontend/src/theme/tokens.ts" | head -n 1 | tr 'a-f' 'A-F'; }
  want="$(token text) $(token 'bg\.editor')"
  got=$(remote "$SOCK" --remote-expr 'printf("%06X %06X", nvim_get_hl(0, {"name": "Normal"}).fg, nvim_get_hl(0, {"name": "Normal"}).bg)') || got=""
  check "Normal uses the tokens text and bg.editor ($got)" test -n "$want" -a "$got" = "$want"

  got=$(remote "$SOCK" --remote-expr 'stdpath("state") . " " . stdpath("config")') || got=""
  case "$got" in
    *"$TMP/home/nvim/"*" "*"$TMP/home/nvim/"*) ok "Neovim keeps its state and configuration under the application's home" ;;
    *) fail "Neovim keeps its state and configuration under the application's home ($got)" ;;
  esac

  # A buffer change made inside Neovim reaches the application.
  remote "$SOCK" --remote-send "<Cmd>edit $PROJ/uvod.md<CR>" >/dev/null || true
  switched() { logged 'event=document:active project=smoke-project rel=uvod\.md'; }
  check "changing the buffer in Neovim reports the new active document" wait_for 10 switched

  # So does the modified state, in both directions.
  remote "$SOCK" --remote-send 'Ohello<Esc>' >/dev/null || true
  dirty() { logged 'event=document:state path=.*uvod\.md"? modified=true'; }
  check "typing reports the document as modified" wait_for 10 dirty
  check "the file on disk is not written before a save" test "$(cat "$PROJ/uvod.md")" = "# Uvod"
  remote "$SOCK" --remote-send '<Cmd>write<CR>' >/dev/null || true
  clean() { [[ "$(grep -E 'event=document:state path=.*uvod\.md"? modified=' "$LOG" | tail -n 1)" == *modified=false ]]; }
  check "saving reports the document as saved" wait_for 10 clean
  check "the saved text is on disk" grep -q '^hello$' "$PROJ/uvod.md"

  # <leader>e in Neovim asks for the tree and changes nothing else.
  remote "$SOCK" --remote-send '<Space>e' >/dev/null || true
  focus_asked() { logged 'event=ui:focus-tree'; }
  check "<Space>e in Neovim asks the application to focus the tree" wait_for 10 focus_asked
  got=$(remote "$SOCK" --remote-expr 'mode() . " " . &modified') || got=""
  check "Neovim stays in normal mode with the buffer unchanged ($got)" test "$got" = "n 0"
fi

kill -TERM "$APP_PID"
app_gone() {
  local p
  for p in $APP_PID $NVIM_PIDS; do
    if is_running "$p"; then return 1; fi
  done
}
if wait_for 10 app_gone; then
  ok "application and its Neovim exited within 10 s of SIGTERM"
else
  fail "application and its Neovim exited within 10 s of SIGTERM"
  for p in $NVIM_PIDS; do
    if is_running "$p"; then kill -KILL "$p" 2>/dev/null || true; fi
  done
fi
wait "$APP_PID" 2>/dev/null || true
APP_PID=""
if [[ -n "$SOCK" ]]; then
  check "the RPC socket and its folder are gone" test ! -e "$SOCK" -a ! -e "$(dirname "$SOCK")"
fi

check "config.json is unchanged" cmp -s "$CONFIG" "$TMP/config.before"
check "the temporary home holds only config.json and Neovim's own folder" \
  test "$(ls -A "$TMP/home" | sort | tr '\n' ' ')" = "config.json nvim "
check "init.lua and the colorscheme were written under the home" \
  test -s "$TMP/home/nvim/config/pisar/init.lua" -a -s "$TMP/home/nvim/config/pisar/colors/pisar.lua"
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
