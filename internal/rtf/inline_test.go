package rtf

import (
	"fmt"
	"testing"
)

func TestInlineEmphasis(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"bold italic strike", `A {\b bold}, {\i italic} and {\strike gone}.`, "A **bold**, _italic_ and ~~gone~~."},
		{"nesting order", `{\strike\i\b all}`, "**_~~all~~_**"},
		{"switched off", `\b one \b0 two \i three\i0  four`, "**one** two _three_ four"},
		{"group scope restores state", `{\b one}{\i two} three`, "**one**_two_ three"},
		{"plain resets", `\b\i one\plain  two`, "**_one_** two"},
		{"explicit off argument", `\b1 on \b0 off`, "**on** off"},
		{"double strike", `{\striked1 gone}`, "~~gone~~"},
		{"adjacent runs merge", `{\b one}{\b  two}{\b  three}`, "**one two three**"},
		{"edge whitespace outside", `x{\b  bold }y`, "x **bold** y"},
		{"whitespace-only run inside a span", `{\b one}{ }{\b two}`, "**one two**"},
		{"whitespace-only run alone", `a{\b  }b`, "a b"},
		{"bold ends at escape", `{\b caf\'e9 }ok`, "**café** ok"},
		{"formatting ignored in skipped destinations", `{\*\foo \b hidden}text`, "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := md(t, `{\rtf1\ansi\pard `+tc.body+`\par}`)
			if got != tc.want+"\n" {
				t.Errorf("got %q, want %q", got, tc.want+"\n")
			}
		})
	}
}

func TestEmphasisStaysOutOfHeadings(t *testing.T) {
	in := `{\rtf1\ansi\pard\outlinelevel0 {\b Bold} heading\par` +
		`\pard {\b Body} text\par}`
	want := "# Bold heading\n\n**Body** text\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEmphasisInTableCell(t *testing.T) {
	in := `{\rtf1\ansi\trowd {\b Head}\cell Other\cell\row\trowd {\i one}\cell two\cell\row}`
	want := "| **Head** | Other |\n|---|---|\n| _one_ | two |\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCharacterStyleEmphasis(t *testing.T) {
	in := `{\rtf1\ansi{\stylesheet{\s0 Normal;}{\*\cs10 Default Paragraph Font;}` +
		`{\*\cs15 \additive \b Strong;}{\*\cs16 \additive \i\strike Odd;}{\*\cs17 \additive \b0 Plain;}}` +
		`\pard {\cs15 loud} and {\cs16 odd} and {\cs10 normal} and {\cs17 plain}\par}`
	want := "**loud** and _~~odd~~_ and normal and plain\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Word's layout: instruction in a group, binary \datafield inside it, result in a second group.
const wordLink = `{\field\fldedit{\*\fldinst {\rtlch\fcs1 \af31507 HYPERLINK "%s"}{\rtlch {\*\datafield 00d0c9ea79f9bace}}}` +
	`{\fldrslt {\cs42\ul\cf24 %s}}}`

func linkDoc(url, label string) string {
	return `{\rtf1\ansi\pard See ` + fmt.Sprintf(wordLink, url, label) + ` now.\par}`
}

func TestHyperlink(t *testing.T) {
	got := md(t, linkDoc("https://example.org/spec", "the spec"))
	want := "See [the spec](https://example.org/spec) now.\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHyperlinkKeepsEmphasisInsideBrackets(t *testing.T) {
	in := `{\rtf1\ansi\pard {\field{\*\fldinst HYPERLINK "https://example.org/"}{\fldrslt {\b bold} and {\i italic} }}tail\par}`
	want := "[**bold** and _italic_](https://example.org/) tail\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHyperlinkInHeadingHasNoEmphasis(t *testing.T) {
	in := `{\rtf1\ansi\pard\outlinelevel0 {\field{\*\fldinst HYPERLINK "https://example.org/"}{\fldrslt {\b Title}}}\par}`
	want := "# [Title](https://example.org/)\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestActiveHyperlinkTargetsAreText(t *testing.T) {
	for _, url := range []string{
		"javascript:alert(1)", "JavaScript:alert(1)", " vbscript:x", "DATA:text/html,x", "java\tscript:x",
	} {
		got := md(t, linkDoc(url, "click"))
		if want := "See click now.\n"; got != want {
			t.Errorf("%q: got %q, want %q", url, got, want)
		}
	}
}

func TestHyperlinkSwitches(t *testing.T) {
	tests := []struct{ name, code, want string }{
		{"tooltip and target", `HYPERLINK "https://example.org/" \\o "Tip" \\t "_blank"`, "[x](https://example.org/)"},
		{"bookmark only", `HYPERLINK \\l "_Toc1"`, "x"},
		{"bookmark after target", `HYPERLINK "https://example.org/" \\l "frag"`, "[x](https://example.org/)"},
		{"mail", `HYPERLINK "mailto:a@example.org"`, "[x](mailto:a@example.org)"},
		{"lower case keyword", `hyperlink "https://example.org/"`, "[x](https://example.org/)"},
		{"escaped byte", `HYPERLINK "https://example.org/caf\'e9"`, "[x](https://example.org/café)"},
		{"other field", `PAGEREF _Toc1 \\h`, "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := `{\rtf1\ansi\pard {\field{\*\fldinst ` + tc.code + `}{\fldrslt x}}\par}`
			if got := md(t, in); got != tc.want+"\n" {
				t.Errorf("got %q, want %q", got, tc.want+"\n")
			}
		})
	}
}

// A link must not leak into text after its field, nor into the next field.
func TestHyperlinkDoesNotLeak(t *testing.T) {
	in := `{\rtf1\ansi\pard {\field{\*\fldinst HYPERLINK "https://a.example/"}{\fldrslt one}} two ` +
		`{\field{\*\fldinst PAGE}{\fldrslt 7}} {\field{\*\fldinst HYPERLINK "https://b.example/"}{\fldrslt three}}\par}`
	want := "[one](https://a.example/) two 7 [three](https://b.example/)\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHyperlinkInTableCell(t *testing.T) {
	in := `{\rtf1\ansi\trowd {\field{\*\fldinst HYPERLINK "https://example.org/"}{\fldrslt {\b Doc}}}\cell Other\cell\row}`
	want := "| [**Doc**](https://example.org/) | Other |\n|---|---|\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Word writes the formatting of a paragraph style again as direct formatting after
// \s; only what the author added on top is emphasis.
func TestParagraphStyleEmphasisIsNotRepeated(t *testing.T) {
	in := `{\rtf1\ansi{\stylesheet{\s0 Normal;}{\s1 \i caption;}}` +
		`\pard\plain\s1\i Table 1: {\b Status} values\par` +
		`\pard\plain\s0 Body {\i text}\par}`
	want := "Table 1: **Status** values\n\nBody _text_\n"
	if got := md(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
