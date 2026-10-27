package pdfnative

import (
	"sort"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"

	"github.com/0x0520/doc2md/internal/mdrender"
)

const (
	// cellGapFactor is the gap between tokens, in font sizes, that separates table cells.
	cellGapFactor = 1.5
	// minTableRows is the number of multi-cell rows that make a table.
	minTableRows = 2
	// continuationPitch: a row set closer than this many font sizes to the previous one is a wrapped continuation of it.
	continuationPitch = 1.5
)

// cell is a run of tokens on one row between cell-sized gaps.
type cell struct {
	startX float64
	text   string
	toks   []pdf.Text
}

// assembleRows turns rows into lines. Runs of multi-cell rows become one table line
// (pre set); runs that look like a contents list are dropped unless verbatim.
func assembleRows(rows [][]pdf.Text, verbatim bool) []line {
	cells := make([][]cell, len(rows))
	tabular := make([]bool, len(rows))
	for i, r := range rows {
		cells[i] = splitCells(r)
		tabular[i] = len(cells[i]) >= 2
	}

	var out []line
	for i := 0; i < len(rows); {
		if tabular[i] {
			fs := dominantSize(rows[i])
			if fs == 0 {
				fs = 10
			}
			tol := cellGapFactor * fs
			innerXs := innerColXs(cells[i])
			nTab := 1
			j := i + 1
			for j < len(rows) {
				if tabular[j] {
					innerXs = append(innerXs, innerColXs(cells[j])...)
					nTab++
					j++
					continue
				}
				// A one-cell row under an inner column is wrapped cell text and stays in the run, unless it is a page number.
				if len(cells[j]) == 1 && alignsToInner(cells[j][0].startX, innerXs, tol) &&
					!pageNumRe.MatchString(strings.ToLower(strings.TrimSpace(cells[j][0].text))) {
					j++
					continue
				}
				break
			}
			if nTab >= minTableRows {
				if !verbatim && isTOCRun(cells[i:j]) {
					i = j
					continue
				}
				if tbl, ok := buildTable(rows[i:j], cells[i:j]); ok {
					out = append(out, tbl)
					i = j
					continue
				}
			}
		}
		out = append(out, joinLine(rows[i]))
		i++
	}
	return out
}

// innerColXs returns the start positions of all cells but the first.
func innerColXs(cs []cell) []float64 {
	xs := make([]float64, 0, len(cs))
	for _, c := range cs[min(1, len(cs)):] {
		xs = append(xs, c.startX)
	}
	return xs
}

func alignsToInner(x float64, xs []float64, tol float64) bool {
	for _, c := range xs {
		if absF(x-c) <= tol {
			return true
		}
	}
	return false
}

// splitCells splits a row into cells at gaps wider than cellGapFactor font sizes.
func splitCells(row []pdf.Text) []cell {
	if len(row) == 0 {
		return nil
	}
	toks := byX(row)
	gap := cellGapFactor * dominantSize(toks)

	var cells []cell
	seg := []pdf.Text{toks[0]}
	for k := 1; k < len(toks); k++ {
		prev := toks[k-1]
		if toks[k].X-(prev.X+prev.W) > gap {
			cells = append(cells, mkCell(seg))
			seg = nil
		}
		seg = append(seg, toks[k])
	}
	if len(seg) > 0 {
		cells = append(cells, mkCell(seg))
	}
	return cells
}

func mkCell(seg []pdf.Text) cell {
	text, _ := joinText(seg)
	return cell{startX: seg[0].X, text: strings.TrimSpace(text), toks: seg}
}

// splitCellAtAnchors splits a cell at word gaps near known column positions, for
// tables whose columns are set no wider apart than a normal word gap.
func splitCellAtAnchors(c cell, cols []float64, tol float64) []cell {
	var out []cell
	cur := c.toks
	for _, a := range cols {
		if len(cur) < 2 {
			break
		}
		if a <= cur[0].X+tol {
			continue
		}
		last := cur[len(cur)-1]
		if last.X+last.W <= a+tol {
			break
		}
		k := wordGapAt(cur, a, tol)
		if k < 0 {
			continue
		}
		out = append(out, mkCell(cur[:k]))
		cur = cur[k:]
	}
	if len(cur) > 0 {
		out = append(out, mkCell(cur))
	}
	if len(out) == 0 {
		return []cell{c}
	}
	return out
}

// wordGapAt returns the index of the token that starts the word gap nearest to x=a
// (within tol), or -1.
func wordGapAt(toks []pdf.Text, a, tol float64) int {
	var span, sumW float64
	for k := 1; k < len(toks); k++ {
		sumW += toks[k-1].W
	}
	span = toks[len(toks)-1].X - toks[0].X
	wReliable := span > 0 && sumW > 0.4*span

	best, bestD := -1, tol
	for k := 1; k < len(toks); k++ {
		if strings.TrimSpace(toks[k].S) == "" {
			continue
		}
		prev := toks[k-1]
		if strings.TrimSpace(prev.S) != "" {
			fs := maxF(toks[k].FontSize, prev.FontSize)
			if fs == 0 {
				fs = 10
			}
			prevW := prev.W
			if !wReliable {
				prevW = estimateWidth(prev.S, prev.FontSize)
			}
			if toks[k].X-prev.X-prevW <= 0.2*fs {
				continue
			}
		}
		if d := absF(toks[k].X - a); d < bestD {
			best, bestD = k, d
		}
	}
	return best
}

