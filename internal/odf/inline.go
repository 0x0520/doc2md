package odf

import (
	"strings"
	"unicode"
)

// emphasis is the set of Markdown inline markers a run carries.
type emphasis struct{ bold, italic, strike bool }

// link is one hyperlink; runs that share the pointer belong to its text.
type link struct{ url string }

// run is a piece of text with uniform formatting.
type run struct {
	text []byte
	em   emphasis
	link *link
}

// inline collects the formatted text of one paragraph or table cell.
type inline struct{ runs []run }

// add appends text, merging it into the previous run when the formatting is the
// same: Word splits text into many runs.
func (b *inline) add(s string, em emphasis, l *link) {
	if s == "" {
		return
	}
	if n := len(b.runs); n > 0 && b.runs[n-1].em == em && b.runs[n-1].link == l {
		b.runs[n-1].text = append(b.runs[n-1].text, s...)
		return
	}
	b.runs = append(b.runs, run{text: []byte(s), em: em, link: l})
}

// piece is a run after links are folded in. raw text is a finished Markdown link
// and is neither merged nor emphasised.
type piece struct {
	text string
	em   emphasis
	raw  bool
}

// render returns the Markdown of the text. Without format, emphasis is dropped
// (headings carry none) but links stay.
func (b *inline) render(format bool) string {
	return join(pieces(b.runs, format), format)
}

func pieces(runs []run, format bool) []piece {
	var out []piece
	for i := 0; i < len(runs); {
		r := runs[i]
		if r.link == nil {
			out = append(out, piece{text: string(r.text), em: r.em})
			i++
			continue
		}
		j := i
		var label []piece
		for ; j < len(runs) && runs[j].link == r.link; j++ {
			label = append(label, piece{text: string(runs[j].text), em: runs[j].em})
		}
		i = j
		text := join(label, format)
		core := strings.TrimSpace(text)
		if core == "" {
			out = append(out, piece{text: text})
			continue
		}
		// Edge whitespace stays outside the brackets, where it separates words.
		lead, trail, _ := strings.Cut(text, core)
		out = append(out, piece{text: lead + "[" + core + "](" + r.link.url + ")" + trail, raw: true})
	}
	return out
}

// join concatenates pieces. With format set, adjacent pieces of equal formatting
// merge and are wrapped in **bold**, _italic_ and ~~strike~~. Whitespace-only
// pieces are never emphasised themselves; they take the formatting their
// neighbours share, so "bold space bold" stays one span.
func join(ps []piece, format bool) string {
	var sb strings.Builder
	if !format {
		for _, p := range ps {
			sb.WriteString(p.text)
		}
		return sb.String()
	}

	n := len(ps)
	em := make([]emphasis, n)
	blank := make([]bool, n)
	for i, p := range ps {
		if !p.raw {
			em[i] = p.em
			blank[i] = strings.TrimSpace(p.text) == ""
		}
	}
	next := make([]emphasis, n)
	var cur emphasis
	for i := n - 1; i >= 0; i-- {
		next[i] = cur
		if !blank[i] {
			cur = em[i]
		}
	}
	cur = emphasis{}
	for i := range ps {
		if blank[i] {
			prev, nxt := cur, next[i]
			em[i] = emphasis{prev.bold && nxt.bold, prev.italic && nxt.italic, prev.strike && nxt.strike}
		} else {
			cur = em[i]
		}
	}

	for i := 0; i < n; {
		j := i + 1
		if !ps[i].raw {
			for j < n && !ps[j].raw && em[j] == em[i] {
				j++
			}
		}
		var group strings.Builder
		for _, p := range ps[i:j] {
			group.WriteString(p.text)
		}
		sb.WriteString(emphasize(group.String(), em[i]))
		i = j
	}
	return sb.String()
}

// emphasize wraps the text in the markers of e, keeping edge whitespace outside
// them: "** bold**" is not emphasis in Markdown.
func emphasize(s string, e emphasis) string {
	if e == (emphasis{}) {
		return s
	}
	core := strings.TrimSpace(s)
	if core == "" {
		return s
	}
	lead, trail, _ := strings.Cut(s, core)
	if e.strike {
		core = "~~" + core + "~~"
	}
	if e.italic {
		core = "_" + core + "_"
	}
	if e.bold {
		core = "**" + core + "**"
	}
	return lead + core + trail
}

// activeScheme reports whether href runs code or embeds a payload instead of
// pointing at a document. Browsers ignore leading whitespace and control
// characters and drop tabs and newlines inside the scheme, so both are removed
// before matching.
func activeScheme(href string) bool {
	clean := strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f {
			return -1
		}
		return unicode.ToLower(r)
	}, href)
	for _, scheme := range []string{"javascript:", "vbscript:", "data:"} {
		if strings.HasPrefix(clean, scheme) {
			return true
		}
	}
	return false
}

// linkTarget returns href if it is a link worth rendering: not empty, not an
// address within the document (those carry editor-internal names) and not an
// active target.
func linkTarget(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || activeScheme(href) {
		return ""
	}
	return href
}
