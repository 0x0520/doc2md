// Package odf converts OpenDocument text (.odt), spreadsheet (.ods) and
// presentation (.odp) files to Markdown by walking the package's content.xml
// with one shared vocabulary: headings, paragraphs, lists and tables.
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

// ErrNotODF is returned when the input is not a zip with a content.xml.
var ErrNotODF = errors.New("not a readable OpenDocument package")

const (
	// Spreadsheets declare their unused region as one cell or row repeated
	// thousands of times (e.g. number-columns-repeated="16384"). Empty runs are
	// only counted, never materialised, and runs with content are capped here.
	maxRepeat = 4096
	maxCells  = 1 << 20
)

// ToMarkdown converts OpenDocument bytes to Markdown and returns the images
// stored in the package (conventionally under Pictures/).
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
	p := &parser{zr: zr, st: newStyles()}
	// styles.xml holds the named styles; content.xml adds the automatic ones.
	if sf := zipx.Find(zr, "styles.xml"); sf != nil {
		if sb, err := zipx.ReadEntry(sf); err == nil {
			p.st.scan(sb)
		}
	}
	p.st.scan(content)
	blocks := p.walk(content)
	if len(blocks) == 0 {
		return "", nil, ErrNotODF
	}
	return mdrender.Document(blocks), p.images, nil
}

// nsDrawing is the namespace name of the drawing vocabulary. Documents bind it
// to the prefix "draw" but the URI says "drawing"; ns() yields the URI segment,
// which is what must be matched, since prefixes are arbitrary.
const nsDrawing = "drawing"

// ns returns the name segment of an element's namespace URI
// ("urn:oasis:names:tc:opendocument:xmlns:<name>:1.0"), e.g. "text" or "table".
func ns(n xml.Name) string {
	if i := strings.LastIndex(n.Space, ":"); i > 0 {
		if j := strings.LastIndex(n.Space[:i], ":"); j >= 0 {
			return n.Space[j+1 : i]
		}
	}
	return n.Space
}

// attr returns the attribute with the given local name; space "" matches any namespace.
func attr(e xml.StartElement, space, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local && (space == "" || ns(a.Name) == space) {
			return a.Value
		}
	}
	return ""
}

// paraStyle is the part of a paragraph style that matters here.
type paraStyle struct{ display, parent, list string }

// styles holds the paragraph styles and, per list style, which levels are numbered.
type styles struct {
	paras map[string]paraStyle
	lists map[string]map[int]bool
}

func newStyles() *styles {
	return &styles{paras: map[string]paraStyle{}, lists: map[string]map[int]bool{}}
}

// scan reads paragraph and list styles from styles.xml, or from the automatic
// styles at the start of content.xml, where it stops at the document body.
func (s *styles) scan(data []byte) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur map[int]bool
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		t, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch ns(t.Name) {
		case "office":
			if t.Name.Local == "body" {
				return
			}
		case "style":
			if t.Name.Local == "style" && attr(t, "style", "family") == "paragraph" {
				s.paras[attr(t, "style", "name")] = paraStyle{
					display: attr(t, "style", "display-name"),
					parent:  attr(t, "style", "parent-style-name"),
					list:    attr(t, "style", "list-style-name"),
				}
			}
		case "text":
			switch t.Name.Local {
			case "list-style":
				cur = map[int]bool{}
				s.lists[attr(t, "style", "name")] = cur
			// An empty num-format means the level shows no number.
			case "list-level-style-number":
				if lvl, err := strconv.Atoi(attr(t, "text", "level")); err == nil && cur != nil {
					cur[lvl] = attr(t, "style", "num-format") != ""
				}
			}
		}
	}
}

// isTitle reports whether a paragraph style is, or derives from, the built-in
// Title style. Word writes the localised name, so "Titel" counts too. The parent
// chain is followed at most 10 steps to survive cycles.
func (s *styles) isTitle(name string) bool {
	for i := 0; name != "" && i < 10; i++ {
		ps, known := s.paras[name]
		for _, n := range []string{name, ps.display} {
			if n = strings.ToLower(n); n == "title" || n == "titel" {
				return true
			}
		}
		if !known {
			return false
		}
		name = ps.parent
	}
	return false
}

