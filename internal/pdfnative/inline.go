package pdfnative

import (
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"

	"github.com/nullwerke/doc2md/internal/mdrender"
)

// linkArea is a /Link annotation with a URI action: its rectangle in page space and the target.
type linkArea struct {
	x0, y0, x1, y1 float64
	uri            string
}

// pageLinks reads the URI links of a page. Internal destinations and links to
// script or embedded data are left out.
func pageLinks(p pdf.Page) (links []linkArea) {
	defer func() {
		if recover() != nil {
			links = nil
		}
	}()
	annots := p.V.Key("Annots")
	for i := 0; i < annots.Len(); i++ {
		a := annots.Index(i)
		if a.Key("Subtype").Name() != "Link" {
			continue
		}
		act := a.Key("A")
		uri := strings.TrimSpace(act.Key("URI").Text())
		if act.Key("S").Name() != "URI" || uri == "" || mdrender.ActiveScheme(uri) {
			continue
		}
		r := a.Key("Rect")
		if r.Len() != 4 {
			continue
		}
		x0, y0, x1, y1 := r.Index(0).Float64(), r.Index(1).Float64(), r.Index(2).Float64(), r.Index(3).Float64()
		links = append(links, linkArea{min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1), uri})
	}
	return links
}

// linkAt returns the index of the link whose rectangle holds the glyph's centre, or -1.
func linkAt(t pdf.Text, links []linkArea) int {
	cx, cy := t.X+t.W/2, t.Y+0.3*t.FontSize
	for i, l := range links {
		if cx >= l.x0 && cx <= l.x1 && cy >= l.y0 && cy <= l.y1 {
			return i
		}
	}
	return -1
}

// isItalicFont guesses an italic or oblique face from the font name.
func isItalicFont(font string) bool {
	f := strings.ToLower(font)
	return strings.Contains(f, "italic") || strings.Contains(f, "oblique")
}

