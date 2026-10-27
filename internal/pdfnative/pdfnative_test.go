package pdfnative

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func TestJoinLine_Spacing(t *testing.T) {
	tests := []struct {
		name string
		toks []pdf.Text
		want string
	}{
		{
			name: "explicit space glyph is kept",
			toks: []pdf.Text{
				{X: 0, W: 10, S: "Hello", FontSize: 10},
				{X: 10, W: 3, S: " ", FontSize: 10},
				{X: 13, W: 10, S: "World", FontSize: 10},
			},
			want: "Hello World",
		},
		{
			name: "positional gap inserts a space",
			toks: []pdf.Text{
				{X: 0, W: 10, S: "Hello", FontSize: 10},
				{X: 20, W: 10, S: "World", FontSize: 10},
			},
			want: "Hello World",
		},
		{
			name: "contiguous glyphs stay one word",
			toks: []pdf.Text{
				{X: 0, W: 5, S: "He", FontSize: 10},
				{X: 5, W: 5, S: "llo", FontSize: 10},
			},
			want: "Hello",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinLine(tc.toks).text; got != tc.want {
				t.Errorf("joinLine = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyHeading(t *testing.T) {
	body := func(text string) line { return line{text: text, fontSize: 11, y: 0} }
	tests := []struct {
		name       string
		target     line
		wantOK     bool
		wantNumber bool
		wantLevel  int
	}{
		{"dotted outline", line{text: "3.1 Requirement package", fontSize: 11, y: 88}, true, true, 2},
		{"appendix outline", line{text: "A.2.1 Interfaces", fontSize: 11, y: 88}, true, true, 3},
		{"bare integer, bold", line{text: "4 System architecture", fontSize: 11, y: 88, bold: true}, true, true, 1},
		{"numbered list item", line{text: "1. The name can be entered.", fontSize: 11, y: 88}, false, false, 0},
		{"large font", line{text: "Introduction", fontSize: 18, y: 88}, true, false, 0},
		{"body sentence", line{text: "The system must store the data.", fontSize: 11, y: 88}, false, false, 0},
		{"inline bold label at body size", line{text: "Remark", fontSize: 11, y: 88, bold: true}, false, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := []line{body("Previous paragraph text"), tc.target, body("Following paragraph text")}
			lines[0].y, lines[2].y = 100, 76
			st := computeStats(lines, 11)
			lvl, _, ok, numbered, _ := classifyHeading(lines, 1, st, nil)
			if ok != tc.wantOK || numbered != tc.wantNumber {
				t.Fatalf("classifyHeading(%q) = (ok %v, numbered %v), want (ok %v, numbered %v)", tc.target.text, ok, numbered, tc.wantOK, tc.wantNumber)
			}
			if tc.wantNumber && lvl != tc.wantLevel {
				t.Errorf("classifyHeading(%q) level = %d, want %d", tc.target.text, lvl, tc.wantLevel)
			}
		})
	}
}

func TestClassifyHeading_TableAdjacency(t *testing.T) {
	const body = 11.0
	tableRow := line{text: "| Status | Meaning |", fontSize: body, y: 88, pre: true}
	prose := func(text string, y float64) line { return line{text: text, fontSize: body, y: y} }

	tests := []struct {
		name   string
		lines  []line
		target int
		wantOK bool
	}{
		{
			name:   "bold body-size caption under a table is not a heading",
			lines:  []line{prose("The status controls the processing.", 110), tableRow, {text: "Table 3: Status values", fontSize: body, y: 70, bold: true}, prose("The deadlines apply from receipt.", 56)},
			target: 2,
		},
		{
			name:   "bold body-size caption above a table is not a heading either",
			lines:  []line{prose("The status controls the processing.", 110), {text: "Table 3: Status values", fontSize: body, y: 96, bold: true}, tableRow, prose("The deadlines apply from receipt.", 56)},
			target: 1,
		},
		{
			name:   "a larger face directly above the table is a real heading",
			lines:  []line{prose("The status controls the processing.", 110), {text: "Status model", fontSize: 15, y: 96, bold: true}, tableRow, prose("The deadlines apply from receipt.", 56)},
			target: 1,
			wantOK: true,
		},
		{
			name:   "body text between heading and table keeps the heading",
			lines:  []line{prose("Previous paragraph text", 124), {text: "Status model", fontSize: body, y: 110, bold: true}, prose("The following table shows the values.", 96), tableRow},
			target: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := computeStats(tc.lines, body)
			_, _, ok, _, _ := classifyHeading(tc.lines, tc.target, st, nil)
			if ok != tc.wantOK {
				t.Errorf("classifyHeading(%q) ok = %v, want %v", tc.lines[tc.target].text, ok, tc.wantOK)
			}
		})
	}

	withProse := []line{prose("The status controls the processing.", 160), {text: "Table 3: Status values", fontSize: 11, y: 110, bold: true}, prose("The deadlines apply from receipt.", 56)}
	if _, _, ok, _, _ := classifyHeading(withProse, 1, computeStats(withProse, 11), nil); !ok {
		t.Error("with prose neighbours the caption should still score as a heading — otherwise this test no longer proves the veto is what demotes it")
	}
}

func TestChapterSequence(t *testing.T) {
	const body = 11.0
	plain := func(text string) line { return line{text: text, fontSize: body} }
	big := func(text string) line { return line{text: text, fontSize: 16, bold: true} }
	seqOf := func(lines []line) map[int]bool { return chapterSequence(lines, computeStats(lines, body)) }

	t.Run("successor of a prominent chapter is promoted", func(t *testing.T) {
		lines := []line{big("3 Objectives"), plain("The goals are described below."), plain("4 System architecture")}
		if seq := seqOf(lines); !seq[2] {
			t.Errorf("line 2 should be promoted, got %v", seq)
		}
	})
	t.Run("a whole document numbered this way resolves", func(t *testing.T) {
		lines := []line{plain("1 Introduction"), plain("The reason is known."), plain("2 Starting point"), plain("Today everything is done on paper."), plain("3 Objectives")}
		seq := seqOf(lines)
		for _, i := range []int{0, 2, 4} {
			if !seq[i] {
				t.Errorf("line %d should be promoted, got %v", i, seq)
			}
		}
	})
	t.Run("a dotted number establishes the chapter", func(t *testing.T) {
		lines := []line{plain("3.4 Data migration"), plain("The data arrives nightly."), plain("4 System architecture")}
		if seq := seqOf(lines); !seq[2] {
			t.Errorf("line 2 should be promoted after 3.4, got %v", seq)
		}
	})
	t.Run("a number out of sequence is refused", func(t *testing.T) {
		lines := []line{big("3 Objectives"), plain("The goals are described below."), plain("7 working days after receipt the notice follows")}
		if seq := seqOf(lines); len(seq) != 0 {
			t.Errorf("nothing should be promoted, got %v", seq)
		}
	})
	t.Run("shape guards still apply to the successor", func(t *testing.T) {
		long := "4 System architecture and the associated interfaces including the runtime environment in detail"
		for _, text := range []string{
			"4. System architecture",
			"4 System architecture.",
			long,
		} {
			lines := []line{big("3 Objectives"), plain("The goals are described below."), plain(text)}
			if seq := seqOf(lines); len(seq) != 0 {
				t.Errorf("%q should not be promoted, got %v", text, seq)
			}
		}
	})
}

// TestGroupBlocks_RepairsMovedFromDownstream covers heading and list decisions made
// while lines are still separate: bulleted headings, doubled bullets, stranded section numbers.
func TestGroupBlocks_RepairsMovedFromDownstream(t *testing.T) {
	const body = 11.0
	at := func(text string, y float64, size float64) line {
		return line{text: text, fontSize: size, y: y}
	}
	run := func(lines ...line) string { return blocksMarkdown(groupBlocks([]page{{lines: lines}}, body, false)) }

	t.Run("a bullet in front of a numbered heading is decoration, not list content", func(t *testing.T) {
		got := run(
			at("The request is entered in the tool.", 300, body),
			at("• 3.4 Data migration from the legacy system", 250, body),
			at("The existing records are migrated nightly.", 200, body),
		)
		if !strings.Contains(got, "## 3.4 Data migration from the legacy system") {
			t.Errorf("bulleted outline heading not promoted:\n%s", got)
		}
	})
	t.Run("a genuine bulleted step stays a list item", func(t *testing.T) {
		got := run(
			at("Initial review flow:", 300, body),
			at("• Receive the request and check completeness.", 250, body),
		)
		if strings.Contains(got, "# Receive") {
			t.Errorf("a bulleted step must not become a heading:\n%s", got)
		}
		if !strings.Contains(got, "- Receive the request") {
			t.Errorf("bulleted step lost its list marker:\n%s", got)
		}
	})
	t.Run("a doubled bullet is collapsed", func(t *testing.T) {
		got := run(at("- • First item", 300, body))
		if strings.Contains(got, "- -") || strings.Contains(got, "- •") {
			t.Errorf("doubled bullet survived: %q", got)
		}
	})
	t.Run("a chapter run together with its sub-heading is split", func(t *testing.T) {
		got := run(
			at("2 Functional requirements 2.1 Requirement package Example cases", 300, 16),
			at("The requirements are described below.", 250, body),
		)
		if !strings.Contains(got, "# 2 Functional requirements") {
			t.Errorf("chapter part missing:\n%s", got)
		}
		if !strings.Contains(got, "## 2.1 Requirement package Example cases") {
			t.Errorf("sub-heading part missing or not levelled from its number:\n%s", got)
		}
	})
	t.Run("a version subtitle is not split on its numbers", func(t *testing.T) {
		got := run(
			at("Specification Version 1.0 Dated: 12.03.2025", 300, 16),
			at("The introduction follows.", 250, body),
		)
		if strings.Count(got, "#") > 1 && strings.Contains(got, "# 1.0") {
			t.Errorf("subtitle was split on a version number:\n%s", got)
		}
	})
	t.Run("a stranded section number rejoins the sentence it was torn from", func(t *testing.T) {
		got := run(
			at("4 System architecture", 400, 16),
			at("The stabilisation proceeds according to step", 300, body),
			at("4.8.", 250, body),
		)
		if strings.Contains(got, "\n4.8.") {
			t.Errorf("stranded section number became its own block:\n%s", got)
		}
		if !strings.Contains(got, "step 4.8.") {
			t.Errorf("section number was not rejoined:\n%s", got)
		}
	})
	t.Run("a date and a version are not stranded section numbers", func(t *testing.T) {
		got := run(
			at("4 System architecture", 460, 16),
			at("Document designation", 400, body),
			at("0.1", 350, body),
			at("Version", 300, body),
			at("14.02.2025", 250, body),
		)
		if strings.Contains(got, "Document designation 0.1") {
			t.Errorf("a version number was merged as a section reference:\n%s", got)
		}
		if strings.Contains(got, "Version 14.02.2025") {
			t.Errorf("a date was merged as a section reference:\n%s", got)
		}
	})
}

func TestIsLabelHeading(t *testing.T) {
	match := []string{
		"Article 12a", "Article 7 [Example title]", "Art. 3", "§ 7 Definitions",
		"Chapter 2 Basics", "Appendix B Transitional rules", "Annex B1",
		"Articolo 5 [Diritti]", "Rozdział 3 Postanowienia", "Artikel 9 Bepalingen",
		"Bölüm 4 Tanımlar", "Chapitre 6 Obligations",
	}
	reject := []string{
		"Version 1.0", "Dated 12.03.2025", "Edition 2,5",
		"Articles of association shall be filed", "Chapters and sections",
		"The third section 5",
	}
	for _, s := range match {
		if !isLabelHeading(s) {
			t.Errorf("isLabelHeading(%q) = false, want true", s)
		}
	}
	for _, s := range reject {
		if isLabelHeading(s) {
			t.Errorf("isLabelHeading(%q) = true, want false", s)
		}
	}
}

func TestBareLabelRe(t *testing.T) {
	match := []string{"Article 12a", "Art. 3", "§ 19", "Articolo 5", "Rozdział 3"}
	reject := []string{"Article 12a [Example title]", "§ 5 Definitions", "Version 1.0", "Chapter"}
	for _, s := range match {
		if !bareLabelRe.MatchString(s) {
			t.Errorf("bareLabelRe should match %q", s)
		}
	}
	for _, s := range reject {
		if bareLabelRe.MatchString(s) {
			t.Errorf("bareLabelRe should NOT match %q", s)
		}
	}
}

func TestNormalizeListMarker(t *testing.T) {
	tests := []struct{ in, want string }{
		{"• First item", "- First item"},
		{"- already Markdown", "- already Markdown"},
		{"* Asterisk", "- Asterisk"},
		{"1. Numbered", "1. Numbered"},
		{"2) Also numbered", "2) Also numbered"},
		{"  12.  Indented number", "12. Indented number"},
		{"• 3. Bullet before a number", "- 3. Bullet before a number"},
	}
	for _, tc := range tests {
		if got := normalizeListMarker(tc.in); got != tc.want {
			t.Errorf("normalizeListMarker(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGroupBlocks_ListContinuation(t *testing.T) {
	tests := []struct {
		name  string
		lines []line
		want  string
	}{
		{
			name: "wrapped bullet folds back into its item",
			lines: []line{
				{text: "• The period logic is verified using cut-off date", y: 540, xStart: 90, fontSize: 11},
				{text: "tests around month boundaries.", y: 525, xStart: 108, fontSize: 11},
				{text: "• Access rules are to be tested separately per user", y: 506, xStart: 90, fontSize: 11},
				{text: "grouped by region and team.", y: 491, xStart: 108, fontSize: 11},
			},
			want: "- The period logic is verified using cut-off date tests around month boundaries.\n" +
				"- Access rules are to be tested separately per user grouped by region and team.\n",
		},
		{
			name: "left-aligned paragraph after a bullet stays separate",
			lines: []line{
				{text: "• A single list item without a line break.", y: 540, xStart: 90, fontSize: 11},
				{text: "A separate paragraph at the left margin stands alone.", y: 525, xStart: 72, fontSize: 11},
			},
			want: "- A single list item without a line break.\n\n" +
				"A separate paragraph at the left margin stands alone.\n",
		},
		{
			name: "distant paragraph after a bullet stays separate",
			lines: []line{
				{text: "• A single list item without a line break.", y: 540, xStart: 90, fontSize: 11},
				{text: "A separate paragraph after a large gap stands alone.", y: 500, xStart: 108, fontSize: 11},
			},
			want: "- A single list item without a line break.\n\n" +
				"A separate paragraph after a large gap stands alone.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := blocksMarkdown(groupBlocks([]page{{lines: tc.lines}}, 11, false))
			if got != tc.want {
				t.Errorf("blocksMarkdown(groupBlocks) mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestJoinWrapped(t *testing.T) {
	tests := []struct{ acc, next, want string }{
		{"listed (RQ-", "023, NRQ-007).", "listed (RQ-023, NRQ-007)."},
		{"actual widget-", "Mounting possible", "actual widget-Mounting possible"},
		{"Authori-", "zation data missing", "Authorization data missing"},
		{"cut-off date tests around", "month boundaries", "cut-off date tests around month boundaries"},
	}
	for _, tc := range tests {
		if got := joinWrapped(tc.acc, tc.next); got != tc.want {
			t.Errorf("joinWrapped(%q, %q) = %q, want %q", tc.acc, tc.next, got, tc.want)
		}
	}
}

func TestPageNumRe(t *testing.T) {
	drop := []string{"3", "Seite 3", "page 12", "3 / 12", "  7  "}
	keep := []string{"3. First steps", "Version 1.0", "Chapter 3 describes"}
	for _, s := range drop {
		if !pageNumRe.MatchString(toLower(s)) {
			t.Errorf("pageNumRe should match %q", s)
		}
	}
	for _, s := range keep {
		if pageNumRe.MatchString(toLower(s)) {
			t.Errorf("pageNumRe should NOT match %q", s)
		}
	}
}

func TestDropCoverPage(t *testing.T) {
	row := func(s string) []pdf.Text { return []pdf.Text{{S: s, FontSize: 12}} }
	pg := func(rows ...[]pdf.Text) page { return page{rows: rows} }
	cover := pg(row("Example Corp - Specification"), row("001"), row("Version 0.1"))
	meta := pg(row("Publisher"), row("Author Sample Person"))
	toc := pg(row("Table of contents"), row("1 Starting point and objectives"))
	body := pg(row("1 Starting point and objectives"), row("Inside the Example Corp ..."))

	tests := []struct {
		name    string
		in      []page
		wantLen int
		wantTop string
	}{
		{"cover before contents on page 3", []page{cover, meta, toc, body}, 3, "Publisher"},
		{"content-first document is untouched", []page{body, body, body}, 3, "1 Starting point and objectives"},
		{"too short for a cover", []page{cover, body}, 2, "Example Corp - Specification"},
		{"first heading already on page 2 stays", []page{cover, body, body}, 3, "Example Corp - Specification"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dropCoverPage(tc.in)
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			if top := rowText(got[0].rows[0]); top != tc.wantTop {
				t.Errorf("first surviving row = %q, want %q", top, tc.wantTop)
			}
		})
	}
}

func TestMergeContinuationRows(t *testing.T) {
	in := [][]string{
		{"PM", "Project manager; read access to"},
		{"", "entries of their project."},
		{"SUP", "Edits in"},
		{"", "locked periods."},
	}
	hy := mergeContinuationRows([][]string{
		{"INV-Referencenum-", "Stored when it concerns a"},
		{"ber", "case against an invoice."},
	}, []bool{false, true})
	if len(hy) != 1 || hy[0][0] != "INV-Referencenumber" {
		t.Fatalf("hyphenated key not rejoined: %v", hy)
	}
	if hy[0][1] != "Stored when it concerns a case against an invoice." {
		t.Errorf("value column merge = %q", hy[0][1])
	}
	nm := mergeContinuationRows([][]string{{"Price-", "x"}, {"Department", "y"}}, []bool{false, true})
	if len(nm) != 2 {
		t.Errorf("capitalised key must not merge as a fragment, got %v", nm)
	}

	got := mergeContinuationRows(in, []bool{false, true, false, true})
	want := [][]string{
		{"PM", "Project manager; read access to entries of their project."},
		{"SUP", "Edits in locked periods."},
	}
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		for c := range want[i] {
			if got[i][c] != want[i][c] {
				t.Errorf("row %d col %d = %q, want %q", i, c, got[i][c], want[i][c])
			}
		}
	}
}

func TestRunningDecorations(t *testing.T) {
	logo := rawImage{bytes: []byte("logo-bytes"), w: 37, h: 52}
	fig := rawImage{bytes: []byte("figure-bytes"), w: 400, h: 300}
	mkPage := func(num int, pl ...imgPlace) page { return page{num: num, places: pl} }
	pages := []page{
		mkPage(1, imgPlace{name: "L", topY: 800}),
		mkPage(2, imgPlace{name: "L", topY: 800}),
		mkPage(3, imgPlace{name: "L", topY: 800}, imgPlace{name: "F", topY: 400}),
		mkPage(4, imgPlace{name: "L", topY: 800}),
		mkPage(5, imgPlace{name: "L", topY: 800}),
		mkPage(6),
	}
	imgs := map[int]map[string]rawImage{
		1: {"L": logo}, 2: {"L": logo}, 3: {"L": logo, "F": fig}, 4: {"L": logo}, 5: {"L": logo},
	}
	running := runningDecorations(pages, imgs, 6)
	if !running[decoSignature(logo, 800)] {
		t.Error("a same-size logo at the same place on 5/6 pages must be flagged running")
	}
	if running[decoSignature(fig, 400)] {
		t.Error("a unique figure must not be flagged running")
	}
}

func TestDecodeDimsAndSignature(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 37, 52))); err != nil {
		t.Fatal(err)
	}
	if w, h := decodeDims(buf.Bytes()); w != 37 || h != 52 {
		t.Errorf("decodeDims = %dx%d, want 37x52", w, h)
	}
	if w, h := decodeDims([]byte("not an image")); w != 0 || h != 0 {
		t.Errorf("decodeDims of junk = %dx%d, want 0x0", w, h)
	}
	logo := rawImage{w: 37, h: 52}
	if decoSignature(logo, 800) != decoSignature(logo, 805) {
		t.Error("near positions should share a signature")
	}
	if decoSignature(logo, 800) == decoSignature(logo, 900) {
		t.Error("far positions should differ")
	}
	if decoSignature(logo, 800) == decoSignature(rawImage{w: 40, h: 52}, 800) {
		t.Error("different sizes should differ")
	}
	if got := decoSignature(rawImage{bytes: []byte("x")}, 800); !strings.HasPrefix(got, "b:") {
		t.Errorf("an undecodable image should use the byte fallback, got %q", got)
	}
}

func TestNormExtAndMarker(t *testing.T) {
	for in, want := range map[string]string{"jpeg": "jpg", "jpg": "jpg", "png": "png", "": "png", "tiff": "tiff"} {
		if got := normExt(in); got != want {
			t.Errorf("normExt(%q)=%q want %q", in, got, want)
		}
	}
	if !isImageMarker("[Image: 3]") || isImageMarker("see [Image: 3] above") || isImageMarker("Image 3") {
		t.Errorf("isImageMarker matched incorrectly")
	}
}

func TestIsTOCRun(t *testing.T) {
	row := func(cs ...string) []cell {
		r := make([]cell, len(cs))
		for i, s := range cs {
			r[i] = cell{startX: float64(i * 100), text: s}
		}
		return r
	}
	toc := [][]cell{
		row("1", "SAMPLE OVERVIEW", "4"),
		row("2", "CREATING A RECORD", "7"),
		row("2.1", "Creation step (Appeal)", "8"),
		row("2.2", "Creation step (Decision)", "9"),
		row("3", "PROCESSING", "13"),
	}
	if !isTOCRun(toc) {
		t.Error("a numbered, page-terminated, nested list should be detected as a TOC")
	}
	data := [][]cell{
		row("1", "Screws", "50"),
		row("2", "Nuts", "30"),
		row("3", "Washers", "12"),
		row("4", "Dowels", "8"),
	}
	if isTOCRun(data) {
		t.Error("a flat numbered data table must not be treated as a TOC")
	}
	kv := [][]cell{row("EMP", "Employee"), row("MGR", "Manager"), row("PM", "Project manager"), row("SUP", "Support")}
	if isTOCRun(kv) {
		t.Error("a key/value table must not be treated as a TOC")
	}
	labelled := [][]cell{
		row("Art. 1 [Example title - Related topic]", "15"),
		row("Art. 2 [Example subject]", "15"),
		row("Art. 3 [Another subject]", "15"),
		row("Art. 12a [Third subject]", "19"),
		row("§ 5 Definitions", "22"),
		row("Appendix B Transitional rules", "31"),
	}
	if !isTOCRun(labelled) {
		t.Error("a labelled, page-terminated contents list should be detected as a TOC")
	}
	titledData := [][]cell{
		row("Art. 1", "Scope", "active"),
		row("Art. 2", "Terms", "active"),
		row("Art. 3", "Principles", "draft"),
		row("Art. 4", "Duties", "active"),
		row("Art. 5", "Rights", "active"),
		row("Art. 6", "Penalties", "planned"),
	}
	if isTOCRun(titledData) {
		t.Error("a titled data table that does not end in page numbers must not be treated as a TOC")
	}
}

func TestMergeAdjacentTables(t *testing.T) {
	t1 := "| Term | Meaning |\n|---|---|\n| EMP | Employee |"
	t2 := "| Term | Meaning |\n|---|---|\n| MGR | Manager |"
	t3 := "| Feld | Typ |\n|---|---|\n| x | int |"

	got := mergeAdjacentTables([]block{{kind: kindTable, text: t1}, {kind: kindTable, text: t2}})
	if len(got) != 1 {
		t.Fatalf("expected 1 merged table, got %d", len(got))
	}
	if want := t1 + "\n| MGR | Manager |"; got[0].text != want {
		t.Errorf("merged text = %q, want %q", got[0].text, want)
	}
	sep := mergeAdjacentTables([]block{{kind: kindTable, text: t1}, {kind: kindParagraph, text: "intro"}, {kind: kindTable, text: t2}})
	if len(sep) != 3 {
		t.Errorf("paragraph between tables must prevent merge, got %d blocks", len(sep))
	}
	diff := mergeAdjacentTables([]block{{kind: kindTable, text: t1}, {kind: kindTable, text: t3}})
	if len(diff) != 2 {
		t.Errorf("different-header tables must not merge, got %d blocks", len(diff))
	}
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// blocksMarkdown is renderWithBreaks without the page breaks.
func blocksMarkdown(blocks []block) string {
	md, _ := renderWithBreaks(blocks)
	return md
}

// TestGroupBlocks_ParagraphSpacing covers word-processor paragraph spacing: a gap of
// well under one line on top of the usual pitch, here 17 pt lines with 25 pt between
// paragraphs, as in a Word document at 12 pt with 8 pt after each paragraph.
func TestGroupBlocks_ParagraphSpacing(t *testing.T) {
	var lines []line
	y := 700.0
	for p := 1; p <= 3; p++ {
		for k, s := range []string{fmt.Sprintf("Paragraph %d of the narrow column.", p), "It wraps over several lines in", "a narrow column of the page."} {
			if k == 0 && p > 1 {
				y -= 25
			} else if k > 0 {
				y -= 17
			}
			lines = append(lines, line{text: s, y: y, xStart: 72, fontSize: 12})
		}
	}
	got := blocksMarkdown(groupBlocks([]page{{lines: lines}}, 12, false))
	want := "Paragraph 1 of the narrow column. It wraps over several lines in a narrow column of the page.\n\n" +
		"Paragraph 2 of the narrow column. It wraps over several lines in a narrow column of the page.\n\n" +
		"Paragraph 3 of the narrow column. It wraps over several lines in a narrow column of the page.\n"
	if got != want {
		t.Errorf("paragraphs not split at the spacing:\n got: %q\nwant: %q", got, want)
	}

	t.Run("a slightly taller line stays in its paragraph", func(t *testing.T) {
		lines := []line{
			{text: "The formula a² + b² = c² raises", y: 700, xStart: 72, fontSize: 12},
			{text: "this line a little above the", y: 680, xStart: 72, fontSize: 12},
			{text: "usual pitch of the text.", y: 663, xStart: 72, fontSize: 12},
			{text: "The next lines are set at the", y: 646, xStart: 72, fontSize: 12},
			{text: "usual pitch again.", y: 629, xStart: 72, fontSize: 12},
		}
		got := blocksMarkdown(groupBlocks([]page{{lines: lines}}, 12, false))
		if strings.Count(got, "\n\n") != 0 {
			t.Errorf("a 20 pt gap against a 17 pt pitch is no paragraph break:\n%s", got)
		}
	})
}
