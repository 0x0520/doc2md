package pdfnative

import (
	"strings"
	"testing"
)

func threePageSpec() []synthPage {
	page := func(n string, heading, body string) synthPage {
		return synthPage{runs: []textRun{
			{text: n + " " + heading, x: 72, yTop: 120, size: 16, bold: true},
			{text: body, x: 72, yTop: 150, size: 10},
		}}
	}
	return []synthPage{
		page("1", "Objectives", "Intake has so far been done by phone."),
		page("2", "Exclusions", "Payment handling remains out of scope."),
		page("3", "Roles", "Case handling checks the process."),
	}
}

func TestRenderWithPagesMapsEachPage(t *testing.T) {
	md, _, breaks, err := RenderWithPages(buildPDF(t, threePageSpec()...))
	if err != nil {
		t.Fatalf("RenderWithPages: %v", err)
	}
	t.Logf("markdown:\n%s\nbreaks: %+v", md, breaks)

	if len(breaks) != 3 {
		t.Fatalf("%d breaks, want 3: %+v", len(breaks), breaks)
	}
	for i, want := range []int{1, 2, 3} {
		if breaks[i].Page != want {
			t.Errorf("break %d is page %d, want %d", i, breaks[i].Page, want)
		}
	}

	for i, b := range breaks {
		if b.Offset < 0 || b.Offset > len(md) {
			t.Fatalf("break %d offset %d outside markdown of %d bytes", i, b.Offset, len(md))
		}
		if i > 0 && b.Offset <= breaks[i-1].Offset {
			t.Errorf("break %d offset %d does not advance past %d", i, b.Offset, breaks[i-1].Offset)
		}
	}

	for i, heading := range []string{"1 Objectives", "2 Exclusions", "3 Roles"} {
		rest := md[breaks[i].Offset:]
		if !strings.HasPrefix(strings.TrimLeft(rest, "#\n "), heading) {
			t.Errorf("break %d (page %d) points at %q, want the section starting %q",
				i, breaks[i].Page, truncateForMsg(rest), heading)
		}
	}
}

func TestRenderWithPagesLeavesMarkdownUntouched(t *testing.T) {
	data := buildPDF(t, threePageSpec()...)

	withPages, _, breaks, err := RenderWithPages(data)
	if err != nil {
		t.Fatalf("RenderWithPages: %v", err)
	}
	plain, _, err := Render(data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if withPages != plain {
		t.Errorf("Render and RenderWithPages disagree on the Markdown:\n%q\nvs\n%q", plain, withPages)
	}
	if strings.Contains(withPages, "page:") {
		t.Errorf("page marker leaked into the rendering:\n%s", withPages)
	}
	if len(breaks) == 0 {
		t.Error("no page breaks reported for a three-page document")
	}
}

func TestRenderWithPagesKeepsSourceNumbering(t *testing.T) {
	cover := synthPage{runs: []textRun{
		{text: "Specification", x: 150, yTop: 300, size: 28, bold: true},
		{text: "Version 1.0", x: 150, yTop: 360, size: 14},
	}}
	frontMatter := synthPage{runs: []textRun{
		{text: "Document information", x: 72, yTop: 120, size: 16, bold: true},
		{text: "Approved by the responsible department.", x: 72, yTop: 150, size: 10},
	}}
	data := buildPDF(t, append([]synthPage{cover, frontMatter}, threePageSpec()...)...)

	md, _, breaks, err := RenderWithPages(data)
	if err != nil {
		t.Fatalf("RenderWithPages: %v", err)
	}
	t.Logf("markdown:\n%s\nbreaks: %+v", md, breaks)

	if strings.Contains(md, "Version 1.0") {
		t.Fatalf("the cover page was not dropped, so this fixture no longer covers "+
			"renumbering:\n%s", md)
	}
	if len(breaks) == 0 {
		t.Fatal("no page breaks reported")
	}
	if breaks[0].Page != 2 {
		t.Errorf("first break is page %d, want 2 -- the source numbering must survive "+
			"the dropped cover", breaks[0].Page)
	}
	if breaks[0].Offset != 0 {
		t.Errorf("first break at offset %d, want 0: the rendering starts on that page",
			breaks[0].Offset)
	}
	for _, b := range breaks {
		if b.Page == 3 && strings.HasPrefix(strings.TrimLeft(md[b.Offset:], "#\n "), "1 Objectives") {
			return
		}
	}
	t.Errorf("no break marks page 3 as the start of \"1 Objectives\": %+v", breaks)
}

func TestRenderWithPagesOnSinglePage(t *testing.T) {
	md, _, breaks, err := RenderWithPages(buildPDF(t, threePageSpec()[0]))
	if err != nil {
		t.Fatalf("RenderWithPages: %v", err)
	}
	if len(breaks) != 1 {
		t.Fatalf("%d breaks, want 1: %+v", len(breaks), breaks)
	}
	if breaks[0].Page != 1 || breaks[0].Offset != 0 {
		t.Errorf("break = %+v, want {Offset:0 Page:1}", breaks[0])
	}
	if !strings.Contains(md, "Objectives") {
		t.Errorf("body text missing:\n%s", md)
	}
}

func truncateForMsg(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