// hasContent reports whether s holds a letter or digit. Runs without one (spaces,
// punctuation, bullet glyphs) take the emphasis their neighbours share, so a bold
// bullet does not become "**•**" and a comma after a bold word stays outside.
func hasContent(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// inlineSeg is one piece of a line: a glyph or an inserted word space.
type inlineSeg struct {
	text    string
	em      mdrender.Emphasis
	link    int
	neutral bool
	blank   bool
}

// richText renders a row with Markdown emphasis and links: rich has both, linked
// only the links (for text whose emphasis is styling). styled reports that all of
// the row's content is bold or all of it italic. Both strings are empty when the
// row carries no markup, so callers fall back to the plain text.
func richText(toks []pdf.Text, links []linkArea) (rich, linked string, styled bool) {
	if !hasMarkup(toks, links) {
		return "", "", false
	}
	sortByX(toks)
	var segs []inlineSeg
	anyEm, anyLink := false, false
	allBold, allItalic, content := true, true, false
	lastFont, lastEm := "", mdrender.Emphasis{}
	wReliable := widthsReliable(toks)
	for i, t := range toks {
		if i > 0 && needsSpace(toks[i-1], t, wReliable, segs[len(segs)-1].text) {
			segs = append(segs, inlineSeg{text: " ", link: -1, neutral: true, blank: true})
		}
		if t.Font != lastFont || i == 0 {
			lastFont, lastEm = t.Font, mdrender.Emphasis{Bold: isBoldFont(t.Font), Italic: isItalicFont(t.Font)}
		}
		s := inlineSeg{text: t.S, em: lastEm, link: linkAt(t, links)}
		s.blank = strings.TrimSpace(t.S) == ""
		s.neutral = s.blank || !hasContent(t.S)
		if !s.neutral {
			content = true
			allBold = allBold && s.em.Bold
			allItalic = allItalic && s.em.Italic
			anyEm = anyEm || s.em != mdrender.Emphasis{}
		}
		if s.link >= 0 && !s.blank {
			anyLink = true
		}
		segs = append(segs, s)
	}
	if !anyEm && !anyLink {
		return "", "", false
	}
	resolveNeutral(segs)
	linked = ""
	if anyLink {
		linked = renderSegs(segs, links, false)
	}
	return renderSegs(segs, links, true), linked, content && (allBold || allItalic)
}

// hasMarkup is a quick test for an emphasised glyph or one inside a link, so rows
// of plain text skip richText's work.
func hasMarkup(toks []pdf.Text, links []linkArea) bool {
	lastFont, lastEm := "", false
	for i, t := range toks {
		if i == 0 || t.Font != lastFont {
			lastFont, lastEm = t.Font, isBoldFont(t.Font) || isItalicFont(t.Font)
		}
		if lastEm && hasContent(t.S) {
			return true
		}
		if len(links) > 0 && strings.TrimSpace(t.S) != "" && linkAt(t, links) >= 0 {
			return true
		}
	}
	return false
}

// resolveNeutral gives neutral segments the emphasis shared by the nearest content
// on both sides, and spaces the link shared by both neighbours.
func resolveNeutral(segs []inlineSeg) {
	n := len(segs)
	nextEm := make([]mdrender.Emphasis, n)
	nextLink := make([]int, n)
	em, link := mdrender.Emphasis{}, -1
	for i := n - 1; i >= 0; i-- {
		nextEm[i], nextLink[i] = em, link
		if !segs[i].neutral {
			em = segs[i].em
		}
		if !segs[i].blank {
			link = segs[i].link
		}
	}
	em, link = mdrender.Emphasis{}, -1
	for i := range segs {
		s := &segs[i]
		if s.neutral {
			s.em = mdrender.Emphasis{Bold: em.Bold && nextEm[i].Bold, Italic: em.Italic && nextEm[i].Italic}
		} else {
			em = s.em
		}
		if s.blank {
			if link != nextLink[i] {
				s.link = -1
			} else {
				s.link = link
			}
		} else {
			link = s.link
		}
	}
	// Link rectangles are drawn with some padding and often take in the full stop
	// after the linked word; sentence punctuation at the end stays outside the link.
	for i := range segs {
		l := segs[i].link
		if l < 0 || (i+1 < len(segs) && segs[i+1].link == l) {
			continue
		}
		for k := i; k >= 0 && segs[k].link == l; k-- {
			if !segs[k].neutral || strings.Trim(segs[k].text, ".,;:!? ") != "" {
				break
			}
			segs[k].link = -1
		}
	}
}

// renderSegs writes the segments through mdrender.Inline, which merges equal
// emphasis and wraps links; the emphasis of neutral segments is already resolved.
func renderSegs(segs []inlineSeg, links []linkArea, withEm bool) string {
	refs := make([]*mdrender.Link, len(links))
	var in mdrender.Inline
	for _, sg := range segs {
		var l *mdrender.Link
		if sg.link >= 0 {
			if refs[sg.link] == nil {
				uri := strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(links[sg.link].uri)
				refs[sg.link] = &mdrender.Link{URL: uri}
			}
			l = refs[sg.link]
		}
		in.Add(sg.text, sg.em, l)
	}
	return strings.Join(strings.Fields(in.Render(withEm)), " ")
}

// inlineText accumulates a paragraph or list item in its plain and marked-up forms.
// Emphasis is dropped when every line is emphasised as a whole: that is the block's
// styling (often a heading candidate), not emphasis within it.
type inlineText struct {
	plain, rich, linked string
	marked, linkedAny   bool
	styled              bool
	lines               int
}

func (t *inlineText) add(ln line) {
	text := strings.TrimSpace(ln.text)
	rich, linked := ln.rich, ln.linked
	if rich == "" {
		rich = text
	} else {
		t.marked = true
	}
	if linked == "" {
		linked = text
	} else {
		t.linkedAny = true
	}
	t.styled = (t.lines == 0 || t.styled) && ln.styled
	t.lines++
	t.plain = joinWrapped(t.plain, text)
	t.rich = joinMarked(t.rich, rich)
	t.linked = joinMarked(t.linked, linked)
}

// addPlain appends text without markup to all three forms.
func (t *inlineText) addPlain(text string) {
	t.plain = joinWrapped(t.plain, text)
	t.rich = joinWrapped(t.rich, text)
	t.linked = joinWrapped(t.linked, text)
}

func (t inlineText) String() string {
	switch {
	case t.marked && !t.styled:
		return t.rich
	case t.linkedAny:
		return t.linked
	}
	return t.plain
}

// joinMarked is joinWrapped for marked-up lines: a span closed at the end of one line
// and reopened at the start of the next is merged into one.
func joinMarked(acc, next string) string {
	for _, m := range []string{"**", "_"} {
		if len(acc) > len(m) && len(next) > len(m) && strings.HasSuffix(acc, m) && strings.HasPrefix(next, m) {
			return joinMarked(acc[:len(acc)-len(m)], next[len(m):])
		}
	}
	return joinWrapped(acc, next)
}
