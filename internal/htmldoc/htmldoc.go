package htmldoc

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
)

var ErrNoContent = errors.New("no content in HTML document")

var dropped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Head: true, atom.Nav: true, atom.Svg: true, atom.Iframe: true,
	atom.Button: true, atom.Form: true, atom.Select: true, atom.Textarea: true,
}

func ToMarkdown(data []byte) (string, []docimg.Image, error) {
	sink := NewDataSink()
	md, err := ToMarkdownWith(data, sink)
	return md, sink.Images(), err
}

func ToMarkdownWith(data []byte, sink ImageSink) (string, error) {
	if sink == nil {
		sink = noImages{}
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	var blocks []mdrender.Block
	walk(root, &blocks, sink)
	if t := findTitle(root); t != "" && !samePhrase(t, firstHeading(blocks)) {
		blocks = append([]mdrender.Block{{Kind: mdrender.Heading, Level: 1, Text: t}}, blocks...)
	}

	md := strings.TrimSpace(mdrender.Document(blocks))
	if md == "" {
		return "", ErrNoContent
	}
	return md, nil
}

func firstHeading(blocks []mdrender.Block) string {
	for _, b := range blocks {
		if b.Kind == mdrender.Heading {
			return b.Text
		}
	}
	return ""
}

func samePhrase(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(collapse(a), collapse(b))
}

func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.DataAtom == atom.Title {
		return collapse(text(n))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}

func walk(n *html.Node, out *[]mdrender.Block, sink ImageSink) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			if c.Type == html.TextNode {
				if s := collapse(c.Data); s != "" {
					*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: s})
				}
			}
			continue
		}
		if dropped[c.DataAtom] {
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
		case atom.Img:
			if m := sink.Marker(attr(c, "src")); m != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: m})
			} else if alt := attr(c, "alt"); alt != "" {
				*out = append(*out, mdrender.Block{Kind: mdrender.Paragraph, Text: alt})
			}
		case atom.Br, atom.Hr, atom.Input:
		default:
			walk(c, out, sink)
		}
	}
}

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

func layoutTable(tbl *html.Node) bool {
	found := false
	eachCell(tbl, func(cell *html.Node) {
		if !found && hasBlocks(cell) {
			found = true
		}
	})
	return found
}

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
				if collapse(text(c)) != "" {
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

func inlineText(n *html.Node, sink ImageSink) string {
	var b strings.Builder
	var add func(*html.Node)
	add = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				b.WriteString(c.Data)
			case c.Type != html.ElementNode || dropped[c.DataAtom]:
			case c.DataAtom == atom.Ul || c.DataAtom == atom.Ol:
			case c.DataAtom == atom.Br:
				b.WriteString(" ")
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
				if href := attr(c, "href"); href != "" && label != "" && !strings.HasPrefix(href, "javascript:") {
					b.WriteString("[" + label + "](" + href + ")")
				} else {
					b.WriteString(label)
				}
			case c.DataAtom == atom.Img:
				if m := sink.Marker(attr(c, "src")); m != "" {
					b.WriteString(" " + m + " ")
				} else if alt := attr(c, "alt"); alt != "" {
					b.WriteString(alt)
				}
			default:
				add(c)
			}
		}
	}
	add(n)
	return collapse(b.String())
}

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

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
