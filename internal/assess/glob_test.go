package assess

import "testing"

func TestCompileGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"pkg/api/**", "pkg/api/echo.go", true},
		{"pkg/api/**", "pkg/api/nested/deep/file.go", true},
		{"pkg/api/**", "pkg/version/version.go", false},
		{"charts/**", "charts/podinfo/values.yaml", true},
		{"charts/**", "pkg/api/echo.go", false},
		{"**/migrations/**", "db/migrations/0001_init.sql", true},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false}, // single * does not cross a path segment
	}
	for _, tc := range tests {
		re, err := compileGlob(tc.pattern)
		if err != nil {
			t.Fatalf("compileGlob(%q): %v", tc.pattern, err)
		}
		if got := re.MatchString(tc.path); got != tc.want {
			t.Errorf("compileGlob(%q).MatchString(%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}
