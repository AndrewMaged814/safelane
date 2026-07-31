#!/usr/bin/env bash
# Wraps the artifact fragment into a complete, browser-openable page.
#
# safelane-brief.html is a FRAGMENT: no doctype, no head, no body. The artifact
# host supplies those and renders Mermaid itself. Opened straight from disk it
# therefore shows the diagram blocks as raw text.
#
# This script emits safelane-brief.local.html — the same content, wrapped in a
# real page that pulls Mermaid from a CDN. Needs an internet connection to draw
# the diagrams; everything else works offline.
#
# Re-run after every edit to safelane-brief.html. The fragment stays the single
# source of truth; the local file is generated and disposable.
#
# Usage:  bash build-preview.sh

set -euo pipefail
cd "$(dirname "$0")"

SRC="safelane-brief.html"
OUT="safelane-brief.local.html"

[ -f "$SRC" ] || { echo "error: $SRC not found" >&2; exit 1; }

{
  cat <<'HEAD'
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<style>
  *, *::before, *::after { box-sizing: border-box; }
  body { margin: 0; }
  img, svg { max-width: 100%; }
</style>
</head>
<body>
HEAD

  cat "$SRC"

  cat <<'FOOT'
<script type="module">
  import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
  mermaid.initialize({ startOnLoad: true, theme: "neutral", securityLevel: "loose" });
</script>
FOOT

  printf '</body>\n</html>\n'
} > "$OUT"

echo "wrote $OUT"
echo "open it with:  start $OUT"
