package pdfnative

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"sort"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type Image = docimg.Image

type rawImage struct {
	bytes []byte
	ext   string
	w, h  int
}

func pdfcpuConfig() *model.Configuration {
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}

func extractRasterImages(data []byte) (out map[int]map[string]rawImage) {
	out = map[int]map[string]rawImage{}
	defer func() { _ = recover() }()
	conf := pdfcpuConfig()
	_ = api.ExtractImages(context.Background(), bytes.NewReader(data), nil, func(img model.Image, _ bool, _ int) error {
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

const decoPosBucket = 20.0

const unplacedTopY = -1e18

func decoSignature(ri rawImage, topY float64) string {
	if ri.w > 0 && ri.h > 0 {
		return fmt.Sprintf("%dx%d@%d", ri.w, ri.h, int(topY/decoPosBucket))
	}
	sum := sha1.Sum(ri.bytes)
	return "b:" + hex.EncodeToString(sum[:])
}

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
