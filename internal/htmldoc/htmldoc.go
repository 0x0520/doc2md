// Package htmldoc converts HTML to Markdown structurally: headings, lists and
// tables map to their Markdown forms and presentation-only markup is dropped.
// It uses the tolerant HTML5 parser, so malformed input yields a tree, not an error.
package htmldoc

import (
	"bytes"
	"errors"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
)

// ErrNoContent is returned when a page has no text outside its scaffolding.
var ErrNoContent = errors.New("no content in HTML document")

// dropped lists elements whose subtree is never document text (scripts, styling,
// navigation, controls); the whole subtree is skipped.
var dropped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Head: true, atom.Nav: true, atom.Svg: true, atom.Iframe: true,
	atom.Button: true, atom.Form: true, atom.Select: true, atom.Textarea: true,
}

// ToMarkdown converts HTML to Markdown and extracts images given as base64
// data: URIs. Images referencing separate files are not resolved.
func ToMarkdown(data []byte) (string, []docimg.Image, error) {
	sink := NewDataSink()
	md, err := ToMarkdownWith(data, sink)
	return md, sink.Images(), err
}

// ToMarkdownWith is ToMarkdown with a caller-supplied ImageSink, so a container
// format (EPUB) can resolve images stored as sibling entries.
func ToMarkdownWith(data []byte, sink ImageSink) (string, error) {
	if sink == nil {
		sink = noImages{}
	}
	root, err := html.Parse(bytes.NewReader(toUTF8(data)))
	if err != nil {
		return "", err
	}
	var blocks []mdrender.Block
	walk(root, &blocks, sink)
	// Skip the <title> when the body already opens with the same heading; every
	// EPUB chapter repeats it in <head> and <h1>.
	if t := findTitle(root); t != "" && !samePhrase(t, firstHeading(blocks)) {
		blocks = append([]mdrender.Block{{Kind: mdrender.Heading, Level: 1, Text: t}}, blocks...)
	}

	md := strings.TrimSpace(mdrender.Document(blocks))
	if md == "" {
		return "", ErrNoContent
	}
	return md, nil
}

// firstHeading returns the first heading's text; only the first counts as the document's name.
func firstHeading(blocks []mdrender.Block) string {
	for _, b := range blocks {
		if b.Kind == mdrender.Heading {
			return b.Text
		}
	}
	return ""
}

// samePhrase compares case- and whitespace-insensitively; two empty strings do not match.
func samePhrase(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(Collapse(a), Collapse(b))
}

func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.DataAtom == atom.Title {
		return Collapse(text(n))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}

// walk emits block-level structure; inline content is left to inlineText.
func walk(n *html.Node, out *[]mdrender.Block, sink ImageSink) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			if c.Type == html.TextNode {
				// Bare text outside any block element is still content.
				if s := Collapse(c.Data); s != "" {
					*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: s})
				}
			}
			continue
		}
		if dropped[c.DataAtom] || isWordTOC(c) {
			continue
		}
		switch c.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			if s := inlineText(c, sink); s != "" {
				*out = append(*out, mdrender.Block{
					Kind:  mdrender.Heading,
					Level: int(c.Data[1] - '0'),
					Text:  s,
				})
			}
		case atom.P, atom.Blockquote, atom.Dd, atom.Dt, atom.Figcaption, atom.Caption:
			if c.DataAtom == atom.P {
				if item, ok := wordListItem(c, sink); ok {
					*out = append(*out, item)
					continue
				}
			}
			if s := inlineText(c, sink); s != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: s})
			}
		case atom.Pre:
			if s := strings.TrimRight(text(c), "\n"); strings.TrimSpace(s) != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: "```\n" + s + "\n```"})
			}
		case atom.Ul, atom.Ol:
			emitList(c, 0, out, sink)
		case atom.Table:
			if layoutTable(c) {
				walkLayout(c, out, sink)
			} else if t := renderTable(c, sink); t != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Table_, Text: t})
			}
		// A standalone image becomes its own block. If the sink cannot resolve it,
		// alt text is used: a marker pointing at no bytes is worse than none.
		case atom.Img:
			if m := sink.Marker(Attr(c, "src")); m != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: m})
			} else if alt := Attr(c, "alt"); alt != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: alt})
			}
		case atom.Br, atom.Hr, atom.Input:
		default:
			walk(c, out, sink)
		}
	}
}

// emitList renders a list with nesting by indentation. An item's text and its
// nested list are separate blocks so the item text is not swallowed.
func emitList(list *html.Node, depth int, out *[]mdrender.Block, sink ImageSink) {
	marker := "- "
	if list.DataAtom == atom.Ol {
		marker = "1. "
	}
	for li := list.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		if s := inlineText(li, sink); s != "" {
			*out = append(*out, mdrender.Block{
				Kind: mdrender.List,
				Text: strings.Repeat("  ", depth) + marker + s,
			})
		}
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.DataAtom == atom.Ul || c.DataAtom == atom.Ol) {
				emitList(c, depth+1, out, sink)
			}
		}
	}
}

// layoutTable reports whether a table is page layout rather than data: some
// cell holds block content, which a one-line pipe-table cell would drop.
func layoutTable(tbl *html.Node) bool {
	found := false
	eachCell(tbl, func(cell *html.Node) {
		if !found && hasBlocks(cell) {
			found = true
		}
	})
	return found
}

