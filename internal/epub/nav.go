package epub

import (
	"archive/zip"
	"encoding/xml"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/0x0520/doc2md/internal/zipx"
)

const maxNavDepth = 6

func navDepths(zr *zip.Reader, base string, p pkg) map[string]int {
	for _, it := range p.Manifest {
		if it.Href == "" {
			continue
		}
		switch {
		case strings.Contains(it.Properties, "nav"):
			if d := depthsFromXHTML(zr, base, it.Href); len(d) > 0 {
				return d
			}
		case it.MediaType == "application/x-dtbncx+xml":
			if d := depthsFromNCX(zr, base, it.Href); len(d) > 0 {
				return d
			}
		}
	}
	return nil
}

func readPart(zr *zip.Reader, base, href string) []byte {
	f := zipx.Find(zr, resolve(base, href))
	if f == nil {
		return nil
	}
	b, err := zipx.ReadEntry(f)
	if err != nil {
		return nil
	}
	return b
}

func depthsFromXHTML(zr *zip.Reader, base, href string) map[string]int {
	raw := readPart(zr, base, href)
	if raw == nil {
		return nil
	}
	root, err := html.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	list := tocList(root)
	if list == nil {
		return nil
	}
	out := map[string]int{}
	walkList(list, 1, path.Dir(resolve(base, href)), out)
	return out
}

func tocList(n *html.Node) *html.Node {
	var firstOL, tocOL *html.Node
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inToc bool) {
		if n.Type == html.ElementNode {
			if n.Data == "nav" {
				t := attrOf(n, "epub:type")
				inToc = t == "" || strings.Contains(t, "toc")
			}
			if n.DataAtom == atom.Ol {
				if firstOL == nil {
					firstOL = n
				}
				if inToc && tocOL == nil {
					tocOL = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inToc)
		}
	}
	walk(n, false)
	if tocOL != nil {
		return tocOL
	}
	return firstOL
}

func walkList(list *html.Node, depth int, base string, out map[string]int) {
	if depth > maxNavDepth {
		return
	}
	for li := list.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.A:
				if h := attrOf(c, "href"); h != "" {
					record(out, resolve(base, h), depth)
				}
			case atom.Ol, atom.Ul:
				walkList(c, depth+1, base, out)
			}
		}
	}
}

func record(out map[string]int, name string, depth int) {
	if name == "" {
		return
	}
	if prev, seen := out[name]; !seen || depth < prev {
		out[name] = depth
	}
}

func depthsFromNCX(zr *zip.Reader, base, href string) map[string]int {
	raw := readPart(zr, base, href)
	if raw == nil {
		return nil
	}
	var doc ncx
	if xml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	navBase := path.Dir(resolve(base, href))
	out := map[string]int{}
	var walk func([]navPoint, int)
	walk = func(points []navPoint, depth int) {
		if depth > maxNavDepth {
			return
		}
		for _, pt := range points {
			record(out, resolve(navBase, pt.Content.Src), depth)
			walk(pt.Children, depth+1)
		}
	}
	walk(doc.Points, 1)
	return out
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func shiftHeadings(md string, n int) string {
	if n <= 0 {
		return md
	}
	lines := strings.Split(md, "\n")
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(l, "#") {
			continue
		}
		level := len(l) - len(strings.TrimLeft(l, "#"))
		if level == 0 || level >= len(l) || l[level] != ' ' {
			continue
		}
		lines[i] = strings.Repeat("#", min(level+n, 6)) + l[level:]
	}
	return strings.Join(lines, "\n")
}
