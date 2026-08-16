// Package intake is the release-intake boundary: it turns raw caller JSON
// into a validated release.Intent, or a typed, actionable rejection.
// It verifies nothing — no GitHub call, no registry call. Its only job is to
// prove the request is well-formed and carries change identity only.
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
// `safelane release --file release-request.json`.
//
// Three checks run, and every problem any of them finds is returned
// together as [release.Errors], so an agent can correct everything the
// request got wrong in a single pass:
//
//  1. Top-level forbidden-field screening against [release.ForbiddenRequestKeys].
//  2. A single well-formed JSON object, decoded into [release.Intent]
//     with (*json.Decoder).DisallowUnknownFields.
//  3. [release.Intent.Validate], for a request that decodes cleanly
//     but is structurally unusable: wrong schema version, missing PR, a
//     mutable image pin, and so on.
func Parse(raw []byte) (release.Intent, error) {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return release.Intent{}, release.Malformed("invalid_json", "",
			"the request is not valid JSON",
			"Submit a single JSON object matching the Release Request schema.").WithCause(err)
	}
	obj, ok := generic.(map[string]any)
	if !ok {
		return release.Intent{}, release.Malformed("invalid_request_shape", "",
			"the request must be a single JSON object",
			"Submit a single JSON object, not an array or a scalar value.")
	}

	var errs release.Errors
	forbiddenTop := make(map[string]bool)
	for _, e := range screenForbiddenTopLevelFields(obj) {
		errs = append(errs, e)
		forbiddenTop[e.Field] = true
	}

	var intent release.Intent
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&intent); err != nil {
		if field, ok := unknownFieldName(err); ok && forbiddenTop[field] {
			// Already reported precisely by the forbidden-field screen above.
		} else {
			errs = append(errs, release.Malformed("unrecognized_request_field", field,
				"the request does not match the Release Request schema",
				"Submit repository, pull_request, and optional environment only. Do not author evidence claims.").WithCause(err))
		}
	} else if err := intent.Validate(); err != nil {
		errs = append(errs, release.Flatten(err)...)
	}

	if err := errs.OrNil(); err != nil {
		return release.Intent{}, err
	}
	return intent, nil
}

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
				"the request must not include %q; SafeLane collects evidence and renders the deployment bundle itself", k)))
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
