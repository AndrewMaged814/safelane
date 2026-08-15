# SafeLane demo application candidates

**Status:** research note, not an implementation design  
**Research date:** 2026-08-15  
**Goal:** choose an existing public application repository for one convincing SafeLane happy-path demonstration.

## Recommendation

Use **[`argoproj/rollouts-demo`](https://github.com/argoproj/rollouts-demo)** for the first end-to-end demo, with one explicit caveat: its repository is an Argo demonstration app rather than a test-heavy production sample. It is the best fit for a hackathon because the repository already contains the Rollout examples, Dockerfile, canary-analysis example, and deliberately good/bad/slow image variants. A visible color change makes the canary easy to understand, while `bad-*` and `slow-*` variants give deterministic failure cases without inventing application behavior.

Use **[`stefanprodan/podinfo`](https://github.com/stefanprodan/podinfo)** as the stronger fallback if the demo needs real application tests, health/readiness signals, metrics, fault injection, image provenance, or a more realistic “reviewed change” artifact. It has the best operational surface but requires adapting its Deployment/Helm output to an Argo `Rollout`.

Do not start with **[`GoogleCloudPlatform/microservices-demo`](https://github.com/GoogleCloudPlatform/microservices-demo)**. It is credible and richly tested, but its 11-service topology creates too much setup and too many independent failure modes for the first SafeLane proof.

## Requirements check

| Candidate | Dockerfile | Tests | Simple deployment | Observable health signals | Small visible change | Fit |
| --- | --- | --- | --- | --- | --- | --- |
| `argoproj/rollouts-demo` | Yes; small Go image build | Weak as application tests; repository has Make targets and rollout/analysis examples | Excellent; Argo repo gives ready-made Kustomize examples | `/color`, HTTP behavior, configurable error rate/latency; Argo analysis examples | Excellent; switch color image, or use `bad-*`/`slow-*` | **Best first demo** |
| `stefanprodan/podinfo` | Yes; multi-stage Go Dockerfile | Strong; `go test ./...`, Helm tests, Kind E2E, CI validation | Good; Helm or Kustomize install, but Rollout adaptation required | `/healthz`, `/readyz`, `/metrics`, `/version`, fault injection, structured logs | Good; UI message/color/version and controlled faults | **Best robust fallback** |
| `GoogleCloudPlatform/microservices-demo` | Yes; Dockerfile per service | Strong; service unit tests and CI/deploy tests | Moderate; one manifest applies all services, but cluster needs are substantial | Frontend HTTP probes, gRPC probes across services, load generator | Good; `FRONTEND_MESSAGE` is already an explicit visible customization | Scope too large for first proof |

## 1. `argoproj/rollouts-demo` — recommended

### Evidence

The repository identifies itself as the Argo Rollouts demo application and explicitly includes examples for canary, blue-green, canary analysis, experiments, preview-stack testing, and Istio traffic splitting ([README](https://github.com/argoproj/rollouts-demo/blob/master/README.md)). The README's happy path is already the shape SafeLane needs: apply an example, watch the Rollout, and trigger an update with `kubectl argo rollouts set image`.

The repository contains a compact Dockerfile that builds the Go app into a `scratch` image and exposes build arguments for `COLOR`, `ERROR_RATE`, and `LATENCY` ([Dockerfile](https://github.com/argoproj/rollouts-demo/blob/master/Dockerfile)). The application serves a visual page and a `/color` endpoint; its source reads the color and failure/latency settings from environment variables ([main.go](https://github.com/argoproj/rollouts-demo/blob/master/main.go)).

The README documents prebuilt image variants:

- normal colors: `red`, `orange`, `yellow`, `green`, `blue`, `purple`;
- high-error variants: `bad-yellow`, etc.;
- high-latency variants: `slow-yellow`, etc.

These variants are unusually useful for SafeLane: the “good” release is visually obvious and the “unsafe” release can fail a runtime probe predictably. The repository also includes an analysis example using a Prometheus metric provider ([analysis Rollout](https://github.com/argoproj/rollouts-demo/blob/master/examples/analysis/rollout-with-analysis.yaml)) and a canary example with explicit 20/40/60/80 weight steps ([canary Rollout](https://github.com/argoproj/rollouts-demo/blob/master/examples/canary/canary-rollout.yaml)).

### Why it fits the happy path

The demo can tell this story in under a minute:

1. Blue is stable.
2. A reviewed change requests yellow; the page visibly changes.
3. SafeLane starts a canary at the permitted initial weight.
4. An unsafe request tries an aggressive weight or `bad-yellow` image.
5. Admission rejects the unsafe request with a correction.
6. The planner submits the corrected typed transition.
7. Argo pauses/progresses and the existing analysis detects health.
8. SafeLane records the artifact and observed release outcome.

The repository's own Getting Started flow confirms that Argo can pause at a canary step, promote, and abort/return to the stable version ([Argo Rollouts getting started](https://argoproj.github.io/argo-rollouts/getting-started/)).

### Caveats

- The repo does not present a conventional application unit-test suite; its Makefile has build, run, image, lint, and release targets but no meaningful `go test` target ([Makefile](https://github.com/argoproj/rollouts-demo/blob/master/Makefile)). Treat Argo's analysis and rollout progression as the integration test for this demo, or add tests only in the SafeLane harness.
- The base example uses normal Kubernetes Service balancing, so small weights are approximate replica ratios. The Argo docs explain that finer-grained traffic percentages require an ingress controller or service mesh ([traffic-shaping note](https://argoproj.github.io/argo-rollouts/getting-started/)). For a 20% first canary this is acceptable; use a real traffic router only if the proof depends on exact percentages.
- The image tags are mutable references. SafeLane must pin the exact built image digest in its request and should not use a tag as evidence of identity.
- The app does not expose the same ready-made `/healthz` and `/metrics` surface as Podinfo. The analysis path may need an HTTP or Prometheus adapter around `/color`, or the demo can use the existing rollout readiness plus a synthetic metric.

### Demo change recommendation

Use a normal color change as the reviewed release and reserve `bad-*` or `slow-*` for the intentionally unsafe branch. The visual difference makes the product story legible without explaining business logic. Do not modify the upstream repo; build a pinned image from a small fork or a local commit and record its digest.

## 2. `stefanprodan/podinfo` — robust fallback

### Evidence

Podinfo is a small Go web application explicitly designed to showcase Kubernetes operational practices. Its README lists health checks, Prometheus/OpenTelemetry instrumentation, fault injection, Helm/Kustomize installers, Kind/Helm end-to-end testing, and signed images with SBOM/SLSA provenance ([repository README](https://github.com/stefanprodan/podinfo/blob/master/README.md)). It includes a multi-stage Dockerfile that embeds the build revision in the binary ([Dockerfile](https://github.com/stefanprodan/podinfo/blob/master/Dockerfile)).

The application has excellent probe and observation surfaces:

- `GET /healthz` for liveness;
- `GET /readyz` for readiness;
- `GET /metrics` for Prometheus;
- `GET /version` with version and Git commit;
- `POST /fault_injection/enable` to produce HTTP 500 responses while probes remain healthy;
- configurable random error and latency behavior.

Those endpoints are documented in the README and wired into the Helm deployment template ([API and health endpoints](https://github.com/stefanprodan/podinfo/blob/master/README.md), [deployment probes](https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/deployment.yaml)). The chart has a Helm test hook that curls the service and checks for a version response ([service test](https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/tests/service.yaml)). The Makefile runs `go test ./...`, and its CI workflow runs unit tests plus Helm/Kustomize/Timoni validation ([Makefile](https://github.com/stefanprodan/podinfo/blob/master/Makefile), [test workflow](https://github.com/stefanprodan/podinfo/blob/master/.github/workflows/test.yml)).

Deployment is straightforward with Helm or Kustomize; the repository documents both and includes local Kind deployment instructions ([install and deploy](https://github.com/stefanprodan/podinfo/blob/master/README.md), [deploy README](https://github.com/stefanprodan/podinfo/blob/master/deploy/README.md)).

### Why it fits SafeLane

Podinfo is the better test of the actual SafeLane hypothesis because it supplies trustworthy evidence inputs without SafeLane inventing them: a Git revision in `/version`, health endpoints, Prometheus metrics, application logs, and existing image provenance. A small reviewed change can alter the UI message or version, while the fault-injection endpoint can create a clear canary failure.

### Caveats

- The upstream deployment is a Deployment/Helm application, not an Argo Rollout. SafeLane must render or adapt the chart into an operator-owned Rollout template and ensure Services/selectors are compatible.
- The repository is much larger than `rollouts-demo` and supports several installers, optional Redis/backend behavior, and many APIs. Restrict the prototype to one frontend instance and its health/metrics endpoints.
- Fault injection is an operational control, not a code change. Use it only to validate abort behavior; keep the “reviewed change” branch as a small source/UI change so artifact identity remains meaningful.

### Demo change recommendation

Prefer a small legitimate build-information improvement over demo branding: add a visible version/commit badge to the homepage, backed by the existing build metadata and `/version` response. Keep `/healthz`, `/readyz`, and `/metrics` unchanged. For the bad branch, enable controlled error/latency injection in the canary or introduce a deliberately failing response path in the demo fork, then verify that analysis aborts while readiness alone would not be sufficient.

## 3. `GoogleCloudPlatform/microservices-demo` — credible but out of scope

### Evidence

Online Boutique is a real, user-visible e-commerce application with 10/11 services, Kubernetes manifests, Helm and Kustomize options, and a documented `kubectl apply -f ./release/kubernetes-manifests.yaml` deployment path ([repository README](https://github.com/GoogleCloudPlatform/microservices-demo)). The repository contains a Dockerfile for each service, service unit tests, Kustomize tests, and GitHub Actions workflows that run code tests and deploy/readiness tests ([source tree](https://github.com/GoogleCloudPlatform/microservices-demo/tree/main/src), [workflow documentation](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/.github/workflows/README.md)).

The release manifest wires readiness and liveness probes for the frontend and gRPC probes for multiple backend services ([release manifest](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/release/kubernetes-manifests.yaml)). The frontend manifest already exposes an explicit `FRONTEND_MESSAGE` customization in the environment, which is a fast user-visible change ([frontend manifest](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/kubernetes-manifests/frontend.yaml)).

### Why it is not the first choice

It is too large for the first SafeLane proof. A canary of the frontend alone still depends on the other services, the load generator, multiple images, and a larger telemetry surface. A canary of the full graph introduces many possible causes for a failed analysis, obscuring whether SafeLane enforced the release boundary. It also needs more registry/build time and more cluster resources than a local hackathon loop deserves.

### When to use it

Use Online Boutique only for a later credibility pass where the demo needs to show a business workflow such as browsing/cart/checkout and a cross-service runtime signal. Do not use it to validate the initial SafeLane architecture.

## Clear recommendation

Start with `argoproj/rollouts-demo` if the goal is the shortest convincing demonstration of SafeLane's release boundary. It minimizes application work and makes both the good path and bad canary visually obvious. Explicitly document its weak unit-test surface and use the SafeLane harness plus Argo AnalysisRun as the test boundary.

Choose `stefanprodan/podinfo` instead if exact artifact/evidence and runtime-probe credibility are more important than minimizing integration work. It is the strongest all-round application substrate, but the team must adapt its chart to Argo Rollouts before the demo.

Do not begin with Online Boutique. Its quality is an argument for a later production-like scenario, not for the first end-to-end proof.

## Sources

- [`argoproj/rollouts-demo` README](https://github.com/argoproj/rollouts-demo/blob/master/README.md)
- [`argoproj/rollouts-demo` Dockerfile](https://github.com/argoproj/rollouts-demo/blob/master/Dockerfile)
- [`argoproj/rollouts-demo` application source](https://github.com/argoproj/rollouts-demo/blob/master/main.go)
- [`argoproj/rollouts-demo` canary Rollout](https://github.com/argoproj/rollouts-demo/blob/master/examples/canary/canary-rollout.yaml)
- [`argoproj/rollouts-demo` analysis Rollout](https://github.com/argoproj/rollouts-demo/blob/master/examples/analysis/rollout-with-analysis.yaml)
- [`argoproj/rollouts-demo` Makefile](https://github.com/argoproj/rollouts-demo/blob/master/Makefile)
- [Argo Rollouts getting started](https://argoproj.github.io/argo-rollouts/getting-started/)
- [`stefanprodan/podinfo` README](https://github.com/stefanprodan/podinfo/blob/master/README.md)
- [`stefanprodan/podinfo` Dockerfile](https://github.com/stefanprodan/podinfo/blob/master/Dockerfile)
- [`stefanprodan/podinfo` Makefile](https://github.com/stefanprodan/podinfo/blob/master/Makefile)
- [`stefanprodan/podinfo` test workflow](https://github.com/stefanprodan/podinfo/blob/master/.github/workflows/test.yml)
- [`stefanprodan/podinfo` deployment template](https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/deployment.yaml)
- [`stefanprodan/podinfo` Helm service test](https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/tests/service.yaml)
- [`stefanprodan/podinfo` deploy README](https://github.com/stefanprodan/podinfo/blob/master/deploy/README.md)
- [`GoogleCloudPlatform/microservices-demo` README](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/README.md)
- [`GoogleCloudPlatform/microservices-demo` source tree](https://github.com/GoogleCloudPlatform/microservices-demo/tree/main/src)
- [`GoogleCloudPlatform/microservices-demo` workflow documentation](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/.github/workflows/README.md)
- [`GoogleCloudPlatform/microservices-demo` release manifest](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/release/kubernetes-manifests.yaml)
- [`GoogleCloudPlatform/microservices-demo` frontend manifest](https://github.com/GoogleCloudPlatform/microservices-demo/blob/main/kubernetes-manifests/frontend.yaml)
