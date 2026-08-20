package execute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// Capabilities is the live authorization answer shared by doctor and the
// Release Record boundary assertion.
type Capabilities struct {
	ControllerPatchRollouts bool
	CallerGetRollouts       bool
	CallerPatchRollouts     bool
}

// AssertCapabilities asks Kubernetes authorization about both SafeLane identities.
func (e *Executor) AssertCapabilities(ctx context.Context, controllerIdentity, callerIdentity string) (Capabilities, error) {
	controllerPatch, err := e.canCaller(ctx, controllerIdentity, "patch")
	if err != nil {
		return Capabilities{}, err
	}
	callerGet, err := e.canCaller(ctx, callerIdentity, "get")
	if err != nil {
		return Capabilities{}, err
	}
	callerPatch, err := e.canCaller(ctx, callerIdentity, "patch")
	if err != nil {
		return Capabilities{}, err
	}
	return Capabilities{
		ControllerPatchRollouts: controllerPatch,
		CallerGetRollouts:       callerGet,
		CallerPatchRollouts:     callerPatch,
	}, nil
}

// AssertBoundary asks Kubernetes authorization about both identities at the release
// boundary. The checks share one timestamp and are executed immediately before
// rollout start by the start orchestration.
func (e *Executor) AssertBoundary(ctx context.Context, controllerIdentity, callerIdentity string) (release.Boundary, error) {
	capabilities, err := e.AssertCapabilities(ctx, controllerIdentity, callerIdentity)
	if err != nil {
		return release.Boundary{}, err
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	boundary := release.Boundary{
		ControllerIdentity: controllerIdentity,
		CallerIdentity:     callerIdentity,
		CallerCapability: release.CallerCapability{
			AssertedAt: now.UTC(), Method: "SubjectAccessReview",
			GetRollouts: capabilities.CallerGetRollouts, PatchRollouts: capabilities.CallerPatchRollouts,
		},
	}
	if err := boundary.Validate(); err != nil {
		return release.Boundary{}, err
	}
	return boundary, nil
}

func (e *Executor) canCaller(ctx context.Context, identity, verb string) (bool, error) {
	args := make([]string, 0, 12)
	if e.ControllerKubeconfig != "" {
		args = append(args, "--kubeconfig", e.ControllerKubeconfig)
	}
	if e.ControllerContext != "" {
		args = append(args, "--context", e.ControllerContext)
	}
	args = append(args, "auth", "can-i", verb, "rollouts.argoproj.io", "--namespace", e.Namespace, "--as", identity)
	out, err := e.Run(ctx, args, nil)
	if err != nil {
		return false, fmt.Errorf("assert caller capability %s rollouts: %w", verb, err)
	}
	switch strings.TrimSpace(string(out)) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("assert caller capability %s rollouts: unexpected kubectl output %q", verb, strings.TrimSpace(string(out)))
	}
}
