package htmldoc

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
)

type ImageSink interface {
	Marker(src string) string
}

type noImages struct{}

func (noImages) Marker(string) string { return "" }

const maxDataURIBytes = 64 << 20

type DataSink struct {
	seq    int
	images []docimg.Image
}

func NewDataSink() *DataSink { return &DataSink{} }

func (d *DataSink) Images() []docimg.Image { return d.images }

func (d *DataSink) Marker(src string) string {
	b, ext, ok := decodeDataURI(src)
	if !ok {
		return ""
	}
	d.seq++
	d.images = append(d.images, docimg.Image{Seq: d.seq, Bytes: b, Ext: ext})
	return docimg.MarkerFor(d.seq)
}

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
