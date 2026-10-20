#!/usr/bin/env bash
# Builds, signs and publishes the release for $CIRCLE_TAG.
#
# Order: validate the tag, require the tagged commit to be on main, run the
# full local gate (make ci) without publishing credentials, obtain a
# CircleCI OIDC token for Sigstore, run GoReleaser (build, SBOM, keyless
# signature verified by sign-and-verify.sh before anything is published,
# GitHub release, cask), then verify the published signature again.
# Nothing is built for a tag that fails validation.
set -euo pipefail

# shellcheck source=.circleci/scripts/release-identity.sh
. "$(dirname "${BASH_SOURCE[0]}")/release-identity.sh"

die() {
	echo "release: $*" >&2
	exit 1
}

export PATH="$HOME/bin:$PATH"

tag="${CIRCLE_TAG:-}"
semver='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]+[A-Za-z-][0-9A-Za-z-]*|[A-Za-z][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]+[A-Za-z-][0-9A-Za-z-]*|[A-Za-z][0-9A-Za-z-]*))*)?$'
# SemVer 2.0.0 prerelease identifiers (numeric ones without leading zeros),
# except that identifiers must start alphanumeric and +BUILD metadata is not
# allowed (install.sh and install.ps1 reject both, and "+" in a release-asset
# URL path needs encoding): every tag accepted here must also be accepted by both
# installers (TestInstallersAcceptEveryReleasableTag).
# [[ =~ ]] is POSIX ERE without REG_NEWLINE: ^ and $ anchor the whole value,
# so a tag containing a newline cannot match.
[[ "$tag" =~ $semver ]] || die "tag '$tag' is not a semantic version vMAJOR.MINOR.PATCH[-PRERELEASE]"

sha="${CIRCLE_SHA1:-}"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die "CIRCLE_SHA1 is not a commit id"
[ "${CIRCLE_PROJECT_ID:-}" = "$CIRCLECI_PROJECT_ID" ] || die "unexpected CircleCI project '${CIRCLE_PROJECT_ID:-}'"
[ -n "${GITHUB_TOKEN:-}" ] || die "GITHUB_TOKEN is not set; add it to the ant-router-release context"

git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main
git merge-base --is-ancestor "$sha" refs/remotes/origin/main || die "tagged commit $sha is not reachable from origin/main"

# The CI gate needs no publishing credentials.
env -u GITHUB_TOKEN -u HOMEBREW_TAP_GITHUB_TOKEN make ci

# Fulcio accepts CircleCI's root-issuer token with the sigstore audience.
SIGSTORE_ID_TOKEN="$(circleci run oidc get --claims '{"aud":"sigstore"}' --root-issuer)"
[ -n "$SIGSTORE_ID_TOKEN" ] || die "could not obtain a CircleCI OIDC token"
export SIGSTORE_ID_TOKEN

if [ -z "${HOMEBREW_TAP_GITHUB_TOKEN:-}" ]; then
	echo "release: HOMEBREW_TAP_GITHUB_TOKEN is not set; the Homebrew cask upload will be skipped" >&2
fi

goreleaser release --clean

# Second line of defense: the published bundle must still verify.
cosign verify-blob \
	--bundle dist/checksums.txt.sigstore.json \
	--certificate-identity "$CERT_IDENTITY" \
	--certificate-oidc-issuer "$OIDC_ISSUER" \
	dist/checksums.txt
