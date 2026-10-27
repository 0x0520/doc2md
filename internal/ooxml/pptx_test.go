package ooxml

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const nsPptx = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`

func TestPptxToMarkdown(t *testing.T) {
	slide := `<?xml version="1.0"?>
<p:sld ` + nsPptx + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:txBody><a:p><a:r><a:t>Title</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:txBody><a:p><a:r><a:t>Content A</a:t></a:r></a:p></p:txBody></p:sp>
<p:graphicFrame><a:graphic><a:graphicData>
<a:tbl>
<a:tr><a:tc><a:txBody><a:p><a:r><a:t>H1</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>H2</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
<a:tr><a:tc><a:txBody><a:p><a:r><a:t>x</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>y</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
</a:tbl>
</a:graphicData></a:graphic></p:graphicFrame>
</p:spTree></p:cSld></p:sld>`

	data := zipDocx(t, map[string]string{
		"[Content_Types].xml":   `<?xml version="1.0"?><Types/>`,
		"ppt/slides/slide1.xml": slide,
	})

	md, _, err := PptxToMarkdown(data)
	if err != nil {
		t.Fatalf("PptxToMarkdown: %v", err)
	}
	for _, want := range []string{
		"# Title",
		"Content A",
		"| H1 | H2 |\n|---|---|\n| x | y |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("output missing %q\n--- full ---\n%s", want, md)
		}
	}
}

func titleSlide(text string) string {
	return `<?xml version="1.0"?>
<p:sld ` + nsPptx + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`
}

// Without presentation.xml the part number is the only order available.
func TestPptxToMarkdown_NoPresentationFallsBackToPartNumber(t *testing.T) {
	parts := map[string]string{"[Content_Types].xml": `<?xml version="1.0"?><Types/>`}
	for _, n := range []int{3, 1, 12, 2, 10} {
		parts[fmt.Sprintf("ppt/slides/slide%d.xml", n)] = titleSlide("Marker" + strconv.Itoa(n) + "End")
	}
	md, _, err := PptxToMarkdown(zipDocx(t, parts))
	if err != nil {
		t.Fatalf("pptx: %v", err)
	}
	prev := -1
	for _, n := range []int{1, 2, 3, 10, 12} {
		idx := strings.Index(md, "Marker"+strconv.Itoa(n)+"End")
		if idx < 0 {
			t.Fatalf("slide %d missing:\n%s", n, md)
		}
		if idx < prev {
			t.Errorf("slide %d out of numeric order:\n%s", n, md)
		}
		prev = idx
	}
}

func TestPptxToMarkdown_PartCountCapped(t *testing.T) {
	parts := map[string]string{"[Content_Types].xml": `<?xml version="1.0"?><Types/>`}
	parts["ppt/slides/slide1.xml"] = titleSlide("FirstSlide")
	for n := 2; n <= maxSlideParts+5; n++ {
		text := "Body"
		if n == maxSlideParts+5 {
			text = "OVERFLOWSENTINEL"
		}
		parts[fmt.Sprintf("ppt/slides/slide%d.xml", n)] = titleSlide(text)
	}
	md, _, err := PptxToMarkdown(zipDocx(t, parts))
	if err != nil {
		t.Fatalf("pptx: %v", err)
	}
	if !strings.Contains(md, "FirstSlide") {
		t.Errorf("first slide missing")
	}
	if strings.Contains(md, "OVERFLOWSENTINEL") {
		t.Errorf("slide beyond maxSlideParts should have been dropped")
	}
}

func TestPptxToMarkdown_NotPptx(t *testing.T) {
	if _, _, err := PptxToMarkdown([]byte("not a zip")); err == nil {
		t.Error("expected error for non-zip bytes")
	}
	noSlides := zipDocx(t, map[string]string{"hello.txt": "hi"})
	if _, _, err := PptxToMarkdown(noSlides); err == nil {
		t.Error("expected error for zip without slides")
	}
}
