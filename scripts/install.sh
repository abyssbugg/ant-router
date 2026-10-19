#!/bin/sh
# Ant Router installer for macOS and Linux.
#
# Downloads a release archive, verifies the keyless Sigstore signature on the
# release checksums.txt with cosign, verifies the archive against that file,
# then installs routerd and routerctl together: either both binaries are
# replaced or, on any failure, the previous ones are restored.
set -eu

REPO_URL="https://github.com/abyssbugg/ant-router"
# Releases are signed keylessly by CircleCI's tag-only "release" pipeline
# definition (.circleci/release.yml); its URL is the certificate identity.
# The identity does not name the tag, so the signed checksums.txt is bound to
# this version by requiring the exact versioned archive name below.
CERT_IDENTITY="https://circleci.com/api/v2/projects/cc5f04e8-66e9-4f18-91b0-71c671674d14/pipeline-definitions/b37a0ec3-4a25-4686-b480-6b4eda16099f"
OIDC_ISSUER="https://oidc.circleci.com"

VERSION=""
if [ -n "${ANT_ROUTER_INSTALL_DIR:-}" ]; then
	INSTALL_DIR="$ANT_ROUTER_INSTALL_DIR"
elif [ -n "${HOME:-}" ]; then
	INSTALL_DIR="$HOME/.local/bin"
else
	INSTALL_DIR=""
fi
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
  --no-verify-signature    INSECURE: skip cosign signature verification
                           (also ANT_ROUTER_INSECURE_SKIP_SIGNATURE=1).
                           Checksums are still verified, but they come from
                           the same host as the archive.
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

[ -n "$INSTALL_DIR" ] || die "install directory is empty; set --install-dir, ANT_ROUTER_INSTALL_DIR, or HOME"
[ "${ANT_ROUTER_INSECURE_SKIP_SIGNATURE:-}" != "1" ] || VERIFY_SIGNATURE=0

# url_allowed URL succeeds when URL is https to any host, or plain http to an
# exact loopback host (127.0.0.1, localhost, [::1]) so local mirrors and tests
# do not need certificates. The authority is parsed rather than glob-matched:
# credentials (userinfo), malformed ports and look-alike hosts are rejected.
url_allowed() {
	case "$1" in
		https://*) url_rest="${1#https://}" url_secure=1 ;;
		http://*) url_rest="${1#http://}" url_secure=0 ;;
		*) return 1 ;;
	esac
	url_auth="${url_rest%%/*}"
	url_auth="${url_auth%%\?*}"
	url_auth="${url_auth%%#*}"
	case "$url_auth" in
		"" | *@* | *\\* | *" "* | *:) return 1 ;;
		\[*\]) url_host="$url_auth" url_port="" ;;
		\[*\]:*) url_host="${url_auth%%]*}]" url_port="${url_auth#*]:}" ;;
		\[* | *\]*) return 1 ;;
		*:*) url_host="${url_auth%%:*}" url_port="${url_auth#*:}" ;;
		*) url_host="$url_auth" url_port="" ;;
	esac
	case "$url_port" in
		*[!0-9]*) return 1 ;;
	esac
	[ -n "$url_host" ] || return 1
	[ "$url_secure" -eq 1 ] && return 0
	case "$url_host" in
		127.0.0.1 | localhost | \[::1\]) return 0 ;;
	esac
	return 1
}

BASE_URL="${BASE_URL%/}"
url_allowed "$BASE_URL" ||
	die "base URL must use https (http is allowed only for loopback) and must not contain credentials: $BASE_URL"

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

# fetch_once URL DEST performs one request without following redirects and
# prints "STATUS LOCATION". Connection and total time are bounded so a stalled
# host cannot hang the installer.
fetch_once() {
	if [ "$DOWNLOADER" = "curl" ]; then
		curl -sS --proto '=https,http' --connect-timeout 10 --max-time 600 \
			-o "$2" -w '%{http_code} %{redirect_url}' "$1"
	else
		# wget reports redirects it refuses to follow as errors; the status is
		# taken from the response headers instead of the exit code. GNU wget
		# strips the CR of each header line; other builds may keep it.
		wget -q -S --max-redirect=0 --timeout=30 --tries=1 -O "$2" "$1" 2>&1 |
			awk '$1 ~ /^HTTP\// { code = $2 } tolower($1) == "location:" { loc = $2 }
				END { sub(/\r$/, "", loc); if (code == "") exit 1; print code " " loc }'
	fi
}

