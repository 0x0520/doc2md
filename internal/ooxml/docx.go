// Package ooxml converts Word (.docx) and PowerPoint (.pptx) documents to Markdown.
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
	"github.com/0x0520/doc2md/internal/zipx"
)

// ErrNotDocx is returned (wrapped) when the input is not a readable .docx.
var ErrNotDocx = errors.New("not a readable .docx (OOXML) document")

// ToMarkdown converts a .docx or .docm to Markdown and returns its embedded figures.
func ToMarkdown(data []byte) (string, []docimg.Image, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNotDocx, err)
	}
	// Only the parts the parser needs are inflated; the rest of the archive is left alone.
	parts := map[string][]byte{}
	for _, f := range zr.File {
		switch f.Name {
		case "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels":
			b, rerr := zipx.ReadEntry(f)
			if rerr == nil {
				parts[f.Name] = b
			}
		}
	}
	doc, ok := parts["word/document.xml"]
	if !ok {
		return "", nil, ErrNotDocx
	}

	p := &parser{
		styles:      parseStyles(parts["word/styles.xml"]),
		nums:        parseNumbering(parts["word/numbering.xml"]),
		rels:        parseRels(parts["word/_rels/document.xml.rels"]),
		defaultSize: parseDefaultSize(parts["word/styles.xml"]),
		media:       newMedia(zr),
		embeds:      relTargets(parts["word/_rels/document.xml.rels"], "word"),
	}
	blocks := p.parseDocument(doc)
	return mdrender.Document(blocks), p.media.images, nil
}

// styleInfo is what heading detection needs from a style: its lowercased name,
// outline level (-1 if unset) and parent style.
type styleInfo struct {
	name    string
	outline int
	basedOn string
}

// parseStyles indexes word/styles.xml by style id; unparsable XML gives an empty map.
func parseStyles(b []byte) map[string]styleInfo {
	out := map[string]styleInfo{}
	if len(b) == 0 {
		return out
	}
	var doc struct {
		Styles []struct {
			StyleID string `xml:"styleId,attr"`
			Name    struct {
				Val string `xml:"val,attr"`
			} `xml:"name"`
			BasedOn struct {
				Val string `xml:"val,attr"`
			} `xml:"basedOn"`
			PPr struct {
				OutlineLvl struct {
					Val string `xml:"val,attr"`
				} `xml:"outlineLvl"`
			} `xml:"pPr"`
		} `xml:"style"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, s := range doc.Styles {
		if s.StyleID == "" {
			continue
		}
		info := styleInfo{name: strings.ToLower(strings.TrimSpace(s.Name.Val)), outline: -1, basedOn: s.BasedOn.Val}
		if s.PPr.OutlineLvl.Val != "" {
			if n, err := strconv.Atoi(s.PPr.OutlineLvl.Val); err == nil {
				info.outline = n
			}
		}
		out[s.StyleID] = info
	}
	return out
}

// parseDefaultSize returns the document's default font size in half-points.
func parseDefaultSize(b []byte) int {
	// Word's own default of 11 pt, in half-points.
	const wordDefault = 22
	if len(b) == 0 {
		return wordDefault
	}
	var doc struct {
		DocDefaults struct {
			RPrDefault struct {
				RPr struct {
					Sz struct {
						Val string `xml:"val,attr"`
					} `xml:"sz"`
				} `xml:"rPr"`
			} `xml:"rPrDefault"`
		} `xml:"docDefaults"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return wordDefault
	}
	if n, err := strconv.Atoi(doc.DocDefaults.RPrDefault.RPr.Sz.Val); err == nil && n > 0 {
		return n
	}
	return wordDefault
}

