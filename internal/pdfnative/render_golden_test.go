package pdfnative

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the testdata goldens from the current rendering")

const bodyLead = 14.0

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".md")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("golden %s rewritten:\n%s", path, got)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-golden to create it): %v", path, err)
	}
	if lf(got) != lf(string(want)) {
		t.Errorf("rendering differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func lf(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func renderSynth(t *testing.T, pages ...synthPage) string {
	t.Helper()
	md, err := Extract(buildPDF(t, pages...))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return md
}

func TestGolden_HeadingsBySize(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "Requirements Specification", x: 72, yTop: 120, size: 20, bold: true},
		{text: "Introduction", x: 72, yTop: 170, size: 15, bold: true},
		{text: "Registration of incoming entries has so far been done", x: 72, yTop: 195, size: 10},
		{text: "mostly by phone and on paper.", x: 72, yTop: 195 + bodyLead, size: 10},
		{text: "Background", x: 72, yTop: 250, size: 12, bold: true},
		{text: "Both routes introduce breaks in the flow.", x: 72, yTop: 275, size: 10},
	}})
	assertGolden(t, "headings_by_size", md)
}

func TestGolden_DottedOutlineAtBodySize(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "The request is entered in the tool and checked.", x: 72, yTop: 120, size: 10},
		{text: "3.4 Data migration from the legacy system", x: 72, yTop: 120 + bodyLead, size: 10},
		{text: "The existing records are migrated nightly.", x: 72, yTop: 120 + 2*bodyLead, size: 10},
	}})
	assertGolden(t, "dotted_outline_at_body_size", md)
}

func TestGolden_BareIntegerAtBodySize(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "The request is entered in the tool and checked.", x: 72, yTop: 120, size: 10},
		{text: "4 System architecture", x: 72, yTop: 120 + bodyLead, size: 10},
		{text: "The application runs in three layers.", x: 72, yTop: 120 + 2*bodyLead, size: 10},
	}})
	assertGolden(t, "bare_integer_at_body_size", md)
}

func TestGolden_ChapterSequenceAtBodySize(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "3 Objectives", x: 72, yTop: 110, size: 14, bold: true},
		{text: "The goals of the project are described below.", x: 72, yTop: 150, size: 10},
		{text: "4 System architecture", x: 72, yTop: 150 + bodyLead, size: 10},
		{text: "The application runs in three layers.", x: 72, yTop: 150 + 2*bodyLead, size: 10},
	}})
	assertGolden(t, "chapter_sequence_at_body_size", md)
}

func TestGolden_BareIntegerWithWhitespace(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "The request is entered in the tool and checked.", x: 72, yTop: 120, size: 10},
		{text: "4 System architecture", x: 72, yTop: 120 + 3*bodyLead, size: 10},
		{text: "The application runs in three layers.", x: 72, yTop: 120 + 6*bodyLead, size: 10},
	}})
	assertGolden(t, "bare_integer_with_whitespace", md)
}

func statusTable(yTop float64) []textRun {
	return []textRun{
		{text: "Status", x: 72, yTop: yTop, size: 10, bold: true},
		{text: "Meaning", x: 220, yTop: yTop, size: 10, bold: true},
		{text: "Deadline", x: 400, yTop: yTop, size: 10, bold: true},
		{text: "open", x: 72, yTop: yTop + bodyLead, size: 10},
		{text: "received, unchecked", x: 220, yTop: yTop + bodyLead, size: 10},
		{text: "5 days", x: 400, yTop: yTop + bodyLead, size: 10},
		{text: "checked", x: 72, yTop: yTop + 2*bodyLead, size: 10},
		{text: "approved by experts", x: 220, yTop: yTop + 2*bodyLead, size: 10},
		{text: "2 days", x: 400, yTop: yTop + 2*bodyLead, size: 10},
	}
}

func TestGolden_CaptionUnderHeadedTable(t *testing.T) {
	runs := append([]textRun{
		{text: "Status model", x: 72, yTop: 110, size: 15, bold: true},
	}, statusTable(140)...)
	runs = append(runs,
		textRun{text: "Table 3: Status values of the report", x: 72, yTop: 210, size: 10, bold: true},
		textRun{text: "The deadlines apply from receipt of the complete report.", x: 72, yTop: 240, size: 10},
	)
	assertGolden(t, "caption_under_headed_table", renderSynth(t, synthPage{runs: runs}))
}

