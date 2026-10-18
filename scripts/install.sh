#!/bin/sh
# Ant Router installer for macOS and Linux.
#
# Downloads a release archive, verifies it against the release checksums.txt
# (and, when cosign is available, verifies the keyless signature on
# checksums.txt), then atomically installs routerd and routerctl.
set -eu

REPO_URL="https://github.com/abyssbugg/ant-router"
CERT_IDENTITY_PREFIX="$REPO_URL/.github/workflows/release.yml@refs/tags/"
OIDC_ISSUER="https://token.actions.githubusercontent.com"

VERSION=""
INSTALL_DIR="${ANT_ROUTER_INSTALL_DIR:-${HOME:-}/.local/bin}"
BASE_URL="$REPO_URL"
VERIFY_SIGNATURE=1
DRY_RUN=0
TEMP_DIR=""

usage() {
	cat <<EOF
Usage: install.sh [options]

Options:
  --version VERSION        Release tag to install, e.g. v1.2.3 (default: latest)
  --install-dir DIR        Installation directory
                           (default: \$ANT_ROUTER_INSTALL_DIR or ~/.local/bin)
  --base-url URL           Release host base URL (default: $REPO_URL)
  --no-verify-signature    Skip cosign signature verification (checksums are
                           always verified)
  --dry-run                Print what would be installed and change nothing
  --help                   Show this help message
EOF
}

die() {
	echo "Error: $*" >&2
	exit 1
}

need_value() {
	# $1 = option name, $2 = remaining argument count
	[ "$2" -ge 2 ] || die "$1 requires a value"
}

while [ $# -gt 0 ]; do
	case "$1" in
		--version)
			need_value "$1" $#
			VERSION="$2"
			shift 2
			;;
		--install-dir)
			need_value "$1" $#
			INSTALL_DIR="$2"
			shift 2
			;;
		--base-url)
			need_value "$1" $#
			BASE_URL="$2"
			shift 2
			;;
		--no-verify-signature)
			VERIFY_SIGNATURE=0
			shift
			;;
		--dry-run)
			DRY_RUN=1
			shift
			;;
		--help | -h)
			usage
			exit 0
			;;
		*)
			usage >&2
			die "unknown option: $1"
			;;
	esac
done

[ -n "$INSTALL_DIR" ] || die "install directory is empty; set --install-dir or HOME"

# Only HTTPS release hosts are accepted. Plain HTTP is allowed solely for
# loopback hosts so local mirrors and tests do not need certificates.
BASE_URL="${BASE_URL%/}"
case "$BASE_URL" in
	https://?*) ;;
	http://127.0.0.1 | http://127.0.0.1:* | http://127.0.0.1/* | \
		http://localhost | http://localhost:* | http://localhost/* | \
		http://\[::1\] | http://\[::1\]:* | http://\[::1\]/*) ;;
	*) die "base URL must use https (http is allowed only for loopback): $BASE_URL" ;;
esac

case "$(uname -s)" in
	Darwin) OS="darwin" ;;
	Linux) OS="linux" ;;
	*) die "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64 | amd64) ARCH="amd64" ;;
	arm64 | aarch64) ARCH="arm64" ;;
	*) die "unsupported architecture: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
	DOWNLOADER="curl"
elif command -v wget >/dev/null 2>&1; then
	DOWNLOADER="wget"
else
	die "curl or wget is required"
fi

# Validates that a URL uses HTTPS or loopback HTTP (for tests/local mirrors only).
validate_url_scheme() {
	# shellcheck disable=SC3043
	local url="$1"
	case "$url" in
		https://?*) return 0 ;;
		http://127.0.0.1*) return 0 ;;
		http://localhost*) return 0 ;;
		http://\[::1\]*) return 0 ;;
		http://?*) 
			die "URL redirected to insecure HTTP (only HTTPS or loopback HTTP allowed): $url"
			;;
		*) 
			die "URL uses unsupported scheme: $url"
			;;
	esac
}

# download URL DEST
download() {
	# shellcheck disable=SC3043
	local final_url
	final_url=$(effective_url "$1") || die "failed to resolve final URL for: $1"
	validate_url_scheme "$final_url"
	
	if [ "$DOWNLOADER" = "curl" ]; then
		curl -fsSL --proto '=https,http' --connect-timeout 10 -o "$2" "$1"
	else
		wget -q --timeout=10 -O "$2" "$1"
	fi
}

# Prints the final URL after following redirects from $1.
effective_url() {
	if [ "$DOWNLOADER" = "curl" ]; then
		curl -fsSL --proto '=https,http' --connect-timeout 10 -o /dev/null -w '%{url_effective}' "$1"
	else
		wget -S --timeout=10 --spider "$1" 2>&1 | awk '$1 == "Location:" { loc = $2 } END { print loc }'
	fi
}

valid_version() {
	printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$'
}

if [ -z "$VERSION" ]; then
	echo "Resolving latest release..."
	LATEST_URL=$(effective_url "$BASE_URL/releases/latest") ||
		die "could not resolve the latest release"
	case "$LATEST_URL" in
		*/releases/tag/*) VERSION="${LATEST_URL##*/releases/tag/}" ;;
		*) die "could not resolve the latest release from: $LATEST_URL" ;;
	esac
fi

