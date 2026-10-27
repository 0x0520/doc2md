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

func TestListMarkerNotDoubled(t *testing.T) {
	in := `{\rtf1\ansi\pard{\pntext\f3 \'b7\tab}First item\par` +
		`\pard{\pntext\f3 \'b7\tab}Second item\par}`
	want := "- First item\n- Second item\n"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

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

func TestBinarySkipped(t *testing.T) {
	got := md(t, `{\rtf1\ansi\pard Before \bin11 \par NOTTEXT After.\par}`)
	if strings.Contains(got, "NOTTEXT") {
		t.Errorf("binary payload was read as stream: %q", got)
	}
	if !strings.Contains(got, "Before") || !strings.Contains(got, "After.") {
		t.Errorf("text around the payload was lost: %q", got)
	}
}

func TestNotRTF(t *testing.T) {
	if _, _, err := ToMarkdown([]byte("Just a plain sentence.")); err == nil {
		t.Fatal("expected an error for non-RTF bytes, got nil")
	}
}
