package intake

import (
	"testing"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func TestParseRejectsCallerSuppliedRiskAndLane(t *testing.T) {
	_, err := Parse([]byte(`{"schema_version":"safelane.release.request/v1","repository":"AndrewMaged814/safelane-demo-api","pull_request":4,"environment":"production","risk":"high","lane":"guarded"}`))
	if err == nil {
		t.Fatal("risk and lane were accepted")
	}
	errs := release.Flatten(err)
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want risk and lane", errs)
	}
	if errs[0].Field != "risk" || errs[1].Field != "lane" {
		t.Fatalf("fields = %q, %q", errs[0].Field, errs[1].Field)
	}
	for _, got := range errs {
		if got.Category != release.CategoryForbiddenField {
			t.Errorf("%s category = %s", got.Field, got.Category)
		}
	}
}
