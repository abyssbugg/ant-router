#!/bin/sh
# Run every analyzer configured in .codacy/codacy.yaml through the Codacy CLI
# and fail if any of them did not produce a successful SARIF run.
#
# codacy-cli v2 exits 0 even when an analyzer fails or is unknown, so its exit
# status alone is not enough: the SARIF output must also contain a run for the
# tool (a failed tool yields "runs": []) and no invocation with
# "executionSuccessful": false. Every tool still runs so a single invocation
# reports all failures.
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
  status=0
  codacy-cli analyze --tool "$tool" --format sarif -o "$sarif" || status=$?
  if [ "$status" -ne 0 ] || [ ! -s "$sarif" ] || ! grep -q '"driver"' "$sarif" || grep -Eq '"executionSuccessful"[[:space:]]*:[[:space:]]*false' "$sarif"; then
    echo "codacy-analyze: $tool did not complete successfully" >&2
    failed="$failed $tool"
    continue
  fi
  # SARIF is usually compact single-line JSON: count occurrences, not lines.
  results=$(awk '{ n += gsub(/"ruleId"/, "") } END { print n + 0 }' "$sarif")
  echo "$tool: $results finding(s)"
done

if [ -n "$failed" ]; then
  echo "codacy-analyze: failed analyzers:$failed" >&2
  exit 1
fi
