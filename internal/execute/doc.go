// Package execute drives Argo Rollouts through kubectl: applying the
// Rendered Manifest Bundle SafeLane already hashed, reading the Rollout's
// status back, and waiting for it to reach a gate.
//
// Every kubectl invocation goes through one injected [Runner], so the
// whole package is unit-testable against canned output with no cluster
// (Appendix D).
package execute
