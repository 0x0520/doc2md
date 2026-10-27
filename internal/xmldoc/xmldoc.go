package xmldoc

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/0x0520/doc2md/internal/mdrender"
)

const (
	maxNodes     = 200_000
	maxDepth     = 64
	minTableRows = 2
)

var ErrNoContent = errors.New("no content in XML document")

type node struct {
	name  string
	attrs [][2]string
	text  string
	kids  []*node
	parts []part
}

type part struct {
	text string
	kid  *node
}

func (n *node) mixed() bool {
	if len(n.kids) == 0 || n.text == "" {
		return false
	}
	for i, p := range n.parts {
		if p.kid == nil && i < len(n.parts)-1 {
			return true
		}
		if p.kid != nil && i > 0 && n.parts[i-1].kid == nil {
			return true
		}
	}
	return false
}

func (n *node) flatten() string {
	var b strings.Builder
	for _, p := range n.parts {
		s := p.text
		if p.kid != nil {
			s = p.kid.flatten()
			if s == "" {
				s = p.kid.value()
			}
		}
		if s == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(s)
	}
	return b.String()
}

func (n *node) leaf() bool { return len(n.kids) == 0 }

func (n *node) fields() [][2]string {
	out := make([][2]string, 0, len(n.attrs)+len(n.kids))
	out = append(out, n.attrs...)
	if n.text != "" {
		out = append(out, [2]string{n.name, n.text})
	}
	for _, k := range n.kids {
		if v := k.value(); k.leaf() && v != "" {
			out = append(out, [2]string{k.name, v})
		}
	}
	return out
}

func (n *node) value() string {
	parts := make([]string, 0, len(n.attrs)+1)
	if n.text != "" {
		parts = append(parts, n.text)
	}
	for _, a := range n.attrs {
		parts = append(parts, a[0]+"="+a[1])
	}
	return strings.Join(parts, " ")
}

func ToMarkdown(data []byte) (string, error) {
	root, err := parse(data)
	if err != nil {
		return "", err
	}
	if root == nil {
		return "", fmt.Errorf("%w: no root element", ErrNoContent)
	}
	var out []string
	render(root, 1, &out)
	md := strings.TrimSpace(strings.Join(out, "\n"))
	if md == "" {
		return "", ErrNoContent
	}
	return md, nil
}

func parse(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var root *node
	var stack []*node
	nodes := 0

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if root != nil {
				break
			}
			return nil, fmt.Errorf("parse XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if nodes >= maxNodes || len(stack) >= maxDepth {
				if err := dec.Skip(); err != nil {
					return root, nil
				}
				continue
			}
			nodes++
			n := &node{name: t.Name.Local}
			for _, a := range t.Attr {
				if v := collapse(a.Value); v != "" && a.Name.Local != "xmlns" && a.Name.Space != "xmlns" {
					n.attrs = append(n.attrs, [2]string{a.Name.Local, v})
				}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, n)
				parent.parts = append(parent.parts, part{kid: n})
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			if s := collapse(string(t)); s != "" {
				cur := stack[len(stack)-1]
				if cur.text == "" {
					cur.text = s
				} else {
					cur.text += " " + s
				}
				cur.parts = append(cur.parts, part{text: s})
			}
		}
	}
	return root, nil
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func render(n *node, level int, out *[]string) {
	if n.mixed() {
		if s := n.flatten(); s != "" {
			*out = append(*out, s, "")
		}
		return
	}
	if n.text == "" && len(n.attrs) == 0 && len(n.kids) == 0 {
		return
	}
	*out = append(*out, strings.Repeat("#", min(level, 6))+" "+n.name, "")

	if fields := ownFields(n); len(fields) > 0 {
		for _, f := range fields {
			*out = append(*out, "**"+f[0]+":** "+f[1])
		}
		*out = append(*out, "")
	}

	for i := 0; i < len(n.kids); {
		run := recordRun(n.kids, i)
		if run >= minTableRows {
			if tbl := recordTable(n.kids[i : i+run]); tbl != "" {
				*out = append(*out, tbl, "")
				i += run
				continue
			}
		}
		k := n.kids[i]
		i++
		if k.leaf() {
			continue
		}
		render(k, level+1, out)
	}
}

func ownFields(n *node) [][2]string {
	out := append([][2]string(nil), n.attrs...)
	if n.text != "" {
		out = append(out, [2]string{"text", n.text})
	}
	for i := 0; i < len(n.kids); {
		if run := recordRun(n.kids, i); run >= minTableRows {
			i += run
			continue
		}
		if k := n.kids[i]; k.leaf() {
			if v := k.value(); v != "" {
				out = append(out, [2]string{k.name, v})
			}
		}
		i++
	}
	return out
}

func recordRun(kids []*node, i int) int {
	name := kids[i].name
	n := 0
	for ; i+n < len(kids); n++ {
		k := kids[i+n]
		if k.name != name || !isRecord(k) {
			break
		}
	}
	return n
}

func isRecord(n *node) bool {
	for _, k := range n.kids {
		if !k.leaf() {
			return false
		}
	}
	return len(n.fields()) > 0
}

func recordTable(records []*node) string {
	var cols []string
	seen := map[string]bool{}
	for _, r := range records {
		for _, f := range r.fields() {
			if !seen[f[0]] {
				seen[f[0]] = true
				cols = append(cols, f[0])
			}
		}
	}
	if len(cols) == 0 {
		return ""
	}
	grid := [][]string{cols}
	for _, r := range records {
		row := make([]string, len(cols))
		for _, f := range r.fields() {
			for c, name := range cols {
				if name == f[0] {
					if row[c] == "" {
						row[c] = f[1]
					}
					break
				}
			}
		}
		grid = append(grid, row)
	}
	return mdrender.Table(mdrender.NormalizeGrid(grid))
}
