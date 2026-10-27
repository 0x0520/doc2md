package rtf

import (
	"strings"

	"github.com/0x0520/doc2md/internal/mdrender"
)

// hyperlinkTarget returns the address of a HYPERLINK field instruction, or "" for
// other fields, for links within the document (\l only) and for active targets.
func hyperlinkTarget(code string) string {
	args := fieldArgs(code)
	if len(args) == 0 || !strings.EqualFold(args[0], "HYPERLINK") {
		return ""
	}
	for i := 1; i < len(args); i++ {
		if !strings.HasPrefix(args[i], `\`) {
			if mdrender.ActiveScheme(args[i]) {
				return ""
			}
			return args[i]
		}
		// The switches \l, \o, \t and \* are followed by their argument.
		switch args[i] {
		case `\l`, `\o`, `\t`, `\*`:
			i++
		}
	}
	return ""
}

// fieldArgs splits a field instruction at whitespace, keeping quoted text together.
func fieldArgs(code string) []string {
	var out []string
	for i := 0; i < len(code); {
		switch c := code[i]; {
		case c == ' ' || c == '\t':
			i++
		case c == '"':
			end := strings.IndexByte(code[i+1:], '"')
			if end < 0 {
				return append(out, code[i+1:])
			}
			out = append(out, code[i+1:i+1+end])
			i += end + 2
		default:
			j := i
			for j < len(code) && code[j] != ' ' && code[j] != '\t' {
				j++
			}
			out = append(out, code[i:j])
			i = j
		}
	}
	return out
}
