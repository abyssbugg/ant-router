#!/usr/bin/env bash
# Installs the exact GoReleaser, cosign and syft releases used to build, sign
# and attach SBOMs to ant-router releases. Every download is verified against
# the sha256 recorded here (taken from each project's published checksum
# file for that version) before it is installed into ~/bin.
set -euo pipefail

GORELEASER_VERSION=v2.18.3
GORELEASER_SHA256=b8dee4728bca1aba0d71a832a17cd0a3799f75372878e7ffa36aef2343bce0bc
COSIGN_VERSION=v3.1.3
COSIGN_SHA256=4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71
SYFT_VERSION=v1.54.1
SYFT_SHA256=c069905b391cc4c20a5ba65ad5c10be2a7ba074f8ea6ad203e24d14e303dad47

die() {
	echo "install-release-tools: $*" >&2
	exit 1
}

[ "$(uname -s)-$(uname -m)" = "Linux-x86_64" ] || die "the pinned checksums are for linux/amd64, got $(uname -s)-$(uname -m)"

bin_dir="$HOME/bin"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$bin_dir"

# fetch <url> <sha256> <output>
fetch() {
	curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 30 --max-time 300 -o "$3" "$1"
	actual="$(sha256sum "$3" | awk '{ print $1 }')"
	[ "$actual" = "$2" ] || die "checksum mismatch for $1: got $actual, want $2"
}

fetch "https://github.com/goreleaser/goreleaser/releases/download/${GORELEASER_VERSION}/goreleaser_Linux_x86_64.tar.gz" \
	"$GORELEASER_SHA256" "$work/goreleaser.tar.gz"
tar -xzf "$work/goreleaser.tar.gz" -C "$work" goreleaser
install -m 0755 "$work/goreleaser" "$bin_dir/goreleaser"

fetch "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-amd64" \
	"$COSIGN_SHA256" "$work/cosign"
install -m 0755 "$work/cosign" "$bin_dir/cosign"

fetch "https://github.com/anchore/syft/releases/download/${SYFT_VERSION}/syft_${SYFT_VERSION#v}_linux_amd64.tar.gz" \
	"$SYFT_SHA256" "$work/syft.tar.gz"
tar -xzf "$work/syft.tar.gz" -C "$work" syft
install -m 0755 "$work/syft" "$bin_dir/syft"

export PATH="$bin_dir:$PATH"
goreleaser --version | head -n 1
cosign version 2>&1 | grep -F GitVersion
syft version | grep -F Version
