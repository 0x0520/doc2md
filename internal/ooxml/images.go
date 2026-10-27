package ooxml

import (
	"archive/zip"
	"encoding/xml"
	"path"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/zipx"
)

// maxImageBytes caps the total image data extracted from one document.
const maxImageBytes = 128 << 20

// media extracts images from a package's media/ parts on demand and numbers them
// in reading order.
type media struct {
	files  map[string]*zip.File
	seq    int
	total  int
	images []docimg.Image
}

func newMedia(zr *zip.Reader) *media {
	m := &media{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		if strings.Contains(f.Name, "/media/") {
			m.files[f.Name] = f
		}
	}
	return m
}

// marker extracts the image at part and returns its "[Image: N]" marker, or "" if
// it is skipped (unknown type, size cap reached, unreadable or empty).
func (m *media) marker(part string, page int) string {
	f := m.files[part]
	if f == nil {
		return ""
	}
	ext := docimg.Ext(path.Ext(part))
	if ext == "" {
		return ""
	}
	if m.total >= maxImageBytes {
		return ""
	}
	b, err := zipx.ReadEntry(f)
	if err != nil || len(b) == 0 {
		return ""
	}
	m.total += len(b)
	m.seq++
	m.images = append(m.images, docimg.Image{Seq: m.seq, Page: page, Bytes: b, Ext: ext})
	return docimg.MarkerFor(m.seq)
}

// relTargets resolves internal relationship targets to package paths. Relative
// targets are taken from baseDir; a leading "/" means package-absolute.
func relTargets(b []byte, baseDir string) map[string]string {
	out := map[string]string{}
	if len(b) == 0 {
		return out
	}
	var doc struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, r := range doc.Rels {
		if r.Mode == "External" || r.Target == "" {
			continue
		}
		t := r.Target
		if strings.HasPrefix(t, "/") {
			out[r.ID] = strings.TrimPrefix(t, "/")
			continue
		}
		out[r.ID] = path.Clean(path.Join(baseDir, t))
	}
	return out
}

// embedID reads up to the closing tag and returns the relationship id of the first
// image: DrawingML a:blip (r:embed) or legacy VML v:imagedata (r:id).
func embedID(dec *xml.Decoder, closing string) string {
	id := ""
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return id
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "blip":
				if v := attr(t, "embed"); v != "" && id == "" {
					id = v
				}
			case "imagedata":
				if v := attr(t, "id"); v != "" && id == "" {
					id = v
				}
			}
			if t.Name.Local == closing {
				depth++
			}
		case xml.EndElement:
			if t.Name.Local == closing {
				depth--
			}
		}
	}
	return id
}
