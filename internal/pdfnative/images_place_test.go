package pdfnative

import (
	"strings"
	"testing"
)

func TestInsertMarkerLineByBaseline(t *testing.T) {
	tests := []struct {
		name string
		topY float64
		want []string
	}{
		{"above everything", 900, []string{"[Image: 1]", "Title", "Paragraph", "Footer"}},
		{"between title and paragraph", 700, []string{"Title", "[Image: 1]", "Paragraph", "Footer"}},
		{"between paragraph and footer", 300, []string{"Title", "Paragraph", "[Image: 1]", "Footer"}},
		{"below everything", 50, []string{"Title", "Paragraph", "Footer", "[Image: 1]"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pg := &page{lines: []line{
				{text: "Title", y: 800},
				{text: "Paragraph", y: 500},
				{text: "Footer", y: 100},
			}}
			insertMarkerLine(pg, 1, tc.topY)
			var got []string
			for _, l := range pg.lines {
				got = append(got, l.text)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInsertMarkerLineIntoEmptyPage(t *testing.T) {
	pg := &page{}
	insertMarkerLine(pg, 3, 400)
	if len(pg.lines) != 1 || pg.lines[0].text != "[Image: 3]" {
		t.Errorf("got %+v, want a single [Image: 3] line", pg.lines)
	}
}

func TestInsertMarkerLineKeepsOrderAcrossFigures(t *testing.T) {
	pg := &page{lines: []line{{text: "Paragraph", y: 500}}}
	insertMarkerLine(pg, 2, 200)
	insertMarkerLine(pg, 1, 800)
	var got []string
	for _, l := range pg.lines {
		got = append(got, l.text)
	}
	want := "[Image: 1]|Paragraph|[Image: 2]"
	if strings.Join(got, "|") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

func TestMarkerForMatchesTheConvention(t *testing.T) {
	if got := markerFor(1); got != "[Image: 1]" {
		t.Errorf("markerFor(1) = %q", got)
	}
	if got := markerFor(42); got != "[Image: 42]" {
		t.Errorf("markerFor(42) = %q", got)
	}
}