// eachCell visits the cells of the table's own rows, skipping nested tables.
func eachCell(tbl *html.Node, visit func(*html.Node)) {
	var rows func(*html.Node)
	rows = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Tr:
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.DataAtom == atom.Td || cell.DataAtom == atom.Th) {
						visit(cell)
					}
				}
			case atom.Table:
			default:
				rows(c)
			}
		}
	}
	rows(tbl)
}

// hasBlocks reports whether n holds block content a pipe-table cell cannot (lists, headings, tables, pre, several paragraphs).
func hasBlocks(n *html.Node) bool {
	paragraphs := 0
	var look func(*html.Node) bool
	look = func(n *html.Node) bool {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || dropped[c.DataAtom] {
				continue
			}
			switch c.DataAtom {
			case atom.Ul, atom.Ol, atom.Table, atom.Pre, atom.Blockquote,
				atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				return true
			case atom.P, atom.Div:
				if Collapse(text(c)) != "" {
					paragraphs++
				}
				if paragraphs > 1 || look(c) {
					return true
				}
			default:
				if look(c) {
					return true
				}
			}
		}
		return false
	}
	return look(n)
}

// walkLayout emits a layout table in reading order: block cells as blocks,
// consecutive inline cells of a row joined by " — " into one paragraph.
func walkLayout(tbl *html.Node, out *[]mdrender.Block, sink ImageSink) {
	var rows func(*html.Node)
	rows = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.DataAtom != atom.Tr {
				if c.DataAtom != atom.Table {
					rows(c)
				}
				continue
			}
			var inline []string
			flush := func() {
				if len(inline) > 0 {
					*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: strings.Join(inline, " — ")})
					inline = nil
				}
			}
			for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type != html.ElementNode || (cell.DataAtom != atom.Td && cell.DataAtom != atom.Th) {
					continue
				}
				if hasBlocks(cell) {
					flush()
					walk(cell, out, sink)
					continue
				}
				if s := inlineText(cell, sink); s != "" {
					inline = append(inline, s)
				}
			}
			flush()
		}
	}
	rows(tbl)
}

// renderTable gathers rows from thead/tbody/tfoot alike (nested tables are skipped) and renders through
// mdrender so tables match those of other formats.
func renderTable(tbl *html.Node, sink ImageSink) string {
	var grid [][]string
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Tr:
				var row []string
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.DataAtom == atom.Td || cell.DataAtom == atom.Th) {
						row = append(row, inlineText(cell, sink))
					}
				}
				if len(row) > 0 {
					grid = append(grid, row)
				}
			case atom.Table:
			default:
				collect(c)
			}
		}
	}
	collect(tbl)
	return mdrender.Table(mdrender.NormalizeGrid(grid))
}

// inlineText renders inline content, keeping emphasis, code and links.
func inlineText(n *html.Node, sink ImageSink) string {
	var b strings.Builder
	var add func(*html.Node)
	add = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				b.WriteString(c.Data)
			case c.Type != html.ElementNode || dropped[c.DataAtom] || isMsoIgnore(c):
			// Nested lists are emitted as blocks by emitList; including them here would duplicate them.
			case c.DataAtom == atom.Ul || c.DataAtom == atom.Ol:
			case c.DataAtom == atom.Br:
				b.WriteString(" ")
			// Pad cells so adjacent ones do not fuse into one word.
			case c.DataAtom == atom.Td || c.DataAtom == atom.Th || c.DataAtom == atom.Tr:
				b.WriteString(" ")
				add(c)
				b.WriteString(" ")
			case c.DataAtom == atom.Strong || c.DataAtom == atom.B:
				b.WriteString(wrap(inlineText(c, sink), "**"))
			case c.DataAtom == atom.Em || c.DataAtom == atom.I:
				b.WriteString(wrap(inlineText(c, sink), "_"))
			case c.DataAtom == atom.Code || c.DataAtom == atom.Kbd || c.DataAtom == atom.Samp:
				b.WriteString(wrap(text(c), "`"))
			case c.DataAtom == atom.A:
				label := inlineText(c, sink)
				if href := Attr(c, "href"); href != "" && label != "" && !activeScheme(href) {
					b.WriteString("[" + label + "](" + href + ")")
				} else {
					b.WriteString(label)
				}
			// The marker is padded so it does not fuse with surrounding words.
			case c.DataAtom == atom.Img:
				if m := sink.Marker(Attr(c, "src")); m != "" {
					b.WriteString(" " + m + " ")
				} else if alt := Attr(c, "alt"); alt != "" {
					b.WriteString(alt)
				}
			default:
				add(c)
			}
		}
	}
	add(n)
	return Collapse(b.String())
}

// wrap returns "" for blank text so an empty element leaves no stray markers.
func wrap(s, with string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return with + s + with
}

func text(n *html.Node) string {
	var b strings.Builder
	var add func(*html.Node)
	add = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			} else if c.Type == html.ElementNode && !dropped[c.DataAtom] {
				add(c)
			}
		}
	}
	add(n)
	return b.String()
}

// Attr returns the value of the named attribute, or "" if it is absent.
func Attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// activeScheme reports whether href runs code or embeds a payload instead of
// pointing at a document. Browsers ignore leading whitespace and control
// characters and drop tabs and newlines inside the scheme, so both are removed
// before matching.
func activeScheme(href string) bool {
	clean := strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f {
			return -1
		}
		return unicode.ToLower(r)
	}, href)
	for _, scheme := range []string{"javascript:", "vbscript:", "data:"} {
		if strings.HasPrefix(clean, scheme) {
			return true
		}
	}
	return false
}

// Collapse joins the whitespace-separated fields of s with single spaces.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
