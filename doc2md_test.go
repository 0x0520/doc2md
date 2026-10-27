package doc2md

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestSniff(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"pdf", "%PDF-1.7\n%âãÏÓ", ".pdf"},
		{"legacy OLE compound file", "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1", ".xls"},
		{"html by doctype", "<!DOCTYPE html>\n<html><body>hi</body></html>", ".html"},
		{"html by root element", "<html lang=\"en\"><body>hi</body></html>", ".html"},
		{"html with leading whitespace and BOM", "\xEF\xBB\xBF\n  <!doctype HTML><body>hi", ".html"},
		{"xml by declaration", `<?xml version="1.0"?><rows><r>1</r></rows>`, ".xml"},
		{"xhtml is html, not xml", `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><body>hi</body></html>`, ".html"},
		{"bare element is still a tree", "<rows><r>1</r></rows>", ".xml"},
		{"plain text identifies as nothing", "Just a plain sentence.", ""},
		{"empty input identifies as nothing", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sniff([]byte(tc.in)); got != tc.want {
				t.Errorf("sniff = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSniffZip(t *testing.T) {
	pkg := func(part, body string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create(part)
		_, _ = w.Write([]byte(body))
		_ = zw.Close()
		return buf.Bytes()
	}
	tests := []struct{ name, part, body, want string }{
		{"docx", "word/document.xml", "<x/>", ".docx"},
		{"pptx", "ppt/presentation.xml", "<x/>", ".pptx"},
		{"xlsx", "xl/workbook.xml", "<x/>", ".xlsx"},
		{"odt", "mimetype", "application/vnd.oasis.opendocument.text", ".odt"},
		{"ods", "mimetype", "application/vnd.oasis.opendocument.spreadsheet", ".ods"},
		{"odp", "mimetype", "application/vnd.oasis.opendocument.presentation", ".odp"},
		{"epub by mimetype", "mimetype", "application/epub+zip", ".epub"},
		{"epub by container", "META-INF/container.xml", "<container/>", ".epub"},
		{"a zip that is no document", "mimetype", "application/zip", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sniff(pkg(tc.part, tc.body)); got != tc.want {
				t.Errorf("a zip containing %q sniffed as %q, want %q", tc.part, got, tc.want)
			}
		})
	}
}

func TestConvert_UnhandledNamesAreReportedAsSuch(t *testing.T) {
	ole := []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1 whatever follows")
	for name, data := range map[string][]byte{
		"report.doc":   ole,
		"slides.ppt":   ole,
		"mail.msg":     ole,
		"mail.eml":     []byte("From: a@b\r\nSubject: x\r\n\r\nText"),
		"report.pages": []byte("PK\x03\x04"),
	} {
		_, _, err := Convert(name, data)
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("Convert(%q) = %v, want ErrUnsupportedFormat", name, err)
		}
	}
}

func TestConvert_MarkupFormatsAreConverted(t *testing.T) {
	md, _, err := Convert("spec.rtf", []byte(`{\rtf1\ansi{\fonttbl{\f0 Times;}}\pard\outlinelevel0 Overview\par\pard The caf\'e9 is open.\par}`))
	if err != nil {
		t.Fatalf("convert rtf: %v", err)
	}
	if !strings.Contains(md, "# Overview") {
		t.Errorf("outline level not converted to a heading: %q", md)
	}
	if strings.Contains(md, `\rtf`) || strings.Contains(md, "fonttbl") {
		t.Errorf("control-word stream survived: %q", md)
	}
	if !strings.Contains(md, "café") {
		t.Errorf("code-page escape not decoded: %q", md)
	}
}

func TestConvert_SniffOnlyForUnknownNames(t *testing.T) {
	page := []byte("<!DOCTYPE html><html><body><h1>Title</h1></body></html>")

	md, _, err := Convert("note.txt", page)
	if err != nil {
		t.Fatalf("txt: %v", err)
	}
	if !strings.Contains(md, "<h1>") {
		t.Errorf("a .txt was converted instead of passed through: %q", md)
	}

	md, _, err = Convert("page.aspx", page)
	if err != nil {
		t.Fatalf("aspx: %v", err)
	}
	if !strings.Contains(md, "# Title") || strings.Contains(md, "<h1>") {
		t.Errorf("an unrecognised name was not identified by content: %q", md)
	}
}

func TestConvert_UnsupportedStillErrors(t *testing.T) {
	if _, _, err := Convert("data.bin", []byte{0x01, 0x02, 0x03, 0x04}); err == nil {
		t.Error("unidentifiable bytes must still be an unsupported format")
	}
}

func TestConvert_SniffedExtensionReachesTheReader(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	const ns = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	for _, p := range [][2]string{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0"?><workbook ` + ns +
			` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="Values" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", `<?xml version="1.0"?><worksheet ` + ns + `><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Stock</t></is></c></row></sheetData></worksheet>`},
	} {
		w, err := zw.Create(p[0])
		if err != nil {
			t.Fatalf("zip: %v", err)
		}
		if _, err := w.Write([]byte(p[1])); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	md, _, err := Convert("", buf.Bytes())
	if err != nil {
		t.Fatalf("Convert with no name: %v", err)
	}
	if !strings.Contains(md, "Stock") {
		t.Errorf("spreadsheet content missing: %q", md)
	}
	if !strings.Contains(md, "# Values") {
		t.Errorf("sheet name did not become a heading: %q", md)
	}
}

func TestConvert_UnsignedTextFormatsNeedTheirName(t *testing.T) {
	adoc := []byte("== Overview\n\nThe café is open.\n")
	if _, _, err := Convert("", adoc); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("unnamed AsciiDoc: got %v, want ErrUnsupportedFormat", err)
	}
	md, _, err := Convert("spec.adoc", adoc)
	if err != nil {
		t.Fatalf("named AsciiDoc: %v", err)
	}
	if !strings.Contains(md, "# Overview") {
		t.Errorf("named AsciiDoc not converted: %q", md)
	}
}

func TestHandlesAndExtensionsAgreeWithConvert(t *testing.T) {
	for _, ext := range Extensions() {
		if !Handles("document" + ext) {
			t.Errorf("Extensions() lists %q but Handles says no", ext)
		}
	}
	for _, name := range []string{"old.doc", "old.ppt", "mail.eml", "mail.msg", "page.pages"} {
		if Handles(name) {
			t.Errorf("Handles(%q) = true, want false — it is named as unhandled", name)
		}
	}
	if Handles("build.pom") {
		t.Error(`Handles("build.pom") = true, want false`)
	}
	if Handles("PRESENTATION.PPTX") != Handles("presentation.pptx") {
		t.Error("Handles is case-sensitive on the extension")
	}
}