// listStyle returns the list style of a paragraph in a list: that of the nearest
// enclosing text:list naming one, else the one its paragraph style names.
func (s *styles) listStyle(stack []string, paragraph string) string {
	if n := len(stack); n > 0 && stack[n-1] != "" {
		return stack[n-1]
	}
	for i := 0; paragraph != "" && i < 10; i++ {
		ps, known := s.paras[paragraph]
		if !known {
			return ""
		}
		if ps.list != "" {
			return ps.list
		}
		paragraph = ps.parent
	}
	return ""
}

// frame is an open draw:frame. title marks a slide's title placeholder, whose
// paragraphs are headings. object is set once the frame holds a table or an
// embedded object, after which a draw:image in it is only the replacement
// picture the producer stores for that object.
type frame struct{ title, object bool }

// parser accumulates blocks during a single walk of content.xml. sheets is set
// for spreadsheets (each table is a named sheet and gets a heading; in text
// documents table names like "Table1" are internal labels). lists holds the
// list style name of each open text:list, frames the open draw:frames. page
// counts slides or sheets so images can record where they stood. titles are the
// indices of the blocks written in the Title style.
type parser struct {
	blocks []mdrender.Block
	titles []int
	lists  []string
	frames []frame
	// cellImages holds the markers of pictures anchored in table cells; a marker in the
	// cell text would add a bogus column or row, so they follow the table instead.
	cellImages []string
	inTable    bool
	st         *styles
	sheets     bool
	zr         *zip.Reader
	page       int
	seq        int
	total      int
	images     []docimg.Image
}

func (p *parser) walk(content []byte) []mdrender.Block {
	defer p.shiftHeadings()
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
			case ns(t.Name) == "text" && t.Name.Local == "list" && len(p.lists) > 0:
				p.lists = p.lists[:len(p.lists)-1]
			case ns(t.Name) == nsDrawing && t.Name.Local == "frame" && len(p.frames) > 0:
				p.frames = p.frames[:len(p.frames)-1]
			}
		}
	}
	return p.blocks
}

// shiftHeadings applies the DOCX reader's rule: Title is "#" and, when the
// document has one, the real headings move one level down.
func (p *parser) shiftHeadings() {
	if len(p.titles) == 0 {
		return
	}
	isTitle := map[int]bool{}
	for _, i := range p.titles {
		isTitle[i] = true
	}
	for i := range p.blocks {
		if p.blocks[i].Kind == mdrender.Heading && !isTitle[i] {
			p.blocks[i].Level++
		}
	}
}

func (p *parser) start(dec *xml.Decoder, t xml.StartElement) {
	switch ns(t.Name) {
	case "office":
		if t.Name.Local == "spreadsheet" {
			p.sheets = true
		}
	case nsDrawing:
		switch t.Name.Local {
		// presentation:class="title" marks the slide's title placeholder.
		case "frame":
			p.frames = append(p.frames, frame{title: attr(t, "presentation", "class") == "title"})
		case "page":
			p.page++
		case "object", "object-ole":
			p.markObject()
		case "image":
			if mk := p.imageMarker(t); mk != "" {
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
			if txt := p.inline(dec); txt != "" {
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: level, Text: txt})
			}
		case "p":
			txt := p.inline(dec)
			if txt == "" {
				return
			}
			style := attr(t, "text", "style-name")
			switch {
			case len(p.lists) > 0:
				marker := "- "
				if p.st.lists[p.st.listStyle(p.lists, style)][len(p.lists)] {
					marker = "1. "
				}
				p.blocks = append(p.blocks, mdrender.Block{
					Kind: mdrender.List,
					Text: strings.Repeat("  ", len(p.lists)-1) + marker + txt,
				})
			case p.inTitleFrame():
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: 2, Text: txt})
			case p.st.isTitle(style):
				p.titles = append(p.titles, len(p.blocks))
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: 1, Text: txt})
			default:
				p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: txt})
			}
		case "list":
			// A nested list without a style continues the style of its parent.
			name := attr(t, "text", "style-name")
			if name == "" && len(p.lists) > 0 {
				name = p.lists[len(p.lists)-1]
			}
			p.lists = append(p.lists, name)
		// The table of contents is generated text with page numbers.
		case "table-of-content":
			_ = dec.Skip()
		}
	case "table":
		if t.Name.Local != "table" {
			return
		}
		name := attr(t, "table", "name")
		if p.sheets {
			p.page++
		}
		p.markObject()
		p.inTable = true
		grid := p.readTable(dec)
		p.inTable = false
		defer p.flushCellImages()
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

