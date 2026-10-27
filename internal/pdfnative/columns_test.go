package pdfnative

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func prose(x0, width, yTop float64, n int, firstWords []string) [][]pdf.Text {
	rows := make([][]pdf.Text, 0, n)
	for i := 0; i < n; i++ {
		text := "and so on in running text"
		if i < len(firstWords) {
			text = firstWords[i]
		}
		rows = append(rows, []pdf.Text{{X: x0, W: width, Y: yTop - float64(i)*12, FontSize: 10, S: text}})
	}
	return rows
}

func TestSplitColumnRows_TwoProseColumns(t *testing.T) {
	left := prose(0, 200, 700, 12, []string{"The start of the paragraph", "and its continuation"})
	right := prose(230, 200, 694, 12, []string{"Right column begins", "with its own text here"})
	var rows [][]pdf.Text
	for i := 0; i < 12; i++ {
		rows = append(rows, left[i], right[i])
	}
	out := splitColumnRows(rows)
	if len(out) != 24 {
		t.Fatalf("row count changed: %d", len(out))
	}
	for i := 0; i < 12; i++ {
		if out[i][0].X != 0 {
			t.Fatalf("row %d should be from the left column, got X=%v (%s)", i, out[i][0].X, rowText(out[i]))
		}
	}
	for i := 12; i < 24; i++ {
		if out[i][0].X != 230 {
			t.Fatalf("row %d should be from the right column, got X=%v", i, out[i][0].X)
		}
	}
}

func TestSplitColumnRows_KeyValueTableUntouched(t *testing.T) {
	keys := []string{"Start of deadline", "(only if applicable)", "End of deadline", "Finality", "Start date", "End date", "Decision status", "Date received", "Date sent", "Clerk in charge", "Case number", "Remarks field"}
	var rows [][]pdf.Text
	for i := 0; i < 12; i++ {
		y := 700 - float64(i)*12
		rows = append(rows,
			[]pdf.Text{{X: 0, W: 150, Y: y, FontSize: 10, S: keys[i]}},
			[]pdf.Text{{X: 180, W: 200, Y: y - 6, FontSize: 10, S: "is calculated automatically and kept up to date"}},
		)
	}
	out := splitColumnRows(rows)
	for i := range rows {
		if rowText(out[i]) != rowText(rows[i]) {
			t.Fatalf("table rows were reordered at %d: %q", i, rowText(out[i]))
		}
	}
}

func TestLabelledHeading(t *testing.T) {
	mk := func(texts ...string) []line {
		ls := make([]line, len(texts))
		for i, s := range texts {
			ls[i] = line{text: s, fontSize: 10}
		}
		return ls
	}
	t.Run("complete on one line", func(t *testing.T) {
		ls := mk("Article 2 [Example title]", "(1) Every person ...")
		lvl, txt, abs, ok := labelledHeading(ls, 0, ls[0].text)
		if !ok || lvl != 3 || abs != 0 || txt != "Article 2 [Example title]" {
			t.Fatalf("got lvl=%d txt=%q abs=%d ok=%v", lvl, txt, abs, ok)
		}
	})
	t.Run("bare label, bracketed title on next line", func(t *testing.T) {
		ls := mk("Article 7", "[Sample subject]", "(1) The entire sample subject ...")
		_, txt, abs, ok := labelledHeading(ls, 0, ls[0].text)
		if !ok || abs != 1 || txt != "Article 7 [Sample subject]" {
			t.Fatalf("got txt=%q abs=%d ok=%v", txt, abs, ok)
		}
	})
	t.Run("title wrapping over two lines", func(t *testing.T) {
		ls := mk("Article 1 [Example title – First subject", "– Second subject of the example]", "(1) ...")
		_, txt, abs, ok := labelledHeading(ls, 0, ls[0].text)
		if !ok || abs != 1 || !strings.HasSuffix(txt, "the example]") {
			t.Fatalf("got txt=%q abs=%d ok=%v", txt, abs, ok)
		}
	})
	t.Run("title-less article straight into (1)", func(t *testing.T) {
		ls := mk("Article 13", "(1) The item is protected.")
		_, txt, abs, ok := labelledHeading(ls, 0, ls[0].text)
		if !ok || abs != 0 || txt != "Article 13" {
			t.Fatalf("got txt=%q abs=%d ok=%v", txt, abs, ok)
		}
	})
	t.Run("prose mentioning an article is not a heading", func(t *testing.T) {
		ls := mk("Article 5 of the Agreement states that", "everyone has the right ...")
		if _, _, _, ok := labelledHeading(ls, 0, ls[0].text); ok {
			t.Fatal("prose line must not become a heading")
		}
	})
}

