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
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "release-request.json"))
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
	intent, err := Parse(marshal(t, obj))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent.PullRequest != 1 || intent.Repository != "AndrewMaged814/podinfo" {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

func TestParse_EmptyObject_IsInvalidRequest(t *testing.T) {
	_, err := Parse([]byte(`{}`))
	if err == nil {
		t.Fatal("empty object must be rejected")
	}
	if release.Categorize(err) != release.CategoryInvalidRequest &&
		release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want invalid or malformed request for {}, got %v (%v)", release.Categorize(err), err)
	}
}

func TestParse_EvidenceFields_Forbidden(t *testing.T) {
	obj := validFixture(t)
	obj["evidence"] = map[string]any{"merge_commit_sha": "claim"}

	_, err := Parse(marshal(t, obj))
	if release.Categorize(err) != release.CategoryForbiddenField {
		t.Fatalf("want CategoryForbiddenField, got %v (%v)", release.Categorize(err), err)
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

func TestParse_MissingPullRequest_RejectedByValidate(t *testing.T) {
	obj := validFixture(t)
	delete(obj, "pull_request")

	_, err := Parse(marshal(t, obj))
	if release.Categorize(err) != release.CategoryInvalidRequest {
		t.Fatalf("want CategoryInvalidRequest for a missing pull request, got %v (%v)", release.Categorize(err), err)
	}
}

// TestParse_ForbiddenFieldsReportedInDocumentOrder pins the order N9
// prints: a request carrying "risk" and then "lane" is told about "risk"
// first, next to where its author wrote it. Sorting the keys, or reading
// them out of a map, would reverse that pair for no reason a reader of
// the request could see.
func TestParse_ForbiddenFieldsReportedInDocumentOrder(t *testing.T) {
	_, err := Parse([]byte(`{ "repository": "AndrewMaged814/podinfo", "pull_request": 4,
  "risk": "low", "lane": "fast" }`))
	errs := release.Flatten(err)
	if len(errs) != 2 {
		t.Fatalf("want exactly the two named fields rejected, got %v", errs)
	}
	if errs[0].Field != "risk" || errs[1].Field != "lane" {
		t.Fatalf("want risk then lane, got %q then %q", errs[0].Field, errs[1].Field)
	}
	for _, e := range errs {
		if e.Code != "unknown_field" {
			t.Errorf("want code unknown_field for %q, got %q", e.Field, e.Code)
		}
		if e.Remedy != "send repository and pull_request only" {
			t.Errorf("want the same remedy on every schema rejection, got %q", e.Remedy)
		}
	}
	if errs[0].Message != "a Release Request carries no risk claims" {
		t.Errorf("unexpected message for risk: %q", errs[0].Message)
	}
	if errs[1].Message != "the lane is selected by assessment, never requested" {
		t.Errorf("unexpected message for lane: %q", errs[1].Message)
	}
}

// A caller cannot assert its own evidence any more than it can name its
// own lane; both are the same rejection with different wording.
func TestParse_EvidenceClaim_IsAnUnknownField(t *testing.T) {
	_, err := Parse([]byte(`{ "repository": "AndrewMaged814/podinfo", "pull_request": 3,
  "evidence": { "approved": true, "check": "success" } }`))
	errs := release.Flatten(err)
	if len(errs) != 1 {
		t.Fatalf("want one rejection, got %v", errs)
	}
	if errs[0].Field != "evidence" || errs[0].Code != "unknown_field" {
		t.Fatalf("want unknown_field (evidence), got %s (%s)", errs[0].Code, errs[0].Field)
	}
	if errs[0].Message != "a Release Request carries no evidence claims" {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}
