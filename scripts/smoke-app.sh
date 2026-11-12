#!/usr/bin/env bash
# Smoke test for the packaged application bin/Pisar.app. Unlike smoke.sh, which
# runs bin/pisar with the environment of the terminal, this starts the bundle
# through `open`, the way Finder does, in an environment with only the system
# directories in PATH, no locale variables, and a working directory outside the
# repository. Run A (tools available) proves that the application finds nvim and
# pandoc by itself, shows a .docx converted by pandoc in Neovim, and quits
# cleanly. Run B (PISAR_TOOL_DIRS empty) proves that without the tools it still
# runs and reports both as missing.
#   task package && bash scripts/smoke-app.sh
#   SIGN_IDENTITY="<identity>" task sign && bash scripts/smoke-app.sh
#
# The bundle is not built here. No keys or clicks are sent: Neovim is driven
# through its socket only. Everything temporary lives in one mktemp directory;
# the script only signals the application it started. The user's real
# configuration (~/Library/Application Support/Pisar) is never touched.
#
# What was measured about `open` on this macOS (it decides the design below):
#   - LaunchServices hands the application the environment of the process that
#     ran `open`, so the terminal's PATH would leak into the app. The script
#     therefore starts `open` itself under `env -i` with a Finder-like minimum.
#   - `--env NAME=` reaches the application as set-but-empty (Tools reports both
#     tools missing when the bare PATH is combined with it); `--env NAME` without
#     a value was not seen as set by Neovim, so the `=` form is used.
#   - `--stderr PATH` makes the application's stderr (the slog debug log) land
#     in that file.
#   - `open` does not print or return the pid. The script lists the pids of the
#     bundle's executable before and after and takes the new one.
#
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

