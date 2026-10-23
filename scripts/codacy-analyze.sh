#!/bin/sh
# Run every analyzer configured in .codacy/codacy.yaml through the Codacy CLI
# and fail if any of them did not produce a successful SARIF run.
#
# codacy-cli v2 exits 0 even when an analyzer fails or is unknown, so its exit
# status alone is not enough: scripts/sarifcheck also parses each SARIF report
# and requires a run of that analyzer with no failed invocation (a failed tool
# yields "runs": []). Every tool still runs so a single invocation reports all
# failures.
set -eu

config=.codacy/codacy.yaml
tools=$(awk '
  /^[^[:space:]]/ { in_tools = ($0 ~ /^tools:/); next }
  in_tools && /^[[:space:]]*-[[:space:]]*/ {
    sub(/^[[:space:]]*-[[:space:]]*/, ""); sub(/@.*/, ""); print
  }' "$config" | sort)
if [ -z "$tools" ]; then
  echo "codacy-analyze: no tools configured in $config" >&2
  exit 1
fi

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

sarifcheck="$workdir/sarifcheck"
go build -o "$sarifcheck" ./scripts/sarifcheck

failed=""
for tool in $tools; do
  echo "--- codacy-cli analyze --tool $tool"
  sarif="$workdir/$tool.sarif"
  status=0
  codacy-cli analyze --tool "$tool" --format sarif -o "$sarif" || status=$?
  if [ "$status" -ne 0 ] || ! "$sarifcheck" "$tool" "$sarif"; then
    echo "codacy-analyze: $tool did not complete successfully" >&2
    failed="$failed $tool"
  fi
done

if [ -n "$failed" ]; then
  echo "codacy-analyze: failed analyzers:$failed" >&2
  exit 1
fi
