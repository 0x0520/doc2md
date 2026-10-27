package mdrender

import (
	"strings"
	"unicode"
)

// Emphasis is the set of Markdown inline markers a run carries.
type Emphasis struct{ Bold, Italic, Strike bool }

// Link is one hyperlink; runs that share the pointer belong to its text.
type Link struct{ URL string }

// run is a piece of text with uniform formatting.
type run struct {
	text []byte
	em   Emphasis
	link *Link
}

// Inline collects the formatted text of one paragraph or table cell.
type Inline struct{ runs []run }

// Add appends text, merging it into the previous run when the formatting is the
// same: Word splits text into many runs.
func (b *Inline) Add(s string, em Emphasis, l *Link) {
	if s == "" {
		return
	}
	if n := len(b.runs); n > 0 && b.runs[n-1].em == em && b.runs[n-1].link == l {
		b.runs[n-1].text = append(b.runs[n-1].text, s...)
		return
	}
	b.runs = append(b.runs, run{text: []byte(s), em: em, link: l})
}

// Piece is text with uniform formatting. Raw text, such as a finished Markdown
// link or a picture marker, is neither merged nor emphasised.
type Piece struct {
	Text string
	Em   Emphasis
	Raw  bool
}

// Render returns the Markdown of the text. Without format, Emphasis is dropped
// (headings carry none) but links stay.
func (b *Inline) Render(format bool) string {
	return Join(pieces(b.runs, format), format)
}

func pieces(runs []run, format bool) []Piece {
	var out []Piece
	for i := 0; i < len(runs); {
		r := runs[i]
		if r.link == nil {
			out = append(out, Piece{Text: string(r.text), Em: r.em})
			i++
			continue
		}
		j := i
		var label []Piece
		for ; j < len(runs) && runs[j].link == r.link; j++ {
			label = append(label, Piece{Text: string(runs[j].text), Em: runs[j].em})
		}
		i = j
		text := Join(label, format)
		core := strings.TrimSpace(text)
		if core == "" {
			out = append(out, Piece{Text: text})
			continue
		}
		// Edge whitespace stays outside the brackets, where it separates words.
		lead, trail, _ := strings.Cut(text, core)
		out = append(out, Piece{Text: lead + "[" + core + "](" + r.link.URL + ")" + trail, Raw: true})
	}
	return out
}

// Join concatenates pieces. With format set, adjacent pieces of equal formatting
// merge and are wrapped in **bold**, _italic_ and ~~strike~~. Whitespace-only
// pieces are never emphasised themselves; they take the formatting their
// neighbours share, so "bold space bold" stays one span.
func Join(ps []Piece, format bool) string {
	var sb strings.Builder
	if !format {
		for _, p := range ps {
			sb.WriteString(p.Text)
		}
		return sb.String()
	}

	n := len(ps)
	em := make([]Emphasis, n)
	blank := make([]bool, n)
	for i, p := range ps {
		if !p.Raw {
			em[i] = p.Em
			blank[i] = strings.TrimSpace(p.Text) == ""
		}
	}
	next := make([]Emphasis, n)
	var cur Emphasis
	for i := n - 1; i >= 0; i-- {
		next[i] = cur
		if !blank[i] {
			cur = em[i]
		}
	}
	cur = Emphasis{}
	for i := range ps {
		if blank[i] {
			prev, nxt := cur, next[i]
			em[i] = Emphasis{prev.Bold && nxt.Bold, prev.Italic && nxt.Italic, prev.Strike && nxt.Strike}
		} else {
			cur = em[i]
		}
	}

	for i := 0; i < n; {
		j := i + 1
		if !ps[i].Raw {
			for j < n && !ps[j].Raw && em[j] == em[i] {
				j++
			}
		}
		var group strings.Builder
		for _, p := range ps[i:j] {
			group.WriteString(p.Text)
		}
		sb.WriteString(Emphasize(group.String(), em[i]))
		i = j
	}
	return sb.String()
}

// Emphasize wraps the text in the markers of e, keeping edge whitespace outside
// them: "** bold**" is not emphasis in Markdown.
func Emphasize(s string, e Emphasis) string {
	if e == (Emphasis{}) {
		return s
	}
	core := strings.TrimSpace(s)
	if core == "" {
		return s
	}
	lead, trail, _ := strings.Cut(s, core)
	if e.Strike {
		core = "~~" + core + "~~"
	}
	if e.Italic {
		core = "_" + core + "_"
	}
	if e.Bold {
		core = "**" + core + "**"
	}
	return lead + core + trail
}

// ActiveScheme reports whether href runs code or embeds a payload instead of
// pointing at a document. Browsers ignore leading whitespace and control
// characters and drop tabs and newlines inside the scheme, so both are removed
// before matching.
func ActiveScheme(href string) bool {
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

// Empty reports whether no text has been added.
func (b *Inline) Empty() bool { return len(b.runs) == 0 }

// Reset discards the collected text.
func (b *Inline) Reset() { b.runs = nil }
