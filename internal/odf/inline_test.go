package odf

import "testing"

// Character styles as Word and LibreOffice write them.
const textStyles = `` +
	`<style:style style:name="B" style:family="text"><style:text-properties fo:font-weight="bold"/></style:style>` +
	`<style:style style:name="I" style:family="text"><style:text-properties fo:font-style="italic"/></style:style>` +
	`<style:style style:name="S" style:family="text"><style:text-properties style:text-line-through-style="solid"/></style:style>` +
	`<style:style style:name="BIS" style:family="text"><style:text-properties fo:font-weight="bold" fo:font-style="italic" style:text-line-through-style="solid"/></style:style>` +
	`<style:style style:name="W7" style:family="text"><style:text-properties fo:font-weight="700"/></style:style>` +
	`<style:style style:name="Normal" style:family="text"><style:text-properties fo:font-weight="normal" fo:font-style="normal" style:text-line-through-style="none"/></style:style>` +
	`<style:style style:name="Under" style:family="text"><style:text-properties style:text-underline-style="solid"/></style:style>` +
	`<style:style style:name="Child" style:family="text" style:parent-style-name="B"/>` +
	`<style:style style:name="Grand" style:family="text" style:parent-style-name="Child"><style:text-properties fo:font-style="italic"/></style:style>` +
	`<style:style style:name="Loop1" style:family="text" style:parent-style-name="Loop2"/>` +
	`<style:style style:name="Loop2" style:family="text" style:parent-style-name="Loop1"/>`

func para(t *testing.T, inner string) string {
	t.Helper()
	got, _ := convert(t, textType, textStyles, `<office:text><text:p>`+inner+`</text:p></office:text>`)
	return got
}

func TestSpanEmphasis(t *testing.T) {
	tests := []struct{ name, inner, want string }{
		{"bold italic strike", `A <text:span text:style-name="B">bold</text:span>, <text:span text:style-name="I">italic</text:span> and <text:span text:style-name="S">gone</text:span>.`, "A **bold**, _italic_ and ~~gone~~."},
		{"nesting order", `<text:span text:style-name="BIS">all</text:span>`, "**_~~all~~_**"},
		{"numeric weight", `<text:span text:style-name="W7">heavy</text:span>`, "**heavy**"},
		{"underline is not emphasis", `<text:span text:style-name="Under">u</text:span>`, "u"},
		{"parent chain", `<text:span text:style-name="Child">c</text:span> <text:span text:style-name="Grand">g</text:span>`, "**c** **_g_**"},
		{"cycle does not hang", `<text:span text:style-name="Loop1">x</text:span>`, "x"},
		{"unknown style", `<text:span text:style-name="Nope">x</text:span>`, "x"},
		{"nested span adds", `<text:span text:style-name="B">a <text:span text:style-name="I">b</text:span> c</text:span>`, "**a** **_b_** **c**"},
		{"nested span clears", `<text:span text:style-name="B">a <text:span text:style-name="Normal">b</text:span> c</text:span>`, "**a** b **c**"},
		{"adjacent spans merge", `<text:span text:style-name="B">one</text:span><text:span text:style-name="B"> two</text:span><text:span text:style-name="B"> three</text:span>`, "**one two three**"},
		{"edge whitespace outside", `x<text:span text:style-name="B"> bold </text:span>y`, "x **bold** y"},
		{"whitespace between spans", `<text:span text:style-name="B">one</text:span><text:span> </text:span><text:span text:style-name="B">two</text:span>`, "**one two**"},
		{"text:s inside a span", `<text:span text:style-name="B">one<text:s/>two</text:span>`, "**one two**"},
		{"whitespace-only span", `a<text:span text:style-name="B"> </text:span>b`, "a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := para(t, tc.inner); got != tc.want+"\n" {
				t.Errorf("got %q, want %q", got, tc.want+"\n")
			}
		})
	}
}

// Named styles live in styles.xml.
func TestSpanStyleFromStylesXML(t *testing.T) {
	styles := entry{"styles.xml", `<office:document-styles ` + nsDecl + `><office:styles>` +
		`<style:style style:name="Strong_20_Emphasis" style:family="text"><style:text-properties fo:font-weight="bold"/></style:style>` +
		`</office:styles></office:document-styles>`}
	body := `<office:text><text:p>a <text:span text:style-name="Strong_20_Emphasis">b</text:span></text:p></office:text>`
	if got, _ := convert(t, textType, "", body, styles); got != "a **b**\n" {
		t.Errorf("got %q", got)
	}
}

