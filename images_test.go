package doc2md

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

var onePixelPNG = mustHex(
	"89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c489" +
		"0000000a49444154789c6360000002000100fdff03fa0000000049454e44ae426082")

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestConvertReturnsFiguresPerFormat(t *testing.T) {
	tests := []struct {
		name  string
		build func(*testing.T) []byte
	}{
		{"specification.docx", docxWithPicture},
		{"presentation.pptx", pptxWithPicture},
		{"table.xlsx", xlsxWithPicture},
		{"text.odt", odtWithPicture},
		{"book.epub", epubWithPicture},
		{"page.html", htmlWithDataURI},
		{"report.rtf", rtfWithPicture},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			md, images, err := Convert(tc.name, tc.build(t))
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if len(images) != 1 {
				t.Fatalf("got %d figures, want 1", len(images))
			}
			img := images[0]
			if img.Seq != 1 {
				t.Errorf("Seq = %d, want 1", img.Seq)
			}
			if img.Ext != "png" {
				t.Errorf("Ext = %q, want png", img.Ext)
			}
			if !bytes.Equal(img.Bytes, onePixelPNG) {
				t.Errorf("bytes differ from the picture the document embedded (%d bytes back)", len(img.Bytes))
			}
			if marker := "[Image: 1]"; !strings.Contains(md, marker) {
				t.Errorf("Markdown carries no %s — the figure came back with no position:\n%s", marker, md)
			}
		})
	}
}

func TestConvertKeepsFigureOrderAcrossSlides(t *testing.T) {
	_, images, err := Convert("deck.pptx", pptxWithTwoPictures(t))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("got %d figures, want 2", len(images))
	}
	if images[0].Seq != 1 || images[1].Seq != 2 {
		t.Errorf("sequence = %d,%d, want 1,2", images[0].Seq, images[1].Seq)
	}
	if images[0].Page != 1 || images[1].Page != 2 {
		t.Errorf("slides = %d,%d, want 1,2", images[0].Page, images[1].Page)
	}
}

func TestConvertIgnoresUnreachablePictures(t *testing.T) {
	t.Run("metafile in a docx", func(t *testing.T) {
		md, images, err := Convert("spec.docx", docxWithPart(t, "word/media/image1.wmf", onePixelPNG))
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if len(images) != 0 {
			t.Errorf("got %d figures, want none — a metafile is not displayable", len(images))
		}
		if strings.Contains(md, "[Image:") {
			t.Errorf("Markdown carries a marker for a picture that was not kept:\n%s", md)
		}
	})

	t.Run("external file in an html page", func(t *testing.T) {
		md, images, err := Convert("page.html", []byte(
			`<html><body><p>Before</p><img src="image.png" alt="A diagram"><p>After</p></body></html>`))
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if len(images) != 0 {
			t.Errorf("got %d figures, want none — the file was never handed over", len(images))
		}
		if strings.Contains(md, "[Image:") {
			t.Errorf("Markdown carries a marker for bytes nobody has:\n%s", md)
		}
		if !strings.Contains(md, "A diagram") {
			t.Errorf("the alt text was dropped as well:\n%s", md)
		}
	})
}

func zipOf(t *testing.T, parts [][2]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i, p := range parts {
		name := p[0].(string)
		var w interface{ Write([]byte) (int, error) }
		var err error
		if i == 0 && name == "mimetype" {
			w, err = zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		} else {
			w, err = zw.Create(name)
		}
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		var body []byte
		switch v := p[1].(type) {
		case string:
			body = []byte(v)
		case []byte:
			body = v
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

const wns = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`

func docxWithPicture(t *testing.T) []byte {
	return docxWithPart(t, "word/media/image1.png", onePixelPNG)
}

func docxWithPart(t *testing.T, part string, payload []byte) []byte {
	t.Helper()
	target := strings.TrimPrefix(part, "word/")
	body := `<w:p><w:r><w:t xml:space="preserve">Before </w:t></w:r>` +
		`<w:r><w:drawing><a:blip r:embed="rId9"/></w:drawing></w:r>` +
		`<w:r><w:t xml:space="preserve"> after</w:t></w:r></w:p>`
	return zipOf(t, [][2]any{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0"?><w:document ` + wns + `><w:body>` + body + `</w:body></w:document>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="` + target + `"/></Relationships>`},
		{part, payload},
	})
}

