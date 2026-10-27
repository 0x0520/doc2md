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
		case "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels",
			"word/footnotes.xml", "word/endnotes.xml":
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
		noteSrc:     map[string][]byte{"footnote": parts["word/footnotes.xml"], "endnote": parts["word/endnotes.xml"]},
	}
	blocks := p.parseDocument(doc)
	// Appended after parsing so that note text never takes part in heading inference.
	for _, d := range p.noteDefs {
		blocks = append(blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: d})
	}
	return mdrender.Document(blocks), p.media.images, nil
}

// tri is a tri-state run property: inherited (unset), explicitly on or explicitly off.
type tri int8

const (
	unset tri = iota
	on
	off
)

// styleInfo is what the parser needs from a style: its lowercased name, outline
// level (-1 if unset), parent style, the list it joins (numID "" if none, ilvl -1 if
// unset) and its character formatting.
type styleInfo struct {
	name                 string
	outline              int
	basedOn              string
	numID                string
	ilvl                 int
	bold, italic, strike tri
}

// valAttr is an element that carries only w:val.
type valAttr struct {
	Val string `xml:"val,attr"`
}

// state reads a toggle property: a bare element is on, "0", "false" and "off" turn it off.
func (v *valAttr) state() tri {
	if v == nil {
		return unset
	}
	return toggle(v.Val)
}

func toggle(val string) tri {
	switch strings.ToLower(val) {
	case "0", "false", "off":
		return off
	}
	return on
}

// combine merges w:strike and w:dstrike: either one being on turns strikethrough on.
func combine(a, b tri) tri {
	if a == on || b == on {
		return on
	}
	if a == off || b == off {
		return off
	}
	return unset
}

// maxStyleChain bounds basedOn walks so a cyclic style definition terminates.
const maxStyleChain = 10

