# shellcheck shell=bash
# Sourced by release.sh and sign-and-verify.sh: the keyless signing identity
# of this project's tag-only "release" pipeline definition. The installers
# (scripts/install.sh, scripts/install.ps1) accept exactly this identity.
# shellcheck disable=SC2034 # consumed by the scripts that source this file
CIRCLECI_PROJECT_ID=cc5f04e8-66e9-4f18-91b0-71c671674d14
# shellcheck disable=SC2034
RELEASE_PIPELINE_DEFINITION=b37a0ec3-4a25-4686-b480-6b4eda16099f
# shellcheck disable=SC2034
CERT_IDENTITY="https://circleci.com/api/v2/projects/${CIRCLECI_PROJECT_ID}/pipeline-definitions/${RELEASE_PIPELINE_DEFINITION}"
# shellcheck disable=SC2034
OIDC_ISSUER="https://oidc.circleci.com"
