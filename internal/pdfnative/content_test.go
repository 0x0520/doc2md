package pdfnative

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func stream(dict, content string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(content), content)
}

// formPDF is a one-page PDF whose page content is pageContent; forms are objects 6 onwards.
func formPDF(pageContent string, forms ...string) []byte {
	return pdfFromObjects(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> /XObject << /Fm1 6 0 R >> >> /Contents 5 0 R >>",
		stream("", pageContent),
	}, forms...))
}

// rowsOf returns the page's rows by text, with their baseline, left edge and size.
func rowsOf(t *testing.T, data []byte) map[string]line {
	t.Helper()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	toks, _, _, ok := pageTokens(r.Page(1))
	if !ok {
		t.Fatal("page content unreadable")
	}
	out := map[string]line{}
	for _, row := range pageRows(toks) {
		ln := joinLine(row)
		out[ln.text] = ln
	}
	return out
}

func TestExtractTokensEntersFormXObjects(t *testing.T) {
	data := formPDF(
		"BT /F1 12 Tf 72 600 Td (Body line on the page) Tj ET q 1 0 0 1 72 0 cm /Fm1 Do Q",
		// Fm1 is moved up by its matrix, draws itself again (a cycle) and a nested form.
		stream("/Type /XObject /Subtype /Form /BBox [0 0 595 842] /Matrix [1 0 0 1 0 50] /Resources << /Font << /F1 3 0 R >> /XObject << /Fm1 6 0 R /Fm2 7 0 R >> >>",
			"BT /F1 12 Tf 0 700 Td (Text inside the form) Tj ET /Fm1 Do /Fm2 Do"),
		// Fm2 has no resources of its own and scales by 2, so 6 pt text renders at 12 pt.
		stream("/Type /XObject /Subtype /Form /BBox [0 0 595 842] /Matrix [2 0 0 2 0 0]",
			"BT /F1 6 Tf 36 300 Td (Nested form text) Tj ET"),
	)
	rows := rowsOf(t, data)

	for text, want := range map[string]line{
		"Body line on the page": {xStart: 72, y: 600, fontSize: 12},
		"Text inside the form":  {xStart: 72, y: 750, fontSize: 12},
		"Nested form text":      {xStart: 144, y: 650, fontSize: 12},
	} {
		got, ok := rows[text]
		if !ok {
			t.Errorf("%q not extracted; rows: %v", text, rows)
			continue
		}
		if absF(got.xStart-want.xStart) > 0.01 || absF(got.y-want.y) > 0.01 || got.fontSize != want.fontSize {
			t.Errorf("%q at x=%.2f y=%.2f size %.1f, want x=%.0f y=%.0f size %.0f",
				text, got.xStart, got.y, got.fontSize, want.xStart, want.y, want.fontSize)
		}
	}
	if len(rows) != 3 {
		t.Errorf("the self-referencing form must be entered once; rows: %v", rows)
	}
}

func TestExtractTokensBoundsFormDepth(t *testing.T) {
	const levels = maxFormDepth + 5
	var forms []string
	for i := 1; i <= levels; i++ {
		content := fmt.Sprintf("BT /F1 12 Tf 72 %d Td (Level %d) Tj ET", 800-i*20, i)
		xobj := ""
		if i < levels {
			content += fmt.Sprintf(" /Fm%d Do", i+1)
			xobj = fmt.Sprintf("/XObject << /Fm%d %d 0 R >>", i+1, 6+i)
		}
		forms = append(forms, stream(
			fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> %s >>", xobj),
			content))
	}
	rows := rowsOf(t, formPDF("/Fm1 Do", forms...))

	if _, ok := rows[fmt.Sprintf("Level %d", maxFormDepth)]; !ok {
		t.Errorf("forms up to the depth limit must be read; rows: %v", rows)
	}
	if _, ok := rows[fmt.Sprintf("Level %d", maxFormDepth+1)]; ok {
		t.Errorf("forms beyond the depth limit must be skipped")
	}
}

func TestRenderReadsFormXObjectText(t *testing.T) {
	data := formPDF("q /Fm1 Do Q",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >>",
			"BT /F1 12 Tf 72 700 Td (Generated reports wrap pages in forms.) Tj ET"))
	md, err := Extract(data)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(md, "Generated reports wrap pages in forms.") {
		t.Errorf("form text missing:\n%s", md)
	}
}

func TestBrokenFormKeepsPageText(t *testing.T) {
	data := formPDF("BT /F1 12 Tf 72 600 Td (Page text survives) Tj ET /Fm1 Do",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >>",
			"BT /F1 12 Tf 72 720 Td <6 g ET"))
	if _, ok := rowsOf(t, data)["Page text survives"]; !ok {
		t.Error("a malformed form stream must not lose the page's own text")
	}
}
