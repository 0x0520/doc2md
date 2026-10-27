package pdfnative

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	outlineHeadingRe = regexp.MustCompile(`^((?:[A-Za-z]\.)?\d+(?:\.\d+){0,3})([.):]?)\s+(\p{Lu}.*)$`)
	labelHeadingRe   = regexp.MustCompile(`^(?:§\s*|\p{L}{2,15}\.?\s+)(\d+[a-z]?|\p{Lu}\d*)\b`)
)

func isLabelHeading(text string) bool {
	m := labelHeadingRe.FindStringSubmatchIndex(text)
	if m == nil {
		return false
	}
	rest := text[m[3]:]
	return !(len(rest) >= 2 && (rest[0] == '.' || rest[0] == ',') && rest[1] >= '0' && rest[1] <= '9')
}

func classifyHeading(lines []line, i int, st docStats, chapterSeq map[int]bool) (level int, text string, ok bool, numbered bool, absorb int) {
	ln := lines[i]
	text = strings.TrimSpace(ln.text)
	if len(text) < 3 || len(text) > 200 {
		return 0, "", false, false, 0
	}
	prominent := ln.fontSize > st.bodySize+0.05*st.bodySize

	if bare := stripBullet(text); bare != text {
		if m := outlineHeadingRe.FindStringSubmatch(bare); m != nil {
			if number, title := m[1], strings.TrimSpace(m[3]); strings.Contains(number, ".") && len(title) <= maxBulletedHeadingLen {
				return clampLevel(strings.Count(number, ".") + 1), bare, true, true, 0
			}
		}
	}

	if m := outlineHeadingRe.FindStringSubmatch(text); m != nil {
		number, punct, title := m[1], m[2], strings.TrimSpace(m[3])
		dotted := strings.Contains(number, ".")
		switch {
		case dotted:
			return clampLevel(strings.Count(number, ".") + 1), text, true, true, 0
		case !strings.HasSuffix(title, ".") && (prominent || (punct == "" && ln.bold)):
			return 1, text, true, true, 0
		case chapterSeq[i]:
			return 1, text, true, true, 0
		}
		return 0, "", false, false, 0
	}

	labelled := isLabelHeading(text)
	if labelled {
		if lvl, txt, abs, ok2 := labelledHeading(lines, i, text); ok2 {
			return lvl, txt, true, true, abs
		}
	}

	if !prominent && (atTable(lines, i-1) || atTable(lines, i+1)) {
		return 0, "", false, false, 0
	}

	var prev, next *line
	for k := i - 1; k >= 0; k-- {
		if !lines[k].pre {
			prev = &lines[k]
			break
		}
	}
	for k := i + 1; k < len(lines); k++ {
		if !lines[k].pre {
			next = &lines[k]
			break
		}
	}
	score := neighborCombined(ln, prev, next) + st.sizeBoost(ln.fontSize)*0.5
	if ln.bold {
		score += st.weightBoost * 0.3
	}
	if startsWithDigit(text) {
		score += 0.15
	}
	if labelled && !strings.HasSuffix(text, ".") {
		score += 0.2
	}
	if score > 0.75 {
		return 0, text, true, false, 0
	}
	return 0, "", false, false, 0
}

const maxChapterOpeningLen = 80

const maxBulletedHeadingLen = 120

func chapterSequence(lines []line, st docStats) map[int]bool {
	promote := map[int]bool{}
	chapter := 0
	for i := range lines {
		if lines[i].pre {
			continue
		}
		text := strings.TrimSpace(lines[i].text)
		m := outlineHeadingRe.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		number, punct, title := m[1], m[2], strings.TrimSpace(m[3])
		major, err := strconv.Atoi(strings.SplitN(number, ".", 2)[0])
		if err != nil {
			continue
		}
		prominent := lines[i].fontSize > st.bodySize+0.05*st.bodySize
		promoted := strings.Contains(number, ".") ||
			(!strings.HasSuffix(title, ".") && (prominent || (punct == "" && lines[i].bold)))
		if promoted {
			if major > chapter {
				chapter = major
			}
			continue
		}
		if punct != "" || strings.HasSuffix(title, ".") || len(text) > maxChapterOpeningLen {
			continue
		}
		if major == chapter+1 {
			chapter = major
			promote[i] = true
		}
	}
	return promote
}

var (
	sectionNumberOnlyRe = regexp.MustCompile(`^\d+(?:\.\d+)+\.?$`)
	gluedOutlineRe      = regexp.MustCompile(`\s(\d+(?:\.\d+)+)\s+\p{Lu}`)
)

