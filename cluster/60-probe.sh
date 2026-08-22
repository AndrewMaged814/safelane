#!/usr/bin/env bash
# Pins the analysis probe to an immutable digest in the operator configuration.
#
# `safelane setup` writes a literal placeholder for this image:
#
#   probe_image: ghcr.io/<owner>/<probe>@sha256:REPLACE_WITH_PUBLISHED_DIGEST
#
# Something has to resolve the real digest and substitute it. The only code that
# ever did was `safelane demo up`, which was removed -- so `safelane doctor`
# reports "analysis probe not pinned by an immutable OCI digest" and the release
# cannot run. This stage does that substitution.
#
# It is the last stage because it edits operator configuration rather than the
# cluster, and it is a no-op for apps that declare no probe.
#
# The proper home for this is `safelane setup` itself: the command that writes
# the placeholder should be the one that fills it. Until it does, this keeps the
# configuration usable.
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

if [ -z "${PROBE_IMAGE_REPO:-}" ]; then
  echo "app ${SAFELANE_APP} declares no analysis probe; nothing to pin"
  exit 0
fi

PROJECT="${SAFELANE_HOME:-${HOME}/.safelane}/apps/${SAFELANE_APP}/project.yml"
if [ ! -f "${PROJECT}" ]; then
  echo "no operator configuration at ${PROJECT}; run 'safelane setup' first" >&2
  exit 1
fi

PLACEHOLDER="ghcr.io/${PROBE_IMAGE_REPO}@sha256:REPLACE_WITH_PUBLISHED_DIGEST"
if ! grep -qF "${PLACEHOLDER}" "${PROJECT}"; then
  current="$(grep -E '^\s*probe_image:' "${PROJECT}" | awk '{print $2}')"
  echo "probe already pinned: ${current##*@}"
  exit 0
fi

DIGEST="$(resolve_digest "${PROBE_IMAGE_REPO}" "${PROBE_TAG:-latest}")"
if [ -z "${DIGEST}" ]; then
  echo "could not resolve a digest for ${PROBE_IMAGE_REPO}:${PROBE_TAG:-latest}" >&2
  exit 1
fi

# Rewrite atomically: a half-written project.yml is worse than an unpinned one.
tmp="$(mktemp "${PROJECT}.XXXXXX")"
sed "s|${PLACEHOLDER}|ghcr.io/${PROBE_IMAGE_REPO}@${DIGEST}|" "${PROJECT}" > "${tmp}"
chmod 600 "${tmp}"
mv -f "${tmp}" "${PROJECT}"
echo "probe pinned: ${DIGEST}"
