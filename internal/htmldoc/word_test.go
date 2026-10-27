package htmldoc

import (
	"strings"
	"testing"
)

const wordHead = `<html xmlns:o="urn:schemas-microsoft-com:office:office"><head>` +
	`<meta http-equiv=Content-Type content="text/html; charset=windows-1252">` +
	`<style><!-- p.MsoToc1 {margin:0cm} --></style></head><body>`

func TestWord_ListsFromMsoListStyle(t *testing.T) {
	in := wordHead + `
<p class=MsoNormal>Intro.</p>
<p class=MsoListParagraphCxSpFirst style='text-indent:-18.0pt;mso-list:l0 level1 lfo1'><![if !supportLists]><span
style='font-family:Symbol;mso-fareast-font-family:Symbol;mso-bidi-font-family:Symbol'><span style='mso-list:Ignore'>` + "\xb7" + `<span
style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp; </span></span></span><![endif]>First <b>item</b></p>
<p class=MsoListParagraphCxSpMiddle style='margin-left:36.0pt;mso-list:l0 level2 lfo1'><![if !supportLists]><span
style='mso-list:Ignore'>o<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;</span></span><![endif]>Nested item</p>
<p class=MsoListParagraphCxSpLast style='mso-list:l0 level1 lfo1'><![if !supportLists]><span
style='mso-list:Ignore'>` + "\xb7" + `<span>&nbsp;&nbsp;</span></span><![endif]>Last item</p>
<p class=MsoListParagraphCxSpFirst style='mso-list:l1 level1 lfo2'><![if !supportLists]><span
style='mso-list:Ignore'>1.<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp; </span></span><![endif]>Step one</p>
<p class=MsoListParagraphCxSpLast style='mso-list:l1 level1 lfo2'><![if !supportLists]><span
style='mso-list:Ignore'>b)<span>&nbsp;&nbsp;</span></span><![endif]>Step two</p>
<p class=MsoNormal>After.</p></body></html>`
	want := strings.Join([]string{
		"Intro.", "",
		"- First **item**",
		"  - Nested item",
		"- Last item", "",
		"1. Step one",
		"2. Step two", "",
		"After.",
	}, "\n")
	got := md(t, in)
	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
	if strings.ContainsAny(got, "�·") || strings.Contains(got, "supportLists") || strings.Contains(got, "endif") {
		t.Errorf("marker or conditional comment leaked:\n%s", got)
	}
}