// parseStyles indexes word/styles.xml by style id; unparsable XML gives an empty map.
func parseStyles(b []byte) map[string]styleInfo {
	out := map[string]styleInfo{}
	if len(b) == 0 {
		return out
	}
	var doc struct {
		Styles []struct {
			StyleID string  `xml:"styleId,attr"`
			Name    valAttr `xml:"name"`
			BasedOn valAttr `xml:"basedOn"`
			PPr     struct {
				OutlineLvl valAttr `xml:"outlineLvl"`
				NumPr      struct {
					Ilvl  *valAttr `xml:"ilvl"`
					NumID *valAttr `xml:"numId"`
				} `xml:"numPr"`
			} `xml:"pPr"`
			RPr struct {
				B       *valAttr `xml:"b"`
				I       *valAttr `xml:"i"`
				Strike  *valAttr `xml:"strike"`
				DStrike *valAttr `xml:"dstrike"`
			} `xml:"rPr"`
		} `xml:"style"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, s := range doc.Styles {
		if s.StyleID == "" {
			continue
		}
		info := styleInfo{
			name:    strings.ToLower(strings.TrimSpace(s.Name.Val)),
			outline: -1,
			basedOn: s.BasedOn.Val,
			ilvl:    -1,
			bold:    s.RPr.B.state(),
			italic:  s.RPr.I.state(),
			strike:  combine(s.RPr.Strike.state(), s.RPr.DStrike.state()),
		}
		if s.PPr.OutlineLvl.Val != "" {
			if n, err := strconv.Atoi(s.PPr.OutlineLvl.Val); err == nil {
				info.outline = n
			}
		}
		if v := s.PPr.NumPr.NumID; v != nil {
			info.numID = v.Val
		}
		if v := s.PPr.NumPr.Ilvl; v != nil {
			if n, err := strconv.Atoi(v.Val); err == nil {
				info.ilvl = clampLevel(n)
			}
		}
		out[s.StyleID] = info
	}
	return out
}

// clampLevel limits a list level to 0-8, the range Word allows.
func clampLevel(n int) int {
	return min(max(n, 0), 8)
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
// heading. The basedOn chain is followed at most maxStyleChain steps to survive cycles.
func headingLevel(styles map[string]styleInfo, styleID string) int {
	for i := 0; styleID != "" && i < maxStyleChain; i++ {
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
	// styleLvl maps abstract id -> style id -> the level that style is linked to.
	styleLvl map[string]map[string]int
}

func parseNumbering(b []byte) *numbering {
	n := &numbering{
		numToAbstract: map[string]string{},
		abstractFmt:   map[string]map[int]string{},
		styleLvl:      map[string]map[string]int{},
	}
	if len(b) == 0 {
		return n
	}
	var doc struct {
		Abstract []struct {
			ID   string `xml:"abstractNumId,attr"`
			Lvls []struct {
				Ilvl   string  `xml:"ilvl,attr"`
				NumFmt valAttr `xml:"numFmt"`
				PStyle valAttr `xml:"pStyle"`
			} `xml:"lvl"`
		} `xml:"abstractNum"`
		Nums []struct {
			ID       string  `xml:"numId,attr"`
			Abstract valAttr `xml:"abstractNumId"`
		} `xml:"num"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return n
	}
	for _, a := range doc.Abstract {
		fmts := map[int]string{}
		links := map[string]int{}
		for _, l := range a.Lvls {
			ilvl, err := strconv.Atoi(l.Ilvl)
			if err != nil {
				continue
			}
			fmts[ilvl] = l.NumFmt.Val
			if l.PStyle.Val != "" {
				links[l.PStyle.Val] = clampLevel(ilvl)
			}
		}
		n.abstractFmt[a.ID] = fmts
		n.styleLvl[a.ID] = links
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

// field is one open complex field (fldChar begin ... end).
type field struct {
	instr  string
	result bool // past the separator: text now belongs to the field's result
	toc    bool // a table-of-contents field, whose result is dropped
}

// parser carries the lookup tables for one pass over word/document.xml. fields is
// state that outlives a paragraph: a TOC field starts in one paragraph and ends in another.
type parser struct {
	styles      map[string]styleInfo
	nums        *numbering
	rels        map[string]string
	defaultSize int
	media       *media
	embeds      map[string]string
	fields      []field
	// boxes collects the text-box paragraphs anchored in the paragraph being read.
	boxes []boxPara
	// Footnotes and endnotes share one numbering in order of first reference.
	// noteSrc holds the raw parts, noteText their parsed notes by kind and id
	// (parsed on first use), noteNum the assigned numbers and noteDefs the finished
	// "[^N]: text" lines.
	noteSrc  map[string][]byte
	noteText map[string]map[string]string
	noteNum  map[string]int
	noteDefs []string
}

// boxPara is a paragraph or table read outside the main flow, with what
// parseDocument needs to place it.
type boxPara struct {
	blk   mdrender.Block
	plain string
	size  int
}

// parseDocument streams document.xml into blocks. Top-level paragraphs and tables
// are found by scanning every element, so content inside w:sdt wrappers is included.
func (p *parser) parseDocument(b []byte) []mdrender.Block {
	dec := xml.NewDecoder(bytes.NewReader(b))
	var blocks []mdrender.Block
	var sizes []int
	var plains []string
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
		case "sdtPr":
			if sdtDropped(dec) {
				skip(dec)
			}
		case "Fallback":
			// The Choice branch of mc:AlternateContent was read already; this is its copy.
			skip(dec)
		case "p":
			blk, plain, size, ok := p.paragraphBlock(dec)
			add := func(blk mdrender.Block, plain string, size int) {
				if size == 0 {
					size = p.defaultSize
				}
				blocks = append(blocks, blk)
				sizes = append(sizes, size)
				plains = append(plains, plain)
				styled = styled || blk.Kind == mdrender.Heading
			}
			if ok {
				add(blk, plain, size)
			}
			for _, b := range p.boxes {
				add(b.blk, b.plain, b.size)
			}
			p.boxes = nil
		case "tbl":
			if blk, ok := p.tableBlock(dec); ok {
				blocks = append(blocks, blk)
				sizes = append(sizes, 0)
				plains = append(plains, blk.Text)
			}
		}
	}
	// No paragraph used a heading style: fall back to inferring headings from font size.
	if !styled {
		promoteBySize(blocks, sizes, plains)
	}
	return blocks
}

