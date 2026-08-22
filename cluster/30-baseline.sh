#!/usr/bin/env bash
# Seeds (or resets) the podinfo namespace to a Healthy Rollout at an older
# image, so the next `rollout start` has a real stable version to canary
# against. Without this, Argo Rollouts treats the Rollout's first-ever
# apply as having no prior version to compare to and skips every canary
# step, going straight to weight 100 -- confirmed live against this exact
# cluster while answering PLAN.md's V4.
#
# Safe to run twice: it deletes and recreates only the Rollout object (the
# one resource whose history matters), so every re-run replays that same
# "first apply, no history" path deterministically. It assumes the
# one-time cluster setup -- kind, ingress-nginx, the Argo Rollouts
# controller, Prometheus, the load generator -- already exists; see
# PLAN.md's V3/V4 answers for what that setup looks like and why.
set -euo pipefail

NAMESPACE=podinfo
ROLLOUT=podinfo

# The oldest of the three images published from AndrewMaged814/podinfo
# (tag sha-56bab3c41a6f2e0a7838d7d937f8fe9ecb34af78, PR #1). Verified
# public and pullable on 19 Aug 2026. Pinned by digest, not tag, for the
# same reason SafeLane itself never trusts a mutable tag.
BASELINE_DIGEST="sha256:99f5be634b8215dc0dff8c9e1c853da1fd4b7fffdb23181afcd9fe8e39cbaf3d"
BASELINE_IMAGE="ghcr.io/andrewmaged814/podinfo@${BASELINE_DIGEST}"

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# The stable/canary Services, AnalysisTemplate and Ingress are idempotent:
# re-applying identical content is a no-op. Only the Rollout is deleted
# and recreated below, because its *history* -- not just its spec -- is
# what determines whether the next apply skips canary steps.
kubectl apply -n "${NAMESPACE}" -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: podinfo-stable
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: podinfo
    app.kubernetes.io/part-of: safelane
    safelane.dev/environment: production
    safelane.dev/service-role: stable
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: podinfo
  ports:
    - name: http
      protocol: TCP
      port: 80
      targetPort: http
---
apiVersion: v1
kind: Service
metadata:
  name: podinfo-canary
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: podinfo
    app.kubernetes.io/part-of: safelane
    safelane.dev/environment: production
    safelane.dev/service-role: canary
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: podinfo
  ports:
    - name: http
      protocol: TCP
      port: 80
      targetPort: http
---
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: podinfo-success-rate
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: podinfo
    app.kubernetes.io/part-of: safelane
spec:
  args:
    - name: service-name
  metrics:
    - name: request-success-rate
      interval: 30s
      # No `count`, and a longer initialDelay. Both were measured live on
      # 2026-08-22 against a genuinely broken canary (podinfo PR #4, which
      # returns HTTP 500 on every request) that this analysis passed:
      #
      #   14:03:39  value=1        canary pod did not exist yet
      #   14:04:09  value=1        still no canary
      #   14:04:39  value=0.9978   canary 11s old; the rate window was still
      #                            almost entirely stable traffic
      #   -> count: 3 satisfied -> Successful -> never measured again
      #
      # Two separate faults. `count: 3` ended the background analysis 90s in,
      # so weights 25, 50 and 100 rolled out completely unanalysed. And the
      # canary Service's endpoints only flip to the canary pod once it is
      # Ready, so a rate window straddling that flip still contains stable
      # traffic recorded under service="<canary>" -- which is why the one
      # post-canary reading came in just above the 0.99 threshold.
      #
      # Omitting `count` keeps a background analysis measuring for the whole
      # rollout, which is what "background" is for. initialDelay 60s with a
      # 30s window puts the first reading a full window clear of the flip.
      initialDelay: 60s
      # No-traffic must not mean healthy: an empty Prometheus result no
      # longer satisfies this condition (PLAN.md V3).
      successCondition: len(result) > 0 && result[0] >= 0.99
      failureLimit: 1
      provider:
        prometheus:
          address: http://prometheus.monitoring.svc.cluster.local:9090
          query: >-
            sum(rate(http_requests_total{namespace="${NAMESPACE}",service="podinfo-canary",status!~"5.."}[30s]))
            /
            sum(rate(http_requests_total{namespace="${NAMESPACE}",service="podinfo-canary"}[30s]))
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: podinfo
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: podinfo
    app.kubernetes.io/part-of: safelane
spec:
  ingressClassName: nginx
  rules:
    - host: podinfo.production.svc.cluster.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: podinfo-stable
                port:
                  name: http
EOF

echo "Applying podinfo Rollout at ${BASELINE_IMAGE}"

kubectl delete rollout "${ROLLOUT}" -n "${NAMESPACE}" --ignore-not-found --wait=true

kubectl apply -n "${NAMESPACE}" -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: ${ROLLOUT}
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: podinfo
    app.kubernetes.io/part-of: safelane
    safelane.dev/environment: production
spec:
  replicas: 4
  revisionHistoryLimit: 5
  selector:
    matchLabels:
      app.kubernetes.io/name: podinfo
  strategy:
    canary:
      canaryService: podinfo-canary
      stableService: podinfo-stable
      trafficRouting:
        nginx:
          stableIngress: podinfo
      analysis:
        templates:
          - templateName: podinfo-success-rate
        args:
          - name: service-name
            value: podinfo-canary
      steps:
        - setWeight: 5
        - pause:
            duration: 60s
        - setWeight: 25
        - pause:
            duration: 60s
        - setWeight: 50
        - pause:
            duration: 60s
  template:
    metadata:
      labels:
        app.kubernetes.io/name: podinfo
        app.kubernetes.io/part-of: safelane
    spec:
      containers:
        - name: podinfo
          image: ${BASELINE_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              protocol: TCP
              containerPort: 9898
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 500m
              memory: 256Mi
EOF

echo "Waiting for Healthy…"
kubectl argo rollouts status "${ROLLOUT}" -n "${NAMESPACE}" --timeout=120s

echo "Baseline seeded. SafeLane will canary against this version."
