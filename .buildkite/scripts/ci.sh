#!/usr/bin/env bash
# Buildkite entry point. Each mode mirrors one job of .github/workflows/ci.yml
# so local `make ci`, GitHub Actions and Buildkite enforce the same gates.
set -euo pipefail

mode="${1:-}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

die() {
  echo "ci: $*" >&2
  exit 1
}

require() {
  local tool
  for tool in "$@"; do
    command -v "$tool" >/dev/null 2>&1 || die "required tool '$tool' is not installed on this agent"
  done
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

# Pinned Node.js for the Codex CLI, verified against the release's SHASUMS256.
NODE_VERSION="v24.22.0"

install_node() {
  local os arch archive root work expected
  case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) die "unsupported OS" ;; esac
  case "$(uname -m)" in x86_64 | amd64) arch=x64 ;; arm64 | aarch64) arch=arm64 ;; *) die "unsupported arch" ;; esac
  archive="node-${NODE_VERSION}-${os}-${arch}.tar.gz"
  root="${BUILDKITE_BUILD_PATH:-${TMPDIR:-/tmp}}/ant-router-node"
  if [[ ! -x "${root}/node-${NODE_VERSION}-${os}-${arch}/bin/node" ]]; then
    mkdir -p "$root"
    work="$(mktemp -d "${root}/download.XXXXXX")"
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "${work}/${archive}" "https://nodejs.org/dist/${NODE_VERSION}/${archive}"
    expected="$(curl -fsSL --proto '=https' --tlsv1.2 --retry 3 "https://nodejs.org/dist/${NODE_VERSION}/SHASUMS256.txt" | awk -v f="$archive" '$2 == f { print $1 }')"
    [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || die "no checksum published for ${archive}"
    [[ "$(sha256_of "${work}/${archive}")" == "$expected" ]] || die "checksum mismatch for ${archive}"
    tar -xzf "${work}/${archive}" -C "$root"
    rm -rf "$work"
  fi
  export PATH="${root}/node-${NODE_VERSION}-${os}-${arch}/bin:${PATH}"
  [[ "$(node --version)" == "$NODE_VERSION" ]] || die "unexpected node version $(node --version)"
}

# shellcheck source=.buildkite/scripts/setup-go.sh
source "${repo_root}/.buildkite/scripts/setup-go.sh"

case "$mode" in
  quality)
    for target in verify config-test config-race fault-test control-test control-race control-fault \
      configservice-test configservice-race cli-test cli-race phase1c-fault phase1c-e2e \
      canonical-test canonical-race canonical-golden canonical-stress \
      streaming-test streaming-race streaming-golden streaming-stress \
      phase3a-ci phase3b-ci phase4a-ci phase4b-ci phase4c-ci codex-alpha-ci; do
      echo "--- make ${target}"
      make "$target"
    done
    ;;
  platform)
    echo "--- go test ./..."
    go test ./...
    go test ./internal/controltransport -run 'TestUnix|TestResolveLocal' -count=20
    make phase1c-e2e
    go test ./internal/canonical ./internal/canonicaljson -count=20
    go test ./internal/streaming -count=20
    go test ./internal/dataplane -run TestOfficialOpenAISDKModelsAndResponses -count=20
    go test -tags=e2e ./test/e2e -run TestMockResponsesSubprocessWorkflow -count=3 -timeout=5m
    for target in phase3b-sdk phase4a-contract phase4a-live-compile phase4b-contract \
      phase4b-live-compile phase4c-contract phase4c-e2e phase4c-live-compile; do
      echo "--- make ${target}"
      make "$target"
    done
    ;;
  codex-e2e)
    install_node
    prefix="$(mktemp -d "${TMPDIR:-/tmp}/ant-router-codex.XXXXXX")"
    cp .github/codex-cli/package.json .github/codex-cli/package-lock.json "$prefix/"
    # Run inside the directory: on macOS `npm ci --prefix <dir>` resolves the
    # lockfile against the wrong root and fails with EUSAGE.
    (cd "$prefix" && npm ci --ignore-scripts)
    export PATH="${prefix}/node_modules/.bin:${PATH}"
    # The E2E test skips when codex is absent; a skip is not evidence.
    require codex
    codex --version
    make codex-alpha-e2e
    ;;
  fuzz)
    for target in fuzz-smoke control-fuzz phase1c-fuzz canonical-fuzz streaming-fuzz \
      phase3a-fuzz phase3b-fuzz phase4a-fuzz phase4b-fuzz phase4c-fuzz codex-alpha-fuzz; do
      echo "--- make ${target} FUZZ_TIME=10s"
      make "$target" FUZZ_TIME=10s
    done
    ;;
  installer)
    # Installer tests skip when a tool is missing; in CI every tool must exist
    # so that both the curl and wget download paths and install.ps1 run.
    if [[ "$(uname -s)" == Darwin ]]; then
      require brew
      export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1
      command -v wget >/dev/null 2>&1 || brew install wget
      command -v shellcheck >/dev/null 2>&1 || brew install shellcheck
      command -v pwsh >/dev/null 2>&1 || brew install --cask powershell
    fi
    require curl wget pwsh shellcheck
    # shellcheck disable=SC2016 # PowerShell expression, intentionally unexpanded.
    pwsh -NoLogo -NoProfile -Command '$PSVersionTable.PSVersion.ToString()'
    shellcheck --version | sed -n 2p
    shellcheck -x .buildkite/scripts/*.sh
    if [[ ! -d test/install ]]; then
      echo "This revision has no installer tests (test/install); CI scripts linted only."
      exit 0
    fi
    shellcheck scripts/*.sh
    ANT_ROUTER_REQUIRE_INSTALL_TOOLS=all go test ./test/install/... -count=1 -v
    ;;
  *)
    die "usage: ci.sh quality|platform|codex-e2e|fuzz|installer"
    ;;
esac
