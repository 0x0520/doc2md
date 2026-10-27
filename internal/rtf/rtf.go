// Package rtf converts RTF documents to Markdown with a single-pass control-word
// scanner: text, headings, bullet and numbered lists, tables and embedded
// PNG/JPEG pictures.
package rtf

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
)

// ErrNotRTF is returned when the input is not RTF or contains no text.
var ErrNotRTF = errors.New("not a readable RTF document")

// maxCells bounds the table cells collected from one document.
const maxCells = 1 << 20

// Destinations whose content is not body text (font and color tables, headers,
// footers, footnotes, field codes, list definitions) and is dropped.
var skipDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true,
	"pict": true, "header": true, "footer": true, "headerl": true, "headerr": true,
	"headerf": true, "footerl": true, "footerr": true, "footerf": true,
	"footnote": true, "annotation": true, "comment": true, "xe": true, "tc": true,
	"filetbl": true, "revtbl": true, "listtable": true, "listoverridetable": true,
	"object": true, "fldinst": true, "atnid": true, "atnauthor": true,
	"pn": true, "pntxtb": true, "pntxta": true,
}

// Destinations holding the generated bullet or number of a list item. The text is
// kept apart from the body and the paragraph is emitted as a list item instead.
var markerDestinations = map[string]bool{"pntext": true, "listtext": true}

// Control words that stand for a single character.
var charWords = map[string]string{
	"lquote": "‘", "rquote": "’", "ldblquote": "“", "rdblquote": "”",
	"endash": "–", "emdash": "—", "bullet": "•",
	"enspace": " ", "emspace": " ", "qmspace": " ", "nbsp": " ",
	"tab": " ", "line": " ", "softline": " ",
}

// codePages maps \ansicpg values to single-byte code pages. Multi-byte East Asian
// pages are not supported: their characters arrive as pairs of \'hh escapes.
var codePages = map[int]*charmap.Charmap{
	437: charmap.CodePage437, 850: charmap.CodePage850, 852: charmap.CodePage852,
	866: charmap.CodePage866, 874: charmap.Windows874,
	1250: charmap.Windows1250, 1251: charmap.Windows1251, 1252: charmap.Windows1252,
	1253: charmap.Windows1253, 1254: charmap.Windows1254, 1255: charmap.Windows1255,
	1256: charmap.Windows1256, 1257: charmap.Windows1257, 1258: charmap.Windows1258,
	10000: charmap.Macintosh,
}

// charsetPages maps the \fcharset of a font to its code page. Charset 0 (ANSI) is
// absent on purpose: it means the document's own code page.
var charsetPages = map[int]*charmap.Charmap{
	161: charmap.Windows1253, 162: charmap.Windows1254, 163: charmap.Windows1258,
	177: charmap.Windows1255, 178: charmap.Windows1256, 186: charmap.Windows1257,
	204: charmap.Windows1251, 222: charmap.Windows874, 238: charmap.Windows1250,
}

// Level number formats (\levelnfc) that are not numbers.
const (
	nfcBullet = 23
	nfcNone   = 255
)

// orderedMarker matches a generated list label such as "1.", "a)" or "(iv.".
var orderedMarker = regexp.MustCompile(`^\(?[0-9A-Za-z]+[.)]`)

// state is the formatting state of one group; it is saved on '{' and restored on '}'.
// uc is the number of fallback characters after a \u escape, level the heading
// level of the current paragraph (0 for body text) and title whether it is in the
// Title style. ls and ilvl identify its list and nesting level, font the current
// font (-1 before any). marker routes text to the list label. em is the character
// emphasis, base the emphasis of the paragraph style and link the hyperlink whose
// result text is being written, if any.
type state struct {
	skip   bool
	marker bool
	title  bool
	uc     int
	level  int
	ls     int
	ilvl   int
	font   int
	em     mdrender.Emphasis
	base   mdrender.Emphasis
	link   *mdrender.Link
}

