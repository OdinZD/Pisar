#!/usr/bin/env bash
# Smoke test for Pisar: starts the real application (a GUI window) with its
# home in a temporary folder, a seeded project and a document named on the
# command line. It proves from the debug log and from Neovim's own RPC socket
# that the frontend loaded the tree and started the editor, that Neovim shows
# the document with the generated colorscheme, that buffer and state changes
# reach the application, that a file created from outside refreshes the tree,
# and that quitting leaves nothing behind. A last run proves the chain behind
# <Space>p (preview) and <Space>f (search) up to the page that comes back.
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

# ---- Word documents ---------------------------------------------------------
#
# The built application, started once per document with a home of its own, and
# a copy of a test document in a "user's folder" in the temporary directory
# (testdata/ is never written to). Neovim is driven through its socket only.
# Waits are on real conditions (the debug log says the export finished, the
# file changed), each with a limit, never a blind sleep.

skip() { printf 'skip %s\n' "$1"; }

# only_in <dir> <name>...: the folder holds exactly these entries, hidden ones included.
only_in() {
  local dir=$1 got want
  shift
  got=$(ls -A "$dir" | sort | tr '\n' ' ')
  want=$(printf '%s\n' "$@" | sort | tr '\n' ' ')
  [[ "$got" == "$want" ]]
}

wlogged() { grep -Eq "$1" "$WLOG" 2>/dev/null; }

# nv <expression>: evaluated in this application's Neovim.
nv() { remote "$WSOCK" --remote-expr "$1"; }

# word_start <label> <document>: a fresh home and a fresh "user's folder" that
# holds a copy of the fixture (and a second copy to compare against).
word_start() {
  local label=$1 doc=$2
  WBASE="$TMP/word-$label"
  WHOME="$WBASE/home"
  WUSER="$WBASE/user"
  WLOG="$WBASE/app.log"
  mkdir -p "$WHOME" "$WUSER" "$WBASE/orig"
  printf '{\n  "version": 1,\n  "projects": []\n}\n' >"$WHOME/config.json"
  cp "$WORD_FIXTURES/$doc" "$WUSER/$doc"
  cp "$WORD_FIXTURES/$doc" "$WBASE/orig/$doc"
}

# word_launch <document>: starts the application with the document on the
# command line. Sets APP_PID, WSOCK and NVIM_PIDS.
word_launch() {
  local doc=$1
  PISAR_HOME="$WHOME" PISAR_LOG=debug "$BIN" "$WUSER/$doc" >"$WLOG" 2>&1 &
  APP_PID=$!
  WSOCK=""
  NVIM_PIDS=""
}

word_up() {
  local pid cmd
  is_running "$APP_PID" || return 1
  if [[ -z "$WSOCK" ]]; then
    for pid in $(descendants "$APP_PID"); do
      cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
      if [[ "$cmd" =~ --listen\ ([^\ ]+) ]]; then
        WSOCK=${BASH_REMATCH[1]}
        break
      fi
    done
  fi
  [[ -n "$WSOCK" ]] || return 1
  [[ "$(nv 'fnamemodify(bufname(""), ":t")' 2>/dev/null)" == "${WANT_BUF:-doc.md}" ]]
}

# word_open <label> <document>: everything above; returns 1 (after reporting)
# when the application does not get to the point of showing doc.md.
word_open() {
  local label=$1 doc=$2
  word_launch "$doc"
  if wait_for 40 word_up; then
    NVIM_PIDS=$(pgrep -f -- "$WSOCK" | tr '\n' ' ')
    return 0
  fi
  fail "$label: the application showed doc.md in Neovim within 40 s"
  grep -Ev 'event=pty:data|method=Write' "$WLOG" | tail -n 10 | sed 's/^/     log: /'
  word_stop "$label" || true
  return 1
}