// sdtDropped reads a w:sdtPr and reports whether the content of the control is to
// be dropped: Word's built-in table of contents (the headings already carry the
// structure it repeats with page numbers) or an unfilled placeholder.
func sdtDropped(dec *xml.Decoder) bool {
	found := false
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "docPartGallery":
				if strings.EqualFold(attr(t, "val"), "Table of Contents") {
					found = true
				}
			case "showingPlcHdr":
				if toggle(attr(t, "val")) == on {
					found = true
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return found
}

// numRef is the direct w:numPr of a paragraph.
type numRef struct {
	id      string
	hasID   bool
	ilvl    int
	hasIlvl bool
}

// paraProps is what a paragraph's w:pPr contributes.
type paraProps struct {
	style string
	num   numRef
}

// paragraphBlock reads a w:p whose start tag was consumed. It returns the block, its
// text without emphasis markers, the largest run size in half-points, and false for
// an empty paragraph.
func (p *parser) paragraphBlock(dec *xml.Decoder) (blk mdrender.Block, plain string, size int, ok bool) {
	var props paraProps
	var segs []segment
loop:
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "pPr" {
				props = p.paragraphProps(dec)
			} else {
				segs = append(segs, p.inlineChild(dec, t)...)
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				break loop
			}
		}
	}

	plain = collapseSpaces(renderSegments(segs, false))
	if plain == "" {
		return mdrender.Block{}, "", 0, false
	}
	for _, s := range segs {
		size = max(size, s.size)
	}
	// Heading text stays unformatted: styles often set bold on whole headings.
	if lvl := headingLevel(p.styles, props.style); lvl > 0 {
		return mdrender.Block{Kind: mdrender.Heading, Level: lvl, Text: plain}, plain, size, true
	}
	text := collapseSpaces(renderSegments(segs, true))
	if ilvl, ordered, isList := p.listInfo(props); isList {
		marker := "- "
		if ordered {
			marker = "1. "
		}
		indent := strings.Repeat("  ", ilvl)
		return mdrender.Block{Kind: mdrender.List, Text: indent + marker + text}, plain, size, true
	}
	return mdrender.Block{Kind: mdrender.Paragraph, Text: text}, plain, size, true
}

func (p *parser) paragraphProps(dec *xml.Decoder) paraProps {
	var pp paraProps
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
				pp.style = attr(t, "val")
				skip(dec)
			case "numPr":
				pp.num = readNumPr(dec)
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "pPr" {
				depth = 0
			}
		}
	}
	return pp
}

// readNumPr reads w:numPr. ilvl is clamped to 0-8, the range Word allows.
func readNumPr(dec *xml.Decoder) numRef {
	var n numRef
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
				if v, err := strconv.Atoi(attr(t, "val")); err == nil {
					n.ilvl, n.hasIlvl = clampLevel(v), true
				}
			case "numId":
				n.id, n.hasID = attr(t, "val"), true
			}
			skip(dec)
		case xml.EndElement:
			if t.Name.Local == "numPr" {
				depth = 0
			}
		}
	}
	return n
}

