#!/usr/bin/env bash
# GoReleaser sign command: keyless-sign ARTIFACT into the Sigstore BUNDLE,
# then verify the bundle with exactly the identity the installers enforce.
# GoReleaser publishes nothing until every sign command succeeds, so a
# certificate for any other identity fails the release before it is public.
set -euo pipefail

if [ "$#" -ne 2 ] || [ -z "$1" ] || [ -z "$2" ]; then
	echo "usage: sign-and-verify.sh ARTIFACT BUNDLE" >&2
	exit 2
fi
artifact=$1
bundle=$2

# shellcheck source=.circleci/scripts/release-identity.sh
. "$(dirname "${BASH_SOURCE[0]}")/release-identity.sh"

cosign sign-blob --yes --bundle="$bundle" "$artifact"
cosign verify-blob \
	--bundle "$bundle" \
	--certificate-identity "$CERT_IDENTITY" \
	--certificate-oidc-issuer "$OIDC_ISSUER" \
	"$artifact"
