package xmldoc

import (
	"strings"
	"testing"
)

func md(t *testing.T, in string) string {
	t.Helper()
	got, err := ToMarkdown([]byte(in))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return got
}

func TestToMarkdown_RecordRunBecomesTable(t *testing.T) {
	in := `<interface name="ITEM1">
	  <field code="ID1"><label>Item flag</label><type>char</type></field>
	  <field code="ID2"><label>Kind</label><type>char</type></field>
	  <field code="ID3"><label>Sector</label><type>num</type></field>
	</interface>`
	got := md(t, in)
	want := strings.Join([]string{
		"# interface",
		"",
		"**name:** ITEM1",
		"",
		"| code | label | type |",
		"|---|---|---|",
		"| ID1 | Item flag | char |",
		"| ID2 | Kind | char |",
		"| ID3 | Sector | num |",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestToMarkdown_RaggedRecords(t *testing.T) {
	in := `<rows><r><a>1</a><b>2</b></r><r><b>3</b></r></rows>`
	got := md(t, in)
	if !strings.Contains(got, "| a | b |") {
		t.Errorf("column union lost: %q", got)
	}
	if !strings.Contains(got, "|  | 3 |") {
		t.Errorf("missing field should leave a blank cell, got: %q", got)
	}
}

func TestToMarkdown_NestingBecomesHeadings(t *testing.T) {
	in := `<spec>
	  <meta><version>1.4</version><author>Jane</author></meta>
	  <section title="Scope"><body>Applies to all items.</body></section>
	</spec>`
	got := md(t, in)
	for _, want := range []string{"# spec", "## meta", "**version:** 1.4", "**author:** Jane", "## section", "**title:** Scope"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToMarkdown_SingleRecordIsNotATable(t *testing.T) {
	got := md(t, `<rows><r><a>1</a><b>2</b></r></rows>`)
	if strings.Contains(got, "|---|") {
		t.Errorf("a single record should not become a table: %q", got)
	}
	for _, want := range []string{"**a:** 1", "**b:** 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestToMarkdown_AttributesOnLeafKept(t *testing.T) {
	got := md(t, `<doc><amount currency="EUR">1200</amount></doc>`)
	if !strings.Contains(got, "1200") || !strings.Contains(got, "currency=EUR") {
		t.Errorf("leaf lost text or attribute: %q", got)
	}
}

func TestToMarkdown_Whitespace(t *testing.T) {
	got := md(t, "<doc><note>Line one\n     and continuation</note></doc>")
	if !strings.Contains(got, "**note:** Line one and continuation") {
		t.Errorf("whitespace not collapsed: %q", got)
	}
}

func TestToMarkdown_MixedContent(t *testing.T) {
	got := md(t, `<doc><p>The <b>value</b> is set on <date>01.03.</date> exactly.</p></doc>`)
	if !strings.Contains(got, "The value is set on 01.03. exactly.") {
		t.Errorf("mixed content was dissected instead of read as prose: %q", got)
	}
	if strings.Contains(got, "**b:**") || strings.Contains(got, "# b") {
		t.Errorf("an inline element became a field or a heading: %q", got)
	}
}

func TestToMarkdown_RecordIsNotProse(t *testing.T) {
	got := md(t, `<field><code>ID1</code><label>Item flag</label></field>`)
	for _, want := range []string{"**code:** ID1", "**label:** Item flag"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestToMarkdown_EmptyElementDropped(t *testing.T) {
	got := md(t, `<doc><a>Content</a><br/><spacer></spacer></doc>`)
	if strings.Contains(got, "br") || strings.Contains(got, "spacer") {
		t.Errorf("an empty element produced output: %q", got)
	}
	if !strings.Contains(got, "Content") {
		t.Errorf("content lost: %q", got)
	}
}

func TestToMarkdown_Errors(t *testing.T) {
	if _, err := ToMarkdown([]byte("not xml at all")); err == nil {
		t.Error("expected an error for input with no root element")
	}
	if _, err := ToMarkdown([]byte("")); err == nil {
		t.Error("expected an error for empty input")
	}
}

func TestToMarkdown_TruncatedRecovers(t *testing.T) {
	got := md(t, `<doc><a>one</a><b>two`)
	if !strings.Contains(got, "one") {
		t.Errorf("content before the truncation was lost: %q", got)
	}
}

func TestToMarkdown_DeepNestingBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("<root><keep>visible</keep>")
	const depth = maxDepth + 50
	for i := 0; i < depth; i++ {
		b.WriteString("<n>")
	}
	b.WriteString("deep")
	for i := 0; i < depth; i++ {
		b.WriteString("</n>")
	}
	b.WriteString("</root>")

	got := md(t, b.String())
	if !strings.Contains(got, "visible") {
		t.Errorf("content beside the deep subtree was lost: %q", got)
	}
	if n := strings.Count(got, "# n"); n > maxDepth {
		t.Errorf("emitted %d nested headings, depth guard did not hold", n)
	}
}
