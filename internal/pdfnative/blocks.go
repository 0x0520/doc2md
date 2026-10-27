package pdfnative

import (
	"regexp"
	"strings"
	"unicode"
)

// listItemRe matches a bullet or numbered list item with text after the marker.
var listItemRe = regexp.MustCompile(`^\s*([-*•]|\d+[.)])\s+\S`)

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
		if !ok && titles[i] {
			lvl, txt, ok = 0, strings.TrimSpace(lines[i].text), true
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
	var para []string
	var paraPrev *line
	var lastListLine *line
	lastListIdx := -1
	// listTextX is where the current item's text starts after its marker, 0 if unknown.
	listTextX := 0.0
	paraPage := 0
	flush := func() {
		if len(para) > 0 {
			blocks = append(blocks, block{kind: kindParagraph, text: joinWrappedLines(para), page: paraPage})
			para, paraPrev, paraPage = nil, nil, 0
		}
	}
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
			lastListLine, lastListIdx = nil, -1
		case isImageMarker(text):
			flush()
			blocks = append(blocks, block{kind: kindParagraph, text: text, page: ln.page})
			lastListLine, lastListIdx = nil, -1
		case isHead[i]:
			flush()
			lvl := outlineLvl[i]
			if lvl == 0 {
				if lvl = levelBySize[roundHalf(ln.fontSize)]; lvl == 0 {
					lvl = 2
				}
			}
			for _, h := range splitGluedHeading(lvl, headText[i]) {
				h.page = ln.page
				blocks = append(blocks, h)
			}
			lastListLine, lastListIdx = nil, -1
		// A dotted number alone on its line is the tail of a reference broken across lines, so it is joined to the text before it.
		case isStrandedSectionNumber(text, chapterMajors):
			switch {
			case len(para) > 0:
				para[len(para)-1] = joinWrapped(para[len(para)-1], text)
			case lastListIdx >= 0:
				blocks[lastListIdx].text = joinWrapped(blocks[lastListIdx].text, text)
			case len(blocks) > 0 && blocks[len(blocks)-1].kind == kindParagraph:
				last := &blocks[len(blocks)-1]
				last.text = joinWrapped(last.text, text)
			default:
				if len(para) == 0 {
					paraPage = ln.page
				}
				para = append(para, text)
				cp := ln
				paraPrev = &cp
			}
		case listItemRe.MatchString(text):
			flush()
			blocks = append(blocks, block{kind: kindList, text: normalizeListMarker(text), page: ln.page})
			cp := ln
			lastListLine, lastListIdx = &cp, len(blocks)-1
			listTextX = 0
			if ln.word2X > ln.xStart {
				listTextX = ln.word2X
			}
		default:
			if len(para) == 0 && lastListLine != nil && continuesListItem(*lastListLine, ln, body, listTextX, pitches) {
				blocks[lastListIdx].text = joinWrapped(blocks[lastListIdx].text, text)
				cp := ln
				lastListLine = &cp
				continue
			}
			if paraPrev != nil && !sameParagraph(*paraPrev, ln, body, pitches) {
				flush()
			}
			if len(para) == 0 {
				paraPage = ln.page
			}
			para = append(para, text)
			cp := ln
			paraPrev = &cp
			lastListLine, lastListIdx = nil, -1
		}
	}
	flush()
	return mergeAdjacentTables(blocks)
}

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
	norm := (prev.y - cur.y) / fs
	if norm < 0.35 || norm > 2.55 || paragraphSpaced(prev, cur, pitches) {
		return false
	}
	if !within(prev.fontSize, cur.fontSize, 0.12) {
		return false
	}
	if absF(prev.xStart-cur.xStart) <= 1.5*fs {
		return true
	}
	return hangingNumRe.MatchString(prev.text) && absF(prev.xStart-cur.xStart) <= 4*fs
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

// joinWrappedLines joins wrapped lines into one paragraph, repairing hyphenation.
func joinWrappedLines(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out = joinWrapped(out, p)
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

// normalizeListMarker keeps a number marker ("1.", "2)") as an ordered list item and
// rewrites any leading bullet glyph to a Markdown "- " marker.
func normalizeListMarker(text string) string {
	trimmed := strings.TrimLeft(text, " \t")
	if m := orderedItemRe.FindStringSubmatch(trimmed); m != nil {
		return m[1] + " " + trimmed[len(m[0]):]
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
// each source page begins. Consecutive list items are separated by a single newline,
// unless they switch between bullets and numbers: a "2." right under a bullet item
// would be read as a lazy continuation of that item, not as a new list.
func renderWithBreaks(blocks []block) (string, []PageBreak) {
	var b strings.Builder
	var breaks []PageBreak
	lastPage := 0
	for i, blk := range blocks {
		if blk.page > 0 && blk.page != lastPage {
			breaks = append(breaks, PageBreak{Offset: b.Len(), Page: blk.page})
			lastPage = blk.page
		}
		switch blk.kind {
		case kindHeading:
			b.WriteString(strings.Repeat("#", clampLevel(blk.level)))
			b.WriteString(" ")
			b.WriteString(blk.text)
			b.WriteString("\n\n")
		case kindList:
			b.WriteString(blk.text)
			b.WriteString("\n")
			if i+1 >= len(blocks) || blocks[i+1].kind != kindList || isOrderedItem(blocks[i+1].text) != isOrderedItem(blk.text) {
				b.WriteString("\n")
			}
		default:
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

func isOrderedItem(text string) bool { return orderedItemRe.MatchString(text) }
