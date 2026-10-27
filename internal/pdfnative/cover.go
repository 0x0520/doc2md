package pdfnative

import "strings"

const minPagesForCover = 3

func dropCoverPage(pages []page) []page {
	if len(pages) < minPagesForCover {
		return pages
	}
	firstHeadingPage := -1
	for pi := range pages {
		for _, row := range pages[pi].rows {
			if outlineHeadingRe.MatchString(strings.TrimSpace(rowText(row))) {
				firstHeadingPage = pi
				break
			}
		}
		if firstHeadingPage >= 0 {
			break
		}
	}
	if firstHeadingPage >= 2 {
		return pages[1:]
	}
	return pages
}