# word_stop <label>: ends the application through its own pid and checks that
# nothing of it is left.
word_stop() {
  local label=$1
  [[ -n "$APP_PID" ]] || return 0
  kill -TERM "$APP_PID"
  if wait_for 10 app_gone; then
    ok "$label: the application and its Neovim exited within 10 s of SIGTERM"
  else
    fail "$label: the application and its Neovim exited within 10 s of SIGTERM"
    local p
    for p in $NVIM_PIDS; do
      if is_running "$p"; then kill -KILL "$p" 2>/dev/null || true; fi
    done
  fi
  wait "$APP_PID" 2>/dev/null || true
  APP_PID=""
  if [[ -n "$WSOCK" ]]; then
    check "$label: the RPC socket and its folder are gone" test ! -e "$WSOCK" -a ! -e "$(dirname "$WSOCK")"
  fi
  check "$label: no working copy of a saved document is left in work/" test -z "$(ls -A "$WHOME/work" 2>/dev/null)"
}

# work_layout [pictures]: the buffer is doc.md in a working folder of this
# application's home, and the folder holds what the conversion makes (and, when
# a number is given, that many pictures in media/).
work_layout() {
  local buf dir
  buf=$(nv 'expand("%:p")') || return 1
  dir=$(dirname "$buf")
  [[ "$(basename "$buf")" == doc.md ]] || return 1
  # Compared as real paths: the temporary folder sits behind /var -> /private/var.
  [[ "$(cd -P "$(dirname "$dir")" && pwd)" == "$(cd -P "$WHOME/work" && pwd)" ]] || return 1
  [[ -s "$dir/doc.md" && -s "$dir/reference.docx" && -s "$dir/meta.json" ]] || return 1
  [[ -z "${1:-}" || "$(find "$dir/media" -type f 2>/dev/null | wc -l | tr -d ' ')" == "$1" ]]
}

# add_line <text>: appends a paragraph to the buffer through the socket. The
# blank line matters: the fixtures end with a footnote definition, and a line
# right after it would be taken for a continuation of the footnote.
add_line() { nv "nvim_buf_set_lines(0, -1, -1, v:false, ['', '$1'])" >/dev/null; }
write_buffer() { remote "$WSOCK" --remote-send '<Cmd>write<CR>' >/dev/null; }

# exports <file>: how many times the log says <file> was exported.
exports() { grep -Ec "event=document:export file=$1 phase=saved" "$WLOG" 2>/dev/null || true; }

# exported <file> <reference> <count>: the application exported <file> that
# many times and the file differs from <reference>.
exported() { [[ "$(exports "$1")" -ge "$3" ]] && ! cmp -s "$2" "$WUSER/$1"; }

# docx_has <file> <text>: <file> is a valid archive whose document.xml has <text>.
docx_has() {
  unzip -p "$1" word/document.xml >"$WBASE/document.xml" 2>/dev/null && grep -Fq -- "$2" "$WBASE/document.xml"
}
docx_valid() { unzip -tq "$1" >/dev/null 2>&1; }

# media_of <docx>: the image entries, one per line, in a file.
media_of() {
  unzip -Z1 "$1" >"$WBASE/entries.txt"
  grep '^word/media/' "$WBASE/entries.txt" || true
}

# same_media <docx> <docx>: each has exactly one image and the bytes agree.
same_media() {
  local a b
  a=$(media_of "$1")
  b=$(media_of "$2")
  [[ -n "$a" && -n "$b" && "$(wc -l <<<"$a" | tr -d ' ')" == 1 && "$(wc -l <<<"$b" | tr -d ' ')" == 1 ]] || return 1
  unzip -p "$1" "$a" >"$WBASE/media.a"
  unzip -p "$2" "$b" >"$WBASE/media.b"
  [[ -s "$WBASE/media.a" ]] && cmp -s "$WBASE/media.a" "$WBASE/media.b"
}

readonly_now() { [[ "$(nv '&modifiable . " " . &readonly' 2>/dev/null)" == "0 1" ]]; }

backups() { find "$WHOME/backups" -type f 2>/dev/null; }

# one_backup_of <file>: exactly one backup exists and it is a copy of <file>.
one_backup_of() {
  local list
  list=$(backups)
  [[ -n "$list" && "$(wc -l <<<"$list" | tr -d ' ')" == 1 ]] && cmp -s "$list" "$1"
}
no_backup() { [[ -z "$(backups)" ]]; }

# kept <name>: the file in the user's folder is still byte for byte the original.
kept() { cmp -s "$WUSER/$1" "$WBASE/orig/$1"; }

if ! command -v pandoc >/dev/null 2>&1; then
  skip "Word documents (basic.docx, comments.docx, legacy.doc): pandoc is not installed"
