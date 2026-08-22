# Shared fixtures

## `release-request.json`

The caller-facing Release Request fixture. This is the file the CLI reads:

```text
safelane release --file testdata/release-request.json
```

It carries identifiers and intent only: repository, pull request, and environment.
Every evidence field — merge SHA, check run, digest, cluster, caller
identity, request id — is collected by SafeLane from GitHub, GHCR, and
`.safelane/project.yml`. The agent must not author those claims.

## `project.yml`

Operator-owned runtime configuration used by tests. A real application
repository gets the same skill from `safelane setup`.

## Release Template fixture

The Release Template fixture lives with the renderer, at
`internal/render/testdata/release-template/`. `safelane setup` writes it into
`.safelane/release-template/` when that directory does not exist. Ahmed owns
the real template; swapping it in is a `template_path` change in project.yml.