// listInfo decides whether a paragraph is a list item and at which level. The
// paragraph's own numPr wins over the style chain, and numId 0 means "no list".
func (p *parser) listInfo(pp paraProps) (ilvl int, ordered, isList bool) {
	numID, haveLvl := "", false
	if pp.num.hasID {
		numID = pp.num.id
	}
	if pp.num.hasIlvl {
		ilvl, haveLvl = pp.num.ilvl, true
	}
	for id, i := pp.style, 0; id != "" && i < maxStyleChain; i++ {
		st, ok := p.styles[id]
		if !ok {
			break
		}
		if numID == "" && !pp.num.hasID && st.numID != "" {
			numID = st.numID
		}
		if !haveLvl && st.ilvl >= 0 {
			ilvl, haveLvl = st.ilvl, true
		}
		id = st.basedOn
	}
	if numID == "" || numID == "0" {
		return 0, false, false
	}
	if !haveLvl {
		ilvl = p.linkedLevel(numID, pp.style)
	}
	return ilvl, p.nums.ordered(numID, ilvl), true
}

// linkedLevel returns the level of list numID that is linked to the style (or one of
// its parents) through w:lvl/w:pStyle, or 0.
func (p *parser) linkedLevel(numID, styleID string) int {
	links := p.nums.styleLvl[p.nums.numToAbstract[numID]]
	for i := 0; styleID != "" && i < maxStyleChain; i++ {
		if lvl, ok := links[styleID]; ok {
			return lvl
		}
		styleID = p.styles[styleID].basedOn
	}
	return 0
}

// segment is a piece of paragraph text with uniform formatting.
type segment struct {
	text                 string
	bold, italic, strike bool
	// raw text (picture markers, links) is emitted verbatim and never merged or emphasised.
	raw  bool
	size int // half-points, 0 if unknown
}

// inlineChild reads one inline element of a paragraph whose start tag was consumed.
func (p *parser) inlineChild(dec *xml.Decoder, t xml.StartElement) []segment {
	switch t.Name.Local {
	case "r":
		return p.runSegments(dec)
	case "hyperlink":
		return p.hyperlink(dec, t)
	case "fldSimple":
		if isTOCInstr(attr(t, "instr")) {
			skip(dec)
			return nil
		}
		return p.inlineSegments(dec, t.Name.Local)
	case "ins", "moveTo", "smartTag":
		return p.inlineSegments(dec, t.Name.Local)
	case "sdt":
		return p.sdtInline(dec)
	case "AlternateContent":
		return p.altContent(dec)
	}
	skip(dec)
	return nil
}

// sdtInline reads a run-level content control: its content is text like any other
// unless it only shows a placeholder.
func (p *parser) sdtInline(dec *xml.Decoder) []segment {
	var segs []segment
	drop := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return segs
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sdtPr":
				drop = sdtDropped(dec)
			case "sdtContent":
				if drop {
					skip(dec)
				} else {
					segs = append(segs, p.inlineSegments(dec, "sdtContent")...)
				}
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "sdt" {
				return segs
			}
		}
	}
}

// altContent reads mc:AlternateContent and keeps only its first Choice or Fallback,
// which would otherwise repeat the same content.
func (p *parser) altContent(dec *xml.Decoder) []segment {
	var segs []segment
	done := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return segs
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if n := t.Name.Local; !done && (n == "Choice" || n == "Fallback") {
				done = true
				segs = append(segs, p.inlineSegments(dec, n)...)
			} else {
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "AlternateContent" {
				return segs
			}
		}
	}
}

// inlineSegments reads inline children up to the end tag named end.
func (p *parser) inlineSegments(dec *xml.Decoder, end string) []segment {
	var segs []segment
	for {
		tok, err := dec.Token()
		if err != nil {
			return segs
		}
		switch t := tok.(type) {
		case xml.StartElement:
			segs = append(segs, p.inlineChild(dec, t)...)
		case xml.EndElement:
			if t.Name.Local == end {
				return segs
			}
		}
	}
}

// isTOCInstr reports whether a field instruction is a table-of-contents field.
func isTOCInstr(instr string) bool {
	f := strings.Fields(instr)
	return len(f) > 0 && strings.EqualFold(f[0], "TOC")
}