// A paragraph style's own formatting is left alone: only spans emphasise, as in the DOCX reader.
func TestEmphasisStaysOutOfHeadings(t *testing.T) {
	auto := textStyles +
		`<style:style style:name="Titel" style:display-name="Title" style:family="paragraph"><style:text-properties fo:font-weight="bold"/></style:style>` +
		`<style:style style:name="BoldPara" style:family="paragraph"><style:text-properties fo:font-weight="bold"/></style:style>`
	body := `<office:text>` +
		`<text:h text:outline-level="1"><text:span text:style-name="B">Bold</text:span> heading</text:h>` +
		`<text:p text:style-name="Titel"><text:span text:style-name="I">Doc</text:span> title</text:p>` +
		`<text:p text:style-name="BoldPara">Plain <text:span text:style-name="B">body</text:span></text:p>` +
		`</office:text>`
	got, _ := convert(t, textType, auto, body)
	want := "## Bold heading\n\n# Doc title\n\nPlain **body**\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSlideTitleHasNoEmphasis(t *testing.T) {
	body := `<office:presentation><draw:page><draw:frame presentation:class="title"><draw:text-box>` +
		`<text:p><text:span text:style-name="B">Title</text:span></text:p></draw:text-box></draw:frame>` +
		`<draw:frame><draw:text-box><text:p><text:span text:style-name="B">Body</text:span></text:p></draw:text-box></draw:frame>` +
		`</draw:page></office:presentation>`
	got, _ := convert(t, slideType, textStyles, body)
	if want := "# Title\n\n**Body**\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEmphasisInListAndTable(t *testing.T) {
	body := `<office:text>` +
		`<text:list><text:list-item><text:p>An <text:span text:style-name="B">item</text:span></text:p></text:list-item></text:list>` +
		`<table:table><table:table-row><table:table-cell><text:p><text:span text:style-name="B">Head</text:span></text:p></table:table-cell>` +
		`<table:table-cell><text:p>Other</text:p></table:table-cell></table:table-row>` +
		`<table:table-row><table:table-cell><text:p><text:span text:style-name="I">one</text:span></text:p></table:table-cell>` +
		`<table:table-cell><text:p>two</text:p></table:table-cell></table:table-row></table:table>` +
		`</office:text>`
	got, _ := convert(t, textType, textStyles, body)
	want := "- An **item**\n\n| **Head** | Other |\n|---|---|\n| _one_ | two |\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHyperlink(t *testing.T) {
	got := para(t, `See <text:a xlink:type="simple" xlink:href="https://example.org/spec">the spec</text:a> now.`)
	if want := "See [the spec](https://example.org/spec) now.\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHyperlinkKeepsEmphasisInsideBrackets(t *testing.T) {
	got := para(t, `<text:a xlink:href="https://example.org/"><text:span text:style-name="B">bold</text:span> and <text:span text:style-name="I">italic</text:span> </text:a>tail`)
	if want := "[**bold** and _italic_](https://example.org/) tail\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEmphasisAroundLink(t *testing.T) {
	got := para(t, `<text:span text:style-name="B">see <text:a xlink:href="https://example.org/">link</text:a> now</text:span>`)
	if want := "**see** [**link**](https://example.org/) **now**\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHyperlinkInHeadingHasNoEmphasis(t *testing.T) {
	got, _ := convert(t, textType, textStyles, `<office:text><text:h text:outline-level="1">`+
		`<text:a xlink:href="https://example.org/"><text:span text:style-name="B">Title</text:span></text:a></text:h></office:text>`)
	if want := "# [Title](https://example.org/)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnusableHyperlinkTargetsAreText(t *testing.T) {
	for _, href := range []string{
		"javascript:alert(1)", "JavaScript:alert(1)", " vbscript:x", "DATA:text/html,x", "java&#9;script:x",
		"", "#Heading|outline",
	} {
		got := para(t, `a <text:a xlink:href="`+href+`">click</text:a> b`)
		if want := "a click b\n"; got != want {
			t.Errorf("%q: got %q, want %q", href, got, want)
		}
	}
}

func TestHyperlinkDoesNotLeak(t *testing.T) {
	got := para(t, `<text:a xlink:href="https://a.example/">one</text:a> two <text:a xlink:href="mailto:x@example.org">three</text:a>`)
	if want := "[one](https://a.example/) two [three](mailto:x@example.org)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
