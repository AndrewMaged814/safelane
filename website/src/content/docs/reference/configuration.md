---
title: Configuration File Schemas
description: The operator-owned project.yml and policy.yml fields.
---

## A caller-supplied config is not operator policy

SafeLane reads these files from SAFELANE_HOME (default ~/.safelane). Keep them outside the application checkout.

### project.yml

    version: 3
    application: podinfo
    repository:
      name: AndrewMaged814/podinfo
      default_branch: main
    release:
      environment: production
      image_repository: ghcr.io/andrewmaged814/podinfo
      image_tag: sha-{{merge_sha}}
      required_checks:
        - build-and-push
        - test
      template_path: release-template
    target:
      cluster: safelane-demo
      namespace: podinfo
      rollout: podinfo
    controller_kubeconfig: controller.kubeconfig
    controller_context: safelane-controller

version must be 1, 2, or 3. Version 3 uses required_checks to name every mandatory check. application, environment, cluster, and namespace must be lowercase DNS labels. repository.name is owner/name. The image repository includes a registry.

### policy.yml

    version: 2
    mandatory_evidence:
      - merged_commit_on_default_branch
      - passing_publish_workflow
      - immutable_ghcr_digest
    lanes:
      fast: { weights: [5, 100] }
      standard: { weights: [5, 25, 50, 100] }
      guarded: { weights: [1, 5, 25, 50, 100] }
    risk_to_lane: { low: fast, medium: standard, high: guarded }
    default_lane: guarded

The policy also contains independent_pr_approval and assessment.heuristic / assessment.model settings. No caller may supply or override these fields.

## Next

- [The Release Policy](../concepts/release-policy/)
- [Setting Up an Application](../guides/setting-up/)
- [Release Record Schema](./release-record/)

