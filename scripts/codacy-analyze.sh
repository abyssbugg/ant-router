#!/bin/sh
# Run every analyzer configured in .codacy/codacy.yaml through the Codacy CLI
# and fail if any of them did not produce a successful SARIF run.
#
# codacy-cli v2 exits 0 even when an analyzer fails or is unknown; the only
# signal is its SARIF output, which then has no run for the tool ("runs": [])
# or an invocation with "executionSuccessful": false. Every tool still runs so
# a single invocation reports all failures.
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

failed=""
for tool in $tools; do
  echo "--- codacy-cli analyze --tool $tool"
  sarif="$workdir/$tool.sarif"
  codacy-cli analyze --tool "$tool" --format sarif -o "$sarif" || true
  if [ ! -s "$sarif" ] || ! grep -q '"driver"' "$sarif" || grep -Eq '"executionSuccessful"[[:space:]]*:[[:space:]]*false' "$sarif"; then
    echo "codacy-analyze: $tool did not complete successfully" >&2
    failed="$failed $tool"
    continue
  fi
  results=$(grep -c '"ruleId"' "$sarif" || true)
  echo "$tool: $results finding(s)"
done

if [ -n "$failed" ]; then
  echo "codacy-analyze: failed analyzers:$failed" >&2
  exit 1
fi
