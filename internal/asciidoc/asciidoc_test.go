package asciidoc

import (
	"strings"
	"testing"
)

func md(t *testing.T, in string) string {
	t.Helper()
	got, err := ToMarkdown([]byte(in))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return got
}

func TestToMarkdown_Headings(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "shallowest title becomes h1, nesting preserved",
			in:   "=== Overview\n\n==== CODE: Item flag\n\n===== Details",
			want: "# Overview\n\n## CODE: Item flag\n\n### Details",
		},
		{
			name: "a document that already starts at = is left where it is",
			in:   "= Title\n\n== Section",
			want: "# Title\n\n## Section",
		},
		{
			name: "a bare run of = is a block delimiter, not a title",
			in:   "====\nContent in the example block\n====",
			want: "Content in the example block",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := md(t, tc.in); got != tc.want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestToMarkdown_Table(t *testing.T) {
	in := `[width="100%",cols="18%,41%,41%",options="header",]
|===
|*CODE* |*NAME* |*NOTES*
|B |Standard item |regular
| |not given |unspecified
|===`
	want := "| **CODE** | **NAME** | **NOTES** |\n|---|---|---|\n| B | Standard item | regular |\n|  | not given | unspecified |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_RaggedTable(t *testing.T) {
	in := "|===\n|A |B |C\n|only one\n|==="
	want := "| A | B | C |\n|---|---|---|\n| only one |  |  |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_RowSpan(t *testing.T) {
	in := "|===\n|Code .3+|Umbrella term |0.42 %\n|020 |0.54 %\n|030 |1.06 %\n|==="
	want := "| Code | Umbrella term | 0.42 % |\n|---|---|---|\n| 020 | Umbrella term | 0.54 % |\n| 030 | Umbrella term | 1.06 % |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_ColSpan(t *testing.T) {
	in := "|===\n|A |B |C\n2+|together |Rest\n|==="
	want := "| A | B | C |\n|---|---|---|\n| together |  | Rest |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// A row-span specifier before the first cell (".2+|CR") repeats that cell on the next row.
func TestToMarkdown_SpecLedRow(t *testing.T) {
	in := "|===\n|Group |Code |Value\n.2+|CR |010 |0.38 %\n|020 |0.42 %\n|==="
	want := "| Group | Code | Value |\n|---|---|---|\n| CR | 010 | 0.38 % |\n| CR | 020 | 0.42 % |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_CellSpecNotASpan(t *testing.T) {
	got := md(t, "|===\n|Language |Grade\n|C++ |good\n|===")
	if !strings.Contains(got, "C++") {
		t.Errorf("cell text ending in \"+\" was read as a span: %q", got)
	}
}

func TestToMarkdown_BlockCell(t *testing.T) {
	in := "[cols=\"3*\"]\n|===\n|*CODE* |*NAME* a|\n*Base specification*\n\n*(summary)*\n\n|A |Active |yes\n|==="
	want := "| **CODE** | **NAME** | **Base specification** **(summary)** |\n|---|---|---|\n| A | Active | yes |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_ColsAttributeSetsWidth(t *testing.T) {
	in := "[cols=\"2*\"]\n|===\n|Name\n|Age\n\n|Ada\n|36\n|==="
	want := "| Name | Age |\n|---|---|\n| Ada | 36 |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// One-character cells such as "a" or "2" look like style or count specifiers but are text.
func TestToMarkdown_ShortCellIsNotASpec(t *testing.T) {
	in := "|===\n|Code |Meaning\n|a |Fixed item\n|2 |Two\n|==="
	want := "| Code | Meaning |\n|---|---|\n| a | Fixed item |\n| 2 | Two |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// "{plus}" expands to "+"; the result must not then be mistaken for a span specifier.
func TestToMarkdown_AttributeValueIsNotASpan(t *testing.T) {
	in := "|===\n|Code |Range\n|02{plus} |from two\n|03{plus} |from three\n|==="
	want := "| Code | Range |\n|---|---|\n| 02+ | from two |\n| 03+ | from three |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_EscapedPipe(t *testing.T) {
	in := "|===\n|Field |Value\n|Separator |a \\| b\n|==="
	want := "| Field | Value |\n|---|---|\n| Separator | a \\| b |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_RoleSpanKeepsText(t *testing.T) {
	got := md(t, "[.underline]#Notice# must be sent.")
	if got != "Notice must be sent." {
		t.Errorf("role markup survived or text was lost: %q", got)
	}
}

func TestToMarkdown_HardBreakInsideCell(t *testing.T) {
	in := "[cols=\"2*\"]\n|===\n|Code |Text\n|A a|\nfirst line +\nsecond line\n\n|==="
	got := md(t, in)
	if strings.Contains(got, "+") {
		t.Errorf("hard-break marker survived inside a multi-line cell: %q", got)
	}
	if !strings.Contains(got, "first line second line") {
		t.Errorf("cell text lost: %q", got)
	}
}

func TestToMarkdown_DropsBlankRows(t *testing.T) {
	in := "|===\n| |\n|A |B\n| |\n|1 |2\n|==="
	want := "| A | B |\n|---|---|\n| 1 | 2 |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_DropsMarkupKeepsText(t *testing.T) {
	in := `[[a24]]
:toc:
// a comment
[width="100%"]
This sentence stays.
include::other.adoc[]`
	want := "This sentence stays.\n[other.adoc](other.adoc)"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_VerbatimBlock(t *testing.T) {
	in := "----\n= do not convert\n|a |b\n----"
	want := "```\n= do not convert\n|a |b\n```"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestToMarkdown_Inline(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"passthrough unwraps to the literal character",
			"Region notes++[++1++]++ are meant.", "Region notes[1] are meant."},
		{"bold becomes bold, not italic", "The *field* is required.", "The **field** is required."},
		{"italic already agrees and is left alone", "The _field_ is optional.", "The _field_ is optional."},
		{"trailing line-break marker is dropped", "Period end = 15.10.98 +", "Period end = 15.10.98"},
		{"a doubled plus around spaces is not passthrough", "C++ and Java++ are old.", "C++ and Java++ are old."},
		{"a built-in character attribute becomes its character", "Value{plus}1 is meant.", "Value+1 is meant."},
		{"a document-defined attribute is left alone", "Version {productname} applies.", "Version {productname} applies."},
		{"a cross-reference keeps its link text", "See <<sec14, Value tables>> here.", "See Value tables here."},
		{"a cross-reference without text keeps its target", "See <<ref_581>> here.", "See ref_581 here."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := md(t, tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToMarkdown_Lists(t *testing.T) {
	got := md(t, "* first\n** nested\n* second\n\n. one\n. two\n.. deeper")
	want := strings.Join([]string{
		"- first", "  - nested", "- second", "",
		"1. one", "1. two", "   1. deeper",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// Without a closing delimiter the table runs to the end of the input.
func TestToMarkdown_UnclosedTable(t *testing.T) {
	got := md(t, "|===\n|A |B\n|1 |2")
	want := "| A | B |\n|---|---|\n| 1 | 2 |"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// The last cell of a body has no separator after it, so a trailing style letter is text.
func TestToMarkdown_LastCellStyleLetterIsText(t *testing.T) {
	for _, letter := range []string{"a", "e", "h", "l", "m", "d", "s", "v"} {
		in := "|===\n|Name |Grade\n|Vitamin |" + letter + "\n|==="
		want := "| Name | Grade |\n|---|---|\n| Vitamin | " + letter + " |"
		if got := md(t, in); got != want {
			t.Errorf("letter %q: got:\n%q\n\nwant:\n%q", letter, got, want)
		}
	}
}

// A style letter that touches a following separator is still a specifier.
func TestToMarkdown_StyleLetterBeforeSeparatorStaysSpec(t *testing.T) {
	in := "|===\n|Name |Grade\n|Vitamin a|C\n|==="
	want := "| Name | Grade |\n|---|---|\n| Vitamin | C |"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// Blank lines inside a verbatim block are content, not collapsible spacing.
func TestToMarkdown_VerbatimKeepsBlankLines(t *testing.T) {
	in := "----\na\n\n\nb\n----"
	want := "```\na\n\n\nb\n```"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}
