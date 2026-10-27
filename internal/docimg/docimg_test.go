package docimg

import "testing"

func TestMarkerForMatchesTheConvention(t *testing.T) {
	for _, tc := range []struct {
		seq  int
		want string
	}{{1, "[Image: 1]"}, {42, "[Image: 42]"}} {
		if got := MarkerFor(tc.seq); got != tc.want {
			t.Errorf("MarkerFor(%d) = %q, want %q", tc.seq, got, tc.want)
		}
		if !IsMarker(tc.want) {
			t.Errorf("IsMarker(%q) = false — the writer and the reader have drifted apart", tc.want)
		}
	}
	for _, s := range []string{"", "[Image: ]", "Text [Image: 1]", "[Image: 1] Text", "[image: 1]"} {
		if IsMarker(s) {
			t.Errorf("IsMarker(%q) = true, want false", s)
		}
	}
}

func TestExt(t *testing.T) {
	for in, want := range map[string]string{
		"image/png":     "png",
		"image/jpeg":    "jpg",
		".JPG":          "jpg",
		"png":           "png",
		".gif":          "gif",
		"image/webp":    "webp",
		".tif":          "tiff",
		"image/svg+xml": "svg",

		"image/x-emf":     "",
		".wmf":            "",
		"application/pdf": "",
		"":                "",
		"image/":          "",
	} {
		if got := Ext(in); got != want {
			t.Errorf("Ext(%q) = %q, want %q", in, got, want)
		}
	}
}
