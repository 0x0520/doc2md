package docimg

import (
	"fmt"
	"regexp"
	"strings"
)

type Image struct {
	Seq   int
	Page  int
	Bytes []byte
	Ext   string
}

func MarkerFor(seq int) string {
	return fmt.Sprintf("[Image: %d]", seq)
}

var MarkerRe = regexp.MustCompile(`^\[Image: \d+\]$`)

func IsMarker(text string) bool {
	return MarkerRe.MatchString(text)
}

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
	case "svg+xml", "svg":
		return "svg"
	case "x-emf", "emf", "x-wmf", "wmf", "x-ms-bmp":
		return ""
	default:
		return ""
	}
}
