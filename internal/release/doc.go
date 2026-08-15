// Package release holds SafeLane's core release domain types: the caller-submitted
// Release Request, the SafeLane-verified Release Evidence, the SafeLane-rendered
// Rendered Manifest Bundle, and the persisted Release record that binds them together.
//
// # Claimed versus verified
//
// The package draws one hard line through the middle of itself.
//
//   - Everything reachable from [ReleaseRequest] is a *claim*. A caller wrote it.
//     Claim-carrying types are named with a "Claimed" prefix so no reader can mistake
//     them for authority. Claims are inputs to verification; they never authorize anything.
//   - Everything reachable from [ReleaseEvidence] is *verified*. Each field records
//     something SafeLane observed against GitHub or the registry itself. A
//     [ReleaseEvidence] value cannot be constructed outside this package except through
//     [NewReleaseEvidence] (or JSON decoding, which runs the same validation), because
//     all of its fields are unexported. A caller-supplied literal cannot be laundered
//     into a Release.
//
// # Unknown is never a pass
//
// Evidence is carried on a Release as an [EvidenceResult], not as a bare
// [ReleaseEvidence]. Its zero value is [EvidenceUnknown], so a field that was never
// populated reads as "unknown", never as "verified". Only [EvidenceVerified] results
// carry a [ReleaseEvidence] at all; a missing, failed or unknown result has nothing to
// read. There is deliberately no boolean conversion, no "OK()" helper, and no default
// severity anywhere in this package: turning an outcome into a risk tier or a policy
// decision is #50's job and must not be short-circuited here.
//
// # No Kubernetes configuration crosses the intake boundary
//
// [ReleaseRequest] is a closed struct. It contains no map[string]any, no
// json.RawMessage, no free-form label or annotation map, and no interface field.
// There is therefore no representation in which a Kubernetes object, a YAML or JSON
// patch, a template selection or a policy selection could reach a Release, even if an
// intake layer forgot to check. Because the spec requires such fields to be *rejected*
// rather than silently dropped, intake must additionally decode raw JSON with
// (*json.Decoder).DisallowUnknownFields and screen the payload against
// [ForbiddenRequestKeys] before populating this type. See [ForbiddenRequestKeys] for
// the contract intake is expected to honour.
//
// # One rendering, reused everywhere
//
// A [RenderedBundle] is produced exactly once per Release, by
// github.com/AndrewMaged814/safelane/internal/render. Nothing in this package can
// render, re-render or mutate one:
//
//   - [RenderedResource] stores its bytes and its content hash together in unexported
//     fields. The hash is computed from the bytes at construction by
//     [NewRenderedResource], which accepts no hash argument, so the two cannot disagree.
//   - [RenderedResource.Bytes] returns a copy, so a caller cannot mutate rendered bytes
//     after they were hashed.
//   - Neither [Release] nor [RenderedBundle] holds a renderer, a template, or any method
//     that produces a bundle. The only way to obtain a second bundle is to call the
//     renderer again with the full inputs, which yields byte-identical output by
//     construction.
//
// # Extension points for #50 and #52
//
// This package deliberately contains no risk vocabulary and no policy vocabulary.
// #50 adds the assessment and the policy decision as an additive section on [Release]
// (a new field plus a new JSON key); #52 adds proof rendering as a read model over
// these types. Neither may change release identity, evidence, or the artifact/target/
// bundle binding enforced by [NewRelease] - those are frozen by this package.
package release
