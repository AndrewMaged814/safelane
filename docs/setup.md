# SafeLane setup

`setup` is the low-click entry point for a repository that has no SafeLane operator configuration.
Run it from the application repository:

```bash
safelane setup
```

## What happens

1. SafeLane reads the GitHub `origin`, default branch, bounded text snapshots of the repository,
   workflow job names, and any GHCR image reference it can find. Secrets, build output, and large or
   binary files are excluded.
2. SafeLane invokes Claude once with that snapshot. Claude has no tools and no session persistence;
   it can recommend data but cannot edit the repository, create credentials, or provision a cluster.
3. SafeLane validates the structured recommendation. The recommendation contains a project-specific
   `policy.yml` and operator-owned Release Template files. The three mandatory evidence requirements
   are preserved: `merged_commit_on_default_branch`, `passing_publish_workflow`, and
   `immutable_ghcr_digest`.
4. SafeLane shows one summary and asks `Apply this setup? [Y/n]`. Only an affirmative answer writes
   files under `~/.safelane/apps/<application>/`.

The setup command does not write `.safelane` files into the application repository. It also does not
deploy anything. It creates the operator project record, policy, Release Template directory, and
agent skill pointers needed by later release commands.

## Fallback and safety

If Claude is not installed, times out, or returns invalid data, SafeLane falls back to a conservative
proposal derived from the repository facts. The fallback still requires the same single approval. To
choose it without invoking Claude:

```bash
safelane setup --no-agent
```

An invalid or declined proposal writes nothing. Setup never overwrites an existing operator
`project.yml`; inspect or remove an old configuration deliberately before starting a new setup.

## Doctor after setup

`doctor` is intentionally deterministic and non-conversational:

```bash
safelane doctor
```

It validates the active `project.yml`, policy, Release Template, GitHub and GHCR access, and the
configured target identities. A successful setup therefore gives you a clear hand-off: approve the
recommendation once, then use `doctor` to see what external readiness still needs attention.

The lower-level command remains available for scripted or fully manual setup:

```bash
safelane init --app <application> --repo <owner/name>
```

`init` keeps its deterministic defaults; `setup` is the repository-aware, agent-assisted path.
