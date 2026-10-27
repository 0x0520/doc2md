package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
	"github.com/0x0520/doc2md/internal/zipx"
)

var ErrNotPptx = errors.New("not a readable .pptx (OOXML) presentation")

var slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

var slideRelsName = regexp.MustCompile(`^ppt/slides/_rels/slide(\d+)\.xml\.rels$`)

const maxSlideParts = 10_000

func PptxToMarkdown(data []byte) (string, []docimg.Image, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotPptx, err)
	}
	type slideRef struct {
		n int
		f *zip.File
	}
	var refs []slideRef
	for _, f := range zr.File {
		m := slideName.FindStringSubmatch(f.Name)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		refs = append(refs, slideRef{n: n, f: f})
	}
	if len(refs) == 0 {
		return "", nil, ErrNotPptx
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].n < refs[j].n })
	if len(refs) > maxSlideParts {
		refs = refs[:maxSlideParts]
	}

	relsBySlide := map[int]map[string]string{}
	for _, f := range zr.File {
		m := slideRelsName.FindStringSubmatch(f.Name)
		if m == nil {
			continue
		}
		b, rerr := zipx.ReadEntry(f)
		if rerr != nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		relsBySlide[n] = relTargets(b, "ppt/slides")
	}
	med := newMedia(zr)

	var blocks []mdrender.Block
	var total int64
	for _, r := range refs {
		if total >= zipx.MaxTotalSize {
			break
		}
		b, rerr := zipx.ReadEntry(r.f)
		if rerr != nil {
			continue
		}
		total += int64(len(b))
		blocks = append(blocks, parseSlide(b, med, relsBySlide[r.n], r.n)...)
	}
	if len(blocks) == 0 {
		return "", nil, ErrNotPptx
	}
	return mdrender.Document(blocks), med.images, nil
}

func parseSlide(b []byte, med *media, embeds map[string]string, slideNum int) []mdrender.Block {
	dec := xml.NewDecoder(bytes.NewReader(b))
	var blocks []mdrender.Block
	titleShape := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "ph":
				if typ := attr(t, "type"); typ == "title" || typ == "ctrTitle" {
					titleShape = true
				}
			case "pic":
				if id := embedID(dec, "pic"); id != "" && med != nil {
					if mk := med.marker(embeds[id], slideNum); mk != "" {
						blocks = append(blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: mk})
					}
				}
			case "tbl":
				if blk, ok := slideTable(dec); ok {
					blocks = append(blocks, blk)
				}
			case "p":
				if txt := paraText(dec); txt != "" {
					if titleShape {
						blocks = append(blocks, mdrender.Block{Kind: mdrender.Heading, Level: 2, Text: txt})
					} else {
						blocks = append(blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: txt})
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "sp" {
				titleShape = false
			}
		}
	}
	return blocks
}

func paraText(dec *xml.Decoder) string {
	var sb bytes.Buffer
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				sb.WriteString(readText(dec))
			case "br":
				sb.WriteString(" ")
				depth++
			default:
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	return collapseSpaces(sb.String())
}

func slideTable(dec *xml.Decoder) (mdrender.Block, bool) {
	var grid [][]string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tr" {
				grid = append(grid, slideRow(dec))
			} else {
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "tbl" {
				depth = 0
			}
		}
	}
	grid = mdrender.NormalizeGrid(grid)
	if len(grid) == 0 {
		return mdrender.Block{}, false
	}
	return mdrender.Block{Kind: mdrender.Table_, Text: mdrender.Table(grid)}, true
}

func slideRow(dec *xml.Decoder) []string {
	var row []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tc" {
				row = append(row, slideCell(dec))
			} else {
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "tr" {
				depth = 0
			}
		}
	}
	return row
}

func slideCell(dec *xml.Decoder) string {
	var parts []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "p" {
				if txt := paraText(dec); txt != "" {
					parts = append(parts, txt)
				}
			} else {
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	return collapseSpaces(strings.Join(parts, " "))
}
