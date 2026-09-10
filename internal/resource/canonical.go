// Package resource canonicalises scarcity identifiers so string variation
// cannot create a new execution domain.
//
// Canonical is a fold, not a denylist: NFKC, dash/space/quote lookalikes,
// strip leading/trailing non-alphanumeric runs, collapse separator-class
// punctuation to ":", then a positive allowlist [a-z0-9:-] and the
// type:ident grammar. Cosmetic variants of one resource therefore share
// one domain key.
//
// When a merchant registers a catalogue, Canonical still folds first and
// then resolves onto a registered ID (or rejects with ErrUnknownResource).
// No free-form string reaches the domain key in that mode.
package resource

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ErrMalformed is returned when a resource identifier is empty or unsafe
// after canonicalisation.
const ErrMalformed = malformed("malformed resource identifier")

// ErrUnknownResource is returned when a catalogue is configured and the
// folded identifier is not in it.
const ErrUnknownResource = unknown("unknown resource")

type malformed string

func (e malformed) Error() string { return string(e) }

type unknown string

func (e unknown) Error() string { return string(e) }

// Grammar after folding:
//
//	type ":" ident
//	type  = [a-z][a-z0-9]*
//	ident = [a-z0-9]+(-[a-z0-9]+)*
var strictID = regexp.MustCompile(`^[a-z][a-z0-9]*:[a-z0-9]+(?:-[a-z0-9]+)*$`)

const separators = "/:;, \t"

// Canonical maps an inbound resource string onto exactly one identifier.
func Canonical(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrMalformed
	}
	if unesc, err := url.PathUnescape(s); err == nil && unesc != "" {
		s = unesc
	}
	if strings.ContainsAny(s, "?#\\") || strings.Contains(s, "..") {
		return "", ErrMalformed
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return "", ErrMalformed
		}
	}
	s = norm.NFKC.String(s)
	s = foldLookalikes(s)
	s = strings.ToLower(s)
	s = trimNonAlnum(s)
	s = collapseSeparators(s)
	s = trimNonAlnum(s)
	if s == "" || strings.Contains(s, "..") {
		return "", ErrMalformed
	}
	for _, r := range s {
		if !allowed(r) {
			return "", ErrMalformed
		}
	}
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

// Catalogue is a merchant-scoped allowlist of canonical resource IDs.
// An empty catalogue folds only (lab / ad-hoc tests). A non-empty
// catalogue resolves a folded inbound string onto a registered ID.
type Catalogue struct {
	ids map[string]struct{}
}

// NewCatalogue folds and indexes registered IDs. Malformed entries are skipped.
func NewCatalogue(registered []string) Catalogue {
	c := Catalogue{ids: map[string]struct{}{}}
	for _, id := range registered {
		folded, err := Canonical(id)
		if err != nil {
			continue
		}
		c.ids[folded] = struct{}{}
	}
	return c
}

// Canonical folds raw and, when the catalogue is non-empty, requires a
// registered ID.
func (c Catalogue) Canonical(raw string) (string, error) {
	folded, err := Canonical(raw)
	if err != nil {
		return "", err
	}
	if len(c.ids) == 0 {
		return folded, nil
	}
	if _, ok := c.ids[folded]; ok {
		return folded, nil
	}
	return "", ErrUnknownResource
}

func foldLookalikes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case isDash(r):
			b.WriteByte('-')
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case isQuote(r):
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isDash(r rune) bool {
	switch r {
	case '-', 0x2010, 0x2011, 0x2012, 0x2013, 0x2014, 0x2015, 0x2212,
		0x2043, 0xFE58, 0xFE63, 0xFF0D:
		return true
	default:
		return false
	}
}

func isQuote(r rune) bool {
	switch r {
	case '\'', '"', '`', 0x2018, 0x2019, 0x201A, 0x201B,
		0x201C, 0x201D, 0x201E, 0x00AB, 0x00BB, 0x2039, 0x203A:
		return true
	default:
		return false
	}
}

func allowed(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ':' || r == '-'
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		(r >= 'A' && r <= 'Z')
}

func trimNonAlnum(s string) string {
	start := 0
	for start < len(s) {
		r, n := utf8.DecodeRuneInString(s[start:])
		if isAlnum(r) {
			break
		}
		start += n
	}
	end := len(s)
	for end > start {
		r, n := utf8.DecodeLastRuneInString(s[:end])
		if isAlnum(r) {
			break
		}
		end -= n
	}
	return s[start:end]
}

func collapseSeparators(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inRun := false
	for _, r := range s {
		if strings.ContainsRune(separators, r) {
			if !inRun {
				b.WriteByte(':')
				inRun = true
			}
			continue
		}
		inRun = false
		b.WriteRune(r)
	}
	return b.String()
}
