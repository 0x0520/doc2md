package pdfnative

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// noteLabelRe matches the label of a note mark: up to three digits, up to four
// lowercase letters (Word numbers endnotes i, ii, iii) or a run of note signs.
var noteLabelRe = regexp.MustCompile(`^(?:\d{1,3}|[a-z]{1,4}|[*†‡§]{1,3})$`)

const (
	// noteMarkRatio is the largest size of a note mark relative to the text it belongs to.
	noteMarkRatio = 0.8
	// noteTextRatio is the largest size of note text relative to the body size; notes are
	// set smaller than the body, which keeps numbered body lines out.
	noteTextRatio = 0.95
)

// noteRef is a superscript mark in the body: tokens toks of row row on page pg, set
// above host, the row of the word it follows.
type noteRef struct {
	pg, row, host int
	toks          []int
	label         string
	x, w          float64
}

// noteDef is a note at the foot of a page or in the endnotes: its label, the rows it
// occupies (label row first) and its text.
type noteDef struct {
	pg, row int
	rows    []int
	label   string
	text    string
	used    bool
}

// extractNotes turns footnote and endnote marks into "[^N]" references and returns
// the "[^N]: text" definitions, numbered in order of reference. A mark counts only
// when a note with the same label follows it, so superscripts such as exponents stay
// as they are. It runs on raw rows: the mark sits on a row of its own above the text,
// and a numeric note label at the foot would otherwise be taken for a page number.
func extractNotes(pages []page) []string {
	body := rowsBodySize(pages)
	var refs []noteRef
	var defs []noteDef
	for pi := range pages {
		refs = append(refs, findNoteRefs(pages[pi].rows, pi)...)
		defs = append(defs, findNoteDefs(pages[pi].rows, pi, body)...)
	}
	if len(refs) == 0 || len(defs) == 0 {
		return nil
	}

	type insert struct {
		host int
		tok  pdf.Text
	}
	drop := make([]map[int]map[int]bool, len(pages)) // page -> row -> token index
	inserts := make([][]insert, len(pages))
	dropRows := make([]map[int]bool, len(pages))
	var out []string
	for _, r := range refs {
		d := matchNoteDef(defs, r)
		if d == nil {
			continue
		}
		d.used = true
		n := len(out) + 1
		out = append(out, "[^"+strconv.Itoa(n)+"]: "+d.text)
		host := pages[r.pg].rows[r.host]
		// The marker takes the host's baseline and no font, so it carries no emphasis.
		inserts[r.pg] = append(inserts[r.pg], insert{r.host, pdf.Text{
			S: "[^" + strconv.Itoa(n) + "]", X: r.x, Y: host[0].Y, W: r.w, FontSize: dominantSize(host),
		}})
		if drop[r.pg] == nil {
			drop[r.pg] = map[int]map[int]bool{}
		}
		if drop[r.pg][r.row] == nil {
			drop[r.pg][r.row] = map[int]bool{}
		}
		for _, k := range r.toks {
			drop[r.pg][r.row][k] = true
		}
		if dropRows[d.pg] == nil {
			dropRows[d.pg] = map[int]bool{}
		}
		for _, k := range d.rows {
			dropRows[d.pg][k] = true
		}
	}
	if len(out) == 0 {
		return nil
	}

	for pi := range pages {
		if drop[pi] == nil && dropRows[pi] == nil {
			continue
		}
		rows := pages[pi].rows
		// Marks are cut out before the markers go in, so token indices stay valid; a
		// row left without visible glyphs goes.
		empty := map[int]bool{}
		for ri, gone := range drop[pi] {
			var rest []pdf.Text
			content := false
			for k, t := range rows[ri] {
				if !gone[k] {
					rest = append(rest, t)
					content = content || strings.TrimSpace(t.S) != ""
				}
			}
			rows[ri] = rest
			empty[ri] = !content
		}
		for _, in := range inserts[pi] {
			row := append(append([]pdf.Text(nil), rows[in.host]...), in.tok)
			sort.SliceStable(row, func(a, b int) bool { return row[a].X < row[b].X })
			rows[in.host] = row
			empty[in.host] = false
		}
		kept := rows[:0:0]
		for ri, row := range rows {
			if !dropRows[pi][ri] && !empty[ri] {
				kept = append(kept, row)
			}
		}
		pages[pi].rows = kept
	}
	return out
}

// matchNoteDef returns the first unused note with r's label after r: lower on the same
// page (a footnote) or on a later page (an endnote).
func matchNoteDef(defs []noteDef, r noteRef) *noteDef {
	for i := range defs {
		d := &defs[i]
		if d.used || d.label != r.label {
			continue
		}
		if d.pg > r.pg || (d.pg == r.pg && d.row > r.host) {
			return d
		}
	}
	return nil
}

// findNoteRefs finds runs of small raised glyphs that sit directly after a word of a
// row just below them and read as a note label.
func findNoteRefs(rows [][]pdf.Text, pg int) []noteRef {
	var refs []noteRef
	for ri, row := range rows {
		for _, g := range glyphGroups(row) {
			label := groupText(row, g)
			if !noteLabelRe.MatchString(label) {
				continue
			}
			first, last := row[g[0]], row[g[len(g)-1]]
			x, w := first.X, last.X+tokenWidth(last)-first.X
			host, ok := noteHost(rows, ri, first, x)
			if !ok {
				continue
			}
			refs = append(refs, noteRef{pg: pg, row: ri, host: host, toks: g, label: label, x: x, w: w})
		}
	}
	return refs
}

