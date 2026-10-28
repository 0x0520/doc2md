// Package asciidoc converts AsciiDoc to Markdown structurally: section titles
// become ATX headings, tables become pipe tables, and inline markup is
// translated. It is not a full implementation: includes are not resolved and
// unrecognised text passes through unchanged.
package asciidoc

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/nullwerke/doc2md/internal/mdrender"
)

var (
	headingRe          = regexp.MustCompile(`^(={1,6})\s+(\S.*)$`)
	anchorRe           = regexp.MustCompile(`^\[\[[^\]]+\]\]$`)
	blockAttrRe        = regexp.MustCompile(`^\[[^\]]*\]$`)
	attrEntryRe        = regexp.MustCompile(`^:[A-Za-z][A-Za-z0-9_-]*!?:`)
	tableDelimRe       = regexp.MustCompile(`^\|={3,}\s*$`)
	colsAttrRe         = regexp.MustCompile(`(?i)\bcols=(?:"([^"]*)"|([^,\]]*))`)
	verbatimDelimChars = "-.*_+/"
	sectionDelimRe     = regexp.MustCompile(`^={2,}\s*$`)
	unorderedItemRe    = regexp.MustCompile(`^(\*+)\s+(\S.*)$`)
	orderedItemRe      = regexp.MustCompile(`^(\.+)\s+(\S.*)$`)
	passthroughRe      = regexp.MustCompile(`\+\+([^+\s]{1,12})\+\+`)
	hardBreakRe        = regexp.MustCompile(`(?m)\s+\+$`)
	roleSpanRe         = regexp.MustCompile(`\[\.[^\]]*\]#{1,2}([^#]*)#{1,2}`)
	boldRe             = regexp.MustCompile(`\*([^*\s](?:[^*]*[^*\s])?)\*`)
	xrefRe             = regexp.MustCompile(`<<([^>,]+?)(?:,\s*([^>]*))?>>`)
	includeRe          = regexp.MustCompile(`^include::(\S+?)\[[^\]]*\]\s*$`)
	attrRefRe          = regexp.MustCompile(`\{([a-z]+)\}`)
)

var charAttrs = map[string]string{
	"plus": "+", "nbsp": " ", "sp": " ", "zwsp": "", "wj": "",
	"startsb": "[", "endsb": "]", "vbar": "|", "amp": "&", "lt": "<", "gt": ">",
	"apos": "'", "quot": `"`, "lsquo": "‘", "rsquo": "’", "ldquo": "“", "rdquo": "”",
	"deg": "°", "cpp": "C++", "brvbar": "¦", "caret": "^", "asterisk": "*",
	"tilde": "~", "backslash": `\`, "backtick": "`", "two-colons": "::",
}

func inline(s string) string {
	s = hardBreakRe.ReplaceAllString(s, "")
	s = roleSpanRe.ReplaceAllString(s, "$1")
	// Only the doubled "++x++" form; its content is short and space-free so "C++ and
	// Java++" is left alone. The single-plus form is indistinguishable from arithmetic.
	s = passthroughRe.ReplaceAllString(s, "$1")
	s = attrRefRe.ReplaceAllStringFunc(s, func(m string) string {
		if c, ok := charAttrs[m[1:len(m)-1]]; ok {
			return c
		}
		return m
	})
	s = xrefRe.ReplaceAllStringFunc(s, func(m string) string {
		g := xrefRe.FindStringSubmatch(m)
		if text := strings.TrimSpace(g[2]); text != "" {
			return text
		}
		return strings.TrimSpace(g[1])
	})
	// AsciiDoc bold is *x*, which Markdown reads as italic; no whitespace is allowed
	// next to the delimiters so a list marker is not an opening one.
	return boldRe.ReplaceAllString(s, "**$1**")
}

// isVerbatimDelim reports whether trimmed is four or more repetitions of one
// verbatimDelimChars character (RE2 has no backreferences). "=" is excluded on
// purpose: example blocks hold ordinary text, and one unpaired delimiter must
// not disable conversion for the rest of the document (see sectionDelimRe).
func isVerbatimDelim(trimmed string) bool {
	if len(trimmed) < 4 || !strings.ContainsRune(verbatimDelimChars, rune(trimmed[0])) {
		return false
	}
	return strings.Count(trimmed, trimmed[:1]) == len(trimmed)
}