// The "filtered HTML" export has no mso-list styles: the class names the list
// and the marker is leading text.
func TestWord_FilteredHTMLLists(t *testing.T) {
	in := wordHead + `
<p class=MsoListBulletCxSpFirst><span style='font-family:Symbol'>` + "\xb7" + `<span
style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp; </span></span>Every
upload is checked.</p>
<p class=MsoListBulletCxSpLast><span style='font-family:Symbol'>` + "\xb7" + `<span
style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp; </span></span>Rejected files are reported.</p>
<p class=MsoListBullet2><span style='font-family:"Courier New"'>o<span>&nbsp;&nbsp;</span></span>Deeper</p>
<p class=MsoListNumberCxSpFirst>1.<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
</span>Receive the file.</p>
<p class=MsoListNumberCxSpLast>2.<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
</span>Store the result.</p></body></html>`
	want := strings.Join([]string{
		"- Every upload is checked.",
		"- Rejected files are reported.",
		"  - Deeper", "",
		"1. Receive the file.",
		"2. Store the result.",
	}, "\n")
	if got := md(t, in); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestWord_ListWordIsNotAMarker(t *testing.T) {
	got := md(t, `<p class=MsoListParagraph style='mso-list:l0 level1 lfo1'>Order matters. Really.</p>`+
		`<p class=MsoListParagraph>Go. now</p>`)
	if got != "- Order matters. Really.\n- Go. now" && got != "- Order matters. Really.\n1. now" {
		t.Errorf("got %q", got)
	}
}

func TestWord_TOCDropped(t *testing.T) {
	got := md(t, wordHead+`
<p class=MsoTocHeading>Contents</p>
<p class=MsoToc1>1 Introduction............................ 3</p>
<p class=MsoToc2>1.1 Scope............................ 3</p>
<p class=MsoToc9>Deep.......... 9</p>
<h1>1 Introduction</h1><p>Text.</p></body></html>`)
	if strings.Contains(got, "....") || strings.Contains(got, "Scope") || strings.Contains(got, "Deep") {
		t.Errorf("TOC lines survived:\n%s", got)
	}
	if !strings.Contains(got, "Contents") || !strings.Contains(got, "# 1 Introduction") || !strings.Contains(got, "Text.") {
		t.Errorf("content lost:\n%s", got)
	}
}

func TestWord_ConditionalCommentsDoNotLeak(t *testing.T) {
	got := md(t, `<p class=MsoNormal><![if !supportLineBreakNewLine]><br><![endif]>Visible<!--[if gte mso 9]><xml><o:x>hidden</o:x></xml><![endif]--></p>`)
	if got != "Visible" {
		t.Errorf("got %q", got)
	}
}

func TestWord_TitleIsTopHeading(t *testing.T) {
	got := md(t, `<html><body><p class=MsoTitle>Product Specification</p>`+
		`<h1>1 Introduction</h1><p>Text.</p><h2>1.1 Scope</h2></body></html>`)
	want := "# Product Specification\n\n## 1 Introduction\n\nText.\n\n### 1.1 Scope"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestWord_FootnotesAndEndnotes(t *testing.T) {
	in := wordHead + `
<p class=MsoNormal>A sentence<a href="#_ftn1" name="_ftnref1" title=""><span class=MsoFootnoteReference>[1]</span></a>
and an endnote<a href="#_edn1" name="_ednref1" title=""><span class=MsoEndnoteReference>[i]</span></a>.</p>
<div><br clear=all><hr align=left size=1 width="33%">
<div id=ftn1><p class=MsoFootnoteText><a href="#_ftnref1" name="_ftn1" title=""><span class=MsoFootnoteReference>[1]</span></a>
The <b>footnote</b> text.</p></div></div>
<div><div id=edn1><p class=MsoEndnoteText><a href="#_ednref1" name="_edn1" title=""><span>[i]</span></a>
The endnote text.</p></div></div></body></html>`
	want := "A sentence[^1] and an endnote[^2].\n\n[^1]: The **footnote** text.\n\n[^2]: The endnote text."
	if got := md(t, in); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestWord_FootnoteWithoutBodyStaysLink(t *testing.T) {
	got := md(t, `<p>Text<sup><a href="#fn1">1</a></sup> and<a href="#_ftn9" name="_ftnref9">[9]</a>.</p>`)
	if !strings.Contains(got, "[1](#fn1)") || !strings.Contains(got, "(#_ftn9)") || strings.Contains(got, "[^") {
		t.Errorf("generic or bodiless references must stay links:\n%s", got)
	}
}

func TestWord_CommentsDropped(t *testing.T) {
	in := wordHead + `<p class=MsoNormal>A commented <a>word</a><span class=MsoCommentReference><a class=msocomanchor id="_anchor_1"
href="#_msocom_1" name="_msoanchor_1">[DL1]</a>&nbsp;</span> here.</p>
<div><hr class=msocomoff align=left size=1 width="33%"><div><div id="_com_1" class=msocomtxt><a name="_msocom_1"></a>
<p class=MsoCommentText><span class=MsoCommentReference>&nbsp;<a href="#_msoanchor_1" class=msocomoff>[DL1]</a></span>A reviewer comment.</p>
</div></div></div></body></html>`
	if got, want := md(t, in), "A commented word here."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWord_TrackedChanges(t *testing.T) {
	in := wordHead + `<p><span class=msoIns><ins cite="mailto:A" datetime="2026-01-01T10:00">kept, </ins></span>` +
		`<span class=msoDel><del cite="mailto:A" datetime="2026-01-01T10:00">gone, </del></span>` +
		`<del cite="mailto:A">gone too, </del>and <del>struck</del>.</p></body></html>`
	if got, want := md(t, in), "kept, and ~~struck~~."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Filtered HTML has no mso-list; only margin-left tells the levels apart, and
// the style's own margin is omitted.
func TestWord_OutlineLevelsFromMargins(t *testing.T) {
	in := wordHead + `
<p class=MsoListParagraphCxSpFirst style='margin-left:18.0pt;text-indent:-18.0pt'>1)<span>&nbsp;&nbsp;</span>First level</p>
<p class=MsoListParagraphCxSpMiddle style='text-indent:-18.0pt'>a)<span>&nbsp;&nbsp;</span>Second level</p>
<p class=MsoListParagraphCxSpMiddle style='margin-left:54.0pt;text-indent:-18.0pt'>i)<span>&nbsp;&nbsp;</span>Third level</p>
<p class=MsoListParagraphCxSpLast style='margin-left:18.0pt;text-indent:-18.0pt'>2)<span>&nbsp;&nbsp;</span>First level again</p></body></html>`
	want := "1. First level\n   1. Second level\n      1. Third level\n2. First level again"
	if got := md(t, in); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestWord_TextBoxBecomesOwnParagraph(t *testing.T) {
	in := wordHead + `<p class=MsoNormal>Before <span style='position:
relative;z-index:1'><span style='position:absolute;left:388px;top:-124px'>
<table><tr><td><span style='position:absolute'><table><tr><td><div><p class=MsoNormal>Text inside a text box.</p></div></td></tr></table></span>&nbsp;</td></tr></table>
</span></span>after.</p></body></html>`
	if got, want := md(t, in), "Before after.\n\nText inside a text box."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	plain := md(t, wordHead+`<p>Lead <span style='position:absolute;top:0'>Floating <b>note</b></span>tail.</p></body></html>`)
	if want := "Lead tail.\n\nFloating **note**"; plain != want {
		t.Errorf("got %q, want %q", plain, want)
	}
}

func TestNestedTableInDataTableIsFlattened(t *testing.T) {
	got := md(t, `<table><tr><td>Outer A</td><td>Outer B</td></tr>
<tr><td>Outer C</td><td><table><tr><td>in 1</td><td>in 2</td></tr><tr><td>in 3</td><td>in 4</td></tr></table><p></p></td></tr></table>`)
	want := "| Outer A | Outer B |\n|---|---|\n| Outer C | in 1, in 2; in 3, in 4 |"
	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestNestedTableInWrapperStaysLayout(t *testing.T) {
	got := md(t, `<table><tr><td><p>Intro text.</p><table><tr><td>Actor</td><td>Staff</td></tr></table></td></tr></table>`)
	if want := "Intro text.\n\n| Actor | Staff |\n|---|---|"; got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