else
  WORD_FIXTURES="$ROOT/testdata/docx"
  echo "--- Word documents"

  # A. basic.docx: opens as a working copy and is exported safely.
  L="basic.docx"
  word_start A basic.docx
  chmod 0640 "$WUSER/basic.docx"
  xattr -w com.pisar.smoke "smoke-value" "$WUSER/basic.docx"
  if word_open "$L" basic.docx; then
    check "$L: Neovim shows doc.md in the working folder (doc.md, reference.docx, meta.json, one picture in media/)" work_layout 1
    check "$L: the .docx is unchanged after opening" kept basic.docx
    check "$L: nothing else is beside the .docx" only_in "$WUSER" basic.docx

    T1="Smoke prvi zapis: čćšžđ ČĆŠŽĐ $$"
    add_line "$T1"
    write_buffer
    check "$L: after :w the application exported and the .docx changed (within 30 s)" \
      wait_for 30 exported basic.docx "$WBASE/orig/basic.docx" 1
    check "$L: the .docx is a valid archive" docx_valid "$WUSER/basic.docx"
    check "$L: word/document.xml has the new text" docx_has "$WUSER/basic.docx" "$T1"
    check "$L: one picture in word/media/, byte for byte the one of the original" \
      same_media "$WUSER/basic.docx" "$WBASE/orig/basic.docx"
    check "$L: exactly one backup, identical to the original" one_backup_of "$WBASE/orig/basic.docx"
    check "$L: still nothing else beside the .docx (hidden files included)" only_in "$WUSER" basic.docx
    check "$L: the permissions are unchanged (640)" test "$(stat -f %Lp "$WUSER/basic.docx")" = 640
    check "$L: the extended attribute set before the start survived the save" \
      test "$(xattr -p com.pisar.smoke "$WUSER/basic.docx" 2>/dev/null)" = "smoke-value"

    cp "$WUSER/basic.docx" "$WBASE/after-first.docx"
    T2="Smoke drugi zapis: žđšćč $$"
    add_line "$T2"
    write_buffer
    check "$L: the second :w exported again and the .docx changed (within 30 s)" \
      wait_for 30 exported basic.docx "$WBASE/after-first.docx" 2
    check "$L: the .docx is valid and has the second text and the first" \
      sh -c 'unzip -tq "$1" >/dev/null && unzip -p "$1" word/document.xml >"$2" && grep -Fq -- "$3" "$2" && grep -Fq -- "$4" "$2"' _ \
      "$WUSER/basic.docx" "$WBASE/document.xml" "$T1" "$T2"
    check "$L: still exactly one backup, still the original" one_backup_of "$WBASE/orig/basic.docx"
    check "$L: still nothing else beside the .docx" only_in "$WUSER" basic.docx
    word_stop "$L"
  fi

  # B. comments.docx: read-only; nothing may reach the file.
  L="comments.docx"
  word_start B comments.docx
  if word_open "$L" comments.docx; then
    check "$L: Neovim shows doc.md in the working folder" work_layout
    check "$L: the buffer is not modifiable and is readonly (within 20 s)" wait_for 20 readonly_now
    before=$(nv 'sha256(join(getline(1, "$"), "\n"))' || true)
    nv "nvim_buf_set_lines(0, -1, -1, v:false, ['Smoke izmjena čćšžđ'])" >/dev/null 2>&1 || true
    nv 'execute("silent! normal! Ohello")' >/dev/null 2>&1 || true
    after=$(nv 'sha256(join(getline(1, "$"), "\n"))' || true)
    check "$L: changes through the socket leave the buffer as it was" test -n "$before" -a "$before" = "$after"
    # :w! overrides 'readonly', so Neovim does write the working copy and the
    # application hears about it ("saved"); it must not turn that into an
    # export. It decides that before anything is queued, so once the write is
    # in the log there is nothing left that could still reach the file.
    remote "$WSOCK" --remote-send '<Cmd>silent! write!<CR>' >/dev/null || true
    heard() { wlogged 'event=saved path=.*/doc\.md'; }
    check "$L: the application heard the :w! write (within 20 s)" wait_for 20 heard
    # A round trip to Neovim: whatever it was going to do has been done.
    nv '1' >/dev/null || true
    check "$L: the application started no export (no conversion in the log)" \
      sh -c '! grep -Eq "event=document:export .*phase=(converting|saved)" "$1"' _ "$WLOG"
    check "$L: the .docx is unchanged after :w!" kept comments.docx
    check "$L: no backup was made" no_backup
    check "$L: nothing else is beside the .docx" only_in "$WUSER" comments.docx
    word_stop "$L"
  fi

  # C. legacy.doc: never written; the first save makes a new .docx beside it.
  L="legacy.doc"
  word_start C legacy.doc
  if word_open "$L" legacy.doc; then
    check "$L: Neovim shows doc.md in the working folder" work_layout
    check "$L: the .doc is unchanged after opening" kept legacy.doc
    check "$L: nothing else is beside the .doc" only_in "$WUSER" legacy.doc

    T3="Smoke zapis iz doca: čćšžđ $$"
    add_line "$T3"
    write_buffer
    new_docx() { [[ "$(exports legacy.docx)" -ge 1 && -s "$WUSER/legacy.docx" ]]; }
    check "$L: after :w a legacy.docx appeared beside the .doc (within 30 s)" wait_for 30 new_docx
    check "$L: legacy.docx is a valid archive with the new text" \
      sh -c 'unzip -tq "$1" >/dev/null && unzip -p "$1" word/document.xml >"$2" && grep -Fq -- "$3" "$2"' _ \
      "$WUSER/legacy.docx" "$WBASE/document.xml" "$T3"
    check "$L: the .doc is still byte for byte the original" kept legacy.doc
    check "$L: only legacy.doc and legacy.docx are in the folder" only_in "$WUSER" legacy.doc legacy.docx
    word_stop "$L"
  fi