// headingLevel resolves a style to a Markdown heading depth, or 0 if it is not a
// heading. The basedOn chain is followed at most 10 steps to survive cycles.
func headingLevel(styles map[string]styleInfo, styleID string) int {
	for i := 0; styleID != "" && i < 10; i++ {
		s, ok := styles[styleID]
		if !ok {
			return 0
		}
		if lvl := headingFromName(s.name); lvl > 0 {
			return lvl
		}
		if s.outline >= 0 && s.outline <= 8 {
			// Outline level 0 is a top-level heading; h1 is reserved for Title, hence the offset.
			return s.outline + 2
		}
		styleID = s.basedOn
	}
	return 0
}

// headingFromName maps the built-in style names "title" and "heading 1".."heading 9".
func headingFromName(name string) int {
	if name == "title" {
		return 1
	}
	if rest, ok := strings.CutPrefix(name, "heading "); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && n >= 1 && n <= 9 {
			return n + 1
		}
	}
	return 0
}

// numbering resolves a list instance (numId) through its abstract definition to a
// number format per level.
type numbering struct {
	numToAbstract map[string]string
	abstractFmt   map[string]map[int]string
}

func parseNumbering(b []byte) *numbering {
	n := &numbering{numToAbstract: map[string]string{}, abstractFmt: map[string]map[int]string{}}
	if len(b) == 0 {
		return n
	}
	var doc struct {
		Abstract []struct {
			ID   string `xml:"abstractNumId,attr"`
			Lvls []struct {
				Ilvl   string `xml:"ilvl,attr"`
				NumFmt struct {
					Val string `xml:"val,attr"`
				} `xml:"numFmt"`
			} `xml:"lvl"`
		} `xml:"abstractNum"`
		Nums []struct {
			ID       string `xml:"numId,attr"`
			Abstract struct {
				Val string `xml:"val,attr"`
			} `xml:"abstractNumId"`
		} `xml:"num"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return n
	}
	for _, a := range doc.Abstract {
		m := map[int]string{}
		for _, l := range a.Lvls {
			if ilvl, err := strconv.Atoi(l.Ilvl); err == nil {
				m[ilvl] = l.NumFmt.Val
			}
		}
		n.abstractFmt[a.ID] = m
	}
	for _, num := range doc.Nums {
		n.numToAbstract[num.ID] = num.Abstract.Val
	}
	return n
}

// ordered reports whether level ilvl of list numID is numbered rather than bulleted.
func (n *numbering) ordered(numID string, ilvl int) bool {
	abs, ok := n.numToAbstract[numID]
	if !ok {
		return false
	}
	fmts, ok := n.abstractFmt[abs]
	if !ok {
		return false
	}
	f, ok := fmts[ilvl]
	if !ok {
		return false
	}
	return f != "" && f != "bullet" && f != "none"
}

// parseRels returns hyperlink targets by relationship id.
func parseRels(b []byte) map[string]string {
	out := map[string]string{}
	if len(b) == 0 {
		return out
	}
	var doc struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, r := range doc.Rels {
		if strings.HasSuffix(r.Type, "/hyperlink") || r.Mode == "External" {
			out[r.ID] = r.Target
		}
	}
	return out
}

// parser carries the lookup tables for one pass over word/document.xml.
type parser struct {
	styles      map[string]styleInfo
	nums        *numbering
	rels        map[string]string
	defaultSize int
	media       *media
	embeds      map[string]string
}

// parseDocument streams document.xml into blocks. Top-level paragraphs and tables
// are found by scanning every element, so content inside w:sdt wrappers is included.
func (p *parser) parseDocument(b []byte) []mdrender.Block {
	dec := xml.NewDecoder(bytes.NewReader(b))
	var blocks []mdrender.Block
	var sizes []int
	styled := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "p":
			if blk, size, ok := p.paragraphBlock(dec); ok {
				if size == 0 {
					size = p.defaultSize
				}
				blocks = append(blocks, blk)
				sizes = append(sizes, size)
				styled = styled || blk.Kind == mdrender.Heading
			}
		case "tbl":
			if blk, ok := p.tableBlock(dec); ok {
				blocks = append(blocks, blk)
				sizes = append(sizes, 0)
			}
		}
	}
	// No paragraph used a heading style: fall back to inferring headings from font size.
	if !styled {
		promoteBySize(blocks, sizes)
	}
	return blocks
}

// paragraphBlock reads a w:p whose start tag was consumed. It returns the block,
// the largest run size in half-points, and false for an empty paragraph.
func (p *parser) paragraphBlock(dec *xml.Decoder) (mdrender.Block, int, bool) {
	styleID, numID, ilvl, hasNum := "", "", 0, false
	size := 0
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "pPr":
				styleID, numID, ilvl, hasNum = p.paragraphProps(dec)
			case "r":
				txt, sz := p.runText(dec)
				sb.WriteString(txt)
				if sz > size {
					size = sz
				}
			case "hyperlink":
				sb.WriteString(p.hyperlink(dec, t))
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				depth = 0
			}
		}
	}

	text := collapseSpaces(sb.String())
	if text == "" {
		return mdrender.Block{}, 0, false
	}
	if lvl := headingLevel(p.styles, styleID); lvl > 0 {
		return mdrender.Block{Kind: mdrender.Heading, Level: lvl, Text: text}, size, true
	}
	if hasNum {
		marker := "- "
		if p.nums.ordered(numID, ilvl) {
			marker = "1. "
		}
		indent := strings.Repeat("  ", ilvl)
		return mdrender.Block{Kind: mdrender.List, Text: indent + marker + text}, size, true
	}
	return mdrender.Block{Kind: mdrender.Paragraph, Text: text}, size, true
}

func (p *parser) paragraphProps(dec *xml.Decoder) (styleID, numID string, ilvl int, hasNum bool) {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "pStyle":
				styleID = attr(t, "val")
			case "numPr":
				numID, ilvl, hasNum = p.numPr(dec)
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "pPr" {
				depth = 0
			}
		}
	}
	return
}

// numPr reads w:numPr. ilvl is clamped to 0-8, the range Word allows.
func (p *parser) numPr(dec *xml.Decoder) (numID string, ilvl int, has bool) {
	has = true
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "ilvl":
				if n, err := strconv.Atoi(attr(t, "val")); err == nil {
					if n < 0 {
						n = 0
					} else if n > 8 {
						n = 8
					}
					ilvl = n
				}
			case "numId":
				numID = attr(t, "val")
			}
			if t.Name.Local != "numPr" {
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "numPr" {
				depth = 0
			}
		}
	}
	return
}

func (p *parser) runText(dec *xml.Decoder) (string, int) {
	var sb strings.Builder
	size := 0
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
			// Tabs and line breaks become spaces: a Markdown paragraph is a single line.
			case "tab":
				sb.WriteString(" ")
			case "br", "cr":
				sb.WriteString(" ")
			case "rPr":
				size = p.runSize(dec)
			case "drawing", "pict", "object":
				sb.WriteString(p.picture(dec, t.Name.Local))
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "r" {
				depth = 0
			}
		}
	}
	return sb.String(), size
}

func (p *parser) runSize(dec *xml.Decoder) int {
	size := 0
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "sz" {
				if n, err := strconv.Atoi(attr(t, "val")); err == nil {
					size = n
				}
			}
			skip(dec)
		case xml.EndElement:
			if t.Name.Local == "rPr" {
				depth = 0
			}
		}
	}
	return size
}

// hyperlink renders a w:hyperlink as a Markdown link, or as plain text when the
// relationship does not resolve.
func (p *parser) hyperlink(dec *xml.Decoder, start xml.StartElement) string {
	rID := attr(start, "id")
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "r" {
				txt, _ := p.runText(dec)
				sb.WriteString(txt)
			} else {
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "hyperlink" {
				depth = 0
			}
		}
	}
	text := sb.String()
	if url, ok := p.rels[rID]; ok && url != "" && strings.TrimSpace(text) != "" {
		return "[" + text + "](" + url + ")"
	}
	return text
}

func (p *parser) tableBlock(dec *xml.Decoder) (mdrender.Block, bool) {
	var grid [][]string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tr":
				grid = append(grid, p.tableRow(dec))
			default:
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

func (p *parser) tableRow(dec *xml.Decoder) []string {
	var row []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tc":
				row = append(row, p.cellText(dec))
			default:
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

// cellText flattens a cell's paragraphs and nested tables onto one line, since a
// pipe-table cell cannot hold blocks.
func (p *parser) cellText(dec *xml.Decoder) string {
	var parts []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				if blk, _, ok := p.paragraphBlock(dec); ok {
					parts = append(parts, stripListMarker(blk))
				}
			case "tbl":
				if blk, ok := p.tableBlock(dec); ok {
					parts = append(parts, blk.Text)
				}
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "tc" {
				depth = 0
			}
		}
	}
	return collapseSpaces(strings.Join(parts, " "))
}

// stripListMarker returns a list block's text without its indent and marker.
func stripListMarker(b mdrender.Block) string {
	if b.Kind != mdrender.List {
		return b.Text
	}
	t := strings.TrimLeft(b.Text, " ")
	t = strings.TrimPrefix(t, "- ")
	t = strings.TrimPrefix(t, "1. ")
	return t
}

// skip consumes the rest of the element whose start tag was just read.
func skip(dec *xml.Decoder) {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

// readText returns the character data of the element whose start tag was just read.
func readText(dec *xml.Decoder) string {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return sb.String()
}

// attr returns the attribute with this local name, ignoring its namespace prefix.
func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

const (
	// Limits for size-based heading promotion: a heading is at most maxHeadingChars
	// long, and promotion is abandoned if candidates reach 1/maxPromotedShare of all
	// paragraphs (then the size differences are formatting, not structure).
	maxHeadingChars  = 120
	maxPromotedShare = 2
)

// promoteBySize turns paragraphs set larger than the body text into headings, for
// documents that use no heading styles. Body size is the one carrying the most
// characters; larger sizes rank to levels 1.., biggest first.
func promoteBySize(blocks []mdrender.Block, sizes []int) {
	chars := map[int]int{}
	paragraphs := 0
	for i, blk := range blocks {
		if blk.Kind != mdrender.Paragraph || sizes[i] == 0 {
			continue
		}
		paragraphs++
		chars[sizes[i]] += len(blk.Text)
	}
	body, most := 0, 0
	for size, n := range chars {
		if n > most || (n == most && size < body) {
			body, most = size, n
		}
	}
	if body == 0 {
		return
	}

	candidates := 0
	larger := map[int]bool{}
	for i, blk := range blocks {
		if isSizeHeading(blk, sizes[i], body) {
			candidates++
			larger[sizes[i]] = true
		}
	}
	if candidates == 0 || candidates*maxPromotedShare >= paragraphs {
		return
	}

	ranked := make([]int, 0, len(larger))
	for size := range larger {
		ranked = append(ranked, size)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ranked)))
	level := map[int]int{}
	for i, size := range ranked {
		level[size] = i + 1
	}
	for i, blk := range blocks {
		if isSizeHeading(blk, sizes[i], body) {
			blocks[i] = mdrender.Block{Kind: mdrender.Heading, Level: level[sizes[i]], Text: blk.Text}
		}
	}
}

func isSizeHeading(blk mdrender.Block, size, body int) bool {
	return blk.Kind == mdrender.Paragraph && size > body && len(blk.Text) <= maxHeadingChars
}

// picture returns the marker for the image inside a drawing, pict or object
// element (closing is its tag name), or "" if there is none.
func (p *parser) picture(dec *xml.Decoder, closing string) string {
	id := embedID(dec, closing)
	if id == "" || p.media == nil {
		return ""
	}
	part := p.embeds[id]
	if part == "" {
		return ""
	}
	if m := p.media.marker(part, 0); m != "" {
		return " " + m + " "
	}
	return ""
}
