package ooxml

import (
	"strings"
	"testing"
)

// wordDoc builds a package from a body plus optional styles and numbering parts.
func wordDoc(t *testing.T, body, styles, numbering string) []byte {
	t.Helper()
	parts := map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document ` + nsW + ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><w:body>` + body + `</w:body></w:document>`,
	}
	if styles != "" {
		parts["word/styles.xml"] = `<?xml version="1.0"?><w:styles ` + nsW + `>` + styles + `</w:styles>`
	}
	if numbering != "" {
		parts["word/numbering.xml"] = `<?xml version="1.0"?><w:numbering ` + nsW + `>` + numbering + `</w:numbering>`
	}
	return zipDocx(t, parts)
}

func mustMarkdown(t *testing.T, data []byte) string {
	t.Helper()
	md, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return md
}

func para(inner string) string { return `<w:p>` + inner + `</w:p>` }

func run(text string) string {
	return `<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r>`
}

func fmtRun(props, text string) string {
	return `<w:r><w:rPr>` + props + `</w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r>`
}

const listNumbering = `
<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:pStyle w:val="ListNumber"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:pStyle w:val="ListBullet"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/><w:pStyle w:val="ListBullet2"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="1"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="0"/></w:num>`

const listStyles = `
<w:style w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:styleId="ListBullet"><w:name w:val="List Bullet"/><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style>
<w:style w:styleId="ListNumber"><w:name w:val="List Number"/><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:numId w:val="2"/></w:numPr></w:pPr></w:style>
<w:style w:styleId="ListBullet2"><w:name w:val="List Bullet 2"/><w:basedOn w:val="ListBullet"/></w:style>
<w:style w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:ilvl w:val="1"/></w:numPr></w:pPr></w:style>`

// Word's "List Bullet" and "List Number" carry numPr in the style, not the paragraph.
func TestList_ThroughParagraphStyle(t *testing.T) {
	body := styledPara("ListBullet", "Alpha") +
		styledPara("ListBullet", "Beta") +
		styledPara("ListNumber", "Step") +
		plain("After.")
	md := mustMarkdown(t, wordDoc(t, body, listStyles, listNumbering))
	// Whether mdrender separates a bullet list from a following numbered one is its business.
	for _, want := range []string{"- Alpha\n- Beta\n", "1. Step\n\nAfter.\n"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%q", want, md)
		}
	}
}

