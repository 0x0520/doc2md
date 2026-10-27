package pdfnative

import (
	"fmt"
	"strings"
	"testing"
)

// inlinePDF is one page with body text set in /F1 (regular), /F2 (bold by name),
// /F3 (italic by name) and /F4 (bold only by its descriptor's weight), plus the
// given annotations.
func inlinePDF(content string, annots ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R /F2 6 0 R /F3 7 0 R /F4 8 0 R >> >> /Contents 4 0 R /Annots [%s] >>", strings.Join(annots, " ")),
		stream("", content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Oblique >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /ABCDEF+Corporate /FontDescriptor << /Type /FontDescriptor /FontName /ABCDEF+Corporate /Flags 32 /FontWeight 700 /ItalicAngle 0 >> >>",
	}
	return pdfFromObjects(objs)
}

func linkAnnot(x0, y0, x1, y1 float64, uri string) string {
	return fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%.2f %.2f %.2f %.2f] /A << /S /URI /URI (%s) >> >>", x0, y0, x1, y1, uri)
}

func TestRenderEmphasisAndLinks(t *testing.T) {
	const size = 12.0
	// Without /Widths the advance is estimated, so the link rectangle is placed with the same estimate.
	before := "A term in bold, one in italic and a "
	x := 72 + estimateWidth(before, size)
	// The rectangle takes in the full stop after the word, as Word draws it.
	x1 := x + estimateWidth("link.", size) - 1
	content := "BT /F1 12 Tf 72 700 Td (A term in ) Tj /F2 12 Tf (bold) Tj /F1 12 Tf (, one in ) Tj " +
		"/F3 12 Tf (italic) Tj /F1 12 Tf ( and a link.) Tj ET " +
		"BT /F1 12 Tf 72 650 Td (Weight from the ) Tj /F4 12 Tf (descriptor) Tj /F1 12 Tf ( counts too.) Tj ET " +
		"BT /F1 12 Tf 72 600 Td (A script link is plain text.) Tj ET"
	md, err := Extract(inlinePDF(content,
		linkAnnot(x-1, 696, x1, 712, "https://example.org/spec"),
		linkAnnot(70, 596, 300, 612, " JavaScript:alert(1)"),
	))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, want := range []string{
		"A term in **bold**, one in _italic_ and a [link](https://example.org/spec).",
		"Weight from the **descriptor** counts too.",
		"A script link is plain text.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "](") && strings.Contains(strings.ToLower(md), "javascript") {
		t.Errorf("a javascript: link was kept:\n%s", md)
	}
}

func TestInlineTextDropsEmphasisOfStyledBlocks(t *testing.T) {
	render := func(lines ...line) string {
		return blocksMarkdown(groupBlocks([]page{{lines: lines}}, 11, false))
	}
	// A paragraph bold throughout keeps its links but not its bold.
	got := render(
		line{text: "The whole paragraph is set in bold and", rich: "**The whole paragraph is set in bold and**", styled: true, y: 500, xStart: 72, fontSize: 11},
		line{text: "links to the spec.", rich: "**links to the [spec](https://example.org)**.", linked: "links to the [spec](https://example.org).", styled: true, y: 486, xStart: 72, fontSize: 11},
	)
	if want := "The whole paragraph is set in bold and links to the [spec](https://example.org).\n"; got != want {
		t.Errorf("styled paragraph:\n got: %q\nwant: %q", got, want)
	}
	// Spans running over a line break merge, and a hyphenated word is repaired inside them.
	got = render(
		line{text: "Only some words are bold compo-", rich: "Only some words are **bold compo-**", y: 500, xStart: 72, fontSize: 11},
		line{text: "nent names here.", rich: "**nent names** here.", y: 486, xStart: 72, fontSize: 11},
	)
	if want := "Only some words are **bold component names** here.\n"; got != want {
		t.Errorf("span over a line break:\n got: %q\nwant: %q", got, want)
	}
	// A list item keeps its marker when its text is emphasised.
	got = render(line{text: "• Faster uploads", rich: "• **Faster** uploads", y: 500, xStart: 72, fontSize: 11})
	if want := "- **Faster** uploads\n"; got != want {
		t.Errorf("list item:\n got: %q\nwant: %q", got, want)
	}
}
