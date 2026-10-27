package odf

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
	"github.com/0x0520/doc2md/internal/zipx"
)

var ErrNotODF = errors.New("not a readable OpenDocument package")

const (
	maxRepeat = 4096
	maxCells  = 1 << 20
)

func ToMarkdown(data []byte) (string, []docimg.Image, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotODF, err)
	}
	f := zipx.Find(zr, "content.xml")
	if f == nil {
		return "", nil, ErrNotODF
	}
	content, err := zipx.ReadEntry(f)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotODF, err)
	}
	p := &parser{zr: zr}
	blocks := p.walk(content)
	if len(blocks) == 0 {
		return "", nil, ErrNotODF
	}
	return mdrender.Document(blocks), p.images, nil
}

const nsDrawing = "drawing"

func ns(n xml.Name) string {
	if i := strings.LastIndex(n.Space, ":"); i > 0 {
		if j := strings.LastIndex(n.Space[:i], ":"); j >= 0 {
			return n.Space[j+1 : i]
		}
	}
	return n.Space
}

func attr(e xml.StartElement, space, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local && (space == "" || ns(a.Name) == space) {
			return a.Value
		}
	}
	return ""
}

type parser struct {
	blocks     []mdrender.Block
	listDepth  int
	sheets     bool
	titleFrame bool
	zr         *zip.Reader
	page       int
	seq        int
	total      int
	images     []docimg.Image
}

func (p *parser) walk(content []byte) []mdrender.Block {
	dec := xml.NewDecoder(bytes.NewReader(content))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			p.start(dec, t)
		case xml.EndElement:
			switch {
			case ns(t.Name) == "text" && t.Name.Local == "list":
				p.listDepth--
			case ns(t.Name) == nsDrawing && t.Name.Local == "frame":
				p.titleFrame = false
			}
		}
	}
	return p.blocks
}

func (p *parser) start(dec *xml.Decoder, t xml.StartElement) {
	switch ns(t.Name) {
	case "office":
		if t.Name.Local == "spreadsheet" {
			p.sheets = true
		}
	case nsDrawing:
		switch t.Name.Local {
		case "frame":
			p.titleFrame = attr(t, "presentation", "class") == "title"
		case "page":
			p.page++
		case "image":
			if mk := p.picture(attr(t, "", "href")); mk != "" {
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: mk})
			}
		}
	case "text":
		switch t.Name.Local {
		case "h":
			level, _ := strconv.Atoi(attr(t, "text", "outline-level"))
			if level < 1 {
				level = 1
			}
			if txt := elemText(dec); txt != "" {
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: level, Text: txt})
			}
		case "p":
			txt := elemText(dec)
			if txt == "" {
				return
			}
			switch {
			case p.listDepth > 0:
				p.blocks = append(p.blocks, mdrender.Block{
					Kind: mdrender.List,
					Text: strings.Repeat("  ", p.listDepth-1) + "- " + txt,
				})
			case p.titleFrame:
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: 2, Text: txt})
			default:
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: txt})
			}
		case "list":
			p.listDepth++
		}
	case "table":
		if t.Name.Local != "table" {
			return
		}
		name := attr(t, "table", "name")
		if p.sheets {
			p.page++
		}
		grid := readTable(dec)
		grid = mdrender.NormalizeGrid(grid)
		if len(grid) == 0 {
			return
		}
		if p.sheets && name != "" {
			p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: 1, Text: name})
		}
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Table_, Text: mdrender.Table(grid)})
	}
}

func elemText(dec *xml.Decoder) string {
	var b strings.Builder
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if ns(t.Name) == "text" {
				switch t.Name.Local {
				case "s", "tab", "line-break":
					b.WriteString(" ")
				}
			}
		case xml.EndElement:
			if depth--; depth == 0 {
				return strings.Join(strings.Fields(b.String()), " ")
			}
		case xml.CharData:
			b.Write(t)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func readTable(dec *xml.Decoder) [][]string {
	var grid [][]string
	cells := 0
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if ns(t.Name) == "table" && t.Name.Local == "table-row" {
				row, n := readRow(dec)
				cells += n
				rep := repeat(t, "number-rows-repeated")
				if len(row) == 0 {
					continue
				}
				for k := 0; k < rep && cells < maxCells; k++ {
					grid = append(grid, row)
					cells += n
				}
				continue
			}
			depth++
		case xml.EndElement:
			if depth--; depth == 0 {
				return grid
			}
		}
	}
	return grid
}

func readRow(dec *xml.Decoder) ([]string, int) {
	var row []string
	col := 0
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if ns(t.Name) == "table" && (t.Name.Local == "table-cell" || t.Name.Local == "covered-table-cell") {
				rep := repeat(t, "number-columns-repeated")
				txt := elemText(dec)
				if txt == "" {
					col += rep
					continue
				}
				for len(row) < col {
					row = append(row, "")
				}
				for k := 0; k < rep && len(row) < maxRepeat; k++ {
					row = append(row, txt)
				}
				col += rep
				continue
			}
			depth++
		case xml.EndElement:
			if depth--; depth == 0 {
				return row, len(row)
			}
		}
	}
	return row, len(row)
}

func repeat(t xml.StartElement, name string) int {
	n, err := strconv.Atoi(attr(t, "table", name))
	if err != nil || n < 1 {
		return 1
	}
	if n > maxRepeat {
		return maxRepeat
	}
	return n
}

const maxImageBytes = 128 << 20

func (p *parser) picture(href string) string {
	if href == "" || p.zr == nil || p.total >= maxImageBytes {
		return ""
	}
	if strings.Contains(href, "://") {
		return ""
	}
	name := path.Clean(strings.TrimPrefix(href, "./"))
	ext := docimg.Ext(path.Ext(name))
	if ext == "" {
		return ""
	}
	f := zipx.Find(p.zr, name)
	if f == nil {
		return ""
	}
	b, err := zipx.ReadEntry(f)
	if err != nil || len(b) == 0 {
		return ""
	}
	p.total += len(b)
	p.seq++
	p.images = append(p.images, docimg.Image{Seq: p.seq, Page: p.page, Bytes: b, Ext: ext})
	return docimg.MarkerFor(p.seq)
}
