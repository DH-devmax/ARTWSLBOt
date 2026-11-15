package control

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var evidenceDocument = regexp.MustCompile(`^docs/[a-zA-Z0-9_/-]+\.md$`)
var evidenceAcceptance = regexp.MustCompile(`^acceptance:[A-Z0-9][A-Za-z0-9_-]{0,95}$`)

// Evidence is an identifier, never a fetched URL or free-form upstream output.
func ValidBusinessEvidenceRefs(refs []string) bool {
	if len(refs) > 16 {
		return false
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if len(ref) > 256 || !utf8.ValidString(ref) || seen[ref] {
			return false
		}
		seen[ref] = true
		if evidenceAcceptance.MatchString(ref) {
			continue
		}
		document, anchor, hasAnchor := strings.Cut(ref, "#")
		if !evidenceDocument.MatchString(document) || path.Clean(document) != document {
			return false
		}
		if hasAnchor && (anchor == "" || utf8.RuneCountInString(anchor) > 80 ||
			strings.IndexFunc(anchor, func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
			}) >= 0) {
			return false
		}
	}
	return true
}
