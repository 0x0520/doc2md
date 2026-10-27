package ooxml

import (
	"strings"
	"testing"
)

const pptxCT = `<?xml version="1.0"?><Types/>`

func slideWith(shapes string) string {
	return `<?xml version="1.0"?><p:sld ` + nsPptx + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:cSld><p:spTree>` + shapes + `</p:spTree></p:cSld></p:sld>`
}

func shape(ph, paras string) string {
	nv := ""
	if ph != "" {
		nv = `<p:nvSpPr><p:nvPr>` + ph + `</p:nvPr></p:nvSpPr>`
	}
	return `<p:sp>` + nv + `<p:txBody><a:bodyPr/>` + paras + `</p:txBody></p:sp>`
}

func apara(ppr, text string) string {
	return `<a:p>` + ppr + `<a:r><a:t>` + text + `</a:t></a:r></a:p>`
}

func pptxMarkdown(t *testing.T, parts map[string]string) string {
	t.Helper()
	parts["[Content_Types].xml"] = pptxCT
	md, _, err := PptxToMarkdown(zipDocx(t, parts))
	if err != nil {
		t.Fatalf("PptxToMarkdown: %v", err)
	}
	return md
}

// Body placeholders inherit their bullets from the layout, so every paragraph is a
// list item unless it switches the bullet off; lvl gives the nesting.
func TestPptx_BodyPlaceholderBullets(t *testing.T) {
	slide := slideWith(
		shape(`<p:ph type="title"/>`, apara("", "Highlights")) +
			shape(`<p:ph type="body" idx="1"/>`,
				apara("", "First")+
					apara(`<a:pPr lvl="1"/>`, "Nested")+
					apara(`<a:pPr lvl="2"><a:buNone/></a:pPr>`, "No bullet")+
					apara(`<a:pPr><a:buAutoNum type="arabicPeriod"/></a:pPr>`, "Numbered")) +
			shape(`<p:ph idx="2"/>`, apara("", "Object placeholder")) +
			shape(`<p:ph type="subTitle" idx="1"/>`, apara("", "Subtitle stays text")) +
			shape("", apara("", "Free text box")))
	md := pptxMarkdown(t, map[string]string{"ppt/slides/slide1.xml": slide})
	want := "# Highlights\n\n- First\n  - Nested\n\nNo bullet\n\n1. Numbered\n\n- Object placeholder\n\nSubtitle stays text\n\nFree text box\n"
	if md != want {
		t.Errorf("got:\n%q\nwant:\n%q", md, want)
	}
}

// A placeholder id on a table or picture frame must not leak into the next shape.
func TestPptx_PlaceholderDoesNotLeakIntoTextBox(t *testing.T) {
	frame := `<p:graphicFrame><p:nvGraphicFramePr><p:nvPr><p:ph idx="3"/></p:nvPr></p:nvGraphicFramePr><a:graphic><a:graphicData><a:tbl><a:tr><a:tc><a:txBody>` +
		apara("", "cell") + `</a:txBody></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	slide := slideWith(frame + shape("", apara("", "Loose text")))
	md := pptxMarkdown(t, map[string]string{"ppt/slides/slide1.xml": slide})
	if !strings.Contains(md, "\nLoose text\n") || strings.Contains(md, "- Loose text") {
		t.Errorf("text box after a table placeholder became a bullet:\n%s", md)
	}
}

// A bullet set on the paragraph itself counts even in a text box.
func TestPptx_ExplicitBulletInTextBox(t *testing.T) {
	slide := slideWith(shape("", apara(`<a:pPr><a:buChar char="-"/></a:pPr>`, "Dash")+apara("", "Plain")))
	md := pptxMarkdown(t, map[string]string{"ppt/slides/slide1.xml": slide})
	if want := "- Dash\n\nPlain\n"; md != want {
		t.Errorf("got %q, want %q", md, want)
	}
}