// parser accumulates blocks while walking the RTF source. Text goes to cell while
// inside a table row, otherwise to para.
type parser struct {
	styles   stylesheet
	lists    map[int][]int
	fonts    map[int]*charmap.Charmap
	docPage  *charmap.Charmap
	defFont  int
	blocks   []mdrender.Block
	titles   []int
	para     mdrender.Inline
	label    strings.Builder
	row      []string
	cell     mdrender.Inline
	grid     [][]string
	cells    int
	inRow    bool
	listItem bool
	// tocField is set between a TOC field's instruction and its result;
	// fieldLink holds the target of a HYPERLINK field until its result.
	tocField  bool
	fieldLink *mdrender.Link

	seq    int
	total  int
	images []docimg.Image
}

// ToMarkdown converts RTF bytes to Markdown and returns embedded pictures.
func ToMarkdown(data []byte) (string, []docimg.Image, error) {
	s := string(data)
	if !strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), `{\rtf`) {
		return "", nil, ErrNotRTF
	}
	p := &parser{
		styles:  parseStylesheet(s),
		lists:   parseLists(s),
		fonts:   parseFonts(s),
		docPage: charmap.Windows1252,
		defFont: -1,
	}
	p.walk(s)
	p.flushParagraph(state{})
	p.flushTable()
	if len(p.blocks) == 0 {
		return "", nil, ErrNotRTF
	}
	p.shiftHeadings()
	return mdrender.Document(p.blocks), p.images, nil
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

// walk scans s once, tracking group nesting; control words are handled in apply.
func (p *parser) walk(s string) {
	st := state{uc: 1, font: -1}
	var stack []state
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '{':
			stack = append(stack, st)
			i++
		case '}':
			if n := len(stack); n > 0 {
				st = stack[n-1]
				stack = stack[:n-1]
			}
			i++
		case '\\':
			word, arg, hasArg, next := control(s, i)
			i = p.apply(&st, s, word, arg, hasArg, next)
		// Raw line breaks are not content in RTF; \par is the paragraph break.
		case '\r', '\n':
			i++
		default:
			p.emit(st, string(c))
			i++
		}
	}
}

