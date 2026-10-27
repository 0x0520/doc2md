package pdfnative

import (
	"sort"
	"strings"
	"unicode"
)

// docStats summarises the document's typography for heading scoring.
type docStats struct {
	bodySize    float64
	biggerSizes []float64
	// weightBoost is 1 if any text line is bold, else 0, so bold only counts as a signal when the document uses it.
	weightBoost float64
}

// computeStats collects the distinct font sizes above body size (rounded to half
// points) from non-table lines.
func computeStats(lines []line, body float64) docStats {
	set := map[float64]bool{}
	anyBold := false
	for _, ln := range lines {
		if ln.pre {
			continue
		}
		if s := roundHalf(ln.fontSize); s > body {
			set[s] = true
		}
		if ln.bold {
			anyBold = true
		}
	}
	bigger := make([]float64, 0, len(set))
	for s := range set {
		bigger = append(bigger, s)
	}
	sort.Float64s(bigger)
	st := docStats{bodySize: body, biggerSizes: bigger}
	if anyBold {
		st.weightBoost = 1
	}
	return st
}

// sizeBoost returns 0..1: the rank of size among the sizes above body, so the
// largest font scores 1.
func (st docStats) sizeBoost(size float64) float64 {
	if len(st.biggerSizes) == 0 || size <= st.bodySize {
		return 0
	}
	s := roundHalf(size)
	rank := 0
	for _, b := range st.biggerSizes {
		if b <= s {
			rank++
		}
	}
	return float64(rank) / float64(len(st.biggerSizes))
}

// sizeLevels assigns heading levels by size: the largest distinct size becomes level 1.
func sizeLevels(sizes []float64) map[float64]int {
	set := map[float64]bool{}
	for _, s := range sizes {
		set[roundHalf(s)] = true
	}
	uniq := make([]float64, 0, len(set))
	for s := range set {
		uniq = append(uniq, s)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(uniq)))
	m := make(map[float64]int, len(uniq))
	for i, s := range uniq {
		m[s] = clampLevel(i + 1)
	}
	return m
}

func isAllCaps(text string) bool {
	hasLetter := false
	for _, r := range text {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

func startsWithDigit(text string) bool {
	for _, r := range strings.TrimSpace(text) {
		return unicode.IsDigit(r)
	}
	return false
}

// within reports whether a and b differ by at most the fraction rel of the larger.
func within(a, b, rel float64) bool {
	m := maxF(a, b)
	if m == 0 {
		return true
	}
	return absF(a-b)/m <= rel
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
