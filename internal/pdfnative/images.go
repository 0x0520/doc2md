package pdfnative

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"sort"
	"sync"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Image is a raster image extracted from the PDF. Its Seq matches the marker line inserted into the text.
type Image = docimg.Image

// rawImage is an image as stored in the PDF; w and h are 0 if it cannot be decoded.
type rawImage struct {
	bytes []byte
	ext   string
	w, h  int
}

// disableConfigDir guards the one-time call that stops pdfcpu from creating a config directory on disk.
var disableConfigDir sync.Once

func pdfcpuConfig() *model.Configuration {
	disableConfigDir.Do(api.DisableConfigDir)
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}

// extractRasterImages returns the page images by page number (1-based) and resource name.
func extractRasterImages(data []byte) (out map[int]map[string]rawImage) {
	out = map[int]map[string]rawImage{}
	defer func() { _ = recover() }()
	conf := pdfcpuConfig()
	_ = api.ExtractImages(bytes.NewReader(data), nil, func(img model.Image, _ bool, _ int) error {
		b, err := io.ReadAll(img)
		if err != nil || len(b) == 0 {
			return nil
		}
		if out[img.PageNr] == nil {
			out[img.PageNr] = map[string]rawImage{}
		}
		w, h := decodeDims(b)
		out[img.PageNr][img.Name] = rawImage{bytes: b, ext: normExt(img.FileType), w: w, h: h}
		return nil
	}, conf)
	return out
}

func decodeDims(b []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// decoPosBucket is the vertical bucket size (points) of an image signature, so the same logo at a slightly different height still matches.
const decoPosBucket = 20.0

// unplacedTopY stands for images that no Do operator drew; they sort to the end of the page.
const unplacedTopY = -1e18

// decoSignature identifies an image across pages by pixel size and coarse height, or by content hash when it cannot be decoded.
func decoSignature(ri rawImage, topY float64) string {
	if ri.w > 0 && ri.h > 0 {
		return fmt.Sprintf("%dx%d@%d", ri.w, ri.h, int(topY/decoPosBucket))
	}
	sum := sha1.Sum(ri.bytes)
	return "b:" + hex.EncodeToString(sum[:])
}

// runningDecorations returns the signatures of images that recur on at least half the
// pages (minimum 2): logos and banners, not figures.
func runningDecorations(pages []page, imgs map[int]map[string]rawImage, numPages int) map[string]bool {
	pagesBySig := map[string]map[int]bool{}
	note := func(sig string, page int) {
		if pagesBySig[sig] == nil {
			pagesBySig[sig] = map[int]bool{}
		}
		pagesBySig[sig][page] = true
	}
	for pi := range pages {
		byName := imgs[pages[pi].num]
		if len(byName) == 0 {
			continue
		}
		consumed := map[string]bool{}
		for _, pl := range pages[pi].places {
			if ri, ok := byName[pl.name]; ok {
				consumed[pl.name] = true
				note(decoSignature(ri, pl.topY), pages[pi].num)
			}
		}
		for name, ri := range byName {
			if !consumed[name] {
				note(decoSignature(ri, unplacedTopY), pages[pi].num)
			}
		}
	}
	threshold := numPages / 2
	if threshold < 2 {
		threshold = 2
	}
	running := map[string]bool{}
	for sig, pgs := range pagesBySig {
		if len(pgs) >= threshold {
			running[sig] = true
		}
	}
	return running
}

// placeImages numbers the remaining images in page order and inserts a marker line at
// each one's vertical position in the page's lines.
func placeImages(pages []page, imgs map[int]map[string]rawImage, numPages int, verbatim bool) []Image {
	if len(imgs) == 0 {
		return nil
	}
	var running map[string]bool
	if !verbatim {
		running = runningDecorations(pages, imgs, numPages)
	}

	type fig struct {
		topY float64
		ri   rawImage
	}
	var out []Image
	seq := 0
	for pi := range pages {
		pg := &pages[pi]
		byName := imgs[pg.num]
		if len(byName) == 0 {
			continue
		}
		consumed := map[string]bool{}
		var figs []fig
		for _, pl := range pg.places {
			ri, ok := byName[pl.name]
			if !ok {
				continue
			}
			consumed[pl.name] = true
			if running[decoSignature(ri, pl.topY)] {
				continue
			}
			figs = append(figs, fig{topY: pl.topY, ri: ri})
		}
		var rest []string
		for name := range byName {
			if !consumed[name] && !running[decoSignature(byName[name], unplacedTopY)] {
				rest = append(rest, name)
			}
		}
		// Sorted so that numbering is deterministic.
		sort.Strings(rest)
		for _, name := range rest {
			figs = append(figs, fig{topY: unplacedTopY, ri: byName[name]})
		}

		sort.SliceStable(figs, func(a, b int) bool { return figs[a].topY > figs[b].topY })
		for _, f := range figs {
			seq++
			out = append(out, Image{Seq: seq, Page: pg.num, Bytes: f.ri.bytes, Ext: f.ri.ext})
			insertMarkerLine(pg, seq, f.topY)
		}
	}
	return out
}

// insertMarkerLine inserts the marker before the first line below topY, keeping lines ordered top to bottom.
func insertMarkerLine(pg *page, seq int, topY float64) {
	m := line{text: markerFor(seq), y: topY}
	idx := len(pg.lines)
	for i, ln := range pg.lines {
		if ln.y < topY {
			idx = i
			break
		}
	}
	pg.lines = append(pg.lines, line{})
	copy(pg.lines[idx+1:], pg.lines[idx:])
	pg.lines[idx] = m
}

func markerFor(seq int) string { return docimg.MarkerFor(seq) }

func isImageMarker(text string) bool { return docimg.IsMarker(text) }

func normExt(fileType string) string {
	switch fileType {
	case "jpg", "jpeg":
		return "jpg"
	case "":
		return "png"
	default:
		return fileType
	}
}
