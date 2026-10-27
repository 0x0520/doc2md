// Package doc2md converts documents to Markdown in pure Go, with no external
// process. It reads PDF, Word, PowerPoint, spreadsheets, OpenDocument, EPUB, RTF,
// HTML, XML and AsciiDoc, and renders headings, lists and pipe tables the same way
// whichever format the document arrived in. Convert is the entry point; it picks
// the reader from the filename and falls back to the document's leading bytes:
//
//	md, images, err := doc2md.Convert("report.docx", data)
//
// Figures are returned as Images, with an "[Image: N]" marker left in the text.
package doc2md

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0x0520/doc2md/internal/asciidoc"
	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/epub"
	"github.com/0x0520/doc2md/internal/htmldoc"
	"github.com/0x0520/doc2md/internal/odf"
	"github.com/0x0520/doc2md/internal/ooxml"
	"github.com/0x0520/doc2md/internal/pdfnative"
	"github.com/0x0520/doc2md/internal/rtf"
	"github.com/0x0520/doc2md/internal/sheets"
	"github.com/0x0520/doc2md/internal/xmldoc"
	"github.com/0x0520/doc2md/internal/zipx"
)

// Image is one figure extracted from a document. Seq is 1-based and matches the
// "[Image: N]" marker in the Markdown; Page is the page, slide or sheet it sat on
// (0 if the format has none); Ext is the lowercase extension without a dot.
type Image = docimg.Image

// ErrUnsupportedFormat is returned (wrapped) by Convert when neither the name nor
// the bytes identify a format this module reads.
var ErrUnsupportedFormat = errors.New("unsupported document format")

// ErrEncrypted is returned (wrapped) for a PDF that needs a user password to open.
var ErrEncrypted = pdfnative.ErrEncrypted

// Formats that are already text and are returned unchanged. AsciiDoc is not one:
// its markup is converted.
var passthroughExts = map[string]bool{
	".md":       true,
	".markdown": true,
	".txt":      true,
	".text":     true,
}

// The single dispatch table from extension to reader; Handles and Extensions
// derive from it so they cannot drift from Convert.
var converters = map[string]converter{
	".pdf":  withImages(pdfnative.Render),
	".docx": withImages(ooxml.ToMarkdown),
	".docm": withImages(ooxml.ToMarkdown),
	".pptx": withImages(ooxml.PptxToMarkdown),
	".pptm": withImages(ooxml.PptxToMarkdown),
	// The spreadsheet reader takes the filename because it uses the extension to tell
	// a workbook from CSV.
	".xlsx":     sheets.ToMarkdown,
	".xlsm":     sheets.ToMarkdown,
	".xls":      sheets.ToMarkdown,
	".csv":      sheets.ToMarkdown,
	".adoc":     byBytes(asciidoc.ToMarkdown),
	".asciidoc": byBytes(asciidoc.ToMarkdown),
	".html":     withImages(htmldoc.ToMarkdown),
	".htm":      withImages(htmldoc.ToMarkdown),
	".xhtml":    withImages(htmldoc.ToMarkdown),
	".xml":      byBytes(xmldoc.ToMarkdown),
	".rtf":      withImages(rtf.ToMarkdown),
	".epub":     withImages(epub.ToMarkdown),
	".odt":      withImages(odf.ToMarkdown),
	".ods":      withImages(odf.ToMarkdown),
	".odp":      withImages(odf.ToMarkdown),
	".ott":      withImages(odf.ToMarkdown),
	".ots":      withImages(odf.ToMarkdown),
	".otp":      withImages(odf.ToMarkdown),
}

// converter renders one document. The filename is only used by readers that need
// it to tell formats apart.
type converter func(filename string, data []byte) (string, []Image, error)

// byBytes adapts a reader for a format that carries no figures.
func byBytes(f func([]byte) (string, error)) converter {
	return func(_ string, data []byte) (string, []Image, error) {
		md, err := f(data)
		return md, nil, err
	}
}

// withImages adapts a reader that needs only the bytes and returns figures.
func withImages(f func([]byte) (string, []Image, error)) converter {
	return func(_ string, data []byte) (string, []Image, error) { return f(data) }
}

// handled reports whether ext names a format Convert renders or passes through.
func handled(ext string) bool {
	return passthroughExts[ext] || converters[ext] != nil
}

// Handles reports whether Convert recognises the filename's extension. A false
// does not mean failure: Convert still sniffs the bytes of an unknown name.
func Handles(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return !unhandled[ext] && handled(ext)
}

// IsPlainText reports whether Convert returns this file's bytes unchanged because
// the format is already text (.md, .markdown, .txt, .text).
func IsPlainText(filename string) bool {
	return passthroughExts[strings.ToLower(filepath.Ext(filename))]
}

