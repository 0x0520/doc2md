// Package docimg defines the extracted-figure type and the "[Image: N]" marker
// convention shared by all readers.
package docimg

import (
	"fmt"
	"regexp"
	"strings"
)

// Image is one raster or SVG figure extracted from a document. Seq is 1-based in
// reading order and matches its marker; Page is the page, slide or sheet it sat
// on (0 if the format has none); Ext is the lowercase extension without a dot.
type Image struct {
	Seq   int
	Page  int
	Bytes []byte
	Ext   string
}

// MarkerFor returns the inline marker for figure seq. MarkerRe must recognise
// exactly this form.
func MarkerFor(seq int) string {
	return fmt.Sprintf("[Image: %d]", seq)
}

// MarkerRe matches a line that consists of nothing but a marker.
var MarkerRe = regexp.MustCompile(`^\[Image: \d+\]$`)

// IsMarker reports whether text is exactly an image marker.
func IsMarker(text string) bool {
	return MarkerRe.MatchString(text)
}

// Ext normalises a media type ("image/png"), extension (".png") or bare name to a
// lowercase extension. It returns "" for types that should not be stored as figures.
func Ext(mediaTypeOrExt string) string {
	s := mediaTypeOrExt
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "."))
	switch s {
	case "jpg", "jpeg", "pjpeg":
		return "jpg"
	case "png", "gif", "bmp", "webp", "tiff":
		return s
	case "tif":
		return "tiff"
	// SVG is kept: a diagram drawn as SVG is a figure the consumer can render.
	case "svg+xml", "svg":
		return "svg"
	// EMF and WMF are vector formats that browsers cannot show and nothing here
	// rasterises.
	case "x-emf", "emf", "x-wmf", "wmf", "x-ms-bmp":
		return ""
	default:
		return ""
	}
}
