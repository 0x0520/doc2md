package pdfnative

import (
	"sort"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// line is one reconstructed text line with the geometry and font signals the later passes need.
// y is the baseline (larger is higher on the page), xStart the left edge, pre marks
// a finished Markdown table.
type line struct {
	text     string
	y        float64
	xStart   float64
	fontSize float64
	bold     bool
	page     int
	pre      bool
}

// page carries one source page through the pipeline: raw token rows, then
// assembled lines. num is the 1-based page number, kept even after a cover is dropped.
type page struct {
	rows   [][]pdf.Text
	lines  []line
	height float64
	num    int
	places []imgPlace
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

	sort.SliceStable(toks, func(i, j int) bool {
		if sameLine(toks[i], toks[j]) {
			return toks[i].X < toks[j].X
		}
		return toks[i].Y > toks[j].Y
	})

	var rows [][]pdf.Text
	var cur []pdf.Text
	for _, t := range toks {
		if len(cur) > 0 && !sameLine(t, cur[0]) {
			rows = append(rows, cur)
			cur = nil
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	return rows
}

func rowText(row []pdf.Text) string { return joinLine(row).text }

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
	sort.SliceStable(toks, func(i, j int) bool { return toks[i].X < toks[j].X })

	var span, sumW float64
	for i := 1; i < len(toks); i++ {
		sumW += toks[i-1].W
	}
	if len(toks) > 0 {
		span = toks[len(toks)-1].X - toks[0].X
	}
	// Reported widths are trusted only when they cover a fair part of the row's extent;
	// broken width tables would otherwise make every gap look like a space.
	wReliable := span > 0 && sumW > 0.4*span

	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			prev := toks[i-1]
			prevW := prev.W
			if !wReliable {
				prevW = estimateWidth(prev.S, prev.FontSize)
			}
			fs := maxF(t.FontSize, prev.FontSize)
			if fs == 0 {
				fs = 10
			}
			endsSpace := strings.HasSuffix(b.String(), " ")
			startsSpace := strings.HasPrefix(t.S, " ")
			if t.X-prev.X-prevW > 0.2*fs && !endsSpace && !startsSpace {
				b.WriteString(" ")
			}
		}
		b.WriteString(t.S)
	}

	text := strings.Join(strings.Fields(b.String()), " ")
	return line{
		text:     text,
		y:        toks[0].Y,
		xStart:   toks[0].X,
		fontSize: dominantSize(toks),
		bold:     dominantBold(toks),
	}
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
	for _, t := range toks {
		if strings.TrimSpace(t.S) == "" {
			continue
		}
		n := len(t.S)
		total += n
		if isBoldFont(t.Font) {
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
