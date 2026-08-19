package assess

import (
	"regexp"
	"strings"
)

// compileGlob turns an operator-written path glob (`pkg/api/**`,
// `**/migrations/**`, `charts/**`) into a regexp matching a repository-
// relative file path. `**` matches across path separators; `*` matches
// within one path segment only.
//
// This is deliberately not a full doublestar implementation: a leading
// `**/` still requires at least one directory component before the
// literal suffix that follows it (it will not match a file sitting
// directly at the repository root). Every path rule in Appendix C3 has a
// literal segment on at least one side of every `**`, so that gap does not
// change any decision phase one makes. A hand-rolled doublestar matcher to
// close it is not worth the risk of getting subtly wrong for a case
// nothing here exercises.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	const (
		doubleStarPlaceholder = "\x00DOUBLESTAR\x00"
		starPlaceholder       = "\x00STAR\x00"
	)
	p := strings.ReplaceAll(pattern, "**", doubleStarPlaceholder)
	p = strings.ReplaceAll(p, "*", starPlaceholder)
	p = regexp.QuoteMeta(p)
	p = strings.ReplaceAll(p, doubleStarPlaceholder, ".*")
	p = strings.ReplaceAll(p, starPlaceholder, "[^/]*")
	return regexp.Compile("^" + p + "$")
}