// A style derived from a list style inherits the list, and the level comes from
// the numbering definition's pStyle link when no ilvl is written down.
func TestList_StyleChainAndLinkedLevel(t *testing.T) {
	body := styledPara("ListBullet", "Top") + styledPara("ListBullet2", "Nested")
	md := mustMarkdown(t, wordDoc(t, body, listStyles, listNumbering))
	want := "- Top\n  - Nested\n"
	if md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

func TestList_DirectIlvlOverStyle(t *testing.T) {
	body := `<w:p><w:pPr><w:pStyle w:val="ListBullet"/><w:numPr><w:ilvl w:val="2"/></w:numPr></w:pPr>` + run("Deep") + `</w:p>`
	md := mustMarkdown(t, wordDoc(t, body, listStyles, listNumbering))
	if want := "- Deep\n"; md != want {
		t.Errorf("got %q, want %q", md, want)
	}
}

// numId 0 switches numbering off, in the paragraph or in a style.
func TestList_NumIdZeroIsNoList(t *testing.T) {
	styles := listStyles + `<w:style w:styleId="NoList"><w:name w:val="No List Item"/><w:basedOn w:val="ListBullet"/><w:pPr><w:numPr><w:numId w:val="0"/></w:numPr></w:pPr></w:style>`
	body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="0"/></w:numPr></w:pPr>` + run("Direct") + `</w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="ListBullet"/><w:numPr><w:numId w:val="0"/></w:numPr></w:pPr>` + run("Over style") + `</w:p>` +
		styledPara("NoList", "By style")
	md := mustMarkdown(t, wordDoc(t, body, styles, listNumbering))
	if want := "Direct\n\nOver style\n\nBy style\n"; md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

// A style with only an ilvl (Word's Subtitle) is no list.
func TestList_IlvlWithoutNumIdIsNoList(t *testing.T) {
	md := mustMarkdown(t, wordDoc(t, styledPara("Subtitle", "Just a subtitle"), listStyles, listNumbering))
	if want := "Just a subtitle\n"; md != want {
		t.Errorf("got %q, want %q", md, want)
	}
}

func TestEmphasis_MergesFragmentedRuns(t *testing.T) {
	body := para(
		run("A ") +
			fmtRun(`<w:b/>`, "bo") + fmtRun(`<w:b/>`, "ld") +
			fmtRun(`<w:b/><w:i/>`, " both ") +
			fmtRun(`<w:i/>`, "slanted") +
			run(" and ") +
			fmtRun(`<w:strike/>`, "gone") + run("."))
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	want := "A **bold** **_both_** _slanted_ and ~~gone~~.\n"
	if md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

// A blank run between two bold runs does not split the span, and blank bold runs
// get no markers of their own.
func TestEmphasis_Whitespace(t *testing.T) {
	body := para(fmtRun(`<w:b/>`, "one")+fmtRun(``, " ")+fmtRun(`<w:b/>`, "two")) +
		para(run("x")+fmtRun(`<w:b/>`, "  ")+run("y")) +
		para(fmtRun(`<w:b/>`, "pad ")+run("tail"))
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	want := "**one two**\n\nx y\n\n**pad** tail\n"
	if md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

func TestEmphasis_OffAndRunStyle(t *testing.T) {
	styles := `<w:style w:styleId="Strong"><w:name w:val="Strong"/><w:rPr><w:b/></w:rPr></w:style>` +
		`<w:style w:styleId="Loud"><w:name w:val="Loud"/><w:basedOn w:val="Strong"/><w:rPr><w:i/></w:rPr></w:style>`
	body := para(
		fmtRun(`<w:rStyle w:val="Strong"/>`, "strong") + run(" ") +
			fmtRun(`<w:rStyle w:val="Loud"/>`, "loud") + run(" ") +
			fmtRun(`<w:rStyle w:val="Strong"/><w:b w:val="0"/>`, "undone") + run(" ") +
			fmtRun(`<w:b w:val="false"/>`, "off"))
	md := mustMarkdown(t, wordDoc(t, body, styles, ""))
	want := "**strong** **_loud_** undone off\n"
	if md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

func TestEmphasis_HeadingStaysPlain(t *testing.T) {
	styles := `<w:style w:styleId="H1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>`
	body := `<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr>` + fmtRun(`<w:b/>`, "Chapter") + `</w:p>` + plain("Text.")
	md := mustMarkdown(t, wordDoc(t, body, styles, ""))
	if want := "# Chapter\n\nText.\n"; md != want {
		t.Errorf("got %q, want %q", md, want)
	}
}

// Size-promoted headings come from the plain text, not from the marked-up paragraph.
func TestEmphasis_SizePromotedHeadingStaysPlain(t *testing.T) {
	big := `<w:p><w:r><w:rPr><w:b/><w:sz w:val="40"/></w:rPr><w:t>Overview</w:t></w:r></w:p>`
	body := big + plain("The sample is checked after arrival and finished within fourteen days.") +
		plain("The period begins with the arrival of the complete material at the office.") +
		plain("An extension is possible on request, if reasons exist.")
	md := mustMarkdown(t, wordDoc(t, body, defaults11pt, ""))
	if !strings.HasPrefix(md, "# Overview\n") {
		t.Errorf("heading should carry no markers:\n%s", md)
	}
}

func TestEmphasis_InLinkAndCell(t *testing.T) {
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/></Relationships>`
	body := para(`<w:hyperlink r:id="rId5">`+fmtRun(`<w:b/>`, "bold link")+`</w:hyperlink>`) +
		`<w:tbl><w:tr><w:tc>` + para(fmtRun(`<w:i/>`, "cell")) + `</w:tc></w:tr></w:tbl>`
	data := zipDocx(t, map[string]string{
		"word/document.xml":            `<?xml version="1.0"?><w:document ` + nsW + `><w:body>` + body + `</w:body></w:document>`,
		"word/_rels/document.xml.rels": rels,
	})
	md := mustMarkdown(t, data)
	for _, want := range []string{"[**bold link**](https://example.com)", "| _cell_ |"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func fld(kind string) string {
	return `<w:r><w:fldChar w:fldCharType="` + kind + `"/></w:r>`
}

func instr(s string) string {
	return `<w:r><w:instrText xml:space="preserve"> ` + s + ` </w:instrText></w:r>`
}

// Only the result of a field is text: the instruction never is.
func TestFields_ResultNotInstruction(t *testing.T) {
	body := para(run("Tabelle ")+`<w:fldSimple w:instr=" SEQ Tabelle \* ARABIC ">`+fmtRun(`<w:noProof/>`, "1")+`</w:fldSimple>`+run(": Status values")) +
		para(run("Page ")+fld("begin")+instr("PAGE")+fld("separate")+run("7")+fld("end")+run(" of text")) +
		para(fld("begin")+instr(`DATE \@ "d"`)+fld("end")+run("after empty field"))
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	want := "Tabelle 1: Status values\n\nPage 7 of text\n\nafter empty field\n"
	if md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

// Fields nest (a PAGEREF inside a REF result); the inner result still reads through.
func TestFields_Nested(t *testing.T) {
	body := para(fld("begin") + instr("REF a") + fld("separate") + run("see ") +
		fld("begin") + instr("PAGEREF b") + fld("separate") + run("p. 4") + fld("end") + fld("end"))
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if want := "see p. 4\n"; md != want {
		t.Errorf("got %q, want %q", md, want)
	}
}

// A TOC field spans paragraphs and has PAGEREF fields in its result.
func TestTOC_FieldAcrossParagraphsDropped(t *testing.T) {
	entry := func(title, page string) string {
		return para(run(title) + `<w:r><w:tab/></w:r>` + fld("begin") + instr("PAGEREF _Toc1 \\h") + fld("separate") + run(page) + fld("end"))
	}
	body := plain("Before.") +
		para(fld("begin")+instr(`TOC \o "1-3"`)+fld("separate")+run("1 Intro")+`<w:r><w:tab/></w:r>`+fld("begin")+instr("PAGEREF _Toc0 \\h")+fld("separate")+run("3")+fld("end")) +
		entry("2 More", "5") +
		para(fld("end")) +
		plain("After.")
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if want := "Before.\n\nAfter.\n"; md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

func TestTOC_ContentControlDropped(t *testing.T) {
	body := plain("Before.") +
		`<w:sdt><w:sdtPr><w:docPartObj><w:docPartGallery w:val="Table of Contents"/><w:docPartUnique/></w:docPartObj></w:sdtPr><w:sdtContent>` +
		plain("Contents") + plain("1 Intro 3") + `</w:sdtContent></w:sdt>` +
		`<w:sdt><w:sdtPr><w:alias w:val="Other"/></w:sdtPr><w:sdtContent>` + plain("Kept.") + `</w:sdtContent></w:sdt>` +
		plain("After.")
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if want := "Before.\n\nKept.\n\nAfter.\n"; md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

func TestTOC_SimpleFieldDropped(t *testing.T) {
	body := plain("Before.") + para(`<w:fldSimple w:instr=" TOC \o &quot;1-3&quot; ">`+run("1 Intro 3")+`</w:fldSimple>`) + plain("After.")
	md := mustMarkdown(t, wordDoc(t, body, "", ""))
	if want := "Before.\n\nAfter.\n"; md != want {
		t.Errorf("got %q, want %q", md, want)
	}
}

// The same picture used twice is extracted once and both places share the marker.
func TestImages_ReusedPictureExtractedOnce(t *testing.T) {
	drawing := `<w:p><w:r><w:drawing><a:graphic><a:graphicData><a:blip r:embed="rId7"/></a:graphicData></a:graphic></w:drawing></w:r></w:p>`
	data := zipDocx(t, map[string]string{
		"word/document.xml":            `<?xml version="1.0"?><w:document ` + nsW + ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><w:body>` + drawing + plain("Between.") + drawing + `</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`,
		"word/media/image1.png":        "PNGDATA",
	})
	md, imgs, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if len(imgs) != 1 {
		t.Errorf("got %d images, want 1", len(imgs))
	}
	if want := "[Image: 1]\n\nBetween.\n\n[Image: 1]\n"; md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}
