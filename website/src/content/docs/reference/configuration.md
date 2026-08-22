---
title: Configuration File Schemas
description: The operator-owned project.yml and policy.yml fields.
---

## A caller-supplied config is not operator policy

SafeLane reads these files from SAFELANE_HOME (default ~/.safelane). Keep them outside the application checkout.

### project.yml

    version: 4
    application: safelane-demo-api
    repository:
      name: AndrewMaged814/safelane-demo-api
      default_branch: main
    release:
      environment: production
      image_repository: ghcr.io/andrewmaged814/safelane-demo-api
      image_tag: sha-{{merge_sha}}
      required_checks:
        - Publish image
        - Test
      template_path: release-template
    target:
      cluster: safelane-demo
      namespace: safelane-demo-api
      rollout: safelane-demo-api
    analysis:
      probe_image: ghcr.io/andrewmaged814/safelane-demo-probe@sha256:<digest>
      assertions:
        - id: demo-response
          surface: GET /api/demo
          expectation: HTTP 200 and JSON status equals "ok"
          covers: correctness
    controller_kubeconfig: controller.kubeconfig
    controller_context: safelane-controller

Version 4 requires a digest-pinned external probe and concrete runtime assertions. `required_checks` names every mandatory static GitHub check; setup excludes dynamic matrix templates because GitHub expands them into different check-run names. Application, environment, cluster, and namespace must be lowercase DNS labels. `repository.name` is `owner/name`. The image repository includes a registry.

### policy.yml

    version: 2
    mandatory_evidence:
      - merged_commit_on_default_branch
      - passing_publish_workflow
      - immutable_ghcr_digest
    lanes:
      fast: { weights: [50, 100] }
      standard: { weights: [25, 50, 100] }
      guarded: { weights: [25, 50, 75, 100] }
    risk_to_lane: { low: fast, medium: standard, high: guarded }
    default_lane: guarded

The policy also contains assessment.heuristic / assessment.model settings. No caller may supply or override these fields.

## Next

- [The Release Policy](../concepts/release-policy/)
- [Setting Up an Application](../guides/setting-up/)
- [Release Record Schema](./release-record/)

