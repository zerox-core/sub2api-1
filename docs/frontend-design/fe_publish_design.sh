#!/bin/bash
# sub2api production frontend publish (Cosmic design tree build) -- guard + full backup + verify.
# ASCII-only reimplementation of docs/frontend-design/fe_publish_design.sh (same guard checks).
# usage: sudo STAGE=/path/to/dist-override bash /tmp/fe_publish_auto_ascii.sh
set -euo pipefail

STAGE=${STAGE:?need STAGE=/path/to/dist-override}
TARGET=${TARGET:-/var/www/sub2api-cosmic}
TS=$(date +%Y%m%d_%H%M%S)
BAK="${TARGET}.bak_design_${TS}"

fail() { echo "!! GUARD FAIL: $*"; exit 1; }

# ---- 0. artifact completeness ----
[ -f "$STAGE/index.html" ] || fail "STAGE missing index.html"
JS=$(ls "$STAGE"/assets/index-*.js 2>/dev/null | head -1)
CSS=$(ls "$STAGE"/assets/style-*.css 2>/dev/null | head -1)
[ -n "${JS:-}" ] || fail "STAGE missing assets/index-*.js"
[ -n "${CSS:-}" ] || fail "STAGE missing assets/style-*.css"

# ---- 1. design/feature guards ----
grep -q 'localStorage.setItem("theme"' "$STAGE/index.html" || fail "index.html missing dark theme bootstrap"
grep -q 'antigravity-quota' "$JS"        || fail "bundle missing antigravity quota badge"
grep -q 'model-allowlist-sources' "$JS"  || fail "bundle missing allowlist source suffix"
grep -q 'plaza-data.json' "$JS"          || fail "bundle missing plaza-data.json wiring"
grep -qi '#07080c' "$CSS"                || fail "css missing Cosmic palette (#07080C)"
grep -q 'custom-open-fab' "$CSS"         || fail "css missing cosmic fab"
# auto smart-routing panel marker (this release)
grep -q 'auto::rr' "$JS"                 || fail "bundle missing auto smart-routing sentinel"
echo "[guard] OK  js=$(basename "$JS")  css=$(basename "$CSS")"

# ---- 2. full backup ----
echo "[1/4] backup -> $BAK"
cp -a "$TARGET" "$BAK"
echo "      rollback: cp -a $BAK/. $TARGET/"

# ---- 3. publish ----
echo "[2/4] publish index.html + assets"
cp -f "$STAGE/index.html" "$TARGET/index.html"
mkdir -p "$TARGET/assets"
cp -f "$JS" "$CSS" "$TARGET/assets/"
[ -f "$STAGE/logo.svg" ] && cp -f "$STAGE/logo.svg" "$TARGET/logo.svg"
chmod 644 "$TARGET/index.html" "$TARGET/assets/$(basename "$JS")" "$TARGET/assets/$(basename "$CSS")"
chmod 755 "$TARGET/assets"

# ---- 4. verify ----
echo "[3/4] verify on disk"
REF=$(grep -o 'assets/index-[A-Za-z0-9_-]*\.js' "$TARGET/index.html" | head -1)
[ -f "$TARGET/$REF" ] || fail "index.html references $REF which does not exist"
echo "      ref=$REF  size=$(stat -c%s "$TARGET/$REF")"
grep -q 'plaza-data.json' "$TARGET/$REF" || fail "published JS verify failed"
grep -q 'auto::rr' "$TARGET/$REF" || fail "published JS missing auto panel"
echo "      assets dir:"; ls -la "$TARGET/assets" | head -20 || true

echo "[4/4] http verify (127.0.0.1, Host: api.zeroxcore.tech)"
for p in "/" "/$REF" "/assets/$(basename "$CSS")" "/plaza-data.json"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: api.zeroxcore.tech' "http://127.0.0.1$p")
  echo "      GET $p -> $code"
done
echo "RELEASE_OK  rollback: cp -a $BAK/. $TARGET/"
