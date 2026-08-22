#!/usr/bin/env bash
# Constant traffic, so rate() has a numerator and a denominator.
#
# Mandatory. Without it every analysis measurement returns an empty result,
# which the successCondition treats as a failure -- so the rollout aborts and
# blames the canary for a missing load generator.
#
# Two modes. `ingress`: hit ingress-nginx with an explicit Host header, because
# the Ingress host is not in-cluster resolvable DNS and hitting the Service
# directly would bypass nginx's traffic split entirely. `service`: no traffic
# router, so drive the stable Service and let kube-proxy spread the load.
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

if [ "${LOADGEN_MODE}" = "ingress" ]; then
  TARGET_URL="http://ingress-nginx-controller.ingress-nginx.svc.cluster.local/"
  HOST_HEADER="--header=\"Host: ${LOADGEN_HOST}\""
else
  TARGET_URL="http://${STABLE_SERVICE}.${NAMESPACE}.svc.cluster.local/"
  HOST_HEADER=""
fi

kubectl apply -n "${NAMESPACE}" -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: loadgen
  namespace: ${NAMESPACE}
  labels: {app.kubernetes.io/name: loadgen, app.kubernetes.io/part-of: safelane}
spec:
  replicas: 2
  selector:
    matchLabels: {app.kubernetes.io/name: loadgen}
  template:
    metadata:
      labels: {app.kubernetes.io/name: loadgen, app.kubernetes.io/part-of: safelane}
    spec:
      containers:
        - name: loadgen
          image: busybox:1.36
          imagePullPolicy: IfNotPresent
          command:
            - /bin/sh
            - -c
            - |
              while true; do
                wget -q -O /dev/null -T 2 ${HOST_HEADER} ${TARGET_URL} 2>/dev/null || true
              done
          resources:
            requests: {cpu: 50m, memory: 16Mi}
            limits:   {cpu: 300m, memory: 64Mi}
YAML
kubectl rollout status -n "${NAMESPACE}" deploy/loadgen --timeout=180s
