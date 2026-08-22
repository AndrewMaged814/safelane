#!/usr/bin/env bash
# One command: an empty-ish minikube to a cluster SafeLane can release into.
#
#   ./cluster/install.sh
#
# Idempotent -- safe to re-run. Each stage is also runnable on its own if you
# need to redo just one part.
#
# What it leaves behind:
#   ingress-nginx            real traffic weights, not replica approximation
#   argo-rollouts            the controller that executes the canary
#   monitoring/prometheus    the analysis provider, 5s scrape, endpoint-role
#                            discovery so the `service` label exists
#   monitoring/grafana       the canary dashboard, anonymous read
#   podinfo/                 the demo app at a healthy baseline, plus the two
#                            SafeLane identities and a load generator
#
# The identity stage rewrites your default kubeconfig context so the agent runs
# as a ServiceAccount that cannot patch rollouts. Your previous context is
# preserved as `safelane-admin` and the file is backed up first.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PATH="${HOME}/.local/bin:${PATH}"

stage() { printf '\n\033[1;36m######## %s ########\033[0m\n' "$1"; }

# Every stage before the last one creates objects, so this must run with a
# cluster-admin context. A completed previous run leaves the default context as
# safelane-caller, which may only read rollouts -- re-running from there used to
# fail at stage 1 with a Forbidden that minikube reported as an addon error.
require_admin() {
  if kubectl config get-contexts -o name | grep -Fxq safelane-admin; then
    kubectl config use-context safelane-admin >/dev/null
  fi
  if [ "$(kubectl auth can-i '*' '*' 2>/dev/null)" != "yes" ]; then
    echo "setup needs a cluster-admin context; current is $(kubectl config current-context)." >&2
    echo "Switch to one (kubectl config use-context safelane-admin) and retry." >&2
    exit 1
  fi
  echo "running as $(kubectl config current-context)"
}

require_admin

stage "1/5  cluster, ingress, Argo Rollouts"
bash "${HERE}/10-cluster.sh"

stage "2/5  Prometheus and Grafana"
bash "${HERE}/20-monitoring.sh"

stage "3/5  podinfo baseline"
bash "${HERE}/30-baseline.sh"

stage "4/5  load generator"
bash "${HERE}/40-loadgen.sh"

# Last on purpose: this stage drops the default kubeconfig context to an
# identity that may only read rollouts. Anything that still needs to create
# objects has to run before it.
stage "5/5  SafeLane identities"
bash "${HERE}/50-identities.sh"

cat <<'DONE'

######## cluster ready ########

Verify:
  kubectl --context safelane-admin get rollout podinfo -n podinfo
  kubectl auth can-i patch rollouts --context safelane-caller -n podinfo   # no

Watch:
  kubectl argo rollouts dashboard        http://localhost:3100
  kubectl port-forward -n monitoring svc/grafana 3000:3000
                                         http://localhost:3000/d/safelane-rollout

Between rehearsals:
  ./cluster/reset.sh

Your default kubectl context is now safelane-caller, which can read rollouts
and nothing else. For ordinary cluster work use --context safelane-admin.
DONE
