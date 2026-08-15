// Package render is SafeLane's render-and-hash seam.
//
// It takes a verified immutable digest, a release target, and an operator-owned
// Release Template, and produces one [github.com/AndrewMaged814/safelane/internal/release.RenderedBundle]:
// the exact final Kubernetes object bytes SafeLane intends to apply, each one
// content-hashed.
//
// The entire security claim of SafeLane lives in this package. "The bytes
// DeployWhisper assessed are the bytes that reach the cluster" is true only if
// rendering happens once, the hash covers exactly what was rendered, and nothing
// downstream renders again. Three properties carry that claim, and each is structural
// rather than a rule someone has to remember.
//
// # 1. Nothing external supplies the bundle
//
// [Render] takes a [Template] loaded from operator-owned files and a
// release.ReleaseEvidence. It does not take a release.ReleaseRequest, so there is no
// parameter through which a caller's Kubernetes objects, patches, or template
// selection could reach rendering - the caller's submission is not in scope here at
// all. The pinned digest is read from the *verified* evidence, and a
// release.ReleaseEvidence cannot be constructed outside the release package except
// through its validating constructor, so rendering against a merely claimed digest is
// not expressible.
//
// # 2. The hash covers exactly the bytes that were rendered
//
// [Render] never hands raw bytes back to a caller alongside a separately computed
// hash, and never re-parses or re-serializes what it rendered. Every rendered file
// goes straight from the template executor's buffer into
// release.NewRenderedResource, which stores the bytes and derives the hash in one
// step and exposes no way to set them apart. The bundle-level digest is derived from
// those per-resource hashes rather than from a second serialization of the same
// objects, so there is only ever one canonical form of the bundle.
//
// The Kubernetes coordinates recorded for each resource (apiVersion, kind, namespace,
// name) are read back *out of* the rendered bytes rather than taken from template
// metadata, so the recorded identity describes what was actually produced. That read
// is for identification only; it never feeds back into the hashed bytes.
//
// # 3. One rendering, reused everywhere
//
// [Render] is the only entry point that produces a bundle, and it is a pure function
// of its arguments. There is no cache keyed by release, no renderer object holding
// state, and - crucially - no method anywhere on release.Release or
// release.RenderedBundle that invokes this package. A caller that has a bundle cannot
// ask for it to be rendered again; it can only read the one it holds. Downstream
// consumers (risk analysis, proof, execution) receive that value, and
// release.RenderedResource.Bytes returns a copy, so they cannot alter the bytes the
// recorded hashes cover.
//
// Calling [Render] twice with the same [Template], target and evidence is not
// forbidden - it is simply pointless, because it is guaranteed to produce
// byte-identical output. That is the determinism property, and it is what makes the
// "single rendering" rule enforceable by comparison rather than by trust.
//
// # Determinism
//
// Same template content digest + same verified digest + same target => byte-identical
// bundle, on any machine, at any time. The mechanism is deliberately boring:
//
//   - text/template over the template files, executed with a *struct* ([TemplateData])
//     rather than a map, so there is no map iteration order to leak into output;
//   - resource order is the lexicographic order of template file paths, fixed and
//     independent of filesystem iteration order;
//   - no template functions are registered at all, so there is no now(), no rand, no
//     uuid, and no env for a template author to reach for;
//   - the renderer's signature accepts no clock, no entropy source, and no
//     release.ReleaseID, so a timestamp or an ID cannot appear in rendered bytes even
//     by accident;
//   - CRLF is normalized to LF when template files are read, so a Windows checkout and
//     a Linux CI runner produce identical hashes;
//   - every value substituted into the output is validated against a strict character
//     set first - which also closes the YAML-injection path that unescaped
//     text/template output would otherwise open.
//
// # The fixture template
//
// Ahmed owns the real Release Template (see #47, "Release Template ownership"). Until
// it exists, this package renders against a fixture under
// internal/render/testdata/release-template. The fixture is structurally real but has
// no authority: swapping in the real directory is a drop-in change, and
// internal/render/testdata/release-template/README.md states exactly what a real
// template must provide.
package render
