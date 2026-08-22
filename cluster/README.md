# cluster

Everything SafeLane needs on the target cluster, in one command.

```bash
./cluster/install.sh
```

SafeLane itself does not provision clusters — it verifies evidence, decides a
lane, renders manifests and coordinates Argo. This folder is the other half:
the infrastructure workstream's environment, reproducible.

## Stages

| | Script | Leaves behind |
|---|---|---|
| 1 | `10-cluster.sh` | minikube running, `ingress-nginx`, Argo Rollouts (pinned) |
| 2 | `20-monitoring.sh` | Prometheus and Grafana via Helm |
| 3 | `30-baseline.sh` | `podinfo` at a healthy baseline digest, Services, AnalysisTemplate, Ingress |
| 4 | `40-loadgen.sh` | constant traffic through the ingress |
| 5 | `50-identities.sh` | `safelane-caller` and `safelane-controller` ServiceAccounts |

Every stage is idempotent and independently runnable. Identities run **last**:
that stage drops the default context to an identity that may only read
rollouts, so anything which still needs to create objects must precede it.

```bash
./cluster/reset.sh     # re-seed the baseline and clear release records
```

## Three things that are easy to get wrong

**Prometheus is not optional.** The AnalysisTemplate queries it, and an empty
result is deliberately treated as a *failed* reading rather than a healthy one.
A cluster without a metrics provider aborts every canary, blaming the change.

**The load generator is not optional either**, for the same reason: no traffic
means no numerator and no denominator.

**The scrape config must discover endpoints, not pods.** The analysis query
scopes on `service="podinfo-canary"`, and only endpoint-role discovery knows
which Service currently selects a pod — Argo distinguishes canary from stable
by pointing each Service's *selector* at a pod-template-hash, never by
labelling the pod. Pod-role discovery scrapes perfectly happily and produces a
`service`-less series that silently never matches. See
`prometheus-values.yaml`.

## Identities

`50-identities.sh` rewrites your default kubeconfig context so the agent runs
as `safelane-caller`, which may read rollouts and nothing else. Your original
context is preserved as `safelane-admin` and the kubeconfig is backed up with a
timestamp first.

```bash
kubectl auth can-i patch rollouts --context safelane-caller -n podinfo   # no
kubectl --context safelane-admin get pods -n podinfo                     # ordinary work
```

That denial is enforced by Kubernetes, not by SafeLane — it holds even if
SafeLane is bypassed entirely.

## Configuration

| Variable | Default |
|---|---|
| `MINIKUBE_PROFILE` | `minikube` |
| `ARGO_ROLLOUTS_VERSION` | `v1.9.1` |
| `PROM_CHART_VERSION` | `29.23.0` |
| `GRAFANA_CHART_VERSION` | `10.5.15` |
| `SAFELANE_APP` | `podinfo` |

Pin Argo Rollouts and do not bump it casually: `ComputeStepHash` is not stable
across controller versions, and a change there can reset `currentStepIndex`
mid-rollout.