// Extensions returns every extension Convert dispatches on, lowercase with the
// leading dot, sorted.
func Extensions() []string {
	out := make([]string, 0, len(converters)+len(passthroughExts))
	for ext := range converters {
		out = append(out, ext)
	}
	for ext := range passthroughExts {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// Formats that are knowingly unsupported. Naming them gives the caller
// ErrUnsupportedFormat; otherwise sniffing would route a legacy .doc (an OLE
// container like .xls) to the spreadsheet reader.
var unhandled = map[string]bool{
	".doc": true, ".ppt": true, ".msg": true, ".eml": true,
	".pages": true, ".numbers": true, ".key": true,
}

// sniff identifies a format from the leading bytes and returns the extension to
// treat it as, or "" if nothing matches.
func sniff(data []byte) string {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	// The BOM precedes any leading whitespace, so it is stripped first.
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(head, []byte{0xEF, 0xBB, 0xBF}), " \t\r\n")
	lower := bytes.ToLower(trimmed)

	switch {
	case bytes.HasPrefix(trimmed, []byte("%PDF-")):
		return ".pdf"
	// OLE compound file; the spreadsheet reader is the only one that can try it.
	case bytes.HasPrefix(trimmed, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return ".xls"
	case bytes.HasPrefix(trimmed, []byte("PK\x03\x04")):
		return sniffZip(data)
	case bytes.HasPrefix(trimmed, []byte(`{\rtf`)):
		return ".rtf"
	// HTML is tested before XML: a page misread as XML comes out as its tag tree,
	// while XML misread as HTML still yields its text.
	case bytes.HasPrefix(lower, []byte("<!doctype html")), bytes.HasPrefix(lower, []byte("<html")):
		return ".html"
	case bytes.HasPrefix(trimmed, []byte("<?xml")):
		if bytes.Contains(lower, []byte("<html")) || bytes.Contains(lower, []byte("xhtml")) {
			return ".html"
		}
		return ".xml"
	case bytes.HasPrefix(trimmed, []byte("<")):
		return ".xml"
	}
	return ""
}

// sniffZip tells zip-based formats apart by a part each one must contain, and
// returns "" for any other zip.
func sniffZip(data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		switch {
		case f.Name == "mimetype":
			if ext := odfExt(f); ext != "" {
				return ext
			}
		case strings.HasPrefix(f.Name, "word/document"):
			return ".docx"
		case strings.HasPrefix(f.Name, "ppt/presentation"):
			return ".pptx"
		case strings.HasPrefix(f.Name, "xl/workbook"):
			return ".xlsx"
		case f.Name == "META-INF/container.xml":
			return ".epub"
		}
	}
	return ""
}

// odfExt maps the media type in a "mimetype" entry to an OpenDocument or EPUB
// extension.
func odfExt(f *zip.File) string {
	b, err := zipx.ReadEntry(f)
	if err != nil {
		return ""
	}
	mt := strings.TrimSpace(string(b))
	switch {
	case strings.HasPrefix(mt, "application/epub"):
		return ".epub"
	case !strings.HasPrefix(mt, "application/vnd.oasis.opendocument."):
		return ""
	case strings.Contains(mt, "spreadsheet"):
		return ".ods"
	case strings.Contains(mt, "presentation"):
		return ".odp"
	default:
		return ".odt"
	}
}

// Convert renders a document to Markdown, choosing the reader by the filename's
// extension. An unrecognised name is identified from the leading bytes; a known
// but unsupported format yields ErrUnsupportedFormat (wrapped).
func Convert(filename string, data []byte) (markdown string, images []Image, err error) {
	return convert(filename, data, false)
}

// ConvertVerbatim is Convert without dropping anything. Only PDF differs (cover,
// contents, running headers and footers stay, see RenderVerbatim); other formats
// are already transcribed whole, so it behaves exactly like Convert.
func ConvertVerbatim(filename string, data []byte) (markdown string, images []Image, err error) {
	return convert(filename, data, true)
}

func convert(filename string, data []byte, verbatim bool) (markdown string, images []Image, err error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if unhandled[ext] {
		return "", nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, ext)
	}
	if !handled(ext) {
		// Sniff only unrecognised names: a ".txt" holding markup is still a text file.
		if sniffed := sniff(data); sniffed != "" {
			ext = sniffed
			// Readers that dispatch on the name (sheets) need the sniffed extension too.
			filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ext
		}
	}
	if passthroughExts[ext] {
		return string(data), nil, nil
	}
	if verbatim && ext == ".pdf" {
		md, imgs, _, err := pdfnative.RenderVerbatim(data)
		return md, imgs, err
	}
	conv := converters[ext]
	if conv == nil {
		return "", nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, ext)
	}
	return conv(filename, data)
}

// Render converts PDF bytes to Markdown and returns the raster figures it found.
// Image extraction never fails the render; vector-only figures yield no images.
func Render(data []byte) (markdown string, images []Image, err error) {
	return pdfnative.Render(data)
}

// PageBreak marks the byte offset in the rendered Markdown where a new source
// page begins. Only PDFs have pages.
type PageBreak = pdfnative.PageBreak

// RenderWithPages is Render plus the PageBreaks. The Markdown is identical to
// Render's; the breaks are returned alongside it, not inserted into it.
func RenderWithPages(data []byte) (markdown string, images []Image, pages []PageBreak, err error) {
	return pdfnative.RenderWithPages(data)
}

// RenderVerbatim renders a PDF without leaving anything out: cover page, table of
// contents, running headers and footers and repeated decorations all stay.
func RenderVerbatim(data []byte) (markdown string, images []Image, pages []PageBreak, err error) {
	return pdfnative.RenderVerbatim(data)
}

// Extract renders PDF bytes to Markdown text only, for callers that do not need
// the figures.
func Extract(data []byte) (markdown string, err error) {
	return pdfnative.Extract(data)
}