// fieldChar tracks complex fields: only the text between separate and end is the
// field's result, and a TOC's result is dropped.
func (p *parser) fieldChar(kind string) {
	n := len(p.fields)
	switch kind {
	case "begin":
		p.fields = append(p.fields, field{})
	case "separate":
		if n > 0 {
			f := &p.fields[n-1]
			f.result, f.toc = true, isTOCInstr(f.instr)
		}
	case "end":
		if n > 0 {
			p.fields = p.fields[:n-1]
		}
	}
}

// visible reports whether text at this point is document content rather than a
// field instruction or a dropped field result.
func (p *parser) visible() bool {
	for _, f := range p.fields {
		if !f.result || f.toc {
			return false
		}
	}
	return true
}

// runProps are the w:rPr values the converter uses.
type runProps struct {
	size                 int
	style                string
	bold, italic, strike tri
}

// runSegments reads a w:r whose start tag was consumed.
func (p *parser) runSegments(dec *xml.Decoder) []segment {
	var segs []segment
	var sb strings.Builder
	var rp runProps
	flush := func() {
		if sb.Len() > 0 {
			segs = append(segs, segment{text: sb.String()})
			sb.Reset()
		}
	}
	depth := 1
	choice := false // inside this run's mc:AlternateContent, a Choice was seen
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "AlternateContent":
				choice = false
			case "Choice":
				choice = true
			case "Fallback":
				if choice {
					skip(dec)
				}
			case "footnoteReference", "endnoteReference":
				if m := p.noteRef(strings.TrimSuffix(t.Name.Local, "Reference"), attr(t, "id")); m != "" && p.visible() {
					flush()
					segs = append(segs, segment{text: m, raw: true})
				}
				skip(dec)
			case "t":
				if txt := readText(dec); p.visible() {
					sb.WriteString(txt)
				}
			// Tabs and line breaks become spaces: a Markdown paragraph is a single line.
			case "tab", "br", "cr":
				if p.visible() {
					sb.WriteString(" ")
				}
			case "rPr":
				rp = readRunProps(dec)
			case "fldChar":
				p.fieldChar(attr(t, "fldCharType"))
				skip(dec)
			case "instrText":
				txt := readText(dec)
				if n := len(p.fields); n > 0 && !p.fields[n-1].result {
					p.fields[n-1].instr += txt
				}
			case "drawing", "pict", "object":
				if !p.visible() {
					skip(dec)
				} else if m := p.picture(dec, t.Name.Local); m != "" {
					flush()
					segs = append(segs, segment{text: m, raw: true})
				}
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "r" {
				depth = 0
			}
		}
	}
	flush()

	bold, italic, strike := p.format(rp)
	for i := range segs {
		segs[i].size = rp.size
		if !segs[i].raw {
			segs[i].bold, segs[i].italic, segs[i].strike = bold, italic, strike
		}
	}
	return segs
}

func readRunProps(dec *xml.Decoder) runProps {
	var rp runProps
	var strike, dstrike tri
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sz":
				if n, err := strconv.Atoi(attr(t, "val")); err == nil {
					rp.size = n
				}
			case "rStyle":
				rp.style = attr(t, "val")
			case "b":
				rp.bold = toggle(attr(t, "val"))
			case "i":
				rp.italic = toggle(attr(t, "val"))
			case "strike":
				strike = toggle(attr(t, "val"))
			case "dstrike":
				dstrike = toggle(attr(t, "val"))
			}
			skip(dec)
		case xml.EndElement:
			if t.Name.Local == "rPr" {
				depth = 0
			}
		}
	}
	rp.strike = combine(strike, dstrike)
	return rp
}

