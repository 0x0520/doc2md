// Package pdfnative converts PDF files to Markdown in pure Go. It interprets content
// streams itself, rebuilds lines, tables and columns from glyph positions, removes
// covers, contents pages and running headers/footers, and infers headings from
// numbering and typography.
package pdfnative

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
)

// PageBreak marks where the text of source page Page (1-based) starts in the Markdown;
// Offset is a byte offset.
type PageBreak struct {
	Offset int
	Page   int
}

// Extract renders the PDF to Markdown and discards images and page breaks.
func Extract(data []byte) (string, error) {
	md, _, err := Render(data)
	return md, err
}

// Render renders the PDF to Markdown and returns the raster images that the
// "[Image: N]" marker lines in the text refer to.
func Render(data []byte) (string, []Image, error) {
	md, images, _, err := RenderWithPages(data)
	return md, images, err
}

// RenderWithPages is Render plus the position of each source page in the Markdown.
func RenderWithPages(data []byte) (string, []Image, []PageBreak, error) {
	return renderPDF(data, false)
}

// RenderVerbatim is RenderWithPages without the noise removal: cover page, contents
// pages, running headers/footers and repeated decorative images are kept.
func RenderVerbatim(data []byte) (string, []Image, []PageBreak, error) {
	return renderPDF(data, true)
}

// ErrEncrypted is returned for a PDF that cannot be opened without a user password.
var ErrEncrypted = errors.New("pdf is encrypted with a password")

// renderPDF retries once on a copy rewritten by pdfcpu when the file cannot be read as is.
func renderPDF(data []byte, verbatim bool) (string, []Image, []PageBreak, error) {
	md, images, breaks, err := renderPDFOnce(data, verbatim)
	if err == nil {
		return md, images, breaks, nil
	}
	repaired, rerr := rewritePDF(data)
	if rerr != nil {
		// The reader's own error for an encrypted file dumps the binary /Encrypt dictionary.
		if errors.Is(rerr, ErrEncrypted) {
			return "", nil, nil, rerr
		}
		return "", nil, nil, err
	}
	md, images, breaks, err2 := renderPDFOnce(repaired, verbatim)
	if err2 != nil {
		return "", nil, nil, err
	}
	return md, images, breaks, nil
}

// rewritePDF repairs or decrypts a file with pdfcpu (relaxed validation): Decrypt first,
// then Optimize, which rebuilds the cross-reference data. A file that needs a user
// password yields ErrEncrypted.
func rewritePDF(data []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("rewrite pdf: %v", r)
		}
	}()
	var dec bytes.Buffer
	derr := api.Decrypt(context.Background(), bytes.NewReader(data), &dec, pdfcpuConfig())
	if derr == nil {
		return dec.Bytes(), nil
	}
	if errors.Is(derr, pdfcpu.ErrWrongPassword) {
		return nil, ErrEncrypted
	}
	var buf bytes.Buffer
	if err := api.Optimize(context.Background(), bytes.NewReader(data), &buf, pdfcpuConfig(), nil); err != nil {
		return nil, fmt.Errorf("rewrite pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// pageTokens extracts a page's glyph runs; ok is false if the page's content stream is unreadable.
func pageTokens(p pdf.Page) (toks []pdf.Text, places []imgPlace, height float64, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	toks, places = extractTokens(p)
	return toks, places, pageHeight(p), true
}

// renderPDFOnce converts panics from the PDF reader on malformed input into errors.
func renderPDFOnce(data []byte, verbatim bool) (md string, images []Image, breaks []PageBreak, err error) {
	defer func() {
		if r := recover(); r != nil {
			md, images, breaks, err = "", nil, nil, fmt.Errorf("read pdf: %v", r)
		}
	}()
	return renderPDFBody(data, verbatim)
}

func renderPDFBody(data []byte, verbatim bool) (string, []Image, []PageBreak, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, nil, fmt.Errorf("open pdf: %w", err)
	}

	var pages []page
	for i, p := range documentPages(r) {
		toks, places, height, ok := pageTokens(p)
		if !ok {
			continue
		}
		pages = append(pages, page{
			rows:   pageRows(toks),
			height: height,
			num:    i + 1,
			links:  pageLinks(p),
			places: places,
		})
	}
	if len(pages) == 0 {
		return "", nil, nil, fmt.Errorf("no pages with content")
	}

	if !verbatim {
		pages = dropCoverPage(pages)
		pages = dropTOCPages(pages)
	}
	// Before the running feet go: a numeric note label alone at the foot reads as a page number.
	notes := extractNotes(pages)
	if !verbatim {
		dropRunningHeadFoot(pages)
	}
	for pi := range pages {
		// Columns are reordered before tables are assembled so the gutter is not read as a cell gap.
		pages[pi].rows = splitColumnRows(orderNearBaselines(pages[pi].rows))
		pages[pi].lines = assembleRows(pages[pi].rows, verbatim, pages[pi].links)
		for li := range pages[pi].lines {
			pages[pi].lines[li].page = pages[pi].num
		}
	}
	images := placeImages(pages, extractRasterImages(data), r.NumPage(), verbatim)

	body := bodyFontSize(pages)
	blocks := groupBlocks(pages, body, verbatim)
	// Appended after grouping so that note text never takes part in heading inference.
	for _, n := range notes {
		blocks = append(blocks, block{kind: kindParagraph, text: n})
	}

	md, breaks := renderWithBreaks(blocks)
	if strings.TrimSpace(md) == "" {
		return "", nil, nil, fmt.Errorf("no text could be extracted")
	}
	return md, images, breaks, nil
}

// documentPages lists the pages in order with one walk of the page tree;
// Reader.Page walks it from the root for every page, which is quadratic in the page
// count. Like Reader.NumPage it trusts the root's /Count.
func documentPages(r *pdf.Reader) []pdf.Page {
	n := r.NumPage()
	var out []pdf.Page
	// seen guards against a cyclic tree. A node's printed form lists its own entries,
	// with /Parent and /Kids as object references, so it is unique per node.
	seen := map[string]bool{}
	var walk func(node pdf.Value)
	walk = func(node pdf.Value) {
		key := node.String()
		if seen[key] {
			return
		}
		seen[key] = true
		kids := node.Key("Kids")
		for i := 0; i < kids.Len() && len(out) < n; i++ {
			kid := kids.Index(i)
			switch kid.Key("Type").Name() {
			case "Pages":
				walk(kid)
			case "Page":
				out = append(out, pdf.Page{V: kid})
			}
		}
	}
	if root := r.Trailer().Key("Root").Key("Pages"); root.Key("Type").Name() == "Pages" {
		walk(root)
	}
	return out
}

// pageHeight returns the MediaBox height, or 0 if the page has none.
func pageHeight(p pdf.Page) float64 {
	mb := inherited(p.V, "MediaBox")
	if mb.Kind() != pdf.Array || mb.Len() < 4 {
		return 0
	}
	return mb.Index(3).Float64() - mb.Index(1).Float64()
}

// inherited looks a key up the page tree, since attributes like MediaBox may sit on an ancestor (PDF 32000-1 §7.7.3.4).
func inherited(v pdf.Value, key string) pdf.Value {
	for ; !v.IsNull(); v = v.Key("Parent") {
		if r := v.Key(key); !r.IsNull() {
			return r
		}
	}
	return pdf.Value{}
}
