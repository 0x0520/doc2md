// Package rtf converts RTF documents to Markdown with a single-pass control-word
// scanner: text, headings, bullet lists, tables and embedded PNG/JPEG pictures.
package rtf

import (
	"errors"
	"strconv"
	"strings"

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
// dropped and the paragraph is emitted as a list item instead.
var markerDestinations = map[string]bool{"pntext": true, "listtext": true}

// Control words that stand for a single character.
var charWords = map[string]string{
	"lquote": "‘", "rquote": "’", "ldblquote": "“", "rdblquote": "”",
	"endash": "–", "emdash": "—", "bullet": "•",
	"enspace": " ", "emspace": " ", "qmspace": " ", "nbsp": " ",
	"tab": " ", "line": " ", "softline": " ",
}

// Windows-1252 characters for bytes 0x80-0x9F, where it differs from Latin-1.
var cp1252High = []rune{
	'€', 0x81, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0x8D, 'Ž', 0x8F,
	0x90, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0x9D, 'ž', 'Ÿ',
}

// state is the formatting state of one group; it is saved on '{' and restored on '}'.
// uc is the number of fallback characters after a \u escape, level the heading
// level of the current paragraph (0 for body text).
type state struct {
	skip  bool
	uc    int
	style int
	level int
	table bool
}

// parser accumulates blocks while walking the RTF source. Text goes to cell while
// inside a table row, otherwise to para.
type parser struct {
	styleLevel map[int]int
	blocks     []mdrender.Block
	para       strings.Builder
	row        []string
	cell       strings.Builder
	grid       [][]string
	cells      int
	inRow      bool
	listItem   bool

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
	p := &parser{styleLevel: parseStylesheet(s)}
	p.walk(s)
	p.flushParagraph(state{})
	p.flushTable()
	if len(p.blocks) == 0 {
		return "", nil, ErrNotRTF
	}
	return mdrender.Document(p.blocks), p.images, nil
}

// walk scans s once, tracking group nesting; control words are handled in apply.
func (p *parser) walk(s string) {
	st := state{uc: 1}
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
			if !st.skip {
				p.write(string(c))
			}
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
		if !st.skip {
			p.write(word)
		}
	case "~":
		if !st.skip {
			p.write(" ")
		}
	// \'hh is one byte of the document code page, taken as Windows-1252.
	case "'":
		if next+2 <= len(s) {
			if b, err := strconv.ParseUint(s[next:next+2], 16, 8); err == nil && !st.skip {
				p.writeByte(byte(b))
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
			if !st.skip {
				p.write(string(rune(r)))
			}
			return skipUnits(s, next, st.uc)
		}
	case "uc":
		if hasArg {
			st.uc = arg
		}
	// \binN is followed by N raw bytes that must not be parsed as RTF.
	case "bin":
		if hasArg && arg > 0 && next+arg <= len(s) {
			return next + arg
		}
	case "par", "sect":
		p.flushParagraph(*st)
	case "pard":
		st.level, st.style, st.table = 0, 0, false
	case "intbl", "trowd":
		st.table = true
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
			st.style = arg
			if lvl, ok := p.styleLevel[arg]; ok {
				st.level = lvl
			}
		}
	// Pictures are decoded by picture; the rest of the group (hex data) is skipped.
	case "pict":
		st.skip = true
		if end := p.picture(s, next); end > 0 {
			return end
		}
	default:
		switch {
		case markerDestinations[word]:
			st.skip = true
			p.listItem = true
		case skipDestinations[word]:
			st.skip = true
		case charWords[word] != "":
			if !st.skip {
				p.write(charWords[word])
			}
		}
	}
	return next
}

func (p *parser) write(s string) {
	if p.inRow {
		p.cell.WriteString(s)
		return
	}
	p.para.WriteString(s)
}

func (p *parser) writeByte(b byte) {
	if b >= 0x80 && b <= 0x9F {
		p.write(string(cp1252High[b-0x80]))
		return
	}
	p.write(string(rune(b)))
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
	p.row = append(p.row, squeeze(p.cell.String()))
	p.cell.Reset()
	if p.cells++; p.cells > maxCells {
		p.endRow()
	}
}

func (p *parser) endRow() {
	if p.cell.Len() > 0 {
		p.row = append(p.row, squeeze(p.cell.String()))
		p.cell.Reset()
	}
	if len(p.row) > 0 {
		p.grid = append(p.grid, p.row)
		p.row = nil
	}
	p.inRow = false
}

// flushParagraph emits the pending text as a list item, heading or paragraph. Inside
// a table row a paragraph break only separates words within the cell.
func (p *parser) flushParagraph(st state) {
	if p.inRow {
		p.cell.WriteString(" ")
		return
	}
	txt := squeeze(p.para.String())
	list := p.listItem
	p.para.Reset()
	p.listItem = false
	if txt == "" {
		return
	}
	p.flushTable()
	switch {
	case list:
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.List, Text: "- " + txt})
	case st.level > 0:
		p.blocks = append(p.blocks, mdrender.Block{Kind: mdrender.Heading, Level: st.level, Text: txt})
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

// parseStylesheet maps paragraph style numbers (\sN) to heading levels, using the
// \outlinelevel of each style definition.
func parseStylesheet(s string) map[int]int {
	out := map[int]int{}
	start := strings.Index(s, `{\stylesheet`)
	if start < 0 {
		return out
	}
	body, ok := group(s, start)
	if !ok {
		return out
	}
	for i := 0; i < len(body); i++ {
		if body[i] != '{' {
			continue
		}
		entry, ok := group(body, i)
		if !ok {
			break
		}
		i += len(entry) + 1
		num, lvl := 0, -1
		for j := 0; j < len(entry); j++ {
			if entry[j] != '\\' {
				continue
			}
			word, arg, hasArg, next := control(entry, j)
			j = next - 1
			switch {
			case word == "s" && hasArg:
				num = arg
			case word == "outlinelevel" && hasArg && arg >= 0 && arg <= 8:
				lvl = arg + 1
			}
		}
		if lvl > 0 {
			out[num] = lvl
		}
	}
	return out
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
