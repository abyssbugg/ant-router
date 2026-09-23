#!/usr/bin/env bash
# Deliberately separate from the token diagnostics. No retries or live providers.
set -euo pipefail

verify_results() {
  python3 - "$1" <<'PY'
import json
import sys

package = "github.com/abyssbugg/ant-router/test/e2e"
required = {
    "TestConfigurationWorkflow",
    "TestMockResponsesSubprocessWorkflow",
    "TestAnthropicProviderSubprocessAvailability",
}
passed = {name: 0 for name in required}
package_passes = 0
with open(sys.argv[1], encoding="utf-8") as source:
    for line in source:
        event = json.loads(line)
        if event.get("Package") != package:
            continue
        action, test = event.get("Action"), event.get("Test", "")
        if action in ("fail", "skip"):
            raise SystemExit("Required E2E evidence contains failure or skip")
        if action == "pass":
            if test in passed:
                passed[test] += 1
            elif not test:
                package_passes += 1
if package_passes != 1 or any(count != 1 for count in passed.values()):
    raise SystemExit("Missing or duplicate required E2E passes: " + repr(passed))
print("Verified all three required offline E2E tests passed")
PY
}

# Parser-only mode is used by contract fixtures and never starts builds.
if [[ ${1:-} == --verify-results && $# == 2 ]]; then
  verify_results "$2"
  exit 0
fi
if [[ $# == 0 ]]; then
  # GNU timeout supervises the worker's entire process group, including children.
  command -v timeout >/dev/null
  exec timeout --signal=TERM --kill-after=5s 720s bash "$0" --worker
fi
[[ $# == 1 && $1 == --worker ]] || { echo 'Invalid coverage invocation' >&2; exit 2; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]]
revision=$(git rev-parse HEAD)
[[ -n ${CIRCLE_SHA1:-} && $revision == "$CIRCLE_SHA1" ]]
printf 'Coverage checkout: %s\n' "$revision"

# Promote the downloaded pinned compiler into PATH for GOTOOLCHAIN=local E2E children.
unset GOROOT
mapfile -t pins < <(awk '/^toolchain[[:space:]]+go[0-9]+\.[0-9]+\.[0-9]+[[:space:]]*$/ {print $2}' go.mod)
[[ ${#pins[@]} == 1 ]] || { echo 'Expected one exact Go toolchain pin' >&2; exit 1; }
toolchain=${pins[0]}
export GOTOOLCHAIN="$toolchain" GOOS=linux GOARCH=amd64
export CGO_ENABLED=1 CC=gcc GOFLAGS=-mod=readonly GOSUMDB=sum.golang.org
goroot=$(go env GOROOT)
export PATH="$goroot/bin:$PATH" GOTOOLCHAIN=local
[[ $(go version) == "go version $toolchain linux/amd64" ]]
[[ $(go env GOHOSTOS) == linux && $(go env GOHOSTARCH) == amd64 ]]
command -v gcc >/dev/null
command -v python3 >/dev/null
command -v make >/dev/null
go version
unformatted=$(gofmt -l cmd internal test)
[[ -z $unformatted ]] || { printf 'Unformatted Go files:\n%s\n' "$unformatted" >&2; exit 1; }
go vet ./...
go test ./... -count=1 -timeout=5m
go test -race ./... -count=1 -timeout=5m
go build ./cmd/routerd ./cmd/routerctl
make cross-build

results=$(mktemp -t tmp_rovodev_pr_e2e.XXXXXX)
trap 'rm -f "$results"' EXIT
go test -json -tags=e2e ./test/e2e -run '^(TestConfigurationWorkflow|TestMockResponsesSubprocessWorkflow|TestAnthropicProviderSubprocessAvailability)$' -count=1 -timeout=5m | tee "$results"
verify_results "$results"