fi

# ---- preview and document search ----------------------------------------------
#
# The same application, started once more with a home and a "user's folder" of
# its own. Everything goes through Neovim's socket: <Space>p and <Space>f are
# sent as keys to Neovim (which proves the mappings), and the rest of the chain
# is read from the debug log: Neovim -> Go (ui:toggle-preview) -> the real
# frontend in the webview (it calls SetPreview) -> pandoc -> preview:content.
# The log holds the size of the HTML and the kind of result, never the page.
# Typing into the search field of the webview would need keys sent to the
# application itself, which this script never does; the search is covered by
# the Go and frontend tests, here only its trigger is proved.

# first_line <pattern>: the number of the first log line that matches, or nothing.
first_line() { grep -En "$1" "$WLOG" 2>/dev/null | head -n 1 | cut -d: -f1 || true; }

# nth <pattern>: how many log lines match.
nth() { grep -Ec "$1" "$WLOG" 2>/dev/null || true; }

# A result of the preview (any state) and one with a page, for pregled.md.
PC_ANY='event=preview:content '
PC_HTML='event=preview:content file=pregled\.md state=html code="?"? html_bytes=[1-9][0-9]*$'
pc_any() { nth "$PC_ANY"; }
html_more_than() { [[ "$(nth "$PC_HTML")" -gt "$1" ]]; }
logged_times() { [[ "$(nth "$1")" -ge "$2" ]]; }

# unchanged_for <seconds> <command...>: the command's output stays the same for
# that long. A "nothing happens" claim has no event to wait for, so time is the
# only possible condition; the debounce is 300 ms and this watches for a
# multiple of it, in steps of 0.1 s so that a late event is seen at once.
unchanged_for() {
  local limit=$(($1 * 10)) n=0 first now
  shift
  first=$("$@")
  while [[ $n -lt $limit ]]; do
    sleep 0.1
    now=$("$@")
    [[ "$now" == "$first" ]] || return 1
    n=$((n + 1))
  done
}

buffer_hash() { nv 'sha256(join(getline(1, "$"), "\n"))'; }

if [[ ! -s "$ROOT/testdata/docx/src/pixel.png" ]]; then
  fail "testdata/docx/src/pixel.png exists"
