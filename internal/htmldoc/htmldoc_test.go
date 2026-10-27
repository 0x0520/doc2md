package htmldoc

import (
	"strings"
	"testing"
)

func md(t *testing.T, in string) string {
	t.Helper()
	got, _, err := ToMarkdown([]byte(in))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return got
}

func TestToMarkdown_Structure(t *testing.T) {
	got := md(t, `<html><head><title>Specification</title></head><body>
	  <h2>Requirements</h2><p>The request is reviewed.</p>
	  <h3>Deadlines</h3><p>Five days.</p></body></html>`)
	for _, want := range []string{"# Specification", "## Requirements", "### Deadlines", "The request is reviewed."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToMarkdown_DropsScaffolding(t *testing.T) {
	got := md(t, `<html><head><style>.x{color:red}</style></head><body>
	  <script>var tracked = 1;</script>
	  <nav><a href="/a">Home page</a></nav>
	  <p>Only this sentence is content.</p>
	  <noscript>Please enable scripts.</noscript></body></html>`)
	for _, unwanted := range []string{"tracked", "color:red", "Home page", "enable scripts"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("scaffolding survived (%q):\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "Only this sentence is content.") {
		t.Errorf("content lost:\n%s", got)
	}
}

func TestToMarkdown_Table(t *testing.T) {
	got := md(t, `<table><thead><tr><th>Field</th><th>Meaning</th></tr></thead>
	  <tbody><tr><td>CODE</td><td>Item flag</td></tr></tbody></table>`)
	for _, want := range []string{"| Field | Meaning |", "|---|---|", "| CODE | Item flag |"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToMarkdown_Lists(t *testing.T) {
	got := md(t, `<ul><li>first<ul><li>nested</li></ul></li><li>second</li></ul>
	  <ol><li>one</li><li>two</li></ol>`)
	for _, want := range []string{"- first", "  - nested", "- second", "1. one", "1. two"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "- first nested") {
		t.Errorf("nested item duplicated into its parent:\n%s", got)
	}
}

func TestToMarkdown_Inline(t *testing.T) {
	got := md(t, `<p>The <strong>field</strong> is <em>optional</em>, see
	  <a href="https://example.org/spec">Specification</a> and <code>CODE</code>.
	  <span>This part too.</span></p>`)
	for _, want := range []string{"**field**", "_optional_", "[Specification](https://example.org/spec)", "`CODE`", "This part too."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToMarkdown_MalformedStillParses(t *testing.T) {
	got := md(t, `<p>First paragraph<p>Second paragraph<b>bold but never closed`)
	for _, want := range []string{"First paragraph", "Second paragraph", "bold but never closed"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToMarkdown_NoContent(t *testing.T) {
	if _, _, err := ToMarkdown([]byte(`<html><head><script>x()</script></head><body></body></html>`)); err == nil {
		t.Error("a page with no content must error")
	}
}

func TestTitleNotRepeatedByBody(t *testing.T) {
	md, _, err := ToMarkdown([]byte(`<html><head><title>Requirements</title></head>` +
		`<body><h1>Requirements</h1><p>The request is reviewed.</p></body></html>`))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if n := strings.Count(md, "# Requirements"); n != 1 {
		t.Errorf("heading appears %d times, want 1:\n%s", n, md)
	}
}

func TestTitleKeptWhenBodyNamesSomethingElse(t *testing.T) {
	md, _, err := ToMarkdown([]byte(`<html><head><title>Specification</title></head>` +
		`<body><h1>Objectives</h1><p>Text.</p></body></html>`))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(md, "Specification") || !strings.Contains(md, "Objectives") {
		t.Errorf("title or body heading lost:\n%s", md)
	}
}

func TestTitleKeptWhenBodyHasNoHeading(t *testing.T) {
	md, _, err := ToMarkdown([]byte(`<html><head><title>Notice sheet</title></head>` +
		`<body><p>Only running text.</p></body></html>`))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(md, "# Notice sheet") {
		t.Errorf("title lost on a page with no heading:\n%s", md)
	}
}

func TestToMarkdown_LayoutTable(t *testing.T) {
	got := md(t, `<body>
	  <table><tr><td><b>Status:</b></td><td>In review</td></tr></table>
	  <table><tr><td id="content">
	    <p>A <b>converter</b> must be built.</p>
	    <p><b>Basics</b></p>
	    <ul><li>This includes:
	      <ul><li>Item one</li><li>Item two</li></ul></li></ul>
	    <p>Case 1</p>
	    <table><tr><td>Actor</td><td>Staff</td></tr></table>
	  </td></tr></table></body>`)
	for _, want := range []string{
		"| **Status:** | In review |",
		"A **converter** must be built.",
		"- This includes:", "  - Item one", "  - Item two",
		"Case 1", "| Actor | Staff |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "| A ") {
		t.Errorf("the frame became a pipe table:\n%s", got)
	}
}
