#!/usr/bin/env bash
# search-relevance.sh — measure how well `cerne search` finds the entity a
# question is about (F-0367 criterion 9). Offline and read-only: the backend
# clone is copied into a throwaway cache and the index is rebuilt there by the
# binary under test, so the real cache is never written.
#
# usage: scripts/search-relevance.sh <questions.tsv> <cache-dir> [cerne-binary]
#   questions.tsv  one "question<TAB>expected-entity-id" per line ('#' = comment)
#   cache-dir      a cerne project cache (holds repo/), e.g. ~/Library/Caches/cerne/cerne
#   cerne-binary   defaults to `cerne` on PATH
#
# Prints, per question, the position of the first result line naming the
# expected entity (within the first 50 lines; "-" when absent), then the top-5
# hit count and the mean position (absent counted as 51).
set -euo pipefail

tsv=${1:?usage: $0 <questions.tsv> <cache-dir> [cerne-binary]}
cache=${2:?usage: $0 <questions.tsv> <cache-dir> [cerne-binary]}
bin=${3:-cerne}
[ -d "$cache/repo/.git" ] || { echo "no backend clone at $cache/repo" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
id=$(basename "$cache")
mkdir -p "$work/cache/$id" "$work/proj"
cp -R "$cache/repo" "$work/cache/$id/repo"
printf 'project_id = "%s"\nbackend = "%s"\n' "$id" "$work/cache/$id/repo" >"$work/proj/.cernerc"

export CERNE_CACHE_DIR="$work/cache"
# Build the index once, outside the per-question queries.
(cd "$work/proj" && "$bin" search --json --limit 1 "index" >/dev/null)

while IFS=$'\t' read -r question want; do
  case "$question" in ''|'#'*) continue ;; esac
  (cd "$work/proj" && "$bin" search --json --limit 50 "$question") |
    python3 -c '
import json, sys
want, question = sys.argv[1], sys.argv[2]
ids = [r["entity_id"] for r in json.load(sys.stdin)["results"]]
pos = ids.index(want) + 1 if want in ids else "-"
print(f"{pos}\t{want}\t{question}")' "$want" "$question"
done <"$tsv" | tee "$work/rows.tsv"

python3 - "$work/rows.tsv" <<'EOF'
import sys
rows = [l.rstrip("\n").split("\t") for l in open(sys.argv[1]) if l.strip()]
pos = [int(p) if p != "-" else 51 for p, *_ in rows]
top5 = sum(p <= 5 for p in pos)
print(f"top5: {top5}/{len(pos)}  mean position: {sum(pos)/len(pos):.1f}  absent: {pos.count(51)}")
EOF
