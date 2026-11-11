#!/usr/bin/env bash
# Benchmark of the Pisar spike: holds Ctrl-d through a 5000-line file, once with
# the DOM renderer and once with WebGL, and prints one table. From anywhere:
#   bash spike/scripts/bench.sh          # build, measure, print the table
#   bash spike/scripts/bench.sh sample   # only write spike/bin/sample-5000.md
#
# The application drives Neovim itself (PISAR_SPIKE_BENCH, see spike/bench.go),
# writes a JSON result and quits. This script only starts it, waits, and checks
# that nothing is left behind. It signals only the application it started and
# the Neovim processes that were its descendants; it never matches by name.
#
# Written for the bash 3.2 that macOS ships (no mapfile, no associative arrays).
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BIN="$ROOT/spike/bin/spike"
SAMPLE="$ROOT/spike/bin/sample-5000.md"
LINES=5000
RUN_LIMIT=120 # seconds one run may take, the application's own limit is 90

TMP=""
APP_PID=""
FAILS=0

ok() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s\n' "$1"
  FAILS=$((FAILS + 1))
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

# stop_app: stops the application this script started, if it still runs.
stop_app() {
  [[ -n "$APP_PID" ]] || return 0
  if is_running "$APP_PID"; then
    local kids k i
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
  if [[ -n "$TMP" && "$TMP" == */pisar-bench.* ]]; then rm -rf "$TMP"; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# make_sample: writes $SAMPLE, exactly $LINES lines. Every line starts with its
# number, so no two are alike. Only single-width characters (the Croatian
# letters and „ ” — are single width in xterm.js and in Neovim), no tabs, at
# most 80 characters, and nothing that Markdown treats as syntax.
make_sample() {
  local dir tmpfile phrases
  dir=$(dirname "$SAMPLE")
  mkdir -p "$dir"
  tmpfile=$(mktemp "$dir/.sample.XXXXXX")
  phrases=$(mktemp "$dir/.phrases.XXXXXX")
  cat >"$phrases" <<'PHRASES'
Čačkalica, šećer, žaba, đak i ćup stoje na polici.
„Dobar dan”, rekao je pisar — i otišao na ručak.
Plavi kišobran čeka ispred ureda do kasno u noć.
Što je dulje: ćelija, đurđica ili šuma kraj ceste?
Pisar prepisuje tekst riječ po riječ, bez žurbe.
Zvončići zvone, a žuti ždral leti iznad polja.
Tipkovnica škljoca ravnomjerno, redak za retkom.
Neovim prikazuje ovaj redak jednako kao i terminal.
Navodnici „ovako” i crtica — ovdje su jednake širine.
Kratki redak.
Dulji redak s riječima koje popunjavaju gotovo cijelu širinu.
Ćiro i Đuro čitaju Šenoine retke u Žumberku.
PHRASES
  awk -v f="$phrases" -v total="$LINES" 'BEGIN {
    while ((getline line < f) > 0) p[++n] = line
    for (i = 1; i <= total; i++) printf "Redak %04d: %s\n", i, p[(i - 1) % n + 1]
  }' >"$tmpfile"
  rm -f "$phrases"
  mv "$tmpfile" "$SAMPLE"
  if [[ $(wc -l <"$SAMPLE" | tr -d ' ') -ne $LINES ]]; then
    echo "sample has the wrong number of lines" >&2
    return 1
  fi
  echo "$SAMPLE"
}

if [[ "${1:-}" == "sample" ]]; then
  make_sample
  exit 0
fi

# run_renderer <dom|webgl>: one run of the application, to $TMP/<r>.json.
run_renderer() {
  local r=$1 out="$TMP/$1.json" log="$TMP/$1.log"
  local seen="" waited=0 rc=0 pid cmd leftover=0
  echo "--- run: $r"

  PISAR_SPIKE_RENDERER=$r PISAR_SPIKE_BENCH=$out "$BIN" "$SAMPLE" >"$log" 2>&1 &
  APP_PID=$!

  # The application ends itself. Remember its descendants meanwhile: after it
  # is gone there is no way to find them.
  while is_running "$APP_PID"; do
    if [[ $waited -ge $((RUN_LIMIT * 4)) ]]; then
      fail "$r: the application did not finish within ${RUN_LIMIT} s"
      stop_app
      break
    fi
    seen="$seen $(descendants "$APP_PID" | tr '\n' ' ')"
    waited=$((waited + 1))
    sleep 0.25
  done
  if [[ -n "$APP_PID" ]]; then
    wait "$APP_PID" || rc=$?
    APP_PID=""
    if [[ $rc -eq 0 ]]; then ok "$r: the application exited by itself with status 0"; else fail "$r: the application exited with status $rc"; fi
  fi

  for pid in $(printf '%s\n' $seen | sort -u); do
    if is_running "$pid"; then
      cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
      if [[ "$cmd" == *nvim* ]]; then
        leftover=1
        kill -KILL "$pid" 2>/dev/null || true
      fi
    fi
  done
  if [[ $leftover -eq 0 ]]; then ok "$r: no Neovim process of ours is left"; else fail "$r: a Neovim process of ours was left (killed)"; fi

  if [[ -s "$out" ]]; then
    cp "$out" "$ROOT/spike/bin/bench-$r.json"
  else
    fail "$r: no result file"
    sed 's/^/     app: /' "$log" | tail -n 20
  fi
}

cat <<'WARN'
bench: keep the application window VISIBLE while it runs (WKWebView slows
animation frames of a hidden window, which would spoil the frame numbers), and
leave the computer alone for the ten seconds or so each of the two runs takes.
WARN

TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-bench.XXXXXX")
if (cd "$ROOT" && task -d spike build >"$TMP/build.log" 2>&1); then
  ok "task -d spike build"
else
  fail "task -d spike build"
  tail -n 30 "$TMP/build.log"
  exit 1
fi
make_sample >/dev/null
ok "sample: $SAMPLE"

run_renderer dom
run_renderer webgl

echo
results=""
for r in dom webgl; do
  if [[ -s "$TMP/$r.json" ]]; then results="$results $TMP/$r.json"; fi
done
if [[ -n "$results" ]]; then
  # The binary prints the table and fails on an error, a mismatch of events or
  # screen. It does not start the application in this mode.
  # shellcheck disable=SC2086
  "$BIN" --bench-table $results || FAILS=$((FAILS + 1))
fi

echo
if [[ "$FAILS" -eq 0 ]]; then
  echo "bench: all runs clean; results in $ROOT/spike/bin/bench-{dom,webgl}.json"
else
  echo "bench: FAILED ($FAILS problem(s))"
fi
exit "$FAILS"
