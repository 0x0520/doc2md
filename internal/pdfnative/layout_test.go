package pdfnative

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/ledongthuc/pdf"
)

func rowTexts(rows [][]pdf.Text) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		for _, t := range r {
			out[i] = append(out[i], t.S)
		}
	}
	return out
}

// TestPageRowsIndependentOfInputOrder uses a drifting baseline, where sameLine holds
// for neighbours but not end to end, which made the old single-sort comparator order
// tokens differently depending on their order in the content stream.
func TestPageRowsIndependentOfInputOrder(t *testing.T) {
	tok := func(s string, x, y float64) pdf.Text { return pdf.Text{S: s, X: x, Y: y, FontSize: 10, W: 5} }
	tokens := []pdf.Text{
		tok("a", 96, 701.8), tok("b", 88, 700.9), tok("c", 80, 700.0), tok("d", 72, 699.9),
		tok("e", 72, 686), tok("f", 80, 686.4), tok("g", 88, 685.7),
		tok("h", 72, 672),
	}
	want := [][]string{{"b", "a"}, {"d", "c"}, {"e", "f", "g"}, {"h"}}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		in := append([]pdf.Text(nil), tokens...)
		rng.Shuffle(len(in), func(a, b int) { in[a], in[b] = in[b], in[a] })
		if got := rowTexts(pageRows(in)); !reflect.DeepEqual(got, want) {
			t.Fatalf("input order %v: rows %v, want %v", rowTexts([][]pdf.Text{in}), got, want)
		}
	}
}
