package intake

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func validFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "release-evidence.json"))
	if err != nil {
		t.Fatalf("could not read fixture: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	return obj
}

func marshal(t *testing.T, obj map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("could not marshal test object: %v", err)
	}
	return raw
}

func TestParse_ValidFixture_Succeeds(t *testing.T) {
	obj := validFixture(t)
	req, err := Parse(marshal(t, obj))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Target.Application != "podinfo" {
		t.Fatalf("unexpected request: %+v", req)
	}
}

// Regression test for the "kind" collision: release.ForbiddenRequestKeys contains
// "kind", which collides with the legitimate nested field caller.kind. The fixture
// itself carries "caller": {"kind": "agent", ...}, so this is really just asserting
// TestParse_ValidFixture_Succeeds does not regress -- but it is worth pinning down
// explicitly, since a naive any-depth forbidden-key scan would break every valid
// request.
func TestParse_CallerKindField_IsNotForbidden(t *testing.T) {
	obj := validFixture(t)
	caller, ok := obj["caller"].(map[string]any)
	if !ok {
		t.Fatalf("fixture has no caller object to assert against")
	}
	if caller["kind"] != "agent" {
		t.Fatalf("expected the fixture to carry caller.kind, got %v", caller["kind"])
	}
	if _, err := Parse(marshal(t, obj)); err != nil {
		t.Fatalf("caller.kind must not be rejected as a forbidden field: %v", err)
	}
}

func TestParse_TopLevelForbiddenField_Rejected(t *testing.T) {
	obj := validFixture(t)
	obj["manifests"] = []any{map[string]any{"apiVersion": "v1", "kind": "Pod"}}

	_, err := Parse(marshal(t, obj))
	if err == nil {
		t.Fatal("want a rejection for a top-level forbidden field")
	}
	if release.Categorize(err) != release.CategoryForbiddenField {
		t.Fatalf("want CategoryForbiddenField, got %v (%v)", release.Categorize(err), err)
	}
	var errs release.Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want release.Errors, got %T", err)
	}
	found := false
	for _, e := range errs {
		if e.Field == "manifests" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a rejection naming field \"manifests\", got %v", errs)
	}
}

func TestParse_MultipleTopLevelForbiddenFields_AllReported(t *testing.T) {
	obj := validFixture(t)
	obj["patch"] = map[string]any{"op": "replace"}
	obj["policy"] = "critical"
	obj["template"] = "custom"

	_, err := Parse(marshal(t, obj))
	var errs release.Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want release.Errors, got %T (%v)", err, err)
	}
	seen := map[string]bool{}
	for _, e := range errs {
		if e.Category == release.CategoryForbiddenField {
			seen[e.Field] = true
		}
	}
	for _, want := range []string{"patch", "policy", "template"} {
		if !seen[want] {
			t.Errorf("want a forbidden-field rejection naming %q, got %v", want, errs)
		}
	}
}

func TestParse_ForbiddenFieldCaseInsensitive(t *testing.T) {
	obj := validFixture(t)
	obj["Manifests"] = []any{"whatever"}

	_, err := Parse(marshal(t, obj))
	if release.Categorize(err) != release.CategoryForbiddenField {
		t.Fatalf("want CategoryForbiddenField for a differently-cased forbidden key, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_NestedForbiddenField_RejectedAsMalformedNotSilentlyDropped(t *testing.T) {
	obj := validFixture(t)
	artifact := obj["artifact"].(map[string]any)
	artifact["patch"] = map[string]any{"op": "replace"}
	obj["artifact"] = artifact

	req, err := Parse(marshal(t, obj))
	if err == nil {
		t.Fatalf("a patch nested under artifact must be rejected, not silently dropped, got request: %+v", req)
	}
	// It is caught by strict decode (an unknown field inside ClaimedArtifact), not by
	// the top-level forbidden-field screen, so it surfaces as malformed rather than
	// forbidden -- rejected either way.
	if release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want CategoryMalformedRequest for a field nested inside a known sub-object, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_InvalidJSON_Rejected(t *testing.T) {
	_, err := Parse([]byte("{not json"))
	if release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want CategoryMalformedRequest, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_JSONArray_Rejected(t *testing.T) {
	_, err := Parse([]byte(`[1, 2, 3]`))
	if release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want CategoryMalformedRequest, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_UnrecognizedField_RejectedAsMalformed(t *testing.T) {
	obj := validFixture(t)
	obj["totally_made_up_field"] = "x"

	_, err := Parse(marshal(t, obj))
	if release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want CategoryMalformedRequest for an unrecognized-but-not-forbidden field, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_StructurallyInvalidRequest_RunsValidate(t *testing.T) {
	obj := validFixture(t)
	obj["schema_version"] = "not-a-real-version"

	_, err := Parse(marshal(t, obj))
	if release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want CategoryMalformedRequest for an unsupported schema version, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_MissingTargetComponent_RejectedByValidate(t *testing.T) {
	obj := validFixture(t)
	target := obj["target"].(map[string]any)
	delete(target, "namespace")
	obj["target"] = target

	_, err := Parse(marshal(t, obj))
	if release.Categorize(err) != release.CategoryInvalidRequest {
		t.Fatalf("want CategoryInvalidRequest for a missing target component, got %v (%v)", release.Categorize(err), err)
	}
}
