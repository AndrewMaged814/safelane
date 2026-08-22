# Release Template — fixture, and the contract a real one must satisfy

This directory is a **fixture**. Ahmed authors and validates the real Release Template
against the demo cluster (#47, "Release Template ownership"); SafeLane consumes it as
operator-owned configuration and never authors it. No caller may select or override it.

Swapping the real template in is a drop-in change:

```go
tmpl, err := render.LoadDir("/path/to/operator-owned/release-template")
bundle, err := render.Render(tmpl, target, verifiedEvidence)
```

Nothing else changes. `Render` is unaware of which directory it was handed.

> **Files in this directory are included in the template content digest** — all of them,
> including this README and `TEMPLATE`. A Release pins the template's exact content, so
> editing any file here changes the identity recorded on subsequent Releases. Keep prose
> out of a production template directory if you do not want that churn.

## Hard requirements for a real template

1. **One Kubernetes object per file, one file per object.** Resource files end in
   `.yaml.tmpl`. SafeLane hashes and identifies each file's output individually, so a
   file that renders two documents (a second `---`) is rejected.
2. **Every file must render a top-level `apiVersion`, a top-level `kind`, and
   `metadata.name`** at one level of indentation. SafeLane reads these back out of the
   rendered bytes to record resource identity; it fails rather than record a blank one.
3. **The pod template must pin the image to the verified digest**, using
   `{{ .ImageReference }}` (or `{{ .ImageDigest }}`). If the rendered bundle contains
   the verified digest nowhere, `Render` fails with `unpinned_template`. SafeLane will
   not record an unpinned bundle.
4. **Render order is the lexicographic order of file paths.** The numeric prefixes here
   (`10-`, `20-`, …) exist to make apply order explicit and stable.
5. **Nothing non-deterministic.** No template functions are registered at all, so
   `now`, `rand`, `uuid` and `env` are unavailable — a template referencing one fails to
   parse. Do not embed timestamps, generated names, or a release ID: rendering must be
   byte-reproducible from the template digest, the verified image digest and the target.
6. **`TEMPLATE` (optional)** holds `name:` and `version:` lines. An unrecognized key is
   an error rather than a silent drop. The content digest, not the version label, is
   what a Release pins.
7. **No Secrets, no credentials.** The bundle is hashed and recorded in Release Proof.

## Values SafeLane supplies

The full set is `render.TemplateData`. Every value is derived from the release target or
from *verified* evidence; none of it is caller free-form input, and none of it is
time-dependent or random.

| Placeholder | Example | Source |
| --- | --- | --- |
| `{{ .Application }}` | `safelane-demo-api` | target |
| `{{ .Environment }}` | `production` | target |
| `{{ .Cluster }}` | `safelane-demo` | target |
| `{{ .Namespace }}` | `safelane-demo-api` | target |
| `{{ .ImageReference }}` | `ghcr.io/andrewmaged814/safelane-demo-api@sha256:…` | **verified** artifact |
| `{{ .ImageRegistry }}` | `ghcr.io` | **verified** artifact |
| `{{ .ImageRepository }}` | `andrewmaged814/safelane-demo-api` | **verified** artifact |
| `{{ .ImageDigest }}` | `sha256:…` | **verified** artifact |
| `{{ .SourceRepository }}` | `AndrewMaged814/safelane-demo-api` | **verified** evidence |
| `{{ .SourceRevision }}` | merge commit SHA on the base branch | **verified** evidence |
| `{{ .SourceBranch }}` | `main` | **verified** evidence |
| `{{ .RolloutName }}` | `safelane-demo-api` | derived: `<application>` |
| `{{ .StableServiceName }}` | `safelane-demo-api-stable` | derived: `<application>-stable` |
| `{{ .CanaryServiceName }}` | `safelane-demo-api-canary` | derived: `<application>-canary` |
| `{{ .AnalysisTemplateName }}` | `safelane-demo-api-success-rate` | derived: `<application>-success-rate` |
| `{{ .IngressName }}` | `safelane-demo-api` | derived: `<application>` |

Every substituted value is validated against a strict character set before
interpolation. `text/template` does not escape its output, so an unvalidated namespace
or repository name would be a YAML-injection path into objects SafeLane is about to
apply.

## What this fixture contains

| File | Object | Note |
| --- | --- | --- |
| `10-service-stable.yaml.tmpl` | `Service` | stable service |
| `20-service-canary.yaml.tmpl` | `Service` | canary service |
| `30-analysistemplate.yaml.tmpl` | `AnalysisTemplate` | Prometheus success-rate metric |
| `35-ingress.yaml.tmpl` | `Ingress` | traffic-routing object referenced by the Rollout's nginx `stableIngress` |
| `40-rollout.yaml.tmpl` | `Rollout` | canary strategy, immutable-digest pod template |

## Fixture values that must be replaced

These are **fixture guesses about Ahmed's cluster**, not SafeLane decisions. Each must
be confirmed against the real environment before the 26 August demo — a rendered bundle
that does not match the pre-created Rollout (#55) will not apply:

- **Resource names.** The derived names above must match the pre-created `safelane-demo-api`
  Rollout, its `stable`/`canary` Services, and the Ingress. If Ahmed's names differ,
  change the derivation in `render.newTemplateData` — do not paper over it in the
  template.
- **`replicas: 4`** and the resource requests/limits in `40-rollout.yaml.tmpl`.
- **The canary step ladder** (`5 / 25 / 50` with 60s pauses; policy envelope includes `100` as the final promote). The operator's static envelope in
  `docs/policy/safelane-policy.yml` is the authority on allowed stages; the
  template's steps must be consistent with it. Evidence does not choose these stages.
- **`trafficRouting.nginx.stableIngress`** and the Ingress host
  (`<app>.<env>.svc.cluster.local`). If the demo cluster has no nginx ingress
  controller, drop `35-ingress.yaml.tmpl` and the `trafficRouting` block; Argo then
  approximates traffic by replica count, which Release Proof must label as an
  approximation rather than an exact percentage.
- **The Prometheus address and query** in `30-analysistemplate.yaml.tmpl`. Both assume a
  Prometheus in `monitoring` scraping `http_requests_total` with `namespace`/`service`
  labels. Replace with whatever the demo cluster actually runs, or drop the
  AnalysisTemplate.
- **`containerPort: 9898`** and the `/healthz` and `/readyz` paths are real SafeLane Demo API
  values and should not need changing.
