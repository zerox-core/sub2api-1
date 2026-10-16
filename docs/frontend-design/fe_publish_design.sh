#!/bin/bash
# Compatibility wrapper.
#
# The real implementation is fe_publish_design_ascii.sh (ASCII-only, safe to ship
# over ssh, and correct for this repo's code-splitting vite build).
#
# This name used to hold a second, staler copy that selected assets with
# `ls assets/index-*.js | head -1` and `ls assets/style-*.css | head -1`. In the
# merged build `style-*` matches nothing, so it copied a stale hashed CSS file
# that index.html no longer referenced, and copying a single JS chunk white-screened
# the app because the vendor-* chunks were omitted. It also verified over plain
# HTTP with a Host header, which the certbot redirect answers with 301, so the
# release aborted after the backup but before it could be called successful.
#
# Anything invoking this path now gets the corrected logic.
set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
exec bash "$HERE/fe_publish_design_ascii.sh" "$@"