else
  echo "--- preview and search"
  HAVE_PANDOC=1
  command -v pandoc >/dev/null 2>&1 || HAVE_PANDOC=0

  L="preview"
  WBASE="$TMP/preview"
  WHOME="$WBASE/home"
  WUSER="$WBASE/user"
  WLOG="$WBASE/app.log"
  mkdir -p "$WHOME" "$WUSER"
  printf '{\n  "version": 1,\n  "projects": []\n}\n' >"$WHOME/config.json"
  cp "$ROOT/testdata/docx/src/pixel.png" "$WUSER/pixel.png"
  cat >"$WUSER/pregled.md" <<'MD'
# Naslov pregleda

Odlomak s dijakriticima: čćšžđ ČĆŠŽĐ.

<script>alert(1)</script>

![lokalna slika](pixel.png)

![udaljena slika](http://127.0.0.1:9/x.png)
MD
  printf 'Obični tekst bez pregleda.\n' >"$WUSER/note.txt"

  WANT_BUF=pregled.md
  word_launch pregled.md
  if ! wait_for 40 word_up; then
    fail "$L: the application showed pregled.md in Neovim within 40 s"
    grep -Ev 'event=pty:data|method=Write' "$WLOG" | tail -n 10 | sed 's/^/     log: /'
    NVIM_PIDS=$(pgrep -f -- "${WSOCK:-no-socket}" | tr '\n' ' ')
    word_stop "$L" || true
  else
    NVIM_PIDS=$(pgrep -f -- "$WSOCK" | tr '\n' ' ')

    # A1. The frontend reports the closed pane when it starts; nothing is converted.
    off_reported() { wlogged 'method=SetPreview enabled=false'; }
    check "$L: the frontend told the service the preview is off at start (SetPreview enabled=false)" wait_for 20 off_reported
    nv '1' >/dev/null || true
    check "$L: before any key there is no preview:content" test "$(pc_any)" = 0
    h0=$(buffer_hash || true)

    # A2. <Space>p in Neovim: the application hears it, the frontend answers
    # with SetPreview(true), and a page comes back.
    remote "$WSOCK" --remote-send '<Space>p' >/dev/null || true
    toggled() { wlogged 'event=ui:toggle-preview'; }
    check "$L: <Space>p in Neovim gives ui:toggle-preview" wait_for 10 toggled
    asked_on() { wlogged 'method=SetPreview enabled=true'; }
    check "$L: the frontend reacted to it with SetPreview enabled=true" wait_for 10 asked_on
    if [[ "$HAVE_PANDOC" == 1 ]]; then
      check "$L: preview:content with state=html and a page (html_bytes > 0) for pregled.md" wait_for 20 html_more_than 0
      # The page has a script line and a picture from a closed port: it must be converted, not refused.
      check "$L: the document with a script line and a remote picture converted (no error, no failure warning)" \
        sh -c '! grep -Eq "event=preview:content .*state=error|preview failed" "$1"' _ "$WLOG"
      check "$L: the order was ui:toggle-preview, SetPreview enabled=true, preview:content" \
        sh -c '[ -n "$1" ] && [ -n "$2" ] && [ -n "$3" ] && [ "$1" -lt "$2" ] && [ "$2" -lt "$3" ]' _ \
        "$(first_line 'event=ui:toggle-preview')" "$(first_line 'method=SetPreview enabled=true')" \
        "$(first_line 'event=preview:content file=pregled\.md state=html ')"
    else
      skip "$L: preview:content with state=html (pandoc is not installed)"
      check "$L: the order was ui:toggle-preview, then SetPreview enabled=true" \
        sh -c '[ -n "$1" ] && [ -n "$2" ] && [ "$1" -lt "$2" ]' _ \
        "$(first_line 'event=ui:toggle-preview')" "$(first_line 'method=SetPreview enabled=true')"
    fi
    check "$L: <Space>p wrote nothing into the buffer" test -n "$h0" -a "$(buffer_hash || true)" = "$h0"

    # A3. A change of the buffer gives a new page.
    if [[ "$HAVE_PANDOC" == 1 ]]; then
      n_html=$(nth "$PC_HTML")
      U1="Jedinstveni redak pregleda 4711"
      add_line "$U1"
      check "$L: a change of the buffer gives a new preview:content with state=html (after $n_html)" wait_for 20 html_more_than "$n_html"
    else
      skip "$L: a change of the buffer gives a new preview:content (pandoc is not installed)"
    fi
    check "$L: the text of the document is not in the log" sh -c '! grep -Fq -- "$1" "$2"' _ "Jedinstveni redak" "$WLOG"

    # A5. The second <Space>p switches it off; the next change gives nothing.
    remote "$WSOCK" --remote-send '<Space>p' >/dev/null || true
    toggled2() { logged_times 'event=ui:toggle-preview' 2 && [[ "$(nth 'method=SetPreview enabled=false')" -ge 2 ]]; }
    check "$L: the second <Space>p gave ui:toggle-preview and SetPreview enabled=false" wait_for 10 toggled2
    nv '1' >/dev/null || true
    n_all=$(pc_any)
    n_changed=$(nth 'event=changed')
    add_line "Drugi jedinstveni redak 4712"
    nv '1' >/dev/null || true
    # Time is the only condition for "nothing arrives": see unchanged_for.
    check "$L: after a change with the preview off there is no new preview:content (1 s watched, $n_all so far)" \
      unchanged_for 1 pc_any
    check "$L: ... and Neovim did not even notify the application of the change" test "$(nth 'event=changed')" = "$n_changed"
    check "$L: ... and the count is still $n_all" test "$(pc_any)" = "$n_all"

    # A6. A .txt document has no preview: with the pane on, the pane is told so.
    remote "$WSOCK" --remote-send '<Cmd>silent write<CR>' >/dev/null || true
    saved_md() { [[ "$(grep -E 'event=document:state path=.*pregled\.md"? modified=' "$WLOG" | tail -n 1)" == *modified=false ]]; }
    check "$L: the Markdown buffer was saved (so that leaving it asks nothing)" wait_for 10 saved_md
    remote "$WSOCK" --remote-send "<Cmd>edit $WUSER/note.txt<CR>" >/dev/null || true
    on_txt() { [[ "$(nv 'fnamemodify(bufname(""), ":t")' 2>/dev/null)" == note.txt ]]; }
    check "$L: Neovim shows note.txt" wait_for 10 on_txt
    remote "$WSOCK" --remote-send '<Space>p' >/dev/null || true
    unsupported() { wlogged 'event=preview:content file=note\.txt state=unsupported'; }
    check "$L: with the preview on again, note.txt gives preview:content with state=unsupported" wait_for 20 unsupported
    check "$L: the third <Space>p gave ui:toggle-preview and SetPreview enabled=true again" \
      sh -c '[ "$1" -ge 3 ] && [ "$2" -ge 2 ]' _ "$(nth 'event=ui:toggle-preview')" "$(nth 'method=SetPreview enabled=true')"

    # B7, B8. <Space>f: the application is asked to open the search, and neither it nor Neovim is hurt.
    h1=$(buffer_hash || true)
    check "$L: no ui:open-search before <Space>f" test "$(nth 'event=ui:open-search')" = 0
    remote "$WSOCK" --remote-send '<Space>f' >/dev/null || true
    search_asked() { wlogged 'event=ui:open-search'; }
    check "$L: <Space>f in Neovim gives ui:open-search" wait_for 10 search_asked
    got=$(nv 'mode() . " " . &modified') || got=""
    check "$L: Neovim still answers, in normal mode, buffer unchanged ($got)" test "$got" = "n 0"
    check "$L: <Space>f wrote nothing into the buffer" test -n "$h1" -a "$(buffer_hash || true)" = "$h1"
    check "$L: the markdown on disk is the text the buffer had when it was saved" grep -Fq -- "Drugi jedinstveni redak 4712" "$WUSER/pregled.md"

    # C. Quit through the pid; nothing of the run is left.
    word_stop "$L"
    check "$L: no process of this run is left (application, Neovim, pandoc)" test -z "$(pgrep -f -- "$WHOME" || true)"
    if [[ "$HAVE_PANDOC" == 1 ]]; then
      check "$L: preview/ in the home holds only the filter" only_in "$WHOME/preview" preview-filter.lua
    fi
  fi
  WANT_BUF=""
fi

check "config.json is unchanged" cmp -s "$CONFIG" "$TMP/config.before"
check "the temporary home holds only config.json and Neovim's own folder" \
  test "$(ls -A "$TMP/home" | sort | tr '\n' ' ')" = "config.json nvim spell "
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
