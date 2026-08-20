import { defineConfig } from "astro/config";
import mermaid from "astro-mermaid";
import starlight from "@astrojs/starlight";

export default defineConfig({
  site: "https://andrewmaged814.github.io",
  base: "/safelane",
  integrations: [
    mermaid({ enableLog: false }),
    starlight({
      title: "SafeLane",
      customCss: ["./src/styles/custom.css"],
      social: {
        github: "https://github.com/AndrewMaged814/safelane"
      },
      sidebar: [
        { label: "Start Here", items: [
          { label: "Introduction", slug: "start-here/introduction" },
          { label: "Quick Start", slug: "start-here/quick-start" },
          { label: "Installation", slug: "start-here/installation" }
        ] },
        { label: "Concepts", items: [
          { label: "The Release Policy", slug: "concepts/release-policy" },
          { label: "Assessment", slug: "concepts/assessment" },
          { label: "The Boundary", slug: "concepts/boundary" },
          { label: "The Release Record & Proof", slug: "concepts/record-and-proof" }
        ] },
        { label: "Guides", items: [
          { label: "Setting Up an Application", slug: "guides/setting-up" },
          { label: "Running a Release End to End", slug: "guides/release-end-to-end" },
          { label: "Handling a Paused or Aborted Rollout", slug: "guides/rollout-recovery" },
          { label: "Pre-flight Checks", slug: "guides/pre-flight" },
          { label: "Installing the Agent Skill", slug: "guides/agent-skill" }
        ] },
        { label: "Reference", items: [
          { label: "CLI Command Reference", slug: "reference/cli" },
          { label: "Configuration File Schemas", slug: "reference/configuration" },
          { label: "Exit Codes", slug: "reference/exit-codes" },
          { label: "Release Record Schema", slug: "reference/release-record" }
        ] }
      ]
    })
  ]
});
