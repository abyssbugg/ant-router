#!/usr/bin/env bash
# Installs the exact Go toolchain pinned by go.mod's `toolchain` directive
# (the same contract as actions/setup-go with go-version-file) and puts it
# first on PATH. Source this file; it exports PATH, GOROOT-free settings and
# GOTOOLCHAIN=local so no other toolchain can be selected implicitly.
set -euo pipefail

ci_die() {
  echo "setup-go: $*" >&2
  exit 1
}

toolchain="$(awk '$1 == "toolchain" { print $2 }' go.mod)"
[[ "$toolchain" =~ ^go[0-9]+\.[0-9]+\.[0-9]+$ ]] || ci_die "go.mod must pin exactly one toolchain goX.Y.Z (got '$toolchain')"

case "$(uname -s)" in
  Linux) go_os=linux ;;
  Darwin) go_os=darwin ;;
  *) ci_die "unsupported OS $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) go_arch=amd64 ;;
  arm64 | aarch64) go_arch=arm64 ;;
  *) ci_die "unsupported architecture $(uname -m)" ;;
esac

archive="${toolchain}.${go_os}-${go_arch}.tar.gz"
cache_root="${BUILDKITE_BUILD_PATH:-${TMPDIR:-/tmp}}/ant-router-go"
install_dir="${cache_root}/${toolchain}-${go_os}-${go_arch}"

if [[ ! -x "${install_dir}/go/bin/go" ]]; then
  mkdir -p "$cache_root"
  work="$(mktemp -d "${cache_root}/download.XXXXXX")"
  trap 'rm -rf "$work"' EXIT
  curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "${work}/${archive}" "https://dl.google.com/go/${archive}"
  expected="$(curl -fsSL --proto '=https' --tlsv1.2 --retry 3 "https://dl.google.com/go/${archive}.sha256" | tr -d '[:space:]')"
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || ci_die "malformed checksum for ${archive}"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${work}/${archive}" | awk '{ print $1 }')"
  else
    actual="$(shasum -a 256 "${work}/${archive}" | awk '{ print $1 }')"
  fi
  [[ "$actual" == "$expected" ]] || ci_die "checksum mismatch for ${archive}"
  mkdir -p "${work}/extract"
  tar -xzf "${work}/${archive}" -C "${work}/extract"
  rm -rf "$install_dir"
  mv "${work}/extract" "$install_dir"
  rm -rf "$work"
  trap - EXIT
fi

unset GOROOT
export PATH="${install_dir}/go/bin:${PATH}"
export GOTOOLCHAIN=local
export GOFLAGS=-mod=readonly

actual_version="$(go env GOVERSION)"
[[ "$actual_version" == "$toolchain" ]] || ci_die "expected ${toolchain}, got ${actual_version}"
go version