// format resolves a run's emphasis: direct formatting wins over its character style
// and that style's parents.
func (p *parser) format(rp runProps) (bold, italic, strike bool) {
	b, i, s := rp.bold, rp.italic, rp.strike
	for id, n := rp.style, 0; id != "" && n < maxStyleChain; n++ {
		st, ok := p.styles[id]
		if !ok {
			break
		}
		if b == unset {
			b = st.bold
		}
		if i == unset {
			i = st.italic
		}
		if s == unset {
			s = st.strike
		}
		id = st.basedOn
	}
	return b == on, i == on, s == on
}

// hyperlink renders a w:hyperlink as a Markdown link, or as plain text when the
// relationship does not resolve.
func (p *parser) hyperlink(dec *xml.Decoder, start xml.StartElement) []segment {
	segs := p.inlineSegments(dec, "hyperlink")
	label := renderSegments(segs, true)
	url, ok := p.rels[attr(start, "id")]
	if !ok || url == "" || strings.TrimSpace(label) == "" {
		return segs
	}
	size := 0
	for _, s := range segs {
		size = max(size, s.size)
	}
	// Edge whitespace stays outside the brackets, where it separates words.
	core := strings.TrimSpace(label)
	lead, trail, _ := strings.Cut(label, core)
	return []segment{{text: lead + "[" + core + "](" + url + ")" + trail, raw: true, size: size}}
}

// emphasis is the set of Markdown inline markers a segment carries.
// renderSegments joins segments into one string; with format set, emphasis is
// merged and rendered as mdrender.Join describes.
func renderSegments(segs []segment, format bool) string {
	ps := make([]mdrender.Piece, len(segs))
	for i, s := range segs {
		ps[i] = mdrender.Piece{Text: s.text, Raw: s.raw, Em: mdrender.Emphasis{Bold: s.bold, Italic: s.italic, Strike: s.strike}}
	}
	return mdrender.Join(ps, format)
}

func (p *parser) tableBlock(dec *xml.Decoder) (mdrender.Block, bool) {
	grid := p.tableGrid(dec)
	if len(grid) == 0 {
		return mdrender.Block{}, false
	}
	return mdrender.Block{Kind: mdrender.Table_, Text: mdrender.Table(grid)}, true
}

// tableGrid reads a w:tbl whose start tag was consumed into a normalised grid.
// Content controls around rows are transparent.
func (p *parser) tableGrid(dec *xml.Decoder) [][]string {
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
			case "sdt", "sdtContent":
			case "sdtPr":
				if sdtDropped(dec) {
					skip(dec)
				}
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "tbl" {
				depth = 0
			}
		}
	}
	return mdrender.NormalizeGrid(grid)
}

// flattenGrid folds a nested table into one line: a Markdown cell holds no blocks,
// so rows are joined with "; " and cells with ", ".
func flattenGrid(grid [][]string) string {
	var rows []string
	for _, r := range grid {
		var cells []string
		for _, c := range r {
			if c != "" {
				cells = append(cells, c)
			}
		}
		if len(cells) > 0 {
			rows = append(rows, strings.Join(cells, ", "))
		}
	}
	return strings.Join(rows, "; ")
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
			case "sdt", "sdtContent":
			case "sdtPr":
				if sdtDropped(dec) {
					skip(dec)
				}
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
				if blk, _, _, ok := p.paragraphBlock(dec); ok {
					parts = append(parts, stripListMarker(blk))
				}
				for _, b := range p.boxes {
					parts = append(parts, stripListMarker(b.blk))
				}
				p.boxes = nil
			case "tbl":
				if s := flattenGrid(p.tableGrid(dec)); s != "" {
					parts = append(parts, s)
				}
			case "sdt", "sdtContent":
			case "sdtPr":
				if sdtDropped(dec) {
					skip(dec)
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

// blockContent reads the paragraphs and tables of a container such as a text box
// (end is its tag name), including text boxes anchored inside it.
func (p *parser) blockContent(dec *xml.Decoder, end string) []boxPara {
	outer := p.boxes
	p.boxes = nil
	var out []boxPara
loop:
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				if blk, plain, size, ok := p.paragraphBlock(dec); ok {
					out = append(out, boxPara{blk, plain, size})
				}
				out = append(out, p.boxes...)
				p.boxes = nil
			case "tbl":
				if blk, ok := p.tableBlock(dec); ok {
					out = append(out, boxPara{blk: blk, plain: blk.Text})
				}
			case "sdt", "sdtContent":
			case "sdtPr":
				if sdtDropped(dec) {
					skip(dec)
				}
			default:
				skip(dec)
			}
		case xml.EndElement:
			if t.Name.Local == end {
				break loop
			}
		}
	}
	p.boxes = outer
	return out
}