// buildTable converts a run of rows into a Markdown table line. It returns false if
// fewer than two columns emerge or the grid ends up empty.
func buildTable(rows [][]pdf.Text, cells [][]cell) (line, bool) {
	repFS := dominantSize(rows[0])
	if repFS == 0 {
		repFS = 10
	}
	cols := clusterColumns(cells, 1.2*repFS)
	if len(cols) < 2 {
		return line{}, false
	}

	for i, rc := range cells {
		var refined []cell
		for _, c := range rc {
			refined = append(refined, splitCellAtAnchors(c, cols, 1.2*repFS)...)
		}
		cells[i] = refined
	}

	raw := make([][]string, len(cells))
	for i, rc := range cells {
		r := make([]string, len(cols))
		for _, c := range rc {
			ci := nearestColumn(cols, c.startX)
			if r[ci] != "" {
				r[ci] += " " + c.text
			} else {
				r[ci] = c.text
			}
		}
		raw[i] = r
	}

	// tight[i] marks a row set close to the previous one, i.e. a wrapped continuation.
	tight := make([]bool, len(rows))
	for i := 1; i < len(rows); i++ {
		fs := dominantSize(rows[i])
		if fs == 0 {
			fs = repFS
		}
		gap := rowBaseline(rows[i-1]) - rowBaseline(rows[i])
		tight[i] = gap > 0 && gap < continuationPitch*fs
	}

	grid := mdrender.NormalizeGrid(mergeContinuationRows(raw, tight))
	if len(grid) == 0 {
		return line{}, false
	}

	return line{
		text:   mdrender.Table(grid),
		y:      rows[0][0].Y,
		xStart: cols[0],
		pre:    true,
	}, true
}

// mergeContinuationRows folds wrapped rows into the previous row: tightly spaced rows
// with an empty first cell, and rows completing a hyphenated first cell.
func mergeContinuationRows(raw [][]string, tight []bool) [][]string {
	out := make([][]string, 0, len(raw))
	for i, r := range raw {
		if len(out) > 0 && rowHasText(r) {
			prev := out[len(out)-1]
			switch {
			case strings.TrimSpace(r[0]) == "" && tight[i]:
				appendCellsFrom(prev, r, 0)
				continue
			case hyphenatedKey(prev[0], r[0]):
				prev[0] = strings.TrimSuffix(strings.TrimSpace(prev[0]), "-") + strings.TrimSpace(r[0])
				appendCellsFrom(prev, r, 1)
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func appendCellsFrom(prev, r []string, start int) {
	for c := start; c < len(r) && c < len(prev); c++ {
		if strings.TrimSpace(r[c]) == "" {
			continue
		}
		if strings.TrimSpace(prev[c]) == "" {
			prev[c] = r[c]
		} else {
			prev[c] = joinWrapped(prev[c], r[c])
		}
	}
}

func hyphenatedKey(prev, cur string) bool {
	prev, cur = strings.TrimSpace(prev), strings.TrimSpace(cur)
	if !strings.HasSuffix(prev, "-") || cur == "" {
		return false
	}
	for _, run := range cur {
		return unicode.IsLower(run)
	}
	return false
}

func rowHasText(r []string) bool {
	for _, c := range r {
		if strings.TrimSpace(c) != "" {
			return true
		}
	}
	return false
}

// clusterColumns returns the column start positions: the sorted cell starts, merged
// within tol. Empty cells count only when at least two align, so stray blanks do not make columns.
func clusterColumns(cells [][]cell, tol float64) []float64 {
	var xs, emptyXs []float64
	for _, rc := range cells {
		for _, c := range rc {
			if c.text == "" {
				emptyXs = append(emptyXs, c.startX)
				continue
			}
			xs = append(xs, c.startX)
		}
	}
	sort.Float64s(emptyXs)
	for i := 0; i < len(emptyXs); {
		j := i + 1
		for j < len(emptyXs) && emptyXs[j]-emptyXs[i] <= tol {
			j++
		}
		if j-i >= 2 {
			xs = append(xs, emptyXs[i])
		}
		i = j
	}
	if len(xs) == 0 {
		return nil
	}
	sort.Float64s(xs)
	cols := []float64{xs[0]}
	for _, x := range xs[1:] {
		if x-cols[len(cols)-1] > tol {
			cols = append(cols, x)
		}
	}
	return cols
}

func nearestColumn(cols []float64, x float64) int {
	best, bestD := 0, absF(x-cols[0])
	for i := 1; i < len(cols); i++ {
		if d := absF(x - cols[i]); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}
