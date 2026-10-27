package epub

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func book(t *testing.T, parts ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p[0])
		if err != nil {
			t.Fatalf("create %s: %v", p[0], err)
		}
		if _, err := w.Write([]byte(p[1])); err != nil {
			t.Fatalf("write %s: %v", p[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/book.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

func opf(spine string) string {
	return `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Sample Book</dc:title></metadata>
  <manifest>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>` + spine + `</spine>
</package>`
}

func chapter(title, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title + `</title></head>
<body><h1>` + title + `</h1><p>` + body + `</p></body></html>`
}

func TestSpineOrderWins(t *testing.T) {
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/ch2.xhtml", chapter("Second Chapter", "The second text.")},
		[2]string{"OEBPS/ch1.xhtml", chapter("First Chapter", "The first text.")},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/><itemref idref="c2"/>`)},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	first, second := strings.Index(got, "First Chapter"), strings.Index(got, "Second Chapter")
	if first < 0 || second < 0 {
		t.Fatalf("a chapter is missing: %q", got)
	}
	if first > second {
		t.Errorf("chapters came out in archive order, not reading order:\n%s", got)
	}
}

func TestHrefRelativeToPackage(t *testing.T) {
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/>`)},
		[2]string{"OEBPS/ch1.xhtml", chapter("Introduction", "Text.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "# Introduction") {
		t.Errorf("chapter under the package's directory was not found: %q", got)
	}
}

func TestMissingSpineItemIsSkipped(t *testing.T) {
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/><itemref idref="c2"/>`)},
		[2]string{"OEBPS/ch1.xhtml", chapter("Present", "Text.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "# Present") {
		t.Errorf("present chapter lost because a sibling was missing: %q", got)
	}
}

func TestNotABook(t *testing.T) {
	if _, _, err := ToMarkdown([]byte("not a zip")); err == nil {
		t.Fatal("expected an error for non-archive bytes, got nil")
	}
	noContainer := book(t, [2]string{"mimetype", "application/epub+zip"})
	if _, _, err := ToMarkdown(noContainer); err == nil {
		t.Fatal("expected an error for an archive without container.xml, got nil")
	}
}

func TestBookTitleFromMetadata(t *testing.T) {
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/>`)},
		[2]string{"OEBPS/ch1.xhtml", chapter("First Chapter", "Text.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.HasPrefix(got, "# Sample Book") {
		t.Errorf("book title missing or not first:\n%s", got)
	}
}

func TestBookTitleNotRepeatedByTitlePage(t *testing.T) {
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/>`)},
		[2]string{"OEBPS/ch1.xhtml", chapter("Sample Book", "By someone.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if n := strings.Count(got, "# Sample Book"); n != 1 {
		t.Errorf("book title appears %d times, want 1:\n%s", n, got)
	}
}

const navXHTML = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<body><nav epub:type="toc"><ol>
  <li><a href="ch1.xhtml">First Chapter</a>
    <ol><li><a href="ch2.xhtml">Second Chapter</a></li></ol>
  </li>
</ol></nav></body></html>`

func TestNavigationNestsChapters(t *testing.T) {
	opfNav := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Sample Book</dc:title></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opfNav},
		[2]string{"OEBPS/nav.xhtml", navXHTML},
		[2]string{"OEBPS/ch1.xhtml", chapter("First Chapter", "Text.")},
		[2]string{"OEBPS/ch2.xhtml", chapter("Second Chapter", "Text.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "# First Chapter") {
		t.Errorf("top-level chapter not at level 1:\n%s", got)
	}
	if !strings.Contains(got, "## Second Chapter") {
		t.Errorf("nested chapter not pushed a level down:\n%s", got)
	}
}

func TestNavigationNCX(t *testing.T) {
	opfNcx := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Sample Book</dc:title></metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`
	tocNcx := `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>
  <navPoint><navLabel><text>First</text></navLabel><content src="ch1.xhtml"/>
    <navPoint><navLabel><text>Second</text></navLabel><content src="ch2.xhtml"/></navPoint>
  </navPoint>
</navMap></ncx>`
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opfNcx},
		[2]string{"OEBPS/toc.ncx", tocNcx},
		[2]string{"OEBPS/ch1.xhtml", chapter("First Chapter", "Text.")},
		[2]string{"OEBPS/ch2.xhtml", chapter("Second Chapter", "Text.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "# First Chapter") || !strings.Contains(got, "## Second Chapter") {
		t.Errorf("ncx nesting not honoured:\n%s", got)
	}
}

func TestNoNavigationLeavesChaptersFlat(t *testing.T) {
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/><itemref idref="c2"/>`)},
		[2]string{"OEBPS/ch1.xhtml", chapter("First Chapter", "Text.")},
		[2]string{"OEBPS/ch2.xhtml", chapter("Second Chapter", "Text.")},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "# First Chapter") || !strings.Contains(got, "# Second Chapter") {
		t.Errorf("chapters should stay flat without navigation:\n%s", got)
	}
	if strings.Contains(got, "## Second Chapter") {
		t.Errorf("nesting invented without a navigation document:\n%s", got)
	}
}

// A chapter in a legacy encoding declares it in the XML declaration, not in a <meta>.
func TestChapterEncodingFromXMLDeclaration(t *testing.T) {
	ch := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" +
		"<html xmlns=\"http://www.w3.org/1999/xhtml\"><head><title>Gr\xfc\xdfe</title></head>" +
		"<body><h1>Gr\xfc\xdfe</h1><p>Sch\xf6ne Gr\xfc\xdfe.</p></body></html>"
	data := book(t,
		[2]string{"mimetype", "application/epub+zip"},
		[2]string{"META-INF/container.xml", containerXML},
		[2]string{"OEBPS/book.opf", opf(`<itemref idref="c1"/>`)},
		[2]string{"OEBPS/ch1.xhtml", ch},
	)
	got, _, err := ToMarkdown(data)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "Schöne Grüße.") || strings.ContainsRune(got, '�') {
		t.Errorf("chapter was not transcoded from its declared encoding:\n%q", got)
	}
}
