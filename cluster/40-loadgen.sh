#!/usr/bin/env bash
# Constant traffic, so rate() has a numerator and a denominator.
#
# Mandatory. If this is not running, every analysis measurement returns an
# empty result, which the successCondition treats as a failure -- so the
# rollout aborts and blames the canary for a missing load generator.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
kubectl apply -f "${HERE}/loadgen.yaml"
kubectl rollout status -n podinfo deploy/loadgen --timeout=180s
