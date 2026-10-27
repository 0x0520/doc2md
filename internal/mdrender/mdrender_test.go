package mdrender

import "testing"

func TestTable(t *testing.T) {
	got := Table([][]string{
		{"A", "B"},
		{"1", "2 | 3"},
		{"multi\nline", "x"},
	})
	want := "| A | B |\n" +
		"|---|---|\n" +
		"| 1 | 2 \\| 3 |\n" +
		"| multi line | x |"
	if got != want {
		t.Errorf("Table mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestTable_Empty(t *testing.T) {
	if got := Table(nil); got != "" {
		t.Errorf("Table(nil) = %q, want empty", got)
	}
	if got := Table([][]string{{}}); got != "" {
		t.Errorf("Table(zero-col) = %q, want empty", got)
	}
}

func TestNormalizeGrid(t *testing.T) {
	in := [][]string{
		{"a", "", "c", ""},
		{"", "", "", ""},
		{"d", "", "f"},
	}
	got := NormalizeGrid(in)
	want := [][]string{
		{"a", "c"},
		{"d", "f"},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("row %d cols = %d, want %d (%v)", i, len(got[i]), len(want[i]), got[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("cell [%d][%d] = %q, want %q", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestDocument_Spacing(t *testing.T) {
	got := Document([]Block{
		{Kind: Heading, Level: 2, Text: "Title"},
		{Kind: Paragraph, Text: "Body."},
		{Kind: List, Text: "- one"},
		{Kind: List, Text: "- two"},
		{Kind: Paragraph, Text: "After."},
	})
	want := "# Title\n\nBody.\n\n- one\n- two\n\nAfter.\n"
	if got != want {
		t.Errorf("Document mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestDocument_HeadingNormalisation(t *testing.T) {
	tests := []struct {
		name string
		in   []Block
		want string
	}{
		{"shallowest becomes h1, distance preserved",
			[]Block{{Kind: Heading, Level: 3, Text: "A"}, {Kind: Heading, Level: 5, Text: "B"}},
			"# A\n\n### B\n"},
		{"a document already starting at h1 is untouched",
			[]Block{{Kind: Heading, Level: 1, Text: "A"}, {Kind: Heading, Level: 2, Text: "B"}},
			"# A\n\n## B\n"},
		{"a lone heading past the clamp is the top level",
			[]Block{{Kind: Heading, Level: 9, Text: "Deep"}},
			"# Deep\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Document(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDocumentNumbersLazyOrderedLists(t *testing.T) {
	item := func(s string) Block { return Block{Kind: List, Text: s} }
	got := Document([]Block{
		item("- a"), item("- b"),
		item("1. one"), item("  - nested"), item("1. two"), item("  1. sub one"), item("  1. sub two"), item("1. three"),
		{Kind: Paragraph, Text: "between"},
		item("3. kept"), item("4. as written"),
	})
	want := "- a\n- b\n\n1. one\n   - nested\n2. two\n   1. sub one\n   2. sub two\n3. three\n\nbetween\n\n3. kept\n4. as written\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
