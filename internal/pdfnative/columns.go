package pdfnative

import (
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const (
	// Prose-column detection thresholds. A side is a candidate column only if it has
	// colMinSideRows rows, its median line fills colMinFill of the p90 line width, is
	// at least colMinWidthFS font sizes wide, and its median line pitch is within
	// colMaxLeadFS font sizes. colMinClearFS is the minimum gutter width.
	colMinSideRows = 9
	colMinFill     = 0.55
	colMinWidthFS  = 8.0
	colMaxLeadFS   = 2.4
	colMinClearFS  = 0.3
	colScanSteps   = 64
	colMaxDepth    = 2
	// colSegGapFactor is tighter than a table cell gap (cellGapFactor): some layouts leave
	// barely a font size between columns, while word gaps stay near 0.3.
	colSegGapFactor = 1.0
)

// colSeg is a run of tokens without a cell-sized gap, keeping both x edges.
type colSeg struct{ x0, x1 float64 }

// splitColumnRows puts the rows of multi-column pages into reading order, left column
// first. It only acts when both sides look like dense prose, so a two-column
// table is left alone; when in doubt the rows are returned unchanged.
func splitColumnRows(rows [][]pdf.Text) [][]pdf.Text {
	return splitColumnsDepth(rows, 0)
}

func splitColumnsDepth(rows [][]pdf.Text, depth int) [][]pdf.Text {
	if depth >= colMaxDepth || len(rows) < 2*colMinSideRows {
		return rows
	}
	segs := make([][]colSeg, len(rows))
	for i, row := range rows {
		segs[i] = rowSegs(row)
	}
	g, ok := findGutter(segs)
	if !ok {
		return rows
	}

	var out [][]pdf.Text
	var band [][]pdf.Text
	flush := func() {
		if len(band) > 0 {
			out = append(out, reorderBand(band, g, depth)...)
			band = nil
		}
	}
	for i, row := range rows {
		// Rows crossing the gutter (titles, spanning captions) cut the page into bands that are reordered separately.
		if segsCross(segs[i], g) {
			flush()
			out = append(out, row)
			continue
		}
		band = append(band, row)
	}
	flush()
	return out
}

// rowSegs splits a row into horizontal segments at gaps wider than colSegGapFactor font sizes.
func rowSegs(row []pdf.Text) []colSeg {
	if len(row) == 0 {
		return nil
	}
	toks := byX(row)
	gap := colSegGapFactor * dominantSize(toks)

	var segs []colSeg
	cur := colSeg{x0: toks[0].X, x1: toks[0].X + toks[0].W}
	for _, t := range toks[1:] {
		if t.X-cur.x1 > gap {
			segs = append(segs, cur)
			cur = colSeg{x0: t.X, x1: t.X + t.W}
			continue
		}
		if end := t.X + t.W; end > cur.x1 {
			cur.x1 = end
		}
	}
	return append(segs, cur)
}

func segsCross(segs []colSeg, g float64) bool {
	for _, s := range segs {
		if s.x0 < g && s.x1 > g {
			return true
		}
	}
	return false
}

// findGutter scans x positions across the middle 30-70% of the text extent for the one
// that separates the most rows into a left and a right population. It is purely
// geometric; the content checks are in reorderBand.
func findGutter(segs [][]colSeg) (float64, bool) {
	minX, maxX := 1e18, -1e18
	for _, ss := range segs {
		for _, s := range ss {
			if s.x0 < minX {
				minX = s.x0
			}
			if s.x1 > maxX {
				maxX = s.x1
			}
		}
	}
	if maxX-minX <= 0 {
		return 0, false
	}

	bestG, bestScore := 0.0, 0
	lo, hi := minX+0.30*(maxX-minX), minX+0.70*(maxX-minX)
	for step := 0; step <= colScanSteps; step++ {
		g := lo + (hi-lo)*float64(step)/colScanSteps
		left, right, cross := 0, 0, 0
		for _, ss := range segs {
			switch {
			case segsCross(ss, g):
				cross++
			// Segments on both sides but none crossing: columns sharing a baseline.
			case sidesOccupied(ss, g):
				left++
				right++
			case allLeft(ss, g):
				left++
			default:
				right++
			}
		}
		score := left
		if right < score {
			score = right
		}
		// Spanning rows must stay a minority: on a single-column page nearly every row crosses any candidate gutter.
		if score >= colMinSideRows && cross <= score && score > bestScore {
			bestG, bestScore = g, score
		}
	}
	return bestG, bestScore > 0
}

func sidesOccupied(segs []colSeg, g float64) bool {
	left, right := false, false
	for _, s := range segs {
		if s.x1 <= g {
			left = true
		}
		if s.x0 >= g {
			right = true
		}
	}
	return left && right
}

func allLeft(segs []colSeg, g float64) bool {
	for _, s := range segs {
		if s.x1 > g {
			return false
		}
	}
	return true
}

// reorderBand returns a band's rows left column first if it passes the prose checks,
// otherwise unchanged. Rows with text on both sides are split into halves, and each
// side is split again for three or more columns.
func reorderBand(band [][]pdf.Text, g float64, depth int) [][]pdf.Text {
	var leftRows, rightRows [][]pdf.Text
	var leftWidths, rightWidths, leftYs, rightYs []float64
	fs := 0.0

	for _, row := range band {
		var left, right []pdf.Text
		for _, t := range row {
			if t.X+t.W/2 < g {
				left = append(left, t)
			} else {
				right = append(right, t)
			}
		}
		if f := dominantSize(row); f > fs {
			fs = f
		}
		if len(left) > 0 {
			leftRows = append(leftRows, left)
			leftWidths = append(leftWidths, spanWidth(left))
			leftYs = append(leftYs, left[0].Y)
		}
		if len(right) > 0 {
			rightRows = append(rightRows, right)
			rightWidths = append(rightWidths, spanWidth(right))
			rightYs = append(rightYs, right[0].Y)
		}
	}
	if fs == 0 {
		fs = 10
	}

	// Guards against reading a two-column table as text columns: both sides must be
	// balanced, wide, evenly filled, regularly spaced, one text run per line, and
	// start many lines lowercase (prose wraps mid-sentence).
	proseColumns := len(leftRows) >= colMinSideRows && len(rightRows) >= colMinSideRows &&
		balanced(len(leftRows), len(rightRows)) &&
		clearance(leftRows, rightRows) >= colMinClearFS*fs &&
		medianF(leftWidths) >= colMinWidthFS*fs && medianF(rightWidths) >= colMinWidthFS*fs &&
		fillRatio(leftWidths) >= colMinFill && fillRatio(rightWidths) >= colMinFill &&
		medianLeading(leftYs) <= colMaxLeadFS*fs && medianLeading(rightYs) <= colMaxLeadFS*fs &&
		multiSegFrac(leftRows, fs) <= 0.2 && multiSegFrac(rightRows, fs) <= 0.2 &&
		lowercaseStartFrac(leftRows) >= 0.3 && lowercaseStartFrac(rightRows) >= 0.3
	if !proseColumns {
		return band
	}
	return append(
		splitColumnsDepth(leftRows, depth+1),
		splitColumnsDepth(rightRows, depth+1)...,
	)
}

// multiSegFrac is the share of rows containing a cell-sized gap: near 0 for a prose
// column, high for the inner half of a wider table.
func multiSegFrac(rows [][]pdf.Text, fs float64) float64 {
	if len(rows) == 0 {
		return 1
	}
	gap := cellGapFactor * fs
	multi := 0
	for _, row := range rows {
		toks := byX(row)
		for i := 1; i < len(toks); i++ {
			if toks[i].X-(toks[i-1].X+toks[i-1].W) > gap {
				multi++
				break
			}
		}
	}
	return float64(multi) / float64(len(rows))
}

// lowercaseStartFrac is the share of rows starting with a lowercase letter. A table's key column starts records with capitals.
func lowercaseStartFrac(rows [][]pdf.Text) float64 {
	if len(rows) == 0 {
		return 0
	}
	lower := 0
	for _, row := range rows {
		if r, _ := utf8.DecodeRuneInString(rowText(row)); unicode.IsLower(r) {
			lower++
		}
	}
	return float64(lower) / float64(len(rows))
}

// clearance is the realised gutter width. The 5th/95th percentiles ignore stray outlier rows.
func clearance(leftRows, rightRows [][]pdf.Text) float64 {
	rightEdges := make([]float64, 0, len(leftRows))
	for _, r := range leftRows {
		rightEdges = append(rightEdges, maxEnd(r))
	}
	leftEdges := make([]float64, 0, len(rightRows))
	for _, r := range rightRows {
		leftEdges = append(leftEdges, minStart(r))
	}
	return quantileF(leftEdges, 0.05) - quantileF(rightEdges, 0.95)
}

func spanWidth(toks []pdf.Text) float64 { return maxEnd(toks) - minStart(toks) }

func minStart(toks []pdf.Text) float64 {
	v := 1e18
	for _, t := range toks {
		if t.X < v {
			v = t.X
		}
	}
	return v
}

func maxEnd(toks []pdf.Text) float64 {
	v := -1e18
	for _, t := range toks {
		if e := t.X + t.W; e > v {
			v = e
		}
	}
	return v
}

// fillRatio is median line width over p90 width: near 1 for justified prose, low for ragged table cells.
func fillRatio(widths []float64) float64 {
	if len(widths) == 0 {
		return 0
	}
	wide := quantileF(widths, 0.9)
	if wide <= 0 {
		return 0
	}
	return medianF(widths) / wide
}

// medianLeading is the median baseline distance; it returns a huge value when it cannot be measured, so the leading check fails.
func medianLeading(ys []float64) float64 {
	if len(ys) < 2 {
		return 1e18
	}
	sorted := append([]float64(nil), ys...)
	sort.Float64s(sorted)
	deltas := make([]float64, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		if d := sorted[i] - sorted[i-1]; d > 0.1 {
			deltas = append(deltas, d)
		}
	}
	if len(deltas) == 0 {
		return 1e18
	}
	return medianF(deltas)
}

// balanced reports whether the larger count is at most three times the smaller.
func balanced(a, b int) bool {
	if a == 0 || b == 0 {
		return false
	}
	if a > b {
		a, b = b, a
	}
	return 3*a >= b
}

func medianF(vs []float64) float64 { return quantileF(vs, 0.5) }

// quantileF returns the q-quantile by index, without interpolation.
func quantileF(vs []float64, q float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vs...)
	sort.Float64s(sorted)
	i := int(q * float64(len(sorted)-1))
	return sorted[i]
}
