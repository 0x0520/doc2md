package htmldoc

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	// noteRefRe and noteBodyRe match the anchor names Word gives footnote and
	// endnote references ("_ftnref1") and their bodies ("_ftn1", "_edn1").
	noteRefRe  = regexp.MustCompile(`^_(ftn|edn)ref(\w+)$`)
	noteBodyRe = regexp.MustCompile(`^_(ftn|edn)(\w+)$`)
	// absoluteRe matches the style of a floating text box; Word breaks the
	// declaration across lines ("position:\nabsolute").
	absoluteRe = regexp.MustCompile(`(?i)position:\s*absolute`)
)

// prepareWord rewrites Word-specific constructs in the tree before the walk:
// comments and tracked deletions disappear, floating text boxes move behind
// their paragraph, and footnotes become "[^N]" references. It returns the
// footnote definitions for the end of the document.
func prepareWord(root *html.Node, sink ImageSink) []string {
	var all []*html.Node
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				all = append(all, c)
				collect(c)
			}
		}
	}
	collect(root)

	for _, n := range all {
		if n.Parent != nil && isWordNoise(n) {
			n.Parent.RemoveChild(n)
		}
	}
	for _, n := range all {
		if n.Parent != nil && isFloatingBox(n) && !insideFloatingBox(n) {
			moveBox(n)
		}
	}
	return extractNotes(root, sink)
}

func hasClass(n *html.Node, name string) bool {
	for _, c := range strings.Fields(Attr(n, "class")) {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

// isWordNoise reports whether n is a review comment (anchor, text or marker) or
// a tracked deletion; neither is part of the document's text.
func isWordNoise(n *html.Node) bool {
	for _, c := range []string{"msocomanchor", "msocomtxt", "msocomoff", "MsoCommentReference", "msoDel"} {
		if hasClass(n, c) {
			return true
		}
	}
	switch n.DataAtom {
	case atom.A:
		return strings.HasPrefix(Attr(n, "href"), "#_msocom_")
	case atom.Del:
		// A plain <del> is struck text that stays visible; Word's carries the author.
		return strings.HasPrefix(Attr(n, "cite"), "mailto:")
	}
	return false
}

// isFloatingBox reports whether n is a text box: an absolutely positioned span
// or a VML textbox.
func isFloatingBox(n *html.Node) bool {
	return n.Data == "v:textbox" || (n.DataAtom == atom.Span && absoluteRe.MatchString(Attr(n, "style")))
}

func insideFloatingBox(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && isFloatingBox(p) {
			return true
		}
	}
	return false
}

// moveBox puts a text box behind the paragraph that anchors it, so its text is
// its own paragraph instead of being glued into the sentence. A box outside a
// paragraph, or in a list item or table cell, stays where it is.
func moveBox(box *html.Node) {
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		switch p.DataAtom {
		case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Dd, atom.Dt:
			box.Parent.RemoveChild(box)
			if hasBlocks(box, true) {
				box.DataAtom, box.Data = atom.Div, "div"
			} else {
				box.DataAtom, box.Data = atom.P, "p"
			}
			p.Parent.InsertBefore(box, p.NextSibling)
			return
		case atom.Li, atom.Td, atom.Th, atom.Table, atom.Body:
			return
		}
	}
}

// extractNotes replaces Word's footnote and endnote references with "[^N]" and
// removes the note bodies, returning "[^N]: text" for each. Footnotes and
// endnotes share one sequence in order of reference. A reference whose body is
// missing stays a link.
func extractNotes(root *html.Node, sink ImageSink) []string {
	type ref struct {
		a   *html.Node
		key string
	}
	var refs []ref
	bodies := map[string]*html.Node{}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.DataAtom == atom.A {
				name := Attr(c, "name")
				if name == "" {
					name = Attr(c, "id")
				}
				if m := noteRefRe.FindStringSubmatch(name); m != nil {
					if Attr(c, "href") == "#_"+m[1]+m[2] {
						refs = append(refs, ref{c, m[1] + m[2]})
					}
				} else if m := noteBodyRe.FindStringSubmatch(name); m != nil {
					bodies[m[1]+m[2]] = c
				}
			}
			visit(c)
		}
	}
	visit(root)

	var defs []string
	for _, r := range refs {
		body := bodies[r.key]
		if body == nil || body.Parent == nil || r.a.Parent == nil {
			continue
		}
		container := noteContainer(body, r.key)
		body.Parent.RemoveChild(body)
		s := noteText(container, sink)
		if s == "" {
			continue
		}
		label := "[^" + strconv.Itoa(len(defs)+1) + "]"
		r.a.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: label}, r.a)
		r.a.Parent.RemoveChild(r.a)
		if container.Parent != nil {
			container.Parent.RemoveChild(container)
		}
		defs = append(defs, label+": "+s)
	}
	return defs
}

// noteContainer finds what holds a note body: Word's div id="ftn1", else the
// paragraph around the back-link anchor.
func noteContainer(body *html.Node, key string) *html.Node {
	for p := body.Parent; p != nil; p = p.Parent {
		if p.DataAtom == atom.Div && Attr(p, "id") == key {
			return p
		}
	}
	for p := body.Parent; p != nil; p = p.Parent {
		if p.DataAtom == atom.P {
			return p
		}
	}
	return body.Parent
}

// noteText renders a note; paragraphs of a multi-paragraph note are joined with
// a space because a footnote definition is one line here.
func noteText(n *html.Node, sink ImageSink) string {
	var parts []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.DataAtom == atom.P || c.DataAtom == atom.Div) {
			if s := inlineText(c, sink); s != "" {
				parts = append(parts, s)
			}
		}
	}
	if len(parts) > 0 && Collapse(text(n)) == Collapse(strings.Join(parts, " ")) {
		return strings.Join(parts, " ")
	}
	return inlineText(n, sink)
}
