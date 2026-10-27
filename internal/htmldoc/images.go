package htmldoc

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
)

// ImageSink maps an <img> src to the marker standing for the picture in the
// text, and stores its bytes. "" means the reference could not be resolved. It
// is an interface because only the caller knows where the bytes live (inline
// data: URI, sibling EPUB entry, or a file that was never handed over).
type ImageSink interface {
	Marker(src string) string
}

// noImages resolves nothing; it is used when no sink is given.
type noImages struct{}

func (noImages) Marker(string) string { return "" }

// maxDataURIBytes caps the decoded size of one inline image.
const maxDataURIBytes = 64 << 20

// DataSink collects images given as base64 data: URIs and ignores all other references.
type DataSink struct {
	seq    int
	images []docimg.Image
}

// NewDataSink returns an empty sink for one document.
func NewDataSink() *DataSink { return &DataSink{} }

// Images returns the collected images in marker order.
func (d *DataSink) Images() []docimg.Image { return d.images }

// Marker decodes a data: URI, stores the image and returns its marker.
func (d *DataSink) Marker(src string) string {
	b, ext, ok := decodeDataURI(src)
	if !ok {
		return ""
	}
	d.seq++
	d.images = append(d.images, docimg.Image{Seq: d.seq, Bytes: b, Ext: ext})
	return docimg.MarkerFor(d.seq)
}

// decodeDataURI accepts only base64 image/* payloads with a known extension.
func decodeDataURI(src string) (data []byte, ext string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(strings.ToLower(src), prefix) {
		return nil, "", false
	}
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, "", false
	}
	meta := strings.ToLower(src[len(prefix):comma])
	payload := src[comma+1:]
	if !strings.Contains(meta, "base64") {
		return nil, "", false
	}
	mediaType := meta
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	if !strings.HasPrefix(mediaType, "image/") {
		return nil, "", false
	}
	ext = docimg.Ext(mediaType)
	if ext == "" {
		return nil, "", false
	}
	// Whitespace inside base64 is legal in HTML attributes but rejected by the decoder.
	payload = strings.Join(strings.Fields(payload), "")
	if base64.StdEncoding.DecodedLen(len(payload)) > maxDataURIBytes {
		return nil, "", false
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(b) == 0 {
		return nil, "", false
	}
	return b, ext, true
}

// CleanSrc strips query and fragment from src and percent-decodes it, giving
// the name of a package entry; exported for container formats such as EPUB.
func CleanSrc(src string) string {
	s := src
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if dec, err := url.PathUnescape(s); err == nil {
		s = dec
	}
	return s
}