ROOT=$(cd -P "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP="$ROOT/bin/Pisar.app"
BIN="$APP/Contents/MacOS/pisar"
REAL_HOME_DIR="$HOME/Library/Application Support/Pisar"

# The environment the application gets: what Finder's launchd gives an
# application, as far as it matters here.
FINDER_PATH=/usr/bin:/bin:/usr/sbin:/sbin
EXTRA_PATH=/opt/homebrew/bin:/usr/local/bin

TMP=""
APP_PID=""
NVIM_PIDS=""
SOCK=""
LOG=""
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
  # An empty socket would make this an ordinary Neovim session. The client also
  # keeps its own state (log, shada) inside the temporary folder, never in the
  # user's ~/.local/state/nvim.
  [[ -n "$1" ]] || return 1
  env XDG_CONFIG_HOME="$TMP/nvim-client/config" XDG_DATA_HOME="$TMP/nvim-client/data" \
    XDG_STATE_HOME="$TMP/nvim-client/state" XDG_CACHE_HOME="$TMP/nvim-client/cache" \
    nvim -i NONE --server "$1" "$2" "$3" >"$out" 2>"$TMP/remote.err" &
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

# nv <expression>: evaluated in this application's Neovim.
nv() { remote "$SOCK" --remote-expr "$1"; }

# logged <pattern>: the debug log has a line matching the extended regex.
logged() { grep -Eq "$1" "$LOG" 2>/dev/null; }

wait_for() {
  local limit=$(($1 * 10)) n=0
  shift
  while ! "$@"; do
    n=$((n + 1))
    if [[ $n -ge $limit ]]; then return 1; fi
    sleep 0.1
  done
}

# Nothing started through `open` is our child, so there is no `wait`: the end
# of the application is seen through ps.
cleanup() {
  local status=$?
  trap - EXIT
  local p
  for p in $APP_PID $NVIM_PIDS; do
    if is_running "$p"; then kill -TERM "$p" 2>/dev/null || true; fi
  done
  sleep 0.5
  for p in $APP_PID $NVIM_PIDS; do
    if is_running "$p"; then kill -KILL "$p" 2>/dev/null || true; fi
  done
  if [[ -n "$TMP" && "$TMP" == */pisar-smoke-app.* ]]; then rm -rf "$TMP"; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# bundle_pids: pids whose command is exactly the executable of this bundle
# (nothing else that merely mentions the path, and no Neovim).
bundle_pids() {
  ps -axo pid=,command= | awk -v b="$BIN" '{
    pid = $1; sub(/^ *[0-9]+ +/, "")
    if ($0 == b || index($0, b " ") == 1) print pid
  }' | sort -n
}

# launch <label> <document> <empty-tool-dirs 0|1>: starts the bundle through
# `open -n` with the Finder-like environment, from a working directory outside
# the repository. Sets APP_PID and LOG; resets SOCK and NVIM_PIDS.
launch() {
  local label=$1 doc=$2 empty_dirs=$3 before after n
  RUN="$TMP/run-$label"
  LOG="$RUN/app.log"
  mkdir -p "$RUN/home" "$RUN/cwd"
  : >"$LOG"
  SOCK=""
  NVIM_PIDS=""
  APP_PID=""
  before=$(bundle_pids | tr '\n' ' ')

  local extra=()
  if [[ "$empty_dirs" == 1 ]]; then extra=(--env PISAR_TOOL_DIRS=); fi
  # env -i: `open` passes its own environment on to the application (see the
  # header), so the terminal's variables must not reach it. TMPDIR is kept
  # because launchd gives it to every application and Go uses it for the socket.
  (
    cd "$RUN/cwd" &&
      env -i PATH="$FINDER_PATH" HOME="$HOME" USER="${USER:-$(id -un)}" LOGNAME="${USER:-$(id -un)}" \
        TMPDIR="${TMPDIR:-/tmp/}" \
        /usr/bin/open -n "$APP" \
        --env "PISAR_HOME=$RUN/home" --env PISAR_LOG=debug \
        ${extra[@]+"${extra[@]}"} \
        --stderr "$LOG" --args "$doc"
  ) || return 1

  # The new pid is the one that was not there before.
  for n in $(seq 1 100); do
    after=$(bundle_pids | tr '\n' ' ')
    APP_PID=$(comm -13 <(printf '%s\n' $before) <(printf '%s\n' $after) | head -n 1)
    [[ -n "$APP_PID" ]] && return 0
    sleep 0.1
  done
  return 1
}

# find_socket: the --listen socket of this application's Neovim.
find_socket() {
  local pid cmd
  is_running "$APP_PID" || return 1
  for pid in $(descendants "$APP_PID"); do
    cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
    if [[ "$cmd" =~ --listen\ ([^\ ]+) ]]; then
      SOCK=${BASH_REMATCH[1]}
      return 0
    fi
  done
  return 1
}

app_gone() {
  local p
  for p in $APP_PID $NVIM_PIDS; do
    if is_running "$p"; then return 1; fi
  done
}

# stop <label>: SIGTERM to the pid of this run, then the end of the application
# and of its Neovim is awaited.
stop() {
  local label=$1 p
  [[ -n "$APP_PID" ]] || return 0
  kill -TERM "$APP_PID" 2>/dev/null || true
  if wait_for 10 app_gone; then
    ok "$label: the application and its Neovim exited within 10 s of SIGTERM"
  else
    fail "$label: the application and its Neovim exited within 10 s of SIGTERM"
    for p in $APP_PID $NVIM_PIDS; do
      if is_running "$p"; then kill -KILL "$p" 2>/dev/null || true; fi
    done
  fi
  if [[ -n "$SOCK" ]]; then
    check "$label: the RPC socket and its folder are gone" test ! -e "$SOCK" -a ! -e "$(dirname "$SOCK")"
  fi
  APP_PID=""
}

if [[ ! -x "$BIN" ]]; then
  echo "smoke-app: $BIN not found: run \`task package\` (or \`task sign\`) first" >&2
  exit 1
fi
for tool in nvim pandoc; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "smoke-app: $tool is needed on the PATH of this script (to talk to the socket and to make the test .docx)" >&2
    exit 1
  fi
done

REAL_HOME_BEFORE=absent
if [[ -e "$REAL_HOME_DIR" ]]; then REAL_HOME_BEFORE=$(ls -la "$REAL_HOME_DIR" | shasum | cut -d' ' -f1); fi

TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-smoke-app.XXXXXX")
# $TMPDIR ends in a slash on macOS; drop the double slash so paths compare as text.
TMP=${TMP//\/\//\/}

echo "--- signature (information only)"
codesign -dv --verbose=2 "$APP" 2>&1 | grep -E 'Authority|Signature' || true

# A variable that exists only in this script's environment: if it shows up in
# the application's Neovim, the environment was the terminal's.
export PISAR_SMOKE_CANARY=terminal

# ---- run A: tools available -----------------------------------------------------

echo "--- run A: nvim and pandoc found from the bare environment"
PROJ="$TMP/Probni projekt"
mkdir -p "$PROJ"
DOCX="$PROJ/Probni članak.docx"
cat >"$TMP/source.md" <<'MD'
# Naslov dokumenta

Odlomak s dijakriticima: č ć š ž đ Č Ć Š Ž Đ.

Jedinstvena rečenica 4711 za dimnu provjeru.
MD
pandoc -f markdown -t docx -o "$DOCX" "$TMP/source.md"
cp "$DOCX" "$TMP/docx.before"

if ! launch A "$DOCX" 0; then
  fail "A: the bundle was started through open and its pid found"
  [[ -s "$LOG" ]] && sed 's/^/     log: /' "$LOG" | tail -n 10
else
  ok "A: the bundle was started through open (pid $APP_PID)"
  cmd=$(ps -o command= -p "$APP_PID" 2>/dev/null || true)
  check "A: the process is alive and its executable is the one in the bundle" \
    sh -c 'case "$1" in "$2"|"$2 "*) exit 0 ;; *) exit 1 ;; esac' _ "$cmd" "$BIN"

  # The log line the frontend's first EditorService.Tools call leaves behind.
  tools_a() { logged 'msg="editor: Tools" nvim=true pandoc=true$'; }
  if wait_for 30 tools_a; then
    ok "A: the log says Tools nvim=true pandoc=true"
  else
    fail "A: the log says Tools nvim=true pandoc=true within 30 s"
    grep -E 'Tools|level=(WARN|ERROR)' "$LOG" | sed 's/^/     log: /' | tail -n 5
  fi

  # The document is open in Neovim: the working copy of the .docx is doc.md.
  buffer_open() { find_socket && [[ "$(nv 'fnamemodify(bufname(""), ":t")' 2>/dev/null)" == doc.md ]]; }
  if wait_for 40 buffer_open; then
    ok "A: Neovim runs and answers on its socket; its buffer is doc.md, the working copy of the .docx"
    NVIM_PIDS=$(pgrep -f -- "$SOCK" | tr '\n' ' ')
    text=$(nv 'join(getline(1, "$"), "\n")' || true)
    missing=""
    for letter in č ć š ž đ Č Ć Š Ž Đ; do
      [[ "$text" == *"$letter"* ]] || missing="$missing $letter"
    done
    check "A: the buffer holds the text of the .docx with č ć š ž đ Č Ć Š Ž Đ (missing:${missing:- none})" test -z "$missing"
    check "A: the buffer holds the unique sentence of the document" \
      sh -c 'case "$1" in *"Jedinstvena rečenica 4711 za dimnu provjeru."*) exit 0 ;; *) exit 1 ;; esac' _ "$text"

    # The environment, as the application's own child sees it.
    got=$(nv 'getenv("PATH")' || true)
    check "A: PATH in Neovim is exactly the system directories plus $EXTRA_PATH ($got)" \
      test "$got" = "$FINDER_PATH:$EXTRA_PATH"
    leaked=""
    oldifs=$IFS
    IFS=:
    for entry in $PATH; do
      case ":$FINDER_PATH:$EXTRA_PATH:" in *":$entry:"*) ;; *) [[ ":$got:" != *":$entry:"* ]] || leaked="$leaked $entry" ;; esac
    done
    IFS=$oldifs
    check "A: no directory of the terminal's PATH is in the application's (leaked:${leaked:- none})" test -z "$leaked"
    check "A: a variable set only in the terminal's environment is not there either" \
      test "$(nv 'exists("$PISAR_SMOKE_CANARY")' || echo err)" = 0
    got=$(nv 'getenv("LC_CTYPE")' || true)
    check "A: LC_CTYPE in Neovim is en_US.UTF-8 ($got)" test "$got" = en_US.UTF-8
  else
    fail "A: Neovim runs and shows the document (doc.md) within 40 s"
    grep -Ev 'event=pty:data|method=Write' "$LOG" | tail -n 10 | sed 's/^/     log: /'
  fi

  cwd=$(lsof -a -p "$APP_PID" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)
  check "A: the working directory of the application is not the repository (${cwd:-unknown})" \
    sh -c '[ -n "$1" ] && [ "$1" != "$2" ] && case "$1" in "$2"/*) exit 1 ;; esac' _ "$cwd" "$ROOT"

  stop A
  check "A: no process of this run is left (application, Neovim)" test -z "$(pgrep -f -- "$RUN/home" || true)"
  check "A: the .docx is byte for byte what it was before the start" cmp -s "$DOCX" "$TMP/docx.before"
  check "A: nothing else is beside the .docx" test "$(ls -A "$PROJ")" = "Probni članak.docx"
