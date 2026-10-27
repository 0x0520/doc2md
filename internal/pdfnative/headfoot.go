package pdfnative

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

var (
	// pageNumRe matches a line that is only a page number: "3", "Seite 3", "p. 3", "3 / 12".
	pageNumRe = regexp.MustCompile(`^\s*(seite|page|p\.?|s\.?)?\s*\d+\s*(/\s*\d+)?\s*$`)
	// letterKeyRe reduces text to its letters, so "Seite 3" and "Seite 4" share one key.
	letterKeyRe = regexp.MustCompile(`[^\p{L}]+`)
	// folioLeadRe and folioTrailRe capture a small number at the start or end of a short
	// margin line: a page number sharing its line with a running title.
	folioLeadRe  = regexp.MustCompile(`^(\d{1,4})\s+\S`)
	folioTrailRe = regexp.MustCompile(`\S\s+(\d{1,4})$`)
)

// rowsBodySize estimates the body size from raw rows (the mode by character count), since lines do not exist yet at this stage.
func rowsBodySize(pages []page) float64 {
	weight := map[float64]int{}
	for pi := range pages {
		for _, row := range pages[pi].rows {
			for _, t := range row {
				if strings.TrimSpace(t.S) == "" {
					continue
				}
				weight[roundHalf(t.FontSize)] += len(t.S)
			}
		}
	}
	if len(weight) == 0 {
		return 12
	}
	return modeByWeight(weight)
}

// folioNums returns the candidate page numbers at the start or end of a short margin
// line. Long lines are body text and yield none.
func folioNums(text string) []int {
	if len(text) > 60 {
		return nil
	}
	var out []int
	if m := folioLeadRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			out = append(out, n)
		}
	}
	if m := folioTrailRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// marginFrac is the share of the page height at each edge searched for running
// heads and feet. It is generous because headers can sit well inside the paper
// edge; repetition across pages is what gates removal.
const marginFrac = 0.15

// dropRunningHeadFoot removes, in place, margin rows that are page numbers, that
// repeat across at least half the pages (minimum 2), or whose edge number follows
// the page number at a constant offset. Pages of unknown height are skipped.
func dropRunningHeadFoot(pages []page) {
	if len(pages) == 0 {
		return
	}

	type mark struct {
		page, idx int
		key, text string
		folios    []int
	}
	var marks []mark
	keyPages := map[string]map[int]bool{}
	body := rowsBodySize(pages)

	for pi := range pages {
		h := pages[pi].height
		if h <= 0 {
			continue
		}
		topCut, botCut := h*(1-marginFrac), h*marginFrac
		for ri, row := range pages[pi].rows {
			y := rowBaseline(row)
			if y < topCut && y > botCut {
				continue
			}
			text := rowText(row)
			single := len(splitCells(row)) == 1
			// Top margin: only single-cell rows form repeat keys, so a table header repeated on
			// every continuation page survives and the table can be rejoined. The bottom
			// margin has no such rows, and slide footers put the page number in its own cell.
			key := ""
			if single || y <= botCut {
				key = strings.ToLower(letterKeyRe.ReplaceAllString(text, ""))
			}
			trimmed := strings.TrimSpace(text)
			folios := folioNums(trimmed)
			// Larger or bold margin text is a chapter heading; its leading number could match the page number by coincidence.
			if dominantSize(row) > body*1.05 || dominantBold(row) {
				folios = nil
			}
			marks = append(marks, mark{pi, ri, key, strings.ToLower(trimmed), folios})
			if key != "" {
				if keyPages[key] == nil {
					keyPages[key] = map[int]bool{}
				}
				keyPages[key][pi] = true
			}
		}
	}

	threshold := len(pages) / 2
	if threshold < 2 {
		threshold = 2
	}

	// The printed page number differs from the physical one by a constant offset,
	// shared by at least half the pages. Body text ending in a number does not do that.
	folioPages := map[int]map[int]bool{}
	for _, m := range marks {
		for _, f := range m.folios {
			off := f - pages[m.page].num
			if folioPages[off] == nil {
				folioPages[off] = map[int]bool{}
			}
			folioPages[off][m.page] = true
		}
	}
	folioOff, folioN := 0, 0
	for off, pgs := range folioPages {
		if len(pgs) > folioN {
			folioOff, folioN = off, len(pgs)
		}
	}
	onFolioOffset := func(m mark) bool {
		for _, f := range m.folios {
			if f-pages[m.page].num == folioOff {
				return true
			}
		}
		return false
	}

	drop := map[[2]int]bool{}
	for _, m := range marks {
		if pageNumRe.MatchString(m.text) ||
			(m.key != "" && len(keyPages[m.key]) >= threshold) ||
			(folioN >= threshold && onFolioOffset(m)) {
			drop[[2]int{m.page, m.idx}] = true
		}
	}
	if len(drop) == 0 {
		return
	}

	for pi := range pages {
		kept := make([][]pdf.Text, 0, len(pages[pi].rows))
		for ri, row := range pages[pi].rows {
			if !drop[[2]int{pi, ri}] {
				kept = append(kept, row)
			}
		}
		pages[pi].rows = kept
	}
}