// ToMarkdown converts AsciiDoc to Markdown. It never fails; the error exists
// so callers can dispatch uniformly with the other converters.
func ToMarkdown(data []byte) (string, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	shift := headingShift(lines)

	var out []string
	inVerbatim := false
	// emit collapses blank-line runs: dropped markup and table padding would otherwise leave gaps.
	// It is never called inside a verbatim block, whose lines are appended as they are.
	emit := func(line string) {
		if strings.TrimSpace(line) == "" {
			if len(out) == 0 || strings.TrimSpace(out[len(out)-1]) == "" {
				return
			}
			out = append(out, "")
			return
		}
		out = append(out, line)
	}

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)

		if isVerbatimDelim(trimmed) {
			inVerbatim = !inVerbatim
			// A fence, not the AsciiDoc delimiter: a dash run is a thematic break in Markdown.
			out = append(out, "```")
			continue
		}
		if inVerbatim {
			out = append(out, raw)
			continue
		}
		if tableDelimRe.MatchString(trimmed) {
			cells, width, next := collectTable(lines, i)
			for _, l := range renderTable(cells, width) {
				emit(l)
			}
			i = next
			continue
		}
		if m := headingRe.FindStringSubmatch(trimmed); m != nil {
			level := min(max(len(m[1])-shift, 1), 6)
			emit(strings.Repeat("#", level) + " " + inline(strings.TrimSpace(m[2])))
			continue
		}
		if anchorRe.MatchString(trimmed) || attrEntryRe.MatchString(trimmed) ||
			strings.HasPrefix(trimmed, "//") || sectionDelimRe.MatchString(trimmed) ||
			blockAttrRe.MatchString(trimmed) {
			continue
		}
		if m := includeRe.FindStringSubmatch(trimmed); m != nil {
			emit("[" + m[1] + "](" + m[1] + ")")
			continue
		}
		if m := unorderedItemRe.FindStringSubmatch(trimmed); m != nil {
			emit(strings.Repeat("  ", len(m[1])-1) + "- " + inline(m[2]))
			continue
		}
		if m := orderedItemRe.FindStringSubmatch(trimmed); m != nil {
			emit(strings.Repeat("   ", len(m[1])-1) + "1. " + inline(m[2]))
			continue
		}
		emit(inline(raw))
	}
	return strings.TrimSpace(strings.Join(out, "\n")), nil
}

// headingShift returns how many "=" to subtract so the shallowest title becomes
// "#"; chapters extracted from a book often start at "===".
func headingShift(lines []string) int {
	shallowest := 0
	inVerbatim := false
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if isVerbatimDelim(trimmed) {
			inVerbatim = !inVerbatim
			continue
		}
		if inVerbatim {
			continue
		}
		if m := headingRe.FindStringSubmatch(trimmed); m != nil {
			if d := len(m[1]); shallowest == 0 || d < shallowest {
				shallowest = d
			}
		}
	}
	if shallowest <= 1 {
		return 0
	}
	return shallowest - 1
}

type cell struct {
	text             string
	colspan, rowspan int
}

// cellSpec is the specifier written before a cell separator. Alignment and
// style are dropped; they have no Markdown counterpart.
type cellSpec struct {
	dup, colspan, rowspan int
}

var defaultSpec = cellSpec{dup: 1, colspan: 1, rowspan: 1}

// cellSpecRe matches a whole specifier token. Every part is optional, so it
// also matches ""; parseCellSpec excludes that.
var cellSpecRe = regexp.MustCompile(`^(?:(\d+)\*)?(?:(\d*)(?:\.(\d+))?\+)?(?:[<^>]|\.[<^>]|[<^>]\.[<^>])?[aehlmdsv]?$`)

func parseCellSpec(tok string) (cellSpec, bool) {
	if tok == "" {
		return defaultSpec, false
	}
	m := cellSpecRe.FindStringSubmatch(tok)
	if m == nil {
		return defaultSpec, false
	}
	// A bare "+" is not a span; otherwise a cell ending in "C++" would lose its tail.
	if strings.Contains(tok, "+") && m[2] == "" && m[3] == "" {
		return defaultSpec, false
	}
	return cellSpec{dup: atLeastOne(m[1]), colspan: atLeastOne(m[2]), rowspan: atLeastOne(m[3])}, true
}

// collectTable returns the table's cells, its column count and the index of the
// closing delimiter. The body is one cell stream, not a row per line, because
// cell content (e.g. "a|" cells) may span lines.
func collectTable(lines []string, delim int) (cells []cell, width, next int) {
	end := len(lines)
	for i := delim + 1; i < len(lines); i++ {
		if tableDelimRe.MatchString(strings.TrimSpace(lines[i])) {
			end = i
			break
		}
	}
	body := lines[delim+1 : end]
	return splitCells(strings.Join(body, "\n")), tableWidth(lines, delim, body), end
}

