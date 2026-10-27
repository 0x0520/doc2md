package ooxml

import (
	"archive/zip"
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func zipDocx(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

const nsW = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

func TestToMarkdown_Docx(t *testing.T) {
	document := `<?xml version="1.0"?>
<w:document ` + nsW + `><w:body>
<w:p><w:pPr><w:pStyle w:val="berschrift1"/></w:pPr><w:r><w:t>Chapter</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">An </w:t></w:r><w:hyperlink r:id="rId5"><w:r><w:t>Link</w:t></w:r></w:hyperlink><w:r><w:t xml:space="preserve"> here.</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Point A</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>Step 1</w:t></w:r></w:p>
<w:tbl>
<w:tr><w:tc><w:p><w:r><w:t>H1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>H2</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>a</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>b</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>
<w:p></w:p>
</w:body></w:document>`

	styles := `<?xml version="1.0"?>
<w:styles ` + nsW + `>
<w:style w:type="paragraph" w:styleId="berschrift1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>
</w:styles>`

	numbering := `<?xml version="1.0"?>
<w:numbering ` + nsW + `>
<w:abstractNum w:abstractNumId="10"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="20"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="10"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="20"/></w:num>
</w:numbering>`

	rels := `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/>
</Relationships>`

	data := zipDocx(t, map[string]string{
		"word/document.xml":            document,
		"word/styles.xml":              styles,
		"word/numbering.xml":           numbering,
		"word/_rels/document.xml.rels": rels,
		"[Content_Types].xml":          `<?xml version="1.0"?><Types/>`,
	})

	md, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}

	for _, want := range []string{
		"# Chapter",
		"An [Link](https://example.com) here.",
		"- Point A",
		"1. Step 1",
		"| H1 | H2 |\n|---|---|\n| a | b |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("output missing %q\n--- full ---\n%s", want, md)
		}
	}
	if !strings.HasPrefix(md, "# Chapter") {
		t.Errorf("expected doc to start with a level-1 heading, got:\n%s", md)
	}
}

// A crafted list level must not turn into an unbounded indent.
func TestToMarkdown_HostileIlvlBounded(t *testing.T) {
	doc := `<?xml version="1.0"?>
<w:document ` + nsW + `><w:body>
<w:p><w:pPr><w:numPr><w:ilvl w:val="2000000000"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Item</w:t></w:r></w:p>
</w:body></w:document>`
	data := zipDocx(t, map[string]string{"word/document.xml": doc})
	md, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("docx: %v", err)
	}
	if !strings.Contains(md, "Item") {
		t.Errorf("list item text missing:\n%q", md)
	}
	if len(md) > 1000 {
		t.Errorf("output unexpectedly large (%d bytes) — indent not clamped", len(md))
	}
}

func TestToMarkdown_NotDocx(t *testing.T) {
	if _, _, err := ToMarkdown([]byte("not a zip")); err == nil {
		t.Error("expected error for non-zip bytes")
	}
	empty := zipDocx(t, map[string]string{"hello.txt": "hi"})
	if _, _, err := ToMarkdown(empty); err == nil {
		t.Error("expected error for zip without word/document.xml")
	}
}

func sizeDoc(t *testing.T, styles, body string) []byte {
	t.Helper()
	return zipDocx(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document ` + nsW + `><w:body>` + body + `</w:body></w:document>`,
		"word/styles.xml":   `<?xml version="1.0"?><w:styles ` + nsW + `>` + styles + `</w:styles>`,
	})
}

const defaults11pt = `<w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>`

func sized(halfPoints int, text string) string {
	return `<w:p><w:r><w:rPr><w:sz w:val="` + itoa(halfPoints) + `"/></w:rPr><w:t>` + text + `</w:t></w:r></w:p>`
}

func plain(text string) string {
	return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestSizeFallback_NoStyles(t *testing.T) {
	body := sized(32, "Overview") +
		plain("The sample is checked after arrival and finished within fourteen days.") +
		plain("The period begins with the arrival of the complete material at the office.") +
		sized(28, "Limits") +
		plain("An extension is possible on request, if reasons exist.") +
		plain("The requester is informed about the extension in writing.")
	md, _, err := ToMarkdown(sizeDoc(t, defaults11pt, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(md, "# Overview") {
		t.Errorf("largest text did not become the top heading:\n%s", md)
	}
	if !strings.Contains(md, "## Limits") {
		t.Errorf("the second size did not become the second level:\n%s", md)
	}
	if strings.Contains(md, "# The sample") {
		t.Errorf("body text was promoted:\n%s", md)
	}
}

func TestSizeFallback_StylesWin(t *testing.T) {
	styles := defaults11pt + `<w:style w:styleId="H1"><w:name w:val="heading 1"/></w:style>`
	body := `<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>Chapter</w:t></w:r></w:p>` +
		sized(48, "A large quote set apart") +
		plain("Running text in the document that sets the base size and is longer.")
	md, _, err := ToMarkdown(sizeDoc(t, styles, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(md, "# Chapter") {
		t.Errorf("styled heading lost:\n%s", md)
	}
	if strings.Contains(md, "# A large quote set apart") {
		t.Errorf("large text outranked a styled heading:\n%s", md)
	}
}

// A few large lines among little body text are formatting, not an outline.
func TestSizeFallback_NotAnOutline(t *testing.T) {
	body := plain("Short body text.") +
		sized(32, "Line one") + sized(32, "Line two") + sized(32, "Line three")
	md, _, err := ToMarkdown(sizeDoc(t, defaults11pt, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if strings.Contains(md, "#") {
		t.Errorf("formatting variety was read as an outline:\n%s", md)
	}
}

// When half the paragraphs are larger than the rest, size is no heading signal.
func TestSizeFallback_HalfAndHalf(t *testing.T) {
	body := plain("First line.") + plain("Second line.") + plain("Third line.") +
		sized(32, "Fourth line") + sized(32, "Fifth line") + sized(32, "Sixth line")
	md, _, err := ToMarkdown(sizeDoc(t, defaults11pt, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if strings.Contains(md, "#") {
		t.Errorf("an even split was read as an outline:\n%s", md)
	}
}

func TestTitleOutranksHeading1(t *testing.T) {
	styles := defaults11pt +
		`<w:style w:styleId="Titel"><w:name w:val="Title"/></w:style>` +
		`<w:style w:styleId="H1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>` +
		`<w:style w:styleId="H2"><w:name w:val="heading 2"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>`
	body := styledPara("Titel", "Specification") +
		styledPara("H1", "1. Goals") + plain("Text.") +
		styledPara("H2", "1.1 Scope") + plain("Text.")
	md, _, err := ToMarkdown(sizeDoc(t, styles, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	want := "# Specification\n\n## 1. Goals\n\nText.\n\n### 1.1 Scope\n\nText.\n"
	if md != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", md, want)
	}
}

func TestNoTitleStillStartsAtH1(t *testing.T) {
	styles := defaults11pt +
		`<w:style w:styleId="H1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>` +
		`<w:style w:styleId="H2"><w:name w:val="heading 2"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>`
	body := styledPara("H1", "1. Goals") + plain("Text.") +
		styledPara("H2", "1.1 Scope") + plain("Text.")
	md, _, err := ToMarkdown(sizeDoc(t, styles, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	want := "# 1. Goals\n\nText.\n\n## 1.1 Scope\n\nText.\n"
	if md != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", md, want)
	}
}

func styledPara(style, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}
