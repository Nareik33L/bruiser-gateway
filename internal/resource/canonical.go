// Package resource canonicalises scarcity identifiers so string variation
// cannot create a new execution domain.
//
// Scarce resource IDs are identifiers, not free-form text. After trim,
// optional percent-decode, case fold, and trailing-slash strip, the
// string must match a closed ASCII grammar. Everything else is rejected.
// There is no punctuation or Unicode folding: lookalikes are different
// strings, and different strings that fail the grammar are not IDs.
package resource

import (
	"net/url"
	"regexp"
	"strings"
)

// ErrMalformed is returned when a resource identifier is empty or unsafe
// after canonicalisation.
const ErrMalformed = malformed("malformed resource identifier")

type malformed string

func (e malformed) Error() string { return string(e) }

// Grammar (after trim / optional unescape / lower / trailing '/' strip):
//
//	type ":" ident
//	type  = [a-z][a-z0-9]*
//	ident = [a-z0-9]+(-[a-z0-9]+)*
//
// One colon. ASCII hyphen only inside the ident. No underscore, dot,
// extra colon, Unicode dash, or trailing punctuation.
var strictID = regexp.MustCompile(`^[a-z][a-z0-9]*:[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Canonical maps an accepted resource string onto exactly one
// merchant-defined resource ID, or rejects it.
func Canonical(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrMalformed
	}
	if unesc, err := url.PathUnescape(s); err == nil {
		s = unesc
	}
	s = strings.ToLower(s)
	s = strings.TrimRight(s, "/")
	if !strictID.MatchString(s) {
		return "", ErrMalformed
	}
	return s, nil
}

// MustCanonical is Canonical that returns "" on error (caller should reject).
func MustCanonical(raw string) string {
	s, err := Canonical(raw)
	if err != nil {
		return ""
	}
	return s
}