// noteRef returns the "[^N]" marker for a footnote or endnote reference (kind is
// "footnote" or "endnote"), numbering notes in order of first reference and
// recording the definition. It returns "" if the note has no text.
func (p *parser) noteRef(kind, id string) string {
	key := kind + ":" + id
	if n, ok := p.noteNum[key]; ok {
		return "[^" + strconv.Itoa(n) + "]"
	}
	text := p.notes(kind)[id]
	if text == "" {
		return ""
	}
	if p.noteNum == nil {
		p.noteNum = map[string]int{}
	}
	n := len(p.noteNum) + 1
	p.noteNum[key] = n
	p.noteDefs = append(p.noteDefs, "[^"+strconv.Itoa(n)+"]: "+text)
	return "[^" + strconv.Itoa(n) + "]"
}

// notes parses footnotes.xml or endnotes.xml once. Separator and continuation
// notes (w:type other than "normal") are not content. The note being parsed must
// not see the field and text-box state of the paragraph that references it.
func (p *parser) notes(kind string) map[string]string {
	if m, ok := p.noteText[kind]; ok {
		return m
	}
	m := map[string]string{}
	if p.noteText == nil {
		p.noteText = map[string]map[string]string{}
	}
	p.noteText[kind] = m
	fields, boxes := p.fields, p.boxes
	p.fields, p.boxes = nil, nil
	defer func() { p.fields, p.boxes = fields, boxes }()

	dec := xml.NewDecoder(bytes.NewReader(p.noteSrc[kind]))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != kind {
			continue
		}
		if t := attr(se, "type"); t != "" && t != "normal" {
			skip(dec)
			continue
		}
		var parts []string
		for _, b := range p.blockContent(dec, kind) {
			parts = append(parts, stripListMarker(b.blk))
		}
		m[attr(se, "id")] = collapseSpaces(strings.Join(parts, " "))
	}
	return m
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
// characters; larger sizes rank to levels 1.., biggest first. plains holds each
// block's text without emphasis markers, which headings do not carry.
func promoteBySize(blocks []mdrender.Block, sizes []int, plains []string) {
	chars := map[int]int{}
	paragraphs := 0
	for i, blk := range blocks {
		if blk.Kind != mdrender.Paragraph || sizes[i] == 0 {
			continue
		}
		paragraphs++
		chars[sizes[i]] += len(plains[i])
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
		if isSizeHeading(blk, plains[i], sizes[i], body) {
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
		if isSizeHeading(blk, plains[i], sizes[i], body) {
			blocks[i] = mdrender.Block{Kind: mdrender.Heading, Level: level[sizes[i]], Text: plains[i]}
		}
	}
}

func isSizeHeading(blk mdrender.Block, plain string, size, body int) bool {
	return blk.Kind == mdrender.Paragraph && size > body && len(plain) <= maxHeadingChars
}

// picture returns the marker for the image inside a drawing, pict or object
// element (closing is its tag name), or "" if there is none.
func (p *parser) picture(dec *xml.Decoder, closing string) string {
	id := embedIDBox(dec, closing, func() {
		p.boxes = append(p.boxes, p.blockContent(dec, "txbxContent")...)
	})
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
