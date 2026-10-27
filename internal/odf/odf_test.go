package odf

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/0x0520/doc2md/internal/docimg"
)

type entry struct{ name, body string }

// pack builds a package from mimetype, content.xml and any extra parts.
func pack(t *testing.T, mimetype, content string, extra ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range append([]entry{{"mimetype", mimetype}, {"content.xml", content}}, extra...) {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

const nsDecl = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
	`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" ` +
	`xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0" ` +
	`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:xlink="http://www.w3.org/1999/xlink"`

const (
	textType  = "application/vnd.oasis.opendocument.text"
	sheetType = "application/vnd.oasis.opendocument.spreadsheet"
	slideType = "application/vnd.oasis.opendocument.presentation"
)

var png = entry{"media/image1.png", "\x89PNG\r\n\x1a\n"}

// convert wraps body in a document with the given automatic styles and extra
// package parts, and returns the Markdown and the extracted images.
func convert(t *testing.T, mimetype, auto, body string, extra ...entry) (string, []docimg.Image) {
	t.Helper()
	doc := `<office:document-content ` + nsDecl + `><office:automatic-styles>` + auto +
		`</office:automatic-styles><office:body>` + body + `</office:body></office:document-content>`
	got, imgs, err := ToMarkdown(pack(t, mimetype, doc, extra...))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return got, imgs
}

func md(t *testing.T, mimetype, body string) string {
	t.Helper()
	got, _ := convert(t, mimetype, "", body)
	return got
}

func TestText(t *testing.T) {
	body := `<office:text>` +
		`<text:h text:outline-level="1">Requirements</text:h>` +
		`<text:p>The request is reviewed.</text:p>` +
		`<text:h text:outline-level="2">Deadlines</text:h>` +
		`<text:list><text:list-item><text:p>within 14 days</text:p></text:list-item>` +
		`<text:list><text:list-item><text:p>Exception</text:p></text:list-item></text:list>` +
		`</text:list>` +
		`</office:text>`
	got := md(t, textType, body)
	want := "# Requirements\n\nThe request is reviewed.\n\n## Deadlines\n\n- within 14 days\n  - Exception\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestSpreadsheetSheetName(t *testing.T) {
	body := `<office:spreadsheet><table:table table:name="Values">` +
		`<table:table-row><table:table-cell><text:p>Code</text:p></table:table-cell>` +
		`<table:table-cell><text:p>Meaning</text:p></table:table-cell></table:table-row>` +
		`<table:table-row><table:table-cell><text:p>B</text:p></table:table-cell>` +
		`<table:table-cell><text:p>Stock</text:p></table:table-cell></table:table-row>` +
		`</table:table></office:spreadsheet>`
	got := md(t, sheetType, body)
	want := "# Values\n\n| Code | Meaning |\n|---|---|\n| B | Stock |\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestEmptyRepeatIsNotMaterialised(t *testing.T) {
	body := `<office:spreadsheet><table:table table:name="S">` +
		`<table:table-row><table:table-cell><text:p>A</text:p></table:table-cell>` +
		`<table:table-cell table:number-columns-repeated="16384"/></table:table-row>` +
		`<table:table-row table:number-rows-repeated="1048576">` +
		`<table:table-cell table:number-columns-repeated="16384"/></table:table-row>` +
		`</table:table></office:spreadsheet>`
	got := md(t, sheetType, body)
	if got != "# S\n\n| A |\n|---|\n" {
		t.Errorf("empty repeat was materialised or content lost: %q", got)
	}
}

func TestRepeatedCellWithContent(t *testing.T) {
	body := `<office:spreadsheet><table:table table:name="S">` +
		`<table:table-row><table:table-cell table:number-columns-repeated="3"><text:p>x</text:p></table:table-cell></table:table-row>` +
		`</table:table></office:spreadsheet>`
	got := md(t, sheetType, body)
	if !strings.Contains(got, "| x | x | x |") {
		t.Errorf("repeated content cell not expanded: %q", got)
	}
}

func TestPresentationTitle(t *testing.T) {
	body := `<office:presentation><draw:page draw:name="Slide1">` +
		`<draw:frame presentation:class="title"><draw:text-box>` +
		`<text:p><text:span>Overview</text:span></text:p></draw:text-box></draw:frame>` +
		`<draw:frame presentation:class="outline"><draw:text-box>` +
		`<text:p>Slide content</text:p></draw:text-box></draw:frame>` +
		`</draw:page></office:presentation>`
	got := md(t, slideType, body)
	want := "# Overview\n\nSlide content\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestNotAPackage(t *testing.T) {
	if _, _, err := ToMarkdown([]byte("not even a zip")); err == nil {
		t.Fatal("expected an error for non-package bytes, got nil")
	}
	if _, _, err := ToMarkdown(pack(t, textType, `<office:document-content `+nsDecl+`><office:body><office:text/></office:body></office:document-content>`)); err == nil {
		t.Fatal("expected an error for a package with no content, got nil")
	}
}

// Word anchors a picture as-char inside a span of a paragraph, with the image
// under media/ rather than Pictures/.
func TestImageInsideParagraph(t *testing.T) {
	body := `<office:text><text:p>A diagram:</text:p>` +
		`<text:p><text:span><draw:frame text:anchor-type="as-char">` +
		`<draw:image xlink:href="media/image1.png" xlink:type="simple"/></draw:frame></text:span></text:p>` +
		`<text:p>After.</text:p></office:text>`
	got, imgs := convert(t, textType, "", body, png)
	want := "A diagram:\n\n[Image: 1]\n\nAfter.\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
	if len(imgs) != 1 || imgs[0].Ext != "png" {
		t.Errorf("images = %+v, want one png", imgs)
	}
}

func TestImageInsideTextBox(t *testing.T) {
	body := `<office:text><text:p><draw:frame><draw:text-box><text:p>Caption ` +
		`<draw:frame><draw:image xlink:href="media/image1.png"/></draw:frame></text:p>` +
		`</draw:text-box></draw:frame></text:p></office:text>`
	got, imgs := convert(t, textType, "", body, png)
	if got != "Caption [Image: 1]\n" || len(imgs) != 1 {
		t.Errorf("got %q with %d images", got, len(imgs))
	}
}

// A picture anchored in a spreadsheet cell must not add a column or row.
func TestImageInSheetCellFollowsTable(t *testing.T) {
	body := `<office:spreadsheet><table:table table:name="S"><table:table-row>` +
		`<table:table-cell><text:p>A</text:p></table:table-cell>` +
		`<table:table-cell><draw:frame><draw:image xlink:href="media/image1.png"/></draw:frame></table:table-cell>` +
		`</table:table-row></table:table></office:spreadsheet>`
	got, imgs := convert(t, sheetType, "", body, png)
	if want := "# S\n\n| A |\n|---|\n\n[Image: 1]\n"; got != want || len(imgs) != 1 {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestNumberedList(t *testing.T) {
	auto := `<text:list-style style:name="Num">` +
		`<text:list-level-style-number text:level="1" style:num-format="1"/>` +
		`<text:list-level-style-bullet text:level="2" text:bullet-char="-"/>` +
		`</text:list-style>` +
		`<text:list-style style:name="Dot"><text:list-level-style-bullet text:level="1"/></text:list-style>` +
		`<style:style style:name="PNum" style:family="paragraph" style:list-style-name="Num"/>`
	body := `<office:text>` +
		`<text:list text:style-name="Num">` +
		`<text:list-item><text:p>Receive the file.</text:p>` +
		`<text:list><text:list-item><text:p>Check size</text:p></text:list-item></text:list></text:list-item>` +
		`<text:list-item><text:p>Store it.</text:p></text:list-item></text:list>` +
		`<text:p>Between.</text:p>` +
		`<text:list text:style-name="Dot"><text:list-item><text:p>Bullet</text:p></text:list-item></text:list>` +
		`<text:p>Between.</text:p>` +
		// No style on the list: the paragraph style names it.
		`<text:list><text:list-item><text:p text:style-name="PNum">Via paragraph</text:p></text:list-item></text:list>` +
		`</office:text>`
	got, _ := convert(t, textType, auto, body)
	want := "1. Receive the file.\n   - Check size\n2. Store it.\n\nBetween.\n\n- Bullet\n\nBetween.\n\n1. Via paragraph\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

// Named list styles live in styles.xml, not in content.xml.
func TestListStyleFromStylesXML(t *testing.T) {
	styles := entry{"styles.xml", `<office:document-styles ` + nsDecl + `><office:styles>` +
		`<text:list-style style:name="Num"><text:list-level-style-number text:level="1" style:num-format="a"/></text:list-style>` +
		`</office:styles></office:document-styles>`}
	body := `<office:text><text:list text:style-name="Num"><text:list-item><text:p>One</text:p></text:list-item></text:list></office:text>`
	if got, _ := convert(t, textType, "", body, styles); got != "1. One\n" {
		t.Errorf("got %q", got)
	}
}

// Office suites store a table's replacement picture as a draw:image in the same frame.
func TestReplacementImageNextToTableIgnored(t *testing.T) {
	body := `<office:presentation><draw:page>` +
		`<draw:frame><table:table><table:table-row><table:table-cell><text:p>Metric</text:p></table:table-cell></table:table-row></table:table>` +
		`<draw:image xlink:href="media/image1.png"/></draw:frame>` +
		`<draw:frame><draw:image xlink:href="media/image1.png"/></draw:frame>` +
		`</draw:page></office:presentation>`
	got, imgs := convert(t, slideType, "", body, png)
	if want := "| Metric |\n|---|\n\n[Image: 1]\n"; got != want || len(imgs) != 1 {
		t.Errorf("got:\n%q\n\nwant:\n%q (images %d)", got, want, len(imgs))
	}
}

func TestCellParagraphsJoinedBySpace(t *testing.T) {
	body := `<office:spreadsheet><table:table table:name="S"><table:table-row>` +
		`<table:table-cell><text:p>Line one</text:p><text:p>line two</text:p></table:table-cell>` +
		`<table:table-cell><text:p>a<text:line-break/>b</text:p></table:table-cell>` +
		`</table:table-row></table:table></office:spreadsheet>`
	got := md(t, sheetType, body)
	if !strings.Contains(got, "| Line one line two | a b |") {
		t.Errorf("got %q", got)
	}
}

func TestTitleStyleIsHeading(t *testing.T) {
	auto := `<style:style style:name="Titel" style:display-name="Titel" style:family="paragraph"/>` +
		`<style:style style:name="Sub" style:family="paragraph" style:parent-style-name="Title"/>` +
		`<style:style style:name="P1" style:family="paragraph" style:parent-style-name="Sub"/>`
	body := `<office:text><text:p text:style-name="P1">Product Specification</text:p>` +
		`<text:p>Intro.</text:p>` +
		`<text:h text:outline-level="1">First</text:h>` +
		`<text:h text:outline-level="2">Second</text:h>` +
		`<text:p text:style-name="Titel">Other title</text:p></office:text>`
	got, _ := convert(t, textType, auto, body)
	want := "# Product Specification\n\nIntro.\n\n## First\n\n### Second\n\n# Other title\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestTableOfContentDropped(t *testing.T) {
	body := `<office:text><text:table-of-content><text:index-body>` +
		`<text:p>1 Introduction<text:tab/>3</text:p></text:index-body></text:table-of-content>` +
		`<text:h text:outline-level="1">Introduction</text:h><text:p>Body.</text:p></office:text>`
	if got := md(t, textType, body); got != "# Introduction\n\nBody.\n" {
		t.Errorf("got %q", got)
	}
}

// The cell cap counts each kept cell once; distinct rows must not use it up twice.
func TestCellCapCountsEachCellOnce(t *testing.T) {
	const rows = 600 // 600 x 1000 cells is 60% of maxCells, 120% if counted twice
	var b strings.Builder
	b.WriteString(`<office:spreadsheet><table:table table:name="S">`)
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, `<table:table-row><table:table-cell table:number-columns-repeated="1000"><text:p>r%d</text:p></table:table-cell></table:table-row>`, i)
	}
	b.WriteString(`</table:table></office:spreadsheet>`)
	got := md(t, sheetType, b.String())
	if n := strings.Count(got, "\n| r"); n != rows {
		t.Errorf("kept %d rows, want %d", n, rows)
	}
}

func TestSpeakerNotesDropped(t *testing.T) {
	body := `<office:presentation><draw:page draw:name="p1">` +
		`<draw:frame presentation:class="title"><draw:text-box><text:p>Slide title</text:p></draw:text-box></draw:frame>` +
		`<presentation:notes><draw:frame><draw:text-box><text:p>Only for the speaker</text:p></draw:text-box></draw:frame></presentation:notes>` +
		`</draw:page></office:presentation>`
	got := md(t, slideType, body)
	if !strings.Contains(got, "Slide title") || strings.Contains(got, "speaker") {
		t.Errorf("got:\n%s", got)
	}
}
