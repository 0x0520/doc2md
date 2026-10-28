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

	"github.com/nullwerke/doc2md/internal/docimg"
	"github.com/nullwerke/doc2md/internal/mdrender"
	"github.com/nullwerke/doc2md/internal/zipx"
)

// ErrNotPptx is returned (wrapped) when the input is not a readable .pptx.
var ErrNotPptx = errors.New("not a readable .pptx (OOXML) presentation")

// slideName matches slide parts; the number is the part number, not necessarily the
// presentation position.
var slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// slideRelsName matches the relationship parts that resolve a slide's picture ids.
var slideRelsName = regexp.MustCompile(`^ppt/slides/_rels/slide(\d+)\.xml\.rels$`)

// maxSlideParts bounds how many slide parts are read from one archive.
const maxSlideParts = 10_000

// PptxToMarkdown converts a .pptx or .pptm to Markdown: slide titles become
// headings, followed by the slide's text, tables and picture markers.
func PptxToMarkdown(data []byte) (string, []docimg.Image, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotPptx, err)
	}
	slides := map[string]*zip.File{}
	rels := map[string]*zip.File{}
	var pres, presRels *zip.File
	for _, f := range zr.File {
		switch {
		case slideName.MatchString(f.Name):
			slides[f.Name] = f
		case f.Name == "ppt/presentation.xml":
			pres = f
		case f.Name == "ppt/_rels/presentation.xml.rels":
			presRels = f
		default:
			if m := slideRelsName.FindStringSubmatch(f.Name); m != nil {
				rels["ppt/slides/slide"+m[1]+".xml"] = f
			}
		}
	}
	if len(slides) == 0 {
		return "", nil, ErrNotPptx
	}
	order := slideOrder(slides, pres, presRels)
	if len(order) > maxSlideParts {
		order = order[:maxSlideParts]
	}
	med := newMedia(zr)

	var blocks []mdrender.Block
	var total int64
	for pos, name := range order {
		// Cap the cumulative size of slide XML read from one archive.
		if total >= zipx.MaxTotalSize {
			break
		}
		b, rerr := zipx.ReadEntry(slides[name])
		if rerr != nil {
			continue
		}
		total += int64(len(b))
		var embeds map[string]string
		if rf := rels[name]; rf != nil {
			if rb, rerr := zipx.ReadEntry(rf); rerr == nil {
				embeds = relTargets(rb, "ppt/slides")
			}
		}
		blocks = append(blocks, parseSlide(b, med, embeds, pos+1)...)
	}
	if len(blocks) == 0 {
		return "", nil, ErrNotPptx
	}
	return mdrender.Document(blocks), med.images, nil
}

// slideOrder returns the slide part names in presentation order: the order of
// p:sldIdLst in presentation.xml, which is independent of the slideN.xml numbers
// (moving a slide in PowerPoint does not rename its part). Without a usable list
// it falls back to the part number, since zip entry order is arbitrary.
func slideOrder(slides map[string]*zip.File, pres, presRels *zip.File) []string {
	if pres != nil && presRels != nil {
		pb, perr := zipx.ReadEntry(pres)
		rb, rerr := zipx.ReadEntry(presRels)
		if perr == nil && rerr == nil {
			var order []string
			seen := map[string]bool{}
			for _, name := range presentationSlides(pb, rb) {
				if slides[name] != nil && !seen[name] {
					seen[name] = true
					order = append(order, name)
				}
			}
			if len(order) > 0 {
				return order
			}
		}
	}
	order := make([]string, 0, len(slides))
	num := map[string]int{}
	for name := range slides {
		order = append(order, name)
		num[name], _ = strconv.Atoi(slideName.FindStringSubmatch(name)[1])
	}
	sort.Slice(order, func(i, j int) bool { return num[order[i]] < num[order[j]] })
	return order
}

// presentationSlides lists the slide part paths named by p:sldIdLst, in list order.
func presentationSlides(pres, rels []byte) []string {
	targets := relTargets(rels, "ppt")
	dec := xml.NewDecoder(bytes.NewReader(pres))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sldId" {
			continue
		}
		// The relationship id is the namespaced r:id; the plain id attribute is a number.
		for _, a := range se.Attr {
			if a.Name.Local == "id" && a.Name.Space != "" {
				if t := targets[a.Value]; t != "" {
					out = append(out, t)
				}
			}
		}
	}
}

// placeholder says what a shape's p:ph makes of its paragraphs.
type placeholder int

const (
	freeText    placeholder = iota // text box or other placeholder: plain paragraphs
	titleHolder                    // title and ctrTitle: headings
	bodyHolder                     // body and object placeholders: bullets inherited from the layout
)

// bullet is a paragraph's own bullet setting.
type bullet int

const (
	bulletInherit bullet = iota
	bulletNone
	bulletChar
	bulletNumber
)

// parseSlide extracts the blocks of one slide in document order. embeds maps the
// slide's relationship ids to image parts.
func parseSlide(b []byte, med *media, embeds map[string]string, slideNum int) []mdrender.Block {
	dec := xml.NewDecoder(bytes.NewReader(b))
	var blocks []mdrender.Block
	holder := freeText
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "ph":
				// The kind applies to the paragraphs up to the end of the shape.
				switch attr(t, "type") {
				case "title", "ctrTitle":
					holder = titleHolder
				// A ph with only an idx has the default type "obj" and inherits the master's body style.
				case "body", "obj", "":
					holder = bodyHolder
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
				if pi := readPara(dec); pi.text != "" {
					blocks = append(blocks, paraBlock(pi, holder))
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "sp", "graphicFrame":
				holder = freeText
			}
		}
	}
	return blocks
}

// paraBlock turns a paragraph into a heading, list item or plain paragraph. A
// bullet set on the paragraph itself counts in any shape; in a body placeholder
// the bullet comes from the layout unless the paragraph switches it off.
func paraBlock(pi paraInfo, holder placeholder) mdrender.Block {
	if holder == titleHolder {
		return mdrender.Block{Kind: mdrender.Heading, Level: 2, Text: pi.text}
	}
	list := pi.bullet == bulletChar || pi.bullet == bulletNumber ||
		(holder == bodyHolder && pi.bullet == bulletInherit)
	if !list {
		return mdrender.Block{Kind: mdrender.Paragraph, Text: pi.text}
	}
	marker := "- "
	if pi.bullet == bulletNumber {
		marker = "1. "
	}
	return mdrender.Block{Kind: mdrender.List, Text: strings.Repeat("  ", pi.level) + marker + pi.text}
}

// paraInfo is one a:p: its text (line breaks as spaces), outline level and bullet setting.
type paraInfo struct {
	text   string
	level  int
	bullet bullet
}

// readPara reads an a:p whose start tag was consumed.
func readPara(dec *xml.Decoder) paraInfo {
	var sb bytes.Buffer
	var pi paraInfo
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
			case "pPr":
				if n, err := strconv.Atoi(attr(t, "lvl")); err == nil {
					pi.level = clampLevel(n)
				}
				depth++
			case "buNone":
				pi.bullet = bulletNone
				depth++
			case "buChar", "buBlip":
				pi.bullet = bulletChar
				depth++
			case "buAutoNum":
				pi.bullet = bulletNumber
				depth++
			default:
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	pi.text = collapseSpaces(sb.String())
	return pi
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
				if txt := readPara(dec).text; txt != "" {
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
