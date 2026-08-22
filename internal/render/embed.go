package render

import "embed"

// FixtureTemplateFS is the demo Release Template copied by `safelane setup`
// into .safelane/release-template when that directory does not exist.
// Runtime loading uses the operator path in project.yml, not this FS.
//
//go:embed testdata/release-template
var FixtureTemplateFS embed.FS
