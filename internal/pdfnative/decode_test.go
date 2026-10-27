package pdfnative

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLexCMap(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "hex codes keep their brackets",
			in:   "<0041> <0042>",
			want: []string{"<0041>", "<0042>"},
		},
		{
			name: "keywords and codes interleave",
			in:   "1 beginbfchar\n<20> <0020>\nendbfchar",
			want: []string{"1", "beginbfchar", "<20>", "<0020>", "endbfchar"},
		},
		{
			name: "array brackets are their own tokens",
			in:   "<01> [<0066><0069>]",
			want: []string{"<01>", "[", "<0066>", "<0069>", "]"},
		},
		{
			name: "a word ends at a bracket without whitespace",
			in:   "endbfrange<41>",
			want: []string{"endbfrange", "<41>"},
		},
		{
			name: "tabs and newlines separate like spaces",
			in:   "\t<41>\r\n<42>\n",
			want: []string{"<41>", "<42>"},
		},
		{
			name: "an unterminated hex string ends the scan",
			in:   "<0041> <0042",
			want: []string{"<0041>"},
		},
		{
			name: "empty input yields nothing",
			in:   "",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lexCMap(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("lexCMap(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLexCMapTerminates(t *testing.T) {
	for _, in := range []string{"<", "[", "]", "<<>>", strings.Repeat("<>", 500), "\x00\x01\x02"} {
		done := make(chan struct{})
		go func() { lexCMap(in); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("lexCMap(%q) did not terminate", in)
		}
	}
}

func TestCoverageGlyphs(t *testing.T) {
	format1 := be16(1, 3, 100, 200, 300)
	if got, want := coverageGlyphs(format1), []int{100, 200, 300}; !reflect.DeepEqual(got, want) {
		t.Errorf("format 1: got %v, want %v", got, want)
	}

	format2 := be16(2, 2, 10, 12, 0, 50, 51, 3)
	if got, want := coverageGlyphs(format2), []int{10, 11, 12, 50, 51}; !reflect.DeepEqual(got, want) {
		t.Errorf("format 2: got %v, want %v", got, want)
	}

	reversed := be16(2, 2, 12, 10, 0, 50, 51, 0)
	if got, want := coverageGlyphs(reversed), []int{50, 51}; !reflect.DeepEqual(got, want) {
		t.Errorf("reversed range: got %v, want %v", got, want)
	}

	if got := coverageGlyphs(be16(7, 1, 100)); got != nil {
		t.Errorf("unknown coverage format: got %v, want nil", got)
	}
}

func TestCoverageGlyphsRejectsAbsurdIndex(t *testing.T) {
	if got := coverageGlyphs(be16(2, 1, 0, 0, 0xFFFF)); got != nil && len(got) > 1<<20 {
		t.Errorf("allocated %d entries from a 10-byte table", len(got))
	}
}

func TestCoverageGlyphsTruncated(t *testing.T) {
	full := be16(1, 3, 100, 200, 300)
	for n := 0; n < len(full); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("coverageGlyphs panicked on %d truncated bytes: %v", n, r)
				}
			}()
			coverageGlyphs(full[:n])
		}()
	}
}

func be16(vals ...int) []byte {
	b := make([]byte, 0, len(vals)*2)
	for _, v := range vals {
		b = append(b, byte(v>>8), byte(v))
	}
	return b
}
