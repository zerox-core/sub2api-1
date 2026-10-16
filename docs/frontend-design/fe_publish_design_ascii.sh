#!/bin/bash
# sub2api production frontend publish -- guard + full backup + verify.
# usage: sudo STAGE=/path/to/dist bash /path/to/fe_publish_design_ascii.sh
#
# 2026-10-17 rewrite: the Cosmic customizations are now merged INTO the main repo
# (F:\sub2api / zerox-core/sub2api-1), so the frontend is built with the repo's normal
# vite.config.ts. That config uses code splitting, so a release is NOT "one js + one css":
# it emits index-*.js + vendor-vue/vendor-i18n/vendor-misc + several css files.
# The old script copied just `assets/index-*.js` and `assets/style-*.css` (picked with
# `head -1`), which was correct only for the old single-bundle override build and is
# actively wrong now: missing vendor chunks = white screen, and the `style-*` glob
# matched a stale file whose hash index.html no longer referenced (nginx 404, raw HTML).
#
# This version derives the exact asset list from index.html itself and copies whole
# directories, then asserts every referenced URL returns HTTP 200.
set -euo pipefail

STAGE=${STAGE:?need STAGE=/path/to/dist}
TARGET=${TARGET:-/var/www/sub2api-cosmic}
HOSTHDR=${HOSTHDR:-api.zeroxcore.tech}
TS=$(date +%Y%m%d_%H%M%S)
BAK="${TARGET}.bak_${TS}"

fail() { echo "!! GUARD FAIL: $*"; exit 1; }

# ---- 0. artifact completeness ----
[ -f "$STAGE/index.html" ] || fail "STAGE missing index.html"
[ -d "$STAGE/assets" ]     || fail "STAGE missing assets/ dir"

# Referenced asset paths, exactly as index.html spells them (leading /assets/...).
REFS=$(grep -o '/assets/[A-Za-z0-9_@./-]*\.\(js\|css\)' "$STAGE/index.html" | sort -u)
[ -n "$REFS" ] || fail "index.html references no /assets/*.js or *.css"

# The entry chunk must exist; guard against publishing a stale/partial stage dir.
MAIN_JS=$(printf '%s\n' "$REFS" | grep -E '^/assets/index-.*\.js$' | head -1)
[ -n "${MAIN_JS:-}" ] || fail "index.html has no /assets/index-*.js entry chunk"

echo "[guard] index.html references $(printf '%s\n' "$REFS" | wc -l) asset(s)"

for r in $REFS; do
  [ -f "$STAGE$r" ] || fail "index.html references $r but $STAGE$r is missing"
done

# ---- 1. design/feature guards ----
# Feature markers can live in any chunk: this repo code-splits, so e.g. auto::rr is in
# GroupsView-*.js and plaza-data.json wiring is in ModelPlazaView-*.js -- neither is
# referenced directly by index.html (they load lazily). Therefore search the WHOLE
# assets tree, not just the entry chunk.
MAIN_CSS=$(printf '%s\n' "$REFS" | grep -E '^/assets/.*\.css$' | head -1)
[ -n "${MAIN_CSS:-}" ] || fail "index.html references no css"

grep -q 'localStorage.setItem("theme"' "$STAGE/index.html" || fail "index.html missing dark theme bootstrap"
# Cosmic palette lives in the tailwind css bundle.
grep -rqi '#07080c' "$STAGE/assets"      || fail "css missing Cosmic palette (#07080C)"
grep -rq  'custom-open-fab' "$STAGE/assets" || fail "css missing cosmic fab"
grep -rq  'model-allowlist-sources' "$STAGE/assets" || fail "bundle missing allowlist source suffix"
grep -rq  'auto::rr'                "$STAGE/assets" || fail "bundle missing auto smart-routing sentinel"
grep -rq  'antigravity-quota'       "$STAGE/assets" || fail "bundle missing antigravity quota badge"
grep -rq  'plaza-data.json'         "$STAGE/assets" || fail "bundle missing plaza-data.json wiring"
echo "[guard] OK  entry=$(basename "$MAIN_JS")  css=$(basename "$MAIN_CSS")"

# ---- 2. full backup ----
echo "[1/4] backup -> $BAK"
cp -a "$TARGET" "$BAK"
echo "      rollback: cp -a $BAK/. $TARGET/"

# ---- 3. publish (whole assets dir: code splitting means many chunks) ----
echo "[2/4] publish index.html + assets/ (all chunks)"
cp -f "$STAGE/index.html" "$TARGET/index.html"
# Replace assets atomically so no stale chunk can be served with the new index.html.
rm -rf "$TARGET/assets.new"
cp -a "$STAGE/assets" "$TARGET/assets.new"
rm -rf "$TARGET/assets.old"
[ -d "$TARGET/assets" ] && mv "$TARGET/assets" "$TARGET/assets.old"
mv "$TARGET/assets.new" "$TARGET/assets"
rm -rf "$TARGET/assets.old"
[ -f "$STAGE/logo.svg" ] && cp -f "$STAGE/logo.svg" "$TARGET/logo.svg"
[ -f "$STAGE/plaza-data.json" ] && cp -f "$STAGE/plaza-data.json" "$TARGET/plaza-data.json"
chmod 644 "$TARGET/index.html"
find "$TARGET/assets" -type f -exec chmod 644 {} +
chmod 755 "$TARGET/assets"

# ---- 4. verify on disk ----
echo "[3/4] verify on disk"
DISK_REF=$(grep -o '/assets/index-[A-Za-z0-9_-]*\.js' "$TARGET/index.html" | head -1)
[ -f "$TARGET$DISK_REF" ] || fail "published index.html references $DISK_REF which does not exist"
echo "      ref=$DISK_REF  size=$(stat -c%s "$TARGET$DISK_REF")"
echo "      chunk count: $(find "$TARGET/assets" -type f | wc -l)"

# ---- 5. http verify: every referenced asset must be 200 ----
# NOTE: a plain-HTTP request with `Host: $HOSTHDR` is answered by the certbot
# redirect block with 301, never 200 -- so the old check could not pass and would
# have aborted mid-release. Verify against the local nginx over TLS instead, using
# --resolve so we do not depend on public DNS.
echo "[4/4] http verify (127.0.0.1:443 via --resolve $HOSTHDR)"
RC=0
httpcode() { curl -s -k -o /dev/null -w '%{http_code}' --resolve "$HOSTHDR:443:127.0.0.1" "$1"; }
for r in $REFS; do
  code=$(httpcode "https://$HOSTHDR$r")
  echo "      GET $r -> $code"
  [ "$code" = "200" ] || RC=1
done
code=$(httpcode "https://$HOSTHDR/")
echo "      GET / -> $code"
[ "$code" = "200" ] || RC=1
[ "$RC" = "0" ] || fail "one or more referenced assets did not return 200 (rollback: cp -a $BAK/. $TARGET/)"

echo "RELEASE_OK  rollback: cp -a $BAK/. $TARGET/"
