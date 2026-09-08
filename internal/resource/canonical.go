// Package resource canonicalises scarcity identifiers so string variation
// cannot create a new execution domain.
package resource

import (
	"net/url"
	"strings"
	"unicode"
)

// ErrMalformed is returned when a resource identifier is empty or unsafe
// after canonicalisation.
const ErrMalformed = malformed("malformed resource identifier")

type malformed string

func (e malformed) Error() string { return string(e) }

// Canonical folds case, whitespace, trailing slashes, duplicate separators,
// and path/punctuation variants into one identifier. Domain keys must use
// only this form.
func Canonical(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrMalformed
	}
	if unesc, err := url.PathUnescape(s); err == nil {
		s = unesc
	}
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSep := false
	for _, r := range s {
		switch {
		case r == 0 || r == '?' || r == '#' || r == '\\' || unicode.IsControl(r):
			return "", ErrMalformed
		case unicode.IsSpace(r):
			continue
		case r == '/' || r == ':' || r == ';' || r == ',':
			if prevSep {
				continue
			}
			b.WriteByte(':')
			prevSep = true
		case r == '.' && prevSep:
			return "", ErrMalformed
		default:
			if r == '.' && strings.HasSuffix(b.String(), ".") {
				return "", ErrMalformed
			}
			b.WriteRune(r)
			prevSep = false
		}
	}
	out := strings.Trim(b.String(), ":")
	if out == "" || out == "." || strings.Contains(out, "..") {
		return "", ErrMalformed
	}
	return out, nil
}

// MustCanonical is Canonical that returns "" on error (caller should reject).
func MustCanonical(raw string) string {
	s, err := Canonical(raw)
	if err != nil {
		return ""
	}
	return s
}
