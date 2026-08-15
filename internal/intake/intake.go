// Package intake is the release-intake boundary: it turns raw caller JSON
// into a validated release.ReleaseRequest, or a typed, actionable rejection.
// It verifies nothing — no GitHub call, no registry call. Its only job is to
// prove the request is well-formed and carries release identity and
// evidence only.
package intake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// Parse decodes and validates raw Release Request JSON, the argument to
// `safelane release --file release-evidence.json`.
//
// Three checks run, and every problem any of them finds is returned
// together as [release.Errors], so an agent can correct everything the
// request got wrong in a single pass:
//
//  1. Top-level forbidden-field screening against [release.ForbiddenRequestKeys].
//     This is deliberately scoped to the request's top level rather than
//     "any depth": [release.ForbiddenRequestKeys] contains "kind", which is
//     both a Kubernetes object field SafeLane must reject and the JSON key
//     of [release.CallerIdentity.Kind] - a legitimate field nested under
//     "caller" on every valid request. An any-depth scan would reject
//     "caller.kind" on the Release Request fixture and on every real
//     submission. A forbidden field nested inside a legitimate sub-object
//     (an attempted "artifact.patch", say) has nowhere to decode into and
//     is caught by check 2 instead - reported as malformed rather than
//     forbidden, but rejected either way, never silently dropped.
//  2. A single well-formed JSON object, decoded into [release.ReleaseRequest]
//     with (*json.Decoder).DisallowUnknownFields. Go's json package applies
//     that recursively through every nested named struct, so a field
//     smuggled at any depth - forbidden or merely misspelled - fails
//     decode.
//  3. [release.ReleaseRequest.Validate], for a request that decodes cleanly
//     but is structurally unusable: wrong schema version, malformed commit
//     SHA, a mutable image reference, and so on.
func Parse(raw []byte) (release.ReleaseRequest, error) {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return release.ReleaseRequest{}, release.Malformed("invalid_json", "",
			"the request is not valid JSON",
			"Submit a single JSON object matching the Release Request schema.").WithCause(err)
	}
	obj, ok := generic.(map[string]any)
	if !ok {
		return release.ReleaseRequest{}, release.Malformed("invalid_request_shape", "",
			"the request must be a single JSON object",
			"Submit a single JSON object, not an array or a scalar value.")
	}

	var errs release.Errors
	forbiddenTop := make(map[string]bool)
	for _, e := range screenForbiddenTopLevelFields(obj) {
		errs = append(errs, e)
		forbiddenTop[e.Field] = true
	}

	var req release.ReleaseRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if field, ok := unknownFieldName(err); ok && forbiddenTop[field] {
			// Already reported precisely by the forbidden-field screen above;
			// the generic decode error would only repeat it less clearly.
		} else {
			errs = append(errs, release.Malformed("unrecognized_request_field", field,
				"the request does not match the Release Request schema",
				"Match the Release Request schema exactly. Remove unrecognized or misspelled fields.").WithCause(err))
		}
	} else if err := req.Validate(); err != nil {
		errs = append(errs, flatten(err)...)
	}

	if err := errs.OrNil(); err != nil {
		return release.ReleaseRequest{}, err
	}
	return req, nil
}

// screenForbiddenTopLevelFields reports every top-level key in obj that
// matches release.ForbiddenRequestKeys. Matching is case-insensitive so a
// caller cannot bypass the check with "Manifests" or "JSONPATCH". Keys are
// checked in sorted order so the returned rejections are reproducible.
func screenForbiddenTopLevelFields(obj map[string]any) []*release.Error {
	forbidden := forbiddenKeySet()
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []*release.Error
	for _, k := range keys {
		if forbidden[strings.ToLower(k)] {
			out = append(out, release.Forbidden(k, fmt.Sprintf(
				"the request must not include %q; SafeLane renders the deployment bundle itself from the operator-owned Release Template", k)))
		}
	}
	return out
}

var forbiddenKeySet = sync.OnceValue(func() map[string]bool {
	m := make(map[string]bool)
	for _, k := range release.ForbiddenRequestKeys() {
		m[strings.ToLower(k)] = true
	}
	return m
})

// unknownFieldName extracts the field name from a
// (*json.Decoder).DisallowUnknownFields error, whose message has the form
// `json: unknown field "name"`. encoding/json exposes no typed error for
// this, so the message is parsed; if the shape ever changes upstream, ok is
// false and the caller falls back to reporting the raw decode error.
func unknownFieldName(err error) (name string, ok bool) {
	const marker = `unknown field "`
	msg := err.Error()
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return "", false
	}
	rest := msg[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// flatten normalizes a single *release.Error or a release.Errors set into a
// slice, mirroring the unexported helper release.ReleaseRequest.Validate
// uses internally.
func flatten(err error) release.Errors {
	switch e := err.(type) {
	case nil:
		return nil
	case release.Errors:
		return e
	case *release.Error:
		return release.Errors{e}
	default:
		return release.Errors{release.Internal("unclassified_error", err.Error())}
	}
}
