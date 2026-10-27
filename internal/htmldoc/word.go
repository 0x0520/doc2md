package htmldoc

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/0x0520/doc2md/internal/mdrender"
)

// Word's HTML export has no <ul>/<ol>: list items are paragraphs whose marker
// is literal text. These patterns recognise those paragraphs.
var (
	// msoListRe matches the list reference in an item's style, e.g. "mso-list:l0 level2 lfo1".
	msoListRe = regexp.MustCompile(`(?i)mso-list:\s*l\d+\s+level(\d+)`)
	// msoIgnoreRe marks the span that holds the marker and its padding.
	msoIgnoreRe = regexp.MustCompile(`(?i)mso-list:\s*ignore`)
	// msoTocRe matches the paragraph classes Word gives table-of-contents lines.
	msoTocRe = regexp.MustCompile(`^MsoToc[1-9]$`)
	// orderedMarkerRe matches a whole marker such as "1.", "2.1.", "a)", "iv." or "(b)".
	orderedMarkerRe = regexp.MustCompile(`^\(?(?:\d+(?:\.\d+)*|[A-Za-z]|[ivxlcdm]+|[IVXLCDM]+)[.)]$`)
)

// isWordTOC reports whether n is a table-of-contents line; the headings below
// already carry the structure, and the leader dots and page numbers are noise.
func isWordTOC(n *html.Node) bool {
	for _, c := range strings.Fields(Attr(n, "class")) {
		if msoTocRe.MatchString(c) {
			return true
		}
	}
	return false
}

// isMsoIgnore reports whether n is the span wrapping a list marker.
func isMsoIgnore(n *html.Node) bool {
	return msoIgnoreRe.MatchString(Attr(n, "style"))
}

// wordListItem converts a Word list paragraph into a list item block.
// Word writes either the "mso-list" style with the marker in an Ignore span, or
// (filtered HTML) only a MsoListBullet/MsoListNumber/MsoListParagraph class
// with the marker as leading text.
func wordListItem(p *html.Node, sink ImageSink) (mdrender.Block, bool) {
	level, ordered, ok := 1, false, false
	if m := msoListRe.FindStringSubmatch(Attr(p, "style")); m != nil {
		level, ok = atoiClamped(m[1]), true
	}
	for _, c := range strings.Fields(Attr(p, "class")) {
		switch {
		case strings.HasPrefix(c, "MsoListBullet"), strings.HasPrefix(c, "MsoListNumber"):
			ordered = strings.HasPrefix(c, "MsoListNumber")
			// "List Bullet 2" is exported as MsoListBullet2; the CxSp variants have no digit.
			if d := strings.TrimPrefix(strings.TrimPrefix(c, "MsoListBullet"), "MsoListNumber"); d != "" && d[0] >= '2' && d[0] <= '9' {
				level = int(d[0] - '0')
			}
			ok = true
		case strings.HasPrefix(c, "MsoListParagraph"):
			ok = true
		}
	}
	if !ok {
		return mdrender.Block{}, false
	}

	s := inlineText(p, sink)
	if span := findIgnoreSpan(p); span != nil {
		// The marker is in the Ignore span, which inlineText skipped.
		ordered = orderedMarkerRe.MatchString(Collapse(text(span))) || (ordered && Collapse(text(span)) == "")
	} else {
		s, ordered = splitLeadingMarker(s, ordered)
	}
	if s == "" {
		return mdrender.Block{}, false
	}
	marker := "- "
	if ordered {
		marker = "1. "
	}
	return mdrender.Block{Kind: mdrender.List, Text: strings.Repeat("  ", level-1) + marker + s}, true
}

// atoiClamped parses a level, limited to a depth a Markdown list can show.
func atoiClamped(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
		if n > 9 {
			return 9
		}
	}
	return max(n, 1)
}

func findIgnoreSpan(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if isMsoIgnore(c) {
			return c
		}
		if f := findIgnoreSpan(c); f != nil {
			return f
		}
	}
	return nil
}

// splitLeadingMarker removes a literal marker (a symbol-font bullet or "1.")
// from the start of s when there is no Ignore span to find it by. A word that
// merely looks like a marker is left alone unless it is a lone symbol.
func splitLeadingMarker(s string, hintOrdered bool) (rest string, ordered bool) {
	first, after, found := strings.Cut(s, " ")
	if !found {
		return s, hintOrdered
	}
	if orderedMarkerRe.MatchString(first) {
		return after, true
	}
	if r, size := utf8.DecodeRuneInString(first); size == len(first) && (first == "o" || isBulletRune(r)) {
		return after, false
	}
	return s, hintOrdered
}

// isBulletRune accepts symbol-font bullets (·, •, ▪, private-use glyphs) but not
// punctuation that opens a sentence.
func isBulletRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(`("'*_[<“‘«`, r)
}