func isStrandedSectionNumber(text string, chapterMajors map[string]bool) bool {
	if !sectionNumberOnlyRe.MatchString(text) {
		return false
	}
	return chapterMajors[strings.SplitN(text, ".", 2)[0]]
}

func splitGluedHeading(level int, title string) []block {
	whole := []block{{kind: kindHeading, level: level, text: title}}
	m := outlineHeadingRe.FindStringSubmatch(title)
	if m == nil {
		return whole
	}
	major := strings.SplitN(m[1], ".", 2)[0]

	var cuts []int
	for _, loc := range gluedOutlineRe.FindAllStringSubmatchIndex(title, -1) {
		if strings.SplitN(title[loc[2]:loc[3]], ".", 2)[0] == major {
			cuts = append(cuts, loc[2])
		}
	}
	if len(cuts) == 0 {
		return whole
	}

	bounds := append([]int{0}, cuts...)
	out := make([]block, 0, len(bounds))
	for i, start := range bounds {
		end := len(title)
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		part := strings.TrimSpace(title[start:end])
		if part == "" {
			continue
		}
		lvl := level
		if pm := outlineHeadingRe.FindStringSubmatch(part); pm != nil {
			lvl = clampLevel(strings.Count(pm[1], ".") + 1)
		}
		out = append(out, block{kind: kindHeading, level: lvl, text: part})
	}
	if len(out) == 0 {
		return whole
	}
	return out
}

func stripBullet(text string) string {
	for _, m := range []string{"•", "*", "-", "+", "–", "—"} {
		if rest, ok := strings.CutPrefix(text, m); ok {
			if trimmed := strings.TrimLeft(rest, " \t"); trimmed != rest {
				return trimmed
			}
		}
	}
	return text
}

func atTable(lines []line, k int) bool {
	return k >= 0 && k < len(lines) && lines[k].pre
}

func labelledHeading(lines []line, i int, text string) (level int, txt string, absorb int, ok bool) {
	next := func(k int) string {
		if k < len(lines) && !lines[k].pre {
			if t := strings.TrimSpace(lines[k].text); !isImageMarker(t) {
				return t
			}
		}
		return ""
	}
	bracketed := func(s string) bool { return strings.Contains(s, "[") }
	closed := func(s string) bool { return strings.HasSuffix(s, "]") }

	switch {
	case bracketed(text) && closed(text):
		return 3, text, 0, true
	case bracketed(text):
		if n1 := next(i + 1); n1 != "" && closed(n1) && len(n1) < 120 {
			return 3, text + " " + n1, 1, true
		}
	case bareLabelRe.MatchString(text):
		n1 := next(i + 1)
		switch {
		case strings.HasPrefix(n1, "[") && closed(n1):
			return 3, text + " " + n1, 1, true
		case strings.HasPrefix(n1, "["):
			if n2 := next(i + 2); n2 != "" && closed(n2) && len(n2) < 120 {
				return 3, text + " " + n1 + " " + n2, 2, true
			}
		case hangingNumRe.MatchString(n1):
			return 3, text, 0, true
		}
	}
	return 0, "", 0, false
}

var bareLabelRe = regexp.MustCompile(`^(?:§\s*|\p{L}{2,15}\.?\s+)\d+[a-z]?$`)

func neighborCombined(cur line, prev, next *line) float64 {
	switch {
	case prev != nil && next != nil:
		return minF(neighborScore(cur, *prev), neighborScore(cur, *next))
	case prev != nil:
		return neighborScore(cur, *prev)
	case next != nil:
		return neighborScore(cur, *next)
	}
	return 0
}

func neighborScore(cur, nb line) float64 {
	fs := maxF(cur.fontSize, nb.fontSize)
	if fs == 0 {
		fs = 10
	}
	eps := 0.05 * fs
	s := 0.0
	if cur.fontSize > nb.fontSize+eps {
		s += 0.5
	}
	if cur.fontSize > 1.5*nb.fontSize {
		s += 0.1
	}
	if nb.fontSize > cur.fontSize+eps {
		s -= 0.3
	}
	if cur.bold && !nb.bold {
		s += 0.4
	}
	if nb.bold && !cur.bold {
		s -= 0.15
	}
	if isAllCaps(cur.text) && !isAllCaps(nb.text) {
		s += 0.25
	}
	if absF(cur.y-nb.y)-fs > 0.5*fs {
		s += 0.2
	}
	return s
}

func clampLevel(l int) int {
	if l < 1 {
		return 1
	}
	if l > 6 {
		return 6
	}
	return l
}