func presentation(rIDs ...string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><p:presentation ` + nsPptx + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst>`)
	for i, id := range rIDs {
		sb.WriteString(`<p:sldId id="` + itoa(256+i) + `" r:id="` + id + `"/>`)
	}
	sb.WriteString(`</p:sldIdLst></p:presentation>`)
	return sb.String()
}

func presentationRels(m map[string]string) string {
	var sb strings.Builder
	sb.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for id, target := range m {
		sb.WriteString(`<Relationship Id="` + id + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="` + target + `"/>`)
	}
	sb.WriteString(`</Relationships>`)
	return sb.String()
}

// Slides are ordered by p:sldIdLst, not by the number in the part name; a slide the
// list does not mention is not part of the deck.
func TestPptx_SlideOrderFollowsPresentation(t *testing.T) {
	md := pptxMarkdown(t, map[string]string{
		"ppt/slides/slide1.xml": titleSlide("FirstPart"),
		"ppt/slides/slide2.xml": titleSlide("SecondPart"),
		"ppt/slides/slide3.xml": titleSlide("ThirdPart"),
		"ppt/slides/slide4.xml": titleSlide("Orphan"),
		"ppt/presentation.xml":  presentation("rId9", "rId7", "rId8"),
		"ppt/_rels/presentation.xml.rels": presentationRels(map[string]string{
			"rId7": "slides/slide1.xml",
			"rId8": "/ppt/slides/slide2.xml",
			"rId9": "slides/slide3.xml",
		}),
	})
	i3, i1, i2 := strings.Index(md, "ThirdPart"), strings.Index(md, "FirstPart"), strings.Index(md, "SecondPart")
	if i3 < 0 || i1 < 0 || i2 < 0 || i3 > i1 || i1 > i2 {
		t.Errorf("slides not in presentation order (Third, First, Second):\n%s", md)
	}
	if strings.Contains(md, "Orphan") {
		t.Errorf("slide outside sldIdLst was included:\n%s", md)
	}
}

// A presentation.xml that resolves to no slide must not empty the deck.
func TestPptx_UnusablePresentationFallsBack(t *testing.T) {
	md := pptxMarkdown(t, map[string]string{
		"ppt/slides/slide2.xml":           titleSlide("TwoPart"),
		"ppt/slides/slide1.xml":           titleSlide("OnePart"),
		"ppt/presentation.xml":            presentation("rId1"),
		"ppt/_rels/presentation.xml.rels": presentationRels(map[string]string{"rId1": "slides/missing.xml"}),
	})
	if strings.Index(md, "OnePart") > strings.Index(md, "TwoPart") || !strings.Contains(md, "OnePart") {
		t.Errorf("fallback order wrong:\n%s", md)
	}
}

// The image page is the slide's position in the deck, and a picture used on two
// slides is extracted once.
func TestPptx_ImageReusedAcrossSlides(t *testing.T) {
	pic := `<p:pic><p:blipFill><a:blip r:embed="rId2"/></p:blipFill></p:pic>`
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`
	data := zipDocx(t, map[string]string{
		"[Content_Types].xml":              pptxCT,
		"ppt/slides/slide1.xml":            slideWith(pic),
		"ppt/slides/slide2.xml":            slideWith(pic),
		"ppt/slides/_rels/slide1.xml.rels": rels,
		"ppt/slides/_rels/slide2.xml.rels": rels,
		"ppt/media/image1.png":             "PNGDATA",
		"ppt/presentation.xml":             presentation("rId1", "rId2"),
		"ppt/_rels/presentation.xml.rels":  presentationRels(map[string]string{"rId1": "slides/slide2.xml", "rId2": "slides/slide1.xml"}),
	})
	md, imgs, err := PptxToMarkdown(data)
	if err != nil {
		t.Fatalf("PptxToMarkdown: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("got %d images, want 1", len(imgs))
	}
	if imgs[0].Page != 1 {
		t.Errorf("image page = %d, want 1 (first slide in the deck)", imgs[0].Page)
	}
	if strings.Count(md, "[Image: 1]") != 2 {
		t.Errorf("marker should appear on both slides:\n%s", md)
	}
}