func TestGolden_CaptionBetweenProse(t *testing.T) {
	runs := append([]textRun{
		{text: "The status of a report controls further processing.", x: 72, yTop: 110, size: 10},
	}, statusTable(140)...)
	runs = append(runs,
		textRun{text: "Table 3: Status values of the report", x: 72, yTop: 210, size: 10, bold: true},
		textRun{text: "The deadlines apply from receipt of the complete report.", x: 72, yTop: 240, size: 10},
	)
	assertGolden(t, "caption_between_prose", renderSynth(t, synthPage{runs: runs}))
}

func TestGolden_TOCLeaderOnContentPage(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "2 Requirements", x: 72, yTop: 110, size: 14, bold: true},
		{text: "The following tables summarise the participants.", x: 72, yTop: 150, size: 10},
		{text: "Table 1: Client ....... 5 Table 2: User ....... 6", x: 72, yTop: 190, size: 10},
		{text: "The deadlines apply from receipt of the report.", x: 72, yTop: 230, size: 10},
	}})
	assertGolden(t, "toc_leader_on_content_page", md)
}

func TestGolden_StatuteLabel(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "Article 12a", x: 72, yTop: 120, size: 10, bold: true},
		{text: "[Example title and subtitle]", x: 72, yTop: 120 + bodyLead, size: 10},
		{text: "(1) Citizens may be required from a certain age", x: 72, yTop: 120 + 2*bodyLead, size: 10},
		{text: "to take part in the example service.", x: 72, yTop: 120 + 3*bodyLead, size: 10},
	}})
	assertGolden(t, "statute_label", md)
}

func TestGolden_OrderedListNotHeadings(t *testing.T) {
	md := renderSynth(t, synthPage{runs: []textRun{
		{text: "Initial review flow", x: 72, yTop: 110, size: 15, bold: true},
		{text: "1. Receive the request and check completeness.", x: 72, yTop: 140, size: 10},
		{text: "2. Request missing documents from the sender.", x: 72, yTop: 140 + bodyLead, size: 10},
		{text: "3. Hand the case over to the expert review.", x: 72, yTop: 140 + 2*bodyLead, size: 10},
	}})
	assertGolden(t, "ordered_list_not_headings", md)
}

func TestGolden_RunningHeadFoot(t *testing.T) {
	page := func(n string, body ...string) synthPage {
		runs := []textRun{
			{text: "Requirements Specification - Version 1.0", x: 72, yTop: 45, size: 8},
			{text: n, x: 290, yTop: 800, size: 8},
		}
		for i, b := range body {
			runs = append(runs, textRun{text: b, x: 72, yTop: 150 + float64(i)*bodyLead, size: 10})
		}
		return synthPage{runs: runs}
	}
	md := renderSynth(t,
		page("2 / 12", "Entry is done through the existing web tool."),
		page("3 / 12", "Documents are uploaded as attachments and checked."),
		page("4 / 12", "The settlement closes the process."),
	)
	assertGolden(t, "running_head_foot", md)
}

func TestGolden_TOCPageDropped(t *testing.T) {
	entries := []string{
		"1 Introduction ............................. 3",
		"1.1 Background ............................. 3",
		"1.2 Purpose and scope ...................... 4",
		"2 Current situation ....................... 5",
		"3 Objectives .............................. 6",
		"4 Functional requirements ................. 9",
		"4.1 Entry in the tool ...................... 9",
		"4.2 Interruption and resumption ........... 12",
		"5 Exclusions ............................ 14",
		"6 Actors ................................ 15",
	}
	toc := synthPage{runs: []textRun{
		{text: "Table of contents", x: 72, yTop: 110, size: 15, bold: true},
	}}
	for i, e := range entries {
		toc.runs = append(toc.runs, textRun{text: e, x: 72, yTop: 150 + float64(i)*bodyLead, size: 10})
	}
	content := synthPage{runs: []textRun{
		{text: "Introduction", x: 72, yTop: 110, size: 15, bold: true},
		{text: "Intake has so far been done by phone.", x: 72, yTop: 150, size: 10},
	}}
	assertGolden(t, "toc_page_dropped", renderSynth(t, toc, content))
}
