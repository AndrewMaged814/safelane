#!/usr/bin/env bash
# Put the cluster back to a clean baseline between rehearsals.
#
# Re-seeds the Rollout at the old digest and clears the persisted Release
# records, so the next release attempt starts from nothing. It does not
# touch the cluster, monitoring or identities -- only the state a rehearsal
# dirties.
set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
HERE="${CLUSTER_DIR}"
APP="${SAFELANE_APP}"
RECORDS="${SAFELANE_HOME:-${HOME}/.safelane}/apps/${APP}/releases"

# Seeding needs write access; the caller identity deliberately has none.
PREVIOUS="$(kubectl config current-context)"
restore() { kubectl config use-context "${PREVIOUS}" >/dev/null 2>&1 || true; }
trap restore EXIT
kubectl config use-context safelane-admin >/dev/null

bash "${HERE}/30-baseline.sh"

if [ -d "${RECORDS}" ]; then
  count=$(find "${RECORDS}" -name '*.json' | wc -l)
  find "${RECORDS}" -name '*.json' -delete
  echo "cleared ${count} release record(s) from ${RECORDS}"
fi

echo "reset complete; context restored to ${PREVIOUS}"