const pns = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

func slideXML(title string) string {
	return `<?xml version="1.0"?><p:sld ` + pns + `><p:cSld><p:spTree>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody>` +
		`<a:p><a:r><a:t>` + title + `</a:t></a:r></a:p></p:txBody></p:sp>` +
		`<p:pic><p:blipFill><a:blip r:embed="rId2"/></p:blipFill></p:pic>` +
		`</p:spTree></p:cSld></p:sld>`
}

func slideRels() string {
	return `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`
}

func pptxWithPicture(t *testing.T) []byte {
	return zipOf(t, [][2]any{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"ppt/presentation.xml", `<?xml version="1.0"?><p:presentation ` + pns + `/>`},
		{"ppt/slides/slide1.xml", slideXML("First slide")},
		{"ppt/slides/_rels/slide1.xml.rels", slideRels()},
		{"ppt/media/image1.png", onePixelPNG},
	})
}

func pptxWithTwoPictures(t *testing.T) []byte {
	t.Helper()
	return zipOf(t, [][2]any{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"ppt/presentation.xml", `<?xml version="1.0"?><p:presentation ` + pns + `/>`},
		{"ppt/slides/slide1.xml", slideXML("First slide")},
		{"ppt/slides/_rels/slide1.xml.rels", slideRels()},
		{"ppt/slides/slide2.xml", slideXML("Second slide")},
		{"ppt/slides/_rels/slide2.xml.rels", slideRels()},
		{"ppt/media/image1.png", onePixelPNG},
	})
}

func xlsxWithPicture(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"
	if err := f.SetCellStr(sheet, "A1", "Code"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.AddPictureFromBytes(sheet, "C3", &excelize.Picture{
		Extension: ".png",
		File:      onePixelPNG,
		Format:    &excelize.GraphicOptions{},
	}); err != nil {
		t.Fatalf("add picture: %v", err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf.Bytes()
}

const odfNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
	`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" ` +
	`xmlns:xlink="http://www.w3.org/1999/xlink" ` +
	`xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0"`

func odtWithPicture(t *testing.T) []byte {
	body := `<office:text><text:h text:outline-level="1">Chapter</text:h>` +
		`<text:p>A paragraph.</text:p>` +
		`<draw:frame><draw:image xlink:href="Pictures/image.png"/></draw:frame>` +
		`</office:text>`
	return zipOf(t, [][2]any{
		{"mimetype", "application/vnd.oasis.opendocument.text"},
		{"content.xml", `<?xml version="1.0" encoding="UTF-8"?><office:document-content ` + odfNS + `><office:body>` + body + `</office:body></office:document-content>`},
		{"Pictures/image.png", onePixelPNG},
	})
}

func epubWithPicture(t *testing.T) []byte {
	chapter := `<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml">` +
		`<head><title>Chapter</title></head><body><h1>Chapter</h1><p>A paragraph.</p>` +
		`<img src="../images/image.png" alt="Diagram"/></body></html>`
	opf := `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0">` +
		`<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Sample Book</dc:title></metadata>` +
		`<manifest><item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/></manifest>` +
		`<spine><itemref idref="c1"/></spine></package>`
	return zipOf(t, [][2]any{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">` +
			`<rootfiles><rootfile full-path="OEBPS/book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"OEBPS/book.opf", opf},
		{"OEBPS/text/ch1.xhtml", chapter},
		{"OEBPS/images/image.png", onePixelPNG},
	})
}

func htmlWithDataURI(t *testing.T) []byte {
	t.Helper()
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(onePixelPNG)
	return []byte(`<html><head><title>Page</title></head><body><h1>Page</h1>` +
		`<p>A paragraph.</p><img src="` + uri + `" alt="Diagram"></body></html>`)
}

func rtfWithPicture(t *testing.T) []byte {
	t.Helper()
	return []byte(`{\rtf1\ansi\ansicpg1252\uc1{\fonttbl{\f0\froman Times;}}` +
		`\pard Before {\pict\pngblip\picw1\pich1 ` + hex.EncodeToString(onePixelPNG) + `} after\par }`)
}

var _ = fmt.Sprintf