// noteHost finds the row a mark at x belongs to: below the mark's row by 15 to 65% of
// its size, at least 1/noteMarkRatio times the mark's size, with a glyph ending just
// before x. Of several candidates the one with the closest glyph wins.
func noteHost(rows [][]pdf.Text, ri int, mark pdf.Text, x float64) (int, bool) {
	best, bestGap := -1, 0.0
	for k := ri + 1; k < len(rows); k++ {
		h := rows[k]
		hs := dominantSize(h)
		if hs == 0 {
			continue
		}
		dy := mark.Y - h[0].Y
		if dy > hs {
			break
		}
		if dy < 0.15*hs || dy > 0.65*hs || mark.FontSize > noteMarkRatio*hs {
			continue
		}
		var prev *pdf.Text
		for i := range h {
			if h[i].X >= x {
				break
			}
			if strings.TrimSpace(h[i].S) != "" {
				prev = &h[i]
			}
		}
		if prev == nil {
			continue
		}
		gap := x - (prev.X + tokenWidth(*prev))
		if gap < -0.15*hs || gap > 0.5*hs {
			continue
		}
		if best < 0 || absF(gap) < bestGap {
			best, bestGap = k, absF(gap)
		}
	}
	return best, best >= 0
}

// findNoteDefs finds notes: a small label opening a row of larger text, or alone on a
// row just above it, with text smaller than the body. Following rows of the text's
// size and pitch that do not open another note continue it.
func findNoteDefs(rows [][]pdf.Text, pg int, body float64) []noteDef {
	var defs []noteDef
	for ri := 0; ri < len(rows); ri++ {
		label, textRow, rest, ok := noteOpening(rows, ri)
		if !ok {
			continue
		}
		ts := dominantSize(rest)
		if ts > noteTextRatio*body {
			continue
		}
		d := noteDef{pg: pg, row: ri, label: label, rows: []int{ri}}
		if textRow != ri {
			d.rows = append(d.rows, textRow)
		}
		text, _ := joinText(append([]pdf.Text(nil), rest...))
		leftX := rows[ri][0].X
		prevY := rest[0].Y
		k := textRow + 1
		for ; k < len(rows); k++ {
			u := rows[k]
			if roundHalf(dominantSize(u)) != roundHalf(ts) || prevY-u[0].Y > 1.6*ts || u[0].X < leftX-0.5*ts {
				break
			}
			if _, _, _, opens := noteOpening(rows, k); opens {
				break
			}
			t, _ := joinText(append([]pdf.Text(nil), u...))
			text = joinWrapped(text, t)
			d.rows = append(d.rows, k)
			prevY = u[0].Y
		}
		d.text = text
		if strings.TrimSpace(text) == "" {
			continue
		}
		defs = append(defs, d)
		ri = k - 1
	}
	return defs
}

// noteOpening reports whether rows[ri] opens a note and returns its label, the row
// holding the note's first text and the text tokens.
func noteOpening(rows [][]pdf.Text, ri int) (label string, textRow int, rest []pdf.Text, ok bool) {
	row := rows[ri]
	groups := glyphGroups(row)
	if len(groups) == 0 {
		return "", 0, nil, false
	}
	g := groups[0]
	label = groupText(row, g)
	if !noteLabelRe.MatchString(label) {
		return "", 0, nil, false
	}
	ls := row[g[0]].FontSize
	after := row[g[len(g)-1]+1:]
	if strings.TrimSpace(rowText(append([]pdf.Text(nil), after...))) != "" {
		// Label and text on one row.
		if ls > noteMarkRatio*dominantSize(after) {
			return "", 0, nil, false
		}
		return label, ri, after, true
	}
	if ri+1 >= len(rows) {
		return "", 0, nil, false
	}
	next := rows[ri+1]
	ts := dominantSize(next)
	dy := row[g[0]].Y - next[0].Y
	firstX := 0.0
	for _, t := range next {
		if strings.TrimSpace(t.S) != "" {
			firstX = t.X
			break
		}
	}
	lx := row[g[0]].X
	if ts == 0 || ls > noteMarkRatio*ts || dy <= 0 || dy > 0.7*ts || firstX < lx || firstX > lx+3*ts {
		return "", 0, nil, false
	}
	return label, ri + 1, next, true
}

// glyphGroups splits a row into runs of visible glyphs of one size with no word gap
// between them; each run is a list of token indices.
func glyphGroups(row []pdf.Text) [][]int {
	var groups [][]int
	var cur []int
	for i, t := range row {
		if strings.TrimSpace(t.S) == "" {
			continue
		}
		if n := len(cur); n > 0 {
			p := row[cur[n-1]]
			if t.FontSize != p.FontSize || t.X-(p.X+tokenWidth(p)) > 0.3*t.FontSize {
				groups = append(groups, cur)
				cur = nil
			}
		}
		cur = append(cur, i)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

func groupText(row []pdf.Text, g []int) string {
	var b strings.Builder
	for _, k := range g {
		b.WriteString(row[k].S)
	}
	return strings.TrimSpace(b.String())
}

// tokenWidth is the glyph's reported width, or an estimate when there is none.
func tokenWidth(t pdf.Text) float64 {
	if t.W > 0 {
		return t.W
	}
	return estimateWidth(t.S, t.FontSize)
}
