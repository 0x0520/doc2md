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
