package rtf

import (
	"strings"
	"testing"
)

func md(t *testing.T, in string) string {
	t.Helper()
	got, _, err := ToMarkdown([]byte(in))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return got
}

func TestMarkupDoesNotSurvive(t *testing.T) {
	in := `{\rtf1\ansi\deff0{\fonttbl{\f0\froman Times New Roman;}}` +
		`{\colortbl;\red0\green0\blue0;}` +
		`{\info{\author Someone}{\title Not the text}}` +
		`\pard The caf\'e9 is open.\par}`
	got := md(t, in)
	if got != "The café is open.\n" {
		t.Errorf("got %q", got)
	}
}

func TestOutlineLevelIsTheHeading(t *testing.T) {
	in := `{\rtf1\ansi\pard\outlinelevel0 Overview\par` +
		`\pard\outlinelevel1 Limits\par` +
		`\pard Running text.\par}`
	want := "# Overview\n\n## Limits\n\nRunning text.\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestStyleCarriesOutlineLevel(t *testing.T) {
	in := `{\rtf1\ansi{\stylesheet{\s0 Normal;}{\s1\outlinelevel0 heading 1;}{\s2\outlinelevel1 heading 2;}}` +
		`\pard\s1 Chapter\par\pard\s2 Section\par\pard\s0 Text.\par}`
	want := "# Chapter\n\n## Section\n\nText.\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestTable(t *testing.T) {
	in := `{\rtf1\ansi\pard Before the table.\par` +
		`\trowd Code\cell Meaning\cell\row` +
		`\trowd B\cell Beta\cell\row` +
		`\pard After the table.\par}`
	want := "Before the table.\n\n| Code | Meaning |\n|---|---|\n| B | Beta |\n\nAfter the table.\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// The generated bullet in \pntext must not appear next to the Markdown marker.
func TestListMarkerNotDoubled(t *testing.T) {
	in := `{\rtf1\ansi\pard{\pntext\f3 \'b7\tab}First item\par` +
		`\pard{\pntext\f3 \'b7\tab}Second item\par}`
	want := "- First item\n- Second item\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// The uc fallback characters after \uN must be skipped, not emitted.
func TestUnicodeEscapeNotDoubled(t *testing.T) {
	got := md(t, "{\\rtf1\\ansi\\uc1\\pard Price: \\u8364 ? 50\\par}")
	if got != "Price: € 50\n" {
		t.Errorf("got %q", got)
	}
	got = md(t, "{\\rtf1\\ansi\\uc2\\pard Price: \\u8364 ?? 50\\par}")
	if got != "Price: € 50\n" {
		t.Errorf("uc2: got %q", got)
	}
}

func TestCharacterControlWords(t *testing.T) {
	got := md(t, `{\rtf1\ansi\pard He said \ldblquote Yes\rdblquote  \endash  short.\par}`)
	if !strings.Contains(got, "“Yes”") || !strings.Contains(got, "–") {
		t.Errorf("character control words lost: %q", got)
	}
}

// \binN payload bytes must not be parsed as RTF.
func TestBinarySkipped(t *testing.T) {
	got := md(t, `{\rtf1\ansi\pard Before \bin11 \par NOTTEXT After.\par}`)
	if strings.Contains(got, "NOTTEXT") {
		t.Errorf("binary payload was read as stream: %q", got)
	}
	if !strings.Contains(got, "Before") || !strings.Contains(got, "After.") {
		t.Errorf("text around the payload was lost: %q", got)
	}
}

// Word's list definitions: \lsN selects a list whose level format says number or bullet.
func TestListTableDecidesNumberedOrBullet(t *testing.T) {
	in := `{\rtf1\ansi{\*\listtable` +
		`{\list\listtemplateid1\listsimple{\listlevel\levelnfc0\levelnfcn0{\leveltext\'02\'00.;}}\listid-120}` +
		`{\list\listtemplateid2\listsimple{\listlevel\levelnfc23\levelnfcn23{\leveltext\'01\u-3913 ?;}}\listid-119}}` +
		`{\*\listoverridetable{\listoverride\listid-119\listoverridecount0\ls1}` +
		`{\listoverride\listid-120\listoverridecount0\ls2}}` +
		`\pard\ls1{\pntext\f3\'b7\tab}Apple\par` +
		`\pard\ls1{\pntext\f3\'b7\tab}Pear\par` +
		`\pard\ls2{\pntext 1.\tab}Receive\par` +
		`\pard\ls2{\pntext 2.\tab}Validate\par}`
	want := "- Apple\n- Pear\n\n1. Receive\n2. Validate\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// Without a list table the generated label is the only evidence.
func TestListLabelDecidesNumberedOrBullet(t *testing.T) {
	in := `{\rtf1\ansi\pard{\pntext\pard 1.\tab}Receive\par` +
		`\pard{\pntext\pard 2.\tab}Validate\par}`
	want := "1. Receive\n2. Validate\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestTitleStyleIsHeading(t *testing.T) {
	in := `{\rtf1\ansi{\stylesheet{\s0 Normal;}{\s1\outlinelevel0 \sbasedon0 heading 1;}` +
		`{\*\cs25 \additive Title Zchn;}{\s24\ql \sbasedon0 \snext0 Title;}}` +
		`\pard\s24 Specification\par\pard\s0 Intro.\par\pard\s1 Chapter\par}`
	want := "# Specification\n\nIntro.\n\n## Chapter\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestDocumentCodePage(t *testing.T) {
	got := md(t, `{\rtf1\ansi\ansicpg1251\pard \'cf\'f0\'e8\'e2\'e5\'f2\par}`)
	if got != "Привет\n" {
		t.Errorf("got %q", got)
	}
}

// A font's \fcharset overrides the document code page for the text set in it.
func TestFontCharsetCodePage(t *testing.T) {
	in := `{\rtf1\ansi\ansicpg1252\deff0{\fonttbl{\f0\fcharset0 Arial;}{\f1\fcharset204 Arial Cyr;}` +
		`{\f2\fcharset161 Arial Greek;}{\f3\fcharset238 Arial CE;}{\f4\fcharset162 Arial Tur;}{\f5\fcharset186 Arial Baltic;}}` +
		`\pard\f0 caf\'e9\par\pard\f1 \'cf\'f0\'e8\'e2\'e5\'f2\par\pard\f2 \'c1\'e5\'f4\par` +
		`\pard\f3 \'b3\par\pard\f4 \'f0\par\pard\f5 \'e0\par` +
		`\pard{\f1 \'cf}\f0 \'e9\par}`
	want := "café\n\nПривет\n\nΑετ\n\nł\n\nğ\n\ną\n\nПé\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// A TOC field's result is generated page text; other fields keep theirs.
func TestTOCFieldDropped(t *testing.T) {
	in := `{\rtf1\ansi\pard Contents\par` +
		`{\field{\*\fldinst {\rtlch TOC \\o "1-3" }}{\fldrslt 1 Introduction\tab 3\par 2 Scope\tab 4\par}}` +
		`\pard Page {\field{\*\fldinst PAGE}{\fldrslt 7}}, Table {\field{\*\fldinst SEQ Table}{\fldrslt 1}}\par}`
	want := "Contents\n\nPage 7, Table 1\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// A list label inside a table cell must not turn the next paragraph into a list item.
func TestListLabelInCellDoesNotLeak(t *testing.T) {
	in := `{\rtf1\ansi\trowd{\pntext\pard 1.\tab}One\cell Two\cell\row\pard After.\par}`
	if got := md(t, in); strings.Contains(got, "- After") || strings.Contains(got, "1. After") {
		t.Errorf("label leaked: %q", got)
	}
}

func TestNotRTF(t *testing.T) {
	if _, _, err := ToMarkdown([]byte("Just a plain sentence.")); err == nil {
		t.Fatal("expected an error for non-RTF bytes, got nil")
	}
}
