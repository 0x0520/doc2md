package pdfnative

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0x0520/doc2md/internal/mdrender"
)

// listItemRe matches a bullet or numbered list item with text after the marker. A
// lowercase letter or roman numeral ("a)", "iv.") marks a nested outline level;
// capitals are left out because "A. Smith" opens prose as often as a list.
var listItemRe = regexp.MustCompile(`^\s*([-*•]|\d+[.)]|[a-z][.)]|[ivx]{2,4}[.)])\s+\S`)

// abbrevRe matches a line opened by an abbreviation of single letters ("z. B.", "d. h."),
// which has the shape of a letter marker.
var abbrevRe = regexp.MustCompile(`^\s*\p{L}\.\s*\p{L}{1,2}\.`)

// isListItem reports whether text opens a list item.
func isListItem(text string) bool {
	return listItemRe.MatchString(text) && !abbrevRe.MatchString(text)
}

type blockKind int

const (
	kindParagraph blockKind = iota
	kindHeading
	kindList
	kindTable
)

// block is one Markdown element; level is set for headings, page is the source page of its first line.
type block struct {
	kind  blockKind
	level int
	text  string
	page  int
}

// groupBlocks turns reading-ordered lines into headings, lists, tables and
// paragraphs. Headings are classified document-wide first because chapter
// numbering and size ranks are only known globally.
func groupBlocks(pages []page, body float64, verbatim bool) []block {
	var lines []line
	for pi := range pages {
		for _, ln := range pages[pi].lines {
			if strings.TrimSpace(ln.text) == "" {
				continue
			}
			// Dot-leader entries are leftover contents lines that restate headings.
			if !verbatim && !ln.pre && isTOCLeader(ln.text) {
				continue
			}
			lines = append(lines, ln)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	st := computeStats(lines, body)
	chapterSeq := chapterSequence(lines, st)
	titles := pageTitles(lines, st)
	pitches := linePitches(lines)

	isHead := make([]bool, len(lines))
	headText := make([]string, len(lines))
	outlineLvl := make([]int, len(lines))
	// skip marks lines absorbed into the preceding multi-line heading.
	skip := make([]bool, len(lines))
	var headSizes []float64
	for i := range lines {
		if lines[i].pre || skip[i] {
			continue
		}
		lvl, txt, ok, numbered, absorb := classifyHeading(lines, i, st, chapterSeq)
		if extra, title := titles[i]; title && (!ok || absorb == 0) {
			if !ok {
				lvl, txt, ok = 0, strings.TrimSpace(lines[i].text), true
			}
			for k := 1; k <= extra; k++ {
				txt += " " + strings.TrimSpace(lines[i+k].text)
			}
			absorb = extra
		}
		if !ok {
			continue
		}
		isHead[i], headText[i] = true, txt
		for k := i + 1; k <= i+absorb && k < len(lines); k++ {
			skip[k] = true
		}
		if numbered {
			outlineLvl[i] = lvl
		} else {
			headSizes = append(headSizes, roundHalf(lines[i].fontSize))
		}
	}
	// Numbered headings take their level from the numbering; the rest are ranked by font size.
	levelBySize := sizeLevels(headSizes)
	shift := titleShift(lines, outlineLvl, headSizes)

	// chapterMajors holds the top-level numbers of the chapters found, to recognise a section number left alone on a line.
	chapterMajors := map[string]bool{}
	for i := range lines {
		if outlineLvl[i] == 0 {
			continue
		}
		if m := outlineHeadingRe.FindStringSubmatch(strings.TrimSpace(stripBullet(headText[i]))); m != nil {
			chapterMajors[strings.SplitN(m[1], ".", 2)[0]] = true
		}
	}

	var blocks []block
	var para inlineText
	var paraPrev *line
	paraPage := 0
	flush := func() {
		if para.lines > 0 {
			blocks = append(blocks, block{kind: kindParagraph, text: para.String(), page: paraPage})
			para, paraPrev, paraPage = inlineText{}, nil, 0
		}
	}

	// The current list: its last line, the block of the open item, where the item's
	// text starts after its marker (0 if unknown), the item's text and marker, and the
	// marker positions of the enclosing levels.
	var lastListLine *line
	lastListIdx := -1
	listTextX := 0.0
	var item inlineText
	itemPrefix := ""
	var listXs []float64
	endList := func() { lastListLine, lastListIdx, listXs = nil, -1, nil }
	setItem := func() { blocks[lastListIdx].text = itemPrefix + item.String() }

	for i := range lines {
		if skip[i] {
			continue
		}
		ln := lines[i]
		text := strings.TrimSpace(ln.text)
		switch {
		case ln.pre:
			flush()
			blocks = append(blocks, block{kind: kindTable, text: ln.text, page: ln.page})
			endList()
		case isImageMarker(text):
			flush()
			blocks = append(blocks, block{kind: kindParagraph, text: text, page: ln.page})
			endList()
		case isHead[i]:
			flush()
			lvl := outlineLvl[i]
			if lvl > 0 {
				lvl += shift
			} else if lvl = levelBySize[roundHalf(ln.fontSize)]; lvl == 0 {
				lvl = 2
			}
			for _, h := range splitGluedHeading(lvl, shift, headText[i]) {
				h.page = ln.page
				blocks = append(blocks, h)
			}
			endList()
		// A dotted number alone on its line is the tail of a reference broken across lines, so it is joined to the text before it.
		case isStrandedSectionNumber(text, chapterMajors):
			switch {
			case para.lines > 0:
				para.addPlain(text)
			case lastListIdx >= 0:
				item.addPlain(text)
				setItem()
			case len(blocks) > 0 && blocks[len(blocks)-1].kind == kindParagraph:
				last := &blocks[len(blocks)-1]
				last.text = joinWrapped(last.text, text)
			default:
				paraPage = ln.page
				para.add(ln)
				cp := ln
				paraPrev = &cp
			}
		case isListItem(text):
			flush()
			var bodyLine line
			itemPrefix, bodyLine = splitListItem(ln, nestLevel(&listXs, ln.xStart, 0.5*maxF(ln.fontSize, body)))
			item = inlineText{}
			item.add(bodyLine)
			blocks = append(blocks, block{kind: kindList, page: ln.page})
			cp := ln
			lastListLine, lastListIdx = &cp, len(blocks)-1
			setItem()
			listTextX = 0
			if ln.word2X > ln.xStart {
				listTextX = ln.word2X
			}
		default:
			if para.lines == 0 && lastListLine != nil && continuesListItem(*lastListLine, ln, body, listTextX, pitches) {
				item.add(ln)
				setItem()
				cp := ln
				lastListLine = &cp
				continue
			}
			if paraPrev != nil && !sameParagraph(*paraPrev, ln, body, pitches) {
				flush()
			}
			if para.lines == 0 {
				paraPage = ln.page
			}
			para.add(ln)
			cp := ln
			paraPrev = &cp
			endList()
		}
	}
	flush()
	return mergeAdjacentTables(blocks)
}

// nestLevel places a list marker at x among the marker positions of the open levels
// (outermost first) and returns its depth: a marker clearly right of the innermost
// opens a level, one left of it closes levels down to the one it aligns with.
func nestLevel(xs *[]float64, x, tol float64) int {
	s := *xs
	for len(s) > 0 && s[len(s)-1] > x+tol {
		s = s[:len(s)-1]
	}
	if len(s) == 0 || x > s[len(s)-1]+tol {
		s = append(s, x)
	} else {
		s[len(s)-1] = x
	}
	*xs = s
	return len(s) - 1
}

// splitListItem returns the Markdown prefix of a list item (two spaces per nesting
// level, then the marker) and the line with only the item's own text. The marked-up
// forms are kept only when they start with the same marker as the plain text.
func splitListItem(ln line, level int) (string, line) {
	plain := strings.TrimSpace(ln.text)
	norm := normalizeListMarker(plain)
	marker, rest, _ := strings.Cut(norm, " ")
	raw := plain[:len(plain)-len(rest)]
	body := ln
	body.text = rest
	cut := func(s string) string {
		if r, ok := strings.CutPrefix(s, raw); ok {
			return strings.TrimSpace(r)
		}
		return ""
	}
	body.rich, body.linked = cut(ln.rich), cut(ln.linked)
	return strings.Repeat("  ", level) + marker + " ", body
}

// titleShift returns how many levels numbered headings move down below unnumbered
// headings that are clearly larger than every top-level numbered one: a document
// title above "1 Introduction". The title sizes take the top ranks of sizeLevels.
func titleShift(lines []line, outlineLvl []int, headSizes []float64) int {
	top, topSize := 0, 0.0
	for i, lvl := range outlineLvl {
		switch {
		case lvl == 0:
		case top == 0 || lvl < top:
			top, topSize = lvl, lines[i].fontSize
		case lvl == top:
			topSize = maxF(topSize, lines[i].fontSize)
		}
	}
	if top == 0 {
		return 0
	}
	larger := map[float64]bool{}
	for _, s := range headSizes {
		if s > titleSizeFactor*topSize {
			larger[s] = true
		}
	}
	return max(0, len(larger)-(top-1))
}

// titleSizeFactor is how much larger than the top numbered headings an unnumbered
// heading must be to rank above them; similar sizes are a design choice, not a level.
const titleSizeFactor = 1.15

// mergeAdjacentTables joins consecutive tables with identical header rows, i.e. one
// table that continued over a page break.
func mergeAdjacentTables(blocks []block) []block {
	out := make([]block, 0, len(blocks))
	for _, b := range blocks {
		if b.kind == kindTable && len(out) > 0 && out[len(out)-1].kind == kindTable {
			if merged, ok := joinTableMarkdown(out[len(out)-1].text, b.text); ok {
				out[len(out)-1].text = merged
				continue
			}
		}
		out = append(out, b)
	}
	return out
}

func joinTableMarkdown(a, b string) (string, bool) {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(al) < 2 || len(bl) < 2 {
		return "", false
	}
	if strings.TrimSpace(al[0]) != strings.TrimSpace(bl[0]) || strings.TrimSpace(al[1]) != strings.TrimSpace(bl[1]) {
		return "", false
	}
	body := bl[2:]
	if len(body) == 0 {
		return a, true
	}
	return a + "\n" + strings.Join(body, "\n"), true
}

// sameParagraph reports whether cur continues the paragraph of prev, judged by line
// pitch (in font sizes and against the usual pitch of the size), font size and left
// indent. A hanging "(1)" number allows a deeper indent.
func sameParagraph(prev, cur line, body float64, pitches map[float64]float64) bool {
	fs := maxF(cur.fontSize, body)
	if fs == 0 {
		fs = 10
	}
	if !within(prev.fontSize, cur.fontSize, 0.12) {
		return false
	}
	if nextColumn(prev, cur, fs) {
		return true
	}
	norm := (prev.y - cur.y) / fs
	if norm < 0.35 || norm > 2.55 || paragraphSpaced(prev, cur, pitches) {
		return false
	}
	if absF(prev.xStart-cur.xStart) <= 1.5*fs {
		return true
	}
	return hangingNumRe.MatchString(prev.text) && absF(prev.xStart-cur.xStart) <= 4*fs
}

// nextColumn reports whether cur continues prev's paragraph at the top of the next
// column: higher up on the same page and well to the right, after a line that ends
// without a full stop or before one that starts in lowercase.
func nextColumn(prev, cur line, fs float64) bool {
	if prev.page != cur.page || cur.y <= prev.y+fs || cur.xStart < prev.xStart+colMinWidthFS*fs/2 {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(strings.TrimSpace(prev.text))
	first, _ := utf8.DecodeRuneInString(strings.TrimSpace(cur.text))
	return !strings.ContainsRune(".!?:", last) || unicode.IsLower(first)
}

var hangingNumRe = regexp.MustCompile(`^\(\d{1,3}[a-z]?\)\s`)

// continuesListItem reports whether cur is the wrapped continuation of a list item.
// The indent may not be left of the item text: textX where the text after the marker
// starts (hanging indent), else the previous line's left edge.
func continuesListItem(prev, cur line, body, textX float64, pitches map[float64]float64) bool {
	fs := maxF(cur.fontSize, body)
	if fs == 0 {
		fs = 10
	}
	norm := (prev.y - cur.y) / fs
	if norm < 0.35 || norm > 2.55 || paragraphSpaced(prev, cur, pitches) {
		return false
	}
	if !within(prev.fontSize, cur.fontSize, 0.12) {
		return false
	}
	left := prev.xStart
	if textX > 0 {
		left = textX
	}
	return cur.xStart >= left-0.5*fs && cur.xStart-prev.xStart <= 4*fs
}

// paraGapFactor is the baseline gap, in multiples of the usual line pitch, from which
// lines are set apart by paragraph spacing. Word's default 8 pt after 12 pt text is
// about 1.45; taller lines from inline formulas or superscripts stay below 1.2.
const paraGapFactor = 1.3

// paragraphSpaced reports whether cur sits a paragraph gap below prev, measured
// against the usual pitch of the size. Paragraph spacing is usually well under one
// line, so a fixed limit in font sizes cannot see it.
func paragraphSpaced(prev, cur line, pitches map[float64]float64) bool {
	size := roundHalf(cur.fontSize)
	if roundHalf(prev.fontSize) != size || prev.page != cur.page {
		return false
	}
	p := pitches[size]
	return p > 0 && prev.y-cur.y > paraGapFactor*p
}

// linePitches returns the usual baseline distance per font size (to 0.5 pt): the most
// frequent gap between consecutive lines of that size on a page, ties going to the
// smaller gap, so paragraphs of two lines still yield the line pitch.
func linePitches(lines []line) map[float64]float64 {
	counts := map[float64]map[float64]int{}
	for i := 1; i < len(lines); i++ {
		prev, cur := lines[i-1], lines[i]
		size := roundHalf(cur.fontSize)
		if prev.pre || cur.pre || prev.page != cur.page || size == 0 || roundHalf(prev.fontSize) != size {
			continue
		}
		gap := prev.y - cur.y
		if gap < 0.35*size || gap > 2.55*size {
			continue
		}
		if counts[size] == nil {
			counts[size] = map[float64]int{}
		}
		counts[size][roundHalf(gap)]++
	}
	out := make(map[float64]float64, len(counts))
	for size, c := range counts {
		out[size] = modeByWeight(c)
	}
	return out
}

// joinWrapped appends next to acc: a soft hyphen is removed, and a hard hyphen only
// before a lowercase letter (before a capital the word is a compound and keeps it).
func joinWrapped(acc, next string) string {
	if acc == "" {
		return next
	}
	if next == "" {
		return acc
	}
	ar := []rune(acc)
	lr := ar[len(ar)-1]
	nr := []rune(next)[0]
	if lr == '­' && (unicode.IsLetter(nr) || unicode.IsDigit(nr)) {
		return string(ar[:len(ar)-1]) + next
	}
	if (lr == '-' || lr == '‐') && (unicode.IsLetter(nr) || unicode.IsDigit(nr)) {
		if unicode.IsLower(nr) {
			return string(ar[:len(ar)-1]) + next
		}
		return acc + next
	}
	return acc + " " + next
}

// orderedItemRe matches a number marker that Markdown accepts for an ordered list (at most nine digits).
var orderedItemRe = regexp.MustCompile(`^(\d{1,9}[.)])\s+`)

// letterItemRe matches the letter and roman markers of listItemRe.
var letterItemRe = regexp.MustCompile(`^(?:[a-z]|[ivx]{2,4})[.)]\s+`)

// normalizeListMarker keeps a number marker ("1.", "2)") as an ordered list item, writes
// a letter or roman marker as "1." and rewrites any leading bullet glyph to a Markdown
// "- " marker.
func normalizeListMarker(text string) string {
	trimmed := strings.TrimLeft(text, " \t")
	if m := orderedItemRe.FindStringSubmatch(trimmed); m != nil {
		// "1)" becomes "1.": CommonMark starts a new list when the delimiter changes.
		number := m[1][:len(m[1])-1]
		return number + ". " + trimmed[len(m[0]):]
	}
	// Markdown numbers lists with digits only; the nesting carries the outline level and
	// mdrender numbers a run of "1." items.
	if m := letterItemRe.FindString(trimmed); m != "" {
		return "1. " + trimmed[len(m):]
	}
	for {
		stripped := false
		for _, m := range []string{"•", "*", "-", "+"} {
			if strings.HasPrefix(trimmed, m) {
				trimmed = strings.TrimSpace(trimmed[len(m):])
				stripped = true
				break
			}
		}
		if !stripped {
			return "- " + trimmed
		}
	}
}

// renderWithBreaks serialises blocks to Markdown and records the byte offset where
// each source page begins. A run of list items goes through mdrender.Document, which
// keeps the items on adjacent lines, separates bullets from numbers and indents
// nested items to their parent's content column as CommonMark requires.
func renderWithBreaks(blocks []block) (string, []PageBreak) {
	var b strings.Builder
	var breaks []PageBreak
	lastPage := 0
	mark := func(page, offset int) {
		if page > 0 && page != lastPage {
			breaks = append(breaks, PageBreak{Offset: offset, Page: page})
			lastPage = page
		}
	}
	for i := 0; i < len(blocks); i++ {
		blk := blocks[i]
		switch blk.kind {
		case kindHeading:
			mark(blk.page, b.Len())
			b.WriteString(strings.Repeat("#", clampLevel(blk.level)))
			b.WriteString(" ")
			b.WriteString(blk.text)
			b.WriteString("\n\n")
		case kindList:
			j := i
			var run []mdrender.Block
			for ; j < len(blocks) && blocks[j].kind == kindList; j++ {
				run = append(run, mdrender.Block{Kind: mdrender.List, Text: blocks[j].text})
			}
			// Document writes one line per item, with blank lines only between lists.
			k := i
			for _, l := range strings.SplitAfter(mdrender.Document(run), "\n") {
				if strings.TrimSpace(l) != "" && k < j {
					mark(blocks[k].page, b.Len())
					k++
				}
				b.WriteString(l)
			}
			b.WriteString("\n")
			i = j - 1
		default:
			mark(blk.page, b.Len())
			b.WriteString(blk.text)
			b.WriteString("\n\n")
		}
	}
	md := strings.TrimRight(b.String(), "\n") + "\n"
	for i := range breaks {
		if breaks[i].Offset > len(md) {
			breaks[i].Offset = len(md)
		}
	}
	return md, breaks
}
