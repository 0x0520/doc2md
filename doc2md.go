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

type Image = docimg.Image

var ErrUnsupportedFormat = errors.New("unsupported document format")

var passthroughExts = map[string]bool{
	".md":       true,
	".markdown": true,
	".txt":      true,
	".text":     true,
}

var converters = map[string]converter{
	".pdf":      withImages(pdfnative.Render),
	".docx":     withImages(ooxml.ToMarkdown),
	".docm":     withImages(ooxml.ToMarkdown),
	".pptx":     withImages(ooxml.PptxToMarkdown),
	".pptm":     withImages(ooxml.PptxToMarkdown),
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

type converter func(filename string, data []byte) (string, []Image, error)

func byBytes(f func([]byte) (string, error)) converter {
	return func(_ string, data []byte) (string, []Image, error) {
		md, err := f(data)
		return md, nil, err
	}
}

func withImages(f func([]byte) (string, []Image, error)) converter {
	return func(_ string, data []byte) (string, []Image, error) { return f(data) }
}

func handled(ext string) bool {
	return passthroughExts[ext] || converters[ext] != nil
}

func Handles(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return !unhandled[ext] && handled(ext)
}

func IsPlainText(filename string) bool {
	return passthroughExts[strings.ToLower(filepath.Ext(filename))]
}

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

var unhandled = map[string]bool{
	".doc": true, ".ppt": true, ".msg": true, ".eml": true,
	".pages": true, ".numbers": true, ".key": true,
}

func sniff(data []byte) string {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(head, []byte{0xEF, 0xBB, 0xBF}), " \t\r\n")
	lower := bytes.ToLower(trimmed)

	switch {
	case bytes.HasPrefix(trimmed, []byte("%PDF-")):
		return ".pdf"
	case bytes.HasPrefix(trimmed, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return ".xls"
	case bytes.HasPrefix(trimmed, []byte("PK\x03\x04")):
		return sniffZip(data)
	case bytes.HasPrefix(trimmed, []byte(`{\rtf`)):
		return ".rtf"
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

func Convert(filename string, data []byte) (markdown string, images []Image, err error) {
	return convert(filename, data, false)
}

func ConvertVerbatim(filename string, data []byte) (markdown string, images []Image, err error) {
	return convert(filename, data, true)
}

func convert(filename string, data []byte, verbatim bool) (markdown string, images []Image, err error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if unhandled[ext] {
		return "", nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, ext)
	}
	if !handled(ext) {
		if sniffed := sniff(data); sniffed != "" {
			ext = sniffed
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

func Render(data []byte) (markdown string, images []Image, err error) {
	return pdfnative.Render(data)
}

type PageBreak = pdfnative.PageBreak

func RenderWithPages(data []byte) (markdown string, images []Image, pages []PageBreak, err error) {
	return pdfnative.RenderWithPages(data)
}

func RenderVerbatim(data []byte) (markdown string, images []Image, pages []PageBreak, err error) {
	return pdfnative.RenderVerbatim(data)
}

func Extract(data []byte) (markdown string, err error) {
	return pdfnative.Extract(data)
}
