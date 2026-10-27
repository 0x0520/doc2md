package odf

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func pack(t *testing.T, mimetype, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, part := range []struct{ name, body string }{
		{"mimetype", mimetype},
		{"content.xml", content},
	} {
		w, err := zw.Create(part.name)
		if err != nil {
			t.Fatalf("create %s: %v", part.name, err)
		}
		if _, err := w.Write([]byte(part.body)); err != nil {
			t.Fatalf("write %s: %v", part.name, err)
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
	`xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0"`

func md(t *testing.T, mimetype, body string) string {
	t.Helper()
	got, _, err := ToMarkdown(pack(t, mimetype, `<office:document-content `+nsDecl+`><office:body>`+body+`</office:body></office:document-content>`))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
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
	got := md(t, "application/vnd.oasis.opendocument.text", body)
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
	got := md(t, "application/vnd.oasis.opendocument.spreadsheet", body)
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
	got := md(t, "application/vnd.oasis.opendocument.spreadsheet", body)
	if got != "# S\n\n| A |\n|---|\n" {
		t.Errorf("empty repeat was materialised or content lost: %q", got)
	}
}

func TestRepeatedCellWithContent(t *testing.T) {
	body := `<office:spreadsheet><table:table table:name="S">` +
		`<table:table-row><table:table-cell table:number-columns-repeated="3"><text:p>x</text:p></table:table-cell></table:table-row>` +
		`</table:table></office:spreadsheet>`
	got := md(t, "application/vnd.oasis.opendocument.spreadsheet", body)
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
	got := md(t, "application/vnd.oasis.opendocument.presentation", body)
	want := "# Overview\n\nSlide content\n"
	if got != want {
		t.Errorf("got:\n%q\n\nwant:\n%q", got, want)
	}
}

func TestNotAPackage(t *testing.T) {
	if _, _, err := ToMarkdown([]byte("not even a zip")); err == nil {
		t.Fatal("expected an error for non-package bytes, got nil")
	}
	if _, _, err := ToMarkdown(pack(t, "application/vnd.oasis.opendocument.text", `<office:document-content `+nsDecl+`><office:body><office:text/></office:body></office:document-content>`)); err == nil {
		t.Fatal("expected an error for a package with no content, got nil")
	}
}