func (p *parser) inTitleFrame() bool {
	return len(p.frames) > 0 && p.frames[len(p.frames)-1].title
}

// markObject records that the innermost frame holds a table or an object.
func (p *parser) markObject() {
	if n := len(p.frames); n > 0 {
		p.frames[n-1].object = true
	}
}

// imageMarker registers the draw:image t and returns its marker, or "" if it is
// not a picture of its own.
func (p *parser) imageMarker(t xml.StartElement) string {
	if n := len(p.frames); n > 0 && p.frames[n-1].object {
		return ""
	}
	// href is matched without namespace: xlink's URI does not have the OpenDocument
	// shape ns() parses, and draw:image has no other href attribute.
	return p.picture(attr(t, "", "href"))
}

// inline returns the text of the element just opened, consuming it through its
// end tag, with a marker where an image stands. Space, tab, line-break and
// nested paragraph elements become one space, since the result is a single
// Markdown line.
func (p *parser) inline(dec *xml.Decoder) string {
	var b strings.Builder
	depth := 1
	open := len(p.frames)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch ns(t.Name) {
			case "text":
				switch t.Name.Local {
				case "s", "tab", "line-break", "p", "h":
					b.WriteString(" ")
				}
			case "table":
				if t.Name.Local == "table" {
					p.markObject()
				}
			case nsDrawing:
				switch t.Name.Local {
				case "frame":
					p.frames = append(p.frames, frame{})
				case "object", "object-ole":
					p.markObject()
				case "image":
					if mk := p.imageMarker(t); mk != "" {
						if p.inTable {
							p.cellImages = append(p.cellImages, mk)
						} else {
							b.WriteString(" " + mk + " ")
						}
					}
				}
			}
		case xml.EndElement:
			if ns(t.Name) == nsDrawing && t.Name.Local == "frame" && len(p.frames) > open {
				p.frames = p.frames[:len(p.frames)-1]
			}
			if depth--; depth == 0 {
				return strings.Join(strings.Fields(b.String()), " ")
			}
		case xml.CharData:
			b.Write(t)
		}
	}
	p.frames = p.frames[:open]
	return strings.Join(strings.Fields(b.String()), " ")
}

// readTable reads the rows of a table, consuming it through its end tag.
func (p *parser) readTable(dec *xml.Decoder) [][]string {
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
				row := p.readRow(dec)
				rep := repeat(t, "number-rows-repeated")
				// A row repeated over the unused region has no content and is not worth repeating.
				if len(row) == 0 {
					continue
				}
				// Only rows that are kept count towards the cap.
				for k := 0; k < rep && cells < maxCells; k++ {
					grid = append(grid, row)
					cells += len(row)
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

// readRow reads one row. Empty cells only advance the column position, which
// keeps the unused region from expanding.
func (p *parser) readRow(dec *xml.Decoder) []string {
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
				txt := p.inline(dec)
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
				return row
			}
		}
	}
	return row
}

// repeat reads a repeat-count attribute, clamped to maxRepeat: large values are
// layout artifacts for "the rest of the sheet", not content.
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

// maxImageBytes caps the total image payload of one document.
const maxImageBytes = 128 << 20

// picture registers the package entry named by href and returns its marker, or
// "" if it is missing, external or in an unsupported format.
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

// flushCellImages emits the markers collected from the table just read.
func (p *parser) flushCellImages() {
	for _, mk := range p.cellImages {
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: mk})
	}
	p.cellImages = nil
}