// tableWidth uses the cols attribute above the table, else the separators on
// the first non-blank body line, as AsciiDoc does.
func tableWidth(lines []string, delim int, body []string) int {
	for k := delim - 1; k >= 0; k-- {
		t := strings.TrimSpace(lines[k])
		if t == "" || !(blockAttrRe.MatchString(t) || anchorRe.MatchString(t) || strings.HasPrefix(t, ".")) {
			break
		}
		if m := colsAttrRe.FindStringSubmatch(t); m != nil {
			if n := countCols(m[1] + m[2]); n > 0 {
				return n
			}
		}
	}
	for _, raw := range body {
		if t := strings.TrimSpace(raw); t != "" {
			return max(strings.Count(t, "|"), 1)
		}
	}
	return 1
}

func countCols(spec string) int {
	if strings.TrimSpace(spec) == "" {
		return 0
	}
	n := 0
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if i := strings.Index(part, "*"); i > 0 {
			if k, err := strconv.Atoi(strings.TrimSpace(part[:i])); err == nil && k > 0 {
				n += k
				continue
			}
		}
		n++
	}
	return n
}

func splitCells(body string) []cell {
	fields := splitUnescaped(body)
	if len(fields) < 2 {
		return nil
	}
	_, spec := takeCellSpec(fields[0])
	var cells []cell
	for i, f := range fields[1:] {
		// The last field is followed by no separator, so nothing in it can be a specifier.
		text, next := strings.TrimSpace(inline(f)), defaultSpec
		if i < len(fields)-2 {
			text, next = takeCellSpec(f)
		}
		for k := 0; k < spec.dup; k++ {
			cells = append(cells, cell{text: text, colspan: spec.colspan, rowspan: spec.rowspan})
		}
		spec = next
	}
	return cells
}

func splitUnescaped(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(out, cur.String())
}

// takeCellSpec splits a trailing specifier off a field; it describes the NEXT
// cell. The field is not right-trimmed: a specifier touches the separator, so
// trailing whitespace means none (a lone "a" cell is text, not a style marker).
// Only fields that a separator follows may carry one; see splitCells.
func takeCellSpec(field string) (text string, spec cellSpec) {
	field = inline(field)
	tok := field[strings.LastIndexAny(field, " \t\n")+1:]
	if s, ok := parseCellSpec(tok); ok {
		return strings.TrimSpace(field[:len(field)-len(tok)]), s
	}
	return strings.TrimSpace(field), defaultSpec
}

func atLeastOne(digits string) int {
	if n, err := strconv.Atoi(digits); err == nil && n > 0 {
		return n
	}
	return 1
}

// layout cuts cells into rows and resolves spans in one pass, since where a row
// ends depends on what a row span carries into it. Markdown has no merged
// cells: column spans leave blanks, row spans repeat the text.
func layout(cells []cell, width int) [][]string {
	if width < 1 {
		width = 1
	}
	carry := make([]carried, width)
	var out [][]string
	row := make([]string, 0, width)
	takeCarried := func() {
		for len(row) < width && carry[len(row)].left > 0 {
			c := &carry[len(row)]
			row = append(row, c.text)
			c.left--
		}
	}
	flush := func() {
		for len(row) < width {
			row = append(row, "")
		}
		out = append(out, row)
		row = make([]string, 0, width)
	}
	for _, c := range cells {
		takeCarried()
		if len(row) >= width {
			flush()
			takeCarried()
		}
		for k := 0; k < c.colspan && len(row) < width; k++ {
			text := ""
			if k == 0 {
				text = c.text
			}
			if c.rowspan > 1 {
				carry[len(row)] = carried{text: text, left: c.rowspan - 1}
			}
			row = append(row, text)
		}
	}
	takeCarried()
	if len(row) > 0 {
		flush()
	}
	for anyCarried(carry) {
		for col := 0; col < width; col++ {
			if carry[col].left > 0 {
				row = append(row, carry[col].text)
				carry[col].left--
				continue
			}
			row = append(row, "")
		}
		flush()
	}
	return out
}

type carried struct {
	text string
	left int
}

func anyCarried(carry []carried) bool {
	for _, c := range carry {
		if c.left > 0 {
			return true
		}
	}
	return false
}

// renderTable drops all-blank rows and columns (vertical spacing must not
// become the header) and promotes the first row to header, as Markdown requires one.
func renderTable(cells []cell, width int) []string {
	grid := mdrender.NormalizeGrid(layout(cells, width))
	if len(grid) == 0 {
		return nil
	}
	return []string{"", mdrender.Table(grid), ""}
}