// apply handles one control word and returns the index to continue scanning from.
func (p *parser) apply(st *state, s, word string, arg int, hasArg bool, next int) int {
	switch word {
	// \* marks an ignorable destination: the rest of the group is dropped.
	case "*":
		st.skip = true
	case `\`, "{", "}":
		p.emit(*st, word)
	case "~":
		p.emit(*st, " ")
	// \'hh is one byte of the code page of the current font, else of the document.
	case "'":
		if next+2 <= len(s) {
			if b, err := strconv.ParseUint(s[next:next+2], 16, 8); err == nil {
				p.emit(*st, string(p.page(*st).DecodeByte(byte(b))))
			}
			return next + 2
		}
	// \uN is a UTF-16 code unit as a signed 16-bit number; the uc fallback characters
	// that follow it are skipped.
	case "u":
		if hasArg {
			r := arg
			if r < 0 {
				r += 65536
			}
			p.emit(*st, string(rune(r)))
			return skipUnits(s, next, st.uc)
		}
	case "uc":
		if hasArg {
			st.uc = arg
		}
	case "ansicpg":
		if cm, ok := codePages[arg]; ok && hasArg {
			p.docPage = cm
		}
	case "mac":
		p.docPage = charmap.Macintosh
	case "deff":
		if hasArg {
			p.defFont = arg
		}
	case "f":
		if hasArg {
			st.font = arg
		}
	// \binN is followed by N raw bytes that must not be parsed as RTF.
	case "bin":
		if hasArg && arg > 0 && next+arg <= len(s) {
			return next + arg
		}
	case "par", "sect":
		p.flushParagraph(*st)
	case "pard":
		st.level, st.title, st.ls, st.ilvl, st.base = 0, false, 0, 0, mdrender.Emphasis{}
	case "intbl", "trowd":
		p.openRow(*st)
	case "cell":
		p.endCell()
	case "row":
		p.endRow()
	// Outline level 0 is the top level, which becomes a level-1 heading.
	case "outlinelevel":
		if hasArg && arg >= 0 && arg <= 8 {
			st.level = arg + 1
		}
	case "s":
		if hasArg {
			st.base = p.styles.paras[arg]
			if p.styles.titles[arg] {
				st.title = true
			} else if lvl, ok := p.styles.levels[arg]; ok {
				st.level = lvl
			}
		}
	case "ls":
		if hasArg {
			st.ls = arg
		}
	case "ilvl":
		if hasArg && arg >= 0 && arg <= 8 {
			st.ilvl = arg
		}
	// Pictures are decoded by picture; the rest of the group (hex data) is skipped.
	case "pict":
		st.skip = true
		if end := p.picture(s, next); end > 0 {
			return end
		}
	case "field":
		p.tocField, p.fieldLink = false, nil
	// A table of contents is generated text with page numbers. Its instruction
	// is only readable here, so the result group that follows is dropped.
	case "fldinst":
		st.skip = true
		code := fieldCode(s, next)
		p.tocField = isTOC(code)
		if url := hyperlinkTarget(code); url != "" {
			p.fieldLink = &mdrender.Link{URL: url}
		}
	case "fldrslt":
		if p.tocField {
			st.skip = true
			p.tocField = false
		}
		st.link = p.fieldLink
	// A switch with argument 0 turns the property off.
	case "b":
		st.em.Bold = !hasArg || arg != 0
	case "i":
		st.em.Italic = !hasArg || arg != 0
	case "strike", "striked":
		st.em.Strike = !hasArg || arg != 0
	case "plain":
		st.em = mdrender.Emphasis{}
	case "cs":
		if cs, ok := p.styles.chars[arg]; ok && hasArg {
			st.em.Bold = st.em.Bold || cs.Bold
			st.em.Italic = st.em.Italic || cs.Italic
			st.em.Strike = st.em.Strike || cs.Strike
		}
	default:
		switch {
		case markerDestinations[word]:
			st.marker = true
			p.listItem = true
			p.label.Reset()
		case skipDestinations[word]:
			st.skip = true
		case charWords[word] != "":
			p.emit(*st, charWords[word])
		}
	}
	return next
}

// page returns the code page for \'hh bytes: that of the current font if its
// charset names one, else the font the document defaults to, else the document's.
func (p *parser) page(st state) *charmap.Charmap {
	for _, f := range []int{st.font, p.defFont} {
		if cm, ok := p.fonts[f]; ok {
			return cm
		}
	}
	return p.docPage
}

// emit writes text of the current group unless it is dropped; list labels are
// collected separately.
func (p *parser) emit(st state, s string) {
	switch {
	case st.skip:
	case st.marker:
		p.label.WriteString(s)
	default:
		p.write(st, s)
	}
}

// write adds text to the open cell or paragraph. Word repeats the formatting of a
// paragraph's style as direct formatting; emphasis the style itself carries is
// dropped, as the DOCX reader drops it, so only the author's own marking remains.
func (p *parser) write(st state, s string) {
	em := mdrender.Emphasis{Bold: st.em.Bold && !st.base.Bold, Italic: st.em.Italic && !st.base.Italic, Strike: st.em.Strike && !st.base.Strike}
	if p.inRow {
		p.cell.Add(s, em, st.link)
		return
	}
	p.para.Add(s, em, st.link)
}

// openRow starts a table row, flushing any open paragraph so it stays outside the table.
func (p *parser) openRow(st state) {
	if p.inRow {
		return
	}
	p.flushParagraph(st)
	p.inRow = true
}

// endCell closes the current cell; past maxCells the row is closed to stop growth.
func (p *parser) endCell() {
	if !p.inRow {
		return
	}
	p.row = append(p.row, squeeze(p.cell.Render(true)))
	p.cell.Reset()
	// A list label inside a cell belongs to the cell, not to the next paragraph.
	p.listItem = false
	p.label.Reset()
	if p.cells++; p.cells > maxCells {
		p.endRow()
	}
}

func (p *parser) endRow() {
	if !p.cell.Empty() {
		p.row = append(p.row, squeeze(p.cell.Render(true)))
		p.cell.Reset()
	}
	if len(p.row) > 0 {
		p.grid = append(p.grid, p.row)
		p.row = nil
	}
	p.inRow = false
}

// listKind reports whether the paragraph is a list item and whether it is numbered.
// The list definition decides; without one, the label Word generated does.
func (p *parser) listKind(st state) (item, ordered bool) {
	if levels := p.lists[st.ls]; st.ls > 0 && len(levels) > 0 {
		nfc := levels[min(st.ilvl, len(levels)-1)]
		return nfc != nfcNone || p.listItem, nfc != nfcBullet && nfc != nfcNone
	}
	if !p.listItem {
		return false, false
	}
	return true, orderedMarker.MatchString(strings.TrimSpace(p.label.String()))
}

// flushParagraph emits the pending text as a heading, list item or paragraph. Inside
// a table row a paragraph break only separates words within the cell.
func (p *parser) flushParagraph(st state) {
	item, ordered := p.listKind(st)
	p.listItem = false
	p.label.Reset()
	if p.inRow {
		p.cell.Add(" ", mdrender.Emphasis{}, nil)
		return
	}
	txt := squeeze(p.para.Render(!st.title && st.level == 0))
	p.para.Reset()
	if txt == "" {
		return
	}
	p.flushTable()
	switch {
	case st.title:
		p.titles = append(p.titles, len(p.blocks))
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: 1, Text: txt})
	case st.level > 0:
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: st.level, Text: txt})
	case item:
		marker := "- "
		if ordered {
			marker = "1. "
		}
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.List, Text: strings.Repeat("  ", st.ilvl) + marker + txt})
	default:
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: txt})
	}
}

func (p *parser) flushTable() {
	if len(p.grid) == 0 {
		return
	}
	grid := mdrender.NormalizeGrid(p.grid)
	p.grid = nil
	if len(grid) == 0 {
		return
	}
	p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Table_, Text: mdrender.Table(grid)})
}

// squeeze collapses runs of whitespace to single spaces.
func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }

// control parses the control word or symbol at s[i] (a backslash). It returns the
// word, its optional numeric argument, and the index after it, including the one
// space that delimits a control word.
func control(s string, i int) (word string, arg int, hasArg bool, next int) {
	j := i + 1
	if j >= len(s) {
		return "", 0, false, len(s)
	}
	c := s[j]
	if !isAlpha(c) {
		return string(c), 0, false, j + 1
	}
	k := j
	for k < len(s) && isAlpha(s[k]) {
		k++
	}
	word = s[j:k]
	n := k
	if n < len(s) && (s[n] == '-' || isDigit(s[n])) {
		m := n
		if s[m] == '-' {
			m++
		}
		for m < len(s) && isDigit(s[m]) {
			m++
		}
		if v, err := strconv.Atoi(s[n:m]); err == nil {
			arg, hasArg = v, true
		}
		n = m
	}
	if n < len(s) && s[n] == ' ' {
		n++
	}
	return word, arg, hasArg, n
}

// skipUnits skips the n fallback characters after \uN; a \'hh escape counts as one.
func skipUnits(s string, i, n int) int {
	for ; n > 0 && i < len(s); n-- {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '\'' {
			i += 4
			continue
		}
		if s[i] == '\\' {
			_, _, _, next := control(s, i)
			i = next
			continue
		}
		if s[i] == '{' || s[i] == '}' {
			return i
		}
		i++
	}
	return i
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// maxFieldCode bounds the instruction text read from a field.
const maxFieldCode = 4096

// fieldCode returns the instruction text of the field whose \fldinst destination
// content begins at s[from], without control words and without ignorable groups
// such as the binary \datafield Word adds to hyperlinks.
func fieldCode(s string, from int) string {
	var b strings.Builder
	depth := 1
	for i := from; i < len(s) && depth > 0 && b.Len() < maxFieldCode; {
		switch s[i] {
		case '{':
			if strings.HasPrefix(s[i+1:], `\*`) {
				g, ok := group(s, i)
				if !ok {
					return strings.TrimSpace(b.String())
				}
				i += len(g) + 2
				continue
			}
			depth++
			i++
		case '}':
			depth--
			i++
		case '\\':
			var word string
			word, _, _, i = control(s, i)
			switch word {
			case `\`, "{", "}":
				b.WriteString(word)
			case "'":
				if i+2 <= len(s) {
					if v, err := strconv.ParseUint(s[i:i+2], 16, 8); err == nil {
						b.WriteRune(charmap.Windows1252.DecodeByte(byte(v)))
					}
					i += 2
				}
			}
		case '\r', '\n':
			i++
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return strings.TrimSpace(b.String())
}

// isTOC reports whether a field instruction is the TOC field.
func isTOC(code string) bool {
	code = strings.ToUpper(code)
	return strings.HasPrefix(code, "TOC") && (len(code) == 3 || !isAlpha(code[3]))
}

// stylesheet holds what the paragraph styles say about headings: the heading
// level of styles with an \outlinelevel and the numbers of styles named Title.
// chars and paras hold the emphasis of the character (\csN) and paragraph (\sN)
// styles; their \sbasedon chain is not followed.
type stylesheet struct {
	levels map[int]int
	titles map[int]bool
	chars  map[int]mdrender.Emphasis
	paras  map[int]mdrender.Emphasis
}

// parseStylesheet reads the paragraph (\sN) and character (\csN) styles from the stylesheet.
func parseStylesheet(s string) stylesheet {
	out := stylesheet{levels: map[int]int{}, titles: map[int]bool{}, chars: map[int]mdrender.Emphasis{}, paras: map[int]mdrender.Emphasis{}}
	start := strings.Index(s, `{\stylesheet`)
	if start < 0 {
		return out
	}
	body, ok := group(s, start)
	if !ok {
		return out
	}
	eachGroup(body, func(entry string) {
		num, lvl, isPara := 0, -1, false
		csNum, cs, isChar := 0, mdrender.Emphasis{}, false
		topWords(entry, func(word string, arg int, hasArg bool) {
			on := !hasArg || arg != 0
			switch {
			case word == "cs" && hasArg:
				csNum, isChar = arg, true
			case word == "b":
				cs.Bold = on
			case word == "i":
				cs.Italic = on
			case word == "strike" || word == "striked":
				cs.Strike = on
			case word == "s" && hasArg:
				num, isPara = arg, true
			case word == "outlinelevel" && hasArg && arg >= 0 && arg <= 8:
				lvl = arg + 1
			}
		})
		if isChar {
			out.chars[csNum] = cs
		}
		if !isPara {
			return
		}
		out.paras[num] = cs
		// Word writes the built-in name even in localised documents; "Titel" is
		// kept for other producers, as in the OpenDocument reader.
		if name := strings.ToLower(styleName(entry)); name == "title" || name == "titel" {
			out.titles[num] = true
		} else if lvl > 0 {
			out.levels[num] = lvl
		}
	})
	return out
}

// styleName returns the text of a stylesheet entry outside its control words and
// subgroups, without the closing semicolon.
func styleName(entry string) string {
	var b strings.Builder
	for i := 0; i < len(entry); {
		switch entry[i] {
		case '{':
			g, ok := group(entry, i)
			if !ok {
				return strings.TrimSpace(strings.TrimSuffix(b.String(), ";"))
			}
			i += len(g) + 2
		case '\\':
			_, _, _, i = control(entry, i)
		case '\r', '\n':
			i++
		default:
			b.WriteByte(entry[i])
			i++
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(b.String()), ";"))
}

// parseLists maps each list override number (\lsN) to the number format
// (\levelnfc) of every level of the list it selects.
func parseLists(s string) map[int][]int {
	out := map[int][]int{}
	templates := map[int][]int{}
	if body, ok := destination(s, `\listtable`); ok {
		eachGroup(body, func(list string) {
			id, hasID := 0, false
			topWords(list, func(word string, arg int, hasArg bool) {
				if word == "listid" && hasArg {
					id, hasID = arg, true
				}
			})
			var levels []int
			eachGroup(list, func(level string) {
				if !strings.HasPrefix(level, `\listlevel`) {
					return
				}
				nfc := 0
				topWords(level, func(word string, arg int, hasArg bool) {
					if word == "levelnfc" && hasArg {
						nfc = arg
					}
				})
				levels = append(levels, nfc)
			})
			if hasID {
				templates[id] = levels
			}
		})
	}
	if body, ok := destination(s, `\listoverridetable`); ok {
		eachGroup(body, func(override string) {
			id, ls := 0, 0
			topWords(override, func(word string, arg int, hasArg bool) {
				switch {
				case word == "listid" && hasArg:
					id = arg
				case word == "ls" && hasArg:
					ls = arg
				}
			})
			if levels, ok := templates[id]; ok && ls > 0 {
				out[ls] = levels
			}
		})
	}
	return out
}

// parseFonts maps font numbers to the code page their \fcharset names. Fonts
// without one (charset 0 and unknown values) use the document's code page.
func parseFonts(s string) map[int]*charmap.Charmap {
	out := map[int]*charmap.Charmap{}
	body, ok := destination(s, `\fonttbl`)
	if !ok {
		return out
	}
	cur := -1
	var stack []int
	for i := 0; i < len(body); {
		switch body[i] {
		case '{':
			stack = append(stack, cur)
			i++
		case '}':
			if n := len(stack); n > 0 {
				cur, stack = stack[n-1], stack[:n-1]
			}
			i++
		case '\\':
			word, arg, hasArg, next := control(body, i)
			switch {
			case word == "f" && hasArg:
				cur = arg
			case word == "fcharset" && hasArg && cur >= 0:
				if cm, ok := charsetPages[arg]; ok {
					out[cur] = cm
				}
			}
			i = next
		default:
			i++
		}
	}
	return out
}

// destination returns the contents of the group opened by the control word name.
func destination(s, name string) (string, bool) {
	idx := strings.Index(s, name)
	if idx < 0 {
		return "", false
	}
	start := strings.LastIndex(s[:idx], "{")
	if start < 0 {
		return "", false
	}
	return group(s, start)
}

// eachGroup calls fn with the contents of every brace group directly inside body.
func eachGroup(body string, fn func(string)) {
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++
		case '{':
			g, ok := group(body, i)
			if !ok {
				return
			}
			fn(g)
			i += len(g) + 1
		}
	}
}

// topWords calls fn for every control word of body outside its subgroups.
func topWords(body string, fn func(word string, arg int, hasArg bool)) {
	for i := 0; i < len(body); {
		switch body[i] {
		case '{':
			g, ok := group(body, i)
			if !ok {
				return
			}
			i += len(g) + 2
		case '\\':
			word, arg, hasArg, next := control(body, i)
			fn(word, arg, hasArg)
			i = next
		default:
			i++
		}
	}
}

// group returns the contents of the brace group opening at s[i], honouring escapes.
func group(s string, i int) (string, bool) {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return s[i+1 : j], true
			}
		}
	}
	return "", false
}
