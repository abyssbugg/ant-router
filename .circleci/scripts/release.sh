#!/usr/bin/env bash
# Builds, signs and publishes the release for $CIRCLE_TAG.
#
# Order: validate the tag, require the tagged commit to be on main, run the
# full local gate (make ci), obtain a CircleCI OIDC token for Sigstore,
# run GoReleaser (build, SBOM, keyless signature, GitHub release, cask),
# then verify the produced signature with exactly the identity the
# installers enforce. Nothing is built for a tag that fails validation.
set -euo pipefail

# The identity is this project's tag-only "release" pipeline definition.
CIRCLECI_PROJECT_ID=cc5f04e8-66e9-4f18-91b0-71c671674d14
RELEASE_PIPELINE_DEFINITION=b37a0ec3-4a25-4686-b480-6b4eda16099f
CERT_IDENTITY="https://circleci.com/api/v2/projects/${CIRCLECI_PROJECT_ID}/pipeline-definitions/${RELEASE_PIPELINE_DEFINITION}"
OIDC_ISSUER="https://oidc.circleci.com"

die() {
	echo "release: $*" >&2
	exit 1
}

export PATH="$HOME/bin:$PATH"

tag="${CIRCLE_TAG:-}"
semver='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
# [[ =~ ]] is POSIX ERE without REG_NEWLINE: ^ and $ anchor the whole value,
# so a tag containing a newline cannot match.
[[ "$tag" =~ $semver ]] || die "tag '$tag' is not a semantic version vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]"

sha="${CIRCLE_SHA1:-}"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die "CIRCLE_SHA1 is not a commit id"
[ "${CIRCLE_PROJECT_ID:-}" = "$CIRCLECI_PROJECT_ID" ] || die "unexpected CircleCI project '${CIRCLE_PROJECT_ID:-}'"
[ -n "${GITHUB_TOKEN:-}" ] || die "GITHUB_TOKEN is not set; add it to the ant-router-release context"

git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main
git merge-base --is-ancestor "$sha" refs/remotes/origin/main || die "tagged commit $sha is not reachable from origin/main"

make ci

# Fulcio accepts CircleCI's root-issuer token with the sigstore audience.
SIGSTORE_ID_TOKEN="$(circleci run oidc get --claims '{"aud":"sigstore"}' --root-issuer)"
[ -n "$SIGSTORE_ID_TOKEN" ] || die "could not obtain a CircleCI OIDC token"
export SIGSTORE_ID_TOKEN

if [ -z "${HOMEBREW_TAP_GITHUB_TOKEN:-}" ]; then
	echo "release: HOMEBREW_TAP_GITHUB_TOKEN is not set; the Homebrew cask upload will be skipped" >&2
fi

goreleaser release --clean

cosign verify-blob \
	--bundle dist/checksums.txt.sigstore.json \
	--certificate-identity "$CERT_IDENTITY" \
	--certificate-oidc-issuer "$OIDC_ISSUER" \
	dist/checksums.txt