fi

# ---- run B: no tools --------------------------------------------------------------

echo "--- run B: no tools on the PATH"
MD="$PROJ/Bilješke.md"
printf '# Bilješke\n\nčćšžđ\n' >"$MD"
cp "$MD" "$TMP/md.before"

if ! launch B "$MD" 1; then
  fail "B: the bundle was started through open and its pid found"
  [[ -s "$LOG" ]] && sed 's/^/     log: /' "$LOG" | tail -n 10
else
  ok "B: the bundle was started through open (pid $APP_PID)"
  tools_b() { logged 'msg="editor: Tools" nvim=false pandoc=false$'; }
  if wait_for 30 tools_b; then
    ok "B: the log says Tools nvim=false pandoc=false"
  else
    fail "B: the log says Tools nvim=false pandoc=false within 30 s"
    grep -E 'Tools|level=(WARN|ERROR)' "$LOG" | sed 's/^/     log: /' | tail -n 5
  fi
  # A crash would show a few seconds after the first calls, not at once.
  sleep 3
  check "B: the application is still alive 3 s later" is_running "$APP_PID"
  check "B: the application has no Neovim (no descendant processes)" test -z "$(descendants "$APP_PID" | tr -d '\n')"
  stop B
  check "B: no process of this run is left" test -z "$(pgrep -f -- "$RUN/home" || true)"
  check "B: the Markdown file is byte for byte what it was before the start" cmp -s "$MD" "$TMP/md.before"
fi

# ---- the real configuration ---------------------------------------------------------

REAL_HOME_AFTER=absent
if [[ -e "$REAL_HOME_DIR" ]]; then REAL_HOME_AFTER=$(ls -la "$REAL_HOME_DIR" | shasum | cut -d' ' -f1); fi
check "the real configuration folder was not created or changed ($REAL_HOME_BEFORE)" test "$REAL_HOME_BEFORE" = "$REAL_HOME_AFTER"

echo
if [[ "$FAILS" -eq 0 ]]; then
  echo "smoke-app: all checks passed"
else
  echo "smoke-app: $FAILS check(s) FAILED"
fi
[[ "$FAILS" -eq 0 ]] || exit 1