func TestIsTOCPage(t *testing.T) {
	row := func(s string) []pdf.Text { return []pdf.Text{{S: s, FontSize: 10}} }
	toc := [][]pdf.Text{
		row("TABLE OF CONTENTS"),
		row("1 SAMPLE OVERVIEW (REF A 05) 4"),
		row("2 CREATING A RECORD (REF A 01, 02) 7"),
		row("2.1 Creation step (Appeal) 8"),
		row("3 PROCESSING 13"),
		row("4 INTERFACES 21"),
		row("5 DATA MODEL 29"),
		row("6 SCREENS 35"),
		row("7 APPENDIX 41"),
		row("Art. 8 Transitional rules 44"),
	}
	if !isTOCPage(toc, 0, nil, false) {
		t.Error("a dense contents page should be detected")
	}
	body := [][]pdf.Text{
		row("3.2 Actors and systems"),
		row("The process describes the end-to-end flow from receipt"),
		row("of a request until its successful completion"),
		row("or its documented failure."),
		row("The criteria follow in chapter 4."),
	}
	if isTOCPage(body, 0, nil, false) {
		t.Error("a body page must not be dropped as a TOC")
	}
}

func TestExpandLigatures(t *testing.T) {
	if got := expandLigatures("difﬁcult"); got != "difficult" {
		t.Errorf("got %q", got)
	}
	if got := expandLigatures("plain ascii"); got != "plain ascii" {
		t.Errorf("ascii fast path changed text: %q", got)
	}
}

