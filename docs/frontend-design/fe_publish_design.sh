#!/bin/bash
# sub2api 鐢熶骇鍓嶇鍙戝竷锛圕osmic 璁捐鏍戞瀯寤轰骇鐗╋級鈥斺€?瀹堝崼 + 鍏ㄩ噺澶囦唤 + 鏍￠獙
#
# 涓?fe_release_safe.sh 鐨勫尯鍒細鏈剼鏈笉鍋氭湇鍔＄鏋勫缓锛屽彧鍙戝竷鏈湴璁捐鏍?# (F:\aily_scratch\fe_build\src\sub2api-0.2.11\frontend -> dist-override) 鐨勪骇鐗┿€?# 鍙戝竷鍓嶅畧鍗細鎷掔粷浠讳綍缂哄皯銆屾繁鑹蹭富棰樺紩瀵艰剼鏈?/ 涓や釜寰界珷 / 骞垮満鏁版嵁婧?/ Cosmic 璋冭壊鏉裤€嶇殑浜х墿銆?#
# 鐢ㄦ硶锛歴udo STAGE=/home/ubuntu/fe_stage_xxx bash fe_publish_design.sh
set -euo pipefail

STAGE=${STAGE:?闇€瑕?STAGE=/path/to/dist-override}
TARGET=${TARGET:-/var/www/sub2api-cosmic}
TS=$(date +%Y%m%d_%H%M%S)
BAK="${TARGET}.bak_design_${TS}"

fail() { echo "!! GUARD FAIL: $*"; exit 1; }

# ---- 0. 浜х墿瀹屾暣鎬?----
[ -f "$STAGE/index.html" ] || fail "STAGE 缂哄皯 index.html"
JS=$(ls "$STAGE"/assets/index-*.js 2>/dev/null | head -1)
CSS=$(ls "$STAGE"/assets/style-*.css 2>/dev/null | head -1)
[ -n "${JS:-}" ] || fail "STAGE 缂哄皯 assets/index-*.js"
[ -n "${CSS:-}" ] || fail "STAGE 缂哄皯 assets/style-*.css"

# ---- 1. 璁捐/鍔熻兘瀹堝崼锛堜换涓€鏉′笉婊¤冻鍗虫嫆缁濆彂甯冿級----
grep -q 'localStorage.setItem("theme"' "$STAGE/index.html" || fail "index.html 缂哄皯娣辫壊涓婚寮曞鑴氭湰"
grep -q 'antigravity-quota' "$JS"        || fail "鎵撳寘浜х墿缂哄皯 antigravity 閰嶉寰界珷"
grep -q 'model-allowlist-sources' "$JS"  || fail "鎵撳寘浜х墿缂哄皯鐧藉悕鍗曟潵婧愯处鍙峰悗缂€"
grep -q 'plaza-data.json' "$JS"          || fail "妯″瀷骞垮満鏈帴 /plaza-data.json锛堜細閫€鍖栨垚婕旂ず鏁版嵁锛?
grep -qi '#07080c' "$CSS"                || fail "CSS 缂哄皯 Cosmic 璋冭壊鏉?(#07080C)"
grep -q 'custom-open-fab' "$CSS"         || fail "CSS 缂哄皯 Cosmic 鎮诞鐞冩牱寮?
echo "[guard] OK  js=$(basename "$JS")  css=$(basename "$CSS")"

# ---- 2. 鍏ㄩ噺澶囦唤锛堝惈 assets锛岀粷涓嶅垹闄ゆ棦鏈夋枃浠讹級----
echo "[1/4] backup -> $BAK"
cp -a "$TARGET" "$BAK"
echo "      rollback: cp -a $BAK/. $TARGET/"

# ---- 3. 鍙戝竷 ----
echo "[2/4] publish index.html + assets"
cp -f "$STAGE/index.html" "$TARGET/index.html"
mkdir -p "$TARGET/assets"
cp -f "$JS" "$CSS" "$TARGET/assets/"
[ -f "$STAGE/logo.svg" ] && cp -f "$STAGE/logo.svg" "$TARGET/logo.svg"
chmod 644 "$TARGET/index.html" "$TARGET/assets/$(basename "$JS")" "$TARGET/assets/$(basename "$CSS")"
chmod 755 "$TARGET/assets"

# ---- 4. 鏍￠獙 ----
echo "[3/4] verify on disk"
REF=$(grep -o 'assets/index-[A-Za-z0-9_-]*\.js' "$TARGET/index.html" | head -1)
[ -f "$TARGET/$REF" ] || fail "index.html 寮曠敤鐨?$REF 涓嶅瓨鍦?
echo "      ref=$REF  size=$(stat -c%s "$TARGET/$REF")"
grep -q 'plaza-data.json' "$TARGET/$REF" || fail "鍙戝竷鍚?JS 鍥炶鏍￠獙澶辫触"
echo "      plaza-data.json: $(stat -c%s "$TARGET/plaza-data.json" 2>/dev/null || echo MISSING)"
echo "      assets dir:"; ls -la "$TARGET/assets" || true

echo "[4/4] http verify (127.0.0.1, Host: api.zeroxcore.tech)"
for p in "/" "/$REF" "/assets/$(basename "$CSS")" "/plaza-data.json"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: api.zeroxcore.tech' "http://127.0.0.1$p")
  echo "      GET $p -> $code"
done
echo "RELEASE_OK  rollback: cp -a $BAK/. $TARGET/"
