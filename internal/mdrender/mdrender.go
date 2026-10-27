package mdrender

import "strings"

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

type Kind int

const (
	Paragraph Kind = iota
	Heading
	List
	Table_
)

type Block struct {
	Kind  Kind
	Level int
	Text  string
}

func Document(blocks []Block) string {
	shift := headingShift(blocks)
	var b strings.Builder
	for i, blk := range blocks {
		switch blk.Kind {
		case Heading:
			b.WriteString(strings.Repeat("#", clampLevel(clampLevel(blk.Level)-shift)))
			b.WriteString(" ")
			b.WriteString(blk.Text)
			b.WriteString("\n\n")
		case List:
			b.WriteString(blk.Text)
			b.WriteString("\n")
			if i+1 >= len(blocks) || blocks[i+1].Kind != List {
				b.WriteString("\n")
			}
		default:
			b.WriteString(blk.Text)
			b.WriteString("\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

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

func clampLevel(l int) int {
	if l < 1 {
		return 1
	}
	if l > 6 {
		return 6
	}
	return l
}
