// Package epub converts an EPUB book to Markdown. Chapters are read in the
// package's spine order (zip order is unspecified) and rendered by the htmldoc
// package, since EPUB content documents are XHTML.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/htmldoc"
	"github.com/0x0520/doc2md/internal/zipx"
)

// ErrNotEpub is returned when the input is not a readable EPUB: bad zip, no
// container.xml or package document, or an empty spine.
var ErrNotEpub = errors.New("not a readable EPUB book")

// maxDocuments caps the number of spine entries rendered.
const maxDocuments = 5000

// container is META-INF/container.xml, which only names the package document.
type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

// pkg is the package document (OPF): title, manifest and spine.
type pkg struct {
	Title    string `xml:"metadata>title"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		Properties string `xml:"properties,attr"`
		MediaType  string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef  string `xml:"idref,attr"`
		Linear string `xml:"linear,attr"`
	} `xml:"spine>itemref"`
}

// navPoint is one entry of an EPUB 2 toc.ncx; nesting is the hierarchy.
type navPoint struct {
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []navPoint `xml:"navPoint"`
}

type ncx struct {
	Points []navPoint `xml:"navMap>navPoint"`
}

// ToMarkdown converts EPUB bytes to Markdown in spine order and returns the
// images found in the package.
func ToMarkdown(data []byte) (string, []docimg.Image, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotEpub, err)
	}
	opfPath, err := packagePath(zr)
	if err != nil {
		return "", nil, err
	}
	f := zipx.Find(zr, opfPath)
	if f == nil {
		return "", nil, fmt.Errorf("%w: package document %q is missing", ErrNotEpub, opfPath)
	}
	raw, err := zipx.ReadEntry(f)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotEpub, err)
	}
	var p pkg
	if err := xml.Unmarshal(raw, &p); err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotEpub, err)
	}

	// Manifest hrefs are relative to the package document, not the archive root.
	base := path.Dir(opfPath)
	href := make(map[string]string, len(p.Manifest))
	for _, it := range p.Manifest {
		if it.ID != "" && it.Href != "" {
			href[it.ID] = resolve(base, it.Href)
		}
	}

	// Chapters do not state how they nest; the navigation document does. Without
	// one, every chapter stays at top level.
	depth := navDepths(zr, base, p)

	// One sink for the whole book, so image numbers run across chapters.
	sink := &packageSink{zr: zr}

	var parts []string
	var total int64
	for _, ref := range p.Spine {
		if len(parts) >= maxDocuments || total >= zipx.MaxTotalSize {
			break
		}
		// linear="no" items (covers, ad pages) are kept; only their position is not guaranteed.
		name := href[ref.IDRef]
		if name == "" {
			continue
		}
		ef := zipx.Find(zr, name)
		if ef == nil {
			continue
		}
		b, rerr := zipx.ReadEntry(ef)
		if rerr != nil {
			continue
		}
		total += int64(len(b))
		// Image paths in a chapter are relative to the chapter's own directory.
		sink.base = path.Dir(name)
		md, cerr := htmldoc.ToMarkdownWith(b, sink)
		if cerr != nil || strings.TrimSpace(md) == "" {
			continue
		}
		parts = append(parts, shiftHeadings(strings.TrimSpace(md), depth[name]-1))
	}
	if len(parts) == 0 {
		return "", nil, ErrNotEpub
	}
	// The book title lives only in the metadata; add it unless the first chapter
	// already opens with it (title pages).
	if t := strings.TrimSpace(p.Title); t != "" && !opensWith(parts[0], t) {
		parts = append([]string{"# " + t}, parts...)
	}
	return strings.Join(parts, "\n\n") + "\n", sink.images, nil
}

// opensWith reports whether the chapter's first line is a heading equal to title.
func opensWith(chapter, title string) bool {
	for line := range strings.SplitSeq(chapter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			return false
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		return strings.EqualFold(collapse(heading), collapse(title))
	}
	return false
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// packagePath returns the package document's path from container.xml.
func packagePath(zr *zip.Reader) (string, error) {
	f := zipx.Find(zr, "META-INF/container.xml")
	if f == nil {
		return "", fmt.Errorf("%w: no META-INF/container.xml", ErrNotEpub)
	}
	b, err := zipx.ReadEntry(f)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotEpub, err)
	}
	var c container
	if err := xml.Unmarshal(b, &c); err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotEpub, err)
	}
	for _, rf := range c.Rootfiles {
		if rf.FullPath != "" {
			return strings.TrimPrefix(path.Clean(rf.FullPath), "/"), nil
		}
	}
	return "", fmt.Errorf("%w: container.xml names no package document", ErrNotEpub)
}

// resolve turns a manifest href into a zip entry name: fragment and query are
// removed and no leading slash is kept.
func resolve(base, href string) string {
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	if base == "." || base == "/" {
		return strings.TrimPrefix(path.Clean(href), "/")
	}
	return strings.TrimPrefix(path.Clean(path.Join(base, href)), "/")
}

// maxImageBytes caps the total image payload of one book; single entries are bounded by zipx.
const maxImageBytes = 128 << 20

// packageSink resolves chapter <img> sources to image entries in the archive.
// base is set per chapter; one sink serves the whole book so numbering is continuous.
type packageSink struct {
	zr     *zip.Reader
	base   string
	seq    int
	total  int
	images []docimg.Image
}

// Marker implements htmldoc.ImageSink. Anything it cannot resolve (web URLs,
// missing entries, unknown formats) yields "" so the alt text is used instead.
func (s *packageSink) Marker(src string) string {
	if src == "" || s.total >= maxImageBytes {
		return ""
	}
	// Any scheme (http:, data:) points outside the package.
	if i := strings.IndexByte(src, ':'); i > 0 && !strings.HasPrefix(src, "//") {
		return ""
	}
	name := htmldoc.CleanSrc(src)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "/") {
		name = strings.TrimPrefix(name, "/")
	} else {
		name = path.Clean(path.Join(s.base, name))
	}
	ext := docimg.Ext(path.Ext(name))
	if ext == "" {
		return ""
	}
	f := zipx.Find(s.zr, name)
	if f == nil {
		return ""
	}
	b, err := zipx.ReadEntry(f)
	if err != nil || len(b) == 0 {
		return ""
	}
	s.total += len(b)
	s.seq++
	s.images = append(s.images, docimg.Image{Seq: s.seq, Bytes: b, Ext: ext})
	return docimg.MarkerFor(s.seq)
}