func TestGlyphNameToText(t *testing.T) {
	cases := map[string]string{
		"eth": "ð", "thorn": "þ", "Thorn": "Þ", "germandbls": "ß",
		"f_t": "ft", "T_h": "Th", "f_iacute": "fí",
		"uni0041": "A", "u00E4": "ä", "a": "a", "Q": "Q",
		"endash": "–", "unknownglyph": "",
	}
	for name, want := range cases {
		if got := glyphNameToText(name); got != want {
			t.Errorf("glyphNameToText(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCMapHexHelpers(t *testing.T) {
	if c, ok := hexTok("<0041>"); !ok || c != 0x41 {
		t.Errorf("hexTok = %v %v", c, ok)
	}
	if _, ok := hexTok("<004100>"); ok {
		t.Error("3-byte token must not parse as a code")
	}
	if s, ok := hexStr("<00660074>"); !ok || s != "ft" {
		t.Errorf("hexStr ligature = %q %v", s, ok)
	}
	if s, ok := hexStr("<00FE>"); !ok || s != "þ" {
		t.Errorf("hexStr thorn = %q %v", s, ok)
	}
}

func TestIsTOCPage_LabelFreeRestated(t *testing.T) {
	row := func(s string) []pdf.Text { return []pdf.Text{{S: s, FontSize: 10}} }
	titles := []string{
		"The season at a glance", "Garden planning in brief", "Weather outlook summary",
		"Soil quality review", "Orchard maintenance notes", "Harvest operations overview",
		"Tools and equipment", "Annual yield discussion",
	}
	entries := [][]pdf.Text{row("Contents")}
	var content [][]pdf.Text
	for i, ti := range titles {
		entries = append(entries, row(fmt.Sprintf("%s %d", ti, 4+i*7)))
		content = append(content, row(ti))
	}
	pages := []page{{rows: entries}, {rows: content}}
	idx := entryPageIndex(pages)

	if !isTOCPage(entries, 0, idx, false) {
		t.Error("entries restated later in the document should drop the page")
	}
	alone := entryPageIndex([]page{{rows: entries}})
	if isTOCPage(entries, 0, alone, false) {
		t.Error("unrestated label-free entries must not drop the page")
	}
}

func TestDropTOCPagesTakesTheTailOfADirectory(t *testing.T) {
	row := func(s string) []pdf.Text { return []pdf.Text{{S: s, FontSize: 10}} }
	rows := func(ss ...string) [][]pdf.Text {
		out := make([][]pdf.Text, 0, len(ss))
		for _, s := range ss {
			out = append(out, row(s))
		}
		return out
	}

	full := rows(
		"Table of contents",
		"1 Structure Overview 13",
		"1.1 Process: A1.1 - Split Handling 14",
		"1.2 Process: A1.10 - Parallel Handling 21",
		"2 Layout Management 44",
		"2.1 Process: B4.10 - Linked Items 45",
		"3 Data Structure 61",
		"4 Transaction 92",
		"5 Life Cycle 97",
		"6 Parallel Handling 140",
	)
	tail := rows(
		"Design and Process Description - Part Three",
		"8.9.2 Process description including local adjustments 155",
		"8.10 Process: A1 - Alerts/Notifications 156",
		"8.10.1 Overview 156",
		"8.10.2 Process description 158",
	)
	body := rows(
		"1 Structure Overview",
		"The structure is modelled in the example system.",
		"Each unit carries a functional and a",
		"line responsibility, which are maintained separately.",
	)

	pages := []page{{rows: full, num: 2}, {rows: tail, num: 3}, {rows: body, num: 4}}
	kept := dropTOCPages(pages)

	if len(kept) != 1 {
		var nums []int
		for _, p := range kept {
			nums = append(nums, p.num)
		}
		t.Fatalf("kept pages %v, want only the body page (4)", nums)
	}
	if kept[0].num != 4 {
		t.Errorf("kept page %d, want the body page 4", kept[0].num)
	}
}

func TestDropTOCPagesKeepsAShortDirectoryStandingAlone(t *testing.T) {
	row := func(s string) []pdf.Text { return []pdf.Text{{S: s, FontSize: 10}} }
	rows := func(ss ...string) [][]pdf.Text {
		out := make([][]pdf.Text, 0, len(ss))
		for _, s := range ss {
			out = append(out, row(s))
		}
		return out
	}

	lonely := rows(
		"3.1 Creation step 8",
		"3.2 Processing 13",
		"3.3 Completion 21",
	)
	prose := rows(
		"The process describes the end-to-end flow from receipt",
		"of a request until the completed rollout.",
	)

	pages := []page{{rows: prose, num: 1}, {rows: lonely, num: 2}}
	if kept := dropTOCPages(pages); len(kept) != 2 {
		t.Errorf("kept %d pages, want both — a short directory that follows no "+
			"contents page must not be dropped", len(kept))
	}
}

func TestRenderVerbatimKeepsWhatRenderDrops(t *testing.T) {
	cover := synthPage{runs: []textRun{
		{text: "Specification", x: 150, yTop: 300, size: 28, bold: true},
		{text: "Version 1.0", x: 150, yTop: 360, size: 14},
	}}
	contents := synthPage{runs: []textRun{
		{text: "Table of contents", x: 72, yTop: 90, size: 16, bold: true},
	}}
	for i, entry := range []string{
		"1 Objectives 3", "1.1 Exclusions 3", "2 Requirements 4",
		"2.1 Capture 4", "2.2 Review 5", "3 Responsibilities 6",
		"4 Interfaces 7", "5 Data model 8", "6 Appendix 9",
	} {
		contents.runs = append(contents.runs, textRun{
			text: entry, x: 72, yTop: float64(120 + i*20), size: 10,
		})
	}
	body := []synthPage{
		{runs: []textRun{
			{text: "1 Objectives", x: 72, yTop: 120, size: 16, bold: true},
			{text: "Intake has so far been done by phone.", x: 72, yTop: 150, size: 10},
		}},
		{runs: []textRun{
			{text: "2 Requirements", x: 72, yTop: 120, size: 16, bold: true},
			{text: "Payment handling remains out of scope.", x: 72, yTop: 150, size: 10},
		}},
	}
	data := buildPDF(t, append([]synthPage{cover, contents}, body...)...)

	plain, _, err := Render(data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	whole, _, _, err := RenderVerbatim(data)
	if err != nil {
		t.Fatalf("RenderVerbatim: %v", err)
	}
	t.Logf("default:\n%s\n---\nverbatim:\n%s", plain, whole)

	if plain == whole {
		t.Fatal("the two renderings are identical — nothing was dropped, so nothing " +
			"was kept back either and this fixture no longer covers the difference")
	}
	for _, want := range []string{"Table of contents", "1.1 Exclusions"} {
		if !strings.Contains(whole, want) {
			t.Errorf("verbatim rendering lost %q:\n%s", want, whole)
		}
		if strings.Contains(plain, want) {
			t.Errorf("default rendering kept %q, which it exists to drop:\n%s", want, plain)
		}
	}
	for _, want := range []string{"phone", "Payment"} {
		if !strings.Contains(plain, want) || !strings.Contains(whole, want) {
			t.Errorf("body text %q missing from one of the renderings", want)
		}
	}
}

// TestDropTOCPagesKeepsRepeatedContent covers a document that prints the same section
// twice: the first copy restates the second word for word, which is not a contents page.
func TestDropTOCPagesKeepsRepeatedContent(t *testing.T) {
	row := func(s string) []pdf.Text { return []pdf.Text{{S: s, FontSize: 10}} }
	var section [][]pdf.Text
	for i := 1; i <= 10; i++ {
		section = append(section, row(fmt.Sprintf("Item number %d of the long table", i)))
	}
	pages := []page{{rows: section, num: 1}, {rows: section, num: 2}, {rows: section, num: 3}}
	if kept := dropTOCPages(pages); len(kept) != 3 {
		t.Errorf("kept %d pages, want all 3: repeated content is not a contents page", len(kept))
	}
}
