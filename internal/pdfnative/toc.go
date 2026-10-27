package pdfnative

import (
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

var (
	// tocNumberRe matches a cell that is only an outline number ("1", "2.1", "(3)").
	tocNumberRe = regexp.MustCompile(`^\(?\d+(?:\.\d+)*\)?$`)
	// tocPageNumRe matches a trailing page number.
	tocPageNumRe = regexp.MustCompile(`(?:^|\s)\d{1,4}$`)
	// tocNumberLabelRe matches the leading label of an entry whose number is glued to its
	// title: a number, a roman numeral or a single letter with a dot. A false positive
	// here can drop a page of real content, so it must not grow wider.
	tocNumberLabelRe = regexp.MustCompile(`(?i)^\(?\d+[a-z]?(?:\.\d+)*\)?(?:[\s.:)\]-]|$)|^[ivxlcdm]+\.\s|^[a-z]\.\s`)
	// tocLetterRe counts letters, to tell a titled entry from a bare numeric row of a data table.
	tocLetterRe = regexp.MustCompile(`\p{L}`)
	// tocLeaderRe matches a leader-dot run followed by a page number at the end of the line.
	tocLeaderRe = regexp.MustCompile(`\.{3,}\s*\d{1,4}\s*$`)
)

// isTOCLeader reports whether a line is a dot-leader contents entry. A list of
// figures can sit on a page with real content, where it would be read as headings.
func isTOCLeader(text string) bool {
	return tocLeaderRe.MatchString(strings.TrimSpace(text))
}

// isTOCEntryLabel reports whether text opens like a contents entry, with a number,
// an enumerator or a label such as "Art. 1".
func isTOCEntryLabel(text string) bool {
	return tocNumberLabelRe.MatchString(text) || isLabelHeading(text)
}

const (
	// Entry counts for a contents page. tocPageMinEntries is high so that a data table
	// never qualifies. A page right after a dropped one gets the lower tocTail bar,
	// because the last sheet of a long list holds few entries; tocTailMaxOther allows
	// that many non-entry rows (running head, page number) on it.
	tocPageMinEntries = 8
	tocTailMinEntries = 3
	tocTailMaxOther   = 3
)

// dropTOCPages removes contents pages from the front matter (the first fifth of the
// document, at least five pages). They only restate headings found later, so no
// content is lost. Pages with more than two images are kept.
func dropTOCPages(pages []page) []page {
	limit := len(pages) / 5
	if limit < 5 {
		limit = 5
	}
	lastPageOf := entryPageIndex(pages)
	kept := make([]page, 0, len(pages))
	afterTOC := false
	for pi := range pages {
		if pi < limit && len(pages[pi].places) <= 2 && isTOCPage(pages[pi].rows, pi, lastPageOf, afterTOC) {
			afterTOC = true
			continue
		}
		afterTOC = false
		kept = append(kept, pages[pi])
	}
	if len(kept) == 0 {
		return pages
	}
	return kept
}

// entryPageIndex maps each normalised row text to the last page it occurs on, to find contents entries that restate a later heading.
func entryPageIndex(pages []page) map[string]int {
	last := map[string]int{}
	for pi := range pages {
		for _, row := range pages[pi].rows {
			if k := tocEntryKey(rowText(row)); k != "" {
				last[k] = pi
			}
		}
	}
	return last
}

// tocEntryKey normalises a row for comparison by dropping leaders and the page number.
// Short keys return "" as too ambiguous.
func tocEntryKey(text string) string {
	t := strings.TrimSpace(text)
	t = tocLeaderRe.ReplaceAllString(t, "")
	t = tocPageSuffixRe.ReplaceAllString(t, "")
	t = strings.ToLower(strings.Join(strings.Fields(t), " "))
	if len(t) < 8 {
		return ""
	}
	return t
}

var tocPageSuffixRe = regexp.MustCompile(`\s+\d{1,4}$`)

// isTOCPage reports whether most rows of a page are contents entries: opening with an
// outline label and ending in a page number, or restating text found on a later page.
// continuation selects the lower bar for pages following a contents page.
func isTOCPage(rows [][]pdf.Text, pi int, lastPageOf map[string]int, continuation bool) bool {
	total, tocLike, endsPage, restated := 0, 0, 0, 0
	for _, row := range rows {
		text := strings.TrimSpace(rowText(row))
		if text == "" {
			continue
		}
		total++
		if k := tocEntryKey(text); k != "" && lastPageOf[k] > pi {
			restated++
		}
		if !tocPageNumRe.MatchString(text) {
			continue
		}
		endsPage++
		if isTOCEntryLabel(text) {
			tocLike++
		}
	}
	if continuation {
		return (tocLike >= tocTailMinEntries && total-tocLike <= tocTailMaxOther) ||
			(restated >= tocTailMinEntries && total-restated <= tocTailMaxOther)
	}
	return (tocLike >= tocPageMinEntries && tocLike*10 >= total*6) ||
		(restated >= tocPageMinEntries && restated*10 >= total*6)
}

// isTOCRun reports whether a run of table-like rows is a contents list: dotted outline
// numbers or titled labels at the start, page numbers at the end, in high shares.
func isTOCRun(cells [][]cell) bool {
	tabular, endsPage, numericStarts, titledEntries := 0, 0, 0, 0
	dotted := false
	for _, rc := range cells {
		if len(rc) < 2 {
			continue
		}
		tabular++
		first := strings.TrimSpace(rc[0].text)
		last := ""
		for k := len(rc) - 1; k >= 0; k-- {
			if t := strings.TrimSpace(rc[k].text); t != "" {
				last = t
				break
			}
		}
		if tocPageNumRe.MatchString(last) {
			endsPage++
		}
		if tocNumberRe.MatchString(first) {
			numericStarts++
			if strings.Contains(first, ".") {
				dotted = true
			}
		}
		if isTOCEntryLabel(first) && len(tocLetterRe.FindAllString(first, 5)) >= 4 {
			titledEntries++
		}
	}
	if tabular < 4 {
		return false
	}
	numeric := dotted && numericStarts >= 3 && endsPage*10 >= tabular*7
	labelled := tabular >= 6 && titledEntries*10 >= tabular*7 && endsPage*10 >= tabular*8
	return numeric || labelled
}
