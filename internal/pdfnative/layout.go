package pdfnative

import (
	"sort"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// line is one reconstructed text line with the geometry and font signals the later passes need.
// y is the baseline (larger is higher on the page), xStart the left edge, pre marks
// a finished Markdown table. rich and linked are the text with emphasis and links
// and with links only (empty without markup, see richText); styled marks a line
// emphasised as a whole. word2X is the left edge of the second word (0 if there is
// none); for a list item it is where the text after the marker starts.
type line struct {
	text     string
	y        float64
	xStart   float64
	word2X   float64
	fontSize float64
	bold     bool
	page     int
	pre      bool
	rich     string
	linked   string
	styled   bool
}

// page carries one source page through the pipeline: raw token rows, then
// assembled lines. num is the 1-based page number, kept even after a cover is dropped.
type page struct {
	rows   [][]pdf.Text
	lines  []line
	height float64
	num    int
	places []imgPlace
	links  []linkArea
}

// minGlyphSize drops invisible text at or below this size (points).
const minGlyphSize = 1.0

// sameLine reports whether two tokens share a baseline: the score
// 1 - 2·|ΔY|/size - 0.033·|ΔSize|/size must reach 0.75, which for equal sizes means
// |ΔY| <= 0.125·size. Word spacing is not considered here.
func sameLine(a, b pdf.Text) bool {
	maxFS := maxF(a.FontSize, b.FontSize)
	if maxFS == 0 {
		maxFS = 10
	}
	x := absF(a.Y-b.Y) / maxFS
	y := absF(a.FontSize-b.FontSize) / maxFS
	return 1-2*x-0.033*y >= 0.75
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// pageRows groups tokens into baseline rows, top to bottom and left to right. It stops
// before table assembly so running heads and footers can be removed from the rows
// first. Whitespace tokens are kept: many PDFs draw word spaces as real glyphs.
func pageRows(tokens []pdf.Text) [][]pdf.Text {
	toks := make([]pdf.Text, 0, len(tokens))
	for _, t := range tokens {
		if t.S == "" || t.FontSize <= minGlyphSize {
			continue
		}
		toks = append(toks, t)
	}

	// sameLine is not transitive, so it cannot order a sort: sort by baseline, cut a row
	// where a token leaves the tolerance of the row's top token, then order each row by x.
	sort.SliceStable(toks, func(i, j int) bool { return toks[i].Y > toks[j].Y })

	var rows [][]pdf.Text
	start := 0
	for i := 1; i <= len(toks); i++ {
		if i < len(toks) && sameLine(toks[i], toks[start]) {
			continue
		}
		row := toks[start:i:i]
		sort.SliceStable(row, func(a, b int) bool { return row[a].X < row[b].X })
		rows = append(rows, row)
		start = i
	}
	return rows
}

func rowText(row []pdf.Text) string {
	text, _ := joinText(row)
	return text
}

// rowBaseline is the baseline of the row's first token.
func rowBaseline(row []pdf.Text) float64 {
	if len(row) == 0 {
		return 0
	}
	return row[0].Y
}

// joinLine joins a row's tokens into one line, inserting a space where the gap exceeds
// 0.2 font sizes. It sorts toks by X in place.
func joinLine(toks []pdf.Text) line {
	text, word2X := joinText(toks)
	return line{
		text:     text,
		y:        toks[0].Y,
		xStart:   toks[0].X,
		word2X:   word2X,
		fontSize: dominantSize(toks),
		bold:     dominantBold(toks),
	}
}

// joinText is the text and second-word edge of joinLine, for callers that need no
// font signals. It sorts toks by X in place.
func joinText(toks []pdf.Text) (string, float64) {
	sortByX(toks)
	wReliable := widthsReliable(toks)

	var b strings.Builder
	var word2X float64
	seenWord, inGap := false, false
	for i, t := range toks {
		if i > 0 && needsSpace(toks[i-1], t, wReliable, b.String()) {
			b.WriteString(" ")
			inGap = seenWord
		}
		b.WriteString(t.S)
		switch {
		case strings.TrimSpace(t.S) == "":
			inGap = seenWord
		case !seenWord:
			seenWord = true
		case inGap && word2X == 0:
			word2X = t.X
		}
	}

	return strings.Join(strings.Fields(b.String()), " "), word2X
}

// widthsReliable reports whether the reported widths cover a fair part of the row's
// extent; broken width tables would otherwise make every gap look like a space.
func widthsReliable(toks []pdf.Text) bool {
	var span, sumW float64
	for i := 1; i < len(toks); i++ {
		sumW += toks[i-1].W
	}
	if len(toks) > 0 {
		span = toks[len(toks)-1].X - toks[0].X
	}
	return span > 0 && sumW > 0.4*span
}

// needsSpace reports whether a word space belongs between prev and t: the gap
// exceeds 0.2 font sizes and neither side is already a space. soFar is the text
// written up to prev.
func needsSpace(prev, t pdf.Text, wReliable bool, soFar string) bool {
	prevW := prev.W
	if !wReliable {
		prevW = estimateWidth(prev.S, prev.FontSize)
	}
	fs := maxF(t.FontSize, prev.FontSize)
	if fs == 0 {
		fs = 10
	}
	return t.X-prev.X-prevW > 0.2*fs && !strings.HasSuffix(soFar, " ") && !strings.HasPrefix(t.S, " ")
}

// sortByX orders tokens left to right in place. Rows come out of pageRows sorted, so
// the check usually saves the sort.
func sortByX(toks []pdf.Text) {
	if !sortedByX(toks) {
		sort.SliceStable(toks, func(i, j int) bool { return toks[i].X < toks[j].X })
	}
}

// byX returns row ordered left to right without changing it: row itself when it is
// already in order, else a sorted copy.
func byX(row []pdf.Text) []pdf.Text {
	if sortedByX(row) {
		return row
	}
	toks := append([]pdf.Text(nil), row...)
	sort.SliceStable(toks, func(i, j int) bool { return toks[i].X < toks[j].X })
	return toks
}

func sortedByX(toks []pdf.Text) bool {
	for i := 1; i < len(toks); i++ {
		if toks[i].X < toks[i-1].X {
			return false
		}
	}
	return true
}

// dominantSize returns the font size (to 0.5 pt) carrying the most characters.
func dominantSize(toks []pdf.Text) float64 {
	weight := map[float64]int{}
	for _, t := range toks {
		if strings.TrimSpace(t.S) == "" {
			continue
		}
		weight[roundHalf(t.FontSize)] += len(t.S)
	}
	return modeByWeight(weight)
}

// dominantBold reports whether at least half of the characters are in a bold face.
func dominantBold(toks []pdf.Text) bool {
	var bold, total int
	// Tokens come one per character, mostly in runs of one font.
	lastFont, lastBold := "", false
	for _, t := range toks {
		if strings.TrimSpace(t.S) == "" {
			continue
		}
		if t.Font != lastFont {
			lastFont, lastBold = t.Font, isBoldFont(t.Font)
		}
		n := len(t.S)
		total += n
		if lastBold {
			bold += n
		}
	}
	return total > 0 && bold*2 >= total
}

// isBoldFont guesses boldness from the font name.
func isBoldFont(font string) bool {
	f := strings.ToLower(font)
	return strings.Contains(f, "bold") || strings.Contains(f, "black") || strings.Contains(f, "heavy") || strings.Contains(f, "semibold")
}

// bodyFontSize returns the size carrying the most text outside tables, 12 for an empty document.
func bodyFontSize(pages []page) float64 {
	weight := map[float64]int{}
	for _, pg := range pages {
		for _, ln := range pg.lines {
			if ln.pre {
				continue
			}
			weight[roundHalf(ln.fontSize)] += len(ln.text)
		}
	}
	if len(weight) == 0 {
		return 12
	}
	return modeByWeight(weight)
}

func modeByWeight(weight map[float64]int) float64 {
	var best float64
	bestN := -1
	for v, n := range weight {
		// Ties go to the smaller value so the result is deterministic.
		if n > bestN || (n == bestN && v < best) {
			best, bestN = v, n
		}
	}
	return best
}

// estimateWidth guesses a string's width from character classes; it is used only when real widths are missing or unreliable.
func estimateWidth(s string, fontSize float64) float64 {
	total := 0.0
	for _, r := range s {
		total += charWidthFactor(r)
	}
	return total * fontSize
}

func charWidthFactor(r rune) float64 {
	switch {
	case r == ' ' || r == '\t' || r == ' ':
		return 0.28
	case strings.ContainsRune("iIl.,:;'!|ftj()[]", r):
		return 0.30
	case strings.ContainsRune("mwMW", r):
		return 0.90
	case strings.ContainsRune("rt", r):
		return 0.38
	case strings.ContainsRune("bdghopquüöä", r):
		return 0.56
	case unicode.IsUpper(r):
		return 0.70
	case unicode.IsDigit(r):
		return 0.5
	default:
		return 0.5
	}
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// roundHalf rounds to the nearest 0.5.
func roundHalf(v float64) float64 {
	return float64(int(v*2+0.5)) / 2
}
