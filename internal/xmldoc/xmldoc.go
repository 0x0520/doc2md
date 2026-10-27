// Package xmldoc converts arbitrary, schema-less XML to Markdown by its shape:
// elements become headings, leaf elements become labelled values and runs of
// same-named sibling records become tables.
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

// maxNodes and maxDepth bound the tree built from untrusted input (depth drives
// recursion, node count drives allocation); exceeding them truncates instead of failing.
// minTableRows is the shortest run of same-named records rendered as a table.
const (
	maxNodes     = 200_000
	maxDepth     = 64
	minTableRows = 2
)

// ErrNoContent is returned when the document holds no text at all.
var ErrNoContent = errors.New("no content in XML document")

// node is one element. parts keeps text and children in document order, which
// is what separates prose with inline markup from a record: "<p>a <b>b</b> c</p>"
// and "<f><x/><y/></f>" have the same parts by type but different meaning.
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

// mixed reports whether the element interleaves its own text with child
// elements, i.e. prose with inline markup rather than a record of fields.
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

// flatten renders the element and its descendants as one line of prose in document order.
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

// leaf reports whether the node has no child elements.
func (n *node) leaf() bool { return len(n.kids) == 0 }

// fields returns attributes, own text (labelled textLabel) and leaf children as
// name/value pairs; attributes and child elements are treated alike. With
// skipRuns, children that render as a table (see recordRun) are left out.
func (n *node) fields(textLabel string, skipRuns bool) [][2]string {
	out := make([][2]string, 0, len(n.attrs)+len(n.kids)+1)
	out = append(out, n.attrs...)
	if n.text != "" {
		out = append(out, [2]string{textLabel, n.text})
	}
	for i := 0; i < len(n.kids); {
		if skipRuns {
			if run := recordRun(n.kids, i); run >= minTableRows {
				i += run
				continue
			}
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

// value is a leaf's text with its attributes appended, so none are lost.
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

// ToMarkdown converts XML to Markdown. Unparseable input is an error; input
// that breaks midway is rendered as far as it parsed.
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

// parse builds the element tree, collapsing character data to single spaces
// (indentation is layout, not content) and tolerating non-strict XML.
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
				// Skip the over-limit subtree and continue with its siblings, so the limit costs a subtree, not the document.
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

// render emits a container as a heading with its values, and runs of
// same-named records as tables.
func render(n *node, level int, out *[]string) {
	// Prose with inline markup is kept as a sentence, not split into one labelled line per inline element.
	if n.mixed() {
		if s := n.flatten(); s != "" {
			*out = append(*out, s, "")
		}
		return
	}
	// An empty element (spacer, line break) has no information in its name alone.
	if n.text == "" && len(n.attrs) == 0 && len(n.kids) == 0 {
		return
	}
	*out = append(*out, strings.Repeat("#", min(level, 6))+" "+n.name, "")

	if fields := n.fields("text", true); len(fields) > 0 {
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

// recordRun returns how many consecutive siblings from i share a name and are
// records; such a run is rendered as a table body.
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

// isRecord reports whether n is a flat bag of at least one value (no container children).
func isRecord(n *node) bool {
	for _, k := range n.kids {
		if !k.leaf() {
			return false
		}
	}
	return len(n.fields(n.name, false)) > 0
}

// recordTable renders records as a pipe table. Columns are the union of field
// names in first-seen order, so a missing field leaves a blank cell instead of shifting the row.
func recordTable(records []*node) string {
	var cols []string
	seen := map[string]bool{}
	for _, r := range records {
		for _, f := range r.fields(r.name, false) {
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
		for _, f := range r.fields(r.name, false) {
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
