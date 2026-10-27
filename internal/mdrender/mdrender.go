// Package mdrender holds the Markdown conventions shared by all converters, so
// tables and headings look the same whatever the source format.
package mdrender

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Table renders a grid as a GFM table with the first row as header. The column
// count comes from the header; short rows are padded and pipes escaped. There is no trailing newline.
func Table(grid [][]string) string {
	if len(grid) == 0 {
		return ""
	}
	ncol := len(grid[0])
	if ncol == 0 {
		return ""
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("|")
		for i := 0; i < ncol; i++ {
			c := ""
			if i < len(cells) {
				// Squeeze whitespace, including newlines in multi-line cells, so each row stays on one line.
				c = strings.ReplaceAll(strings.Join(strings.Fields(cells[i]), " "), "|", "\\|")
			}
			b.WriteString(" " + c + " |")
		}
		b.WriteString("\n")
	}
	writeRow(grid[0])
	b.WriteString("|")
	for i := 0; i < ncol; i++ {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, r := range grid[1:] {
		writeRow(r)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Kind classifies a Block for Document's spacing rules.
type Kind int

const (
	// Paragraph is any block set off by blank lines: text, a table or a fenced code block.
	Paragraph Kind = iota
	// Heading is an ATX heading; Block.Level gives its depth.
	Heading
	// List is one list item line; consecutive items stay adjacent.
	List
	// Table_ is a rendered pipe table (underscore because Table is the function).
	Table_
)

// Block is one unit of output. Level (1..6) applies to Heading only; Text is
// already-rendered inline Markdown.
type Block struct {
	Kind  Kind
	Level int
	Text  string
}

// Document joins blocks with blank lines, keeping consecutive List items on adjacent
// lines, and ends with exactly one newline. Heading levels are shifted so the
// shallowest becomes "#": a level only has meaning within one document, and
// converters pick levels without seeing the whole (one heading per sheet or slide,
// or whatever outline levels a Word author used).
func Document(blocks []Block) string {
	shift := headingShift(blocks)
	blocks = slices.Clone(blocks)
	numberLists(blocks)
	var b strings.Builder
	top := ""
	for i, blk := range blocks {
		switch blk.Kind {
		case Heading:
			// Clamp before shifting so the shift reflects the levels actually rendered.
			b.WriteString(strings.Repeat("#", clampLevel(clampLevel(blk.Level)-shift)))
			b.WriteString(" ")
			b.WriteString(blk.Text)
			b.WriteString("\n\n")
		case List:
			if indent, _, _ := listItem(blk.Text); indent == 0 {
				top = blk.Text
			}
			b.WriteString(blk.Text)
			b.WriteString("\n")
			if i+1 >= len(blocks) || blocks[i+1].Kind != List || listBreak(top, blocks[i+1].Text) {
				b.WriteString("\n")
				top = ""
			}
		default:
			b.WriteString(blk.Text)
			b.WriteString("\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// NormalizeGrid drops rows and columns that are empty throughout and pads the
// rest to a rectangle, so blank spacing rows do not become table headers.
func NormalizeGrid(grid [][]string) [][]string {
	ncol := 0
	for _, r := range grid {
		if len(r) > ncol {
			ncol = len(r)
		}
	}
	colUsed := make([]bool, ncol)
	for _, r := range grid {
		for c := 0; c < len(r); c++ {
			if strings.TrimSpace(r[c]) != "" {
				colUsed[c] = true
			}
		}
	}
	keepCol := make([]int, 0, ncol)
	for c := 0; c < ncol; c++ {
		if colUsed[c] {
			keepCol = append(keepCol, c)
		}
	}
	if len(keepCol) == 0 {
		return nil
	}
	var out [][]string
	for _, r := range grid {
		row := make([]string, len(keepCol))
		nonEmpty := false
		for i, c := range keepCol {
			if c < len(r) {
				row[i] = r[c]
				if strings.TrimSpace(r[c]) != "" {
					nonEmpty = true
				}
			}
		}
		if nonEmpty {
			out = append(out, row)
		}
	}
	return out
}

// headingShift returns how much to subtract so the shallowest heading becomes level 1.
func headingShift(blocks []Block) int {
	shallowest := 0
	for _, blk := range blocks {
		if blk.Kind != Heading {
			continue
		}
		if lvl := clampLevel(blk.Level); shallowest == 0 || lvl < shallowest {
			shallowest = lvl
		}
	}
	if shallowest <= 1 {
		return 0
	}
	return shallowest - 1
}

// clampLevel limits a heading level to 1..6.
func clampLevel(l int) int {
	if l < 1 {
		return 1
	}
	if l > 6 {
		return 6
	}
	return l
}

var orderedItemRe = regexp.MustCompile(`^( *)(\d+)([.)]) `)

// listItem splits a rendered list line into its indent width, whether it is
// ordered, and its number (0 for bullets).
func listItem(text string) (indent int, ordered bool, n int) {
	if m := orderedItemRe.FindStringSubmatch(text); m != nil {
		n, _ = strconv.Atoi(m[2])
		return len(m[1]), true, n
	}
	return len(text) - len(strings.TrimLeft(text, " ")), false, 0
}

// listBreak reports whether next starts a different top-level list than the one
// whose last top-level item is top (bullets then numbers or the reverse).
func listBreak(top, next string) bool {
	it, ot, _ := listItem(top)
	in, on, _ := listItem(next)
	return top != "" && it == 0 && in == 0 && ot != on
}

// numberLists renumbers ordered items that a reader wrote as "1." throughout,
// level by level, so the source's real numbers are only replaced when it had none.
func numberLists(blocks []Block) {
	for start := 0; start < len(blocks); {
		if blocks[start].Kind != List {
			start++
			continue
		}
		end := start
		for end < len(blocks) && blocks[end].Kind == List {
			end++
		}
		numberRun(blocks[start:end])
		start = end
	}
}

func numberRun(items []Block) {
	var groups []*[]int
	open := map[int]*[]int{}
	for i, it := range items {
		indent, ordered, _ := listItem(it.Text)
		for d := range open {
			if d > indent {
				delete(open, d)
			}
		}
		if !ordered {
			delete(open, indent)
			continue
		}
		g := open[indent]
		if g == nil {
			g = new([]int)
			open[indent] = g
			groups = append(groups, g)
		}
		*g = append(*g, i)
	}
	for _, g := range groups {
		if len(*g) < 2 {
			continue
		}
		allOnes := true
		for _, i := range *g {
			if _, _, n := listItem(items[i].Text); n != 1 {
				allOnes = false
				break
			}
		}
		if !allOnes {
			continue
		}
		for k, i := range *g {
			m := orderedItemRe.FindStringSubmatchIndex(items[i].Text)
			t := items[i].Text
			items[i].Text = t[:m[4]] + strconv.Itoa(k+1) + t[m[5]:]
		}
	}
}
