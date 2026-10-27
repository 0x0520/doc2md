package pdfnative

import (
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

var (
	tocNumberRe      = regexp.MustCompile(`^\(?\d+(?:\.\d+)*\)?$`)
	tocPageNumRe     = regexp.MustCompile(`(?:^|\s)\d{1,4}$`)
	tocNumberLabelRe = regexp.MustCompile(`(?i)^\(?\d+[a-z]?(?:\.\d+)*\)?(?:[\s.:)\]-]|$)|^[ivxlcdm]+\.\s|^[a-z]\.\s`)
	tocLetterRe      = regexp.MustCompile(`\p{L}`)
	tocLeaderRe      = regexp.MustCompile(`\.{3,}\s*\d{1,4}\s*$`)
)

func isTOCLeader(text string) bool {
	return tocLeaderRe.MatchString(strings.TrimSpace(text))
}

func isTOCEntryLabel(text string) bool {
	return tocNumberLabelRe.MatchString(text) || isLabelHeading(text)
}

const (
	tocPageMinEntries = 8
	tocTailMinEntries = 3
	tocTailMaxOther   = 3
)

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
