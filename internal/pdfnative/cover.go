package pdfnative

import "strings"

// minPagesForCover is the shortest document that may lose a cover page.
const minPagesForCover = 3

// dropCoverPage drops the first page when the first numbered heading appears only on
// page 3 or later, which means the opening pages are title and front matter.
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
