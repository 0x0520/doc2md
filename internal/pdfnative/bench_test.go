package pdfnative

import (
	"fmt"
	"testing"
)

// benchPages lays out pages with a heading, two prose columns, a caption and a table,
// so every pass (rows, columns, cells, tables, headings) has work to do.
func benchPages(n int) []synthPage {
	words := []string{"the", "request", "is", "entered", "in", "tool", "and", "checked", "by", "staff", "before", "approval"}
	prose := func(seed int) string {
		s := ""
		for w := 0; len(s) < 40; w++ {
			if w > 0 {
				s += " "
			}
			s += words[(seed+w*5)%len(words)]
		}
		return s
	}
	pages := make([]synthPage, n)
	for p := range pages {
		// The heading and the caption cross the gutter, so they cut the page into bands.
		runs := []textRun{{text: fmt.Sprintf("%d Chapter number %d of the benchmark document", p+1, p+1), x: 72, yTop: 80, size: 16, bold: true}}
		for k := 0; k < 24; k++ {
			y := 120 + float64(k)*bodyLead
			runs = append(runs,
				textRun{text: prose(p + k), x: 72, yTop: y, size: 10},
				textRun{text: prose(p + k + 7), x: 320, yTop: y, size: 10},
			)
		}
		runs = append(runs, textRun{text: "The table below lists the items that were checked during the review.", x: 72, yTop: 480, size: 10})
		for r := 0; r < 12; r++ {
			y := 510 + float64(r)*bodyLead
			runs = append(runs,
				textRun{text: fmt.Sprintf("R%d", r), x: 72, yTop: y, size: 10},
				textRun{text: fmt.Sprintf("Item number %d", r), x: 200, yTop: y, size: 10},
				textRun{text: fmt.Sprintf("%d,50", r*12), x: 420, yTop: y, size: 10},
			)
		}
		pages[p] = synthPage{runs: runs}
	}
	return pages
}

// BenchmarkRender converts a 30-page synthetic document end to end.
func BenchmarkRender(b *testing.B) {
	data := buildPDF(b, benchPages(30)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Render(data); err != nil {
			b.Fatal(err)
		}
	}
}