valid_version "$VERSION" || die "invalid version: $VERSION (expected a tag like v1.2.3)"

ARCHIVE="ant-router_${VERSION#v}_${OS}_${ARCH}.tar.gz"
RELEASE_URL="$BASE_URL/releases/download/$VERSION"

echo "Installing ant-router $VERSION for $OS/$ARCH into $INSTALL_DIR"
if [ "$DRY_RUN" -eq 1 ]; then
	echo "[dry run] would download $RELEASE_URL/$ARCHIVE"
	echo "[dry run] would verify $RELEASE_URL/checksums.txt"
	echo "[dry run] would install routerd and routerctl into $INSTALL_DIR"
	exit 0
fi

cleanup() {
	if [ -n "$TEMP_DIR" ]; then
		rm -rf "$TEMP_DIR"
	fi
	rm -f "$INSTALL_DIR/.routerd.tmp.$$" "$INSTALL_DIR/.routerctl.tmp.$$"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

TEMP_DIR=$(mktemp -d)

echo "Downloading $ARCHIVE..."
download "$RELEASE_URL/$ARCHIVE" "$TEMP_DIR/$ARCHIVE" || die "failed to download $ARCHIVE"
download "$RELEASE_URL/checksums.txt" "$TEMP_DIR/checksums.txt" || die "failed to download checksums.txt"

if [ "$VERIFY_SIGNATURE" -eq 1 ]; then
	if command -v cosign >/dev/null 2>&1; then
		echo "Verifying checksums.txt signature..."
		download "$RELEASE_URL/checksums.txt.sigstore.json" "$TEMP_DIR/checksums.txt.sigstore.json" ||
			die "failed to download checksums.txt.sigstore.json"
		cosign verify-blob \
			--bundle "$TEMP_DIR/checksums.txt.sigstore.json" \
			--certificate-identity "$CERT_IDENTITY_PREFIX$VERSION" \
			--certificate-oidc-issuer "$OIDC_ISSUER" \
			"$TEMP_DIR/checksums.txt" || die "signature verification failed"
	else
		# Signature verification is required; allow override only with explicit insecure flag
		if [ -z "${ANT_ROUTER_INSECURE_SKIP_SIGNATURE:-}" ]; then
			die "cosign not found; signature verification is required (install cosign or set ANT_ROUTER_INSECURE_SKIP_SIGNATURE=1 to accept this risk)"
		fi
		echo "WARNING: Skipping signature verification (ANT_ROUTER_INSECURE_SKIP_SIGNATURE is set). This installation is not secure."
	fi
elif [ -n "${ANT_ROUTER_INSECURE_SKIP_SIGNATURE:-}" ]; then
	echo "WARNING: Signature verification is disabled (ANT_ROUTER_INSECURE_SKIP_SIGNATURE is set). Checksum verification only."
fi

echo "Verifying checksum..."
# Exact filename match on the second field; exactly one well-formed line.
EXPECTED=$(awk -v name="$ARCHIVE" '$2 == name || $2 == "*" name { print $1 }' "$TEMP_DIR/checksums.txt")
case "$EXPECTED" in
	"") die "no checksum for $ARCHIVE in checksums.txt" ;;
	*[!0-9a-f]*) die "malformed or duplicate checksum entry for $ARCHIVE" ;;
esac
[ "${#EXPECTED}" -eq 64 ] || die "malformed or duplicate checksum entry for $ARCHIVE"

if command -v sha256sum >/dev/null 2>&1; then
	ACTUAL=$(sha256sum "$TEMP_DIR/$ARCHIVE" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	ACTUAL=$(shasum -a 256 "$TEMP_DIR/$ARCHIVE" | awk '{ print $1 }')
else
	die "sha256sum or shasum is required"
fi
[ "$ACTUAL" = "$EXPECTED" ] || die "checksum mismatch for $ARCHIVE (expected $EXPECTED, got $ACTUAL)"

# Extract only the two expected members; nothing else in the archive is written.
mkdir "$TEMP_DIR/extract"
tar -xzf "$TEMP_DIR/$ARCHIVE" -C "$TEMP_DIR/extract" routerd routerctl ||
	die "failed to extract routerd and routerctl from $ARCHIVE"

for BIN in routerd routerctl; do
	SRC="$TEMP_DIR/extract/$BIN"
	if [ -L "$SRC" ] || [ ! -f "$SRC" ]; then
		die "$BIN in $ARCHIVE is not a regular file"
	fi
done

mkdir -p "$INSTALL_DIR"

# Stage both binaries next to their destination, then rename each into place
# so a failure never leaves a partially written binary.
for BIN in routerd routerctl; do
	STAGED="$INSTALL_DIR/.$BIN.tmp.$$"
	cp "$TEMP_DIR/extract/$BIN" "$STAGED" || die "failed to stage $BIN"
	chmod 0755 "$STAGED" || die "failed to set permissions on $BIN"
done
for BIN in routerd routerctl; do
	mv -f "$INSTALL_DIR/.$BIN.tmp.$$" "$INSTALL_DIR/$BIN" || die "failed to install $BIN"
	echo "Installed $INSTALL_DIR/$BIN"
done

case ":${PATH:-}:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		echo ""
		echo "Note: $INSTALL_DIR is not on your PATH. Add it with:"
		echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
		;;
esac

echo "Installation complete."
