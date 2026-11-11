#!/usr/bin/env bash
# Generates the sample documents in testdata/docx/ from the sources in
# testdata/docx/src/ and checks the result. The documents are committed; run
# this only when a source changes.
#   bash scripts/make-fixtures.sh
#
# Needs pandoc, textutil, zip, unzip, xmllint and file. Everything temporary
# lives in one mktemp directory, which is the only thing the script removes.
# A generated file replaces the committed one only if its bytes differ, so a
# second run changes nothing. Exit code is not 0 if any check fails.
#
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUT="$ROOT/testdata/docx"
SRC="$OUT/src"

# pandoc stamps the build time into core.xml; with this set, the stamp is
# fixed and the output depends only on the sources (pandoc manual, "Reproducible
# builds"). 1767225600 is 2026-01-01T00:00:00Z.
export SOURCE_DATE_EPOCH=1767225600
# Fixed time zone for the zip entry times set below.
export TZ=UTC

TMP=""
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

cleanup() {
  local status=$?
  trap - EXIT
  # Only ever the directory this script made itself.
  if [[ -n "$TMP" && "$TMP" == */pisar-fixtures.* ]]; then rm -rf "$TMP"; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

for tool in pandoc textutil zip unzip xmllint file; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "missing tool: $tool" >&2
    exit 1
  }
done

TMP=$(mktemp -d "${TMPDIR:-/tmp}/pisar-fixtures.XXXXXX")
mkdir "$TMP/build"
BUILD="$TMP/build"

# md2docx <name>: src/<name>.md -> build/<name>.docx. markdown-smart keeps the
# typographic quotes and the dash as written instead of turning them into
# straight quotes and "---".
md2docx() {
  pandoc -f markdown-smart -t docx --resource-path="$SRC" \
    -o "$BUILD/$1.docx" "$SRC/$1.md"
}

# publish <file>: moves build/<file> to testdata/docx/ unless the bytes are
# already the same.
publish() {
  if [[ -f "$OUT/$1" ]] && cmp -s "$BUILD/$1" "$OUT/$1"; then
    echo "same $1"
  else
    mv -f "$BUILD/$1" "$OUT/$1"
    echo "wrote $1"
  fi
}

