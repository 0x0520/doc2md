package pdfnative

import (
	"regexp"
	"strings"
	"unicode"
)

var listItemRe = regexp.MustCompile(`^\s*([-*•]|\d+[.)])\s+\S`)

type blockKind int

const (
	kindParagraph blockKind = iota
	kindHeading
	kindList
	kindTable
)

type block struct {
	kind  blockKind
	level int
	text  string
	page  int
}

func groupBlocks(pages []page, body float64, verbatim bool) []block {
	var lines []line
	for pi := range pages {
		for _, ln := range pages[pi].lines {
			if strings.TrimSpace(ln.text) == "" {
				continue
			}
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

	isHead := make([]bool, len(lines))
	headText := make([]string, len(lines))
	outlineLvl := make([]int, len(lines))
	skip := make([]bool, len(lines))
	var headSizes []float64
	for i := range lines {
		if lines[i].pre || skip[i] {
			continue
		}
		lvl, txt, ok, numbered, absorb := classifyHeading(lines, i, st, chapterSeq)
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
	levelBySize := sizeLevels(headSizes)

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
		default:
			if len(para) == 0 && lastListLine != nil && continuesListItem(*lastListLine, ln, body) {
				blocks[lastListIdx].text = joinWrapped(blocks[lastListIdx].text, text)
				cp := ln
				lastListLine = &cp
				continue
			}
			if paraPrev != nil && !sameParagraph(*paraPrev, ln, body) {
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

func sameParagraph(prev, cur line, body float64) bool {
	fs := maxF(cur.fontSize, body)
	if fs == 0 {
		fs = 10
	}
	norm := (prev.y - cur.y) / fs
	if norm < 0.35 || norm > 2.55 {
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

func continuesListItem(prev, cur line, body float64) bool {
	fs := maxF(cur.fontSize, body)
	if fs == 0 {
		fs = 10
	}
	norm := (prev.y - cur.y) / fs
	if norm < 0.35 || norm > 2.55 {
		return false
	}
	if !within(prev.fontSize, cur.fontSize, 0.12) {
		return false
	}
	return cur.xStart >= prev.xStart-0.5*fs && cur.xStart-prev.xStart <= 4*fs
}

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

func normalizeListMarker(text string) string {
	trimmed := strings.TrimLeft(text, " \t")
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

func render(blocks []block) string {
	md, _ := renderWithBreaks(blocks)
	return md
}

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
			if i+1 >= len(blocks) || blocks[i+1].kind != kindList {
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
