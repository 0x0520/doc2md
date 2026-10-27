package pdfnative

import (
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func TestIsListItem(t *testing.T) {
	for s, want := range map[string]bool{
		"a) Second level":         true,
		"b. Second level":         true,
		"iv) Fourth item":         true,
		"1) First level":          true,
		"z. B. ein Zeilenanfang":  false,
		"d. h. die Frist beginnt": false,
		"A. Smith wrote this":     false,
		"ab) not a marker":        false,
	} {
		if got := isListItem(s); got != want {
			t.Errorf("isListItem(%q) = %v, want %v", s, got, want)
		}
	}
}

// glyphs lays s out one token per character from x, with the reader's width estimate.
func glyphs(s string, x, y, size float64) []pdf.Text {
	var out []pdf.Text
	for _, r := range s {
		w := estimateWidth(string(r), size)
		out = append(out, pdf.Text{S: string(r), X: x, Y: y, W: w, FontSize: size, Font: "Helvetica"})
		x += w
	}
	return out
}

func rowsText(rows [][]pdf.Text) []string {
	var out []string
	for _, r := range rows {
		out = append(out, rowText(append([]pdf.Text(nil), r...)))
	}
	return out
}

func TestExtractNotes(t *testing.T) {
	word := glyphs("Area in m", 72, 700, 10)
	end := word[len(word)-1].X + word[len(word)-1].W
	tests := []struct {
		name     string
		rows     [][]pdf.Text
		wantDefs []string
		wantRows []string
	}{
		{
			name: "mark with a note becomes a reference",
			rows: [][]pdf.Text{
				glyphs("1", end, 703.5, 6),
				append(word, glyphs(" is large.", end+3, 700, 10)...),
				glyphs("Body text in the body size fills the page.", 72, 686, 10),
				glyphs("1", 72, 83.5, 6),
				glyphs("The note.", 76, 80, 8),
			},
			wantDefs: []string{"[^1]: The note."},
			wantRows: []string{"Area in m[^1] is large.", "Body text in the body size fills the page."},
		},
		{
			name: "an exponent without a note stays",
			rows: [][]pdf.Text{
				glyphs("2", end, 703.5, 6),
				append(word, glyphs(" is large.", end+3, 700, 10)...),
				glyphs("Body text in the body size fills the page.", 72, 686, 10),
			},
			wantRows: []string{"2", "Area in m is large.", "Body text in the body size fills the page."},
		},
		{
			name: "a note at body size is body text",
			rows: [][]pdf.Text{
				glyphs("1", end, 703.5, 6),
				append(word, glyphs(" is large.", end+3, 700, 10)...),
				glyphs("1", 72, 83.5, 6),
				glyphs("A numbered line in the body size.", 76, 80, 10),
			},
			wantRows: []string{"1", "Area in m is large.", "1", "A numbered line in the body size."},
		},
		{
			name: "a mark away from any word is no reference",
			rows: [][]pdf.Text{
				glyphs("1", end+80, 703.5, 6),
				append(word, glyphs(" is large.", end+3, 700, 10)...),
				glyphs("Body text in the body size fills the page.", 72, 686, 10),
				glyphs("1", 72, 83.5, 6),
				glyphs("The note.", 76, 80, 8),
			},
			wantRows: []string{"1", "Area in m is large.", "Body text in the body size fills the page.", "1", "The note."},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pages := []page{{rows: tc.rows}}
			defs := extractNotes(pages)
			if strings.Join(defs, "|") != strings.Join(tc.wantDefs, "|") {
				t.Errorf("defs = %q, want %q", defs, tc.wantDefs)
			}
			if got := rowsText(pages[0].rows); strings.Join(got, "|") != strings.Join(tc.wantRows, "|") {
				t.Errorf("rows = %q, want %q", got, tc.wantRows)
			}
		})
	}
}

func TestOrderNearBaselines(t *testing.T) {
	box := glyphs("Text inside a box.", 300, 584.5, 12)
	text := glyphs("A sentence the box is laid over.", 72, 582.8, 12)
	next := glyphs("The next line.", 72, 568, 12)
	got := rowsText(orderNearBaselines([][]pdf.Text{box, text, next}))
	want := []string{"A sentence the box is laid over.", "Text inside a box.", "The next line."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("order = %q, want %q", got, want)
	}
}