# insert_textbox <in.docx> <out.docx>: pandoc cannot write a text box, so the
# XML paragraph in src/textbox.xml is put into word/document.xml before the
# final w:sectPr, and the archive is packed again in its original entry order
# with fixed entry times, so the output does not depend on when it is built.
insert_textbox() {
  local dir="$TMP/textbox"
  mkdir "$dir"
  unzip -q "$1" -d "$dir"
  xmllint --noout "$SRC/textbox.xml"
  FRAGMENT=$(cat "$SRC/textbox.xml") perl -0pi -e '
    BEGIN { $f = $ENV{FRAGMENT}; $f =~ s/\s+\z//; }
    $n = s/<w:sectPr/$f<w:sectPr/;
    END { exit($n ? 0 : 1) }
  ' "$dir/word/document.xml"
  xmllint --noout "$dir/word/document.xml"
  find "$dir" -exec touch -t 202601010000 {} +
  (cd "$dir" && unzip -Z1 "$1" | zip -X -D -q "$2" -@)
}

echo "== generating"
for name in basic complex-table comments tracked-changes textbox legacy; do
  md2docx "$name"
done

# The legacy source is a Word 2007+ file with a heading, a list, a table, a
# link and a footnote; textutil turns it into the old format. Which of those
# survive is what the import loses.
textutil -convert doc -output "$BUILD/legacy.doc" "$BUILD/legacy.docx"

mv "$BUILD/textbox.docx" "$BUILD/textbox-plain.docx"
insert_textbox "$BUILD/textbox-plain.docx" "$BUILD/textbox.docx"
rm -f "$BUILD/textbox-plain.docx" "$BUILD/legacy.docx"

mkdir -p "$OUT"
for f in basic.docx complex-table.docx comments.docx tracked-changes.docx textbox.docx legacy.doc; do
  [[ -s "$BUILD/$f" ]] || {
    echo "no output: $f" >&2
    exit 1
  }
  publish "$f"
done

echo "== checking"

# member <docx> <member> <file>: extracts one archive member. A temp file
# rather than a pipe: grep -q closing the pipe early would make unzip fail
# under pipefail.
member() { unzip -p "$1" "$2" >"$3" 2>/dev/null; }

# has <docx> <member> <fixed string>: the member contains the string.
has() {
  local x="$TMP/member.xml"
  member "$1" "$2" "$x" && grep -Fq -- "$3" "$x"
}

# lacks <docx> <member> <fixed string>: the member exists and does not contain
# the string. (word/comments.xml exists even in a document without comments,
# so absence is judged by elements, never by the file.)
lacks() {
  local x="$TMP/member.xml"
  member "$1" "$2" "$x" && ! grep -Fq -- "$3" "$x"
}

valid_docx() {
  # The listing goes to a file for the same reason as in member: grep -q on a
  # pipe closes it early and unzip then fails under pipefail.
  unzip -tq "$1" >/dev/null 2>&1 && unzip -Z1 "$1" >"$TMP/names.txt" &&
    grep -qx 'word/document.xml' "$TMP/names.txt" &&
    member "$1" word/document.xml "$TMP/member.xml" && xmllint --noout "$TMP/member.xml"
}

media_count() { [[ $(unzip -Z1 "$1" | grep -c '^word/media/[^/]') -eq 1 ]]; }

is_cdf() { file -b "$1" | grep -q 'Composite Document File'; }

nonempty() { [[ -s "$1" ]]; }

cells_text() {
  # complex-table: one cell with two paragraphs and one with a list.
  local x="$TMP/member.xml"
  member "$1" word/document.xml "$x" || return 1
  grep -Fq 'Prvi odlomak ćelije.' "$x" && grep -Fq 'Drugi odlomak ćelije.' "$x" &&
    grep -Fq '<w:numPr>' "$x" && grep -Fq '<w:tbl>' "$x"
}

for f in basic complex-table comments tracked-changes textbox; do
  check "$f.docx is a valid archive with word/document.xml" valid_docx "$OUT/$f.docx"
done

B="$OUT/basic.docx"
check "basic.docx: exactly one file in word/media/" media_count "$B"
check "basic.docx: has a footnote reference" has "$B" word/document.xml '<w:footnoteReference'
check "basic.docx: has a table" has "$B" word/document.xml '<w:tbl>'
check "basic.docx: has three heading levels" has "$B" word/document.xml 'w:val="Heading3"'
check "basic.docx: has a hyperlink" has "$B" word/document.xml '<w:hyperlink'
check "basic.docx: diacritics survive" has "$B" word/document.xml 'č ć ž š đ, Č Ć Ž Š Đ'
check "basic.docx: typographic quotes and dash survive" has "$B" word/document.xml '„Dobar dan” i crtica — kao'
check "basic.docx: no <w:ins " lacks "$B" word/document.xml '<w:ins '
check "basic.docx: no <w:del " lacks "$B" word/document.xml '<w:del '
check "basic.docx: no <w:comment " lacks "$B" word/comments.xml '<w:comment '
check "basic.docx: no w:txbxContent" lacks "$B" word/document.xml 'w:txbxContent'

check "complex-table.docx: cell with two paragraphs and a list" cells_text "$OUT/complex-table.docx"
check "comments.docx: <w:comment  in word/comments.xml" has "$OUT/comments.docx" word/comments.xml '<w:comment '
check "tracked-changes.docx: <w:ins  in word/document.xml" has "$OUT/tracked-changes.docx" word/document.xml '<w:ins '
check "tracked-changes.docx: <w:del  in word/document.xml" has "$OUT/tracked-changes.docx" word/document.xml '<w:del '
check "textbox.docx: w:txbxContent in word/document.xml" has "$OUT/textbox.docx" word/document.xml 'w:txbxContent'

check "legacy.doc exists and is not empty" nonempty "$OUT/legacy.doc"
check "legacy.doc is a Composite Document File" is_cdf "$OUT/legacy.doc"

if [[ $FAILS -ne 0 ]]; then
  echo "$FAILS check(s) failed" >&2
  exit 1
fi
echo "all checks passed"
