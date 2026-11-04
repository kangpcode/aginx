#!/usr/bin/env bash
set -euo pipefail
if ! command -v wrk >/dev/null; then
  echo 'wrk belum tersedia; pasang di mesin benchmark lalu ulangi.' >&2
  exit 1
fi
target_url="${1:-http://127.0.0.1:8080/index.html}"
result_dir="${AGINX_BENCH_RESULTS:-bench/results}"
mkdir -p "$result_dir"
for connections in 1 64 512 4096; do
  threads=4
  if (( connections < threads )); then threads="$connections"; fi
  wrk -t"$threads" -c"$connections" -d10s "$target_url" >/dev/null
  for trial in 1 2 3 4 5; do
    wrk -t"$threads" -c"$connections" -d30s --latency "$target_url" >"$result_dir/c${connections}-run${trial}.txt"
  done
done