# resolve_location BASE LOCATION turns a redirect target into an absolute URL.
resolve_location() {
	case "$2" in
		https://* | http://*) printf '%s\n' "$2" ;;
		/*)
			origin_rest="${1#*://}"
			printf '%s://%s%s\n' "${1%%://*}" "${origin_rest%%/*}" "$2"
			;;
		*) return 1 ;;
	esac
}

# download URL DEST follows at most 10 redirects manually, validating every
# hop with url_allowed before any request is made to it. On success the final
# URL is stored in DL_FINAL_URL.
DL_FINAL_URL=""
download() {
	dl_url="$1"
	dl_hops=0
	while :; do
		url_allowed "$dl_url" ||
			die "refusing to download from $dl_url (only https, or http to loopback, without credentials, is allowed)"
		dl_result=$(fetch_once "$dl_url" "$2") || die "failed to download $dl_url"
		dl_code="${dl_result%% *}"
		dl_location="${dl_result#* }"
		case "$dl_code" in
			2??)
				DL_FINAL_URL="$dl_url"
				return 0
				;;
			301 | 302 | 303 | 307 | 308)
				[ -n "$dl_location" ] || die "redirect without a Location header from $dl_url"
				dl_hops=$((dl_hops + 1))
				[ "$dl_hops" -le 10 ] || die "too many redirects downloading $1"
				dl_url=$(resolve_location "$dl_url" "$dl_location") ||
					die "unsupported redirect target from $dl_url: $dl_location"
				;;
			*) die "HTTP $dl_code from $dl_url" ;;
		esac
	done
}

valid_version() {
	printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$'
}

if [ -z "$VERSION" ]; then
	echo "Resolving latest release..."
	download "$BASE_URL/releases/latest" /dev/null
	case "$DL_FINAL_URL" in
		*/releases/tag/*) VERSION="${DL_FINAL_URL##*/releases/tag/}" ;;
		*) die "could not resolve the latest release from: $DL_FINAL_URL" ;;
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

if [ "$VERIFY_SIGNATURE" -eq 1 ]; then
	command -v cosign >/dev/null 2>&1 ||
		die "cosign is required to verify the release signature; install it (https://docs.sigstore.dev/cosign/system_config/installation/) or rerun with --no-verify-signature to accept the risk"
else
	echo "WARNING: signature verification is disabled; checksums.txt is not authenticated and only guards against corrupted downloads." >&2
fi

COMMITTED=0
BACKED_UP=" "
REPLACED=" "

# cleanup rolls back a partial install: every replaced binary is restored from
# its backup (or removed when there was none), so the directory never holds a
# mixed-version pair.
cleanup() {
	for BIN in routerd routerctl; do
		DEST="$INSTALL_DIR/$BIN"
		BACKUP="$INSTALL_DIR/.$BIN.bak.$$"
		case "$COMMITTED$REPLACED$BACKED_UP" in
			0*" $BIN "*" $BIN "*)
				mv -f "$BACKUP" "$DEST" || echo "Error: could not restore $DEST from $BACKUP" >&2
				;;
			0*" $BIN "*)
				case "$REPLACED" in
					*" $BIN "*) rm -f "$DEST" ;;
				esac
				rm -f "$BACKUP"
				;;
			*) rm -f "$BACKUP" ;;
		esac
		rm -f "$INSTALL_DIR/.$BIN.tmp.$$"
	done
	if [ -n "$TEMP_DIR" ]; then
		rm -rf "$TEMP_DIR"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ant-router-install.XXXXXX") ||
	die "failed to create a temporary directory"

echo "Downloading $ARCHIVE..."
download "$RELEASE_URL/$ARCHIVE" "$TEMP_DIR/$ARCHIVE"
download "$RELEASE_URL/checksums.txt" "$TEMP_DIR/checksums.txt"

if [ "$VERIFY_SIGNATURE" -eq 1 ]; then
	echo "Verifying checksums.txt signature..."
	download "$RELEASE_URL/checksums.txt.sigstore.json" "$TEMP_DIR/checksums.txt.sigstore.json"
	cosign verify-blob \
		--bundle "$TEMP_DIR/checksums.txt.sigstore.json" \
		--certificate-identity "$CERT_IDENTITY" \
		--certificate-oidc-issuer "$OIDC_ISSUER" \
		"$TEMP_DIR/checksums.txt" || die "signature verification failed"
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

mkdir -p "$INSTALL_DIR" || die "failed to create $INSTALL_DIR"

# A directory (or a symlink to one) at the destination would make mv nest the
# binary inside it and report success without installing anything.
for BIN in routerd routerctl; do
	[ ! -d "$INSTALL_DIR/$BIN" ] || die "$INSTALL_DIR/$BIN is a directory"
done

# Stage both binaries next to their destination before touching either one.
for BIN in routerd routerctl; do
	STAGED="$INSTALL_DIR/.$BIN.tmp.$$"
	cp "$TEMP_DIR/extract/$BIN" "$STAGED" || die "failed to stage $BIN"
	chmod 0755 "$STAGED" || die "failed to set permissions on $BIN"
done

# Back up each existing binary with a hard link (falling back to a copy), so
# the destination never disappears, then rename the staged binary over it.
# Any failure triggers the rollback in cleanup.
for BIN in routerd routerctl; do
	DEST="$INSTALL_DIR/$BIN"
	BACKUP="$INSTALL_DIR/.$BIN.bak.$$"
	if [ -e "$DEST" ] || [ -L "$DEST" ]; then
		ln -f "$DEST" "$BACKUP" 2>/dev/null || cp -p "$DEST" "$BACKUP" ||
			die "failed to back up $DEST"
		BACKED_UP="$BACKED_UP$BIN "
	fi
	mv -f "$INSTALL_DIR/.$BIN.tmp.$$" "$DEST" || die "failed to install $BIN"
	REPLACED="$REPLACED$BIN "
done
COMMITTED=1
for BIN in routerd routerctl; do
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
