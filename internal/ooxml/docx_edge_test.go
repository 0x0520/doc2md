package ooxml

import (
	"strings"
	"testing"
)

func tc(inner string) string { return `<w:tc>` + inner + `</w:tc>` }

func sdt(props, content string) string {
	return `<w:sdt><w:sdtPr>` + props + `</w:sdtPr><w:sdtContent>` + content + `</w:sdtContent></w:sdt>`
}

func expectMarkdown(t *testing.T, body, want string) {
	t.Helper()
	if got := mustMarkdown(t, wordDoc(t, body, "", "")); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestContentControl_InlineInParagraph(t *testing.T) {
	body := para(run("Customer: ") + sdt(`<w:text/>`, run("Example Ltd")) + run(" signed."))
	expectMarkdown(t, body, "Customer: Example Ltd signed.\n")
}

func TestContentControl_InTableCellRowsAndCells(t *testing.T) {
	body := `<w:tbl>` +
		`<w:tr>` + tc(para(run("Field"))) + tc(para(run("Value"))) + `</w:tr>` +
		`<w:tr>` + tc(para(run("Order"))) + tc(sdt(`<w:text/>`, para(run("A-1042")))) + `</w:tr>` +
		sdt(``, `<w:tr>`+tc(para(run("Row")))+tc(para(run("wrapped")))+`</w:tr>`) +
		`<w:tr>` + sdt(``, tc(para(run("Cell")))) + tc(para(run("wrapped"))) + `</w:tr>` +
		`</w:tbl>`
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	for _, want := range []string{"| Order | A-1042 |", "| Row | wrapped |", "| Cell | wrapped |"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestContentControl_PlaceholderContributesNothing(t *testing.T) {
	body := para(run("A ")+sdt(`<w:showingPlcHdr/>`, run("Click to enter text"))+run("B")) +
		sdt(`<w:showingPlcHdr/>`, para(run("Pick a date."))) +
		`<w:tbl><w:tr>` + tc(sdt(`<w:showingPlcHdr/>`, para(run("Choose")))+para(run("Real"))) + `</w:tr></w:tbl>`
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if strings.Contains(md, "Click") || strings.Contains(md, "Pick") || strings.Contains(md, "Choose") {
		t.Errorf("placeholder text leaked:\n%s", md)
	}
	if !strings.Contains(md, "A B") || !strings.Contains(md, "Real") {
		t.Errorf("real text lost:\n%s", md)
	}
}

func TestContentControl_Nested(t *testing.T) {
	body := para(sdt(``, run("a ")+sdt(``, run("b"))))
	expectMarkdown(t, body, "a b\n")
}

const txbxDrawing = `<w:drawing><wp:anchor><a:graphic><a:graphicData><wps:wsp><wps:txbx><w:txbxContent>` +
	`<w:p><w:r><w:t>Box one.</w:t></w:r></w:p><w:p><w:r><w:t>Box two.</w:t></w:r></w:p>` +
	`</w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:anchor></w:drawing>`

const txbxPict = `<w:pict><v:shape><v:textbox><w:txbxContent><w:p><w:r><w:t>Box one.</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t>Box two.</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict>`

// A text box is its own paragraphs after the anchor, not glued into it.
func TestTextBox_DrawingAfterAnchorParagraph(t *testing.T) {
	body := para(run("Before ")+`<w:r>`+txbxDrawing+`</w:r>`+run("after.")) + plain("Next.")
	expectMarkdown(t, body, "Before after.\n\nBox one.\n\nBox two.\n\nNext.\n")
}

func TestTextBox_LegacyPict(t *testing.T) {
	body := para(run("Anchor.")+`<w:r>`+txbxPict+`</w:r>`) + plain("Next.")
	expectMarkdown(t, body, "Anchor.\n\nBox one.\n\nBox two.\n\nNext.\n")
}

// Word writes the text box twice, as a Choice and as a VML Fallback.
func TestTextBox_AlternateContentNotDoubled(t *testing.T) {
	body := para(run("Anchor.")+`<w:r><mc:AlternateContent><mc:Choice Requires="wps">`+txbxDrawing+
		`</mc:Choice><mc:Fallback>`+txbxPict+`</mc:Fallback></mc:AlternateContent></w:r>`) + plain("Next.")
	data := zipDocx(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document ` + nsW +
			` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"` +
			` xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" xmlns:v="urn:schemas-microsoft-com:vml"` +
			` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><w:body>` + body + `</w:body></w:document>`,
	})
	if got, want := mustMarkdown(t, data), "Anchor.\n\nBox one.\n\nBox two.\n\nNext.\n"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestTextBox_InTableCell(t *testing.T) {
	body := `<w:tbl><w:tr>` + tc(para(run("Cell ")+`<w:r>`+txbxDrawing+`</w:r>`)) + `</w:tr></w:tbl>`
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if !strings.Contains(md, "| Cell Box one. Box two. |") {
		t.Errorf("got:\n%s", md)
	}
}

func notesDoc(t *testing.T, body string) []byte {
	t.Helper()
	notes := func(tag, extra string) string {
		return `<?xml version="1.0"?><w:` + tag + `s ` + nsW + `>` +
			`<w:` + tag + ` w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:` + tag + `>` +
			`<w:` + tag + ` w:type="continuationSeparator" w:id="0"><w:p><w:r><w:continuationSeparator/></w:r></w:p></w:` + tag + `>` +
			extra + `</w:` + tag + `s>`
	}
	return zipDocx(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document ` + nsW + `><w:body>` + body + `</w:body></w:document>`,
		"word/footnotes.xml": notes("footnote",
			`<w:footnote w:id="1"><w:p><w:r><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> First </w:t></w:r>`+
				`<w:r><w:rPr><w:b/></w:rPr><w:t>bold</w:t></w:r></w:p><w:p><w:r><w:t>second paragraph.</w:t></w:r></w:p></w:footnote>`+
				`<w:footnote w:id="2"><w:p><w:r><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> Later footnote.</w:t></w:r></w:p></w:footnote>`),
		"word/endnotes.xml": notes("endnote",
			`<w:endnote w:id="1"><w:p><w:r><w:endnoteRef/></w:r><w:r><w:t xml:space="preserve"> The endnote.</w:t></w:r></w:p></w:endnote>`),
	})
}

func TestNotes_FootnotesAndEndnotesNumberedByFirstReference(t *testing.T) {
	ref := func(kind, id string) string { return `<w:r><w:` + kind + `Reference w:id="` + id + `"/></w:r>` }
	body := para(run("One")+ref("endnote", "1")+run(" two")+ref("footnote", "2")+run(" three")+ref("footnote", "1")+run(" again")+ref("endnote", "1")+run(".")) +
		para(run("Last."))
	got := mustMarkdown(t, notesDoc(t, body))
	want := "One[^1] two[^2] three[^3] again[^1].\n\nLast.\n\n" +
		"[^1]: The endnote.\n\n[^2]: Later footnote.\n\n[^3]: First **bold** second paragraph.\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNotes_SeparatorsAndUnknownIdsDropped(t *testing.T) {
	body := para(run("Text") + `<w:r><w:footnoteReference w:id="0"/></w:r><w:r><w:footnoteReference w:id="99"/></w:r>`)
	expectMarkdown(t, body, "Text\n")
	if got := mustMarkdown(t, notesDoc(t, body)); got != "Text\n" {
		t.Errorf("got %q", got)
	}
}

func TestNestedTable_FlattenedIntoCell(t *testing.T) {
	inner := `<w:tbl><w:tr>` + tc(para(run("in 1"))) + tc(para(run("in 2"))) + `</w:tr><w:tr>` +
		tc(para(run("in 3"))) + tc(para(run("in 4"))) + `</w:tr></w:tbl>`
	body := `<w:tbl><w:tr>` + tc(para(run("Outer A"))) + tc(para(run("Outer B"))) + `</w:tr><w:tr>` +
		tc(para(run("Outer C"))) + tc(para(run("Lead"))+inner+`<w:p/>`) + `</w:tr></w:tbl>`
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if want := "| Outer C | Lead in 1, in 2; in 3, in 4 |"; !strings.Contains(md, want) {
		t.Errorf("missing %q in:\n%s", want, md)
	}
	if strings.Contains(md, `\|`) {
		t.Errorf("nested table leaked pipes:\n%s", md)
	}
}

func TestTrackedChanges_DeletionsDroppedInsertionsKept(t *testing.T) {
	body := para(`<w:ins w:id="1" w:author="a"><w:r><w:t xml:space="preserve">Kept, </w:t></w:r></w:ins>` +
		`<w:del w:id="2" w:author="a"><w:r><w:delText xml:space="preserve">removed, </w:delText></w:r></w:del>` +
		run("plain."))
	expectMarkdown(t, body, "Kept, plain.\n")
}

func TestComments_NeverAppear(t *testing.T) {
	body := para(run("A ") + `<w:commentRangeStart w:id="0"/>` + run("word") + `<w:commentRangeEnd w:id="0"/>` +
		`<w:r><w:commentReference w:id="0"/></w:r>` + run(" here."))
	data := zipDocx(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document ` + nsW + `><w:body>` + body + `</w:body></w:document>`,
		"word/comments.xml": `<?xml version="1.0"?><w:comments ` + nsW + `><w:comment w:id="0"><w:p><w:r><w:t>Secret remark</w:t></w:r></w:p></w:comment></w:comments>`,
	})
	if got, want := mustMarkdown(t, data), "A word here.\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
